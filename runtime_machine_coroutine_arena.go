package ember

import (
	"errors"
	"math"
)

type machineCoroutineStatus uint8

const (
	machineCoroutineSuspended machineCoroutineStatus = iota
	machineCoroutineRunning
	machineCoroutineNormal
	machineCoroutineDead
)

func (status machineCoroutineStatus) String() string {
	switch status {
	case machineCoroutineSuspended:
		return "suspended"
	case machineCoroutineRunning:
		return "running"
	case machineCoroutineNormal:
		return "normal"
	case machineCoroutineDead:
		return "dead"
	default:
		return "dead"
	}
}

type machineCoroutineHandle struct {
	owner      uint64
	index      uint32
	generation uint16
}

type machineCoroutineRoot struct {
	module  programModuleID
	proto   int32
	closure machineClosureHandle
}

// machineCoroutineFrameState is the scalar continuation point at which the
// generated loop can restart. resumeRegister/resumeCount describe where the
// arguments of the next resume become the results of coroutine.yield.
type machineCoroutineFrameState struct {
	module         programModuleID
	proto          int32
	pc             int32
	base           int32
	closure        machineClosureHandle
	varargStart    int32
	varargCount    int32
	openStart      int32
	openCount      int32
	cellStart      int32
	resumeRegister int32
	resumeCount    int32
	callDepth      uint32
	resume         machineSemanticResume
}

type machineCoroutineOpenCell struct {
	register   uint32
	cell       machineCellID
	generation uint16
}

// machineCoroutineRecord is deliberately pointer-free. Variable-sized frame
// storage lives in scalar arena spans, and invocation effects live in the
// explicit cold sidecar in runtime_machine_coroutine.go.
type machineCoroutineRecord struct {
	root                 machineCoroutineRoot
	frame                machineCoroutineFrameState
	registerStart        uint32
	registerCount        uint32
	registerCapacity     uint32
	numberStart          uint32
	numberCount          uint32
	numberCapacity       uint32
	continuationStart    uint32
	continuationCount    uint32
	continuationCapacity uint32
	openCellStart        uint32
	openCellCount        uint32
	openCellCapacity     uint32
	generation           uint16
	status               machineCoroutineStatus
	live                 uint8
	started              uint8
}

type machineCoroutineSnapshot struct {
	frame         machineCoroutineFrameState
	registers     []slot
	numberBits    []uint64
	continuations []machineContinuation
	openCells     []machineCoroutineOpenCell
}

// machineCoroutineArena is owner-local, dense scalar storage. Its hot record
// and span element types contain no Go pointers, interfaces, strings, maps, or
// effectful values. All mutation is stopped and serialized by the controller.
type machineCoroutineArena struct {
	records       []machineCoroutineRecord
	registers     []slot
	numberBits    []uint64
	continuations []machineContinuation
	openCells     []machineCoroutineOpenCell
	free          []uint32
	owner         uint64
	limit         uint32
	live          uint32
	closed        bool
}

var (
	errMachineCoroutineArenaClosed = errors.New("machine coroutine arena is closed")
	errMachineCoroutineOwner       = errors.New("machine coroutine belongs to another owner")
	errMachineCoroutineIndex       = errors.New("machine coroutine index is invalid")
	errMachineCoroutineGeneration  = errors.New("machine coroutine generation is stale")
	errMachineCoroutineDead        = errors.New("cannot resume dead coroutine")
	errMachineCoroutineRunning     = errors.New("cannot resume running coroutine")
	errMachineCoroutineNormal      = errors.New("cannot resume non-suspended coroutine")
	errMachineCoroutineNotRunning  = errors.New("machine coroutine is not running")
	errMachineCoroutineSnapshot    = errors.New("machine coroutine snapshot is invalid")
)

func (arena *machineCoroutineArena) bindStopped(owner uint64, limit uint32) error {
	if arena == nil || arena.closed {
		return errMachineCoroutineArenaClosed
	}
	if owner == 0 {
		return errMachineCoroutineIndex
	}
	arena.clearStorageStopped(false)
	arena.owner = owner
	arena.limit = limit
	return nil
}

