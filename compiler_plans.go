package ember

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
	values      []expression
	targetCount int
	open        bool
}

func fixedValueListPlan(tree syntaxTree, values []expression, targetCount int) valueListPlan {
	return valueListPlan{tree: tree, values: values, targetCount: targetCount}
}

func openValueListPlan(tree syntaxTree, values []expression) valueListPlan {
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
	target        term
	receiver      *term
	args          valueListPlan
	fixedArgCount int
}

func planCall(tree syntaxTree, call callExpression) callPlan {
	fixedArgCount := 0
	if tree.callReceiver(&call) != nil {
		fixedArgCount = 1
	}
	return callPlan{
		target:        *tree.callTarget(&call),
		receiver:      tree.callReceiver(&call),
		args:          openValueListPlan(tree, tree.callArgs(&call)),
		fixedArgCount: fixedArgCount,
	}
}

type closurePlan struct {
	params         []string
	paramID        syntaxID
	implicitSelfID syntaxID
	variadic       bool
	body           []statement
	functionName   string
}

func planFunctionExpression(tree syntaxTree, fn functionExpression) closurePlan {
	return closurePlan{
		params:       tree.functionExpressionParams(&fn),
		paramID:      tree.functionExpressionParamID(&fn),
		variadic:     tree.functionExpressionVariadic(&fn),
		body:         tree.functionExpressionStatements(&fn),
		functionName: "<anonymous>",
	}
}

func planLocalFunction(tree syntaxTree, stmt localFunctionStatement) closurePlan {
	return closurePlan{
		params:       tree.localFunctionParams(&stmt),
		paramID:      tree.localFunctionParamID(&stmt),
		variadic:     tree.localFunctionVariadic(&stmt),
		body:         tree.localFunctionStatements(&stmt),
		functionName: tree.localFunctionName(&stmt),
	}
}

func planFunctionDeclaration(tree syntaxTree, stmt functionDeclarationStatement) closurePlan {
	plan := closurePlan{
		params:       tree.functionDeclarationParams(&stmt),
		paramID:      tree.functionDeclarationParamID(&stmt),
		variadic:     tree.functionDeclarationVariadic(&stmt),
		body:         tree.functionDeclarationStatements(&stmt),
		functionName: functionDeclarationName(tree, *tree.functionDeclarationTarget(&stmt), tree.functionDeclarationMethod(&stmt)),
	}
	if tree.functionDeclarationMethod(&stmt) {
		plan.implicitSelfID = tree.functionDeclarationSelfID(&stmt)
	}
	return plan
}

func functionDeclarationName(tree syntaxTree, target assignTarget, method bool) string {
	name := tree.assignTargetName(&target)
	selectors := tree.assignTargetSelectors(&target)
	for index := range selectors {
		selector := &selectors[index]
		if tree.selectorField(selector) == "" {
			continue
		}
		separator := "."
		if method && index == len(selectors)-1 {
			separator = ":"
		}
		name += separator + tree.selectorField(selector)
	}
	return name
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
	return p.params[index], syntaxNameID(p.paramID, index)
}

func expressionExpands(tree syntaxTree, expr expression) bool {
	if _, ok := expressionSingleVararg(tree, expr); ok {
		return true
	}
	if _, ok := expressionSingleCall(tree, expr); ok {
		return true
	}
	return false
}
