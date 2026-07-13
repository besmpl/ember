package ember

import (
	"reflect"
	"testing"
)

func TestControlFlowSimplificationFoldsThreadsAndCompactsOnce(t *testing.T) {
	ir := []bytecodeIRInstruction{
		lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: 0, b: 0}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opJumpIfFalse, a: 0, b: 5}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opJump, b: 4}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: 1, b: 1}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opJump, b: 6}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: 1, b: 2}, sourceRange{}),
		lowerInstructionToBytecodeIR(instruction{op: opReturnOne, a: 0}, sourceRange{}),
	}
	facts := bytecodeIROptimizationFacts{
		constants: []Value{BoolValue(true), NumberValue(1), NumberValue(2)},
	}

	got := materializeBytecodeIR(simplifyBytecodeIRControlFlow(ir, facts))
	want := []instruction{
		{op: opLoadConst, a: 0, b: 0},
		{op: opReturnOne, a: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("simplified code is %#v, want %#v", got, want)
	}
}

func TestControlFlowSimplificationAllocationBudget(t *testing.T) {
	const jumps = 256
	ir := make([]bytecodeIRInstruction, 0, jumps+1)
	for pc := 0; pc < jumps; pc++ {
		ir = append(ir, lowerInstructionToBytecodeIR(instruction{op: opJump, b: pc + 1}, sourceRange{}))
	}
	ir = append(ir, lowerInstructionToBytecodeIR(instruction{op: opReturn}, sourceRange{}))

	allocs := testing.AllocsPerRun(25, func() {
		optimized := simplifyBytecodeIRControlFlow(ir, bytecodeIROptimizationFacts{})
		if len(optimized) != 1 || optimized[0].opcodeValue() != opReturn {
			t.Fatalf("simplified %d-jump chain to %#v, want one RETURN", jumps, optimized)
		}
	})
	if allocs > 20 {
		t.Fatalf("control-flow simplification used %.0f allocs/op, want at most 20", allocs)
	}
}
