package sprigproof_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/sprigproof"
	"github.com/besmpl/ember/internal/sprigproof/generated"
)

func proofProgram(t testing.TB) sprigproof.Program {
	t.Helper()
	program, diagnostics := sprigproof.Compile(sprigproof.ProofSource)
	if len(diagnostics) != 0 {
		t.Fatalf("compile proof: %v", diagnostics)
	}
	return program
}

func TestEvaluatorAndExactGeneratedAgreement(t *testing.T) {
	program := proofProgram(t)
	tests := []struct {
		name  string
		input []sprigproof.Reading
		limit uint64
		want  sprigproof.Result
	}{
		{"empty", nil, 1, sprigproof.Result{Tag: sprigproof.ResultOK}},
		{"reduce and reject", []sprigproof.Reading{{Value: 12, Divisor: 3}, {Value: 99, Divisor: 0}, {Value: -9, Divisor: 2}}, 4, sprigproof.Result{Tag: sprigproof.ResultOK, Total: 0, Rejected: []int64{7}}},
		{"division overflow", []sprigproof.Reading{{Value: math.MinInt64, Divisor: -1}}, 2, sprigproof.Result{Tag: sprigproof.ResultError, Code: sprigproof.DomainOverflow}},
		{"addition overflow", []sprigproof.Reading{{Value: math.MaxInt64, Divisor: 1}, {Value: 1, Divisor: 1}}, 3, sprigproof.Result{Tag: sprigproof.ResultError, Code: sprigproof.DomainOverflow}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := append([]sprigproof.Reading(nil), test.input...)
			before := append([]sprigproof.Reading(nil), input...)
			got, err := program.Reduce(context.Background(), input, test.limit)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("evaluator=%#v want %#v", got, test.want)
			}
			generatedInput := make([]generated.Reading, len(input))
			for i, r := range input {
				generatedInput[i] = generated.Reading{Value: r.Value, Divisor: r.Divisor}
			}
			generatedBefore := append([]generated.Reading(nil), generatedInput...)
			native, nativeErr := generated.Reduce(context.Background(), generatedInput, test.limit)
			if nativeErr != nil {
				t.Fatal(nativeErr)
			}
			if !reflect.DeepEqual(resultProjection(native), got) {
				t.Fatalf("generated=%#v evaluator=%#v", native, got)
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatalf("evaluator mutated input: %#v", input)
			}
			if !slices.Equal(generatedInput, generatedBefore) {
				t.Fatalf("generated mutated input: %#v", generatedInput)
			}
		})
	}
}

func resultProjection(result generated.Result) sprigproof.Result {
	return sprigproof.Result{Tag: sprigproof.ResultTag(result.Tag), Total: result.Total, Rejected: append([]int64(nil), result.Rejected...), Code: sprigproof.DomainCode(result.Code)}
}

