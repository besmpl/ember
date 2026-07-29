package ember_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/besmpl/ember"
	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
	"github.com/besmpl/ember/internal/preparedworkerparity"
	"github.com/besmpl/ember/preparedworker"
	"github.com/besmpl/ember/preparedworkerbuild"
)

const (
	preparedWorkerParitySwapSoakEnvironment       = "EMBER_PREPARED_WORKER_SWAP_SOAK"
	preparedWorkerParitySwapSoakOutputEnvironment = "EMBER_PREPARED_WORKER_SWAP_SOAK_OUTPUT"
)

type preparedWorkerParityPublication struct {
	build     preparedworker.Build
	published workerartifact.Published
}

type preparedWorkerParityClient struct {
	runner   preparedworker.Runner[preparedworkerparity.Request, preparedworkerparity.Result]
	position preparedworker.Position
}

func (client *preparedWorkerParityClient) call(
	caseIndex uint16,
	iterations int,
	seed int64,
) (float64, string, error) {
	if client == nil || client.runner == nil {
		return 0, "", fmt.Errorf("EPW2 parity client is closed")
	}
	if iterations <= 0 || uint64(iterations) > uint64(preparedworkerparity.MaxIterations) {
		return 0, "", fmt.Errorf("EPW2 parity iterations %d are out of bounds", iterations)
	}
	operation := preparedworker.Operation[preparedworkerparity.Request]{
		Sequence:     client.position.Sequence + 1,
		BaseRevision: client.position.Revision,
		Request: preparedworkerparity.Request{
			Case: caseIndex, Iterations: uint32(iterations), Seed: seed,
		},
	}
	start := time.Now()
	completion, err := client.runner.Apply(context.Background(), operation)
	elapsed := time.Since(start)
	if err != nil {
		return float64(elapsed.Nanoseconds()), "", err
	}
	client.position = completion.Position
	return float64(elapsed.Nanoseconds()), strconv.FormatInt(completion.Result.Checksum, 10), nil
}

func preparedWorkerParityCaseIndices() map[string]uint16 {
	entries := parityCaseManifest()
	indices := make(map[string]uint16, len(entries))
	for index, entry := range entries {
		indices[entry.Corpus+"/"+entry.Name] = uint16(index)
	}
	return indices
}

