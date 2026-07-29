// Package seedcompiler is a deliberately small, independently versioned
// compiler fixture. It owns its source language, semantic model, evaluator,
// diagnostics, and Go emission. Its only Ember dependency is the immutable
// preparedsource result value.
package seedcompiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/format"
	gotoken "go/token"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/besmpl/ember/preparedsource"
)

const (
	maximumSourceBytes = 8 << 10
	maximumTokens      = 512
	maximumNodes       = 256
	maximumDiagnostics = 32
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

// DomainCode is a language-level result, not a Go execution error.
type DomainCode uint8

const (
	DomainOverflow DomainCode = iota + 1
	DomainDivideByZero
)

// Result is the detached return-by-value result of Reduce.
type Result struct {
	Value int64
	Code  DomainCode
}

// ErrStepLimit reports exhaustion of the explicit per-call execution budget.
var ErrStepLimit = errors.New("seed: step limit exceeded")

// Program is one immutable checked Seed program. Its private tree is the
// language-owned semantic representation, not a shared compiler IR.
type Program struct{ checked *checkedProgram }

func (p Program) IsZero() bool { return p.checked == nil }

// Identity returns the exact source identity, or zero for a zero Program.
func (p Program) Identity() [sha256.Size]byte {
	if p.checked == nil {
		return [sha256.Size]byte{}
	}
	return p.checked.identity
}

// PackageName returns the checked source package name.
func (p Program) PackageName() string {
	if p.checked == nil {
		return ""
	}
	return p.checked.packageName
}

// Compile lexes, parses, resolves, and checks one bounded Seed source file.
// Diagnostics are deterministic and returned in source order.
func Compile(source string) (Program, []Diagnostic) {
	if len(source) > maximumSourceBytes {
		return Program{}, []Diagnostic{{Message: fmt.Sprintf("seed: source bytes %d exceed limit %d", len(source), maximumSourceBytes)}}
	}
	tokens, diagnostics := lex(source)
	if len(diagnostics) != 0 {
		return Program{}, diagnostics
	}
	raw, diagnostics := parse(tokens)
	if len(diagnostics) != 0 {
		return Program{}, diagnostics
	}
	checked, diagnostics := check(raw, source)
	if len(diagnostics) != 0 {
		return Program{}, diagnostics
	}
	return Program{checked: checked}, nil
}

// Evaluate executes the checked expression canonically. Cancellation is
// observed before the step budget and domain failures remain Result values.
func (p Program) Evaluate(ctx context.Context, value, divisor int64, stepLimit uint64) (Result, error) {
	if p.checked == nil {
		return Result{}, errors.New("seed: zero program")
	}
	if ctx == nil {
		return Result{}, errors.New("seed: nil context")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if stepLimit == 0 {
		return Result{}, ErrStepLimit
	}
	return evaluateExpression(p.checked.expression, [3]int64{0, value, divisor}), nil
}

// PreparedSource emits direct typed Go and returns one location-neutral Set.
// The compiler performs no filesystem, toolchain, cache, or registration
// effect and chooses no application mount path.
func (p Program) PreparedSource() (preparedsource.Set, error) {
	if p.checked == nil {
		return preparedsource.Set{}, errors.New("seed: zero program")
	}
	source, err := emitGo(p.checked)
	if err != nil {
		return preparedsource.Set{}, err
	}
	return preparedsource.NewSet(p.checked.packageName, []preparedsource.File{{
		Name:    "reduce_generated.go",
		Kind:    preparedsource.GoFile,
		Content: source,
	}})
}

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenInteger
	tokenLeftParen
	tokenRightParen
	tokenLeftBrace
	tokenRightBrace
	tokenComma
	tokenPlus
	tokenSlash
)

type lexeme struct {
	kind tokenKind
	text string
	span Span
}

type position struct {
	offset, line, column int
}

type lexer struct {
	source               string
	offset, line, column int
	tokens               []lexeme
	diagnostics          []Diagnostic
}

func lex(source string) ([]lexeme, []Diagnostic) {
	l := lexer{source: source, line: 1, column: 1}
	for l.offset < len(source) && len(l.diagnostics) == 0 {
		if l.skipSpaceOrComment() {
			continue
		}
		start := l.position()
		character := l.peek()
		switch {
		case asciiLetter(character) || character == '_':
			l.advance()
			for asciiLetter(l.peek()) || asciiDigit(l.peek()) || l.peek() == '_' {
				l.advance()
			}
			l.add(tokenIdentifier, start)
		case asciiDigit(character):
			l.advance()
			for asciiDigit(l.peek()) {
				l.advance()
			}
			l.add(tokenInteger, start)
		default:
			l.advance()
			var kind tokenKind
			switch character {
			case '(':
				kind = tokenLeftParen
			case ')':
				kind = tokenRightParen
			case '{':
				kind = tokenLeftBrace
			case '}':
				kind = tokenRightBrace
			case ',':
				kind = tokenComma
			case '+':
				kind = tokenPlus
			case '/':
				kind = tokenSlash
			default:
				l.problem(l.span(start), fmt.Sprintf("seed: invalid character %q", character))
				continue
			}
			l.add(kind, start)
		}
		if len(l.tokens) > maximumTokens {
			l.problem(l.span(start), fmt.Sprintf("seed: token count exceeds limit %d", maximumTokens))
		}
	}
	end := l.position()
	l.tokens = append(l.tokens, lexeme{kind: tokenEOF, span: l.span(end)})
	return l.tokens, sortDiagnostics(l.diagnostics)
}

func (l *lexer) skipSpaceOrComment() bool {
	if strings.ContainsRune(" \t\r\n", rune(l.peek())) {
		l.advance()
		return true
	}
	if l.peek() == '/' && l.peekAt(1) == '/' {
		for l.offset < len(l.source) && l.peek() != '\n' {
			l.advance()
		}
		return true
	}
	return false
}

func (l *lexer) peek() byte { return l.peekAt(0) }

func (l *lexer) peekAt(delta int) byte {
	if l.offset+delta >= len(l.source) {
		return 0
	}
	return l.source[l.offset+delta]
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

func (l *lexer) position() position {
	return position{offset: l.offset, line: l.line, column: l.column}
}

func (l *lexer) span(start position) Span {
	return Span{Start: start.offset, End: l.offset, StartLine: start.line, StartColumn: start.column, EndLine: l.line, EndColumn: l.column}
}

func (l *lexer) add(kind tokenKind, start position) {
	l.tokens = append(l.tokens, lexeme{kind: kind, text: l.source[start.offset:l.offset], span: l.span(start)})
}

func (l *lexer) problem(span Span, message string) {
	if len(l.diagnostics) < maximumDiagnostics {
		l.diagnostics = append(l.diagnostics, Diagnostic{Span: span, Message: message})
	}
}

func asciiLetter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func asciiDigit(character byte) bool { return character >= '0' && character <= '9' }

type rawProgram struct {
	packageName, functionName string
	packageSpan, functionSpan Span
	parameters                []rawParameter
	resultType                string
	resultSpan                Span
	expression                *rawExpression
}

type rawParameter struct {
	name, typ      string
	nameSpan, span Span
}

type expressionKind uint8

const (
	expressionInteger expressionKind = iota + 1
	expressionName
	expressionAdd
	expressionDivide
)

type rawExpression struct {
	kind        expressionKind
	span        Span
	text        string
	left, right *rawExpression
}

type parser struct {
	tokens      []lexeme
	at, nodes   int
	diagnostics []Diagnostic
}

func parse(tokens []lexeme) (rawProgram, []Diagnostic) {
	p := parser{tokens: tokens}
	var out rawProgram
	p.expectWord("package")
	packageName := p.expect(tokenIdentifier, "package name")
	out.packageName, out.packageSpan = packageName.text, packageName.span
	p.expectWord("func")
	functionName := p.expect(tokenIdentifier, "function name")
	out.functionName, out.functionSpan = functionName.text, functionName.span
	p.expect(tokenLeftParen, "'('")
	if p.current().kind != tokenRightParen {
		for {
			name := p.expect(tokenIdentifier, "parameter name")
			typ := p.expect(tokenIdentifier, "parameter type")
			out.parameters = append(out.parameters, rawParameter{name: name.text, typ: typ.text, nameSpan: name.span, span: joinSpan(name.span, typ.span)})
			if p.current().kind != tokenComma {
				break
			}
			p.advance()
		}
	}
	p.expect(tokenRightParen, "')'")
	result := p.expect(tokenIdentifier, "result type")
	out.resultType, out.resultSpan = result.text, result.span
	p.expect(tokenLeftBrace, "'{'")
	p.expectWord("return")
	out.expression = p.parseAdd()
	p.expect(tokenRightBrace, "'}'")
	p.expect(tokenEOF, "end of file")
	return out, sortDiagnostics(p.diagnostics)
}

func (p *parser) parseAdd() *rawExpression {
	left := p.parseDivide()
	for p.current().kind == tokenPlus {
		p.advance()
		right := p.parseDivide()
		left = p.node(expressionAdd, joinSpan(left.span, right.span), "", left, right)
	}
	return left
}

func (p *parser) parseDivide() *rawExpression {
	left := p.parsePrimary()
	for p.current().kind == tokenSlash {
		p.advance()
		right := p.parsePrimary()
		left = p.node(expressionDivide, joinSpan(left.span, right.span), "", left, right)
	}
	return left
}

func (p *parser) parsePrimary() *rawExpression {
	token := p.current()
	switch token.kind {
	case tokenInteger:
		p.advance()
		return p.node(expressionInteger, token.span, token.text, nil, nil)
	case tokenIdentifier:
		p.advance()
		return p.node(expressionName, token.span, token.text, nil, nil)
	case tokenLeftParen:
		p.advance()
		expression := p.parseAdd()
		end := p.expect(tokenRightParen, "')'")
		expression.span = joinSpan(token.span, end.span)
		return expression
	default:
		p.problem(token.span, fmt.Sprintf("seed: expected expression, found %q", token.text))
		p.advance()
		return p.node(expressionInteger, token.span, "0", nil, nil)
	}
}

func (p *parser) node(kind expressionKind, span Span, text string, left, right *rawExpression) *rawExpression {
	p.nodes++
	if p.nodes > maximumNodes {
		p.problem(span, fmt.Sprintf("seed: expression nodes exceed limit %d", maximumNodes))
	}
	return &rawExpression{kind: kind, span: span, text: text, left: left, right: right}
}

func (p *parser) expectWord(word string) lexeme {
	token := p.expect(tokenIdentifier, fmt.Sprintf("%q", word))
	if token.text != word {
		p.problem(token.span, fmt.Sprintf("seed: expected %q, found %q", word, token.text))
	}
	return token
}

func (p *parser) expect(kind tokenKind, wanted string) lexeme {
	token := p.current()
	if token.kind != kind {
		p.problem(token.span, fmt.Sprintf("seed: expected %s, found %q", wanted, token.text))
		if token.kind != tokenEOF {
			p.advance()
		}
		return token
	}
	p.advance()
	return token
}

func (p *parser) current() lexeme {
	if p.at >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.at]
}

func (p *parser) advance() {
	if p.at < len(p.tokens)-1 {
		p.at++
	}
}

func (p *parser) problem(span Span, message string) {
	if len(p.diagnostics) < maximumDiagnostics {
		p.diagnostics = append(p.diagnostics, Diagnostic{Span: span, Message: message})
	}
}

type bindingID uint8

type checkedExpression struct {
	kind        expressionKind
	value       int64
	binding     bindingID
	left, right *checkedExpression
}

type checkedProgram struct {
	packageName string
	identity    [sha256.Size]byte
	expression  *checkedExpression
}

func check(raw rawProgram, source string) (*checkedProgram, []Diagnostic) {
	var diagnostics []Diagnostic
	problem := func(span Span, message string) {
		if len(diagnostics) < maximumDiagnostics {
			diagnostics = append(diagnostics, Diagnostic{Span: span, Message: message})
		}
	}
	if !validPackageName(raw.packageName) {
		problem(raw.packageSpan, fmt.Sprintf("seed: invalid package name %q", raw.packageName))
	}
	if raw.functionName != "Reduce" {
		problem(raw.functionSpan, fmt.Sprintf("seed: entry function must be Reduce, found %q", raw.functionName))
	}
	if len(raw.parameters) != 2 {
		problem(raw.functionSpan, fmt.Sprintf("seed: Reduce has %d parameters, want 2", len(raw.parameters)))
	}
	bindings := make(map[string]bindingID, len(raw.parameters))
	for index, parameter := range raw.parameters {
		if !validSourceIdentifier(parameter.name) {
			problem(parameter.nameSpan, fmt.Sprintf("seed: invalid parameter name %q", parameter.name))
		}
		if parameter.typ != "i64" {
			problem(parameter.span, fmt.Sprintf("seed: parameter %q has type %q, want i64", parameter.name, parameter.typ))
		}
		if _, exists := bindings[parameter.name]; exists {
			problem(parameter.nameSpan, fmt.Sprintf("seed: duplicate parameter %q", parameter.name))
			continue
		}
		if index < 2 {
			bindings[parameter.name] = bindingID(index + 1)
		}
	}
	if raw.resultType != "i64" {
		problem(raw.resultSpan, fmt.Sprintf("seed: result type is %q, want i64", raw.resultType))
	}
	var checkExpression func(*rawExpression) *checkedExpression
	checkExpression = func(expression *rawExpression) *checkedExpression {
		if expression == nil {
			return &checkedExpression{kind: expressionInteger}
		}
		checked := &checkedExpression{kind: expression.kind}
		switch expression.kind {
		case expressionInteger:
			value, err := strconv.ParseInt(expression.text, 10, 64)
			if err != nil {
				problem(expression.span, fmt.Sprintf("seed: integer %q is outside i64", expression.text))
			} else {
				checked.value = value
			}
		case expressionName:
			binding, exists := bindings[expression.text]
			if !exists {
				problem(expression.span, fmt.Sprintf("seed: unknown binding %q", expression.text))
			} else {
				checked.binding = binding
			}
		case expressionAdd, expressionDivide:
			checked.left = checkExpression(expression.left)
			checked.right = checkExpression(expression.right)
		}
		return checked
	}
	expression := checkExpression(raw.expression)
	diagnostics = sortDiagnostics(diagnostics)
	if len(diagnostics) != 0 {
		return nil, diagnostics
	}
	hash := sha256.New()
	hash.Write([]byte("example.com/ember-external-compiler:seed-source:v1\x00"))
	hash.Write([]byte(source))
	var identity [sha256.Size]byte
	copy(identity[:], hash.Sum(nil))
	return &checkedProgram{packageName: raw.packageName, identity: identity, expression: expression}, nil
}

func validSourceIdentifier(name string) bool {
	if name == "" || !(asciiLetter(name[0]) || name[0] == '_') {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !(asciiLetter(name[index]) || asciiDigit(name[index]) || name[index] == '_') {
			return false
		}
	}
	switch name {
	case "package", "func", "return", "i64":
		return false
	default:
		return true
	}
}

func validPackageName(name string) bool {
	if name == "" || name == "main" || name[0] < 'a' || name[0] > 'z' || gotoken.Lookup(name).IsKeyword() {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_') {
			return false
		}
	}
	return len(name) <= 64
}

