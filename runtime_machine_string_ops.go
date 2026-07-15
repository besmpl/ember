package ember

import (
	"fmt"
	"math"
)

type machineStringActionKind uint8

const (
	machineStringActionInvalid machineStringActionKind = iota
	machineStringActionReuse
	machineStringActionIntern
	machineStringActionEffect
)

// machineStringAction is a pointer-free request for the stopped executor.
// Reuse names an existing owner-local string. Intern names bytes in the
// caller-owned scratch buffer; generatedBytes is charged only if stopped
// interning confirms those bytes are new. Effect delegates metamethod/error
// behavior without partially producing a result.
type machineStringAction struct {
	stringID       machineStringID
	offset         uint32
	length         uint32
	generatedBytes uint64
	operand        int32
	kind           machineStringActionKind
	_              [3]byte
}

func machinePrepareToString(scratch []byte, operand machineScalarOperand, strings *machineStringArena) ([]byte, machineStringAction, error) {
	switch slotValueKind(operand.value) {
	case StringKind:
		text, err := machineScalarStringBytes(operand.value, strings)
		if err != nil {
			return scratch, machineStringAction{}, err
		}
		index, _, _ := slotValidateHandle(operand.value, slotTagString)
		return scratch, machineStringAction{
			kind:     machineStringActionReuse,
			stringID: machineStringID(index),
			length:   uint32(len(text)),
		}, nil
	case NumberKind:
		bits, err := machineScalarNumberBits(operand)
		if err != nil {
			return scratch, machineStringAction{}, err
		}
		start := len(scratch)
		scratch = appendLuauNumber(scratch, math.Float64frombits(bits))
		return machineGeneratedStringAction(scratch, start, true)
	case NilKind:
		if !slotImmediatePayloadZero(operand.value) {
			return scratch, machineStringAction{}, fmt.Errorf("compact Machine nil immediate is invalid")
		}
		start := len(scratch)
		scratch = append(scratch, "nil"...)
		return machineGeneratedStringAction(scratch, start, false)
	case BoolKind:
		boolean, err := slotBoolValue(operand.value)
		if err != nil {
			return scratch, machineStringAction{}, err
		}
		start := len(scratch)
		if boolean {
			scratch = append(scratch, "true"...)
		} else {
			scratch = append(scratch, "false"...)
		}
		return machineGeneratedStringAction(scratch, start, false)
	default:
		return scratch, machineStringAction{kind: machineStringActionEffect, operand: 0}, nil
	}
}

func machinePrepareConcat(
	scratch []byte,
	left machineScalarOperand,
	right machineScalarOperand,
	strings *machineStringArena,
) ([]byte, machineStringAction, error) {
	operands := [2]machineScalarOperand{left, right}
	return machinePrepareConcatChain(scratch, operands[:], strings)
}

func machinePrepareConcatChain(scratch []byte, operands []machineScalarOperand, strings *machineStringArena) ([]byte, machineStringAction, error) {
	for index, operand := range operands {
		ok, err := machineRawConcatOperand(operand, strings)
		if err != nil {
			return scratch, machineStringAction{}, err
		}
		if !ok {
			return scratch, machineStringAction{kind: machineStringActionEffect, operand: int32(index)}, nil
		}
	}
	start := len(scratch)
	for _, operand := range operands {
		var err error
		scratch, err = machineAppendConcatOperand(scratch, operand, strings)
		if err != nil {
			return scratch[:start], machineStringAction{}, err
		}
	}
	return machineGeneratedStringAction(scratch, start, true)
}

func machineStringActionBytes(action machineStringAction, scratch []byte, strings *machineStringArena) ([]byte, error) {
	switch action.kind {
	case machineStringActionReuse:
		if strings == nil {
			return nil, fmt.Errorf("compact Machine string arena is unavailable")
		}
		text, ok := strings.bytesFor(action.stringID)
		if !ok || uint64(len(text)) != uint64(action.length) {
			return nil, fmt.Errorf("compact Machine string action references an invalid string")
		}
		return text, nil
	case machineStringActionIntern:
		end := uint64(action.offset) + uint64(action.length)
		if end > uint64(len(scratch)) {
			return nil, fmt.Errorf("compact Machine string action exceeds scratch bytes")
		}
		return scratch[action.offset:uint32(end)], nil
	default:
		return nil, fmt.Errorf("compact Machine string action has no ready bytes")
	}
}

// machineChargeGeneratedStringStopped is called after stopped interning. The
// old VM charges only newly interned generated strings, never source/host
// strings or an intern hit.
func machineChargeGeneratedStringStopped(controller *executionController, action machineStringAction, alreadyInterned bool) error {
	if action.kind != machineStringActionIntern || alreadyInterned {
		return nil
	}
	return controller.chargeGeneratedStringBytes(action.generatedBytes)
}

func machineRawConcatOperand(operand machineScalarOperand, strings *machineStringArena) (bool, error) {
	switch slotValueKind(operand.value) {
	case StringKind:
		_, err := machineScalarStringBytes(operand.value, strings)
		return err == nil, err
	case NumberKind:
		_, err := machineScalarNumberBits(operand)
		return err == nil, err
	default:
		return false, nil
	}
}

func machineAppendConcatOperand(scratch []byte, operand machineScalarOperand, strings *machineStringArena) ([]byte, error) {
	switch slotValueKind(operand.value) {
	case StringKind:
		text, err := machineScalarStringBytes(operand.value, strings)
		if err != nil {
			return scratch, err
		}
		return append(scratch, text...), nil
	case NumberKind:
		bits, err := machineScalarNumberBits(operand)
		if err != nil {
			return scratch, err
		}
		return appendLuauNumber(scratch, math.Float64frombits(bits)), nil
	default:
		return scratch, fmt.Errorf("concat operand is %s, want string or number", slotValueKind(operand.value))
	}
}

func machineGeneratedStringAction(scratch []byte, start int, generated bool) ([]byte, machineStringAction, error) {
	if start < 0 || start > len(scratch) || uint64(start) > math.MaxUint32 || uint64(len(scratch)-start) > math.MaxUint32 {
		return scratch, machineStringAction{}, fmt.Errorf("compact Machine generated string exceeds uint32 scratch range")
	}
	length := uint64(len(scratch) - start)
	action := machineStringAction{
		kind:   machineStringActionIntern,
		offset: uint32(start),
		length: uint32(length),
	}
	if generated {
		action.generatedBytes = length
	}
	return scratch, action, nil
}
