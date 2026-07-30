package rubyproof

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestH1bSourceIdentityIsFrozen(t *testing.T) {
	t.Parallel()

	if got, want := len(H1bSource), 718; got != want {
		t.Fatalf("len(H1bSource) = %d, want %d", got, want)
	}
	if got, want := strings.Count(H1bSource, "\n"), 35; got != want {
		t.Fatalf("H1bSource line feeds = %d, want %d", got, want)
	}
	if !strings.HasSuffix(H1bSource, "\n") {
		t.Fatal("H1bSource does not end in a line feed")
	}
	assertH1bSHA(t, []byte(H1bSource), "999dee89ddb14f0c197fa0edde6fead99910eb82996b541409945ae09c3f41cb")
	assertH1bSHA(t, []byte(H1bOracleOutput+"\n"), "bea4358be9700b9e664d3ac83b5385e2549c8889295e118ec79996cfe0ceff06")
}

func TestCompileH1bSealsExactStablePlan(t *testing.T) {
	t.Parallel()

	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 {
		t.Fatalf("CompileH1b diagnostics = %v", diagnostics)
	}
	if program.IsZero() || program.checked == nil {
		t.Fatal("CompileH1b returned a zero program")
	}
	identity := program.Identity()
	if got, want := hex.EncodeToString(identity[:]), "c9cf9ceea54752159bed7cce29c49b1251442d9dd7da0930408c345b5544dd3c"; got != want {
		t.Fatalf("H1b identity = %s, want %s", got, want)
	}

	plan := program.checked
	if got, want := hex.EncodeToString(plan.sourceHash[:]), "999dee89ddb14f0c197fa0edde6fead99910eb82996b541409945ae09c3f41cb"; got != want {
		t.Fatalf("source hash = %s, want %s", got, want)
	}
	if plan.sourceBytes != 718 || plan.sourceLineFeeds != 35 {
		t.Fatalf("source dimensions = %d bytes/%d LF, want 718/35", plan.sourceBytes, plan.sourceLineFeeds)
	}
	if plan.class != (h1bClassPlan{dispatch: 1, shape: 1, ordinaryValue: 7}) {
		t.Fatalf("class plan = %#v", plan.class)
	}
	if got, want := plan.bindings, (h1bBindingPlan{
		readReceiver: 1, installReceiver: 2, captured: 3, receiver: 4, peer: 5,
		before: 6, warm: 7, peerBefore: 8, first: 9, second: 10, peerAfter: 11,
		peerAfterReceiverCollection: 12, result: 13,
	}); got != want {
		t.Fatalf("binding plan = %#v, want %#v", got, want)
	}
	if got, want := plan.call, (h1bCallPlan{site: 1, selector: 1, singletonTarget: 1, visibility: lookupVisibilityPublic}); got != want {
		t.Fatalf("call plan = %#v, want %#v", got, want)
	}
	if got, want := plan.capture, (h1bCapturePlan{binding: 3, cell: 0, initial: 40, bodyIncrement: 1, helperIncrement: 2}); got != want {
		t.Fatalf("capture plan = %#v, want %#v", got, want)
	}
	if got, want := plan.roots, (h1bRootPlan{topSlots: 2, installObjectRoots: 1, maximumSlots: 3}); got != want {
		t.Fatalf("root plan = %#v, want %#v", got, want)
	}
	if got, want := plan.capacity, (h1bCapacityPlan{
		objects: 4, environments: 2, roots: 8, markWork: 6, objectFields: 2, environmentCells: 1,
		referenceIndexBits: 8, maximumGeneration: 0x00ff_ffff,
	}); got != want {
		t.Fatalf("capacity plan = %#v, want %#v", got, want)
	}
	wantCollections := [3]h1bCollectionPlan{
		{id: 1, markedObjects: 2, markedEnvironments: 1, markWork: 3},
		{id: 2, markedObjects: 1, reclaimedObjects: 1, reclaimedEnvironments: 1, markWork: 1},
		{id: 3, reclaimedObjects: 1},
	}
	if plan.collections != wantCollections {
		t.Fatalf("collection plan = %#v, want %#v", plan.collections, wantCollections)
	}
	if got, want := plan.result, ([7]int64{7, 7, 7, 43, 44, 7, 7}); got != want {
		t.Fatalf("result plan = %v, want %v", got, want)
	}
	if plan.spans.readCall.StartLine != 8 || plan.spans.captureDeclaration.StartLine != 12 ||
		plan.spans.collections[0].StartLine != 16 || plan.spans.collections[1].StartLine != 31 ||
		plan.spans.collections[2].StartLine != 35 {
		t.Fatalf("key checked spans = read %v capture %v collections %v", plan.spans.readCall, plan.spans.captureDeclaration, plan.spans.collections)
	}
}

