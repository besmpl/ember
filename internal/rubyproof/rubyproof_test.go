package rubyproof

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/besmpl/ember/internal/rubyproof/generated"
)

var (
	projectionCallSink  int64
	projectionStateSink generated.ProjectionScalars
)

func proofProgram(t testing.TB) Program {
	t.Helper()
	program, diagnostics := Compile(ProofSource)
	if len(diagnostics) != 0 {
		t.Fatalf("compile ProofSource: %v", diagnostics)
	}
	if program.IsZero() {
		t.Fatal("Compile returned zero Program")
	}
	return program
}

func TestProofSourceCanonicalRuntimeMatchesRubyOracle(t *testing.T) {
	program := proofProgram(t)
	runtime, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	got, err := runtime.Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
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
		Singleton: SingletonResult{
			Before: 13, Warm: 13, PeerBefore: 13, After: 44, AfterHit: 44, PeerAfter: 13, Trace: 334433,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Run = %#v, want %#v", got, want)
	}
	wantOutput := ProofOracleOutput + "\n" + ProofC1OracleOutput + "\n" + ProofC2OracleOutput
	if formatted := formatResult(got); formatted != wantOutput {
		t.Fatalf("formatted result = %q, want oracle %q", formatted, wantOutput)
	}
	firstOwner := runtime.state.lookup
	second, err := runtime.Run(context.Background(), ProofLimits())
	if err != nil || !reflect.DeepEqual(second, want) {
		t.Fatalf("second Run = %#v, %v; want %#v", second, err, want)
	}
	if firstOwner == runtime.state.lookup || firstOwner.image != nil || firstOwner.cells != nil {
		t.Fatal("repeated Run did not close the previous lookup owner and reconstruct cold state")
	}
}

func TestCheckedModulesAndLiteralAttachmentsStayOwnerTyped(t *testing.T) {
	checked := proofProgram(t).checked
	if len(checked.classOrder) != 7 || len(checked.lookupOrder) != 8 || len(checked.definitions) != 17 || len(checked.orderedOperations) != 24 {
		t.Fatalf("checked owner/lookup-owner/definition/operation counts = %d/%d/%d/%d, want 7/8/17/24", len(checked.classOrder), len(checked.lookupOrder), len(checked.definitions), len(checked.orderedOperations))
	}
	included := checked.classes["IncludedLabel"]
	prepended := checked.classes["PrependedLabel"]
	delta := checked.classes["Delta"]
	for _, module := range []*checkedClass{included, prepended} {
		if module == nil || module.kind != checkedOwnerModule || module.superclass != nil || module.realized || module.dispatch != 0 || module.shape != 0 {
			t.Fatalf("checked module = %#v; modules must own methods but no superclass/dispatch/shape", module)
		}
	}
	if delta == nil || delta.kind != checkedOwnerClass || delta.superclass != checked.classes["Alpha"] || !delta.realized || delta.dispatch != 4 || delta.shape != 4 {
		t.Fatalf("checked Delta = %#v", delta)
	}
	if checked.selectors["include"] != 0 || checked.selectors["prepend"] != 0 {
		t.Fatal("topology operations leaked into the method-selector space")
	}
	if len(checked.attachments) != 2 {
		t.Fatalf("checked attachment count = %d, want 2", len(checked.attachments))
	}
	want := []struct {
		kind           lookupAttachmentKind
		target, module *checkedClass
	}{
		{lookupAttachmentInclude, checked.classes["Alpha"], included},
		{lookupAttachmentPrepend, checked.classes["Beta"], prepended},
	}
	for index, attachment := range checked.attachments {
		if attachment.id != lookupAttachmentID(index+1) || attachment.kind != want[index].kind || attachment.target != want[index].target || attachment.module != want[index].module || attachment.ordinal != 0 {
			t.Fatalf("checked attachment %d = %#v", index, attachment)
		}
	}
	wantRoutes := [][][]lookupNodeID{
		{{2, 1}, {3, 1}, {4, 1}, {7, 2, 1}, {8, 2, 1}},
		{{2, 5, 1}, {3, 1}, {4, 1}, {7, 2, 5, 1}, {8, 2, 5, 1}},
		{{2, 1}, {6, 3, 1}, {4, 1}, {7, 2, 1}, {8, 2, 1}},
		{{2, 5, 1}, {6, 3, 1}, {4, 1}, {7, 2, 5, 1}, {8, 2, 5, 1}},
	}
	if len(checked.lookup.routeOracles) != 20 || len(checked.lookup.oraclePaths) != 56 {
		t.Fatalf("checked route oracle/path counts = %d/%d, want 20/56", len(checked.lookup.routeOracles), len(checked.lookup.oraclePaths))
	}
	for maskIndex, routes := range wantRoutes {
		for rowIndex, wantRoute := range routes {
			oracle := checked.lookup.routeOracles[maskIndex*len(routes)+rowIndex]
			end := int(oracle.pathStart) + int(oracle.pathLength)
			if oracle.activations != uint64(maskIndex) || oracle.row != dispatchClassID(rowIndex+1) || end > len(checked.lookup.oraclePaths) {
				t.Fatalf("checked route oracle mask/row %d/%d = %#v", maskIndex, rowIndex+1, oracle)
			}
			if got := checked.lookup.oraclePaths[oracle.pathStart:uint32(end)]; !reflect.DeepEqual(got, wantRoute) {
				t.Fatalf("checked route mask/row %02b/%d = %v, want %v", maskIndex, rowIndex+1, got, wantRoute)
			}
		}
	}
	if checked.finalResultBinding == 0 || checked.topologyResultBinding == 0 || checked.singletonResultBinding == 0 ||
		checked.finalResultBinding == checked.topologyResultBinding || checked.finalResultBinding == checked.singletonResultBinding ||
		checked.topologyResultBinding == checked.singletonResultBinding {
		t.Fatalf("detached result bindings = %d/%d/%d", checked.finalResultBinding, checked.topologyResultBinding, checked.singletonResultBinding)
	}
}

