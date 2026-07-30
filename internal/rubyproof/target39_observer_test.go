package rubyproof

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	goParser "go/parser"
	goToken "go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"unsafe"
)

const (
	target39SourceBytes      = 3_330
	target39BaseSourceBytes  = 2_480
	target39BaseSourceSHA256 = "aad50c456aad030a12365e85596cdc30ac1a8d85437c9adcacb6dbeb2c9907a5"
	target39ExtensionSHA256  = "4c3388faa84323c4382ec815006e1a799f74b85738d4bd20148f8bb92429acf0"
	target39SourceSHA256     = "8f2611ea5042f09f8313c1291af5b44a5bbf8e76ae3733e0745579e8c838c168"
	target39OutputSHA256     = "01c3e3bbbf334e42fe3940a0ef2c8d2e25ec0891b23ffc36a3bc9020ce066ea0"
	target39BaseOutput       = "true|false|1|3|4|1|3|2|2|3|4|1|2|5|5|2|3|2|2|14|106|66776777124532|88887|99"
	target39TopologyOutput   = "2|11|2|22|13|1|713726"
)

// target39LiteralRoutes is a handwritten observer fact. Production route
// construction must consume superclass and attachment facts, not this table.
// Mask 10 is intentionally unreachable in ProofSource: observing it prevents
// an implementation from disguising three sealed route snapshots as an
// attachment-based route constructor.
var target39LiteralRoutes = []struct {
	name string
	mask uint64
	rows [4][]lookupNodeID
}{
	{
		name: "none", mask: 0b00,
		rows: [4][]lookupNodeID{{2, 1}, {3, 1}, {4, 1}, {7, 2, 1}},
	},
	{
		name: "include", mask: 0b01,
		rows: [4][]lookupNodeID{{2, 5, 1}, {3, 1}, {4, 1}, {7, 2, 5, 1}},
	},
	{
		name: "prepend_only", mask: 0b10,
		rows: [4][]lookupNodeID{{2, 1}, {6, 3, 1}, {4, 1}, {7, 2, 1}},
	},
	{
		name: "include_then_prepend", mask: 0b11,
		rows: [4][]lookupNodeID{{2, 5, 1}, {6, 3, 1}, {4, 1}, {7, 2, 5, 1}},
	},
}

// target39FrozenSnapshotB is the strongest closed-C1 alternative to the
// selected attachment representation. It is intentionally test-only: the
// four activation masks select four sealed snapshots, whose row spans index
// exactly the 42 independently frozen path slots below. Neither the snapshot
// table nor its resolver is available to production lookup.
//
// The compact widths are fair for this exact finite closure: both B and C use
// implicit one-based IDs, uint8 indexes/counts where seven nodes and 42 slots
// fit, and the same four external dispatch-row roots. Common entry, dense-cell,
// epoch, and changed-cell storage is excluded from both topology payloads.
type target39SnapshotSpan struct {
	start, length uint8
}

type target39SnapshotB struct {
	rows [4]target39SnapshotSpan
}

type target39SnapshotBImage struct {
	snapshots [4]target39SnapshotB
	paths     [42]lookupNodeID
}

type target39SnapshotBOwner struct {
	live, staged uint8
}

var target39FrozenSnapshotB = target39SnapshotBImage{
	snapshots: [4]target39SnapshotB{
		{rows: [4]target39SnapshotSpan{{0, 2}, {2, 2}, {4, 2}, {6, 3}}},
		{rows: [4]target39SnapshotSpan{{9, 3}, {12, 2}, {14, 2}, {16, 4}}},
		{rows: [4]target39SnapshotSpan{{20, 2}, {22, 3}, {25, 2}, {27, 3}}},
		{rows: [4]target39SnapshotSpan{{30, 3}, {33, 3}, {36, 2}, {38, 4}}},
	},
	paths: [42]lookupNodeID{
		2, 1, 3, 1, 4, 1, 7, 2, 1,
		2, 5, 1, 3, 1, 4, 1, 7, 2, 5, 1,
		2, 1, 6, 3, 1, 4, 1, 7, 2, 1,
		2, 5, 1, 6, 3, 1, 4, 1, 7, 2, 5, 1,
	},
}

// These are compact test-only replicas of C's generated topology facts. They
// contain every field needed to derive a route and no route or snapshot.
type target39AttachmentCNode struct {
	superclass      lookupNodeID
	attachmentStart uint8
	attachmentCount uint8
	kind            lookupNodeKind
}

type target39AttachmentCFact struct {
	target lookupNodeID
	module lookupNodeID
	kind   lookupAttachmentKind
}

type target39AttachmentCImage struct {
	nodes       [7]target39AttachmentCNode
	attachments [2]target39AttachmentCFact
}

type target39AttachmentCWorkspace struct {
	nodes [7]lookupNodeID
	marks [7]uint8
}

type target39AttachmentCOwner struct {
	live, staged uint64
	workspace    target39AttachmentCWorkspace
}

var target39FrozenAttachmentC = target39AttachmentCImage{
	nodes: [7]target39AttachmentCNode{
		{kind: lookupNodeClass},
		{superclass: 1, attachmentCount: 1, kind: lookupNodeClass},
		{superclass: 1, attachmentStart: 1, attachmentCount: 1, kind: lookupNodeClass},
		{superclass: 1, attachmentStart: 2, kind: lookupNodeClass},
		{attachmentStart: 2, kind: lookupNodeModule},
		{attachmentStart: 2, kind: lookupNodeModule},
		{superclass: 2, attachmentStart: 2, kind: lookupNodeClass},
	},
	attachments: [2]target39AttachmentCFact{
		{target: 2, module: 5, kind: lookupAttachmentInclude},
		{target: 3, module: 6, kind: lookupAttachmentPrepend},
	},
}

