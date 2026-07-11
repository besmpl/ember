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
	source, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "vm.go"))
	if err != nil {
		t.Fatalf("read vm.go: %v", err)
	}
	if strings.Contains(string(source), ".unpack()") {
		t.Fatal("vm execution still calls packedInstruction.unpack; decode packed operands in place")
	}

	text := string(source)
	assertDirectLoopUsesLocalPC(t, text, "func runDirectFrameInstrumentedLoop", "func runDirectFrameProductionLoop")
	assertDirectLoopUsesLocalPC(t, text, "func runDirectFrameProductionLoop", "func (thread *vmThread) runColdInstructionLoop")
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
