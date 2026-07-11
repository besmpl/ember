package ember

import (
	"math"
	"testing"
)

func TestFunctionIRCachesAnalysisUntilInstructionsChange(t *testing.T) {
	ir := []bytecodeIRInstruction{
		lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: 0, b: 0}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opReturnOne, a: 0}, sourceRange{}),
	}
	function := newFunctionIR(ir)
	first := function.currentAnalysis()
	if first == nil {
		t.Fatal("currentAnalysis returned nil")
	}
	if got := function.currentAnalysis(); got != first {
		t.Fatal("unchanged function rebuilt analysis")
	}

	same := append([]bytecodeIRInstruction(nil), ir...)
	function.replace(same)
	if function.revision != 0 {
		t.Fatalf("identical replacement advanced revision to %d, want 0", function.revision)
	}
	if got := function.currentAnalysis(); got != first {
		t.Fatal("identical replacement rebuilt analysis")
	}

	changed := append([]bytecodeIRInstruction(nil), ir...)
	changed[0].operands.a.value = 1
	function.replace(changed)
	if function.revision != 1 {
		t.Fatalf("changed replacement advanced revision to %d, want 1", function.revision)
	}
	second := function.currentAnalysis()
	if second == first {
		t.Fatal("changed function reused stale analysis")
	}
	if second.revision != function.revision {
		t.Fatalf("analysis revision is %d, want %d", second.revision, function.revision)
	}
}

func TestFunctionAnalysisOwnsCFGDataflowAndEffects(t *testing.T) {
	ir := []bytecodeIRInstruction{
		lowerInstructionToBytecodeIR(instruction{op: opJumpIfFalse, a: 0, b: 2}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opReturnOne, a: 1}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: 1, b: 0}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opReturnOne, a: 1}, sourceRange{}),
	}
	function := newFunctionIR(ir)
	analysis := function.currentAnalysis()

	if len(analysis.blocks) != 3 || len(analysis.successors) != 3 || len(analysis.predecessors) != 3 {
		t.Fatalf("analysis CFG sizes are blocks=%d successors=%d predecessors=%d, want 3 each", len(analysis.blocks), len(analysis.successors), len(analysis.predecessors))
	}
	if len(analysis.reachable) != 3 || !analysis.reachable[0] || !analysis.reachable[1] || !analysis.reachable[2] {
		t.Fatalf("analysis reachability is %#v, want all three blocks reachable", analysis.reachable)
	}
	if len(analysis.use) != 3 || len(analysis.def) != 3 || len(analysis.liveness) != 3 {
		t.Fatalf("analysis dataflow sizes are use=%d def=%d liveness=%d, want 3 each", len(analysis.use), len(analysis.def), len(analysis.liveness))
	}
	if !analysis.use[0].contains(0) {
		t.Fatal("entry block use set does not contain branch register 0")
	}
	if len(analysis.effects) != len(ir) || analysis.effects[0] != opcodeEffect(opJumpIfFalse) || analysis.effects[2] != opcodeEffect(opLoadConst) {
		t.Fatalf("analysis effects are %#v, want per-instruction opcode effects", analysis.effects)
	}
}

func TestFunctionAnalysisComputesEphemeralLiveAfter(t *testing.T) {
	ir := []bytecodeIRInstruction{
		lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: 0, b: 0}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opMove, a: 1, b: 0}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opReturnOne, a: 1}, sourceRange{}),
	}
	analysis := newFunctionIR(ir).currentAnalysis()
	if len(analysis.liveAfter) != len(ir) {
		t.Fatalf("live-after length = %d, want %d", len(analysis.liveAfter), len(ir))
	}
	if !analysis.liveAfter[0].contains(0) {
		t.Fatal("load's result was not live after the load")
	}
	if !analysis.liveAfter[1].contains(1) || analysis.liveAfter[1].contains(0) {
		t.Fatalf("move live-after set = %#v, want only r1", analysis.liveAfter[1].values())
	}
	if len(analysis.liveAfter[2].values()) != 0 {
		t.Fatalf("return live-after set = %#v, want empty", analysis.liveAfter[2].values())
	}
}