type target39ColdWork struct {
	routes          uint16
	routeSlots      uint16
	nodeFacts       uint16
	attachmentFacts uint16
	resolutions     uint16
	entryProbes     uint16
	cellChecks      uint16
	cellWrites      uint16
}

type target39ComparatorState struct {
	entries [56]lookupEntry
	cells   [32]lookupReceipt
}

func TestTarget39FrozenSourceAndOracleFacts(t *testing.T) {
	if len(ProofSource) < target39SourceBytes {
		t.Fatalf("ProofSource closure = %d bytes, shorter than frozen Target39 prefix", len(ProofSource))
	}
	target39Source := ProofSource[:target39SourceBytes]
	if strings.Count(target39Source, "\n") != 209 {
		t.Fatalf("Target39 source prefix = %d LF, want 209", strings.Count(target39Source, "\n"))
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(target39Source))); got != target39SourceSHA256 {
		t.Fatalf("Target39 source prefix SHA-256 = %s, want %s", got, target39SourceSHA256)
	}
	base, extension := target39Source[:target39BaseSourceBytes], target39Source[target39BaseSourceBytes:]
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(base))); got != target39BaseSourceSHA256 {
		t.Fatalf("base source SHA-256 = %s, want preserved C4 %s", got, target39BaseSourceSHA256)
	}
	if len(extension) != 850 || strings.Count(extension, "\n") != 51 {
		t.Fatalf("C1 extension closure = %d bytes/%d LF, want 850/51", len(extension), strings.Count(extension, "\n"))
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(extension))); got != target39ExtensionSHA256 {
		t.Fatalf("C1 extension SHA-256 = %s, want %s", got, target39ExtensionSHA256)
	}
	if ProofOracleOutput != target39BaseOutput {
		t.Fatalf("base oracle output changed:\n got %q\nwant %q", ProofOracleOutput, target39BaseOutput)
	}
	if ProofC1OracleOutput != target39TopologyOutput {
		t.Fatalf("topology oracle output changed:\n got %q\nwant %q", ProofC1OracleOutput, target39TopologyOutput)
	}
	twoLines := target39BaseOutput + "\n" + target39TopologyOutput + "\n"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(twoLines))); got != target39OutputSHA256 {
		t.Fatalf("two-line oracle SHA-256 = %s, want %s", got, target39OutputSHA256)
	}
}

func TestTarget39LiteralRouteOracleCoversEveryAttachmentMask(t *testing.T) {
	wantSlots := map[uint64]int{0b00: 9, 0b01: 11, 0b10: 10, 0b11: 12}
	seen := make(map[uint64]bool, len(target39LiteralRoutes))
	for _, state := range target39LiteralRoutes {
		if seen[state.mask] {
			t.Fatalf("duplicate literal route mask %02b", state.mask)
		}
		seen[state.mask] = true
		slots := 0
		for row, path := range state.rows {
			if len(path) == 0 || len(path) > maximumLookupDepth {
				t.Fatalf("%s row %d path length = %d", state.name, row+1, len(path))
			}
			for _, node := range path {
				if node == 0 || node > 7 {
					t.Fatalf("%s row %d contains node %d", state.name, row+1, node)
				}
			}
			slots += len(path)
		}
		if slots != wantSlots[state.mask] {
			t.Fatalf("%s route slots = %d, want %d", state.name, slots, wantSlots[state.mask])
		}
	}
	for mask := uint64(0); mask < 4; mask++ {
		if !seen[mask] {
			t.Fatalf("literal route oracle omits attachment mask %02b", mask)
		}
	}

	// These cross-state inequalities are deliberately route-only: receipts are
	// identical across the empty-prepend transition, so a cell-derived route
	// implementation cannot satisfy this observer.
	include := target39LiteralRoute(0b01)
	both := target39LiteralRoute(0b11)
	if equalLookupPath(include.rows[1], both.rows[1]) || !equalLookupPath(include.rows[0], both.rows[0]) ||
		!equalLookupPath(include.rows[2], both.rows[2]) || !equalLookupPath(include.rows[3], both.rows[3]) {
		t.Fatalf("empty prepend literal routes do not isolate Beta: include=%v both=%v", include.rows, both.rows)
	}
}