func (arena *machineCoroutineArena) createStopped(root machineCoroutineRoot) (machineCoroutineHandle, error) {
	if err := arena.ready(); err != nil {
		return machineCoroutineHandle{}, err
	}
	if root.closure.owner != arena.owner || root.closure.index == 0 || root.closure.generation == 0 || root.proto < 0 {
		return machineCoroutineHandle{}, errMachineCoroutineOwner
	}
	if arena.limit != 0 && arena.live >= arena.limit {
		return machineCoroutineHandle{}, &LimitError{Kind: LimitCoroutines, Limit: uint64(arena.limit), Used: uint64(arena.live) + 1}
	}
	var index uint32
	if len(arena.free) != 0 {
		index = arena.free[len(arena.free)-1]
		arena.free = arena.free[:len(arena.free)-1]
	} else {
		if uint64(len(arena.records))+1 > uint64(math.MaxUint32) {
			return machineCoroutineHandle{}, errMachineCoroutineIndex
		}
		arena.records = append(arena.records, machineCoroutineRecord{})
		index = uint32(len(arena.records))
	}
	record := &arena.records[index-1]
	generation := nextMachineCoroutineGeneration(record.generation)
	registerStart, registerCapacity := record.registerStart, record.registerCapacity
	numberStart, numberCapacity := record.numberStart, record.numberCapacity
	continuationStart, continuationCapacity := record.continuationStart, record.continuationCapacity
	openCellStart, openCellCapacity := record.openCellStart, record.openCellCapacity
	*record = machineCoroutineRecord{
		root:                 root,
		registerStart:        registerStart,
		registerCapacity:     registerCapacity,
		numberStart:          numberStart,
		numberCapacity:       numberCapacity,
		continuationStart:    continuationStart,
		continuationCapacity: continuationCapacity,
		openCellStart:        openCellStart,
		openCellCapacity:     openCellCapacity,
		generation:           generation,
		status:               machineCoroutineSuspended,
		live:                 1,
	}
	arena.live++
	return machineCoroutineHandle{owner: arena.owner, index: index, generation: generation}, nil
}

func nextMachineCoroutineGeneration(generation uint16) uint16 {
	if generation == slotMaxGeneration || generation == 0 {
		return 1
	}
	return generation + 1
}

func (arena *machineCoroutineArena) statusStopped(handle machineCoroutineHandle) (machineCoroutineStatus, error) {
	record, err := arena.record(handle)
	if err != nil {
		return machineCoroutineDead, err
	}
	return record.status, nil
}

// beginResumeStopped atomically changes a suspended coroutine to running. The
// returned started bit selects root entry (false) or continuation restore
// (true). Failure never changes the record.
func (arena *machineCoroutineArena) beginResumeStopped(handle machineCoroutineHandle) (machineCoroutineRoot, bool, uint32, error) {
	record, err := arena.record(handle)
	if err != nil {
		return machineCoroutineRoot{}, false, 0, err
	}
	switch record.status {
	case machineCoroutineDead:
		return machineCoroutineRoot{}, false, 0, errMachineCoroutineDead
	case machineCoroutineRunning:
		return machineCoroutineRoot{}, false, 0, errMachineCoroutineRunning
	case machineCoroutineNormal:
		return machineCoroutineRoot{}, false, 0, errMachineCoroutineNormal
	case machineCoroutineSuspended:
	default:
		return machineCoroutineRoot{}, false, 0, errMachineCoroutineSnapshot
	}
	record.status = machineCoroutineRunning
	depth := uint32(1)
	if record.started != 0 {
		depth = record.frame.callDepth
		if depth == 0 {
			record.status = machineCoroutineSuspended
			return machineCoroutineRoot{}, false, 0, errMachineCoroutineSnapshot
		}
	}
	return record.root, record.started != 0, depth, nil
}

func (arena *machineCoroutineArena) cancelResumeStopped(handle machineCoroutineHandle) error {
	record, err := arena.record(handle)
	if err != nil {
		return err
	}
	if record.status != machineCoroutineRunning {
		return errMachineCoroutineNotRunning
	}
	record.status = machineCoroutineSuspended
	return nil
}

func (arena *machineCoroutineArena) setNormalStopped(handle machineCoroutineHandle) error {
	record, err := arena.record(handle)
	if err != nil {
		return err
	}
	if record.status != machineCoroutineRunning {
		return errMachineCoroutineNotRunning
	}
	record.status = machineCoroutineNormal
	return nil
}

