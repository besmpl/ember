package h1bgenerated

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

const target48GeneratedSHA256 = "253527270149ecf1a8101257c83bb946ec27be88e999fde902c389d7ba467a6d"

var (
	target48HitChecksum       uint64
	target48HitFinalCell      int64
	target48CollectorChecksum uint64
	target48CollectorState    uint64
)

const (
	target48HitVector        = 64
	target48HitSingletons    = 32
	target48HitWarmupVectors = (1 << 20) / target48HitVector
)

// target48HitSchedule is the frozen primary hit workload. Index zero selects
// the ordinary peer and index one selects the singleton receiver. It keeps
// both warmed PIC arms active at one physical readLabel callsite instead of
// timing only the easiest monomorphic branch.
var target48HitSchedule = [target48HitVector]uint8{
	1, 0, 1, 1, 0, 0, 1, 0,
	0, 1, 0, 1, 1, 0, 0, 1,
	1, 1, 0, 1, 0, 0, 0, 1,
	0, 1, 1, 0, 1, 0, 1, 0,
	0, 0, 1, 1, 0, 1, 1, 0,
	1, 0, 0, 1, 0, 1, 0, 1,
	1, 0, 1, 0, 0, 1, 1, 0,
	0, 1, 0, 0, 1, 1, 0, 1,
}

//go:noinline
func target48CurrentHit(e *Engine, reference objectRef) (int64, error) {
	return e.readLabel(reference)
}

//go:noinline
func target48CurrentCollectionCycle(e *Engine) error {
	if err := e.arena.collect(); err != nil {
		return err
	}
	if err := e.arena.popRoots(1); err != nil {
		return err
	}
	if err := e.arena.collect(); err != nil {
		return err
	}
	if err := e.arena.popRoots(0); err != nil {
		return err
	}
	return e.arena.collect()
}

//go:noinline
func target48NewCurrentHitState() (Engine, [2]objectRef, error) {
	var e Engine
	e.arena.init()
	peer, err := e.arena.allocateObject()
	if err != nil {
		return Engine{}, [2]objectRef{}, err
	}
	receiver, err := e.arena.allocateObject()
	if err != nil {
		return Engine{}, [2]objectRef{}, err
	}
	if _, err = e.readLabel(peer); err != nil {
		return Engine{}, [2]objectRef{}, err
	}
	tx, err := e.arena.beginDefinition(receiver, integerValue(40))
	if err != nil {
		return Engine{}, [2]objectRef{}, err
	}
	if err = e.arena.commitDefinition(tx); err != nil {
		return Engine{}, [2]objectRef{}, err
	}
	if _, err = e.readLabel(receiver); err != nil {
		return Engine{}, [2]objectRef{}, err
	}
	return e, [2]objectRef{peer, receiver}, nil
}

//go:noinline
func target48NewCurrentCollectorState() Engine {
	var e Engine
	e.arena.init()
	peer := objectRef(pack(1, 1))
	receiver := objectRef(pack(2, 1))
	environment := environmentRef(pack(1, 1))
	e.arena.objects[0] = objectSlot{generation: 1, live: true, dispatch: 1, shape: 1}
	e.arena.objects[1] = objectSlot{generation: 1, live: true, dispatch: 1, shape: 1, environment: environment, present: true}
	e.arena.objects[2] = objectSlot{generation: 1, nextFree: 4}
	e.arena.objects[3] = objectSlot{generation: 1}
	e.arena.environments[0] = environmentSlot{generation: 1, live: true, cell: integerValue(40)}
	e.arena.environments[1] = environmentSlot{generation: 1}
	e.arena.objectFree = 3
	e.arena.environmentFree = 2
	e.arena.roots[0] = objectValue(peer)
	e.arena.roots[2] = objectValue(receiver)
	e.arena.rootCount = 3
	e.arena.stats = Stats{ObjectAllocations: 2, EnvironmentAllocations: 1, LiveObjects: 2, LiveEnvironments: 1, Roots: 3}
	e.singletonArm = weakArm{object: receiver, environment: environment}
	return e
}

type target48SelectionReceipt struct {
	values          []int64
	ordinaryValid   bool
	ordinaryHits    uint8
	ordinaryMisses  uint8
	singletonObject uint32
	singletonEnv    uint32
	cell            int64
}

type target48ObjectState struct {
	generation  uint32
	nextFree    uint8
	live        bool
	marked      bool
	retired     bool
	dispatch    uint16
	shape       uint16
	environment uint32
	present     bool
}

type target48EnvironmentState struct {
	generation uint32
	nextFree   uint8
	live       bool
	reserved   bool
	marked     bool
	retired    bool
	cellKind   uint8
	cell       int64
}

type target48OrdinaryState struct {
	dispatch uint16
	shape    uint16
	value    int64
	valid    bool
	hits     uint8
	misses   uint8
}

// target48HitState is a detached, comparable receipt for every owner field
// that readLabel can inspect or mutate. It makes failure-effect and benchmark
// teardown checks independent of either implementation's private types.
type target48HitState struct {
	objects                  [objectCapacity]target48ObjectState
	environments             [environmentCapacity]target48EnvironmentState
	objectFree               uint8
	environmentFree          uint8
	rootCount                uint8
	markCount                uint8
	transactionID            uint32
	transactionObject        uint32
	transactionEnvironment   uint32
	objectAllocations        uint64
	environmentAllocations   uint64
	objectReclaims           uint64
	environmentReclaims      uint64
	collections              uint64
	totalMarkWork            uint64
	lastMarkWork             uint8
	markHighWater            uint8
	liveObjects              uint8
	liveEnvironments         uint8
	statsRoots               uint8
	ordinary                 target48OrdinaryState
	singletonObject          uint32
	singletonEnvironment     uint32
	forceEnvironmentPressure bool
	busy                     bool
	closed                   bool
	poisoned                 bool
}