func TestTarget39CheckedTopologyFactsMatchIndependentLiterals(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	if image == nil {
		t.Fatal("checked program has no lookup image")
	}

	wantNodes := []lookupNodeDescriptor{
		{id: 1, kind: lookupNodeClass},
		{id: 2, superclass: 1, dispatch: 1, attachmentStart: 0, attachmentCount: 1, kind: lookupNodeClass},
		{id: 3, superclass: 1, dispatch: 2, attachmentStart: 1, attachmentCount: 1, kind: lookupNodeClass},
		{id: 4, superclass: 1, dispatch: 3, attachmentStart: 2, kind: lookupNodeClass},
		{id: 5, attachmentStart: 2, kind: lookupNodeModule},
		{id: 6, attachmentStart: 2, kind: lookupNodeModule},
		{id: 7, superclass: 2, dispatch: 4, attachmentStart: 2, kind: lookupNodeClass},
	}
	if len(image.nodes) < len(wantNodes) {
		t.Fatalf("lookup nodes = %d, want at least historical prefix %d", len(image.nodes), len(wantNodes))
	}
	for index, want := range wantNodes {
		if got := image.nodes[index]; got != want {
			t.Errorf("lookup node %d = %#v, want %#v", index+1, got, want)
		}
	}
	wantRows := []dispatchRowDescriptor{
		{id: 1, node: 2, cellStart: 0, selectorCount: 8},
		{id: 2, node: 3, cellStart: 8, selectorCount: 8},
		{id: 3, node: 4, cellStart: 16, selectorCount: 8},
		{id: 4, node: 7, cellStart: 24, selectorCount: 8},
	}
	if len(image.rows) < len(wantRows) {
		t.Fatalf("dispatch rows = %d, want at least historical prefix %d", len(image.rows), len(wantRows))
	}
	for index, want := range wantRows {
		if got := image.rows[index]; got != want {
			t.Errorf("dispatch row %d = %#v, want %#v", index+1, got, want)
		}
	}
	wantAttachments := []lookupAttachmentDescriptor{
		{id: 1, target: 2, module: 5, ordinal: 0, kind: lookupAttachmentInclude},
		{id: 2, target: 3, module: 6, ordinal: 0, kind: lookupAttachmentPrepend},
	}
	if len(image.attachments) != len(wantAttachments) {
		t.Fatalf("attachments = %d, want %d", len(image.attachments), len(wantAttachments))
	}
	for index, want := range wantAttachments {
		if got := image.attachments[index]; got != want {
			t.Errorf("attachment %d = %#v, want %#v", index+1, got, want)
		}
	}
	if len(image.routeOracles) < len(target39LiteralRoutes)*len(image.rows) {
		t.Fatalf("sealed route comparison facts = %d, want at least %d for the extended row stride", len(image.routeOracles), len(target39LiteralRoutes)*len(image.rows))
	}
	for maskIndex, state := range target39LiteralRoutes {
		for rowIndex, wantPath := range state.rows {
			oracle := image.routeOracles[maskIndex*len(image.rows)+rowIndex]
			if oracle.activations != state.mask || oracle.row != wantRows[rowIndex].id || oracle.pathLength != uint16(len(wantPath)) {
				t.Errorf("sealed route oracle %s row %d = %#v", state.name, rowIndex+1, oracle)
			}
			pathStart, end := int(oracle.pathStart), int(oracle.pathStart)+int(oracle.pathLength)
			if pathStart < 0 || end > len(image.oraclePaths) {
				t.Fatalf("sealed route span %s row %d = %d:%d outside %d", state.name, rowIndex+1, pathStart, end, len(image.oraclePaths))
			}
			if got := image.oraclePaths[pathStart:end]; !equalLookupPath(got, wantPath) {
				t.Errorf("sealed route path %s row %d = %v, want independent literal %v", state.name, rowIndex+1, got, wantPath)
			}
		}
	}

	// Deliberately remove the sealed comparison facts from a shallow image
	// copy. The canonical builder must still derive every route, including the
	// test-only mask 10, solely from nodes, attachments, and activation bits.
	withoutOracle := *image
	withoutOracle.routeOracles = nil
	withoutOracle.oraclePaths = nil
	for _, state := range target39LiteralRoutes {
		for rowIndex, row := range wantRows {
			var route [maximumLookupDepth]lookupNodeID
			var marks [maximumLookupNodes]uint8
			length, err := deriveLookupRoute(&withoutOracle, state.mask, row.node, route[:], marks[:])
			if err != nil {
				t.Fatalf("derive %s row %d: %v", state.name, rowIndex+1, err)
			}
			if got, want := route[:length], state.rows[rowIndex]; !equalLookupPath(got, want) {
				t.Errorf("derive %s row %d = %v, want independent literal %v", state.name, rowIndex+1, got, want)
			}
			for index, node := range route[length:] {
				if node != 0 {
					t.Errorf("derive %s row %d retained route scratch at %d: %d", state.name, rowIndex+1, length+index, node)
					break
				}
			}
			for index, mark := range marks[:len(image.nodes)] {
				if mark != 0 {
					t.Errorf("derive %s row %d retained traversal mark %d: %d", state.name, rowIndex+1, index, mark)
					break
				}
			}
		}
	}
}

func TestTarget39RouteDerivationAllocatesNothing(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	if image == nil {
		t.Fatal("checked program has no lookup image")
	}
	var route [maximumLookupDepth]lookupNodeID
	var marks [maximumLookupNodes]uint8
	var deriveErr error
	var total int
	allocations := testing.AllocsPerRun(1_000, func() {
		total = 0
		for _, state := range target39LiteralRoutes {
			for _, row := range image.rows[:4] {
				length, err := deriveLookupRoute(image, state.mask, row.node, route[:], marks[:])
				if err != nil {
					deriveErr = err
					return
				}
				total += length
			}
		}
	})
	if deriveErr != nil {
		t.Fatal(deriveErr)
	}
	if total != 42 {
		t.Fatalf("all-mask route slots = %d, want 42", total)
	}
	if allocations != 0 {
		t.Fatalf("all-mask route derivation allocations = %g, want 0", allocations)
	}
}

