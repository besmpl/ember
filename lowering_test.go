package ember

import "testing"

func TestLowerFixedValueListExpandsOnlyFinalExpressionAndPadsNil(t *testing.T) {
	values := parseReturnValuesForLoweringTest(t, `
local function pair()
	return 1, 2
end
return pair(), 3, pair()
`)

	list := lowerFixedValueList(values, 5)

	assertLoweredValueList(t, list, []loweredValueKind{
		loweredValueSingle,
		loweredValueSingle,
		loweredValueExpanded,
		loweredValueNil,
		loweredValueNil,
	})
	if got := list.items[2].resultCount; got != 3 {
		t.Fatalf("final expanded resultCount is %d, want 3", got)
	}
}

func TestLowerOpenValueListExpandsOnlyFinalExpression(t *testing.T) {
	values := parseReturnValuesForLoweringTest(t, `
local function pair()
	return 1, 2
end
return pair(), 3, pair()
`)

	list := lowerOpenValueList(values)

	assertLoweredValueList(t, list, []loweredValueKind{
		loweredValueSingle,
		loweredValueSingle,
		loweredValueExpanded,
	})
	if got := list.items[2].resultCount; got != -1 {
		t.Fatalf("open expanded resultCount is %d, want -1", got)
	}
}

func TestLowerCallRecordsReceiverAndOpenArguments(t *testing.T) {
	call := parseReturnCallForLoweringTest(t, `
local object = {}
local function pair()
	return 1, 2
end
return object:method(1, pair())
`)

	lowered := lowerCall(call)

	if lowered.receiver == nil {
		t.Fatal("lowered call receiver is nil, want method receiver")
	}
	if lowered.fixedArgCount != 1 {
		t.Fatalf("lowered call fixedArgCount is %d, want receiver self-argument", lowered.fixedArgCount)
	}
	assertLoweredValueList(t, lowered.args, []loweredValueKind{
		loweredValueSingle,
		loweredValueExpanded,
	})
	if got := lowered.args.items[1].resultCount; got != -1 {
		t.Fatalf("open call argument resultCount is %d, want -1", got)
	}
}

func TestLowerCallLeavesNonFinalNestedCallSingle(t *testing.T) {
	call := parseReturnCallForLoweringTest(t, `
local function pair()
	return 1, 2
end
return collect(pair(), 3)
`)

	lowered := lowerCall(call)

	if lowered.receiver != nil {
		t.Fatal("lowered call receiver is set, want nil")
	}
	if lowered.fixedArgCount != 0 {
		t.Fatalf("lowered call fixedArgCount is %d, want 0", lowered.fixedArgCount)
	}
	assertLoweredValueList(t, lowered.args, []loweredValueKind{
		loweredValueSingle,
		loweredValueSingle,
	})
}

func TestLowerWhileLoopIsPreTestWithContinueToCondition(t *testing.T) {
	stmt := parseWhileForLoweringTest(t, `
while keepGoing do
	continue
end
return keepGoing
`)

	loop := lowerWhileLoop(stmt)

	if loop.kind != loweredLoopPreTest {
		t.Fatalf("lowered loop kind is %v, want pre-test", loop.kind)
	}
	if loop.continueTarget != loweredLoopContinueCondition {
		t.Fatalf("continue target is %v, want condition", loop.continueTarget)
	}
	if len(loop.body) != 1 || !loop.body[0].continues {
		t.Fatalf("lowered loop body is %#v, want one continue statement", loop.body)
	}
}

func TestLowerRepeatLoopIsPostTestWithContinueToCondition(t *testing.T) {
	stmt := parseRepeatForLoweringTest(t, `
repeat
	continue
until done
return done
`)

	loop := lowerRepeatLoop(stmt)

	if loop.kind != loweredLoopPostTest {
		t.Fatalf("lowered loop kind is %v, want post-test", loop.kind)
	}
	if loop.continueTarget != loweredLoopContinueCondition {
		t.Fatalf("continue target is %v, want condition", loop.continueTarget)
	}
	if len(loop.body) != 1 || !loop.body[0].continues {
		t.Fatalf("lowered loop body is %#v, want one continue statement", loop.body)
	}
}

