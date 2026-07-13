package ember

import "fmt"

func resolveArenaName(tree syntaxTree, id stringID) (string, bool) {
	value, ok := tree.stringValue(id)
	return value, ok && value != ""
}

func resolveArenaSelectorField(tree syntaxTree, selector arenaSelector) (string, bool) {
	if selector.field == 0 {
		return "", false
	}
	return resolveArenaName(tree, selector.field)
}

type valuePlanKind uint8

const (
	valuePlanSingle valuePlanKind = iota
	valuePlanExpanded
	valuePlanNil
)

type valuePlan struct {
	kind        valuePlanKind
	source      int
	resultCount int
}

type valueListPlan struct {
	tree        syntaxTree
	values      []expressionID
	targetCount int
	open        bool
}

func fixedValueListPlan(tree syntaxTree, values []expressionID, targetCount int) valueListPlan {
	return valueListPlan{tree: tree, values: values, targetCount: targetCount}
}

func openValueListPlan(tree syntaxTree, values []expressionID) valueListPlan {
	return valueListPlan{tree: tree, values: values, targetCount: len(values), open: true}
}

func (p valueListPlan) len() int {
	return p.targetCount
}

func (p valueListPlan) item(index int) valuePlan {
	if index < 0 || index >= p.targetCount {
		return valuePlan{kind: valuePlanNil, source: -1, resultCount: 1}
	}
	if index >= len(p.values) {
		return valuePlan{kind: valuePlanNil, source: -1, resultCount: 1}
	}
	if index == len(p.values)-1 && expressionExpands(p.tree, p.values[index]) {
		resultCount := p.targetCount - index
		if p.open {
			resultCount = -1
		}
		return valuePlan{kind: valuePlanExpanded, source: index, resultCount: resultCount}
	}
	return valuePlan{kind: valuePlanSingle, source: index, resultCount: 1}
}

type callPlan struct {
	target        termID
	receiver      termID
	args          valueListPlan
	fixedArgCount int
}

func planCall(tree syntaxTree, call arenaCallID) callPlan {
	fixedArgCount := 0
	receiver := tree.callReceiver(call)
	if receiver != 0 {
		fixedArgCount = 1
	}
	args, _ := tree.callArgs(call)
	return callPlan{
		target:        tree.callTarget(call),
		receiver:      receiver,
		args:          openValueListPlan(tree, args),
		fixedArgCount: fixedArgCount,
	}
}

type closurePlan struct {
	tree           syntaxTree
	params         []stringID
	paramID        syntaxID
	implicitSelfID syntaxID
	variadic       bool
	body           []statementID
	functionName   string
}

func planFunctionExpression(tree syntaxTree, fn arenaFunctionID) (closurePlan, error) {
	paramSpan, ok := tree.functionExpressionParamIDs(fn)
	if !ok {
		return closurePlan{}, fmt.Errorf("compile: invalid function expression")
	}
	bodySpan, ok := tree.functionExpressionStatementIDs(fn)
	if !ok {
		return closurePlan{}, fmt.Errorf("compile: invalid function expression")
	}
	params, body, err := closurePlanSpans(tree, paramSpan, bodySpan)
	if err != nil {
		return closurePlan{}, err
	}
	return closurePlan{
		tree:           tree,
		params:         params,
		paramID:        tree.functionExpressionParamID(fn),
		implicitSelfID: 0,
		variadic:       tree.functionExpressionVariadic(fn),
		body:           body,
		functionName:   "<anonymous>",
	}, nil
}

func planLocalFunction(tree syntaxTree, stmt arenaFunctionStatement) (closurePlan, error) {
	name, ok := resolveArenaName(tree, stmt.name)
	if !ok {
		return closurePlan{}, fmt.Errorf("compile: invalid local function name")
	}
	params, body, err := closurePlanSpans(tree, stmt.params, stmt.statements)
	if err != nil {
		return closurePlan{}, err
	}
	return closurePlan{
		tree:         tree,
		params:       params,
		paramID:      stmt.paramID,
		variadic:     stmt.variadic,
		body:         body,
		functionName: name,
	}, nil
}

func planFunctionDeclaration(tree syntaxTree, stmt arenaFunctionStatement) (closurePlan, error) {
	params, body, err := closurePlanSpans(tree, stmt.params, stmt.statements)
	if err != nil {
		return closurePlan{}, err
	}
	name, err := functionDeclarationName(tree, stmt.target, stmt.method)
	if err != nil {
		return closurePlan{}, err
	}
	plan := closurePlan{
		tree:         tree,
		params:       params,
		paramID:      stmt.paramID,
		variadic:     stmt.variadic,
		body:         body,
		functionName: name,
	}
	if stmt.method {
		plan.implicitSelfID = stmt.selfID
	}
	return plan, nil
}

func closurePlanSpans(tree syntaxTree, paramSpan, bodySpan nodeSpan) ([]stringID, []statementID, error) {
	params, ok := tree.statementStrings(paramSpan)
	if !ok {
		return nil, nil, fmt.Errorf("compile: invalid function parameter span")
	}
	for _, param := range params {
		if _, ok := resolveArenaName(tree, param); !ok {
			return nil, nil, fmt.Errorf("compile: invalid function parameter name")
		}
	}
	body, ok := tree.statementChildren(bodySpan)
	if !ok {
		return nil, nil, fmt.Errorf("compile: invalid function body span")
	}
	return params, body, nil
}

func functionDeclarationName(tree syntaxTree, targetID assignTargetID, method bool) (string, error) {
	target, ok := tree.statementArenaTarget(targetID)
	if !ok {
		return "", fmt.Errorf("compile: invalid function declaration target")
	}
	name, ok := resolveArenaName(tree, target.name)
	if !ok {
		return "", fmt.Errorf("compile: invalid function declaration name")
	}
	selectors, ok := tree.selectorSpan(target.selectors)
	if !ok {
		return "", fmt.Errorf("compile: invalid function declaration selector span")
	}
	for index, selector := range selectors {
		field, ok := resolveArenaSelectorField(tree, selector)
		if !ok {
			return "", fmt.Errorf("compile: invalid selector field")
		}
		separator := "."
		if method && index == len(selectors)-1 {
			separator = ":"
		}
		name += separator + field
	}
	return name, nil
}

func statementChildValues(tree syntaxTree, span nodeSpan) []statementID {
	ids, _ := tree.statementChildren(span)
	return ids
}

func (p closurePlan) paramCount() int {
	if p.implicitSelfID != 0 {
		return len(p.params) + 1
	}
	return len(p.params)
}

func (p closurePlan) param(index int) (string, syntaxID) {
	if p.implicitSelfID != 0 {
		if index == 0 {
			return "self", p.implicitSelfID
		}
		index--
	}
	name, _ := resolveArenaName(p.tree, p.params[index])
	return name, syntaxNameID(p.paramID, index)
}

func expressionExpands(tree syntaxTree, expr expressionID) bool {
	if value, ok := expressionRawSingleTerm(tree, expr); ok && tree.termKind(value) == syntaxTermGroup {
		return false
	}
	if _, ok := expressionSingleVararg(tree, expr); ok {
		return true
	}
	if _, ok := expressionSingleCall(tree, expr); ok {
		return true
	}
	return false
}
