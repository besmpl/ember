package generated

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

const (
	target41GeneratedSourceBytes  = 79_361
	target41GeneratedSourceLF     = 2_344
	target41GeneratedSourceSHA256 = "b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46"
)

func TestLookupOwnerUsesTheFixedProofClosureLayout(t *testing.T) {
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"entry", unsafe.Sizeof(rubyEntry{}), 12},
		{"receipt", unsafe.Sizeof(rubyReceipt{}), 16},
		{"cell", unsafe.Sizeof(rubyCell{}), 24},
		{"arm", unsafe.Sizeof(rubyArm{}), 32},
		{"node", unsafe.Sizeof(rubyNode{}), 8},
		{"attachment", unsafe.Sizeof(rubyAttachment{}), 12},
		{"mutation", unsafe.Sizeof(rubyMutation{}), 56},
		{"route workspace", unsafe.Sizeof(rubyRouteWorkspace{}), 40},
		{"lookup", unsafe.Sizeof(rubyLookup{}), 448},
		{"site", unsafe.Sizeof(rubySite{}), 64},
		{"object", unsafe.Sizeof(rubyObject{}), 32},
		{"stats", unsafe.Sizeof(Stats{}), 64},
		// Three 4-byte atomic.Bool owners precede 4 bytes of alignment for
		// rubyLookup; the remaining fixed fields end on an 8-byte boundary.
		{"engine", unsafe.Sizeof(Engine{}), 824},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s size = %d, want %d", test.name, test.got, test.want)
		}
	}
	if rubyNodeCount != 8 || rubyRowCount != 5 || rubySelectorCount != 2 || rubyAttachmentCount != 2 || len((rubySite{}).arms) != 2 {
		t.Fatalf("generated closure = nodes %d rows %d selectors %d attachments %d arms %d, want 8/5/2/2/2", rubyNodeCount, rubyRowCount, rubySelectorCount, rubyAttachmentCount, len((rubySite{}).arms))
	}
}

