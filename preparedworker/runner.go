package preparedworker

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	workercontract "github.com/besmpl/ember/internal/preparedworkercontract"
)

const (
	defaultCloseTimeout = 5 * time.Second
	defaultCandidates   = 2
	defaultRetired      = 2
)

// Runner is the complete steady-state transaction capability. It exposes no
// build, generation, persistence, process, or protocol mechanism.
type Runner[Q, R any] interface {
	Apply(context.Context, Operation[Q]) (Completion[R], error)
	Resolve(context.Context, Operation[Q]) (Resolution[R], error)
	Close(context.Context) error
}

// Reload is the development-only capability for preparing an immutable build.
// It is intentionally separate from Runner so release callers cannot recover
// reload authority through a type assertion.
type Reload interface {
	Prepare(context.Context, Build) (*Candidate, error)
}

// Build is opaque launch authority for one verified immutable worker build.
type Build struct {
	artifact preparedArtifact
}

func buildFromArtifact(artifact preparedArtifact) Build {
	return Build{artifact: artifact}
}

func (build Build) valid() bool {
	return build.artifact.valid()
}

// Schema binds one application record type to a canonical codec and bound.
// For Effect, MaxBytes also bounds the combined effects in one decision.
type Schema[T any] struct {
	Name     string
	MaxBytes int
	Codec    Codec[T]
}

// Contract is the complete typed application transaction contract.
type Contract[Q, R, C, E any] struct {
	CodecVersion uint32
	Request      Schema[Q]
	Result       Schema[R]
	Checkpoint   Schema[C]
	Effect       Schema[E]
	MaxEffects   int
}

// Restore is the detached durable state supplied to a new embedded handler.
type Restore[C any] struct {
	Position   Position
	Checkpoint C
}

// HandlerFactory restores one complete embedded static-AOT handler.
type HandlerFactory[Q, R, C, E any] func(
	context.Context,
	Restore[C],
) (Handler[Q, R, C, E], error)

// Options fixes durable runner policy. StatePath is exclusively owned and
// locked until Runner.Close succeeds.
type Options[Q, R, C, E any] struct {
	Stream          string
	Contract        Contract[Q, R, C, E]
	Initial         C
	StatePath       string
	MaxStateBytes   int64
	ShutdownTimeout time.Duration
	Deliver         func(context.Context, Delivery[E]) error
}

// DeliveryID is a stable idempotency key for one ordered committed effect.
// Its representation is opaque but comparable and text-marshallable.
type DeliveryID struct {
	stream   contentDigest
	sequence uint64
	ordinal  uint32
}

func deliveryIDFromOutbox(id outboxID) DeliveryID {
	return DeliveryID{stream: id.Stream.digest, sequence: id.Sequence, ordinal: id.Ordinal}
}

// String returns the stable text form of the delivery ID.
func (id DeliveryID) String() string {
	return hex.EncodeToString(id.stream[:]) + ":" + strconv.FormatUint(id.sequence, 10) + ":" + strconv.FormatUint(uint64(id.ordinal), 10)
}

// MarshalText implements encoding.TextMarshaler.
func (id DeliveryID) MarshalText() ([]byte, error) {
	if id.stream == (contentDigest{}) || id.sequence == 0 || id.ordinal == 0 {
		return nil, fmt.Errorf("prepared worker: invalid delivery ID")
	}
	return []byte(id.String()), nil
}

// Delivery is one typed effect with its stable idempotency key.
type Delivery[E any] struct {
	ID     DeliveryID
	Effect E
}

// OpenEmbedded restores and activates one in-process static-AOT handler. The
// returned capability never exposes reload operations.
func OpenEmbedded[Q, R, C, E any](
	ctx context.Context,
	options Options[Q, R, C, E],
	factory HandlerFactory[Q, R, C, E],
) (Runner[Q, R], error) {
	if factory == nil {
		return nil, fmt.Errorf("prepared worker: embedded handler factory is required")
	}
	config, initial, journal, err := openRunnerConfig(options)
	if err != nil {
		return nil, err
	}
	config.Preparer = newMemoryPreparer(
		config.RequestCodec,
		config.ResultCodec,
		config.CheckpointCodec,
		config.OutboxCodec,
		func(ctx context.Context, position Position, checkpoint C) (Handler[Q, R, C, E], error) {
			return factory(ctx, Restore[C]{Position: position, Checkpoint: checkpoint})
		},
	)
	contractID, err := config.Contract.Identity(config.Limits)
	if err != nil {
		_ = journal.Close()
		return nil, err
	}
	embeddedID, err := identityFromDigest(hashContent(append([]byte("ember-embedded-build-v1"), contractID.digest[:]...)))
	if err != nil {
		_ = journal.Close()
		return nil, err
	}
	deployment, err := openDeployment(ctx, config, initial, journal, artifactFromIdentity(embeddedID))
	if err != nil {
		return nil, err
	}
	return &runnerCapability[Q, R, C, E]{deployment: deployment}, nil
}

