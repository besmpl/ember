package preparedworker

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
)

const workerProtocolVersion = workerartifact.ProtocolVersion

// EPW2 is a private, closed parent/worker protocol. The framing deliberately
// contains no Go encoding or runtime values: both peers validate one bounded
// canonical payload before acting on it.
const (
	processFrameHeaderBytes = 4 + 1 + 1 + 2 + 8 + 4 + sha256DigestBytes
	processMaxFrameBytes    = 1 << 30
	processMaxFailureBytes  = 64 << 10
)

var processFrameMagic = [4]byte{'E', 'P', 'W', '2'}

type processMessageKind uint8

const (
	processMessageHello processMessageKind = iota + 1
	processMessageReady
	processMessageApply
	processMessageReplay
	processMessageDecision
	processMessageFailure
	processMessageClose
	processMessageClosed
)

type processFrame struct {
	Kind        processMessageKind
	Correlation uint64
	Payload     []byte
}

func encodeProcessFrame(frame processFrame, maxPayload int) ([]byte, error) {
	if !frame.Kind.valid() {
		return nil, fmt.Errorf("prepared worker protocol: invalid frame kind %d", frame.Kind)
	}
	if err := validateProcessPayloadSize(len(frame.Payload), maxPayload); err != nil {
		return nil, err
	}
	encoded := make([]byte, processFrameHeaderBytes+len(frame.Payload))
	copy(encoded[:4], processFrameMagic[:])
	encoded[4] = byte(workerProtocolVersion)
	encoded[5] = byte(frame.Kind)
	binary.BigEndian.PutUint16(encoded[6:8], 0)
	binary.BigEndian.PutUint64(encoded[8:16], frame.Correlation)
	binary.BigEndian.PutUint32(encoded[16:20], uint32(len(frame.Payload)))
	digest := hashContent(frame.Payload)
	copy(encoded[20:processFrameHeaderBytes], digest[:])
	copy(encoded[processFrameHeaderBytes:], frame.Payload)
	return encoded, nil
}

func writeProcessFrame(writer io.Writer, frame processFrame, maxPayload int) error {
	if writer == nil {
		return fmt.Errorf("prepared worker protocol: nil writer")
	}
	encoded, err := encodeProcessFrame(frame, maxPayload)
	if err != nil {
		return err
	}
	if err := writeFull(writer, encoded); err != nil {
		return fmt.Errorf("prepared worker protocol: write frame: %w", err)
	}
	return nil
}

func decodeProcessFrame(reader io.Reader, maxPayload int) (processFrame, error) {
	if reader == nil {
		return processFrame{}, fmt.Errorf("prepared worker protocol: nil reader")
	}
	if err := validateProcessPayloadSize(0, maxPayload); err != nil {
		return processFrame{}, err
	}
	header := make([]byte, processFrameHeaderBytes)
	if _, err := io.ReadFull(reader, header); err != nil {
		return processFrame{}, fmt.Errorf("prepared worker protocol: read header: %w", err)
	}
	if !bytes.Equal(header[:4], processFrameMagic[:]) {
		return processFrame{}, fmt.Errorf("prepared worker protocol: invalid magic")
	}
	if version := uint32(header[4]); version != workerProtocolVersion {
		return processFrame{}, fmt.Errorf("prepared worker protocol: version %d, want %d", version, workerProtocolVersion)
	}
	kind := processMessageKind(header[5])
	if !kind.valid() {
		return processFrame{}, fmt.Errorf("prepared worker protocol: unknown frame kind %d", kind)
	}
	if flags := binary.BigEndian.Uint16(header[6:8]); flags != 0 {
		return processFrame{}, fmt.Errorf("prepared worker protocol: unsupported flags %#x", flags)
	}
	length := int(binary.BigEndian.Uint32(header[16:20]))
	if length > maxPayload {
		return processFrame{}, fmt.Errorf("prepared worker protocol: payload length %d exceeds %d", length, maxPayload)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return processFrame{}, fmt.Errorf("prepared worker protocol: read payload: %w", err)
	}
	digest := hashContent(payload)
	if !bytes.Equal(header[20:processFrameHeaderBytes], digest[:]) {
		return processFrame{}, fmt.Errorf("prepared worker protocol: payload digest differs")
	}
	return processFrame{
		Kind:        kind,
		Correlation: binary.BigEndian.Uint64(header[8:16]),
		Payload:     payload,
	}, nil
}

