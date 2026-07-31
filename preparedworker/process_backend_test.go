package preparedworker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

const processLauncherFinalFrameChild = "EMBER_PROCESS_LAUNCHER_FINAL_FRAME_CHILD"

func TestOSProcessLauncherRetainsFinalFrameAfterWait(t *testing.T) {
	if os.Getenv(processLauncherFinalFrameChild) == "1" {
		if err := writeProcessFrame(os.Stdout, processFrame{
			Kind: processMessageClosed, Correlation: 1,
		}, processMaxFrameBytes); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestOSProcessLauncherRetainsFinalFrameAfterWait$")
	command.Env = append(os.Environ(), processLauncherFinalFrameChild+"=1")
	child, err := launchWorkerCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.closeTransport)
	select {
	case err := <-child.wait:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper process did not exit")
	}
	frame, err := decodeProcessFrame(child.output, 0)
	if err != nil {
		t.Fatalf("decode final frame after process wait: %v", err)
	}
	if frame.Kind != processMessageClosed || frame.Correlation != 1 || len(frame.Payload) != 0 {
		t.Fatalf("final frame = %#v", frame)
	}
}

func TestProcessPreparerVerifiesHandshakeAndRunsOneExchangePerApply(t *testing.T) {
	limits := processProtocolTestLimits()
	contract := identityFor("process-backend-contract")
	checkpointSchema := identityFor("process-backend-checkpoint")
	artifact := processBackendTestArtifact(t, contract)
	workerBackend := &processServerBackend{}
	launcher := &processBackendTestLauncher{options: processWorkerOptions{
		BuildID: artifact.process.buildID, Contract: contract,
		CheckpointSchema: checkpointSchema, Limits: limits,
		Preparer: &processServerPreparer{backend: workerBackend},
	}}
	preparer := newProcessPreparer(launcher)
	checkpoint := []byte("restore")
	spec := backendSpec{
		Artifact: artifact.Identity(), Contract: contract, Stream: identityFor("stream"),
		CheckpointSchema: checkpointSchema, Position: Position{Sequence: 2, Revision: 3},
		CheckpointDigest: hashContent(checkpoint), Checkpoint: checkpoint, Limits: limits,
		artifact: artifact,
	}
	backend, ready, err := preparer.Prepare(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if ready != readyFor(spec) {
		t.Fatalf("READY = %#v, want %#v", ready, readyFor(spec))
	}
	operation := encodedOperation{Sequence: 3, BaseRevision: 3, Request: []byte("request")}
	decision, err := backend.Apply(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decision, workerBackend.decision) {
		t.Fatalf("decision = %#v, want %#v", decision, workerBackend.decision)
	}
	parentFrames := decodeProcessTranscript(t, launcher.parentTranscript())
	workerFrames := decodeProcessTranscript(t, launcher.workerTranscript())
	if len(parentFrames) != 2 || parentFrames[0].Kind != processMessageHello ||
		parentFrames[1].Kind != processMessageApply {
		t.Fatalf("parent frames = %#v, want exactly HELLO then APPLY", parentFrames)
	}
	if len(workerFrames) != 2 || workerFrames[0].Kind != processMessageReady ||
		workerFrames[1].Kind != processMessageDecision {
		t.Fatalf("worker frames = %#v, want exactly READY then DECISION", workerFrames)
	}
	if parentFrames[1].Correlation == 0 ||
		workerFrames[1].Correlation != parentFrames[1].Correlation {
		t.Fatalf(
			"ordinary exchange correlations = parent %d, worker %d",
			parentFrames[1].Correlation,
			workerFrames[1].Correlation,
		)
	}
	if launcher.launches != 1 {
		t.Fatalf("launches = %d, want 1", launcher.launches)
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
	if !workerBackend.closed {
		t.Fatal("worker backend remained open")
	}
}

func TestProcessPreparerRejectsChangedExecutableBeforeLaunch(t *testing.T) {
	limits := processProtocolTestLimits()
	contract := identityFor("changed-executable-contract")
	artifact := processBackendTestArtifact(t, contract)
	if err := os.WriteFile(artifact.process.executable, []byte("changed executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := &processBackendTestLauncher{}
	preparer := newProcessPreparer(launcher)
	checkpoint := []byte("restore")
	_, _, err := preparer.Prepare(context.Background(), backendSpec{
		Artifact: artifact.Identity(), Contract: contract, Stream: identityFor("stream"),
		CheckpointSchema: identityFor("checkpoint"), CheckpointDigest: hashContent(checkpoint),
		Checkpoint: checkpoint, Limits: limits, artifact: artifact,
	})
	if err == nil {
		t.Fatal("changed executable launched")
	}
	if launcher.launches != 0 {
		t.Fatalf("launches = %d, want 0", launcher.launches)
	}
}

func TestProcessBackendMapsWorkerFailureAndBecomesTerminal(t *testing.T) {
	limits := processProtocolTestLimits()
	contract := identityFor("process-failure-contract")
	checkpointSchema := identityFor("process-failure-checkpoint")
	artifact := processBackendTestArtifact(t, contract)
	workerBackend := &processBackendFailingBackend{err: errors.New("guest rejected request")}
	launcher := &processBackendTestLauncher{options: processWorkerOptions{
		BuildID: artifact.process.buildID, Contract: contract,
		CheckpointSchema: checkpointSchema, Limits: limits,
		Preparer: &processServerPreparer{backend: workerBackend},
	}}
	preparer := newProcessPreparer(launcher)
	checkpoint := []byte("restore")
	backend, _, err := preparer.Prepare(context.Background(), backendSpec{
		Artifact: artifact.Identity(), Contract: contract, Stream: identityFor("stream"),
		CheckpointSchema: checkpointSchema, CheckpointDigest: hashContent(checkpoint),
		Checkpoint: checkpoint, Limits: limits, artifact: artifact,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Apply(context.Background(), encodedOperation{
		Sequence: 1, Request: []byte("request"),
	})
	if !IsFailure(err, FailureGuest) {
		t.Fatalf("Apply error = %v, want guest failure", err)
	}
	_, err = backend.Apply(context.Background(), encodedOperation{
		Sequence: 1, Request: []byte("request"),
	})
	if !IsFailure(err, FailureLost) {
		t.Fatalf("second Apply error = %v, want terminal lost backend", err)
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProcessBackendCancellationAbortsReapsAndClosesIdempotently(t *testing.T) {
	limits := processProtocolTestLimits()
	contract := identityFor("process-cancellation-contract")
	checkpointSchema := identityFor("process-cancellation-checkpoint")
	artifact := processBackendTestArtifact(t, contract)
	workerBackend := &processBackendBlockingBackend{entered: make(chan struct{})}
	launcher := &processBackendTestLauncher{options: processWorkerOptions{
		BuildID: artifact.process.buildID, Contract: contract,
		CheckpointSchema: checkpointSchema, Limits: limits,
		Preparer: &processServerPreparer{backend: workerBackend},
	}}
	preparer := newProcessPreparer(launcher)
	checkpoint := []byte("restore")
	backend, _, err := preparer.Prepare(context.Background(), backendSpec{
		Artifact: artifact.Identity(), Contract: contract, Stream: identityFor("stream"),
		CheckpointSchema: checkpointSchema, CheckpointDigest: hashContent(checkpoint),
		Checkpoint: checkpoint, Limits: limits, artifact: artifact,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = backend.Apply(ctx, encodedOperation{Sequence: 1, Request: []byte("request")})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Apply error = %v, want deadline exceeded", err)
	}
	select {
	case <-workerBackend.entered:
	default:
		t.Fatal("worker backend was not entered before cancellation")
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	if err := backend.Close(closeCtx); err != nil {
		t.Fatalf("Close after cancellation: %v", err)
	}
	if err := backend.Close(closeCtx); err != nil {
		t.Fatalf("idempotent Close after cancellation: %v", err)
	}
	if !workerBackend.isClosed() {
		t.Fatal("canceled worker backend remained open")
	}
}

func TestProcessPreparerReturnsFailedHandshakeBackendForOwnedCleanup(t *testing.T) {
	limits := processProtocolTestLimits()
	contract := identityFor("process-handshake-cleanup-contract")
	checkpointSchema := identityFor("process-handshake-cleanup-checkpoint")
	artifact := processBackendTestArtifact(t, contract)
	launcher := &processBackendTestLauncher{options: processWorkerOptions{
		BuildID: hashContent([]byte("another-build")), Contract: contract,
		CheckpointSchema: checkpointSchema, Limits: limits,
		Preparer: &processServerPreparer{backend: &processServerBackend{}},
	}}
	preparer := newProcessPreparer(launcher)
	checkpoint := []byte("restore")
	backend, _, err := preparer.Prepare(context.Background(), backendSpec{
		Artifact: artifact.Identity(), Contract: contract, Stream: identityFor("stream"),
		CheckpointSchema: checkpointSchema, CheckpointDigest: hashContent(checkpoint),
		Checkpoint: checkpoint, Limits: limits, artifact: artifact,
	})
	if !IsFailure(err, FailureIdentity) {
		t.Fatalf("Prepare error = %v, want identity failure", err)
	}
	if backend == nil {
		t.Fatal("failed handshake returned no backend cleanup ownership")
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := backend.Close(closeCtx); err != nil {
		t.Fatalf("cleanup failed handshake: %v", err)
	}
}

type processBackendTestLauncher struct {
	mu               sync.Mutex
	options          processWorkerOptions
	launches         int
	parentFrames     bytes.Buffer
	workerFramesSeen bytes.Buffer
}

func (launcher *processBackendTestLauncher) Launch(_ *processArtifact) (*processChild, error) {
	launcher.mu.Lock()
	launcher.launches++
	launcher.mu.Unlock()
	serverInput, parentInput := io.Pipe()
	parentOutput, serverOutput := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	wait := make(chan error, 1)
	go func() {
		wait <- serveProcessWorker(ctx, serverInput, serverOutput, launcher.options, false)
		_ = serverInput.Close()
		_ = serverOutput.Close()
	}()
	var terminateOnce sync.Once
	terminate := func() error {
		terminateOnce.Do(func() {
			cancel()
			_ = parentInput.Close()
			_ = parentOutput.Close()
		})
		return nil
	}
	return &processChild{
		input: &recordingProcessWriteCloser{
			WriteCloser: parentInput,
			record:      launcher.recordParent,
		},
		output: &recordingProcessReadCloser{
			ReadCloser: parentOutput,
			record:     launcher.recordWorker,
		},
		wait:      wait,
		terminate: terminate, stderr: func() string { return "" }, cleanup: cancel,
	}, nil
}

func (launcher *processBackendTestLauncher) recordParent(data []byte) {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	_, _ = launcher.parentFrames.Write(data)
}

func (launcher *processBackendTestLauncher) recordWorker(data []byte) {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	_, _ = launcher.workerFramesSeen.Write(data)
}

func (launcher *processBackendTestLauncher) parentTranscript() []byte {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return append([]byte(nil), launcher.parentFrames.Bytes()...)
}

func (launcher *processBackendTestLauncher) workerTranscript() []byte {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return append([]byte(nil), launcher.workerFramesSeen.Bytes()...)
}

type recordingProcessWriteCloser struct {
	io.WriteCloser
	record func([]byte)
}

func (writer *recordingProcessWriteCloser) Write(data []byte) (int, error) {
	written, err := writer.WriteCloser.Write(data)
	if written != 0 {
		writer.record(data[:written])
	}
	return written, err
}

type recordingProcessReadCloser struct {
	io.ReadCloser
	record func([]byte)
}

func (reader *recordingProcessReadCloser) Read(data []byte) (int, error) {
	read, err := reader.ReadCloser.Read(data)
	if read != 0 {
		reader.record(data[:read])
	}
	return read, err
}

func decodeProcessTranscript(t *testing.T, data []byte) []processFrame {
	t.Helper()
	reader := bytes.NewReader(data)
	frames := make([]processFrame, 0, 4)
	for reader.Len() != 0 {
		frame, err := decodeProcessFrame(reader, processMaxFrameBytes)
		if err != nil {
			t.Fatalf("decode process transcript: %v", err)
		}
		frames = append(frames, frame)
	}
	return frames
}

type processBackendFailingBackend struct{ err error }

func (backend *processBackendFailingBackend) Apply(
	context.Context,
	encodedOperation,
) (wireDecision, error) {
	return wireDecision{}, backend.err
}

func (*processBackendFailingBackend) Close(context.Context) error { return nil }

type processBackendBlockingBackend struct {
	mu        sync.Mutex
	entered   chan struct{}
	enterOnce sync.Once
	closed    bool
}

func (backend *processBackendBlockingBackend) Apply(
	ctx context.Context,
	_ encodedOperation,
) (wireDecision, error) {
	backend.enterOnce.Do(func() { close(backend.entered) })
	<-ctx.Done()
	return wireDecision{}, ctx.Err()
}

func (backend *processBackendBlockingBackend) Close(context.Context) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.closed = true
	return nil
}

func (backend *processBackendBlockingBackend) isClosed() bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.closed
}

func processBackendTestArtifact(t *testing.T, contract contentIdentity) preparedArtifact {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "worker")
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := artifactTestDescriptor()
	descriptor.Contract = contract.Digest()
	artifact, err := openProcessArtifact(path, descriptor, int64(len(data)+1))
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}
