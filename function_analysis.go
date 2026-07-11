package ember

import "math"

type functionIR struct {
	instructions []bytecodeIRInstruction
	revision     uint64
	analysis     *functionAnalysis
}

type functionAnalysis struct {
	revision     uint64
	blocks       []bytecodeIRBlock
	successors   [][]int
	predecessors [][]int
	reachable    []bool
	use          []registerSet
	def          []registerSet
	liveness     []bytecodeIRLivenessBlock
	// liveAfter is ephemeral finalization data, never a Proto side table.
	liveAfter []registerSet
	effects   []opcodeEffects
}

func newFunctionIR(ir []bytecodeIRInstruction) *functionIR {
	return &functionIR{instructions: ir}
}

func (function *functionIR) replace(ir []bytecodeIRInstruction) {
	if function == nil {
		return
	}
	if !equalBytecodeIR(function.instructions, ir) {
		function.revision++
		function.analysis = nil
	}
	function.instructions = ir
}

func (function *functionIR) currentAnalysis() *functionAnalysis {
	if function == nil {
		return nil
	}
	if function.analysis == nil || function.analysis.revision != function.revision {
		function.analysis = analyzeBytecodeIR(function.instructions, function.revision)
	}
	return function.analysis
}

func equalBytecodeIR(left []bytecodeIRInstruction, right []bytecodeIRInstruction) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func analyzeBytecodeIR(ir []bytecodeIRInstruction, revision uint64) *functionAnalysis {
	blocks := bytecodeIRBlockOrder(ir)
	successors := bytecodeIRBlockSuccessors(ir, blocks)
	liveness := bytecodeIRLivenessForGraph(ir, blocks, successors)
	analysis := &functionAnalysis{
		revision:     revision,
		blocks:       blocks,
		successors:   successors,
		predecessors: bytecodeIRBlockPredecessors(successors),
		reachable:    bytecodeIRReachableBlocks(successors),
		use:          make([]registerSet, len(liveness)),
		def:          make([]registerSet, len(liveness)),
		liveness:     liveness,
		liveAfter:    bytecodeIRLiveAfter(ir, blocks, liveness),
		effects:      make([]opcodeEffects, len(ir)),
	}
	for block := range liveness {
		analysis.use[block] = liveness[block].use
		analysis.def[block] = liveness[block].def
	}
	for pc, ins := range ir {
		analysis.effects[pc] = opcodeEffect(ins.op)
	}
	return analysis
}

func bytecodeIRLiveAfter(ir []bytecodeIRInstruction, blocks []bytecodeIRBlock, liveness []bytecodeIRLivenessBlock) []registerSet {
	if len(ir) == 0 {
		return nil
	}
	liveAfter := make([]registerSet, len(ir))
	for index, block := range blocks {
		if index < 0 || index >= len(liveness) {
			continue
		}
		live := liveness[index].liveOut.copy()
		for pc := block.end - 1; pc >= block.start; pc-- {
			liveAfter[pc].assign(live)
			raw := assembleBytecodeIRInstruction(ir[pc])
			writes := instructionRegisters(raw, instructionRegisterWrite)
			for register, ok := writes.next(); ok; register, ok = writes.next() {
				live.remove(register)
			}
			reads := instructionRegisters(raw, instructionRegisterRead)
			for register, ok := reads.next(); ok; register, ok = reads.next() {
				live.add(register)
			}
		}
	}
	return liveAfter
}

// fixedCallBorrowFact is an ephemeral proof used while finalizing code. The
// marker is the only persistent output reserved for a future runtime-window
// activation.
type fixedCallBorrowFact struct {
	pc            int
	op            opcode
	argumentStart int
	argumentCount int
	result        int
	suffixStart   int
	suffixEnd     int
	eligible      bool
	reason        string
}

func fixedCallBorrowShape(ins instruction) (argumentStart, argumentCount, result, rawCount int, ok bool) {
	switch ins.op {
	case opCallOne:
		argumentCount, _ = decodeFixedCallCount(ins.c)
		return ins.b + 1, argumentCount, ins.a, ins.c, true
	case opCallLocalOne, opCallUpvalueOne:
		argumentCount, _ = decodeFixedCallCount(ins.d)
		return ins.c, argumentCount, ins.a, ins.d, true
	case opCallMethodOne:
		explicitCount, _ := decodeFixedCallCount(ins.d)
		// The VM stages receiver/self at A+1 before the explicit arguments.
		// The result at A remains outside the borrowed callee window.
		return ins.a + 1, explicitCount + 1, ins.a, ins.d, true
	default:
		return 0, 0, 0, 0, false
	}
}