func validateProcessPayloadSize(size, limit int) error {
	if limit < 0 || limit > processMaxFrameBytes {
		return fmt.Errorf("prepared worker protocol: invalid payload limit %d", limit)
	}
	if size > limit {
		return fmt.Errorf("prepared worker protocol: payload uses %d bytes, limit %d", size, limit)
	}
	return nil
}

func (kind processMessageKind) valid() bool {
	return kind >= processMessageHello && kind <= processMessageClosed
}

type processHello struct {
	Artifact         contentIdentity
	BuildID          contentDigest
	Contract         contentIdentity
	Stream           contentIdentity
	CheckpointSchema contentIdentity
	Position         Position
	CheckpointDigest contentDigest
	Checkpoint       []byte
	Limits           transactionLimits
}

type processReady struct {
	Artifact         contentIdentity
	BuildID          contentDigest
	Contract         contentIdentity
	Stream           contentIdentity
	CheckpointSchema contentIdentity
	Position         Position
	CheckpointDigest contentDigest
	Limits           transactionLimits
}

type processReplay struct {
	Operation      encodedOperation
	ExpectedRecord contentDigest
}

type processFailure struct {
	Kind      FailureKind
	Operation string
	Message   string
}

type processEncoder struct{ bytes.Buffer }

func (encoder *processEncoder) uint8(value uint8) { _ = encoder.WriteByte(value) }
func (encoder *processEncoder) uint32(value uint32) {
	_ = binary.Write(&encoder.Buffer, binary.BigEndian, value)
}
func (encoder *processEncoder) uint64(value uint64) {
	_ = binary.Write(&encoder.Buffer, binary.BigEndian, value)
}
func (encoder *processEncoder) digest(value contentDigest)     { _, _ = encoder.Write(value[:]) }
func (encoder *processEncoder) identity(value contentIdentity) { encoder.digest(value.digest) }
func (encoder *processEncoder) bytes(value []byte) {
	encoder.uint32(uint32(len(value)))
	_, _ = encoder.Write(value)
}
func (encoder *processEncoder) string(value string) { encoder.bytes([]byte(value)) }
func (encoder *processEncoder) position(value Position) {
	encoder.uint64(value.Sequence)
	encoder.uint64(value.Revision)
}
func (encoder *processEncoder) limits(value transactionLimits) {
	for _, limit := range []int{
		value.MaxRequestBytes,
		value.MaxResultBytes,
		value.MaxCheckpointBytes,
		value.MaxOutboxItems,
		value.MaxOutboxBytes,
		value.MaxCandidates,
		value.MaxRetired,
	} {
		encoder.uint64(uint64(limit))
	}
}

type processDecoder struct {
	data   []byte
	offset int
	err    error
}

