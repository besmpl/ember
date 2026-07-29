package preparedsource_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestExternalCompilerBuildsFromCheckedSourceThenDisappears(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	fixtureDir := filepath.Join(filepath.Dir(sourceFile), "testdata", "externalcompiler")
	repositoryDir := filepath.Dir(filepath.Dir(sourceFile))
	temp := t.TempDir()
	compilerDir := filepath.Join(temp, "compiler")
	copyTree(t, fixtureDir, compilerDir)
	compilerModule := "module example.com/ember-external-compiler\n\ngo 1.26\n\nrequire github.com/besmpl/ember v0.0.0\n\nreplace github.com/besmpl/ember => " + filepath.ToSlash(repositoryDir) + "\n"
	if err := os.WriteFile(filepath.Join(compilerDir, "go.mod"), []byte(compilerModule), 0o644); err != nil {
		t.Fatal(err)
	}
	env := testEnvironment(
		"CGO_ENABLED=0",
		"GOENV=off",
		"GOFLAGS=",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(temp, "compiler-go-cache"),
		"GOMODCACHE="+filepath.Join(temp, "compiler-mod-cache"),
	)

	if module := strings.TrimSpace(runGo(t, compilerDir, env, "list", "-m", "-f", "{{.Path}}")); module != "example.com/ember-external-compiler" {
		t.Fatalf("compiler module = %q", module)
	}
	assertGoImports(t, compilerDir, func(path string) bool {
		return path == "github.com/besmpl/ember/preparedsource" ||
			path == "example.com/ember-external-compiler" ||
			!strings.Contains(path, ".")
	})
	compilerDeps := lines(runGo(t, compilerDir, env, "list", "-deps", "./..."))
	if !containsLine(compilerDeps, "github.com/besmpl/ember/preparedsource") {
		t.Fatal("external compiler does not depend on preparedsource")
	}
	for _, dependency := range compilerDeps {
		if dependency == "github.com/besmpl/ember" ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/internal/") ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/preparedworker") ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/preparedworkerbuild") {
			t.Fatalf("external compiler has forbidden Ember dependency %q", dependency)
		}
	}
	compilerNonstandard := lines(runGo(t, compilerDir, env, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	assertExactLines(t, "compiler non-standard dependencies", compilerNonstandard, []string{
		"example.com/ember-external-compiler",
		"example.com/ember-external-compiler/cmd/appmaterialize",
		"github.com/besmpl/ember/preparedsource",
	})
	runGo(t, compilerDir, env, "test", "-count=1", "./...")
	seedSource, err := os.ReadFile(filepath.Join(compilerDir, "program.seed"))
	if err != nil {
		t.Fatal(err)
	}

	applicationDir := filepath.Join(temp, "application")
	secondApplicationDir := filepath.Join(temp, "application-repeat")
	identities := runGo(t, compilerDir, env, "run", "./cmd/appmaterialize", "-out", applicationDir)
	repeatedIdentities := runGo(t, compilerDir, env, "run", "./cmd/appmaterialize", "-out", secondApplicationDir)
	if identities != repeatedIdentities {
		t.Fatalf("repeated compilation identities differ:\n%s\n%s", identities, repeatedIdentities)
	}
	wantIdentities := map[string]string{
		"program": "b5f1ada51772f913502ae4a0cb40790dcfb4f98f620412f33e35df86b5b543f9",
		"set":     "ab879222f8fb1795de30d5c2c958e3a1253390ee2ff314ec54c8c266ae1c1a67",
		"layout":  "8e428a43250dfc85069c04d2f26bb0bed24447ba01727500990764fbf4f52b69",
	}
	if got := identityMap(t, identities); !reflect.DeepEqual(got, wantIdentities) {
		t.Fatalf("compiler identities = %v, want %v", got, wantIdentities)
	}
	firstSnapshot := directorySnapshot(t, applicationDir)
	secondSnapshot := directorySnapshot(t, secondApplicationDir)
	if !reflect.DeepEqual(firstSnapshot, secondSnapshot) {
		t.Fatal("repeated compilation changed application bytes")
	}
	runCommandMustFail(t, compilerDir, env, "go", "run", "./cmd/appmaterialize", "-out", applicationDir)
	invalidSource := filepath.Join(compilerDir, "invalid.seed")
	if err := os.WriteFile(invalidSource, []byte("package score\nfunc Reduce(value i64, divisor i64) i64 { return missing }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCommandMustFail(t, compilerDir, env, "go", "run", "./cmd/appmaterialize", "-source", invalidSource, "-out", filepath.Join(temp, "invalid-application"))

	if err := os.RemoveAll(compilerDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(compilerDir); !os.IsNotExist(err) {
		t.Fatalf("compiler tooling still exists before application proof: %v", err)
	}
	applicationEnv := testEnvironment(
		"CGO_ENABLED=0",
		"GOENV=off",
		"GOFLAGS=",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(temp, "application-go-cache"),
		"GOMODCACHE="+filepath.Join(temp, "application-mod-cache"),
	)
	assertExactLines(t, "application files", sortedKeys(firstSnapshot), []string{
		"cmd/app/main.go",
		"cmd/app/main_test.go",
		"generated/score/reduce_generated.go",
		"go.mod",
	})
	assertGoImports(t, applicationDir, func(path string) bool {
		return path == "example.com/external-compiled-application/generated/score" || !strings.Contains(path, ".")
	})
	if goMod := firstSnapshot["go.mod"]; goMod != "module example.com/external-compiled-application\n\ngo 1.26\n" {
		t.Fatalf("application go.mod contains delivery dependencies:\n%s", goMod)
	}
	if modules := strings.TrimSpace(runGo(t, applicationDir, applicationEnv, "list", "-m", "all")); modules != "example.com/external-compiled-application" {
		t.Fatalf("application module graph retained an unexpected module:\n%s", modules)
	}
	applicationDeps := lines(runGo(t, applicationDir, applicationEnv, "list", "-deps", "./..."))
	for _, dependency := range applicationDeps {
		if strings.Contains(dependency, "github.com/besmpl/ember") ||
			strings.Contains(dependency, "ember-external-compiler") ||
			strings.Contains(dependency, "preparedsource") ||
			strings.Contains(dependency, "preparedworker") {
			t.Fatalf("compiled application retained tooling dependency %q", dependency)
		}
	}
	applicationNonstandard := lines(runGo(t, applicationDir, applicationEnv, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	assertExactLines(t, "application non-standard dependencies", applicationNonstandard, []string{
		"example.com/external-compiled-application/cmd/app",
		"example.com/external-compiled-application/generated/score",
	})
	runGo(t, applicationDir, applicationEnv, "test", "-count=1", "./...")

	binary := filepath.Join(temp, "external-compiled-application")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	runGo(t, applicationDir, applicationEnv, "build", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", binary, "./cmd/app")
	buildInfo := runCommand(t, applicationDir, applicationEnv, "go", "version", "-m", binary)
	assertStaticBuildInfo(t, buildInfo)
	assertNoDeliveryNames(t, "binary symbols", runGo(t, applicationDir, applicationEnv, "tool", "nm", binary))
	if output := strings.TrimSpace(runCommand(t, applicationDir, applicationEnv, binary)); output != "6|1|2|1|true|true" {
		t.Fatalf("compiled application output = %q", output)
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("seed_source_sha256=%x generated_source_sha256=%x generated_bytes=%d binary_sha256=%x binary_bytes=%d", sha256.Sum256(seedSource), sha256.Sum256([]byte(firstSnapshot["generated/score/reduce_generated.go"])), len(firstSnapshot["generated/score/reduce_generated.go"]), sha256.Sum256(binaryBytes), len(binaryBytes))

	if os.Getenv("EMBER_EXTERNAL_COMPILER_PERF") != "" {
		benchmarks := runGo(t, applicationDir, applicationEnv, "test", "-run", "^$", "-bench", "^Benchmark(Generated|Handwritten)$", "-benchtime=200ms", "-count=7", "./cmd/app")
		generated := benchmarkMedian(t, benchmarks, "BenchmarkGenerated-")
		handwritten := benchmarkMedian(t, benchmarks, "BenchmarkHandwritten-")
		ratio := generated / handwritten
		t.Logf("external compiler steady median generated=%.3f ns/op handwritten=%.3f ns/op ratio=%.3fx\n%s", generated, handwritten, ratio, benchmarks)
		if ratio > 1.10 {
			t.Fatalf("generated steady ratio %.3fx exceeds 1.10x", ratio)
		}
	}
	if os.Getenv("EMBER_EXTERNAL_COMPILER_CROSS") != "" {
		for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
			parts := strings.Split(target, "/")
			targetEnv := testEnvironment(append(applicationEnv, "GOOS="+parts[0], "GOARCH="+parts[1])...)
			targetBinary := filepath.Join(temp, "external-compiled-app-"+parts[0]+"-"+parts[1])
			runGo(t, applicationDir, targetEnv, "build", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", targetBinary, "./cmd/app")
			assertStaticBuildInfo(t, runCommand(t, applicationDir, targetEnv, "go", "version", "-m", targetBinary))
		}
	}
}

func identityMap(t *testing.T, output string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, line := range lines(output) {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
			t.Fatalf("invalid compiler identity line %q", line)
		}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate compiler identity %q", key)
		}
		out[key] = value
	}
	return out
}

func directorySnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot contains symlink %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sortedKeys(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func benchmarkMedian(t *testing.T, output, prefix string) float64 {
	t.Helper()
	var samples []float64
	for _, line := range lines(output) {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasPrefix(fields[0], prefix) || fields[3] != "ns/op" {
			continue
		}
		value, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			t.Fatalf("parse benchmark line %q: %v", line, err)
		}
		samples = append(samples, value)
	}
	if len(samples) != 7 {
		t.Fatalf("benchmark %s samples=%v\n%s", prefix, samples, output)
	}
	sort.Float64s(samples)
	return samples[len(samples)/2]
}
