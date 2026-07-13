package ember

type syntaxID int

type syntaxIDAssigner struct {
	nextNode     syntaxID
	nextFunction int
	limit        uint64
	err          error
	tree         syntaxTree
}

func assignProgramSyntaxIDs(prog *program) {
	_ = assignProgramSyntaxIDsWithLimit(prog, 0)
}

func assignProgramSyntaxIDsWithLimit(prog *program, limit uint64) error {
	if prog == nil {
		return nil
	}
	tree := newSyntaxTree(*prog)
	err := assignSyntaxTreeIDsWithLimit(&tree, limit)
	*prog = tree.root
	return err
}

// assignSyntaxTreeIDsWithLimit assigns IDs through the syntaxTree root seam.
// The concrete pointers returned by the facade are intentionally retained so
// this remains allocation-free while allowing the storage behind syntaxTree to
// change in a later arena slice.
func assignSyntaxTreeIDs(tree *syntaxTree) {
	_ = assignSyntaxTreeIDsWithLimit(tree, 0)
}

func assignSyntaxTreeIDsWithLimit(tree *syntaxTree, limit uint64) error {
	if tree == nil {
		return nil
	}
	a := syntaxIDAssigner{limit: limit, tree: *tree}
	tree.root.id = a.node()
	a.statements(tree.statements())
	tree.root.nodeCount = int(a.nextNode)
	return a.err
}

func (a *syntaxIDAssigner) node() syntaxID {
	if a.err != nil {
		return a.nextNode
	}
	a.nextNode++
	if a.limit != 0 && uint64(a.nextNode) > a.limit && a.err == nil {
		a.err = &LimitError{Kind: LimitSyntaxNodes, Limit: a.limit, Used: uint64(a.nextNode)}
	}
	return a.nextNode
}

func (a *syntaxIDAssigner) function() int {
	if a.err != nil {
		return 0
	}
	a.nextFunction++
	return a.nextFunction
}

func (a *syntaxIDAssigner) names(names []string) syntaxID {
	if a.err != nil {
		return 0
	}
	if len(names) == 0 {
		return 0
	}
	first := a.node()
	for range names[1:] {
		a.node()
		if a.err != nil {
			return 0
		}
	}
	return first
}

func syntaxNameID(first syntaxID, index int) syntaxID { return first + syntaxID(index) }

func (a *syntaxIDAssigner) statements(statements []statement) {
	for i := range statements {
		if a.err != nil {
			return
		}
		a.statement(&statements[i])
	}
}

