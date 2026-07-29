package preparedworker

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sync"
)

// memoryJournal is a thread-safe in-memory Journal adapter. It is intended for
// contract tests and embedded hosts that do not require restart durability.
type memoryJournal struct {
	mu      sync.Mutex
	streams map[contentIdentity]*memoryJournalStream
}

type memoryJournalStream struct {
	records    []transactionRecord
	bySequence map[uint64]int
}

// newMemoryJournal returns an empty in-memory journal.
func newMemoryJournal() *memoryJournal {
	return &memoryJournal{streams: make(map[contentIdentity]*memoryJournalStream)}
}

func (journal *memoryJournal) Latest(
	ctx context.Context,
	stream contentIdentity,
) (transactionRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	if journal == nil {
		return transactionRecord{}, false, fmt.Errorf("prepared worker memory journal: nil journal")
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	records := journal.streams[stream]
	if records == nil || len(records.records) == 0 {
		return transactionRecord{}, false, nil
	}
	return cloneRecord(records.records[len(records.records)-1]), true, nil
}

func (journal *memoryJournal) LatestQuiescent(
	ctx context.Context,
	stream contentIdentity,
) (transactionRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	if journal == nil {
		return transactionRecord{}, false, fmt.Errorf("prepared worker memory journal: nil journal")
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	streamRecords := journal.streams[stream]
	if streamRecords == nil {
		return transactionRecord{}, false, nil
	}
	for index := len(streamRecords.records) - 1; index >= 0; index-- {
		record := streamRecords.records[index]
		if record.Checkpoint != nil {
			return cloneRecord(record), true, nil
		}
	}
	return transactionRecord{}, false, nil
}

func (journal *memoryJournal) Lookup(
	ctx context.Context,
	stream contentIdentity,
	sequence uint64,
) (transactionRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	if journal == nil {
		return transactionRecord{}, false, fmt.Errorf("prepared worker memory journal: nil journal")
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	records := journal.streams[stream]
	if records == nil {
		return transactionRecord{}, false, nil
	}
	index, ok := records.bySequence[sequence]
	if !ok {
		return transactionRecord{}, false, nil
	}
	record := records.records[index]
	if record.Sequence != sequence {
		return transactionRecord{}, false, fmt.Errorf(
			"prepared worker memory journal: corrupt stream sequence %d contains %d",
			sequence,
			record.Sequence,
		)
	}
	return cloneRecord(record), true, nil
}

func (journal *memoryJournal) Commit(
	ctx context.Context,
	expected Position,
	record transactionRecord,
) (commitDisposition, error) {
	if err := contextError(ctx); err != nil {
		return commitNotStored, err
	}
	if journal == nil {
		return commitNotStored, fmt.Errorf("prepared worker memory journal: nil journal")
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return commitNotStored, err
	}
	records := journal.streams[record.Stream]
	if records != nil && len(records.records) != 0 {
		if index, exists := records.bySequence[record.Sequence]; exists {
			if records.records[index].Digest == record.Digest {
				return commitStored, nil
			}
			return commitNotStored, nil
		}
		latest := records.records[len(records.records)-1]
		actual := Position{Sequence: latest.Sequence, Revision: latest.Revision}
		if actual != expected {
			return commitNotStored, nil
		}
	}
	if err := validateContiguousRecord(expected, record); err != nil {
		return commitNotStored, err
	}
	if records == nil {
		records = &memoryJournalStream{bySequence: make(map[uint64]int)}
		if journal.streams == nil {
			journal.streams = make(map[contentIdentity]*memoryJournalStream)
		}
		journal.streams[record.Stream] = records
	}
	if records.bySequence == nil {
		records.bySequence = make(map[uint64]int)
	}
	records.bySequence[record.Sequence] = len(records.records)
	records.records = append(records.records, cloneRecord(record))
	return commitStored, nil
}

func (journal *memoryJournal) MarkPublished(
	ctx context.Context,
	stream contentIdentity,
	id outboxID,
) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if journal == nil {
		return fmt.Errorf("prepared worker memory journal: nil journal")
	}
	if id.Stream != stream {
		return fmt.Errorf("prepared worker memory journal: outbox stream does not match journal stream")
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	records := journal.streams[stream]
	if records == nil {
		return fmt.Errorf("prepared worker memory journal: outbox transaction %d is not committed", id.Sequence)
	}
	index, ok := records.bySequence[id.Sequence]
	if !ok {
		return fmt.Errorf("prepared worker memory journal: outbox transaction %d is not committed", id.Sequence)
	}
	record := &records.records[index]
	if record.Sequence != id.Sequence {
		return fmt.Errorf("prepared worker memory journal: corrupt outbox transaction %d", id.Sequence)
	}
	for index := range record.Outbox {
		if record.Outbox[index].ID == id {
			record.Outbox[index].Published = true
			return nil
		}
	}
	return fmt.Errorf(
		"prepared worker memory journal: outbox record %d/%d is not committed",
		id.Sequence,
		id.Ordinal,
	)
}

func validateContiguousRecord(expected Position, record transactionRecord) error {
	if expected.Sequence == math.MaxUint64 || record.Sequence == 0 ||
		record.Sequence != expected.Sequence+1 {
		return fmt.Errorf(
			"prepared worker memory journal: sequence %d does not follow %d",
			record.Sequence,
			expected.Sequence,
		)
	}
	if expected.Revision == math.MaxUint64 || record.BaseRevision != expected.Revision ||
		record.Revision != expected.Revision+1 {
		return fmt.Errorf(
			"prepared worker memory journal: revision %d/%d does not follow %d",
			record.BaseRevision,
			record.Revision,
			expected.Revision,
		)
	}
	if record.Checkpoint != nil && record.Checkpoint.Position != (Position{
		Sequence: record.Sequence,
		Revision: record.Revision,
	}) {
		return fmt.Errorf("prepared worker memory journal: checkpoint position does not match record")
	}
	seenOutbox := make(map[outboxID]struct{}, len(record.Outbox))
	for index, outbox := range record.Outbox {
		want := outboxID{Stream: record.Stream, Sequence: record.Sequence, Ordinal: uint32(index + 1)}
		if outbox.ID != want {
			return fmt.Errorf(
				"prepared worker memory journal: outbox ID at index %d is not contiguous",
				index,
			)
		}
		if _, exists := seenOutbox[outbox.ID]; exists {
			return fmt.Errorf("prepared worker memory journal: duplicate outbox ID")
		}
		seenOutbox[outbox.ID] = struct{}{}
	}
	return nil
}

// memoryPublisher is a thread-safe idempotent Publisher adapter.
type memoryPublisher struct {
	mu      sync.Mutex
	records []outboxRecord
	byID    map[outboxID]outboxRecord
}

// newMemoryPublisher returns an empty in-memory publisher.
func newMemoryPublisher() *memoryPublisher {
	return &memoryPublisher{byID: make(map[outboxID]outboxRecord)}
}

func (publisher *memoryPublisher) Publish(ctx context.Context, record outboxRecord) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if publisher == nil {
		return fmt.Errorf("prepared worker memory publisher: nil publisher")
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if previous, ok := publisher.byID[record.ID]; ok {
		if previous.Digest != record.Digest || !bytes.Equal(previous.Payload, record.Payload) {
			return fmt.Errorf(
				"prepared worker memory publisher: outbox ID %d/%d conflicts with prior publication",
				record.ID.Sequence,
				record.ID.Ordinal,
			)
		}
		return nil
	}
	cloned := cloneOutboxRecord(record)
	if publisher.byID == nil {
		publisher.byID = make(map[outboxID]outboxRecord)
	}
	publisher.byID[record.ID] = cloned
	publisher.records = append(publisher.records, cloned)
	return nil
}

// Records returns the unique publications in first-publication order.
func (publisher *memoryPublisher) Records() []outboxRecord {
	if publisher == nil {
		return nil
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	records := make([]outboxRecord, len(publisher.records))
	for index, record := range publisher.records {
		records[index] = cloneOutboxRecord(record)
	}
	return records
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
