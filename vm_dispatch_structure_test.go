package ember

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestApprovedExecutionLoopsRemainExplicit(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(testFile)
	var loops []string
	for _, name := range []string{"vm_dispatch_generated.go", "runtime_machine_generated.go"} {
		file, err := goparser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			isDispatchLoop := false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				loop, ok := node.(*ast.ForStmt)
				if !ok {
					return true
				}
				ast.Inspect(loop.Body, func(node ast.Node) bool {
					switchStatement, ok := node.(*ast.SwitchStmt)
					if !ok {
						return true
					}
					identifier, identOK := switchStatement.Tag.(*ast.Ident)
					selector, selectorOK := switchStatement.Tag.(*ast.SelectorExpr)
					if identOK && identifier.Name == "op" || selectorOK && selector.Sel.Name == "op" {
						isDispatchLoop = true
					}
					return !isDispatchLoop
				})
				return !isDispatchLoop
			})
			if isDispatchLoop {
				loops = append(loops, function.Name.Name)
			}
		}
	}
	sort.Strings(loops)
	want := []string{"runGeneratedDirectFrameInstrumentedLoop", "runGeneratedDirectFrameProductionLoop", "runGeneratedScalarMachineLoop"}
	sort.Strings(want)
	if !reflect.DeepEqual(loops, want) {
		t.Fatalf("instruction dispatch loops = %v, want approved set %v", loops, want)
	}
}

func TestGeneratedDispatchMatchesSemanticSource(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(testFile)
	spec, err := os.ReadFile(filepath.Join(root, "vm_dispatch_spec.go"))
	if err != nil {
		t.Fatalf("read dispatch spec: %v", err)
	}
	generated, err := os.ReadFile(filepath.Join(root, "vm_dispatch_generated.go"))
	if err != nil {
		t.Fatalf("read generated dispatch: %v", err)
	}
	template, err := os.ReadFile(filepath.Join(root, "vm_dispatch_template.go.tmpl"))
	if err != nil {
		t.Fatalf("read dispatch template: %v", err)
	}
	template = bytes.TrimPrefix(template, []byte("package ember\n\n"))
	hashInput := append(append([]byte{}, spec...), template...)
	want := fmt.Sprintf("dispatch spec sha256: %x", sha256.Sum256(hashInput))
	if !strings.Contains(string(generated), want) {
		t.Fatalf("generated dispatch is stale: missing %q", want)
	}
	for _, name := range []string{"func runGeneratedDirectFrameProductionLoop", "func runGeneratedDirectFrameInstrumentedLoop"} {
		if !strings.Contains(string(generated), name) {
			t.Fatalf("generated dispatch is missing %s", name)
		}
	}
	machineTemplate, err := os.ReadFile(filepath.Join(root, "runtime_machine_template.go.tmpl"))
	if err != nil {
		t.Fatalf("read Machine template: %v", err)
	}
	machineGenerated, err := os.ReadFile(filepath.Join(root, "runtime_machine_generated.go"))
	if err != nil {
		t.Fatalf("read generated Machine: %v", err)
	}
	metadata, err := os.ReadFile(filepath.Join(root, "bytecode.go"))
	if err != nil {
		t.Fatalf("read opcode metadata: %v", err)
	}
	machineHashInput := append(append([]byte{}, metadata...), machineTemplate...)
	machineStamp := fmt.Sprintf("Machine metadata sha256: %x", sha256.Sum256(machineHashInput))
	if !strings.Contains(string(machineGenerated), machineStamp) {
		t.Fatalf("generated Machine is stale: missing %q", machineStamp)
	}
	if !strings.Contains(string(machineGenerated), "func runGeneratedScalarMachineLoop") {
		t.Fatal("generated Machine dispatch is missing runGeneratedScalarMachineLoop")
	}
	check := exec.Command("go", "run", "./cmd/ember-vmgen", "-check")
	check.Dir = root
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("generated dispatch check failed: %v\n%s", err, output)
	}
}

