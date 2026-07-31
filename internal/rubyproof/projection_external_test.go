package rubyproof_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/besmpl/ember/internal/rubyproof"
	"github.com/besmpl/ember/preparedsource"
)

const projectionStaticMain = `package main

import (
	"context"
	"errors"
	"fmt"

	rubyv1 "example.com/ruby-projection-static/generated/rubyv1"
	rubyv2 "example.com/ruby-projection-static/generated/rubyv2"
)

func check(err error) {
	if err != nil { panic(err) }
}

func main() {
	ctx := context.Background()
	first, err := RestoreV1(ctx, InitialCheckpoint(), rubyv1.ProjectionLimits())
	check(err)
	firstCall, err := first.Call(ctx)
	check(err)
	checkpoint, err := SnapshotV1(ctx, first)
	check(err)
	encoded, err := Encode(checkpoint)
	check(err)
	checkpoint, err = Decode(encoded)
	check(err)
	check(first.Close())
	if _, err := first.Call(ctx); !errors.Is(err, rubyv1.ErrClosed) { panic("v1 owner remained live") }

	second, err := RestoreV2(ctx, checkpoint, rubyv2.ProjectionLimits())
	check(err)
	defer func() { check(second.Close()) }()
	secondCall, err := second.Call(ctx)
	check(err)
	roundTrip, err := SnapshotV2(ctx, second)
	check(err)

	aliases := roundTrip.Roots[0] == roundTrip.Roots[1]
	distinct := roundTrip.Roots[0] != roundTrip.Roots[2]
	cycles := roundTrip.Edges[0] == (EdgeRecord{From: 1, Kind: EdgeSelf, To: 1})
	rootScalar, otherScalar := roundTrip.Nodes[0].Scalar, roundTrip.Nodes[1].Scalar
	if !aliases || !distinct || !cycles || rootScalar != 40 || otherScalar != 40 { panic("bad application topology") }
	fmt.Printf("%t|%t|%t|%d|%d|%d|%d|%t",
		aliases, distinct, cycles, rootScalar, otherScalar, firstCall, secondCall, roundTrip == checkpoint)
}
`

