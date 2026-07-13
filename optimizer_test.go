package ember

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestScalarConstantPropagationFoldsAcrossAliasesAndBranches(t *testing.T) {
	proto, err := Compile(`
local function calculate(input)
	local value = nil
	local enabled = nil
	if input then
		value = 40
		enabled = true
	else
		value = 40
		enabled = true
	end
	local alias = value
	if enabled then
		return alias + 2
	end
	return 0
end
return calculate(false)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1: %#v", len(results), results)
	}
	if number, ok := results[0].Number(); !ok || number != 42 {
		t.Fatalf("Run result is %#v, want number 42", results[0])
	}

	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled program has %d child prototypes, want 1", len(proto.prototypes))
	}
	disassembly := disassembleProto(proto.prototypes[0])
	if disassemblyHasAnyInstruction(disassembly, "ADD", "ADD_K") {
		t.Fatalf("constant arithmetic survived scalar propagation: %#v", disassembly)
	}
	branches := 0
	for _, line := range disassembly {
		if strings.Contains(line, "JUMP_IF_FALSE") {
			branches++
		}
	}
	if branches != 1 {
		t.Fatalf("compiled bytecode has %d conditional branches, want only the unknown input branch: %#v", branches, disassembly)
	}
}

func TestScalarConstantPropagationTracksNilBoolAndStringJoins(t *testing.T) {
	proto, err := Compile(`
local function render(input)
	local text = ""
	local absent = true
	local disabled = true
	if input then
		text = "ember"
		absent = nil
		disabled = false
	else
		text = "ember"
		absent = nil
		disabled = false
	end
	if absent then
		return "bad nil"
	end
	if disabled then
		return "bad bool"
	end
	return text .. "!"
end
return render(false)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1: %#v", len(results), results)
	}
	if text, ok := results[0].String(); !ok || text != "ember!" {
		t.Fatalf("Run result is %#v, want string ember!", results[0])
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled program has %d child prototypes, want 1", len(proto.prototypes))
	}
	disassembly := disassembleProto(proto.prototypes[0])
	if disassemblyHasInstruction(disassembly, "CONCAT") {
		t.Fatalf("constant string concat survived scalar propagation: %#v", disassembly)
	}
	branches := 0
	for _, line := range disassembly {
		if strings.Contains(line, "JUMP_IF_FALSE") {
			branches++
		}
	}
	if branches != 1 {
		t.Fatalf("compiled bytecode has %d conditional branches, want only the unknown input branch: %#v", branches, disassembly)
	}
}

func TestScalarConstantPropagationPreservesNumericEdgeSemantics(t *testing.T) {
	proto, err := Compile(`
local left = -7
local right = 3
local zero = -0.0
return left % right, left // right, zero * 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3: %#v", len(results), results)
	}
	for index, want := range []float64{2, -3} {
		if got, ok := results[index].Number(); !ok || got != want {
			t.Fatalf("result %d is %#v, want number %v", index, results[index], want)
		}
	}
	zero, ok := results[2].Number()
	if !ok || zero != 0 || !math.Signbit(zero) {
		t.Fatalf("result 2 is %#v, want negative zero", results[2])
	}
	if disassemblyHasAnyInstruction(disassembleProto(proto), "MOD", "IDIV", "MUL") {
		t.Fatalf("constant numeric bytecode was not folded: %#v", disassembleProto(proto))
	}
}

func TestScalarConstantPropagationInvalidatesCapturedLocalsAcrossCalls(t *testing.T) {
	proto, err := Compile(`
local value = 1
local function mutate()
	value = 2
end
mutate()
return value + 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1: %#v", len(results), results)
	}
	if number, ok := results[0].Number(); !ok || number != 3 {
		t.Fatalf("Run result is %#v, want number 3", results[0])
	}
	if !disassemblyHasAnyInstruction(disassembleProto(proto), "ADD", "ADD_K") {
		t.Fatalf("captured local arithmetic was unsafely folded across a call: %#v", disassembleProto(proto))
	}
}

