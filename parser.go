package ember

import (
	"fmt"
	"math"
	"strings"
)

type program struct {
	id            syntaxID
	statements    []statement
	statementSpan nodeSpan
	mode          sourceMode
	nodeCount     int
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
	arenaID    statementID
	local      *localStatement
	localFunc  *localFunctionStatement
	funcDecl   *functionDeclarationStatement
	assign     *assignStatement
	call       termID
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
	annotations []typeID
	values      []expressionID
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
	value       typeID
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
	paramAnnotations   []typeID
	variadic           bool
	variadicAnnotation typeID
	returnAnnotation   typeID
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
	paramAnnotations   []typeID
	variadic           bool
	variadicAnnotation typeID
	returnAnnotation   typeID
	statements         []statement
	method             bool
}

type parserFunctionDraft struct {
	id                 syntaxID
	functionID         int
	typeParams         []string
	typeParamID        syntaxID
	typePacks          []string
	typePackID         syntaxID
	params             []string
	paramID            syntaxID
	paramAnnotations   []typeID
	variadic           bool
	variadicAnnotation typeID
	returnAnnotation   typeID
	statements         []statement
}

type assignStatement struct {
	targets []assignTarget
	values  []expressionID
}

type assignTarget struct {
	id        syntaxID
	start     int
	end       int
	name      string
	selectors []selector
}

type ifStatement struct {
	condition      expressionID
	thenStatements []statement
	elseStatements []statement
}

type whileStatement struct {
	condition  expressionID
	statements []statement
}

type forStatement struct {
	nameID     syntaxID
	name       string
	start      expressionID
	limit      expressionID
	step       expressionID
	statements []statement
}

type genericForStatement struct {
	names      []string
	nameID     syntaxID
	values     []expressionID
	statements []statement
}

type parsedForStatement struct {
	numeric *forStatement
	generic *genericForStatement
}

type repeatStatement struct {
	statements []statement
	condition  expressionID
}

type blockStatement struct {
	statements []statement
}