func (a *syntaxIDAssigner) statement(stmt *statement) {
	if a.err != nil {
		return
	}
	stmt.id = a.node()
	if a.err != nil {
		return
	}
	switch a.tree.statementKind(stmt) {
	case syntaxStatementLocal:
		local := a.tree.local(stmt)
		local.nameID = a.names(a.tree.localNames(local))
		a.types(a.tree.localAnnotations(local))
		a.expressions(a.tree.localValues(local))
	case syntaxStatementLocalFunction:
		fn := a.tree.localFunction(stmt)
		fn.id, fn.nameID, fn.functionID = a.node(), a.node(), a.function()
		fn.typeParamID, fn.typePackID, fn.paramID = a.names(a.tree.localFunctionTypeParams(fn)), a.names(a.tree.localFunctionTypePacks(fn)), a.names(a.tree.localFunctionParams(fn))
		a.types(a.tree.localFunctionParamAnnotations(fn))
		a.typeExpression(a.tree.localFunctionVariadicAnnotation(fn))
		a.typeExpression(a.tree.localFunctionReturnAnnotation(fn))
		a.statements(a.tree.localFunctionStatements(fn))
	case syntaxStatementFunctionDeclaration:
		fn := a.tree.functionDeclaration(stmt)
		fn.id, fn.functionID = a.node(), a.function()
		a.assignTarget(a.tree.functionDeclarationTarget(fn))
		fn.typeParamID, fn.typePackID = a.names(a.tree.functionDeclarationTypeParams(fn)), a.names(a.tree.functionDeclarationTypePacks(fn))
		if a.tree.functionDeclarationMethod(fn) {
			fn.selfID = a.node()
		}
		fn.paramID = a.names(a.tree.functionDeclarationParams(fn))
		a.types(a.tree.functionDeclarationParamAnnotations(fn))
		a.typeExpression(a.tree.functionDeclarationVariadicAnnotation(fn))
		a.typeExpression(a.tree.functionDeclarationReturnAnnotation(fn))
		a.statements(a.tree.functionDeclarationStatements(fn))
	case syntaxStatementAssign:
		assign := a.tree.assignment(stmt)
		for i := range a.tree.assignmentTargets(assign) {
			a.assignTarget(&a.tree.assignmentTargets(assign)[i])
		}
		a.expressions(a.tree.assignmentValues(assign))
	case syntaxStatementCall:
		a.term(a.tree.call(stmt))
	case syntaxStatementIf:
		ifStmt := a.tree.ifStatement(stmt)
		a.expression(a.tree.ifCondition(ifStmt))
		a.statements(a.tree.ifThenStatements(ifStmt))
		a.statements(a.tree.ifElseStatements(ifStmt))
	case syntaxStatementWhile:
		whileStmt := a.tree.whileStatement(stmt)
		a.expression(a.tree.whileCondition(whileStmt))
		a.statements(a.tree.whileStatements(whileStmt))
	case syntaxStatementFor:
		forStmt := a.tree.forStatement(stmt)
		forStmt.nameID = a.node()
		a.expression(a.tree.numericForStart(forStmt))
		a.expression(a.tree.numericForLimit(forStmt))
		if step := a.tree.numericForStep(forStmt); step != nil {
			a.expression(step)
		}
		a.statements(a.tree.numericForStatements(forStmt))
	case syntaxStatementGenericFor:
		forStmt := a.tree.genericForStatement(stmt)
		forStmt.nameID = a.names(a.tree.genericForNames(forStmt))
		a.expressions(a.tree.genericForValues(forStmt))
		a.statements(a.tree.genericForStatements(forStmt))
	case syntaxStatementRepeat:
		repeat := a.tree.repeatStatement(stmt)
		a.statements(a.tree.repeatStatements(repeat))
		a.expression(a.tree.repeatCondition(repeat))
	case syntaxStatementBlock:
		a.statements(a.tree.blockStatements(a.tree.blockStatement(stmt)))
	case syntaxStatementReturn:
		a.expressions(a.tree.returnValues(a.tree.returnStatement(stmt)))
	case syntaxStatementTypeAlias:
		alias := a.tree.typeAliasStatement(stmt)
		alias.id, alias.nameID = a.node(), a.node()
		alias.typeParamID, alias.typePackID = a.names(a.tree.typeAliasTypeParams(alias)), a.names(a.tree.typeAliasTypePacks(alias))
		a.typeExpression(a.tree.typeAliasValue(alias))
	}
}

func (a *syntaxIDAssigner) expressions(expressions []expression) {
	for i := range expressions {
		if a.err != nil {
			return
		}
		a.expression(&expressions[i])
	}
}

func (a *syntaxIDAssigner) expression(expr *expression) {
	if expr == nil {
		return
	}
	expr.id = a.node()
	if a.err != nil {
		return
	}
	for i := range a.tree.expressionTerms(expr) {
		and := &a.tree.expressionTerms(expr)[i]
		for j := range a.tree.andTerms(and) {
			comparison := &a.tree.andTerms(and)[j]
			a.concat(a.tree.comparisonLeftRef(comparison))
			if right := a.tree.comparisonRight(comparison); right != nil {
				a.concat(right)
			}
		}
	}
}

func (a *syntaxIDAssigner) concat(expr *concatExpression) {
	if a.err != nil {
		return
	}
	a.additive(a.tree.concatFirstRef(expr))
	for i := range a.tree.concatRest(expr) {
		a.additive(&a.tree.concatRest(expr)[i])
	}
}

func (a *syntaxIDAssigner) additive(expr *additiveExpression) {
	if a.err != nil {
		return
	}
	a.multiplicative(a.tree.additiveFirstRef(expr))
	for i := range a.tree.additiveRest(expr) {
		a.multiplicative(a.tree.additivePartValue(&a.tree.additiveRest(expr)[i]))
	}
}

