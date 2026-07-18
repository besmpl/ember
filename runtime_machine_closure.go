package ember

import (
	"errors"
	"math"
	"sync/atomic"
)

// machineProtoID is the scalar owner-neutral prototype identifier carried by
// a compact closure record.
type machineProtoID uint32

type machineClosureID uint32
type machineCellID uint32

type machineCaptureMode uint8

const (
	machineCaptureShared machineCaptureMode = iota
	machineCaptureByValue
	machineCaptureOpen
)

// machineCellHandle and machineClosureHandle are boundary handles. Their
// owner cookie and generation make cross-owner and stale access fail closed;
// all fields are scalar.
type machineCellHandle struct {
	owner      uint64
	index      uint32
	generation uint16
}

type machineClosureHandle struct {
	owner      uint64
	index      uint32
	generation uint16
}

// machineCaptureDescriptor is supplied at the stopped capture boundary.
// Shared captures alias an existing cell; by-value captures allocate a new
// cell from value; open captures are deliberately unsupported in this slice.
type machineCaptureDescriptor struct {
	mode  machineCaptureMode
	cell  machineCellHandle
	value slot
}

type machineClosureRecord struct {
	module       programModuleID
	proto        machineProtoID
	captureStart uint32
	captureCount uint32
	generation   uint16
	live         uint8
}

type machineCellRecord struct {
	value        slot
	openRegister uint32
	generation   uint16
	live         uint8
}

type machineOpenCellRecord struct {
	register uint32
	cell     machineCellID
}

// machineClosureArena owns dense closures, cells, and capture spans for one
// Machine. It contains no Go maps, strings, interfaces, or pointers in hot
// records; growth occurs only through stopped methods.
type machineClosureArena struct {
	closures     []machineClosureRecord
	cells        []machineCellRecord
	captureCells []machineCellID
	openCells    []machineOpenCellRecord
	protoCounts  []uint32
	owner        uint64
	closed       bool
}

var (
	errMachineClosureArenaClosed = errors.New("machine closure arena is closed")
	errMachineClosureOwner       = errors.New("machine closure handle belongs to another owner")
	errMachineClosureIndex       = errors.New("machine closure or cell index is invalid")
	errMachineClosureGeneration  = errors.New("machine closure or cell generation is stale")
	errMachineClosureProto       = errors.New("machine closure prototype ID is invalid")
	errMachineClosureModule      = errors.New("machine closure module ID is invalid")
	errMachineClosureCapture     = errors.New("machine closure capture is invalid")
	errMachineClosureOpen        = errors.New("open-frame captures are unsupported")
)

// machineClosureOwnerSequence is only a cold standalone-owner fallback;
// Machine integration supplies an explicit cookie via bindWithOwnerStopped.
var machineClosureOwnerSequence atomic.Uint64

func nextMachineClosureOwner() uint64 {
	owner := machineClosureOwnerSequence.Add(1)
	if owner == 0 {
		owner = machineClosureOwnerSequence.Add(1)
	}
	return owner
}

// bindStopped is a cold convenience for standalone tests/owners that do not
// yet expose a machine owner cookie. Production binding should pass the
// Machine's explicit cookie through bindWithOwnerStopped.
func (arena *machineClosureArena) bindStopped(moduleCount, protoCount int) error {
	return arena.bindWithOwnerStopped(nextMachineClosureOwner(), moduleCount, protoCount)
}

func (arena *machineClosureArena) bindSingleModuleWithOwnerStopped(owner uint64, protoCount uint32) error {
	if arena == nil || arena.closed {
		return errMachineClosureArenaClosed
	}
	if owner == 0 {
		return errMachineClosureIndex
	}
	if cap(arena.protoCounts) < 1 {
		arena.protoCounts = make([]uint32, 1)
	} else {
		arena.protoCounts = arena.protoCounts[:1]
	}
	arena.protoCounts[0] = protoCount
	return arena.rebindOwnerStopped(owner)
}

