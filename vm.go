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
	directFramePICCounts    *directFramePICCounts
	directFramePCCounts     map[*Proto][]uint64
	intrinsicGuards         *baseFieldIntrinsicGuardCache
	runtimePaths            [8]runtimePathCacheEntry
	runtimePathCount        uint8
	runtimePathHits         uint64
	runtimePathStores       uint64
	directLeafRegisters     []Value
	directLeafBusy          bool
}

type directFrameOpcodeCounts [256]uint64

type directFramePICCounts struct {
	monomorphicHits                uint64
	polymorphicHits                uint64
	keyMisses                      uint64
	shapeMisses                    uint64
	metatableMisses                uint64
	missingKeyFallbacks            uint64
	nilWriteFallbacks              uint64
	invalidKeyFallbacks            uint64
	numericArrayIndexHits          uint64
	sideExits                      [directFrameSideExitReasonCount]uint64
	directBlockEntries             uint64
	directBlockResumes             uint64
	directBlockFallbacks           uint64
	directBlockSideExits           [directFrameSideExitReasonCount]uint64
	pathCacheHits                  uint64
	pathCacheMisses                uint64
	pathCacheStale                 uint64
	pathCacheStores                uint64
	intrinsicGuardChecks           uint64
	intrinsicGuardHits             uint64
	intrinsicGuardMisses           uint64
	fixedCallFrameReuses           uint64
	fixedCallFrameMaterializations uint64
	fixedCallArgCopies             uint64
	fixedCallRegisterCopies        uint64
}

type directFrameSideExitReason uint8

const (
	directFrameSideExitReasonNone directFrameSideExitReason = iota
	directFrameSideExitReasonGenericFrame
	directFrameSideExitReasonTable
	directFrameSideExitReasonIntrinsic
	directFrameSideExitReasonCall
	directFrameSideExitReasonMetatable
	directFrameSideExitReasonDebug
	directFrameSideExitReasonBudget
	directFrameSideExitReasonYield
	directFrameSideExitReasonError
	directFrameSideExitReasonCount
)

type baseFieldIntrinsicGuardKey struct {
	globalName string
	field      string
}

type baseFieldIntrinsicGuardCache struct {
	entries     [6]baseFieldIntrinsicGuardEntry
	count       uint8
	hits        uint64
	resolutions uint64
	paths       [8]runtimePathCacheEntry
	pathCount   uint8
	pathHits    uint64
	pathStores  uint64
}

type baseFieldIntrinsicGuardEntry struct {
	key        baseFieldIntrinsicGuardKey
	envVersion uint64
	table      *Table
	token      tableStringShapeToken
	callee     Value
}

type runtimePathCacheEntry struct {
	pc         int
	dynamic    bool
	base       *Table
	firstKey   string
	firstSlot  tableStringFieldSlot
	child      *Table
	secondKey  string
	secondSlot tableStringFieldSlot
}

type runtimePathCacheHit struct {
	child      *Table
	secondSlot tableStringFieldSlot
	value      Value
}

func (thread *vmThread) runtimePathPlanCacheEnabled() bool {
	return thread != nil
}

func (counts *directFramePICCounts) addHit(entryIndex int) {
	if counts == nil {
		return
	}
	if entryIndex == 0 {
		counts.monomorphicHits++
		return
	}
	counts.polymorphicHits++
}

func (counts *directFramePICCounts) addKeyMiss() {
	if counts == nil {
		return
	}
	counts.keyMisses++
}

func (counts *directFramePICCounts) addShapeMiss() {
	if counts == nil {
		return
	}
	counts.shapeMisses++
}

func (counts *directFramePICCounts) addMetatableMiss() {
	if counts == nil {
		return
	}
	counts.metatableMisses++
}

func (counts *directFramePICCounts) addMissingKeyFallback() {
	if counts == nil {
		return
	}
	counts.missingKeyFallbacks++
}

func (counts *directFramePICCounts) addNilWriteFallback() {
	if counts == nil {
		return
	}
	counts.nilWriteFallbacks++
}

func (counts *directFramePICCounts) addInvalidKeyFallback() {
	if counts == nil {
		return
	}
	counts.invalidKeyFallbacks++
}

func (counts *directFramePICCounts) addNumericArrayIndexHit() {
	if counts == nil {
		return
	}
	counts.numericArrayIndexHits++
}

func (counts *directFramePICCounts) addSideExit(reason directFrameSideExitReason) {
	if counts == nil || reason <= directFrameSideExitReasonNone || reason >= directFrameSideExitReasonCount {
		return
	}
	counts.sideExits[reason]++
}

func (counts *directFramePICCounts) sideExitCount(reason directFrameSideExitReason) uint64 {
	if counts == nil || reason <= directFrameSideExitReasonNone || reason >= directFrameSideExitReasonCount {
		return 0
	}
	return counts.sideExits[reason]
}

func (counts *directFramePICCounts) addDirectBlockEntry() {
	if counts == nil {
		return
	}
	counts.directBlockEntries++
}

func (counts *directFramePICCounts) addDirectBlockResume() {
	if counts == nil {
		return
	}
	counts.directBlockResumes++
}

func (counts *directFramePICCounts) addDirectBlockFallback(reason directFrameSideExitReason) {
	if counts == nil {
		return
	}
	counts.directBlockFallbacks++
	if reason <= directFrameSideExitReasonNone || reason >= directFrameSideExitReasonCount {
		return
	}
	counts.directBlockSideExits[reason]++
}

func (counts *directFramePICCounts) directBlockSideExitCount(reason directFrameSideExitReason) uint64 {
	if counts == nil || reason <= directFrameSideExitReasonNone || reason >= directFrameSideExitReasonCount {
		return 0
	}
	return counts.directBlockSideExits[reason]
}

func (counts *directFramePICCounts) addPathCacheHit() {
	if counts == nil {
		return
	}
	counts.pathCacheHits++
}

func (counts *directFramePICCounts) addPathCacheMiss() {
	if counts == nil {
		return
	}
	counts.pathCacheMisses++
}

func (counts *directFramePICCounts) addPathCacheStale() {
	if counts == nil {
		return
	}
	counts.pathCacheStale++
}

func (counts *directFramePICCounts) addPathCacheStore() {
	if counts == nil {
		return
	}
	counts.pathCacheStores++
}

func (counts *directFramePICCounts) addIntrinsicGuardCheck() {
	if counts == nil {
		return
	}
	counts.intrinsicGuardChecks++
}

func (counts *directFramePICCounts) addIntrinsicGuardHit() {
	if counts == nil {
		return
	}
	counts.intrinsicGuardHits++
}

func (counts *directFramePICCounts) addIntrinsicGuardMiss() {
	if counts == nil {
		return
	}
	counts.intrinsicGuardMisses++
}

func (counts *directFramePICCounts) addFixedCallFrameReuse() {
	if counts == nil {
		return
	}
	counts.fixedCallFrameReuses++
}

func (counts *directFramePICCounts) addFixedCallFrameMaterialization() {
	if counts == nil {
		return
	}
	counts.fixedCallFrameMaterializations++
}

func (counts *directFramePICCounts) addFixedCallArgCopies(count int) {
	if counts == nil || count <= 0 {
		return
	}
	counts.fixedCallArgCopies += uint64(count)
}

func (counts *directFramePICCounts) addFixedCallRegisterCopies(count int) {
	if counts == nil || count <= 0 {
		return
	}
	counts.fixedCallRegisterCopies += uint64(count)
}

func (counts *directFramePICCounts) totalMechanismActivity() uint64 {
	if counts == nil {
		return 0
	}
	total := counts.monomorphicHits +
		counts.polymorphicHits +
		counts.keyMisses +
		counts.shapeMisses +
		counts.metatableMisses +
		counts.missingKeyFallbacks +
		counts.nilWriteFallbacks +
		counts.invalidKeyFallbacks +
		counts.numericArrayIndexHits +
		counts.directBlockEntries +
		counts.directBlockResumes +
		counts.directBlockFallbacks +
		counts.pathCacheHits +
		counts.pathCacheMisses +
		counts.pathCacheStale +
		counts.pathCacheStores +
		counts.intrinsicGuardChecks +
		counts.intrinsicGuardHits +
		counts.intrinsicGuardMisses +
		counts.fixedCallFrameReuses +
		counts.fixedCallFrameMaterializations +
		counts.fixedCallArgCopies +
		counts.fixedCallRegisterCopies
	for _, count := range counts.sideExits {
		total += count
	}
	for _, count := range counts.directBlockSideExits {
		total += count
	}
	return total
}

type directFrameOpcodeCount struct {
	op    opcode
	count uint64
}

type directFrameMechanismSnapshot struct {
	opcodeCounts directFrameOpcodeCounts
	picCounts    directFramePICCounts
	pcCounts     map[*Proto][]uint64
}

func (snapshot *directFrameMechanismSnapshot) opcodeCount(op opcode) uint64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.opcodeCounts.count(op)
}

func (snapshot *directFrameMechanismSnapshot) rankedOpcodes() []directFrameOpcodeCount {
	if snapshot == nil {
		return nil
	}
	return snapshot.opcodeCounts.ranked()
}

func (snapshot *directFrameMechanismSnapshot) pcCount(proto *Proto, pc int) uint64 {
	if snapshot == nil || proto == nil || pc < 0 {
		return 0
	}
	counts := snapshot.pcCounts[proto]
	if pc >= len(counts) {
		return 0
	}
	return counts[pc]
}

func runWithDirectFrameMechanismCounters(proto *Proto, globals map[string]Value) ([]Value, directFrameMechanismSnapshot, error) {
	var snapshot directFrameMechanismSnapshot
	if proto == nil {
		return nil, snapshot, fmt.Errorf("run: nil prototype")
	}
	if proto.verifyErr != nil {
		return nil, snapshot, fmt.Errorf("run: invalid prototype: %w", proto.verifyErr)
	}

	thread := newVMThreadWithContext(context.Background(), runtimeGlobals(globals))
	thread.instructionBudget = -1
	thread.directFrameOpcodeCounts = &snapshot.opcodeCounts
	thread.directFramePICCounts = &snapshot.picCounts
	snapshot.pcCounts = make(map[*Proto][]uint64)
	thread.directFramePCCounts = snapshot.pcCounts
	results, err := thread.run(proto, nil, nil)
	return results, snapshot, err
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
	indexCaches     []dynamicStringIndexCache
	tableCallCache  *tableFieldCallCache
}

type dynamicStringIndexCache struct {
	entries [4]dynamicStringIndexCacheEntry
	next    uint8
}

type dynamicStringIndexCacheEntry struct {
	table *Table
	key   string
	slot  tableStringFieldSlot
}

type tableFieldCallCache struct {
	entries [4]tableFieldCallCacheEntry
	next    uint8
}

type tableFieldCallCacheEntry struct {
	table   *Table
	key     string
	token   tableStringShapeToken
	closure *closure
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
	state      vmCallState
	valuesList vmValueList
	scriptCall vmScriptCall
}

type vmValueList struct {
	values      []Value
	inline      [2]Value
	count       int
	borrowed    bool
	usingInline bool
}

func vmEmptyValueList() vmValueList {
	return vmValueList{}
}

func vmInlineValueList(values ...Value) vmValueList {
	list := vmValueList{usingInline: true, count: len(values)}
	copy(list.inline[:], values)
	if list.count > len(list.inline) {
		list.values = append([]Value(nil), values...)
		list.usingInline = false
	}
	return list
}

func vmInlineArrayValueList(values [2]Value, count int) vmValueList {
	if count < 0 {
		count = 0
	}
	if count > len(values) {
		count = len(values)
	}
	return vmValueList{inline: values, count: count, usingInline: true}
}

func vmOwnedValueList(values []Value) vmValueList {
	return vmValueList{values: values, count: len(values)}
}

func vmBorrowedValueList(values []Value) vmValueList {
	return vmValueList{values: values, count: len(values), borrowed: true}
}

func (list vmValueList) len() int {
	return list.count
}

func (list vmValueList) at(index int) Value {
	if index < 0 || index >= list.count {
		return NilValue()
	}
	if list.usingInline {
		return list.inline[index]
	}
	return list.values[index]
}

func (list vmValueList) ownedValues() []Value {
	if list.count == 0 {
		return nil
	}
	values := make([]Value, list.count)
	if list.usingInline {
		copy(values, list.inline[:list.count])
		return values
	}
	copy(values, list.values[:list.count])
	return values
}

func (list vmValueList) retainedValues(reuse []Value) []Value {
	if list.count == 0 {
		return reuse[:0]
	}
	if !list.borrowed && !list.usingInline {
		return list.values[:list.count]
	}
	reuse = reuse[:0]
	if list.usingInline {
		reuse = append(reuse, list.inline[:list.count]...)
		return reuse
	}
	reuse = append(reuse, list.values[:list.count]...)
	return reuse
}

func (list vmValueList) adjustedRetainedValues(reuse []Value) []Value {
	if list.count == 0 {
		reuse = reuse[:0]
		reuse = append(reuse, NilValue())
		return reuse
	}
	return list.retainedValues(reuse)
}

func (list vmValueList) adjustedOwnedValues() []Value {
	if list.count == 0 {
		return []Value{NilValue()}
	}
	return list.ownedValues()
}

func (list vmValueList) ownedValuesWithPrefix(prefix Value) []Value {
	values := make([]Value, 0, list.count+1)
	values = append(values, prefix)
	if list.usingInline {
		values = append(values, list.inline[:list.count]...)
		return values
	}
	values = append(values, list.values[:list.count]...)
	return values
}

type directFrameSideExitKind uint8

const (
	directFrameSideExitResume directFrameSideExitKind = iota
	directFrameSideExitReturn
	directFrameSideExitCall
	directFrameSideExitYield
	directFrameSideExitGenericFrame
	directFrameSideExitFail
)

type directFrameSideExit struct {
	kind   directFrameSideExitKind
	reason directFrameSideExitReason
	result vmFrameResult
	err    error
}

func directFrameResume() directFrameSideExit {
	return directFrameSideExit{kind: directFrameSideExitResume}
}

func directFrameReturn(result vmFrameResult) directFrameSideExit {
	return directFrameSideExit{kind: directFrameSideExitReturn, result: result}
}

func directFrameCall(result vmFrameResult) directFrameSideExit {
	return directFrameSideExit{kind: directFrameSideExitCall, reason: directFrameSideExitReasonCall, result: result}
}

func directFrameYield(result vmFrameResult) directFrameSideExit {
	return directFrameSideExit{kind: directFrameSideExitYield, reason: directFrameSideExitReasonYield, result: result}
}

func directFrameEnterGenericFrame() directFrameSideExit {
	return directFrameEnterGenericFrameFor(directFrameSideExitReasonGenericFrame)
}

func directFrameEnterGenericFrameFor(reason directFrameSideExitReason) directFrameSideExit {
	return directFrameSideExit{kind: directFrameSideExitGenericFrame, reason: reason}
}

func directFrameFail(err error) directFrameSideExit {
	return directFrameSideExit{kind: directFrameSideExitFail, reason: directFrameSideExitReasonError, err: err}
}

