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
	if proto == nil {
		return nil, fmt.Errorf("run: nil prototype")
	}
	if proto.verifyErr != nil {
		return nil, fmt.Errorf("run: invalid prototype: %w", proto.verifyErr)
	}

	return executeProto(context.Background(), proto, nil, executeOptions{
		maxInstructions: -1,
	})
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

	var env *globalEnv
	if globals != nil {
		env = runtimeGlobals(globals)
	}
	return executeProto(context.Background(), proto, env, executeOptions{
		maxInstructions: -1,
	})
}

type executeOptions struct {
	args            []Value
	upvalues        []*cell
	upvalueValues   []Value
	upvalueValueOK  []bool
	maxInstructions int
}

func executeProto(ctx context.Context, proto *Proto, globals *globalEnv, options executeOptions) ([]Value, error) {
	thread := acquireVMThread(ctx, globals)
	defer releaseVMThread(thread)
	thread.instructionBudget = options.maxInstructions
	return thread.runWithUpvalues(proto, options.args, options.upvalues, options.upvalueValues, options.upvalueValueOK)
}

var vmThreadPool = sync.Pool{
	New: func() any {
		thread := newVMThreadWithContext(context.Background(), nil)
		return &thread
	},
}

type vmThread struct {
	ctx         context.Context
	globals     *globalEnv
	baseGlobals globalEnv
	frames      []*vmFrame
	frameSlots  []*vmFrame
	stackOwner  *vmStackOwner
	// stack is kept as a short-lived view for existing runtime helpers and
	// diagnostics.  stackOwner.values is the owning storage; both slices are
	// updated together whenever the window grows or is truncated.
	stack []Value
	// nearestProtectedFrame is the depth of the innermost frame carrying a
	// protected pending call. A negative value means that no protected call is
	// active. The marker is maintained as frames are installed, completed, and
	// unwound so recovery does not need to scan the frame stack.
	nearestProtectedFrame    int
	protectedRecoveryLookups uint64
	protectedRecoveryScans   uint64
	protectedRecoveryErrors  uint64
	instructionBudget        int
	coroutine                *vmCoroutine
	nonYieldableDepth        int
	debugHook                vmDebugHook
	debugCountInterval       int
	debugInstructionCount    int
	debugLineHook            bool
	debugCallHook            bool
	debugReturnHook          bool

	maxFrames               int
	directFrameInstrumented bool
	directFrameOpcodeCounts *directFrameOpcodeCounts
	directFramePICCounts    *directFramePICCounts
	directFramePCCounts     map[*Proto][]uint64
	intrinsicGuards         *baseFieldIntrinsicGuardCache
	coldInstructionFrame    *vmFrame
	coldInstructionRan      bool
	stringIntern            map[string]*stringBox
	stringConcatIntern      map[stringConcatKey]*stringBox
	stringScratch           []byte
}

type stringConcatKey struct {
	values [4]*stringBox
	count  uint8
}

type directFrameOpcodeCounts [256]uint64

type directFramePICCounts struct {
	monomorphicHits uint64
	polymorphicHits uint64
	// pointerHits records cache hits that matched the interned key box and
	// could address the guarded slot without a hash/byte key comparison.
	pointerHits uint64
	// hashByteFallbacks records cache lookups that had to validate an equal
	// string by hash, length, and bytes (for example, distinct string boxes).
	hashByteFallbacks uint64
	// indexedHashHits records live hits into the open-addressed string sidecar
	// using the cached hash entry index rather than probing from the key.
	indexedHashHits                uint64
	keyMisses                      uint64
	shapeMisses                    uint64
	metatableMisses                uint64
	missingKeyFallbacks            uint64
	nilWriteFallbacks              uint64
	invalidKeyFallbacks            uint64
	numericArrayIndexHits          uint64
	sideExits                      [directFrameSideExitReasonCount]uint64
	intrinsicGuardChecks           uint64
	intrinsicGuardHits             uint64
	intrinsicGuardMisses           uint64
	globalSlotHits                 uint64
	globalSlotMisses               uint64
	fixedCallFrameReuses           uint64
	fixedCallFrameMaterializations uint64
	fixedCallArgCopies             uint64
	fixedCallRegisterCopies        uint64
	// fixedCallTrampolineEntries counts fixed script calls that were entered
	// by the outer iterative frame trampoline.  fixedCallRecursiveEntries is
	// retained as a test-only tracer while cold call paths still use the
	// legacy inline helpers.
	fixedCallTrampolineEntries uint64
	fixedCallRecursiveEntries  uint64
	arrayIteratorFastSteps     uint64
	scalarEqualityFastChecks   uint64
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
}

type baseFieldIntrinsicGuardEntry struct {
	key        baseFieldIntrinsicGuardKey
	envVersion uint64
	table      *Table
	token      tableStringShapeToken
	callee     Value
}