func fixedCallBorrowFactForInstruction(ins instruction, pc int, registers int, capturedLocals []bool, liveAfter registerSet) fixedCallBorrowFact {
	fact := fixedCallBorrowFact{pc: pc, op: ins.op, suffixEnd: registers}
	argumentStart, argumentCount, result, rawCount, ok := fixedCallBorrowShape(ins)
	if !ok {
		return fact
	}
	fact.argumentStart = argumentStart
	fact.argumentCount = argumentCount
	fact.result = result
	fact.suffixStart = argumentStart + argumentCount
	if rawCount < -32768 || rawCount > 32767 {
		fact.reason = "argument count is outside packed int16 range"
		return fact
	}
	if registers < 0 || argumentStart < 0 || argumentStart > registers || argumentCount < 0 || argumentCount > registers-argumentStart {
		fact.reason = "argument window is outside caller registers"
		return fact
	}
	if result < 0 || result >= registers {
		fact.reason = "result destination is outside caller registers"
		return fact
	}
	if result >= fact.suffixStart && result < registers {
		fact.reason = "result destination overlaps the scratch suffix"
		return fact
	}
	if len(capturedLocals) > result && capturedLocals[result] {
		fact.reason = "result destination is captured"
		return fact
	}
	for register := argumentStart; register < registers; register++ {
		if register < len(capturedLocals) && capturedLocals[register] {
			fact.reason = "borrowed suffix contains a captured local"
			return fact
		}
		if register != result && liveAfter.contains(register) {
			fact.reason = "borrowed suffix remains live after the call"
			return fact
		}
	}
	fact.eligible = true
	return fact
}

func analyzeFixedCallBorrowFacts(code []instruction, registers int, capturedLocals []bool) []fixedCallBorrowFact {
	if len(code) == 0 {
		return nil
	}
	ir := lowerInstructionsToBytecodeIR(code)
	analysis := analyzeBytecodeIR(ir, 0)
	facts := make([]fixedCallBorrowFact, 0)
	for pc, ins := range code {
		if _, _, _, _, ok := fixedCallBorrowShape(ins); !ok {
			continue
		}
		var liveAfter registerSet
		if pc < len(analysis.liveAfter) {
			liveAfter = analysis.liveAfter[pc]
		}
		facts = append(facts, fixedCallBorrowFactForInstruction(ins, pc, registers, capturedLocals, liveAfter))
	}
	return facts
}

// markBorrowableFixedCallWindows encodes a borrow hint only after liveness and
// capture analysis prove that the fixed-call register suffix is dead. All four
// runtime fixed-call forms decode the hint and retain guarded cold fallbacks.
func markBorrowableFixedCallWindows(code []instruction, registers int, capturedLocals []bool) []instruction {
	hasFixedCall := false
	for _, ins := range code {
		if _, _, _, _, ok := fixedCallBorrowShape(ins); ok {
			hasFixedCall = true
			break
		}
	}
	if !hasFixedCall {
		return code
	}
	marked := append([]instruction(nil), code...)
	for _, fact := range analyzeFixedCallBorrowFacts(code, registers, capturedLocals) {
		if !fact.eligible || fact.pc < 0 || fact.pc >= len(marked) {
			continue
		}
		switch marked[fact.pc].op {
		case opCallOne:
			marked[fact.pc].c = encodeFixedCallCount(fact.argumentCount, true)
		case opCallLocalOne, opCallUpvalueOne:
			marked[fact.pc].d = encodeFixedCallCount(fact.argumentCount, true)
		case opCallMethodOne:
			marked[fact.pc].d = encodeFixedCallCount(fact.argumentCount-1, true)
		}
	}
	return marked
}

func bytecodeIRBlockPredecessors(successors [][]int) [][]int {
	predecessors := make([][]int, len(successors))
	for block, next := range successors {
		for _, successor := range next {
			if successor >= 0 && successor < len(predecessors) {
				predecessors[successor] = append(predecessors[successor], block)
			}
		}
	}
	return predecessors
}

func bytecodeIRReachableBlocks(successors [][]int) []bool {
	if len(successors) == 0 {
		return nil
	}
	reachable := make([]bool, len(successors))
	worklist := []int{0}
	reachable[0] = true
	for len(worklist) != 0 {
		last := len(worklist) - 1
		block := worklist[last]
		worklist = worklist[:last]
		for _, successor := range successors[block] {
			if successor < 0 || successor >= len(reachable) || reachable[successor] {
				continue
			}
			reachable[successor] = true
			worklist = append(worklist, successor)
		}
	}
	return reachable
}

// genericFieldBlockPlan is a transient analysis result for the one generic
// candidate selected by the structural-runtime-optimization plan.  It is
// deliberately not attached to Proto or to the VM: Slice 6.2 proves that a
// region is safe to consider, while a later slice may decide whether and how
// to execute one.
//
// The descriptor names the canonical instruction range, the instructions
// making up the read/modify/write shape, and all guard/side-exit PCs needed to
// re-check the proof.  The slices are owned by the result and never retained
// by a prototype.
type genericFieldBlockPlan struct {
	kind genericFieldBlockPlanKind

	canonicalStartPC int
	canonicalEndPC   int // exclusive
	readPC           int
	modifyPC         int
	writePC          int

	tableRegister int
	keyConstant   int
	valueRegister int
	tableIdentity int
	valueKind     ValueKind
	effectEpoch   uint64
	// These are transient validation facts, not runtime state.
	tableGuardSource int
	tableLayoutEpoch uint64

	guardPCs    []int
	sideExitPCs []int
}