type returnStatement struct {
	start  int
	end    int
	values []expressionID
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

type additivePart struct {
	op    additiveOperator
	value multiplicativeExpressionID
}

type additiveOperator string

const (
	additiveAdd      additiveOperator = "+"
	additiveSubtract additiveOperator = "-"
)

type multiplicativeOperator string

const (
	multiplicativeMultiply multiplicativeOperator = "*"
	multiplicativeDivide   multiplicativeOperator = "/"
	multiplicativeModulo   multiplicativeOperator = "%"
	multiplicativeFloorDiv multiplicativeOperator = "//"
)

type selector struct {
	field string
	index expressionID
}

type parserCheckpoint struct {
	pos        int
	tokenIndex int
}

type parser struct {
	source     string
	limits     CompileLimits
	nesting    uint32
	maxNesting uint32
	pos        int
	mode       sourceMode
	tokens     []sourceToken
	stringPool []string
	tokenIndex int
	arena      *syntaxArena
}

func (p *parser) enterNesting(kind string) error {
	if p.limits.MaxNesting != 0 && p.nesting >= p.limits.MaxNesting {
		return &LimitError{Kind: LimitNesting, Limit: uint64(p.limits.MaxNesting), Used: uint64(p.nesting) + 1}
	}
	p.nesting++
	if p.nesting > p.maxNesting {
		p.maxNesting = p.nesting
	}
	return nil
}

func (p *parser) leaveNesting() {
	if p.nesting > 0 {
		p.nesting--
	}
}

func (p *parser) mark() parserCheckpoint {
	return parserCheckpoint{pos: p.pos, tokenIndex: p.tokenIndex}
}

func (p *parser) restore(checkpoint parserCheckpoint) {
	p.pos = checkpoint.pos
	p.tokenIndex = checkpoint.tokenIndex
}

func (p *parser) parse() (syntaxTree, error) {
	lexed, err := lexSourceForCompileWithTokenLimit(p.source, p.limits.MaxTokens)
	if err != nil {
		return syntaxTree{}, err
	}
	if p.arena == nil {
		p.arena = newSyntaxArena(len(lexed.tokens))
	}
	p.mode = lexed.mode
	p.tokens = lexed.tokens
	p.stringPool = lexed.decodedStrings

	statements, err := p.parseArenaBlock()
	if err != nil {
		return syntaxTree{}, err
	}

	p.skipSpace()
	if !p.done() {
		return syntaxTree{}, p.errorf("unexpected input %q", p.source[p.pos:])
	}
	root := program{statementSpan: statements, mode: p.mode}
	tree := newSyntaxTreeWithArena(root, p.arena)
	if err := assignSyntaxTreeIDsWithLimit(&tree, p.limits.MaxSyntaxNodes); err != nil {
		return syntaxTree{}, err
	}
	return tree, nil
}

// appendStatementStrings and appendStatementTypes bridge the legacy parser
// drafts into the typed sidecars owned by syntaxArena. They are deliberately
// one-way: consumers read spans and never retain parser-owned slices.
func (p *parser) appendStatementStrings(values []string) nodeSpan {
	if len(values) == 0 {
		return nodeSpan{}
	}
	ids := p.arena.statements.stringIDs
	start := uint32(len(ids))
	for _, value := range values {
		ids = append(ids, p.stringID(value))
	}
	p.arena.statements.stringIDs = ids
	return nodeSpan{start: start, count: uint32(len(values))}
}

func (p *parser) appendStatementTypes(values []typeID) nodeSpan {
	if len(values) == 0 {
		return nodeSpan{}
	}
	start := uint32(len(p.arena.statements.typeIDs))
	p.arena.statements.typeIDs = append(p.arena.statements.typeIDs, values...)
	return nodeSpan{start: start, count: uint32(len(values))}
}

func (p *parser) appendStatementExpressions(values []expressionID) nodeSpan {
	if len(values) == 0 {
		return nodeSpan{}
	}
	start := uint32(len(p.arena.statements.expressionIDs))
	p.arena.statements.expressionIDs = append(p.arena.statements.expressionIDs, values...)
	return nodeSpan{start: start, count: uint32(len(values))}
}

func (p *parser) materializeStatementSpan(values []statement) nodeSpan {
	if len(values) == 0 {
		return nodeSpan{}
	}
	start := uint32(len(p.arena.statements.statementIDs))
	for i := range values {
		p.arena.statements.statementIDs = append(p.arena.statements.statementIDs, p.materializeStatement(&values[i]))
	}
	return nodeSpan{start: start, count: uint32(len(values))}
}

func (p *parser) materializeStatement(stmt *statement) statementID {
	if stmt == nil {
		return 0
	}
	if stmt.arenaID != 0 {
		return stmt.arenaID
	}
	var kind syntaxStatementKind
	var payload uint32
	switch {
	case stmt.local != nil:
		kind = syntaxStatementLocal
		value := stmt.local
		names := p.appendStatementStrings(value.names)
		rangesStart := uint32(len(p.arena.statements.sourceRanges))
		p.arena.statements.sourceRanges = append(p.arena.statements.sourceRanges, value.nameRanges...)
		local := arenaLocalStatement{
			nameID:      value.nameID,
			names:       names,
			nameRanges:  nodeSpan{start: rangesStart, count: uint32(len(value.nameRanges))},
			annotations: p.appendStatementTypes(value.annotations),
			values:      p.appendStatementExpressions(value.values),
		}
		payload = p.arena.statements.appendLocal(local)
	case stmt.localFunc != nil:
		kind = syntaxStatementLocalFunction
		value := stmt.localFunc
		fn := p.materializeFunctionStatement(value.id, value.functionID, value.name, 0, value.typeParams, value.typePacks, value.params, value.paramID, value.paramAnnotations, value.variadic, value.variadicAnnotation, value.returnAnnotation, value.statements, false)
		payload = p.arena.statements.appendLocalFunction(fn)
	case stmt.funcDecl != nil:
		kind = syntaxStatementFunctionDeclaration
		value := stmt.funcDecl
		target := p.materializeAssignTarget(&value.target)
		fn := p.materializeFunctionStatement(value.id, value.functionID, "", target, value.typeParams, value.typePacks, value.params, value.paramID, value.paramAnnotations, value.variadic, value.variadicAnnotation, value.returnAnnotation, value.statements, value.method)
		fn.selfID = value.selfID
		payload = p.arena.statements.appendFunctionDeclaration(fn)
	case stmt.assign != nil:
		kind = syntaxStatementAssign
		value := stmt.assign
		targetStart := uint32(len(p.arena.statements.assignTargetIDs))
		for i := range value.targets {
			p.arena.statements.assignTargetIDs = append(p.arena.statements.assignTargetIDs, p.materializeAssignTarget(&value.targets[i]))
		}
		assign := arenaAssignStatement{targets: nodeSpan{start: targetStart, count: uint32(len(value.targets))}, values: p.appendStatementExpressions(value.values)}
		payload = p.arena.statements.appendAssign(assign)
	case stmt.call != 0:
		kind = syntaxStatementCall
		payload = uint32(stmt.call)
	case stmt.ifStmt != nil:
		kind = syntaxStatementIf
		value := stmt.ifStmt
		payload = p.arena.statements.appendIf(arenaIfStatement{condition: value.condition, thenStatements: p.materializeStatementSpan(value.thenStatements), elseStatements: p.materializeStatementSpan(value.elseStatements)})
	case stmt.while != nil:
		kind = syntaxStatementWhile
		value := stmt.while
		payload = p.arena.statements.appendWhile(arenaWhileStatement{condition: value.condition, statements: p.materializeStatementSpan(value.statements)})
	case stmt.forLoop != nil:
		kind = syntaxStatementFor
		value := stmt.forLoop
		payload = p.arena.statements.appendFor(arenaForStatement{name: p.stringID(value.name), nameID: value.nameID, start: value.start, limit: value.limit, step: value.step, statements: p.materializeStatementSpan(value.statements)})
	case stmt.genericFor != nil:
		kind = syntaxStatementGenericFor
		value := stmt.genericFor
		payload = p.arena.statements.appendGenericFor(arenaGenericForStatement{names: p.appendStatementStrings(value.names), nameID: value.nameID, values: p.appendStatementExpressions(value.values), statements: p.materializeStatementSpan(value.statements)})
	case stmt.repeat != nil:
		kind = syntaxStatementRepeat
		value := stmt.repeat
		payload = p.arena.statements.appendRepeat(arenaRepeatStatement{statements: p.materializeStatementSpan(value.statements), condition: value.condition})
	case stmt.block != nil:
		kind = syntaxStatementBlock
		payload = p.arena.statements.appendBlock(arenaBlockStatement{statements: p.materializeStatementSpan(stmt.block.statements)})
	case stmt.typeAlias != nil:
		kind = syntaxStatementTypeAlias
		value := stmt.typeAlias
		payload = p.arena.statements.appendTypeAlias(arenaTypeAliasStatement{id: value.id, exported: value.exported, name: p.stringID(value.name), nameID: value.nameID, start: value.start, end: value.end, nameStart: value.nameStart, nameEnd: value.nameEnd, typeParams: p.appendStatementStrings(value.typeParams), typeParamID: value.typeParamID, typePacks: p.appendStatementStrings(value.typePacks), typePackID: value.typePackID, value: value.value})
	case stmt.breaking:
		kind = syntaxStatementBreak
	case stmt.continues:
		kind = syntaxStatementContinue
	case stmt.ret != nil:
		kind = syntaxStatementReturn
		value := stmt.ret
		payload = p.arena.statements.appendReturn(arenaReturnStatement{start: value.start, end: value.end, values: p.appendStatementExpressions(value.values)})
	default:
		return 0
	}
	return p.arena.statements.appendStatement(arenaStatement{id: stmt.id, kind: kind, payload: payload})
}

func (p *parser) materializeFunctionStatement(id syntaxID, functionID int, name string, target assignTargetID, typeParams, typePacks, params []string, paramID syntaxID, annotations []typeID, variadic bool, variadicAnnotation, returnAnnotation typeID, statements []statement, method bool) arenaFunctionStatement {
	return arenaFunctionStatement{id: id, functionID: functionID, name: p.stringID(name), target: target, typeParams: p.appendStatementStrings(typeParams), typePacks: p.appendStatementStrings(typePacks), params: p.appendStatementStrings(params), paramID: paramID, paramAnnotations: p.appendStatementTypes(annotations), variadic: variadic, variadicAnnotation: variadicAnnotation, returnAnnotation: returnAnnotation, statements: p.materializeStatementSpan(statements), method: method}
}

func (p *parser) materializeAssignTarget(value *assignTarget) assignTargetID {
	if value == nil {
		return 0
	}
	start := uint32(len(p.arena.selectors))
	for _, selector := range value.selectors {
		p.arena.selectors = append(p.arena.selectors, arenaSelector{field: p.stringID(selector.field), index: selector.index})
	}
	return p.arena.statements.appendAssignTarget(arenaAssignTarget{id: value.id, start: value.start, end: value.end, name: p.stringID(value.name), selectors: nodeSpan{start: start, count: uint32(len(value.selectors))}})
}

func (p *parser) appendStatementStringID(id stringID) nodeSpan {
	start := uint32(len(p.arena.statements.stringIDs))
	p.arena.statements.stringIDs = append(p.arena.statements.stringIDs, id)
	return nodeSpan{start: start, count: 1}
}

func (p *parser) appendStatementTypeID(id typeID) nodeSpan {
	start := uint32(len(p.arena.statements.typeIDs))
	p.arena.statements.typeIDs = append(p.arena.statements.typeIDs, id)
	return nodeSpan{start: start, count: 1}
}

func (p *parser) appendStatementExpressionID(id expressionID) nodeSpan {
	start := uint32(len(p.arena.statements.expressionIDs))
	p.arena.statements.expressionIDs = append(p.arena.statements.expressionIDs, id)
	return nodeSpan{start: start, count: 1}
}

func (p *parser) appendStatementID(id statementID) nodeSpan {
	start := uint32(len(p.arena.statements.statementIDs))
	p.arena.statements.statementIDs = append(p.arena.statements.statementIDs, id)
	return nodeSpan{start: start, count: 1}
}

func (p *parser) appendArenaStatement(kind syntaxStatementKind, payload uint32) statementID {
	return p.arena.statements.appendStatement(arenaStatement{kind: kind, payload: payload})
}

func (p *parser) parseArenaBlock(stopKeywords ...string) (nodeSpan, error) {
	if len(stopKeywords) > 0 {
		if err := p.enterNesting("block"); err != nil {
			return nodeSpan{}, err
		}
		defer p.leaveNesting()
	}
	ids := idListBuilder[statementID]{}
	for {
		p.skipSpace()
		if p.done() {
			if len(stopKeywords) > 0 {
				return nodeSpan{}, p.errorf("expected %s", strings.Join(stopKeywords, " or "))
			}
			span, ok := ids.span(&p.arena.statements.statementIDs)
			if !ok {
				return nodeSpan{}, p.errorf("statement arena exhausted")
			}
			return span, nil
		}
		matched := false
		for _, keyword := range stopKeywords {
			if p.matchKeyword(keyword) {
				matched = true
				break
			}
		}
		if matched {
			span, ok := ids.span(&p.arena.statements.statementIDs)
			if !ok {
				return nodeSpan{}, p.errorf("statement arena exhausted")
			}
			return span, nil
		}
		id, err := p.parseArenaStatement()
		if err != nil {
			return nodeSpan{}, err
		}
		ids.append(id)
	}
}

func (p *parser) parseArenaStatement() (statementID, error) {
	token, ok := p.currentToken()
	if !ok {
		return 0, p.errorf("expected statement")
	}
	if token.matchesWordAt(p.source, p.pos, "type") || token.matchesWordAt(p.source, p.pos, "export") {
		if alias, parsed, err := p.parseArenaTypeAliasStatement(); parsed || err != nil {
			if err != nil {
				return 0, err
			}
			return alias, nil
		}
	}
	switch {
	case token.matchesWordAt(p.source, p.pos, "local"):
		p.consumeKeyword("local")
		p.skipSpace()
		if p.consumeKeyword("function") {
			return p.parseArenaLocalFunctionStatement()
		}
		return p.parseArenaLocalStatement()
	case token.matchesWordAt(p.source, p.pos, "return"):
		start := token.startOffset()
		end := token.endOffset()
		p.consumeKeyword("return")
		values, err := p.parseArenaExpressionListOptional()
		if err != nil {
			return 0, err
		}
		if values.count > 0 {
			end = p.trimSourceEnd(p.pos)
		}
		payload := p.arena.statements.appendReturn(arenaReturnStatement{start: start, end: end, values: values})
		return p.appendArenaStatement(syntaxStatementReturn, payload), nil
	case token.matchesWordAt(p.source, p.pos, "function"):
		p.consumeKeyword("function")
		return p.parseArenaFunctionDeclarationStatement()
	case token.matchesWordAt(p.source, p.pos, "if"):
		p.consumeKeyword("if")
		return p.parseArenaIfStatement()
	case token.matchesWordAt(p.source, p.pos, "while"):
		p.consumeKeyword("while")
		return p.parseArenaWhileStatement()
	case token.matchesWordAt(p.source, p.pos, "for"):
		p.consumeKeyword("for")
		return p.parseArenaForStatement()
	case token.matchesWordAt(p.source, p.pos, "repeat"):
		p.consumeKeyword("repeat")
		return p.parseArenaRepeatStatement()
	case token.matchesWordAt(p.source, p.pos, "do"):
		p.consumeKeyword("do")
		return p.parseArenaDoBlockStatement()
	case token.matchesWordAt(p.source, p.pos, "break"):
		p.consumeKeyword("break")
		return p.appendArenaStatement(syntaxStatementBreak, 0), nil
	case token.matchesWordAt(p.source, p.pos, "continue"):
		p.consumeKeyword("continue")
		return p.appendArenaStatement(syntaxStatementContinue, 0), nil
	case token.kind == tokenIdentifier:
		return p.parseArenaIdentifierStatement()
	default:
		return 0, p.errorf("expected statement")
	}
}

func (p *parser) parseArenaExpressionListOptional() (nodeSpan, error) {
	p.skipSpace()
	if p.done() || p.matchKeyword("end") || p.matchKeyword("else") || p.matchKeyword("elseif") || p.matchKeyword("until") {
		return nodeSpan{}, nil
	}
	return p.parseArenaExpressionList()
}

func (p *parser) parseArenaExpressionList() (nodeSpan, error) {
	values := idListBuilder[expressionID]{}
	for {
		value, err := p.parseExpression()
		if err != nil {
			return nodeSpan{}, err
		}
		values.append(value)
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
	}
	span, ok := values.span(&p.arena.statements.expressionIDs)
	if !ok {
		return nodeSpan{}, p.errorf("statement arena exhausted")
	}
	return span, nil
}

func (p *parser) expressionSpanEnd(span nodeSpan) int {
	values, ok := p.arena.statements.spanExpressions(span)
	if !ok || len(values) == 0 {
		return p.pos
	}
	return p.expressionEnd(values[len(values)-1])
}

func (p *parser) expressionEnd(id expressionID) int {
	node, ok := p.arena.expression(id)
	if !ok {
		return p.pos
	}
	terms, ok := p.arena.andIDs(node.terms)
	if !ok || len(terms) == 0 {
		return p.pos
	}
	return p.andExpressionEnd(terms[len(terms)-1])
}

func (p *parser) andExpressionEnd(id andExpressionID) int {
	node, ok := p.arena.and(id)
	if !ok {
		return p.pos
	}
	values, ok := p.arena.comparisonIDs(node.terms)
	if !ok || len(values) == 0 {
		return p.pos
	}
	return p.comparisonEnd(values[len(values)-1])
}

func (p *parser) comparisonEnd(id comparisonExpressionID) int {
	node, ok := p.arena.comparison(id)
	if !ok {
		return p.pos
	}
	return p.concatEnd(node.right)
}

func (p *parser) concatEnd(id concatExpressionID) int {
	node, ok := p.arena.concat(id)
	if !ok {
		return p.pos
	}
	return p.additiveEnd(node.first)
}

func (p *parser) additiveEnd(id additiveExpressionID) int {
	node, ok := p.arena.additive(id)
	if !ok {
		return p.pos
	}
	return p.multiplicativeEnd(node.first)
}

func (p *parser) multiplicativeEnd(id multiplicativeExpressionID) int {
	node, ok := p.arena.multiplicative(id)
	if !ok {
		return p.pos
	}
	return p.termEnd(node.first)
}

func (p *parser) parseArenaIdentifierStatement() (statementID, error) {
	target, err := p.parseArenaAssignTarget()
	if err != nil {
		return 0, err
	}
	targets := idListBuilder[assignTargetID]{}
	targets.append(target)
	p.skipSpace()
	for p.consumeByte(',') {
		p.skipSpace()
		next, err := p.parseArenaAssignTarget()
		if err != nil {
			return 0, err
		}
		targets.append(next)
		p.skipSpace()
	}
	targetSpan, ok := targets.span(&p.arena.statements.assignTargetIDs)
	if !ok {
		return 0, p.errorf("statement arena exhausted")
	}
	if p.consumeByte('=') {
		values, err := p.parseArenaExpressionList()
		if err != nil {
			return 0, err
		}
		payload := p.arena.statements.appendAssign(arenaAssignStatement{targets: targetSpan, values: values})
		return p.appendArenaStatement(syntaxStatementAssign, payload), nil
	}
	if targetSpan.count != 1 {
		return 0, p.errorf("expected =")
	}
	if !p.currentSymbol("(") && !p.currentSymbol(":") && !p.currentSymbol("<<") {
		return 0, p.errorf("expected =")
	}
	targetValue, ok := p.arena.statements.assignTarget(assignTargetID(target))
	if !ok {
		return 0, p.errorf("invalid identifier term")
	}
	selectorsStart := uint32(len(p.arena.selectors))
	selectors, _ := p.arena.selectorIDs(targetValue.selectors)
	for _, selector := range selectors {
		p.arena.selectors = append(p.arena.selectors, selector)
	}
	selectorSpan := nodeSpan{start: selectorsStart, count: uint32(len(selectors))}
	value := p.arena.appendTerm(arenaTerm{start: targetValue.start, end: targetValue.end, kind: termKindName, payload: uint64(targetValue.name), selectors: selectorSpan})
	value, err = p.parseTermSuffixesWithCasts(value, false)
	if err != nil {
		return 0, err
	}
	node, ok := p.arena.term(value)
	if !ok || node.kind != termKindCall {
		return 0, p.errorf("expected =")
	}
	return p.appendArenaStatement(syntaxStatementCall, uint32(value)), nil
}

func (p *parser) parseArenaAssignTarget() (assignTargetID, error) {
	start := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return 0, err
	}
	selectors := idListBuilder[arenaSelector]{}
	end := p.pos
	for {
		p.skipSpace()
		if p.consumeByte('.') {
			field, err := p.parseIdentifier()
			if err != nil {
				return 0, err
			}
			selectors.append(arenaSelector{field: p.stringID(field)})
			end = p.pos
			continue
		}
		if p.consumeByte('[') {
			index, err := p.parseExpression()
			if err != nil {
				return 0, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return 0, p.errorf("expected ]")
			}
			selectors.append(arenaSelector{index: index})
			end = p.pos
			continue
		}
		break
	}
	selectorSpan, ok := selectors.span(&p.arena.selectors)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	return p.arena.statements.appendAssignTarget(arenaAssignTarget{start: start, end: end, name: p.stringID(name), selectors: selectorSpan}), nil
}