func (thread *vmThread) intrinsicGuardCacheEnabled() bool {
	return thread != nil && (thread.directFrameInstrumented || thread.intrinsicGuards != nil)
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

func (counts *directFramePICCounts) addPointerHit() {
	if counts == nil {
		return
	}
	counts.pointerHits++
}

func (counts *directFramePICCounts) addHashByteFallback() {
	if counts == nil {
		return
	}
	counts.hashByteFallbacks++
}

func (counts *directFramePICCounts) addIndexedHashHit() {
	if counts == nil {
		return
	}
	counts.indexedHashHits++
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

func (counts *directFramePICCounts) addArrayIteratorFastStep() {
	if counts == nil {
		return
	}
	counts.arrayIteratorFastSteps++
}

func (counts *directFramePICCounts) addScalarEqualityFastCheck() {
	if counts == nil {
		return
	}
	counts.scalarEqualityFastChecks++
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

func (counts *directFramePICCounts) addGlobalSlotHit() {
	if counts == nil {
		return
	}
	counts.globalSlotHits++
}

func (counts *directFramePICCounts) addGlobalSlotMiss() {
	if counts == nil {
		return
	}
	counts.globalSlotMisses++
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

func (counts *directFramePICCounts) addFixedCallTrampolineEntry() {
	if counts == nil {
		return
	}
	counts.fixedCallTrampolineEntries++
}

func (counts *directFramePICCounts) addFixedCallRecursiveEntry() {
	if counts == nil {
		return
	}
	counts.fixedCallRecursiveEntries++
}

func (counts *directFramePICCounts) totalMechanismActivity() uint64 {
	if counts == nil {
		return 0
	}
	total := counts.monomorphicHits +
		counts.polymorphicHits +
		counts.pointerHits +
		counts.hashByteFallbacks +
		counts.indexedHashHits +
		counts.keyMisses +
		counts.shapeMisses +
		counts.metatableMisses +
		counts.missingKeyFallbacks +
		counts.nilWriteFallbacks +
		counts.invalidKeyFallbacks +
		counts.numericArrayIndexHits +
		counts.intrinsicGuardChecks +
		counts.intrinsicGuardHits +
		counts.intrinsicGuardMisses +
		counts.globalSlotHits +
		counts.globalSlotMisses +
		counts.fixedCallFrameReuses +
		counts.fixedCallFrameMaterializations +
		counts.fixedCallArgCopies +
		counts.fixedCallRegisterCopies +
		counts.fixedCallTrampolineEntries +
		counts.fixedCallRecursiveEntries +
		counts.arrayIteratorFastSteps +
		counts.scalarEqualityFastChecks
	for _, count := range counts.sideExits {
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
	thread.directFrameInstrumented = true
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

type vmFrame struct {
	proto           *Proto
	caller          *vmFrame
	depth           int
	window          vmRegisterWindow
	registerBase    int
	registerCount   int
	owner           *vmStackOwner
	registers       []Value
	cells           []*cell
	upvalues        []*cell
	upvalueValues   []Value
	upvalueValueOK  []bool
	varargs         []Value
	pc              int
	debugLine       int
	openResultStart int
	openResults     vmResultWindow
	pendingCall     vmPendingCall
	hasPendingCall  bool
}

// vmRegisterWindow describes one frame's view into the thread stack.  The
// owner and absolute base remain stable when the stack backing array grows;
// registers is only a short-lived view that is rebound from this descriptor.
// A borrowed window starts inside its caller's dead scratch suffix and keeps
// the caller's prior logical stack length so nested frames can restore it.
type vmRegisterWindow struct {
	owner               *vmStackOwner
	base                int
	length              int
	previousStackLength int
	borrowed            bool
}

type dynamicStringIndexCache struct {
	entries [4]dynamicStringIndexCacheEntry
	next    uint8
}

type dynamicStringIndexCacheEntry struct {
	table *Table
	// key is kept for the raw-string adapter and for diagnostics.  The hot
	// path uses keyBox/keyHash/keyLength so an interned Value can match without
	// hashing or comparing bytes.
	key       string
	keyBox    *stringBox
	keyHash   uint64
	keyLength int
	symbol    int
	slot      tableStringFieldSlot
}

type dynamicStringIndexKey struct {
	text   string
	box    *stringBox
	hash   uint64
	length int
	symbol int
}

type dynamicStringIndexKeyMatch uint8

const (
	dynamicStringIndexKeyMiss dynamicStringIndexKeyMatch = iota
	dynamicStringIndexKeyPointer
	dynamicStringIndexKeyHashBytes
)

func (proto *Proto) directFrameIndexCacheAt(pc int) *dynamicStringIndexCache {
	if proto == nil || pc < 0 || pc >= len(proto.directFrameIndexCaches) {
		return nil
	}
	return &proto.directFrameIndexCaches[pc]
}

type vmSuspendedFrames struct {
	ctx                   context.Context
	globals               *globalEnv
	frames                []*vmFrame
	owner                 *vmStackOwner
	stack                 []Value
	nearestProtectedFrame int
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
	destination       vmResultDestination
	protected         *vmProtectedCall
	protectedBoundary bool
	host              *vmPendingHostCall
}

const noProtectedFrame = -1

type vmResultDestination struct {
	register int
	count    int
}

type vmProtectedCall struct {
	handler                Value
	hasHandler             bool
	previousProtectedFrame int
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
	closure       *closure
	caller        *vmFrame
	argumentStart int
	argumentCount int
	borrowHint    bool
	args          []Value
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
	window     vmResultWindow
	scriptCall vmScriptCall
}

type capturedUpvalueSet struct {
	count        int
	cells        [2]*cell
	values       [2]Value
	valueOK      [2]bool
	cellSpill    []*cell
	valueSpill   []Value
	valueOKSpill []bool
}

type vmResultWindow struct {
	values      []Value
	inline      [vmResultInlineCapacity]Value
	count       int
	borrowed    bool
	usingInline bool
}

const vmResultInlineCapacity = 4

func vmEmptyResultWindow() vmResultWindow {
	return vmResultWindow{}
}

func vmInlineResultWindow(values ...Value) vmResultWindow {
	list := vmResultWindow{usingInline: true, count: len(values)}
	copy(list.inline[:], values)
	if list.count > len(list.inline) {
		list.values = append([]Value(nil), values...)
		list.usingInline = false
	}
	return list
}

func vmSingleResultWindow(value Value) vmResultWindow {
	return vmResultWindow{inline: [vmResultInlineCapacity]Value{value}, count: 1, usingInline: true}
}

func vmInlineArrayResultWindow(values [2]Value, count int) vmResultWindow {
	if count < 0 {
		count = 0
	}
	if count > len(values) {
		count = len(values)
	}
	var inline [vmResultInlineCapacity]Value
	copy(inline[:], values[:count])
	return vmResultWindow{inline: inline, count: count, usingInline: true}
}

func vmOwnedResultWindow(values []Value) vmResultWindow {
	return vmResultWindow{values: values, count: len(values)}
}

func vmBorrowedResultWindow(values []Value) vmResultWindow {
	return vmResultWindow{values: values, count: len(values), borrowed: true}
}

func vmAdjustedBorrowedResultWindow(values []Value) vmResultWindow {
	if len(values) == 0 {
		return vmSingleResultWindow(NilValue())
	}
	return vmBorrowedResultWindow(values)
}

func (list vmResultWindow) len() int {
	return list.count
}

func (list vmResultWindow) at(index int) Value {
	if index < 0 || index >= list.count {
		return NilValue()
	}
	if list.usingInline {
		return list.inline[index]
	}
	return list.values[index]
}

func (list vmResultWindow) ownedValues() []Value {
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

func (list vmResultWindow) retainedValues(reuse []Value) []Value {
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

func (list vmResultWindow) retainedAdjustedWindow(reuse []Value) vmResultWindow {
	if list.count == 0 {
		reuse = reuse[:0]
		reuse = append(reuse, NilValue())
		return vmOwnedResultWindow(reuse)
	}
	return vmOwnedResultWindow(list.retainedValues(reuse))
}

func (list vmResultWindow) adjustedOwnedValues() []Value {
	if list.count == 0 {
		return []Value{NilValue()}
	}
	return list.ownedValues()
}

func (list vmResultWindow) appendTo(values []Value) []Value {
	if list.count == 0 {
		return values
	}
	if list.usingInline {
		return append(values, list.inline[:list.count]...)
	}
	return append(values, list.values[:list.count]...)
}

func (list *vmResultWindow) borrowedValues() []Value {
	if list.count == 0 {
		return nil
	}
	if list.usingInline {
		return list.inline[:list.count]
	}
	return list.values[:list.count]
}

func (list vmResultWindow) ownedValuesWithPrefix(prefix Value) []Value {
	values := make([]Value, 0, list.count+1)
	values = append(values, prefix)
	return list.appendTo(values)
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

func functionValueWithCapturedUpvalues(proto *Proto, captured capturedUpvalueSet) Value {
	if captured.count == 0 {
		if proto != nil && proto.reuseZeroCaptureClosure {
			if proto.canonicalClosure == nil {
				proto.canonicalClosure = &closure{proto: proto}
			}
			return closureFunctionValue(proto.canonicalClosure)
		}
		return functionValue(proto, nil)
	}
	closure := &closure{proto: proto}
	if captured.count <= len(closure.inlineUpvalues) {
		copy(closure.inlineUpvalues[:], captured.cells[:captured.count])
		copy(closure.inlineUpvalueValues[:], captured.values[:captured.count])
		copy(closure.inlineUpvalueOK[:], captured.valueOK[:captured.count])
		closure.upvalues = closure.inlineUpvalues[:captured.count]
		if anyBool(closure.inlineUpvalueOK[:captured.count]) {
			closure.upvalueValues = closure.inlineUpvalueValues[:captured.count]
			closure.upvalueValueOK = closure.inlineUpvalueOK[:captured.count]
		}
		return closureFunctionValue(closure)
	}
	closure.upvalues = captured.cellSpill
	closure.upvalueValues = captured.valueSpill
	closure.upvalueValueOK = captured.valueOKSpill
	return closureFunctionValue(closure)
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

var errColdInstructionResume = errors.New("cold instruction resumed")

type vmYieldRequest struct {
	values    []Value
	protected *vmProtectedCall
	host      *vmPendingHostCall
}

func vmReturnedValues(values []Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateReturned, window: vmOwnedResultWindow(values)}
}

func vmReturnedValue(value Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateReturned, window: vmInlineResultWindow(value)}
}

func vmReturnedBorrowedValues(values []Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateReturned, window: vmBorrowedResultWindow(values)}
}

func vmReturnedPrefixAndWindow(prefix []Value, suffix vmResultWindow) vmFrameResult {
	count := len(prefix) + suffix.len()
	if count <= vmResultInlineCapacity {
		var inline [vmResultInlineCapacity]Value
		copied := copy(inline[:], prefix)
		for i := 0; i < suffix.len(); i++ {
			inline[copied+i] = suffix.at(i)
		}
		return vmFrameResult{
			state:  vmCallStateReturned,
			window: vmResultWindow{inline: inline, count: count, usingInline: true},
		}
	}
	results := make([]Value, 0, count)
	results = append(results, prefix...)
	results = suffix.appendTo(results)
	return vmReturnedValues(results)
}

func vmYieldedValues(values []Value) vmFrameResult {
	return vmFrameResult{state: vmCallStateYielded, window: vmOwnedResultWindow(values)}
}

func (result vmFrameResult) values() []Value {
	return result.window.ownedValues()
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
		ctx:                   ctx,
		globals:               globals,
		stackOwner:            &vmStackOwner{},
		nearestProtectedFrame: noProtectedFrame,
		instructionBudget:     -1,
	}
}

func acquireVMThread(ctx context.Context, globals *globalEnv) *vmThread {
	thread := vmThreadPool.Get().(*vmThread)
	thread.resetForRun(ctx, globals)
	return thread
}

func releaseVMThread(thread *vmThread) {
	if thread == nil {
		return
	}
	thread.resetForPool()
	vmThreadPool.Put(thread)
}

func (thread *vmThread) resetForRun(ctx context.Context, globals *globalEnv) {
	if ctx == nil {
		ctx = context.Background()
	}
	if globals == nil {
		thread.baseGlobals = globalEnv{}
		globals = &thread.baseGlobals
	}
	thread.ctx = ctx
	thread.globals = globals
	thread.frames = thread.frames[:0]
	if thread.stackOwner == nil {
		thread.stackOwner = &vmStackOwner{}
	}
	thread.stackOwner.values = thread.stackOwner.values[:0]
	thread.stack = thread.stackOwner.values
	thread.nearestProtectedFrame = noProtectedFrame
	thread.protectedRecoveryLookups = 0
	thread.protectedRecoveryScans = 0
	thread.protectedRecoveryErrors = 0
	thread.instructionBudget = -1
	thread.coroutine = nil
	thread.nonYieldableDepth = 0
	thread.debugHook = nil
	thread.debugCountInterval = 0
	thread.debugInstructionCount = 0
	thread.debugLineHook = false
	thread.debugCallHook = false
	thread.debugReturnHook = false
	thread.maxFrames = 0
	thread.directFrameInstrumented = false
	thread.directFrameOpcodeCounts = nil
	thread.directFramePICCounts = nil
	thread.directFramePCCounts = nil
	thread.intrinsicGuards = nil
	thread.coldInstructionFrame = nil
	thread.coldInstructionRan = false
	if cap(thread.stringScratch) > 64*1024 {
		thread.stringScratch = nil
	} else {
		thread.stringScratch = thread.stringScratch[:0]
	}
}

func (thread *vmThread) resetForPool() {
	thread.dropFrames(0)
	thread.nearestProtectedFrame = noProtectedFrame
	thread.protectedRecoveryLookups = 0
	thread.protectedRecoveryScans = 0
	thread.protectedRecoveryErrors = 0
	if cap(thread.stack) > 0 {
		values := thread.stack[:cap(thread.stack)]
		clear(values)
		if thread.stackOwner == nil {
			thread.stackOwner = &vmStackOwner{}
		}
		thread.stackOwner.values = values[:0]
		thread.stack = thread.stackOwner.values
	}
	thread.ctx = context.Background()
	thread.globals = nil
	thread.baseGlobals = globalEnv{}
	thread.instructionBudget = -1
	thread.coroutine = nil
	thread.nonYieldableDepth = 0
	thread.debugHook = nil
	thread.debugCountInterval = 0
	thread.debugInstructionCount = 0
	thread.debugLineHook = false
	thread.debugCallHook = false
	thread.debugReturnHook = false
	thread.maxFrames = 0
	thread.directFrameInstrumented = false
	thread.directFrameOpcodeCounts = nil
	thread.directFramePICCounts = nil
	thread.directFramePCCounts = nil
	thread.intrinsicGuards = nil
	thread.coldInstructionFrame = nil
	thread.coldInstructionRan = false
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

// installPendingCall is the single transition that records a frame's pending
// call. Protected calls form an intrusive linked stack through the prior
// nearest-frame marker stored on vmProtectedCall. Recovery can therefore
// index the boundary directly without walking the frame stack.
func (thread *vmThread) installPendingCall(frame *vmFrame, call vmPendingCall) {
	if frame == nil {
		return
	}
	thread.clearPendingCall(frame)
	frame.pendingCall = call
	frame.hasPendingCall = true
	if call.protected == nil {
		return
	}
	// A protected yield can be propagated through a still-live child frame.
	// Keep the child as the nearest recovery boundary; the parent pending call
	// still needs the protected bit for result shaping but is not a second
	// recovery boundary.
	if thread != nil && frame.depth < thread.nearestProtectedFrame &&
		thread.nearestProtectedFrame < len(thread.frames) {
		nearest := thread.frames[thread.nearestProtectedFrame]
		if nearest != nil && nearest.hasPendingCall && nearest.pendingCall.protected == call.protected {
			call.protectedBoundary = false
			frame.pendingCall = call
			return
		}
	}
	call.protectedBoundary = true
	frame.pendingCall = call
	thread.linkProtectedBoundary(frame)
}

// replacePendingCall is intentionally separate from installPendingCall at the
// call sites where a suspended host continuation yields again. Clearing first
// restores the prior boundary before the new call is linked into the chain.
func (thread *vmThread) replacePendingCall(frame *vmFrame, call vmPendingCall) {
	thread.installPendingCall(frame, call)
}

// clearPendingCall removes a frame's pending call and, when it is protected,
// restores the marker that was live before that boundary was installed.
func (thread *vmThread) clearPendingCall(frame *vmFrame) {
	if frame == nil {
		return
	}
	call := frame.pendingCall
	if frame.hasPendingCall && call.protected != nil && thread != nil {
		if call.protectedBoundary && thread.nearestProtectedFrame == frame.depth {
			thread.nearestProtectedFrame = call.protected.previousProtectedFrame
		} else if call.protectedBoundary {
			thread.protectedRecoveryErrors++
		}
	}
	frame.resetPendingCallState()
}

func (thread *vmThread) linkProtectedBoundary(frame *vmFrame) {
	if thread == nil || frame == nil || frame.pendingCall.protected == nil {
		return
	}
	protected := frame.pendingCall.protected
	nearest := thread.nearestProtectedFrame
	if nearest == noProtectedFrame {
		protected.previousProtectedFrame = noProtectedFrame
		thread.nearestProtectedFrame = frame.depth
		return
	}
	if nearest < frame.depth {
		protected.previousProtectedFrame = nearest
		thread.nearestProtectedFrame = frame.depth
		return
	}
	// A protected call may be propagated to a caller while the callee frame
	// remains suspended. Insert this boundary below the live child chain so
	// recovery still selects the innermost frame. The chain is normally one
	// entry deep; this path only runs during a protected yield transition.
	index := nearest
	for index >= 0 && index < len(thread.frames) {
		child := thread.frames[index]
		if child == nil || !child.hasPendingCall || child.pendingCall.protected == nil {
			thread.protectedRecoveryErrors++
			return
		}
		next := child.pendingCall.protected.previousProtectedFrame
		if next == noProtectedFrame || next < frame.depth {
			protected.previousProtectedFrame = next
			child.pendingCall.protected.previousProtectedFrame = frame.depth
			return
		}
		index = next
	}
	thread.protectedRecoveryErrors++
}

func protectedYieldRequest(request vmYieldRequest, handler Value, hasHandler bool) vmYieldRequest {
	request.protected = &vmProtectedCall{
		handler:                handler,
		hasHandler:             hasHandler,
		previousProtectedFrame: noProtectedFrame,
	}
	return request
}

func (thread *vmThread) internStringValue(text string) Value {
	if thread == nil {
		return StringValue(text)
	}
	if thread.stringIntern == nil {
		thread.stringIntern = make(map[string]*stringBox, 64)
	}
	if box, ok := thread.stringIntern[text]; ok {
		return stringValueFromBox(box)
	}
	if len(thread.stringIntern) >= 1024 {
		thread.stringIntern = make(map[string]*stringBox, 64)
	}
	box := newStringBox(text)
	thread.stringIntern[text] = box
	return stringValueFromBox(box)
}

func (thread *vmThread) internStringConcatValues(values []Value) (Value, bool) {
	if thread == nil || len(values) == 0 || len(values) > len(stringConcatKey{}.values) {
		return NilValue(), false
	}
	var key stringConcatKey
	key.count = uint8(len(values))
	for i, value := range values {
		if valueKind(value) != StringKind {
			return NilValue(), false
		}
		key.values[i] = value.stringBox()
	}
	if thread.stringConcatIntern == nil {
		thread.stringConcatIntern = make(map[stringConcatKey]*stringBox, 64)
	}
	if box, ok := thread.stringConcatIntern[key]; ok {
		return stringValueFromBox(box), true
	}
	if len(thread.stringConcatIntern) >= 2048 {
		thread.stringConcatIntern = make(map[stringConcatKey]*stringBox, 64)
	}
	scratch := thread.stringScratch[:0]
	for i := 0; i < int(key.count); i++ {
		scratch = append(scratch, key.values[i].text...)
	}
	thread.stringScratch = scratch
	text := string(scratch)
	var box *stringBox
	if thread.stringIntern != nil {
		box = thread.stringIntern[text]
	}
	if box == nil {
		box = newStringBox(text)
		if thread.stringIntern == nil {
			thread.stringIntern = make(map[string]*stringBox, 64)
		}
		thread.stringIntern[text] = box
	}
	thread.stringConcatIntern[key] = box
	return stringValueFromBox(box), true
}

func (thread *vmThread) concatRawChainString(values []Value) (string, bool, error) {
	if thread == nil {
		return valuesConcatRawChain(values)
	}
	for _, value := range values {
		switch valueKind(value) {
		case StringKind, NumberKind:
		default:
			return "", false, nil
		}
	}
	scratch := thread.stringScratch[:0]
	var err error
	scratch, err = appendConcatRawChain(scratch, values)
	thread.stringScratch = scratch
	if err != nil {
		return "", false, err
	}
	return string(scratch), true, nil
}

func stringValueInGlobalEnv(globals *globalEnv, text string) Value {
	if globals != nil && globals.thread != nil {
		return globals.thread.internStringValue(text)
	}
	return StringValue(text)
}

func (thread *vmThread) run(proto *Proto, args []Value, upvalues []*cell) ([]Value, error) {
	restore := thread.activate()
	defer restore()

	return thread.runScript(proto, args, upvalues)
}

func (thread *vmThread) runWithUpvalues(proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) ([]Value, error) {
	restore := thread.activate()
	defer restore()

	return thread.runScriptWithUpvalues(proto, args, upvalues, upvalueValues, upvalueValueOK)
}

func (thread *vmThread) runScriptWithUpvalues(proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) ([]Value, error) {
	baseDepth := len(thread.frames)
	frame := thread.newFrameWithUpvalues(proto, args, upvalues, upvalueValues, upvalueValueOK)
	thread.pushFrame(frame)
	if thread.debugHook != nil && thread.debugCallHook {
		if err := thread.runDebugCallHook(frame); err != nil {
			if !isVMYieldRequest(err) {
				thread.dropFrames(baseDepth)
			}
			return nil, err
		}
	}
	return thread.runUntilDepth(baseDepth)
}

func (thread *vmThread) runScriptProtectedWithUpvalues(proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) ([]Value, error) {
	baseDepth := len(thread.frames)
	frame := thread.newFrameWithUpvalues(proto, args, upvalues, upvalueValues, upvalueValueOK)
	thread.pushFrame(frame)
	if thread.debugHook != nil && thread.debugCallHook {
		if err := thread.runDebugCallHook(frame); err != nil {
			if !isVMYieldRequest(err) {
				thread.dropFrames(baseDepth)
			}
			return nil, err
		}
	}
	results, err := thread.runUntilDepth(baseDepth)
	if err != nil && !isVMYieldRequest(err) {
		thread.dropFrames(baseDepth)
	}
	return results, err
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
		owner:                 thread.stackOwner,
		stack:                 thread.stack,
		nearestProtectedFrame: thread.nearestProtectedFrame,
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
	thread.stackOwner = nil
	thread.stack = nil
	thread.nearestProtectedFrame = noProtectedFrame
	return suspended
}

func (thread *vmThread) resumeFrames(suspended vmSuspendedFrames) {
	thread.ctx = suspended.ctx
	thread.globals = suspended.globals
	thread.frames = suspended.frames
	thread.stackOwner = suspended.owner
	if thread.stackOwner != nil {
		thread.stack = thread.stackOwner.values
	} else {
		thread.stack = suspended.stack
	}
	thread.nearestProtectedFrame = suspended.nearestProtectedFrame
	thread.rebindFrameWindows()
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
	frame.applyCallResults(thread, args)
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
			thread.replacePendingCall(frame, vmPendingCall{
				destination: call.destination,
				protected:   call.protected,
				host:        yield.host,
			})
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
	frame.applyCallResults(thread, results)
	return thread.runUntilDepth(0)
}

func (thread *vmThread) runScript(proto *Proto, args []Value, upvalues []*cell) ([]Value, error) {
	baseDepth := len(thread.frames)
	frame := thread.newFrame(proto, args, upvalues)
	thread.pushFrame(frame)
	if thread.debugHook != nil && thread.debugCallHook {
		if err := thread.runDebugCallHook(frame); err != nil {
			if !isVMYieldRequest(err) {
				thread.dropFrames(0)
			}
			return nil, err
		}
	}
	results, err := thread.runUntilDepth(baseDepth)
	if err != nil && !isVMYieldRequest(err) {
		thread.dropFrames(0)
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
				thread.dropFrames(baseDepth)
			}
			return nil, err
		}
	}
	results, err := thread.runUntilDepth(baseDepth)
	if err != nil && !isVMYieldRequest(err) {
		thread.dropFrames(baseDepth)
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
			caller := frame
			if call.caller != nil {
				caller = call.caller
			}
			frame = thread.newScriptCallFrame(caller, call)
			thread.pushFrame(frame)
			thread.directFramePICCounts.addFixedCallTrampolineEntry()
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
		result = stabilizeFrameResultBeforeRelease(frame, result)
		thread.popFrame()
		if len(thread.frames) == baseDepth {
			return result, nil
		}
		caller := thread.frames[len(thread.frames)-1]
		if !caller.hasPendingCall {
			return result, nil
		}
		caller.applyFrameCallResults(thread, result)
	}
	return vmFrameResult{}, fmt.Errorf("run: empty VM call stack")
}

func (thread *vmThread) runInlineScriptCall(closure *closure, args []Value) (vmFrameResult, error) {
	baseDepth := len(thread.frames)
	calleeFrame := thread.newClosureCallFrame(closure, args)
	return thread.runInlineScriptFrame(calleeFrame, baseDepth)
}

func (thread *vmThread) runInlineScriptCallFixed(closure *closure, first Value, second Value, third Value, count int) (vmFrameResult, error) {
	if count < 0 {
		count = 0
	}
	if count > 3 {
		count = 3
	}
	if closure == nil || closure.proto == nil || closure.proto.variadic {
		args := [3]Value{first, second, third}
		return thread.runInlineScriptCall(closure, args[:count])
	}
	baseDepth := len(thread.frames)
	calleeFrame := thread.newClosureCallFrameFixed(closure, first, second, third, count)
	return thread.runInlineScriptFrame(calleeFrame, baseDepth)
}

func (thread *vmThread) runInlineScriptCallPrependedFromFrame(closure *closure, first Value, caller *vmFrame, argStart int, argCount int) (vmFrameResult, error) {
	if argCount < 0 {
		argCount = 0
	}
	if closure == nil || closure.proto == nil || closure.proto.variadic {
		args := make([]Value, 1+argCount)
		args[0] = first
		for i := 0; i < argCount; i++ {
			args[i+1] = caller.register(argStart + i)
		}
		return thread.runInlineScriptCall(closure, args)
	}
	baseDepth := len(thread.frames)
	calleeFrame := thread.newClosureCallFramePrependedFromFrame(closure, first, caller, argStart, argCount)
	return thread.runInlineScriptFrame(calleeFrame, baseDepth)
}

func (thread *vmThread) runInlineScriptFrame(calleeFrame *vmFrame, baseDepth int) (vmFrameResult, error) {
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
		frame := thread.newScriptCallFrame(calleeFrame, call)
		thread.pushFrame(frame)
		thread.directFramePICCounts.addFixedCallTrampolineEntry()
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
	result = stabilizeFrameResultBeforeRelease(calleeFrame, result)
	thread.popFrame()
	return result, nil
}

func stabilizeFrameResultBeforeRelease(frame *vmFrame, result vmFrameResult) vmFrameResult {
	if frame == nil || !frame.window.borrowed || !result.window.borrowed {
		return result
	}
	count := result.window.len()
	if count <= vmResultInlineCapacity {
		var inline [vmResultInlineCapacity]Value
		for i := 0; i < count; i++ {
			inline[i] = result.window.at(i)
		}
		result.window = vmResultWindow{inline: inline, count: count, usingInline: true}
		return result
	}
	result.window = vmOwnedResultWindow(result.window.ownedValues())
	return result
}

func (thread *vmThread) runInlineScriptCallOneNoHook(closure *closure, args []Value) (Value, error) {
	if thread.debugHook != nil {
		result, err := thread.runInlineScriptCall(closure, args)
		if err != nil {
			return NilValue(), err
		}
		return result.window.at(0), nil
	}

	baseDepth := len(thread.frames)
	calleeFrame := thread.newClosureCallFrame(closure, args)
	thread.pushFrame(calleeFrame)
	result, err := thread.runFrame(calleeFrame)
	if err != nil {
		if thread.recoverProtectedError(err) {
			result, err = thread.runUntilDepthResult(baseDepth)
			if err != nil {
				return NilValue(), err
			}
			return result.window.at(0), nil
		}
		return NilValue(), err
	}
	if result.state == vmCallStateScriptCall {
		call := result.scriptCall
		frame := thread.newClosureCallFrame(call.closure, call.args)
		thread.pushFrame(frame)
		result, err = thread.runUntilDepthResult(baseDepth)
		if err != nil {
			return NilValue(), err
		}
		return result.window.at(0), nil
	}
	if result.state == vmCallStateYielded {
		return NilValue(), vmYieldRequest{values: result.values()}
	}
	if result.state == vmCallStateHostInterrupt {
		return NilValue(), vmHostInterrupt{}
	}
	result = stabilizeFrameResultBeforeRelease(calleeFrame, result)
	thread.popFrame()
	return result.window.at(0), nil
}

func (thread *vmThread) runInlineScriptCallFixedOneNoHook(closure *closure, first Value, second Value, third Value, count int) (Value, error) {
	if thread.debugHook != nil || closure == nil || closure.proto == nil || closure.proto.variadic {
		result, err := thread.runInlineScriptCallFixed(closure, first, second, third, count)
		if err != nil {
			return NilValue(), err
		}
		return result.window.at(0), nil
	}

	baseDepth := len(thread.frames)
	calleeFrame := thread.newClosureCallFrameFixed(closure, first, second, third, count)
	thread.pushFrame(calleeFrame)
	result, err := thread.runFrame(calleeFrame)
	if err != nil {
		if thread.recoverProtectedError(err) {
			result, err = thread.runUntilDepthResult(baseDepth)
			if err != nil {
				return NilValue(), err
			}
			return result.window.at(0), nil
		}
		return NilValue(), err
	}
	if result.state == vmCallStateScriptCall {
		call := result.scriptCall
		frame := thread.newClosureCallFrame(call.closure, call.args)
		thread.pushFrame(frame)
		result, err = thread.runUntilDepthResult(baseDepth)
		if err != nil {
			return NilValue(), err
		}
		return result.window.at(0), nil
	}
	if result.state == vmCallStateYielded {
		return NilValue(), vmYieldRequest{values: result.values()}
	}
	if result.state == vmCallStateHostInterrupt {
		return NilValue(), vmHostInterrupt{}
	}
	result = stabilizeFrameResultBeforeRelease(calleeFrame, result)
	thread.popFrame()
	return result.window.at(0), nil
}

func fixedRegisterArgs(registers []Value, start int, count int) (Value, Value, Value) {
	var first, second, third Value
	if count > 0 {
		first = registers[start]
	}
	if count > 1 {
		second = registers[start+1]
	}
	if count > 2 {
		third = registers[start+2]
	}
	return first, second, third
}

func (thread *vmThread) recoverProtectedError(err error) bool {
	if isVMYieldRequest(err) || isVMHostInterrupt(err) {
		return false
	}
	thread.protectedRecoveryLookups++
	index := thread.nearestProtectedFrame
	if index < 0 {
		return false
	}
	if index >= len(thread.frames) {
		thread.protectedRecoveryErrors++
		thread.nearestProtectedFrame = noProtectedFrame
		return false
	}
	frame := thread.frames[index]
	if frame == nil || frame.depth != index || !frame.hasPendingCall || frame.pendingCall.protected == nil {
		thread.protectedRecoveryErrors++
		thread.nearestProtectedFrame = noProtectedFrame
		return false
	}
	protected := frame.pendingCall.protected
	thread.dropFrames(index + 1)
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
	frame.applyProtectedErrorResults(thread, append([]Value{BoolValue(false)}, results...))
	return true
}

func (thread *vmThread) pushFrame(frame *vmFrame) {
	if frame == nil {
		return
	}
	pending := frame.pendingCall
	needsProtectedLink := frame.hasPendingCall && pending.protected != nil && !pending.protectedBoundary
	if len(thread.frames) > 0 {
		frame.caller = thread.frames[len(thread.frames)-1]
	}
	frame.depth = len(thread.frames)
	thread.frames = append(thread.frames, frame)
	if needsProtectedLink {
		thread.installPendingCall(frame, pending)
	}
	if len(thread.frames) > thread.maxFrames {
		thread.maxFrames = len(thread.frames)
	}
}

func (thread *vmThread) popFrame() {
	frame := thread.frames[len(thread.frames)-1]
	thread.clearPendingCall(frame)
	thread.frames = thread.frames[:len(thread.frames)-1]
	thread.releaseFrameWindow(frame)
	frame.resetForReuse()
}

func newVMFrame(proto *Proto, args []Value, upvalues []*cell) *vmFrame {
	frame := &vmFrame{}
	frame.reset(proto, args, upvalues, nil, nil)
	return frame
}

func (thread *vmThread) newFrame(proto *Proto, args []Value, upvalues []*cell) *vmFrame {
	return thread.newFrameWithUpvalues(proto, args, upvalues, nil, nil)
}

func (thread *vmThread) newFrameWithUpvalues(proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) *vmFrame {
	frame := thread.frameSlot(len(thread.frames))
	thread.resetFrame(frame, proto, args, upvalues, upvalueValues, upvalueValueOK)
	return frame
}

func (thread *vmThread) newCallFrame(proto *Proto, args []Value, upvalues []*cell) *vmFrame {
	return thread.newCallFrameWithUpvalues(proto, args, upvalues, nil, nil)
}

func (thread *vmThread) newClosureCallFrame(closure *closure, args []Value) *vmFrame {
	return thread.newCallFrameWithUpvalues(closure.proto, args, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK)
}

func (thread *vmThread) newClosureCallFrameFixed(closure *closure, first Value, second Value, third Value, count int) *vmFrame {
	frame := thread.newCallFrameWithUpvalues(closure.proto, nil, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK)
	paramCount := closure.proto.params
	if paramCount > closure.proto.registers {
		paramCount = closure.proto.registers
	}
	if count > paramCount {
		count = paramCount
	}
	thread.directFramePICCounts.addFixedCallArgCopies(count)
	thread.directFramePICCounts.addFixedCallRegisterCopies(count)
	for i := 0; i < count; i++ {
		var value Value
		switch i {
		case 0:
			value = first
		case 1:
			value = second
		case 2:
			value = third
		}
		frame.setRegister(i, value)
	}
	return frame
}

// newBorrowedClosureCallFrame reuses the caller's dead argument/scratch
// suffix as the callee's register window. The marker is a compiler proof; the
// guards below keep the optimization conservative when runtime state has
// become cold or the closure shape differs from that proof.
func (thread *vmThread) newBorrowedClosureCallFrame(closure *closure, caller *vmFrame, argumentStart, argumentCount int) (*vmFrame, bool) {
	if thread == nil || caller == nil || closure == nil || closure.proto == nil {
		return nil, false
	}
	proto := closure.proto
	if proto.variadic || thread.debugHook != nil || thread.instructionBudget >= 0 ||
		thread.coroutine != nil || thread.nonYieldableDepth != 0 ||
		thread.nearestProtectedFrame != noProtectedFrame ||
		caller.hasPendingCall || caller.openResultStart >= 0 || len(proto.capturedLocals) != 0 {
		return nil, false
	}
	if argumentCount < 0 || argumentStart < 0 || argumentStart > caller.registerCount ||
		argumentCount > caller.registerCount-argumentStart ||
		caller.hasCellsInRange(argumentStart, caller.registerCount-argumentStart) {
		return nil, false
	}
	owner := caller.window.owner
	if owner == nil {
		owner = caller.owner
	}
	if owner == nil || owner != thread.stackOwner {
		return nil, false
	}
	base := caller.registerBase + argumentStart
	if base < 0 || base > len(owner.values) || proto.registers < 0 || base > int(^uint(0)>>1)-proto.registers {
		return nil, false
	}
	previousLength := len(owner.values)
	end := base + proto.registers
	if end > previousLength {
		thread.growStack(end)
		owner = thread.stackOwner
	}
	if owner == nil || end > len(owner.values) {
		return nil, false
	}
	frame := thread.frameSlot(len(thread.frames))
	thread.clearPendingCall(frame)
	paramCount := argumentCount
	if paramCount > proto.params {
		paramCount = proto.params
	}
	if paramCount > proto.registers {
		paramCount = proto.registers
	}
	args := owner.values[base : base+paramCount]
	registers := owner.values[base:end]
	frame.resetFrameIntoRegisters(proto, args, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK, owner, base, registers)
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              len(registers),
		previousStackLength: previousLength,
		borrowed:            true,
	}
	thread.directFramePICCounts.addFixedCallFrameReuse()
	return frame, true
}

func installFixedResultPendingCall(frame *vmFrame, destination vmResultDestination) {
	if frame == nil {
		return
	}
	frame.pendingCall = vmPendingCall{destination: destination}
	frame.hasPendingCall = true
}

// newScriptCallFrame materializes a script callee only when the compact call
// spec cannot use its proven borrowed register window.  The caller remains on
// the frame stack with its pending result destination while the new frame is
// dispatched by runUntilDepthResult.
func (thread *vmThread) newScriptCallFrame(caller *vmFrame, call vmScriptCall) *vmFrame {
	if call.borrowHint && caller != nil {
		if frame, ok := thread.newBorrowedClosureCallFrame(call.closure, caller, call.argumentStart, call.argumentCount); ok {
			return frame
		}
	}

	args := call.args
	if args == nil && caller != nil {
		args = caller.retainedFixedCallArgs(call.argumentStart, call.argumentCount).values
	}
	return thread.newClosureCallFrame(call.closure, args)
}

func (thread *vmThread) newCallFrameWithUpvalues(proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) *vmFrame {
	counts := thread.directFramePICCounts
	counts.addFixedCallFrameMaterialization()
	counts.addFixedCallArgCopies(fixedCallParamCopyCount(proto, args))
	frame := thread.frameSlot(len(thread.frames))
	counts.addFixedCallFrameReuse()
	thread.resetFrame(frame, proto, args, upvalues, upvalueValues, upvalueValueOK)
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

func (thread *vmThread) frameSlot(depth int) *vmFrame {
	for len(thread.frameSlots) <= depth {
		thread.frameSlots = append(thread.frameSlots, nil)
	}
	if thread.frameSlots[depth] == nil {
		thread.frameSlots[depth] = &vmFrame{}
	}
	return thread.frameSlots[depth]
}

func (thread *vmThread) resetFrame(frame *vmFrame, proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) {
	thread.clearPendingCall(frame)
	owner := thread.ensureStackOwner()
	previousLength := len(owner.values)
	base := previousLength
	thread.growStack(base + proto.registers)
	registers := owner.values[base : base+proto.registers]
	frame.resetFrameIntoRegisters(proto, args, upvalues, upvalueValues, upvalueValueOK, owner, base, registers)
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              len(registers),
		previousStackLength: previousLength,
	}
}

func (thread *vmThread) ensureStackOwner() *vmStackOwner {
	if thread.stackOwner == nil {
		thread.stackOwner = &vmStackOwner{values: thread.stack}
	}
	thread.stack = thread.stackOwner.values
	return thread.stackOwner
}

func (thread *vmThread) growStack(size int) {
	owner := thread.ensureStackOwner()
	if size <= cap(owner.values) {
		owner.values = owner.values[:size]
		thread.stack = owner.values
		return
	}
	nextCap := cap(owner.values) * 2
	if nextCap < 64 {
		nextCap = 64
	}
	for nextCap < size {
		nextCap *= 2
	}
	next := make([]Value, size, nextCap)
	copy(next, owner.values)
	owner.values = next
	thread.stack = owner.values
	thread.rebindFrameWindows()
}

func (thread *vmThread) rebindFrameWindows() {
	for _, frame := range thread.frames {
		if frame == nil || frame.registerCount == 0 {
			continue
		}
		owner := frame.window.owner
		if owner == nil {
			owner = thread.stackOwner
			frame.owner = owner
			frame.window.owner = owner
		}
		base := frame.window.base
		length := frame.window.length
		if length == 0 {
			base = frame.registerBase
			length = frame.registerCount
		}
		if owner == nil || base+length > len(owner.values) {
			continue
		}
		frame.registerBase = base
		frame.registerCount = length
		frame.registers = owner.values[base : base+length]
	}
}

func (thread *vmThread) releaseFrameWindow(frame *vmFrame) {
	if frame == nil || frame.registerCount == 0 {
		return
	}
	frame.closeCells()
	window := frame.window
	owner := window.owner
	if owner == nil {
		owner = thread.stackOwner
	}
	previousLength := window.previousStackLength
	if window.length == 0 {
		window.base = frame.registerBase
		window.length = frame.registerCount
		previousLength = frame.registerBase
	}
	if owner != nil {
		// Borrowed windows can overlap the caller's dead suffix. Clear only the
		// window itself, after its result has been applied, then restore the
		// caller's prior logical length. Materialized windows retain the old
		// truncation behavior.
		if window.borrowed {
			start := window.base
			end := start + window.length
			if start < 0 {
				start = 0
			}
			if end > len(owner.values) {
				end = len(owner.values)
			}
			if start < end {
				clear(owner.values[start:end])
			}
		}
		if previousLength < 0 {
			previousLength = 0
		}
		if previousLength <= len(owner.values) {
			owner.values = owner.values[:previousLength]
			if owner == thread.stackOwner {
				thread.stack = owner.values
			}
		}
	} else if frame.registerBase <= len(thread.stack) {
		thread.stack = thread.stack[:frame.registerBase]
	}
	if owner != nil && owner == thread.stackOwner {
		thread.stack = owner.values
	}
}

func (thread *vmThread) dropFrames(depth int) {
	if depth < 0 {
		depth = 0
	}
	if depth > len(thread.frames) {
		depth = len(thread.frames)
	}
	for i := len(thread.frames) - 1; i >= depth; i-- {
		frame := thread.frames[i]
		thread.clearPendingCall(frame)
		thread.releaseFrameWindow(frame)
		if frame != nil {
			frame.resetForReuse()
		}
	}
	thread.frames = thread.frames[:depth]
	if depth == 0 {
		if owner := thread.stackOwner; owner != nil {
			clear(owner.values)
			owner.values = owner.values[:0]
			thread.stack = owner.values
		} else {
			clear(thread.stack)
			thread.stack = thread.stack[:0]
		}
		return
	}
	top := thread.frames[depth-1]
	if top == nil {
		return
	}
	end := top.registerBase + top.registerCount
	if owner := thread.stackOwner; owner != nil && end <= len(owner.values) {
		owner.values = owner.values[:end]
		thread.stack = owner.values
	} else if end <= len(thread.stack) {
		thread.stack = thread.stack[:end]
	}
}

func (frame *vmFrame) reset(proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) {
	registers := make([]Value, proto.registers)
	owner := &vmStackOwner{values: registers}
	frame.resetFrameIntoRegisters(proto, args, upvalues, upvalueValues, upvalueValueOK, owner, 0, registers)
	frame.window = vmRegisterWindow{owner: owner, length: len(registers), previousStackLength: 0}
}

func (thread *vmThread) newClosureCallFramePrependedFromFrame(closure *closure, first Value, caller *vmFrame, argStart int, argCount int) *vmFrame {
	frame := thread.frameSlot(len(thread.frames))
	thread.clearPendingCall(frame)
	proto := closure.proto
	owner := thread.ensureStackOwner()
	previousLength := len(owner.values)
	base := previousLength
	thread.growStack(base + proto.registers)
	registers := owner.values[base : base+proto.registers]
	frame.resetFrameIntoRegisters(proto, nil, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK, owner, base, registers)
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              len(registers),
		previousStackLength: previousLength,
	}
	if proto.params > 0 && proto.registers > 0 {
		frame.setRegister(0, first)
	}
	paramsFromCaller := proto.params - 1
	if paramsFromCaller > argCount {
		paramsFromCaller = argCount
	}
	for i := 0; i < paramsFromCaller && i+1 < proto.registers; i++ {
		frame.setRegister(i+1, caller.register(argStart+i))
	}
	copied := paramsFromCaller
	if proto.params > 0 && proto.registers > 0 {
		copied++
	}
	thread.directFramePICCounts.addFixedCallArgCopies(copied)
	thread.directFramePICCounts.addFixedCallRegisterCopies(copied)
	return frame
}

func (frame *vmFrame) resetFrameIntoRegisters(proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool, owner *vmStackOwner, base int, registers []Value) {
	for _, register := range proto.entryNilRegisters {
		registers[register] = NilValue()
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
				cells[index] = &cell{}
				cells[index].openAt(owner, base+index)
			}
		}
	}

	frame.proto = proto
	frame.caller = nil
	frame.depth = noProtectedFrame
	frame.registerBase = base
	frame.registerCount = len(registers)
	frame.owner = owner
	frame.registers = registers
	frame.cells = cells
	frame.upvalues = upvalues
	frame.upvalueValues = upvalueValues
	frame.upvalueValueOK = upvalueValueOK
	frame.varargs = varargs
	frame.pc = 0
	frame.debugLine = -1
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	frame.resetPendingCallState()
}

func (frame *vmFrame) resetForReuse() {
	frame.closeCells()
	frame.proto = nil
	frame.caller = nil
	frame.depth = noProtectedFrame
	frame.window = vmRegisterWindow{}
	frame.registerBase = 0
	frame.registerCount = 0
	frame.owner = nil
	frame.upvalues = nil
	frame.upvalueValues = nil
	frame.upvalueValueOK = nil
	frame.varargs = frame.varargs[:0]
	frame.pc = 0
	frame.debugLine = -1
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	frame.resetPendingCallState()
}

func (frame *vmFrame) resetForPool() {
	clear(frame.registers)
	clear(frame.cells)
	if cap(frame.openResults.values) > 0 {
		clear(frame.openResults.values[:cap(frame.openResults.values)])
	}
	frame.resetForReuse()
}

func capturedLocalRegisters(proto *Proto) []bool {
	captured := make([]bool, proto.registers)
	hasCaptured := false
	for _, child := range proto.prototypes {
		for _, desc := range child.upvalues {
			if desc.local && !desc.copy {
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
	return frame.registers[index]
}

func (frame *vmFrame) setRegister(index int, value Value) {
	frame.registers[index] = value
}

func (frame *vmFrame) registerCell(index int) *cell {
	if len(frame.cells) < len(frame.registers) {
		cells := make([]*cell, len(frame.registers))
		copy(cells, frame.cells)
		frame.cells = cells
	}
	if frame.cells[index] == nil {
		frame.cells[index] = &cell{}
		owner := frame.owner
		if owner == nil {
			owner = &vmStackOwner{values: frame.registers}
			frame.owner = owner
		}
		frame.cells[index].openAt(owner, frame.registerBase+index)
	}
	return frame.cells[index]
}

func (frame *vmFrame) closeCells() {
	if frame == nil || len(frame.cells) == 0 {
		return
	}
	for _, cell := range frame.cells {
		if cell != nil {
			cell.close()
		}
	}
}

func (frame *vmFrame) upvalue(index int) (Value, error) {
	if index < 0 {
		return NilValue(), fmt.Errorf("run: upvalue index %d out of range", index)
	}
	if index < len(frame.upvalueValueOK) && frame.upvalueValueOK[index] {
		return frame.upvalueValues[index], nil
	}
	if index >= len(frame.upvalues) || frame.upvalues[index] == nil {
		return NilValue(), fmt.Errorf("run: upvalue index %d out of range", index)
	}
	return frame.upvalues[index].get(), nil
}

func (frame *vmFrame) setUpvalue(index int, value Value) error {
	if index < 0 {
		return fmt.Errorf("run: upvalue index %d out of range", index)
	}
	if index < len(frame.upvalueValueOK) && frame.upvalueValueOK[index] {
		return fmt.Errorf("run: immutable upvalue index %d cannot be assigned", index)
	}
	if index >= len(frame.upvalues) || frame.upvalues[index] == nil {
		return fmt.Errorf("run: upvalue index %d out of range", index)
	}
	frame.upvalues[index].set(value)
	return nil
}

func (frame *vmFrame) applyCallResults(thread *vmThread, results []Value) {
	call := frame.pendingCall
	thread.clearPendingCall(frame)
	if call.protected != nil {
		results = append([]Value{BoolValue(true)}, results...)
	}
	frame.applyResultDestination(call.destination, results)
}

func (frame *vmFrame) applyFrameCallResults(thread *vmThread, result vmFrameResult) {
	call := frame.pendingCall
	thread.clearPendingCall(frame)
	if call.protected != nil {
		frame.applyResultDestination(call.destination, result.window.ownedValuesWithPrefix(BoolValue(true)))
		return
	}
	frame.applyValueListDestination(call.destination, result.window)
}

func (frame *vmFrame) applySingleFrameCallResult(thread *vmThread, register int, result vmFrameResult) {
	thread.clearPendingCall(frame)
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	frame.setRegister(register, result.window.at(0))
}

func (frame *vmFrame) applyFrameResultDestination(destination vmResultDestination, result vmFrameResult) {
	frame.applyValueListDestination(destination, result.window)
}

func (frame *vmFrame) applySingleFrameResult(register int, result vmFrameResult) {
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	frame.registers[register] = result.window.at(0)
}

func (frame *vmFrame) applyProtectedErrorResults(thread *vmThread, results []Value) {
	call := frame.pendingCall
	thread.clearPendingCall(frame)
	frame.applyResultDestination(call.destination, results)
}

func (frame *vmFrame) callValueToDestination(callee Value, globals *globalEnv, args []Value, destination vmResultDestination) (vmFrameResult, bool, error) {
	thread := activeThread(globals)
	if closure, ok := callee.scriptFunction(); ok && globals != nil && globals.thread != nil {
		result, err := thread.runInlineScriptCall(closure, args)
		if err != nil {
			if yield, ok := err.(vmYieldRequest); ok {
				thread.installPendingCall(frame, vmPendingCall{
					destination: destination,
					protected:   yield.protected,
					host:        yield.host,
				})
				frame.pc++
				return vmYieldedValues(yield.values), true, nil
			}
			if isVMHostInterrupt(err) {
				return vmFrameResult{}, true, err
			}
			return vmFrameResult{}, true, fmt.Errorf("run: call failed: %w", err)
		}
		frame.applyFrameResultDestination(destination, result)
		return vmFrameResult{}, false, nil
	}
	results, err := callValue(callee, globals, args)
	if err != nil {
		if yield, ok := err.(vmYieldRequest); ok {
			thread.installPendingCall(frame, vmPendingCall{
				destination: destination,
				protected:   yield.protected,
				host:        yield.host,
			})
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

func (frame *vmFrame) callFixedTableScriptCallMetamethod(callee Value, globals *globalEnv, argStart int, argCount int, destination vmResultDestination) (bool, error) {
	if globals == nil || globals.thread == nil || argCount < 0 {
		return false, nil
	}
	table, ok := callee.Table()
	if !ok || table.metatable == nil {
		return false, nil
	}
	metamethod, err := table.metatable.rawGetString("__call")
	if err != nil {
		return true, err
	}
	closure, ok := metamethod.scriptFunction()
	if !ok {
		return false, nil
	}
	restore := globals.thread.enterNonYieldable()
	result, err := globals.thread.runInlineScriptCallPrependedFromFrame(closure, callee, frame, argStart, argCount)
	restore()
	if err != nil {
		return true, err
	}
	frame.applyFrameResultDestination(destination, result)
	return true, nil
}

func (frame *vmFrame) resetPendingCallState() {
	frame.pendingCall = vmPendingCall{}
	frame.hasPendingCall = false
}

func (frame *vmFrame) applyResultDestination(destination vmResultDestination, results []Value) {
	frame.applyValueListDestination(destination, vmBorrowedResultWindow(results))
}

func (frame *vmFrame) applyValueListDestination(destination vmResultDestination, results vmResultWindow) {
	resultCount := destination.count
	if resultCount < 0 {
		frame.openResultStart = destination.register
		reuse := frame.openResults.values
		if frame.openResults.borrowed {
			reuse = nil
		}
		frame.openResults = results.retainedAdjustedWindow(reuse)
		frame.setRegister(destination.register, frame.openResults.at(0))
		return
	}

	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	for i := 0; i < resultCount; i++ {
		frame.setRegister(destination.register+i, results.at(i))
	}
}

func (frame *vmFrame) applyInlineResultDestination(destination vmResultDestination, results [2]Value, count int) {
	frame.applyValueListDestination(destination, vmInlineArrayResultWindow(results, count))
}

func (thread *vmThread) runFrame(frame *vmFrame) (vmFrameResult, error) {
	instrumented := thread.directFrameInstrumented || thread.debugHook != nil || thread.instructionBudget >= 0
	for {
		var exit directFrameSideExit
		if instrumented {
			exit = thread.runDirectFrameInstrumented(&frame)
		} else {
			exit = thread.runDirectFrame(&frame)
		}
		if instrumented {
			thread.directFramePICCounts.addSideExit(exit.reason)
		}
		if result, complete, err := exit.frameResult(); complete || err != nil {
			return result, err
		}
		if exit.kind != directFrameSideExitGenericFrame {
			break
		}
		result, complete, resumed, err := thread.runColdInstruction(frame)
		if complete || err != nil {
			return result, err
		}
		if !resumed {
			break
		}
	}
	return vmFrameResult{}, fmt.Errorf("run: direct frame stopped without a result")
}

func (thread *vmThread) runColdInstruction(frame *vmFrame) (vmFrameResult, bool, bool, error) {
	previousFrame := thread.coldInstructionFrame
	previousRan := thread.coldInstructionRan
	thread.coldInstructionFrame = frame
	thread.coldInstructionRan = false
	result, err := thread.runColdInstructionLoop(frame)
	thread.coldInstructionFrame = previousFrame
	thread.coldInstructionRan = previousRan
	if errors.Is(err, errColdInstructionResume) {
		return vmFrameResult{}, false, true, nil
	}
	return result, true, false, err
}

func directFrameStringField(value Value, key string) (Value, bool, error) {
	return directFrameStringFieldBox(value, key, nil)
}

func directFrameStringFieldBox(value Value, key string, box *stringBox) (Value, bool, error) {
	table := value.tableRef()
	if table == nil {
		return NilValue(), false, fmt.Errorf("get field target is %s, want table", value.Kind())
	}
	var field Value
	var ok bool
	if box != nil {
		field, ok = table.rawStringFieldBox(box)
	} else {
		field, ok = table.rawStringField(key)
	}
	if ok {
		return field, true, nil
	}
	if table.metatable != nil {
		return NilValue(), false, nil
	}
	return NilValue(), true, nil
}

func directFrameRawConcatOperand(value Value) bool {
	return valueKind(value) == StringKind || valueKind(value) == NumberKind
}

func directFrameRowStringField(value Value, key string, slotIndex int) (Value, bool, error) {
	return directFrameRowStringFieldBox(value, key, nil, slotIndex)
}

func directFrameRowStringFieldBox(value Value, key string, box *stringBox, slotIndex int) (Value, bool, error) {
	table := value.tableRef()
	if table == nil {
		return NilValue(), false, fmt.Errorf("get field target is %s, want table", value.Kind())
	}
	var field Value
	var ok bool
	if box != nil {
		field, ok = table.rawRowStringFieldBox(rowStringFieldSlotRefFromIndex(slotIndex), box)
	} else {
		field, ok = table.rawRowStringField(rowStringFieldSlotRefFromIndex(slotIndex), key)
	}
	if ok {
		return field, true, nil
	}
	if table.metatable != nil {
		return NilValue(), false, nil
	}
	return NilValue(), true, nil
}

func directFrameRowStringFieldFast(value Value, key string, slotIndex int) (Value, bool, bool) {
	table := value.tableRef()
	if table == nil {
		return NilValue(), false, false
	}
	if slotIndex >= 0 &&
		!table.hasStringOverflow() &&
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

func directFrameRowStringFieldsStringEqualFast(leftValue Value, leftKey string, leftSlot int, rightValue Value, rightKey string, rightSlot int) (bool, bool, bool) {
	leftTable := leftValue.tableRef()
	if leftTable == nil {
		return false, false, false
	}
	rightTable := rightValue.tableRef()
	if rightTable == nil {
		return false, false, false
	}
	left := NilValue()
	leftOK := false
	if leftSlot >= 0 &&
		!leftTable.hasStringOverflow() &&
		leftSlot < len(leftTable.stringFields) &&
		leftTable.stringFields[leftSlot].key == leftKey {
		left = leftTable.stringFields[leftSlot].value
		leftOK = true
	} else if field, ok := leftTable.rawStringField(leftKey); ok {
		left = field
		leftOK = true
	}
	if !leftOK || valueKind(left) != StringKind {
		return false, false, true
	}
	right := NilValue()
	rightOK := false
	if rightSlot >= 0 &&
		!rightTable.hasStringOverflow() &&
		rightSlot < len(rightTable.stringFields) &&
		rightTable.stringFields[rightSlot].key == rightKey {
		right = rightTable.stringFields[rightSlot].value
		rightOK = true
	} else if field, ok := rightTable.rawStringField(rightKey); ok {
		right = field
		rightOK = true
	}
	if !rightOK || valueKind(right) != StringKind {
		return false, false, true
	}
	return left.stringText() == right.stringText(), true, true
}

func directFrameScalarValuesEqual(left Value, right Value) (bool, bool) {
	if valueKind(left) != valueKind(right) {
		if valueKind(left) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == TableKind || valueKind(right) == UserDataKind {
			return false, false
		}
		return false, true
	}
	switch valueKind(left) {
	case NilKind:
		return true, true
	case BoolKind:
		return valueBool(left) == valueBool(right), true
	case NumberKind:
		if math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
			return false, true
		}
		return valueNumber(left) == valueNumber(right), true
	case StringKind:
		return left.stringText() == right.stringText(), true
	default:
		return false, false
	}
}

func directFrameRowStringFieldSlot(value Value, key string, slotIndex int) (Value, *Table, bool, bool) {
	table := value.tableRef()
	if table == nil {
		return Value{}, nil, false, false
	}
	if slotIndex >= 0 &&
		!table.hasStringOverflow() &&
		slotIndex < len(table.stringFields) &&
		table.stringFields[slotIndex].key == key {
		return table.stringFields[slotIndex].value, table, true, true
	}
	return Value{}, table, false, true
}

func directFrameTableGetIsland(globals *globalEnv, table *Table, key Value) (Value, bool, error) {
	var seen map[*Table]bool
	depth := 0
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
		if seen != nil {
			if seen[table] {
				return NilValue(), true, fmt.Errorf("table: cyclic __index chain")
			}
			seen[table] = true
		} else if depth >= metatableWalkInlineLimit {
			seen = make(map[*Table]bool)
			seen[table] = true
		}

		index, ok, err := table.cachedIndexFallback()
		if err != nil {
			return NilValue(), true, err
		}
		if !ok {
			return NilValue(), true, nil
		}
		if indexTable, ok := index.Table(); ok {
			table = indexTable
			depth++
			continue
		}
		if callableValue(index) {
			value, err := runtimeTableAccess(globals).callIndex(index, table, key)
			return value, true, err
		}
		return NilValue(), true, fmt.Errorf("table: __index is %s, want table or function", index.Kind())
	}
}

func directFrameTableSetIsland(globals *globalEnv, table *Table, key Value, value Value) (bool, error) {
	var seen map[*Table]bool
	depth := 0
	for {
		current, err := table.rawGet(key)
		if err != nil {
			return true, err
		}
		if !current.IsNil() || table == nil || table.metatable == nil {
			return true, table.rawSet(key, value)
		}
		if seen != nil {
			if seen[table] {
				return true, fmt.Errorf("table: cyclic __newindex chain")
			}
			seen[table] = true
		} else if depth >= metatableWalkInlineLimit {
			seen = make(map[*Table]bool)
			seen[table] = true
		}

		newIndex, ok, err := table.cachedNewIndexFallback()
		if err != nil {
			return true, err
		}
		if !ok {
			return true, table.rawSet(key, value)
		}
		if newIndexTable, ok := newIndex.Table(); ok {
			table = newIndexTable
			depth++
			continue
		}
		if callableValue(newIndex) {
			return true, runtimeTableAccess(globals).callNewIndex(newIndex, table, key, value)
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
		results, err := host(ownedHostArgs(args))
		if err != nil {
			return nil, true, fmt.Errorf("host function failed: %w", err)
		}
		return results, true, nil
	}
	return nil, false, nil
}

func directFrameApplyCallIslandResults(frame *vmFrame, registers []Value, start int, count int, results []Value) {
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	if count < 0 {
		frame.openResultStart = start
		frame.openResults = vmBorrowedResultWindow(results).retainedAdjustedWindow(frame.openResults.values)
		registers[start] = frame.openResults.at(0)
		return
	}
	if count == 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		registers[start+i] = adjustedResultAt(results, i)
	}
}

func (thread *vmThread) runDirectFastCall(frame *vmFrame, nativeID nativeFuncID, start int, argCount int, resultCount int, counts *directFramePICCounts) directFrameSideExit {
	if nativeID == nativeFuncCoroutineResume {
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonYield)
	}
	registers := frame.registers
	callee, nativeUnchanged, err := fastCallCallee(thread.globals, nativeID)
	if err != nil {
		return directFrameFail(err)
	}
	if !nativeUnchanged {
		counts.addSideExit(directFrameSideExitReasonIntrinsic)
		args := registers[start : start+argCount]
		if nativeID == nativeFuncSelect {
			args = make([]Value, 1+len(frame.varargs))
			args[0] = StringValue("#")
			copy(args[1:], frame.varargs)
		}
		results, ok, err := directFrameNonYieldingCallIsland(callee, thread.globals, args)
		if err != nil {
			return directFrameFail(fmt.Errorf("run: call failed: %w", err))
		}
		if !ok {
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
		}
		directFrameApplyCallIslandResults(frame, registers, start, resultCount, results)
		return directFrameResume()
	}
	switch nativeID {
	case nativeFuncTableInsert:
		if _, err := baseTableInsert(registers[start : start+argCount]); err != nil {
			return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
		}
		directFrameApplyCallIslandResults(frame, registers, start, resultCount, nil)
	case nativeFuncTableRemove:
		position := NilValue()
		if argCount > 1 {
			position = registers[start+1]
		}
		removed, ok, err := baseTableRemoveFastArrayValue(registers[start], position, argCount)
		if err != nil {
			return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
		}
		if !ok {
			removed, err = baseTableRemoveValue(registers[start : start+argCount])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
		}
		directFrameApplyCallIslandResults(frame, registers, start, resultCount, []Value{removed})
	case nativeFuncMathMin:
		if resultCount != 1 {
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
		}
		minimum, err := baseMathMinValue(registers[start : start+argCount])
		if err != nil {
			return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
		}
		directFrameApplyCallIslandResults(frame, registers, start, resultCount, []Value{NumberValue(minimum)})
	case nativeFuncRawLen:
		value, err := baseRawLenValue(registers[start : start+argCount])
		if err != nil {
			return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
		}
		directFrameApplyCallIslandResults(frame, registers, start, resultCount, []Value{value})
	case nativeFuncSelect:
		count := NumberValue(float64(len(frame.varargs)))
		frame.openResultStart = -1
		frame.openResults = vmResultWindow{}
		if resultCount < 0 {
			frame.openResultStart = start
			frame.openResults = vmSingleResultWindow(count)
			registers[start] = frame.openResults.at(0)
			return directFrameResume()
		}
		if resultCount == 0 {
			resultCount = 1
		}
		for i := 0; i < resultCount; i++ {
			registers[start+i] = adjustedResultAt([]Value{count}, i)
		}
	default:
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
	}
	return directFrameResume()
}

func (thread *vmThread) runColdFastCall(frame *vmFrame, nativeID nativeFuncID, start int, argCount int, resultCount int) (vmFrameResult, bool, error) {
	destination := vmResultDestination{register: start, count: resultCount}
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	if nativeID == nativeFuncSelect {
		callee, nativeUnchanged, err := fastCallCallee(thread.globals, nativeID)
		if err != nil {
			return vmFrameResult{}, true, err
		}
		if nativeUnchanged {
			frame.applyInlineResultDestination(destination, [2]Value{NumberValue(float64(len(frame.varargs)))}, 1)
			return vmFrameResult{}, false, nil
		}
		args := make([]Value, 1+len(frame.varargs))
		args[0] = StringValue("#")
		copy(args[1:], frame.varargs)
		return frame.callValueToDestination(callee, thread.globals, args, destination)
	}
	args := frame.scriptCallArgs(start, argCount)
	callee, nativeUnchanged, err := fastCallCallee(thread.globals, nativeID)
	if err != nil {
		return vmFrameResult{}, true, err
	}
	if nativeUnchanged {
		switch nativeID {
		case nativeFuncTableInsert:
			if _, err := baseTableInsert(args); err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: call failed: host function failed: %w", err)
			}
			frame.applyInlineResultDestination(destination, [2]Value{NilValue()}, 1)
			return vmFrameResult{}, false, nil
		case nativeFuncTableRemove:
			removed, err := baseTableRemoveValue(args)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: call failed: host function failed: %w", err)
			}
			frame.applyInlineResultDestination(destination, [2]Value{removed}, 1)
			return vmFrameResult{}, false, nil
		case nativeFuncMathMin:
			minimum, err := baseMathMinValue(args)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: call failed: host function failed: %w", err)
			}
			frame.applyInlineResultDestination(destination, [2]Value{NumberValue(minimum)}, 1)
			return vmFrameResult{}, false, nil
		case nativeFuncRawLen:
			value, err := baseRawLenValue(args)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: call failed: host function failed: %w", err)
			}
			frame.applyInlineResultDestination(destination, [2]Value{value}, 1)
			return vmFrameResult{}, false, nil
		case nativeFuncCoroutineResume:
			results, err := baseCoroutineResume(thread.globals, args)
			if err != nil {
				return vmFrameResult{}, true, fmt.Errorf("run: call failed: host function failed: %w", err)
			}
			frame.applyResultDestination(destination, results)
			return vmFrameResult{}, false, nil
		}
	}
	return frame.callValueToDestination(callee, thread.globals, args, destination)
}

func fastCallNativeUnchanged(globals *globalEnv, nativeID nativeFuncID) bool {
	switch nativeID {
	case nativeFuncTableInsert:
		return baseFieldIntrinsicUnchangedWithValues(globals, "table", "insert", nativeID)
	case nativeFuncTableRemove:
		return baseFieldIntrinsicUnchangedWithValues(globals, "table", "remove", nativeID)
	case nativeFuncCoroutineResume:
		return baseFieldIntrinsicUnchangedWithValues(globals, "coroutine", "resume", nativeID)
	case nativeFuncMathMin:
		return baseFieldIntrinsicUnchangedWithValues(globals, "math", "min", nativeID)
	case nativeFuncRawLen:
		return globals == nil || globals.nativeGlobalUnchanged("rawlen", nativeID)
	case nativeFuncSelect:
		return globals == nil || globals.nativeGlobalUnchanged("select", nativeID)
	default:
		return false
	}
}

func fastCallCallee(globals *globalEnv, nativeID nativeFuncID) (Value, bool, error) {
	switch nativeID {
	case nativeFuncTableInsert:
		return tableIntrinsicCallee(globals, "insert")
	case nativeFuncTableRemove:
		return tableIntrinsicCallee(globals, "remove")
	case nativeFuncCoroutineResume:
		return coroutineIntrinsicCallee(globals, "resume")
	case nativeFuncMathMin:
		return mathIntrinsicCallee(globals, "min")
	case nativeFuncRawLen:
		return rawLenIntrinsicCallee(globals)
	case nativeFuncSelect:
		return selectIntrinsicCallee(globals)
	default:
		return NilValue(), false, fmt.Errorf("run: unknown fast call native id %d", nativeID)
	}
}

func vmRowStringField(globals *globalEnv, table *Table, keyValue Value, key string, slotIndex int) (Value, error) {
	var value Value
	var ok bool
	if box := keyValue.stringBox(); box != nil {
		value, ok = table.rawRowStringFieldBox(rowStringFieldSlotRefFromIndex(slotIndex), box)
	} else {
		value, ok = table.rawRowStringField(rowStringFieldSlotRefFromIndex(slotIndex), key)
	}
	if ok {
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
	return cache.getKey(table, dynamicStringIndexKey{
		text:   key,
		hash:   hashString(key),
		length: len(key),
	})
}

func (cache *dynamicStringIndexCache) getValue(table *Table, key Value) (Value, bool) {
	box := key.stringBox()
	if box == nil {
		return NilValue(), false
	}
	return cache.getKey(table, dynamicStringIndexKey{
		text:   box.text,
		box:    box,
		hash:   box.hash,
		length: len(box.text),
	})
}

func (cache *dynamicStringIndexCache) getCounted(table *Table, key string, counts *directFramePICCounts) (Value, bool) {
	return cache.getKeyCounted(table, dynamicStringIndexKey{
		text:   key,
		hash:   hashString(key),
		length: len(key),
	}, counts)
}

func (cache *dynamicStringIndexCache) getSymbolCounted(table *Table, key string, symbol int, counts *directFramePICCounts) (Value, bool) {
	return cache.getKeyCounted(table, dynamicStringIndexKey{
		text:   key,
		hash:   hashString(key),
		length: len(key),
		symbol: symbol,
	}, counts)
}

func (cache *dynamicStringIndexCache) getValueCounted(table *Table, key Value, counts *directFramePICCounts) (Value, bool) {
	box := key.stringBox()
	if box == nil {
		counts.addKeyMiss()
		return NilValue(), false
	}
	return cache.getKeyCounted(table, dynamicStringIndexKey{
		text:   box.text,
		box:    box,
		hash:   box.hash,
		length: len(box.text),
	}, counts)
}

func (cache *dynamicStringIndexCache) hasValueKey(table *Table, key Value) bool {
	if cache == nil || table == nil {
		return false
	}
	box := key.stringBox()
	if box == nil {
		return false
	}
	probe := dynamicStringIndexKey{text: box.text, box: box, hash: box.hash, length: len(box.text)}
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == table && entry.matchKey(probe) != dynamicStringIndexKeyMiss {
			return true
		}
	}
	return false
}

func (cache *dynamicStringIndexCache) getKeyCounted(table *Table, key dynamicStringIndexKey, counts *directFramePICCounts) (Value, bool) {
	if cache == nil {
		counts.addKeyMiss()
		return NilValue(), false
	}
	keyMatched := false
	for i := range cache.entries {
		entry := &cache.entries[i]
		match := entry.matchKey(key)
		if entry.table == nil || match == dynamicStringIndexKeyMiss {
			continue
		}
		keyMatched = true
		if match == dynamicStringIndexKeyHashBytes {
			counts.addHashByteFallback()
		}
		value, ok, indexed := directFrameReadCachedStringSlot(table, entry.slot, key, entry.table == table && match == dynamicStringIndexKeyPointer)
		if !ok {
			counts.addShapeMiss()
			continue
		}
		if indexed {
			counts.addIndexedHashHit()
		}
		if match == dynamicStringIndexKeyPointer && entry.table == table {
			counts.addPointerHit()
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

func (cache *dynamicStringIndexCache) getKey(table *Table, key dynamicStringIndexKey) (Value, bool) {
	if cache == nil {
		return NilValue(), false
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		match := entry.matchKey(key)
		if entry.table == nil || match == dynamicStringIndexKeyMiss {
			continue
		}
		value, ok, _ := directFrameReadCachedStringSlot(table, entry.slot, key, entry.table == table && match == dynamicStringIndexKeyPointer)
		if ok {
			return value, true
		}
	}
	return NilValue(), false
}

func (cache *dynamicStringIndexCache) store(table *Table, key string, slot tableStringFieldSlot) {
	cache.storeKey(table, dynamicStringIndexKey{
		text:   key,
		box:    slot.key,
		hash:   hashString(key),
		length: len(key),
	}, slot)
}

func (cache *dynamicStringIndexCache) storeSymbol(table *Table, key string, symbol int, slot tableStringFieldSlot) {
	cache.storeKey(table, dynamicStringIndexKey{
		text:   key,
		box:    slot.key,
		hash:   hashString(key),
		length: len(key),
		symbol: symbol,
	}, slot)
}

func (cache *dynamicStringIndexCache) storeValue(table *Table, key Value, slot tableStringFieldSlot) {
	box := key.stringBox()
	if box == nil {
		return
	}
	cache.storeKey(table, dynamicStringIndexKey{
		text:   box.text,
		box:    box,
		hash:   box.hash,
		length: len(box.text),
	}, slot)
}

func (cache *dynamicStringIndexCache) storeKey(table *Table, key dynamicStringIndexKey, slot tableStringFieldSlot) {
	if cache == nil {
		return
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == table && entry.matchKey(key) != dynamicStringIndexKeyMiss {
			*entry = dynamicStringIndexCacheEntry{
				table:     table,
				key:       key.text,
				keyBox:    key.box,
				keyHash:   key.hash,
				keyLength: key.length,
				symbol:    key.symbol,
				slot:      slot,
			}
			return
		}
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		if entry.table == nil {
			*entry = dynamicStringIndexCacheEntry{
				table:     table,
				key:       key.text,
				keyBox:    key.box,
				keyHash:   key.hash,
				keyLength: key.length,
				symbol:    key.symbol,
				slot:      slot,
			}
			return
		}
	}
	index := int(cache.next % uint8(len(cache.entries)))
	cache.next++
	cache.entries[index] = dynamicStringIndexCacheEntry{
		table:     table,
		key:       key.text,
		keyBox:    key.box,
		keyHash:   key.hash,
		keyLength: key.length,
		symbol:    key.symbol,
		slot:      slot,
	}
}

func (cache *dynamicStringIndexCache) write(table *Table, key string, value Value) bool {
	return cache.writeKey(table, dynamicStringIndexKey{
		text:   key,
		hash:   hashString(key),
		length: len(key),
	}, value)
}

func (cache *dynamicStringIndexCache) writeValue(table *Table, key Value, value Value) bool {
	box := key.stringBox()
	if box == nil {
		return false
	}
	return cache.writeKey(table, dynamicStringIndexKey{
		text:   box.text,
		box:    box,
		hash:   box.hash,
		length: len(box.text),
	}, value)
}

func (cache *dynamicStringIndexCache) writeCounted(table *Table, key string, value Value, counts *directFramePICCounts) bool {
	return cache.writeKeyCounted(table, dynamicStringIndexKey{
		text:   key,
		hash:   hashString(key),
		length: len(key),
	}, value, counts)
}

func (cache *dynamicStringIndexCache) writeSymbolCounted(table *Table, key string, symbol int, value Value, counts *directFramePICCounts) bool {
	return cache.writeKeyCounted(table, dynamicStringIndexKey{
		text:   key,
		hash:   hashString(key),
		length: len(key),
		symbol: symbol,
	}, value, counts)
}

func (cache *dynamicStringIndexCache) writeValueCounted(table *Table, key Value, value Value, counts *directFramePICCounts) bool {
	box := key.stringBox()
	if box == nil {
		counts.addKeyMiss()
		return false
	}
	return cache.writeKeyCounted(table, dynamicStringIndexKey{
		text:   box.text,
		box:    box,
		hash:   box.hash,
		length: len(box.text),
	}, value, counts)
}

func (cache *dynamicStringIndexCache) writeKeyCounted(table *Table, key dynamicStringIndexKey, value Value, counts *directFramePICCounts) bool {
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
		match := entry.matchKey(key)
		if entry.table == nil || match == dynamicStringIndexKeyMiss {
			continue
		}
		keyMatched = true
		if match == dynamicStringIndexKeyHashBytes {
			counts.addHashByteFallback()
		}
		ok, indexed := directFrameWriteCachedStringSlot(table, entry.slot, key, value, entry.table == table && match == dynamicStringIndexKeyPointer)
		if !ok {
			counts.addShapeMiss()
			continue
		}
		if indexed {
			counts.addIndexedHashHit()
		}
		if match == dynamicStringIndexKeyPointer && entry.table == table {
			counts.addPointerHit()
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

func (cache *dynamicStringIndexCache) writeKey(table *Table, key dynamicStringIndexKey, value Value) bool {
	if value.IsNil() || cache == nil {
		return false
	}
	for i := range cache.entries {
		entry := &cache.entries[i]
		match := entry.matchKey(key)
		if entry.table == nil || match == dynamicStringIndexKeyMiss {
			continue
		}
		if ok, _ := directFrameWriteCachedStringSlot(table, entry.slot, key, value, entry.table == table && match == dynamicStringIndexKeyPointer); ok {
			return true
		}
	}
	return false
}

func (entry *dynamicStringIndexCacheEntry) matchKey(key dynamicStringIndexKey) dynamicStringIndexKeyMatch {
	if entry == nil || entry.table == nil {
		return dynamicStringIndexKeyMiss
	}
	if entry.keyBox != nil && key.box != nil && entry.keyBox == key.box {
		return dynamicStringIndexKeyPointer
	}
	if entry.keyHash != key.hash || entry.keyLength != key.length || entry.key != key.text {
		return dynamicStringIndexKeyMiss
	}
	return dynamicStringIndexKeyHashBytes
}

func directFrameReadCachedStringSlot(table *Table, slot tableStringFieldSlot, key dynamicStringIndexKey, pointer bool) (Value, bool, bool) {
	if table == nil || !slot.token.matchesTableLayout(table) {
		return NilValue(), false, false
	}
	if slot.keyHash != cachedStringSlotHash(slot, key) || len(slot.keyText) != key.length {
		return NilValue(), false, false
	}
	if slot.token.storage == 1 {
		fields := table.hashFields()
		if fields == nil || slot.index < 0 || slot.index >= len(fields.entries) {
			return NilValue(), false, false
		}
		entry := &fields.entries[slot.index]
		if entry.state != tableHashFull || entry.value.IsNil() || entry.hash != cachedStringSlotHash(slot, key) {
			return NilValue(), false, false
		}
		if !pointer && (len(entry.key.str) != key.length || entry.key.str != key.text) {
			return NilValue(), false, false
		}
		return entry.value, true, true
	}
	if slot.token.storage != 0 || slot.index < 0 || slot.index >= len(table.stringFields) {
		return NilValue(), false, false
	}
	field := &table.stringFields[slot.index]
	if field.value.IsNil() {
		return NilValue(), false, false
	}
	if !pointer {
		if field.box != nil {
			if field.box.hash != key.hash || len(field.box.text) != key.length || field.box.text != key.text {
				return NilValue(), false, false
			}
		} else if len(field.key) != key.length || field.key != key.text {
			return NilValue(), false, false
		}
	}
	return field.value, true, false
}

func directFrameWriteCachedStringSlot(table *Table, slot tableStringFieldSlot, key dynamicStringIndexKey, value Value, pointer bool) (bool, bool) {
	if table == nil || value.IsNil() || !slot.token.matchesTableLayout(table) {
		return false, false
	}
	if slot.keyHash != cachedStringSlotHash(slot, key) || len(slot.keyText) != key.length {
		return false, false
	}
	if slot.token.storage == 1 {
		fields := table.hashFields()
		if fields == nil || slot.index < 0 || slot.index >= len(fields.entries) {
			return false, false
		}
		entry := &fields.entries[slot.index]
		if entry.state != tableHashFull || entry.value.IsNil() || entry.hash != cachedStringSlotHash(slot, key) {
			return false, false
		}
		if !pointer && (len(entry.key.str) != key.length || entry.key.str != key.text) {
			return false, false
		}
		entry.value = value
		table.stringValueVersion++
		return true, true
	}
	if slot.token.storage != 0 || slot.index < 0 || slot.index >= len(table.stringFields) {
		return false, false
	}
	field := &table.stringFields[slot.index]
	if field.value.IsNil() {
		return false, false
	}
	if !pointer {
		if field.box != nil {
			if field.box.hash != key.hash || len(field.box.text) != key.length || field.box.text != key.text {
				return false, false
			}
		} else if len(field.key) != key.length || field.key != key.text {
			return false, false
		}
	}
	field.value = value
	table.stringValueVersion++
	return true, false
}

func cachedStringSlotHash(slot tableStringFieldSlot, key dynamicStringIndexKey) uint64 {
	if slot.token.storage == 1 {
		return (tableKey{kind: StringKind, strHash: key.hash}).hash()
	}
	return key.hash
}

func directFrameBinaryArithmeticValue(
	counts *directFramePICCounts,
	globals *globalEnv,
	left Value,
	right Value,
	metafield string,
	operator string,
	primitive func(float64, float64) float64,
) (Value, error) {
	if directFrameValueHasMetatable(left) || directFrameValueHasMetatable(right) {
		counts.addSideExit(directFrameSideExitReasonMetatable)
	}
	return binaryArithmeticValue(left, right, globals, metafield, operator, primitive)
}

func directFrameBinaryArithmeticValueUncounted(
	globals *globalEnv,
	left Value,
	right Value,
	metafield string,
	operator string,
	primitive func(float64, float64) float64,
) (Value, error) {
	return binaryArithmeticValue(left, right, globals, metafield, operator, primitive)
}

func directFrameUnaryArithmeticValue(
	counts *directFramePICCounts,
	globals *globalEnv,
	value Value,
	fn func(Value, *globalEnv) (Value, error),
) (Value, error) {
	if directFrameValueHasMetatable(value) {
		counts.addSideExit(directFrameSideExitReasonMetatable)
	}
	return fn(value, globals)
}

func directFrameUnaryArithmeticValueUncounted(
	globals *globalEnv,
	value Value,
	fn func(Value, *globalEnv) (Value, error),
) (Value, error) {
	return fn(value, globals)
}

func directFrameLessForBranch(counts *directFramePICCounts, globals *globalEnv, left Value, right Value) (bool, error) {
	if directFrameValueHasMetatable(left) || directFrameValueHasMetatable(right) {
		counts.addSideExit(directFrameSideExitReasonMetatable)
	}
	return lessValue(left, right, globals)
}

func directFrameLessForBranchUncounted(globals *globalEnv, left Value, right Value) (bool, error) {
	return lessValue(left, right, globals)
}

func directFrameValueHasMetatable(value Value) bool {
	table := value.tableRef()
	return table != nil && table.metatable != nil
}

func (thread *vmThread) runDirectFrame(frame **vmFrame) directFrameSideExit {
	return runDirectFrameProductionLoop(thread, frame)
}

func (thread *vmThread) runDirectFrameInstrumented(frame **vmFrame) directFrameSideExit {
	return runDirectFrameInstrumentedLoop(thread, frame)
}

// resumeDirectFrameChild applies a returned nested callee result while the
// direct dispatcher is still active. It is intentionally bounded by the
// entry depth: frames owned by the surrounding cold dispatcher remain its
// responsibility.
func (thread *vmThread) resumeDirectFrameChild(rootDepth int, frame **vmFrame, result vmFrameResult) bool {
	if thread == nil || frame == nil || *frame == nil || len(thread.frames) == 0 || len(thread.frames)-1 <= rootDepth {
		return false
	}
	if thread.frames[len(thread.frames)-1] != *frame || len(thread.frames) < 2 {
		return false
	}
	result = stabilizeFrameResultBeforeRelease(*frame, result)
	thread.popFrame()
	caller := thread.frames[len(thread.frames)-1]
	if caller == nil || !caller.hasPendingCall {
		return false
	}
	caller.applyFrameCallResults(thread, result)
	*frame = caller
	return true
}

// resumeDirectFrameChildOne is the fixed-one-result fast return. The scalar
// result is copied before releasing the borrowed callee window, then written
// directly into the caller's destination without constructing a result window
// or running the generic result-retention machinery.
func (thread *vmThread) resumeDirectFrameChildOne(rootDepth int, frame **vmFrame, value Value) bool {
	if thread == nil || frame == nil || *frame == nil || len(thread.frames) == 0 || len(thread.frames)-1 <= rootDepth {
		return false
	}
	if thread.frames[len(thread.frames)-1] != *frame || len(thread.frames) < 2 {
		return false
	}
	caller := thread.frames[len(thread.frames)-2]
	if caller == nil || !caller.hasPendingCall || caller.pendingCall.protected != nil || caller.pendingCall.destination.count != 1 {
		return false
	}
	destination := caller.pendingCall.destination
	thread.popFrame()
	caller.pendingCall = vmPendingCall{}
	caller.hasPendingCall = false
	caller.openResultStart = -1
	caller.openResults = vmResultWindow{}
	caller.setRegister(destination.register, value)
	*frame = caller
	return true
}

func runDirectFrameInstrumentedLoop(thread *vmThread, frameRef **vmFrame) directFrameSideExit {
	frame := *frameRef
	rootDepth := len(thread.frames) - 1
	var (
		proto                *Proto
		code                 []packedInstruction
		constants            []Value
		constantKeys         []tableKey
		constantKeyOK        []bool
		constantNumbers      []float64
		constantNumberOK     []bool
		registers            []Value
		picCounts            *directFramePICCounts
		runLineHook          bool
		runCountHook         bool
		runInstructionBudget bool
		directChildActive    bool
	)

reload:
	*frameRef = frame
	directChildActive = len(thread.frames)-1 > rootDepth
	proto = frame.proto
	code = proto.packedCode
	constants = proto.constants
	constantKeys = proto.constantKeys
	constantKeyOK = proto.constantKeyOK
	constantNumbers = proto.constantNumbers
	constantNumberOK = proto.constantNumberOK
	registers = frame.registers
	picCounts = thread.directFramePICCounts
	runLineHook = thread.debugHook != nil && thread.debugLineHook
	runCountHook = thread.debugCountInterval > 0 && thread.debugHook != nil
	runInstructionBudget = thread.instructionBudget >= 0

	for frame.pc < len(code) {
		if runInstructionBudget && !thread.consumeInstruction() {
			return directFrameReturn(vmFrameResult{state: vmCallStateHostInterrupt})
		}
		if runLineHook {
			if err := thread.runDebugLineHook(frame); err != nil {
				return directFrameFail(err)
			}
		}
		if runCountHook {
			if err := thread.runDebugCountHook(frame); err != nil {
				return directFrameFail(err)
			}
		}
		ins := code[frame.pc].unpack()
		if counts := thread.directFrameOpcodeCounts; counts != nil {
			counts[uint8(ins.op)]++
		}
		if pcCounts := thread.directFramePCCounts; pcCounts != nil {
			perProto := pcCounts[proto]
			if perProto == nil {
				perProto = make([]uint64, len(code))
				pcCounts[proto] = perProto
			}
			perProto[frame.pc]++
		}
		switch ins.op {
		case opLoadConst:
			registers[ins.a] = constants[ins.b]

		case opLoadGlobal:
			name, _ := constants[ins.b].String()
			value, ok, hit := thread.globals.getSlot(proto.globalSlot(ins.c, name), name)
			if hit {
				picCounts.addGlobalSlotHit()
			} else {
				picCounts.addGlobalSlotMiss()
			}
			if !ok {
				return directFrameFail(fmt.Errorf("run: undefined global %q", name))
			}
			registers[ins.a] = value

		case opSetGlobal:
			name, _ := constants[ins.a].String()
			thread.globals.setSlot(proto.globalSlot(ins.c, name), name, registers[ins.b])

		case opNewTable:
			registers[ins.a] = TableValue(newTableWithCapacity(ins.b, ins.c))

		case opMove:
			registers[ins.a] = registers[ins.b]

		case opGetUpvalue:
			value, err := frame.upvalue(ins.b)
			if err != nil {
				return directFrameFail(err)
			}
			registers[ins.a] = value

		case opSetUpvalue:
			if err := frame.setUpvalue(ins.a, registers[ins.b]); err != nil {
				return directFrameFail(err)
			}

		case opVararg:
			resultCount := ins.b
			if resultCount == 0 {
				resultCount = 1
			}
			if resultCount < 0 {
				frame.openResultStart = ins.a
				frame.openResults = vmAdjustedBorrowedResultWindow(frame.varargs)
				registers[ins.a] = frame.openResults.at(0)
				frame.pc++
				continue
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			for i := 0; i < resultCount; i++ {
				if i >= len(frame.varargs) {
					registers[ins.a+i] = NilValue()
				} else {
					registers[ins.a+i] = frame.varargs[i]
				}
			}

		case opSetField:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			if constantKeyOK[ins.b] {
				keyValue := constants[ins.b]
				var err error
				if valueKind(keyValue) == StringKind {
					table.setRawStringFieldBox(keyValue.stringText(), keyValue.stringBox(), registers[ins.c])
				} else {
					err = table.rawSetKey(constantKeys[ins.b], registers[ins.c])
				}
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				break
			}
			if err := table.rawSet(constants[ins.b], registers[ins.c]); err != nil {
				return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
			}

		case opSetStringField:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			keyValue := constants[ins.b]
			key := constantKeys[ins.b].str
			value := registers[ins.c]
			if valueKind(keyValue) == StringKind && !value.IsNil() {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if cache.hasValueKey(table, keyValue) {
					if cache.writeValueCounted(table, keyValue, value, picCounts) {
						break
					}
				} else {
					slot, slotOK := table.rawStringFieldSlotBox(keyValue.stringBox())
					if !slotOK {
						slot, slotOK = table.rawStringFieldSlot(key)
					}
					if slotOK {
						cache.storeValue(table, keyValue, slot)
						if cache.writeValueCounted(table, keyValue, value, picCounts) {
							break
						}
						if table.setRawStringFieldAtSlot(slot, key, value) {
							break
						}
					}
				}
			}
			// The boxed constant is retained for new keys and for the nil/delete
			// path; host-facing raw-string adapters remain text-only.
			table.setRawStringFieldBox(key, keyValue.stringBox(), value)

		case opSetStringFieldIndex:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			firstKey := constantKeys[ins.b].str
			firstBox := constants[ins.b].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = table.rawStringField(firstKey)
			}
			if !ok {
				if table.metatable != nil {
					picCounts.addMetatableMiss()
					return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
				}
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", NilValue().Kind()))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", first.Kind()))
			}
			if nextTable.metatable != nil {
				picCounts.addMetatableMiss()
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			key := registers[ins.c]
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				value := registers[ins.d]
				if cache.writeValueCounted(nextTable, key, value, picCounts) {
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok && nextTable.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(nextTable, key, slot)
					break
				}
			} else {
				picCounts.addInvalidKeyFallback()
			}
			if err := nextTable.rawSet(key, registers[ins.d]); err != nil {
				return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
			}

		case opGetStringField:
			base := registers[ins.b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			key := constants[ins.c]
			keyText := constantKeys[ins.c].str
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if value, ok := cache.getValueCounted(table, key, picCounts); ok {
					registers[ins.a] = value
					break
				}
				slot, slotOK := table.rawStringFieldSlotBox(key.stringBox())
				if !slotOK {
					slot, slotOK = table.rawStringFieldSlot(keyText)
				}
				if slotOK {
					if value, ok := table.rawStringFieldAtSlot(slot, keyText); ok {
						cache.storeValue(table, key, slot)
						registers[ins.a] = value
						break
					}
				}
			} else if value, ok := table.rawStringField(keyText); ok {
				// Keep the existing raw-string adapter as a defensive fallback for
				// malformed hand-built prototypes.
				registers[ins.a] = value
				break
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				value, ok, err := directFrameTableGetIsland(thread.globals, table, constants[ins.c])
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

		case opGetStringFieldIndex:
			base := registers[ins.b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			firstKey := constantKeys[ins.c].str
			firstBox := constants[ins.c].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = table.rawStringField(firstKey)
			}
			if !ok {
				if table.metatable != nil {
					picCounts.addMetatableMiss()
					return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
				}
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", NilValue().Kind()))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", first.Kind()))
			}
			if nextTable.metatable != nil {
				picCounts.addMetatableMiss()
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			key := registers[ins.d]
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if value, ok := cache.getValueCounted(nextTable, key, picCounts); ok {
					registers[ins.a] = value
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok {
					value, ok := nextTable.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(nextTable, key, slot)
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

		case opSetIndex:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addMetatableMiss()
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(thread.globals, table, registers[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			key := registers[ins.b]
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				value := registers[ins.c]
				if cache.writeValueCounted(table, key, value, picCounts) {
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok && table.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(table, key, slot)
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
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addMetatableMiss()
				picCounts.addSideExit(directFrameSideExitReasonTable)
				value, ok, err := directFrameTableGetIsland(thread.globals, table, registers[ins.c])
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
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if value, ok := cache.getValueCounted(table, key, picCounts); ok {
					registers[ins.a] = value
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok {
					value, ok := table.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(table, key, slot)
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
			child := proto.prototypes[ins.b]
			captured := captureUpvalues(child, frame)
			registers[ins.a] = functionValueWithCapturedUpvalues(child, captured)

		case opPrepareIter:
			iterValue := registers[ins.a]
			iterTable := iterValue.tableRef()
			if iterTable != nil && iterTable.metatable == nil {
				if tableCanIterateCleanArray(iterTable) {
					registers[ins.a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext)
					registers[ins.b] = iterValue
					registers[ins.c] = NilValue()
					break
				}
				registers[ins.a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncTableNext)
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
			var first Value
			var second Value
			var count int
			var ok bool
			var err error
			if valueNativeID(callee) == nativeFuncArrayNext {
				ok = true
				tableValue := registers[ins.c]
				table := tableValue.tableRef()
				if table == nil {
					err = fmt.Errorf("array iterator: argument #1 is %s, want table", tableValue.Kind())
				} else {
					controlValue := registers[ins.a]
					index := 0
					if valueKind(controlValue) != NilKind {
						if valueKind(controlValue) != NumberKind {
							err = fmt.Errorf("array iterator: index is %s, want number or nil", controlValue.Kind())
						} else {
							index = int(valueNumber(controlValue))
							if float64(index) != valueNumber(controlValue) {
								err = fmt.Errorf("array iterator: index is %s, want integer", controlValue.Kind())
							}
						}
					}
					if err == nil {
						next := index + 1
						if next < 1 || next > len(table.array) {
							first = NilValue()
							count = 1
						} else {
							first = NumberValue(float64(next))
							second = table.array[next-1]
							count = 2
						}
					}
				}
				picCounts.addArrayIteratorFastStep()
			} else {
				first, second, count, ok, err = directFrameIteratorNext(callee, registers[ins.c], registers[ins.a])
			}
			if !ok {
				return directFrameEnterGenericFrame()
			}
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			for i := 0; i < ins.d; i++ {
				if i >= count {
					registers[ins.a+i] = NilValue()
					continue
				}
				if i == 0 {
					registers[ins.a+i] = first
				} else {
					registers[ins.a+i] = second
				}
			}

		case opArrayNextJump2:
			callee := registers[ins.b]
			if valueNativeID(callee) == nativeFuncArrayNext {
				tableValue := registers[ins.c]
				table := tableValue.tableRef()
				if table == nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: argument #1 is %s, want table", tableValue.Kind()))
				}
				controlValue := registers[ins.a]
				index := 0
				if valueKind(controlValue) != NilKind {
					if valueKind(controlValue) != NumberKind {
						return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want number or nil", controlValue.Kind()))
					}
					index = int(valueNumber(controlValue))
					if float64(index) != valueNumber(controlValue) {
						return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want integer", controlValue.Kind()))
					}
				}
				picCounts.addArrayIteratorFastStep()
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				next := index + 1
				if next < 1 || next > len(table.array) {
					registers[ins.a] = NilValue()
					registers[ins.a+1] = NilValue()
					frame.pc = ins.d
					continue
				}
				registers[ins.a] = NumberValue(float64(next))
				registers[ins.a+1] = table.array[next-1]
				break
			}
			first, second, count, ok, err := directFrameIteratorNext(callee, registers[ins.c], registers[ins.a])
			if !ok {
				return directFrameEnterGenericFrame()
			}
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			if count < 1 || first.IsNil() {
				registers[ins.a] = NilValue()
				registers[ins.a+1] = NilValue()
				frame.pc = ins.d
				continue
			}
			registers[ins.a] = first
			if count > 1 {
				registers[ins.a+1] = second
			} else {
				registers[ins.a+1] = NilValue()
			}

		case opAdd:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__add",
					"add",
					func(left float64, right float64) float64 { return left + right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: add failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) + valueNumber(right))

		case opSub:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__sub",
					"subtract",
					func(left float64, right float64) float64 { return left - right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: subtract failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) - valueNumber(right))

		case opMul:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__mul",
					"multiply",
					func(left float64, right float64) float64 { return left * right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: multiply failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) * valueNumber(right))

		case opDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__div",
					"divide",
					func(left float64, right float64) float64 { return left / right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) / valueNumber(right))

		case opMod:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__mod",
					"modulo",
					math.Mod,
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: modulo failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/valueNumber(right))*valueNumber(right))

		case opIDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__idiv",
					"floor divide",
					func(left float64, right float64) float64 { return math.Floor(left / right) },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: floor divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(math.Floor(valueNumber(left) / valueNumber(right)))

		case opPow:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__pow",
					"power",
					math.Pow,
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: power failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(math.Pow(valueNumber(left), valueNumber(right)))

		case opAddK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__add",
					"add",
					func(left float64, right float64) float64 { return left + right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: add failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) + constantNumbers[ins.c])

		case opSubK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__sub",
					"subtract",
					func(left float64, right float64) float64 { return left - right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: subtract failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) - constantNumbers[ins.c])

		case opMulK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__mul",
					"multiply",
					func(left float64, right float64) float64 { return left * right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: multiply failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) * constantNumbers[ins.c])

		case opDivK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__div",
					"divide",
					func(left float64, right float64) float64 { return left / right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) / constantNumbers[ins.c])

		case opModK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__mod",
					"modulo",
					math.Mod,
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: modulo failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			right := constantNumbers[ins.c]
			registers[ins.a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/right)*right)

		case opIDivK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValue(
					picCounts,
					thread.globals,
					left,
					right,
					"__idiv",
					"floor divide",
					func(left float64, right float64) float64 { return math.Floor(left / right) },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: floor divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(math.Floor(valueNumber(left) / constantNumbers[ins.c]))

		case opNeg:
			operand := registers[ins.b]
			if valueKind(operand) != NumberKind {
				value, err := directFrameUnaryArithmeticValue(picCounts, thread.globals, operand, negateValue)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(-valueNumber(operand))

		case opLen:
			operand := registers[ins.b]
			switch valueKind(operand) {
			case StringKind:
				registers[ins.a] = NumberValue(float64(len(operand.stringText())))
			case TableKind:
				table := operand.tableRef()
				if table == nil {
					return directFrameFail(fmt.Errorf("run: length failed: table: nil table"))
				}
				if table.metatable != nil {
					picCounts.addSideExit(directFrameSideExitReasonMetatable)
					value, err := lengthValue(operand, thread.globals)
					if err != nil {
						return directFrameFail(fmt.Errorf("run: length failed: %w", err))
					}
					registers[ins.a] = value
					break
				}
				length, err := table.rawLen()
				if err != nil {
					return directFrameFail(fmt.Errorf("run: length failed: %w", err))
				}
				registers[ins.a] = NumberValue(float64(length))
			default:
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lengthValue(operand, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: length failed: %w", err))
				}
				registers[ins.a] = value
			}

		case opConcat:
			left := registers[ins.b]
			right := registers[ins.c]
			if !directFrameRawConcatOperand(left) || !directFrameRawConcatOperand(right) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := concatValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			concatValues := [2]Value{left, right}
			if value, ok := thread.internStringConcatValues(concatValues[:]); ok {
				registers[ins.a] = value
				break
			}
			leftText, err := concatOperandString(left, "left")
			if err != nil {
				return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
			}
			rightText, err := concatOperandString(right, "right")
			if err != nil {
				return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
			}
			registers[ins.a] = thread.internStringValue(leftText + rightText)

		case opConcatChain:
			if value, ok := thread.internStringConcatValues(registers[ins.b : ins.b+ins.c]); ok {
				registers[ins.a] = value
				break
			}
			text, ok, err := thread.concatRawChainString(registers[ins.b : ins.b+ins.c])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
			}
			if !ok {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := concatChainValue(registers[ins.b:ins.b+ins.c], thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = thread.internStringValue(text)

		case opEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valuesEqual(left, right))

		case opNotEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(!value)
				break
			}
			registers[ins.a] = BoolValue(!valuesEqual(left, right))

		case opLess:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() < right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: less failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) < valueNumber(right))

		case opLessEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() <= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessEqualValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: less equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) <= valueNumber(right))

		case opGreater:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() > right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessValue(right, left, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) > valueNumber(right))

		case opGreaterEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() >= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessEqualValue(right, left, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: greater equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) >= valueNumber(right))

		case opNumericForCheck:
			loopValue := registers[ins.a]
			limitValue := registers[ins.b]
			stepValue := registers[ins.c]
			if valueKind(loopValue) != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind()))
			}
			if valueKind(limitValue) != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind()))
			}
			if valueKind(stepValue) != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind()))
			}
			if math.IsNaN(valueNumber(loopValue)) || math.IsNaN(valueNumber(limitValue)) || math.IsNaN(valueNumber(stepValue)) {
				return directFrameFail(fmt.Errorf("run: numeric for operand is NaN"))
			}
			if valueNumber(stepValue) > 0 {
				if valueNumber(loopValue) > valueNumber(limitValue) {
					frame.pc = ins.d
					continue
				}
				break
			}
			if valueNumber(loopValue) < valueNumber(limitValue) {
				frame.pc = ins.d
				continue
			}

		case opNumericForLoop:
			loopValue := registers[ins.a]
			stepValue := registers[ins.b]
			if valueKind(loopValue) != NumberKind || valueKind(stepValue) != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(valueNumber(loopValue) + valueNumber(stepValue))
			frame.pc = ins.d
			continue

		case opJumpIfNotEqualK:
			left := registers[ins.a]
			if valueKind(left) == NumberKind && constantNumberOK[ins.b] {
				if valueNumber(left) != constantNumbers[ins.b] {
					frame.pc = ins.d
					continue
				}
				break
			}
			if valueKind(left) == StringKind && constantKeyOK[ins.b] {
				if left.stringText() != constantKeys[ins.b].str {
					frame.pc = ins.d
					continue
				}
				break
			}
			right := constants[ins.b]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
			}
			equal, err := equalValue(left, right, thread.globals)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
			}
			if !equal {
				frame.pc = ins.d
				continue
			}

		case opJumpIfTableHasMetatable:
			base := registers[ins.a]
			if table := base.tableRef(); table != nil && table.metatable != nil {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLessK:
			left := registers[ins.a]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, constants[ins.b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if !less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotGreaterK:
			left := registers[ins.a]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, constants[ins.b], left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if !greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfLessK:
			left := registers[ins.a]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, constants[ins.b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfGreaterK:
			left := registers[ins.a]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, constants[ins.b], left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLess:
			left := registers[ins.a]
			right := registers[ins.b]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, right)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if !less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotGreater:
			left := registers[ins.a]
			right := registers[ins.b]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, right, left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if !greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfLess:
			left := registers[ins.a]
			right := registers[ins.b]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, right)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfGreater:
			left := registers[ins.a]
			right := registers[ins.b]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, right, left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfModKNotEqualK:
			left := registers[ins.a]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.b] || !constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			modRight := constantNumbers[ins.b]
			want := constantNumbers[ins.c]
			got := valueNumber(left) - math.Floor(valueNumber(left)/modRight)*modRight
			if got != want {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotEqualK:
			left, ok, err := directFrameStringFieldBox(registers[ins.a], constantKeys[ins.b].str, constants[ins.b].stringBox())
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			if !ok {
				return directFrameEnterGenericFrame()
			}
			right := constants[ins.c]
			if valueKind(left) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == TableKind || valueKind(right) == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			if equal, fast := directFrameScalarValuesEqual(left, right); fast {
				picCounts.addScalarEqualityFastCheck()
				if !equal {
					frame.pc = ins.d
					continue
				}
				break
			}
			if !valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
			left, ok, err := directFrameStringFieldBox(registers[ins.a], constantKeys[ins.b].str, constants[ins.b].stringBox())
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			if !ok || valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			right := constantNumbers[ins.c]
			if math.IsNaN(valueNumber(left)) || math.IsNaN(right) {
				return directFrameEnterGenericFrame()
			}
			greater := valueNumber(left) > right
			if (ins.op == opJumpIfStringFieldNotGreaterK && !greater) ||
				(ins.op == opJumpIfStringFieldGreaterK && greater) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterR:
			left, ok, err := directFrameStringFieldBox(registers[ins.a], constantKeys[ins.b].str, constants[ins.b].stringBox())
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			right := registers[ins.c]
			if !ok || valueKind(left) != NumberKind || valueKind(right) != NumberKind ||
				math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				return directFrameEnterGenericFrame()
			}
			if !(valueNumber(left) > valueNumber(right)) {
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
			if ins.c == 2 && resultCount == 2 {
				first, second, count, ok, err := directFrameIteratorNext(callee, registers[ins.b+1], registers[ins.b+2])
				if ok {
					if err != nil {
						return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
					}
					frame.openResultStart = -1
					frame.openResults = vmResultWindow{}
					for i := 0; i < resultCount; i++ {
						if i >= count {
							registers[ins.a+i] = NilValue()
						} else if i == 0 {
							registers[ins.a+i] = first
						} else {
							registers[ins.a+i] = second
						}
					}
					break
				}
			}
			if resultCount == 1 && valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[ins.b+1 : ins.b+1+ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = value
				break
			}
			if resultCount == 1 && valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if ins.c > 0 {
					value = registers[ins.b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = result
				break
			}
			if closure, ok := callee.scriptFunction(); ok && ins.c >= 0 {
				destination := vmResultDestination{register: ins.a, count: ins.d}
				args := registers[ins.b+1 : ins.b+1+ins.c]
				frame.pc++
				result, err := thread.runInlineScriptCall(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						thread.installPendingCall(frame, vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						})
						return directFrameYield(vmYieldedValues(yield.values))
					}
					return directFrameFail(err)
				}
				// A nested materialized call may grow the shared register arena and
				// rebind every live frame. Refresh the dispatch slice before the
				// caller resumes so subsequent instructions cannot read the stale
				// backing array.
				registers = frame.registers
				frame.applyValueListDestination(destination, result.window)
				continue
			}
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)

		case opCallOne:
			callee := registers[ins.b]
			argCount, borrowHint := decodeFixedCallCount(ins.c)
			if valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[ins.b+1 : ins.b+1+argCount])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = value
				break
			}
			if valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if argCount > 0 {
					value = registers[ins.b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = result
				break
			}
			if borrowHint {
				if closure, ok := callee.scriptFunction(); ok {
					if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.b+1, argCount); borrowed {
						frame.pc++
						installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
						thread.pushFrame(child)
						thread.directFramePICCounts.addFixedCallTrampolineEntry()
						frame = child
						directChildActive = true
						goto reload
					}
				}
			}
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)

		case opCallLocalOne:
			callee := registers[ins.b]
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			argCount, borrowHint := decodeFixedCallCount(ins.d)
			frame.pc++
			if borrowHint {
				if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.c, argCount); borrowed {
					installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
					thread.pushFrame(child)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					frame = child
					directChildActive = true
					goto reload
				}
			}
			var value Value
			var callErr error
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, ins.c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[ins.c : ins.c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: ins.a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(callErr)
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[ins.a] = value
			continue

		case opCallUpvalueOne:
			callee, err := frame.upvalue(ins.b)
			if err != nil {
				return directFrameFail(err)
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			argCount, borrowHint := decodeFixedCallCount(ins.d)
			frame.pc++
			var value Value
			var callErr error
			if borrowHint {
				if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.c, argCount); borrowed {
					installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
					thread.pushFrame(child)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, ins.c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[ins.c : ins.c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: ins.a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(callErr)
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[ins.a] = value
			continue

		case opCallMethodOne:
			receiver := registers[ins.b]
			table := receiver.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", receiver.Kind()))
			}
			key := constantKeys[ins.c].str
			callee, ok := table.rawStringFieldBox(constants[ins.c].stringBox())
			if constants[ins.c].stringBox() == nil {
				callee, ok = table.rawStringField(key)
			}
			if !ok {
				if table.metatable != nil {
					picCounts.addMetatableMiss()
					return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
				}
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			registers[ins.a+1] = receiver
			frame.pc++
			explicitCount, borrowHint := decodeFixedCallCount(ins.d)
			argCount := explicitCount + 1
			var value Value
			var err error
			if borrowHint && table.metatable == nil {
				if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.a+1, argCount); borrowed {
					installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
					thread.pushFrame(child)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, ins.a+1, argCount)
				value, err = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[ins.a+1 : ins.a+1+argCount]
				value, err = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: ins.a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(err)
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[ins.a] = value
			continue

		case opFastCall:
			exit := thread.runDirectFastCall(frame, nativeFuncID(ins.b), ins.a, ins.c, ins.d, picCounts)
			if exit.resumesDirectFrame() {
				break
			}
			return exit

		case opReturnOne:
			if directChildActive && thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[ins.a]) {
				goto reload
			}
			result := vmReturnedValue(registers[ins.a])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameReturn(result)

		case opReturn:
			count := ins.b
			if count < 0 {
				prefixCount := -count - 1
				if frame.openResultStart == ins.a+prefixCount {
					result := vmReturnedPrefixAndWindow(registers[ins.a:ins.a+prefixCount], frame.openResults)
					if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
						goto reload
					}
					return directFrameReturn(result)
				}
				result := vmReturnedValue(registers[ins.a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			if count == 0 {
				result := vmReturnedValues(nil)
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			if count == 1 {
				if directChildActive && thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[ins.a]) {
					goto reload
				}
				result := vmReturnedValue(registers[ins.a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			result := vmReturnedBorrowedValues(registers[ins.a : ins.a+count])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameReturn(result)

		default:
			return directFrameEnterGenericFrame()
		}
		frame.pc++
	}
	result := vmReturnedValues(nil)
	if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
		goto reload
	}
	return directFrameReturn(result)
}

func runDirectFrameProductionLoop(thread *vmThread, frameRef **vmFrame) directFrameSideExit {
	frame := *frameRef
	rootDepth := len(thread.frames) - 1
	var (
		proto             *Proto
		code              []packedInstruction
		constants         []Value
		constantKeys      []tableKey
		constantKeyOK     []bool
		constantNumbers   []float64
		constantNumberOK  []bool
		registers         []Value
		directChildActive bool
	)

reload:
	*frameRef = frame
	directChildActive = len(thread.frames)-1 > rootDepth
	proto = frame.proto
	code = proto.packedCode
	constants = proto.constants
	constantKeys = proto.constantKeys
	constantKeyOK = proto.constantKeyOK
	constantNumbers = proto.constantNumbers
	constantNumberOK = proto.constantNumberOK
	registers = frame.registers

	for frame.pc < len(code) {
		ins := code[frame.pc].unpack()
		switch ins.op {
		case opLoadConst:
			registers[ins.a] = constants[ins.b]

		case opLoadGlobal:
			name, _ := constants[ins.b].String()
			value, ok, _ := thread.globals.getSlot(proto.globalSlot(ins.c, name), name)
			if !ok {
				return directFrameFail(fmt.Errorf("run: undefined global %q", name))
			}
			registers[ins.a] = value

		case opSetGlobal:
			name, _ := constants[ins.a].String()
			thread.globals.setSlot(proto.globalSlot(ins.c, name), name, registers[ins.b])

		case opNewTable:
			registers[ins.a] = TableValue(newTableWithCapacity(ins.b, ins.c))

		case opMove:
			registers[ins.a] = registers[ins.b]

		case opGetUpvalue:
			value, err := frame.upvalue(ins.b)
			if err != nil {
				return directFrameFail(err)
			}
			registers[ins.a] = value

		case opSetUpvalue:
			if err := frame.setUpvalue(ins.a, registers[ins.b]); err != nil {
				return directFrameFail(err)
			}

		case opVararg:
			resultCount := ins.b
			if resultCount == 0 {
				resultCount = 1
			}
			if resultCount < 0 {
				frame.openResultStart = ins.a
				frame.openResults = vmAdjustedBorrowedResultWindow(frame.varargs)
				registers[ins.a] = frame.openResults.at(0)
				frame.pc++
				continue
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			for i := 0; i < resultCount; i++ {
				if i >= len(frame.varargs) {
					registers[ins.a+i] = NilValue()
				} else {
					registers[ins.a+i] = frame.varargs[i]
				}
			}

		case opSetField:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			if constantKeyOK[ins.b] {
				keyValue := constants[ins.b]
				var err error
				if valueKind(keyValue) == StringKind {
					table.setRawStringFieldBox(keyValue.stringText(), keyValue.stringBox(), registers[ins.c])
				} else {
					err = table.rawSetKey(constantKeys[ins.b], registers[ins.c])
				}
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				break
			}
			if err := table.rawSet(constants[ins.b], registers[ins.c]); err != nil {
				return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
			}

		case opSetStringField:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			keyValue := constants[ins.b]
			key := constantKeys[ins.b].str
			value := registers[ins.c]
			if valueKind(keyValue) == StringKind && !value.IsNil() {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if cache.hasValueKey(table, keyValue) {
					if cache.writeValue(table, keyValue, value) {
						break
					}
				} else {
					slot, slotOK := table.rawStringFieldSlotBox(keyValue.stringBox())
					if !slotOK {
						slot, slotOK = table.rawStringFieldSlot(key)
					}
					if slotOK {
						cache.storeValue(table, keyValue, slot)
						if cache.writeValue(table, keyValue, value) {
							break
						}
						if table.setRawStringFieldAtSlot(slot, key, value) {
							break
						}
					}
				}
			}
			// The boxed constant is retained for new keys and for the nil/delete
			// path; host-facing raw-string adapters remain text-only.
			table.setRawStringFieldBox(key, keyValue.stringBox(), value)

		case opSetStringFieldIndex:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			firstKey := constantKeys[ins.b].str
			firstBox := constants[ins.b].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = table.rawStringField(firstKey)
			}
			if !ok {
				if table.metatable != nil {
					return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
				}
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", NilValue().Kind()))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", first.Kind()))
			}
			if nextTable.metatable != nil {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			key := registers[ins.c]
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				value := registers[ins.d]
				if cache.writeValue(nextTable, key, value) {
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok && nextTable.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(nextTable, key, slot)
					break
				}
			}
			if err := nextTable.rawSet(key, registers[ins.d]); err != nil {
				return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
			}

		case opGetStringField:
			base := registers[ins.b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			key := constants[ins.c]
			keyText := constantKeys[ins.c].str
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if value, ok := cache.getValue(table, key); ok {
					registers[ins.a] = value
					break
				}
				slot, slotOK := table.rawStringFieldSlotBox(key.stringBox())
				if !slotOK {
					slot, slotOK = table.rawStringFieldSlot(keyText)
				}
				if slotOK {
					if value, ok := table.rawStringFieldAtSlot(slot, keyText); ok {
						cache.storeValue(table, key, slot)
						registers[ins.a] = value
						break
					}
				}
			} else if value, ok := table.rawStringField(keyText); ok {
				// Keep the existing raw-string adapter as a defensive fallback for
				// malformed hand-built prototypes.
				registers[ins.a] = value
				break
			}
			if table.metatable != nil {
				value, ok, err := directFrameTableGetIsland(thread.globals, table, constants[ins.c])
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

		case opGetStringFieldIndex:
			base := registers[ins.b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			firstKey := constantKeys[ins.c].str
			firstBox := constants[ins.c].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = table.rawStringField(firstKey)
			}
			if !ok {
				if table.metatable != nil {
					return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
				}
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", NilValue().Kind()))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", first.Kind()))
			}
			if nextTable.metatable != nil {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
			}
			key := registers[ins.d]
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if value, ok := cache.getValue(nextTable, key); ok {
					registers[ins.a] = value
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok {
					value, ok := nextTable.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(nextTable, key, slot)
						registers[ins.a] = value
						break
					}
				}
			}
			value, err := nextTable.rawGet(key)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
			}
			registers[ins.a] = value

		case opSetIndex:
			base := registers[ins.a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, registers[ins.b], registers[ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			key := registers[ins.b]
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				value := registers[ins.c]
				if cache.writeValue(table, key, value) {
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok && table.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(table, key, slot)
					break
				}
			}
			if err := table.rawSet(registers[ins.b], registers[ins.c]); err != nil {
				return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
			}

		case opGetIndex:
			base := registers[ins.b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				value, ok, err := directFrameTableGetIsland(thread.globals, table, registers[ins.c])
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
			if valueKind(key) == StringKind {
				cache := proto.directFrameIndexCacheAt(frame.pc)
				if value, ok := cache.getValue(table, key); ok {
					registers[ins.a] = value
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok {
					value, ok := table.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(table, key, slot)
						registers[ins.a] = value
						break
					}
				}
			} else if index, ok := tableArrayIndexFromValue(key); ok && index <= len(table.array) {
				registers[ins.a] = table.array[index-1]
				break
			}
			value, err := table.rawGet(key)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
			}
			registers[ins.a] = value

		case opClosure:
			child := proto.prototypes[ins.b]
			captured := captureUpvalues(child, frame)
			registers[ins.a] = functionValueWithCapturedUpvalues(child, captured)

		case opPrepareIter:
			iterValue := registers[ins.a]
			iterTable := iterValue.tableRef()
			if iterTable != nil && iterTable.metatable == nil {
				if tableCanIterateCleanArray(iterTable) {
					registers[ins.a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext)
					registers[ins.b] = iterValue
					registers[ins.c] = NilValue()
					break
				}
				registers[ins.a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncTableNext)
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
			var first Value
			var second Value
			var count int
			var ok bool
			var err error
			if valueNativeID(callee) == nativeFuncArrayNext {
				ok = true
				tableValue := registers[ins.c]
				table := tableValue.tableRef()
				if table == nil {
					err = fmt.Errorf("array iterator: argument #1 is %s, want table", tableValue.Kind())
				} else {
					controlValue := registers[ins.a]
					index := 0
					if valueKind(controlValue) != NilKind {
						if valueKind(controlValue) != NumberKind {
							err = fmt.Errorf("array iterator: index is %s, want number or nil", controlValue.Kind())
						} else {
							index = int(valueNumber(controlValue))
							if float64(index) != valueNumber(controlValue) {
								err = fmt.Errorf("array iterator: index is %s, want integer", controlValue.Kind())
							}
						}
					}
					if err == nil {
						next := index + 1
						if next < 1 || next > len(table.array) {
							first = NilValue()
							count = 1
						} else {
							first = NumberValue(float64(next))
							second = table.array[next-1]
							count = 2
						}
					}
				}
			} else {
				first, second, count, ok, err = directFrameIteratorNext(callee, registers[ins.c], registers[ins.a])
			}
			if !ok {
				return directFrameEnterGenericFrame()
			}
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			for i := 0; i < ins.d; i++ {
				if i >= count {
					registers[ins.a+i] = NilValue()
					continue
				}
				if i == 0 {
					registers[ins.a+i] = first
				} else {
					registers[ins.a+i] = second
				}
			}

		case opArrayNextJump2:
			callee := registers[ins.b]
			if valueNativeID(callee) == nativeFuncArrayNext {
				tableValue := registers[ins.c]
				table := tableValue.tableRef()
				if table == nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: argument #1 is %s, want table", tableValue.Kind()))
				}
				controlValue := registers[ins.a]
				index := 0
				if valueKind(controlValue) != NilKind {
					if valueKind(controlValue) != NumberKind {
						return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want number or nil", controlValue.Kind()))
					}
					index = int(valueNumber(controlValue))
					if float64(index) != valueNumber(controlValue) {
						return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want integer", controlValue.Kind()))
					}
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				next := index + 1
				if next < 1 || next > len(table.array) {
					registers[ins.a] = NilValue()
					registers[ins.a+1] = NilValue()
					frame.pc = ins.d
					continue
				}
				registers[ins.a] = NumberValue(float64(next))
				registers[ins.a+1] = table.array[next-1]
				break
			}
			first, second, count, ok, err := directFrameIteratorNext(callee, registers[ins.c], registers[ins.a])
			if !ok {
				return directFrameEnterGenericFrame()
			}
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			if count < 1 || first.IsNil() {
				registers[ins.a] = NilValue()
				registers[ins.a+1] = NilValue()
				frame.pc = ins.d
				continue
			}
			registers[ins.a] = first
			if count > 1 {
				registers[ins.a+1] = second
			} else {
				registers[ins.a+1] = NilValue()
			}

		case opAdd:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__add",
					"add",
					func(left float64, right float64) float64 { return left + right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: add failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) + valueNumber(right))

		case opSub:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__sub",
					"subtract",
					func(left float64, right float64) float64 { return left - right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: subtract failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) - valueNumber(right))

		case opMul:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mul",
					"multiply",
					func(left float64, right float64) float64 { return left * right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: multiply failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) * valueNumber(right))

		case opDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__div",
					"divide",
					func(left float64, right float64) float64 { return left / right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) / valueNumber(right))

		case opMod:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mod",
					"modulo",
					math.Mod,
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: modulo failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/valueNumber(right))*valueNumber(right))

		case opIDiv:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__idiv",
					"floor divide",
					func(left float64, right float64) float64 { return math.Floor(left / right) },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: floor divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(math.Floor(valueNumber(left) / valueNumber(right)))

		case opPow:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__pow",
					"power",
					math.Pow,
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: power failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(math.Pow(valueNumber(left), valueNumber(right)))

		case opAddK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__add",
					"add",
					func(left float64, right float64) float64 { return left + right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: add failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) + constantNumbers[ins.c])

		case opSubK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__sub",
					"subtract",
					func(left float64, right float64) float64 { return left - right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: subtract failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) - constantNumbers[ins.c])

		case opMulK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mul",
					"multiply",
					func(left float64, right float64) float64 { return left * right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: multiply failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) * constantNumbers[ins.c])

		case opDivK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__div",
					"divide",
					func(left float64, right float64) float64 { return left / right },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(valueNumber(left) / constantNumbers[ins.c])

		case opModK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mod",
					"modulo",
					math.Mod,
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: modulo failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			right := constantNumbers[ins.c]
			registers[ins.a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/right)*right)

		case opIDivK:
			left := registers[ins.b]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				right := constants[ins.c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__idiv",
					"floor divide",
					func(left float64, right float64) float64 { return math.Floor(left / right) },
				)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: floor divide failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(math.Floor(valueNumber(left) / constantNumbers[ins.c]))

		case opNeg:
			operand := registers[ins.b]
			if valueKind(operand) != NumberKind {
				value, err := directFrameUnaryArithmeticValueUncounted(thread.globals, operand, negateValue)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = NumberValue(-valueNumber(operand))

		case opLen:
			operand := registers[ins.b]
			switch valueKind(operand) {
			case StringKind:
				registers[ins.a] = NumberValue(float64(len(operand.stringText())))
			case TableKind:
				table := operand.tableRef()
				if table == nil {
					return directFrameFail(fmt.Errorf("run: length failed: table: nil table"))
				}
				if table.metatable != nil {
					value, err := lengthValue(operand, thread.globals)
					if err != nil {
						return directFrameFail(fmt.Errorf("run: length failed: %w", err))
					}
					registers[ins.a] = value
					break
				}
				length, err := table.rawLen()
				if err != nil {
					return directFrameFail(fmt.Errorf("run: length failed: %w", err))
				}
				registers[ins.a] = NumberValue(float64(length))
			default:
				value, err := lengthValue(operand, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: length failed: %w", err))
				}
				registers[ins.a] = value
			}

		case opConcat:
			left := registers[ins.b]
			right := registers[ins.c]
			if !directFrameRawConcatOperand(left) || !directFrameRawConcatOperand(right) {
				value, err := concatValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			concatValues := [2]Value{left, right}
			if value, ok := thread.internStringConcatValues(concatValues[:]); ok {
				registers[ins.a] = value
				break
			}
			leftText, err := concatOperandString(left, "left")
			if err != nil {
				return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
			}
			rightText, err := concatOperandString(right, "right")
			if err != nil {
				return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
			}
			registers[ins.a] = thread.internStringValue(leftText + rightText)

		case opConcatChain:
			if value, ok := thread.internStringConcatValues(registers[ins.b : ins.b+ins.c]); ok {
				registers[ins.a] = value
				break
			}
			text, ok, err := thread.concatRawChainString(registers[ins.b : ins.b+ins.c])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
			}
			if !ok {
				value, err := concatChainValue(registers[ins.b:ins.b+ins.c], thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
				}
				registers[ins.a] = value
				break
			}
			registers[ins.a] = thread.internStringValue(text)

		case opEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valuesEqual(left, right))

		case opNotEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(!value)
				break
			}
			registers[ins.a] = BoolValue(!valuesEqual(left, right))

		case opLess:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() < right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: less failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) < valueNumber(right))

		case opLessEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() <= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessEqualValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: less equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) <= valueNumber(right))

		case opGreater:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() > right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessValue(right, left, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) > valueNumber(right))

		case opGreaterEqual:
			left := registers[ins.b]
			right := registers[ins.c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[ins.a] = BoolValue(left.stringText() >= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessEqualValue(right, left, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: greater equal failed: %w", err))
				}
				registers[ins.a] = BoolValue(value)
				break
			}
			registers[ins.a] = BoolValue(valueNumber(left) >= valueNumber(right))

		case opNumericForCheck:
			loopValue := registers[ins.a]
			limitValue := registers[ins.b]
			stepValue := registers[ins.c]
			if valueKind(loopValue) != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind()))
			}
			if valueKind(limitValue) != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind()))
			}
			if valueKind(stepValue) != NumberKind {
				return directFrameFail(fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind()))
			}
			if math.IsNaN(valueNumber(loopValue)) || math.IsNaN(valueNumber(limitValue)) || math.IsNaN(valueNumber(stepValue)) {
				return directFrameFail(fmt.Errorf("run: numeric for operand is NaN"))
			}
			if valueNumber(stepValue) > 0 {
				if valueNumber(loopValue) > valueNumber(limitValue) {
					frame.pc = ins.d
					continue
				}
				break
			}
			if valueNumber(loopValue) < valueNumber(limitValue) {
				frame.pc = ins.d
				continue
			}

		case opNumericForLoop:
			loopValue := registers[ins.a]
			stepValue := registers[ins.b]
			if valueKind(loopValue) != NumberKind || valueKind(stepValue) != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[ins.a] = NumberValue(valueNumber(loopValue) + valueNumber(stepValue))
			frame.pc = ins.d
			continue

		case opJumpIfNotEqualK:
			left := registers[ins.a]
			if valueKind(left) == NumberKind && constantNumberOK[ins.b] {
				if valueNumber(left) != constantNumbers[ins.b] {
					frame.pc = ins.d
					continue
				}
				break
			}
			if valueKind(left) == StringKind && constantKeyOK[ins.b] {
				if left.stringText() != constantKeys[ins.b].str {
					frame.pc = ins.d
					continue
				}
				break
			}
			right := constants[ins.b]
			equal, err := equalValue(left, right, thread.globals)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
			}
			if !equal {
				frame.pc = ins.d
				continue
			}

		case opJumpIfTableHasMetatable:
			base := registers[ins.a]
			if table := base.tableRef(); table != nil && table.metatable != nil {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLessK:
			left := registers[ins.a]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, constants[ins.b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if !less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotGreaterK:
			left := registers[ins.a]
			greater, err := directFrameLessForBranchUncounted(thread.globals, constants[ins.b], left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if !greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfLessK:
			left := registers[ins.a]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, constants[ins.b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfGreaterK:
			left := registers[ins.a]
			greater, err := directFrameLessForBranchUncounted(thread.globals, constants[ins.b], left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLess:
			left := registers[ins.a]
			right := registers[ins.b]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, right)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if !less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotGreater:
			left := registers[ins.a]
			right := registers[ins.b]
			greater, err := directFrameLessForBranchUncounted(thread.globals, right, left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if !greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfLess:
			left := registers[ins.a]
			right := registers[ins.b]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, right)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if less {
				frame.pc = ins.d
				continue
			}

		case opJumpIfGreater:
			left := registers[ins.a]
			right := registers[ins.b]
			greater, err := directFrameLessForBranchUncounted(thread.globals, right, left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if greater {
				frame.pc = ins.d
				continue
			}

		case opJumpIfModKNotEqualK:
			left := registers[ins.a]
			if valueKind(left) != NumberKind || !constantNumberOK[ins.b] || !constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			modRight := constantNumbers[ins.b]
			want := constantNumbers[ins.c]
			got := valueNumber(left) - math.Floor(valueNumber(left)/modRight)*modRight
			if got != want {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotEqualK:
			left, ok, err := directFrameStringFieldBox(registers[ins.a], constantKeys[ins.b].str, constants[ins.b].stringBox())
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			if !ok {
				return directFrameEnterGenericFrame()
			}
			right := constants[ins.c]
			if valueKind(left) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == TableKind || valueKind(right) == UserDataKind {
				return directFrameEnterGenericFrame()
			}
			if equal, fast := directFrameScalarValuesEqual(left, right); fast {
				if !equal {
					frame.pc = ins.d
					continue
				}
				break
			}
			if !valuesEqual(left, right) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
			left, ok, err := directFrameStringFieldBox(registers[ins.a], constantKeys[ins.b].str, constants[ins.b].stringBox())
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			if !ok || valueKind(left) != NumberKind || !constantNumberOK[ins.c] {
				return directFrameEnterGenericFrame()
			}
			right := constantNumbers[ins.c]
			if math.IsNaN(valueNumber(left)) || math.IsNaN(right) {
				return directFrameEnterGenericFrame()
			}
			greater := valueNumber(left) > right
			if (ins.op == opJumpIfStringFieldNotGreaterK && !greater) ||
				(ins.op == opJumpIfStringFieldGreaterK && greater) {
				frame.pc = ins.d
				continue
			}

		case opJumpIfStringFieldNotGreaterR:
			left, ok, err := directFrameStringFieldBox(registers[ins.a], constantKeys[ins.b].str, constants[ins.b].stringBox())
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
			}
			right := registers[ins.c]
			if !ok || valueKind(left) != NumberKind || valueKind(right) != NumberKind ||
				math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				return directFrameEnterGenericFrame()
			}
			if !(valueNumber(left) > valueNumber(right)) {
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
			if ins.c == 2 && resultCount == 2 {
				first, second, count, ok, err := directFrameIteratorNext(callee, registers[ins.b+1], registers[ins.b+2])
				if ok {
					if err != nil {
						return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
					}
					frame.openResultStart = -1
					frame.openResults = vmResultWindow{}
					for i := 0; i < resultCount; i++ {
						if i >= count {
							registers[ins.a+i] = NilValue()
						} else if i == 0 {
							registers[ins.a+i] = first
						} else {
							registers[ins.a+i] = second
						}
					}
					break
				}
			}
			if resultCount == 1 && valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[ins.b+1 : ins.b+1+ins.c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = value
				break
			}
			if resultCount == 1 && valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if ins.c > 0 {
					value = registers[ins.b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = result
				break
			}
			if closure, ok := callee.scriptFunction(); ok && ins.c >= 0 {
				destination := vmResultDestination{register: ins.a, count: ins.d}
				args := registers[ins.b+1 : ins.b+1+ins.c]
				frame.pc++
				result, err := thread.runInlineScriptCall(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						thread.installPendingCall(frame, vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						})
						return directFrameYield(vmYieldedValues(yield.values))
					}
					return directFrameFail(err)
				}
				// A nested materialized call may grow the shared register arena and
				// rebind every live frame. Refresh the dispatch slice before the
				// caller resumes so subsequent instructions cannot read the stale
				// backing array.
				registers = frame.registers
				frame.applyValueListDestination(destination, result.window)
				continue
			}
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)

		case opCallOne:
			callee := registers[ins.b]
			argCount, borrowHint := decodeFixedCallCount(ins.c)
			if valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[ins.b+1 : ins.b+1+argCount])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = value
				break
			}
			if valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if argCount > 0 {
					value = registers[ins.b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[ins.a] = result
				break
			}
			if borrowHint {
				if closure, ok := callee.scriptFunction(); ok {
					if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.b+1, argCount); borrowed {
						frame.pc++
						installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
						thread.pushFrame(child)
						frame = child
						directChildActive = true
						goto reload
					}
				}
			}
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)

		case opCallLocalOne:
			callee := registers[ins.b]
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			argCount, borrowHint := decodeFixedCallCount(ins.d)
			frame.pc++
			if borrowHint {
				if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.c, argCount); borrowed {
					installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
					thread.pushFrame(child)
					frame = child
					directChildActive = true
					goto reload
				}
			}
			var value Value
			var callErr error
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, ins.c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[ins.c : ins.c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: ins.a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(callErr)
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[ins.a] = value
			continue

		case opCallUpvalueOne:
			callee, err := frame.upvalue(ins.b)
			if err != nil {
				return directFrameFail(err)
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			argCount, borrowHint := decodeFixedCallCount(ins.d)
			frame.pc++
			var value Value
			var callErr error
			if borrowHint {
				if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.c, argCount); borrowed {
					installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
					thread.pushFrame(child)
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, ins.c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[ins.c : ins.c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: ins.a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(callErr)
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[ins.a] = value
			continue

		case opCallMethodOne:
			receiver := registers[ins.b]
			table := receiver.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", receiver.Kind()))
			}
			key := constantKeys[ins.c].str
			callee, ok := table.rawStringFieldBox(constants[ins.c].stringBox())
			if constants[ins.c].stringBox() == nil {
				callee, ok = table.rawStringField(key)
			}
			if !ok {
				if table.metatable != nil {
					return directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable)
				}
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			registers[ins.a+1] = receiver
			frame.pc++
			explicitCount, borrowHint := decodeFixedCallCount(ins.d)
			argCount := explicitCount + 1
			var value Value
			var err error
			if borrowHint && table.metatable == nil {
				if child, borrowed := thread.newBorrowedClosureCallFrame(closure, frame, ins.a+1, argCount); borrowed {
					installFixedResultPendingCall(frame, vmResultDestination{register: ins.a, count: 1})
					thread.pushFrame(child)
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, ins.a+1, argCount)
				value, err = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[ins.a+1 : ins.a+1+argCount]
				value, err = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: ins.a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(err)
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[ins.a] = value
			continue

		case opFastCall:
			exit := thread.runDirectFastCall(frame, nativeFuncID(ins.b), ins.a, ins.c, ins.d, nil)
			if exit.resumesDirectFrame() {
				break
			}
			return exit

		case opReturnOne:
			if directChildActive && thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[ins.a]) {
				goto reload
			}
			result := vmReturnedValue(registers[ins.a])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameReturn(result)

		case opReturn:
			count := ins.b
			if count < 0 {
				prefixCount := -count - 1
				if frame.openResultStart == ins.a+prefixCount {
					result := vmReturnedPrefixAndWindow(registers[ins.a:ins.a+prefixCount], frame.openResults)
					if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
						goto reload
					}
					return directFrameReturn(result)
				}
				result := vmReturnedValue(registers[ins.a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			if count == 0 {
				result := vmReturnedValues(nil)
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			if count == 1 {
				if directChildActive && thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[ins.a]) {
					goto reload
				}
				result := vmReturnedValue(registers[ins.a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			result := vmReturnedBorrowedValues(registers[ins.a : ins.a+count])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameReturn(result)

		default:
			return directFrameEnterGenericFrame()
		}
		frame.pc++
	}
	result := vmReturnedValues(nil)
	if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
		goto reload
	}
	return directFrameReturn(result)
}

func (thread *vmThread) runColdInstructionLoop(frame *vmFrame) (vmFrameResult, error) {
	proto := frame.proto
	globals := thread.globals
	varargs := frame.varargs
	runLineHook := thread.debugHook != nil && thread.debugLineHook
	runCountHook := thread.debugHook != nil && thread.debugCountInterval > 0
	runInstructionBudget := thread.instructionBudget >= 0

	code := frame.proto.packedCode
	for frame.pc < len(code) {
		coldInstructionFirstInstruction := thread.coldInstructionFrame == frame && !thread.coldInstructionRan
		if thread.coldInstructionFrame == frame && thread.coldInstructionRan {
			return vmFrameResult{}, errColdInstructionResume
		}
		if coldInstructionFirstInstruction {
			thread.coldInstructionRan = true
		}
		if runInstructionBudget && !coldInstructionFirstInstruction {
			if thread.instructionBudget == 0 {
				return vmFrameResult{state: vmCallStateHostInterrupt}, nil
			}
			thread.instructionBudget--
		}
		if runLineHook && !coldInstructionFirstInstruction {
			if err := thread.runDebugLineHook(frame); err != nil {
				return vmFrameResult{}, err
			}
		}
		if runCountHook && !coldInstructionFirstInstruction {
			if err := thread.runDebugCountHook(frame); err != nil {
				return vmFrameResult{}, err
			}
		}
		ins := code[frame.pc].unpack()

		switch ins.op {
		case opLoadConst:
			if true {
				frame.registers[ins.a] = proto.constants[ins.b]
				break
			}
			frame.setRegister(ins.a, proto.constants[ins.b])

		case opLoadGlobal:
			name, _ := proto.constants[ins.b].String()
			value, ok, hit := globals.getSlot(proto.globalSlot(ins.c, name), name)
			if hit {
				thread.directFramePICCounts.addGlobalSlotHit()
			} else {
				thread.directFramePICCounts.addGlobalSlotMiss()
			}
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: undefined global %q", name)
			}
			if true {
				frame.registers[ins.a] = value
				break
			}
			frame.setRegister(ins.a, value)

		case opSetGlobal:
			name, _ := proto.constants[ins.a].String()
			if true {
				globals.setSlot(proto.globalSlot(ins.c, name), name, frame.registers[ins.b])
				break
			}
			globals.setSlot(proto.globalSlot(ins.c, name), name, frame.register(ins.b))

		case opMove:
			if true {
				frame.registers[ins.a] = frame.registers[ins.b]
				break
			}
			frame.setRegister(ins.a, frame.register(ins.b))

		case opNewTable:
			if true {
				frame.registers[ins.a] = TableValue(newTableWithCapacity(ins.b, ins.c))
				break
			}
			frame.setRegister(ins.a, TableValue(newTableWithCapacity(ins.b, ins.c)))

		case opClosure:
			captured := captureUpvalues(proto.prototypes[ins.b], frame)
			value := functionValueWithCapturedUpvalues(proto.prototypes[ins.b], captured)
			if true {
				frame.registers[ins.a] = value
				break
			}
			frame.setRegister(ins.a, value)

		case opGetUpvalue:
			value, err := frame.upvalue(ins.b)
			if err != nil {
				return vmFrameResult{}, err
			}
			if true {
				frame.registers[ins.a] = value
				break
			}
			frame.setRegister(ins.a, value)

		case opSetUpvalue:
			var value Value
			if true {
				value = frame.registers[ins.b]
			} else {
				value = frame.register(ins.b)
			}
			if err := frame.setUpvalue(ins.a, value); err != nil {
				return vmFrameResult{}, err
			}

		case opVararg:
			resultCount := ins.b
			if resultCount == 0 {
				resultCount = 1
			}
			if resultCount < 0 {
				frame.openResultStart = ins.a
				frame.openResults = vmAdjustedBorrowedResultWindow(varargs)
				if true {
					frame.registers[ins.a] = frame.openResults.at(0)
				} else {
					frame.setRegister(ins.a, frame.openResults.at(0))
				}
				frame.pc++
				continue
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			if true {
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
			var tableValue Value
			var controlValue Value
			if true {
				tableValue = frame.registers[ins.c]
				controlValue = frame.registers[ins.a]
			} else {
				tableValue = frame.register(ins.c)
				controlValue = frame.register(ins.a)
			}
			if results, count, ok, err := inlineNativeIteratorNext(callee, tableValue, controlValue); ok {
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				if true {
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
			var tableValue Value
			var controlValue Value
			if true {
				tableValue = frame.registers[ins.c]
				controlValue = frame.registers[ins.a]
			} else {
				tableValue = frame.register(ins.c)
				controlValue = frame.register(ins.a)
			}
			if results, count, ok, err := inlineNativeIteratorNext(callee, tableValue, controlValue); ok {
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				if true {
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
			if true {
				base := frame.registers[ins.a]
				table := base.tableRef()
				if table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				if table.metatable == nil && proto.constantKeyOK[ins.b] {
					value := frame.registers[ins.c]
					keyValue := proto.constants[ins.b]
					var err error
					if valueKind(keyValue) == StringKind {
						table.setRawStringFieldBox(keyValue.stringText(), keyValue.stringBox(), value)
					} else {
						err = table.rawSetKey(proto.constantKeys[ins.b], value)
					}
					if err != nil {
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
					keyValue := proto.constants[ins.b]
					var err error
					if valueKind(keyValue) == StringKind {
						table.setRawStringFieldBox(keyValue.stringText(), keyValue.stringBox(), frame.register(ins.c))
					} else {
						err = table.rawSetKey(proto.constantKeys[ins.b], frame.register(ins.c))
					}
					if err != nil {
						return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
					}
					break
				}
			}
			if err := runtimeTableAccess(globals).set(table, proto.constants[ins.b], frame.register(ins.c)); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSetStringField:
			if true {
				base := frame.registers[ins.a]
				table := base.tableRef()
				if table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				value := frame.registers[ins.c]
				if table.metatable == nil {
					key := proto.constants[ins.b]
					table.setRawStringFieldBox(key.stringText(), key.stringBox(), value)
					break
				}
			}
			table, ok := frame.register(ins.a).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", frame.register(ins.a).Kind())
			}
			if table.metatable == nil {
				key := proto.constants[ins.b]
				table.setRawStringFieldBox(key.stringText(), key.stringBox(), frame.register(ins.c))
				break
			}
			if err := runtimeTableAccess(globals).set(table, proto.constants[ins.b], frame.register(ins.c)); err != nil {
				return vmFrameResult{}, fmt.Errorf("run: set field failed: %w", err)
			}

		case opSetStringFieldIndex:
			firstKey := proto.constantKeys[ins.b].str
			firstBox := proto.constants[ins.b].stringBox()
			if true {
				base := frame.registers[ins.a]
				table := base.tableRef()
				if table == nil {
					return vmFrameResult{}, fmt.Errorf("run: set field target is %s, want table", base.Kind())
				}
				first, firstOK := table.rawStringFieldBox(firstBox)
				if firstBox == nil {
					first, firstOK = table.rawStringField(firstKey)
				}
				if firstOK {
					nextTable := first.tableRef()
					if nextTable == nil {
						return vmFrameResult{}, fmt.Errorf("run: set index target is %s, want table", first.Kind())
					}
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

		case opGetStringField:
			key := proto.constantKeys[ins.c].str
			keyBox := proto.constants[ins.c].stringBox()
			if true {
				base := frame.registers[ins.b]
				table := base.tableRef()
				if table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				value, fieldOK := table.rawStringFieldBox(keyBox)
				if keyBox == nil {
					value, fieldOK = table.rawStringField(key)
				}
				if fieldOK {
					frame.registers[ins.a] = value
					break
				}
				if table.metatable == nil {
					frame.registers[ins.a] = NilValue()
					break
				}
			}
			table, ok := frame.register(ins.b).Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", frame.register(ins.b).Kind())
			}
			value, fieldOK := table.rawStringFieldBox(keyBox)
			if keyBox == nil {
				value, fieldOK = table.rawStringField(key)
			}
			if fieldOK {
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

		case opGetStringFieldIndex:
			firstKey := proto.constantKeys[ins.c].str
			firstBox := proto.constants[ins.c].stringBox()
			if true {
				base := frame.registers[ins.b]
				table := base.tableRef()
				if table == nil {
					return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
				}
				first, firstOK := table.rawStringFieldBox(firstBox)
				if firstBox == nil {
					first, firstOK = table.rawStringField(firstKey)
				}
				if firstOK {
					nextTable := first.tableRef()
					if nextTable == nil {
						return vmFrameResult{}, fmt.Errorf("run: get index target is %s, want table", first.Kind())
					}
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
			base := frame.register(ins.b)
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
			if true {
				left := frame.registers[ins.b]
				right := frame.registers[ins.c]
				if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
					frame.registers[ins.a] = NumberValue(valueNumber(left) + valueNumber(right))
					break
				}
			}
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)+valueNumber(right)))
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
			if true {
				left := frame.registers[ins.b]
				right := frame.registers[ins.c]
				if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
					frame.registers[ins.a] = NumberValue(valueNumber(left) - valueNumber(right))
					break
				}
			}
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)-valueNumber(right)))
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
			if true {
				left := frame.registers[ins.b]
				right := frame.registers[ins.c]
				if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
					frame.registers[ins.a] = NumberValue(valueNumber(left) * valueNumber(right))
					break
				}
			}
			left := frame.register(ins.b)
			right := frame.register(ins.c)
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)*valueNumber(right)))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)/valueNumber(right)))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)-math.Floor(valueNumber(left)/valueNumber(right))*valueNumber(right)))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(math.Floor(valueNumber(left)/valueNumber(right))))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(math.Pow(valueNumber(left), valueNumber(right))))
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
			if valueKind(operand) == NumberKind {
				frame.setRegister(ins.a, NumberValue(-valueNumber(operand)))
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

		case opConcatChain:
			operands := make([]Value, ins.c)
			for index := range operands {
				operands[index] = frame.register(ins.b + index)
			}
			value, err := concatChainValue(operands, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: concat failed: %w", err)
			}
			frame.setRegister(ins.a, value)

		case opAddK:
			if true {
				left := frame.registers[ins.b]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(valueNumber(left) + proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)+valueNumber(right)))
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
			if true {
				left := frame.registers[ins.b]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(valueNumber(left) - proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)-valueNumber(right)))
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
			if true {
				left := frame.registers[ins.b]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(valueNumber(left) * proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)*valueNumber(right)))
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
			if true {
				left := frame.registers[ins.b]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(valueNumber(left) / proto.constantNumbers[ins.c])
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)/valueNumber(right)))
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
			if true {
				left := frame.registers[ins.b]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.c] {
					right := proto.constantNumbers[ins.c]
					frame.registers[ins.a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/right)*right)
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(valueNumber(left)-math.Floor(valueNumber(left)/valueNumber(right))*valueNumber(right)))
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
			if true {
				left := frame.registers[ins.b]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.c] {
					frame.registers[ins.a] = NumberValue(math.Floor(valueNumber(left) / proto.constantNumbers[ins.c]))
					break
				}
			}
			left := frame.register(ins.b)
			right := proto.constants[ins.c]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				frame.setRegister(ins.a, NumberValue(math.Floor(valueNumber(left)/valueNumber(right))))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				frame.setRegister(ins.a, BoolValue(valueNumber(left) < valueNumber(right)))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				frame.setRegister(ins.a, BoolValue(valueNumber(left) <= valueNumber(right)))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				frame.setRegister(ins.a, BoolValue(valueNumber(left) > valueNumber(right)))
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
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				frame.setRegister(ins.a, BoolValue(valueNumber(left) >= valueNumber(right)))
				break
			}
			value, err := lessEqualValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater equal failed: %w", err)
			}
			frame.setRegister(ins.a, BoolValue(value))

		case opNumericForCheck:
			if true {
				loopValue := frame.registers[ins.a]
				limitValue := frame.registers[ins.b]
				stepValue := frame.registers[ins.c]
				if valueKind(loopValue) != NumberKind {
					return vmFrameResult{}, fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind())
				}
				if valueKind(limitValue) != NumberKind {
					return vmFrameResult{}, fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind())
				}
				if valueKind(stepValue) != NumberKind {
					return vmFrameResult{}, fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind())
				}
				if math.IsNaN(valueNumber(loopValue)) || math.IsNaN(valueNumber(limitValue)) || math.IsNaN(valueNumber(stepValue)) {
					return vmFrameResult{}, fmt.Errorf("run: numeric for operand is NaN")
				}
				if valueNumber(stepValue) > 0 {
					if valueNumber(loopValue) > valueNumber(limitValue) {
						frame.pc = ins.d
						continue
					}
					break
				}
				if valueNumber(loopValue) < valueNumber(limitValue) {
					frame.pc = ins.d
					continue
				}
				break
			}
			loopValue := frame.register(ins.a)
			limitValue := frame.register(ins.b)
			stepValue := frame.register(ins.c)
			if valueKind(loopValue) != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind())
			}
			if valueKind(limitValue) != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind())
			}
			if valueKind(stepValue) != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind())
			}
			if math.IsNaN(valueNumber(loopValue)) || math.IsNaN(valueNumber(limitValue)) || math.IsNaN(valueNumber(stepValue)) {
				return vmFrameResult{}, fmt.Errorf("run: numeric for operand is NaN")
			}
			if valueNumber(stepValue) > 0 {
				if valueNumber(loopValue) > valueNumber(limitValue) {
					frame.pc = ins.d
					continue
				}
				break
			}
			if valueNumber(loopValue) < valueNumber(limitValue) {
				frame.pc = ins.d
				continue
			}

		case opNumericForLoop:
			loopValue := frame.register(ins.a)
			stepValue := frame.register(ins.b)
			if valueKind(loopValue) != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind())
			}
			if valueKind(stepValue) != NumberKind {
				return vmFrameResult{}, fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind())
			}
			frame.setRegister(ins.a, NumberValue(valueNumber(loopValue)+valueNumber(stepValue)))
			frame.pc = ins.d
			continue

		case opJumpIfNotEqualK:
			if true {
				left := frame.registers[ins.a]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.b] {
					if valueNumber(left) != proto.constantNumbers[ins.b] {
						frame.pc = ins.d
						continue
					}
					break
				}
				right := proto.constants[ins.b]
				if valueKind(left) == StringKind && valueKind(right) == StringKind {
					if left.stringText() != right.stringText() {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := proto.constants[ins.b]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind {
				if valueNumber(left) != valueNumber(right) {
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
			if table := base.tableRef(); table != nil && table.metatable != nil {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLessK:
			if true {
				left := frame.registers[ins.a]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.b] {
					right := proto.constantNumbers[ins.b]
					if !math.IsNaN(valueNumber(left)) && !math.IsNaN(right) && valueNumber(left) >= right {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := proto.constants[ins.b]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) >= valueNumber(right) {
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

		case opJumpIfNotGreaterK:
			if true {
				left := frame.registers[ins.a]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.b] {
					right := proto.constantNumbers[ins.b]
					if !math.IsNaN(valueNumber(left)) && !math.IsNaN(right) && valueNumber(left) <= right {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := proto.constants[ins.b]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) <= valueNumber(right) {
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

		case opJumpIfLessK:
			if true {
				left := frame.registers[ins.a]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.b] {
					right := proto.constantNumbers[ins.b]
					if !math.IsNaN(valueNumber(left)) && !math.IsNaN(right) && valueNumber(left) < right {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := proto.constants[ins.b]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) < valueNumber(right) {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := lessValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: less failed: %w", err)
			}
			if value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfGreaterK:
			if true {
				left := frame.registers[ins.a]
				if valueKind(left) == NumberKind && proto.constantNumberOK[ins.b] {
					right := proto.constantNumbers[ins.b]
					if !math.IsNaN(valueNumber(left)) && !math.IsNaN(right) && valueNumber(left) > right {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := proto.constants[ins.b]
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) > valueNumber(right) {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := lessValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
			}
			if value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfNotLess:
			if true {
				left := frame.registers[ins.a]
				right := frame.registers[ins.b]
				if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
					if valueNumber(left) >= valueNumber(right) {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := frame.register(ins.b)
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) >= valueNumber(right) {
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
			if true {
				left := frame.registers[ins.a]
				right := frame.registers[ins.b]
				if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
					if valueNumber(left) <= valueNumber(right) {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := frame.register(ins.b)
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) <= valueNumber(right) {
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

		case opJumpIfLess:
			if true {
				left := frame.registers[ins.a]
				right := frame.registers[ins.b]
				if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
					if valueNumber(left) < valueNumber(right) {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := frame.register(ins.b)
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) < valueNumber(right) {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := lessValue(left, right, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: less failed: %w", err)
			}
			if value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfGreater:
			if true {
				left := frame.registers[ins.a]
				right := frame.registers[ins.b]
				if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
					if valueNumber(left) > valueNumber(right) {
						frame.pc = ins.d
						continue
					}
					break
				}
			}
			left := frame.register(ins.a)
			right := frame.register(ins.b)
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind && !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if valueNumber(left) > valueNumber(right) {
					frame.pc = ins.d
					continue
				}
				break
			}
			value, err := lessValue(right, left, globals)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: greater failed: %w", err)
			}
			if value {
				frame.pc = ins.d
				continue
			}

		case opJumpIfModKNotEqualK:
			var left Value
			if true {
				left = frame.registers[ins.a]
			} else {
				left = frame.register(ins.a)
			}
			modRight := proto.constants[ins.b]
			want := proto.constants[ins.c]
			if valueKind(left) == NumberKind && valueKind(modRight) == NumberKind && valueKind(want) == NumberKind {
				got := valueNumber(left) - math.Floor(valueNumber(left)/valueNumber(modRight))*valueNumber(modRight)
				if got != valueNumber(want) {
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
			keyBox := proto.constants[ins.b].stringBox()
			var base Value
			if true {
				base = frame.registers[ins.a]
			} else {
				base = frame.register(ins.a)
			}
			table := base.tableRef()
			if table == nil {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			var left Value
			fieldValue, fieldOK := table.rawStringFieldBox(keyBox)
			if keyBox == nil {
				fieldValue, fieldOK = table.rawStringField(key)
			}
			if fieldOK {
				left = fieldValue
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
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				if left.stringText() != right.stringText() {
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
			keyBox := proto.constants[ins.b].stringBox()
			var base Value
			if true {
				base = frame.registers[ins.a]
			} else {
				base = frame.register(ins.a)
			}
			table := base.tableRef()
			if table == nil {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			var left Value
			fieldValue, fieldOK := table.rawStringFieldBox(keyBox)
			if keyBox == nil {
				fieldValue, fieldOK = table.rawStringField(key)
			}
			if fieldOK {
				left = fieldValue
			} else if table.metatable == nil {
				left = NilValue()
			} else {
				value, err := runtimeTableAccess(globals).get(table, proto.constants[ins.b])
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: get field failed: %w", err)
				}
				left = value
			}
			if valueKind(left) == NumberKind && proto.constantNumberOK[ins.c] {
				right := proto.constantNumbers[ins.c]
				if !math.IsNaN(valueNumber(left)) && !math.IsNaN(right) {
					greater := valueNumber(left) > right
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

		case opJumpIfStringFieldNotGreaterR:
			key := proto.constantKeys[ins.b].str
			keyBox := proto.constants[ins.b].stringBox()
			var base Value
			if true {
				base = frame.registers[ins.a]
			} else {
				base = frame.register(ins.a)
			}
			table := base.tableRef()
			if table == nil {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", base.Kind())
			}
			var left Value
			fieldValue, fieldOK := table.rawStringFieldBox(keyBox)
			if keyBox == nil {
				fieldValue, fieldOK = table.rawStringField(key)
			}
			if fieldOK {
				left = fieldValue
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
			if true {
				right = frame.registers[ins.c]
			} else {
				right = frame.register(ins.c)
			}
			if valueKind(left) == NumberKind && valueKind(right) == NumberKind &&
				!math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				if !(valueNumber(left) > valueNumber(right)) {
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

		case opFastCall:
			if result, done, err := thread.runColdFastCall(frame, nativeFuncID(ins.b), ins.a, ins.c, ins.d); done || err != nil {
				return result, err
			}

		case opCall:
			var callee Value
			if true {
				callee = frame.registers[ins.b]
			} else {
				callee = frame.register(ins.b)
			}
			destination := vmResultDestination{register: ins.a, count: ins.d}
			resultCount := destination.count
			if resultCount == 0 {
				resultCount = 1
			}
			if resultCount == 1 && ins.c >= 0 && valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if ins.c > 0 {
					value = frame.register(ins.b + 1)
				}
				result, err := baseToStringValue(globals, value)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.applyInlineResultDestination(destination, [2]Value{result}, 1)
				break
			}
			if ins.c >= 0 {
				done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, ins.b+1, ins.c, destination)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
				}
				if done {
					break
				}
			}
			var args []Value
			if ins.c < 0 {
				prefixCount := -ins.c - 1
				if frame.openResultStart == ins.b+1+prefixCount {
					if _, ok := callee.scriptFunction(); ok && prefixCount == 0 && globals != nil && globals.thread != nil {
						args = frame.openResults.borrowedValues()
					} else {
						args = make([]Value, 0, prefixCount+frame.openResults.len())
						for register := ins.b + 1; register <= ins.b+prefixCount; register++ {
							if true {
								args = append(args, frame.registers[register])
							} else {
								args = append(args, frame.register(register))
							}
						}
						args = frame.openResults.appendTo(args)
					}
				} else {
					args = frame.retainedFixedCallArgs(ins.b+1, prefixCount).values
				}
			} else if _, ok := callee.scriptFunction(); ok && globals != nil && globals.thread != nil {
				args = frame.borrowedFixedCallArgs(ins.b+1, ins.c).values
			} else {
				args = frame.retainedFixedCallArgs(ins.b+1, ins.c).values
			}
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

		case opCallOne:
			var callee Value
			if true {
				callee = frame.registers[ins.b]
			} else {
				callee = frame.register(ins.b)
			}
			destination := vmResultDestination{register: ins.a, count: 1}
			argCount, _ := decodeFixedCallCount(ins.c)
			if valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if argCount > 0 {
					value = frame.register(ins.b + 1)
				}
				result, err := baseToStringValue(globals, value)
				if err != nil {
					return vmFrameResult{}, fmt.Errorf("run: call failed: host function failed: %w", err)
				}
				frame.applyInlineResultDestination(destination, [2]Value{result}, 1)
				break
			}
			done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, ins.b+1, argCount, destination)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			if done {
				break
			}
			var args []Value
			if _, ok := callee.scriptFunction(); ok && globals != nil && globals.thread != nil {
				args = frame.borrowedFixedCallArgs(ins.b+1, argCount).values
			} else {
				args = frame.retainedFixedCallArgs(ins.b+1, argCount).values
			}
			if result, done, err := frame.callValueToDestination(callee, globals, args, destination); done || err != nil {
				return result, err
			}

		case opCallLocalOne:
			callee := frame.register(ins.b)
			destination := vmResultDestination{register: ins.a, count: 1}
			argCount, _ := decodeFixedCallCount(ins.d)
			if closure, ok := callee.scriptFunction(); ok {
				var args []Value
				if true {
					args = frame.registers[ins.c : ins.c+argCount]
				} else {
					args = frame.scriptCallArgs(ins.c, argCount)
				}
				frame.pc++
				if thread.debugHook != nil {
					result, err := thread.runInlineScriptCall(closure, args)
					if err != nil {
						if yield, ok := err.(vmYieldRequest); ok {
							thread.installPendingCall(frame, vmPendingCall{
								destination: destination,
								protected:   yield.protected,
								host:        yield.host,
							})
						}
						return vmFrameResult{}, err
					}
					frame.applySingleFrameResult(ins.a, result)
					continue
				}
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						thread.installPendingCall(frame, vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						})
					}
					return vmFrameResult{}, err
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				if true {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}

			done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, ins.c, argCount, destination)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			if done {
				break
			}
			args := frame.retainedFixedCallArgs(ins.c, argCount).values
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					})
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
			callee, err := frame.upvalue(ins.b)
			if err != nil {
				return vmFrameResult{}, err
			}
			destination := vmResultDestination{register: ins.a, count: 1}
			argCount, _ := decodeFixedCallCount(ins.d)
			if closure, ok := callee.scriptFunction(); ok {
				var args []Value
				if true {
					args = frame.registers[ins.c : ins.c+argCount]
				} else {
					args = frame.scriptCallArgs(ins.c, argCount)
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						thread.installPendingCall(frame, vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						})
					}
					return vmFrameResult{}, err
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				if true {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}

			done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, ins.c, argCount, destination)
			if err != nil {
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			if done {
				break
			}
			args := frame.retainedFixedCallArgs(ins.c, argCount).values
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					})
					frame.pc++
					return vmYieldedValues(yield.values), nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.applyResultDestination(destination, results)

		case opCallMethodOne:
			var receiver Value
			if true {
				receiver = frame.registers[ins.b]
			} else {
				receiver = frame.register(ins.b)
			}
			table, ok := receiver.Table()
			if !ok {
				return vmFrameResult{}, fmt.Errorf("run: get field target is %s, want table", receiver.Kind())
			}
			key := proto.constantKeys[ins.c].str
			keyBox := proto.constants[ins.c].stringBox()
			var callee Value
			value, fieldOK := table.rawStringFieldBox(keyBox)
			if keyBox == nil {
				value, fieldOK = table.rawStringField(key)
			}
			if fieldOK {
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
			if true {
				frame.registers[ins.a+1] = receiver
			} else {
				frame.setRegister(ins.a+1, receiver)
			}
			explicitCount, _ := decodeFixedCallCount(ins.d)
			argCount := explicitCount + 1
			args := frame.scriptCallArgs(ins.a+1, argCount)
			destination := vmResultDestination{register: ins.a, count: 1}
			if closure, ok := callee.scriptFunction(); ok {
				if true {
					args = frame.registers[ins.a+1 : ins.a+1+argCount]
				}
				frame.pc++
				value, err := thread.runInlineScriptCallOneNoHook(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						thread.installPendingCall(frame, vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						})
					}
					return vmFrameResult{}, err
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				if true {
					frame.registers[ins.a] = value
				} else {
					frame.setRegister(ins.a, value)
				}
				continue
			}
			results, err := callValue(callee, globals, args)
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: destination,
						protected:   yield.protected,
						host:        yield.host,
					})
					frame.pc++
					return vmYieldedValues(yield.values), nil
				}
				if isVMHostInterrupt(err) {
					return vmFrameResult{}, err
				}
				return vmFrameResult{}, fmt.Errorf("run: call failed: %w", err)
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			if len(results) == 0 {
				if true {
					frame.registers[ins.a] = NilValue()
				} else {
					frame.setRegister(ins.a, NilValue())
				}
				break
			}
			if true {
				frame.registers[ins.a] = results[0]
			} else {
				frame.setRegister(ins.a, results[0])
			}

		case opJumpIfFalse:
			var condition Value
			if true {
				condition = frame.registers[ins.a]
			} else {
				condition = frame.register(ins.a)
			}
			if !condition.truthy() {
				frame.pc = ins.b
				continue
			}

		case opJump:
			frame.pc = ins.b
			continue

		case opReturnOne:
			if true {
				return vmReturnedValue(frame.registers[ins.a]), nil
			}
			return vmReturnedValue(frame.register(ins.a)), nil

		case opReturn:
			count := ins.b
			if count < 0 {
				prefixCount := -count - 1
				if frame.openResultStart == ins.a+prefixCount {
					return vmReturnedPrefixAndWindow(frame.registers[ins.a:ins.a+prefixCount], frame.openResults), nil
				}
				return vmReturnedValue(frame.register(ins.a)), nil
			}
			if count == 0 {
				return vmReturnedValues(nil), nil
			}
			if count == 1 {
				if true {
					return vmReturnedValue(frame.registers[ins.a]), nil
				}
				return vmReturnedValue(frame.register(ins.a)), nil
			}
			if true {
				return vmReturnedBorrowedValues(frame.registers[ins.a : ins.a+count]), nil
			}
			results := make([]Value, count)
			for i := range results {
				results[i] = frame.register(ins.a + i)
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

func prepareIterator(value Value, globals *globalEnv) (Value, Value, Value, bool, error) {
	table, ok := value.Table()
	if !ok {
		return NilValue(), NilValue(), NilValue(), false, nil
	}

	if table.metatable != nil {
		metamethod, err := table.metatable.rawGetString("__iter")
		if err != nil {
			return NilValue(), NilValue(), NilValue(), false, err
		}
		if !metamethod.IsNil() {
			results, err := callRuntimeMetamethodWindow1(metamethod, globals, value)
			if err != nil {
				return NilValue(), NilValue(), NilValue(), false, err
			}
			return results.at(0), results.at(1), results.at(2), true, nil
		}
	}

	if table.metatable == nil {
		if tableCanIterateCleanArray(table) {
			return valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext), TableValue(table), NilValue(), true, nil
		}
		return valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncTableNext), TableValue(table), NilValue(), true, nil
	}
	return nativeFuncValueWithID(baseNextNative, nativeFuncNext), TableValue(table), NilValue(), true, nil
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

func baseTableNextNative(_ *globalEnv, args []Value) ([]Value, error) {
	table, err := tableArg("table iterator", args, 0)
	if err != nil {
		return nil, err
	}
	key := NilValue()
	if len(args) > 1 {
		key = args[1]
	}
	nextKey, value, err := table.rawNext(key)
	if err != nil {
		return nil, fmt.Errorf("table iterator: %w", err)
	}
	if nextKey.IsNil() {
		return []Value{NilValue()}, nil
	}
	return []Value{nextKey, value}, nil
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

func baseTableNextInline(tableValue Value, controlValue Value) ([2]Value, int, error) {
	table, ok := tableValue.Table()
	if !ok {
		return [2]Value{}, 0, fmt.Errorf("table iterator: argument #1 is %s, want table", tableValue.Kind())
	}
	nextKey, value, err := table.rawNext(controlValue)
	if err != nil {
		return [2]Value{}, 0, fmt.Errorf("table iterator: %w", err)
	}
	if nextKey.IsNil() {
		return [2]Value{NilValue()}, 1, nil
	}
	return [2]Value{nextKey, value}, 2, nil
}

func inlineNativeIteratorNext(callee Value, tableValue Value, controlValue Value) ([2]Value, int, bool, error) {
	switch valueNativeID(callee) {
	case nativeFuncArrayNext:
		results, count, err := baseArrayNextInline(tableValue, controlValue)
		return results, count, true, err
	case nativeFuncNext, nativeFuncTableNext:
		results, count, err := baseTableNextInline(tableValue, controlValue)
		return results, count, true, err
	default:
		return [2]Value{}, 0, false, nil
	}
}

func directFrameArrayIteratorNext(tableValue Value, controlValue Value) (Value, Value, int, error) {
	table := tableValue.tableRef()
	if table == nil {
		return NilValue(), NilValue(), 0, fmt.Errorf("array iterator: argument #1 is %s, want table", tableValue.Kind())
	}
	index := 0
	if valueKind(controlValue) != NilKind {
		if valueKind(controlValue) != NumberKind {
			return NilValue(), NilValue(), 0, fmt.Errorf("array iterator: index is %s, want number or nil", controlValue.Kind())
		}
		index = int(valueNumber(controlValue))
		if float64(index) != valueNumber(controlValue) {
			return NilValue(), NilValue(), 0, fmt.Errorf("array iterator: index is %s, want integer", controlValue.Kind())
		}
	}
	next := index + 1
	if next < 1 || next > len(table.array) {
		return NilValue(), NilValue(), 1, nil
	}
	return NumberValue(float64(next)), table.array[next-1], 2, nil
}

func directFrameIteratorNext(callee Value, tableValue Value, controlValue Value) (Value, Value, int, bool, error) {
	switch valueNativeID(callee) {
	case nativeFuncArrayNext:
		first, second, count, err := directFrameArrayIteratorNext(tableValue, controlValue)
		return first, second, count, true, err
	case nativeFuncNext, nativeFuncTableNext:
		table := tableValue.tableRef()
		if table == nil {
			return NilValue(), NilValue(), 0, true, fmt.Errorf("table iterator: argument #1 is %s, want table", tableValue.Kind())
		}
		nextKey, value, err := table.rawNext(controlValue)
		if err != nil {
			return NilValue(), NilValue(), 0, true, fmt.Errorf("table iterator: %w", err)
		}
		if nextKey.IsNil() {
			return NilValue(), NilValue(), 1, true, nil
		}
		return nextKey, value, 2, true, nil
	default:
		return NilValue(), NilValue(), 0, false, nil
	}
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
		metamethod, err := table.metatable.rawGetString("__len")
		if err != nil {
			return NilValue(), err
		}
		if !metamethod.IsNil() {
			results, err := callRuntimeMetamethodWindow1(metamethod, globals, value)
			if err != nil {
				return NilValue(), err
			}
			result := results.at(0)
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
	leftNumber, leftOK := numericOperandValue(left)
	rightNumber, rightOK := numericOperandValue(right)
	if leftOK && rightOK {
		return NumberValue(primitive(leftNumber, rightNumber)), nil
	}
	if value, ok, err := callBinaryMetamethod(metafield, left, right, globals); ok || err != nil {
		return value, err
	}
	_, leftErr := numericOperand(left, "left", operator)
	if leftErr != nil {
		return NilValue(), leftErr
	}
	_, rightErr := numericOperand(right, "right", operator)
	return NilValue(), rightErr
}

func concatValue(left Value, right Value, globals *globalEnv) (Value, error) {
	text, err := valuesConcat(left, right)
	if err == nil {
		return stringValueInGlobalEnv(globals, text), nil
	}
	if value, ok, metamethodErr := callBinaryMetamethod("__concat", left, right, globals); ok || metamethodErr != nil {
		return value, metamethodErr
	}
	return NilValue(), err
}

func concatChainValue(operands []Value, globals *globalEnv) (Value, error) {
	text, ok, err := activeThread(globals).concatRawChainString(operands)
	if err != nil {
		return NilValue(), err
	}
	if ok {
		return stringValueInGlobalEnv(globals, text), nil
	}
	if len(operands) == 0 {
		return stringValueInGlobalEnv(globals, ""), nil
	}
	result := operands[0]
	for _, operand := range operands[1:] {
		value, err := concatValue(result, operand, globals)
		if err != nil {
			return NilValue(), err
		}
		result = value
	}
	return result, nil
}

func lessValue(left Value, right Value, globals *globalEnv) (bool, error) {
	if valueKind(left) == valueKind(right) {
		switch valueKind(left) {
		case NumberKind:
			if !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				return valueNumber(left) < valueNumber(right), nil
			}
		case StringKind:
			return left.stringText() < right.stringText(), nil
		}
	}
	if result, ok, metamethodErr := callComparisonMetamethod("__lt", left, right, globals); ok || metamethodErr != nil {
		return result, metamethodErr
	}
	return valuesLess(left, right)
}

func lessEqualValue(left Value, right Value, globals *globalEnv) (bool, error) {
	if valuesEqual(left, right) {
		return true, nil
	}
	if valueKind(left) == valueKind(right) {
		switch valueKind(left) {
		case NumberKind:
			if !math.IsNaN(valueNumber(left)) && !math.IsNaN(valueNumber(right)) {
				return valueNumber(left) < valueNumber(right), nil
			}
		case StringKind:
			return left.stringText() < right.stringText(), nil
		}
	}
	if result, ok, metamethodErr := callComparisonMetamethod("__le", left, right, globals); ok || metamethodErr != nil {
		return result, metamethodErr
	}
	return valuesLessEqual(left, right)
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
	results, err := callRuntimeMetamethodWindow1(metamethod, globals, value)
	if err != nil {
		return NilValue(), true, err
	}
	return results.at(0), true, nil
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
	results, err := callRuntimeMetamethodWindow2(metamethod, globals, left, right)
	if err != nil {
		return NilValue(), true, err
	}
	return results.at(0), true, nil
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
	metamethod, err := table.metatable.rawGetString(name)
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
			upvalueValues:   closure.upvalueValues,
			upvalueValueOK:  closure.upvalueValueOK,
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

func callRuntimeMetamethodWindow(fn Value, globals *globalEnv, args []Value) (vmResultWindow, error) {
	if globals != nil && globals.thread != nil {
		if closure, ok := fn.scriptFunction(); ok {
			restore := globals.thread.enterNonYieldable()
			result, err := globals.thread.runInlineScriptCall(closure, args)
			restore()
			if err != nil {
				return vmResultWindow{}, err
			}
			return result.window, nil
		}
	}
	results, err := callRuntimeMetamethod(fn, globals, args)
	if err != nil {
		return vmResultWindow{}, err
	}
	return vmOwnedResultWindow(results), nil
}

func callRuntimeMetamethodWindow1(fn Value, globals *globalEnv, first Value) (vmResultWindow, error) {
	if globals != nil && globals.thread != nil {
		if closure, ok := fn.scriptFunction(); ok {
			restore := globals.thread.enterNonYieldable()
			result, err := globals.thread.runInlineScriptCallFixed(closure, first, NilValue(), NilValue(), 1)
			restore()
			if err != nil {
				return vmResultWindow{}, err
			}
			return result.window, nil
		}
	}
	args := [1]Value{first}
	return callRuntimeMetamethodWindow(fn, globals, args[:])
}

func callRuntimeMetamethodWindow2(fn Value, globals *globalEnv, first Value, second Value) (vmResultWindow, error) {
	if globals != nil && globals.thread != nil {
		if closure, ok := fn.scriptFunction(); ok {
			restore := globals.thread.enterNonYieldable()
			result, err := globals.thread.runInlineScriptCallFixed(closure, first, second, NilValue(), 2)
			restore()
			if err != nil {
				return vmResultWindow{}, err
			}
			return result.window, nil
		}
	}
	args := [2]Value{first, second}
	return callRuntimeMetamethodWindow(fn, globals, args[:])
}

func callRuntimeMetamethodWindow3(fn Value, globals *globalEnv, first Value, second Value, third Value) (vmResultWindow, error) {
	if globals != nil && globals.thread != nil {
		if closure, ok := fn.scriptFunction(); ok {
			restore := globals.thread.enterNonYieldable()
			result, err := globals.thread.runInlineScriptCallFixed(closure, first, second, third, 3)
			restore()
			if err != nil {
				return vmResultWindow{}, err
			}
			return result.window, nil
		}
	}
	args := [3]Value{first, second, third}
	return callRuntimeMetamethodWindow(fn, globals, args[:])
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

func rawLenIntrinsicCallee(globals *globalEnv) (Value, bool, error) {
	const globalName = "rawlen"
	key := baseFieldIntrinsicGuardKey{globalName: globalName}
	thread := activeThread(globals)
	if guard, ok := thread.baseFieldIntrinsicGuard(key, globals); ok {
		return guard.callee, true, nil
	}
	callee := valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncRawLen)
	if globals == nil {
		return callee, true, nil
	}
	if value, ok := globals.overrideValue(globalName); ok {
		fast := valueNativeID(value) == nativeFuncRawLen
		if fast {
			thread.storeBaseFieldIntrinsicGuard(key, globals, nil, value)
		} else {
			thread.clearBaseFieldIntrinsicGuard(key)
		}
		return value, fast, nil
	}
	thread.storeBaseFieldIntrinsicGuard(key, globals, nil, callee)
	return callee, true, nil
}

func selectIntrinsicCallee(globals *globalEnv) (Value, bool, error) {
	const globalName = "select"
	key := baseFieldIntrinsicGuardKey{globalName: globalName}
	thread := activeThread(globals)
	if guard, ok := thread.baseFieldIntrinsicGuard(key, globals); ok {
		return guard.callee, true, nil
	}
	callee := valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncSelect)
	if globals == nil {
		return callee, true, nil
	}
	if value, ok := globals.overrideValue(globalName); ok {
		fast := valueNativeID(value) == nativeFuncSelect
		if fast {
			thread.storeBaseFieldIntrinsicGuard(key, globals, nil, value)
		} else {
			thread.clearBaseFieldIntrinsicGuard(key)
		}
		return value, fast, nil
	}
	thread.storeBaseFieldIntrinsicGuard(key, globals, nil, callee)
	return callee, true, nil
}

func rawLenGlobalUnchanged(globals *globalEnv) bool {
	return globals == nil || globals.nativeGlobalUnchanged("rawlen", nativeFuncRawLen)
}

func baseFieldIntrinsicUnchangedWithValues(globals *globalEnv, globalName string, field string, nativeID nativeFuncID) bool {
	tableValue, ok := globals.overrideValue(globalName)
	if !ok {
		return true
	}
	table := tableValue.tableRef()
	if table == nil || table.metatable != nil {
		return false
	}
	callee, ok := table.rawStringField(field)
	return ok && valueNativeID(callee) == nativeID
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
	tableValue, ok := globals.overrideValue(globalName)
	if !ok {
		callee := valueWithRefAndNativeID(HostFuncKind, nil, intrinsic.nativeID)
		thread.storeBaseFieldIntrinsicGuard(key, globals, nil, callee)
		return callee, true, nil
	}
	table, ok := tableValue.Table()
	if !ok {
		thread.clearBaseFieldIntrinsicGuard(key)
		return NilValue(), false, fmt.Errorf("run: get field target is %s, want table", tableValue.Kind())
	}
	if callee, ok := table.rawStringField(field); ok {
		fast := valueNativeID(callee) == intrinsic.nativeID
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
	return callee, valueNativeID(callee) == intrinsic.nativeID, nil
}

func (thread *vmThread) baseFieldIntrinsicGuard(key baseFieldIntrinsicGuardKey, globals *globalEnv) (baseFieldIntrinsicGuardEntry, bool) {
	var counts *directFramePICCounts
	if thread != nil && thread.directFrameInstrumented {
		counts = thread.directFramePICCounts
	}
	counts.addIntrinsicGuardCheck()
	if thread == nil || globals == nil || thread.intrinsicGuards == nil {
		counts.addIntrinsicGuardMiss()
		return baseFieldIntrinsicGuardEntry{}, false
	}
	cache := thread.intrinsicGuards
	for i := 0; i < int(cache.count); i++ {
		entry := cache.entries[i]
		if entry.key != key {
			continue
		}
		if entry.envVersion != globals.version {
			counts.addIntrinsicGuardMiss()
			return baseFieldIntrinsicGuardEntry{}, false
		}
		if entry.table != nil && !entry.token.matchesTableValues(entry.table) {
			counts.addIntrinsicGuardMiss()
			return baseFieldIntrinsicGuardEntry{}, false
		}
		cache.hits++
		counts.addIntrinsicGuardHit()
		return entry, true
	}
	counts.addIntrinsicGuardMiss()
	return baseFieldIntrinsicGuardEntry{}, false
}

func (thread *vmThread) storeBaseFieldIntrinsicGuard(key baseFieldIntrinsicGuardKey, globals *globalEnv, table *Table, callee Value) {
	if globals == nil || !thread.intrinsicGuardCacheEnabled() {
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
		return finishHostCallResult(host(globals, ownedHostArgs(args)))
	}

	if host, ok := fn.hostFunction(); ok {
		if host == nil {
			return nil, fmt.Errorf("call target is nil host_function")
		}
		results, err := host(ownedHostArgs(args))
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
				return globals.thread.runScriptProtectedWithUpvalues(closure.proto, args, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK)
			}
			return globals.thread.runScriptWithUpvalues(closure.proto, args, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK)
		}
		return executeProto(context.Background(), closure.proto, globals, executeOptions{
			args:            args,
			upvalues:        closure.upvalues,
			upvalueValues:   closure.upvalueValues,
			upvalueValueOK:  closure.upvalueValueOK,
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
		metamethod, err := table.metatable.rawGetString("__call")
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
	metamethod, err := table.metatable.rawGetString("__call")
	if err != nil {
		return false, err
	}
	return !metamethod.IsNil(), nil
}

func captureUpvalues(proto *Proto, frame *vmFrame) capturedUpvalueSet {
	if len(proto.upvalues) == 0 {
		return capturedUpvalueSet{}
	}

	captured := capturedUpvalueSet{count: len(proto.upvalues)}
	if len(proto.upvalues) > len(captured.cells) {
		captured.cellSpill = make([]*cell, len(proto.upvalues))
		captured.valueSpill = make([]Value, len(proto.upvalues))
		captured.valueOKSpill = make([]bool, len(proto.upvalues))
	}
	for i, desc := range proto.upvalues {
		if desc.local {
			if desc.copy {
				captured.setValue(i, frame.register(desc.index))
				continue
			}
			captured.setCell(i, frame.registerCell(desc.index))
			continue
		}

		if desc.index < len(frame.upvalueValueOK) && frame.upvalueValueOK[desc.index] {
			captured.setValue(i, frame.upvalueValues[desc.index])
			continue
		}
		captured.setCell(i, frame.upvalues[desc.index])
	}
	return captured
}

func (set *capturedUpvalueSet) setCell(index int, cell *cell) {
	if set.count <= len(set.cells) {
		set.cells[index] = cell
		return
	}
	set.cellSpill[index] = cell
}

func (set *capturedUpvalueSet) setValue(index int, value Value) {
	if set.count <= len(set.values) {
		set.values[index] = value
		set.valueOK[index] = true
		return
	}
	set.valueSpill[index] = value
	set.valueOKSpill[index] = true
}

func anyBool(values []bool) bool {
	for _, value := range values {
		if value {
			return true
		}
	}
	return false
}