func TestLookupResetIsColdAndSourceDerived(t *testing.T) {
	engine := NewEngine()
	wantEntries := [rubyEntryCount]rubyEntry{
		{slot: 3, definition: rubyBaseLabelInitialDefinition, kind: rubyEntryDefined, visibility: rubyPublic},
		{slot: 4, definition: rubySavedLabelDefinition, kind: rubyEntryDefined, visibility: rubyPublic},
		{}, {},
		{slot: 10, definition: rubyBetaLabelDefinition, kind: rubyEntryDefined, visibility: rubyPublic},
		{},
		{slot: 12, definition: rubyGammaLabelDefinition, kind: rubyEntryDefined, visibility: rubyPublic},
		{},
	}
	if engine.lookup.entries != wantEntries || engine.lookup.topology != 0 || engine.lookup.nextEpoch != 10 || !engine.lookup.validateAll() || engine.poisoned.Load() {
		t.Fatalf("cold lookup = %#v, poisoned=%v", engine.lookup, engine.poisoned.Load())
	}
	wantCells := [rubyCellCount]rubyCell{
		{epoch: 1, receipt: rubyReceipt{owner: rubyBaseNode, slot: 3, definition: rubyBaseLabelInitialDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 2, receipt: rubyReceipt{owner: rubyBaseNode, slot: 4, definition: rubySavedLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 3, receipt: rubyReceipt{owner: rubyBetaNode, slot: 10, definition: rubyBetaLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 4, receipt: rubyReceipt{owner: rubyBaseNode, slot: 4, definition: rubySavedLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 5, receipt: rubyReceipt{owner: rubyGammaNode, slot: 12, definition: rubyGammaLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 6, receipt: rubyReceipt{owner: rubyBaseNode, slot: 4, definition: rubySavedLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 7, receipt: rubyReceipt{owner: rubyBaseNode, slot: 3, definition: rubyBaseLabelInitialDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 8, receipt: rubyReceipt{owner: rubyBaseNode, slot: 4, definition: rubySavedLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 9, receipt: rubyReceipt{owner: rubyBaseNode, slot: 3, definition: rubyBaseLabelInitialDefinition, outcome: rubyResolved, visibility: rubyPublic}},
		{epoch: 10, receipt: rubyReceipt{owner: rubyBaseNode, slot: 4, definition: rubySavedLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}},
	}
	if engine.lookup.cells != wantCells || engine.labelSite != (rubySite{}) || engine.savedSite != (rubySite{}) || engine.publicSite != (rubySite{}) || engine.singletonSite != (rubySite{}) || engine.stats != (Stats{}) {
		t.Fatalf("cold cells/sites/stats = %#v / %#v / %#v / %#v / %#v / %#v", engine.lookup.cells, engine.labelSite, engine.savedSite, engine.publicSite, engine.singletonSite, engine.stats)
	}
}

func TestTarget41GeneratedRunMatchesFrozenResultAndStatistics(t *testing.T) {
	engine := NewEngine()
	result, err := engine.Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Same: true, Other: false,
		Before: 1, BetaBefore: 3, GammaBefore: 4, AlphaWarm: 1, BetaWarm: 3,
		After: 2, AlphaAfterHit: 2, BetaAfter: 3, GammaAfter: 4, Saved: 1,
		PublicBefore: 2, ProtectedRejected: 5, PrivateRejected: 5, PrivateSent: 2, PrivateBeta: 3, PublicRestored: 2,
		BetaRemoved: 2, Returned: 14, Value: 106, Trace: 66776777124532, BetaTrace: 88887, GammaTrace: 99,
		Topology:  TopologyResult{DeltaBefore: 2, DeltaIncluded: 11, BetaRoute: 2, BetaDefined: 22, DeltaRedefined: 13, SavedStable: 1, Trace: 713726},
		Singleton: SingletonResult{Before: 13, Warm: 13, PeerBefore: 13, After: 44, AfterHit: 44, PeerAfter: 13, Trace: 334433},
	}
	if result != want {
		t.Fatalf("Target41 result = %#v, want %#v", result, want)
	}
	wantStats := Stats{Hits: 9, ColdAdmissions: 7, StaleMisses: 6, Repairs: 6, UncachedFallbacks: 5, Admissions: 7, OccupiedArms: 7}
	if stats := engine.Stats(); stats != wantStats {
		t.Fatalf("Target41 stats = %#v, want %#v", stats, wantStats)
	}
	if engine.lookup.topology != 0b11 || !engine.lookup.validateAll() {
		t.Fatalf("Target41 final lookup topology=%02b valid=%v", engine.lookup.topology, engine.lookup.validateAll())
	}
}

func TestTarget41GeneratedRouteBuilderMatchesIndependentAllMaskLiterals(t *testing.T) {
	want := [4][5][]rubyNodeID{
		{{2, 1}, {3, 1}, {4, 1}, {7, 2, 1}, {8, 2, 1}},
		{{2, 5, 1}, {3, 1}, {4, 1}, {7, 2, 5, 1}, {8, 2, 5, 1}},
		{{2, 1}, {6, 3, 1}, {4, 1}, {7, 2, 1}, {8, 2, 1}},
		{{2, 5, 1}, {6, 3, 1}, {4, 1}, {7, 2, 5, 1}, {8, 2, 5, 1}},
	}
	var workspace rubyRouteWorkspace
	for mask, rows := range want {
		for rowIndex, wantPath := range rows {
			length, ok := rubyBuildRoute(uint64(mask), rubyDispatchID(rowIndex+1), &workspace)
			if !ok || length != len(wantPath) || !reflect.DeepEqual(workspace.nodes[:length], wantPath) {
				t.Errorf("mask %02b row %d route = %v/%d/%v, want %v", mask, rowIndex+1, ok, length, workspace.nodes[:length], wantPath)
			}
			for index, node := range workspace.nodes[length:] {
				if node != 0 {
					t.Errorf("mask %02b row %d retained route suffix %d=%d", mask, rowIndex+1, length+index, node)
					break
				}
			}
			for index, mark := range workspace.marks {
				if mark != 0 {
					t.Errorf("mask %02b row %d retained mark %d=%d", mask, rowIndex+1, index, mark)
					break
				}
			}
		}
	}

	var total int
	failed := false
	allocations := testing.AllocsPerRun(1_000, func() {
		total = 0
		for mask := uint64(0); mask < 4; mask++ {
			for dispatch := rubyDispatchID(1); dispatch <= rubyRowCount; dispatch++ {
				length, ok := rubyBuildRoute(mask, dispatch, &workspace)
				if !ok {
					failed = true
					return
				}
				total += length
			}
		}
	})
	if allocations != 0 || failed || total != 56 {
		t.Fatalf("generated all-mask derivation allocations=%g failed=%v slots=%d, want 0/false/56", allocations, failed, total)
	}
}

func TestTarget41GeneratedLookupFactsAreOneExactClosedProjection(t *testing.T) {
	wantNodes := []rubyNode{
		{kind: rubyNodeClass},
		{superclass: rubyBaseNode, attachmentCount: 1, kind: rubyNodeClass},
		{superclass: rubyBaseNode, attachmentStart: 1, attachmentCount: 1, kind: rubyNodeClass},
		{superclass: rubyBaseNode, attachmentStart: 2, kind: rubyNodeClass},
		{attachmentStart: 2, kind: rubyNodeModule},
		{attachmentStart: 2, kind: rubyNodeModule},
		{superclass: rubyAlphaNode, attachmentStart: 2, kind: rubyNodeClass},
		{superclass: rubyAlphaNode, attachmentStart: 2, kind: rubyNodeSingleton},
	}
	for index, want := range wantNodes {
		if got, ok := rubyNodeFact(rubyNodeID(index + 1)); !ok || got != want {
			t.Errorf("node %d = %#v/%v, want %#v/true", index+1, got, ok, want)
		}
	}
	if _, ok := rubyNodeFact(rubyNodeCount + 1); ok {
		t.Fatal("node fact accepted an ID outside the eight-node closure")
	}
	wantShapes := []rubyShapeID{rubyAlphaShape, rubyBetaShape, rubyGammaShape, rubyDeltaShape, rubyAlphaShape}
	for index, want := range wantShapes {
		if got, ok := rubyDispatchShape(rubyDispatchID(index + 1)); !ok || got != want {
			t.Errorf("dispatch %d shape = %d/%v, want %d/true", index+1, got, ok, want)
		}
	}
	if rubySingletonDispatch == rubyAlphaDispatch || rubySingletonLabelDefinition != 17 || rubySingletonLabelSlot != 15 || rubySingletonDefineMutation != 24 {
		t.Fatalf("singleton discriminants dispatch/definition/slot/mutation = %d/%d/%d/%d", rubySingletonDispatch, rubySingletonLabelDefinition, rubySingletonLabelSlot, rubySingletonDefineMutation)
	}

	definitions := 0
	for id := rubyDefinitionID(1); id <= 17; id++ {
		if _, ok := rubyDefinitionFact(id); ok {
			definitions++
		}
	}
	mutations := 0
	for id := uint32(1); id <= 24; id++ {
		if _, ok := rubyMutationFact(id); ok {
			mutations++
		}
	}
	if definitions != 9 || mutations != 12 {
		t.Fatalf("emitted definition/mutation facts = %d/%d, want 9/12", definitions, mutations)
	}

	matches := []struct {
		dispatch   rubyDispatchID
		shape      rubyShapeID
		selector   rubySelectorID
		definition rubyDefinitionID
		slot       rubySlotID
		target     rubyTargetID
	}{
		{1, 1, rubyLabelSelector, 3, 3, 1},
		{2, 2, rubyLabelSelector, 10, 10, 2},
		{3, 3, rubyLabelSelector, 12, 12, 3},
		{1, 1, rubyLabelSelector, 13, 3, 4},
		{2, 2, rubyLabelSelector, 13, 3, 4},
		{1, 1, rubySavedSelector, 4, 4, 5},
		{4, 4, rubyLabelSelector, 13, 3, 4},
		{4, 4, rubyLabelSelector, 14, 13, 6},
		{2, 2, rubyLabelSelector, 15, 14, 7},
		{4, 4, rubyLabelSelector, 16, 13, 8},
		{1, 1, rubyLabelSelector, 16, 13, 8},
		{5, 1, rubyLabelSelector, 16, 13, 8},
		{5, 1, rubyLabelSelector, 17, 15, 9},
	}
	seenTargets := make(map[rubyTargetID]bool, 9)
	for index, match := range matches {
		receipt := rubyReceipt{slot: match.slot, definition: match.definition, outcome: rubyResolved, visibility: rubyPublic}
		got, ok := rubyTargetFor(match.dispatch, match.shape, match.selector, receipt)
		if !ok || got != match.target {
			t.Errorf("target match %d = %d/%v, want %d/true", index+1, got, ok, match.target)
		}
		seenTargets[got] = true
	}
	if len(matches) != 13 || len(seenTargets) != 9 {
		t.Fatalf("target matches/targets = %d/%d, want 13/9", len(matches), len(seenTargets))
	}
	if _, ok := rubyTargetFor(rubyAlphaDispatch, rubyBetaShape, rubyLabelSelector, rubyReceipt{slot: 13, definition: 16, outcome: rubyResolved, visibility: rubyPublic}); ok {
		t.Fatal("target matcher admitted a wrong shape")
	}

	sites := [...]uint32{rubyReadLabelSite, rubyReadSavedSite, rubyReadPublicSite, rubyReadSingletonSite}
	seenSites := make(map[uint32]bool, len(sites))
	for _, site := range sites {
		if site == 0 || seenSites[site] {
			t.Fatalf("generated call sites are not four nonzero distinct IDs: %v", sites)
		}
		seenSites[site] = true
	}
	if len(sites)*len((rubySite{}).arms) != 8 {
		t.Fatalf("generated site arms = %d, want 8", len(sites)*len((rubySite{}).arms))
	}
}

func TestTarget39GeneratedTopologyTransactionsPreserveK0Sites(t *testing.T) {
	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 1_024, frames: 64, objects: 16}
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 10}
	beta := rubyObject{id: 2, dispatch: rubyBetaDispatch, shape: rubyBetaShape, value: 10}
	if value, err := engine.readLabel(&control, &beta); err != nil || value != 3 {
		t.Fatalf("warm Beta = %d, %v", value, err)
	}
	if value, err := engine.readSaved(&control, &alpha); err != nil || value != 1 {
		t.Fatalf("warm saved Alpha = %d, %v", value, err)
	}

	applyAndWant := func(mutation uint32, wantChanged []int, wantTopology uint64) {
		t.Helper()
		beforeCells, beforeEpoch := engine.lookup.cells, engine.lookup.nextEpoch
		beforeLabel, beforeSaved, beforePublic, beforeSingleton := engine.labelSite, engine.savedSite, engine.publicSite, engine.singletonSite
		if err := engine.lookup.apply(&control, mutation); err != nil {
			t.Fatalf("apply mutation %d: %v", mutation, err)
		}
		changed := target39GeneratedChangedCells(beforeCells, engine.lookup.cells)
		if !reflect.DeepEqual(changed, wantChanged) || engine.lookup.nextEpoch != beforeEpoch+uint64(len(wantChanged)) || engine.lookup.topology != wantTopology {
			t.Fatalf("mutation %d changed=%v epoch=%d/%d topology=%02b, want %v/+%d/%02b", mutation, changed, beforeEpoch, engine.lookup.nextEpoch, engine.lookup.topology, wantChanged, len(wantChanged), wantTopology)
		}
		if engine.labelSite != beforeLabel || engine.savedSite != beforeSaved || engine.publicSite != beforePublic || engine.singletonSite != beforeSingleton {
			t.Fatalf("mutation %d traversed or rewrote a PIC site", mutation)
		}
	}

	alphaLabel, _ := rubyCellIndex(rubyAlphaDispatch, rubyLabelSelector)
	betaLabel, _ := rubyCellIndex(rubyBetaDispatch, rubyLabelSelector)
	deltaLabel, _ := rubyCellIndex(rubyDeltaDispatch, rubyLabelSelector)
	singletonLabel, _ := rubyCellIndex(rubySingletonDispatch, rubyLabelSelector)
	applyAndWant(rubyIncludedLabelInitialMutation, nil, 0b00)

	before := engine.lookup
	canceled := rubyControl{ctx: &cancelAfterContext{Context: context.Background(), cancelAt: 7}}
	if err := engine.lookup.apply(&canceled, rubyAlphaIncludeMutation); !errors.Is(err, context.Canceled) || engine.lookup != before {
		t.Fatalf("include final-poll cancellation = %v, published=%v", err, engine.lookup != before)
	}
	applyAndWant(rubyAlphaIncludeMutation, []int{alphaLabel, deltaLabel, singletonLabel}, 0b01)

	before = engine.lookup
	canceled = rubyControl{ctx: &cancelAfterContext{Context: context.Background(), cancelAt: 7}}
	if err := engine.lookup.apply(&canceled, rubyBetaPrependMutation); !errors.Is(err, context.Canceled) || engine.lookup != before {
		t.Fatalf("prepend final-poll cancellation = %v, published=%v", err, engine.lookup != before)
	}
	applyAndWant(rubyBetaPrependMutation, nil, 0b11)

	beforeBetaArm, beforeStats := engine.labelSite.arms[1], engine.stats
	if value, err := engine.readLabel(&control, &beta); err != nil || value != 3 {
		t.Fatalf("empty-prepend Beta hit = %d, %v", value, err)
	}
	if engine.labelSite.arms[1] != beforeBetaArm || engine.stats.Hits != beforeStats.Hits+1 {
		t.Fatalf("empty-prepend hit arm/stats = %#v/%#v, before %#v/%#v", engine.labelSite.arms[1], engine.stats, beforeBetaArm, beforeStats)
	}

	applyAndWant(rubyPrependedLabelMutation, []int{betaLabel}, 0b11)
	if value, err := engine.readLabel(&control, &beta); err != nil || value != 22 {
		t.Fatalf("late PrependedLabel repair = %d, %v", value, err)
	}
	if engine.labelSite.arms[1].target != rubyPrependedLabelTarget || engine.labelSite.arms[1].epoch != engine.lookup.cells[betaLabel].epoch {
		t.Fatalf("late PrependedLabel arm = %#v", engine.labelSite.arms[1])
	}

	applyAndWant(rubyIncludedLabelRedefinedMutation, []int{alphaLabel, deltaLabel, singletonLabel}, 0b11)
	beforeSavedArm, beforeStats := engine.savedSite.arms[0], engine.stats
	if value, err := engine.readSaved(&control, &alpha); err != nil || value != 1 {
		t.Fatalf("saved alias after include redefine = %d, %v", value, err)
	}
	if engine.savedSite.arms[0] != beforeSavedArm || engine.stats.Hits != beforeStats.Hits+1 {
		t.Fatalf("saved stable hit arm/stats = %#v/%#v, before %#v/%#v", engine.savedSite.arms[0], engine.stats, beforeSavedArm, beforeStats)
	}
}

func TestTarget41GeneratedSingletonSiteScheduleIsReceiverSpecific(t *testing.T) {
	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 1_024, frames: 64, objects: 16}
	target41ApplyC1(t, engine, &control)

	alphaLabel, _ := rubyCellIndex(rubyAlphaDispatch, rubyLabelSelector)
	deltaLabel, _ := rubyCellIndex(rubyDeltaDispatch, rubyLabelSelector)
	singletonLabel, _ := rubyCellIndex(rubySingletonDispatch, rubyLabelSelector)
	if engine.lookup.nextEpoch != 31 || engine.lookup.cells[alphaLabel].epoch != 29 || engine.lookup.cells[deltaLabel].epoch != 30 || engine.lookup.cells[singletonLabel].epoch != 31 {
		t.Fatalf("pre-singleton epochs next/Alpha/Delta/singleton = %d/%d/%d/%d, want 31/29/30/31", engine.lookup.nextEpoch, engine.lookup.cells[alphaLabel].epoch, engine.lookup.cells[deltaLabel].epoch, engine.lookup.cells[singletonLabel].epoch)
	}
	receiver := rubyObject{id: 1, dispatch: rubySingletonDispatch, shape: rubyAlphaShape, value: rubySingletonValue}
	peer := rubyObject{id: 2, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: rubySingletonValue}

	if value, err := engine.readSingleton(&control, &receiver); err != nil || value != 13 {
		t.Fatalf("receiver cold = %d, %v", value, err)
	}
	wantReceiverBefore := rubyArm{epoch: 31, dispatch: rubySingletonDispatch, shape: rubyAlphaShape, slot: rubyIncludedLabelSlot, definition: rubyIncludedLabelRedefinedDefinition, target: rubyIncludedLabelRedefinedTarget}
	if engine.singletonSite.arms[0] != wantReceiverBefore || receiver.trace != 3 {
		t.Fatalf("receiver cold arm/trace = %#v/%d, want %#v/3", engine.singletonSite.arms[0], receiver.trace, wantReceiverBefore)
	}
	if value, err := engine.readSingleton(&control, &receiver); err != nil || value != 13 {
		t.Fatalf("receiver warm = %d, %v", value, err)
	}
	if value, err := engine.readSingleton(&control, &peer); err != nil || value != 13 {
		t.Fatalf("peer cold = %d, %v", value, err)
	}
	wantPeer := rubyArm{epoch: 29, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, slot: rubyIncludedLabelSlot, definition: rubyIncludedLabelRedefinedDefinition, target: rubyIncludedLabelRedefinedTarget}
	if engine.singletonSite.arms[1] != wantPeer || receiver.trace != 33 || peer.trace != 3 {
		t.Fatalf("pre-mutation site/traces = %#v / %d/%d", engine.singletonSite, receiver.trace, peer.trace)
	}

	beforeSite, beforeStats := engine.singletonSite, engine.stats
	beforeCells := engine.lookup.cells
	if err := engine.lookup.apply(&control, rubySingletonDefineMutation); err != nil {
		t.Fatal(err)
	}
	if engine.lookup.nextEpoch != 32 || engine.lookup.cells[singletonLabel].epoch != 32 ||
		engine.lookup.cells[singletonLabel].receipt != (rubyReceipt{owner: rubySingletonNode, slot: rubySingletonLabelSlot, definition: rubySingletonLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}) ||
		engine.lookup.cells[alphaLabel] != beforeCells[alphaLabel] || engine.lookup.cells[deltaLabel] != beforeCells[deltaLabel] ||
		engine.singletonSite != beforeSite || engine.stats != beforeStats {
		t.Fatalf("singleton mutation lookup/site/stats = %#v / %#v / %#v", engine.lookup, engine.singletonSite, engine.stats)
	}

	if value, err := engine.readSingleton(&control, &receiver); err != nil || value != 44 {
		t.Fatalf("receiver repair = %d, %v", value, err)
	}
	wantReceiverAfter := rubyArm{epoch: 32, dispatch: rubySingletonDispatch, shape: rubyAlphaShape, slot: rubySingletonLabelSlot, definition: rubySingletonLabelDefinition, target: rubySingletonLabelTarget}
	if engine.singletonSite.arms[0] != wantReceiverAfter || engine.singletonSite.arms[1] != wantPeer {
		t.Fatalf("post-repair singleton site = %#v, want %#v/%#v", engine.singletonSite, wantReceiverAfter, wantPeer)
	}
	if value, err := engine.readSingleton(&control, &receiver); err != nil || value != 44 {
		t.Fatalf("receiver repaired hit = %d, %v", value, err)
	}
	if value, err := engine.readSingleton(&control, &peer); err != nil || value != 13 {
		t.Fatalf("peer retained hit = %d, %v", value, err)
	}
	if receiver.trace*100+peer.trace != 334433 {
		t.Fatalf("singleton trace = %d, want 334433", receiver.trace*100+peer.trace)
	}
	wantDelta := Stats{Hits: 3, ColdAdmissions: 2, StaleMisses: 1, Repairs: 1, Admissions: 2, OccupiedArms: 2}
	if engine.stats != wantDelta || !rubySingletonSiteValid(&engine.singletonSite, engine.lookup.nextEpoch) {
		t.Fatalf("singleton stats/site = %#v / valid=%v, want %#v / true", engine.stats, rubySingletonSiteValid(&engine.singletonSite, engine.lookup.nextEpoch), wantDelta)
	}

	failed := false
	allocations := testing.AllocsPerRun(1_000, func() {
		receiver.trace, peer.trace = 0, 0
		control := rubyControl{ctx: context.Background(), steps: 16, frames: 8}
		if _, err := engine.readSingleton(&control, &receiver); err != nil {
			failed = true
		}
		if _, err := engine.readSingleton(&control, &peer); err != nil {
			failed = true
		}
	})
	if allocations != 0 || failed {
		t.Fatalf("warmed receiver/peer allocations = %.2f, failed=%v", allocations, failed)
	}
}

func TestTarget41SingletonMutationCancellationEpochAndTargetPublication(t *testing.T) {
	for cancelAt := 1; cancelAt <= 7; cancelAt++ {
		engine := NewEngine()
		control := rubyControl{ctx: context.Background(), steps: 1_024, frames: 64, objects: 16}
		target41ApplyC1(t, engine, &control)
		before := engine.lookup
		canceled := rubyControl{ctx: &cancelAfterContext{Context: context.Background(), cancelAt: cancelAt}}
		if err := engine.lookup.apply(&canceled, rubySingletonDefineMutation); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel checkpoint %d = %v, want canceled", cancelAt, err)
		}
		if engine.lookup != before {
			t.Fatalf("cancel checkpoint %d published singleton lookup state", cancelAt)
		}
	}

	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 1_024, frames: 64, objects: 16}
	target41ApplyC1(t, engine, &control)
	engine.lookup.nextEpoch = ^uint64(0) - 1
	if err := engine.lookup.apply(nil, rubySingletonDefineMutation); err != nil || engine.lookup.nextEpoch != ^uint64(0) {
		t.Fatalf("maximum safe singleton epoch = %d, %v", engine.lookup.nextEpoch, err)
	}

	engine = NewEngine()
	control = rubyControl{ctx: context.Background(), steps: 1_024, frames: 64, objects: 16}
	target41ApplyC1(t, engine, &control)
	receiver := rubyObject{id: 1, dispatch: rubySingletonDispatch, shape: rubyAlphaShape, value: rubySingletonValue}
	if _, err := engine.readSingleton(&control, &receiver); err != nil {
		t.Fatal(err)
	}
	if err := engine.lookup.apply(&control, rubySingletonDefineMutation); err != nil {
		t.Fatal(err)
	}
	beforeArm, beforeStats := engine.singletonSite.arms[0], engine.stats
	canceled := rubyControl{ctx: &cancelAfterContext{Context: context.Background(), cancelAt: 6}, steps: 64, frames: 8}
	if _, err := engine.readSingleton(&canceled, &receiver); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-target cancellation = %v, want canceled", err)
	}
	if receiver.trace != 34 || engine.singletonSite.arms[0] != beforeArm || engine.stats != beforeStats {
		t.Fatalf("post-target cancellation trace/arm/stats = %d/%#v/%#v, want 34/%#v/%#v", receiver.trace, engine.singletonSite.arms[0], engine.stats, beforeArm, beforeStats)
	}
}

func target41ApplyC1(t *testing.T, engine *Engine, control *rubyControl) {
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

func target39GeneratedChangedCells(before, after [rubyCellCount]rubyCell) []int {
	var changed []int
	for index := range before {
		if before[index] != after[index] {
			changed = append(changed, index)
		}
	}
	return changed
}

func TestLookupMutationAndSiteRepairAreSelective(t *testing.T) {
	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 512, frames: 16, objects: 8}
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
	beta := rubyObject{id: 2, dispatch: rubyBetaDispatch, shape: rubyBetaShape, value: 4}
	gamma := rubyObject{id: 3, dispatch: rubyGammaDispatch, shape: rubyGammaShape, value: 4}

	if value, err := engine.readLabel(&control, &alpha); err != nil || value != 1 {
		t.Fatalf("cold Alpha = %d, %v", value, err)
	}
	if value, err := engine.readLabel(&control, &beta); err != nil || value != 3 {
		t.Fatalf("cold Beta = %d, %v", value, err)
	}
	beforeGammaSite := engine.labelSite
	if value, err := engine.readLabel(&control, &gamma); err != nil || value != 4 {
		t.Fatalf("cold Gamma = %d, %v", value, err)
	}
	if engine.labelSite != beforeGammaSite {
		t.Fatal("Gamma changed the two admitted label arms")
	}

	beforeCells, beforeLabel, beforeSaved := engine.lookup.cells, engine.labelSite, engine.savedSite
	if err := engine.lookup.apply(&control, rubyBaseRedefineMutation); err != nil {
		t.Fatal(err)
	}
	alphaLabel, _ := rubyCellIndex(rubyAlphaDispatch, rubyLabelSelector)
	betaLabel, _ := rubyCellIndex(rubyBetaDispatch, rubyLabelSelector)
	gammaLabel, _ := rubyCellIndex(rubyGammaDispatch, rubyLabelSelector)
	deltaLabel, _ := rubyCellIndex(rubyDeltaDispatch, rubyLabelSelector)
	singletonLabel, _ := rubyCellIndex(rubySingletonDispatch, rubyLabelSelector)
	if engine.lookup.cells[alphaLabel] == beforeCells[alphaLabel] || engine.lookup.cells[deltaLabel] == beforeCells[deltaLabel] ||
		engine.lookup.cells[singletonLabel] == beforeCells[singletonLabel] ||
		engine.lookup.cells[betaLabel] != beforeCells[betaLabel] || engine.lookup.cells[gammaLabel] != beforeCells[gammaLabel] {
		t.Fatalf("selective redefine cells before=%#v after=%#v", beforeCells, engine.lookup.cells)
	}
	for dispatch := rubyDispatchID(1); dispatch <= rubyRowCount; dispatch++ {
		index, _ := rubyCellIndex(dispatch, rubySavedSelector)
		if engine.lookup.cells[index] != beforeCells[index] {
			t.Fatalf("redefine changed saved cell %d", dispatch)
		}
	}
	if engine.labelSite != beforeLabel || engine.savedSite != beforeSaved {
		t.Fatal("mutation traversed or rewrote site arms")
	}

	betaArm := engine.labelSite.arms[1]
	if value, err := engine.readLabel(&control, &alpha); err != nil || value != 2 {
		t.Fatalf("repaired Alpha = %d, %v", value, err)
	}
	if engine.labelSite.arms[0].target != 4 || engine.labelSite.arms[0].epoch != engine.lookup.cells[alphaLabel].epoch || engine.labelSite.arms[1] != betaArm {
		t.Fatalf("repair arms = %#v", engine.labelSite.arms)
	}
	if value, err := engine.readSaved(&control, &alpha); err != nil || value != 1 {
		t.Fatalf("saved alias = %d, %v", value, err)
	}
	savedArm := engine.savedSite
	if value, err := engine.readPublic(&control, &alpha); err != nil || value != 2 {
		t.Fatalf("public Alpha = %d, %v", value, err)
	}
	publicArm := engine.publicSite.arms[0]
	for _, mutation := range []uint32{rubyBaseProtectedMutation, rubyBasePrivateMutation} {
		beforeCells, beforePublic, beforeStats, beforeTrace := engine.lookup.cells, engine.publicSite, engine.stats, alpha.trace
		if err := engine.lookup.apply(&control, mutation); err != nil {
			t.Fatal(err)
		}
		if engine.lookup.cells[alphaLabel] == beforeCells[alphaLabel] || engine.lookup.cells[deltaLabel] == beforeCells[deltaLabel] ||
			engine.lookup.cells[singletonLabel] == beforeCells[singletonLabel] ||
			engine.lookup.cells[betaLabel] != beforeCells[betaLabel] || engine.lookup.cells[gammaLabel] != beforeCells[gammaLabel] {
			t.Fatalf("visibility mutation %d was not selective", mutation)
		}
		if value, err := engine.readPublic(&control, &alpha); err != nil || value != rubyPublicRejected {
			t.Fatalf("visibility mutation %d public_send = %d, %v", mutation, value, err)
		}
		if alpha.trace != beforeTrace || engine.publicSite != beforePublic || engine.stats != beforeStats {
			t.Fatalf("visibility rejection %d published an effect/arm/stat", mutation)
		}
	}
	if value, err := engine.readLabel(&control, &alpha); err != nil || value != 2 {
		t.Fatalf("private send = %d, %v", value, err)
	}
	if value, err := engine.readPublic(&control, &beta); err != nil || value != 3 {
		t.Fatalf("shadowed Beta public_send = %d, %v", value, err)
	}
	if engine.publicSite.arms[0] != publicArm || engine.publicSite.arms[1].dispatch != rubyBetaDispatch {
		t.Fatalf("private Base changed public site arms unexpectedly: %#v", engine.publicSite.arms)
	}
	if err := engine.lookup.apply(&control, rubyBasePublicMutation); err != nil {
		t.Fatal(err)
	}
	if value, err := engine.readPublic(&control, &alpha); err != nil || value != 2 {
		t.Fatalf("restored public_send = %d, %v", value, err)
	}
	if engine.publicSite.arms[0].epoch != engine.lookup.cells[alphaLabel].epoch || engine.publicSite.arms[1].dispatch != rubyBetaDispatch {
		t.Fatalf("public restoration did not selectively repair Alpha: %#v", engine.publicSite.arms)
	}

	beforeCells, beforeLabel = engine.lookup.cells, engine.labelSite
	if err := engine.lookup.apply(&control, rubyBetaRemoveMutation); err != nil {
		t.Fatal(err)
	}
	if engine.lookup.cells[betaLabel] == beforeCells[betaLabel] || engine.lookup.cells[alphaLabel] != beforeCells[alphaLabel] ||
		engine.lookup.cells[gammaLabel] != beforeCells[gammaLabel] || engine.lookup.cells[deltaLabel] != beforeCells[deltaLabel] ||
		engine.lookup.cells[singletonLabel] != beforeCells[singletonLabel] {
		t.Fatalf("selective remove cells before=%#v after=%#v", beforeCells, engine.lookup.cells)
	}
	if engine.labelSite != beforeLabel || engine.savedSite != savedArm {
		t.Fatal("remove traversed a site")
	}
	if value, err := engine.readLabel(&control, &beta); err != nil || value != 2 {
		t.Fatalf("removed Beta = %d, %v", value, err)
	}
	if engine.labelSite.arms[1].target != 0 && engine.labelSite.arms[1].target != 4 {
		t.Fatalf("Beta repair target = %d", engine.labelSite.arms[1].target)
	}

	beforeTrace := gamma.trace
	if err := engine.lookup.apply(&control, rubyGammaUndefMutation); err != nil {
		t.Fatal(err)
	}
	cell := engine.lookup.cells[gammaLabel]
	if cell.receipt.outcome != rubyUndef || cell.receipt.owner != rubyGammaNode || cell.receipt.definition != 0 || gamma.trace != beforeTrace {
		t.Fatalf("Gamma undef cell/effect = %#v trace=%d", cell, gamma.trace)
	}
	if _, ok := rubyTargetFor(gamma.dispatch, gamma.shape, rubyLabelSelector, cell.receipt); ok {
		t.Fatal("undef receipt admitted a target")
	}
}

func TestLookupApplyCancellationAndEpochFailurePublishNothing(t *testing.T) {
	for cancelAt := 1; cancelAt <= 7; cancelAt++ {
		engine := NewEngine()
		before := engine.lookup
		ctx := &cancelAfterContext{Context: context.Background(), cancelAt: cancelAt}
		control := rubyControl{ctx: ctx, steps: 64, frames: 8, objects: 4}
		if err := engine.lookup.apply(&control, rubyBaseRedefineMutation); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel checkpoint %d = %v, want canceled", cancelAt, err)
		}
		if engine.lookup != before {
			t.Fatalf("cancel checkpoint %d published lookup state", cancelAt)
		}
	}

	engine := NewEngine()
	engine.lookup.nextEpoch = ^uint64(0)
	for index := range engine.lookup.cells {
		engine.lookup.cells[index].epoch = ^uint64(0)
	}
	before := engine.lookup
	if err := engine.lookup.apply(nil, rubyBaseRedefineMutation); !errors.Is(err, ErrInternal) {
		t.Fatalf("epoch exhaustion = %v, want internal", err)
	}
	if engine.lookup != before {
		t.Fatal("epoch exhaustion published lookup state")
	}
}

