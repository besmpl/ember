package generated

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

const (
	target42GeneratedSourceBytes  = 79_361
	target42GeneratedSourceLF     = 2_344
	target42GeneratedSourceSHA256 = "b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46"
)

type target42GeneratedLane uint8

const (
	target42GenericLane target42GeneratedLane = iota + 1
	target42CurrentLane
)

var target42BenchmarkSink int64

//go:noinline
func target42BenchmarkGeneric(e *Engine, c *rubyControl, object *rubyObject) (int64, error) {
	return target42GenericReadSingleton(e, c, object)
}

// target42GenericReadSingleton reconstructs the generic target-dispatch shape
// around the candidate's one recovery authority. It intentionally keeps the
// exact receiver-to-cell and receiver-to-arm mapping required by the bounded
// singleton site; only the warmed target call remains generic.
func target42GenericReadSingleton(e *Engine, c *rubyControl, object *rubyObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := c.step(); err != nil {
		return 0, err
	}
	var cell *rubyCell
	var arm *rubyArm
	switch object.dispatch {
	case rubySingletonDispatch:
		cell, arm = &e.lookup.cells[rubySingletonLabelCell], &e.singletonSite.arms[0]
		if object.shape != rubyAlphaShape || !rubySingletonLabelCellValid(cell, e.lookup.nextEpoch) {
			return 0, ErrInternal
		}
	case rubyAlphaDispatch:
		cell, arm = &e.lookup.cells[rubyAlphaLabelCell], &e.singletonSite.arms[1]
		if object.shape != rubyAlphaShape || !rubySingletonPeerCellValid(cell, e.lookup.nextEpoch) {
			return 0, ErrInternal
		}
	default:
		return 0, ErrInternal
	}
	if rubySingletonSiteValid(&e.singletonSite, e.lookup.nextEpoch) && rubyArmHits(arm, object, cell) {
		value, err := e.callLabelTarget(c, object, arm.target)
		if err == nil {
			e.stats.Hits++
		}
		return value, err
	}
	return e.readSingletonColdAfterStep(c, object, cell, arm)
}

//go:noinline
func target42BenchmarkCurrent(e *Engine, c *rubyControl, object *rubyObject) (int64, error) {
	return e.readSingleton(c, object)
}

//go:noinline
func target42BenchmarkEquivalent(e *target42EquivalentEngine, c *target42EquivalentControl, object *target42EquivalentObject) (int64, error) {
	return target42EquivalentRead(e, c, object)
}

func target42ApplyC1(t testing.TB, engine *Engine, control *rubyControl) {
	t.Helper()
	for _, mutation := range []uint32{
		rubyBaseRedefineMutation, rubyBaseProtectedMutation, rubyBasePrivateMutation, rubyBasePublicMutation,
		rubyBetaRemoveMutation, rubyGammaUndefMutation, rubyIncludedLabelInitialMutation, rubyAlphaIncludeMutation,
		rubyBetaPrependMutation, rubyPrependedLabelMutation, rubyIncludedLabelRedefinedMutation,
	} {
		if err := engine.lookup.apply(control, mutation); err != nil {
			t.Fatalf("apply C1 mutation %d: %v", mutation, err)
		}
	}
}

func target42SetupGeneratedBeforeRepair(t testing.TB) (*Engine, rubyObject, rubyObject) {
	t.Helper()
	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 1_024, frames: 64, objects: 16}
	target42ApplyC1(t, engine, &control)
	receiver := rubyObject{id: 1, dispatch: rubySingletonDispatch, shape: rubyAlphaShape, value: rubySingletonValue}
	peer := rubyObject{id: 2, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: rubySingletonValue}
	if value, err := engine.readSingleton(&control, &receiver); err != nil || value != 13 {
		t.Fatalf("receiver cold = %d, %v", value, err)
	}
	if value, err := engine.readSingleton(&control, &receiver); err != nil || value != 13 {
		t.Fatalf("receiver warm = %d, %v", value, err)
	}
	if value, err := engine.readSingleton(&control, &peer); err != nil || value != 13 {
		t.Fatalf("peer cold = %d, %v", value, err)
	}
	if err := engine.lookup.apply(&control, rubySingletonDefineMutation); err != nil {
		t.Fatal(err)
	}
	wantStats := Stats{Hits: 1, ColdAdmissions: 2, Admissions: 2, OccupiedArms: 2}
	wantSite := rubySite{arms: [2]rubyArm{
		{epoch: 31, dispatch: rubySingletonDispatch, shape: rubyAlphaShape, slot: rubyIncludedLabelSlot, definition: rubyIncludedLabelRedefinedDefinition, target: rubyIncludedLabelRedefinedTarget},
		{epoch: 29, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, slot: rubyIncludedLabelSlot, definition: rubyIncludedLabelRedefinedDefinition, target: rubyIncludedLabelRedefinedTarget},
	}}
	if receiver.trace != 33 || peer.trace != 3 || engine.lookup.nextEpoch != 32 || engine.singletonSite != wantSite || engine.stats != wantStats {
		t.Fatalf("pre-repair state traces=%d/%d epoch=%d site=%#v stats=%#v", receiver.trace, peer.trace, engine.lookup.nextEpoch, engine.singletonSite, engine.stats)
	}
	return engine, receiver, peer
}

