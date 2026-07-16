package ember

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

func machineCoroutineNativeID(nativeID nativeFuncID) bool {
	switch nativeID {
	case nativeFuncCoroutineCreate, nativeFuncCoroutineStatus,
		nativeFuncCoroutineResume, nativeFuncCoroutineYield:
		return true
	default:
		return false
	}
}

type machineCoroutineActionKind uint8

const (
	machineCoroutineActionStart machineCoroutineActionKind = iota + 1
	machineCoroutineActionResume
	machineCoroutineActionYield
	machineCoroutineActionReturn
	machineCoroutineActionError
)

// machineCoroutineAction is the pointer-free dispatch contract between the
// coroutine owner and the generated loop. Values and Go errors are returned
// separately in machineCoroutineExit at the stopped boundary.
type machineCoroutineAction struct {
	kind       machineCoroutineActionKind
	handle     machineCoroutineHandle
	root       machineCoroutineRoot
	frame      machineCoroutineFrameState
	callDepth  uint32
	valueCount uint32
}

type machineCoroutineExit struct {
	action machineCoroutineAction
	// values borrows the transfer buffer supplied to yieldStopped or
	// returnStopped; dispatch must consume it before reusing that buffer.
	values []machineTransferValue
	err    error
}

// machineCoroutineLoopSignal is the typed stop returned through the generated
// dispatch when coroutine.yield suspends the active scalar frame. It is not a
// script failure; the caller-side resume seam consumes it immediately.
type machineCoroutineLoopSignal struct {
	exit      machineCoroutineExit
	hostToken any
}

func (signal *machineCoroutineLoopSignal) Error() string {
	return "machine coroutine yielded"
}

// captureMachineCoroutineValuesStopped removes register-relative number
// ownership from values before they cross a coroutine handoff. Callers may
// reuse destination across yields and resumes.
func captureMachineCoroutineValuesStopped(machine *scalarMachine, destination []machineTransferValue, values []slot) ([]machineTransferValue, error) {
	if machine == nil {
		return destination[:0], errMachineCoroutineSnapshot
	}
	destination = destination[:0]
	for _, value := range values {
		transfer, err := machineCoroutineTransferValue(machine, value)
		if err != nil {
			clear(destination)
			return destination[:0], err
		}
		destination = append(destination, transfer)
	}
	return destination, nil
}

func machineCoroutineTransferValue(machine *scalarMachine, value slot) (machineTransferValue, error) {
	transfer := machineTransferValue{value: value}
	if slotValueKind(value) != NumberKind {
		return transfer, nil
	}
	number, err := machine.number(value)
	if err != nil {
		return machineTransferValue{}, err
	}
	transfer.numberBits = math.Float64bits(number)
	transfer.isNumber = 1
	return transfer, nil
}

// applyMachineCoroutineValuesStopped publishes transfer-safe values into the
// resumed frame. requested follows the Machine result-count convention: -1
// publishes an open result window, while non-negative values are fixed-width.
func applyMachineCoroutineValuesStopped(machine *scalarMachine, destination, requested int, values []machineTransferValue) error {
	if machine == nil || destination < 0 || requested < -1 {
		return errMachineCoroutineSnapshot
	}
	for _, value := range values {
		if value.isNumber == 0 && slotValueKind(value.value) == NumberKind {
			return errMachineCoroutineSnapshot
		}
	}
	resultCount := requested
	if resultCount < 0 {
		resultCount = len(values)
		if err := machine.ensureStack(destination + resultCount); err != nil {
			return err
		}
		machine.activeOpenStart = destination
		machine.activeOpenCount = resultCount
	} else {
		if destination+resultCount > len(machine.registers) {
			return errMachineCoroutineSnapshot
		}
		machine.activeOpenStart = 0
		machine.activeOpenCount = 0
	}
	for index := 0; index < resultCount; index++ {
		value := machineTransferValue{value: slotNil}
		if index < len(values) {
			value = values[index]
		}
		if err := machine.copyTransfer(destination+index, value); err != nil {
			return err
		}
	}
	return nil
}

type machineCoroutineColdEntry struct {
	controller *executionController
	effects    machineRunEffects
	parent     machineCoroutineHandle
	callDepth  uint32
	generation uint16
	active     bool
}

