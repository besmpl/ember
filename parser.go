package ember

import (
	"fmt"
	"strings"
)

type program struct {
	id         syntaxID
	statements []statement
	mode       sourceMode
	nodeCount  int
}

type sourceMode string

const (
	sourceModeUnspecified sourceMode = ""
	sourceModeStrict      sourceMode = "strict"
	sourceModeNonStrict   sourceMode = "nonstrict"
	sourceModeNoCheck     sourceMode = "nocheck"
)

type statement struct {
	id         syntaxID
	local      *localStatement
	localFunc  *localFunctionStatement
	funcDecl   *functionDeclarationStatement
	assign     *assignStatement
	call       *term
	ifStmt     *ifStatement
	while      *whileStatement
	forLoop    *forStatement
	genericFor *genericForStatement
	repeat     *repeatStatement
	block      *blockStatement
	typeAlias  *typeAliasStatement
	breaking   bool
	continues  bool
	ret        *returnStatement
}

type localStatement struct {
	names       []string
	nameID      syntaxID
	nameRanges  []sourceRange
	annotations []*typeExpression
	values      []expression
}

type typeAliasStatement struct {
	id          syntaxID
	exported    bool
	name        string
	nameID      syntaxID
	start       int
	end         int
	nameStart   int
	nameEnd     int
	typeParams  []string
	typeParamID syntaxID
	typePacks   []string
	typePackID  syntaxID
	value       *typeExpression
}

type localFunctionStatement struct {
	id                 syntaxID
	functionID         int
	name               string
	nameID             syntaxID
	typeParams         []string
	typeParamID        syntaxID
	typePacks          []string
	typePackID         syntaxID
	params             []string
	paramID            syntaxID
	paramAnnotations   []*typeExpression
	variadic           bool
	variadicAnnotation *typeExpression
	returnAnnotation   *typeExpression
	statements         []statement
}

type functionDeclarationStatement struct {
	id                 syntaxID
	functionID         int
	target             assignTarget
	typeParams         []string
	typeParamID        syntaxID
	typePacks          []string
	typePackID         syntaxID
	params             []string
	paramID            syntaxID
	selfID             syntaxID
	paramAnnotations   []*typeExpression
	variadic           bool
	variadicAnnotation *typeExpression
	returnAnnotation   *typeExpression
	statements         []statement
	method             bool
}

type functionExpression struct {
	id                 syntaxID
	functionID         int
	typeParams         []string
	typeParamID        syntaxID
	typePacks          []string
	typePackID         syntaxID
	params             []string
	paramID            syntaxID
	paramAnnotations   []*typeExpression
	variadic           bool
	variadicAnnotation *typeExpression
	returnAnnotation   *typeExpression
	statements         []statement
}

type typeKind string

const (
	typeKindName            typeKind = "name"
	typeKindUnion           typeKind = "union"
	typeKindIntersection    typeKind = "intersection"
	typeKindNilable         typeKind = "nilable"
	typeKindTable           typeKind = "table"
	typeKindFunction        typeKind = "function"
	typeKindGenericFunction typeKind = "genericFunction"
	typeKindTypeof          typeKind = "typeof"
	typeKindVariadic        typeKind = "variadic"
	typeKindGenericPack     typeKind = "genericPack"
	typeKindSingleton       typeKind = "singleton"
)

type typeExpression struct {
	id          syntaxID
	start       int
	end         int
	kind        typeKind
	name        []string
	typeArgs    []*typeExpression
	types       []*typeExpression
	inner       *typeExpression
	fields      []typeField
	params      []typeFunctionParam
	returnType  *typeExpression
	typeParams  []string
	typeParamID syntaxID
	typePacks   []string
	typePackID  syntaxID
	expr        *expression
	literal     *Value
}

type typeField struct {
	access string
	name   string
	key    *typeExpression
	value  *typeExpression
}

type typeFunctionParam struct {
	name     string
	value    *typeExpression
	variadic bool
}

type assignStatement struct {
	targets []assignTarget
	values  []expression
}

type assignTarget struct {
	id        syntaxID
	start     int
	end       int
	name      string
	selectors []selector
}

type ifStatement struct {
	condition      expression
	thenStatements []statement
	elseStatements []statement
}

type ifExpression struct {
	condition expression
	thenValue expression
	elseValue expression
}

type whileStatement struct {
	condition  expression
	statements []statement
}

type forStatement struct {
	nameID     syntaxID
	name       string
	start      expression
	limit      expression
	step       *expression
	statements []statement
}

type genericForStatement struct {
	names      []string
	nameID     syntaxID
	values     []expression
	statements []statement
}

type parsedForStatement struct {
	numeric *forStatement
	generic *genericForStatement
}

type repeatStatement struct {
	statements []statement
	condition  expression
}

type blockStatement struct {
	statements []statement
}

type returnStatement struct {
	start  int
	end    int
	values []expression
}

type expression struct {
	id    syntaxID
	terms []andExpression
}

type andExpression struct {
	terms []comparisonExpression
}

type comparisonExpression struct {
	left  concatExpression
	op    comparisonOperator
	right *concatExpression
}

type comparisonOperator string

const (
	comparisonEqual        comparisonOperator = "=="
	comparisonNotEqual     comparisonOperator = "~="
	comparisonLess         comparisonOperator = "<"
	comparisonLessEqual    comparisonOperator = "<="
	comparisonGreater      comparisonOperator = ">"
	comparisonGreaterEqual comparisonOperator = ">="
)

type additiveExpression struct {
	first multiplicativeExpression
	rest  []additivePart
}

type concatExpression struct {
	first additiveExpression
	rest  []additiveExpression
}

type additivePart struct {
	op    additiveOperator
	value multiplicativeExpression
}

type additiveOperator string

const (
	additiveAdd      additiveOperator = "+"
	additiveSubtract additiveOperator = "-"
)

type multiplicativeExpression struct {
	first term
	rest  []multiplicativePart
}

type multiplicativePart struct {
	op    multiplicativeOperator
	value term
}

type multiplicativeOperator string

const (
	multiplicativeMultiply multiplicativeOperator = "*"
	multiplicativeDivide   multiplicativeOperator = "/"
	multiplicativeModulo   multiplicativeOperator = "%"
	multiplicativeFloorDiv multiplicativeOperator = "//"
)

type powerExpression struct {
	base     term
	exponent term
}

type term struct {
	id         syntaxID
	start      int
	end        int
	number     *float64
	lit        *Value
	table      *tableExpression
	function   *functionExpression
	ifExpr     *ifExpression
	call       *callExpression
	vararg     bool
	unaryNot   *term
	unaryMinus *term
	unaryLen   *term
	power      *powerExpression
	group      *expression
	cast       *typeExpression
	selectors  []selector
	name       string
}

type tableExpression struct {
	fields []tableField
}

type tableField struct {
	name       string
	arrayIndex int
	key        *expression
	value      expression
}

type callExpression struct {
	target   term
	receiver *term
	typeArgs []*typeExpression
	args     []expression
}

type selector struct {
	field string
	index *expression
}