func target42SetupGenerated(t testing.TB) (*Engine, rubyObject, rubyObject, Stats) {
	t.Helper()
	engine, receiver, peer := target42SetupGeneratedBeforeRepair(t)
	control := rubyControl{ctx: context.Background(), steps: 2, frames: 3}
	if value, err := engine.readSingleton(&control, &receiver); err != nil || value != 44 {
		t.Fatalf("receiver repair = %d, %v", value, err)
	}
	wantStats := Stats{Hits: 1, ColdAdmissions: 2, StaleMisses: 1, Repairs: 1, Admissions: 2, OccupiedArms: 2}
	wantSite := rubySite{arms: [2]rubyArm{
		{epoch: 32, dispatch: rubySingletonDispatch, shape: rubyAlphaShape, slot: rubySingletonLabelSlot, definition: rubySingletonLabelDefinition, target: rubySingletonLabelTarget},
		{epoch: 29, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, slot: rubyIncludedLabelSlot, definition: rubyIncludedLabelRedefinedDefinition, target: rubyIncludedLabelRedefinedTarget},
	}}
	if receiver.trace != 334 || peer.trace != 3 || control.steps != 0 || control.depth != 0 || control.nextFrame != 3 ||
		engine.lookup.nextEpoch != 32 || engine.singletonSite != wantSite || engine.stats != wantStats {
		t.Fatalf("warmed generated state traces=%d/%d control=%#v epoch=%d site=%#v stats=%#v", receiver.trace, peer.trace, control, engine.lookup.nextEpoch, engine.singletonSite, engine.stats)
	}
	receiver.trace, peer.trace = 0, 0
	return engine, receiver, peer, wantStats
}

func target42SetupEquivalent(t testing.TB) (*target42EquivalentEngine, target42EquivalentObject, target42EquivalentObject, target42EquivalentStats) {
	t.Helper()
	engine, receiver, peer := target42NewEquivalentEngine()
	wantStats := target42EquivalentStats{hits: 1, coldAdmissions: 2, staleMisses: 1, repairs: 1, admissions: 2, occupiedArms: 2}
	wantSite := target42EquivalentSite{arms: [2]target42EquivalentArm{
		{epoch: 32, dispatch: 5, shape: 1, slot: 15, definition: 17, target: 9},
		{epoch: 29, dispatch: 1, shape: 1, slot: 13, definition: 16, target: 8},
	}}
	if engine.lookup.nextEpoch != 32 || engine.site != wantSite || engine.stats != wantStats || receiver.trace != 0 || peer.trace != 0 {
		t.Fatalf("equivalent warmed state lookup=%#v site=%#v stats=%#v receiver=%#v peer=%#v", engine.lookup, engine.site, engine.stats, receiver, peer)
	}
	return engine, receiver, peer, wantStats
}

type target42GeneratedSnapshot struct {
	lookup                          rubyLookup
	label, saved, public, singleton rubySite
	hotObject                       rubyObject
	nextObject                      uint64
	stats                           Stats
	active, closed, poisoned        bool
}

func target42SnapshotGenerated(engine *Engine) target42GeneratedSnapshot {
	return target42GeneratedSnapshot{
		lookup: engine.lookup, label: engine.labelSite, saved: engine.savedSite, public: engine.publicSite, singleton: engine.singletonSite,
		hotObject: engine.hotObject, nextObject: engine.nextObject, stats: engine.stats,
		active: engine.active.Load(), closed: engine.closed.Load(), poisoned: engine.poisoned.Load(),
	}
}

type target42ControlState struct {
	steps, frames, objects, depth, nextFrame uint64
}

func target42GeneratedControlState(control rubyControl) target42ControlState {
	return target42ControlState{control.steps, control.frames, control.objects, control.depth, control.nextFrame}
}

