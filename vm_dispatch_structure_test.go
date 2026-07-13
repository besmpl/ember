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
	if strings.Contains(text, "packedCode") || strings.Contains(text, "packedInstruction") {
		t.Fatal("vm.go still executes through the legacy packed instruction stream")
	}
	for _, marker := range []string{"raw := words[pc]", "wordcodeAuxBit", "wordcodeDecodeAD", "nextWord := pc + 1"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("direct loops must fetch and decode wordcode directly; missing %q", marker)
		}
	}
	assertDirectLoopUsesLocalPC(t, text, "func runDirectFrameInstrumentedLoop", "func runDirectFrameProductionLoop")
	assertDirectLoopUsesLocalPC(t, text, "func runDirectFrameProductionLoop", "func (thread *vmThread) runDebugCountHook")
	productionStart := strings.Index(text, "func runDirectFrameProductionLoop")
	if productionStart < 0 {
		t.Fatal("vm.go is missing the production direct loop boundaries")
	}
	productionEnd := strings.Index(text[productionStart:], "func (thread *vmThread) runDebugCountHook")
	if productionEnd < 0 {
		t.Fatal("vm.go is missing the production direct loop end boundary")
	}
	production := text[productionStart : productionStart+productionEnd]
	for _, forbidden := range []string{
		"opcodeMetadata(",
		"wordcodeDecodeByte(",
		"wordcodeDecodeAD(",
		"wordcodeDecodeE(",
		"decodeWordcodeDispatch(",
		"unknown opcode byte",
		"unexpected AUX",
		"is missing AUX",
		"AUX is truncated",
		"unsupported format",
		"unsupported AUX mode",
	} {
		if strings.Contains(production, forbidden) {
			t.Fatalf("production direct loop still uses generic decoder %q", forbidden)
		}
	}
	for _, marker := range []string{
		"a := int(uint8(raw >> 8))",
		"b := int(uint8(raw >> 16))",
		"c := int(uint8(raw >> 24))",
	} {
		if !strings.Contains(production, marker) {
			t.Fatalf("production direct loop must decode primary operands in place; missing %q", marker)
		}
	}
	if strings.Contains(production, "wordcodeEncodingTable") {
		t.Fatal("production direct loop still consults wordcode encoding metadata per instruction")
	}

	coldSource, err := os.ReadFile(filepath.Join(root, "vm_cold.go"))
	if err != nil {
		t.Fatalf("read vm_cold.go: %v", err)
	}
	coldText := string(coldSource)
	if strings.Contains(coldText, "packedCode") || strings.Contains(coldText, "packedInstruction") {
		t.Fatal("vm_cold.go still executes through the legacy packed instruction stream")
	}
	if !strings.Contains(coldText, "decodeWordcodeDispatch(proto.words, frame.pc, proto.cacheIndex)") {
		t.Fatal("cold side-exit helper must fetch and decode wordcode directly")
	}
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

func TestDirectFrameRunsPhysicalWordcodeAcrossAuxAndRelativeBranch(t *testing.T) {
	proto, err := Compile(`
local i = 0
while i < 200 do
	i = i + 1
end
return i
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	decoded, boundaries, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		t.Fatalf("wordcode decode failed: %v", err)
	}
	if len(proto.words) <= len(decoded) {
		t.Fatalf("wordcode has %d words for %d logical instructions, want at least one AUX word", len(proto.words), len(decoded))
	}
	if len(boundaries) != len(decoded)+1 {
		t.Fatalf("wordcode boundaries = (%d decoded, %d boundaries), want %d boundaries", len(decoded), len(boundaries), len(decoded)+1)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want one", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 200 {
		t.Fatalf("Run result = %v (%t), want number 200", got, ok)
	}

	instrumented := newVMThread(runtimeGlobals(nil))
	instrumented.directFrameInstrumented = true
	instrumentedResults, err := instrumented.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("instrumented run returned error: %v", err)
	}
	if len(instrumentedResults) != 1 {
		t.Fatalf("instrumented run returned %d results, want one", len(instrumentedResults))
	}
	instrumentedValue, ok := instrumentedResults[0].Number()
	if !ok || instrumentedValue != 200 {
		t.Fatalf("instrumented run result = %v (%t), want number 200", instrumentedValue, ok)
	}
}

func TestDirectFrameDefersRuntimeFunctionInstanceLookupUntilCacheUse(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "vm.go"))
	if err != nil {
		t.Fatalf("read vm.go: %v", err)
	}
	text := string(source)
	for _, bounds := range [][2]string{
		{"func runDirectFrameInstrumentedLoop", "func runDirectFrameProductionLoop"},
		{"func runDirectFrameProductionLoop", "func (thread *vmThread) runDebugCountHook"},
	} {
		start := strings.Index(text, bounds[0])
		end := strings.Index(text, bounds[1])
		if start < 0 || end <= start {
			t.Fatalf("vm.go is missing direct-loop bounds %q..%q", bounds[0], bounds[1])
		}
		loop := text[start:end]
		reload := strings.Index(loop, "reload:")
		if reload < 0 {
			t.Fatalf("%s is missing reload marker", bounds[0])
		}
		dispatch := strings.Index(loop[reload:], "for uint(pc) < uint(len(words))")
		if dispatch < 0 {
			t.Fatalf("%s is missing reload/dispatch markers", bounds[0])
		}
		setup := loop[reload : reload+dispatch]
		if strings.Contains(setup, "thread.functionInstance(proto)") {
			t.Fatalf("%s eagerly looks up runtime function state on every frame reload", bounds[0])
		}
		if !strings.Contains(setup, "functionInstance = nil") {
			t.Fatalf("%s does not reset its lazy runtime function state", bounds[0])
		}
		if got, want := strings.Count(loop, "functionInstance = thread.functionInstance(proto)"), 6; got != want {
			t.Fatalf("%s has %d lazy runtime function lookups, want %d cache paths", bounds[0], got, want)
		}
		if got, want := strings.Count(loop, "cache := functionInstance.cacheAt(cacheID)"), 6; got != want {
			t.Fatalf("%s has %d runtime cache reads, want %d cache paths", bounds[0], got, want)
		}
		dispatchLoop := loop[reload+dispatch:]
		opSwitch := strings.Index(dispatchLoop, "switch op {")
		if opSwitch < 0 {
			t.Fatalf("%s is missing opcode switch", bounds[0])
		}
		if strings.Contains(dispatchLoop[:opSwitch], "cacheSiteAt(pc)") {
			t.Fatalf("%s resolves cache metadata before opcode dispatch", bounds[0])
		}
		if got, want := strings.Count(loop, "cacheSiteAt(pc)"), 6; got != want {
			t.Fatalf("%s has %d cache metadata lookups, want %d cache opcode handlers", bounds[0], got, want)
		}
	}
}

func TestColdInstructionErrorKeepsCurrentPhysicalPC(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("field")},
		[]instruction{{op: opSetField, a: 0, b: 0, c: 1}},
		nil, nil, 2, 0, false,
	)
	if proto.verifyErr != nil {
		t.Fatalf("newProto returned invalid prototype: %v", proto.verifyErr)
	}
	frame := &vmFrame{
		proto:     proto,
		registers: []Value{NumberValue(1), NumberValue(2)},
	}
	thread := newVMThread(runtimeGlobals(nil))
	action := thread.runColdInstruction(frame)
	if action.kind != coldInstructionActionError {
		t.Fatalf("cold action kind = %d, want error", action.kind)
	}
	if frame.pc != 0 {
		t.Fatalf("cold error pc = %d, want current physical pc 0", frame.pc)
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