func TestCorruptArmAndCellFailBeforeLabelEffects(t *testing.T) {
	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
	if _, err := engine.readLabel(&control, &alpha); err != nil {
		t.Fatal(err)
	}
	beforeTrace := alpha.trace
	engine.labelSite.arms[0].target = 2
	if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, ErrInternal) {
		t.Fatalf("corrupt target = %v, want internal", err)
	}
	if alpha.trace != beforeTrace {
		t.Fatal("corrupt target executed a label effect")
	}

	engine = NewEngine()
	alpha.trace = 0
	index, _ := rubyCellIndex(rubyAlphaDispatch, rubyLabelSelector)
	engine.lookup.cells[index].receipt.definition = rubyBetaLabelDefinition
	if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, ErrInternal) {
		t.Fatalf("corrupt cell = %v, want internal", err)
	}
	if alpha.trace != 0 {
		t.Fatal("corrupt cell executed a label effect")
	}
}

func TestWholeSiteAndCorrelatedCorruptionFailBeforeSelectedEffects(t *testing.T) {
	t.Run("unused occupied arm", func(t *testing.T) {
		engine := NewEngine()
		control := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
		alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
		beta := rubyObject{id: 2, dispatch: rubyBetaDispatch, shape: rubyBetaShape, value: 4}
		if _, err := engine.readLabel(&control, &alpha); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.readLabel(&control, &beta); err != nil {
			t.Fatal(err)
		}
		engine.labelSite.arms[1].target = rubyGammaLabelTarget
		beforeSite, beforeStats, beforeTrace := engine.labelSite, engine.stats, alpha.trace
		if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, ErrInternal) {
			t.Fatalf("unused-arm corruption = %v, want internal", err)
		}
		if engine.labelSite != beforeSite || engine.stats != beforeStats || alpha.trace != beforeTrace {
			t.Fatal("unused-arm corruption published a selected-arm effect")
		}
	})

	t.Run("partially initialized empty arm", func(t *testing.T) {
		engine := NewEngine()
		control := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
		alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
		engine.labelSite.arms[1].epoch = 1
		beforeSite, beforeStats := engine.labelSite, engine.stats
		if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, ErrInternal) {
			t.Fatalf("partial empty arm = %v, want internal", err)
		}
		if engine.labelSite != beforeSite || engine.stats != beforeStats || alpha.trace != 0 {
			t.Fatal("partial empty arm published a label effect")
		}
	})

	t.Run("correlated cell and target", func(t *testing.T) {
		engine := NewEngine()
		control := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
		alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
		if _, err := engine.readLabel(&control, &alpha); err != nil {
			t.Fatal(err)
		}
		index, _ := rubyCellIndex(rubyAlphaDispatch, rubyLabelSelector)
		cell := engine.lookup.cells[index]
		cell.receipt = rubyReceipt{owner: rubyGammaNode, slot: rubyGammaLabelSlot, definition: rubyGammaLabelDefinition, outcome: rubyResolved, visibility: rubyPublic}
		engine.lookup.cells[index] = cell
		engine.labelSite.arms[0] = rubyArm{epoch: cell.epoch, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, slot: rubyGammaLabelSlot, definition: rubyGammaLabelDefinition, target: rubyGammaLabelTarget}
		beforeSite, beforeStats, beforeTrace := engine.labelSite, engine.stats, alpha.trace
		if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, ErrInternal) {
			t.Fatalf("correlated corruption = %v, want internal", err)
		}
		if engine.labelSite != beforeSite || engine.stats != beforeStats || alpha.trace != beforeTrace {
			t.Fatal("correlated corruption executed an incompatible target")
		}
	})
}

