package mixedcheckpointproof_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/rubyproof"
	"github.com/besmpl/ember/internal/sprigproof"
	"github.com/besmpl/ember/preparedsource"
)

const staticHandlerSource = `package main

import (
	"context"
	"fmt"

	ruby "example.com/mixed-static/generated/ruby"
	sprig "example.com/mixed-static/generated/sprig"
)

const checkpointVersion uint32 = 1

type policy struct {
	ruby       ruby.Limits
	sprigSteps uint64
}

type request struct {
	reading     sprig.Reading
	advanceRuby bool
}

type checkpoint struct {
	version    uint32
	ruby       ruby.State
	sprigTotal int64
}

type handler struct {
	ruby       *ruby.Engine
	checkpoint checkpoint
	policy     policy
}

func proofPolicy() policy {
	return policy{ruby: ruby.ProofLimits(), sprigSteps: 4}
}

func initialCheckpoint() checkpoint {
	return checkpoint{version: checkpointVersion, ruby: ruby.InitialState()}
}

func sprigReading(value, divisor int64) sprig.Reading {
	return sprig.Reading{Value: value, Divisor: divisor}
}

func newHandler(saved checkpoint, limits policy) (*handler, error) {
	if saved.version != checkpointVersion {
		return nil, fmt.Errorf("checkpoint version %d", saved.version)
	}
	engine, err := ruby.NewEngineFromState(saved.ruby)
	if err != nil {
		return nil, fmt.Errorf("restore Ruby: %w", err)
	}
	return &handler{ruby: engine, checkpoint: saved, policy: limits}, nil
}

func (h *handler) apply(ctx context.Context, input request) (checkpoint, error) {
	readings := [...]sprig.Reading{
		{Value: h.checkpoint.sprigTotal, Divisor: 1},
		input.reading,
	}
	sprigResult, err := sprig.Reduce(ctx, readings[:], h.policy.sprigSteps)
	if err != nil {
		return checkpoint{}, err
	}
	if sprigResult.Tag != sprig.ResultOK || len(sprigResult.Rejected) != 0 {
		return checkpoint{}, fmt.Errorf("unexpected Sprig result: %#v", sprigResult)
	}
	rubyState := h.checkpoint.ruby
	if input.advanceRuby {
		rubyState, err = h.ruby.ApplyRescuedRaise(ctx, h.policy.ruby)
	} else {
		_, err = h.ruby.Hot(ctx)
	}
	if err != nil {
		return checkpoint{}, err
	}
	h.checkpoint = checkpoint{
		version: checkpointVersion, ruby: rubyState, sprigTotal: sprigResult.Total,
	}
	return h.checkpoint, nil
}

func (h *handler) close() error {
	if h == nil || h.ruby == nil {
		return nil
	}
	if err := h.ruby.Close(); err != nil {
		return err
	}
	h.ruby = nil
	return nil
}
`

const staticMainSource = `package main

import (
	"context"
	"fmt"
)

func main() {
	ctx := context.Background()
	limits := proofPolicy()
	first, err := newHandler(initialCheckpoint(), limits)
	if err != nil {
		panic(err)
	}
	firstState, err := first.apply(ctx, request{
		reading: sprigReading(10, 2), advanceRuby: true,
	})
	if err != nil {
		panic(err)
	}
	if err := first.close(); err != nil {
		panic(err)
	}
	second, err := newHandler(firstState, limits)
	if err != nil {
		panic(err)
	}
	secondState, err := second.apply(ctx, request{
		reading: sprigReading(8, 2), advanceRuby: true,
	})
	if err != nil {
		panic(err)
	}
	if err := second.close(); err != nil {
		panic(err)
	}
	fmt.Printf("%d|%d|%d|%d|%d|%d",
		firstState.ruby.Value, firstState.ruby.Trace, firstState.sprigTotal,
		secondState.ruby.Value, secondState.ruby.Trace, secondState.sprigTotal,
	)
}
`

