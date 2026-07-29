package preparedworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

type moduleOwner struct{}

// Candidate is an owner-qualified, one-shot prepared generation handle.
// Copying it does not duplicate the underlying candidate.
type Candidate struct {
	owner    *moduleOwner
	state    *candidateState
	activate func() error
	close    func(context.Context, *candidateState) error
	timeout  time.Duration
}

// Activate publishes this candidate at an idle, caught-up safe point. It is
// nonblocking and performs no backend, codec, journal, or guest work.
func (candidate *Candidate) Activate() error {
	if candidate == nil || candidate.state == nil || candidate.activate == nil {
		return fail(FailureIdentity, "activate", fmt.Errorf("invalid candidate"))
	}
	return candidate.activate()
}

// Close discards an unused candidate. It is idempotent and context-bounded.
func (candidate *Candidate) Close(ctx context.Context) error {
	if candidate == nil || candidate.state == nil || candidate.close == nil {
		return nil
	}
	var cancel context.CancelFunc
	ctx, cancel = boundedContext(ctx, candidate.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	return candidate.close(ctx, candidate.state)
}

type candidateStatus uint8

const (
	candidateReady candidateStatus = iota + 1
	candidateClosing
	candidateConsumed
	candidateClosed
)

type candidateState struct {
	closeGate        chan struct{}
	base             Position
	checkpointDigest contentDigest
	backend          generationBackend
	status           candidateStatus
	following        bool
	followErr        error
	followCancel     context.CancelFunc
	followDone       chan struct{}
}

type generationState struct {
	epoch   uint64
	backend generationBackend
	lost    bool
}

type retiredGeneration struct {
	backend generationBackend
	// scheduled grants the single owned reaper its next Close attempt.
	scheduled bool
	// failure is an asynchronous Close result not yet reported to a caller.
	failure error
}

// transactionModule owns one serialized typed transaction stream and its replaceable
// prepared backends.
type transactionModule[Q, R, C, E any] struct {
	config   moduleConfig[Q, R, C, E]
	owner    *moduleOwner
	contract contentIdentity

	mu             sync.Mutex
	recoveryMu     sync.Mutex
	head           Position
	checkpoint     *checkpointRecord
	initial        checkpointRecord
	recovered      bool
	active         *generationState
	nextEpoch      uint64
	candidates     map[*candidateState]struct{}
	retired        []retiredGeneration
	reaping        bool
	reaperDone     chan struct{}
	pendingOutbox  bool
	preparing      int
	prepareChanged chan struct{}
	headChanged    chan struct{}
	closing        bool
	closed         bool

	gate           chan struct{}
	lifetime       context.Context
	cancelLifetime context.CancelFunc
}

// newTransactionModule constructs an inert module. Preparation is the first operation
// that reads the journal or creates a backend.
func newTransactionModule[Q, R, C, E any](
	config moduleConfig[Q, R, C, E],
	initial initialState[C],
) (*transactionModule[Q, R, C, E], error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.CloseTimeout == 0 {
		config.CloseTimeout = defaultCloseTimeout
	}
	contract, err := config.Contract.Identity(config.Limits)
	if err != nil {
		return nil, err
	}
	if initial.Position.Sequence == math.MaxUint64 || initial.Position.Revision == math.MaxUint64 {
		return nil, fmt.Errorf("prepared worker: initial position is exhausted")
	}
	payload, _, err := encodeCanonical(
		config.CheckpointCodec,
		initial.Checkpoint,
		config.Limits.MaxCheckpointBytes,
		"initial checkpoint",
	)
	if err != nil {
		return nil, err
	}
	checkpoint := checkpointRecord{
		Schema:   config.Contract.CheckpointSchema,
		Position: initial.Position,
		Payload:  payload,
		Digest:   hashContent(payload),
	}
	lifetime, cancel := context.WithCancel(context.Background())
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	module := &transactionModule[Q, R, C, E]{
		config:         config,
		owner:          &moduleOwner{},
		contract:       contract,
		head:           initial.Position,
		checkpoint:     cloneCheckpoint(&checkpoint),
		initial:        checkpoint,
		candidates:     make(map[*candidateState]struct{}),
		prepareChanged: make(chan struct{}),
		headChanged:    make(chan struct{}),
		gate:           gate,
		lifetime:       lifetime,
		cancelLifetime: cancel,
	}
	return module, nil
}

// Prepare restores and verifies one inert candidate from the latest durable
// quiescent checkpoint. Active transactions may continue while preparation is
// running; activation later rejects a candidate that fell behind.
func (module *transactionModule[Q, R, C, E]) Prepare(
	ctx context.Context,
	artifact preparedArtifact,
) (*Candidate, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if module == nil {
		return nil, fail(FailureClosed, "prepare", fmt.Errorf("nil module"))
	}
	if !artifact.valid() {
		return nil, fail(FailureIdentity, "prepare", fmt.Errorf("zero artifact identity"))
	}
	if artifact.process != nil && artifact.process.descriptor.Contract != module.contract.digest {
		return nil, fail(FailureIdentity, "prepare", fmt.Errorf("artifact contract does not match module contract"))
	}
	if err := module.ensureRecovered(ctx); err != nil {
		return nil, err
	}

	module.mu.Lock()
	if err := module.openErrorLocked("prepare"); err != nil {
		module.mu.Unlock()
		return nil, err
	}
	if module.checkpoint == nil {
		module.mu.Unlock()
		return nil, fail(FailureCorrupt, "prepare", fmt.Errorf("durable stream has no restore checkpoint"))
	}
	if len(module.candidates)+module.preparing >= module.config.Limits.MaxCandidates {
		module.mu.Unlock()
		return nil, fail(FailureLimit, "prepare", fmt.Errorf("candidate limit %d reached", module.config.Limits.MaxCandidates))
	}
	checkpoint := cloneCheckpoint(module.checkpoint)
	module.preparing++
	module.signalPrepareLocked()
	module.mu.Unlock()
	if err := module.reapRetired(ctx); err != nil {
		module.finishPreparation()
		return nil, err
	}

	merged, stop := mergeContext(ctx, module.lifetime)
	backendSpec := backendSpec{
		Artifact:         artifact.identity,
		Contract:         module.contract,
		Stream:           module.config.Stream,
		CheckpointSchema: module.config.Contract.CheckpointSchema,
		Position:         checkpoint.Position,
		CheckpointDigest: checkpoint.Digest,
		Checkpoint:       append([]byte(nil), checkpoint.Payload...),
		Limits:           module.config.Limits,
		artifact:         artifact,
	}
	backend, ready, prepareErr := module.config.Preparer.Prepare(merged, backendSpec)
	stop()
	if prepareErr != nil {
		cleanupErr := module.rejectPreparedBackend(ctx, backend)
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, cleanupErr)
		}
		return nil, fail(FailureLost, "prepare", errors.Join(prepareErr, cleanupErr))
	}
	if backend == nil {
		_ = module.rejectPreparedBackend(ctx, nil)
		return nil, fail(FailureProtocol, "prepare", fmt.Errorf("preparer returned nil backend"))
	}
	if ready != readyFor(backendSpec) {
		cleanupErr := module.rejectPreparedBackend(ctx, backend)
		return nil, fail(
			FailureIdentity,
			"prepare",
			errors.Join(fmt.Errorf("READY identity does not match prepared specification"), cleanupErr),
		)
	}

	module.mu.Lock()
	if module.closing || module.closed {
		module.mu.Unlock()
		cleanupErr := module.rejectPreparedBackend(ctx, backend)
		return nil, fail(
			FailureClosed,
			"prepare",
			errors.Join(fmt.Errorf("module closed during preparation"), cleanupErr),
		)
	}
	state := &candidateState{
		closeGate:        make(chan struct{}, 1),
		base:             checkpoint.Position,
		checkpointDigest: checkpoint.Digest,
		backend:          backend,
		status:           candidateReady,
		followDone:       make(chan struct{}),
	}
	state.closeGate <- struct{}{}
	followContext, cancelFollow := context.WithCancel(module.lifetime)
	state.followCancel = cancelFollow
	module.candidates[state] = struct{}{}
	module.preparing--
	module.signalPrepareLocked()
	module.mu.Unlock()
	go module.followCandidate(followContext, state)
	candidate := &Candidate{
		owner:   module.owner,
		state:   state,
		close:   module.closeCandidate,
		timeout: module.config.CloseTimeout,
	}
	candidate.activate = func() error {
		return module.activateCandidate(candidate)
	}
	return candidate, nil
}