type genericFieldBlockPlanKind uint8

const (
	genericFieldReadModifyWrite genericFieldBlockPlanKind = iota + 1
)

// genericFieldPlan is kept as an alias for callers that describe this result
// as a plan rather than a block.  Both names remain private intentionally.
type genericFieldPlan = genericFieldBlockPlan

// genericFieldValueFact is a must-fact for one register.  Independent parts
// of a fact may survive a join (for example, both paths can prove NumberKind
// while disagreeing about the exact constant).  A table identity is the
// allocation PC of an opNewTable and is therefore also an escape token.
type genericFieldValueFact struct {
	kind          ValueKind
	kindKnown     bool
	constant      int
	constantKnown bool
	tableID       int
	tableKnown    bool
}

type genericFieldTableFact struct {
	guardPC      int
	guardKnown   bool
	noMetatable  bool
	allocationPC int
	layoutEpoch  uint64
	layoutKnown  bool
}

type genericFieldKey struct {
	tableID     int
	keyConstant int
}

type genericFieldDataflowState struct {
	registers   []genericFieldValueFact
	tables      map[int]genericFieldTableFact
	fields      map[genericFieldKey]genericFieldValueFact
	effectEpoch uint64
	epochKnown  bool
}

type genericFieldRead struct {
	tableRegister int
	tableID       int
	keyConstant   int
	destination   int
	value         genericFieldValueFact
	guardPC       int
}

type genericFieldWrite struct {
	tableID       int
	keyConstant   int
	valueRegister int
}

func unknownGenericFieldValueFact() genericFieldValueFact {
	return genericFieldValueFact{constant: -1, tableID: -1}
}

func newGenericFieldDataflowState(registers int) genericFieldDataflowState {
	state := genericFieldDataflowState{
		registers:  make([]genericFieldValueFact, registers),
		tables:     make(map[int]genericFieldTableFact),
		fields:     make(map[genericFieldKey]genericFieldValueFact),
		epochKnown: true,
	}
	for register := range state.registers {
		state.registers[register] = unknownGenericFieldValueFact()
	}
	return state
}

func (state genericFieldDataflowState) copy() genericFieldDataflowState {
	copyState := genericFieldDataflowState{
		registers:   append([]genericFieldValueFact(nil), state.registers...),
		tables:      make(map[int]genericFieldTableFact, len(state.tables)),
		fields:      make(map[genericFieldKey]genericFieldValueFact, len(state.fields)),
		effectEpoch: state.effectEpoch,
		epochKnown:  state.epochKnown,
	}
	for identity, table := range state.tables {
		copyState.tables[identity] = table
	}
	for key, value := range state.fields {
		copyState.fields[key] = value
	}
	return copyState
}

func equalGenericFieldValueFact(left, right genericFieldValueFact) bool {
	return left == right
}

func equalGenericFieldState(left, right genericFieldDataflowState) bool {
	if left.effectEpoch != right.effectEpoch || left.epochKnown != right.epochKnown || len(left.registers) != len(right.registers) {
		return false
	}
	for register := range left.registers {
		if !equalGenericFieldValueFact(left.registers[register], right.registers[register]) {
			return false
		}
	}
	if len(left.tables) != len(right.tables) || len(left.fields) != len(right.fields) {
		return false
	}
	for identity, table := range left.tables {
		if other, ok := right.tables[identity]; !ok || other != table {
			return false
		}
	}
	for key, value := range left.fields {
		if other, ok := right.fields[key]; !ok || !equalGenericFieldValueFact(other, value) {
			return false
		}
	}
	return true
}

// joinGenericFieldValueFacts keeps only facts that are true on both paths.
// Unknown parts are not allowed to acquire a value merely because one path
// happened to prove it.
func joinGenericFieldValueFacts(left, right genericFieldValueFact) genericFieldValueFact {
	joined := unknownGenericFieldValueFact()
	if left.kindKnown && right.kindKnown && left.kind == right.kind {
		joined.kind = left.kind
		joined.kindKnown = true
	}
	if left.constantKnown && right.constantKnown && left.constant == right.constant {
		joined.constant = left.constant
		joined.constantKnown = true
	}
	if left.tableKnown && right.tableKnown && left.tableID == right.tableID {
		joined.tableID = left.tableID
		joined.tableKnown = true
	}
	return joined
}

