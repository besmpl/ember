package ember

import (
	"fmt"
	"strconv"
	"strings"
)

// Compile compiles a tiny supported Luau source category into Ember bytecode.
//
// This seed compiler currently accepts scalar literals, array and named-field
// table literals, local bindings, assignments to existing locals or table
// indexes, if statements, host function calls, and expressions containing
// number literals or local names joined by +.
func Compile(source string) (*Proto, error) {
	p := parser{source: source}
	prog, err := p.parse()
	if err != nil {
		return nil, err
	}

	return compileProgram(prog)
}

type program struct {
	statements []statement
}

type statement struct {
	local  *localStatement
	assign *assignStatement
	ifStmt *ifStatement
	ret    *returnStatement
}

type localStatement struct {
	name  string
	value expression
}

type assignStatement struct {
	target assignTarget
	value  expression
}

type assignTarget struct {
	name  string
	field string
	index *expression
}

type ifStatement struct {
	condition      expression
	thenStatements []statement
	elseStatements []statement
}

type returnStatement struct {
	value expression
}

type expression struct {
	terms []term
}

type term struct {
	number *float64
	lit    *Value
	table  *tableExpression
	call   *callExpression
	field  *fieldExpression
	index  *indexExpression
	name   string
}

type tableExpression struct {
	fields []tableField
}

type tableField struct {
	name       string
	arrayIndex int
	value      expression
}

type callExpression struct {
	name string
	args []expression
}

type fieldExpression struct {
	table string
	name  string
}

type indexExpression struct {
	table string
	index expression
}

type parser struct {
	source string
	pos    int
}

func (p *parser) parse() (program, error) {
	statements, err := p.parseBlock()
	if err != nil {
		return program{}, err
	}

	p.skipSpace()
	if !p.done() {
		return program{}, p.errorf("unexpected input %q", p.source[p.pos:])
	}
	if !statementsHaveReturn(statements) {
		return program{}, p.errorf("expected return statement")
	}

	return program{statements: statements}, nil
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
	if p.consumeKeyword("local") {
		stmt, err := p.parseLocalStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{local: &stmt}, nil
	}

	if p.consumeKeyword("return") {
		stmt, err := p.parseReturnStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{ret: &stmt}, nil
	}

	if p.consumeKeyword("if") {
		stmt, err := p.parseIfStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{ifStmt: &stmt}, nil
	}

	if !p.done() && isIdentStartByte(p.source[p.pos]) {
		stmt, err := p.parseAssignStatement()
		if err != nil {
			return statement{}, err
		}
		return statement{assign: &stmt}, nil
	}

	return statement{}, p.errorf("expected statement")
}

