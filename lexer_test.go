package ember

import (
	"reflect"
	"strings"
	"testing"
)

func TestLexSourceKeepsDirectivesCommentsAndTokenRanges(t *testing.T) {
	source := "--!strict\nlocal hp = 10 -- health\nreturn hp\n"

	tokens, comments, mode, err := lexSource(source)
	if err != nil {
		t.Fatalf("lexSource returned error: %v", err)
	}

	if mode != sourceModeStrict {
		t.Fatalf("mode is %q, want strict", mode)
	}

	gotComments := make([]string, len(comments))
	for i, comment := range comments {
		gotComments[i] = comment.text
	}
	wantComments := []string{"!strict", "health"}
	if !reflect.DeepEqual(gotComments, wantComments) {
		t.Fatalf("comments = %#v, want %#v", gotComments, wantComments)
	}

	var got []string
	for _, token := range tokens {
		got = append(got, token.text)
	}
	want := []string{"local", "hp", "=", "10", "return", "hp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("token texts = %#v, want %#v", got, want)
	}

	hp := tokens[1]
	if hp.start != 16 || hp.end != 18 {
		t.Fatalf("hp range is %d..%d, want 16..18", hp.start, hp.end)
	}
}

func TestLexSourceKeepsMultiCharacterSymbols(t *testing.T) {
	tokens, _, _, err := lexSource(`return ... :: number // 2 .. "hp" == value ~= nil <= max >= min -> out <<T>>`)
	if err != nil {
		t.Fatalf("lexSource returned error: %v", err)
	}

	var got []string
	for _, token := range tokens {
		if token.kind == tokenSymbol {
			got = append(got, token.text)
		}
	}
	want := []string{"...", "::", "//", "..", "==", "~=", "<=", ">=", "->", "<<", ">>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("symbol tokens = %#v, want %#v", got, want)
	}
}

func TestLexSourceParsesNumberAndStringValues(t *testing.T) {
	tokens, _, _, err := lexSource(`return 42.5, "ember\n\t\""`)
	if err != nil {
		t.Fatalf("lexSource returned error: %v", err)
	}

	if tokens[1].kind != tokenNumber || tokens[1].number != 42.5 {
		t.Fatalf("number token is %#v, want parsed 42.5", tokens[1])
	}
	if tokens[3].kind != tokenString || tokens[3].stringValue != "ember\n\t\"" {
		t.Fatalf("string token is %#v, want decoded string", tokens[3])
	}
}

func TestLexSourceRejectsUnsupportedStringEscape(t *testing.T) {
	_, _, _, err := lexSource(`return "bad\q"`)
	if err == nil {
		t.Fatal("lexSource succeeded, want unsupported escape error")
	}
	if !strings.Contains(err.Error(), `unsupported string escape \q`) {
		t.Fatalf("lexSource error is %q, want unsupported escape", err)
	}
}