func TestLowerNumericForLoopRecordsControlPlan(t *testing.T) {
	stmt := parseNumericForForLoweringTest(t, `
for index = 1, 5 do
	continue
end
return index
`)

	loop := lowerNumericForLoop(stmt)

	if loop.name != "index" {
		t.Fatalf("loop name is %q, want index", loop.name)
	}
	if loop.step != nil {
		t.Fatal("loop step is set, want nil default step")
	}
	if !loop.defaultStep {
		t.Fatal("loop defaultStep is false, want true")
	}
	if loop.continueTarget != loweredNumericForContinueIncrement {
		t.Fatalf("continue target is %v, want increment", loop.continueTarget)
	}
	if len(loop.body) != 1 || !loop.body[0].continues {
		t.Fatalf("lowered loop body is %#v, want one continue statement", loop.body)
	}
}

func TestLowerNumericForLoopRecordsExplicitStep(t *testing.T) {
	stmt := parseNumericForForLoweringTest(t, `
for index = 1, 5, -2 do
end
return index
`)

	loop := lowerNumericForLoop(stmt)

	if loop.step == nil {
		t.Fatal("loop step is nil, want explicit step")
	}
	if loop.defaultStep {
		t.Fatal("loop defaultStep is true, want false")
	}
}

func TestLowerGenericForLoopRecordsIteratorPlan(t *testing.T) {
	stmt := parseGenericForForLoweringTest(t, `
for key, value in source do
	continue
end
return source
`)

	loop := lowerGenericForLoop(stmt)

	if got, want := len(loop.names), 2; got != want {
		t.Fatalf("lowered loop has %d names, want %d", got, want)
	}
	if loop.names[0] != "key" || loop.names[1] != "value" {
		t.Fatalf("lowered loop names are %#v, want key/value", loop.names)
	}
	if got, want := len(loop.values), 1; got != want {
		t.Fatalf("lowered loop has %d iterator values, want %d", got, want)
	}
	if !loop.prepareDirectIterator {
		t.Fatal("prepareDirectIterator is false, want true for one iterator expression")
	}
	if loop.continueTarget != loweredGenericForContinueIterator {
		t.Fatalf("continue target is %v, want iterator", loop.continueTarget)
	}
	if len(loop.body) != 1 || !loop.body[0].continues {
		t.Fatalf("lowered loop body is %#v, want one continue statement", loop.body)
	}
}

func TestLowerGenericForLoopSkipsPrepareForExplicitTriplet(t *testing.T) {
	stmt := parseGenericForForLoweringTest(t, `
for key, value in next, source, nil do
end
return source
`)

	loop := lowerGenericForLoop(stmt)

	if loop.prepareDirectIterator {
		t.Fatal("prepareDirectIterator is true, want false for explicit iterator triplet")
	}
	if got, want := len(loop.values), 3; got != want {
		t.Fatalf("lowered loop has %d iterator values, want %d", got, want)
	}
}

func TestLowerClosureRecordsParametersVariadicAndBody(t *testing.T) {
	fn := parseAnonymousFunctionForLoweringTest(t, `
return function(first, ...)
	return first, ...
end
`)

	closure := lowerClosure(fn)

	assertStrings(t, closure.params, []string{"first"})
	if !closure.variadic {
		t.Fatal("closure variadic is false, want true")
	}
	if len(closure.body) != 1 || closure.body[0].ret == nil {
		t.Fatalf("closure body is %#v, want one return statement", closure.body)
	}
}

func TestLowerFunctionDeclarationInjectsMethodSelf(t *testing.T) {
	stmt := parseFunctionDeclarationForLoweringTest(t, `
function player:heal(amount)
	return self.hp + amount
end
return player
`)

	closure := lowerFunctionDeclarationClosure(stmt)

	assertStrings(t, closure.params, []string{"self", "amount"})
	if closure.variadic {
		t.Fatal("closure variadic is true, want false")
	}
	if len(closure.body) != 1 || closure.body[0].ret == nil {
		t.Fatalf("closure body is %#v, want one return statement", closure.body)
	}
}