func joinGenericFieldStates(left, right genericFieldDataflowState) genericFieldDataflowState {
	if len(left.registers) != len(right.registers) {
		return newGenericFieldDataflowState(0)
	}
	joined := newGenericFieldDataflowState(len(left.registers))
	for register := range joined.registers {
		joined.registers[register] = joinGenericFieldValueFacts(left.registers[register], right.registers[register])
	}
	if left.epochKnown && right.epochKnown && left.effectEpoch == right.effectEpoch {
		joined.effectEpoch = left.effectEpoch
	} else {
		joined.epochKnown = false
	}
	for identity, leftTable := range left.tables {
		rightTable, ok := right.tables[identity]
		if !ok || leftTable != rightTable {
			continue
		}
		joined.tables[identity] = leftTable
	}
	for key, leftValue := range left.fields {
		rightValue, ok := right.fields[key]
		if !ok {
			continue
		}
		value := joinGenericFieldValueFacts(leftValue, rightValue)
		if value.kindKnown || value.constantKnown || value.tableKnown {
			joined.fields[key] = value
		}
	}
	return joined
}

func (state *genericFieldDataflowState) clearRegister(register int) {
	if state == nil || register < 0 || register >= len(state.registers) {
		return
	}
	state.registers[register] = unknownGenericFieldValueFact()
}

func (state *genericFieldDataflowState) clearInstructionWrites(ins instruction) {
	if state == nil {
		return
	}
	writes := instructionRegistersBounded(ins, instructionRegisterWrite, len(state.registers))
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		state.clearRegister(register)
	}
}

func (state *genericFieldDataflowState) bumpEffectEpoch() {
	if state == nil || !state.epochKnown {
		return
	}
	if state.effectEpoch == ^uint64(0) {
		state.epochKnown = false
		return
	}
	state.effectEpoch++
}

func (state *genericFieldDataflowState) invalidateAll() {
	if state == nil {
		return
	}
	state.bumpEffectEpoch()
	state.tables = make(map[int]genericFieldTableFact)
	state.fields = make(map[genericFieldKey]genericFieldValueFact)
	for register := range state.registers {
		state.registers[register] = unknownGenericFieldValueFact()
	}
}

func (state *genericFieldDataflowState) invalidateTable(identity int) {
	if state == nil || identity < 0 {
		return
	}
	state.bumpEffectEpoch()
	delete(state.tables, identity)
	for key := range state.fields {
		if key.tableID == identity {
			delete(state.fields, key)
		}
	}
	for register, value := range state.registers {
		if value.tableKnown && value.tableID == identity {
			state.clearRegister(register)
		}
	}
}

func (state *genericFieldDataflowState) escapeTableValue(value genericFieldValueFact) {
	if value.tableKnown {
		state.invalidateTable(value.tableID)
	}
}

func (state genericFieldDataflowState) tableFact(value genericFieldValueFact) (genericFieldTableFact, bool) {
	if !value.kindKnown || value.kind != TableKind || !value.tableKnown {
		return genericFieldTableFact{}, false
	}
	table, ok := state.tables[value.tableID]
	if !ok || table.allocationPC != value.tableID || !table.guardKnown || !table.noMetatable {
		return genericFieldTableFact{}, false
	}
	return table, true
}

func (state genericFieldDataflowState) fieldFact(tableID, keyConstant int) (genericFieldValueFact, bool) {
	value, ok := state.fields[genericFieldKey{tableID: tableID, keyConstant: keyConstant}]
	return value, ok
}

func genericFieldConstant(constants []Value, index int) (Value, bool) {
	if index < 0 || index >= len(constants) {
		return Value{}, false
	}
	return constants[index], true
}

func genericFieldStringConstant(constants []Value, index int) bool {
	value, ok := genericFieldConstant(constants, index)
	return ok && valueKind(value) == StringKind
}

func genericFieldNumberConstant(constants []Value, index int) (float64, bool) {
	value, ok := genericFieldConstant(constants, index)
	if !ok || valueKind(value) != NumberKind {
		return 0, false
	}
	number, ok := value.Number()
	return number, ok
}

func genericFieldValueIsNonNaNNumber(value genericFieldValueFact, constants []Value) bool {
	if !value.kindKnown || value.kind != NumberKind || !value.constantKnown {
		return false
	}
	number, ok := genericFieldNumberConstant(constants, value.constant)
	return ok && !math.IsNaN(number)
}

func genericFieldRegisterFact(state genericFieldDataflowState, register int) (genericFieldValueFact, bool) {
	if register < 0 || register >= len(state.registers) {
		return genericFieldValueFact{}, false
	}
	value := state.registers[register]
	return value, value.kindKnown || value.constantKnown || value.tableKnown
}

func genericFieldKeyFromRegister(state genericFieldDataflowState, register int) (int, bool) {
	value, ok := genericFieldRegisterFact(state, register)
	return value.constant, ok && value.constantKnown
}

