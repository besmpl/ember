package ember

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// Machine-owner errors intentionally share the existing lifecycle error
// identities so callers can reason about busy/closed ownership uniformly
// while the compact owner remains private to the new Machine path.
var (
	errMachineOwnerActive   = errRuntimeOwnerActive
	errMachineOwnerBusy     = errRuntimeOwnerBusy
	errMachineOwnerClosed   = errRuntimeOwnerClosed
	errMachineOwnerReleased = errRuntimeOwnerReleased
	errMachineOwnerInvalid  = errRuntimeOwnerInvalid
)

type machineOwnerState uint8

const (
	machineOwnerOpen machineOwnerState = iota
	machineOwnerClosed
)

// machineOwnerStringRange locates one module's image-string translation in
// the flat owner translation array. The record itself is scalar so it can be
// read safely by future Machine kernels.
type machineOwnerStringRange struct {
	offset uint32
	count  uint32
}

// machineOwner is the persistent owner-bound state shell for one immutable
// Program image. The image is the only pointer capability retained by this
// shell; all mutable records and translations are scalar arena data.
type machineOwner struct {
	mu sync.Mutex

	image *programImage
	state machineOwnerState

	activeRuns uint32

	scalarMachine

	globals machineGlobalArena
	modules machineModuleArena
	hosts   machineHostCallableArena

	stringRanges       []machineOwnerStringRange
	stringTranslations []machineStringID
	globalRanges       []machineOwnerStringRange
	globalTranslations []uint32
}

// machineRunLease serializes one active owner run. Its end operation is
// idempotent so defer/recovery paths cannot underflow owner activity.
type machineRunLease struct {
	owner *machineOwner
	once  sync.Once
}

func newMachineOwner(image *programImage) (*machineOwner, error) {
	if err := validateMachineOwnerImage(image); err != nil {
		return nil, err
	}
	owner := &machineOwner{image: image}
	owner.scalarMachine.persistentOwner = owner
	owner.scalarMachine.bound = true
	if err := owner.modules.bindStopped(len(image.modules)); err != nil {
		return nil, fmt.Errorf("bind machine owner modules: %w", err)
	}
	if err := owner.bindImageStrings(); err != nil {
		owner.modules.close()
		owner.strings.close()
		return nil, err
	}
	if err := owner.bindImageGlobals(); err != nil {
		owner.closeStopped()
		return nil, err
	}
	protoCounts := make([]uint32, len(image.modules))
	maxRegisters, maxResults := 0, 0
	for index, module := range image.modules {
		if len(module.code.prototypes) > math.MaxUint32 {
			owner.closeStopped()
			return nil, fmt.Errorf("bind machine owner: module %d prototype inventory exceeds uint32", index)
		}
		protoCounts[index] = uint32(len(module.code.prototypes))
		if module.code.registers > maxRegisters {
			maxRegisters = module.code.registers
		}
		if module.code.maxResults > maxResults {
			maxResults = module.code.maxResults
		}
	}
	ownerCookie := nextMachineClosureOwner()
	if err := owner.closures.bindModulesWithOwnerStopped(ownerCookie, protoCounts); err != nil {
		owner.closeStopped()
		return nil, fmt.Errorf("bind machine owner closures: %w", err)
	}
	if err := owner.hosts.bindStopped(ownerCookie); err != nil {
		owner.closeStopped()
		return nil, fmt.Errorf("bind machine owner host callables: %w", err)
	}
	owner.registers = make([]slot, 0, maxRegisters)
	owner.results = make([]slot, 0, maxResults)
	owner.numberBits = make([]uint64, 0, maxRegisters+maxResults+1)
	return owner, nil
}

