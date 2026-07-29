package preparedworker_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/internal/preparedworkerprobe"
	"github.com/besmpl/ember/preparedworker"
	"github.com/besmpl/ember/preparedworkerbuild"
)

func TestProcessAdapterMatchesEmbeddedStaticAOTAcrossReload(t *testing.T) {
	build := buildProcessFixtureBuild(t)
	var embeddedDeliveries []preparedworker.Delivery[preparedworkerfixture.Effect]
	embedded, err := preparedworker.OpenEmbedded(
		context.Background(),
		richGameRunnerOptions(t, "embedded", &embeddedDeliveries),
		preparedworkerprobe.NewTransactionHandler,
	)
	if err != nil {
		t.Fatal(err)
	}
	var processDeliveries []preparedworker.Delivery[preparedworkerfixture.Effect]
	process, reload, err := preparedworker.OpenDevelopment(
		context.Background(),
		richGameRunnerOptions(t, "process", &processDeliveries),
		build,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := embedded.(preparedworker.Reload); ok {
		t.Fatal("embedded release runner exposes reload capability")
	}
	if _, ok := process.(preparedworker.Reload); ok {
		t.Fatal("development runner exposes reload capability through type assertion")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := embedded.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := process.Close(ctx); err != nil {
			t.Error(err)
		}
	})

	requests := []preparedworkerfixture.TurnRequest{
		richGameFirstRequest(),
		richGameSecondRequest(richGameFirstResult().State),
	}
	for _, request := range requests {
		want, err := embedded.Apply(context.Background(), richGameOperation(request))
		if err != nil {
			t.Fatal(err)
		}
		got, err := process.Apply(context.Background(), richGameOperation(request))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("process completion = %#v, want embedded %#v", got, want)
		}
	}

	replacement, err := reload.Prepare(context.Background(), build)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Activate(); err != nil {
		t.Fatal(err)
	}
	thirdRequest := richGameThirdRequest(richGameSecondResult().State)
	want, err := embedded.Apply(context.Background(), richGameOperation(thirdRequest))
	if err != nil {
		t.Fatal(err)
	}
	got, err := process.Apply(context.Background(), richGameOperation(thirdRequest))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded process completion = %#v, want embedded %#v", got, want)
	}
	if !reflect.DeepEqual(processDeliveries, embeddedDeliveries) {
		t.Fatalf("process deliveries = %#v, want embedded %#v", processDeliveries, embeddedDeliveries)
	}
}

func TestProcessBuildExecutesProgramFirstSeenAfterParentBuild(t *testing.T) {
	const unknownNumericSource = `
return function(seed, work)
    local numeric = 0
    for index = 1, work do
        local n = seed + ((index - 1) % 3)
        local previous = 0
        local current = 1
        for fibIndex = 1, n do
            local nextValue = previous + current
            previous = current
            current = nextValue
        end
        numeric = numeric + previous
    end
    return numeric + 1000
end
`
	build := buildProcessFixtureBuildForArtifact(
		t,
		preparedWorkerFixtureArtifactFor(t, unknownNumericSource),
		"ember-unknown-source-worker",
	)
	runner, _, err := preparedworker.OpenDevelopment(
		context.Background(),
		richGameRunnerOptions(t, "unknown-source", new([]preparedworker.Delivery[preparedworkerfixture.Effect])),
		build,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runner.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	completion, err := runner.Apply(context.Background(), richGameOperation(richGameFirstRequest()))
	if err != nil {
		t.Fatal(err)
	}
	if completion.Result.State.Total != 1006 || len(completion.Result.Effects) != 2 ||
		completion.Result.Effects[1].Value != 1005 {
		t.Fatalf("unknown-source result = %#v, want total 1006 and numeric effect 1005", completion.Result)
	}
}

func richGameRunnerOptions(
	t *testing.T,
	name string,
	deliveries *[]preparedworker.Delivery[preparedworkerfixture.Effect],
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
		Stream:    "rich-game-caller-interface",
		Contract:  preparedworkerprobe.TransactionRunnerContract(),
		StatePath: filepath.Join(t.TempDir(), name+".journal"), MaxStateBytes: 16 << 20,
		ShutdownTimeout: 5 * time.Second,
		Deliver: func(_ context.Context, delivery preparedworker.Delivery[preparedworkerfixture.Effect]) error {
			*deliveries = append(*deliveries, delivery)
			return nil
		},
	}
}

func buildProcessFixtureBuild(t *testing.T) preparedworker.Build {
	return buildProcessFixtureBuildForArtifact(
		t,
		preparedWorkerFixtureArtifact(t),
		"ember-prepared-worker",
	)
}

func buildProcessFixtureBuildForArtifact(
	t *testing.T,
	artifact *ember.PreparedGoArtifact,
	name string,
) preparedworker.Build {
	t.Helper()
	root, err := filepath.Abs("..")
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
	options.WorkerPackage = "./internal/preparedworkerprobe/cmd/worker"
	options.Target = preparedworkerbuild.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	options.OutputDir = t.TempDir()
	options.Name = name
	options.MaxExecutableBytes = 128 << 20
	result, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	build, err := preparedworker.OpenBuild(result.Manifest, options.MaxExecutableBytes)
	if err != nil {
		t.Fatal(err)
	}
	return build
}

func preparedWorkerFixtureArtifact(t *testing.T) *ember.PreparedGoArtifact {
	return preparedWorkerFixtureArtifactFor(t, preparedworkerfixture.NumericSource)
}

func preparedWorkerFixtureArtifactFor(
	t *testing.T,
	numericSource string,
) *ember.PreparedGoArtifact {
	t.Helper()
	recipe, err := ember.NewPreparedProgramRecipe(
		[]ember.PreparedProgramModule{
			{
				Module: ember.LogicalModule("prepared-worker/main"), SourceName: preparedworkerfixture.MainModule,
				SourceText: preparedworkerfixture.MainSource,
			},
			{
				Module: ember.LogicalModule("prepared-worker/numeric"), SourceName: preparedworkerfixture.NumericModule,
				SourceText: numericSource,
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
