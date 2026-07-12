package ember

import (
	"math"
	"sync"
)

// slotExecutionState is the scratch storage for the conservative scalar
// runner.  It intentionally owns only unboxed slots: references to a VM
// owner, runtime heap, or Value must never survive a pooled run.
type slotExecutionState struct {
	registers []slot
	constants []slot
	results   []slot
}

const maxPooledSlotExecutionCapacity = 4 * 1024

var slotExecutionPool = sync.Pool{
	New: func() any { return &slotExecutionState{} },
}

func acquireSlotExecutionState(registers, constants int) *slotExecutionState {
	state := slotExecutionPool.Get().(*slotExecutionState)
	if cap(state.registers) < registers {
		state.registers = make([]slot, registers)
	}
	state.registers = state.registers[:registers]
	if cap(state.constants) < constants {
		state.constants = make([]slot, constants)
	}
	state.constants = state.constants[:constants]
	state.results = state.results[:0]
	return state
}

func releaseSlotExecutionState(state *slotExecutionState) {
	if state == nil {
		return
	}
	state.resetForPool()
	slotExecutionPool.Put(state)
}

func (state *slotExecutionState) resetForPool() {
	// Slots are scalar bits, but clear the backing arrays so a future change to
	// slot representation cannot accidentally retain an owner or heap root.
	clear(state.registers)
	clear(state.constants)
	clear(state.results)
	state.registers = resetSlotExecutionBuffer(state.registers)
	state.constants = resetSlotExecutionBuffer(state.constants)
	state.results = resetSlotExecutionBuffer(state.results)
}

func resetSlotExecutionBuffer(values []slot) []slot {
	if cap(values) > maxPooledSlotExecutionCapacity {
		return nil
	}
	return values[:0]
}