func TestLowerTableRecordsArrayNamedAndComputedFields(t *testing.T) {
	table := parseReturnTableForLoweringTest(t, `
return {10, hp = 20, ["mp"] = 30, 40}
`)

	lowered := lowerTable(table)

	if got, want := len(lowered.fields), 4; got != want {
		t.Fatalf("lowered table has %d fields, want %d", got, want)
	}
	if lowered.fields[0].kind != loweredTableFieldArray || lowered.fields[0].arrayIndex != 1 {
		t.Fatalf("first lowered field is %#v, want array index 1", lowered.fields[0])
	}
	if lowered.fields[1].kind != loweredTableFieldNamed || lowered.fields[1].name != "hp" {
		t.Fatalf("second lowered field is %#v, want named hp", lowered.fields[1])
	}
	if lowered.fields[2].kind != loweredTableFieldComputed || lowered.fields[2].key == nil {
		t.Fatalf("third lowered field is %#v, want computed key", lowered.fields[2])
	}
	if lowered.fields[3].kind != loweredTableFieldArray || lowered.fields[3].arrayIndex != 2 {
		t.Fatalf("fourth lowered field is %#v, want array index 2", lowered.fields[3])
	}
}

func TestLowerIfStatementRecordsConditionAndBranches(t *testing.T) {
	stmt := parseIfForLoweringTest(t, `
if enabled then
	return 1
else
	return 2
end
`)

	branch := lowerIfStatement(stmt)

	if len(branch.thenBody) != 1 || branch.thenBody[0].ret == nil {
		t.Fatalf("then body is %#v, want one return statement", branch.thenBody)
	}
	if len(branch.elseBody) != 1 || branch.elseBody[0].ret == nil {
		t.Fatalf("else body is %#v, want one return statement", branch.elseBody)
	}
	if len(branch.condition.terms) == 0 {
		t.Fatal("condition is empty")
	}
}

func TestLowerIfExpressionRecordsBranchValues(t *testing.T) {
	expr := parseReturnIfExpressionForLoweringTest(t, `
return if enabled then "on" else "off"
`)

	branch := lowerIfExpression(expr)

	if len(branch.condition.terms) == 0 {
		t.Fatal("condition is empty")
	}
	if got := stringValueFromExpressionForLoweringTest(t, branch.thenValue); got != "on" {
		t.Fatalf("then value is %q, want on", got)
	}
	if got := stringValueFromExpressionForLoweringTest(t, branch.elseValue); got != "off" {
		t.Fatalf("else value is %q, want off", got)
	}
}

func TestLowerAssignmentExpandsFinalCallToTargets(t *testing.T) {
	stmt := parseAssignmentForLoweringTest(t, `
local left, middle, right = 0, 0, 0
local function pair()
	return 2, 3
end
left, middle, right = 1, pair()
return left, middle, right
`)

	lowered := lowerAssignment(stmt)

	if got, want := len(lowered.targets), 3; got != want {
		t.Fatalf("lowered assignment has %d targets, want %d", got, want)
	}
	assertLoweredValueList(t, lowered.values, []loweredValueKind{
		loweredValueSingle,
		loweredValueExpanded,
		loweredValueNil,
	})
	if got := lowered.values.items[1].resultCount; got != 2 {
		t.Fatalf("expanded assignment resultCount is %d, want 2", got)
	}
}

func TestLowerAssignmentPadsMissingValuesWithNil(t *testing.T) {
	stmt := parseAssignmentForLoweringTest(t, `
local left, right = 0, 0
left, right = 1
return left, right
`)

	lowered := lowerAssignment(stmt)

	assertLoweredValueList(t, lowered.values, []loweredValueKind{
		loweredValueSingle,
		loweredValueNil,
	})
}