func target48CurrentHitState(e *Engine) target48HitState {
	r := target48HitState{
		objectFree: e.arena.objectFree, environmentFree: e.arena.environmentFree,
		rootCount: e.arena.rootCount, markCount: e.arena.markCount,
		transactionID: e.arena.transaction.id, transactionObject: uint32(e.arena.transaction.object),
		transactionEnvironment: uint32(e.arena.transaction.environment),
		objectAllocations:      e.arena.stats.ObjectAllocations, environmentAllocations: e.arena.stats.EnvironmentAllocations,
		objectReclaims: e.arena.stats.ObjectReclaims, environmentReclaims: e.arena.stats.EnvironmentReclaims,
		collections: e.arena.stats.Collections, totalMarkWork: e.arena.stats.TotalMarkWork,
		lastMarkWork: e.arena.stats.LastMarkWork, markHighWater: e.arena.stats.MarkHighWater,
		liveObjects: e.arena.stats.LiveObjects, liveEnvironments: e.arena.stats.LiveEnvironments, statsRoots: e.arena.stats.Roots,
		ordinary: target48OrdinaryState{
			dispatch: e.ordinaryArm.dispatch, shape: e.ordinaryArm.shape, value: e.ordinaryArm.value,
			valid: e.ordinaryArm.valid, hits: e.ordinaryArm.hits, misses: e.ordinaryArm.misses,
		},
		singletonObject: uint32(e.singletonArm.object), singletonEnvironment: uint32(e.singletonArm.environment),
		forceEnvironmentPressure: e.forceEnvironmentPressure, busy: e.busy, closed: e.closed, poisoned: e.poisoned,
	}
	for i, slot := range e.arena.objects {
		r.objects[i] = target48ObjectState{
			generation: slot.generation, nextFree: slot.nextFree, live: slot.live, marked: slot.marked,
			retired: slot.retired, dispatch: slot.dispatch, shape: slot.shape,
			environment: uint32(slot.environment), present: slot.present,
		}
	}
	for i, slot := range e.arena.environments {
		r.environments[i] = target48EnvironmentState{
			generation: slot.generation, nextFree: slot.nextFree, live: slot.live, reserved: slot.reserved,
			marked: slot.marked, retired: slot.retired, cellKind: uint8(slot.cell.kind), cell: slot.cell.integer(),
		}
	}
	return r
}

func target48EquivalentHitState(e *target48Engine) target48HitState {
	r := target48HitState{
		objectFree: e.arena.objectFree, environmentFree: e.arena.environmentFree,
		rootCount: e.arena.rootCount, markCount: e.arena.markCount,
		transactionID: e.arena.transaction.id, transactionObject: uint32(e.arena.transaction.object),
		transactionEnvironment: uint32(e.arena.transaction.environment),
		objectAllocations:      e.arena.stats.objectAllocations, environmentAllocations: e.arena.stats.environmentAllocations,
		objectReclaims: e.arena.stats.objectReclaims, environmentReclaims: e.arena.stats.environmentReclaims,
		collections: e.arena.stats.collections, totalMarkWork: e.arena.stats.totalMarkWork,
		lastMarkWork: e.arena.stats.lastMarkWork, markHighWater: e.arena.stats.markHighWater,
		liveObjects: e.arena.stats.liveObjects, liveEnvironments: e.arena.stats.liveEnvironments, statsRoots: e.arena.stats.roots,
		ordinary: target48OrdinaryState{
			dispatch: e.ordinaryArm.dispatch, shape: e.ordinaryArm.shape, value: e.ordinaryArm.value,
			valid: e.ordinaryArm.valid, hits: e.ordinaryArm.hits, misses: e.ordinaryArm.misses,
		},
		singletonObject: uint32(e.singletonArm.object), singletonEnvironment: uint32(e.singletonArm.environment),
		forceEnvironmentPressure: e.forceEnvironmentPressure, busy: e.busy, closed: e.closed, poisoned: e.poisoned,
	}
	for i, slot := range e.arena.objects {
		r.objects[i] = target48ObjectState{
			generation: slot.generation, nextFree: slot.nextFree, live: slot.live, marked: slot.marked,
			retired: slot.retired, dispatch: slot.dispatch, shape: slot.shape,
			environment: uint32(slot.environment), present: slot.present,
		}
	}
	for i, slot := range e.arena.environments {
		r.environments[i] = target48EnvironmentState{
			generation: slot.generation, nextFree: slot.nextFree, live: slot.live, reserved: slot.reserved,
			marked: slot.marked, retired: slot.retired, cellKind: uint8(slot.cell.kind), cell: slot.cell.target48Integer(),
		}
	}
	return r
}

func target48CurrentSelection(t testing.TB) target48SelectionReceipt {
	t.Helper()
	var e Engine
	e.arena.init()
	peer, err := e.arena.allocateObject()
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := e.arena.allocateObject()
	if err != nil {
		t.Fatal(err)
	}
	values := make([]int64, 0, 7)
	for _, reference := range []objectRef{receiver, receiver, peer} {
		v, callErr := e.readLabel(reference)
		if callErr != nil {
			t.Fatal(callErr)
		}
		values = append(values, v)
	}
	tx, err := e.arena.beginDefinition(receiver, integerValue(40))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.arena.commitDefinition(tx); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []objectRef{receiver, receiver, peer, peer} {
		v, callErr := e.readLabel(reference)
		if callErr != nil {
			t.Fatal(callErr)
		}
		values = append(values, v)
	}
	cell, err := e.arena.environmentCell(e.singletonArm.environment)
	if err != nil {
		t.Fatal(err)
	}
	return target48SelectionReceipt{
		values: values, ordinaryValid: e.ordinaryArm.valid,
		ordinaryHits: e.ordinaryArm.hits, ordinaryMisses: e.ordinaryArm.misses,
		singletonObject: uint32(e.singletonArm.object), singletonEnv: uint32(e.singletonArm.environment), cell: cell,
	}
}

