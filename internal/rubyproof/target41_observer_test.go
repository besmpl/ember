package rubyproof

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"unsafe"
)

var target41LiteralRoutes = []struct {
	mask uint64
	rows [5][]lookupNodeID
}{
	{0b00, [5][]lookupNodeID{{2, 1}, {3, 1}, {4, 1}, {7, 2, 1}, {8, 2, 1}}},
	{0b01, [5][]lookupNodeID{{2, 5, 1}, {3, 1}, {4, 1}, {7, 2, 5, 1}, {8, 2, 5, 1}}},
	{0b10, [5][]lookupNodeID{{2, 1}, {6, 3, 1}, {4, 1}, {7, 2, 1}, {8, 2, 1}}},
	{0b11, [5][]lookupNodeID{{2, 5, 1}, {6, 3, 1}, {4, 1}, {7, 2, 5, 1}, {8, 2, 5, 1}}},
}

type target41SnapshotSpan struct {
	start, length uint8
}

type target41Snapshot struct {
	rows [5]target41SnapshotSpan
}

type target41SnapshotImage struct {
	snapshots [4]target41Snapshot
	paths     [56]lookupNodeID
}

type target41SnapshotOwner struct {
	live, staged uint8
}

var target41FrozenSnapshots = target41SnapshotImage{
	snapshots: [4]target41Snapshot{
		{rows: [5]target41SnapshotSpan{{0, 2}, {2, 2}, {4, 2}, {6, 3}, {9, 3}}},
		{rows: [5]target41SnapshotSpan{{12, 3}, {15, 2}, {17, 2}, {19, 4}, {23, 4}}},
		{rows: [5]target41SnapshotSpan{{27, 2}, {29, 3}, {32, 2}, {34, 3}, {37, 3}}},
		{rows: [5]target41SnapshotSpan{{40, 3}, {43, 3}, {46, 2}, {48, 4}, {52, 4}}},
	},
	paths: [56]lookupNodeID{
		2, 1, 3, 1, 4, 1, 7, 2, 1, 8, 2, 1,
		2, 5, 1, 3, 1, 4, 1, 7, 2, 5, 1, 8, 2, 5, 1,
		2, 1, 6, 3, 1, 4, 1, 7, 2, 1, 8, 2, 1,
		2, 5, 1, 6, 3, 1, 4, 1, 7, 2, 5, 1, 8, 2, 5, 1,
	},
}

type target41AttachmentNode struct {
	superclass      lookupNodeID
	attachmentStart uint8
	attachmentCount uint8
	kind            lookupNodeKind
}

type target41AttachmentFact struct {
	target lookupNodeID
	module lookupNodeID
	kind   lookupAttachmentKind
}

type target41AttachmentImage struct {
	nodes       [8]target41AttachmentNode
	attachments [2]target41AttachmentFact
}

type target41AttachmentWorkspace struct {
	nodes [8]lookupNodeID
	marks [8]uint8
}

type target41AttachmentOwner struct {
	live, staged uint64
	workspace    target41AttachmentWorkspace
}

var target41FrozenAttachments = target41AttachmentImage{
	nodes: [8]target41AttachmentNode{
		{kind: lookupNodeClass},
		{superclass: 1, attachmentCount: 1, kind: lookupNodeClass},
		{superclass: 1, attachmentStart: 1, attachmentCount: 1, kind: lookupNodeClass},
		{superclass: 1, attachmentStart: 2, kind: lookupNodeClass},
		{attachmentStart: 2, kind: lookupNodeModule},
		{attachmentStart: 2, kind: lookupNodeModule},
		{superclass: 2, attachmentStart: 2, kind: lookupNodeClass},
		{superclass: 2, attachmentStart: 2, kind: lookupNodeSingleton},
	},
	attachments: [2]target41AttachmentFact{
		{target: 2, module: 5, kind: lookupAttachmentInclude},
		{target: 3, module: 6, kind: lookupAttachmentPrepend},
	},
}

type target41ComparatorState struct {
	entries [64]lookupEntry
	cells   [40]lookupReceipt
}

