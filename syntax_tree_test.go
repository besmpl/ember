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
	statements := tree.statements()
	if len(statements) != len(tree.root.statements) || len(statements) == 0 {
		t.Fatalf("statements=%d, want non-empty root view", len(statements))
	}
	if tree.statement(0) != &tree.root.statements[0] {
		t.Fatal("statement accessor changed pointer identity")
	}
	if tree.statement(-1) != nil || tree.statement(len(statements)) != nil {
		t.Fatal("out-of-range statement accessor was non-nil")
	}
	local := tree.local(tree.statement(0))
	if local == nil || tree.statementKind(tree.statement(0)) != syntaxStatementLocal {
		t.Fatalf("local payload=%#v kind=%v", local, tree.statementKind(tree.statement(0)))
	}
	got, ok := tree.expressionTerms(local.values[0])
	if !ok || len(got) == 0 {
		t.Fatal("expression term facade did not resolve arena children")
	}
	node, ok := tree.arena.expression(local.values[0])
	if !ok || got[0] != tree.arena.expressionTerms[node.terms.start] {
		t.Fatal("expression term facade changed child identity")
	}
}

func TestSyntaxTreeFacadePreservesNilAndEmptyChildren(t *testing.T) {
	tree := newSyntaxTree(program{})
	if tree.statements() != nil {
		t.Fatal("nil root statements became non-nil")
	}
	if terms, ok := tree.expressionTerms(0); ok || terms != nil {
		t.Fatal("zero expression ID returned children")
	}
	if tree.typeArgs(nil) != nil {
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
