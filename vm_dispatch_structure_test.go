package ember

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVMExecutionDoesNotMaterializePackedInstructions(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(testFile)
	source, err := os.ReadFile(filepath.Join(root, "vm.go"))
	if err != nil {
		t.Fatalf("read vm.go: %v", err)
	}
	text := string(source)
	if strings.Contains(text, ".unpack()") {
		t.Fatal("vm execution still calls packedInstruction.unpack; decode packed operands in place")
	}
	if strings.Contains(text, "runColdInstructionLoop") {
		t.Fatal("vm.go still carries the legacy full cold instruction loop")
	}
	assertDirectLoopUsesLocalPC(t, text, "func runDirectFrameInstrumentedLoop", "func runDirectFrameProductionLoop")
	assertDirectLoopUsesLocalPC(t, text, "func runDirectFrameProductionLoop", "func (thread *vmThread) consumeInstruction")

	coldSource, err := os.ReadFile(filepath.Join(root, "vm_cold.go"))
	if err != nil {
		t.Fatalf("read vm_cold.go: %v", err)
	}
	coldText := string(coldSource)
	if strings.Contains(coldText, "for frame.pc") {
		t.Fatal("cold side-exit helper must execute one instruction, not own the frame loop")
	}
	for _, opcode := range []string{
		"case opSetField:",
		"case opGetIndex:",
		"case opArrayNext, opArrayNextJump2:",
		"case opNumericForLoop:",
		"case opFastCall:",
		"case opCall:",
		"case opCallMethodOne:",
	} {
		if !strings.Contains(coldText, opcode) {
			t.Fatalf("cold side-exit helper is missing %s", opcode)
		}
	}
}

func assertDirectLoopUsesLocalPC(t *testing.T, source, startMarker, endMarker string) {
	t.Helper()
	start := strings.Index(source, startMarker)
	if start < 0 {
		t.Fatalf("vm.go is missing %q", startMarker)
	}
	endOffset := strings.Index(source[start:], endMarker)
	if endOffset < 0 {
		t.Fatalf("vm.go is missing %q after %q", endMarker, startMarker)
	}
	loop := source[start : start+endOffset]
	for _, forbidden := range []string{"for frame.pc <", "code[frame.pc]", "frame.pc++"} {
		if strings.Contains(loop, forbidden) {
			t.Fatalf("%s still dispatches through frame state %q; keep pc local", startMarker, forbidden)
		}
	}
}
