package ember

import (
	"strings"
	"testing"
)

func TestCompileRunConditionTemporaries(t *testing.T) {
	source := conditionTemporarySource(256)
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		t.Fatalf("decodeProtoInstructions returned error: %v", err)
	}
	if got := len(code); got != 1282 {
		t.Fatalf("instruction count = %d, want 1282", got)
	}
	if got := len(proto.constants); got != 4 {
		t.Fatalf("constant count = %d, want 4", got)
	}
	if got := proto.registers; got > 2 {
		t.Fatalf("frame register count = %d, want at most 2", got)
	}
	if len(proto.lines) != len(code) {
		t.Fatalf("line table length = %d, want %d", len(proto.lines), len(code))
	}
	lineCount := strings.Count(source, "\n")
	for index, line := range proto.lines {
		if line != -1 && (line < 1 || line > lineCount) {
			t.Fatalf("line table entry %d = %d, want -1 or source line 1..%d", index, line, lineCount)
		}
	}

	results, err := RunWithGlobals(proto, map[string]Value{"flag": BoolValue(false)})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 512 {
		t.Fatalf("RunWithGlobals result = %v (%t), want 512", results[0], ok)
	}
}

func TestCompileRunNestedConditionTemporaries(t *testing.T) {
	source := `local total = 0
local i = 0
while i < 2 do
    if flag then
        total = total + 1
    else
        total = total + 2
    end
    repeat
        i = i + 1
    until i >= 2
end
return total`
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := RunWithGlobals(proto, map[string]Value{"flag": BoolValue(false)})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 2 {
		t.Fatalf("RunWithGlobals result = %v (%t), want 2", results[0], ok)
	}
	if got := proto.registers; got > 4 {
		t.Fatalf("nested condition frame registers = %d, want at most 4", got)
	}
}

func TestCompileRunConditionTemporariesPreserveCallFrames(t *testing.T) {
	source := `local base = 4
local function add(...)
    local first, second = ...
    if first > 0 then
        return base + first + second
    end
    return base
end
return add(2, 3)`
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 9 {
		t.Fatalf("Run result = %v (%t), want 9", results[0], ok)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("root child prototype count = %d, want 1", len(proto.prototypes))
	}
	if proto.prototypes[0].params != 0 || !proto.prototypes[0].variadic {
		t.Fatalf("child call frame params=%d variadic=%t, want variadic zero-parameter frame", proto.prototypes[0].params, proto.prototypes[0].variadic)
	}
}

func TestCompileTempExpressionReleasesOnError(t *testing.T) {
	c := compiler{}
	if _, err := c.compileTempExpression(expressionID(0)); err == nil {
		t.Fatal("compileTempExpression succeeded for an empty expression")
	}
	if c.nextReg != 1 {
		t.Fatalf("next register = %d, want 1 after failed temporary expression", c.nextReg)
	}
	if len(c.freeTemps) != 1 || c.freeTemps[0] != 0 {
		t.Fatalf("free temporaries = %#v, want [0] after failed temporary expression", c.freeTemps)
	}
	if register := c.allocTemp(); register != 0 {
		t.Fatalf("reused temporary register = %d, want 0", register)
	}
}

func conditionTemporarySource(branches int) string {
	var source strings.Builder
	source.WriteString("local value = 0\n")
	for branch := 0; branch < branches; branch++ {
		source.WriteString("if flag then\nvalue = value + 1\nelse\nvalue = value + 2\nend\n")
	}
	source.WriteString("return value\n")
	return source.String()
}