func (p *parser) parseArenaLocalStatement() (statementID, error) {
	nameStart := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return 0, err
	}
	nameIDsStart := uint32(len(p.arena.statements.stringIDs))
	rangesStart := uint32(len(p.arena.statements.sourceRanges))
	typesStart := uint32(len(p.arena.statements.typeIDs))
	nameCount := uint32(0)
	appendName := func(value string, start, end int, annotation typeID) {
		p.arena.statements.stringIDs = append(p.arena.statements.stringIDs, p.stringID(value))
		p.arena.statements.sourceRanges = append(p.arena.statements.sourceRanges, sourceRange{start: start, end: end})
		p.arena.statements.typeIDs = append(p.arena.statements.typeIDs, annotation)
		nameCount++
	}
	nameEnd := p.pos
	annotation, err := p.parseOptionalTypeAnnotation()
	if err != nil {
		return 0, err
	}
	appendName(name, nameStart, nameEnd, annotation)
	for {
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
		start := p.pos
		name, err := p.parseIdentifier()
		if err != nil {
			return 0, err
		}
		end := p.pos
		annotation, err := p.parseOptionalTypeAnnotation()
		if err != nil {
			return 0, err
		}
		appendName(name, start, end, annotation)
	}
	p.skipSpace()
	if !p.consumeByte('=') {
		return 0, p.errorf("expected =")
	}
	values, err := p.parseArenaExpressionList()
	if err != nil {
		return 0, err
	}
	payload := p.arena.statements.appendLocal(arenaLocalStatement{names: nodeSpan{start: nameIDsStart, count: nameCount}, nameRanges: nodeSpan{start: rangesStart, count: nameCount}, annotations: nodeSpan{start: typesStart, count: nameCount}, values: values})
	return p.appendArenaStatement(syntaxStatementLocal, payload), nil
}

func (p *parser) parseArenaTypeAliasStatement() (statementID, bool, error) {
	checkpoint := p.mark()
	start := checkpoint.pos
	exported := false
	if p.consumeKeyword("export") {
		p.skipSpace()
		if !p.consumeKeyword("type") {
			p.restore(checkpoint)
			return 0, false, nil
		}
		exported = true
	} else if !p.consumeKeyword("type") {
		return 0, false, nil
	}
	p.skipSpace()
	if !p.currentIdentifier() {
		p.restore(checkpoint)
		return 0, false, nil
	}
	nameStart := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return 0, false, err
	}
	nameEnd := p.pos
	typeParams, typePacks, err := p.parseOptionalTypeParameterList()
	if err != nil {
		return 0, true, err
	}
	p.skipSpace()
	if !p.consumeByte('=') {
		p.restore(checkpoint)
		return 0, false, nil
	}
	p.skipSpace()
	value, err := p.parseType()
	if err != nil {
		return 0, true, err
	}
	valueNode, err := p.parsedType(value)
	if err != nil {
		return 0, true, err
	}
	payload := p.arena.statements.appendTypeAlias(arenaTypeAliasStatement{exported: exported, name: p.stringID(name), start: start, end: p.trimSourceEnd(valueNode.end), nameStart: nameStart, nameEnd: nameEnd, typeParams: p.appendStatementStrings(typeParams), typePacks: p.appendStatementStrings(typePacks), value: value})
	return p.appendArenaStatement(syntaxStatementTypeAlias, payload), true, nil
}

func (p *parser) parseArenaLocalFunctionStatement() (statementID, error) {
	p.skipSpace()
	name, err := p.parseIdentifier()
	if err != nil {
		return 0, err
	}
	body, err := p.parseArenaFunctionBody()
	if err != nil {
		return 0, err
	}
	payload := p.arena.statements.appendLocalFunction(arenaFunctionStatement{name: p.stringID(name), typeParams: body.typeParams, typePacks: body.typePacks, params: body.params, paramAnnotations: body.paramAnnotations, variadic: body.variadic, variadicAnnotation: body.variadicAnnotation, returnAnnotation: body.returnAnnotation, statements: body.statements})
	return p.appendArenaStatement(syntaxStatementLocalFunction, payload), nil
}

func (p *parser) parseArenaFunctionDeclarationStatement() (statementID, error) {
	p.skipSpace()
	target, method, err := p.parseArenaFunctionDeclarationTarget()
	if err != nil {
		return 0, err
	}
	body, err := p.parseArenaFunctionBody()
	if err != nil {
		return 0, err
	}
	payload := p.arena.statements.appendFunctionDeclaration(arenaFunctionStatement{target: target, typeParams: body.typeParams, typePacks: body.typePacks, params: body.params, paramAnnotations: body.paramAnnotations, variadic: body.variadic, variadicAnnotation: body.variadicAnnotation, returnAnnotation: body.returnAnnotation, statements: body.statements, method: method})
	return p.appendArenaStatement(syntaxStatementFunctionDeclaration, payload), nil
}

func (p *parser) parseArenaFunctionDeclarationTarget() (assignTargetID, bool, error) {
	start := p.pos
	name, err := p.parseIdentifier()
	if err != nil {
		return 0, false, err
	}
	selectorsStart := uint32(len(p.arena.selectors))
	count := uint32(0)
	end := p.pos
	method := false
	for {
		p.skipSpace()
		if p.consumeByte('.') {
			field, err := p.parseIdentifier()
			if err != nil {
				return 0, false, err
			}
			p.arena.selectors = append(p.arena.selectors, arenaSelector{field: p.stringID(field)})
			count++
			end = p.pos
			continue
		}
		if p.consumeByte(':') {
			field, err := p.parseIdentifier()
			if err != nil {
				return 0, false, err
			}
			p.arena.selectors = append(p.arena.selectors, arenaSelector{field: p.stringID(field)})
			count++
			end = p.pos
			method = true
			break
		}
		break
	}
	return p.arena.statements.appendAssignTarget(arenaAssignTarget{start: start, end: end, name: p.stringID(name), selectors: nodeSpan{start: selectorsStart, count: count}}), method, nil
}

type parserArenaFunctionBody struct {
	typeParams, typePacks, params        nodeSpan
	paramAnnotations                     nodeSpan
	variadic                             bool
	variadicAnnotation, returnAnnotation typeID
	statements                           nodeSpan
}

func (p *parser) parseArenaFunctionBody() (parserArenaFunctionBody, error) {
	if err := p.enterNesting("function"); err != nil {
		return parserArenaFunctionBody{}, err
	}
	defer p.leaveNesting()
	typeParams, typePacks, err := p.parseOptionalTypeParameterList()
	if err != nil {
		return parserArenaFunctionBody{}, err
	}
	params, annotations, variadic, variadicAnnotation, err := p.parseArenaParameterList()
	if err != nil {
		return parserArenaFunctionBody{}, err
	}
	returnAnnotation, err := p.parseOptionalTypeAnnotation()
	if err != nil {
		return parserArenaFunctionBody{}, err
	}
	statements, err := p.parseArenaBlock("end")
	if err != nil {
		return parserArenaFunctionBody{}, err
	}
	p.skipSpace()
	if !p.consumeKeyword("end") {
		return parserArenaFunctionBody{}, p.errorf("expected end")
	}
	return parserArenaFunctionBody{typeParams: p.appendStatementStrings(typeParams), typePacks: p.appendStatementStrings(typePacks), params: params, paramAnnotations: annotations, variadic: variadic, variadicAnnotation: variadicAnnotation, returnAnnotation: returnAnnotation, statements: statements}, nil
}

