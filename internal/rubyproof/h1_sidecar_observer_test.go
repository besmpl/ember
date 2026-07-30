package rubyproof

import (
	"crypto/sha256"
	"errors"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type h1SidecarOracleKind uint8

const (
	h1SidecarOracleObject h1SidecarOracleKind = iota + 1
	h1SidecarOracleEnvironment
)

type h1SidecarOracleNode struct {
	kind h1SidecarOracleKind
	ref  uint32
}

type h1SidecarOracleGraph struct {
	nodes []h1SidecarOracleNode
	edges map[h1SidecarOracleNode][]h1SidecarOracleNode
}

func (graph *h1SidecarOracleGraph) addNode(node h1SidecarOracleNode) {
	graph.nodes = append(graph.nodes, node)
}

func (graph *h1SidecarOracleGraph) addEdge(from, to h1SidecarOracleNode) {
	graph.edges[from] = append(graph.edges[from], to)
}

// reachable is an independent breadth-first oracle over the guest graph
// declared by this test. It does not inspect sidecar slots, marks, or worklists.
func (graph h1SidecarOracleGraph) reachable(roots []h1SidecarOracleNode) map[h1SidecarOracleNode]bool {
	reachable := make(map[h1SidecarOracleNode]bool, len(graph.nodes))
	queue := append([]h1SidecarOracleNode(nil), roots...)
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		if reachable[current] {
			continue
		}
		reachable[current] = true
		queue = append(queue, graph.edges[current]...)
	}
	return reachable
}

type h1SidecarOracleRoot struct {
	node  h1SidecarOracleNode
	value h1SidecarValue
}

type h1SidecarOracleFixture struct {
	arena        *h1SidecarArena
	objects      [h1SidecarObjectCapacity]h1SidecarObjectRef
	environments [h1SidecarEnvironmentCapacity]h1SidecarEnvironmentRef
	roots        []h1SidecarOracleRoot
	graph        h1SidecarOracleGraph
}

func newH1SidecarOracleFixture(t *testing.T) h1SidecarOracleFixture {
	t.Helper()
	fixture := h1SidecarOracleFixture{
		arena: newH1SidecarArena(), graph: h1SidecarOracleGraph{edges: make(map[h1SidecarOracleNode][]h1SidecarOracleNode)},
	}
	for index := range fixture.objects {
		object, err := fixture.arena.allocateObject(dispatchClassID(index+1), shapeID(index+1), int64(index+10))
		if err != nil {
			t.Fatalf("allocate object %d: %v", index, err)
		}
		fixture.objects[index] = object
		fixture.graph.addNode(h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(object)})
	}
	for index := 0; index < h1SidecarEnvironmentCapacity; index++ {
		parent := h1SidecarEnvironmentRef(0)
		if index != 0 {
			parent = fixture.environments[index-1]
		}
		transaction, err := fixture.arena.beginSingletonDefinition(
			fixture.objects[index], h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic,
			h1SidecarIntegerValue(int64(index+100)), parent,
		)
		if err != nil {
			t.Fatalf("begin sidecar %d: %v", index, err)
		}
		if err := fixture.arena.commitSingletonDefinition(transaction, false); err != nil {
			t.Fatalf("commit sidecar %d: %v", index, err)
		}
		data, err := fixture.arena.resolveCallData(fixture.objects[index], h1SidecarSelector)
		if err != nil {
			t.Fatalf("resolve sidecar %d: %v", index, err)
		}
		fixture.environments[index] = data.environment
		objectNode := h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(fixture.objects[index])}
		environmentNode := h1SidecarOracleNode{kind: h1SidecarOracleEnvironment, ref: uint32(data.environment)}
		fixture.graph.addNode(environmentNode)
		fixture.graph.addEdge(objectNode, environmentNode)
		if parent != 0 {
			fixture.graph.addEdge(environmentNode, h1SidecarOracleNode{kind: h1SidecarOracleEnvironment, ref: uint32(parent)})
		}
	}
	for index, environment := range fixture.environments {
		target := fixture.objects[(index+1)%h1SidecarEnvironmentCapacity]
		if err := fixture.arena.storeEnvironment(environment, 0, h1SidecarObjectValue(target)); err != nil {
			t.Fatalf("store cycle edge %d: %v", index, err)
		}
		fixture.graph.addEdge(
			h1SidecarOracleNode{kind: h1SidecarOracleEnvironment, ref: uint32(environment)},
			h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(target)},
		)
	}
	if err := fixture.arena.storeObjectField(fixture.objects[2], 0, h1SidecarObjectValue(fixture.objects[3])); err != nil {
		t.Fatal(err)
	}
	if err := fixture.arena.storeObjectField(fixture.objects[3], 0, h1SidecarEnvironmentValue(fixture.environments[0])); err != nil {
		t.Fatal(err)
	}
	fixture.graph.addEdge(
		h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(fixture.objects[2])},
		h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(fixture.objects[3])},
	)
	fixture.graph.addEdge(
		h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(fixture.objects[3])},
		h1SidecarOracleNode{kind: h1SidecarOracleEnvironment, ref: uint32(fixture.environments[0])},
	)
	for _, object := range fixture.objects {
		fixture.roots = append(fixture.roots, h1SidecarOracleRoot{
			node: h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(object)}, value: h1SidecarObjectValue(object),
		})
	}
	for _, environment := range fixture.environments {
		fixture.roots = append(fixture.roots, h1SidecarOracleRoot{
			node: h1SidecarOracleNode{kind: h1SidecarOracleEnvironment, ref: uint32(environment)}, value: h1SidecarEnvironmentValue(environment),
		})
	}
	return fixture
}

