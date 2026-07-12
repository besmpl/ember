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
// This first slice deliberately admits only immediate LOAD/MOVE/RETURN code;
// all richer bytecode remains on the established VM until Slice 4.3 migrates
// those operations one coherent family at a time.
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
	pc := 0
	for pc < len(proto.words) {
		dispatch, err := decodeWordcodeDispatch(proto.words, pc)
		if err != nil {
			return 0, false
		}
		next := dispatch.nextWord
		switch dispatch.op {
		case opLoadConst:
			if dispatch.a < 0 || dispatch.a >= len(state.registers) || dispatch.b < 0 || dispatch.b >= len(state.constants) {
				return 0, false
			}
			state.registers[dispatch.a] = state.constants[dispatch.b]
		case opMove:
			if dispatch.a < 0 || dispatch.a >= len(state.registers) || dispatch.b < 0 || dispatch.b >= len(state.registers) {
				return 0, false
			}
			state.registers[dispatch.a] = state.registers[dispatch.b]
		case opReturnOne:
			if dispatch.a < 0 || dispatch.a >= len(state.registers) {
				return 0, false
			}
			state.results = append(state.results[:0], state.registers[dispatch.a])
			return 1, true
		case opReturn:
			if dispatch.b < 0 || dispatch.b > len(state.registers) || dispatch.a < 0 || dispatch.a+dispatch.b > len(state.registers) {
				return 0, false
			}
			if dispatch.b == 0 {
				return 0, true
			}
			state.results = append(state.results[:0], state.registers[dispatch.a:dispatch.a+dispatch.b]...)
			return dispatch.b, true
		default:
			// Temporary Slice 4.3 migration seam: richer operations continue
			// through the existing VM until their slot ABI is proven.
			return 0, false
		}
		pc = next
	}
	return 0, true
}