func validateMachineOwnerImage(image *programImage) error {
	if image == nil {
		return fmt.Errorf("bind machine owner: nil program image")
	}
	if uint64(len(image.modules)) > uint64(math.MaxUint32) {
		return fmt.Errorf("bind machine owner: module count %d overflows dense IDs", len(image.modules))
	}
	seenKeys := make(map[moduleKey]struct{}, len(image.modules))
	for index, module := range image.modules {
		if module.moduleID != programModuleID(index) {
			return fmt.Errorf("bind machine owner: module %d has ID %d", index, module.moduleID)
		}
		if _, ok := seenKeys[module.key]; ok {
			return fmt.Errorf("bind machine owner: duplicate module key %s", module.key.String())
		}
		seenKeys[module.key] = struct{}{}
		if module.code == nil {
			return fmt.Errorf("bind machine owner: module %d has nil code image", index)
		}
		if err := validateMachineOwnerCodeStrings(module.code, index); err != nil {
			return err
		}
	}
	for index, entrypoint := range image.entrypoints {
		if uint64(entrypoint.moduleID) >= uint64(len(image.modules)) {
			return fmt.Errorf("bind machine owner: entrypoint %d references module %d outside image", index, entrypoint.moduleID)
		}
	}
	if image.moduleIDs != nil {
		for key, moduleID := range image.moduleIDs {
			if uint64(moduleID) >= uint64(len(image.modules)) || image.modules[moduleID].key != key {
				return fmt.Errorf("bind machine owner: module index for %s is inconsistent", key.String())
			}
		}
	}
	return nil
}

func validateMachineOwnerCodeStrings(code *codeImage, moduleIndex int) error {
	for stringIndex, record := range code.stringRecords {
		end := uint64(record.offset) + uint64(record.length)
		if end > uint64(len(code.stringData)) {
			return fmt.Errorf("bind machine owner: module %d string %d has invalid byte span", moduleIndex, stringIndex+1)
		}
	}
	return nil
}

func (owner *machineOwner) bindImageStrings() error {
	if owner == nil || owner.image == nil {
		return errMachineOwnerInvalid
	}
	totalBytes := uint64(0)
	totalStrings := uint64(0)
	for _, module := range owner.image.modules {
		totalBytes += uint64(len(module.code.stringData))
		totalStrings += uint64(len(module.code.stringRecords))
		if totalBytes > math.MaxUint32 || totalStrings > math.MaxUint32-uint64(len(owner.image.modules)) {
			return errors.New("bind machine owner: image string capacity overflows uint32")
		}
	}
	if totalStrings += uint64(len(owner.image.modules)); totalStrings > math.MaxUint32 {
		return errors.New("bind machine owner: image string translation capacity overflows uint32")
	}
	if totalBytes > uint64(math.MaxInt) || totalStrings > uint64(math.MaxInt) {
		return errors.New("bind machine owner: image string capacity overflows int")
	}
	if err := owner.strings.reserveStopped(int(totalBytes), int(totalStrings)); err != nil {
		return fmt.Errorf("bind machine owner strings: %w", err)
	}
	owner.stringRanges = make([]machineOwnerStringRange, len(owner.image.modules))
	owner.stringTranslations = make([]machineStringID, int(totalStrings))
	translationOffset := uint64(0)
	for moduleIndex, module := range owner.image.modules {
		count := uint64(len(module.code.stringRecords)) + 1
		owner.stringRanges[moduleIndex] = machineOwnerStringRange{
			offset: uint32(translationOffset),
			count:  uint32(count),
		}
		for stringIndex, record := range module.code.stringRecords {
			start := int(record.offset)
			end := start + int(record.length)
			id, err := owner.strings.internBytesStopped(module.code.stringData[start:end])
			if err != nil {
				return fmt.Errorf("bind machine owner: module %d string %d: %w", moduleIndex, stringIndex+1, err)
			}
			owner.stringTranslations[int(translationOffset)+stringIndex+1] = id
		}
		translationOffset += count
	}
	return nil
}

