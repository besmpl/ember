package ember

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestPhase22BoundFactsUseCompactStorage(t *testing.T) {
	if got := reflect.TypeOf(symbolKind(0)).Size(); got > 1 {
		t.Fatalf("symbolKind size = %d, want at most 1 byte", got)
	}
	if got := reflect.TypeOf(boundNodeFacts{}).Size(); got > 16 {
		t.Fatalf("boundNodeFacts size = %d, want at most 16 bytes", got)
	}
}

func TestPhase22BindingDistinguishesGlobalsFromUnvisitedNodes(t *testing.T) {
	prog := parseSourceForBindTest(t, "return hostGlobal, 1")
	tree := newSyntaxTree(prog)
	result := bindProgram(prog)

	global, ok := expressionSingleTerm(tree, prog.statements[0].ret.values[0])
	if !ok {
		t.Fatal("global expression is not a single term")
	}
	if got := result.useClassification(global.id); got != boundUseGlobal {
		t.Fatalf("global use classification = %v, want global", got)
	}
	literal, ok := expressionSingleTerm(tree, prog.statements[0].ret.values[1])
	if !ok {
		t.Fatal("literal expression is not a single term")
	}
	if got := result.useClassification(literal.id); got != boundUseUnvisited {
		t.Fatalf("literal use classification = %v, want unvisited", got)
	}
}

func TestPhase22CaptureStorageIsSparse(t *testing.T) {
	var source strings.Builder
	source.WriteString("local first = 1\n")
	for i := 0; i < 512; i++ {
		source.WriteString("local filler")
		source.WriteString(string(rune('a' + (i % 26))))
		source.WriteString(" = 1\n")
	}
	source.WriteString("local function inner() return first end\nreturn inner()\n")

	result := bindProgram(parseSourceForBindTest(t, source.String()))
	if len(result.scopes) < 2 {
		t.Fatalf("scopes = %d, want nested function scope", len(result.scopes))
	}
	if got := len(result.scopes[1].capturedSymbols); got > 2 {
		t.Fatalf("captured symbol storage = %d entries, want sparse capture list", got)
	}
}

func TestPhase22CaptureStorageDedupesLargeLists(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 10; i++ {
		source.WriteString("local value")
		source.WriteString(string(rune('a' + i)))
		source.WriteString(" = ")
		source.WriteString(string(rune('0' + i)))
		source.WriteString("\n")
	}
	source.WriteString("local function inner() return ")
	for i := 0; i < 10; i++ {
		if i != 0 {
			source.WriteString(", ")
		}
		source.WriteString("value")
		source.WriteString(string(rune('a' + i)))
	}
	source.WriteString(" end\nreturn inner()\n")

	result := bindProgram(parseSourceForBindTest(t, source.String()))
	if len(result.scopes) < 2 {
		t.Fatalf("scopes = %d, want nested function scope", len(result.scopes))
	}
	captured := result.scopes[1].capturedSymbols
	if len(captured) != 10 {
		t.Fatalf("captured symbols = %d, want ten distinct captures", len(captured))
	}
	seen := make(map[int32]struct{}, len(captured))
	for _, symbolID := range captured {
		if _, ok := seen[symbolID]; ok {
			t.Fatalf("captured symbol %d appears more than once", symbolID)
		}
		seen[symbolID] = struct{}{}
	}
}

