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

	slotGenerationShift   = 28
	slotGenerationMask    = uint64(0xffff)
	slotIndexMask         = uint64((1 << 28) - 1)
	slotMaxGeneration     = uint16(0xffff)
	slotHandlePayloadMask = (slotGenerationMask << slotGenerationShift) | slotIndexMask
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
	if uint64(value)&slotHandlePayloadMask != 0 {
		return false, fmt.Errorf("slot: boolean immediate has a non-zero payload")
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

func slotImmediatePayloadZero(value slot) bool {
	return uint64(value)&slotHandlePayloadMask == 0
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

func slotValidateHandle(value slot, wantKind slotTag) (uint32, uint16, error) {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return 0, 0, err
	}
	if kind != wantKind {
		return 0, 0, fmt.Errorf("slot: %v handle is not a %v handle", kind, wantKind)
	}
	if index == 0 || generation == 0 {
		return 0, 0, fmt.Errorf("slot: malformed %v handle", wantKind)
	}
	return index, generation, nil
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
	id := nativeFuncID(uint64(value) & 0xff)
	if id == nativeFuncUnknown {
		return 0, fmt.Errorf("slot: native function immediate has unknown ID")
	}
	return id, nil
}
