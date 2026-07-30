package rubyproof

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCanonicalSingletonMethodIsReceiverSpecificAndOwnerBound(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatalf("compile ProofSource: %v", diagnostics)
	}
	runtime, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}

	got, err := runtime.Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	wantSingleton := SingletonResult{
		Before: 13, Warm: 13, PeerBefore: 13,
		After: 44, AfterHit: 44, PeerAfter: 13, Trace: 334433,
	}
	if got.Singleton != wantSingleton {
		t.Fatalf("singleton result = %#v, want %#v", got.Singleton, wantSingleton)
	}

	legacy := got
	legacy.Singleton = SingletonResult{}
	wantLegacy := Result{
		Same: true, Other: false,
		Before: 1, BetaBefore: 3, GammaBefore: 4, AlphaWarm: 1, BetaWarm: 3,
		After: 2, AlphaAfterHit: 2, BetaAfter: 3, GammaAfter: 4,
		Saved: 1, PublicBefore: 2, ProtectedRejected: 5, PrivateRejected: 5,
		PrivateSent: 2, PrivateBeta: 3, PublicRestored: 2,
		BetaRemoved: 2, Returned: 14, Value: 106, Trace: 66776777124532, BetaTrace: 88887, GammaTrace: 99,
		Topology: TopologyResult{
			DeltaBefore: 2, DeltaIncluded: 11, BetaRoute: 2, BetaDefined: 22,
			DeltaRedefined: 13, SavedStable: 1, Trace: 713726,
		},
	}
	if !reflect.DeepEqual(legacy, wantLegacy) {
		t.Fatalf("pre-C2 result changed: got %#v, want %#v", legacy, wantLegacy)
	}

	checked := program.checked
	allocation := checked.calls[checked.singletonAllocationCall]
	definition := checked.calls[checked.singletonDefinitionCall]
	receiverValue, ok := runtime.state.top[checked.singletonReceiverBinding]
	if !ok || receiverValue.kind != rubyObjectValue || receiverValue.object == nil {
		t.Fatal("completed owner did not retain the checked c2_receiver binding")
	}
	receiver := receiverValue.object
	alpha := checked.classes["Alpha"]
	if receiver.class != alpha || receiver.dispatch != allocation.dispatch || receiver.shape != allocation.shape ||
		definition.dispatch != receiver.dispatch || definition.shape != receiver.shape ||
		receiver.dispatch == alpha.dispatch || receiver.shape != alpha.shape ||
		!checked.lookup.validObjectBinding(alpha.node, alpha.dispatch, alpha.shape, receiver.dispatch, receiver.shape) {
		t.Fatalf("singleton receiver binding = %#v; allocation=%#v definition=%#v Alpha=%#v", receiver, allocation, definition, alpha)
	}
	if trace := receiver.fields["@trace"]; trace.kind != rubyInteger || trace.integer != 3344 {
		t.Fatalf("singleton receiver trace = %#v, want 3344", trace)
	}
	if value := receiver.fields["@value"]; value.kind != rubyInteger || value.integer != 10 {
		t.Fatalf("singleton receiver value = %#v, want 10", value)
	}
	unique := 0
	for _, value := range runtime.state.top {
		if value.kind == rubyObjectValue && value.object != nil && value.object.dispatch == receiver.dispatch {
			unique++
		}
	}
	if unique != 1 {
		t.Fatalf("owner retained %d objects with the receiver-specific dispatch, want 1", unique)
	}

	lookup := runtime.state.lookup
	dispatch, shape := receiver.dispatch, receiver.shape
	if err := runtime.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if runtime.state.lookup != nil || runtime.state.top != nil || runtime.state.topMethods != nil || runtime.state.nextObjectID != 0 || runtime.state.nextFrameID != 0 {
		t.Fatalf("closed runtime retained owner state: %#v", runtime.state)
	}
	if lookup.image != nil || lookup.entries != nil || lookup.cells != nil || lookup.staged != nil || lookup.routeScratch != nil || lookup.routeMarks != nil {
		t.Fatalf("closed runtime retained singleton lookup state: %#v", lookup)
	}
	if receiver.dispatch != dispatch || receiver.shape != shape {
		t.Fatal("lookup mutation or owner close changed immutable object dispatch/shape")
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
}