func genericFieldTableKeyForRead(ins instruction, state genericFieldDataflowState, constants []Value) (int, int, int, bool) {
	switch ins.op {
	case opGetStringField:
		if !genericFieldStringConstant(constants, ins.c) {
			return 0, 0, 0, false
		}
		return ins.b, ins.c, ins.a, true
	case opGetIndex:
		key, ok := genericFieldKeyFromRegister(state, ins.c)
		if !ok || !genericFieldStringConstant(constants, key) {
			return 0, 0, 0, false
		}
		return ins.b, key, ins.a, true
	default:
		return 0, 0, 0, false
	}
}

func genericFieldReadAt(ins instruction, state genericFieldDataflowState, constants []Value) (genericFieldRead, bool) {
	tableRegister, keyConstant, destination, ok := genericFieldTableKeyForRead(ins, state, constants)
	if !ok {
		return genericFieldRead{}, false
	}
	tableValue, ok := genericFieldRegisterFact(state, tableRegister)
	if !ok {
		return genericFieldRead{}, false
	}
	table, ok := state.tableFact(tableValue)
	if !ok {
		return genericFieldRead{}, false
	}
	value, ok := state.fieldFact(tableValue.tableID, keyConstant)
	if !ok || !genericFieldValueIsNonNaNNumber(value, constants) {
		return genericFieldRead{}, false
	}
	return genericFieldRead{
		tableRegister: tableRegister,
		tableID:       tableValue.tableID,
		keyConstant:   keyConstant,
		destination:   destination,
		value:         value,
		guardPC:       table.guardPC,
	}, true
}

func genericFieldWriteAt(ins instruction, state genericFieldDataflowState, constants []Value) (genericFieldWrite, bool) {
	var tableRegister, keyConstant, valueRegister int
	switch ins.op {
	case opSetStringField:
		tableRegister, keyConstant, valueRegister = ins.a, ins.b, ins.c
		if !genericFieldStringConstant(constants, keyConstant) {
			return genericFieldWrite{}, false
		}
	case opSetField:
		tableRegister, keyConstant, valueRegister = ins.a, ins.b, ins.c
		if !genericFieldStringConstant(constants, keyConstant) {
			return genericFieldWrite{}, false
		}
	case opSetIndex:
		tableRegister, valueRegister = ins.a, ins.c
		var ok bool
		keyConstant, ok = genericFieldKeyFromRegister(state, ins.b)
		if !ok || !genericFieldStringConstant(constants, keyConstant) {
			return genericFieldWrite{}, false
		}
	default:
		return genericFieldWrite{}, false
	}
	tableValue, ok := genericFieldRegisterFact(state, tableRegister)
	if !ok {
		return genericFieldWrite{}, false
	}
	if _, ok := state.tableFact(tableValue); !ok {
		return genericFieldWrite{}, false
	}
	return genericFieldWrite{tableID: tableValue.tableID, keyConstant: keyConstant, valueRegister: valueRegister}, true
}

func genericFieldArithmeticFact(ins instruction, state genericFieldDataflowState, constants []Value, read genericFieldRead) (int, bool) {
	if ins.a < 0 || ins.a >= len(state.registers) {
		return 0, false
	}
	if ins.op == opNeg {
		if ins.b != read.destination || !genericFieldValueIsNonNaNNumber(read.value, constants) {
			return 0, false
		}
		return ins.a, true
	}
	var other genericFieldValueFact
	var otherOK bool
	switch ins.op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		if ins.b == read.destination {
			other, otherOK = genericFieldRegisterFact(state, ins.c)
		} else if ins.c == read.destination {
			other, otherOK = genericFieldRegisterFact(state, ins.b)
		} else {
			return 0, false
		}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if ins.b != read.destination {
			return 0, false
		}
		number, ok := genericFieldNumberConstant(constants, ins.c)
		if !ok || math.IsNaN(number) {
			return 0, false
		}
		other = genericFieldValueFact{kind: NumberKind, kindKnown: true, constant: ins.c, constantKnown: true}
		otherOK = true
	default:
		return 0, false
	}
	if !otherOK || !genericFieldValueIsNonNaNNumber(other, constants) {
		return 0, false
	}
	// Integer division and modulo by zero take an error path.  Slice 6.2 does
	// not model a partial-write side exit, so suppress those regions rather
	// than relying on a later runtime guard to recreate error ordering.
	if (ins.op == opIDiv || ins.op == opMod) && other.constantKnown {
		if number, ok := genericFieldNumberConstant(constants, other.constant); ok && number == 0 {
			return 0, false
		}
	}
	return ins.a, true
}

func genericFieldArithmeticIsSafe(ins instruction, state genericFieldDataflowState, constants []Value) bool {
	registerValue := func(register int) (genericFieldValueFact, bool) {
		value, ok := genericFieldRegisterFact(state, register)
		return value, ok && genericFieldValueIsNonNaNNumber(value, constants)
	}
	switch ins.op {
	case opNeg:
		_, ok := registerValue(ins.b)
		return ok
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		left, leftOK := registerValue(ins.b)
		right, rightOK := registerValue(ins.c)
		return leftOK && rightOK && left.kind == NumberKind && right.kind == NumberKind
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		_, leftOK := registerValue(ins.b)
		right, rightOK := genericFieldNumberConstant(constants, ins.c)
		return leftOK && rightOK && !math.IsNaN(right)
	default:
		return false
	}
}

