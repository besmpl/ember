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
// Arena nodes are read by value and written back after their IDs are assigned.
func assignSyntaxTreeIDs(tree *syntaxTree) {
	_ = assignSyntaxTreeIDsWithLimit(tree, 0)
}

func assignSyntaxTreeIDsWithLimit(tree *syntaxTree, limit uint64) error {
	if tree == nil {
		return nil
	}
	a := syntaxIDAssigner{limit: limit, tree: *tree}
	tree.root.id = a.node()
	if ids, ok := tree.statementIDs(); ok && len(ids) > 0 {
		a.arenaStatements(tree.root.statementSpan)
	} else {
		a.statements(tree.statements())
	}
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

func (a *syntaxIDAssigner) arenaNames(span nodeSpan) syntaxID {
	if a.tree.arena == nil {
		return 0
	}
	ids, ok := a.tree.arena.statements.spanStrings(span)
	if !ok || len(ids) == 0 || a.err != nil {
		return 0
	}
	first := a.node()
	for range ids[1:] {
		a.node()
	}
	return first
}

func (a *syntaxIDAssigner) arenaTypes(span nodeSpan) {
	if a.tree.arena == nil {
		return
	}
	ids, ok := a.tree.arena.statements.spanTypes(span)
	if !ok {
		return
	}
	for _, id := range ids {
		a.arenaTypeID(id)
	}
}

func (a *syntaxIDAssigner) arenaTypeID(id typeID) {
	if id == 0 || a.tree.arena == nil || a.err != nil {
		return
	}
	a.typeExpression(id)
}

func (a *syntaxIDAssigner) arenaStatements(span nodeSpan) {
	if a.tree.arena == nil {
		return
	}
	ids, ok := a.tree.arena.statements.spanStatements(span)
	if !ok {
		return
	}
	for _, id := range ids {
		if a.err != nil {
			return
		}
		a.arenaStatement(id)
	}
}

func (a *syntaxIDAssigner) arenaStatement(id statementID) {
	node, ok := a.tree.arena.statements.statement(id)
	if !ok {
		return
	}
	node.id = a.node()
	a.tree.arena.statements.statements[uint64(id)-1] = node
	s := a.tree.arena.statements
	switch node.kind {
	case syntaxStatementLocal:
		payload, valid := arenaNode(s.locals, node.payload)
		if !valid {
			return
		}
		payload.nameID = a.arenaNames(payload.names)
		a.arenaTypes(payload.annotations)
		a.arenaExpressions(payload.values)
		s.locals[node.payload-1] = payload
	case syntaxStatementLocalFunction:
		payload, valid := arenaNode(s.localFuncs, node.payload)
		if !valid {
			return
		}
		a.arenaLocalFunctionStatement(&payload)
		s.localFuncs[node.payload-1] = payload
	case syntaxStatementFunctionDeclaration:
		payload, valid := arenaNode(s.functionDecls, node.payload)
		if !valid {
			return
		}
		a.arenaFunctionDeclarationStatement(&payload)
		s.functionDecls[node.payload-1] = payload
	case syntaxStatementAssign:
		payload, valid := arenaNode(s.assigns, node.payload)
		if !valid {
			return
		}
		if targets, ok := s.spanAssignTargets(payload.targets); ok {
			for _, target := range targets {
				a.arenaAssignTarget(target)
			}
		}
		a.arenaExpressions(payload.values)
	case syntaxStatementCall:
		a.term(termID(node.payload))
	case syntaxStatementIf:
		payload, valid := arenaNode(s.ifStatements, node.payload)
		if !valid {
			return
		}
		a.expression(payload.condition)
		a.arenaStatements(payload.thenStatements)
		a.arenaStatements(payload.elseStatements)
	case syntaxStatementWhile:
		payload, valid := arenaNode(s.whileStatements, node.payload)
		if !valid {
			return
		}
		a.expression(payload.condition)
		a.arenaStatements(payload.statements)
	case syntaxStatementFor:
		payload, valid := arenaNode(s.forStatements, node.payload)
		if !valid {
			return
		}
		payload.nameID = a.node()
		a.expression(payload.start)
		a.expression(payload.limit)
		a.expression(payload.step)
		a.arenaStatements(payload.statements)
		s.forStatements[node.payload-1] = payload
	case syntaxStatementGenericFor:
		payload, valid := arenaNode(s.genericForStatements, node.payload)
		if !valid {
			return
		}
		payload.nameID = a.arenaNames(payload.names)
		a.arenaExpressions(payload.values)
		a.arenaStatements(payload.statements)
		s.genericForStatements[node.payload-1] = payload
	case syntaxStatementRepeat:
		payload, valid := arenaNode(s.repeatStatements, node.payload)
		if !valid {
			return
		}
		a.arenaStatements(payload.statements)
		a.expression(payload.condition)
	case syntaxStatementBlock:
		payload, valid := arenaNode(s.blockStatements, node.payload)
		if !valid {
			return
		}
		a.arenaStatements(payload.statements)
	case syntaxStatementReturn:
		payload, valid := arenaNode(s.returnStatements, node.payload)
		if !valid {
			return
		}
		a.arenaExpressions(payload.values)
	case syntaxStatementTypeAlias:
		payload, valid := arenaNode(s.typeAliases, node.payload)
		if !valid {
			return
		}
		payload.id, payload.nameID = a.node(), a.node()
		payload.typeParamID, payload.typePackID = a.arenaNames(payload.typeParams), a.arenaNames(payload.typePacks)
		a.arenaTypeID(payload.value)
		s.typeAliases[node.payload-1] = payload
	}
}

func (a *syntaxIDAssigner) arenaFunctionStatement(fn *arenaFunctionStatement) {
	fn.id, fn.nameID, fn.functionID = a.node(), a.node(), a.function()
	fn.typeParamID, fn.typePackID, fn.paramID = a.arenaNames(fn.typeParams), a.arenaNames(fn.typePacks), a.arenaNames(fn.params)
	if fn.method {
		fn.selfID = a.node()
	}
	a.arenaTypes(fn.paramAnnotations)
	a.arenaTypeID(fn.variadicAnnotation)
	a.arenaTypeID(fn.returnAnnotation)
	a.arenaStatements(fn.statements)
}

func (a *syntaxIDAssigner) arenaLocalFunctionStatement(fn *arenaFunctionStatement) {
	a.arenaFunctionStatement(fn)
}

func (a *syntaxIDAssigner) arenaFunctionDeclarationStatement(fn *arenaFunctionStatement) {
	fn.id, fn.functionID = a.node(), a.function()
	if fn.target != 0 {
		a.arenaAssignTarget(fn.target)
	}
	fn.typeParamID, fn.typePackID = a.arenaNames(fn.typeParams), a.arenaNames(fn.typePacks)
	if fn.method {
		fn.selfID = a.node()
	}
	fn.paramID = a.arenaNames(fn.params)
	a.arenaTypes(fn.paramAnnotations)
	a.arenaTypeID(fn.variadicAnnotation)
	a.arenaTypeID(fn.returnAnnotation)
	a.arenaStatements(fn.statements)
}

func (a *syntaxIDAssigner) arenaAssignTarget(id assignTargetID) {
	target, ok := a.tree.arena.statements.assignTarget(id)
	if !ok {
		return
	}
	target.id = a.node()
	a.tree.arena.statements.assignTargets[uint64(id)-1] = target
	selectors, ok := a.tree.arena.selectorIDs(target.selectors)
	if !ok {
		return
	}
	for _, selector := range selectors {
		a.expression(selector.index)
	}
}

func (a *syntaxIDAssigner) arenaExpressions(span nodeSpan) {
	values, ok := a.tree.arena.statements.spanExpressions(span)
	if !ok {
		return
	}
	a.expressions(values)
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
		if step := a.tree.numericForStep(forStmt); step != 0 {
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

func (a *syntaxIDAssigner) expressions(values []expressionID) {
	for _, value := range values {
		a.expression(value)
	}
}
func (a *syntaxIDAssigner) expression(id expressionID) {
	if id == 0 || a.err != nil {
		return
	}
	node, ok := a.tree.arenaExpression(id)
	if !ok {
		return
	}
	node.id = a.node()
	a.tree.arena.expressions[id-1] = node
	terms, ok := a.tree.arenaExpressionTerms(node.terms)
	if !ok {
		return
	}
	for _, andID := range terms {
		a.and(andID)
	}
}
func (a *syntaxIDAssigner) and(id andExpressionID) {
	node, ok := a.tree.arenaAnd(id)
	if !ok {
		return
	}
	values, ok := a.tree.arenaAndTerms(node.terms)
	if !ok {
		return
	}
	for _, value := range values {
		a.comparison(value)
	}
}
func (a *syntaxIDAssigner) comparison(id comparisonExpressionID) {
	node, ok := a.tree.arenaComparison(id)
	if !ok {
		return
	}
	a.concat(node.left)
	a.concat(node.right)
}
func (a *syntaxIDAssigner) concat(id concatExpressionID) {
	node, ok := a.tree.arenaConcat(id)
	if !ok {
		return
	}
	a.additive(node.first)
	values, ok := a.tree.arenaConcatRest(node.rest)
	if !ok {
		return
	}
	for _, value := range values {
		a.additive(value)
	}
}
func (a *syntaxIDAssigner) additive(id additiveExpressionID) {
	node, ok := a.tree.arenaAdditive(id)
	if !ok {
		return
	}
	a.multiplicative(node.first)
	values, ok := a.tree.arenaAdditiveRest(node.rest)
	if !ok {
		return
	}
	for _, value := range values {
		a.multiplicative(value.value)
	}
}
func (a *syntaxIDAssigner) multiplicative(id multiplicativeExpressionID) {
	node, ok := a.tree.arenaMultiplicative(id)
	if !ok {
		return
	}
	a.term(node.first)
	values, ok := a.tree.arenaMultiplicativeRest(node.rest)
	if !ok {
		return
	}
	for _, value := range values {
		a.term(value.value)
	}
}
func (a *syntaxIDAssigner) term(id termID) {
	if id == 0 || a.err != nil {
		return
	}
	node, ok := a.tree.arenaTerm(id)
	if !ok {
		return
	}
	node.id = a.node()
	a.tree.arena.terms[id-1] = node
	switch node.kind {
	case termKindPower:
		power, ok := a.tree.arena.power(arenaPowerID(node.payload))
		if ok {
			a.term(power.base)
			a.term(power.exponent)
		}
	case termKindTable:
		table, ok := a.tree.arena.table(arenaTableID(node.payload))
		if ok {
			fields, _ := a.tree.arena.tableFieldsIDs(table.fields)
			for _, field := range fields {
				a.expression(field.key)
				a.expression(field.value)
			}
		}
	case termKindFunction:
		fn, ok := a.tree.arena.function(arenaFunctionID(node.payload))
		if ok {
			fn.id, fn.functionID = a.node(), a.function()
			fn.typeParamID, fn.typePackID, fn.paramID = a.arenaNames(fn.typeParams), a.arenaNames(fn.typePacks), a.arenaNames(fn.params)
			a.arenaTypes(fn.paramAnnotations)
			a.arenaTypeID(fn.variadicAnnotation)
			a.arenaTypeID(fn.returnAnnotation)
			a.arenaStatements(fn.statements)
			a.tree.arena.functions[arenaFunctionID(node.payload)-1] = fn
		}
	case termKindIf:
		value, ok := a.tree.arena.ifExpression(arenaIfExpressionID(node.payload))
		if ok {
			a.expression(value.condition)
			a.expression(value.thenValue)
			a.expression(value.elseValue)
		}
	case termKindCall:
		call, ok := a.tree.arena.call(arenaCallID(node.payload))
		if ok {
			a.term(call.target)
			a.term(call.receiver)
			typeArgs, _ := a.tree.arena.types.spanTypeIDs(call.typeArgs)
			a.types(typeArgs)
			args, _ := a.tree.arena.callArgIDs(call.args)
			a.expressions(args)
		}
	case termKindUnaryNot, termKindUnaryMinus, termKindUnaryLength:
		a.term(termID(node.payload))
	case termKindGroup:
		a.expression(expressionID(node.payload))
	}
	if node.castType != 0 {
		a.typeExpression(node.castType)
	}
	selectors, _ := a.tree.arena.selectorIDs(node.selectors)
	for _, selector := range selectors {
		a.expression(selector.index)
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
		if index := a.tree.selectorIndex(&a.tree.assignTargetSelectors(target)[i]); index != 0 {
			a.expression(index)
		}
	}
}

func (a *syntaxIDAssigner) types(values []typeID) {
	for _, value := range values {
		if a.err != nil {
			return
		}
		a.typeExpression(value)
	}
}

func (a *syntaxIDAssigner) typeExpression(value typeID) {
	if value == 0 || a.tree.arena == nil || a.err != nil {
		return
	}
	node, ok := a.tree.arena.types.node(value)
	if !ok {
		return
	}
	node.id = a.node()
	a.tree.arena.types.setNode(value, node)
	if a.err != nil {
		return
	}
	if fn, ok := a.tree.typeFunctionNode(value); ok {
		paramIDs, paramsOK := a.tree.arena.types.spanStringIDs(fn.typeParams)
		packIDs, packsOK := a.tree.arena.types.spanStringIDs(fn.typePacks)
		if paramsOK {
			fn.typeParamID = a.rawNames(paramIDs)
		}
		if packsOK {
			fn.typePackID = a.rawNames(packIDs)
		}
		a.tree.arena.types.functions[arenaFunctionTypeID(node.payload)-1] = fn
	}
	a.types(a.tree.typeArgs(value))
	a.types(a.tree.typeChildren(value))
	if inner, ok := a.tree.typeInner(value); ok {
		a.typeExpression(inner)
	}
	for _, field := range a.tree.typeFields(value) {
		a.typeExpression(a.tree.typeFieldKey(field))
		a.typeExpression(a.tree.typeFieldValue(field))
	}
	for _, param := range a.tree.typeParams(value) {
		a.typeExpression(a.tree.typeParamValue(param))
	}
	if result, ok := a.tree.typeReturn(value); ok {
		a.typeExpression(result)
	}
	if expr, ok := a.tree.typeExpression(value); ok {
		a.expression(expr)
	}
}

func (a *syntaxIDAssigner) rawNames(ids []stringID) syntaxID {
	if len(ids) == 0 || a.err != nil {
		return 0
	}
	first := a.node()
	for range ids[1:] {
		a.node()
	}
	return first
}
