package preparedworker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

// defaultProcessPreparer returns the production process adapter. Preparation is
// lazy: it verifies and launches only the opaque Artifact supplied to Prepare.
func defaultProcessPreparer() backendPreparer {
	return newProcessPreparer(osProcessLauncher{})
}

type processLauncher interface {
	Launch(*processArtifact) (*processChild, error)
}

type processPreparer struct {
	launcher processLauncher
}

func newProcessPreparer(launcher processLauncher) backendPreparer {
	return &processPreparer{launcher: launcher}
}

func (preparer *processPreparer) Prepare(
	ctx context.Context,
	spec backendSpec,
) (generationBackend, backendReady, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, backendReady{}, err
	}
	if preparer == nil || preparer.launcher == nil {
		return nil, backendReady{}, fmt.Errorf("prepared worker process backend: incomplete preparer")
	}
	artifact := spec.artifact.process
	if artifact == nil || spec.Artifact != spec.artifact.identity {
		return nil, backendReady{}, fmt.Errorf("prepared worker process backend: artifact has no launch authority")
	}
	if artifact.descriptor.Contract != spec.Contract.digest {
		return nil, backendReady{}, fmt.Errorf("prepared worker process backend: artifact contract differs")
	}
	if err := artifact.verifyExecutable(); err != nil {
		return nil, backendReady{}, err
	}
	helloLimit, requestLimit, responseLimit, err := processPayloadLimits(spec.Limits)
	if err != nil {
		return nil, backendReady{}, err
	}
	child, err := preparer.launcher.Launch(artifact)
	if err != nil {
		return nil, backendReady{}, fmt.Errorf("prepared worker process backend: launch: %w", err)
	}
	if child == nil || child.input == nil || child.output == nil || child.wait == nil ||
		child.terminate == nil || child.stderr == nil {
		if child != nil {
			_ = child.abort()
		}
		return nil, backendReady{}, fmt.Errorf("prepared worker process backend: launcher returned incomplete child")
	}
	backend := &processBackend{
		child: child, limits: spec.Limits,
		requestLimit: max(helloLimit, requestLimit), responseLimit: responseLimit,
		waitDone: make(chan struct{}),
	}
	backend.startWaitOwner()
	helloPayload, err := encodeProcessHello(processHello{
		Artifact: spec.Artifact, BuildID: artifact.buildID, Contract: spec.Contract,
		Stream: spec.Stream, CheckpointSchema: spec.CheckpointSchema,
		Position: spec.Position, CheckpointDigest: spec.CheckpointDigest,
		Checkpoint: append([]byte(nil), spec.Checkpoint...), Limits: spec.Limits,
	})
	if err != nil {
		_ = backend.abort()
		return backend, backendReady{}, err
	}
	backend.mu.Lock()
	frame, exchangeErr := backend.exchangeLocked(ctx, processFrame{
		Kind: processMessageHello, Payload: helloPayload,
	})
	if exchangeErr != nil {
		backend.lost = true
	}
	backend.mu.Unlock()
	if exchangeErr != nil {
		return backend, backendReady{}, backend.withStderr("handshake", exchangeErr)
	}
	if frame.Correlation != 0 {
		_ = backend.failTerminal()
		return backend, backendReady{}, fail(FailureProtocol, "prepare", fmt.Errorf("READY correlation differs"))
	}
	if frame.Kind == processMessageFailure {
		failureErr := decodeProcessFailureError(frame.Payload)
		_ = backend.failTerminal()
		return backend, backendReady{}, backend.withStderr("prepare", failureErr)
	}
	if frame.Kind != processMessageReady {
		_ = backend.failTerminal()
		return backend, backendReady{}, fail(FailureProtocol, "prepare", fmt.Errorf("response kind %d is not READY", frame.Kind))
	}
	processReady, err := decodeProcessReady(frame.Payload)
	if err != nil {
		_ = backend.failTerminal()
		return backend, backendReady{}, fail(FailureProtocol, "prepare", err)
	}
	wantProcessReady := processReadyFor(spec, artifact.buildID)
	if processReady != wantProcessReady {
		_ = backend.failTerminal()
		return backend, backendReady{}, fail(FailureIdentity, "prepare", fmt.Errorf("worker READY identity differs"))
	}
	return backend, readyFor(spec), nil
}

