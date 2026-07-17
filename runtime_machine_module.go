package ember

import (
	"errors"
	"math"
)

// machineModuleLoadState is the scalar lifecycle for one owner-local module.
type machineModuleLoadState uint8

const (
	machineModuleUnloaded machineModuleLoadState = iota
	machineModuleLoading
	machineModuleLoaded
)

// Compatibility aliases keep the state vocabulary explicit at call sites.
const (
	machineModuleStateUnloaded = machineModuleUnloaded
	machineModuleStateLoading  = machineModuleLoading
	machineModuleStateLoaded   = machineModuleLoaded
)

// machineModuleArena stores module state and exports for one owner. All hot
// records and the loading stack are scalar elements; error text is generated
// only at the cold/stopped boundary.
type machineModuleArena struct {
	states  []machineModuleLoadState
	exports []slot
	loading []programModuleID
	closed  bool
}

var (
	errMachineModuleArenaClosed  = errors.New("machine module arena is closed")
	errMachineModuleIndexInvalid = errors.New("machine module ID is out of bounds")
	errMachineModuleTransition   = errors.New("machine module lifecycle transition is invalid")
	errMachineModuleCycle        = errors.New("machine module loading cycle")
)

// bindStopped binds a fixed module inventory and clears all prior state. The
// loading stack is reserved to the inventory count so begin/finish/abort do
// not grow storage during execution.
func (arena *machineModuleArena) bindStopped(moduleCount int) error {
	if arena == nil || arena.closed {
		return errMachineModuleArenaClosed
	}
	if moduleCount < 0 || uint64(moduleCount) > uint64(math.MaxInt) {
		return errors.New("machine module count is invalid")
	}
	if cap(arena.states) < moduleCount {
		arena.states = append(arena.states, make([]machineModuleLoadState, moduleCount-len(arena.states))...)
	}
	if cap(arena.exports) < moduleCount {
		arena.exports = append(arena.exports, make([]slot, moduleCount-len(arena.exports))...)
	}
	if cap(arena.loading) < moduleCount {
		arena.loading = append(arena.loading, make([]programModuleID, moduleCount-len(arena.loading))...)
	}
	arena.states = arena.states[:moduleCount]
	arena.exports = arena.exports[:moduleCount]
	clear(arena.states)
	clear(arena.exports)
	arena.loading = arena.loading[:0]
	return nil
}

// state performs a checked scalar state read.
func (arena *machineModuleArena) state(id programModuleID) (machineModuleLoadState, error) {
	index, err := arena.check(id)
	if err != nil {
		return machineModuleUnloaded, err
	}
	return arena.states[index], nil
}

// begin starts loading id. If the module is already loaded, it returns its
// cached export and cached=true. A loading module is a cycle.
func (arena *machineModuleArena) begin(id programModuleID) (export slot, cached bool, err error) {
	index, err := arena.check(id)
	if err != nil {
		return slotNil, false, err
	}
	switch arena.states[index] {
	case machineModuleLoaded:
		return arena.exports[index], true, nil
	case machineModuleLoading:
		return slotNil, false, errMachineModuleCycle
	case machineModuleUnloaded:
		arena.states[index] = machineModuleLoading
		arena.loading = append(arena.loading, id)
		return slotNil, false, nil
	default:
		return slotNil, false, errMachineModuleTransition
	}
}

// finish commits an export, including slotNil, and returns the named module to
// the loaded state. Independent resumable initializers may finish in any order.
func (arena *machineModuleArena) finish(id programModuleID, export slot) error {
	index, err := arena.check(id)
	if err != nil {
		return err
	}
	if arena.states[index] != machineModuleLoading || !arena.removeLoading(id) {
		return errMachineModuleTransition
	}
	arena.exports[index] = export
	arena.states[index] = machineModuleLoaded
	return nil
}

// abort drops an in-progress module and makes it eligible for a later retry.
func (arena *machineModuleArena) abort(id programModuleID) error {
	index, err := arena.check(id)
	if err != nil {
		return err
	}
	if arena.states[index] != machineModuleLoading || !arena.removeLoading(id) {
		return errMachineModuleTransition
	}
	arena.states[index] = machineModuleUnloaded
	arena.exports[index] = slotNil
	return nil
}

// export returns a cached export only after successful finish. Unloaded
// modules return absent; loading modules return a transition error.
func (arena *machineModuleArena) export(id programModuleID) (slot, bool, error) {
	index, err := arena.check(id)
	if err != nil {
		return slotNil, false, err
	}
	switch arena.states[index] {
	case machineModuleLoaded:
		return arena.exports[index], true, nil
	case machineModuleUnloaded:
		return slotNil, false, nil
	case machineModuleLoading:
		return slotNil, false, errMachineModuleTransition
	default:
		return slotNil, false, errMachineModuleTransition
	}
}

func (arena *machineModuleArena) loadingDepth() int {
	if arena == nil || arena.closed {
		return 0
	}
	return len(arena.loading)
}

func (arena *machineModuleArena) removeLoading(id programModuleID) bool {
	for index := len(arena.loading) - 1; index >= 0; index-- {
		if arena.loading[index] != id {
			continue
		}
		copy(arena.loading[index:], arena.loading[index+1:])
		arena.loading[len(arena.loading)-1] = 0
		arena.loading = arena.loading[:len(arena.loading)-1]
		return true
	}
	return false
}

func (arena *machineModuleArena) check(id programModuleID) (int, error) {
	if arena == nil || arena.closed {
		return 0, errMachineModuleArenaClosed
	}
	index := uint64(id)
	if index >= uint64(len(arena.states)) {
		return 0, errMachineModuleIndexInvalid
	}
	return int(index), nil
}

// reset clears state while retaining scalar capacity for another stopped bind.
func (arena *machineModuleArena) reset() {
	if arena == nil {
		return
	}
	clear(arena.states)
	clear(arena.exports)
	clear(arena.loading)
	arena.states = arena.states[:0]
	arena.exports = arena.exports[:0]
	arena.loading = arena.loading[:0]
	arena.closed = false
}

// close releases all storage and makes future access fail closed. It is
// idempotent.
func (arena *machineModuleArena) close() {
	if arena == nil {
		return
	}
	arena.states = nil
	arena.exports = nil
	arena.loading = nil
	arena.closed = true
}