func TestTarget41LookupAndEmissionClosureAreExact(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	checked, image := program.checked, program.checked.lookup
	if image == nil {
		t.Fatal("checked lookup image is nil")
	}
	if len(image.nodes) != 8 || len(image.rows) != 5 || len(image.selectors) != 8 || len(image.definitions) != 17 ||
		len(image.mutations) != 24 || len(image.attachments) != 2 || len(image.routeOracles) != 20 || len(image.oraclePaths) != 56 {
		t.Fatalf("checked closure nodes/rows/selectors/definitions/mutations/attachments/oracles/paths = %d/%d/%d/%d/%d/%d/%d/%d, want 8/5/8/17/24/2/20/56",
			len(image.nodes), len(image.rows), len(image.selectors), len(image.definitions), len(image.mutations), len(image.attachments), len(image.routeOracles), len(image.oraclePaths))
	}
	shapes := make(map[shapeID]bool)
	for _, owner := range checked.lookupOrder {
		if owner.shape != 0 {
			shapes[owner.shape] = true
		}
	}
	if len(shapes) != 4 || checked.singletonOwner.shape != checked.classes["Alpha"].shape || checked.singletonOwner.dispatch != 5 {
		t.Fatalf("checked shapes/singleton dispatch = %v/%d, want four shapes/dispatch 5 sharing Alpha shape", shapes, checked.singletonOwner.dispatch)
	}
	for maskIndex, literal := range target41LiteralRoutes {
		for rowIndex, want := range literal.rows {
			oracle := image.routeOracles[maskIndex*len(image.rows)+rowIndex]
			start, end := int(oracle.pathStart), int(oracle.pathStart)+int(oracle.pathLength)
			if oracle.activations != literal.mask || oracle.row != dispatchClassID(rowIndex+1) || start < 0 || end > len(image.oraclePaths) || !equalLookupPath(image.oraclePaths[start:end], want) {
				t.Fatalf("mask %02b row %d oracle/path = %#v/%v, want %v", literal.mask, rowIndex+1, oracle, image.oraclePaths[start:end], want)
			}
		}
	}

	emission, err := buildLookupEmissionPlan(checked)
	if err != nil {
		t.Fatal(err)
	}
	if emission.NodeCount != 8 || emission.RowCount != 5 || emission.SelectorCount != 2 || emission.AttachmentCount != 2 ||
		len(emission.Nodes) != 8 || len(emission.Rows) != 5 || len(emission.Routes) != 20 || len(emission.Definitions) != 9 ||
		len(emission.InitialDefinitions) != 4 || len(emission.Mutations) != 12 || len(emission.Targets) != 9 || len(emission.TargetMatches) != 13 {
		t.Fatalf("emitted closure counts = nodes %d/%d rows %d/%d selectors %d attachments %d routes %d definitions %d initial %d mutations %d targets %d matches %d",
			emission.NodeCount, len(emission.Nodes), emission.RowCount, len(emission.Rows), emission.SelectorCount, emission.AttachmentCount,
			len(emission.Routes), len(emission.Definitions), len(emission.InitialDefinitions), len(emission.Mutations), len(emission.Targets), len(emission.TargetMatches))
	}
	sites := [...]uint32{emission.ReadLabelSite, emission.ReadSavedSite, emission.ReadPublicSite, emission.ReadSingletonSite}
	seenSites := make(map[uint32]bool, len(sites))
	for _, site := range sites {
		if site == 0 || seenSites[site] {
			t.Fatalf("emitted sites are not four nonzero distinct IDs: %v", sites)
		}
		seenSites[site] = true
	}
	if emission.SingletonNode != 8 || emission.SingletonDispatch != 5 || emission.SingletonLabelDefinition != 17 || emission.SingletonDefineMutation != 24 {
		t.Fatalf("emitted singleton node/dispatch/definition/mutation = %d/%d/%d/%d", emission.SingletonNode, emission.SingletonDispatch, emission.SingletonLabelDefinition, emission.SingletonDefineMutation)
	}
}

