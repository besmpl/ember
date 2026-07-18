package ember

import (
	"errors"
	"math"
)

// machineStringID is an owner-local dense handle. Zero is reserved as the
// invalid/missing value so an ID can also be used as an empty hash-table slot.
type machineStringID uint32

const invalidMachineStringID machineStringID = 0

// machineStringRecord contains all metadata needed to read an interned
// string. Its fields deliberately contain no Go pointers; the bytes live in
// machineStringArena.data.
type machineStringRecord struct {
	offset uint32
	length uint32
	hash   uint64
}

// machineStringArena owns the strings for one Machine. The index is an
// open-addressed table of IDs rather than a Go map, so stable ID reads only
// touch scalar records and bytes. The stopped suffix on mutating methods is a
// reminder that they may grow backing storage and must run at a stopped Go
// boundary, never from the guest kernel.
type machineStringArena struct {
	records []machineStringRecord
	data    []byte
	index   []machineStringID
	closed  bool
}

var errMachineStringArenaClosed = errors.New("machine string arena is closed")

const (
	machineStringHashOffset   uint64 = 14695981039346656037
	machineStringHashPrime    uint64 = 1099511628211
	machineStringLoadNum             = 7
	machineStringLoadDen             = 10
	machineStringInitialIndex        = 8
)

// internBytesStopped returns the existing ID for value or appends one copy
// and returns a new dense ID. Input bytes are copied exactly, including empty,
// NUL, and non-UTF-8 bytes.
func (arena *machineStringArena) internBytesStopped(value []byte) (machineStringID, error) {
	if arena == nil || arena.closed {
		return invalidMachineStringID, errMachineStringArenaClosed
	}
	hash := machineStringHash(value)
	if id := arena.find(value, hash); id != invalidMachineStringID {
		return id, nil
	}
	if uint64(len(arena.records)) == math.MaxUint32 {
		return invalidMachineStringID, errors.New("machine string arena has too many strings")
	}
	if uint64(len(arena.data))+uint64(len(value)) > math.MaxUint32 {
		return invalidMachineStringID, errors.New("machine string arena byte capacity overflows uint32")
	}
	if err := arena.reserveIndexStopped(len(arena.records) + 1); err != nil {
		return invalidMachineStringID, err
	}

	offset := uint32(len(arena.data))
	length := uint32(len(value))
	arena.data = append(arena.data, value...)
	arena.records = append(arena.records, machineStringRecord{offset: offset, length: length, hash: hash})
	id := machineStringID(len(arena.records))
	arena.insertIndex(id, hash)
	return id, nil
}

// internStringStopped is the string-input counterpart to internBytesStopped.
// The conversion is intentionally at this stopped boundary; stable reads do
// not construct Go strings.
func (arena *machineStringArena) internStringStopped(value string) (machineStringID, error) {
	return arena.internBytesStopped([]byte(value))
}

// reserveStopped reserves room for the requested number of bytes and records
// before a burst starts. It never changes logical contents.
func (arena *machineStringArena) reserveStopped(byteCapacity, stringCapacity int) error {
	if arena == nil || arena.closed {
		return errMachineStringArenaClosed
	}
	if byteCapacity < len(arena.data) || stringCapacity < len(arena.records) || byteCapacity < 0 || stringCapacity < 0 {
		return errors.New("machine string arena reservation is smaller than current contents")
	}
	if uint64(byteCapacity) > math.MaxUint32 || uint64(stringCapacity) > math.MaxUint32 {
		return errors.New("machine string arena reservation overflows uint32")
	}
	if cap(arena.data) < byteCapacity {
		grown := make([]byte, len(arena.data), byteCapacity)
		copy(grown, arena.data)
		arena.data = grown
	}
	if cap(arena.records) < stringCapacity {
		grown := make([]machineStringRecord, len(arena.records), stringCapacity)
		copy(grown, arena.records)
		arena.records = grown
	}
	return arena.reserveIndexStopped(stringCapacity)
}

// lookup performs a checked scalar lookup. It validates both the record index
// and its byte span, so a malformed ID or record cannot panic the caller.
func (arena *machineStringArena) lookup(id machineStringID) (machineStringRecord, bool) {
	if arena == nil || arena.closed || id == invalidMachineStringID {
		return machineStringRecord{}, false
	}
	index := uint64(id - 1)
	if index >= uint64(len(arena.records)) {
		return machineStringRecord{}, false
	}
	record := arena.records[index]
	end := uint64(record.offset) + uint64(record.length)
	if end > uint64(len(arena.data)) {
		return machineStringRecord{}, false
	}
	return record, true
}

