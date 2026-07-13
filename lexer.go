package ember

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// tokenKind is deliberately kept to one byte. Token text is always recoverable
// from the source span; the payload carries the one value that cannot be
// recovered without decoding (numbers and escaped strings).
type tokenKind uint8

const (
	tokenIdentifier tokenKind = iota
	tokenNumber
	tokenString
	tokenSymbol
	tokenKeyword
)

const maxSourceTokenOffset = uint64(^uint32(0))

// sourceToken is the hot lexer/parser representation. Keep this shape small:
// a source span and one kind-specific payload are enough for every token.
type sourceToken struct {
	kind    tokenKind
	start   uint32
	end     uint32
	payload uint64
}

type sourceComment struct {
	text  string
	start int
	end   int
}

type lexResult struct {
	tokens         []sourceToken
	comments       []sourceComment
	decodedStrings []string
	mode           sourceMode
}

type lexerOptions struct {
	retainComments bool
	maxTokens      uint64
}

func (t sourceToken) startOffset() int {
	return int(t.start)
}

func (t sourceToken) endOffset() int {
	return int(t.end)
}

func (t sourceToken) textAt(source string) string {
	return source[t.startOffset():t.endOffset()]
}

func (t sourceToken) matchesWordAt(source string, pos int, word string) bool {
	return t.startOffset() == pos &&
		t.textAt(source) == word &&
		(t.kind == tokenKeyword || t.kind == tokenIdentifier)
}

func (t sourceToken) numberValue() float64 {
	return math.Float64frombits(t.payload)
}

// stringValue resolves a string token using the source span for raw literals
// and the lexer-owned side pool for escaped literals.
func (t sourceToken) stringValue(source string, decodedStrings []string) string {
	if t.payload != 0 {
		index := int(t.payload - 1)
		if index >= 0 && index < len(decodedStrings) {
			return decodedStrings[index]
		}
		return ""
	}
	if t.end <= t.start+1 {
		return ""
	}
	return source[t.startOffset()+1 : t.endOffset()-1]
}

func (t sourceToken) rawEquals(source, text string) bool {
	return len(text) == t.endOffset()-t.startOffset() &&
		source[t.startOffset():t.endOffset()] == text
}

type lexer struct {
	source         string
	pos            int
	mode           sourceMode
	retainComments bool
	decodedStrings []string
}

// lexSource retains comments for the focused lexer seam and existing tooling
// tests. Compile parsing uses lexSourceForCompile, which recognizes directives
// but does not retain discarded comment text.
func lexSource(source string) (lexResult, error) {
	return lexSourceWithOptions(source, lexerOptions{retainComments: true})
}

func lexSourceForCompile(source string) (lexResult, error) {
	return lexSourceWithOptions(source, lexerOptions{})
}

func lexSourceForCompileWithTokenLimit(source string, maxTokens uint64) (lexResult, error) {
	return lexSourceWithOptions(source, lexerOptions{maxTokens: maxTokens})
}

func lexSourceWithOptions(source string, options lexerOptions) (lexResult, error) {
	if err := validateSourceByteLength(uint64(len(source))); err != nil {
		return lexResult{}, err
	}

	l := lexer{
		source:         source,
		retainComments: options.retainComments,
	}
	// Source density is intentionally only a hint and is bounded so a huge
	// comment or string cannot turn preallocation into an unbounded request.
	tokens := make([]sourceToken, 0, estimatedTokenCapacity(len(source)))
	var comments []sourceComment
	if options.retainComments {
		comments = make([]sourceComment, 0, estimatedCommentCapacity(len(source)))
	}

	for {
		comment, ok, err := l.skipSpaceAndComment()
		if err != nil {
			return lexResult{}, err
		}
		if ok {
			if options.retainComments {
				comments = append(comments, comment)
			}
			continue
		}
		if l.done() {
			return lexResult{
				tokens:         tokens,
				comments:       comments,
				decodedStrings: l.decodedStrings,
				mode:           l.mode,
			}, nil
		}

		token, err := l.nextToken()
		if err != nil {
			return lexResult{}, err
		}
		if options.maxTokens != 0 && uint64(len(tokens)) >= options.maxTokens {
			return lexResult{}, &LimitError{Kind: LimitTokens, Limit: options.maxTokens, Used: uint64(len(tokens)) + 1}
		}
		tokens = append(tokens, token)
	}
}

