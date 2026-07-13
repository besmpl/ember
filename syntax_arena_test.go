package ember

import (
	"math"
	"testing"
	"unsafe"
)

func firstReturnExpressionIDs(tree syntaxTree) []expressionID {
	ids, ok := tree.statementIDs()
	if !ok || len(ids) == 0 {
		return nil
	}
	ret, ok := tree.returnArena(ids[0])
	if !ok {
		return nil
	}
	values, _ := tree.statementExpressions(ret.values)
	return values
}

func TestExpressionArenaStoresTypedIDsAndContiguousChildren(t *testing.T) {
	tree := syntaxTree{arena: &syntaxArena{
		expressions:     []arenaExpression{{terms: nodeSpan{start: 0, count: 2}}},
		expressionTerms: []andExpressionID{1, 2},
	}}
	node, ok := tree.arena.expression(expressionID(1))
	if !ok {
		t.Fatal("expression ID did not resolve")
	}
	if node.terms.count != 2 {
		t.Fatalf("expression child count=%d, want 2", node.terms.count)
	}
	children, ok := tree.arena.andIDs(node.terms)
	if !ok {
		t.Fatal("expression child span did not resolve")
	}
	if len(children) != 2 || children[0] == 0 || children[1] == 0 {
		t.Fatalf("expression child IDs=%v", children)
	}
	if node.terms.start+node.terms.count > uint32(len(tree.arena.expressionTerms)) {
		t.Fatalf("expression children span is out of bounds: span=%#v storage=%d", node.terms, len(tree.arena.expressionTerms))
	}
	for i, child := range children {
		if child != tree.arena.expressionTerms[node.terms.start+uint32(i)] {
			t.Fatalf("expression child span mismatch at %d: got %d want %d", i, child, tree.arena.expressionTerms[node.terms.start+uint32(i)])
		}
	}
}

func TestExpressionArenaCheckedResolversRejectInvalidIDs(t *testing.T) {
	tree := syntaxTree{arena: &syntaxArena{expressions: []arenaExpression{{}}}}
	if _, ok := tree.arena.expression(expressionID(2)); ok {
		t.Fatal("out-of-range expression ID resolved")
	}
	if _, ok := tree.arena.and(andExpressionID(1)); ok {
		t.Fatal("wrong typed ID unexpectedly resolved")
	}
}

func TestExpressionArenaFacadeRejectsInvalidTermPayloadIDs(t *testing.T) {
	tree := syntaxTree{arena: &syntaxArena{terms: []arenaTerm{
		{kind: termKindCall, payload: uint64(math.MaxUint32)},
		{kind: termKindGroup, payload: uint64(math.MaxUint32)},
		{kind: termKindUnaryNot, payload: uint64(math.MaxUint32)},
	}}}
	if _, ok := tree.termCall(1); ok {
		t.Fatal("call term resolved an invalid call payload")
	}
	if _, ok := tree.termGroup(2); ok {
		t.Fatal("group term resolved an invalid expression payload")
	}
	if _, ok := tree.termChild(3); ok {
		t.Fatal("unary term resolved an invalid child payload")
	}
}

func TestIDListBuilderUsesInlineStorageForShortLists(t *testing.T) {
	var b idListBuilder[termID]
	for i := termID(1); i <= 4; i++ {
		b.append(i)
	}
	if b.count != 4 || len(b.extra) != 0 {
		t.Fatalf("builder after four IDs=%#v", b)
	}
	b.append(5)
	if b.count != 5 || len(b.extra) != 1 {
		t.Fatalf("builder after spill=%#v", b)
	}
}

func TestArenaNodeLayouts(t *testing.T) {
	t.Logf("arenaExpression=%d arenaAnd=%d arenaComparison=%d arenaConcat=%d arenaAdditive=%d arenaMultiplicative=%d arenaTerm=%d", unsafe.Sizeof(arenaExpression{}), unsafe.Sizeof(arenaAndExpression{}), unsafe.Sizeof(arenaComparisonExpression{}), unsafe.Sizeof(arenaConcatExpression{}), unsafe.Sizeof(arenaAdditiveExpression{}), unsafe.Sizeof(arenaMultiplicativeExpression{}), unsafe.Sizeof(arenaTerm{}))
}

func TestArenaResolversRejectNilAndOverflowSpans(t *testing.T) {
	var arena *syntaxArena
	if _, ok := arena.expression(expressionID(1)); ok {
		t.Fatal("nil arena resolved an expression")
	}
	if _, ok := arena.andIDs(nodeSpan{start: 0, count: 1}); ok {
		t.Fatal("nil arena resolved a child span")
	}
	values := []termID{1}
	if _, ok := arenaSpan(values, nodeSpan{start: math.MaxUint32, count: 1}); ok {
		t.Fatal("overflow span resolved")
	}
	if _, ok := arenaSpan(values, nodeSpan{start: math.MaxUint32, count: math.MaxUint32}); ok {
		t.Fatal("overflowing span resolved")
	}
}

