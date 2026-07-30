package rubyproof

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func allocateH1SidecarLifetimeObject(t *testing.T, arena *h1SidecarArena, value int64) h1SidecarObjectRef {
	t.Helper()
	reference, err := arena.allocateObject(dispatchClassID(1), shapeID(1), value)
	if err != nil {
		t.Fatalf("allocate H1-prime object: %v", err)
	}
	return reference
}

func installH1SidecarLifetimeSingleton(t *testing.T, arena *h1SidecarArena, object h1SidecarObjectRef, captured h1SidecarValue) h1SidecarCallData {
	t.Helper()
	transaction, err := arena.beginSingletonDefinition(
		object, h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic, captured, 0,
	)
	if err != nil {
		t.Fatalf("begin H1-prime singleton definition: %v", err)
	}
	if err := arena.commitSingletonDefinition(transaction, false); err != nil {
		t.Fatalf("commit H1-prime singleton definition: %v", err)
	}
	data, err := arena.resolveCallData(object, h1SidecarSelector)
	if err != nil {
		t.Fatalf("resolve H1-prime call data: %v", err)
	}
	return data
}

func requireH1SidecarReferenceIndex(t *testing.T, reference uint32, want uint8) uint32 {
	t.Helper()
	index, generation, ok := h1SidecarUnpackReference(reference)
	if !ok || index != want {
		t.Fatalf("reference %#x decoded as index=%d generation=%d valid=%v, want index=%d", reference, index, generation, ok, want)
	}
	return generation
}

func TestH1SidecarObjectEnvironmentCycleLifetime(t *testing.T) {
	arena := newH1SidecarArena()
	object := allocateH1SidecarLifetimeObject(t, arena, 7)
	data := installH1SidecarLifetimeSingleton(t, arena, object, h1SidecarObjectValue(object))
	if err := arena.pushRoot(h1SidecarObjectValue(object)); err != nil {
		t.Fatal(err)
	}

	if err := arena.collect(false); err != nil {
		t.Fatalf("collect rooted object/sidecar/environment cycle: %v", err)
	}
	if got := arena.stats(); got.LiveObjects != 1 || got.LiveEnvironments != 1 {
		t.Fatalf("rooted cycle live counts = %#v, want one object and environment", got)
	}
	objectSlot, err := arena.resolveObject(object)
	if err != nil || !objectSlot.sidecarPresent || objectSlot.sidecar.environment != data.environment {
		t.Fatalf("rooted sidecar = %#v, %v; want published environment %#x", objectSlot, err, data.environment)
	}
	cell, err := arena.loadEnvironment(data.environment, 0)
	if err != nil || cell.kind != h1SidecarValueObject || cell.objectRef() != object {
		t.Fatalf("cycle environment cell = %#v, %v; want object %#x", cell, err, object)
	}

	if err := arena.popRoots(0); err != nil {
		t.Fatal(err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatalf("collect unrooted object/sidecar/environment cycle: %v", err)
	}
	if got := arena.stats(); got.LiveObjects != 0 || got.LiveEnvironments != 0 || got.ObjectReclaims != 1 || got.EnvironmentReclaims != 1 {
		t.Fatalf("unrooted cycle reclamation = %#v, want one object and environment reclaim", got)
	}
	if _, err := arena.resolveObject(object); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("reclaimed cycle object resolved with %v", err)
	}
	if _, err := arena.resolveEnvironment(data.environment); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("reclaimed cycle environment resolved with %v", err)
	}
}

