package preparedworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
)

const (
	fileJournalVersion       = uint16(2)
	fileJournalHeaderSize    = 4 + 2 + 1 + 1 + 4 + sha256DigestBytes*2
	fileJournalMaxEntryBytes = 1 << 30
	fileJournalMaxOutbox     = 1 << 20
)

const sha256DigestBytes = 32

var fileJournalMagic = [4]byte{'E', 'J', 'L', '1'}

type fileJournalEntryKind uint8

const (
	fileJournalTransaction fileJournalEntryKind = iota + 1
	fileJournalCommit
	fileJournalPublished
)

// fileJournal is an exclusive-owner, append-only durable Journal. Open rejects
// a path already owned by another fileJournal in this or another process.
// Complete committed records survive restart; a provably incomplete tail is
// discarded on open.
type fileJournal struct {
	mu        sync.Mutex
	file      fileJournalFile
	path      string
	maxBytes  int64
	size      int64
	last      contentDigest
	streams   map[contentIdentity]*memoryJournalStream
	closed    bool
	uncertain error
}

type fileJournalFile interface {
	io.Reader
	io.Writer
	io.Seeker
	Stat() (os.FileInfo, error)
	Sync() error
	Truncate(int64) error
	Close() error
}

type fileJournalOps struct {
	open          func(string) (fileJournalFile, error)
	syncDirectory func(string) error
}

func defaultFileJournalOps() fileJournalOps {
	return fileJournalOps{
		open:          openAndLockFileJournal,
		syncDirectory: syncFileJournalDirectory,
	}
}

// openDurableJournal opens or creates one exclusive-owner ledger and durably
// establishes its path before returning. maxBytes is a hard bound on the
// complete file, including framing and delivery markers.
func openDurableJournal(path string, maxBytes int64) (*fileJournal, error) {
	return openFileJournal(path, maxBytes, defaultFileJournalOps())
}

func openFileJournal(path string, maxBytes int64, ops fileJournalOps) (*fileJournal, error) {
	if path == "" {
		return nil, fmt.Errorf("prepared worker file journal: empty path")
	}
	if maxBytes < fileJournalHeaderSize*2 || maxBytes > math.MaxInt32 {
		return nil, fmt.Errorf("prepared worker file journal: max bytes %d is out of bounds", maxBytes)
	}
	file, err := ops.open(path)
	if err != nil {
		return nil, fmt.Errorf("prepared worker file journal: open %s: %w", path, err)
	}
	journal := &fileJournal{
		file:     file,
		path:     path,
		maxBytes: maxBytes,
		streams:  make(map[contentIdentity]*memoryJournalStream),
	}
	if err := journal.scan(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("prepared worker file journal: sync recovered ledger: %w", err)
	}
	if err := ops.syncDirectory(path); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("prepared worker file journal: sync containing directory: %w", err)
	}
	if _, err := file.Seek(journal.size, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("prepared worker file journal: seek append position: %w", err)
	}
	return journal, nil
}

func openAndLockFileJournal(path string) (fileJournalFile, error) {
	flags := os.O_RDWR | fileJournalOpenFlags()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|flags, 0o600)
	if errors.Is(err, os.ErrExist) {
		file, err = os.OpenFile(path, flags, 0)
	}
	if err != nil {
		return nil, err
	}
	if err := lockFileJournal(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("exclusive ownership: %w", err)
	}
	return file, nil
}

func (journal *fileJournal) Latest(
	ctx context.Context,
	stream contentIdentity,
) (transactionRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.availableLocked(); err != nil {
		return transactionRecord{}, false, err
	}
	records := journal.streams[stream]
	if records == nil || len(records.records) == 0 {
		return transactionRecord{}, false, nil
	}
	return cloneRecord(records.records[len(records.records)-1]), true, nil
}