func TestRubyProjectionApplicationBuildsTwoGenerationsWithoutCompilerOrSharedRuntime(t *testing.T) {
	const initialLabelV1 = "  def label()\n    mark(6)\n    1\n  end"
	if count := strings.Count(rubyproof.ProofSource, initialLabelV1); count != 1 {
		t.Fatalf("initial-label transform matches %d locations, want 1", count)
	}
	v2Source := strings.Replace(rubyproof.ProofSource, initialLabelV1, "  def label()\n    mark(6)\n    9\n  end", 1)
	v1Program, diagnostics := rubyproof.Compile(rubyproof.ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	v2Program, diagnostics := rubyproof.Compile(v2Source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if v1Program.Identity() == v2Program.Identity() {
		t.Fatal("v1 and v2 source generations have the same Program identity")
	}
	v1Result, err := v1Program.Run(context.Background(), rubyproof.ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	v2Result, err := v2Program.Run(context.Background(), rubyproof.ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	if v1Result.Before != 1 || v2Result.Before != 9 || v1Result.AlphaWarm != 1 || v2Result.AlphaWarm != 9 {
		t.Fatalf("checked generation behavior v1/v2 before=%d/%d warm=%d/%d, want 1/9 and 1/9", v1Result.Before, v2Result.Before, v1Result.AlphaWarm, v2Result.AlphaWarm)
	}
	v2Comparable := v2Result
	v2Comparable.Before = v1Result.Before
	v2Comparable.AlphaWarm = v1Result.AlphaWarm
	v2Comparable.Saved = v1Result.Saved
	v2Comparable.Topology.SavedStable = v1Result.Topology.SavedStable
	if v2Comparable != v1Result {
		t.Fatalf("v2 changed unrelated semantics: v1=%#v v2=%#v", v1Result, v2Result)
	}
	v1Set, err := v1Program.PreparedSource("rubyv1")
	if err != nil {
		t.Fatal(err)
	}
	v2Set, err := v2Program.PreparedSource("rubyv2")
	if err != nil {
		t.Fatal(err)
	}
	if v1Set.Digest() == v2Set.Digest() || v1Set.Files()[0].Content == v2Set.Files()[0].Content {
		t.Fatal("v1 and v2 generated Sets are not independently identified")
	}
	layout, err := preparedsource.NewLayout([]preparedsource.Mount{
		{Path: "generated/rubyv1", Set: v1Set},
		{Path: "generated/rubyv2", Set: v2Set},
	})
	if err != nil {
		t.Fatal(err)
	}

	appRoot, cacheRoot, outputRoot := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(appRoot, "go.mod"), "module example.com/ruby-projection-static\n\ngo 1.26\n")
	writeLayout(t, appRoot, layout)
	writeFile(t, filepath.Join(appRoot, "cmd/app/projection.go"), projectionApplicationSource(t))
	writeFile(t, filepath.Join(appRoot, "cmd/app/main.go"), projectionStaticMain)
	assertFiles(t, appRoot, []string{
		"cmd/app/main.go",
		"cmd/app/projection.go",
		"generated/rubyv1/ruby_generated.go",
		"generated/rubyv2/ruby_generated.go",
		"go.mod",
	})

	env := cleanGoEnvironment(cacheRoot)
	if modules := strings.TrimSpace(run(t, appRoot, env, "go", "list", "-m", "all")); modules != "example.com/ruby-projection-static" {
		t.Fatalf("module graph = %q", modules)
	}
	dependencies := commandLines(run(t, appRoot, env, "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	sort.Strings(dependencies)
	wantDependencies := []string{
		"example.com/ruby-projection-static/cmd/app",
		"example.com/ruby-projection-static/generated/rubyv1",
		"example.com/ruby-projection-static/generated/rubyv2",
	}
	if fmt.Sprint(dependencies) != fmt.Sprint(wantDependencies) {
		t.Fatalf("non-standard dependencies = %v, want %v", dependencies, wantDependencies)
	}
	sourceKinds := commandLines(run(t, appRoot, env, "go", "list", "-f", `{{if not .Standard}}{{.ImportPath}}|C={{join .CFiles ","}}|CGO={{join .CgoFiles ","}}|S={{join .SFiles ","}}{{end}}`, "./..."))
	sort.Strings(sourceKinds)
	wantSourceKinds := []string{
		"example.com/ruby-projection-static/cmd/app|C=|CGO=|S=",
		"example.com/ruby-projection-static/generated/rubyv1|C=|CGO=|S=",
		"example.com/ruby-projection-static/generated/rubyv2|C=|CGO=|S=",
	}
	if fmt.Sprint(sourceKinds) != fmt.Sprint(wantSourceKinds) {
		t.Fatalf("non-standard source kinds = %v, want %v", sourceKinds, wantSourceKinds)
	}

	binary := filepath.Join(outputRoot, "ruby-projection-static")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	run(t, appRoot, env, "go", "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", binary, "./cmd/app")
	if _, err := os.Stat(filepath.Join(appRoot, "go.sum")); !os.IsNotExist(err) {
		t.Fatalf("standalone application created go.sum: %v", err)
	}
	const wantOutput = "true|true|true|40|40|41|49|true"
	if output := strings.TrimSpace(run(t, appRoot, env, binary)); output != wantOutput {
		t.Fatalf("static projection output = %q, want %q", output, wantOutput)
	}
	buildInfo := run(t, appRoot, env, "go", "version", "-m", binary)
	if !strings.Contains(buildInfo, "path\texample.com/ruby-projection-static/cmd/app") ||
		strings.Contains(buildInfo, "dep\t") || !strings.Contains(buildInfo, "build\tCGO_ENABLED=0") ||
		!strings.Contains(buildInfo, "build\tGOOS="+runtime.GOOS) || !strings.Contains(buildInfo, "build\tGOARCH="+runtime.GOARCH) {
		t.Fatalf("unexpected build info:\n%s", buildInfo)
	}
	assertNoCompilerNames(t, "projection build info", buildInfo)
	symbols := run(t, appRoot, env, "go", "tool", "nm", binary)
	assertNoCompilerNames(t, "projection binary symbols", symbols)
	for _, required := range []string{
		"example.com/ruby-projection-static/generated/rubyv1",
		"example.com/ruby-projection-static/generated/rubyv2",
	} {
		if !strings.Contains(symbols, required) {
			t.Fatalf("binary symbols do not retain concrete generated package %q", required)
		}
	}
	// Do not match the generic word "registry": Windows binaries legitimately
	// retain internal/syscall/windows/registry. The package graph and the exact
	// compiler-name checks above prove the tooling exclusion without that false
	// positive.
	for _, forbidden := range []string{"preparedsource", "preparedworker", "plugin", "Backend", "Compiler"} {
		if strings.Contains(symbols, forbidden) {
			t.Fatalf("projection binary retained forbidden shared/tooling symbol %q", forbidden)
		}
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("v1 Program=%x Set=%x source=%x; v2 Program=%x Set=%x source=%x; Layout=%x binary=%x bytes=%d",
		v1Program.Identity(), v1Set.Digest(), sha256.Sum256([]byte(v1Set.Files()[0].Content)),
		v2Program.Identity(), v2Set.Digest(), sha256.Sum256([]byte(v2Set.Files()[0].Content)),
		layout.Digest(), sha256.Sum256(binaryBytes), len(binaryBytes))

	if os.Getenv("EMBER_RUBY_PROJECTION_CROSS") != "" {
		for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
			parts := strings.Split(target, "/")
			targetCache, targetOutput := t.TempDir(), t.TempDir()
			targetEnv := replaceEnvironment(cleanGoEnvironment(targetCache), []string{"GOOS=" + parts[0], "GOARCH=" + parts[1], "CGO_ENABLED=0"})
			output := filepath.Join(targetOutput, "ruby-projection-static")
			if parts[0] == "windows" {
				output += ".exe"
			}
			run(t, appRoot, targetEnv, "go", "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", output, "./cmd/app")
			info := run(t, appRoot, targetEnv, "go", "version", "-m", output)
			if !strings.Contains(info, "path\texample.com/ruby-projection-static/cmd/app") || strings.Contains(info, "dep\t") ||
				!strings.Contains(info, "build\tCGO_ENABLED=0") || !strings.Contains(info, "build\tGOOS="+parts[0]) ||
				!strings.Contains(info, "build\tGOARCH="+parts[1]) {
				t.Fatalf("%s build info:\n%s", target, info)
			}
			t.Logf("cross-built %s with CGO_ENABLED=0", target)
		}
	}
}

func projectionApplicationSource(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "rubyprojectionproof", "projection.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	replacements := [][2]string{
		{"package rubyprojectionproof", "package main"},
		{`"github.com/besmpl/ember/internal/rubyprojectionproof/generated/rubyv2"`, `"example.com/ruby-projection-static/generated/rubyv2"`},
		{`"github.com/besmpl/ember/internal/rubyproof/generated"`, `"example.com/ruby-projection-static/generated/rubyv1"`},
	}
	for _, replacement := range replacements {
		if count := strings.Count(source, replacement[0]); count != 1 {
			t.Fatalf("projection source replacement %q matched %d locations, want 1", replacement[0], count)
		}
		source = strings.Replace(source, replacement[0], replacement[1], 1)
	}
	return source
}
