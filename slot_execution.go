package ember

import (
	"math"
	"sync"
)

// slotExecutionState is the scratch storage for the conservative slot
// runner. Slots are always scalar bits; heap is activated only while a run
// needs an out-of-line handle (for example a string or a tag-colliding NaN).
// The heap is retained empty by the pool for reference-run reuse.
type slotExecutionState struct {
	registers       []slot
	constants       []slot
	results         []slot
	heap            *runtimeHeap
	heapActive      bool
	transientInline [8]slot
	transient       []slot
}

func (state *slotExecutionState) addTransient(value slot) {
	if state == nil || !slotIsLiveHandle(value) {
		return
	}
	if state.transient == nil {
		state.transient = state.transientInline[:0]
	}
	state.transient = append(state.transient, value)
}

func (state *slotExecutionState) releaseTransients(heap *runtimeHeap) {
	if state == nil || heap == nil {
		return
	}
	for _, handle := range state.transient {
		switch slotTagOf(handle) {
		case slotTagUserdata, slotTagHostCallable:
			// importValue gives opaque userdata and host callables a default
			// pin. These handles were created by this transient execution, so
			// drop that temporary pin before releasing; preexisting owner
			// handles are never present in this list.
			if err := heap.unpinHandle(handle); err != nil {
				continue
			}
		}
		_ = heap.releaseHandle(handle)
	}
	clear(state.transient)
	state.transient = state.transientInline[:0]
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
	state.heapActive = false
	state.transient = state.transientInline[:0]
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
	clear(state.transient)
	state.registers = resetSlotExecutionBuffer(state.registers)
	state.constants = resetSlotExecutionBuffer(state.constants)
	state.results = resetSlotExecutionBuffer(state.results)
	state.transient = state.transientInline[:0]
	if state.heapActive && state.heap != nil {
		if runtimeHeapExceedsPooledCapacity(state.heap) {
			state.heap = nil
		} else if !state.heap.resetForReuse() {
			state.heap = nil
		}
	}
	state.heapActive = false
}

