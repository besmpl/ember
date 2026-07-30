package rubyproof_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/rubyproof"
	"github.com/besmpl/ember/preparedsource"
)

func TestRubyPreparedApplicationBuildsOfflineWithoutCompilerOrRuntimeGraph(t *testing.T) {
	program, diagnostics := rubyproof.Compile(rubyproof.ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	set, err := program.PreparedSource("rubygen")
	if err != nil {
		t.Fatal(err)
	}
	files := set.Files()
	if len(files) != 1 {
		t.Fatalf("prepared Ruby file count = %d, want 1", len(files))
	}
	if files[0].Name != "ruby_generated.go" {
		t.Fatalf("prepared Ruby file name = %q, want ruby_generated.go", files[0].Name)
	}
	assertNoCompilerNames(t, "generated source", files[0].Content)
	layout, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "generated/ruby", Set: set}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/ruby-static\n\ngo 1.26\n")
	writeLayout(t, root, layout)
	writeFile(t, filepath.Join(root, "cmd/app/main.go"), `package main
import (
	"context"
	"fmt"
	ruby "example.com/ruby-static/generated/ruby"
)
func main() {
	engine := ruby.NewEngine()
	result, err := engine.Run(context.Background(), ruby.ProofLimits())
	if err != nil { panic(err) }
	stats := engine.Stats()
		fmt.Printf("%t|%t|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d",
			result.Same, result.Other, result.Before, result.BetaBefore, result.GammaBefore, result.AlphaWarm, result.BetaWarm,
			result.After, result.AlphaAfterHit, result.BetaAfter, result.GammaAfter, result.Saved,
			result.PublicBefore, result.ProtectedRejected, result.PrivateRejected, result.PrivateSent, result.PrivateBeta, result.PublicRestored,
			result.BetaRemoved, result.Returned, result.Value, result.Trace, result.BetaTrace, result.GammaTrace,
			result.Topology.DeltaBefore, result.Topology.DeltaIncluded, result.Topology.BetaRoute, result.Topology.BetaDefined,
			result.Topology.DeltaRedefined, result.Topology.SavedStable, result.Topology.Trace,
			result.Singleton.Before, result.Singleton.Warm, result.Singleton.PeerBefore, result.Singleton.After,
			result.Singleton.AfterHit, result.Singleton.PeerAfter, result.Singleton.Trace,
		stats.Hits, stats.ColdAdmissions, stats.StaleMisses, stats.Repairs, stats.UncachedFallbacks, stats.Admissions, stats.Evictions, stats.OccupiedArms)
}
`)
	assertFiles(t, root, []string{"cmd/app/main.go", "generated/ruby/ruby_generated.go", "go.mod"})
	env := cleanGoEnvironment(root)
	if modules := strings.TrimSpace(run(t, root, env, "go", "list", "-m", "all")); modules != "example.com/ruby-static" {
		t.Fatalf("module graph = %q", modules)
	}
	dependencies := commandLines(run(t, root, env, "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	sort.Strings(dependencies)
	wantDependencies := []string{"example.com/ruby-static/cmd/app", "example.com/ruby-static/generated/ruby"}
	if fmt.Sprint(dependencies) != fmt.Sprint(wantDependencies) {
		t.Fatalf("non-standard dependencies = %v, want %v", dependencies, wantDependencies)
	}
	binary := filepath.Join(root, "ruby-static")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	run(t, root, env, "go", "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", binary, "./cmd/app")
	wantOutput := rubyproof.ProofOracleOutput + "|" + rubyproof.ProofC1OracleOutput + "|" + rubyproof.ProofC2OracleOutput + "|9|7|6|6|5|7|0|7"
	if output := strings.TrimSpace(run(t, root, env, binary)); output != wantOutput {
		t.Fatalf("static application output = %q, want %q", output, wantOutput)
	}
	buildInfo := run(t, root, env, "go", "version", "-m", binary)
	if !strings.Contains(buildInfo, "path\texample.com/ruby-static/cmd/app") || strings.Contains(buildInfo, "dep\t") || !strings.Contains(buildInfo, "build\tCGO_ENABLED=0") {
		t.Fatalf("unexpected build info:\n%s", buildInfo)
	}
	assertNoCompilerNames(t, "build info", buildInfo)
	assertNoCompilerNames(t, "binary symbols", run(t, root, env, "go", "tool", "nm", binary))
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Program=%x Set=%x Layout=%x source=%x binary=%x bytes=%d", program.Identity(), set.Digest(), layout.Digest(), sha256.Sum256([]byte(files[0].Content)), sha256.Sum256(binaryBytes), len(binaryBytes))

	if os.Getenv("EMBER_RUBY_CROSS") != "" {
		for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
			parts := strings.Split(target, "/")
			targetEnv := replaceEnvironment(env, []string{"GOOS=" + parts[0], "GOARCH=" + parts[1], "CGO_ENABLED=0"})
			output := filepath.Join(root, "ruby-static-"+parts[0]+"-"+parts[1])
			run(t, root, targetEnv, "go", "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", output, "./cmd/app")
			info := run(t, root, targetEnv, "go", "version", "-m", output)
			if !strings.Contains(info, "build\tCGO_ENABLED=0") || !strings.Contains(info, "build\tGOOS="+parts[0]) || !strings.Contains(info, "build\tGOARCH="+parts[1]) {
				t.Fatalf("%s build info:\n%s", target, info)
			}
		}
	}
}

func TestRubyProofAgainstPinnedLocalCRuby(t *testing.T) {
	if os.Getenv("EMBER_RUBY_ORACLE") == "" {
		t.Skip("set EMBER_RUBY_ORACLE=1 to run the pinned local CRuby differential")
	}
	const (
		rubyPath      = "/usr/bin/ruby"
		wantRuby      = "ruby|2.6.10|universal.arm64e-darwin24"
		wantRubySHA   = "0c6118f2b4a9fe448d51eba2e79c81342ae1e111f015d58dce5c11740f4dfacd"
		wantSourceSHA = "bf355c37216015a49a5b2079e3ff842393755a01fe7cf85d070d1f4fbbf66a52"
		wantOutputSHA = "c09d4fceec6a72f86c690145113737013b4e2ad4a408341bae7506d177ec85ab"
	)
	rubyBytes, err := os.ReadFile(rubyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(rubyBytes)); got != wantRubySHA {
		t.Fatalf("CRuby executable SHA-256 = %s, want %s", got, wantRubySHA)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(rubyproof.ProofSource))); got != wantSourceSHA {
		t.Fatalf("ProofSource SHA-256 = %s, want %s", got, wantSourceSHA)
	}
	rubyEnvironment := cleanRubyEnvironment()
	version := strings.TrimSpace(runRubyTarget39(t, rubyEnvironment, "", rubyPath, "--disable-gems", "-e", `puts [RUBY_ENGINE,RUBY_VERSION,RUBY_PLATFORM].join("|")`))
	if version != wantRuby {
		t.Fatalf("CRuby identity = %q, want %q", version, wantRuby)
	}
	script := rubyproof.ProofSource + "\nputs result.join(\"|\")\nputs c1_result.join(\"|\")\nputs c2_result.join(\"|\")\n"
	wantOutput := rubyproof.ProofOracleOutput + "\n" + rubyproof.ProofC1OracleOutput + "\n" + rubyproof.ProofC2OracleOutput + "\n"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(wantOutput))); got != wantOutputSHA {
		t.Fatalf("three-line output SHA-256 = %s, want %s", got, wantOutputSHA)
	}
	for run := 1; run <= 2; run++ {
		output := runRubyTarget39(t, rubyEnvironment, script, rubyPath, "--disable-gems", "-")
		if output != wantOutput {
			t.Fatalf("CRuby run %d output = %q, want %q", run, output, wantOutput)
		}
	}
	program, diagnostics := rubyproof.Compile(rubyproof.ProofSource)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	canonical, err := program.Run(context.Background(), rubyproof.ProofLimits())
	if err != nil {
		t.Fatal(err)
	}
	gotBase := fmt.Sprintf("%t|%t|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d",
		canonical.Same, canonical.Other, canonical.Before, canonical.BetaBefore, canonical.GammaBefore, canonical.AlphaWarm, canonical.BetaWarm,
		canonical.After, canonical.AlphaAfterHit, canonical.BetaAfter, canonical.GammaAfter, canonical.Saved,
		canonical.PublicBefore, canonical.ProtectedRejected, canonical.PrivateRejected, canonical.PrivateSent, canonical.PrivateBeta, canonical.PublicRestored,
		canonical.BetaRemoved, canonical.Returned, canonical.Value, canonical.Trace, canonical.BetaTrace, canonical.GammaTrace)
	gotTopology := fmt.Sprintf("%d|%d|%d|%d|%d|%d|%d",
		canonical.Topology.DeltaBefore, canonical.Topology.DeltaIncluded, canonical.Topology.BetaRoute,
		canonical.Topology.BetaDefined, canonical.Topology.DeltaRedefined, canonical.Topology.SavedStable, canonical.Topology.Trace)
	gotSingleton := fmt.Sprintf("%d|%d|%d|%d|%d|%d|%d",
		canonical.Singleton.Before, canonical.Singleton.Warm, canonical.Singleton.PeerBefore,
		canonical.Singleton.After, canonical.Singleton.AfterHit, canonical.Singleton.PeerAfter, canonical.Singleton.Trace)
	if gotBase != rubyproof.ProofOracleOutput || gotTopology != rubyproof.ProofC1OracleOutput || gotSingleton != rubyproof.ProofC2OracleOutput {
		t.Fatalf("canonical output = %q / %q / %q, want CRuby %q / %q / %q", gotBase, gotTopology, gotSingleton, rubyproof.ProofOracleOutput, rubyproof.ProofC1OracleOutput, rubyproof.ProofC2OracleOutput)
	}
}