func TestGeneratedDirectSemanticCatalogCoversOpcodeMetadata(t *testing.T) {
	if directAdaptiveHandlerCount > directAdaptiveHandlerCap {
		t.Fatalf("adaptive handler count = %d, cap = %d", directAdaptiveHandlerCount, directAdaptiveHandlerCap)
	}
	seenSpecialized := make(map[directHandlerID]opcode)
	for _, op := range allOpcodes {
		semantic, ok := directSemanticMetadataFor(op)
		if !ok {
			t.Fatalf("missing generated semantic metadata for %s", opcodeName(op))
		}
		base, _ := opcodeMetadata(op)
		if semantic.source != op || semantic.genericHandler != directHandlerID(op) {
			t.Fatalf("%s source/handler = %d/%d", opcodeName(op), semantic.source, semantic.genericHandler)
		}
		if semantic.guestCharge != base.machine.guestCharge || semantic.errorClass != base.machine.errorClass {
			t.Fatalf("%s charge/error metadata drifted", opcodeName(op))
		}
		if semantic.effects != base.effects || semantic.wordcode != base.wordcode {
			t.Fatalf("%s effect/encoding metadata drifted", opcodeName(op))
		}
		if semantic.family == directSpecializationNone {
			if semantic.specializedHandler != directHandlerInvalid {
				t.Fatalf("%s has no family but handler %d", opcodeName(op), semantic.specializedHandler)
			}
			continue
		}
		if semantic.specializedHandler < directHandlerID(opcodeLimit) {
			t.Fatalf("%s specialized handler %d overlaps generic opcode space", opcodeName(op), semantic.specializedHandler)
		}
		if previous, duplicate := seenSpecialized[semantic.specializedHandler]; duplicate {
			t.Fatalf("%s and %s share specialized handler %d", opcodeName(previous), opcodeName(op), semantic.specializedHandler)
		}
		seenSpecialized[semantic.specializedHandler] = op
	}
	if len(seenSpecialized) != directAdaptiveHandlerCount {
		t.Fatalf("specialized handlers = %d, declared %d", len(seenSpecialized), directAdaptiveHandlerCount)
	}
	if _, ok := directSemanticMetadataFor(opcodeLimit); ok {
		t.Fatal("semantic metadata accepted opcodeLimit")
	}
}

