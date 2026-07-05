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
	name     string
	kind     symbolKind
	scope    int
	funcID   int
	shadowed int
}

type boundUse struct {
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
	id     int
	parent int
	funcID int
}

type bindResult struct {
	scopes   []bindScope
	symbols  []boundSymbol
	uses     []boundUse
	captures []boundCapture
}

func (r bindResult) findSymbol(scope int, name string, kind symbolKind) (boundSymbol, bool) {
	for _, symbol := range r.symbols {
		if symbol.scope == scope && symbol.name == name && symbol.kind == kind {
			return symbol, true
		}
	}
	return boundSymbol{}, false
}

func (r bindResult) useAt(start int, end int) (boundUse, bool) {
	for _, use := range r.uses {
		if use.start == start && use.end == end {
			return use, true
		}
	}
	return boundUse{}, false
}

type binder struct {
	result     bindResult
	scopes     []int
	nextFuncID int
}

func bindProgram(prog program) bindResult {
	b := binder{}
	b.pushScope()
	b.bindStatements(prog.statements)
	b.popScope()
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
		for _, name := range stmt.local.names {
			b.define(name, symbolLocal)
		}
	case stmt.localFunc != nil:
		b.define(stmt.localFunc.name, symbolLocalFunction)
		b.bindFunction(stmt.localFunc.typeParams, stmt.localFunc.typePacks, stmt.localFunc.params, stmt.localFunc.paramAnnotations, stmt.localFunc.variadicAnnotation, stmt.localFunc.returnAnnotation, stmt.localFunc.statements)
	case stmt.funcDecl != nil:
		b.bindAssignTarget(stmt.funcDecl.target)
		b.bindFunction(stmt.funcDecl.typeParams, stmt.funcDecl.typePacks, stmt.funcDecl.params, stmt.funcDecl.paramAnnotations, stmt.funcDecl.variadicAnnotation, stmt.funcDecl.returnAnnotation, stmt.funcDecl.statements)
	case stmt.assign != nil:
		for _, value := range stmt.assign.values {
			b.bindExpression(value)
		}
		for _, target := range stmt.assign.targets {
			b.bindAssignTarget(target)
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
		b.define(stmt.forLoop.name, symbolLocal)
		b.bindStatements(stmt.forLoop.statements)
		b.popScope()
	case stmt.genericFor != nil:
		for _, value := range stmt.genericFor.values {
			b.bindExpression(value)
		}
		b.pushScope()
		for _, name := range stmt.genericFor.names {
			b.define(name, symbolLocal)
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
		b.define(stmt.typeAlias.name, symbolTypeAlias)
		b.pushScope()
		for _, name := range stmt.typeAlias.typeParams {
			b.define(name, symbolTypeParameter)
		}
		for _, name := range stmt.typeAlias.typePacks {
			b.define(name, symbolTypePack)
		}
		b.bindTypeExpression(stmt.typeAlias.value)
		b.popScope()
	}
}

func (b *binder) bindFunction(typeParams []string, typePacks []string, params []string, paramAnnotations []*typeExpression, variadicAnnotation *typeExpression, returnAnnotation *typeExpression, statements []statement) {
	b.pushFunctionScope()
	for _, name := range typeParams {
		b.define(name, symbolTypeParameter)
	}
	for _, name := range typePacks {
		b.define(name, symbolTypePack)
	}
	for _, annotation := range paramAnnotations {
		b.bindTypeExpression(annotation)
	}
	b.bindTypeExpression(variadicAnnotation)
	b.bindTypeExpression(returnAnnotation)
	for _, name := range params {
		b.define(name, symbolParameter)
	}
	b.bindStatements(statements)
	b.popScope()
}

func (b *binder) bindExpression(expr expression) {
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
		b.bindFunction(value.function.typeParams, value.function.typePacks, value.function.params, value.function.paramAnnotations, value.function.variadicAnnotation, value.function.returnAnnotation, value.function.statements)
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
		b.use(value.name, value.start, value.start+len(value.name))
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
			b.useType(value.name[0], value.start, value.start+len(value.name[0]))
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
		for _, name := range value.typeParams {
			b.define(name, symbolTypeParameter)
		}
		for _, name := range value.typePacks {
			b.define(name, symbolTypePack)
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

func (b *binder) bindAssignTarget(target assignTarget) {
	b.use(target.name, target.start, target.end)
	for _, selector := range target.selectors {
		if selector.index != nil {
			b.bindExpression(*selector.index)
		}
	}
}

func (b *binder) useType(name string, start int, end int) {
	symbol, ok := b.lookup(name)
	if !ok {
		return
	}
	b.result.uses = append(b.result.uses, boundUse{
		name:   name,
		symbol: symbol.id,
		scope:  b.currentScope(),
		start:  start,
		end:    end,
	})
}

func (b *binder) bindScoped(statements []statement) {
	b.pushScope()
	b.bindStatements(statements)
	b.popScope()
}

func (b *binder) define(name string, kind symbolKind) boundSymbol {
	scope := b.currentScope()
	symbol := boundSymbol{
		id:       len(b.result.symbols),
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
	return symbol
}

func (b *binder) use(name string, start int, end int) {
	symbol, ok := b.lookup(name)
	if !ok {
		return
	}
	captured := symbol.funcID != b.currentFunction()
	b.result.uses = append(b.result.uses, boundUse{
		name:     name,
		symbol:   symbol.id,
		scope:    b.currentScope(),
		start:    start,
		end:      end,
		captured: captured,
	})
	if captured {
		b.capture(symbol.id, b.currentScope())
	}
}

func (b *binder) capture(symbolID int, scope int) {
	for _, capture := range b.result.captures {
		if capture.symbol == symbolID && capture.scope == scope {
			return
		}
	}
	b.result.captures = append(b.result.captures, boundCapture{symbol: symbolID, scope: scope})
}

func (b *binder) lookup(name string) (boundSymbol, bool) {
	for i := len(b.scopes) - 1; i >= 0; i-- {
		scope := b.scopes[i]
		for j := len(b.result.symbols) - 1; j >= 0; j-- {
			if b.result.symbols[j].scope == scope && b.result.symbols[j].name == name {
				return b.result.symbols[j], true
			}
		}
	}
	return boundSymbol{}, false
}

func (b *binder) pushScope() int {
	return b.pushScopeForFunction(b.currentFunction())
}

func (b *binder) pushFunctionScope() int {
	b.nextFuncID++
	return b.pushScopeForFunction(b.nextFuncID)
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
	}
	b.result.scopes = append(b.result.scopes, scope)
	b.scopes = append(b.scopes, scope.id)
	return scope.id
}

func (b *binder) popScope() {
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
