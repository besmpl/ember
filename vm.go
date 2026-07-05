package ember

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
)

// Run executes a compiled Ember prototype with Ember's base globals and returns
// its result values.
func Run(proto *Proto) ([]Value, error) {
	return RunWithGlobals(proto, nil)
}

// RunWithGlobals executes a compiled Ember prototype with Ember's base globals
// plus explicit global values available to the script. Explicit globals
// override base globals with the same name.
func RunWithGlobals(proto *Proto, globals map[string]Value) ([]Value, error) {
	if proto == nil {
		return nil, fmt.Errorf("run: nil prototype")
	}
	if proto.verifyErr != nil {
		return nil, fmt.Errorf("run: invalid prototype: %w", proto.verifyErr)
	}

	return executeProto(context.Background(), proto, runtimeGlobals(globals), executeOptions{
		maxInstructions: -1,
	})
}

type executeOptions struct {
	args            []Value
	upvalues        []*cell
	maxInstructions int
}

func executeProto(ctx context.Context, proto *Proto, globals *globalEnv, options executeOptions) ([]Value, error) {
	thread := newVMThreadWithContext(ctx, globals)
	thread.instructionBudget = options.maxInstructions
	return thread.run(proto, options.args, options.upvalues)
}

type vmThread struct {
	ctx                   context.Context
	globals               *globalEnv
	frames                []*vmFrame
	freeFrames            []*vmFrame
	instructionBudget     int
	coroutine             *vmCoroutine
	nonYieldableDepth     int
	debugHook             vmDebugHook
	debugCountInterval    int
	debugInstructionCount int
	debugLineHook         bool
	debugCallHook         bool
	debugReturnHook       bool

	maxFrames               int
	directFrameOpcodeCounts *directFrameOpcodeCounts
	directFrameArrayNext    *directFrameArrayNextCounts
}

type directFrameOpcodeCounts [256]uint64

type directFrameArrayNextCounts struct {
	genericInline uint64
}

func (counts *directFrameArrayNextCounts) addGenericInline() {
	if counts == nil {
		return
	}
	counts.genericInline++
}

type directFrameOpcodeCount struct {
	op    opcode
	count uint64
}

func (counts *directFrameOpcodeCounts) add(op opcode) {
	if counts == nil {
		return
	}
	counts[uint8(op)]++
}

func (counts *directFrameOpcodeCounts) count(op opcode) uint64 {
	if counts == nil {
		return 0
	}
	return counts[uint8(op)]
}

func (counts *directFrameOpcodeCounts) ranked() []directFrameOpcodeCount {
	if counts == nil {
		return nil
	}
	ranked := make([]directFrameOpcodeCount, 0)
	for index, count := range counts {
		if count == 0 {
			continue
		}
		ranked = append(ranked, directFrameOpcodeCount{op: opcode(index), count: count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count == ranked[j].count {
			return opcodeName(ranked[i].op) < opcodeName(ranked[j].op)
		}
		return ranked[i].count > ranked[j].count
	})
	return ranked
}

var vmFramePool = sync.Pool{
	New: func() any {
		return &vmFrame{}
	},
}

type vmFrame struct {
	proto           *Proto
	caller          *vmFrame
	registerBase    int
	registerCount   int
	directRegisters bool
	registers       []Value
	cells           []*cell
	upvalues        []*cell
	varargs         []Value
	pc              int
	debugLine       int
	openCallStart   int
	openCallResults []Value
	pendingCall     vmPendingCall
	hasPendingCall  bool
}

type vmSuspendedFrames struct {
	ctx                   context.Context
	globals               *globalEnv
	frames                []*vmFrame
	instructionBudget     int
	coroutine             *vmCoroutine
	nonYieldableDepth     int
	debugHook             vmDebugHook
	debugCountInterval    int
	debugInstructionCount int
	debugLineHook         bool
	debugCallHook         bool
	debugReturnHook       bool
	maxFrames             int
}

type vmDebugEventKind int

const (
	vmDebugEventCount vmDebugEventKind = iota
	vmDebugEventLine
	vmDebugEventCall
	vmDebugEventReturn
)

type vmDebugEvent struct {
	kind  vmDebugEventKind
	frame *vmFrame
	pc    int
	line  int
}

type vmDebugHook func(globals *globalEnv, event vmDebugEvent) error

type vmPendingCall struct {
	destination vmResultDestination
	protected   *vmProtectedCall
	host        *vmPendingHostCall
}

type vmResultDestination struct {
	register int
	count    int
}

type vmProtectedCall struct {
	handler    Value
	hasHandler bool
}

type vmPendingHostCall struct {
	continuation vmHostContinuation
}

type vmHostContinuation func(globals *globalEnv, args []Value) vmHostCallResult

type vmHostCallResult struct {
	values    []Value
	err       error
	yield     *vmHostYield
	interrupt bool
}

type vmHostYield struct {
	values       []Value
	continuation vmHostContinuation
}

type vmScriptCall struct {
	closure *closure
	args    []Value
}

type vmCallState int

const (
	vmCallStateReturned vmCallState = iota
	vmCallStateScriptCall
	vmCallStateYielded
	vmCallStateProtectedReturn
	vmCallStateHostInterrupt
)

type vmFrameResult struct {
	state             vmCallState
	results           []Value
	inlineResults     [2]Value
	inlineResultCount int
	scriptCall        vmScriptCall
}

type vmYieldRequest struct {
	values    []Value
	protected *vmProtectedCall
	host      *vmPendingHostCall
}

func vmReturnedValues(values []Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateReturned, results: values}
}

func vmReturnedValue(value Value) vmFrameResult {
	return vmFrameResult{
		state:             vmCallStateReturned,
		inlineResults:     [2]Value{value},
		inlineResultCount: 1,
	}
}

func (result vmFrameResult) values() []Value {
	if result.inlineResultCount == 0 {
		return result.results
	}
	values := make([]Value, result.inlineResultCount)
	copy(values, result.inlineResults[:result.inlineResultCount])
	return values
}

func (request vmYieldRequest) Error() string {
	return "coroutine yield"
}

type vmHostInterrupt struct{}

func (interrupt vmHostInterrupt) Error() string {
	return "run: instruction budget exhausted"
}

func newVMThread(globals *globalEnv) vmThread {
	return newVMThreadWithContext(context.Background(), globals)
}

func newVMThreadWithContext(ctx context.Context, globals *globalEnv) vmThread {
	if ctx == nil {
		ctx = context.Background()
	}
	return vmThread{
		ctx:               ctx,
		globals:           globals,
		instructionBudget: -1,
	}
}

func (thread *vmThread) inheritDebugConfig(parent *vmThread) {
	if thread == nil || parent == nil {
		return
	}
	thread.debugHook = parent.debugHook
	thread.debugCountInterval = parent.debugCountInterval
	thread.debugInstructionCount = parent.debugInstructionCount
	thread.debugLineHook = parent.debugLineHook
	thread.debugCallHook = parent.debugCallHook
	thread.debugReturnHook = parent.debugReturnHook
}

func (thread *vmThread) inheritRuntimeState(parent *vmThread) {
	if thread == nil || parent == nil {
		return
	}
	thread.ctx = parent.ctx
	thread.instructionBudget = parent.instructionBudget
	thread.inheritDebugConfig(parent)
}

func (thread *vmThread) run(proto *Proto, args []Value, upvalues []*cell) ([]Value, error) {
	restore := thread.activate()
	defer restore()
	defer thread.releaseFreeFramesToPool()

	return thread.runScript(proto, args, upvalues)
}

func (thread *vmThread) activate() func() {
	previousThread := thread.globals.thread
	thread.globals.thread = thread
	return func() {
		thread.globals.thread = previousThread
	}
}

func (thread *vmThread) suspendFrames() vmSuspendedFrames {
	suspended := vmSuspendedFrames{
		ctx:                   thread.ctx,
		globals:               thread.globals,
		frames:                thread.frames,
		instructionBudget:     thread.instructionBudget,
		coroutine:             thread.coroutine,
		nonYieldableDepth:     thread.nonYieldableDepth,
		debugHook:             thread.debugHook,
		debugCountInterval:    thread.debugCountInterval,
		debugInstructionCount: thread.debugInstructionCount,
		debugLineHook:         thread.debugLineHook,
		debugCallHook:         thread.debugCallHook,
		debugReturnHook:       thread.debugReturnHook,
		maxFrames:             thread.maxFrames,
	}
	thread.frames = nil
	return suspended
}

func (thread *vmThread) resumeFrames(suspended vmSuspendedFrames) {
	thread.ctx = suspended.ctx
	thread.globals = suspended.globals
	thread.frames = suspended.frames
	thread.instructionBudget = suspended.instructionBudget
	thread.coroutine = suspended.coroutine
	thread.nonYieldableDepth = suspended.nonYieldableDepth
	thread.debugHook = suspended.debugHook
	thread.debugCountInterval = suspended.debugCountInterval
	thread.debugInstructionCount = suspended.debugInstructionCount
	thread.debugLineHook = suspended.debugLineHook
	thread.debugCallHook = suspended.debugCallHook
	thread.debugReturnHook = suspended.debugReturnHook
	thread.maxFrames = suspended.maxFrames
}

func (thread *vmThread) enterNonYieldable() func() {
	thread.nonYieldableDepth++
	return func() {
		thread.nonYieldableDepth--
	}
}

func (thread *vmThread) isYieldable() bool {
	return thread != nil && thread.nonYieldableDepth == 0
}

func (thread *vmThread) continueSuspended(args []Value) ([]Value, error) {
	restore := thread.activate()
	defer restore()
	defer thread.releaseFreeFramesToPool()

	if len(thread.frames) == 0 {
		return nil, fmt.Errorf("coroutine.resume: missing suspended frame")
	}
	frame := thread.frames[len(thread.frames)-1]
	if !frame.hasPendingCall {
		return nil, fmt.Errorf("coroutine.resume: suspended frame has no yield destination")
	}
	if frame.pendingCall.host != nil {
		return thread.continueHostCall(frame, args)
	}
	frame.applyCallResults(args)
	return thread.runUntilDepth(0)
}

func (thread *vmThread) continueHostCall(frame *vmFrame, args []Value) ([]Value, error) {
	call := frame.pendingCall
	if call.host.continuation == nil {
		return nil, fmt.Errorf("coroutine.resume: suspended host call has no continuation")
	}
	results, err := finishHostCallResult(call.host.continuation(thread.globals, args))
	if err != nil {
		if yield, ok := err.(vmYieldRequest); ok {
			frame.pendingCall = vmPendingCall{
				destination: call.destination,
				protected:   call.protected,
				host:        yield.host,
			}
			frame.hasPendingCall = true
			return nil, vmYieldRequest{
				values:    yield.values,
				protected: call.protected,
				host:      yield.host,
			}
		}
		if thread.recoverProtectedError(err) {
			return thread.runUntilDepth(0)
		}
		return nil, err
	}
	frame.applyCallResults(results)
	return thread.runUntilDepth(0)
}

func (thread *vmThread) runScript(proto *Proto, args []Value, upvalues []*cell) ([]Value, error) {
	baseDepth := len(thread.frames)
	frame := thread.newFrame(proto, args, upvalues)
	thread.pushFrame(frame)
	if thread.debugHook != nil && thread.debugCallHook {
		if err := thread.runDebugCallHook(frame); err != nil {
			if !isVMYieldRequest(err) {
				thread.frames = nil
			}
			return nil, err
		}
	}
	results, err := thread.runUntilDepth(baseDepth)
	if err != nil && !isVMYieldRequest(err) {
		thread.frames = nil
	}
	return results, err
}

func (thread *vmThread) runScriptProtected(proto *Proto, args []Value, upvalues []*cell) ([]Value, error) {
	baseDepth := len(thread.frames)
	frame := thread.newFrame(proto, args, upvalues)
	thread.pushFrame(frame)
	if thread.debugHook != nil && thread.debugCallHook {
		if err := thread.runDebugCallHook(frame); err != nil {
			if !isVMYieldRequest(err) {
				thread.frames = thread.frames[:baseDepth]
			}
			return nil, err
		}
	}
	results, err := thread.runUntilDepth(baseDepth)
	if err != nil && !isVMYieldRequest(err) {
		thread.frames = thread.frames[:baseDepth]
	}
	return results, err
}

func isVMYieldRequest(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(vmYieldRequest)
	return ok
}

func isVMHostInterrupt(err error) bool {
	if err == nil {
		return false
	}
	var interrupt vmHostInterrupt
	return errors.As(err, &interrupt)
}

func (thread *vmThread) runUntilDepth(baseDepth int) ([]Value, error) {
	result, err := thread.runUntilDepthResult(baseDepth)
	if err != nil {
		return nil, err
	}
	return result.values(), nil
}

func (thread *vmThread) runUntilDepthResult(baseDepth int) (vmFrameResult, error) {
	for len(thread.frames) > 0 {
		frame := thread.frames[len(thread.frames)-1]
		result, err := thread.runFrame(frame)
		if err != nil {
			if thread.recoverProtectedError(err) {
				continue
			}
			return vmFrameResult{}, err
		}
		if result.state == vmCallStateScriptCall {
			call := result.scriptCall
			frame := thread.newFrame(call.closure.proto, call.args, call.closure.upvalues)
			thread.pushFrame(frame)
			if thread.debugHook != nil && thread.debugCallHook {
				if err := thread.runDebugCallHook(frame); err != nil {
					if thread.recoverProtectedError(err) {
						continue
					}
					return vmFrameResult{}, err
				}
			}
			continue
		}
		if result.state == vmCallStateYielded {
			return vmFrameResult{}, vmYieldRequest{values: result.values()}
		}
		if result.state == vmCallStateHostInterrupt {
			return vmFrameResult{}, vmHostInterrupt{}
		}

		if thread.debugHook != nil && thread.debugReturnHook {
			if err := thread.runDebugReturnHook(frame); err != nil {
				if thread.recoverProtectedError(err) {
					continue
				}
				return vmFrameResult{}, err
			}
		}
		thread.popFrame()
		if len(thread.frames) == baseDepth {
			return result, nil
		}
		caller := thread.frames[len(thread.frames)-1]
		if !caller.hasPendingCall {
			return result, nil
		}
		caller.applyFrameCallResults(result)
	}
	return vmFrameResult{}, fmt.Errorf("run: empty VM call stack")
}

func (thread *vmThread) runInlineScriptCall(closure *closure, args []Value) (vmFrameResult, error) {
	baseDepth := len(thread.frames)
	calleeFrame := thread.newFrame(closure.proto, args, closure.upvalues)
	thread.pushFrame(calleeFrame)
	if thread.debugHook != nil && thread.debugCallHook {
		if err := thread.runDebugCallHook(calleeFrame); err != nil {
			if thread.recoverProtectedError(err) {
				return thread.runUntilDepthResult(baseDepth)
			}
			return vmFrameResult{}, err
		}
	}
	result, err := thread.runFrame(calleeFrame)
	if err != nil {
		if thread.recoverProtectedError(err) {
			return thread.runUntilDepthResult(baseDepth)
		}
		return vmFrameResult{}, err
	}
	if result.state == vmCallStateScriptCall {
		call := result.scriptCall
		frame := thread.newFrame(call.closure.proto, call.args, call.closure.upvalues)
		thread.pushFrame(frame)
		if thread.debugHook != nil && thread.debugCallHook {
			if err := thread.runDebugCallHook(frame); err != nil {
				if thread.recoverProtectedError(err) {
					return thread.runUntilDepthResult(baseDepth)
				}
				return vmFrameResult{}, err
			}
		}
		return thread.runUntilDepthResult(baseDepth)
	}
	if result.state == vmCallStateYielded {
		return vmFrameResult{}, vmYieldRequest{values: result.values()}
	}
	if result.state == vmCallStateHostInterrupt {
		return vmFrameResult{}, vmHostInterrupt{}
	}
	if thread.debugHook != nil && thread.debugReturnHook {
		if err := thread.runDebugReturnHook(calleeFrame); err != nil {
			if thread.recoverProtectedError(err) {
				return thread.runUntilDepthResult(baseDepth)
			}
			return vmFrameResult{}, err
		}
	}
	thread.popFrame()
	return result, nil
}

