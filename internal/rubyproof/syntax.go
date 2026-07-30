package rubyproof

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	maximumSourceBytes = 64 << 10
	maximumTokens      = 8192
	maximumNodes       = 4096
	maximumDiagnostics = 128
)

// Span is a half-open byte range with one-based line and column positions.
type Span struct {
	Start, End             int
	StartLine, StartColumn int
	EndLine, EndColumn     int
}

// Diagnostic is one deterministic source diagnostic.
type Diagnostic struct {
	Span    Span
	Message string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d-%d:%d: %s", d.Span.StartLine, d.Span.StartColumn, d.Span.EndLine, d.Span.EndColumn, d.Message)
}

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenNewline
	tokenIdentifier
	tokenConstant
	tokenInstanceVariable
	tokenInteger
	tokenString
	tokenLeftParen
	tokenRightParen
	tokenLeftBracket
	tokenRightBracket
	tokenComma
	tokenDot
	tokenPipe
	tokenLess
	tokenPlus
	tokenStar
	tokenAssign
	tokenColon
)

type token struct {
	kind tokenKind
	text string
	span Span
}

type sourcePosition struct {
	offset, line, column int
}

type lexer struct {
	source               string
	offset, line, column int
	tokens               []token
	diagnostics          []Diagnostic
}

func lex(source string) ([]token, []Diagnostic) {
	if len(source) > maximumSourceBytes {
		return nil, []Diagnostic{{Message: fmt.Sprintf("ruby: source bytes %d exceed limit %d", len(source), maximumSourceBytes)}}
	}
	l := lexer{source: source, line: 1, column: 1}
	for l.offset < len(source) && len(l.diagnostics) == 0 {
		if l.skipHorizontalSpaceOrComment() {
			continue
		}
		start := l.position()
		character := l.peek()
		switch {
		case character == '\n':
			l.advance()
			l.add(tokenNewline, start)
		case character == '@':
			l.advance()
			if !lowerASCII(l.peek()) && l.peek() != '_' {
				l.problem(l.span(start), "ruby: instance variable name expected after @")
				continue
			}
			l.scanIdentifierTail()
			l.add(tokenInstanceVariable, start)
		case lowerASCII(character) || character == '_':
			l.advance()
			l.scanIdentifierTail()
			if l.peek() == '?' || l.peek() == '!' {
				l.advance()
			}
			l.add(tokenIdentifier, start)
		case upperASCII(character):
			l.advance()
			l.scanIdentifierTail()
			l.add(tokenConstant, start)
		case digitASCII(character):
			l.advance()
			for digitASCII(l.peek()) {
				l.advance()
			}
			l.add(tokenInteger, start)
		case character == '"':
			l.advance()
			for l.offset < len(source) && l.peek() != '"' && l.peek() != '\n' {
				if l.peek() == '\\' {
					l.problem(l.span(start), "ruby: string escapes are outside the proof subset")
					break
				}
				l.advance()
			}
			if len(l.diagnostics) != 0 {
				continue
			}
			if l.peek() != '"' {
				l.problem(l.span(start), "ruby: unterminated string")
				continue
			}
			l.advance()
			l.add(tokenString, start)
		default:
			l.advance()
			var kind tokenKind
			switch character {
			case '(':
				kind = tokenLeftParen
			case ')':
				kind = tokenRightParen
			case '[':
				kind = tokenLeftBracket
			case ']':
				kind = tokenRightBracket
			case ',':
				kind = tokenComma
			case '.':
				kind = tokenDot
			case '|':
				kind = tokenPipe
			case '<':
				kind = tokenLess
			case '+':
				kind = tokenPlus
			case '*':
				kind = tokenStar
			case '=':
				kind = tokenAssign
			case ':':
				kind = tokenColon
			default:
				l.problem(l.span(start), fmt.Sprintf("ruby: invalid character %q", character))
				continue
			}
			l.add(kind, start)
		}
		if len(l.tokens) > maximumTokens {
			l.problem(l.span(start), fmt.Sprintf("ruby: token count exceeds limit %d", maximumTokens))
		}
	}
	end := l.position()
	l.tokens = append(l.tokens, token{kind: tokenEOF, span: l.span(end)})
	return l.tokens, l.diagnostics
}