func writePreparedWorkerParityBuildEvidence(
	t testing.TB,
	output string,
	capture preparedWorkerCaptureContext,
	publication preparedWorkerParityPublication,
) {
	t.Helper()
	descriptor := publication.published.Descriptor
	if len(descriptor.ProtoCounts) != int(preparedworkerparity.CaseCount) {
		t.Fatalf(
			"EPW2 publication has %d modules, want %d",
			len(descriptor.ProtoCounts),
			preparedworkerparity.CaseCount,
		)
	}
	file := createPreparedWorkerCaptureFile(t, filepath.Join(output, "worker-build.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\twire_magic\tprotocol_version\tbuild_id\texecutable_sha256\tcontract_sha256\trecipe_sha256\tprogram_sha256\tgenerated_sha256\twrapper_sha256\ttarget_os\ttarget_arch\tmodule_count\tactivated\tenvironment_sha256\n")
	writePreparedWorkerCapture(
		t,
		file,
		"1\t%s\t%s\t%s\tEPW2\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\ttrue\t%s\n",
		capture.ID,
		capture.Pair,
		capture.SourceCommit,
		descriptor.ProtocolVersion,
		publication.published.BuildID.String(),
		publication.published.ExecutableDigest.String(),
		descriptor.Contract.String(),
		descriptor.ProgramRecipeDigest.String(),
		descriptor.ProgramHash.String(),
		descriptor.GeneratedDigest.String(),
		descriptor.WrapperDigest.String(),
		descriptor.TargetOS,
		descriptor.TargetArch,
		len(descriptor.ProtoCounts),
		capture.EnvironmentHash,
	)
}

func calibratePreparedWorkerParityClientScale(
	client preparedWorkerParityCaller,
	caseIndex uint16,
) (preparedWorkerParityCalibration, error) {
	return selectPreparedWorkerParityCallScale(func(iterations int) (float64, error) {
		return measurePreparedWorkerParityClientSpan(client, caseIndex, iterations)
	})
}

func verifyPreparedWorkerParityClientScale(
	callScale int,
	client preparedWorkerParityCaller,
	caseIndex uint16,
) (preparedWorkerParityCalibration, error) {
	return verifyPreparedWorkerParityCallScale(callScale, func(iterations int) (float64, error) {
		return measurePreparedWorkerParityClientSpan(client, caseIndex, iterations)
	})
}

func measurePreparedWorkerParityClientSpan(
	client preparedWorkerParityCaller,
	caseIndex uint16,
	iterations int,
) (float64, error) {
	baseline, _, err := client.call(caseIndex, 1, parityCaptureSeed)
	if err != nil {
		return 0, err
	}
	maximum, _, err := client.call(caseIndex, iterations, parityCaptureSeed)
	if err != nil {
		return 0, err
	}
	span := maximum - baseline
	if span <= 0 {
		// A non-positive observation means the guest-work delta is still below
		// host/durability noise. Keep it below the target so calibration grows
		// the scale instead of treating intercept as useful signal.
		return 1, nil
	}
	return span, nil
}

type preparedWorkerParityCalibrationClient struct {
	calls int
}

func (client *preparedWorkerParityCalibrationClient) call(
	_ uint16,
	iterations int,
	_ int64,
) (float64, string, error) {
	client.calls++
	return float64((10 * time.Millisecond).Nanoseconds()) + float64(iterations*7), "1", nil
}

func TestPreparedWorkerParityClientCalibrationSubtractsFixedApplyCost(t *testing.T) {
	client := &preparedWorkerParityCalibrationClient{}
	calibration, err := calibratePreparedWorkerParityClientScale(client, 0)
	if err != nil {
		t.Fatal(err)
	}
	if calibration.Scale != 32 || len(calibration.Samples) != 18 || client.calls != 36 {
		t.Fatalf(
			"calibration = %#v after %d client calls, want scale 32, 18 span samples, and 36 calls",
			calibration,
			client.calls,
		)
	}
}

func TestPreparedWorkerParityTypedEmbeddedAndProcessAgreeAll37(t *testing.T) {
	build := buildPreparedWorkerParityBuild(t, preparedWorkerParityArtifact(t, parityDefaultFixtureVariant))
	embedded, err := preparedworker.OpenEmbedded(
		context.Background(),
		preparedWorkerParityRunnerOptions(t, "embedded"),
		preparedworkerparity.NewHandler,
	)
	if err != nil {
		t.Fatal(err)
	}
	process, _, err := preparedworker.OpenDevelopment(
		context.Background(),
		preparedWorkerParityRunnerOptions(t, "process"),
		build,
	)
	if err != nil {
		closePreparedWorkerParityRunner(t, embedded)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closePreparedWorkerParityRunner(t, embedded)
		closePreparedWorkerParityRunner(t, process)
	})

	for caseIndex := uint16(0); caseIndex < preparedworkerparity.CaseCount; caseIndex++ {
		operation := preparedworker.Operation[preparedworkerparity.Request]{
			Sequence:     uint64(caseIndex) + 1,
			BaseRevision: uint64(caseIndex),
			Request: preparedworkerparity.Request{
				Case: caseIndex, Iterations: 1, Seed: 17,
			},
		}
		want, err := embedded.Apply(context.Background(), operation)
		if err != nil {
			t.Fatalf("embedded case %d: %v", caseIndex, err)
		}
		got, err := process.Apply(context.Background(), operation)
		if err != nil {
			t.Fatalf("process case %d: %v", caseIndex, err)
		}
		if got != want {
			t.Fatalf("process case %d = %#v, want embedded %#v", caseIndex, got, want)
		}
	}

	operation := preparedworker.Operation[preparedworkerparity.Request]{
		Sequence:     uint64(preparedworkerparity.CaseCount) + 1,
		BaseRevision: uint64(preparedworkerparity.CaseCount),
		Request: preparedworkerparity.Request{
			Case: 10, Iterations: 3, Seed: 17,
		},
	}
	for name, runner := range map[string]preparedworker.Runner[preparedworkerparity.Request, preparedworkerparity.Result]{
		"embedded": embedded,
		"process":  process,
	} {
		completion, err := runner.Apply(context.Background(), operation)
		if err != nil {
			t.Fatalf("%s frozen recursive case: %v", name, err)
		}
		if completion.Result.Checksum != 90152 {
			t.Fatalf("%s frozen recursive checksum = %d, want 90152", name, completion.Result.Checksum)
		}
	}
}