// bindWithOwnerStopped binds image bounds with an explicit owner cookie and
// clears prior storage. The cookie is scalar and is copied into every handle.
func (arena *machineClosureArena) bindWithOwnerStopped(owner uint64, moduleCount, protoCount int) error {
	if arena == nil || arena.closed {
		return errMachineClosureArenaClosed
	}
	if owner == 0 || moduleCount < 0 || protoCount < 0 || uint64(moduleCount) > uint64(math.MaxUint32) || uint64(protoCount) > uint64(math.MaxUint32) {
		return errMachineClosureIndex
	}
	counts := make([]uint32, moduleCount)
	for index := range counts {
		counts[index] = uint32(protoCount)
	}
	return arena.bindModulesWithOwnerStopped(owner, counts)
}

// bindModulesWithOwnerStopped binds one explicit owner and the prototype
// inventory of every module. Prototype IDs are module-local, so retaining one
// aggregate bound would let a closure from a small module name another
// module's prototype accidentally.
func (arena *machineClosureArena) bindModulesWithOwnerStopped(owner uint64, protoCounts []uint32) error {
	if arena == nil || arena.closed {
		return errMachineClosureArenaClosed
	}
	if owner == 0 || uint64(len(protoCounts)) > uint64(math.MaxUint32) {
		return errMachineClosureIndex
	}
	if cap(arena.protoCounts) < len(protoCounts) {
		arena.protoCounts = make([]uint32, len(protoCounts))
	} else {
		arena.protoCounts = arena.protoCounts[:len(protoCounts)]
	}
	copy(arena.protoCounts, protoCounts)
	return arena.rebindOwnerStopped(owner)
}

func (arena *machineClosureArena) rebindOwnerStopped(owner uint64) error {
	arena.owner = owner
	clear(arena.closures)
	clear(arena.cells)
	clear(arena.captureCells)
	clear(arena.openCells)
	arena.closures = arena.closures[:0]
	arena.cells = arena.cells[:0]
	arena.captureCells = arena.captureCells[:0]
	arena.openCells = arena.openCells[:0]
	return nil
}

func (arena *machineClosureArena) newCellStopped(value slot) (machineCellHandle, error) {
	if err := arena.ready(); err != nil {
		return machineCellHandle{}, err
	}
	if uint64(len(arena.cells)) >= math.MaxUint32 {
		return machineCellHandle{}, errMachineClosureIndex
	}
	arena.cells = append(arena.cells, machineCellRecord{value: value, generation: 1, live: 1})
	return machineCellHandle{owner: arena.owner, index: uint32(len(arena.cells)), generation: 1}, nil
}

// openCellStopped returns the shared cell for one live Machine register,
// creating it only on the first capture. Zero is reserved as the closed-cell
// marker, so register indices are stored one-based.
func (arena *machineClosureArena) openCellStopped(register int, value slot) (machineCellHandle, error) {
	if err := arena.ready(); err != nil {
		return machineCellHandle{}, err
	}
	if register < 0 || uint64(register)+1 > uint64(math.MaxUint32) {
		return machineCellHandle{}, errMachineClosureIndex
	}
	openRegister := uint32(register + 1)
	for index := len(arena.openCells) - 1; index >= 0; index-- {
		open := arena.openCells[index]
		if open.register == openRegister && open.cell != 0 && uint64(open.cell) <= uint64(len(arena.cells)) {
			record := arena.cells[open.cell-1]
			if record.live != 0 && record.openRegister == openRegister {
				return machineCellHandle{owner: arena.owner, index: uint32(open.cell), generation: record.generation}, nil
			}
		}
	}
	if uint64(len(arena.cells)) >= math.MaxUint32 {
		return machineCellHandle{}, errMachineClosureIndex
	}
	arena.cells = append(arena.cells, machineCellRecord{value: value, openRegister: openRegister, generation: 1, live: 1})
	arena.openCells = append(arena.openCells, machineOpenCellRecord{register: openRegister, cell: machineCellID(len(arena.cells))})
	return machineCellHandle{owner: arena.owner, index: uint32(len(arena.cells)), generation: 1}, nil
}