func target48EquivalentSelection(t testing.TB) target48SelectionReceipt {
	t.Helper()
	var e target48Engine
	e.arena.target48Init()
	peer, err := e.arena.target48AllocateObject()
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := e.arena.target48AllocateObject()
	if err != nil {
		t.Fatal(err)
	}
	values := make([]int64, 0, 7)
	for _, reference := range []target48ObjectRef{receiver, receiver, peer} {
		v, callErr := target48ReadLabel(&e, reference)
		if callErr != nil {
			t.Fatal(callErr)
		}
		values = append(values, v)
	}
	if err = e.arena.target48Install(receiver, 40); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []target48ObjectRef{receiver, receiver, peer, peer} {
		v, callErr := target48ReadLabel(&e, reference)
		if callErr != nil {
			t.Fatal(callErr)
		}
		values = append(values, v)
	}
	cell, err := e.arena.target48EnvironmentCell(e.singletonArm.environment)
	if err != nil {
		t.Fatal(err)
	}
	return target48SelectionReceipt{
		values: values, ordinaryValid: e.ordinaryArm.valid,
		ordinaryHits: e.ordinaryArm.hits, ordinaryMisses: e.ordinaryArm.misses,
		singletonObject: uint32(e.singletonArm.object), singletonEnv: uint32(e.singletonArm.environment), cell: cell,
	}
}

func TestTarget48HitComparatorSelectionAndWarmedParity(t *testing.T) {
	singletons := 0
	for _, lane := range target48HitSchedule {
		if lane > 1 {
			t.Fatalf("primary schedule lane = %d, want 0 or 1", lane)
		}
		singletons += int(lane)
	}
	if singletons != target48HitSingletons || len(target48HitSchedule)-singletons != target48HitSingletons {
		t.Fatalf("primary schedule ordinary/singleton = %d/%d", len(target48HitSchedule)-singletons, singletons)
	}

	current, equivalent := target48CurrentSelection(t), target48EquivalentSelection(t)
	if !reflect.DeepEqual(current, equivalent) {
		t.Fatalf("selection receipts differ:\ncurrent   %#v\nequivalent %#v", current, equivalent)
	}
	if !reflect.DeepEqual(current.values, []int64{7, 7, 7, 41, 42, 7, 7}) || !current.ordinaryValid || current.ordinaryHits != 4 || current.ordinaryMisses != 1 || current.cell != 42 {
		t.Fatalf("unexpected selection receipt: %#v", current)
	}

	currentEngine, currentRefs, err := target48NewCurrentHitState()
	if err != nil {
		t.Fatal(err)
	}
	equivalentEngine, equivalentRefs, err := target48NewHitState()
	if err != nil {
		t.Fatal(err)
	}
	for vector := 0; vector < 257; vector++ {
		for call, lane := range target48HitSchedule {
			gotCurrent, currentErr := target48CurrentHit(&currentEngine, currentRefs[lane])
			gotEquivalent, equivalentErr := target48EquivalentHit(&equivalentEngine, equivalentRefs[lane])
			if currentErr != nil || equivalentErr != nil || gotCurrent != gotEquivalent {
				t.Fatalf("vector/call %d/%d: current=(%d,%v) equivalent=(%d,%v)", vector, call, gotCurrent, currentErr, gotEquivalent, equivalentErr)
			}
		}
	}
	currentCell, err := currentEngine.arena.environmentCell(currentEngine.singletonArm.environment)
	if err != nil {
		t.Fatal(err)
	}
	equivalentCell, err := equivalentEngine.arena.target48EnvironmentCell(equivalentEngine.singletonArm.environment)
	if err != nil {
		t.Fatal(err)
	}
	wantCell := int64(41 + 257*target48HitSingletons)
	if currentCell != wantCell || equivalentCell != currentCell || uint32(currentEngine.singletonArm.object) != uint32(equivalentEngine.singletonArm.object) || uint32(currentEngine.singletonArm.environment) != uint32(equivalentEngine.singletonArm.environment) {
		t.Fatalf("final warmed states differ: current cell/ref=%d/%#v equivalent=%d/%#v", currentCell, currentEngine.singletonArm, equivalentCell, equivalentEngine.singletonArm)
	}
	wantOrdinaryHits := uint8((257 * target48HitSingletons) % 256)
	if currentEngine.ordinaryArm.hits != wantOrdinaryHits || equivalentEngine.ordinaryArm.hits != wantOrdinaryHits || currentEngine.ordinaryArm.misses != 1 || equivalentEngine.ordinaryArm.misses != 1 {
		t.Fatalf("final ordinary PIC hits/misses current=%d/%d equivalent=%d/%d", currentEngine.ordinaryArm.hits, currentEngine.ordinaryArm.misses, equivalentEngine.ordinaryArm.hits, equivalentEngine.ordinaryArm.misses)
	}
}

