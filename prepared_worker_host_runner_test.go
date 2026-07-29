package ember_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember"
	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/internal/preparedworkerprobe"
	"github.com/besmpl/ember/preparedworker"
	"github.com/besmpl/ember/preparedworkerbuild"
)

type preparedWorkerTurnTransactor interface {
	Transact(context.Context, preparedworkerfixture.TurnRequest) (preparedworkerfixture.TurnResult, error)
}

type preparedWorkerHostPublication struct {
	build     preparedworker.Build
	published workerartifact.Published
}

// preparedWorkerHostRunner is the only adapter between the fixture's rich-game
// record and the production Runner API. Both embedded release execution and
// supervised development execution use it, so admission evidence cannot drift
// onto a benchmark-only protocol.
type preparedWorkerHostRunner struct {
	runner     preparedworker.Runner[preparedworkerfixture.TurnRequest, preparedworkerfixture.TurnResult]
	reload     preparedworker.Reload
	position   preparedworker.Position
	executable string
}

func (runner *preparedWorkerHostRunner) Transact(
	ctx context.Context,
	request preparedworkerfixture.TurnRequest,
) (preparedworkerfixture.TurnResult, error) {
	if runner == nil || runner.runner == nil {
		return preparedworkerfixture.TurnResult{}, fmt.Errorf("prepared worker host runner: closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	completion, err := runner.runner.Apply(ctx, preparedworker.Operation[preparedworkerfixture.TurnRequest]{
		Sequence:     request.Sequence,
		BaseRevision: request.Revision,
		Request:      request,
	})
	if err != nil {
		return preparedworkerfixture.TurnResult{}, err
	}
	if completion.Position.Sequence != request.Sequence ||
		completion.Position.Revision != completion.Result.Revision {
		return preparedworkerfixture.TurnResult{}, fmt.Errorf(
			"prepared worker host runner: completion position %#v differs from result sequence/revision %d/%d",
			completion.Position,
			completion.Result.Sequence,
			completion.Result.Revision,
		)
	}
	runner.position = completion.Position
	return completion.Result, nil
}

func (runner *preparedWorkerHostRunner) Close() error {
	if runner == nil || runner.runner == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := runner.runner.Close(ctx)
	if err == nil {
		runner.runner = nil
	}
	return err
}

func openPreparedWorkerEmbeddedRunner(t testing.TB, name string) *preparedWorkerHostRunner {
	t.Helper()
	runner, err := preparedworker.OpenEmbedded(
		context.Background(),
		preparedWorkerHostRunnerOptions(t, name),
		preparedworkerprobe.NewTransactionHandler,
	)
	if err != nil {
		t.Fatal(err)
	}
	result := &preparedWorkerHostRunner{runner: runner}
	t.Cleanup(func() {
		if err := result.Close(); err != nil {
			t.Error(err)
		}
	})
	return result
}

func openPreparedWorkerProcessRunner(
	t testing.TB,
	publication preparedWorkerHostPublication,
	name string,
) *preparedWorkerHostRunner {
	t.Helper()
	runner, reload, err := preparedworker.OpenDevelopment(
		context.Background(),
		preparedWorkerHostRunnerOptions(t, name),
		publication.build,
	)
	if err != nil {
		t.Fatal(err)
	}
	result := &preparedWorkerHostRunner{
		runner: runner, reload: reload, executable: publication.published.Executable,
	}
	t.Cleanup(func() {
		if err := result.Close(); err != nil {
			t.Error(err)
		}
	})
	return result
}

func (runner *preparedWorkerHostRunner) Reactivate(
	ctx context.Context,
	publication preparedWorkerHostPublication,
) error {
	if runner == nil || runner.reload == nil {
		return fmt.Errorf("prepared worker host runner: reload unavailable")
	}
	candidate, err := runner.reload.Prepare(ctx, publication.build)
	if err != nil {
		return err
	}
	return candidate.Activate()
}

