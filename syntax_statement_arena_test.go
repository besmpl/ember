package ember

import "testing"

func TestStatementArenaRejectsInvalidIDsAndSpans(t *testing.T) {
	arena := statementArena{statements: []arenaStatement{{kind: syntaxStatementBreak}}}
	if _, ok := arena.statement(0); ok {
		t.Fatal("zero statement ID resolved")
	}
	if _, ok := arena.statement(2); ok {
		t.Fatal("out-of-range statement ID resolved")
	}
	if _, ok := arena.spanStatements(nodeSpan{start: 1, count: 1}); ok {
		t.Fatal("out-of-range statement span resolved")
	}
}

func TestParserStoresTypedStatementIDs(t *testing.T) {
	tree, err := (&parser{source: "local x = 1\nx = 2\nreturn x"}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	ids, ok := tree.statementIDs()
	if !ok || len(ids) != 3 {
		t.Fatalf("statement IDs=(%v,%t), want three IDs", ids, ok)
	}
	want := []syntaxStatementKind{syntaxStatementLocal, syntaxStatementAssign, syntaxStatementReturn}
	for i, id := range ids {
		if got := tree.statementKindID(id); got != want[i] {
			t.Fatalf("statement %d kind=%v, want %v", i, got, want[i])
		}
	}
	assignID := ids[1]
	assign, ok := tree.assignmentArena(assignID)
	if !ok {
		t.Fatal("assignment payload missing")
	}
	targets, ok := tree.statementTargets(assign.targets)
	if !ok || len(targets) != 1 {
		t.Fatalf("assignment targets=(%v,%t), want one target", targets, ok)
	}
	if _, ok := tree.statementArenaTarget(targets[0]); !ok {
		t.Fatal("assignment target payload missing")
	}
}

func TestNestedStatementSpansRemainContiguous(t *testing.T) {
	tree, err := (&parser{source: "if true then\nlocal x = 1\nend\nreturn 2"}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	ids, ok := tree.statementIDs()
	if !ok || len(ids) != 2 {
		t.Fatalf("root IDs=(%v,%t), want if and return", ids, ok)
	}
	if tree.statementKindID(ids[0]) != syntaxStatementIf || tree.statementKindID(ids[1]) != syntaxStatementReturn {
		t.Fatalf("root kinds=(%v,%v), want if/return", tree.statementKindID(ids[0]), tree.statementKindID(ids[1]))
	}
	ifStmt, ok := tree.ifArena(ids[0])
	if !ok {
		t.Fatal("if payload missing")
	}
	children, ok := tree.statementChildren(ifStmt.thenStatements)
	if !ok || len(children) != 1 || tree.statementKindID(children[0]) != syntaxStatementLocal {
		t.Fatalf("if children=(%v,%t), want one local", children, ok)
	}
}

func TestDirectStatementRangesSurviveArenaParsing(t *testing.T) {
	source := "local value = 1\nvalue.field = 2\nreturn value"
	tree, err := (&parser{source: source}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	ids, ok := tree.statementIDs()
	if !ok || len(ids) != 3 {
		t.Fatalf("statement IDs=(%v,%t), want three statements", ids, ok)
	}
	local, ok := tree.localArena(ids[0])
	if !ok {
		t.Fatal("local payload missing")
	}
	ranges, ok := tree.statementRanges(local.nameRanges)
	if !ok || len(ranges) != 1 || source[ranges[0].start:ranges[0].end] != "value" {
		t.Fatalf("local ranges=(%v,%t), want value", ranges, ok)
	}
	assign, ok := tree.assignmentArena(ids[1])
	if !ok {
		t.Fatal("assignment payload missing")
	}
	targetIDs, ok := tree.statementTargets(assign.targets)
	if !ok || len(targetIDs) != 1 {
		t.Fatalf("assignment targets=(%v,%t), want one target", targetIDs, ok)
	}
	target, ok := tree.statementArenaTarget(targetIDs[0])
	if !ok || source[target.start:target.end] != "value.field" {
		t.Fatalf("assignment target=(%#v,%t), want value.field", target, ok)
	}
	ret, ok := tree.returnArena(ids[2])
	if !ok || source[ret.start:ret.end] != "return value" {
		t.Fatalf("return=(%#v,%t), want return range", ret, ok)
	}
}

func TestDirectFunctionBodyStatementSpan(t *testing.T) {
	tree, err := (&parser{source: "local function f()\nreturn 1\nend\nreturn f()"}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	ids, ok := tree.statementIDs()
	if !ok || len(ids) != 2 || tree.statementKindID(ids[0]) != syntaxStatementLocalFunction || tree.statementKindID(ids[1]) != syntaxStatementReturn {
		t.Fatalf("root IDs/kinds=(%v,%t), want local function and return", ids, ok)
	}
	fn, ok := tree.localFunctionArena(ids[0])
	if !ok {
		t.Fatal("local function payload missing")
	}
	body, ok := tree.statementChildren(fn.statements)
	if !ok || len(body) != 1 || tree.statementKindID(body[0]) != syntaxStatementReturn {
		t.Fatalf("function body=(%v,%t), want one return", body, ok)
	}
}

func TestDirectFunctionDeclarationTargetAndIDs(t *testing.T) {
	source := "function object.method()\nreturn 1\nend\nreturn object.method()"
	tree, err := (&parser{source: source}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	ids, ok := tree.statementIDs()
	if !ok || len(ids) != 2 {
		t.Fatalf("root IDs=(%v,%t), want declaration and return", ids, ok)
	}
	fn, ok := tree.functionDeclarationArena(ids[0])
	if !ok || fn.id == 0 || fn.functionID == 0 || fn.target == 0 {
		t.Fatalf("function declaration=(%#v,%t), want assigned IDs", fn, ok)
	}
	target, ok := tree.statementArenaTarget(fn.target)
	if !ok || target.id == 0 || source[target.start:target.end] != "object.method" {
		t.Fatalf("declaration target=(%#v,%t), want exact range", target, ok)
	}
	body, ok := tree.statementChildren(fn.statements)
	if !ok || len(body) != 1 || tree.statementKindID(body[0]) != syntaxStatementReturn {
		t.Fatalf("declaration body=(%v,%t), want one return", body, ok)
	}
}

func TestDirectStatementFamilies(t *testing.T) {
	source := "local value = 1\nif value then local branch = 2 end\nwhile false do break end\nfor i = 1, 1 do continue end\nfor key in value do break end\nrepeat break until true\ndo local scoped = 3 end\ntype Alias = number\nreturn value"
	tree, err := (&parser{source: source}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	ids, ok := tree.statementIDs()
	if !ok {
		t.Fatal("statement IDs missing")
	}
	want := []syntaxStatementKind{syntaxStatementLocal, syntaxStatementIf, syntaxStatementWhile, syntaxStatementFor, syntaxStatementGenericFor, syntaxStatementRepeat, syntaxStatementBlock, syntaxStatementTypeAlias, syntaxStatementReturn}
	if len(ids) != len(want) {
		t.Fatalf("statement count=%d, want %d (%v)", len(ids), len(want), ids)
	}
	for i, id := range ids {
		if got := tree.statementKindID(id); got != want[i] {
			t.Errorf("statement %d kind=%v, want %v", i, got, want[i])
		}
	}
}

func TestEmptyStringLiteralKeepsNonzeroArenaID(t *testing.T) {
	tree, err := (&parser{source: "local focus = \"\"\nreturn focus"}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	var found bool
	for _, term := range tree.arena.terms {
		if term.kind == termKindString && term.start < term.end && term.payload != 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("empty string literal has no nonzero arena payload")
	}
	proto, err := Compile("local focus = \"\"\nreturn focus")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	values, err := Run(proto)
	if err != nil || len(values) != 1 {
		t.Fatalf("Run returned values=%v err=%v, want one value", values, err)
	}
	if value, ok := values[0].String(); !ok || value != "" {
		t.Fatalf("Run value=(%q,%t), want empty string", value, ok)
	}
}