// bytesFor returns the exact bytes for id without consulting the interning
// index or constructing a Go string.
func (arena *machineStringArena) bytesFor(id machineStringID) ([]byte, bool) {
	record, ok := arena.lookup(id)
	if !ok {
		return nil, false
	}
	start := int(record.offset)
	return arena.data[start : start+int(record.length)], true
}

// equal reports content identity for two valid owner-local IDs. IDs from
// different arenas must not be compared through this method.
func (arena *machineStringArena) equal(left, right machineStringID) bool {
	if left == invalidMachineStringID || left != right {
		return false
	}
	_, ok := arena.lookup(left)
	return ok
}

// reset clears all logical state while retaining zeroed backing capacity for
// reuse by a later stopped bind. In particular, old string bytes are wiped.
func (arena *machineStringArena) reset() {
	if arena == nil {
		return
	}
	clear(arena.records)
	clear(arena.data)
	clear(arena.index)
	arena.records = arena.records[:0]
	arena.data = arena.data[:0]
	arena.index = arena.index[:0]
	arena.closed = false
}

// close releases all backing storage and makes future mutation fail closed.
// It is idempotent.
func (arena *machineStringArena) close() {
	if arena == nil {
		return
	}
	arena.records = nil
	arena.data = nil
	arena.index = nil
	arena.closed = true
}

func (arena *machineStringArena) find(value []byte, hash uint64) machineStringID {
	if len(arena.index) == 0 {
		return invalidMachineStringID
	}
	mask := len(arena.index) - 1
	for probe := 0; probe < len(arena.index); probe++ {
		slot := (int(hash) + probe) & mask
		id := arena.index[slot]
		if id == invalidMachineStringID {
			return invalidMachineStringID
		}
		record, ok := arena.lookup(id)
		if ok && record.hash == hash {
			start := int(record.offset)
			end := int(uint64(record.offset) + uint64(record.length))
			if bytesEqual(arena.data[start:end], value) {
				return id
			}
		}
	}
	return invalidMachineStringID
}

func (arena *machineStringArena) insertIndex(id machineStringID, hash uint64) {
	mask := len(arena.index) - 1
	for probe := 0; probe < len(arena.index); probe++ {
		slot := (int(hash) + probe) & mask
		if arena.index[slot] == invalidMachineStringID {
			arena.index[slot] = id
			return
		}
	}
	// reserveIndexStopped leaves an empty slot by construction. Keep this
	// panic unreachable rather than silently losing an interned string if that
	// invariant is ever broken during development.
	panic("machine string arena index is full")
}

func (arena *machineStringArena) reserveIndexStopped(stringCount int) error {
	if stringCount < 0 || uint64(stringCount) > math.MaxUint32 {
		return errors.New("machine string arena string count overflows uint32")
	}
	needed := stringCount
	if needed == 0 {
		return nil
	}
	capacity := len(arena.index)
	if capacity >= needed && needed <= capacity*machineStringLoadNum/machineStringLoadDen {
		return nil
	}
	if capacity < machineStringInitialIndex {
		capacity = machineStringInitialIndex
	}
	for needed > capacity*machineStringLoadNum/machineStringLoadDen {
		if capacity > int(^uint(0)>>1)/2 {
			return errors.New("machine string arena index capacity overflows int")
		}
		capacity *= 2
	}
	if capacity&(capacity-1) != 0 {
		power := machineStringInitialIndex
		for power < capacity {
			power *= 2
		}
		capacity = power
	}
	newIndex := make([]machineStringID, capacity)
	for idIndex, record := range arena.records {
		id := machineStringID(idIndex + 1)
		mask := len(newIndex) - 1
		for probe := 0; probe < len(newIndex); probe++ {
			slot := (int(record.hash) + probe) & mask
			if newIndex[slot] == invalidMachineStringID {
				newIndex[slot] = id
				break
			}
		}
	}
	arena.index = newIndex
	return nil
}

func machineStringHash(value []byte) uint64 {
	hash := machineStringHashOffset
	for _, byteValue := range value {
		hash ^= uint64(byteValue)
		hash *= machineStringHashPrime
	}
	return hash
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index, byteValue := range left {
		if byteValue != right[index] {
			return false
		}
	}
	return true
}

func bytesCompare(left, right []byte) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}
