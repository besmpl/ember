package ember

type syntaxID int

type syntaxIDAssigner struct {
	nextNode     syntaxID
	nextFunction int
}

func assignProgramSyntaxIDs(prog *program) {
	if prog == nil {
		return
	}
	a := syntaxIDAssigner{}
	prog.id = a.node()
	a.statements(prog.statements)
	prog.nodeCount = int(a.nextNode)
}

func (a *syntaxIDAssigner) node() syntaxID {
	a.nextNode++
	return a.nextNode
}

func (a *syntaxIDAssigner) function() int {
	a.nextFunction++
	return a.nextFunction
}

func (a *syntaxIDAssigner) names(names []string) syntaxID {
	if len(names) == 0 {
		return 0
	}
	first := a.node()
	for range names[1:] {
		a.node()
	}
	return first
}

func syntaxNameID(first syntaxID, index int) syntaxID { return first + syntaxID(index) }

func (a *syntaxIDAssigner) statements(statements []statement) {
	for i := range statements {
		a.statement(&statements[i])
	}
}

func (a *syntaxIDAssigner) statement(stmt *statement) {
	stmt.id = a.node()
	switch {
	case stmt.local != nil:
		stmt.local.nameID = a.names(stmt.local.names)
		a.types(stmt.local.annotations)
		a.expressions(stmt.local.values)
	case stmt.localFunc != nil:
		fn := stmt.localFunc
		fn.id, fn.nameID, fn.functionID = a.node(), a.node(), a.function()
		fn.typeParamID, fn.typePackID, fn.paramID = a.names(fn.typeParams), a.names(fn.typePacks), a.names(fn.params)
		a.types(fn.paramAnnotations)
		a.typeExpression(fn.variadicAnnotation)
		a.typeExpression(fn.returnAnnotation)
		a.statements(fn.statements)
	case stmt.funcDecl != nil:
		fn := stmt.funcDecl
		fn.id, fn.functionID = a.node(), a.function()
		a.assignTarget(&fn.target)
		fn.typeParamID, fn.typePackID = a.names(fn.typeParams), a.names(fn.typePacks)
		if fn.method {
			fn.selfID = a.node()
		}
		fn.paramID = a.names(fn.params)
		a.types(fn.paramAnnotations)
		a.typeExpression(fn.variadicAnnotation)
		a.typeExpression(fn.returnAnnotation)
		a.statements(fn.statements)
	case stmt.assign != nil:
		for i := range stmt.assign.targets {
			a.assignTarget(&stmt.assign.targets[i])
		}
		a.expressions(stmt.assign.values)
	case stmt.call != nil:
		a.term(stmt.call)
	case stmt.ifStmt != nil:
		a.expression(&stmt.ifStmt.condition)
		a.statements(stmt.ifStmt.thenStatements)
		a.statements(stmt.ifStmt.elseStatements)
	case stmt.while != nil:
		a.expression(&stmt.while.condition)
		a.statements(stmt.while.statements)
	case stmt.forLoop != nil:
		stmt.forLoop.nameID = a.node()
		a.expression(&stmt.forLoop.start)
		a.expression(&stmt.forLoop.limit)
		if stmt.forLoop.step != nil {
			a.expression(stmt.forLoop.step)
		}
		a.statements(stmt.forLoop.statements)
	case stmt.genericFor != nil:
		stmt.genericFor.nameID = a.names(stmt.genericFor.names)
		a.expressions(stmt.genericFor.values)
		a.statements(stmt.genericFor.statements)
	case stmt.repeat != nil:
		a.statements(stmt.repeat.statements)
		a.expression(&stmt.repeat.condition)
	case stmt.block != nil:
		a.statements(stmt.block.statements)
	case stmt.ret != nil:
		a.expressions(stmt.ret.values)
	case stmt.typeAlias != nil:
		alias := stmt.typeAlias
		alias.id, alias.nameID = a.node(), a.node()
		alias.typeParamID, alias.typePackID = a.names(alias.typeParams), a.names(alias.typePacks)
		a.typeExpression(alias.value)
	}
}

