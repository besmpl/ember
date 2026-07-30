// Package rubyprojectionproof is a private application-architecture proof.
// It translates two concrete generated Ruby generations through one bounded,
// canonical application record without sharing a runtime, graph ABI, or code.
package rubyprojectionproof

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	rubyv2 "github.com/besmpl/ember/internal/rubyprojectionproof/generated/rubyv2"
	rubyv1 "github.com/besmpl/ember/internal/rubyproof/generated"
)

const (
	CheckpointVersion uint32 = 1
	MaximumRoots             = 3
	MaximumNodes             = 4
	MaximumEdges             = 6
	checkpointBytes          = 52

	canonicalRoots     uint8 = 3
	canonicalNodes     uint8 = 3
	canonicalEdges     uint8 = 3
	canonicalBehaviors uint8 = 1
	maximumScalar            = math.MaxInt64 - 9
)

var (
	ErrCheckpoint = errors.New("Ruby projection proof: invalid checkpoint")
	ErrLength     = errors.New("Ruby projection proof: invalid checkpoint length")
	ErrVersion    = errors.New("Ruby projection proof: unsupported checkpoint version")
	ErrLimit      = errors.New("Ruby projection proof: checkpoint limit exceeded")
	ErrCanonical  = errors.New("Ruby projection proof: non-canonical checkpoint")
	ErrTopology   = errors.New("Ruby projection proof: invalid checkpoint topology")
	ErrTag        = errors.New("Ruby projection proof: unknown checkpoint tag")
	ErrBehavior   = errors.New("Ruby projection proof: behavior is not checkpointable")
	ErrNilContext = errors.New("Ruby projection proof: nil context")
)

type NodeID uint8
type NodeKind uint8
type EdgeKind uint8
type BehaviorKind uint8

const (
	NodeCounter NodeKind = iota + 1
)

const (
	EdgeSelf EdgeKind = iota + 1
	EdgeNext
	EdgeBack
)

const (
	BehaviorCapturedSelf BehaviorKind = 7
	BehaviorLiveReturn   BehaviorKind = 8
)

type NodeRecord struct {
	ID     NodeID
	Kind   NodeKind
	Scalar int64
}

type EdgeRecord struct {
	From NodeID
	Kind EdgeKind
	To   NodeID
}

type BehaviorRecord struct {
	Kind BehaviorKind
	Self NodeID
}

// Checkpoint is the application's complete admitted durable projection. Its
// NodeIDs are application identities only. Ruby object IDs, pointers, method
// epochs, class tables, frames, return targets, and executable closures are
// deliberately absent.
type Checkpoint struct {
	Version                                        uint32
	RootCount, NodeCount, EdgeCount, BehaviorCount uint8
	Roots                                          [MaximumRoots]NodeID
	Nodes                                          [MaximumNodes]NodeRecord
	Edges                                          [MaximumEdges]EdgeRecord
	Behavior                                       BehaviorRecord
}

func InitialCheckpoint() Checkpoint {
	return Checkpoint{
		Version:       CheckpointVersion,
		RootCount:     canonicalRoots,
		NodeCount:     canonicalNodes,
		EdgeCount:     canonicalEdges,
		BehaviorCount: canonicalBehaviors,
		Roots:         [MaximumRoots]NodeID{1, 1, 2},
		Nodes: [MaximumNodes]NodeRecord{
			{ID: 1, Kind: NodeCounter, Scalar: 40},
			{ID: 2, Kind: NodeCounter, Scalar: 40},
			{ID: 3, Kind: NodeCounter, Scalar: 7},
		},
		Edges: [MaximumEdges]EdgeRecord{
			{From: 1, Kind: EdgeSelf, To: 1},
			{From: 2, Kind: EdgeNext, To: 3},
			{From: 3, Kind: EdgeBack, To: 1},
		},
		Behavior: BehaviorRecord{Kind: BehaviorCapturedSelf, Self: 1},
	}
}

