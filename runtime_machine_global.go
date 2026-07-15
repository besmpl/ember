package ember

import (
	"errors"
	"math"
	"sort"
)

// machineGlobalName is the scalar image/owner bridge for one global name.
// Zero is reserved as an invalid name ID.
type machineGlobalName = machineStringID

// machineGlobalArena is one owner-local dense set of image-declared globals.
// Names are sorted by scalar ID at bind time, making hot lookup a binary search
// over pointer-free storage. Growth and sorting are stopped-boundary work.
type machineGlobalArena struct {
	names    []machineGlobalName
	values   []slot
	versions []uint64
	present  []uint8
	epoch    uint64
	closed   bool
}

var (
	errMachineGlobalArenaClosed   = errors.New("machine global arena is closed")
	errMachineGlobalNameInvalid   = errors.New("machine global name ID is invalid")
	errMachineGlobalNameUnknown   = errors.New("machine global name is not bound")
	errMachineGlobalIndexInvalid  = errors.New("machine global dense index is invalid")
	errMachineGlobalNameDuplicate = errors.New("machine global name is duplicated")
)

// bindNamesStopped binds an image's deterministic scalar name IDs. It rejects
// zero and duplicate names and clears all prior owner values.
func (arena *machineGlobalArena) bindNamesStopped(names []machineGlobalName) error {
	if arena == nil || arena.closed {
		return errMachineGlobalArenaClosed
	}
	for _, name := range names {
		if name == invalidMachineStringID {
			return errMachineGlobalNameInvalid
		}
	}
	candidate := make([]machineGlobalName, len(names))
	copy(candidate, names)
	sort.Slice(candidate, func(left, right int) bool { return candidate[left] < candidate[right] })
	for index := 1; index < len(candidate); index++ {
		if candidate[index] == candidate[index-1] {
			return errMachineGlobalNameDuplicate
		}
	}
	if err := arena.reserveStopped(len(names)); err != nil {
		return err
	}
	arena.names = arena.names[:len(names)]
	copy(arena.names, candidate)
	clear(arena.values)
	clear(arena.versions)
	clear(arena.present)
	arena.values = arena.values[:len(names)]
	arena.versions = arena.versions[:len(names)]
	arena.present = arena.present[:len(names)]
	arena.epoch = 0
	return nil
}

// reserveStopped grows scalar storage for count names without changing
// logical state. It is intended for a stopped Go bind/effect boundary only.
func (arena *machineGlobalArena) reserveStopped(count int) error {
	if arena == nil || arena.closed {
		return errMachineGlobalArenaClosed
	}
	if count < 0 || uint64(count) > uint64(math.MaxInt) {
		return errors.New("machine global count is invalid")
	}
	if cap(arena.names) < count {
		arena.names = append(arena.names, make([]machineGlobalName, count-len(arena.names))...)
	}
	if cap(arena.values) < count {
		arena.values = append(arena.values, make([]slot, count-len(arena.values))...)
	}
	if cap(arena.versions) < count {
		arena.versions = append(arena.versions, make([]uint64, count-len(arena.versions))...)
	}
	if cap(arena.present) < count {
		arena.present = append(arena.present, make([]uint8, count-len(arena.present))...)
	}
	return nil
}

// get returns a present value, including a present nil slot. A known but
// unset global returns (slotNil, false, nil); an unknown name is an error.
func (arena *machineGlobalArena) get(name machineGlobalName) (slot, bool, error) {
	index, err := arena.lookupIndex(name)
	if err != nil {
		return slotNil, false, err
	}
	return arena.getAt(index)
}

// set stores any scalar slot, including slotNil, and marks the global present.
// Existing names only are accepted; this path never grows or allocates.
func (arena *machineGlobalArena) set(name machineGlobalName, value slot) error {
	index, err := arena.lookupIndex(name)
	if err != nil {
		return err
	}
	return arena.setAt(index, value)
}

// getAt is the hot dense-index read. It performs only owner/lifecycle and
// bounds checks; callers that already hold an image global index need not
// repeat name lookup.
func (arena *machineGlobalArena) getAt(index int) (slot, bool, error) {
	if err := arena.checkIndex(index); err != nil {
		return slotNil, false, err
	}
	if arena.present[index] == 0 {
		return slotNil, false, nil
	}
	return arena.values[index], true, nil
}

// setAt is the hot dense-index write. It never grows storage or consults a
// Go map/string.
func (arena *machineGlobalArena) setAt(index int, value slot) error {
	if err := arena.checkIndex(index); err != nil {
		return err
	}
	arena.epoch = nextMachineGlobalVersion(arena.epoch)
	arena.values[index] = value
	arena.versions[index] = arena.epoch
	arena.present[index] = 1
	return nil
}

func (arena *machineGlobalArena) version(name machineGlobalName) (uint64, bool, error) {
	index, err := arena.lookupIndex(name)
	if err != nil {
		return 0, false, err
	}
	return arena.versionAt(index)
}

func (arena *machineGlobalArena) versionAt(index int) (uint64, bool, error) {
	if err := arena.checkIndex(index); err != nil {
		return 0, false, err
	}
	return arena.versions[index], arena.present[index] != 0, nil
}

// reset clears logical state and zeroes old values while retaining capacities
// for another stopped bind.
func (arena *machineGlobalArena) reset() {
	if arena == nil {
		return
	}
	clear(arena.names)
	clear(arena.values)
	clear(arena.versions)
	clear(arena.present)
	arena.names = arena.names[:0]
	arena.values = arena.values[:0]
	arena.versions = arena.versions[:0]
	arena.present = arena.present[:0]
	arena.epoch = 0
	arena.closed = false
}

func (arena *machineGlobalArena) clearValuesStopped() error {
	if arena == nil || arena.closed {
		return errMachineGlobalArenaClosed
	}
	clear(arena.values)
	clear(arena.versions)
	clear(arena.present)
	arena.epoch = 0
	return nil
}

// close releases all scalar backing storage and makes future access fail
// closed. It is idempotent.
func (arena *machineGlobalArena) close() {
	if arena == nil {
		return
	}
	arena.names = nil
	arena.values = nil
	arena.versions = nil
	arena.present = nil
	arena.epoch = 0
	arena.closed = true
}

func (arena *machineGlobalArena) lookupIndex(name machineGlobalName) (int, error) {
	if arena == nil || arena.closed {
		return 0, errMachineGlobalArenaClosed
	}
	if name == invalidMachineStringID {
		return 0, errMachineGlobalNameInvalid
	}
	index := sort.Search(len(arena.names), func(index int) bool { return arena.names[index] >= name })
	if index >= len(arena.names) || arena.names[index] != name {
		return 0, errMachineGlobalNameUnknown
	}
	return index, nil
}

func (arena *machineGlobalArena) checkIndex(index int) error {
	if arena == nil || arena.closed {
		return errMachineGlobalArenaClosed
	}
	if index < 0 || index >= len(arena.names) {
		return errMachineGlobalIndexInvalid
	}
	return nil
}

func nextMachineGlobalVersion(version uint64) uint64 {
	if version == math.MaxUint64 {
		return 1
	}
	return version + 1
}