func TestStaticApplicationComposesRubyAndSprigAcrossCheckpoint(t *testing.T) {
	rubyProgram, diagnostics := rubyproof.Compile(rubyproof.ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	rubySet, err := rubyProgram.PreparedSource("ruby")
	if err != nil {
		t.Fatal(err)
	}
	sprigProgram, sprigDiagnostics := sprigproof.Compile(sprigproof.ProofSource)
	if len(sprigDiagnostics) != 0 {
		t.Fatal(sprigDiagnostics)
	}
	sprigSet, err := sprigProgram.PreparedSource("sprig")
	if err != nil {
		t.Fatal(err)
	}

	mounts := []preparedsource.Mount{
		{Path: "generated/ruby", Set: rubySet},
		{Path: "generated/sprig", Set: sprigSet},
	}
	layout, err := preparedsource.NewLayout(mounts)
	if err != nil {
		t.Fatal(err)
	}
	permuted, err := preparedsource.NewLayout([]preparedsource.Mount{mounts[1], mounts[0]})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Digest() != permuted.Digest() {
		t.Fatalf("Layout digest changed under mount permutation: %x / %x", layout.Digest(), permuted.Digest())
	}

	temp := t.TempDir()
	application := filepath.Join(temp, "application")
	writeStaticFile(t, filepath.Join(application, "go.mod"), "module example.com/mixed-static\n\ngo 1.26\n")
	materializeStaticLayout(t, application, layout)
	writeStaticFile(t, filepath.Join(application, "cmd/app/handler.go"), staticHandlerSource)
	writeStaticFile(t, filepath.Join(application, "cmd/app/main.go"), staticMainSource)
	assertStaticFiles(t, application, []string{
		"cmd/app/handler.go",
		"cmd/app/main.go",
		"generated/ruby/ruby_generated.go",
		"generated/sprig/reduce_generated.go",
		"go.mod",
	})
	assertStaticImports(t, application)

	environment := cleanStaticEnvironment(temp)
	modules := strings.TrimSpace(runCommand(t, application, environment, "go", "list", "-mod=readonly", "-m", "all"))
	if modules != "example.com/mixed-static" {
		t.Fatalf("module graph = %q", modules)
	}
	dependencies := commandLines(runCommand(t, application, environment, "go", "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}}", "./..."))
	assertNoForbiddenDependencies(t, dependencies)
	nonstandard := commandLines(runCommand(t, application, environment, "go", "list", "-mod=readonly", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	sort.Strings(nonstandard)
	wantDependencies := []string{
		"example.com/mixed-static/cmd/app",
		"example.com/mixed-static/generated/ruby",
		"example.com/mixed-static/generated/sprig",
	}
	if fmt.Sprint(nonstandard) != fmt.Sprint(wantDependencies) {
		t.Fatalf("non-standard dependencies = %v, want %v", nonstandard, wantDependencies)
	}

	binary := filepath.Join(temp, "mixed-static")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	buildStaticApplication(t, application, environment, binary)
	if output := strings.TrimSpace(runCommand(t, application, environment, binary)); output != "105|532|5|206|532532|9" {
		t.Fatalf("static application output = %q", output)
	}
	buildInfo := runCommand(t, application, environment, "go", "version", "-m", binary)
	assertStaticBuildInfo(t, buildInfo, runtime.GOOS, runtime.GOARCH)
	assertNoForbiddenSymbols(t, runCommand(t, application, environment, "go", "tool", "nm", binary))

	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ruby_program=%x ruby_set=%x ruby_source=%x sprig_set=%x sprig_source=%x layout=%x handler=%x main=%x binary=%x binary_bytes=%d",
		rubyProgram.Identity(), rubySet.Digest(), sha256.Sum256([]byte(rubySet.Files()[0].Content)),
		sprigSet.Digest(), sha256.Sum256([]byte(sprigSet.Files()[0].Content)), layout.Digest(),
		sha256.Sum256([]byte(staticHandlerSource)), sha256.Sum256([]byte(staticMainSource)),
		sha256.Sum256(binaryBytes), len(binaryBytes),
	)

	if os.Getenv("EMBER_MIXED_CHECKPOINT_CROSS") == "1" {
		for _, target := range []string{
			"darwin/arm64", "darwin/amd64",
			"linux/arm64", "linux/amd64",
			"windows/arm64", "windows/amd64",
		} {
			goos, goarch, _ := strings.Cut(target, "/")
			targetEnvironment := replaceStaticEnvironment(environment, []string{"GOOS=" + goos, "GOARCH=" + goarch, "CGO_ENABLED=0"})
			targetBinary := filepath.Join(temp, "mixed-static-"+goos+"-"+goarch)
			if goos == "windows" {
				targetBinary += ".exe"
			}
			buildStaticApplication(t, application, targetEnvironment, targetBinary)
			assertStaticBuildInfo(t, runCommand(t, application, targetEnvironment, "go", "version", "-m", targetBinary), goos, goarch)
		}
	}
}

func materializeStaticLayout(t *testing.T, root string, layout preparedsource.Layout) {
	t.Helper()
	for _, mount := range layout.Mounts() {
		for _, file := range mount.Set.Files() {
			writeStaticFile(t, filepath.Join(root, filepath.FromSlash(mount.Path), file.Name), file.Content)
		}
	}
}

func writeStaticFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertStaticFiles(t *testing.T, root string, want []string) {
	t.Helper()
	var got []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("application files = %v, want %v", got, want)
	}
}

func assertStaticImports(t *testing.T, root string) {
	t.Helper()
	allowed := map[string]bool{
		"example.com/mixed-static/generated/ruby":  true,
		"example.com/mixed-static/generated/sprig": true,
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if strings.Contains(importPath, ".") && !allowed[importPath] {
				return fmt.Errorf("%s imports non-generated application dependency %q", path, importPath)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func cleanStaticEnvironment(root string) []string {
	return replaceStaticEnvironment(os.Environ(), []string{
		"CGO_ENABLED=0",
		"GOENV=off",
		"GOFLAGS=",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOCACHE=" + filepath.Join(root, "go-cache"),
		"GOMODCACHE=" + filepath.Join(root, "mod-cache"),
	})
}

func replaceStaticEnvironment(base, replacements []string) []string {
	values := make(map[string]string, len(base)+len(replacements))
	for _, item := range append(append([]string(nil), base...), replacements...) {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			values[key] = item
		}
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func buildStaticApplication(t *testing.T, root string, environment []string, binary string) {
	t.Helper()
	runCommand(t, root, environment, "go", "build",
		"-mod=readonly", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", binary, "./cmd/app")
}

func runCommand(t *testing.T, root string, environment []string, name string, arguments ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = root
	command.Env = environment
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s %v timed out: %v\n%s", name, arguments, ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, arguments, err, output)
	}
	return string(output)
}

func commandLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func assertNoForbiddenDependencies(t *testing.T, dependencies []string) {
	t.Helper()
	for _, dependency := range dependencies {
		forbidden := dependency == "plugin" || dependency == "runtime/cgo" ||
			forbiddenPackage(dependency, "github.com/besmpl/ember") ||
			forbiddenPackage(dependency, "github.com/besmpl/ember/internal/mixedcheckpointproof") ||
			forbiddenPackage(dependency, "github.com/besmpl/ember/internal/rubyproof") ||
			forbiddenPackage(dependency, "github.com/besmpl/ember/internal/sprigproof") ||
			forbiddenPackage(dependency, "github.com/besmpl/ember/internal/preparedworker") ||
			forbiddenPackage(dependency, "github.com/besmpl/ember/preparedsource") ||
			forbiddenPackage(dependency, "github.com/besmpl/ember/preparedworker") ||
			forbiddenPackage(dependency, "github.com/besmpl/ember/preparedworkerbuild")
		if forbidden {
			t.Fatalf("application retained forbidden dependency %q", dependency)
		}
	}
}

func forbiddenPackage(dependency, path string) bool {
	return dependency == path || strings.HasPrefix(dependency, path+"/")
}

func assertStaticBuildInfo(t *testing.T, buildInfo, goos, goarch string) {
	t.Helper()
	if !strings.Contains(buildInfo, "path\texample.com/mixed-static/cmd/app") {
		t.Fatalf("build info omits application path:\n%s", buildInfo)
	}
	for _, line := range strings.Split(buildInfo, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "dep\t") {
			t.Fatalf("build info retained a module dependency:\n%s", buildInfo)
		}
	}
	for _, setting := range []string{"CGO_ENABLED=0", "GOOS=" + goos, "GOARCH=" + goarch} {
		if !strings.Contains(buildInfo, "build\t"+setting) {
			t.Fatalf("build info omits %s:\n%s", setting, buildInfo)
		}
	}
}

func assertNoForbiddenSymbols(t *testing.T, symbols string) {
	t.Helper()
	for _, forbidden := range []string{
		"github.com/besmpl/ember.",
		"github.com/besmpl/ember/internal/mixedcheckpointproof.",
		"github.com/besmpl/ember/internal/preparedworker",
		"github.com/besmpl/ember/internal/rubyproof.",
		"github.com/besmpl/ember/internal/sprigproof.",
		"github.com/besmpl/ember/preparedsource.",
		"github.com/besmpl/ember/preparedworker.",
		"github.com/besmpl/ember/preparedworkerbuild.",
	} {
		if strings.Contains(symbols, forbidden) {
			t.Fatalf("binary retained forbidden compiler/delivery symbol prefix %q", forbidden)
		}
	}
}
