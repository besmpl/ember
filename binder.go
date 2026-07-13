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

func bindProgram(prog program) bindResult {
	return bindSyntaxTree(newSyntaxTree(prog))
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
	b.bindStatements(tree.statements())
	b.popScope()
	for i := range b.result.symbols {
		facts := &b.result.symbols[i].facts
		facts.immutableCopyEligible = facts.captured && !facts.mutatedAfterCapture
	}
	return b.result
}

func (b *binder) bindStatements(statements []statement) {
	for _, stmt := range statements {
		b.bindStatement(stmt)
	}
}

func (b *binder) bindStatement(stmt statement) {
	switch b.tree.statementKind(&stmt) {
	case syntaxStatementLocal:
		local := b.tree.local(&stmt)
		for _, annotation := range b.tree.localAnnotations(local) {
			b.bindTypeExpression(annotation)
		}
		for _, value := range b.tree.localValues(local) {
			b.bindExpression(value)
		}
		for i, name := range b.tree.localNames(local) {
			b.define(name, symbolLocal, syntaxNameID(b.tree.localNameID(local), i))
		}
	case syntaxStatementLocalFunction:
		localFunc := b.tree.localFunction(&stmt)
		b.define(b.tree.localFunctionName(localFunc), symbolLocalFunction, b.tree.localFunctionNameID(localFunc))
		b.bindFunction(b.tree.localFunctionID(localFunc), b.tree.localFunctionTypeParams(localFunc), b.tree.localFunctionTypeParamID(localFunc), b.tree.localFunctionTypePacks(localFunc), b.tree.localFunctionTypePackID(localFunc), b.tree.localFunctionParams(localFunc), b.tree.localFunctionParamID(localFunc), b.tree.localFunctionParamAnnotations(localFunc), b.tree.localFunctionVariadicAnnotation(localFunc), b.tree.localFunctionReturnAnnotation(localFunc), b.tree.localFunctionStatements(localFunc))
	case syntaxStatementFunctionDeclaration:
		funcDecl := b.tree.functionDeclaration(&stmt)
		target := b.tree.functionDeclarationTarget(funcDecl)
		b.bindAssignTarget(*target, true)
		params := b.tree.functionDeclarationParams(funcDecl)
		paramID := b.tree.functionDeclarationParamID(funcDecl)
		if b.tree.functionDeclarationMethod(funcDecl) {
			params = append([]string{"self"}, params...)
			paramID = b.tree.functionDeclarationSelfID(funcDecl)
		}
		b.bindFunction(b.tree.functionDeclarationID(funcDecl), b.tree.functionDeclarationTypeParams(funcDecl), b.tree.functionDeclarationTypeParamID(funcDecl), b.tree.functionDeclarationTypePacks(funcDecl), b.tree.functionDeclarationTypePackID(funcDecl), params, paramID, b.tree.functionDeclarationParamAnnotations(funcDecl), b.tree.functionDeclarationVariadicAnnotation(funcDecl), b.tree.functionDeclarationReturnAnnotation(funcDecl), b.tree.functionDeclarationStatements(funcDecl))
	case syntaxStatementAssign:
		assign := b.tree.assignment(&stmt)
		for _, value := range b.tree.assignmentValues(assign) {
			b.bindExpression(value)
		}
		for _, target := range b.tree.assignmentTargets(assign) {
			b.bindAssignTarget(target, true)
		}
	case syntaxStatementCall:
		b.bindTerm(*b.tree.call(&stmt))
	case syntaxStatementIf:
		ifStmt := b.tree.ifStatement(&stmt)
		b.bindExpression(*b.tree.ifCondition(ifStmt))
		b.bindScoped(b.tree.ifThenStatements(ifStmt))
		b.bindScoped(b.tree.ifElseStatements(ifStmt))
	case syntaxStatementWhile:
		while := b.tree.whileStatement(&stmt)
		b.bindExpression(*b.tree.whileCondition(while))
		b.bindScoped(b.tree.whileStatements(while))
	case syntaxStatementFor:
		forLoop := b.tree.forStatement(&stmt)
		b.bindExpression(*b.tree.numericForStart(forLoop))
		b.bindExpression(*b.tree.numericForLimit(forLoop))
		if step := b.tree.numericForStep(forLoop); step != nil {
			b.bindExpression(*step)
		}
		b.pushScope()
		b.define(b.tree.numericForName(forLoop), symbolLocal, b.tree.numericForNameID(forLoop))
		b.bindStatements(b.tree.numericForStatements(forLoop))
		b.popScope()
	case syntaxStatementGenericFor:
		genericFor := b.tree.genericForStatement(&stmt)
		for _, value := range b.tree.genericForValues(genericFor) {
			b.bindExpression(value)
		}
		b.pushScope()
		for i, name := range b.tree.genericForNames(genericFor) {
			b.define(name, symbolLocal, syntaxNameID(b.tree.genericForNameID(genericFor), i))
		}
		b.bindStatements(b.tree.genericForStatements(genericFor))
		b.popScope()
	case syntaxStatementRepeat:
		repeat := b.tree.repeatStatement(&stmt)
		b.pushScope()
		b.bindStatements(b.tree.repeatStatements(repeat))
		b.bindExpression(*b.tree.repeatCondition(repeat))
		b.popScope()
	case syntaxStatementBlock:
		b.bindScoped(b.tree.blockStatements(b.tree.blockStatement(&stmt)))
	case syntaxStatementReturn:
		for _, value := range b.tree.returnValues(b.tree.returnStatement(&stmt)) {
			b.bindExpression(value)
		}
	case syntaxStatementTypeAlias:
		typeAlias := b.tree.typeAliasStatement(&stmt)
		b.define(b.tree.typeAliasName(typeAlias), symbolTypeAlias, b.tree.typeAliasNameID(typeAlias))
		b.pushScope()
		for i, name := range b.tree.typeAliasTypeParams(typeAlias) {
			b.define(name, symbolTypeParameter, syntaxNameID(b.tree.typeAliasTypeParamID(typeAlias), i))
		}
		for i, name := range b.tree.typeAliasTypePacks(typeAlias) {
			b.define(name, symbolTypePack, syntaxNameID(b.tree.typeAliasTypePackID(typeAlias), i))
		}
		b.bindTypeExpression(b.tree.typeAliasValue(typeAlias))
		b.popScope()
	}
}

