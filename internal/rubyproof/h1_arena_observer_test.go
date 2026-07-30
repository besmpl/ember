package rubyproof

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"reflect"
	"strconv"
	"testing"
)

type h1OracleKind uint8

const (
	h1OracleObject h1OracleKind = iota + 1
	h1OracleEigen
	h1OracleEnvironment
)

type h1OracleNode struct {
	kind h1OracleKind
	ref  uint32
}

type h1OracleGraph struct {
	nodes []h1OracleNode
	edges map[h1OracleNode][]h1OracleNode
}

func (graph *h1OracleGraph) addNode(node h1OracleNode) {
	graph.nodes = append(graph.nodes, node)
}

func (graph *h1OracleGraph) addEdge(from, to h1OracleNode) {
	graph.edges[from] = append(graph.edges[from], to)
}

// reachable is deliberately a test-owned breadth-first graph oracle. It knows
// nothing about arena slots, mark bits, free lists, or traversal order.
func (graph h1OracleGraph) reachable(roots []h1OracleNode) map[h1OracleNode]bool {
	reachable := make(map[h1OracleNode]bool, len(graph.nodes))
	queue := append([]h1OracleNode(nil), roots...)
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

type h1OracleFixture struct {
	arena        *h1Arena
	objects      [h1ObjectCapacity]h1ObjectRef
	environments [h1EnvironmentCapacity]h1EnvironmentRef
	rootValues   []h1OracleRoot
	graph        h1OracleGraph
}

type h1OracleRoot struct {
	node  h1OracleNode
	value h1Value
}

func newH1OracleFixture(t *testing.T) h1OracleFixture {
	t.Helper()
	fixture := h1OracleFixture{arena: newH1Arena(), graph: h1OracleGraph{edges: make(map[h1OracleNode][]h1OracleNode)}}
	for index := range fixture.objects {
		object, err := fixture.arena.allocateObject(dispatchClassID(index+1), shapeID(index+1), int64(index+10))
		if err != nil {
			t.Fatalf("allocate object %d: %v", index, err)
		}
		fixture.objects[index] = object
		fixture.graph.addNode(h1OracleNode{kind: h1OracleObject, ref: uint32(object)})
	}
	for index := 0; index < h1EigenCapacity; index++ {
		parent := h1EnvironmentRef(0)
		if index != 0 {
			parent = fixture.environments[index-1]
		}
		transaction, err := fixture.arena.beginSingletonDefinition(
			fixture.objects[index], selectorID(index+1), h1TargetID(index+1), lookupVisibilityPublic,
			h1IntegerValue(int64(100+index)), parent,
		)
		if err != nil {
			t.Fatalf("begin singleton %d: %v", index, err)
		}
		if err := fixture.arena.commitSingletonDefinition(transaction, false); err != nil {
			t.Fatalf("commit singleton %d: %v", index, err)
		}
		data, err := fixture.arena.resolveCallData(fixture.objects[index], selectorID(index+1))
		if err != nil {
			t.Fatalf("resolve call data %d: %v", index, err)
		}
		objectNode := h1OracleNode{kind: h1OracleObject, ref: uint32(fixture.objects[index])}
		eigenNode := h1OracleNode{kind: h1OracleEigen, ref: uint32(data.eigen)}
		environmentNode := h1OracleNode{kind: h1OracleEnvironment, ref: uint32(data.environment)}
		fixture.environments[index] = data.environment
		fixture.graph.addNode(eigenNode)
		fixture.graph.addNode(environmentNode)
		fixture.graph.addEdge(objectNode, eigenNode)
		fixture.graph.addEdge(eigenNode, environmentNode)
		if parent != 0 {
			fixture.graph.addEdge(environmentNode, h1OracleNode{kind: h1OracleEnvironment, ref: uint32(parent)})
		}
	}
	// Form one cycle through the two independently allocated capture cells.
	// The oracle records the logical guest graph supplied by the test; it does
	// not discover these edges by reading arena slots.
	for index := 0; index < h1EigenCapacity; index++ {
		data, err := fixture.arena.resolveCallData(fixture.objects[index], selectorID(index+1))
		if err != nil {
			t.Fatalf("resolve cycle call data %d: %v", index, err)
		}
		target := fixture.objects[(index+1)%h1EigenCapacity]
		if err := fixture.arena.storeEnvironment(data.environment, 0, h1ObjectValue(target)); err != nil {
			t.Fatalf("store cycle edge %d: %v", index, err)
		}
		fixture.graph.addEdge(
			h1OracleNode{kind: h1OracleEnvironment, ref: uint32(data.environment)},
			h1OracleNode{kind: h1OracleObject, ref: uint32(target)},
		)
	}
	// Exercise both admitted object-field edge kinds independently of the
	// singleton chain: object 2 reaches object 3, which reaches environment 0.
	if err := fixture.arena.storeObjectField(fixture.objects[2], 0, h1ObjectValue(fixture.objects[3])); err != nil {
		t.Fatalf("store object field edge: %v", err)
	}
	if err := fixture.arena.storeObjectField(fixture.objects[3], 0, h1EnvironmentValue(fixture.environments[0])); err != nil {
		t.Fatalf("store environment field edge: %v", err)
	}
	fixture.graph.addEdge(
		h1OracleNode{kind: h1OracleObject, ref: uint32(fixture.objects[2])},
		h1OracleNode{kind: h1OracleObject, ref: uint32(fixture.objects[3])},
	)
	fixture.graph.addEdge(
		h1OracleNode{kind: h1OracleObject, ref: uint32(fixture.objects[3])},
		h1OracleNode{kind: h1OracleEnvironment, ref: uint32(fixture.environments[0])},
	)
	for _, object := range fixture.objects {
		fixture.rootValues = append(fixture.rootValues, h1OracleRoot{
			node: h1OracleNode{kind: h1OracleObject, ref: uint32(object)}, value: h1ObjectValue(object),
		})
	}
	for _, environment := range fixture.environments {
		fixture.rootValues = append(fixture.rootValues, h1OracleRoot{
			node: h1OracleNode{kind: h1OracleEnvironment, ref: uint32(environment)}, value: h1EnvironmentValue(environment),
		})
	}
	return fixture
}

func TestH1ArenaIndependentOracleAllRootMasks(t *testing.T) {
	const rootCandidates = h1ObjectCapacity + h1EnvironmentCapacity
	for mask := 0; mask < 1<<rootCandidates; mask++ {
		t.Run(strconv.Itoa(mask), func(t *testing.T) {
			fixture := newH1OracleFixture(t)
			roots := make([]h1OracleNode, 0, rootCandidates)
			for index, root := range fixture.rootValues {
				if mask&(1<<index) == 0 {
					continue
				}
				if err := fixture.arena.pushRoot(root.value); err != nil {
					t.Fatalf("push root %d: %v", index, err)
				}
				roots = append(roots, root.node)
			}

			wantLive := fixture.graph.reachable(roots)
			if err := fixture.arena.collect(false); err != nil {
				t.Fatalf("collect: %v", err)
			}
			assertH1OracleState(t, fixture.arena, fixture.graph.nodes, wantLive, len(roots))

			// Reallocation must consume exactly the reclaimed object indices in
			// ascending order and advance, rather than reuse, their generations.
			var wantIndices []uint8
			oldGeneration := make(map[uint8]uint32)
			for index, object := range fixture.objects {
				node := h1OracleNode{kind: h1OracleObject, ref: uint32(object)}
				if wantLive[node] {
					continue
				}
				slot, generation, ok := h1UnpackReference(uint32(object))
				if !ok {
					t.Fatalf("object %d has malformed reference %d", index, object)
				}
				wantIndices = append(wantIndices, slot)
				oldGeneration[slot] = generation
			}
			for ordinal, wantIndex := range wantIndices {
				reused, err := fixture.arena.allocateObject(dispatchClassID(20+ordinal), shapeID(20+ordinal), int64(20+ordinal))
				if err != nil {
					t.Fatalf("reuse %d: %v", ordinal, err)
				}
				gotIndex, gotGeneration, ok := h1UnpackReference(uint32(reused))
				if !ok || gotIndex != wantIndex || gotGeneration != oldGeneration[wantIndex]+1 {
					t.Fatalf("reuse %d = (%d,%d,%v), want (%d,%d,true)", ordinal, gotIndex, gotGeneration, ok, wantIndex, oldGeneration[wantIndex]+1)
				}
			}
		})
	}
}

func assertH1OracleState(t *testing.T, arena *h1Arena, nodes []h1OracleNode, wantLive map[h1OracleNode]bool, rootCount int) {
	t.Helper()
	gotLive := map[h1OracleKind]uint8{}
	initial := map[h1OracleKind]uint64{}
	for _, node := range nodes {
		initial[node.kind]++
		var err error
		switch node.kind {
		case h1OracleObject:
			_, err = arena.resolveObject(h1ObjectRef(node.ref))
		case h1OracleEigen:
			_, err = arena.resolveEigen(h1EigenRef(node.ref))
		case h1OracleEnvironment:
			_, err = arena.resolveEnvironment(h1EnvironmentRef(node.ref))
		default:
			t.Fatalf("unknown oracle kind %d", node.kind)
		}
		if wantLive[node] {
			if err != nil {
				t.Errorf("reachable %v rejected: %v", node, err)
			}
			gotLive[node.kind]++
		} else if !errors.Is(err, errH1ArenaInvalidRef) {
			t.Errorf("reclaimed %v resolve error = %v, want invalid reference", node, err)
		}
	}

	stats := arena.stats()
	if stats.LiveObjects != gotLive[h1OracleObject] || stats.LiveEigen != gotLive[h1OracleEigen] || stats.LiveEnvironments != gotLive[h1OracleEnvironment] {
		t.Errorf("live stats = %d/%d/%d, oracle = %d/%d/%d", stats.LiveObjects, stats.LiveEigen, stats.LiveEnvironments,
			gotLive[h1OracleObject], gotLive[h1OracleEigen], gotLive[h1OracleEnvironment])
	}
	if stats.ObjectReclaims != initial[h1OracleObject]-uint64(gotLive[h1OracleObject]) ||
		stats.EigenReclaims != initial[h1OracleEigen]-uint64(gotLive[h1OracleEigen]) ||
		stats.EnvironmentReclaims != initial[h1OracleEnvironment]-uint64(gotLive[h1OracleEnvironment]) {
		t.Errorf("reclaims = %d/%d/%d, oracle live = %d/%d/%d", stats.ObjectReclaims, stats.EigenReclaims, stats.EnvironmentReclaims,
			gotLive[h1OracleObject], gotLive[h1OracleEigen], gotLive[h1OracleEnvironment])
	}
	if stats.Collections != 1 {
		t.Errorf("collections = %d, want 1", stats.Collections)
	}
	if stats.MarkHighWater > h1MarkCapacity {
		t.Errorf("mark high-water = %d, cap = %d", stats.MarkHighWater, h1MarkCapacity)
	}
	if stats.LastMarkWork != uint8(len(wantLive)) {
		t.Errorf("mark work = %d, independent reachable set = %d", stats.LastMarkWork, len(wantLive))
	}
	if stats.LastMarkWork > h1MarkCapacity {
		t.Errorf("mark work = %d, hard ceiling = %d", stats.LastMarkWork, h1MarkCapacity)
	}
	work := arena.collectionWork()
	if work.RootValues != uint8(rootCount) || work.MarkedObjects != gotLive[h1OracleObject] ||
		work.MarkedEigen != gotLive[h1OracleEigen] || work.MarkedEnvironments != gotLive[h1OracleEnvironment] {
		t.Errorf("collection work roots/marked = %d/%d/%d/%d, oracle = %d/%d/%d/%d", work.RootValues,
			work.MarkedObjects, work.MarkedEigen, work.MarkedEnvironments, rootCount,
			gotLive[h1OracleObject], gotLive[h1OracleEigen], gotLive[h1OracleEnvironment])
	}
	if work.ObjectScans != h1ObjectCapacity || work.EigenScans != h1EigenCapacity ||
		work.EnvironmentScans != h1EnvironmentCapacity {
		t.Errorf("sweep scans = %d/%d/%d, want fixed capacities %d/%d/%d", work.ObjectScans,
			work.EigenScans, work.EnvironmentScans, h1ObjectCapacity, h1EigenCapacity, h1EnvironmentCapacity)
	}
	if work.ReclaimedObjects != uint8(initial[h1OracleObject])-gotLive[h1OracleObject] ||
		work.ReclaimedEigen != uint8(initial[h1OracleEigen])-gotLive[h1OracleEigen] ||
		work.ReclaimedEnvs != uint8(initial[h1OracleEnvironment])-gotLive[h1OracleEnvironment] {
		t.Errorf("collection reclaimed = %d/%d/%d, oracle = %d/%d/%d", work.ReclaimedObjects, work.ReclaimedEigen,
			work.ReclaimedEnvs, uint8(initial[h1OracleObject])-gotLive[h1OracleObject],
			uint8(initial[h1OracleEigen])-gotLive[h1OracleEigen], uint8(initial[h1OracleEnvironment])-gotLive[h1OracleEnvironment])
	}
}

func TestH1ArenaFixedEconomy(t *testing.T) {
	if h1ObjectCapacity != 4 || h1EigenCapacity != 2 || h1EnvironmentCapacity != 2 || h1RootCapacity != 8 ||
		h1EnvironmentCells != 1 || h1ObjectFields != 2 || h1MarkCapacity != 8 {
		t.Fatalf("capacity drift: objects=%d eigen=%d environments=%d roots=%d cells=%d fields=%d marks=%d", h1ObjectCapacity,
			h1EigenCapacity, h1EnvironmentCapacity, h1RootCapacity, h1EnvironmentCells, h1ObjectFields, h1MarkCapacity)
	}

	checks := []struct {
		name string
		got  uintptr
		max  uintptr
	}{
		{"reference", reflect.TypeOf(h1ObjectRef(0)).Size(), 4},
		{"value", reflect.TypeOf(h1Value{}).Size(), 16},
		{"object slot", reflect.TypeOf(h1ObjectSlot{}).Size(), 72},
		{"eigen entry", reflect.TypeOf(h1EigenEntry{}).Size(), 32},
		{"eigen slot", reflect.TypeOf(h1EigenSlot{}).Size(), 48},
		{"environment slot", reflect.TypeOf(h1EnvironmentSlot{}).Size(), 32},
		{"mark item", reflect.TypeOf(h1MarkItem{}).Size(), 8},
		{"call value", reflect.TypeOf(h1CallData{}).Size(), 40},
		{"owner", reflect.TypeOf(h1Arena{}).Size(), 800},
	}
	for _, check := range checks {
		t.Logf("H1a %s size: %d bytes (ceiling %d)", check.name, check.got, check.max)
		if check.got > check.max {
			t.Errorf("%s size = %d, exceeds %d", check.name, check.got, check.max)
		}
	}
}

func TestH1ArenaDeterministicTypedReuse(t *testing.T) {
	fixture := newH1OracleFixture(t)
	oldEigen := fixture.environments
	var oldRows [h1EigenCapacity]h1EigenRef
	for index := range oldRows {
		data, err := fixture.arena.resolveCallData(fixture.objects[index], selectorID(index+1))
		if err != nil {
			t.Fatal(err)
		}
		oldRows[index] = data.eigen
	}
	if err := fixture.arena.collect(false); err != nil {
		t.Fatal(err)
	}

	for ordinal := 0; ordinal < h1EigenCapacity; ordinal++ {
		object, err := fixture.arena.allocateObject(dispatchClassID(30+ordinal), shapeID(30+ordinal), int64(30+ordinal))
		if err != nil {
			t.Fatalf("allocate replacement %d: %v", ordinal, err)
		}
		objectIndex, objectGeneration, ok := h1UnpackReference(uint32(object))
		if !ok || objectIndex != uint8(ordinal+1) || objectGeneration != 2 {
			t.Fatalf("replacement object %d = (%d,%d,%v), want (%d,2,true)", ordinal, objectIndex, objectGeneration, ok, ordinal+1)
		}
		transaction, err := fixture.arena.beginSingletonDefinition(
			object, selectorID(20+ordinal), h1TargetID(20+ordinal), lookupVisibilityPublic, h1IntegerValue(int64(20+ordinal)), 0,
		)
		if err != nil {
			t.Fatalf("begin replacement %d: %v", ordinal, err)
		}
		if err := fixture.arena.commitSingletonDefinition(transaction, false); err != nil {
			t.Fatalf("commit replacement %d: %v", ordinal, err)
		}
		data, err := fixture.arena.resolveCallData(object, selectorID(20+ordinal))
		if err != nil {
			t.Fatalf("resolve replacement %d: %v", ordinal, err)
		}
		assertH1ReusedReference(t, "eigen", uint32(oldRows[ordinal]), uint32(data.eigen), uint8(ordinal+1))
		assertH1ReusedReference(t, "environment", uint32(oldEigen[ordinal]), uint32(data.environment), uint8(ordinal+1))
	}
}

func assertH1ReusedReference(t *testing.T, kind string, old, current uint32, wantIndex uint8) {
	t.Helper()
	oldIndex, oldGeneration, oldOK := h1UnpackReference(old)
	index, generation, ok := h1UnpackReference(current)
	if !oldOK || !ok || oldIndex != wantIndex || index != wantIndex || generation != oldGeneration+1 || current == old {
		t.Fatalf("%s reuse old=(%d,%d,%v) current=(%d,%d,%v), want index %d and next generation",
			kind, oldIndex, oldGeneration, oldOK, index, generation, ok, wantIndex)
	}
}

func TestH1ArenaAdmittedCycleAllocatesNoGoHeapAfterConstruction(t *testing.T) {
	arena := newH1Arena()
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
		transaction, err := arena.beginSingletonDefinition(object, 1, 1, lookupVisibilityPublic, h1ObjectValue(object), 0)
		if err != nil {
			runErr = err
			return
		}
		if err = arena.commitSingletonDefinition(transaction, false); err != nil {
			runErr = err
			return
		}
		marker := arena.rootMarker()
		if err = arena.pushRoot(h1ObjectValue(object)); err != nil {
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
		t.Fatalf("allocation cycle: %v", runErr)
	}
	if allocations != 0 {
		t.Fatalf("Go allocations per admitted allocate/root/collect/reuse cycle = %g, want 0", allocations)
	}
	t.Logf("H1a admitted allocate/root/collect/reuse Go allocations after owner construction: %.0f", allocations)
}

func TestH1ArenaProductionSourceUsesOnlyAdmittedMechanisms(t *testing.T) {
	contents, err := os.ReadFile("h1_arena.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "h1_arena.go", contents, goparser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		switch path {
		case "C", "unsafe", "reflect", "runtime", "sync", "sync/atomic":
			t.Errorf("forbidden H1a production import %q", path)
		}
	}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if ast.IsExported(declaration.Name.Name) {
				t.Errorf("H1a production function %q is exported", declaration.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(spec.Name.Name) {
						t.Errorf("H1a production type %q is exported", spec.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if ast.IsExported(name.Name) {
							t.Errorf("H1a production value %q is exported", name.Name)
						}
					}
				}
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.GoStmt:
			t.Error("H1a production source contains a goroutine")
		case *ast.ChanType:
			t.Error("H1a production source contains a channel")
		case *ast.MapType:
			t.Error("H1a production source contains a map")
		}
		return true
	})
}

func TestH1ArenaPreservesTarget41Artifact(t *testing.T) {
	contents, err := os.ReadFile("generated/ruby_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	got := sha256.Sum256(contents)
	const want = "b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("generated Ruby artifact SHA-256 = %x, want %s", got, want)
	}
}
