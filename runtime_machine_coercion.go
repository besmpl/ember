package ember

import (
	"fmt"
	"math"
	"strconv"
	"unsafe"
)

// machineScalarOperand is the pointer-free input shared by stopped scalar
// semantic helpers. numberBits is the resolved IEEE-754 payload for a boxed
// number handle and is ignored for every other value.
type machineScalarOperand struct {
	value      slot
	numberBits uint64
}

type machineNumericActionKind uint8

const (
	machineNumericActionInvalid machineNumericActionKind = iota
	machineNumericActionReturn
	machineNumericActionReject
)

// machineNumericAction is a pointer-free coercion decision. Parsing happens
// before dispatch mutates registers; the executor can either consume bits or
// produce the old-VM error without retaining string-arena memory.
type machineNumericAction struct {
	bits       uint64
	kind       machineNumericActionKind
	rejectKind ValueKind
	_          [6]byte
}

func machinePrepareNumericOperand(operand machineScalarOperand, strings *machineStringArena) (machineNumericAction, error) {
	switch slotValueKind(operand.value) {
	case NumberKind:
		bits, err := machineScalarNumberBits(operand)
		if err != nil {
			return machineNumericAction{}, err
		}
		return machineNumericAction{kind: machineNumericActionReturn, bits: bits}, nil
	case StringKind:
		text, err := machineScalarStringBytes(operand.value, strings)
		if err != nil {
			return machineNumericAction{}, err
		}
		// ParseFloat never retains its input. The arena owns text for the whole
		// call, so a read-only string view avoids allocating on every coercion.
		number, err := strconv.ParseFloat(unsafe.String(unsafe.SliceData(text), len(text)), 64)
		if err != nil {
			return machineNumericAction{kind: machineNumericActionReject, rejectKind: StringKind}, nil
		}
		return machineNumericAction{kind: machineNumericActionReturn, bits: math.Float64bits(number)}, nil
	default:
		return machineNumericAction{kind: machineNumericActionReject, rejectKind: slotValueKind(operand.value)}, nil
	}
}

func machineNumericOperandResult(action machineNumericAction, side, operator string) (float64, error) {
	switch action.kind {
	case machineNumericActionReturn:
		return math.Float64frombits(action.bits), nil
	case machineNumericActionReject:
		operand := "operand"
		if side != "" {
			operand = side + " operand"
		}
		return 0, fmt.Errorf("%s %s is %s, want number", operator, operand, action.rejectKind)
	default:
		return 0, fmt.Errorf("%s numeric operand action is invalid", operator)
	}
}

func machineScalarNumberBits(operand machineScalarOperand) (uint64, error) {
	if !slotIsTagged(operand.value) {
		return uint64(operand.value), nil
	}
	if slotTagOf(operand.value) != slotTagBoxedNumber {
		return 0, fmt.Errorf("compact Machine value is %s, want number", slotValueKind(operand.value))
	}
	_, index, generation, err := slotUnpackHandle(operand.value)
	if err != nil || index == 0 || (generation != 1 && generation != 2) {
		return 0, fmt.Errorf("compact Machine boxed number handle is invalid")
	}
	return operand.numberBits, nil
}

func machineScalarStringBytes(value slot, strings *machineStringArena) ([]byte, error) {
	index, generation, err := slotValidateHandle(value, slotTagString)
	if err != nil || generation != 1 || strings == nil {
		return nil, fmt.Errorf("compact Machine value is not a valid string handle")
	}
	text, ok := strings.bytesFor(machineStringID(index))
	if !ok {
		return nil, fmt.Errorf("compact Machine string ID %d is invalid", index)
	}
	return text, nil
}
