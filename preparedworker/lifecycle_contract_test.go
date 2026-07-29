package preparedworker

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestCandidateActivateIsOneShot(t *testing.T) {
	module, _ := newLifecycleModule(t, func() *lifecycleBackend {
		return &lifecycleBackend{}
	})
	candidate := prepareLifecycleCandidate(t, module, "one-shot")

	if err := candidate.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := candidate.Activate(); !IsFailure(err, FailureStale) {
		t.Fatalf("second activation error = %v, want stale", err)
	}
}

func TestCandidateCloseCanBeRetried(t *testing.T) {
	backendFailure := errors.New("candidate backend close failed")
	tests := []struct {
		name           string
		firstClose     func(*Candidate, *lifecycleBackend) error
		wantCloseCalls int
	}{
		{
			name:           "canceled context",
			wantCloseCalls: 1,
			firstClose: func(candidate *Candidate, _ *lifecycleBackend) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return candidate.Close(ctx)
			},
		},
		{
			name:           "backend failure",
			wantCloseCalls: 2,
			firstClose: func(candidate *Candidate, backend *lifecycleBackend) error {
				backend.closeErrors = []error{backendFailure}
				return candidate.Close(context.Background())
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module, backends := newLifecycleModule(t, func() *lifecycleBackend {
				return &lifecycleBackend{}
			})
			candidate := prepareLifecycleCandidate(t, module, test.name)
			backend := backends()[0]

			if err := test.firstClose(candidate, backend); err == nil {
				t.Fatal("first candidate close succeeded, want retryable failure")
			}
			if err := candidate.Close(context.Background()); err != nil {
				t.Fatalf("retry candidate close: %v", err)
			}
			if calls := backend.closeCount(); calls != test.wantCloseCalls {
				t.Fatalf("backend close calls = %d, want %d", calls, test.wantCloseCalls)
			}
			if err := candidate.Activate(); !IsFailure(err, FailureStale) {
				t.Fatalf("closed candidate activation error = %v, want stale", err)
			}
		})
	}
}

