package rubyproof

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/besmpl/ember/internal/rubyproof/h1bgenerated"
	"github.com/besmpl/ember/preparedsource"
)

const (
	target47StaticSourceBytes  = 3_901
	target47StaticSourceLF     = 228
	target47StaticSourceSHA256 = "bf355c37216015a49a5b2079e3ff842393755a01fe7cf85d070d1f4fbbf66a52"
	target47StaticGoBytes      = 79_361
	target47StaticGoLF         = 2_344
	target47StaticGoSHA256     = "b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46"
)

func TestH1bFrozenStaticProductIsByteAndStructurallyExact(t *testing.T) {
	if len(ProofSource) != target47StaticSourceBytes || strings.Count(ProofSource, "\n") != target47StaticSourceLF {
		t.Fatalf("ProofSource = %d bytes/%d LF, want %d/%d", len(ProofSource), strings.Count(ProofSource, "\n"), target47StaticSourceBytes, target47StaticSourceLF)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(ProofSource))); got != target47StaticSourceSHA256 {
		t.Fatalf("ProofSource SHA-256 = %s, want %s", got, target47StaticSourceSHA256)
	}

	contents, err := os.ReadFile("generated/ruby_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != target47StaticGoBytes || strings.Count(string(contents), "\n") != target47StaticGoLF {
		t.Fatalf("static generated product = %d bytes/%d LF, want %d/%d", len(contents), strings.Count(string(contents), "\n"), target47StaticGoBytes, target47StaticGoLF)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got != target47StaticGoSHA256 {
		t.Fatalf("static generated product SHA-256 = %s, want %s", got, target47StaticGoSHA256)
	}

	file, err := goparser.ParseFile(gotoken.NewFileSet(), "generated/ruby_generated.go", contents, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantFunctions := map[string]bool{"readSingletonAfterStep": false, "labelSingleton": false}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
			continue
		}
		if _, tracked := wantFunctions[function.Name.Name]; !tracked {
			continue
		}
		wantFunctions[function.Name.Name] = true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			name := strings.ToLower(identifier.Name)
			for _, forbidden := range []string{"h1b", "sidecar", "environmentref", "markwork", "collect"} {
				if strings.Contains(name, forbidden) {
					t.Errorf("frozen static function %s contains H1b mechanism %q", function.Name.Name, identifier.Name)
				}
			}
			return true
		})
	}
	for name, found := range wantFunctions {
		if !found {
			t.Errorf("frozen static function %s is absent", name)
		}
	}
}