func (exit directFrameSideExit) resumesDirectFrame() bool {
	return exit.kind == directFrameSideExitResume
}

func (exit directFrameSideExit) frameResult() (vmFrameResult, bool, error) {
	switch exit.kind {
	case directFrameSideExitResume, directFrameSideExitGenericFrame:
		return vmFrameResult{}, false, nil
	case directFrameSideExitReturn, directFrameSideExitCall, directFrameSideExitYield:
		return exit.result, true, nil
	case directFrameSideExitFail:
		return vmFrameResult{}, true, exit.err
	default:
		return vmFrameResult{}, true, fmt.Errorf("run: unknown direct-frame side exit %d", exit.kind)
	}
}

type vmYieldRequest struct {
	values    []Value
	protected *vmProtectedCall
	host      *vmPendingHostCall
}

func vmReturnedValues(values []Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateReturned, valuesList: vmOwnedValueList(values)}
}

func vmReturnedValue(value Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateReturned, valuesList: vmInlineValueList(value)}
}

func vmYieldedValues(values []Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateYielded, valuesList: vmOwnedValueList(values)}
}

func (result vmFrameResult) values() []Value {
	return result.valuesList.ownedValues()
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
			frame := thread.newCallFrame(call.closure.proto, call.args, call.closure.upvalues)
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
	calleeFrame := thread.newCallFrame(closure.proto, args, closure.upvalues)
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
		frame := thread.newCallFrame(call.closure.proto, call.args, call.closure.upvalues)
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

const directLeafCallRegisterLimit = 48

func (thread *vmThread) canRunDirectLeafScriptCallOne(closure *closure) bool {
	if closure == nil || closure.proto == nil {
		return false
	}
	proto := closure.proto
	if !proto.directLeafCallOne || len(closure.upvalues) != 0 {
		return false
	}
	if proto.registers > directLeafCallRegisterLimit || thread.directLeafBusy {
		return false
	}
	if !thread.canRunDirectFrame() || thread.hasProtectedCallBoundary() {
		return false
	}
	return true
}

func (thread *vmThread) hasProtectedCallBoundary() bool {
	for _, frame := range thread.frames {
		if frame != nil && frame.hasPendingCall && frame.pendingCall.protected != nil {
			return true
		}
	}
	return false
}

func (thread *vmThread) runDirectLeafScriptCallOne(closure *closure, args []Value) (Value, error) {
	proto := closure.proto
	thread.directFramePICCounts.addFixedCallFrameReuse()

	thread.directLeafBusy = true
	defer func() {
		thread.directLeafBusy = false
	}()

	if cap(thread.directLeafRegisters) < proto.registers {
		thread.directLeafRegisters = make([]Value, proto.registers)
	}
	registers := thread.directLeafRegisters[:proto.registers]
	for _, register := range proto.entryNilRegisters {
		registers[register] = NilValue()
	}
	paramCount := proto.params
	if paramCount > len(registers) {
		paramCount = len(registers)
	}
	copied := copy(registers[:paramCount], args)
	for i := copied; i < paramCount; i++ {
		registers[i] = NilValue()
	}
	thread.directFramePICCounts.addFixedCallArgCopies(copied)

	baseDepth := len(thread.frames)
	leaf := vmFrame{
		proto:           proto,
		registerCount:   len(registers),
		directRegisters: true,
		registers:       registers,
		pc:              0,
		debugLine:       -1,
		openCallStart:   -1,
	}

	exit := thread.runDirectFrame(&leaf)
	if exit.reason != directFrameSideExitReasonNone {
		thread.directFramePICCounts.addSideExit(exit.reason)
	}
	switch exit.kind {
	case directFrameSideExitReturn:
		return exit.result.valuesList.at(0), nil
	case directFrameSideExitGenericFrame, directFrameSideExitCall:
		return thread.continueDirectLeafFrameOne(&leaf, closure.upvalues, baseDepth)
	case directFrameSideExitFail:
		return NilValue(), exit.err
	case directFrameSideExitYield:
		return NilValue(), vmYieldRequest{values: exit.result.values()}
	case directFrameSideExitResume:
		return NilValue(), fmt.Errorf("run: direct leaf call resumed without return")
	default:
		return NilValue(), fmt.Errorf("run: unknown direct leaf side exit %d", exit.kind)
	}
}

func (thread *vmThread) continueDirectLeafFrameOne(leaf *vmFrame, upvalues []*cell, baseDepth int) (Value, error) {
	if leaf == nil || leaf.proto == nil {
		return NilValue(), fmt.Errorf("run: missing direct leaf frame")
	}
	thread.directFramePICCounts.addFixedCallFrameMaterialization()
	calleeFrame := thread.newFrame(leaf.proto, nil, upvalues)
	copy(calleeFrame.registers[:leaf.proto.registers], leaf.registers[:leaf.proto.registers])
	thread.directFramePICCounts.addFixedCallRegisterCopies(leaf.proto.registers)
	calleeFrame.pc = leaf.pc
	calleeFrame.openCallStart = leaf.openCallStart
	if len(leaf.openCallResults) != 0 {
		calleeFrame.openCallResults = append(calleeFrame.openCallResults[:0], leaf.openCallResults...)
	}
	thread.pushFrame(calleeFrame)
	result, err := thread.runUntilDepthResult(baseDepth)
	if err != nil {
		return NilValue(), err
	}
	return result.valuesList.at(0), nil
}

func (thread *vmThread) runInlineScriptCallOneNoHook(closure *closure, args []Value) (Value, error) {
	if thread.canRunDirectLeafScriptCallOne(closure) {
		return thread.runDirectLeafScriptCallOne(closure, args)
	}

	if thread.debugHook != nil {
		result, err := thread.runInlineScriptCall(closure, args)
		if err != nil {
			return NilValue(), err
		}
		return result.valuesList.at(0), nil
	}

	baseDepth := len(thread.frames)
	calleeFrame := thread.newCallFrame(closure.proto, args, closure.upvalues)
	thread.pushFrame(calleeFrame)
	result, err := thread.runFrame(calleeFrame)
	if err != nil {
		if thread.recoverProtectedError(err) {
			result, err = thread.runUntilDepthResult(baseDepth)
			if err != nil {
				return NilValue(), err
			}
			return result.valuesList.at(0), nil
		}
		return NilValue(), err
	}
	if result.state == vmCallStateScriptCall {
		call := result.scriptCall
		frame := thread.newCallFrame(call.closure.proto, call.args, call.closure.upvalues)
		thread.pushFrame(frame)
		result, err = thread.runUntilDepthResult(baseDepth)
		if err != nil {
			return NilValue(), err
		}
		return result.valuesList.at(0), nil
	}
	if result.state == vmCallStateYielded {
		return NilValue(), vmYieldRequest{values: result.values()}
	}
	if result.state == vmCallStateHostInterrupt {
		return NilValue(), vmHostInterrupt{}
	}
	thread.popFrame()
	return result.valuesList.at(0), nil
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

func (thread *vmThread) newCallFrame(proto *Proto, args []Value, upvalues []*cell) *vmFrame {
	counts := thread.directFramePICCounts
	counts.addFixedCallFrameMaterialization()
	counts.addFixedCallArgCopies(fixedCallParamCopyCount(proto, args))
	if frame := thread.takeFreeFrame(proto); frame != nil {
		counts.addFixedCallFrameReuse()
		frame.reset(proto, args, upvalues)
		return frame
	}
	frame := vmFramePool.Get().(*vmFrame)
	frame.reset(proto, args, upvalues)
	return frame
}

func fixedCallParamCopyCount(proto *Proto, args []Value) int {
	if proto == nil || proto.params <= 0 || len(args) == 0 {
		return 0
	}
	paramCount := proto.params
	if proto.registers < paramCount {
		paramCount = proto.registers
	}
	if len(args) < paramCount {
		return len(args)
	}
	return paramCount
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
	if !proto.directFrameDispatch || !proto.directFrameIndexCache {
		clear(frame.indexCaches)
		frame.indexCaches = frame.indexCaches[:0]
	} else if cap(frame.indexCaches) >= len(proto.code) {
		frame.indexCaches = frame.indexCaches[:len(proto.code)]
		clear(frame.indexCaches)
	} else {
		frame.indexCaches = make([]dynamicStringIndexCache, len(proto.code))
	}
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
	clear(frame.indexCaches)
	frame.indexCaches = frame.indexCaches[:0]
	if frame.tableCallCache != nil {
		*frame.tableCallCache = tableFieldCallCache{}
	}
	frame.clearPendingCall()
}

func (frame *vmFrame) resetForPool() {
	clear(frame.registers)
	clear(frame.cells)
	if cap(frame.openCallResults) > 0 {
		clear(frame.openCallResults[:cap(frame.openCallResults)])
	}
	if cap(frame.indexCaches) > 0 {
		clear(frame.indexCaches[:cap(frame.indexCaches)])
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
		frame.applyResultDestination(call.destination, result.valuesList.ownedValuesWithPrefix(BoolValue(true)))
		return
	}
	frame.applyValueListDestination(call.destination, result.valuesList)
}

func (frame *vmFrame) applySingleFrameCallResult(register int, result vmFrameResult) {
	frame.clearPendingCall()
	frame.openCallStart = -1
	frame.openCallResults = nil
	frame.setRegister(register, result.valuesList.at(0))
}

func (frame *vmFrame) applyFrameResultDestination(destination vmResultDestination, result vmFrameResult) {
	frame.applyValueListDestination(destination, result.valuesList)
}

func (frame *vmFrame) applySingleFrameResult(register int, result vmFrameResult) {
	frame.openCallStart = -1
	frame.openCallResults = nil
	if frame.directRegisters {
		frame.registers[register] = result.valuesList.at(0)
		return
	}
	frame.setRegister(register, result.valuesList.at(0))
}

func (frame *vmFrame) applyProtectedErrorResults(results []Value) {
	call := frame.pendingCall
	frame.clearPendingCall()
	frame.applyResultDestination(call.destination, results)
}

func (frame *vmFrame) callValueToDestination(callee Value, globals *globalEnv, args []Value, destination vmResultDestination) (vmFrameResult, bool, error) {
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
			return vmYieldedValues(yield.values), true, nil
		}
		if isVMHostInterrupt(err) {
			return vmFrameResult{}, true, err
		}
		return vmFrameResult{}, true, fmt.Errorf("run: call failed: %w", err)
	}
	frame.applyResultDestination(destination, results)
	return vmFrameResult{}, false, nil
}

func (frame *vmFrame) clearPendingCall() {
	frame.pendingCall = vmPendingCall{}
	frame.hasPendingCall = false
}

func (frame *vmFrame) applyResultDestination(destination vmResultDestination, results []Value) {
	frame.applyValueListDestination(destination, vmBorrowedValueList(results))
}

func (frame *vmFrame) applyValueListDestination(destination vmResultDestination, results vmValueList) {
	resultCount := destination.count
	if resultCount < 0 {
		frame.openCallStart = destination.register
		frame.openCallResults = results.adjustedRetainedValues(frame.openCallResults)
		frame.setRegister(destination.register, frame.openCallResults[0])
		return
	}

	frame.openCallStart = -1
	frame.openCallResults = nil
	for i := 0; i < resultCount; i++ {
		frame.setRegister(destination.register+i, results.at(i))
	}
}

func (frame *vmFrame) applyInlineResultDestination(destination vmResultDestination, results [2]Value, count int) {
	frame.applyValueListDestination(destination, vmInlineArrayValueList(results, count))
}

func (thread *vmThread) runFrame(frame *vmFrame) (vmFrameResult, error) {
	if frame.proto.directFrameDispatch {
		if !thread.canRunDirectFrame() {
			thread.countDirectFrameBlockedSideExit()
			return thread.runGenericFrame(frame)
		}
		exit := thread.runDirectFrame(frame)
		thread.directFramePICCounts.addSideExit(exit.reason)
		if result, complete, err := exit.frameResult(); complete || err != nil {
			return result, err
		}
	}
	return thread.runGenericFrame(frame)
}

func (thread *vmThread) canRunDirectFrame() bool {
	return thread.debugHook == nil && thread.instructionBudget < 0
}

