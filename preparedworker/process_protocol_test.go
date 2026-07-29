package preparedworker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"
)

const (
	testProcessFrameHeaderSize   = 52
	testProcessFrameFlagsOffset  = 6
	testProcessFrameLengthOffset = 16
	testProcessFrameDigestOffset = 20
)

func TestProcessFrameRoundTripsCorrelationAndPayloadDigest(t *testing.T) {
	want := processFrame{
		Kind:        processMessageApply,
		Correlation: 0x0102030405060708,
		Payload:     []byte("typed application payload"),
	}
	encoded, err := encodeProcessFrame(want, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != testProcessFrameHeaderSize+len(want.Payload) {
		t.Fatalf("encoded frame bytes = %d, want %d", len(encoded), testProcessFrameHeaderSize+len(want.Payload))
	}
	if got := string(encoded[:4]); got != "EPW2" {
		t.Fatalf("frame magic = %q, want EPW2", got)
	}
	if got := binary.BigEndian.Uint64(encoded[8:16]); got != want.Correlation {
		t.Fatalf("frame correlation = %#x, want %#x", got, want.Correlation)
	}
	if got := binary.BigEndian.Uint32(encoded[testProcessFrameLengthOffset:testProcessFrameDigestOffset]); got != uint32(len(want.Payload)) {
		t.Fatalf("frame payload length = %d, want %d", got, len(want.Payload))
	}
	wantDigest := hashContent(want.Payload)
	if got := encoded[testProcessFrameDigestOffset:testProcessFrameHeaderSize]; !bytes.Equal(got, wantDigest[:]) {
		t.Fatalf("frame digest = %x, want %x", got, wantDigest)
	}

	decoded, err := decodeProcessFrame(bytes.NewReader(encoded), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded frame = %#v, want %#v", decoded, want)
	}
}

func TestProcessFrameRejectsOversizeBeforeReadingPayload(t *testing.T) {
	encoded, err := encodeProcessFrame(processFrame{
		Kind:        processMessageDecision,
		Correlation: 9,
		Payload:     []byte("payload"),
	}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	header := append([]byte(nil), encoded[:testProcessFrameHeaderSize]...)
	binary.BigEndian.PutUint32(
		header[testProcessFrameLengthOffset:testProcessFrameDigestOffset],
		1025,
	)
	reader := &headerThenForbiddenReader{header: header}
	if _, err := decodeProcessFrame(reader, 1024); err == nil {
		t.Fatal("oversized frame decoded")
	}
	if reader.payloadRead {
		t.Fatal("decoder read payload before rejecting its declared size")
	}
}

func TestProcessFrameRejectsMalformedHeaderDigestAndTruncation(t *testing.T) {
	valid, err := encodeProcessFrame(processFrame{
		Kind:        processMessageApply,
		Correlation: 17,
		Payload:     []byte("payload"),
	}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "magic", mutate: func(frame []byte) []byte { frame[0] ^= 0xff; return frame }},
		{name: "version", mutate: func(frame []byte) []byte { frame[4]++; return frame }},
		{name: "flags", mutate: func(frame []byte) []byte {
			binary.BigEndian.PutUint16(frame[testProcessFrameFlagsOffset:8], 1)
			return frame
		}},
		{name: "digest", mutate: func(frame []byte) []byte {
			frame[testProcessFrameDigestOffset] ^= 0xff
			return frame
		}},
		{name: "header truncation", mutate: func(frame []byte) []byte {
			return frame[:testProcessFrameHeaderSize-1]
		}},
		{name: "payload truncation", mutate: func(frame []byte) []byte { return frame[:len(frame)-1] }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame := test.mutate(append([]byte(nil), valid...))
			if _, err := decodeProcessFrame(bytes.NewReader(frame), 1024); err == nil {
				t.Fatal("malformed frame decoded")
			}
		})
	}
}

func TestProcessHelloAndReadyRoundTripExactIdentityLimitsAndCheckpoint(t *testing.T) {
	limits := processProtocolTestLimits()
	checkpoint := []byte("canonical detached checkpoint")
	hello := processHello{
		Artifact:         identityFor("artifact"),
		BuildID:          hashContent([]byte("build")),
		Contract:         identityFor("contract"),
		Stream:           identityFor("stream"),
		CheckpointSchema: identityFor("checkpoint-v2"),
		Position:         Position{Sequence: 41, Revision: 37},
		CheckpointDigest: hashContent(checkpoint),
		Checkpoint:       checkpoint,
		Limits:           limits,
	}
	encodedHello, err := encodeProcessHello(hello)
	if err != nil {
		t.Fatal(err)
	}
	decodedHello, err := decodeProcessHello(encodedHello, limits.MaxCheckpointBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedHello, hello) {
		t.Fatalf("decoded HELLO = %#v, want %#v", decodedHello, hello)
	}

	ready := processReady{
		Artifact:         hello.Artifact,
		BuildID:          hello.BuildID,
		Contract:         hello.Contract,
		Stream:           hello.Stream,
		CheckpointSchema: hello.CheckpointSchema,
		Position:         hello.Position,
		CheckpointDigest: hello.CheckpointDigest,
		Limits:           limits,
	}
	encodedReady, err := encodeProcessReady(ready)
	if err != nil {
		t.Fatal(err)
	}
	decodedReady, err := decodeProcessReady(encodedReady)
	if err != nil {
		t.Fatal(err)
	}
	if decodedReady != ready {
		t.Fatalf("decoded READY = %#v, want %#v", decodedReady, ready)
	}
}

