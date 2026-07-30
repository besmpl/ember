package rubyproof

import (
	"context"
	"errors"
	"testing"
)

const (
	h1LifetimeSelector = selectorID(1)
	h1LifetimeTarget   = h1TargetID(1)
)

func allocateH1LifetimeObject(t *testing.T, arena *h1Arena, value int64) h1ObjectRef {
	t.Helper()
	reference, err := arena.allocateObject(dispatchClassID(1), shapeID(1), value)
	if err != nil {
		t.Fatalf("allocate H1 object: %v", err)
	}
	return reference
}

func installH1LifetimeSingleton(t *testing.T, arena *h1Arena, object h1ObjectRef, captured int64) h1CallData {
	t.Helper()
	transaction, err := arena.beginSingletonDefinition(object, h1LifetimeSelector, h1LifetimeTarget, lookupVisibilityPublic, h1IntegerValue(captured), 0)
	if err != nil {
		t.Fatalf("begin H1 singleton definition: %v", err)
	}
	if err := arena.commitSingletonDefinition(transaction, false); err != nil {
		t.Fatalf("commit H1 singleton definition: %v", err)
	}
	data, err := arena.resolveCallData(object, h1LifetimeSelector)
	if err != nil {
		t.Fatalf("resolve H1 call data: %v", err)
	}
	return data
}

func TestH1ObjectEigenEnvironmentCycleLifetime(t *testing.T) {
	arena := newH1Arena()
	object := allocateH1LifetimeObject(t, arena, 7)
	transaction, err := arena.beginSingletonDefinition(
		object, h1LifetimeSelector, h1LifetimeTarget, lookupVisibilityPublic, h1ObjectValue(object), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.commitSingletonDefinition(transaction, false); err != nil {
		t.Fatal(err)
	}
	if err := arena.pushRoot(h1ObjectValue(object)); err != nil {
		t.Fatal(err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatalf("collect rooted object/eigen/environment cycle: %v", err)
	}
	if got := arena.stats(); got.LiveObjects != 1 || got.LiveEigen != 1 || got.LiveEnvironments != 1 {
		t.Fatalf("rooted cycle live counts = %#v, want one object/eigen/environment", got)
	}
	cell, err := arena.loadEnvironment(transaction.environment, 0)
	if err != nil || cell.kind != h1ValueObject || cell.objectRef() != object {
		t.Fatalf("cycle environment cell = %#v, %v; want object %#x", cell, err, object)
	}

	if err := arena.popRoots(0); err != nil {
		t.Fatal(err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatalf("collect unrooted cycle: %v", err)
	}
	if got := arena.stats(); got.LiveObjects != 0 || got.LiveEigen != 0 || got.LiveEnvironments != 0 ||
		got.ObjectReclaims != 1 || got.EigenReclaims != 1 || got.EnvironmentReclaims != 1 {
		t.Fatalf("unrooted cycle reclamation = %#v, want one reclaim of every kind", got)
	}
	if _, err := arena.resolveObject(object); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("reclaimed cycle object resolved with %v", err)
	}
	if _, err := arena.resolveEigen(transaction.eigen); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("reclaimed cycle eigen resolved with %v", err)
	}
	if _, err := arena.resolveEnvironment(transaction.environment); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("reclaimed cycle environment resolved with %v", err)
	}
}

func requireH1ReferenceIndex(t *testing.T, reference uint32, want uint8) uint32 {
	t.Helper()
	index, generation, ok := h1UnpackReference(reference)
	if !ok || index != want {
		t.Fatalf("reference %#x decoded as index=%d generation=%d valid=%v, want index=%d", reference, index, generation, ok, want)
	}
	return generation
}