func TestSingletonDefinitionChangesExactlyOneCellAndFailsAtomically(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatalf("compile ProofSource: %v", diagnostics)
	}
	checked := program.checked
	image := checked.lookup
	mutation := image.mutations[len(image.mutations)-1]
	singleton := checked.singletonOwner
	alpha := checked.classes["Alpha"]
	label := checked.selectors["label"]
	if singleton == nil || singleton.kind != checkedOwnerSingleton || mutation.owner != singleton.node || mutation.selector != label || mutation.kind != lookupMutationDefine {
		t.Fatalf("singleton facts = owner %#v mutation %#v label %d", singleton, mutation, label)
	}

	owner := singletonOwnerBeforeDefinition(t, image)
	singletonCell := int(image.rows[singleton.dispatch-1].cellStart) + int(label) - 1
	alphaCell := int(image.rows[alpha.dispatch-1].cellStart) + int(label) - 1
	beforeReceipt, err := owner.canonical(singleton.dispatch, label)
	if err != nil {
		t.Fatal(err)
	}
	alphaReceipt, err := owner.canonical(alpha.dispatch, label)
	if err != nil || beforeReceipt != alphaReceipt {
		t.Fatalf("singleton pre-definition receipt = %#v, Alpha %#v, %v", beforeReceipt, alphaReceipt, err)
	}
	object := rubyObject{class: alpha, dispatch: singleton.dispatch, shape: alpha.shape}
	beforeEntries := append([]lookupEntry(nil), owner.entries...)
	beforeCells := append([]resolutionCell(nil), owner.cells...)
	beforeTopology := owner.topology
	beforeEpoch := owner.nextEpoch
	if err := owner.apply(context.Background(), mutation); err != nil {
		t.Fatalf("define singleton label: %v", err)
	}
	if got := changedSingletonCells(beforeCells, owner.cells); got != 1 || owner.cells[singletonCell] == beforeCells[singletonCell] {
		t.Fatalf("singleton definition changed K=%d cells; singleton cell before=%#v after=%#v", got, beforeCells[singletonCell], owner.cells[singletonCell])
	}
	if owner.cells[alphaCell] != beforeCells[alphaCell] {
		t.Fatalf("singleton definition changed peer Alpha cell: before=%#v after=%#v", beforeCells[alphaCell], owner.cells[alphaCell])
	}
	for index := range owner.cells {
		if index != singletonCell && owner.cells[index] != beforeCells[index] {
			t.Fatalf("singleton definition changed unrelated cell %d", index)
		}
	}
	entryChanges := 0
	for index := range owner.entries {
		if owner.entries[index] != beforeEntries[index] {
			entryChanges++
		}
	}
	if entryChanges != 1 || owner.nextEpoch != beforeEpoch+1 || owner.topology != beforeTopology ||
		owner.cells[singletonCell].receipt.owner != singleton.node || owner.cells[singletonCell].receipt.definition != mutation.after.definition ||
		object.dispatch != singleton.dispatch || object.shape != alpha.shape {
		t.Fatalf("singleton commit entryChanges=%d epoch=%d/%d topology=%#v/%#v cell=%#v object=%#v", entryChanges, owner.nextEpoch, beforeEpoch, owner.topology, beforeTopology, owner.cells[singletonCell], object)
	}

	tests := []struct {
		name    string
		prepare func(*lookupOwner)
		ctx     func() context.Context
		want    error
	}{
		{
			name: "staged cancellation",
			ctx:  func() context.Context { return &cancelAfterContext{allowedErrCalls: 3} },
			want: context.Canceled,
		},
		{
			name: "epoch exhaustion",
			prepare: func(owner *lookupOwner) {
				owner.nextEpoch = math.MaxUint64
			},
			ctx:  context.Background,
			want: errLookupEpochLimit,
		},
		{
			name: "live cell corruption",
			prepare: func(owner *lookupOwner) {
				owner.cells[alphaCell].receipt.reserved32 = 1
			},
			ctx:  context.Background,
			want: errLookupCorrupt,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			owner := singletonOwnerBeforeDefinition(t, image)
			if test.prepare != nil {
				test.prepare(owner)
			}
			entries := append([]lookupEntry(nil), owner.entries...)
			cells := append([]resolutionCell(nil), owner.cells...)
			topology := owner.topology
			epoch := owner.nextEpoch
			if err := owner.apply(test.ctx(), mutation); !errors.Is(err, test.want) {
				t.Fatalf("define returned %v, want %v", err, test.want)
			}
			if !reflect.DeepEqual(owner.entries, entries) || !reflect.DeepEqual(owner.cells, cells) || owner.topology != topology || owner.nextEpoch != epoch {
				t.Fatal("failed singleton definition published live lookup state")
			}
			assertSingletonScratchClear(t, owner)
		})
	}

	replayOwner := singletonOwnerBeforeDefinition(t, image)
	if err := replayOwner.apply(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}
	replayEntries := append([]lookupEntry(nil), replayOwner.entries...)
	replayCells := append([]resolutionCell(nil), replayOwner.cells...)
	replayEpoch := replayOwner.nextEpoch
	if err := replayOwner.apply(context.Background(), mutation); !errors.Is(err, errLookupCorrupt) {
		t.Fatalf("replayed singleton definition returned %v, want %v", err, errLookupCorrupt)
	}
	if !reflect.DeepEqual(replayOwner.entries, replayEntries) || !reflect.DeepEqual(replayOwner.cells, replayCells) || replayOwner.nextEpoch != replayEpoch {
		t.Fatal("replayed singleton definition changed live lookup state")
	}
	assertSingletonScratchClear(t, replayOwner)

	if image.validObjectBinding(alpha.node, alpha.dispatch, alpha.shape, singleton.dispatch, alpha.shape+1) ||
		image.validObjectBinding(checked.classes["Beta"].node, checked.classes["Beta"].dispatch, checked.classes["Beta"].shape, singleton.dispatch, checked.classes["Beta"].shape) {
		t.Fatal("singleton dispatch admitted a wrong shape or ordinary class")
	}
}