func TestTarget39FrozenSealedSnapshotComparatorLayoutAndColdWork(t *testing.T) {
	layouts := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"B row span", unsafe.Sizeof(target39SnapshotSpan{}), 2},
		{"B snapshot", unsafe.Sizeof(target39SnapshotB{}), 8},
		{"B immutable topology", unsafe.Sizeof(target39SnapshotBImage{}), 200},
		{"B mutable topology", unsafe.Sizeof(target39SnapshotBOwner{}), 2},
		{"B total topology", unsafe.Sizeof(target39SnapshotBImage{}) + unsafe.Sizeof(target39SnapshotBOwner{}), 202},
		{"C node", unsafe.Sizeof(target39AttachmentCNode{}), 8},
		{"C attachment", unsafe.Sizeof(target39AttachmentCFact{}), 12},
		{"C immutable topology", unsafe.Sizeof(target39AttachmentCImage{}), 80},
		{"C transient workspace", unsafe.Sizeof(target39AttachmentCWorkspace{}), 36},
		{"C mutable topology plus workspace", unsafe.Sizeof(target39AttachmentCOwner{}), 56},
		{"C total topology", unsafe.Sizeof(target39AttachmentCImage{}) + unsafe.Sizeof(target39AttachmentCOwner{}), 136},
	}
	for _, layout := range layouts {
		if layout.got != layout.want {
			t.Errorf("%s bytes = %d, want %d", layout.name, layout.got, layout.want)
		}
	}

	// Seal B against the independent all-mask literals and prove that C derives
	// the same 9/11/10/12 slots without consulting either snapshot array.
	var bAll, cAll target39ColdWork
	var workspace target39AttachmentCWorkspace
	for _, state := range target39LiteralRoutes {
		for row, want := range state.rows {
			bRoute, ok := target39SnapshotBRoute(state.mask, row, &bAll)
			if !ok || !equalLookupPath(bRoute, want) {
				t.Fatalf("B mask %02b row %d = %v/%v, want %v", state.mask, row+1, bRoute, ok, want)
			}
			cRoute, ok := target39AttachmentCRoute(state.mask, row, &workspace, &cAll)
			if !ok || !equalLookupPath(cRoute, want) {
				t.Fatalf("C mask %02b row %d = %v/%v, want %v", state.mask, row+1, cRoute, ok, want)
			}
		}
	}
	if bAll != (target39ColdWork{routes: 16, routeSlots: 42}) {
		t.Fatalf("B all-mask route work = %#v, want 16 routes/42 slots only", bAll)
	}
	if cAll != (target39ColdWork{routes: 16, routeSlots: 42, nodeFacts: 42, attachmentFacts: 24}) {
		t.Fatalf("C all-mask route work = %#v, want 16/42/42/24 route/slot/node/attachment facts", cAll)
	}

	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	if image == nil {
		t.Fatal("checked comparator image is nil")
	}
	if len(image.mutations) < 23 {
		t.Fatalf("checked comparator mutations = %d, want at least historical prefix 23", len(image.mutations))
	}
	initial := target39ComparatorInitialState(t, image)
	bState, cState := initial, initial
	bOwner := target39SnapshotBOwner{}
	cOwner := target39AttachmentCOwner{}

	// Metrics count deterministic cold structural work, not elapsed time. B
	// reads one sealed row span per route; C constructs the same route while
	// reading node and attachment facts. Both use the same test-only resolver,
	// so receipt resolutions, entry probes, cell checks, and writes are directly
	// comparable without either representation feeding production resolution.
	wantChanged := [...]int{0, 2, 0, 1, 2}
	var bWork, cWork [5]target39ColdWork
	for index, mutation := range image.mutations[18:23] {
		bOwner.staged = uint8(mutation.afterActivations)
		bChanged, err := target39ComparatorApply(&bState, uint64(bOwner.live), mutation, func(mask uint64, row int, work *target39ColdWork) ([]lookupNodeID, bool) {
			return target39SnapshotBRoute(mask, row, work)
		}, &bWork[index])
		if err != nil {
			t.Fatalf("B mutation %d: %v", mutation.id, err)
		}
		bOwner.live, bOwner.staged = bOwner.staged, bOwner.live
		cOwner.staged = mutation.afterActivations
		cChanged, err := target39ComparatorApply(&cState, cOwner.live, mutation, func(mask uint64, row int, work *target39ColdWork) ([]lookupNodeID, bool) {
			return target39AttachmentCRoute(mask, row, &cOwner.workspace, work)
		}, &cWork[index])
		if err != nil {
			t.Fatalf("C mutation %d: %v", mutation.id, err)
		}
		cOwner.live, cOwner.staged = cOwner.staged, cOwner.live
		cOwner.workspace = target39AttachmentCWorkspace{}
		if bChanged != wantChanged[index] || cChanged != wantChanged[index] || bState != cState || uint64(bOwner.live) != cOwner.live {
			t.Fatalf("mutation %d B/C changed=%d/%d state-equal=%v masks=%02b/%02b, want K=%d", mutation.id, bChanged, cChanged, bState == cState, bOwner.live, cOwner.live, wantChanged[index])
		}
	}

	// The per-operation vectors freeze bounded cold work for detached define
	// K0, include K2, empty prepend K0, late define K1, and redefine K2. They
	// are intentionally structural counters, not a timing-superiority claim.
	wantB := [5]target39ColdWork{
		{routes: 4, routeSlots: 9, resolutions: 8, entryProbes: 16, cellChecks: 4},
		{routes: 8, routeSlots: 20, resolutions: 64, entryProbes: 152, cellChecks: 32, cellWrites: 2},
		{routes: 8, routeSlots: 23, resolutions: 64, entryProbes: 174, cellChecks: 32},
		{routes: 4, routeSlots: 12, resolutions: 8, entryProbes: 16, cellChecks: 4, cellWrites: 1},
		{routes: 4, routeSlots: 12, resolutions: 8, entryProbes: 14, cellChecks: 4, cellWrites: 2},
	}
	wantC := [5]target39ColdWork{
		{routes: 4, routeSlots: 9, nodeFacts: 9, attachmentFacts: 6, resolutions: 8, entryProbes: 16, cellChecks: 4},
		{routes: 8, routeSlots: 20, nodeFacts: 20, attachmentFacts: 12, resolutions: 64, entryProbes: 152, cellChecks: 32, cellWrites: 2},
		{routes: 8, routeSlots: 23, nodeFacts: 23, attachmentFacts: 12, resolutions: 64, entryProbes: 174, cellChecks: 32},
		{routes: 4, routeSlots: 12, nodeFacts: 12, attachmentFacts: 6, resolutions: 8, entryProbes: 16, cellChecks: 4, cellWrites: 1},
		{routes: 4, routeSlots: 12, nodeFacts: 12, attachmentFacts: 6, resolutions: 8, entryProbes: 14, cellChecks: 4, cellWrites: 2},
	}
	if bWork != wantB || cWork != wantC {
		t.Fatalf("cold comparator metrics changed:\n B=%#v\n C=%#v", bWork, cWork)
	}
	if bOwner != (target39SnapshotBOwner{live: 0b11, staged: 0b11}) ||
		cOwner != (target39AttachmentCOwner{live: 0b11, staged: 0b11}) {
		t.Fatalf("comparator owners retained wrong state or scratch: B=%#v C=%#v", bOwner, cOwner)
	}
}