type parserCheckpoint struct {
	pos        int
	tokenIndex int
}

type parser struct {
	source     string
	pos        int
	mode       sourceMode
	tokens     []sourceToken
	tokenIndex int
}

func (p *parser) mark() parserCheckpoint {
	return parserCheckpoint{pos: p.pos, tokenIndex: p.tokenIndex}
}

func (p *parser) restore(checkpoint parserCheckpoint) {
	p.pos = checkpoint.pos
	p.tokenIndex = checkpoint.tokenIndex
}

func (p *parser) parse() (program, error) {
	tokens, _, mode, err := lexSource(p.source)
	if err != nil {
		return program{}, err
	}
	p.mode = mode
	p.tokens = tokens

	statements, err := p.parseBlock()
	if err != nil {
		return program{}, err
	}

	p.skipSpace()
	if !p.done() {
		return program{}, p.errorf("unexpected input %q", p.source[p.pos:])
	}
	prog := program{statements: statements, mode: p.mode}
	assignProgramSyntaxIDs(&prog)
	return prog, nil
}

func (p *parser) parseBlock(stopKeywords ...string) ([]statement, error) {
	var statements []statement
	for {
		p.skipSpace()

		if p.done() {
			if len(stopKeywords) > 0 {
				return nil, p.errorf("expected %s", strings.Join(stopKeywords, " or "))
			}
			return statements, nil
		}

		for _, keyword := range stopKeywords {
			if p.matchKeyword(keyword) {
				return statements, nil
			}
		}

		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		statements = append(statements, stmt)
	}
}

func (p *parser) parseStatement() (statement, error) {
	if alias, ok, err := p.tryParseTypeAliasStatement(); ok || err != nil {
		if err != nil {
			return statement{}, err
		}
		return statement{typeAlias: alias}, nil
	}

	if p.consumeKeyword("local") {
		p.skipSpace()
		if p.consumeKeyword("function") {
			stmt, err := p.parseLocalFunctionStatement()
			if err != nil {
				return statement{}, err
			}
			return statement{localFunc: &stmt}, nil
		}

		stmt, err := p.parseLocalStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{local: &stmt}, nil
	}

	if token, ok := p.currentToken(); ok && token.matchesWordAt(p.pos, "return") {
		p.consumeKeyword("return")
		stmt, err := p.parseReturnStatement()
		if err != nil {
			return statement{}, err
		}
		stmt.start = token.start
		stmt.end = token.end
		return statement{ret: &stmt}, nil
	}

	if p.consumeKeyword("function") {
		stmt, err := p.parseFunctionDeclarationStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{funcDecl: &stmt}, nil
	}

	if p.consumeKeyword("if") {
		stmt, err := p.parseIfStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{ifStmt: &stmt}, nil
	}

	if p.consumeKeyword("while") {
		stmt, err := p.parseWhileStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{while: &stmt}, nil
	}

	if p.consumeKeyword("for") {
		stmt, err := p.parseForStatement()
		if err != nil {
			return statement{}, err
		}
		if stmt.numeric != nil {
			return statement{forLoop: stmt.numeric}, nil
		}
		return statement{genericFor: stmt.generic}, nil
	}

	if p.consumeKeyword("repeat") {
		stmt, err := p.parseRepeatStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{repeat: &stmt}, nil
	}

	if p.consumeKeyword("do") {
		stmt, err := p.parseDoBlockStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{block: &stmt}, nil
	}

	if p.consumeKeyword("break") {
		return statement{breaking: true}, nil
	}

	if p.consumeKeyword("continue") {
		return statement{continues: true}, nil
	}

	if p.currentIdentifier() {
		return p.parseIdentifierStatement()
	}

	return statement{}, p.errorf("expected statement")
}

func (p *parser) tryParseTypeAliasStatement() (*typeAliasStatement, bool, error) {
	checkpoint := p.mark()
	start := checkpoint.pos
	exported := false
	if p.consumeKeyword("export") {
		p.skipSpace()
		if !p.consumeKeyword("type") {
			p.restore(checkpoint)
			return nil, false, nil
		}
		exported = true
	} else if !p.consumeKeyword("type") {
		return nil, false, nil
	}

	p.skipSpace()
	if !p.currentIdentifier() {
		p.restore(checkpoint)
		return nil, false, nil
	}
	nameStart := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return nil, false, err
	}
	nameEnd := p.pos
	typeParams, typePacks, err := p.parseOptionalTypeParameterList()
	if err != nil {
		return nil, true, err
	}

	p.skipSpace()
	if !p.consumeByte('=') {
		p.restore(checkpoint)
		return nil, false, nil
	}

	p.skipSpace()
	value, err := p.parseType()
	if err != nil {
		return nil, true, err
	}
	return &typeAliasStatement{exported: exported, name: name, start: start, end: p.trimSourceEnd(value.end), nameStart: nameStart, nameEnd: nameEnd, typeParams: typeParams, typePacks: typePacks, value: value}, true, nil
}

func (p *parser) parseLocalFunctionStatement() (localFunctionStatement, error) {
	p.skipSpace()
	name, err := p.parseIdentifier()
	if err != nil {
		return localFunctionStatement{}, err
	}

	body, err := p.parseFunctionBody()
	if err != nil {
		return localFunctionStatement{}, err
	}

	return localFunctionStatement{
		name:               name,
		typeParams:         body.typeParams,
		typePacks:          body.typePacks,
		params:             body.params,
		paramAnnotations:   body.paramAnnotations,
		variadic:           body.variadic,
		variadicAnnotation: body.variadicAnnotation,
		returnAnnotation:   body.returnAnnotation,
		statements:         body.statements,
	}, nil
}

func (p *parser) parseFunctionDeclarationStatement() (functionDeclarationStatement, error) {
	p.skipSpace()
	target, method, err := p.parseFunctionDeclarationTarget()
	if err != nil {
		return functionDeclarationStatement{}, err
	}

	body, err := p.parseFunctionBody()
	if err != nil {
		return functionDeclarationStatement{}, err
	}

	return functionDeclarationStatement{
		target:             target,
		typeParams:         body.typeParams,
		typePacks:          body.typePacks,
		params:             body.params,
		paramAnnotations:   body.paramAnnotations,
		variadic:           body.variadic,
		variadicAnnotation: body.variadicAnnotation,
		returnAnnotation:   body.returnAnnotation,
		statements:         body.statements,
		method:             method,
	}, nil
}

func (p *parser) parseFunctionDeclarationTarget() (assignTarget, bool, error) {
	start := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return assignTarget{}, false, err
	}
	target := assignTarget{start: start, end: p.pos, name: name}

	for {
		p.skipSpace()
		if p.consumeByte('.') {
			field, err := p.parseIdentifier()
			if err != nil {
				return assignTarget{}, false, err
			}
			target.selectors = append(target.selectors, selector{field: field})
			continue
		}
		if p.consumeByte(':') {
			field, err := p.parseIdentifier()
			if err != nil {
				return assignTarget{}, false, err
			}
			target.selectors = append(target.selectors, selector{field: field})
			return target, true, nil
		}
		return target, false, nil
	}
}