func TestScalarConstantPropagationExcludesTablesAndFunctions(t *testing.T) {
	proto, err := Compile(`
local object = {}
local function callback()
	return 1
end
return object == object, callback == callback
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2: %#v", len(results), results)
	}
	for index, result := range results {
		if value, ok := result.Bool(); !ok || !value {
			t.Fatalf("result %d is %#v, want true", index, result)
		}
	}
	if !disassemblyHasInstruction(disassembleProto(proto), "EQUAL") {
		t.Fatalf("table/function equality was unsafely replaced by a scalar constant: %#v", disassembleProto(proto))
	}
}

func TestScalarConstantPropagationDoesNotFoldNaNOrdering(t *testing.T) {
	proto, err := Compile(`
local zero = 0
local nan = zero / zero
local alias = nan
return alias < 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want NaN comparison error")
	}
	if !strings.Contains(err.Error(), "NaN") {
		t.Fatalf("Run error is %q, want NaN detail", err)
	}
}

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
				optimizationHIRSimplify:      true,
				optimizationBytecodePeephole: true,
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
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
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
	wordcodeBytes := int64(reflect.TypeOf(wordcodeWord(0)).Size())
	if got := metrics.WordcodeBytes / wordcodeBytes; got > 8 {
		t.Fatalf("compiled arithmetic has %d wordcode words, want at most 8", got)
	}

	// The green baseline measured 132 allocations for this fixed source. Keep
	// only a small deterministic headroom so this gate cannot drift back to the
	// historical 520-allocation ceiling.
	const measuredArithmeticCompileAllocs = 132
	const maxAllocsPerCompile = measuredArithmeticCompileAllocs * 105 / 100
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

func TestOptimizerCompilerRunParityAgainstDisabledPipeline(t *testing.T) {
	for _, source := range []string{
		`return 1 + 2 * 3`,
		`local total = 0
for i = 1, 3 do
 total = total + i
end
return total`,
		`local function add(a, b)
 return a + b
end
return add(2, 3)`,
		`local value = false
if value then
 return 1
end
return 2`,
	} {
		t.Run(strings.ReplaceAll(source, "\n", "/"), func(t *testing.T) {
			optimized, err := Compile(source)
			if err != nil {
				t.Fatalf("optimized Compile returned error: %v", err)
			}
			artifact := parseSourceForOptimizationTest(t, source)
			disabled, err := compileProgramWithOptions(artifact, compilerOptions{
				optimizations: optimizationOptions{disableAll: true},
			})
			if err != nil {
				t.Fatalf("disabled Compile returned error: %v", err)
			}
			optimizedDisassembly := disassembleProto(optimized)
			disabledDisassembly := disassembleProto(disabled)
			if len(optimizedDisassembly) == 0 || len(disabledDisassembly) == 0 {
				t.Fatalf("missing disassembly: optimized=%#v disabled=%#v", optimizedDisassembly, disabledDisassembly)
			}
			optimizedValues, optimizedErr := Run(optimized)
			disabledValues, disabledErr := Run(disabled)
			if !equalTestErrors(optimizedErr, disabledErr) {
				t.Fatalf("optimized Run error is %v, disabled Run error is %v", optimizedErr, disabledErr)
			}
			if optimizedErr != nil {
				t.Fatalf("Run returned error: %v", optimizedErr)
			}
			if !reflect.DeepEqual(optimizedValues, disabledValues) {
				t.Fatalf("optimized Run values = %#v, disabled Run values = %#v\noptimized disassembly: %#v\ndisabled disassembly: %#v", optimizedValues, disabledValues, optimizedDisassembly, disabledDisassembly)
			}
		})
	}
}