func TestH1SidecarCapturedCellSurvivesDefiningFrameRootRemoval(t *testing.T) {
	arena := newH1SidecarArena()
	object := allocateH1SidecarLifetimeObject(t, arena, 7)
	if err := arena.pushRoot(h1SidecarObjectValue(object)); err != nil {
		t.Fatal(err)
	}
	frameMarker := arena.rootMarker()
	if err := arena.pushRoot(h1SidecarObjectValue(object)); err != nil {
		t.Fatal(err)
	}
	data := installH1SidecarLifetimeSingleton(t, arena, object, h1SidecarIntegerValue(41))
	if err := arena.storeEnvironment(data.environment, 0, h1SidecarIntegerValue(42)); err != nil {
		t.Fatal(err)
	}
	if err := arena.popRoots(frameMarker); err != nil {
		t.Fatalf("remove defining-frame root: %v", err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatalf("collect after defining frame returned: %v", err)
	}
	cell, err := arena.loadEnvironment(data.environment, 0)
	if err != nil || cell.kind != h1SidecarValueInteger || cell.integerValue() != 42 {
		t.Fatalf("captured cell after frame-root removal = %#v, %v; want integer 42", cell, err)
	}
	if got := arena.stats(); got.LiveObjects != 1 || got.LiveEnvironments != 1 {
		t.Fatalf("escaped capture live counts = %#v", got)
	}
}

func TestH1SidecarStaleReferencesAndArmRejectAfterObjectReuse(t *testing.T) {
	arena := newH1SidecarArena()
	oldObject := allocateH1SidecarLifetimeObject(t, arena, 1)
	oldData := installH1SidecarLifetimeSingleton(t, arena, oldObject, h1SidecarIntegerValue(10))
	arm, err := arena.admitPICArm(oldData)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.collect(false); err != nil {
		t.Fatal(err)
	}
	if got := arena.stats(); got.LiveObjects != 0 || got.LiveEnvironments != 0 {
		t.Fatalf("weak arm retained graph: %#v", got)
	}
	if _, err := arena.resolveObject(oldObject); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("stale object resolution returned %v", err)
	}
	if _, err := arena.resolveEnvironment(oldData.environment); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("stale environment resolution returned %v", err)
	}

	newObject := allocateH1SidecarLifetimeObject(t, arena, 2)
	newData := installH1SidecarLifetimeSingleton(t, arena, newObject, h1SidecarIntegerValue(20))
	oldObjectGeneration := requireH1SidecarReferenceIndex(t, uint32(oldObject), 1)
	newObjectGeneration := requireH1SidecarReferenceIndex(t, uint32(newObject), 1)
	oldEnvironmentGeneration := requireH1SidecarReferenceIndex(t, uint32(oldData.environment), 1)
	newEnvironmentGeneration := requireH1SidecarReferenceIndex(t, uint32(newData.environment), 1)
	if newObjectGeneration != oldObjectGeneration+1 || newEnvironmentGeneration != oldEnvironmentGeneration+1 {
		t.Fatalf("reuse generations object %d->%d environment %d->%d, want exact increments", oldObjectGeneration, newObjectGeneration, oldEnvironmentGeneration, newEnvironmentGeneration)
	}
	if data, hit := arena.hitPICArm(newObject, h1SidecarSelector, arm); hit || data != (h1SidecarCallData{}) {
		t.Fatalf("stale object-keyed arm result = %#v, hit=%v; want zero and miss", data, hit)
	}
	if _, err := arena.resolveObject(oldObject); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("old object aliased reused sidecar: %v", err)
	}
	if _, err := arena.resolveEnvironment(oldData.environment); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("old environment aliased reused slot: %v", err)
	}
}

func TestH1SidecarMaximumGenerationRetiresRatherThanWrapping(t *testing.T) {
	arena := newH1SidecarArena()
	arena.objects[0].generation = h1SidecarMaximumGeneration
	arena.environments[0].generation = h1SidecarMaximumGeneration
	oldObject := allocateH1SidecarLifetimeObject(t, arena, 1)
	oldData := installH1SidecarLifetimeSingleton(t, arena, oldObject, h1SidecarIntegerValue(1))
	if err := arena.collect(false); err != nil {
		t.Fatal(err)
	}
	if got := arena.stats(); got.RetiredObjects != 1 || got.RetiredEnvironments != 1 {
		t.Fatalf("retirement stats = %#v, want one retired object and environment", got)
	}
	if !arena.objects[0].retired || !arena.environments[0].retired ||
		arena.objects[0].generation != h1SidecarMaximumGeneration || arena.environments[0].generation != h1SidecarMaximumGeneration {
		t.Fatalf("maximum generations wrapped or remained reusable: object=%#v environment=%#v", arena.objects[0], arena.environments[0])
	}

	newObject := allocateH1SidecarLifetimeObject(t, arena, 2)
	newData := installH1SidecarLifetimeSingleton(t, arena, newObject, h1SidecarIntegerValue(2))
	if generation := requireH1SidecarReferenceIndex(t, uint32(newObject), 2); generation != 1 {
		t.Fatalf("post-retirement object generation = %d, want slot 2 generation 1", generation)
	}
	if generation := requireH1SidecarReferenceIndex(t, uint32(newData.environment), 2); generation != 1 {
		t.Fatalf("post-retirement environment generation = %d, want slot 2 generation 1", generation)
	}
	if _, err := arena.resolveObject(oldObject); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("retired object reference resolved with %v", err)
	}
	if _, err := arena.resolveEnvironment(oldData.environment); !errors.Is(err, errH1SidecarInvalidRef) {
		t.Fatalf("retired environment reference resolved with %v", err)
	}
}

