package preparedworkerbuild_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/internal/preparedworkerprobe"
	"github.com/besmpl/ember/preparedworker"
	"github.com/besmpl/ember/preparedworkerbuild"
)

func TestBuildPublishesOpaqueManifestAndReusesVerifiedCache(t *testing.T) {
	options := richGameBuildOptions(t, t.TempDir(), preparedworkerbuild.Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})

	first, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached {
		t.Fatal("first build unexpectedly reported a cache hit")
	}
	if _, err := preparedworker.OpenBuild(first.Manifest, options.MaxExecutableBytes); err != nil {
		t.Fatalf("open first published build: %v", err)
	}

	second, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached {
		t.Fatal("second identical build did not reuse its verified cache entry")
	}
	if second.Manifest != first.Manifest {
		t.Fatalf("second manifest = %q, want %q", second.Manifest, first.Manifest)
	}
	if _, err := preparedworker.OpenBuild(second.Manifest, options.MaxExecutableBytes); err != nil {
		t.Fatalf("open cached published build: %v", err)
	}

	otherSelection := options
	otherSelection.Name = "other-selection"
	otherSelection.MaxExecutableBytes = 256 << 20
	third, err := preparedworkerbuild.Build(context.Background(), otherSelection)
	if err != nil {
		t.Fatal(err)
	}
	if !third.Cached {
		t.Fatal("publication name and executable bound fragmented the build cache")
	}
	if third.Manifest == first.Manifest {
		t.Fatalf("other selection manifest = %q, want a distinct publication", third.Manifest)
	}
	if _, err := preparedworker.OpenBuild(third.Manifest, otherSelection.MaxExecutableBytes); err != nil {
		t.Fatalf("open other cached publication: %v", err)
	}

	selectedBefore, err := os.ReadFile(first.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	tooSmall := options
	tooSmall.MaxExecutableBytes = 1
	if _, err := preparedworkerbuild.Build(context.Background(), tooSmall); err == nil {
		t.Fatal("cache selection ignored the executable byte bound")
	}
	selectedAfter, err := os.ReadFile(first.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(selectedAfter, selectedBefore) {
		t.Fatal("failed bounded cache selection replaced the previous manifest")
	}
}

func TestBuildOverlaysUnknownGeneratedArtifactWithoutMutatingModuleTree(t *testing.T) {
	output := t.TempDir()
	options := richGameBuildOptions(t, output, preparedworkerbuild.Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})
	generatedPath := options.Program.GeneratedGo
	before, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	options.Program.Artifact = richGamePreparedArtifact(
		t,
		preparedworkerfixture.NumericSource+"\n-- first seen after the parent was built\n",
	)
	first, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	firstManifest, err := os.ReadFile(first.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	options.Program.Artifact = richGamePreparedArtifact(
		t,
		preparedworkerfixture.NumericSource+"\n-- a later hot-reload source\n",
	)
	second, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := os.ReadFile(second.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("overlay build mutated the module's generated source")
	}
	if bytes.Equal(firstManifest, secondManifest) {
		t.Fatal("different unknown source retained the same selected build identity")
	}
}

func TestBuildRequiresImmutableReconstructibleGeneratedArtifact(t *testing.T) {
	options := richGameBuildOptions(t, t.TempDir(), preparedworkerbuild.Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})
	options.Program.Artifact = nil
	if _, err := preparedworkerbuild.Build(context.Background(), options); err == nil ||
		!strings.Contains(err.Error(), "generated artifact is required") {
		t.Fatalf("missing artifact error = %v", err)
	}
	withoutRecipe, err := richGameProgram(t, preparedworkerfixture.NumericSource).GeneratePreparedGo(
		ember.PreparedGoOptions{Package: "preparedworkerfixturegenerated"},
	)
	if err != nil {
		t.Fatal(err)
	}
	options.Program.Artifact = withoutRecipe
	if _, err := preparedworkerbuild.Build(context.Background(), options); err == nil ||
		!strings.Contains(err.Error(), "program recipe digest is zero") {
		t.Fatalf("missing recipe error = %v", err)
	}
}

func TestBuildRequiresExplicitGoCommand(t *testing.T) {
	options := richGameBuildOptions(t, t.TempDir(), preparedworkerbuild.Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})
	options.GoCommand = ""
	if _, err := preparedworkerbuild.Build(context.Background(), options); err == nil ||
		!strings.Contains(err.Error(), "GoCommand") {
		t.Fatalf("missing GoCommand error = %v", err)
	}
}

