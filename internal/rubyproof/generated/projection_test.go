package generated

import (
	"context"
	"errors"
	"testing"
)

var projectionTestScalars = ProjectionScalars{Root: 40, Other: 40, Tail: 7}

func newProjectionTestEngine(t *testing.T) *ProjectionEngine {
	t.Helper()
	engine, err := NewProjectionEngineFromScalars(context.Background(), projectionTestScalars, ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestProjectionEngineOwnsFixedTopology(t *testing.T) {
	engine := newProjectionTestEngine(t)
	if engine.root == nil || engine.root != engine.alias || engine.root == engine.other ||
		engine.nodes[0] != engine.root || engine.nodes[1] != engine.other || engine.nodes[2] == nil ||
		engine.nodes[2] == engine.root || engine.nodes[2] == engine.other ||
		engine.root.edge != engine.root || engine.other.edge != engine.nodes[2] || engine.nodes[2].edge != engine.root ||
		engine.behavior.kind != projectionCapturedSelf || engine.behavior.self != engine.root {
		t.Fatal("generated owner did not reconstruct alias, distinct objects, cycle, tail, and captured self")
	}
	if scalars, err := engine.Snapshot(context.Background()); err != nil || scalars != projectionTestScalars {
		t.Fatalf("snapshot = %#v, %v", scalars, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectionSnapshotRejectsEveryCorruptTopologyClass(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProjectionEngine)
	}{
		{"nil root", func(engine *ProjectionEngine) { engine.root = nil }},
		{"broken alias", func(engine *ProjectionEngine) { engine.alias = engine.other }},
		{"nil other", func(engine *ProjectionEngine) { engine.other = nil }},
		{"other aliases root", func(engine *ProjectionEngine) { engine.other = engine.root }},
		{"wrong root slot", func(engine *ProjectionEngine) { engine.nodes[0] = engine.other }},
		{"wrong other slot", func(engine *ProjectionEngine) { engine.nodes[1] = engine.root }},
		{"nil tail", func(engine *ProjectionEngine) { engine.nodes[2] = nil }},
		{"tail aliases root", func(engine *ProjectionEngine) {
			engine.nodes[2] = engine.root
			engine.other.edge = engine.root
		}},
		{"tail aliases other", func(engine *ProjectionEngine) { engine.nodes[2] = engine.other }},
		{"broken root cycle", func(engine *ProjectionEngine) { engine.root.edge = engine.other }},
		{"broken next edge", func(engine *ProjectionEngine) { engine.other.edge = engine.root }},
		{"broken back edge", func(engine *ProjectionEngine) { engine.nodes[2].edge = engine.other }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := newProjectionTestEngine(t)
			test.mutate(engine)
			if scalars, err := engine.Snapshot(context.Background()); scalars != (ProjectionScalars{}) || !errors.Is(err, ErrInternal) {
				t.Fatalf("corrupt topology snapshot = %#v, %v", scalars, err)
			}
			if err := engine.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProjectionSnapshotRejectsLiveReturnTarget(t *testing.T) {
	engine := newProjectionTestEngine(t)
	engine.behavior.kind = projectionLiveReturn
	engine.behavior.returnTarget = 1

	scalars, err := engine.Snapshot(context.Background())
	if scalars != (ProjectionScalars{}) || !errors.Is(err, ErrNonCheckpointable) {
		t.Fatalf("live-return snapshot = %#v, %v", scalars, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}
