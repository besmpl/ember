// Command appmaterialize is application-owned compile tooling. It invokes one
// concrete external compiler, chooses the final mount, and copies only the
// canonical Layout into a fresh application. Neither the compiler nor
// preparedsource is a dependency of the resulting module.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	compiler "example.com/ember-external-compiler"
	"github.com/besmpl/ember/preparedsource"
)

const applicationMain = `package main

import (
	"context"
	"errors"
	"fmt"
	"math"

	"example.com/external-compiled-application/generated/score"
)

func result() string {
	good, goodErr := score.Reduce(context.Background(), 12, 3, 1)
	left, leftErr := score.Reduce(context.Background(), math.MinInt64, -1, 1)
	right, rightErr := score.Reduce(context.Background(), 0, 1, 1)
	addition, additionErr := score.Reduce(context.Background(), math.MaxInt64, 1, 1)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, cancelErr := score.Reduce(canceled, 12, 3, 0)
	_, limitErr := score.Reduce(context.Background(), 12, 3, 0)
	if goodErr != nil || leftErr != nil || rightErr != nil || additionErr != nil {
		panic("unexpected execution error")
	}
	return fmt.Sprintf("%d|%d|%d|%d|%t|%t", good.Value, left.Code, right.Code, addition.Code, errors.Is(cancelErr, context.Canceled), errors.Is(limitErr, score.ErrStepLimit))
}

func main() { fmt.Println(result()) }
`

const applicationTest = `package main

import (
	"context"
	"errors"
	"math"
	"testing"

	"example.com/external-compiled-application/generated/score"
)

func TestGeneratedSemanticsAndAllocation(t *testing.T) {
	if got := result(); got != "6|1|2|1|true|true" {
		t.Fatalf("result = %q", got)
	}
	ctx := context.Background()
	if allocations := testing.AllocsPerRun(1000, func() {
		got, err := score.Reduce(ctx, 12, 3, 1)
		if err != nil || got != (score.Result{Value: 6}) {
			panic("generated result changed")
		}
	}); allocations != 0 {
		t.Fatalf("generated allocations = %v, want 0", allocations)
	}
}

var benchmarkResult score.Result
var benchmarkError error

func BenchmarkGenerated(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		benchmarkResult, benchmarkError = score.Reduce(ctx, 12, 3, 1)
	}
}

func BenchmarkHandwritten(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		benchmarkResult, benchmarkError = handwritten(ctx, 12, 3, 1)
	}
}

func handwritten(ctx context.Context, value, divisor int64, limit uint64) (score.Result, error) {
	if ctx == nil {
		return score.Result{}, errors.New("seed: nil context")
	}
	if err := ctx.Err(); err != nil {
		return score.Result{}, err
	}
	if limit == 0 {
		return score.Result{}, score.ErrStepLimit
	}
	if divisor == 0 {
		return score.Result{Code: score.DomainDivideByZero}, nil
	}
	if value == math.MinInt64 && divisor == -1 {
		return score.Result{Code: score.DomainOverflow}, nil
	}
	quotient := value / divisor
	if value == 0 {
		return score.Result{Code: score.DomainDivideByZero}, nil
	}
	second := int64(1) / value
	firstSum, overflow := handwrittenAdd(quotient, second)
	if overflow {
		return score.Result{Code: score.DomainOverflow}, nil
	}
	final, overflow := handwrittenAdd(firstSum, 2)
	if overflow {
		return score.Result{Code: score.DomainOverflow}, nil
	}
	return score.Result{Value: final}, nil
}

func handwrittenAdd(left, right int64) (int64, bool) {
	sum := left + right
	return sum, right > 0 && sum < left || right < 0 && sum > left
}
`

func main() {
	out := flag.String("out", "", "fresh application directory to materialize")
	sourcePath := flag.String("source", "program.seed", "Seed source file")
	flag.Parse()
	if *out == "" {
		fatal(fmt.Errorf("-out is required"))
	}
	source, err := os.ReadFile(*sourcePath)
	if err != nil {
		fatal(err)
	}
	program, diagnostics := compiler.Compile(string(source))
	if len(diagnostics) != 0 {
		fatal(fmt.Errorf("compile diagnostics: %v", diagnostics))
	}
	oracle, err := program.Evaluate(context.Background(), 12, 3, 1)
	if err != nil || oracle != (compiler.Result{Value: 6}) {
		fatal(fmt.Errorf("canonical evaluator = %#v, %v", oracle, err))
	}
	set, err := program.PreparedSource()
	if err != nil {
		fatal(err)
	}
	layout, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "generated/score", Set: set}})
	if err != nil {
		fatal(err)
	}
	if err := os.Mkdir(*out, 0o755); err != nil {
		fatal(err)
	}
	for _, mount := range layout.Mounts() {
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
	write(filepath.Join(*out, "go.mod"), "module example.com/external-compiled-application\n\ngo 1.26\n")
	write(filepath.Join(*out, "cmd", "app", "main.go"), applicationMain)
	write(filepath.Join(*out, "cmd", "app", "main_test.go"), applicationTest)
	fmt.Printf("program=%x\nset=%x\nlayout=%x\n", program.Identity(), set.Digest(), layout.Digest())
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
