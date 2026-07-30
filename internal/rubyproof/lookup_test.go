package rubyproof

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLookupLayoutsAndMaximumAdmissionLedger(t *testing.T) {
	t.Parallel()

	wantSizes := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"entry", reflect.TypeOf(lookupEntry{}).Size(), lookupEntryBytes},
		{"receipt", reflect.TypeOf(lookupReceipt{}).Size(), 24},
		{"cell", reflect.TypeOf(resolutionCell{}).Size(), resolutionCellBytes},
		{"node", reflect.TypeOf(lookupNodeDescriptor{}).Size(), lookupNodeDescriptorBytes},
		{"row", reflect.TypeOf(dispatchRowDescriptor{}).Size(), dispatchRowDescriptorBytes},
		{"selector", reflect.TypeOf(selectorDescriptor{}).Size(), selectorDescriptorBytes},
		{"attachment", reflect.TypeOf(lookupAttachmentDescriptor{}).Size(), lookupAttachmentDescriptorBytes},
		{"route oracle", reflect.TypeOf(lookupRouteOracle{}).Size(), lookupRouteOracleBytes},
		{"topology bank", reflect.TypeOf(lookupTopologyBank{}).Size(), lookupTopologyBankBytes},
		{"arm", reflect.TypeOf(lookupArm{}).Size(), 32},
		{"site", reflect.TypeOf(lookupSite{}).Size(), lookupSiteBytes},
	}
	for _, item := range wantSizes {
		if item.got != item.want {
			t.Errorf("%s size = %d, want %d", item.name, item.got, item.want)
		}
	}
	if got := reflect.TypeOf(lookupOwner{}).Size(); got > lookupOwnerHeaderReserve {
		t.Fatalf("lookup owner header size = %d, reserve %d", got, lookupOwnerHeaderReserve)
	}

	got, ok := maximumLookupAdmissionBytes()
	if !ok {
		t.Fatal("reviewed maximum layout was rejected")
	}
	const want = 1_442_960
	if got != want {
		t.Fatalf("maximum lookup ledger = %d, want %d", got, want)
	}
	if margin := maximumLookupOwnerBytes - got; margin != 129_904 {
		t.Fatalf("maximum lookup ledger margin = %d, want 129904", margin)
	}
	if _, ok := lookupAdmissionBytes(maximumLookupNodes, maximumDispatchClasses, maximumPreparedSelectors, maximumLookupAttachments, maximumLookupRouteOracles, maximumLookupPathSlots, maximumLookupSites); ok {
		t.Fatal("non-composable individual maxima were admitted")
	}
	if _, ok := lookupAdmissionBytes(maximumLookupNodes, maximumDispatchClasses, maximumPreparedSelectors, maximumLookupAttachments, maximumLookupRouteOracles, maximumLookupPathSlots, maximumLookupSites+1); ok {
		t.Fatal("site count above the reviewed maximum was admitted")
	}
}

func TestSuperclassSyntaxIsHeaderOnly(t *testing.T) {
	t.Parallel()

	source := "class Base\nend\nclass Alpha < Base\nend\nclass Alpha\nend\n"
	tokens, diagnostics := lex(source)
	if len(diagnostics) != 0 {
		t.Fatalf("lex inheritance source: %v", diagnostics)
	}
	module, diagnostics := parse(tokens)
	if len(diagnostics) != 0 {
		t.Fatalf("parse inheritance source: %v", diagnostics)
	}
	if len(module.statements) != 3 || module.statements[0].name != "Base" || module.statements[0].superclass != "" ||
		module.statements[1].name != "Alpha" || module.statements[1].superclass != "Base" || module.statements[2].superclass != "" {
		t.Fatalf("parsed superclass facts = %#v", module.statements)
	}

	for _, malformed := range []string{
		"class Alpha < base\nend\n",
		"class Alpha < Base < Other\nend\n",
		"value = 1 < 2\n",
	} {
		tokens, diagnostics := lex(malformed)
		if len(diagnostics) == 0 {
			_, diagnostics = parse(tokens)
		}
		if len(diagnostics) == 0 {
			t.Fatalf("unsupported less-than form parsed: %q", malformed)
		}
	}
}

func TestSuperclassFactsRejectUnknownSelfAndMismatchedReopen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		want       string
		wantParent string
	}{
		{
			name:       "valid",
			source:     "class Base\nend\nclass Alpha < Base\nend\nclass Alpha\nend\n",
			wantParent: "Base",
		},
		{name: "unknown", source: "class Alpha < Missing\nend\n", want: "unknown superclass"},
		{name: "self", source: "class Alpha < Alpha\nend\n", want: "cannot inherit from itself"},
		{
			name:   "mismatch",
			source: "class Base\nend\nclass Other\nend\nclass Alpha < Base\nend\nclass Alpha < Other\nend\n",
			want:   "superclass mismatch",
		},
		{
			name:   "late explicit parent",
			source: "class Base\nend\nclass Alpha\nend\nclass Alpha < Base\nend\n",
			want:   "superclass mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens, diagnostics := lex(test.source)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			module, diagnostics := parse(tokens)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			program := &checkedProgram{
				module:     module,
				classes:    make(map[string]*checkedClass),
				topMethods: make(map[string][]*checkedMethod),
				methods:    make(map[*statement]*checkedMethod),
			}
			checker := checker{program: program}
			checker.declare(module.statements)
			if test.want == "" {
				if len(checker.diagnostics) != 0 {
					t.Fatalf("declare valid inheritance: %v", checker.diagnostics)
				}
				if got := program.classes["Alpha"].superclass; got == nil || got.name != test.wantParent {
					t.Fatalf("Alpha superclass = %#v, want %q", got, test.wantParent)
				}
				return
			}
			if len(checker.diagnostics) == 0 || !strings.Contains(checker.diagnostics[0].Message, test.want) {
				t.Fatalf("diagnostics = %v, want %q", checker.diagnostics, test.want)
			}
		})
	}
}

