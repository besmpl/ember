package ember

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSlice23CompileRunStableSymbolScopes(t *testing.T) {
	source := `
local outer = 10
local function makeCounter(seed)
	local value = seed
	local function bump()
		value = value + 1
		return value
	end
	return bump
end
local counter = makeCounter(outer)
local first = counter()
local second = counter()
local sum = 0
for i = 1, 3 do
	do
		local outer = i
		sum = sum + outer
	end
end
local object = {value = 4}
function object:add(amount)
	self.value = self.value + amount
	return self.value
end
local methodResult = object:add(3)
local rawlen = function()
	return 99
end
return first, second, sum, methodResult, rawlen()
`
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := []float64{11, 12, 6, 7, 99}
	if len(results) != len(want) {
		t.Fatalf("Run returned %d results, want %d", len(results), len(want))
	}
	for i, expected := range want {
		got, ok := results[i].Number()
		if !ok || got != expected {
			t.Fatalf("result %d = %v (%t), want %v", i, results[i], ok, expected)
		}
	}
}

func TestSlice23CompileRejectsMissingBindingFact(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return missing"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	term, ok := expressionSingleTerm(artifact.tree, artifact.tree.returnValues(artifact.tree.statements()[0].ret)[0])
	if !ok {
		t.Fatal("return expression is not a single term")
	}
	node := artifact.tree.termSyntaxID(term)
	if node <= 0 {
		t.Fatalf("return term has invalid syntax id %d", node)
	}
	artifact.bind.nodeFacts[node].use = int32(boundUseUnvisited)
	artifact.bind.nodeFacts[node].flags &^= boundNodeUseValid
	_, err = compileProgram(artifact)
	if err == nil {
		t.Fatal("compileProgram succeeded with an unvisited binding fact")
	}
	if !strings.Contains(err.Error(), "missing binding fact") {
		t.Fatalf("compileProgram error is %q, want missing binding fact", err)
	}
}

func TestSlice23CompileRejectsStaleBindingIDWithoutValidFlag(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return missing"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	term, ok := expressionSingleTerm(artifact.tree, artifact.tree.returnValues(artifact.tree.statements()[0].ret)[0])
	if !ok {
		t.Fatal("return expression is not a single term")
	}
	node := artifact.tree.termSyntaxID(term)
	artifact.bind.nodeFacts[node].use = 0
	artifact.bind.nodeFacts[node].flags &^= boundNodeUseValid
	_, err = compileProgram(artifact)
	if err == nil {
		t.Fatal("compileProgram succeeded with stale binding id")
	}
	if !strings.Contains(err.Error(), "missing binding fact") {
		t.Fatalf("compileProgram error is %q, want missing binding fact", err)
	}
}

func TestSlice23CompileRejectsCorruptDefinitionSymbol(t *testing.T) {
	artifact, err := parseSource(Source{Text: "local value = 1\nreturn value"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	if len(artifact.bind.symbols) == 0 {
		t.Fatal("parseSource returned no symbols")
	}
	artifact.bind.symbols[0].id = len(artifact.bind.symbols) + 1
	_, err = compileProgram(artifact)
	if err == nil {
		t.Fatal("compileProgram succeeded with corrupt definition symbol")
	}
	if !strings.Contains(err.Error(), "invalid binding symbol") {
		t.Fatalf("compileProgram error is %q, want invalid binding symbol", err)
	}
}

func TestSlice23EmitterHasNoNameMapSnapshots(t *testing.T) {
	typeOfCompiler := reflect.TypeOf(compiler{})
	for i := 0; i < typeOfCompiler.NumField(); i++ {
		field := typeOfCompiler.Field(i)
		if field.Type.Kind() == reflect.Map && field.Type.Key().Kind() == reflect.String {
			t.Fatalf("compiler retains map field %q of type %s", field.Name, field.Type)
		}
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "emitter.go"))
	if err != nil {
		t.Fatalf("read emitter.go: %v", err)
	}
	text := string(source)
	for _, forbidden := range []string{
		"locals             map[string]int",
		"upvalues           map[string]int",
		"copyLocals(",
		"resolveVariable(",
		"resolveUpvalue(",
		"addUpvalue(",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("emitter.go still contains obsolete name-map mechanism %q", forbidden)
		}
	}
}

func TestSlice23CompilerUsesBoundGlobalClassification(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return hostValue"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	term, ok := expressionSingleTerm(artifact.tree, artifact.tree.returnValues(artifact.tree.statements()[0].ret)[0])
	if !ok {
		t.Fatal("return expression is not a single term")
	}
	node := artifact.tree.termSyntaxID(term)
	if got := artifact.bind.useClassification(node); got != boundUseGlobal {
		t.Fatalf("host term classification = %d, want global %d", got, boundUseGlobal)
	}
	proto, err := compileProgram(artifact)
	if err != nil {
		t.Fatalf("compileProgram returned error for valid global: %v", err)
	}
	results, err := RunWithGlobals(proto, map[string]Value{"hostValue": NumberValue(42)})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 42 {
		t.Fatalf("global result = %v (%t), want 42", results[0], ok)
	}
}

func TestSlice23MissingBindingErrorIncludesNode(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return missing"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	term, ok := expressionSingleTerm(artifact.tree, artifact.tree.returnValues(artifact.tree.statements()[0].ret)[0])
	if !ok {
		t.Fatal("return expression is not a single term")
	}
	node := artifact.tree.termSyntaxID(term)
	artifact.bind.nodeFacts[node].use = int32(boundUseUnvisited)
	artifact.bind.nodeFacts[node].flags &^= boundNodeUseValid
	_, err = compileProgram(artifact)
	if err == nil {
		t.Fatal("compileProgram succeeded with missing binding")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("node %d", node)) {
		t.Fatalf("compileProgram error is %q, want node id %d", err, node)
	}
}