func (p *parser) parseArenaParameterList() (nodeSpan, nodeSpan, bool, typeID, error) {
	p.skipSpace()
	if !p.consumeByte('(') {
		return nodeSpan{}, nodeSpan{}, false, 0, p.errorf("expected (")
	}
	p.skipSpace()
	if p.consumeByte(')') {
		return nodeSpan{}, nodeSpan{}, false, 0, nil
	}
	params := idListBuilder[stringID]{}
	annotations := idListBuilder[typeID]{}
	for {
		if p.consumeString("...") {
			annotation, err := p.parseOptionalTypeAnnotation()
			if err != nil {
				return nodeSpan{}, nodeSpan{}, false, 0, err
			}
			p.skipSpace()
			if !p.consumeByte(')') {
				return nodeSpan{}, nodeSpan{}, false, 0, p.errorf("expected )")
			}
			paramSpan, ok := params.span(&p.arena.statements.stringIDs)
			if !ok {
				return nodeSpan{}, nodeSpan{}, false, 0, p.errorf("statement arena exhausted")
			}
			annotationSpan, ok := annotations.span(&p.arena.statements.typeIDs)
			if !ok {
				return nodeSpan{}, nodeSpan{}, false, 0, p.errorf("statement arena exhausted")
			}
			return paramSpan, annotationSpan, true, annotation, nil
		}
		name, err := p.parseIdentifier()
		if err != nil {
			return nodeSpan{}, nodeSpan{}, false, 0, err
		}
		annotation, err := p.parseOptionalTypeAnnotation()
		if err != nil {
			return nodeSpan{}, nodeSpan{}, false, 0, err
		}
		params.append(p.stringID(name))
		annotations.append(annotation)
		p.skipSpace()
		if p.consumeByte(')') {
			break
		}
		if !p.consumeByte(',') {
			return nodeSpan{}, nodeSpan{}, false, 0, p.errorf("expected , or )")
		}
		p.skipSpace()
	}
	paramSpan, ok := params.span(&p.arena.statements.stringIDs)
	if !ok {
		return nodeSpan{}, nodeSpan{}, false, 0, p.errorf("statement arena exhausted")
	}
	annotationSpan, ok := annotations.span(&p.arena.statements.typeIDs)
	if !ok {
		return nodeSpan{}, nodeSpan{}, false, 0, p.errorf("statement arena exhausted")
	}
	return paramSpan, annotationSpan, false, 0, nil
}

func (p *parser) parseArenaIfStatement() (statementID, error) {
	if err := p.enterNesting("if"); err != nil {
		return 0, err
	}
	defer p.leaveNesting()
	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.consumeKeyword("then") {
		return 0, p.errorf("expected then")
	}
	thenStatements, err := p.parseArenaBlock("elseif", "else", "end")
	if err != nil {
		return 0, err
	}
	var elseStatements nodeSpan
	p.skipSpace()
	if p.consumeKeyword("elseif") {
		nested, err := p.parseArenaIfStatement()
		if err != nil {
			return 0, err
		}
		elseStatements = p.appendStatementID(nested)
		payload := p.arena.statements.appendIf(arenaIfStatement{condition: condition, thenStatements: thenStatements, elseStatements: elseStatements})
		return p.appendArenaStatement(syntaxStatementIf, payload), nil
	} else if p.consumeKeyword("else") {
		elseStatements, err = p.parseArenaBlock("end")
		if err != nil {
			return 0, err
		}
		p.skipSpace()
	}
	if !p.consumeKeyword("end") {
		return 0, p.errorf("expected end")
	}
	payload := p.arena.statements.appendIf(arenaIfStatement{condition: condition, thenStatements: thenStatements, elseStatements: elseStatements})
	return p.appendArenaStatement(syntaxStatementIf, payload), nil
}

func (p *parser) parseArenaWhileStatement() (statementID, error) {
	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.consumeKeyword("do") {
		return 0, p.errorf("expected do")
	}
	statements, err := p.parseArenaBlock("end")
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.consumeKeyword("end") {
		return 0, p.errorf("expected end")
	}
	payload := p.arena.statements.appendWhile(arenaWhileStatement{condition: condition, statements: statements})
	return p.appendArenaStatement(syntaxStatementWhile, payload), nil
}

func (p *parser) parseArenaForStatement() (statementID, error) {
	p.skipSpace()
	namesStart := uint32(len(p.arena.statements.stringIDs))
	name, err := p.parseIdentifier()
	if err != nil {
		return 0, err
	}
	p.arena.statements.stringIDs = append(p.arena.statements.stringIDs, p.stringID(name))
	nameCount := uint32(1)
	for {
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
		name, err := p.parseIdentifier()
		if err != nil {
			return 0, err
		}
		p.arena.statements.stringIDs = append(p.arena.statements.stringIDs, p.stringID(name))
		nameCount++
	}
	p.skipSpace()
	if p.consumeKeyword("in") {
		p.skipSpace()
		values, err := p.parseArenaExpressionList()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if !p.consumeKeyword("do") {
			return 0, p.errorf("expected do")
		}
		statements, err := p.parseArenaBlock("end")
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if !p.consumeKeyword("end") {
			return 0, p.errorf("expected end")
		}
		payload := p.arena.statements.appendGenericFor(arenaGenericForStatement{names: nodeSpan{start: namesStart, count: nameCount}, values: values, statements: statements})
		return p.appendArenaStatement(syntaxStatementGenericFor, payload), nil
	}
	if nameCount != 1 {
		return 0, p.errorf("expected in")
	}
	if !p.consumeByte('=') {
		return 0, p.errorf("expected =")
	}
	p.skipSpace()
	start, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.consumeByte(',') {
		return 0, p.errorf("expected ,")
	}
	p.skipSpace()
	limit, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	var step expressionID
	p.skipSpace()
	if p.consumeByte(',') {
		p.skipSpace()
		step, err = p.parseExpression()
		if err != nil {
			return 0, err
		}
	}
	p.skipSpace()
	if !p.consumeKeyword("do") {
		return 0, p.errorf("expected do")
	}
	statements, err := p.parseArenaBlock("end")
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.consumeKeyword("end") {
		return 0, p.errorf("expected end")
	}
	names, _ := p.arena.statements.spanStrings(nodeSpan{start: namesStart, count: 1})
	var value stringID
	if len(names) > 0 {
		value = names[0]
	}
	payload := p.arena.statements.appendFor(arenaForStatement{name: value, start: start, limit: limit, step: step, statements: statements})
	return p.appendArenaStatement(syntaxStatementFor, payload), nil
}

func (p *parser) parseArenaRepeatStatement() (statementID, error) {
	statements, err := p.parseArenaBlock("until")
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.consumeKeyword("until") {
		return 0, p.errorf("expected until")
	}
	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	payload := p.arena.statements.appendRepeat(arenaRepeatStatement{statements: statements, condition: condition})
	return p.appendArenaStatement(syntaxStatementRepeat, payload), nil
}

func (p *parser) parseArenaDoBlockStatement() (statementID, error) {
	statements, err := p.parseArenaBlock("end")
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if !p.consumeKeyword("end") {
		return 0, p.errorf("expected end")
	}
	payload := p.arena.statements.appendBlock(arenaBlockStatement{statements: statements})
	return p.appendArenaStatement(syntaxStatementBlock, payload), nil
}

func (p *parser) parseBlock(stopKeywords ...string) ([]statement, error) {
	if len(stopKeywords) > 0 {
		if err := p.enterNesting("block"); err != nil {
			return nil, err
		}
		defer p.leaveNesting()
	}
	var statements []statement
	if len(stopKeywords) == 0 && len(p.tokens) > 0 {
		statements = make([]statement, 0, len(p.tokens)/3+1)
	}
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
	value, err := p.parseStatementLegacy()
	if err != nil {
		return statement{}, err
	}
	if value.arenaID != 0 {
		return value, nil
	}
	return statement{arenaID: p.materializeStatement(&value)}, nil
}