// OpenDevelopment restores and activates initial in a supervised process. It
// returns distinct steady-state and reload capabilities backed by one owner.
func OpenDevelopment[Q, R, C, E any](
	ctx context.Context,
	options Options[Q, R, C, E],
	initialBuild Build,
) (Runner[Q, R], Reload, error) {
	if !initialBuild.valid() || initialBuild.artifact.process == nil {
		return nil, nil, fmt.Errorf("prepared worker: development build has no process launch authority")
	}
	config, initial, journal, err := openRunnerConfig(options)
	if err != nil {
		return nil, nil, err
	}
	config.Preparer = defaultProcessPreparer()
	deployment, err := openDeployment(ctx, config, initial, journal, initialBuild.artifact)
	if err != nil {
		return nil, nil, err
	}
	return &runnerCapability[Q, R, C, E]{deployment: deployment},
		&reloadCapability[Q, R, C, E]{deployment: deployment}, nil
}

type deployment[Q, R, C, E any] struct {
	module       *transactionModule[Q, R, C, E]
	journal      *fileJournal
	closeTimeout time.Duration

	mu            sync.Mutex
	journalClosed bool
}

type runnerCapability[Q, R, C, E any] struct {
	deployment *deployment[Q, R, C, E]
}

func (runner *runnerCapability[Q, R, C, E]) Apply(
	ctx context.Context,
	operation Operation[Q],
) (Completion[R], error) {
	if runner == nil || runner.deployment == nil {
		return Completion[R]{}, fail(FailureClosed, "apply", fmt.Errorf("nil runner"))
	}
	return runner.deployment.module.Apply(ctx, operation)
}

func (runner *runnerCapability[Q, R, C, E]) Resolve(
	ctx context.Context,
	operation Operation[Q],
) (Resolution[R], error) {
	if runner == nil || runner.deployment == nil {
		return Resolution[R]{Status: ResolveUnresolved}, fail(FailureClosed, "resolve", fmt.Errorf("nil runner"))
	}
	return runner.deployment.module.Resolve(ctx, operation)
}

func (runner *runnerCapability[Q, R, C, E]) Close(ctx context.Context) error {
	if runner == nil || runner.deployment == nil {
		return nil
	}
	return runner.deployment.close(ctx)
}

type reloadCapability[Q, R, C, E any] struct {
	deployment *deployment[Q, R, C, E]
}

func (reload *reloadCapability[Q, R, C, E]) Prepare(
	ctx context.Context,
	build Build,
) (*Candidate, error) {
	if reload == nil || reload.deployment == nil {
		return nil, fail(FailureClosed, "prepare", fmt.Errorf("nil reload capability"))
	}
	if !build.valid() || build.artifact.process == nil {
		return nil, fail(FailureIdentity, "prepare", fmt.Errorf("build has no process launch authority"))
	}
	return reload.deployment.module.Prepare(ctx, build.artifact)
}

func openRunnerConfig[Q, R, C, E any](
	options Options[Q, R, C, E],
) (moduleConfig[Q, R, C, E], initialState[C], *fileJournal, error) {
	if options.Stream == "" {
		return moduleConfig[Q, R, C, E]{}, initialState[C]{}, nil, fmt.Errorf("prepared worker: stream is required")
	}
	if options.StatePath == "" || options.MaxStateBytes <= 0 {
		return moduleConfig[Q, R, C, E]{}, initialState[C]{}, nil, fmt.Errorf("prepared worker: durable state path and positive byte limit are required")
	}
	if options.Deliver == nil {
		return moduleConfig[Q, R, C, E]{}, initialState[C]{}, nil, fmt.Errorf("prepared worker: delivery function is required")
	}
	wire, limits, err := options.Contract.wire()
	if err != nil {
		return moduleConfig[Q, R, C, E]{}, initialState[C]{}, nil, err
	}
	timeout := options.ShutdownTimeout
	if timeout < 0 {
		return moduleConfig[Q, R, C, E]{}, initialState[C]{}, nil, fmt.Errorf("prepared worker: negative shutdown timeout")
	}
	if timeout == 0 {
		timeout = defaultCloseTimeout
	}
	journal, err := openDurableJournal(options.StatePath, options.MaxStateBytes)
	if err != nil {
		return moduleConfig[Q, R, C, E]{}, initialState[C]{}, nil, err
	}
	stream := identityFor("prepared-worker-stream-v1:" + options.Stream)
	publisher := &typedPublisher[E]{codec: options.Contract.Effect.Codec, deliver: options.Deliver, maxBytes: options.Contract.Effect.MaxBytes}
	return moduleConfig[Q, R, C, E]{
		Stream: stream, Contract: wire, Limits: limits,
		RequestCodec: options.Contract.Request.Codec, ResultCodec: options.Contract.Result.Codec,
		CheckpointCodec: options.Contract.Checkpoint.Codec, OutboxCodec: options.Contract.Effect.Codec,
		Journal: journal, Publisher: publisher, CloseTimeout: timeout,
	}, initialState[C]{Checkpoint: options.Initial}, journal, nil
}