func (arena *machineCoroutineArena) restoreRunningStopped(handle machineCoroutineHandle) error {
	record, err := arena.record(handle)
	if err != nil {
		return err
	}
	if record.status != machineCoroutineNormal {
		return errMachineCoroutineNormal
	}
	record.status = machineCoroutineRunning
	return nil
}

func (arena *machineCoroutineArena) suspendStopped(handle machineCoroutineHandle, snapshot machineCoroutineSnapshot) error {
	record, err := arena.record(handle)
	if err != nil {
		return err
	}
	if record.status != machineCoroutineRunning {
		return errMachineCoroutineNotRunning
	}
	if err := validateMachineCoroutineSnapshot(snapshot); err != nil {
		return err
	}
	arena.clearRecordSpansStopped(record)
	if err := arena.appendSnapshotStopped(record, snapshot); err != nil {
		return err
	}
	record.frame = snapshot.frame
	record.started = 1
	record.status = machineCoroutineSuspended
	return nil
}

func validateMachineCoroutineSnapshot(snapshot machineCoroutineSnapshot) error {
	if snapshot.frame.proto < 0 || snapshot.frame.pc < 0 || snapshot.frame.base < 0 || snapshot.frame.callDepth == 0 {
		return errMachineCoroutineSnapshot
	}
	if snapshot.frame.callDepth != uint32(len(snapshot.continuations))+1 {
		return errMachineCoroutineSnapshot
	}
	if snapshot.frame.resumeRegister < 0 || snapshot.frame.resumeCount < -1 {
		return errMachineCoroutineSnapshot
	}
	if uint64(len(snapshot.registers)) > math.MaxUint32 || uint64(len(snapshot.numberBits)) > math.MaxUint32 ||
		uint64(len(snapshot.continuations)) > math.MaxUint32 || uint64(len(snapshot.openCells)) > math.MaxUint32 {
		return errMachineCoroutineSnapshot
	}
	return nil
}

func (arena *machineCoroutineArena) appendSnapshotStopped(record *machineCoroutineRecord, snapshot machineCoroutineSnapshot) error {
	if !machineCoroutineSpanCanStore(len(arena.registers), record.registerStart, record.registerCapacity, len(snapshot.registers)) ||
		!machineCoroutineSpanCanStore(len(arena.numberBits), record.numberStart, record.numberCapacity, len(snapshot.numberBits)) ||
		!machineCoroutineSpanCanStore(len(arena.continuations), record.continuationStart, record.continuationCapacity, len(snapshot.continuations)) ||
		!machineCoroutineSpanCanStore(len(arena.openCells), record.openCellStart, record.openCellCapacity, len(snapshot.openCells)) {
		return errMachineCoroutineSnapshot
	}
	machineCoroutineStoreSpan(&arena.registers, &record.registerStart, &record.registerCount, &record.registerCapacity, snapshot.registers)
	machineCoroutineStoreSpan(&arena.numberBits, &record.numberStart, &record.numberCount, &record.numberCapacity, snapshot.numberBits)
	machineCoroutineStoreSpan(&arena.continuations, &record.continuationStart, &record.continuationCount, &record.continuationCapacity, snapshot.continuations)
	machineCoroutineStoreSpan(&arena.openCells, &record.openCellStart, &record.openCellCount, &record.openCellCapacity, snapshot.openCells)
	return nil
}

func machineCoroutineSpanCanStore(length int, start, capacity uint32, count int) bool {
	if count < 0 || uint64(count) > math.MaxUint32 {
		return false
	}
	if uint64(count) <= uint64(capacity) {
		return machineCoroutineSpanValid(start, capacity, length)
	}
	_, ok := machineCoroutineAppendBound(length, machineCoroutineSpanCapacity(count))
	return ok
}

func machineCoroutineSpanCapacity(count int) int {
	capacity := 1
	for capacity < count {
		if uint64(capacity) > uint64(math.MaxUint32)/2 {
			return count
		}
		capacity *= 2
	}
	if count == 0 {
		return 0
	}
	return capacity
}