func (l *lexer) skipHorizontalSpaceOrComment() bool {
	if l.peek() == ' ' || l.peek() == '\t' || l.peek() == '\r' {
		l.advance()
		return true
	}
	if l.peek() == '#' {
		for l.offset < len(l.source) && l.peek() != '\n' {
			l.advance()
		}
		return true
	}
	return false
}

func (l *lexer) scanIdentifierTail() {
	for lowerASCII(l.peek()) || upperASCII(l.peek()) || digitASCII(l.peek()) || l.peek() == '_' {
		l.advance()
	}
}

func (l *lexer) peek() byte {
	if l.offset >= len(l.source) {
		return 0
	}
	return l.source[l.offset]
}

func (l *lexer) advance() {
	if l.offset >= len(l.source) {
		return
	}
	character := l.source[l.offset]
	l.offset++
	if character == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
}

func (l *lexer) position() sourcePosition {
	return sourcePosition{offset: l.offset, line: l.line, column: l.column}
}

func (l *lexer) span(start sourcePosition) Span {
	return Span{Start: start.offset, End: l.offset, StartLine: start.line, StartColumn: start.column, EndLine: l.line, EndColumn: l.column}
}

func (l *lexer) add(kind tokenKind, start sourcePosition) {
	l.tokens = append(l.tokens, token{kind: kind, text: l.source[start.offset:l.offset], span: l.span(start)})
}

func (l *lexer) problem(span Span, message string) {
	if len(l.diagnostics) < maximumDiagnostics {
		l.diagnostics = append(l.diagnostics, Diagnostic{Span: span, Message: message})
	}
}

func lowerASCII(character byte) bool { return character >= 'a' && character <= 'z' }
func upperASCII(character byte) bool { return character >= 'A' && character <= 'Z' }
func digitASCII(character byte) bool { return character >= '0' && character <= '9' }

type statementKind uint8

const (
	statementClass statementKind = iota + 1
	statementModule
	statementMethod
	statementAssign
	statementExpression
	statementReturn
	statementRaise
	statementBegin
)

type expressionKind uint8

const (
	expressionInteger expressionKind = iota + 1
	expressionString
	expressionSymbol
	expressionLocal
	expressionInstanceVariable
	expressionConstant
	expressionArray
	expressionBinary
	expressionCall
	expressionYield
)

type statement struct {
	kind        statementKind
	span        Span
	name        string
	superclass  string
	parameters  []string
	body        []*statement
	rescueClass string
	rescueBody  []*statement
	ensureBody  []*statement
	expression  *expression
}

type expression struct {
	kind     expressionKind
	span     Span
	text     string
	integer  int64
	left     *expression
	right    *expression
	items    []*expression
	receiver *expression
	name     string
	args     []*expression
	block    *block
}

type block struct {
	span       Span
	parameters []string
	body       []*statement
}

type syntaxModule struct {
	statements []*statement
}

type parser struct {
	tokens      []token
	index       int
	nodes       int
	diagnostics []Diagnostic
}

func parse(tokens []token) (*syntaxModule, []Diagnostic) {
	p := parser{tokens: tokens}
	p.skipNewlines()
	statements := p.parseStatements(nil)
	if p.current().kind != tokenEOF && len(p.diagnostics) == 0 {
		p.problem(p.current().span, "ruby: unexpected trailing input")
	}
	if len(p.diagnostics) != 0 {
		return nil, p.diagnostics
	}
	return &syntaxModule{statements: statements}, nil
}