func TestLowerLocalExpandsFinalCallToNames(t *testing.T) {
	stmt := parseLocalForLoweringTest(t, `
local function pair()
	return 2, 3
end
local left, middle, right = 1, pair()
return left, middle, right
`, "left", "middle", "right")

	lowered := lowerLocal(stmt)

	assertStrings(t, lowered.names, []string{"left", "middle", "right"})
	assertLoweredValueList(t, lowered.values, []loweredValueKind{
		loweredValueSingle,
		loweredValueExpanded,
		loweredValueNil,
	})
	if got := lowered.values.items[1].resultCount; got != 2 {
		t.Fatalf("expanded local resultCount is %d, want 2", got)
	}
}

func TestLowerLocalPadsMissingValuesWithNilAndKeepsAnnotations(t *testing.T) {
	stmt := parseLocalForLoweringTest(t, `
local left: number, right: string = 1
return left, right
`, "left", "right")

	lowered := lowerLocal(stmt)

	assertStrings(t, lowered.names, []string{"left", "right"})
	if got, want := len(lowered.annotations), 2; got != want {
		t.Fatalf("lowered local has %d annotations, want %d", got, want)
	}
	if lowered.annotations[0] == nil || lowered.annotations[1] == nil {
		t.Fatalf("lowered local annotations are %#v, want both preserved", lowered.annotations)
	}
	assertLoweredValueList(t, lowered.values, []loweredValueKind{
		loweredValueSingle,
		loweredValueNil,
	})
}

func TestLowerReturnExpandsFinalCallOpen(t *testing.T) {
	stmt := parseReturnForLoweringTest(t, `
local function pair()
	return 2, 3
end
return 1, pair()
`)

	lowered := lowerReturn(stmt)

	assertLoweredValueList(t, lowered.values, []loweredValueKind{
		loweredValueSingle,
		loweredValueExpanded,
	})
	if got := lowered.values.items[1].resultCount; got != -1 {
		t.Fatalf("expanded return resultCount is %d, want open results", got)
	}
}

func TestLowerReturnLeavesNonFinalCallSingle(t *testing.T) {
	stmt := parseReturnForLoweringTest(t, `
local function pair()
	return 2, 3
end
return pair(), 4
`)

	lowered := lowerReturn(stmt)

	assertLoweredValueList(t, lowered.values, []loweredValueKind{
		loweredValueSingle,
		loweredValueSingle,
	})
}

func TestLowerBlockRecordsLexicalScopeAndBody(t *testing.T) {
	stmt := parseBlockForLoweringTest(t, `
do
	local value = 3
	value = value + 1
end
return value
`)

	lowered := lowerBlock(stmt)

	if !lowered.lexicalScope {
		t.Fatal("lowered block lexicalScope is false, want true")
	}
	if got, want := len(lowered.body), 2; got != want {
		t.Fatalf("lowered block has %d statements, want %d: %#v", got, want, lowered.body)
	}
	if lowered.body[0].local == nil {
		t.Fatalf("first lowered block statement is %#v, want local", lowered.body[0])
	}
	if lowered.body[1].assign == nil {
		t.Fatalf("second lowered block statement is %#v, want assignment", lowered.body[1])
	}
}

func TestLowerCallStatementRecordsDiscardedMethodCall(t *testing.T) {
	stmt := parseCallStatementForLoweringTest(t, `
local object = {}
local function pair()
	return 2, 3
end
object:touch(1, pair())
return object
`)

	lowered := lowerCallStatement(stmt)

	if !lowered.discardResults {
		t.Fatal("lowered call statement discardResults is false, want true")
	}
	if lowered.resultCount != 1 {
		t.Fatalf("lowered call statement resultCount is %d, want one ignored result", lowered.resultCount)
	}
	if lowered.call.receiver == nil {
		t.Fatal("lowered call statement receiver is nil, want method receiver")
	}
	if lowered.call.fixedArgCount != 1 {
		t.Fatalf("lowered call statement fixedArgCount is %d, want receiver self-argument", lowered.call.fixedArgCount)
	}
	assertLoweredValueList(t, lowered.call.args, []loweredValueKind{
		loweredValueSingle,
		loweredValueExpanded,
	})
}

