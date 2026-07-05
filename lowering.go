package ember

type loweredValueKind int

const (
	loweredValueSingle loweredValueKind = iota
	loweredValueExpanded
	loweredValueNil
)

type loweredValue struct {
	kind        loweredValueKind
	source      int
	resultCount int
}

type loweredValueList struct {
	items []loweredValue
}

type loweredCall struct {
	target        term
	receiver      *term
	args          loweredValueList
	fixedArgCount int
}

type loweredLoopKind int

const (
	loweredLoopPreTest loweredLoopKind = iota
	loweredLoopPostTest
)

type loweredLoopContinueTarget int

const (
	loweredLoopContinueCondition loweredLoopContinueTarget = iota
)

type loweredLoop struct {
	kind           loweredLoopKind
	condition      expression
	body           []statement
	continueTarget loweredLoopContinueTarget
}

type loweredNumericForContinueTarget int

const (
	loweredNumericForContinueIncrement loweredNumericForContinueTarget = iota
)

type loweredNumericForLoop struct {
	name           string
	start          expression
	limit          expression
	step           *expression
	defaultStep    bool
	body           []statement
	continueTarget loweredNumericForContinueTarget
}

type loweredGenericForContinueTarget int

const (
	loweredGenericForContinueIterator loweredGenericForContinueTarget = iota
)

type loweredGenericForLoop struct {
	names                 []string
	values                []expression
	body                  []statement
	prepareDirectIterator bool
	continueTarget        loweredGenericForContinueTarget
}

type loweredClosure struct {
	typeParams         []string
	typePacks          []string
	params             []string
	paramAnnotations   []*typeExpression
	variadic           bool
	variadicAnnotation *typeExpression
	returnAnnotation   *typeExpression
	body               []statement
}

type loweredTableFieldKind int

const (
	loweredTableFieldArray loweredTableFieldKind = iota
	loweredTableFieldNamed
	loweredTableFieldComputed
)

type loweredTableField struct {
	kind       loweredTableFieldKind
	name       string
	arrayIndex int
	key        *expression
	value      expression
}

type loweredTable struct {
	fields []loweredTableField
}

type loweredIfStatement struct {
	condition expression
	thenBody  []statement
	elseBody  []statement
}

type loweredIfExpression struct {
	condition expression
	thenValue expression
	elseValue expression
}

type loweredAssignment struct {
	targets []assignTarget
	sources []expression
	values  loweredValueList
}

type loweredLocal struct {
	names       []string
	annotations []*typeExpression
	sources     []expression
	values      loweredValueList
}

type loweredReturn struct {
	sources []expression
	values  loweredValueList
}

type loweredBlock struct {
	body         []statement
	lexicalScope bool
}

type loweredCallStatement struct {
	call           loweredCall
	args           []expression
	discardResults bool
	resultCount    int
}

type loweredStatementKind int

const (
	loweredStatementLocal loweredStatementKind = iota
	loweredStatementLocalFunction
	loweredStatementFunctionDeclaration
	loweredStatementAssignment
	loweredStatementCall
	loweredStatementIf
	loweredStatementWhile
	loweredStatementNumericFor
	loweredStatementGenericFor
	loweredStatementRepeat
	loweredStatementBlock
	loweredStatementTypeAlias
	loweredStatementBreak
	loweredStatementContinue
	loweredStatementReturn
	loweredStatementEmpty
)

type loweredStatement struct {
	kind                loweredStatementKind
	local               *loweredLocal
	localFunction       *localFunctionStatement
	functionDeclaration *functionDeclarationStatement
	assignment          *loweredAssignment
	call                *loweredCallStatement
	ifStatement         *loweredIfStatement
	while               *whileStatement
	numericFor          *forStatement
	genericFor          *genericForStatement
	repeat              *repeatStatement
	block               *loweredBlock
	typeAlias           *typeAliasStatement
	ret                 *loweredReturn
}

type loweredProgram struct {
	statements []loweredStatement
}

func lowerProgram(prog program) loweredProgram {
	return loweredProgram{statements: lowerStatements(prog.statements)}
}

