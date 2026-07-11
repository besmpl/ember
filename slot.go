package ember

import "fmt"

// slot is the compact, private representation used by the runtime's future
// register and stack storage. Its bits are either an IEEE-754 number or a
// tagged immediate/handle payload.
type slot uint64

const (
	slotTaggedPrefix uint64 = 0x7ff8_0000_0000_0000
	slotTaggedMask   uint64 = 0xffff_0000_0000_0000

	slotTagShift = 44
	slotTagMask  = uint64(0xf)

	slotGenerationShift = 28
	slotGenerationMask  = uint64(0xffff)
	slotIndexMask       = uint64((1 << 28) - 1)
	slotMaxGeneration   = uint16(0xffff)
)

// The high 16 bits identify tagged slots. The four bits immediately below
// that prefix identify the immediate or handle kind; the remaining 44 bits
// are kind-specific payload.
type slotTag uint8

const (
	slotTagNil slotTag = iota
	slotTagFalse
	slotTagTrue
	slotTagString
	slotTagTable
	slotTagClosure
	slotTagUpvalue
	slotTagUserdata
	slotTagHostCallable
	slotTagNativeID
	slotTagBoxedNumber
)

const (
	slotTagUnboxedNumber slotTag   = 0xff
	slotInvalidValueKind ValueKind = 0xff

	// slotNil is deliberately distinct from slot(0): zero bits are the exact
	// IEEE-754 representation of positive zero, not Luau nil.
	slotNil slot = slot(slotTaggedPrefix | uint64(slotTagNil)<<slotTagShift)
)

// runtimeHeap is the private owner for out-of-line slot payloads. It is kept
// intentionally small until the runtime's public Value/slot conversion seam
// is introduced.
type slotNumberEntry struct {
	bits       uint64
	generation uint16
	live       bool
	retired    bool
}

// slotNumberSlab is the first typed generation-checked slab. Index zero is
// reserved for typed-nil payloads, while retired entries are never recycled.
type slotNumberSlab struct {
	entries []slotNumberEntry
	free    []uint32
}

type runtimeHeap struct {
	boxedNumbers slotNumberSlab
}

func slotIsTagged(value slot) bool {
	return uint64(value)&slotTaggedMask == slotTaggedPrefix
}

func slotTagOf(value slot) slotTag {
	if !slotIsTagged(value) {
		return slotTagUnboxedNumber
	}
	return slotTag((uint64(value) >> slotTagShift) & slotTagMask)
}

func slotValueKind(value slot) ValueKind {
	if !slotIsTagged(value) {
		return NumberKind
	}

	switch slotTagOf(value) {
	case slotTagNil:
		return NilKind
	case slotTagFalse, slotTagTrue:
		return BoolKind
	case slotTagString:
		return StringKind
	case slotTagTable:
		return TableKind
	case slotTagClosure:
		return FunctionKind
	case slotTagUserdata:
		return UserDataKind
	case slotTagHostCallable, slotTagNativeID:
		return HostFuncKind
	case slotTagBoxedNumber:
		return NumberKind
	default:
		return slotInvalidValueKind
	}
}

func slotBool(value bool) slot {
	tag := slotTagFalse
	if value {
		tag = slotTagTrue
	}
	return slot(slotTaggedPrefix | uint64(tag)<<slotTagShift)
}

func slotBoolValue(value slot) (bool, error) {
	if !slotIsTagged(value) {
		return false, fmt.Errorf("slot: untagged value is not a boolean")
	}
	switch slotTagOf(value) {
	case slotTagFalse:
		return false, nil
	case slotTagTrue:
		return true, nil
	default:
		return false, fmt.Errorf("slot: tagged %v is not a boolean", slotTagOf(value))
	}
}

func slotHandleKind(kind slotTag) bool {
	switch kind {
	case slotTagString, slotTagTable, slotTagClosure, slotTagUpvalue, slotTagUserdata, slotTagHostCallable, slotTagBoxedNumber:
		return true
	default:
		return false
	}
}

func slotTypedReferenceKind(kind slotTag) bool {
	switch kind {
	case slotTagString, slotTagTable, slotTagClosure, slotTagUpvalue, slotTagUserdata, slotTagHostCallable:
		return true
	default:
		return false
	}
}

func slotPackHandle(kind slotTag, index uint32, generation uint16) (slot, error) {
	if !slotHandleKind(kind) {
		return 0, fmt.Errorf("slot: %v is not a heap handle kind", kind)
	}
	if uint64(index) > slotIndexMask {
		return 0, fmt.Errorf("slot: handle index %d overflows %d bits", index, 28)
	}
	if index == 0 {
		return 0, fmt.Errorf("slot: handle index must be non-zero")
	}
	if generation == 0 {
		return 0, fmt.Errorf("slot: handle generation must be non-zero")
	}
	return slot(slotTaggedPrefix |
		uint64(kind)<<slotTagShift |
		uint64(generation)<<slotGenerationShift |
		uint64(index)), nil
}

