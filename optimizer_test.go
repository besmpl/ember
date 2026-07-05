package ember

import (
	"fmt"
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

func TestBytecodePeepholeSkipsControlFlow(t *testing.T) {
	artifact := parseSourceForOptimizationTest(t, `
local value = 1
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
	if !disassemblyHasInstruction(disassembly, "JUMP_IF_FALSE") || !disassemblyHasInstruction(disassembly, "JUMP") {
		t.Fatalf("control-flow bytecode should keep branch structure until jump targets can be rewritten: %#v", disassembly)
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
