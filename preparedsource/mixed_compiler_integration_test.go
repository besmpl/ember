package preparedsource_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestMixedRealCompilersComposeWithoutSharedABI(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	preparedSourceDir := filepath.Dir(sourceFile)
	repositoryDir := filepath.Dir(preparedSourceDir)
	temp := t.TempDir()
	seedCompilerDir := filepath.Join(temp, "seed-compiler")
	mixedCompilerDir := filepath.Join(temp, "mixed-compiler")
	copyTree(t, filepath.Join(preparedSourceDir, "testdata", "externalcompiler"), seedCompilerDir)
	copyTree(t, filepath.Join(preparedSourceDir, "testdata", "mixedcompiler"), mixedCompilerDir)

	seedModule := "module example.com/ember-external-compiler\n\ngo 1.26\n\nrequire github.com/besmpl/ember v0.0.0\n\nreplace github.com/besmpl/ember => " + filepath.ToSlash(repositoryDir) + "\n"
	if err := os.WriteFile(filepath.Join(seedCompilerDir, "go.mod"), []byte(seedModule), 0o644); err != nil {
		t.Fatal(err)
	}
	mixedModule := fmt.Sprintf(`module github.com/besmpl/ember/mixedcompiler

go 1.26

require (
	example.com/ember-external-compiler v0.0.0
	github.com/besmpl/ember v0.0.0
)

replace example.com/ember-external-compiler => %s

replace github.com/besmpl/ember => %s
`, filepath.ToSlash(seedCompilerDir), filepath.ToSlash(repositoryDir))
	if err := os.WriteFile(filepath.Join(mixedCompilerDir, "go.mod"), []byte(mixedModule), 0o644); err != nil {
		t.Fatal(err)
	}

	compilerEnv := testEnvironment(
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
	if module := strings.TrimSpace(runGo(t, mixedCompilerDir, compilerEnv, "list", "-m", "-f", "{{.Path}}")); module != "github.com/besmpl/ember/mixedcompiler" {
		t.Fatalf("mixed compiler module = %q", module)
	}
	assertGoImports(t, mixedCompilerDir, func(path string) bool {
		return path == "example.com/ember-external-compiler" ||
			path == "github.com/besmpl/ember/internal/sprigproof" ||
			path == "github.com/besmpl/ember/preparedsource" ||
			!strings.Contains(path, ".")
	})
	compilerDeps := lines(runGo(t, mixedCompilerDir, compilerEnv, "list", "-deps", "./..."))
	for _, required := range []string{
		"example.com/ember-external-compiler",
		"github.com/besmpl/ember/internal/sprigproof",
		"github.com/besmpl/ember/preparedsource",
	} {
		if !containsLine(compilerDeps, required) {
			t.Fatalf("mixed compiler omits required concrete dependency %q", required)
		}
	}
	for _, dependency := range compilerDeps {
		if dependency == "github.com/besmpl/ember" ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/internal/") && dependency != "github.com/besmpl/ember/internal/sprigproof" ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/preparedworker") {
			t.Fatalf("mixed compiler has forbidden dependency %q", dependency)
		}
	}
	compilerNonstandard := lines(runGo(t, mixedCompilerDir, compilerEnv, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	assertExactLines(t, "mixed compiler non-standard dependencies", compilerNonstandard, []string{
		"example.com/ember-external-compiler",
		"github.com/besmpl/ember/internal/sprigproof",
		"github.com/besmpl/ember/mixedcompiler/cmd/appmaterialize",
		"github.com/besmpl/ember/preparedsource",
	})
	runGo(t, mixedCompilerDir, compilerEnv, "test", "-count=1", "./...")

	seedSource := filepath.Join(seedCompilerDir, "program.seed")
	seedBytes, err := os.ReadFile(seedSource)
	if err != nil {
		t.Fatal(err)
	}
	applicationDir := filepath.Join(temp, "application")
	repeatedApplicationDir := filepath.Join(temp, "application-repeat")
	identities := runGo(t, mixedCompilerDir, compilerEnv, "run", "./cmd/appmaterialize", "-seed", seedSource, "-out", applicationDir)
	repeatedIdentities := runGo(t, mixedCompilerDir, compilerEnv, "run", "./cmd/appmaterialize", "-seed", seedSource, "-out", repeatedApplicationDir)
	if identities != repeatedIdentities {
		t.Fatalf("repeated mixed compilation identities differ:\n%s\n%s", identities, repeatedIdentities)
	}
	wantIdentities := map[string]string{
		"layout":        "f7374a3c394fc1c3ab46f527fd4e46bb74b484a2b7b40ff824d3f239105da286",
		"seed_program":  "b5f1ada51772f913502ae4a0cb40790dcfb4f98f620412f33e35df86b5b543f9",
		"seed_set":      "ab879222f8fb1795de30d5c2c958e3a1253390ee2ff314ec54c8c266ae1c1a67",
		"sprig_package": "e92a97f8e5fc66e6531402275e1e3f02a9e869df10314603ecc4a7e1c32fb42b",
		"sprig_project": "d06d57d8034c95e7619a3781a4fb32475d71ef97fed3bbcfb8807053c2f0374f",
		"sprig_set":     "8fd86e4a044cf616356881fe2a8a055bf147f110cb0a81f80456de880ea8d76b",
	}
	if got := identityMap(t, identities); !reflect.DeepEqual(got, wantIdentities) {
		t.Fatalf("mixed compiler identities = %#v, want %#v", got, wantIdentities)
	}
	firstSnapshot := directorySnapshot(t, applicationDir)
	secondSnapshot := directorySnapshot(t, repeatedApplicationDir)
	if !reflect.DeepEqual(firstSnapshot, secondSnapshot) {
		t.Fatal("repeated mixed compilation changed application bytes")
	}
	runCommandMustFail(t, mixedCompilerDir, compilerEnv, "go", "run", "./cmd/appmaterialize", "-seed", seedSource, "-out", applicationDir)

	if err := os.RemoveAll(mixedCompilerDir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(seedCompilerDir); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{mixedCompilerDir, seedCompilerDir} {
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatalf("compiler tooling still exists before application proof at %s: %v", directory, err)
		}
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
	assertExactLines(t, "mixed application files", sortedKeys(firstSnapshot), []string{
		"cmd/app/handler.go",
		"cmd/app/handler_test.go",
		"cmd/app/main.go",
		"generated/counter/reduce_generated.go",
		"generated/score/reduce_generated.go",
		"go.mod",
	})
	assertGoImports(t, applicationDir, func(path string) bool {
		return path == "example.com/mixed-compiled-application/generated/counter" ||
			path == "example.com/mixed-compiled-application/generated/score" ||
			!strings.Contains(path, ".")
	})
	if goMod := firstSnapshot["go.mod"]; goMod != "module example.com/mixed-compiled-application\n\ngo 1.26\n" {
		t.Fatalf("mixed application go.mod contains delivery dependencies:\n%s", goMod)
	}
	if modules := strings.TrimSpace(runGo(t, applicationDir, applicationEnv, "list", "-m", "all")); modules != "example.com/mixed-compiled-application" {
		t.Fatalf("mixed application module graph retained an unexpected module:\n%s", modules)
	}
	applicationDeps := lines(runGo(t, applicationDir, applicationEnv, "list", "-deps", "./..."))
	for _, dependency := range applicationDeps {
		if strings.Contains(dependency, "github.com/besmpl/ember") ||
			strings.Contains(dependency, "ember-external-compiler") ||
			strings.Contains(dependency, "mixedcompiler") ||
			strings.Contains(dependency, "sprigproof") ||
			strings.Contains(dependency, "preparedsource") ||
			strings.Contains(dependency, "preparedworker") {
			t.Fatalf("mixed application retained compiler/delivery dependency %q", dependency)
		}
	}
	applicationNonstandard := lines(runGo(t, applicationDir, applicationEnv, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	assertExactLines(t, "mixed application non-standard dependencies", applicationNonstandard, []string{
		"example.com/mixed-compiled-application/cmd/app",
		"example.com/mixed-compiled-application/generated/counter",
		"example.com/mixed-compiled-application/generated/score",
	})
	runGo(t, applicationDir, applicationEnv, "test", "-count=1", "./...")

	binary := filepath.Join(temp, "mixed-compiled-application")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	runGo(t, applicationDir, applicationEnv, "build", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", binary, "./cmd/app")
	buildInfo := runCommand(t, applicationDir, applicationEnv, "go", "version", "-m", binary)
	assertStaticBuildInfo(t, buildInfo)
	assertNoDeliveryNames(t, "mixed binary symbols", runGo(t, applicationDir, applicationEnv, "tool", "nm", binary))
	if output := strings.TrimSpace(runCommand(t, applicationDir, applicationEnv, binary)); output != "3:4:6|1:1:0|2:2|true:true:true" {
		t.Fatalf("mixed application output = %q", output)
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("seed_source_sha256=%x seed_generated_sha256=%x seed_generated_bytes=%d sprig_generated_sha256=%x sprig_generated_bytes=%d handler_sha256=%x handler_bytes=%d binary_sha256=%x binary_bytes=%d",
		sha256.Sum256(seedBytes),
		sha256.Sum256([]byte(firstSnapshot["generated/score/reduce_generated.go"])), len(firstSnapshot["generated/score/reduce_generated.go"]),
		sha256.Sum256([]byte(firstSnapshot["generated/counter/reduce_generated.go"])), len(firstSnapshot["generated/counter/reduce_generated.go"]),
		sha256.Sum256([]byte(firstSnapshot["cmd/app/handler.go"])), len(firstSnapshot["cmd/app/handler.go"]),
		sha256.Sum256(binaryBytes), len(binaryBytes),
	)

	if os.Getenv("EMBER_MIXED_COMPILER_PERF") != "" {
		var mixedOutput, directOutput strings.Builder
		benchmark := func(name string) string {
			return runGo(t, applicationDir, applicationEnv,
				"test", "-run", "^$", "-bench", "^"+name+"$", "-benchtime=200ms", "-count=1", "-cpu=1", "./cmd/app")
		}
		for round := range 7 {
			if round%2 == 0 {
				mixedOutput.WriteString(benchmark("BenchmarkMixedHandler"))
				directOutput.WriteString(benchmark("BenchmarkConcreteCalls"))
			} else {
				directOutput.WriteString(benchmark("BenchmarkConcreteCalls"))
				mixedOutput.WriteString(benchmark("BenchmarkMixedHandler"))
			}
		}
		mixed := benchmarkMedian(t, mixedOutput.String(), "BenchmarkMixedHandler")
		direct := benchmarkMedian(t, directOutput.String(), "BenchmarkConcreteCalls")
		ratio := mixed / direct
		t.Logf("mixed steady median handler=%.3f ns/op concrete=%.3f ns/op ratio=%.3fx\n--- handler ---\n%s--- concrete ---\n%s", mixed, direct, ratio, mixedOutput.String(), directOutput.String())
		if ratio > 1.05 {
			t.Fatalf("mixed handler steady ratio %.3fx exceeds 1.05x", ratio)
		}
	}
	if os.Getenv("EMBER_MIXED_COMPILER_CROSS") != "" {
		for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
			parts := strings.Split(target, "/")
			targetEnv := testEnvironment(append(applicationEnv, "GOOS="+parts[0], "GOARCH="+parts[1])...)
			targetBinary := filepath.Join(temp, "mixed-app-"+parts[0]+"-"+parts[1])
			runGo(t, applicationDir, targetEnv, "build", "-buildvcs=false", "-trimpath", "-pgo=off", "-o", targetBinary, "./cmd/app")
			assertStaticBuildInfo(t, runCommand(t, applicationDir, targetEnv, "go", "version", "-m", targetBinary))
		}
	}
}
