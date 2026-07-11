package ember

import (
	"reflect"
	"strings"
	"testing"
)

func TestLexSourceKeepsDirectivesCommentsAndTokenRanges(t *testing.T) {
	source := "--!strict\nlocal hp = 10 -- health\nreturn hp\n"

	lexed, err := lexSource(source)
	if err != nil {
		t.Fatalf("lexSource returned error: %v", err)
	}

	if lexed.mode != sourceModeStrict {
		t.Fatalf("mode is %q, want strict", lexed.mode)
	}

	gotComments := make([]string, len(lexed.comments))
	for i, comment := range lexed.comments {
		gotComments[i] = comment.text
	}
	wantComments := []string{"!strict", "health"}
	if !reflect.DeepEqual(gotComments, wantComments) {
		t.Fatalf("comments = %#v, want %#v", gotComments, wantComments)
	}

	var got []string
	for _, token := range lexed.tokens {
		got = append(got, token.textAt(source))
	}
	want := []string{"local", "hp", "=", "10", "return", "hp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("token texts = %#v, want %#v", got, want)
	}

	hp := lexed.tokens[1]
	if hp.start != 16 || hp.end != 18 {
		t.Fatalf("hp range is %d..%d, want 16..18", hp.start, hp.end)
	}
}

func TestLexSourceKeepsMultiCharacterSymbols(t *testing.T) {
	source := `return ... :: number // 2 .. "hp" == value ~= nil <= max >= min -> out <<T>>`
	lexed, err := lexSource(source)
	if err != nil {
		t.Fatalf("lexSource returned error: %v", err)
	}

	var got []string
	for _, token := range lexed.tokens {
		if token.kind == tokenSymbol {
			got = append(got, token.textAt(source))
		}
	}
	want := []string{"...", "::", "//", "..", "==", "~=", "<=", ">=", "->", "<<", ">>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("symbol tokens = %#v, want %#v", got, want)
	}
}

func TestLexSourceParsesNumberAndStringValues(t *testing.T) {
	source := `return 42.5, "ember\n\t\""`
	lexed, err := lexSource(source)
	if err != nil {
		t.Fatalf("lexSource returned error: %v", err)
	}

	if lexed.tokens[1].kind != tokenNumber || lexed.tokens[1].numberValue() != 42.5 {
		t.Fatalf("number token is %#v, want parsed 42.5", lexed.tokens[1])
	}
	if lexed.tokens[3].kind != tokenString || lexed.tokens[3].stringValue(source, lexed.decodedStrings) != "ember\n\t\"" {
		t.Fatalf("string token is %#v, want decoded string", lexed.tokens[3])
	}
}

func TestLexSourceRejectsUnsupportedStringEscape(t *testing.T) {
	_, err := lexSource(`return "bad\q"`)
	if err == nil {
		t.Fatal("lexSource succeeded, want unsupported escape error")
	}
	if !strings.Contains(err.Error(), `unsupported string escape \q`) {
		t.Fatalf("lexSource error is %q, want unsupported escape", err)
	}
}
