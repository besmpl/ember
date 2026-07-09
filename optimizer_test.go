package ember

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestHIRSimplifyFoldsNumberArithmetic(t *testing.T) {
	proto, err := Compile("return 1 + 2 * 3")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	number, ok := results[0].Number()
	if !ok || number != 7 {
		t.Fatalf("Run result is %#v, want number 7", results[0])
	}
	disassembly := disassembleProto(proto)
	if disassemblyHasInstruction(disassembly, "ADD") || disassemblyHasInstruction(disassembly, "MUL") {
		t.Fatalf("folded bytecode still has arithmetic instructions: %#v", disassembly)
	}

	artifact := parseSourceForOptimizationTest(t, "return 1 + 2 * 3")
	disabled, err := compileProgramWithOptions(artifact, compilerOptions{
		optimizations: optimizationOptions{
			disabledCategories: map[optimizationCategory]bool{
				optimizationHIRSimplify: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("compileProgramWithOptions returned error: %v", err)
	}
	disabledDisassembly := disassembleProto(disabled)
	if !disassemblyHasAnyInstruction(disabledDisassembly, "ADD", "ADD_K") {
		t.Fatalf("HIR-simplify disabled bytecode is missing runtime arithmetic: %#v", disabledDisassembly)
	}
}

func TestHIRSimplifyLeavesNonLiteralArithmetic(t *testing.T) {
	proto, err := Compile("return 1 + input")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !disassemblyHasInstruction(disassembleProto(proto), "ADD") {
		t.Fatalf("non-literal arithmetic was folded: %#v", disassembleProto(proto))
	}
}

func TestBytecodePeepholeOptimizationRemovesSelfMove(t *testing.T) {
	artifact := parseSourceForOptimizationTest(t, `
local value = 1
value = value
return value
`)

	optimized, err := compileProgram(artifact)
	if err != nil {
		t.Fatalf("compileProgram returned error: %v", err)
	}
	if disassemblyHasAdjacentLines(disassembleProto(optimized), "MOVE r1 r0", "MOVE r0 r1") {
		t.Fatalf("optimized bytecode kept self-assignment round trip: %#v", disassembleProto(optimized))
	}

	disabled, err := compileProgramWithOptions(artifact, compilerOptions{
		optimizations: optimizationOptions{
			disabledCategories: map[optimizationCategory]bool{
				optimizationBytecodePeephole: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("compileProgramWithOptions returned error: %v", err)
	}
	if !disassemblyHasAdjacentLines(disassembleProto(disabled), "MOVE r1 r0", "MOVE r0 r1") {
		t.Fatalf("disabled bytecode is missing self-assignment round trip: %#v", disassembleProto(disabled))
	}
}

func TestOptimizationDisableAllSkipsBytecodePeephole(t *testing.T) {
	artifact := parseSourceForOptimizationTest(t, `
local value = 1
value = value
return value
`)

	proto, err := compileProgramWithOptions(artifact, compilerOptions{
		optimizations: optimizationOptions{disableAll: true},
	})
	if err != nil {
		t.Fatalf("compileProgramWithOptions returned error: %v", err)
	}
	if !disassemblyHasAdjacentLines(disassembleProto(proto), "MOVE r1 r0", "MOVE r0 r1") {
		t.Fatalf("disable-all bytecode is missing self-assignment round trip: %#v", disassembleProto(proto))
	}
}

func TestBytecodePeepholeRemovesJumpToNextAfterControlFlowRemap(t *testing.T) {
	artifact := parseSourceForOptimizationTest(t, `
local value = input
if value then
	value = value
end
return value
`)

	proto, err := compileProgram(artifact)
	if err != nil {
		t.Fatalf("compileProgram returned error: %v", err)
	}
	disassembly := disassembleProto(proto)
	if !disassemblyHasInstruction(disassembly, "JUMP_IF_FALSE") {
		t.Fatalf("control-flow bytecode should keep the conditional branch: %#v", disassembly)
	}
	if disassemblyHasInstruction(disassembly, "JUMP") {
		t.Fatalf("control-flow bytecode kept a jump-to-next instruction after remapping: %#v", disassembly)
	}
}

func TestOptimizerThreadsJumpChains(t *testing.T) {
	var builder bytecodeBuilder
	jumpElse := builder.emitJumpIfFalse(0)
	builder.emit(instruction{op: opReturnOne, a: 1})
	jumpChain := builder.emitJump()
	builder.emitLoadConst(9, NumberValue(99))
	elseStart := builder.pc()
	builder.patchJump(jumpElse, jumpChain)
	builder.patchJump(jumpChain, elseStart)
	builder.emit(instruction{op: opReturnOne, a: 2})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIRRaw(optimized)
	want := []instruction{
		{op: opJumpIfFalse, a: 0, b: 2},
		{op: opReturnOne, a: 1},
		{op: opReturnOne, a: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized raw bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizerRemovesConstantBranches(t *testing.T) {
	for _, tc := range []struct {
		name      string
		condition Value
		want      []instruction
	}{
		{
			name:      "true",
			condition: BoolValue(true),
			want: []instruction{
				{op: opLoadConst, a: 1, b: 1},
				{op: opReturnOne, a: 1},
			},
		},
		{
			name:      "false",
			condition: BoolValue(false),
			want: []instruction{
				{op: opLoadConst, a: 2, b: 2},
				{op: opReturnOne, a: 2},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var builder bytecodeBuilder
			builder.emitLoadConst(0, tc.condition)
			jumpElse := builder.emitJumpIfFalse(0)
			builder.emitLoadConst(1, NumberValue(1))
			builder.emit(instruction{op: opReturnOne, a: 1})
			elseStart := builder.pc()
			builder.patchJump(jumpElse, elseStart)
			builder.emitLoadConst(2, NumberValue(2))
			builder.emit(instruction{op: opReturnOne, a: 2})

			optimized := optimizeBytecodeIRWithConstants(builder.ir, builder.constants, optimizationOptions{})
			got := assembleBytecodeIR(optimized)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("optimized bytecode = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestCompilerFoldsConstantExpressionsWithoutChangingErrors(t *testing.T) {
	proto, err := Compile(`
return (2 + 3 * 4) % 5, "hp=" .. 10 .. "/" .. (5 + 10), #"ember", #{1, 2, 3}, 2 ^ 3, 7 // 2
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 6 {
		t.Fatalf("Run returned %d results, want 6: %#v", len(results), results)
	}
	for index, want := range []float64{4, 5, 3, 8, 3} {
		resultIndex := index
		if index > 0 {
			resultIndex = index + 1
		}
		got, ok := results[resultIndex].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %#v, want number %v", resultIndex, results[resultIndex], want)
		}
	}
	if got, ok := results[1].String(); !ok || got != "hp=10/15" {
		t.Fatalf("result 1 is %#v, want string hp=10/15", results[1])
	}
	disassembly := disassembleProto(proto)
	if disassemblyHasAnyInstruction(disassembly, "ADD", "MUL", "MOD", "IDIV", "POW", "CONCAT", "CONCAT_CHAIN", "LEN") {
		t.Fatalf("constant expression bytecode kept foldable instructions: %#v", disassembly)
	}

	assertOptimizedRunErrorMatchesDisabledHIR(t, `return "x" + 1`)
	assertOptimizedRunErrorMatchesDisabledHIR(t, `return "item=" .. {name = "ember"}`)
}

func TestCompileArithmeticCostBudget(t *testing.T) {
	const source = `
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2
`
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	metrics := CompilerBenchmarkMetricsForTest(proto)
	if metrics.Instructions > 8 {
		t.Fatalf("compiled arithmetic has %d instructions, want at most 8", metrics.Instructions)
	}
	if metrics.Constants > 3 {
		t.Fatalf("compiled arithmetic has %d constants, want at most 3", metrics.Constants)
	}
	if metrics.RegisterSlots > 3 {
		t.Fatalf("compiled arithmetic has %d register slots, want at most 3", metrics.RegisterSlots)
	}
	if metrics.ChildProtos != 0 {
		t.Fatalf("compiled arithmetic has %d child protos, want 0", metrics.ChildProtos)
	}
	packedInstructionBytes := int64(reflect.TypeOf(packedInstruction{}).Size())
	if got := metrics.PackedBytes / packedInstructionBytes; got > 8 {
		t.Fatalf("compiled arithmetic has %d packed instructions, want at most 8", got)
	}

	const maxAllocsPerCompile = 520
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := Compile(source); err != nil {
			t.Fatalf("Compile returned error: %v", err)
		}
	})
	if allocs > maxAllocsPerCompile {
		t.Fatalf("Compile used %.0f allocs/op, want at most %d", allocs, maxAllocsPerCompile)
	}
}

func assertOptimizedRunErrorMatchesDisabledHIR(t *testing.T, source string) {
	t.Helper()
	optimized, err := Compile(source)
	if err != nil {
		t.Fatalf("optimized Compile returned error: %v", err)
	}
	_, optimizedErr := Run(optimized)
	if optimizedErr == nil {
		t.Fatal("optimized Run succeeded, want error")
	}

	artifact := parseSourceForOptimizationTest(t, source)
	disabled, err := compileProgramWithOptions(artifact, compilerOptions{
		optimizations: optimizationOptions{
			disabledCategories: map[optimizationCategory]bool{
				optimizationHIRSimplify: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("disabled Compile returned error: %v", err)
	}
	_, disabledErr := Run(disabled)
	if disabledErr == nil {
		t.Fatal("disabled Run succeeded, want error")
	}
	if optimizedErr.Error() != disabledErr.Error() {
		t.Fatalf("optimized Run error is %q, want disabled error %q", optimizedErr, disabledErr)
	}
}

func TestInstructionSuccessorsIncludeSpecializedModuloBranch(t *testing.T) {
	code := []instruction{
		{op: opJumpIfModKNotEqualK, d: 2},
		{op: opReturnOne},
	}

	if got, want := instructionSuccessors(code, 0), []int{1, 2}; !equalIntSlices(got, want) {
		t.Fatalf("specialized modulo branch successors are %#v, want %#v", got, want)
	}
}

func parseSourceForOptimizationTest(t *testing.T, source string) sourceArtifact {
	t.Helper()
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	return artifact
}

func disassemblyHasLine(lines []string, suffix string) bool {
	for _, line := range lines {
		if len(line) >= len(suffix) && line[len(line)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

func disassemblyHasAdjacentLines(lines []string, first string, second string) bool {
	for i := 0; i+1 < len(lines); i++ {
		if disassemblyHasSuffix(lines[i], first) && disassemblyHasSuffix(lines[i+1], second) {
			return true
		}
	}
	return false
}

func disassemblyHasInstruction(lines []string, name string) bool {
	needle := " " + name + " "
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

func disassemblyHasAnyInstruction(lines []string, names ...string) bool {
	for _, name := range names {
		if disassemblyHasInstruction(lines, name) {
			return true
		}
	}
	return false
}

func disassemblyHasMoveRoundTrip(lines []string) bool {
	for i := 0; i+1 < len(lines); i++ {
		firstFrom, firstTo, ok := disassemblyMoveOperands(lines[i])
		if !ok {
			continue
		}
		secondFrom, secondTo, ok := disassemblyMoveOperands(lines[i+1])
		if ok && firstFrom == secondTo && firstTo == secondFrom {
			return true
		}
	}
	return false
}

func disassemblyMoveOperands(line string) (int, int, bool) {
	var pc int
	var to int
	var from int
	if _, err := fmt.Sscanf(line, "%d MOVE r%d r%d", &pc, &to, &from); err != nil {
		return 0, 0, false
	}
	return from, to, true
}

func disassemblyHasSuffix(line string, suffix string) bool {
	return len(line) >= len(suffix) && line[len(line)-len(suffix):] == suffix
}