func (b *binder) bindFunction(functionID int, typeParams []string, typeParamID syntaxID, typePacks []string, typePackID syntaxID, params []string, paramID syntaxID, paramAnnotations []*typeExpression, variadicAnnotation *typeExpression, returnAnnotation *typeExpression, statements []statement) {
	b.pushScopeForFunction(functionID)
	for i, name := range typeParams {
		b.define(name, symbolTypeParameter, syntaxNameID(typeParamID, i))
	}
	for i, name := range typePacks {
		b.define(name, symbolTypePack, syntaxNameID(typePackID, i))
	}
	for _, annotation := range paramAnnotations {
		b.bindTypeExpression(annotation)
	}
	b.bindTypeExpression(variadicAnnotation)
	b.bindTypeExpression(returnAnnotation)
	for i, name := range params {
		b.define(name, symbolParameter, syntaxNameID(paramID, i))
	}
	b.bindStatements(statements)
	b.popScope()
}

func (b *binder) bindExpression(expr expression) {
	if b.tree.expressionID(&expr) > 0 {
		multiret := expressionExpands(b.tree, expr)
		arity := 1
		if multiret {
			arity = -1
		}
		facts := &b.result.nodeFacts[b.tree.expressionID(&expr)]
		facts.expressionArity = int32(arity)
		facts.flags |= boundNodeExpressionValid
		if multiret {
			facts.flags |= boundNodeMultiret
		}
	}
	for _, and := range b.tree.expressionTerms(&expr) {
		for _, comparison := range b.tree.andTerms(&and) {
			b.bindConcatExpression(b.tree.comparisonLeft(&comparison))
			if right := b.tree.comparisonRight(&comparison); right != nil {
				b.bindConcatExpression(*right)
			}
		}
	}
}

func (b *binder) bindConcatExpression(expr concatExpression) {
	b.bindAdditiveExpression(b.tree.concatFirst(&expr))
	for _, part := range b.tree.concatRest(&expr) {
		b.bindAdditiveExpression(part)
	}
}

func (b *binder) bindAdditiveExpression(expr additiveExpression) {
	b.bindMultiplicativeExpression(b.tree.additiveFirst(&expr))
	for _, part := range b.tree.additiveRest(&expr) {
		b.bindMultiplicativeExpression(*b.tree.additivePartValue(&part))
	}
}

func (b *binder) bindMultiplicativeExpression(expr multiplicativeExpression) {
	b.bindTerm(b.tree.multiplicativeFirst(&expr))
	for _, part := range b.tree.multiplicativeRest(&expr) {
		b.bindTerm(*b.tree.multiplicativePartValue(&part))
	}
}