func slotUnpackHandle(value slot) (slotTag, uint32, uint16, error) {
	if !slotIsTagged(value) {
		return 0, 0, 0, fmt.Errorf("slot: untagged value is not a heap handle")
	}
	kind := slotTagOf(value)
	if !slotHandleKind(kind) {
		return 0, 0, 0, fmt.Errorf("slot: %v is not a heap handle kind", kind)
	}
	index := uint32(uint64(value) & slotIndexMask)
	generation := uint16((uint64(value) >> slotGenerationShift) & slotGenerationMask)
	return kind, index, generation, nil
}

func slotPackTypedNil(kind slotTag) (slot, error) {
	if !slotTypedReferenceKind(kind) {
		return 0, fmt.Errorf("slot: %v is not a typed reference kind", kind)
	}
	return slot(slotTaggedPrefix | uint64(kind)<<slotTagShift), nil
}

func slotNativeID(id nativeFuncID) slot {
	return slot(slotTaggedPrefix | uint64(slotTagNativeID)<<slotTagShift | uint64(id))
}

func slotNativeIDValue(value slot) (nativeFuncID, error) {
	if !slotIsTagged(value) || slotTagOf(value) != slotTagNativeID {
		return 0, fmt.Errorf("slot: value is not a native function immediate")
	}
	if uint64(value)&(slotIndexMask|(slotGenerationMask<<slotGenerationShift)) > 0xff {
		return 0, fmt.Errorf("slot: native function immediate payload is malformed")
	}
	return nativeFuncID(uint64(value) & 0xff), nil
}

func slotFromNumberBits(bits uint64, heap *runtimeHeap) (slot, error) {
	value := slot(bits)
	if !slotIsTagged(value) {
		return value, nil
	}
	if heap == nil {
		return 0, fmt.Errorf("slot: number bits %#x require a heap box", bits)
	}
	return heap.boxNumberBits(bits)
}

func (heap *runtimeHeap) boxNumberBits(bits uint64) (slot, error) {
	slab := &heap.boxedNumbers
	if len(slab.entries) == 0 {
		// Index zero is reserved so that an all-zero handle payload can remain a
		// typed-nil sentinel once other reference kinds use this slab.
		slab.entries = append(slab.entries, slotNumberEntry{})
	}
	for len(slab.free) > 0 {
		last := len(slab.free) - 1
		index := slab.free[last]
		slab.free = slab.free[:last]
		entry := &slab.entries[index]
		if entry.retired || entry.live {
			continue
		}
		entry.bits = bits
		entry.live = true
		return slotPackHandle(slotTagBoxedNumber, index, entry.generation)
	}
	if len(slab.entries) > int(slotIndexMask) {
		return 0, fmt.Errorf("slot: boxed number slab exhausted")
	}
	index := uint32(len(slab.entries))
	slab.entries = append(slab.entries, slotNumberEntry{
		bits:       bits,
		generation: 1,
		live:       true,
	})
	return slotPackHandle(slotTagBoxedNumber, index, 1)
}

func (heap *runtimeHeap) releaseBoxedNumber(value slot) error {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return err
	}
	if kind != slotTagBoxedNumber {
		return fmt.Errorf("slot: %v is not a boxed number handle", kind)
	}
	slab := &heap.boxedNumbers
	if index == 0 || int(index) >= len(slab.entries) {
		return fmt.Errorf("slot: boxed number index %d is invalid", index)
	}
	entry := &slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return fmt.Errorf("slot: boxed number handle is stale")
	}
	entry.bits = 0
	entry.live = false
	if generation == slotMaxGeneration {
		entry.retired = true
		return nil
	}
	entry.generation++
	slab.free = append(slab.free, index)
	return nil
}

func slotNumberBits(value slot, heap *runtimeHeap) (uint64, error) {
	if !slotIsTagged(value) {
		return uint64(value), nil
	}
	if slotTagOf(value) == slotTagBoxedNumber {
		if heap == nil {
			return 0, fmt.Errorf("slot: boxed number needs a heap")
		}
		kind, index, generation, err := slotUnpackHandle(value)
		if err != nil {
			return 0, err
		}
		if kind != slotTagBoxedNumber {
			return 0, fmt.Errorf("slot: %v is not a boxed number handle", kind)
		}
		if index == 0 || int(index) >= len(heap.boxedNumbers.entries) {
			return 0, fmt.Errorf("slot: boxed number index %d is invalid", index)
		}
		entry := heap.boxedNumbers.entries[index]
		if !entry.live || entry.retired || entry.generation != generation {
			return 0, fmt.Errorf("slot: boxed number handle is stale")
		}
		return entry.bits, nil
	}
	return 0, fmt.Errorf("slot: tagged %v is not an inline number", slotTagOf(value))
}