func (p *parser) parseStatementLegacy() (statement, error) {
	token, ok := p.currentToken()
	if !ok {
		return statement{}, p.errorf("expected statement")
	}
	if token.matchesWordAt(p.source, p.pos, "type") || token.matchesWordAt(p.source, p.pos, "export") {
		if alias, parsed, err := p.tryParseTypeAliasStatement(); parsed || err != nil {
			if err != nil {
				return statement{}, err
			}
			return statement{typeAlias: alias}, nil
		}
	}

	switch {
	case token.matchesWordAt(p.source, p.pos, "local"):
		p.consumeKeyword("local")
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

	case token.matchesWordAt(p.source, p.pos, "return"):
		p.consumeKeyword("return")
		stmt, err := p.parseReturnStatement()
		if err != nil {
			return statement{}, err
		}
		stmt.start = token.startOffset()
		stmt.end = token.endOffset()
		return statement{ret: &stmt}, nil
	case token.matchesWordAt(p.source, p.pos, "function"):
		p.consumeKeyword("function")
		stmt, err := p.parseFunctionDeclarationStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{funcDecl: &stmt}, nil
	case token.matchesWordAt(p.source, p.pos, "if"):
		p.consumeKeyword("if")
		stmt, err := p.parseIfStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{ifStmt: &stmt}, nil
	case token.matchesWordAt(p.source, p.pos, "while"):
		p.consumeKeyword("while")
		stmt, err := p.parseWhileStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{while: &stmt}, nil
	case token.matchesWordAt(p.source, p.pos, "for"):
		p.consumeKeyword("for")
		stmt, err := p.parseForStatement()
		if err != nil {
			return statement{}, err
		}
		if stmt.numeric != nil {
			return statement{forLoop: stmt.numeric}, nil
		}
		return statement{genericFor: stmt.generic}, nil
	case token.matchesWordAt(p.source, p.pos, "repeat"):
		p.consumeKeyword("repeat")
		stmt, err := p.parseRepeatStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{repeat: &stmt}, nil
	case token.matchesWordAt(p.source, p.pos, "do"):
		p.consumeKeyword("do")
		stmt, err := p.parseDoBlockStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{block: &stmt}, nil
	case token.matchesWordAt(p.source, p.pos, "break"):
		p.consumeKeyword("break")
		return statement{breaking: true}, nil
	case token.matchesWordAt(p.source, p.pos, "continue"):
		p.consumeKeyword("continue")
		return statement{continues: true}, nil
	case token.kind == tokenIdentifier:
		return p.parseIdentifierStatement()
	default:
		return statement{}, p.errorf("expected statement")
	}
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
	valueNode, err := p.parsedType(value)
	if err != nil {
		return nil, true, err
	}
	return &typeAliasStatement{exported: exported, name: name, start: start, end: p.trimSourceEnd(valueNode.end), nameStart: nameStart, nameEnd: nameEnd, typeParams: typeParams, typePacks: typePacks, value: value}, true, nil
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

func (p *parser) parseFunctionExpression() (arenaFunctionID, error) {
	if !p.consumeKeyword("function") {
		return 0, p.errorf("expected function")
	}

	body, err := p.parseArenaFunctionBody()
	if err != nil {
		return 0, err
	}
	return p.arena.appendFunction(arenaFunction{
		typeParams: body.typeParams, typePacks: body.typePacks, params: body.params,
		paramAnnotations: body.paramAnnotations, variadic: body.variadic,
		variadicAnnotation: body.variadicAnnotation,
		returnAnnotation:   body.returnAnnotation,
		statements:         body.statements,
	}), nil
}

func (p *parser) parseFunctionBody() (parserFunctionDraft, error) {
	if err := p.enterNesting("function"); err != nil {
		return parserFunctionDraft{}, err
	}
	defer p.leaveNesting()
	typeParams, typePacks, err := p.parseOptionalTypeParameterList()
	if err != nil {
		return parserFunctionDraft{}, err
	}

	params, annotations, variadic, variadicAnnotation, err := p.parseParameterList()
	if err != nil {
		return parserFunctionDraft{}, err
	}
	returnAnnotation, err := p.parseOptionalTypeAnnotation()
	if err != nil {
		return parserFunctionDraft{}, err
	}

	statements, err := p.parseBlock("end")
	if err != nil {
		return parserFunctionDraft{}, err
	}
	p.skipSpace()
	if !p.consumeKeyword("end") {
		return parserFunctionDraft{}, p.errorf("expected end")
	}

	return parserFunctionDraft{
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

func (p *parser) parseParameterList() ([]string, []typeID, bool, typeID, error) {
	p.skipSpace()
	if !p.consumeByte('(') {
		return nil, nil, false, 0, p.errorf("expected (")
	}
	p.skipSpace()
	if p.consumeByte(')') {
		return nil, nil, false, 0, nil
	}

	var params []string
	var annotations []typeID
	for {
		if p.consumeString("...") {
			annotation, err := p.parseOptionalTypeAnnotation()
			if err != nil {
				return nil, nil, false, 0, err
			}
			p.skipSpace()
			if !p.consumeByte(')') {
				return nil, nil, false, 0, p.errorf("expected )")
			}
			return params, annotations, true, annotation, nil
		}

		param, err := p.parseIdentifier()
		if err != nil {
			return nil, nil, false, 0, err
		}
		annotation, err := p.parseOptionalTypeAnnotation()
		if err != nil {
			return nil, nil, false, 0, err
		}
		params = append(params, param)
		annotations = append(annotations, annotation)

		p.skipSpace()
		if p.consumeByte(')') {
			return params, annotations, false, 0, nil
		}
		if !p.consumeByte(',') {
			return nil, nil, false, 0, p.errorf("expected , or )")
		}
		p.skipSpace()
	}
}

func (p *parser) parseIdentifierStatement() (statement, error) {
	target, err := p.parseAssignTarget()
	if err != nil {
		return statement{}, err
	}
	targetID := p.materializeAssignTarget(&target)
	targetStart := uint32(len(p.arena.statements.assignTargetIDs))
	p.arena.statements.assignTargetIDs = append(p.arena.statements.assignTargetIDs, targetID)
	targetCount := uint32(1)
	p.skipSpace()
	for p.consumeByte(',') {
		p.skipSpace()
		next, err := p.parseAssignTarget()
		if err != nil {
			return statement{}, err
		}
		p.arena.statements.assignTargetIDs = append(p.arena.statements.assignTargetIDs, p.materializeAssignTarget(&next))
		targetCount++
		p.skipSpace()
	}

	if p.consumeByte('=') {
		values, err := p.parseExpressionListSpan()
		if err != nil {
			return statement{}, err
		}
		payload := p.arena.statements.appendAssign(arenaAssignStatement{targets: nodeSpan{start: targetStart, count: targetCount}, values: values})
		id := p.arena.statements.appendStatement(arenaStatement{kind: syntaxStatementAssign, payload: payload})
		return statement{arenaID: id}, nil
	}
	if targetCount != 1 {
		return statement{}, p.errorf("expected =")
	}

	if !p.currentSymbol("(") && !p.currentSymbol(":") && !p.currentSymbol("<<") {
		return statement{}, p.errorf("expected =")
	}

	selectors := idListBuilder[arenaSelector]{}
	for _, selector := range target.selectors {
		selectors.append(arenaSelector{field: p.stringID(selector.field), index: selector.index})
	}
	selectorSpan, ok := selectors.span(&p.arena.selectors)
	if !ok {
		return statement{}, p.errorf("expression arena exhausted")
	}
	value := p.arena.appendTerm(arenaTerm{
		start:     target.start,
		end:       target.end,
		kind:      termKindName,
		payload:   uint64(p.stringID(target.name)),
		selectors: selectorSpan,
	})
	value, err = p.parseTermSuffixesWithCasts(value, false)
	if err != nil {
		return statement{}, err
	}
	node, ok := p.arena.term(value)
	if !ok {
		return statement{}, p.errorf("invalid identifier term")
	}
	if node.kind != termKindCall {
		return statement{}, p.errorf("expected =")
	}
	return identifierCallStatement(value), nil
}

// Keep call-term address-taking out of the assignment path so ordinary terms stay stack-allocated.
func identifierCallStatement(value termID) statement {
	return statement{call: value}
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

func (p *parser) parseExpressionList() ([]expressionID, error) {
	value, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	values := []expressionID{value}
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

func (p *parser) parseExpressionListSpan() (nodeSpan, error) {
	start := uint32(len(p.arena.statements.expressionIDs))
	count := uint32(0)
	value, err := p.parseExpression()
	if err != nil {
		return nodeSpan{}, err
	}
	p.arena.statements.expressionIDs = append(p.arena.statements.expressionIDs, value)
	count++
	for {
		p.skipSpace()
		if !p.consumeByte(',') {
			break
		}
		p.skipSpace()
		value, err := p.parseExpression()
		if err != nil {
			return nodeSpan{}, err
		}
		p.arena.statements.expressionIDs = append(p.arena.statements.expressionIDs, value)
		count++
	}
	return nodeSpan{start: start, count: count}, nil
}

func (p *parser) parseIfStatement() (ifStatement, error) {
	if err := p.enterNesting("if"); err != nil {
		return ifStatement{}, err
	}
	defer p.leaveNesting()
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

	var step expressionID
	p.skipSpace()
	if p.consumeByte(',') {
		p.skipSpace()
		value, err := p.parseExpression()
		if err != nil {
			return parsedForStatement{}, err
		}
		step = value
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
	annotations := []typeID{annotation}

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

func (p *parser) parseOptionalTypeAnnotation() (typeID, error) {
	p.skipSpace()
	if !p.consumeByte(':') {
		return 0, nil
	}

	p.skipSpace()
	return p.parseType()
}

func (p *parser) appendType(node arenaType) (typeID, error) {
	used := uint64(len(p.arena.types.nodes)) + 1
	if limit := p.limits.MaxSyntaxNodes; limit != 0 && used > limit {
		return 0, &LimitError{Kind: LimitSyntaxNodes, Limit: limit, Used: used}
	}
	id, ok := p.arena.types.append(node)
	if !ok {
		return 0, p.errorf("type arena exhausted")
	}
	return id, nil
}

func (p *parser) parsedType(id typeID) (arenaType, error) {
	node, ok := p.arena.types.node(id)
	if !ok {
		return arenaType{}, p.errorf("invalid type arena ID")
	}
	return node, nil
}

func (p *parser) parseType() (typeID, error) {
	if err := p.enterNesting("type"); err != nil {
		return 0, err
	}
	defer p.leaveNesting()
	first, err := p.parseIntersectionType()
	if err != nil {
		return 0, err
	}
	types := idListBuilder[typeID]{}
	types.append(first)

	for {
		p.skipSpace()
		if !p.consumeByte('|') {
			if types.count == 1 {
				return first, nil
			}
			last := types.at(types.count - 1)
			children, ok := types.span(&p.arena.types.typeIDs)
			if !ok {
				return 0, p.errorf("type arena exhausted")
			}
			firstNode, err := p.parsedType(first)
			if err != nil {
				return 0, err
			}
			lastNode, err := p.parsedType(last)
			if err != nil {
				return 0, err
			}
			return p.appendType(arenaType{start: firstNode.start, end: lastNode.end, kind: typeKindUnion, children: children})
		}
		p.skipSpace()
		next, err := p.parseIntersectionType()
		if err != nil {
			return 0, err
		}
		types.append(next)
	}
}

func (p *parser) parseIntersectionType() (typeID, error) {
	first, err := p.parsePostfixType()
	if err != nil {
		return 0, err
	}
	types := idListBuilder[typeID]{}
	types.append(first)

	for {
		p.skipSpace()
		if !p.consumeByte('&') {
			if types.count == 1 {
				return first, nil
			}
			last := types.at(types.count - 1)
			children, ok := types.span(&p.arena.types.typeIDs)
			if !ok {
				return 0, p.errorf("type arena exhausted")
			}
			firstNode, err := p.parsedType(first)
			if err != nil {
				return 0, err
			}
			lastNode, err := p.parsedType(last)
			if err != nil {
				return 0, err
			}
			return p.appendType(arenaType{start: firstNode.start, end: lastNode.end, kind: typeKindIntersection, children: children})
		}
		p.skipSpace()
		next, err := p.parsePostfixType()
		if err != nil {
			return 0, err
		}
		types.append(next)
	}
}

func (p *parser) parsePostfixType() (typeID, error) {
	value, err := p.parsePrimaryType()
	if err != nil {
		return 0, err
	}

	for {
		p.skipSpace()
		if p.consumeString("...") {
			node, err := p.parsedType(value)
			if err != nil {
				return 0, err
			}
			return p.appendType(arenaType{start: node.start, end: p.pos, kind: typeKindGenericPack, payload: uint64(value)})
		}
		if !p.consumeByte('?') {
			return value, nil
		}
		node, err := p.parsedType(value)
		if err != nil {
			return 0, err
		}
		value, err = p.appendType(arenaType{start: node.start, end: p.pos, kind: typeKindNilable, payload: uint64(value)})
		if err != nil {
			return 0, err
		}
	}
}

func (p *parser) parsePrimaryType() (typeID, error) {
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
		parts := idListBuilder[stringID]{}
		parts.append(p.stringID("nil"))
		partSpan, _ := parts.span(&p.arena.types.stringIDs)
		named, ok := p.arena.types.appendNamed(arenaNamedType{parts: partSpan})
		if !ok {
			return 0, p.errorf("type arena exhausted")
		}
		return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindName, payload: uint64(named)})
	}
	if p.consumeKeyword("true") {
		return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindSingleton, scalarKind: BoolKind, payload: 1})
	}
	if p.consumeKeyword("false") {
		return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindSingleton, scalarKind: BoolKind})
	}
	if p.currentDoubleQuotedString() {
		s, err := p.parseString()
		if err != nil {
			return 0, err
		}
		return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindSingleton, scalarKind: StringKind, payload: uint64(p.typeStringID(s))})
	}
	if p.currentTokenKind(tokenNumber) {
		n, err := p.parseNumber()
		if err != nil {
			return 0, err
		}
		return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindSingleton, scalarKind: NumberKind, payload: math.Float64bits(n)})
	}
	if p.consumeString("...") {
		p.skipSpace()
		if p.done() || p.currentSymbol(")") || p.currentSymbol(",") {
			return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindVariadic})
		}
		value, err := p.parseType()
		if err != nil {
			return 0, err
		}
		node, err := p.parsedType(value)
		if err != nil {
			return 0, err
		}
		return p.appendType(arenaType{start: start, end: node.end, kind: typeKindVariadic, payload: uint64(value)})
	}
	return p.parseTypeName()
}

