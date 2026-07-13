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
	registers []slot
	constants []slot
	results   []slot
	// numericRegisters is the untagged register file for prototypes whose
	// compiler proof guarantees numeric reads, writes, branches, and returns.
	numericRegisters []float64
	heap             *runtimeHeap
	heapActive       bool
	transientInline  [8]slot
	transient        []slot
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

// slotNumericAnalysisState is pooled compiler scratch. Numeric-tier proof is
// deliberately allocation-free after warm-up so the runtime speedup does not
// tax every compilation with a per-instruction graph of Go objects.
type slotNumericAnalysisState struct {
	facts     []uint64
	scratch   []uint64
	reachable []uint8
	queued    []uint8
	work      []int
	words     int
	codeLen   int
}

const maxPooledSlotNumericAnalysisWords = 64 * 1024

var slotNumericAnalysisPool = sync.Pool{
	New: func() any { return &slotNumericAnalysisState{} },
}

func acquireSlotNumericAnalysisState(codeLen int, registers int) *slotNumericAnalysisState {
	state := slotNumericAnalysisPool.Get().(*slotNumericAnalysisState)
	words := (registers + 63) / 64
	factCount := codeLen * words
	if cap(state.facts) < factCount {
		state.facts = make([]uint64, factCount)
	} else {
		state.facts = state.facts[:factCount]
		clear(state.facts)
	}
	if cap(state.scratch) < words {
		state.scratch = make([]uint64, words)
	} else {
		state.scratch = state.scratch[:words]
		clear(state.scratch)
	}
	if cap(state.reachable) < codeLen {
		state.reachable = make([]uint8, codeLen)
	} else {
		state.reachable = state.reachable[:codeLen]
		clear(state.reachable)
	}
	if cap(state.queued) < codeLen {
		state.queued = make([]uint8, codeLen)
	} else {
		state.queued = state.queued[:codeLen]
		clear(state.queued)
	}
	state.work = state.work[:0]
	if cap(state.work) < codeLen {
		state.work = make([]int, 0, codeLen)
	}
	state.words = words
	state.codeLen = codeLen
	return state
}

func releaseSlotNumericAnalysisState(state *slotNumericAnalysisState) {
	if state == nil {
		return
	}
	if cap(state.facts) > maxPooledSlotNumericAnalysisWords {
		state.facts = nil
	} else {
		clear(state.facts)
		state.facts = state.facts[:0]
	}
	if cap(state.scratch) > maxPooledSlotNumericAnalysisWords {
		state.scratch = nil
	} else {
		clear(state.scratch)
		state.scratch = state.scratch[:0]
	}
	clear(state.reachable)
	clear(state.queued)
	clear(state.work)
	state.reachable = state.reachable[:0]
	state.queued = state.queued[:0]
	state.work = state.work[:0]
	state.words = 0
	state.codeLen = 0
	slotNumericAnalysisPool.Put(state)
}

func (state *slotNumericAnalysisState) row(pc int) []uint64 {
	start := pc * state.words
	return state.facts[start : start+state.words]
}

func slotNumericFact(facts []uint64, register int) bool {
	return register >= 0 && register/64 < len(facts) && facts[register/64]&(uint64(1)<<uint(register&63)) != 0
}

func slotNumericSetFact(facts []uint64, register int) {
	facts[register/64] |= uint64(1) << uint(register&63)
}