// rejectPreparedBackend retains preparation ownership until the rejected
// backend has either closed or been transferred to Module.Close for retry.
func (module *transactionModule[Q, R, C, E]) rejectPreparedBackend(
	ctx context.Context,
	backend generationBackend,
) error {
	var closeErr error
	if backend != nil {
		closeErr = backend.Close(ctx)
	}
	module.mu.Lock()
	if backend != nil && closeErr != nil {
		module.retired = append(module.retired, retiredGeneration{backend: backend})
	}
	module.finishPreparationLocked()
	module.mu.Unlock()
	return closeErr
}

func (module *transactionModule[Q, R, C, E]) reapRetired(ctx context.Context) error {
	for {
		if err := module.waitForRetiredReaper(ctx); err != nil {
			return err
		}
		module.mu.Lock()
		if err := module.openErrorLocked("prepare"); err != nil {
			module.mu.Unlock()
			return err
		}
		if len(module.retired) == 0 {
			module.mu.Unlock()
			return nil
		}
		if err := module.retired[0].failure; err != nil {
			module.retired[0].failure = nil
			module.mu.Unlock()
			return fail(FailureLost, "prepare", fmt.Errorf("reap retired generation: %w", err))
		}
		module.retired[0].scheduled = true
		module.startRetiredReaperLocked()
		module.mu.Unlock()
	}
}

func (module *transactionModule[Q, R, C, E]) startRetiredReaperLocked() {
	if module.reaping || len(module.retired) == 0 ||
		!module.retired[0].scheduled || module.retired[0].failure != nil {
		return
	}
	done := make(chan struct{})
	module.reaping = true
	module.reaperDone = done
	go module.runRetiredReaper(done)
}

// runRetiredReaper owns every scheduled head until Close succeeds or records
// one bounded failure. The head remains counted toward MaxRetired throughout.
func (module *transactionModule[Q, R, C, E]) runRetiredReaper(done chan struct{}) {
	for {
		module.mu.Lock()
		if len(module.retired) == 0 ||
			!module.retired[0].scheduled || module.retired[0].failure != nil {
			module.finishRetiredReaperLocked(done)
			module.mu.Unlock()
			return
		}
		backend := module.retired[0].backend
		module.mu.Unlock()

		ctx, cancel := boundedContext(context.Background(), module.config.CloseTimeout)
		err := backend.Close(ctx)
		cancel()

		module.mu.Lock()
		if err != nil {
			module.retired[0].scheduled = false
			module.retired[0].failure = err
			module.finishRetiredReaperLocked(done)
			module.mu.Unlock()
			return
		}
		module.retired = module.retired[1:]
		module.mu.Unlock()
	}
}

func (module *transactionModule[Q, R, C, E]) finishRetiredReaperLocked(done chan struct{}) {
	module.reaping = false
	module.reaperDone = nil
	close(done)
}