func TestTarget39OwnerPersistsBitsAndScratchButNoRouteAuthority(t *testing.T) {
	typeOf := reflect.TypeOf(lookupOwner{})
	wantFields := map[string]reflect.Type{
		"topology":       reflect.TypeOf(lookupTopologyBank{}),
		"stagedTopology": reflect.TypeOf(lookupTopologyBank{}),
		"routeScratch":   reflect.TypeOf([]lookupNodeID(nil)),
		"routeMarks":     reflect.TypeOf([]uint8(nil)),
	}
	for name, want := range wantFields {
		field, ok := typeOf.FieldByName(name)
		if !ok || field.Type != want {
			t.Errorf("lookupOwner.%s = %v, present=%v; want %v", name, field.Type, ok, want)
		}
	}
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		lower := strings.ToLower(field.Name)
		if strings.Contains(lower, "snapshot") || strings.Contains(lower, "stateid") || strings.Contains(lower, "path") ||
			strings.Contains(lower, "route") && field.Name != "routeScratch" && field.Name != "routeMarks" {
			t.Errorf("lookupOwner retains forbidden authority %s %v", field.Name, field.Type)
		}
	}
	bankType := reflect.TypeOf(lookupTopologyBank{})
	if bankType.NumField() != 1 || bankType.Field(0).Name != "activations" || bankType.Field(0).Type.Kind() != reflect.Uint64 {
		t.Fatalf("topology bank = %v, want only uint64 activations", bankType)
	}
}