func (thread *vmThread) countDirectFrameBlockedSideExit() {
	if thread.debugHook != nil {
		thread.directFramePICCounts.addSideExit(directFrameSideExitReasonDebug)
		return
	}
	if thread.instructionBudget >= 0 {
		thread.directFramePICCounts.addSideExit(directFrameSideExitReasonBudget)
	}
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

func directFrameApplyFastMethodFieldAdd(closure *closure, receiver Value, amount Value) (Value, bool) {
	if closure == nil || closure.proto == nil || !closure.proto.hasFastMethodFieldAdd {
		return NilValue(), false
	}
	proto := closure.proto
	if proto.fastMethodFieldAdd < 0 || proto.fastMethodFieldAdd >= len(proto.constants) {
		return NilValue(), false
	}
	if amount.kind != NumberKind || receiver.kind != TableKind || receiver.table == nil {
		return NilValue(), false
	}
	table := receiver.table
	if table.metatable != nil {
		return NilValue(), false
	}
	field := proto.constants[proto.fastMethodFieldAdd].str
	current, ok := table.rawStringField(field)
	if !ok || current.kind != NumberKind {
		return NilValue(), false
	}
	value := NumberValue(current.number + amount.number)
	table.setRawStringField(field, value)
	return value, true
}

func directFrameRowStringField(value Value, key string, slotIndex int) (Value, bool, error) {
	if value.kind != TableKind || value.table == nil {
		return NilValue(), false, fmt.Errorf("get field target is %s, want table", value.Kind())
	}
	table := value.table
	if field, ok := table.rawRowStringField(rowStringFieldSlotRefFromIndex(slotIndex), key); ok {
		return field, true, nil
	}
	if table.metatable != nil {
		return NilValue(), false, nil
	}
	return NilValue(), true, nil
}

func directFrameRowStringFieldFast(value Value, key string, slotIndex int) (Value, bool, bool) {
	if value.kind != TableKind || value.table == nil {
		return NilValue(), false, false
	}
	table := value.table
	if slotIndex >= 0 &&
		table.stringFieldMap == nil &&
		slotIndex < len(table.stringFields) &&
		table.stringFields[slotIndex].key == key {
		return table.stringFields[slotIndex].value, true, true
	}
	if field, ok := table.rawStringField(key); ok {
		return field, true, true
	}
	if table.metatable != nil {
		return NilValue(), false, true
	}
	return NilValue(), true, true
}

func directFrameRowStringFieldSlot(value Value, key string, slotIndex int) (Value, *Table, bool, bool) {
	if value.kind != TableKind || value.table == nil {
		return Value{}, nil, false, false
	}
	table := value.table
	if slotIndex >= 0 &&
		table.stringFieldMap == nil &&
		slotIndex < len(table.stringFields) &&
		table.stringFields[slotIndex].key == key {
		return table.stringFields[slotIndex].value, table, true, true
	}
	return Value{}, table, false, true
}

func directFrameTableGetIsland(table *Table, key Value) (Value, bool, error) {
	var seen map[*Table]bool
	for {
		value, err := table.rawGet(key)
		if err != nil {
			return NilValue(), true, err
		}
		if !value.IsNil() {
			return value, true, nil
		}
		if table == nil || table.metatable == nil {
			return NilValue(), true, nil
		}
		if seen != nil && seen[table] {
			return NilValue(), true, fmt.Errorf("table: cyclic __index chain")
		}
		if seen == nil {
			seen = make(map[*Table]bool)
		}
		seen[table] = true

		index, err := table.metatable.rawGet(StringValue("__index"))
		if err != nil {
			return NilValue(), true, err
		}
		if index.IsNil() {
			return NilValue(), true, nil
		}
		if indexTable, ok := index.Table(); ok {
			table = indexTable
			continue
		}
		if callableValue(index) {
			return NilValue(), false, nil
		}
		return NilValue(), true, fmt.Errorf("table: __index is %s, want table or function", index.Kind())
	}
}

func directFrameTableSetIsland(table *Table, key Value, value Value) (bool, error) {
	var seen map[*Table]bool
	for {
		current, err := table.rawGet(key)
		if err != nil {
			return true, err
		}
		if !current.IsNil() || table == nil || table.metatable == nil {
			return true, table.rawSet(key, value)
		}
		if seen != nil && seen[table] {
			return true, fmt.Errorf("table: cyclic __newindex chain")
		}
		if seen == nil {
			seen = make(map[*Table]bool)
		}
		seen[table] = true

		newIndex, err := table.metatable.rawGet(StringValue("__newindex"))
		if err != nil {
			return true, err
		}
		if newIndex.IsNil() {
			return true, table.rawSet(key, value)
		}
		if newIndexTable, ok := newIndex.Table(); ok {
			table = newIndexTable
			continue
		}
		if callableValue(newIndex) {
			return false, nil
		}
		return true, fmt.Errorf("table: __newindex is %s, want table or function", newIndex.Kind())
	}
}

func directFrameNonYieldingCallIsland(callee Value, globals *globalEnv, args []Value) ([]Value, bool, error) {
	if native, ok := callee.nativeFunction(); ok {
		results, err := native(globals, args)
		if err != nil {
			if _, ok := err.(vmYieldRequest); ok {
				return nil, false, nil
			}
			if isVMHostInterrupt(err) {
				return nil, true, err
			}
			return nil, true, fmt.Errorf("host function failed: %w", err)
		}
		return results, true, nil
	}
	if host, ok := callee.hostFunction(); ok {
		if host == nil {
			return nil, true, fmt.Errorf("call target is nil host_function")
		}
		results, err := host(args)
		if err != nil {
			return nil, true, fmt.Errorf("host function failed: %w", err)
		}
		return results, true, nil
	}
	return nil, false, nil
}

func directFrameApplyCallIslandResults(frame *vmFrame, registers []Value, start int, count int, results []Value) {
	frame.openCallStart = -1
	frame.openCallResults = nil
	for i := 0; i < count; i++ {
		registers[start+i] = adjustedResultAt(results, i)
	}
}

func vmRowStringField(globals *globalEnv, table *Table, keyValue Value, key string, slotIndex int) (Value, error) {
	if value, ok := table.rawRowStringField(rowStringFieldSlotRefFromIndex(slotIndex), key); ok {
		return value, nil
	}
	if table.metatable == nil {
		return NilValue(), nil
	}
	value, err := runtimeTableAccess(globals).get(table, keyValue)
	if err != nil {
		return NilValue(), fmt.Errorf("run: get field failed: %w", err)
	}
	return value, nil
}

func (cache *dynamicStringIndexCache) get(table *Table, key string) (Value, bool) {
	return cache.getCounted(table, key, nil)
}

func (cache *dynamicStringIndexCache) getCounted(table *Table, key string, counts *directFramePICCounts) (Value, bool) {
	if cache == nil {
		counts.addKeyMiss()
		return NilValue(), false
	}
	keyMatched := false
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == nil || entry.key != key {
			continue
		}
		keyMatched = true
		value, ok := table.rawStringFieldAtSlot(entry.slot, key)
		if !ok {
			counts.addShapeMiss()
			continue
		}
		counts.addHit(i)
		return value, true
	}
	if keyMatched {
		return NilValue(), false
	}
	counts.addKeyMiss()
	return NilValue(), false
}

func (cache *dynamicStringIndexCache) store(table *Table, key string, slot tableStringFieldSlot) {
	if cache == nil {
		return
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table != nil &&
			entry.key == key &&
			entry.slot.index == slot.index &&
			entry.slot.token.sameLayout(slot.token) {
			entry.table = table
			entry.slot = slot
			return
		}
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == nil {
			entry.table = table
			entry.key = key
			entry.slot = slot
			return
		}
	}
	index := int(cache.next % uint8(len(cache.entries)))
	cache.next++
	cache.entries[index] = dynamicStringIndexCacheEntry{
		table: table,
		key:   key,
		slot:  slot,
	}
}

func (cache *dynamicStringIndexCache) write(table *Table, key string, value Value) bool {
	return cache.writeCounted(table, key, value, nil)
}

func (cache *dynamicStringIndexCache) writeCounted(table *Table, key string, value Value, counts *directFramePICCounts) bool {
	if value.IsNil() {
		counts.addNilWriteFallback()
		return false
	}
	if cache == nil {
		counts.addKeyMiss()
		return false
	}
	keyMatched := false
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == nil || entry.key != key {
			continue
		}
		keyMatched = true
		if !table.setRawStringFieldAtSlot(entry.slot, key, value) {
			counts.addShapeMiss()
			continue
		}
		counts.addHit(i)
		return true
	}
	if keyMatched {
		return false
	}
	counts.addKeyMiss()
	return false
}

func (cache *tableFieldCallCache) get(table *Table, key string) (*closure, bool) {
	return cache.getCounted(table, key, nil)
}

func (cache *tableFieldCallCache) getCounted(table *Table, key string, counts *directFramePICCounts) (*closure, bool) {
	if cache == nil {
		counts.addKeyMiss()
		return nil, false
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table != table || entry.key != key {
			continue
		}
		if entry.closure == nil || !entry.token.matchesTableValues(table) {
			counts.addShapeMiss()
			return nil, false
		}
		counts.addHit(i)
		return entry.closure, true
	}
	counts.addKeyMiss()
	return nil, false
}

func (cache *tableFieldCallCache) store(table *Table, key string, closure *closure) {
	if cache == nil {
		return
	}
	token := table.stringShapeToken()
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == table && entry.key == key {
			entry.token = token
			entry.closure = closure
			return
		}
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == nil {
			entry.table = table
			entry.key = key
			entry.token = token
			entry.closure = closure
			return
		}
	}
	index := int(cache.next % uint8(len(cache.entries)))
	cache.next++
	cache.entries[index] = tableFieldCallCacheEntry{
		table:   table,
		key:     key,
		token:   token,
		closure: closure,
	}
}

func directFrameApplyMoveOnlyBlockPlan(proto *Proto, registers []Value, plan directBlockPlanDesc) bool {
	if proto == nil || plan.startPC < 0 || plan.resumePC > len(proto.code) || plan.startPC >= plan.resumePC {
		return false
	}
	for pc := plan.startPC + 1; pc < plan.resumePC; pc++ {
		ins := proto.code[pc]
		if ins.op == opJump && ins.b == plan.resumePC && pc == plan.resumePC-1 {
			continue
		}
		if ins.op != opMove || ins.a < 0 || ins.a >= len(registers) || ins.b < 0 || ins.b >= len(registers) {
			return false
		}
		registers[ins.a] = registers[ins.b]
	}
	return true
}

func directFrameApplyPairedRowDiffBlockPlan(frame *vmFrame, registers []Value, plan directBlockPlanDesc, picCounts *directFramePICCounts) directFrameSideExit {
	proto := frame.proto
	if proto == nil || plan.startPC < 0 || plan.startPC+3 >= len(proto.code) || plan.resumePC != plan.startPC+4 {
		return directFrameEnterGenericFrame()
	}
	get := proto.code[plan.startPC]
	leftLoad := proto.code[plan.startPC+1]
	rightLoad := proto.code[plan.startPC+2]
	diff := proto.code[plan.startPC+3]
	if get.op != opGetIndex || leftLoad.op != opGetRowStringField || rightLoad.op != opGetRowStringField || diff.op != opSub {
		return directFrameEnterGenericFrame()
	}

	base := registers[get.b]
	if base.kind != TableKind || base.table == nil {
		return directFrameFail(fmt.Errorf("run: get index target is %s, want table", base.Kind()))
	}
	table := base.table
	if table.metatable != nil {
		if picCounts != nil {
			picCounts.addMetatableMiss()
			picCounts.addSideExit(directFrameSideExitReasonTable)
		}
		frame.pc = plan.startPC
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonTable)
	}
	rightRow, err := table.rawGet(registers[get.c])
	if err != nil {
		return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
	}
	registers[get.a] = rightRow

	left, ok, err := directFrameRowStringField(registers[leftLoad.b], proto.constantKeys[leftLoad.c].str, leftLoad.d)
	if err != nil {
		return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
	}
	if !ok {
		frame.pc = plan.startPC + 1
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonTable)
	}
	registers[leftLoad.a] = left

	right, ok, err := directFrameRowStringField(registers[rightLoad.b], proto.constantKeys[rightLoad.c].str, rightLoad.d)
	if err != nil {
		return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
	}
	if !ok {
		frame.pc = plan.startPC + 2
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonTable)
	}
	registers[rightLoad.a] = right

	if left.kind != NumberKind || right.kind != NumberKind {
		frame.pc = plan.startPC + 3
		return directFrameEnterGenericFrame()
	}
	registers[diff.a] = NumberValue(left.number - right.number)
	return directFrameResume()
}