func (module *transactionModule[Q, R, C, E]) waitForRetiredReaper(ctx context.Context) error {
	for {
		module.mu.Lock()
		if !module.reaping {
			module.mu.Unlock()
			return nil
		}
		done := module.reaperDone
		module.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
}

func (module *transactionModule[Q, R, C, E]) finishPreparation() {
	module.mu.Lock()
	module.finishPreparationLocked()
	module.mu.Unlock()
}

func (module *transactionModule[Q, R, C, E]) finishPreparationLocked() {
	module.preparing--
	module.signalPrepareLocked()
}

func (module *transactionModule[Q, R, C, E]) activateCandidate(candidate *Candidate) error {
	if module == nil {
		return fail(FailureClosed, "activate", fmt.Errorf("nil module"))
	}
	if !module.tryAcquire() {
		return fail(FailureBusy, "activate", fmt.Errorf("transaction already admitted"))
	}
	defer module.release()

	module.mu.Lock()
	defer module.mu.Unlock()
	if err := module.openErrorLocked("activate"); err != nil {
		return err
	}
	if candidate == nil || candidate.owner != module.owner || candidate.state == nil {
		return fail(FailureIdentity, "activate", fmt.Errorf("candidate belongs to another module"))
	}
	state := candidate.state
	if _, ok := module.candidates[state]; !ok || state.status != candidateReady || state.backend == nil {
		return fail(FailureStale, "activate", fmt.Errorf("candidate is no longer ready"))
	}
	if state.followErr != nil {
		return fail(FailureLost, "activate", fmt.Errorf("candidate catch-up failed: %w", state.followErr))
	}
	if state.following || state.base != module.head {
		return fail(FailureBehind, "activate", fmt.Errorf(
			"candidate is at %d/%d while active head is %d/%d",
			state.base.Sequence,
			state.base.Revision,
			module.head.Sequence,
			module.head.Revision,
		))
	}
	if module.checkpoint == nil || module.checkpoint.Position != module.head {
		return fail(FailureNotQuiescent, "activate", fmt.Errorf("active head has pending continuations"))
	}
	if state.checkpointDigest != module.checkpoint.Digest {
		return fail(FailureCorrupt, "activate", fmt.Errorf("candidate checkpoint digest differs at active head"))
	}
	if module.active != nil && len(module.retired) >= module.config.Limits.MaxRetired {
		return fail(FailureLimit, "activate", fmt.Errorf("retired generation limit %d reached", module.config.Limits.MaxRetired))
	}
	if module.nextEpoch == math.MaxUint64 {
		return fail(FailureLimit, "activate", fmt.Errorf("generation epoch exhausted"))
	}
	if module.active != nil {
		module.retired = append(module.retired, retiredGeneration{
			backend:   module.active.backend,
			scheduled: true,
		})
		module.startRetiredReaperLocked()
	}
	module.nextEpoch++
	module.active = &generationState{
		epoch:   module.nextEpoch,
		backend: state.backend,
	}
	state.status = candidateConsumed
	if state.followCancel != nil {
		state.followCancel()
	}
	state.backend = nil
	delete(module.candidates, state)
	return nil
}

// Apply admits at most one typed operation against the current route, durably
// records its completion and outbox, then publishes committed output in order.
// It never exposes generation selection to the caller.
func (module *transactionModule[Q, R, C, E]) Apply(
	ctx context.Context,
	operation Operation[Q],
) (Completion[R], error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return Completion[R]{}, err
	}
	if module == nil {
		return Completion[R]{}, fail(FailureClosed, "apply", fmt.Errorf("nil module"))
	}
	if err := module.ensureRecovered(ctx); err != nil {
		return Completion[R]{}, err
	}
	if !module.tryAcquire() {
		return Completion[R]{}, fail(FailureBusy, "apply", fmt.Errorf("another operation is admitted"))
	}
	defer module.release()

	module.mu.Lock()
	if err := module.openErrorLocked("apply"); err != nil {
		module.mu.Unlock()
		return Completion[R]{}, err
	}
	head := module.head
	module.mu.Unlock()
	request, _, err := encodeCanonical(
		module.config.RequestCodec,
		operation.Request,
		module.config.Limits.MaxRequestBytes,
		"request",
	)
	if err != nil {
		return Completion[R]{}, canonicalFailure("apply", err)
	}
	if operation.Sequence == 0 {
		return Completion[R]{}, fail(FailureGap, "apply", fmt.Errorf("sequence zero is not an operation"))
	}
	if operation.Sequence <= head.Sequence {
		return module.replay(ctx, 0, operation, request, hashContent(request))
	}
	active, head, pending, err := module.currentActive()
	if err != nil {
		return Completion[R]{}, err
	}
	return module.applyAdmitted(ctx, "apply", active, head, pending, operation)
}

// Resolve queries durable state without entering guest code. An unavailable
// or corrupt lookup is unresolved; absence is reported only when the journal
// authoritatively proves that no record exists.
func (module *transactionModule[Q, R, C, E]) Resolve(
	ctx context.Context,
	operation Operation[Q],
) (Resolution[R], error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return Resolution[R]{Status: ResolveUnresolved}, err
	}
	if module == nil {
		return Resolution[R]{Status: ResolveUnresolved}, fail(FailureClosed, "resolve", fmt.Errorf("nil module"))
	}
	if err := module.ensureRecovered(ctx); err != nil {
		return Resolution[R]{Status: ResolveUnresolved}, err
	}
	if !module.tryAcquire() {
		return Resolution[R]{Status: ResolveUnresolved}, fail(FailureBusy, "resolve", fmt.Errorf("another operation is admitted"))
	}
	defer module.release()
	module.mu.Lock()
	openErr := module.openErrorLocked("resolve")
	module.mu.Unlock()
	if openErr != nil {
		return Resolution[R]{Status: ResolveUnresolved}, openErr
	}
	if operation.Sequence == 0 {
		return Resolution[R]{Status: ResolveUnresolved}, fail(FailureGap, "resolve", fmt.Errorf("sequence zero is not an operation"))
	}
	request, _, err := encodeCanonical(
		module.config.RequestCodec,
		operation.Request,
		module.config.Limits.MaxRequestBytes,
		"request",
	)
	if err != nil {
		return Resolution[R]{Status: ResolveUnresolved}, canonicalFailure("resolve", err)
	}
	record, ok, err := module.config.Journal.Lookup(ctx, module.config.Stream, operation.Sequence)
	if err != nil {
		return Resolution[R]{Status: ResolveUnresolved}, fail(FailureUnknownCommit, "resolve", err)
	}
	if !ok {
		return Resolution[R]{Status: ResolveNotCommitted}, nil
	}
	resolution := Resolution[R]{Status: ResolveCommitted}
	if err := module.validatePersistedRecord(record); err != nil {
		return Resolution[R]{Status: ResolveUnresolved}, err
	}
	requestDigest := hashContent(request)
	if record.BaseRevision != operation.BaseRevision || record.RequestDigest != requestDigest ||
		!bytes.Equal(record.Request, request) {
		return resolution, fail(
			FailureConflict,
			"resolve",
			fmt.Errorf("sequence %d has another canonical request", operation.Sequence),
		)
	}
	result, _, err := decodeAndCanonicalize(
		module.config.ResultCodec,
		record.Result,
		module.config.Limits.MaxResultBytes,
		"committed result",
	)
	if err != nil {
		return Resolution[R]{Status: ResolveUnresolved}, fail(FailureCorrupt, "resolve", err)
	}
	resolution.Completion = Completion[R]{
		Position: Position{Sequence: record.Sequence, Revision: record.Revision},
		Result:   result,
	}
	if err := module.publishRecord(ctx, record); err != nil {
		var delivery *Failure
		if errors.As(err, &delivery) && delivery.Kind == FailureDelivery {
			return resolution, fail(FailureDelivery, "resolve", delivery.Err)
		}
		return resolution, fail(FailureDelivery, "resolve", err)
	}
	return resolution, nil
}