func (a *syntaxIDAssigner) multiplicative(expr *multiplicativeExpression) {
	if a.err != nil {
		return
	}
	a.term(a.tree.multiplicativeFirstRef(expr))
	for i := range a.tree.multiplicativeRest(expr) {
		a.term(a.tree.multiplicativePartValue(&a.tree.multiplicativeRest(expr)[i]))
	}
}

func (a *syntaxIDAssigner) term(value *term) {
	if value == nil {
		return
	}
	value.id = a.node()
	if a.err != nil {
		return
	}
	if power := a.tree.termPower(value); power != nil {
		a.term(a.tree.powerBase(power))
		a.term(a.tree.powerExponent(power))
	}
	if table := a.tree.termTable(value); table != nil {
		for i := range a.tree.tableFields(table) {
			field := &a.tree.tableFields(table)[i]
			if key := a.tree.tableFieldKey(field); key != nil {
				a.expression(key)
			}
			a.expression(a.tree.tableFieldValue(field))
		}
	}
	if fn := a.tree.termFunction(value); fn != nil {
		fn.id, fn.functionID = a.node(), a.function()
		fn.typeParamID, fn.typePackID = a.names(a.tree.functionExpressionTypeParams(fn)), a.names(a.tree.functionExpressionTypePacks(fn))
		fn.paramID = a.names(a.tree.functionExpressionParams(fn))
		a.types(a.tree.functionExpressionParamAnnotations(fn))
		a.typeExpression(a.tree.functionExpressionVariadicAnnotation(fn))
		a.typeExpression(a.tree.functionExpressionReturnAnnotation(fn))
		a.statements(a.tree.functionExpressionStatements(fn))
	}
	if ifExpr := a.tree.termIf(value); ifExpr != nil {
		a.expression(a.tree.ifExpressionCondition(ifExpr))
		a.expression(a.tree.ifExpressionThen(ifExpr))
		a.expression(a.tree.ifExpressionElse(ifExpr))
	}
	if call := a.tree.termCall(value); call != nil {
		a.term(a.tree.callTarget(call))
		a.term(a.tree.callReceiver(call))
		a.types(a.tree.callTypeArgs(call))
		a.expressions(a.tree.callArgs(call))
	}
	a.term(a.tree.termUnaryNot(value))
	a.term(a.tree.termUnaryMinus(value))
	a.term(a.tree.termUnaryLength(value))
	a.expression(a.tree.termGroup(value))
	a.typeExpression(a.tree.termCast(value))
	for i := range a.tree.termSelectors(value) {
		if index := a.tree.selectorIndex(&a.tree.termSelectors(value)[i]); index != nil {
			a.expression(index)
		}
	}
}

func (a *syntaxIDAssigner) assignTarget(target *assignTarget) {
	if a.err != nil {
		return
	}
	target.id = a.node()
	if a.err != nil {
		return
	}
	for i := range a.tree.assignTargetSelectors(target) {
		if index := a.tree.selectorIndex(&a.tree.assignTargetSelectors(target)[i]); index != nil {
			a.expression(index)
		}
	}
}

func (a *syntaxIDAssigner) types(values []*typeExpression) {
	for _, value := range values {
		if a.err != nil {
			return
		}
		a.typeExpression(value)
	}
}

func (a *syntaxIDAssigner) typeExpression(value *typeExpression) {
	if value == nil {
		return
	}
	value.id = a.node()
	if a.err != nil {
		return
	}
	value.typeParamID, value.typePackID = a.names(a.tree.typeTypeParams(value)), a.names(a.tree.typePacks(value))
	a.types(a.tree.typeArgs(value))
	a.types(a.tree.typeChildren(value))
	a.typeExpression(a.tree.typeInner(value))
	for i := range a.tree.typeFields(value) {
		field := &a.tree.typeFields(value)[i]
		a.typeExpression(a.tree.typeFieldKey(field))
		a.typeExpression(a.tree.typeFieldValue(field))
	}
	for i := range a.tree.typeParams(value) {
		a.typeExpression(a.tree.typeParamValue(&a.tree.typeParams(value)[i]))
	}
	a.typeExpression(a.tree.typeReturn(value))
	if expr := a.tree.typeExpression(value); expr != nil {
		a.expression(expr)
	}
}
