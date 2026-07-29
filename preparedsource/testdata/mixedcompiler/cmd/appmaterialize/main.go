// Command appmaterialize is application-owned compile tooling. It combines
// two concrete checked language compilers through preparedsource values, owns
// final placement and call order, and emits a dependency-free static app.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	seedcompiler "example.com/ember-external-compiler"
	"github.com/besmpl/ember/internal/sprigproof"
	"github.com/besmpl/ember/preparedsource"
)

const applicationHandler = `package main

import (
	"context"
	"fmt"

	counter "example.com/mixed-compiled-application/generated/counter"
	score "example.com/mixed-compiled-application/generated/score"
)

type Limits struct {
	Sprig uint64
	Seed  uint64
}

type Request struct {
	Readings    []counter.Reading
	SeedValue   int64
	SeedDivisor int64
}

type Stage uint8

const (
	StageSprigDomain Stage = iota + 1
	StageSeedDomain
	StageComplete
)

type Decision struct {
	Stage Stage
	Sprig counter.Result
	Seed  score.Result
}

func Apply(ctx context.Context, limits Limits, request Request) (Decision, error) {
	sprigResult, err := counter.Reduce(ctx, request.Readings, limits.Sprig)
	if err != nil {
		return Decision{}, fmt.Errorf("mixed: sprig: %w", err)
	}
	if sprigResult.Tag == counter.ResultError {
		return Decision{Stage: StageSprigDomain, Sprig: sprigResult}, nil
	}
	seedResult, err := score.Reduce(ctx, request.SeedValue, request.SeedDivisor, limits.Seed)
	if err != nil {
		return Decision{}, fmt.Errorf("mixed: seed: %w", err)
	}
	if seedResult.Code != 0 {
		return Decision{Stage: StageSeedDomain, Sprig: sprigResult, Seed: seedResult}, nil
	}
	return Decision{Stage: StageComplete, Sprig: sprigResult, Seed: seedResult}, nil
}
`

const applicationMain = `package main

import (
	"context"
	"errors"
	"fmt"
	"math"

	counter "example.com/mixed-compiled-application/generated/counter"
	score "example.com/mixed-compiled-application/generated/score"
)

func result() string {
	good, goodErr := Apply(context.Background(), Limits{Sprig: 2, Seed: 1}, Request{
		Readings: []counter.Reading{{Value: 12, Divisor: 3}}, SeedValue: 12, SeedDivisor: 3,
	})
	sprigDomain, sprigDomainErr := Apply(context.Background(), Limits{Sprig: 2, Seed: 1}, Request{
		Readings: []counter.Reading{{Value: math.MinInt64, Divisor: -1}}, SeedValue: 12, SeedDivisor: 3,
	})
	seedDomain, seedDomainErr := Apply(context.Background(), Limits{Sprig: 2, Seed: 1}, Request{
		Readings: []counter.Reading{{Value: 12, Divisor: 3}}, SeedValue: 0, SeedDivisor: 1,
	})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, cancelErr := Apply(canceled, Limits{}, Request{})
	_, sprigLimitErr := Apply(context.Background(), Limits{Seed: 1}, Request{})
	_, seedLimitErr := Apply(context.Background(), Limits{Sprig: 2}, Request{
		Readings: []counter.Reading{{Value: 12, Divisor: 3}}, SeedValue: 12, SeedDivisor: 3,
	})
	if goodErr != nil || sprigDomainErr != nil || seedDomainErr != nil {
		panic("unexpected mixed execution error")
	}
	return fmt.Sprintf("%d:%d:%d|%d:%d:%d|%d:%d|%t:%t:%t",
		good.Stage, good.Sprig.Total, good.Seed.Value,
		sprigDomain.Stage, sprigDomain.Sprig.Code, sprigDomain.Seed.Code,
		seedDomain.Stage, seedDomain.Seed.Code,
		errors.Is(cancelErr, context.Canceled),
		errors.Is(sprigLimitErr, counter.ErrStepLimit),
		errors.Is(seedLimitErr, score.ErrStepLimit),
	)
}

func main() { fmt.Println(result()) }
`