func (module *transactionModule[Q, R, C, E]) applyAdmitted(
	ctx context.Context,
	operationName string,
	active *generationState,
	head Position,
	pending bool,
	operation Operation[Q],
) (Completion[R], error) {
	request, _, err := encodeCanonical(
		module.config.RequestCodec,
		operation.Request,
		module.config.Limits.MaxRequestBytes,
		"request",
	)
	if err != nil {
		return Completion[R]{}, canonicalFailure(operationName, err)
	}
	requestDigest := hashContent(request)

	if operation.Sequence == 0 {
		return Completion[R]{}, fail(FailureGap, operationName, fmt.Errorf("sequence zero is not an operation"))
	}
	if operation.Sequence <= head.Sequence {
		return module.replay(ctx, active.epoch, operation, request, requestDigest)
	}
	if head.Sequence == math.MaxUint64 || head.Revision == math.MaxUint64 {
		return Completion[R]{}, fail(FailureLimit, operationName, fmt.Errorf("durable position exhausted"))
	}
	if operation.Sequence != head.Sequence+1 {
		return Completion[R]{}, fail(FailureGap, operationName, fmt.Errorf("sequence %d, want %d", operation.Sequence, head.Sequence+1))
	}
	if operation.BaseRevision != head.Revision {
		return Completion[R]{}, fail(FailureConflict, operationName, fmt.Errorf("base revision %d, want %d", operation.BaseRevision, head.Revision))
	}
	if pending {
		if err := module.publishHead(ctx, active.epoch, head.Sequence); err != nil {
			return Completion[R]{}, err
		}
	}

	merged, stop := mergeContext(ctx, module.lifetime)
	encoded, applyErr := active.backend.Apply(merged, encodedOperation{
		Sequence:     operation.Sequence,
		BaseRevision: operation.BaseRevision,
		Request:      append([]byte(nil), request...),
	})
	stop()
	if applyErr != nil {
		module.markLost(active.epoch)
		if err := ctx.Err(); err != nil {
			return Completion[R]{}, fail(FailureUnknownCommit, operationName, errors.Join(err, applyErr))
		}
		var backendFailure *Failure
		if errors.As(applyErr, &backendFailure) {
			return Completion[R]{}, applyErr
		}
		return Completion[R]{}, fail(FailureGuest, operationName, applyErr)
	}
	wantPosition := Position{Sequence: operation.Sequence, Revision: operation.BaseRevision + 1}
	if encoded.Position != wantPosition {
		module.markLost(active.epoch)
		return Completion[R]{}, fail(FailureProtocol, operationName, fmt.Errorf(
			"completion position %d/%d, want %d/%d",
			encoded.Position.Sequence,
			encoded.Position.Revision,
			wantPosition.Sequence,
			wantPosition.Revision,
		))
	}
	result, resultBytes, err := decodeAndCanonicalize(
		module.config.ResultCodec,
		encoded.Result,
		module.config.Limits.MaxResultBytes,
		"result",
	)
	if err != nil {
		module.markLost(active.epoch)
		return Completion[R]{}, canonicalFailure(operationName, err)
	}
	if encoded.Quiescent != encoded.HasCheckpoint {
		module.markLost(active.epoch)
		return Completion[R]{}, fail(
			FailureProtocol,
			operationName,
			fmt.Errorf("backend quiescence and checkpoint presence differ"),
		)
	}
	decision := Decision[R, C, E]{
		Result: result,
	}
	if encoded.HasCheckpoint {
		checkpoint, _, err := decodeAndCanonicalize(
			module.config.CheckpointCodec,
			encoded.Checkpoint,
			module.config.Limits.MaxCheckpointBytes,
			"checkpoint",
		)
		if err != nil {
			module.markLost(active.epoch)
			return Completion[R]{}, canonicalFailure(operationName, err)
		}
		decision.Checkpoint = &checkpoint
	}
	if len(encoded.Effects) > module.config.Limits.MaxOutboxItems {
		module.markLost(active.epoch)
		return Completion[R]{}, fail(
			FailureLimit,
			operationName,
			fmt.Errorf("effects have %d items, limit %d", len(encoded.Effects), module.config.Limits.MaxOutboxItems),
		)
	}
	totalEffects := 0
	for _, payload := range encoded.Effects {
		totalEffects += len(payload)
		if totalEffects > module.config.Limits.MaxOutboxBytes {
			module.markLost(active.epoch)
			return Completion[R]{}, fail(
				FailureLimit,
				operationName,
				fmt.Errorf("effects use %d bytes, limit %d", totalEffects, module.config.Limits.MaxOutboxBytes),
			)
		}
		effect, _, err := decodeAndCanonicalize(
			module.config.OutboxCodec,
			payload,
			module.config.Limits.MaxOutboxBytes,
			"effect",
		)
		if err != nil {
			module.markLost(active.epoch)
			return Completion[R]{}, canonicalFailure(operationName, err)
		}
		decision.Effects = append(decision.Effects, effect)
	}
	completion := Completion[R]{Position: wantPosition, Result: result}
	record, err := module.buildRecord(operation, request, requestDigest, resultBytes, decision)
	if err != nil {
		module.markLost(active.epoch)
		return Completion[R]{}, err
	}

	stored, err := module.commit(ctx, active.epoch, head, record)
	if err != nil {
		return Completion[R]{}, err
	}
	module.acceptCommit(active.epoch, stored)
	if err := module.publishRecord(ctx, stored); err != nil {
		return completion, err
	}
	module.clearPendingAt(stored)
	return completion, nil
}

// Close rejects new work immediately, cancels admitted work, and owns cleanup
// of active, candidate, and retired backends. A deadline failure may be retried.
func (module *transactionModule[Q, R, C, E]) Close(ctx context.Context) error {
	if module == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	module.mu.Lock()
	if module.closed {
		module.mu.Unlock()
		return nil
	}
	if !module.closing {
		module.closing = true
		module.cancelLifetime()
	}
	module.mu.Unlock()

	if err := module.waitForPrepares(ctx); err != nil {
		return err
	}
	if err := module.acquire(ctx); err != nil {
		return err
	}
	defer module.release()
	if err := module.waitForRetiredReaper(ctx); err != nil {
		return err
	}

	module.mu.Lock()
	candidates := make([]*candidateState, 0, len(module.candidates))
	for candidate := range module.candidates {
		candidates = append(candidates, candidate)
	}
	module.mu.Unlock()

	for _, candidate := range candidates {
		if err := module.closeCandidate(ctx, candidate); err != nil {
			return err
		}
	}

	module.mu.Lock()
	active := module.active
	module.mu.Unlock()
	if active != nil && active.backend != nil {
		if err := active.backend.Close(ctx); err != nil {
			return err
		}
		module.mu.Lock()
		if module.active == active {
			module.active = nil
		}
		module.mu.Unlock()
	}

	for {
		module.mu.Lock()
		if len(module.retired) == 0 {
			module.mu.Unlock()
			break
		}
		if err := module.retired[0].failure; err != nil {
			module.retired[0].failure = nil
			module.mu.Unlock()
			return err
		}
		backend := module.retired[0].backend
		module.mu.Unlock()
		if err := backend.Close(ctx); err != nil {
			return err
		}
		module.mu.Lock()
		if len(module.retired) != 0 {
			module.retired = module.retired[1:]
		}
		module.mu.Unlock()
	}

	module.mu.Lock()
	module.closed = true
	module.mu.Unlock()
	return nil
}

