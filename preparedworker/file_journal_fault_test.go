package preparedworker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

var errInjectedFileJournalFault = errors.New("injected file journal fault")

func TestFileJournalPartialTransactionWriteIsUnknownUntilReopenFindsItAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-partial-transaction")
	record := fileJournalTestRecord(stream, 1, 0)
	ops := fileJournalFaultOps(1, 7, 0)
	journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops)
	if err != nil {
		t.Fatal(err)
	}

	disposition, err := journal.Commit(context.Background(), Position{}, record)
	if !errors.Is(err, errInjectedFileJournalFault) || disposition != commitUnknown {
		t.Fatalf("partial write disposition/error = %v/%v, want unknown/injected fault", disposition, err)
	}
	if _, _, err := journal.Latest(context.Background(), stream); err == nil ||
		!strings.Contains(err.Error(), "durability unresolved") {
		t.Fatalf("latest after partial write error = %v, want durability unresolved", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	if _, found, err := journal.Lookup(context.Background(), stream, record.Sequence); err != nil || found {
		t.Fatalf("lookup after recovery = found %v, error %v; want false, nil", found, err)
	}
	if size := fileJournalTestSize(t, path); size != 0 {
		t.Fatalf("recovered journal size = %d, want 0", size)
	}
}

func TestFileJournalPartialCommitWriteDiscardsTheWholeTransactionOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-partial-commit")
	record := fileJournalTestRecord(stream, 1, 0)
	ops := fileJournalFaultOps(2, 11, 0)
	journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops)
	if err != nil {
		t.Fatal(err)
	}

	disposition, err := journal.Commit(context.Background(), Position{}, record)
	if !errors.Is(err, errInjectedFileJournalFault) || disposition != commitUnknown {
		t.Fatalf("partial commit disposition/error = %v/%v, want unknown/injected fault", disposition, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	if _, found, err := journal.Lookup(context.Background(), stream, record.Sequence); err != nil || found {
		t.Fatalf("lookup after recovery = found %v, error %v; want false, nil", found, err)
	}
	if size := fileJournalTestSize(t, path); size != 0 {
		t.Fatalf("recovered journal size = %d, want 0", size)
	}
}

func TestFileJournalCancellationBetweenTransactionAndCommitIsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-cancel-between-entries")
	record := fileJournalTestRecord(stream, 1, 0)
	ctx, cancel := context.WithCancel(context.Background())
	ops := defaultFileJournalOps()
	open := ops.open
	ops.open = func(path string) (fileJournalFile, error) {
		file, err := open(path)
		if err != nil {
			return nil, err
		}
		return &cancelingFileJournalFile{fileJournalFile: file, cancel: cancel}, nil
	}
	journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops)
	if err != nil {
		t.Fatal(err)
	}

	disposition, err := journal.Commit(ctx, Position{}, record)
	if !errors.Is(err, context.Canceled) || disposition != commitUnknown {
		t.Fatalf("canceled commit disposition/error = %v/%v, want unknown/canceled", disposition, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	if _, found, err := journal.Lookup(context.Background(), stream, record.Sequence); err != nil || found {
		t.Fatalf("lookup after recovery = found %v, error %v; want false, nil", found, err)
	}
}

func TestFileJournalSyncFailureIsUnknownUntilReopenFindsTheCompleteCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-commit-sync")
	record := fileJournalTestRecord(stream, 1, 0)
	ops := fileJournalFaultOps(0, 0, 2)
	journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops)
	if err != nil {
		t.Fatal(err)
	}

	disposition, err := journal.Commit(context.Background(), Position{}, record)
	if !errors.Is(err, errInjectedFileJournalFault) || disposition != commitUnknown {
		t.Fatalf("sync failure disposition/error = %v/%v, want unknown/injected fault", disposition, err)
	}
	if _, _, err := journal.Lookup(context.Background(), stream, record.Sequence); err == nil ||
		!strings.Contains(err.Error(), "durability unresolved") {
		t.Fatalf("lookup after sync failure error = %v, want durability unresolved", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	got, found, err := journal.Lookup(context.Background(), stream, record.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Digest != record.Digest {
		t.Fatalf("lookup after recovery = digest %v, found %v; want %v, true", got.Digest, found, record.Digest)
	}
}

func TestFileJournalPartialPublicationWriteReopensAsUnpublished(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-publication-write")
	record := fileJournalTestRecord(stream, 1, 0)
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	fileJournalTestCommit(t, journal, Position{}, record)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	ops := fileJournalFaultOps(1, 9, 0)
	journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.MarkPublished(context.Background(), stream, record.Outbox[0].ID); !errors.Is(err, errInjectedFileJournalFault) {
		t.Fatalf("partial publication error = %v, want injected fault", err)
	}
	if _, _, err := journal.Latest(context.Background(), stream); err == nil ||
		!strings.Contains(err.Error(), "durability unresolved") {
		t.Fatalf("latest after publication write error = %v, want durability unresolved", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	got, found, err := journal.Lookup(context.Background(), stream, record.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Outbox[0].Published {
		t.Fatalf("publication after recovery = found %v, published %v; want true, false", found, got.Outbox[0].Published)
	}
}

func TestFileJournalPublicationSyncFailureReopensAsPublished(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.journal")
	stream := identityFor("file-journal-publication-sync")
	record := fileJournalTestRecord(stream, 1, 0)
	journal := openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	fileJournalTestCommit(t, journal, Position{}, record)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	ops := fileJournalFaultOps(0, 0, 2)
	journal, err := openFileJournal(path, fileJournalTestMaxBytes, ops)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.MarkPublished(context.Background(), stream, record.Outbox[0].ID); !errors.Is(err, errInjectedFileJournalFault) {
		t.Fatalf("publication sync error = %v, want injected fault", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	journal = openFileJournalForTest(t, path, fileJournalTestMaxBytes)
	defer func() { _ = journal.Close() }()
	got, found, err := journal.Lookup(context.Background(), stream, record.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !got.Outbox[0].Published {
		t.Fatalf("publication after recovery = found %v, published %v; want true, true", found, got.Outbox[0].Published)
	}
}

type fileJournalFaultFile struct {
	fileJournalFile
	failWriteCall int
	partialWrite  int
	failSyncCall  int
	writes        int
	syncs         int
}

type cancelingFileJournalFile struct {
	fileJournalFile
	cancel context.CancelFunc
	writes int
}

func (file *cancelingFileJournalFile) Write(data []byte) (int, error) {
	written, err := file.fileJournalFile.Write(data)
	file.writes++
	if file.writes == 1 {
		file.cancel()
	}
	return written, err
}

func (file *fileJournalFaultFile) Write(data []byte) (int, error) {
	file.writes++
	if file.writes != file.failWriteCall {
		return file.fileJournalFile.Write(data)
	}
	partial := file.partialWrite
	if partial > len(data) {
		partial = len(data)
	}
	if partial == 0 {
		return 0, errInjectedFileJournalFault
	}
	written, err := file.fileJournalFile.Write(data[:partial])
	if err != nil {
		return written, err
	}
	return written, errInjectedFileJournalFault
}

func (file *fileJournalFaultFile) Sync() error {
	file.syncs++
	if file.syncs == file.failSyncCall {
		return errInjectedFileJournalFault
	}
	return file.fileJournalFile.Sync()
}

func fileJournalFaultOps(failWriteCall, partialWrite, failSyncCall int) fileJournalOps {
	ops := defaultFileJournalOps()
	open := ops.open
	ops.open = func(path string) (fileJournalFile, error) {
		file, err := open(path)
		if err != nil {
			return nil, err
		}
		return &fileJournalFaultFile{
			fileJournalFile: file,
			failWriteCall:   failWriteCall,
			partialWrite:    partialWrite,
			failSyncCall:    failSyncCall,
		}, nil
	}
	return ops
}