func (arena *machineClosureArena) createClosureStopped(module programModuleID, proto machineProtoID, captures []machineCaptureDescriptor) (machineClosureHandle, error) {
	if err := arena.ready(); err != nil {
		return machineClosureHandle{}, err
	}
	if uint64(module) >= uint64(len(arena.protoCounts)) {
		return machineClosureHandle{}, errMachineClosureModule
	}
	if uint64(proto) >= uint64(arena.protoCounts[module]) {
		return machineClosureHandle{}, errMachineClosureProto
	}
	if uint64(len(arena.closures)) >= math.MaxUint32 || uint64(len(arena.captureCells))+uint64(len(captures)) > math.MaxUint32 {
		return machineClosureHandle{}, errMachineClosureIndex
	}
	byValueCount := 0
	// Validate descriptors before mutating either span or closure storage.
	for _, capture := range captures {
		switch capture.mode {
		case machineCaptureShared:
			if _, err := arena.cellRecord(capture.cell); err != nil {
				return machineClosureHandle{}, errMachineClosureCapture
			}
		case machineCaptureByValue:
			byValueCount++
		case machineCaptureOpen:
			return machineClosureHandle{}, errMachineClosureOpen
		default:
			return machineClosureHandle{}, errMachineClosureCapture
		}
	}
	if uint64(len(arena.cells))+uint64(byValueCount) > math.MaxUint32 {
		return machineClosureHandle{}, errMachineClosureIndex
	}
	start := uint32(len(arena.captureCells))
	for _, capture := range captures {
		if capture.mode == machineCaptureShared {
			arena.captureCells = append(arena.captureCells, machineCellID(capture.cell.index))
			continue
		}
		cell, err := arena.newCellStopped(capture.value)
		if err != nil {
			return machineClosureHandle{}, err
		}
		arena.captureCells = append(arena.captureCells, machineCellID(cell.index))
	}
	arena.closures = append(arena.closures, machineClosureRecord{
		module:       module,
		proto:        proto,
		captureStart: start,
		captureCount: uint32(len(captures)),
		generation:   1,
		live:         1,
	})
	return machineClosureHandle{owner: arena.owner, index: uint32(len(arena.closures)), generation: 1}, nil
}

func (arena *machineClosureArena) closureRecord(handle machineClosureHandle) (machineClosureRecord, error) {
	if err := arena.ready(); err != nil {
		return machineClosureRecord{}, err
	}
	if handle.owner != arena.owner {
		return machineClosureRecord{}, errMachineClosureOwner
	}
	if handle.index == 0 || uint64(handle.index) > uint64(len(arena.closures)) {
		return machineClosureRecord{}, errMachineClosureIndex
	}
	record := arena.closures[handle.index-1]
	if record.live == 0 {
		return machineClosureRecord{}, errMachineClosureIndex
	}
	if record.generation != handle.generation || handle.generation == 0 {
		return machineClosureRecord{}, errMachineClosureGeneration
	}
	return record, nil
}

func (arena *machineClosureArena) cellRecord(handle machineCellHandle) (machineCellRecord, error) {
	if err := arena.ready(); err != nil {
		return machineCellRecord{}, err
	}
	if handle.owner != arena.owner {
		return machineCellRecord{}, errMachineClosureOwner
	}
	if handle.index == 0 || uint64(handle.index) > uint64(len(arena.cells)) {
		return machineCellRecord{}, errMachineClosureIndex
	}
	record := arena.cells[handle.index-1]
	if record.live == 0 {
		return machineCellRecord{}, errMachineClosureIndex
	}
	if record.generation != handle.generation || handle.generation == 0 {
		return machineCellRecord{}, errMachineClosureGeneration
	}
	return record, nil
}

func (arena *machineClosureArena) cellGet(handle machineCellHandle) (slot, error) {
	record, err := arena.cellRecord(handle)
	if err != nil {
		return slotNil, err
	}
	return record.value, nil
}

