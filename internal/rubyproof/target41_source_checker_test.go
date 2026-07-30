package rubyproof

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const (
	target41PriorSourceBytes  = 3_330
	target41PriorSourceSHA256 = "8f2611ea5042f09f8313c1291af5b44a5bbf8e76ae3733e0745579e8c838c168"
	target41SourceDeltaSHA256 = "1cdf5cfadcf34d44e8b8eadd82213eab32589b9ad64db813f17e3aba85c07256"
	target41FullSourceSHA256  = "bf355c37216015a49a5b2079e3ff842393755a01fe7cf85d070d1f4fbbf66a52"
	target41ThreeLineSHA256   = "c09d4fceec6a72f86c690145113737013b4e2ad4a408341bae7506d177ec85ab"
	target41SingletonOutput   = "13|13|13|44|44|13|334433"
)

func TestTarget41SourceLoweringFrozenClosure(t *testing.T) {
	if len(ProofSource) != 3_901 || strings.Count(ProofSource, "\n") != 228 {
		t.Fatalf("ProofSource closure = %d bytes/%d LF, want 3901/228", len(ProofSource), strings.Count(ProofSource, "\n"))
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(ProofSource))); got != target41FullSourceSHA256 {
		t.Fatalf("ProofSource SHA-256 = %s, want %s", got, target41FullSourceSHA256)
	}
	prior, delta := ProofSource[:target41PriorSourceBytes], ProofSource[target41PriorSourceBytes:]
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(prior))); got != target41PriorSourceSHA256 {
		t.Fatalf("prior source SHA-256 = %s, want %s", got, target41PriorSourceSHA256)
	}
	if len(delta) != 571 || strings.Count(delta, "\n") != 19 {
		t.Fatalf("singleton extension = %d bytes/%d LF, want 571/19", len(delta), strings.Count(delta, "\n"))
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(delta))); got != target41SourceDeltaSHA256 {
		t.Fatalf("singleton extension SHA-256 = %s, want %s", got, target41SourceDeltaSHA256)
	}
	if strings.Count(ProofSource, "c2_receiver = Alpha.new(10)") != 1 ||
		strings.Count(ProofSource, "c2_receiver.define_singleton_method(:label) do") != 1 {
		t.Fatal("ProofSource does not contain exactly one checked singleton allocation and definition")
	}
	if ProofC2OracleOutput != target41SingletonOutput {
		t.Fatalf("singleton oracle output = %q, want %q", ProofC2OracleOutput, target41SingletonOutput)
	}
	threeLines := ProofOracleOutput + "\n" + ProofC1OracleOutput + "\n" + ProofC2OracleOutput + "\n"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(threeLines))); got != target41ThreeLineSHA256 {
		t.Fatalf("three-line oracle SHA-256 = %s, want %s", got, target41ThreeLineSHA256)
	}
}

func TestTarget41SourceLoweringSealsSingletonFacts(t *testing.T) {
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatalf("Compile(ProofSource): %v", diagnostics)
	}
	checked := program.checked
	if len(checked.classes) != 7 || len(checked.classOrder) != 7 || len(checked.lookupOrder) != 8 {
		t.Fatalf("declared/ordered/lookup owners = %d/%d/%d, want 7/7/8", len(checked.classes), len(checked.classOrder), len(checked.lookupOrder))
	}
	alpha, singleton := checked.classes["Alpha"], checked.singletonOwner
	if singleton == nil || singleton != checked.lookupOrder[7] || singleton.id != 8 || singleton.kind != checkedOwnerSingleton ||
		singleton.superclass != alpha || singleton.node != 8 || singleton.dispatch != 5 || singleton.shape != alpha.shape || singleton.shape != 1 {
		t.Fatalf("checked singleton owner = %#v; Alpha = %#v", singleton, alpha)
	}
	if len(checked.definitions) != 17 || len(checked.methodsByDefinition) != 17 || len(checked.orderedOperations) != 24 {
		t.Fatalf("definitions/bodies/operations = %d/%d/%d, want 17/17/24", len(checked.definitions), len(checked.methodsByDefinition), len(checked.orderedOperations))
	}
	definition, operation := checked.singletonDefinition, checked.singletonOperation
	if definition == nil || definition.id != 17 || definition.owner != singleton || definition.selector != checked.selectors["label"] ||
		definition.body == nil || definition.body.definition != definition.id || operation == nil || operation.id != 24 ||
		operation.kind != lookupMutationDefine || operation.owner != singleton || operation.definition != definition || operation.visibility != lookupVisibilityPublic {
		t.Fatalf("singleton definition/operation = %#v / %#v", definition, operation)
	}

	image := checked.lookup
	if image == nil {
		t.Fatal("checked lookup image is nil")
	}
	if len(image.nodes) != 8 || len(image.rows) != 5 || len(image.selectors) != 8 ||
		len(image.definitions) != 17 || len(image.mutations) != 24 || len(image.routeOracles) != 20 || len(image.oraclePaths) != 56 {
		t.Fatalf("singleton lookup inventory nodes/rows/selectors/definitions/mutations/oracles/paths = %d/%d/%d/%d/%d/%d/%d, want 8/5/8/17/24/20/56",
			len(image.nodes), len(image.rows), len(image.selectors), len(image.definitions), len(image.mutations), len(image.routeOracles), len(image.oraclePaths))
	}
	node, row := image.nodes[7], image.rows[4]
	if node.id != 8 || node.kind != lookupNodeSingleton || node.superclass != alpha.node || node.dispatch != singleton.dispatch ||
		row.id != singleton.dispatch || row.node != singleton.node {
		t.Fatalf("singleton node/row = %#v / %#v", node, row)
	}
	wantSingletonRoutes := [][]lookupNodeID{{8, 2, 1}, {8, 2, 5, 1}, {8, 2, 1}, {8, 2, 5, 1}}
	for mask, want := range wantSingletonRoutes {
		oracle := image.routeOracles[mask*len(image.rows)+4]
		end := int(oracle.pathStart) + int(oracle.pathLength)
		if oracle.activations != uint64(mask) || oracle.row != singleton.dispatch || end > len(image.oraclePaths) ||
			!reflect.DeepEqual(image.oraclePaths[oracle.pathStart:uint32(end)], want) {
			t.Fatalf("singleton route mask %02b = %#v / %v, want %v", mask, oracle, image.oraclePaths[oracle.pathStart:uint32(end)], want)
		}
	}
	lastMutation := image.mutations[len(image.mutations)-1]
	if lastMutation.id != 24 || lastMutation.owner != singleton.node || lastMutation.selector != checked.selectors["label"] ||
		lastMutation.kind != lookupMutationDefine || lastMutation.before != (lookupEntry{}) ||
		lastMutation.after.kind != lookupEntryDefined || lastMutation.after.visibility != lookupVisibilityPublic {
		t.Fatalf("singleton mutation = %#v", lastMutation)
	}

	allocationFact := checked.calls[checked.singletonAllocationCall]
	definitionFact := checked.calls[checked.singletonDefinitionCall]
	if allocationFact.kind != checkedCallNew || allocationFact.class != alpha.id || allocationFact.dispatch != singleton.dispatch || allocationFact.shape != alpha.shape {
		t.Fatalf("singleton allocation fact = %#v", allocationFact)
	}
	if definitionFact.kind != checkedCallDefineSingleton || definitionFact.selector != checked.selectors["label"] ||
		definitionFact.mutation != operation.id || definitionFact.dispatch != singleton.dispatch || definitionFact.shape != alpha.shape {
		t.Fatalf("singleton definition fact = %#v", definitionFact)
	}
	peer := target41TopAssignment(checked.module.statements, "c2_peer")
	if peer == nil || peer.expression == nil {
		t.Fatal("checked c2_peer allocation is missing")
	}
	peerFact := checked.calls[peer.expression]
	if peerFact.kind != checkedCallNew || peerFact.dispatch != alpha.dispatch || peerFact.shape != alpha.shape || peerFact.dispatch == singleton.dispatch {
		t.Fatalf("ordinary c2_peer allocation fact = %#v", peerFact)
	}
}

