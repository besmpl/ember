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
	return boundUseClassification(r.nodeFacts[node].use)
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
	result           bindResult
	scopes           []int
	scopeLastSymbols []int32
	symbolPrevious   []int32
	captureSets      []map[int]struct{}
	activeValueNames map[string]int
	activeTypeNames  map[string]int
}

func bindProgram(prog program) bindResult {
	if prog.nodeCount == 0 {
		assignProgramSyntaxIDs(&prog)
	}
	nodeFacts := make([]boundNodeFacts, prog.nodeCount+1)
	for i := range nodeFacts {
		nodeFacts[i].definition = -1
		nodeFacts[i].use = int32(boundUseUnvisited)
	}
	b := binder{
		result: bindResult{
			nodeFacts: nodeFacts,
		},
		activeValueNames: make(map[string]int),
		activeTypeNames:  make(map[string]int),
	}
	b.pushScopeForFunction(0)
	b.bindStatements(prog.statements)
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
	switch {
	case stmt.local != nil:
		for _, annotation := range stmt.local.annotations {
			b.bindTypeExpression(annotation)
		}
		for _, value := range stmt.local.values {
			b.bindExpression(value)
		}
		for i, name := range stmt.local.names {
			b.define(name, symbolLocal, syntaxNameID(stmt.local.nameID, i))
		}
	case stmt.localFunc != nil:
		b.define(stmt.localFunc.name, symbolLocalFunction, stmt.localFunc.nameID)
		b.bindFunction(stmt.localFunc.functionID, stmt.localFunc.typeParams, stmt.localFunc.typeParamID, stmt.localFunc.typePacks, stmt.localFunc.typePackID, stmt.localFunc.params, stmt.localFunc.paramID, stmt.localFunc.paramAnnotations, stmt.localFunc.variadicAnnotation, stmt.localFunc.returnAnnotation, stmt.localFunc.statements)
	case stmt.funcDecl != nil:
		b.bindAssignTarget(stmt.funcDecl.target, true)
		params := stmt.funcDecl.params
		paramID := stmt.funcDecl.paramID
		if stmt.funcDecl.method {
			params = append([]string{"self"}, params...)
			paramID = stmt.funcDecl.selfID
		}
		b.bindFunction(stmt.funcDecl.functionID, stmt.funcDecl.typeParams, stmt.funcDecl.typeParamID, stmt.funcDecl.typePacks, stmt.funcDecl.typePackID, params, paramID, stmt.funcDecl.paramAnnotations, stmt.funcDecl.variadicAnnotation, stmt.funcDecl.returnAnnotation, stmt.funcDecl.statements)
	case stmt.assign != nil:
		for _, value := range stmt.assign.values {
			b.bindExpression(value)
		}
		for _, target := range stmt.assign.targets {
			b.bindAssignTarget(target, true)
		}
	case stmt.call != nil:
		b.bindTerm(*stmt.call)
	case stmt.ifStmt != nil:
		b.bindExpression(stmt.ifStmt.condition)
		b.bindScoped(stmt.ifStmt.thenStatements)
		b.bindScoped(stmt.ifStmt.elseStatements)
	case stmt.while != nil:
		b.bindExpression(stmt.while.condition)
		b.bindScoped(stmt.while.statements)
	case stmt.forLoop != nil:
		b.bindExpression(stmt.forLoop.start)
		b.bindExpression(stmt.forLoop.limit)
		if stmt.forLoop.step != nil {
			b.bindExpression(*stmt.forLoop.step)
		}
		b.pushScope()
		b.define(stmt.forLoop.name, symbolLocal, stmt.forLoop.nameID)
		b.bindStatements(stmt.forLoop.statements)
		b.popScope()
	case stmt.genericFor != nil:
		for _, value := range stmt.genericFor.values {
			b.bindExpression(value)
		}
		b.pushScope()
		for i, name := range stmt.genericFor.names {
			b.define(name, symbolLocal, syntaxNameID(stmt.genericFor.nameID, i))
		}
		b.bindStatements(stmt.genericFor.statements)
		b.popScope()
	case stmt.repeat != nil:
		b.pushScope()
		b.bindStatements(stmt.repeat.statements)
		b.bindExpression(stmt.repeat.condition)
		b.popScope()
	case stmt.block != nil:
		b.bindScoped(stmt.block.statements)
	case stmt.ret != nil:
		for _, value := range stmt.ret.values {
			b.bindExpression(value)
		}
	case stmt.typeAlias != nil:
		b.define(stmt.typeAlias.name, symbolTypeAlias, stmt.typeAlias.nameID)
		b.pushScope()
		for i, name := range stmt.typeAlias.typeParams {
			b.define(name, symbolTypeParameter, syntaxNameID(stmt.typeAlias.typeParamID, i))
		}
		for i, name := range stmt.typeAlias.typePacks {
			b.define(name, symbolTypePack, syntaxNameID(stmt.typeAlias.typePackID, i))
		}
		b.bindTypeExpression(stmt.typeAlias.value)
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
	if expr.id > 0 {
		multiret := expressionExpands(expr)
		arity := 1
		if multiret {
			arity = -1
		}
		facts := &b.result.nodeFacts[expr.id]
		facts.expressionArity = int32(arity)
		facts.flags |= boundNodeExpressionValid
		if multiret {
			facts.flags |= boundNodeMultiret
		}
	}
	for _, and := range expr.terms {
		for _, comparison := range and.terms {
			b.bindConcatExpression(comparison.left)
			if comparison.right != nil {
				b.bindConcatExpression(*comparison.right)
			}
		}
	}
}

func (b *binder) bindConcatExpression(expr concatExpression) {
	b.bindAdditiveExpression(expr.first)
	for _, part := range expr.rest {
		b.bindAdditiveExpression(part)
	}
}

func (b *binder) bindAdditiveExpression(expr additiveExpression) {
	b.bindMultiplicativeExpression(expr.first)
	for _, part := range expr.rest {
		b.bindMultiplicativeExpression(part.value)
	}
}

func (b *binder) bindMultiplicativeExpression(expr multiplicativeExpression) {
	b.bindTerm(expr.first)
	for _, part := range expr.rest {
		b.bindTerm(part.value)
	}
}

func (b *binder) bindTerm(value term) {
	if value.power != nil {
		b.bindTerm(value.power.base)
		b.bindTerm(value.power.exponent)
	}
	if value.table != nil {
		for _, field := range value.table.fields {
			if field.key != nil {
				b.bindExpression(*field.key)
			}
			b.bindExpression(field.value)
		}
	}
	if value.function != nil {
		b.bindFunction(value.function.functionID, value.function.typeParams, value.function.typeParamID, value.function.typePacks, value.function.typePackID, value.function.params, value.function.paramID, value.function.paramAnnotations, value.function.variadicAnnotation, value.function.returnAnnotation, value.function.statements)
	}
	if value.ifExpr != nil {
		b.bindExpression(value.ifExpr.condition)
		b.bindExpression(value.ifExpr.thenValue)
		b.bindExpression(value.ifExpr.elseValue)
	}
	if value.call != nil {
		b.bindCall(*value.call)
	}
	if value.unaryNot != nil {
		b.bindTerm(*value.unaryNot)
	}
	if value.unaryMinus != nil {
		b.bindTerm(*value.unaryMinus)
	}
	if value.unaryLen != nil {
		b.bindTerm(*value.unaryLen)
	}
	if value.group != nil {
		b.bindExpression(*value.group)
	}
	b.bindTypeExpression(value.cast)
	if value.name != "" {
		b.recordUse(value.id, value.name, valueNamespace)
	}
	for _, selector := range value.selectors {
		if selector.index != nil {
			b.bindExpression(*selector.index)
		}
	}
}

func (b *binder) bindCall(call callExpression) {
	b.bindTerm(call.target)
	if call.receiver != nil {
		b.bindTerm(*call.receiver)
	}
	for _, arg := range call.typeArgs {
		b.bindTypeExpression(arg)
	}
	for _, arg := range call.args {
		b.bindExpression(arg)
	}
}

func (b *binder) bindTypeExpression(value *typeExpression) {
	if value == nil {
		return
	}
	switch value.kind {
	case typeKindName:
		if len(value.name) > 0 {
			namespace := typeNamespace
			// Qualified module types resolve their root through the value
			// namespace (for example, Types.Count); the exported member is
			// checked against the module summary rather than local type facts.
			if len(value.name) > 1 {
				namespace = valueNamespace
			}
			b.recordUse(value.id, value.name[0], namespace)
		}
		for _, arg := range value.typeArgs {
			b.bindTypeExpression(arg)
		}
	case typeKindUnion, typeKindIntersection:
		for _, option := range value.types {
			b.bindTypeExpression(option)
		}
	case typeKindNilable, typeKindVariadic, typeKindGenericPack:
		b.bindTypeExpression(value.inner)
	case typeKindTable:
		for _, field := range value.fields {
			b.bindTypeExpression(field.key)
			b.bindTypeExpression(field.value)
		}
	case typeKindFunction:
		b.bindTypeFunction(value)
	case typeKindGenericFunction:
		b.pushScope()
		for i, name := range value.typeParams {
			b.define(name, symbolTypeParameter, syntaxNameID(value.typeParamID, i))
		}
		for i, name := range value.typePacks {
			b.define(name, symbolTypePack, syntaxNameID(value.typePackID, i))
		}
		b.bindTypeFunction(value)
		b.popScope()
	case typeKindTypeof:
		if value.expr != nil {
			b.bindExpression(*value.expr)
		}
	}
}

func (b *binder) bindTypeFunction(value *typeExpression) {
	for _, param := range value.params {
		b.bindTypeExpression(param.value)
	}
	b.bindTypeExpression(value.returnType)
}

func (b *binder) bindAssignTarget(target assignTarget, assignment bool) {
	b.recordUse(target.id, target.name, valueNamespace)
	if assignment && len(target.selectors) == 0 {
		if use, ok := b.result.use(target.id); ok {
			facts := &b.result.symbols[use.symbol].facts
			facts.assigned = true
			if facts.captured {
				facts.mutatedAfterCapture = true
			}
		}
	}
	for _, selector := range target.selectors {
		if selector.index != nil {
			b.bindExpression(*selector.index)
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