func validateConfig[Q, R, C, E any](config moduleConfig[Q, R, C, E]) error {
	if !config.Stream.valid() {
		return fmt.Errorf("prepared worker: stream identity is required")
	}
	if _, err := config.Contract.Identity(config.Limits); err != nil {
		return err
	}
	if config.RequestCodec == nil || config.ResultCodec == nil || config.CheckpointCodec == nil ||
		config.OutboxCodec == nil || config.Preparer == nil ||
		config.Journal == nil || config.Publisher == nil {
		return fmt.Errorf("prepared worker: incomplete module configuration")
	}
	if config.CloseTimeout < 0 {
		return fmt.Errorf("prepared worker: negative close timeout")
	}
	if config.CloseTimeout == 0 {
		config.CloseTimeout = defaultCloseTimeout
	}
	return nil
}

func (module *transactionModule[Q, R, C, E]) ensureRecovered(ctx context.Context) error {
	module.recoveryMu.Lock()
	defer module.recoveryMu.Unlock()
	module.mu.Lock()
	if module.recovered {
		module.mu.Unlock()
		return nil
	}
	if err := module.openErrorLocked("recover"); err != nil {
		module.mu.Unlock()
		return err
	}
	module.mu.Unlock()

	record, ok, err := module.config.Journal.Latest(ctx, module.config.Stream)
	if err != nil {
		return fail(FailureLost, "recover", err)
	}
	if !ok {
		module.mu.Lock()
		module.recovered = true
		module.mu.Unlock()
		return nil
	}
	if err := module.validatePersistedRecord(record); err != nil {
		return err
	}
	position := Position{Sequence: record.Sequence, Revision: record.Revision}
	if position.Sequence < module.initial.Position.Sequence || position.Revision < module.initial.Position.Revision {
		return fail(FailureCorrupt, "recover", fmt.Errorf("journal head precedes initial checkpoint"))
	}
	checkpoint := cloneCheckpoint(&module.initial)
	quiescent, quiescentOK, err := module.config.Journal.LatestQuiescent(ctx, module.config.Stream)
	if err != nil {
		return fail(FailureLost, "recover", err)
	}
	if quiescentOK {
		if err := module.validatePersistedRecord(quiescent); err != nil {
			return err
		}
		if quiescent.Checkpoint == nil {
			return fail(FailureCorrupt, "recover", fmt.Errorf("latest quiescent record has no checkpoint"))
		}
		checkpoint = cloneCheckpoint(quiescent.Checkpoint)
	}
	if checkpoint.Position.Sequence < module.initial.Position.Sequence ||
		checkpoint.Position.Revision < module.initial.Position.Revision ||
		checkpoint.Position.Sequence > position.Sequence || checkpoint.Position.Revision > position.Revision {
		return fail(FailureCorrupt, "recover", fmt.Errorf("restore checkpoint is outside durable history"))
	}
	module.mu.Lock()
	module.head = position
	module.checkpoint = checkpoint
	module.pendingOutbox = recordHasPendingOutbox(record)
	module.recovered = true
	module.mu.Unlock()
	return nil
}

func (module *transactionModule[Q, R, C, E]) currentActive() (*generationState, Position, bool, error) {
	module.mu.Lock()
	defer module.mu.Unlock()
	if err := module.openErrorLocked("apply"); err != nil {
		return nil, Position{}, false, err
	}
	if module.active == nil {
		return nil, Position{}, false, fail(FailureLost, "apply", fmt.Errorf("no active generation"))
	}
	if module.active.lost || module.active.backend == nil {
		return nil, Position{}, false, fail(FailureLost, "apply", fmt.Errorf("active generation is quarantined"))
	}
	return module.active, module.head, module.pendingOutbox, nil
}

func (module *transactionModule[Q, R, C, E]) replay(
	ctx context.Context,
	epoch uint64,
	operation Operation[Q],
	request []byte,
	requestDigest contentDigest,
) (Completion[R], error) {
	record, ok, err := module.config.Journal.Lookup(ctx, module.config.Stream, operation.Sequence)
	if err != nil {
		return Completion[R]{}, fail(FailureLost, "replay", err)
	}
	if !ok {
		module.markLost(epoch)
		return Completion[R]{}, fail(FailureCorrupt, "replay", fmt.Errorf("committed sequence %d is missing", operation.Sequence))
	}
	if err := module.validatePersistedRecord(record); err != nil {
		module.markLost(epoch)
		return Completion[R]{}, err
	}
	if record.BaseRevision != operation.BaseRevision || record.RequestDigest != requestDigest ||
		!bytes.Equal(record.Request, request) {
		return Completion[R]{}, fail(FailureConflict, "replay", fmt.Errorf("sequence %d has another canonical request", operation.Sequence))
	}
	result, _, err := decodeAndCanonicalize(
		module.config.ResultCodec,
		record.Result,
		module.config.Limits.MaxResultBytes,
		"cached result",
	)
	if err != nil {
		module.markLost(epoch)
		return Completion[R]{}, canonicalFailure("replay", err)
	}
	completion := Completion[R]{
		Position: Position{Sequence: record.Sequence, Revision: record.Revision},
		Result:   result,
	}
	if err := module.publishRecord(ctx, record); err != nil {
		return completion, err
	}
	module.clearPendingAt(record)
	return completion, nil
}

