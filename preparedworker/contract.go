// Package preparedworker coordinates typed application transactions across
// replaceable prepared generations. It deliberately knows nothing about Ember
// runtime values, host callbacks, or application-specific record layouts.
package preparedworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
)

// contentDigest is a SHA-256 content digest.
type contentDigest = workerartifact.Digest

// hashContent returns the SHA-256 digest of data.
func hashContent(data []byte) contentDigest {
	return workerartifact.Hash(data)
}

// contentIdentity is an opaque content identity. It is data, not authority; only
// owner-qualified Candidate and Generation handles can select a generation.
type contentIdentity struct {
	digest contentDigest
}

// identityFor derives a deterministic identity from a canonical label.
func identityFor(label string) contentIdentity {
	return contentIdentity{digest: hashContent([]byte(label))}
}

// identityFromDigest constructs a nonzero content identity.
func identityFromDigest(digest contentDigest) (contentIdentity, error) {
	if digest == (contentDigest{}) {
		return contentIdentity{}, fmt.Errorf("prepared worker: zero identity")
	}
	return contentIdentity{digest: digest}, nil
}

// Digest returns the identity's content digest.
func (identity contentIdentity) Digest() contentDigest {
	return identity.digest
}

func (identity contentIdentity) String() string {
	return identity.digest.String()
}

func (identity contentIdentity) valid() bool {
	return identity.digest != (contentDigest{})
}

// Position identifies the durable operation head.
type Position struct {
	Sequence uint64
	Revision uint64
}

// transactionLimits are fixed before a generation is prepared and are included in its
// READY identity.
type transactionLimits struct {
	MaxRequestBytes    int
	MaxResultBytes     int
	MaxCheckpointBytes int
	MaxOutboxItems     int
	MaxOutboxBytes     int
	MaxCandidates      int
	MaxRetired         int
}

// wireContract identifies the four closed application records and their shared
// codec revision. Its derived identity also binds every record-shape limit;
// candidate and retirement counts remain parent lifecycle policy.
type wireContract struct {
	RequestSchema    contentIdentity
	ResultSchema     contentIdentity
	CheckpointSchema contentIdentity
	EffectSchema     contentIdentity
	CodecVersion     uint32
}

// Identity returns the canonical contract-and-limits identity attested by
// every embedded or process backend.
func (contract wireContract) Identity(limits transactionLimits) (contentIdentity, error) {
	if !contract.RequestSchema.valid() || !contract.ResultSchema.valid() ||
		!contract.CheckpointSchema.valid() || !contract.EffectSchema.valid() ||
		contract.CodecVersion == 0 {
		return contentIdentity{}, fmt.Errorf("prepared worker: incomplete contract identity")
	}
	if err := limits.validate(); err != nil {
		return contentIdentity{}, err
	}
	var encoded bytes.Buffer
	_, _ = encoded.WriteString("ember-prepared-worker-contract-v1")
	for _, identity := range []contentIdentity{
		contract.RequestSchema,
		contract.ResultSchema,
		contract.CheckpointSchema,
		contract.EffectSchema,
	} {
		_, _ = encoded.Write(identity.digest[:])
	}
	_ = binary.Write(&encoded, binary.BigEndian, contract.CodecVersion)
	for _, value := range []int{
		limits.MaxRequestBytes,
		limits.MaxResultBytes,
		limits.MaxCheckpointBytes,
		limits.MaxOutboxItems,
		limits.MaxOutboxBytes,
	} {
		_ = binary.Write(&encoded, binary.BigEndian, uint64(value))
	}
	return identityFromDigest(hashContent(encoded.Bytes()))
}

func (limits transactionLimits) validate() error {
	const maximumBytes = 1 << 30
	byteLimits := []struct {
		name  string
		value int
	}{
		{name: "request bytes", value: limits.MaxRequestBytes},
		{name: "result bytes", value: limits.MaxResultBytes},
		{name: "checkpoint bytes", value: limits.MaxCheckpointBytes},
		{name: "outbox bytes", value: limits.MaxOutboxBytes},
	}
	for _, limit := range byteLimits {
		if limit.value <= 0 || limit.value > maximumBytes {
			return fmt.Errorf("prepared worker: %s limit %d is out of bounds", limit.name, limit.value)
		}
	}
	countLimits := []struct {
		name  string
		value int
	}{
		{name: "outbox items", value: limits.MaxOutboxItems},
		{name: "candidates", value: limits.MaxCandidates},
		{name: "retired", value: limits.MaxRetired},
	}
	for _, limit := range countLimits {
		if limit.value <= 0 || limit.value > 1<<20 {
			return fmt.Errorf("prepared worker: %s limit %d is out of bounds", limit.name, limit.value)
		}
	}
	return nil
}

// Codec converts one closed application record to and from canonical bytes.
// Encode(Decode(Encode(value))) must reproduce the first encoding exactly.
type Codec[T any] interface {
	Encode(T) ([]byte, error)
	Decode([]byte) (T, error)
}

// Operation is the common ordering envelope around a typed application
// request.
type Operation[T any] struct {
	Sequence     uint64
	BaseRevision uint64
	Request      T
}

// Completion is the common durable position around a typed application
// result.
type Completion[T any] struct {
	Position Position
	Result   T
}

// ResolveStatus is the durable disposition of one operation identity.
type ResolveStatus uint8

const (
	ResolveUnresolved ResolveStatus = iota
	ResolveCommitted
	ResolveNotCommitted
)