func TestH1bGeneratedSetIsSingleDependencyFreePureGoProduct(t *testing.T) {
	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 {
		t.Fatalf("CompileH1b(H1bSource): %v", diagnostics)
	}
	set, err := program.PreparedSource("h1bgenerated")
	if err != nil {
		t.Fatal(err)
	}
	files := set.Files()
	if set.PackageName() != "h1bgenerated" || len(files) != 1 || files[0].Name != "ruby_h1b_generated.go" || files[0].Kind != preparedsource.GoFile {
		t.Fatalf("H1b prepared set = package %q files %#v", set.PackageName(), files)
	}

	file, err := goparser.ParseFile(gotoken.NewFileSet(), files[0].Name, files[0].Content, 0)
	if err != nil {
		t.Fatal(err)
	}
	if file.Name.Name != "h1bgenerated" {
		t.Fatalf("generated package = %q, want h1bgenerated", file.Name.Name)
	}
	if len(file.Imports) != 1 {
		t.Fatalf("generated imports = %v, want only context", file.Imports)
	}
	path, err := strconv.Unquote(file.Imports[0].Path.Value)
	if err != nil || path != "context" || file.Imports[0].Name != nil {
		t.Fatalf("generated import = name %v path %q error %v, want context", file.Imports[0].Name, path, err)
	}
	wantCapacities := map[string]int{
		"objectCapacity":      4,
		"environmentCapacity": 2,
		"rootCapacity":        8,
		"markCapacity":        6,
	}
	gotCapacities := generatedIntegerConstants(file, wantCapacities)
	for name, want := range wantCapacities {
		if got, ok := gotCapacities[name]; !ok || got != want {
			t.Errorf("generated %s = %d/present=%v, want %d", name, got, ok, want)
		}
	}

	wantArrays := map[string]string{
		"objects":      "objectCapacity",
		"environments": "environmentCapacity",
		"roots":        "rootCapacity",
		"marks":        "markCapacity",
	}
	foundArrays := make(map[string]bool, len(wantArrays))
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.MapType:
			t.Errorf("generated product contains a map at token %d", node.Pos())
		case *ast.ChanType:
			t.Errorf("generated product contains a channel at token %d", node.Pos())
		case *ast.GoStmt:
			t.Errorf("generated product starts a goroutine at token %d", node.Pos())
		case *ast.TypeSpec:
			if node.Name.Name != "arena" {
				break
			}
			structure, ok := node.Type.(*ast.StructType)
			if !ok {
				t.Errorf("generated arena is %T, want struct", node.Type)
				break
			}
			for _, field := range structure.Fields.List {
				if len(field.Names) != 1 {
					continue
				}
				wantLength, tracked := wantArrays[field.Names[0].Name]
				if !tracked {
					continue
				}
				array, ok := field.Type.(*ast.ArrayType)
				length, lengthOK := arrayLengthIdentifier(array)
				if !ok || !lengthOK || length != wantLength {
					t.Errorf("generated arena field %s = %T/%q, want [%s]", field.Names[0].Name, field.Type, length, wantLength)
				} else {
					foundArrays[field.Names[0].Name] = true
				}
			}
		}
		return true
	})
	for name := range wantArrays {
		if !foundArrays[name] {
			t.Errorf("generated arena has no fixed %s array", name)
		}
	}
}

func TestH1bGeneratedFixtureIsFresh(t *testing.T) {
	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 {
		t.Fatalf("CompileH1b(H1bSource): %v", diagnostics)
	}
	set, err := program.PreparedSource("h1bgenerated")
	if err != nil {
		t.Fatal(err)
	}
	files := set.Files()
	if len(files) != 1 || files[0].Name != "ruby_h1b_generated.go" || files[0].Kind != preparedsource.GoFile {
		t.Fatalf("H1b prepared files = %#v, want one ruby_h1b_generated.go", files)
	}
	fixture := filepath.Join("h1bgenerated", files[0].Name)
	if os.Getenv("EMBER_UPDATE_RUBY_H1B_FIXTURE") != "" {
		if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture, []byte(files[0].Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read H1b generated fixture: %v; run EMBER_UPDATE_RUBY_H1B_FIXTURE=1 go test -count=1 -run '^TestH1bGeneratedFixtureIsFresh$' ./internal/rubyproof", err)
	}
	if string(got) != files[0].Content {
		t.Fatal("generated H1b fixture is stale; run EMBER_UPDATE_RUBY_H1B_FIXTURE=1 go test -count=1 -run '^TestH1bGeneratedFixtureIsFresh$' ./internal/rubyproof")
	}
}

func TestH1bGeneratedMatchesCanonicalResultAndReclamation(t *testing.T) {
	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 {
		t.Fatalf("CompileH1b(H1bSource): %v", diagnostics)
	}
	canonical, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	generated := h1bgenerated.NewEngine()
	t.Cleanup(func() {
		if err := canonical.Close(); err != nil {
			t.Errorf("close canonical H1b runtime: %v", err)
		}
		if err := generated.Close(); err != nil {
			t.Errorf("close generated H1b engine: %v", err)
		}
	})

	for run := 1; run <= 2; run++ {
		want, err := canonical.Run(context.Background(), H1bProofLimits())
		if err != nil {
			t.Fatalf("canonical H1b run %d: %v", run, err)
		}
		got, err := generated.Run(context.Background(), h1bgenerated.ProofLimits())
		if err != nil {
			t.Fatalf("generated H1b run %d: %v", run, err)
		}
		assertTarget47H1bResult(t, want)
		assertTarget47H1bFinalStats(t, want.Stats)
		assertTarget47GeneratedParity(t, got, want)
	}
}

func TestH1bGeneratedRunAllocatesNoGoHeapAfterConstruction(t *testing.T) {
	engine := h1bgenerated.NewEngine()
	defer func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close generated H1b engine: %v", err)
		}
	}()
	ctx, limits := context.Background(), h1bgenerated.ProofLimits()
	var got h1bgenerated.Result
	var runErr error
	allocations := testing.AllocsPerRun(100, func() {
		if runErr != nil {
			return
		}
		got, runErr = engine.Run(ctx, limits)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	assertTarget47GeneratedResult(t, got)
	if allocations != 0 {
		t.Fatalf("generated H1b Run allocations after engine construction = %g, want 0", allocations)
	}
}

func generatedIntegerConstants(file *ast.File, wanted map[string]int) map[string]int {
	result := make(map[string]int, len(wanted))
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != gotoken.CONST {
			continue
		}
		for _, specification := range group.Specs {
			values, ok := specification.(*ast.ValueSpec)
			if !ok || len(values.Names) != len(values.Values) {
				continue
			}
			for index, name := range values.Names {
				if _, tracked := wanted[name.Name]; !tracked {
					continue
				}
				literal, ok := values.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != gotoken.INT {
					continue
				}
				value, err := strconv.Atoi(literal.Value)
				if err == nil {
					result[name.Name] = value
				}
			}
		}
	}
	return result
}

