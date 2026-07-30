package generated

import (
	"context"
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

type target37GeneratedLane uint8

const (
	target37GenericLane target37GeneratedLane = iota + 1
	target37SpecializedLane
)

var target37BenchmarkSink int64

//go:noinline
func target37BenchmarkGenericSend(e *Engine, c *rubyControl, object *rubyObject) (int64, error) {
	return target37GenericReadSelectorMode(e, c, object, rubyLabelSelector, &e.labelSite, false)
}

//go:noinline
func target37BenchmarkGenericPublic(e *Engine, c *rubyControl, object *rubyObject) (int64, error) {
	value, err := target37GenericReadSelectorMode(e, c, object, rubyLabelSelector, &e.publicSite, true)
	if err == errNoMethod {
		return rubyPublicRejected, nil
	}
	return value, err
}

// target37GenericReadSelectorMode is the pre-specialization generic algorithm
// compiled against the candidate's current facts and helpers. It is a
// test-only shape baseline, not a byte-identical copy of the prior artifact.
// Production generated code has one recovery authority:
// readSelectorColdAfterStep.
func target37GenericReadSelectorMode(e *Engine, c *rubyControl, object *rubyObject, selector rubySelectorID, site *rubySite, publicOnly bool) (int64, error) {
	_, err := c.enterFrame()
	if err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := c.step(); err != nil {
		return 0, err
	}
	shape, ok := rubyDispatchShape(object.dispatch)
	cellIndex, indexed := rubyCellIndex(object.dispatch, selector)
	if !ok || !indexed || shape != object.shape {
		return 0, ErrInternal
	}
	cell := e.lookup.cells[cellIndex]
	if !e.lookup.validCell(cell, selector) || cell.receipt.outcome != rubyResolved || !e.validateSite(site, selector) {
		return 0, ErrInternal
	}
	if publicOnly && cell.receipt.visibility != rubyPublic {
		return 0, errNoMethod
	}
	if rubyArmHits(&site.arms[0], object, &cell) {
		value, callErr := e.callLabelTarget(c, object, site.arms[0].target)
		if callErr == nil {
			e.stats.Hits++
		}
		return value, callErr
	}
	if rubyArmHits(&site.arms[1], object, &cell) {
		value, callErr := e.callLabelTarget(c, object, site.arms[1].target)
		if callErr == nil {
			e.stats.Hits++
		}
		return value, callErr
	}
	stale := -1
	for index, arm := range site.arms {
		if arm.target != 0 && arm.dispatch == object.dispatch && arm.shape == object.shape {
			if arm.epoch == cell.epoch {
				return 0, ErrInternal
			}
			stale = index
		}
	}
	receipt, ok := rubyResolve(&e.lookup.entries, e.lookup.topology, object.dispatch, selector)
	if !ok || !rubyReceiptsEqual(receipt, cell.receipt) {
		return 0, ErrInternal
	}
	target, ok := rubyTargetFor(object.dispatch, object.shape, selector, receipt)
	if !ok {
		return 0, ErrInternal
	}
	value, err := e.callLabelTarget(c, object, target)
	if err != nil {
		return 0, err
	}
	if err := c.poll(); err != nil {
		return 0, err
	}
	if !e.validateSite(site, selector) || e.lookup.cells[cellIndex] != cell {
		return 0, ErrInternal
	}
	current, ok := rubyResolve(&e.lookup.entries, e.lookup.topology, object.dispatch, selector)
	if !ok || !rubyReceiptsEqual(current, cell.receipt) {
		return 0, ErrInternal
	}
	if stale >= 0 {
		if stale != rubyCacheArm(selector, object.dispatch) {
			return 0, ErrInternal
		}
		e.publishArm(site, stale, object, cell, target)
		e.stats.StaleMisses++
		e.stats.Repairs++
		return value, nil
	}
	arm := rubyCacheArm(selector, object.dispatch)
	if arm < 0 {
		e.stats.UncachedFallbacks++
		return value, nil
	}
	if site.arms[arm] != (rubyArm{}) {
		return 0, ErrInternal
	}
	e.publishArm(site, arm, object, cell, target)
	e.stats.ColdAdmissions++
	e.stats.Admissions++
	e.stats.OccupiedArms++
	return value, nil
}

//go:noinline
func target37BenchmarkSpecializedSend(e *Engine, c *rubyControl, object *rubyObject) (int64, error) {
	return e.readLabel(c, object)
}

//go:noinline
func target37BenchmarkSpecializedPublic(e *Engine, c *rubyControl, object *rubyObject) (int64, error) {
	return e.readPublic(c, object)
}

//go:noinline
func target37BenchmarkEquivalentSend(e *target37EquivalentEngine, c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	return target37EquivalentSend(e, c, object)
}

//go:noinline
func target37BenchmarkEquivalentPublic(e *target37EquivalentEngine, c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	return target37EquivalentPublicCall(e, c, object)
}

func target37SetupGenerated(t testing.TB, lane target37GeneratedLane, public bool) (*Engine, rubyObject, rubyObject, Stats) {
	t.Helper()
	engine := NewEngine()
	alpha := rubyObject{id: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, value: 4}
	beta := rubyObject{id: 2, dispatch: rubyBetaDispatch, shape: rubyBetaShape, value: 4}
	control := rubyControl{ctx: context.Background(), steps: 4, frames: 3}
	call := func(object *rubyObject) (int64, error) {
		if lane == target37GenericLane {
			if public {
				return target37BenchmarkGenericPublic(engine, &control, object)
			}
			return target37BenchmarkGenericSend(engine, &control, object)
		}
		if public {
			return target37BenchmarkSpecializedPublic(engine, &control, object)
		}
		return target37BenchmarkSpecializedSend(engine, &control, object)
	}
	if value, err := call(&alpha); err != nil || value != 1 {
		t.Fatalf("warm Alpha = %d, %v", value, err)
	}
	if value, err := call(&beta); err != nil || value != 3 {
		t.Fatalf("warm Beta = %d, %v", value, err)
	}
	if control.steps != 0 || control.depth != 0 || control.nextFrame != 6 || alpha.trace != 6 || beta.trace != 8 {
		t.Fatalf("warm control/effects = steps %d depth %d frames %d traces %d/%d", control.steps, control.depth, control.nextFrame, alpha.trace, beta.trace)
	}
	wantStats := Stats{ColdAdmissions: 2, Admissions: 2, OccupiedArms: 2}
	if engine.stats != wantStats {
		t.Fatalf("warm stats = %#v, want %#v", engine.stats, wantStats)
	}
	wantArms := [2]rubyArm{
		{epoch: 1, dispatch: rubyAlphaDispatch, shape: rubyAlphaShape, slot: rubyBaseLabelSlot, definition: rubyBaseLabelInitialDefinition, target: rubyBaseLabelInitialTarget},
		{epoch: 3, dispatch: rubyBetaDispatch, shape: rubyBetaShape, slot: rubyBetaLabelSlot, definition: rubyBetaLabelDefinition, target: rubyBetaLabelTarget},
	}
	var gotArms [2]rubyArm
	if public {
		gotArms = engine.publicSite.arms
	} else {
		gotArms = engine.labelSite.arms
	}
	if gotArms != wantArms {
		t.Fatalf("warm arms = %#v, want %#v", gotArms, wantArms)
	}
	alpha.trace, beta.trace = 0, 0
	return engine, alpha, beta, wantStats
}

func target37SetupEquivalent(t testing.TB, public bool) (*target37EquivalentEngine, target37EquivalentObject, target37EquivalentObject, target37EquivalentStats) {
	t.Helper()
	engine, alpha, beta := target37NewEquivalentEngine()
	control := target37EquivalentControl{ctx: context.Background(), steps: 4, frames: 3}
	call := func(object *target37EquivalentObject) (int64, error) {
		if public {
			return target37BenchmarkEquivalentPublic(engine, &control, object)
		}
		return target37BenchmarkEquivalentSend(engine, &control, object)
	}
	if value, err := call(&alpha); err != nil || value != 1 {
		t.Fatalf("equivalent warm Alpha = %d, %v", value, err)
	}
	if value, err := call(&beta); err != nil || value != 3 {
		t.Fatalf("equivalent warm Beta = %d, %v", value, err)
	}
	if control.steps != 0 || control.depth != 0 || control.nextFrame != 6 || alpha.trace != 6 || beta.trace != 8 {
		t.Fatalf("equivalent warm control/effects = steps %d depth %d frames %d traces %d/%d", control.steps, control.depth, control.nextFrame, alpha.trace, beta.trace)
	}
	wantStats := target37EquivalentStats{coldAdmissions: 2, admissions: 2, occupiedArms: 2}
	if engine.stats != wantStats {
		t.Fatalf("equivalent warm stats = %#v, want %#v", engine.stats, wantStats)
	}
	wantArms := [2]target37EquivalentArm{
		{epoch: 1, dispatch: 1, shape: 1, slot: 3, definition: 3, target: 1},
		{epoch: 3, dispatch: 2, shape: 2, slot: 10, definition: 10, target: 2},
	}
	gotArms := engine.labelSite.arms
	if public {
		gotArms = engine.publicSite.arms
	}
	if gotArms != wantArms {
		t.Fatalf("equivalent warm arms = %#v, want %#v", gotArms, wantArms)
	}
	alpha.trace, beta.trace = 0, 0
	return engine, alpha, beta, wantStats
}

func target37CheckGeneratedVector(t testing.TB, engine *Engine, alpha, beta *rubyObject, control rubyControl, sum int64, before Stats) {
	t.Helper()
	want := before
	want.Hits += 8
	if sum != 16 || alpha.trace != 6666 || beta.trace != 8888 || control.steps != 0 || control.depth != 0 || control.nextFrame != 24 || engine.stats != want {
		t.Fatalf("generated vector sum=%d traces=%d/%d control=%#v stats=%#v want=%#v", sum, alpha.trace, beta.trace, control, engine.stats, want)
	}
}

func target37CheckEquivalentVector(t testing.TB, engine *target37EquivalentEngine, alpha, beta *target37EquivalentObject, control target37EquivalentControl, sum int64, before target37EquivalentStats) {
	t.Helper()
	want := before
	want.hits += 8
	if sum != 16 || alpha.trace != 6666 || beta.trace != 8888 || control.steps != 0 || control.depth != 0 || control.nextFrame != 24 || engine.stats != want {
		t.Fatalf("equivalent vector sum=%d traces=%d/%d control=%#v stats=%#v want=%#v", sum, alpha.trace, beta.trace, control, engine.stats, want)
	}
}

func TestTarget37BenchmarkVectorAndComparatorIsolation(t *testing.T) {
	for _, lane := range []target37GeneratedLane{target37GenericLane, target37SpecializedLane} {
		for _, public := range []bool{false, true} {
			engine, alpha, beta, before := target37SetupGenerated(t, lane, public)
			control := rubyControl{ctx: context.Background(), steps: 16, frames: 3}
			var sum int64
			for index := 0; index < 8; index++ {
				object := &alpha
				if index&1 != 0 {
					object = &beta
				}
				var value int64
				var err error
				if lane == target37GenericLane {
					if public {
						value, err = target37BenchmarkGenericPublic(engine, &control, object)
					} else {
						value, err = target37BenchmarkGenericSend(engine, &control, object)
					}
				} else if public {
					value, err = target37BenchmarkSpecializedPublic(engine, &control, object)
				} else {
					value, err = target37BenchmarkSpecializedSend(engine, &control, object)
				}
				if err != nil {
					t.Fatal(err)
				}
				sum += value
			}
			target37CheckGeneratedVector(t, engine, &alpha, &beta, control, sum, before)
		}
	}
	for _, public := range []bool{false, true} {
		engine, alpha, beta, before := target37SetupEquivalent(t, public)
		control := target37EquivalentControl{ctx: context.Background(), steps: 16, frames: 3}
		var sum int64
		for index := 0; index < 8; index++ {
			object := &alpha
			if index&1 != 0 {
				object = &beta
			}
			var value int64
			var err error
			if public {
				value, err = target37BenchmarkEquivalentPublic(engine, &control, object)
			} else {
				value, err = target37BenchmarkEquivalentSend(engine, &control, object)
			}
			if err != nil {
				t.Fatal(err)
			}
			sum += value
		}
		target37CheckEquivalentVector(t, engine, &alpha, &beta, control, sum, before)
	}

	source, err := os.ReadFile("target37_equivalent_test.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "target37_equivalent_test.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	generatedSource, err := os.ReadFile("ruby_generated.go")
	if err != nil {
		t.Fatal(err)
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
		{name: "receipt", generated: unsafe.Sizeof(rubyReceipt{}), equivalent: unsafe.Sizeof(target37EquivalentReceipt{}), wantGenerated: 16, wantEquivalent: 16},
		{name: "cell", generated: unsafe.Sizeof(rubyCell{}), equivalent: unsafe.Sizeof(target37EquivalentCell{}), wantGenerated: 24, wantEquivalent: 24},
		{name: "arm", generated: unsafe.Sizeof(rubyArm{}), equivalent: unsafe.Sizeof(target37EquivalentArm{}), wantGenerated: 32, wantEquivalent: 32},
		{name: "lookup", generated: unsafe.Sizeof(rubyLookup{}), equivalent: unsafe.Sizeof(target37EquivalentLookup{}), wantGenerated: 448, wantEquivalent: 248},
		{name: "site", generated: unsafe.Sizeof(rubySite{}), equivalent: unsafe.Sizeof(target37EquivalentSite{}), wantGenerated: 64, wantEquivalent: 64},
		{name: "object", generated: unsafe.Sizeof(rubyObject{}), equivalent: unsafe.Sizeof(target37EquivalentObject{}), wantGenerated: 32, wantEquivalent: 32},
		{name: "control", generated: unsafe.Sizeof(rubyControl{}), equivalent: unsafe.Sizeof(target37EquivalentControl{}), wantGenerated: 56, wantEquivalent: 56},
		{name: "stats", generated: unsafe.Sizeof(Stats{}), equivalent: unsafe.Sizeof(target37EquivalentStats{}), wantGenerated: 64, wantEquivalent: 64},
		{name: "engine", generated: unsafe.Sizeof(Engine{}), equivalent: unsafe.Sizeof(target37EquivalentEngine{}), wantGenerated: 824, wantEquivalent: 560},
	}
	for _, layout := range layouts {
		if layout.generated != layout.wantGenerated || layout.equivalent != layout.wantEquivalent {
			t.Errorf("%s layouts generated/equivalent = %d/%d, want %d/%d", layout.name, layout.generated, layout.equivalent, layout.wantGenerated, layout.wantEquivalent)
		}
	}

	wrappers := []any{
		target37BenchmarkGenericSend, target37BenchmarkGenericPublic,
		target37BenchmarkSpecializedSend, target37BenchmarkSpecializedPublic,
		target37BenchmarkEquivalentSend, target37BenchmarkEquivalentPublic,
	}
	seen := make(map[uintptr]bool, len(wrappers))
	for _, wrapper := range wrappers {
		address := reflect.ValueOf(wrapper).Pointer()
		if address == 0 || seen[address] {
			t.Fatalf("benchmark wrapper has zero or duplicate address %#x", address)
		}
		seen[address] = true
	}
}

func TestTarget37EquivalentComparatorExtendedInventory(t *testing.T) {
	t.Run("target4 stale repair and warm hit", func(t *testing.T) {
		engine, alpha, beta, initialStats := target37SetupEquivalent(t, false)
		engine.lookup.entries[0] = target37EquivalentEntry{slot: 3, definition: 13, kind: target37EquivalentDefined, visibility: target37EquivalentPublic}
		engine.lookup.nextEpoch = 7
		engine.lookup.cells[0] = target37EquivalentCell{
			epoch: 7,
			receipt: target37EquivalentReceipt{
				owner: 1, slot: 3, definition: 13,
				outcome: target37EquivalentResolved, visibility: target37EquivalentPublic,
			},
		}
		control := target37EquivalentControl{ctx: context.Background(), steps: 2, frames: 3}
		value, err := target37BenchmarkEquivalentSend(engine, &control, &alpha)
		if err != nil || value != 2 || alpha.trace != 7 || control.steps != 0 || control.depth != 0 || control.nextFrame != 3 {
			t.Fatalf("Alpha target4 repair = %d, %v; trace/control = %d/%#v", value, err, alpha.trace, control)
		}
		wantStats := initialStats
		wantStats.staleMisses, wantStats.repairs = 1, 1
		if engine.stats != wantStats || engine.labelSite.arms[0] != (target37EquivalentArm{epoch: 7, dispatch: 1, shape: 1, slot: 3, definition: 13, target: 4}) {
			t.Fatalf("Alpha target4 repair state = stats %#v arm %#v", engine.stats, engine.labelSite.arms[0])
		}
		alpha.trace = 0
		control = target37EquivalentControl{ctx: context.Background(), steps: 2, frames: 3}
		value, err = target37BenchmarkEquivalentSend(engine, &control, &alpha)
		wantStats.hits++
		if err != nil || value != 2 || alpha.trace != 7 || control.steps != 0 || control.depth != 0 || control.nextFrame != 3 || engine.stats != wantStats {
			t.Fatalf("Alpha target4 hit = %d, %v; trace/control/stats = %d/%#v/%#v", value, err, alpha.trace, control, engine.stats)
		}

		engine.lookup.entries[4] = target37EquivalentEntry{slot: 10, kind: target37EquivalentAbsent}
		engine.lookup.nextEpoch = 8
		engine.lookup.cells[2] = target37EquivalentCell{
			epoch: 8,
			receipt: target37EquivalentReceipt{
				owner: 1, slot: 3, definition: 13,
				outcome: target37EquivalentResolved, visibility: target37EquivalentPublic,
			},
		}
		control = target37EquivalentControl{ctx: context.Background(), steps: 2, frames: 3}
		value, err = target37BenchmarkEquivalentSend(engine, &control, &beta)
		wantStats.staleMisses++
		wantStats.repairs++
		if err != nil || value != 2 || beta.trace != 7 || control.steps != 0 || control.depth != 0 || control.nextFrame != 3 || engine.stats != wantStats ||
			engine.labelSite.arms[1] != (target37EquivalentArm{epoch: 8, dispatch: 2, shape: 2, slot: 3, definition: 13, target: 4}) {
			t.Fatalf("Beta target4 repair = %d, %v; trace/control/stats/arm = %d/%#v/%#v/%#v", value, err, beta.trace, control, engine.stats, engine.labelSite.arms[1])
		}

		beforeArms := engine.labelSite.arms
		gamma := target37EquivalentObject{id: 3, dispatch: 3, shape: 3, value: 4}
		control = target37EquivalentControl{ctx: context.Background(), steps: 2, frames: 3}
		value, err = target37BenchmarkEquivalentSend(engine, &control, &gamma)
		wantStats.uncachedFallbacks++
		if err != nil || value != 4 || gamma.trace != 9 || control.steps != 0 || control.depth != 0 || control.nextFrame != 3 || engine.stats != wantStats || engine.labelSite.arms != beforeArms {
			t.Fatalf("Gamma fallback = %d, %v; trace/control/stats/arms = %d/%#v/%#v/%#v", value, err, gamma.trace, control, engine.stats, engine.labelSite.arms)
		}
	})

	t.Run("visibility and whole-site corruption fail before effect", func(t *testing.T) {
		publicEngine, alpha, _, before := target37SetupEquivalent(t, true)
		publicEngine.lookup.entries[0].visibility = target37EquivalentProtected
		publicEngine.lookup.cells[0].receipt.visibility = target37EquivalentProtected
		beforeArms := publicEngine.publicSite.arms
		control := target37EquivalentControl{ctx: context.Background(), steps: 1, frames: 3}
		value, err := target37BenchmarkEquivalentPublic(publicEngine, &control, &alpha)
		if err != nil || value != 5 || alpha.trace != 0 || control.steps != 0 || control.depth != 0 || control.nextFrame != 1 || publicEngine.stats != before || publicEngine.publicSite.arms != beforeArms {
			t.Fatalf("public rejection = %d, %v; trace/control/stats/arms = %d/%#v/%#v/%#v", value, err, alpha.trace, control, publicEngine.stats, publicEngine.publicSite.arms)
		}

		engine, selected, _, cleanStats := target37SetupEquivalent(t, false)
		engine.labelSite.arms[1].target = 3
		control = target37EquivalentControl{ctx: context.Background(), steps: 1, frames: 3}
		value, err = target37BenchmarkEquivalentSend(engine, &control, &selected)
		if err != target37EquivalentInternal || value != 0 || selected.trace != 0 || control.steps != 0 || control.depth != 0 || control.nextFrame != 1 || engine.stats != cleanStats {
			t.Fatalf("unused-arm corruption = %d, %v; trace/control/stats = %d/%#v/%#v", value, err, selected.trace, control, engine.stats)
		}
	})

	t.Run("undef receipt retains slot", func(t *testing.T) {
		engine, _, _ := target37NewEquivalentEngine()
		engine.lookup.entries[6] = target37EquivalentEntry{slot: 12, kind: target37EquivalentUndefined}
		receipt, ok := engine.resolve(3, false)
		want := target37EquivalentReceipt{owner: 4, slot: 12, outcome: target37EquivalentUndef}
		if !ok || receipt != want {
			t.Fatalf("undef receipt = %#v, %v; want %#v", receipt, ok, want)
		}
	})
}

func benchmarkTarget37GenericSend(b *testing.B) {
	engine, alpha, beta, before := target37SetupGenerated(b, target37GenericLane, false)
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl rubyControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		alpha.trace, beta.trace = 0, 0
		control := rubyControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for index := 0; index < 8; index++ {
			object := &alpha
			if index&1 != 0 {
				object = &beta
			}
			value, err := target37BenchmarkGenericSend(engine, &control, object)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + alpha.trace + beta.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	target37FinishGeneratedBenchmark(b, engine, &alpha, &beta, before, lastControl, lastSum, checksum, failed)
}

func benchmarkTarget37GenericPublic(b *testing.B) {
	engine, alpha, beta, before := target37SetupGenerated(b, target37GenericLane, true)
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl rubyControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		alpha.trace, beta.trace = 0, 0
		control := rubyControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for index := 0; index < 8; index++ {
			object := &alpha
			if index&1 != 0 {
				object = &beta
			}
			value, err := target37BenchmarkGenericPublic(engine, &control, object)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + alpha.trace + beta.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	target37FinishGeneratedBenchmark(b, engine, &alpha, &beta, before, lastControl, lastSum, checksum, failed)
}

func benchmarkTarget37SpecializedSend(b *testing.B) {
	engine, alpha, beta, before := target37SetupGenerated(b, target37SpecializedLane, false)
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl rubyControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		alpha.trace, beta.trace = 0, 0
		control := rubyControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for index := 0; index < 8; index++ {
			object := &alpha
			if index&1 != 0 {
				object = &beta
			}
			value, err := target37BenchmarkSpecializedSend(engine, &control, object)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + alpha.trace + beta.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	target37FinishGeneratedBenchmark(b, engine, &alpha, &beta, before, lastControl, lastSum, checksum, failed)
}

func benchmarkTarget37SpecializedPublic(b *testing.B) {
	engine, alpha, beta, before := target37SetupGenerated(b, target37SpecializedLane, true)
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl rubyControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		alpha.trace, beta.trace = 0, 0
		control := rubyControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for index := 0; index < 8; index++ {
			object := &alpha
			if index&1 != 0 {
				object = &beta
			}
			value, err := target37BenchmarkSpecializedPublic(engine, &control, object)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + alpha.trace + beta.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	target37FinishGeneratedBenchmark(b, engine, &alpha, &beta, before, lastControl, lastSum, checksum, failed)
}

func target37FinishGeneratedBenchmark(b *testing.B, engine *Engine, alpha, beta *rubyObject, before Stats, control rubyControl, sum, checksum int64, failed bool) {
	b.Helper()
	want := before
	want.Hits += uint64(8 * b.N)
	wantChecksum := int64(b.N) * (16 + 6666 + 8888 + 24)
	if failed || sum != 16 || alpha.trace != 6666 || beta.trace != 8888 || control.steps != 0 || control.depth != 0 || control.nextFrame != 24 || engine.stats != want || checksum != wantChecksum {
		b.Fatalf("generated benchmark failed=%v sum=%d traces=%d/%d control=%#v stats=%#v want=%#v checksum=%d wantChecksum=%d", failed, sum, alpha.trace, beta.trace, control, engine.stats, want, checksum, wantChecksum)
	}
	target37BenchmarkSink = checksum
}

func benchmarkTarget37EquivalentSend(b *testing.B) {
	engine, alpha, beta, before := target37SetupEquivalent(b, false)
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl target37EquivalentControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		alpha.trace, beta.trace = 0, 0
		control := target37EquivalentControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for index := 0; index < 8; index++ {
			object := &alpha
			if index&1 != 0 {
				object = &beta
			}
			value, err := target37BenchmarkEquivalentSend(engine, &control, object)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + alpha.trace + beta.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	target37FinishEquivalentBenchmark(b, engine, &alpha, &beta, before, lastControl, lastSum, checksum, failed)
}

func benchmarkTarget37EquivalentPublic(b *testing.B) {
	engine, alpha, beta, before := target37SetupEquivalent(b, true)
	ctx := context.Background()
	var checksum, lastSum int64
	var lastControl target37EquivalentControl
	failed := false
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		alpha.trace, beta.trace = 0, 0
		control := target37EquivalentControl{ctx: ctx, steps: 16, frames: 3}
		var sum int64
		for index := 0; index < 8; index++ {
			object := &alpha
			if index&1 != 0 {
				object = &beta
			}
			value, err := target37BenchmarkEquivalentPublic(engine, &control, object)
			if err != nil {
				failed = true
			}
			sum += value
		}
		checksum += sum + alpha.trace + beta.trace + int64(control.nextFrame)
		lastSum, lastControl = sum, control
	}
	b.StopTimer()
	target37FinishEquivalentBenchmark(b, engine, &alpha, &beta, before, lastControl, lastSum, checksum, failed)
}

func target37FinishEquivalentBenchmark(b *testing.B, engine *target37EquivalentEngine, alpha, beta *target37EquivalentObject, before target37EquivalentStats, control target37EquivalentControl, sum, checksum int64, failed bool) {
	b.Helper()
	want := before
	want.hits += uint64(8 * b.N)
	wantChecksum := int64(b.N) * (16 + 6666 + 8888 + 24)
	if failed || sum != 16 || alpha.trace != 6666 || beta.trace != 8888 || control.steps != 0 || control.depth != 0 || control.nextFrame != 24 || engine.stats != want || checksum != wantChecksum {
		b.Fatalf("equivalent benchmark failed=%v sum=%d traces=%d/%d control=%#v stats=%#v want=%#v checksum=%d wantChecksum=%d", failed, sum, alpha.trace, beta.trace, control, engine.stats, want, checksum, wantChecksum)
	}
	target37BenchmarkSink = checksum
}

func BenchmarkTarget37GenericSend(b *testing.B)       { benchmarkTarget37GenericSend(b) }
func BenchmarkTarget37SpecializedSend(b *testing.B)   { benchmarkTarget37SpecializedSend(b) }
func BenchmarkTarget37EquivalentSend(b *testing.B)    { benchmarkTarget37EquivalentSend(b) }
func BenchmarkTarget37GenericPublic(b *testing.B)     { benchmarkTarget37GenericPublic(b) }
func BenchmarkTarget37SpecializedPublic(b *testing.B) { benchmarkTarget37SpecializedPublic(b) }
func BenchmarkTarget37EquivalentPublic(b *testing.B)  { benchmarkTarget37EquivalentPublic(b) }

type target37CaptureLane uint8

const (
	target37CaptureGeneric target37CaptureLane = iota + 1
	target37CaptureSpecialized
	target37CaptureEquivalent
)

var target37CaptureAOrders = [7][3]target37CaptureLane{
	{target37CaptureGeneric, target37CaptureSpecialized, target37CaptureEquivalent},
	{target37CaptureSpecialized, target37CaptureEquivalent, target37CaptureGeneric},
	{target37CaptureEquivalent, target37CaptureGeneric, target37CaptureSpecialized},
	{target37CaptureGeneric, target37CaptureSpecialized, target37CaptureEquivalent},
	{target37CaptureSpecialized, target37CaptureEquivalent, target37CaptureGeneric},
	{target37CaptureEquivalent, target37CaptureGeneric, target37CaptureSpecialized},
	{target37CaptureGeneric, target37CaptureSpecialized, target37CaptureEquivalent},
}

var target37CaptureBOrders = [7][3]target37CaptureLane{
	{target37CaptureEquivalent, target37CaptureSpecialized, target37CaptureGeneric},
	{target37CaptureSpecialized, target37CaptureGeneric, target37CaptureEquivalent},
	{target37CaptureGeneric, target37CaptureEquivalent, target37CaptureSpecialized},
	{target37CaptureEquivalent, target37CaptureSpecialized, target37CaptureGeneric},
	{target37CaptureSpecialized, target37CaptureGeneric, target37CaptureEquivalent},
	{target37CaptureGeneric, target37CaptureEquivalent, target37CaptureSpecialized},
	{target37CaptureEquivalent, target37CaptureGeneric, target37CaptureSpecialized},
}

func BenchmarkTarget37CaptureA(b *testing.B) {
	target37RunCapture(b, "A", target37CaptureAOrders)
}

func BenchmarkTarget37CaptureB(b *testing.B) {
	target37RunCapture(b, "B", target37CaptureBOrders)
}

func target37RunCapture(b *testing.B, capture string, orders [7][3]target37CaptureLane) {
	b.Helper()
	if os.Getenv("EMBER_RUBY_TARGET37_CAPTURE") != capture {
		b.Skipf("set EMBER_RUBY_TARGET37_CAPTURE=%s to run retained capture", capture)
	}
	if runtime.GOMAXPROCS(0) != 1 {
		b.Fatalf("Target 37 capture requires GOMAXPROCS=1, got %d", runtime.GOMAXPROCS(0))
	}
	identity, err := target37CaptureIdentity(capture)
	if err != nil {
		b.Fatal(err)
	}
	b.Log(identity)
	for _, mode := range []struct {
		name   string
		public bool
	}{
		{name: "Send"},
		{name: "Public", public: true},
	} {
		b.Run(mode.name, func(b *testing.B) {
			for round, order := range orders {
				b.Run(fmt.Sprintf("Round%02d", round+1), func(b *testing.B) {
					for _, lane := range order {
						target37RunCaptureLane(b, lane, mode.public)
					}
				})
			}
		})
	}
}

func target37RunCaptureLane(b *testing.B, lane target37CaptureLane, public bool) {
	b.Helper()
	switch {
	case lane == target37CaptureGeneric && !public:
		b.Run("Generic", benchmarkTarget37GenericSend)
	case lane == target37CaptureSpecialized && !public:
		b.Run("Specialized", benchmarkTarget37SpecializedSend)
	case lane == target37CaptureEquivalent && !public:
		b.Run("Equivalent", benchmarkTarget37EquivalentSend)
	case lane == target37CaptureGeneric:
		b.Run("Generic", benchmarkTarget37GenericPublic)
	case lane == target37CaptureSpecialized:
		b.Run("Specialized", benchmarkTarget37SpecializedPublic)
	case lane == target37CaptureEquivalent:
		b.Run("Equivalent", benchmarkTarget37EquivalentPublic)
	default:
		b.Fatalf("unknown Target 37 capture lane %d", lane)
	}
}