func directFrameApplyRowFieldAddStoreBlockPlan(frame *vmFrame, registers []Value, plan directBlockPlanDesc) directFrameSideExit {
	proto := frame.proto
	if proto == nil || plan.startPC < 0 || plan.startPC >= len(proto.code) || plan.resumePC != plan.startPC+1 {
		return directFrameEnterGenericFrame()
	}
	ins := proto.code[plan.startPC]
	if ins.op != opAddStringField ||
		ins.a != plan.register ||
		ins.b != plan.field ||
		ins.c != plan.candidate ||
		plan.slot < 0 {
		return directFrameEnterGenericFrame()
	}
	base := registers[ins.a]
	if base.kind != TableKind || base.table == nil {
		return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
	}
	table := base.table
	if table.metatable != nil {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonIntrinsic)
	}
	right := registers[ins.c]
	if right.kind != NumberKind {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	key := proto.constantKeys[ins.b].str
	left, ok := table.rawRowStringField(rowStringFieldSlotRefFromIndex(plan.slot), key)
	if !ok || left.kind != NumberKind {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	table.setRawRowStringField(rowStringFieldSlotRefFromIndex(plan.slot), key, NumberValue(left.number+right.number))
	return directFrameResume()
}

func directFrameApplyRowFieldBranchStoreBlockPlan(frame *vmFrame, registers []Value, plan directBlockPlanDesc) directFrameSideExit {
	proto := frame.proto
	if proto == nil || plan.startPC < 0 || plan.startPC+2 >= len(proto.code) || plan.resumePC <= plan.startPC+2 || plan.resumePC > len(proto.code) {
		return directFrameEnterGenericFrame()
	}
	branch := proto.code[plan.startPC]
	first := proto.code[plan.startPC+1]
	store := proto.code[plan.startPC+2]
	if (branch.op != opJumpIfRowStringFieldNotGreaterK && branch.op != opJumpIfRowStringFieldGreaterK) ||
		branch.a != plan.register ||
		plan.slot < 0 {
		return directFrameEnterGenericFrame()
	}
	desc := proto.rowFieldEqualOps[branch.b]
	if desc.field != plan.field ||
		desc.slot != plan.slot ||
		!directFrameRowFieldBranchStoreBodyMatches(proto, first, store, plan) {
		return directFrameEnterGenericFrame()
	}
	left, ok, err := directFrameRowStringField(registers[branch.a], proto.constantKeys[desc.field].str, desc.slot)
	if err != nil {
		return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
	}
	if !ok || left.kind != NumberKind || !proto.constantNumberOK[desc.value] {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	right := proto.constantNumbers[desc.value]
	if math.IsNaN(left.number) || math.IsNaN(right) {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	greater := left.number > right
	shouldJump := (branch.op == opJumpIfRowStringFieldNotGreaterK && !greater) ||
		(branch.op == opJumpIfRowStringFieldGreaterK && greater)
	if shouldJump {
		return directFrameResume()
	}
	base := registers[store.a]
	if base.kind != TableKind || base.table == nil {
		return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
	}
	switch store.op {
	case opSetRowStringField, opAddStringField, opSubStringField:
		registers[first.a] = proto.constants[first.b]
		key := proto.constantKeys[store.b].str
		if store.op == opSetRowStringField {
			base.table.setRawRowStringField(rowStringFieldSlotRefFromIndex(plan.slot), key, registers[store.c])
			return directFrameResume()
		}
		if base.table.metatable != nil {
			frame.pc = plan.startPC + 2
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonIntrinsic)
		}
		right := registers[store.c]
		if right.kind != NumberKind {
			frame.pc = plan.startPC + 2
			return directFrameEnterGenericFrame()
		}
		next := left.number + right.number
		if store.op == opSubStringField {
			next = left.number - right.number
		}
		base.table.setRawRowStringField(rowStringFieldSlotRefFromIndex(plan.slot), key, NumberValue(next))
	case opSubAddStringField:
		registers[first.a] = registers[first.b]
		if base.table.metatable != nil {
			frame.pc = plan.startPC + 2
			return directFrameEnterGenericFrame()
		}
		subAdd := proto.rowFieldSubAddOps[store.b]
		subtract := registers[store.c]
		addKey := proto.constantKeys[subAdd.add].str
		add, addOK := base.table.rawRowStringField(rowStringFieldSlotRefFromIndex(subAdd.addSlot), addKey)
		if subtract.kind != NumberKind || !addOK || add.kind != NumberKind {
			frame.pc = plan.startPC + 2
			return directFrameEnterGenericFrame()
		}
		key := proto.constantKeys[subAdd.target].str
		base.table.setRawRowStringField(rowStringFieldSlotRefFromIndex(plan.slot), key, NumberValue(left.number-subtract.number+add.number))
	default:
		return directFrameEnterGenericFrame()
	}
	return directFrameResume()
}

func directFrameRowFieldBranchStoreBodyMatches(proto *Proto, first instruction, store instruction, plan directBlockPlanDesc) bool {
	switch store.op {
	case opSetRowStringField, opAddStringField, opSubStringField:
		return first.op == opLoadConst &&
			rowFieldBranchStoreMutationMatches(proto, store, plan.register, first.a, plan.field, plan.slot)
	case opSubAddStringField:
		if first.op != opMove || store.a != plan.register || store.c != first.a || first.b != plan.candidate {
			return false
		}
		desc, ok := rowFieldSubAddDesc(proto, store.b)
		return ok && desc.targetSlot == plan.slot && desc.addSlot >= 0 && sameStringConstant(proto, desc.target, plan.field)
	default:
		return false
	}
}

func (thread *vmThread) executeVerifiedPlan(frame *vmFrame, plan verifiedPlanDesc) directFrameSideExit {
	picCounts := thread.directFramePICCounts
	switch plan.kind {
	case verifiedPlanKindDirectBlock:
		picCounts.addDirectBlockEntry()
		exit := thread.executeVerifiedDirectBlockPlan(frame, plan.directBlock)
		if exit.resumesDirectFrame() {
			picCounts.addDirectBlockResume()
			frame.pc = plan.resumePC
			return exit
		}
		picCounts.addDirectBlockFallback(exit.reason)
		return exit
	default:
		return directFrameEnterGenericFrame()
	}
}

func (thread *vmThread) executeVerifiedDirectBlockPlan(frame *vmFrame, plan directBlockPlanDesc) directFrameSideExit {
	block, ok := blockPlanFromDirectBlock(plan)
	if !ok {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	return thread.executeBlockPlan(frame, block)
}

func (thread *vmThread) executeBlockPlan(frame *vmFrame, plan blockPlanDesc) directFrameSideExit {
	registers := frame.registers
	switch plan.kind {
	case blockPlanKindAbsoluteDelta:
		return directFrameApplyAbsoluteDeltaBlockPlan(frame, registers, plan.directBlock)
	case blockPlanKindMax:
		return directFrameApplyMaxBlockPlan(frame, registers, plan.directBlock)
	case blockPlanKindPairedRowDiff:
		return directFrameApplyPairedRowDiffBlockPlan(frame, registers, plan.directBlock, thread.directFramePICCounts)
	case blockPlanKindRowFieldAddStore:
		return directFrameApplyRowFieldAddStoreBlockPlan(frame, registers, plan.directBlock)
	case blockPlanKindRowFieldBranchStore:
		return directFrameApplyRowFieldBranchStoreBlockPlan(frame, registers, plan.directBlock)
	case blockPlanKindDynamicPathAddStore:
		return directFrameApplyDynamicPathAddStoreBlockPlan(frame, registers, plan)
	case blockPlanKindRowFieldAddFieldStore:
		return directFrameApplyRowFieldAddFieldStoreBlockPlan(frame, registers, plan)
	default:
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
}

func directFrameApplyRowFieldAddFieldStoreBlockPlan(frame *vmFrame, registers []Value, plan blockPlanDesc) directFrameSideExit {
	proto := frame.proto
	desc := plan.rowField
	if proto == nil ||
		desc.field < 0 ||
		desc.field >= len(proto.constantKeyOK) ||
		!proto.constantKeyOK[desc.field] ||
		desc.addField < 0 ||
		desc.addField >= len(proto.constantKeyOK) ||
		!proto.constantKeyOK[desc.addField] ||
		desc.constant < 0 ||
		desc.constant >= len(proto.constantNumberOK) ||
		!proto.constantNumberOK[desc.constant] ||
		desc.slot < 0 ||
		desc.addSlot < 0 {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	base := registers[desc.base]
	if base.kind != TableKind || base.table == nil {
		return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
	}
	table := base.table
	if table.metatable != nil {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonIntrinsic)
	}
	targetKey := proto.constantKeys[desc.field].str
	addKey := proto.constantKeys[desc.addField].str
	if table.stringFieldMap != nil ||
		desc.slot >= len(table.stringFields) ||
		desc.addSlot >= len(table.stringFields) {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	targetField := &table.stringFields[desc.slot]
	addField := &table.stringFields[desc.addSlot]
	if targetField.key != targetKey ||
		addField.key != addKey ||
		targetField.value.kind != NumberKind ||
		addField.value.kind != NumberKind {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	constant := proto.constantNumbers[desc.constant]
	next := targetField.value.number + constant
	if desc.constOp == opSubK {
		next = targetField.value.number - constant
	} else if desc.constOp != opAddK {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	if desc.op == opAdd {
		next += addField.value.number
	} else if desc.op == opSub {
		next -= addField.value.number
	} else {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	value := NumberValue(next)
	targetField.value = value
	table.stringValueVersion++
	registers[desc.result] = value
	frame.pc = plan.resumePC
	return directFrameResume()
}

func directFrameApplyDynamicPathAddStoreBlockPlan(frame *vmFrame, registers []Value, plan blockPlanDesc) directFrameSideExit {
	proto := frame.proto
	desc := plan.dynamicPath
	if proto == nil || plan.startPC < 0 || plan.startPC >= len(proto.code) || plan.resumePC <= plan.startPC {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	base := registers[desc.base]
	if base.kind != TableKind || base.table == nil {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	table := base.table
	if table.metatable != nil || desc.field < 0 || desc.field >= len(proto.constantKeys) {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	key := registers[desc.key]
	if key.kind != StringKind {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	first, ok := table.rawStringField(proto.constantKeys[desc.field].str)
	if !ok || first.kind != TableKind || first.table == nil {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	child := first.table
	if child.metatable != nil {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	left, ok := child.rawStringField(key.str)
	if !ok {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	delta := registers[desc.delta]
	if left.kind != NumberKind || delta.kind != NumberKind {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	next := left.number + delta.number
	if desc.op == opSub {
		next = left.number - delta.number
	} else if desc.op != opAdd {
		frame.pc = plan.fallbackPC
		return directFrameEnterGenericFrame()
	}
	value := NumberValue(next)
	child.setRawStringField(key.str, value)
	registers[desc.result] = value
	frame.pc = plan.resumePC
	return directFrameResume()
}

func directFrameApplyAbsoluteDeltaBlockPlan(frame *vmFrame, registers []Value, plan directBlockPlanDesc) directFrameSideExit {
	proto := frame.proto
	if proto == nil || plan.startPC < 0 || plan.startPC >= len(proto.code) {
		return directFrameEnterGenericFrame()
	}
	ins := proto.code[plan.startPC]
	if ins.op != opJumpIfNotLessK || ins.a != plan.register || plan.resumePC != ins.d {
		return directFrameEnterGenericFrame()
	}
	left := registers[ins.a]
	if left.kind != NumberKind || !proto.constantNumberOK[ins.b] {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	right := proto.constantNumbers[ins.b]
	if !math.IsNaN(left.number) && !math.IsNaN(right) && left.number >= right {
		return directFrameResume()
	}
	registers[plan.register] = NumberValue(-left.number)
	return directFrameResume()
}

func directFrameApplyMaxBlockPlan(frame *vmFrame, registers []Value, plan directBlockPlanDesc) directFrameSideExit {
	proto := frame.proto
	if proto == nil || plan.startPC < 0 || plan.startPC >= len(proto.code) {
		return directFrameEnterGenericFrame()
	}
	ins := proto.code[plan.startPC]
	if ins.op != opJumpIfNotGreater || ins.a != plan.candidate || ins.b != plan.register || plan.resumePC != ins.d {
		return directFrameEnterGenericFrame()
	}
	left := registers[ins.a]
	right := registers[ins.b]
	if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	if left.number <= right.number {
		return directFrameResume()
	}
	if !directFrameApplyMoveOnlyBlockPlan(proto, registers, plan) {
		frame.pc = plan.startPC
		return directFrameEnterGenericFrame()
	}
	return directFrameResume()
}

func (thread *vmThread) runDirectFrame(frame *vmFrame) directFrameSideExit {
	proto := frame.proto
	registers := frame.registers
	opcodeCounts := thread.directFrameOpcodeCounts
	picCounts := thread.directFramePICCounts
	verifiedPlans := proto.verifiedPlans
	verifiedPlanPCs := proto.verifiedPlanPCs
	hasVerifiedPlans := picCounts != nil && len(verifiedPlans) != 0 && len(verifiedPlanPCs) != 0
	blockPlans := proto.blockPlans
	blockPlanPCs := proto.blockPlanPCs
	hasBlockPlans := len(blockPlans) != 0 && len(blockPlanPCs) != 0

	for frame.pc < len(proto.code) {
		ins := proto.code[frame.pc]
		if opcodeCounts != nil {
			opcodeCounts[uint8(ins.op)]++
		}
		if pcCountsByProto := thread.directFramePCCounts; pcCountsByProto != nil {
			pcCounts := pcCountsByProto[proto]
			if pcCounts == nil {
				pcCounts = make([]uint64, len(proto.code))
				pcCountsByProto[proto] = pcCounts
			}
			pcCounts[frame.pc]++
		}
		if hasVerifiedPlans && frame.pc < len(verifiedPlanPCs) {
			planIndex := verifiedPlanPCs[frame.pc]
			if planIndex >= 0 && planIndex < len(verifiedPlans) {
				exit := thread.executeVerifiedPlan(frame, verifiedPlans[planIndex])
				if exit.resumesDirectFrame() {
					continue
				}
				return exit
			}
		}

		switch ins.op {
		case opLoadConst:
			registers[ins.a] = proto.constants[ins.b]

		case opLoadGlobal:
			name, _ := proto.constants[ins.b].String()
			value, ok := thread.globals.get(name)
			if !ok {
				return directFrameFail(fmt.Errorf("run: undefined global %q", name))
			}
			registers[ins.a] = value

		case opNewTable:
			registers[ins.a] = TableValue(newTableWithCapacity(ins.b, ins.c))

		case opMove:
			registers[ins.a] = registers[ins.b]

		case opSetField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(table, proto.constants[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			if proto.constantKeyOK[ins.b] {
				if err := table.rawSetKey(proto.constantKeys[ins.b], registers[ins.c]); err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				break
			}
			if err := table.rawSet(proto.constants[ins.b], registers[ins.c]); err != nil {
				return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
			}

		case opSetStringField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(table, proto.constants[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			table.setRawStringField(proto.constantKeys[ins.b].str, registers[ins.c])

		case opSetRowStringField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(table, proto.constants[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			key := proto.constantKeys[ins.b].str
			value := registers[ins.c]
			if !value.IsNil() && table.stringFieldMap == nil && ins.d >= 0 && ins.d < len(table.stringFields) && table.stringFields[ins.d].key == key {
				table.stringFields[ins.d].value = value
				table.stringValueVersion++
				break
			}
			table.setRawRowStringField(rowStringFieldSlotRefFromIndex(ins.d), key, value)

		case opSetStringField2:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			table := base.table
			firstKey := proto.constantKeys[ins.b].str
			secondKey := proto.constantKeys[ins.c].str
			value := registers[ins.d]
			pathCacheAllowed := thread.runtimePathPlanCacheEnabled() && proto.pathPlanCacheAllowsStringField2(frame.pc, "write", ins.a, ins.b, ins.c)
			if pathCacheAllowed && thread.writeRuntimePathCache(frame.pc, table, firstKey, secondKey, value) {
				break
			}
			first, ok := table.rawStringField(firstKey)
			if !ok {
				if table.metatable != nil {
					return directFrameEnterGenericFrame()
				}
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", NilValue().Kind()))
			}
			if first.kind != TableKind || first.table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", first.Kind()))
			}
			nextTable := first.table
			if nextTable.metatable != nil {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonIntrinsic)
			}
			nextTable.setRawStringField(secondKey, value)
			if pathCacheAllowed && !value.IsNil() {
				thread.storeRuntimePathCacheFromResolved(frame.pc, table, firstKey, nextTable, secondKey)
			}

		case opSetStringFieldIndex:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			table := base.table
			firstKey := proto.constantKeys[ins.b].str
			pathCacheAllowed := thread.runtimePathPlanCacheEnabled() && proto.pathPlanCacheAllowsStringFieldIndex(frame.pc, "write", ins.a, ins.b)
			var nextTable *Table
			pathCacheHit := false
			if pathCacheAllowed {
				nextTable, pathCacheHit = thread.getRuntimeDynamicPathCache(frame.pc, table, firstKey)
			}
			if !pathCacheAllowed || !pathCacheHit {
				first, ok := table.rawStringField(firstKey)
				if !ok {
					if table.metatable != nil {
						picCounts.addMetatableMiss()
						return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
					}
					return directFrameFail(fmt.Errorf("run: set index target is %s, want table", NilValue().Kind()))
				}
				if first.kind != TableKind || first.table == nil {
					return directFrameFail(fmt.Errorf("run: set index target is %s, want table", first.Kind()))
				}
				nextTable = first.table
				if pathCacheAllowed {
					if firstSlot, ok := table.rawStringFieldSlot(firstKey); ok {
						thread.storeRuntimeDynamicPathCache(frame.pc, table, firstKey, firstSlot, nextTable)
					}
				}
			}
			if nextTable.metatable != nil {
				picCounts.addMetatableMiss()
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			key := registers[ins.c]
			if key.kind == StringKind {
				cache := &frame.indexCaches[frame.pc]
				value := registers[ins.d]
				if cache.writeCounted(nextTable, key.str, value, picCounts) {
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.str); ok && nextTable.setRawStringFieldAtSlot(slot, key.str, value) {
					cache.store(nextTable, key.str, slot)
					break
				}
			} else {
				picCounts.addInvalidKeyFallback()
			}
			if err := nextTable.rawSet(key, registers[ins.d]); err != nil {
				return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
			}

		case opGetField:
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				value, ok, err := directFrameTableGetIsland(table, proto.constants[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = value
				break
			}
			var value Value
			var err error
			if proto.constantKeyOK[ins.c] {
				value, err = table.rawGetKey(proto.constantKeys[ins.c])
			} else {
				value, err = table.rawGet(proto.constants[ins.c])
			}
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			registers[ins.a] = value

		case opGetStringField:
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			if value, ok := table.rawStringField(proto.constantKeys[ins.c].str); ok {
				registers[ins.a] = value
				break
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				value, ok, err := directFrameTableGetIsland(table, proto.constants[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NilValue()

		case opGetRowStringField:
			if hasBlockPlans && frame.pc < len(blockPlanPCs) {
				planIndex := blockPlanPCs[frame.pc]
				if planIndex >= 0 && planIndex < len(blockPlans) {
					plan := blockPlans[planIndex]
					if plan.kind == blockPlanKindRowFieldAddFieldStore {
						exit := directFrameApplyRowFieldAddFieldStoreBlockPlan(frame, registers, plan)
						if exit.resumesDirectFrame() {
							continue
						}
						return exit
					}
				}
			}
			key := proto.constantKeys[ins.c].str
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.stringFieldMap == nil && ins.d >= 0 && ins.d < len(table.stringFields) && table.stringFields[ins.d].key == key {
				registers[ins.a] = table.stringFields[ins.d].value
				break
			}
			if field, ok := table.rawStringField(key); ok {
				registers[ins.a] = field
				break
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				var ok bool
				var err error
				var value Value
				value, ok, err = directFrameTableGetIsland(table, proto.constants[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NilValue()

		case opGetStringField2:
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			firstKey := proto.constantKeys[ins.c].str
			secondKey := proto.constantKeys[ins.d].str
			pathCacheAllowed := proto.pathFactAllowsStringField2(frame.pc, ins)
			if pathCacheAllowed {
				if value, ok := thread.getRuntimePathCache(frame.pc, table, firstKey, secondKey); ok {
					registers[ins.a] = value
					break
				}
			}
			first, ok := table.rawStringField(firstKey)
			if !ok {
				if table.metatable != nil {
					return directFrameEnterGenericFrame()
				}
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", NilValue().Kind()))
			}
			if first.kind != TableKind || first.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", first.Kind()))
			}
			nextTable := first.table
			if value, ok := nextTable.rawStringField(secondKey); ok {
				if pathCacheAllowed {
					thread.storeRuntimePathCacheFromResolved(frame.pc, table, firstKey, nextTable, secondKey)
				}
				registers[ins.a] = value
				break
			}
			if nextTable.metatable != nil {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonIntrinsic)
			}
			registers[ins.a] = NilValue()

		case opGetStringFieldIndex:
			if hasBlockPlans && frame.pc < len(blockPlanPCs) {
				planIndex := blockPlanPCs[frame.pc]
				if planIndex >= 0 && planIndex < len(blockPlans) {
					plan := blockPlans[planIndex]
					if plan.kind == blockPlanKindDynamicPathAddStore {
						exit := directFrameApplyDynamicPathAddStoreBlockPlan(frame, registers, plan)
						if exit.resumesDirectFrame() {
							continue
						}
						return exit
					}
				}
			}
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			firstKey := proto.constantKeys[ins.c].str
			pathCacheAllowed := proto.pathFactAllowsStringFieldIndex(frame.pc, ins)
			var nextTable *Table
			pathCacheHit := false
			if pathCacheAllowed {
				nextTable, pathCacheHit = thread.getRuntimeDynamicPathCache(frame.pc, table, firstKey)
			}
			if !pathCacheAllowed || !pathCacheHit {
				first, ok := table.rawStringField(firstKey)
				if !ok {
					if table.metatable != nil {
						picCounts.addMetatableMiss()
						return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
					}
					return directFrameFail(fmt.Errorf("run: get index target is %s, want table", NilValue().Kind()))
				}
				if first.kind != TableKind || first.table == nil {
					return directFrameFail(fmt.Errorf("run: get index target is %s, want table", first.Kind()))
				}
				nextTable = first.table
				if pathCacheAllowed {
					if firstSlot, ok := table.rawStringFieldSlot(firstKey); ok {
						thread.storeRuntimeDynamicPathCache(frame.pc, table, firstKey, firstSlot, nextTable)
					}
				}
			}
			if nextTable.metatable != nil {
				picCounts.addMetatableMiss()
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			key := registers[ins.d]
			if key.kind == StringKind {
				cache := &frame.indexCaches[frame.pc]
				if value, ok := cache.getCounted(nextTable, key.str, picCounts); ok {
					registers[ins.a] = value
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.str); ok {
					value, ok := nextTable.rawStringFieldAtSlot(slot, key.str)
					if ok {
						cache.store(nextTable, key.str, slot)
						registers[ins.a] = value
						break
					}
				} else {
					picCounts.addMissingKeyFallback()
				}
			} else {
				picCounts.addInvalidKeyFallback()
			}
			value, err := nextTable.rawGet(key)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
			}
			registers[ins.a] = value

		case opAddStringField, opSubStringField:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonIntrinsic)
			}
			right := registers[ins.c]
			if right.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			key := proto.constantKeys[ins.b].str
			left := NilValue()
			ok := false
			slotHit := false
			if table.stringFieldMap == nil && ins.d >= 0 && ins.d < len(table.stringFields) && table.stringFields[ins.d].key == key {
				left = table.stringFields[ins.d].value
				ok = true
				slotHit = true
			} else if field, found := table.rawStringField(key); found {
				left = field
				ok = true
			}
			if !ok || left.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			next := left.number + right.number
			if ins.op == opSubStringField {
				next = left.number - right.number
			}
			if slotHit {
				table.stringFields[ins.d].value = NumberValue(next)
				table.stringValueVersion++
			} else if ins.d >= 0 {
				table.setRawRowStringField(rowStringFieldSlotRefFromIndex(ins.d), key, NumberValue(next))
			} else {
				table.setRawStringField(key, NumberValue(next))
			}

		case opSubAddStringField:
			desc := proto.rowFieldSubAddOps[ins.b]
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				return directFrameEnterGenericFrame()
			}
			subtract := registers[ins.c]
			if subtract.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			targetKey := proto.constantKeys[desc.target].str
			addKey := proto.constantKeys[desc.add].str
			var left Value
			var add Value
			var leftOK bool
			var addOK bool
			targetRef := rowStringFieldSlotRefFromIndex(desc.targetSlot)
			addRef := rowStringFieldSlotRefFromIndex(desc.addSlot)
			left, leftOK = table.rawRowStringField(targetRef, targetKey)
			add, addOK = table.rawRowStringField(addRef, addKey)
			if !leftOK || !addOK {
				return directFrameEnterGenericFrame()
			}
			if left.kind != NumberKind || add.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			table.setRawRowStringField(targetRef, targetKey, NumberValue(left.number-subtract.number+add.number))

		case opAddSubStringField2:
			desc := proto.stringField2AddSubOps[ins.b]
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				return directFrameEnterGenericFrame()
			}
			targetFirstKey := proto.constantKeys[desc.targetFirst].str
			targetSecondKey := proto.constantKeys[desc.targetSecond].str
			addFirstKey := proto.constantKeys[desc.addFirst].str
			addSecondKey := proto.constantKeys[desc.addSecond].str
			subFirstKey := proto.constantKeys[desc.subFirst].str
			subSecondKey := proto.constantKeys[desc.subSecond].str
			pathPlanCacheEnabled := thread.runtimePathPlanCacheEnabled()
			targetCacheAllowed := pathPlanCacheEnabled && proto.pathPlanCacheAllowsStringField2(frame.pc, "read_modify_write", ins.a, desc.targetFirst, desc.targetSecond)
			addCacheAllowed := pathPlanCacheEnabled && proto.pathPlanCacheAllowsStringField2(frame.pc, "read", ins.a, desc.addFirst, desc.addSecond)
			subCacheAllowed := pathPlanCacheEnabled && proto.pathPlanCacheAllowsStringField2(frame.pc, "read", ins.a, desc.subFirst, desc.subSecond)
			if targetCacheAllowed && addCacheAllowed && subCacheAllowed {
				targetHit, targetOK := thread.getRuntimePathCacheHit(frame.pc, table, targetFirstKey, targetSecondKey)
				addHit, addOK := thread.getRuntimePathCacheHit(frame.pc, table, addFirstKey, addSecondKey)
				subHit, subOK := thread.getRuntimePathCacheHit(frame.pc, table, subFirstKey, subSecondKey)
				if targetOK && addOK && subOK {
					if targetHit.value.kind != NumberKind || addHit.value.kind != NumberKind || subHit.value.kind != NumberKind {
						return directFrameEnterGenericFrame()
					}
					next := NumberValue(targetHit.value.number + addHit.value.number - subHit.value.number)
					if targetHit.child.setRawStringFieldAtSlot(targetHit.secondSlot, targetSecondKey, next) {
						break
					}
				}
			}
			targetFirst, ok := table.rawStringField(targetFirstKey)
			if !ok {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", NilValue().Kind()))
			}
			if targetFirst.kind != TableKind || targetFirst.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", targetFirst.Kind()))
			}
			addFirst, ok := table.rawStringField(addFirstKey)
			if !ok {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", NilValue().Kind()))
			}
			if addFirst.kind != TableKind || addFirst.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", addFirst.Kind()))
			}
			subFirst, ok := table.rawStringField(subFirstKey)
			if !ok {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", NilValue().Kind()))
			}
			if subFirst.kind != TableKind || subFirst.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", subFirst.Kind()))
			}
			targetTable := targetFirst.table
			addTable := addFirst.table
			subTable := subFirst.table
			if targetTable.metatable != nil || addTable.metatable != nil || subTable.metatable != nil {
				return directFrameEnterGenericFrame()
			}
			left, _ := targetTable.rawStringField(targetSecondKey)
			addRight, _ := addTable.rawStringField(addSecondKey)
			subRight, _ := subTable.rawStringField(subSecondKey)
			if left.kind != NumberKind || addRight.kind != NumberKind || subRight.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			targetTable.setRawStringField(targetSecondKey, NumberValue(left.number+addRight.number-subRight.number))
			if targetCacheAllowed {
				thread.storeRuntimePathCacheFromResolved(frame.pc, table, targetFirstKey, targetTable, targetSecondKey)
			}
			if addCacheAllowed {
				thread.storeRuntimePathCacheFromResolved(frame.pc, table, addFirstKey, addTable, addSecondKey)
			}
			if subCacheAllowed {
				thread.storeRuntimePathCacheFromResolved(frame.pc, table, subFirstKey, subTable, subSecondKey)
			}

		case opSetIndex:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				picCounts.addMetatableMiss()
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(table, registers[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			key := registers[ins.b]
			if key.kind == StringKind {
				cache := &frame.indexCaches[frame.pc]
				value := registers[ins.c]
				if cache.writeCounted(table, key.str, value, picCounts) {
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.str); ok && table.setRawStringFieldAtSlot(slot, key.str, value) {
					cache.store(table, key.str, slot)
					break
				}
			} else {
				picCounts.addInvalidKeyFallback()
			}
			if err := table.rawSet(registers[ins.b], registers[ins.c]); err != nil {
				return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
			}

		case opGetIndex:
			base := registers[ins.b]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", base.Kind()))
			}
			table := base.table
			if table.metatable != nil {
				picCounts.addMetatableMiss()
				picCounts.addSideExit(directFrameSideExitReasonTable)
				value, ok, err := directFrameTableGetIsland(table, registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = value
				break
			}
			key := registers[ins.c]
			if key.kind == StringKind {
				cache := &frame.indexCaches[frame.pc]
				if value, ok := cache.getCounted(table, key.str, picCounts); ok {
					registers[ins.a] = value
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.str); ok {
					value, ok := table.rawStringFieldAtSlot(slot, key.str)
					if ok {
						cache.store(table, key.str, slot)
						registers[ins.a] = value
						break
					}
				} else {
					picCounts.addMissingKeyFallback()
				}
			} else if index, ok := tableArrayIndexFromValue(key); ok && index <= len(table.array) {
				picCounts.addNumericArrayIndexHit()
				registers[ins.a] = table.array[index-1]
				break
			} else {
				picCounts.addInvalidKeyFallback()
			}
			value, err := table.rawGet(key)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
			}
			registers[ins.a] = value

		case opClosure:
			captured := captureUpvalues(proto.prototypes[ins.b], frame)
			registers[ins.a] = functionValue(proto.prototypes[ins.b], captured)

		case opPrepareIter:
			iterValue := registers[ins.a]
			if iterValue.kind == TableKind && iterValue.table != nil && tableCanIterateCleanArray(iterValue.table) {
				registers[ins.a] = Value{kind: HostFuncKind, nativeID: nativeFuncArrayNext}
				registers[ins.b] = iterValue
				registers[ins.c] = NilValue()
				break
			}
			generator, state, control, ok, err := prepareIterator(iterValue, thread.globals)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: prepare iterator failed: %w", err))
			}
			if ok {
				registers[ins.a] = generator
				registers[ins.b] = state
				registers[ins.c] = control
			}

		case opArrayNext:
			callee := registers[ins.b]
			if callee.nativeID != nativeFuncArrayNext {
				return directFrameEnterGenericFrame()
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			tableValue := registers[ins.c]
			if tableValue.kind != TableKind || tableValue.table == nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: argument #1 is %s, want table", tableValue.Kind()))
			}
			controlValue := registers[ins.a]
			index := 0
			if !controlValue.IsNil() {
				if controlValue.kind != NumberKind {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want number or nil", controlValue.Kind()))
				}
				index = int(controlValue.number)
				if float64(index) != controlValue.number {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want integer", controlValue.Kind()))
				}
			}
			next := index + 1
			if next < 1 || next > len(tableValue.table.array) {
				registers[ins.a] = NilValue()
				for i := 1; i < ins.d; i++ {
					registers[ins.a+i] = NilValue()
				}
				break
			}
			registers[ins.a] = NumberValue(float64(next))
			if ins.d > 1 {
				registers[ins.a+1] = tableValue.table.array[next-1]
			}
			for i := 2; i < ins.d; i++ {
				registers[ins.a+i] = NilValue()
			}

		case opArrayNextJump2:
			callee := registers[ins.b]
			if callee.nativeID != nativeFuncArrayNext {
				return directFrameEnterGenericFrame()
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			tableValue := registers[ins.c]
			if tableValue.kind != TableKind || tableValue.table == nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: argument #1 is %s, want table", tableValue.Kind()))
			}
			controlValue := registers[ins.a]
			index := 0
			if !controlValue.IsNil() {
				if controlValue.kind != NumberKind {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want number or nil", controlValue.Kind()))
				}
				index = int(controlValue.number)
				if float64(index) != controlValue.number {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want integer", controlValue.Kind()))
				}
			}
			next := index + 1
			if next < 1 || next > len(tableValue.table.array) {
				registers[ins.a] = NilValue()
				registers[ins.a+1] = NilValue()
				frame.pc = ins.d
				continue
			}
			registers[ins.a] = NumberValue(float64(next))
			registers[ins.a+1] = tableValue.table.array[next-1]

		case opAdd:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				registers[ins.a] = NumberValue(left.number + right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number + right.number)

		case opSub:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				registers[ins.a] = NumberValue(left.number - right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number - right.number)

		case opMul:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				registers[ins.a] = NumberValue(left.number * right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number * right.number)

		case opDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				registers[ins.a] = NumberValue(left.number / right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number / right.number)

		case opMod:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				registers[ins.a] = NumberValue(left.number - math.Floor(left.number/right.number)*right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number - math.Floor(left.number/right.number)*right.number)

		case opIDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				registers[ins.a] = NumberValue(math.Floor(left.number / right.number))
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(math.Floor(left.number / right.number))

		case opAddK:
			left := registers[ins.b]
			if proto.numericOperandsProvenAt(frame.pc, ins) && proto.constantNumberOK[ins.c] {
				registers[ins.a] = NumberValue(left.number + proto.constantNumbers[ins.c])
				break
			}
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number + proto.constantNumbers[ins.c])

		case opSubK:
			left := registers[ins.b]
			if proto.numericOperandsProvenAt(frame.pc, ins) && proto.constantNumberOK[ins.c] {
				registers[ins.a] = NumberValue(left.number - proto.constantNumbers[ins.c])
				break
			}
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number - proto.constantNumbers[ins.c])

		case opMulK:
			left := registers[ins.b]
			if proto.numericOperandsProvenAt(frame.pc, ins) && proto.constantNumberOK[ins.c] {
				registers[ins.a] = NumberValue(left.number * proto.constantNumbers[ins.c])
				break
			}
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number * proto.constantNumbers[ins.c])

		case opDivK:
			left := registers[ins.b]
			if proto.numericOperandsProvenAt(frame.pc, ins) && proto.constantNumberOK[ins.c] {
				registers[ins.a] = NumberValue(left.number / proto.constantNumbers[ins.c])
				break
			}
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(left.number / proto.constantNumbers[ins.c])

		case opModK:
			left := registers[ins.b]
			if proto.numericOperandsProvenAt(frame.pc, ins) && proto.constantNumberOK[ins.c] {
				right := proto.constantNumbers[ins.c]
				registers[ins.a] = NumberValue(left.number - math.Floor(left.number/right)*right)
				break
			}
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			right := proto.constantNumbers[ins.c]
			registers[ins.a] = NumberValue(left.number - math.Floor(left.number/right)*right)

		case opIDivK:
			left := registers[ins.b]
			if proto.numericOperandsProvenAt(frame.pc, ins) && proto.constantNumberOK[ins.c] {
				registers[ins.a] = NumberValue(math.Floor(left.number / proto.constantNumbers[ins.c]))
				break
			}
			if left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(math.Floor(left.number / proto.constantNumbers[ins.c]))

		case opAddNumericModK:
			desc := proto.numericAddModOps[ins.c]
			if !proto.constantNumberOK[desc.mul] ||
				!proto.constantNumberOK[desc.idiv] ||
				!proto.constantNumberOK[desc.mod] {
				return directFrameEnterGenericFrame()
			}
			left := registers[ins.a]
			source := registers[ins.b]
			if left.kind != NumberKind || source.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			mul := source.number * proto.constantNumbers[desc.mul]
			idiv := math.Floor(source.number / proto.constantNumbers[desc.idiv])
			beforeMod := mul - idiv
			mod := proto.constantNumbers[desc.mod]
			registers[ins.a] = NumberValue(left.number + beforeMod - math.Floor(beforeMod/mod)*mod)

		case opNeg:
			operand := registers[ins.b]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				registers[ins.a] = NumberValue(-operand.number)
				break
			}
			if operand.kind != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(-operand.number)

		case opEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind == TableKind || right.kind == TableKind || left.kind == UserDataKind || right.kind == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = BoolValue(valuesEqual(left, right))

		case opNotEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if left.kind == TableKind || right.kind == TableKind || left.kind == UserDataKind || right.kind == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = BoolValue(!valuesEqual(left, right))

		case opLess:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				if math.IsNaN(left.number) || math.IsNaN(right.number) {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = BoolValue(left.number < right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = BoolValue(left.number < right.number)

		case opLessEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				if math.IsNaN(left.number) || math.IsNaN(right.number) {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = BoolValue(left.number <= right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = BoolValue(left.number <= right.number)

		case opGreater:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				if math.IsNaN(left.number) || math.IsNaN(right.number) {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = BoolValue(left.number > right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = BoolValue(left.number > right.number)

		case opGreaterEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if proto.numericOperandsProvenAt(frame.pc, ins) {
				if math.IsNaN(left.number) || math.IsNaN(right.number) {
					return directFrameEnterGenericFrame()
				}
				registers[ins.a] = BoolValue(left.number >= right.number)
				break
			}
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = BoolValue(left.number >= right.number)

		case opNumericForCheck:
			loopValue := registers[ins.a]
			limitValue := registers[ins.b]
			stepValue := registers[ins.c]
			if loopValue.kind != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind()))
			}
			if limitValue.kind != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind()))
			}
			if stepValue.kind != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind()))
			}
			if math.IsNaN(loopValue.number) || math.IsNaN(limitValue.number) || math.IsNaN(stepValue.number) {
				return directFrameFail(fmt.Errorf("run: numeric for operand is NaN"))
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
			if left.kind == NumberKind && proto.constantNumberOK[ins.b] {
				if left.number != proto.constantNumbers[ins.b] {
					frame.pc = ins.d
					continue
				}
				break
			}
			right := proto.constants[ins.b]
			if left.kind == StringKind && right.kind == StringKind {
				if left.str != right.str {
					frame.pc = ins.d
					continue
				}
				break
			}
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)

		case opJumpIfTableHasMetatable:
			base := registers[ins.a]
			if base.kind == TableKind && base.table != nil && base.table.metatable != nil {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLessK:
			left := registers[ins.a]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.b] {
				return directFrameEnterGenericFrame()
			}
			right := proto.constantNumbers[ins.b]
			if !math.IsNaN(left.number) && !math.IsNaN(right) && left.number >= right {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLess:
			left := registers[ins.a]
			right := registers[ins.b]
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			if left.number >= right.number {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotGreater:
			left := registers[ins.a]
			right := registers[ins.b]
			if left.kind != NumberKind || right.kind != NumberKind || math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			if left.number <= right.number {
				frame.pc = ins.d
				continue
			}

		case opJumpIfModKNotEqualK:
			left := registers[ins.a]
			if left.kind != NumberKind || !proto.constantNumberOK[ins.b] || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
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
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			if !ok {
				return directFrameEnterGenericFrame()
			}
			right := proto.constants[ins.c]
			if left.kind == TableKind || left.kind == UserDataKind || right.kind == TableKind || right.kind == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			if !valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotEqualK:
			desc := proto.rowFieldEqualOps[ins.b]
			left, ok, targetOK := directFrameRowStringFieldFast(registers[ins.a], proto.constantKeys[desc.field].str, desc.slot)
			if !targetOK {
				base := registers[ins.a]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			if !ok {
				return directFrameEnterGenericFrame()
			}
			right := proto.constants[desc.value]
			if left.kind == TableKind || left.kind == UserDataKind || right.kind == TableKind || right.kind == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			if !valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotEqualField:
			desc := proto.rowFieldPairOps[ins.b]
			left, leftOK, targetOK := directFrameRowStringFieldFast(registers[ins.a], proto.constantKeys[desc.leftField].str, desc.leftSlot)
			if !targetOK {
				base := registers[ins.a]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			if !leftOK {
				return directFrameEnterGenericFrame()
			}
			right, rightOK, targetOK := directFrameRowStringFieldFast(registers[ins.c], proto.constantKeys[desc.rightField].str, desc.rightSlot)
			if !targetOK {
				base := registers[ins.c]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			if !rightOK {
				return directFrameEnterGenericFrame()
			}
			if left.kind == TableKind || left.kind == UserDataKind || right.kind == TableKind || right.kind == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			if !valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldEqualField:
			desc := proto.rowFieldPairOps[ins.b]
			left, leftOK, targetOK := directFrameRowStringFieldFast(registers[ins.a], proto.constantKeys[desc.leftField].str, desc.leftSlot)
			if !targetOK {
				base := registers[ins.a]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			if !leftOK {
				return directFrameEnterGenericFrame()
			}
			right, rightOK, targetOK := directFrameRowStringFieldFast(registers[ins.c], proto.constantKeys[desc.rightField].str, desc.rightSlot)
			if !targetOK {
				base := registers[ins.c]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			if !rightOK {
				return directFrameEnterGenericFrame()
			}
			if left.kind == TableKind || left.kind == UserDataKind || right.kind == TableKind || right.kind == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			if valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
			left, ok, err := directFrameStringField(registers[ins.a], proto.constantKeys[ins.b].str)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			if !ok || left.kind != NumberKind || !proto.constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			right := proto.constantNumbers[ins.c]
			if math.IsNaN(left.number) || math.IsNaN(right) {
				return directFrameEnterGenericFrame()
			}
			greater := left.number > right
			if (ins.op == opJumpIfStringFieldNotGreaterK && !greater) ||
				(ins.op == opJumpIfStringFieldGreaterK && greater) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
			desc := proto.rowFieldEqualOps[ins.b]
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			key := proto.constantKeys[desc.field].str
			left := NilValue()
			ok := true
			if table.stringFieldMap == nil && desc.slot >= 0 && desc.slot < len(table.stringFields) && table.stringFields[desc.slot].key == key {
				left = table.stringFields[desc.slot].value
			} else if field, found := table.rawStringField(key); found {
				left = field
			} else if table.metatable != nil {
				ok = false
			}
			if !ok || left.kind != NumberKind || !proto.constantNumberOK[desc.value] {
				return directFrameEnterGenericFrame()
			}
			right := proto.constantNumbers[desc.value]
			if math.IsNaN(left.number) || math.IsNaN(right) {
				return directFrameEnterGenericFrame()
			}
			greater := left.number > right
			if (ins.op == opJumpIfRowStringFieldNotGreaterK && !greater) ||
				(ins.op == opJumpIfRowStringFieldGreaterK && greater) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterR:
			left, ok, err := directFrameStringField(registers[ins.a], proto.constantKeys[ins.b].str)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			right := registers[ins.c]
			if !ok || left.kind != NumberKind || right.kind != NumberKind ||
				math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			if !(left.number > right.number) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotGreaterR:
			desc := proto.rowFieldRegisterOps[ins.b]
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			key := proto.constantKeys[desc.field].str
			left := NilValue()
			ok := true
			if table.stringFieldMap == nil && desc.slot >= 0 && desc.slot < len(table.stringFields) && table.stringFields[desc.slot].key == key {
				left = table.stringFields[desc.slot].value
			} else if field, found := table.rawStringField(key); found {
				left = field
			} else if table.metatable != nil {
				ok = false
			}
			right := registers[ins.c]
			if !ok || left.kind != NumberKind || right.kind != NumberKind ||
				math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			if !(left.number > right.number) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotLessField:
			desc := proto.rowFieldPairOps[ins.b]
			left, leftOK, targetOK := directFrameRowStringFieldFast(registers[ins.a], proto.constantKeys[desc.leftField].str, desc.leftSlot)
			if !targetOK {
				base := registers[ins.a]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			right, rightOK, targetOK := directFrameRowStringFieldFast(registers[ins.a], proto.constantKeys[desc.rightField].str, desc.rightSlot)
			if !targetOK {
				base := registers[ins.a]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			if !leftOK || !rightOK || left.kind != NumberKind || right.kind != NumberKind ||
				math.IsNaN(left.number) || math.IsNaN(right.number) {
				return directFrameEnterGenericFrame()
			}
			if !(left.number < right.number) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldFalse:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			key := proto.constantKeys[ins.b].str
			value := NilValue()
			if table.stringFieldMap == nil && ins.c >= 0 && ins.c < len(table.stringFields) && table.stringFields[ins.c].key == key {
				value = table.stringFields[ins.c].value
			} else if field, ok := table.rawStringField(key); ok {
				value = field
			} else if table.metatable != nil {
				return directFrameEnterGenericFrame()
			}
			if !value.truthy() {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNil:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			key := proto.constantKeys[ins.b].str
			value := NilValue()
			if table.stringFieldMap == nil && ins.c >= 0 && ins.c < len(table.stringFields) && table.stringFields[ins.c].key == key {
				value = table.stringFields[ins.c].value
			} else if field, ok := table.rawStringField(key); ok {
				value = field
			} else if table.metatable != nil {
				return directFrameEnterGenericFrame()
			}
			if value.IsNil() {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotNil:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			key := proto.constantKeys[ins.b].str
			value := NilValue()
			if table.stringFieldMap == nil && ins.c >= 0 && ins.c < len(table.stringFields) && table.stringFields[ins.c].key == key {
				value = table.stringFields[ins.c].value
			} else if field, ok := table.rawStringField(key); ok {
				value = field
			} else if table.metatable != nil {
				return directFrameEnterGenericFrame()
			}
			if !value.IsNil() {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldTrue:
			base := registers[ins.a]
			if base.kind != TableKind || base.table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			table := base.table
			key := proto.constantKeys[ins.b].str
			value := NilValue()
			if table.stringFieldMap == nil && ins.c >= 0 && ins.c < len(table.stringFields) && table.stringFields[ins.c].key == key {
				value = table.stringFields[ins.c].value
			} else if field, ok := table.rawStringField(key); ok {
				value = field
			} else if table.metatable != nil {
				return directFrameEnterGenericFrame()
			}
			if value.truthy() {
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
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
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
			if resultCount == 1 && callee.nativeID == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[ins.b+1 : ins.b+1+ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				registers[ins.a] = value
				break
			}
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)

		case opCallOne:
			callee := registers[ins.b]
			if callee.nativeID == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[ins.b+1 : ins.b+1+ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				registers[ins.a] = value
				break
			}
			return directFrameEnterGenericFrame()

		case opCallLocalOne:
			callee := registers[ins.b]
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			args := registers[ins.c : ins.c+ins.d]
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
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(err)
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			registers[ins.a] = value
			continue

		case opCallTableFieldKeyOne:
			argCount := tableFieldKeyCallArgCount(ins.d)
			keySource := ins.a + argCount + 1
			keyValue, ok, targetOK := directFrameRowStringFieldFast(registers[keySource], proto.constantKeys[ins.c].str, tableFieldKeyCallKeySlot(ins.d))
			if !targetOK {
				base := registers[keySource]
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			if !ok || keyValue.kind != StringKind {
				if !ok {
					picCounts.addMetatableMiss()
				} else {
					picCounts.addInvalidKeyFallback()
				}
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			handlerTableValue := registers[ins.b]
			if handlerTableValue.kind != TableKind || handlerTableValue.table == nil {
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", handlerTableValue.Kind()))
			}
			handlerTable := handlerTableValue.table
			if handlerTable.metatable != nil {
				picCounts.addMetatableMiss()
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			closure, ok := frame.tableCallCache.getCounted(handlerTable, keyValue.str, picCounts)
			if !ok {
				callee, ok := handlerTable.rawStringField(keyValue.str)
				if !ok {
					picCounts.addMissingKeyFallback()
					return directFrameEnterGenericFrame()
				}
				closure, ok = callee.scriptFunction()
				if !ok {
					return directFrameEnterGenericFrame()
				}
				if frame.tableCallCache == nil {
					frame.tableCallCache = &tableFieldCallCache{}
				}
				frame.tableCallCache.store(handlerTable, keyValue.str, closure)
			}
			if argCount == 2 {
				if value, ok := directFrameApplyFastMethodFieldAdd(closure, registers[ins.a+1], registers[ins.a+2]); ok {
					frame.openCallStart = -1
					frame.openCallResults = nil
					registers[ins.a] = value
					frame.pc++
					continue
				}
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
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(err)
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			registers[ins.a] = value
			continue

		case opTableInsert:
			callee, fast, err := tableIntrinsicCallee(thread.globals, "insert")
			if err != nil {
				return directFrameFail(err)
			}
			if !fast {
				if ins.d > 0 {
					picCounts.addSideExit(directFrameSideExitReasonIntrinsic)
					results, ok, err := directFrameNonYieldingCallIsland(callee, thread.globals, registers[ins.a:ins.a+ins.b])
					if err != nil {
						return directFrameFail(fmt.Errorf("run: call failed: %w", err))
					}
					if ok {
						directFrameApplyCallIslandResults(frame, registers, ins.a, ins.d, results)
						break
					}
				}
				return directFrameEnterGenericFrame()
			}
			if _, err := baseTableInsert(registers[ins.a : ins.a+ins.b]); err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			directFrameApplyCallIslandResults(frame, registers, ins.a, ins.d, nil)

		case opTableRemove:
			callee, fast, err := tableIntrinsicCallee(thread.globals, "remove")
			if err != nil {
				return directFrameFail(err)
			}
			if !fast {
				if ins.d > 0 {
					picCounts.addSideExit(directFrameSideExitReasonIntrinsic)
					results, ok, err := directFrameNonYieldingCallIsland(callee, thread.globals, registers[ins.a:ins.a+ins.b])
					if err != nil {
						return directFrameFail(fmt.Errorf("run: call failed: %w", err))
					}
					if ok {
						directFrameApplyCallIslandResults(frame, registers, ins.a, ins.d, results)
						break
					}
				}
				return directFrameEnterGenericFrame()
			}
			removed, err := baseTableRemoveValue(registers[ins.a : ins.a+ins.b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			if ins.d > 0 {
				registers[ins.a] = removed
				for i := 1; i < ins.d; i++ {
					registers[ins.a+i] = NilValue()
				}
			}

		case opMathMin:
			callee, fast, err := mathIntrinsicCallee(thread.globals, "min")
			if err != nil {
				return directFrameFail(err)
			}
			if !fast || ins.d != 1 {
				if !fast && ins.d == 1 {
					picCounts.addSideExit(directFrameSideExitReasonIntrinsic)
					results, ok, err := directFrameNonYieldingCallIsland(callee, thread.globals, registers[ins.a:ins.a+ins.b])
					if err != nil {
						return directFrameFail(fmt.Errorf("run: call failed: %w", err))
					}
					if ok {
						directFrameApplyCallIslandResults(frame, registers, ins.a, 1, results)
						break
					}
				}
				return directFrameEnterGenericFrame()
			}
			minimum, err := baseMathMinValue(registers[ins.a : ins.a+ins.b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			frame.openCallStart = -1
			frame.openCallResults = nil
			registers[ins.a] = NumberValue(minimum)

		case opReturnOne:
			return directFrameReturn(vmReturnedValue(registers[ins.a]))

		case opReturn:
			count := ins.b
			if count < 0 {
				return directFrameEnterGenericFrame()
			}
			if count == 0 {
				return directFrameReturn(vmReturnedValues(nil))
			}
			if count == 1 {
				return directFrameReturn(vmReturnedValue(registers[ins.a]))
			}
			results := make([]Value, count)
			copy(results, registers[ins.a:ins.a+count])
			return directFrameReturn(vmReturnedValues(results))

		default:
			return directFrameEnterGenericFrame()
		}
		frame.pc++
	}

	return directFrameReturn(vmReturnedValues(nil))
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

		case opArrayNext:
			callee := frame.register(ins.b)
			destination := vmResultDestination{register: ins.a, count: ins.d}
			if callee.nativeID == nativeFuncArrayNext {
				var tableValue Value
				var controlValue Value
				if frame.directRegisters {
					tableValue = frame.registers[ins.c]
					controlValue = frame.registers[ins.a]
				} else {
					tableValue = frame.register(ins.c)
					controlValue = frame.register(ins.a)
				}
				results, count, err := baseArrayNextInline(tableValue, controlValue)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					for i := 0; i < ins.d; i++ {
						if i >= count {
							frame.registers[ins.a+i] = NilValue()
						} else {
							frame.registers[ins.a+i] = results[i]
						}
					}
				} else {
					frame.applyInlineResultDestination(destination, results, count)
				}
				break
			}
			args := frame.scriptCallArgs(ins.c, 2)
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

		case opArrayNextJump2:
			callee := frame.register(ins.b)
			destination := vmResultDestination{register: ins.a, count: 2}
			if callee.nativeID == nativeFuncArrayNext {
				var tableValue Value
				var controlValue Value
				if frame.directRegisters {
					tableValue = frame.registers[ins.c]
					controlValue = frame.registers[ins.a]
				} else {
					tableValue = frame.register(ins.c)
					controlValue = frame.register(ins.a)
				}
				results, count, err := baseArrayNextInline(tableValue, controlValue)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.openCallStart = -1
				frame.openCallResults = nil
				if frame.directRegisters {
					for i := 0; i < 2; i++ {
						if i >= count {
							frame.registers[ins.a+i] = NilValue()
						} else {
							frame.registers[ins.a+i] = results[i]
						}
					}
				} else {
					frame.applyInlineResultDestination(destination, results, count)
				}
			} else {
				args := frame.scriptCallArgs(ins.c, 2)
				if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
					return result, err
				}
			}
			if frame.register(ins.a).IsNil() {
				frame.pc = ins.d
				continue
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
					if err := table.rawSetKey(key, value); err != nil {
						return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
					}
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
					table.setRawRowStringField(rowStringFieldSlotRefFromIndex(ins.d), key, value)
					break
				}
			}
			table, ok := frame.register(ins.a).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", frame.register(ins.a).Kind())
			}
			value := frame.register(ins.c)
			if table.metatable == nil {
				table.setRawRowStringField(rowStringFieldSlotRefFromIndex(ins.d), key, value)
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

		case opSetStringFieldIndex:
			firstKey := proto.constantKeys[ins.b].str
			if frame.directRegisters {
				base := frame.registers[ins.a]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				table := base.table
				if first, ok := table.rawStringField(firstKey); ok {
					if first.kind != TableKind || first.table == nil {
						return vmFrameResult{}, fmt.Errorf("run: set index target is %s, want table", first.Kind())
					}
					nextTable := first.table
					if nextTable.metatable == nil {
						if err := nextTable.rawSet(frame.registers[ins.c], frame.registers[ins.d]); err != nil {
							return vmFrameResult{}, fmt.Errorf("run: set index failed: %w", err)
						}
						break
					}
				} else if table.metatable == nil {
					return vmFrameResult{}, fmt.Errorf("run: set index target is %s, want table", NilValue().Kind())
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
				return vmFrameResult{}, fmt.Errorf("run: set index target is %s, want table", first.Kind())
			}
			if err := access.set(nextTable, frame.register(ins.c), frame.register(ins.d)); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set index failed: %w", err)
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
					targetRef := rowStringFieldSlotRefFromIndex(desc.targetSlot)
					addRef := rowStringFieldSlotRefFromIndex(desc.addSlot)
					left, leftOK := table.rawRowStringField(targetRef, targetKey)
					add, addOK := table.rawRowStringField(addRef, addKey)
					if leftOK && addOK &&
						left.kind == NumberKind &&
						subtract.kind == NumberKind &&
						add.kind == NumberKind {
						table.setRawRowStringField(targetRef, targetKey, NumberValue(left.number-subtract.number+add.number))
						break
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
					if value, ok := table.rawGenericField(key); ok {
						frame.registers[ins.a] = value
						break
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
						if value, ok := indexTable.rawGenericField(key); ok {
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
			value, err := vmRowStringField(globals, table, proto.constants[ins.c], key, ins.d)
			if err != nil {
				return vmFrameResult{}, err
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

		case opGetStringFieldIndex:
			firstKey := proto.constantKeys[ins.c].str
			if frame.directRegisters {
				base := frame.registers[ins.b]
				if base.kind != TableKind || base.table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				if first, ok := table.rawStringField(firstKey); ok {
					if first.kind != TableKind || first.table == nil {
						return vmFrameResult{}, fmt.Errorf("run: get index target is %s, want table", first.Kind())
					}
					nextTable := first.table
					if nextTable.metatable == nil {
						value, err := nextTable.rawGet(frame.registers[ins.d])
						if err != nil {
							return vmFrameResult{}, fmt.Errorf("run: get index failed: %w", err)
						}
						frame.registers[ins.a] = value
						break
					}
				} else if table.metatable == nil {
					return vmFrameResult{}, fmt.Errorf("run: get index target is %s, want table", NilValue().Kind())
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
				return vmFrameResult{}, fmt.Errorf("run: get index target is %s, want table", first.Kind())
			}
			value, err := access.get(nextTable, frame.register(ins.d))
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: get index failed: %w", err)
			}
			if frame.directRegisters {
				frame.registers[ins.a] = value
				break
			}
			frame.setRegister(ins.a, value)

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
				right := proto.constants[ins.b]
				if left.kind == StringKind && right.kind == StringKind {
					if left.str != right.str {
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

		case opJumpIfTableHasMetatable:
			base := frame.register(ins.a)
			if base.kind == TableKind && base.table != nil && base.table.metatable != nil {
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

		case opJumpIfNotLess:
			if frame.directRegisters {
				left := frame.registers[ins.a]
				right := frame.registers[ins.b]
				if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
					if left.number >= right.number {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := frame.register(ins.b)
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

		case opJumpIfNotGreater:
			if frame.directRegisters {
				left := frame.registers[ins.a]
				right := frame.registers[ins.b]
				if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
					if left.number <= right.number {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := frame.register(ins.b)
			if left.kind == NumberKind && right.kind == NumberKind && !math.IsNaN(left.number) && !math.IsNaN(right.number) {
				if left.number <= right.number {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := lessValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
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
			left, err := vmRowStringField(globals, table, proto.constants[desc.field], key, desc.slot)
			if err != nil {
				return vmFrameResult{}, err
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

		case opJumpIfRowStringFieldNotEqualField:
			desc := proto.rowFieldPairOps[ins.b]
			getRowField := func(register int, fieldConstant int, slotIndex int) (Value, error) {
				var base Value
				if frame.directRegisters {
					base = frame.registers[register]
				} else {
					base = frame.register(register)
				}
				if base.kind != TableKind || base.table == nil {
					return NilValue(), fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				key := proto.constantKeys[fieldConstant].str
				return vmRowStringField(globals, table, proto.constants[fieldConstant], key, slotIndex)
			}
			left, err := getRowField(ins.a, desc.leftField, desc.leftSlot)
			if err != nil {
				return vmFrameResult{}, err
			}
			right, err := getRowField(ins.c, desc.rightField, desc.rightSlot)
			if err != nil {
				return vmFrameResult{}, err
			}
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

		case opJumpIfRowStringFieldEqualField:
			desc := proto.rowFieldPairOps[ins.b]
			getRowField := func(register int, fieldConstant int, slotIndex int) (Value, error) {
				var base Value
				if frame.directRegisters {
					base = frame.registers[register]
				} else {
					base = frame.register(register)
				}
				if base.kind != TableKind || base.table == nil {
					return NilValue(), fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				table := base.table
				key := proto.constantKeys[fieldConstant].str
				return vmRowStringField(globals, table, proto.constants[fieldConstant], key, slotIndex)
			}
			left, err := getRowField(ins.a, desc.leftField, desc.leftSlot)
			if err != nil {
				return vmFrameResult{}, err
			}
			right, err := getRowField(ins.c, desc.rightField, desc.rightSlot)
			if err != nil {
				return vmFrameResult{}, err
			}
			if left.kind == StringKind && right.kind == StringKind {
				if left.str == right.str {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := equalValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: equal failed: %w", err)
			}
			if value {
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

		case opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
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
			left, err := vmRowStringField(globals, table, proto.constants[desc.field], key, desc.slot)
			if err != nil {
				return vmFrameResult{}, err
			}
			if left.kind == NumberKind && proto.constantNumberOK[desc.value] {
				right := proto.constantNumbers[desc.value]
				if !math.IsNaN(left.number) && !math.IsNaN(right) {
					greater := left.number > right
					if (ins.op == opJumpIfRowStringFieldNotGreaterK && !greater) ||
						(ins.op == opJumpIfRowStringFieldGreaterK && greater) {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			right := proto.constants[desc.value]
			greater, err := lessValue(right, left, globals)
			if err != nil {
				if ins.op == opJumpIfRowStringFieldGreaterK {
					return vmFrameResult{}, fmt.Errorf("run: less equal failed: %w", err)
				}
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
			}
			if (ins.op == opJumpIfRowStringFieldNotGreaterK && !greater) ||
				(ins.op == opJumpIfRowStringFieldGreaterK && greater) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterR:
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
			var right Value
			if frame.directRegisters {
				right = frame.registers[ins.c]
			} else {
				right = frame.register(ins.c)
			}
			if left.kind == NumberKind && right.kind == NumberKind &&
				!math.IsNaN(left.number) && !math.IsNaN(right.number) {
				if !(left.number > right.number) {
					frame.pc = ins.d
					continue
				}
				break
			}
			greater, err := lessValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
			}
			if !greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotGreaterR:
			desc := proto.rowFieldRegisterOps[ins.b]
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
			left, err := vmRowStringField(globals, table, proto.constants[desc.field], key, desc.slot)
			if err != nil {
				return vmFrameResult{}, err
			}
			var right Value
			if frame.directRegisters {
				right = frame.registers[ins.c]
			} else {
				right = frame.register(ins.c)
			}
			if left.kind == NumberKind && right.kind == NumberKind &&
				!math.IsNaN(left.number) && !math.IsNaN(right.number) {
				if !(left.number > right.number) {
					frame.pc = ins.d
					continue
				}
				break
			}
			greater, err := lessValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
			}
			if !greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfRowStringFieldNotLessField:
			desc := proto.rowFieldPairOps[ins.b]
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
			getRowField := func(fieldConstant int, slotIndex int) (Value, error) {
				key := proto.constantKeys[fieldConstant].str
				return vmRowStringField(globals, table, proto.constants[fieldConstant], key, slotIndex)
			}
			left, err := getRowField(desc.leftField, desc.leftSlot)
			if err != nil {
				return vmFrameResult{}, err
			}
			right, err := getRowField(desc.rightField, desc.rightSlot)
			if err != nil {
				return vmFrameResult{}, err
			}
			if left.kind == NumberKind && right.kind == NumberKind &&
				!math.IsNaN(left.number) && !math.IsNaN(right.number) {
				if !(left.number < right.number) {
					frame.pc = ins.d
					continue
				}
				break
			}
			less, err := lessValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: less failed: %w", err)
			}
			if !less {
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
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

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
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

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
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

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
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

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
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

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

			args := frame.retainedFixedCallArgs(ins.c, ins.d).values
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
					return vmYieldedValues(yield.values), nil
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

			args := frame.retainedFixedCallArgs(ins.c, ins.d).values
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
					return vmYieldedValues(yield.values), nil
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

			args := frame.retainedFixedCallArgs(ins.c, ins.d).values
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
					return vmYieldedValues(yield.values), nil
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
					return vmYieldedValues(yield.values), nil
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
					return vmYieldedValues(yield.values), nil
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
					return vmYieldedValues(yield.values), nil
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
				args = frame.retainedFixedCallArgs(ins.b+1, ins.c).values
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
					return vmYieldedValues(yield.values), nil
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
					args = frame.retainedFixedCallArgs(ins.b+1, ins.c).values
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
					return vmYieldedValues(yield.values), nil
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
				if field, ok := table.rawRowStringField(rowStringFieldSlotRefFromIndex(ins.c), key); ok {
					value = field
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

		case opJumpIfStringFieldNil:
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
				if field, ok := table.rawRowStringField(rowStringFieldSlotRefFromIndex(ins.c), key); ok {
					value = field
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
			if value.IsNil() {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotNil:
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
				if field, ok := table.rawRowStringField(rowStringFieldSlotRefFromIndex(ins.c), key); ok {
					value = field
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
			if !value.IsNil() {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldTrue:
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
				if field, ok := table.rawRowStringField(rowStringFieldSlotRefFromIndex(ins.c), key); ok {
					value = field
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
			if value.truthy() {
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
	return frame.borrowedFixedCallArgs(start, count).values
}

type vmFixedArgWindow struct {
	values   []Value
	borrowed bool
}

func (frame *vmFrame) borrowedFixedCallArgs(start int, count int) vmFixedArgWindow {
	if count == 0 {
		return vmFixedArgWindow{}
	}
	if !frame.hasCellsInRange(start, count) {
		return vmFixedArgWindow{values: frame.registers[start : start+count], borrowed: true}
	}
	return frame.retainedFixedCallArgs(start, count)
}

func (frame *vmFrame) retainedFixedCallArgs(start int, count int) vmFixedArgWindow {
	return vmFixedArgWindow{values: frame.copiedCallArgs(start, count)}
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
			results, err := callRuntimeMetamethod1(metamethod, globals, value)
			if err != nil {
				return NilValue(), NilValue(), NilValue(), false, err
			}
			return adjustedResultAt(results, 0), adjustedResultAt(results, 1), adjustedResultAt(results, 2), true, nil
		}
	}

	if tableCanIterateCleanArray(table) {
		return Value{kind: HostFuncKind, nativeID: nativeFuncArrayNext}, TableValue(table), NilValue(), true, nil
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
			results, err := callRuntimeMetamethod1(metamethod, globals, value)
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
	results, err := callRuntimeMetamethod1(metamethod, globals, value)
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
	results, err := callRuntimeMetamethod2(metamethod, globals, left, right)
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

func callRuntimeMetamethod1(fn Value, globals *globalEnv, first Value) ([]Value, error) {
	args := [1]Value{first}
	return callRuntimeMetamethod(fn, globals, args[:])
}

func callRuntimeMetamethod2(fn Value, globals *globalEnv, first Value, second Value) ([]Value, error) {
	args := [2]Value{first, second}
	return callRuntimeMetamethod(fn, globals, args[:])
}

func callRuntimeMetamethod3(fn Value, globals *globalEnv, first Value, second Value, third Value) ([]Value, error) {
	args := [3]Value{first, second, third}
	return callRuntimeMetamethod(fn, globals, args[:])
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
	key := baseFieldIntrinsicGuardKey{globalName: globalName, field: field}
	thread := activeThread(globals)
	if guard, ok := thread.baseFieldIntrinsicGuard(key, globals); ok {
		return guard.callee, true, nil
	}
	if globals == nil || globals.values == nil {
		return Value{kind: HostFuncKind, nativeID: intrinsic.nativeID}, true, nil
	}
	tableValue, ok := globals.values[globalName]
	if !ok {
		callee := Value{kind: HostFuncKind, nativeID: intrinsic.nativeID}
		thread.storeBaseFieldIntrinsicGuard(key, globals, nil, callee)
		return callee, true, nil
	}
	table, ok := tableValue.Table()
	if !ok {
		thread.clearBaseFieldIntrinsicGuard(key)
		return NilValue(), false, fmt.Errorf("run: get field target is %s, want table", tableValue.Kind())
	}
	if callee, ok := table.rawStringField(field); ok {
		fast := callee.nativeID == intrinsic.nativeID
		if fast {
			thread.storeBaseFieldIntrinsicGuard(key, globals, table, callee)
		} else {
			thread.clearBaseFieldIntrinsicGuard(key)
		}
		return callee, fast, nil
	}
	callee, err := runtimeTableAccess(globals).get(table, StringValue(field))
	if err != nil {
		thread.clearBaseFieldIntrinsicGuard(key)
		return NilValue(), false, fmt.Errorf("run: get field failed: %w", err)
	}
	thread.clearBaseFieldIntrinsicGuard(key)
	return callee, callee.nativeID == intrinsic.nativeID, nil
}

func (thread *vmThread) baseFieldIntrinsicGuard(key baseFieldIntrinsicGuardKey, globals *globalEnv) (baseFieldIntrinsicGuardEntry, bool) {
	if thread != nil {
		thread.directFramePICCounts.addIntrinsicGuardCheck()
	}
	if thread == nil || globals == nil || thread.intrinsicGuards == nil {
		if thread != nil {
			thread.directFramePICCounts.addIntrinsicGuardMiss()
		}
		return baseFieldIntrinsicGuardEntry{}, false
	}
	cache := thread.intrinsicGuards
	for i := 0; i < int(cache.count); i++ {
		entry := cache.entries[i]
		if entry.key != key {
			continue
		}
		if entry.envVersion != globals.version {
			thread.directFramePICCounts.addIntrinsicGuardMiss()
			return baseFieldIntrinsicGuardEntry{}, false
		}
		if entry.table != nil && !entry.token.matchesTableValues(entry.table) {
			thread.directFramePICCounts.addIntrinsicGuardMiss()
			return baseFieldIntrinsicGuardEntry{}, false
		}
		cache.hits++
		thread.directFramePICCounts.addIntrinsicGuardHit()
		return entry, true
	}
	thread.directFramePICCounts.addIntrinsicGuardMiss()
	return baseFieldIntrinsicGuardEntry{}, false
}

func (thread *vmThread) storeBaseFieldIntrinsicGuard(key baseFieldIntrinsicGuardKey, globals *globalEnv, table *Table, callee Value) {
	if thread == nil || globals == nil {
		return
	}
	if thread.intrinsicGuards == nil {
		thread.intrinsicGuards = &baseFieldIntrinsicGuardCache{}
	}
	cache := thread.intrinsicGuards
	cache.resolutions++
	entry := baseFieldIntrinsicGuardEntry{
		key:        key,
		envVersion: globals.version,
		table:      table,
		callee:     callee,
	}
	if table != nil {
		entry.token = table.stringShapeToken()
	}
	for i := 0; i < int(cache.count); i++ {
		if cache.entries[i].key == key {
			cache.entries[i] = entry
			return
		}
	}
	if int(cache.count) >= len(cache.entries) {
		cache.entries[0] = entry
		return
	}
	cache.entries[cache.count] = entry
	cache.count++
}

func (thread *vmThread) clearBaseFieldIntrinsicGuard(key baseFieldIntrinsicGuardKey) {
	if thread == nil || thread.intrinsicGuards == nil {
		return
	}
	cache := thread.intrinsicGuards
	for i := 0; i < int(cache.count); i++ {
		if cache.entries[i].key != key {
			continue
		}
		last := int(cache.count) - 1
		cache.entries[i] = cache.entries[last]
		cache.entries[last] = baseFieldIntrinsicGuardEntry{}
		cache.count--
		return
	}
}

func (thread *vmThread) getRuntimePathCache(pc int, base *Table, firstKey string, secondKey string) (Value, bool) {
	hit, ok := thread.getRuntimePathCacheHit(pc, base, firstKey, secondKey)
	if !ok {
		return NilValue(), false
	}
	return hit.value, true
}

func (thread *vmThread) getRuntimePathCacheHit(pc int, base *Table, firstKey string, secondKey string) (runtimePathCacheHit, bool) {
	if thread == nil {
		return runtimePathCacheHit{}, false
	}
	for i := 0; i < int(thread.runtimePathCount); i++ {
		entry := thread.runtimePaths[i]
		if entry.dynamic || entry.pc != pc || entry.base != base || entry.firstKey != firstKey || entry.secondKey != secondKey {
			continue
		}
		first, ok := base.rawStringFieldAtSlot(entry.firstSlot, firstKey)
		if !ok || first.kind != TableKind || first.table != entry.child {
			thread.directFramePICCounts.addPathCacheStale()
			return runtimePathCacheHit{}, false
		}
		value, ok := entry.child.rawStringFieldAtSlot(entry.secondSlot, secondKey)
		if !ok {
			thread.directFramePICCounts.addPathCacheStale()
			return runtimePathCacheHit{}, false
		}
		thread.runtimePathHits++
		thread.directFramePICCounts.addPathCacheHit()
		return runtimePathCacheHit{
			child:      entry.child,
			secondSlot: entry.secondSlot,
			value:      value,
		}, true
	}
	thread.directFramePICCounts.addPathCacheMiss()
	return runtimePathCacheHit{}, false
}

func (thread *vmThread) writeRuntimePathCache(pc int, base *Table, firstKey string, secondKey string, value Value) bool {
	if value.IsNil() {
		thread.directFramePICCounts.addNilWriteFallback()
		return false
	}
	hit, ok := thread.getRuntimePathCacheHit(pc, base, firstKey, secondKey)
	if !ok {
		return false
	}
	return hit.child.setRawStringFieldAtSlot(hit.secondSlot, secondKey, value)
}

func (thread *vmThread) storeRuntimePathCache(pc int, base *Table, firstKey string, firstSlot tableStringFieldSlot, child *Table, secondKey string, secondSlot tableStringFieldSlot) {
	if thread == nil {
		return
	}
	thread.runtimePathStores++
	thread.directFramePICCounts.addPathCacheStore()
	entry := runtimePathCacheEntry{
		pc:         pc,
		dynamic:    false,
		base:       base,
		firstKey:   firstKey,
		firstSlot:  firstSlot,
		child:      child,
		secondKey:  secondKey,
		secondSlot: secondSlot,
	}
	for i := 0; i < int(thread.runtimePathCount); i++ {
		if runtimePathCacheSamePath(thread.runtimePaths[i], entry) {
			thread.runtimePaths[i] = entry
			return
		}
	}
	if int(thread.runtimePathCount) >= len(thread.runtimePaths) {
		thread.runtimePaths[0] = entry
		return
	}
	thread.runtimePaths[thread.runtimePathCount] = entry
	thread.runtimePathCount++
}

func (thread *vmThread) storeRuntimePathCacheFromResolved(pc int, base *Table, firstKey string, child *Table, secondKey string) {
	firstSlot, firstOK := base.rawStringFieldSlot(firstKey)
	if !firstOK {
		return
	}
	secondSlot, secondOK := child.rawStringFieldSlot(secondKey)
	if !secondOK {
		return
	}
	thread.storeRuntimePathCache(pc, base, firstKey, firstSlot, child, secondKey, secondSlot)
}

func runtimePathCacheSamePath(left runtimePathCacheEntry, right runtimePathCacheEntry) bool {
	return left.pc == right.pc &&
		left.dynamic == right.dynamic &&
		left.base == right.base &&
		left.firstKey == right.firstKey &&
		left.secondKey == right.secondKey
}

func (thread *vmThread) getRuntimeDynamicPathCache(pc int, base *Table, firstKey string) (*Table, bool) {
	if thread == nil {
		return nil, false
	}
	for i := 0; i < int(thread.runtimePathCount); i++ {
		entry := thread.runtimePaths[i]
		if !entry.dynamic || entry.pc != pc || entry.base != base || entry.firstKey != firstKey {
			continue
		}
		first, ok := base.rawStringFieldAtSlot(entry.firstSlot, firstKey)
		if !ok || first.kind != TableKind || first.table != entry.child {
			thread.directFramePICCounts.addPathCacheStale()
			return nil, false
		}
		thread.runtimePathHits++
		thread.directFramePICCounts.addPathCacheHit()
		return entry.child, true
	}
	thread.directFramePICCounts.addPathCacheMiss()
	return nil, false
}

func (thread *vmThread) storeRuntimeDynamicPathCache(pc int, base *Table, firstKey string, firstSlot tableStringFieldSlot, child *Table) {
	if thread == nil {
		return
	}
	thread.runtimePathStores++
	thread.directFramePICCounts.addPathCacheStore()
	entry := runtimePathCacheEntry{
		pc:        pc,
		dynamic:   true,
		base:      base,
		firstKey:  firstKey,
		firstSlot: firstSlot,
		child:     child,
	}
	for i := 0; i < int(thread.runtimePathCount); i++ {
		if runtimePathCacheSamePath(thread.runtimePaths[i], entry) {
			thread.runtimePaths[i] = entry
			return
		}
	}
	if int(thread.runtimePathCount) >= len(thread.runtimePaths) {
		thread.runtimePaths[0] = entry
		return
	}
	thread.runtimePaths[thread.runtimePathCount] = entry
	thread.runtimePathCount++
}

func (proto *Proto) pathFactAllowsStringField2(pc int, ins instruction) bool {
	if proto == nil || len(proto.pathFacts) == 0 {
		return false
	}
	for _, fact := range proto.pathFacts {
		if fact.dynamic || fact.second < 0 {
			continue
		}
		if pc < fact.loopStart || pc >= fact.loopEnd {
			continue
		}
		if fact.base == ins.b && fact.field == ins.c && fact.second == ins.d {
			return true
		}
		if fact.base != ins.b {
			continue
		}
		if fact.field >= 0 && fact.field < len(proto.constants) &&
			ins.c >= 0 && ins.c < len(proto.constants) &&
			proto.constants[fact.field].kind == StringKind &&
			proto.constants[ins.c].kind == StringKind &&
			proto.constants[fact.field].str == proto.constants[ins.c].str &&
			fact.second >= 0 && fact.second < len(proto.constants) &&
			ins.d >= 0 && ins.d < len(proto.constants) &&
			proto.constants[fact.second].kind == StringKind &&
			proto.constants[ins.d].kind == StringKind &&
			proto.constants[fact.second].str == proto.constants[ins.d].str {
			return true
		}
	}
	return false
}

func (proto *Proto) pathPlanCacheAllowsStringField2(pc int, access string, base int, field int, second int) bool {
	if proto == nil || len(proto.pathPlans) == 0 {
		return false
	}
	for _, plan := range proto.pathPlans {
		if plan.pc != pc ||
			plan.access != access ||
			plan.dynamic ||
			plan.loopStart < 0 ||
			plan.base != base {
			continue
		}
		if sameStringConstant(proto, plan.field, field) && sameStringConstant(proto, plan.second, second) {
			return true
		}
	}
	return false
}

func (proto *Proto) pathFactAllowsStringFieldIndex(pc int, ins instruction) bool {
	if proto == nil || len(proto.pathFacts) == 0 {
		return false
	}
	for _, fact := range proto.pathFacts {
		if !fact.dynamic || fact.second >= 0 {
			continue
		}
		if pc < fact.loopStart || pc >= fact.loopEnd {
			continue
		}
		if fact.base == ins.b && sameStringConstant(proto, fact.field, ins.c) {
			return true
		}
	}
	return false
}

func (proto *Proto) pathPlanCacheAllowsStringFieldIndex(pc int, access string, base int, field int) bool {
	if proto == nil || len(proto.pathPlans) == 0 {
		return false
	}
	for _, plan := range proto.pathPlans {
		if plan.pc != pc ||
			plan.access != access ||
			!plan.dynamic ||
			plan.loopStart < 0 ||
			plan.base != base {
			continue
		}
		if sameStringConstant(proto, plan.field, field) {
			return true
		}
	}
	return false
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
			globals.thread.directFramePICCounts.addFixedCallFrameMaterialization()
			globals.thread.directFramePICCounts.addFixedCallArgCopies(fixedCallParamCopyCount(closure.proto, args))
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