func target42ValidateGenericEquivalence(t *testing.T) {
	t.Helper()
	type scenario struct {
		name   string
		peer   bool
		steps  uint64
		frames uint64
		ctx    context.Context
		mutate func(*Engine, *rubyObject)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	scenarios := []scenario{
		{name: "receiver hit", steps: 2, frames: 3, ctx: context.Background()},
		{name: "peer hit", peer: true, steps: 2, frames: 3, ctx: context.Background()},
		{name: "outer step limit", frames: 3, ctx: context.Background()},
		{name: "outer frame limit", steps: 2, ctx: context.Background()},
		{name: "target frame limit", steps: 2, frames: 1, ctx: context.Background()},
		{name: "mark frame limit", steps: 2, frames: 2, ctx: context.Background()},
		{name: "target step limit", steps: 1, frames: 3, ctx: context.Background()},
		{name: "pre-entry cancellation", steps: 2, frames: 3, ctx: canceled},
		{name: "wrong dispatch", steps: 2, frames: 3, ctx: context.Background(), mutate: func(_ *Engine, object *rubyObject) { object.dispatch = rubyBetaDispatch }},
		{name: "wrong shape", steps: 2, frames: 3, ctx: context.Background(), mutate: func(_ *Engine, object *rubyObject) { object.shape = rubyBetaShape }},
		{name: "receiver cell corruption", steps: 2, frames: 3, ctx: context.Background(), mutate: func(engine *Engine, _ *rubyObject) { engine.lookup.cells[rubySingletonLabelCell].receipt.owner = 0 }},
		{name: "receiver arm corruption", steps: 2, frames: 3, ctx: context.Background(), mutate: func(engine *Engine, _ *rubyObject) { engine.singletonSite.arms[0].dispatch = rubyAlphaDispatch }},
		{name: "peer cell corruption", peer: true, steps: 2, frames: 3, ctx: context.Background(), mutate: func(engine *Engine, _ *rubyObject) { engine.lookup.cells[rubyAlphaLabelCell].receipt.owner = 0 }},
		{name: "peer arm corruption", peer: true, steps: 2, frames: 3, ctx: context.Background(), mutate: func(engine *Engine, _ *rubyObject) { engine.singletonSite.arms[1].dispatch = rubySingletonDispatch }},
		{name: "unused arm corruption", steps: 2, frames: 3, ctx: context.Background(), mutate: func(engine *Engine, _ *rubyObject) { engine.singletonSite.arms[1].target = rubySingletonLabelTarget }},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			leftEngine, leftReceiver, leftPeer, _ := target42SetupGenerated(t)
			rightEngine, rightReceiver, rightPeer, _ := target42SetupGenerated(t)
			leftObject, rightObject := &leftReceiver, &rightReceiver
			if scenario.peer {
				leftObject, rightObject = &leftPeer, &rightPeer
			}
			if scenario.mutate != nil {
				scenario.mutate(leftEngine, leftObject)
				scenario.mutate(rightEngine, rightObject)
			}
			leftControl := rubyControl{ctx: scenario.ctx, steps: scenario.steps, frames: scenario.frames}
			rightControl := rubyControl{ctx: scenario.ctx, steps: scenario.steps, frames: scenario.frames}
			leftValue, leftErr := target42BenchmarkCurrent(leftEngine, &leftControl, leftObject)
			rightValue, rightErr := target42BenchmarkGeneric(rightEngine, &rightControl, rightObject)
			if leftValue != rightValue || leftErr != rightErr || *leftObject != *rightObject ||
				target42GeneratedControlState(leftControl) != target42GeneratedControlState(rightControl) ||
				target42SnapshotGenerated(leftEngine) != target42SnapshotGenerated(rightEngine) {
				t.Fatalf("current/generic differ: values=%d/%d errors=%v/%v objects=%#v/%#v controls=%#v/%#v states=%#v/%#v",
					leftValue, rightValue, leftErr, rightErr, *leftObject, *rightObject,
					target42GeneratedControlState(leftControl), target42GeneratedControlState(rightControl),
					target42SnapshotGenerated(leftEngine), target42SnapshotGenerated(rightEngine))
			}
		})
	}

	t.Run("stale recovery", func(t *testing.T) {
		leftEngine, leftReceiver, _ := target42SetupGeneratedBeforeRepair(t)
		rightEngine, rightReceiver, _ := target42SetupGeneratedBeforeRepair(t)
		leftControl := rubyControl{ctx: context.Background(), steps: 2, frames: 3}
		rightControl := rubyControl{ctx: context.Background(), steps: 2, frames: 3}
		leftValue, leftErr := target42BenchmarkCurrent(leftEngine, &leftControl, &leftReceiver)
		rightValue, rightErr := target42BenchmarkGeneric(rightEngine, &rightControl, &rightReceiver)
		if leftValue != 44 || rightValue != 44 || leftErr != nil || rightErr != nil || leftReceiver != rightReceiver ||
			target42GeneratedControlState(leftControl) != target42GeneratedControlState(rightControl) ||
			target42SnapshotGenerated(leftEngine) != target42SnapshotGenerated(rightEngine) {
			t.Fatalf("current/generic stale recovery differs: values=%d/%d errors=%v/%v receivers=%#v/%#v controls=%#v/%#v states=%#v/%#v",
				leftValue, rightValue, leftErr, rightErr, leftReceiver, rightReceiver,
				target42GeneratedControlState(leftControl), target42GeneratedControlState(rightControl),
				target42SnapshotGenerated(leftEngine), target42SnapshotGenerated(rightEngine))
		}
	})

	for cancelAt := 1; cancelAt <= 6; cancelAt++ {
		t.Run(fmt.Sprintf("cancellation poll %d", cancelAt), func(t *testing.T) {
			leftEngine, leftReceiver, _, _ := target42SetupGenerated(t)
			rightEngine, rightReceiver, _, _ := target42SetupGenerated(t)
			leftContext := &target42CancelAfterContext{Context: context.Background(), cancelAt: cancelAt}
			rightContext := &target42CancelAfterContext{Context: context.Background(), cancelAt: cancelAt}
			leftControl := rubyControl{ctx: leftContext, steps: 2, frames: 3}
			rightControl := rubyControl{ctx: rightContext, steps: 2, frames: 3}
			leftValue, leftErr := target42BenchmarkCurrent(leftEngine, &leftControl, &leftReceiver)
			rightValue, rightErr := target42BenchmarkGeneric(rightEngine, &rightControl, &rightReceiver)
			if leftValue != rightValue || leftErr != rightErr || leftReceiver != rightReceiver || leftContext.calls != rightContext.calls ||
				target42GeneratedControlState(leftControl) != target42GeneratedControlState(rightControl) ||
				target42SnapshotGenerated(leftEngine) != target42SnapshotGenerated(rightEngine) {
				t.Fatalf("current/generic cancellation poll %d differs: values=%d/%d errors=%v/%v receivers=%#v/%#v polls=%d/%d controls=%#v/%#v states=%#v/%#v",
					cancelAt, leftValue, rightValue, leftErr, rightErr, leftReceiver, rightReceiver, leftContext.calls, rightContext.calls,
					target42GeneratedControlState(leftControl), target42GeneratedControlState(rightControl),
					target42SnapshotGenerated(leftEngine), target42SnapshotGenerated(rightEngine))
			}
		})
	}
}