func TestFixedCallBorrowFactsRejectUnsafeSuffixes(t *testing.T) {
	base := []instruction{
		{op: opCallLocalOne, a: 0, b: 3, c: 1, d: 2},
		{op: opReturnOne, a: 0},
	}
	facts := analyzeFixedCallBorrowFacts(base, 5, nil)
	if len(facts) != 1 || !facts[0].eligible {
		t.Fatalf("safe fixed-call fact = %#v, want eligible", facts)
	}

	liveSuffix := append([]instruction(nil), base...)
	liveSuffix[1] = instruction{op: opReturnOne, a: 4}
	if fact := analyzeFixedCallBorrowFacts(liveSuffix, 5, nil)[0]; fact.eligible || fact.reason == "" {
		t.Fatalf("live suffix fact = %#v, want rejection reason", fact)
	}

	captured := analyzeFixedCallBorrowFacts(base, 5, []bool{false, true, false, false, false})[0]
	if captured.eligible || captured.reason == "" {
		t.Fatalf("captured suffix fact = %#v, want rejection reason", captured)
	}

	destinationOverlap := []instruction{
		{op: opCallLocalOne, a: 3, b: 0, c: 1, d: 1},
		{op: opReturnOne, a: 3},
	}
	overlap := analyzeFixedCallBorrowFacts(destinationOverlap, 5, nil)[0]
	if overlap.eligible || overlap.reason == "" {
		t.Fatalf("destination-overlap fact = %#v, want rejection reason", overlap)
	}
}

func TestFixedCallBorrowFactsHandleMissingExtraArgsAndRecursion(t *testing.T) {
	for _, count := range []int{0, 3} {
		code := []instruction{
			{op: opCallLocalOne, a: 0, b: 3, c: 1, d: count},
			{op: opReturnOne, a: 0},
		}
		facts := analyzeFixedCallBorrowFacts(code, 5, nil)
		if len(facts) != 1 || !facts[0].eligible {
			t.Fatalf("fixed-call count %d fact = %#v, want eligible", count, facts)
		}
	}

	recursive := []instruction{
		{op: opCallLocalOne, a: 0, b: 3, c: 1, d: 2},
		{op: opJumpIfFalse, a: 0, b: 0},
		{op: opReturnOne, a: 0},
	}
	facts := analyzeFixedCallBorrowFacts(recursive, 5, nil)
	if len(facts) != 1 || facts[0].eligible || facts[0].reason == "" {
		t.Fatalf("recursive fixed-call fact = %#v, want deterministic rejection", facts)
	}
}

func TestMarkBorrowableFixedCallWindowsUsesOnlyFixedOpcodes(t *testing.T) {
	callOne := markBorrowableFixedCallWindows([]instruction{
		{op: opCallOne, a: 0, b: 3, c: 2}, {op: opReturnOne, a: 0},
	}, 6, nil)
	if got, borrow := decodeFixedCallCount(callOne[0].c); got != 2 || !borrow {
		t.Fatalf("CALL_ONE count = (%d, %t), want (2, true)", got, borrow)
	}
	local := markBorrowableFixedCallWindows([]instruction{
		{op: opCallLocalOne, a: 0, b: 3, c: 1, d: 2}, {op: opReturnOne, a: 0},
	}, 6, nil)
	if got, borrow := decodeFixedCallCount(local[0].d); got != 2 || !borrow {
		t.Fatalf("CALL_LOCAL_ONE count = (%d, %t), want (2, true)", got, borrow)
	}
	upvalue := markBorrowableFixedCallWindows([]instruction{
		{op: opCallUpvalueOne, a: 0, b: 0, c: 1, d: 2}, {op: opReturnOne, a: 0},
	}, 6, nil)
	if got, borrow := decodeFixedCallCount(upvalue[0].d); got != 2 || !borrow {
		t.Fatalf("CALL_UPVALUE_ONE count = (%d, %t), want (2, true)", got, borrow)
	}
	method := markBorrowableFixedCallWindows([]instruction{
		{op: opCallMethodOne, a: 0, b: 3, c: 0, d: 1}, {op: opReturnOne, a: 0},
	}, 6, nil)
	if got, borrow := decodeFixedCallCount(method[0].d); got != 1 || !borrow {
		t.Fatalf("CALL_METHOD_ONE count = (%d, %t), want (1, true)", got, borrow)
	}
	methodFact := analyzeFixedCallBorrowFacts([]instruction{
		{op: opCallMethodOne, a: 0, b: 3, c: 0, d: 1}, {op: opReturnOne, a: 0},
	}, 6, nil)[0]
	if methodFact.argumentStart != 1 || methodFact.argumentCount != 2 || methodFact.result != 0 {
		t.Fatalf("method borrow shape = start %d count %d result %d, want 1, 2, 0", methodFact.argumentStart, methodFact.argumentCount, methodFact.result)
	}
	generic := []instruction{
		{op: opCall, a: 0, b: 3, c: -2, d: 1}, {op: opReturnOne, a: 0},
	}
	markedGeneric := markBorrowableFixedCallWindows(generic, 6, nil)
	if markedGeneric[0].c != generic[0].c {
		t.Fatalf("generic CALL count changed from %d to %d", generic[0].c, markedGeneric[0].c)
	}
}