func machineCoroutineStoreSpan[T any](storage *[]T, start, count, capacity *uint32, values []T) {
	if len(values) > int(*capacity) {
		if machineCoroutineSpanValid(*start, *capacity, len(*storage)) {
			clear((*storage)[int(*start):int(*start+*capacity)])
		}
		newCapacity := machineCoroutineSpanCapacity(len(values))
		newStart := len(*storage)
		*storage = append(*storage, make([]T, newCapacity)...)
		*start = uint32(newStart)
		*capacity = uint32(newCapacity)
	}
	span := (*storage)[int(*start):int(*start+*capacity)]
	clear(span)
	copy(span, values)
	*count = uint32(len(values))
}

func machineCoroutineAppendBound(start, count int) (uint32, bool) {
	if start < 0 || count < 0 || uint64(start)+uint64(count) > math.MaxUint32 {
		return 0, false
	}
	return uint32(start), true
}

func (arena *machineCoroutineArena) snapshotStopped(handle machineCoroutineHandle, destination *machineCoroutineSnapshot) error {
	record, err := arena.record(handle)
	if err != nil {
		return err
	}
	if destination == nil || record.status != machineCoroutineRunning || record.started == 0 {
		return errMachineCoroutineSnapshot
	}
	if !arena.recordSpansValid(record) {
		return errMachineCoroutineSnapshot
	}
	destination.frame = record.frame
	destination.registers = append(destination.registers[:0], arena.registerSpan(record)...)
	destination.numberBits = append(destination.numberBits[:0], arena.numberSpan(record)...)
	destination.continuations = append(destination.continuations[:0], arena.continuationSpan(record)...)
	destination.openCells = append(destination.openCells[:0], arena.openCellSpan(record)...)
	arena.clearRecordSpansStopped(record)
	return nil
}

func (arena *machineCoroutineArena) completeStopped(handle machineCoroutineHandle) (uint32, error) {
	record, err := arena.record(handle)
	if err != nil {
		return 0, err
	}
	if record.status != machineCoroutineRunning {
		return 0, errMachineCoroutineNotRunning
	}
	depth := uint32(1)
	if record.started != 0 {
		depth = record.frame.callDepth
	}
	arena.clearRecordSpansStopped(record)
	record.status = machineCoroutineDead
	if arena.live > 0 {
		arena.live--
	}
	return depth, nil
}

func (arena *machineCoroutineArena) closeCoroutineStopped(handle machineCoroutineHandle) error {
	record, err := arena.record(handle)
	if err != nil {
		return err
	}
	if record.status == machineCoroutineRunning || record.status == machineCoroutineNormal {
		return errMachineCoroutineRunning
	}
	if record.status != machineCoroutineDead && arena.live > 0 {
		arena.live--
	}
	arena.clearRecordSpansStopped(record)
	record.status = machineCoroutineDead
	return nil
}

// releaseDeadStopped is the collector seam. A released dead handle becomes
// stale; its record may then be reused with a different generation.
func (arena *machineCoroutineArena) releaseDeadStopped(handle machineCoroutineHandle) error {
	record, err := arena.record(handle)
	if err != nil {
		return err
	}
	if record.status != machineCoroutineDead {
		return errMachineCoroutineRunning
	}
	generation := record.generation
	registerStart, registerCapacity := record.registerStart, record.registerCapacity
	numberStart, numberCapacity := record.numberStart, record.numberCapacity
	continuationStart, continuationCapacity := record.continuationStart, record.continuationCapacity
	openCellStart, openCellCapacity := record.openCellStart, record.openCellCapacity
	*record = machineCoroutineRecord{
		registerStart:        registerStart,
		registerCapacity:     registerCapacity,
		numberStart:          numberStart,
		numberCapacity:       numberCapacity,
		continuationStart:    continuationStart,
		continuationCapacity: continuationCapacity,
		openCellStart:        openCellStart,
		openCellCapacity:     openCellCapacity,
		generation:           generation,
	}
	arena.free = append(arena.free, handle.index)
	return nil
}