func (p *parser) tryParseTypeofType() (typeID, bool, error) {
	checkpoint := p.mark()
	start := checkpoint.pos
	if !p.consumeKeyword("typeof") {
		return 0, false, nil
	}

	p.skipSpace()
	if !p.consumeByte('(') {
		p.restore(checkpoint)
		return 0, false, nil
	}

	p.skipSpace()
	expr, err := p.parseExpression()
	if err != nil {
		return 0, true, err
	}
	p.skipSpace()
	if !p.consumeByte(')') {
		return 0, true, p.errorf("expected )")
	}
	value, err := p.appendType(arenaType{start: start, end: p.pos, kind: typeKindTypeof, payload: uint64(expr)})
	return value, true, err
}

func (p *parser) parseGenericFunctionType(start int) (typeID, error) {
	typeParams, typePacks, err := p.parseTypeParameterListAfterOpen()
	if err != nil {
		return 0, err
	}

	p.skipSpace()
	if !p.consumeByte('(') {
		return 0, p.errorf("expected (")
	}
	value, err := p.parseParenthesizedOrFunctionType(p.pos - 1)
	if err != nil {
		return 0, err
	}
	node, err := p.parsedType(value)
	if err != nil {
		return 0, err
	}
	fn, ok := p.arena.types.functionType(arenaFunctionTypeID(node.payload))
	if !ok {
		return 0, p.errorf("expected function type")
	}
	fn.typeParams = p.appendTypeStrings(typeParams)
	fn.typePacks = p.appendTypeStrings(typePacks)
	p.arena.types.functions[arenaFunctionTypeID(node.payload)-1] = fn
	node.start = start
	node.kind = typeKindGenericFunction
	p.arena.types.setNode(value, node)
	return value, nil
}

func (p *parser) parseParenthesizedOrFunctionType(start int) (typeID, error) {
	p.skipSpace()
	params := idListBuilder[arenaTypeParam]{}
	if !p.consumeByte(')') {
		for {
			param, err := p.parseFunctionTypeArgument()
			if err != nil {
				return 0, err
			}
			params.append(param)
			p.skipSpace()
			if p.consumeByte(')') {
				break
			}
			if !p.consumeByte(',') {
				return 0, p.errorf("expected , or )")
			}
			p.skipSpace()
		}
	}
	closeEnd := p.pos

	p.skipSpace()
	if !p.consumeString("->") {
		if params.count == 1 {
			param := params.at(0)
			if param.name == 0 && !param.variadic {
				return param.value, nil
			}
		}
		paramSpan, ok := params.span(&p.arena.types.params)
		if !ok {
			return 0, p.errorf("type arena exhausted")
		}
		fn, ok := p.arena.types.appendFunction(arenaFunctionType{params: paramSpan})
		if !ok {
			return 0, p.errorf("type arena exhausted")
		}
		return p.appendType(arenaType{start: start, end: closeEnd, kind: typeKindFunction, payload: uint64(fn)})
	}
	p.skipSpace()
	returnType, err := p.parseType()
	if err != nil {
		return 0, err
	}
	returnNode, err := p.parsedType(returnType)
	if err != nil {
		return 0, err
	}
	paramSpan, ok := params.span(&p.arena.types.params)
	if !ok {
		return 0, p.errorf("type arena exhausted")
	}
	fn, ok := p.arena.types.appendFunction(arenaFunctionType{params: paramSpan, returnType: returnType})
	if !ok {
		return 0, p.errorf("type arena exhausted")
	}
	return p.appendType(arenaType{start: start, end: returnNode.end, kind: typeKindFunction, payload: uint64(fn)})
}

func (p *parser) parseFunctionTypeArgument() (arenaTypeParam, error) {
	p.skipSpace()
	start := p.pos
	if p.consumeString("...") {
		p.skipSpace()
		if p.consumeByte(':') {
			p.skipSpace()
			value, err := p.parseType()
			if err != nil {
				return arenaTypeParam{}, err
			}
			return arenaTypeParam{value: value, variadic: true}, nil
		}
		if p.done() || p.currentSymbol(")") || p.currentSymbol(",") {
			return arenaTypeParam{variadic: true}, nil
		}
		value, err := p.parseType()
		if err != nil {
			return arenaTypeParam{}, err
		}
		node, err := p.parsedType(value)
		if err != nil {
			return arenaTypeParam{}, err
		}
		variadic, err := p.appendType(arenaType{start: start, end: node.end, kind: typeKindVariadic, payload: uint64(value)})
		return arenaTypeParam{value: variadic}, err
	}

	if p.currentIdentifier() {
		checkpoint := p.mark()
		name, err := p.parseIdentifier()
		if err != nil {
			return arenaTypeParam{}, err
		}
		p.skipSpace()
		if p.consumeByte(':') {
			p.skipSpace()
			value, err := p.parseType()
			if err != nil {
				return arenaTypeParam{}, err
			}
			return arenaTypeParam{name: p.stringID(name), value: value}, nil
		}
		p.restore(checkpoint)
	}

	value, err := p.parseType()
	if err != nil {
		return arenaTypeParam{}, err
	}
	return arenaTypeParam{value: value}, nil
}

func (p *parser) parseTableTypeBody(start int) (typeID, error) {
	p.skipSpace()
	if p.consumeByte('}') {
		table, ok := p.arena.types.appendTable(arenaTableType{})
		if !ok {
			return 0, p.errorf("type arena exhausted")
		}
		return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindTable, payload: uint64(table)})
	}

	fields := idListBuilder[arenaTypeField]{}
	for {
		access := p.parseOptionalTableFieldAccess()
		if p.consumeByte('[') {
			p.skipSpace()
			key, err := p.parseType()
			if err != nil {
				return 0, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return 0, p.errorf("expected ]")
			}
			p.skipSpace()
			if !p.consumeByte(':') {
				return 0, p.errorf("expected :")
			}
			p.skipSpace()
			value, err := p.parseType()
			if err != nil {
				return 0, err
			}
			fields.append(arenaTypeField{access: typeFieldAccess(access), key: key, value: value})
		} else if p.currentIdentifier() {
			fieldCheckpoint := p.mark()
			name, err := p.parseIdentifier()
			if err != nil {
				return 0, err
			}
			p.skipSpace()
			if p.consumeByte(':') {
				p.skipSpace()
				value, err := p.parseType()
				if err != nil {
					return 0, err
				}
				fields.append(arenaTypeField{access: typeFieldAccess(access), name: p.stringID(name), value: value})
			} else {
				if access != "" {
					return 0, p.errorf("expected :")
				}
				p.restore(fieldCheckpoint)
				value, err := p.parseType()
				if err != nil {
					return 0, err
				}
				fields.append(arenaTypeField{value: value})
			}
		} else {
			if access != "" {
				return 0, p.errorf("expected table property or indexer")
			}
			value, err := p.parseType()
			if err != nil {
				return 0, err
			}
			fields.append(arenaTypeField{value: value})
		}

		p.skipSpace()
		if p.consumeByte('}') {
			return p.finishTableType(start, fields)
		}
		if !p.consumeByte(',') && !p.consumeByte(';') {
			return 0, p.errorf("expected , ; or }")
		}
		p.skipSpace()
		if p.consumeByte('}') {
			return p.finishTableType(start, fields)
		}
	}
}

func (p *parser) finishTableType(start int, fields idListBuilder[arenaTypeField]) (typeID, error) {
	fieldSpan, ok := fields.span(&p.arena.types.fields)
	if !ok {
		return 0, p.errorf("type arena exhausted")
	}
	table, ok := p.arena.types.appendTable(arenaTableType{fields: fieldSpan})
	if !ok {
		return 0, p.errorf("type arena exhausted")
	}
	return p.appendType(arenaType{start: start, end: p.pos, kind: typeKindTable, payload: uint64(table)})
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

func (p *parser) parseTypeName() (typeID, error) {
	start := p.pos
	name, err := p.parseTypeNamePart()
	if err != nil {
		return 0, err
	}
	parts := idListBuilder[stringID]{}
	parts.append(p.stringID(name))
	nameEnd := p.pos
	typeArgs, err := p.parseOptionalTypeArguments()
	if err != nil {
		return 0, err
	}
	end := nameEnd
	if typeArgs.count != 0 {
		end = p.pos
	}

	for {
		p.skipSpace()
		if p.currentSymbol("...") {
			return p.finishNamedType(start, end, parts, typeArgs)
		}
		if !p.consumeByte('.') {
			return p.finishNamedType(start, end, parts, typeArgs)
		}
		p.skipSpace()
		part, err := p.parseTypeNamePart()
		if err != nil {
			return 0, err
		}
		parts.append(p.stringID(part))
		partEnd := p.pos
		nextArgs, err := p.parseOptionalTypeArguments()
		if err != nil {
			return 0, err
		}
		if nextArgs.count > 0 {
			typeArgs = nextArgs
		}
		end = partEnd
		if nextArgs.count != 0 {
			end = p.pos
		}
	}
}

func (p *parser) finishNamedType(start, end int, parts idListBuilder[stringID], args nodeSpan) (typeID, error) {
	partSpan, ok := parts.span(&p.arena.types.stringIDs)
	if !ok {
		return 0, p.errorf("type arena exhausted")
	}
	named, ok := p.arena.types.appendNamed(arenaNamedType{parts: partSpan, args: args})
	if !ok {
		return 0, p.errorf("type arena exhausted")
	}
	return p.appendType(arenaType{start: start, end: end, kind: typeKindName, payload: uint64(named)})
}

func (p *parser) appendTypeStrings(values []string) nodeSpan {
	ids := idListBuilder[stringID]{}
	for _, value := range values {
		ids.append(p.stringID(value))
	}
	span, _ := ids.span(&p.arena.types.stringIDs)
	return span
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

func (p *parser) parseOptionalTypeArguments() (nodeSpan, error) {
	p.skipSpace()
	if !p.consumeByte('<') {
		return nodeSpan{}, nil
	}

	args := idListBuilder[typeID]{}
	for {
		p.skipSpace()
		arg, err := p.parseType()
		if err != nil {
			return nodeSpan{}, err
		}
		args.append(arg)
		p.skipSpace()
		if p.consumeByte('>') {
			span, ok := args.span(&p.arena.types.typeIDs)
			if !ok {
				return nodeSpan{}, p.errorf("type arena exhausted")
			}
			return span, nil
		}
		if !p.consumeByte(',') {
			return nodeSpan{}, p.errorf("expected , or >")
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
			target.selectors = append(target.selectors, selector{index: index})
			target.end = p.pos
			continue
		}
		break
	}

	return target, nil
}

func (p *parser) parseExpression() (expressionID, error) {
	if err := p.enterNesting("expression"); err != nil {
		return 0, err
	}
	defer p.leaveNesting()
	first, err := p.parseAndExpression()
	if err != nil {
		return 0, err
	}
	terms := idListBuilder[andExpressionID]{}
	terms.append(first)

	for {
		p.skipSpace()
		if !p.consumeKeyword("or") {
			break
		}

		next, err := p.parseAndExpression()
		if err != nil {
			return 0, err
		}
		terms.append(next)
	}
	span, ok := terms.span(&p.arena.expressionTerms)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	return p.arena.appendExpression(arenaExpression{terms: span}), nil
}

func (p *parser) parseAndExpression() (andExpressionID, error) {
	first, err := p.parseComparisonExpression()
	if err != nil {
		return 0, err
	}
	terms := idListBuilder[comparisonExpressionID]{}
	terms.append(first)

	for {
		p.skipSpace()
		if !p.consumeKeyword("and") {
			break
		}

		next, err := p.parseComparisonExpression()
		if err != nil {
			return 0, err
		}
		terms.append(next)
	}
	span, ok := terms.span(&p.arena.andTerms)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	return p.arena.appendAnd(arenaAndExpression{terms: span}), nil
}

func (p *parser) parseComparisonExpression() (comparisonExpressionID, error) {
	left, err := p.parseConcatExpression()
	if err != nil {
		return 0, err
	}
	node := arenaComparisonExpression{left: left}

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
		return p.arena.appendComparison(node), nil
	}

	p.skipSpace()
	right, err := p.parseConcatExpression()
	if err != nil {
		return 0, err
	}
	node.op = arenaComparisonOperator(op)
	node.right = right
	return p.arena.appendComparison(node), nil
}

func (p *parser) parseConcatExpression() (concatExpressionID, error) {
	p.skipSpace()
	first, err := p.parseAdditiveExpression()
	if err != nil {
		return 0, err
	}
	rest := idListBuilder[additiveExpressionID]{}

	for {
		p.skipSpace()
		if !p.consumeString("..") {
			break
		}

		p.skipSpace()
		next, err := p.parseAdditiveExpression()
		if err != nil {
			return 0, err
		}
		rest.append(next)
	}
	span, ok := rest.span(&p.arena.concatRest)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	return p.arena.appendConcat(arenaConcatExpression{first: first, rest: span}), nil
}

func (p *parser) parseAdditiveExpression() (additiveExpressionID, error) {
	p.skipSpace()
	first, err := p.parseMultiplicativeExpression()
	if err != nil {
		return 0, err
	}
	parts := idListBuilder[arenaAdditivePart]{}

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
			return 0, err
		}
		parts.append(arenaAdditivePart{op: arenaAdditiveOperator(op), value: next})
	}
	span, ok := parts.span(&p.arena.additiveRest)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	return p.arena.appendAdditive(arenaAdditiveExpression{first: first, rest: span}), nil
}