func (p *parser) parseFunctionExpression() (functionExpression, error) {
	if !p.consumeKeyword("function") {
		return functionExpression{}, p.errorf("expected function")
	}

	return p.parseFunctionBody()
}

func (p *parser) parseFunctionBody() (functionExpression, error) {
	typeParams, typePacks, err := p.parseOptionalTypeParameterList()
	if err != nil {
		return functionExpression{}, err
	}

	params, annotations, variadic, variadicAnnotation, err := p.parseParameterList()
	if err != nil {
		return functionExpression{}, err
	}
	returnAnnotation, err := p.parseOptionalTypeAnnotation()
	if err != nil {
		return functionExpression{}, err
	}

	statements, err := p.parseBlock("end")
	if err != nil {
		return functionExpression{}, err
	}
	p.skipSpace()
	if !p.consumeKeyword("end") {
		return functionExpression{}, p.errorf("expected end")
	}

	return functionExpression{
		typeParams:         typeParams,
		typePacks:          typePacks,
		params:             params,
		paramAnnotations:   annotations,
		variadic:           variadic,
		variadicAnnotation: variadicAnnotation,
		returnAnnotation:   returnAnnotation,
		statements:         statements,
	}, nil
}

func (p *parser) parseParameterList() ([]string, []*typeExpression, bool, *typeExpression, error) {
	p.skipSpace()
	if !p.consumeByte('(') {
		return nil, nil, false, nil, p.errorf("expected (")
	}
	p.skipSpace()
	if p.consumeByte(')') {
		return nil, nil, false, nil, nil
	}

	var params []string
	var annotations []*typeExpression
	for {
		if p.consumeString("...") {
			annotation, err := p.parseOptionalTypeAnnotation()
			if err != nil {
				return nil, nil, false, nil, err
			}
			p.skipSpace()
			if !p.consumeByte(')') {
				return nil, nil, false, nil, p.errorf("expected )")
			}
			return params, annotations, true, annotation, nil
		}

		param, err := p.parseIdentifier()
		if err != nil {
			return nil, nil, false, nil, err
		}
		annotation, err := p.parseOptionalTypeAnnotation()
		if err != nil {
			return nil, nil, false, nil, err
		}
		params = append(params, param)
		annotations = append(annotations, annotation)

		p.skipSpace()
		if p.consumeByte(')') {
			return params, annotations, false, nil, nil
		}
		if !p.consumeByte(',') {
			return nil, nil, false, nil, p.errorf("expected , or )")
		}
		p.skipSpace()
	}
}

func (p *parser) parseIdentifierStatement() (statement, error) {
	value, err := p.parseIdentifierStatementTerm()
	if err != nil {
		return statement{}, err
	}
	if value.call != nil {
		return identifierCallStatement(value), nil
	}

	target := assignTargetFromIdentifierTerm(value)
	targets := []assignTarget{target}
	for {
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
		target, err := p.parseAssignTarget()
		if err != nil {
			return statement{}, err
		}
		targets = append(targets, target)
	}

	if !p.consumeByte('=') {
		return statement{}, p.errorf("expected =")
	}
	values, err := p.parseExpressionList()
	if err != nil {
		return statement{}, err
	}
	return statement{assign: &assignStatement{targets: targets, values: values}}, nil
}

// Keep call-term address-taking out of the assignment path so ordinary terms stay stack-allocated.
func identifierCallStatement(value term) statement {
	return statement{call: &value}
}

func assignTargetFromIdentifierTerm(value term) assignTarget {
	return assignTarget{
		start:     value.start,
		end:       value.end,
		name:      value.name,
		selectors: value.selectors,
	}
}

func (p *parser) parseReturnStatement() (returnStatement, error) {
	p.skipSpace()
	if p.done() || p.matchKeyword("end") {
		return returnStatement{}, nil
	}
	values, err := p.parseExpressionList()
	if err != nil {
		return returnStatement{}, err
	}
	return returnStatement{values: values}, nil
}

func (p *parser) parseExpressionList() ([]expression, error) {
	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	values := []expression{value}
	for {
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
		value, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}

	return values, nil
}

func (p *parser) parseIfStatement() (ifStatement, error) {
	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return ifStatement{}, err
	}

	p.skipSpace()
	if !p.consumeKeyword("then") {
		return ifStatement{}, p.errorf("expected then")
	}

	thenStatements, err := p.parseBlock("elseif", "else", "end")
	if err != nil {
		return ifStatement{}, err
	}

	var elseStatements []statement
	p.skipSpace()
	if p.consumeKeyword("elseif") {
		nested, err := p.parseIfStatement()
		if err != nil {
			return ifStatement{}, err
		}
		elseStatements = []statement{{ifStmt: &nested}}
		return ifStatement{
			condition:      condition,
			thenStatements: thenStatements,
			elseStatements: elseStatements,
		}, nil
	} else if p.consumeKeyword("else") {
		elseStatements, err = p.parseBlock("end")
		if err != nil {
			return ifStatement{}, err
		}
		p.skipSpace()
	}

	if !p.consumeKeyword("end") {
		return ifStatement{}, p.errorf("expected end")
	}

	return ifStatement{
		condition:      condition,
		thenStatements: thenStatements,
		elseStatements: elseStatements,
	}, nil
}

func (p *parser) parseWhileStatement() (whileStatement, error) {
	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return whileStatement{}, err
	}

	p.skipSpace()
	if !p.consumeKeyword("do") {
		return whileStatement{}, p.errorf("expected do")
	}

	statements, err := p.parseBlock("end")
	if err != nil {
		return whileStatement{}, err
	}

	p.skipSpace()
	if !p.consumeKeyword("end") {
		return whileStatement{}, p.errorf("expected end")
	}

	return whileStatement{
		condition:  condition,
		statements: statements,
	}, nil
}