func processReadyFor(spec backendSpec, buildID contentDigest) processReady {
	return processReady{
		Artifact: spec.Artifact, BuildID: buildID, Contract: spec.Contract,
		Stream: spec.Stream, CheckpointSchema: spec.CheckpointSchema,
		Position: spec.Position, CheckpointDigest: spec.CheckpointDigest, Limits: spec.Limits,
	}
}

type processChild struct {
	input     io.WriteCloser
	output    io.ReadCloser
	wait      <-chan error
	terminate func() error
	stderr    func() string
	cleanup   func()

	abortOnce sync.Once
	abortErr  error
	closeOnce sync.Once
}

func (child *processChild) abort() error {
	if child == nil {
		return nil
	}
	child.abortOnce.Do(func() {
		child.abortErr = child.terminate()
		child.closeTransport()
	})
	return child.abortErr
}

func (child *processChild) closeTransport() {
	if child == nil {
		return
	}
	child.closeOnce.Do(func() {
		_ = child.input.Close()
		_ = child.output.Close()
	})
}

type processBackend struct {
	mu            sync.Mutex
	child         *processChild
	limits        transactionLimits
	requestLimit  int
	responseLimit int
	correlation   uint64
	lost          bool
	closing       bool
	closed        bool
	aborted       bool

	waitMu   sync.Mutex
	waitDone chan struct{}
	waitErr  error
}

func (backend *processBackend) startWaitOwner() {
	go func() {
		err := <-backend.child.wait
		if backend.child.cleanup != nil {
			backend.child.cleanup()
		}
		backend.waitMu.Lock()
		backend.waitErr = err
		close(backend.waitDone)
		backend.waitMu.Unlock()
	}()
}

func (backend *processBackend) Apply(
	ctx context.Context,
	operation encodedOperation,
) (wireDecision, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return wireDecision{}, err
	}
	if backend == nil {
		return wireDecision{}, fail(FailureClosed, "apply", fmt.Errorf("nil process backend"))
	}
	payload, err := encodeProcessApply(operation, backend.limits.MaxRequestBytes)
	if err != nil {
		return wireDecision{}, fail(FailureProtocol, "apply", err)
	}
	return backend.requestDecision(ctx, processMessageApply, payload, "apply")
}

func (backend *processBackend) Replay(
	ctx context.Context,
	operation encodedOperation,
	expectedRecord contentDigest,
) (wireDecision, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return wireDecision{}, err
	}
	if backend == nil {
		return wireDecision{}, fail(FailureClosed, "replay", fmt.Errorf("nil process backend"))
	}
	payload, err := encodeProcessReplay(processReplay{
		Operation: operation, ExpectedRecord: expectedRecord,
	}, backend.limits.MaxRequestBytes)
	if err != nil {
		return wireDecision{}, fail(FailureProtocol, "replay", err)
	}
	return backend.requestDecision(ctx, processMessageReplay, payload, "replay")
}

func (backend *processBackend) requestDecision(
	ctx context.Context,
	kind processMessageKind,
	payload []byte,
	operation string,
) (wireDecision, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.closed || backend.closing {
		return wireDecision{}, fail(FailureClosed, operation, fmt.Errorf("process backend is closing"))
	}
	if backend.lost {
		return wireDecision{}, fail(FailureLost, operation, fmt.Errorf("process backend is terminal"))
	}
	correlation, err := backend.nextCorrelationLocked()
	if err != nil {
		backend.lost = true
		return wireDecision{}, err
	}
	frame, err := backend.exchangeLocked(ctx, processFrame{
		Kind: kind, Correlation: correlation, Payload: payload,
	})
	if err != nil {
		backend.lost = true
		return wireDecision{}, backend.withStderr(operation, err)
	}
	if frame.Correlation != correlation {
		backend.lost = true
		_ = backend.abortLocked()
		return wireDecision{}, fail(FailureProtocol, operation, fmt.Errorf("response correlation differs"))
	}
	if frame.Kind == processMessageFailure {
		backend.lost = true
		_ = backend.abortLocked()
		return wireDecision{}, backend.withStderr(operation, decodeProcessFailureError(frame.Payload))
	}
	if frame.Kind != processMessageDecision {
		backend.lost = true
		_ = backend.abortLocked()
		return wireDecision{}, fail(FailureProtocol, operation, fmt.Errorf("response kind %d is not DECISION", frame.Kind))
	}
	decision, err := decodeProcessDecision(frame.Payload, backend.limits)
	if err != nil {
		backend.lost = true
		_ = backend.abortLocked()
		return wireDecision{}, fail(FailureProtocol, operation, err)
	}
	return decision, nil
}