func TestAcceptedSourceChangesIdentityAndSemantics(t *testing.T) {
	base := proofProgram(t)
	changedSource := strings.Replace(ProofSource, "Alpha.new(4)", "Alpha.new(5)", 2)
	changed, diagnostics := Compile(changedSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if changed.Identity() == base.Identity() {
		t.Fatal("source change preserved Program identity")
	}
	got, err := changed.Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Same: true, Other: false,
		Before: 1, BetaBefore: 3, GammaBefore: 4, AlphaWarm: 1, BetaWarm: 3,
		After: 2, AlphaAfterHit: 2, BetaAfter: 3, GammaAfter: 4,
		Saved: 1, PublicBefore: 2, ProtectedRejected: 5, PrivateRejected: 5,
		PrivateSent: 2, PrivateBeta: 3, PublicRestored: 2,
		BetaRemoved: 2, Returned: 15, Value: 107, Trace: 66776777124532, BetaTrace: 88887, GammaTrace: 99,
		Topology: TopologyResult{
			DeltaBefore: 2, DeltaIncluded: 11, BetaRoute: 2, BetaDefined: 22,
			DeltaRedefined: 13, SavedStable: 1, Trace: 713726,
		},
		Singleton: SingletonResult{
			Before: 13, Warm: 13, PeerBefore: 13, After: 44, AfterHit: 44, PeerAfter: 13, Trace: 334433,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed Run = %#v, want %#v", got, want)
	}

	redefinedSource := strings.Replace(ProofSource, "mark(7)\n    2", "mark(5)\n    2", 1)
	redefined, diagnostics := Compile(redefinedSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	got, err = redefined.Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Trace != 66556555124532 || got.BetaTrace != 88885 || got.After != 2 {
		t.Fatalf("redefined Run = %#v", got)
	}
}

func TestRuntimeControlErrorsPoisonOwner(t *testing.T) {
	program := proofProgram(t)
	tests := []struct {
		name   string
		ctx    func() context.Context
		limits Limits
		want   error
	}{
		{
			name: "pre-cancel beats zero limits",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			limits: Limits{},
			want:   context.Canceled,
		},
		{name: "step", ctx: context.Background, limits: Limits{Frames: 32, Objects: 8}, want: ErrStepLimit},
		{name: "frame", ctx: context.Background, limits: Limits{Steps: 512, Objects: 8}, want: ErrFrameLimit},
		{name: "object", ctx: context.Background, limits: Limits{Steps: 512, Frames: 32}, want: ErrObjectLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := program.NewRuntime()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.Run(test.ctx(), test.limits); !errors.Is(err, test.want) {
				t.Fatalf("Run error = %v, want %v", err, test.want)
			}
			if _, err := runtime.Run(context.Background(), ProofLimits()); !errors.Is(err, ErrPoisoned) {
				t.Fatalf("reuse error = %v, want poisoned", err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHostAbortBypassesRubyRescueAndEnsure(t *testing.T) {
	program := proofProgram(t)
	var counterBinding bindingID
	for _, item := range program.checked.module.statements {
		if item.kind == statementAssign && item.name == "alpha" {
			counterBinding = program.checked.assignments[item]
			break
		}
	}
	if counterBinding == 0 {
		t.Fatal("counter binding not found")
	}
	for steps := uint64(1); steps < ProofLimits().Steps; steps++ {
		runtime, err := program.NewRuntime()
		if err != nil {
			t.Fatal(err)
		}
		_, err = runtime.Run(context.Background(), Limits{Steps: steps, Frames: 32, Objects: 8})
		if !errors.Is(err, ErrStepLimit) {
			_ = runtime.Close()
			continue
		}
		counter := runtime.state.top[counterBinding]
		if counter.kind != rubyObjectValue || counter.object == nil {
			_ = runtime.Close()
			continue
		}
		trace := counter.object.fields["@trace"]
		if trace.kind == rubyInteger && trace.integer == 667767771245 {
			if _, err := runtime.Run(context.Background(), ProofLimits()); !errors.Is(err, ErrPoisoned) {
				t.Fatalf("reuse after abort = %v", err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			t.Logf("step budget %d aborts after block effect and before Ruby rescue/ensure", steps)
			return
		}
		_ = runtime.Close()
	}
	t.Fatal("no step boundary observed between the raising block effect and Ruby rescue/ensure")
}

func TestGuestRaiseRunsEnsureAndDoesNotPoisonOwner(t *testing.T) {
	const rescue = `    rescue RuntimeError
      mark(3)
      @value = @value + 100
`
	source := strings.Replace(ProofSource, rescue, "", 1)
	program, diagnostics := Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	runtime, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for attempt := 0; attempt < 2; attempt++ {
		_, err := runtime.Run(context.Background(), ProofLimits())
		var rubyError *RubyError
		if !errors.As(err, &rubyError) || rubyError.Class != "RuntimeError" || rubyError.Message != "boom" {
			t.Fatalf("attempt %d error = %#v", attempt+1, err)
		}
		var counterBinding bindingID
		for _, item := range program.checked.module.statements {
			if item.kind == statementAssign && item.name == "alpha" {
				counterBinding = program.checked.assignments[item]
				break
			}
		}
		counter := runtime.state.top[counterBinding]
		trace := counter.object.fields["@trace"]
		if trace.kind != rubyInteger || trace.integer != 6677677712452 {
			t.Fatalf("attempt %d trace = %#v, want ensure-suffixed 6677677712452", attempt+1, trace)
		}
	}
}

func TestRuntimeOwnerAdmissionAndClose(t *testing.T) {
	runtime, err := proofProgram(t).NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	runtime.active.Store(true)
	if _, err := runtime.Run(context.Background(), ProofLimits()); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy Run = %v", err)
	}
	if err := runtime.Close(); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy Close = %v", err)
	}
	runtime.active.Store(false)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("idempotent Close = %v", err)
	}
	if _, err := runtime.Run(context.Background(), ProofLimits()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after close = %v", err)
	}
}

func TestCompileRejectsMalformedAndUnsupportedSourceDeterministically(t *testing.T) {
	tests := []struct {
		name, source, contains string
	}{
		{"invalid byte", "counter = $\n", "invalid character"},
		{"unknown local", strings.Replace(ProofSource, "alpha = Alpha.new(4)", "alpha = missing", 1), "unknown local"},
		{"unknown rescue", strings.Replace(ProofSource, "rescue RuntimeError", "rescue StandardError", 1), "outside the proof subset"},
		{"alias missing source", strings.Replace(ProofSource, "alias_method(:saved_label, :label)", "alias_method(:saved_label, :missing)", 1), "alias_method source \"missing\" is not defined"},
		{"alias wrong arity", strings.Replace(ProofSource, "alias_method(:saved_label, :label)", "alias_method(:saved_label)", 1), "alias_method requires two symbol arguments"},
		{"remove missing local", strings.Replace(ProofSource, "remove_method(:label)", "remove_method(:missing)", 1), "remove_method target \"missing\" is not locally defined"},
		{"remove non-symbol", strings.Replace(ProofSource, "remove_method(:label)", "remove_method(1)", 1), "remove_method requires one symbol argument"},
		{"undef missing local", strings.Replace(ProofSource, "undef_method(:label)", "undef_method(:missing)", 1), "undef_method target \"missing\" is not locally defined"},
		{"undef wrong arity", strings.Replace(ProofSource, "undef_method(:label)", "undef_method(:label, :other)", 1), "undef_method requires one symbol argument"},
		{"visibility missing local", strings.Replace(ProofSource, "protected(:label)", "protected(:missing)", 1), "protected target \"missing\" is not locally defined"},
		{"visibility non-symbol", strings.Replace(ProofSource, "protected(:label)", "protected(1)", 1), "protected requires one symbol argument"},
		{"visibility no-op", strings.Replace(ProofSource, "protected(:label)", "public(:label)", 1), "already has that visibility"},
		{"include non-literal", strings.Replace(ProofSource, "include(IncludedLabel)", "include(1)", 1), "include requires one literal module constant"},
		{"include class", strings.Replace(ProofSource, "include(IncludedLabel)", "include(Alpha)", 1), "include target \"Alpha\" is not a module"},
		{"module attachment", strings.Replace(ProofSource, "module PrependedLabel\nend", "module PrependedLabel\n  include(IncludedLabel)\nend", 1), "include is admitted only in a class body"},
		{"module superclass", strings.Replace(ProofSource, "class Delta < Alpha", "class Delta < IncludedLabel", 1), "superclass \"IncludedLabel\" is not a class"},
		{"module new", strings.Replace(ProofSource, "c1_alpha = Alpha.new(10)", "c1_alpha = IncludedLabel.new(10)", 1), "new receiver must be a known class"},
		{"send non-symbol", strings.Replace(ProofSource, "receiver.send(:label)", "receiver.send(\"label\")", 1), "send requires a literal method symbol"},
		{"public send unknown", strings.Replace(ProofSource, "receiver.public_send(:label)", "receiver.public_send(:missing)", 1), "unknown public_send target \"missing\""},
		{"public send target arity", strings.Replace(ProofSource, "receiver.public_send(:label)", "receiver.public_send(:label, 1)", 1), "expects 0 arguments, got 1"},
		{"public send rescue", strings.Replace(ProofSource, "rescue NoMethodError", "rescue StandardError", 1), "outside the proof subset"},
		{"source limit", strings.Repeat(" ", maximumSourceBytes+1), "source bytes"},
		{"token limit", strings.Repeat("\n", maximumTokens+1), "token count"},
		{"node limit", strings.Repeat("1\n", maximumNodes/2+1), "syntax node count"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, first := Compile(test.source)
			_, second := Compile(test.source)
			if len(first) == 0 || !reflect.DeepEqual(first, second) {
				t.Fatalf("diagnostics first=%v second=%v", first, second)
			}
			if !strings.Contains(first[0].Message, test.contains) {
				t.Fatalf("diagnostic = %q, want substring %q", first[0].Message, test.contains)
			}
		})
	}
}

func TestPreparedSourceIsOneLocationNeutralSetAndFixtureIsFresh(t *testing.T) {
	program := proofProgram(t)
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	if set.IsZero() || set.PackageName() != "generated" || len(set.Files()) != 1 || set.Files()[0].Name != "ruby_generated.go" {
		t.Fatalf("prepared Set = package %q files %#v", set.PackageName(), set.Files())
	}
	if strings.Contains(set.Files()[0].Content, "github.com/besmpl/ember") || strings.Contains(set.Files()[0].Content, "preparedsource") {
		t.Fatal("generated source retained delivery/compiler dependency")
	}
	changedSource := strings.Replace(ProofSource, "Alpha.new(4)", "Alpha.new(5)", 2)
	changed, diagnostics := Compile(changedSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	changedSet, err := changed.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	if changedSet.Digest() == set.Digest() || changedSet.Files()[0].Content == set.Files()[0].Content {
		t.Fatal("accepted semantic source change preserved generated package")
	}
	unsupportedSource := strings.Replace(ProofSource, "def hot()\n    @value", "def hot()\n    @trace", 1)
	unsupported, diagnostics := Compile(unsupportedSource)
	if len(diagnostics) != 0 {
		t.Fatalf("compile checked but unprepared shape: %v", diagnostics)
	}
	if _, err := unsupported.PreparedSource("generated"); err == nil || !strings.Contains(err.Error(), "hot body is outside prepared proof") {
		t.Fatalf("unsupported prepared shape error = %v", err)
	}
	for _, name := range []string{"", "Generated", "go", "bad-name"} {
		if _, err := program.PreparedSource(name); err == nil {
			t.Fatalf("PreparedSource(%q) succeeded", name)
		}
	}

	const fixture = "generated/ruby_generated.go"
	if os.Getenv("EMBER_UPDATE_RUBY_FIXTURE") != "" {
		if err := os.MkdirAll("generated", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture, []byte(set.Files()[0].Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != set.Files()[0].Content {
		t.Fatal("generated Ruby fixture is stale; run EMBER_UPDATE_RUBY_FIXTURE=1 go test ./internal/rubyproof -run TestPreparedSourceIsOneLocationNeutralSetAndFixtureIsFresh")
	}
	t.Logf("Program=%x Set=%x generated=%x bytes=%d", program.Identity(), set.Digest(), sha256.Sum256(content), len(content))
}

func TestGeneratedPreparedRuntimeMatchesCanonicalAndRecoversBeforeEffects(t *testing.T) {
	canonical, err := proofProgram(t).Run(context.Background(), ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	engine := generated.NewEngine()
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close generated engine: %v", err)
		}
	})
	got, err := engine.Run(context.Background(), generated.ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	converted := Result{
		Same: got.Same, Other: got.Other,
		Before: got.Before, BetaBefore: got.BetaBefore, GammaBefore: got.GammaBefore,
		AlphaWarm: got.AlphaWarm, BetaWarm: got.BetaWarm,
		After: got.After, AlphaAfterHit: got.AlphaAfterHit, BetaAfter: got.BetaAfter, GammaAfter: got.GammaAfter,
		Saved: got.Saved, PublicBefore: got.PublicBefore,
		ProtectedRejected: got.ProtectedRejected, PrivateRejected: got.PrivateRejected,
		PrivateSent: got.PrivateSent, PrivateBeta: got.PrivateBeta, PublicRestored: got.PublicRestored,
		BetaRemoved: got.BetaRemoved,
		Returned:    got.Returned, Value: got.Value, Trace: got.Trace, BetaTrace: got.BetaTrace, GammaTrace: got.GammaTrace,
		Topology: TopologyResult{
			DeltaBefore: got.Topology.DeltaBefore, DeltaIncluded: got.Topology.DeltaIncluded,
			BetaRoute: got.Topology.BetaRoute, BetaDefined: got.Topology.BetaDefined,
			DeltaRedefined: got.Topology.DeltaRedefined, SavedStable: got.Topology.SavedStable,
			Trace: got.Topology.Trace,
		},
		Singleton: SingletonResult{
			Before: got.Singleton.Before, Warm: got.Singleton.Warm, PeerBefore: got.Singleton.PeerBefore,
			After: got.Singleton.After, AfterHit: got.Singleton.AfterHit, PeerAfter: got.Singleton.PeerAfter,
			Trace: got.Singleton.Trace,
		},
	}
	if !reflect.DeepEqual(converted, canonical) {
		t.Fatalf("generated = %#v, canonical = %#v", converted, canonical)
	}
	wantStats := generated.Stats{Hits: 9, ColdAdmissions: 7, StaleMisses: 6, Repairs: 6, UncachedFallbacks: 5, Admissions: 7, Evictions: 0, OccupiedArms: 7}
	if stats := engine.Stats(); stats != wantStats {
		t.Fatalf("prepared lookup stats = %#v, want %#v", stats, wantStats)
	}
	if converted.Trace != 66776777124532 || converted.BetaTrace != 88887 || converted.GammaTrace != 99 || converted.Topology.Trace != 713726 || converted.Singleton.Trace != 334433 {
		t.Fatalf("prepared traces = %d/%d/%d/%d/%d; fallback duplicated or reordered an effect", converted.Trace, converted.BetaTrace, converted.GammaTrace, converted.Topology.Trace, converted.Singleton.Trace)
	}
}

func TestGeneratedPreparedRuntimeControlAndLifetime(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	engine := generated.NewEngine()
	if _, err := engine.Run(canceled, generated.Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel = %v", err)
	}
	if _, err := engine.Run(context.Background(), generated.ProofLimits()); !errors.Is(err, generated.ErrPoisoned) {
		t.Fatalf("reuse = %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Run(context.Background(), generated.ProofLimits()); !errors.Is(err, generated.ErrClosed) {
		t.Fatalf("after close = %v", err)
	}

	limits := []struct {
		limits generated.Limits
		want   error
	}{
		{generated.Limits{Frames: 16, Objects: 4}, generated.ErrStepLimit},
		{generated.Limits{Steps: 256, Objects: 4}, generated.ErrFrameLimit},
		{generated.Limits{Steps: 256, Frames: 16}, generated.ErrObjectLimit},
	}
	for _, test := range limits {
		engine := generated.NewEngine()
		if _, err := engine.Run(context.Background(), test.limits); !errors.Is(err, test.want) {
			t.Fatalf("generated limit %#v = %v, want %v", test.limits, err, test.want)
		}
		if _, err := engine.Run(context.Background(), generated.ProofLimits()); !errors.Is(err, generated.ErrPoisoned) {
			t.Fatalf("generated reuse after %v = %v", test.want, err)
		}
		if err := engine.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGeneratedPreparedRuntimeProjectsAndRestoresDetachedState(t *testing.T) {
	first := generated.NewEngine()
	state, err := first.ApplyRescuedRaise(context.Background(), generated.ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	if want := (generated.State{Value: 105, Trace: 532}); state != want {
		t.Fatalf("first state = %#v, want %#v", state, want)
	}
	if snapshot, err := first.Snapshot(context.Background()); err != nil || snapshot != state {
		t.Fatalf("snapshot = %#v, %v; want %#v", snapshot, err, state)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := generated.NewEngineFromState(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close restored engine: %v", err)
		}
	})
	if value, err := second.Hot(context.Background()); err != nil || value != state.Value {
		t.Fatalf("restored hot = %d, %v; want %d", value, err, state.Value)
	}
	state, err = second.ApplyRescuedRaise(context.Background(), generated.ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	if want := (generated.State{Value: 206, Trace: 532532}); state != want {
		t.Fatalf("second state = %#v, want %#v", state, want)
	}

	if engine, err := generated.NewEngineFromState(generated.State{Trace: -1}); !errors.Is(err, generated.ErrStateLimit) || engine != nil {
		t.Fatalf("invalid restore = %#v, %v", engine, err)
	}
	overflow, err := generated.NewEngineFromState(generated.State{Value: 1<<63 - 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := overflow.ApplyRescuedRaise(context.Background(), generated.ProofLimits()); !errors.Is(err, generated.ErrStateLimit) {
		t.Fatalf("state overflow = %v", err)
	}
	if _, err := overflow.Snapshot(context.Background()); !errors.Is(err, generated.ErrPoisoned) {
		t.Fatalf("reuse after state overflow = %v", err)
	}
	if err := overflow.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedProjectionEnginePreservesBoundedTopologyAndUsesCurrentCode(t *testing.T) {
	ctx := context.Background()
	initial := generated.ProjectionScalars{Root: 40, Other: 40, Tail: 7}
	first, err := generated.NewProjectionEngineFromScalars(ctx, initial, generated.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if call, err := first.Call(ctx); err != nil || call != 41 {
		t.Fatalf("captured-self call = %d, %v; want 41", call, err)
	}
	scalars, err := first.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (generated.ProjectionScalars{Root: 40, Other: 40, Tail: 7}); scalars != want {
		t.Fatalf("snapshot = %#v, want %#v", scalars, want)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := generated.NewProjectionEngineFromScalars(ctx, scalars, generated.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := restored.Close(); err != nil {
			t.Errorf("close restored projection: %v", err)
		}
	})
	if restoredScalars, err := restored.Snapshot(ctx); err != nil || restoredScalars != scalars {
		t.Fatalf("restored snapshot = %#v, %v; want %#v", restoredScalars, err, scalars)
	}
	if call, err := restored.Call(ctx); err != nil || call != 41 {
		t.Fatalf("restored captured-self call = %d, %v; want 41", call, err)
	}

	changedSource := strings.Replace(ProofSource, "mark(6)\n    1", "mark(6)\n    9", 1)
	changed, diagnostics := Compile(changedSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	plan, err := buildEmissionPlan(changed.checked)
	if err != nil {
		t.Fatal(err)
	}
	if plan.InitialLabelReturn != 9 {
		t.Fatalf("changed projection code fact = %d, want 9", plan.InitialLabelReturn)
	}
	set, err := changed.PreparedSource("changed")
	if err != nil {
		t.Fatal(err)
	}
	content := set.Files()[0].Content
	initialReturnReached := false
	for _, line := range strings.Split(content, "\n") {
		if strings.Join(strings.Fields(line), " ") == "rubyInitialLabelReturn int64 = 9" {
			initialReturnReached = true
			break
		}
	}
	if !initialReturnReached {
		t.Fatal("changed checked label return did not reach generated code")
	}
	if !strings.Contains(content, "e.behavior.self.scalar + rubyInitialLabelReturn") {
		t.Fatal("projection call does not consume the checked initial label return")
	}
	if strings.Contains(content, "func NewProjectionEngine(") || strings.Contains(content, "initialProjectionScalars") {
		t.Fatal("generated package retained application-owned initial projection defaults")
	}
}

func TestGeneratedProjectionEngineRejectsOverflowBeforePublication(t *testing.T) {
	scalars := generated.ProjectionScalars{Root: 40, Other: 40, Tail: 7}
	scalars.Other++
	if engine, err := generated.NewProjectionEngineFromScalars(context.Background(), scalars, generated.ProjectionLimits()); engine != nil || !errors.Is(err, generated.ErrStateLimit) {
		t.Fatalf("unequal restore = %#v, %v; want nil state-limit error", engine, err)
	}
	scalars.Root, scalars.Other = math.MaxInt64, math.MaxInt64
	if engine, err := generated.NewProjectionEngineFromScalars(context.Background(), scalars, generated.ProjectionLimits()); engine != nil || !errors.Is(err, generated.ErrStateLimit) {
		t.Fatalf("overflow restore = %#v, %v; want nil state-limit error", engine, err)
	}
	scalars.Root, scalars.Other = math.MaxInt64-1, math.MaxInt64-1
	engine, err := generated.NewProjectionEngineFromScalars(context.Background(), scalars, generated.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if value, err := engine.Call(context.Background()); value != math.MaxInt64 || err != nil {
		t.Fatalf("maximum safe call = %d, %v", value, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedProjectionScalarsExposeNoApplicationTopology(t *testing.T) {
	typeOf := reflect.TypeOf(generated.ProjectionScalars{})
	want := []string{"Root", "Other", "Tail"}
	if typeOf.NumField() != len(want) {
		t.Fatalf("ProjectionScalars has %d fields, want %d", typeOf.NumField(), len(want))
	}
	for index, name := range want {
		field := typeOf.Field(index)
		if field.Name != name || field.Type.Kind() != reflect.Int64 {
			t.Fatalf("ProjectionScalars field %d = %s %s, want %s int64", index, field.Name, field.Type, name)
		}
	}
}

func TestGeneratedProjectionRestoreIsBoundedAndPublishesNoFailure(t *testing.T) {
	scalars := generated.ProjectionScalars{Root: 40, Other: 40, Tail: 7}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	boundaryTests := []struct {
		name   string
		ctx    context.Context
		limits generated.Limits
		want   error
	}{
		{"nil context", nil, generated.ProjectionLimits(), generated.ErrNilContext},
		{"pre-canceled", canceled, generated.ProjectionLimits(), context.Canceled},
	}
	for _, test := range boundaryTests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := generated.NewProjectionEngineFromScalars(test.ctx, scalars, test.limits)
			if engine != nil || !errors.Is(err, test.want) {
				t.Fatalf("restore = %#v, %v; want nil and %v", engine, err, test.want)
			}
		})
	}
	for steps := uint64(0); steps < generated.ProjectionLimits().Steps; steps++ {
		t.Run(fmt.Sprintf("step limit %d", steps), func(t *testing.T) {
			engine, err := generated.NewProjectionEngineFromScalars(context.Background(), scalars, generated.Limits{Steps: steps, Objects: 3})
			if engine != nil || !errors.Is(err, generated.ErrStepLimit) {
				t.Fatalf("restore = %#v, %v; want nil and step-limit error", engine, err)
			}
		})
	}
	for objects := uint64(0); objects < generated.ProjectionLimits().Objects; objects++ {
		t.Run(fmt.Sprintf("object limit %d", objects), func(t *testing.T) {
			engine, err := generated.NewProjectionEngineFromScalars(context.Background(), scalars, generated.Limits{Steps: 5, Objects: objects})
			if engine != nil || !errors.Is(err, generated.ErrObjectLimit) {
				t.Fatalf("restore = %#v, %v; want nil and object-limit error", engine, err)
			}
		})
	}
	for cancelAt := 1; cancelAt <= 9; cancelAt++ {
		t.Run(fmt.Sprintf("cancellation poll %d", cancelAt), func(t *testing.T) {
			ctx := &projectionCancelAfterContext{Context: context.Background(), cancelAt: cancelAt}
			engine, err := generated.NewProjectionEngineFromScalars(ctx, scalars, generated.ProjectionLimits())
			if engine != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("restore = %#v, %v; want nil and canceled", engine, err)
			}
		})
	}
}

func TestGeneratedProjectionAdmissionCloseAndAllocation(t *testing.T) {
	scalars := generated.ProjectionScalars{Root: 40, Other: 40, Tail: 7}
	operations := []struct {
		name string
		run  func(*generated.ProjectionEngine, context.Context) error
	}{
		{"call", func(engine *generated.ProjectionEngine, ctx context.Context) error {
			_, err := engine.Call(ctx)
			return err
		}},
		{"snapshot", func(engine *generated.ProjectionEngine, ctx context.Context) error {
			_, err := engine.Snapshot(ctx)
			return err
		}},
	}
	for _, operation := range operations {
		t.Run("busy close during "+operation.name, func(t *testing.T) {
			engine, err := generated.NewProjectionEngineFromScalars(context.Background(), scalars, generated.ProjectionLimits())
			if err != nil {
				t.Fatal(err)
			}
			blocking := &projectionBlockingContext{
				Context: context.Background(),
				entered: make(chan struct{}),
				release: make(chan struct{}),
			}
			done := make(chan error, 1)
			go func() { done <- operation.run(engine, blocking) }()
			<-blocking.entered
			if err := engine.Close(); !errors.Is(err, generated.ErrBusy) {
				t.Fatalf("busy close = %v", err)
			}
			close(blocking.release)
			if err := <-done; err != nil {
				t.Fatalf("blocked %s = %v", operation.name, err)
			}
			if err := engine.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}

	engine, err := generated.NewProjectionEngineFromScalars(context.Background(), scalars, generated.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}

	failed := false
	if allocations := testing.AllocsPerRun(1000, func() {
		state, snapshotErr := engine.Snapshot(context.Background())
		if snapshotErr != nil {
			failed = true
		}
		projectionStateSink = state
	}); allocations != 0 || failed {
		t.Fatalf("Snapshot allocations = %.2f, failed=%v", allocations, failed)
	}
	if allocations := testing.AllocsPerRun(1000, func() {
		value, callErr := engine.Call(context.Background())
		if callErr != nil {
			failed = true
		}
		projectionCallSink = value
	}); allocations != 0 || failed {
		t.Fatalf("Call allocations = %.2f, failed=%v", allocations, failed)
	}

	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("idempotent close = %v", err)
	}
	if _, err := engine.Call(context.Background()); !errors.Is(err, generated.ErrClosed) {
		t.Fatalf("call after close = %v", err)
	}
	if _, err := engine.Snapshot(context.Background()); !errors.Is(err, generated.ErrClosed) {
		t.Fatalf("snapshot after close = %v", err)
	}
}

type projectionCancelAfterContext struct {
	context.Context
	calls    int
	cancelAt int
}

func (ctx *projectionCancelAfterContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

type projectionBlockingContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (ctx *projectionBlockingContext) Err() error {
	ctx.once.Do(func() { close(ctx.entered) })
	<-ctx.release
	return nil
}

func formatResult(result Result) string {
	legacy := strings.Join([]string{
		formatBool(result.Same),
		formatBool(result.Other),
		formatInt(result.Before),
		formatInt(result.BetaBefore),
		formatInt(result.GammaBefore),
		formatInt(result.AlphaWarm),
		formatInt(result.BetaWarm),
		formatInt(result.After),
		formatInt(result.AlphaAfterHit),
		formatInt(result.BetaAfter),
		formatInt(result.GammaAfter),
		formatInt(result.Saved),
		formatInt(result.PublicBefore),
		formatInt(result.ProtectedRejected),
		formatInt(result.PrivateRejected),
		formatInt(result.PrivateSent),
		formatInt(result.PrivateBeta),
		formatInt(result.PublicRestored),
		formatInt(result.BetaRemoved),
		formatInt(result.Returned),
		formatInt(result.Value),
		formatInt(result.Trace),
		formatInt(result.BetaTrace),
		formatInt(result.GammaTrace),
	}, "|")
	topology := strings.Join([]string{
		formatInt(result.Topology.DeltaBefore),
		formatInt(result.Topology.DeltaIncluded),
		formatInt(result.Topology.BetaRoute),
		formatInt(result.Topology.BetaDefined),
		formatInt(result.Topology.DeltaRedefined),
		formatInt(result.Topology.SavedStable),
		formatInt(result.Topology.Trace),
	}, "|")
	singleton := strings.Join([]string{
		formatInt(result.Singleton.Before),
		formatInt(result.Singleton.Warm),
		formatInt(result.Singleton.PeerBefore),
		formatInt(result.Singleton.After),
		formatInt(result.Singleton.AfterHit),
		formatInt(result.Singleton.PeerAfter),
		formatInt(result.Singleton.Trace),
	}, "|")
	return legacy + "\n" + topology + "\n" + singleton
}

func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func formatInt(value int64) string { return fmt.Sprintf("%d", value) }