func TestTarget41SnapshotAndAttachmentObserversAreFairAndExact(t *testing.T) {
	layouts := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"snapshot span", unsafe.Sizeof(target41SnapshotSpan{}), 2},
		{"snapshot", unsafe.Sizeof(target41Snapshot{}), 10},
		{"snapshot immutable", unsafe.Sizeof(target41SnapshotImage{}), 264},
		{"snapshot owner", unsafe.Sizeof(target41SnapshotOwner{}), 2},
		{"snapshot total", unsafe.Sizeof(target41SnapshotImage{}) + unsafe.Sizeof(target41SnapshotOwner{}), 266},
		{"attachment node", unsafe.Sizeof(target41AttachmentNode{}), 8},
		{"attachment fact", unsafe.Sizeof(target41AttachmentFact{}), 12},
		{"attachment immutable", unsafe.Sizeof(target41AttachmentImage{}), 88},
		{"attachment workspace", unsafe.Sizeof(target41AttachmentWorkspace{}), 40},
		{"attachment owner/workspace", unsafe.Sizeof(target41AttachmentOwner{}), 56},
		{"attachment total", unsafe.Sizeof(target41AttachmentImage{}) + unsafe.Sizeof(target41AttachmentOwner{}), 144},
	}
	for _, layout := range layouts {
		if layout.got != layout.want {
			t.Errorf("%s = %d bytes, want %d", layout.name, layout.got, layout.want)
		}
	}

	var snapshotWork, attachmentWork target39ColdWork
	var workspace target41AttachmentWorkspace
	for _, literal := range target41LiteralRoutes {
		for row, want := range literal.rows {
			if got, ok := target41SnapshotRoute(literal.mask, row, &snapshotWork); !ok || !equalLookupPath(got, want) {
				t.Fatalf("snapshot mask %02b row %d = %v/%v, want %v", literal.mask, row+1, got, ok, want)
			}
			if got, ok := target41AttachmentRoute(literal.mask, row, &workspace, &attachmentWork); !ok || !equalLookupPath(got, want) {
				t.Fatalf("attachment mask %02b row %d = %v/%v, want %v", literal.mask, row+1, got, ok, want)
			}
		}
	}
	if snapshotWork != (target39ColdWork{routes: 20, routeSlots: 56}) {
		t.Fatalf("snapshot all-mask work = %#v, want 20 routes/56 slots", snapshotWork)
	}
	if attachmentWork != (target39ColdWork{routes: 20, routeSlots: 56, nodeFacts: 56, attachmentFacts: 32}) {
		t.Fatalf("attachment all-mask work = %#v, want 20/56/56/32", attachmentWork)
	}

	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	initial := target41ComparatorInitial(t, image)
	snapshotState, attachmentState := initial, initial
	snapshotOwner := target41SnapshotOwner{}
	attachmentOwner := target41AttachmentOwner{}
	wantK := [...]int{0, 3, 0, 1, 3, 1}
	var snapshotWorkByMutation, attachmentWorkByMutation [6]target39ColdWork
	for index, mutation := range image.mutations[18:24] {
		snapshotOwner.staged = uint8(mutation.afterActivations)
		snapshotChanged, err := target41ComparatorApply(&snapshotState, uint64(snapshotOwner.live), mutation, target41SnapshotRoute, &snapshotWorkByMutation[index])
		if err != nil {
			t.Fatalf("snapshot mutation %d: %v", mutation.id, err)
		}
		snapshotOwner.live, snapshotOwner.staged = snapshotOwner.staged, snapshotOwner.live
		attachmentOwner.staged = mutation.afterActivations
		attachmentChanged, err := target41ComparatorApply(&attachmentState, attachmentOwner.live, mutation, func(mask uint64, row int, work *target39ColdWork) ([]lookupNodeID, bool) {
			return target41AttachmentRoute(mask, row, &attachmentOwner.workspace, work)
		}, &attachmentWorkByMutation[index])
		if err != nil {
			t.Fatalf("attachment mutation %d: %v", mutation.id, err)
		}
		attachmentOwner.live, attachmentOwner.staged = attachmentOwner.staged, attachmentOwner.live
		attachmentOwner.workspace = target41AttachmentWorkspace{}
		if snapshotChanged != wantK[index] || attachmentChanged != wantK[index] || snapshotState != attachmentState {
			t.Fatalf("mutation %d changed snapshot/attachment=%d/%d equal=%v, want K=%d", mutation.id, snapshotChanged, attachmentChanged, snapshotState == attachmentState, wantK[index])
		}
	}
	wantSnapshotSingleton := target39ColdWork{routes: 5, routeSlots: 16, resolutions: 10, entryProbes: 18, cellChecks: 5, cellWrites: 1}
	wantAttachmentSingleton := target39ColdWork{routes: 5, routeSlots: 16, nodeFacts: 16, attachmentFacts: 8, resolutions: 10, entryProbes: 18, cellChecks: 5, cellWrites: 1}
	if snapshotWorkByMutation[5] != wantSnapshotSingleton || attachmentWorkByMutation[5] != wantAttachmentSingleton {
		t.Fatalf("singleton transaction work snapshot/attachment = %#v/%#v, want %#v/%#v", snapshotWorkByMutation[5], attachmentWorkByMutation[5], wantSnapshotSingleton, wantAttachmentSingleton)
	}
}

