package ember

import "testing"

func TestSyntaxTreeFacadePreservesRootAndPayloadIdentity(t *testing.T) {
	artifact, err := parseSource(Source{Text: "local value = 1\nreturn value\n"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	tree := artifact.tree
	if tree.nodeCount() != tree.root.nodeCount || tree.mode() != tree.root.mode {
		t.Fatalf("root metadata changed through facade")
	}
	statements, ok := tree.statementIDs()
	if !ok || len(statements) != 2 {
		t.Fatalf("statements=%d, want two typed root IDs", len(statements))
	}
	if _, ok := tree.statementAt(-1); ok {
		t.Fatal("negative statement index resolved")
	}
	if _, ok := tree.statementAt(len(statements)); ok {
		t.Fatal("out-of-range statement index resolved")
	}
	local, ok := tree.localArena(statements[0])
	if !ok || tree.statementKindID(statements[0]) != syntaxStatementLocal {
		t.Fatalf("local payload=%#v kind=%v", local, tree.statementKindID(statements[0]))
	}
	values, ok := tree.statementExpressions(local.values)
	if !ok || len(values) != 1 {
		t.Fatalf("local values=%v, ok=%t", values, ok)
	}
	got, ok := tree.expressionTerms(values[0])
	if !ok || len(got) == 0 {
		t.Fatal("expression term facade did not resolve arena children")
	}
	node, ok := tree.arena.expression(values[0])
	if !ok || got[0] != tree.arena.expressionTerms[node.terms.start] {
		t.Fatal("expression term facade changed child identity")
	}
}

func TestSyntaxTreeFacadePreservesNilAndEmptyChildren(t *testing.T) {
	tree := newSyntaxTree(program{})
	if ids, ok := tree.statementIDs(); ok && ids != nil {
		t.Fatal("nil root statements became non-nil typed IDs")
	}
	if terms, ok := tree.expressionTerms(0); ok || terms != nil {
		t.Fatal("zero expression ID returned children")
	}
	if tree.typeArgs(0) != nil {
		t.Fatal("nil child facade returned non-nil value")
	}
	if group, ok := tree.termGroup(0); ok || group != 0 {
		t.Fatal("zero term ID returned a group")
	}
	tree.arena = &syntaxArena{
		expressions:     []arenaExpression{{terms: nodeSpan{}}},
		expressionTerms: make([]andExpressionID, 0),
	}
	if got, ok := tree.expressionTerms(1); !ok || got == nil || len(got) != 0 {
		t.Fatalf("empty expression terms=%#v, want non-nil empty slice", got)
	}
}