// slotExecutionEligible is sealed onto Proto by finalizeProtoExecutionArtifact.
// The scalar runner is deliberately restricted to pure operations whose
// scratch-only restart cannot duplicate an externally visible effect.  In
// particular, no global, table, reference, call, closure, upvalue, vararg,
// yield, or mutation opcode is admitted here.
func slotExecutionEligible(proto *Proto, code []instruction) bool {
	if proto == nil || proto.registers < 0 || proto.params != 0 || proto.variadic ||
		len(proto.upvalues) != 0 || len(proto.prototypes) != 0 {
		return false
	}
	for _, constant := range proto.constants {
		if !slotExecutionImmediateValue(constant) {
			return false
		}
	}
	for _, ins := range code {
		switch ins.op {
		case opLoadConst:
			if ins.b < 0 || ins.b >= len(proto.constants) {
				return false
			}
		case opMove:
		case opAdd:
		case opAddK:
			if ins.c < 0 || ins.c >= len(proto.constants) {
				return false
			}
		case opNumericForCheck, opNumericForLoop:
		case opReturnOne:
		case opReturn:
			// Negative counts are open returns (calls/varargs) and cannot be
			// represented by the immediate-only result window.
			if ins.b < 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func slotExecutionImmediateValue(value Value) bool {
	switch valueKind(value) {
	case NilKind, BoolKind:
		return true
	case NumberKind:
		// A quiet NaN whose high bits collide with the tag prefix needs a
		// boxed-number handle.  Reject it here rather than putting a heap
		// reference in this runner's scalar slot pool.
		return !slotExecutionNumberNeedsBox(valueNumber(value))
	default:
		return false
	}
}

func slotExecutionNumberNeedsBox(value float64) bool {
	return math.Float64bits(value)&slotTaggedMask == slotTaggedPrefix
}

// runSlotExecution attempts the canonical immediate-only runner.  handled is
// false for a safe fallback to the established VM; it is not a user-visible
// execution error.
func runSlotExecution(proto *Proto) (values []Value, handled bool, err error) {
	if proto == nil || !proto.slotExecutionEligible {
		return nil, false, nil
	}
	state := acquireSlotExecutionState(proto.registers, len(proto.constants))
	defer releaseSlotExecutionState(state)

	for index, constant := range proto.constants {
		encoded, ok := slotExecutionImportImmediate(constant)
		if !ok {
			return nil, false, nil
		}
		state.constants[index] = encoded
	}
	for index := range state.registers {
		state.registers[index] = slotNil
	}

	count, ok := runSlotExecutionWords(proto, state)
	if !ok {
		return nil, false, nil
	}
	if count == 0 {
		return nil, true, nil
	}
	values = make([]Value, count)
	for index := range values {
		value, ok := slotExecutionExportImmediate(state.results[index])
		if !ok {
			return nil, false, nil
		}
		values[index] = value
	}
	return values, true, nil
}

func slotExecutionImportImmediate(value Value) (slot, bool) {
	switch valueKind(value) {
	case NilKind:
		return slotNil, true
	case BoolKind:
		return slotBool(valueBool(value)), true
	case NumberKind:
		if slotExecutionNumberNeedsBox(valueNumber(value)) {
			return 0, false
		}
		return slot(value.bits), true
	default:
		return 0, false
	}
}

func slotExecutionExportImmediate(value slot) (Value, bool) {
	if !slotIsTagged(value) {
		return NumberValue(math.Float64frombits(uint64(value))), true
	}
	switch slotTagOf(value) {
	case slotTagNil:
		if !slotImmediatePayloadZero(value) {
			return Value{}, false
		}
		return NilValue(), true
	case slotTagFalse:
		if !slotImmediatePayloadZero(value) {
			return Value{}, false
		}
		return BoolValue(false), true
	case slotTagTrue:
		if !slotImmediatePayloadZero(value) {
			return Value{}, false
		}
		return BoolValue(true), true
	default:
		return Value{}, false
	}
}

func runSlotExecutionWords(proto *Proto, state *slotExecutionState) (int, bool) {
	words := proto.words
	pc := 0
	for pc < len(words) {
		raw := words[pc]
		op := opcode(raw & wordcodeOpcodeMask)
		hasAux := raw&wordcodeAuxBit != 0
		a := int(uint8(raw >> 8))
		b := int(uint8(raw >> 16))
		c := int(uint8(raw >> 24))
		d := 0
		next := pc + 1
		switch op {
		case opLoadConst:
			b = int(uint16(raw >> 16))
			if hasAux {
				if next >= len(words) {
					return 0, false
				}
				b = int(int32(words[next]))
				next++
			}
		case opMove, opAdd, opReturnOne:
			if hasAux {
				return 0, false
			}
		case opAddK:
			if hasAux {
				if next >= len(words) {
					return 0, false
				}
				c = int(int32(words[next]))
				next++
			}
		case opNumericForCheck, opNumericForLoop:
			if !hasAux || next >= len(words) {
				return 0, false
			}
			d = int(int32(words[next])) + next + 1
			next++
		case opReturn:
			if hasAux {
				return 0, false
			}
			b = int(int16(uint16(raw >> 16)))
		default:
			return 0, false
		}
		switch op {
		case opLoadConst:
			if a < 0 || a >= len(state.registers) || b < 0 || b >= len(state.constants) {
				return 0, false
			}
			state.registers[a] = state.constants[b]
		case opMove:
			if a < 0 || a >= len(state.registers) || b < 0 || b >= len(state.registers) {
				return 0, false
			}
			state.registers[a] = state.registers[b]
		case opAdd:
			if a < 0 || a >= len(state.registers) || b < 0 || b >= len(state.registers) || c < 0 || c >= len(state.registers) {
				return 0, false
			}
			left, leftOK := slotExecutionNumber(state.registers[b])
			right, rightOK := slotExecutionNumber(state.registers[c])
			if !leftOK || !rightOK || !slotExecutionStoreNumber(state.registers, a, left+right) {
				return 0, false
			}
		case opAddK:
			if a < 0 || a >= len(state.registers) || b < 0 || b >= len(state.registers) || c < 0 || c >= len(state.constants) {
				return 0, false
			}
			left, leftOK := slotExecutionNumber(state.registers[b])
			right, rightOK := slotExecutionNumber(state.constants[c])
			if !leftOK || !rightOK || !slotExecutionStoreNumber(state.registers, a, left+right) {
				return 0, false
			}
		case opNumericForCheck:
			if a < 0 || a >= len(state.registers) || b < 0 || b >= len(state.registers) || c < 0 || c >= len(state.registers) {
				return 0, false
			}
			loopValue, loopOK := slotExecutionNumber(state.registers[a])
			limitValue, limitOK := slotExecutionNumber(state.registers[b])
			stepValue, stepOK := slotExecutionNumber(state.registers[c])
			if !loopOK || !limitOK || !stepOK || math.IsNaN(loopValue) || math.IsNaN(limitValue) || math.IsNaN(stepValue) {
				return 0, false
			}
			if (stepValue > 0 && loopValue > limitValue) || (stepValue <= 0 && loopValue < limitValue) {
				if d < 0 || d > len(words) {
					return 0, false
				}
				pc = d
				continue
			}
		case opNumericForLoop:
			if a < 0 || a >= len(state.registers) || b < 0 || b >= len(state.registers) {
				return 0, false
			}
			loopValue, loopOK := slotExecutionNumber(state.registers[a])
			stepValue, stepOK := slotExecutionNumber(state.registers[b])
			if !loopOK || !stepOK || !slotExecutionStoreNumber(state.registers, a, loopValue+stepValue) {
				return 0, false
			}
			if d < 0 || d > len(words) {
				return 0, false
			}
			pc = d
			continue
		case opReturnOne:
			if a < 0 || a >= len(state.registers) {
				return 0, false
			}
			state.results = append(state.results[:0], state.registers[a])
			return 1, true
		case opReturn:
			if b < 0 || b > len(state.registers) || a < 0 || a+b > len(state.registers) {
				return 0, false
			}
			if b == 0 {
				return 0, true
			}
			state.results = append(state.results[:0], state.registers[a:a+b]...)
			return b, true
		default:
			return 0, false
		}
		pc = next
	}
	return 0, true
}

// slotExecutionNumber accepts only untagged IEEE-754 numbers.  A tagged
// immediate or a reference-like value is a safe restart signal, not an error:
// the established VM owns coercions and diagnostics for those cases.
func slotExecutionNumber(value slot) (float64, bool) {
	if slotIsTagged(value) {
		return 0, false
	}
	return math.Float64frombits(uint64(value)), true
}

func slotExecutionStoreNumber(registers []slot, index int, value float64) bool {
	if index < 0 || index >= len(registers) || slotExecutionNumberNeedsBox(value) {
		return false
	}
	registers[index] = slot(valueFloat64Bits(value))
	return true
}