func TestH1SidecarTransactionRollbackRestoresEnvironmentCapacity(t *testing.T) {
	for _, test := range []struct {
		name     string
		rollback func(*h1SidecarArena, h1SidecarSingletonTransaction) error
		want     error
	}{
		{
			name: "canceled commit",
			rollback: func(arena *h1SidecarArena, transaction h1SidecarSingletonTransaction) error {
				return arena.commitSingletonDefinition(transaction, true)
			},
			want: context.Canceled,
		},
		{
			name: "explicit abort",
			rollback: func(arena *h1SidecarArena, transaction h1SidecarSingletonTransaction) error {
				return arena.abortSingletonDefinition(transaction)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			arena := newH1SidecarArena()
			objectRef := allocateH1SidecarLifetimeObject(t, arena, 1)
			transaction, err := arena.beginSingletonDefinition(
				objectRef, h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic, h1SidecarIntegerValue(41), 0,
			)
			if err != nil {
				t.Fatal(err)
			}
			object, err := arena.resolveObject(objectRef)
			if err != nil || object.sidecarPresent || object.sidecar != (h1SidecarRow{}) {
				t.Fatalf("staging published presence or payload: object=%#v err=%v", object, err)
			}
			if err := test.rollback(arena, transaction); !errors.Is(err, test.want) {
				t.Fatalf("rollback returned %v, want %v", err, test.want)
			}
			if object.sidecarPresent || object.sidecar != (h1SidecarRow{}) || arena.transaction.id != 0 {
				t.Fatalf("rollback published or retained state: object=%#v transaction=%#v", object, arena.transaction)
			}
			if got := arena.stats(); got.LiveEnvironments != 0 || got.EnvironmentAllocations != 0 {
				t.Fatalf("rollback changed committed stats: %#v", got)
			}
			if arena.environmentFree != 1 {
				t.Fatalf("rollback environment free head = %d, want 1", arena.environmentFree)
			}
			if _, err := arena.resolveEnvironment(transaction.environment); !errors.Is(err, errH1SidecarInvalidRef) {
				t.Fatalf("rolled-back environment resolved with %v", err)
			}

			next, err := arena.beginSingletonDefinition(
				objectRef, h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic, h1SidecarIntegerValue(42), 0,
			)
			if err != nil {
				t.Fatalf("begin after rollback: %v", err)
			}
			if next.environment != transaction.environment {
				t.Fatalf("environment capacity not deterministically restored: first=%#x next=%#x", transaction.environment, next.environment)
			}
			if err := arena.abortSingletonDefinition(next); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestH1SidecarCollectionRejectsStaging(t *testing.T) {
	arena := newH1SidecarArena()
	object := allocateH1SidecarLifetimeObject(t, arena, 1)
	transaction, err := arena.beginSingletonDefinition(
		object, h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic, h1SidecarIntegerValue(41), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	before := *arena
	if err := arena.collect(false); !errors.Is(err, errH1SidecarTransactionRun) {
		t.Fatalf("collection during staging returned %v, want %v", err, errH1SidecarTransactionRun)
	}
	if *arena != before {
		t.Fatal("rejected collection mutated staging")
	}
	if err := arena.commitSingletonDefinition(transaction, false); err != nil {
		t.Fatalf("transaction was not intact after rejected collection: %v", err)
	}
}

func TestH1SidecarRootObjectAndEnvironmentCapsFailClosed(t *testing.T) {
	t.Run("roots", func(t *testing.T) {
		arena := newH1SidecarArena()
		object := allocateH1SidecarLifetimeObject(t, arena, 1)
		for range h1SidecarRootCapacity {
			if err := arena.pushRoot(h1SidecarObjectValue(object)); err != nil {
				t.Fatal(err)
			}
		}
		before := *arena
		if err := arena.pushRoot(h1SidecarObjectValue(object)); !errors.Is(err, errH1SidecarCapacity) {
			t.Fatalf("root above cap returned %v", err)
		}
		if *arena != before {
			t.Fatal("root cap failure mutated owner")
		}
		if err := arena.popRoots(h1SidecarRootCapacity + 1); !errors.Is(err, errH1SidecarRootOrder) {
			t.Fatalf("invalid root marker returned %v", err)
		}
		if *arena != before {
			t.Fatal("invalid root marker mutated owner")
		}
	})

	t.Run("objects", func(t *testing.T) {
		arena := newH1SidecarArena()
		for index := range h1SidecarObjectCapacity {
			allocateH1SidecarLifetimeObject(t, arena, int64(index))
		}
		before := *arena
		if reference, err := arena.allocateObject(dispatchClassID(1), shapeID(1), 99); reference != 0 || !errors.Is(err, errH1SidecarCapacity) {
			t.Fatalf("object above cap = %#x, %v", reference, err)
		}
		if *arena != before {
			t.Fatal("object cap failure mutated owner")
		}
	})

	t.Run("environments", func(t *testing.T) {
		arena := newH1SidecarArena()
		objects := [...]h1SidecarObjectRef{
			allocateH1SidecarLifetimeObject(t, arena, 1),
			allocateH1SidecarLifetimeObject(t, arena, 2),
			allocateH1SidecarLifetimeObject(t, arena, 3),
		}
		installH1SidecarLifetimeSingleton(t, arena, objects[0], h1SidecarIntegerValue(1))
		installH1SidecarLifetimeSingleton(t, arena, objects[1], h1SidecarIntegerValue(2))
		before := *arena
		transaction, err := arena.beginSingletonDefinition(
			objects[2], h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic, h1SidecarIntegerValue(3), 0,
		)
		if transaction != (h1SidecarSingletonTransaction{}) || !errors.Is(err, errH1SidecarCapacity) {
			t.Fatalf("definition above environment cap = %#v, %v", transaction, err)
		}
		if *arena != before {
			t.Fatal("environment cap failure mutated owner")
		}
		object, resolveErr := arena.resolveObject(objects[2])
		if resolveErr != nil || object.sidecarPresent || object.sidecar != (h1SidecarRow{}) {
			t.Fatalf("environment cap failure published object sidecar: %#v, %v", object, resolveErr)
		}
	})
}

func TestH1SidecarInvalidGraphEdgesAbortBeforeSweep(t *testing.T) {
	for _, test := range []struct {
		name    string
		corrupt func(*h1SidecarArena, h1SidecarObjectRef, h1SidecarCallData) func()
	}{
		{
			name: "sidecar to environment",
			corrupt: func(arena *h1SidecarArena, objectRef h1SidecarObjectRef, data h1SidecarCallData) func() {
				object, _ := arena.resolveObject(objectRef)
				original := object.sidecar.environment
				index, generation, _ := h1SidecarUnpackReference(uint32(original))
				object.sidecar.environment = h1SidecarEnvironmentRef(h1SidecarPackReference(index, generation+1))
				return func() { object.sidecar.environment = original }
			},
		},
		{
			name: "environment to object",
			corrupt: func(arena *h1SidecarArena, _ h1SidecarObjectRef, data h1SidecarCallData) func() {
				environment, _ := arena.resolveEnvironment(data.environment)
				original := environment.cells[0]
				index, generation, _ := h1SidecarUnpackReference(uint32(data.object))
				environment.cells[0] = h1SidecarObjectValue(h1SidecarObjectRef(h1SidecarPackReference(index, generation+1)))
				return func() { environment.cells[0] = original }
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			arena := newH1SidecarArena()
			root := allocateH1SidecarLifetimeObject(t, arena, 1)
			if err := arena.pushRoot(h1SidecarObjectValue(root)); err != nil {
				t.Fatal(err)
			}
			data := installH1SidecarLifetimeSingleton(t, arena, root, h1SidecarIntegerValue(41))
			doomed := allocateH1SidecarLifetimeObject(t, arena, 2)
			restore := test.corrupt(arena, root, data)
			before := arena.stats()

			if err := arena.collect(false); !errors.Is(err, errH1SidecarInvalidRef) {
				t.Fatalf("collect invalid graph returned %v, want %v", err, errH1SidecarInvalidRef)
			}
			after := arena.stats()
			if after.Collections != before.Collections || after.ObjectReclaims != before.ObjectReclaims || after.EnvironmentReclaims != before.EnvironmentReclaims ||
				after.LiveObjects != before.LiveObjects || after.LiveEnvironments != before.LiveEnvironments {
				t.Fatalf("invalid graph exposed partial sweep: before=%#v after=%#v", before, after)
			}
			if arena.markCount != 0 {
				t.Fatalf("failed trace left %d work items", arena.markCount)
			}
			for index := range arena.objects {
				if arena.objects[index].marked {
					t.Fatalf("failed trace left object %d marked", index+1)
				}
			}
			for index := range arena.environments {
				if arena.environments[index].marked {
					t.Fatalf("failed trace left environment %d marked", index+1)
				}
			}
			if _, err := arena.resolveObject(doomed); err != nil {
				t.Fatalf("unrooted object swept after trace failure: %v", err)
			}

			restore()
			if err := arena.collect(false); err != nil {
				t.Fatalf("collect repaired graph: %v", err)
			}
			if _, err := arena.resolveObject(doomed); !errors.Is(err, errH1SidecarInvalidRef) {
				t.Fatalf("repaired collection left unrooted object: %v", err)
			}
		})
	}
}

func TestH1SidecarCloseBusyIdempotentAndRejectsOldReferences(t *testing.T) {
	arena := newH1SidecarArena()
	object := allocateH1SidecarLifetimeObject(t, arena, 1)
	transaction, err := arena.beginSingletonDefinition(
		object, h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic, h1SidecarIntegerValue(41), 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	before := *arena
	if err := arena.close(); !errors.Is(err, errH1SidecarTransactionRun) {
		t.Fatalf("close during staging returned %v, want %v", err, errH1SidecarTransactionRun)
	}
	if *arena != before {
		t.Fatal("busy close mutated staged owner")
	}
	if err := arena.abortSingletonDefinition(transaction); err != nil {
		t.Fatalf("busy close damaged transaction: %v", err)
	}
	data := installH1SidecarLifetimeSingleton(t, arena, object, h1SidecarIntegerValue(42))
	if err := arena.pushRoot(h1SidecarObjectValue(object)); err != nil {
		t.Fatal(err)
	}
	if err := arena.close(); err != nil {
		t.Fatalf("close idle arena: %v", err)
	}
	if err := arena.close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if *arena != (h1SidecarArena{closed: true}) {
		t.Fatalf("close did not erase owner state: %#v", arena)
	}
	if _, err := arena.resolveObject(object); !errors.Is(err, errH1SidecarClosed) {
		t.Fatalf("old object after close returned %v", err)
	}
	if _, err := arena.resolveEnvironment(data.environment); !errors.Is(err, errH1SidecarClosed) {
		t.Fatalf("old environment after close returned %v", err)
	}
	if _, hit := arena.hitPICArm(object, h1SidecarSelector, h1SidecarPICArm{object: object, environment: data.environment}); hit {
		t.Fatal("old object-keyed weak arm hit after close")
	}
}

func TestH1SidecarHasNoIndependentRowIdentityOrLifetime(t *testing.T) {
	rowType := reflect.TypeOf(h1SidecarRow{})
	if rowType.NumField() != 1 || rowType.Field(0).Name != "environment" || rowType.Field(0).Type != reflect.TypeOf(h1SidecarEnvironmentRef(0)) {
		t.Fatalf("sidecar row gained independent identity or state: %v", rowType)
	}
	objectType := reflect.TypeOf(h1SidecarObjectSlot{})
	rowField, ok := objectType.FieldByName("sidecar")
	if !ok || rowField.Type != rowType {
		t.Fatalf("object does not own row by value: field=%#v present=%v", rowField, ok)
	}
	presentField, ok := objectType.FieldByName("sidecarPresent")
	if !ok || presentField.Type.Kind() != reflect.Bool {
		t.Fatalf("object does not own sidecar presence: field=%#v present=%v", presentField, ok)
	}

	arenaType := reflect.TypeOf(h1SidecarArena{})
	for index := 0; index < arenaType.NumField(); index++ {
		field := arenaType.Field(index)
		if field.Type == rowType || (field.Type.Kind() == reflect.Array && field.Type.Elem() == rowType) {
			t.Fatalf("arena gained independent sidecar storage %q", field.Name)
		}
	}
	if field, ok := arenaType.FieldByName("objects"); !ok || field.Type.Elem() != objectType {
		t.Fatalf("arena object storage = %#v, present=%v", field, ok)
	}
	if field, ok := arenaType.FieldByName("roots"); !ok || field.Type.Elem() != reflect.TypeOf(h1SidecarValue{}) {
		t.Fatalf("roots admit a non-value identity: field=%#v present=%v", field, ok)
	}
	if h1SidecarValueEnvironment != 3 || h1SidecarMarkEnvironment != 2 {
		t.Fatalf("sidecar gained a root or mark kind: value-environment=%d mark-environment=%d", h1SidecarValueEnvironment, h1SidecarMarkEnvironment)
	}
	for _, shape := range []reflect.Type{
		reflect.TypeOf(h1SidecarCallData{}),
		reflect.TypeOf(h1SidecarPICArm{}),
	} {
		for index := 0; index < shape.NumField(); index++ {
			field := shape.Field(index)
			if field.Type == rowType || field.Name == "row" || field.Name == "eigen" {
				t.Fatalf("%v exposes independent sidecar identity through %s", shape, field.Name)
			}
		}
	}
}
