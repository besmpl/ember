package preparedworker

import "context"

// transactionRecord is one durably committed transaction. Published is mutable delivery
// metadata; every other field, including Digest, is immutable after Commit.
type transactionRecord struct {
	Stream        contentIdentity
	Sequence      uint64
	BaseRevision  uint64
	Revision      uint64
	RequestDigest contentDigest
	Request       []byte
	ResultDigest  contentDigest
	Result        []byte
	Checkpoint    *checkpointRecord
	Quiescent     bool
	Outbox        []outboxRecord
	Digest        contentDigest
}

// checkpointRecord is an application-owned detached checkpoint associated
// with a committed transaction position.
type checkpointRecord struct {
	Schema   contentIdentity
	Position Position
	Payload  []byte
	Digest   contentDigest
}

// outboxID identifies one ordered publication from a logical transaction.
// It is stable across worker generations and process restarts.
type outboxID struct {
	Stream   contentIdentity
	Sequence uint64
	Ordinal  uint32
}

// outboxRecord is one durable post-commit publication. Published is journal
// metadata and is not part of the containing Record's immutable digest.
type outboxRecord struct {
	ID        outboxID
	Payload   []byte
	Digest    contentDigest
	Published bool
}

// commitDisposition reports whether Commit is known to have stored a record.
// An unknown disposition must be resolved through Lookup before a caller may
// expose the result or retry any work.
type commitDisposition uint8

const (
	commitUnknown commitDisposition = iota
	commitStored
	commitNotStored
)

// transactionJournal durably orders transaction records per stream. Commit is a
// compare-and-swap against the stream's current position.
type transactionJournal interface {
	Latest(context.Context, contentIdentity) (transactionRecord, bool, error)
	LatestQuiescent(context.Context, contentIdentity) (transactionRecord, bool, error)
	Lookup(context.Context, contentIdentity, uint64) (transactionRecord, bool, error)
	Commit(context.Context, Position, transactionRecord) (commitDisposition, error)
	MarkPublished(context.Context, contentIdentity, outboxID) error
}

// effectPublisher performs an idempotent external publication. Implementations must
// reject reuse of an OutboxID for different immutable content.
type effectPublisher interface {
	Publish(context.Context, outboxRecord) error
}

func cloneRecord(record transactionRecord) transactionRecord {
	cloned := record
	cloned.Request = append([]byte(nil), record.Request...)
	cloned.Result = append([]byte(nil), record.Result...)
	if record.Checkpoint != nil {
		checkpoint := *record.Checkpoint
		checkpoint.Payload = append([]byte(nil), record.Checkpoint.Payload...)
		cloned.Checkpoint = &checkpoint
	}
	if record.Outbox != nil {
		cloned.Outbox = make([]outboxRecord, len(record.Outbox))
		for index, outbox := range record.Outbox {
			cloned.Outbox[index] = cloneOutboxRecord(outbox)
		}
	}
	return cloned
}

func cloneOutboxRecord(record outboxRecord) outboxRecord {
	cloned := record
	cloned.Payload = append([]byte(nil), record.Payload...)
	return cloned
}