func TestTarget39TopologyTransactionsPublishOnlySelectiveReceipts(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	image := program.checked.lookup
	if image == nil || len(image.mutations) < 23 {
		t.Fatalf("checked topology mutation inventory = %d, want at least historical prefix 23", len(image.mutations))
	}
	owner, err := newLookupOwner(image)
	if err != nil {
		t.Fatal(err)
	}

	// Execute the original eighteen operations exactly once. The nineteenth
	// operation defines IncludedLabel#label while the module is detached.
	for index := 0; index < 18; index++ {
		if err := owner.apply(context.Background(), image.mutations[index]); err != nil {
			t.Fatalf("apply original mutation %d: %v", index+1, err)
		}
	}
	label := program.checked.selectors["label"]
	if label == 0 {
		t.Fatal("checked label selector is absent")
	}
	wantMutation := func(index int, kind lookupMutationKind, ownerID lookupNodeID, attachment lookupAttachmentID, beforeMask, afterMask uint64) lookupMutationDescriptor {
		t.Helper()
		got := image.mutations[index]
		if got.id != uint32(index+1) || got.kind != kind || got.owner != ownerID || got.attachment != attachment ||
			got.beforeActivations != beforeMask || got.afterActivations != afterMask {
			t.Fatalf("mutation %d = %#v, want kind=%d owner=%d attachment=%d masks=%02b/%02b", index+1, got, kind, ownerID, attachment, beforeMask, afterMask)
		}
		return got
	}
	includedInitial := wantMutation(18, lookupMutationDefine, 5, 0, 0b00, 0b00)
	include := wantMutation(19, lookupMutationInclude, 2, 1, 0b00, 0b01)
	prepend := wantMutation(20, lookupMutationPrepend, 3, 2, 0b01, 0b11)
	prependedDefine := wantMutation(21, lookupMutationDefine, 6, 0, 0b11, 0b11)
	includedRedefine := wantMutation(22, lookupMutationDefine, 5, 0, 0b11, 0b11)
	for _, mutation := range []lookupMutationDescriptor{includedInitial, prependedDefine, includedRedefine} {
		if mutation.selector != label || mutation.before.kind == lookupEntryDefined && mutation.before.definition == mutation.after.definition ||
			mutation.after.kind != lookupEntryDefined || mutation.after.definition == 0 {
			t.Fatalf("definition mutation %d does not carry an exact label replacement: %#v", mutation.id, mutation)
		}
	}
	for _, mutation := range []lookupMutationDescriptor{include, prepend} {
		if mutation.selector != 0 || mutation.before != (lookupEntry{}) || mutation.after != (lookupEntry{}) {
			t.Fatalf("topology mutation %d retained an entry coordinate: %#v", mutation.id, mutation)
		}
	}

	beforeCells, beforeEpoch := append([]resolutionCell(nil), owner.cells...), owner.nextEpoch
	if err := owner.apply(context.Background(), includedInitial); err != nil {
		t.Fatal(err)
	}
	if changed := target39ChangedCells(beforeCells, owner.cells); len(changed) != 0 || owner.nextEpoch != beforeEpoch || owner.topology.activations != 0 {
		t.Fatalf("detached IncludedLabel definition published cells/epoch/topology: changed=%v epoch=%d/%d mask=%02b", changed, beforeEpoch, owner.nextEpoch, owner.topology.activations)
	}

	// Cancel after complete staging but before the publish-last boundary. Live
	// entries, cells, activation bits, and epoch must remain byte-identical and
	// the same descriptor must remain retryable.
	assertCanceled := func(name string, mutation lookupMutationDescriptor) {
		t.Helper()
		entries := append([]lookupEntry(nil), owner.entries...)
		cells := append([]resolutionCell(nil), owner.cells...)
		topology, epoch := owner.topology, owner.nextEpoch
		ctx := &target39CancelAfterContext{Context: context.Background(), allowedErrCalls: 6}
		if err := owner.apply(ctx, mutation); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s final-poll cancellation = %v, want canceled", name, err)
		}
		if !reflect.DeepEqual(owner.entries, entries) || !reflect.DeepEqual(owner.cells, cells) || owner.topology != topology || owner.nextEpoch != epoch {
			t.Fatalf("%s cancellation published live state", name)
		}
		target39RequireClearedScratch(t, owner)
	}

	alphaLabel := target39CellIndex(t, image, program.checked.classes["Alpha"].dispatch, label)
	betaLabel := target39CellIndex(t, image, program.checked.classes["Beta"].dispatch, label)
	deltaLabel := target39CellIndex(t, image, program.checked.classes["Delta"].dispatch, label)
	singletonLabel := target39CellIndex(t, image, program.checked.singletonOwner.dispatch, label)

	assertCanceled("include", include)
	beforeCells, beforeEpoch = append([]resolutionCell(nil), owner.cells...), owner.nextEpoch
	if err := owner.apply(context.Background(), include); err != nil {
		t.Fatal(err)
	}
	if changed := target39ChangedCells(beforeCells, owner.cells); fmt.Sprint(changed) != fmt.Sprint([]int{alphaLabel, deltaLabel, singletonLabel}) || owner.nextEpoch != beforeEpoch+3 || owner.topology.activations != 0b01 {
		t.Fatalf("include receipt publication changed=%v epoch=%d/%d mask=%02b", changed, beforeEpoch, owner.nextEpoch, owner.topology.activations)
	}
	target39RequireClearedScratch(t, owner)

	assertCanceled("empty prepend", prepend)
	beforeCells, beforeEpoch = append([]resolutionCell(nil), owner.cells...), owner.nextEpoch
	if err := owner.apply(context.Background(), prepend); err != nil {
		t.Fatal(err)
	}
	if changed := target39ChangedCells(beforeCells, owner.cells); len(changed) != 0 || owner.nextEpoch != beforeEpoch || owner.topology.activations != 0b11 {
		t.Fatalf("empty prepend K=0 publication changed=%v epoch=%d/%d mask=%02b", changed, beforeEpoch, owner.nextEpoch, owner.topology.activations)
	}
	target39RequireClearedScratch(t, owner)

	beforeCells, beforeEpoch = append([]resolutionCell(nil), owner.cells...), owner.nextEpoch
	if err := owner.apply(context.Background(), prependedDefine); err != nil {
		t.Fatal(err)
	}
	if changed := target39ChangedCells(beforeCells, owner.cells); fmt.Sprint(changed) != fmt.Sprint([]int{betaLabel}) || owner.nextEpoch != beforeEpoch+1 {
		t.Fatalf("late PrependedLabel definition changed=%v epoch=%d/%d", changed, beforeEpoch, owner.nextEpoch)
	}

	beforeCells, beforeEpoch = append([]resolutionCell(nil), owner.cells...), owner.nextEpoch
	if err := owner.apply(context.Background(), includedRedefine); err != nil {
		t.Fatal(err)
	}
	if changed := target39ChangedCells(beforeCells, owner.cells); fmt.Sprint(changed) != fmt.Sprint([]int{alphaLabel, deltaLabel, singletonLabel}) || owner.nextEpoch != beforeEpoch+3 {
		t.Fatalf("IncludedLabel redefinition changed=%v epoch=%d/%d", changed, beforeEpoch, owner.nextEpoch)
	}
	target39RequireClearedScratch(t, owner)

	owner.close()
	if owner.image != nil || owner.entries != nil || owner.cells != nil || owner.staged != nil || owner.changed != nil || owner.marks != nil ||
		owner.topology != (lookupTopologyBank{}) || owner.stagedTopology != (lookupTopologyBank{}) || owner.routeScratch != nil || owner.routeMarks != nil || owner.nextEpoch != 0 {
		t.Fatalf("closed Target39 owner retained topology or transaction state: %#v", owner)
	}
}

func TestTarget39GeneratedWarmFunctionsContainNoTopologyDependency(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate observer source")
	}
	generatedPath := filepath.Join(filepath.Dir(current), "generated", "ruby_generated.go")
	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := goParser.ParseFile(goToken.NewFileSet(), generatedPath, generated, 0)
	if err != nil {
		t.Fatal(err)
	}

	wantFunctions := map[string]bool{
		"readLabelAfterStep":  false,
		"readSavedAfterStep":  false,
		"readPublicAfterStep": false,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil {
			continue
		}
		if _, wanted := wantFunctions[function.Name.Name]; !wanted {
			continue
		}
		wantFunctions[function.Name.Name] = true
		var forbidden []string
		ast.Inspect(function.Body, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			lower := strings.ToLower(identifier.Name)
			for _, fragment := range []string{"attachment", "topology", "route"} {
				if strings.Contains(lower, fragment) {
					forbidden = append(forbidden, identifier.Name)
				}
			}
			return true
		})
		if len(forbidden) != 0 {
			sort.Strings(forbidden)
			t.Errorf("%s contains topology dependencies: %v", function.Name.Name, forbidden)
		}
	}
	for name, found := range wantFunctions {
		if !found {
			t.Errorf("generated warm function %s not found", name)
		}
	}
}