func genericFieldDirectTableAccess(ins instruction, state genericFieldDataflowState, constants []Value) bool {
	switch ins.op {
	case opGetStringField, opSetStringField, opSetField:
		var tableRegister int
		if ins.op == opGetStringField {
			tableRegister = ins.b
		} else {
			tableRegister = ins.a
		}
		value, ok := genericFieldRegisterFact(state, tableRegister)
		if !ok {
			return false
		}
		_, ok = state.tableFact(value)
		if !ok {
			return false
		}
		if ins.op == opSetField {
			return genericFieldStringConstant(constants, ins.b)
		}
		if ins.op == opGetStringField {
			return genericFieldStringConstant(constants, ins.c)
		}
		return genericFieldStringConstant(constants, ins.b)
	case opGetIndex, opSetIndex:
		tableRegister := ins.b
		if ins.op == opSetIndex {
			tableRegister = ins.a
		}
		value, ok := genericFieldRegisterFact(state, tableRegister)
		if !ok {
			return false
		}
		if _, ok := state.tableFact(value); !ok {
			return false
		}
		keyRegister := ins.c
		if ins.op == opSetIndex {
			keyRegister = ins.b
		}
		key, ok := genericFieldKeyFromRegister(state, keyRegister)
		return ok && genericFieldStringConstant(constants, key)
	default:
		return false
	}
}

func genericFieldWriteState(state *genericFieldDataflowState, write genericFieldWrite, value genericFieldValueFact) {
	if state == nil {
		return
	}
	state.bumpEffectEpoch()
	if table, ok := state.tables[write.tableID]; ok {
		table.layoutEpoch = state.effectEpoch
		table.layoutKnown = state.epochKnown
		state.tables[write.tableID] = table
	}
	// A raw write can resize/repack a table, so prior field facts are not
	// layout-stable across it.  Retain only the fact produced by this write;
	// subsequent reads of unrelated fields must re-prove their value kind.
	for key := range state.fields {
		if key.tableID == write.tableID {
			delete(state.fields, key)
		}
	}
	state.fields[genericFieldKey{tableID: write.tableID, keyConstant: write.keyConstant}] = value
}

func genericFieldSetTableGuard(state *genericFieldDataflowState, register, pc int) bool {
	if state == nil {
		return false
	}
	value, ok := genericFieldRegisterFact(*state, register)
	if !ok || !value.tableKnown {
		return false
	}
	table, ok := state.tables[value.tableID]
	if !ok {
		return false
	}
	table.guardPC = pc
	table.guardKnown = true
	table.noMetatable = true
	state.tables[value.tableID] = table
	return true
}

