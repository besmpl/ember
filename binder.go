package ember

type symbolKind uint8

const (
	symbolInvalid symbolKind = iota
	symbolLocal
	symbolLocalFunction
	symbolParameter
	symbolTypeAlias
	symbolTypeParameter
	symbolTypePack
)

func (kind symbolKind) String() string {
	switch kind {
	case symbolLocal:
		return "local"
	case symbolLocalFunction:
		return "localFunction"
	case symbolParameter:
		return "parameter"
	case symbolTypeAlias:
		return "typeAlias"
	case symbolTypeParameter:
		return "typeParameter"
	case symbolTypePack:
		return "typePack"
	default:
		return "invalid"
	}
}

type symbolNamespace uint8

const (
	valueNamespace symbolNamespace = iota
	typeNamespace
)

func (kind symbolKind) namespace() symbolNamespace {
	switch kind {
	case symbolTypeAlias, symbolTypeParameter, symbolTypePack:
		return typeNamespace
	default:
		return valueNamespace
	}
}

type boundSymbol struct {
	id       int
	node     syntaxID
	name     string
	kind     symbolKind
	scope    int
	funcID   int
	shadowed int
	facts    boundSymbolFacts
}

type boundUse struct {
	symbol   int
	captured bool
}

// boundUseClassification is stored directly in boundNodeFacts.use. A
// nonnegative value is a bound symbol id; negative values distinguish an
// identifier the binder has not visited from a valid unresolved global.
type boundUseClassification int32

const (
	boundUseUnvisited boundUseClassification = -1
	boundUseGlobal    boundUseClassification = -2
)

type boundNodeFlags uint8

const (
	boundNodeUseValid boundNodeFlags = 1 << iota
	boundNodeCaptured
	boundNodeExpressionValid
	boundNodeMultiret
)

type bindScope struct {
	parent          int
	funcID          int
	symbolStart     int
	symbolCount     int
	capturedSymbols []int32
}

type boundSymbolFacts struct {
	assigned              bool
	captured              bool
	mutatedAfterCapture   bool
	immutableCopyEligible bool
}

type boundExpressionFact struct {
	valid    bool
	arity    int
	multiret bool
}

type boundNodeFacts struct {
	definition      int32
	use             int32
	expressionArity int32
	flags           boundNodeFlags
}

type bindResult struct {
	scopes       []bindScope
	scopeSymbols []int32
	symbols      []boundSymbol
	nodeFacts    []boundNodeFacts
}

func (r bindResult) definition(node syntaxID) (boundSymbol, bool) {
	if node <= 0 || int(node) >= len(r.nodeFacts) {
		return boundSymbol{}, false
	}
	symbolID := int(r.nodeFacts[node].definition)
	if symbolID < 0 || symbolID >= len(r.symbols) {
		return boundSymbol{}, false
	}
	return r.symbols[symbolID], true
}

func (r bindResult) use(node syntaxID) (boundUse, bool) {
	if node <= 0 || int(node) >= len(r.nodeFacts) {
		return boundUse{}, false
	}
	facts := r.nodeFacts[node]
	use := boundUse{
		symbol:   int(facts.use),
		captured: facts.flags&boundNodeCaptured != 0,
	}
	return use, facts.flags&boundNodeUseValid != 0 && facts.use >= 0
}

func (r bindResult) useClassification(node syntaxID) boundUseClassification {
	if node <= 0 || int(node) >= len(r.nodeFacts) {
		return boundUseUnvisited
	}
	facts := r.nodeFacts[node]
	if facts.flags&boundNodeUseValid == 0 {
		return boundUseUnvisited
	}
	classification := boundUseClassification(facts.use)
	if classification >= 0 && int(classification) >= len(r.symbols) {
		return boundUseUnvisited
	}
	if classification < 0 && classification != boundUseGlobal {
		return boundUseUnvisited
	}
	return classification
}

func (r bindResult) expressionFact(node syntaxID) (boundExpressionFact, bool) {
	if node <= 0 || int(node) >= len(r.nodeFacts) {
		return boundExpressionFact{}, false
	}
	facts := r.nodeFacts[node]
	fact := boundExpressionFact{
		valid:    facts.flags&boundNodeExpressionValid != 0,
		arity:    int(facts.expressionArity),
		multiret: facts.flags&boundNodeMultiret != 0,
	}
	return fact, fact.valid
}

