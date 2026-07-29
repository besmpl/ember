package preparedworker

import (
	"context"
	"io"
	"reflect"
	"sync"
	"testing"
)

func TestProcessServerOwnsHandshakeApplyAndGracefulClose(t *testing.T) {
	limits := processProtocolTestLimits()
	contract := identityFor("server-contract")
	checkpointSchema := identityFor("server-checkpoint")
	buildID := hashContent([]byte("server-build"))
	backend := &processServerBackend{}
	preparer := &processServerPreparer{backend: backend}
	parentReader, parentWriter, wait := startProcessServerForTest(t, processWorkerOptions{
		BuildID:          buildID,
		Contract:         contract,
		CheckpointSchema: checkpointSchema,
		Limits:           limits,
		Preparer:         preparer,
	})

	checkpoint := []byte("restore")
	hello := processHello{
		Artifact:         identityFor("server-artifact"),
		BuildID:          buildID,
		Contract:         contract,
		Stream:           identityFor("server-stream"),
		CheckpointSchema: checkpointSchema,
		Position:         Position{Sequence: 3, Revision: 4},
		CheckpointDigest: hashContent(checkpoint),
		Checkpoint:       checkpoint,
		Limits:           limits,
	}
	helloPayload, err := encodeProcessHello(hello)
	if err != nil {
		t.Fatal(err)
	}
	writeProcessFrameForTest(t, parentWriter, processFrame{Kind: processMessageHello, Payload: helloPayload})
	readyFrame := readProcessFrameForTest(t, parentReader, processReadyPayloadBytes)
	if readyFrame.Kind != processMessageReady || readyFrame.Correlation != 0 {
		t.Fatalf("READY frame = %#v", readyFrame)
	}
	ready, err := decodeProcessReady(readyFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	wantReady := processReady{
		Artifact: hello.Artifact, BuildID: buildID, Contract: contract,
		Stream: hello.Stream, CheckpointSchema: checkpointSchema,
		Position: hello.Position, CheckpointDigest: hello.CheckpointDigest, Limits: limits,
	}
	if ready != wantReady {
		t.Fatalf("READY = %#v, want %#v", ready, wantReady)
	}

	operation := encodedOperation{Sequence: 4, BaseRevision: 4, Request: []byte("request")}
	applyPayload, err := encodeProcessApply(operation, limits.MaxRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	writeProcessFrameForTest(t, parentWriter, processFrame{
		Kind: processMessageApply, Correlation: 19, Payload: applyPayload,
	})
	decisionFrame := readProcessFrameForTest(t, parentReader, processDecisionPayloadLimit(limits))
	if decisionFrame.Kind != processMessageDecision || decisionFrame.Correlation != 19 {
		t.Fatalf("DECISION frame = %#v", decisionFrame)
	}
	decision, err := decodeProcessDecision(decisionFrame.Payload, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decision, backend.decision) {
		t.Fatalf("decision = %#v, want %#v", decision, backend.decision)
	}
	if !reflect.DeepEqual(backend.operation, operation) {
		t.Fatalf("operation = %#v, want %#v", backend.operation, operation)
	}
	if preparer.spec.Artifact != hello.Artifact || preparer.spec.Contract != contract ||
		preparer.spec.Position != hello.Position || !reflect.DeepEqual(preparer.spec.Checkpoint, checkpoint) {
		t.Fatalf("prepare spec = %#v", preparer.spec)
	}

	writeProcessFrameForTest(t, parentWriter, processFrame{
		Kind: processMessageClose, Correlation: 20, Payload: encodeProcessClose(),
	})
	closed := readProcessFrameForTest(t, parentReader, 0)
	if closed.Kind != processMessageClosed || closed.Correlation != 20 || len(closed.Payload) != 0 {
		t.Fatalf("CLOSED frame = %#v", closed)
	}
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	if !backend.closed {
		t.Fatal("backend was not closed")
	}
}

func TestProcessServerRejectsMismatchedBuildBeforePreparing(t *testing.T) {
	limits := processProtocolTestLimits()
	preparer := &processServerPreparer{backend: &processServerBackend{}}
	parentReader, parentWriter, wait := startProcessServerForTest(t, processWorkerOptions{
		BuildID:          hashContent([]byte("expected-build")),
		Contract:         identityFor("contract"),
		CheckpointSchema: identityFor("checkpoint"),
		Limits:           limits,
		Preparer:         preparer,
	})
	checkpoint := []byte("restore")
	helloPayload, err := encodeProcessHello(processHello{
		Artifact:         identityFor("artifact"),
		BuildID:          hashContent([]byte("wrong-build")),
		Contract:         identityFor("contract"),
		Stream:           identityFor("stream"),
		CheckpointSchema: identityFor("checkpoint"),
		CheckpointDigest: hashContent(checkpoint),
		Checkpoint:       checkpoint,
		Limits:           limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeProcessFrameForTest(t, parentWriter, processFrame{Kind: processMessageHello, Payload: helloPayload})
	failureFrame := readProcessFrameForTest(t, parentReader, processMaxFailurePayloadBytes)
	if failureFrame.Kind != processMessageFailure {
		t.Fatalf("response kind = %d, want failure", failureFrame.Kind)
	}
	failure, err := decodeProcessFailure(failureFrame.Payload, processMaxFailureBytes)
	if err != nil {
		t.Fatal(err)
	}
	if failure.Kind != FailureIdentity {
		t.Fatalf("failure = %#v", failure)
	}
	if err := wait(); err == nil {
		t.Fatal("mismatched worker handshake returned success")
	}
	if preparer.called {
		t.Fatal("preparer ran before build identity was accepted")
	}
}

type processServerPreparer struct {
	backend generationBackend
	spec    backendSpec
	called  bool
}

func (preparer *processServerPreparer) Prepare(
	_ context.Context,
	spec backendSpec,
) (generationBackend, backendReady, error) {
	preparer.spec = spec
	preparer.called = true
	return preparer.backend, readyFor(spec), nil
}

type processServerBackend struct {
	mu        sync.Mutex
	operation encodedOperation
	decision  wireDecision
	closed    bool
}

func (backend *processServerBackend) Apply(
	_ context.Context,
	operation encodedOperation,
) (wireDecision, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.operation = operation
	backend.decision = wireDecision{
		Position: Position{Sequence: operation.Sequence, Revision: operation.BaseRevision + 1},
		Result:   []byte("result"), Checkpoint: []byte("checkpoint"), HasCheckpoint: true,
		Effects: [][]byte{[]byte("effect")}, Quiescent: true,
	}
	return backend.decision, nil
}

func (backend *processServerBackend) Close(context.Context) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.closed = true
	return nil
}

func startProcessServerForTest(
	t *testing.T,
	options processWorkerOptions,
) (io.Reader, io.Writer, func() error) {
	t.Helper()
	serverInput, parentWriter := io.Pipe()
	parentReader, serverOutput := io.Pipe()
	completed := make(chan error, 1)
	go func() {
		completed <- serveProcessWorker(context.Background(), serverInput, serverOutput, options, false)
		_ = serverInput.Close()
		_ = serverOutput.Close()
	}()
	wait := func() error { return <-completed }
	return parentReader, parentWriter, wait
}

func writeProcessFrameForTest(t *testing.T, writer io.Writer, frame processFrame) {
	t.Helper()
	if err := writeProcessFrame(writer, frame, processMaxFrameBytes); err != nil {
		t.Fatal(err)
	}
}

func readProcessFrameForTest(t *testing.T, reader io.Reader, limit int) processFrame {
	t.Helper()
	frame, err := decodeProcessFrame(reader, limit)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}
