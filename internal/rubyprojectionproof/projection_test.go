package rubyprojectionproof

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	rubyv2 "github.com/besmpl/ember/internal/rubyprojectionproof/generated/rubyv2"
	rubyv1 "github.com/besmpl/ember/internal/rubyproof/generated"
)

func TestMigrationClosesV1AndRestoresTopologyWithV2Code(t *testing.T) {
	ctx := context.Background()
	first, err := RestoreV1(ctx, InitialCheckpoint(), rubyv1.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	firstCheckpoint, err := SnapshotV1(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := first.Call(ctx); err != nil || value != 41 {
		t.Fatalf("v1 call = %d, %v; want 41", value, err)
	}

	second, checkpoint, err := Migrate(ctx, first, rubyv2.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close v2: %v", err)
		}
	})
	if checkpoint != InitialCheckpoint() {
		t.Fatalf("checkpoint = %#v, want %#v", checkpoint, InitialCheckpoint())
	}
	if checkpoint != firstCheckpoint {
		t.Fatalf("migration checkpoint = %#v, want pre-close snapshot %#v", checkpoint, firstCheckpoint)
	}
	if _, err := first.Call(ctx); !errors.Is(err, rubyv1.ErrClosed) {
		t.Fatalf("v1 call after migration = %v, want closed", err)
	}
	if value, err := second.Call(ctx); err != nil || value != 49 {
		t.Fatalf("v2 call = %d, %v; want 49 from v2 checked code", value, err)
	}
	roundTrip, err := SnapshotV2(ctx, second)
	if err != nil || roundTrip != checkpoint {
		t.Fatalf("v2 checkpoint = %#v, %v; want %#v", roundTrip, err, checkpoint)
	}
	if roundTrip.Roots[0] != roundTrip.Roots[1] || roundTrip.Roots[0] == roundTrip.Roots[2] ||
		roundTrip.Nodes[0].Scalar != roundTrip.Nodes[1].Scalar || roundTrip.Edges[0] != (EdgeRecord{From: 1, Kind: EdgeSelf, To: 1}) {
		t.Fatalf("restored application topology = %#v", roundTrip)
	}
}