func Validate(checkpoint Checkpoint) error {
	if checkpoint.Version != CheckpointVersion {
		return checkpointError(ErrVersion, "version %d", checkpoint.Version)
	}
	if err := validateCounts(checkpoint); err != nil {
		return err
	}
	for index := 0; index < int(canonicalNodes); index++ {
		node := checkpoint.Nodes[index]
		if node.ID != NodeID(index+1) {
			return checkpointError(ErrCanonical, "node %d has ID %d", index, node.ID)
		}
		if node.Kind != NodeCounter {
			return checkpointError(ErrTag, "node %d has kind %d", node.ID, node.Kind)
		}
		if node.Scalar < 0 || node.Scalar > maximumScalar {
			return checkpointError(ErrLimit, "node %d scalar %d", node.ID, node.Scalar)
		}
	}
	for index := int(canonicalNodes); index < len(checkpoint.Nodes); index++ {
		if checkpoint.Nodes[index] != (NodeRecord{}) {
			return checkpointError(ErrCanonical, "unused node slot %d is nonzero", index)
		}
	}
	for index := 0; index < int(canonicalEdges); index++ {
		edge := checkpoint.Edges[index]
		if !validNodeID(edge.From) || !validNodeID(edge.To) {
			return checkpointError(ErrTopology, "edge %d references %d -> %d", index, edge.From, edge.To)
		}
		switch edge.Kind {
		case EdgeSelf, EdgeNext, EdgeBack:
		default:
			return checkpointError(ErrTag, "edge %d has kind %d", index, edge.Kind)
		}
		if index > 0 && !edgeLess(checkpoint.Edges[index-1], edge) {
			return checkpointError(ErrCanonical, "edge %d is not strictly ordered", index)
		}
	}
	for index := int(canonicalEdges); index < len(checkpoint.Edges); index++ {
		if checkpoint.Edges[index] != (EdgeRecord{}) {
			return checkpointError(ErrCanonical, "unused edge slot %d is nonzero", index)
		}
	}
	switch checkpoint.Behavior.Kind {
	case BehaviorCapturedSelf:
	case BehaviorLiveReturn:
		return checkpointError(ErrBehavior, "live targeted return")
	default:
		return checkpointError(ErrTag, "behavior kind %d", checkpoint.Behavior.Kind)
	}
	if !validNodeID(checkpoint.Behavior.Self) {
		return checkpointError(ErrTopology, "behavior self %d", checkpoint.Behavior.Self)
	}

	want := InitialCheckpoint()
	if checkpoint.Roots != want.Roots {
		return checkpointError(ErrTopology, "roots %v", checkpoint.Roots)
	}
	if checkpoint.Nodes[0].Scalar != checkpoint.Nodes[1].Scalar {
		return checkpointError(ErrTopology, "nodes 1 and 2 are not equal-valued")
	}
	for index := 0; index < int(canonicalEdges); index++ {
		if checkpoint.Edges[index] != want.Edges[index] {
			return checkpointError(ErrTopology, "edge %d is %#v", index, checkpoint.Edges[index])
		}
	}
	if checkpoint.Behavior != want.Behavior {
		return checkpointError(ErrTopology, "behavior is %#v", checkpoint.Behavior)
	}
	return nil
}

func validateCounts(checkpoint Checkpoint) error {
	if checkpoint.RootCount > MaximumRoots || checkpoint.NodeCount > MaximumNodes ||
		checkpoint.EdgeCount > MaximumEdges || checkpoint.BehaviorCount > 1 {
		return checkpointError(ErrLimit, "counts roots=%d nodes=%d edges=%d behaviors=%d",
			checkpoint.RootCount, checkpoint.NodeCount, checkpoint.EdgeCount, checkpoint.BehaviorCount)
	}
	if checkpoint.RootCount != canonicalRoots || checkpoint.NodeCount != canonicalNodes ||
		checkpoint.EdgeCount != canonicalEdges || checkpoint.BehaviorCount != canonicalBehaviors {
		return checkpointError(ErrCanonical, "counts roots=%d nodes=%d edges=%d behaviors=%d",
			checkpoint.RootCount, checkpoint.NodeCount, checkpoint.EdgeCount, checkpoint.BehaviorCount)
	}
	return nil
}

func validNodeID(id NodeID) bool { return id >= 1 && id <= NodeID(canonicalNodes) }