func (owner *machineOwner) bindImageGlobals() error {
	if owner == nil || owner.image == nil {
		return errMachineOwnerInvalid
	}
	names := owner.image.globalNames
	if names == nil {
		var err error
		names, err = programImageGlobalNames(owner.image.modules)
		if err != nil {
			return fmt.Errorf("bind machine owner globals: %w", err)
		}
	}
	ownerNames := make([]machineGlobalName, len(names))
	for index, name := range names {
		id, err := owner.strings.internStringStopped(name)
		if err != nil {
			return fmt.Errorf("bind machine owner global %q: %w", name, err)
		}
		ownerNames[index] = id
	}
	if err := owner.globals.bindNamesStopped(ownerNames); err != nil {
		return fmt.Errorf("bind machine owner globals: %w", err)
	}
	owner.globalRanges = make([]machineOwnerStringRange, len(owner.image.modules))
	total := 0
	for _, module := range owner.image.modules {
		total += len(module.code.globalNames)
	}
	owner.globalTranslations = make([]uint32, total)
	offset := 0
	for moduleIndex, module := range owner.image.modules {
		owner.globalRanges[moduleIndex] = machineOwnerStringRange{offset: uint32(offset), count: uint32(len(module.code.globalNames))}
		for globalIndex, imageID := range module.code.globalNames {
			ownerID, err := owner.translateImageStringIDStopped(programModuleID(moduleIndex), imageID)
			if err != nil {
				return fmt.Errorf("bind machine owner: module %d global %d: %w", moduleIndex, globalIndex, err)
			}
			dense, err := owner.globals.lookupIndex(ownerID)
			if err != nil {
				return fmt.Errorf("bind machine owner: module %d global %d: %w", moduleIndex, globalIndex, err)
			}
			owner.globalTranslations[offset+globalIndex] = uint32(dense)
		}
		offset += len(module.code.globalNames)
	}
	return nil
}