func TestTarget41SingletonTransactionPublishesOneCellAndClearsScratch(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	owner, err := newLookupOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range image.mutations[:23] {
		if err := owner.apply(context.Background(), mutation); err != nil {
			t.Fatalf("apply mutation %d: %v", mutation.id, err)
		}
	}
	mutation := image.mutations[23]
	label := program.checked.selectors["label"]
	singletonCell := target39CellIndex(t, image, program.checked.singletonOwner.dispatch, label)

	for cancelAt := 1; cancelAt <= 7; cancelAt++ {
		entries := append([]lookupEntry(nil), owner.entries...)
		cells := append([]resolutionCell(nil), owner.cells...)
		topology, epoch := owner.topology, owner.nextEpoch
		ctx := &target39CancelAfterContext{Context: context.Background(), allowedErrCalls: cancelAt - 1}
		if err := owner.apply(ctx, mutation); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel checkpoint %d = %v, want canceled", cancelAt, err)
		}
		if !reflect.DeepEqual(owner.entries, entries) || !reflect.DeepEqual(owner.cells, cells) || owner.topology != topology || owner.nextEpoch != epoch {
			t.Fatalf("cancel checkpoint %d published singleton transaction state", cancelAt)
		}
		target39RequireClearedScratch(t, owner)
	}

	beforeCells, beforeEpoch := append([]resolutionCell(nil), owner.cells...), owner.nextEpoch
	if err := owner.apply(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}
	if changed := target39ChangedCells(beforeCells, owner.cells); !reflect.DeepEqual(changed, []int{singletonCell}) || owner.nextEpoch != beforeEpoch+1 ||
		owner.cells[singletonCell].receipt != (lookupReceipt{owner: 8, slot: 15, definition: 17, outcome: lookupResolved, visibility: lookupVisibilityPublic}) {
		t.Fatalf("singleton publication changed=%v epoch=%d/%d receipt=%#v", changed, beforeEpoch, owner.nextEpoch, owner.cells[singletonCell].receipt)
	}
	target39RequireClearedScratch(t, owner)
	owner.close()
	if owner.image != nil || owner.entries != nil || owner.cells != nil || owner.staged != nil || owner.changed != nil || owner.routeScratch != nil || owner.routeMarks != nil || owner.nextEpoch != 0 {
		t.Fatalf("closed singleton owner retained state: %#v", owner)
	}
	if fresh, err := newLookupOwner(image); err != nil {
		t.Fatal(err)
	} else {
		fresh.close()
	}
}

func TestTarget41LookupImageRejectsSingletonCorruption(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	tests := []struct {
		name   string
		mutate func(*lookupImage)
	}{
		{"singleton kind", func(copy *lookupImage) { copy.nodes[7].kind = lookupNodeClass }},
		{"singleton superclass", func(copy *lookupImage) { copy.nodes[7].superclass = 3 }},
		{"singleton attachment", func(copy *lookupImage) { copy.nodes[7].attachmentCount = 1 }},
		{"singleton row", func(copy *lookupImage) { copy.rows[4].node = 2 }},
		{"singleton mutation precondition", func(copy *lookupImage) { copy.mutations[23].before.kind = lookupEntryDefined }},
		{"singleton route mask", func(copy *lookupImage) { copy.routeOracles[19].activations = 2 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			corrupt := *image
			corrupt.nodes = append([]lookupNodeDescriptor(nil), image.nodes...)
			corrupt.rows = append([]dispatchRowDescriptor(nil), image.rows...)
			corrupt.mutations = append([]lookupMutationDescriptor(nil), image.mutations...)
			corrupt.routeOracles = append([]lookupRouteOracle(nil), image.routeOracles...)
			test.mutate(&corrupt)
			if err := validateLookupImage(&corrupt); err == nil {
				t.Fatal("corrupt singleton image validated")
			}
		})
	}
}