func genericFieldTransfer(state genericFieldDataflowState, ins instruction, pc int, edge int, constants []Value) genericFieldDataflowState {
	next := state.copy()

	// The metatable test has an asymmetric transfer: its fallthrough edge is
	// the proven no-metatable path; its jump edge is an explicit side exit.
	if ins.op == opJumpIfTableHasMetatable {
		if edge == pc+1 {
			if !genericFieldSetTableGuard(&next, ins.a, pc) {
				next.invalidateAll()
			}
		} else {
			value, ok := genericFieldRegisterFact(next, ins.a)
			if ok && value.tableKnown {
				next.invalidateTable(value.tableID)
			} else {
				next.invalidateAll()
			}
		}
		return next
	}

	if ins.op == opNewTable {
		next.clearInstructionWrites(ins)
		if ins.a < 0 || ins.a >= len(next.registers) {
			next.invalidateAll()
			return next
		}
		identity := pc
		next.registers[ins.a] = genericFieldValueFact{kind: TableKind, kindKnown: true, tableID: identity, tableKnown: true, constant: -1}
		next.tables[identity] = genericFieldTableFact{
			guardPC:      pc,
			guardKnown:   true,
			noMetatable:  true,
			allocationPC: pc,
			layoutEpoch:  next.effectEpoch,
			layoutKnown:  next.epochKnown,
		}
		return next
	}

	// Raw, guarded field operations are safe enough for this analysis to keep
	// the surrounding facts.  The regular opcode effect table intentionally
	// treats these operations as callback-capable because an unguarded table can
	// invoke metamethods; the planner must prove the guard before overriding
	// that conservative classification.
	if genericFieldDirectTableAccess(ins, next, constants) {
		switch ins.op {
		case opGetStringField, opGetIndex:
			next.clearInstructionWrites(ins)
			readTable, key, destination, ok := genericFieldTableKeyForRead(ins, next, constants)
			if !ok {
				next.clearInstructionWrites(ins)
				return next
			}
			tableValue, ok := genericFieldRegisterFact(next, readTable)
			if !ok {
				next.clearInstructionWrites(ins)
				return next
			}
			if value, found := next.fieldFact(tableValue.tableID, key); found {
				next.registers[destination] = value
			} else {
				next.registers[destination] = unknownGenericFieldValueFact()
			}
			return next
		case opSetStringField, opSetField, opSetIndex:
			write, ok := genericFieldWriteAt(ins, next, constants)
			if !ok {
				next.clearInstructionWrites(ins)
				return next
			}
			value, valueOK := genericFieldRegisterFact(next, write.valueRegister)
			if !valueOK {
				value = unknownGenericFieldValueFact()
			}
			// A table stored in another table escapes the nonescaping proof even
			// when the receiver itself remains local.
			next.escapeTableValue(value)
			next.clearInstructionWrites(ins)
			genericFieldWriteState(&next, write, value)
			return next
		}
	}

	if genericFieldArithmeticIsSafe(ins, next, constants) {
		next.clearInstructionWrites(ins)
		if ins.a >= 0 && ins.a < len(next.registers) {
			next.registers[ins.a] = genericFieldValueFact{kind: NumberKind, kindKnown: true, constant: -1}
		}
		return next
	}

	// Simple value movement and constants do not touch the heap.  Keep exact
	// constant identity through moves so dynamic string-key forms can be
	// matched with their constant-field counterparts.
	switch ins.op {
	case opLoadConst:
		next.clearInstructionWrites(ins)
		if ins.a >= 0 && ins.a < len(next.registers) {
			value, ok := genericFieldConstant(constants, ins.b)
			if ok {
				kind := valueKind(value)
				fact := genericFieldValueFact{kind: kind, kindKnown: kind != UserDataKind && kind != FunctionKind && kind != HostFuncKind, constant: ins.b, constantKnown: true, tableID: -1}
				if kind == TableKind {
					fact.kindKnown = true
					fact.tableKnown = false
				}
				next.registers[ins.a] = fact
			}
		}
		return next
	case opMove:
		next.clearInstructionWrites(ins)
		if ins.a >= 0 && ins.a < len(next.registers) && ins.b >= 0 && ins.b < len(next.registers) {
			next.registers[ins.a] = next.registers[ins.b]
		}
		return next
	case opJump, opJumpIfFalse, opNumericForLoop:
		return next
	}

	// Any effect not proven pure above invalidates all heap identity, guard,
	// field, and register facts.  This includes calls, yields, global/upvalue
	// access, unknown heap effects, captures, and metamethod-capable operations.
	effect := opcodeEffect(ins.op)
	if !effect.classified || effect.invokesScriptOrHostCode || effect.mayYield ||
		effect.readsGlobals || effect.writesGlobals || effect.readsUpvalues ||
		effect.writesUpvalues || effect.readsUnknownHeap || effect.writesUnknownHeap ||
		effect.readsTables || effect.writesTables || effect.mayError ||
		effect.allocatesOrObservesIdentity {
		next.invalidateAll()
		return next
	}
	// A classified effect with no known pure transfer is still conservatively
	// invalidated; keeping this fallback explicit makes future opcode additions
	// fail closed until the planner teaches them a safe transfer.
	next.invalidateAll()
	return next
}

func genericFieldCFG(code []instruction) ([][]int, [][]int) {
	successors := make([][]int, len(code))
	predecessors := make([][]int, len(code))
	for pc := range code {
		for _, successor := range instructionSuccessors(code, pc) {
			if successor < 0 || successor >= len(code) {
				continue
			}
			successors[pc] = append(successors[pc], successor)
			predecessors[successor] = append(predecessors[successor], pc)
		}
	}
	return successors, predecessors
}

func genericFieldRegionHasSingleEntry(predecessors [][]int, start, end int) bool {
	if start < 0 || end <= start || end > len(predecessors) {
		return false
	}
	for pc := start + 1; pc < end; pc++ {
		for _, predecessor := range predecessors[pc] {
			if predecessor < start || predecessor >= end {
				return false
			}
		}
	}
	return true
}