func (thread *vmThread) runInlineScriptCallOneNoHook(closure *closure, args []Value) (Value, error) {
	if thread.debugHook != nil {
		result, err := thread.runInlineScriptCall(closure, args)
		if err != nil {
			return NilValue(), err
		}
		if result.inlineResultCount > 0 {
			return result.inlineResults[0], nil
		}
		if len(result.results) > 0 {
			return result.results[0], nil
		}
		return NilValue(), nil
	}

	baseDepth := len(thread.frames)
	calleeFrame := thread.newFrame(closure.proto, args, closure.upvalues)
	thread.pushFrame(calleeFrame)
	result, err := thread.runFrame(calleeFrame)
	if err != nil {
		if thread.recoverProtectedError(err) {
			result, err = thread.runUntilDepthResult(baseDepth)
			if err != nil {
				return NilValue(), err
			}
			if result.inlineResultCount > 0 {
				return result.inlineResults[0], nil
			}
			if len(result.results) > 0 {
				return result.results[0], nil
			}
			return NilValue(), nil
		}
		return NilValue(), err
	}
	if result.state == vmCallStateScriptCall {
		call := result.scriptCall
		frame := thread.newFrame(call.closure.proto, call.args, call.closure.upvalues)
		thread.pushFrame(frame)
		result, err = thread.runUntilDepthResult(baseDepth)
		if err != nil {
			return NilValue(), err
		}
		if result.inlineResultCount > 0 {
			return result.inlineResults[0], nil
		}
		if len(result.results) > 0 {
			return result.results[0], nil
		}
		return NilValue(), nil
	}
	if result.state == vmCallStateYielded {
		return NilValue(), vmYieldRequest{values: result.values()}
	}
	if result.state == vmCallStateHostInterrupt {
		return NilValue(), vmHostInterrupt{}
	}
	thread.popFrame()
	if result.inlineResultCount > 0 {
		return result.inlineResults[0], nil
	}
	if len(result.results) > 0 {
		return result.results[0], nil
	}
	return NilValue(), nil
}

func (thread *vmThread) recoverProtectedError(err error) bool {
	if isVMYieldRequest(err) || isVMHostInterrupt(err) {
		return false
	}
	for index := len(thread.frames) - 1; index >= 0; index-- {
		frame := thread.frames[index]
		if !frame.hasPendingCall || frame.pendingCall.protected == nil {
			continue
		}
		protected := frame.pendingCall.protected
		thread.frames = thread.frames[:index+1]
		results := []Value{StringValue(err.Error())}
		if protected.hasHandler {
			restore := thread.enterNonYieldable()
			handled, handlerErr := callValue(protected.handler, thread.globals, results)
			restore()
			if handlerErr != nil {
				results = []Value{StringValue(handlerErr.Error())}
			} else {
				results = handled
			}
		}
		frame.applyProtectedErrorResults(append([]Value{BoolValue(false)}, results...))
		return true
	}
	return false
}

func (thread *vmThread) pushFrame(frame *vmFrame) {
	if len(thread.frames) > 0 {
		frame.caller = thread.frames[len(thread.frames)-1]
	}
	thread.frames = append(thread.frames, frame)
	if len(thread.frames) > thread.maxFrames {
		thread.maxFrames = len(thread.frames)
	}
}

func (thread *vmThread) popFrame() {
	frame := thread.frames[len(thread.frames)-1]
	thread.frames = thread.frames[:len(thread.frames)-1]
	thread.releaseFrame(frame)
}

func newVMFrame(proto *Proto, args []Value, upvalues []*cell) *vmFrame {
	frame := &vmFrame{}
	frame.reset(proto, args, upvalues)
	return frame
}

func (thread *vmThread) newFrame(proto *Proto, args []Value, upvalues []*cell) *vmFrame {
	if frame := thread.takeFreeFrame(proto); frame != nil {
		frame.reset(proto, args, upvalues)
		return frame
	}
	frame := vmFramePool.Get().(*vmFrame)
	frame.reset(proto, args, upvalues)
	return frame
}

func (thread *vmThread) takeFreeFrame(proto *Proto) *vmFrame {
	if len(proto.capturedLocals) != 0 {
		return nil
	}
	last := len(thread.freeFrames) - 1
	if last >= 0 {
		frame := thread.freeFrames[last]
		if cap(frame.registers) >= proto.registers {
			thread.freeFrames = thread.freeFrames[:last]
			return frame
		}
	}
	for i := len(thread.freeFrames) - 1; i >= 0; i-- {
		frame := thread.freeFrames[i]
		if cap(frame.registers) < proto.registers {
			continue
		}
		thread.freeFrames = append(thread.freeFrames[:i], thread.freeFrames[i+1:]...)
		return frame
	}
	return nil
}

func (thread *vmThread) releaseFrame(frame *vmFrame) {
	if frame == nil || len(frame.cells) != 0 {
		return
	}
	frame.resetForReuse()
	thread.freeFrames = append(thread.freeFrames, frame)
}

func (thread *vmThread) releaseFreeFramesToPool() {
	for _, frame := range thread.freeFrames {
		frame.resetForPool()
		vmFramePool.Put(frame)
	}
	thread.freeFrames = thread.freeFrames[:0]
}

func (frame *vmFrame) reset(proto *Proto, args []Value, upvalues []*cell) {
	var registers []Value
	if cap(frame.registers) >= proto.registers {
		registers = frame.registers[:proto.registers]
		for _, register := range proto.entryNilRegisters {
			registers[register] = NilValue()
		}
	} else {
		registers = make([]Value, proto.registers)
	}

	varargs := []Value(nil)
	if proto.variadic && len(args) > proto.params {
		varargs = args[proto.params:]
	}

	for i := 0; i < proto.params && i < len(registers); i++ {
		if i < len(args) {
			registers[i] = args[i]
		} else {
			registers[i] = NilValue()
		}
	}

	var cells []*cell
	if len(proto.capturedLocals) != 0 {
		if cap(frame.cells) >= proto.registers {
			cells = frame.cells[:proto.registers]
			for i := range cells {
				cells[i] = nil
			}
		} else {
			cells = make([]*cell, proto.registers)
		}
		for index, captured := range proto.capturedLocals {
			if captured {
				cells[index] = &cell{value: registers[index]}
			}
		}
	}

	frame.proto = proto
	frame.caller = nil
	frame.registerBase = 0
	frame.registerCount = len(registers)
	frame.directRegisters = proto.directRegisters
	frame.registers = registers
	frame.cells = cells
	frame.upvalues = upvalues
	frame.varargs = varargs
	frame.pc = 0
	frame.debugLine = -1
	frame.openCallStart = -1
	frame.openCallResults = nil
	frame.clearPendingCall()
}

func (frame *vmFrame) resetForReuse() {
	frame.proto = nil
	frame.caller = nil
	frame.registerBase = 0
	frame.registerCount = 0
	frame.directRegisters = false
	frame.upvalues = nil
	frame.varargs = frame.varargs[:0]
	frame.pc = 0
	frame.debugLine = -1
	frame.openCallStart = -1
	frame.openCallResults = nil
	frame.clearPendingCall()
}

func (frame *vmFrame) resetForPool() {
	clear(frame.registers)
	clear(frame.cells)
	if cap(frame.openCallResults) > 0 {
		clear(frame.openCallResults[:cap(frame.openCallResults)])
	}
	frame.resetForReuse()
}

func capturedLocalRegisters(proto *Proto) []bool {
	captured := make([]bool, proto.registers)
	hasCaptured := false
	for _, child := range proto.prototypes {
		for _, desc := range child.upvalues {
			if desc.local {
				if desc.index < 0 || desc.index >= proto.registers {
					continue
				}
				captured[desc.index] = true
				hasCaptured = true
			}
		}
	}
	if !hasCaptured {
		return nil
	}
	return captured
}

func (frame *vmFrame) register(index int) Value {
	if frame.directRegisters {
		return frame.registers[index]
	}
	if index < len(frame.cells) && frame.cells[index] != nil {
		cell := frame.cells[index]
		return cell.value
	}
	return frame.registers[index]
}

func (frame *vmFrame) setRegister(index int, value Value) {
	frame.registers[index] = value
	if frame.directRegisters {
		return
	}
	if index < len(frame.cells) && frame.cells[index] != nil {
		cell := frame.cells[index]
		cell.value = value
	}
}

func (frame *vmFrame) registerCell(index int) *cell {
	if len(frame.cells) < len(frame.registers) {
		cells := make([]*cell, len(frame.registers))
		copy(cells, frame.cells)
		frame.cells = cells
	}
	if frame.cells[index] == nil {
		frame.cells[index] = &cell{value: frame.registers[index]}
	}
	return frame.cells[index]
}

func (frame *vmFrame) applyCallResults(results []Value) {
	call := frame.pendingCall
	frame.clearPendingCall()
	if call.protected != nil {
		results = append([]Value{BoolValue(true)}, results...)
	}
	frame.applyResultDestination(call.destination, results)
}

func (frame *vmFrame) applyFrameCallResults(result vmFrameResult) {
	call := frame.pendingCall
	frame.clearPendingCall()
	if call.protected != nil {
		frame.applyResultDestination(call.destination, append([]Value{BoolValue(true)}, result.values()...))
		return
	}
	if result.inlineResultCount == 0 {
		frame.applyResultDestination(call.destination, result.results)
		return
	}
	frame.applyInlineResultDestination(call.destination, result.inlineResults, result.inlineResultCount)
}

func (frame *vmFrame) applySingleFrameCallResult(register int, result vmFrameResult) {
	frame.clearPendingCall()
	frame.openCallStart = -1
	frame.openCallResults = nil
	if result.inlineResultCount > 0 {
		frame.setRegister(register, result.inlineResults[0])
		return
	}
	if len(result.results) > 0 {
		frame.setRegister(register, result.results[0])
		return
	}
	frame.setRegister(register, NilValue())
}

func (frame *vmFrame) applyFrameResultDestination(destination vmResultDestination, result vmFrameResult) {
	if result.inlineResultCount == 0 {
		frame.applyResultDestination(destination, result.results)
		return
	}
	frame.applyInlineResultDestination(destination, result.inlineResults, result.inlineResultCount)
}

func (frame *vmFrame) applySingleFrameResult(register int, result vmFrameResult) {
	frame.openCallStart = -1
	frame.openCallResults = nil
	if frame.directRegisters {
		if result.inlineResultCount > 0 {
			frame.registers[register] = result.inlineResults[0]
			return
		}
		if len(result.results) > 0 {
			frame.registers[register] = result.results[0]
			return
		}
		frame.registers[register] = NilValue()
		return
	}
	if result.inlineResultCount > 0 {
		frame.setRegister(register, result.inlineResults[0])
		return
	}
	if len(result.results) > 0 {
		frame.setRegister(register, result.results[0])
		return
	}
	frame.setRegister(register, NilValue())
}

func (frame *vmFrame) applyProtectedErrorResults(results []Value) {
	call := frame.pendingCall
	frame.clearPendingCall()
	frame.applyResultDestination(call.destination, results)
}

func (frame *vmFrame) clearPendingCall() {
	frame.pendingCall = vmPendingCall{}
	frame.hasPendingCall = false
}

func (frame *vmFrame) applyResultDestination(destination vmResultDestination, results []Value) {
	resultCount := destination.count
	if resultCount < 0 {
		frame.openCallStart = destination.register
		frame.openCallResults = adjustedCallResults(results)
		if len(frame.openCallResults) == 0 {
			frame.setRegister(destination.register, NilValue())
		} else {
			frame.setRegister(destination.register, frame.openCallResults[0])
		}
		return
	}

	frame.openCallStart = -1
	frame.openCallResults = nil
	for i := 0; i < resultCount; i++ {
		if i >= len(results) {
			frame.setRegister(destination.register+i, NilValue())
		} else {
			frame.setRegister(destination.register+i, results[i])
		}
	}
	if len(results) == 0 && resultCount == 1 {
		frame.setRegister(destination.register, NilValue())
	}
}

func (frame *vmFrame) applyInlineResultDestination(destination vmResultDestination, results [2]Value, count int) {
	resultCount := destination.count
	if resultCount < 0 {
		frame.openCallStart = destination.register
		frame.openCallResults = frame.openCallResults[:0]
		frame.openCallResults = append(frame.openCallResults, results[:count]...)
		if len(frame.openCallResults) == 0 {
			frame.setRegister(destination.register, NilValue())
		} else {
			frame.setRegister(destination.register, frame.openCallResults[0])
		}
		return
	}

	frame.openCallStart = -1
	frame.openCallResults = nil
	for i := 0; i < resultCount; i++ {
		if i >= count {
			frame.setRegister(destination.register+i, NilValue())
		} else {
			frame.setRegister(destination.register+i, results[i])
		}
	}
	if count == 0 && resultCount == 1 {
		frame.setRegister(destination.register, NilValue())
	}
}

func (thread *vmThread) runFrame(frame *vmFrame) (vmFrameResult, error) {
	if frame.proto.directFrameDispatch && thread.canRunDirectFrame() {
		if result, complete, err := thread.runDirectFrame(frame); complete || err != nil {
			return result, err
		}
	}
	return thread.runGenericFrame(frame)
}

func (thread *vmThread) canRunDirectFrame() bool {
	return thread.debugHook == nil && thread.instructionBudget < 0
}

func directFrameStringField(value Value, key string) (Value, bool, error) {
	if value.kind != TableKind || value.table == nil {
		return NilValue(), false, fmt.Errorf("get field target is %s, want table", value.Kind())
	}
	table := value.table
	if field, ok := table.rawStringField(key); ok {
		return field, true, nil
	}
	if table.metatable != nil {
		return NilValue(), false, nil
	}
	return NilValue(), true, nil
}

func directFrameRowStringField(value Value, key string, slotIndex int) (Value, bool, error) {
	if value.kind != TableKind || value.table == nil {
		return NilValue(), false, fmt.Errorf("get field target is %s, want table", value.Kind())
	}
	table := value.table
	if table.metatable == nil && slotIndex >= 0 {
		if table.stringFieldMap == nil && slotIndex < len(table.stringFields) && table.stringFields[slotIndex].key == key {
			return table.stringFields[slotIndex].value, true, nil
		}
	}
	return directFrameStringField(value, key)
}

