package ember

import (
	"testing"
)

func TestParserCheckpointRestoresExactToken(t *testing.T) {
	source := "type Value = number\nreturn Value"
	lexed, err := lexSource(source)
	if err != nil {
		t.Fatalf("lexSource returned error: %v", err)
	}

	parser := parser{source: source, tokens: lexed.tokens, stringPool: lexed.decodedStrings}
	parser.skipSpace()
	checkpoint := parser.mark()
	want, ok := parser.currentToken()
	if !ok {
		t.Fatal("currentToken returned no token before speculation")
	}

	parser.consumeKeyword("type")
	parser.skipSpace()
	parser.consumeToken(tokenIdentifier)
	parser.restore(checkpoint)

	got, ok := parser.currentToken()
	if !ok {
		t.Fatal("currentToken returned no token after restore")
	}
	if got != want {
		t.Fatalf("restored token = %#v, want %#v", got, want)
	}
	if parser.pos != checkpoint.pos || parser.tokenIndex != checkpoint.tokenIndex {
		t.Fatalf("restored cursor = (%d, %d), want (%d, %d)", parser.pos, parser.tokenIndex, checkpoint.pos, checkpoint.tokenIndex)
	}
}

func TestCompileRunWhitespaceAndSpeculativeForms(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   Value
	}{
		{
			name:   "assignment after call speculation",
			source: "  \nlocal value = 1\n\nvalue = value + 2\nreturn value\n",
			want:   NumberValue(3),
		},
		{
			name: "named function type arguments",
			source: `--!strict
type Formatter = (amount: number, label: string?) -> string
local format: Formatter = function(amount: number, label: string?): string
    return label .. amount
end
return format(5, "hp")`,
			want: StringValue("hp5"),
		},
		{
			name: "typeof and named table fields",
			source: `
local template = {name = "ember"}
type Template = typeof(template)
local clone: typeof(template) = template
return clone.name`,
			want: StringValue("ember"),
		},
		{
			name: "table type access modifiers",
			source: `--!strict
export type Model = {
    read Name: string,
    write [number]: boolean,
}
local model: Model = {Name = "ember"}
return model.Name`,
			want: StringValue("ember"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
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
			assertParserCursorValue(t, results[0], test.want)
		})
	}
}

func FuzzParserSpeculativePaths(f *testing.F) {
	for _, source := range []string{
		"value = 1\nreturn value",
		"value(1)\nreturn value",
		"type Value = number\nreturn 1",
		"local value: typeof(source) = source\nreturn value",
		"local value: (item: number) -> number = function(item) return item end\nreturn value(1)",
		"local value: {read name: string, write count: number} = {}\nreturn value",
		"return {name = \"ember\", 1}",
	} {
		f.Add(source)
	}

	f.Fuzz(func(t *testing.T, source string) {
		_, _ = parseSource(Source{Text: source})
	})
}

func assertParserCursorValue(t *testing.T, got Value, want Value) {
	t.Helper()
	if got.Kind() != want.Kind() {
		t.Fatalf("Run result kind is %s, want %s", got.Kind(), want.Kind())
	}
	switch want.Kind() {
	case NumberKind:
		gotNumber, gotOK := got.Number()
		wantNumber, wantOK := want.Number()
		if !gotOK || !wantOK || gotNumber != wantNumber {
			t.Fatalf("Run result is %v, want %v", got, want)
		}
	case StringKind:
		gotString, gotOK := got.String()
		wantString, wantOK := want.String()
		if !gotOK || !wantOK || gotString != wantString {
			t.Fatalf("Run result is %v, want %v", got, want)
		}
	default:
		t.Fatalf("unsupported expected value kind %s", want.Kind())
	}
}
