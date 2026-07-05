package ember

import "fmt"

type vmCoroutineStatus string

const (
	vmCoroutineSuspended vmCoroutineStatus = "suspended"
	vmCoroutineRunning   vmCoroutineStatus = "running"
	vmCoroutineNormal    vmCoroutineStatus = "normal"
	vmCoroutineDead      vmCoroutineStatus = "dead"
)

type vmCoroutine struct {
	status        vmCoroutineStatus
	thread        vmThread
	root          *closure
	userdata      *UserData
	suspended     vmSuspendedFrames
	yieldedValues []Value
	yieldedInline [2]Value
	resumeArgs    []Value
	resumeResults []Value
	err           error
}

func baseCoroutine() *Table {
	table := newTableWithCapacity(0, 8)
	_ = table.Set(StringValue("create"), nativeFuncValue(baseCoroutineCreate))
	_ = table.Set(StringValue("resume"), nativeFuncValueWithID(baseCoroutineResume, nativeFuncCoroutineResume))
	_ = table.Set(StringValue("yield"), nativeFuncValue(baseCoroutineYield))
	_ = table.Set(StringValue("status"), nativeFuncValueWithID(baseCoroutineStatusNative, nativeFuncCoroutineStatus))
	_ = table.Set(StringValue("close"), HostFuncValue(baseCoroutineClose))
	_ = table.Set(StringValue("running"), nativeFuncValue(baseCoroutineRunning))
	_ = table.Set(StringValue("isyieldable"), nativeFuncValue(baseCoroutineIsYieldable))
	_ = table.Set(StringValue("wrap"), nativeFuncValue(baseCoroutineWrap))
	return table
}

func baseCoroutineCreate(globals *globalEnv, args []Value) ([]Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("coroutine.create: missing function")
	}
	closure, ok := args[0].scriptFunction()
	if !ok {
		return nil, fmt.Errorf("coroutine.create: argument is %s, want function", args[0].Kind())
	}
	coroutine := newVMCoroutine(globals, closure)
	return []Value{UserDataValue(coroutine.userdata)}, nil
}

func newVMCoroutine(globals *globalEnv, root *closure) *vmCoroutine {
	coroutine := &vmCoroutine{
		status: vmCoroutineSuspended,
		thread: newVMThread(globals),
		root:   root,
	}
	if globals != nil {
		coroutine.thread.inheritRuntimeState(globals.thread)
	}
	coroutine.userdata = NewUserData(coroutine)
	return coroutine
}

func baseCoroutineResume(globals *globalEnv, args []Value) ([]Value, error) {
	coroutine, err := coroutineArg("coroutine.resume", args, 0)
	if err != nil {
		return nil, err
	}
	if coroutine.status == vmCoroutineDead {
		return []Value{BoolValue(false), StringValue("cannot resume dead coroutine")}, nil
	}
	if coroutine.status == vmCoroutineRunning {
		return []Value{BoolValue(false), StringValue("cannot resume running coroutine")}, nil
	}
	if coroutine.status == vmCoroutineNormal {
		return []Value{BoolValue(false), StringValue("cannot resume non-suspended coroutine")}, nil
	}

	coroutine.status = vmCoroutineRunning
	coroutine.resumeArgs = append(coroutine.resumeArgs[:0], args[1:]...)
	parent, restoreParent := activeParentCoroutine(globals, coroutine)
	if parent != nil {
		parent.status = vmCoroutineNormal
		defer restoreParent()
	}
	parentThread := activeThread(globals)
	if parentThread != nil && parentThread.instructionBudget >= 0 {
		coroutine.thread.ctx = parentThread.ctx
		coroutine.thread.instructionBudget = parentThread.instructionBudget
		defer func() {
			parentThread.instructionBudget = coroutine.thread.instructionBudget
		}()
	}
	results, err := resumeCoroutine(coroutine, globals, args[1:])
	if yield, ok := err.(vmYieldRequest); ok {
		coroutine.status = vmCoroutineSuspended
		coroutine.yieldedValues = append(coroutine.yieldedValues[:0], yield.values...)
		coroutine.thread.coroutine = coroutine
		coroutine.suspended = coroutine.thread.suspendFrames()
		return coroutine.resumeResult(true, yield.values), nil
	}
	if err != nil {
		coroutine.status = vmCoroutineDead
		coroutine.err = err
		coroutine.suspended = vmSuspendedFrames{}
		return []Value{BoolValue(false), StringValue(err.Error())}, nil
	}
	coroutine.status = vmCoroutineDead
	coroutine.suspended = vmSuspendedFrames{}
	return coroutine.resumeResult(true, results), nil
}

func (coroutine *vmCoroutine) resumeResult(ok bool, values []Value) []Value {
	needed := 1 + len(values)
	if cap(coroutine.resumeResults) < needed {
		coroutine.resumeResults = make([]Value, needed)
	} else {
		coroutine.resumeResults = coroutine.resumeResults[:needed]
	}
	coroutine.resumeResults[0] = BoolValue(ok)
	copy(coroutine.resumeResults[1:], values)
	return coroutine.resumeResults
}

func activeParentCoroutine(globals *globalEnv, child *vmCoroutine) (*vmCoroutine, func()) {
	if globals == nil || globals.thread == nil {
		return nil, func() {}
	}
	parent := globals.thread.coroutine
	if parent == nil || parent == child || parent.status != vmCoroutineRunning {
		return nil, func() {}
	}
	previousStatus := parent.status
	return parent, func() {
		parent.status = previousStatus
	}
}