func target39LiteralRoute(mask uint64) struct {
	name string
	mask uint64
	rows [4][]lookupNodeID
} {
	for _, state := range target39LiteralRoutes {
		if state.mask == mask {
			return state
		}
	}
	panic("missing Target39 literal route")
}

func equalLookupPath(left, right []lookupNodeID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func target39CellIndex(t *testing.T, image *lookupImage, dispatch dispatchClassID, selector selectorID) int {
	t.Helper()
	if image == nil || dispatch == 0 || int(dispatch) > len(image.rows) || selector == 0 || int(selector) > len(image.selectors) {
		t.Fatalf("invalid cell coordinate dispatch=%d selector=%d", dispatch, selector)
	}
	return int(image.rows[dispatch-1].cellStart) + int(selector) - 1
}

func target39ChangedCells(before, after []resolutionCell) []int {
	if len(before) != len(after) {
		panic("Target39 cell banks differ in length")
	}
	var changed []int
	for index := range before {
		if before[index] != after[index] {
			changed = append(changed, index)
		}
	}
	return changed
}

func target39RequireClearedScratch(t *testing.T, owner *lookupOwner) {
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

func target39SnapshotBRoute(mask uint64, row int, work *target39ColdWork) ([]lookupNodeID, bool) {
	if mask >= uint64(len(target39FrozenSnapshotB.snapshots)) || row < 0 || row >= len(target39FrozenSnapshotB.snapshots[0].rows) {
		return nil, false
	}
	span := target39FrozenSnapshotB.snapshots[mask].rows[row]
	end := int(span.start) + int(span.length)
	if span.length == 0 || end > len(target39FrozenSnapshotB.paths) {
		return nil, false
	}
	if work != nil {
		work.routes++
		work.routeSlots += uint16(span.length)
	}
	return target39FrozenSnapshotB.paths[span.start:end], true
}

func target39AttachmentCRoute(mask uint64, row int, workspace *target39AttachmentCWorkspace, work *target39ColdWork) ([]lookupNodeID, bool) {
	if mask >= 1<<len(target39FrozenAttachmentC.attachments) || row < 0 || row >= 4 || workspace == nil {
		return nil, false
	}
	*workspace = target39AttachmentCWorkspace{}
	roots := [4]lookupNodeID{2, 3, 4, 7}
	length := 0
	if !target39AttachmentCAppend(mask, roots[row], workspace, &length, work) || length == 0 {
		*workspace = target39AttachmentCWorkspace{}
		return nil, false
	}
	clear(workspace.marks[:])
	if work != nil {
		work.routes++
		work.routeSlots += uint16(length)
	}
	return workspace.nodes[:length], true
}

func target39AttachmentCAppend(mask uint64, id lookupNodeID, workspace *target39AttachmentCWorkspace, length *int, work *target39ColdWork) bool {
	if id == 0 || int(id) > len(target39FrozenAttachmentC.nodes) || workspace == nil || length == nil {
		return false
	}
	mark := &workspace.marks[id-1]
	switch *mark {
	case 2:
		return true
	case 1:
		return false
	case 0:
	default:
		return false
	}
	*mark = 1
	node := target39FrozenAttachmentC.nodes[id-1]
	if work != nil {
		work.nodeFacts++
	}
	if node.kind != lookupNodeClass && node.kind != lookupNodeModule {
		return false
	}
	start, end := int(node.attachmentStart), int(node.attachmentStart)+int(node.attachmentCount)
	if start < 0 || end < start || end > len(target39FrozenAttachmentC.attachments) {
		return false
	}
	for index := end - 1; index >= start; index-- {
		attachment := target39FrozenAttachmentC.attachments[index]
		if work != nil {
			work.attachmentFacts++
		}
		if attachment.target != id {
			return false
		}
		if attachment.kind == lookupAttachmentPrepend && mask&(uint64(1)<<index) != 0 &&
			!target39AttachmentCAppend(mask, attachment.module, workspace, length, work) {
			return false
		}
	}
	if *length >= len(workspace.nodes) {
		return false
	}
	workspace.nodes[*length] = id
	(*length)++
	for index := end - 1; index >= start; index-- {
		attachment := target39FrozenAttachmentC.attachments[index]
		if work != nil {
			work.attachmentFacts++
		}
		if attachment.target != id {
			return false
		}
		if attachment.kind == lookupAttachmentInclude && mask&(uint64(1)<<index) != 0 &&
			!target39AttachmentCAppend(mask, attachment.module, workspace, length, work) {
			return false
		}
	}
	*mark = 2
	if node.kind == lookupNodeModule {
		return node.superclass == 0
	}
	if node.superclass == 0 {
		return true
	}
	return target39AttachmentCAppend(mask, node.superclass, workspace, length, work)
}

func target39ComparatorInitialState(t *testing.T, image *lookupImage) target39ComparatorState {
	t.Helper()
	if len(image.initialEntries) < 56 || len(image.rows) < 4 || len(image.selectors) != 8 {
		t.Fatalf("comparator closure entries/rows/selectors = %d/%d/%d, want at least historical 56/4/8", len(image.initialEntries), len(image.rows), len(image.selectors))
	}
	var state target39ComparatorState
	copy(state.entries[:], image.initialEntries[:56])
	for _, mutation := range image.mutations[:18] {
		if mutation.kind == lookupMutationInclude || mutation.kind == lookupMutationPrepend || mutation.beforeActivations != 0 || mutation.afterActivations != 0 {
			t.Fatalf("original mutation %d unexpectedly changes topology", mutation.id)
		}
		entryIndex := (int(mutation.owner)-1)*len(image.selectors) + int(mutation.selector) - 1
		if entryIndex < 0 || entryIndex >= len(state.entries) || state.entries[entryIndex] != mutation.before {
			t.Fatalf("original mutation %d entry precondition failed", mutation.id)
		}
		state.entries[entryIndex] = mutation.after
	}
	for row, route := range target39LiteralRoute(0).rows {
		for selectorIndex := 0; selectorIndex < len(image.selectors); selectorIndex++ {
			receipt, ok := target39ComparatorResolve(&state.entries, route, selectorID(selectorIndex+1), nil)
			if !ok {
				t.Fatalf("initial comparator resolve row %d selector %d", row+1, selectorIndex+1)
			}
			state.cells[row*len(image.selectors)+selectorIndex] = receipt
		}
	}
	return state
}

type target39RouteProvider func(mask uint64, row int, work *target39ColdWork) ([]lookupNodeID, bool)

func target39ComparatorApply(state *target39ComparatorState, activations uint64, mutation lookupMutationDescriptor, routeFor target39RouteProvider, work *target39ColdWork) (int, error) {
	if state == nil || routeFor == nil || work == nil || activations != mutation.beforeActivations {
		return 0, errLookupCorrupt
	}
	stagedEntries, stagedCells := state.entries, state.cells
	entryMutation := false
	switch mutation.kind {
	case lookupMutationDefine, lookupMutationAlias, lookupMutationRemove, lookupMutationUndef, lookupMutationVisibility:
		entryMutation = true
	case lookupMutationInclude, lookupMutationPrepend:
	default:
		return 0, errLookupCorrupt
	}
	if entryMutation {
		entryIndex := (int(mutation.owner)-1)*8 + int(mutation.selector) - 1
		if mutation.beforeActivations != mutation.afterActivations || entryIndex < 0 || entryIndex >= len(stagedEntries) || stagedEntries[entryIndex] != mutation.before {
			return 0, errLookupCorrupt
		}
		stagedEntries[entryIndex] = mutation.after
	} else if mutation.attachment == 0 || mutation.attachment > 2 ||
		mutation.afterActivations != mutation.beforeActivations|uint64(1)<<(mutation.attachment-1) {
		return 0, errLookupCorrupt
	}

	changed := 0
	for row := 0; row < 4; row++ {
		currentRoute, ok := routeFor(activations, row, work)
		if !ok {
			return 0, errLookupCorrupt
		}
		if entryMutation {
			selector := mutation.selector
			cellIndex := row*8 + int(selector) - 1
			current, ok := target39ComparatorResolve(&state.entries, currentRoute, selector, work)
			if !ok {
				return 0, errLookupCorrupt
			}
			proposed, ok := target39ComparatorResolve(&stagedEntries, currentRoute, selector, work)
			if !ok {
				return 0, errLookupCorrupt
			}
			work.cellChecks++
			if state.cells[cellIndex] != current {
				return 0, errLookupCorrupt
			}
			if proposed != current {
				stagedCells[cellIndex] = proposed
				changed++
				work.cellWrites++
			}
			continue
		}

		var currentReceipts [8]lookupReceipt
		for selectorIndex := 0; selectorIndex < 8; selectorIndex++ {
			selector := selectorID(selectorIndex + 1)
			cellIndex := row*8 + selectorIndex
			current, ok := target39ComparatorResolve(&state.entries, currentRoute, selector, work)
			if !ok {
				return 0, errLookupCorrupt
			}
			work.cellChecks++
			if state.cells[cellIndex] != current {
				return 0, errLookupCorrupt
			}
			currentReceipts[selectorIndex] = current
		}
		proposedRoute, ok := routeFor(mutation.afterActivations, row, work)
		if !ok {
			return 0, errLookupCorrupt
		}
		for selectorIndex := 0; selectorIndex < 8; selectorIndex++ {
			selector := selectorID(selectorIndex + 1)
			cellIndex := row*8 + selectorIndex
			proposed, ok := target39ComparatorResolve(&state.entries, proposedRoute, selector, work)
			if !ok {
				return 0, errLookupCorrupt
			}
			if proposed != currentReceipts[selectorIndex] {
				stagedCells[cellIndex] = proposed
				changed++
				work.cellWrites++
			}
		}
	}
	state.entries, state.cells = stagedEntries, stagedCells
	return changed, nil
}

func target39ComparatorResolve(entries *[56]lookupEntry, route []lookupNodeID, selector selectorID, work *target39ColdWork) (lookupReceipt, bool) {
	if entries == nil || len(route) == 0 || selector == 0 || selector > 8 {
		return lookupReceipt{}, false
	}
	if work != nil {
		work.resolutions++
	}
	for _, node := range route {
		entryIndex := (int(node)-1)*8 + int(selector) - 1
		if node == 0 || entryIndex < 0 || entryIndex >= len(entries) {
			return lookupReceipt{}, false
		}
		if work != nil {
			work.entryProbes++
		}
		entry := entries[entryIndex]
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

type target39CancelAfterContext struct {
	context.Context
	allowedErrCalls int
	errCalls        int
}

func (ctx *target39CancelAfterContext) Err() error {
	ctx.errCalls++
	if ctx.errCalls > ctx.allowedErrCalls {
		return context.Canceled
	}
	return nil
}
