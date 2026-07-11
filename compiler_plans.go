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
	values      []expression
	targetCount int
	open        bool
}

func fixedValueListPlan(values []expression, targetCount int) valueListPlan {
	return valueListPlan{values: values, targetCount: targetCount}
}

func openValueListPlan(values []expression) valueListPlan {
	return valueListPlan{values: values, targetCount: len(values), open: true}
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
	if index == len(p.values)-1 && expressionExpands(p.values[index]) {
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

func planCall(call callExpression) callPlan {
	fixedArgCount := 0
	if call.receiver != nil {
		fixedArgCount = 1
	}
	return callPlan{
		target:        call.target,
		receiver:      call.receiver,
		args:          openValueListPlan(call.args),
		fixedArgCount: fixedArgCount,
	}
}

type closurePlan struct {
	params         []string
	paramID        syntaxID
	implicitSelfID syntaxID
	variadic       bool
	body           []statement
}

func planFunctionExpression(fn functionExpression) closurePlan {
	return closurePlan{
		params:   fn.params,
		paramID:  fn.paramID,
		variadic: fn.variadic,
		body:     fn.statements,
	}
}

func planLocalFunction(stmt localFunctionStatement) closurePlan {
	return closurePlan{
		params:   stmt.params,
		paramID:  stmt.paramID,
		variadic: stmt.variadic,
		body:     stmt.statements,
	}
}

func planFunctionDeclaration(stmt functionDeclarationStatement) closurePlan {
	plan := closurePlan{
		params:   stmt.params,
		paramID:  stmt.paramID,
		variadic: stmt.variadic,
		body:     stmt.statements,
	}
	if stmt.method {
		plan.implicitSelfID = stmt.selfID
	}
	return plan
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

func expressionExpands(expr expression) bool {
	if _, ok := expressionSingleVararg(expr); ok {
		return true
	}
	if _, ok := expressionSingleCall(expr); ok {
		return true
	}
	return false
}