func TestLookupImageIsDeepSealedAndDeterministic(t *testing.T) {
	t.Parallel()

	build := d1LookupBuild()
	first := sealD1LookupBuild(build)
	second := sealD1LookupBuild(build)
	if first.identity != second.identity {
		t.Fatalf("same lookup facts produced different identities: %x != %x", first.identity, second.identity)
	}
	if err := validateLookupImage(&first); err != nil {
		t.Fatalf("validate sealed image: %v", err)
	}

	build.nodes[0].id = 99
	build.rows[0].node = 99
	build.selectors[0].nameHash = 0
	build.definitions[0].owner = 99
	build.routeOracles[0].row = 99
	build.oraclePaths[0] = 99
	build.entries[0].definition = 99
	build.receipts[0].definition = 99
	if err := validateLookupImage(&first); err != nil {
		t.Fatalf("builder mutation changed the sealed image: %v", err)
	}

	changed := d1LookupBuild()
	changed.definitions[3].id = 3
	changedImage := sealD1LookupBuild(changed)
	if changedImage.identity == first.identity {
		t.Fatal("semantic lookup fact mutation did not change the image identity")
	}
	if err := validateLookupImage(&changedImage); !errors.Is(err, errLookupImage) {
		t.Fatalf("malformed dense definition IDs returned %v, want %v", err, errLookupImage)
	}
}

func TestLookupOwnerRejectsMalformedImageBeforePublication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*lookupImage)
	}{
		{"schema", func(image *lookupImage) { image.schema++ }},
		{"identity", func(image *lookupImage) { image.identity[0] ^= 0xff }},
		{"self parent", func(image *lookupImage) { image.nodes[1].superclass = image.nodes[1].id }},
		{"path order", func(image *lookupImage) { image.oraclePaths[0] = image.nodes[0].id }},
		{"definition owner", func(image *lookupImage) { image.definitions[0].owner = 99 }},
		{"entry definition", func(image *lookupImage) { image.initialEntries[0].definition = 2 }},
		{"entry visibility", func(image *lookupImage) { image.initialEntries[0].visibility = lookupVisibility(99) }},
		{"receipt mismatch", func(image *lookupImage) { image.initialReceipts[0].definition = 4 }},
		{"receipt visibility", func(image *lookupImage) { image.initialReceipts[0].visibility = lookupVisibilityPrivate }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image := sealD1LookupBuild(d1LookupBuild())
			test.mutate(&image)
			if test.name != "schema" && test.name != "identity" {
				image.identity = lookupImageIdentity(&image)
			}
			owner, err := newLookupOwner(&image)
			if owner != nil || !errors.Is(err, errLookupImage) {
				t.Fatalf("new owner = %#v, %v; want nil malformed-image error", owner, err)
			}
		})
	}
}

