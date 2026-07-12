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
	// The canonical scalar runner has no owner, globals, upvalues, or
	// suspension state. Fixed arguments are imported into its per-run slot
	// state; everything else stays on the established VM until the
	// corresponding slot ABI slice is proven.
	if globals == nil && len(options.upvalues) == 0 &&
		len(options.upvalueValues) == 0 && len(options.upvalueValueOK) == 0 &&
		options.maxInstructions < 0 {
		if values, handled, err := runSlotExecution(proto, options.args); handled || err != nil {
			return values, err
		}
	}
	if globals != nil && globals.owner != nil {
		return executeProtoWithOwner(ctx, proto, globals, options)
	}
	thread := acquireVMThread(ctx, globals)
	defer releaseVMThread(thread)
	thread.instructionBudget = options.maxInstructions
	return thread.runWithUpvalues(proto, options.args, options.upvalues, options.upvalueValues, options.upvalueValueOK)
}

func executeProtoWithOwner(ctx context.Context, proto *Proto, globals *globalEnv, options executeOptions) ([]Value, error) {
	// A pure prototype does not need the VM thread or global environment. Route
	// it through the compact slot runner when there is no budget/upvalue state
	// and the context cannot be cancelled. The owner activity counter keeps
	// close from racing this VM-thread-free path; the slot runner itself keeps
	// using its pooled ephemeral heap until collector/root integration lands.
	if proto != nil && proto.slotExecutionEligible && globals.thread == nil && options.maxInstructions < 0 &&
		len(options.upvalues) == 0 && len(options.upvalueValues) == 0 &&
		len(options.upvalueValueOK) == 0 &&
		(ctx == nil || (ctx.Done() == nil && ctx.Err() == nil)) {
		values, handled, err := runOwnerSlotExecution(globals.owner, proto, options.args)
		if handled || err != nil {
			return values, err
		}
	}
	thread := acquireVMThread(ctx, globals)
	if err := thread.bindOwner(globals.owner); err != nil {
		thread.owner = nil
		thread.resetForPool()
		vmThreadPool.Put(thread)
		return nil, err
	}
	defer releaseVMThread(thread)
	thread.instructionBudget = options.maxInstructions
	return thread.runWithUpvalues(proto, options.args, options.upvalues, options.upvalueValues, options.upvalueValueOK)
}

func runOwnerSlotExecution(owner *runtimeOwner, proto *Proto, args []Value) (values []Value, handled bool, err error) {
	if err := owner.beginSlotRun(); err != nil {
		return nil, false, err
	}
	defer owner.endSlotRun()
	if owner.heap == nil {
		return nil, false, errRuntimeOwnerReleased
	}
	return runSlotExecutionWithHeap(proto, args, owner.heap)
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
	owner       *runtimeOwner
	ownerBound  bool
	frames      []*vmFrame
	frameSlots  []*vmFrame
	// frameRecords is the compact continuation stack used by the borrowed
	// fixed-result call bridge. The vmFrame remains the execution bridge until
	// the rest of the call ABI moves onto records.
	frameRecords []vmFrameRecord
	// rootClosureSlots keep stable synthetic identities for physical frame
	// roots without widening vmFrameRecord.
	rootClosureSlots []*closure
	maxFrameRecords  int
	stackOwner       *vmStackOwner
	// openUpvalues is the thread-owned index of cells that still point into a
	// stack owner. Entries are kept in insertion order and keyed by the owner
	// identity plus the cell's absolute register index.
	openUpvalues []*cell
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
	stringIntern            map[string]*stringBox
	stringConcatIntern      map[stringConcatKey]*stringBox
	stringScratch           []byte
	functionInstances       map[*Proto]*vmFunctionInstance
	functionInstanceSites   int
}