func (p *parser) parseForStatement() (parsedForStatement, error) {
	p.skipSpace()
	name, err := p.parseIdentifier()
	if err != nil {
		return parsedForStatement{}, err
	}
	names := []string{name}

	for {
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
		name, err := p.parseIdentifier()
		if err != nil {
			return parsedForStatement{}, err
		}
		names = append(names, name)
	}

	p.skipSpace()
	if p.consumeKeyword("in") {
		p.skipSpace()
		values, err := p.parseExpressionList()
		if err != nil {
			return parsedForStatement{}, err
		}
		p.skipSpace()
		if !p.consumeKeyword("do") {
			return parsedForStatement{}, p.errorf("expected do")
		}

		statements, err := p.parseBlock("end")
		if err != nil {
			return parsedForStatement{}, err
		}

		p.skipSpace()
		if !p.consumeKeyword("end") {
			return parsedForStatement{}, p.errorf("expected end")
		}

		return parsedForStatement{generic: &genericForStatement{
			names:      names,
			values:     values,
			statements: statements,
		}}, nil
	}

	if len(names) != 1 {
		return parsedForStatement{}, p.errorf("expected in")
	}

	if !p.consumeByte('=') {
		return parsedForStatement{}, p.errorf("expected =")
	}

	p.skipSpace()
	start, err := p.parseExpression()
	if err != nil {
		return parsedForStatement{}, err
	}

	p.skipSpace()
	if !p.consumeByte(',') {
		return parsedForStatement{}, p.errorf("expected ,")
	}

	p.skipSpace()
	limit, err := p.parseExpression()
	if err != nil {
		return parsedForStatement{}, err
	}

	var step *expression
	p.skipSpace()
	if p.consumeByte(',') {
		p.skipSpace()
		value, err := p.parseExpression()
		if err != nil {
			return parsedForStatement{}, err
		}
		step = &value
	}

	p.skipSpace()
	if !p.consumeKeyword("do") {
		return parsedForStatement{}, p.errorf("expected do")
	}

	statements, err := p.parseBlock("end")
	if err != nil {
		return parsedForStatement{}, err
	}

	p.skipSpace()
	if !p.consumeKeyword("end") {
		return parsedForStatement{}, p.errorf("expected end")
	}

	return parsedForStatement{numeric: &forStatement{
		name:       name,
		start:      start,
		limit:      limit,
		step:       step,
		statements: statements,
	}}, nil
}

func (p *parser) parseRepeatStatement() (repeatStatement, error) {
	statements, err := p.parseBlock("until")
	if err != nil {
		return repeatStatement{}, err
	}

	p.skipSpace()
	if !p.consumeKeyword("until") {
		return repeatStatement{}, p.errorf("expected until")
	}

	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return repeatStatement{}, err
	}

	return repeatStatement{
		statements: statements,
		condition:  condition,
	}, nil
}

func (p *parser) parseDoBlockStatement() (blockStatement, error) {
	statements, err := p.parseBlock("end")
	if err != nil {
		return blockStatement{}, err
	}

	p.skipSpace()
	if !p.consumeKeyword("end") {
		return blockStatement{}, p.errorf("expected end")
	}

	return blockStatement{statements: statements}, nil
}

func (p *parser) parseLocalStatement() (localStatement, error) {
	p.skipSpace()
	nameStart := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return localStatement{}, err
	}
	nameEnd := p.pos
	annotation, err := p.parseOptionalTypeAnnotation()
	if err != nil {
		return localStatement{}, err
	}
	names := []string{name}
	nameRanges := []sourceRange{{start: nameStart, end: nameEnd}}
	annotations := []*typeExpression{annotation}

	for {
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
		nameStart := p.pos
		name, err := p.parseIdentifier()
		if err != nil {
			return localStatement{}, err
		}
		nameEnd := p.pos
		annotation, err := p.parseOptionalTypeAnnotation()
		if err != nil {
			return localStatement{}, err
		}
		names = append(names, name)
		nameRanges = append(nameRanges, sourceRange{start: nameStart, end: nameEnd})
		annotations = append(annotations, annotation)
	}

	p.skipSpace()
	if !p.consumeByte('=') {
		return localStatement{}, p.errorf("expected =")
	}

	p.skipSpace()
	values, err := p.parseExpressionList()
	if err != nil {
		return localStatement{}, err
	}

	return localStatement{names: names, nameRanges: nameRanges, annotations: annotations, values: values}, nil
}

func (p *parser) parseOptionalTypeAnnotation() (*typeExpression, error) {
	p.skipSpace()
	if !p.consumeByte(':') {
		return nil, nil
	}

	p.skipSpace()
	return p.parseType()
}

func (p *parser) parseType() (*typeExpression, error) {
	first, err := p.parseIntersectionType()
	if err != nil {
		return nil, err
	}
	types := []*typeExpression{first}

	for {
		p.skipSpace()
		if !p.consumeByte('|') {
			if len(types) == 1 {
				return first, nil
			}
			return &typeExpression{
				start: first.start,
				end:   types[len(types)-1].end,
				kind:  typeKindUnion,
				types: types,
			}, nil
		}
		p.skipSpace()
		next, err := p.parseIntersectionType()
		if err != nil {
			return nil, err
		}
		types = append(types, next)
	}
}

func (p *parser) parseIntersectionType() (*typeExpression, error) {
	first, err := p.parsePostfixType()
	if err != nil {
		return nil, err
	}
	types := []*typeExpression{first}

	for {
		p.skipSpace()
		if !p.consumeByte('&') {
			if len(types) == 1 {
				return first, nil
			}
			return &typeExpression{
				start: first.start,
				end:   types[len(types)-1].end,
				kind:  typeKindIntersection,
				types: types,
			}, nil
		}
		p.skipSpace()
		next, err := p.parsePostfixType()
		if err != nil {
			return nil, err
		}
		types = append(types, next)
	}
}

func (p *parser) parsePostfixType() (*typeExpression, error) {
	value, err := p.parsePrimaryType()
	if err != nil {
		return nil, err
	}

	for {
		p.skipSpace()
		if p.consumeString("...") {
			value.kind = typeKindGenericPack
			value.end = p.pos
			return value, nil
		}
		if !p.consumeByte('?') {
			return value, nil
		}
		value = &typeExpression{
			start: value.start,
			end:   p.pos,
			kind:  typeKindNilable,
			inner: value,
		}
	}
}

func (p *parser) parsePrimaryType() (*typeExpression, error) {
	p.skipSpace()
	if value, ok, err := p.tryParseTypeofType(); ok {
		return value, err
	}
	start := p.pos
	if p.consumeByte('<') {
		return p.parseGenericFunctionType(start)
	}
	if p.consumeByte('{') {
		return p.parseTableTypeBody(start)
	}
	if p.consumeByte('(') {
		return p.parseParenthesizedOrFunctionType(start)
	}
	if p.consumeKeyword("nil") {
		return &typeExpression{start: start, end: p.pos, kind: typeKindName, name: []string{"nil"}}, nil
	}
	if p.consumeKeyword("true") {
		v := BoolValue(true)
		return &typeExpression{start: start, end: p.pos, kind: typeKindSingleton, literal: &v}, nil
	}
	if p.consumeKeyword("false") {
		v := BoolValue(false)
		return &typeExpression{start: start, end: p.pos, kind: typeKindSingleton, literal: &v}, nil
	}
	if p.currentDoubleQuotedString() {
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		v := StringValue(s)
		return &typeExpression{start: start, end: p.pos, kind: typeKindSingleton, literal: &v}, nil
	}
	if p.currentTokenKind(tokenNumber) {
		n, err := p.parseNumber()
		if err != nil {
			return nil, err
		}
		v := NumberValue(n)
		return &typeExpression{start: start, end: p.pos, kind: typeKindSingleton, literal: &v}, nil
	}
	if p.consumeString("...") {
		p.skipSpace()
		if p.done() || p.currentSymbol(")") || p.currentSymbol(",") {
			return &typeExpression{start: start, end: p.pos, kind: typeKindVariadic}, nil
		}
		value, err := p.parseType()
		if err != nil {
			return nil, err
		}
		return &typeExpression{start: start, end: value.end, kind: typeKindVariadic, inner: value}, nil
	}
	return p.parseTypeName()
}