func TestH1SidecarIndependentOracleAllRootMasks(t *testing.T) {
	const candidates = h1SidecarObjectCapacity + h1SidecarEnvironmentCapacity
	for mask := 0; mask < 1<<candidates; mask++ {
		t.Run(strconv.Itoa(mask), func(t *testing.T) {
			fixture := newH1SidecarOracleFixture(t)
			roots := make([]h1SidecarOracleNode, 0, candidates)
			for index, root := range fixture.roots {
				if mask&(1<<index) == 0 {
					continue
				}
				if err := fixture.arena.pushRoot(root.value); err != nil {
					t.Fatal(err)
				}
				roots = append(roots, root.node)
			}
			want := fixture.graph.reachable(roots)
			if err := fixture.arena.collect(false); err != nil {
				t.Fatal(err)
			}
			assertH1SidecarOracle(t, fixture.arena, fixture.graph.nodes, want, len(roots))

			for _, object := range fixture.objects {
				node := h1SidecarOracleNode{kind: h1SidecarOracleObject, ref: uint32(object)}
				if want[node] {
					continue
				}
				oldIndex, oldGeneration, _ := h1SidecarUnpackReference(uint32(object))
				reused, err := fixture.arena.allocateObject(20, 20, 20)
				if err != nil {
					t.Fatal(err)
				}
				index, generation, ok := h1SidecarUnpackReference(uint32(reused))
				if !ok || index != oldIndex || generation != oldGeneration+1 {
					t.Fatalf("object reuse = (%d,%d,%v), want (%d,%d,true)", index, generation, ok, oldIndex, oldGeneration+1)
				}
			}
		})
	}
}