func (thread *vmThread) runDirectFrame(frame *vmFrame) (vmFrameResult, bool, error) {
	proto := frame.proto
	registers := frame.registers

	for frame.pc < len(proto.code) {
		ins := proto.code[frame.pc]
		thread.directFrameOpcodeCounts.add(ins.op)

		switch ins.op {
		case opLoadConst:
			registers[ins.a] = proto.constants[ins.b]

		case opNewTable:
			registers[ins.a] = TableValue(newTableWithCapacity(ins.b, ins.c))

		case opMove:
			registers[ins.a] = registers[ins.b]

		case opSetField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: set field target is %s, want table", base.Kind())
			}
			table := base.table
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			if proto.constantKeyOK[ins.b] {
				if err := table.rawSetKey(proto.constantKeys[ins.b], registers[ins.c]); err != nil {
					return vmFrameResult{}, true, fmt.Errorf("run: set field failed: %w", err)
				}
				break
			}
			if err := table.rawSet(proto.constants[ins.b], registers[ins.c]); err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSetStringField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: set field target is %s, want table", base.Kind())
			}
			table := base.table
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			table.setRawStringField(proto.constantKeys[ins.b].str, registers[ins.c])

		case opSetRowStringField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: set field target is %s, want table", base.Kind())
			}
			table := base.table
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			key := proto.constantKeys[ins.b].str
			slot := tableStringFieldSlot{index: ins.d, version: table.stringVersion}
			if !table.setRawStringFieldAtSlot(slot, key, registers[ins.c]) {
				table.setRawStringField(key, registers[ins.c])
			}

		case opGetField:
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			var value Value
			var err error
			if proto.constantKeyOK[ins.c] {
				value, err = table.rawGetKey(proto.constantKeys[ins.c])
			} else {
				value, err = table.rawGet(proto.constants[ins.c])
			}
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field failed: %w", err)
			}
			registers[ins.a] = value

		case opGetStringField:
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			if value, ok := table.rawStringField(proto.constantKeys[ins.c].str); ok {
				registers[ins.a] = value
				break
			}
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NilValue()

		case opGetRowStringField:
			value, ok, err := directFrameRowStringField(registers[ins.b], proto.constantKeys[ins.c].str, ins.d)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field failed: %w", err)
			}
			if !ok {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = value

		case opAddStringField, opSubStringField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			right := registers[ins.c]
			if right.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			key := proto.constantKeys[ins.b].str
			left, ok := table.rawStringField(key)
			if !ok || left.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			next := left.number + right.number
			if ins.op == opSubStringField {
				next = left.number - right.number
			}
			table.setRawStringField(key, NumberValue(next))

		case opSubAddStringField:
			desc := proto.rowFieldSubAddOps[ins.b]
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			subtract := registers[ins.c]
			if subtract.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			targetKey := proto.constantKeys[desc.target].str
			addKey := proto.constantKeys[desc.add].str
			var left Value
			var add Value
			var targetSlot tableStringFieldSlot
			if desc.targetSlot >= 0 && desc.addSlot >= 0 {
				version := table.stringVersion
				targetSlot = tableStringFieldSlot{index: desc.targetSlot, version: version}
				addSlot := tableStringFieldSlot{index: desc.addSlot, version: version}
				var leftOK bool
				var addOK bool
				left, leftOK = table.rawStringFieldAtSlot(targetSlot, targetKey)
				add, addOK = table.rawStringFieldAtSlot(addSlot, addKey)
				if !leftOK || !addOK {
					return vmFrameResult{}, false, nil
				}
			} else {
				var ok bool
				targetSlot, ok = table.rawStringFieldSlot(targetKey)
				if !ok {
					return vmFrameResult{}, false, nil
				}
				left, ok = table.rawStringFieldAtSlot(targetSlot, targetKey)
				if !ok {
					return vmFrameResult{}, false, nil
				}
				add, ok = table.rawStringField(addKey)
				if !ok {
					return vmFrameResult{}, false, nil
				}
			}
			if left.kind != NumberKind || add.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			if !table.setRawStringFieldAtSlot(targetSlot, targetKey, NumberValue(left.number-subtract.number+add.number)) {
				return vmFrameResult{}, false, nil
			}

		case opSetIndex:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: set index target is %s, want table", base.Kind())
			}
			table := base.table
			if table.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			if err := table.rawSet(registers[ins.b], registers[ins.c]); err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: set index failed: %w", err)
			}

		case opClosure:
			captured := captureUpvalues(proto.prototypes[ins.b], frame)
			registers[ins.a] = functionValue(proto.prototypes[ins.b], captured)

		case opPrepareIter:
			generator, state, control, ok, err := prepareIterator(registers[ins.a], thread.globals)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: prepare iterator failed: %w", err)
			}
			if ok {
				registers[ins.a] = generator
				registers[ins.b] = state
				registers[ins.c] = control
			}

		case opAdd:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number + right.number)

		case opSub:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number - right.number)

		case opMul:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number * right.number)

		case opDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number / right.number)

		case opMod:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number - math.Floor(left.number/right.number)*right.number)

		case opIDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(math.Floor(left.number / right.number))

		case opAddK:
			left := registers[ins.b]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number + proto.constantNumbers[ins.c])

		case opSubK:
			left := registers[ins.b]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number - proto.constantNumbers[ins.c])

		case opMulK:
			left := registers[ins.b]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number * proto.constantNumbers[ins.c])

		case opDivK:
			left := registers[ins.b]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(left.number / proto.constantNumbers[ins.c])

		case opModK:
			left := registers[ins.b]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			right := proto.constantNumbers[ins.c]
			registers[ins.a] = NumberValue(left.number - math.Floor(left.number/right)*right)

		case opIDivK:
			left := registers[ins.b]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = NumberValue(math.Floor(left.number / proto.constantNumbers[ins.c]))

		case opAddNumericModK:
			desc := proto.numericAddModOps[ins.c]
			if !proto.constantNumberOK[desc.mul] ||
				!proto.constantNumberOK[desc.idiv] ||
				!proto.constantNumberOK[desc.mod] {
				return vmFrameResult{}, false, nil
			}
			left := registers[ins.a]
			source := registers[ins.b]
			if left.kind != NumberKind || source.kind != NumberKind {
				return vmFrameResult{}, false, nil
			}
			mul := source.number * proto.constantNumbers[desc.mul]
			idiv := math.Floor(source.number / proto.constantNumbers[desc.idiv])
			beforeMod := mul - idiv
			mod := proto.constantNumbers[desc.mod]
			registers[ins.a] = NumberValue(left.number + beforeMod - math.Floor(beforeMod/mod)*mod)

		case opEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind == TableKind || right.kind == TableKind || left.kind == UserDataKind || right.kind == UserDataKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = BoolValue(valuesEqual(left, right))

		case opNotEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind == TableKind || right.kind == TableKind || left.kind == UserDataKind || right.kind == UserDataKind {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = BoolValue(!valuesEqual(left, right))

		case opLess:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = BoolValue(left.number < right.number)

		case opLessEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = BoolValue(left.number <= right.number)

		case opGreater:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = BoolValue(left.number > right.number)

		case opGreaterEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return vmFrameResult{}, false, nil
			}
			registers[ins.a] = BoolValue(left.number >= right.number)

		case opNumericForCheck:
			loopValue := registers[ins.a]
			limitValue := registers[ins.b]
			stepValue := registers[ins.c]
			if loopValue.kind != NumberKind {
				return vmFrameResult{}, true, fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind())
			}
			if limitValue.kind != NumberKind {
				return vmFrameResult{}, true, fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind())
			}
			if stepValue.kind != NumberKind {
				return vmFrameResult{}, true, fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind())
			}
			if math.IsNaN(loopValue.number) || math.IsNaN(limitValue.number) || math.IsNaN(stepValue.number) {
				return vmFrameResult{}, true, fmt.Errorf("run: numeric for operand is NaN")
			}
			if stepValue.number > 0 {
				if loopValue.number > limitValue.number {
					frame.pc = ins.d
					continue
				}
				break
			}
			if loopValue.number < limitValue.number {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotEqualK:
			left := registers[ins.a]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.b] {
				return vmFrameResult{}, false, nil
			}
			if left.number != proto.constantNumbers[ins.b] {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLessK:
			left := registers[ins.a]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.b] {
				return vmFrameResult{}, false, nil
			}
			right := proto.constantNumbers[ins.b]
			if !math.IsNaN(left.number) && !math.IsNaN(right) && left.number >= right {
				frame.pc = ins.d
				continue
			}

		case opJumpIfModKNotEqualK:
			left := registers[ins.a]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.b] || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			modRight := proto.constantNumbers[ins.b]
			want := proto.constantNumbers[ins.c]
			got := left.number - math.Floor(left.number/modRight)*modRight
			if got != want {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotEqualK:
			left, ok, err := directFrameStringField(registers[ins.a], proto.constantKeys[ins.b].str)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field failed: %w", err)
			}
			if !ok {
				return vmFrameResult{}, false, nil
			}
			right := proto.constants[ins.c]
			if left.kind == TableKind || left.kind == UserDataKind || right.kind == TableKind || right.kind == UserDataKind {
				return vmFrameResult{}, false, nil
			}
			if !valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotEqualK:
			desc := proto.rowFieldEqualOps[ins.b]
			left, ok, err := directFrameRowStringField(registers[ins.a], proto.constantKeys[desc.field].str, desc.slot)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field failed: %w", err)
			}
			if !ok {
				return vmFrameResult{}, false, nil
			}
			right := proto.constants[desc.value]
			if left.kind == TableKind || left.kind == UserDataKind || right.kind == TableKind || right.kind == UserDataKind {
				return vmFrameResult{}, false, nil
			}
			if !valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
			left, ok, err := directFrameStringField(registers[ins.a], proto.constantKeys[ins.b].str)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field failed: %w", err)
			}
			if !ok || left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return vmFrameResult{}, false, nil
			}
			right := proto.constantNumbers[ins.c]
			if math.IsNaN(left.number) || math.IsNaN(right) {
				return vmFrameResult{}, false, nil
			}
			greater := left.number > right
			if (ins.op == opJumpIfStringFieldNotGreaterK && !greater) ||
				(ins.op == opJumpIfStringFieldGreaterK && greater) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldFalse:
			value, ok, err := directFrameRowStringField(registers[ins.a], proto.constantKeys[ins.b].str, ins.c)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field failed: %w", err)
			}
			if !ok {
				return vmFrameResult{}, false, nil
			}
			if !value.truthy() {
				frame.pc = ins.d
				continue
			}

		case opJumpIfFalse:
			if !registers[ins.a].truthy() {
				frame.pc = ins.b
				continue
			}

		case opJump:
			frame.pc = ins.b
			continue

		case opCall:
			resultCount := ins.d
			if resultCount == 0 {
				resultCount = 1
			}
			callee := registers[ins.b]
			if ins.c == 2 && resultCount == 2 && callee.nativeID == nativeFuncArrayNext {
				results, count, err := baseArrayNextInline(registers[ins.b+1], registers[ins.b+2])
				if err != nil {
					return vmFrameResult{}, true, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				thread.directFrameArrayNext.addGenericInline()
				frame.openCallStart = -1
				frame.openCallResults = nil
				for i := 0; i < resultCount; i++ {
					if i >= count {
						registers[ins.a+i] = NilValue()
					} else {
						registers[ins.a+i] = results[i]
					}
				}
				break
			}
			return vmFrameResult{}, false, nil

		case opCallTableFieldKeyOne:
			argCount := tableFieldKeyCallArgCount(ins.d)
			keySource := ins.a + argCount + 1
			keyValue, ok, err := directFrameRowStringField(registers[keySource], proto.constantKeys[ins.c].str, tableFieldKeyCallKeySlot(ins.d))
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get field failed: %w", err)
			}
			if !ok || keyValue.kind != StringKind {
				return vmFrameResult{}, false, nil
			}
			handlerTableValue := registers[ins.b]
			if handlerTableValue.kind != TableKind || handlerTableValue.table == nil {
				return vmFrameResult{}, true, fmt.Errorf("run: get index target is %s, want table", handlerTableValue.Kind())
			}
			handlerTable := handlerTableValue.table
			if handlerTable.metatable != nil {
				return vmFrameResult{}, false, nil
			}
			callee, ok := handlerTable.rawStringField(keyValue.str)
			if !ok {
				return vmFrameResult{}, false, nil
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return vmFrameResult{}, false, nil
			}
			args := registers[ins.a+1 : ins.a+1+argCount]
			frame.pc++
			value, err := thread.runInlineScriptCallOneNoHook(closure, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: vmResultDestination{register: ins.a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
				}
				return vmFrameResult{}, true, err
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			registers[ins.a] = value
			continue

		case opMathMin:
			_, fast, err := mathIntrinsicCallee(thread.globals, "min")
			if err != nil {
				return vmFrameResult{}, true, err
			}
			if !fast || ins.d != 1 {
				return vmFrameResult{}, false, nil
			}
			minimum, err := baseMathMinValue(registers[ins.a : ins.a+ins.b])
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: call failed: host function failed: %w", err)
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			registers[ins.a] = NumberValue(minimum)

		case opReturnOne:
			return vmReturnedValue(registers[ins.a]), true, nil

		case opReturn:
			count := ins.b
			if count < 0 {
				return vmFrameResult{}, false, nil
			}
			if count == 0 {
				return vmReturnedValues(nil), true, nil
			}
			if count == 1 {
				return vmReturnedValue(registers[ins.a]), true, nil
			}
			results := make([]Value, count)
			copy(results, registers[ins.a:ins.a+count])
			return vmReturnedValues(results), true, nil

		default:
			return vmFrameResult{}, false, nil
		}
		frame.pc++
	}

	return vmReturnedValues(nil), true, nil
}