func lowerStatements(statements []statement) []loweredStatement {
	lowered := make([]loweredStatement, 0, len(statements))
	for _, stmt := range statements {
		lowered = append(lowered, lowerStatement(stmt))
	}
	return lowered
}

func lowerFixedValueList(values []expression, targetCount int) loweredValueList {
	items := make([]loweredValue, 0, targetCount)
	for i := 0; i < targetCount; i++ {
		if i >= len(values) {
			items = append(items, loweredValue{kind: loweredValueNil, source: -1, resultCount: 1})
			continue
		}
		if i == len(values)-1 && expressionExpands(values[i]) {
			items = append(items, loweredValue{kind: loweredValueExpanded, source: i, resultCount: targetCount - i})
			continue
		}
		items = append(items, loweredValue{kind: loweredValueSingle, source: i, resultCount: 1})
	}
	return loweredValueList{items: items}
}

func lowerOpenValueList(values []expression) loweredValueList {
	items := make([]loweredValue, 0, len(values))
	for i := range values {
		if i == len(values)-1 && expressionExpands(values[i]) {
			items = append(items, loweredValue{kind: loweredValueExpanded, source: i, resultCount: -1})
			continue
		}
		items = append(items, loweredValue{kind: loweredValueSingle, source: i, resultCount: 1})
	}
	return loweredValueList{items: items}
}

func lowerCall(call callExpression) loweredCall {
	fixedArgCount := 0
	if call.receiver != nil {
		fixedArgCount = 1
	}
	return loweredCall{
		target:        call.target,
		receiver:      call.receiver,
		args:          lowerOpenValueList(call.args),
		fixedArgCount: fixedArgCount,
	}
}

func lowerWhileLoop(stmt whileStatement) loweredLoop {
	return loweredLoop{
		kind:           loweredLoopPreTest,
		condition:      stmt.condition,
		body:           stmt.statements,
		continueTarget: loweredLoopContinueCondition,
	}
}

func lowerRepeatLoop(stmt repeatStatement) loweredLoop {
	return loweredLoop{
		kind:           loweredLoopPostTest,
		condition:      stmt.condition,
		body:           stmt.statements,
		continueTarget: loweredLoopContinueCondition,
	}
}

func lowerNumericForLoop(stmt forStatement) loweredNumericForLoop {
	return loweredNumericForLoop{
		name:           stmt.name,
		start:          stmt.start,
		limit:          stmt.limit,
		step:           stmt.step,
		defaultStep:    stmt.step == nil,
		body:           stmt.statements,
		continueTarget: loweredNumericForContinueIncrement,
	}
}

func lowerGenericForLoop(stmt genericForStatement) loweredGenericForLoop {
	return loweredGenericForLoop{
		names:                 append([]string(nil), stmt.names...),
		values:                append([]expression(nil), stmt.values...),
		body:                  stmt.statements,
		prepareDirectIterator: len(stmt.values) == 1,
		continueTarget:        loweredGenericForContinueIterator,
	}
}

func lowerClosure(fn functionExpression) loweredClosure {
	return loweredClosure{
		typeParams:         append([]string(nil), fn.typeParams...),
		typePacks:          append([]string(nil), fn.typePacks...),
		params:             append([]string(nil), fn.params...),
		paramAnnotations:   append([]*typeExpression(nil), fn.paramAnnotations...),
		variadic:           fn.variadic,
		variadicAnnotation: fn.variadicAnnotation,
		returnAnnotation:   fn.returnAnnotation,
		body:               fn.statements,
	}
}

func lowerLocalFunctionClosure(stmt localFunctionStatement) loweredClosure {
	return lowerClosure(functionExpression{
		typeParams:         stmt.typeParams,
		typePacks:          stmt.typePacks,
		params:             stmt.params,
		paramAnnotations:   stmt.paramAnnotations,
		variadic:           stmt.variadic,
		variadicAnnotation: stmt.variadicAnnotation,
		returnAnnotation:   stmt.returnAnnotation,
		statements:         stmt.statements,
	})
}