func (journal *fileJournal) LatestQuiescent(
	ctx context.Context,
	stream contentIdentity,
) (transactionRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.availableLocked(); err != nil {
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

func (journal *fileJournal) Lookup(
	ctx context.Context,
	stream contentIdentity,
	sequence uint64,
) (transactionRecord, bool, error) {
	if err := contextError(ctx); err != nil {
		return transactionRecord{}, false, err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.availableLocked(); err != nil {
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
	return cloneRecord(records.records[index]), true, nil
}

func (journal *fileJournal) Commit(
	ctx context.Context,
	expected Position,
	record transactionRecord,
) (commitDisposition, error) {
	if err := contextError(ctx); err != nil {
		return commitNotStored, err
	}
	if err := validateLedgerRecord(record); err != nil {
		return commitNotStored, fmt.Errorf("prepared worker file journal: invalid record: %w", err)
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.availableLocked(); err != nil {
		return commitUnknown, err
	}
	if err := contextError(ctx); err != nil {
		return commitNotStored, err
	}
	if records := journal.streams[record.Stream]; records != nil && len(records.records) != 0 {
		if index, exists := records.bySequence[record.Sequence]; exists {
			if records.records[index].Digest == record.Digest {
				return commitStored, nil
			}
			return commitNotStored, nil
		}
		latest := records.records[len(records.records)-1]
		if (Position{Sequence: latest.Sequence, Revision: latest.Revision}) != expected {
			return commitNotStored, nil
		}
	}
	if err := validateContiguousRecord(expected, record); err != nil {
		return commitNotStored, err
	}
	payload, err := encodeFileJournalRecord(record)
	if err != nil {
		return commitNotStored, err
	}
	transaction, transactionDigest, err := encodeFileJournalEntry(
		fileJournalTransaction,
		journal.last,
		payload,
	)
	if err != nil {
		return commitNotStored, err
	}
	commit, commitDigest, err := encodeFileJournalEntry(
		fileJournalCommit,
		transactionDigest,
		transactionDigest[:],
	)
	if err != nil {
		return commitNotStored, err
	}
	entryBytes := int64(len(transaction) + len(commit))
	if entryBytes > journal.maxBytes-journal.size {
		return commitNotStored, fmt.Errorf("prepared worker file journal: max bytes %d exceeded", journal.maxBytes)
	}
	if err := journal.appendAndSyncLocked(ctx, transaction, commit); err != nil {
		return commitUnknown, err
	}
	journal.size += entryBytes
	journal.last = commitDigest
	if err := journal.applyCommittedRecordLocked(record); err != nil {
		journal.uncertain = err
		return commitUnknown, err
	}
	return commitStored, nil
}

func (journal *fileJournal) MarkPublished(
	ctx context.Context,
	stream contentIdentity,
	id outboxID,
) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if id.Stream != stream {
		return fmt.Errorf("prepared worker file journal: outbox stream differs")
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.availableLocked(); err != nil {
		return err
	}
	output, published, err := journal.findOutboxLocked(stream, id)
	if err != nil {
		return err
	}
	if published {
		return nil
	}
	payload := encodeFileJournalPublished(id, output.Digest)
	entry, entryDigest, err := encodeFileJournalEntry(fileJournalPublished, journal.last, payload)
	if err != nil {
		return err
	}
	if int64(len(entry)) > journal.maxBytes-journal.size {
		return fmt.Errorf("prepared worker file journal: max bytes %d exceeded", journal.maxBytes)
	}
	if err := journal.appendAndSyncLocked(ctx, entry); err != nil {
		return err
	}
	journal.size += int64(len(entry))
	journal.last = entryDigest
	return journal.markPublishedLocked(stream, id, output.Digest)
}

// Close releases the file. It is idempotent; an uncertain journal can still
// be closed and then reopened to resolve its complete durable prefix.
func (journal *fileJournal) Close() error {
	if journal == nil {
		return nil
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.closed {
		return nil
	}
	journal.closed = true
	if journal.file == nil {
		return nil
	}
	err := journal.file.Close()
	journal.file = nil
	return err
}

func (journal *fileJournal) availableLocked() error {
	if journal == nil || journal.closed || journal.file == nil {
		return fmt.Errorf("prepared worker file journal: closed")
	}
	if journal.uncertain != nil {
		return fmt.Errorf("prepared worker file journal: durability unresolved: %w", journal.uncertain)
	}
	return nil
}

func (journal *fileJournal) appendAndSyncLocked(ctx context.Context, chunks ...[]byte) error {
	for _, chunk := range chunks {
		if err := contextError(ctx); err != nil {
			journal.uncertain = err
			return err
		}
		if err := writeFull(journal.file, chunk); err != nil {
			journal.uncertain = err
			return fmt.Errorf("prepared worker file journal: append: %w", err)
		}
	}
	if err := journal.file.Sync(); err != nil {
		journal.uncertain = err
		return fmt.Errorf("prepared worker file journal: sync: %w", err)
	}
	return nil
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) != 0 {
		written, err := writer.Write(data)
		if written > 0 {
			data = data[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

type pendingFileJournalTransaction struct {
	record   transactionRecord
	digest   contentDigest
	start    int64
	previous contentDigest
}

func (journal *fileJournal) scan() error {
	metadata, err := journal.file.Stat()
	if err != nil {
		return fmt.Errorf("prepared worker file journal: stat: %w", err)
	}
	if !metadata.Mode().IsRegular() {
		return fmt.Errorf("prepared worker file journal: path is not a regular file")
	}
	if metadata.Size() > journal.maxBytes {
		return fmt.Errorf("prepared worker file journal: file uses %d bytes, limit %d", metadata.Size(), journal.maxBytes)
	}
	if _, err := journal.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("prepared worker file journal: seek start: %w", err)
	}

	var (
		offset  int64
		last    contentDigest
		pending *pendingFileJournalTransaction
	)
	for {
		entryStart := offset
		kind, payload, digest, size, complete, err := readFileJournalEntry(journal.file, last)
		if err != nil {
			return fmt.Errorf("prepared worker file journal: scan at byte %d: %w", entryStart, err)
		}
		if !complete {
			truncateAt := entryStart
			if pending != nil {
				truncateAt = pending.start
				last = pending.previous
			}
			if err := journal.truncateRecoveredTail(truncateAt); err != nil {
				return err
			}
			journal.size = truncateAt
			journal.last = last
			return nil
		}
		offset += size
		switch kind {
		case fileJournalTransaction:
			if pending != nil {
				return fmt.Errorf("prepared worker file journal: transaction without commit")
			}
			record, err := decodeFileJournalRecord(payload)
			if err != nil {
				return fmt.Errorf("decode transaction: %w", err)
			}
			pending = &pendingFileJournalTransaction{
				record: record, digest: digest, start: entryStart, previous: last,
			}
		case fileJournalCommit:
			if pending == nil || len(payload) != len(contentDigest{}) ||
				!bytes.Equal(payload, pending.digest[:]) {
				return fmt.Errorf("prepared worker file journal: commit does not match transaction")
			}
			if err := journal.applyCommittedRecordLocked(pending.record); err != nil {
				return fmt.Errorf("apply transaction: %w", err)
			}
			pending = nil
		case fileJournalPublished:
			if pending != nil {
				return fmt.Errorf("prepared worker file journal: publication interrupts transaction")
			}
			id, outputDigest, err := decodeFileJournalPublished(payload)
			if err != nil {
				return err
			}
			if err := journal.markPublishedLocked(id.Stream, id, outputDigest); err != nil {
				return err
			}
		default:
			return fmt.Errorf("prepared worker file journal: unknown entry kind %d", kind)
		}
		last = digest
	}
}

func (journal *fileJournal) truncateRecoveredTail(size int64) error {
	if err := journal.file.Truncate(size); err != nil {
		return fmt.Errorf("prepared worker file journal: truncate torn tail: %w", err)
	}
	return nil
}

func (journal *fileJournal) applyCommittedRecordLocked(record transactionRecord) error {
	if err := validateLedgerRecord(record); err != nil {
		return err
	}
	records := journal.streams[record.Stream]
	if records == nil {
		records = &memoryJournalStream{bySequence: make(map[uint64]int)}
		journal.streams[record.Stream] = records
	} else if len(records.records) != 0 {
		latest := records.records[len(records.records)-1]
		expected := Position{Sequence: latest.Sequence, Revision: latest.Revision}
		if err := validateContiguousRecord(expected, record); err != nil {
			return err
		}
	}
	if _, duplicate := records.bySequence[record.Sequence]; duplicate {
		return fmt.Errorf("duplicate sequence %d", record.Sequence)
	}
	records.bySequence[record.Sequence] = len(records.records)
	records.records = append(records.records, cloneRecord(record))
	return nil
}

func (journal *fileJournal) findOutboxLocked(
	stream contentIdentity,
	id outboxID,
) (outboxRecord, bool, error) {
	records := journal.streams[stream]
	if records == nil {
		return outboxRecord{}, false, fmt.Errorf("prepared worker file journal: transaction %d is not committed", id.Sequence)
	}
	index, ok := records.bySequence[id.Sequence]
	if !ok {
		return outboxRecord{}, false, fmt.Errorf("prepared worker file journal: transaction %d is not committed", id.Sequence)
	}
	for _, output := range records.records[index].Outbox {
		if output.ID == id {
			return output, output.Published, nil
		}
	}
	return outboxRecord{}, false, fmt.Errorf("prepared worker file journal: outbox %d/%d is not committed", id.Sequence, id.Ordinal)
}

func (journal *fileJournal) markPublishedLocked(stream contentIdentity, id outboxID, digest contentDigest) error {
	records := journal.streams[stream]
	if records == nil {
		return fmt.Errorf("prepared worker file journal: publication has no transaction")
	}
	index, ok := records.bySequence[id.Sequence]
	if !ok {
		return fmt.Errorf("prepared worker file journal: publication transaction is missing")
	}
	for outputIndex := range records.records[index].Outbox {
		output := &records.records[index].Outbox[outputIndex]
		if output.ID != id {
			continue
		}
		if output.Digest != digest {
			return fmt.Errorf("prepared worker file journal: publication digest differs")
		}
		output.Published = true
		return nil
	}
	return fmt.Errorf("prepared worker file journal: publication outbox is missing")
}

func encodeFileJournalEntry(
	kind fileJournalEntryKind,
	previous contentDigest,
	payload []byte,
) ([]byte, contentDigest, error) {
	if len(payload) > fileJournalMaxEntryBytes {
		return nil, contentDigest{}, fmt.Errorf("prepared worker file journal: entry uses %d bytes", len(payload))
	}
	entry := make([]byte, fileJournalHeaderSize+len(payload))
	copy(entry[:4], fileJournalMagic[:])
	binary.BigEndian.PutUint16(entry[4:6], fileJournalVersion)
	entry[6] = byte(kind)
	entry[7] = 0
	binary.BigEndian.PutUint32(entry[8:12], uint32(len(payload)))
	copy(entry[12:44], previous[:])
	payloadDigest := hashContent(payload)
	copy(entry[44:76], payloadDigest[:])
	copy(entry[fileJournalHeaderSize:], payload)
	return entry, hashContent(entry), nil
}

func readFileJournalEntry(
	reader io.Reader,
	previous contentDigest,
) (fileJournalEntryKind, []byte, contentDigest, int64, bool, error) {
	header := make([]byte, fileJournalHeaderSize)
	read, err := io.ReadFull(reader, header)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, nil, contentDigest{}, int64(read), false, nil
	}
	if err != nil {
		return 0, nil, contentDigest{}, int64(read), false, err
	}
	if !bytes.Equal(header[:4], fileJournalMagic[:]) ||
		binary.BigEndian.Uint16(header[4:6]) != fileJournalVersion || header[7] != 0 {
		return 0, nil, contentDigest{}, 0, false, fmt.Errorf("invalid entry header")
	}
	if !bytes.Equal(header[12:44], previous[:]) {
		return 0, nil, contentDigest{}, 0, false, fmt.Errorf("entry hash chain differs")
	}
	length := int(binary.BigEndian.Uint32(header[8:12]))
	if length > fileJournalMaxEntryBytes {
		return 0, nil, contentDigest{}, 0, false, fmt.Errorf("entry length %d is out of bounds", length)
	}
	payload := make([]byte, length)
	read, err = io.ReadFull(reader, payload)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, nil, contentDigest{}, int64(fileJournalHeaderSize + read), false, nil
	}
	if err != nil {
		return 0, nil, contentDigest{}, 0, false, err
	}
	payloadDigest := hashContent(payload)
	if !bytes.Equal(header[44:76], payloadDigest[:]) {
		return 0, nil, contentDigest{}, 0, false, fmt.Errorf("entry payload digest differs")
	}
	entry := append(header, payload...)
	return fileJournalEntryKind(header[6]), payload, hashContent(entry), int64(len(entry)), true, nil
}

type fileJournalEncoder struct{ bytes.Buffer }

func (encoder *fileJournalEncoder) uint8(value uint8) { _ = encoder.WriteByte(value) }
func (encoder *fileJournalEncoder) uint32(value uint32) {
	_ = binary.Write(&encoder.Buffer, binary.BigEndian, value)
}
func (encoder *fileJournalEncoder) uint64(value uint64) {
	_ = binary.Write(&encoder.Buffer, binary.BigEndian, value)
}
func (encoder *fileJournalEncoder) digest(value contentDigest)     { _, _ = encoder.Write(value[:]) }
func (encoder *fileJournalEncoder) identity(value contentIdentity) { encoder.digest(value.digest) }
func (encoder *fileJournalEncoder) bytes(value []byte) {
	encoder.uint32(uint32(len(value)))
	_, _ = encoder.Write(value)
}

func encodeFileJournalRecord(record transactionRecord) ([]byte, error) {
	if err := validateLedgerRecord(record); err != nil {
		return nil, err
	}
	var encoder fileJournalEncoder
	encoder.identity(record.Stream)
	encoder.uint64(record.Sequence)
	encoder.uint64(record.BaseRevision)
	encoder.uint64(record.Revision)
	encoder.digest(record.RequestDigest)
	encoder.bytes(record.Request)
	encoder.digest(record.ResultDigest)
	encoder.bytes(record.Result)
	if record.Checkpoint == nil {
		encoder.uint8(0)
	} else {
		encoder.uint8(1)
		encoder.identity(record.Checkpoint.Schema)
		encoder.uint64(record.Checkpoint.Position.Sequence)
		encoder.uint64(record.Checkpoint.Position.Revision)
		encoder.digest(record.Checkpoint.Digest)
		encoder.bytes(record.Checkpoint.Payload)
	}
	if record.Quiescent {
		encoder.uint8(1)
	} else {
		encoder.uint8(0)
	}
	encoder.uint32(uint32(len(record.Outbox)))
	for _, output := range record.Outbox {
		encoder.identity(output.ID.Stream)
		encoder.uint64(output.ID.Sequence)
		encoder.uint32(output.ID.Ordinal)
		encoder.digest(output.Digest)
		encoder.bytes(output.Payload)
	}
	encoder.digest(record.Digest)
	return encoder.Bytes(), nil
}

type fileJournalDecoder struct {
	data   []byte
	offset int
	err    error
}

func (decoder *fileJournalDecoder) take(size int) []byte {
	if decoder.err != nil {
		return nil
	}
	if size < 0 || size > len(decoder.data)-decoder.offset {
		decoder.err = io.ErrUnexpectedEOF
		return nil
	}
	value := decoder.data[decoder.offset : decoder.offset+size]
	decoder.offset += size
	return value
}

func (decoder *fileJournalDecoder) uint8() uint8 {
	value := decoder.take(1)
	if value == nil {
		return 0
	}
	return value[0]
}

func (decoder *fileJournalDecoder) uint32() uint32 {
	value := decoder.take(4)
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint32(value)
}

func (decoder *fileJournalDecoder) uint64() uint64 {
	value := decoder.take(8)
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func (decoder *fileJournalDecoder) digest() contentDigest {
	var value contentDigest
	copy(value[:], decoder.take(len(value)))
	return value
}

func (decoder *fileJournalDecoder) identity() contentIdentity {
	return contentIdentity{digest: decoder.digest()}
}

func (decoder *fileJournalDecoder) bytes() []byte {
	length := decoder.uint32()
	if uint64(length) > uint64(len(decoder.data)-decoder.offset) {
		decoder.err = io.ErrUnexpectedEOF
		return nil
	}
	return append([]byte(nil), decoder.take(int(length))...)
}

func decodeFileJournalRecord(payload []byte) (transactionRecord, error) {
	decoder := fileJournalDecoder{data: payload}
	record := transactionRecord{
		Stream:        decoder.identity(),
		Sequence:      decoder.uint64(),
		BaseRevision:  decoder.uint64(),
		Revision:      decoder.uint64(),
		RequestDigest: decoder.digest(),
		Request:       decoder.bytes(),
		ResultDigest:  decoder.digest(),
		Result:        decoder.bytes(),
	}
	switch decoder.uint8() {
	case 0:
	case 1:
		record.Checkpoint = &checkpointRecord{
			Schema: decoder.identity(),
			Position: Position{
				Sequence: decoder.uint64(),
				Revision: decoder.uint64(),
			},
			Digest:  decoder.digest(),
			Payload: decoder.bytes(),
		}
	default:
		return transactionRecord{}, fmt.Errorf("invalid checkpoint presence")
	}
	switch decoder.uint8() {
	case 0:
	case 1:
		record.Quiescent = true
	default:
		return transactionRecord{}, fmt.Errorf("invalid quiescence")
	}
	outboxCount := decoder.uint32()
	if outboxCount > fileJournalMaxOutbox {
		return transactionRecord{}, fmt.Errorf("outbox count %d is out of bounds", outboxCount)
	}
	if outboxCount != 0 {
		record.Outbox = make([]outboxRecord, int(outboxCount))
	}
	for index := range record.Outbox {
		record.Outbox[index] = outboxRecord{
			ID: outboxID{
				Stream:   decoder.identity(),
				Sequence: decoder.uint64(),
				Ordinal:  decoder.uint32(),
			},
			Digest:  decoder.digest(),
			Payload: decoder.bytes(),
		}
	}
	record.Digest = decoder.digest()
	if decoder.err != nil {
		return transactionRecord{}, decoder.err
	}
	if decoder.offset != len(decoder.data) {
		return transactionRecord{}, fmt.Errorf("transaction has %d trailing bytes", len(decoder.data)-decoder.offset)
	}
	if err := validateLedgerRecord(record); err != nil {
		return transactionRecord{}, err
	}
	return record, nil
}

func encodeFileJournalPublished(id outboxID, digest contentDigest) []byte {
	var encoder fileJournalEncoder
	encoder.identity(id.Stream)
	encoder.uint64(id.Sequence)
	encoder.uint32(id.Ordinal)
	encoder.digest(digest)
	return encoder.Bytes()
}

func decodeFileJournalPublished(payload []byte) (outboxID, contentDigest, error) {
	decoder := fileJournalDecoder{data: payload}
	id := outboxID{
		Stream: decoder.identity(), Sequence: decoder.uint64(), Ordinal: decoder.uint32(),
	}
	digest := decoder.digest()
	if decoder.err != nil {
		return outboxID{}, contentDigest{}, decoder.err
	}
	if decoder.offset != len(payload) {
		return outboxID{}, contentDigest{}, fmt.Errorf("publication has trailing bytes")
	}
	if !id.Stream.valid() || id.Sequence == 0 || id.Ordinal == 0 || digest == (contentDigest{}) {
		return outboxID{}, contentDigest{}, fmt.Errorf("publication identity is invalid")
	}
	return id, digest, nil
}

func validateLedgerRecord(record transactionRecord) error {
	if !record.Stream.valid() || record.Sequence == 0 ||
		record.BaseRevision == math.MaxUint64 || record.Revision != record.BaseRevision+1 {
		return fmt.Errorf("record identity or position is invalid")
	}
	if record.RequestDigest != hashContent(record.Request) || record.ResultDigest != hashContent(record.Result) {
		return fmt.Errorf("record request or result digest differs")
	}
	if record.Checkpoint != nil {
		if !record.Checkpoint.Schema.valid() ||
			record.Checkpoint.Position != (Position{Sequence: record.Sequence, Revision: record.Revision}) ||
			record.Checkpoint.Digest != hashContent(record.Checkpoint.Payload) {
			return fmt.Errorf("record checkpoint is invalid")
		}
	}
	if record.Quiescent != (record.Checkpoint != nil) {
		return fmt.Errorf("record quiescence and checkpoint presence differ")
	}
	if len(record.Outbox) > fileJournalMaxOutbox {
		return fmt.Errorf("record outbox count is out of bounds")
	}
	for index, output := range record.Outbox {
		want := outboxID{Stream: record.Stream, Sequence: record.Sequence, Ordinal: uint32(index + 1)}
		if output.ID != want || output.Digest != hashContent(output.Payload) || output.Published {
			return fmt.Errorf("record outbox item %d is invalid", index+1)
		}
	}
	if record.Digest != recordDigest(record) {
		return fmt.Errorf("record digest differs")
	}
	return nil
}
