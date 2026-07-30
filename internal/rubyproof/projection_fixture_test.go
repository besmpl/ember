package rubyproof

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRubyProjectionV2FixtureIsFresh(t *testing.T) {
	const initialLabelV1 = "  def label()\n    mark(6)\n    1\n  end"
	if count := strings.Count(ProofSource, initialLabelV1); count != 1 {
		t.Fatalf("initial-label transform matches %d locations, want 1", count)
	}
	source := strings.Replace(ProofSource, initialLabelV1, "  def label()\n    mark(6)\n    9\n  end", 1)
	program, diagnostics := Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	set, err := program.PreparedSource("rubyv2")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(set.Files()[0].Content)
	fixture := filepath.Join("..", "rubyprojectionproof", "generated", "rubyv2", "ruby_generated.go")
	if os.Getenv("EMBER_UPDATE_RUBY_PROJECTION_FIXTURE") != "" {
		if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatal("generated Ruby projection v2 fixture is stale; run EMBER_UPDATE_RUBY_PROJECTION_FIXTURE=1 go test ./internal/rubyproof -run TestRubyProjectionV2FixtureIsFresh")
	}
	t.Logf("Program=%x Set=%x generated=%x bytes=%d", program.Identity(), set.Digest(), sha256.Sum256(content), len(content))
}