func (module *transactionModule[Q, R, C, E]) buildRecord(
	operation Operation[Q],
	request []byte,
	requestDigest contentDigest,
	result []byte,
	decision Decision[R, C, E],
) (transactionRecord, error) {
	record := transactionRecord{
		Stream:        module.config.Stream,
		Sequence:      operation.Sequence,
		BaseRevision:  operation.BaseRevision,
		Revision:      operation.BaseRevision + 1,
		RequestDigest: requestDigest,
		Request:       append([]byte(nil), request...),
		ResultDigest:  hashContent(result),
		Result:        append([]byte(nil), result...),
	}
	if decision.Checkpoint != nil {
		payload, _, err := encodeCanonical(
			module.config.CheckpointCodec,
			*decision.Checkpoint,
			module.config.Limits.MaxCheckpointBytes,
			"checkpoint",
		)
		if err != nil {
			return transactionRecord{}, canonicalFailure("transact", err)
		}
		record.Checkpoint = &checkpointRecord{
			Schema:   module.config.Contract.CheckpointSchema,
			Position: Position{Sequence: record.Sequence, Revision: record.Revision},
			Payload:  payload,
			Digest:   hashContent(payload),
		}
	}
	record.Quiescent = decision.Checkpoint != nil
	if len(decision.Effects) > module.config.Limits.MaxOutboxItems {
		return transactionRecord{}, fail(FailureLimit, "apply", fmt.Errorf("outbox has %d items, limit %d", len(decision.Effects), module.config.Limits.MaxOutboxItems))
	}
	var outboxBytes int
	for index, value := range decision.Effects {
		payload, _, err := encodeCanonical(
			module.config.OutboxCodec,
			value,
			module.config.Limits.MaxOutboxBytes,
			"outbox item",
		)
		if err != nil {
			return transactionRecord{}, canonicalFailure("apply", err)
		}
		outboxBytes += len(payload)
		if outboxBytes > module.config.Limits.MaxOutboxBytes {
			return transactionRecord{}, fail(FailureLimit, "apply", fmt.Errorf("outbox uses %d bytes, limit %d", outboxBytes, module.config.Limits.MaxOutboxBytes))
		}
		record.Outbox = append(record.Outbox, outboxRecord{
			ID: outboxID{
				Stream:   module.config.Stream,
				Sequence: operation.Sequence,
				Ordinal:  uint32(index + 1),
			},
			Payload: append([]byte(nil), payload...),
			Digest:  hashContent(payload),
		})
	}
	record.Digest = recordDigest(record)
	return record, nil
}

func (module *transactionModule[Q, R, C, E]) commit(
	ctx context.Context,
	epoch uint64,
	expected Position,
	record transactionRecord,
) (transactionRecord, error) {
	disposition, commitErr := module.config.Journal.Commit(ctx, expected, record)
	if disposition == commitStored {
		return record, nil
	}
	stored, ok, lookupErr := module.config.Journal.Lookup(ctx, module.config.Stream, record.Sequence)
	if lookupErr == nil && ok {
		if err := module.validatePersistedRecord(stored); err != nil {
			module.markLost(epoch)
			return transactionRecord{}, err
		}
		if stored.Digest == record.Digest {
			return stored, nil
		}
		module.markLost(epoch)
		if stored.RequestDigest != record.RequestDigest {
			return transactionRecord{}, fail(FailureConflict, "commit", fmt.Errorf("sequence %d committed another request", record.Sequence))
		}
		return transactionRecord{}, fail(FailureCorrupt, "commit", fmt.Errorf("sequence %d committed another result", record.Sequence))
	}
	module.markLost(epoch)
	if disposition == commitUnknown || commitErr != nil || lookupErr != nil {
		return transactionRecord{}, fail(FailureUnknownCommit, "commit", errors.Join(commitErr, lookupErr))
	}
	return transactionRecord{}, fail(FailureConflict, "commit", fmt.Errorf("journal rejected head %d/%d", expected.Sequence, expected.Revision))
}

func (module *transactionModule[Q, R, C, E]) acceptCommit(epoch uint64, record transactionRecord) {
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.active == nil || module.active.epoch != epoch {
		return
	}
	module.head = Position{Sequence: record.Sequence, Revision: record.Revision}
	if record.Checkpoint != nil {
		module.checkpoint = cloneCheckpoint(record.Checkpoint)
	}
	module.pendingOutbox = recordHasPendingOutbox(record)
	module.signalHeadLocked()
}

func (module *transactionModule[Q, R, C, E]) followCandidate(
	ctx context.Context,
	state *candidateState,
) {
	defer close(state.followDone)
	for {
		module.mu.Lock()
		if state.status != candidateReady || state.followErr != nil {
			module.mu.Unlock()
			return
		}
		if state.base == module.head {
			changed := module.headChanged
			module.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-changed:
				continue
			}
		}
		if state.base.Sequence == math.MaxUint64 {
			state.followErr = fmt.Errorf("candidate sequence is exhausted")
			module.mu.Unlock()
			return
		}
		nextSequence := state.base.Sequence + 1
		backend := state.backend
		state.following = true
		module.mu.Unlock()

		err := module.followCandidateRecord(ctx, state, backend, nextSequence)
		module.mu.Lock()
		state.following = false
		if err != nil && state.status == candidateReady && ctx.Err() == nil {
			state.followErr = err
		}
		shouldStop := state.status != candidateReady || state.followErr != nil || ctx.Err() != nil
		module.mu.Unlock()
		if shouldStop {
			return
		}
	}
}

func (module *transactionModule[Q, R, C, E]) followCandidateRecord(
	ctx context.Context,
	state *candidateState,
	backend generationBackend,
	sequence uint64,
) error {
	record, ok, err := module.config.Journal.Lookup(ctx, module.config.Stream, sequence)
	if err != nil {
		return fmt.Errorf("lookup sequence %d: %w", sequence, err)
	}
	if !ok {
		return fmt.Errorf("committed sequence %d is absent", sequence)
	}
	if err := module.validatePersistedRecord(record); err != nil {
		return err
	}
	operation := encodedOperation{
		Sequence: record.Sequence, BaseRevision: record.BaseRevision,
		Request: append([]byte(nil), record.Request...),
	}
	expected := replayRecordDigest(record)
	var decision wireDecision
	if replayBackend, ok := backend.(interface {
		Replay(context.Context, encodedOperation, contentDigest) (wireDecision, error)
	}); ok {
		decision, err = replayBackend.Replay(ctx, operation, expected)
	} else {
		decision, err = backend.Apply(ctx, operation)
	}
	if err != nil {
		return fmt.Errorf("replay sequence %d: %w", sequence, err)
	}
	candidateRecord, err := encodedReplayRecord(backendSpec{
		Contract: module.contract,
		Stream:   module.config.Stream, CheckpointSchema: module.config.Contract.CheckpointSchema,
		Limits: module.config.Limits,
	}, operation, decision)
	if err != nil {
		return fmt.Errorf("validate replay sequence %d: %w", sequence, err)
	}
	if replayRecordDigest(candidateRecord) != expected {
		return fmt.Errorf("replay sequence %d differs from committed decision", sequence)
	}

	module.mu.Lock()
	defer module.mu.Unlock()
	if state.status != candidateReady || state.backend != backend {
		return context.Canceled
	}
	if state.base.Sequence+1 != record.Sequence || state.base.Revision != record.BaseRevision {
		return fmt.Errorf("candidate base changed during replay")
	}
	state.base = Position{Sequence: record.Sequence, Revision: record.Revision}
	if record.Checkpoint == nil {
		state.checkpointDigest = contentDigest{}
	} else {
		state.checkpointDigest = record.Checkpoint.Digest
	}
	return nil
}

