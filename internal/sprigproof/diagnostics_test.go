package sprigproof

import (
	"strings"
	"testing"
)

func TestDeterministicDiagnosticsAndExactSpans(t *testing.T) {
	source := "package p\nrecord Reading { value i64 divisor i64 }\nunion Step { Add { by i64 } Reject { code i64 } }\nunion Result { Ok { total i64 rejected []i64 } Err { code i64 } }\nfunc Reduce(readings []Reading) Result { let x = missing return Ok { total: x rejected: [] } }\n"
	_, first := Compile(source)
	_, second := Compile(source)
	if len(first) == 0 {
		t.Fatal("invalid program compiled")
	}
	if !diagnosticsEqual(first, second) {
		t.Fatalf("diagnostics unstable:\n%v\n%v", first, second)
	}
	want := "5:50-5:57: unknown binding \"missing\""
	found := false
	for _, d := range first {
		if d.String() == want {
			found = true
		}
		if d.Span.Start < 0 || d.Span.Start > d.Span.End || d.Span.End > len(source) {
			t.Fatalf("invalid span %#v", d.Span)
		}
	}
	if !found {
		t.Fatalf("missing exact diagnostic %q in %v", want, first)
	}
}
func TestExhaustivenessAndUnsupportedFrontierAreRejected(t *testing.T) {
	nonexhaustive := strings.Replace(ProofSource, "      Reject { code } -> {\n        rejected = append(rejected, code)\n      }\n", "", 1)
	_, diagnostics := Compile(nonexhaustive)
	assertDiagnosticContains(t, diagnostics, "non-exhaustive match: missing Reject")
	noReturn := strings.Replace(ProofSource, "  return Ok { total: total, rejected: rejected }\n", "", 1)
	_, diagnostics = Compile(noReturn)
	assertDiagnosticContains(t, diagnostics, "function must end with return")
	localLoop := strings.Replace(ProofSource, "for reading in readings", "for reading in rejected", 1)
	_, diagnostics = Compile(localLoop)
	assertDiagnosticContains(t, diagnostics, "loops may range only over the Reduce input")
	reserved := strings.ReplaceAll(ProofSource, "readings", "ctx")
	if _, diagnostics = Compile(reserved); len(diagnostics) != 0 {
		t.Fatalf("support spelling rejected: %v", diagnostics)
	}
	collision := strings.ReplaceAll(ProofSource, "readings", "poll")
	if _, diagnostics = Compile(collision); len(diagnostics) != 0 {
		t.Fatalf("support collision rejected: %v", diagnostics)
	}
	reversed := strings.Replace(ProofSource, "Ok { total: total, rejected: rejected }", "Ok { rejected: rejected, total: total }", 1)
	_, diagnostics = Compile(reversed)
	assertDiagnosticContains(t, diagnostics, "fields must follow declaration order")
	wrongPackage := strings.Replace(ProofSource, "package counter", "package other", 1)
	_, diagnostics = Compile(wrongPackage)
	assertDiagnosticContains(t, diagnostics, "proof package must be counter")
}
func TestMalformedInputIsBounded(t *testing.T) {
	_, diagnostics := Compile("package p\n" + strings.Repeat("@", maximumSourceBytes))
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "source exceeds") {
		t.Fatalf("oversize diagnostics=%v", diagnostics)
	}
	_, diagnostics = Compile("package p\n" + strings.Repeat("x ", maximumTokens+1))
	if len(diagnostics) == 0 || len(diagnostics) > maximumDiagnostics {
		t.Fatalf("token-bound diagnostics=%d", len(diagnostics))
	}
	nodeHeavy := "package counter\nrecord Reading { value i64 divisor i64 }\nunion Step { Add { by i64 } Reject { code i64 } }\nunion Result { Ok { total i64 rejected []i64 } Err { code i64 } }\nfunc Reduce(readings []Reading) Result {\n" + strings.Repeat("return 1\n", 3000) + "}\n"
	_, diagnostics = Compile(nodeHeavy)
	assertDiagnosticContains(t, diagnostics, "syntax node count exceeds limit")
}
func diagnosticsEqual(a, b []Diagnostic) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func assertDiagnosticContains(t *testing.T, diagnostics []Diagnostic, want string) {
	t.Helper()
	for _, d := range diagnostics {
		if strings.Contains(d.Message, want) {
			return
		}
	}
	t.Fatalf("missing diagnostic containing %q in %v", want, diagnostics)
}