func TestArenaAppendIDsUseIndexPlusOne(t *testing.T) {
	var arena syntaxArena
	if got := arena.appendExpression(arenaExpression{}); got != 1 {
		t.Fatalf("first expression ID=%d, want 1", got)
	}
	if got := arena.appendTerm(arenaTerm{}); got != 1 {
		t.Fatalf("first term ID=%d, want 1", got)
	}
	if _, ok := arena.expression(0); ok {
		t.Fatal("zero expression ID resolved")
	}
	if _, ok := arena.term(0); ok {
		t.Fatal("zero term ID resolved")
	}
	if span, ok := appendArenaIDsChecked(&arena.expressionTerms, []andExpressionID{1, 2}); !ok || span != (nodeSpan{start: 0, count: 2}) {
		t.Fatalf("child span=(%#v,%t)", span, ok)
	}
}

func TestSyntaxIDsPersistInFunctionArenaPayload(t *testing.T) {
	arena := &syntaxArena{
		expressions:     []arenaExpression{{terms: nodeSpan{start: 0, count: 1}}},
		expressionTerms: []andExpressionID{1},
		andExpressions:  []arenaAndExpression{{terms: nodeSpan{start: 0, count: 1}}},
		andTerms:        []comparisonExpressionID{1},
		comparisons:     []arenaComparisonExpression{{left: 1}},
		concats:         []arenaConcatExpression{{first: 1}},
		additives:       []arenaAdditiveExpression{{first: 1}},
		multiplicatives: []arenaMultiplicativeExpression{{first: 1}},
		terms:           []arenaTerm{{kind: termKindFunction, payload: 1}},
		functions:       []arenaFunction{{params: nodeSpan{start: 0, count: 1}}},
		statements: statementArena{
			statements:       []arenaStatement{{kind: syntaxStatementReturn, payload: 1}},
			statementIDs:     []statementID{1},
			returnStatements: []arenaReturnStatement{{values: nodeSpan{start: 0, count: 1}}},
			expressionIDs:    []expressionID{1},
			stringIDs:        []stringID{1},
		},
	}
	tree := newSyntaxTreeWithArena(program{statementSpan: nodeSpan{start: 0, count: 1}}, arena)
	if err := assignSyntaxTreeIDsWithLimit(&tree, 0); err != nil {
		t.Fatalf("assignSyntaxTreeIDsWithLimit returned error: %v", err)
	}
	fn, ok := tree.arena.function(1)
	if !ok || fn.id == 0 || fn.functionID == 0 || fn.paramID == 0 {
		t.Fatalf("function payload IDs not persisted: %#v", fn)
	}
}

func TestParserAssignsReachableExpressionArenaIDs(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return 1 + 2 * 3 or false"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	if artifact.tree.arena == nil {
		t.Fatal("parser did not attach an arena")
	}
	values := firstReturnExpressionIDs(artifact.tree)
	if len(values) != 1 || values[0] == 0 {
		t.Fatalf("return expression arena ID missing: %#v", values)
	}
	root, ok := artifact.tree.arena.expression(values[0])
	if !ok {
		t.Fatal("root expression arena ID did not resolve")
	}
	children, ok := artifact.tree.arena.andIDs(root.terms)
	if !ok || len(children) != 2 {
		t.Fatalf("root expression children=%v, ok=%t", children, ok)
	}
	for _, child := range children {
		if _, ok := artifact.tree.arena.and(child); !ok {
			t.Fatalf("unreachable/invalid and-expression ID %d", child)
		}
	}
}

func TestExpressionArenaPreservesRangesSelectorsAndStableSyntaxIDs(t *testing.T) {
	const source = "return foo.bar[1]"
	first, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("first parseSource returned error: %v", err)
	}
	second, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("second parseSource returned error: %v", err)
	}
	firstValue := firstReturnExpressionIDs(first.tree)[0]
	secondValue := firstReturnExpressionIDs(second.tree)[0]
	firstTerm, ok := expressionSingleTerm(first.tree, firstValue)
	if !ok {
		t.Fatal("first return value is not a single term")
	}
	secondTerm, ok := expressionSingleTerm(second.tree, secondValue)
	if !ok {
		t.Fatal("second return value is not a single term")
	}
	start, end := first.tree.termRange(firstTerm)
	if got := source[start:end]; got != "foo.bar[1]" {
		t.Fatalf("term range selects %q, want foo.bar[1]", got)
	}
	selectors, ok := first.tree.termSelectors(firstTerm)
	if !ok || len(selectors) != 2 || first.tree.termSelectorField(selectors[0]) != "bar" || first.tree.termSelectorIndex(selectors[1]) == 0 {
		t.Fatalf("term selectors=%#v, ok=%t", selectors, ok)
	}
	if first.tree.expressionSyntaxID(firstValue) != second.tree.expressionSyntaxID(secondValue) ||
		first.tree.termSyntaxID(firstTerm) != second.tree.termSyntaxID(secondTerm) ||
		first.tree.nodeCount() != second.tree.nodeCount() {
		t.Fatal("syntax IDs or node count changed across identical parses")
	}
}

