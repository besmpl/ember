package preparedworker_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/besmpl/ember/internal/preparedworkerjobfixture"
	"github.com/besmpl/ember/preparedworker"
)

func TestProcessReloadMigratesStableCheckpointEnvelopeAndReopens(t *testing.T) {
	v1 := buildProcessJobBuildFor(
		t,
		"./internal/preparedworkerjobfixture/cmd/worker_v1",
		"job-worker-v1",
	)
	v2 := buildProcessJobBuildFor(
		t,
		"./internal/preparedworkerjobfixture/cmd/worker",
		"job-worker-v2",
	)
	statePath := filepath.Join(t.TempDir(), "migration.journal")
	var deliveries []preparedworker.Delivery[preparedworkerjobfixture.Effect]
	runner, reload, err := preparedworker.OpenDevelopment(
		context.Background(),
		jobRunnerOptions(statePath, &deliveries),
		v1,
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := runner.Apply(context.Background(), preparedworker.Operation[preparedworkerjobfixture.Request]{
		Sequence: 1,
		Request: preparedworkerjobfixture.Request{
			Queue: 7,
			Jobs:  []preparedworkerjobfixture.Job{{ID: 11, Seed: 3, Work: 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Position != (preparedworker.Position{Sequence: 1, Revision: 1}) {
		t.Fatalf("first position = %#v", first.Position)
	}
	candidate, err := reload.Prepare(context.Background(), v2)
	if err != nil {
		t.Fatalf("prepare v2 migration candidate: %v", err)
	}
	if err := candidate.Activate(); err != nil {
		t.Fatalf("activate v2 migration candidate: %v", err)
	}
	second, err := runner.Apply(context.Background(), preparedworker.Operation[preparedworkerjobfixture.Request]{
		Sequence: 2, BaseRevision: 1,
		Request: preparedworkerjobfixture.Request{
			Queue: 7,
			Jobs:  []preparedworkerjobfixture.Job{{ID: 8, Seed: 4, Work: 3}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Position != (preparedworker.Position{Sequence: 2, Revision: 2}) {
		t.Fatalf("second position = %#v", second.Position)
	}
	rollback, err := reload.Prepare(context.Background(), v1)
	if rollback != nil || !preparedworker.IsFailure(err, preparedworker.FailureLost) {
		t.Fatalf("prepare v1 from v2 checkpoint = %#v, %v; want nil/lost", rollback, err)
	}
	third, err := runner.Apply(context.Background(), preparedworker.Operation[preparedworkerjobfixture.Request]{
		Sequence: 3, BaseRevision: 2,
		Request: preparedworkerjobfixture.Request{
			Queue: 7,
			Jobs:  []preparedworkerjobfixture.Job{{ID: 9, Seed: 2, Work: 2}},
		},
	})
	if err != nil {
		t.Fatalf("active v2 after rejected rollback: %v", err)
	}
	if third.Result != (preparedworkerjobfixture.Result{
		Queue: 7, Accepted: 1, Completed: 3, Checksum: 24, Cursor: 28024,
	}) {
		t.Fatalf("post-rollback result = %#v", third.Result)
	}
	closeJobRunner(t, runner)

	var reopenedDeliveries []preparedworker.Delivery[preparedworkerjobfixture.Effect]
	reopened, _, err := preparedworker.OpenDevelopment(
		context.Background(),
		jobRunnerOptions(statePath, &reopenedDeliveries),
		v2,
	)
	if err != nil {
		t.Fatal(err)
	}
	fourth, err := reopened.Apply(context.Background(), preparedworker.Operation[preparedworkerjobfixture.Request]{
		Sequence: 4, BaseRevision: 3,
		Request: preparedworkerjobfixture.Request{
			Queue: 7,
			Jobs:  []preparedworkerjobfixture.Job{{ID: 5, Seed: 3, Work: 1}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fourth.Result != (preparedworkerjobfixture.Result{
		Queue: 7, Accepted: 1, Completed: 4, Checksum: 26, Cursor: 33026,
	}) {
		t.Fatalf("reopened result = %#v", fourth.Result)
	}
	closeJobRunner(t, reopened)

	journal, err := preparedworker.OpenTestFileJournal(statePath, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	}()
	stream := preparedworker.ExportIdentityFor("prepared-worker-stream-v1:process-job-parity-stream")
	wantFormats := []byte{1, 2, 2, 2}
	for index, want := range wantFormats {
		record, ok, err := journal.Lookup(context.Background(), stream, uint64(index+1))
		if err != nil || !ok {
			t.Fatalf("record %d = present %v, error %v", index+1, ok, err)
		}
		if record.Checkpoint == nil || len(record.Checkpoint.Payload) < 3 || record.Checkpoint.Payload[2] != want {
			t.Fatalf("checkpoint %d = %x, want envelope format %d", index+1, record.Checkpoint.Payload, want)
		}
	}
}
