package rubyproof

import (
	"context"
	"errors"
	"testing"
	"time"
)

func compileH1bRuntimeTestProgram(t testing.TB) H1bProgram {
	t.Helper()
	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 || program.IsZero() {
		t.Fatalf("CompileH1b(H1bSource) = zero=%v diagnostics=%v", program.IsZero(), diagnostics)
	}
	return program
}

func installH1bRuntimeTestDefinition(t testing.TB, arena *h1bArena, object h1bObjectRef, captured int64) h1bPICArm {
	t.Helper()
	transaction, err := arena.beginDefinition(object, captured)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.commitDefinition(context.Background(), transaction); err != nil {
		t.Fatal(err)
	}
	return h1bPICArm{object: object, environment: transaction.environment}
}

func assertH1bRuntimeTestResult(t testing.TB, result H1bResult) {
	t.Helper()
	if result.Before != 7 || result.Warm != 7 || result.PeerBefore != 7 || result.First != 43 ||
		result.Second != 44 || result.PeerAfter != 7 || result.PeerAfterReceiverCollection != 7 {
		t.Fatalf("detached result = %#v, want 7/7/7/43/44/7/7", result)
	}
}

func TestH1bRuntimeExactRootsCollectionsPICAndReclamation(t *testing.T) {
	runtime, err := compileH1bRuntimeTestProgram(t).NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	result, err := runtime.Run(context.Background(), H1bProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertH1bRuntimeTestResult(t, result)
	wantStats := H1bStats{
		ObjectAllocations: 2, EnvironmentAllocations: 1,
		ObjectReclaims: 2, EnvironmentReclaims: 1,
		Collections: 3, TotalMarkWork: 4, LastMarkWork: 0, MarkHighWater: 3,
	}
	if result.Stats != wantStats {
		t.Fatalf("stats = %#v, want %#v", result.Stats, wantStats)
	}
	if runtime.collectionCount != 3 {
		t.Fatalf("collection count = %d, want 3", runtime.collectionCount)
	}
	wantWork := [...]h1bCollectionWork{
		{RootSlots: 3, RootValues: 2, MarkedObjects: 2, MarkedEnvironments: 1},
		{RootSlots: 2, RootValues: 1, MarkedObjects: 1, ReclaimedObjects: 1, ReclaimedEnvironments: 1},
		{RootSlots: 2, ReclaimedObjects: 1},
	}
	for index := range wantWork {
		if runtime.collections[index] != wantWork[index] {
			t.Fatalf("collection %d = %#v, want %#v", index+1, runtime.collections[index], wantWork[index])
		}
	}
	if runtime.pic.ordinaryHits != 4 || runtime.pic.ordinaryMisses != 1 ||
		runtime.pic.singletonHits != 1 || runtime.pic.singletonMisses != 2 ||
		!runtime.pic.ordinaryValid || !runtime.pic.singletonValid {
		t.Fatalf("PIC observations = %#v", runtime.pic)
	}
	if _, err := runtime.arena.resolveObject(runtime.lastReceiver); !errors.Is(err, errH1bInvalidRef) {
		t.Fatalf("reclaimed receiver resolved with %v", err)
	}
	if _, err := runtime.arena.resolveEnvironment(runtime.lastEnvironment); !errors.Is(err, errH1bInvalidRef) {
		t.Fatalf("reclaimed environment resolved with %v", err)
	}
	if _, hit := runtime.arena.hitPICArm(runtime.lastReceiver, runtime.lastSingletonArm); hit {
		t.Fatal("weak singleton PIC rooted or authorized reclaimed receiver")
	}
}

func TestH1bRuntimeEnvironmentPressureUsesOnlyCalleeReceiverRoot(t *testing.T) {
	runtime, err := compileH1bRuntimeTestProgram(t).NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	runtime.forceEnvironmentPressure = true
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	result, err := runtime.Run(context.Background(), H1bProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertH1bRuntimeTestResult(t, result)
	if runtime.collectionCount != 4 {
		t.Fatalf("collections = %d, want pressure plus three explicit collections", runtime.collectionCount)
	}
	pressure := runtime.collections[0]
	if pressure != (h1bCollectionWork{
		RootSlots: 3, RootValues: 2, MarkedObjects: 2,
		ReclaimedObjects: 2, ReclaimedEnvironments: 2,
	}) {
		t.Fatalf("environment-pressure work = %#v; want peer and callee-only receiver rooted and pressure records reclaimed", pressure)
	}
	if helper := runtime.collections[1]; helper != (h1bCollectionWork{
		RootSlots: 3, RootValues: 2, MarkedObjects: 2, MarkedEnvironments: 1,
	}) {
		t.Fatalf("helper explicit collection = %#v", helper)
	}
	if result.Stats.ObjectAllocations != 4 || result.Stats.EnvironmentAllocations != 3 ||
		result.Stats.ObjectReclaims != 4 || result.Stats.EnvironmentReclaims != 3 ||
		result.Stats.Collections != 4 || result.Stats.TotalMarkWork != 6 ||
		result.Stats.LiveObjects != 0 || result.Stats.LiveEnvironments != 0 || result.Stats.Roots != 0 {
		t.Fatalf("pressure-run final stats = %#v", result.Stats)
	}
}

type h1bCancelOnPoll struct {
	calls    int
	cancelAt int
}

func (*h1bCancelOnPoll) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*h1bCancelOnPoll) Done() <-chan struct{}       { return nil }
func (*h1bCancelOnPoll) Value(any) any               { return nil }
func (ctx *h1bCancelOnPoll) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestH1bRuntimeDefinitionCancellationRollsBackAndPoisons(t *testing.T) {
	runtime, err := compileH1bRuntimeTestProgram(t).NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	ctx := &h1bCancelOnPoll{cancelAt: 5} // final pre-publication poll
	if _, err := runtime.Run(ctx, H1bProofLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled definition returned %v", err)
	}
	if runtime.arena.transaction != (h1bTransaction{}) || runtime.arena.environmentFree != 1 ||
		runtime.arena.statistics.EnvironmentAllocations != 0 || runtime.arena.statistics.LiveEnvironments != 0 ||
		runtime.arena.rootCount != 0 {
		t.Fatalf("definition rollback state = arena %#v", runtime.arena)
	}
	for index := range runtime.arena.objects {
		object := runtime.arena.objects[index]
		if object.live && (object.present || object.environment != 0) {
			t.Fatalf("rollback published object %d sidecar: %#v", index+1, object)
		}
	}
	if _, err := runtime.Run(context.Background(), H1bProofLimits()); !errors.Is(err, ErrH1bPoisoned) {
		t.Fatalf("reuse after canceled run returned %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestH1bRuntimeCancellationBeforeSweepIsAtomic(t *testing.T) {
	arena := newH1bArena()
	rooted, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
	if err != nil {
		t.Fatal(err)
	}
	unrooted, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
	if err != nil {
		t.Fatal(err)
	}
	root, err := arena.reserveRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setRoot(root, h1bObjectValue(rooted)); err != nil {
		t.Fatal(err)
	}
	before := arena.statistics
	ctx := &h1bCancelOnPoll{cancelAt: 2}
	if err := arena.collect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("collect cancellation returned %v", err)
	}
	if arena.statistics != before || arena.markCount != 0 {
		t.Fatalf("canceled trace exposed sweep or retained work: stats=%#v marks=%d", arena.statistics, arena.markCount)
	}
	for index := range arena.objects {
		if arena.objects[index].marked {
			t.Fatalf("object %d remained marked", index+1)
		}
	}
	if _, err := arena.resolveObject(unrooted); err != nil {
		t.Fatalf("unrooted object swept after cancellation: %v", err)
	}
}

func TestH1bRuntimeStagingAndInvalidTraceFailBeforeSweep(t *testing.T) {
	t.Run("staging", func(t *testing.T) {
		arena := newH1bArena()
		object, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := arena.beginDefinition(object, 40)
		if err != nil {
			t.Fatal(err)
		}
		before := arena
		if err := arena.collect(context.Background()); !errors.Is(err, errH1bTransactionRun) {
			t.Fatalf("collection during staging returned %v", err)
		}
		if arena != before {
			t.Fatal("rejected staging collection mutated arena")
		}
		if err := arena.abortDefinition(transaction); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invalid edge", func(t *testing.T) {
		arena := newH1bArena()
		rooted, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
		if err != nil {
			t.Fatal(err)
		}
		arm := installH1bRuntimeTestDefinition(t, &arena, rooted, 40)
		doomed, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
		if err != nil {
			t.Fatal(err)
		}
		root, err := arena.reserveRoot()
		if err != nil {
			t.Fatal(err)
		}
		if err := arena.setRoot(root, h1bObjectValue(rooted)); err != nil {
			t.Fatal(err)
		}
		object, _ := arena.resolveObject(rooted)
		_, generation, _ := h1bUnpackReference(uint32(arm.environment))
		object.environment = h1bEnvironmentRef(h1bPackReference(1, generation+1))
		before := arena.statistics
		if err := arena.collect(context.Background()); !errors.Is(err, errH1bInvalidRef) {
			t.Fatalf("invalid trace returned %v", err)
		}
		if arena.statistics != before {
			t.Fatalf("invalid trace exposed sweep: before=%#v after=%#v", before, arena.statistics)
		}
		if _, err := arena.resolveObject(doomed); err != nil {
			t.Fatalf("unrooted object swept after invalid trace: %v", err)
		}
	})
}

func TestH1bRuntimeCapsFailClosed(t *testing.T) {
	t.Run("roots", func(t *testing.T) {
		arena := newH1bArena()
		for range h1bRootCapacity {
			if _, err := arena.reserveRoot(); err != nil {
				t.Fatal(err)
			}
		}
		before := arena
		if _, err := arena.reserveRoot(); !errors.Is(err, errH1bCapacity) {
			t.Fatalf("root above cap returned %v", err)
		}
		if arena != before {
			t.Fatal("root cap failure mutated arena")
		}
		if err := arena.popRoots(h1bRootCapacity + 1); !errors.Is(err, errH1bRootOrder) {
			t.Fatalf("invalid marker returned %v", err)
		}
		if arena != before {
			t.Fatal("invalid marker mutated arena")
		}
	})

	t.Run("objects", func(t *testing.T) {
		arena := newH1bArena()
		for range h1bObjectCapacity {
			if _, err := arena.allocateObject(dispatchClassID(1), shapeID(1)); err != nil {
				t.Fatal(err)
			}
		}
		before := arena
		if reference, err := arena.allocateObject(dispatchClassID(1), shapeID(1)); reference != 0 || !errors.Is(err, errH1bCapacity) {
			t.Fatalf("object above cap = %#x, %v", reference, err)
		}
		if arena != before {
			t.Fatal("object cap failure mutated arena")
		}
	})

	t.Run("environments", func(t *testing.T) {
		arena := newH1bArena()
		objects := [3]h1bObjectRef{}
		for index := range objects {
			object, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
			if err != nil {
				t.Fatal(err)
			}
			objects[index] = object
		}
		installH1bRuntimeTestDefinition(t, &arena, objects[0], 1)
		installH1bRuntimeTestDefinition(t, &arena, objects[1], 2)
		before := arena
		if transaction, err := arena.beginDefinition(objects[2], 3); transaction != (h1bTransaction{}) || !errors.Is(err, errH1bCapacity) {
			t.Fatalf("environment above cap = %#v, %v", transaction, err)
		}
		if arena != before {
			t.Fatal("environment cap failure mutated arena")
		}
	})
}

func TestH1bRuntimeABAAndMaximumGenerationRetirement(t *testing.T) {
	t.Run("ABA", func(t *testing.T) {
		arena := newH1bArena()
		oldObject, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
		if err != nil {
			t.Fatal(err)
		}
		oldArm := installH1bRuntimeTestDefinition(t, &arena, oldObject, 40)
		if err := arena.collect(context.Background()); err != nil {
			t.Fatal(err)
		}
		newObject, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
		if err != nil {
			t.Fatal(err)
		}
		newArm := installH1bRuntimeTestDefinition(t, &arena, newObject, 41)
		oldIndex, oldGeneration, _ := h1bUnpackReference(uint32(oldObject))
		newIndex, newGeneration, _ := h1bUnpackReference(uint32(newObject))
		if oldIndex != 1 || newIndex != oldIndex || newGeneration != oldGeneration+1 || newArm.environment == oldArm.environment {
			t.Fatalf("reuse = objects %#x/%#x environments %#x/%#x", oldObject, newObject, oldArm.environment, newArm.environment)
		}
		if _, err := arena.resolveObject(oldObject); !errors.Is(err, errH1bInvalidRef) {
			t.Fatalf("stale object resolved with %v", err)
		}
		if _, err := arena.resolveEnvironment(oldArm.environment); !errors.Is(err, errH1bInvalidRef) {
			t.Fatalf("stale environment resolved with %v", err)
		}
		if _, hit := arena.hitPICArm(newObject, oldArm); hit {
			t.Fatal("stale weak PIC authorized reused slots")
		}
	})

	t.Run("retirement", func(t *testing.T) {
		arena := newH1bArena()
		arena.objects[0].generation = h1bMaximumGeneration
		arena.environments[0].generation = h1bMaximumGeneration
		oldObject, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
		if err != nil {
			t.Fatal(err)
		}
		oldArm := installH1bRuntimeTestDefinition(t, &arena, oldObject, 40)
		if err := arena.collect(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !arena.objects[0].retired || !arena.environments[0].retired ||
			arena.objects[0].generation != h1bMaximumGeneration || arena.environments[0].generation != h1bMaximumGeneration {
			t.Fatalf("maximum generation wrapped: object=%#v environment=%#v", arena.objects[0], arena.environments[0])
		}
		newObject, err := arena.allocateObject(dispatchClassID(1), shapeID(1))
		if err != nil {
			t.Fatal(err)
		}
		newArm := installH1bRuntimeTestDefinition(t, &arena, newObject, 41)
		objectIndex, objectGeneration, _ := h1bUnpackReference(uint32(newObject))
		environmentIndex, environmentGeneration, _ := h1bUnpackReference(uint32(newArm.environment))
		if objectIndex != 2 || objectGeneration != 1 || environmentIndex != 2 || environmentGeneration != 1 {
			t.Fatalf("post-retirement allocation = object %#x environment %#x", newObject, newArm.environment)
		}
		if _, err := arena.resolveObject(oldObject); !errors.Is(err, errH1bInvalidRef) {
			t.Fatalf("retired object resolved with %v", err)
		}
		if _, err := arena.resolveEnvironment(oldArm.environment); !errors.Is(err, errH1bInvalidRef) {
			t.Fatalf("retired environment resolved with %v", err)
		}
	})
}

func TestH1bRuntimeLimitCancellationAndCloseAdmission(t *testing.T) {
	program := compileH1bRuntimeTestProgram(t)
	runtime, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	before := *runtime
	if _, err := runtime.Run(context.Background(), H1bLimits{}); !errors.Is(err, ErrH1bLimit) {
		t.Fatalf("zero limits returned %v", err)
	}
	if *runtime != before {
		t.Fatal("limit admission mutated runtime")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.Run(canceled, H1bProofLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-admission cancellation returned %v", err)
	}
	if *runtime != before {
		t.Fatal("pre-admission cancellation mutated runtime")
	}

	object, err := runtime.arena.allocateObject(dispatchClassID(1), shapeID(1))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := runtime.arena.beginDefinition(object, 40)
	if err != nil {
		t.Fatal(err)
	}
	staged := *runtime
	if err := runtime.Close(); !errors.Is(err, ErrH1bBusy) {
		t.Fatalf("close during transaction returned %v", err)
	}
	if *runtime != staged {
		t.Fatal("busy close mutated runtime")
	}
	if err := runtime.arena.abortDefinition(transaction); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if *runtime != (H1bRuntime{closed: true, arena: h1bArena{closed: true}}) {
		t.Fatalf("close did not erase owner: %#v", runtime)
	}
	if _, err := runtime.arena.resolveObject(object); !errors.Is(err, ErrH1bClosed) {
		t.Fatalf("old object after close returned %v", err)
	}

	busy, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	busy.busy = true
	if err := busy.Close(); !errors.Is(err, ErrH1bBusy) {
		t.Fatalf("close busy runtime returned %v", err)
	}
	busy.busy = false
	if err := busy.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestH1bRuntimeAllocatesNoGoHeapAfterConstruction(t *testing.T) {
	runtime, err := compileH1bRuntimeTestProgram(t).NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx, limits := context.Background(), H1bProofLimits()
	var result H1bResult
	var runErr error
	allocations := testing.AllocsPerRun(100, func() {
		if runErr == nil {
			result, runErr = runtime.Run(ctx, limits)
		}
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	assertH1bRuntimeTestResult(t, result)
	if allocations != 0 {
		t.Fatalf("Run allocations after construction = %g, want 0", allocations)
	}
}