func resumeCoroutine(coroutine *vmCoroutine, globals *globalEnv, args []Value) ([]Value, error) {
	coroutine.thread.globals = globals
	previousCoroutine := coroutine.thread.coroutine
	coroutine.thread.coroutine = coroutine
	defer func() {
		coroutine.thread.coroutine = previousCoroutine
	}()

	if len(coroutine.suspended.frames) > 0 {
		coroutine.thread.resumeFrames(coroutine.suspended)
		coroutine.thread.coroutine = coroutine
		coroutine.suspended = vmSuspendedFrames{}
		return coroutine.thread.continueSuspended(args)
	}
	return coroutine.thread.run(coroutine.root.proto, args, coroutine.root.upvalues)
}

func baseCoroutineYield(globals *globalEnv, args []Value) ([]Value, error) {
	if globals == nil || globals.thread == nil || globals.thread.coroutine == nil {
		return nil, fmt.Errorf("coroutine.yield: outside coroutine")
	}
	if !globals.thread.isYieldable() {
		return nil, fmt.Errorf("coroutine.yield: not yieldable")
	}
	coroutine := globals.thread.coroutine
	if coroutine.status != vmCoroutineRunning {
		return nil, fmt.Errorf("coroutine.yield: coroutine is not running")
	}
	if len(args) <= len(coroutine.yieldedInline) {
		copy(coroutine.yieldedInline[:], args)
		coroutine.yieldedValues = coroutine.yieldedInline[:len(args)]
	} else {
		coroutine.yieldedValues = append(coroutine.yieldedValues[:0], args...)
	}
	return nil, vmYieldRequest{values: coroutine.yieldedValues}
}

func baseCoroutineStatusNative(_ *globalEnv, args []Value) ([]Value, error) {
	return baseCoroutineStatus(args)
}

func baseCoroutineStatus(args []Value) ([]Value, error) {
	status, err := baseCoroutineStatusValue(args)
	if err != nil {
		return nil, err
	}
	return []Value{status}, nil
}

func baseCoroutineStatusValue(args []Value) (Value, error) {
	coroutine, err := coroutineArg("coroutine.status", args, 0)
	if err != nil {
		return NilValue(), err
	}
	return StringValue(string(coroutine.status)), nil
}

func baseCoroutineClose(args []Value) ([]Value, error) {
	coroutine, err := coroutineArg("coroutine.close", args, 0)
	if err != nil {
		return nil, err
	}
	if coroutine.status == vmCoroutineRunning {
		return nil, fmt.Errorf("coroutine.close: cannot close running coroutine")
	}
	if coroutine.err != nil {
		err := coroutine.err
		coroutine.err = nil
		coroutine.status = vmCoroutineDead
		coroutine.suspended = vmSuspendedFrames{}
		coroutine.thread.frames = nil
		return []Value{BoolValue(false), StringValue(err.Error())}, nil
	}
	coroutine.status = vmCoroutineDead
	coroutine.suspended = vmSuspendedFrames{}
	coroutine.thread.frames = nil
	return []Value{BoolValue(true)}, nil
}

func baseCoroutineRunning(globals *globalEnv, args []Value) ([]Value, error) {
	if globals == nil || globals.thread == nil || globals.thread.coroutine == nil {
		return []Value{NilValue()}, nil
	}
	coroutine := globals.thread.coroutine
	if coroutine.userdata == nil {
		return []Value{NilValue()}, nil
	}
	return []Value{UserDataValue(coroutine.userdata)}, nil
}

func baseCoroutineIsYieldable(globals *globalEnv, args []Value) ([]Value, error) {
	yieldable := globals != nil &&
		globals.thread != nil &&
		globals.thread.isYieldable() &&
		globals.thread.coroutine != nil &&
		globals.thread.coroutine.status == vmCoroutineRunning
	return []Value{BoolValue(yieldable)}, nil
}

func baseCoroutineWrap(globals *globalEnv, args []Value) ([]Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("coroutine.wrap: missing function")
	}
	closure, ok := args[0].scriptFunction()
	if !ok {
		return nil, fmt.Errorf("coroutine.wrap: argument is %s, want function", args[0].Kind())
	}
	coroutine := newVMCoroutine(globals, closure)
	wrapped := func(args []Value) ([]Value, error) {
		results, err := baseCoroutineResume(globals, append([]Value{UserDataValue(coroutine.userdata)}, args...))
		if err != nil {
			return nil, err
		}
		ok, _ := adjustedResultAt(results, 0).Bool()
		if !ok {
			return nil, fmt.Errorf("%s", valueToString(adjustedResultAt(results, 1)))
		}
		return results[1:], nil
	}
	return []Value{HostFuncValue(wrapped)}, nil
}

func activeThread(globals *globalEnv) *vmThread {
	if globals == nil {
		return nil
	}
	return globals.thread
}

func coroutineArg(name string, args []Value, index int) (*vmCoroutine, error) {
	if index >= len(args) {
		return nil, fmt.Errorf("%s: missing coroutine", name)
	}
	userdata, ok := args[index].UserData()
	if !ok {
		return nil, fmt.Errorf("%s: argument is %s, want thread", name, args[index].Kind())
	}
	coroutine, ok := userdata.Payload().(*vmCoroutine)
	if !ok || coroutine == nil {
		return nil, fmt.Errorf("%s: argument is userdata, want thread", name)
	}
	return coroutine, nil
}