func evaluateExpression(expression *checkedExpression, bindings [3]int64) Result {
	switch expression.kind {
	case expressionInteger:
		return Result{Value: expression.value}
	case expressionName:
		return Result{Value: bindings[expression.binding]}
	case expressionAdd:
		left := evaluateExpression(expression.left, bindings)
		if left.Code != 0 {
			return left
		}
		right := evaluateExpression(expression.right, bindings)
		if right.Code != 0 {
			return right
		}
		value, overflow := checkedAdd(left.Value, right.Value)
		if overflow {
			return Result{Code: DomainOverflow}
		}
		return Result{Value: value}
	case expressionDivide:
		left := evaluateExpression(expression.left, bindings)
		if left.Code != 0 {
			return left
		}
		right := evaluateExpression(expression.right, bindings)
		if right.Code != 0 {
			return right
		}
		if right.Value == 0 {
			return Result{Code: DomainDivideByZero}
		}
		if left.Value == math.MinInt64 && right.Value == -1 {
			return Result{Code: DomainOverflow}
		}
		return Result{Value: left.Value / right.Value}
	default:
		panic("checked Seed expression kind")
	}
}

func checkedAdd(left, right int64) (int64, bool) {
	sum := left + right
	return sum, right > 0 && sum < left || right < 0 && sum > left
}