func TestTarget48HitComparatorRepresentativeInvalidStates(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Engine, *target48Engine, *objectRef, *target48ObjectRef)
		ok       bool
		noEffect bool
	}{
		{name: "zero receiver", noEffect: true, mutate: func(_ *Engine, _ *target48Engine, c *objectRef, e *target48ObjectRef) { *c, *e = 0, 0 }},
		{name: "out of range receiver", noEffect: true, mutate: func(_ *Engine, _ *target48Engine, c *objectRef, e *target48ObjectRef) {
			*c = objectRef(pack(objectCapacity+1, 1))
			*e = target48ObjectRef(target48Pack(target48ObjectCapacity+1, 1))
		}},
		{name: "stale receiver generation", noEffect: true, mutate: func(_ *Engine, _ *target48Engine, c *objectRef, e *target48ObjectRef) {
			*c = objectRef(pack(2, 2))
			*e = target48ObjectRef(target48Pack(2, 2))
		}},
		{name: "dead receiver", noEffect: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.arena.objects[1].live = false
			e.arena.objects[1].live = false
		}},
		{name: "retired receiver", noEffect: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.arena.objects[1].retired = true
			e.arena.objects[1].retired = true
		}},
		{name: "missing receiver environment", mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.arena.objects[1].environment = 0
			e.arena.objects[1].environment = 0
		}},
		{name: "stale environment generation", noEffect: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			current := environmentRef(pack(1, 2))
			equivalent := target48EnvironmentRef(target48Pack(1, 2))
			c.arena.objects[1].environment, c.singletonArm.environment = current, current
			e.arena.objects[1].environment, e.singletonArm.environment = equivalent, equivalent
		}},
		{name: "dead environment", noEffect: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.arena.environments[0].live = false
			e.arena.environments[0].live = false
		}},
		{name: "reserved environment", noEffect: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.arena.environments[0].reserved = true
			e.arena.environments[0].reserved = true
		}},
		{name: "retired environment", noEffect: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.arena.environments[0].retired = true
			e.arena.environments[0].retired = true
		}},
		{name: "non integer cell", noEffect: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.arena.environments[0].cell = value{}
			e.arena.environments[0].cell = target48Value{}
		}},
		{name: "ordinary inconsistent environment", mutate: func(c *Engine, e *target48Engine, cRef *objectRef, eRef *target48ObjectRef) {
			*cRef, *eRef = objectRef(pack(1, 1)), target48ObjectRef(target48Pack(1, 1))
			c.arena.objects[0].environment = c.singletonArm.environment
			e.arena.objects[0].environment = e.singletonArm.environment
		}},
		{name: "weak arm repair", ok: true, mutate: func(c *Engine, e *target48Engine, _ *objectRef, _ *target48ObjectRef) {
			c.singletonArm = weakArm{}
			e.singletonArm = target48WeakArm{}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current, currentRefs, err := target48NewCurrentHitState()
			if err != nil {
				t.Fatal(err)
			}
			equivalent, equivalentRefs, err := target48NewHitState()
			if err != nil {
				t.Fatal(err)
			}
			currentRef, equivalentRef := currentRefs[1], equivalentRefs[1]
			test.mutate(&current, &equivalent, &currentRef, &equivalentRef)
			currentBefore, equivalentBefore := target48CurrentHitState(&current), target48EquivalentHitState(&equivalent)
			if currentBefore != equivalentBefore {
				t.Fatalf("pre-call states differ:\ncurrent   %#v\nequivalent %#v", currentBefore, equivalentBefore)
			}
			currentValue, currentErr := current.readLabel(currentRef)
			equivalentValue, equivalentErr := target48ReadLabel(&equivalent, equivalentRef)
			if (currentErr == nil) != (equivalentErr == nil) || (currentErr == nil && currentValue != equivalentValue) || (currentErr == nil) != test.ok {
				t.Fatalf("current=(%d,%v) equivalent=(%d,%v) want success=%v", currentValue, currentErr, equivalentValue, equivalentErr, test.ok)
			}
			if !test.ok && (currentErr != errInvalidRef || equivalentErr != target48ErrInvalidRef) {
				t.Fatalf("error classes current=%T/%v equivalent=%T/%v, want invalid-reference classes", currentErr, currentErr, equivalentErr, equivalentErr)
			}
			currentAfter, equivalentAfter := target48CurrentHitState(&current), target48EquivalentHitState(&equivalent)
			if currentAfter != equivalentAfter {
				t.Fatalf("post-call states differ:\ncurrent   %#v\nequivalent %#v", currentAfter, equivalentAfter)
			}
			if test.noEffect && (currentAfter != currentBefore || equivalentAfter != equivalentBefore) {
				t.Fatalf("failed call changed state:\nbefore %#v\nafter  %#v", currentBefore, currentAfter)
			}
		})
	}
}

type target48CollectorReceipt struct {
	objectAllocations, environmentAllocations uint64
	objectReclaims, environmentReclaims       uint64
	collections, totalMarkWork                uint64
	lastMarkWork, markHighWater               uint8
	liveObjects, liveEnvironments, roots      uint8
	objectFree, environmentFree               uint8
	objectGenerations                         [4]uint32
	environmentGenerations                    [2]uint32
	objectNext                                [4]uint8
	environmentNext                           [2]uint8
	markCount                                 uint8
	allMarksClear                             bool
}

func target48CurrentCollectorReceipt(e *Engine) target48CollectorReceipt {
	r := target48CollectorReceipt{
		objectAllocations: e.arena.stats.ObjectAllocations, environmentAllocations: e.arena.stats.EnvironmentAllocations,
		objectReclaims: e.arena.stats.ObjectReclaims, environmentReclaims: e.arena.stats.EnvironmentReclaims,
		collections: e.arena.stats.Collections, totalMarkWork: e.arena.stats.TotalMarkWork,
		lastMarkWork: e.arena.stats.LastMarkWork, markHighWater: e.arena.stats.MarkHighWater,
		liveObjects: e.arena.stats.LiveObjects, liveEnvironments: e.arena.stats.LiveEnvironments, roots: e.arena.stats.Roots,
		objectFree: e.arena.objectFree, environmentFree: e.arena.environmentFree, markCount: e.arena.markCount, allMarksClear: true,
	}
	for i := range e.arena.objects {
		r.objectGenerations[i], r.objectNext[i] = e.arena.objects[i].generation, e.arena.objects[i].nextFree
		r.allMarksClear = r.allMarksClear && !e.arena.objects[i].marked
	}
	for i := range e.arena.environments {
		r.environmentGenerations[i], r.environmentNext[i] = e.arena.environments[i].generation, e.arena.environments[i].nextFree
		r.allMarksClear = r.allMarksClear && !e.arena.environments[i].marked
	}
	for _, item := range e.arena.marks {
		r.allMarksClear = r.allMarksClear && item == (markItem{})
	}
	return r
}