func (arena *machineClosureArena) cellSet(handle machineCellHandle, value slot) error {
	if _, err := arena.cellRecord(handle); err != nil {
		return err
	}
	arena.cells[handle.index-1].value = value
	return nil
}

func (arena *machineClosureArena) cellGetOpen(handle machineCellHandle, registers []slot) (slot, error) {
	record, err := arena.cellRecord(handle)
	if err != nil {
		return slotNil, err
	}
	if record.openRegister == 0 {
		return record.value, nil
	}
	register := int(record.openRegister - 1)
	if register < 0 || register >= len(registers) {
		return slotNil, errMachineClosureIndex
	}
	return registers[register], nil
}

func (arena *machineClosureArena) cellSetOpen(handle machineCellHandle, registers []slot, value slot) error {
	record, err := arena.cellRecord(handle)
	if err != nil {
		return err
	}
	if record.openRegister == 0 {
		arena.cells[handle.index-1].value = value
		return nil
	}
	register := int(record.openRegister - 1)
	if register < 0 || register >= len(registers) {
		return errMachineClosureIndex
	}
	registers[register] = value
	return nil
}

// closeRegistersStopped snapshots every cell backed by the retiring register
// range before the Machine truncates its scalar stack.
func (arena *machineClosureArena) closeRegistersStopped(registers []slot, start, end int) error {
	if err := arena.ready(); err != nil {
		return err
	}
	if start < 0 || end < start || end > len(registers) {
		return errMachineClosureIndex
	}
	for index := range arena.cells {
		record := &arena.cells[index]
		if record.live == 0 || record.openRegister == 0 {
			continue
		}
		register := int(record.openRegister - 1)
		if register >= start && register < end {
			record.value = registers[register]
			record.openRegister = 0
		}
	}
	return nil
}

func (arena *machineClosureArena) captureCell(handle machineClosureHandle, captureIndex int) (machineCellHandle, error) {
	record, err := arena.closureRecord(handle)
	if err != nil {
		return machineCellHandle{}, err
	}
	if captureIndex < 0 || uint64(captureIndex) >= uint64(record.captureCount) {
		return machineCellHandle{}, errMachineClosureIndex
	}
	cellID := arena.captureCells[int(record.captureStart)+captureIndex]
	if cellID == 0 || uint64(cellID) > uint64(len(arena.cells)) {
		return machineCellHandle{}, errMachineClosureIndex
	}
	cellRecord, err := arena.cellRecord(machineCellHandle{owner: arena.owner, index: uint32(cellID), generation: arena.cells[cellID-1].generation})
	if err != nil {
		return machineCellHandle{}, err
	}
	return machineCellHandle{owner: arena.owner, index: uint32(cellID), generation: cellRecord.generation}, nil
}

func (arena *machineClosureArena) ready() error {
	if arena == nil || arena.closed {
		return errMachineClosureArenaClosed
	}
	if arena.owner == 0 {
		return errMachineClosureArenaClosed
	}
	return nil
}

func (arena *machineClosureArena) reset() {
	arena.resetWithOwnerStopped(nextMachineClosureOwner())
}

func (arena *machineClosureArena) resetWithOwnerStopped(owner uint64) {
	if arena == nil {
		return
	}
	clear(arena.closures)
	clear(arena.cells)
	clear(arena.captureCells)
	clear(arena.openCells)
	arena.closures = arena.closures[:0]
	arena.cells = arena.cells[:0]
	arena.captureCells = arena.captureCells[:0]
	arena.openCells = arena.openCells[:0]
	clear(arena.protoCounts)
	arena.protoCounts = arena.protoCounts[:0]
	arena.owner = owner
	arena.closed = false
}

func (arena *machineClosureArena) close() {
	if arena == nil {
		return
	}
	arena.closures = nil
	arena.cells = nil
	arena.captureCells = nil
	arena.openCells = nil
	arena.protoCounts = nil
	arena.owner = 0
	arena.closed = true
}