func (p *parser) tryParseTypeofType() (*typeExpression, bool, error) {
	checkpoint := p.mark()
	start := checkpoint.pos
	if !p.consumeKeyword("typeof") {
		return nil, false, nil
	}

	p.skipSpace()
	if !p.consumeByte('(') {
		p.restore(checkpoint)
		return nil, false, nil
	}

	p.skipSpace()
	expr, err := p.parseExpression()
	if err != nil {
		return nil, true, err
	}
	p.skipSpace()
	if !p.consumeByte(')') {
		return nil, true, p.errorf("expected )")
	}
	return &typeExpression{start: start, end: p.pos, kind: typeKindTypeof, expr: &expr}, true, nil
}

func (p *parser) parseGenericFunctionType(start int) (*typeExpression, error) {
	typeParams, typePacks, err := p.parseTypeParameterListAfterOpen()
	if err != nil {
		return nil, err
	}

	p.skipSpace()
	if !p.consumeByte('(') {
		return nil, p.errorf("expected (")
	}
	value, err := p.parseParenthesizedOrFunctionType(p.pos - 1)
	if err != nil {
		return nil, err
	}
	value.start = start
	value.kind = typeKindGenericFunction
	value.typeParams = typeParams
	value.typePacks = typePacks
	return value, nil
}

func (p *parser) parseParenthesizedOrFunctionType(start int) (*typeExpression, error) {
	p.skipSpace()
	var params []typeFunctionParam
	if !p.consumeByte(')') {
		for {
			param, err := p.parseFunctionTypeArgument()
			if err != nil {
				return nil, err
			}
			params = append(params, param)
			p.skipSpace()
			if p.consumeByte(')') {
				break
			}
			if !p.consumeByte(',') {
				return nil, p.errorf("expected , or )")
			}
			p.skipSpace()
		}
	}
	closeEnd := p.pos

	p.skipSpace()
	if !p.consumeString("->") {
		if len(params) == 1 && params[0].name == "" && !params[0].variadic {
			return params[0].value, nil
		}
		return &typeExpression{start: start, end: closeEnd, kind: typeKindFunction, params: params}, nil
	}
	p.skipSpace()
	returnType, err := p.parseType()
	if err != nil {
		return nil, err
	}
	return &typeExpression{start: start, end: returnType.end, kind: typeKindFunction, params: params, returnType: returnType}, nil
}

func (p *parser) parseFunctionTypeArgument() (typeFunctionParam, error) {
	p.skipSpace()
	start := p.pos
	if p.consumeString("...") {
		p.skipSpace()
		if p.consumeByte(':') {
			p.skipSpace()
			value, err := p.parseType()
			if err != nil {
				return typeFunctionParam{}, err
			}
			return typeFunctionParam{value: value, variadic: true}, nil
		}
		if p.done() || p.currentSymbol(")") || p.currentSymbol(",") {
			return typeFunctionParam{variadic: true}, nil
		}
		value, err := p.parseType()
		if err != nil {
			return typeFunctionParam{}, err
		}
		return typeFunctionParam{
			value: &typeExpression{start: start, end: value.end, kind: typeKindVariadic, inner: value},
		}, nil
	}

	if p.currentIdentifier() {
		checkpoint := p.mark()
		name, err := p.parseIdentifier()
		if err != nil {
			return typeFunctionParam{}, err
		}
		p.skipSpace()
		if p.consumeByte(':') {
			p.skipSpace()
			value, err := p.parseType()
			if err != nil {
				return typeFunctionParam{}, err
			}
			return typeFunctionParam{name: name, value: value}, nil
		}
		p.restore(checkpoint)
	}

	value, err := p.parseType()
	if err != nil {
		return typeFunctionParam{}, err
	}
	return typeFunctionParam{value: value}, nil
}

func (p *parser) parseTableTypeBody(start int) (*typeExpression, error) {
	p.skipSpace()
	if p.consumeByte('}') {
		return &typeExpression{start: start, end: p.pos, kind: typeKindTable}, nil
	}

	var fields []typeField
	for {
		access := p.parseOptionalTableFieldAccess()
		if p.consumeByte('[') {
			p.skipSpace()
			key, err := p.parseType()
			if err != nil {
				return nil, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return nil, p.errorf("expected ]")
			}
			p.skipSpace()
			if !p.consumeByte(':') {
				return nil, p.errorf("expected :")
			}
			p.skipSpace()
			value, err := p.parseType()
			if err != nil {
				return nil, err
			}
			fields = append(fields, typeField{access: access, key: key, value: value})
		} else if p.currentIdentifier() {
			fieldCheckpoint := p.mark()
			name, err := p.parseIdentifier()
			if err != nil {
				return nil, err
			}
			p.skipSpace()
			if p.consumeByte(':') {
				p.skipSpace()
				value, err := p.parseType()
				if err != nil {
					return nil, err
				}
				fields = append(fields, typeField{access: access, name: name, value: value})
			} else {
				if access != "" {
					return nil, p.errorf("expected :")
				}
				p.restore(fieldCheckpoint)
				value, err := p.parseType()
				if err != nil {
					return nil, err
				}
				fields = append(fields, typeField{value: value})
			}
		} else {
			if access != "" {
				return nil, p.errorf("expected table property or indexer")
			}
			value, err := p.parseType()
			if err != nil {
				return nil, err
			}
			fields = append(fields, typeField{value: value})
		}

		p.skipSpace()
		if p.consumeByte('}') {
			return &typeExpression{start: start, end: p.pos, kind: typeKindTable, fields: fields}, nil
		}
		if !p.consumeByte(',') && !p.consumeByte(';') {
			return nil, p.errorf("expected , ; or }")
		}
		p.skipSpace()
		if p.consumeByte('}') {
			return &typeExpression{start: start, end: p.pos, kind: typeKindTable, fields: fields}, nil
		}
	}
}

func (p *parser) parseOptionalTableFieldAccess() string {
	checkpoint := p.mark()
	var access string
	if p.consumeKeyword("read") {
		access = "read"
	} else if p.consumeKeyword("write") {
		access = "write"
	} else {
		return ""
	}

	p.skipSpace()
	if p.currentSymbol("[") || p.currentIdentifier() {
		return access
	}

	p.restore(checkpoint)
	return ""
}

func (p *parser) parseTypeName() (*typeExpression, error) {
	start := p.pos
	name, err := p.parseTypeNamePart()
	if err != nil {
		return nil, err
	}
	parts := []string{name}
	typeArgs, err := p.parseOptionalTypeArguments()
	if err != nil {
		return nil, err
	}
	end := p.pos

	for {
		p.skipSpace()
		if p.currentSymbol("...") {
			return &typeExpression{start: start, end: end, kind: typeKindName, name: parts, typeArgs: typeArgs}, nil
		}
		if !p.consumeByte('.') {
			return &typeExpression{start: start, end: end, kind: typeKindName, name: parts, typeArgs: typeArgs}, nil
		}
		p.skipSpace()
		part, err := p.parseTypeNamePart()
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
		nextArgs, err := p.parseOptionalTypeArguments()
		if err != nil {
			return nil, err
		}
		if len(nextArgs) > 0 {
			typeArgs = nextArgs
		}
		end = p.pos
	}
}