func TestCompileH1bIsDeterministicAndDeeplyUnaliased(t *testing.T) {
	t.Parallel()

	first, firstDiagnostics := CompileH1b(H1bSource)
	second, secondDiagnostics := CompileH1b(H1bSource)
	if len(firstDiagnostics) != 0 || len(secondDiagnostics) != 0 {
		t.Fatalf("CompileH1b diagnostics first=%v second=%v", firstDiagnostics, secondDiagnostics)
	}
	if first.checked == second.checked {
		t.Fatal("independent compilations share a checked plan pointer")
	}
	if !reflect.DeepEqual(first.checked, second.checked) {
		t.Fatalf("independent plans differ:\nfirst=%#v\nsecond=%#v", first.checked, second.checked)
	}
	assertH1bValueOnlyType(t, reflect.TypeOf(*first.checked), "checkedH1bPlan")

	secondIdentity := second.Identity()
	first.checked.result[0] = -1
	first.checked.spans.reads[0] = Span{}
	if second.checked.result != ([7]int64{7, 7, 7, 43, 44, 7, 7}) || second.checked.spans.reads[0] == (Span{}) || second.Identity() != secondIdentity {
		t.Fatal("mutating one independently compiled plan changed the other")
	}
}

func TestCompileH1bRejectsEveryDocumentedWidening(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		old  string
		new  string
	}{
		{name: "additional class", old: "class H1bReceiver", new: "class Extra\nend\n\nclass H1bReceiver"},
		{name: "different class", old: "class H1bReceiver", new: "class OtherReceiver"},
		{name: "ordinary method arity", old: "def label()", new: "def label(value)"},
		{name: "ordinary method value", old: "    7\n", new: "    8\n"},
		{name: "dynamic selector", old: "define_singleton_method(:label)", new: "define_singleton_method(selector)"},
		{name: "alternate singleton body", old: "captured = captured + 1", new: "captured = captured + 2"},
		{name: "multiple singleton body statements", old: "    captured = captured + 1\n", new: "    captured = captured + 1\n    captured = captured + 1\n"},
		{name: "block parameter", old: "(:label) do\n", new: "(:label) do |value|\n"},
		{name: "second capture", old: "  captured = 40\n", new: "  captured = 40\n  other = 1\n"},
		{name: "helper increment", old: "captured = captured + 2", new: "captured = captured + 3"},
		{name: "missing helper collection", old: "  GC.start()\n", new: ""},
		{name: "extra helper collection", old: "  GC.start()\n", new: "  GC.start()\n  GC.start()\n"},
		{name: "different receiver", old: "receiver.define_singleton_method", new: "peer.define_singleton_method"},
		{name: "peer aliases receiver", old: "peer = H1bReceiver.new()", new: "peer = receiver"},
		{name: "third allocation", old: "peer = H1bReceiver.new()\n", new: "peer = H1bReceiver.new()\nextra = H1bReceiver.new()\n"},
		{name: "redefinition", old: "receiver = 0\n", new: "receiver.define_singleton_method(:label) do\n    9\n  end\nreceiver = 0\n"},
		{name: "removal", old: "receiver = 0\n", new: "receiver.remove_method(:label)\nreceiver = 0\n"},
		{name: "visibility mutation", old: "receiver = 0\n", new: "receiver.private(:label)\nreceiver = 0\n"},
		{name: "block escape", old: "receiver.define_singleton_method(:label) do", new: "saved = receiver.define_singleton_method(:label) do"},
		{name: "capture escape", old: "  receiver\nend\n", new: "  captured\nend\n"},
		{name: "reflective hook", old: "  def label()\n", new: "  def singleton_method_added()\n    0\n  end\n\n  def label()\n"},
		{name: "finalizer", old: "receiver = 0\n", new: "ObjectSpace.define_finalizer(receiver)\nreceiver = 0\n"},
		{name: "host pin", old: "receiver = 0\n", new: "pin(receiver)\nreceiver = 0\n"},
		{name: "object result", old: "peer_after_receiver_collection]", new: "peer_after_receiver_collection, peer]"},
		{name: "reified eigenclass", old: "receiver = H1bReceiver.new()", new: "receiver = H1bReceiver.new()\nclass << receiver\nend"},
		{name: "recursion", old: "    7\n", new: "    label()\n"},
		{name: "additional control flow", old: "  captured = 40\n", new: "  begin\n    captured = 40\n  end\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(H1bSource, test.old, test.new, 1)
			if source == H1bSource {
				t.Fatalf("test replacement %q was not applied", test.old)
			}
			first, firstDiagnostics := CompileH1b(source)
			second, secondDiagnostics := CompileH1b(source)
			if !first.IsZero() || !second.IsZero() {
				t.Fatal("widened source compiled")
			}
			if len(firstDiagnostics) == 0 || !reflect.DeepEqual(firstDiagnostics, secondDiagnostics) {
				t.Fatalf("diagnostics first=%v second=%v", firstDiagnostics, secondDiagnostics)
			}
			for _, diagnostic := range firstDiagnostics {
				if diagnostic.Span.StartLine < 1 || diagnostic.Span.StartColumn < 1 || diagnostic.Span.End < diagnostic.Span.Start {
					t.Fatalf("diagnostic is not source-attributed: %#v", diagnostic)
				}
			}
		})
	}
}

