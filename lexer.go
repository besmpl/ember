package ember

import (
	"fmt"
	"strconv"
	"strings"
)

type tokenKind string

const (
	tokenIdentifier tokenKind = "identifier"
	tokenNumber     tokenKind = "number"
	tokenString     tokenKind = "string"
	tokenSymbol     tokenKind = "symbol"
	tokenKeyword    tokenKind = "keyword"
)

type sourceToken struct {
	kind        tokenKind
	text        string
	start       int
	end         int
	number      float64
	stringValue string
}

type sourceComment struct {
	text  string
	start int
	end   int
}

func (t sourceToken) matchesWordAt(pos int, word string) bool {
	return t.start == pos &&
		t.text == word &&
		(t.kind == tokenKeyword || t.kind == tokenIdentifier)
}

type lexer struct {
	source string
	pos    int
	mode   sourceMode
}

func lexSource(source string) ([]sourceToken, []sourceComment, sourceMode, error) {
	l := lexer{source: source}
	var tokens []sourceToken
	var comments []sourceComment

	for {
		comment, ok, err := l.skipSpaceAndComment()
		if err != nil {
			return nil, nil, "", err
		}
		if ok {
			comments = append(comments, comment)
			continue
		}
		if l.done() {
			return tokens, comments, l.mode, nil
		}

		token, err := l.nextToken()
		if err != nil {
			return nil, nil, "", err
		}
		tokens = append(tokens, token)
	}
}

func (l *lexer) skipSpaceAndComment() (sourceComment, bool, error) {
	for !l.done() {
		switch l.source[l.pos] {
		case ' ', '\t', '\n', '\r':
			l.pos++
			continue
		case '-':
			if !strings.HasPrefix(l.source[l.pos:], "--") {
				return sourceComment{}, false, nil
			}
			if strings.HasPrefix(l.source[l.pos:], "--[[") {
				return l.blockComment()
			}
			return l.lineComment()
		default:
			return sourceComment{}, false, nil
		}
	}
	return sourceComment{}, false, nil
}

func (l *lexer) lineComment() (sourceComment, bool, error) {
	start := l.pos
	l.pos += 2
	textStart := l.pos
	for !l.done() && l.source[l.pos] != '\n' && l.source[l.pos] != '\r' {
		l.pos++
	}
	text := strings.TrimSpace(l.source[textStart:l.pos])
	l.applyDirective(text)
	return sourceComment{text: text, start: start, end: l.pos}, true, nil
}

func (l *lexer) blockComment() (sourceComment, bool, error) {
	start := l.pos
	l.pos += len("--[[")
	textStart := l.pos
	end := strings.Index(l.source[l.pos:], "]]")
	if end < 0 {
		return sourceComment{}, false, l.errorf("unterminated block comment")
	}
	textEnd := l.pos + end
	l.pos = textEnd + len("]]")
	return sourceComment{
		text:  strings.TrimSpace(l.source[textStart:textEnd]),
		start: start,
		end:   l.pos,
	}, true, nil
}

func (l *lexer) applyDirective(text string) {
	if mode, ok := sourceModeDirective(text); ok {
		l.mode = mode
	}
}

func (l *lexer) nextToken() (sourceToken, error) {
	start := l.pos
	ch := l.source[l.pos]
	if isIdentStartByte(ch) {
		l.pos++
		for !l.done() && isIdentByte(l.source[l.pos]) {
			l.pos++
		}
		text := l.source[start:l.pos]
		kind := tokenIdentifier
		if isKeyword(text) {
			kind = tokenKeyword
		}
		return sourceToken{kind: kind, text: text, start: start, end: l.pos}, nil
	}
	if l.isNumberStart() {
		l.pos++
		for !l.done() && (isIdentByte(l.source[l.pos]) || l.source[l.pos] == '.') {
			l.pos++
		}
		text := l.source[start:l.pos]
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return sourceToken{}, l.errorf("invalid number %q", text)
		}
		return sourceToken{kind: tokenNumber, text: text, start: start, end: l.pos, number: number}, nil
	}
	if ch == '"' || ch == '\'' {
		return l.stringToken()
	}
	return l.symbolToken()
}

func (l *lexer) stringToken() (sourceToken, error) {
	start := l.pos
	quote := l.source[l.pos]
	l.pos++
	var b strings.Builder
	for !l.done() {
		ch := l.source[l.pos]
		l.pos++

		switch ch {
		case quote:
			return sourceToken{
				kind:        tokenString,
				text:        l.source[start:l.pos],
				start:       start,
				end:         l.pos,
				stringValue: b.String(),
			}, nil
		case '\\':
			if l.done() {
				return sourceToken{}, l.errorf("unterminated string")
			}
			escaped := l.source[l.pos]
			l.pos++
			switch escaped {
			case '\\', quote:
				b.WriteByte(escaped)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return sourceToken{}, l.errorf("unsupported string escape \\%c", escaped)
			}
		case '\n', '\r':
			return sourceToken{}, l.errorf("unterminated string")
		default:
			b.WriteByte(ch)
		}
	}
	return sourceToken{}, l.errorf("unterminated string")
}

func (l *lexer) symbolToken() (sourceToken, error) {
	start := l.pos
	for _, symbol := range []string{"...", "::", "//", "..", "==", "~=", "<=", ">=", "->", "<<", ">>"} {
		if strings.HasPrefix(l.source[l.pos:], symbol) {
			l.pos += len(symbol)
			return sourceToken{kind: tokenSymbol, text: symbol, start: start, end: l.pos}, nil
		}
	}
	l.pos++
	return sourceToken{kind: tokenSymbol, text: l.source[start:l.pos], start: start, end: l.pos}, nil
}

func (l *lexer) isNumberStart() bool {
	ch := l.source[l.pos]
	if ch >= '0' && ch <= '9' {
		return true
	}
	return ch == '.' && l.pos+1 < len(l.source) && l.source[l.pos+1] >= '0' && l.source[l.pos+1] <= '9'
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
	case "and", "break", "continue", "do", "else", "elseif", "end", "false", "for", "function", "if", "local", "nil", "not", "or", "repeat", "return", "then", "true", "until", "while":
		return true
	default:
		return false
	}
}

func (l *lexer) done() bool {
	return l.pos >= len(l.source)
}

func (l *lexer) errorf(format string, args ...any) error {
	return fmt.Errorf("lex: byte %d: %s", l.pos, fmt.Sprintf(format, args...))
}
