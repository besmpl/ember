package preparedworker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestActivateSchedulesRetiredBackendWithoutWaitingForClose(t *testing.T) {
	retired := newBlockingRetirementBackend()
	defer retired.release()
	preparer := &sequenceLifecyclePreparer{backends: []generationBackend{
		retired,
		&lifecycleBackend{},
	}}
	module := newLifecycleModuleFromPreparer(t, preparer)

	first := prepareLifecycleCandidate(t, module, "immediate-retirement-first")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	second := prepareLifecycleCandidate(t, module, "immediate-retirement-second")
	activated := make(chan error, 1)
	go func() { activated <- second.Activate() }()

	select {
	case err := <-activated:
		if err != nil {
			t.Fatalf("Activate: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Activate waited for retired backend Close")
	}
	select {
	case <-retired.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("retired backend Close was not scheduled after activation")
	}
	select {
	case <-retired.closeReturned:
		t.Fatal("retired backend Close returned before its blocker was released")
	default:
	}

	retired.release()
	select {
	case <-retired.closeReturned:
	case <-time.After(time.Second):
		t.Fatal("retired backend Close did not finish after release")
	}
}

func TestModuleCloseReportsAndRetriesTimedOutRetirement(t *testing.T) {
	retired := newTimedOutRetirementBackend()
	preparer := &sequenceLifecyclePreparer{backends: []generationBackend{
		retired,
		&lifecycleBackend{},
	}}
	module := newLifecycleModuleFromPreparerWithPolicy(t, preparer, 1, 1, 20*time.Millisecond)

	first := prepareLifecycleCandidate(t, module, "timed-retirement-first")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	second := prepareLifecycleCandidate(t, module, "timed-retirement-second")
	if err := second.Activate(); err != nil {
		t.Fatalf("Activate must not report asynchronous retirement failure: %v", err)
	}
	select {
	case <-retired.firstReturned:
	case <-time.After(time.Second):
		t.Fatal("retired backend Close did not observe the configured timeout")
	}

	if err := module.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Module.Close error = %v, want retained retirement deadline", err)
	}
	if calls := retired.closeCount(); calls != 1 {
		t.Fatalf("retired backend close calls after reported failure = %d, want 1", calls)
	}
	if err := module.Close(context.Background()); err != nil {
		t.Fatalf("retry Module.Close: %v", err)
	}
	if err := module.Close(context.Background()); err != nil {
		t.Fatalf("idempotent Module.Close: %v", err)
	}
	if calls := retired.closeCount(); calls != 2 {
		t.Fatalf("retired backend close calls after retry = %d, want 2", calls)
	}
}

func TestPrepareReportsAsynchronousRetirementFailureBeforeRetrying(t *testing.T) {
	retirementFailure := errors.New("injected retirement failure")
	retired := newFailingRetirementBackend(retirementFailure)
	preparer := &sequenceLifecyclePreparer{backends: []generationBackend{
		retired,
		&lifecycleBackend{},
		&lifecycleBackend{},
	}}
	module := newLifecycleModuleFromPreparer(t, preparer)

	first := prepareLifecycleCandidate(t, module, "failed-retirement-first")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	second := prepareLifecycleCandidate(t, module, "failed-retirement-second")
	if err := second.Activate(); err != nil {
		t.Fatalf("Activate must not report asynchronous retirement failure: %v", err)
	}
	select {
	case <-retired.firstReturned:
	case <-time.After(time.Second):
		t.Fatal("retired backend Close did not return its injected failure")
	}

	candidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("failed-retirement-third"),
	))
	if candidate != nil || !IsFailure(err, FailureLost) ||
		!errors.Is(err, retirementFailure) {
		t.Fatalf("first Prepare after retirement failure = %#v, %v; want nil/lost injected failure", candidate, err)
	}
	if calls := retired.closeCount(); calls != 1 {
		t.Fatalf("retired backend close calls before explicit retry = %d, want 1", calls)
	}

	candidate, err = module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("failed-retirement-third"),
	))
	if err != nil {
		t.Fatalf("retry Prepare: %v", err)
	}
	if calls := retired.closeCount(); calls != 2 {
		t.Fatalf("retired backend close calls after explicit retry = %d, want 2", calls)
	}
	if err := candidate.Close(context.Background()); err != nil {
		t.Fatalf("close retry candidate: %v", err)
	}
}

func TestModuleCloseJoinsInFlightRetirementWithoutDoubleClose(t *testing.T) {
	retired := newBlockingRetirementBackend()
	defer retired.release()
	preparer := &sequenceLifecyclePreparer{backends: []generationBackend{
		retired,
		&lifecycleBackend{},
	}}
	module := newLifecycleModuleFromPreparer(t, preparer)

	first := prepareLifecycleCandidate(t, module, "joined-retirement-first")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	second := prepareLifecycleCandidate(t, module, "joined-retirement-second")
	if err := second.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-retired.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("retired backend Close did not start")
	}

	closed := make(chan error, 1)
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		closed <- module.Close(context.Background())
	}()
	<-closeStarted
	select {
	case err := <-closed:
		t.Fatalf("Module.Close returned before in-flight retirement completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if calls, concurrent := retired.closeStats(); calls != 1 || concurrent != 1 {
		t.Fatalf("in-flight retired close calls/concurrency = %d/%d, want 1/1", calls, concurrent)
	}

	retired.release()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Module.Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Module.Close did not join completed retirement")
	}
	if calls, concurrent := retired.closeStats(); calls != 1 || concurrent != 0 {
		t.Fatalf("completed retired close calls/concurrency = %d/%d, want 1/0", calls, concurrent)
	}
}

