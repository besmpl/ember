package h1bgenerated

import (
	"context"
	"errors"
	"testing"
)

func assertProofResult(t testing.TB, got Result) {
	t.Helper()
	if got.Before != 7 || got.Warm != 7 || got.PeerBefore != 7 || got.First != 43 ||
		got.Second != 44 || got.PeerAfter != 7 || got.PeerAfterReceiverCollection != 7 {
		t.Fatalf("result = %#v, want 7/7/7/43/44/7/7", got)
	}
}

func assertNormalStats(t testing.TB, got Stats) {
	t.Helper()
	want := Stats{
		ObjectAllocations: 2, EnvironmentAllocations: 1,
		ObjectReclaims: 2, EnvironmentReclaims: 1,
		Collections: 3, TotalMarkWork: 4, LastMarkWork: 0, MarkHighWater: 3,
	}
	if got != want {
		t.Fatalf("stats = %#v, want %#v", got, want)
	}
}

func TestGeneratedH1bExactResultPICAndReclamation(t *testing.T) {
	engine := NewEngine()
	got, err := engine.Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertProofResult(t, got)
	assertNormalStats(t, got.Stats)
	if !engine.ordinaryArm.valid || engine.ordinaryArm.hits != 4 || engine.ordinaryArm.misses != 1 {
		t.Fatalf("ordinary PIC = %#v, want valid with 4 hits and publish transition miss", engine.ordinaryArm)
	}
	if engine.singletonArm.object == 0 || engine.singletonArm.environment == 0 {
		t.Fatalf("singleton PIC = %#v, want populated weak arm", engine.singletonArm)
	}
	if _, err := engine.arena.resolveObject(engine.singletonArm.object); !errors.Is(err, errInvalidRef) {
		t.Fatalf("weak singleton arm retained reclaimed object: %v", err)
	}
	if _, err := engine.arena.resolveEnvironment(engine.singletonArm.environment); !errors.Is(err, errInvalidRef) {
		t.Fatalf("weak singleton arm retained reclaimed environment: %v", err)
	}
}

func TestGeneratedH1bEnvironmentPressureUsesOnlyInstallRoot(t *testing.T) {
	engine := NewEngine()
	engine.forceEnvironmentPressure = true
	got, err := engine.Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertProofResult(t, got)
	want := Stats{
		ObjectAllocations: 4, EnvironmentAllocations: 3,
		ObjectReclaims: 4, EnvironmentReclaims: 3,
		Collections: 4, TotalMarkWork: 6, LastMarkWork: 0, MarkHighWater: 3,
	}
	if got.Stats != want {
		t.Fatalf("pressure stats = %#v, want %#v", got.Stats, want)
	}
	if !engine.ordinaryArm.valid || engine.ordinaryArm.hits != 4 || engine.ordinaryArm.misses != 1 {
		t.Fatalf("pressure ordinary PIC = %#v", engine.ordinaryArm)
	}
}

func TestGeneratedH1bRunsAllocateNoGoHeapAfterConstruction(t *testing.T) {
	ctx, limits := context.Background(), ProofLimits()
	for _, test := range []struct {
		name     string
		pressure bool
		want     Stats
	}{
		{name: "normal", want: Stats{ObjectAllocations: 2, EnvironmentAllocations: 1, ObjectReclaims: 2, EnvironmentReclaims: 1, Collections: 3, TotalMarkWork: 4, MarkHighWater: 3}},
		{name: "pressure", pressure: true, want: Stats{ObjectAllocations: 4, EnvironmentAllocations: 3, ObjectReclaims: 4, EnvironmentReclaims: 3, Collections: 4, TotalMarkWork: 6, MarkHighWater: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := NewEngine()
			engine.forceEnvironmentPressure = test.pressure
			var got Result
			var runErr error
			allocations := testing.AllocsPerRun(100, func() {
				if runErr == nil {
					got, runErr = engine.Run(ctx, limits)
				}
			})
			if runErr != nil {
				t.Fatal(runErr)
			}
			assertProofResult(t, got)
			if got.Stats != test.want {
				t.Fatalf("stats = %#v, want %#v", got.Stats, test.want)
			}
			if allocations != 0 {
				t.Fatalf("Run allocations = %g, want 0", allocations)
			}
		})
	}
}

func TestGeneratedH1bAdmissionCloseABAAndRetirement(t *testing.T) {
	t.Run("admission and close", func(t *testing.T) {
		engine := NewEngine()
		if _, err := engine.Run(context.Background(), Limits{}); !errors.Is(err, ErrStepLimit) {
			t.Fatalf("zero limits returned %v", err)
		}
		engine.busy = true
		if _, err := engine.Run(context.Background(), ProofLimits()); !errors.Is(err, ErrBusy) {
			t.Fatalf("busy Run returned %v", err)
		}
		if err := engine.Close(); !errors.Is(err, ErrBusy) {
			t.Fatalf("busy Close returned %v", err)
		}
		engine.busy = false
		if err := engine.Close(); err != nil {
			t.Fatal(err)
		}
		if err := engine.Close(); err != nil {
			t.Fatalf("idempotent Close returned %v", err)
		}
		if _, err := engine.Run(context.Background(), ProofLimits()); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed Run returned %v", err)
		}
	})

	t.Run("ABA", func(t *testing.T) {
		var arena arena
		arena.init()
		oldObject, err := arena.allocateObject()
		if err != nil {
			t.Fatal(err)
		}
		tx, err := arena.beginDefinition(oldObject, integerValue(40))
		if err != nil {
			t.Fatal(err)
		}
		if err := arena.commitDefinition(tx); err != nil {
			t.Fatal(err)
		}
		oldArm := weakArm{object: oldObject, environment: tx.environment}
		if err := arena.collect(); err != nil {
			t.Fatal(err)
		}
		newObject, err := arena.allocateObject()
		if err != nil {
			t.Fatal(err)
		}
		if uint8(uint32(newObject)&indexMask) != uint8(uint32(oldObject)&indexMask) || newObject == oldObject {
			t.Fatalf("deterministic reuse old=%#x new=%#x", oldObject, newObject)
		}
		newSlot, err := arena.resolveObject(newObject)
		if err != nil {
			t.Fatal(err)
		}
		if newSlot.present || newSlot.environment != 0 || oldArm.object == newObject {
			t.Fatalf("stale arm authorized reused object: arm=%#v slot=%#v", oldArm, newSlot)
		}
		if _, err := arena.resolveEnvironment(oldArm.environment); !errors.Is(err, errInvalidRef) {
			t.Fatalf("old environment resolved after reuse: %v", err)
		}
	})

	t.Run("maximum generation retires", func(t *testing.T) {
		var arena arena
		arena.init()
		arena.objects[0].generation = maximumGeneration
		old, err := arena.allocateObject()
		if err != nil {
			t.Fatal(err)
		}
		if err := arena.collect(); err != nil {
			t.Fatal(err)
		}
		if !arena.objects[0].retired || arena.objects[0].generation != 0 || arena.objectFree != 2 {
			t.Fatalf("retired slot/free list = %#v/free %d", arena.objects[0], arena.objectFree)
		}
		if _, err := arena.resolveObject(old); !errors.Is(err, errInvalidRef) {
			t.Fatalf("retired reference resolved: %v", err)
		}
	})
}