func TestCancelledSingletonDefinitionDoesNotExecuteBodyOrPublishLookup(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatalf("compile ProofSource: %v", diagnostics)
	}
	checked := program.checked
	lookup, err := newLookupOwner(checked.lookup)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newRuntime(checked)
	runtime.state = runtimeState{
		lookup:     lookup,
		topMethods: make([]*checkedMethod, len(checked.topMethodNames)),
		top:        make(map[bindingID]rubyValue),
		control: executionControl{
			ctx:              context.Background(),
			remainingSteps:   ProofLimits().Steps,
			maximumFrames:    ProofLimits().Frames,
			remainingObjects: ProofLimits().Objects,
		},
	}
	scope := &evalContext{environment: runtime.state.top}
	definitionStatementFound := false
	for _, statement := range checked.module.statements {
		if statement.kind == statementExpression && statement.expression == checked.singletonDefinitionCall {
			definitionStatementFound = true
			break
		}
		if err := runtime.state.control.step(); err != nil {
			t.Fatalf("prefix statement control: %v", err)
		}
		result, err := runtime.evalStatement(statement, scope)
		if err != nil || result.flow != flowNormal {
			t.Fatalf("evaluate prefix statement: result=%#v err=%v", result, err)
		}
	}
	if !definitionStatementFound {
		t.Fatal("checked singleton definition statement is absent")
	}
	receiver := runtime.state.top[checked.singletonReceiverBinding].object
	if receiver == nil {
		t.Fatal("singleton receiver was not allocated by the checked prefix")
	}
	if trace := receiver.fields["@trace"]; trace.kind != rubyInteger || trace.integer != 33 {
		t.Fatalf("pre-definition receiver trace = %#v, want 33", trace)
	}
	beforeEntries := append([]lookupEntry(nil), lookup.entries...)
	beforeCells := append([]resolutionCell(nil), lookup.cells...)
	beforeTopology := lookup.topology
	beforeEpoch := lookup.nextEpoch
	dispatch, shape := receiver.dispatch, receiver.shape

	// Receiver and symbol evaluation consume the first two polls; lookup.apply
	// consumes the third and then begins its row-by-row staging polls. Cancel
	// after staging has begun but before its final publish poll.
	runtime.state.control.ctx = &cancelAfterContext{allowedErrCalls: 4}
	if _, err := runtime.evalCall(checked.singletonDefinitionCall, scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled singleton definition returned %v, want %v", err, context.Canceled)
	}
	if !reflect.DeepEqual(lookup.entries, beforeEntries) || !reflect.DeepEqual(lookup.cells, beforeCells) ||
		lookup.topology != beforeTopology || lookup.nextEpoch != beforeEpoch {
		t.Fatal("cancelled singleton definition published live lookup state")
	}
	if trace := receiver.fields["@trace"]; trace.kind != rubyInteger || trace.integer != 33 {
		t.Fatalf("cancelled definition executed the method body: trace=%#v", trace)
	}
	if receiver.dispatch != dispatch || receiver.shape != shape {
		t.Fatal("cancelled definition changed object dispatch or shape")
	}
	assertSingletonScratchClear(t, lookup)
	if err := runtime.Close(); err != nil {
		t.Fatalf("close cancelled-prefix owner: %v", err)
	}
}

func singletonOwnerBeforeDefinition(t *testing.T, image *lookupImage) *lookupOwner {
	t.Helper()
	owner, err := newLookupOwner(image)
	if err != nil {
		t.Fatalf("new lookup owner: %v", err)
	}
	for index := 0; index < len(image.mutations)-1; index++ {
		if err := owner.apply(context.Background(), image.mutations[index]); err != nil {
			t.Fatalf("apply mutation %d: %v", index+1, err)
		}
	}
	return owner
}

func changedSingletonCells(before, after []resolutionCell) int {
	if len(before) != len(after) {
		return -1
	}
	changed := 0
	for index := range before {
		if before[index] != after[index] {
			changed++
		}
	}
	return changed
}

func assertSingletonScratchClear(t *testing.T, owner *lookupOwner) {
	t.Helper()
	for index, node := range owner.routeScratch {
		if node != 0 {
			t.Fatalf("route scratch %d retained node %d", index, node)
		}
	}
	for index, mark := range owner.routeMarks {
		if mark != 0 {
			t.Fatalf("route mark %d retained %d", index, mark)
		}
	}
}