func lowerFunctionDeclarationClosure(stmt functionDeclarationStatement) loweredClosure {
	params := append([]string(nil), stmt.params...)
	if stmt.method {
		params = append([]string{"self"}, params...)
	}
	return lowerClosure(functionExpression{
		typeParams:         stmt.typeParams,
		typePacks:          stmt.typePacks,
		params:             params,
		paramAnnotations:   stmt.paramAnnotations,
		variadic:           stmt.variadic,
		variadicAnnotation: stmt.variadicAnnotation,
		returnAnnotation:   stmt.returnAnnotation,
		statements:         stmt.statements,
	})
}

func lowerTable(table tableExpression) loweredTable {
	fields := make([]loweredTableField, 0, len(table.fields))
	for _, field := range table.fields {
		lowered := loweredTableField{
			name:       field.name,
			arrayIndex: field.arrayIndex,
			key:        field.key,
			value:      field.value,
		}
		switch {
		case field.key != nil:
			lowered.kind = loweredTableFieldComputed
		case field.name != "":
			lowered.kind = loweredTableFieldNamed
		default:
			lowered.kind = loweredTableFieldArray
		}
		fields = append(fields, lowered)
	}
	return loweredTable{fields: fields}
}

func lowerIfStatement(stmt ifStatement) loweredIfStatement {
	return loweredIfStatement{
		condition: stmt.condition,
		thenBody:  stmt.thenStatements,
		elseBody:  stmt.elseStatements,
	}
}

func lowerIfExpression(expr ifExpression) loweredIfExpression {
	return loweredIfExpression{
		condition: expr.condition,
		thenValue: expr.thenValue,
		elseValue: expr.elseValue,
	}
}

func lowerAssignment(stmt assignStatement) loweredAssignment {
	targets := append([]assignTarget(nil), stmt.targets...)
	sources := append([]expression(nil), stmt.values...)
	return loweredAssignment{
		targets: targets,
		sources: sources,
		values:  lowerFixedValueList(sources, len(targets)),
	}
}

func lowerLocal(stmt localStatement) loweredLocal {
	names := append([]string(nil), stmt.names...)
	annotations := append([]*typeExpression(nil), stmt.annotations...)
	sources := append([]expression(nil), stmt.values...)
	return loweredLocal{
		names:       names,
		annotations: annotations,
		sources:     sources,
		values:      lowerFixedValueList(sources, len(names)),
	}
}

func lowerReturn(stmt returnStatement) loweredReturn {
	sources := append([]expression(nil), stmt.values...)
	return loweredReturn{
		sources: sources,
		values:  lowerOpenValueList(sources),
	}
}

func lowerBlock(stmt blockStatement) loweredBlock {
	return loweredBlock{
		body:         stmt.statements,
		lexicalScope: true,
	}
}

func lowerCallStatement(stmt term) loweredCallStatement {
	args := []expression(nil)
	var call loweredCall
	if stmt.call != nil {
		args = append(args, stmt.call.args...)
		call = lowerCall(*stmt.call)
	}
	return loweredCallStatement{
		call:           call,
		args:           args,
		discardResults: true,
		resultCount:    1,
	}
}