func TestExpressionArenaSyntaxIDAssignmentIsRepeatable(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return ({value = 1}):read()"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	value := firstReturnExpressionIDs(artifact.tree)[0]
	term, ok := expressionSingleTerm(artifact.tree, value)
	if !ok {
		t.Fatal("return value is not a single term")
	}
	wantExpressionID := artifact.tree.expressionSyntaxID(value)
	wantTermID := artifact.tree.termSyntaxID(term)
	wantNodes := artifact.tree.nodeCount()
	if err := assignSyntaxTreeIDsWithLimit(&artifact.tree, 0); err != nil {
		t.Fatalf("second ID assignment returned error: %v", err)
	}
	if got := artifact.tree.expressionSyntaxID(value); got != wantExpressionID {
		t.Fatalf("expression syntax ID after reassignment=%d, want %d", got, wantExpressionID)
	}
	if got := artifact.tree.termSyntaxID(term); got != wantTermID {
		t.Fatalf("term syntax ID after reassignment=%d, want %d", got, wantTermID)
	}
	if got := artifact.tree.nodeCount(); got != wantNodes {
		t.Fatalf("node count after reassignment=%d, want %d", got, wantNodes)
	}
}

func TestExpressionArenaPreservesOperatorPrecedence(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return 1 + 2 * 3 == 7 and 4 or 5"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	values := firstReturnExpressionIDs(artifact.tree)
	orTerms, ok := artifact.tree.expressionTerms(values[0])
	if !ok || len(orTerms) != 2 {
		t.Fatalf("or terms=%v, ok=%t, want two", orTerms, ok)
	}
	andTerms, ok := artifact.tree.andTerms(orTerms[0])
	if !ok || len(andTerms) != 2 {
		t.Fatalf("and terms=%v, ok=%t, want two", andTerms, ok)
	}
	comparison := andTerms[0]
	if artifact.tree.comparisonOperator(comparison) != comparisonEqual {
		t.Fatalf("comparison operator=%q, want ==", artifact.tree.comparisonOperator(comparison))
	}
	left := artifact.tree.comparisonLeft(comparison)
	add := artifact.tree.concatFirst(left)
	addRest, ok := artifact.tree.additiveRest(add)
	if !ok || len(addRest) != 1 || artifact.tree.additivePartOperator(addRest[0]) != additiveAdd {
		t.Fatalf("additive rest=%#v, ok=%t", addRest, ok)
	}
	mulRest, ok := artifact.tree.multiplicativeRest(artifact.tree.additivePartValue(addRest[0]))
	if !ok || len(mulRest) != 1 || artifact.tree.multiplicativePartOperator(mulRest[0]) != multiplicativeMultiply {
		t.Fatalf("multiplicative rest=%#v, ok=%t", mulRest, ok)
	}
}

func TestExpressionArenaStoresTableCallFunctionAndIfTerms(t *testing.T) {
	const source = `return {
	call = f(1),
	fn = function(x) return x end,
	choice = if true then 1 else 2,
}`
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	values := firstReturnExpressionIDs(artifact.tree)
	tableTerm, ok := expressionSingleTerm(artifact.tree, values[0])
	if !ok {
		t.Fatal("return value is not a single table term")
	}
	table, ok := artifact.tree.termTable(tableTerm)
	if !ok {
		t.Fatal("return term is not a table")
	}
	fields, ok := artifact.tree.tableFields(table)
	if !ok || len(fields) != 3 {
		t.Fatalf("table fields=%#v, ok=%t", fields, ok)
	}
	want := []syntaxTermKind{syntaxTermCall, syntaxTermFunction, syntaxTermIf}
	for i, field := range fields {
		value, ok := expressionSingleTerm(artifact.tree, artifact.tree.tableFieldValue(field))
		if !ok || artifact.tree.termKind(value) != want[i] {
			t.Fatalf("field %d kind=%v, ok=%t, want %v", i, artifact.tree.termKind(value), ok, want[i])
		}
	}
}

func TestExpressionArenaExecutesCastedAndTableMethodCalls(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   float64
	}{
		{
			name:   "casted local",
			source: "local f = function(x) return x + 1 end\nreturn (f :: (number) -> number)(2)",
			want:   3,
		},
		{
			name:   "table method",
			source: "return ({value = 7, read = function(self) return self.value end}):read()",
			want:   7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			results, err := Run(proto)
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("Run returned %d values, want one", len(results))
			}
			got, ok := results[0].Number()
			if !ok || got != test.want {
				t.Fatalf("Run result=%v, numeric=%t, want %v", results[0], ok, test.want)
			}
		})
	}
}