type goEmitter struct {
	out    bytes.Buffer
	indent int
	temp   int
}

func emitGo(program *checkedProgram) (string, error) {
	var emitter goEmitter
	emitter.line("// Code generated by example.com/ember-external-compiler. DO NOT EDIT.")
	emitter.line("// Seed source identity: %x", program.identity)
	emitter.line("package %s", program.packageName)
	emitter.line("")
	emitter.line("import (\n\t\"context\"\n\t\"errors\"\n\t\"math\"\n)")
	emitter.line("")
	emitter.line("type DomainCode uint8")
	emitter.line("const ( DomainOverflow DomainCode = iota + 1; DomainDivideByZero )")
	emitter.line("type Result struct { Value int64; Code DomainCode }")
	emitter.line("var ErrStepLimit = errors.New(%q)", ErrStepLimit.Error())
	emitter.line("func seedCheckedAdd(left,right int64)(int64,bool){ sum:=left+right; return sum,(right>0&&sum<left)||(right<0&&sum>left) }")
	emitter.line("")
	emitter.line("func Reduce(seedCtx context.Context, seedB1, seedB2 int64, seedLimit uint64) (Result, error) {")
	emitter.indent++
	emitter.line("if seedCtx == nil { return Result{}, errors.New(\"seed: nil context\") }")
	emitter.line("if err := seedCtx.Err(); err != nil { return Result{}, err }")
	emitter.line("if seedLimit == 0 { return Result{}, ErrStepLimit }")
	emitter.line("_ = math.MinInt64")
	value := emitter.expression(program.expression)
	emitter.line("return Result{Value: %s}, nil", value)
	emitter.indent--
	emitter.line("}")
	formatted, err := format.Source(emitter.out.Bytes())
	if err != nil {
		return "", fmt.Errorf("seed: format generated Go: %w\n%s", err, emitter.out.String())
	}
	return string(formatted), nil
}