func TestTarget41SourceLoweringRejectsSingletonShapeDrift(t *testing.T) {
	tests := []struct {
		name, old, replacement, want string
	}{
		{"allocation class", "c2_receiver = Alpha.new(10)", "c2_receiver = Beta.new(10)", "c2_receiver must be assigned directly from Alpha.new(10)"},
		{"receiver", "c2_receiver.define_singleton_method(:label) do", "c2_peer.define_singleton_method(:label) do", "singleton definition requires c2_receiver.define_singleton_method(:label)"},
		{"selector", "c2_receiver.define_singleton_method(:label) do", "c2_receiver.define_singleton_method(:trace) do", "singleton definition requires c2_receiver.define_singleton_method(:label)"},
		{"block parameter", "define_singleton_method(:label) do\n  mark(4)", "define_singleton_method(:label) do |value|\n  mark(4)", "zero-parameter block"},
		{"block effect", "define_singleton_method(:label) do\n  mark(4)\n  44", "define_singleton_method(:label) do\n  mark(5)\n  44", "must be exactly mark(4) followed by 44"},
		{"block result", "define_singleton_method(:label) do\n  mark(4)\n  44", "define_singleton_method(:label) do\n  mark(4)\n  45", "must be exactly mark(4) followed by 44"},
		{"captured local", "define_singleton_method(:label) do\n  mark(4)\n  44", "define_singleton_method(:label) do\n  mark(4)\n  c2_before", "must be exactly mark(4) followed by 44"},
		{"repeated allocation", "c2_receiver = Alpha.new(10)\nc2_peer", "c2_receiver = Alpha.new(10)\nc2_receiver = Alpha.new(10)\nc2_peer", "allocation must appear exactly once"},
		{"escaping alias", "c2_peer_before = read_singleton(c2_peer)\nc2_receiver.define", "c2_peer_before = read_singleton(c2_peer)\nc2_escape = c2_receiver\nc2_receiver.define", "outside the straight-line singleton schedule"},
		{"clone", "c2_before = read_singleton(c2_receiver)", "c2_before = c2_receiver.clone()", "must read the checked c2_receiver"},
		{"reflection", "c2_before = read_singleton(c2_receiver)", "c2_before = c2_receiver.singleton_class()", "must read the checked c2_receiver"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if strings.Count(ProofSource, test.old) != 1 {
				t.Fatalf("test fixture occurrence count for %q is not one", test.old)
			}
			_, diagnostics := Compile(strings.Replace(ProofSource, test.old, test.replacement, 1))
			if !target41HasDiagnostic(diagnostics, test.want) {
				t.Fatalf("Compile diagnostics = %v, want one containing %q", diagnostics, test.want)
			}
		})
	}
}

func target41TopAssignment(statements []*statement, name string) *statement {
	for _, item := range statements {
		if item.kind == statementAssign && item.name == name {
			return item
		}
	}
	return nil
}

func target41HasDiagnostic(diagnostics []Diagnostic, want string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, want) {
			return true
		}
	}
	return false
}
