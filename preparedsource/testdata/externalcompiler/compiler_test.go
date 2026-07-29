package seedcompiler_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	compiler "example.com/ember-external-compiler"
)

const programSource = `package score

func Reduce(value i64, divisor i64) i64 {
  return value / divisor + 1 / value + 2
}
`

func TestCompileEvaluateAndPrepareDeterministically(t *testing.T) {
	program, diagnostics := compiler.Compile(programSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if program.IsZero() || program.PackageName() != "score" || program.Identity() == [32]byte{} {
		t.Fatalf("invalid checked program: zero=%v package=%q identity=%x", program.IsZero(), program.PackageName(), program.Identity())
	}
	tests := []struct {
		name           string
		value, divisor int64
		want           compiler.Result
	}{
		{"positive", 12, 3, compiler.Result{Value: 6}},
		{"left domain wins", math.MinInt64, -1, compiler.Result{Code: compiler.DomainOverflow}},
		{"right domain after left", 0, 1, compiler.Result{Code: compiler.DomainDivideByZero}},
		{"addition overflow", math.MaxInt64, 1, compiler.Result{Code: compiler.DomainOverflow}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := program.Evaluate(context.Background(), test.value, test.divisor, 1)
			if err != nil || got != test.want {
				t.Fatalf("Evaluate=%#v,%v want %#v,nil", got, err, test.want)
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := program.Evaluate(canceled, 12, 3, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled Evaluate=%v", err)
	}
	if _, err := program.Evaluate(context.Background(), 12, 3, 0); !errors.Is(err, compiler.ErrStepLimit) {
		t.Fatalf("limited Evaluate=%v", err)
	}
	if _, err := program.Evaluate(nil, 12, 3, 1); err == nil || !strings.Contains(err.Error(), "nil context") {
		t.Fatalf("nil-context Evaluate=%v", err)
	}

	first, err := program.PreparedSource()
	if err != nil {
		t.Fatal(err)
	}
	secondProgram, diagnostics := compiler.Compile(programSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	second, err := secondProgram.PreparedSource()
	if err != nil {
		t.Fatal(err)
	}
	if first.IsZero() || first.PackageName() != "score" || first.Digest() != second.Digest() || !reflect.DeepEqual(first.Files(), second.Files()) {
		t.Fatalf("nondeterministic prepared source: %x/%x", first.Digest(), second.Digest())
	}
	if len(first.Files()) != 1 || first.Files()[0].Name != "reduce_generated.go" || !strings.Contains(first.Files()[0].Content, "Seed source identity: "+hexIdentity(program.Identity())) {
		t.Fatalf("prepared source does not bind checked identity: %#v", first.Files())
	}

	changed, diagnostics := compiler.Compile(programSource + "// exact source identity changes\n")
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	changedSet, err := changed.PreparedSource()
	if err != nil {
		t.Fatal(err)
	}
	if changed.Identity() == program.Identity() || changedSet.Digest() == first.Digest() {
		t.Fatal("exact source change preserved compiler or Set identity")
	}
}

func TestDiagnosticsAreBoundedAndDeterministic(t *testing.T) {
	tokenHeavy := sourceWithExpression(strings.Repeat("1 + ", 260) + "1")
	nodeHeavy := sourceWithExpression(strings.Repeat("1 + ", 130) + "1")
	tests := []struct {
		name, source, want string
	}{
		{"empty", "", "expected \"package\""},
		{"character", strings.Replace(programSource, "+ 2", "@ 2", 1), "invalid character"},
		{"package", strings.Replace(programSource, "package score", "package Main", 1), "invalid package name"},
		{"entry", strings.Replace(programSource, "Reduce", "Apply", 1), "entry function must be Reduce"},
		{"arity", strings.Replace(programSource, "value i64, divisor i64", "value i64", 1), "want 2"},
		{"duplicate", strings.Replace(programSource, "divisor i64", "value i64", 1), "duplicate parameter"},
		{"reserved parameter", strings.Replace(programSource, "value i64", "return i64", 1), "invalid parameter name"},
		{"parameter type", strings.Replace(programSource, "divisor i64", "divisor bool", 1), "want i64"},
		{"result type", strings.Replace(programSource, ") i64 {", ") bool {", 1), "result type"},
		{"unknown", strings.Replace(programSource, "1 / value", "1 / missing", 1), "unknown binding"},
		{"integer", strings.Replace(programSource, "+ 2", "+ 999999999999999999999999", 1), "outside i64"},
		{"tokens", tokenHeavy, "token count"},
		{"nodes", nodeHeavy, "expression nodes"},
		{"oversized", strings.Repeat(" ", 8193), "source bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, firstDiagnostics := compiler.Compile(test.source)
			second, secondDiagnostics := compiler.Compile(test.source)
			if !first.IsZero() || !second.IsZero() || len(firstDiagnostics) == 0 || !reflect.DeepEqual(firstDiagnostics, secondDiagnostics) {
				t.Fatalf("Compile diagnostics=%v/%v programs=%#v/%#v", firstDiagnostics, secondDiagnostics, first, second)
			}
			joined := make([]string, len(firstDiagnostics))
			for index, diagnostic := range firstDiagnostics {
				joined[index] = diagnostic.String()
			}
			if !strings.Contains(strings.Join(joined, "\n"), test.want) {
				t.Fatalf("diagnostics %q do not contain %q", joined, test.want)
			}
		})
	}
}

func sourceWithExpression(expression string) string {
	return "package score\nfunc Reduce(value i64, divisor i64) i64 { return " + expression + " }\n"
}

func TestZeroProgramFailsClosed(t *testing.T) {
	var program compiler.Program
	if !program.IsZero() || program.PackageName() != "" || program.Identity() != [32]byte{} {
		t.Fatalf("zero Program=%#v", program)
	}
	if _, err := program.Evaluate(context.Background(), 1, 1, 1); err == nil || !strings.Contains(err.Error(), "zero program") {
		t.Fatalf("zero Evaluate=%v", err)
	}
	if set, err := program.PreparedSource(); err == nil || !strings.Contains(err.Error(), "zero program") || !set.IsZero() {
		t.Fatalf("zero PreparedSource=%#v,%v", set, err)
	}
}

func hexIdentity(identity [32]byte) string {
	const digits = "0123456789abcdef"
	buffer := make([]byte, 64)
	for index, value := range identity {
		buffer[index*2] = digits[value>>4]
		buffer[index*2+1] = digits[value&15]
	}
	return string(buffer)
}
