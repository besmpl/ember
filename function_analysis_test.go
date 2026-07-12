package ember

import (
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

func TestMarkBorrowableFixedCallWindowsEncodesEligibleCallShapes(t *testing.T) {
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
	fixedMulti := []instruction{
		{op: opCall, a: 0, b: 2, c: 0, d: 2},
		{op: opReturn, a: 0, b: 2},
	}
	markedMulti := markBorrowableFixedCallWindows(fixedMulti, 4, nil)
	if got, borrow := decodeFixedMultiResultCount(markedMulti[0].d, 4); got != 2 || !borrow {
		t.Fatalf("fixed-multi CALL result count = (%d, %t), want (2, true)", got, borrow)
	}
	normalized := normalizeFixedMultiResultCounts(markedMulti, 4)
	if normalized[0].d != 2 {
		t.Fatalf("normalized fixed-multi result count = %d, want 2", normalized[0].d)
	}
	remarked := markBorrowableFixedCallWindows(normalized, 4, nil)
	if remarked[0].d != markedMulti[0].d {
		t.Fatalf("re-finalized marker = %d, want stable %d", remarked[0].d, markedMulti[0].d)
	}
	capturedDestination := analyzeFixedCallBorrowFacts(fixedMulti, 4, []bool{false, true})
	if len(capturedDestination) != 1 || capturedDestination[0].eligible || capturedDestination[0].reason != "result destination is captured" {
		t.Fatalf("captured multi-result fact = %#v, want rejection", capturedDestination)
	}
}