// Resolution reports whether an operation is durably committed. Completion
// is populated only for ResolveCommitted.
type Resolution[T any] struct {
	Status     ResolveStatus
	Completion Completion[T]
}

// Decision is the complete typed outcome of one admitted operation. The
// result, detached checkpoint, and ordered effects cross the backend seam as
// one unit. A non-nil checkpoint is the sole quiescence evidence.
type Decision[R, C, E any] struct {
	Result     R
	Checkpoint *C
	Effects    []E
}

// initialState supplies the quiescent position from which an empty journal starts.
type initialState[C any] struct {
	Position   Position
	Checkpoint C
}

// preparedArtifact is an opaque immutable prepared-build authority. Embedded adapters
// need only its identity; process adapters additionally retain verified launch
// metadata behind the same value.
type preparedArtifact struct {
	identity contentIdentity
	process  *processArtifact
}

// artifactFromIdentity constructs an embedded/test artifact authority. Process
// artifacts must be opened through the verified artifact loader.
func artifactFromIdentity(identity contentIdentity) preparedArtifact {
	return preparedArtifact{identity: identity}
}

// Identity returns the artifact's content identity.
func (artifact preparedArtifact) Identity() contentIdentity {
	return artifact.identity
}

func (artifact preparedArtifact) valid() bool {
	return artifact.identity.valid()
}

// backendSpec is the complete identity and restore point presented to a
// generation backend during preparation.
type backendSpec struct {
	Artifact         contentIdentity
	Contract         contentIdentity
	Stream           contentIdentity
	CheckpointSchema contentIdentity
	Position         Position
	CheckpointDigest contentDigest
	Checkpoint       []byte
	Limits           transactionLimits
	artifact         preparedArtifact
}

// backendReady is the identity a prepared backend must echo before it may become a
// candidate.
type backendReady struct {
	Artifact         contentIdentity
	Contract         contentIdentity
	Stream           contentIdentity
	CheckpointSchema contentIdentity
	Position         Position
	CheckpointDigest contentDigest
	Limits           transactionLimits
}

func readyFor(spec backendSpec) backendReady {
	return backendReady{
		Artifact:         spec.Artifact,
		Contract:         spec.Contract,
		Stream:           spec.Stream,
		CheckpointSchema: spec.CheckpointSchema,
		Position:         spec.Position,
		CheckpointDigest: spec.CheckpointDigest,
		Limits:           spec.Limits,
	}
}

// encodedOperation is the closed byte-oriented backend seam. Typed application
// requests remain outside this boundary.
type encodedOperation struct {
	Sequence     uint64
	BaseRevision uint64
	Request      []byte
}

// wireDecision is the closed byte-oriented backend result.
type wireDecision struct {
	Position      Position
	Result        []byte
	Checkpoint    []byte
	HasCheckpoint bool
	Effects       [][]byte
	Quiescent     bool
}

// generationBackend owns one complete prepared generation.
type generationBackend interface {
	Apply(context.Context, encodedOperation) (wireDecision, error)
	Close(context.Context) error
}

// backendPreparer restores and verifies an inert backend. It must not mutate the
// module's active route.
type backendPreparer interface {
	Prepare(context.Context, backendSpec) (generationBackend, backendReady, error)
}

// Handler is the typed in-memory equivalent of a process backend.
type Handler[Q, R, C, E any] interface {
	Apply(context.Context, Operation[Q]) (Decision[R, C, E], error)
	Close(context.Context) error
}

// moduleConfig injects application record policy and effectful persistence edges.
type moduleConfig[Q, R, C, E any] struct {
	Stream          contentIdentity
	Contract        wireContract
	Limits          transactionLimits
	RequestCodec    Codec[Q]
	ResultCodec     Codec[R]
	CheckpointCodec Codec[C]
	OutboxCodec     Codec[E]
	Preparer        backendPreparer
	Journal         transactionJournal
	Publisher       effectPublisher
	CloseTimeout    time.Duration
}

// FailureKind is a stable branchable transaction or lifecycle outcome.
type FailureKind string

const (
	FailureGuest         FailureKind = "guest"
	FailureIdentity      FailureKind = "identity"
	FailureProtocol      FailureKind = "protocol"
	FailureCorrupt       FailureKind = "corrupt"
	FailureStale         FailureKind = "stale"
	FailureBehind        FailureKind = "behind"
	FailureLost          FailureKind = "lost"
	FailureConflict      FailureKind = "conflict"
	FailureGap           FailureKind = "gap"
	FailureUnknownCommit FailureKind = "unknown-commit"
	FailureDelivery      FailureKind = "delivery"
	FailureBusy          FailureKind = "busy"
	FailureClosed        FailureKind = "closed"
	FailureLimit         FailureKind = "limit"
	FailureNotQuiescent  FailureKind = "not-quiescent"
)

// Failure classifies an operation without discarding its underlying cause.
type Failure struct {
	Kind      FailureKind
	Operation string
	Err       error
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "prepared worker: failure"
	}
	if failure.Err == nil {
		return fmt.Sprintf("prepared worker %s: %s", failure.Operation, failure.Kind)
	}
	return fmt.Sprintf("prepared worker %s: %s: %v", failure.Operation, failure.Kind, failure.Err)
}

func (failure *Failure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

// IsFailure reports whether err contains the requested failure class.
func IsFailure(err error, kind FailureKind) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Kind == kind
}

func fail(kind FailureKind, operation string, err error) error {
	return &Failure{Kind: kind, Operation: operation, Err: err}
}