func TestH1CapturedCellSurvivesDefiningFrameRootRemoval(t *testing.T) {
	arena := newH1Arena()
	object := allocateH1LifetimeObject(t, arena, 7)

	// The lower root models the escaped receiver. The top duplicate models the
	// defining frame. Removing the frame must not release the installed capture.
	if err := arena.pushRoot(h1ObjectValue(object)); err != nil {
		t.Fatal(err)
	}
	frameMarker := arena.rootMarker()
	if err := arena.pushRoot(h1ObjectValue(object)); err != nil {
		t.Fatal(err)
	}
	data := installH1LifetimeSingleton(t, arena, object, 41)
	if err := arena.storeEnvironment(data.environment, 0, h1IntegerValue(42)); err != nil {
		t.Fatalf("mutate captured cell: %v", err)
	}
	if err := arena.popRoots(frameMarker); err != nil {
		t.Fatalf("remove defining-frame root: %v", err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatalf("collect after defining frame returned: %v", err)
	}
	if got, err := arena.loadEnvironment(data.environment, 0); err != nil || got.kind != h1ValueInteger || got.integerValue() != 42 {
		t.Fatalf("captured cell after collection = %#v, %v; want integer 42, nil", got, err)
	}
	if got := arena.stats(); got.LiveObjects != 1 || got.LiveEigen != 1 || got.LiveEnvironments != 1 {
		t.Fatalf("live graph after frame-root removal = %#v, want one object/eigen/environment", got)
	}

	if err := arena.popRoots(0); err != nil {
		t.Fatal(err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatal(err)
	}
	if _, err := arena.loadEnvironment(data.environment, 0); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("unrooted captured environment load returned %v, want %v", err, errH1ArenaInvalidRef)
	}
}

func TestH1StaleReferencesRejectAfterDeterministicReuse(t *testing.T) {
	arena := newH1Arena()
	oldObject := allocateH1LifetimeObject(t, arena, 1)
	oldData := installH1LifetimeSingleton(t, arena, oldObject, 10)
	if err := arena.collect(false); err != nil {
		t.Fatal(err)
	}
	if _, err := arena.resolveObject(oldObject); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("stale object resolution returned %v, want %v", err, errH1ArenaInvalidRef)
	}
	if _, err := arena.resolveEigen(oldData.eigen); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("stale eigen resolution returned %v, want %v", err, errH1ArenaInvalidRef)
	}
	if _, err := arena.resolveEnvironment(oldData.environment); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("stale environment resolution returned %v, want %v", err, errH1ArenaInvalidRef)
	}

	newObject := allocateH1LifetimeObject(t, arena, 2)
	newData := installH1LifetimeSingleton(t, arena, newObject, 20)
	oldObjectGeneration := requireH1ReferenceIndex(t, uint32(oldObject), 1)
	newObjectGeneration := requireH1ReferenceIndex(t, uint32(newObject), 1)
	oldEigenGeneration := requireH1ReferenceIndex(t, uint32(oldData.eigen), 1)
	newEigenGeneration := requireH1ReferenceIndex(t, uint32(newData.eigen), 1)
	oldEnvironmentGeneration := requireH1ReferenceIndex(t, uint32(oldData.environment), 1)
	newEnvironmentGeneration := requireH1ReferenceIndex(t, uint32(newData.environment), 1)
	if newObjectGeneration != oldObjectGeneration+1 || newEigenGeneration != oldEigenGeneration+1 || newEnvironmentGeneration != oldEnvironmentGeneration+1 {
		t.Fatalf("reuse generations object %d->%d eigen %d->%d environment %d->%d, want exact increments", oldObjectGeneration, newObjectGeneration, oldEigenGeneration, newEigenGeneration, oldEnvironmentGeneration, newEnvironmentGeneration)
	}
	if _, err := arena.resolveObject(oldObject); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("old object aliased reused slot: %v", err)
	}
	if _, err := arena.resolveEigen(oldData.eigen); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("old eigen aliased reused slot: %v", err)
	}
	if _, err := arena.resolveEnvironment(oldData.environment); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("old environment aliased reused slot: %v", err)
	}
}