func (p *parser) parseOptionalTypeParameterList() ([]string, []string, error) {
	p.skipSpace()
	if !p.consumeByte('<') {
		return nil, nil, nil
	}

	return p.parseTypeParameterListAfterOpen()
}

func (p *parser) parseTypeParameterListAfterOpen() ([]string, []string, error) {
	var params []string
	var packs []string
	for {
		p.skipSpace()
		param, err := p.parseIdentifier()
		if err != nil {
			return nil, nil, err
		}
		if p.consumeString("...") {
			packs = append(packs, param)
		} else {
			params = append(params, param)
		}
		p.skipSpace()
		if p.consumeByte('>') {
			return params, packs, nil
		}
		if !p.consumeByte(',') {
			return nil, nil, p.errorf("expected , or >")
		}
	}
}

func (p *parser) parseTypeNamePart() (string, error) {
	return p.parseIdentifier()
}

func (p *parser) parseOptionalTypeArguments() ([]*typeExpression, error) {
	p.skipSpace()
	if !p.consumeByte('<') {
		return nil, nil
	}

	var args []*typeExpression
	for {
		p.skipSpace()
		arg, err := p.parseType()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		p.skipSpace()
		if p.consumeByte('>') {
			return args, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("expected , or >")
		}
	}
}

func (p *parser) parseAssignTarget() (assignTarget, error) {
	start := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return assignTarget{}, err
	}

	target := assignTarget{start: start, end: p.pos, name: name}

	for {
		p.skipSpace()
		if p.consumeByte('.') {
			field, err := p.parseIdentifier()
			if err != nil {
				return assignTarget{}, err
			}
			target.selectors = append(target.selectors, selector{field: field})
			target.end = p.pos
			continue
		}
		if p.consumeByte('[') {
			index, err := p.parseExpression()
			if err != nil {
				return assignTarget{}, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return assignTarget{}, p.errorf("expected ]")
			}
			target.selectors = append(target.selectors, selector{index: &index})
			target.end = p.pos
			continue
		}
		break
	}

	return target, nil
}

func (p *parser) parseExpression() (expression, error) {
	first, err := p.parseAndExpression()
	if err != nil {
		return expression{}, err
	}

	expr := expression{terms: []andExpression{first}}

	for {
		p.skipSpace()
		if !p.consumeKeyword("or") {
			break
		}

		next, err := p.parseAndExpression()
		if err != nil {
			return expression{}, err
		}
		expr.terms = append(expr.terms, next)
	}

	return expr, nil
}

func (p *parser) parseAndExpression() (andExpression, error) {
	first, err := p.parseComparisonExpression()
	if err != nil {
		return andExpression{}, err
	}

	expr := andExpression{terms: []comparisonExpression{first}}

	for {
		p.skipSpace()
		if !p.consumeKeyword("and") {
			break
		}

		next, err := p.parseComparisonExpression()
		if err != nil {
			return andExpression{}, err
		}
		expr.terms = append(expr.terms, next)
	}

	return expr, nil
}

func (p *parser) parseComparisonExpression() (comparisonExpression, error) {
	left, err := p.parseConcatExpression()
	if err != nil {
		return comparisonExpression{}, err
	}

	expr := comparisonExpression{left: left}

	p.skipSpace()
	var op comparisonOperator
	if p.consumeString("==") {
		op = comparisonEqual
	} else if p.consumeString("~=") {
		op = comparisonNotEqual
	} else if p.consumeString("<=") {
		op = comparisonLessEqual
	} else if p.consumeString(">=") {
		op = comparisonGreaterEqual
	} else if p.consumeString("<") {
		op = comparisonLess
	} else if p.consumeString(">") {
		op = comparisonGreater
	}
	if op == "" {
		return expr, nil
	}

	p.skipSpace()
	right, err := p.parseConcatExpression()
	if err != nil {
		return comparisonExpression{}, err
	}
	expr.op = op
	expr.right = &right
	return expr, nil
}

func (p *parser) parseConcatExpression() (concatExpression, error) {
	p.skipSpace()
	first, err := p.parseAdditiveExpression()
	if err != nil {
		return concatExpression{}, err
	}

	expr := concatExpression{first: first}

	for {
		p.skipSpace()
		if !p.consumeString("..") {
			break
		}

		p.skipSpace()
		next, err := p.parseAdditiveExpression()
		if err != nil {
			return concatExpression{}, err
		}
		expr.rest = append(expr.rest, next)
	}

	return expr, nil
}

func (p *parser) parseAdditiveExpression() (additiveExpression, error) {
	p.skipSpace()
	first, err := p.parseMultiplicativeExpression()
	if err != nil {
		return additiveExpression{}, err
	}

	expr := additiveExpression{first: first}

	for {
		p.skipSpace()
		var op additiveOperator
		if p.consumeByte('+') {
			op = additiveAdd
		} else if p.consumeByte('-') {
			op = additiveSubtract
		} else {
			break
		}

		p.skipSpace()
		next, err := p.parseMultiplicativeExpression()
		if err != nil {
			return additiveExpression{}, err
		}
		expr.rest = append(expr.rest, additivePart{op: op, value: next})
	}

	return expr, nil
}

func (p *parser) parseMultiplicativeExpression() (multiplicativeExpression, error) {
	p.skipSpace()
	first, err := p.parseTerm()
	if err != nil {
		return multiplicativeExpression{}, err
	}

	expr := multiplicativeExpression{first: first}

	for {
		p.skipSpace()
		var op multiplicativeOperator
		if p.consumeByte('*') {
			op = multiplicativeMultiply
		} else if p.consumeString("//") {
			op = multiplicativeFloorDiv
		} else if p.consumeByte('/') {
			op = multiplicativeDivide
		} else if p.consumeByte('%') {
			op = multiplicativeModulo
		} else {
			break
		}

		p.skipSpace()
		next, err := p.parseTerm()
		if err != nil {
			return multiplicativeExpression{}, err
		}
		expr.rest = append(expr.rest, multiplicativePart{op: op, value: next})
	}

	return expr, nil
}

func (p *parser) parseTerm() (term, error) {
	p.skipSpace()
	start := p.pos
	if p.consumeKeyword("not") {
		value, err := p.parseTerm()
		if err != nil {
			return term{}, err
		}
		return term{start: start, end: value.end, unaryNot: &value}, nil
	}
	if p.consumeByte('-') {
		value, err := p.parseTerm()
		if err != nil {
			return term{}, err
		}
		return term{start: start, end: value.end, unaryMinus: &value}, nil
	}
	if p.consumeByte('#') {
		value, err := p.parseTerm()
		if err != nil {
			return term{}, err
		}
		return term{start: start, end: value.end, unaryLen: &value}, nil
	}

	value, err := p.parsePrimaryTerm()
	if err != nil {
		return term{}, err
	}
	value, err = p.parseTermSuffixes(value)
	if err != nil {
		return term{}, err
	}
	p.skipSpace()
	if p.consumeByte('^') {
		exponent, err := p.parseTerm()
		if err != nil {
			return term{}, err
		}
		return term{start: value.start, end: exponent.end, power: &powerExpression{base: value, exponent: exponent}}, nil
	}
	return value, nil
}