func runtimeHeapExceedsPooledCapacity(heap *runtimeHeap) bool {
	if heap == nil {
		return false
	}
	capacity := cap(heap.boxedNumbers.entries) +
		cap(heap.strings.entries) +
		cap(heap.tables.entries) +
		cap(heap.closures.entries) +
		cap(heap.upvalues.entries) +
		cap(heap.userdata.entries) +
		cap(heap.hostCallables.entries)
	return capacity > maxPooledSlotExecutionCapacity
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
// yield, or mutation opcode is admitted here. Fixed parameters are allowed;
// they are imported into the same per-run scratch heap as constants.
func slotExecutionEligible(proto *Proto, code []instruction) bool {
	if proto == nil || proto.registers < 0 || proto.params < 0 ||
		proto.params > proto.registers || proto.variadic ||
		len(proto.upvalues) != 0 || len(proto.prototypes) != 0 {
		return false
	}
	for _, constant := range proto.constants {
		if !slotExecutionValueSupported(constant) {
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

// slotExecutionValueSupported describes the complete public Value set that
// runtimeHeap can import. Immediate-only values stay on the allocation-free
// path; the remaining supported values use a per-run heap adapter.
func slotExecutionValueSupported(value Value) bool {
	switch valueKind(value) {
	case NilKind, BoolKind, NumberKind, StringKind, TableKind, FunctionKind, UserDataKind, HostFuncKind:
		return true
	default:
		return false
	}
}

func slotExecutionNumberNeedsBox(value float64) bool {
	return math.Float64bits(value)&slotTaggedMask == slotTaggedPrefix
}

// runSlotExecution attempts the canonical slot runner. Constants and the
// fixed-parameter prefix of args are imported once at the public/host seam;
// opcode bodies operate only on slots. handled is false for a safe fallback
// to the established VM; it is not a user-visible execution error. args is
// the caller-provided parameter slice; extra values are ignored by the
// fixed-parameter ABI, matching the established VM.
func runSlotExecution(proto *Proto, args []Value) (values []Value, handled bool, err error) {
	if proto == nil || !proto.slotExecutionEligible {
		return nil, false, nil
	}
	state := acquireSlotExecutionState(proto.registers, len(proto.constants))
	defer releaseSlotExecutionState(state)
	return runSlotExecutionState(proto, args, state, false)
}

// runSlotExecutionWithHeap executes an eligible prototype against a heap
// borrowed from its runtime owner. The borrowed heap is never reset here.
// Values created by this run are released when it ends; preexisting owner
// roots and pins are left untouched.
func runSlotExecutionWithHeap(proto *Proto, args []Value, heap *runtimeHeap) (values []Value, handled bool, err error) {
	if proto == nil || !proto.slotExecutionEligible || heap == nil {
		return nil, false, nil
	}
	state := acquireSlotExecutionState(proto.registers, len(proto.constants))
	for index := range state.registers {
		state.registers[index] = slotNil
	}
	params, needsHeap, ok := slotExecutionPrepareImmediates(state, proto.constants, args, proto.params)
	if !ok {
		releaseSlotExecutionState(state)
		return nil, false, nil
	}
	if !needsHeap {
		values, handled, err = runSlotExecutionPrepared(proto, state)
		releaseSlotExecutionState(state)
		return values, handled, err
	}

	// Immediate-only runs never contend on the owner heap. Reference-bearing
	// runs hold its collector gate across import, execution, export, and
	// transactional release so concurrent owner runs and root/pin operations
	// cannot observe or mutate a partial heap transaction.
	heap.collectMu.Lock()
	previousHeap := state.heap
	state.heap = heap
	state.heapActive = true
	if slotExecutionImportAll(state, proto.constants, args, params, true) {
		values, handled, err = runSlotExecutionPrepared(proto, state)
	}
	state.releaseTransients(heap)
	// Borrowed owner heaps must never be reset by the pooled state. Restore the
	// state's own reusable heap before returning it to the pool.
	state.heapActive = false
	state.heap = previousHeap
	releaseSlotExecutionState(state)
	heap.collectMu.Unlock()
	return values, handled, err
}

func runSlotExecutionState(proto *Proto, args []Value, state *slotExecutionState, trackTransients bool) (values []Value, handled bool, err error) {
	for index := range state.registers {
		state.registers[index] = slotNil
	}
	if !slotExecutionImportConstantsAndArgsTracked(state, proto.constants, args, proto.params, trackTransients) {
		return nil, false, nil
	}
	return runSlotExecutionPrepared(proto, state)
}

func runSlotExecutionPrepared(proto *Proto, state *slotExecutionState) (values []Value, handled bool, err error) {
	count, ok := runSlotExecutionWords(proto, state)
	if !ok {
		return nil, false, nil
	}
	if count == 0 {
		return nil, true, nil
	}
	values = make([]Value, count)
	for index := range values {
		value, ok := slotExecutionExport(state, state.results[index])
		if !ok {
			return nil, false, nil
		}
		values[index] = value
	}
	return values, true, nil
}

// slotExecutionImportConstants keeps the common immediate-only path free of
// heap allocation. A reference run lazily activates the pooled heap and
// imports all constants through the public/host seam before opcode execution.
func slotExecutionImportConstants(state *slotExecutionState, constants []Value) bool {
	return slotExecutionImportConstantsAndArgs(state, constants, nil, 0)
}

// slotExecutionImportConstantsAndArgs imports constants and the relevant
// fixed-parameter prefix into one run state. Immediate values remain
// allocation-free; when any value needs an out-of-line representation, all
// values are imported through the same pooled runtime heap so handles can be
// resolved and exported together.
func slotExecutionImportConstantsAndArgs(state *slotExecutionState, constants, args []Value, params int) bool {
	return slotExecutionImportConstantsAndArgsTracked(state, constants, args, params, false)
}

// slotExecutionImportConstantsAndArgsTracked is the owner-heap variant of
// slotExecutionImportConstantsAndArgs. imported receives only handles created
// by this import pass; handles already present in an owner heap may represent a
// root or pin and must not be released by the compact run.
func slotExecutionImportConstantsAndArgsTracked(state *slotExecutionState, constants, args []Value, params int, trackTransients bool) bool {
	params, needsHeap, ok := slotExecutionPrepareImmediates(state, constants, args, params)
	if !ok || !needsHeap {
		return ok
	}
	if state.heap == nil {
		state.heap = &runtimeHeap{}
	}
	state.heapActive = true
	return slotExecutionImportAll(state, constants, args, params, trackTransients)
}

// slotExecutionPrepareImmediates fills the scratch arrays without touching a
// heap and reports whether a second, heap-backed import pass is required.
func slotExecutionPrepareImmediates(state *slotExecutionState, constants, args []Value, params int) (int, bool, bool) {
	if state == nil {
		return 0, false, false
	}
	if params < 0 {
		return 0, false, false
	}
	if params > len(state.registers) {
		params = len(state.registers)
	}
	if params > len(args) {
		params = len(args)
	}
	state.heapActive = false
	needsHeap := false
	for index, constant := range constants {
		encoded, ok := slotExecutionImportImmediate(constant)
		if !ok {
			needsHeap = true
			break
		}
		state.constants[index] = encoded
	}
	if !needsHeap {
		for index := 0; index < params; index++ {
			encoded, ok := slotExecutionImportImmediate(args[index])
			if !ok {
				needsHeap = true
				break
			}
			state.registers[index] = encoded
		}
	}
	if !needsHeap {
		return params, false, true
	}
	return params, true, true
}

func slotExecutionImportAll(state *slotExecutionState, constants, args []Value, params int, trackTransients bool) bool {
	for index, constant := range constants {
		encoded, err := slotExecutionImportValue(state, constant, trackTransients)
		if err != nil {
			return false
		}
		state.constants[index] = encoded
	}
	for index := 0; index < params; index++ {
		encoded, err := slotExecutionImportValue(state, args[index], trackTransients)
		if err != nil {
			return false
		}
		state.registers[index] = encoded
	}
	return true
}

func slotExecutionImportValue(state *slotExecutionState, value Value, trackTransient bool) (slot, error) {
	encoded, created, err := state.heap.importTransientValue(value)
	if err != nil {
		return 0, err
	}
	if created && trackTransient {
		state.addTransient(encoded)
	}
	return encoded, nil
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

func slotExecutionExport(state *slotExecutionState, value slot) (Value, bool) {
	if state == nil || !state.heapActive || state.heap == nil {
		return slotExecutionExportImmediate(value)
	}
	exported, err := state.heap.exportValue(value)
	if err != nil {
		return Value{}, false
	}
	return exported, true
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