func TestGeneratedDirectSemanticCatalogRejectsMalformedMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*[opcodeLimit]directSemanticMetadata)
		want   string
	}{
		{
			name: "missing entry",
			mutate: func(table *[opcodeLimit]directSemanticMetadata) {
				table[opAdd] = directSemanticMetadata{}
			},
			want: "is unclassified",
		},
		{
			name: "base metadata drift",
			mutate: func(table *[opcodeLimit]directSemanticMetadata) {
				table[opAdd].guestCharge++
			},
			want: "charge metadata drifted",
		},
		{
			name: "generic handler drift",
			mutate: func(table *[opcodeLimit]directSemanticMetadata) {
				table[opAdd].genericHandler = directHandlerID(opSub)
			},
			want: "generic handler",
		},
		{
			name: "duplicate specialized handler",
			mutate: func(table *[opcodeLimit]directSemanticMetadata) {
				table[opSub].specializedHandler = table[opAdd].specializedHandler
			},
			want: "duplicate specialized handler",
		},
		{
			name: "pure control transfer",
			mutate: func(table *[opcodeLimit]directSemanticMetadata) {
				table[opJump].tiling = directTilingPure
			},
			want: "pure tiling transfers control",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table := generatedDirectSemanticMetadata
			test.mutate(&table)
			err := validateDirectSemanticMetadata(table)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateDirectSemanticMetadata error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestVMExecutionDoesNotMaterializePackedInstructions(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(testFile)
	source, err := os.ReadFile(filepath.Join(root, "vm_dispatch_generated.go"))
	if err != nil {
		t.Fatalf("read vm.go: %v", err)
	}
	text := string(source)
	if strings.Contains(text, ".unpack()") {
		t.Fatal("vm execution still calls packedInstruction.unpack; decode packed operands in place")
	}
	if strings.Contains(text, "runColdInstructionLoop") {
		t.Fatal("generated dispatch still carries the legacy full cold instruction loop")
	}
	if strings.Contains(text, "packedCode") || strings.Contains(text, "packedInstruction") {
		t.Fatal("vm.go still executes through the legacy packed instruction stream")
	}
	for _, marker := range []string{"raw := words[pc]", "wordcodeAuxBit", "nextWord := pc + 1"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("direct loops must fetch and decode wordcode directly; missing %q", marker)
		}
	}
	assertDirectLoopUsesLocalPC(t, text, "func runGeneratedDirectFrameProductionLoop", "func runGeneratedDirectFrameInstrumentedLoop")
	productionStart := strings.Index(text, "func runGeneratedDirectFrameProductionLoop")
	if productionStart < 0 {
		t.Fatal("vm.go is missing the production direct loop boundaries")
	}
	productionEnd := strings.Index(text[productionStart+len("func runGeneratedDirectFrameProductionLoop"):], "\nfunc ")
	if productionEnd < 0 {
		productionEnd = len(text) - productionStart
	} else {
		productionEnd += len("func runGeneratedDirectFrameProductionLoop")
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

func TestDirectFrameFunctionInstanceLookupMatchesGeneratedPolicy(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "vm_dispatch_generated.go"))
	if err != nil {
		t.Fatalf("read vm.go: %v", err)
	}
	text := string(source)
	for _, name := range []string{"runGeneratedDirectFrameProductionLoop", "runGeneratedDirectFrameInstrumentedLoop"} {
		production := name == "runGeneratedDirectFrameProductionLoop"
		start := strings.Index(text, "func "+name)
		if start < 0 {
			t.Fatalf("generated dispatch is missing %s", name)
		}
		end := strings.Index(text[start+len("func "+name):], "\nfunc ")
		if end < 0 {
			end = len(text)
		} else {
			end += start + len("func "+name)
		}
		loop := text[start:end]
		reload := strings.Index(loop, "reload:")
		if reload < 0 {
			t.Fatalf("%s is missing reload marker", name)
		}
		dispatch := strings.Index(loop[reload:], "for uint(pc) < uint(len(words))")
		if dispatch < 0 {
			t.Fatalf("%s is missing reload/dispatch markers", name)
		}
		setup := loop[reload : reload+dispatch]
		if !strings.Contains(setup, "functionInstance = nil") {
			t.Fatalf("%s does not reset its lazy runtime function state", name)
		}
		if production {
			if got := strings.Count(setup, "functionInstance, shadowErr = thread.shadowFunctionInstance(proto)"); got != 1 {
				t.Fatalf("%s has %d owner-shadow lookups, want one per reload", name, got)
			}
			if strings.Contains(loop, "functionInstance = thread.functionInstance(proto)") {
				t.Fatalf("%s retained cache-local function-instance lookups after shadow binding", name)
			}
		} else {
			if strings.Contains(loop, "shadowFunctionInstance") || strings.Contains(loop, "shadowWords") {
				t.Fatalf("%s retained production shadow policy", name)
			}
			if strings.Contains(setup, "thread.functionInstance(proto)") {
				t.Fatalf("%s eagerly looks up runtime function state on every frame reload", name)
			}
			if got, want := strings.Count(loop, "functionInstance = thread.functionInstance(proto)"), 6; got != want {
				t.Fatalf("%s has %d lazy runtime function lookups, want %d cache paths", name, got, want)
			}
		}
		if got, want := strings.Count(loop, "cache := functionInstance.cacheAt(cacheID)"), 6; got != want {
			t.Fatalf("%s has %d runtime cache reads, want %d cache paths", name, got, want)
		}
		dispatchLoop := loop[reload+dispatch:]
		opSwitch := strings.Index(dispatchLoop, "switch op {")
		if opSwitch < 0 {
			t.Fatalf("%s is missing opcode switch", name)
		}
		if strings.Contains(dispatchLoop[:opSwitch], "cacheSiteAt(pc)") {
			t.Fatalf("%s resolves cache metadata before opcode dispatch", name)
		}
		if production {
			for _, marker := range []string{"shadowWord := shadowWords[pc]", "raw = shadowWord.raw()", "op = opcode(shadowWord.handler())"} {
				if !strings.Contains(dispatchLoop[:opSwitch], marker) {
					t.Fatalf("%s fetch is missing %q", name, marker)
				}
			}
		}
		if got, want := strings.Count(loop, "cacheSiteAt(pc)"), 6; got != want {
			t.Fatalf("%s has %d cache metadata lookups, want %d cache opcode handlers", name, got, want)
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
	endOffset := strings.Index(source[start+len(startMarker):], "\nfunc ")
	if endOffset < 0 {
		endOffset = len(source) - start
	} else {
		endOffset += len(startMarker)
	}
	loop := source[start : start+endOffset]
	for _, forbidden := range []string{"for frame.pc <", "code[frame.pc]", "frame.pc++"} {
		if strings.Contains(loop, forbidden) {
			t.Fatalf("%s still dispatches through frame state %q; keep pc local", startMarker, forbidden)
		}
	}
}