func TestH1MaximumGenerationRetiresRatherThanWrapping(t *testing.T) {
	arena := newH1Arena()
	arena.objects[0].generation = h1MaximumGeneration
	arena.eigen[0].generation = h1MaximumGeneration
	arena.environments[0].generation = h1MaximumGeneration

	oldObject := allocateH1LifetimeObject(t, arena, 1)
	oldData := installH1LifetimeSingleton(t, arena, oldObject, 1)
	if err := arena.collect(false); err != nil {
		t.Fatal(err)
	}
	if got := arena.stats(); got.RetiredObjects != 1 || got.RetiredEigen != 1 || got.RetiredEnvironments != 1 {
		t.Fatalf("retirement stats = %#v, want one retired slot of every kind", got)
	}
	if !arena.objects[0].retired || !arena.eigen[0].retired || !arena.environments[0].retired {
		t.Fatalf("maximum-generation slots were not retired: object=%#v eigen=%#v environment=%#v", arena.objects[0], arena.eigen[0], arena.environments[0])
	}
	if arena.objects[0].generation != h1MaximumGeneration || arena.eigen[0].generation != h1MaximumGeneration || arena.environments[0].generation != h1MaximumGeneration {
		t.Fatal("retirement wrapped or changed a maximum generation")
	}

	newObject := allocateH1LifetimeObject(t, arena, 2)
	newData := installH1LifetimeSingleton(t, arena, newObject, 2)
	if generation := requireH1ReferenceIndex(t, uint32(newObject), 2); generation != 1 {
		t.Fatalf("post-retirement object generation = %d, want 1 in slot 2", generation)
	}
	if generation := requireH1ReferenceIndex(t, uint32(newData.eigen), 2); generation != 1 {
		t.Fatalf("post-retirement eigen generation = %d, want 1 in slot 2", generation)
	}
	if generation := requireH1ReferenceIndex(t, uint32(newData.environment), 2); generation != 1 {
		t.Fatalf("post-retirement environment generation = %d, want 1 in slot 2", generation)
	}
	for name, resolve := range map[string]func() error{
		"object":      func() error { _, err := arena.resolveObject(oldObject); return err },
		"eigen":       func() error { _, err := arena.resolveEigen(oldData.eigen); return err },
		"environment": func() error { _, err := arena.resolveEnvironment(oldData.environment); return err },
	} {
		if err := resolve(); !errors.Is(err, errH1ArenaInvalidRef) {
			t.Fatalf("retired %s reference resolved with %v, want %v", name, err, errH1ArenaInvalidRef)
		}
	}
}