func target48EquivalentCollectorReceipt(e *target48Engine) target48CollectorReceipt {
	r := target48CollectorReceipt{
		objectAllocations: e.arena.stats.objectAllocations, environmentAllocations: e.arena.stats.environmentAllocations,
		objectReclaims: e.arena.stats.objectReclaims, environmentReclaims: e.arena.stats.environmentReclaims,
		collections: e.arena.stats.collections, totalMarkWork: e.arena.stats.totalMarkWork,
		lastMarkWork: e.arena.stats.lastMarkWork, markHighWater: e.arena.stats.markHighWater,
		liveObjects: e.arena.stats.liveObjects, liveEnvironments: e.arena.stats.liveEnvironments, roots: e.arena.stats.roots,
		objectFree: e.arena.objectFree, environmentFree: e.arena.environmentFree, markCount: e.arena.markCount, allMarksClear: true,
	}
	for i := range e.arena.objects {
		r.objectGenerations[i], r.objectNext[i] = e.arena.objects[i].generation, e.arena.objects[i].nextFree
		r.allMarksClear = r.allMarksClear && !e.arena.objects[i].marked
	}
	for i := range e.arena.environments {
		r.environmentGenerations[i], r.environmentNext[i] = e.arena.environments[i].generation, e.arena.environments[i].nextFree
		r.allMarksClear = r.allMarksClear && !e.arena.environments[i].marked
	}
	for _, item := range e.arena.marks {
		r.allMarksClear = r.allMarksClear && item == (target48MarkItem{})
	}
	return r
}

var target48FinalCollectorReceipt = target48CollectorReceipt{
	objectAllocations: 2, environmentAllocations: 1,
	objectReclaims: 2, environmentReclaims: 1,
	collections: 3, totalMarkWork: 4, lastMarkWork: 0, markHighWater: 3,
	liveObjects: 0, liveEnvironments: 0, roots: 0,
	objectFree: 1, environmentFree: 1,
	objectGenerations:      [4]uint32{2, 2, 1, 1},
	environmentGenerations: [2]uint32{2, 1},
	objectNext:             [4]uint8{2, 3, 4, 0},
	environmentNext:        [2]uint8{2, 0},
	markCount:              0,
	allMarksClear:          true,
}

func TestTarget48CollectorComparatorThreePhaseParity(t *testing.T) {
	current, equivalent := target48NewCurrentCollectorState(), target48NewCollectorState()
	currentPhases := make([]target48CollectorReceipt, 0, 3)
	equivalentPhases := make([]target48CollectorReceipt, 0, 3)
	if err := current.arena.collect(); err != nil {
		t.Fatal(err)
	}
	if err := equivalent.arena.target48Collect(); err != nil {
		t.Fatal(err)
	}
	currentPhases = append(currentPhases, target48CurrentCollectorReceipt(&current))
	equivalentPhases = append(equivalentPhases, target48EquivalentCollectorReceipt(&equivalent))
	if err := current.arena.popRoots(1); err != nil {
		t.Fatal(err)
	}
	if err := equivalent.arena.target48PopRoots(1); err != nil {
		t.Fatal(err)
	}
	if err := current.arena.collect(); err != nil {
		t.Fatal(err)
	}
	if err := equivalent.arena.target48Collect(); err != nil {
		t.Fatal(err)
	}
	currentPhases = append(currentPhases, target48CurrentCollectorReceipt(&current))
	equivalentPhases = append(equivalentPhases, target48EquivalentCollectorReceipt(&equivalent))
	if err := current.arena.popRoots(0); err != nil {
		t.Fatal(err)
	}
	if err := equivalent.arena.target48PopRoots(0); err != nil {
		t.Fatal(err)
	}
	if err := current.arena.collect(); err != nil {
		t.Fatal(err)
	}
	if err := equivalent.arena.target48Collect(); err != nil {
		t.Fatal(err)
	}
	currentPhases = append(currentPhases, target48CurrentCollectorReceipt(&current))
	equivalentPhases = append(equivalentPhases, target48EquivalentCollectorReceipt(&equivalent))
	if !reflect.DeepEqual(currentPhases, equivalentPhases) {
		t.Fatalf("collector phases differ:\ncurrent   %#v\nequivalent %#v", currentPhases, equivalentPhases)
	}
	if got := currentPhases[0]; got.collections != 1 || got.totalMarkWork != 3 || got.objectReclaims != 0 || got.environmentReclaims != 0 || got.liveObjects != 2 || got.liveEnvironments != 1 || got.roots != 3 {
		t.Fatalf("helper phase = %#v", got)
	}
	if got := currentPhases[1]; got.collections != 2 || got.totalMarkWork != 4 || got.objectReclaims != 1 || got.environmentReclaims != 1 || got.liveObjects != 1 || got.liveEnvironments != 0 || got.roots != 1 {
		t.Fatalf("receiver-drop phase = %#v", got)
	}
	if got := currentPhases[2]; got != target48FinalCollectorReceipt {
		t.Fatalf("final phase = %#v, want %#v", got, target48FinalCollectorReceipt)
	}
}