func lowerStatement(stmt statement) loweredStatement {
	switch {
	case stmt.local != nil:
		lowered := lowerLocal(*stmt.local)
		return loweredStatement{kind: loweredStatementLocal, local: &lowered}
	case stmt.localFunc != nil:
		return loweredStatement{kind: loweredStatementLocalFunction, localFunction: stmt.localFunc}
	case stmt.funcDecl != nil:
		return loweredStatement{kind: loweredStatementFunctionDeclaration, functionDeclaration: stmt.funcDecl}
	case stmt.assign != nil:
		lowered := lowerAssignment(*stmt.assign)
		return loweredStatement{kind: loweredStatementAssignment, assignment: &lowered}
	case stmt.call != nil:
		lowered := lowerCallStatement(*stmt.call)
		return loweredStatement{kind: loweredStatementCall, call: &lowered}
	case stmt.ifStmt != nil:
		lowered := lowerIfStatement(*stmt.ifStmt)
		return loweredStatement{kind: loweredStatementIf, ifStatement: &lowered}
	case stmt.while != nil:
		return loweredStatement{kind: loweredStatementWhile, while: stmt.while}
	case stmt.forLoop != nil:
		return loweredStatement{kind: loweredStatementNumericFor, numericFor: stmt.forLoop}
	case stmt.genericFor != nil:
		return loweredStatement{kind: loweredStatementGenericFor, genericFor: stmt.genericFor}
	case stmt.repeat != nil:
		return loweredStatement{kind: loweredStatementRepeat, repeat: stmt.repeat}
	case stmt.block != nil:
		lowered := lowerBlock(*stmt.block)
		return loweredStatement{kind: loweredStatementBlock, block: &lowered}
	case stmt.typeAlias != nil:
		return loweredStatement{kind: loweredStatementTypeAlias, typeAlias: stmt.typeAlias}
	case stmt.breaking:
		return loweredStatement{kind: loweredStatementBreak}
	case stmt.continues:
		return loweredStatement{kind: loweredStatementContinue}
	case stmt.ret != nil:
		lowered := lowerReturn(*stmt.ret)
		return loweredStatement{kind: loweredStatementReturn, ret: &lowered}
	default:
		return loweredStatement{kind: loweredStatementEmpty}
	}
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

func collectLoweredRequireRequests(prog loweredProgram) []string {
	var requests []string
	collectLoweredStatementsRequireRequests(prog.statements, &requests)
	return requests
}

func collectLoweredStatementsRequireRequests(statements []loweredStatement, requests *[]string) {
	for _, stmt := range statements {
		collectLoweredStatementRequireRequests(stmt, requests)
	}
}

func collectLoweredStatementRequireRequests(stmt loweredStatement, requests *[]string) {
	switch stmt.kind {
	case loweredStatementLocal:
		if stmt.local != nil {
			collectExpressionsRequireRequests(stmt.local.sources, requests)
		}
	case loweredStatementAssignment:
		if stmt.assignment != nil {
			collectExpressionsRequireRequests(stmt.assignment.sources, requests)
		}
	case loweredStatementCall:
		if stmt.call != nil {
			collectLoweredCallStatementRequireRequest(*stmt.call, requests)
		}
	case loweredStatementIf:
		if stmt.ifStatement != nil {
			collectExpressionRequireRequests(stmt.ifStatement.condition, requests)
			collectLoweredStatementsRequireRequests(lowerStatements(stmt.ifStatement.thenBody), requests)
			collectLoweredStatementsRequireRequests(lowerStatements(stmt.ifStatement.elseBody), requests)
		}
	case loweredStatementWhile:
		if stmt.while != nil {
			collectExpressionRequireRequests(stmt.while.condition, requests)
			collectLoweredStatementsRequireRequests(lowerStatements(stmt.while.statements), requests)
		}
	case loweredStatementNumericFor:
		if stmt.numericFor != nil {
			collectExpressionRequireRequests(stmt.numericFor.start, requests)
			collectExpressionRequireRequests(stmt.numericFor.limit, requests)
			if stmt.numericFor.step != nil {
				collectExpressionRequireRequests(*stmt.numericFor.step, requests)
			}
			collectLoweredStatementsRequireRequests(lowerStatements(stmt.numericFor.statements), requests)
		}
	case loweredStatementGenericFor:
		if stmt.genericFor != nil {
			collectExpressionsRequireRequests(stmt.genericFor.values, requests)
			collectLoweredStatementsRequireRequests(lowerStatements(stmt.genericFor.statements), requests)
		}
	case loweredStatementRepeat:
		if stmt.repeat != nil {
			collectLoweredStatementsRequireRequests(lowerStatements(stmt.repeat.statements), requests)
			collectExpressionRequireRequests(stmt.repeat.condition, requests)
		}
	case loweredStatementBlock:
		if stmt.block != nil {
			collectLoweredStatementsRequireRequests(lowerStatements(stmt.block.body), requests)
		}
	case loweredStatementReturn:
		if stmt.ret != nil {
			collectExpressionsRequireRequests(stmt.ret.sources, requests)
		}
	}
}

func collectLoweredCallStatementRequireRequest(stmt loweredCallStatement, requests *[]string) {
	collectCallRequireRequest(callExpression{
		target:   stmt.call.target,
		receiver: stmt.call.receiver,
		args:     stmt.args,
	}, requests)
}