func TestCheckedLookupFactsAreDeterministicAndSourceAttributed(t *testing.T) {
	t.Parallel()

	first, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	second, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	left, right := first.checked, second.checked
	if left.lookup == nil || right.lookup == nil || left.lookup.identity != right.lookup.identity || left.lookup.attribution != first.Identity() {
		t.Fatalf("lookup identities left=%#v right=%#v program=%x", left.lookup, right.lookup, first.Identity())
	}
	if err := validateLookupImage(left.lookup); err != nil {
		t.Fatalf("validate checked lookup image: %v", err)
	}
	if len(left.lookup.nodes) != 8 || len(left.lookup.rows) != 5 || len(left.lookup.selectors) != 8 || len(left.lookup.definitions) != 17 ||
		len(left.lookup.attachments) != 2 || len(left.lookup.mutations) != 24 || len(left.lookup.routeOracles) != 20 {
		t.Fatalf("checked lookup inventory nodes=%d rows=%d selectors=%d definitions=%d attachments=%d mutations=%d route-oracles=%d", len(left.lookup.nodes), len(left.lookup.rows), len(left.lookup.selectors), len(left.lookup.definitions), len(left.lookup.attachments), len(left.lookup.mutations), len(left.lookup.routeOracles))
	}
	for index, name := range []string{"apply", "hot", "initialize", "label", "mark", "saved_label", "trace", "value"} {
		if left.selectorNames[index] != name || left.selectors[name] != selectorID(index+1) {
			t.Fatalf("selector %d = %q/%d, want %q/%d", index, left.selectorNames[index], left.selectors[name], name, index+1)
		}
	}
	wantNodes := []struct {
		name       string
		node       lookupNodeID
		superclass lookupNodeID
		dispatch   dispatchClassID
		shape      shapeID
	}{
		{"Base", 1, 0, 0, 0},
		{"Alpha", 2, 1, 1, 1},
		{"Beta", 3, 1, 2, 2},
		{"Gamma", 4, 1, 3, 3},
	}
	for _, want := range wantNodes {
		class := left.classes[want.name]
		if class.node != want.node || lookupNodeIDOf(class.superclass) != want.superclass || class.dispatch != want.dispatch || class.shape != want.shape {
			t.Fatalf("%s facts node=%d super=%d dispatch=%d shape=%d", want.name, class.node, lookupNodeIDOf(class.superclass), class.dispatch, class.shape)
		}
	}
	label := left.selectors["label"]
	wantLabels := []struct {
		definition definitionID
		owner      lookupNodeID
		slot       methodSlotID
	}{
		{3, 1, 3},
		{10, 3, 10},
		{12, 4, 12},
		{13, 1, 3},
		{17, 8, 15},
	}
	alias := left.lookup.definitions[3]
	if alias.id != 4 || alias.owner != 1 || alias.selector != left.selectors["saved_label"] || alias.slot != 4 || alias.body != 3 {
		t.Fatalf("saved_label alias definition = %#v, want Base/saved slot 4 body 3", alias)
	}
	for _, want := range wantLabels {
		definition := left.lookup.definitions[want.definition-1]
		if definition.id != want.definition || definition.owner != want.owner || definition.selector != label || definition.slot != want.slot {
			t.Fatalf("label definition %d = %#v, want owner=%d selector=%d slot=%d", want.definition, definition, want.owner, label, want.slot)
		}
	}
	for index, receipt := range left.lookup.initialReceipts {
		if receipt != (lookupReceipt{outcome: lookupMissing}) {
			t.Fatalf("initial receipt %d = %#v, want missing", index, receipt)
		}
	}
	readLabel := left.topMethods["read_label"][0].declaration.body[0].expression
	call := left.calls[readLabel]
	if call.kind != checkedCallReceiver || call.mode != checkedCallAnyVisibility || call.argumentOffset != 1 || call.selector != label || call.site == 0 {
		t.Fatalf("read_label call fact = %#v", call)
	}
	readSaved := left.topMethods["read_saved"][0].declaration.body[0].expression
	savedCall := left.calls[readSaved]
	if savedCall.kind != checkedCallReceiver || savedCall.mode != checkedCallPublicVisibility || savedCall.argumentOffset != 0 || savedCall.selector != left.selectors["saved_label"] || savedCall.site == 0 || savedCall.site == call.site {
		t.Fatalf("read_saved call fact = %#v", savedCall)
	}
	readPublic := left.topMethods["read_public"][0].declaration.body[0].body[0].expression
	publicCall := left.calls[readPublic]
	if publicCall.kind != checkedCallReceiver || publicCall.mode != checkedCallPublicVisibility || publicCall.argumentOffset != 1 || publicCall.selector != label || publicCall.site == 0 || publicCall.site == call.site || publicCall.site == savedCall.site {
		t.Fatalf("read_public call fact = %#v", publicCall)
	}
	seenSites := make([]bool, maximumLookupSites+1)
	count := 0
	for _, fact := range left.calls {
		if fact.kind != checkedCallReceiver {
			continue
		}
		if fact.site == 0 || int(fact.site) > maximumLookupSites || seenSites[fact.site] {
			t.Fatalf("invalid or duplicate receiver site %#v", fact)
		}
		seenSites[fact.site] = true
		count++
	}
	for site := 1; site <= count; site++ {
		if !seenSites[site] {
			t.Fatalf("receiver call-site IDs are not dense at %d", site)
		}
	}

	changedSource := strings.Replace(ProofSource, "mark(7)\n    2", "mark(7)\n    5", 1)
	changed, diagnostics := Compile(changedSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if changed.Identity() == first.Identity() || changed.checked.lookup.identity == left.lookup.identity || changed.checked.lookup.attribution == left.lookup.attribution {
		t.Fatal("accepted method-body change preserved source-attributed program or lookup identity")
	}
}

func TestLookupEmissionPlanProjectsOnlyTheCheckedFourSiteClosure(t *testing.T) {
	t.Parallel()

	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	plan, err := buildLookupEmissionPlan(program.checked)
	if err != nil {
		t.Fatal(err)
	}
	if plan.NodeCount != 8 || plan.RowCount != 5 || plan.SelectorCount != 2 || plan.AttachmentCount != 2 ||
		len(plan.Nodes) != 8 || len(plan.Rows) != 5 || len(plan.Attachments) != 2 || len(plan.Routes) != 20 ||
		len(plan.Definitions) != 9 || len(plan.InitialDefinitions) != 4 || len(plan.Mutations) != 12 || len(plan.Targets) != 9 || len(plan.TargetMatches) != 13 {
		t.Fatalf("emission closure = %#v", plan)
	}
	wantDefinitions := []uint32{3, 10, 12, 13, 4, 14, 15, 16, 17}
	for index, want := range wantDefinitions {
		if plan.Definitions[index].ID != want {
			t.Fatalf("emitted definition %d = %d, want %d", index, plan.Definitions[index].ID, want)
		}
	}
	if got := plan.InitialDefinitions; !reflect.DeepEqual(got, []uint32{3, 4, 10, 12}) {
		t.Fatalf("initial definitions = %v, want [3 4 10 12]", got)
	}
	for index, target := range plan.Targets {
		if target.ID != uint32(index+1) || target.Definition != wantDefinitions[index] {
			t.Fatalf("target %d = %#v", index+1, target)
		}
	}
	if len(program.checked.lookup.selectors) != 8 || len(program.checked.lookup.definitions) != 17 || len(program.checked.lookup.mutations) != 24 {
		t.Fatal("emission projection mutated the complete checked lookup image")
	}

	program.checked.classes["Beta"].methods["label"][0].slot++
	if _, err := buildLookupEmissionPlan(program.checked); err == nil {
		t.Fatalf("checked/image disagreement returned %v", err)
	}
}

func TestLookupOwnerInheritanceAndSelectiveEpochs(t *testing.T) {
	t.Parallel()

	image := sealD1LookupBuild(d1LookupBuild())
	owner, err := newLookupOwner(&image)
	if err != nil {
		t.Fatalf("new lookup owner: %v", err)
	}

	wantBefore := []definitionID{1, 2, 3}
	for index, want := range wantBefore {
		receipt, err := owner.canonical(dispatchClassID(index+1), 1)
		if err != nil {
			t.Fatalf("canonical dispatch %d: %v", index+1, err)
		}
		if receipt.definition != want {
			t.Fatalf("canonical dispatch %d definition = %d, want %d", index+1, receipt.definition, want)
		}
		cell, err := owner.effectiveCell(dispatchClassID(index+1), 1)
		if err != nil || cell.receipt != receipt {
			t.Fatalf("effective dispatch %d = %#v, %v; canonical %#v", index+1, cell, err, receipt)
		}
	}

	before := append([]resolutionCell(nil), owner.cells...)
	if err := owner.apply(context.Background(), image.mutations[1]); err != nil {
		t.Fatalf("install Base v2: %v", err)
	}
	if owner.cells[0].receipt.definition != 4 || owner.cells[0].epoch == before[0].epoch {
		t.Fatalf("Alpha cell was not selectively changed: before %#v, after %#v", before[0], owner.cells[0])
	}
	if owner.cells[1] != before[1] {
		t.Fatalf("Beta cell changed through shadowed Base mutation: before %#v, after %#v", before[1], owner.cells[1])
	}
	if owner.cells[2] != before[2] {
		t.Fatalf("Gamma cell changed through shadowed Base mutation: before %#v, after %#v", before[2], owner.cells[2])
	}
	if got := owner.nextEpoch; got != 4 {
		t.Fatalf("next epoch = %d, want 4", got)
	}

	owner.cells[0].receipt = before[1].receipt
	receipt, err := owner.canonical(1, 1)
	if err != nil || receipt.definition != 4 {
		t.Fatalf("canonical lookup trusted corrupt derived cell: %#v, %v", receipt, err)
	}
	if _, err := owner.effectiveCell(1, 1); !errors.Is(err, errLookupCorrupt) {
		t.Fatalf("corrupt effective cell returned %v, want %v", err, errLookupCorrupt)
	}
}

func TestProofLookupOwnerAliasVisibilityRemoveAndUndefTransitions(t *testing.T) {
	t.Parallel()

	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	if len(image.mutations) != 24 {
		t.Fatalf("checked mutations = %d, want 24", len(image.mutations))
	}
	owner, err := newLookupOwner(image)
	if err != nil {
		t.Fatalf("new lookup owner: %v", err)
	}
	for index := 0; index < 12; index++ {
		if err := owner.apply(context.Background(), image.mutations[index]); err != nil {
			t.Fatalf("apply initial mutation %d: %v", index+1, err)
		}
	}

	label := program.checked.selectors["label"]
	saved := program.checked.selectors["saved_label"]
	alpha := program.checked.classes["Alpha"]
	beta := program.checked.classes["Beta"]
	gamma := program.checked.classes["Gamma"]
	delta := program.checked.classes["Delta"]
	singleton := program.checked.singletonOwner
	cellIndex := func(dispatch dispatchClassID, selector selectorID) int {
		row := image.rows[dispatch-1]
		return int(row.cellStart) + int(selector) - 1
	}
	wantReceipt := func(dispatch dispatchClassID, selector selectorID, want lookupReceipt) {
		t.Helper()
		got, err := owner.canonical(dispatch, selector)
		if err != nil || got != want {
			t.Fatalf("canonical dispatch=%d selector=%d = %#v, %v; want %#v", dispatch, selector, got, err, want)
		}
		cell, err := owner.effectiveCell(dispatch, selector)
		if err != nil || cell.receipt != want {
			t.Fatalf("effective dispatch=%d selector=%d = %#v, %v; want %#v", dispatch, selector, cell, err, want)
		}
	}

	wantReceipt(alpha.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 3, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	wantReceipt(beta.dispatch, label, lookupReceipt{owner: beta.node, slot: 10, definition: 10, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	wantReceipt(gamma.dispatch, label, lookupReceipt{owner: gamma.node, slot: 12, definition: 12, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	wantReceipt(delta.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 3, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	wantReceipt(singleton.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 3, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	for _, class := range []*checkedClass{alpha, beta, gamma, delta, singleton} {
		wantReceipt(class.dispatch, saved, lookupReceipt{owner: alpha.superclass.node, slot: 4, definition: 4, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	}
	alias, err := image.definition(4)
	if err != nil || alias.body != 3 || alias.id == alias.body {
		t.Fatalf("saved alias descriptor = %#v, %v; want distinct definition 4 with body 3", alias, err)
	}

	beforeRedefine := append([]resolutionCell(nil), owner.cells...)
	if err := owner.apply(context.Background(), image.mutations[12]); err != nil {
		t.Fatalf("apply Base redefinition: %v", err)
	}
	alphaLabel := cellIndex(alpha.dispatch, label)
	betaLabel := cellIndex(beta.dispatch, label)
	gammaLabel := cellIndex(gamma.dispatch, label)
	deltaLabel := cellIndex(delta.dispatch, label)
	singletonLabel := cellIndex(singleton.dispatch, label)
	if owner.cells[alphaLabel] == beforeRedefine[alphaLabel] || owner.cells[deltaLabel] == beforeRedefine[deltaLabel] ||
		owner.cells[singletonLabel] == beforeRedefine[singletonLabel] ||
		owner.cells[betaLabel] != beforeRedefine[betaLabel] || owner.cells[gammaLabel] != beforeRedefine[gammaLabel] {
		t.Fatalf("selective redefine cells before=%#v after=%#v", beforeRedefine, owner.cells)
	}
	for _, class := range []*checkedClass{alpha, beta, gamma, delta} {
		index := cellIndex(class.dispatch, saved)
		if owner.cells[index] != beforeRedefine[index] {
			t.Fatalf("Base redefinition changed saved alias cell for %s", class.name)
		}
	}
	wantReceipt(alpha.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 13, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	wantReceipt(delta.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 13, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	wantReceipt(singleton.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 13, outcome: lookupResolved, visibility: lookupVisibilityPublic})
	wantReceipt(alpha.dispatch, saved, lookupReceipt{owner: alpha.superclass.node, slot: 4, definition: 4, outcome: lookupResolved, visibility: lookupVisibilityPublic})

	for offset, visibility := range []lookupVisibility{lookupVisibilityProtected, lookupVisibilityPrivate, lookupVisibilityPublic} {
		beforeVisibility := append([]resolutionCell(nil), owner.cells...)
		if err := owner.apply(context.Background(), image.mutations[13+offset]); err != nil {
			t.Fatalf("apply Base visibility %d: %v", visibility, err)
		}
		for index := range owner.cells {
			if index == alphaLabel || index == deltaLabel || index == singletonLabel {
				if owner.cells[index] == beforeVisibility[index] || owner.cells[index].receipt.visibility != visibility {
					t.Fatalf("visibility %d did not change inherited label cell %d: before=%#v after=%#v", visibility, index, beforeVisibility[index], owner.cells[index])
				}
				continue
			}
			if owner.cells[index] != beforeVisibility[index] {
				t.Fatalf("visibility %d changed shadowed/unrelated cell %d", visibility, index)
			}
		}
		wantReceipt(alpha.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 13, outcome: lookupResolved, visibility: visibility})
		wantReceipt(delta.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 13, outcome: lookupResolved, visibility: visibility})
		wantReceipt(singleton.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 13, outcome: lookupResolved, visibility: visibility})
	}

	beforeRemove := append([]resolutionCell(nil), owner.cells...)
	if err := owner.apply(context.Background(), image.mutations[16]); err != nil {
		t.Fatalf("apply Beta remove: %v", err)
	}
	for index := range owner.cells {
		if index == betaLabel {
			if owner.cells[index] == beforeRemove[index] {
				t.Fatal("Beta remove did not change the Beta label cell")
			}
			continue
		}
		if owner.cells[index] != beforeRemove[index] {
			t.Fatalf("Beta remove changed unrelated cell %d", index)
		}
	}
	wantReceipt(beta.dispatch, label, lookupReceipt{owner: alpha.superclass.node, slot: 3, definition: 13, outcome: lookupResolved, visibility: lookupVisibilityPublic})

	beforeUndef := append([]resolutionCell(nil), owner.cells...)
	if err := owner.apply(context.Background(), image.mutations[17]); err != nil {
		t.Fatalf("apply Gamma undef: %v", err)
	}
	for index := range owner.cells {
		if index == gammaLabel {
			if owner.cells[index] == beforeUndef[index] {
				t.Fatal("Gamma undef did not change the Gamma label cell")
			}
			continue
		}
		if owner.cells[index] != beforeUndef[index] {
			t.Fatalf("Gamma undef changed unrelated cell %d", index)
		}
	}
	wantReceipt(gamma.dispatch, label, lookupReceipt{owner: gamma.node, slot: 12, outcome: lookupUndef})
}

func TestLookupMutationPreflightIsAtomic(t *testing.T) {
	t.Parallel()

	image := sealD1LookupBuild(d1LookupBuild())
	mutation := image.mutations[1]

	t.Run("cancellation", func(t *testing.T) {
		owner, err := newLookupOwner(&image)
		if err != nil {
			t.Fatal(err)
		}
		beforeEntries := append([]lookupEntry(nil), owner.entries...)
		beforeCells := append([]resolutionCell(nil), owner.cells...)
		beforeEpoch := owner.nextEpoch
		ctx := &cancelAfterContext{allowedErrCalls: 2}
		if err := owner.apply(ctx, mutation); !errors.Is(err, context.Canceled) {
			t.Fatalf("install returned %v, want cancellation", err)
		}
		if !reflect.DeepEqual(owner.entries, beforeEntries) || !reflect.DeepEqual(owner.cells, beforeCells) || owner.nextEpoch != beforeEpoch {
			t.Fatalf("cancelled mutation published live state: entries=%v cells=%v epoch=%d", owner.entries, owner.cells, owner.nextEpoch)
		}
	})

	t.Run("epoch exhaustion", func(t *testing.T) {
		owner, err := newLookupOwner(&image)
		if err != nil {
			t.Fatal(err)
		}
		beforeEntries := append([]lookupEntry(nil), owner.entries...)
		beforeCells := append([]resolutionCell(nil), owner.cells...)
		owner.nextEpoch = math.MaxUint64
		if err := owner.apply(context.Background(), mutation); !errors.Is(err, errLookupEpochLimit) {
			t.Fatalf("install returned %v, want epoch exhaustion", err)
		}
		if !reflect.DeepEqual(owner.entries, beforeEntries) || !reflect.DeepEqual(owner.cells, beforeCells) || owner.nextEpoch != math.MaxUint64 {
			t.Fatal("epoch-exhausted mutation published live state")
		}
	})

	t.Run("zero-change at maximum epoch", func(t *testing.T) {
		owner, err := newLookupOwner(&image)
		if err != nil {
			t.Fatal(err)
		}
		owner.nextEpoch = math.MaxUint64
		before := append([]resolutionCell(nil), owner.cells...)
		if err := owner.apply(context.Background(), image.mutations[0]); err != nil {
			t.Fatalf("zero-change install at maximum epoch: %v", err)
		}
		if !reflect.DeepEqual(owner.cells, before) || owner.nextEpoch != math.MaxUint64 {
			t.Fatal("zero-change install changed cells or epoch")
		}
	})

	t.Run("live corruption", func(t *testing.T) {
		owner, err := newLookupOwner(&image)
		if err != nil {
			t.Fatal(err)
		}
		owner.cells[2].receipt.definition = 2
		beforeEntries := append([]lookupEntry(nil), owner.entries...)
		beforeCells := append([]resolutionCell(nil), owner.cells...)
		beforeEpoch := owner.nextEpoch
		if err := owner.apply(context.Background(), mutation); !errors.Is(err, errLookupCorrupt) {
			t.Fatalf("install returned %v, want corruption", err)
		}
		if !reflect.DeepEqual(owner.entries, beforeEntries) || !reflect.DeepEqual(owner.cells, beforeCells) || owner.nextEpoch != beforeEpoch {
			t.Fatal("corruption repair was partially published")
		}
	})
}

func TestLookupTopologyTransactionsPublishBitsAndSelectiveEpochs(t *testing.T) {
	t.Parallel()

	image, owner := newTarget39LookupOwner(t, 18)
	if owner.topology.activations != 0 {
		t.Fatalf("initial topology activations = %02b, want 00", owner.topology.activations)
	}
	wantChanged := [...]int{0, 3, 0, 1, 3}
	wantActivations := [...]uint64{0b00, 0b01, 0b11, 0b11, 0b11}
	for offset, wantK := range wantChanged {
		mutation := image.mutations[18+offset]
		beforeCells := append([]resolutionCell(nil), owner.cells...)
		beforeEpoch := owner.nextEpoch
		if err := owner.apply(context.Background(), mutation); err != nil {
			t.Fatalf("apply Target39 mutation %d: %v", mutation.id, err)
		}
		if got := changedLookupEpochs(beforeCells, owner.cells); got != wantK {
			t.Fatalf("mutation %d changed K=%d cells, want %d", mutation.id, got, wantK)
		}
		if owner.nextEpoch-beforeEpoch != uint64(wantK) {
			t.Fatalf("mutation %d epoch delta = %d, want %d", mutation.id, owner.nextEpoch-beforeEpoch, wantK)
		}
		if owner.topology.activations != wantActivations[offset] {
			t.Fatalf("mutation %d activations = %02b, want %02b", mutation.id, owner.topology.activations, wantActivations[offset])
		}
		assertLookupScratchClear(t, owner)
		if offset == 2 && !reflect.DeepEqual(owner.cells, beforeCells) {
			t.Fatal("empty prepend changed a receipt or epoch")
		}
	}

	label := selectorID(0)
	for _, selector := range image.selectors {
		if selector.nameHash == selectorNameHash("label") {
			label = selector.id
			break
		}
	}
	if label == 0 {
		t.Fatal("checked label selector is absent")
	}
	wants := []struct {
		dispatch dispatchClassID
		owner    lookupNodeID
		outcome  lookupOutcome
	}{
		{dispatch: 1, owner: 5, outcome: lookupResolved}, // Alpha -> IncludedLabel v2.
		{dispatch: 2, owner: 6, outcome: lookupResolved}, // Beta -> PrependedLabel v1.
		{dispatch: 3, owner: 4, outcome: lookupUndef},    // Gamma's local undef remains terminal.
		{dispatch: 4, owner: 5, outcome: lookupResolved}, // Delta inherits Alpha's include.
		{dispatch: 5, owner: 5, outcome: lookupResolved}, // The singleton row inherits Alpha before its own definition.
	}
	for _, want := range wants {
		receipt, err := owner.canonical(want.dispatch, label)
		if err != nil || receipt.outcome != want.outcome || receipt.owner != want.owner {
			t.Fatalf("final dispatch %d label = %#v, %v; want outcome %d owner %d", want.dispatch, receipt, err, want.outcome, want.owner)
		}
		assertLookupScratchClear(t, owner)
	}

	// The empty prepend is a real topology publication even when nextEpoch has
	// no capacity. Prepare through include, force the epoch ceiling, and prove
	// the activation bank commits while every effective cell remains exact.
	maxImage, maxOwner := newTarget39LookupOwner(t, 20)
	if maxOwner.topology.activations != 0b01 {
		t.Fatalf("pre-prepend activations = %02b, want 01", maxOwner.topology.activations)
	}
	beforeCells := append([]resolutionCell(nil), maxOwner.cells...)
	maxOwner.nextEpoch = math.MaxUint64
	if err := maxOwner.apply(context.Background(), maxImage.mutations[20]); err != nil {
		t.Fatalf("K0 prepend at maximum epoch: %v", err)
	}
	if maxOwner.topology.activations != 0b11 || maxOwner.nextEpoch != math.MaxUint64 || !reflect.DeepEqual(maxOwner.cells, beforeCells) {
		t.Fatalf("K0 prepend publication topology=%02b epoch=%d cells-changed=%v", maxOwner.topology.activations, maxOwner.nextEpoch, !reflect.DeepEqual(maxOwner.cells, beforeCells))
	}
	length, err := maxOwner.deriveRoute(maxOwner.topology.activations, maxImage.rows[1])
	if err != nil || !reflect.DeepEqual(maxOwner.routeScratch[:length], []lookupNodeID{6, 3, 1}) {
		t.Fatalf("published empty-prepend Beta route = %v, %v; want [6 3 1]", maxOwner.routeScratch[:length], err)
	}
	maxOwner.clearRouteScratch()
	assertLookupScratchClear(t, maxOwner)
}

func TestLookupTopologyTransactionsArePublishLastAndEraseScratch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(*lookupOwner, *lookupImage)
		ctx     func() context.Context
		wantErr error
	}{
		{
			name:    "cancellation",
			ctx:     func() context.Context { return &cancelAfterContext{allowedErrCalls: 2} },
			wantErr: context.Canceled,
		},
		{
			name:    "final publish poll cancellation",
			ctx:     func() context.Context { return &cancelAfterContext{allowedErrCalls: 6} },
			wantErr: context.Canceled,
		},
		{
			name: "epoch exhaustion",
			prepare: func(owner *lookupOwner, _ *lookupImage) {
				owner.nextEpoch = math.MaxUint64 - 1
			},
			ctx:     context.Background,
			wantErr: errLookupEpochLimit,
		},
		{
			name: "unrelated live cell corruption",
			prepare: func(owner *lookupOwner, image *lookupImage) {
				selector := len(image.selectors) - 1
				owner.cells[2*len(image.selectors)+selector].receipt.reserved32 = 1
			},
			ctx:     context.Background,
			wantErr: errLookupCorrupt,
		},
		{
			name: "sealed oracle corruption",
			prepare: func(_ *lookupOwner, image *lookupImage) {
				for _, oracle := range image.routeOracles {
					if oracle.activations == 0b01 && oracle.row == 1 {
						image.oraclePaths[oracle.pathStart] = 1
						return
					}
				}
				panic("missing mask-01 Alpha oracle")
			},
			ctx:     context.Background,
			wantErr: errLookupCorrupt,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image, owner := newTarget39LookupOwner(t, 19)
			if test.prepare != nil {
				test.prepare(owner, image)
			}
			beforeEntries := append([]lookupEntry(nil), owner.entries...)
			beforeCells := append([]resolutionCell(nil), owner.cells...)
			beforeTopology := owner.topology
			beforeEpoch := owner.nextEpoch
			if err := owner.apply(test.ctx(), image.mutations[19]); !errors.Is(err, test.wantErr) {
				t.Fatalf("include returned %v, want %v", err, test.wantErr)
			}
			if !reflect.DeepEqual(owner.entries, beforeEntries) || !reflect.DeepEqual(owner.cells, beforeCells) ||
				owner.topology != beforeTopology || owner.nextEpoch != beforeEpoch {
				t.Fatalf("failed topology operation published live state: topology=%#v entries=%v cells=%v epoch=%d", owner.topology, owner.entries, owner.cells, owner.nextEpoch)
			}
			assertLookupScratchClear(t, owner)
		})
	}

	image, owner := newTarget39LookupOwner(t, 19)
	if err := owner.apply(context.Background(), image.mutations[20]); !errors.Is(err, errLookupCorrupt) {
		t.Fatalf("skipped include accepted prepend: %v", err)
	}
	if err := owner.apply(context.Background(), image.mutations[19]); err != nil {
		t.Fatalf("apply include after rejected skip: %v", err)
	}
	if err := owner.apply(context.Background(), image.mutations[19]); !errors.Is(err, errLookupCorrupt) {
		t.Fatalf("replayed include returned %v, want corruption", err)
	}
	assertLookupScratchClear(t, owner)

	owner.close()
	owner.close()
	if owner.topology != (lookupTopologyBank{}) || owner.stagedTopology != (lookupTopologyBank{}) ||
		owner.routeScratch != nil || owner.routeMarks != nil {
		t.Fatalf("closed topology owner retained banks or scratch: %#v", owner)
	}
}

func TestLookupTopologyValidationRejectsDuplicateCycleOverflowAndBadOracles(t *testing.T) {
	t.Parallel()

	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}

	t.Run("seal owns attachment and oracle facts", func(t *testing.T) {
		source := program.checked.lookup
		attachments := append([]lookupAttachmentDescriptor(nil), source.attachments...)
		oracles := append([]lookupRouteOracle(nil), source.routeOracles...)
		paths := append([]lookupNodeID(nil), source.oraclePaths...)
		sealed := sealLookupImage(source.attribution, source.nodes, source.rows, source.selectors, source.definitions, attachments, source.mutations, oracles, paths, source.initialEntries, source.initialReceipts)
		attachments[0].module = 0
		oracles[0].row = 0
		paths[0] = 0
		if err := validateLookupImage(&sealed); err != nil {
			t.Fatalf("builder mutation changed sealed topology facts: %v", err)
		}
	})

	t.Run("duplicate attachment", func(t *testing.T) {
		image := cloneLookupImage(program.checked.lookup)
		image.attachments[1] = lookupAttachmentDescriptor{id: 2, target: 2, module: 5, ordinal: 1, kind: lookupAttachmentInclude}
		image.nodes[1].attachmentCount = 2
		for index := 2; index < len(image.nodes); index++ {
			image.nodes[index].attachmentStart = 2
		}
		image.identity = lookupImageIdentity(&image)
		if err := validateLookupImage(&image); !errors.Is(err, errLookupImage) {
			t.Fatalf("duplicate attachment validation = %v, want %v", err, errLookupImage)
		}
	})

	t.Run("missing required oracle mask", func(t *testing.T) {
		image := cloneLookupImage(program.checked.lookup)
		dropLookupOracleMask(&image, 0b01)
		image.identity = lookupImageIdentity(&image)
		if err := validateLookupImage(&image); !errors.Is(err, errLookupImage) {
			t.Fatalf("missing required oracle validation = %v, want %v", err, errLookupImage)
		}
	})

	t.Run("duplicate oracle mask", func(t *testing.T) {
		image := cloneLookupImage(program.checked.lookup)
		rowCount := len(image.rows)
		for index := rowCount; index < 2*rowCount; index++ {
			image.routeOracles[index].activations = 0
		}
		image.identity = lookupImageIdentity(&image)
		if err := validateLookupImage(&image); !errors.Is(err, errLookupImage) {
			t.Fatalf("duplicate oracle mask validation = %v, want %v", err, errLookupImage)
		}
	})

	t.Run("oracle route mismatch", func(t *testing.T) {
		image := cloneLookupImage(program.checked.lookup)
		image.oraclePaths[0] = 1
		image.identity = lookupImageIdentity(&image)
		if err := validateLookupImage(&image); !errors.Is(err, errLookupImage) {
			t.Fatalf("oracle mismatch validation = %v, want %v", err, errLookupImage)
		}
	})

	t.Run("attachment cycle", func(t *testing.T) {
		image := lookupImage{
			nodes: []lookupNodeDescriptor{
				{id: 1, attachmentStart: 0, attachmentCount: 1, kind: lookupNodeClass},
				{id: 2, attachmentStart: 1, attachmentCount: 1, kind: lookupNodeModule},
				{id: 3, attachmentStart: 2, attachmentCount: 1, kind: lookupNodeModule},
			},
			attachments: []lookupAttachmentDescriptor{
				{id: 1, target: 1, module: 2, kind: lookupAttachmentInclude},
				{id: 2, target: 2, module: 3, kind: lookupAttachmentInclude},
				{id: 3, target: 3, module: 2, kind: lookupAttachmentInclude},
			},
		}
		var route [maximumLookupDepth]lookupNodeID
		var marks [maximumLookupNodes]uint8
		if _, err := deriveLookupRoute(&image, 0b111, 1, route[:], marks[:]); !errors.Is(err, errLookupCorrupt) {
			t.Fatalf("cyclic route derivation = %v, want %v", err, errLookupCorrupt)
		}
		for index, mark := range marks[:len(image.nodes)] {
			if mark != 0 {
				t.Fatalf("cycle rejection retained traversal mark %d=%d", index, mark)
			}
		}
	})

	t.Run("route overflow", func(t *testing.T) {
		nodes := make([]lookupNodeDescriptor, maximumLookupDepth+1)
		for index := range nodes {
			nodes[index] = lookupNodeDescriptor{id: lookupNodeID(index + 1), superclass: lookupNodeID(index), kind: lookupNodeClass}
		}
		image := lookupImage{nodes: nodes}
		var route [maximumLookupDepth]lookupNodeID
		var marks [maximumLookupNodes]uint8
		if _, err := deriveLookupRoute(&image, 0, lookupNodeID(len(nodes)), route[:], marks[:]); !errors.Is(err, errLookupCorrupt) {
			t.Fatalf("overflow route derivation = %v, want %v", err, errLookupCorrupt)
		}
		for index, mark := range marks[:len(nodes)] {
			if mark != 0 {
				t.Fatalf("overflow rejection retained traversal mark %d=%d", index, mark)
			}
		}
	})
}

func TestLookupOwnerCloseClearsState(t *testing.T) {
	t.Parallel()

	image := sealD1LookupBuild(d1LookupBuild())
	owner, err := newLookupOwner(&image)
	if err != nil {
		t.Fatal(err)
	}
	owner.close()
	owner.close()
	if owner.image != nil || owner.entries != nil || owner.cells != nil || owner.staged != nil || owner.changed != nil || owner.marks != nil ||
		owner.topology != (lookupTopologyBank{}) || owner.stagedTopology != (lookupTopologyBank{}) ||
		owner.routeScratch != nil || owner.routeMarks != nil || owner.nextEpoch != 0 {
		t.Fatalf("closed lookup owner retained state: %#v", owner)
	}
	if _, err := owner.canonical(1, 1); !errors.Is(err, errLookupCorrupt) {
		t.Fatalf("closed owner canonical lookup returned %v, want %v", err, errLookupCorrupt)
	}

	fresh, err := newLookupOwner(&image)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == owner || fresh.nextEpoch != 3 || len(fresh.cells) != 3 {
		t.Fatalf("fresh owner did not reconstruct cold state: %#v", fresh)
	}
}

func newTarget39LookupOwner(t *testing.T, appliedMutations int) (*lookupImage, *lookupOwner) {
	t.Helper()
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	if image == nil || len(image.mutations) != 24 || appliedMutations < 0 || appliedMutations > len(image.mutations) {
		t.Fatalf("Target39 lookup image/mutation prefix = %#v/%d", image, appliedMutations)
	}
	owner, err := newLookupOwner(image)
	if err != nil {
		t.Fatalf("new Target39 lookup owner: %v", err)
	}
	for index := 0; index < appliedMutations; index++ {
		if err := owner.apply(context.Background(), image.mutations[index]); err != nil {
			t.Fatalf("apply Target39 prefix mutation %d: %v", index+1, err)
		}
	}
	assertLookupScratchClear(t, owner)
	return image, owner
}

func changedLookupEpochs(before, after []resolutionCell) int {
	if len(before) != len(after) {
		return -1
	}
	changed := 0
	for index := range before {
		if before[index].epoch != after[index].epoch {
			changed++
		}
	}
	return changed
}

func assertLookupScratchClear(t *testing.T, owner *lookupOwner) {
	t.Helper()
	for index, node := range owner.routeScratch {
		if node != 0 {
			t.Fatalf("lookup route scratch retained node %d=%d", index, node)
		}
	}
	for index, mark := range owner.routeMarks {
		if mark != 0 {
			t.Fatalf("lookup route scratch retained mark %d=%d", index, mark)
		}
	}
}

func cloneLookupImage(source *lookupImage) lookupImage {
	clone := *source
	clone.nodes = append([]lookupNodeDescriptor(nil), source.nodes...)
	clone.rows = append([]dispatchRowDescriptor(nil), source.rows...)
	clone.selectors = append([]selectorDescriptor(nil), source.selectors...)
	clone.definitions = append([]definitionDescriptor(nil), source.definitions...)
	clone.attachments = append([]lookupAttachmentDescriptor(nil), source.attachments...)
	clone.mutations = append([]lookupMutationDescriptor(nil), source.mutations...)
	clone.routeOracles = append([]lookupRouteOracle(nil), source.routeOracles...)
	clone.oraclePaths = append([]lookupNodeID(nil), source.oraclePaths...)
	clone.initialEntries = append([]lookupEntry(nil), source.initialEntries...)
	clone.initialReceipts = append([]lookupReceipt(nil), source.initialReceipts...)
	return clone
}

func dropLookupOracleMask(image *lookupImage, drop uint64) {
	oracles := make([]lookupRouteOracle, 0, len(image.routeOracles)-len(image.rows))
	paths := make([]lookupNodeID, 0, len(image.oraclePaths))
	for _, oracle := range image.routeOracles {
		if oracle.activations == drop {
			continue
		}
		end := int(oracle.pathStart) + int(oracle.pathLength)
		path := image.oraclePaths[int(oracle.pathStart):end]
		oracle.pathStart = uint32(len(paths))
		oracles = append(oracles, oracle)
		paths = append(paths, path...)
	}
	image.routeOracles = oracles
	image.oraclePaths = paths
}

type d1LookupBuilder struct {
	nodes        []lookupNodeDescriptor
	rows         []dispatchRowDescriptor
	selectors    []selectorDescriptor
	definitions  []definitionDescriptor
	attachments  []lookupAttachmentDescriptor
	mutations    []lookupMutationDescriptor
	routeOracles []lookupRouteOracle
	oraclePaths  []lookupNodeID
	entries      []lookupEntry
	receipts     []lookupReceipt
}

func d1LookupBuild() d1LookupBuilder {
	return d1LookupBuilder{
		nodes: []lookupNodeDescriptor{
			{id: 1, kind: lookupNodeClass},
			{id: 2, superclass: 1, dispatch: 1, kind: lookupNodeClass},
			{id: 3, superclass: 1, dispatch: 2, kind: lookupNodeClass},
			{id: 4, superclass: 1, dispatch: 3, kind: lookupNodeClass},
		},
		rows: []dispatchRowDescriptor{
			{id: 1, node: 2, cellStart: 0, selectorCount: 1},
			{id: 2, node: 3, cellStart: 1, selectorCount: 1},
			{id: 3, node: 4, cellStart: 2, selectorCount: 1},
		},
		selectors: []selectorDescriptor{{id: 1, ordinal: 0, nameHash: selectorNameHash("label")}},
		definitions: []definitionDescriptor{
			{id: 1, owner: 1, selector: 1, slot: 1, body: 1},
			{id: 2, owner: 3, selector: 1, slot: 2, body: 2},
			{id: 3, owner: 4, selector: 1, slot: 3, body: 3},
			{id: 4, owner: 1, selector: 1, slot: 1, body: 4},
		},
		mutations: []lookupMutationDescriptor{
			{id: 1, owner: 1, selector: 1, kind: lookupMutationDefine,
				before: lookupEntry{slot: 1, definition: 1, kind: lookupEntryDefined, visibility: lookupVisibilityPublic},
				after:  lookupEntry{slot: 1, definition: 1, kind: lookupEntryDefined, visibility: lookupVisibilityPublic}},
			{id: 2, owner: 1, selector: 1, kind: lookupMutationDefine,
				before: lookupEntry{slot: 1, definition: 1, kind: lookupEntryDefined, visibility: lookupVisibilityPublic},
				after:  lookupEntry{slot: 1, definition: 4, kind: lookupEntryDefined, visibility: lookupVisibilityPublic}},
		},
		routeOracles: []lookupRouteOracle{
			{activations: 0, row: 1, pathStart: 0, pathLength: 2},
			{activations: 0, row: 2, pathStart: 2, pathLength: 2},
			{activations: 0, row: 3, pathStart: 4, pathLength: 2},
		},
		oraclePaths: []lookupNodeID{2, 1, 3, 1, 4, 1},
		entries: []lookupEntry{
			{slot: 1, definition: 1, kind: lookupEntryDefined, visibility: lookupVisibilityPublic},
			{},
			{slot: 2, definition: 2, kind: lookupEntryDefined, visibility: lookupVisibilityPublic},
			{slot: 3, definition: 3, kind: lookupEntryDefined, visibility: lookupVisibilityPublic},
		},
		receipts: []lookupReceipt{
			{owner: 1, slot: 1, definition: 1, outcome: lookupResolved, visibility: lookupVisibilityPublic},
			{owner: 3, slot: 2, definition: 2, outcome: lookupResolved, visibility: lookupVisibilityPublic},
			{owner: 4, slot: 3, definition: 3, outcome: lookupResolved, visibility: lookupVisibilityPublic},
		},
	}
}

func sealD1LookupBuild(build d1LookupBuilder) lookupImage {
	attribution := sha256.Sum256([]byte("rubyproof d1 lookup test"))
	return sealLookupImage(attribution, build.nodes, build.rows, build.selectors, build.definitions, build.attachments, build.mutations, build.routeOracles, build.oraclePaths, build.entries, build.receipts)
}

type cancelAfterContext struct {
	context.Context
	allowedErrCalls int
	errCalls        int
}

func (c *cancelAfterContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterContext) Done() <-chan struct{}       { return nil }
func (c *cancelAfterContext) Value(any) any               { return nil }
func (c *cancelAfterContext) Err() error {
	c.errCalls++
	if c.errCalls > c.allowedErrCalls {
		return context.Canceled
	}
	return nil
}