// machineCoroutineController serializes the scalar arena and owns the one
// explicit cold sidecar in which an active invocation may hold Go references.
// Suspended coroutines never retain a context, controller, callback, or error.
type machineCoroutineController struct {
	mu    sync.Mutex
	arena machineCoroutineArena
	cold  []machineCoroutineColdEntry
	pins  map[slot]struct{}
}

func (controller *machineCoroutineController) bindStopped(owner uint64, limit uint32) error {
	if controller == nil {
		return errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	clear(controller.cold)
	controller.cold = controller.cold[:0]
	clear(controller.pins)
	return controller.arena.bindStopped(owner, limit)
}

func (controller *machineCoroutineController) createStopped(root machineCoroutineRoot) (machineCoroutineHandle, error) {
	if controller == nil {
		return machineCoroutineHandle{}, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.arena.createStopped(root)
}

func (controller *machineCoroutineController) status(handle machineCoroutineHandle) (machineCoroutineStatus, error) {
	if controller == nil {
		return machineCoroutineDead, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.arena.statusStopped(handle)
}

// importValueStopped converts an owner-bearing public coroutine value into a
// compact slot only after validating that its arena record is still live.
func (controller *machineCoroutineController) importValueStopped(value Value) (slot, error) {
	if controller == nil {
		return slotNil, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	handle, err := decodeMachineCoroutineValue(value, controller.arena.owner, func(candidate machineCoroutineHandle) bool {
		_, recordErr := controller.arena.record(candidate)
		return recordErr == nil
	})
	if err != nil {
		return slotNil, err
	}
	return slotPackHandle(slotTagCoroutine, handle.index, handle.generation)
}

// exportValueStopped restores the arena owner cookie omitted from a compact
// coroutine slot and rejects malformed, foreign, or stale handles.
func (controller *machineCoroutineController) exportValueStopped(value slot) (Value, error) {
	if controller == nil {
		return Value{}, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	handle, err := controller.handleFromSlotStopped(value)
	if err != nil {
		return Value{}, err
	}
	if controller.pins == nil {
		controller.pins = make(map[slot]struct{})
	}
	controller.pins[value] = struct{}{}
	return machineCoroutineValue(handle)
}

func (controller *machineCoroutineController) hasDeadStopped() bool {
	if controller == nil {
		return false
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	for index := range controller.arena.records {
		record := &controller.arena.records[index]
		if record.live != 0 && record.status == machineCoroutineDead {
			return true
		}
	}
	return false
}

// reclaimUnreachableDeadStopped releases only dead records that have no
// owner-local scalar reference and have never crossed the public Value
// boundary. Exported handles stay pinned because public Value lifetimes are
// deliberately outside the owner's reachability graph.
func (controller *machineCoroutineController) reclaimUnreachableDeadStopped(reachable map[slot]struct{}) error {
	if controller == nil {
		return errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	for index := range controller.arena.records {
		record := &controller.arena.records[index]
		if record.live == 0 || record.status != machineCoroutineDead {
			continue
		}
		value, err := slotPackHandle(slotTagCoroutine, uint32(index+1), record.generation)
		if err != nil {
			return err
		}
		if _, ok := reachable[value]; ok {
			continue
		}
		if _, ok := controller.pins[value]; ok {
			continue
		}
		handle := machineCoroutineHandle{owner: controller.arena.owner, index: uint32(index + 1), generation: record.generation}
		if err := controller.arena.releaseDeadStopped(handle); err != nil {
			return err
		}
	}
	return nil
}

func (controller *machineCoroutineController) pinnedSlotsStopped() []slot {
	if controller == nil {
		return nil
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	values := make([]slot, 0, len(controller.pins))
	for value := range controller.pins {
		values = append(values, value)
	}
	return values
}

func (controller *machineCoroutineController) referencedSlotsStopped(value slot) ([]slot, []machineClosureHandle, error) {
	if controller == nil {
		return nil, nil, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	handle, err := controller.handleFromSlotStopped(value)
	if err != nil {
		return nil, nil, err
	}
	record, err := controller.arena.record(handle)
	if err != nil {
		return nil, nil, err
	}
	values := append([]slot(nil), controller.arena.registerSpan(record)...)
	closures := []machineClosureHandle{record.root.closure, record.frame.closure}
	for _, continuation := range controller.arena.continuationSpan(record) {
		closures = append(closures, continuation.closure)
	}
	return values, closures, nil
}

func (controller *machineCoroutineController) handleFromSlot(value slot) (machineCoroutineHandle, error) {
	if controller == nil {
		return machineCoroutineHandle{}, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.handleFromSlotStopped(value)
}

func (controller *machineCoroutineController) handleFromSlotStopped(value slot) (machineCoroutineHandle, error) {
	index, generation, err := slotValidateHandle(value, slotTagCoroutine)
	if err != nil {
		return machineCoroutineHandle{}, err
	}
	handle := machineCoroutineHandle{owner: controller.arena.owner, index: index, generation: generation}
	if _, err := controller.arena.record(handle); err != nil {
		return machineCoroutineHandle{}, err
	}
	return handle, nil
}

// beginResumeStopped attaches the current invocation's controller and effects.
// It charges the saved scalar call depth against that shared controller. A
// non-zero parent is changed from running to normal until child exit. values
// remains caller-owned; valueCount lets dispatch verify the transfer payload it
// applies at root entry or the saved yield result window.
func (controller *machineCoroutineController) beginResumeStopped(parent, handle machineCoroutineHandle, execution *executionController, effects machineRunEffects, values []machineTransferValue) (machineCoroutineAction, error) {
	if controller == nil {
		return machineCoroutineAction{}, errMachineCoroutineArenaClosed
	}
	if uint64(len(values)) > math.MaxUint32 || !validMachineCoroutineTransfers(values) {
		return machineCoroutineAction{}, errMachineCoroutineSnapshot
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	root, started, depth, err := controller.arena.beginResumeStopped(handle)
	if err != nil {
		return machineCoroutineAction{}, err
	}
	parentNormal := false
	if parent.index != 0 {
		if err := controller.arena.setNormalStopped(parent); err != nil {
			_ = controller.arena.cancelResumeStopped(handle)
			return machineCoroutineAction{}, err
		}
		parentNormal = true
	}
	if execution != nil {
		if err := execution.enterCalls(depth); err != nil {
			if parentNormal {
				_ = controller.arena.restoreRunningStopped(parent)
			}
			_ = controller.arena.cancelResumeStopped(handle)
			return machineCoroutineAction{}, err
		}
	}
	controller.ensureColdStopped(handle.index)
	cold := &controller.cold[handle.index-1]
	if cold.active {
		machineCoroutineLeaveCalls(execution, depth)
		if parentNormal {
			_ = controller.arena.restoreRunningStopped(parent)
		}
		_ = controller.arena.cancelResumeStopped(handle)
		return machineCoroutineAction{}, errMachineCoroutineRunning
	}
	*cold = machineCoroutineColdEntry{controller: execution, effects: effects, parent: parent, callDepth: depth, generation: handle.generation, active: true}
	kind := machineCoroutineActionStart
	var frame machineCoroutineFrameState
	if started {
		kind = machineCoroutineActionResume
		record, _ := controller.arena.record(handle)
		frame = record.frame
	}
	return machineCoroutineAction{kind: kind, handle: handle, root: root, frame: frame, callDepth: depth, valueCount: uint32(len(values))}, nil
}

func (controller *machineCoroutineController) takeSnapshotStopped(handle machineCoroutineHandle, destination *machineCoroutineSnapshot) error {
	if controller == nil {
		return errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if _, err := controller.activeColdStopped(handle); err != nil {
		return err
	}
	return controller.arena.snapshotStopped(handle, destination)
}

func (controller *machineCoroutineController) yieldStopped(handle machineCoroutineHandle, snapshot machineCoroutineSnapshot, values []machineTransferValue) (machineCoroutineExit, error) {
	if controller == nil {
		return machineCoroutineExit{}, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if _, err := controller.activeColdStopped(handle); err != nil {
		return machineCoroutineExit{}, err
	}
	if !validMachineCoroutineTransfers(values) {
		return machineCoroutineExit{}, errMachineCoroutineSnapshot
	}
	if err := controller.arena.suspendStopped(handle, snapshot); err != nil {
		return machineCoroutineExit{}, err
	}
	cold := controller.detachColdStopped(handle)
	machineCoroutineLeaveCalls(cold.controller, cold.callDepth)
	controller.restoreParentStopped(cold.parent)
	return machineCoroutineExit{
		action: machineCoroutineAction{kind: machineCoroutineActionYield, handle: handle, frame: snapshot.frame, callDepth: cold.callDepth},
		values: values,
	}, nil
}

func (controller *machineCoroutineController) returnStopped(handle machineCoroutineHandle, values []machineTransferValue) (machineCoroutineExit, error) {
	return controller.finishStopped(handle, values, nil)
}

func (controller *machineCoroutineController) failStopped(handle machineCoroutineHandle, failure error) (machineCoroutineExit, error) {
	if failure == nil {
		failure = errors.New("machine coroutine failed")
	}
	return controller.finishStopped(handle, nil, failure)
}

func (controller *machineCoroutineController) finishStopped(handle machineCoroutineHandle, values []machineTransferValue, failure error) (machineCoroutineExit, error) {
	if controller == nil {
		return machineCoroutineExit{}, errMachineCoroutineArenaClosed
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if _, err := controller.activeColdStopped(handle); err != nil {
		return machineCoroutineExit{}, err
	}
	if !validMachineCoroutineTransfers(values) {
		return machineCoroutineExit{}, errMachineCoroutineSnapshot
	}
	if _, err := controller.arena.completeStopped(handle); err != nil {
		return machineCoroutineExit{}, err
	}
	cold := controller.detachColdStopped(handle)
	machineCoroutineLeaveCalls(cold.controller, cold.callDepth)
	controller.restoreParentStopped(cold.parent)
	kind := machineCoroutineActionReturn
	if failure != nil {
		kind = machineCoroutineActionError
	}
	return machineCoroutineExit{
		action: machineCoroutineAction{kind: kind, handle: handle, callDepth: cold.callDepth},
		values: values,
		err:    failure,
	}, nil
}

func validMachineCoroutineTransfers(values []machineTransferValue) bool {
	for _, value := range values {
		if value.isNumber == 0 && slotValueKind(value.value) == NumberKind {
			return false
		}
	}
	return true
}

func (controller *machineCoroutineController) activeEffectsStopped(handle machineCoroutineHandle) (machineRunEffects, error) {
	_, effects, err := controller.activeInvocationStopped(handle)
	return effects, err
}

func (controller *machineCoroutineController) activeInvocationStopped(handle machineCoroutineHandle) (*executionController, machineRunEffects, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	cold, err := controller.activeColdStopped(handle)
	if err != nil {
		return nil, machineRunEffects{}, err
	}
	return cold.controller, cold.effects, nil
}

func (controller *machineCoroutineController) setActiveDepthStopped(handle machineCoroutineHandle, depth uint32) error {
	if depth == 0 {
		return errMachineCoroutineSnapshot
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	cold, err := controller.activeColdStopped(handle)
	if err != nil {
		return err
	}
	cold.callDepth = depth
	return nil
}

func (controller *machineCoroutineController) closeCoroutineStopped(handle machineCoroutineHandle) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.arena.closeCoroutineStopped(handle)
}

func (controller *machineCoroutineController) close() {
	if controller == nil {
		return
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	for index := range controller.cold {
		cold := controller.cold[index]
		if cold.active {
			machineCoroutineLeaveCalls(cold.controller, cold.callDepth)
		}
		controller.cold[index] = machineCoroutineColdEntry{}
	}
	controller.cold = nil
	controller.pins = nil
	controller.arena.close()
}

func (controller *machineCoroutineController) ensureColdStopped(index uint32) {
	for uint64(len(controller.cold)) < uint64(index) {
		controller.cold = append(controller.cold, machineCoroutineColdEntry{})
	}
}

func (controller *machineCoroutineController) activeColdStopped(handle machineCoroutineHandle) (*machineCoroutineColdEntry, error) {
	if handle.index == 0 || uint64(handle.index) > uint64(len(controller.cold)) {
		return nil, errMachineCoroutineIndex
	}
	cold := &controller.cold[handle.index-1]
	if !cold.active || cold.generation != handle.generation {
		return nil, errMachineCoroutineNotRunning
	}
	return cold, nil
}

func (controller *machineCoroutineController) detachColdStopped(handle machineCoroutineHandle) machineCoroutineColdEntry {
	cold := controller.cold[handle.index-1]
	controller.cold[handle.index-1] = machineCoroutineColdEntry{}
	return cold
}

func (controller *machineCoroutineController) restoreParentStopped(parent machineCoroutineHandle) {
	if parent.index != 0 {
		_ = controller.arena.restoreRunningStopped(parent)
	}
}

func machineCoroutineLeaveCalls(controller *executionController, count uint32) {
	for range count {
		controller.leaveCall()
	}
}

// captureMachineCoroutineStopped detaches every open cell from the reusable
// Machine register slice. The closed cell remains authoritative while the
// coroutine is suspended; restore refreshes the register from it before
// reopening the alias.
func captureMachineCoroutineStopped(machine *scalarMachine, frame machineCoroutineFrameState, destination *machineCoroutineSnapshot) error {
	if machine == nil || !machine.bound || destination == nil {
		return errMachineCoroutineSnapshot
	}
	frame.module = machine.activeModule
	frame.proto = machine.activeProto
	frame.base = int32(machine.activeBase)
	frame.closure = machine.activeClosure
	frame.varargStart = int32(machine.activeVarargStart)
	frame.varargCount = int32(machine.activeVarargCount)
	frame.openStart = int32(machine.activeOpenStart)
	frame.openCount = int32(machine.activeOpenCount)
	frame.cellStart = int32(machine.activeCellStart)
	frame.callDepth = uint32(len(machine.continuations)) + 1
	frame.resume = machine.resume
	if err := validateMachineCoroutineSnapshot(machineCoroutineSnapshot{frame: frame, registers: machine.registers, numberBits: machine.numberBits, continuations: machine.continuations}); err != nil {
		return err
	}
	machine.transfer = machine.transfer[:0]
	defer func() {
		clear(machine.transfer)
		machine.transfer = machine.transfer[:0]
	}()
	for openIndex, open := range machine.closures.openCells {
		if open.cell == 0 || uint64(open.cell) > uint64(len(machine.closures.cells)) {
			return errMachineCoroutineSnapshot
		}
		cell := &machine.closures.cells[open.cell-1]
		if cell.live == 0 || cell.openRegister != open.register || open.register == 0 || uint64(open.register) > uint64(len(machine.registers)) {
			return errMachineCoroutineSnapshot
		}
		for prior := range openIndex {
			if machine.closures.openCells[prior].cell == open.cell || machine.closures.openCells[prior].register == open.register {
				return errMachineCoroutineSnapshot
			}
		}
		value, err := machine.stableMachineValueStopped(machine.registers[open.register-1])
		if err != nil {
			return err
		}
		machine.transfer = append(machine.transfer, machineTransferValue{value: value})
	}
	destination.frame = frame
	destination.registers = append(destination.registers[:0], machine.registers...)
	destination.numberBits = append(destination.numberBits[:0], machine.numberBits...)
	destination.continuations = append(destination.continuations[:0], machine.continuations...)
	destination.openCells = destination.openCells[:0]
	for _, open := range machine.closures.openCells {
		cell := &machine.closures.cells[open.cell-1]
		destination.openCells = append(destination.openCells, machineCoroutineOpenCell{register: open.register, cell: open.cell, generation: cell.generation})
	}
	machine.window.commit()
	for index, open := range machine.closures.openCells {
		cell := &machine.closures.cells[open.cell-1]
		cell.value = machine.transfer[index].value
		cell.openRegister = 0
	}
	clear(machine.closures.openCells)
	machine.closures.openCells = machine.closures.openCells[:0]
	machine.effects = machineRunEffects{}
	machine.window = executionWindow{}
	return nil
}

func restoreMachineCoroutineStopped(machine *scalarMachine, snapshot machineCoroutineSnapshot) error {
	if machine == nil || !machine.bound || machine.persistentOwner == nil || machine.persistentOwner.image == nil {
		return errMachineCoroutineSnapshot
	}
	if err := validateMachineCoroutineSnapshot(snapshot); err != nil {
		return err
	}
	if len(machine.closures.openCells) != 0 || uint64(snapshot.frame.module) >= uint64(len(machine.persistentOwner.image.modules)) {
		return errMachineCoroutineSnapshot
	}
	image := machine.persistentOwner.image.modules[snapshot.frame.module].code
	if image == nil || int(snapshot.frame.proto) >= len(image.prototypes) {
		return errMachineCoroutineSnapshot
	}
	machine.transfer = machine.transfer[:0]
	defer func() {
		clear(machine.transfer)
		machine.transfer = machine.transfer[:0]
	}()
	for openIndex, open := range snapshot.openCells {
		if open.cell == 0 || uint64(open.cell) > uint64(len(machine.closures.cells)) || open.register == 0 || uint64(open.register) > uint64(len(snapshot.registers)) {
			return errMachineCoroutineSnapshot
		}
		for prior := range openIndex {
			if snapshot.openCells[prior].cell == open.cell || snapshot.openCells[prior].register == open.register {
				return errMachineCoroutineSnapshot
			}
		}
		cell := &machine.closures.cells[open.cell-1]
		if cell.live == 0 || cell.generation != open.generation || cell.openRegister != 0 {
			return errMachineCoroutineSnapshot
		}
		transfer, err := machineCoroutineTransferValue(machine, cell.value)
		if err != nil {
			return errMachineCoroutineSnapshot
		}
		machine.transfer = append(machine.transfer, transfer)
	}
	machine.image = image
	machine.activeModule = snapshot.frame.module
	machine.activeProto = snapshot.frame.proto
	machine.activeBase = int(snapshot.frame.base)
	machine.activeClosure = snapshot.frame.closure
	machine.activeVarargStart = int(snapshot.frame.varargStart)
	machine.activeVarargCount = int(snapshot.frame.varargCount)
	machine.activeOpenStart = int(snapshot.frame.openStart)
	machine.activeOpenCount = int(snapshot.frame.openCount)
	machine.activeCellStart = int(snapshot.frame.cellStart)
	machine.resume = snapshot.frame.resume
	machine.registers = append(machine.registers[:0], snapshot.registers...)
	machine.numberBits = append(machine.numberBits[:0], snapshot.numberBits...)
	machine.continuations = append(machine.continuations[:0], snapshot.continuations...)
	for index, open := range snapshot.openCells {
		cell := &machine.closures.cells[open.cell-1]
		if err := machine.copyTransfer(int(open.register-1), machine.transfer[index]); err != nil {
			return fmt.Errorf("restore machine coroutine capture: %w", err)
		}
		cell.openRegister = open.register
		machine.closures.openCells = append(machine.closures.openCells, machineOpenCellRecord{register: open.register, cell: open.cell})
	}
	return nil
}

func (machine *scalarMachine) callCoroutineNativeStopped(nativeID nativeFuncID, arguments []slot, destination, resultCount, returnPC int) error {
	if machine == nil || machine.persistentOwner == nil {
		return fmt.Errorf("compact Machine coroutine requires a persistent owner")
	}
	switch nativeID {
	case nativeFuncCoroutineCreate:
		return machine.createCoroutineStopped(arguments, destination, resultCount)
	case nativeFuncCoroutineStatus:
		return machine.statusCoroutineStopped(arguments, destination, resultCount)
	case nativeFuncCoroutineResume:
		return machine.resumeCoroutineStopped(arguments, destination, resultCount)
	case nativeFuncCoroutineYield:
		return machine.yieldCoroutineStopped(arguments, destination, resultCount, returnPC)
	default:
		return fmt.Errorf("compact Machine coroutine native ID %d is unsupported", nativeID)
	}
}

func (machine *scalarMachine) createCoroutineStopped(arguments []slot, destination, resultCount int) error {
	if len(arguments) == 0 {
		return fmt.Errorf("coroutine.create: missing function")
	}
	if slotValueKind(arguments[0]) != FunctionKind {
		return fmt.Errorf("coroutine.create: argument is %s, want function", slotValueKind(arguments[0]))
	}
	closure, module, proto, err := machine.closureTarget(arguments[0])
	if err != nil {
		return err
	}
	handle, err := machine.persistentOwner.coroutines.createStopped(machineCoroutineRoot{module: module, proto: proto, closure: closure})
	if err != nil {
		return err
	}
	encoded, err := slotPackHandle(slotTagCoroutine, handle.index, handle.generation)
	if err != nil {
		_ = machine.persistentOwner.coroutines.closeCoroutineStopped(handle)
		return err
	}
	if err := machine.copySlot(destination, encoded); err != nil {
		_ = machine.persistentOwner.coroutines.closeCoroutineStopped(handle)
		return err
	}
	return machine.fillFixedCallResults(destination, resultCount, 1)
}

func (machine *scalarMachine) statusCoroutineStopped(arguments []slot, destination, resultCount int) error {
	handle, err := machine.coroutineArgumentStopped("coroutine.status", arguments)
	if err != nil {
		return err
	}
	status, err := machine.persistentOwner.coroutines.status(handle)
	if err != nil {
		return err
	}
	id, err := machine.strings.internStringStopped(status.String())
	if err != nil {
		return err
	}
	value, err := slotPackHandle(slotTagString, uint32(id), 1)
	if err != nil {
		return err
	}
	if err := machine.copySlot(destination, value); err != nil {
		return err
	}
	return machine.fillFixedCallResults(destination, resultCount, 1)
}

func (machine *scalarMachine) coroutineArgumentStopped(name string, arguments []slot) (machineCoroutineHandle, error) {
	if len(arguments) == 0 {
		return machineCoroutineHandle{}, fmt.Errorf("%s: missing coroutine", name)
	}
	if slotTagOf(arguments[0]) != slotTagCoroutine {
		return machineCoroutineHandle{}, fmt.Errorf("%s: argument 1 is %s, want coroutine", name, slotValueKind(arguments[0]))
	}
	return machine.persistentOwner.coroutines.handleFromSlot(arguments[0])
}

func (machine *scalarMachine) resumeCoroutineStopped(arguments []slot, destination, resultCount int) error {
	handle, err := machine.coroutineArgumentStopped("coroutine.resume", arguments)
	if err != nil {
		return err
	}
	values, err := captureMachineCoroutineValuesStopped(machine, machine.coroutineTransfer, arguments[1:])
	if err != nil {
		return err
	}
	machine.coroutineTransfer = values
	action, err := machine.persistentOwner.coroutines.beginResumeStopped(
		machine.activeCoroutine, handle, machine.window.controller, machine.effects, values,
	)
	if err != nil {
		if isProtectedBoundaryError(err) {
			return err
		}
		return machine.publishCoroutineFailureStopped(destination, resultCount, err)
	}

	callerController := machine.window.controller
	callerEffects := machine.effects
	callerCoroutine := machine.activeCoroutine
	var caller machineCoroutineSnapshot
	if err := captureMachineCoroutineStopped(machine, machineCoroutineFrameState{pc: 0, resumeRegister: 0, resumeCount: 0}, &caller); err != nil {
		_, _ = machine.persistentOwner.coroutines.failStopped(handle, err)
		return err
	}

	childErr := machine.enterCoroutineActionStopped(action, values)
	var exit machineCoroutineExit
	if childErr == nil {
		errorPC, runErr := runGeneratedScalarMachineLoop(machine)
		if signal := new(machineCoroutineLoopSignal); errors.As(runErr, &signal) {
			exit = signal.exit
		} else if runErr != nil {
			childErr = machine.wrapError(errorPC, runErr)
		} else {
			machine.window.commit()
			machine.coroutineTransfer, childErr = captureMachineCoroutineValuesStopped(machine, machine.coroutineTransfer[:0], machine.results[:machine.resultCount])
			if childErr == nil {
				depth := uint32(len(machine.continuations)) + 1
				childErr = machine.persistentOwner.coroutines.setActiveDepthStopped(handle, depth)
			}
			if childErr == nil {
				exit, childErr = machine.persistentOwner.coroutines.returnStopped(handle, machine.coroutineTransfer)
			}
		}
	}
	if childErr != nil {
		machine.window.commit()
		depth := uint32(len(machine.continuations)) + 1
		_ = machine.persistentOwner.coroutines.setActiveDepthStopped(handle, depth)
		exit, _ = machine.persistentOwner.coroutines.failStopped(handle, childErr)
	}

	clear(machine.closures.openCells)
	machine.closures.openCells = machine.closures.openCells[:0]
	if err := restoreMachineCoroutineStopped(machine, caller); err != nil {
		return err
	}
	machine.window = newExecutionWindow(callerController)
	machine.effects = callerEffects
	machine.activeCoroutine = callerCoroutine
	machine.restartPC = 0

	if exit.err != nil {
		if isProtectedBoundaryError(exit.err) {
			return exit.err
		}
		return machine.publishCoroutineFailureStopped(destination, resultCount, exit.err)
	}
	return machine.publishCoroutineSuccessStopped(destination, resultCount, exit.values)
}

func (machine *scalarMachine) enterCoroutineActionStopped(action machineCoroutineAction, values []machineTransferValue) error {
	machine.activeCoroutine = action.handle
	machine.results = machine.results[:0]
	machine.resultCount = 0
	machine.skipCharge = 0
	machine.resume = machineSemanticResume{}
	machine.scratch = slotNil
	execution, effects, err := machine.persistentOwner.coroutines.activeInvocationStopped(action.handle)
	if err != nil {
		return err
	}
	machine.effects = effects
	machine.window = newExecutionWindow(execution)
	if action.kind == machineCoroutineActionResume {
		var snapshot machineCoroutineSnapshot
		if err := machine.persistentOwner.coroutines.takeSnapshotStopped(action.handle, &snapshot); err != nil {
			return err
		}
		if err := restoreMachineCoroutineStopped(machine, snapshot); err != nil {
			return err
		}
		machine.effects = effects
		machine.window = newExecutionWindow(execution)
		machine.restartPC = int(snapshot.frame.pc)
		return applyMachineCoroutineValuesStopped(machine, int(snapshot.frame.resumeRegister), int(snapshot.frame.resumeCount), values)
	}
	if action.kind != machineCoroutineActionStart || uint64(action.root.module) >= uint64(len(machine.persistentOwner.image.modules)) {
		return errMachineCoroutineSnapshot
	}
	image := machine.persistentOwner.image.modules[action.root.module].code
	if image == nil || action.root.proto < 0 || int(action.root.proto) >= len(image.prototypes) {
		return errMachineCoroutineSnapshot
	}
	target := &image.prototypes[action.root.proto]
	varargCount := 0
	if target.variadic && len(values) > target.params {
		varargCount = len(values) - target.params
	}
	machine.image = image
	machine.activeModule = action.root.module
	machine.activeProto = action.root.proto
	machine.activeBase = 0
	machine.activeClosure = action.root.closure
	machine.registers = resizeSlots(machine.registers, target.registers+varargCount)
	machine.continuations = machine.continuations[:0]
	machine.activeVarargStart = target.registers
	machine.activeVarargCount = varargCount
	machine.activeOpenStart = 0
	machine.activeOpenCount = 0
	machine.activeCellStart = len(machine.closures.openCells)
	machine.numberBits = resizeUint64s(machine.numberBits, len(machine.registers)+image.maxResults+1)
	for index := 0; index < target.params && index < len(values); index++ {
		if err := machine.copyTransfer(index, values[index]); err != nil {
			return err
		}
	}
	for index := 0; index < varargCount; index++ {
		if err := machine.copyTransfer(target.registers+index, values[target.params+index]); err != nil {
			return err
		}
	}
	machine.restartPC = 0
	return nil
}

func (machine *scalarMachine) yieldCoroutineStopped(arguments []slot, destination, resultCount, returnPC int) error {
	if machine.activeCoroutine.index == 0 {
		return fmt.Errorf("coroutine.yield: outside coroutine")
	}
	values, err := captureMachineCoroutineValuesStopped(machine, machine.coroutineTransfer[:0], arguments)
	if err != nil {
		return err
	}
	machine.coroutineTransfer = values
	var snapshot machineCoroutineSnapshot
	if err := captureMachineCoroutineStopped(machine, machineCoroutineFrameState{
		pc: int32(returnPC), resumeRegister: int32(destination), resumeCount: int32(resultCount),
	}, &snapshot); err != nil {
		return err
	}
	if err := machine.persistentOwner.coroutines.setActiveDepthStopped(machine.activeCoroutine, snapshot.frame.callDepth); err != nil {
		return err
	}
	exit, err := machine.persistentOwner.coroutines.yieldStopped(machine.activeCoroutine, snapshot, values)
	if err != nil {
		return err
	}
	return &machineCoroutineLoopSignal{exit: exit}
}

func (machine *scalarMachine) publishCoroutineSuccessStopped(destination, requested int, values []machineTransferValue) error {
	machine.transfer = append(machine.transfer[:0], machineTransferValue{value: slotBool(true)})
	machine.transfer = append(machine.transfer, values...)
	err := applyMachineCoroutineValuesStopped(machine, destination, requested, machine.transfer)
	clear(machine.transfer)
	machine.transfer = machine.transfer[:0]
	return err
}

func (machine *scalarMachine) publishCoroutineFailureStopped(destination, requested int, failure error) error {
	message := failure.Error()
	id, err := machine.strings.internStringStopped(message)
	if err != nil {
		return err
	}
	encoded, err := slotPackHandle(slotTagString, uint32(id), 1)
	if err != nil {
		return err
	}
	values := []machineTransferValue{{value: slotBool(false)}, {value: encoded}}
	return applyMachineCoroutineValuesStopped(machine, destination, requested, values)
}
