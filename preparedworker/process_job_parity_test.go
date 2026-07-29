package preparedworker_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerjobfixture"
	"github.com/besmpl/ember/preparedworker"
	"github.com/besmpl/ember/preparedworkerbuild"
)

func TestProcessAdapterMatchesEmbeddedStaticAOTJobContractAcrossReload(t *testing.T) {
	build := buildProcessJobBuild(t)
	embeddedPath := filepath.Join(t.TempDir(), "embedded.journal")
	processPath := filepath.Join(t.TempDir(), "process.journal")
	var embeddedDeliveries []preparedworker.Delivery[preparedworkerjobfixture.Effect]
	embedded, err := preparedworker.OpenEmbedded(
		context.Background(),
		jobRunnerOptions(embeddedPath, &embeddedDeliveries),
		preparedworkerjobfixture.NewHandler,
	)
	if err != nil {
		t.Fatal(err)
	}
	var processDeliveries []preparedworker.Delivery[preparedworkerjobfixture.Effect]
	process, reload, err := preparedworker.OpenDevelopment(
		context.Background(),
		jobRunnerOptions(processPath, &processDeliveries),
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
		closeJobRunner(t, embedded)
		closeJobRunner(t, process)
	})

	firstOperation := preparedworker.Operation[preparedworkerjobfixture.Request]{
		Sequence: 1,
		Request: preparedworkerjobfixture.Request{
			Queue: 7,
			Jobs: []preparedworkerjobfixture.Job{
				{ID: 11, Seed: 3, Work: 2},
				{ID: 4, Seed: 5, Work: 1},
			},
		},
	}
	first := applyJobRunnerOperation(t, embedded, process, firstOperation)
	wantFirst := preparedworker.Completion[preparedworkerjobfixture.Result]{
		Position: preparedworker.Position{Sequence: 1, Revision: 1},
		Result: preparedworkerjobfixture.Result{
			Queue: 7, Accepted: 2, Completed: 2, Checksum: 10, Cursor: 15010,
		},
	}
	if first != wantFirst {
		t.Fatalf("first completion = %#v, want %#v", first, wantFirst)
	}
	if !reflect.DeepEqual(processDeliveries, embeddedDeliveries) {
		t.Fatalf("process deliveries = %#v, want embedded %#v", processDeliveries, embeddedDeliveries)
	}

	replacement, err := reload.Prepare(context.Background(), build)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Activate(); err != nil {
		t.Fatal(err)
	}
	secondOperation := preparedworker.Operation[preparedworkerjobfixture.Request]{
		Sequence:     2,
		BaseRevision: 1,
		Request: preparedworkerjobfixture.Request{
			Queue: 7,
			Jobs:  []preparedworkerjobfixture.Job{{ID: 8, Seed: 4, Work: 3}},
		},
	}
	second := applyJobRunnerOperation(t, embedded, process, secondOperation)
	wantSecond := preparedworker.Completion[preparedworkerjobfixture.Result]{
		Position: preparedworker.Position{Sequence: 2, Revision: 2},
		Result: preparedworkerjobfixture.Result{
			Queue: 7, Accepted: 1, Completed: 3, Checksum: 26, Cursor: 23026,
		},
	}
	if second != wantSecond {
		t.Fatalf("reloaded completion = %#v, want %#v", second, wantSecond)
	}
	if !reflect.DeepEqual(processDeliveries, embeddedDeliveries) {
		t.Fatalf("reloaded process deliveries = %#v, want embedded %#v", processDeliveries, embeddedDeliveries)
	}

	embeddedPublished := append([]preparedworker.Delivery[preparedworkerjobfixture.Effect](nil), embeddedDeliveries...)
	processPublished := append([]preparedworker.Delivery[preparedworkerjobfixture.Effect](nil), processDeliveries...)
	embeddedReplay, err := embedded.Apply(context.Background(), firstOperation)
	if err != nil {
		t.Fatal(err)
	}
	processReplay, err := process.Apply(context.Background(), firstOperation)
	if err != nil {
		t.Fatal(err)
	}
	if embeddedReplay != wantFirst || processReplay != wantFirst {
		t.Fatalf("replayed completions = %#v/%#v, want %#v", embeddedReplay, processReplay, wantFirst)
	}
	if !reflect.DeepEqual(embeddedDeliveries, embeddedPublished) ||
		!reflect.DeepEqual(processDeliveries, processPublished) {
		t.Fatal("committed replay republished job effects")
	}

	closeJobRunner(t, embedded)
	closeJobRunner(t, process)
	assertJobJournalParity(t, embeddedPath, processPath)
}