func assertH1SidecarOracle(t *testing.T, arena *h1SidecarArena, nodes []h1SidecarOracleNode, want map[h1SidecarOracleNode]bool, rootCount int) {
	t.Helper()
	live := map[h1SidecarOracleKind]uint8{}
	initial := map[h1SidecarOracleKind]uint8{}
	for _, node := range nodes {
		initial[node.kind]++
		var err error
		switch node.kind {
		case h1SidecarOracleObject:
			_, err = arena.resolveObject(h1SidecarObjectRef(node.ref))
		case h1SidecarOracleEnvironment:
			_, err = arena.resolveEnvironment(h1SidecarEnvironmentRef(node.ref))
		}
		if want[node] {
			if err != nil {
				t.Errorf("reachable %v rejected: %v", node, err)
			}
			live[node.kind]++
		} else if !errors.Is(err, errH1SidecarInvalidRef) {
			t.Errorf("reclaimed %v resolve = %v, want invalid ref", node, err)
		}
	}
	stats, work := arena.stats(), arena.collectionWork()
	if stats.LiveObjects != live[h1SidecarOracleObject] || stats.LiveEnvironments != live[h1SidecarOracleEnvironment] ||
		stats.ObjectReclaims != uint64(initial[h1SidecarOracleObject]-live[h1SidecarOracleObject]) ||
		stats.EnvironmentReclaims != uint64(initial[h1SidecarOracleEnvironment]-live[h1SidecarOracleEnvironment]) {
		t.Errorf("stats %#v differ from oracle object/env live %d/%d", stats, live[h1SidecarOracleObject], live[h1SidecarOracleEnvironment])
	}
	if stats.Collections != 1 || stats.LastMarkWork != uint8(len(want)) || stats.LastMarkWork > h1SidecarMarkCapacity ||
		stats.MarkHighWater > h1SidecarMarkCapacity {
		t.Errorf("mark stats = collections %d work %d high-water %d; want 1/%d and <=%d",
			stats.Collections, stats.LastMarkWork, stats.MarkHighWater, len(want), h1SidecarMarkCapacity)
	}
	if work.RootValues != uint8(rootCount) || work.MarkedObjects != live[h1SidecarOracleObject] ||
		work.MarkedEnvironments != live[h1SidecarOracleEnvironment] || work.ObjectScans != h1SidecarObjectCapacity ||
		work.EnvironmentScans != h1SidecarEnvironmentCapacity ||
		work.ReclaimedObjects != initial[h1SidecarOracleObject]-live[h1SidecarOracleObject] ||
		work.ReclaimedEnvs != initial[h1SidecarOracleEnvironment]-live[h1SidecarOracleEnvironment] {
		t.Errorf("work %#v differs from exact oracle", work)
	}
}

