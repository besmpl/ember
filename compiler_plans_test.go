package ember

import (
	"reflect"
	"testing"
)

func TestValueListPlanComputesItemsWithoutMaterializingSlice(t *testing.T) {
	tree, err := (&parser{source: "return 1, f()"}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	ids, _ := tree.statementIDs()
	ret, ok := tree.returnArena(ids[0])
	if !ok {
		t.Fatal("return payload missing")
	}
	values, _ := tree.statementExpressions(ret.values)
	plan := fixedValueListPlan(tree, values, 4)
	want := []valuePlan{
		{kind: valuePlanSingle, source: 0, resultCount: 1},
		{kind: valuePlanExpanded, source: 1, resultCount: 3},
		{kind: valuePlanNil, source: -1, resultCount: 1},
		{kind: valuePlanNil, source: -1, resultCount: 1},
	}
	for i, expected := range want {
		if got := plan.item(i); got != expected {
			t.Fatalf("item %d = %#v, want %#v", i, got, expected)
		}
	}
}

func TestClosurePlanViewsMethodSelfWithoutCopyingParams(t *testing.T) {
	arena := &syntaxArena{
		strings:   []string{"amount", "object", "method"},
		selectors: []arenaSelector{{field: 3}},
		statements: statementArena{
			stringIDs: []stringID{1},
			assignTargets: []arenaAssignTarget{{
				name: 2, selectors: nodeSpan{count: 1},
			}},
			functionDecls: []arenaFunctionStatement{{
				target: 1, params: nodeSpan{count: 1}, paramID: 10, selfID: 9, method: true,
			}},
			statements:   []arenaStatement{{kind: syntaxStatementFunctionDeclaration, payload: 1}},
			statementIDs: []statementID{1},
		},
	}
	arena.statements.functionDecls[0].params = nodeSpan{count: 1}
	tree := newSyntaxTreeWithArena(program{statementSpan: nodeSpan{count: 1}}, arena)
	stmt, ok := tree.functionDeclarationArena(1)
	if !ok {
		t.Fatal("function declaration payload missing")
	}
	plan, err := planFunctionDeclaration(tree, stmt)
	if err != nil {
		t.Fatalf("planFunctionDeclaration returned error: %v", err)
	}
	if plan.paramCount() != 2 {
		t.Fatalf("param count = %d, want 2", plan.paramCount())
	}
	if name, id := plan.param(0); name != "self" || id != 9 {
		t.Fatalf("param 0 = %q, %d, want self, 9", name, id)
	}
	if name, id := plan.param(1); name != "amount" || id != 10 {
		t.Fatalf("param 1 = %q, %d, want amount, 10", name, id)
	}
	arena.strings[0] = "updated"
	if name, _ := plan.param(1); name != "updated" {
		t.Fatalf("param 1 = %q after arena update, want updated", name)
	}
}

func TestCollectRequireRequestsWalksSyntaxDirectly(t *testing.T) {
	tree := parseSourceForBindTest(t, `
local inventory = require("./inventory")
require("../shared/register")
local hooks = {
	startup = function()
		return require("host:clock")
	end,
}
return require("./final")
`)
	want := []string{"./inventory", "../shared/register", "host:clock", "./final"}
	if got := collectRequireRequestsTree(tree); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %#v, want %#v", got, want)
	}
}