func TestRetiringGenerationCountsTowardLimitUntilReaped(t *testing.T) {
	retired := newBlockingRetirementBackend()
	defer retired.release()
	preparer := &sequenceLifecyclePreparer{backends: []generationBackend{
		retired,
		&lifecycleBackend{},
		&lifecycleBackend{},
	}}
	module := newLifecycleModuleFromPreparerWithPolicy(t, preparer, 2, 1, 0)

	first := prepareLifecycleCandidate(t, module, "limited-retirement-first")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	second := prepareLifecycleCandidate(t, module, "limited-retirement-second")
	third := prepareLifecycleCandidate(t, module, "limited-retirement-third")
	if err := second.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-retired.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("retired backend Close did not start")
	}
	if err := activateAfterFollowing(third); !IsFailure(err, FailureLimit) {
		t.Fatalf("Activate while retirement is in flight = %v, want limit", err)
	}

	retired.release()
	select {
	case <-retired.closeReturned:
	case <-time.After(time.Second):
		t.Fatal("retired backend Close did not finish")
	}
	if err := activateAfterRetirement(third); err != nil {
		t.Fatalf("Activate after retirement completed: %v", err)
	}
}

func TestPrepareClosesEveryRejectedBackendBeforeReturning(t *testing.T) {
	prepareFailure := errors.New("injected prepare failure")
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "preparer error", err: prepareFailure},
		{name: "ready mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &lifecycleBackend{}
			preparer := rejectedLifecyclePreparer{backend: backend, err: test.err}
			module := newLifecycleModuleFromPreparer(t, preparer)

			_, err := module.Prepare(context.Background(), artifactFromIdentity(
				identityFor("rejected-preparation"),
			))
			if test.err != nil {
				if !IsFailure(err, FailureLost) {
					t.Fatalf("Prepare error = %v, want lost", err)
				}
			} else if !IsFailure(err, FailureIdentity) {
				t.Fatalf("Prepare error = %v, want identity", err)
			}
			if calls := backend.closeCount(); calls != 1 {
				t.Fatalf("rejected backend close calls before return = %d, want 1", calls)
			}
		})
	}
}

func TestPrepareWaitsForScheduledRetirementBeforeBuildingAnotherCandidate(t *testing.T) {
	preparer := &lifecyclePreparer{factory: func() *lifecycleBackend {
		return &lifecycleBackend{}
	}}
	module := newLifecycleModuleFromPreparer(t, preparer)

	first := prepareLifecycleCandidate(t, module, "reap-first")
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	second := prepareLifecycleCandidate(t, module, "reap-second")
	if err := second.Activate(); err != nil {
		t.Fatal(err)
	}
	backends := func() []*lifecycleBackend {
		preparer.mu.Lock()
		defer preparer.mu.Unlock()
		return append([]*lifecycleBackend(nil), preparer.backends...)
	}()
	third := prepareLifecycleCandidate(t, module, "reap-third")
	if calls := backends[0].closeCount(); calls != 1 {
		t.Fatalf("retired backend close calls before next Prepare returns = %d, want 1", calls)
	}
	if err := third.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type rejectedLifecyclePreparer struct {
	backend *lifecycleBackend
	err     error
}

type sequenceLifecyclePreparer struct {
	mu       sync.Mutex
	backends []generationBackend
}

func (preparer *sequenceLifecyclePreparer) Prepare(
	_ context.Context,
	spec backendSpec,
) (generationBackend, backendReady, error) {
	preparer.mu.Lock()
	defer preparer.mu.Unlock()
	if len(preparer.backends) == 0 {
		return nil, backendReady{}, errors.New("test preparer exhausted")
	}
	backend := preparer.backends[0]
	preparer.backends = preparer.backends[1:]
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

type blockingRetirementBackend struct {
	lifecycleBackend
	mu            sync.Mutex
	closeCalls    int
	concurrent    int
	closeStarted  chan struct{}
	closeReturned chan struct{}
	releaseClose  chan struct{}
	startOnce     sync.Once
	returnOnce    sync.Once
	releaseOnce   sync.Once
}

type timedOutRetirementBackend struct {
	mu            sync.Mutex
	closeCalls    int
	firstReturned chan struct{}
	returnOnce    sync.Once
}

type failingRetirementBackend struct {
	lifecycleBackend
	mu            sync.Mutex
	closeCalls    int
	firstError    error
	firstReturned chan struct{}
	returnOnce    sync.Once
}

func newFailingRetirementBackend(firstError error) *failingRetirementBackend {
	return &failingRetirementBackend{
		firstError:    firstError,
		firstReturned: make(chan struct{}),
	}
}

func (backend *failingRetirementBackend) Close(context.Context) error {
	backend.mu.Lock()
	backend.closeCalls++
	call := backend.closeCalls
	backend.mu.Unlock()
	if call != 1 {
		return nil
	}
	backend.returnOnce.Do(func() { close(backend.firstReturned) })
	return backend.firstError
}

func (backend *failingRetirementBackend) closeCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.closeCalls
}

func newTimedOutRetirementBackend() *timedOutRetirementBackend {
	return &timedOutRetirementBackend{firstReturned: make(chan struct{})}
}

func (backend *timedOutRetirementBackend) Apply(
	ctx context.Context,
	operation encodedOperation,
) (wireDecision, error) {
	return (&lifecycleBackend{}).Apply(ctx, operation)
}

func (backend *timedOutRetirementBackend) Close(ctx context.Context) error {
	backend.mu.Lock()
	backend.closeCalls++
	call := backend.closeCalls
	backend.mu.Unlock()
	if call != 1 {
		return nil
	}
	<-ctx.Done()
	backend.returnOnce.Do(func() { close(backend.firstReturned) })
	return ctx.Err()
}

func (backend *timedOutRetirementBackend) closeCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.closeCalls
}