func TestTarget48ComparatorsAllocateNoGoHeap(t *testing.T) {
	currentHit, currentRefs, err := target48NewCurrentHitState()
	if err != nil {
		t.Fatal(err)
	}
	equivalentHit, equivalentRefs, err := target48NewHitState()
	if err != nil {
		t.Fatal(err)
	}
	var currentValue, equivalentValue int64
	currentAllocs := testing.AllocsPerRun(1000, func() {
		for _, lane := range target48HitSchedule {
			currentValue, err = target48CurrentHit(&currentHit, currentRefs[lane])
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	equivalentAllocs := testing.AllocsPerRun(1000, func() {
		for _, lane := range target48HitSchedule {
			equivalentValue, err = target48EquivalentHit(&equivalentHit, equivalentRefs[lane])
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if currentAllocs != 0 || equivalentAllocs != 0 || currentValue != equivalentValue {
		t.Fatalf("hit allocations/value current=%g/%d equivalent=%g/%d", currentAllocs, currentValue, equivalentAllocs, equivalentValue)
	}

	currentSeed, equivalentSeed := target48NewCurrentCollectorState(), target48NewCollectorState()
	var currentCollector Engine
	var currentEquivalentCollector target48Engine
	currentCollectorAllocs := testing.AllocsPerRun(1000, func() {
		currentCollector = currentSeed
		err = target48CurrentCollectionCycle(&currentCollector)
	})
	if err != nil {
		t.Fatal(err)
	}
	equivalentCollectorAllocs := testing.AllocsPerRun(1000, func() {
		currentEquivalentCollector = equivalentSeed
		err = target48EquivalentCollectionCycle(&currentEquivalentCollector)
	})
	if err != nil {
		t.Fatal(err)
	}
	if currentCollectorAllocs != 0 || equivalentCollectorAllocs != 0 || !reflect.DeepEqual(target48CurrentCollectorReceipt(&currentCollector), target48EquivalentCollectorReceipt(&currentEquivalentCollector)) {
		t.Fatalf("collector allocations current=%g equivalent=%g", currentCollectorAllocs, equivalentCollectorAllocs)
	}
}

func TestTarget48EquivalentLayoutAndIsolation(t *testing.T) {
	pairs := [][2]reflect.Type{
		{reflect.TypeOf(value{}), reflect.TypeOf(target48Value{})},
		{reflect.TypeOf(objectSlot{}), reflect.TypeOf(target48ObjectSlot{})},
		{reflect.TypeOf(environmentSlot{}), reflect.TypeOf(target48EnvironmentSlot{})},
		{reflect.TypeOf(markItem{}), reflect.TypeOf(target48MarkItem{})},
		{reflect.TypeOf(transaction{}), reflect.TypeOf(target48Transaction{})},
		{reflect.TypeOf(weakArm{}), reflect.TypeOf(target48WeakArm{})},
		{reflect.TypeOf(ordinaryPICArm{}), reflect.TypeOf(target48OrdinaryArm{})},
		{reflect.TypeOf(arena{}), reflect.TypeOf(target48Arena{})},
		{reflect.TypeOf(Engine{}), reflect.TypeOf(target48Engine{})},
	}
	for _, pair := range pairs {
		current, equivalent := pair[0], pair[1]
		if current.Size() != equivalent.Size() || current.Align() != equivalent.Align() || current.NumField() != equivalent.NumField() {
			t.Fatalf("layout %s/%s size=%d/%d align=%d/%d fields=%d/%d", current, equivalent, current.Size(), equivalent.Size(), current.Align(), equivalent.Align(), current.NumField(), equivalent.NumField())
		}
		for i := 0; i < current.NumField(); i++ {
			cf, ef := current.Field(i), equivalent.Field(i)
			if cf.Offset != ef.Offset || cf.Type.Size() != ef.Type.Size() || cf.Type.Align() != ef.Type.Align() || cf.Type.Kind() != ef.Type.Kind() {
				t.Fatalf("layout %s field %d differs: offset=%d/%d size=%d/%d align=%d/%d kind=%s/%s", current, i, cf.Offset, ef.Offset, cf.Type.Size(), ef.Type.Size(), cf.Type.Align(), ef.Type.Align(), cf.Type.Kind(), ef.Type.Kind())
			}
		}
	}

	path := filepath.Join("target48_equivalent_test.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Imports) != 0 {
		t.Fatalf("equivalent comparator imports %d packages", len(file.Imports))
	}
	generatedSource, err := os.ReadFile("ruby_h1b_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(generatedSource)); got != target48GeneratedSHA256 {
		t.Fatalf("generated source SHA-256 = %s, want %s", got, target48GeneratedSHA256)
	}
	generatedFile, err := parser.ParseFile(token.NewFileSet(), "ruby_h1b_generated.go", generatedSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	productionNames := make(map[string]bool, len(generatedFile.Scope.Objects))
	for name := range generatedFile.Scope.Objects {
		productionNames[name] = true
	}
	comparatorNames := make(map[string]bool, len(file.Scope.Objects))
	for name := range file.Scope.Objects {
		comparatorNames[name] = true
	}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			comparatorNames[function.Name.Name] = true
		}
	}
	selectorNames := make(map[token.Pos]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			selectorNames[selector.Sel.Pos()] = true
		}
		return true
	})
	ast.Inspect(file, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Obj == nil && !selectorNames[ident.Pos()] && productionNames[ident.Name] && !comparatorNames[ident.Name] {
			t.Errorf("equivalent comparator references production identifier %q", ident.Name)
		}
		return true
	})
	currentPC := reflect.ValueOf(target48CurrentHit).Pointer()
	equivalentPC := reflect.ValueOf(target48EquivalentHit).Pointer()
	if currentPC == 0 || equivalentPC == 0 || currentPC == equivalentPC || runtime.FuncForPC(currentPC) == nil || runtime.FuncForPC(equivalentPC) == nil {
		t.Fatalf("hit wrapper symbols are not distinct: current=%#x equivalent=%#x", currentPC, equivalentPC)
	}
}

func target48ExpectedHitChecksum(start, calls int64) uint64 {
	// Divide the even factor before multiplying. The final multiplication and
	// additions intentionally use uint64 modular arithmetic, matching the sink.
	a, c := uint64(calls), uint64(calls+1)
	if a&1 == 0 {
		a /= 2
	} else {
		c /= 2
	}
	return uint64(calls)*7 + uint64(start)*uint64(calls) + a*c
}

func target48ValidateHitFinal(b *testing.B, state target48HitState, start int64, checksum uint64) {
	b.Helper()
	const maxInt64 = int64(1<<63 - 1)
	if b.N < 0 || int64(b.N) > (maxInt64-start)/target48HitSingletons {
		b.Fatalf("benchmark iteration count %d overflows the exact hit receipt", b.N)
	}
	calls := int64(b.N) * target48HitSingletons
	final := start + calls
	peer := uint32(pack(1, 1))
	receiver := uint32(pack(2, 1))
	environment := uint32(pack(1, 1))
	wantStart := int64(41 + target48HitWarmupVectors*target48HitSingletons)
	if start != wantStart {
		b.Fatalf("post-warmup cell = %d, want %d", start, wantStart)
	}
	want := target48HitState{
		objects: [objectCapacity]target48ObjectState{
			{generation: 1, live: true, dispatch: 1, shape: 1},
			{generation: 1, live: true, dispatch: 1, shape: 1, environment: environment, present: true},
			{generation: 1, nextFree: 4},
			{generation: 1},
		},
		environments: [environmentCapacity]target48EnvironmentState{
			{generation: 1, live: true, cellKind: uint8(valueInteger), cell: final},
			{generation: 1},
		},
		objectFree: 3, environmentFree: 2,
		objectAllocations: 2, environmentAllocations: 1, liveObjects: 2, liveEnvironments: 1,
		ordinary: target48OrdinaryState{
			dispatch: 1, shape: 1, value: 7, valid: true,
			hits: uint8(calls), misses: 1,
		},
		singletonObject: receiver, singletonEnvironment: environment,
	}
	if state != want {
		b.Fatalf("final hit state differs:\ngot  %#v\nwant %#v", state, want)
	}
	if peer == 0 || state.singletonObject == peer {
		b.Fatalf("peer/singleton identity collapsed: peer=%#x singleton=%#x", peer, state.singletonObject)
	}
	if wantChecksum := target48ExpectedHitChecksum(start, calls); checksum != wantChecksum {
		b.Fatalf("hit checksum = %#x, want %#x", checksum, wantChecksum)
	}
}

func target48BenchmarkHitCurrent(b *testing.B) {
	e, references, err := target48NewCurrentHitState()
	if err != nil {
		b.Fatal(err)
	}
	runtime.GC()
	oldGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGCPercent)
	for range target48HitWarmupVectors {
		for _, lane := range target48HitSchedule {
			if _, err = target48CurrentHit(&e, references[lane]); err != nil {
				b.Fatal(err)
			}
		}
	}
	start, err := e.arena.environmentCell(e.singletonArm.environment)
	if err != nil {
		b.Fatal(err)
	}
	var checksum uint64
	b.ReportAllocs()
	for b.Loop() {
		for _, lane := range target48HitSchedule {
			value, callErr := target48CurrentHit(&e, references[lane])
			if callErr != nil {
				b.Fatal(callErr)
			}
			checksum += uint64(value)
		}
	}
	final, err := e.arena.environmentCell(e.singletonArm.environment)
	if err != nil {
		b.Fatal(err)
	}
	want := start + int64(b.N)*target48HitSingletons
	if final != want {
		b.Fatalf("final cell = %d, want %d", final, want)
	}
	target48ValidateHitFinal(b, target48CurrentHitState(&e), start, checksum)
	target48HitChecksum, target48HitFinalCell = checksum, final
	runtime.KeepAlive(&e)
	b.ReportMetric(target48HitVector, "calls/op")
	b.ReportMetric(target48HitSingletons, "singleton-calls/op")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*target48HitVector), "ns/semantic-call")
}