func TestPreparedWorkerParityAlternatesIndependentStaticAOTGenerations(t *testing.T) {
	standardArtifact := preparedWorkerParityArtifact(t, parityDefaultFixtureVariant)
	holdoutArtifact := preparedWorkerParityArtifact(t, parityHoldoutFixtureVariant)
	standardIdentity := standardArtifact.Identity()
	holdoutIdentity := holdoutArtifact.Identity()
	if standardIdentity.Bundle.ProgramHash == holdoutIdentity.Bundle.ProgramHash ||
		standardIdentity.GeneratedDigest == holdoutIdentity.GeneratedDigest {
		t.Fatal("standard and holdout artifacts do not have independent identities")
	}
	outputDir := t.TempDir()
	standardPublication := buildPreparedWorkerParityPublication(
		t, standardArtifact, outputDir, "ember-epw2-parity-standard",
	)
	holdoutPublication := buildPreparedWorkerParityPublication(
		t, holdoutArtifact, outputDir, "ember-epw2-parity-holdout",
	)
	embedded, err := preparedworker.OpenEmbedded(
		context.Background(),
		preparedWorkerParityRunnerOptions(t, "swap-embedded"),
		preparedworkerparity.NewHandler,
	)
	if err != nil {
		t.Fatal(err)
	}
	process, reload, err := preparedworker.OpenDevelopment(
		context.Background(),
		preparedWorkerParityRunnerOptions(t, "swap-process"),
		standardPublication.build,
	)
	if err != nil {
		closePreparedWorkerParityRunner(t, embedded)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closePreparedWorkerParityRunner(t, embedded)
		closePreparedWorkerParityRunner(t, process)
	})
	embeddedClient := preparedWorkerParityClient{runner: embedded}
	processClient := preparedWorkerParityClient{runner: process}
	resources := preparedWorkerResourceEvidence{
		TargetOS:      runtime.GOOS,
		TargetArch:    runtime.GOARCH,
		GoVersion:     runtime.Version(),
		StandardBuild: standardPublication.published.BuildID.String(),
		HoldoutBuild:  holdoutPublication.published.BuildID.String(),
	}
	observePreparedWorkerResources(t, &resources, "initial", 0)

	swaps := 8
	soak := os.Getenv(preparedWorkerParitySwapSoakEnvironment) == "1"
	if soak {
		swaps = 1024
	}
	builds := [...]preparedworker.Build{holdoutPublication.build, standardPublication.build}
	for swap := 0; swap < swaps; swap++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		candidate, prepareErr := reload.Prepare(ctx, builds[swap%len(builds)])
		cancel()
		if prepareErr != nil {
			t.Fatalf("swap %d/%d prepare: %v", swap+1, swaps, prepareErr)
		}
		observePreparedWorkerResources(t, &resources, "prepared", swap+1)
		if err := candidate.Activate(); err != nil {
			t.Fatalf("swap %d/%d activate: %v", swap+1, swaps, err)
		}
		caseIndex := uint16(swap % int(preparedworkerparity.CaseCount))
		seed := parityCaptureSeed + int64(swap%17)
		_, want, err := embeddedClient.call(caseIndex, 1, seed)
		if err != nil {
			t.Fatalf("swap %d/%d embedded case %d: %v", swap+1, swaps, caseIndex, err)
		}
		_, got, err := processClient.call(caseIndex, 1, seed)
		if err != nil {
			t.Fatalf("swap %d/%d process case %d: %v", swap+1, swaps, caseIndex, err)
		}
		if got != want {
			t.Fatalf("swap %d/%d case %d = %s, want %s", swap+1, swaps, caseIndex, got, want)
		}
	}
	closePreparedWorkerParityRunner(t, process)
	observePreparedWorkerResources(t, &resources, "closed", swaps)
	if soak {
		output := os.Getenv(preparedWorkerParitySwapSoakOutputEnvironment)
		if output == "" {
			t.Fatalf("%s is required for retained soak evidence", preparedWorkerParitySwapSoakOutputEnvironment)
		}
		if err := writePreparedWorkerResourceEvidence(output, resources, swaps); err != nil {
			t.Fatal(err)
		}
	} else if err := validatePreparedWorkerResourceEvidence(resources, swaps); err != nil {
		t.Fatal(err)
	}
}