func TestReadSavedRetainsOrdinaryPublicVisibility(t *testing.T) {
	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
	if _, err := engine.readSaved(&control, &alpha); err != nil {
		t.Fatal(err)
	}
	index, _ := rubyCellIndex(rubyAlphaDispatch, rubySavedSelector)
	engine.lookup.cells[index].receipt.visibility = rubyPrivate
	beforeSite, beforeStats, beforeTrace := engine.savedSite, engine.stats, alpha.trace
	if _, err := engine.readSaved(&control, &alpha); !errors.Is(err, errNoMethod) {
		t.Fatalf("private saved_label = %v, want NoMethodError", err)
	}
	if engine.savedSite != beforeSite || engine.stats != beforeStats || alpha.trace != beforeTrace {
		t.Fatal("private ordinary call published a target, arm, or statistic")
	}
}

func TestWarmedLookupPreservesExactControlBoundary(t *testing.T) {
	for cancelAt := 1; cancelAt <= 5; cancelAt++ {
		engine := NewEngine()
		warm := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
		alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
		if _, err := engine.readLabel(&warm, &alpha); err != nil {
			t.Fatal(err)
		}
		ctx := &cancelAfterContext{Context: context.Background(), cancelAt: cancelAt}
		control := rubyControl{ctx: ctx, steps: 16, frames: 8}
		beforeSite, beforeStats, beforeTrace := engine.labelSite, engine.stats, alpha.trace
		if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel checkpoint %d = %v, want canceled", cancelAt, err)
		}
		if control.depth != 0 || engine.labelSite != beforeSite || engine.stats != beforeStats || alpha.trace != beforeTrace {
			t.Fatalf("cancel checkpoint %d leaked frame/effect/cache state", cancelAt)
		}
	}

	engine := NewEngine()
	warm := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
	if _, err := engine.readLabel(&warm, &alpha); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		ctx     context.Context
		steps   uint64
		frames  uint64
		wantErr error
	}{
		{name: "cancellation before frame limit", ctx: &cancelAfterContext{Context: context.Background(), cancelAt: 1}, frames: 0, wantErr: context.Canceled},
		{name: "frame before step", ctx: context.Background(), frames: 0, wantErr: ErrFrameLimit},
		{name: "step after admitted frame", ctx: context.Background(), frames: 8, wantErr: ErrStepLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := rubyControl{ctx: test.ctx, steps: test.steps, frames: test.frames}
			beforeStats, beforeTrace := engine.stats, alpha.trace
			if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, test.wantErr) {
				t.Fatalf("read = %v, want %v", err, test.wantErr)
			}
			if control.depth != 0 || engine.stats != beforeStats || alpha.trace != beforeTrace {
				t.Fatal("failed control admission leaked frame/effect/stat state")
			}
		})
	}

	ctx := &cancelAfterContext{Context: context.Background(), cancelAt: 100}
	control := rubyControl{ctx: ctx, steps: 2, frames: 3}
	beforeHits := engine.stats.Hits
	if _, err := engine.readLabel(&control, &alpha); err != nil {
		t.Fatal(err)
	}
	if ctx.calls != 5 || control.steps != 0 || control.depth != 0 || control.nextFrame != 3 || engine.stats.Hits != beforeHits+1 {
		t.Fatalf("warm controls calls=%d steps=%d depth=%d frames=%d hits=%d", ctx.calls, control.steps, control.depth, control.nextFrame, engine.stats.Hits-beforeHits)
	}

	ctx = &cancelAfterContext{Context: context.Background(), cancelAt: 100}
	control = rubyControl{ctx: ctx, steps: 2, frames: 1}
	beforeStats, beforeTrace := engine.stats, alpha.trace
	if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, ErrFrameLimit) {
		t.Fatalf("target frame failure = %v, want frame limit", err)
	}
	if ctx.calls != 3 || control.depth != 0 || engine.stats != beforeStats || alpha.trace != beforeTrace {
		t.Fatal("target error retried recovery or published an effect")
	}
}