func jobRunnerOptions(
	statePath string,
	deliveries *[]preparedworker.Delivery[preparedworkerjobfixture.Effect],
) preparedworker.Options[
	preparedworkerjobfixture.Request,
	preparedworkerjobfixture.Result,
	preparedworkerjobfixture.Checkpoint,
	preparedworkerjobfixture.Effect,
] {
	return preparedworker.Options[
		preparedworkerjobfixture.Request,
		preparedworkerjobfixture.Result,
		preparedworkerjobfixture.Checkpoint,
		preparedworkerjobfixture.Effect,
	]{
		Stream: "process-job-parity-stream", Contract: preparedworkerjobfixture.RunnerContract(),
		StatePath: statePath, MaxStateBytes: 16 << 20, ShutdownTimeout: 5 * time.Second,
		Deliver: func(_ context.Context, delivery preparedworker.Delivery[preparedworkerjobfixture.Effect]) error {
			*deliveries = append(*deliveries, delivery)
			return nil
		},
	}
}

func applyJobRunnerOperation(
	t *testing.T,
	embedded preparedworker.Runner[preparedworkerjobfixture.Request, preparedworkerjobfixture.Result],
	process preparedworker.Runner[preparedworkerjobfixture.Request, preparedworkerjobfixture.Result],
	operation preparedworker.Operation[preparedworkerjobfixture.Request],
) preparedworker.Completion[preparedworkerjobfixture.Result] {
	t.Helper()
	want, err := embedded.Apply(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	got, err := process.Apply(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("process completion = %#v, want embedded %#v", got, want)
	}
	return want
}

func closeJobRunner[Q, R any](t *testing.T, runner preparedworker.Runner[Q, R]) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runner.Close(ctx); err != nil {
		t.Error(err)
	}
}

func assertJobJournalParity(
	t *testing.T,
	embeddedPath string,
	processPath string,
) {
	t.Helper()
	embedded, err := preparedworker.OpenTestFileJournal(embeddedPath, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := embedded.Close(); err != nil {
			t.Error(err)
		}
	}()
	process, err := preparedworker.OpenTestFileJournal(processPath, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := process.Close(); err != nil {
			t.Error(err)
		}
	}()

	stream := preparedworker.ExportIdentityFor("prepared-worker-stream-v1:process-job-parity-stream")
	wants := []struct {
		checkpoint string
		effects    []string
	}{
		{
			checkpoint: "c2010200000000000000010000000000000001000700000002000000000000000a0000000000003aa2",
			effects: []string{
				"d2010000000b0000000000002afd0000000000000005",
				"d201000000040000000000003aa20000000000000005",
			},
		},
		{
			checkpoint: "c2010200000000000000020000000000000002000700000003000000000000001a00000000000059f2",
			effects:    []string{"d2010000000800000000000059f20000000000000010"},
		},
	}
	for index, want := range wants {
		sequence := uint64(index + 1)
		embeddedRecord, ok, err := embedded.Lookup(context.Background(), stream, sequence)
		if err != nil || !ok {
			t.Fatalf("embedded record %d = present %v, error %v", sequence, ok, err)
		}
		processRecord, ok, err := process.Lookup(context.Background(), stream, sequence)
		if err != nil || !ok {
			t.Fatalf("process record %d = present %v, error %v", sequence, ok, err)
		}
		if !reflect.DeepEqual(processRecord, embeddedRecord) {
			t.Fatalf("process record %d = %#v, want embedded %#v", sequence, processRecord, embeddedRecord)
		}
		wantCheckpoint := decodeJobParityHex(t, want.checkpoint)
		if embeddedRecord.Checkpoint == nil || !embeddedRecord.Quiescent ||
			!bytes.Equal(embeddedRecord.Checkpoint.Payload, wantCheckpoint) {
			t.Fatalf("checkpoint %d = %#v, want %x and quiescent", sequence, embeddedRecord.Checkpoint, wantCheckpoint)
		}
		if len(embeddedRecord.Outbox) != len(want.effects) {
			t.Fatalf("outbox %d has %d effects, want %d", sequence, len(embeddedRecord.Outbox), len(want.effects))
		}
		for effectIndex, encoded := range want.effects {
			wantEffect := decodeJobParityHex(t, encoded)
			if !embeddedRecord.Outbox[effectIndex].Published ||
				!bytes.Equal(embeddedRecord.Outbox[effectIndex].Payload, wantEffect) {
				t.Fatalf("outbox %d/%d = %#v, want published %x", sequence, effectIndex+1, embeddedRecord.Outbox[effectIndex], wantEffect)
			}
		}
	}
}

func decodeJobParityHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func buildProcessJobBuild(t *testing.T) preparedworker.Build {
	return buildProcessJobBuildFor(
		t,
		"./internal/preparedworkerjobfixture/cmd/worker",
		"ember-prepared-job-worker",
	)
}

func buildProcessJobBuildFor(t *testing.T, workerPackage, name string) preparedworker.Build {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	options := preparedworkerbuild.NewOptions(preparedworkerjobfixture.RunnerContract())
	goCommand, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	options.GoCommand = goCommand
	options.ModuleDir = root
	options.Program = preparedworkerbuild.Program{
		Artifact: preparedWorkerFixtureArtifact(t),
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