type binder struct {
	tree             syntaxTree
	result           bindResult
	scopes           []int
	scopeLastSymbols []int32
	symbolPrevious   []int32
	captureSets      []map[int]struct{}
	activeValueNames map[string]int
	activeTypeNames  map[string]int
}

func bindSyntaxTree(tree syntaxTree) bindResult {
	if tree.nodeCount() == 0 {
		assignSyntaxTreeIDs(&tree)
	}
	nodeFacts := make([]boundNodeFacts, tree.nodeCount()+1)
	for i := range nodeFacts {
		nodeFacts[i].definition = -1
		nodeFacts[i].use = int32(boundUseUnvisited)
	}
	b := binder{
		tree: tree,
		result: bindResult{
			nodeFacts: nodeFacts,
		},
		activeValueNames: make(map[string]int),
		activeTypeNames:  make(map[string]int),
	}
	b.pushScopeForFunction(0)
	statements, _ := tree.statementIDs()
	b.bindStatements(statements)
	b.popScope()
	for i := range b.result.symbols {
		facts := &b.result.symbols[i].facts
		facts.immutableCopyEligible = facts.captured && !facts.mutatedAfterCapture
	}
	return b.result
}

func (b *binder) bindStatements(statements []statementID) {
	for _, stmt := range statements {
		b.bindStatement(stmt)
	}
}

func (b *binder) bindStatement(stmt statementID) {
	node, ok := b.tree.statementNode(stmt)
	if !ok {
		return
	}
	switch node.kind {
	case syntaxStatementLocal:
		local, ok := b.tree.localArena(stmt)
		if !ok {
			return
		}
		annotations, _ := b.tree.statementTypes(local.annotations)
		for _, annotation := range annotations {
			b.bindTypeExpression(annotation)
		}
		values, _ := b.tree.statementExpressions(local.values)
		for _, value := range values {
			b.bindExpression(value)
		}
		names, _ := b.tree.statementStrings(local.names)
		for i, nameID := range names {
			b.defineArenaName(nameID, symbolLocal, syntaxNameID(local.nameID, i))
		}
	case syntaxStatementLocalFunction:
		localFunc, ok := b.tree.localFunctionArena(stmt)
		if !ok {
			return
		}
		b.defineArenaName(localFunc.name, symbolLocalFunction, localFunc.nameID)
		b.bindFunctionStatement(localFunc)
	case syntaxStatementFunctionDeclaration:
		funcDecl, ok := b.tree.functionDeclarationArena(stmt)
		if !ok {
			return
		}
		b.bindAssignTargetID(funcDecl.target, true)
		b.bindFunctionStatement(funcDecl)
	case syntaxStatementAssign:
		assign, ok := b.tree.assignmentArena(stmt)
		if !ok {
			return
		}
		values, _ := b.tree.statementExpressions(assign.values)
		for _, value := range values {
			b.bindExpression(value)
		}
		targets, _ := b.tree.statementTargets(assign.targets)
		for _, target := range targets {
			b.bindAssignTargetID(target, true)
		}
	case syntaxStatementCall:
		b.bindTerm(termID(node.payload))
	case syntaxStatementIf:
		ifStmt, ok := b.tree.ifArena(stmt)
		if !ok {
			return
		}
		b.bindExpression(ifStmt.condition)
		thenBody, _ := b.tree.statementChildren(ifStmt.thenStatements)
		elseBody, _ := b.tree.statementChildren(ifStmt.elseStatements)
		b.bindScoped(thenBody)
		b.bindScoped(elseBody)
	case syntaxStatementWhile:
		while, ok := b.tree.whileArena(stmt)
		if !ok {
			return
		}
		b.bindExpression(while.condition)
		body, _ := b.tree.statementChildren(while.statements)
		b.bindScoped(body)
	case syntaxStatementFor:
		forLoop, ok := b.tree.forArena(stmt)
		if !ok {
			return
		}
		b.bindExpression(forLoop.start)
		b.bindExpression(forLoop.limit)
		if forLoop.step != 0 {
			b.bindExpression(forLoop.step)
		}
		b.pushScope()
		b.defineArenaName(forLoop.name, symbolLocal, forLoop.nameID)
		body, _ := b.tree.statementChildren(forLoop.statements)
		b.bindStatements(body)
		b.popScope()
	case syntaxStatementGenericFor:
		genericFor, ok := b.tree.genericForArena(stmt)
		if !ok {
			return
		}
		values, _ := b.tree.statementExpressions(genericFor.values)
		for _, value := range values {
			b.bindExpression(value)
		}
		b.pushScope()
		names, _ := b.tree.statementStrings(genericFor.names)
		for i, nameID := range names {
			b.defineArenaName(nameID, symbolLocal, syntaxNameID(genericFor.nameID, i))
		}
		body, _ := b.tree.statementChildren(genericFor.statements)
		b.bindStatements(body)
		b.popScope()
	case syntaxStatementRepeat:
		repeat, ok := b.tree.repeatArena(stmt)
		if !ok {
			return
		}
		b.pushScope()
		body, _ := b.tree.statementChildren(repeat.statements)
		b.bindStatements(body)
		b.bindExpression(repeat.condition)
		b.popScope()
	case syntaxStatementBlock:
		block, ok := b.tree.blockArena(stmt)
		if !ok {
			return
		}
		body, _ := b.tree.statementChildren(block.statements)
		b.bindScoped(body)
	case syntaxStatementReturn:
		ret, ok := b.tree.returnArena(stmt)
		if !ok {
			return
		}
		values, _ := b.tree.statementExpressions(ret.values)
		for _, value := range values {
			b.bindExpression(value)
		}
	case syntaxStatementTypeAlias:
		typeAlias, ok := b.tree.typeAliasArena(stmt)
		if !ok {
			return
		}
		b.defineArenaName(typeAlias.name, symbolTypeAlias, typeAlias.nameID)
		b.pushScope()
		typeParams, _ := b.tree.statementStrings(typeAlias.typeParams)
		for i, nameID := range typeParams {
			b.defineArenaName(nameID, symbolTypeParameter, syntaxNameID(typeAlias.typeParamID, i))
		}
		typePacks, _ := b.tree.statementStrings(typeAlias.typePacks)
		for i, nameID := range typePacks {
			b.defineArenaName(nameID, symbolTypePack, syntaxNameID(typeAlias.typePackID, i))
		}
		b.bindTypeExpression(typeAlias.value)
		b.popScope()
	}
}