func genericFieldPlanTestConstants(values ...Value) []Value { return values }

func genericFieldPlanTestCode(withNaN bool) ([]instruction, []Value) {
	constants := genericFieldPlanTestConstants(StringValue("score"), NumberValue(1), NumberValue(2))
	if withNaN {
		constants[1] = NumberValue(math.NaN())
	}
	return []instruction{
		{op: opNewTable, a: 0},
		{op: opLoadConst, a: 1, b: 1},
		{op: opLoadConst, a: 3, b: 2},
		{op: opSetStringField, a: 0, b: 0, c: 1},
		{op: opGetStringField, a: 2, b: 0, c: 0},
		{op: opAdd, a: 4, b: 2, c: 3},
		{op: opSetStringField, a: 0, b: 0, c: 4},
		{op: opReturnOne, a: 4},
	}, constants
}

func TestGenericFieldReadModifyWritePlannerBuildsTransientPlan(t *testing.T) {
	code, constants := genericFieldPlanTestCode(false)
	plans := analyzeGenericFieldReadModifyWriteBlocks(code, constants, 5)
	if len(plans) != 1 {
		t.Fatalf("plans = %#v, want one field plan", plans)
	}
	plan := plans[0]
	if plan.kind != genericFieldReadModifyWrite || plan.canonicalStartPC != 4 || plan.canonicalEndPC != 7 {
		t.Fatalf("plan range = %#v, want kind/read range (4,7)", plan)
	}
	if plan.readPC != 4 || plan.modifyPC != 5 || plan.writePC != 6 || plan.tableRegister != 0 || plan.keyConstant != 0 || plan.valueRegister != 4 {
		t.Fatalf("plan shape = %#v, want read=4 modify=5 write=6 table=r0 key=k0 result=r4", plan)
	}
	if plan.tableIdentity != 0 || plan.valueKind != NumberKind || len(plan.guardPCs) != 1 || plan.guardPCs[0] != 0 {
		t.Fatalf("plan guards = %#v, want allocation guard at pc0", plan)
	}
	if len(plan.sideExitPCs) != 0 {
		t.Fatalf("unmetatable allocation has side exits %#v", plan.sideExitPCs)
	}
}

func TestGenericFieldReadModifyWritePlannerMatchesConstantDynamicKey(t *testing.T) {
	constants := genericFieldPlanTestConstants(StringValue("score"), NumberValue(1), NumberValue(2))
	code := []instruction{
		{op: opNewTable, a: 0},
		{op: opLoadConst, a: 1, b: 1},
		{op: opLoadConst, a: 3, b: 2},
		{op: opLoadConst, a: 5, b: 0},
		{op: opSetIndex, a: 0, b: 5, c: 1},
		{op: opGetIndex, a: 2, b: 0, c: 5},
		{op: opAdd, a: 4, b: 2, c: 3},
		{op: opSetIndex, a: 0, b: 5, c: 4},
		{op: opReturnOne, a: 4},
	}
	plans := analyzeGenericFieldReadModifyWriteBlocks(code, constants, 6)
	if len(plans) != 1 || plans[0].keyConstant != 0 {
		t.Fatalf("dynamic-key plans = %#v, want one key-identity plan", plans)
	}
}

