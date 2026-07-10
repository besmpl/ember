package ember

import "testing"

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
	symbol := result.mustSymbol(t, "value", symbolLocal, 1)
	if symbol.scope != 1 || symbol.name != "value" || symbol.kind != symbolLocal {
		t.Fatalf("symbol = %#v, want block value local", symbol)
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
	if outerUse.symbol != outer.id || !outerUse.captured {
		t.Fatalf("outer use = %#v, want captured symbol %d", outerUse, outer.id)
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

	targetID := prog.statements[1].assign.targets[0].id
	targetUse, ok := result.use(targetID)
	if !ok {
		t.Fatalf("assignment target use(%d) was not resolved", targetID)
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

	if got := result.countUses(typeParam.id); got != 1 {
		t.Fatalf("type parameter T use count = %d, want 1", got)
	}
	if got := result.countUses(alias.id); got != 4 {
		t.Fatalf("Alias use count = %d, want 4", got)
	}
}

func TestBindProgramIndexesUsesAndDefinitionsByStableSyntaxID(t *testing.T) {
	prog := parseSourceForBindTest(t, `
local value = 1
value = value + 1
return value
`)
	result := bindProgram(prog)

	definitionID := prog.statements[0].local.nameID
	symbol, ok := result.definition(definitionID)
	if !ok || symbol.name != "value" {
		t.Fatalf("definition(%d) = %#v, %t, want value symbol", definitionID, symbol, ok)
	}
	assignmentID := prog.statements[1].assign.targets[0].id
	use, ok := result.use(assignmentID)
	if !ok || use.symbol != symbol.id {
		t.Fatalf("use(%d) = %#v, %t, want symbol %d", assignmentID, use, ok, symbol.id)
	}
	returnTerm, ok := expressionSingleTerm(prog.statements[2].ret.values[0])
	if !ok {
		t.Fatal("return expression is not a single term")
	}
	if use, ok := result.use(returnTerm.id); !ok || use.symbol != symbol.id {
		t.Fatalf("use(%d) = %#v, %t, want symbol %d", returnTerm.id, use, ok, symbol.id)
	}
}

func TestBindProgramRecordsDenseCaptureAndExpressionFacts(t *testing.T) {
	prog := parseSourceForBindTest(t, `
local before = 0
before = 1
local readBefore = function() return before end

local after = 0
local readAfter = function() return after end
after = 1

return before, after, readAfter()
`)
	result := bindProgram(prog)
	before := result.mustSymbol(t, "before", symbolLocal, 0)
	after := result.mustSymbol(t, "after", symbolLocal, 0)

	beforeFacts := result.symbols[before.id].facts
	if !beforeFacts.assigned || !beforeFacts.captured || beforeFacts.mutatedAfterCapture || !beforeFacts.immutableCopyEligible {
		t.Fatalf("before facts = %#v, want assigned captured immutable copy", beforeFacts)
	}
	afterFacts := result.symbols[after.id].facts
	if !afterFacts.assigned || !afterFacts.captured || !afterFacts.mutatedAfterCapture || afterFacts.immutableCopyEligible {
		t.Fatalf("after facts = %#v, want assigned captured mutation after capture", afterFacts)
	}

	ret := prog.statements[len(prog.statements)-1].ret
	if fact, ok := result.expressionFact(ret.values[0].id); !ok || fact.multiret {
		t.Fatalf("first return expression fact = %#v, %t, want single result", fact, ok)
	}
	if fact, ok := result.expressionFact(ret.values[1].id); !ok || fact.multiret {
		t.Fatalf("second return expression fact = %#v, %t, want single result", fact, ok)
	}
	if fact, ok := result.expressionFact(ret.values[2].id); !ok || !fact.multiret || fact.arity != -1 {
		t.Fatalf("third return expression fact = %#v, %t, want open multiret", fact, ok)
	}
}

func TestParserAssignsStableFunctionIDs(t *testing.T) {
	const source = `
local function outer(value)
	return function() return value end
end
`
	first := parseSourceForBindTest(t, source)
	second := parseSourceForBindTest(t, source)
	outerFirst := first.statements[0].localFunc
	outerSecond := second.statements[0].localFunc
	innerFirst, ok := expressionSingleTerm(outerFirst.statements[0].ret.values[0])
	if !ok || innerFirst.function == nil {
		t.Fatal("inner expression is not a function")
	}
	innerSecond, ok := expressionSingleTerm(outerSecond.statements[0].ret.values[0])
	if !ok || innerSecond.function == nil {
		t.Fatal("second inner expression is not a function")
	}
	if outerFirst.functionID <= 0 || innerFirst.function.functionID <= 0 || outerFirst.functionID == innerFirst.function.functionID {
		t.Fatalf("function IDs = outer %d inner %d, want distinct positive IDs", outerFirst.functionID, innerFirst.function.functionID)
	}
	if outerFirst.functionID != outerSecond.functionID {
		t.Fatalf("outer function ID changed from %d to %d", outerFirst.functionID, outerSecond.functionID)
	}
	if innerFirst.function.functionID != innerSecond.function.functionID {
		t.Fatalf("inner function ID changed from %d to %d", innerFirst.function.functionID, innerSecond.function.functionID)
	}
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
	for _, facts := range r.nodeFacts {
		if facts.flags&boundNodeUseValid != 0 && facts.use == int32(symbolID) && (facts.flags&boundNodeCaptured != 0) == captured {
			return boundUse{symbol: symbolID, captured: captured}
		}
	}
	t.Fatalf("missing use %q -> %d captured=%t; node facts: %#v", name, symbolID, captured, r.nodeFacts)
	return boundUse{}
}

func (r bindResult) countUses(symbolID int) int {
	count := 0
	for _, facts := range r.nodeFacts {
		if facts.flags&boundNodeUseValid != 0 && facts.use == int32(symbolID) {
			count++
		}
	}
	return count
}

func (r bindResult) mustCapture(t *testing.T, symbolID int, scope int) {
	t.Helper()
	if scope >= 0 && scope < len(r.scopes) {
		for _, captured := range r.scopes[scope].capturedSymbols {
			if captured == int32(symbolID) {
				return
			}
		}
	}
	t.Fatalf("missing capture symbol %d in scope %d; scopes: %#v", symbolID, scope, r.scopes)
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