func TestCandidateActivateReturnsBusyWhileApplyIsAdmitted(t *testing.T) {
	active := &lifecycleBackend{
		applyStarted: make(chan struct{}),
		releaseApply: make(chan struct{}),
	}
	module, backends := newLifecycleModule(t, func() *lifecycleBackend {
		if active != nil {
			backend := active
			active = nil
			return backend
		}
		return &lifecycleBackend{}
	})
	first := prepareLifecycleCandidate(t, module, "active")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	next := prepareLifecycleCandidate(t, module, "next")

	applyDone := make(chan error, 1)
	go func() {
		_, err := module.Apply(context.Background(), Operation[lifecycleRequest]{
			Sequence: 1,
			Request:  lifecycleRequest{Value: 7},
		})
		applyDone <- err
	}()
	select {
	case <-backends()[0].applyStarted:
	case <-time.After(time.Second):
		t.Fatal("apply did not enter backend")
	}

	if err := next.Activate(); !IsFailure(err, FailureBusy) {
		t.Fatalf("activation during apply error = %v, want busy", err)
	}
	close(backends()[0].releaseApply)
	if err := <-applyDone; err != nil {
		t.Fatalf("apply: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		err := next.Activate()
		if err == nil {
			break
		}
		if !IsFailure(err, FailureBehind) {
			t.Fatalf("activation after head advanced error = %v, want behind or success", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("candidate did not catch up before activation deadline")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCandidateFollowsCommittedWorkBeforeNoIOActivation(t *testing.T) {
	followerStarted := make(chan struct{})
	releaseFollower := make(chan struct{})
	defer func() {
		select {
		case <-releaseFollower:
		default:
			close(releaseFollower)
		}
	}()
	backends := []*lifecycleBackend{
		{},
		{applyStarted: followerStarted, releaseApply: releaseFollower},
	}
	module, _ := newLifecycleModule(t, func() *lifecycleBackend {
		backend := backends[0]
		backends = backends[1:]
		return backend
	})
	active := prepareLifecycleCandidate(t, module, "follow-active")
	if err := active.Activate(); err != nil {
		t.Fatal(err)
	}
	candidate := prepareLifecycleCandidate(t, module, "follow-candidate")
	if _, err := module.Apply(context.Background(), Operation[lifecycleRequest]{
		Sequence: 1,
		Request:  lifecycleRequest{Value: 9},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-followerStarted:
	case <-time.After(time.Second):
		t.Fatal("candidate follower did not enter committed operation")
	}
	if err := candidate.Activate(); !IsFailure(err, FailureBehind) {
		t.Fatalf("activation during catch-up error = %v, want behind", err)
	}
	close(releaseFollower)
	deadline := time.Now().Add(time.Second)
	for {
		err := candidate.Activate()
		if err == nil {
			break
		}
		if !IsFailure(err, FailureBehind) {
			t.Fatalf("activation after catch-up error = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("caught-up candidate did not activate")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCandidateRestoresLatestQuiescentCheckpointAndFollowsNonQuiescentHead(t *testing.T) {
	module, _ := newLifecycleModule(t, func() *lifecycleBackend {
		return &lifecycleBackend{quiescent: func(sequence uint64) bool { return sequence%2 == 0 }}
	})
	first := prepareLifecycleCandidate(t, module, "latest-checkpoint-a")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Apply(context.Background(), Operation[lifecycleRequest]{
		Sequence: 1, Request: lifecycleRequest{Value: 10},
	}); err != nil {
		t.Fatal(err)
	}

	replacement, err := module.Prepare(
		context.Background(),
		artifactFromIdentity(identityFor("lifecycle-latest-checkpoint-b")),
	)
	if err != nil {
		t.Fatalf("Prepare from latest quiescent checkpoint: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		err := replacement.Activate()
		if IsFailure(err, FailureNotQuiescent) {
			break
		}
		if !IsFailure(err, FailureBehind) || time.Now().After(deadline) {
			t.Fatalf("Activate at non-quiescent head = %v, want not-quiescent", err)
		}
		runtime.Gosched()
	}

	if _, err := module.Apply(context.Background(), Operation[lifecycleRequest]{
		Sequence: 2, BaseRevision: 1, Request: lifecycleRequest{Value: 20},
	}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for {
		err := replacement.Activate()
		if err == nil {
			break
		}
		if !IsFailure(err, FailureBehind) || time.Now().After(deadline) {
			t.Fatalf("Activate after quiescent catch-up = %v", err)
		}
		runtime.Gosched()
	}
}

func TestModuleCloseCanBeRetried(t *testing.T) {
	backendFailure := errors.New("module backend close failed")
	t.Run("after backend failure", func(t *testing.T) {
		module, backends := newLifecycleModule(t, func() *lifecycleBackend {
			return &lifecycleBackend{closeErrors: []error{backendFailure}}
		})
		candidate := prepareLifecycleCandidate(t, module, "backend-failure")
		if err := candidate.Activate(); err != nil {
			t.Fatal(err)
		}

		if err := module.Close(context.Background()); !errors.Is(err, backendFailure) {
			t.Fatalf("first module close error = %v, want backend failure", err)
		}
		if err := module.Close(context.Background()); err != nil {
			t.Fatalf("retry module close: %v", err)
		}
		if err := module.Close(context.Background()); err != nil {
			t.Fatalf("idempotent module close: %v", err)
		}
		if calls := backends()[0].closeCount(); calls != 2 {
			t.Fatalf("backend close calls = %d, want 2", calls)
		}
	})

	t.Run("after deadline", func(t *testing.T) {
		module, backends := newLifecycleModule(t, func() *lifecycleBackend {
			return &lifecycleBackend{waitForFirstCloseContext: true}
		})
		candidate := prepareLifecycleCandidate(t, module, "deadline")
		if err := candidate.Activate(); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := module.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("first module close error = %v, want deadline exceeded", err)
		}
		if err := module.Close(context.Background()); err != nil {
			t.Fatalf("retry module close: %v", err)
		}
		if calls := backends()[0].closeCount(); calls != 2 {
			t.Fatalf("backend close calls = %d, want 2", calls)
		}
	})
}

func TestResolveSerializesWithApplyAndRejectsAfterClose(t *testing.T) {
	module, backends := newLifecycleModule(t, func() *lifecycleBackend {
		return &lifecycleBackend{applyStarted: make(chan struct{}), releaseApply: make(chan struct{})}
	})
	candidate := prepareLifecycleCandidate(t, module, "resolve-lifecycle")
	if err := candidate.Activate(); err != nil {
		t.Fatal(err)
	}
	operation := Operation[lifecycleRequest]{Sequence: 1, Request: lifecycleRequest{Value: 7}}
	applyDone := make(chan error, 1)
	go func() {
		_, err := module.Apply(context.Background(), operation)
		applyDone <- err
	}()
	<-backends()[0].applyStarted
	if _, err := module.Resolve(context.Background(), operation); !IsFailure(err, FailureBusy) {
		t.Fatalf("Resolve during Apply = %v, want busy", err)
	}
	close(backends()[0].releaseApply)
	if err := <-applyDone; err != nil {
		t.Fatal(err)
	}
	if err := module.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Resolve(context.Background(), operation); !IsFailure(err, FailureClosed) {
		t.Fatalf("Resolve after Close = %v, want closed", err)
	}
}

type lifecycleRequest struct {
	Value int `json:"value"`
}

type lifecycleResult struct {
	Value int `json:"value"`
}

type lifecycleCheckpoint struct {
	Position Position `json:"position"`
}

type lifecycleEffect struct{}

type lifecycleBackend struct {
	mu                       sync.Mutex
	applyOnce                sync.Once
	applyStarted             chan struct{}
	releaseApply             chan struct{}
	closeErrors              []error
	closeCalls               int
	waitForFirstCloseContext bool
	quiescent                func(uint64) bool
}

func (backend *lifecycleBackend) Apply(
	ctx context.Context,
	operation encodedOperation,
) (wireDecision, error) {
	if backend.applyStarted != nil {
		backend.applyOnce.Do(func() { close(backend.applyStarted) })
	}
	if backend.releaseApply != nil {
		select {
		case <-backend.releaseApply:
		case <-ctx.Done():
			return wireDecision{}, ctx.Err()
		}
	}
	var request lifecycleRequest
	if err := json.Unmarshal(operation.Request, &request); err != nil {
		return wireDecision{}, err
	}
	position := Position{
		Sequence: operation.Sequence,
		Revision: operation.BaseRevision + 1,
	}
	result, err := json.Marshal(lifecycleResult{Value: request.Value})
	if err != nil {
		return wireDecision{}, err
	}
	decision := wireDecision{Position: position, Result: result}
	if backend.quiescent != nil && !backend.quiescent(operation.Sequence) {
		return decision, nil
	}
	checkpoint, err := json.Marshal(lifecycleCheckpoint{Position: position})
	if err != nil {
		return wireDecision{}, err
	}
	decision.Checkpoint = checkpoint
	decision.HasCheckpoint = true
	decision.Quiescent = true
	return decision, nil
}

func (backend *lifecycleBackend) Close(ctx context.Context) error {
	backend.mu.Lock()
	backend.closeCalls++
	call := backend.closeCalls
	wait := backend.waitForFirstCloseContext && call == 1
	var err error
	if len(backend.closeErrors) != 0 {
		err = backend.closeErrors[0]
		backend.closeErrors = backend.closeErrors[1:]
	}
	backend.mu.Unlock()
	if wait {
		<-ctx.Done()
		return ctx.Err()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (backend *lifecycleBackend) closeCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.closeCalls
}

type lifecyclePreparer struct {
	mu       sync.Mutex
	factory  func() *lifecycleBackend
	backends []*lifecycleBackend
}

func (preparer *lifecyclePreparer) Prepare(
	_ context.Context,
	spec backendSpec,
) (generationBackend, backendReady, error) {
	backend := preparer.factory()
	preparer.mu.Lock()
	preparer.backends = append(preparer.backends, backend)
	preparer.mu.Unlock()
	return backend, backendReady{
		Artifact:         spec.Artifact,
		Contract:         spec.Contract,
		Stream:           spec.Stream,
		CheckpointSchema: spec.CheckpointSchema,
		Position:         spec.Position,
		CheckpointDigest: spec.CheckpointDigest,
		Limits:           spec.Limits,
	}, nil
}

func newLifecycleModule(
	t *testing.T,
	factory func() *lifecycleBackend,
) (*transactionModule[lifecycleRequest, lifecycleResult, lifecycleCheckpoint, lifecycleEffect], func() []*lifecycleBackend) {
	t.Helper()
	preparer := &lifecyclePreparer{factory: factory}
	codecRequest := lifecycleJSONCodec[lifecycleRequest]{}
	codecResult := lifecycleJSONCodec[lifecycleResult]{}
	codecCheckpoint := lifecycleJSONCodec[lifecycleCheckpoint]{}
	codecEffect := lifecycleJSONCodec[lifecycleEffect]{}
	module, err := newTransactionModule(
		moduleConfig[lifecycleRequest, lifecycleResult, lifecycleCheckpoint, lifecycleEffect]{
			Stream:   identityFor("lifecycle-stream"),
			Contract: testContract("lifecycle"),
			Limits: transactionLimits{
				MaxRequestBytes:    1024,
				MaxResultBytes:     1024,
				MaxCheckpointBytes: 1024,
				MaxOutboxItems:     4,
				MaxOutboxBytes:     1024,
				MaxCandidates:      4,
				MaxRetired:         4,
			},
			RequestCodec:    codecRequest,
			ResultCodec:     codecResult,
			CheckpointCodec: codecCheckpoint,
			OutboxCodec:     codecEffect,
			Preparer:        preparer,
			Journal:         newMemoryJournal(),
			Publisher:       newMemoryPublisher(),
		},
		initialState[lifecycleCheckpoint]{Checkpoint: lifecycleCheckpoint{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = module.Close(context.Background()) })
	return module, func() []*lifecycleBackend {
		preparer.mu.Lock()
		defer preparer.mu.Unlock()
		return append([]*lifecycleBackend(nil), preparer.backends...)
	}
}

func prepareLifecycleCandidate(
	t *testing.T,
	module *transactionModule[lifecycleRequest, lifecycleResult, lifecycleCheckpoint, lifecycleEffect],
	artifact string,
) *Candidate {
	t.Helper()
	candidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("lifecycle-"+artifact),
	))
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

type lifecycleJSONCodec[T any] struct{}

func (lifecycleJSONCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }

func (lifecycleJSONCodec[T]) Decode(data []byte) (T, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}