func (b *binder) bindTerm(value term) {
	if power := b.tree.termPower(&value); power != nil {
		b.bindTerm(*b.tree.powerBase(power))
		b.bindTerm(*b.tree.powerExponent(power))
	}
	if table := b.tree.termTable(&value); table != nil {
		for _, field := range b.tree.tableFields(table) {
			if key := b.tree.tableFieldKey(&field); key != nil {
				b.bindExpression(*key)
			}
			b.bindExpression(*b.tree.tableFieldValue(&field))
		}
	}
	if function := b.tree.termFunction(&value); function != nil {
		b.bindFunction(b.tree.functionExpressionFunctionID(function), b.tree.functionExpressionTypeParams(function), b.tree.functionExpressionTypeParamID(function), b.tree.functionExpressionTypePacks(function), b.tree.functionExpressionTypePackID(function), b.tree.functionExpressionParams(function), b.tree.functionExpressionParamID(function), b.tree.functionExpressionParamAnnotations(function), b.tree.functionExpressionVariadicAnnotation(function), b.tree.functionExpressionReturnAnnotation(function), b.tree.functionExpressionStatements(function))
	}
	if ifExpr := b.tree.termIf(&value); ifExpr != nil {
		b.bindExpression(*b.tree.ifExpressionCondition(ifExpr))
		b.bindExpression(*b.tree.ifExpressionThen(ifExpr))
		b.bindExpression(*b.tree.ifExpressionElse(ifExpr))
	}
	if call := b.tree.termCall(&value); call != nil {
		b.bindCall(*call)
	}
	if unaryNot := b.tree.termUnaryNot(&value); unaryNot != nil {
		b.bindTerm(*unaryNot)
	}
	if unaryMinus := b.tree.termUnaryMinus(&value); unaryMinus != nil {
		b.bindTerm(*unaryMinus)
	}
	if unaryLen := b.tree.termUnaryLength(&value); unaryLen != nil {
		b.bindTerm(*unaryLen)
	}
	if group := b.tree.termGroup(&value); group != nil {
		b.bindExpression(*group)
	}
	b.bindTypeExpression(b.tree.termCast(&value))
	if name := b.tree.termName(&value); name != "" {
		b.recordUse(b.tree.termID(&value), name, valueNamespace)
	}
	for _, selector := range b.tree.termSelectors(&value) {
		if index := b.tree.selectorIndex(&selector); index != nil {
			b.bindExpression(*index)
		}
	}
}

func (b *binder) bindCall(call callExpression) {
	b.bindTerm(*b.tree.callTarget(&call))
	if receiver := b.tree.callReceiver(&call); receiver != nil {
		b.bindTerm(*receiver)
	}
	for _, arg := range b.tree.callTypeArgs(&call) {
		b.bindTypeExpression(arg)
	}
	for _, arg := range b.tree.callArgs(&call) {
		b.bindExpression(arg)
	}
}

func (b *binder) bindTypeExpression(value *typeExpression) {
	if value == nil {
		return
	}
	switch b.tree.typeKind(value) {
	case typeKindName:
		name := b.tree.typeName(value)
		if len(name) > 0 {
			namespace := typeNamespace
			// Qualified module types resolve their root through the value
			// namespace (for example, Types.Count); the exported member is
			// checked against the module summary rather than local type facts.
			if len(name) > 1 {
				namespace = valueNamespace
			}
			b.recordUse(b.tree.typeID(value), name[0], namespace)
		}
		for _, arg := range b.tree.typeArgs(value) {
			b.bindTypeExpression(arg)
		}
	case typeKindUnion, typeKindIntersection:
		for _, option := range b.tree.typeChildren(value) {
			b.bindTypeExpression(option)
		}
	case typeKindNilable, typeKindVariadic, typeKindGenericPack:
		b.bindTypeExpression(b.tree.typeInner(value))
	case typeKindTable:
		for _, field := range b.tree.typeFields(value) {
			b.bindTypeExpression(b.tree.typeFieldKey(&field))
			b.bindTypeExpression(b.tree.typeFieldValue(&field))
		}
	case typeKindFunction:
		b.bindTypeFunction(value)
	case typeKindGenericFunction:
		b.pushScope()
		for i, name := range b.tree.typeTypeParams(value) {
			b.define(name, symbolTypeParameter, syntaxNameID(b.tree.typeParamID(value), i))
		}
		for i, name := range b.tree.typePacks(value) {
			b.define(name, symbolTypePack, syntaxNameID(b.tree.typePackID(value), i))
		}
		b.bindTypeFunction(value)
		b.popScope()
	case typeKindTypeof:
		if expr := b.tree.typeExpression(value); expr != nil {
			b.bindExpression(*expr)
		}
	}
}

func (b *binder) bindTypeFunction(value *typeExpression) {
	for _, param := range b.tree.typeParams(value) {
		b.bindTypeExpression(b.tree.typeParamValue(&param))
	}
	b.bindTypeExpression(b.tree.typeReturn(value))
}

func (b *binder) bindAssignTarget(target assignTarget, assignment bool) {
	targetID := b.tree.assignTargetID(&target)
	b.recordUse(targetID, b.tree.assignTargetName(&target), valueNamespace)
	if assignment && len(b.tree.assignTargetSelectors(&target)) == 0 {
		if use, ok := b.result.use(targetID); ok {
			facts := &b.result.symbols[use.symbol].facts
			facts.assigned = true
			if facts.captured {
				facts.mutatedAfterCapture = true
			}
		}
	}
	for _, selector := range b.tree.assignTargetSelectors(&target) {
		if index := b.tree.selectorIndex(&selector); index != nil {
			b.bindExpression(*index)
		}
	}
}

func (b *binder) bindScoped(statements []statement) {
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
