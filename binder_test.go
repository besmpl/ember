package ember

import (
	"strings"
	"testing"
)

func TestBindProgramRecordsLexicalSymbolsAndScopes(t *testing.T) {
	prog := parseSourceForBindTest(t, `
local value = 1
do
	local value = 2
	type Pair<T, U...> = {left: T}
	local function inner(param: number)
		return value + param
	end
end
return value
`)

	result := bindProgram(prog)

	rootValue := result.mustSymbol(t, "value", symbolLocal, 0)
	blockValue := result.mustSymbol(t, "value", symbolLocal, 1)
	if blockValue.shadowed != rootValue.id {
		t.Fatalf("block value shadows symbol %d, want root value %d", blockValue.shadowed, rootValue.id)
	}

	result.mustSymbol(t, "Pair", symbolTypeAlias, 1)
	result.mustSymbol(t, "T", symbolTypeParameter, 2)
	result.mustSymbol(t, "U", symbolTypePack, 2)
	result.mustSymbol(t, "inner", symbolLocalFunction, 1)
	result.mustSymbol(t, "param", symbolParameter, 3)
}

func TestBindProgramDoesNotShadowWithClosedScopeSymbols(t *testing.T) {
	prog := parseSourceForBindTest(t, `
do
	local closed = 1
end
local closed = 2
return closed
`)

	result := bindProgram(prog)
	outer := result.mustSymbol(t, "closed", symbolLocal, 0)
	if outer.shadowed != -1 {
		t.Fatalf("outer closed shadows symbol %d from a closed scope, want -1", outer.shadowed)
	}
}

func TestParseSourceReturnsProgramAndBindResult(t *testing.T) {
	artifact, err := parseSource(Source{Text: `
local value = 1
return value
`})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	if len(artifact.program.statements) == 0 {
		t.Fatal("parseSource returned no statements")
	}
	artifact.bind.mustSymbol(t, "value", symbolLocal, 0)
}

func TestBindResultFindsSymbolsByScopeAndKind(t *testing.T) {
	prog := parseSourceForBindTest(t, `
local value = 1
do
	local value = 2
end
return value
`)

	result := bindProgram(prog)
	symbol, ok := result.findSymbol(1, "value", symbolLocal)
	if !ok {
		t.Fatalf("findSymbol did not find block local; symbols: %#v", result.symbols)
	}
	if symbol.scope != 1 || symbol.name != "value" || symbol.kind != symbolLocal {
		t.Fatalf("findSymbol returned %#v, want block value local", symbol)
	}
}

func TestBindProgramResolvesUsesAndCapturesUpvalues(t *testing.T) {
	source := `
local outer = 1
local function add(inner)
	return outer + inner
end
return add(2)
`
	prog := parseSourceForBindTest(t, source)

	result := bindProgram(prog)
	outer := result.mustSymbol(t, "outer", symbolLocal, 0)
	inner := result.mustSymbol(t, "inner", symbolParameter, 1)

	outerUse := result.mustUse(t, "outer", outer.id, true)
	result.mustUse(t, "inner", inner.id, false)
	result.mustCapture(t, outer.id, 1)

	resolved, ok := result.useAt(outerUse.start, outerUse.end)
	if !ok {
		t.Fatalf("useAt(%d, %d) did not find outer use", outerUse.start, outerUse.end)
	}
	if resolved.symbol != outer.id {
		t.Fatalf("useAt resolved symbol %d, want outer %d", resolved.symbol, outer.id)
	}
	if got := source[outerUse.start:outerUse.end]; got != "outer" {
		t.Fatalf("outer use range contains %q, want outer", got)
	}
}

func TestBindProgramRecordsAssignmentTargetUseRange(t *testing.T) {
	source := `
local value = 1
value = value + 1
return value
`
	prog := parseSourceForBindTest(t, source)
	result := bindProgram(prog)
	value := result.mustSymbol(t, "value", symbolLocal, 0)

	targetStart := strings.Index(source, "value = value")
	if targetStart < 0 {
		t.Fatalf("test source missing assignment target")
	}
	targetUse, ok := result.useAt(targetStart, targetStart+len("value"))
	if !ok {
		t.Fatalf("useAt did not find assignment target at %d", targetStart)
	}
	if targetUse.symbol != value.id {
		t.Fatalf("assignment target resolved symbol %d, want %d", targetUse.symbol, value.id)
	}
}

func TestBindProgramResolvesTypeAnnotationUses(t *testing.T) {
	source := `
type Alias = number
type Box<T> = {value: T, other: Alias}
local value: Alias = 1
local function convert(param: Alias): Alias
	return param
end
return convert(value)
`
	prog := parseSourceForBindTest(t, source)
	result := bindProgram(prog)

	alias := result.mustSymbol(t, "Alias", symbolTypeAlias, 0)
	typeParam := result.mustSymbol(t, "T", symbolTypeParameter, 2)

	result.mustUseAtText(t, source, "value: T", "T", typeParam.id)
	result.mustUseAtText(t, source, "other: Alias", "Alias", alias.id)
	result.mustUseAtText(t, source, "value: Alias", "Alias", alias.id)
	result.mustUseAtText(t, source, "param: Alias", "Alias", alias.id)
	result.mustUseAtText(t, source, "): Alias", "Alias", alias.id)
}

func parseSourceForBindTest(t *testing.T, source string) program {
	t.Helper()
	p := parser{source: source}
	prog, err := p.parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	return prog
}

func (r bindResult) mustUse(t *testing.T, name string, symbolID int, captured bool) boundUse {
	t.Helper()
	for _, use := range r.uses {
		if use.name == name && use.symbol == symbolID && use.captured == captured {
			return use
		}
	}
	t.Fatalf("missing use %q -> %d captured=%t; uses: %#v", name, symbolID, captured, r.uses)
	return boundUse{}
}

func (r bindResult) mustUseAtText(t *testing.T, source string, context string, name string, symbolID int) boundUse {
	t.Helper()
	contextStart := strings.Index(source, context)
	if contextStart < 0 {
		t.Fatalf("test source missing context %q", context)
	}
	nameStart := strings.Index(context, name)
	if nameStart < 0 {
		t.Fatalf("context %q missing name %q", context, name)
	}
	start := contextStart + nameStart
	use, ok := r.useAt(start, start+len(name))
	if !ok {
		t.Fatalf("missing use for %q at [%d,%d); uses: %#v", name, start, start+len(name), r.uses)
	}
	if use.symbol != symbolID {
		t.Fatalf("use %q at [%d,%d) resolved symbol %d, want %d", name, start, start+len(name), use.symbol, symbolID)
	}
	return use
}

func (r bindResult) mustCapture(t *testing.T, symbolID int, scope int) boundCapture {
	t.Helper()
	for _, capture := range r.captures {
		if capture.symbol == symbolID && capture.scope == scope {
			return capture
		}
	}
	t.Fatalf("missing capture symbol %d in scope %d; captures: %#v", symbolID, scope, r.captures)
	return boundCapture{}
}

func (r bindResult) mustSymbol(t *testing.T, name string, kind symbolKind, scope int) boundSymbol {
	t.Helper()
	for _, symbol := range r.symbols {
		if symbol.name == name && symbol.kind == kind && symbol.scope == scope {
			return symbol
		}
	}
	t.Fatalf("missing %s symbol %q in scope %d; symbols: %#v", kind, name, scope, r.symbols)
	return boundSymbol{}
}