func TestRestoreV1ValidatesApplicationStateBeforePublication(t *testing.T) {
	invalid := InitialCheckpoint()
	invalid.Nodes[1].Scalar++
	if engine, err := RestoreV1(context.Background(), invalid, rubyv1.ProjectionLimits()); engine != nil || !errors.Is(err, ErrTopology) {
		t.Fatalf("invalid v1 restore = %#v, %v; want nil topology error", engine, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if engine, err := RestoreV1(canceled, InitialCheckpoint(), rubyv1.ProjectionLimits()); engine != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled v1 restore = %#v, %v; want nil canceled error", engine, err)
	}
}

func TestV2GeneratedRestoreRejectsSourceDerivedOverflowAtDirectBoundary(t *testing.T) {
	scalars := rubyv2.ProjectionScalars{Root: 40, Other: 40, Tail: 7}
	scalars.Other++
	if engine, err := rubyv2.NewProjectionEngineFromScalars(context.Background(), scalars, rubyv2.ProjectionLimits()); engine != nil || !errors.Is(err, rubyv2.ErrStateLimit) {
		t.Fatalf("unequal restore = %#v, %v; want nil state-limit error", engine, err)
	}
	scalars.Root, scalars.Other = math.MaxInt64, math.MaxInt64
	engine, err := rubyv2.NewProjectionEngineFromScalars(context.Background(), scalars, rubyv2.ProjectionLimits())
	if engine != nil || !errors.Is(err, rubyv2.ErrStateLimit) {
		t.Fatalf("overflow restore = %#v, %v; want nil state-limit error", engine, err)
	}
	scalars.Root, scalars.Other = math.MaxInt64-9, math.MaxInt64-9
	engine, err = rubyv2.NewProjectionEngineFromScalars(context.Background(), scalars, rubyv2.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if value, err := engine.Call(context.Background()); value != math.MaxInt64 || err != nil {
		t.Fatalf("maximum safe v2 call = %d, %v", value, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationScalarPolicyStaysOutsideGeneratedOwner(t *testing.T) {
	tests := []struct {
		name    string
		scalars rubyv2.ProjectionScalars
	}{
		{"negative application value", rubyv2.ProjectionScalars{Root: -1, Other: -1, Tail: 7}},
		{"application limit", rubyv2.ProjectionScalars{Root: 40, Other: 40, Tail: math.MaxInt64}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := rubyv2.NewProjectionEngineFromScalars(context.Background(), test.scalars, rubyv2.ProjectionLimits())
			if err != nil {
				t.Fatalf("generation-local restore = %v", err)
			}
			scalars, err := engine.Snapshot(context.Background())
			if err != nil || scalars != test.scalars {
				t.Fatalf("generation-local snapshot = %#v, %v", scalars, err)
			}
			if checkpoint, err := checkpointFromV2(scalars); checkpoint != (Checkpoint{}) || !errors.Is(err, ErrLimit) {
				t.Fatalf("application mapping = %#v, %v; want zero checkpoint and application limit", checkpoint, err)
			}
			if err := engine.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckpointCodecIsFixedCanonicalAndFailClosed(t *testing.T) {
	checkpoint := InitialCheckpoint()
	data, err := Encode(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "01000000030303010101020101280000000000000002012800000000000000030107000000000000000101010202030303010701"
	if got := hex.EncodeToString(data); got != golden {
		t.Fatalf("encoded checkpoint = %s, want %s", got, golden)
	}
	decoded, err := Decode(data)
	if err != nil || decoded != checkpoint {
		t.Fatalf("decoded checkpoint = %#v, %v", decoded, err)
	}
	reencoded, err := Encode(decoded)
	if err != nil || !bytes.Equal(reencoded, data) {
		t.Fatalf("re-encoded checkpoint = %x, %v", reencoded, err)
	}
	for _, size := range []int{0, checkpointBytes - 1, checkpointBytes + 1} {
		if value, err := Decode(make([]byte, size)); value != (Checkpoint{}) || !errors.Is(err, ErrLength) || !errors.Is(err, ErrCheckpoint) {
			t.Fatalf("Decode(%d bytes) = %#v, %v", size, value, err)
		}
	}

	for index := range data {
		mutated := append([]byte(nil), data...)
		mutated[index] ^= 0x80
		value, err := Decode(mutated)
		if err != nil {
			if !errors.Is(err, ErrCheckpoint) {
				t.Fatalf("byte %d error = %v, want checkpoint category", index, err)
			}
			continue
		}
		canonical, err := Encode(value)
		if err != nil || !bytes.Equal(canonical, mutated) {
			t.Fatalf("accepted byte %d is not canonical: %#v, %v, %x", index, value, err, canonical)
		}
	}
}

func TestCheckpointValidationRejectsMalformedValuesByCategory(t *testing.T) {
	tests := []struct {
		name   string
		want   error
		mutate func(*Checkpoint)
	}{
		{"version", ErrVersion, func(value *Checkpoint) { value.Version++ }},
		{"root count limit", ErrLimit, func(value *Checkpoint) { value.RootCount = MaximumRoots + 1 }},
		{"root count schema", ErrCanonical, func(value *Checkpoint) { value.RootCount-- }},
		{"node count limit", ErrLimit, func(value *Checkpoint) { value.NodeCount = MaximumNodes + 1 }},
		{"node count schema", ErrCanonical, func(value *Checkpoint) { value.NodeCount-- }},
		{"edge count limit", ErrLimit, func(value *Checkpoint) { value.EdgeCount = MaximumEdges + 1 }},
		{"edge count schema", ErrCanonical, func(value *Checkpoint) { value.EdgeCount-- }},
		{"behavior count limit", ErrLimit, func(value *Checkpoint) { value.BehaviorCount = 2 }},
		{"behavior count schema", ErrCanonical, func(value *Checkpoint) { value.BehaviorCount = 0 }},
		{"zero node ID", ErrCanonical, func(value *Checkpoint) { value.Nodes[0].ID = 0 }},
		{"duplicate node ID", ErrCanonical, func(value *Checkpoint) { value.Nodes[1].ID = 1 }},
		{"swapped node IDs", ErrCanonical, func(value *Checkpoint) { value.Nodes[0].ID, value.Nodes[1].ID = 2, 1 }},
		{"node tag", ErrTag, func(value *Checkpoint) { value.Nodes[0].Kind = 99 }},
		{"negative scalar", ErrLimit, func(value *Checkpoint) { value.Nodes[0].Scalar = -1 }},
		{"overflow scalar", ErrLimit, func(value *Checkpoint) { value.Nodes[0].Scalar = math.MaxInt64 - 8 }},
		{"unused node", ErrCanonical, func(value *Checkpoint) { value.Nodes[3] = NodeRecord{ID: 4} }},
		{"broken alias", ErrTopology, func(value *Checkpoint) { value.Roots[1] = 2 }},
		{"missing distinct root", ErrTopology, func(value *Checkpoint) { value.Roots[2] = 1 }},
		{"unequal distinct node", ErrTopology, func(value *Checkpoint) { value.Nodes[1].Scalar++ }},
		{"swapped edges", ErrCanonical, func(value *Checkpoint) { value.Edges[0], value.Edges[1] = value.Edges[1], value.Edges[0] }},
		{"duplicate edge", ErrCanonical, func(value *Checkpoint) { value.Edges[1] = value.Edges[0] }},
		{"dangling edge source", ErrTopology, func(value *Checkpoint) { value.Edges[0].From = 4 }},
		{"dangling edge target", ErrTopology, func(value *Checkpoint) { value.Edges[0].To = 4 }},
		{"edge tag", ErrTag, func(value *Checkpoint) { value.Edges[0].Kind = 99 }},
		{"redirected self cycle", ErrTopology, func(value *Checkpoint) { value.Edges[0].To = 2 }},
		{"redirected next", ErrTopology, func(value *Checkpoint) { value.Edges[1].To = 1 }},
		{"unreachable tail", ErrTopology, func(value *Checkpoint) { value.Edges[1].To = 2 }},
		{"unused edge", ErrCanonical, func(value *Checkpoint) { value.Edges[3] = EdgeRecord{From: 1, Kind: EdgeNext, To: 2} }},
		{"live return", ErrBehavior, func(value *Checkpoint) { value.Behavior.Kind = BehaviorLiveReturn }},
		{"behavior tag", ErrTag, func(value *Checkpoint) { value.Behavior.Kind = 99 }},
		{"dangling behavior self", ErrTopology, func(value *Checkpoint) { value.Behavior.Self = 4 }},
		{"wrong behavior self", ErrTopology, func(value *Checkpoint) { value.Behavior.Self = 2 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := InitialCheckpoint()
			test.mutate(&value)
			if data, err := Encode(value); data != nil || !errors.Is(err, test.want) || !errors.Is(err, ErrCheckpoint) {
				t.Fatalf("Encode = %x, %v; want nil and %v", data, err, test.want)
			}
		})
	}
}

func TestDecodeChecksBoundedHeaderBeforeBodies(t *testing.T) {
	data, err := Encode(InitialCheckpoint())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		offset int
		value  byte
		want   error
	}{
		{"root limit", 4, MaximumRoots + 1, ErrLimit},
		{"node limit", 5, MaximumNodes + 1, ErrLimit},
		{"edge limit", 6, MaximumEdges + 1, ErrLimit},
		{"behavior limit", 7, 2, ErrLimit},
		{"root schema", 4, 2, ErrCanonical},
		{"node schema", 5, 2, ErrCanonical},
		{"edge schema", 6, 2, ErrCanonical},
		{"behavior schema", 7, 0, ErrCanonical},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := append([]byte(nil), data...)
			mutated[test.offset] = test.value
			if value, err := Decode(mutated); value != (Checkpoint{}) || !errors.Is(err, test.want) {
				t.Fatalf("Decode = %#v, %v; want %v", value, err, test.want)
			}
		})
	}
}

func TestRestoreV2FailsBeforePublicationAndCleansConstructedOwners(t *testing.T) {
	checkpoint := InitialCheckpoint()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	live := checkpoint
	live.Behavior.Kind = BehaviorLiveReturn
	tests := []struct {
		name   string
		ctx    context.Context
		value  Checkpoint
		limits rubyv2.Limits
		want   error
	}{
		{"nil context", nil, checkpoint, rubyv2.ProjectionLimits(), ErrNilContext},
		{"pre-canceled", canceled, checkpoint, rubyv2.ProjectionLimits(), context.Canceled},
		{"live return", context.Background(), live, rubyv2.ProjectionLimits(), ErrBehavior},
		{"step limit", context.Background(), checkpoint, rubyv2.Limits{Objects: 4}, rubyv2.ErrStepLimit},
		{"partial object limit", context.Background(), checkpoint, rubyv2.Limits{Steps: 4, Objects: 1}, rubyv2.ErrObjectLimit},
		{"partial cancellation", &cancelAfterContext{Context: context.Background(), at: 9}, checkpoint, rubyv2.ProjectionLimits(), context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := RestoreV2(test.ctx, test.value, test.limits)
			if engine != nil || !errors.Is(err, test.want) {
				t.Fatalf("RestoreV2 = %#v, %v; want nil and %v", engine, err, test.want)
			}
			fresh, err := RestoreV2(context.Background(), checkpoint, rubyv2.ProjectionLimits())
			if err != nil {
				t.Fatalf("fresh restore after failure: %v", err)
			}
			if err := fresh.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}

	sentinel := errors.New("injected restore failure")
	for stage := stageValidated; stage <= stageBeforePublish; stage++ {
		t.Run("observer stage "+stageName(stage), func(t *testing.T) {
			var constructed *rubyv2.ProjectionEngine
			engine, err := restoreV2(context.Background(), checkpoint, rubyv2.ProjectionLimits(), func(current restoreStage, owner *rubyv2.ProjectionEngine) error {
				if current != stage {
					return nil
				}
				constructed = owner
				return sentinel
			})
			if engine != nil || !errors.Is(err, sentinel) {
				t.Fatalf("restore = %#v, %v", engine, err)
			}
			if constructed != nil {
				if _, err := constructed.Call(context.Background()); !errors.Is(err, rubyv2.ErrClosed) {
					t.Fatalf("constructed owner after failure = %v, want closed", err)
				}
			}
		})

		t.Run("cancellation stage "+stageName(stage), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			var constructed *rubyv2.ProjectionEngine
			engine, err := restoreV2(ctx, checkpoint, rubyv2.ProjectionLimits(), func(current restoreStage, owner *rubyv2.ProjectionEngine) error {
				if current == stage {
					constructed = owner
					cancel()
				}
				return nil
			})
			if engine != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("restore = %#v, %v", engine, err)
			}
			if constructed != nil {
				if _, err := constructed.Call(context.Background()); !errors.Is(err, rubyv2.ErrClosed) {
					t.Fatalf("constructed owner after cancellation = %v, want closed", err)
				}
			}
		})
	}
}

func TestCheckpointHasOnlyClosedFixedApplicationData(t *testing.T) {
	var visit func(reflect.Type, string)
	visit = func(value reflect.Type, path string) {
		switch value.Kind() {
		case reflect.Pointer, reflect.UnsafePointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
			t.Fatalf("%s has owner-backed or open kind %s", path, value.Kind())
		case reflect.Array:
			visit(value.Elem(), path+"[]")
		case reflect.Struct:
			for index := 0; index < value.NumField(); index++ {
				field := value.Field(index)
				lower := strings.ToLower(field.Name)
				for _, forbidden := range []string{"object", "pointer", "epoch", "frame", "returntarget", "method", "class", "closure"} {
					if strings.Contains(lower, forbidden) {
						t.Fatalf("%s.%s retains forbidden runtime identity", path, field.Name)
					}
				}
				visit(field.Type, path+"."+field.Name)
			}
		}
	}
	visit(reflect.TypeOf(Checkpoint{}), "Checkpoint")
}

type cancelAfterContext struct {
	context.Context
	calls int
	at    int
}

func (ctx *cancelAfterContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.at {
		return context.Canceled
	}
	return nil
}

func stageName(stage restoreStage) string {
	switch stage {
	case stageValidated:
		return "validated"
	case stageMapped:
		return "mapped"
	case stageOwnerConstructed:
		return "owner-constructed"
	case stageBeforePublish:
		return "before-publish"
	default:
		return "unknown"
	}
}