func estimatedTokenCapacity(sourceLength int) int {
	if sourceLength == 0 {
		return 0
	}
	// The compiler corpus averages just over three source bytes per token.
	// Using that measured density avoids repeated slice growth without making
	// comment-heavy or unusually sparse source allocate in proportion to an
	// unbounded token count.
	capacity := sourceLength / 3
	if capacity < 8 {
		capacity = 8
	}
	const maxTokenCapacity = 4096
	if capacity > maxTokenCapacity {
		return maxTokenCapacity
	}
	return capacity
}

func estimatedCommentCapacity(sourceLength int) int {
	capacity := sourceLength / 32
	if capacity < 1 && sourceLength > 0 {
		capacity = 1
	}
	const maxCommentCapacity = 1024
	if capacity > maxCommentCapacity {
		return maxCommentCapacity
	}
	return capacity
}

func validateSourceByteLength(length uint64) error {
	if length > maxSourceTokenOffset {
		return fmt.Errorf("lex: source too large: %d bytes exceeds uint32 offset limit %d", length, maxSourceTokenOffset)
	}
	return nil
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
	if !l.retainComments {
		return sourceComment{start: start, end: l.pos}, true, nil
	}
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
	if !l.retainComments {
		return sourceComment{start: start, end: l.pos}, true, nil
	}
	text := strings.TrimSpace(l.source[textStart:textEnd])
	return sourceComment{text: text, start: start, end: l.pos}, true, nil
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
		kind := tokenIdentifier
		if isKeyword(l.source[start:l.pos]) {
			kind = tokenKeyword
		}
		return compactSourceToken(kind, start, l.pos, 0), nil
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
		return compactSourceToken(tokenNumber, start, l.pos, math.Float64bits(number)), nil
	}
	if ch == '"' || ch == '\'' {
		return l.stringToken()
	}
	return l.symbolToken()
}

func compactSourceToken(kind tokenKind, start, end int, payload uint64) sourceToken {
	return sourceToken{kind: kind, start: uint32(start), end: uint32(end), payload: payload}
}

func (l *lexer) stringToken() (sourceToken, error) {
	start := l.pos
	quote := l.source[l.pos]
	l.pos++
	rawStart := l.pos
	hasEscape := false
	var builder strings.Builder
	for !l.done() {
		chStart := l.pos
		ch := l.source[l.pos]
		l.pos++

		switch ch {
		case quote:
			if !hasEscape {
				return compactSourceToken(tokenString, start, l.pos, 0), nil
			}
			builder.WriteString(l.source[rawStart:chStart])
			index := uint64(len(l.decodedStrings))
			l.decodedStrings = append(l.decodedStrings, builder.String())
			return compactSourceToken(tokenString, start, l.pos, index+1), nil
		case '\\':
			if l.done() {
				return sourceToken{}, l.errorf("unterminated string")
			}
			if !hasEscape {
				hasEscape = true
				builder.Grow(l.pos - start)
			}
			builder.WriteString(l.source[rawStart:chStart])
			escaped := l.source[l.pos]
			l.pos++
			switch escaped {
			case '\\', quote:
				builder.WriteByte(escaped)
			case 'n':
				builder.WriteByte('\n')
			case 't':
				builder.WriteByte('\t')
			default:
				return sourceToken{}, l.errorf("unsupported string escape \\%c", escaped)
			}
			rawStart = l.pos
		case '\n', '\r':
			return sourceToken{}, l.errorf("unterminated string")
		}
	}
	return sourceToken{}, l.errorf("unterminated string")
}

func (l *lexer) symbolToken() (sourceToken, error) {
	start := l.pos
	for _, symbol := range []string{"...", "::", "//", "..", "==", "~=", "<=", ">=", "->", "<<", ">>"} {
		if strings.HasPrefix(l.source[l.pos:], symbol) {
			l.pos += len(symbol)
			return compactSourceToken(tokenSymbol, start, l.pos, 0), nil
		}
	}
	l.pos++
	return compactSourceToken(tokenSymbol, start, l.pos, 0), nil
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