const applicationTest = `package main

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	counter "example.com/mixed-compiled-application/generated/counter"
	score "example.com/mixed-compiled-application/generated/score"
)

type observedContext struct {
	context.Context
	calls      int
	cancelAt   int
	panicAfter int
}

func (c *observedContext) Err() error {
	c.calls++
	if c.panicAfter != 0 && c.calls > c.panicAfter {
		panic("later language was entered")
	}
	if c.cancelAt != 0 && c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func positiveRequest() Request {
	return Request{
		Readings: []counter.Reading{{Value: 12, Divisor: 3}},
		SeedValue: 12, SeedDivisor: 3,
	}
}

func TestMixedSemanticsOrderCancellationAndLimits(t *testing.T) {
	goodContext := &observedContext{Context: context.Background()}
	good, err := Apply(goodContext, Limits{Sprig: 2, Seed: 1}, positiveRequest())
	if err != nil || good.Stage != StageComplete || good.Sprig.Total != 4 || good.Seed.Value != 6 || goodContext.calls != 3 {
		t.Fatalf("positive = %#v, %v, calls=%d", good, err, goodContext.calls)
	}

	orderContext := &observedContext{Context: context.Background(), panicAfter: 1}
	sprigDomain, err := Apply(orderContext, Limits{Sprig: 2, Seed: 1}, Request{
		Readings: []counter.Reading{{Value: math.MinInt64, Divisor: -1}},
		SeedValue: 12, SeedDivisor: 3,
	})
	if err != nil || sprigDomain.Stage != StageSprigDomain || sprigDomain.Sprig.Code != counter.DomainOverflow || sprigDomain.Seed != (score.Result{}) || orderContext.calls != 1 {
		t.Fatalf("Sprig domain = %#v, %v, calls=%d", sprigDomain, err, orderContext.calls)
	}

	seedDomainContext := &observedContext{Context: context.Background()}
	seedDomain, err := Apply(seedDomainContext, Limits{Sprig: 2, Seed: 1}, Request{
		Readings: []counter.Reading{{Value: 12, Divisor: 3}},
		SeedValue: 0, SeedDivisor: 1,
	})
	if err != nil || seedDomain.Stage != StageSeedDomain || seedDomain.Sprig.Total != 4 || seedDomain.Seed.Code != score.DomainDivideByZero || seedDomainContext.calls != 3 {
		t.Fatalf("Seed domain = %#v, %v, calls=%d", seedDomain, err, seedDomainContext.calls)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Apply(canceled, Limits{}, positiveRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled error = %v", err)
	}
	sprigLimitContext := &observedContext{Context: context.Background(), panicAfter: 1}
	if _, err := Apply(sprigLimitContext, Limits{Seed: 1}, positiveRequest()); !errors.Is(err, counter.ErrStepLimit) || sprigLimitContext.calls != 1 {
		t.Fatalf("Sprig limit error = %v, calls=%d", err, sprigLimitContext.calls)
	}
	seedLimitContext := &observedContext{Context: context.Background()}
	if _, err := Apply(seedLimitContext, Limits{Sprig: 2}, positiveRequest()); !errors.Is(err, score.ErrStepLimit) || seedLimitContext.calls != 3 {
		t.Fatalf("Seed limit error = %v, calls=%d", err, seedLimitContext.calls)
	}
	seedCancelContext := &observedContext{Context: context.Background(), cancelAt: 3}
	if _, err := Apply(seedCancelContext, Limits{Sprig: 2, Seed: 1}, positiveRequest()); !errors.Is(err, context.Canceled) || seedCancelContext.calls != 3 {
		t.Fatalf("Seed cancellation = %v, calls=%d", err, seedCancelContext.calls)
	}
	if _, err := Apply(nil, Limits{Sprig: 2, Seed: 1}, positiveRequest()); err == nil || !strings.Contains(err.Error(), "sprig: nil context") {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestMixedValuesAreDetachedAndPositivePathAllocatesNothing(t *testing.T) {
	readings := []counter.Reading{{Value: 12, Divisor: 0}}
	decision, err := Apply(context.Background(), Limits{Sprig: 2, Seed: 1}, Request{
		Readings: readings, SeedValue: 12, SeedDivisor: 3,
	})
	if err != nil || decision.Stage != StageComplete || !slices.Equal(decision.Sprig.Rejected, []int64{7}) || decision.Seed.Value != 6 {
		t.Fatalf("detached decision = %#v, %v", decision, err)
	}
	readings[0] = counter.Reading{Value: 1, Divisor: 1}
	if !slices.Equal(decision.Sprig.Rejected, []int64{7}) {
		t.Fatalf("input mutation changed output: %#v", decision)
	}
	decision.Sprig.Rejected[0] = 99
	again, err := Apply(context.Background(), Limits{Sprig: 2, Seed: 1}, Request{
		Readings: []counter.Reading{{Value: 12, Divisor: 0}}, SeedValue: 12, SeedDivisor: 3,
	})
	if err != nil || !slices.Equal(again.Sprig.Rejected, []int64{7}) {
		t.Fatalf("output mutation escaped = %#v, %v", again, err)
	}

	ctx := context.Background()
	request := positiveRequest()
	limits := Limits{Sprig: 2, Seed: 1}
	if allocations := testing.AllocsPerRun(1000, func() {
		got, err := Apply(ctx, limits, request)
		if err != nil || got.Stage != StageComplete || got.Sprig.Total != 4 || got.Seed.Value != 6 {
			panic("mixed positive result changed")
		}
	}); allocations != 0 {
		t.Fatalf("mixed positive allocations = %v, want 0", allocations)
	}
}

var benchmarkDecision Decision
var benchmarkError error

func BenchmarkMixedHandler(b *testing.B) {
	ctx := context.Background()
	request := positiveRequest()
	limits := Limits{Sprig: 2, Seed: 1}
	b.ReportAllocs()
	for range b.N {
		benchmarkDecision, benchmarkError = Apply(ctx, limits, request)
	}
}

func BenchmarkConcreteCalls(b *testing.B) {
	ctx := context.Background()
	request := positiveRequest()
	b.ReportAllocs()
	for range b.N {
		sprigResult, err := counter.Reduce(ctx, request.Readings, 2)
		if err != nil {
			benchmarkError = err
			continue
		}
		if sprigResult.Tag == counter.ResultError {
			benchmarkDecision = Decision{Stage: StageSprigDomain, Sprig: sprigResult}
			benchmarkError = nil
			continue
		}
		seedResult, err := score.Reduce(ctx, request.SeedValue, request.SeedDivisor, 1)
		if err != nil {
			benchmarkError = err
			continue
		}
		if seedResult.Code != 0 {
			benchmarkDecision = Decision{Stage: StageSeedDomain, Sprig: sprigResult, Seed: seedResult}
			benchmarkError = nil
			continue
		}
		benchmarkDecision = Decision{Stage: StageComplete, Sprig: sprigResult, Seed: seedResult}
		benchmarkError = nil
	}
}
`