func (b *binder) bindFunctionParts(functionID int, typeParams nodeSpan, typeParamID syntaxID, typePacks nodeSpan, typePackID syntaxID, params nodeSpan, paramID syntaxID, selfID syntaxID, paramAnnotations nodeSpan, variadic bool, variadicAnnotation typeID, returnAnnotation typeID, statements nodeSpan) {
	b.pushScopeForFunction(functionID)
	typeParamIDs, _ := b.tree.statementStrings(typeParams)
	for i, nameID := range typeParamIDs {
		b.defineArenaName(nameID, symbolTypeParameter, syntaxNameID(typeParamID, i))
	}
	typePackIDs, _ := b.tree.statementStrings(typePacks)
	for i, nameID := range typePackIDs {
		b.defineArenaName(nameID, symbolTypePack, syntaxNameID(typePackID, i))
	}
	annotations, _ := b.tree.statementTypes(paramAnnotations)
	for _, annotationID := range annotations {
		b.bindTypeExpression(annotationID)
	}
	b.bindTypeExpression(variadicAnnotation)
	b.bindTypeExpression(returnAnnotation)
	if selfID != 0 {
		b.define("self", symbolParameter, selfID)
	}
	paramIDs, _ := b.tree.statementStrings(params)
	for i, nameID := range paramIDs {
		b.defineArenaName(nameID, symbolParameter, syntaxNameID(paramID, i))
	}
	body, _ := b.tree.statementChildren(statements)
	b.bindStatements(body)
	b.popScope()
}

func (b *binder) bindFunctionStatement(fn arenaFunctionStatement) {
	b.bindFunctionParts(fn.functionID, fn.typeParams, fn.typeParamID, fn.typePacks, fn.typePackID, fn.params, fn.paramID, fn.selfID, fn.paramAnnotations, fn.variadic, fn.variadicAnnotation, fn.returnAnnotation, fn.statements)
}

func (b *binder) bindFunctionExpression(fn arenaFunction) {
	b.bindFunctionParts(fn.functionID, fn.typeParams, fn.typeParamID, fn.typePacks, fn.typePackID, fn.params, fn.paramID, 0, fn.paramAnnotations, fn.variadic, fn.variadicAnnotation, fn.returnAnnotation, fn.statements)
}