func (runner *preparedWorkerHostRunner) ProcessID(t testing.TB) int {
	t.Helper()
	if runner == nil || runner.executable == "" {
		t.Fatal("prepared worker host runner has no supervised executable")
	}
	pid, err := findPreparedWorkerChildPID(os.Getpid(), runner.executable)
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func findPreparedWorkerChildPID(parentPID int, executable string) (int, error) {
	if parentPID <= 0 || executable == "" {
		return 0, fmt.Errorf("prepared worker process inventory: invalid parent or executable")
	}
	command := exec.Command("ps", "-axo", "pid=,ppid=,args=")
	output, err := command.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("prepared worker process inventory: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parsePreparedWorkerChildPID(string(output), parentPID, executable)
}

func parsePreparedWorkerChildPID(output string, parentPID int, executable string) (int, error) {
	if parentPID <= 0 || executable == "" {
		return 0, fmt.Errorf("prepared worker process inventory: invalid parent or executable")
	}
	matched := 0
	for lineNumber, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pidText, remainder, ok := cutPreparedWorkerProcessField(line)
		if !ok {
			return 0, fmt.Errorf("prepared worker process inventory: malformed row %d", lineNumber+1)
		}
		parentText, command, ok := cutPreparedWorkerProcessField(remainder)
		if !ok || command == "" {
			return 0, fmt.Errorf("prepared worker process inventory: malformed row %d", lineNumber+1)
		}
		pid, err := strconv.Atoi(pidText)
		if err != nil || pid <= 0 {
			return 0, fmt.Errorf("prepared worker process inventory: invalid PID in row %d", lineNumber+1)
		}
		parent, err := strconv.Atoi(parentText)
		if err != nil || parent < 0 {
			return 0, fmt.Errorf("prepared worker process inventory: invalid parent PID in row %d", lineNumber+1)
		}
		if parent != parentPID || command != executable {
			continue
		}
		if matched != 0 {
			return 0, fmt.Errorf("prepared worker process inventory: multiple exact owned workers")
		}
		matched = pid
	}
	if matched == 0 {
		return 0, fmt.Errorf("prepared worker process inventory: exact owned worker not found")
	}
	return matched, nil
}

func cutPreparedWorkerProcessField(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	index := strings.IndexAny(line, " \t")
	if index <= 0 {
		return "", "", false
	}
	return line[:index], strings.TrimSpace(line[index:]), true
}

func preparedWorkerHostRunnerOptions(
	t testing.TB,
	name string,
) preparedworker.Options[
	preparedworkerfixture.TurnRequest,
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
] {
	t.Helper()
	return preparedworker.Options[
		preparedworkerfixture.TurnRequest,
		preparedworkerfixture.TurnResult,
		preparedworkerfixture.Checkpoint,
		preparedworkerfixture.Effect,
	]{
		Stream:    "rich-game-host-admission",
		Contract:  preparedworkerprobe.TransactionRunnerContract(),
		StatePath: filepath.Join(t.TempDir(), name+".journal"), MaxStateBytes: 16 << 20,
		ShutdownTimeout: 5 * time.Second,
		Deliver: func(context.Context, preparedworker.Delivery[preparedworkerfixture.Effect]) error {
			return nil
		},
	}
}

func buildPreparedWorkerHostPublication(
	t testing.TB,
	workerPackage string,
	name string,
) preparedWorkerHostPublication {
	t.Helper()
	artifact := preparedWorkerHostArtifact(t)
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	options := preparedworkerbuild.NewOptions(preparedworkerprobe.TransactionRunnerContract())
	goCommand, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	options.GoCommand = goCommand
	options.ModuleDir = root
	options.Program = preparedworkerbuild.Program{
		Artifact: artifact,
		GeneratedGo: filepath.Join(
			root,
			"internal",
			"preparedworkerfixture",
			"generated",
			"prepared_generated.go",
		),
	}
	options.WorkerPackage = workerPackage
	options.Target = preparedworkerbuild.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	options.OutputDir = t.TempDir()
	options.Name = name
	options.MaxExecutableBytes = 128 << 20
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	buildResult, err := preparedworkerbuild.Build(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	published, err := workerartifact.Open(buildResult.Manifest, options.MaxExecutableBytes)
	if err != nil {
		t.Fatal(err)
	}
	identity := artifact.Identity()
	if published.Descriptor.ProgramRecipeDigest != workerartifact.Digest(identity.ProgramRecipeDigest) ||
		published.Descriptor.ProgramHash != workerartifact.Digest(identity.Bundle.ProgramHash) ||
		published.Descriptor.GeneratedDigest != workerartifact.Digest(identity.GeneratedDigest) {
		t.Fatal("published rich-game worker differs from its generated Program artifact")
	}
	build, err := preparedworker.OpenBuild(buildResult.Manifest, options.MaxExecutableBytes)
	if err != nil {
		t.Fatal(err)
	}
	return preparedWorkerHostPublication{build: build, published: published}
}

func preparedWorkerHostArtifact(t testing.TB) *ember.PreparedGoArtifact {
	t.Helper()
	recipe, err := ember.NewPreparedProgramRecipe(
		[]ember.PreparedProgramModule{
			{
				Module: ember.LogicalModule("prepared-worker/main"), SourceName: preparedworkerfixture.MainModule,
				SourceText: preparedworkerfixture.MainSource,
			},
			{
				Module: ember.LogicalModule("prepared-worker/numeric"), SourceName: preparedworkerfixture.NumericModule,
				SourceText: preparedworkerfixture.NumericSource,
			},
			{
				Module: ember.LogicalModule("prepared-worker/shared"), SourceName: preparedworkerfixture.SharedModule,
				SourceText: preparedworkerfixture.SharedSource,
			},
		},
		[]ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("prepared-worker/main")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	program, _, err := recipe.Load(context.Background(), ember.ProgramOptions{Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := program.GeneratePreparedGo(ember.PreparedGoOptions{
		Package: "preparedworkerfixturegenerated", EmbedProgramRecipe: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestParsePreparedWorkerChildPIDRequiresOneExactOwnedExecutable(t *testing.T) {
	const executable = "/tmp/ember worker/cache/worker"
	got, err := parsePreparedWorkerChildPID(`
  101  77 /tmp/other/worker
  102  88 /tmp/ember worker/cache/worker
  103  77 /tmp/ember worker/cache/worker
  104  77 /tmp/ember worker/cache/worker --unexpected
`, 77, executable)
	if err != nil {
		t.Fatal(err)
	}
	if got != 103 {
		t.Fatalf("child PID = %d, want 103", got)
	}
	if _, err := parsePreparedWorkerChildPID("103 77 "+executable+"\n104 77 "+executable+"\n", 77, executable); err == nil {
		t.Fatal("ambiguous worker process inventory accepted")
	}
	if _, err := parsePreparedWorkerChildPID("not-a-process-row\n", 77, executable); err == nil {
		t.Fatal("malformed worker process inventory accepted")
	}
}