func (p *parser) parseStatements(terminators map[string]bool) []*statement {
	var out []*statement
	for p.current().kind != tokenEOF {
		p.skipNewlines()
		if p.current().kind == tokenEOF || terminators != nil && terminators[p.current().text] {
			break
		}
		item := p.parseStatement()
		if item != nil {
			out = append(out, item)
		}
		if len(p.diagnostics) != 0 {
			break
		}
		if p.current().kind != tokenNewline && p.current().kind != tokenEOF && !(terminators != nil && terminators[p.current().text]) {
			p.problem(p.current().span, "ruby: newline expected after statement")
			break
		}
	}
	return out
}

func (p *parser) parseStatement() *statement {
	start := p.current().span
	if p.atWord("class") {
		return p.parseClass(start)
	}
	if p.atWord("module") {
		return p.parseModule(start)
	}
	if p.atWord("def") {
		return p.parseMethod(start)
	}
	if p.atWord("begin") {
		return p.parseBegin(start)
	}
	if p.atWord("return") {
		p.advance()
		value := p.parseParenthesizedKeywordExpression("return")
		return p.newStatement(&statement{kind: statementReturn, span: joinSpan(start, value.span), expression: value})
	}
	if p.atWord("raise") {
		p.advance()
		value := p.parseParenthesizedKeywordExpression("raise")
		return p.newStatement(&statement{kind: statementRaise, span: joinSpan(start, value.span), expression: value})
	}
	if (p.current().kind == tokenIdentifier || p.current().kind == tokenInstanceVariable) && p.peek(1).kind == tokenAssign {
		name := p.current()
		p.advance()
		p.advance()
		value := p.parseExpression()
		return p.newStatement(&statement{kind: statementAssign, span: joinSpan(start, value.span), name: name.text, expression: value})
	}
	value := p.parseExpression()
	return p.newStatement(&statement{kind: statementExpression, span: value.span, expression: value})
}

func (p *parser) parseClass(start Span) *statement {
	p.advance()
	name := p.expect(tokenConstant, "class name")
	var superclass string
	if p.current().kind == tokenLess {
		p.advance()
		superclass = p.expect(tokenConstant, "superclass name").text
	}
	p.expectLineEnd("class header")
	body := p.parseStatements(map[string]bool{"end": true})
	end := p.expectWord("end")
	return p.newStatement(&statement{kind: statementClass, span: joinSpan(start, end.span), name: name.text, superclass: superclass, body: body})
}

func (p *parser) parseModule(start Span) *statement {
	p.advance()
	name := p.expect(tokenConstant, "module name")
	p.expectLineEnd("module header")
	body := p.parseStatements(map[string]bool{"end": true})
	end := p.expectWord("end")
	return p.newStatement(&statement{kind: statementModule, span: joinSpan(start, end.span), name: name.text, body: body})
}

func (p *parser) parseMethod(start Span) *statement {
	p.advance()
	name := p.expect(tokenIdentifier, "method name")
	parameters := p.parseParameterList()
	p.expectLineEnd("method header")
	body := p.parseStatements(map[string]bool{"ensure": true, "end": true})
	if p.atWord("ensure") {
		ensureStart := p.current().span
		p.advance()
		p.expectLineEnd("ensure")
		ensureBody := p.parseStatements(map[string]bool{"end": true})
		body = []*statement{p.newStatement(&statement{kind: statementBegin, span: joinSpan(ensureStart, p.current().span), body: body, ensureBody: ensureBody})}
	}
	end := p.expectWord("end")
	return p.newStatement(&statement{kind: statementMethod, span: joinSpan(start, end.span), name: name.text, parameters: parameters, body: body})
}

func (p *parser) parseBegin(start Span) *statement {
	p.advance()
	p.expectLineEnd("begin")
	body := p.parseStatements(map[string]bool{"rescue": true, "ensure": true, "end": true})
	var rescueClass string
	var rescueBody []*statement
	if p.atWord("rescue") {
		p.advance()
		rescueClass = p.expect(tokenConstant, "rescue class").text
		p.expectLineEnd("rescue")
		rescueBody = p.parseStatements(map[string]bool{"ensure": true, "end": true})
	}
	var ensureBody []*statement
	if p.atWord("ensure") {
		p.advance()
		p.expectLineEnd("ensure")
		ensureBody = p.parseStatements(map[string]bool{"end": true})
	}
	end := p.expectWord("end")
	return p.newStatement(&statement{kind: statementBegin, span: joinSpan(start, end.span), body: body, rescueClass: rescueClass, rescueBody: rescueBody, ensureBody: ensureBody})
}