func (p *parser) parseMultiplicativeExpression() (multiplicativeExpressionID, error) {
	p.skipSpace()
	first, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	parts := idListBuilder[arenaMultiplicativePart]{}

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
			return 0, err
		}
		parts.append(arenaMultiplicativePart{op: arenaMultiplicativeOperator(op), value: next})
	}
	span, ok := parts.span(&p.arena.multiplicativeRest)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	return p.arena.appendMultiplicative(arenaMultiplicativeExpression{first: first, rest: span}), nil
}

func (p *parser) parseTerm() (termID, error) {
	if err := p.enterNesting("term"); err != nil {
		return 0, err
	}
	defer p.leaveNesting()
	p.skipSpace()
	start := p.pos
	if p.consumeKeyword("not") {
		value, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		return p.appendUnaryTerm(termKindUnaryNot, value, start), nil
	}
	if p.consumeByte('-') {
		value, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		return p.appendUnaryTerm(termKindUnaryMinus, value, start), nil
	}
	if p.consumeByte('#') {
		value, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		return p.appendUnaryTerm(termKindUnaryLength, value, start), nil
	}

	value, err := p.parsePrimaryTerm()
	if err != nil {
		return 0, err
	}
	value, err = p.parseTermSuffixes(value)
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.consumeByte('^') {
		exponent, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		base, _ := p.arena.term(value)
		power := p.arena.appendPower(arenaPower{base: value, exponent: exponent})
		return p.arena.appendTerm(arenaTerm{start: base.start, end: p.termEnd(exponent), kind: termKindPower, payload: uint64(power)}), nil
	}
	return value, nil
}

func (p *parser) termEnd(id termID) int {
	if node, ok := p.arena.term(id); ok {
		return node.end
	}
	return 0
}

func (p *parser) appendUnaryTerm(kind termKind, child termID, start int) termID {
	return p.arena.appendTerm(arenaTerm{start: start, end: p.termEnd(child), kind: kind, payload: uint64(child)})
}

func (p *parser) stringID(value string) stringID {
	if value == "" {
		return 0
	}
	p.arena.strings = append(p.arena.strings, value)
	return stringID(len(p.arena.strings))
}

// typeStringID interns a type-syntax string without constructing a runtime
// Value. Unlike ordinary identifier storage, the empty singleton string needs
// a real non-zero ID so zero remains the absence value.
func (p *parser) typeStringID(value string) stringID {
	if value != "" {
		return p.stringID(value)
	}
	p.arena.strings = append(p.arena.strings, value)
	return stringID(len(p.arena.strings))
}

func typeFieldAccess(value string) arenaTypeFieldAccess {
	switch value {
	case "read":
		return arenaTypeFieldAccessRead
	case "write":
		return arenaTypeFieldAccessWrite
	default:
		return arenaTypeFieldAccessNone
	}
}

func (p *parser) literalStringID(value string) stringID {
	var id stringID
	if value == "" {
		p.arena.strings = append(p.arena.strings, value)
		id = stringID(len(p.arena.strings))
	} else {
		id = p.stringID(value)
	}
	if p.arena.stringLiterals == nil {
		p.arena.stringLiterals = make(map[stringID]Value)
	}
	p.arena.stringLiterals[id] = StringValue(value)
	return id
}

func (p *parser) parsePrimaryTerm() (termID, error) {
	p.skipSpace()
	start := p.pos
	if p.consumeByte('(') {
		value, err := p.parseExpression()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if !p.consumeByte(')') {
			return 0, p.errorf("expected )")
		}
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindGroup, payload: uint64(value)}), nil
	}
	if p.matchKeyword("if") {
		ifExpr, err := p.parseIfExpression()
		if err != nil {
			return 0, err
		}
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindIf, payload: uint64(ifExpr)}), nil
	}
	if p.consumeKeyword("nil") {
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindNil}), nil
	}
	if p.consumeKeyword("true") {
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindBool, payload: 1}), nil
	}
	if p.consumeKeyword("false") {
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindBool}), nil
	}
	if p.consumeString("...") {
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindVararg}), nil
	}
	if p.matchKeyword("function") {
		fn, err := p.parseFunctionExpression()
		if err != nil {
			return 0, err
		}
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindFunction, payload: uint64(fn)}), nil
	}
	if p.currentDoubleQuotedString() {
		s, err := p.parseString()
		if err != nil {
			return 0, err
		}
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindString, payload: uint64(p.literalStringID(s))}), nil
	}
	if p.currentSymbol("{") {
		table, err := p.parseTable()
		if err != nil {
			return 0, err
		}
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindTable, payload: uint64(table)}), nil
	}
	if p.currentTokenKind(tokenNumber) {
		n, err := p.parseNumber()
		if err != nil {
			return 0, err
		}
		return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindNumber, payload: math.Float64bits(n)}), nil
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return 0, err
	}
	return p.arena.appendTerm(arenaTerm{start: start, end: p.pos, kind: termKindName, payload: uint64(p.stringID(name))}), nil
}

func (p *parser) parseIfExpression() (arenaIfExpressionID, error) {
	if !p.consumeKeyword("if") {
		return 0, p.errorf("expected if")
	}
	return p.parseIfExpressionBody()
}

func (p *parser) parseIfExpressionBody() (arenaIfExpressionID, error) {
	if err := p.enterNesting("if-expression"); err != nil {
		return 0, err
	}
	defer p.leaveNesting()
	p.skipSpace()
	condition, err := p.parseExpression()
	if err != nil {
		return 0, err
	}

	p.skipSpace()
	if !p.consumeKeyword("then") {
		return 0, p.errorf("expected then")
	}

	p.skipSpace()
	thenValue, err := p.parseExpression()
	if err != nil {
		return 0, err
	}

	p.skipSpace()
	var elseValue expressionID
	if p.consumeKeyword("elseif") {
		nested, err := p.parseIfExpressionBody()
		if err != nil {
			return 0, err
		}
		term := p.arena.appendTerm(arenaTerm{kind: termKindIf, payload: uint64(nested)})
		elseValue, err = p.wrapTermExpression(term)
		if err != nil {
			return 0, err
		}
	} else if p.consumeKeyword("else") {
		p.skipSpace()
		elseValue, err = p.parseExpression()
		if err != nil {
			return 0, err
		}
	} else {
		return 0, p.errorf("expected elseif or else")
	}
	return p.arena.appendIfExpression(arenaIfExpression{condition: condition, thenValue: thenValue, elseValue: elseValue}), nil
}

func (p *parser) wrapTermExpression(value termID) (expressionID, error) {
	mul := p.arena.appendMultiplicative(arenaMultiplicativeExpression{first: value})
	add := p.arena.appendAdditive(arenaAdditiveExpression{first: mul})
	cat := p.arena.appendConcat(arenaConcatExpression{first: add})
	cmp := p.arena.appendComparison(arenaComparisonExpression{left: cat})
	var comparisons idListBuilder[comparisonExpressionID]
	comparisons.append(cmp)
	comparisonSpan, ok := comparisons.span(&p.arena.andTerms)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	and := p.arena.appendAnd(arenaAndExpression{terms: comparisonSpan})
	var ands idListBuilder[andExpressionID]
	ands.append(and)
	andSpan, ok := ands.span(&p.arena.expressionTerms)
	if !ok {
		return 0, p.errorf("expression arena exhausted")
	}
	return p.arena.appendExpression(arenaExpression{terms: andSpan}), nil
}

func (p *parser) parseTermSuffixes(value termID) (termID, error) {
	return p.parseTermSuffixesWithCasts(value, true)
}