func preparedWorkerParityRunnerOptions(
	t testing.TB,
	name string,
) preparedworker.Options[
	preparedworkerparity.Request,
	preparedworkerparity.Result,
	preparedworkerparity.Checkpoint,
	preparedworkerparity.Effect,
] {
	t.Helper()
	return preparedworker.Options[
		preparedworkerparity.Request,
		preparedworkerparity.Result,
		preparedworkerparity.Checkpoint,
		preparedworkerparity.Effect,
	]{
		Stream: "epw2-all37-parity", Contract: preparedworkerparity.Contract(),
		StatePath: filepath.Join(t.TempDir(), name+".journal"), MaxStateBytes: 16 << 20,
		ShutdownTimeout: 5 * time.Second,
		Deliver: func(context.Context, preparedworker.Delivery[preparedworkerparity.Effect]) error {
			return nil
		},
	}
}

func buildPreparedWorkerParityBuild(
	t testing.TB,
	artifact *ember.PreparedGoArtifact,
) preparedworker.Build {
	t.Helper()
	return buildPreparedWorkerParityBuildIn(
		t, artifact, t.TempDir(), "ember-epw2-parity-worker",
	)
}

func buildPreparedWorkerParityBuildIn(
	t testing.TB,
	artifact *ember.PreparedGoArtifact,
	outputDir string,
	name string,
) preparedworker.Build {
	t.Helper()
	return buildPreparedWorkerParityPublication(
		t, artifact, outputDir, name,
	).build
}

func buildPreparedWorkerParityPublication(
	t testing.TB,
	artifact *ember.PreparedGoArtifact,
	outputDir string,
	name string,
) preparedWorkerParityPublication {
	t.Helper()
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	options := preparedworkerbuild.NewOptions(preparedworkerparity.Contract())
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
			"preparedworkerparity",
			"generated",
			"prepared_generated.go",
		),
	}
	options.WorkerPackage = "./internal/preparedworkerparity/cmd/worker"
	options.Target = preparedworkerbuild.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	options.OutputDir = outputDir
	options.Name = name
	options.MaxExecutableBytes = 256 << 20
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := preparedworkerbuild.Build(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	published, err := workerartifact.Open(result.Manifest, options.MaxExecutableBytes)
	if err != nil {
		t.Fatal(err)
	}
	identity := artifact.Identity()
	if published.Descriptor.ProgramRecipeDigest != workerartifact.Digest(identity.ProgramRecipeDigest) ||
		published.Descriptor.ProgramHash != workerartifact.Digest(identity.Bundle.ProgramHash) ||
		published.Descriptor.GeneratedDigest != workerartifact.Digest(identity.GeneratedDigest) {
		t.Fatal("published worker descriptor differs from its generated Program artifact")
	}
	build, err := preparedworker.OpenBuild(result.Manifest, options.MaxExecutableBytes)
	if err != nil {
		t.Fatal(err)
	}
	return preparedWorkerParityPublication{build: build, published: published}
}

func closePreparedWorkerParityRunner(
	t testing.TB,
	runner preparedworker.Runner[preparedworkerparity.Request, preparedworkerparity.Result],
) {
	t.Helper()
	if runner == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runner.Close(ctx); err != nil {
		t.Error(err)
	}
}