func target41SnapshotRoute(mask uint64, row int, work *target39ColdWork) ([]lookupNodeID, bool) {
	if mask > 3 || row < 0 || row >= 5 || work == nil {
		return nil, false
	}
	span := target41FrozenSnapshots.snapshots[mask].rows[row]
	start, end := int(span.start), int(span.start)+int(span.length)
	if span.length == 0 || end > len(target41FrozenSnapshots.paths) {
		return nil, false
	}
	work.routes++
	work.routeSlots += uint16(span.length)
	return target41FrozenSnapshots.paths[start:end], true
}

func target41AttachmentRoute(mask uint64, row int, workspace *target41AttachmentWorkspace, work *target39ColdWork) ([]lookupNodeID, bool) {
	if mask > 3 || row < 0 || row >= 5 || workspace == nil || work == nil {
		return nil, false
	}
	*workspace = target41AttachmentWorkspace{}
	length := 0
	root := [...]lookupNodeID{2, 3, 4, 7, 8}[row]
	if !target41AttachmentAppend(mask, root, workspace, &length, work) || length == 0 {
		*workspace = target41AttachmentWorkspace{}
		return nil, false
	}
	clear(workspace.marks[:])
	work.routes++
	work.routeSlots += uint16(length)
	return workspace.nodes[:length], true
}

func target41AttachmentAppend(mask uint64, id lookupNodeID, workspace *target41AttachmentWorkspace, length *int, work *target39ColdWork) bool {
	if id == 0 || int(id) > len(target41FrozenAttachments.nodes) || workspace == nil || length == nil {
		return false
	}
	mark := &workspace.marks[id-1]
	if *mark != 0 {
		return *mark == 2
	}
	*mark = 1
	node := target41FrozenAttachments.nodes[id-1]
	work.nodeFacts++
	if node.kind != lookupNodeClass && node.kind != lookupNodeModule && node.kind != lookupNodeSingleton {
		return false
	}
	start, end := int(node.attachmentStart), int(node.attachmentStart)+int(node.attachmentCount)
	if end > len(target41FrozenAttachments.attachments) || node.kind == lookupNodeSingleton && (node.attachmentCount != 0 || node.superclass == 0) {
		return false
	}
	for index := end - 1; index >= start; index-- {
		attachment := target41FrozenAttachments.attachments[index]
		work.attachmentFacts++
		if attachment.target != id || attachment.kind == lookupAttachmentPrepend && mask&(uint64(1)<<index) != 0 && !target41AttachmentAppend(mask, attachment.module, workspace, length, work) {
			return false
		}
	}
	if *length >= len(workspace.nodes) {
		return false
	}
	workspace.nodes[*length] = id
	*length++
	for index := end - 1; index >= start; index-- {
		attachment := target41FrozenAttachments.attachments[index]
		work.attachmentFacts++
		if attachment.target != id || attachment.kind == lookupAttachmentInclude && mask&(uint64(1)<<index) != 0 && !target41AttachmentAppend(mask, attachment.module, workspace, length, work) {
			return false
		}
	}
	*mark = 2
	if node.kind == lookupNodeModule {
		return node.superclass == 0
	}
	return node.superclass == 0 || target41AttachmentAppend(mask, node.superclass, workspace, length, work)
}

func target41ComparatorInitial(t *testing.T, image *lookupImage) target41ComparatorState {
	t.Helper()
	if len(image.initialEntries) != 64 || len(image.rows) != 5 || len(image.selectors) != 8 {
		t.Fatalf("comparator closure entries/rows/selectors = %d/%d/%d, want 64/5/8", len(image.initialEntries), len(image.rows), len(image.selectors))
	}
	var state target41ComparatorState
	copy(state.entries[:], image.initialEntries)
	for _, mutation := range image.mutations[:18] {
		index := (int(mutation.owner)-1)*8 + int(mutation.selector) - 1
		if mutation.kind == lookupMutationInclude || mutation.kind == lookupMutationPrepend || index < 0 || index >= len(state.entries) || state.entries[index] != mutation.before {
			t.Fatalf("original mutation %d cannot seed comparator", mutation.id)
		}
		state.entries[index] = mutation.after
	}
	for row, route := range target41LiteralRoutes[0].rows {
		for selector := selectorID(1); selector <= 8; selector++ {
			receipt, ok := target41ComparatorResolve(&state.entries, route, selector, nil)
			if !ok {
				t.Fatalf("initial comparator resolve row %d selector %d", row+1, selector)
			}
			state.cells[row*8+int(selector)-1] = receipt
		}
	}
	return state
}