func (b *binder) bindExpression(expr expressionID) {
	if expr > 0 {
		multiret := expressionExpands(b.tree, expr)
		arity := 1
		if multiret {
			arity = -1
		}
		facts := &b.result.nodeFacts[b.tree.expressionSyntaxID(expr)]
		facts.expressionArity = int32(arity)
		facts.flags |= boundNodeExpressionValid
		if multiret {
			facts.flags |= boundNodeMultiret
		}
	}
	terms, _ := b.tree.expressionTerms(expr)
	for _, and := range terms {
		comparisons, _ := b.tree.andTerms(and)
		for _, comparison := range comparisons {
			b.bindConcatExpression(b.tree.comparisonLeft(comparison))
			if right := b.tree.comparisonRight(comparison); right != 0 {
				b.bindConcatExpression(right)
			}
		}
	}
}

func (b *binder) bindConcatExpression(expr concatExpressionID) {
	b.bindAdditiveExpression(b.tree.concatFirst(expr))
	parts, _ := b.tree.concatRest(expr)
	for _, part := range parts {
		b.bindAdditiveExpression(part)
	}
}

func (b *binder) bindAdditiveExpression(expr additiveExpressionID) {
	b.bindMultiplicativeExpression(b.tree.additiveFirst(expr))
	parts, _ := b.tree.additiveRest(expr)
	for _, part := range parts {
		b.bindMultiplicativeExpression(part.value)
	}
}

func (b *binder) bindMultiplicativeExpression(expr multiplicativeExpressionID) {
	b.bindTerm(b.tree.multiplicativeFirst(expr))
	parts, _ := b.tree.multiplicativeRest(expr)
	for _, part := range parts {
		b.bindTerm(part.value)
	}
}

func (b *binder) bindTerm(value termID) {
	if power, ok := b.tree.termPower(value); ok {
		b.bindTerm(b.tree.powerBase(power))
		b.bindTerm(b.tree.powerExponent(power))
	}
	if table, ok := b.tree.termTable(value); ok {
		fields, _ := b.tree.tableFields(table)
		for _, field := range fields {
			if key := b.tree.tableFieldKey(field); key != 0 {
				b.bindExpression(key)
			}
			b.bindExpression(b.tree.tableFieldValue(field))
		}
	}
	if function, ok := b.tree.termFunction(value); ok {
		if b.tree.arena != nil {
			if fn, valid := b.tree.arena.function(function); valid {
				b.bindFunctionExpression(fn)
			}
		}
	}
	if ifExpr, ok := b.tree.termIf(value); ok {
		b.bindExpression(b.tree.ifExpressionCondition(ifExpr))
		b.bindExpression(b.tree.ifExpressionThen(ifExpr))
		b.bindExpression(b.tree.ifExpressionElse(ifExpr))
	}
	if call, ok := b.tree.termCall(value); ok {
		b.bindCall(call)
	}
	if child, ok := b.tree.termChild(value); ok {
		b.bindTerm(child)
	}
	if group, ok := b.tree.termGroup(value); ok {
		b.bindExpression(group)
	}
	if cast, ok := b.tree.termCast(value); ok {
		b.bindTypeExpression(cast)
	}
	if name := b.tree.termName(value); name != "" {
		b.recordUse(b.tree.termSyntaxID(value), name, valueNamespace)
	}
	selectors, _ := b.tree.termSelectors(value)
	for _, selector := range selectors {
		if index := selector.index; index != 0 {
			b.bindExpression(index)
		}
	}
}

func (b *binder) bindCall(call arenaCallID) {
	b.bindTerm(b.tree.callTarget(call))
	if receiver := b.tree.callReceiver(call); receiver != 0 {
		b.bindTerm(receiver)
	}
	for _, arg := range b.tree.callTypeArgs(call) {
		b.bindTypeExpression(arg)
	}
	args, _ := b.tree.callArgs(call)
	for _, arg := range args {
		b.bindExpression(arg)
	}
}