func TestCancellationLimitsDetachmentAndConcurrency(t *testing.T) {
	program := proofProgram(t)
	input := []sprigproof.Reading{{Value: 6, Divisor: 2}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := program.Reduce(ctx, input, 0); err != context.Canceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("evaluator cancellation=%v", err)
	}
	if _, err := generated.Reduce(ctx, []generated.Reading{{Value: 6, Divisor: 2}}, 0); err != context.Canceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("generated cancellation=%v", err)
	}
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer deadlineCancel()
	if _, err := program.Reduce(deadlineCtx, nil, 1); err != context.DeadlineExceeded {
		t.Fatalf("evaluator deadline=%v", err)
	}
	if _, err := generated.Reduce(deadlineCtx, nil, 1); err != context.DeadlineExceeded {
		t.Fatalf("generated deadline=%v", err)
	}
	if _, err := program.Reduce(context.Background(), input, 1); !errors.Is(err, sprigproof.ErrStepLimit) {
		t.Fatalf("evaluator step error=%v", err)
	}
	if _, err := program.Reduce(context.Background(), nil, 0); !errors.Is(err, sprigproof.ErrStepLimit) {
		t.Fatalf("evaluator entry poll=%v", err)
	}
	if got, err := program.Reduce(context.Background(), nil, 1); err != nil || got.Tag != sprigproof.ResultOK {
		t.Fatalf("evaluator empty limit boundary=%#v,%v", got, err)
	}
	if _, err := generated.Reduce(context.Background(), nil, 0); !errors.Is(err, generated.ErrStepLimit) {
		t.Fatalf("generated entry poll=%v", err)
	}
	if got, err := generated.Reduce(context.Background(), nil, 1); err != nil || got.Tag != generated.ResultOK {
		t.Fatalf("generated empty limit boundary=%#v,%v", got, err)
	}
	if _, err := generated.Reduce(context.Background(), []generated.Reading{{Value: 6, Divisor: 2}}, 1); !errors.Is(err, generated.ErrStepLimit) {
		t.Fatalf("generated step error=%v", err)
	}
	if got, err := program.Reduce(context.Background(), input, 2); err != nil || got.Total != 3 {
		t.Fatalf("independent evaluator call=%#v,%v", got, err)
	}
	first, _ := program.Reduce(context.Background(), []sprigproof.Reading{{Value: 1, Divisor: 0}}, 2)
	first.Rejected[0] = 99
	second, _ := program.Reduce(context.Background(), []sprigproof.Reading{{Value: 1, Divisor: 0}}, 2)
	if second.Rejected[0] != 7 {
		t.Fatalf("evaluator output alias=%v", second.Rejected)
	}
	nativeFirst, _ := generated.Reduce(context.Background(), []generated.Reading{{Value: 1, Divisor: 0}}, 2)
	nativeFirst.Rejected[0] = 99
	nativeSecond, _ := generated.Reduce(context.Background(), []generated.Reading{{Value: 1, Divisor: 0}}, 2)
	if nativeSecond.Rejected[0] != 7 {
		t.Fatalf("generated output alias=%v", nativeSecond.Rejected)
	}
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := program.Reduce(context.Background(), input, 2)
			if err != nil {
				errs <- err
			} else if got.Total != 3 {
				errs <- errors.New("wrong concurrent result")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var nativeWG sync.WaitGroup
	nativeErrs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		nativeWG.Add(1)
		go func() {
			defer nativeWG.Done()
			got, err := generated.Reduce(context.Background(), []generated.Reading{{Value: 6, Divisor: 2}}, 2)
			if err != nil {
				nativeErrs <- err
			} else if got.Total != 3 {
				nativeErrs <- errors.New("wrong concurrent generated result")
			}
		}()
	}
	nativeWG.Wait()
	close(nativeErrs)
	for err := range nativeErrs {
		t.Fatal(err)
	}
}

func TestBoundaryErrorsAgree(t *testing.T) {
	program := proofProgram(t)
	input := make([]sprigproof.Reading, 4097)
	_, evalErr := program.Reduce(context.Background(), input, 1)
	nativeInput := make([]generated.Reading, len(input))
	_, nativeErr := generated.Reduce(context.Background(), nativeInput, 1)
	if evalErr == nil || nativeErr == nil || evalErr.Error() != nativeErr.Error() {
		t.Fatalf("oversize errors evaluator/generated=%v/%v", evalErr, nativeErr)
	}
	_, evalErr = program.Reduce(context.Background(), []sprigproof.Reading{{Value: 1, Divisor: 1}, {Value: 1, Divisor: 1}}, 2)
	_, nativeErr = generated.Reduce(context.Background(), []generated.Reading{{Value: 1, Divisor: 1}, {Value: 1, Divisor: 1}}, 2)
	if !errors.Is(evalErr, sprigproof.ErrStepLimit) || !errors.Is(nativeErr, generated.ErrStepLimit) || evalErr.Error() != nativeErr.Error() {
		t.Fatalf("backedge errors evaluator/generated=%v/%v", evalErr, nativeErr)
	}
}

