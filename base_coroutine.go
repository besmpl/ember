package ember

import (
	"context"
	"fmt"
)

type vmCoroutineStatus string

const (
	vmCoroutineSuspended vmCoroutineStatus = "suspended"
	vmCoroutineRunning   vmCoroutineStatus = "running"
	vmCoroutineNormal    vmCoroutineStatus = "normal"
	vmCoroutineDead      vmCoroutineStatus = "dead"
)

type vmCoroutine struct {
	status        vmCoroutineStatus
	owner         *runtimeOwner
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
	coroutine, err := newVMCoroutineChecked(globals, closure)
	if err != nil {
		return nil, err
	}
	return []Value{UserDataValue(coroutine.userdata)}, nil
}

func newVMCoroutine(globals *globalEnv, root *closure) *vmCoroutine {
	coroutine, _ := newVMCoroutineChecked(globals, root)
	return coroutine
}

func newVMCoroutineChecked(globals *globalEnv, root *closure) (*vmCoroutine, error) {
	controller := executionControllerForGlobals(globals)
	if controller != nil {
		if err := controller.chargeRuntimeObjects(2); err != nil {
			return nil, err
		}
	}
	owner := runtimeOwnerFromGlobals(globals)
	coroutineGlobals := globals
	if globals != nil && globals.pooled {
		coroutineGlobals = promotePooledGlobals(globals)
	}
	coroutine := &vmCoroutine{
		status: vmCoroutineSuspended,
		owner:  owner,
		thread: newVMThread(coroutineGlobals),
		root:   root,
	}
	if globals != nil {
		if globals.thread != nil {
			coroutine.thread.owner = globals.thread.owner
			coroutine.thread.inheritDebugConfig(globals.thread)
		}
	}
	coroutine.userdata = NewUserData(coroutine)
	if owner != nil {
		if err := owner.retainCoroutine(coroutine); err != nil {
			if controller != nil {
				controller.releaseRuntimeObjects(2)
			}
			coroutine.disposeFrames()
			return nil, err
		}
	}
	return coroutine, nil
}

// promotePooledGlobals transfers an active pooled environment to a stable
// heap object before a coroutine can retain it. The parent thread switches to
// that same object, preserving shared global values, slot versions, and host
// snapshots across parent/sibling coroutine execution. A pooled environment
// with no active thread is cloned as a defensive slow-path fallback.
func promotePooledGlobals(globals *globalEnv) *globalEnv {
	if globals == nil || !globals.pooled {
		return globals
	}
	if globals.thread == nil {
		promoted := *globals
		promoted.values = cloneGlobalValues(globals.values)
		promoted.host = cloneGlobalValues(globals.host)
		promoted.slots = cloneGlobalSlots(globals.slots)
		promoted.thread = nil
		promoted.pooled = false
		promoted.scope = invocationScope{}
		promoted.hasScope = false
		promoted.controller = nil
		return &promoted
	}
	parent := globals.thread
	promoted := *globals
	// Do not let pool reset clear backing maps that the retained coroutine will
	// keep using. The parent switches to this same promoted environment, so the
	// copies still preserve all parent/sibling sharing and slot versions.
	promoted.values = cloneGlobalValues(globals.values)
	promoted.host = cloneGlobalValues(globals.host)
	promoted.slots = cloneGlobalSlots(globals.slots)
	promoted.pooled = false
	promoted.thread = parent
	parent.globals = &promoted
	return &promoted
}

func cloneGlobalValues(values map[string]Value) map[string]Value {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]Value, len(values))
	for name, value := range values {
		clone[name] = value
	}
	return clone
}

func cloneGlobalSlots(slots []globalSlot) []globalSlot {
	if len(slots) == 0 {
		return nil
	}
	clone := make([]globalSlot, len(slots))
	copy(clone, slots)
	return clone
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
	callerOwner := runtimeOwnerFromGlobals(globals)
	if coroutine.owner != callerOwner {
		return []Value{BoolValue(false), StringValue("coroutine.resume: runtime owner mismatch")}, nil
	}
	if coroutine.owner != nil {
		if err := coroutine.owner.registerCoroutine(coroutine); err != nil {
			return []Value{BoolValue(false), StringValue("coroutine.resume: " + err.Error())}, nil
		}
		defer coroutine.owner.unregisterCoroutine(coroutine)
	}

	coroutine.status = vmCoroutineRunning
	coroutine.resumeArgs = append(coroutine.resumeArgs[:0], args[1:]...)
	parent, restoreParent := activeParentCoroutine(globals, coroutine)
	if parent != nil {
		parent.status = vmCoroutineNormal
		defer restoreParent()
	}
	parentThread := activeThread(globals)
	if parentThread != nil {
		coroutine.thread.ctx = parentThread.ctx
		coroutine.thread.controller = parentThread.controller
		coroutine.thread.scope = parentThread.scope
		coroutine.thread.hasScope = parentThread.hasScope
		coroutine.thread.inheritedScriptFrames = parentThread.inheritedScriptFrames
	}
	results, err := resumeCoroutine(coroutine, globals, args[1:])
	if yield, ok := err.(vmYieldRequest); ok {
		coroutine.status = vmCoroutineSuspended
		coroutine.yieldedValues = append(coroutine.yieldedValues[:0], yield.values...)
		coroutine.thread.coroutine = coroutine
		coroutine.suspended = coroutine.thread.suspendFrames()
		// A yielded coroutine must not retain the invocation's spent budget or
		// cancellation context. Public resume attaches the new invocation
		// controller before restoring these frames.
		coroutine.suspended.ctx = context.Background()
		coroutine.suspended.controller = nil
		coroutine.suspended.scope = invocationScope{}
		coroutine.suspended.hasScope = false
		coroutine.suspended.inheritedScriptFrames = nil
		coroutine.thread.ctx = context.Background()
		coroutine.thread.controller = nil
		coroutine.thread.scope = invocationScope{}
		coroutine.thread.hasScope = false
		coroutine.thread.inheritedScriptFrames = nil
		return coroutine.resumeResult(true, yield.values), nil
	}
	if err != nil {
		coroutine.status = vmCoroutineDead
		coroutine.err = err
		coroutine.disposeFrames()
		coroutine.releaseOwner()
		return []Value{BoolValue(false), StringValue(err.Error())}, nil
	}
	coroutine.status = vmCoroutineDead
	returned := coroutine.resumeResult(true, results)
	coroutine.disposeFrames()
	coroutine.releaseOwner()
	return returned, nil
}