type vmCacheBundle struct {
	stringIntern          map[string]*stringBox
	stringConcatIntern    map[stringConcatKey]*stringBox
	functionInstances     map[*Proto]*vmFunctionInstance
	functionInstanceSites int
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

// vmFrameRecord is the state that survives a dispatch-loop frame switch. Its
// fixed-width indices keep the record dense and leave the current frame's
// decoded slices and loop counters in locals rather than in the call stack.
// resultCount uses max uint32 for an open result range.
type vmFrameRecord struct {
	closure           *closure
	returnPC          uint32
	base              uint32
	top               uint32
	resultDestination uint32
	resultCount       uint32
	argumentBase      uint32
	varargBase        uint32
	protectedDepth    uint16
	frameDepth        uint16
	argumentCount     uint16
	varargCount       uint16
	flags             vmFrameRecordFlags
}

type vmFrameRecordFlags uint16

const (
	vmFrameRecordFlagProtected vmFrameRecordFlags = 1 << iota
	vmFrameRecordFlagOpenResults
	vmFrameRecordFlagRecordOnly
	vmFrameRecordFlagCallerBorrowed
	vmFrameRecordFlagOpenArguments
)

func (thread *vmThread) pushFrameRecord(record vmFrameRecord) {
	if thread == nil {
		return
	}
	thread.frameRecords = append(thread.frameRecords, record)
	if len(thread.frameRecords) > thread.maxFrameRecords {
		thread.maxFrameRecords = len(thread.frameRecords)
	}
}

func (thread *vmThread) popFrameRecord() (vmFrameRecord, bool) {
	if thread == nil || len(thread.frameRecords) == 0 {
		return vmFrameRecord{}, false
	}
	last := len(thread.frameRecords) - 1
	record := thread.frameRecords[last]
	thread.frameRecords[last] = vmFrameRecord{}
	thread.frameRecords = thread.frameRecords[:last]
	return record, true
}

func (thread *vmThread) peekFrameRecord(frame *vmFrame) (vmFrameRecord, bool) {
	if thread == nil || frame == nil || len(thread.frameRecords) == 0 || frame.registerBase < 0 {
		return vmFrameRecord{}, false
	}
	record := thread.frameRecords[len(thread.frameRecords)-1]
	if record.flags&vmFrameRecordFlagRecordOnly != 0 {
		return vmFrameRecord{}, false
	}
	if record.closure == nil || record.closure != frame.currentClosure || record.closure.proto != frame.proto || uint64(frame.registerBase) != uint64(record.base) ||
		frame.depth < 0 || uint64(frame.depth) != uint64(record.frameDepth) {
		return vmFrameRecord{}, false
	}
	return record, true
}

func (thread *vmThread) popFrameRecordFor(frame *vmFrame) (vmFrameRecord, bool) {
	record, ok := thread.peekFrameRecord(frame)
	if !ok {
		return vmFrameRecord{}, false
	}
	_, _ = thread.popFrameRecord()
	return record, true
}

func (thread *vmThread) clearFrameRecords() {
	if thread == nil {
		return
	}
	clear(thread.frameRecords)
	thread.frameRecords = thread.frameRecords[:0]
	thread.maxFrameRecords = 0
}

func (thread *vmThread) clearRootClosureSlots() {
	if thread == nil {
		return
	}
	for _, identity := range thread.rootClosureSlots {
		if identity == nil {
			continue
		}
		identity.proto = nil
		identity.upvalues = nil
		identity.upvalueValues = nil
		identity.upvalueValueOK = nil
	}
}

func (thread *vmThread) clearRootClosureSlot(depth int) {
	if thread == nil || depth < 0 || depth >= len(thread.rootClosureSlots) {
		return
	}
	identity := thread.rootClosureSlots[depth]
	if identity == nil {
		return
	}
	identity.proto = nil
	identity.upvalues = nil
	identity.upvalueValues = nil
	identity.upvalueValueOK = nil
}

func (thread *vmThread) truncateFrameRecords(depth int) {
	if thread == nil {
		return
	}
	if depth < 0 {
		depth = 0
	}
	if depth >= len(thread.frameRecords) {
		return
	}
	clear(thread.frameRecords[depth:])
	thread.frameRecords = thread.frameRecords[:depth]
}

type vmFrame struct {
	proto           *Proto
	currentClosure  *closure
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
	varargOwner     *vmStackOwner
	varargBase      int
	varargCount     int
	pc              int
	debugLine       int
	openResultStart int
	openResults     vmResultWindow
	// openRangeOwner/base/count describe a contiguous, owner-backed open
	// result range.  openRangeLogicalTop is the owner length before the range
	// was published; it lets the range be cleared and the shared stack be
	// rebound without retaining a separate result slice.  The owner pointer is
	// the existing frame/thread stack owner, never a newly allocated object.
	openRangeOwner      *vmStackOwner
	openRangeBase       int
	openRangeCount      int
	openRangeLogicalTop int
	pendingCall         vmPendingCall
	hasPendingCall      bool
	// recordBaseDepth is the compact continuation depth owned by this physical
	// frame. It survives direct-loop side exits so a resumed logical callee can
	// still unwind record-only callers.
	recordBaseDepth int
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

// vmFunctionInstance owns mutable execution artifacts for one immutable Proto
// on one vmThread. Compiled prototypes can therefore be run concurrently
// without sharing inline caches or reusable closure values.
type vmFunctionInstance struct {
	caches           []*dynamicStringIndexCache
	canonicalClosure *closure
}

// dynamicStringIndexCacheCold marks a cache site that has been observed once
// but has not yet earned a full inline-cache allocation. It is never returned
// to callers, so the shared sentinel cannot be mutated by cache helpers.
var dynamicStringIndexCacheCold = &dynamicStringIndexCache{}

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

func (instance *vmFunctionInstance) cacheAt(cacheID uint32) *dynamicStringIndexCache {
	if instance == nil || cacheID >= uint32(len(instance.caches)) {
		return nil
	}
	cache := instance.caches[cacheID]
	if cache == nil {
		instance.caches[cacheID] = dynamicStringIndexCacheCold
		return nil
	}
	if cache == dynamicStringIndexCacheCold {
		cache = &dynamicStringIndexCache{}
		instance.caches[cacheID] = cache
	}
	return cache
}

type vmSuspendedFrames struct {
	ctx                   context.Context
	globals               *globalEnv
	frames                []*vmFrame
	frameRecords          []vmFrameRecord
	rootClosureSlots      []*closure
	maxFrameRecords       int
	owner                 *vmStackOwner
	openUpvalues          []*cell
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

func (thread *vmThread) functionValueWithCapturedUpvalues(proto *Proto, captured capturedUpvalueSet) Value {
	if captured.count == 0 {
		if proto != nil && proto.reuseZeroCaptureClosure {
			instance := thread.functionInstance(proto)
			if instance != nil {
				if instance.canonicalClosure == nil {
					instance.canonicalClosure = &closure{proto: proto}
				}
				return closureFunctionValue(instance.canonicalClosure)
			}
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
	thread := vmThread{
		ctx:                   ctx,
		globals:               globals,
		stackOwner:            &vmStackOwner{},
		nearestProtectedFrame: noProtectedFrame,
		instructionBudget:     -1,
	}
	if globals != nil {
		thread.owner = globals.owner
	}
	return thread
}

func (thread *vmThread) functionInstance(proto *Proto) *vmFunctionInstance {
	if thread == nil || proto == nil || (proto.cacheSiteCount == 0 && !proto.reuseZeroCaptureClosure) {
		return nil
	}
	if thread.functionInstances == nil {
		thread.functionInstances = make(map[*Proto]*vmFunctionInstance)
	}
	instance := thread.functionInstances[proto]
	if instance == nil {
		instance = &vmFunctionInstance{}
		if proto.cacheSiteCount > 0 {
			instance.caches = make([]*dynamicStringIndexCache, proto.cacheSiteCount)
		}
		thread.functionInstanceSites += proto.cacheSiteCount
		thread.functionInstances[proto] = instance
		return instance
	}
	if len(instance.caches) != proto.cacheSiteCount {
		thread.functionInstanceSites += proto.cacheSiteCount - len(instance.caches)
		if proto.cacheSiteCount > 0 {
			instance.caches = make([]*dynamicStringIndexCache, proto.cacheSiteCount)
		} else {
			instance.caches = nil
		}
	}
	return instance
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
		thread.baseGlobals = globalEnv{pooled: true}
		globals = &thread.baseGlobals
	}
	thread.ctx = ctx
	thread.globals = globals
	thread.owner = nil
	if globals != nil {
		thread.owner = globals.owner
	}
	thread.frames = thread.frames[:0]
	thread.clearFrameRecords()
	thread.clearRootClosureSlots()
	thread.closeAllOpenUpvalues()
	if thread.stackOwner == nil {
		thread.stackOwner = &vmStackOwner{}
	}
	thread.stackOwner.thread = thread
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
	if cap(thread.stringScratch) > 64*1024 {
		thread.stringScratch = nil
	} else {
		thread.stringScratch = thread.stringScratch[:0]
	}
}

func (thread *vmThread) resetForPool() {
	owned := thread.ownerBound
	thread.dropFrames(0)
	thread.clearFrameRecords()
	thread.clearRootClosureSlots()
	thread.closeAllOpenUpvalues()
	if owned {
		for _, frame := range thread.frameSlots {
			frame.resetForPool()
		}
	}
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
		thread.stackOwner.thread = thread
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
	if len(thread.functionInstances) > 64 || thread.functionInstanceSites > 1024 {
		thread.functionInstances = nil
		thread.functionInstanceSites = 0
	}
	thread.unbindOwner()
}

func (thread *vmThread) bindOwner(owner *runtimeOwner) error {
	if thread == nil {
		return errRuntimeOwnerInvalid
	}
	if owner == nil {
		thread.owner = nil
		thread.ownerBound = false
		return nil
	}
	if thread.ownerBound {
		if thread.owner != owner {
			return errRuntimeOwnerInvalid
		}
		return nil
	}
	bundle, err := owner.checkoutVMThread(thread)
	if err != nil {
		return err
	}
	thread.owner = owner
	thread.ownerBound = true
	thread.attachVMCaches(bundle)
	return nil
}

func (thread *vmThread) unbindOwner() {
	if thread == nil {
		return
	}
	if thread.ownerBound {
		if thread.owner != nil {
			owner := thread.owner
			bundle := thread.detachVMCaches()
			owner.returnVMThread(thread, bundle)
		}
		thread.ownerBound = false
	}
	thread.owner = nil
}

func (thread *vmThread) attachVMCaches(bundle vmCacheBundle) {
	thread.stringIntern = bundle.stringIntern
	thread.stringConcatIntern = bundle.stringConcatIntern
	thread.functionInstances = bundle.functionInstances
	thread.functionInstanceSites = bundle.functionInstanceSites
}

func (thread *vmThread) detachVMCaches() vmCacheBundle {
	bundle := vmCacheBundle{
		stringIntern:          thread.stringIntern,
		stringConcatIntern:    thread.stringConcatIntern,
		functionInstances:     thread.functionInstances,
		functionInstanceSites: thread.functionInstanceSites,
	}
	thread.stringIntern = nil
	thread.stringConcatIntern = nil
	thread.functionInstances = nil
	thread.functionInstanceSites = 0
	return bundle
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
	thread.owner = parent.owner
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
		frameRecords:          thread.frameRecords,
		rootClosureSlots:      thread.rootClosureSlots,
		maxFrameRecords:       thread.maxFrameRecords,
		owner:                 thread.stackOwner,
		openUpvalues:          thread.openUpvalues,
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
	thread.frameRecords = nil
	thread.rootClosureSlots = nil
	thread.stackOwner = nil
	thread.stack = nil
	thread.openUpvalues = nil
	thread.nearestProtectedFrame = noProtectedFrame
	return suspended
}

func (thread *vmThread) resumeFrames(suspended vmSuspendedFrames) {
	thread.ctx = suspended.ctx
	thread.globals = suspended.globals
	thread.frames = suspended.frames
	thread.frameRecords = suspended.frameRecords
	thread.rootClosureSlots = suspended.rootClosureSlots
	thread.maxFrameRecords = suspended.maxFrameRecords
	thread.stackOwner = suspended.owner
	if thread.stackOwner != nil {
		thread.stackOwner.thread = thread
	}
	thread.openUpvalues = suspended.openUpvalues
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
	depth := -1
	if frame != nil {
		depth = frame.depth
	}
	thread.clearPendingCall(frame)
	thread.frames = thread.frames[:len(thread.frames)-1]
	if frame != nil && frame.recordBaseDepth >= 0 {
		thread.truncateFrameRecords(frame.recordBaseDepth)
	}
	thread.popFrameRecordFor(frame)
	thread.releaseFrameWindow(frame)
	frame.resetForReuse()
	thread.clearRootClosureSlot(depth)
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
	frame.currentClosure = thread.rootClosureSlot(len(thread.frames), proto, upvalues, upvalueValues, upvalueValueOK)
	return frame
}

func (thread *vmThread) newCallFrame(proto *Proto, args []Value, upvalues []*cell) *vmFrame {
	return thread.newCallFrameWithUpvalues(proto, args, upvalues, nil, nil)
}

func (thread *vmThread) newClosureCallFrame(closure *closure, args []Value) *vmFrame {
	frame := thread.newCallFrameWithUpvalues(closure.proto, args, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK)
	frame.currentClosure = closure
	return frame
}

func (thread *vmThread) newClosureCallFrameFixed(closure *closure, first Value, second Value, third Value, count int) *vmFrame {
	frame := thread.newCallFrameWithUpvalues(closure.proto, nil, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK)
	frame.currentClosure = closure
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
	frame.currentClosure = closure
	thread.indexFrameOpenUpvalues(frame)
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

// newBorrowedFixedFrameRecord builds the compact continuation metadata for a
// fixed one-result call while retaining the existing vmFrame as an execution
// bridge. The record is pushed only after the borrowed window has passed all
// runtime guards, so cold/generic calls keep their established path.
func (thread *vmThread) newBorrowedFixedFrameRecord(
	closure *closure,
	caller *vmFrame,
	returnPC int,
	argumentStart int,
	argumentCount int,
	destination vmResultDestination,
) (*vmFrame, vmFrameRecord, bool) {
	child, borrowed := thread.newBorrowedClosureCallFrame(closure, caller, argumentStart, argumentCount)
	if !borrowed || child == nil || child.proto == nil {
		return nil, vmFrameRecord{}, false
	}
	returnPCValue, returnPCOK := vmFrameRecordUint32(returnPC)
	baseValue, baseOK := vmFrameRecordUint32(child.registerBase)
	topValue, topOK := vmFrameRecordAddUint32(child.registerBase, child.registerCount)
	resultDestination, resultOK := vmFrameRecordAddUint32(caller.registerBase, destination.register)
	argumentCountValue, argumentCountOK := vmFrameRecordUint16(argumentCount)
	resultCountValue, resultCountOK := vmFrameRecordUint32(destination.count)
	frameDepth, frameDepthOK := vmFrameRecordUint16(len(thread.frames))
	if !returnPCOK || !baseOK || !topOK || !resultOK || !argumentCountOK || !resultCountOK || !frameDepthOK {
		thread.releaseFrameWindow(child)
		child.resetForReuse()
		return nil, vmFrameRecord{}, false
	}

	flags := vmFrameRecordFlags(0)
	protectedDepth := uint16(0)
	if thread.nearestProtectedFrame >= 0 {
		var protectedOK bool
		protectedDepth, protectedOK = vmFrameRecordUint16(thread.nearestProtectedFrame)
		if !protectedOK {
			thread.releaseFrameWindow(child)
			child.resetForReuse()
			return nil, vmFrameRecord{}, false
		}
		flags |= vmFrameRecordFlagProtected
	}
	if destination.count < 0 {
		flags |= vmFrameRecordFlagOpenResults
	}
	record := vmFrameRecord{
		closure:           closure,
		returnPC:          returnPCValue,
		base:              baseValue,
		top:               topValue,
		resultDestination: resultDestination,
		resultCount:       resultCountValue,
		argumentBase:      baseValue,
		varargBase:        0,
		protectedDepth:    protectedDepth,
		frameDepth:        frameDepth,
		argumentCount:     argumentCountValue,
		varargCount:       0,
		flags:             flags,
	}
	return child, record, true
}

// enterRecordOnlyFixedCall switches the live physical frame to a fixed-result
// callee and leaves the caller continuation entirely in a compact record.
// Caller cells remain indexed on the thread and are reconstructed when the
// physical frame is rebound to the saved closure.
func (thread *vmThread) maybeEnterRecordOnlyFixedCall(
	closure *closure,
	frame *vmFrame,
	returnPC int,
	argumentStart int,
	argumentCount int,
	destination vmResultDestination,
) (vmFrameRecord, bool) {
	if frame == nil {
		return vmFrameRecord{}, false
	}
	return thread.enterRecordOnlyFixedCall(closure, frame, returnPC, argumentStart, argumentCount, destination)
}

func (thread *vmThread) enterRecordOnlyFixedCall(
	closure *closure,
	frame *vmFrame,
	returnPC int,
	argumentStart int,
	argumentCount int,
	destination vmResultDestination,
) (vmFrameRecord, bool) {
	if thread == nil || closure == nil || closure.proto == nil || frame == nil || frame.proto == nil {
		return vmFrameRecord{}, false
	}
	proto := closure.proto
	if frame.varargCount != 0 ||
		thread.debugHook != nil || thread.instructionBudget >= 0 || thread.coroutine != nil ||
		thread.nonYieldableDepth != 0 || thread.nearestProtectedFrame != noProtectedFrame ||
		frame.hasPendingCall || frame.openResultStart >= 0 {
		return vmFrameRecord{}, false
	}
	openDestination := destination.count < 0
	if destination.count < -1 || destination.count == 0 || destination.register < 0 ||
		(openDestination && destination.register >= frame.registerCount) ||
		(!openDestination && destination.count > frame.registerCount-destination.register) ||
		((destination.count >= 2 || openDestination) && destination.register >= argumentStart) || argumentCount < 0 || argumentStart < 0 ||
		argumentStart > frame.registerCount || argumentCount > frame.registerCount-argumentStart ||
		frame.hasCellsInRange(argumentStart, frame.registerCount-argumentStart) ||
		(openDestination && frame.hasCellsInRange(destination.register, frame.registerCount-destination.register)) {
		return vmFrameRecord{}, false
	}
	if frame.registerBase < 0 || argumentStart > int(^uint(0)>>1)-frame.registerBase {
		return vmFrameRecord{}, false
	}
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	if owner == nil || owner != thread.stackOwner {
		return vmFrameRecord{}, false
	}

	callerClosure := thread.currentClosureForFrame(frame)
	callerBase, callerBaseOK := vmFrameRecordUint32(frame.registerBase)
	callerTop, callerTopOK := vmFrameRecordAddUint32(frame.registerBase, frame.registerCount)
	returnPCValue, returnPCOK := vmFrameRecordUint32(returnPC)
	resultDestination, resultOK := vmFrameRecordAddUint32(frame.registerBase, destination.register)
	argumentCountValue, argumentCountOK := vmFrameRecordUint16(argumentCount)
	resultCountValue, resultCountOK := vmFrameRecordUint32(destination.count)
	if openDestination {
		resultCountValue = ^uint32(0)
		resultCountOK = true
	}
	frameDepth, frameDepthOK := vmFrameRecordUint16(frame.depth)
	previousStackLength, previousStackLengthOK := vmFrameRecordUint32(frame.window.previousStackLength)
	childBase := frame.registerBase + argumentStart
	childBaseValue, childBaseOK := vmFrameRecordUint32(childBase)
	if !callerBaseOK || !callerTopOK || !returnPCOK || !resultOK || !argumentCountOK || !resultCountOK ||
		!frameDepthOK || !previousStackLengthOK || !childBaseOK || proto.registers < 0 ||
		childBase > int(^uint(0)>>1)-proto.registers {
		return vmFrameRecord{}, false
	}
	childEnd := childBase + proto.registers
	previousLength := len(owner.values)
	if childEnd > previousLength {
		thread.growStack(childEnd)
		owner = thread.stackOwner
	}
	if owner == nil || childEnd > len(owner.values) {
		return vmFrameRecord{}, false
	}

	// Reserve the owner-backed vararg tail before taking any source slices.
	// Stack growth may reallocate the owner, so the reset below passes absolute
	// source offsets rather than retaining a pre-growth slice.
	varargCount := 0
	if proto.variadic && argumentCount > proto.params {
		varargCount = argumentCount - proto.params
		varargBase := childBase + proto.registers
		if varargBase < childBase || varargCount > int(^uint(0)>>1)-varargBase {
			return vmFrameRecord{}, false
		}
		thread.growStack(varargBase + varargCount)
		owner = thread.stackOwner
		if owner == nil || varargBase+varargCount > len(owner.values) {
			return vmFrameRecord{}, false
		}
	}
	paramCount := argumentCount
	if paramCount > proto.params {
		paramCount = proto.params
	}
	if paramCount > proto.registers {
		paramCount = proto.registers
	}
	args := owner.values[childBase : childBase+paramCount]
	registers := owner.values[childBase:childEnd]
	physicalDepth := frame.depth
	recordBaseDepth := frame.recordBaseDepth
	callerBorrowed := frame.window.borrowed
	if proto.variadic && varargCount > 0 {
		frame.resetFrameIntoRegistersWithVarargSource(
			proto,
			args,
			closure.upvalues,
			closure.upvalueValues,
			closure.upvalueValueOK,
			owner,
			childBase,
			registers,
			childBase+proto.params,
			varargCount,
		)
	} else {
		frame.resetFrameIntoRegisters(
			proto,
			args,
			closure.upvalues,
			closure.upvalueValues,
			closure.upvalueValueOK,
			owner,
			childBase,
			registers,
		)
	}
	frame.currentClosure = closure
	thread.indexFrameOpenUpvalues(frame)
	frame.depth = physicalDepth
	if recordBaseDepth < 0 {
		recordBaseDepth = len(thread.frameRecords)
	}
	frame.recordBaseDepth = recordBaseDepth
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                childBase,
		length:              len(registers),
		previousStackLength: previousLength,
		borrowed:            true,
	}
	flags := vmFrameRecordFlagRecordOnly
	if openDestination {
		flags |= vmFrameRecordFlagOpenResults
	}
	if callerBorrowed {
		flags |= vmFrameRecordFlagCallerBorrowed
	}
	return vmFrameRecord{
		closure:           callerClosure,
		returnPC:          returnPCValue,
		base:              callerBase,
		top:               callerTop,
		resultDestination: resultDestination,
		resultCount:       resultCountValue,
		argumentBase:      childBaseValue,
		varargBase:        previousStackLength,
		frameDepth:        frameDepth,
		argumentCount:     argumentCountValue,
		flags:             flags,
	}, true
}

// maybeEnterRecordOnlyOpenArgumentCall switches the physical frame to a
// callee whose arguments are a fixed prefix followed by the caller's
// owner-backed open-result range. The dynamic tail is deliberately borrowed:
// only the fixed prefix is copied into the dead scratch slots immediately
// before the range. This keeps open forwarding allocation-free while leaving
// the existing compact return records in charge of restoring the caller.
func (thread *vmThread) maybeEnterRecordOnlyOpenArgumentCall(
	closure *closure,
	frame *vmFrame,
	returnPC int,
	argumentStart int,
	prefixCount int,
	destination vmResultDestination,
) (vmFrameRecord, bool) {
	if thread == nil || closure == nil || closure.proto == nil || frame == nil || frame.proto == nil {
		return vmFrameRecord{}, false
	}
	proto := closure.proto
	// Variadic callers retain a separate vararg window which the compact
	// record does not currently encode. They stay on the established cold
	// materialized path until that ABI is widened.
	if proto.variadic || frame.varargCount != 0 ||
		thread.debugHook != nil || thread.instructionBudget >= 0 || thread.coroutine != nil ||
		thread.nonYieldableDepth != 0 || thread.nearestProtectedFrame != noProtectedFrame ||
		frame.hasPendingCall || frame.openResultStart < 0 || len(proto.capturedLocals) != 0 {
		return vmFrameRecord{}, false
	}
	if prefixCount < 0 || argumentStart < 0 || argumentStart > frame.registerCount-prefixCount {
		return vmFrameRecord{}, false
	}
	openStart := argumentStart + prefixCount
	if openStart < argumentStart || frame.openResultStart != openStart || openStart >= frame.registerCount {
		return vmFrameRecord{}, false
	}
	// The marker's proof reserves exactly the caller's suffix for the fixed
	// prefix. The open range itself must begin at the static register top; an
	// unrelated owner tail would make the physical child window ambiguous.
	scratchStart := frame.registerCount - prefixCount
	if scratchStart < 0 || scratchStart+prefixCount > frame.registerCount {
		return vmFrameRecord{}, false
	}
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	if owner == nil || owner != thread.stackOwner || frame.registerBase < 0 {
		return vmFrameRecord{}, false
	}
	openBase := frame.openRangeBase
	openCount := frame.openRangeCount
	if frame.openRangeOwner != owner || openBase < 0 || openCount <= 0 ||
		openBase != frame.registerBase+frame.registerCount ||
		openBase > len(owner.values) || openCount > len(owner.values)-openBase {
		return vmFrameRecord{}, false
	}
	childBase := openBase - prefixCount
	if childBase < frame.registerBase || childBase != frame.registerBase+scratchStart ||
		proto.registers < 0 || childBase > int(^uint(0)>>1)-proto.registers {
		return vmFrameRecord{}, false
	}
	childEnd := childBase + proto.registers
	if childEnd < childBase {
		return vmFrameRecord{}, false
	}
	// Result slots must remain outside the scratch suffix. The open-result
	// destination is checked in the same way; its dynamic tail is produced by
	// the child and restored by resumeRecordOnlyOpenCall.
	openDestination := destination.count < 0
	if destination.count < -1 || destination.register < 0 || destination.register >= frame.registerCount ||
		(openDestination && destination.register >= scratchStart) ||
		(!openDestination && (destination.count == 0 || destination.register > frame.registerCount-destination.count ||
			(destination.count > 0 && destination.register+destination.count > scratchStart))) {
		return vmFrameRecord{}, false
	}
	if frame.hasCellsInRange(scratchStart, prefixCount) {
		return vmFrameRecord{}, false
	}
	if openCount > int(^uint16(0))-prefixCount {
		return vmFrameRecord{}, false
	}
	argumentCount := prefixCount + openCount
	callerClosure := thread.currentClosureForFrame(frame)
	callerBase, callerBaseOK := vmFrameRecordUint32(frame.registerBase)
	callerTop, callerTopOK := vmFrameRecordAddUint32(frame.registerBase, frame.registerCount)
	returnPCValue, returnPCOK := vmFrameRecordUint32(returnPC)
	resultDestination, resultOK := vmFrameRecordAddUint32(frame.registerBase, destination.register)
	argumentBase, argumentBaseOK := vmFrameRecordUint32(childBase)
	argumentCountValue, argumentCountOK := vmFrameRecordUint16(argumentCount)
	resultCountValue, resultCountOK := vmFrameRecordUint32(destination.count)
	if openDestination {
		resultCountValue = ^uint32(0)
		resultCountOK = true
	}
	frameDepth, frameDepthOK := vmFrameRecordUint16(frame.depth)
	previousStackLength, previousStackLengthOK := vmFrameRecordUint32(frame.window.previousStackLength)
	if !callerBaseOK || !callerTopOK || !returnPCOK || !resultOK || !argumentBaseOK || !argumentCountOK ||
		!resultCountOK || !frameDepthOK || !previousStackLengthOK || childEnd > int(^uint32(0)) {
		return vmFrameRecord{}, false
	}
	previousLength := len(owner.values)
	if childEnd > previousLength {
		thread.growStack(childEnd)
		owner = thread.stackOwner
	}
	if owner == nil || childEnd > len(owner.values) {
		return vmFrameRecord{}, false
	}
	// Copy only the fixed prefix. The dynamic tail already starts at
	// childBase+prefixCount and remains in place for the child parameters.
	if prefixCount > 0 {
		prefixSource := frame.registerBase + argumentStart
		if prefixSource < 0 || prefixSource > len(owner.values)-prefixCount {
			return vmFrameRecord{}, false
		}
		copy(owner.values[childBase:childBase+prefixCount], owner.values[prefixSource:prefixSource+prefixCount])
	}
	// Detach the caller's open metadata without clearing its owner range: that
	// range is now the child's argument tail and will be released with the
	// child window on return. resetFrameIntoRegisters aliases the contiguous
	// owner range for parameter initialization, so no dynamic-tail copy occurs.
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	frame.openRangeOwner = nil
	frame.openRangeBase = -1
	frame.openRangeCount = 0
	frame.openRangeLogicalTop = -1
	copyCount := proto.params
	if copyCount > proto.registers {
		copyCount = proto.registers
	}
	if copyCount > argumentCount {
		copyCount = argumentCount
	}
	var args []Value
	if copyCount > 0 {
		args = owner.values[childBase : childBase+copyCount]
	}
	physicalDepth := frame.depth
	recordBaseDepth := frame.recordBaseDepth
	callerBorrowed := frame.window.borrowed
	registers := owner.values[childBase:childEnd]
	frame.resetFrameIntoRegisters(proto, args, closure.upvalues, closure.upvalueValues, closure.upvalueValueOK, owner, childBase, registers)
	// Arguments beyond the callee's physical register window are ignored by
	// the call. Clear that suffix now as well as during record resume so an
	// error/yield cleanup cannot retain the forwarded tail in the shared owner.
	ignoredStart := childEnd
	if ignoredStart < openBase {
		ignoredStart = openBase
	}
	ignoredEnd := openBase + openCount
	if ignoredStart < ignoredEnd {
		clear(owner.values[ignoredStart:ignoredEnd])
	}
	frame.currentClosure = closure
	thread.indexFrameOpenUpvalues(frame)
	frame.depth = physicalDepth
	if recordBaseDepth < 0 {
		recordBaseDepth = len(thread.frameRecords)
	}
	frame.recordBaseDepth = recordBaseDepth
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                childBase,
		length:              len(registers),
		previousStackLength: previousLength,
		borrowed:            true,
	}
	flags := vmFrameRecordFlagRecordOnly | vmFrameRecordFlagOpenArguments
	if openDestination {
		flags |= vmFrameRecordFlagOpenResults
	}
	if callerBorrowed {
		flags |= vmFrameRecordFlagCallerBorrowed
	}
	return vmFrameRecord{
		closure:           callerClosure,
		returnPC:          returnPCValue,
		base:              callerBase,
		top:               callerTop,
		resultDestination: resultDestination,
		resultCount:       resultCountValue,
		argumentBase:      argumentBase,
		varargBase:        previousStackLength,
		frameDepth:        frameDepth,
		argumentCount:     argumentCountValue,
		flags:             flags,
	}, true
}

func vmFrameRecordUint32(value int) (uint32, bool) {
	if value < 0 || uint64(value) > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(value), true
}

func vmFrameRecordUint16(value int) (uint16, bool) {
	if value < 0 || uint64(value) > uint64(^uint16(0)) {
		return 0, false
	}
	return uint16(value), true
}

func vmFrameRecordAddUint32(left, right int) (uint32, bool) {
	leftValue, leftOK := vmFrameRecordUint32(left)
	rightValue, rightOK := vmFrameRecordUint32(right)
	if !leftOK || !rightOK || uint64(leftValue)+uint64(rightValue) > uint64(^uint32(0)) {
		return 0, false
	}
	return leftValue + rightValue, true
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

func (thread *vmThread) rootClosureSlot(depth int, proto *Proto, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) *closure {
	if thread == nil || depth < 0 {
		return nil
	}
	for len(thread.rootClosureSlots) <= depth {
		thread.rootClosureSlots = append(thread.rootClosureSlots, nil)
	}
	identity := thread.rootClosureSlots[depth]
	if identity == nil {
		identity = &closure{}
		thread.rootClosureSlots[depth] = identity
	}
	identity.proto = proto
	identity.upvalues = upvalues
	identity.upvalueValues = upvalueValues
	identity.upvalueValueOK = upvalueValueOK
	return identity
}

func (thread *vmThread) currentClosureForFrame(frame *vmFrame) *closure {
	if frame == nil {
		return nil
	}
	if frame.currentClosure != nil {
		return frame.currentClosure
	}
	return thread.rootClosureSlot(frame.depth, frame.proto, frame.upvalues, frame.upvalueValues, frame.upvalueValueOK)
}

func (thread *vmThread) resetFrame(frame *vmFrame, proto *Proto, args []Value, upvalues []*cell, upvalueValues []Value, upvalueValueOK []bool) {
	thread.clearPendingCall(frame)
	owner := thread.ensureStackOwner()
	previousLength := len(owner.values)
	base := previousLength
	thread.growStack(base + proto.registers)
	registers := owner.values[base : base+proto.registers]
	frame.resetFrameIntoRegisters(proto, args, upvalues, upvalueValues, upvalueValueOK, owner, base, registers)
	thread.indexFrameOpenUpvalues(frame)
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              len(registers),
		previousStackLength: previousLength,
	}
}

// growStackOwner extends an owner-backed frame range without requiring a
// vmThread. Standalone test frames use owners that are not attached to a
// thread; active frames still keep the thread's stack view synchronized.
func growStackOwner(owner *vmStackOwner, size int) bool {
	if owner == nil || size < 0 {
		return false
	}
	if owner.thread != nil && owner.thread.stackOwner == owner {
		owner.thread.growStack(size)
		return owner.thread.stackOwner == owner && size <= len(owner.values)
	}
	if size > cap(owner.values) {
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
	} else {
		owner.values = owner.values[:size]
	}
	return true
}

func (thread *vmThread) ensureStackOwner() *vmStackOwner {
	if thread.stackOwner == nil {
		thread.stackOwner = &vmStackOwner{values: thread.stack}
	}
	thread.stackOwner.thread = thread
	thread.stack = thread.stackOwner.values
	return thread.stackOwner
}

// openUpvalue returns the unique live cell for an absolute stack slot on this
// thread. Keeping the index on the thread makes captures from the same slot
// share one cell even when multiple closures observe it.
func (thread *vmThread) openUpvalue(owner *vmStackOwner, index int) *cell {
	if thread == nil || owner == nil || index < 0 {
		return nil
	}
	for _, candidate := range thread.openUpvalues {
		if candidate != nil && candidate.owner == owner && candidate.index == index {
			return candidate
		}
	}
	cell := &cell{}
	cell.openAt(owner, index)
	thread.openUpvalues = append(thread.openUpvalues, cell)
	return cell
}

func (thread *vmThread) lookupOpenUpvalue(owner *vmStackOwner, index int) *cell {
	if thread == nil || owner == nil || index < 0 {
		return nil
	}
	for _, candidate := range thread.openUpvalues {
		if candidate != nil && candidate.owner == owner && candidate.index == index {
			return candidate
		}
	}
	return nil
}

func (thread *vmThread) restoreFrameCells(frame *vmFrame, proto *Proto, owner *vmStackOwner, base int) []*cell {
	if frame == nil || proto == nil || len(proto.capturedLocals) == 0 {
		return nil
	}
	var cells []*cell
	if cap(frame.cells) >= proto.registers {
		cells = frame.cells[:proto.registers]
		clear(cells)
	} else {
		cells = make([]*cell, proto.registers)
	}
	for index, captured := range proto.capturedLocals {
		if !captured {
			continue
		}
		cells[index] = thread.lookupOpenUpvalue(owner, base+index)
	}
	return cells
}

// indexFrameOpenUpvalues replaces a frame's compatibility cell slots with the
// thread-owned cells for their absolute stack positions. The frame.cells slice
// remains available to existing helpers and tests, while the thread index
// provides cross-capture identity and a single close point.
func (thread *vmThread) indexFrameOpenUpvalues(frame *vmFrame) {
	if thread == nil || frame == nil || frame.owner == nil {
		return
	}
	for index, candidate := range frame.cells {
		if candidate == nil {
			continue
		}
		absoluteIndex := frame.registerBase + index
		var indexed *cell
		for _, open := range thread.openUpvalues {
			if open != nil && open.owner == frame.owner && open.index == absoluteIndex {
				indexed = open
				break
			}
		}
		if indexed == nil {
			thread.openUpvalues = append(thread.openUpvalues, candidate)
			continue
		}
		if indexed != candidate {
			candidate.close()
			frame.cells[index] = indexed
		}
	}
}

// closeOpenUpvalues closes and removes cells in [start, end) before the stack
// owner is truncated. This ordering is required because a cell reads its live
// value from the owner's current slice.
func (thread *vmThread) closeOpenUpvalues(owner *vmStackOwner, start, end int) {
	if thread == nil || owner == nil || start < 0 || end <= start {
		return
	}
	entries := thread.openUpvalues
	kept := entries[:0]
	for _, candidate := range entries {
		if candidate != nil && candidate.owner == owner && candidate.index >= start && candidate.index < end {
			candidate.close()
			continue
		}
		kept = append(kept, candidate)
	}
	clear(entries[len(kept):])
	thread.openUpvalues = kept
}

func (thread *vmThread) closeAllOpenUpvalues() {
	if thread == nil {
		return
	}
	for _, candidate := range thread.openUpvalues {
		if candidate != nil {
			candidate.close()
		}
	}
	clear(thread.openUpvalues)
	thread.openUpvalues = thread.openUpvalues[:0]
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
	frame.clearOpenResultRange()
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
		start := window.base
		end := start + window.length
		if window.length == 0 {
			start = frame.registerBase
			end = frame.registerBase + frame.registerCount
		}
		thread.closeOpenUpvalues(owner, start, end)
		frame.closeCells()
		if frame.varargOwner == owner {
			frame.clearVarargStorage()
		}
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
		frame.closeCells()
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
		frameDepth := -1
		if frame != nil {
			frameDepth = frame.depth
		}
		thread.clearPendingCall(frame)
		thread.clearDroppedFrameRecordArguments(frame)
		if frame != nil && frame.recordBaseDepth >= 0 {
			thread.truncateFrameRecords(frame.recordBaseDepth)
		}
		thread.popFrameRecordFor(frame)
		thread.releaseFrameWindow(frame)
		if frame != nil {
			frame.resetForReuse()
		}
		thread.clearRootClosureSlot(frameDepth)
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
	frame.currentClosure = closure
	thread.indexFrameOpenUpvalues(frame)
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
	frame.resetFrameIntoRegistersWithVarargSource(proto, args, upvalues, upvalueValues, upvalueValueOK, owner, base, registers, -1, -1)
}

// resetFrameIntoRegistersWithVarargSource is the owner-aware reset used by
// borrowed variadic calls. A normal frame derives its vararg source from the
// args slice. A borrowed frame instead passes an absolute owner offset so a
// stack growth/reallocation cannot leave a stale source slice behind.
func (frame *vmFrame) resetFrameIntoRegistersWithVarargSource(
	proto *Proto,
	args []Value,
	upvalues []*cell,
	upvalueValues []Value,
	upvalueValueOK []bool,
	owner *vmStackOwner,
	base int,
	registers []Value,
	varargSourceBase int,
	varargSourceCount int,
) {
	frame.clearOpenResultRange()
	frame.currentClosure = nil
	varargCount := varargSourceCount
	if varargCount < 0 {
		varargCount = 0
		if proto.variadic && len(args) > proto.params {
			varargCount = len(args) - proto.params
		}
	}
	varargBase := base + len(registers)
	if varargCount > 0 {
		if varargBase < base || varargCount > int(^uint(0)>>1)-varargBase || !growStackOwner(owner, varargBase+varargCount) {
			panic("ember: unable to allocate variadic frame storage")
		}
		registers = owner.values[base : base+len(registers)]
		if varargSourceBase >= 0 {
			if varargSourceBase > len(owner.values) || varargCount > len(owner.values)-varargSourceBase {
				panic("ember: invalid owner-backed variadic source")
			}
			copy(owner.values[varargBase:varargBase+varargCount], owner.values[varargSourceBase:varargSourceBase+varargCount])
		} else {
			copy(owner.values[varargBase:varargBase+varargCount], args[proto.params:])
		}
	}

	// Nil entry initialization must happen after owner-backed vararg copying:
	// entryNilRegisters can overlap extra argument slots in a borrowed window.
	for _, register := range proto.entryNilRegisters {
		registers[register] = NilValue()
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
	frame.depth = noProtectedFrame
	frame.registerBase = base
	frame.registerCount = len(registers)
	frame.owner = owner
	frame.registers = registers
	frame.cells = cells
	frame.upvalues = upvalues
	frame.upvalueValues = upvalueValues
	frame.upvalueValueOK = upvalueValueOK
	frame.varargOwner = owner
	frame.varargBase = varargBase
	frame.varargCount = varargCount
	frame.pc = 0
	frame.debugLine = -1
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	frame.openRangeOwner = nil
	frame.openRangeBase = -1
	frame.openRangeCount = 0
	frame.openRangeLogicalTop = -1
	frame.resetPendingCallState()
	frame.recordBaseDepth = -1
}

func (frame *vmFrame) resetForReuse() {
	frame.closeCells()
	frame.clearOpenResultRange()
	frame.proto = nil
	frame.currentClosure = nil
	frame.depth = noProtectedFrame
	frame.window = vmRegisterWindow{}
	frame.registerBase = 0
	frame.registerCount = 0
	frame.owner = nil
	frame.upvalues = nil
	frame.upvalueValues = nil
	frame.upvalueValueOK = nil
	frame.varargOwner = nil
	frame.varargBase = 0
	frame.varargCount = 0
	frame.pc = 0
	frame.debugLine = -1
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
	frame.openRangeOwner = nil
	frame.openRangeBase = -1
	frame.openRangeCount = 0
	frame.openRangeLogicalTop = -1
	frame.resetPendingCallState()
	frame.recordBaseDepth = -1
}

func (frame *vmFrame) resetForPool() {
	clear(frame.registers)
	clear(frame.cells)
	frame.clearVarargStorage()
	if cap(frame.openResults.values) > 0 {
		clear(frame.openResults.values[:cap(frame.openResults.values)])
	}
	frame.resetForReuse()
	frame.registers = nil
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

func (frame *vmFrame) varargLen() int {
	if frame == nil || frame.varargCount <= 0 || frame.varargOwner == nil {
		return 0
	}
	start := frame.varargBase
	end := start + frame.varargCount
	if start < 0 || end < start || end > len(frame.varargOwner.values) {
		return 0
	}
	return frame.varargCount
}

func (frame *vmFrame) clearVarargStorage() {
	if frame == nil || frame.varargOwner == nil || frame.varargCount <= 0 {
		return
	}
	start := frame.varargBase
	end := start + frame.varargCount
	if start < 0 || end < start || start >= len(frame.varargOwner.values) {
		return
	}
	if end > len(frame.varargOwner.values) {
		end = len(frame.varargOwner.values)
	}
	clear(frame.varargOwner.values[start:end])
}

func (frame *vmFrame) varargAt(index int) Value {
	if frame == nil || index < 0 || index >= frame.varargLen() {
		return NilValue()
	}
	return frame.varargOwner.values[frame.varargBase+index]
}

func (frame *vmFrame) varargValues() []Value {
	count := frame.varargLen()
	if count == 0 {
		return nil
	}
	return frame.varargOwner.values[frame.varargBase : frame.varargBase+count]
}

// clearOpenResultRange releases an owner-backed open result range and restores
// the shared stack's logical top from before the range was published. It uses
// the existing frame/thread stack owner and never allocates a result object.
func (frame *vmFrame) clearOpenResultRange() {
	if frame == nil {
		return
	}
	owner := frame.openRangeOwner
	base := frame.openRangeBase
	count := frame.openRangeCount
	logicalTop := frame.openRangeLogicalTop
	if owner != nil && base >= 0 && count > 0 && base <= len(owner.values) {
		end := base + count
		if end > len(owner.values) {
			end = len(owner.values)
		}
		if end > base {
			clear(owner.values[base:end])
		}
		if logicalTop >= 0 && logicalTop <= len(owner.values) {
			owner.values = owner.values[:logicalTop]
			if owner.thread != nil && owner.thread.stackOwner == owner {
				owner.thread.stack = owner.values
			}
		}
	}
	frame.openRangeOwner = nil
	frame.openRangeBase = -1
	frame.openRangeCount = 0
	frame.openRangeLogicalTop = -1
}

// clearOpenResultState clears either open-result representation. Keeping range
// cleanup here prevents stale stack-backed values from surviving a subsequent
// fixed-result or host result.
func (frame *vmFrame) clearOpenResultState() {
	if frame == nil {
		return
	}
	// Fixed-result paths call this frequently. Avoid the slower owner cleanup
	// unless a stack-backed range is actually live.
	if frame.openRangeOwner != nil {
		frame.clearOpenResultRange()
	}
	frame.openResultStart = -1
	frame.openResults = vmResultWindow{}
}

// publishOpenResultRange copies values into the existing stack owner and
// publishes the resulting absolute range. Empty varargs retain Luau's open
// result adjustment by materializing one nil value.
func (frame *vmFrame) publishOpenResultRange(thread *vmThread, values []Value) bool {
	if frame == nil {
		return false
	}
	frame.clearOpenResultRange()
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	if owner == nil && thread != nil {
		owner = thread.ensureStackOwner()
		frame.owner = owner
	}
	if owner == nil {
		return false
	}
	if thread == nil || owner != thread.ensureStackOwner() {
		return false
	}
	count := len(values)
	if count == 0 {
		count = 1
	}
	logicalTop := len(owner.values)
	end := logicalTop + count
	if end < logicalTop {
		return false
	}
	thread.growStack(end)
	owner = thread.stackOwner
	copy(owner.values[logicalTop:logicalTop+len(values)], values)
	if len(values) == 0 {
		owner.values[logicalTop] = NilValue()
	}
	frame.openRangeOwner = owner
	frame.openRangeBase = logicalTop
	frame.openRangeCount = count
	frame.openRangeLogicalTop = logicalTop
	return true
}

// publishOpenVarargRange publishes the frame-owned vararg slots without
// materializing a temporary []Value. The source owner remains valid across
// stack growth because growth replaces its values slice in place.
func (frame *vmFrame) publishOpenVarargRange(thread *vmThread) bool {
	if frame == nil || thread == nil {
		return false
	}
	sourceOwner := frame.varargOwner
	sourceBase := frame.varargBase
	count := frame.varargLen()
	if sourceOwner == nil || sourceBase < 0 {
		return false
	}
	frame.clearOpenResultRange()
	destinationOwner := frame.window.owner
	if destinationOwner == nil {
		destinationOwner = frame.owner
	}
	if destinationOwner == nil || destinationOwner != thread.ensureStackOwner() {
		return false
	}
	if count == 0 {
		count = 1
	}
	logicalTop := len(destinationOwner.values)
	if logicalTop < 0 || count > int(^uint(0)>>1)-logicalTop {
		return false
	}
	thread.growStack(logicalTop + count)
	destinationOwner = thread.stackOwner
	if destinationOwner == nil || logicalTop+count > len(destinationOwner.values) {
		return false
	}
	if frame.varargLen() == 0 {
		destinationOwner.values[logicalTop] = NilValue()
	} else {
		for index := 0; index < count; index++ {
			destinationOwner.values[logicalTop+index] = sourceOwner.values[sourceBase+index]
		}
	}
	frame.openRangeOwner = destinationOwner
	frame.openRangeBase = logicalTop
	frame.openRangeCount = count
	frame.openRangeLogicalTop = logicalTop
	return true
}

func (frame *vmFrame) openResultRangeValues() []Value {
	if frame == nil || frame.openResultStart < 0 || frame.openRangeOwner == nil || frame.openRangeBase < 0 || frame.openRangeCount <= 0 {
		return nil
	}
	start := frame.openRangeBase
	end := start + frame.openRangeCount
	if start > len(frame.openRangeOwner.values) || end > len(frame.openRangeOwner.values) || end < start {
		return nil
	}
	return frame.openRangeOwner.values[start:end]
}

func (frame *vmFrame) openResultAt(index int) Value {
	if values := frame.openResultRangeValues(); values != nil {
		if index < 0 || index >= len(values) {
			return NilValue()
		}
		return values[index]
	}
	return frame.openResults.at(index)
}

func (frame *vmFrame) openResultWindow() vmResultWindow {
	if values := frame.openResultRangeValues(); values != nil {
		return vmBorrowedResultWindow(values)
	}
	return frame.openResults
}

func (frame *vmFrame) registerCell(index int) *cell {
	if len(frame.cells) < len(frame.registers) {
		cells := make([]*cell, len(frame.registers))
		copy(cells, frame.cells)
		frame.cells = cells
	}
	if frame.cells[index] == nil {
		owner := frame.owner
		if owner == nil {
			owner = &vmStackOwner{values: frame.registers}
			frame.owner = owner
		}
		frame.cells[index] = &cell{}
		frame.cells[index].openAt(owner, frame.registerBase+index)
	}
	return frame.cells[index]
}

func (thread *vmThread) registerCell(frame *vmFrame, index int) *cell {
	if thread == nil || frame == nil {
		return nil
	}
	if len(frame.cells) < len(frame.registers) {
		cells := make([]*cell, len(frame.registers))
		copy(cells, frame.cells)
		frame.cells = cells
	}
	if frame.cells[index] == nil {
		owner := frame.owner
		if owner == nil {
			owner = thread.stackOwner
			frame.owner = owner
		}
		frame.cells[index] = thread.openUpvalue(owner, frame.registerBase+index)
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
	frame.clearOpenResultState()
	frame.setRegister(register, result.window.at(0))
}

func (frame *vmFrame) applyFrameResultDestination(destination vmResultDestination, result vmFrameResult) {
	frame.applyValueListDestination(destination, result.window)
}

func (frame *vmFrame) applySingleFrameResult(register int, result vmFrameResult) {
	frame.clearOpenResultState()
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
	frame.clearOpenResultRange()
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

	frame.clearOpenResultState()
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
		action := thread.runColdInstruction(frame)
		switch action.kind {
		case coldInstructionActionResume, coldInstructionActionContinue, coldInstructionActionCall:
			continue
		case coldInstructionActionYield, coldInstructionActionReturn:
			return action.result, action.err
		case coldInstructionActionError:
			return vmFrameResult{}, action.err
		default:
			return vmFrameResult{}, fmt.Errorf("run: unknown cold instruction action %d", action.kind)
		}
	}
	return vmFrameResult{}, fmt.Errorf("run: direct frame stopped without a result")
}

func directFrameStringField(value Value, key string) (Value, bool, error) {
	return directFrameStringFieldBox(value, key, nil)
}

func directFrameArrayIndexInBounds(number float64, arrayLen int) (int, bool) {
	if !(number >= 1 && number <= float64(arrayLen)) {
		return 0, false
	}
	index := int(number)
	if float64(index) != number {
		return 0, false
	}
	return index, true
}

// directFrameRawStringField keeps the VM's common text-key lookup local. It
// avoids constructing a general probe for inline fields while retaining the
// hash sidecar fallback for overflow storage.
func directFrameRawStringField(table *Table, key string) (Value, bool) {
	if table == nil {
		return NilValue(), false
	}
	for i := range table.stringFields {
		field := &table.stringFields[i]
		if !field.value.IsNil() && field.key == key {
			return field.value, true
		}
	}
	if !table.hasStringOverflow() {
		return NilValue(), false
	}
	fields := table.hashFields()
	if fields == nil {
		return NilValue(), false
	}
	return fields.get(tableKey{kind: StringKind, str: key, strHash: hashString(key)})
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
		field, ok = directFrameRawStringField(table, key)
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
	if field, ok := directFrameRawStringField(table, key); ok {
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
	} else if field, ok := directFrameRawStringField(leftTable, leftKey); ok {
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
	} else if field, ok := directFrameRawStringField(rightTable, rightKey); ok {
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
	reuse := frame.openResults.values
	if frame.openResults.borrowed {
		reuse = nil
	}
	frame.clearOpenResultState()
	if count < 0 {
		frame.openResultStart = start
		frame.openResults = vmBorrowedResultWindow(results).retainedAdjustedWindow(reuse)
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

// directFrameApplySingleCallIslandResult publishes a native result without
// constructing an escaping one-element []Value. The open-result form stays in
// the result window's inline storage; fixed multi-result destinations are
// nil-filled directly in the register file.
func directFrameApplySingleCallIslandResult(frame *vmFrame, registers []Value, start int, count int, result Value) {
	frame.clearOpenResultState()
	if count < 0 {
		frame.openResultStart = start
		frame.openResults = vmSingleResultWindow(result)
		registers[start] = result
		return
	}
	if count == 0 {
		count = 1
	}
	registers[start] = result
	for i := 1; i < count; i++ {
		registers[start+i] = NilValue()
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
			args = make([]Value, 1+frame.varargLen())
			args[0] = StringValue("#")
			copy(args[1:], frame.varargValues())
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
		directFrameApplySingleCallIslandResult(frame, registers, start, resultCount, removed)
	case nativeFuncMathMin:
		if resultCount != 1 {
			return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
		}
		minimum, err := baseMathMinValue(registers[start : start+argCount])
		if err != nil {
			return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
		}
		directFrameApplySingleCallIslandResult(frame, registers, start, resultCount, NumberValue(minimum))
	case nativeFuncRawLen:
		value, err := baseRawLenValue(registers[start : start+argCount])
		if err != nil {
			return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
		}
		directFrameApplySingleCallIslandResult(frame, registers, start, resultCount, value)
	case nativeFuncSelect:
		directFrameApplySingleCallIslandResult(frame, registers, start, resultCount, NumberValue(float64(frame.varargLen())))
	default:
		return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
	}
	return directFrameResume()
}

func (thread *vmThread) runColdFastCall(frame *vmFrame, nativeID nativeFuncID, start int, argCount int, resultCount int) (vmFrameResult, bool, error) {
	destination := vmResultDestination{register: start, count: resultCount}
	frame.clearOpenResultState()
	if nativeID == nativeFuncSelect {
		callee, nativeUnchanged, err := fastCallCallee(thread.globals, nativeID)
		if err != nil {
			return vmFrameResult{}, true, err
		}
		if nativeUnchanged {
			frame.applyInlineResultDestination(destination, [2]Value{NumberValue(float64(frame.varargLen()))}, 1)
			return vmFrameResult{}, false, nil
		}
		args := make([]Value, 1+frame.varargLen())
		args[0] = StringValue("#")
		copy(args[1:], frame.varargValues())
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
		pointer := entry.table == table && match == dynamicStringIndexKeyPointer
		if field, ok := directFrameInlineCachedStringField(table, entry.slot, pointer); ok && !field.value.IsNil() {
			counts.addPointerHit()
			counts.addHit(i)
			return field.value, true
		}
		value, ok, indexed := directFrameReadCachedStringSlot(table, entry.slot, key, pointer)
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
		pointer := entry.table == table && match == dynamicStringIndexKeyPointer
		if field, ok := directFrameInlineCachedStringField(table, entry.slot, pointer); ok && !field.value.IsNil() {
			return field.value, true
		}
		value, ok, _ := directFrameReadCachedStringSlot(table, entry.slot, key, pointer)
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
		pointer := entry.table == table && match == dynamicStringIndexKeyPointer
		if field, ok := directFrameInlineCachedStringField(table, entry.slot, pointer); ok && !field.value.IsNil() {
			field.value = value
			table.stringValueVersion++
			counts.addPointerHit()
			counts.addHit(i)
			return true
		}
		ok, indexed := directFrameWriteCachedStringSlot(table, entry.slot, key, value, pointer)
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
		pointer := entry.table == table && match == dynamicStringIndexKeyPointer
		if field, ok := directFrameInlineCachedStringField(table, entry.slot, pointer); ok && !field.value.IsNil() {
			field.value = value
			table.stringValueVersion++
			return true
		}
		if ok, _ := directFrameWriteCachedStringSlot(table, entry.slot, key, value, pointer); ok {
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

// directFrameInlineCachedStringField is the common monomorphic row path. A
// pointer-key hit on the same table needs only the cached layout proof and the
// live inline slot; hash-backed and polymorphic cases keep the full validator.
func directFrameInlineCachedStringField(table *Table, slot tableStringFieldSlot, pointer bool) (*tableStringField, bool) {
	if table == nil || !pointer || slot.token.storage != 0 ||
		slot.token.layout != table.stringVersion ||
		slot.token.metatable != table.metatable ||
		(table.cold != nil && table.cold.stringHashCount > 0) ||
		slot.index < 0 || slot.index >= len(table.stringFields) {
		return nil, false
	}
	return &table.stringFields[slot.index], true
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

// directFrameExitAt publishes the local dispatch PC only when a direct loop
// leaves its hot path. Keeping this write at the exit seam avoids touching the
// frame on every production instruction while preserving cold-loop resumption.
func directFrameExitAt(frame *vmFrame, pc int, exit directFrameSideExit) directFrameSideExit {
	frame.pc = pc
	return exit
}

// wordcodeDispatch is the scalar view used by the cold side-exit helper. It
// deliberately does not materialize an instruction: the helper decodes the
// primary word and optional AUX payload directly into register-sized operands.
// Branch operands are returned as physical word PCs so callers can keep
// frame.pc in the same coordinate system as the stream.
type wordcodeDispatch struct {
	op       opcode
	a, b, c  int
	d        int
	cacheID  uint32
	nextWord int
}

func decodeWordcodeDispatch(words []wordcodeWord, pc int, indexes ...*wordcodeCacheIndex) (wordcodeDispatch, error) {
	if len(indexes) > 1 {
		return wordcodeDispatch{}, fmt.Errorf("wordcode dispatch received %d cache indexes", len(indexes))
	}
	var cacheIndex *wordcodeCacheIndex
	if len(indexes) != 0 {
		cacheIndex = indexes[0]
	}
	if pc < 0 || pc >= len(words) {
		return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d out of range", pc)
	}
	raw := words[pc]
	rawOp := uint8(raw)
	hasAux := raw&wordcodeAuxBit != 0
	op := opcode(rawOp & uint8(wordcodeOpcodeMask))
	meta, ok := opcodeMetadata(op)
	if !ok {
		return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d has unknown opcode byte 0x%02x", pc, rawOp)
	}
	if meta.wordcode.aux == wordcodeAuxNone && hasAux {
		return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d %s has unexpected AUX", pc, opcodeName(op))
	}
	if meta.wordcode.auxRequired && !hasAux {
		return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d %s is missing AUX", pc, opcodeName(op))
	}
	dispatch := wordcodeDispatch{op: op}
	switch meta.wordcode.format {
	case wordcodeFormatABC:
		dispatch.a = wordcodeDecodeByte(op, wordcodeSlotA, raw>>8)
		dispatch.b = wordcodeDecodeByte(op, wordcodeSlotB, raw>>16)
		dispatch.c = wordcodeDecodeByte(op, wordcodeSlotC, raw>>24)
		if wordcodeCacheableOpcode(op) {
			switch op {
			case opSetStringFieldIndex:
				dispatch.d, dispatch.c, dispatch.b = dispatch.c, dispatch.b, 0
			case opGetStringFieldIndex:
				dispatch.d, dispatch.c = dispatch.c, 0
			}
		}
	case wordcodeFormatAD:
		dispatch.a = wordcodeDecodeByte(op, wordcodeSlotA, raw>>8)
		value := wordcodeDecodeAD(op, meta.wordcode.adOperand, raw>>16)
		setWordcodeDispatchSlot(&dispatch, meta.wordcode.adOperand, value)
	case wordcodeFormatE:
		setWordcodeDispatchSlot(&dispatch, opcodeJumpTargetSlotToWordcode(opcodeJumpTarget(op)), wordcodeDecodeE(raw>>8))
	default:
		return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d %s has unsupported format %d", pc, opcodeName(op), meta.wordcode.format)
	}
	nextWord := pc + 1
	if hasAux {
		if nextWord >= len(words) {
			return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d %s AUX is truncated", pc, opcodeName(op))
		}
		aux := words[nextWord]
		switch meta.wordcode.aux {
		case wordcodeAuxA, wordcodeAuxB, wordcodeAuxC, wordcodeAuxD:
			setWordcodeDispatchSlot(&dispatch, wordcodeAuxSlot(meta.wordcode.aux), int(int32(aux)))
		case wordcodeAuxBC16:
			dispatch.b = wordcodeDecodeAD(op, wordcodeSlotB, aux)
			dispatch.c = wordcodeDecodeAD(op, wordcodeSlotC, aux>>16)
		case wordcodeAuxAC16:
			dispatch.a = wordcodeDecodeAD(op, wordcodeSlotA, aux)
			dispatch.c = wordcodeDecodeAD(op, wordcodeSlotC, aux>>16)
		case wordcodeAuxBD16:
			dispatch.b = wordcodeDecodeAD(op, wordcodeSlotB, aux)
			dispatch.d = wordcodeDecodeAD(op, wordcodeSlotD, aux>>16)
		case wordcodeAuxCD16:
			dispatch.c = wordcodeDecodeAD(op, wordcodeSlotC, aux)
			dispatch.d = wordcodeDecodeAD(op, wordcodeSlotD, aux>>16)
		default:
			return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d %s has unsupported AUX mode %d", pc, opcodeName(op), meta.wordcode.aux)
		}
		nextWord++
	}
	if jumpTarget := opcodeJumpTarget(op); jumpTarget != opcodeJumpTargetNone {
		switch jumpTarget {
		case opcodeJumpTargetB:
			dispatch.b += nextWord
		case opcodeJumpTargetD:
			dispatch.d += nextWord
		}
	}
	if wordcodeCacheableOpcode(op) && cacheIndex != nil {
		cacheID, descriptor, ok := cacheIndex.cacheSiteAt(pc)
		if !ok {
			return wordcodeDispatch{}, fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op))
		}
		dispatch.cacheID = cacheID
		if descriptor != wordcodeCacheDynamicConstant {
			switch op {
			case opSetStringField, opSetStringFieldIndex:
				dispatch.b = descriptor
			case opGetStringField, opGetStringFieldIndex:
				dispatch.c = descriptor
			}
		}
	}
	dispatch.nextWord = nextWord
	return dispatch, nil
}

func setWordcodeDispatchSlot(dispatch *wordcodeDispatch, slot wordcodeOperandSlot, value int) {
	switch slot {
	case wordcodeSlotA:
		dispatch.a = value
	case wordcodeSlotB:
		dispatch.b = value
	case wordcodeSlotC:
		dispatch.c = value
	case wordcodeSlotD:
		dispatch.d = value
	}
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

// vmFrameRecordArgumentCleanupEnd extends the child-window cleanup boundary
// for open-argument records. A callee may accept fewer parameters than the
// forwarded dynamic tail; those ignored owner slots sit beyond the physical
// child registers and must still be cleared before the shared stack is
// truncated.
func vmFrameRecordArgumentCleanupEnd(record vmFrameRecord, childEnd int) (int, bool) {
	if record.flags&vmFrameRecordFlagOpenArguments == 0 {
		return childEnd, true
	}
	base, baseOK := vmFrameRecordUint32ToInt(record.argumentBase)
	count, countOK := vmFrameRecordUint32ToInt(uint32(record.argumentCount))
	if !baseOK || !countOK || base < 0 || count < 0 || base > int(^uint(0)>>1)-count {
		return 0, false
	}
	end := base + count
	if end > childEnd {
		childEnd = end
	}
	return childEnd, true
}

// vmFrameCleanupEnd extends record cleanup through a variadic callee's
// owner-backed tail. Record metadata deliberately stays 48 bytes, so the
// active frame carries this transient range while it is executing.
func vmFrameCleanupEnd(record vmFrameRecord, frame *vmFrame, owner *vmStackOwner, childEnd int) (int, bool) {
	cleanupEnd, ok := vmFrameRecordArgumentCleanupEnd(record, childEnd)
	if !ok || frame == nil || frame.varargOwner != owner || frame.varargCount <= 0 {
		return cleanupEnd, ok
	}
	base := frame.varargBase
	count := frame.varargCount
	if base < 0 || count < 0 || base > int(^uint(0)>>1)-count {
		return 0, false
	}
	if end := base + count; end > cleanupEnd {
		cleanupEnd = end
	}
	return cleanupEnd, true
}

func clearOwnerRangeExcept(owner *vmStackOwner, start, end, preserveStart, preserveEnd int) {
	if owner == nil {
		return
	}
	if start < 0 {
		start = 0
	}
	if end > len(owner.values) {
		end = len(owner.values)
	}
	if start >= end {
		return
	}
	if preserveEnd <= start || preserveStart >= end {
		clear(owner.values[start:end])
		return
	}
	if preserveStart > start {
		clear(owner.values[start:preserveStart])
	}
	if preserveEnd < end {
		clear(owner.values[preserveEnd:end])
	}
}

// clearDroppedFrameRecordArguments releases every borrowed argument range
// owned by a physical frame before its compact records are truncated. Normal
// returns clear the range in the result-resume helpers; this path covers
// errors, protected recovery, and other abnormal unwinds where no result
// application runs. The record's destination is outside its argument range
// by construction, so clearing the complete range cannot erase a live result.
func (thread *vmThread) clearDroppedFrameRecordArguments(frame *vmFrame) {
	if thread == nil || frame == nil || frame.recordBaseDepth < 0 || frame.recordBaseDepth >= len(thread.frameRecords) {
		return
	}
	owner := thread.stackOwner
	if owner == nil {
		return
	}
	for _, record := range thread.frameRecords[frame.recordBaseDepth:] {
		if record.flags&vmFrameRecordFlagRecordOnly == 0 {
			continue
		}
		base, baseOK := vmFrameRecordUint32ToInt(record.argumentBase)
		count, countOK := vmFrameRecordUint32ToInt(uint32(record.argumentCount))
		if !baseOK || !countOK || base < 0 || count <= 0 || base > len(owner.values) || count > len(owner.values)-base {
			continue
		}
		clear(owner.values[base : base+count])
	}
	if frame.varargOwner == owner && frame.varargCount > 0 {
		frame.clearVarargStorage()
	}
}

func (thread *vmThread) resumeRecordOnlyFixedCallOne(rootRecordDepth int, frame **vmFrame, value Value) bool {
	if thread == nil || frame == nil || *frame == nil || len(thread.frameRecords) <= rootRecordDepth {
		return false
	}
	record := thread.frameRecords[len(thread.frameRecords)-1]
	if record.flags&vmFrameRecordFlagRecordOnly == 0 || record.closure == nil || record.resultCount != 1 {
		return false
	}
	base, baseOK := vmFrameRecordUint32ToInt(record.base)
	top, topOK := vmFrameRecordUint32ToInt(record.top)
	returnPC, returnPCOK := vmFrameRecordUint32ToInt(record.returnPC)
	destination, destinationOK := vmFrameRecordUint32ToInt(record.resultDestination)
	previousStackLength, previousStackLengthOK := vmFrameRecordUint32ToInt(record.varargBase)
	if !baseOK || !topOK || !returnPCOK || !destinationOK || !previousStackLengthOK ||
		record.closure == nil || record.closure.proto == nil ||
		base < 0 || top < base || destination < base || destination >= top {
		return false
	}
	current := *frame
	owner := current.window.owner
	if owner == nil {
		owner = current.owner
	}
	if owner == nil || owner != thread.stackOwner || top > len(owner.values) {
		return false
	}
	childStart := current.window.base
	childEnd := childStart + current.window.length
	if childStart < 0 {
		childStart = 0
	}
	if childEnd > len(owner.values) {
		childEnd = len(owner.values)
	}
	cleanupEnd, cleanupOK := vmFrameCleanupEnd(record, current, owner, childEnd)
	if !cleanupOK {
		return false
	}
	if childStart < cleanupEnd {
		thread.closeOpenUpvalues(owner, childStart, childEnd)
		clearOwnerRangeExcept(owner, childStart, cleanupEnd, destination, destination+1)
	}
	owner.values = owner.values[:top]
	thread.stack = owner.values
	_, _ = thread.popFrameRecord()

	current.proto = record.closure.proto
	current.currentClosure = record.closure
	current.registerBase = base
	current.registerCount = top - base
	current.owner = owner
	current.registers = owner.values[base:top]
	current.cells = thread.restoreFrameCells(current, record.closure.proto, owner, base)
	current.upvalues = record.closure.upvalues
	current.upvalueValues = record.closure.upvalueValues
	current.upvalueValueOK = record.closure.upvalueValueOK
	current.varargOwner = nil
	current.varargBase = 0
	current.varargCount = 0
	current.pc = returnPC
	current.debugLine = -1
	current.openResultStart = -1
	current.openResults = vmResultWindow{}
	current.openRangeOwner = nil
	current.openRangeBase = -1
	current.openRangeCount = 0
	current.openRangeLogicalTop = -1
	current.resetPendingCallState()
	current.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              top - base,
		previousStackLength: previousStackLength,
		borrowed:            record.flags&vmFrameRecordFlagCallerBorrowed != 0,
	}
	if current.recordBaseDepth >= 0 && len(thread.frameRecords) == current.recordBaseDepth {
		current.recordBaseDepth = -1
	}
	current.setRegister(destination-base, value)
	return true
}

// resumeRecordOnlyOpenCall transfers a callee's fixed prefix plus its dynamic
// open-result range into the caller's owner-backed open window. The caller's
// static register frame ends at record.top; dynamic results live immediately
// after that boundary, while A receives the first value for ordinary register
// reads. The temporary stack tail is used only when source and destination
// ranges overlap in a way that would corrupt a later source segment.
func (thread *vmThread) resumeRecordOnlyOpenCall(rootRecordDepth int, frame **vmFrame, sourceRegister, sourceCount int, openResults *vmResultWindow) bool {
	if thread == nil || frame == nil || *frame == nil || len(thread.frameRecords) <= rootRecordDepth || sourceRegister < 0 || sourceCount < 0 {
		return false
	}
	record := thread.frameRecords[len(thread.frameRecords)-1]
	if record.flags&vmFrameRecordFlagRecordOnly == 0 || record.flags&vmFrameRecordFlagOpenResults == 0 || record.closure == nil || record.closure.proto == nil || record.resultCount != ^uint32(0) {
		return false
	}
	base, baseOK := vmFrameRecordUint32ToInt(record.base)
	top, topOK := vmFrameRecordUint32ToInt(record.top)
	returnPC, returnPCOK := vmFrameRecordUint32ToInt(record.returnPC)
	destination, destinationOK := vmFrameRecordUint32ToInt(record.resultDestination)
	previousStackLength, previousStackLengthOK := vmFrameRecordUint32ToInt(record.varargBase)
	if !baseOK || !topOK || !returnPCOK || !destinationOK || !previousStackLengthOK || base < 0 || top < base || destination < base || destination >= top {
		return false
	}
	current := *frame
	owner := current.window.owner
	if owner == nil {
		owner = current.owner
	}
	if owner == nil || owner != thread.stackOwner {
		return false
	}
	childStart := current.window.base
	childEnd := childStart + current.window.length
	if childStart < 0 || childEnd < childStart || childEnd > len(owner.values) || sourceRegister > current.window.length || sourceCount > current.window.length-sourceRegister {
		return false
	}
	cleanupEnd, cleanupOK := vmFrameCleanupEnd(record, current, owner, childEnd)
	if !cleanupOK {
		return false
	}
	sourceStart := childStart + sourceRegister
	if sourceStart < childStart || sourceStart > len(owner.values)-sourceCount {
		return false
	}
	window := vmResultWindow{}
	if openResults != nil {
		window = *openResults
	}
	openCount := window.len()
	openBase := -1
	if openResults != nil && current.openRangeOwner == owner && current.openRangeBase >= 0 && current.openRangeCount == openCount && current.openRangeBase <= len(owner.values)-openCount {
		openBase = current.openRangeBase
	}
	total := sourceCount + openCount
	if total < sourceCount {
		return false
	}
	if total == 0 {
		total = 1
	}
	targetBase := top
	if targetBase < 0 || total > int(^uint(0)>>1)-targetBase {
		return false
	}
	targetEnd := targetBase + total
	rangesOverlap := func(left, leftCount, right, rightCount int) bool {
		if leftCount <= 0 || rightCount <= 0 {
			return false
		}
		return left < right+rightCount && right < left+leftCount
	}
	needsTemp := rangesOverlap(targetBase, total, sourceStart, sourceCount) || rangesOverlap(targetBase, total, openBase, openCount)
	thread.closeOpenUpvalues(owner, childStart, childEnd)
	tempBase := -1
	if needsTemp {
		tempBase = len(owner.values)
		if tempBase < targetEnd {
			tempBase = targetEnd
		}
		if total > int(^uint(0)>>1)-tempBase {
			return false
		}
		thread.growStack(tempBase + total)
		owner = thread.stackOwner
		if owner == nil || tempBase+total > len(owner.values) {
			return false
		}
		for index := 0; index < sourceCount; index++ {
			owner.values[tempBase+index] = owner.values[sourceStart+index]
		}
		for index := 0; index < openCount; index++ {
			if openBase >= 0 {
				owner.values[tempBase+sourceCount+index] = owner.values[openBase+index]
			} else {
				owner.values[tempBase+sourceCount+index] = window.at(index)
			}
		}
		if sourceCount+openCount == 0 {
			owner.values[tempBase] = NilValue()
		}
		copy(owner.values[targetBase:targetEnd], owner.values[tempBase:tempBase+total])
	} else {
		if targetEnd > len(owner.values) {
			thread.growStack(targetEnd)
		}
		owner = thread.stackOwner
		if owner == nil || targetEnd > len(owner.values) {
			return false
		}
		for index := 0; index < sourceCount; index++ {
			owner.values[targetBase+index] = owner.values[sourceStart+index]
		}
		for index := 0; index < openCount; index++ {
			if openBase >= 0 {
				owner.values[targetBase+sourceCount+index] = owner.values[openBase+index]
			} else {
				owner.values[targetBase+sourceCount+index] = window.at(index)
			}
		}
		if sourceCount+openCount == 0 {
			owner.values[targetBase] = NilValue()
		}
	}
	clearOwnerRangeExcept(owner, childStart, cleanupEnd, targetBase, targetEnd)
	if openBase >= 0 {
		clearOwnerRangeExcept(owner, openBase, openBase+openCount, targetBase, targetEnd)
	}
	if tempBase >= 0 {
		clear(owner.values[tempBase : tempBase+total])
	}
	owner.values = owner.values[:targetEnd]
	thread.stack = owner.values
	_, _ = thread.popFrameRecord()

	current.proto = record.closure.proto
	current.currentClosure = record.closure
	current.registerBase = base
	current.registerCount = top - base
	current.owner = owner
	current.registers = owner.values[base:top]
	current.cells = thread.restoreFrameCells(current, record.closure.proto, owner, base)
	current.upvalues = record.closure.upvalues
	current.upvalueValues = record.closure.upvalueValues
	current.upvalueValueOK = record.closure.upvalueValueOK
	current.varargOwner = nil
	current.varargBase = 0
	current.varargCount = 0
	current.pc = returnPC
	current.debugLine = -1
	current.openResultStart = destination - base
	current.openResults = vmResultWindow{}
	current.openRangeOwner = owner
	current.openRangeBase = targetBase
	current.openRangeCount = total
	current.openRangeLogicalTop = targetBase
	current.resetPendingCallState()
	current.window = vmRegisterWindow{owner: owner, base: base, length: top - base, previousStackLength: previousStackLength, borrowed: record.flags&vmFrameRecordFlagCallerBorrowed != 0}
	if current.recordBaseDepth >= 0 && len(thread.frameRecords) == current.recordBaseDepth {
		current.recordBaseDepth = -1
	}
	current.setRegister(destination-base, owner.values[targetBase])
	return true
}

// resumeRecordOnlyFixedCall copies a fixed result range directly between the
// shared slot stack and the caller destination, then restores the caller in
// the same physical frame.  The copy is overlap-safe because compiler
// liveness marks prove the destination starts before the borrowed child
// window; nil-fill covers short returns without constructing a result window.
func (thread *vmThread) resumeRecordOnlyFixedCall(rootRecordDepth int, frame **vmFrame, sourceRegister, sourceCount int, openResults *vmResultWindow) bool {
	if thread == nil || frame == nil || *frame == nil || len(thread.frameRecords) <= rootRecordDepth || sourceRegister < 0 || sourceCount < 0 {
		return false
	}
	record := thread.frameRecords[len(thread.frameRecords)-1]
	if record.flags&vmFrameRecordFlagOpenResults != 0 && record.resultCount == ^uint32(0) {
		return thread.resumeRecordOnlyOpenCall(rootRecordDepth, frame, sourceRegister, sourceCount, openResults)
	}
	if record.flags&vmFrameRecordFlagRecordOnly == 0 || record.closure == nil || record.closure.proto == nil || record.resultCount < 2 {
		return false
	}
	base, baseOK := vmFrameRecordUint32ToInt(record.base)
	top, topOK := vmFrameRecordUint32ToInt(record.top)
	returnPC, returnPCOK := vmFrameRecordUint32ToInt(record.returnPC)
	destination, destinationOK := vmFrameRecordUint32ToInt(record.resultDestination)
	previousStackLength, previousStackLengthOK := vmFrameRecordUint32ToInt(record.varargBase)
	requested, requestedOK := vmFrameRecordUint32ToInt(record.resultCount)
	if !baseOK || !topOK || !returnPCOK || !destinationOK || !previousStackLengthOK || !requestedOK ||
		requested < 2 || base < 0 || top < base || destination < base || requested > top-destination {
		return false
	}
	current := *frame
	owner := current.window.owner
	if owner == nil {
		owner = current.owner
	}
	if owner == nil || owner != thread.stackOwner || top > len(owner.values) {
		return false
	}
	sourceStart := current.window.base + sourceRegister
	if sourceStart < current.window.base || sourceRegister > current.window.length || sourceCount > current.window.length-sourceRegister || sourceStart > len(owner.values)-sourceCount {
		return false
	}
	childStart := current.window.base
	childEnd := childStart + current.window.length
	if childStart < 0 || childEnd < childStart || childEnd > len(owner.values) {
		return false
	}
	cleanupEnd, cleanupOK := vmFrameCleanupEnd(record, current, owner, childEnd)
	if !cleanupOK {
		return false
	}
	thread.closeOpenUpvalues(owner, childStart, childEnd)
	available := sourceCount
	if openResults != nil {
		available += openResults.len()
	}
	copyCount := available
	if copyCount > requested {
		copyCount = requested
	}
	if openResults == nil {
		copy(owner.values[destination:destination+copyCount], owner.values[sourceStart:sourceStart+copyCount])
	} else {
		for index := 0; index < copyCount; index++ {
			if index < sourceCount {
				owner.values[destination+index] = owner.values[sourceStart+index]
			} else {
				owner.values[destination+index] = openResults.at(index - sourceCount)
			}
		}
	}
	for index := copyCount; index < requested; index++ {
		owner.values[destination+index] = NilValue()
	}
	// Release the child window while preserving any caller destination slots
	// that overlap it. Open-argument records extend cleanup through the full
	// forwarded tail, including arguments beyond the callee's register count.
	clearOwnerRangeExcept(owner, childStart, cleanupEnd, destination, destination+requested)
	owner.values = owner.values[:top]
	thread.stack = owner.values
	_, _ = thread.popFrameRecord()

	current.proto = record.closure.proto
	current.currentClosure = record.closure
	current.registerBase = base
	current.registerCount = top - base
	current.owner = owner
	current.registers = owner.values[base:top]
	current.cells = thread.restoreFrameCells(current, record.closure.proto, owner, base)
	current.upvalues = record.closure.upvalues
	current.upvalueValues = record.closure.upvalueValues
	current.upvalueValueOK = record.closure.upvalueValueOK
	current.varargOwner = nil
	current.varargBase = 0
	current.varargCount = 0
	current.pc = returnPC
	current.debugLine = -1
	current.openResultStart = -1
	current.openResults = vmResultWindow{}
	current.openRangeOwner = nil
	current.openRangeBase = -1
	current.openRangeCount = 0
	current.openRangeLogicalTop = -1
	current.resetPendingCallState()
	current.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              top - base,
		previousStackLength: previousStackLength,
		borrowed:            record.flags&vmFrameRecordFlagCallerBorrowed != 0,
	}
	if current.recordBaseDepth >= 0 && len(thread.frameRecords) == current.recordBaseDepth {
		current.recordBaseDepth = -1
	}
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
	if record, ok := thread.peekFrameRecord(*frame); ok {
		absoluteDestination, destinationOK := vmFrameRecordUint32ToInt(record.resultDestination)
		returnPC, returnPCOK := vmFrameRecordUint32ToInt(record.returnPC)
		if !destinationOK || !returnPCOK || record.resultCount != 1 ||
			absoluteDestination < caller.registerBase || absoluteDestination-caller.registerBase >= caller.registerCount {
			return false
		}
		destination.register = absoluteDestination - caller.registerBase
		caller.pc = returnPC
	}
	thread.popFrame()
	caller.pendingCall = vmPendingCall{}
	caller.hasPendingCall = false
	caller.clearOpenResultState()
	caller.setRegister(destination.register, value)
	*frame = caller
	return true
}

func vmFrameRecordUint32ToInt(value uint32) (int, bool) {
	if uint64(value) > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(value), true
}

func runDirectFrameInstrumentedLoop(thread *vmThread, frameRef **vmFrame) directFrameSideExit {
	frame := *frameRef
	var pc int
	defer func() { frame.pc = pc }()
	rootDepth := len(thread.frames) - 1
	rootRecordDepth := len(thread.frameRecords)
	if frame.recordBaseDepth >= 0 {
		rootRecordDepth = frame.recordBaseDepth
	}
	var (
		proto                *Proto
		functionInstance     *vmFunctionInstance
		words                []wordcodeWord
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
	directChildActive = len(thread.frames)-1 > rootDepth || len(thread.frameRecords) > rootRecordDepth
	proto = frame.proto
	functionInstance = nil
	if proto.verifyErr != nil {
		return directFrameExitAt(frame, frame.pc, directFrameFail(fmt.Errorf("run: invalid prototype: %w", proto.verifyErr)))
	}
	words = proto.words
	constants = proto.constants
	constantKeys = proto.constantKeys
	constantKeyOK = proto.constantKeyOK
	constantNumbers = proto.constantNumbers
	constantNumberOK = proto.constantNumberOK
	registers = frame.registers
	pc = frame.pc
	picCounts = thread.directFramePICCounts
	runLineHook = thread.debugHook != nil && thread.debugLineHook
	runCountHook = thread.debugCountInterval > 0 && thread.debugHook != nil
	runInstructionBudget = thread.instructionBudget >= 0

	for uint(pc) < uint(len(words)) {
		if runInstructionBudget || runLineHook || runCountHook {
			frame.pc = pc
		}
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
		raw := words[pc]
		rawOp := uint8(raw)
		hasAux := raw&wordcodeAuxBit != 0
		op := opcode(rawOp & uint8(wordcodeOpcodeMask))
		wordMeta, ok := opcodeMetadata(op)
		if !ok {
			return directFrameFail(fmt.Errorf("wordcode pc %d has unknown opcode byte 0x%02x", pc, rawOp))
		}
		encoding := wordMeta.wordcode
		if encoding.aux == wordcodeAuxNone && hasAux {
			return directFrameFail(fmt.Errorf("wordcode pc %d %s has unexpected AUX", pc, opcodeName(op)))
		}
		if encoding.auxRequired && !hasAux {
			return directFrameFail(fmt.Errorf("wordcode pc %d %s is missing AUX", pc, opcodeName(op)))
		}
		a, b, c, d := 0, 0, 0, 0
		switch encoding.format {
		case wordcodeFormatABC:
			a = wordcodeDecodeByte(op, wordcodeSlotA, raw>>8)
			b = wordcodeDecodeByte(op, wordcodeSlotB, raw>>16)
			c = wordcodeDecodeByte(op, wordcodeSlotC, raw>>24)
		case wordcodeFormatAD:
			a = wordcodeDecodeByte(op, wordcodeSlotA, raw>>8)
			value := wordcodeDecodeAD(op, encoding.adOperand, raw>>16)
			switch encoding.adOperand {
			case wordcodeSlotA:
				a = value
			case wordcodeSlotB:
				b = value
			case wordcodeSlotC:
				c = value
			case wordcodeSlotD:
				d = value
			}
		case wordcodeFormatE:
			value := wordcodeDecodeE(raw >> 8)
			switch opcodeJumpTarget(op) {
			case opcodeJumpTargetB:
				b = value
			case opcodeJumpTargetD:
				d = value
			}
		default:
			return directFrameFail(fmt.Errorf("wordcode pc %d %s has unsupported format %d", pc, opcodeName(op), encoding.format))
		}
		nextWord := pc + 1
		if hasAux {
			if nextWord >= len(words) {
				return directFrameFail(fmt.Errorf("wordcode pc %d %s AUX is truncated", pc, opcodeName(op)))
			}
			aux := words[nextWord]
			switch encoding.aux {
			case wordcodeAuxA:
				a = int(int32(aux))
			case wordcodeAuxB:
				b = int(int32(aux))
			case wordcodeAuxC:
				c = int(int32(aux))
			case wordcodeAuxD:
				d = int(int32(aux))
			case wordcodeAuxBC16:
				b = wordcodeDecodeAD(op, wordcodeSlotB, aux)
				c = wordcodeDecodeAD(op, wordcodeSlotC, aux>>16)
			case wordcodeAuxAC16:
				a = wordcodeDecodeAD(op, wordcodeSlotA, aux)
				c = wordcodeDecodeAD(op, wordcodeSlotC, aux>>16)
			case wordcodeAuxBD16:
				b = wordcodeDecodeAD(op, wordcodeSlotB, aux)
				d = wordcodeDecodeAD(op, wordcodeSlotD, aux>>16)
			case wordcodeAuxCD16:
				c = wordcodeDecodeAD(op, wordcodeSlotC, aux)
				d = wordcodeDecodeAD(op, wordcodeSlotD, aux>>16)
			default:
				return directFrameFail(fmt.Errorf("wordcode pc %d %s has unsupported AUX mode %d", pc, opcodeName(op), encoding.aux))
			}
			nextWord++
		}
		switch opcodeJumpTarget(op) {
		case opcodeJumpTargetB:
			b += nextWord
		case opcodeJumpTargetD:
			d += nextWord
		}
		if counts := thread.directFrameOpcodeCounts; counts != nil {
			counts[uint8(op)]++
		}
		if pcCounts := thread.directFramePCCounts; pcCounts != nil {
			perProto := pcCounts[proto]
			if perProto == nil {
				perProto = make([]uint64, len(words))
				pcCounts[proto] = perProto
			}
			perProto[pc]++
		}
		switch op {
		case opLoadConst:
			registers[a] = constants[b]

		case opLoadGlobal:
			name, _ := constants[b].String()
			value, ok, hit := thread.globals.getSlot(proto.globalSlot(c, name), name)
			if hit {
				picCounts.addGlobalSlotHit()
			} else {
				picCounts.addGlobalSlotMiss()
			}
			if !ok {
				return directFrameFail(fmt.Errorf("run: undefined global %q", name))
			}
			registers[a] = value

		case opSetGlobal:
			name, _ := constants[a].String()
			thread.globals.setSlot(proto.globalSlot(c, name), name, registers[b])

		case opNewTable:
			registers[a] = TableValue(newTableWithCapacity(b, c))

		case opMove:
			registers[a] = registers[b]

		case opGetUpvalue:
			value, err := frame.upvalue(b)
			if err != nil {
				return directFrameFail(err)
			}
			registers[a] = value

		case opSetUpvalue:
			if err := frame.setUpvalue(a, registers[b]); err != nil {
				return directFrameFail(err)
			}

		case opVararg:
			resultCount := b
			if resultCount == 0 {
				resultCount = 1
			}
			if resultCount < 0 {
				frame.clearOpenResultState()
				frame.openResultStart = a
				if !frame.publishOpenVarargRange(thread) {
					frame.openResults = vmAdjustedBorrowedResultWindow(frame.varargValues())
				}
				registers = frame.registers
				registers[a] = frame.openResultAt(0)
				pc = nextWord
				continue
			}
			frame.clearOpenResultState()
			for i := 0; i < resultCount; i++ {
				if i >= frame.varargLen() {
					registers[a+i] = NilValue()
				} else {
					registers[a+i] = frame.varargAt(i)
				}
			}

		case opSetField:
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[b], registers[c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			if constantKeyOK[b] {
				keyValue := constants[b]
				var err error
				if valueKind(keyValue) == StringKind {
					table.setRawStringFieldBox(keyValue.stringText(), keyValue.stringBox(), registers[c])
				} else {
					err = table.rawSetKey(constantKeys[b], registers[c])
				}
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				break
			}
			if err := table.rawSet(constants[b], registers[c]); err != nil {
				return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
			}

		case opSetStringField:
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op)))
			}
			b = descriptor
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[b], registers[c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			keyValue := constants[b]
			key := constantKeys[b].str
			value := registers[c]
			if valueKind(keyValue) == StringKind && !value.IsNil() {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
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
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op)))
			}
			d, c, b = c, b, descriptor
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind()))
			}
			firstKey := constantKeys[b].str
			firstBox := constants[b].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = directFrameRawStringField(table, firstKey)
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
			key := registers[c]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				value := registers[d]
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
			if err := nextTable.rawSet(key, registers[d]); err != nil {
				return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
			}

		case opGetStringField:
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op)))
			}
			c = descriptor
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			key := constants[c]
			keyText := constantKeys[c].str
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValueCounted(table, key, picCounts); ok {
					registers[a] = value
					break
				}
				slot, slotOK := table.rawStringFieldSlotBox(key.stringBox())
				if !slotOK {
					slot, slotOK = table.rawStringFieldSlot(keyText)
				}
				if slotOK {
					if value, ok := table.rawStringFieldAtSlot(slot, keyText); ok {
						cache.storeValue(table, key, slot)
						registers[a] = value
						break
					}
				}
			} else if value, ok := directFrameRawStringField(table, keyText); ok {
				// Keep the existing raw-string adapter as a defensive fallback for
				// malformed hand-built prototypes.
				registers[a] = value
				break
			}
			if table.metatable != nil {
				picCounts.addSideExit(directFrameSideExitReasonTable)
				value, ok, err := directFrameTableGetIsland(thread.globals, table, constants[c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: get field failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				registers[a] = value
				break
			}
			registers[a] = NilValue()

		case opGetStringFieldIndex:
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op)))
			}
			d, c = c, descriptor
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind()))
			}
			firstKey := constantKeys[c].str
			firstBox := constants[c].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = directFrameRawStringField(table, firstKey)
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
			key := registers[d]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValueCounted(nextTable, key, picCounts); ok {
					registers[a] = value
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok {
					value, ok := nextTable.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(nextTable, key, slot)
						registers[a] = value
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
			registers[a] = value

		case opSetIndex:
			cacheID, _, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op)))
			}
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: set index target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addMetatableMiss()
				picCounts.addSideExit(directFrameSideExitReasonTable)
				ok, err := directFrameTableSetIsland(thread.globals, table, registers[b], registers[c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				break
			}
			key := registers[b]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				value := registers[c]
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
			if err := table.rawSet(registers[b], registers[c]); err != nil {
				return directFrameFail(fmt.Errorf("run: set index failed: %w", err))
			}

		case opGetIndex:
			cacheID, _, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op)))
			}
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get index target is %s, want table", base.Kind()))
			}
			if table.metatable != nil {
				picCounts.addMetatableMiss()
				picCounts.addSideExit(directFrameSideExitReasonTable)
				value, ok, err := directFrameTableGetIsland(thread.globals, table, registers[c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
				}
				if !ok {
					return directFrameEnterGenericFrame()
				}
				registers[a] = value
				break
			}
			key := registers[c]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValueCounted(table, key, picCounts); ok {
					registers[a] = value
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok {
					value, ok := table.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(table, key, slot)
						registers[a] = value
						break
					}
				} else {
					picCounts.addMissingKeyFallback()
				}
			} else if valueKind(key) == NumberKind {
				if index, ok := directFrameArrayIndexInBounds(valueNumber(key), len(table.array)); ok {
					picCounts.addNumericArrayIndexHit()
					registers[a] = table.array[index-1]
					break
				}
				picCounts.addInvalidKeyFallback()
			} else {
				picCounts.addInvalidKeyFallback()
			}
			value, err := table.rawGet(key)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: get index failed: %w", err))
			}
			registers[a] = value

		case opClosure:
			child := proto.prototypes[b]
			captured := thread.captureUpvalues(child, frame)
			registers[a] = thread.functionValueWithCapturedUpvalues(child, captured)

		case opPrepareIter:
			iterValue := registers[a]
			iterTable := iterValue.tableRef()
			if iterTable != nil && iterTable.metatable == nil {
				if tableCanIterateCleanArray(iterTable) {
					registers[a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext)
					registers[b] = iterValue
					registers[c] = NilValue()
					break
				}
				registers[a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncTableNext)
				registers[b] = iterValue
				registers[c] = NilValue()
				break
			}
			generator, state, control, ok, err := prepareIterator(iterValue, thread.globals)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: prepare iterator failed: %w", err))
			}
			if ok {
				registers[a] = generator
				registers[b] = state
				registers[c] = control
			}

		case opArrayNext:
			callee := registers[b]
			var first Value
			var second Value
			var count int
			var ok bool
			var err error
			if valueNativeID(callee) == nativeFuncArrayNext {
				ok = true
				tableValue := registers[c]
				table := tableValue.tableRef()
				if table == nil {
					err = fmt.Errorf("array iterator: argument #1 is %s, want table", tableValue.Kind())
				} else {
					controlValue := registers[a]
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
				first, second, count, ok, err = directFrameIteratorNext(callee, registers[c], registers[a])
			}
			if !ok {
				return directFrameEnterGenericFrame()
			}
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			for i := 0; i < d; i++ {
				if i >= count {
					registers[a+i] = NilValue()
					continue
				}
				if i == 0 {
					registers[a+i] = first
				} else {
					registers[a+i] = second
				}
			}

		case opArrayNextJump2:
			callee := registers[b]
			if valueNativeID(callee) == nativeFuncArrayNext {
				tableValue := registers[c]
				table := tableValue.tableRef()
				if table == nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: argument #1 is %s, want table", tableValue.Kind()))
				}
				controlValue := registers[a]
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
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				next := index + 1
				if next < 1 || next > len(table.array) {
					registers[a] = NilValue()
					registers[a+1] = NilValue()
					pc = d
					continue
				}
				registers[a] = NumberValue(float64(next))
				registers[a+1] = table.array[next-1]
				break
			}
			first, second, count, ok, err := directFrameIteratorNext(callee, registers[c], registers[a])
			if !ok {
				return directFrameEnterGenericFrame()
			}
			if err != nil {
				return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			if count < 1 || first.IsNil() {
				registers[a] = NilValue()
				registers[a+1] = NilValue()
				pc = d
				continue
			}
			registers[a] = first
			if count > 1 {
				registers[a+1] = second
			} else {
				registers[a+1] = NilValue()
			}

		case opAdd:
			left := registers[b]
			right := registers[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) + valueNumber(right))

		case opSub:
			left := registers[b]
			right := registers[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - valueNumber(right))

		case opMul:
			left := registers[b]
			right := registers[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) * valueNumber(right))

		case opDiv:
			left := registers[b]
			right := registers[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) / valueNumber(right))

		case opMod:
			left := registers[b]
			right := registers[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/valueNumber(right))*valueNumber(right))

		case opIDiv:
			left := registers[b]
			right := registers[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Floor(valueNumber(left) / valueNumber(right)))

		case opPow:
			left := registers[b]
			right := registers[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Pow(valueNumber(left), valueNumber(right)))

		case opAddK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) + constantNumbers[c])

		case opSubK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - constantNumbers[c])

		case opMulK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) * constantNumbers[c])

		case opDivK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) / constantNumbers[c])

		case opModK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
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
				registers[a] = value
				break
			}
			right := constantNumbers[c]
			registers[a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/right)*right)

		case opIDivK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
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
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Floor(valueNumber(left) / constantNumbers[c]))

		case opNeg:
			operand := registers[b]
			if valueKind(operand) != NumberKind {
				value, err := directFrameUnaryArithmeticValue(picCounts, thread.globals, operand, negateValue)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: %w", err))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(-valueNumber(operand))

		case opLen:
			operand := registers[b]
			switch valueKind(operand) {
			case StringKind:
				registers[a] = NumberValue(float64(len(operand.stringText())))
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
					registers[a] = value
					break
				}
				length, err := table.rawLen()
				if err != nil {
					return directFrameFail(fmt.Errorf("run: length failed: %w", err))
				}
				registers[a] = NumberValue(float64(length))
			default:
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lengthValue(operand, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: length failed: %w", err))
				}
				registers[a] = value
			}

		case opConcat:
			left := registers[b]
			right := registers[c]
			if !directFrameRawConcatOperand(left) || !directFrameRawConcatOperand(right) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := concatValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
				}
				registers[a] = value
				break
			}
			concatValues := [2]Value{left, right}
			if value, ok := thread.internStringConcatValues(concatValues[:]); ok {
				registers[a] = value
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
			registers[a] = thread.internStringValue(leftText + rightText)

		case opConcatChain:
			if value, ok := thread.internStringConcatValues(registers[b : b+c]); ok {
				registers[a] = value
				break
			}
			text, ok, err := thread.concatRawChainString(registers[b : b+c])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
			}
			if !ok {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := concatChainValue(registers[b:b+c], thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: concat failed: %w", err))
				}
				registers[a] = value
				break
			}
			registers[a] = thread.internStringValue(text)

		case opEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valuesEqual(left, right))

		case opNotEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
				}
				registers[a] = BoolValue(!value)
				break
			}
			registers[a] = BoolValue(!valuesEqual(left, right))

		case opLess:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() < right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: less failed: %w", err))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) < valueNumber(right))

		case opLessEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() <= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessEqualValue(left, right, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: less equal failed: %w", err))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) <= valueNumber(right))

		case opGreater:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() > right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessValue(right, left, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) > valueNumber(right))

		case opGreaterEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() >= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
				value, err := lessEqualValue(right, left, thread.globals)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: greater equal failed: %w", err))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) >= valueNumber(right))

		case opNumericForCheck:
			loopValue := registers[a]
			limitValue := registers[b]
			stepValue := registers[c]
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
					pc = d
					continue
				}
				break
			}
			if valueNumber(loopValue) < valueNumber(limitValue) {
				pc = d
				continue
			}

		case opNumericForLoop:
			loopValue := registers[a]
			stepValue := registers[b]
			if valueKind(loopValue) != NumberKind || valueKind(stepValue) != NumberKind {
				return directFrameEnterGenericFrame()
			}
			registers[a] = NumberValue(valueNumber(loopValue) + valueNumber(stepValue))
			pc = d
			continue

		case opJumpIfNotEqualK:
			left := registers[a]
			if valueKind(left) == NumberKind && constantNumberOK[b] {
				if valueNumber(left) != constantNumbers[b] {
					pc = d
					continue
				}
				break
			}
			if valueKind(left) == StringKind && constantKeyOK[b] {
				if left.stringText() != constantKeys[b].str {
					pc = d
					continue
				}
				break
			}
			right := constants[b]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				picCounts.addSideExit(directFrameSideExitReasonMetatable)
			}
			equal, err := equalValue(left, right, thread.globals)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: equal failed: %w", err))
			}
			if !equal {
				pc = d
				continue
			}

		case opJumpIfTableHasMetatable:
			base := registers[a]
			if table := base.tableRef(); table != nil && table.metatable != nil {
				pc = d
				continue
			}

		case opJumpIfNotLessK:
			left := registers[a]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, constants[b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if !less {
				pc = d
				continue
			}

		case opJumpIfNotGreaterK:
			left := registers[a]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, constants[b], left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if !greater {
				pc = d
				continue
			}

		case opJumpIfLessK:
			left := registers[a]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, constants[b])
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if less {
				pc = d
				continue
			}

		case opJumpIfGreaterK:
			left := registers[a]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, constants[b], left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if greater {
				pc = d
				continue
			}

		case opJumpIfNotLess:
			left := registers[a]
			right := registers[b]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, right)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if !less {
				pc = d
				continue
			}

		case opJumpIfNotGreater:
			left := registers[a]
			right := registers[b]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, right, left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if !greater {
				pc = d
				continue
			}

		case opJumpIfLess:
			left := registers[a]
			right := registers[b]
			less, err := directFrameLessForBranch(picCounts, thread.globals, left, right)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: less failed: %w", err))
			}
			if less {
				pc = d
				continue
			}

		case opJumpIfGreater:
			left := registers[a]
			right := registers[b]
			greater, err := directFrameLessForBranch(picCounts, thread.globals, right, left)
			if err != nil {
				return directFrameFail(fmt.Errorf("run: greater failed: %w", err))
			}
			if greater {
				pc = d
				continue
			}

		case opJumpIfFalse:
			if !registers[a].truthy() {
				pc = b
				continue
			}

		case opJump:
			pc = b
			continue

		case opCall:
			resultCount, fixedMultiBorrow := decodeFixedMultiResultCount(d, frame.proto.registers)
			openResultBorrow := decodeOpenResultCallMarker(d)
			openArgumentPrefix, openArgumentMarker := decodeOpenArgumentCallMarker(c)
			if openResultBorrow {
				resultCount = -1
			}
			if openArgumentMarker {
				prefixCount, _ := decodeOpenArgumentCallMarker(c)
				c = -prefixCount - 1
			}
			if !fixedMultiBorrow && resultCount == 0 {
				resultCount = 1
			}
			callee := registers[b]
			if c == 2 && resultCount == 2 {
				first, second, count, ok, err := directFrameIteratorNext(callee, registers[b+1], registers[b+2])
				if ok {
					if err != nil {
						return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
					}
					if frame.openRangeOwner != nil {
						frame.clearOpenResultRange()
					}
					frame.openResultStart = -1
					frame.openResults = vmResultWindow{}
					for i := 0; i < resultCount; i++ {
						if i >= count {
							registers[a+i] = NilValue()
						} else if i == 0 {
							registers[a+i] = first
						} else {
							registers[a+i] = second
						}
					}
					break
				}
			}
			if !openArgumentMarker && resultCount == 1 && valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[b+1 : b+1+c])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = value
				break
			}
			if !openArgumentMarker && resultCount == 1 && valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if c > 0 {
					value = registers[b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = result
				break
			}
			if openArgumentMarker {
				if closure, ok := callee.scriptFunction(); ok {
					destinationCount := d
					if fixedMultiBorrow || openResultBorrow || destinationCount == 0 {
						destinationCount = resultCount
					}
					if record, entered := thread.maybeEnterRecordOnlyOpenArgumentCall(closure, frame, nextWord, b+1, openArgumentPrefix, vmResultDestination{register: a, count: destinationCount}); entered {
						thread.pushFrameRecord(record)
						thread.directFramePICCounts.addFixedCallTrampolineEntry()
						directChildActive = true
						goto reload
					}
				}
			}
			if closure, ok := callee.scriptFunction(); ok && c >= 0 {
				destinationCount := d
				if fixedMultiBorrow || openResultBorrow {
					destinationCount = resultCount
					if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, b+1, c, vmResultDestination{register: a, count: destinationCount}); entered {
						thread.pushFrameRecord(record)
						thread.directFramePICCounts.addFixedCallTrampolineEntry()
						directChildActive = true
						goto reload
					}
				}
				destination := vmResultDestination{register: a, count: destinationCount}
				args := registers[b+1 : b+1+c]
				pc = nextWord
				frame.pc = pc
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
			callee := registers[b]
			argCount, borrowHint := decodeFixedCallCount(c)
			if valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[b+1 : b+1+argCount])
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = value
				break
			}
			if valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if argCount > 0 {
					value = registers[b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = result
				break
			}
			if borrowHint {
				if closure, ok := callee.scriptFunction(); ok {
					if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, b+1, argCount, vmResultDestination{register: a, count: 1}); entered {
						thread.pushFrameRecord(record)
						thread.directFramePICCounts.addFixedCallTrampolineEntry()
						directChildActive = true
						goto reload
					}
					if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, b+1, argCount, vmResultDestination{register: a, count: 1}); borrowed {
						pc = nextWord
						frame.pc = pc
						thread.pushFrameRecord(record)
						installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
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
			callee := registers[b]
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			argCount, borrowHint := decodeFixedCallCount(d)
			pc = nextWord
			frame.pc = pc
			if borrowHint {
				if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); entered {
					thread.pushFrameRecord(record)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					directChildActive = true
					goto reload
				}
				if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); borrowed {
					thread.pushFrameRecord(record)
					installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
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
				first, second, third := fixedRegisterArgs(registers, c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[c : c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(callErr)
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[a] = value
			continue

		case opCallUpvalueOne:
			callee, err := frame.upvalue(b)
			if err != nil {
				return directFrameFail(err)
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameEnterGenericFrameFor(directFrameSideExitReasonCall)
			}
			argCount, borrowHint := decodeFixedCallCount(d)
			pc = nextWord
			frame.pc = pc
			var value Value
			var callErr error
			if borrowHint {
				if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); entered {
					thread.pushFrameRecord(record)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					directChildActive = true
					goto reload
				}
				if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); borrowed {
					thread.pushFrameRecord(record)
					installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
					thread.pushFrame(child)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[c : c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(callErr)
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[a] = value
			continue

		case opCallMethodOne:
			receiver := registers[b]
			table := receiver.tableRef()
			if table == nil {
				return directFrameFail(fmt.Errorf("run: get field target is %s, want table", receiver.Kind()))
			}
			key := constantKeys[c].str
			callee, ok := table.rawStringFieldBox(constants[c].stringBox())
			if constants[c].stringBox() == nil {
				callee, ok = directFrameRawStringField(table, key)
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
			registers[a+1] = receiver
			pc = nextWord
			frame.pc = pc
			explicitCount, borrowHint := decodeFixedCallCount(d)
			argCount := explicitCount + 1
			var value Value
			var err error
			if borrowHint && table.metatable == nil {
				if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, a+1, argCount, vmResultDestination{register: a, count: 1}); entered {
					thread.pushFrameRecord(record)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					directChildActive = true
					goto reload
				}
				if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, a+1, argCount, vmResultDestination{register: a, count: 1}); borrowed {
					thread.pushFrameRecord(record)
					installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
					thread.pushFrame(child)
					thread.directFramePICCounts.addFixedCallTrampolineEntry()
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, a+1, argCount)
				value, err = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[a+1 : a+1+argCount]
				value, err = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameYield(vmYieldedValues(yield.values))
				}
				return directFrameFail(err)
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[a] = value
			continue

		case opFastCall:
			exit := thread.runDirectFastCall(frame, nativeFuncID(b), a, c, d, picCounts)
			if exit.resumesDirectFrame() {
				break
			}
			return exit

		case opReturnOne:
			if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 1, nil) {
				goto reload
			}
			if directChildActive && (thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) ||
				thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[a])) {
				goto reload
			}
			result := vmReturnedValue(registers[a])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameReturn(result)

		case opReturn:
			count := b
			if count < 0 {
				prefixCount := -count - 1
				if frame.openResultStart == a+prefixCount {
					openResults := frame.openResultWindow()
					if directChildActive {
						if thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, prefixCount, &openResults) {
							goto reload
						}
						value := NilValue()
						if prefixCount > 0 {
							value = registers[a]
						} else {
							value = frame.openResultAt(0)
						}
						if thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, value) {
							goto reload
						}
					}
					result := vmReturnedPrefixAndWindow(registers[a:a+prefixCount], openResults)
					if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
						goto reload
					}
					return directFrameReturn(result)
				}
				if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 1, nil) {
					goto reload
				}
				if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) {
					goto reload
				}
				result := vmReturnedValue(registers[a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			if count == 0 {
				if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 0, nil) {
					goto reload
				}
				if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, NilValue()) {
					goto reload
				}
				result := vmReturnedValues(nil)
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			if count == 1 {
				if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 1, nil) {
					goto reload
				}
				if directChildActive && (thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) ||
					thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[a])) {
					goto reload
				}
				result := vmReturnedValue(registers[a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameReturn(result)
			}
			if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, count, nil) {
				goto reload
			}
			if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) {
				goto reload
			}
			result := vmReturnedBorrowedValues(registers[a : a+count])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameReturn(result)

		default:
			return directFrameEnterGenericFrame()
		}
		pc = nextWord
	}
	if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, 0, 0, nil) {
		goto reload
	}
	if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, NilValue()) {
		goto reload
	}
	result := vmReturnedValues(nil)
	if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
		goto reload
	}
	return directFrameReturn(result)
}