func TestMaximumInputAndOutputListBound(t *testing.T) {
	program := proofProgram(t)
	input := make([]sprigproof.Reading, 4096)
	nativeInput := make([]generated.Reading, len(input))
	got, err := program.Reduce(context.Background(), input, 4097)
	if err != nil || len(got.Rejected) != 4096 {
		t.Fatalf("evaluator maximum=%d,%v", len(got.Rejected), err)
	}
	native, nativeErr := generated.Reduce(context.Background(), nativeInput, 4097)
	if nativeErr != nil || len(native.Rejected) != 4096 {
		t.Fatalf("generated maximum=%d,%v", len(native.Rejected), nativeErr)
	}
}

func TestAlternateAcceptedSourceChangesEvaluatorAndEmitter(t *testing.T) {
	alternate := strings.ReplaceAll(sprigproof.ProofSource, "let total = 0", "let total = 10")
	alternate = strings.ReplaceAll(alternate, "code: 7", "code: 9")
	program, diagnostics := sprigproof.Compile(alternate)
	if len(diagnostics) != 0 {
		t.Fatalf("alternate diagnostics=%v", diagnostics)
	}
	got, err := program.Reduce(context.Background(), []sprigproof.Reading{{Value: 6, Divisor: 2}, {Value: 1, Divisor: 0}}, 3)
	if err != nil || !reflect.DeepEqual(got, sprigproof.Result{Tag: sprigproof.ResultOK, Total: 13, Rejected: []int64{9}}) {
		t.Fatalf("alternate result=%#v,%v", got, err)
	}
	set, err := program.PreparedSource("alternate")
	if err != nil {
		t.Fatal(err)
	}
	source := set.Files()[0].Content
	if !strings.Contains(source, "int64 = 10") || !strings.Contains(source, "code: 9") || strings.Contains(source, "var total") {
		t.Fatalf("alternate generated source did not change:\n%s", source)
	}
}

func TestDirectDivisionByZeroIsDomainResult(t *testing.T) {
	source := strings.Replace(sprigproof.ProofSource, "reading.divisor == 0", "reading.divisor == 99", 1)
	program, diagnostics := sprigproof.Compile(source)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%v", diagnostics)
	}
	got, err := program.Reduce(context.Background(), []sprigproof.Reading{{Value: 1, Divisor: 0}}, 2)
	if err != nil || got.Tag != sprigproof.ResultError || got.Code != sprigproof.DomainDivideByZero {
		t.Fatalf("division zero=%#v,%v", got, err)
	}
	set, err := program.PreparedSource("dividezero")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(set.Files()[0].Content, "Code: DomainDivideByZero") {
		t.Fatal("generated source omitted division-by-zero domain result")
	}
}

func TestGeneratedFixtureIsFreshAndStdlibOnly(t *testing.T) {
	program := proofProgram(t)
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile("generated/reduce_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("EMBER_UPDATE_SPRIG_FIXTURE") != "" {
		if err := os.WriteFile("generated/reduce_generated.go", []byte(set.Files()[0].Content), 0o644); err != nil {
			t.Fatal(err)
		}
		content = []byte(set.Files()[0].Content)
	}
	if string(content) != set.Files()[0].Content {
		t.Fatal("generated fixture is stale")
	}
	t.Logf("Set digest=%x generated SHA-256=%x", set.Digest(), sha256.Sum256(content))
}

func TestPreparedSourceRejectsInvalidGoPackageName(t *testing.T) {
	program := proofProgram(t)
	for _, name := range []string{"type", "not-a-package"} {
		if _, err := program.PreparedSource(name); err == nil || !strings.Contains(err.Error(), "invalid generated Go package name") {
			t.Fatalf("PreparedSource(%q) error=%v", name, err)
		}
	}
}