func runtimeOwnerFromGlobals(globals *globalEnv) *runtimeOwner {
	if globals == nil {
		return nil
	}
	if globals.owner != nil {
		return globals.owner
	}
	if globals.thread != nil {
		return globals.thread.owner
	}
	return nil
}

func (coroutine *vmCoroutine) releaseOwner() {
	if coroutine != nil && coroutine.owner != nil {
		coroutine.owner.releaseCoroutine(coroutine)
	}
}

func (coroutine *vmCoroutine) disposeFrames() {
	if coroutine == nil {
		return
	}
	if len(coroutine.suspended.frames) != 0 || len(coroutine.suspended.frameRecords) != 0 || coroutine.suspended.owner != nil {
		coroutine.thread.resumeFrames(coroutine.suspended)
		coroutine.suspended = vmSuspendedFrames{}
	}
	coroutine.thread.dropFrames(0)
	coroutine.thread.closeAllOpenUpvalues()
	coroutine.thread.clearFrameRecords()
	coroutine.thread.clearRootClosureSlots()
	for _, frame := range coroutine.thread.frameSlots {
		if frame != nil {
			frame.resetForPool()
		}
	}
	coroutine.thread.frames = nil
	coroutine.thread.frameSlots = nil
	coroutine.thread.rootClosureSlots = nil
	coroutine.thread.stack = nil
	coroutine.thread.stackOwner = nil
	coroutine.thread.globals = nil
	coroutine.thread.scope = invocationScope{}
	coroutine.thread.hasScope = false
	coroutine.thread.inheritedScriptFrames = nil
	coroutine.thread.baseGlobals = globalEnv{}
	coroutine.thread.ctx = context.Background()
	coroutine.thread.controller = nil
	coroutine.thread.resumeCallDepthErr = nil
	coroutine.thread.coroutine = nil
	coroutine.thread.stringIntern = nil
	coroutine.thread.stringConcatIntern = nil
	coroutine.thread.functionInstances = nil
	coroutine.thread.functionInstanceSites = 0
	coroutine.thread.intrinsicGuards = nil
	coroutine.thread.debugHook = nil
	coroutine.root = nil
	coroutine.yieldedValues = nil
	clear(coroutine.yieldedInline[:])
	coroutine.resumeArgs = nil
	coroutine.resumeResults = nil
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
	if coroutine.thread.globals == nil {
		coroutine.thread.globals = globals
	}
	previousCoroutine := coroutine.thread.coroutine
	coroutine.thread.coroutine = coroutine
	defer func() {
		coroutine.thread.coroutine = previousCoroutine
	}()

	if len(coroutine.suspended.frames) > 0 || len(coroutine.suspended.frameRecords) > 0 {
		coroutine.thread.resumeFrames(coroutine.suspended)
		coroutine.thread.coroutine = coroutine
		coroutine.suspended = vmSuspendedFrames{}
		return coroutine.thread.continueSuspended(args)
	}
	return coroutine.thread.runWithUpvalues(coroutine.root.proto, args, coroutine.root.upvalues, coroutine.root.upvalueValues, coroutine.root.upvalueValueOK)
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
		coroutine.disposeFrames()
		coroutine.releaseOwner()
		return []Value{BoolValue(false), StringValue(err.Error())}, nil
	}
	coroutine.status = vmCoroutineDead
	coroutine.disposeFrames()
	coroutine.releaseOwner()
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
	coroutine, err := newVMCoroutineChecked(globals, closure)
	if err != nil {
		return nil, err
	}
	definingGlobals := coroutine.thread.globals
	wrapped := func(callerGlobals *globalEnv, args []Value) ([]Value, error) {
		if callerGlobals == nil {
			callerGlobals = definingGlobals
		}
		results, err := baseCoroutineResume(callerGlobals, append([]Value{UserDataValue(coroutine.userdata)}, args...))
		if err != nil {
			return nil, err
		}
		ok, _ := adjustedResultAt(results, 0).Bool()
		if !ok {
			return nil, fmt.Errorf("%s", valueToString(adjustedResultAt(results, 1)))
		}
		return results[1:], nil
	}
	return []Value{nativeFuncValue(wrapped)}, nil
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