func TestLowerStatementRecordsLocalPayload(t *testing.T) {
	stmt := parseFirstStatementForLoweringTest(t, `
local left, right = 1
return left, right
`)

	lowered := lowerStatement(stmt)

	if lowered.kind != loweredStatementLocal {
		t.Fatalf("lowered statement kind is %v, want local", lowered.kind)
	}
	if lowered.local == nil {
		t.Fatal("lowered statement local payload is nil")
	}
	assertStrings(t, lowered.local.names, []string{"left", "right"})
	assertLoweredValueList(t, lowered.local.values, []loweredValueKind{
		loweredValueSingle,
		loweredValueNil,
	})
}

func TestLowerStatementRecordsCallPayload(t *testing.T) {
	stmt := parseCallOnlyStatementForLoweringTest(t, `
local function touch()
	return 1
end
touch()
return 2
`)

	lowered := lowerStatement(stmt)

	if lowered.kind != loweredStatementCall {
		t.Fatalf("lowered statement kind is %v, want call", lowered.kind)
	}
	if lowered.call == nil {
		t.Fatal("lowered statement call payload is nil")
	}
	if !lowered.call.discardResults {
		t.Fatal("lowered statement call discardResults is false, want true")
	}
}

func TestLowerProgramCollectsRequireRequestsFromLoweredStatements(t *testing.T) {
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

	requests := collectLoweredRequireRequests(lowerProgram(prog))

	assertStrings(t, requests, []string{
		"./inventory",
		"../shared/register",
		"host:clock",
		"./final",
	})
}

func parseReturnValuesForLoweringTest(t *testing.T, source string) []expression {
	t.Helper()
	stmt := parseReturnForLoweringTest(t, source)
	return stmt.values
}

func parseReturnForLoweringTest(t *testing.T, source string) returnStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	for i := len(prog.statements) - 1; i >= 0; i-- {
		stmt := prog.statements[i]
		if stmt.ret != nil {
			return *stmt.ret
		}
	}
	t.Fatal("test source has no return statement")
	return returnStatement{}
}

func parseReturnCallForLoweringTest(t *testing.T, source string) callExpression {
	t.Helper()
	values := parseReturnValuesForLoweringTest(t, source)
	if len(values) != 1 {
		t.Fatalf("return has %d values, want 1", len(values))
	}
	call, ok := expressionSingleCall(values[0])
	if !ok {
		t.Fatal("return value is not a single call")
	}
	return call
}

func parseWhileForLoweringTest(t *testing.T, source string) whileStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 || prog.statements[0].while == nil {
		t.Fatalf("test source did not start with one while statement: %#v", prog.statements)
	}
	return *prog.statements[0].while
}

func parseRepeatForLoweringTest(t *testing.T, source string) repeatStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 || prog.statements[0].repeat == nil {
		t.Fatalf("test source did not start with one repeat statement: %#v", prog.statements)
	}
	return *prog.statements[0].repeat
}

func parseNumericForForLoweringTest(t *testing.T, source string) forStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 || prog.statements[0].forLoop == nil {
		t.Fatalf("test source did not start with one numeric for statement: %#v", prog.statements)
	}
	return *prog.statements[0].forLoop
}

func parseGenericForForLoweringTest(t *testing.T, source string) genericForStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 || prog.statements[0].genericFor == nil {
		t.Fatalf("test source did not start with one generic for statement: %#v", prog.statements)
	}
	return *prog.statements[0].genericFor
}

func parseAssignmentForLoweringTest(t *testing.T, source string) assignStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	for _, stmt := range prog.statements {
		if stmt.assign != nil {
			return *stmt.assign
		}
	}
	t.Fatalf("test source has no assignment statement: %#v", prog.statements)
	return assignStatement{}
}

func parseLocalForLoweringTest(t *testing.T, source string, wantNames ...string) localStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	for _, stmt := range prog.statements {
		if stmt.local == nil {
			continue
		}
		if len(wantNames) == 0 || stringsEqual(stmt.local.names, wantNames) {
			return *stmt.local
		}
	}
	t.Fatalf("test source has no matching local statement %v: %#v", wantNames, prog.statements)
	return localStatement{}
}