func (a *syntaxIDAssigner) expressions(expressions []expression) {
	for i := range expressions {
		a.expression(&expressions[i])
	}
}

func (a *syntaxIDAssigner) expression(expr *expression) {
	if expr == nil {
		return
	}
	expr.id = a.node()
	for i := range expr.terms {
		for j := range expr.terms[i].terms {
			comparison := &expr.terms[i].terms[j]
			a.concat(&comparison.left)
			if comparison.right != nil {
				a.concat(comparison.right)
			}
		}
	}
}

func (a *syntaxIDAssigner) concat(expr *concatExpression) {
	a.additive(&expr.first)
	for i := range expr.rest {
		a.additive(&expr.rest[i])
	}
}

func (a *syntaxIDAssigner) additive(expr *additiveExpression) {
	a.multiplicative(&expr.first)
	for i := range expr.rest {
		a.multiplicative(&expr.rest[i].value)
	}
}

func (a *syntaxIDAssigner) multiplicative(expr *multiplicativeExpression) {
	a.term(&expr.first)
	for i := range expr.rest {
		a.term(&expr.rest[i].value)
	}
}

func (a *syntaxIDAssigner) term(value *term) {
	if value == nil {
		return
	}
	value.id = a.node()
	if value.power != nil {
		a.term(&value.power.base)
		a.term(&value.power.exponent)
	}
	if value.table != nil {
		for i := range value.table.fields {
			field := &value.table.fields[i]
			if field.key != nil {
				a.expression(field.key)
			}
			a.expression(&field.value)
		}
	}
	if value.function != nil {
		fn := value.function
		fn.id, fn.functionID = a.node(), a.function()
		fn.typeParamID, fn.typePackID = a.names(fn.typeParams), a.names(fn.typePacks)
		fn.paramID = a.names(fn.params)
		a.types(fn.paramAnnotations)
		a.typeExpression(fn.variadicAnnotation)
		a.typeExpression(fn.returnAnnotation)
		a.statements(fn.statements)
	}
	if value.ifExpr != nil {
		a.expression(&value.ifExpr.condition)
		a.expression(&value.ifExpr.thenValue)
		a.expression(&value.ifExpr.elseValue)
	}
	if value.call != nil {
		a.term(&value.call.target)
		a.term(value.call.receiver)
		a.types(value.call.typeArgs)
		a.expressions(value.call.args)
	}
	a.term(value.unaryNot)
	a.term(value.unaryMinus)
	a.term(value.unaryLen)
	a.expression(value.group)
	a.typeExpression(value.cast)
	for i := range value.selectors {
		if value.selectors[i].index != nil {
			a.expression(value.selectors[i].index)
		}
	}
}

func (a *syntaxIDAssigner) assignTarget(target *assignTarget) {
	target.id = a.node()
	for i := range target.selectors {
		if target.selectors[i].index != nil {
			a.expression(target.selectors[i].index)
		}
	}
}

func (a *syntaxIDAssigner) types(values []*typeExpression) {
	for _, value := range values {
		a.typeExpression(value)
	}
}

func (a *syntaxIDAssigner) typeExpression(value *typeExpression) {
	if value == nil {
		return
	}
	value.id = a.node()
	value.typeParamID, value.typePackID = a.names(value.typeParams), a.names(value.typePacks)
	a.types(value.typeArgs)
	a.types(value.types)
	a.typeExpression(value.inner)
	for i := range value.fields {
		a.typeExpression(value.fields[i].key)
		a.typeExpression(value.fields[i].value)
	}
	for i := range value.params {
		a.typeExpression(value.params[i].value)
	}
	a.typeExpression(value.returnType)
	if value.expr != nil {
		a.expression(value.expr)
	}
}
