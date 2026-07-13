package ember

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCompileLimitsExactBoundaries(t *testing.T) {
	const source = "return 1"
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource() error = %v", err)
	}

	tests := []struct {
		name   string
		limits CompileLimits
		want   LimitKind
	}{
		{name: "source bytes", limits: CompileLimits{MaxSourceBytes: uint64(len(source) - 1)}, want: LimitSourceBytes},
		{name: "tokens", limits: CompileLimits{MaxTokens: 1}, want: LimitTokens},
		{name: "syntax nodes", limits: CompileLimits{MaxSyntaxNodes: uint64(artifact.program.nodeCount - 1)}, want: LimitSyntaxNodes},
	}
	for _, test := range tests {
		t.Run(test.name+" rejects", func(t *testing.T) {
			_, err := CompileWithOptions(source, CompileOptions{Limits: test.limits})
			assertCompileLimitKind(t, err, test.want)
		})
	}

	if _, err := CompileWithOptions(source, CompileOptions{Limits: CompileLimits{
		MaxSourceBytes: uint64(len(source)),
		MaxTokens:      artifact.metrics.tokens,
		MaxSyntaxNodes: uint64(artifact.program.nodeCount),
	}}); err != nil {
		t.Fatalf("exact compile limits rejected valid source: %v", err)
	}
}

func TestCompileLimitsDeepNestingReturnsTypedError(t *testing.T) {
	const depth = 10000
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "parentheses", source: "return " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth)},
		{name: "unary", source: "return " + strings.Repeat("- ", depth) + "1"},
		{name: "type", source: "type T = " + strings.Repeat("(", depth) + "number" + strings.Repeat(")", depth) + "\nreturn 1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileWithOptions(test.source, CompileOptions{Limits: CompileLimits{MaxNesting: 64}})
			assertCompileLimitKind(t, err, LimitNesting)
		})
	}
}

func TestCompileLimitsGuardNestedParserConstructs(t *testing.T) {
	tests := []struct {
		name    string
		shallow string
		deep    string
	}{
		{name: "tables and calls", shallow: "return f(1)", deep: "return f({value = f({value = 1})})"},
		{name: "function bodies", shallow: "local function f() return 1 end return f()", deep: "local function f() local function g() return 1 end return g() end return f()"},
		{name: "elseif chain", shallow: "if true then return 1 else return 2 end", deep: "if false then return 1 elseif false then return 2 elseif true then return 3 else return 4 end"},
		{name: "if expression chain", shallow: "return if true then 1 else 2", deep: "return if false then 1 elseif false then 2 elseif true then 3 else 4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shallow, err := parseSource(Source{Text: test.shallow})
			if err != nil {
				t.Fatalf("shallow parse failed: %v", err)
			}
			if _, err := CompileWithOptions(test.shallow, CompileOptions{Limits: CompileLimits{MaxNesting: shallow.metrics.nesting}}); err != nil {
				t.Fatalf("shallow source rejected at observed nesting %d: %v", shallow.metrics.nesting, err)
			}
			_, err = CompileWithOptions(test.deep, CompileOptions{Limits: CompileLimits{MaxNesting: shallow.metrics.nesting}})
			assertCompileLimitKind(t, err, LimitNesting)
		})
	}
}

func TestCompileUnlimitedCompatibility(t *testing.T) {
	if _, err := Compile("return 1 + 2"); err != nil {
		t.Fatalf("unlimited Compile returned error: %v", err)
	}
}

func TestCompileTokenLimitCountsBeforeAppend(t *testing.T) {
	_, err := CompileWithOptions("return 1 + 2", CompileOptions{Limits: CompileLimits{MaxTokens: 3}})
	assertCompileLimitKind(t, err, LimitTokens)
}

func TestCompileSyntaxNodeLimitReportsCrossing(t *testing.T) {
	const source = "return 1 + 2"
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource() error = %v", err)
	}
	_, err = CompileWithOptions(source, CompileOptions{Limits: CompileLimits{MaxSyntaxNodes: uint64(artifact.program.nodeCount - 1)}})
	assertCompileLimitKind(t, err, LimitSyntaxNodes)
	if artifact.program.nodeCount <= 1 {
		t.Fatalf("fixture node count = %d, want multiple nodes", artifact.program.nodeCount)
	}
}

func TestSourceArtifactCacheValidatesStricterLimits(t *testing.T) {
	tests := []struct {
		name   string
		source string
		limits CompileLimits
		want   LimitKind
	}{
		{name: "source bytes", source: "return 1", limits: CompileLimits{MaxSourceBytes: 7}, want: LimitSourceBytes},
		{name: "tokens", source: "return 1 + 2", limits: CompileLimits{MaxTokens: 3}, want: LimitTokens},
		{name: "nesting", source: "return ((((1))))", limits: CompileLimits{MaxNesting: 2}, want: LimitNesting},
		{name: "syntax nodes", source: "return 1 + 2", limits: CompileLimits{MaxSyntaxNodes: 1}, want: LimitSyntaxNodes},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := Source{Name: "logical:limits/" + test.name, Text: test.source}
			identity := identifyModuleSource(source)
			store := newSourceArtifactStore()
			if _, err := store.parse(source, identity); err != nil {
				t.Fatalf("unlimited cache fill failed: %v", err)
			}
			_, err := store.parseWithLimits(source, identity, test.limits)
			assertCompileLimitKind(t, err, test.want)
		})
	}
}

func TestSourceArtifactPreparationRetriesLimitErrorsForDifferentLimits(t *testing.T) {
	strict := CompileLimits{MaxTokens: 3}
	preparation := sourceArtifactPreparation{
		limits: strict,
		err:    &LimitError{Kind: LimitTokens, Limit: 3, Used: 4},
	}
	if preparation.retryFor(strict) {
		t.Fatal("same-limit waiter should share the preparation error")
	}
	if !preparation.retryFor(CompileLimits{MaxTokens: 100}) {
		t.Fatal("broader-limit waiter should retry under its own limits")
	}
	preparation.err = fmt.Errorf("parse failed")
	if preparation.retryFor(CompileLimits{MaxTokens: 100}) {
		t.Fatal("ordinary parse errors should be shared across waiters")
	}
}

func assertCompileLimitKind(t *testing.T, err error, want LimitKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s limit", want)
	}
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("error = %v, want ErrLimitExceeded", err)
	}
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want *LimitError", err)
	}
	if limitErr.Kind != want {
		t.Fatalf("limit kind = %q, want %q (%v)", limitErr.Kind, want, fmt.Sprintf("%#v", limitErr))
	}
}
