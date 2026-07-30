package rubyproof_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/besmpl/ember/internal/rubyproof"
)

const (
	h1bSourceBytes  = 718
	h1bSourceLF     = 35
	h1bSourceSHA256 = "999dee89ddb14f0c197fa0edde6fead99910eb82996b541409945ae09c3f41cb"
	h1bOracleOutput = "7|7|7|43|44|7|7"
	h1bOutputSHA256 = "bea4358be9700b9e664d3ac83b5385e2549c8889295e118ec79996cfe0ceff06"
)

func TestH1bSourceAndOracleAreFrozen(t *testing.T) {
	if len(rubyproof.H1bSource) != h1bSourceBytes || strings.Count(rubyproof.H1bSource, "\n") != h1bSourceLF ||
		!strings.HasSuffix(rubyproof.H1bSource, "\n") {
		t.Fatalf("H1bSource = %d bytes/%d LF/final-LF=%v, want %d/%d/true",
			len(rubyproof.H1bSource), strings.Count(rubyproof.H1bSource, "\n"), strings.HasSuffix(rubyproof.H1bSource, "\n"),
			h1bSourceBytes, h1bSourceLF)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(rubyproof.H1bSource))); got != h1bSourceSHA256 {
		t.Fatalf("H1bSource SHA-256 = %s, want %s", got, h1bSourceSHA256)
	}
	if rubyproof.H1bOracleOutput != h1bOracleOutput {
		t.Fatalf("H1bOracleOutput = %q, want %q", rubyproof.H1bOracleOutput, h1bOracleOutput)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(rubyproof.H1bOracleOutput+"\n"))); got != h1bOutputSHA256 {
		t.Fatalf("H1b oracle output SHA-256 = %s, want %s", got, h1bOutputSHA256)
	}

	first, diagnostics := rubyproof.CompileH1b(rubyproof.H1bSource)
	if len(diagnostics) != 0 || first.IsZero() {
		t.Fatalf("CompileH1b(H1bSource) = zero=%v diagnostics=%v", first.IsZero(), diagnostics)
	}
	second, diagnostics := rubyproof.CompileH1b(rubyproof.H1bSource)
	if len(diagnostics) != 0 || second.IsZero() || first.Identity() != second.Identity() || first.Identity() == ([sha256.Size]byte{}) {
		t.Fatalf("repeated CompileH1b = identities %x/%x zero=%v diagnostics=%v", first.Identity(), second.Identity(), second.IsZero(), diagnostics)
	}
}

func TestH1bAgainstPinnedLocalCRuby(t *testing.T) {
	if os.Getenv("EMBER_RUBY_H1B_ORACLE") == "" {
		t.Skip("set EMBER_RUBY_H1B_ORACLE=1 to run the pinned local CRuby H1b differential")
	}
	const (
		rubyPath    = "/usr/bin/ruby"
		wantRuby    = "ruby|2.6.10|universal.arm64e-darwin24"
		wantRubySHA = "0c6118f2b4a9fe448d51eba2e79c81342ae1e111f015d58dce5c11740f4dfacd"
	)
	rubyBytes, err := os.ReadFile(rubyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(rubyBytes)); got != wantRubySHA {
		t.Fatalf("CRuby executable SHA-256 = %s, want %s", got, wantRubySHA)
	}
	environment := cleanRubyEnvironment()
	version := strings.TrimSpace(runRubyTarget39(t, environment, "", rubyPath, "--disable-gems", "-e", `puts [RUBY_ENGINE,RUBY_VERSION,RUBY_PLATFORM].join("|")`))
	if version != wantRuby {
		t.Fatalf("CRuby identity = %q, want %q", version, wantRuby)
	}
	script := rubyproof.H1bSource + `puts h1b_result.join("|")` + "\n"
	want := rubyproof.H1bOracleOutput + "\n"
	for run := 1; run <= 2; run++ {
		if got := runRubyTarget39(t, environment, script, rubyPath, "--disable-gems", "-"); got != want {
			t.Fatalf("CRuby H1b run %d output = %q, want %q", run, got, want)
		}
	}
}