func TestCompileH1bRejectsStaticAndParserDrift(t *testing.T) {
	t.Parallel()

	if program, diagnostics := CompileH1b(ProofSource); !program.IsZero() || len(diagnostics) != 1 ||
		diagnostics[0].Message != "ruby: H1b source does not match the exact admitted program" {
		t.Fatalf("CompileH1b(ProofSource) = (%#v, %v)", program, diagnostics)
	}
	if plan, diagnostics := checkH1bRuby(H1bSource, nil); plan != nil || len(diagnostics) != 1 ||
		diagnostics[0].Message != "ruby: H1b parsed shape does not match the exact admitted program" {
		t.Fatalf("checkH1bRuby(exact, nil) = (%#v, %v)", plan, diagnostics)
	}
	if got := (H1bProgram{}).Identity(); got != ([sha256.Size]byte{}) || !(H1bProgram{}).IsZero() {
		t.Fatalf("zero H1bProgram identity/state = (%x, %t)", got, (H1bProgram{}).IsZero())
	}
}

func assertH1bSHA(t *testing.T, data []byte, want string) {
	t.Helper()
	got := sha256.Sum256(data)
	if encoded := hex.EncodeToString(got[:]); encoded != want {
		t.Fatalf("SHA-256 = %s, want %s", encoded, want)
	}
}

func assertH1bValueOnlyType(t *testing.T, typ reflect.Type, path string) {
	t.Helper()
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		t.Fatalf("%s retains mutable/indirect %s", path, typ)
	case reflect.Array:
		assertH1bValueOnlyType(t, typ.Elem(), path+"[]")
	case reflect.Struct:
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			assertH1bValueOnlyType(t, field.Type, path+"."+field.Name)
		}
	}
}