// translateImageStringID converts a module-local immutable image string ID
// into an owner-local string ID. Both indices are checked at this cold seam;
// the returned ID is scalar and valid only while the owner remains open.
func (owner *machineOwner) translateImageStringID(moduleID programModuleID, imageStringID machineStringID) (machineStringID, error) {
	if owner == nil {
		return invalidMachineStringID, errMachineOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == machineOwnerClosed {
		return invalidMachineStringID, errMachineOwnerClosed
	}
	return owner.translateImageStringIDStopped(moduleID, imageStringID)
}

func (owner *machineOwner) translateImageStringIDStopped(moduleID programModuleID, imageStringID machineStringID) (machineStringID, error) {
	if uint64(moduleID) >= uint64(len(owner.stringRanges)) {
		return invalidMachineStringID, fmt.Errorf("machine owner: module %d is invalid", moduleID)
	}
	if imageStringID == invalidMachineStringID {
		return invalidMachineStringID, errors.New("machine owner: image string ID is invalid")
	}
	rangeInfo := owner.stringRanges[moduleID]
	if uint64(imageStringID) >= uint64(rangeInfo.count) {
		return invalidMachineStringID, fmt.Errorf("machine owner: image string ID %d is invalid for module %d", imageStringID, moduleID)
	}
	translationIndex := uint64(rangeInfo.offset) + uint64(imageStringID)
	if translationIndex >= uint64(len(owner.stringTranslations)) {
		return invalidMachineStringID, errors.New("machine owner: stale image string translation")
	}
	ownerStringID := owner.stringTranslations[translationIndex]
	if ownerStringID == invalidMachineStringID {
		return invalidMachineStringID, errors.New("machine owner: stale image string translation")
	}
	if _, ok := owner.strings.lookup(ownerStringID); !ok {
		return invalidMachineStringID, errors.New("machine owner: stale owner string ID")
	}
	return ownerStringID, nil
}

func (owner *machineOwner) globalIndexStopped(moduleID programModuleID, imageIndex int32) (int, error) {
	if owner == nil || uint64(moduleID) >= uint64(len(owner.globalRanges)) || imageIndex < 0 {
		return 0, errMachineGlobalIndexInvalid
	}
	rangeInfo := owner.globalRanges[moduleID]
	if uint64(imageIndex) >= uint64(rangeInfo.count) {
		return 0, errMachineGlobalIndexInvalid
	}
	index := uint64(rangeInfo.offset) + uint64(imageIndex)
	if index >= uint64(len(owner.globalTranslations)) {
		return 0, errMachineGlobalIndexInvalid
	}
	return int(owner.globalTranslations[index]), nil
}

func (owner *machineOwner) beginRun() (*machineRunLease, error) {
	if owner == nil {
		return nil, errMachineOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == machineOwnerClosed {
		return nil, errMachineOwnerClosed
	}
	if owner.activeRuns != 0 {
		return nil, errMachineOwnerBusy
	}
	owner.activeRuns = 1
	return &machineRunLease{owner: owner}, nil
}

func (owner *machineOwner) executeRoot(moduleID programModuleID, controller *executionController, effects ...machineRunEffects) error {
	lease, err := owner.beginRun()
	if err != nil {
		return err
	}
	defer lease.end()
	return owner.executeRootStopped(moduleID, controller, firstMachineRunEffects(effects))
}

func (owner *machineOwner) executeRootStopped(moduleID programModuleID, controller *executionController, effects machineRunEffects) error {
	return owner.executeStopped(moduleID, 0, machineClosureHandle{}, nil, controller, effects)
}

func (owner *machineOwner) executeClosure(callable slot, args []slot, controller *executionController, effects ...machineRunEffects) error {
	lease, err := owner.beginRun()
	if err != nil {
		return err
	}
	defer lease.end()
	return owner.executeClosureStopped(callable, args, controller, firstMachineRunEffects(effects))
}

func (owner *machineOwner) executeClosureStopped(callable slot, args []slot, controller *executionController, effects machineRunEffects) error {
	closure, moduleID, protoID, err := owner.scalarMachine.closureTarget(callable)
	if err != nil {
		return err
	}
	return owner.executeStopped(moduleID, protoID, closure, args, controller, effects)
}

func (owner *machineOwner) executeStopped(moduleID programModuleID, protoID int32, closure machineClosureHandle, args []slot, controller *executionController, effects machineRunEffects) error {
	if owner == nil || owner.image == nil || uint64(moduleID) >= uint64(len(owner.image.modules)) {
		return errMachineOwnerInvalid
	}
	image := owner.image.modules[moduleID].code
	if image == nil || !image.eligible || protoID < 0 || int(protoID) >= len(image.prototypes) {
		return fmt.Errorf("execute machine owner: module %d prototype %d is ineligible", moduleID, protoID)
	}
	target := &image.prototypes[protoID]
	argCount := len(args)
	varargCount := 0
	if target.variadic && argCount > target.params {
		varargCount = argCount - target.params
	}
	machine := &owner.scalarMachine
	machine.image = image
	machine.activeModule = moduleID
	machine.activeProto = protoID
	machine.activeBase = 0
	machine.activeClosure = closure
	machine.registers = resizeSlots(machine.registers, target.registers+varargCount)
	machine.results = machine.results[:0]
	machine.continuations = machine.continuations[:0]
	machine.transfer = machine.transfer[:0]
	machine.captureScratch = machine.captureScratch[:0]
	machine.resultCount = 0
	machine.skipCharge = 0
	machine.scratch = slotNil
	machine.activeVarargStart = target.registers
	machine.activeVarargCount = varargCount
	machine.activeOpenStart = 0
	machine.activeOpenCount = 0
	machine.activeCellStart = len(machine.closures.openCells)
	machine.numberBits = resizeUint64s(machine.numberBits, len(machine.registers)+image.maxResults+1)
	for index := 0; index < target.params && index < argCount; index++ {
		if err := machine.copySlot(index, args[index]); err != nil {
			return err
		}
	}
	for index := 0; index < varargCount; index++ {
		if err := machine.copySlot(target.registers+index, args[target.params+index]); err != nil {
			return err
		}
	}
	machine.window = newExecutionWindow(controller)
	machine.effects = effects
	defer func() { machine.effects = machineRunEffects{} }()
	defer machine.window.commit()
	if controller != nil {
		if err := controller.enterCall(); err != nil {
			return machine.wrapError(0, err)
		}
		defer controller.leaveCall()
	}
	errorPC, err := runGeneratedScalarMachineLoop(machine)
	if err != nil {
		for range machine.continuations {
			if controller != nil {
				controller.leaveCall()
			}
		}
		machine.continuations = machine.continuations[:0]
		if closeErr := machine.closeAllCapturesStopped(); closeErr != nil {
			return machine.wrapError(errorPC, closeErr)
		}
		return machine.wrapError(errorPC, err)
	}
	return nil
}

func (owner *machineOwner) resultAt(index int) (slot, error) {
	if owner == nil {
		return slotNil, errMachineOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == machineOwnerClosed {
		return slotNil, errMachineOwnerClosed
	}
	if owner.activeRuns != 0 {
		return slotNil, errMachineOwnerBusy
	}
	if index < 0 || index >= owner.resultCount || index >= len(owner.results) {
		return slotNil, fmt.Errorf("machine owner result %d is out of range", index)
	}
	return owner.results[index], nil
}

func (owner *machineOwner) loadModule(moduleID programModuleID, controller *executionController, effects ...machineRunEffects) (slot, error) {
	lease, err := owner.beginRun()
	if err != nil {
		return slotNil, err
	}
	defer lease.end()
	export, _, err := owner.loadModuleStopped(moduleID, controller, firstMachineRunEffects(effects))
	return export, err
}

func (owner *machineOwner) loadModuleStopped(moduleID programModuleID, controller *executionController, effects machineRunEffects) (slot, bool, error) {
	export, cached, err := owner.modules.begin(moduleID)
	if err != nil || cached {
		return export, false, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = owner.modules.abort(moduleID)
		}
	}()
	if err := owner.executeStopped(moduleID, 0, machineClosureHandle{}, nil, controller, effects); err != nil {
		return slotNil, false, err
	}
	export = slotNil
	if owner.resultCount != 0 {
		export, err = owner.stableMachineValueStopped(owner.results[0])
		if err != nil {
			return slotNil, false, err
		}
	}
	if err := owner.modules.finish(moduleID, export); err != nil {
		return slotNil, false, err
	}
	complete = true
	return export, true, nil
}

func firstMachineRunEffects(effects []machineRunEffects) machineRunEffects {
	if len(effects) == 0 {
		return machineRunEffects{}
	}
	return effects[0]
}

func (lease *machineRunLease) end() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.owner == nil {
			return
		}
		lease.owner.mu.Lock()
		if lease.owner.activeRuns != 0 {
			lease.owner.activeRuns--
		}
		lease.owner.mu.Unlock()
	})
}

func (lease *machineRunLease) release() {
	lease.end()
}

func (owner *machineOwner) close() error {
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == machineOwnerClosed {
		return nil
	}
	if owner.activeRuns != 0 {
		return errMachineOwnerActive
	}
	owner.state = machineOwnerClosed
	owner.image = nil
	owner.stringRanges = nil
	owner.stringTranslations = nil
	owner.globalRanges = nil
	owner.globalTranslations = nil
	owner.closeStopped()
	return nil
}

func (owner *machineOwner) closeStopped() {
	owner.scalarMachine.image = nil
	owner.registers = nil
	owner.results = nil
	owner.continuations = nil
	owner.transfer = nil
	owner.captureScratch = nil
	owner.numberBits = nil
	owner.tableNumbers = nil
	owner.strings.close()
	owner.globals.close()
	owner.tables.close()
	owner.closures.close()
	owner.modules.close()
	owner.hosts.close()
	owner.scalarMachine.persistentOwner = nil
	owner.scalarMachine.bound = false
}