func (b *binder) bindTypeExpression(value typeID) {
	if value == 0 {
		return
	}
	switch b.tree.typeKind(value) {
	case typeKindName:
		nameIDs, ok := b.tree.typeNameIDs(value)
		if !ok || len(nameIDs) == 0 {
			return
		}
		name, ok := b.tree.stringValue(nameIDs[0])
		if !ok || name == "" {
			return
		}
		namespace := typeNamespace
		// Qualified module types resolve their root through the value
		// namespace (for example, Types.Count); the exported member is
		// checked against the module summary rather than local type facts.
		if len(nameIDs) > 1 {
			namespace = valueNamespace
		}
		b.recordUse(b.tree.typeID(value), name, namespace)
		args, ok := b.tree.typeArgIDs(value)
		if !ok {
			return
		}
		for _, arg := range args {
			b.bindTypeExpression(arg)
		}
	case typeKindGenericPack:
		inner, ok := b.tree.typeInner(value)
		if ok {
			b.bindTypeExpression(inner)
		}
	case typeKindUnion, typeKindIntersection:
		options, ok := b.tree.typeChildIDs(value)
		if !ok {
			return
		}
		for _, option := range options {
			b.bindTypeExpression(option)
		}
	case typeKindNilable, typeKindVariadic:
		inner, ok := b.tree.typeInner(value)
		if ok {
			b.bindTypeExpression(inner)
		}
	case typeKindTable:
		fields, ok := b.tree.typeFieldSpan(value)
		if !ok {
			return
		}
		fieldValues, ok := b.tree.arena.types.spanFields(fields)
		if !ok {
			return
		}
		for _, field := range fieldValues {
			b.bindTypeExpression(field.key)
			b.bindTypeExpression(field.value)
		}
	case typeKindFunction:
		b.bindTypeFunction(value)
	case typeKindGenericFunction:
		b.pushScope()
		paramIDs, ok := b.tree.typeTypeParamIDs(value)
		if !ok {
			b.popScope()
			return
		}
		for i, nameID := range paramIDs {
			name, ok := b.tree.stringValue(nameID)
			if !ok {
				continue
			}
			b.define(name, symbolTypeParameter, syntaxNameID(b.tree.typeParamID(value), i))
		}
		packIDs, ok := b.tree.typePackIDs(value)
		if !ok {
			b.popScope()
			return
		}
		for i, nameID := range packIDs {
			name, ok := b.tree.stringValue(nameID)
			if !ok {
				continue
			}
			b.define(name, symbolTypePack, syntaxNameID(b.tree.typePackID(value), i))
		}
		b.bindTypeFunction(value)
		b.popScope()
	case typeKindTypeof:
		if expr, ok := b.tree.typeExpression(value); ok {
			b.bindExpression(expr)
		}
	}
}

func (b *binder) bindTypeFunction(value typeID) {
	params, ok := b.tree.typeParamSpan(value)
	if !ok {
		return
	}
	paramValues, ok := b.tree.arena.types.spanParams(params)
	if !ok {
		return
	}
	for _, param := range paramValues {
		b.bindTypeExpression(param.value)
	}
	if returnType, ok := b.tree.typeReturn(value); ok {
		b.bindTypeExpression(returnType)
	}
}

func (b *binder) bindAssignTargetID(targetID assignTargetID, assignment bool) {
	target, ok := b.tree.statementArenaTarget(targetID)
	if !ok {
		return
	}
	name, ok := resolveArenaName(b.tree, target.name)
	if !ok {
		return
	}
	b.recordUse(target.id, name, valueNamespace)
	selectors, _ := b.tree.selectorSpan(target.selectors)
	if assignment && len(selectors) == 0 {
		if use, ok := b.result.use(syntaxID(target.id)); ok {
			facts := &b.result.symbols[use.symbol].facts
			facts.assigned = true
			if facts.captured {
				facts.mutatedAfterCapture = true
			}
		}
	}
	for _, selector := range selectors {
		if index := selector.index; index != 0 {
			b.bindExpression(index)
		}
	}
}

func (b *binder) defineArenaName(nameID stringID, kind symbolKind, node syntaxID) {
	name, ok := resolveArenaName(b.tree, nameID)
	if !ok {
		return
	}
	b.define(name, kind, node)
}

func (b *binder) bindScoped(statements []statementID) {
	b.pushScope()
	b.bindStatements(statements)
	b.popScope()
}

func (b *binder) define(name string, kind symbolKind, node syntaxID) boundSymbol {
	scope := b.currentScope()
	symbol := boundSymbol{
		id:       len(b.result.symbols),
		node:     node,
		name:     name,
		kind:     kind,
		scope:    scope,
		funcID:   b.currentFunction(),
		shadowed: -1,
	}
	if shadowed, ok := b.lookup(name, kind.namespace()); ok {
		symbol.shadowed = shadowed.id
	}
	b.result.symbols = append(b.result.symbols, symbol)
	b.symbolPrevious = append(b.symbolPrevious, b.scopeLastSymbols[scope])
	if node > 0 {
		b.result.nodeFacts[node].definition = int32(symbol.id)
	}
	b.scopeLastSymbols[scope] = int32(symbol.id)
	b.result.scopes[scope].symbolCount++
	b.activeNames(kind.namespace())[name] = symbol.id
	return symbol
}