func (p *parser) parseReturnStatement() (returnStatement, error) {
	p.skipSpace()
	value, err := p.parseExpression()
	if err != nil {
		return returnStatement{}, err
	}
	return returnStatement{value: value}, nil
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

	thenStatements, err := p.parseBlock("else", "end")
	if err != nil {
		return ifStatement{}, err
	}

	var elseStatements []statement
	p.skipSpace()
	if p.consumeKeyword("else") {
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

func (p *parser) parseLocalStatement() (localStatement, error) {
	p.skipSpace()
	name, err := p.parseIdentifier()
	if err != nil {
		return localStatement{}, err
	}

	p.skipSpace()
	if !p.consumeByte('=') {
		return localStatement{}, p.errorf("expected =")
	}

	p.skipSpace()
	value, err := p.parseExpression()
	if err != nil {
		return localStatement{}, err
	}

	return localStatement{name: name, value: value}, nil
}

func (p *parser) parseAssignStatement() (assignStatement, error) {
	name, err := p.parseIdentifier()
	if err != nil {
		return assignStatement{}, err
	}

	target := assignTarget{name: name}

	p.skipSpace()
	if p.consumeByte('.') {
		field, err := p.parseIdentifier()
		if err != nil {
			return assignStatement{}, err
		}
		target.field = field
		p.skipSpace()
	}
	if p.consumeByte('[') {
		index, err := p.parseExpression()
		if err != nil {
			return assignStatement{}, err
		}
		p.skipSpace()
		if !p.consumeByte(']') {
			return assignStatement{}, p.errorf("expected ]")
		}
		target.index = &index
		p.skipSpace()
	}

	if !p.consumeByte('=') {
		return assignStatement{}, p.errorf("expected =")
	}

	value, err := p.parseExpression()
	if err != nil {
		return assignStatement{}, err
	}

	return assignStatement{target: target, value: value}, nil
}

func (p *parser) parseExpression() (expression, error) {
	p.skipSpace()
	first, err := p.parseTerm()
	if err != nil {
		return expression{}, err
	}

	expr := expression{terms: []term{first}}

	for {
		p.skipSpace()
		if !p.consumeByte('+') {
			break
		}

		p.skipSpace()
		next, err := p.parseTerm()
		if err != nil {
			return expression{}, err
		}
		expr.terms = append(expr.terms, next)
	}

	return expr, nil
}

func (p *parser) parseTerm() (term, error) {
	p.skipSpace()
	if p.consumeKeyword("nil") {
		v := NilValue()
		return term{lit: &v}, nil
	}
	if p.consumeKeyword("true") {
		v := BoolValue(true)
		return term{lit: &v}, nil
	}
	if p.consumeKeyword("false") {
		v := BoolValue(false)
		return term{lit: &v}, nil
	}
	if !p.done() && p.source[p.pos] == '"' {
		s, err := p.parseString()
		if err != nil {
			return term{}, err
		}
		v := StringValue(s)
		return term{lit: &v}, nil
	}
	if !p.done() && p.source[p.pos] == '{' {
		table, err := p.parseTable()
		if err != nil {
			return term{}, err
		}
		return term{table: &table}, nil
	}
	if !p.done() && isNumberStart(p.source[p.pos]) {
		n, err := p.parseNumber()
		if err != nil {
			return term{}, err
		}
		return term{number: &n}, nil
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return term{}, err
	}
	p.skipSpace()
	if p.consumeByte('(') {
		args, err := p.parseArguments()
		if err != nil {
			return term{}, err
		}
		return term{call: &callExpression{name: name, args: args}}, nil
	}
	if p.consumeByte('.') {
		field, err := p.parseIdentifier()
		if err != nil {
			return term{}, err
		}
		return term{field: &fieldExpression{table: name, name: field}}, nil
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
		return term{index: &indexExpression{table: name, index: index}}, nil
	}
	return term{name: name}, nil
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
		if !p.done() && isIdentStartByte(p.source[p.pos]) {
			start := p.pos
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
			p.pos = start
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
	if !p.consumeByte('"') {
		return "", p.errorf("expected string")
	}

	var b strings.Builder
	for !p.done() {
		ch := p.source[p.pos]
		p.pos++

		switch ch {
		case '"':
			return b.String(), nil
		case '\\':
			if p.done() {
				return "", p.errorf("unterminated string")
			}
			escaped := p.source[p.pos]
			p.pos++
			switch escaped {
			case '\\', '"':
				b.WriteByte(escaped)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return "", p.errorf("unsupported string escape \\%c", escaped)
			}
		case '\n', '\r':
			return "", p.errorf("unterminated string")
		default:
			b.WriteByte(ch)
		}
	}

	return "", p.errorf("unterminated string")
}

func (p *parser) parseNumber() (float64, error) {
	start := p.pos
	for !p.done() {
		ch := p.source[p.pos]
		if (ch < '0' || ch > '9') && ch != '.' {
			break
		}
		p.pos++
	}
	if start == p.pos {
		return 0, p.errorf("expected number")
	}

	n, err := strconv.ParseFloat(p.source[start:p.pos], 64)
	if err != nil {
		return 0, p.errorf("invalid number %q", p.source[start:p.pos])
	}
	return n, nil
}

func (p *parser) parseIdentifier() (string, error) {
	if p.done() || !isIdentStartByte(p.source[p.pos]) {
		return "", p.errorf("expected identifier")
	}

	start := p.pos
	p.pos++
	for !p.done() && isIdentByte(p.source[p.pos]) {
		p.pos++
	}

	name := p.source[start:p.pos]
	if isKeyword(name) {
		return "", p.errorf("expected identifier")
	}
	return name, nil
}

func (p *parser) consumeKeyword(keyword string) bool {
	if !p.matchKeyword(keyword) {
		return false
	}

	p.pos += len(keyword)
	return true
}

func (p *parser) matchKeyword(keyword string) bool {
	if !strings.HasPrefix(p.source[p.pos:], keyword) {
		return false
	}

	end := p.pos + len(keyword)
	return end >= len(p.source) || !isIdentByte(p.source[end])
}

func (p *parser) consumeByte(ch byte) bool {
	if p.done() || p.source[p.pos] != ch {
		return false
	}
	p.pos++
	return true
}

func (p *parser) skipSpace() {
	for !p.done() {
		switch p.source[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) done() bool {
	return p.pos >= len(p.source)
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("compile: byte %d: %s", p.pos, fmt.Sprintf(format, args...))
}

func isNumberStart(ch byte) bool {
	return (ch >= '0' && ch <= '9') || ch == '.'
}

func isIdentStartByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		ch == '_'
}

func isIdentByte(ch byte) bool {
	return isIdentStartByte(ch) ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}

func isKeyword(name string) bool {
	switch name {
	case "else", "end", "false", "if", "local", "nil", "return", "then", "true":
		return true
	default:
		return false
	}
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
	}
	return false
}

type compiler struct {
	constants []Value
	code      []instruction
	locals    map[string]int
	nextReg   int
}

func compileProgram(prog program) (*Proto, error) {
	c := compiler{
		locals: make(map[string]int),
	}

	if err := c.compileStatements(prog.statements); err != nil {
		return nil, err
	}

	return newProto(c.constants, c.code, c.nextReg), nil
}

func (c *compiler) compileStatements(statements []statement) error {
	for _, stmt := range statements {
		if err := c.compileStatement(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileStatement(stmt statement) error {
	if stmt.local != nil {
		target := c.allocReg()
		if err := c.compileExpressionTo(stmt.local.value, target); err != nil {
			return err
		}
		c.locals[stmt.local.name] = target
		return nil
	}

	if stmt.assign != nil {
		return c.compileAssignment(*stmt.assign)
	}

	if stmt.ifStmt != nil {
		return c.compileIf(*stmt.ifStmt)
	}

	if stmt.ret != nil {
		result, err := c.compileExpression(stmt.ret.value)
		if err != nil {
			return err
		}
		c.code = append(c.code, instruction{op: opReturn, a: result})
		return nil
	}

	return fmt.Errorf("compile: empty statement")
}

func (c *compiler) compileExpression(expr expression) (int, error) {
	target := c.allocReg()
	if err := c.compileExpressionTo(expr, target); err != nil {
		return 0, err
	}
	return target, nil
}

func (c *compiler) compileExpressionTo(expr expression, target int) error {
	if len(expr.terms) == 0 {
		return fmt.Errorf("compile: empty expression")
	}

	if err := c.compileTermTo(expr.terms[0], target); err != nil {
		return err
	}

	for _, term := range expr.terms[1:] {
		right := c.allocReg()
		if err := c.compileTermTo(term, right); err != nil {
			return err
		}
		c.code = append(c.code, instruction{op: opAdd, a: target, b: target, c: right})
	}

	return nil
}

func (c *compiler) compileTermTo(term term, target int) error {
	if term.number != nil {
		constant := len(c.constants)
		c.constants = append(c.constants, NumberValue(*term.number))
		c.code = append(c.code, instruction{op: opLoadConst, a: target, b: constant})
		return nil
	}
	if term.lit != nil {
		constant := len(c.constants)
		c.constants = append(c.constants, *term.lit)
		c.code = append(c.code, instruction{op: opLoadConst, a: target, b: constant})
		return nil
	}
	if term.table != nil {
		return c.compileTableTo(*term.table, target)
	}
	if term.call != nil {
		return c.compileCallTo(*term.call, target)
	}
	if term.field != nil {
		return c.compileFieldTo(*term.field, target)
	}
	if term.index != nil {
		return c.compileIndexTo(*term.index, target)
	}

	register, ok := c.locals[term.name]
	if !ok {
		return fmt.Errorf("compile: undefined local %q", term.name)
	}
	c.code = append(c.code, instruction{op: opMove, a: target, b: register})
	return nil
}

func (c *compiler) compileAssignment(stmt assignStatement) error {
	if stmt.target.field == "" {
		if stmt.target.index != nil {
			table := c.compileTableReceiver(stmt.target.name)

			index := c.allocReg()
			if err := c.compileExpressionTo(*stmt.target.index, index); err != nil {
				return err
			}
			value := c.allocReg()
			if err := c.compileExpressionTo(stmt.value, value); err != nil {
				return err
			}
			c.code = append(c.code, instruction{op: opSetIndex, a: table, b: index, c: value})
			return nil
		}
		target, ok := c.locals[stmt.target.name]
		if !ok {
			return fmt.Errorf("compile: undefined local %q", stmt.target.name)
		}
		return c.compileExpressionTo(stmt.value, target)
	}

	table := c.compileTableReceiver(stmt.target.name)

	value := c.allocReg()
	if err := c.compileExpressionTo(stmt.value, value); err != nil {
		return err
	}

	key := len(c.constants)
	c.constants = append(c.constants, StringValue(stmt.target.field))
	c.code = append(c.code, instruction{op: opSetField, a: table, b: key, c: value})
	return nil
}

func (c *compiler) compileIf(stmt ifStatement) error {
	condition, err := c.compileExpression(stmt.condition)
	if err != nil {
		return err
	}

	jumpIfFalse := len(c.code)
	c.code = append(c.code, instruction{op: opJumpIfFalse, a: condition})

	outerLocals := copyLocals(c.locals)
	if err := c.compileStatements(stmt.thenStatements); err != nil {
		return err
	}
	c.locals = copyLocals(outerLocals)

	jumpEnd := len(c.code)
	c.code = append(c.code, instruction{op: opJump})

	elseStart := len(c.code)
	c.code[jumpIfFalse].b = elseStart

	if len(stmt.elseStatements) > 0 {
		if err := c.compileStatements(stmt.elseStatements); err != nil {
			return err
		}
		c.locals = copyLocals(outerLocals)
	}

	c.code[jumpEnd].b = len(c.code)
	return nil
}

func (c *compiler) compileTableTo(table tableExpression, target int) error {
	c.code = append(c.code, instruction{op: opNewTable, a: target})
	for _, field := range table.fields {
		value := c.allocReg()
		if err := c.compileExpressionTo(field.value, value); err != nil {
			return err
		}
		key := len(c.constants)
		if field.name == "" {
			c.constants = append(c.constants, NumberValue(float64(field.arrayIndex)))
		} else {
			c.constants = append(c.constants, StringValue(field.name))
		}
		c.code = append(c.code, instruction{op: opSetField, a: target, b: key, c: value})
	}
	return nil
}

func (c *compiler) compileFieldTo(field fieldExpression, target int) error {
	table := c.compileTableReceiver(field.table)

	key := len(c.constants)
	c.constants = append(c.constants, StringValue(field.name))
	c.code = append(c.code, instruction{op: opGetField, a: target, b: table, c: key})
	return nil
}

func (c *compiler) compileIndexTo(index indexExpression, target int) error {
	table := c.compileTableReceiver(index.table)

	key := c.allocReg()
	if err := c.compileExpressionTo(index.index, key); err != nil {
		return err
	}
	c.code = append(c.code, instruction{op: opGetIndex, a: target, b: table, c: key})
	return nil
}

func (c *compiler) compileTableReceiver(name string) int {
	if register, ok := c.locals[name]; ok {
		return register
	}

	target := c.allocReg()
	constant := len(c.constants)
	c.constants = append(c.constants, StringValue(name))
	c.code = append(c.code, instruction{op: opLoadGlobal, a: target, b: constant})
	return target
}

func (c *compiler) compileCallTo(call callExpression, target int) error {
	if register, ok := c.locals[call.name]; ok {
		c.code = append(c.code, instruction{op: opMove, a: target, b: register})
	} else {
		constant := len(c.constants)
		c.constants = append(c.constants, StringValue(call.name))
		c.code = append(c.code, instruction{op: opLoadGlobal, a: target, b: constant})
	}

	for _, arg := range call.args {
		argRegister := c.allocReg()
		if err := c.compileExpressionTo(arg, argRegister); err != nil {
			return err
		}
	}

	c.code = append(c.code, instruction{op: opCall, a: target, b: target, c: len(call.args)})
	return nil
}

func (c *compiler) allocReg() int {
	register := c.nextReg
	c.nextReg++
	return register
}

func copyLocals(locals map[string]int) map[string]int {
	copied := make(map[string]int, len(locals))
	for name, register := range locals {
		copied[name] = register
	}
	return copied
}