func (p *parser) parseParameterList() []string {
	p.expect(tokenLeftParen, "(")
	var parameters []string
	if p.current().kind != tokenRightParen {
		for {
			parameters = append(parameters, p.expect(tokenIdentifier, "parameter").text)
			if p.current().kind != tokenComma {
				break
			}
			p.advance()
		}
	}
	p.expect(tokenRightParen, ")")
	return parameters
}

func (p *parser) parseParenthesizedKeywordExpression(keyword string) *expression {
	p.expect(tokenLeftParen, "( after "+keyword)
	value := p.parseExpression()
	p.expect(tokenRightParen, ") after "+keyword)
	return value
}

func (p *parser) parseExpression() *expression { return p.parseAdd() }

func (p *parser) parseAdd() *expression {
	left := p.parseMultiply()
	for p.current().kind == tokenPlus {
		operator := p.current()
		p.advance()
		right := p.parseMultiply()
		left = p.newExpression(&expression{kind: expressionBinary, span: joinSpan(left.span, right.span), text: operator.text, left: left, right: right})
	}
	return left
}

func (p *parser) parseMultiply() *expression {
	left := p.parsePostfix()
	for p.current().kind == tokenStar {
		operator := p.current()
		p.advance()
		right := p.parsePostfix()
		left = p.newExpression(&expression{kind: expressionBinary, span: joinSpan(left.span, right.span), text: operator.text, left: left, right: right})
	}
	return left
}

func (p *parser) parsePostfix() *expression {
	value := p.parsePrimary()
	for p.current().kind == tokenDot {
		p.advance()
		name := p.expect(tokenIdentifier, "method name after .")
		args := p.parseArguments()
		value = p.newExpression(&expression{kind: expressionCall, span: joinSpan(value.span, p.previous().span), receiver: value, name: name.text, args: args})
	}
	if p.atWord("do") {
		if value.kind != expressionCall {
			p.problem(p.current().span, "ruby: block must follow a call")
			return value
		}
		value.block = p.parseBlock()
		value.span = joinSpan(value.span, value.block.span)
	}
	return value
}

func (p *parser) parsePrimary() *expression {
	current := p.current()
	switch current.kind {
	case tokenInteger:
		p.advance()
		value, err := strconv.ParseInt(current.text, 10, 64)
		if err != nil {
			p.problem(current.span, "ruby: integer literal is outside int64 proof range")
		}
		return p.newExpression(&expression{kind: expressionInteger, span: current.span, integer: value})
	case tokenString:
		p.advance()
		return p.newExpression(&expression{kind: expressionString, span: current.span, text: strings.Trim(current.text, "\"")})
	case tokenColon:
		start := current.span
		p.advance()
		name := p.expect(tokenIdentifier, "symbol name")
		return p.newExpression(&expression{kind: expressionSymbol, span: joinSpan(start, name.span), text: name.text})
	case tokenInstanceVariable:
		p.advance()
		return p.newExpression(&expression{kind: expressionInstanceVariable, span: current.span, text: current.text})
	case tokenConstant:
		p.advance()
		return p.newExpression(&expression{kind: expressionConstant, span: current.span, text: current.text})
	case tokenIdentifier:
		if current.text == "yield" {
			p.advance()
			args := p.parseArguments()
			if len(args) != 1 {
				p.problem(current.span, "ruby: yield requires one argument in proof subset")
			}
			return p.newExpression(&expression{kind: expressionYield, span: joinSpan(current.span, p.previous().span), args: args})
		}
		p.advance()
		if p.current().kind == tokenLeftParen {
			args := p.parseArguments()
			return p.newExpression(&expression{kind: expressionCall, span: joinSpan(current.span, p.previous().span), name: current.text, args: args})
		}
		return p.newExpression(&expression{kind: expressionLocal, span: current.span, text: current.text})
	case tokenLeftBracket:
		start := current.span
		p.advance()
		var items []*expression
		if p.current().kind != tokenRightBracket {
			for {
				items = append(items, p.parseExpression())
				if p.current().kind != tokenComma {
					break
				}
				p.advance()
			}
		}
		end := p.expect(tokenRightBracket, "]")
		return p.newExpression(&expression{kind: expressionArray, span: joinSpan(start, end.span), items: items})
	default:
		p.problem(current.span, "ruby: expression expected")
		p.advance()
		return p.newExpression(&expression{kind: expressionInteger, span: current.span})
	}
}