type target41RouteProvider func(uint64, int, *target39ColdWork) ([]lookupNodeID, bool)

func target41ComparatorApply(state *target41ComparatorState, activations uint64, mutation lookupMutationDescriptor, routeFor target41RouteProvider, work *target39ColdWork) (int, error) {
	if state == nil || routeFor == nil || work == nil || activations != mutation.beforeActivations {
		return 0, errLookupCorrupt
	}
	stagedEntries, stagedCells := state.entries, state.cells
	entryMutation := mutation.kind != lookupMutationInclude && mutation.kind != lookupMutationPrepend
	if entryMutation {
		index := (int(mutation.owner)-1)*8 + int(mutation.selector) - 1
		if mutation.beforeActivations != mutation.afterActivations || index < 0 || index >= len(stagedEntries) || stagedEntries[index] != mutation.before {
			return 0, errLookupCorrupt
		}
		stagedEntries[index] = mutation.after
	} else if mutation.attachment == 0 || mutation.attachment > 2 || mutation.afterActivations != mutation.beforeActivations|uint64(1)<<(mutation.attachment-1) {
		return 0, errLookupCorrupt
	}

	changed := 0
	for row := 0; row < 5; row++ {
		currentRoute, ok := routeFor(activations, row, work)
		if !ok {
			return 0, errLookupCorrupt
		}
		if entryMutation {
			selector := mutation.selector
			cellIndex := row*8 + int(selector) - 1
			current, ok := target41ComparatorResolve(&state.entries, currentRoute, selector, work)
			if !ok || state.cells[cellIndex] != current {
				return 0, errLookupCorrupt
			}
			proposed, ok := target41ComparatorResolve(&stagedEntries, currentRoute, selector, work)
			if !ok {
				return 0, errLookupCorrupt
			}
			work.cellChecks++
			if proposed != current {
				stagedCells[cellIndex] = proposed
				changed++
				work.cellWrites++
			}
			continue
		}
		var current [8]lookupReceipt
		for selectorIndex := range current {
			selector := selectorID(selectorIndex + 1)
			cellIndex := row*8 + selectorIndex
			receipt, ok := target41ComparatorResolve(&state.entries, currentRoute, selector, work)
			if !ok || state.cells[cellIndex] != receipt {
				return 0, errLookupCorrupt
			}
			current[selectorIndex] = receipt
			work.cellChecks++
		}
		proposedRoute, ok := routeFor(mutation.afterActivations, row, work)
		if !ok {
			return 0, errLookupCorrupt
		}
		for selectorIndex, currentReceipt := range current {
			proposed, ok := target41ComparatorResolve(&stagedEntries, proposedRoute, selectorID(selectorIndex+1), work)
			if !ok {
				return 0, errLookupCorrupt
			}
			if proposed != currentReceipt {
				stagedCells[row*8+selectorIndex] = proposed
				changed++
				work.cellWrites++
			}
		}
	}
	state.entries, state.cells = stagedEntries, stagedCells
	return changed, nil
}

func target41ComparatorResolve(entries *[64]lookupEntry, route []lookupNodeID, selector selectorID, work *target39ColdWork) (lookupReceipt, bool) {
	if entries == nil || len(route) == 0 || selector == 0 || selector > 8 {
		return lookupReceipt{}, false
	}
	if work != nil {
		work.resolutions++
	}
	for _, node := range route {
		index := (int(node)-1)*8 + int(selector) - 1
		if node == 0 || index < 0 || index >= len(entries) {
			return lookupReceipt{}, false
		}
		if work != nil {
			work.entryProbes++
		}
		entry := entries[index]
		switch entry.kind {
		case lookupEntryAbsent:
			continue
		case lookupEntryUndef:
			return lookupReceipt{owner: node, slot: entry.slot, outcome: lookupUndef}, true
		case lookupEntryDefined:
			return lookupReceipt{owner: node, slot: entry.slot, definition: entry.definition, outcome: lookupResolved, visibility: entry.visibility}, true
		default:
			return lookupReceipt{}, false
		}
	}
	return lookupReceipt{outcome: lookupMissing}, true
}