func cleanRubyEnvironment() []string {
	blocked := func(key string) bool {
		switch key {
		case "RUBYOPT", "RUBYLIB", "GEM_HOME", "GEM_PATH", "BUNDLE_GEMFILE", "BUNDLE_PATH", "BUNDLE_BIN_PATH":
			return true
		}
		return strings.HasPrefix(key, "BUNDLE_")
	}
	environment := make([]string, 0, len(os.Environ())+3)
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && !blocked(key) && key != "LC_ALL" && key != "LANG" && key != "TZ" {
			environment = append(environment, item)
		}
	}
	environment = append(environment, "LC_ALL=C", "LANG=C", "TZ=UTC")
	sort.Strings(environment)
	return environment
}

func runRubyTarget39(t *testing.T, environment []string, input, command string, arguments ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, command, arguments...)
	process.Env = environment
	process.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out: %v", command, ctx.Err())
	}
	if err != nil {
		t.Fatalf("%s failed: %v\nstderr:\n%s", command, err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("%s wrote stderr: %q", command, stderr.String())
	}
	if stdout.Len() > 4<<10 {
		t.Fatalf("%s stdout = %d bytes, limit %d", command, stdout.Len(), 4<<10)
	}
	return stdout.String()
}

func writeLayout(t *testing.T, root string, layout preparedsource.Layout) {
	t.Helper()
	for _, mount := range layout.Mounts() {
		for _, file := range mount.Set.Files() {
			writeFile(t, filepath.Join(root, filepath.FromSlash(mount.Path), file.Name), file.Content)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFiles(t *testing.T, root string, want []string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("application files = %v, want %v", got, want)
	}
}

func cleanGoEnvironment(root string) []string {
	return replaceEnvironment(os.Environ(), []string{
		"CGO_ENABLED=0", "GOENV=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local",
		"GOCACHE=" + filepath.Join(root, "go-cache"), "GOMODCACHE=" + filepath.Join(root, "mod-cache"),
	})
}

func replaceEnvironment(base, replacements []string) []string {
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

func run(t *testing.T, directory string, environment []string, command string, arguments ...string) string {
	t.Helper()
	return runWithInput(t, directory, environment, "", command, arguments...)
}

func runWithInput(t *testing.T, directory string, environment []string, input, command string, arguments ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, command, arguments...)
	process.Dir = directory
	process.Env = environment
	if input != "" {
		process.Stdin = strings.NewReader(input)
	}
	output, err := process.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s %v timed out: %s", command, arguments, output)
	}
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", command, arguments, err, output)
	}
	return string(output)
}

func commandLines(output string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func assertNoCompilerNames(t *testing.T, subject, value string) {
	t.Helper()
	for _, forbidden := range []string{
		"github.com/besmpl/ember", "internal/rubyproof", "preparedsource", "preparedworker", "preparedworkerbuild", "rubyproof.", "Backend", "Core",
		"routeOracle", "RouteOracle", "oraclePaths",
	} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("%s retained forbidden tooling/runtime name %q", subject, forbidden)
		}
	}
}