func genericFieldAddUnique(values []int, value int) []int {
	if value < 0 {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func genericFieldGuardPCs(state genericFieldDataflowState, tableID int) (guardPCs, sideExitPCs []int) {
	table, ok := state.tables[tableID]
	if !ok || !table.guardKnown {
		return nil, nil
	}
	guardPCs = genericFieldAddUnique(guardPCs, table.guardPC)
	return guardPCs, sideExitPCs
}

func genericFieldGuardSideExit(code []instruction, guardPC int) (int, bool) {
	if guardPC < 0 || guardPC >= len(code) || code[guardPC].op != opJumpIfTableHasMetatable {
		return 0, false
	}
	target, ok := instructionJumpTarget(code[guardPC])
	return target, ok && target >= 0 && target < len(code)
}

func genericFieldArithmeticPlan(code []instruction, constants []Value, states []genericFieldDataflowState, predecessors [][]int, pc int) (genericFieldBlockPlan, bool) {
	if pc < 0 || pc+2 >= len(code) || pc >= len(states) {
		return genericFieldBlockPlan{}, false
	}
	read, ok := genericFieldReadAt(code[pc], states[pc], constants)
	if !ok {
		return genericFieldBlockPlan{}, false
	}
	stateAfterRead := genericFieldTransfer(states[pc], code[pc], pc, pc+1, constants)
	modify := code[pc+1]
	result, ok := genericFieldArithmeticFact(modify, stateAfterRead, constants, read)
	if !ok {
		return genericFieldBlockPlan{}, false
	}
	stateAfterModify := genericFieldTransfer(stateAfterRead, modify, pc+1, pc+2, constants)
	write, ok := genericFieldWriteAt(code[pc+2], stateAfterModify, constants)
	if !ok || write.tableID != read.tableID || write.keyConstant != read.keyConstant || write.valueRegister != result {
		return genericFieldBlockPlan{}, false
	}
	if !genericFieldRegionHasSingleEntry(predecessors, pc, pc+3) {
		return genericFieldBlockPlan{}, false
	}
	if len(instructionSuccessors(code, pc)) != 1 || len(instructionSuccessors(code, pc+1)) != 1 {
		return genericFieldBlockPlan{}, false
	}
	if instructionSuccessors(code, pc)[0] != pc+1 || instructionSuccessors(code, pc+1)[0] != pc+2 {
		return genericFieldBlockPlan{}, false
	}
	if !states[pc].epochKnown {
		return genericFieldBlockPlan{}, false
	}
	guardPCs, sideExitPCs := genericFieldGuardPCs(states[pc], read.tableID)
	if len(guardPCs) == 0 {
		return genericFieldBlockPlan{}, false
	}
	tableFact, tableOK := states[pc].tables[read.tableID]
	if !tableOK || !tableFact.layoutKnown {
		return genericFieldBlockPlan{}, false
	}
	for _, guardPC := range guardPCs {
		if sideExit, ok := genericFieldGuardSideExit(code, guardPC); ok {
			sideExitPCs = genericFieldAddUnique(sideExitPCs, sideExit)
		}
	}
	return genericFieldBlockPlan{
		kind:             genericFieldReadModifyWrite,
		canonicalStartPC: pc,
		canonicalEndPC:   pc + 3,
		readPC:           pc,
		modifyPC:         pc + 1,
		writePC:          pc + 2,
		tableRegister:    read.tableRegister,
		keyConstant:      read.keyConstant,
		valueRegister:    result,
		tableIdentity:    read.tableID,
		valueKind:        NumberKind,
		effectEpoch:      states[pc].effectEpoch,
		tableGuardSource: tableFact.guardPC,
		tableLayoutEpoch: tableFact.layoutEpoch,
		guardPCs:         guardPCs,
		sideExitPCs:      sideExitPCs,
	}, true
}

// analyzeGenericFieldReadModifyWriteBlocks runs a private must-fact worklist
// over the existing instruction CFG.  Unknown joins and effects deliberately
// collapse facts; no annotation produced here is consulted for runtime
// correctness yet.
func analyzeGenericFieldReadModifyWriteBlocks(code []instruction, constants []Value, registers int) []genericFieldBlockPlan {
	if len(code) == 0 || registers <= 0 {
		return nil
	}
	successors, predecessors := genericFieldCFG(code)
	states := make([]genericFieldDataflowState, len(code))
	seen := make([]bool, len(code))
	states[0] = newGenericFieldDataflowState(registers)
	seen[0] = true
	worklist := []int{0}
	for len(worklist) > 0 {
		pc := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		for _, successor := range successors[pc] {
			next := genericFieldTransfer(states[pc], code[pc], pc, successor, constants)
			if !seen[successor] {
				states[successor] = next
				seen[successor] = true
				worklist = append(worklist, successor)
				continue
			}
			joined := joinGenericFieldStates(states[successor], next)
			if equalGenericFieldState(states[successor], joined) {
				continue
			}
			states[successor] = joined
			worklist = append(worklist, successor)
		}
	}

	plans := make([]genericFieldBlockPlan, 0)
	for pc := range code {
		if !seen[pc] {
			continue
		}
		if plan, ok := genericFieldArithmeticPlan(code, constants, states, predecessors, pc); ok {
			plans = append(plans, plan)
		}
	}
	return plans
}

// Short aliases keep the analysis seam readable at call sites and make it
// explicit that the result is transient rather than a Proto side table.
func analyzeFieldReadModifyWriteBlocks(code []instruction, constants []Value, registers int) []genericFieldBlockPlan {
	return analyzeGenericFieldReadModifyWriteBlocks(code, constants, registers)
}

func analyzeGenericFieldBlocks(code []instruction, constants []Value, registers int) []genericFieldBlockPlan {
	return analyzeGenericFieldReadModifyWriteBlocks(code, constants, registers)
}