func TestHostAbortAfterStaleTargetEffectPublishesNoRepair(t *testing.T) {
	engine := NewEngine()
	warmControl := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
	if _, err := engine.readLabel(&warmControl, &alpha); err != nil {
		t.Fatal(err)
	}
	if err := engine.lookup.apply(&warmControl, rubyBaseRedefineMutation); err != nil {
		t.Fatal(err)
	}
	staleArm := engine.labelSite.arms[0]
	beforeStats := engine.stats
	ctx := &cancelAfterContext{Context: context.Background(), cancelAt: 6}
	control := rubyControl{ctx: ctx, steps: 64, frames: 8, objects: 4}
	if _, err := engine.readLabel(&control, &alpha); !errors.Is(err, context.Canceled) {
		t.Fatalf("stale host abort = %v, want canceled", err)
	}
	if alpha.trace != 67 {
		t.Fatalf("new target effect trace = %d, want exactly one appended 7", alpha.trace)
	}
	if engine.labelSite.arms[0] != staleArm || engine.stats != beforeStats {
		t.Fatal("host abort published a repair or stats")
	}
}

func TestLookupHotHitAllocatesNothing(t *testing.T) {
	engine := NewEngine()
	control := rubyControl{ctx: context.Background(), steps: 64, frames: 8, objects: 4}
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
	if _, err := engine.readLabel(&control, &alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.readPublic(&control, &alpha); err != nil {
		t.Fatal(err)
	}
	failed := false
	allocations := testing.AllocsPerRun(1_000, func() {
		control := rubyControl{ctx: context.Background(), steps: 16, frames: 8}
		if _, err := engine.readLabel(&control, &alpha); err != nil {
			failed = true
		}
		if _, err := engine.readPublic(&control, &alpha); err != nil {
			failed = true
		}
	})
	if allocations != 0 || failed {
		t.Fatalf("warmed send/public_send allocations = %.2f, failed=%v", allocations, failed)
	}
}

func TestTarget41SingletonWarmedLeafHasNoDynamicLookupAuthority(t *testing.T) {
	source, err := os.ReadFile("ruby_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "ruby_generated.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantExisting := map[string]bool{"readLabelAfterStep": false, "readSavedAfterStep": false, "readPublicAfterStep": false}
	var singleton *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil {
			continue
		}
		if _, ok := wantExisting[function.Name.Name]; ok {
			wantExisting[function.Name.Name] = true
			ast.Inspect(function.Body, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				lower := strings.ToLower(identifier.Name)
				for _, fragment := range []string{"singleton", "allocation", "newobject"} {
					if strings.Contains(lower, fragment) {
						t.Errorf("existing warm leaf %s gained %q", function.Name.Name, identifier.Name)
					}
				}
				return true
			})
		}
		if function.Name.Name == "readSingletonAfterStep" {
			singleton = function
		}
	}
	for name, found := range wantExisting {
		if !found {
			t.Errorf("existing warm leaf %s is missing", name)
		}
	}
	if singleton == nil || singleton.Body == nil || len(singleton.Body.List) == 0 {
		t.Fatal("readSingletonAfterStep is missing")
	}
	last, ok := singleton.Body.List[len(singleton.Body.List)-1].(*ast.ReturnStmt)
	if !ok || len(last.Results) != 1 {
		t.Fatal("singleton warmed leaf has no terminal cold side exit")
	}
	call, ok := last.Results[0].(*ast.CallExpr)
	if !ok {
		t.Fatalf("singleton terminal side exit = %#v", last.Results[0])
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "readSingletonColdAfterStep" {
		t.Fatalf("singleton terminal side exit = %#v", last.Results[0])
	}
	required := map[string]bool{
		"rubySingletonDispatch":  false,
		"rubyAlphaDispatch":      false,
		"rubyAlphaShape":         false,
		"rubySingletonLabelCell": false,
		"rubyAlphaLabelCell":     false,
		"rubyArmHits":            false,
		"labelIncludedRedefined": false,
		"labelSingleton":         false,
	}
	forbidden := make(map[string]bool)
	for _, statement := range singleton.Body.List[:len(singleton.Body.List)-1] {
		ast.Inspect(statement, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.Ident:
				if _, ok := required[value.Name]; ok {
					required[value.Name] = true
				}
				lower := strings.ToLower(value.Name)
				for _, fragment := range []string{"attachment", "topology", "route", "resolve", "entry", "oracle", "snapshot", "stateid", "newobject", "calllabeltarget"} {
					if strings.Contains(lower, fragment) {
						forbidden[value.Name] = true
					}
				}
			case *ast.SelectorExpr:
				if value.Sel.Name == "id" {
					forbidden["object.id"] = true
				}
			}
			return true
		})
	}
	for name, found := range required {
		if !found {
			t.Errorf("singleton warmed leaf lacks exact guard/target %s", name)
		}
	}
	if len(forbidden) != 0 {
		t.Errorf("singleton warmed leaf contains dynamic lookup authority: %v", forbidden)
	}
}