func (backend *processBackend) Close(ctx context.Context) error {
	if backend == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	backend.mu.Lock()
	if backend.closed {
		backend.mu.Unlock()
		return nil
	}
	if !backend.closing {
		backend.closing = true
		if backend.lost {
			_ = backend.abortLocked()
		} else {
			correlation, err := backend.nextCorrelationLocked()
			if err == nil {
				var frame processFrame
				frame, err = backend.exchangeLocked(ctx, processFrame{
					Kind: processMessageClose, Correlation: correlation, Payload: encodeProcessClose(),
				})
				if err == nil && (frame.Kind != processMessageClosed || frame.Correlation != correlation || len(frame.Payload) != 0) {
					err = fail(FailureProtocol, "close", fmt.Errorf("worker did not acknowledge CLOSED"))
				}
			}
			if err != nil {
				backend.lost = true
				_ = backend.abortLocked()
				backend.mu.Unlock()
				if contextErr := ctx.Err(); contextErr != nil {
					return contextErr
				}
				return backend.withStderr("close", err)
			}
			backend.child.closeTransport()
		}
	}
	backend.mu.Unlock()

	if err := backend.await(ctx); err != nil {
		return err
	}
	backend.mu.Lock()
	backend.closed = true
	backend.mu.Unlock()
	return nil
}

func (backend *processBackend) exchangeLocked(
	ctx context.Context,
	request processFrame,
) (processFrame, error) {
	type exchangeResult struct {
		frame processFrame
		err   error
	}
	completed := make(chan exchangeResult, 1)
	go func() {
		if err := writeProcessFrame(backend.child.input, request, backend.requestLimit); err != nil {
			completed <- exchangeResult{err: err}
			return
		}
		frame, err := decodeProcessFrame(backend.child.output, backend.responseLimit)
		completed <- exchangeResult{frame: frame, err: err}
	}()
	select {
	case result := <-completed:
		return result.frame, result.err
	case <-ctx.Done():
		backend.lost = true
		abortErr := backend.abortLocked()
		return processFrame{}, errors.Join(ctx.Err(), abortErr)
	}
}

func (backend *processBackend) nextCorrelationLocked() (uint64, error) {
	if backend.correlation == math.MaxUint64 {
		return 0, fail(FailureLimit, "protocol", fmt.Errorf("correlation space exhausted"))
	}
	backend.correlation++
	return backend.correlation, nil
}

func (backend *processBackend) abort() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.abortLocked()
}

func (backend *processBackend) abortLocked() error {
	backend.aborted = true
	return backend.child.abort()
}

func (backend *processBackend) failTerminal() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.lost = true
	return backend.abortLocked()
}

func (backend *processBackend) await(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-backend.waitDone:
	}
	backend.waitMu.Lock()
	waitErr := backend.waitErr
	backend.waitMu.Unlock()
	backend.mu.Lock()
	aborted := backend.aborted
	backend.mu.Unlock()
	if waitErr != nil && !aborted {
		return backend.withStderr("wait", waitErr)
	}
	return nil
}

func (backend *processBackend) withStderr(operation string, err error) error {
	if err == nil || backend == nil || backend.child == nil {
		return err
	}
	stderr := backend.child.stderr()
	if stderr == "" {
		return err
	}
	return fmt.Errorf("prepared worker process %s: %w (stderr: %s)", operation, err, stderr)
}

func decodeProcessFailureError(payload []byte) error {
	failure, err := decodeProcessFailure(payload, processMaxFailureBytes)
	if err != nil {
		return fail(FailureProtocol, "worker", err)
	}
	if !validFailureKind(failure.Kind) {
		return fail(FailureProtocol, "worker", fmt.Errorf("unknown failure kind %q", failure.Kind))
	}
	return fail(failure.Kind, failure.Operation, errors.New(failure.Message))
}
