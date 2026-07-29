package preparedworker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const fileJournalTestMaxBytes int64 = 1 << 20

func TestFileJournalCommitLookupLatestAndPublishedSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-persistence")
	record := fileJournalTestRecord(stream, 1, 0)

	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	if disposition, err := journal.Commit(context.Background(), Position{}, record); err != nil {
		t.Fatal(err)
	} else if disposition != commitStored {
		t.Fatalf("commit disposition = %v, want stored", disposition)
	}
	if err := journal.MarkPublished(context.Background(), stream, record.Outbox[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	t.Cleanup(func() { _ = journal.Close() })
	want := cloneRecord(record)
	want.Outbox[0].Published = true
	got, ok, err := journal.Lookup(context.Background(), stream, record.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("lookup = %#v, %v; want %#v, true", got, ok, want)
	}
	latest, ok, err := journal.Latest(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !reflect.DeepEqual(latest, want) {
		t.Fatalf("latest = %#v, %v; want %#v, true", latest, ok, want)
	}
}

func TestFileJournalLatestQuiescentSurvivesNonQuiescentHeadAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-latest-quiescent")
	quiescent := fileJournalTestRecord(stream, 1, 0)
	nonQuiescent := fileJournalTestRecord(stream, 2, 1)
	nonQuiescent.Checkpoint = nil
	nonQuiescent.Quiescent = false
	nonQuiescent.Digest = recordDigest(nonQuiescent)

	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	fileJournalTestCommit(t, journal, Position{}, quiescent)
	fileJournalTestCommit(t, journal, Position{Sequence: 1, Revision: 1}, nonQuiescent)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	head, ok, err := journal.Latest(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !reflect.DeepEqual(head, nonQuiescent) {
		t.Fatalf("latest = %#v, %v; want non-quiescent head %#v, true", head, ok, nonQuiescent)
	}
	restore, ok, err := journal.LatestQuiescent(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !reflect.DeepEqual(restore, quiescent) {
		t.Fatalf("latest quiescent = %#v, %v; want %#v, true", restore, ok, quiescent)
	}
}

func TestFileJournalRejectsConcurrentOwnerAndReleasesOwnershipOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	first := openFileJournalForTest(t, path, fileJournalTestMaxBytes)

	if second, err := openDurableJournal(path, fileJournalTestMaxBytes); err == nil {
		_ = second.Close()
		t.Fatal("opened journal while another owner held it")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileJournalCreationFailsWhenDirectoryEntryCannotBeSynced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	ops := defaultFileJournalOps()
	ops.syncDirectory = func(string) error { return errors.New("injected directory sync failure") }

	if journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops); err == nil {
		_ = journal.Close()
		t.Fatal("created journal without durable directory entry")
	}

	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileJournalReopenFailsWhenDirectoryEntryCannotBeSynced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	ops := defaultFileJournalOps()
	ops.syncDirectory = func(string) error { return errors.New("injected directory sync failure") }

	if journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops); err == nil {
		_ = journal.Close()
		t.Fatal("reopened journal without durable directory entry")
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileJournalDiscardsTornTailAndTruncatesToLastCompleteRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-torn-tail")
	first := fileJournalTestRecord(stream, 1, 0)
	second := fileJournalTestRecord(stream, 2, 1)

	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	fileJournalTestCommit(t, journal, Position{}, first)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	firstBoundary := fileJournalTestSize(t, path)

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	fileJournalTestCommit(t, journal, Position{Sequence: 1, Revision: 1}, second)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	secondBoundary := fileJournalTestSize(t, path)
	if secondBoundary <= firstBoundary+1 {
		t.Fatalf("second record did not extend journal: %d -> %d", firstBoundary, secondBoundary)
	}
	tornSize := firstBoundary + (secondBoundary-firstBoundary)/2
	if err := os.Truncate(path, tornSize); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	if got := fileJournalTestSize(t, path); got != firstBoundary {
		t.Fatalf("recovered journal size = %d, want committed boundary %d", got, firstBoundary)
	}
	latest, ok, err := journal.Latest(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !reflect.DeepEqual(latest, first) {
		t.Fatalf("latest after torn tail = %#v, %v; want %#v, true", latest, ok, first)
	}
	if _, ok, err := journal.Lookup(context.Background(), stream, second.Sequence); err != nil || ok {
		t.Fatalf("torn record lookup found = %v, error = %v; want false, nil", ok, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileJournalRejectsInteriorCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-interior-corruption")
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)

	first := fileJournalTestRecord(stream, 1, 0)
	fileJournalTestCommit(t, journal, Position{}, first)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	firstBoundary := fileJournalTestSize(t, path)

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	second := fileJournalTestRecord(stream, 2, 1)
	fileJournalTestCommit(t, journal, Position{Sequence: 1, Revision: 1}, second)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	secondBoundary := fileJournalTestSize(t, path)

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	third := fileJournalTestRecord(stream, 3, 2)
	fileJournalTestCommit(t, journal, Position{Sequence: 2, Revision: 2}, third)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	offset := firstBoundary + (secondBoundary-firstBoundary)/2
	corrupted := []byte{0}
	if _, err := file.ReadAt(corrupted, offset); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	corrupted[0] ^= 0xff
	if _, err := file.WriteAt(corrupted, offset); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if journal, err := openDurableJournal(path, fileJournalTestMaxBytes); err == nil {
		_ = journal.Close()
		t.Fatal("opened journal with interior corruption")
	}
}

func TestFileJournalExactDuplicateAndContinuity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-continuity")
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	t.Cleanup(func() { _ = journal.Close() })

	first := fileJournalTestRecord(stream, 1, 0)
	fileJournalTestCommit(t, journal, Position{}, first)
	sizeAfterFirst := fileJournalTestSize(t, path)
	if disposition, err := journal.Commit(context.Background(), Position{}, cloneRecord(first)); err != nil {
		t.Fatalf("exact duplicate commit: %v", err)
	} else if disposition != commitStored {
		t.Fatalf("exact duplicate disposition = %v, want stored", disposition)
	}
	if got := fileJournalTestSize(t, path); got != sizeAfterFirst {
		t.Fatalf("exact duplicate grew journal to %d, want %d", got, sizeAfterFirst)
	}

	conflict := cloneRecord(first)
	conflict.Result = []byte("different result")
	conflict.ResultDigest = hashContent(conflict.Result)
	conflict.Digest = recordDigest(conflict)
	if disposition, err := journal.Commit(context.Background(), Position{}, conflict); err != nil {
		t.Fatalf("conflicting duplicate commit: %v", err)
	} else if disposition != commitNotStored {
		t.Fatalf("conflicting duplicate disposition = %v, want not stored", disposition)
	}

	gap := fileJournalTestRecord(stream, 3, 1)
	if disposition, err := journal.Commit(
		context.Background(),
		Position{Sequence: 1, Revision: 1},
		gap,
	); err == nil || disposition != commitNotStored {
		t.Fatalf("gap commit disposition/error = %v/%v, want not stored/error", disposition, err)
	}
	second := fileJournalTestRecord(stream, 2, 1)
	if disposition, err := journal.Commit(context.Background(), Position{}, second); err != nil {
		t.Fatalf("stale expected position commit: %v", err)
	} else if disposition != commitNotStored {
		t.Fatalf("stale expected position disposition = %v, want not stored", disposition)
	}
	fileJournalTestCommit(t, journal, Position{Sequence: 1, Revision: 1}, second)
}

func TestFileJournalExactDuplicatePublicationDoesNotGrow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-duplicate-publication")
	record := fileJournalTestRecord(stream, 1, 0)
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	fileJournalTestCommit(t, journal, Position{}, record)
	if err := journal.MarkPublished(context.Background(), stream, record.Outbox[0].ID); err != nil {
		t.Fatal(err)
	}
	sizeAfterFirst := fileJournalTestSize(t, path)

	if err := journal.MarkPublished(context.Background(), stream, record.Outbox[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := fileJournalTestSize(t, path); got != sizeAfterFirst {
		t.Fatalf("duplicate publication grew journal to %d, want %d", got, sizeAfterFirst)
	}
}

func TestFileJournalMaxBytesRejectsBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-max-bytes")
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	first := fileJournalTestRecord(stream, 1, 0)
	fileJournalTestCommit(t, journal, Position{}, first)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	committedSize := fileJournalTestSize(t, path)

	journal = openFileJournalForTest(t, path, committedSize)
	second := fileJournalTestRecord(stream, 2, 1)
	if disposition, err := journal.Commit(
		context.Background(),
		Position{Sequence: 1, Revision: 1},
		second,
	); err == nil || disposition != commitNotStored {
		t.Fatalf("oversize commit disposition/error = %v/%v, want not stored/error", disposition, err)
	}
	if got := fileJournalTestSize(t, path); got != committedSize {
		t.Fatalf("oversize commit changed size to %d, want %d", got, committedSize)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, committedSize)
	latest, ok, err := journal.Latest(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !reflect.DeepEqual(latest, first) {
		t.Fatalf("latest after oversize rejection = %#v, %v; want %#v, true", latest, ok, first)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileJournalMaxBytesRejectsPublicationBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-publication-max-bytes")
	record := fileJournalTestRecord(stream, 1, 0)
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	fileJournalTestCommit(t, journal, Position{}, record)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	committedSize := fileJournalTestSize(t, path)

	journal = openFileJournalForTest(t, path, committedSize)
	if err := journal.MarkPublished(context.Background(), stream, record.Outbox[0].ID); err == nil {
		t.Fatal("published past journal byte bound")
	}
	if got := fileJournalTestSize(t, path); got != committedSize {
		t.Fatalf("oversize publication changed size to %d, want %d", got, committedSize)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, committedSize)
	defer func() { _ = journal.Close() }()
	got, found, err := journal.Lookup(context.Background(), stream, record.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Outbox[0].Published {
		t.Fatalf("publication after reopen = found %v, published %v; want true, false", found, got.Outbox[0].Published)
	}
}

func openFileJournalForTest(t *testing.T, path string, maxBytes int64) *fileJournal {
	t.Helper()
	journal, err := openDurableJournal(path, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func fileJournalTestCommit(t *testing.T, journal transactionJournal, expected Position, record transactionRecord) {
	t.Helper()
	disposition, err := journal.Commit(context.Background(), expected, record)
	if err != nil {
		t.Fatal(err)
	}
	if disposition != commitStored {
		t.Fatalf("commit disposition = %v, want stored", disposition)
	}
}

func fileJournalTestRecord(stream contentIdentity, sequence, baseRevision uint64) transactionRecord {
	position := Position{Sequence: sequence, Revision: baseRevision + 1}
	request := []byte{byte(sequence), 0x51}
	result := []byte{byte(sequence), 0x52}
	checkpoint := []byte{byte(sequence), 0x43}
	effect := []byte{byte(sequence), 0x45}
	record := transactionRecord{
		Stream:        stream,
		Sequence:      sequence,
		BaseRevision:  baseRevision,
		Revision:      baseRevision + 1,
		RequestDigest: hashContent(request),
		Request:       request,
		ResultDigest:  hashContent(result),
		Result:        result,
		Checkpoint: &checkpointRecord{
			Schema:   identityFor("file-journal-checkpoint-v1"),
			Position: position,
			Payload:  checkpoint,
			Digest:   hashContent(checkpoint),
		},
		Quiescent: true,
		Outbox: []outboxRecord{{
			ID: outboxID{
				Stream:   stream,
				Sequence: sequence,
				Ordinal:  1,
			},
			Payload: effect,
			Digest:  hashContent(effect),
		}},
	}
	record.Digest = recordDigest(record)
	return record
}

func fileJournalTestSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
