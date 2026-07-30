package sprigproof

import (
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"
)

type tokenKind uint8

const (
	tEOF tokenKind = iota
	tIdent
	tInt
	tPackage
	tImport
	tRecord
	tUnion
	tFunc
	tLet
	tFor
	tIn
	tIf
	tThen
	tElse
	tMatch
	tReturn
	tAppend
	tI64
	tLBrace
	tRBrace
	tLParen
	tRParen
	tLBracket
	tRBracket
	tColon
	tComma
	tDot
	tAssign
	tDeclare
	tArrow
	tPlus
	tMinus
	tSlash
	tEqual
)

type token struct {
	kind tokenKind
	text string
	span Span
}

var keywords = map[string]tokenKind{
	"package": tPackage, "import": tImport, "record": tRecord, "union": tUnion, "func": tFunc,
	"let": tLet, "for": tFor, "in": tIn, "if": tIf, "then": tThen,
	"else": tElse, "match": tMatch, "return": tReturn, "append": tAppend,
	"i64": tI64,
}

func lex(source string) ([]token, []Diagnostic) {
	positions := newSourcePositions(source)
	if len(source) > maximumSourceBytes {
		span := positions.span(0, len(source))
		return nil, []Diagnostic{{span, fmt.Sprintf("source exceeds %d-byte limit", maximumSourceBytes)}}
	}
	var tokens []token
	var diagnostics []Diagnostic
	for pos := 0; pos < len(source); {
		c := source[pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			pos++
			continue
		}
		if c == '/' && pos+1 < len(source) && source[pos+1] == '/' {
			pos += 2
			for pos < len(source) && source[pos] != '\n' {
				pos++
			}
			continue
		}
		start := pos
		if asciiLetter(c) || c == '_' {
			pos++
			for pos < len(source) && (asciiLetter(source[pos]) || asciiDigit(source[pos]) || source[pos] == '_') {
				pos++
			}
			text := source[start:pos]
			kind := tIdent
			if keyword, ok := keywords[text]; ok {
				kind = keyword
			}
			tokens = append(tokens, token{kind, text, positions.span(start, pos)})
		} else if asciiDigit(c) {
			pos++
			for pos < len(source) && asciiDigit(source[pos]) {
				pos++
			}
			tokens = append(tokens, token{tInt, source[start:pos], positions.span(start, pos)})
		} else {
			pos++
			kind := tEOF
			switch c {
			case '{':
				kind = tLBrace
			case '}':
				kind = tRBrace
			case '(':
				kind = tLParen
			case ')':
				kind = tRParen
			case '[':
				kind = tLBracket
			case ']':
				kind = tRBracket
			case ':':
				kind = tColon
			case ',':
				kind = tComma
			case '.':
				kind = tDot
			case '+':
				kind = tPlus
			case '/':
				kind = tSlash
			case '=':
				if pos < len(source) && source[pos] == '=' {
					pos++
					kind = tEqual
				} else {
					kind = tAssign
				}
			case '-':
				if pos < len(source) && source[pos] == '>' {
					pos++
					kind = tArrow
				} else {
					kind = tMinus
				}
			}
			if kind == tEOF {
				_, size := utf8.DecodeRuneInString(source[start:])
				if size > 1 {
					pos = start + size
				}
				diagnostics = append(diagnostics, Diagnostic{positions.span(start, pos), fmt.Sprintf("unexpected character %q", source[start:pos])})
				if len(diagnostics) >= maximumDiagnostics {
					return nil, diagnostics
				}
			} else {
				tokens = append(tokens, token{kind, source[start:pos], positions.span(start, pos)})
			}
		}
		if len(tokens) > maximumTokens {
			diagnostics = append(diagnostics, Diagnostic{positions.span(start, pos), fmt.Sprintf("token count exceeds limit %d", maximumTokens)})
			return nil, diagnostics
		}
	}
	tokens = append(tokens, token{kind: tEOF, span: positions.span(len(source), len(source))})
	return tokens, diagnostics
}

func asciiLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func asciiDigit(c byte) bool  { return c >= '0' && c <= '9' }

type sourcePositions struct{ lineStarts []int }

func newSourcePositions(source string) sourcePositions {
	starts := []int{0}
	for i, c := range []byte(source) {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return sourcePositions{starts}
}
func (p sourcePositions) span(start, end int) Span {
	sl := sort.Search(len(p.lineStarts), func(i int) bool { return p.lineStarts[i] > start })
	el := sort.Search(len(p.lineStarts), func(i int) bool { return p.lineStarts[i] > end })
	return Span{start, end, sl, start - p.lineStarts[sl-1] + 1, el, end - p.lineStarts[el-1] + 1}
}

type moduleAST struct {
	packageName string
	packageSpan Span
	imports     []importDecl
	decls       []decl
}
type importDecl struct {
	alias, packageName string
	span               Span
}
type decl interface{ declNode() }
type recordDecl struct {
	name   string
	span   Span
	fields []fieldDecl
	id     DeclID
}
type unionDecl struct {
	name     string
	span     Span
	variants []variantDecl
	id       DeclID
}
type funcDecl struct {
	name       string
	span       Span
	parameter  fieldDecl
	parameters []fieldDecl
	result     typeRef
	body       []stmt
	id         DeclID
}

func (recordDecl) declNode() {}
func (unionDecl) declNode()  {}
func (funcDecl) declNode()   {}

type fieldDecl struct {
	name    string
	span    Span
	typ     typeRef
	id      FieldID
	binding BindingID
}
type variantDecl struct {
	name   string
	span   Span
	fields []fieldDecl
	id     VariantID
}
type typeRef struct {
	name  string
	slice bool
	span  Span
}
type stmt interface {
	stmtNode()
	statementSpan() Span
}
type letStmt struct {
	name    string
	span    Span
	value   expr
	binding BindingID
}

func (letStmt) stmtNode()             {}
func (s letStmt) statementSpan() Span { return s.span }

type assignStmt struct {
	name    string
	span    Span
	value   expr
	binding BindingID
}

func (assignStmt) stmtNode()             {}
func (s assignStmt) statementSpan() Span { return s.span }

type forStmt struct {
	name, source           string
	span                   Span
	body                   []stmt
	binding, sourceBinding BindingID
}

func (forStmt) stmtNode()             {}
func (s forStmt) statementSpan() Span { return s.span }

type matchStmt struct {
	value expr
	span  Span
	arms  []matchArm
}

func (matchStmt) stmtNode()             {}
func (s matchStmt) statementSpan() Span { return s.span }

type returnStmt struct {
	span  Span
	value expr
}

func (returnStmt) stmtNode()             {}
func (s returnStmt) statementSpan() Span { return s.span }

type matchArm struct {
	variant   string
	span      Span
	binds     []string
	bindings  []BindingID
	variantID VariantID
	body      []stmt
}
type expr interface {
	exprNode()
	expressionSpan() Span
}
type intExpr struct {
	value int64
	span  Span
}

func (intExpr) exprNode()              {}
func (e intExpr) expressionSpan() Span { return e.span }

type sliceExpr struct{ span Span }

func (sliceExpr) exprNode()              {}
func (e sliceExpr) expressionSpan() Span { return e.span }

type nameExpr struct {
	parts    []string
	span     Span
	binding  BindingID
	fieldIDs []FieldID
}

func (nameExpr) exprNode()              {}
func (e nameExpr) expressionSpan() Span { return e.span }

type binaryExpr struct {
	op          tokenKind
	left, right expr
	span        Span
}

func (binaryExpr) exprNode()              {}
func (e binaryExpr) expressionSpan() Span { return e.span }

type ifExpr struct {
	span               Span
	condition, yes, no expr
}

func (ifExpr) exprNode()              {}
func (e ifExpr) expressionSpan() Span { return e.span }

type constructExpr struct {
	name      string
	span      Span
	fields    []namedExpr
	variantID VariantID
}

func (constructExpr) exprNode()              {}
func (e constructExpr) expressionSpan() Span { return e.span }

type appendExpr struct {
	span        Span
	list, value expr
}

type callExpr struct {
	span            Span
	alias, function string
	args            []expr
	call            checkedCall
}

func (callExpr) exprNode()              {}
func (e callExpr) expressionSpan() Span { return e.span }

func (appendExpr) exprNode()              {}
func (e appendExpr) expressionSpan() Span { return e.span }

type namedExpr struct {
	name    string
	span    Span
	value   expr
	fieldID FieldID
}

type parser struct {
	tokens      []token
	at, nodes   int
	diagnostics []Diagnostic
}

func parse(tokens []token) (moduleAST, []Diagnostic) {
	p := parser{tokens: tokens}
	var m moduleAST
	p.expect(tPackage, "expected 'package'")
	n := p.expect(tIdent, "expected package name")
	m.packageName = n.text
	m.packageSpan = n.span
	for p.peek().kind == tImport {
		start := p.take()
		alias := p.expect(tIdent, "expected import alias")
		pkg := p.expect(tIdent, "expected imported package")
		m.imports = append(m.imports, importDecl{alias: alias.text, packageName: pkg.text, span: joinSpan(start.span, pkg.span)})
	}
	for p.peek().kind != tEOF {
		start := p.at
		switch p.peek().kind {
		case tRecord:
			m.decls = append(m.decls, p.parseRecord())
		case tUnion:
			m.decls = append(m.decls, p.parseUnion())
		case tFunc:
			m.decls = append(m.decls, p.parseFunc())
		default:
			p.problem(p.peek().span, "expected record, union, or func declaration")
			p.at++
		}
		if p.at == start {
			p.at++
		}
		p.node(p.peek().span)
	}
	return m, p.diagnostics
}
func (p *parser) parseRecord() recordDecl {
	start := p.take()
	n := p.expect(tIdent, "expected record name")
	d := recordDecl{name: n.text, span: start.span}
	p.expect(tLBrace, "expected '{'")
	for p.peek().kind != tRBrace && p.peek().kind != tEOF {
		d.fields = append(d.fields, p.parseField())
	}
	end := p.expect(tRBrace, "expected '}'")
	d.span.End = end.span.End
	return d
}
func (p *parser) parseUnion() unionDecl {
	start := p.take()
	n := p.expect(tIdent, "expected union name")
	d := unionDecl{name: n.text, span: start.span}
	p.expect(tLBrace, "expected '{'")
	for p.peek().kind != tRBrace && p.peek().kind != tEOF {
		v := p.expect(tIdent, "expected variant name")
		x := variantDecl{name: v.text, span: v.span}
		p.expect(tLBrace, "expected '{'")
		for p.peek().kind != tRBrace && p.peek().kind != tEOF {
			x.fields = append(x.fields, p.parseField())
		}
		p.expect(tRBrace, "expected '}'")
		d.variants = append(d.variants, x)
	}
	end := p.expect(tRBrace, "expected '}'")
	d.span.End = end.span.End
	return d
}
func (p *parser) parseField() fieldDecl {
	n := p.expect(tIdent, "expected field name")
	p.node(n.span)
	return fieldDecl{name: n.text, span: n.span, typ: p.parseType()}
}
func (p *parser) parseType() typeRef {
	start := p.peek().span
	slice := false
	if p.accept(tLBracket) {
		p.expect(tRBracket, "expected ']'")
		slice = true
	}
	n := p.take()
	p.node(n.span)
	if n.kind != tIdent && n.kind != tI64 {
		p.problem(n.span, "expected type")
	}
	return typeRef{n.text, slice, Span{Start: start.Start, End: n.span.End, StartLine: start.StartLine, StartColumn: start.StartColumn, EndLine: n.span.EndLine, EndColumn: n.span.EndColumn}}
}
func (p *parser) parseFunc() funcDecl {
	start := p.take()
	n := p.expect(tIdent, "expected function name")
	d := funcDecl{name: n.text, span: start.span}
	p.expect(tLParen, "expected '('")
	for p.peek().kind != tRParen && p.peek().kind != tEOF {
		d.parameters = append(d.parameters, p.parseField())
		p.accept(tComma)
	}
	if len(d.parameters) != 0 {
		d.parameter = d.parameters[0]
	}
	p.expect(tRParen, "expected ')'")
	d.result = p.parseType()
	d.body = p.parseBlock()
	return d
}
func (p *parser) parseBlock() []stmt {
	p.expect(tLBrace, "expected '{'")
	var out []stmt
	for p.peek().kind != tRBrace && p.peek().kind != tEOF {
		start := p.at
		switch p.peek().kind {
		case tLet:
			out = append(out, p.parseLet())
		case tFor:
			out = append(out, p.parseFor())
		case tMatch:
			out = append(out, p.parseMatch())
		case tReturn:
			out = append(out, p.parseReturn())
		case tIdent:
			out = append(out, p.parseAssign())
		default:
			p.problem(p.peek().span, "expected statement")
			p.at++
		}
		if start == p.at {
			p.at++
		}
		p.node(p.previous().span)
	}
	p.expect(tRBrace, "expected '}'")
	return out
}
func (p *parser) parseLet() stmt {
	start := p.take()
	n := p.expect(tIdent, "expected binding name")
	p.expect(tAssign, "expected '='")
	return letStmt{name: n.text, span: joinSpan(start.span, n.span), value: p.parseExpr(0)}
}
func (p *parser) parseAssign() stmt {
	n := p.take()
	p.expect(tAssign, "expected '='")
	return assignStmt{name: n.text, span: n.span, value: p.parseExpr(0)}
}
func (p *parser) parseFor() stmt {
	start := p.take()
	n := p.expect(tIdent, "expected loop binding")
	p.expect(tIn, "expected 'in'")
	src := p.expect(tIdent, "expected slice binding")
	return forStmt{name: n.text, source: src.text, span: joinSpan(start.span, src.span), body: p.parseBlock()}
}
func (p *parser) parseMatch() stmt {
	start := p.take()
	value := p.expect(tIdent, "expected union binding")
	s := matchStmt{span: start.span, value: nameExpr{parts: []string{value.text}, span: value.span}}
	p.expect(tLBrace, "expected '{'")
	for p.peek().kind != tRBrace && p.peek().kind != tEOF {
		v := p.expect(tIdent, "expected variant")
		a := matchArm{variant: v.text, span: v.span}
		p.expect(tLBrace, "expected '{'")
		for p.peek().kind != tRBrace && p.peek().kind != tEOF {
			a.binds = append(a.binds, p.expect(tIdent, "expected field binding").text)
		}
		p.expect(tRBrace, "expected '}'")
		p.expect(tArrow, "expected '->'")
		a.body = p.parseBlock()
		s.arms = append(s.arms, a)
	}
	p.expect(tRBrace, "expected '}'")
	return s
}
func (p *parser) parseReturn() stmt { start := p.take(); return returnStmt{start.span, p.parseExpr(0)} }
func (p *parser) parseExpr(min int) expr {
	left := p.parsePrimary()
	for {
		prec := precedence(p.peek().kind)
		if prec < min {
			break
		}
		op := p.take()
		right := p.parseExpr(prec + 1)
		left = binaryExpr{op.kind, left, right, joinSpan(left.expressionSpan(), right.expressionSpan())}
		p.node(op.span)
	}
	return left
}
func (p *parser) parsePrimary() expr {
	t := p.take()
	p.node(t.span)
	switch t.kind {
	case tMinus:
		n := p.expect(tInt, "expected integer after '-'")
		v, err := strconv.ParseUint(n.text, 10, 64)
		if err != nil || v > 1<<63 {
			p.problem(n.span, "integer literal out of i64 range")
			return intExpr{0, joinSpan(t.span, n.span)}
		}
		if v == 1<<63 {
			return intExpr{-1 << 63, joinSpan(t.span, n.span)}
		}
		return intExpr{-int64(v), joinSpan(t.span, n.span)}
	case tInt:
		v, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			p.problem(t.span, "integer literal out of i64 range")
		}
		return intExpr{v, t.span}
	case tLBracket:
		p.expect(tRBracket, "expected ']'")
		return sliceExpr{joinSpan(t.span, p.previous().span)}
	case tIf:
		cond := p.parseExpr(0)
		p.expect(tThen, "expected 'then'")
		yes := p.parseExpr(0)
		p.expect(tElse, "expected 'else'")
		no := p.parseExpr(0)
		return ifExpr{joinSpan(t.span, no.expressionSpan()), cond, yes, no}
	case tAppend:
		p.expect(tLParen, "expected '('")
		a := p.parseExpr(0)
		p.expect(tComma, "expected ','")
		b := p.parseExpr(0)
		p.expect(tRParen, "expected ')'")
		return appendExpr{joinSpan(t.span, b.expressionSpan()), a, b}
	case tIdent:
		parts := []string{t.text}
		for p.accept(tDot) {
			parts = append(parts, p.expect(tIdent, "expected field name").text)
		}
		if p.accept(tLParen) {
			if len(parts) != 2 {
				p.problem(t.span, "calls require an explicit import alias")
			}
			call := callExpr{alias: parts[0], function: parts[len(parts)-1], span: t.span}
			for p.peek().kind != tRParen && p.peek().kind != tEOF {
				call.args = append(call.args, p.parseExpr(0))
				if !p.accept(tComma) {
					break
				}
			}
			end := p.expect(tRParen, "expected ')'")
			call.span = joinSpan(t.span, end.span)
			return call
		}
		if p.accept(tLBrace) {
			c := constructExpr{name: t.text, span: t.span}
			for p.peek().kind != tRBrace && p.peek().kind != tEOF {
				n := p.expect(tIdent, "expected field name")
				p.expect(tColon, "expected ':'")
				c.fields = append(c.fields, namedExpr{name: n.text, span: n.span, value: p.parseExpr(0)})
				p.accept(tComma)
			}
			end := p.expect(tRBrace, "expected '}'")
			c.span = joinSpan(t.span, end.span)
			return c
		}
		return nameExpr{parts: parts, span: joinSpan(t.span, p.previous().span)}
	case tLParen:
		e := p.parseExpr(0)
		p.expect(tRParen, "expected ')'")
		return e
	default:
		p.problem(t.span, "expected expression")
		return intExpr{0, t.span}
	}
}
func precedence(k tokenKind) int {
	switch k {
	case tEqual:
		return 1
	case tPlus:
		return 2
	case tSlash:
		return 3
	}
	return -1
}
func (p *parser) peek() token {
	if p.at >= len(p.tokens) {
		return token{kind: tEOF}
	}
	return p.tokens[p.at]
}
func (p *parser) take() token {
	t := p.peek()
	if p.at < len(p.tokens) {
		p.at++
	}
	return t
}
func (p *parser) previous() token {
	if p.at == 0 {
		return token{}
	}
	return p.tokens[p.at-1]
}
func (p *parser) accept(k tokenKind) bool {
	if p.peek().kind == k {
		p.at++
		return true
	}
	return false
}
func (p *parser) expect(k tokenKind, msg string) token {
	if p.peek().kind == k {
		return p.take()
	}
	t := p.peek()
	p.problem(t.span, msg)
	if t.kind != tEOF {
		p.at++
	}
	return t
}
func (p *parser) problem(s Span, msg string) {
	if len(p.diagnostics) < maximumDiagnostics {
		p.diagnostics = append(p.diagnostics, Diagnostic{s, msg})
	}
}
func (p *parser) node(span Span) {
	p.nodes++
	if p.nodes == maximumNodes+1 {
		p.problem(span, fmt.Sprintf("syntax node count exceeds limit %d", maximumNodes))
	}
}
func joinSpan(a, b Span) Span {
	return Span{a.Start, b.End, a.StartLine, a.StartColumn, b.EndLine, b.EndColumn}
}
