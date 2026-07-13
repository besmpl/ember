package ember

import (
	"math"
	"testing"
)

func TestBindProgramRecordsLexicalSymbolsAndScopes(t *testing.T) {
	tree := parseSourceForBindTest(t, `
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

	result := bindSyntaxTree(tree)

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
	tree := parseSourceForBindTest(t, `
do
	local closed = 1
end
local closed = 2
return closed
`)

	result := bindSyntaxTree(tree)
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
	statements, ok := artifact.tree.statementIDs()
	if !ok || len(statements) == 0 {
		t.Fatal("parseSource returned no statements")
	}
	artifact.bind.mustSymbol(t, "value", symbolLocal, 0)
}

func TestBindResultFindsSymbolsByScopeAndKind(t *testing.T) {
	tree := parseSourceForBindTest(t, `
local value = 1
do
	local value = 2
end
return value
`)

	result := bindSyntaxTree(tree)
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
	tree := parseSourceForBindTest(t, source)

	result := bindSyntaxTree(tree)
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
	tree := parseSourceForBindTest(t, source)
	result := bindSyntaxTree(tree)
	value := result.mustSymbol(t, "value", symbolLocal, 0)

	statements, _ := tree.statementIDs()
	assign, ok := tree.assignmentArena(statements[1])
	if !ok {
		t.Fatal("assignment statement is not present in the statement arena")
	}
	targets, _ := tree.statementTargets(assign.targets)
	target, ok := tree.statementArenaTarget(targets[0])
	if !ok {
		t.Fatal("assignment target is not present in the statement arena")
	}
	targetID := target.id
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
	tree := parseSourceForBindTest(t, source)
	result := bindSyntaxTree(tree)

	alias := result.mustSymbol(t, "Alias", symbolTypeAlias, 0)
	typeParam := result.mustSymbol(t, "T", symbolTypeParameter, 2)

	if got := result.countUses(typeParam.id); got != 1 {
		t.Fatalf("type parameter T use count = %d, want 1", got)
	}
	if got := result.countUses(alias.id); got != 4 {
		t.Fatalf("Alias use count = %d, want 4", got)
	}
}

func TestBindProgramResolvesGenericPackTypeUse(t *testing.T) {
	tree := parseSourceForBindTest(t, "type Pack<T...> = T...")
	result := bindSyntaxTree(tree)

	pack := result.mustSymbol(t, "T", symbolTypePack, 1)
	if got := result.countUses(pack.id); got != 1 {
		t.Fatalf("generic type pack use count = %d, want one", got)
	}
}

func TestBinderFailsClosedOnMalformedArenaTypeData(t *testing.T) {
	t.Run("annotation span", func(t *testing.T) {
		tree := parseSourceForBindTest(t, "local value: number = 1")
		statements, ok := tree.statementIDs()
		if !ok || len(statements) != 1 {
			t.Fatalf("statement IDs = %v, want one local statement", statements)
		}
		node, ok := tree.statementNode(statements[0])
		if !ok || node.kind != syntaxStatementLocal || node.payload == 0 {
			t.Fatal("local statement is not present in the statement arena")
		}
		localIndex := int(node.payload) - 1
		tree.arena.statements.locals[localIndex].annotations = nodeSpan{start: math.MaxUint32, count: 1}

		result := bindSyntaxTree(tree)
		result.mustSymbol(t, "value", symbolLocal, 0)
	})

	t.Run("type ID", func(t *testing.T) {
		tree := parseSourceForBindTest(t, "type Alias = number")
		statements, ok := tree.statementIDs()
		if !ok || len(statements) != 1 {
			t.Fatalf("statement IDs = %v, want one type alias statement", statements)
		}
		node, ok := tree.statementNode(statements[0])
		if !ok || node.kind != syntaxStatementTypeAlias || node.payload == 0 {
			t.Fatal("type alias statement is not present in the statement arena")
		}
		aliasIndex := int(node.payload) - 1
		tree.arena.statements.typeAliases[aliasIndex].value = typeID(math.MaxUint32)

		result := bindSyntaxTree(tree)
		result.mustSymbol(t, "Alias", symbolTypeAlias, 0)
	})
}

func TestBindProgramIndexesUsesAndDefinitionsByStableSyntaxID(t *testing.T) {
	tree := parseSourceForBindTest(t, `
local value = 1
value = value + 1
return value
`)
	result := bindSyntaxTree(tree)

	statements, _ := tree.statementIDs()
	local, ok := tree.localArena(statements[0])
	if !ok {
		t.Fatal("local statement is not present in the statement arena")
	}
	definitionID := local.nameID
	symbol, ok := result.definition(definitionID)
	if !ok || symbol.name != "value" {
		t.Fatalf("definition(%d) = %#v, %t, want value symbol", definitionID, symbol, ok)
	}
	assign, ok := tree.assignmentArena(statements[1])
	if !ok {
		t.Fatal("assignment statement is not present in the statement arena")
	}
	targets, _ := tree.statementTargets(assign.targets)
	target, ok := tree.statementArenaTarget(targets[0])
	if !ok {
		t.Fatal("assignment target is not present in the statement arena")
	}
	assignmentID := target.id
	use, ok := result.use(assignmentID)
	if !ok || use.symbol != symbol.id {
		t.Fatalf("use(%d) = %#v, %t, want symbol %d", assignmentID, use, ok, symbol.id)
	}
	ret, ok := tree.returnArena(statements[2])
	if !ok {
		t.Fatal("return statement is not present in the statement arena")
	}
	values, _ := tree.statementExpressions(ret.values)
	returnTerm, ok := expressionSingleTerm(tree, values[0])
	if !ok {
		t.Fatal("return expression is not a single term")
	}
	returnID := tree.termSyntaxID(returnTerm)
	if use, ok := result.use(returnID); !ok || use.symbol != symbol.id {
		t.Fatalf("use(%d) = %#v, %t, want symbol %d", returnID, use, ok, symbol.id)
	}
}

func TestBindProgramRecordsDenseCaptureAndExpressionFacts(t *testing.T) {
	tree := parseSourceForBindTest(t, `
local before = 0
before = 1
local readBefore = function() return before end

local after = 0
local readAfter = function() return after end
after = 1

return before, after, readAfter()
`)
	result := bindSyntaxTree(tree)
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

	statementIDs, _ := tree.statementIDs()
	ret, ok := tree.returnArena(statementIDs[len(statementIDs)-1])
	if !ok {
		t.Fatal("return statement is not present in the statement arena")
	}
	values, _ := tree.statementExpressions(ret.values)
	if fact, ok := result.expressionFact(tree.expressionSyntaxID(values[0])); !ok || fact.multiret {
		t.Fatalf("first return expression fact = %#v, %t, want single result", fact, ok)
	}
	if fact, ok := result.expressionFact(tree.expressionSyntaxID(values[1])); !ok || fact.multiret {
		t.Fatalf("second return expression fact = %#v, %t, want single result", fact, ok)
	}
	if fact, ok := result.expressionFact(tree.expressionSyntaxID(values[2])); !ok || !fact.multiret || fact.arity != -1 {
		t.Fatalf("third return expression fact = %#v, %t, want open multiret", fact, ok)
	}
}

func TestParserAssignsStableFunctionIDs(t *testing.T) {
	const source = `
local function outer(value)
	return function() return value end
end
`
	firstTree := parseSourceForBindTest(t, source)
	secondTree := parseSourceForBindTest(t, source)
	firstStatements, _ := firstTree.statementIDs()
	secondStatements, _ := secondTree.statementIDs()
	outerFirst, ok := firstTree.localFunctionArena(firstStatements[0])
	if !ok {
		t.Fatal("outer function is not present in the statement arena")
	}
	outerSecond, ok := secondTree.localFunctionArena(secondStatements[0])
	if !ok {
		t.Fatal("second outer function is not present in the statement arena")
	}
	firstBody, _ := firstTree.statementChildren(outerFirst.statements)
	firstReturn, ok := firstTree.returnArena(firstBody[0])
	if !ok {
		t.Fatal("first function return is not present in the statement arena")
	}
	firstValues, _ := firstTree.statementExpressions(firstReturn.values)
	innerFirstTerm, ok := expressionSingleTerm(firstTree, firstValues[0])
	innerFirst, isFunction := firstTree.termFunction(innerFirstTerm)
	if !ok || !isFunction {
		t.Fatal("inner expression is not a function")
	}
	secondBody, _ := secondTree.statementChildren(outerSecond.statements)
	secondReturn, ok := secondTree.returnArena(secondBody[0])
	if !ok {
		t.Fatal("second function return is not present in the statement arena")
	}
	secondValues, _ := secondTree.statementExpressions(secondReturn.values)
	innerSecondTerm, ok := expressionSingleTerm(secondTree, secondValues[0])
	innerSecond, isFunction := secondTree.termFunction(innerSecondTerm)
	if !ok || !isFunction {
		t.Fatal("second inner expression is not a function")
	}
	innerFirstID := firstTree.functionExpressionFunctionID(innerFirst)
	innerSecondID := secondTree.functionExpressionFunctionID(innerSecond)
	if outerFirst.functionID <= 0 || innerFirstID <= 0 || outerFirst.functionID == innerFirstID {
		t.Fatalf("function IDs = outer %d inner %d, want distinct positive IDs", outerFirst.functionID, innerFirstID)
	}
	if outerFirst.functionID != outerSecond.functionID {
		t.Fatalf("outer function ID changed from %d to %d", outerFirst.functionID, outerSecond.functionID)
	}
	if innerFirstID != innerSecondID {
		t.Fatalf("inner function ID changed from %d to %d", innerFirstID, innerSecondID)
	}
}

func parseSourceForBindTest(t *testing.T, source string) syntaxTree {
	t.Helper()
	p := parser{source: source}
	tree, err := p.parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	return tree
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