func TestGenericFieldReadModifyWritePlannerTracksExplicitMetatableGuard(t *testing.T) {
	code, constants := genericFieldPlanTestCode(false)
	// The guard's jump is the side exit.  Keep the canonical region unchanged
	// on its no-metatable fallthrough path.
	code = append(code[:1], append([]instruction{{op: opJumpIfTableHasMetatable, a: 0, d: 8}}, code[1:]...)...)
	plans := analyzeGenericFieldReadModifyWriteBlocks(code, constants, 5)
	if len(plans) != 1 {
		t.Fatalf("plans = %#v, want one guarded field plan", plans)
	}
	plan := plans[0]
	if len(plan.guardPCs) != 1 || plan.guardPCs[0] != 1 {
		t.Fatalf("guard PCs = %#v, want explicit guard pc1", plan.guardPCs)
	}
	if len(plan.sideExitPCs) != 1 || plan.sideExitPCs[0] != 8 {
		t.Fatalf("side exits = %#v, want metatable side exit pc8", plan.sideExitPCs)
	}
}

func TestGenericFieldReadModifyWritePlannerSuppressesUncertainFacts(t *testing.T) {
	tests := []struct {
		name string
		code func() ([]instruction, []Value)
	}{
		{
			name: "nan",
			code: func() ([]instruction, []Value) { return genericFieldPlanTestCode(true) },
		},
		{
			name: "unknown table",
			code: func() ([]instruction, []Value) {
				code, constants := genericFieldPlanTestCode(false)
				code[0] = instruction{op: opLoadGlobal, a: 0, b: 0}
				return code, constants
			},
		},
		{
			name: "host call",
			code: func() ([]instruction, []Value) {
				code, constants := genericFieldPlanTestCode(false)
				code[4] = instruction{op: opCall, a: 4, b: 0, c: 0, d: 0}
				return code, constants
			},
		},
		{
			name: "unknown mutation",
			code: func() ([]instruction, []Value) {
				code, constants := genericFieldPlanTestCode(false)
				code[3] = instruction{op: opSetIndex, a: 0, b: 1, c: 3}
				return code, constants
			},
		},
		{
			name: "integer division error",
			code: func() ([]instruction, []Value) {
				code, constants := genericFieldPlanTestCode(false)
				constants[2] = NumberValue(0)
				code[5] = instruction{op: opIDiv, a: 4, b: 2, c: 3}
				return code, constants
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, constants := test.code()
			if plans := analyzeGenericFieldReadModifyWriteBlocks(code, constants, 5); len(plans) != 0 {
				t.Fatalf("plans = %#v, want suppression", plans)
			}
		})
	}
}

func TestGenericFieldReadModifyWritePlannerConvergesAndSuppressesJoins(t *testing.T) {
	code, constants := genericFieldPlanTestCode(false)
	// The branch reaches the field read with one path having no table facts.
	// The worklist must converge and join the disagreement to unknown.
	code = append(code, make([]instruction, 3)...)
	code[4] = instruction{op: opJumpIfFalse, a: 3, b: 6}
	code[5] = instruction{op: opJump, b: 7}
	code[6] = instruction{op: opLoadGlobal, a: 0, b: 0}
	code[7] = instruction{op: opGetStringField, a: 2, b: 0, c: 0}
	code[8] = instruction{op: opAdd, a: 4, b: 2, c: 3}
	code[9] = instruction{op: opSetStringField, a: 0, b: 0, c: 4}
	code[10] = instruction{op: opReturnOne, a: 4}
	if plans := analyzeGenericFieldReadModifyWriteBlocks(code, constants, 5); len(plans) != 0 {
		t.Fatalf("join plans = %#v, want conservative suppression", plans)
	}
}

func TestGenericFieldReadModifyWritePlannerConvergesOnLoops(t *testing.T) {
	code, constants := genericFieldPlanTestCode(false)
	code[7] = instruction{op: opJump, b: 4}
	if plans := analyzeGenericFieldReadModifyWriteBlocks(code, constants, 5); len(plans) != 0 {
		t.Fatalf("loop plans = %#v, want conservative suppression after epoch disagreement", plans)
	}
}
