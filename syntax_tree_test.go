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
	if got := tree.expressionTerms(&local.values[0]); got == nil || &got[0] != &local.values[0].terms[0] {
		t.Fatal("expression term facade changed slice identity")
	}
}

func TestSyntaxTreeFacadePreservesNilAndEmptyChildren(t *testing.T) {
	tree := newSyntaxTree(program{})
	if tree.statements() != nil {
		t.Fatal("nil root statements became non-nil")
	}
	if tree.expressionTerms(nil) != nil || tree.typeArgs(nil) != nil || tree.termGroup(nil) != nil {
		t.Fatal("nil child facade returned non-nil value")
	}
	empty := &expression{terms: []andExpression{}}
	if got := tree.expressionTerms(empty); got == nil || len(got) != 0 {
		t.Fatalf("empty expression terms=%#v, want non-nil empty slice", got)
	}
}