func (b *binder) recordUse(node syntaxID, name string, namespace symbolNamespace) {
	if node <= 0 || int(node) >= len(b.result.nodeFacts) {
		return
	}
	facts := &b.result.nodeFacts[node]
	symbol, ok := b.lookup(name, namespace)
	if !ok {
		facts.use = int32(boundUseGlobal)
		facts.flags |= boundNodeUseValid
		return
	}
	captured := symbol.funcID != b.currentFunction()
	facts.use = int32(symbol.id)
	facts.flags |= boundNodeUseValid
	if captured {
		facts.flags |= boundNodeCaptured
	}
	if captured {
		b.capture(symbol.id, b.currentScope())
	}
}

func (b *binder) capture(symbolID int, scope int) {
	facts := &b.result.symbols[symbolID].facts
	facts.captured = true
	capturedSymbols := &b.result.scopes[scope].capturedSymbols
	capturedSet := b.captureSets[scope]
	if capturedSet != nil {
		if _, ok := capturedSet[symbolID]; ok {
			return
		}
	} else {
		for _, captured := range *capturedSymbols {
			if captured == int32(symbolID) {
				return
			}
		}
	}
	if len(*capturedSymbols) >= 8 && capturedSet == nil {
		capturedSet = make(map[int]struct{}, len(*capturedSymbols)+1)
		for _, captured := range *capturedSymbols {
			capturedSet[int(captured)] = struct{}{}
		}
		b.captureSets[scope] = capturedSet
	}
	*capturedSymbols = append(*capturedSymbols, int32(symbolID))
	if capturedSet != nil {
		capturedSet[symbolID] = struct{}{}
	}
}

func (b *binder) activeNames(namespace symbolNamespace) map[string]int {
	if namespace == typeNamespace {
		return b.activeTypeNames
	}
	return b.activeValueNames
}

func (b *binder) lookup(name string, namespace symbolNamespace) (boundSymbol, bool) {
	if symbolID, ok := b.activeNames(namespace)[name]; ok {
		return b.result.symbols[symbolID], true
	}
	return boundSymbol{}, false
}

func (b *binder) pushScope() int {
	return b.pushScopeForFunction(b.currentFunction())
}

func (b *binder) pushScopeForFunction(funcID int) int {
	parent := -1
	if len(b.scopes) > 0 {
		parent = b.scopes[len(b.scopes)-1]
	}
	scopeID := len(b.result.scopes)
	scope := bindScope{
		parent: parent,
		funcID: funcID,
	}
	b.result.scopes = append(b.result.scopes, scope)
	b.scopeLastSymbols = append(b.scopeLastSymbols, -1)
	b.captureSets = append(b.captureSets, nil)
	b.scopes = append(b.scopes, scopeID)
	return scopeID
}

func (b *binder) popScope() {
	scopeID := b.currentScope()
	scope := &b.result.scopes[scopeID]
	if scope.symbolCount > 0 {
		scope.symbolStart = len(b.result.scopeSymbols)
		for symbolID := int(b.scopeLastSymbols[scopeID]); symbolID >= 0; symbolID = int(b.symbolPrevious[symbolID]) {
			b.result.scopeSymbols = append(b.result.scopeSymbols, int32(symbolID))
		}
		b.scopeLastSymbols[scopeID] = -1
	}
	for i := scope.symbolStart; i < scope.symbolStart+scope.symbolCount; i++ {
		symbolID := int(b.result.scopeSymbols[i])
		name := b.result.symbols[symbolID].name
		shadowed := b.result.symbols[symbolID].shadowed
		for shadowed >= 0 && b.result.symbols[shadowed].scope == scopeID {
			shadowed = b.result.symbols[shadowed].shadowed
		}
		activeNames := b.activeNames(b.result.symbols[symbolID].kind.namespace())
		if shadowed >= 0 {
			activeNames[name] = shadowed
		} else {
			delete(activeNames, name)
		}
	}
	b.scopes = b.scopes[:len(b.scopes)-1]
}

func (b *binder) currentScope() int {
	return b.scopes[len(b.scopes)-1]
}

func (b *binder) currentFunction() int {
	if len(b.scopes) == 0 {
		return 0
	}
	return b.result.scopes[b.currentScope()].funcID
}
