package ember

type symbolKind string

const (
	symbolLocal         symbolKind = "local"
	symbolLocalFunction symbolKind = "localFunction"
	symbolParameter     symbolKind = "parameter"
	symbolTypeAlias     symbolKind = "typeAlias"
	symbolTypeParameter symbolKind = "typeParameter"
	symbolTypePack      symbolKind = "typePack"
)

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
	node     syntaxID
	name     string
	symbol   int
	scope    int
	start    int
	end      int
	captured bool
}

type boundCapture struct {
	symbol int
	scope  int
}

type bindScope struct {
	id              int
	parent          int
	funcID          int
	names           map[string]int
	capturedSymbols []bool
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
	definition int
	use        boundUse
	expression boundExpressionFact
}

type bindResult struct {
	scopes    []bindScope
	symbols   []boundSymbol
	captures  []boundCapture
	nodeFacts []boundNodeFacts
}

func (r bindResult) definition(node syntaxID) (boundSymbol, bool) {
	if node <= 0 || int(node) >= len(r.nodeFacts) {
		return boundSymbol{}, false
	}
	symbolID := r.nodeFacts[node].definition
	if symbolID < 0 || symbolID >= len(r.symbols) {
		return boundSymbol{}, false
	}
	return r.symbols[symbolID], true
}

func (r bindResult) use(node syntaxID) (boundUse, bool) {
	if node <= 0 || int(node) >= len(r.nodeFacts) {
		return boundUse{}, false
	}
	use := r.nodeFacts[node].use
	return use, use.symbol >= 0
}

func (r bindResult) expressionFact(node syntaxID) (boundExpressionFact, bool) {
	if node <= 0 || int(node) >= len(r.nodeFacts) {
		return boundExpressionFact{}, false
	}
	fact := r.nodeFacts[node].expression
	return fact, fact.valid
}

type binder struct {
	result      bindResult
	scopes      []int
	activeNames map[string]int
}

func bindProgram(prog program) bindResult {
	if prog.nodeCount == 0 {
		assignProgramSyntaxIDs(&prog)
	}
	nodeFacts := make([]boundNodeFacts, prog.nodeCount+1)
	for i := range nodeFacts {
		nodeFacts[i].definition = -1
		nodeFacts[i].use.symbol = -1
	}
	b := binder{
		result: bindResult{
			nodeFacts: nodeFacts,
		},
		activeNames: make(map[string]int),
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
		b.result.nodeFacts[expr.id].expression = boundExpressionFact{valid: true, arity: arity, multiret: multiret}
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
		b.recordUse(value.id, value.name, value.start, value.start+len(value.name))
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
			b.recordUse(value.id, value.name[0], value.start, value.start+len(value.name[0]))
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
	b.recordUse(target.id, target.name, target.start, target.end)
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
	if shadowed, ok := b.lookup(name); ok {
		symbol.shadowed = shadowed.id
	}
	b.result.symbols = append(b.result.symbols, symbol)
	if node > 0 {
		b.result.nodeFacts[node].definition = symbol.id
	}
	b.result.scopes[scope].names[name] = symbol.id
	b.activeNames[name] = symbol.id
	return symbol
}

func (b *binder) recordUse(node syntaxID, name string, start int, end int) {
	symbol, ok := b.lookup(name)
	if !ok {
		return
	}
	captured := symbol.funcID != b.currentFunction()
	use := boundUse{
		node:     node,
		name:     name,
		symbol:   symbol.id,
		scope:    b.currentScope(),
		start:    start,
		end:      end,
		captured: captured,
	}
	if node > 0 {
		b.result.nodeFacts[node].use = use
	}
	if captured {
		b.capture(symbol.id, b.currentScope())
	}
}

func (b *binder) capture(symbolID int, scope int) {
	facts := &b.result.symbols[symbolID].facts
	facts.captured = true
	scopeFacts := &b.result.scopes[scope]
	if len(scopeFacts.capturedSymbols) <= symbolID {
		scopeFacts.capturedSymbols = append(scopeFacts.capturedSymbols, make([]bool, symbolID-len(scopeFacts.capturedSymbols)+1)...)
	}
	if scopeFacts.capturedSymbols[symbolID] {
		return
	}
	scopeFacts.capturedSymbols[symbolID] = true
	b.result.captures = append(b.result.captures, boundCapture{symbol: symbolID, scope: scope})
}

func (b *binder) lookup(name string) (boundSymbol, bool) {
	if symbolID, ok := b.activeNames[name]; ok {
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
	scope := bindScope{
		id:     len(b.result.scopes),
		parent: parent,
		funcID: funcID,
		names:  make(map[string]int),
	}
	b.result.scopes = append(b.result.scopes, scope)
	b.scopes = append(b.scopes, scope.id)
	return scope.id
}

func (b *binder) popScope() {
	scope := b.result.scopes[b.currentScope()]
	for name, symbolID := range scope.names {
		shadowed := b.result.symbols[symbolID].shadowed
		for shadowed >= 0 && b.result.symbols[shadowed].scope == scope.id {
			shadowed = b.result.symbols[shadowed].shadowed
		}
		if shadowed >= 0 {
			b.activeNames[name] = shadowed
		} else {
			delete(b.activeNames, name)
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