func main() {
	out := flag.String("out", "", "fresh application directory to materialize")
	seedPath := flag.String("seed", "", "Seed source file")
	flag.Parse()
	if *out == "" || *seedPath == "" {
		fatal(fmt.Errorf("-out and -seed are required"))
	}

	project, diagnostics := sprigproof.CompileProject(sprigproof.ProofProjectSources())
	if len(diagnostics) != 0 {
		fatal(fmt.Errorf("compile Sprig diagnostics: %v", diagnostics))
	}
	sprigProgram, ok := project.Program("counter")
	if !ok {
		fatal(fmt.Errorf("Sprig counter Reduce is missing"))
	}
	sprigOracle, err := sprigProgram.Reduce(context.Background(), []sprigproof.Reading{{Value: 12, Divisor: 3}}, 2)
	if err != nil || sprigOracle.Tag != sprigproof.ResultOK || sprigOracle.Total != 4 || len(sprigOracle.Rejected) != 0 {
		fatal(fmt.Errorf("Sprig canonical evaluator = %#v, %v", sprigOracle, err))
	}
	sprigDomain, err := sprigProgram.Reduce(context.Background(), []sprigproof.Reading{{Value: math.MinInt64, Divisor: -1}}, 2)
	if err != nil || sprigDomain.Tag != sprigproof.ResultError || sprigDomain.Code != sprigproof.DomainOverflow {
		fatal(fmt.Errorf("Sprig domain evaluator = %#v, %v", sprigDomain, err))
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sprigProgram.Reduce(canceled, nil, 0); !errors.Is(err, context.Canceled) {
		fatal(fmt.Errorf("Sprig cancellation evaluator = %v", err))
	}
	if _, err := sprigProgram.Reduce(context.Background(), nil, 0); !errors.Is(err, sprigproof.ErrStepLimit) {
		fatal(fmt.Errorf("Sprig limit evaluator = %v", err))
	}
	sprigPackages, err := project.PrepareStandalone([]sprigproof.PackageID{"counter"})
	if err != nil || len(sprigPackages) != 1 {
		fatal(fmt.Errorf("prepare Sprig counter = %v, %v", sprigPackages, err))
	}

	seedSource, err := os.ReadFile(*seedPath)
	if err != nil {
		fatal(err)
	}
	seedProgram, seedDiagnostics := seedcompiler.Compile(string(seedSource))
	if len(seedDiagnostics) != 0 {
		fatal(fmt.Errorf("compile Seed diagnostics: %v", seedDiagnostics))
	}
	seedOracle, err := seedProgram.Evaluate(context.Background(), 12, 3, 1)
	if err != nil || seedOracle != (seedcompiler.Result{Value: 6}) {
		fatal(fmt.Errorf("Seed canonical evaluator = %#v, %v", seedOracle, err))
	}
	seedDomain, err := seedProgram.Evaluate(context.Background(), 0, 1, 1)
	if err != nil || seedDomain.Code != seedcompiler.DomainDivideByZero {
		fatal(fmt.Errorf("Seed domain evaluator = %#v, %v", seedDomain, err))
	}
	if _, err := seedProgram.Evaluate(canceled, 12, 3, 0); !errors.Is(err, context.Canceled) {
		fatal(fmt.Errorf("Seed cancellation evaluator = %v", err))
	}
	if _, err := seedProgram.Evaluate(context.Background(), 12, 3, 0); !errors.Is(err, seedcompiler.ErrStepLimit) {
		fatal(fmt.Errorf("Seed limit evaluator = %v", err))
	}
	seedSet, err := seedProgram.PreparedSource()
	if err != nil {
		fatal(err)
	}

	sprigSet := sprigPackages[0].Set()
	mounts := []preparedsource.Mount{
		{Path: "generated/score", Set: seedSet},
		{Path: "generated/counter", Set: sprigSet},
	}
	layout, err := preparedsource.NewLayout(mounts)
	if err != nil {
		fatal(err)
	}
	permuted, err := preparedsource.NewLayout([]preparedsource.Mount{mounts[1], mounts[0]})
	if err != nil || permuted.Digest() != layout.Digest() {
		fatal(fmt.Errorf("layout permutation changed identity: %x/%x, %v", layout.Digest(), permuted.Digest(), err))
	}
	canonical := layout.Mounts()
	if len(canonical) != 2 || canonical[0].Path != "generated/counter" || canonical[1].Path != "generated/score" {
		fatal(fmt.Errorf("canonical mounts = %#v", canonical))
	}

	if err := os.Mkdir(*out, 0o755); err != nil {
		fatal(err)
	}
	for _, mount := range canonical {
		directory := filepath.Join(*out, filepath.FromSlash(mount.Path))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			fatal(err)
		}
		for _, file := range mount.Set.Files() {
			write(filepath.Join(directory, file.Name), file.Content)
		}
	}
	if err := os.MkdirAll(filepath.Join(*out, "cmd", "app"), 0o755); err != nil {
		fatal(err)
	}
	write(filepath.Join(*out, "go.mod"), "module example.com/mixed-compiled-application\n\ngo 1.26\n")
	write(filepath.Join(*out, "cmd", "app", "handler.go"), applicationHandler)
	write(filepath.Join(*out, "cmd", "app", "main.go"), applicationMain)
	write(filepath.Join(*out, "cmd", "app", "handler_test.go"), applicationTest)
	fmt.Printf("sprig_project=%x\nsprig_package=%x\nsprig_set=%x\nseed_program=%x\nseed_set=%x\nlayout=%x\n",
		project.Identity(), sprigPackages[0].Identity(), sprigSet.Digest(),
		seedProgram.Identity(), seedSet.Digest(), layout.Digest())
}

func write(path, content string) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fatal(err)
	}
	written, writeErr := io.WriteString(file, content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	closeErr := file.Close()
	if writeErr != nil {
		fatal(writeErr)
	}
	if closeErr != nil {
		fatal(closeErr)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