func TestTarget42GenericRecoveryShapeIsEquivalent(t *testing.T) {
	target42ValidateGenericEquivalence(t)
}

func target42EquivalentControlState(control target42EquivalentControl) target42ControlState {
	return target42ControlState{control.steps, control.frames, control.objects, control.depth, control.nextFrame}
}

func target42EquivalentStatsAsGenerated(stats target42EquivalentStats) Stats {
	return Stats{
		Hits: stats.hits, ColdAdmissions: stats.coldAdmissions, StaleMisses: stats.staleMisses, Repairs: stats.repairs,
		UncachedFallbacks: stats.uncachedFallbacks, Admissions: stats.admissions, Evictions: stats.evictions, OccupiedArms: stats.occupiedArms,
	}
}

func TestTarget42EquivalentComparatorMatchesWarmedSemantics(t *testing.T) {
	type scenario struct {
		name        string
		peer        bool
		steps       uint64
		frames      uint64
		ctx         context.Context
		mutation    string
		wantValue   int64
		wantTrace   int64
		wantError   string
		wantControl target42ControlState
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, scenario := range []scenario{
		{name: "receiver hit", steps: 2, frames: 3, ctx: context.Background(), wantValue: 44, wantTrace: 4, wantControl: target42ControlState{frames: 3, nextFrame: 3}},
		{name: "peer hit", peer: true, steps: 2, frames: 3, ctx: context.Background(), wantValue: 13, wantTrace: 3, wantControl: target42ControlState{frames: 3, nextFrame: 3}},
		{name: "outer step limit", frames: 3, ctx: context.Background(), wantError: "step", wantControl: target42ControlState{frames: 3, nextFrame: 1}},
		{name: "outer frame limit", steps: 2, ctx: context.Background(), wantError: "frame", wantControl: target42ControlState{steps: 2}},
		{name: "target frame limit", steps: 2, frames: 1, ctx: context.Background(), wantError: "frame", wantControl: target42ControlState{steps: 1, frames: 1, nextFrame: 1}},
		{name: "mark frame limit", steps: 2, frames: 2, ctx: context.Background(), wantError: "frame", wantControl: target42ControlState{steps: 1, frames: 2, nextFrame: 2}},
		{name: "target step limit", steps: 1, frames: 3, ctx: context.Background(), wantError: "step", wantControl: target42ControlState{frames: 3, nextFrame: 3}},
		{name: "pre-entry cancellation", steps: 2, frames: 3, ctx: canceled, wantError: "canceled", wantControl: target42ControlState{steps: 2, frames: 3}},
		{name: "wrong dispatch", steps: 2, frames: 3, ctx: context.Background(), mutation: "wrong dispatch", wantError: "internal", wantControl: target42ControlState{steps: 1, frames: 3, nextFrame: 1}},
		{name: "wrong shape", steps: 2, frames: 3, ctx: context.Background(), mutation: "wrong shape", wantError: "internal", wantControl: target42ControlState{steps: 1, frames: 3, nextFrame: 1}},
		{name: "selected cell corruption", steps: 2, frames: 3, ctx: context.Background(), mutation: "selected cell", wantError: "internal", wantControl: target42ControlState{steps: 1, frames: 3, nextFrame: 1}},
		{name: "selected arm corruption", steps: 2, frames: 3, ctx: context.Background(), mutation: "selected arm", wantError: "internal", wantControl: target42ControlState{steps: 1, frames: 3, nextFrame: 1}},
		{name: "peer selected cell corruption", peer: true, steps: 2, frames: 3, ctx: context.Background(), mutation: "peer selected cell", wantError: "internal", wantControl: target42ControlState{steps: 1, frames: 3, nextFrame: 1}},
		{name: "peer selected arm corruption", peer: true, steps: 2, frames: 3, ctx: context.Background(), mutation: "peer selected arm", wantError: "internal", wantControl: target42ControlState{steps: 1, frames: 3, nextFrame: 1}},
		{name: "unused peer arm corruption", steps: 2, frames: 3, ctx: context.Background(), mutation: "unused peer arm", wantError: "internal", wantControl: target42ControlState{steps: 1, frames: 3, nextFrame: 1}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			generatedEngine, generatedReceiver, generatedPeer, generatedBefore := target42SetupGenerated(t)
			equivalentEngine, equivalentReceiver, equivalentPeer, equivalentBefore := target42SetupEquivalent(t)
			generatedObject, equivalentObject := &generatedReceiver, &equivalentReceiver
			if scenario.peer {
				generatedObject, equivalentObject = &generatedPeer, &equivalentPeer
			}
			switch scenario.mutation {
			case "wrong dispatch":
				generatedObject.dispatch = rubyBetaDispatch
				equivalentObject.dispatch = 2
			case "wrong shape":
				generatedObject.shape = rubyBetaShape
				equivalentObject.shape = 2
			case "selected cell":
				generatedEngine.lookup.cells[rubySingletonLabelCell].receipt.owner = 0
				equivalentEngine.lookup.receiver.receipt.owner = 0
			case "selected arm":
				generatedEngine.singletonSite.arms[0].dispatch = rubyAlphaDispatch
				equivalentEngine.site.arms[0].dispatch = 1
			case "peer selected cell":
				generatedEngine.lookup.cells[rubyAlphaLabelCell].receipt.owner = 0
				equivalentEngine.lookup.peer.receipt.owner = 0
			case "peer selected arm":
				generatedEngine.singletonSite.arms[1].dispatch = rubySingletonDispatch
				equivalentEngine.site.arms[1].dispatch = 5
			case "unused peer arm":
				generatedEngine.singletonSite.arms[1].target = rubySingletonLabelTarget
				equivalentEngine.site.arms[1].target = 9
			}
			generatedLookup, generatedSite := generatedEngine.lookup, generatedEngine.singletonSite
			equivalentLookup, equivalentSite := equivalentEngine.lookup, equivalentEngine.site
			generatedControl := rubyControl{ctx: scenario.ctx, steps: scenario.steps, frames: scenario.frames}
			equivalentControl := target42EquivalentControl{ctx: scenario.ctx, steps: scenario.steps, frames: scenario.frames}
			generatedValue, generatedErr := target42BenchmarkCurrent(generatedEngine, &generatedControl, generatedObject)
			equivalentValue, equivalentErr := target42BenchmarkEquivalent(equivalentEngine, &equivalentControl, equivalentObject)
			generatedError := target42GeneratedErrorClass(generatedErr)
			equivalentError := target42EquivalentErrorClass(equivalentErr)
			wantGeneratedStats := generatedBefore
			wantEquivalentStats := equivalentBefore
			if scenario.wantError == "" {
				wantGeneratedStats.Hits++
				wantEquivalentStats.hits++
			}
			if generatedValue != scenario.wantValue || equivalentValue != scenario.wantValue || generatedError != scenario.wantError || equivalentError != scenario.wantError ||
				generatedObject.trace != scenario.wantTrace || equivalentObject.trace != scenario.wantTrace ||
				target42GeneratedControlState(generatedControl) != scenario.wantControl || target42EquivalentControlState(equivalentControl) != scenario.wantControl ||
				generatedEngine.lookup != generatedLookup || generatedEngine.singletonSite != generatedSite || equivalentEngine.lookup != equivalentLookup || equivalentEngine.site != equivalentSite ||
				generatedEngine.stats != wantGeneratedStats || equivalentEngine.stats != wantEquivalentStats ||
				generatedEngine.stats != target42EquivalentStatsAsGenerated(equivalentEngine.stats) {
				t.Fatalf("generated/equivalent mismatch: values=%d/%d errors=%s/%s traces=%d/%d controls=%#v/%#v stats=%#v/%#v",
					generatedValue, equivalentValue, generatedError, equivalentError, generatedObject.trace, equivalentObject.trace,
					target42GeneratedControlState(generatedControl), target42EquivalentControlState(equivalentControl), generatedEngine.stats, equivalentEngine.stats)
			}
		})
	}
}