func (p *parser) parseTermSuffixesWithCasts(value termID, allowCasts bool) (termID, error) {
	var selectors idListBuilder[arenaSelector]
	flushSelectors := func() error {
		if selectors.count == 0 {
			return nil
		}
		node, ok := p.arena.term(value)
		if !ok || node.selectors.count != 0 {
			return p.errorf("invalid term selector state")
		}
		span, ok := selectors.span(&p.arena.selectors)
		if !ok {
			return p.errorf("expression arena exhausted")
		}
		node.selectors = span
		p.arena.terms[value-1] = node
		return nil
	}
	finish := func() (termID, error) {
		if err := flushSelectors(); err != nil {
			return 0, err
		}
		return value, nil
	}

	for {
		p.skipSpace()
		if p.currentSymbol("..") {
			return finish()
		}
		if allowCasts && p.consumeString("::") {
			p.skipSpace()
			cast, err := p.parseType()
			if err != nil {
				return 0, err
			}
			node, ok := p.arena.term(value)
			if !ok {
				return 0, p.errorf("invalid term")
			}
			node.castType = cast
			castNode, err := p.parsedType(cast)
			if err != nil {
				return 0, err
			}
			node.end = castNode.end
			p.arena.terms[value-1] = node
			continue
		}
		if !allowCasts && p.currentSymbol("::") {
			return finish()
		}
		if p.consumeByte('.') {
			field, err := p.parseIdentifier()
			if err != nil {
				return 0, err
			}
			selectors.append(arenaSelector{field: p.stringID(field)})
			node, ok := p.arena.term(value)
			if !ok {
				return 0, p.errorf("invalid term")
			}
			node.end = p.pos
			p.arena.terms[value-1] = node
			continue
		}
		if p.consumeByte('[') {
			index, err := p.parseExpression()
			if err != nil {
				return 0, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return 0, p.errorf("expected ]")
			}
			selectors.append(arenaSelector{index: index})
			node, ok := p.arena.term(value)
			if !ok {
				return 0, p.errorf("invalid term")
			}
			node.end = p.pos
			p.arena.terms[value-1] = node
			continue
		}
		typeArgs, err := p.parseOptionalCallTypeArguments()
		if err != nil {
			return 0, err
		}
		if p.consumeByte('(') {
			if err := flushSelectors(); err != nil {
				return 0, err
			}
			target, ok := p.arena.term(value)
			if !ok {
				return 0, p.errorf("invalid term")
			}
			argSpan, err := p.parseArguments()
			if err != nil {
				return 0, err
			}
			call := p.arena.appendCall(arenaCall{target: value, typeArgs: typeArgs, args: argSpan})
			value = p.arena.appendTerm(arenaTerm{start: target.start, end: p.pos, kind: termKindCall, payload: uint64(call)})
			selectors = idListBuilder[arenaSelector]{}
			continue
		}
		if typeArgs.count > 0 {
			return 0, p.errorf("expected (")
		}
		if p.consumeByte(':') {
			if err := flushSelectors(); err != nil {
				return 0, err
			}
			method, err := p.parseIdentifier()
			if err != nil {
				return 0, err
			}
			methodEnd := p.pos
			p.skipSpace()
			typeArgs, err := p.parseOptionalCallTypeArguments()
			if err != nil {
				return 0, err
			}
			if !p.consumeByte('(') {
				return 0, p.errorf("expected (")
			}
			argSpan, err := p.parseArguments()
			if err != nil {
				return 0, err
			}
			receiver := value
			receiverNode, ok := p.arena.term(receiver)
			if !ok {
				return 0, p.errorf("invalid term")
			}
			targetSelectors := idListBuilder[arenaSelector]{}
			if receiverNode.selectors.count != 0 {
				existing, ok := p.arena.selectorIDs(receiverNode.selectors)
				if !ok {
					return 0, p.errorf("invalid term selector state")
				}
				for _, selector := range existing {
					targetSelectors.append(selector)
				}
			}
			targetSelectors.append(arenaSelector{field: p.stringID(method)})
			targetSpan, ok := targetSelectors.span(&p.arena.selectors)
			if !ok {
				return 0, p.errorf("expression arena exhausted")
			}
			targetNode := receiverNode
			targetNode.id = 0
			targetNode.end = methodEnd
			targetNode.selectors = targetSpan
			target := p.arena.appendTerm(targetNode)
			call := p.arena.appendCall(arenaCall{target: target, receiver: receiver, typeArgs: typeArgs, args: argSpan})
			value = p.arena.appendTerm(arenaTerm{start: receiverNode.start, end: p.pos, kind: termKindCall, payload: uint64(call)})
			selectors = idListBuilder[arenaSelector]{}
			continue
		}
		return finish()
	}
}

func (p *parser) parseOptionalCallTypeArguments() (nodeSpan, error) {
	p.skipSpace()
	if !p.consumeString("<<") {
		return nodeSpan{}, nil
	}

	args := idListBuilder[typeID]{}
	for {
		p.skipSpace()
		arg, err := p.parseType()
		if err != nil {
			return nodeSpan{}, err
		}
		args.append(arg)

		p.skipSpace()
		if p.consumeString(">>") {
			span, ok := args.span(&p.arena.types.typeIDs)
			if !ok {
				return nodeSpan{}, p.errorf("type arena exhausted")
			}
			return span, nil
		}
		if !p.consumeByte(',') {
			return nodeSpan{}, p.errorf("expected , or >>")
		}
	}
}

func (p *parser) parseTable() (arenaTableID, error) {
	if err := p.enterNesting("table"); err != nil {
		return 0, err
	}
	defer p.leaveNesting()
	if !p.consumeByte('{') {
		return 0, p.errorf("expected table")
	}

	fields := idListBuilder[arenaTableField]{}
	finish := func() (arenaTableID, error) {
		span, ok := fields.span(&p.arena.tableFields)
		if !ok {
			return 0, p.errorf("expression arena exhausted")
		}
		return p.arena.appendTable(arenaTable{fields: span}), nil
	}
	p.skipSpace()
	if p.consumeByte('}') {
		return finish()
	}

	arrayIndex := 1
	for {
		p.skipSpace()
		if p.consumeByte('[') {
			key, err := p.parseExpression()
			if err != nil {
				return 0, err
			}
			p.skipSpace()
			if !p.consumeByte(']') {
				return 0, p.errorf("expected ]")
			}
			p.skipSpace()
			if !p.consumeByte('=') {
				return 0, p.errorf("expected =")
			}
			p.skipSpace()
			value, err := p.parseExpression()
			if err != nil {
				return 0, err
			}
			fields.append(arenaTableField{key: key, value: value})
			done, err := p.finishTableField()
			if err != nil {
				return 0, err
			}
			if done {
				return finish()
			}
			continue
		}

		if p.currentIdentifier() {
			fieldCheckpoint := p.mark()
			name, err := p.parseIdentifier()
			if err != nil {
				return 0, err
			}

			p.skipSpace()
			if p.consumeByte('=') {
				value, err := p.parseExpression()
				if err != nil {
					return 0, err
				}
				fields.append(arenaTableField{name: p.stringID(name), value: value})
				done, err := p.finishTableField()
				if err != nil {
					return 0, err
				}
				if done {
					return finish()
				}
				continue
			}
			p.restore(fieldCheckpoint)
		}

		value, err := p.parseExpression()
		if err != nil {
			return 0, err
		}
		fields.append(arenaTableField{arrayIndex: arrayIndex, value: value})
		arrayIndex++
		done, err := p.finishTableField()
		if err != nil {
			return 0, err
		}
		if done {
			return finish()
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

func (p *parser) parseArguments() (nodeSpan, error) {
	p.skipSpace()
	if p.consumeByte(')') {
		return nodeSpan{}, nil
	}

	args := idListBuilder[expressionID]{}
	for {
		arg, err := p.parseExpression()
		if err != nil {
			return nodeSpan{}, err
		}
		args.append(arg)

		p.skipSpace()
		if p.consumeByte(')') {
			span, ok := args.span(&p.arena.callArgs)
			if !ok {
				return nodeSpan{}, p.errorf("expression arena exhausted")
			}
			return span, nil
		}
		if !p.consumeByte(',') {
			return nodeSpan{}, p.errorf("expected , or )")
		}
	}
}

func (p *parser) parseString() (string, error) {
	token, ok := p.consumeToken(tokenString)
	if !ok {
		return "", p.errorf("expected string")
	}
	value := token.stringValue(p.source, p.stringPool)
	if token.payload == 0 {
		// Clone source-span strings before they enter the syntax tree. A parsed
		// program must not retain the complete source through a small literal.
		value = strings.Clone(value)
	}
	return value, nil
}

func (p *parser) parseNumber() (float64, error) {
	token, ok := p.consumeToken(tokenNumber)
	if !ok {
		return 0, p.errorf("expected number")
	}
	return token.numberValue(), nil
}

func (p *parser) parseIdentifier() (string, error) {
	token, ok := p.consumeToken(tokenIdentifier)
	if !ok {
		return "", p.errorf("expected identifier")
	}
	return token.textAt(p.source), nil
}

func (p *parser) consumeKeyword(keyword string) bool {
	token, ok := p.currentToken()
	if !ok || !token.matchesWordAt(p.source, p.pos, keyword) {
		return false
	}
	p.tokenIndex++
	p.pos = token.endOffset()
	return true
}

func (p *parser) matchKeyword(keyword string) bool {
	token, ok := p.currentToken()
	return ok && token.matchesWordAt(p.source, p.pos, keyword)
}

func (p *parser) consumeByte(ch byte) bool {
	token, ok := p.currentToken()
	if !ok ||
		token.kind != tokenSymbol ||
		token.startOffset() != p.pos ||
		token.endOffset()-token.startOffset() != 1 ||
		p.source[token.startOffset()] != ch {
		return false
	}
	p.tokenIndex++
	p.pos = token.endOffset()
	return true
}

func (p *parser) consumeString(s string) bool {
	token, ok := p.currentToken()
	if !ok || token.kind != tokenSymbol || token.startOffset() != p.pos || !token.rawEquals(p.source, s) {
		return false
	}
	p.tokenIndex++
	p.pos = token.endOffset()
	return true
}

func (p *parser) skipSpace() {
	p.advanceTokenIndex()
	if p.tokenIndex >= len(p.tokens) {
		p.pos = len(p.source)
		return
	}
	if next := p.tokens[p.tokenIndex].startOffset(); next > p.pos {
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
	if token.startOffset() != p.pos || token.kind != kind {
		return sourceToken{}, false
	}
	p.tokenIndex++
	p.pos = token.endOffset()
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
	return ok && token.startOffset() == p.pos && token.kind == kind
}

func (p *parser) currentIdentifier() bool {
	return p.currentTokenKind(tokenIdentifier)
}

func (p *parser) currentSymbol(symbol string) bool {
	token, ok := p.currentToken()
	return ok && token.startOffset() == p.pos && token.kind == tokenSymbol && token.rawEquals(p.source, symbol)
}

func (p *parser) currentDoubleQuotedString() bool {
	token, ok := p.currentToken()
	return ok && token.startOffset() == p.pos && token.kind == tokenString && p.pos < len(p.source) && p.source[p.pos] == '"'
}

func (p *parser) advanceTokenIndex() {
	for p.tokenIndex < len(p.tokens) && p.tokens[p.tokenIndex].endOffset() <= p.pos {
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