func TestPhase22RepeatedSameNameScopesRestoreOuterBinding(t *testing.T) {
	proto, err := Compile(`
local value = 10
local function read()
	local value = 20
	do
		local value = 30
	end
	local value = 40
	return value
end
return read(), value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := []Value{NumberValue(40), NumberValue(10)}
	if len(values) != len(want) {
		t.Fatalf("values = %#v, want %#v", values, want)
	}
	for i := range want {
		if !valuesEqual(values[i], want[i]) {
			t.Fatalf("values[%d] = %#v, want %#v", i, values[i], want[i])
		}
	}
}

func TestPhase22ValueAndTypeNamespacesDoNotShadowEachOther(t *testing.T) {
	prog := parseSourceForBindTest(t, `
local T = 1
type T = string
local value: T = "typed"
return T, value
`)
	result := bindProgram(prog)
	tree := newSyntaxTree(prog)
	value := result.mustSymbol(t, "T", symbolLocal, 0)
	alias := result.mustSymbol(t, "T", symbolTypeAlias, 0)

	annotation := prog.statements[2].local.annotations[0]
	if use, ok := result.use(annotation.id); !ok || use.symbol != alias.id {
		t.Fatalf("type annotation use = %#v, %t, want type alias %d", use, ok, alias.id)
	}
	returnValue, ok := expressionSingleTerm(tree, prog.statements[3].ret.values[0])
	if !ok {
		t.Fatal("return T expression is not a single term")
	}
	if use, ok := result.use(returnValue.id); !ok || use.symbol != value.id {
		t.Fatalf("value use = %#v, %t, want local symbol %d", use, ok, value.id)
	}
}

func TestPhase22NestedValueAndTypeNamespacesRestoreIndependently(t *testing.T) {
	prog := parseSourceForBindTest(t, `
type T = string
local T = 1
do
	local T = 2
	type T = number
	local inside: T = 3
end
local after: T = "done"
return T
`)
	result := bindProgram(prog)
	tree := newSyntaxTree(prog)
	outerValue := result.mustSymbol(t, "T", symbolLocal, 0)
	outerType := result.mustSymbol(t, "T", symbolTypeAlias, 0)
	blockValue := result.mustSymbol(t, "T", symbolLocal, 2)
	blockType := result.mustSymbol(t, "T", symbolTypeAlias, 2)

	inside := prog.statements[2].block.statements[2].local.annotations[0]
	if use, ok := result.use(inside.id); !ok || use.symbol != blockType.id {
		t.Fatalf("nested type use = %#v, %t, want block type %d", use, ok, blockType.id)
	}
	after := prog.statements[3].local.annotations[0]
	if use, ok := result.use(after.id); !ok || use.symbol != outerType.id {
		t.Fatalf("restored type use = %#v, %t, want outer type %d", use, ok, outerType.id)
	}
	returnValue, ok := expressionSingleTerm(tree, prog.statements[4].ret.values[0])
	if !ok {
		t.Fatal("return T expression is not a single term")
	}
	if use, ok := result.use(returnValue.id); !ok || use.symbol != outerValue.id {
		t.Fatalf("restored value use = %#v, %t, want outer value %d", use, ok, outerValue.id)
	}
	if blockValue.shadowed != outerValue.id || blockType.shadowed != outerType.id {
		t.Fatalf("nested namespace shadow links = value:%d type:%d, want value:%d type:%d", blockValue.shadowed, blockType.shadowed, outerValue.id, outerType.id)
	}
}

func TestPhase22ScopeSymbolLinksRestoreEveryDefinition(t *testing.T) {
	prog := parseSourceForBindTest(t, `
local outer = 0
do
	local x = 1
	local y = 2
	local x = 3
end
return outer
`)
	result := bindProgram(prog)
	xSymbols := make([]boundSymbol, 0, 2)
	for _, symbol := range result.symbols {
		if symbol.name == "x" {
			xSymbols = append(xSymbols, symbol)
		}
	}
	if len(xSymbols) != 2 {
		t.Fatalf("x symbols = %#v, want two definitions", xSymbols)
	}
	scope := result.scopes[1]
	gotOrder := result.scopeSymbols[scope.symbolStart : scope.symbolStart+scope.symbolCount]
	wantOrder := []int32{int32(xSymbols[1].id), int32(xSymbols[0].id + 1), int32(xSymbols[0].id)}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("scope symbol order = %v, want reverse definitions %v", gotOrder, wantOrder)
	}
	if got := len(result.scopeSymbols); got != len(result.symbols) {
		t.Fatalf("scope symbol index length = %d, want %d", got, len(result.symbols))
	}
}

func TestPhase22NestedCaptureMutationFacts(t *testing.T) {
	prog := parseSourceForBindTest(t, `
local value = 0
local function outer()
	local function inner()
		return value
	end
	return inner
end
value = 1
return outer()
`)
	result := bindProgram(prog)
	value := result.mustSymbol(t, "value", symbolLocal, 0)
	facts := result.symbols[value.id].facts
	if !facts.assigned || !facts.captured || !facts.mutatedAfterCapture || facts.immutableCopyEligible {
		t.Fatalf("nested capture facts = %#v, want assigned captured and mutated", facts)
	}
	got := 0
	for _, scope := range result.scopes {
		got += len(scope.capturedSymbols)
	}
	if got != 1 {
		t.Fatalf("nested capture records = %d, want one unique capture", got)
	}
}

func TestPhase22BinderAllocationBudgets(t *testing.T) {
	straight := `local value = 0
value = value + 1
return value
`
	nested := `local value = 0
local function outer()
	local function inner()
		return value
	end
	return inner
end
return outer()
`
	for _, tc := range []struct {
		name      string
		source    string
		maxAllocs int
	}{
		{name: "straight", source: straight, maxAllocs: 16},
		{name: "nested", source: nested, maxAllocs: 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseSourceForBindTest(t, tc.source)
			allocs := testing.AllocsPerRun(20, func() {
				compilerStageBindSink = bindProgram(prog)
			})
			if allocs > float64(tc.maxAllocs) {
				t.Fatalf("bind allocations = %.0f, want <= %d", allocs, tc.maxAllocs)
			}
		})
	}
}

func TestPhase22BinderRetainedByteBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "straight", source: phase22RepeatedSource(192, false)},
		{name: "nested", source: phase22RepeatedSource(192, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseSourceForBindTest(t, tc.source)
			result := bindProgram(prog)
			got := phase22BindRetainedBytes(result)
			legacy := phase22LegacyBindRetainedBytes(result)
			t.Logf("node_count=%d symbols=%d scopes=%d retained=%d legacy_estimate=%d ratio=%.3f", prog.nodeCount, len(result.symbols), len(result.scopes), got, legacy, float64(got)/float64(legacy))
			if got*2 > legacy {
				t.Fatalf("bind retained bytes = %d, legacy estimate = %d, want at least 50%% reduction", got, legacy)
			}
		})
	}
}

func phase22RepeatedSource(lines int, nested bool) string {
	var source strings.Builder
	source.WriteString("local value = 0\n")
	if nested {
		source.WriteString("local function outer()\n")
		source.WriteString("local function inner()\n")
	}
	for i := 0; i < lines; i++ {
		source.WriteString("value = value + 1\n")
	}
	if nested {
		source.WriteString("return inner\nend\nend\nreturn outer()\n")
	} else {
		source.WriteString("return value\n")
	}
	return source.String()
}

func phase22BindRetainedBytes(result bindResult) int64 {
	bytes := int64(cap(result.nodeFacts)) * int64(unsafe.Sizeof(boundNodeFacts{}))
	bytes += int64(cap(result.scopes)) * int64(unsafe.Sizeof(bindScope{}))
	bytes += int64(cap(result.scopeSymbols)) * int64(unsafe.Sizeof(int32(0)))
	bytes += int64(cap(result.symbols)) * int64(unsafe.Sizeof(boundSymbol{}))
	for _, scope := range result.scopes {
		bytes += int64(cap(scope.capturedSymbols)) * int64(unsafe.Sizeof(int32(0)))
	}
	return bytes
}

func phase22LegacyBindRetainedBytes(result bindResult) int64 {
	legacyNode := int64(unsafe.Sizeof(struct {
		definition int
		use        struct {
			node     syntaxID
			name     string
			symbol   int
			scope    int
			start    int
			end      int
			captured bool
		}
		expression boundExpressionFact
	}{}))
	legacyScope := int64(unsafe.Sizeof(struct {
		id              int
		parent          int
		funcID          int
		names           map[string]int
		capturedSymbols []bool
	}{}))
	legacySymbol := int64(unsafe.Sizeof(struct {
		id       int
		node     syntaxID
		name     string
		kind     string
		scope    int
		funcID   int
		shadowed int
		facts    boundSymbolFacts
	}{}))
	bytes := int64(len(result.nodeFacts)) * legacyNode
	bytes += int64(len(result.scopes)) * legacyScope
	bytes += int64(len(result.symbols)) * legacySymbol
	return bytes
}
