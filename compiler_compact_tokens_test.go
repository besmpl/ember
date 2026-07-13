package ember

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

func TestCompileRunStillHandlesStringLiteralAfterCompactLexing(t *testing.T) {
	proto, err := Compile(`return "ember\n\tvalue"`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	got, ok := results[0].String()
	if !ok || got != "ember\n\tvalue" {
		t.Fatalf("Run result = %q, %t; want decoded string", got, ok)
	}
}

func TestCompileLexerSkipsCommentsButKeepsDirectives(t *testing.T) {
	source := "--!strict\n-- a discarded comment\nreturn 1\n"
	lexed, err := lexSourceForCompile(source)
	if err != nil {
		t.Fatalf("lexSourceForCompile returned error: %v", err)
	}
	if lexed.mode != sourceModeStrict {
		t.Fatalf("mode = %q, want strict", lexed.mode)
	}
	if len(lexed.comments) != 0 {
		t.Fatalf("compile lexer retained %d comments, want none", len(lexed.comments))
	}
	if got := len(lexed.tokens); got != 2 {
		t.Fatalf("compile lexer produced %d tokens, want return and literal", got)
	}
}

func TestCompileLexerBoundsSparseCommentPreallocation(t *testing.T) {
	source := strings.Repeat("-- discarded comment\n", 1<<14)
	if len(source) < 256<<10 {
		source += strings.Repeat(" ", (256<<10)-len(source))
	}
	lexed, err := lexSourceForCompile(source)
	if err != nil {
		t.Fatalf("lexSourceForCompile returned error: %v", err)
	}
	if len(lexed.tokens) != 0 {
		t.Fatalf("comment-only source produced %d tokens, want none", len(lexed.tokens))
	}
	if got := cap(lexed.tokens); got > 4096 {
		t.Fatalf("comment-only token capacity = %d, want bounded at 4096", got)
	}
}

func TestCompileLexerTiesTokenPreallocationToExplicitLimit(t *testing.T) {
	lexed, err := lexSourceWithOptions("return 1", lexerOptions{maxTokens: 1})
	if err == nil {
		t.Fatal("lexSourceWithOptions succeeded, want token limit error")
	}

	lexed, err = lexSourceWithOptions("return 1", lexerOptions{maxTokens: 2})
	if err != nil {
		t.Fatalf("lexSourceWithOptions returned error: %v", err)
	}
	if got := cap(lexed.tokens); got > 2 {
		t.Fatalf("token capacity = %d, want at most explicit limit 2", got)
	}
}

func TestEstimatedTokenCapacityKeepsDenseAndCommentHeavyBounds(t *testing.T) {
	if got := estimatedTokenCapacityForSource(strings.Repeat("value = value + 1\n", 15000), 0); got != 64<<10 {
		t.Fatalf("dense token capacity = %d, want exact 64K-token cap", got)
	}
	denseWithComment := strings.Repeat("value = value + 1\n", 15000) + "-- one comment\n"
	if got := estimatedTokenCapacityForSource(denseWithComment, 0); got != 64<<10 {
		t.Fatalf("dense source with one comment capacity = %d, want exact 64K-token cap", got)
	}
	stringMarkerSource := strings.Repeat("return \"-- marker\"\n", 16000)
	if got := estimatedTokenCapacityForSource(stringMarkerSource, 0); got != 64<<10 {
		t.Fatalf("string marker source capacity = %d, want exact 64K-token cap", got)
	}
	commentSource := strings.Repeat("-- comment\n", 1<<14) + "return 1\n"
	if got := estimatedTokenCapacityForSource(commentSource, 0); got > 4096 {
		t.Fatalf("comment-heavy token capacity = %d, want sparse bound 4096", got)
	}
}

func TestCompactTokenPayloadsSeparateRawAndEscapedStrings(t *testing.T) {
	source := `return "plain", "line\nfeed"`
	lexed, err := lexSourceForCompile(source)
	if err != nil {
		t.Fatalf("lexSourceForCompile returned error: %v", err)
	}
	if got := len(lexed.decodedStrings); got != 1 {
		t.Fatalf("decoded string side pool length = %d, want 1", got)
	}
	if lexed.tokens[1].payload != 0 {
		t.Fatalf("unescaped string payload = %d, want raw-span sentinel 0", lexed.tokens[1].payload)
	}
	if lexed.tokens[3].payload == 0 {
		t.Fatal("escaped string payload is zero, want side-pool index")
	}
	if got := lexed.tokens[1].stringValue(source, lexed.decodedStrings); got != "plain" {
		t.Fatalf("raw string value = %q, want plain", got)
	}
	if got := lexed.tokens[3].stringValue(source, lexed.decodedStrings); got != "line\nfeed" {
		t.Fatalf("escaped string value = %q, want decoded line feed", got)
	}
}

func TestLexSourceRejectsSourceOffsetOverflowWithoutAllocatingSource(t *testing.T) {
	if err := validateSourceByteLength(maxSourceTokenOffset); err != nil {
		t.Fatalf("maximum representable source length rejected: %v", err)
	}
	err := validateSourceByteLength(maxSourceTokenOffset + 1)
	if err == nil {
		t.Fatal("source offset overflow accepted")
	}
	want := fmt.Sprintf("lex: source too large: %d bytes exceeds uint32 offset limit %d", maxSourceTokenOffset+1, maxSourceTokenOffset)
	if err.Error() != want {
		t.Fatalf("source overflow error = %q, want %q", err, want)
	}
}

func TestCompileClonesRawStringBeforeProtoOwnership(t *testing.T) {
	const literal = "literal"
	prefix := strings.Repeat(" ", 2048)
	source := prefix + `return "` + literal + `"`
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	for _, value := range proto.constants {
		if value.Kind() != StringKind {
			continue
		}
		text, ok := value.String()
		if !ok || text != literal {
			continue
		}
		sourceStart := strings.Index(source, literal)
		if sourceStart < 0 {
			t.Fatal("source literal missing")
		}
		if unsafe.StringData(text) == unsafe.StringData(source[sourceStart:sourceStart+len(literal)]) {
			t.Fatal("Proto string still aliases source backing storage")
		}
		return
	}
	t.Fatalf("Proto constants did not contain %q", literal)
}

func TestCompilePreservesExactLexicalErrorBytes(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   string
	}{
		{source: `return "bad\q"`, want: `lex: byte 13: unsupported string escape \q`},
		{source: `return "bad`, want: `lex: byte 11: unterminated string`},
		{source: `--[[`, want: `lex: byte 4: unterminated block comment`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			_, err := Compile(tc.source)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Compile error = %v, want %q", err, tc.want)
			}
		})
	}
}

func FuzzLexMalformedEscapes(f *testing.F) {
	for _, seed := range []string{`\q`, `\`, `\n`, `\t`, `\\`, `\"`, "\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		source := `return "` + body + `"`
		_, _ = lexSource(source)
	})
}