func (state *slotNumericAnalysisState) merge(pc int, facts []uint64) bool {
	if pc == state.codeLen {
		return true
	}
	if pc < 0 || pc >= state.codeLen {
		return false
	}
	row := state.row(pc)
	changed := false
	if state.reachable[pc] == 0 {
		copy(row, facts)
		state.reachable[pc] = 1
		changed = true
	} else {
		for index, incoming := range facts {
			merged := row[index] & incoming
			if merged != row[index] {
				row[index] = merged
				changed = true
			}
		}
	}
	if changed && state.queued[pc] == 0 {
		state.queued[pc] = 1
		state.work = append(state.work, pc)
	}
	return true
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
	clear(state.numericRegisters)
	clear(state.transient)
	state.registers = resetSlotExecutionBuffer(state.registers)
	state.constants = resetSlotExecutionBuffer(state.constants)
	state.results = resetSlotExecutionBuffer(state.results)
	state.numericRegisters = resetNumericExecutionBuffer(state.numericRegisters)
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

func resetNumericExecutionBuffer(values []float64) []float64 {
	if cap(values) > maxPooledSlotExecutionCapacity {
		return nil
	}
	return values[:0]
}

func (state *slotExecutionState) prepareNumericRegisters(count int) []float64 {
	if cap(state.numericRegisters) < count {
		state.numericRegisters = make([]float64, count)
	} else {
		state.numericRegisters = state.numericRegisters[:count]
		clear(state.numericRegisters)
	}
	return state.numericRegisters
}

// slotExecutionEligible is sealed onto Proto by finalizeProtoExecutionArtifact.
// The compact runner accepts only operations that cannot observe or mutate
// external state. If a runtime type check fails, execution can therefore be
// restarted safely in the established VM without duplicating an effect.
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
	registerOK := func(index int) bool { return index >= 0 && index < proto.registers }
	constantOK := func(index int) bool { return index >= 0 && index < len(proto.constants) }
	jumpOK := func(ins instruction) bool {
		target, ok := instructionJumpTarget(ins)
		return ok && target >= 0 && target <= len(code)
	}
	for _, ins := range code {
		switch ins.op {
		case opLoadConst:
			if !registerOK(ins.a) || !constantOK(ins.b) {
				return false
			}
		case opMove, opNeg:
			if !registerOK(ins.a) || !registerOK(ins.b) {
				return false
			}
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow,
			opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
			if !registerOK(ins.a) || !registerOK(ins.b) || !registerOK(ins.c) {
				return false
			}
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if !registerOK(ins.a) || !registerOK(ins.b) || !constantOK(ins.c) {
				return false
			}
		case opNumericForCheck:
			if !registerOK(ins.a) || !registerOK(ins.b) || !registerOK(ins.c) || !jumpOK(ins) {
				return false
			}
		case opNumericForLoop:
			if !registerOK(ins.a) || !registerOK(ins.b) || !jumpOK(ins) {
				return false
			}
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if !registerOK(ins.a) || !constantOK(ins.b) || !jumpOK(ins) {
				return false
			}
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if !registerOK(ins.a) || !registerOK(ins.b) || !jumpOK(ins) {
				return false
			}
		case opJumpIfFalse:
			if !registerOK(ins.a) || !jumpOK(ins) {
				return false
			}
		case opJump:
			if !jumpOK(ins) {
				return false
			}
		case opReturnOne:
			if !registerOK(ins.a) {
				return false
			}
		case opReturn:
			if ins.b < 0 || ins.a < 0 || ins.a+ins.b > proto.registers {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// slotExecutionNumericEligible proves the strict numeric subset with a compact
// forward data-flow pass. Every reachable register read must be numeric on
// every predecessor. Fixed arguments are validated once when execution enters
// the tier; opcode bodies then operate on raw float64 registers.
func slotExecutionNumericEligible(proto *Proto, code []instruction) bool {
	if proto == nil || !proto.slotExecutionEligible {
		return false
	}
	if len(code) == 0 {
		return true
	}
	analysis := acquireSlotNumericAnalysisState(len(code), proto.registers)
	defer releaseSlotNumericAnalysisState(analysis)
	entry := analysis.row(0)
	for register := 0; register < proto.params; register++ {
		slotNumericSetFact(entry, register)
	}
	analysis.reachable[0] = 1
	analysis.queued[0] = 1
	analysis.work = append(analysis.work, 0)

	for head := 0; head < len(analysis.work); head++ {
		pc := analysis.work[head]
		analysis.queued[pc] = 0
		facts := analysis.scratch
		copy(facts, analysis.row(pc))
		ins := code[pc]
		constantNumber := func(index int) bool {
			return index >= 0 && index < len(proto.constants) && valueKind(proto.constants[index]) == NumberKind
		}

		switch ins.op {
		case opLoadConst:
			if !constantNumber(ins.b) {
				return false
			}
			slotNumericSetFact(facts, ins.a)
		case opMove:
			if !slotNumericFact(facts, ins.b) {
				return false
			}
			slotNumericSetFact(facts, ins.a)
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
			if !slotNumericFact(facts, ins.b) || !slotNumericFact(facts, ins.c) {
				return false
			}
			slotNumericSetFact(facts, ins.a)
		case opNeg:
			if !slotNumericFact(facts, ins.b) {
				return false
			}
			slotNumericSetFact(facts, ins.a)
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if !slotNumericFact(facts, ins.b) || !constantNumber(ins.c) {
				return false
			}
			slotNumericSetFact(facts, ins.a)
		case opNumericForCheck:
			if !slotNumericFact(facts, ins.a) || !slotNumericFact(facts, ins.b) || !slotNumericFact(facts, ins.c) {
				return false
			}
		case opNumericForLoop:
			if !slotNumericFact(facts, ins.a) || !slotNumericFact(facts, ins.b) {
				return false
			}
			slotNumericSetFact(facts, ins.a)
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if !slotNumericFact(facts, ins.a) || !constantNumber(ins.b) {
				return false
			}
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if !slotNumericFact(facts, ins.a) || !slotNumericFact(facts, ins.b) {
				return false
			}
		case opJump:
		case opReturnOne:
			if !slotNumericFact(facts, ins.a) {
				return false
			}
		case opReturn:
			for register := ins.a; register < ins.a+ins.b; register++ {
				if !slotNumericFact(facts, register) {
					return false
				}
			}
		default:
			return false
		}

		switch opcodeControlFlow(ins.op) {
		case opcodeControlReturn:
			continue
		case opcodeControlJump:
			target, _ := instructionJumpTarget(ins)
			if !analysis.merge(target, facts) {
				return false
			}
		case opcodeControlBranch:
			target, _ := instructionJumpTarget(ins)
			if !analysis.merge(target, facts) || !analysis.merge(pc+1, facts) {
				return false
			}
		default:
			if !analysis.merge(pc+1, facts) {
				return false
			}
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
	if proto == nil || (!proto.slotExecutionEligible && proto.compact == nil) {
		return nil, false, nil
	}
	if proto.slotExecutionEligible {
		if proto.slotExecutionNumeric {
			state := acquireSlotExecutionState(0, 0)
			values, handled, err := runNumericSlotExecution(proto, args, state)
			releaseSlotExecutionState(state)
			if handled || err != nil {
				return values, handled, err
			}
		}
		state := acquireSlotExecutionState(proto.registers, len(proto.constants))
		defer releaseSlotExecutionState(state)
		return runSlotExecutionState(proto, args, state, false)
	}
	state := acquireCompactExecutionState()
	values, handled, err = runCompactNumericExecution(proto, args, state)
	releaseCompactExecutionState(state)
	return values, handled, err
}

// runSlotExecutionWithHeap executes an eligible prototype against a heap
// borrowed from its runtime owner. The borrowed heap is never reset here.
// Values created by this run are released when it ends; preexisting owner
// roots and pins are left untouched.
func runSlotExecutionWithHeap(proto *Proto, args []Value, heap *runtimeHeap) (values []Value, handled bool, err error) {
	if proto == nil || heap == nil || (!proto.slotExecutionEligible && proto.compact == nil) {
		return nil, false, nil
	}
	if !proto.slotExecutionEligible {
		state := acquireCompactExecutionState()
		values, handled, err := runCompactNumericExecution(proto, args, state)
		releaseCompactExecutionState(state)
		return values, handled, err
	}
	if proto.slotExecutionNumeric {
		state := acquireSlotExecutionState(0, 0)
		values, handled, err := runNumericSlotExecution(proto, args, state)
		releaseSlotExecutionState(state)
		if handled || err != nil {
			return values, handled, err
		}
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

// runNumericSlotExecution executes a compiler-proven numeric prototype over a
// raw float64 register file. Constants remain in immutable prototype storage;
// only fixed arguments cross the Value boundary before dispatch.
func runNumericSlotExecution(proto *Proto, args []Value, state *slotExecutionState) ([]Value, bool, error) {
	if proto == nil || state == nil || !proto.slotExecutionNumeric || len(args) < proto.params {
		return nil, false, nil
	}
	registers := state.prepareNumericRegisters(proto.registers)
	for index := 0; index < proto.params; index++ {
		if valueKind(args[index]) != NumberKind {
			return nil, false, nil
		}
		registers[index] = valueNumber(args[index])
	}
	start, count, ok := runNumericSlotExecutionWords(proto, registers)
	if !ok {
		return nil, false, nil
	}
	if count == 0 {
		return nil, true, nil
	}
	values := make([]Value, count)
	for index := range values {
		number := registers[start+index]
		// Preserve the tagged runner's safe-fallback contract for the rare
		// quiet-NaN payloads that overlap the slot tag prefix.
		if slotExecutionNumberNeedsBox(number) {
			return nil, false, nil
		}
		values[index] = NumberValue(number)
	}
	return values, true, nil
}

// runNumericSlotExecutionWords is the hot numeric tier. Eligibility and
// wordcode verification happen once at compilation, so dispatch performs one
// opcode switch with no Value-kind or tagged-slot checks.
func runNumericSlotExecutionWords(proto *Proto, registers []float64) (int, int, bool) {
	words := proto.words
	constants := proto.constantNumbers
	pc := 0
	for uint(pc) < uint(len(words)) {
		raw := words[pc]
		op := opcode(uint8(raw) & uint8(wordcodeOpcodeMask))
		a := int(uint8(raw >> 8))
		b := int(uint8(raw >> 16))
		c := int(uint8(raw >> 24))
		next := pc + 1

		switch op {
		case opLoadConst:
			b = int(uint16(raw >> 16))
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, 0, false
				}
				b = int(int32(words[next]))
				next++
			}
			registers[a] = constants[b]

		case opMove:
			registers[a] = registers[b]

		case opAdd:
			registers[a] = registers[b] + registers[c]
		case opSub:
			registers[a] = registers[b] - registers[c]
		case opMul:
			registers[a] = registers[b] * registers[c]
		case opDiv:
			registers[a] = registers[b] / registers[c]
		case opMod:
			left, right := registers[b], registers[c]
			registers[a] = left - math.Floor(left/right)*right
		case opIDiv:
			registers[a] = math.Floor(registers[b] / registers[c])
		case opPow:
			registers[a] = math.Pow(registers[b], registers[c])
		case opNeg:
			registers[a] = -registers[b]

		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, 0, false
				}
				c = int(int32(words[next]))
				next++
			}
			left, right := registers[b], constants[c]
			switch op {
			case opAddK:
				registers[a] = left + right
			case opSubK:
				registers[a] = left - right
			case opMulK:
				registers[a] = left * right
			case opDivK:
				registers[a] = left / right
			case opModK:
				registers[a] = left - math.Floor(left/right)*right
			case opIDivK:
				registers[a] = math.Floor(left / right)
			}

		case opNumericForCheck:
			if next >= len(words) {
				return 0, 0, false
			}
			target := int(int32(words[next]))
			next++
			target += next
			loopValue, limitValue, stepValue := registers[a], registers[b], registers[c]
			if math.IsNaN(loopValue) || math.IsNaN(limitValue) || math.IsNaN(stepValue) {
				return 0, 0, false
			}
			if (stepValue > 0 && loopValue > limitValue) || (stepValue <= 0 && loopValue < limitValue) {
				pc = target
				continue
			}

		case opNumericForLoop:
			if next >= len(words) {
				return 0, 0, false
			}
			target := int(int32(words[next]))
			next++
			target += next
			registers[a] += registers[b]
			pc = target
			continue

		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if next >= len(words) {
				return 0, 0, false
			}
			b = int(uint16(raw >> 16))
			target := int(int32(words[next]))
			next++
			target += next
			left, right := registers[a], constants[b]
			jump := false
			switch op {
			case opJumpIfNotEqualK:
				jump = left != right
			default:
				if math.IsNaN(left) || math.IsNaN(right) {
					return 0, 0, false
				}
				switch op {
				case opJumpIfNotLessK:
					jump = !(left < right)
				case opJumpIfNotGreaterK:
					jump = !(left > right)
				case opJumpIfLessK:
					jump = left < right
				case opJumpIfGreaterK:
					jump = left > right
				}
			}
			if jump {
				pc = target
				continue
			}

		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if next >= len(words) {
				return 0, 0, false
			}
			target := int(int32(words[next]))
			next++
			target += next
			left, right := registers[a], registers[b]
			if math.IsNaN(left) || math.IsNaN(right) {
				return 0, 0, false
			}
			jump := false
			switch op {
			case opJumpIfNotLess:
				jump = !(left < right)
			case opJumpIfNotGreater:
				jump = !(left > right)
			case opJumpIfLess:
				jump = left < right
			case opJumpIfGreater:
				jump = left > right
			}
			if jump {
				pc = target
				continue
			}

		case opJump:
			pc = int(int32(raw)>>8) + next
			continue

		case opReturnOne:
			return a, 1, true
		case opReturn:
			b = int(int16(uint16(raw >> 16)))
			if b < 0 || a < 0 || a+b > len(registers) {
				return 0, 0, false
			}
			return a, b, true
		default:
			return 0, 0, false
		}
		pc = next
	}
	return 0, 0, true
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
	registers := state.registers
	constants := state.constants
	pc := 0
	for uint(pc) < uint(len(words)) {
		raw := words[pc]
		op := opcode(uint8(raw) & uint8(wordcodeOpcodeMask))
		a := int(uint8(raw >> 8))
		b := int(uint8(raw >> 16))
		c := int(uint8(raw >> 24))
		next := pc + 1

		switch op {
		case opLoadConst:
			b = int(uint16(raw >> 16))
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, false
				}
				b = int(int32(words[next]))
				next++
			}
			registers[a] = constants[b]

		case opMove:
			registers[a] = registers[b]

		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
			left, leftOK := slotExecutionNumber(registers[b])
			right, rightOK := slotExecutionNumber(registers[c])
			if !leftOK || !rightOK {
				return 0, false
			}
			var result float64
			switch op {
			case opAdd:
				result = left + right
			case opSub:
				result = left - right
			case opMul:
				result = left * right
			case opDiv:
				result = left / right
			case opMod:
				result = left - math.Floor(left/right)*right
			case opIDiv:
				result = math.Floor(left / right)
			case opPow:
				result = math.Pow(left, right)
			}
			if !slotExecutionStoreNumber(registers, a, result) {
				return 0, false
			}

		case opNeg:
			operand, ok := slotExecutionNumber(registers[b])
			if !ok || !slotExecutionStoreNumber(registers, a, -operand) {
				return 0, false
			}

		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, false
				}
				c = int(int32(words[next]))
				next++
			}
			left, leftOK := slotExecutionNumber(registers[b])
			right, rightOK := slotExecutionNumber(constants[c])
			if !leftOK || !rightOK {
				return 0, false
			}
			var result float64
			switch op {
			case opAddK:
				result = left + right
			case opSubK:
				result = left - right
			case opMulK:
				result = left * right
			case opDivK:
				result = left / right
			case opModK:
				result = left - math.Floor(left/right)*right
			case opIDivK:
				result = math.Floor(left / right)
			}
			if !slotExecutionStoreNumber(registers, a, result) {
				return 0, false
			}

		case opEqual, opNotEqual:
			equal, ok := slotExecutionEqual(state, registers[b], registers[c])
			if !ok {
				return 0, false
			}
			if op == opNotEqual {
				equal = !equal
			}
			registers[a] = slotBool(equal)

		case opLess, opLessEqual, opGreater, opGreaterEqual:
			left, right := registers[b], registers[c]
			if op == opGreater || op == opGreaterEqual {
				left, right = right, left
			}
			result, ok := slotExecutionLess(state, left, right, op == opLessEqual || op == opGreaterEqual)
			if !ok {
				return 0, false
			}
			registers[a] = slotBool(result)

		case opNumericForCheck:
			if next >= len(words) {
				return 0, false
			}
			target := int(int32(words[next]))
			next++
			target += next
			loopValue, loopOK := slotExecutionNumber(registers[a])
			limitValue, limitOK := slotExecutionNumber(registers[b])
			stepValue, stepOK := slotExecutionNumber(registers[c])
			if !loopOK || !limitOK || !stepOK || math.IsNaN(loopValue) || math.IsNaN(limitValue) || math.IsNaN(stepValue) {
				return 0, false
			}
			if (stepValue > 0 && loopValue > limitValue) || (stepValue <= 0 && loopValue < limitValue) {
				pc = target
				continue
			}

		case opNumericForLoop:
			if next >= len(words) {
				return 0, false
			}
			target := int(int32(words[next]))
			next++
			target += next
			loopValue, loopOK := slotExecutionNumber(registers[a])
			stepValue, stepOK := slotExecutionNumber(registers[b])
			if !loopOK || !stepOK || !slotExecutionStoreNumber(registers, a, loopValue+stepValue) {
				return 0, false
			}
			pc = target
			continue

		case opJumpIfNotEqualK:
			if next >= len(words) {
				return 0, false
			}
			b = int(uint16(raw >> 16))
			target := int(int32(words[next]))
			next++
			target += next
			equal, ok := slotExecutionEqual(state, registers[a], constants[b])
			if !ok {
				return 0, false
			}
			if !equal {
				pc = target
				continue
			}

		case opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if next >= len(words) {
				return 0, false
			}
			b = int(uint16(raw >> 16))
			target := int(int32(words[next]))
			next++
			target += next
			left, right := registers[a], constants[b]
			if op == opJumpIfNotGreaterK || op == opJumpIfGreaterK {
				left, right = right, left
			}
			less, ok := slotExecutionLess(state, left, right, false)
			if !ok {
				return 0, false
			}
			jump := less
			if op == opJumpIfNotLessK || op == opJumpIfNotGreaterK {
				jump = !jump
			}
			if jump {
				pc = target
				continue
			}

		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if next >= len(words) {
				return 0, false
			}
			target := int(int32(words[next]))
			next++
			target += next
			left, right := registers[a], registers[b]
			if op == opJumpIfNotGreater || op == opJumpIfGreater {
				left, right = right, left
			}
			less, ok := slotExecutionLess(state, left, right, false)
			if !ok {
				return 0, false
			}
			jump := less
			if op == opJumpIfNotLess || op == opJumpIfNotGreater {
				jump = !jump
			}
			if jump {
				pc = target
				continue
			}

		case opJumpIfFalse:
			b = int(int16(uint16(raw >> 16)))
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, false
				}
				b = int(int32(words[next]))
				next++
			}
			target := b + next
			truthy, ok := slotExecutionTruthy(registers[a])
			if !ok {
				return 0, false
			}
			if !truthy {
				pc = target
				continue
			}

		case opJump:
			pc = int(int32(raw)>>8) + next
			continue

		case opReturnOne:
			state.results = append(state.results[:0], registers[a])
			return 1, true
		case opReturn:
			b = int(int16(uint16(raw >> 16)))
			if b < 0 || a < 0 || a+b > len(registers) {
				return 0, false
			}
			if b == 0 {
				return 0, true
			}
			state.results = append(state.results[:0], registers[a:a+b]...)
			return b, true
		default:
			return 0, false
		}
		pc = next
	}
	return 0, true
}