func (p *parser) parseIdentifierStatementTerm() (term, error) {
	p.skipSpace()
	start := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return term{}, err
	}
	value := term{start: start, end: p.pos, name: name}
	return p.parseTermSuffixesWithCasts(value, false)
}

func (p *parser) parsePrimaryTerm() (term, error) {
	p.skipSpace()
	start := p.pos
	if p.consumeByte('(') {
		value, err := p.parseExpression()
		if err != nil {
			return term{}, err
		}
		p.skipSpace()
		if !p.consumeByte(')') {
			return term{}, p.errorf("expected )")
		}
		return term{start: start, end: p.pos, group: &value}, nil
	}
	if p.matchKeyword("if") {
		ifExpr, err := p.parseIfExpression()
		if err != nil {
			return term{}, err
		}
		return term{start: start, end: p.pos, ifExpr: &ifExpr}, nil
	}
	if p.consumeKeyword("nil") {
		v := NilValue()
		return term{start: start, end: p.pos, lit: &v}, nil
	}
	if p.consumeKeyword("true") {
		v := BoolValue(true)
		return term{start: start, end: p.pos, lit: &v}, nil
	}
	if p.consumeKeyword("false") {
		v := BoolValue(false)
		return term{start: start, end: p.pos, lit: &v}, nil
	}
	if p.consumeString("...") {
		return term{start: start, end: p.pos, vararg: true}, nil
	}
	if p.matchKeyword("function") {
		fn, err := p.parseFunctionExpression()
		if err != nil {
			return term{}, err
		}
		return term{start: start, end: p.pos, function: &fn}, nil
	}
	if p.currentDoubleQuotedString() {
		s, err := p.parseString()
		if err != nil {
			return term{}, err
		}
		v := StringValue(s)
		return term{start: start, end: p.pos, lit: &v}, nil
	}
	if p.currentSymbol("{") {
		table, err := p.parseTable()
		if err != nil {
			return term{}, err
		}
		return term{start: start, end: p.pos, table: &table}, nil
	}
	if p.currentTokenKind(tokenNumber) {
		n, err := p.parseNumber()
		if err != nil {
			return term{}, err
		}
		return term{start: start, end: p.pos, number: &n}, nil
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return term{}, err
	}
	return term{start: start, end: p.pos, name: name}, nil
}

func (p *parser) parseIfExpression() (ifExpression, error) {
	if !p.consumeKeyword("if") {
		return ifExpression{}, p.errorf("expected if")
	}
	return p.parseIfExpressionBody()
}

func (p *parser) parseIfExpressionBody() (ifExpression, error) {
	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return ifExpression{}, err
	}

	p.skipSpace()
	if !p.consumeKeyword("then") {
		return ifExpression{}, p.errorf("expected then")
	}

	p.skipSpace()
	thenValue, err := p.parseExpression()
	if err != nil {
		return ifExpression{}, err
	}

	p.skipSpace()
	var elseValue expression
	if p.consumeKeyword("elseif") {
		nested, err := p.parseIfExpressionBody()
		if err != nil {
			return ifExpression{}, err
		}
		elseValue = expressionFromTerm(term{ifExpr: &nested})
	} else if p.consumeKeyword("else") {
		p.skipSpace()
		elseValue, err = p.parseExpression()
		if err != nil {
			return ifExpression{}, err
		}
	} else {
		return ifExpression{}, p.errorf("expected elseif or else")
	}

	return ifExpression{
		condition: condition,
		thenValue: thenValue,
		elseValue: elseValue,
	}, nil
}

func expressionFromTerm(value term) expression {
	return expression{terms: []andExpression{{
		terms: []comparisonExpression{{
			left: concatExpression{
				first: additiveExpression{
					first: multiplicativeExpression{
						first: value,
					},
				},
			},
		}},
	}}}
}

func (p *parser) parseTermSuffixes(value term) (term, error) {
	return p.parseTermSuffixesWithCasts(value, true)
}

func (p *parser) parseTermSuffixesWithCasts(value term, allowCasts bool) (term, error) {
	for {
		p.skipSpace()
		if p.currentSymbol("..") {
			return value, nil
		}
		if allowCasts && p.consumeString("::") {
			p.skipSpace()
			cast, err := p.parseType()
			if err != nil {
				return term{}, err
			}
			value.cast = cast
			value.end = cast.end
			continue
		}
		if !allowCasts && p.currentSymbol("::") {
			return value, nil
		}
		if p.consumeByte('.') {
			field, err := p.parseIdentifier()
			if err != nil {
				return term{}, err
			}
			value.selectors = append(value.selectors, selector{field: field})
			value.end = p.pos
			continue
		}
		if p.consumeByte('[') {
			index, err := p.parseExpression()
			if err != nil {
				return term{}, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return term{}, p.errorf("expected ]")
			}
			value.selectors = append(value.selectors, selector{index: &index})
			value.end = p.pos
			continue
		}
		typeArgs, err := p.parseOptionalCallTypeArguments()
		if err != nil {
			return term{}, err
		}
		if p.consumeByte('(') {
			callStart := value.start
			args, err := p.parseArguments()
			if err != nil {
				return term{}, err
			}
			value = term{start: callStart, end: p.pos, call: &callExpression{target: value, typeArgs: typeArgs, args: args}}
			continue
		}
		if len(typeArgs) > 0 {
			return term{}, p.errorf("expected (")
		}
		if p.consumeByte(':') {
			method, err := p.parseIdentifier()
			if err != nil {
				return term{}, err
			}
			methodEnd := p.pos
			p.skipSpace()
			typeArgs, err := p.parseOptionalCallTypeArguments()
			if err != nil {
				return term{}, err
			}
			if !p.consumeByte('(') {
				return term{}, p.errorf("expected (")
			}
			callStart := value.start
			args, err := p.parseArguments()
			if err != nil {
				return term{}, err
			}
			receiver := value
			target := value
			target.selectors = append(target.selectors, selector{field: method})
			target.end = methodEnd
			value = term{start: callStart, end: p.pos, call: &callExpression{
				target:   target,
				receiver: &receiver,
				typeArgs: typeArgs,
				args:     args,
			}}
			continue
		}
		return value, nil
	}
}

func (p *parser) parseOptionalCallTypeArguments() ([]*typeExpression, error) {
	p.skipSpace()
	if !p.consumeString("<<") {
		return nil, nil
	}

	var args []*typeExpression
	for {
		p.skipSpace()
		arg, err := p.parseType()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)

		p.skipSpace()
		if p.consumeString(">>") {
			return args, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("expected , or >>")
		}
	}
}