func TestGatedOptimizerMatchesLegacyPipelineCorpus(t *testing.T) {
	fixtures, err := prepareCompilerStageFixtures()
	if err != nil {
		t.Fatalf("prepareCompilerStageFixtures returned error: %v", err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			optimized := optimizeCompilerStageIR(fixture.emission)
			legacy := legacyOptimizeCompilerStageIR(fixture.emission)
			if !reflect.DeepEqual(optimized.ir, legacy.ir) {
				t.Fatalf("gated IR differs from legacy pipeline:\ngated=%#v\nlegacy=%#v", optimized.ir, legacy.ir)
			}
			if !reflect.DeepEqual(optimized.constants, legacy.constants) {
				t.Fatalf("gated constants = %#v, legacy constants = %#v", optimized.constants, legacy.constants)
			}
			gatedProto, err := assembleAndSealCompilerStage(fixture.emission, optimized)
			if err != nil {
				t.Fatalf("assemble gated stage: %v", err)
			}
			legacyProto, err := assembleAndSealCompilerStage(fixture.emission, legacy)
			if err != nil {
				t.Fatalf("assemble legacy stage: %v", err)
			}
			if gatedDisassembly, legacyDisassembly := disassembleProto(gatedProto), disassembleProto(legacyProto); !reflect.DeepEqual(gatedDisassembly, legacyDisassembly) {
				t.Fatalf("gated disassembly = %#v, legacy disassembly = %#v", gatedDisassembly, legacyDisassembly)
			}
		})
	}
}

func legacyOptimizeCompilerStageIR(emission compilerStageEmission) compilerStageOptimization {
	constantPool := bytecodeBuilder{}
	constantPool.resetConstants(append([]Value(nil), emission.constants...))
	optimized := legacyOptimizeBytecodeIRWithFacts(
		cloneCompilerStageIR(emission.ir),
		bytecodeIROptimizationFacts{
			constants:         constantPool.constants,
			capturedRegisters: functionDraftCapturedRegisters(emission.children),
			constantPool:      &constantPool,
		},
		defaultCompilerOptions().optimizations,
	)
	return compilerStageOptimization{ir: optimized, constants: append([]Value(nil), constantPool.constants...)}
}

func legacyOptimizeBytecodeIRWithFacts(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts, options optimizationOptions) []bytecodeIRInstruction {
	if !options.enabled(optimizationBytecodePeephole) {
		return append([]bytecodeIRInstruction(nil), ir...)
	}
	function := newFunctionIR(append([]bytecodeIRInstruction(nil), ir...))
	function.replace(applyBytecodeIRRemovalSet(
		function.instructions,
		bytecodeIRPeepholeRemovalSet(function.instructions, assembleBytecodeIRRaw(function.instructions), function.currentAnalysis()),
	))
	function.replace(simplifyBytecodeIRControlFlow(function.instructions, bytecodeIROptimizationFacts{}))
	function.replace(propagateBytecodeIRScalarConstants(function.instructions, facts))
	function.replace(propagateBytecodeIRSingleUseMoves(function.instructions, function.currentAnalysis()))
	function.replace(coalesceBytecodeIRMoveProducers(function.instructions, facts.capturedRegisters, function.currentAnalysis()))
	function.replace(hoistBytecodeIRLoopInvariantHeaderLoads(function.instructions))
	function.replace(applyBytecodeIRRemovalSet(
		function.instructions,
		bytecodeIRDeadCodeRemovalSet(function.instructions, facts, function.currentAnalysis()),
	))
	function.replace(simplifyBytecodeIRControlFlow(function.instructions, bytecodeIROptimizationFacts{}))
	if facts.constantPool != nil {
		constants := facts.scalarConstants()
		compactedIR, compactedConstants := compactBytecodeIRConstants(function.instructions, constants)
		function.replace(compactedIR)
		if len(compactedConstants) != len(constants) {
			facts.constantPool.resetConstants(compactedConstants)
		}
	}
	return function.instructions
}

func TestInstructionSuccessorsIncludeGeneralModuloBranch(t *testing.T) {
	code := []instruction{
		{op: opModK, a: 0, b: 0, c: 1},
		{op: opJumpIfNotEqualK, a: 0, b: 1, d: 3},
		{op: opReturnOne},
	}

	if got, want := instructionSuccessors(code, 1), []int{2, 3}; !equalIntSlices(got, want) {
		t.Fatalf("general modulo branch successors are %#v, want %#v", got, want)
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