func slotExecutionTruthy(value slot) (bool, bool) {
	if !slotIsTagged(value) {
		return true, true
	}
	switch slotTagOf(value) {
	case slotTagNil:
		return false, slotImmediatePayloadZero(value)
	case slotTagFalse:
		return false, slotImmediatePayloadZero(value)
	case slotTagTrue:
		return true, slotImmediatePayloadZero(value)
	case slotTagString, slotTagTable, slotTagClosure, slotTagUserdata, slotTagHostCallable, slotTagNativeID, slotTagBoxedNumber:
		return true, true
	default:
		return false, false
	}
}

func slotExecutionEqual(state *slotExecutionState, left slot, right slot) (bool, bool) {
	leftKind := slotValueKind(left)
	rightKind := slotValueKind(right)
	if leftKind == slotInvalidValueKind || rightKind == slotInvalidValueKind {
		return false, false
	}
	if leftKind != rightKind {
		return false, true
	}
	if leftKind == TableKind {
		// Table equality may invoke __eq. Restart in the established VM before
		// observing a result or running user code.
		return false, false
	}
	if !slotIsTagged(left) && !slotIsTagged(right) {
		leftNumber := math.Float64frombits(uint64(left))
		rightNumber := math.Float64frombits(uint64(right))
		if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
			return false, true
		}
		return leftNumber == rightNumber, true
	}
	leftValue, leftOK := slotExecutionExport(state, left)
	rightValue, rightOK := slotExecutionExport(state, right)
	if !leftOK || !rightOK {
		return false, false
	}
	return valuesEqual(leftValue, rightValue), true
}

func slotExecutionLess(state *slotExecutionState, left slot, right slot, orEqual bool) (bool, bool) {
	if !slotIsTagged(left) && !slotIsTagged(right) {
		leftNumber := math.Float64frombits(uint64(left))
		rightNumber := math.Float64frombits(uint64(right))
		if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
			return false, false
		}
		if orEqual {
			return leftNumber <= rightNumber, true
		}
		return leftNumber < rightNumber, true
	}
	leftValue, leftOK := slotExecutionExport(state, left)
	rightValue, rightOK := slotExecutionExport(state, right)
	if !leftOK || !rightOK {
		return false, false
	}
	var (
		result bool
		err    error
	)
	if orEqual {
		result, err = valuesLessEqual(leftValue, rightValue)
	} else {
		result, err = valuesLess(leftValue, rightValue)
	}
	return result, err == nil
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