func target48BenchmarkHitEquivalent(b *testing.B) {
	e, references, err := target48NewHitState()
	if err != nil {
		b.Fatal(err)
	}
	runtime.GC()
	oldGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGCPercent)
	for range target48HitWarmupVectors {
		for _, lane := range target48HitSchedule {
			if _, err = target48EquivalentHit(&e, references[lane]); err != nil {
				b.Fatal(err)
			}
		}
	}
	start, err := e.arena.target48EnvironmentCell(e.singletonArm.environment)
	if err != nil {
		b.Fatal(err)
	}
	var checksum uint64
	b.ReportAllocs()
	for b.Loop() {
		for _, lane := range target48HitSchedule {
			value, callErr := target48EquivalentHit(&e, references[lane])
			if callErr != nil {
				b.Fatal(callErr)
			}
			checksum += uint64(value)
		}
	}
	final, err := e.arena.target48EnvironmentCell(e.singletonArm.environment)
	if err != nil {
		b.Fatal(err)
	}
	want := start + int64(b.N)*target48HitSingletons
	if final != want {
		b.Fatalf("final cell = %d, want %d", final, want)
	}
	target48ValidateHitFinal(b, target48EquivalentHitState(&e), start, checksum)
	target48HitChecksum, target48HitFinalCell = checksum, final
	runtime.KeepAlive(&e)
	b.ReportMetric(target48HitVector, "calls/op")
	b.ReportMetric(target48HitSingletons, "singleton-calls/op")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*target48HitVector), "ns/semantic-call")
}

const target48CollectorBankSize = 65536