func (e *goEmitter) expression(expression *checkedExpression) string {
	switch expression.kind {
	case expressionInteger:
		return "int64(" + strconv.FormatInt(expression.value, 10) + ")"
	case expressionName:
		return fmt.Sprintf("seedB%d", expression.binding)
	case expressionAdd:
		left := e.expression(expression.left)
		right := e.expression(expression.right)
		value := e.next("Sum")
		overflow := e.next("Overflow")
		e.line("%s, %s := seedCheckedAdd(%s, %s)", value, overflow, left, right)
		e.line("if %s { return Result{Code: DomainOverflow}, nil }", overflow)
		return value
	case expressionDivide:
		left := e.expression(expression.left)
		right := e.expression(expression.right)
		divisor := e.next("Divisor")
		e.line("%s := %s", divisor, right)
		e.line("if %s == 0 { return Result{Code: DomainDivideByZero}, nil }", divisor)
		e.line("if %s == math.MinInt64 && %s == -1 { return Result{Code: DomainOverflow}, nil }", left, divisor)
		value := e.next("Quotient")
		e.line("%s := %s / %s", value, left, divisor)
		return value
	default:
		panic("checked Seed expression kind")
	}
}

func (e *goEmitter) line(format string, arguments ...any) {
	e.out.WriteString(strings.Repeat("\t", e.indent))
	fmt.Fprintf(&e.out, format, arguments...)
	e.out.WriteByte('\n')
}

func (e *goEmitter) next(kind string) string {
	e.temp++
	return fmt.Sprintf("seed%s%d", kind, e.temp)
}

func joinSpan(left, right Span) Span {
	return Span{Start: left.Start, End: right.End, StartLine: left.StartLine, StartColumn: left.StartColumn, EndLine: right.EndLine, EndColumn: right.EndColumn}
}

func sortDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := append([]Diagnostic(nil), diagnostics...)
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Span.Start != out[right].Span.Start {
			return out[left].Span.Start < out[right].Span.Start
		}
		if out[left].Span.End != out[right].Span.End {
			return out[left].Span.End < out[right].Span.End
		}
		return out[left].Message < out[right].Message
	})
	return out
}