func parseBlockForLoweringTest(t *testing.T, source string) blockStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 || prog.statements[0].block == nil {
		t.Fatalf("test source did not start with one block statement: %#v", prog.statements)
	}
	return *prog.statements[0].block
}

func parseCallStatementForLoweringTest(t *testing.T, source string) term {
	t.Helper()
	stmt := parseCallOnlyStatementForLoweringTest(t, source)
	return *stmt.call
}

func parseFirstStatementForLoweringTest(t *testing.T, source string) statement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 {
		t.Fatalf("test source has no statements")
	}
	return prog.statements[0]
}

func parseCallOnlyStatementForLoweringTest(t *testing.T, source string) statement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	for _, stmt := range prog.statements {
		if stmt.call != nil {
			return stmt
		}
	}
	t.Fatalf("test source has no call statement: %#v", prog.statements)
	return statement{}
}

func parseAnonymousFunctionForLoweringTest(t *testing.T, source string) functionExpression {
	t.Helper()
	values := parseReturnValuesForLoweringTest(t, source)
	if len(values) != 1 {
		t.Fatalf("return has %d values, want 1", len(values))
	}
	value, ok := expressionSingleTerm(values[0])
	if !ok || value.function == nil {
		t.Fatal("return value is not one anonymous function")
	}
	return *value.function
}

func parseFunctionDeclarationForLoweringTest(t *testing.T, source string) functionDeclarationStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 || prog.statements[0].funcDecl == nil {
		t.Fatalf("test source did not start with one function declaration: %#v", prog.statements)
	}
	return *prog.statements[0].funcDecl
}

func parseIfForLoweringTest(t *testing.T, source string) ifStatement {
	t.Helper()
	prog := parseSourceForBindTest(t, source)
	if len(prog.statements) == 0 || prog.statements[0].ifStmt == nil {
		t.Fatalf("test source did not start with one if statement: %#v", prog.statements)
	}
	return *prog.statements[0].ifStmt
}

func parseReturnIfExpressionForLoweringTest(t *testing.T, source string) ifExpression {
	t.Helper()
	values := parseReturnValuesForLoweringTest(t, source)
	if len(values) != 1 {
		t.Fatalf("return has %d values, want 1", len(values))
	}
	value, ok := expressionSingleTerm(values[0])
	if !ok || value.ifExpr == nil {
		t.Fatal("return value is not one if expression")
	}
	return *value.ifExpr
}

func parseReturnTableForLoweringTest(t *testing.T, source string) tableExpression {
	t.Helper()
	values := parseReturnValuesForLoweringTest(t, source)
	if len(values) != 1 {
		t.Fatalf("return has %d values, want 1", len(values))
	}
	value, ok := expressionSingleTerm(values[0])
	if !ok || value.table == nil {
		t.Fatal("return value is not one table literal")
	}
	return *value.table
}

func stringValueFromExpressionForLoweringTest(t *testing.T, expr expression) string {
	t.Helper()
	value, ok := expressionSingleTerm(expr)
	if !ok || value.lit == nil {
		t.Fatalf("expression is not one literal term: %#v", expr)
	}
	got, ok := value.lit.String()
	if !ok {
		t.Fatalf("literal is %s, want string", value.lit.Kind())
	}
	return got
}

func assertLoweredValueList(t *testing.T, list loweredValueList, want []loweredValueKind) {
	t.Helper()
	if len(list.items) != len(want) {
		t.Fatalf("lowered list has %d items, want %d: %#v", len(list.items), len(want), list.items)
	}
	for i, item := range list.items {
		if item.kind != want[i] {
			t.Fatalf("item %d kind is %v, want %v; items: %#v", i, item.kind, want[i], list.items)
		}
	}
}

func stringsEqual(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d strings, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("string %d is %q, want %q; got %#v", i, got[i], want[i], got)
		}
	}
}