func runDirectFrameProductionLoop(thread *vmThread, frameRef **vmFrame) directFrameSideExit {
	frame := *frameRef
	var pc int
	rootDepth := len(thread.frames) - 1
	rootRecordDepth := len(thread.frameRecords)
	if frame.recordBaseDepth >= 0 {
		rootRecordDepth = frame.recordBaseDepth
	}
	var (
		proto             *Proto
		functionInstance  *vmFunctionInstance
		words             []wordcodeWord
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
	directChildActive = len(thread.frames)-1 > rootDepth || len(thread.frameRecords) > rootRecordDepth
	proto = frame.proto
	functionInstance = nil
	if proto.verifyErr != nil {
		return directFrameExitAt(frame, frame.pc, directFrameFail(fmt.Errorf("run: invalid prototype: %w", proto.verifyErr)))
	}
	words = proto.words
	constants = proto.constants
	constantKeys = proto.constantKeys
	constantKeyOK = proto.constantKeyOK
	constantNumbers = proto.constantNumbers
	constantNumberOK = proto.constantNumberOK
	registers = frame.registers
	pc = frame.pc

	for uint(pc) < uint(len(words)) {
		raw := words[pc]
		op := opcode(uint8(raw) & uint8(wordcodeOpcodeMask))
		// Verified wordcode lets the production loop decode the common primary
		// fields directly. Opcode-specific wide, signed, AUX, and jump operands
		// are decoded in their cases below, avoiding metadata dispatch on every
		// instruction.
		a := int(uint8(raw >> 8))
		b := int(uint8(raw >> 16))
		c := int(uint8(raw >> 24))
		nextWord := pc + 1
		switch op {
		case opLoadConst:
			b = int(uint16(raw >> 16))
			if raw&wordcodeAuxBit != 0 {
				b = int(int32(words[nextWord]))
				nextWord++
			}
			registers[a] = constants[b]

		case opLoadGlobal:
			aux := words[nextWord]
			b = int(uint16(aux))
			c = int(uint16(aux >> 16))
			nextWord++
			name, _ := constants[b].String()
			value, ok, _ := thread.globals.getSlot(proto.globalSlot(c, name), name)
			if !ok {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: undefined global %q", name)))
			}
			registers[a] = value

		case opSetGlobal:
			aux := words[nextWord]
			a = int(uint16(aux))
			c = int(uint16(aux >> 16))
			nextWord++
			name, _ := constants[a].String()
			thread.globals.setSlot(proto.globalSlot(c, name), name, registers[b])

		case opNewTable:
			if raw&wordcodeAuxBit != 0 {
				aux := words[nextWord]
				b = int(uint16(aux))
				c = int(uint16(aux >> 16))
				nextWord++
			}
			registers[a] = TableValue(newTableWithCapacity(b, c))

		case opMove:
			registers[a] = registers[b]

		case opGetUpvalue:
			b = int(uint16(raw >> 16))
			if raw&wordcodeAuxBit != 0 {
				b = int(int32(words[nextWord]))
				nextWord++
			}
			value, err := frame.upvalue(b)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(err))
			}
			registers[a] = value

		case opSetUpvalue:
			a = int(int32(words[nextWord]))
			nextWord++
			if err := frame.setUpvalue(a, registers[b]); err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(err))
			}

		case opVararg:
			b = int(int16(uint16(raw >> 16)))
			resultCount := b
			if resultCount == 0 {
				resultCount = 1
			}
			if resultCount < 0 {
				frame.clearOpenResultState()
				frame.openResultStart = a
				if !frame.publishOpenVarargRange(thread) {
					frame.openResults = vmAdjustedBorrowedResultWindow(frame.varargValues())
				}
				registers = frame.registers
				registers[a] = frame.openResultAt(0)
				pc = nextWord
				frame.pc = pc
				continue
			}
			frame.clearOpenResultState()
			for i := 0; i < resultCount; i++ {
				if i >= frame.varargLen() {
					registers[a+i] = NilValue()
				} else {
					registers[a+i] = frame.varargAt(i)
				}
			}

		case opSetField:
			if raw&wordcodeAuxBit != 0 {
				b = int(int32(words[nextWord]))
				nextWord++
			}
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind())))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[b], registers[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				break
			}
			if constantKeyOK[b] {
				keyValue := constants[b]
				var err error
				if valueKind(keyValue) == StringKind {
					table.setRawStringFieldBox(keyValue.stringText(), keyValue.stringBox(), registers[c])
				} else {
					err = table.rawSetKey(constantKeys[b], registers[c])
				}
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
				}
				break
			}
			if err := table.rawSet(constants[b], registers[c]); err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
			}

		case opSetStringField:
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op))))
			}
			b = descriptor
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind())))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[b], registers[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				break
			}
			keyValue := constants[b]
			key := constantKeys[b].str
			value := registers[c]
			if valueKind(keyValue) == StringKind && !value.IsNil() {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
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
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op))))
			}
			d := c
			c = b
			b = descriptor
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind())))
			}
			firstKey := constantKeys[b].str
			firstBox := constants[b].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = directFrameRawStringField(table, firstKey)
			}
			if !ok {
				if table.metatable != nil {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
				}
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index target is %s, want table", NilValue().Kind())))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index target is %s, want table", first.Kind())))
			}
			if nextTable.metatable != nil {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
			}
			key := registers[c]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				value := registers[d]
				if cache.writeValue(nextTable, key, value) {
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok && nextTable.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(nextTable, key, slot)
					break
				}
			}
			if err := nextTable.rawSet(key, registers[d]); err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index failed: %w", err)))
			}

		case opGetStringField:
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op))))
			}
			c = descriptor
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind())))
			}
			key := constants[c]
			keyText := constantKeys[c].str
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValue(table, key); ok {
					registers[a] = value
					break
				}
				slot, slotOK := table.rawStringFieldSlotBox(key.stringBox())
				if !slotOK {
					slot, slotOK = table.rawStringFieldSlot(keyText)
				}
				if slotOK {
					if value, ok := table.rawStringFieldAtSlot(slot, keyText); ok {
						cache.storeValue(table, key, slot)
						registers[a] = value
						break
					}
				}
			} else if value, ok := directFrameRawStringField(table, keyText); ok {
				// Keep the existing raw-string adapter as a defensive fallback for
				// malformed hand-built prototypes.
				registers[a] = value
				break
			}
			if table.metatable != nil {
				value, ok, err := directFrameTableGetIsland(thread.globals, table, constants[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get field failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				registers[a] = value
				break
			}
			registers[a] = NilValue()

		case opGetStringFieldIndex:
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op))))
			}
			d := c
			c = descriptor
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind())))
			}
			firstKey := constantKeys[c].str
			firstBox := constants[c].stringBox()
			first, ok := table.rawStringFieldBox(firstBox)
			if firstBox == nil {
				first, ok = directFrameRawStringField(table, firstKey)
			}
			if !ok {
				if table.metatable != nil {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
				}
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index target is %s, want table", NilValue().Kind())))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index target is %s, want table", first.Kind())))
			}
			if nextTable.metatable != nil {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
			}
			key := registers[d]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValue(nextTable, key); ok {
					registers[a] = value
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok {
					value, ok := nextTable.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(nextTable, key, slot)
						registers[a] = value
						break
					}
				}
			}
			value, err := nextTable.rawGet(key)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index failed: %w", err)))
			}
			registers[a] = value

		case opSetIndex:
			cacheID, _, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op))))
			}
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index target is %s, want table", base.Kind())))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, registers[b], registers[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				break
			}
			key := registers[b]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				value := registers[c]
				if cache.writeValue(table, key, value) {
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok && table.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(table, key, slot)
					break
				}
			}
			if err := table.rawSet(registers[b], registers[c]); err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index failed: %w", err)))
			}

		case opGetIndex:
			cacheID, _, cacheOK := proto.cacheIndex.cacheSiteAt(pc)
			if !cacheOK {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("wordcode pc %d %s is not a complete cache primary", pc, opcodeName(op))))
			}
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index target is %s, want table", base.Kind())))
			}
			if table.metatable != nil {
				value, ok, err := directFrameTableGetIsland(thread.globals, table, registers[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				registers[a] = value
				break
			}
			key := registers[c]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValue(table, key); ok {
					registers[a] = value
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok {
					value, ok := table.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(table, key, slot)
						registers[a] = value
						break
					}
				}
			} else if valueKind(key) == NumberKind {
				if index, ok := directFrameArrayIndexInBounds(valueNumber(key), len(table.array)); ok {
					registers[a] = table.array[index-1]
					break
				}
			}
			value, err := table.rawGet(key)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index failed: %w", err)))
			}
			registers[a] = value

		case opClosure:
			b = int(uint16(raw >> 16))
			if raw&wordcodeAuxBit != 0 {
				b = int(int32(words[nextWord]))
				nextWord++
			}
			child := proto.prototypes[b]
			captured := thread.captureUpvalues(child, frame)
			registers[a] = thread.functionValueWithCapturedUpvalues(child, captured)

		case opPrepareIter:
			iterValue := registers[a]
			iterTable := iterValue.tableRef()
			if iterTable != nil && iterTable.metatable == nil {
				if tableCanIterateCleanArray(iterTable) {
					registers[a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext)
					registers[b] = iterValue
					registers[c] = NilValue()
					break
				}
				registers[a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncTableNext)
				registers[b] = iterValue
				registers[c] = NilValue()
				break
			}
			generator, state, control, ok, err := prepareIterator(iterValue, thread.globals)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: prepare iterator failed: %w", err)))
			}
			if ok {
				registers[a] = generator
				registers[b] = state
				registers[c] = control
			}

		case opArrayNext:
			d := int(int32(words[nextWord]))
			nextWord++
			callee := registers[b]
			var first Value
			var second Value
			var count int
			var ok bool
			var err error
			if valueNativeID(callee) == nativeFuncArrayNext {
				ok = true
				tableValue := registers[c]
				table := tableValue.tableRef()
				if table == nil {
					err = fmt.Errorf("array iterator: argument #1 is %s, want table", tableValue.Kind())
				} else {
					controlValue := registers[a]
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
				first, second, count, ok, err = directFrameIteratorNext(callee, registers[c], registers[a])
			}
			if !ok {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
			}
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
			}
			frame.clearOpenResultState()
			for i := 0; i < d; i++ {
				if i >= count {
					registers[a+i] = NilValue()
					continue
				}
				if i == 0 {
					registers[a+i] = first
				} else {
					registers[a+i] = second
				}
			}

		case opArrayNextJump2:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			if proto.directLoopKernels != nil {
				if kernel := proto.directLoopKernels.kernelAt(pc); kernel != nil {
					exit := runDirectLoopKernel(thread, frame, kernel)
					if !exit.resumesDirectFrame() {
						return exit
					}
					pc = frame.pc
					continue
				}
			}
			callee := registers[b]
			if valueNativeID(callee) == nativeFuncArrayNext {
				tableValue := registers[c]
				table := tableValue.tableRef()
				if table == nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: argument #1 is %s, want table", tableValue.Kind())))
				}
				controlValue := registers[a]
				index := 0
				if valueKind(controlValue) != NilKind {
					if valueKind(controlValue) != NumberKind {
						return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want number or nil", controlValue.Kind())))
					}
					index = int(valueNumber(controlValue))
					if float64(index) != valueNumber(controlValue) {
						return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want integer", controlValue.Kind())))
					}
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				next := index + 1
				if next < 1 || next > len(table.array) {
					registers[a] = NilValue()
					registers[a+1] = NilValue()
					pc = d
					continue
				}
				registers[a] = NumberValue(float64(next))
				registers[a+1] = table.array[next-1]
				break
			}
			first, second, count, ok, err := directFrameIteratorNext(callee, registers[c], registers[a])
			if !ok {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
			}
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			if count < 1 || first.IsNil() {
				registers[a] = NilValue()
				registers[a+1] = NilValue()
				pc = d
				continue
			}
			registers[a] = first
			if count > 1 {
				registers[a+1] = second
			} else {
				registers[a+1] = NilValue()
			}

		case opAdd:
			left := registers[b]
			right := registers[c]
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
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: add failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) + valueNumber(right))

		case opSub:
			left := registers[b]
			right := registers[c]
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
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: subtract failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - valueNumber(right))

		case opMul:
			left := registers[b]
			right := registers[c]
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
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: multiply failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) * valueNumber(right))

		case opDiv:
			left := registers[b]
			right := registers[c]
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
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) / valueNumber(right))

		case opMod:
			left := registers[b]
			right := registers[c]
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
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: modulo failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/valueNumber(right))*valueNumber(right))

		case opIDiv:
			left := registers[b]
			right := registers[c]
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
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: floor divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Floor(valueNumber(left) / valueNumber(right)))

		case opPow:
			left := registers[b]
			right := registers[c]
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
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: power failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Pow(valueNumber(left), valueNumber(right)))

		case opAddK:
			if raw&wordcodeAuxBit != 0 {
				c = int(int32(words[nextWord]))
				nextWord++
			}
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__add",
					"add",
					func(left float64, right float64) float64 { return left + right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: add failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) + constantNumbers[c])

		case opSubK:
			if raw&wordcodeAuxBit != 0 {
				c = int(int32(words[nextWord]))
				nextWord++
			}
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__sub",
					"subtract",
					func(left float64, right float64) float64 { return left - right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: subtract failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - constantNumbers[c])

		case opMulK:
			if raw&wordcodeAuxBit != 0 {
				c = int(int32(words[nextWord]))
				nextWord++
			}
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mul",
					"multiply",
					func(left float64, right float64) float64 { return left * right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: multiply failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) * constantNumbers[c])

		case opDivK:
			if raw&wordcodeAuxBit != 0 {
				c = int(int32(words[nextWord]))
				nextWord++
			}
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__div",
					"divide",
					func(left float64, right float64) float64 { return left / right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) / constantNumbers[c])

		case opModK:
			if raw&wordcodeAuxBit != 0 {
				c = int(int32(words[nextWord]))
				nextWord++
			}
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mod",
					"modulo",
					math.Mod,
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: modulo failed: %w", err)))
				}
				registers[a] = value
				break
			}
			right := constantNumbers[c]
			registers[a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/right)*right)

		case opIDivK:
			if raw&wordcodeAuxBit != 0 {
				c = int(int32(words[nextWord]))
				nextWord++
			}
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__idiv",
					"floor divide",
					func(left float64, right float64) float64 { return math.Floor(left / right) },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: floor divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Floor(valueNumber(left) / constantNumbers[c]))

		case opNeg:
			operand := registers[b]
			if valueKind(operand) != NumberKind {
				value, err := directFrameUnaryArithmeticValueUncounted(thread.globals, operand, negateValue)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(-valueNumber(operand))

		case opLen:
			operand := registers[b]
			switch valueKind(operand) {
			case StringKind:
				registers[a] = NumberValue(float64(len(operand.stringText())))
			case TableKind:
				table := operand.tableRef()
				if table == nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: length failed: table: nil table")))
				}
				if table.metatable != nil {
					value, err := lengthValue(operand, thread.globals)
					if err != nil {
						return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: length failed: %w", err)))
					}
					registers[a] = value
					break
				}
				length, err := table.rawLen()
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: length failed: %w", err)))
				}
				registers[a] = NumberValue(float64(length))
			default:
				value, err := lengthValue(operand, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: length failed: %w", err)))
				}
				registers[a] = value
			}

		case opConcat:
			left := registers[b]
			right := registers[c]
			if !directFrameRawConcatOperand(left) || !directFrameRawConcatOperand(right) {
				value, err := concatValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: concat failed: %w", err)))
				}
				registers[a] = value
				break
			}
			concatValues := [2]Value{left, right}
			if value, ok := thread.internStringConcatValues(concatValues[:]); ok {
				registers[a] = value
				break
			}
			leftText, err := concatOperandString(left, "left")
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: concat failed: %w", err)))
			}
			rightText, err := concatOperandString(right, "right")
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: concat failed: %w", err)))
			}
			registers[a] = thread.internStringValue(leftText + rightText)

		case opConcatChain:
			if raw&wordcodeAuxBit != 0 {
				c = int(int32(words[nextWord]))
				nextWord++
			}
			if value, ok := thread.internStringConcatValues(registers[b : b+c]); ok {
				registers[a] = value
				break
			}
			text, ok, err := thread.concatRawChainString(registers[b : b+c])
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: concat failed: %w", err)))
			}
			if !ok {
				value, err := concatChainValue(registers[b:b+c], thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: concat failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = thread.internStringValue(text)

		case opEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: equal failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valuesEqual(left, right))

		case opNotEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: equal failed: %w", err)))
				}
				registers[a] = BoolValue(!value)
				break
			}
			registers[a] = BoolValue(!valuesEqual(left, right))

		case opLess:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() < right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) < valueNumber(right))

		case opLessEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() <= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessEqualValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less equal failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) <= valueNumber(right))

		case opGreater:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() > right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessValue(right, left, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) > valueNumber(right))

		case opGreaterEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() >= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessEqualValue(right, left, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater equal failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) >= valueNumber(right))

		case opNumericForCheck:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			loopValue := registers[a]
			limitValue := registers[b]
			stepValue := registers[c]
			if valueKind(loopValue) != NumberKind {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind())))
			}
			if valueKind(limitValue) != NumberKind {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: numeric for limit is %s, want number", limitValue.Kind())))
			}
			if valueKind(stepValue) != NumberKind {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind())))
			}
			if math.IsNaN(valueNumber(loopValue)) || math.IsNaN(valueNumber(limitValue)) || math.IsNaN(valueNumber(stepValue)) {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: numeric for operand is NaN")))
			}
			if valueNumber(stepValue) > 0 {
				if valueNumber(loopValue) > valueNumber(limitValue) {
					pc = d
					continue
				}
				break
			}
			if valueNumber(loopValue) < valueNumber(limitValue) {
				pc = d
				continue
			}

		case opNumericForLoop:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			loopValue := registers[a]
			stepValue := registers[b]
			if valueKind(loopValue) != NumberKind || valueKind(stepValue) != NumberKind {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
			}
			registers[a] = NumberValue(valueNumber(loopValue) + valueNumber(stepValue))
			pc = d
			continue

		case opJumpIfNotEqualK:
			b = int(uint16(raw >> 16))
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			if valueKind(left) == NumberKind && constantNumberOK[b] {
				if valueNumber(left) != constantNumbers[b] {
					pc = d
					continue
				}
				break
			}
			if valueKind(left) == StringKind && constantKeyOK[b] {
				if left.stringText() != constantKeys[b].str {
					pc = d
					continue
				}
				break
			}
			right := constants[b]
			equal, err := equalValue(left, right, thread.globals)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: equal failed: %w", err)))
			}
			if !equal {
				pc = d
				continue
			}

		case opJumpIfTableHasMetatable:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			base := registers[a]
			if table := base.tableRef(); table != nil && table.metatable != nil {
				pc = d
				continue
			}

		case opJumpIfNotLessK:
			b = int(uint16(raw >> 16))
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, constants[b])
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if !less {
				pc = d
				continue
			}

		case opJumpIfNotGreaterK:
			b = int(uint16(raw >> 16))
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			greater, err := directFrameLessForBranchUncounted(thread.globals, constants[b], left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if !greater {
				pc = d
				continue
			}

		case opJumpIfLessK:
			b = int(uint16(raw >> 16))
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, constants[b])
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if less {
				pc = d
				continue
			}

		case opJumpIfGreaterK:
			b = int(uint16(raw >> 16))
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			greater, err := directFrameLessForBranchUncounted(thread.globals, constants[b], left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if greater {
				pc = d
				continue
			}

		case opJumpIfNotLess:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			right := registers[b]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, right)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if !less {
				pc = d
				continue
			}

		case opJumpIfNotGreater:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			right := registers[b]
			greater, err := directFrameLessForBranchUncounted(thread.globals, right, left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if !greater {
				pc = d
				continue
			}

		case opJumpIfLess:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			right := registers[b]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, right)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if less {
				pc = d
				continue
			}

		case opJumpIfGreater:
			d := int(int32(words[nextWord]))
			nextWord++
			d += nextWord
			left := registers[a]
			right := registers[b]
			greater, err := directFrameLessForBranchUncounted(thread.globals, right, left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if greater {
				pc = d
				continue
			}

		case opJumpIfFalse:
			b = int(int16(uint16(raw >> 16)))
			if raw&wordcodeAuxBit != 0 {
				b = int(int32(words[nextWord]))
				nextWord++
			}
			b += nextWord
			if !registers[a].truthy() {
				pc = b
				continue
			}

		case opJump:
			b = int(int32(raw)>>8) + nextWord
			pc = b
			continue

		case opCall:
			aux := words[nextWord]
			c = int(int16(uint16(aux)))
			d := int(int16(uint16(aux >> 16)))
			nextWord++
			resultCount, fixedMultiBorrow := decodeFixedMultiResultCount(d, frame.proto.registers)
			openResultBorrow := decodeOpenResultCallMarker(d)
			openArgumentPrefix, openArgumentMarker := decodeOpenArgumentCallMarker(c)
			if openResultBorrow {
				resultCount = -1
			}
			if openArgumentMarker {
				prefixCount, _ := decodeOpenArgumentCallMarker(c)
				c = -prefixCount - 1
			}
			if !fixedMultiBorrow && resultCount == 0 {
				resultCount = 1
			}
			callee := registers[b]
			if c == 2 && resultCount == 2 {
				first, second, count, ok, err := directFrameIteratorNext(callee, registers[b+1], registers[b+2])
				if ok {
					if err != nil {
						return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
					}
					if frame.openRangeOwner != nil {
						frame.clearOpenResultRange()
					}
					frame.openResultStart = -1
					frame.openResults = vmResultWindow{}
					for i := 0; i < resultCount; i++ {
						if i >= count {
							registers[a+i] = NilValue()
						} else if i == 0 {
							registers[a+i] = first
						} else {
							registers[a+i] = second
						}
					}
					break
				}
			}
			if !openArgumentMarker && resultCount == 1 && valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[b+1 : b+1+c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = value
				break
			}
			if !openArgumentMarker && resultCount == 1 && valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if c > 0 {
					value = registers[b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = result
				break
			}
			if openArgumentMarker {
				if closure, ok := callee.scriptFunction(); ok {
					destinationCount := d
					if fixedMultiBorrow || openResultBorrow || destinationCount == 0 {
						destinationCount = resultCount
					}
					if record, entered := thread.maybeEnterRecordOnlyOpenArgumentCall(closure, frame, nextWord, b+1, openArgumentPrefix, vmResultDestination{register: a, count: destinationCount}); entered {
						thread.pushFrameRecord(record)
						thread.directFramePICCounts.addFixedCallTrampolineEntry()
						directChildActive = true
						goto reload
					}
				}
			}
			if closure, ok := callee.scriptFunction(); ok && c >= 0 {
				destinationCount := d
				if fixedMultiBorrow || openResultBorrow {
					destinationCount = resultCount
					if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, b+1, c, vmResultDestination{register: a, count: destinationCount}); entered {
						thread.pushFrameRecord(record)
						directChildActive = true
						goto reload
					}
				}
				destination := vmResultDestination{register: a, count: destinationCount}
				args := registers[b+1 : b+1+c]
				pc = nextWord
				frame.pc = pc
				result, err := thread.runInlineScriptCall(closure, args)
				if err != nil {
					if yield, ok := err.(vmYieldRequest); ok {
						thread.installPendingCall(frame, vmPendingCall{
							destination: destination,
							protected:   yield.protected,
							host:        yield.host,
						})
						return directFrameExitAt(frame, pc, directFrameYield(vmYieldedValues(yield.values)))
					}
					return directFrameExitAt(frame, pc, directFrameFail(err))
				}
				// A nested materialized call may grow the shared register arena and
				// rebind every live frame. Refresh the dispatch slice before the
				// caller resumes so subsequent instructions cannot read the stale
				// backing array.
				registers = frame.registers
				frame.applyValueListDestination(destination, result.window)
				continue
			}
			return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonCall))

		case opCallOne:
			aux := words[nextWord]
			c = int(int16(uint16(aux)))
			nextWord++
			callee := registers[b]
			argCount, borrowHint := decodeFixedCallCount(c)
			if valueNativeID(callee) == nativeFuncRawLen {
				value, err := baseRawLenValue(registers[b+1 : b+1+argCount])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = value
				break
			}
			if valueNativeID(callee) == nativeFuncToString {
				value := NilValue()
				if argCount > 0 {
					value = registers[b+1]
				}
				result, err := baseToStringValue(thread.globals, value)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				registers[a] = result
				break
			}
			if borrowHint {
				if closure, ok := callee.scriptFunction(); ok {
					if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, b+1, argCount, vmResultDestination{register: a, count: 1}); entered {
						thread.pushFrameRecord(record)
						directChildActive = true
						goto reload
					}
					if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, b+1, argCount, vmResultDestination{register: a, count: 1}); borrowed {
						pc = nextWord
						frame.pc = pc
						thread.pushFrameRecord(record)
						installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
						thread.pushFrame(child)
						frame = child
						directChildActive = true
						goto reload
					}
				}
			}
			return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonCall))

		case opCallLocalOne:
			d := int(int32(words[nextWord]))
			nextWord++
			callee := registers[b]
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonCall))
			}
			argCount, borrowHint := decodeFixedCallCount(d)
			pc = nextWord
			frame.pc = pc
			if borrowHint {
				if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); entered {
					thread.pushFrameRecord(record)
					directChildActive = true
					goto reload
				}
				if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); borrowed {
					thread.pushFrameRecord(record)
					installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
					thread.pushFrame(child)
					frame = child
					directChildActive = true
					goto reload
				}
			}
			var value Value
			var callErr error
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[c : c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameExitAt(frame, pc, directFrameYield(vmYieldedValues(yield.values)))
				}
				return directFrameExitAt(frame, pc, directFrameFail(callErr))
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[a] = value
			continue

		case opCallUpvalueOne:
			d := int(int32(words[nextWord]))
			nextWord++
			callee, err := frame.upvalue(b)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(err))
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonCall))
			}
			argCount, borrowHint := decodeFixedCallCount(d)
			pc = nextWord
			frame.pc = pc
			var value Value
			var callErr error
			if borrowHint {
				if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); entered {
					thread.pushFrameRecord(record)
					directChildActive = true
					goto reload
				}
				if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, c, argCount, vmResultDestination{register: a, count: 1}); borrowed {
					thread.pushFrameRecord(record)
					installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
					thread.pushFrame(child)
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, c, argCount)
				value, callErr = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[c : c+argCount]
				value, callErr = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if callErr != nil {
				if yield, ok := callErr.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameExitAt(frame, pc, directFrameYield(vmYieldedValues(yield.values)))
				}
				return directFrameExitAt(frame, pc, directFrameFail(callErr))
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[a] = value
			continue

		case opCallMethodOne:
			aux := words[nextWord]
			c = int(uint16(aux))
			d := int(int16(uint16(aux >> 16)))
			nextWord++
			receiver := registers[b]
			table := receiver.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get field target is %s, want table", receiver.Kind())))
			}
			key := constantKeys[c].str
			callee, ok := table.rawStringFieldBox(constants[c].stringBox())
			if constants[c].stringBox() == nil {
				callee, ok = directFrameRawStringField(table, key)
			}
			if !ok {
				if table.metatable != nil {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
				}
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonCall))
			}
			closure, ok := callee.scriptFunction()
			if !ok {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonCall))
			}
			registers[a+1] = receiver
			pc = nextWord
			frame.pc = pc
			explicitCount, borrowHint := decodeFixedCallCount(d)
			argCount := explicitCount + 1
			var value Value
			var err error
			if borrowHint && table.metatable == nil {
				if record, entered := thread.maybeEnterRecordOnlyFixedCall(closure, frame, nextWord, a+1, argCount, vmResultDestination{register: a, count: 1}); entered {
					thread.pushFrameRecord(record)
					directChildActive = true
					goto reload
				}
				if child, record, borrowed := thread.newBorrowedFixedFrameRecord(closure, frame, nextWord, a+1, argCount, vmResultDestination{register: a, count: 1}); borrowed {
					thread.pushFrameRecord(record)
					installFixedResultPendingCall(frame, vmResultDestination{register: a, count: 1})
					thread.pushFrame(child)
					frame = child
					directChildActive = true
					goto reload
				}
			}
			if argCount <= 3 {
				first, second, third := fixedRegisterArgs(registers, a+1, argCount)
				value, err = thread.runInlineScriptCallFixedOneNoHook(closure, first, second, third, argCount)
			} else {
				args := registers[a+1 : a+1+argCount]
				value, err = thread.runInlineScriptCallOneNoHook(closure, args)
			}
			if err != nil {
				if yield, ok := err.(vmYieldRequest); ok {
					thread.installPendingCall(frame, vmPendingCall{
						destination: vmResultDestination{register: a, count: 1},
						protected:   yield.protected,
						host:        yield.host,
					})
					return directFrameExitAt(frame, pc, directFrameYield(vmYieldedValues(yield.values)))
				}
				return directFrameExitAt(frame, pc, directFrameFail(err))
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			registers = frame.registers
			registers[a] = value
			continue

		case opFastCall:
			aux := words[nextWord]
			c = int(uint16(aux))
			d := int(int16(uint16(aux >> 16)))
			nextWord++
			exit := thread.runDirectFastCall(frame, nativeFuncID(b), a, c, d, nil)
			if exit.resumesDirectFrame() {
				break
			}
			frame.pc = pc
			return exit

		case opReturnOne:
			if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 1, nil) {
				goto reload
			}
			if directChildActive && (thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) ||
				thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[a])) {
				goto reload
			}
			result := vmReturnedValue(registers[a])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameExitAt(frame, pc, directFrameReturn(result))

		case opReturn:
			b = int(int16(uint16(raw >> 16)))
			count := b
			if count < 0 {
				prefixCount := -count - 1
				if frame.openResultStart == a+prefixCount {
					openResults := frame.openResultWindow()
					if directChildActive {
						if thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, prefixCount, &openResults) {
							goto reload
						}
						value := NilValue()
						if prefixCount > 0 {
							value = registers[a]
						} else {
							value = frame.openResultAt(0)
						}
						if thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, value) {
							goto reload
						}
					}
					result := vmReturnedPrefixAndWindow(registers[a:a+prefixCount], openResults)
					if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
						goto reload
					}
					return directFrameExitAt(frame, pc, directFrameReturn(result))
				}
				if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 1, nil) {
					goto reload
				}
				if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) {
					goto reload
				}
				result := vmReturnedValue(registers[a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameExitAt(frame, pc, directFrameReturn(result))
			}
			if count == 0 {
				if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 0, nil) {
					goto reload
				}
				if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, NilValue()) {
					goto reload
				}
				result := vmReturnedValues(nil)
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameExitAt(frame, pc, directFrameReturn(result))
			}
			if count == 1 {
				if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, 1, nil) {
					goto reload
				}
				if directChildActive && (thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) ||
					thread.resumeDirectFrameChildOne(rootDepth, &frame, registers[a])) {
					goto reload
				}
				result := vmReturnedValue(registers[a])
				if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
					goto reload
				}
				return directFrameExitAt(frame, pc, directFrameReturn(result))
			}
			if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, a, count, nil) {
				goto reload
			}
			if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, registers[a]) {
				goto reload
			}
			result := vmReturnedBorrowedValues(registers[a : a+count])
			if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
				goto reload
			}
			return directFrameExitAt(frame, pc, directFrameReturn(result))

		default:
			return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
		}
		pc = nextWord
	}
	if directChildActive && thread.resumeRecordOnlyFixedCall(rootRecordDepth, &frame, 0, 0, nil) {
		goto reload
	}
	if directChildActive && thread.resumeRecordOnlyFixedCallOne(rootRecordDepth, &frame, NilValue()) {
		goto reload
	}
	result := vmReturnedValues(nil)
	if directChildActive && thread.resumeDirectFrameChild(rootDepth, &frame, result) {
		goto reload
	}
	return directFrameExitAt(frame, pc, directFrameReturn(result))
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
	if frame == nil || frame.proto == nil || pc < 0 {
		return -1
	}
	if len(frame.proto.wordLines) != 0 {
		if pc >= len(frame.proto.wordLines) {
			return -1
		}
		return frame.proto.wordLines[pc]
	}
	if pc >= len(frame.proto.lines) {
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
	callee, ok := directFrameRawStringField(table, field)
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
	if callee, ok := directFrameRawStringField(table, field); ok {
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

func (thread *vmThread) captureUpvalues(proto *Proto, frame *vmFrame) capturedUpvalueSet {
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
			captured.setCell(i, thread.registerCell(frame, desc.index))
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