func TestProcessApplyAndDecisionRoundTripClosedTypedRecords(t *testing.T) {
	operation := encodedOperation{
		Sequence:     42,
		BaseRevision: 37,
		Request:      []byte("canonical request"),
	}
	encodedApply, err := encodeProcessApply(operation, 1024)
	if err != nil {
		t.Fatal(err)
	}
	decodedApply, err := decodeProcessApply(encodedApply, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedApply, operation) {
		t.Fatalf("decoded APPLY = %#v, want %#v", decodedApply, operation)
	}

	decision := wireDecision{
		Position:      Position{Sequence: 42, Revision: 38},
		Result:        []byte("canonical result"),
		Checkpoint:    []byte("canonical checkpoint"),
		HasCheckpoint: true,
		Effects: [][]byte{
			[]byte("effect one"),
			[]byte("effect two"),
		},
		Quiescent: true,
	}
	encodedDecision, err := encodeProcessDecision(decision, processProtocolTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	decodedDecision, err := decodeProcessDecision(encodedDecision, processProtocolTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedDecision, decision) {
		t.Fatalf("decoded DECISION = %#v, want %#v", decodedDecision, decision)
	}
}

func TestProcessDecisionRejectsTrailingInvalidBooleanAndCount(t *testing.T) {
	limits := processProtocolTestLimits()
	decision := wireDecision{
		Position:      Position{Sequence: 1, Revision: 1},
		Result:        []byte("result"),
		Checkpoint:    []byte("checkpoint"),
		HasCheckpoint: true,
		Effects:       [][]byte{[]byte("effect")},
		Quiescent:     true,
	}
	valid, err := encodeProcessDecision(decision, limits)
	if err != nil {
		t.Fatal(err)
	}
	// DECISION is position, result blob, checkpoint-presence bool, optional
	// checkpoint blob, quiescent bool, effect count, then effect blobs.
	hasCheckpointOffset := 16 + 4 + len(decision.Result)
	quiescentOffset := hasCheckpointOffset + 1 + 4 + len(decision.Checkpoint)
	effectCountOffset := quiescentOffset + 1
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "trailing", mutate: func(payload []byte) []byte { return append(payload, 0) }},
		{name: "checkpoint bool", mutate: func(payload []byte) []byte {
			payload[hasCheckpointOffset] = 2
			return payload
		}},
		{name: "quiescent bool", mutate: func(payload []byte) []byte {
			payload[quiescentOffset] = 2
			return payload
		}},
		{name: "effect count", mutate: func(payload []byte) []byte {
			binary.BigEndian.PutUint32(payload[effectCountOffset:effectCountOffset+4], uint32(limits.MaxOutboxItems+1))
			return payload
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := test.mutate(append([]byte(nil), valid...))
			if _, err := decodeProcessDecision(payload, limits); err == nil {
				t.Fatal("malformed DECISION decoded")
			}
		})
	}
}

func TestProcessFailureAndCloseUseBoundedCanonicalForms(t *testing.T) {
	want := processFailure{
		Kind:      FailureGuest,
		Operation: "apply",
		Message:   "script rejected the turn",
	}
	encoded, err := encodeProcessFailure(want, 128)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeProcessFailure(encoded, 128)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != want {
		t.Fatalf("decoded FAIL = %#v, want %#v", decoded, want)
	}
	oversized := want
	oversized.Message = string(bytes.Repeat([]byte{'x'}, 129))
	if _, err := encodeProcessFailure(oversized, 128); err == nil {
		t.Fatal("oversized FAIL encoded")
	}
	if _, err := decodeProcessFailure(append(encoded, 0), 128); err == nil {
		t.Fatal("FAIL with trailing bytes decoded")
	}

	closePayload := encodeProcessClose()
	if len(closePayload) != 0 {
		t.Fatalf("CLOSE payload = %x, want empty bounded form", closePayload)
	}
	if err := decodeProcessClose(closePayload); err != nil {
		t.Fatal(err)
	}
	if err := decodeProcessClose([]byte{0}); err == nil {
		t.Fatal("nonempty CLOSE decoded")
	}
}

func processProtocolTestLimits() transactionLimits {
	return transactionLimits{
		MaxRequestBytes:    1024,
		MaxResultBytes:     2048,
		MaxCheckpointBytes: 4096,
		MaxOutboxItems:     8,
		MaxOutboxBytes:     8192,
		MaxCandidates:      2,
		MaxRetired:         2,
	}
}

type headerThenForbiddenReader struct {
	header      []byte
	offset      int
	payloadRead bool
}

func (reader *headerThenForbiddenReader) Read(target []byte) (int, error) {
	if reader.offset < len(reader.header) {
		count := copy(target, reader.header[reader.offset:])
		reader.offset += count
		return count, nil
	}
	reader.payloadRead = true
	return 0, errors.New("payload read was forbidden")
}

var _ io.Reader = (*headerThenForbiddenReader)(nil)