func (decoder *processDecoder) take(size int) []byte {
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

func (decoder *processDecoder) uint8() uint8 {
	value := decoder.take(1)
	if value == nil {
		return 0
	}
	return value[0]
}

func (decoder *processDecoder) uint32() uint32 {
	value := decoder.take(4)
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint32(value)
}

func (decoder *processDecoder) uint64() uint64 {
	value := decoder.take(8)
	if value == nil {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func (decoder *processDecoder) digest() contentDigest {
	var value contentDigest
	copy(value[:], decoder.take(len(value)))
	return value
}

func (decoder *processDecoder) identity() contentIdentity {
	return contentIdentity{digest: decoder.digest()}
}

func (decoder *processDecoder) bytes(limit int, label string) []byte {
	length := decoder.uint32()
	if decoder.err != nil {
		return nil
	}
	if uint64(length) > uint64(limit) {
		decoder.err = fmt.Errorf("%s uses %d bytes, limit %d", label, length, limit)
		return nil
	}
	if uint64(length) > uint64(len(decoder.data)-decoder.offset) {
		decoder.err = io.ErrUnexpectedEOF
		return nil
	}
	return append([]byte(nil), decoder.take(int(length))...)
}

func (decoder *processDecoder) string(limit int, label string) string {
	return string(decoder.bytes(limit, label))
}

func (decoder *processDecoder) position() Position {
	return Position{Sequence: decoder.uint64(), Revision: decoder.uint64()}
}

func (decoder *processDecoder) limits() transactionLimits {
	values := make([]int, 7)
	for index := range values {
		value := decoder.uint64()
		if value > uint64(math.MaxInt) {
			decoder.err = fmt.Errorf("limit is outside int range")
			return transactionLimits{}
		}
		values[index] = int(value)
	}
	return transactionLimits{
		MaxRequestBytes: values[0], MaxResultBytes: values[1], MaxCheckpointBytes: values[2],
		MaxOutboxItems: values[3], MaxOutboxBytes: values[4], MaxCandidates: values[5], MaxRetired: values[6],
	}
}

func (decoder *processDecoder) finish(label string) error {
	if decoder.err != nil {
		return fmt.Errorf("prepared worker protocol: decode %s: %w", label, decoder.err)
	}
	if decoder.offset != len(decoder.data) {
		return fmt.Errorf("prepared worker protocol: %s has %d trailing bytes", label, len(decoder.data)-decoder.offset)
	}
	return nil
}

func encodeProcessHello(message processHello) ([]byte, error) {
	if err := validateProcessHello(message); err != nil {
		return nil, err
	}
	var encoder processEncoder
	encoder.identity(message.Artifact)
	encoder.digest(message.BuildID)
	encoder.identity(message.Contract)
	encoder.identity(message.Stream)
	encoder.identity(message.CheckpointSchema)
	encoder.position(message.Position)
	encoder.digest(message.CheckpointDigest)
	encoder.bytes(message.Checkpoint)
	encoder.limits(message.Limits)
	return encoder.Bytes(), nil
}

func decodeProcessHello(payload []byte, maxCheckpointBytes int) (processHello, error) {
	decoder := processDecoder{data: payload}
	message := processHello{
		Artifact: decoder.identity(), BuildID: decoder.digest(), Contract: decoder.identity(),
		Stream: decoder.identity(), CheckpointSchema: decoder.identity(), Position: decoder.position(),
		CheckpointDigest: decoder.digest(), Checkpoint: decoder.bytes(maxCheckpointBytes, "checkpoint"),
		Limits: decoder.limits(),
	}
	if err := decoder.finish("HELLO"); err != nil {
		return processHello{}, err
	}
	if err := validateProcessHello(message); err != nil {
		return processHello{}, err
	}
	if message.Limits.MaxCheckpointBytes != maxCheckpointBytes {
		return processHello{}, fmt.Errorf("prepared worker protocol: HELLO checkpoint bound differs")
	}
	return message, nil
}

func validateProcessHello(message processHello) error {
	if !message.Artifact.valid() || message.BuildID == (contentDigest{}) || !message.Contract.valid() ||
		!message.Stream.valid() || !message.CheckpointSchema.valid() {
		return fmt.Errorf("prepared worker protocol: HELLO identity is incomplete")
	}
	if message.CheckpointDigest != hashContent(message.Checkpoint) {
		return fmt.Errorf("prepared worker protocol: HELLO checkpoint digest differs")
	}
	if err := message.Limits.validate(); err != nil {
		return err
	}
	if len(message.Checkpoint) > message.Limits.MaxCheckpointBytes {
		return fmt.Errorf("prepared worker protocol: HELLO checkpoint exceeds limit")
	}
	return nil
}

func encodeProcessReady(message processReady) ([]byte, error) {
	if err := validateProcessReady(message); err != nil {
		return nil, err
	}
	var encoder processEncoder
	encoder.identity(message.Artifact)
	encoder.digest(message.BuildID)
	encoder.identity(message.Contract)
	encoder.identity(message.Stream)
	encoder.identity(message.CheckpointSchema)
	encoder.position(message.Position)
	encoder.digest(message.CheckpointDigest)
	encoder.limits(message.Limits)
	return encoder.Bytes(), nil
}

func decodeProcessReady(payload []byte) (processReady, error) {
	decoder := processDecoder{data: payload}
	message := processReady{
		Artifact: decoder.identity(), BuildID: decoder.digest(), Contract: decoder.identity(),
		Stream: decoder.identity(), CheckpointSchema: decoder.identity(), Position: decoder.position(),
		CheckpointDigest: decoder.digest(), Limits: decoder.limits(),
	}
	if err := decoder.finish("READY"); err != nil {
		return processReady{}, err
	}
	if err := validateProcessReady(message); err != nil {
		return processReady{}, err
	}
	return message, nil
}

func validateProcessReady(message processReady) error {
	if !message.Artifact.valid() || message.BuildID == (contentDigest{}) || !message.Contract.valid() ||
		!message.Stream.valid() || !message.CheckpointSchema.valid() || message.CheckpointDigest == (contentDigest{}) {
		return fmt.Errorf("prepared worker protocol: READY identity is incomplete")
	}
	return message.Limits.validate()
}

func encodeProcessApply(operation encodedOperation, maxRequestBytes int) ([]byte, error) {
	if operation.Sequence == 0 || operation.BaseRevision == math.MaxUint64 {
		return nil, fmt.Errorf("prepared worker protocol: invalid operation position")
	}
	if len(operation.Request) > maxRequestBytes {
		return nil, fmt.Errorf("prepared worker protocol: request exceeds limit")
	}
	var encoder processEncoder
	encoder.uint64(operation.Sequence)
	encoder.uint64(operation.BaseRevision)
	encoder.bytes(operation.Request)
	return encoder.Bytes(), nil
}

func decodeProcessApply(payload []byte, maxRequestBytes int) (encodedOperation, error) {
	decoder := processDecoder{data: payload}
	operation := encodedOperation{
		Sequence: decoder.uint64(), BaseRevision: decoder.uint64(),
		Request: decoder.bytes(maxRequestBytes, "request"),
	}
	if err := decoder.finish("APPLY"); err != nil {
		return encodedOperation{}, err
	}
	if operation.Sequence == 0 || operation.BaseRevision == math.MaxUint64 {
		return encodedOperation{}, fmt.Errorf("prepared worker protocol: invalid operation position")
	}
	return operation, nil
}

func encodeProcessDecision(decision wireDecision, limits transactionLimits) ([]byte, error) {
	if err := validateProcessDecision(decision, limits); err != nil {
		return nil, err
	}
	var encoder processEncoder
	encoder.position(decision.Position)
	encoder.bytes(decision.Result)
	if decision.HasCheckpoint {
		encoder.uint8(1)
		encoder.bytes(decision.Checkpoint)
	} else {
		encoder.uint8(0)
	}
	if decision.Quiescent {
		encoder.uint8(1)
	} else {
		encoder.uint8(0)
	}
	encoder.uint32(uint32(len(decision.Effects)))
	for _, effect := range decision.Effects {
		encoder.bytes(effect)
	}
	return encoder.Bytes(), nil
}

func decodeProcessDecision(payload []byte, limits transactionLimits) (wireDecision, error) {
	if err := limits.validate(); err != nil {
		return wireDecision{}, err
	}
	decoder := processDecoder{data: payload}
	decision := wireDecision{
		Position: decoder.position(),
		Result:   decoder.bytes(limits.MaxResultBytes, "result"),
	}
	switch decoder.uint8() {
	case 0:
	case 1:
		decision.HasCheckpoint = true
		decision.Checkpoint = decoder.bytes(limits.MaxCheckpointBytes, "checkpoint")
	default:
		return wireDecision{}, fmt.Errorf("prepared worker protocol: invalid checkpoint presence")
	}
	switch decoder.uint8() {
	case 0:
	case 1:
		decision.Quiescent = true
	default:
		return wireDecision{}, fmt.Errorf("prepared worker protocol: invalid quiescence")
	}
	effectCount := decoder.uint32()
	if uint64(effectCount) > uint64(limits.MaxOutboxItems) {
		return wireDecision{}, fmt.Errorf(
			"prepared worker protocol: effect count %d exceeds %d",
			effectCount,
			limits.MaxOutboxItems,
		)
	}
	if effectCount != 0 {
		decision.Effects = make([][]byte, int(effectCount))
	}
	remainingEffects := limits.MaxOutboxBytes
	for index := range decision.Effects {
		decision.Effects[index] = decoder.bytes(remainingEffects, "effect")
		remainingEffects -= len(decision.Effects[index])
	}
	if err := decoder.finish("DECISION"); err != nil {
		return wireDecision{}, err
	}
	if err := validateProcessDecision(decision, limits); err != nil {
		return wireDecision{}, err
	}
	return decision, nil
}

func validateProcessDecision(decision wireDecision, limits transactionLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	if decision.Position.Sequence == 0 || decision.Position.Revision == 0 ||
		decision.HasCheckpoint != decision.Quiescent {
		return fmt.Errorf("prepared worker protocol: invalid decision shape")
	}
	if len(decision.Result) > limits.MaxResultBytes {
		return fmt.Errorf("prepared worker protocol: result exceeds limit")
	}
	if len(decision.Checkpoint) > limits.MaxCheckpointBytes {
		return fmt.Errorf("prepared worker protocol: checkpoint exceeds limit")
	}
	if len(decision.Effects) > limits.MaxOutboxItems {
		return fmt.Errorf("prepared worker protocol: effect count exceeds limit")
	}
	total := 0
	for _, effect := range decision.Effects {
		if len(effect) > limits.MaxOutboxBytes-total {
			return fmt.Errorf("prepared worker protocol: effects exceed byte limit")
		}
		total += len(effect)
	}
	return nil
}

func encodeProcessReplay(replay processReplay, maxRequestBytes int) ([]byte, error) {
	operation, err := encodeProcessApply(replay.Operation, maxRequestBytes)
	if err != nil {
		return nil, err
	}
	if replay.ExpectedRecord == (contentDigest{}) {
		return nil, fmt.Errorf("prepared worker protocol: zero replay digest")
	}
	var encoder processEncoder
	encoder.bytes(operation)
	encoder.digest(replay.ExpectedRecord)
	return encoder.Bytes(), nil
}

func decodeProcessReplay(payload []byte, maxRequestBytes int) (processReplay, error) {
	decoder := processDecoder{data: payload}
	operationPayload := decoder.bytes(maxRequestBytes+20, "replay operation")
	replay := processReplay{ExpectedRecord: decoder.digest()}
	if err := decoder.finish("REPLAY"); err != nil {
		return processReplay{}, err
	}
	operation, err := decodeProcessApply(operationPayload, maxRequestBytes)
	if err != nil {
		return processReplay{}, err
	}
	if replay.ExpectedRecord == (contentDigest{}) {
		return processReplay{}, fmt.Errorf("prepared worker protocol: zero replay digest")
	}
	replay.Operation = operation
	return replay, nil
}

func encodeProcessFailure(failure processFailure, maxMessageBytes int) ([]byte, error) {
	if failure.Kind == "" || failure.Operation == "" || failure.Message == "" ||
		len(failure.Operation) > 256 || len(failure.Message) > maxMessageBytes ||
		maxMessageBytes <= 0 || maxMessageBytes > processMaxFailureBytes {
		return nil, fmt.Errorf("prepared worker protocol: invalid failure")
	}
	var encoder processEncoder
	encoder.string(string(failure.Kind))
	encoder.string(failure.Operation)
	encoder.string(failure.Message)
	return encoder.Bytes(), nil
}

func decodeProcessFailure(payload []byte, maxMessageBytes int) (processFailure, error) {
	if maxMessageBytes <= 0 || maxMessageBytes > processMaxFailureBytes {
		return processFailure{}, fmt.Errorf("prepared worker protocol: invalid failure limit")
	}
	decoder := processDecoder{data: payload}
	failure := processFailure{
		Kind:      FailureKind(decoder.string(64, "failure kind")),
		Operation: decoder.string(256, "failure operation"),
		Message:   decoder.string(maxMessageBytes, "failure message"),
	}
	if err := decoder.finish("FAIL"); err != nil {
		return processFailure{}, err
	}
	if _, err := encodeProcessFailure(failure, maxMessageBytes); err != nil {
		return processFailure{}, err
	}
	return failure, nil
}

func encodeProcessClose() []byte { return nil }

func decodeProcessClose(payload []byte) error {
	if len(payload) != 0 {
		return fmt.Errorf("prepared worker protocol: CLOSE has %d trailing bytes", len(payload))
	}
	return nil
}