func TestBuildFailureLeavesPreviousManifestSelected(t *testing.T) {
	options := richGameBuildOptions(t, t.TempDir(), preparedworkerbuild.Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})
	result, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(result.Manifest)
	if err != nil {
		t.Fatal(err)
	}

	options.WorkerPackage = "./internal/worker-package-does-not-exist"
	if _, err := preparedworkerbuild.Build(context.Background(), options); err == nil {
		t.Fatal("build with missing worker package succeeded")
	}
	after, err := os.ReadFile(result.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed build replaced the previous selected manifest")
	}
}

func TestBuildRejectsCorruptCacheEntryWithoutReplacingSelection(t *testing.T) {
	output := t.TempDir()
	options := richGameBuildOptions(t, output, preparedworkerbuild.Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})
	result, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(result.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	executableName := "worker"
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}
	matches, err := filepath.Glob(filepath.Join(
		output,
		".ember-worker-cache",
		runtime.GOOS+"-"+runtime.GOARCH,
		"*",
		executableName,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("cache executables = %v, want one", matches)
	}
	if err := os.Chmod(matches[0], 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(matches[0], os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("corrupt")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := preparedworkerbuild.Build(context.Background(), options); err == nil {
		t.Fatal("build reused a corrupt content-addressed cache entry")
	}
	after, err := os.ReadFile(result.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("cache verification failure replaced the selected manifest")
	}
}

func TestBuildPublishesCrossTargetWithoutPretendingItIsRunnable(t *testing.T) {
	otherArch := "amd64"
	if runtime.GOARCH == otherArch {
		otherArch = "arm64"
	}
	targetOS := runtime.GOOS
	if targetOS != "darwin" && targetOS != "linux" && targetOS != "windows" {
		targetOS = "linux"
	}
	options := richGameBuildOptions(t, t.TempDir(), preparedworkerbuild.Target{
		GOOS: targetOS, GOARCH: otherArch,
	})
	result, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := preparedworker.OpenBuild(result.Manifest, options.MaxExecutableBytes); err == nil ||
		!strings.Contains(err.Error(), "cannot run") {
		t.Fatalf("OpenBuild cross-target error = %v, want target rejection", err)
	}
	cached, err := preparedworkerbuild.Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Cached || cached.Manifest != result.Manifest {
		t.Fatalf("cross-target cache result = %#v, want cached %q", cached, result.Manifest)
	}
}

func TestConcurrentIdenticalBuildsPublishOneVerifiedCacheEntry(t *testing.T) {
	options := richGameBuildOptions(t, t.TempDir(), preparedworkerbuild.Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})
	start := make(chan struct{})
	var wait sync.WaitGroup
	results := make([]preparedworkerbuild.Result, 2)
	errorsFound := make([]error, 2)
	for index := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errorsFound[index] = preparedworkerbuild.Build(context.Background(), options)
		}(index)
	}
	close(start)
	wait.Wait()
	for _, err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0].Manifest != results[1].Manifest {
		t.Fatalf("concurrent manifests = %q and %q", results[0].Manifest, results[1].Manifest)
	}
	cacheHits := 0
	for _, result := range results {
		if result.Cached {
			cacheHits++
		}
	}
	if cacheHits != 1 {
		t.Fatalf("concurrent cache hits = %d, want one winner and one reuse", cacheHits)
	}
	if _, err := preparedworker.OpenBuild(results[0].Manifest, options.MaxExecutableBytes); err != nil {
		t.Fatalf("open concurrently published build: %v", err)
	}
}

func richGameBuildOptions(
	t *testing.T,
	output string,
	target preparedworkerbuild.Target,
) preparedworkerbuild.Options[
	preparedworkerfixture.TurnRequest,
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
] {
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
		Artifact: richGamePreparedArtifact(t, preparedworkerfixture.NumericSource),
		GeneratedGo: filepath.Join(
			root,
			"internal",
			"preparedworkerfixture",
			"generated",
			"prepared_generated.go",
		),
	}
	options.WorkerPackage = "./internal/preparedworkerprobe/cmd/worker"
	options.Target = target
	options.OutputDir = output
	options.Name = "rich-game-worker"
	options.MaxExecutableBytes = 128 << 20
	return options
}

func richGamePreparedArtifact(t *testing.T, numericSource string) *ember.PreparedGoArtifact {
	t.Helper()
	program := richGameProgram(t, numericSource)
	artifact, err := program.GeneratePreparedGo(ember.PreparedGoOptions{
		Package: "preparedworkerfixturegenerated", EmbedProgramRecipe: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func richGameProgram(t *testing.T, numericSource string) *ember.Program {
	t.Helper()
	modules := []ember.PreparedProgramModule{
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
	}
	recipe, err := ember.NewPreparedProgramRecipe(modules, []ember.Entrypoint{{
		Name: "main", Module: ember.LogicalModule("prepared-worker/main"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	program, _, err := recipe.Load(context.Background(), ember.ProgramOptions{Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	return program
}