func (p *parser) parseTable() (tableExpression, error) {
	if !p.consumeByte('{') {
		return tableExpression{}, p.errorf("expected table")
	}

	var table tableExpression
	p.skipSpace()
	if p.consumeByte('}') {
		return table, nil
	}

	arrayIndex := 1
	for {
		p.skipSpace()
		if p.consumeByte('[') {
			key, err := p.parseExpression()
			if err != nil {
				return tableExpression{}, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return tableExpression{}, p.errorf("expected ]")
			}
			p.skipSpace()
			if !p.consumeByte('=') {
				return tableExpression{}, p.errorf("expected =")
			}
			p.skipSpace()
			value, err := p.parseExpression()
			if err != nil {
				return tableExpression{}, err
			}
			table.fields = append(table.fields, tableField{key: &key, value: value})
			done, err := p.finishTableField()
			if err != nil {
				return tableExpression{}, err
			}
			if done {
				return table, nil
			}
			continue
		}

		if p.currentIdentifier() {
			fieldCheckpoint := p.mark()
			name, err := p.parseIdentifier()
			if err != nil {
				return tableExpression{}, err
			}

			p.skipSpace()
			if p.consumeByte('=') {
				value, err := p.parseExpression()
				if err != nil {
					return tableExpression{}, err
				}
				table.fields = append(table.fields, tableField{name: name, value: value})
				done, err := p.finishTableField()
				if err != nil {
					return tableExpression{}, err
				}
				if done {
					return table, nil
				}
				continue
			}
			p.restore(fieldCheckpoint)
		}

		value, err := p.parseExpression()
		if err != nil {
			return tableExpression{}, err
		}
		table.fields = append(table.fields, tableField{arrayIndex: arrayIndex, value: value})
		arrayIndex++
		done, err := p.finishTableField()
		if err != nil {
			return tableExpression{}, err
		}
		if done {
			return table, nil
		}
	}
}

func (p *parser) finishTableField() (bool, error) {
	p.skipSpace()
	if p.consumeByte('}') {
		return true, nil
	}
	if !p.consumeByte(',') {
		return false, p.errorf("expected , or }")
	}
	p.skipSpace()
	return p.consumeByte('}'), nil
}

func (p *parser) parseArguments() ([]expression, error) {
	p.skipSpace()
	if p.consumeByte(')') {
		return nil, nil
	}

	var args []expression
	for {
		arg, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)

		p.skipSpace()
		if p.consumeByte(')') {
			return args, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("expected , or )")
		}
	}
}

func (p *parser) parseString() (string, error) {
	token, ok := p.consumeToken(tokenString)
	if !ok {
		return "", p.errorf("expected string")
	}
	return token.stringValue, nil
}

func (p *parser) parseNumber() (float64, error) {
	token, ok := p.consumeToken(tokenNumber)
	if !ok {
		return 0, p.errorf("expected number")
	}
	return token.number, nil
}

func (p *parser) parseIdentifier() (string, error) {
	token, ok := p.consumeToken(tokenIdentifier)
	if !ok {
		return "", p.errorf("expected identifier")
	}
	return token.text, nil
}

func (p *parser) consumeKeyword(keyword string) bool {
	token, ok := p.currentToken()
	if !ok || !token.matchesWordAt(p.pos, keyword) {
		return false
	}
	p.tokenIndex++
	p.pos = token.end
	return true
}

func (p *parser) matchKeyword(keyword string) bool {
	token, ok := p.currentToken()
	return ok && token.matchesWordAt(p.pos, keyword)
}

func (p *parser) consumeByte(ch byte) bool {
	token, ok := p.currentToken()
	if !ok ||
		token.kind != tokenSymbol ||
		token.start != p.pos ||
		len(token.text) != 1 ||
		token.text[0] != ch {
		return false
	}
	p.tokenIndex++
	p.pos = token.end
	return true
}

func (p *parser) consumeString(s string) bool {
	token, ok := p.currentToken()
	if !ok || token.kind != tokenSymbol || token.start != p.pos || token.text != s {
		return false
	}
	p.tokenIndex++
	p.pos = token.end
	return true
}

func (p *parser) skipSpace() {
	p.advanceTokenIndex()
	if p.tokenIndex >= len(p.tokens) {
		p.pos = len(p.source)
		return
	}
	if next := p.tokens[p.tokenIndex].start; next > p.pos {
		p.pos = next
	}
}

func (p *parser) trimSourceEnd(end int) int {
	if end > len(p.source) {
		end = len(p.source)
	}
	for end > 0 {
		switch p.source[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return end
		}
	}
	return end
}

func sourceModeDirective(text string) (sourceMode, bool) {
	switch text {
	case "!strict":
		return sourceModeStrict, true
	case "!nonstrict":
		return sourceModeNonStrict, true
	case "!nocheck":
		return sourceModeNoCheck, true
	default:
		return "", false
	}
}

func (p *parser) done() bool {
	return p.pos >= len(p.source)
}

func (p *parser) consumeToken(kind tokenKind) (sourceToken, bool) {
	p.advanceTokenIndex()
	if p.tokenIndex >= len(p.tokens) {
		return sourceToken{}, false
	}
	token := p.tokens[p.tokenIndex]
	if token.start != p.pos || token.kind != kind {
		return sourceToken{}, false
	}
	p.tokenIndex++
	p.pos = token.end
	return token, true
}

func (p *parser) currentToken() (sourceToken, bool) {
	p.advanceTokenIndex()
	if p.tokenIndex >= len(p.tokens) {
		return sourceToken{}, false
	}
	return p.tokens[p.tokenIndex], true
}

func (p *parser) currentTokenKind(kind tokenKind) bool {
	token, ok := p.currentToken()
	return ok && token.start == p.pos && token.kind == kind
}

func (p *parser) currentIdentifier() bool {
	return p.currentTokenKind(tokenIdentifier)
}

func (p *parser) currentSymbol(symbol string) bool {
	token, ok := p.currentToken()
	return ok && token.start == p.pos && token.kind == tokenSymbol && token.text == symbol
}

func (p *parser) currentDoubleQuotedString() bool {
	token, ok := p.currentToken()
	return ok && token.start == p.pos && token.kind == tokenString && strings.HasPrefix(token.text, "\"")
}

func (p *parser) advanceTokenIndex() {
	for p.tokenIndex < len(p.tokens) && p.tokens[p.tokenIndex].end <= p.pos {
		p.tokenIndex++
	}
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("compile: byte %d: %s", p.pos, fmt.Sprintf(format, args...))
}

func statementsHaveReturn(statements []statement) bool {
	for _, stmt := range statements {
		if stmt.ret != nil {
			return true
		}
		if stmt.ifStmt != nil &&
			(statementsHaveReturn(stmt.ifStmt.thenStatements) ||
				statementsHaveReturn(stmt.ifStmt.elseStatements)) {
			return true
		}
		if stmt.while != nil && statementsHaveReturn(stmt.while.statements) {
			return true
		}
	}
	return false
}