func target48BenchmarkCollectorCurrent(b *testing.B) {
	seed := target48NewCurrentCollectorState()
	var bank [target48CollectorBankSize]Engine
	var checksum uint64
	for i := range bank {
		bank[i] = seed
	}
	runtime.GC()
	oldGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGCPercent)
	for range 2 {
		for i := range bank {
			if err := target48CurrentCollectionCycle(&bank[i]); err != nil {
				b.Fatal(err)
			}
		}
		for i := range bank {
			bank[i] = seed
		}
	}
	b.ReportAllocs()
	index := 0
	var lastState uint64
	for b.Loop() {
		if err := target48CurrentCollectionCycle(&bank[index]); err != nil {
			b.Fatal(err)
		}
		index++
		if index == len(bank) {
			b.StopTimer()
			for i := range bank {
				receipt := target48CurrentCollectorReceipt(&bank[i])
				if receipt != target48FinalCollectorReceipt {
					b.Fatalf("collector receipt = %#v, want %#v", receipt, target48FinalCollectorReceipt)
				}
				checksum += receipt.collections + receipt.totalMarkWork + receipt.objectReclaims + receipt.environmentReclaims
			}
			lastState = uint64(bank[len(bank)-1].arena.objectFree)<<8 | uint64(bank[len(bank)-1].arena.environmentFree)
			for i := range bank {
				bank[i] = seed
			}
			index = 0
			b.StartTimer()
		}
	}
	for i := 0; i < index; i++ {
		receipt := target48CurrentCollectorReceipt(&bank[i])
		if receipt != target48FinalCollectorReceipt {
			b.Fatalf("collector receipt = %#v, want %#v", receipt, target48FinalCollectorReceipt)
		}
		checksum += receipt.collections + receipt.totalMarkWork + receipt.objectReclaims + receipt.environmentReclaims
		lastState = uint64(bank[i].arena.objectFree)<<8 | uint64(bank[i].arena.environmentFree)
	}
	target48CollectorChecksum = checksum
	target48CollectorState = lastState
	if want := uint64(b.N) * 10; checksum != want {
		b.Fatalf("collector checksum = %d, want %d", checksum, want)
	}
	runtime.KeepAlive(&bank)
	b.ReportMetric(3, "collections/op")
	b.ReportMetric(target48CollectorBankSize, "arenas/batch")
	b.ReportMetric(float64(target48CollectorBankSize)*float64(reflect.TypeOf(Engine{}).Size()), "bank-bytes")
	b.ReportMetric(4, "roots-examined/op")
	b.ReportMetric(1, "edges-examined/op")
	b.ReportMetric(4, "successful-marks/op")
	b.ReportMetric(0, "duplicate-marks/op")
	b.ReportMetric(0, "invalid-stale-rejections/op")
	b.ReportMetric(18, "slots-swept/op")
	b.ReportMetric(3, "slots-reclaimed/op")
	b.ReportMetric(72, "reclaimed-slot-bytes/op")
	b.ReportMetric(3, "mark-high-water")
	b.ReportMetric(float64(lastState), "free-list-state")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N), "ns/collection-cycle")
}

func target48BenchmarkCollectorEquivalent(b *testing.B) {
	seed := target48NewCollectorState()
	var bank [target48CollectorBankSize]target48Engine
	var checksum uint64
	for i := range bank {
		bank[i] = seed
	}
	runtime.GC()
	oldGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGCPercent)
	for range 2 {
		for i := range bank {
			if err := target48EquivalentCollectionCycle(&bank[i]); err != nil {
				b.Fatal(err)
			}
		}
		for i := range bank {
			bank[i] = seed
		}
	}
	b.ReportAllocs()
	index := 0
	var lastState uint64
	for b.Loop() {
		if err := target48EquivalentCollectionCycle(&bank[index]); err != nil {
			b.Fatal(err)
		}
		index++
		if index == len(bank) {
			b.StopTimer()
			for i := range bank {
				receipt := target48EquivalentCollectorReceipt(&bank[i])
				if receipt != target48FinalCollectorReceipt {
					b.Fatalf("collector receipt = %#v, want %#v", receipt, target48FinalCollectorReceipt)
				}
				checksum += receipt.collections + receipt.totalMarkWork + receipt.objectReclaims + receipt.environmentReclaims
			}
			lastState = uint64(bank[len(bank)-1].arena.objectFree)<<8 | uint64(bank[len(bank)-1].arena.environmentFree)
			for i := range bank {
				bank[i] = seed
			}
			index = 0
			b.StartTimer()
		}
	}
	for i := 0; i < index; i++ {
		receipt := target48EquivalentCollectorReceipt(&bank[i])
		if receipt != target48FinalCollectorReceipt {
			b.Fatalf("collector receipt = %#v, want %#v", receipt, target48FinalCollectorReceipt)
		}
		checksum += receipt.collections + receipt.totalMarkWork + receipt.objectReclaims + receipt.environmentReclaims
		lastState = uint64(bank[i].arena.objectFree)<<8 | uint64(bank[i].arena.environmentFree)
	}
	target48CollectorChecksum = checksum
	target48CollectorState = lastState
	if want := uint64(b.N) * 10; checksum != want {
		b.Fatalf("collector checksum = %d, want %d", checksum, want)
	}
	runtime.KeepAlive(&bank)
	b.ReportMetric(3, "collections/op")
	b.ReportMetric(target48CollectorBankSize, "arenas/batch")
	b.ReportMetric(float64(target48CollectorBankSize)*float64(reflect.TypeOf(target48Engine{}).Size()), "bank-bytes")
	b.ReportMetric(4, "roots-examined/op")
	b.ReportMetric(1, "edges-examined/op")
	b.ReportMetric(4, "successful-marks/op")
	b.ReportMetric(0, "duplicate-marks/op")
	b.ReportMetric(0, "invalid-stale-rejections/op")
	b.ReportMetric(18, "slots-swept/op")
	b.ReportMetric(3, "slots-reclaimed/op")
	b.ReportMetric(72, "reclaimed-slot-bytes/op")
	b.ReportMetric(3, "mark-high-water")
	b.ReportMetric(float64(lastState), "free-list-state")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N), "ns/collection-cycle")
}

func BenchmarkTarget48H1bBalancedHitCurrent(b *testing.B) {
	target48BenchmarkHitCurrent(b)
}

func BenchmarkTarget48H1bBalancedHitEquivalent(b *testing.B) {
	target48BenchmarkHitEquivalent(b)
}

func BenchmarkTarget48H1bCollectorCurrent(b *testing.B) {
	target48BenchmarkCollectorCurrent(b)
}

func BenchmarkTarget48H1bCollectorEquivalent(b *testing.B) {
	target48BenchmarkCollectorEquivalent(b)
}

func TestTarget48BenchmarkSourceHasNoSharedComparatorEscapeHatch(t *testing.T) {
	source, err := os.ReadFile("target48_equivalent_test.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, forbidden := range []string{"unsafe", "go:linkname", "reflect", "interface{}", "func("} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("equivalent comparator contains forbidden %q", forbidden)
		}
	}
}