func (p *parser) parseArguments() []*expression {
	p.expect(tokenLeftParen, "(")
	var args []*expression
	if p.current().kind != tokenRightParen {
		for {
			args = append(args, p.parseExpression())
			if p.current().kind != tokenComma {
				break
			}
			p.advance()
		}
	}
	p.expect(tokenRightParen, ")")
	return args
}

func (p *parser) parseBlock() *block {
	start := p.current().span
	p.advance()
	var parameters []string
	if p.current().kind == tokenPipe {
		p.advance()
		if p.current().kind != tokenPipe {
			for {
				parameters = append(parameters, p.expect(tokenIdentifier, "block parameter").text)
				if p.current().kind != tokenComma {
					break
				}
				p.advance()
			}
		}
		p.expect(tokenPipe, "closing block |")
	}
	p.expectLineEnd("block header")
	body := p.parseStatements(map[string]bool{"end": true})
	end := p.expectWord("end")
	p.nodes++
	p.checkNodeLimit(end.span)
	return &block{span: joinSpan(start, end.span), parameters: parameters, body: body}
}

func (p *parser) expectLineEnd(context string) {
	if p.current().kind != tokenNewline && p.current().kind != tokenEOF {
		p.problem(p.current().span, "ruby: newline expected after "+context)
		return
	}
	p.skipNewlines()
}

func (p *parser) skipNewlines() {
	for p.current().kind == tokenNewline {
		p.advance()
	}
}

func (p *parser) atWord(word string) bool {
	return p.current().kind == tokenIdentifier && p.current().text == word
}

func (p *parser) expectWord(word string) token {
	current := p.current()
	if !p.atWord(word) {
		p.problem(current.span, fmt.Sprintf("ruby: expected %q", word))
		return current
	}
	p.advance()
	return current
}

func (p *parser) expect(kind tokenKind, wanted string) token {
	current := p.current()
	if current.kind != kind {
		p.problem(current.span, "ruby: expected "+wanted)
		return current
	}
	p.advance()
	return current
}

func (p *parser) current() token {
	if p.index >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.index]
}

func (p *parser) peek(delta int) token {
	index := p.index + delta
	if index >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[index]
}

func (p *parser) previous() token {
	if p.index == 0 {
		return p.tokens[0]
	}
	return p.tokens[p.index-1]
}

func (p *parser) advance() {
	if p.index < len(p.tokens)-1 {
		p.index++
	}
}

func (p *parser) newStatement(value *statement) *statement {
	p.nodes++
	p.checkNodeLimit(value.span)
	return value
}

func (p *parser) newExpression(value *expression) *expression {
	p.nodes++
	p.checkNodeLimit(value.span)
	return value
}

func (p *parser) checkNodeLimit(span Span) {
	if p.nodes > maximumNodes {
		p.problem(span, fmt.Sprintf("ruby: syntax node count exceeds limit %d", maximumNodes))
	}
}

func (p *parser) problem(span Span, message string) {
	if len(p.diagnostics) < maximumDiagnostics {
		p.diagnostics = append(p.diagnostics, Diagnostic{Span: span, Message: message})
	}
}

func joinSpan(left, right Span) Span {
	return Span{Start: left.Start, End: right.End, StartLine: left.StartLine, StartColumn: left.StartColumn, EndLine: right.EndLine, EndColumn: right.EndColumn}
}