func (contract Contract[Q, R, C, E]) wire() (wireContract, transactionLimits, error) {
	description, err := workercontract.Derive(contractSpecification(contract))
	if err != nil {
		return wireContract{}, transactionLimits{}, err
	}
	request, err := identityFromDigest(description.RequestSchema)
	if err != nil {
		return wireContract{}, transactionLimits{}, err
	}
	result, err := identityFromDigest(description.ResultSchema)
	if err != nil {
		return wireContract{}, transactionLimits{}, err
	}
	checkpoint, err := identityFromDigest(description.CheckpointSchema)
	if err != nil {
		return wireContract{}, transactionLimits{}, err
	}
	effect, err := identityFromDigest(description.EffectSchema)
	if err != nil {
		return wireContract{}, transactionLimits{}, err
	}
	wire := wireContract{
		RequestSchema: request, ResultSchema: result,
		CheckpointSchema: checkpoint, EffectSchema: effect,
		CodecVersion: contract.CodecVersion,
	}
	limits := transactionLimits{
		MaxRequestBytes: description.MaxRequestBytes, MaxResultBytes: description.MaxResultBytes,
		MaxCheckpointBytes: description.MaxCheckpointBytes, MaxOutboxItems: description.MaxEffects,
		MaxOutboxBytes: description.MaxEffectBytes, MaxCandidates: defaultCandidates, MaxRetired: defaultRetired,
	}
	identity, err := wire.Identity(limits)
	if err != nil {
		return wireContract{}, transactionLimits{}, err
	}
	if identity.digest != description.Contract {
		return wireContract{}, transactionLimits{}, fmt.Errorf("prepared worker: internal contract identity mismatch")
	}
	return wire, limits, nil
}

func contractSpecification[Q, R, C, E any](contract Contract[Q, R, C, E]) workercontract.Specification {
	return workercontract.Specification{
		CodecVersion: contract.CodecVersion,
		Request: workercontract.Schema{
			Name: contract.Request.Name, MaxBytes: contract.Request.MaxBytes, CodecPresent: contract.Request.Codec != nil,
		},
		Result: workercontract.Schema{
			Name: contract.Result.Name, MaxBytes: contract.Result.MaxBytes, CodecPresent: contract.Result.Codec != nil,
		},
		Checkpoint: workercontract.Schema{
			Name: contract.Checkpoint.Name, MaxBytes: contract.Checkpoint.MaxBytes, CodecPresent: contract.Checkpoint.Codec != nil,
		},
		Effect: workercontract.Schema{
			Name: contract.Effect.Name, MaxBytes: contract.Effect.MaxBytes, CodecPresent: contract.Effect.Codec != nil,
		},
		MaxEffects: contract.MaxEffects,
	}
}

func openDeployment[Q, R, C, E any](
	ctx context.Context,
	config moduleConfig[Q, R, C, E],
	initial initialState[C],
	journal *fileJournal,
	artifact preparedArtifact,
) (*deployment[Q, R, C, E], error) {
	module, err := newTransactionModule(config, initial)
	if err != nil {
		_ = journal.Close()
		return nil, err
	}
	candidate, err := module.Prepare(ctx, artifact)
	if err == nil {
		err = candidate.Activate()
	}
	if err != nil {
		closeCtx, cancel := boundedContext(context.Background(), config.CloseTimeout)
		defer cancel()
		cleanupErr := module.Close(closeCtx)
		journalErr := journal.Close()
		return nil, errors.Join(err, cleanupErr, journalErr)
	}
	return &deployment[Q, R, C, E]{
		module: module, journal: journal, closeTimeout: config.CloseTimeout,
	}, nil
}

func (deployment *deployment[Q, R, C, E]) close(ctx context.Context) error {
	deployment.mu.Lock()
	defer deployment.mu.Unlock()
	bounded, cancel := boundedContext(ctx, deployment.closeTimeout)
	defer cancel()
	if err := deployment.module.Close(bounded); err != nil {
		return err
	}
	if deployment.journalClosed {
		return nil
	}
	if err := deployment.journal.Close(); err != nil {
		return err
	}
	deployment.journalClosed = true
	return nil
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	parent = normalizeContext(parent)
	if timeout <= 0 {
		timeout = defaultCloseTimeout
	}
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

type typedPublisher[E any] struct {
	codec    Codec[E]
	deliver  func(context.Context, Delivery[E]) error
	maxBytes int
}

func (publisher *typedPublisher[E]) Publish(ctx context.Context, record outboxRecord) error {
	effect, err := decodeCanonical(publisher.codec, record.Payload, publisher.maxBytes, "delivery effect")
	if err != nil {
		return err
	}
	return publisher.deliver(ctx, Delivery[E]{ID: deliveryIDFromOutbox(record.ID), Effect: effect})
}