func (arena *machineCoroutineArena) record(handle machineCoroutineHandle) (*machineCoroutineRecord, error) {
	if err := arena.ready(); err != nil {
		return nil, err
	}
	if handle.owner != arena.owner {
		return nil, errMachineCoroutineOwner
	}
	if handle.index == 0 || uint64(handle.index) > uint64(len(arena.records)) {
		return nil, errMachineCoroutineIndex
	}
	record := &arena.records[handle.index-1]
	if record.live == 0 {
		return nil, errMachineCoroutineIndex
	}
	if handle.generation == 0 || handle.generation != record.generation {
		return nil, errMachineCoroutineGeneration
	}
	return record, nil
}

func (arena *machineCoroutineArena) ready() error {
	if arena == nil || arena.closed {
		return errMachineCoroutineArenaClosed
	}
	if arena.owner == 0 {
		return errMachineCoroutineOwner
	}
	return nil
}

func (arena *machineCoroutineArena) recordSpansValid(record *machineCoroutineRecord) bool {
	return record.registerCount <= record.registerCapacity && record.numberCount <= record.numberCapacity &&
		record.continuationCount <= record.continuationCapacity && record.openCellCount <= record.openCellCapacity &&
		machineCoroutineSpanValid(record.registerStart, record.registerCapacity, len(arena.registers)) &&
		machineCoroutineSpanValid(record.numberStart, record.numberCapacity, len(arena.numberBits)) &&
		machineCoroutineSpanValid(record.continuationStart, record.continuationCapacity, len(arena.continuations)) &&
		machineCoroutineSpanValid(record.openCellStart, record.openCellCapacity, len(arena.openCells))
}

func machineCoroutineSpanValid(start, count uint32, length int) bool {
	return uint64(start)+uint64(count) <= uint64(length)
}

func (arena *machineCoroutineArena) registerSpan(record *machineCoroutineRecord) []slot {
	return arena.registers[int(record.registerStart):int(record.registerStart+record.registerCount)]
}

func (arena *machineCoroutineArena) numberSpan(record *machineCoroutineRecord) []uint64 {
	return arena.numberBits[int(record.numberStart):int(record.numberStart+record.numberCount)]
}

func (arena *machineCoroutineArena) continuationSpan(record *machineCoroutineRecord) []machineContinuation {
	return arena.continuations[int(record.continuationStart):int(record.continuationStart+record.continuationCount)]
}

func (arena *machineCoroutineArena) openCellSpan(record *machineCoroutineRecord) []machineCoroutineOpenCell {
	return arena.openCells[int(record.openCellStart):int(record.openCellStart+record.openCellCount)]
}

func (arena *machineCoroutineArena) clearRecordSpansStopped(record *machineCoroutineRecord) {
	if record == nil || !arena.recordSpansValid(record) {
		return
	}
	clear(arena.registers[int(record.registerStart):int(record.registerStart+record.registerCapacity)])
	clear(arena.numberBits[int(record.numberStart):int(record.numberStart+record.numberCapacity)])
	clear(arena.continuations[int(record.continuationStart):int(record.continuationStart+record.continuationCapacity)])
	clear(arena.openCells[int(record.openCellStart):int(record.openCellStart+record.openCellCapacity)])
	record.registerCount = 0
	record.numberCount = 0
	record.continuationCount = 0
	record.openCellCount = 0
}

func (arena *machineCoroutineArena) reset() {
	if arena == nil || arena.closed {
		return
	}
	arena.clearStorageStopped(false)
	arena.owner = 0
	arena.limit = 0
}

func (arena *machineCoroutineArena) close() {
	if arena == nil || arena.closed {
		return
	}
	arena.clearStorageStopped(true)
	arena.owner = 0
	arena.limit = 0
	arena.closed = true
}

func (arena *machineCoroutineArena) clearStorageStopped(release bool) {
	clear(arena.records)
	clear(arena.registers)
	clear(arena.numberBits)
	clear(arena.continuations)
	clear(arena.openCells)
	clear(arena.free)
	if release {
		arena.records = nil
		arena.registers = nil
		arena.numberBits = nil
		arena.continuations = nil
		arena.openCells = nil
		arena.free = nil
	} else {
		arena.records = arena.records[:0]
		arena.registers = arena.registers[:0]
		arena.numberBits = arena.numberBits[:0]
		arena.continuations = arena.continuations[:0]
		arena.openCells = arena.openCells[:0]
		arena.free = arena.free[:0]
	}
	arena.live = 0
}