func (thread *vmThread) runGenericFrame(frame *vmFrame) (vmFrameResult, error) {
	proto := frame.proto
	upvalues := frame.upvalues
	globals := thread.globals
	varargs := frame.varargs
	runLineHook := thread.debugHook != nil && thread.debugLineHook
	runCountHook := thread.debugHook != nil && thread.debugCountInterval > 0
	runInstructionBudget := thread.instructionBudget >= 0

	for frame.pc < len(frame.proto.code) {
		if runInstructionBudget {
			if thread.instructionBudget == 0 {
				return vmFrameResult{state: vmCallStateHostInterrupt}, nil
			}
			thread.instructionBudget--
		}
		if runLineHook {
			if err := thread.runDebugLineHook(frame); err != nil {
				return vmFrameResult{}, err
			}
		}
		if runCountHook {
			if err := thread.runDebugCountHook(frame); err != nil {
				return vmFrameResult{}, err
			}
		}
		ins := frame.proto.code[frame.pc]

		switch ins.op {
		case opLoadConst:
			if frame.directRegisters {
				frame.registers[ins.a] = proto.constants[ins.b]
				break
			}
			frame.setRegister(ins.a, proto.constants[ins.b])

		case opLoadGlobal:
			name, _ := proto.constants[ins.b].String()
			value, ok := globals.get(name)
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: undefined global %q", name)
			}
			if frame.directRegisters {
				frame.registers[ins.a] = value
				break
			}
			frame.setRegister(ins.a, value)

		case opSetGlobal:
			name, _ := proto.constants[ins.a].String()
			if frame.directRegisters {
				globals.set(name, frame.registers[ins.b])
				break
			}
			globals.set(name, frame.register(ins.b))

		case opMove:
			if frame.directRegisters {
				frame.registers[ins.a] = frame.registers[ins.b]
				break
			}
			frame.setRegister(ins.a, frame.register(ins.b))

		case opNewTable:
			if frame.directRegisters {
				frame.registers[ins.a] = TableValue(newTableWithCapacity(ins.b, ins.c))
				break
			}
			frame.setRegister(ins.a, TableValue(newTableWithCapacity(ins.b, ins.c)))

		case opClosure:
			captured := captureUpvalues(proto.prototypes[ins.b], frame)
			value := functionValue(proto.prototypes[ins.b], captured)
			if frame.directRegisters {
				frame.registers[ins.a] = value
				break
			}
			frame.setRegister(ins.a, value)

		case opGetUpvalue:
			if frame.directRegisters {
				frame.registers[ins.a] = upvalues[ins.b].value
				break
			}
			frame.setRegister(ins.a, upvalues[ins.b].value)

		case opSetUpvalue:
			if frame.directRegisters {
				upvalues[ins.a].value = frame.registers[ins.b]
				break
			}
			upvalues[ins.a].value = frame.register(ins.b)

		case opVararg:
			resultCount := ins.b
			if resultCount == 0 {
				resultCount = 1
			}
			if resultCount < 0 {
				frame.openCallStart = ins.a
				frame.openCallResults = adjustedCallResults(varargs)
				if frame.directRegisters {
					frame.registers[ins.a] = frame.openCallResults[0]
				} else {
					frame.setRegister(ins.a, frame.openCallResults[0])
				}
				frame.pc++
				continue
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			if frame.directRegisters {
				copied := false
				if len(varargs) >= resultCount {
					switch resultCount {
					case 1:
						frame.registers[ins.a] = varargs[0]
						copied = true
					case 2:
						frame.registers[ins.a] = varargs[0]
						frame.registers[ins.a+1] = varargs[1]
						copied = true
					case 3:
						frame.registers[ins.a] = varargs[0]
						frame.registers[ins.a+1] = varargs[1]
						frame.registers[ins.a+2] = varargs[2]
						copied = true
					case 4:
						frame.registers[ins.a] = varargs[0]
						frame.registers[ins.a+1] = varargs[1]
						frame.registers[ins.a+2] = varargs[2]
						frame.registers[ins.a+3] = varargs[3]
						copied = true
					case 5:
						frame.registers[ins.a] = varargs[0]
						frame.registers[ins.a+1] = varargs[1]
						frame.registers[ins.a+2] = varargs[2]
						frame.registers[ins.a+3] = varargs[3]
						frame.registers[ins.a+4] = varargs[4]
						copied = true
					}
					if copied {
						break
					}
				}
				for i := 0; i < resultCount; i++ {
					if i >= len(varargs) {
						frame.registers[ins.a+i] = NilValue()
					} else {
						frame.registers[ins.a+i] = varargs[i]
					}
				}
				break
			}
			for i := 0; i < resultCount; i++ {
				if i >= len(varargs) {
					frame.setRegister(ins.a+i, NilValue())
				} else {
					frame.setRegister(ins.a+i, varargs[i])
				}
			}

		case opPrepareIter:
			generator, state, control, ok, err := prepareIterator(frame.register(ins.a), globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: prepare iterator failed: %w", err)
			}
			if ok {
				frame.setRegister(ins.a, generator)
				frame.setRegister(ins.b, state)
				frame.setRegister(ins.c, control)
			}

		case opSetField:
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				table := base.table
				if table.metatable == nil && proto.constantKeyOK[ins.b] {
					value := frame.registers[ins.c]
					key := proto.constantKeys[ins.b]
					if key.kind == StringKind {
						table.setRawStringField(key.str, value)
						break
					}
					if value.IsNil() {
						delete(table.fields, key)
						break
					}
					if table.fields == nil {
						table.fields = make(map[tableKey]Value)
					}
					table.fields[key] = value
					break
				}
			}
			table, ok := frame.register(ins.a).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", frame.register(ins.a).Kind())
			}
			if table.metatable == nil {
				if proto.constantKeyOK[ins.b] {
					if err := table.rawSetKey(proto.constantKeys[ins.b], frame.register(ins.c)); err != nil {
						return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
					}
					break
				}
			}
			if err := runtimeTableAccess(globals).set(table, proto.constants[ins.b], frame.register(ins.c)); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSetStringField:
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				table := base.table
				value := frame.registers[ins.c]
				if table.metatable == nil {
					table.setRawStringField(proto.constantKeys[ins.b].str, value)
					break
				}
			}
			table, ok := frame.register(ins.a).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", frame.register(ins.a).Kind())
			}
			if table.metatable == nil {
				table.setRawStringField(proto.constantKeys[ins.b].str, frame.register(ins.c))
				break
			}
			if err := runtimeTableAccess(globals).set(table, proto.constants[ins.b], frame.register(ins.c)); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSetRowStringField:
			key := proto.constantKeys[ins.b].str
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				table := base.table
				value := frame.registers[ins.c]
				if table.metatable == nil {
					slot := tableStringFieldSlot{index: ins.d, version: table.stringVersion}
					if !table.setRawStringFieldAtSlot(slot, key, value) {
						table.setRawStringField(key, value)
					}
					break
				}
			}
			table, ok := frame.register(ins.a).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", frame.register(ins.a).Kind())
			}
			value := frame.register(ins.c)
			if table.metatable == nil {
				slot := tableStringFieldSlot{index: ins.d, version: table.stringVersion}
				if !table.setRawStringFieldAtSlot(slot, key, value) {
					table.setRawStringField(key, value)
				}
				break
			}
			if err := runtimeTableAccess(globals).set(table, proto.constants[ins.b], value); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSetStringField2:
			firstKey := proto.constantKeys[ins.b].str
			secondKey := proto.constantKeys[ins.c].str
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				table := base.table
				if first, ok := table.rawStringField(firstKey); ok {
					if first.kind != TableKind || first.table == nil {
						return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", first.Kind())
					}
					nextTable := first.table
					if nextTable.metatable == nil {
						nextTable.setRawStringField(secondKey, frame.registers[ins.d])
						break
					}
				} else if table.metatable == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", NilValue().Kind())
				}
			}
			base := frame.register(ins.a)
			table, ok := base.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
			}
			access := runtimeTableAccess(globals)
			first, err := access.getString(table, firstKey, proto.constants[ins.b])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			nextTable, ok := first.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", first.Kind())
			}
			if nextTable.metatable == nil {
				nextTable.setRawStringField(secondKey, frame.register(ins.d))
				break
			}
			if err := access.set(nextTable, proto.constants[ins.c], frame.register(ins.d)); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opAddStringField:
			key := proto.constantKeys[ins.b].str
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				right := frame.registers[ins.c]
				if table.metatable == nil {
					left, _ := table.rawStringField(key)
					if left.kind == NumberKind && right.kind == NumberKind {
						table.setRawStringField(key, NumberValue(left.number+right.number))
						break
					}
					value, err := binaryArithmeticValue(
						left,
						right,
						globals,
						"__add",
						"add",
						func(left float64, right float64) float64 { return left + right },
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
					}
					table.setRawStringField(key, value)
					break
				}
			}
			base := frame.register(ins.a)
			table, ok := base.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			access := runtimeTableAccess(globals)
			left, err := access.getString(table, key, proto.constants[ins.b])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			right := frame.register(ins.c)
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__add",
				"add",
				func(left float64, right float64) float64 { return left + right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
			}
			if err := access.set(table, proto.constants[ins.b], value); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSubStringField:
			key := proto.constantKeys[ins.b].str
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				right := frame.registers[ins.c]
				if table.metatable == nil {
					if slot, ok := table.rawStringFieldSlot(key); ok {
						left, leftOK := table.rawStringFieldAtSlot(slot, key)
						if leftOK &&
							left.kind == NumberKind &&
							right.kind == NumberKind &&
							table.setRawStringFieldAtSlot(slot, key, NumberValue(left.number-right.number)) {
							break
						}
					}
					left, _ := table.rawStringField(key)
					value, err := binaryArithmeticValue(
						left,
						right,
						globals,
						"__sub",
						"subtract",
						func(left float64, right float64) float64 { return left - right },
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
					}
					table.setRawStringField(key, value)
					break
				}
			}
			base := frame.register(ins.a)
			table, ok := base.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			access := runtimeTableAccess(globals)
			left, err := access.getString(table, key, proto.constants[ins.b])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			right := frame.register(ins.c)
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
			}
			if err := access.set(table, proto.constants[ins.b], value); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSubAddStringField:
			desc := proto.rowFieldSubAddOps[ins.b]
			targetKey := proto.constantKeys[desc.target].str
			addKey := proto.constantKeys[desc.add].str
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				subtract := frame.registers[ins.c]
				if table.metatable == nil {
					var left Value
					var add Value
					if desc.targetSlot >= 0 && desc.addSlot >= 0 {
						version := table.stringVersion
						targetSlot := tableStringFieldSlot{index: desc.targetSlot, version: version}
						addSlot := tableStringFieldSlot{index: desc.addSlot, version: version}
						var leftOK bool
						var addOK bool
						left, leftOK = table.rawStringFieldAtSlot(targetSlot, targetKey)
						add, addOK = table.rawStringFieldAtSlot(addSlot, addKey)
						if leftOK && addOK &&
							left.kind == NumberKind &&
							subtract.kind == NumberKind &&
							add.kind == NumberKind &&
							table.setRawStringFieldAtSlot(targetSlot, targetKey, NumberValue(left.number-subtract.number+add.number)) {
							break
						}
					} else if targetSlot, ok := table.rawStringFieldSlot(targetKey); ok {
						if addSlot, ok := table.rawStringFieldSlot(addKey); ok {
							var leftOK bool
							var addOK bool
							left, leftOK = table.rawStringFieldAtSlot(targetSlot, targetKey)
							add, addOK = table.rawStringFieldAtSlot(addSlot, addKey)
							if leftOK && addOK &&
								left.kind == NumberKind &&
								subtract.kind == NumberKind &&
								add.kind == NumberKind &&
								table.setRawStringFieldAtSlot(targetSlot, targetKey, NumberValue(left.number-subtract.number+add.number)) {
								break
							}
						}
					}
					left, _ = table.rawStringField(targetKey)
					add, _ = table.rawStringField(addKey)
					subValue, err := binaryArithmeticValue(
						left,
						subtract,
						globals,
						"__sub",
						"subtract",
						func(left float64, right float64) float64 { return left - right },
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
					}
					value, err := binaryArithmeticValue(
						subValue,
						add,
						globals,
						"__add",
						"add",
						func(left float64, right float64) float64 { return left + right },
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
					}
					table.setRawStringField(targetKey, value)
					break
				}
			}
			base := frame.register(ins.a)
			table, ok := base.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			access := runtimeTableAccess(globals)
			left, err := access.getString(table, targetKey, proto.constants[desc.target])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			subtract := frame.register(ins.c)
			subValue, err := binaryArithmeticValue(
				left,
				subtract,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
			}
			add, err := access.getString(table, addKey, proto.constants[desc.add])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			value, err := binaryArithmeticValue(
				subValue,
				add,
				globals,
				"__add",
				"add",
				func(left float64, right float64) float64 { return left + right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
			}
			if err := access.set(table, proto.constants[desc.target], value); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opAddSubStringField2:
			desc := proto.stringField2AddSubOps[ins.b]
			targetFirstKey := proto.constantKeys[desc.targetFirst].str
			targetSecondKey := proto.constantKeys[desc.targetSecond].str
			addFirstKey := proto.constantKeys[desc.addFirst].str
			addSecondKey := proto.constantKeys[desc.addSecond].str
			subFirstKey := proto.constantKeys[desc.subFirst].str
			subSecondKey := proto.constantKeys[desc.subSecond].str
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				if table.metatable == nil {
					targetFirst, ok := table.rawStringField(targetFirstKey)
					if !ok {
						return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", NilValue().Kind())
					}
					if targetFirst.kind != TableKind || targetFirst.table == nil {
						return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", targetFirst.Kind())
					}
					targetTable := targetFirst.table
					addFirst, ok := table.rawStringField(addFirstKey)
					if !ok {
						return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", NilValue().Kind())
					}
					if addFirst.kind != TableKind || addFirst.table == nil {
						return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", addFirst.Kind())
					}
					addTable := addFirst.table
					subFirst, ok := table.rawStringField(subFirstKey)
					if !ok {
						return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", NilValue().Kind())
					}
					if subFirst.kind != TableKind || subFirst.table == nil {
						return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", subFirst.Kind())
					}
					subTable := subFirst.table
					if targetTable.metatable == nil && addTable.metatable == nil && subTable.metatable == nil {
						left, _ := targetTable.rawStringField(targetSecondKey)
						addRight, _ := addTable.rawStringField(addSecondKey)
						subRight, _ := subTable.rawStringField(subSecondKey)
						if left.kind == NumberKind && addRight.kind == NumberKind && subRight.kind == NumberKind {
							targetTable.setRawStringField(targetSecondKey, NumberValue(left.number+addRight.number-subRight.number))
							break
						}
					}
				}
			}
			base := frame.register(ins.a)
			table, ok := base.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			access := runtimeTableAccess(globals)
			left, err := getStringField2(access, table, targetFirstKey, proto.constants[desc.targetFirst], targetSecondKey, proto.constants[desc.targetSecond])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			addRight, err := getStringField2(access, table, addFirstKey, proto.constants[desc.addFirst], addSecondKey, proto.constants[desc.addSecond])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			value, err := binaryArithmeticValue(
				left,
				addRight,
				globals,
				"__add",
				"add",
				func(left float64, right float64) float64 { return left + right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
			}
			subRight, err := getStringField2(access, table, subFirstKey, proto.constants[desc.subFirst], subSecondKey, proto.constants[desc.subSecond])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			value, err = binaryArithmeticValue(
				value,
				subRight,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
			}
			if err := setStringField2(access, table, targetFirstKey, proto.constants[desc.targetFirst], targetSecondKey, proto.constants[desc.targetSecond], value); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opGetField:
			if frame.directRegisters {
				base := frame.registers[ins.b]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				if proto.constantKeyOK[ins.c] {
					key := proto.constantKeys[ins.c]
					if key.kind == StringKind {
						if value, ok := table.rawStringField(key.str); ok {
							frame.registers[ins.a] = value
							break
						}
						if table.metatable == nil {
							frame.registers[ins.a] = NilValue()
							break
						}
						if indexTable, ok, err := table.cachedIndexTable(); err != nil {
							return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
						} else if ok {
							if value, ok := indexTable.rawStringField(key.str); ok {
								frame.registers[ins.a] = value
								break
							}
							if indexTable.metatable == nil {
								frame.registers[ins.a] = NilValue()
								break
							}
							value, err := runtimeTableAccess(globals).getSeen(
								indexTable,
								proto.constants[ins.c],
								map[*Table]bool{table: true},
							)
							if err != nil {
								return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
							}
							frame.registers[ins.a] = value
							break
						}
						index, err := table.metatable.rawGetString("__index")
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
						}
						if index.IsNil() {
							frame.registers[ins.a] = NilValue()
							break
						}
						if indexTable, ok := index.Table(); ok {
							if value, ok := indexTable.rawStringField(key.str); ok {
								frame.registers[ins.a] = value
								break
							}
							if indexTable.metatable == nil {
								frame.registers[ins.a] = NilValue()
								break
							}
							value, err := runtimeTableAccess(globals).getSeen(
								indexTable,
								proto.constants[ins.c],
								map[*Table]bool{table: true},
							)
							if err != nil {
								return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
							}
							frame.registers[ins.a] = value
							break
						}
						if callableValue(index) {
							value, err := runtimeTableAccess(globals).callIndex(index, table, proto.constants[ins.c])
							if err != nil {
								return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
							}
							frame.registers[ins.a] = value
							break
						}
						return vmFrameResult{}, fmt.Errorf("run: get field failed: table: __index is %s, want table or function", index.Kind())
					}
					if table.fields != nil {
						if value, ok := table.fields[key]; ok {
							frame.registers[ins.a] = value
							break
						}
					}
					if table.metatable == nil {
						frame.registers[ins.a] = NilValue()
						break
					}
					index, err := table.metatable.rawGetKey(tableKey{kind: StringKind, str: "__index"})
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
					}
					if index.IsNil() {
						frame.registers[ins.a] = NilValue()
						break
					}
					if indexTable, ok := index.Table(); ok {
						if indexTable.fields != nil {
							if value, ok := indexTable.fields[key]; ok {
								frame.registers[ins.a] = value
								break
							}
						}
						if indexTable.metatable == nil {
							frame.registers[ins.a] = NilValue()
							break
						}
						value, err := runtimeTableAccess(globals).getSeen(
							indexTable,
							proto.constants[ins.c],
							map[*Table]bool{table: true},
						)
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
						}
						frame.registers[ins.a] = value
						break
					}
					if callableValue(index) {
						value, err := runtimeTableAccess(globals).callIndex(index, table, proto.constants[ins.c])
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
						}
						frame.registers[ins.a] = value
						break
					}
					return vmFrameResult{}, fmt.Errorf("run: get field failed: table: __index is %s, want table or function", index.Kind())
				}
			}
			table, ok := frame.register(ins.b).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", frame.register(ins.b).Kind())
			}
			if table.metatable == nil {
				if proto.constantKeyOK[ins.c] {
					value, err := table.rawGetKey(proto.constantKeys[ins.c])
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
					}
					frame.setRegister(ins.a, value)
					break
				}
			}
			value, err := runtimeTableAccess(globals).get(table, proto.constants[ins.c])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opGetStringField:
			key := proto.constantKeys[ins.c].str
			if frame.directRegisters {
				base := frame.registers[ins.b]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				if value, ok := table.rawStringField(key); ok {
					frame.registers[ins.a] = value
					break
				}
				if table.metatable == nil {
					frame.registers[ins.a] = NilValue()
					break
				}
				if indexTable, ok, err := table.cachedIndexTable(); err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				} else if ok {
					if value, ok := indexTable.rawStringField(key); ok {
						frame.registers[ins.a] = value
						break
					}
					if indexTable.metatable == nil {
						frame.registers[ins.a] = NilValue()
						break
					}
					value, err := runtimeTableAccess(globals).getSeen(
						indexTable,
						proto.constants[ins.c],
						map[*Table]bool{table: true},
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
					}
					frame.registers[ins.a] = value
					break
				}
				index, err := table.metatable.rawGetString("__index")
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				if index.IsNil() {
					frame.registers[ins.a] = NilValue()
					break
				}
				if indexTable, ok := index.Table(); ok {
					if value, ok := indexTable.rawStringField(key); ok {
						frame.registers[ins.a] = value
						break
					}
					if indexTable.metatable == nil {
						frame.registers[ins.a] = NilValue()
						break
					}
					value, err := runtimeTableAccess(globals).getSeen(
						indexTable,
						proto.constants[ins.c],
						map[*Table]bool{table: true},
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
					}
					frame.registers[ins.a] = value
					break
				}
				if callableValue(index) {
					value, err := runtimeTableAccess(globals).callIndex(index, table, proto.constants[ins.c])
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
					}
					frame.registers[ins.a] = value
					break
				}
				return vmFrameResult{}, fmt.Errorf("run: get field failed: table: __index is %s, want table or function", index.Kind())
			}
			table, ok := frame.register(ins.b).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", frame.register(ins.b).Kind())
			}
			if value, ok := table.rawStringField(key); ok {
				frame.setRegister(ins.a, value)
				break
			}
			if table.metatable == nil {
				frame.setRegister(ins.a, NilValue())
				break
			}
			value, err := runtimeTableAccess(globals).get(table, proto.constants[ins.c])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opGetRowStringField:
			key := proto.constantKeys[ins.c].str
			var base Value
			if frame.directRegisters {
				base = frame.registers[ins.b]
			} else {
				base = frame.register(ins.b)
			}
			table, ok := base.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			if table.metatable == nil {
				slot := tableStringFieldSlot{index: ins.d, version: table.stringVersion}
				if value, ok := table.rawStringFieldAtSlot(slot, key); ok {
					if frame.directRegisters {
						frame.registers[ins.a] = value
					} else {
						frame.setRegister(ins.a, value)
					}
					break
				}
				if value, ok := table.rawStringField(key); ok {
					if frame.directRegisters {
						frame.registers[ins.a] = value
					} else {
						frame.setRegister(ins.a, value)
					}
					break
				}
				if frame.directRegisters {
					frame.registers[ins.a] = NilValue()
				} else {
					frame.setRegister(ins.a, NilValue())
				}
				break
			}
			value, err := runtimeTableAccess(globals).get(table, proto.constants[ins.c])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			if frame.directRegisters {
				frame.registers[ins.a] = value
			} else {
				frame.setRegister(ins.a, value)
			}

		case opGetStringField2:
			firstKey := proto.constantKeys[ins.c].str
			secondKey := proto.constantKeys[ins.d].str
			if frame.directRegisters {
				base := frame.registers[ins.b]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				if first, ok := table.rawStringField(firstKey); ok {
					if first.kind != TableKind || first.table == nil {
						return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", first.Kind())
					}
					nextTable := first.table
					if second, ok := nextTable.rawStringField(secondKey); ok {
						frame.registers[ins.a] = second
						break
					}
					if nextTable.metatable == nil {
						frame.registers[ins.a] = NilValue()
						break
					}
				} else if table.metatable == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", NilValue().Kind())
				}
			}
			var base Value
			if frame.directRegisters {
				base = frame.registers[ins.b]
			} else {
				base = frame.register(ins.b)
			}
			table, ok := base.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			access := runtimeTableAccess(globals)
			first, err := access.getString(table, firstKey, proto.constants[ins.c])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			nextTable, ok := first.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", first.Kind())
			}
			second, err := access.getString(nextTable, secondKey, proto.constants[ins.d])
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
			}
			if frame.directRegisters {
				frame.registers[ins.a] = second
				break
			}
			frame.setRegister(ins.a, second)

		case opSetIndex:
			table, ok := frame.register(ins.a).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set index target is %s, want table", frame.register(ins.a).Kind())
			}
			if err := runtimeTableAccess(globals).set(table, frame.register(ins.b), frame.register(ins.c)); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set index failed: %w", err)
			}

		case opGetIndex:
			table, ok := frame.register(ins.b).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get index target is %s, want table", frame.register(ins.b).Kind())
			}
			value, err := runtimeTableAccess(globals).get(table, frame.register(ins.c))
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get index failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opAdd:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				right := frame.registers[ins.c]
				if left.kind == NumberKind && right.kind == NumberKind {
					frame.registers[ins.a] = NumberValue(left.number + right.number)
					break
				}
			}
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number+right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__add",
				"add",
				func(left float64, right float64) float64 { return left + right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opSub:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				right := frame.registers[ins.c]
				if left.kind == NumberKind && right.kind == NumberKind {
					frame.registers[ins.a] = NumberValue(left.number - right.number)
					break
				}
			}
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number-right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opMul:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				right := frame.registers[ins.c]
				if left.kind == NumberKind && right.kind == NumberKind {
					frame.registers[ins.a] = NumberValue(left.number * right.number)
					break
				}
			}
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number*right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__mul",
				"multiply",
				func(left float64, right float64) float64 { return left * right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opDiv:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number/right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__div",
				"divide",
				func(left float64, right float64) float64 { return left / right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opMod:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number-math.Floor(left.number/right.number)*right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__mod",
				"modulo",
				func(left float64, right float64) float64 {
					return left - math.Floor(left/right)*right
				},
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opIDiv:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(math.Floor(left.number/right.number)))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__idiv",
				"floor divide",
				func(left float64, right float64) float64 { return math.Floor(left / right) },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opPow:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(math.Pow(left.number, right.number)))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__pow",
				"power",
				func(left float64, right float64) float64 { return math.Pow(left, right) },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opNeg:
			operand := frame.register(ins.b)
			if operand.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(-operand.number))
				break
			}
			value, err := negateValue(operand, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opLen:
			value, err := lengthValue(frame.register(ins.b), globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: length failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opConcat:
			value, err := concatValue(frame.register(ins.b), frame.register(ins.c), globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: concat failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opAddK:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				if left.kind == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(left.number + proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number+right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__add",
				"add",
				func(left float64, right float64) float64 { return left + right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opSubK:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				if left.kind == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(left.number - proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number-right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opMulK:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				if left.kind == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(left.number * proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number*right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__mul",
				"multiply",
				func(left float64, right float64) float64 { return left * right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: multiply failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opDivK:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				if left.kind == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(left.number / proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number/right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__div",
				"divide",
				func(left float64, right float64) float64 { return left / right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: divide failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opModK:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				if left.kind == NumberKind && proto.constantNumberOK[ins.c] {
					right := proto.constantNumbers[ins.c]
					frame.registers[ins.a] = NumberValue(left.number - math.Floor(left.number/right)*right)
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(left.number-math.Floor(left.number/right.number)*right.number))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__mod",
				"modulo",
				math.Mod,
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: modulo failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opIDivK:
			if frame.directRegisters {
				left := frame.registers[ins.b]
				if left.kind == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(math.Floor(left.number / proto.constantNumbers[ins.c]))
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if left.kind == NumberKind && right.kind == NumberKind {
				frame.setRegister(ins.a, NumberValue(math.Floor(left.number/right.number)))
				break
			}
			value, err := binaryArithmeticValue(
				left,
				right,
				globals,
				"__idiv",
				"floor divide",
				func(left float64, right float64) float64 { return math.Floor(left / right) },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: floor divide failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opAddNumericModK:
			desc := proto.numericAddModOps[ins.c]
			mulRight := proto.constants[desc.mul]
			idivRight := proto.constants[desc.idiv]
			modRight := proto.constants[desc.mod]
			if frame.directRegisters &&
				proto.constantNumberOK[desc.mul] &&
				proto.constantNumberOK[desc.idiv] &&
				proto.constantNumberOK[desc.mod] {
				left := frame.registers[ins.a]
				source := frame.registers[ins.b]
				if left.kind == NumberKind && source.kind == NumberKind {
					mul := source.number * proto.constantNumbers[desc.mul]
					idiv := math.Floor(source.number / proto.constantNumbers[desc.idiv])
					beforeMod := mul - idiv
					mod := proto.constantNumbers[desc.mod]
					frame.registers[ins.a] = NumberValue(left.number + beforeMod - math.Floor(beforeMod/mod)*mod)
					break
				}
			}
			left := frame.register(ins.a)
			source := frame.register(ins.b)
			mulValue, err := binaryArithmeticValue(
				source,
				mulRight,
				globals,
				"__mul",
				"multiply",
				func(left float64, right float64) float64 { return left * right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: multiply failed: %w", err)
			}
			idivValue, err := binaryArithmeticValue(
				source,
				idivRight,
				globals,
				"__idiv",
				"floor divide",
				func(left float64, right float64) float64 { return math.Floor(left / right) },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: floor divide failed: %w", err)
			}
			subValue, err := binaryArithmeticValue(
				mulValue,
				idivValue,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
			}
			modValue, err := binaryArithmeticValue(
				subValue,
				modRight,
				globals,
				"__mod",
				"modulo",
				func(left float64, right float64) float64 {
					return left - math.Floor(left/right)*right
				},
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: modulo failed: %w", err)
			}
			value, err := binaryArithmeticValue(
				left,
				modValue,
				globals,
				"__add",
				"add",
				func(left float64, right float64) float64 { return left + right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
			}
			if frame.directRegisters {
				frame.registers[ins.a] = value
			} else {
				frame.setRegister(ins.a, value)
			}

		case opEqual:
			value, err := equalValue(frame.register(ins.b), frame.register(ins.c), globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: equal failed: %w", err)
			}
			frame.setRegister(ins.a, BoolValue(value))

		case opNotEqual:
			value, err := equalValue(frame.register(ins.b), frame.register(ins.c), globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: not equal failed: %w", err)
			}
			frame.setRegister(ins.a, BoolValue(!value))

		case opLess:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
				frame.setRegister(ins.a, BoolValue(left.number < right.number))
				break
			}
			value, err := lessValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: less failed: %w", err)
			}
			frame.setRegister(ins.a, BoolValue(value))

		case opLessEqual:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
				frame.setRegister(ins.a, BoolValue(left.number <= right.number))
				break
			}
			value, err := lessEqualValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: less equal failed: %w", err)
			}
			frame.setRegister(ins.a, BoolValue(value))

		case opGreater:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
				frame.setRegister(ins.a, BoolValue(left.number > right.number))
				break
			}
			value, err := lessValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
			}
			frame.setRegister(ins.a, BoolValue(value))

		case opGreaterEqual:
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
				frame.setRegister(ins.a, BoolValue(left.number >= right.number))
				break
			}
			value, err := lessEqualValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater equal failed: %w", err)
			}
			frame.setRegister(ins.a, BoolValue(value))

		case opNumericForCheck:
			if frame.directRegisters {
				loopValue := frame.registers[ins.a]
				limitValue := frame.registers[ins.b]
				stepValue := frame.registers[ins.c]
				if loopValue.kind != NumberKind {
					return vmFrameResult{}, fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind())
				}
				if limitValue.kind != NumberKind {
					return vmFrameResult{}, fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind())
				}
				if stepValue.kind != NumberKind {
					return vmFrameResult{}, fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind())
				}
				if math.IsNaN(loopValue.number) || math.IsNaN(limitValue.number) || math.IsNaN(stepValue.number) {
					return vmFrameResult{}, fmt.Errorf("run: numeric for operand is NaN")
				}
				if stepValue.number > 0 {
					if loopValue.number > limitValue.number {
						frame.pc = ins.d
						continue
					}
					break
				}
				if loopValue.number < limitValue.number {
					frame.pc = ins.d
					continue
				}
				break
			}
			loopValue := frame.register(ins.a)
			limitValue := frame.register(ins.b)
			stepValue := frame.register(ins.c)
			if loopValue.kind != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind())
			}
			if limitValue.kind != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind())
			}
			if stepValue.kind != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind())
			}
			if math.IsNaN(loopValue.number) || math.IsNaN(limitValue.number) || math.IsNaN(stepValue.number) {
				return vmFrameResult{}, fmt.Errorf("run: numeric for operand is NaN")
			}
			if stepValue.number > 0 {
				if loopValue.number > limitValue.number {
					frame.pc = ins.d
					continue
				}
				break
			}
			if loopValue.number < limitValue.number {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotEqualK:
			if frame.directRegisters {
				left := frame.registers[ins.a]
				if left.kind == NumberKind && proto.constantNumberOK[ins.b] {
					if left.number != proto.constantNumbers[ins.b] {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := proto.constants[ins.b]
			if left.kind == NumberKind && right.kind == NumberKind {
				if left.number != right.number {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := equalValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: equal failed: %w", err)
			}
			if !value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLessK:
			if frame.directRegisters {
				left := frame.registers[ins.a]
				if left.kind == NumberKind && proto.constantNumberOK[ins.b] {
					right := proto.constantNumbers[ins.b]
					if !math.IsNaN(left.number) && !math.IsNaN(right) && left.number >= right {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := proto.constants[ins.b]
			if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
				if left.number >= right.number {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := lessValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: less failed: %w", err)
			}
			if !value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfModKNotEqualK:
			var left Value
			if frame.directRegisters {
				left = frame.registers[ins.a]
			} else {
				left = frame.register(ins.a)
			}
			modRight := proto.constants[ins.b]
			want := proto.constants[ins.c]
			if left.kind == NumberKind && modRight.kind == NumberKind && want.kind == NumberKind {
				got := left.number - math.Floor(left.number/modRight.number)*modRight.number
				if got != want.number {
					frame.pc = ins.d
					continue
				}
				break
			}
			modValue, err := binaryArithmeticValue(
				left,
				modRight,
				globals,
				"__mod",
				"modulo",
				func(left float64, right float64) float64 {
					return left - math.Floor(left/right)*right
				},
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: modulo failed: %w", err)
			}
			equal, err := equalValue(modValue, want, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: equal failed: %w", err)
			}
			if !equal {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotEqualK:
			key := proto.constantKeys[ins.b].str
			var base Value
			if frame.directRegisters {
				base = frame.registers[ins.a]
			} else {
				base = frame.register(ins.a)
			}
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			var left Value
			if value, ok := table.rawStringField(key); ok {
				left = value
			} else if table.metatable == nil {
				left = NilValue()
			} else {
				value, err := runtimeTableAccess(globals).get(table, proto.constants[ins.b])
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				left = value
			}
			right := proto.constants[ins.c]
			if left.kind == StringKind && right.kind == StringKind {
				if left.str != right.str {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := equalValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: equal failed: %w", err)
			}
			if !value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotEqualK:
			desc := proto.rowFieldEqualOps[ins.b]
			key := proto.constantKeys[desc.field].str
			var base Value
			if frame.directRegisters {
				base = frame.registers[ins.a]
			} else {
				base = frame.register(ins.a)
			}
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			var left Value
			if table.metatable == nil && desc.slot >= 0 {
				slot := tableStringFieldSlot{index: desc.slot, version: table.stringVersion}
				if value, ok := table.rawStringFieldAtSlot(slot, key); ok {
					left = value
				} else if value, ok := table.rawStringField(key); ok {
					left = value
				} else {
					left = NilValue()
				}
			} else if value, ok := table.rawStringField(key); ok {
				left = value
			} else if table.metatable == nil {
				left = NilValue()
			} else {
				value, err := runtimeTableAccess(globals).get(table, proto.constants[desc.field])
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				left = value
			}
			right := proto.constants[desc.value]
			if left.kind == StringKind && right.kind == StringKind {
				if left.str != right.str {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := equalValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: equal failed: %w", err)
			}
			if !value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
			key := proto.constantKeys[ins.b].str
			var base Value
			if frame.directRegisters {
				base = frame.registers[ins.a]
			} else {
				base = frame.register(ins.a)
			}
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			var left Value
			if value, ok := table.rawStringField(key); ok {
				left = value
			} else if table.metatable == nil {
				left = NilValue()
			} else {
				value, err := runtimeTableAccess(globals).get(table, proto.constants[ins.b])
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				left = value
			}
			if left.kind == NumberKind && proto.constantNumberOK[ins.c] {
				right := proto.constantNumbers[ins.c]
				if !math.IsNaN(left.number) && !math.IsNaN(right) {
					greater := left.number > right
					if (ins.op == opJumpIfStringFieldNotGreaterK && !greater) ||
						(ins.op == opJumpIfStringFieldGreaterK && greater) {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			right := proto.constants[ins.c]
			greater, err := lessValue(right, left, globals)
			if err != nil {
				if ins.op == opJumpIfStringFieldGreaterK {
					return vmFrameResult{}, fmt.Errorf("run: less equal failed: %w", err)
				}
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
			}
			if (ins.op == opJumpIfStringFieldNotGreaterK && !greater) ||
				(ins.op == opJumpIfStringFieldGreaterK && greater) {
				frame.pc = ins.d
				continue
			}

		case opTableInsert:
			args := frame.scriptCallArgs(ins.a, ins.b)
			callee, fast, err := tableIntrinsicCallee(globals, "insert")
			if err != nil {
				return vmFrameResult{}, err
			}
			destination := vmResultDestination{register: ins.a, count: ins.d}
			if fast {
				if _, err := baseTableInsert(args); err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.applyInlineResultDestination(destination, [2]Value{NilValue()}, 1)
				break
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opTableRemove:
			args := frame.scriptCallArgs(ins.a, ins.b)
			callee, fast, err := tableIntrinsicCallee(globals, "remove")
			if err != nil {
				return vmFrameResult{}, err
			}
			destination := vmResultDestination{register: ins.a, count: ins.d}
			if fast {
				removed, err := baseTableRemoveValue(args)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.applyInlineResultDestination(destination, [2]Value{removed}, 1)
				break
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opCoroutineResume:
			args := frame.scriptCallArgs(ins.a, ins.b)
			callee, fast, err := coroutineIntrinsicCallee(globals, "resume")
			if err != nil {
				return vmFrameResult{}, err
			}
			destination := vmResultDestination{register: ins.a, count: ins.d}
			if fast {
				results, err := baseCoroutineResume(globals, args)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.applyResultDestination(destination, results)
				break
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opMathMin:
			args := frame.scriptCallArgs(ins.a, ins.b)
			callee, fast, err := mathIntrinsicCallee(globals, "min")
			if err != nil {
				return vmFrameResult{}, err
			}
			destination := vmResultDestination{register: ins.a, count: ins.d}
			if fast {
				minimum, err := baseMathMinValue(args)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.applyInlineResultDestination(destination, [2]Value{NumberValue(minimum)}, 1)
				break
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opSelectVarargCount:
			destination := vmResultDestination{register: ins.a, count: ins.d}
			frame.openCallStart = -1
			frame.openCallResults = nil
			if globals.nativeGlobalUnchanged("select", nativeFuncSelect) {
				count := NumberValue(float64(len(varargs)))
				if ins.d == 1 {
					if frame.directRegisters {
						frame.registers[ins.a] = count
					} else {
						frame.setRegister(ins.a, count)
					}
					break
				}
				frame.applyInlineResultDestination(destination, [2]Value{count}, 1)
				break
			}
			callee, ok := globals.get("select")
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: undefined global %q", "select")
			}
			args := make([]Value, 1+len(varargs))
			args[0] = StringValue("#")
			copy(args[1:], varargs)
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opCallLocalOne:
			callee := frame.register(ins.b)
			destination := vmResultDestination{register: ins.a, count: 1}
			if closure, ok := callee.scriptFunction(); ok {
				if thread.debugHook == nil &&
					closure.proto != nil &&
					closure.proto.hasFastVariadicSum &&
					ins.d >= len(closure.proto.fastVariadicWeights) {
					total := float64(ins.d)
					fast := true
					for index, weightConstant := range closure.proto.fastVariadicWeights {
						var arg Value
						if frame.directRegisters {
							arg = frame.registers[ins.c+index]
						} else {
							arg = frame.register(ins.c + index)
						}
						if arg.kind != NumberKind || !closure.proto.constantNumberOK[weightConstant] {
							fast = false
							break
						}
						total += arg.number * closure.proto.constantNumbers[weightConstant]
					}
					if fast {
						value := NumberValue(total)
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = value
						} else {
							frame.setRegister(ins.a, value)
						}
						break
					}
				}
				if thread.debugHook == nil &&
					ins.d == 1 &&
					closure.proto != nil &&
					closure.proto.hasFastUpvalueAdd &&
					closure.proto.fastUpvalueAdd < len(closure.upvalues) {
					cell := closure.upvalues[closure.proto.fastUpvalueAdd]
					var arg Value
					if frame.directRegisters {
						arg = frame.registers[ins.c]
					} else {
						arg = frame.register(ins.c)
					}
					if cell != nil && cell.value.kind == NumberKind && arg.kind == NumberKind {
						value := NumberValue(cell.value.number + arg.number)
						cell.value = value
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = value
						} else {
							frame.setRegister(ins.a, value)
						}
						break
					}
				}
				var args []Value
				if frame.directRegisters {
					args = frame.registers[ins.c : ins.c+ins.d]
				} else {
					args = frame.scriptCallArgs(ins.c, ins.d)
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}

			args := frame.copiedCallArgs(ins.c, ins.d)
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opCallUpvalueOne:
			callee := upvalues[ins.b].value
			destination := vmResultDestination{register: ins.a, count: 1}
			if closure, ok := callee.scriptFunction(); ok {
				var args []Value
				if frame.directRegisters {
					args = frame.registers[ins.c : ins.c+ins.d]
				} else {
					args = frame.scriptCallArgs(ins.c, ins.d)
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}

			args := frame.copiedCallArgs(ins.c, ins.d)
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opCallUpvalueSelfOne:
			callee := upvalues[ins.b].value
			destination := vmResultDestination{register: ins.a, count: 1}
			if callee.kind == FunctionKind && callee.function != nil && callee.function.proto == proto {
				var args []Value
				if frame.directRegisters {
					args = frame.registers[ins.c : ins.c+ins.d]
				} else {
					args = frame.scriptCallArgs(ins.c, ins.d)
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(callee.function, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}
			if closure, ok := callee.scriptFunction(); ok {
				var args []Value
				if frame.directRegisters {
					args = frame.registers[ins.c : ins.c+ins.d]
				} else {
					args = frame.scriptCallArgs(ins.c, ins.d)
				}
				frame.pc++
				result, err := thread.runInlineScriptCall(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.applySingleFrameResult(ins.a, result)
				continue
			}

			args := frame.copiedCallArgs(ins.c, ins.d)
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opCallUpvalueSelfKOne:
			callee := upvalues[ins.b].value
			right := proto.constants[ins.d]
			var arg Value
			if frame.directRegisters {
				left := frame.registers[ins.c]
				if left.kind == NumberKind && proto.constantNumberOK[ins.d] {
					arg = NumberValue(left.number - proto.constantNumbers[ins.d])
				} else {
					value, err := binaryArithmeticValue(
						left,
						right,
						globals,
						"__sub",
						"subtract",
						func(left float64, right float64) float64 { return left - right },
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
					}
					arg = value
				}
				frame.registers[ins.a] = arg
			} else {
				left := frame.register(ins.c)
				if left.kind == NumberKind && right.kind == NumberKind {
					arg = NumberValue(left.number - right.number)
				} else {
					value, err := binaryArithmeticValue(
						left,
						right,
						globals,
						"__sub",
						"subtract",
						func(left float64, right float64) float64 { return left - right },
					)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
					}
					arg = value
				}
				frame.setRegister(ins.a, arg)
			}

			destination := vmResultDestination{register: ins.a, count: 1}
			if callee.kind == FunctionKind && callee.function != nil && callee.function.proto == proto {
				var args []Value
				if frame.directRegisters {
					args = frame.registers[ins.a : ins.a+1]
				} else {
					args = frame.scriptCallArgs(ins.a, 1)
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(callee.function, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}

			args := []Value{arg}
			if closure, ok := callee.scriptFunction(); ok {
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opCallUpvalueSelfAddKOne:
			callee := upvalues[ins.b].value
			desc := proto.selfCallAddOps[ins.d]
			firstSub := proto.constants[desc.firstSub]
			secondSub := proto.constants[desc.secondSub]
			var source Value
			if frame.directRegisters {
				source = frame.registers[ins.c]
			} else {
				source = frame.register(ins.c)
			}
			if thread.debugHook == nil &&
				callee.kind == FunctionKind &&
				callee.function != nil &&
				callee.function.proto == proto &&
				source.kind == NumberKind &&
				proto.constantNumberOK[desc.baseLess] &&
				proto.constantNumberOK[desc.firstSub] &&
				proto.constantNumberOK[desc.secondSub] {
				value, ok := numericSelfPairAdd(
					source.number,
					proto.constantNumbers[desc.baseLess],
					proto.constantNumbers[desc.firstSub],
					proto.constantNumbers[desc.secondSub],
				)
				if ok {
					frame.openCallStart = -1
					frame.openCallResults = nil
					if frame.directRegisters {
						frame.registers[ins.a] = NumberValue(value)
					} else {
						frame.setRegister(ins.a, NumberValue(value))
					}
					break
				}
			}

			firstArg, err := binaryArithmeticValue(
				source,
				firstSub,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
			}
			secondArg, err := binaryArithmeticValue(
				source,
				secondSub,
				globals,
				"__sub",
				"subtract",
				func(left float64, right float64) float64 { return left - right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: subtract failed: %w", err)
			}
			firstResults, err := callValue(callee, globals, []Value{firstArg})
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			secondResults, err := callValue(callee, globals, []Value{secondArg})
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			value, err := binaryArithmeticValue(
				adjustedResultAt(firstResults, 0),
				adjustedResultAt(secondResults, 0),
				globals,
				"__add",
				"add",
				func(left float64, right float64) float64 { return left + right },
			)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: add failed: %w", err)
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			if frame.directRegisters {
				frame.registers[ins.a] = value
			} else {
				frame.setRegister(ins.a, value)
			}

		case opCallMethodOne:
			var receiver Value
			if frame.directRegisters {
				receiver = frame.registers[ins.b]
			} else {
				receiver = frame.register(ins.b)
			}
			table, ok := receiver.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", receiver.Kind())
			}
			key := proto.constantKeys[ins.c].str
			var callee Value
			if value, ok := table.rawStringField(key); ok {
				callee = value
			} else if table.metatable == nil {
				callee = NilValue()
			} else {
				value, err := runtimeTableAccess(globals).get(table, proto.constants[ins.c])
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				callee = value
			}
			if thread.debugHook == nil &&
				ins.d == 1 &&
				callee.kind == FunctionKind &&
				callee.function != nil &&
				callee.function.proto != nil &&
				callee.function.proto.hasFastMethodFieldAdd {
				methodProto := callee.function.proto
				field := methodProto.constants[methodProto.fastMethodFieldAdd].str
				current, currentOK := table.rawStringField(field)
				var amount Value
				if frame.directRegisters {
					amount = frame.registers[ins.a+2]
				} else {
					amount = frame.register(ins.a + 2)
				}
				if currentOK && current.kind == NumberKind && amount.kind == NumberKind {
					value := NumberValue(current.number + amount.number)
					table.setRawStringField(field, value)
					frame.openCallStart = -1
					frame.openCallResults = nil
					if frame.directRegisters {
						frame.registers[ins.a] = value
					} else {
						frame.setRegister(ins.a, value)
					}
					break
				}
			}
			if frame.directRegisters {
				frame.registers[ins.a+1] = receiver
			} else {
				frame.setRegister(ins.a+1, receiver)
			}
			args := frame.scriptCallArgs(ins.a+1, ins.d+1)
			destination := vmResultDestination{register: ins.a, count: 1}
			if closure, ok := callee.scriptFunction(); ok {
				if frame.directRegisters {
					args = frame.registers[ins.a+1 : ins.a+2+ins.d]
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			if len(results) == 0 {
				if frame.directRegisters {
					frame.registers[ins.a] = NilValue()
				} else {
					frame.setRegister(ins.a, NilValue())
				}
				break
			}
			if frame.directRegisters {
				frame.registers[ins.a] = results[0]
			} else {
				frame.setRegister(ins.a, results[0])
			}

		case opCallTableFieldKeyOne:
			var handlerTableValue Value
			var keySourceValue Value
			argCount := tableFieldKeyCallArgCount(ins.d)
			keySource := ins.a + argCount + 1
			if frame.directRegisters {
				handlerTableValue = frame.registers[ins.b]
				keySourceValue = frame.registers[keySource]
			} else {
				handlerTableValue = frame.register(ins.b)
				keySourceValue = frame.register(keySource)
			}
			keySourceTable, ok := keySourceValue.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", keySourceValue.Kind())
			}
			keyField := proto.constantKeys[ins.c].str
			var keyValue Value
			if value, ok := keySourceTable.rawStringField(keyField); ok {
				keyValue = value
			} else if keySourceTable.metatable == nil {
				keyValue = NilValue()
			} else {
				value, err := runtimeTableAccess(globals).get(keySourceTable, proto.constants[ins.c])
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				keyValue = value
			}

			handlerTable, ok := handlerTableValue.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get index target is %s, want table", handlerTableValue.Kind())
			}
			var callee Value
			if keyValue.kind == StringKind {
				if value, ok := handlerTable.rawStringField(keyValue.str); ok {
					callee = value
				} else if handlerTable.metatable == nil {
					callee = NilValue()
				} else {
					value, err := runtimeTableAccess(globals).get(handlerTable, keyValue)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: get index failed: %w", err)
					}
					callee = value
				}
			} else {
				value, err := runtimeTableAccess(globals).get(handlerTable, keyValue)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get index failed: %w", err)
				}
				callee = value
			}

			var args []Value
			if frame.directRegisters {
				args = frame.registers[ins.a+1 : ins.a+1+argCount]
			} else {
				args = frame.scriptCallArgs(ins.a+1, argCount)
			}
			destination := vmResultDestination{register: ins.a, count: 1}
			if closure, ok := callee.scriptFunction(); ok {
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			if len(results) == 0 {
				if frame.directRegisters {
					frame.registers[ins.a] = NilValue()
				} else {
					frame.setRegister(ins.a, NilValue())
				}
				break
			}
			if frame.directRegisters {
				frame.registers[ins.a] = results[0]
			} else {
				frame.setRegister(ins.a, results[0])
			}

		case opCallOne:
			var callee Value
			if frame.directRegisters {
				callee = frame.registers[ins.b]
			} else {
				callee = frame.register(ins.b)
			}
			destination := vmResultDestination{register: ins.a, count: 1}
			if closure, ok := callee.scriptFunction(); ok {
				var args []Value
				if frame.directRegisters {
					args = frame.registers[ins.b+1 : ins.b+1+ins.c]
				} else {
					args = frame.scriptCallArgs(ins.b+1, ins.c)
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						frame.pendingCall = vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						}
						frame.hasPendingCall = true
					}
					return vmFrameResult{}, err
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}

			var args []Value
			if _, ok := callee.nativeFunction(); ok {
				args = frame.scriptCallArgs(ins.b+1, ins.c)
				if callee.nativeID == nativeFuncSelect && len(args) > 0 {
					if marker, ok := args[0].String(); ok && marker == "#" {
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = NumberValue(float64(len(args) - 1))
						} else {
							frame.setRegister(ins.a, NumberValue(float64(len(args)-1)))
						}
						break
					}
				}
				if callee.nativeID == nativeFuncTableInsert {
					if _, err := baseTableInsert(args); err != nil {
						return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
					}
					frame.openCallStart = -1
					frame.openCallResults = nil
					if frame.directRegisters {
						frame.registers[ins.a] = NilValue()
					} else {
						frame.setRegister(ins.a, NilValue())
					}
					break
				}
				if callee.nativeID == nativeFuncTableRemove {
					removed, err := baseTableRemoveValue(args)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
					}
					frame.openCallStart = -1
					frame.openCallResults = nil
					if frame.directRegisters {
						frame.registers[ins.a] = removed
					} else {
						frame.setRegister(ins.a, removed)
					}
					break
				}
				if callee.nativeID == nativeFuncCoroutineStatus {
					status, err := baseCoroutineStatusValue(args)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
					}
					frame.openCallStart = -1
					frame.openCallResults = nil
					if frame.directRegisters {
						frame.registers[ins.a] = status
					} else {
						frame.setRegister(ins.a, status)
					}
					break
				}
				if callee.nativeID == nativeFuncRawLen {
					length, err := baseRawLenValue(args)
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
					}
					frame.openCallStart = -1
					frame.openCallResults = nil
					if frame.directRegisters {
						frame.registers[ins.a] = length
					} else {
						frame.setRegister(ins.a, length)
					}
					break
				}
			} else {
				args = frame.copiedCallArgs(ins.b+1, ins.c)
			}

			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			if len(results) == 0 {
				if frame.directRegisters {
					frame.registers[ins.a] = NilValue()
				} else {
					frame.setRegister(ins.a, NilValue())
				}
				break
			}
			if frame.directRegisters {
				frame.registers[ins.a] = results[0]
			} else {
				frame.setRegister(ins.a, results[0])
			}

		case opCall:
			var callee Value
			if frame.directRegisters {
				callee = frame.registers[ins.b]
			} else {
				callee = frame.register(ins.b)
			}
			resultCount := ins.d
			if resultCount == 0 {
				resultCount = 1
			}

			var args []Value
			if ins.c < 0 {
				prefixCount := -ins.c - 1
				openArgStart := ins.b + 1 + prefixCount
				if frame.openCallStart != openArgStart {
					return vmFrameResult{}, fmt.Errorf("run: call open argument missing results")
				}
				if callee.nativeID == nativeFuncSelect && resultCount == 1 && prefixCount > 0 {
					markerValue := frame.register(ins.b + 1)
					if frame.directRegisters {
						markerValue = frame.registers[ins.b+1]
					}
					if marker, ok := markerValue.String(); ok && marker == "#" {
						count := prefixCount + len(frame.openCallResults) - 1
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = NumberValue(float64(count))
						} else {
							frame.setRegister(ins.a, NumberValue(float64(count)))
						}
						frame.pc++
						continue
					}
				}
				args = make([]Value, 0, prefixCount+len(frame.openCallResults))
				for i := 0; i < prefixCount; i++ {
					args = append(args, frame.register(ins.b+1+i))
				}
				args = append(args, frame.openCallResults...)
				if callee.nativeID == nativeFuncSelect && resultCount == 1 && len(args) > 0 {
					if marker, ok := args[0].String(); ok && marker == "#" {
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = NumberValue(float64(len(args) - 1))
						} else {
							frame.setRegister(ins.a, NumberValue(float64(len(args)-1)))
						}
						frame.pc++
						continue
					}
				}
			} else {
				if closure, ok := callee.scriptFunction(); ok {
					args = frame.scriptCallArgs(ins.b+1, ins.c)
					destination := vmResultDestination{
						register: ins.a,
						count:    resultCount,
					}
					frame.pc++
					if resultCount == 1 {
						value, err := thread.runInlineScriptCallOneNoHook(closure, args)
						if err != nil {
							if yield, ok := err.(vmYieldRequest); ok {
								frame.pendingCall = vmPendingCall{
									destination: destination,
									protected:   yield.protected,
									host:        yield.host,
								}
								frame.hasPendingCall = true
							}
							return vmFrameResult{}, err
						}
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = value
						} else {
							frame.setRegister(ins.a, value)
						}
						continue
					}
					result, err := thread.runInlineScriptCall(closure, args)
					if err != nil {
						if yield, ok := err.(vmYieldRequest); ok {
							frame.pendingCall = vmPendingCall{
								destination: destination,
								protected:   yield.protected,
								host:        yield.host,
							}
							frame.hasPendingCall = true
						}
						return vmFrameResult{}, err
					}
					frame.applyFrameResultDestination(destination, result)
					continue
				} else if _, ok := callee.nativeFunction(); ok {
					if callee.nativeID == nativeFuncArrayNext && resultCount == 2 && ins.c == 2 {
						var tableValue Value
						var controlValue Value
						if frame.directRegisters {
							tableValue = frame.registers[ins.b+1]
							controlValue = frame.registers[ins.b+2]
						} else {
							tableValue = frame.register(ins.b + 1)
							controlValue = frame.register(ins.b + 2)
						}
						results, count, err := baseArrayNextInline(tableValue, controlValue)
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
						}
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							for i := 0; i < resultCount; i++ {
								if i >= count {
									frame.registers[ins.a+i] = NilValue()
								} else {
									frame.registers[ins.a+i] = results[i]
								}
							}
						} else {
							frame.applyInlineResultDestination(vmResultDestination{register: ins.a, count: resultCount}, results, count)
						}
						break
					}
					args = frame.scriptCallArgs(ins.b+1, ins.c)
					if callee.nativeID == nativeFuncSelect && resultCount == 1 && len(args) > 0 {
						if marker, ok := args[0].String(); ok && marker == "#" {
							frame.openCallStart = -1
							frame.openCallResults = nil
							if frame.directRegisters {
								frame.registers[ins.a] = NumberValue(float64(len(args) - 1))
							} else {
								frame.setRegister(ins.a, NumberValue(float64(len(args)-1)))
							}
							break
						}
					}
					if callee.nativeID == nativeFuncTableInsert && resultCount == 1 {
						if _, err := baseTableInsert(args); err != nil {
							return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
						}
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = NilValue()
						} else {
							frame.setRegister(ins.a, NilValue())
						}
						break
					}
					if callee.nativeID == nativeFuncTableRemove && resultCount == 1 {
						removed, err := baseTableRemoveValue(args)
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
						}
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = removed
						} else {
							frame.setRegister(ins.a, removed)
						}
						break
					}
					if callee.nativeID == nativeFuncCoroutineStatus && resultCount == 1 {
						status, err := baseCoroutineStatusValue(args)
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
						}
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = status
						} else {
							frame.setRegister(ins.a, status)
						}
						break
					}
					if callee.nativeID == nativeFuncRawLen && resultCount == 1 {
						length, err := baseRawLenValue(args)
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
						}
						frame.openCallStart = -1
						frame.openCallResults = nil
						if frame.directRegisters {
							frame.registers[ins.a] = length
						} else {
							frame.setRegister(ins.a, length)
						}
						break
					}
				} else {
					args = frame.copiedCallArgs(ins.b+1, ins.c)
				}
			}

			if closure, ok := callee.scriptFunction(); ok {
				frame.pendingCall = vmPendingCall{
					destination: vmResultDestination{
						register: ins.a,
						count:    resultCount,
					},
				}
				frame.hasPendingCall = true
				frame.pc++
				return vmFrameResult{
					state: vmCallStateScriptCall,
					scriptCall: vmScriptCall{
						closure: closure,
						args:    args,
					},
				}, nil
			}

			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					frame.pendingCall = vmPendingCall{
						destination: vmResultDestination{
							register: ins.a,
							count:    resultCount,
						},
						protected: yield.protected,
						host:      yield.host,
					}
					frame.hasPendingCall = true
					frame.pc++
					return vmFrameResult{state: vmCallStateYielded, results: yield.values}, nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			if resultCount < 0 {
				frame.openCallStart = ins.a
				frame.openCallResults = adjustedCallResults(results)
				if len(frame.openCallResults) == 0 {
					frame.setRegister(ins.a, NilValue())
				} else {
					frame.setRegister(ins.a, frame.openCallResults[0])
				}
				frame.pc++
				continue
			}

			frame.openCallStart = -1
			frame.openCallResults = nil
			for i := 0; i < resultCount; i++ {
				if i >= len(results) {
					frame.setRegister(ins.a+i, NilValue())
				} else {
					frame.setRegister(ins.a+i, results[i])
				}
			}
			if len(results) == 0 && resultCount == 1 {
				frame.setRegister(ins.a, NilValue())
			}

		case opJumpIfFalse:
			if frame.directRegisters {
				if !frame.registers[ins.a].truthy() {
					frame.pc = ins.b
					continue
				}
				break
			}
			if !frame.register(ins.a).truthy() {
				frame.pc = ins.b
				continue
			}

		case opJumpIfStringFieldFalse:
			key := proto.constantKeys[ins.b].str
			var base Value
			if frame.directRegisters {
				base = frame.registers[ins.a]
			} else {
				base = frame.register(ins.a)
			}
			if base.kind != TableKind || base.table == nil {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			table := base.table
			var value Value
			if table.metatable == nil && ins.c >= 0 {
				if table.stringFieldMap == nil && ins.c < len(table.stringFields) && table.stringFields[ins.c].key == key {
					value = table.stringFields[ins.c].value
				} else {
					value = NilValue()
				}
			} else if field, ok := table.rawStringField(key); ok {
				value = field
			} else if table.metatable == nil {
				value = NilValue()
			} else {
				field, err := runtimeTableAccess(globals).get(table, proto.constants[ins.b])
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				value = field
			}
			if !value.truthy() {
				frame.pc = ins.d
				continue
			}

		case opJump:
			frame.pc = ins.b
			continue

		case opReturnOne:
			if frame.directRegisters {
				return vmReturnedValue(frame.registers[ins.a]), nil
			}
			return vmReturnedValue(frame.register(ins.a)), nil

		case opReturn:
			count := ins.b
			if count < 0 {
				prefixCount := -count - 1
				if frame.openCallStart == ins.a+prefixCount {
					results := make([]Value, 0, prefixCount+len(frame.openCallResults))
					for i := 0; i < prefixCount; i++ {
						results = append(results, frame.register(ins.a+i))
					}
					results = append(results, frame.openCallResults...)
					return vmReturnedValues(results), nil
				}
				return vmReturnedValue(frame.register(ins.a)), nil
			}
			if count == 0 {
				return vmReturnedValues(nil), nil
			}
			if count == 1 {
				if frame.directRegisters {
					return vmReturnedValue(frame.registers[ins.a]), nil
				}
				return vmReturnedValue(frame.register(ins.a)), nil
			}
			results := make([]Value, count)
			if frame.directRegisters {
				copy(results, frame.registers[ins.a:ins.a+count])
			} else {
				for i := range results {
					results[i] = frame.register(ins.a + i)
				}
			}
			return vmReturnedValues(results), nil

		default:
			return vmFrameResult{}, fmt.Errorf("run: unknown opcode %d", ins.op)
		}
		frame.pc++
	}

	return vmFrameResult{}, fmt.Errorf("run: prototype did not return")
}

func (frame *vmFrame) scriptCallArgs(start int, count int) []Value {
	if count == 0 {
		return nil
	}
	if !frame.hasCellsInRange(start, count) {
		return frame.registers[start : start+count]
	}
	return frame.copiedCallArgs(start, count)
}

func (frame *vmFrame) copiedCallArgs(start int, count int) []Value {
	args := make([]Value, count)
	for i := range args {
		args[i] = frame.register(start + i)
	}
	return args
}

func (frame *vmFrame) hasCellsInRange(start int, count int) bool {
	if len(frame.cells) == 0 {
		return false
	}
	for i := 0; i < count; i++ {
		index := start + i
		if index < len(frame.cells) && frame.cells[index] != nil {
			return true
		}
	}
	return false
}

func (thread *vmThread) consumeInstruction() bool {
	if thread.instructionBudget < 0 {
		return true
	}
	if thread.instructionBudget == 0 {
		return false
	}
	thread.instructionBudget--
	return true
}

func (thread *vmThread) runDebugCountHook(frame *vmFrame) error {
	if thread.debugHook == nil || thread.debugCountInterval <= 0 {
		return nil
	}
	thread.debugInstructionCount++
	if thread.debugInstructionCount%thread.debugCountInterval != 0 {
		return nil
	}
	return thread.runDebugHook(vmDebugEvent{
		kind:  vmDebugEventCount,
		frame: frame,
		pc:    frame.pc,
		line:  frame.debugLine,
	})
}

func (thread *vmThread) runDebugLineHook(frame *vmFrame) error {
	if thread.debugHook == nil || !thread.debugLineHook {
		return nil
	}
	line := frame.protoLine(frame.pc)
	if line <= 0 || line == frame.debugLine {
		return nil
	}
	frame.debugLine = line
	return thread.runDebugHook(vmDebugEvent{
		kind:  vmDebugEventLine,
		frame: frame,
		pc:    frame.pc,
		line:  line,
	})
}

func (thread *vmThread) runDebugCallHook(frame *vmFrame) error {
	if thread.debugHook == nil || !thread.debugCallHook {
		return nil
	}
	return thread.runDebugHook(vmDebugEvent{
		kind:  vmDebugEventCall,
		frame: frame,
		pc:    frame.pc,
		line:  frame.protoLine(frame.pc),
	})
}

func (thread *vmThread) runDebugReturnHook(frame *vmFrame) error {
	if thread.debugHook == nil || !thread.debugReturnHook {
		return nil
	}
	return thread.runDebugHook(vmDebugEvent{
		kind:  vmDebugEventReturn,
		frame: frame,
		pc:    frame.pc,
		line:  frame.debugLine,
	})
}

func (thread *vmThread) runDebugHook(event vmDebugEvent) error {
	restore := thread.enterNonYieldable()
	defer restore()
	err := thread.debugHook(thread.globals, event)
	if err == nil {
		return nil
	}
	if _, ok := err.(vmYieldRequest); ok {
		return fmt.Errorf("debug hook: coroutine yield is not allowed")
	}
	return err
}

func (frame *vmFrame) protoLine(pc int) int {
	if frame == nil || frame.proto == nil || pc < 0 || pc >= len(frame.proto.lines) {
		return -1
	}
	return frame.proto.lines[pc]
}

func adjustedCallResults(results []Value) []Value {
	if len(results) == 0 {
		return []Value{NilValue()}
	}
	return results
}

func prepareIterator(value Value, globals *globalEnv) (Value, Value, Value, bool, error) {
	table, ok := value.Table()
	if !ok {
		return NilValue(), NilValue(), NilValue(), false, nil
	}

	if table.metatable != nil {
		metamethod, err := table.metatable.rawGet(StringValue("__iter"))
		if err != nil {
			return NilValue(), NilValue(), NilValue(), false, err
		}
		if !metamethod.IsNil() {
			results, err := callRuntimeMetamethod(metamethod, globals, []Value{value})
			if err != nil {
				return NilValue(), NilValue(), NilValue(), false, err
			}
			return adjustedResultAt(results, 0), adjustedResultAt(results, 1), adjustedResultAt(results, 2), true, nil
		}
	}

	if tableCanIterateCleanArray(table) {
		return nativeFuncValueWithID(baseArrayNextNative, nativeFuncArrayNext), TableValue(table), NilValue(), true, nil
	}
	return HostFuncValue(baseNext), TableValue(table), NilValue(), true, nil
}

func getStringField2(access tableAccess, table *Table, firstKey string, firstKeyValue Value, secondKey string, secondKeyValue Value) (Value, error) {
	first, err := access.getString(table, firstKey, firstKeyValue)
	if err != nil {
		return NilValue(), err
	}
	nextTable, ok := first.Table()
	if !ok {
		return NilValue(), fmt.Errorf("get field target is %s, want table", first.Kind())
	}
	return access.getString(nextTable, secondKey, secondKeyValue)
}

func setStringField2(access tableAccess, table *Table, firstKey string, firstKeyValue Value, secondKey string, secondKeyValue Value, value Value) error {
	first, err := access.getString(table, firstKey, firstKeyValue)
	if err != nil {
		return err
	}
	nextTable, ok := first.Table()
	if !ok {
		return fmt.Errorf("set field target is %s, want table", first.Kind())
	}
	return access.set(nextTable, secondKeyValue, value)
}

func adjustedResultAt(results []Value, index int) Value {
	if index < len(results) {
		return results[index]
	}
	return NilValue()
}

func numericSelfPairAdd(value float64, baseLess float64, firstSub float64, secondSub float64) (float64, bool) {
	if math.IsNaN(value) ||
		math.IsNaN(baseLess) ||
		math.IsNaN(firstSub) ||
		math.IsNaN(secondSub) ||
		firstSub <= 0 ||
		secondSub <= 0 {
		return 0, false
	}
	if value < baseLess {
		return value, true
	}
	first, ok := numericSelfPairAdd(value-firstSub, baseLess, firstSub, secondSub)
	if !ok {
		return 0, false
	}
	second, ok := numericSelfPairAdd(value-secondSub, baseLess, firstSub, secondSub)
	if !ok {
		return 0, false
	}
	return first + second, true
}

func baseArrayNextNative(_ *globalEnv, args []Value) ([]Value, error) {
	table, err := tableArg("array iterator", args, 0)
	if err != nil {
		return nil, err
	}
	var index int
	if len(args) > 1 && !args[1].IsNil() {
		number, ok := args[1].Number()
		if !ok {
			return nil, fmt.Errorf("array iterator: index is %s, want number or nil", args[1].Kind())
		}
		index = int(number)
		if float64(index) != number {
			return nil, fmt.Errorf("array iterator: index is %s, want integer", args[1].Kind())
		}
	}
	next := index + 1
	if next < 1 || next > len(table.array) {
		return []Value{NilValue()}, nil
	}
	return []Value{NumberValue(float64(next)), table.array[next-1]}, nil
}

func baseArrayNextInline(tableValue Value, controlValue Value) ([2]Value, int, error) {
	table, ok := tableValue.Table()
	if !ok {
		return [2]Value{}, 0, fmt.Errorf("array iterator: argument #1 is %s, want table", tableValue.Kind())
	}
	var index int
	if !controlValue.IsNil() {
		number, ok := controlValue.Number()
		if !ok {
			return [2]Value{}, 0, fmt.Errorf("array iterator: index is %s, want number or nil", controlValue.Kind())
		}
		index = int(number)
		if float64(index) != number {
			return [2]Value{}, 0, fmt.Errorf("array iterator: index is %s, want integer", controlValue.Kind())
		}
	}
	next := index + 1
	if next < 1 || next > len(table.array) {
		return [2]Value{NilValue()}, 1, nil
	}
	return [2]Value{NumberValue(float64(next)), table.array[next-1]}, 2, nil
}

func callableValue(value Value) bool {
	if _, ok := value.nativeFunction(); ok {
		return true
	}
	if _, ok := value.yieldableHostFunction(); ok {
		return true
	}
	if _, ok := value.hostFunction(); ok {
		return true
	}
	if _, ok := value.scriptFunction(); ok {
		return true
	}
	return false
}

func lengthValue(value Value, globals *globalEnv) (Value, error) {
	if table, ok := value.Table(); ok && table.metatable != nil {
		metamethod, err := table.metatable.rawGet(StringValue("__len"))
		if err != nil {
			return NilValue(), err
		}
		if !metamethod.IsNil() {
			results, err := callRuntimeMetamethod(metamethod, globals, []Value{value})
			if err != nil {
				return NilValue(), err
			}
			result := NilValue()
			if len(results) > 0 {
				result = results[0]
			}
			if _, ok := result.Number(); !ok {
				return NilValue(), fmt.Errorf("__len returned %s, want number", result.Kind())
			}
			return result, nil
		}
	}

	length, err := rawLength(value)
	if err != nil {
		return NilValue(), err
	}
	return NumberValue(float64(length)), nil
}

func negateValue(value Value, globals *globalEnv) (Value, error) {
	number, err := numericOperand(value, "", "negate")
	if err == nil {
		return NumberValue(-number), nil
	}
	if result, ok, metamethodErr := callUnaryMetamethod("__unm", value, globals); ok || metamethodErr != nil {
		return result, metamethodErr
	}
	return NilValue(), err
}

func binaryArithmeticValue(
	left Value,
	right Value,
	globals *globalEnv,
	metafield string,
	operator string,
	primitive func(float64, float64) float64,
) (Value, error) {
	leftNumber, leftErr := numericOperand(left, "left", operator)
	rightNumber, rightErr := numericOperand(right, "right", operator)
	if leftErr == nil && rightErr == nil {
		return NumberValue(primitive(leftNumber, rightNumber)), nil
	}
	if value, ok, err := callBinaryMetamethod(metafield, left, right, globals); ok || err != nil {
		return value, err
	}
	if leftErr != nil {
		return NilValue(), leftErr
	}
	return NilValue(), rightErr
}

func concatValue(left Value, right Value, globals *globalEnv) (Value, error) {
	text, err := valuesConcat(left, right)
	if err == nil {
		return StringValue(text), nil
	}
	if value, ok, metamethodErr := callBinaryMetamethod("__concat", left, right, globals); ok || metamethodErr != nil {
		return value, metamethodErr
	}
	return NilValue(), err
}

func lessValue(left Value, right Value, globals *globalEnv) (bool, error) {
	value, err := valuesLess(left, right)
	if err == nil {
		return value, nil
	}
	if result, ok, metamethodErr := callComparisonMetamethod("__lt", left, right, globals); ok || metamethodErr != nil {
		return result, metamethodErr
	}
	return false, err
}

func lessEqualValue(left Value, right Value, globals *globalEnv) (bool, error) {
	value, err := valuesLessEqual(left, right)
	if err == nil {
		return value, nil
	}
	if result, ok, metamethodErr := callComparisonMetamethod("__le", left, right, globals); ok || metamethodErr != nil {
		return result, metamethodErr
	}
	return false, err
}

func equalValue(left Value, right Value, globals *globalEnv) (bool, error) {
	if _, leftTable := left.Table(); leftTable {
		if _, rightTable := right.Table(); rightTable {
			if result, ok, err := callEqualityMetamethod(left, right, globals); ok || err != nil {
				return result, err
			}
		}
	}
	return valuesEqual(left, right), nil
}

func callEqualityMetamethod(left Value, right Value, globals *globalEnv) (bool, bool, error) {
	value, ok, err := callBinaryMetamethod("__eq", left, right, globals)
	if err != nil || !ok {
		return false, ok, err
	}
	return value.truthy(), true, nil
}

func callComparisonMetamethod(name string, left Value, right Value, globals *globalEnv) (bool, bool, error) {
	value, ok, err := callBinaryMetamethod(name, left, right, globals)
	if err != nil || !ok {
		return false, ok, err
	}
	result, ok := value.Bool()
	if !ok {
		return false, true, fmt.Errorf("%s returned %s, want boolean", name, value.Kind())
	}
	return result, true, nil
}

func callUnaryMetamethod(name string, value Value, globals *globalEnv) (Value, bool, error) {
	metamethod, ok, err := valueMetamethod(value, name)
	if err != nil || !ok {
		return NilValue(), ok, err
	}
	callable, err := metamethodCallable(metamethod)
	if err != nil {
		return NilValue(), true, err
	}
	if !callable {
		return NilValue(), true, fmt.Errorf("%s is %s, want function", name, metamethod.Kind())
	}
	results, err := callRuntimeMetamethod(metamethod, globals, []Value{value})
	if err != nil {
		return NilValue(), true, err
	}
	return adjustedResultAt(results, 0), true, nil
}

func callBinaryMetamethod(name string, left Value, right Value, globals *globalEnv) (Value, bool, error) {
	metamethod, ok, err := binaryMetamethod(name, left, right)
	if err != nil || !ok {
		return NilValue(), ok, err
	}
	callable, err := metamethodCallable(metamethod)
	if err != nil {
		return NilValue(), true, err
	}
	if !callable {
		return NilValue(), true, fmt.Errorf("%s is %s, want function", name, metamethod.Kind())
	}
	results, err := callRuntimeMetamethod(metamethod, globals, []Value{left, right})
	if err != nil {
		return NilValue(), true, err
	}
	return adjustedResultAt(results, 0), true, nil
}

func binaryMetamethod(name string, left Value, right Value) (Value, bool, error) {
	if metamethod, ok, err := valueMetamethod(left, name); err != nil || ok {
		return metamethod, ok, err
	}
	return valueMetamethod(right, name)
}

func valueMetamethod(value Value, name string) (Value, bool, error) {
	table, ok := value.Table()
	if !ok || table.metatable == nil {
		return NilValue(), false, nil
	}
	metamethod, err := table.metatable.rawGet(StringValue(name))
	if err != nil {
		return NilValue(), false, err
	}
	return metamethod, !metamethod.IsNil(), nil
}

func metamethodCallable(value Value) (bool, error) {
	if callableValue(value) {
		return true, nil
	}
	return hasCallMetamethod(value)
}

func callValue(fn Value, globals *globalEnv, args []Value) ([]Value, error) {
	return callValueSeen(fn, globals, args, nil, false)
}

func callValueWithContext(ctx context.Context, fn Value, globals *globalEnv, args []Value) ([]Value, error) {
	return callValueWithContextBudget(ctx, fn, globals, args, -1)
}

func callValueWithContextBudget(ctx context.Context, fn Value, globals *globalEnv, args []Value, maxInstructions int) ([]Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if globals != nil && globals.thread != nil {
		return callValue(fn, globals, args)
	}
	if closure, ok := fn.scriptFunction(); ok {
		return executeProto(ctx, closure.proto, globals, executeOptions{
			args:            args,
			upvalues:        closure.upvalues,
			maxInstructions: maxInstructions,
		})
	}
	return callValue(fn, globals, args)
}

func contextFromGlobalEnv(globals *globalEnv) context.Context {
	if globals != nil && globals.thread != nil && globals.thread.ctx != nil {
		return globals.thread.ctx
	}
	return context.Background()
}

func protectedCallValue(fn Value, globals *globalEnv, args []Value) ([]Value, error) {
	return callValueSeen(fn, globals, args, nil, true)
}

func callRuntimeMetamethod(fn Value, globals *globalEnv, args []Value) ([]Value, error) {
	return callRuntimeMetamethodSeen(fn, globals, args, nil, false)
}

func tableIntrinsicCallee(globals *globalEnv, field string) (Value, bool, error) {
	return baseFieldIntrinsicCallee(globals, "table", field)
}

func coroutineIntrinsicCallee(globals *globalEnv, field string) (Value, bool, error) {
	return baseFieldIntrinsicCallee(globals, "coroutine", field)
}

func mathIntrinsicCallee(globals *globalEnv, field string) (Value, bool, error) {
	return baseFieldIntrinsicCallee(globals, "math", field)
}

func baseFieldIntrinsicCallee(globals *globalEnv, globalName string, field string) (Value, bool, error) {
	intrinsic, ok := baseFieldIntrinsic(globalName, field)
	if !ok {
		return NilValue(), false, fmt.Errorf("run: unknown intrinsic %s.%s", globalName, field)
	}
	if globals == nil || globals.values == nil {
		return Value{kind: HostFuncKind, nativeID: intrinsic.nativeID}, true, nil
	}
	tableValue, ok := globals.values[globalName]
	if !ok {
		return Value{kind: HostFuncKind, nativeID: intrinsic.nativeID}, true, nil
	}
	table, ok := tableValue.Table()
	if !ok {
		return NilValue(), false, fmt.Errorf("run: get field target is %s, want table", tableValue.Kind())
	}
	callee, err := runtimeTableAccess(globals).get(table, StringValue(field))
	if err != nil {
		return NilValue(), false, fmt.Errorf("run: get field failed: %w", err)
	}
	return callee, callee.nativeID == intrinsic.nativeID, nil
}

func callRuntimeMetamethodSeen(
	fn Value,
	globals *globalEnv,
	args []Value,
	seen map[*Table]bool,
	protected bool,
) ([]Value, error) {
	if globals == nil || globals.thread == nil {
		return callValueSeen(fn, globals, args, seen, protected)
	}
	restore := globals.thread.enterNonYieldable()
	defer restore()
	return callValueSeen(fn, globals, args, seen, protected)
}

func callValueSeen(fn Value, globals *globalEnv, args []Value, seen map[*Table]bool, protected bool) ([]Value, error) {
	if native, ok := fn.nativeFunction(); ok {
		results, err := native(globals, args)
		if err != nil {
			if _, ok := err.(vmYieldRequest); ok {
				return nil, err
			}
			if isVMHostInterrupt(err) {
				return nil, err
			}
			return nil, fmt.Errorf("host function failed: %w", err)
		}
		return results, nil
	}

	if host, ok := fn.yieldableHostFunction(); ok {
		if host == nil {
			return nil, fmt.Errorf("call target is nil host_function")
		}
		return finishHostCallResult(host(globals, args))
	}

	if host, ok := fn.hostFunction(); ok {
		if host == nil {
			return nil, fmt.Errorf("call target is nil host_function")
		}
		results, err := host(args)
		if err != nil {
			return nil, fmt.Errorf("host function failed: %w", err)
		}
		return results, nil
	}

	if closure, ok := fn.scriptFunction(); ok {
		if globals != nil && globals.thread != nil {
			if protected {
				return globals.thread.runScriptProtected(closure.proto, args, closure.upvalues)
			}
			return globals.thread.runScript(closure.proto, args, closure.upvalues)
		}
		return executeProto(context.Background(), closure.proto, globals, executeOptions{
			args:            args,
			upvalues:        closure.upvalues,
			maxInstructions: -1,
		})
	}

	if table, ok := fn.Table(); ok && table.metatable != nil {
		if seen[table] {
			return nil, fmt.Errorf("cyclic __call chain")
		}
		if seen == nil {
			seen = make(map[*Table]bool)
		}
		seen[table] = true
		metamethod, err := table.metatable.rawGet(StringValue("__call"))
		if err != nil {
			return nil, err
		}
		if !metamethod.IsNil() {
			if !callableValue(metamethod) {
				hasCall, err := hasCallMetamethod(metamethod)
				if err != nil {
					return nil, err
				}
				if !hasCall {
					return nil, fmt.Errorf("__call is %s, want function", metamethod.Kind())
				}
			}
			callArgs := make([]Value, 0, len(args)+1)
			callArgs = append(callArgs, fn)
			callArgs = append(callArgs, args...)
			return callRuntimeMetamethodSeen(metamethod, globals, callArgs, seen, protected)
		}
	}

	return nil, fmt.Errorf("call target is %s, want function", fn.Kind())
}

func finishHostCallResult(result vmHostCallResult) ([]Value, error) {
	if result.interrupt {
		return nil, vmHostInterrupt{}
	}
	if result.err != nil {
		return nil, fmt.Errorf("host function failed: %w", result.err)
	}
	if result.yield != nil {
		if result.yield.continuation == nil {
			return nil, fmt.Errorf("host function failed: missing yield continuation")
		}
		return nil, vmYieldRequest{
			values: result.yield.values,
			host: &vmPendingHostCall{
				continuation: result.yield.continuation,
			},
		}
	}
	return result.values, nil
}

func hasCallMetamethod(value Value) (bool, error) {
	table, ok := value.Table()
	if !ok || table.metatable == nil {
		return false, nil
	}
	metamethod, err := table.metatable.rawGet(StringValue("__call"))
	if err != nil {
		return false, err
	}
	return !metamethod.IsNil(), nil
}

func captureUpvalues(proto *Proto, frame *vmFrame) []*cell {
	if len(proto.upvalues) == 0 {
		return nil
	}

	captured := make([]*cell, len(proto.upvalues))
	for i, desc := range proto.upvalues {
		if desc.local {
			captured[i] = frame.registerCell(desc.index)
			continue
		}

		captured[i] = frame.upvalues[desc.index]
	}
	return captured
}