func newBlockingRetirementBackend() *blockingRetirementBackend {
	return &blockingRetirementBackend{
		closeStarted:  make(chan struct{}),
		closeReturned: make(chan struct{}),
		releaseClose:  make(chan struct{}),
	}
}

func (backend *blockingRetirementBackend) Close(ctx context.Context) error {
	backend.mu.Lock()
	backend.closeCalls++
	backend.concurrent++
	backend.mu.Unlock()
	defer func() {
		backend.mu.Lock()
		backend.concurrent--
		backend.mu.Unlock()
	}()
	backend.startOnce.Do(func() { close(backend.closeStarted) })
	defer backend.returnOnce.Do(func() { close(backend.closeReturned) })
	select {
	case <-backend.releaseClose:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (backend *blockingRetirementBackend) closeStats() (calls, concurrent int) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.closeCalls, backend.concurrent
}

func (backend *blockingRetirementBackend) release() {
	backend.releaseOnce.Do(func() { close(backend.releaseClose) })
}

func activateAfterFollowing(candidate *Candidate) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := candidate.Activate()
		if !IsFailure(err, FailureBehind) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

func activateAfterRetirement(candidate *Candidate) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := candidate.Activate()
		if (!IsFailure(err, FailureBehind) &&
			!IsFailure(err, FailureLimit)) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

func (preparer rejectedLifecyclePreparer) Prepare(
	context.Context,
	backendSpec,
) (generationBackend, backendReady, error) {
	return preparer.backend, backendReady{}, preparer.err
}

func newLifecycleModuleFromPreparer(
	t *testing.T,
	preparer backendPreparer,
) *transactionModule[lifecycleRequest, lifecycleResult, lifecycleCheckpoint, lifecycleEffect] {
	return newLifecycleModuleFromPreparerWithPolicy(t, preparer, 1, 1, 0)
}

func newLifecycleModuleFromPreparerWithPolicy(
	t *testing.T,
	preparer backendPreparer,
	maxCandidates int,
	maxRetired int,
	closeTimeout time.Duration,
) *transactionModule[lifecycleRequest, lifecycleResult, lifecycleCheckpoint, lifecycleEffect] {
	t.Helper()
	requestCodec := lifecycleJSONCodec[lifecycleRequest]{}
	resultCodec := lifecycleJSONCodec[lifecycleResult]{}
	checkpointCodec := lifecycleJSONCodec[lifecycleCheckpoint]{}
	effectCodec := lifecycleJSONCodec[lifecycleEffect]{}
	module, err := newTransactionModule(
		moduleConfig[lifecycleRequest, lifecycleResult, lifecycleCheckpoint, lifecycleEffect]{
			Stream:   identityFor("rejected-preparation-stream"),
			Contract: testContract("rejected-preparation"),
			Limits: transactionLimits{
				MaxRequestBytes:    1024,
				MaxResultBytes:     1024,
				MaxCheckpointBytes: 1024,
				MaxOutboxItems:     4,
				MaxOutboxBytes:     1024,
				MaxCandidates:      maxCandidates,
				MaxRetired:         maxRetired,
			},
			RequestCodec:    requestCodec,
			ResultCodec:     resultCodec,
			CheckpointCodec: checkpointCodec,
			OutboxCodec:     effectCodec,
			Preparer:        preparer,
			Journal:         newMemoryJournal(),
			Publisher:       newMemoryPublisher(),
			CloseTimeout:    closeTimeout,
		},
		initialState[lifecycleCheckpoint]{Checkpoint: lifecycleCheckpoint{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = module.Close(context.Background()) })
	return module
}