func TestH1SingletonTransactionRollbackRestoresCapacity(t *testing.T) {
	for _, test := range []struct {
		name     string
		rollback func(*h1Arena, h1SingletonTransaction) error
		want     error
	}{
		{
			name: "canceled commit",
			rollback: func(arena *h1Arena, transaction h1SingletonTransaction) error {
				return arena.commitSingletonDefinition(transaction, true)
			},
			want: context.Canceled,
		},
		{
			name: "explicit abort",
			rollback: func(arena *h1Arena, transaction h1SingletonTransaction) error {
				return arena.abortSingletonDefinition(transaction)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			arena := newH1Arena()
			object := allocateH1LifetimeObject(t, arena, 1)
			transaction, err := arena.beginSingletonDefinition(object, h1LifetimeSelector, h1LifetimeTarget, lookupVisibilityPublic, h1IntegerValue(41), 0)
			if err != nil {
				t.Fatal(err)
			}
			objectSlot, err := arena.resolveObject(object)
			if err != nil || objectSlot.eigen != 0 {
				t.Fatalf("staged transaction published object link: slot=%#v err=%v", objectSlot, err)
			}
			if err := test.rollback(arena, transaction); !errors.Is(err, test.want) {
				t.Fatalf("rollback returned %v, want %v", err, test.want)
			}
			if objectSlot.eigen != 0 || arena.transaction.id != 0 {
				t.Fatalf("rollback left publication or active transaction: object=%#v transaction=%#v", objectSlot, arena.transaction)
			}
			if got := arena.stats(); got.LiveEigen != 0 || got.LiveEnvironments != 0 || got.EigenAllocations != 0 || got.EnvironmentAllocations != 0 {
				t.Fatalf("rollback changed committed lifetime stats: %#v", got)
			}
			if arena.eigenFree != 1 || arena.environmentFree != 1 {
				t.Fatalf("rollback free heads eigen=%d environment=%d, want 1/1", arena.eigenFree, arena.environmentFree)
			}
			if _, err := arena.resolveEigen(transaction.eigen); !errors.Is(err, errH1ArenaInvalidRef) {
				t.Fatalf("rolled-back eigen resolved with %v", err)
			}
			if _, err := arena.resolveEnvironment(transaction.environment); !errors.Is(err, errH1ArenaInvalidRef) {
				t.Fatalf("rolled-back environment resolved with %v", err)
			}

			next, err := arena.beginSingletonDefinition(object, h1LifetimeSelector, h1LifetimeTarget, lookupVisibilityPublic, h1IntegerValue(42), 0)
			if err != nil {
				t.Fatalf("begin after rollback: %v", err)
			}
			if next.eigen != transaction.eigen || next.environment != transaction.environment {
				t.Fatalf("capacity was not deterministically restored: first=%#v next=%#v", transaction, next)
			}
			if err := arena.abortSingletonDefinition(next); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestH1CollectionRejectsStagedTransaction(t *testing.T) {
	arena := newH1Arena()
	object := allocateH1LifetimeObject(t, arena, 1)
	transaction, err := arena.beginSingletonDefinition(object, h1LifetimeSelector, h1LifetimeTarget, lookupVisibilityPublic, h1IntegerValue(41), 0)
	if err != nil {
		t.Fatal(err)
	}
	before := arena.stats()
	if err := arena.collect(false); !errors.Is(err, errH1ArenaTransactionRun) {
		t.Fatalf("collection during staging returned %v, want %v", err, errH1ArenaTransactionRun)
	}
	if arena.transaction != transaction || arena.stats() != before {
		t.Fatalf("rejected collection mutated staging: transaction=%#v stats=%#v, want %#v/%#v", arena.transaction, arena.stats(), transaction, before)
	}
	if err := arena.commitSingletonDefinition(transaction, false); err != nil {
		t.Fatalf("transaction was not intact after rejected collection: %v", err)
	}
}

func TestH1RootAndSlabCapsFailClosed(t *testing.T) {
	t.Run("root cap and order", func(t *testing.T) {
		arena := newH1Arena()
		object := allocateH1LifetimeObject(t, arena, 1)
		other := allocateH1LifetimeObject(t, arena, 2)
		for range h1RootCapacity {
			if err := arena.pushRoot(h1ObjectValue(object)); err != nil {
				t.Fatal(err)
			}
		}
		before := arena.stats()
		if err := arena.pushRoot(h1ObjectValue(object)); !errors.Is(err, errH1ArenaCapacity) {
			t.Fatalf("root above cap returned %v, want %v", err, errH1ArenaCapacity)
		}
		if arena.stats() != before || arena.rootCount != h1RootCapacity {
			t.Fatalf("root cap failure mutated owner: stats=%#v rootCount=%d", arena.stats(), arena.rootCount)
		}
		_ = other
		if err := arena.popRoots(h1RootCapacity + 1); !errors.Is(err, errH1ArenaRootOrder) {
			t.Fatalf("invalid root marker returned %v, want %v", err, errH1ArenaRootOrder)
		}
		if arena.stats() != before || arena.rootCount != h1RootCapacity {
			t.Fatal("out-of-order root pop mutated the root stack")
		}
	})

	t.Run("object cap", func(t *testing.T) {
		arena := newH1Arena()
		for index := range h1ObjectCapacity {
			allocateH1LifetimeObject(t, arena, int64(index))
		}
		before := arena.stats()
		if reference, err := arena.allocateObject(dispatchClassID(1), shapeID(1), 99); reference != 0 || !errors.Is(err, errH1ArenaCapacity) {
			t.Fatalf("object above cap = %#x, %v; want zero, %v", reference, err, errH1ArenaCapacity)
		}
		if arena.stats() != before || arena.objectFree != 0 {
			t.Fatalf("object cap failure mutated owner: stats=%#v free=%d", arena.stats(), arena.objectFree)
		}
	})

	t.Run("eigen and environment cap", func(t *testing.T) {
		arena := newH1Arena()
		objects := [...]h1ObjectRef{
			allocateH1LifetimeObject(t, arena, 1),
			allocateH1LifetimeObject(t, arena, 2),
			allocateH1LifetimeObject(t, arena, 3),
		}
		installH1LifetimeSingleton(t, arena, objects[0], 1)
		installH1LifetimeSingleton(t, arena, objects[1], 2)
		before := arena.stats()
		transaction, err := arena.beginSingletonDefinition(objects[2], h1LifetimeSelector, h1LifetimeTarget, lookupVisibilityPublic, h1IntegerValue(3), 0)
		if transaction != (h1SingletonTransaction{}) || !errors.Is(err, errH1ArenaCapacity) {
			t.Fatalf("definition above cap = %#v, %v; want zero, %v", transaction, err, errH1ArenaCapacity)
		}
		object, resolveErr := arena.resolveObject(objects[2])
		if resolveErr != nil || object.eigen != 0 || arena.transaction.id != 0 || arena.stats() != before {
			t.Fatalf("definition cap failure partially published: object=%#v resolve=%v transaction=%#v stats=%#v", object, resolveErr, arena.transaction, arena.stats())
		}
	})

	t.Run("invalid allocation facts", func(t *testing.T) {
		arena := newH1Arena()
		before := arena.stats()
		if reference, err := arena.allocateObject(0, shapeID(1), 1); reference != 0 || !errors.Is(err, errH1ArenaInvalidRef) {
			t.Fatalf("zero dispatch allocation = %#x, %v", reference, err)
		}
		if reference, err := arena.allocateObject(dispatchClassID(1), 0, 1); reference != 0 || !errors.Is(err, errH1ArenaInvalidRef) {
			t.Fatalf("zero shape allocation = %#x, %v", reference, err)
		}
		if arena.stats() != before || arena.objectFree != 1 {
			t.Fatalf("invalid allocation mutated owner: stats=%#v free=%d", arena.stats(), arena.objectFree)
		}
	})
}

func TestH1InvalidGraphReferenceAbortsBeforeSweep(t *testing.T) {
	for _, test := range []struct {
		name    string
		corrupt func(*h1Arena, h1ObjectRef, h1CallData) func()
	}{
		{
			name: "object to eigen",
			corrupt: func(arena *h1Arena, objectRef h1ObjectRef, data h1CallData) func() {
				object, _ := arena.resolveObject(objectRef)
				original := object.eigen
				index, generation, _ := h1UnpackReference(uint32(original))
				object.eigen = h1EigenRef(h1PackReference(index, generation+1))
				return func() { object.eigen = original }
			},
		},
		{
			name: "eigen to environment",
			corrupt: func(arena *h1Arena, _ h1ObjectRef, data h1CallData) func() {
				eigen, _ := arena.resolveEigen(data.eigen)
				original := eigen.entry.environment
				index, generation, _ := h1UnpackReference(uint32(original))
				eigen.entry.environment = h1EnvironmentRef(h1PackReference(index, generation+1))
				return func() { eigen.entry.environment = original }
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			arena := newH1Arena()
			root := allocateH1LifetimeObject(t, arena, 1)
			if err := arena.pushRoot(h1ObjectValue(root)); err != nil {
				t.Fatal(err)
			}
			data := installH1LifetimeSingleton(t, arena, root, 41)
			doomed := allocateH1LifetimeObject(t, arena, 2)
			restore := test.corrupt(arena, root, data)
			before := arena.stats()

			if err := arena.collect(false); !errors.Is(err, errH1ArenaInvalidRef) {
				t.Fatalf("collect invalid graph returned %v, want %v", err, errH1ArenaInvalidRef)
			}
			after := arena.stats()
			if after.Collections != before.Collections || after.ObjectReclaims != before.ObjectReclaims || after.EigenReclaims != before.EigenReclaims || after.EnvironmentReclaims != before.EnvironmentReclaims ||
				after.LiveObjects != before.LiveObjects || after.LiveEigen != before.LiveEigen || after.LiveEnvironments != before.LiveEnvironments {
				t.Fatalf("failed mark exposed a partial sweep: before=%#v after=%#v", before, after)
			}
			if arena.markCount != 0 {
				t.Fatalf("failed mark left %d work items", arena.markCount)
			}
			for index := range arena.objects {
				if arena.objects[index].marked {
					t.Fatalf("failed mark left object %d marked", index+1)
				}
			}
			for index := range arena.eigen {
				if arena.eigen[index].marked {
					t.Fatalf("failed mark left eigen %d marked", index+1)
				}
			}
			for index := range arena.environments {
				if arena.environments[index].marked {
					t.Fatalf("failed mark left environment %d marked", index+1)
				}
			}
			if _, err := arena.resolveObject(doomed); err != nil {
				t.Fatalf("unrooted object was swept after mark failure: %v", err)
			}

			restore()
			if err := arena.collect(false); err != nil {
				t.Fatalf("collect repaired graph: %v", err)
			}
			if _, err := arena.resolveObject(doomed); !errors.Is(err, errH1ArenaInvalidRef) {
				t.Fatalf("repaired collection left unrooted object: %v", err)
			}
		})
	}
}

func TestH1WeakArmDoesNotRootOrAuthorizeReusedRow(t *testing.T) {
	arena := newH1Arena()
	oldObject := allocateH1LifetimeObject(t, arena, 1)
	oldData := installH1LifetimeSingleton(t, arena, oldObject, 10)
	arm, err := arena.admitPICArm(oldData)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatal(err)
	}
	if got := arena.stats(); got.LiveObjects != 0 || got.LiveEigen != 0 || got.LiveEnvironments != 0 {
		t.Fatalf("weak arm retained graph: %#v", got)
	}

	newObject := allocateH1LifetimeObject(t, arena, 2)
	newData := installH1LifetimeSingleton(t, arena, newObject, 20)
	oldIndex, oldGeneration, _ := h1UnpackReference(uint32(oldData.eigen))
	newIndex, newGeneration, _ := h1UnpackReference(uint32(newData.eigen))
	if oldIndex != newIndex || oldGeneration == newGeneration {
		t.Fatalf("row was not deterministically reused with a new generation: old=%#x new=%#x", oldData.eigen, newData.eigen)
	}
	if data, hit := arena.hitPICArm(newObject, h1LifetimeSelector, arm); hit || data != newData {
		t.Fatalf("stale weak arm result = %#v, hit=%v; want current data and miss", data, hit)
	}
	if _, err := arena.resolveEigen(oldData.eigen); !errors.Is(err, errH1ArenaInvalidRef) {
		t.Fatalf("old weak-arm row resolved after reuse: %v", err)
	}
}

func TestH1CloseBusyIdempotentAndRejectsOldReferences(t *testing.T) {
	arena := newH1Arena()
	object := allocateH1LifetimeObject(t, arena, 1)
	transaction, err := arena.beginSingletonDefinition(
		object, h1LifetimeSelector, h1LifetimeTarget, lookupVisibilityPublic, h1IntegerValue(41), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	before := *arena
	if err := arena.close(); !errors.Is(err, errH1ArenaTransactionRun) {
		t.Fatalf("close during staging returned %v, want %v", err, errH1ArenaTransactionRun)
	}
	if *arena != before {
		t.Fatal("busy close mutated the staged owner")
	}
	if err := arena.abortSingletonDefinition(transaction); err != nil {
		t.Fatalf("staged transaction was not intact after busy close: %v", err)
	}

	data := installH1LifetimeSingleton(t, arena, object, 42)
	if err := arena.pushRoot(h1ObjectValue(object)); err != nil {
		t.Fatal(err)
	}
	if err := arena.close(); err != nil {
		t.Fatalf("close idle arena: %v", err)
	}
	if err := arena.close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if *arena != (h1Arena{closed: true}) {
		t.Fatalf("close did not erase owner state: %#v", arena)
	}
	for name, resolve := range map[string]func() error{
		"object":      func() error { _, err := arena.resolveObject(object); return err },
		"eigen":       func() error { _, err := arena.resolveEigen(data.eigen); return err },
		"environment": func() error { _, err := arena.resolveEnvironment(data.environment); return err },
	} {
		if err := resolve(); !errors.Is(err, errH1ArenaClosed) {
			t.Fatalf("old %s reference after close returned %v, want %v", name, err, errH1ArenaClosed)
		}
	}
}
