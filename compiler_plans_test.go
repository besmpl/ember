package ember

import (
	"reflect"
	"testing"
)

func TestValueListPlanComputesItemsWithoutMaterializingSlice(t *testing.T) {
	prog, err := (&parser{source: "return 1, f()"}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	tree := newSyntaxTree(prog)
	values := prog.statements[0].ret.values
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
	params := []string{"amount"}
	stmt := functionDeclarationStatement{
		params:  params,
		paramID: 10,
		selfID:  9,
		method:  true,
	}
	tree := newSyntaxTree(program{statements: []statement{{funcDecl: &stmt}}})
	plan := planFunctionDeclaration(tree, *tree.functionDeclaration(tree.statement(0)))
	if plan.paramCount() != 2 {
		t.Fatalf("param count = %d, want 2", plan.paramCount())
	}
	if name, id := plan.param(0); name != "self" || id != 9 {
		t.Fatalf("param 0 = %q, %d, want self, 9", name, id)
	}
	if name, id := plan.param(1); name != "amount" || id != 10 {
		t.Fatalf("param 1 = %q, %d, want amount, 10", name, id)
	}
	params[0] = "updated"
	if name, _ := plan.param(1); name != "updated" {
		t.Fatalf("plan copied params; got %q after source update", name)
	}
}

func TestCollectRequireRequestsWalksSyntaxDirectly(t *testing.T) {
	prog := parseSourceForBindTest(t, `
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
	if got := collectRequireRequests(prog); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %#v, want %#v", got, want)
	}
}