func edgeLess(left, right EdgeRecord) bool {
	if left.From != right.From {
		return left.From < right.From
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.To < right.To
}

func checkpointError(category error, format string, arguments ...any) error {
	return fmt.Errorf("%w: %w: %s", ErrCheckpoint, category, fmt.Sprintf(format, arguments...))
}

// Encode emits the one exact 52-byte little-endian application record. The
// complete value is validated before any bytes are returned.
func Encode(checkpoint Checkpoint) ([]byte, error) {
	if err := Validate(checkpoint); err != nil {
		return nil, err
	}
	data := make([]byte, checkpointBytes)
	binary.LittleEndian.PutUint32(data[0:4], checkpoint.Version)
	data[4], data[5], data[6], data[7] = checkpoint.RootCount, checkpoint.NodeCount, checkpoint.EdgeCount, checkpoint.BehaviorCount
	for index := range checkpoint.Roots {
		data[8+index] = byte(checkpoint.Roots[index])
	}
	offset := 11
	for index := 0; index < int(canonicalNodes); index++ {
		node := checkpoint.Nodes[index]
		data[offset], data[offset+1] = byte(node.ID), byte(node.Kind)
		binary.LittleEndian.PutUint64(data[offset+2:offset+10], uint64(node.Scalar))
		offset += 10
	}
	for index := 0; index < int(canonicalEdges); index++ {
		edge := checkpoint.Edges[index]
		data[offset], data[offset+1], data[offset+2] = byte(edge.From), byte(edge.Kind), byte(edge.To)
		offset += 3
	}
	data[offset], data[offset+1] = byte(checkpoint.Behavior.Kind), byte(checkpoint.Behavior.Self)
	return data, nil
}

// Decode validates fixed length and bounded header counts before parsing the
// fixed bodies. It returns only a fully canonical application value.
func Decode(data []byte) (Checkpoint, error) {
	if len(data) != checkpointBytes {
		return Checkpoint{}, checkpointError(ErrLength, "bytes %d, want %d", len(data), checkpointBytes)
	}
	checkpoint := Checkpoint{
		Version:       binary.LittleEndian.Uint32(data[0:4]),
		RootCount:     data[4],
		NodeCount:     data[5],
		EdgeCount:     data[6],
		BehaviorCount: data[7],
	}
	if checkpoint.RootCount > MaximumRoots || checkpoint.NodeCount > MaximumNodes ||
		checkpoint.EdgeCount > MaximumEdges || checkpoint.BehaviorCount > 1 {
		return Checkpoint{}, checkpointError(ErrLimit, "encoded counts roots=%d nodes=%d edges=%d behaviors=%d",
			checkpoint.RootCount, checkpoint.NodeCount, checkpoint.EdgeCount, checkpoint.BehaviorCount)
	}
	if err := validateCounts(checkpoint); err != nil {
		return Checkpoint{}, err
	}
	for index := range checkpoint.Roots {
		checkpoint.Roots[index] = NodeID(data[8+index])
	}
	offset := 11
	for index := 0; index < int(canonicalNodes); index++ {
		checkpoint.Nodes[index] = NodeRecord{
			ID:     NodeID(data[offset]),
			Kind:   NodeKind(data[offset+1]),
			Scalar: int64(binary.LittleEndian.Uint64(data[offset+2 : offset+10])),
		}
		offset += 10
	}
	for index := 0; index < int(canonicalEdges); index++ {
		checkpoint.Edges[index] = EdgeRecord{From: NodeID(data[offset]), Kind: EdgeKind(data[offset+1]), To: NodeID(data[offset+2])}
		offset += 3
	}
	checkpoint.Behavior = BehaviorRecord{Kind: BehaviorKind(data[offset]), Self: NodeID(data[offset+1])}
	if err := Validate(checkpoint); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

// RestoreV1 is the only application initialization path. Initial values come
// from the application checkpoint rather than a generated-package default.
func RestoreV1(ctx context.Context, checkpoint Checkpoint, limits rubyv1.Limits) (*rubyv1.ProjectionEngine, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scalars, err := scalarsForV1(checkpoint)
	if err != nil {
		return nil, err
	}
	engine, err := rubyv1.NewProjectionEngineFromScalars(ctx, scalars, limits)
	if err != nil {
		if engine != nil {
			return nil, errors.Join(err, engine.Close())
		}
		return nil, err
	}
	return engine, nil
}

func scalarsForV1(checkpoint Checkpoint) (rubyv1.ProjectionScalars, error) {
	if err := Validate(checkpoint); err != nil {
		return rubyv1.ProjectionScalars{}, err
	}
	return rubyv1.ProjectionScalars{
		Root:  checkpoint.Nodes[0].Scalar,
		Other: checkpoint.Nodes[1].Scalar,
		Tail:  checkpoint.Nodes[2].Scalar,
	}, nil
}

// SnapshotV1 translates the concrete v1 generation's scalar projection into
// the application's canonical topology. Generated code never owns or accepts
// application IDs, edges, or behavior tags.
func SnapshotV1(ctx context.Context, engine *rubyv1.ProjectionEngine) (Checkpoint, error) {
	if ctx == nil {
		return Checkpoint{}, ErrNilContext
	}
	scalars, err := engine.Snapshot(ctx)
	if err != nil {
		return Checkpoint{}, err
	}
	return checkpointFromV1(scalars)
}

func checkpointFromV1(scalars rubyv1.ProjectionScalars) (Checkpoint, error) {
	checkpoint := InitialCheckpoint()
	checkpoint.Nodes[0].Scalar = scalars.Root
	checkpoint.Nodes[1].Scalar = scalars.Other
	checkpoint.Nodes[2].Scalar = scalars.Tail
	if err := Validate(checkpoint); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

// SnapshotV2 translates a restored v2 owner back to the same application
// record without sharing v1 state types or executable behavior.
func SnapshotV2(ctx context.Context, engine *rubyv2.ProjectionEngine) (Checkpoint, error) {
	if ctx == nil {
		return Checkpoint{}, ErrNilContext
	}
	scalars, err := engine.Snapshot(ctx)
	if err != nil {
		return Checkpoint{}, err
	}
	return checkpointFromV2(scalars)
}

func checkpointFromV2(scalars rubyv2.ProjectionScalars) (Checkpoint, error) {
	checkpoint := InitialCheckpoint()
	checkpoint.Nodes[0].Scalar = scalars.Root
	checkpoint.Nodes[1].Scalar = scalars.Other
	checkpoint.Nodes[2].Scalar = scalars.Tail
	if err := Validate(checkpoint); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

func scalarsForV2(checkpoint Checkpoint) (rubyv2.ProjectionScalars, error) {
	if err := Validate(checkpoint); err != nil {
		return rubyv2.ProjectionScalars{}, err
	}
	return rubyv2.ProjectionScalars{
		Root:  checkpoint.Nodes[0].Scalar,
		Other: checkpoint.Nodes[1].Scalar,
		Tail:  checkpoint.Nodes[2].Scalar,
	}, nil
}

type restoreStage uint8

const (
	stageValidated restoreStage = iota + 1
	stageMapped
	stageOwnerConstructed
	stageBeforePublish
)

type restoreObserver func(restoreStage, *rubyv2.ProjectionEngine) error

func RestoreV2(ctx context.Context, checkpoint Checkpoint, limits rubyv2.Limits) (*rubyv2.ProjectionEngine, error) {
	return restoreV2(ctx, checkpoint, limits, nil)
}

func restoreV2(ctx context.Context, checkpoint Checkpoint, limits rubyv2.Limits, observer restoreObserver) (*rubyv2.ProjectionEngine, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := Validate(checkpoint); err != nil {
		return nil, err
	}
	if err := observeRestore(ctx, observer, stageValidated, nil); err != nil {
		return nil, err
	}
	scalars, err := scalarsForV2(checkpoint)
	if err != nil {
		return nil, err
	}
	if err := observeRestore(ctx, observer, stageMapped, nil); err != nil {
		return nil, err
	}
	engine, err := rubyv2.NewProjectionEngineFromScalars(ctx, scalars, limits)
	if err != nil {
		if engine != nil {
			return nil, errors.Join(err, engine.Close())
		}
		return nil, err
	}
	if err := observeRestore(ctx, observer, stageOwnerConstructed, engine); err != nil {
		return nil, errors.Join(err, engine.Close())
	}
	if err := observeRestore(ctx, observer, stageBeforePublish, engine); err != nil {
		return nil, errors.Join(err, engine.Close())
	}
	return engine, nil
}

func observeRestore(ctx context.Context, observer restoreObserver, stage restoreStage, engine *rubyv2.ProjectionEngine) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if observer != nil {
		if err := observer(stage, engine); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Migrate snapshots a still-live v1 owner, closes it, and only then constructs
// a fresh v2 owner. A snapshot or busy-close failure leaves v1 authoritative;
// a later restore failure returns the durable checkpoint but no v2 owner.
func Migrate(ctx context.Context, source *rubyv1.ProjectionEngine, limits rubyv2.Limits) (*rubyv2.ProjectionEngine, Checkpoint, error) {
	checkpoint, err := SnapshotV1(ctx, source)
	if err != nil {
		return nil, Checkpoint{}, err
	}
	if err := source.Close(); err != nil {
		return nil, Checkpoint{}, err
	}
	engine, err := RestoreV2(ctx, checkpoint, limits)
	return engine, checkpoint, err
}