func (module *transactionModule[Q, R, C, E]) publishHead(ctx context.Context, epoch, sequence uint64) error {
	record, ok, err := module.config.Journal.Lookup(ctx, module.config.Stream, sequence)
	if err != nil {
		return fail(FailureLost, "publish", err)
	}
	if !ok {
		module.markLost(epoch)
		return fail(FailureCorrupt, "publish", fmt.Errorf("head sequence %d is missing", sequence))
	}
	if err := module.validatePersistedRecord(record); err != nil {
		module.markLost(epoch)
		return err
	}
	if err := module.publishRecord(ctx, record); err != nil {
		return err
	}
	module.clearPendingAt(record)
	return nil
}

func (module *transactionModule[Q, R, C, E]) publishRecord(ctx context.Context, record transactionRecord) error {
	for _, output := range record.Outbox {
		if output.Published {
			continue
		}
		if err := module.config.Publisher.Publish(ctx, output); err != nil {
			return fail(FailureDelivery, "publish", err)
		}
		if err := module.config.Journal.MarkPublished(ctx, module.config.Stream, output.ID); err != nil {
			return fail(FailureDelivery, "publish", err)
		}
	}
	return nil
}

func (module *transactionModule[Q, R, C, E]) validatePersistedRecord(record transactionRecord) error {
	if err := validateRecordStructure(record, module.config.Stream, module.config.Contract.CheckpointSchema, module.config.Limits); err != nil {
		return fail(FailureCorrupt, "journal", err)
	}
	if _, _, err := decodeAndCanonicalize(module.config.RequestCodec, record.Request, module.config.Limits.MaxRequestBytes, "journal request"); err != nil {
		return fail(FailureCorrupt, "journal", err)
	}
	if _, _, err := decodeAndCanonicalize(module.config.ResultCodec, record.Result, module.config.Limits.MaxResultBytes, "journal result"); err != nil {
		return fail(FailureCorrupt, "journal", err)
	}
	if record.Checkpoint != nil {
		if _, _, err := decodeAndCanonicalize(module.config.CheckpointCodec, record.Checkpoint.Payload, module.config.Limits.MaxCheckpointBytes, "journal checkpoint"); err != nil {
			return fail(FailureCorrupt, "journal", err)
		}
	}
	if record.Quiescent != (record.Checkpoint != nil) {
		return fmt.Errorf("record quiescence and checkpoint presence differ")
	}
	for _, output := range record.Outbox {
		if _, _, err := decodeAndCanonicalize(module.config.OutboxCodec, output.Payload, module.config.Limits.MaxOutboxBytes, "journal outbox"); err != nil {
			return fail(FailureCorrupt, "journal", err)
		}
	}
	return nil
}

func (module *transactionModule[Q, R, C, E]) closeCandidate(ctx context.Context, state *candidateState) error {
	if state == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-state.closeGate:
	}
	defer func() { state.closeGate <- struct{}{} }()
	module.mu.Lock()
	if state.status == candidateClosed || state.status == candidateConsumed {
		module.mu.Unlock()
		return nil
	}
	if _, ok := module.candidates[state]; !ok {
		module.mu.Unlock()
		return nil
	}
	state.status = candidateClosing
	if state.followCancel != nil {
		state.followCancel()
	}
	following := state.following
	followDone := state.followDone
	backend := state.backend
	module.mu.Unlock()
	if following {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-followDone:
		}
	}
	if backend == nil {
		module.mu.Lock()
		state.status = candidateClosed
		delete(module.candidates, state)
		module.mu.Unlock()
		return nil
	}
	if err := backend.Close(ctx); err != nil {
		return err
	}
	module.mu.Lock()
	state.backend = nil
	state.status = candidateClosed
	delete(module.candidates, state)
	module.mu.Unlock()
	return nil
}

func (module *transactionModule[Q, R, C, E]) openErrorLocked(operation string) error {
	if module.closed || module.closing {
		return fail(FailureClosed, operation, fmt.Errorf("module is closed"))
	}
	return nil
}

func (module *transactionModule[Q, R, C, E]) markLost(epoch uint64) {
	module.mu.Lock()
	if module.active != nil && module.active.epoch == epoch {
		module.active.lost = true
	}
	module.mu.Unlock()
}

func (module *transactionModule[Q, R, C, E]) clearPendingAt(record transactionRecord) {
	module.mu.Lock()
	if module.head == (Position{Sequence: record.Sequence, Revision: record.Revision}) {
		module.pendingOutbox = false
	}
	module.mu.Unlock()
}

func (module *transactionModule[Q, R, C, E]) signalPrepareLocked() {
	close(module.prepareChanged)
	module.prepareChanged = make(chan struct{})
}

func (module *transactionModule[Q, R, C, E]) signalHeadLocked() {
	close(module.headChanged)
	module.headChanged = make(chan struct{})
}

func (module *transactionModule[Q, R, C, E]) waitForPrepares(ctx context.Context) error {
	for {
		module.mu.Lock()
		if module.preparing == 0 {
			module.mu.Unlock()
			return nil
		}
		changed := module.prepareChanged
		module.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (module *transactionModule[Q, R, C, E]) tryAcquire() bool {
	select {
	case <-module.gate:
		return true
	default:
		return false
	}
}

func (module *transactionModule[Q, R, C, E]) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-module.gate:
		return nil
	}
}