func TestGeneratedLookupHasNoMutableGlobalOrOldDispatchAuthority(t *testing.T) {
	source, err := os.ReadFile("ruby_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if len(source) != target41GeneratedSourceBytes || strings.Count(text, "\n") != target41GeneratedSourceLF {
		t.Fatalf("generated Target41 source = %d bytes/%d LF, want %d/%d", len(source), strings.Count(text, "\n"), target41GeneratedSourceBytes, target41GeneratedSourceLF)
	}
	const wantSHA256 = target41GeneratedSourceSHA256
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != wantSHA256 {
		t.Fatalf("generated Target39 source SHA-256 = %s, want %s", got, wantSHA256)
	}
	for _, forbidden := range []string{
		"methodEpochs", "labelEpoch", "labelVersion", "sendLabel", "GuardHits", "GuardMisses", "map[",
		"sync.Map", "runtime.mapaccess", "github.com/besmpl/ember", "preparedsource", "RouteOracle", "routeOracle", "oraclePaths",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("generated source retained forbidden authority %q", forbidden)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), "ruby_generated.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if ok && general.Tok == token.VAR {
			t.Fatalf("generated source has mutable package var at %d", general.Pos())
		}
	}
	for _, value := range []any{rubyLookup{}, rubyRouteWorkspace{}, rubySite{}, rubyArm{}, rubyObject{}, Engine{}} {
		assertNoDynamicOwnerFields(t, reflect.TypeOf(value), map[reflect.Type]bool{})
	}
	lookupType := reflect.TypeOf(rubyLookup{})
	for index := 0; index < lookupType.NumField(); index++ {
		field := lookupType.Field(index)
		lower := strings.ToLower(field.Name)
		if strings.Contains(lower, "route") || strings.Contains(lower, "path") || strings.Contains(lower, "snapshot") || strings.Contains(lower, "stateid") {
			t.Errorf("generated lookup retains forbidden topology authority %s %s", field.Name, field.Type)
		}
	}
}

func assertNoDynamicOwnerFields(t *testing.T, value reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	if seen[value] {
		return
	}
	seen[value] = true
	switch value.Kind() {
	case reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan, reflect.Pointer, reflect.UnsafePointer:
		t.Fatalf("owner state retains dynamic field type %s", value)
	case reflect.Array:
		assertNoDynamicOwnerFields(t, value.Elem(), seen)
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.Type.PkgPath() == "sync/atomic" {
				continue
			}
			assertNoDynamicOwnerFields(t, field.Type, seen)
		}
	}
}

type cancelAfterContext struct {
	context.Context
	calls, cancelAt int
}

func (ctx *cancelAfterContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