func arrayLengthIdentifier(array *ast.ArrayType) (string, bool) {
	if array == nil {
		return "", false
	}
	identifier, ok := array.Len.(*ast.Ident)
	if !ok {
		return "", false
	}
	return identifier.Name, true
}

func TestH1bCanonicalResultAndReclamationObserver(t *testing.T) {
	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 {
		t.Fatalf("CompileH1b(H1bSource): %v", diagnostics)
	}
	runtime, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close H1b runtime: %v", err)
		}
	})
	got, err := runtime.Run(context.Background(), H1bProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertTarget47H1bResult(t, got)
	assertTarget47H1bFinalStats(t, got.Stats)
}

func TestH1bCanonicalRunAllocatesNoGoHeapAfterConstruction(t *testing.T) {
	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 {
		t.Fatalf("CompileH1b(H1bSource): %v", diagnostics)
	}
	runtime, err := program.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close H1b runtime: %v", err)
		}
	}()
	ctx, limits := context.Background(), H1bProofLimits()
	var got H1bResult
	var runErr error
	allocations := testing.AllocsPerRun(100, func() {
		if runErr != nil {
			return
		}
		got, runErr = runtime.Run(ctx, limits)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	assertTarget47H1bResult(t, got)
	assertTarget47H1bFinalStats(t, got.Stats)
	if allocations != 0 {
		t.Fatalf("H1b canonical Run allocations after owner construction = %g, want 0", allocations)
	}
}

func assertTarget47H1bResult(t testing.TB, got H1bResult) {
	t.Helper()
	if got.Before != 7 || got.Warm != 7 || got.PeerBefore != 7 || got.First != 43 || got.Second != 44 ||
		got.PeerAfter != 7 || got.PeerAfterReceiverCollection != 7 {
		t.Fatalf("H1b detached scalar result = %#v, want 7/7/7/43/44/7/7", got)
	}
	formatted := fmt.Sprintf("%d|%d|%d|%d|%d|%d|%d", got.Before, got.Warm, got.PeerBefore, got.First, got.Second, got.PeerAfter, got.PeerAfterReceiverCollection)
	if formatted != H1bOracleOutput {
		t.Fatalf("H1b canonical output = %q, want %q", formatted, H1bOracleOutput)
	}
}

func assertTarget47H1bFinalStats(t testing.TB, stats H1bStats) {
	t.Helper()
	if stats.ObjectAllocations != 2 || stats.ObjectReclaims != 2 ||
		stats.EnvironmentAllocations != 1 || stats.EnvironmentReclaims != 1 ||
		stats.Collections != 3 || stats.TotalMarkWork != 4 || stats.LastMarkWork != 0 ||
		stats.LiveObjects != 0 || stats.LiveEnvironments != 0 || stats.Roots != 0 {
		t.Fatalf("H1b final stats = %#v; want allocations/reclaims object 2/2 environment 1/1, collections/work 3/4, and no live records or roots", stats)
	}
	if stats.MarkHighWater != 3 {
		t.Fatalf("H1b per-collection mark-work high-water = %d, want 3", stats.MarkHighWater)
	}
}

func assertTarget47GeneratedResult(t testing.TB, got h1bgenerated.Result) {
	t.Helper()
	if got.Before != 7 || got.Warm != 7 || got.PeerBefore != 7 || got.First != 43 || got.Second != 44 ||
		got.PeerAfter != 7 || got.PeerAfterReceiverCollection != 7 {
		t.Fatalf("generated H1b detached scalar result = %#v, want 7/7/7/43/44/7/7", got)
	}
	if got.Stats.ObjectAllocations != 2 || got.Stats.ObjectReclaims != 2 ||
		got.Stats.EnvironmentAllocations != 1 || got.Stats.EnvironmentReclaims != 1 ||
		got.Stats.Collections != 3 || got.Stats.TotalMarkWork != 4 || got.Stats.LastMarkWork != 0 ||
		got.Stats.MarkHighWater != 3 ||
		got.Stats.LiveObjects != 0 || got.Stats.LiveEnvironments != 0 || got.Stats.Roots != 0 {
		t.Fatalf("generated H1b final stats = %#v", got.Stats)
	}
}

func assertTarget47GeneratedParity(t testing.TB, got h1bgenerated.Result, want H1bResult) {
	t.Helper()
	assertTarget47GeneratedResult(t, got)
	if got.Before != want.Before || got.Warm != want.Warm || got.PeerBefore != want.PeerBefore ||
		got.First != want.First || got.Second != want.Second || got.PeerAfter != want.PeerAfter ||
		got.PeerAfterReceiverCollection != want.PeerAfterReceiverCollection {
		t.Fatalf("generated H1b scalar result = %#v, canonical %#v", got, want)
	}
	stats := got.Stats
	canonical := want.Stats
	if stats.ObjectAllocations != canonical.ObjectAllocations ||
		stats.EnvironmentAllocations != canonical.EnvironmentAllocations ||
		stats.ObjectReclaims != canonical.ObjectReclaims ||
		stats.EnvironmentReclaims != canonical.EnvironmentReclaims ||
		stats.Collections != canonical.Collections || stats.TotalMarkWork != canonical.TotalMarkWork ||
		stats.LastMarkWork != canonical.LastMarkWork || stats.MarkHighWater != canonical.MarkHighWater ||
		stats.LiveObjects != canonical.LiveObjects || stats.LiveEnvironments != canonical.LiveEnvironments ||
		stats.Roots != canonical.Roots {
		t.Fatalf("generated H1b stats = %#v, canonical %#v", stats, canonical)
	}
}

// BenchmarkH1bCanonicalProduct is a diagnostic full-product benchmark, not a
// warmed-hit or collector acceptance comparator. Retained Target47 captures
// must use independently equivalent generated hot-hit and collection lanes.
func BenchmarkH1bCanonicalProduct(b *testing.B) {
	program, diagnostics := CompileH1b(H1bSource)
	if len(diagnostics) != 0 {
		b.Fatal(diagnostics)
	}
	runtime, err := program.NewRuntime()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			b.Error(err)
		}
	})
	ctx, limits := context.Background(), H1bProofLimits()
	got, err := runtime.Run(ctx, limits)
	if err != nil {
		b.Fatal(err)
	}
	assertTarget47H1bResult(b, got)
	assertTarget47H1bFinalStats(b, got.Stats)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err = runtime.Run(ctx, limits)
		if err != nil {
			b.Fatal(err)
		}
	}
}