type target42CancelAfterContext struct {
	context.Context
	cancelAt, calls int
}

func (c *target42CancelAfterContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestTarget42EquivalentComparatorMatchesEveryWarmedCancellationPoll(t *testing.T) {
	wantControls := map[int]target42ControlState{
		1: {steps: 2, frames: 3},
		2: {steps: 2, frames: 3, nextFrame: 1},
		3: {steps: 1, frames: 3, nextFrame: 1},
		4: {steps: 1, frames: 3, nextFrame: 2},
		5: {steps: 1, frames: 3, nextFrame: 3},
		6: {steps: 0, frames: 3, nextFrame: 3},
	}
	for cancelAt := 1; cancelAt <= 6; cancelAt++ {
		t.Run(fmt.Sprintf("poll%d", cancelAt), func(t *testing.T) {
			generatedEngine, generatedReceiver, _, generatedBefore := target42SetupGenerated(t)
			equivalentEngine, equivalentReceiver, _, equivalentBefore := target42SetupEquivalent(t)
			generatedLookup, generatedSite := generatedEngine.lookup, generatedEngine.singletonSite
			equivalentLookup, equivalentSite := equivalentEngine.lookup, equivalentEngine.site
			generatedContext := &target42CancelAfterContext{Context: context.Background(), cancelAt: cancelAt}
			equivalentContext := &target42CancelAfterContext{Context: context.Background(), cancelAt: cancelAt}
			generatedControl := rubyControl{ctx: generatedContext, steps: 2, frames: 3}
			equivalentControl := target42EquivalentControl{ctx: equivalentContext, steps: 2, frames: 3}
			generatedValue, generatedErr := target42BenchmarkCurrent(generatedEngine, &generatedControl, &generatedReceiver)
			equivalentValue, equivalentErr := target42BenchmarkEquivalent(equivalentEngine, &equivalentControl, &equivalentReceiver)
			wantValue, wantTrace, wantError := int64(0), int64(0), "canceled"
			wantGeneratedStats, wantEquivalentStats := generatedBefore, equivalentBefore
			wantPolls := cancelAt
			if cancelAt == 6 {
				// Five polls precede the effect. Success at cancelAt=6 proves
				// neither implementation performs a post-effect poll.
				wantValue, wantTrace, wantError, wantPolls = 44, 4, "", 5
				wantGeneratedStats.Hits++
				wantEquivalentStats.hits++
			}
			if generatedValue != wantValue || equivalentValue != wantValue || target42GeneratedErrorClass(generatedErr) != wantError || target42EquivalentErrorClass(equivalentErr) != wantError ||
				generatedReceiver.trace != wantTrace || equivalentReceiver.trace != wantTrace || generatedContext.calls != wantPolls || equivalentContext.calls != wantPolls ||
				target42GeneratedControlState(generatedControl) != wantControls[cancelAt] || target42EquivalentControlState(equivalentControl) != wantControls[cancelAt] ||
				generatedEngine.lookup != generatedLookup || generatedEngine.singletonSite != generatedSite || equivalentEngine.lookup != equivalentLookup || equivalentEngine.site != equivalentSite ||
				generatedEngine.stats != wantGeneratedStats || equivalentEngine.stats != wantEquivalentStats || generatedEngine.stats != target42EquivalentStatsAsGenerated(equivalentEngine.stats) {
				t.Fatalf("poll %d generated/equivalent mismatch: values=%d/%d errors=%s/%s traces=%d/%d polls=%d/%d controls=%#v/%#v stats=%#v/%#v",
					cancelAt, generatedValue, equivalentValue, target42GeneratedErrorClass(generatedErr), target42EquivalentErrorClass(equivalentErr),
					generatedReceiver.trace, equivalentReceiver.trace, generatedContext.calls, equivalentContext.calls,
					target42GeneratedControlState(generatedControl), target42EquivalentControlState(equivalentControl), generatedEngine.stats, equivalentEngine.stats)
			}
		})
	}
}

func target42GeneratedErrorClass(err error) string {
	switch err {
	case nil:
		return ""
	case ErrStepLimit:
		return "step"
	case ErrFrameLimit:
		return "frame"
	case context.Canceled:
		return "canceled"
	case ErrInternal:
		return "internal"
	default:
		return "unexpected"
	}
}

func target42EquivalentErrorClass(err error) string {
	switch err {
	case nil:
		return ""
	case target42EquivalentStepLimit:
		return "step"
	case target42EquivalentFrameLimit:
		return "frame"
	case context.Canceled:
		return "canceled"
	case target42EquivalentInternal:
		return "internal"
	default:
		return "unexpected"
	}
}

func target42RunGeneratedVector(t testing.TB, lane target42GeneratedLane) {
	t.Helper()
	engine, receiver, _, before := target42SetupGenerated(t)
	beforeLookup, beforeSite := engine.lookup, engine.singletonSite
	control := rubyControl{ctx: context.Background(), steps: 16, frames: 3}
	var sum int64
	switch lane {
	case target42GenericLane:
		for range 8 {
			value, err := target42BenchmarkGeneric(engine, &control, &receiver)
			if err != nil {
				t.Fatal(err)
			}
			sum += value
		}
	case target42CurrentLane:
		for range 8 {
			value, err := target42BenchmarkCurrent(engine, &control, &receiver)
			if err != nil {
				t.Fatal(err)
			}
			sum += value
		}
	default:
		t.Fatalf("unknown generated vector lane %d", lane)
	}
	wantStats := before
	wantStats.Hits += 8
	if sum != 352 || receiver.trace != 44_444_444 || control.steps != 0 || control.depth != 0 || control.nextFrame != 24 ||
		engine.lookup != beforeLookup || engine.singletonSite != beforeSite || engine.stats != wantStats {
		t.Fatalf("generated vector lane=%d sum=%d receiver=%#v control=%#v lookupChanged=%v site=%#v stats=%#v want=%#v",
			lane, sum, receiver, control, engine.lookup != beforeLookup, engine.singletonSite, engine.stats, wantStats)
	}
}

func target42RunEquivalentVector(t testing.TB) {
	t.Helper()
	engine, receiver, _, before := target42SetupEquivalent(t)
	beforeLookup, beforeSite := engine.lookup, engine.site
	control := target42EquivalentControl{ctx: context.Background(), steps: 16, frames: 3}
	var sum int64
	for range 8 {
		value, err := target42BenchmarkEquivalent(engine, &control, &receiver)
		if err != nil {
			t.Fatal(err)
		}
		sum += value
	}
	wantStats := before
	wantStats.hits += 8
	if sum != 352 || receiver.trace != 44_444_444 || control.steps != 0 || control.depth != 0 || control.nextFrame != 24 ||
		engine.lookup != beforeLookup || engine.site != beforeSite || engine.stats != wantStats {
		t.Fatalf("equivalent vector sum=%d receiver=%#v control=%#v lookupChanged=%v site=%#v stats=%#v want=%#v",
			sum, receiver, control, engine.lookup != beforeLookup, engine.site, engine.stats, wantStats)
	}
}

func TestTarget42BenchmarkVectorComparatorIsolationAndZeroAllocations(t *testing.T) {
	target42RunGeneratedVector(t, target42GenericLane)
	target42RunGeneratedVector(t, target42CurrentLane)
	target42RunEquivalentVector(t)

	generic, genericReceiver, _, _ := target42SetupGenerated(t)
	genericFailed := false
	genericAllocations := testing.AllocsPerRun(1_000, func() {
		genericReceiver.trace = 0
		control := rubyControl{ctx: context.Background(), steps: 16, frames: 3}
		for range 8 {
			if _, err := target42BenchmarkGeneric(generic, &control, &genericReceiver); err != nil {
				genericFailed = true
			}
		}
	})
	if genericAllocations != 0 || genericFailed {
		t.Errorf("generic allocations=%.2f failed=%v", genericAllocations, genericFailed)
	}
	current, currentReceiver, _, _ := target42SetupGenerated(t)
	currentFailed := false
	currentAllocations := testing.AllocsPerRun(1_000, func() {
		currentReceiver.trace = 0
		control := rubyControl{ctx: context.Background(), steps: 16, frames: 3}
		for range 8 {
			if _, err := target42BenchmarkCurrent(current, &control, &currentReceiver); err != nil {
				currentFailed = true
			}
		}
	})
	if currentAllocations != 0 || currentFailed {
		t.Errorf("current allocations=%.2f failed=%v", currentAllocations, currentFailed)
	}
	equivalent, receiver, _, _ := target42SetupEquivalent(t)
	failed := false
	allocations := testing.AllocsPerRun(1_000, func() {
		receiver.trace = 0
		control := target42EquivalentControl{ctx: context.Background(), steps: 16, frames: 3}
		for range 8 {
			if _, err := target42BenchmarkEquivalent(equivalent, &control, &receiver); err != nil {
				failed = true
			}
		}
	})
	if allocations != 0 || failed {
		t.Errorf("equivalent allocations=%.2f failed=%v", allocations, failed)
	}

	source, err := os.ReadFile("target42_equivalent_test.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "target42_equivalent_test.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	generatedSource, err := os.ReadFile("ruby_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(generatedSource) != target42GeneratedSourceBytes || strings.Count(string(generatedSource), "\n") != target42GeneratedSourceLF ||
		fmt.Sprintf("%x", sha256.Sum256(generatedSource)) != target42GeneratedSourceSHA256 {
		t.Fatalf("generated artifact identity = %d bytes/%d LF/%x", len(generatedSource), strings.Count(string(generatedSource), "\n"), sha256.Sum256(generatedSource))
	}
	generatedFile, err := parser.ParseFile(token.NewFileSet(), "ruby_generated.go", generatedSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := make(map[string]bool)
	for _, declaration := range generatedFile.Decls {
		switch declaration := declaration.(type) {
		case *ast.GenDecl:
			for _, specification := range declaration.Specs {
				switch specification := specification.(type) {
				case *ast.TypeSpec:
					forbidden[specification.Name.Name] = true
				case *ast.ValueSpec:
					for _, name := range specification.Names {
						forbidden[name.Name] = true
					}
				}
			}
		case *ast.FuncDecl:
			if declaration.Recv == nil {
				forbidden[declaration.Name.Name] = true
			}
		}
	}
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if path != "context" {
			t.Errorf("equivalent comparator imports non-allowlisted package %q", path)
		}
	}
	allowedGeneratedNamePositions := make(map[token.Pos]bool)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv != nil && function.Name.Name == "Error" {
			allowedGeneratedNamePositions[function.Name.Pos()] = true
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && !allowedGeneratedNamePositions[identifier.Pos()] && (strings.HasPrefix(identifier.Name, "ruby") || forbidden[identifier.Name]) {
			t.Errorf("equivalent comparator references generated identifier %q", identifier.Name)
		}
		return true
	})

	layouts := []struct {
		name                          string
		generated, equivalent         uintptr
		wantGenerated, wantEquivalent uintptr
	}{
		{name: "receipt", generated: unsafe.Sizeof(rubyReceipt{}), equivalent: unsafe.Sizeof(target42EquivalentReceipt{}), wantGenerated: 16, wantEquivalent: 16},
		{name: "cell", generated: unsafe.Sizeof(rubyCell{}), equivalent: unsafe.Sizeof(target42EquivalentCell{}), wantGenerated: 24, wantEquivalent: 24},
		{name: "arm", generated: unsafe.Sizeof(rubyArm{}), equivalent: unsafe.Sizeof(target42EquivalentArm{}), wantGenerated: 32, wantEquivalent: 32},
		{name: "site", generated: unsafe.Sizeof(rubySite{}), equivalent: unsafe.Sizeof(target42EquivalentSite{}), wantGenerated: 64, wantEquivalent: 64},
		{name: "object", generated: unsafe.Sizeof(rubyObject{}), equivalent: unsafe.Sizeof(target42EquivalentObject{}), wantGenerated: 32, wantEquivalent: 32},
		{name: "control", generated: unsafe.Sizeof(rubyControl{}), equivalent: unsafe.Sizeof(target42EquivalentControl{}), wantGenerated: 56, wantEquivalent: 56},
		{name: "stats", generated: unsafe.Sizeof(Stats{}), equivalent: unsafe.Sizeof(target42EquivalentStats{}), wantGenerated: 64, wantEquivalent: 64},
		{name: "engine", generated: unsafe.Sizeof(Engine{}), equivalent: unsafe.Sizeof(target42EquivalentEngine{}), wantGenerated: 824, wantEquivalent: 240},
	}
	for _, layout := range layouts {
		if layout.generated != layout.wantGenerated || layout.equivalent != layout.wantEquivalent {
			t.Errorf("%s layouts generated/equivalent = %d/%d, want %d/%d", layout.name, layout.generated, layout.equivalent, layout.wantGenerated, layout.wantEquivalent)
		}
	}

	wrappers := []any{target42BenchmarkGeneric, target42BenchmarkCurrent, target42BenchmarkEquivalent}
	seen := make(map[uintptr]bool, len(wrappers))
	for _, wrapper := range wrappers {
		address := reflect.ValueOf(wrapper).Pointer()
		if address == 0 || seen[address] {
			t.Fatalf("benchmark wrapper has zero or duplicate address %#x", address)
		}
		seen[address] = true
	}
}

func benchmarkTarget42Generic(b *testing.B) {
	engine, receiver, _, before := target42SetupGenerated(b)
	beforeLookup, beforeSite := engine.lookup, engine.singletonSite
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl rubyControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		receiver.trace = 0
		control := rubyControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for range 8 {
			value, err := target42BenchmarkGeneric(engine, &control, &receiver)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + receiver.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	wantStats := before
	wantStats.Hits += uint64(8 * b.N)
	wantChecksum := int64(b.N) * 44_444_820
	if failed || lastSum != 352 || receiver.trace != 44_444_444 || lastControl.steps != 0 || lastControl.depth != 0 || lastControl.nextFrame != 24 ||
		engine.lookup != beforeLookup || engine.singletonSite != beforeSite || engine.stats != wantStats || checksum != wantChecksum {
		b.Fatalf("generic benchmark failed=%v sum=%d receiver=%#v control=%#v lookupChanged=%v site=%#v stats=%#v want=%#v checksum=%d/%d",
			failed, lastSum, receiver, lastControl, engine.lookup != beforeLookup, engine.singletonSite, engine.stats, wantStats, checksum, wantChecksum)
	}
	target42BenchmarkSink = checksum
}

func benchmarkTarget42Current(b *testing.B) {
	engine, receiver, _, before := target42SetupGenerated(b)
	beforeLookup, beforeSite := engine.lookup, engine.singletonSite
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl rubyControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		receiver.trace = 0
		control := rubyControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for range 8 {
			value, err := target42BenchmarkCurrent(engine, &control, &receiver)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + receiver.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	wantStats := before
	wantStats.Hits += uint64(8 * b.N)
	wantChecksum := int64(b.N) * 44_444_820
	if failed || lastSum != 352 || receiver.trace != 44_444_444 || lastControl.steps != 0 || lastControl.depth != 0 || lastControl.nextFrame != 24 ||
		engine.lookup != beforeLookup || engine.singletonSite != beforeSite || engine.stats != wantStats || checksum != wantChecksum {
		b.Fatalf("current benchmark failed=%v sum=%d receiver=%#v control=%#v lookupChanged=%v site=%#v stats=%#v want=%#v checksum=%d/%d",
			failed, lastSum, receiver, lastControl, engine.lookup != beforeLookup, engine.singletonSite, engine.stats, wantStats, checksum, wantChecksum)
	}
	target42BenchmarkSink = checksum
}

func benchmarkTarget42Equivalent(b *testing.B) {
	engine, receiver, _, before := target42SetupEquivalent(b)
	beforeLookup, beforeSite := engine.lookup, engine.site
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl target42EquivalentControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		receiver.trace = 0
		control := target42EquivalentControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for range 8 {
			value, err := target42BenchmarkEquivalent(engine, &control, &receiver)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + receiver.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	wantStats := before
	wantStats.hits += uint64(8 * b.N)
	wantChecksum := int64(b.N) * 44_444_820
	if failed || lastSum != 352 || receiver.trace != 44_444_444 || lastControl.steps != 0 || lastControl.depth != 0 || lastControl.nextFrame != 24 ||
		engine.lookup != beforeLookup || engine.site != beforeSite || engine.stats != wantStats || checksum != wantChecksum {
		b.Fatalf("equivalent benchmark failed=%v sum=%d receiver=%#v control=%#v lookupChanged=%v site=%#v stats=%#v want=%#v checksum=%d/%d",
			failed, lastSum, receiver, lastControl, engine.lookup != beforeLookup, engine.site, engine.stats, wantStats, checksum, wantChecksum)
	}
	target42BenchmarkSink = checksum
}

func BenchmarkTarget42Generic(b *testing.B)    { benchmarkTarget42Generic(b) }
func BenchmarkTarget42Current(b *testing.B)    { benchmarkTarget42Current(b) }
func BenchmarkTarget42Equivalent(b *testing.B) { benchmarkTarget42Equivalent(b) }

type target42CaptureLane uint8

const (
	target42CaptureCurrent target42CaptureLane = iota + 1
	target42CaptureEquivalent
	target42CaptureRounds = 20
)

var target42CaptureAOrders = [target42CaptureRounds][2]target42CaptureLane{
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
}

var target42CaptureBOrders = [target42CaptureRounds][2]target42CaptureLane{
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
	{target42CaptureEquivalent, target42CaptureCurrent},
	{target42CaptureCurrent, target42CaptureEquivalent},
}

func BenchmarkTarget42CaptureA(b *testing.B) {
	target42RunCapture(b, "A", target42CaptureAOrders)
}

func BenchmarkTarget42CaptureB(b *testing.B) {
	target42RunCapture(b, "B", target42CaptureBOrders)
}

func target42RunCapture(b *testing.B, capture string, orders [target42CaptureRounds][2]target42CaptureLane) {
	b.Helper()
	if os.Getenv("EMBER_RUBY_TARGET42_CAPTURE") != capture {
		b.Skipf("set EMBER_RUBY_TARGET42_CAPTURE=%s to run retained capture", capture)
	}
	if runtime.GOMAXPROCS(0) != 1 {
		b.Fatalf("Target 42 capture requires GOMAXPROCS=1, got %d", runtime.GOMAXPROCS(0))
	}
	identity, err := target42CaptureIdentity(capture)
	if err != nil {
		b.Fatal(err)
	}
	b.Log(identity)
	b.Run("ReceiverAfterDefinition", func(b *testing.B) {
		for round, order := range orders {
			b.Run(fmt.Sprintf("Round%02d", round+1), func(b *testing.B) {
				for _, lane := range order {
					switch lane {
					case target42CaptureCurrent:
						b.Run("Current", benchmarkTarget42Current)
					case target42CaptureEquivalent:
						b.Run("Equivalent", benchmarkTarget42Equivalent)
					default:
						b.Fatalf("unknown Target 42 capture lane %d", lane)
					}
				}
			})
		}
	})
}
