package main

import (
	"strings"
	"testing"
)

func TestParseDispatchSpecCoversEveryOpcodeExactlyOnce(t *testing.T) {
	source := []byte("package ember\nconst directFrameSemanticSpec = `\nopLoadConst none pure none\nopMove none pure none\n`\n")
	entries, err := parseDispatchSpec(source, []string{"opLoadConst", "opMove"})
	if err != nil {
		t.Fatalf("parseDispatchSpec returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("parseDispatchSpec returned %d entries, want 2", len(entries))
	}
	if entries[0].opcode != "opLoadConst" || entries[0].family != "none" || entries[0].tiling != "pure" || entries[0].cache != "none" {
		t.Fatalf("first entry = %#v", entries[0])
	}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "missing opcode",
			source: "opLoadConst none pure none",
			want:   "missing opMove",
		},
		{
			name:   "duplicate opcode",
			source: "opLoadConst none pure none\nopLoadConst none pure none\nopMove none pure none",
			want:   "duplicate opLoadConst",
		},
		{
			name:   "unknown opcode",
			source: "opLoadConst none pure none\nopMove none pure none\nopBenchmarkSpecial benchmark pure none",
			want:   "unknown opcode opBenchmarkSpecial",
		},
		{
			name:   "unknown family",
			source: "opLoadConst corpus-identity pure none\nopMove none pure none",
			want:   "unknown specialization family",
		},
		{
			name:   "unknown tiling",
			source: "opLoadConst none whole-program none\nopMove none pure none",
			want:   "unknown tiling policy",
		},
		{
			name:   "missing cache field",
			source: "opLoadConst none pure\nopMove none pure none",
			want:   "want opcode family tiling cache",
		},
		{
			name:   "unknown cache",
			source: "opLoadConst none pure benchmark-name\nopMove none pure none",
			want:   "unknown cache layout",
		},
		{
			name:   "extra field",
			source: "opLoadConst none pure none arithmetic_for\nopMove none pure none",
			want:   "want opcode family tiling cache",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wrapped := []byte("package ember\nconst directFrameSemanticSpec = `\n" + test.source + "\n`\n")
			_, err := parseDispatchSpec(wrapped, []string{"opLoadConst", "opMove"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseDispatchSpec error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseDispatchSpecRequiresOneRawStringDeclaration(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "missing", source: "package ember\n", want: "exactly once"},
		{name: "interpreted string", source: "package ember\nconst directFrameSemanticSpec = \"opLoadConst none pure none\"\n", want: "raw string"},
		{name: "duplicate", source: "package ember\nconst directFrameSemanticSpec = `opLoadConst none pure none`\nconst directFrameSemanticSpec = `opLoadConst none pure none`\n", want: "exactly once"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseDispatchSpec([]byte(test.source), []string{"opLoadConst"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseDispatchSpec error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseFusionSpecIsClosedAndBounded(t *testing.T) {
	source := []byte("package ember\nconst directFrameFusionSpec = `\nnumeric-for-trace numeric-loop 16\n`\n")
	entries, err := parseFusionSpec(source)
	if err != nil {
		t.Fatalf("parseFusionSpec returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].name != "numeric-for-trace" || entries[0].family != "numeric-loop" || entries[0].instructionCap != 16 {
		t.Fatalf("fusion entries = %#v", entries)
	}

	tests := []struct {
		name string
		spec string
		want string
	}{
		{name: "duplicate", spec: "numeric-for-trace numeric-loop 16\nnumeric-for-trace numeric-loop 16", want: "duplicate fusion"},
		{name: "unknown fusion", spec: "arithmetic-for-benchmark numeric-loop 16", want: "unknown fusion"},
		{name: "unknown family", spec: "numeric-for-trace source-identity 16", want: "unknown fusion family"},
		{name: "zero cap", spec: "numeric-for-trace numeric-loop 0", want: "instruction cap"},
		{name: "large cap", spec: "numeric-for-trace numeric-loop 65", want: "instruction cap"},
		{name: "extra field", spec: "numeric-for-trace numeric-loop 16 corpus", want: "want name family instruction-cap"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wrapped := []byte("package ember\nconst directFrameFusionSpec = `\n" + test.spec + "\n`\n")
			_, err := parseFusionSpec(wrapped)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseFusionSpec error = %v, want %q", err, test.want)
			}
		})
	}
}