func (module *transactionModule[Q, R, C, E]) release() {
	module.gate <- struct{}{}
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func mergeContext(parent, lifetime context.Context) (context.Context, func()) {
	merged, cancel := context.WithCancel(normalizeContext(parent))
	stop := context.AfterFunc(lifetime, cancel)
	return merged, func() {
		stop()
		cancel()
	}
}

type canonicalRecordError struct {
	limit bool
	err   error
}

func (recordError *canonicalRecordError) Error() string { return recordError.err.Error() }
func (recordError *canonicalRecordError) Unwrap() error { return recordError.err }

func encodeCanonical[T any](codec Codec[T], value T, limit int, label string) ([]byte, T, error) {
	encoded, err := codec.Encode(value)
	if err != nil {
		var zero T
		return nil, zero, &canonicalRecordError{err: fmt.Errorf("%s encode: %w", label, err)}
	}
	return canonicalizeEncoded(codec, encoded, limit, label)
}

func decodeCanonical[T any](codec Codec[T], encoded []byte, limit int, label string) (T, error) {
	value, _, err := decodeAndCanonicalize(codec, encoded, limit, label)
	return value, err
}

func decodeAndCanonicalize[T any](codec Codec[T], encoded []byte, limit int, label string) (T, []byte, error) {
	canonical, value, err := canonicalizeEncoded(codec, encoded, limit, label)
	return value, canonical, err
}

func canonicalizeEncoded[T any](codec Codec[T], encoded []byte, limit int, label string) ([]byte, T, error) {
	var zero T
	if len(encoded) > limit {
		return nil, zero, &canonicalRecordError{
			limit: true,
			err:   fmt.Errorf("%s uses %d bytes, limit %d", label, len(encoded), limit),
		}
	}
	value, err := codec.Decode(encoded)
	if err != nil {
		return nil, zero, &canonicalRecordError{err: fmt.Errorf("%s decode: %w", label, err)}
	}
	canonical, err := codec.Encode(value)
	if err != nil {
		return nil, zero, &canonicalRecordError{err: fmt.Errorf("%s re-encode: %w", label, err)}
	}
	if len(canonical) > limit {
		return nil, zero, &canonicalRecordError{
			limit: true,
			err:   fmt.Errorf("canonical %s uses %d bytes, limit %d", label, len(canonical), limit),
		}
	}
	if !bytes.Equal(encoded, canonical) {
		return nil, zero, &canonicalRecordError{err: fmt.Errorf("%s is not canonical", label)}
	}
	return append([]byte(nil), canonical...), value, nil
}

func canonicalFailure(operation string, err error) error {
	var recordError *canonicalRecordError
	if errors.As(err, &recordError) && recordError.limit {
		return fail(FailureLimit, operation, err)
	}
	return fail(FailureProtocol, operation, err)
}

func validateRecordStructure(record transactionRecord, stream, schema contentIdentity, limits transactionLimits) error {
	if record.Stream != stream {
		return fmt.Errorf("record identity differs")
	}
	if record.Sequence == 0 || record.BaseRevision == math.MaxUint64 ||
		record.Revision != record.BaseRevision+1 {
		return fmt.Errorf("record position %d/%d from base %d is invalid", record.Sequence, record.Revision, record.BaseRevision)
	}
	if len(record.Request) > limits.MaxRequestBytes || record.RequestDigest != hashContent(record.Request) {
		return fmt.Errorf("record request is invalid")
	}
	if len(record.Result) > limits.MaxResultBytes || record.ResultDigest != hashContent(record.Result) {
		return fmt.Errorf("record result is invalid")
	}
	if record.Checkpoint != nil {
		if record.Checkpoint.Schema != schema ||
			record.Checkpoint.Position != (Position{Sequence: record.Sequence, Revision: record.Revision}) ||
			len(record.Checkpoint.Payload) > limits.MaxCheckpointBytes ||
			record.Checkpoint.Digest != hashContent(record.Checkpoint.Payload) {
			return fmt.Errorf("record checkpoint is invalid")
		}
	}
	if len(record.Outbox) > limits.MaxOutboxItems {
		return fmt.Errorf("record outbox has %d items", len(record.Outbox))
	}
	var total int
	for index, output := range record.Outbox {
		total += len(output.Payload)
		if output.ID.Stream != stream || output.ID.Sequence != record.Sequence ||
			output.ID.Ordinal != uint32(index+1) || output.Digest != hashContent(output.Payload) {
			return fmt.Errorf("record outbox item %d is invalid", index+1)
		}
	}
	if total > limits.MaxOutboxBytes {
		return fmt.Errorf("record outbox uses %d bytes", total)
	}
	if record.Digest != recordDigest(record) {
		return fmt.Errorf("record digest differs")
	}
	return nil
}

func recordDigest(record transactionRecord) contentDigest {
	var encoded bytes.Buffer
	writeDigest := func(digest contentDigest) { _, _ = encoded.Write(digest[:]) }
	writeIdentity := func(identity contentIdentity) { writeDigest(identity.digest) }
	writeUint64 := func(value uint64) { _ = binary.Write(&encoded, binary.BigEndian, value) }
	writeUint32 := func(value uint32) { _ = binary.Write(&encoded, binary.BigEndian, value) }
	writeBytes := func(value []byte) {
		writeUint64(uint64(len(value)))
		_, _ = encoded.Write(value)
	}
	writeIdentity(record.Stream)
	writeUint64(record.Sequence)
	writeUint64(record.BaseRevision)
	writeUint64(record.Revision)
	writeDigest(record.RequestDigest)
	writeBytes(record.Request)
	writeDigest(record.ResultDigest)
	writeBytes(record.Result)
	if record.Checkpoint == nil {
		_ = encoded.WriteByte(0)
	} else {
		_ = encoded.WriteByte(1)
		writeIdentity(record.Checkpoint.Schema)
		writeUint64(record.Checkpoint.Position.Sequence)
		writeUint64(record.Checkpoint.Position.Revision)
		writeDigest(record.Checkpoint.Digest)
		writeBytes(record.Checkpoint.Payload)
	}
	if record.Quiescent {
		_ = encoded.WriteByte(1)
	} else {
		_ = encoded.WriteByte(0)
	}
	writeUint64(uint64(len(record.Outbox)))
	for _, output := range record.Outbox {
		writeIdentity(output.ID.Stream)
		writeUint64(output.ID.Sequence)
		writeUint32(output.ID.Ordinal)
		writeDigest(output.Digest)
		writeBytes(output.Payload)
	}
	return hashContent(encoded.Bytes())
}

func cloneCheckpoint(checkpoint *checkpointRecord) *checkpointRecord {
	if checkpoint == nil {
		return nil
	}
	cloned := *checkpoint
	cloned.Payload = append([]byte(nil), checkpoint.Payload...)
	return &cloned
}

func recordHasPendingOutbox(record transactionRecord) bool {
	for _, output := range record.Outbox {
		if !output.Published {
			return true
		}
	}
	return false
}
