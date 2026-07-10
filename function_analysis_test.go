package ember

import "testing"

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