func TestH1SidecarDeterministicTypedReuse(t *testing.T) {
	fixture := newH1SidecarOracleFixture(t)
	oldEnvironments := fixture.environments
	if err := fixture.arena.collect(false); err != nil {
		t.Fatal(err)
	}
	for ordinal := 0; ordinal < h1SidecarEnvironmentCapacity; ordinal++ {
		object, err := fixture.arena.allocateObject(dispatchClassID(30+ordinal), shapeID(30+ordinal), 30)
		if err != nil {
			t.Fatal(err)
		}
		objectIndex, objectGeneration, ok := h1SidecarUnpackReference(uint32(object))
		if !ok || objectIndex != uint8(ordinal+1) || objectGeneration != 2 {
			t.Fatalf("object %d reuse = %d/%d/%v", ordinal, objectIndex, objectGeneration, ok)
		}
		transaction, err := fixture.arena.beginSingletonDefinition(
			object, h1SidecarSelector, h1SidecarTarget, lookupVisibilityPublic, h1SidecarIntegerValue(20), 0,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.arena.commitSingletonDefinition(transaction, false); err != nil {
			t.Fatal(err)
		}
		data, err := fixture.arena.resolveCallData(object, h1SidecarSelector)
		if err != nil {
			t.Fatal(err)
		}
		oldIndex, oldGeneration, _ := h1SidecarUnpackReference(uint32(oldEnvironments[ordinal]))
		index, generation, valid := h1SidecarUnpackReference(uint32(data.environment))
		if !valid || index != oldIndex || generation != oldGeneration+1 {
			t.Fatalf("environment %d reuse = %d/%d/%v, want %d/%d", ordinal, index, generation, valid, oldIndex, oldGeneration+1)
		}
	}
}

func TestH1SidecarFixedEconomyAgainstTarget44(t *testing.T) {
	if h1SidecarObjectCapacity != 4 || h1SidecarEnvironmentCapacity != 2 || h1SidecarRootCapacity != 8 ||
		h1SidecarObjectFields != 2 || h1SidecarEnvironmentCells != 1 || h1SidecarMarkCapacity != 6 {
		t.Fatalf("sidecar capacity drift: %d/%d/%d/%d/%d/%d", h1SidecarObjectCapacity, h1SidecarEnvironmentCapacity,
			h1SidecarRootCapacity, h1SidecarObjectFields, h1SidecarEnvironmentCells, h1SidecarMarkCapacity)
	}
	candidateOwner := reflect.TypeOf(h1SidecarArena{}).Size()
	baselineOwner := reflect.TypeOf(h1Arena{}).Size()
	candidatePayload := uintptr(h1SidecarObjectCapacity) * reflect.TypeOf(h1SidecarObjectSlot{}).Size()
	baselinePayload := uintptr(h1ObjectCapacity)*reflect.TypeOf(h1ObjectSlot{}).Size() +
		uintptr(h1EigenCapacity)*reflect.TypeOf(h1EigenSlot{}).Size()
	checks := []struct {
		name string
		got  uintptr
		max  uintptr
	}{
		{"reference", reflect.TypeOf(h1SidecarObjectRef(0)).Size(), 4},
		{"value", reflect.TypeOf(h1SidecarValue{}).Size(), 16},
		{"row", reflect.TypeOf(h1SidecarRow{}).Size(), 4},
		{"object slot", reflect.TypeOf(h1SidecarObjectSlot{}).Size(), 72},
		{"environment slot", reflect.TypeOf(h1SidecarEnvironmentSlot{}).Size(), 32},
		{"mark item", reflect.TypeOf(h1SidecarMarkItem{}).Size(), 8},
		{"call data", reflect.TypeOf(h1SidecarCallData{}).Size(), 12},
		{"complete owner", candidateOwner, 792},
	}
	for _, check := range checks {
		t.Logf("H1-prime %s: %d bytes (ceiling %d)", check.name, check.got, check.max)
		if check.got > check.max {
			t.Errorf("%s = %d, exceeds %d", check.name, check.got, check.max)
		}
	}
	t.Logf("H1-prime versus Target44 (64-bit): owner %d versus %d bytes; object+sidecar payload %d versus object+eigen payload %d bytes; max mark work %d versus %d",
		candidateOwner, baselineOwner, candidatePayload, baselinePayload, h1SidecarMarkCapacity, h1MarkCapacity)
	t.Logf("H1-prime versus Target44 structure: 2 versus 3 typed pools/free lists; 2 versus 3 mark kinds")
	if candidateOwner > baselineOwner {
		t.Errorf("H1-prime owner = %d bytes, Target44 = %d", candidateOwner, baselineOwner)
	}
}

func TestH1SidecarStaticRowAndTwoKindInventory(t *testing.T) {
	if h1SidecarSelector != selectorID(1) || h1SidecarTarget != h1SidecarTargetID(1) {
		t.Fatalf("static admission selector/target = %d/%d, want 1/1", h1SidecarSelector, h1SidecarTarget)
	}
	row := reflect.TypeOf(h1SidecarRow{})
	if row.NumField() != 1 || row.Field(0).Name != "environment" || row.Field(0).Type != reflect.TypeOf(h1SidecarEnvironmentRef(0)) {
		t.Fatalf("stored row fields = %v, want only generational environment ref", reflectedFieldNames(row))
	}
	object := reflect.TypeOf(h1SidecarObjectSlot{})
	forbiddenFields := map[string]bool{
		"selector": true, "target": true, "visibility": true, "installation": true, "epoch": true,
		"fallback": true, "generation": true, "retired": true,
	}
	// generation and retired belong to the owning object slot, never the row.
	for index := 0; index < row.NumField(); index++ {
		if forbiddenFields[row.Field(index).Name] {
			t.Errorf("stored row retains dynamic field %q", row.Field(index).Name)
		}
	}
	if field, ok := object.FieldByName("sidecar"); !ok || field.Type != row {
		t.Errorf("object sidecar field = %v/%v, want embedded static row", field.Type, ok)
	}
	if field, ok := object.FieldByName("sidecarPresent"); !ok {
		t.Error("object has no publish-last sidecar presence field")
	} else if field.Type.Kind() != reflect.Bool {
		t.Errorf("object sidecar presence field = %v/%v, want publish-last bool", field.Type, ok)
	}

	arena := reflect.TypeOf(h1SidecarArena{})
	poolFields, freeListFields := 0, 0
	objectSlot, environmentSlot := reflect.TypeOf(h1SidecarObjectSlot{}), reflect.TypeOf(h1SidecarEnvironmentSlot{})
	for index := 0; index < arena.NumField(); index++ {
		field := arena.Field(index)
		if field.Type.Kind() == reflect.Array && (field.Type.Elem() == objectSlot || field.Type.Elem() == environmentSlot) {
			if field.Name != "objects" && field.Name != "environments" {
				t.Errorf("unexpected typed pool %q", field.Name)
			}
			poolFields++
		}
		if strings.HasSuffix(strings.ToLower(field.Name), "free") {
			if field.Name != "objectFree" && field.Name != "environmentFree" {
				t.Errorf("unexpected free list %q", field.Name)
			}
			freeListFields++
		}
	}
	if poolFields != 2 || freeListFields != 2 {
		t.Errorf("pool/free-list fields = %d/%d, want 2/2", poolFields, freeListFields)
	}
	if h1SidecarMarkObject != 1 || h1SidecarMarkEnvironment != 2 {
		t.Fatalf("mark kinds = %d/%d, want exactly object=1/environment=2", h1SidecarMarkObject, h1SidecarMarkEnvironment)
	}
	t.Log("stored row: environment ref only; 2 fixed pools, 2 free lists, 2 mark kinds; no independent row identity")
}

func reflectedFieldNames(record reflect.Type) []string {
	names := make([]string, record.NumField())
	for index := range names {
		names[index] = record.Field(index).Name
	}
	return names
}

func TestH1SidecarAdmittedCycleAllocatesNoGoHeapAfterConstruction(t *testing.T) {
	arena := newH1SidecarArena()
	var runErr error
	allocations := testing.AllocsPerRun(100, func() {
		if runErr != nil {
			return
		}
		object, err := arena.allocateObject(1, 1, 10)
		if err != nil {
			runErr = err
			return
		}
		transaction, err := arena.beginSingletonDefinition(object, 1, 1, lookupVisibilityPublic, h1SidecarObjectValue(object), 0)
		if err != nil {
			runErr = err
			return
		}
		if err = arena.commitSingletonDefinition(transaction, false); err != nil {
			runErr = err
			return
		}
		marker := arena.rootMarker()
		if err = arena.pushRoot(h1SidecarObjectValue(object)); err != nil {
			runErr = err
			return
		}
		if err = arena.collect(false); err != nil {
			runErr = err
			return
		}
		if err = arena.popRoots(marker); err != nil {
			runErr = err
			return
		}
		runErr = arena.collect(false)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if allocations != 0 {
		t.Fatalf("H1-prime admitted cycle allocations = %g, want 0", allocations)
	}
	t.Logf("H1-prime admitted cycle Go allocations after construction: %.0f", allocations)
}

func TestH1SidecarProductionShapeAndArtifact(t *testing.T) {
	contents, err := os.ReadFile("h1_sidecar.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "h1_sidecar.go", contents, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		switch path {
		case "C", "unsafe", "reflect", "runtime", "sync", "sync/atomic":
			t.Errorf("forbidden candidate import %q", path)
		}
	}
	markKinds := make(map[string]bool)
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if ast.IsExported(declaration.Name.Name) || strings.Contains(strings.ToLower(declaration.Name.Name), "eigen") {
				t.Errorf("forbidden candidate function %q", declaration.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(spec.Name.Name) || strings.Contains(strings.ToLower(spec.Name.Name), "eigen") {
						t.Errorf("forbidden candidate type %q", spec.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if strings.HasPrefix(name.Name, "h1SidecarMark") && name.Name != "h1SidecarMarkCapacity" {
							markKinds[name.Name] = true
						}
						if ast.IsExported(name.Name) || strings.Contains(strings.ToLower(name.Name), "eigen") {
							t.Errorf("forbidden candidate value %q", name.Name)
						}
					}
				}
			}
		}
	}
	if len(markKinds) != 2 || !markKinds["h1SidecarMarkObject"] || !markKinds["h1SidecarMarkEnvironment"] {
		t.Errorf("mark-kind constants = %v, want object/environment only", markKinds)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.GoStmt, *ast.ChanType, *ast.MapType:
			t.Errorf("forbidden candidate mechanism %T", node)
		}
		return true
	})

	generated, err := os.ReadFile("generated/ruby_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := sha256.Sum256(generated); got != [sha256.Size]byte{0xb3, 0x0d, 0xb6, 0x31, 0xa5, 0x71, 0x27, 0xbd, 0x5f, 0x42, 0x04, 0x27, 0x26, 0x2c, 0xff, 0xf3, 0x97, 0x0c, 0x14, 0xf6, 0xcf, 0x5c, 0x91, 0xcd, 0x38, 0x2f, 0xd3, 0xb0, 0x8d, 0x61, 0xbf, 0x46} {
		t.Fatalf("generated artifact SHA-256 = %x", got)
	}
}
