package preparedsource_test

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExternalProducerBuildsConcreteGeneratedApplication(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	fixtureDir := filepath.Join(filepath.Dir(sourceFile), "testdata", "externalproducer")
	repositoryDir := filepath.Dir(filepath.Dir(sourceFile))
	temp := t.TempDir()
	producerDir := filepath.Join(temp, "producer")
	copyTree(t, fixtureDir, producerDir)
	producerModule := "module example.com/ember-external-producer\n\ngo 1.26\n\nrequire github.com/besmpl/ember v0.0.0\n\nreplace github.com/besmpl/ember => " + filepath.ToSlash(repositoryDir) + "\n"
	if err := os.WriteFile(filepath.Join(producerDir, "go.mod"), []byte(producerModule), 0o644); err != nil {
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
		"GOCACHE="+filepath.Join(temp, "go-cache"),
		"GOMODCACHE="+filepath.Join(temp, "mod-cache"),
	)

	module := runGo(t, producerDir, env, "list", "-m", "-f", "{{.Path}}")
	if strings.TrimSpace(module) != "example.com/ember-external-producer" {
		t.Fatalf("producer module = %q", strings.TrimSpace(module))
	}
	assertGoImports(t, producerDir, func(path string) bool {
		return path == "github.com/besmpl/ember/preparedsource" ||
			path == "example.com/ember-external-producer" ||
			!strings.Contains(path, ".")
	})
	producerDeps := lines(runGo(t, producerDir, env, "list", "-deps", "./..."))
	if !containsLine(producerDeps, "github.com/besmpl/ember/preparedsource") {
		t.Fatal("external producer does not depend on preparedsource")
	}
	for _, dependency := range producerDeps {
		if dependency == "github.com/besmpl/ember" ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/internal/") ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/preparedworker") ||
			strings.HasPrefix(dependency, "github.com/besmpl/ember/preparedworkerbuild") {
			t.Fatalf("external producer has forbidden Ember dependency %q", dependency)
		}
	}
	producerNonstandard := lines(runGo(t, producerDir, env, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	assertExactLines(t, "producer non-standard dependencies", producerNonstandard, []string{
		"example.com/ember-external-producer",
		"example.com/ember-external-producer/cmd/appmaterialize",
		"github.com/besmpl/ember/preparedsource",
	})
	runGo(t, producerDir, env, "test", "-count=1", "./...")

	applicationDir := filepath.Join(temp, "application")
	identities := runGo(t, producerDir, env, "run", "./cmd/appmaterialize", "-out", applicationDir)
	identityLines := lines(identities)
	if len(identityLines) != 3 {
		t.Fatalf("materializer identities = %q", identities)
	}
	for _, line := range identityLines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || len(parts[1]) != 64 || strings.Trim(parts[1], "0123456789abcdef") != "" {
			t.Fatalf("invalid materializer identity %q", line)
		}
	}
	if strings.SplitN(identityLines[0], "=", 2)[1] == strings.SplitN(identityLines[1], "=", 2)[1] {
		t.Fatalf("Ruby-shaped and Sprig-shaped Set identities match: %q", identityLines[0])
	}
	t.Log(strings.TrimSpace(identities))
	runCommandMustFail(t, producerDir, env, "go", "run", "./cmd/appmaterialize", "-out", applicationDir)

	if err := os.RemoveAll(producerDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(producerDir); !os.IsNotExist(err) {
		t.Fatalf("producer tooling still exists before application proof: %v", err)
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
	assertGoImports(t, applicationDir, func(path string) bool {
		return path == "example.com/generated-application/generated/ruby" ||
			path == "example.com/generated-application/generated/sprig" ||
			!strings.Contains(path, ".")
	})
	assertExactLines(t, "application Go files", goFiles(t, applicationDir), []string{
		"cmd/app/main.go",
		"cmd/app/main_test.go",
		"generated/ruby/ownership.go",
		"generated/ruby/scope.go",
		"generated/sprig/composition.go",
		"generated/sprig/scope.go",
	})
	goMod, err := os.ReadFile(filepath.Join(applicationDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(goMod) != "module example.com/generated-application\n\ngo 1.26\n" {
		t.Fatalf("application go.mod contains delivery dependencies:\n%s", goMod)
	}
	applicationDeps := lines(runGo(t, applicationDir, applicationEnv, "list", "-deps", "./..."))
	for _, dependency := range applicationDeps {
		if strings.Contains(dependency, "github.com/besmpl/ember") ||
			strings.Contains(dependency, "ember-external-producer") ||
			strings.Contains(dependency, "preparedsource") ||
			strings.Contains(dependency, "preparedworker") {
			t.Fatalf("static application retained delivery tooling dependency %q", dependency)
		}
	}
	modules := runGo(t, applicationDir, applicationEnv, "list", "-m", "all")
	if strings.TrimSpace(modules) != "example.com/generated-application" {
		t.Fatalf("application module graph retained an unexpected module:\n%s", modules)
	}
	nonstandard := lines(runGo(t, applicationDir, applicationEnv, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	wanted := map[string]bool{
		"example.com/generated-application/cmd/app":         true,
		"example.com/generated-application/generated/ruby":  true,
		"example.com/generated-application/generated/sprig": true,
	}
	for _, dependency := range nonstandard {
		if !wanted[dependency] {
			t.Fatalf("unexpected non-standard application dependency %q", dependency)
		}
		delete(wanted, dependency)
	}
	if len(wanted) != 0 {
		t.Fatalf("application dependency allowlist entries not observed: %v", wanted)
	}
	runGo(t, applicationDir, applicationEnv, "test", "-count=1", "./...")

	binary := filepath.Join(temp, "generated-application")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	runGo(t, applicationDir, applicationEnv, "build", "-o", binary, "./cmd/app")
	buildInfo := runCommand(t, applicationDir, applicationEnv, "go", "version", "-m", binary)
	assertStaticBuildInfo(t, buildInfo)
	symbols := runGo(t, applicationDir, applicationEnv, "tool", "nm", binary)
	assertNoDeliveryNames(t, "binary symbols", symbols)

	output := runCommand(t, applicationDir, applicationEnv, binary)
	if strings.TrimSpace(output) != "shape-only|1:Widget|42" {
		t.Fatalf("application output = %q", strings.TrimSpace(output))
	}
	if os.Getenv("EMBER_EXTERNAL_PRODUCER_CROSS") != "" {
		for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
			parts := strings.Split(target, "/")
			targetEnv := testEnvironment(append(applicationEnv, "GOOS="+parts[0], "GOARCH="+parts[1])...)
			targetBinary := filepath.Join(temp, "app-"+parts[0]+"-"+parts[1])
			runGo(t, applicationDir, targetEnv, "build", "-o", targetBinary, "./cmd/app")
			assertStaticBuildInfo(t, runCommand(t, applicationDir, targetEnv, "go", "version", "-m", targetBinary))
		}
	}
}

func testEnvironment(overrides ...string) []string {
	values := make(map[string]string, len(overrides))
	order := make([]string, 0, len(overrides))
	for _, item := range overrides {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			panic("test environment override has no equals sign")
		}
		key = environmentKey(key)
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = item
	}
	environment := make([]string, 0, len(os.Environ())+len(order))
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, exists := values[environmentKey(key)]; exists {
				continue
			}
		}
		environment = append(environment, item)
	}
	for _, key := range order {
		environment = append(environment, values[key])
	}
	return environment
}

func environmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func runGo(t *testing.T, directory string, env []string, args ...string) string {
	t.Helper()
	return runCommand(t, directory, env, "go", args...)
}

func runCommand(t *testing.T, directory string, env []string, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = env
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out: %v", command, ctx.Err())
	}
	if err != nil {
		t.Fatalf("%s: %v\n%s", command, err, output)
	}
	return string(output)
}

func runCommandMustFail(t *testing.T, directory string, env []string, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = env
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out: %v", command, ctx.Err())
	}
	if err == nil {
		t.Fatalf("%s unexpectedly succeeded:\n%s", command, output)
	}
}

func assertNoDeliveryNames(t *testing.T, observer, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	for _, forbidden := range []string{
		"github.com/besmpl/ember",
		"ember-external-producer",
		"ember-external-compiler",
		"seedcompiler",
		"mixedcompiler",
		"sprigproof",
		"preparedsource",
		"preparedworker",
		"preparedworkerbuild",
		"language registry",
		"universal value",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("%s retained %q", observer, forbidden)
		}
	}
}

func assertStaticBuildInfo(t *testing.T, buildInfo string) {
	t.Helper()
	assertNoDeliveryNames(t, "binary build info", buildInfo)
	if strings.Contains(buildInfo, "\tdep\t") {
		t.Fatalf("binary build info contains a module dependency:\n%s", buildInfo)
	}
	if !containsLine(lines(buildInfo), "\tbuild\tCGO_ENABLED=0") {
		t.Fatalf("binary build info does not prove CGO_ENABLED=0:\n%s", buildInfo)
	}
}

func lines(output string) []string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func containsLine(lines []string, wanted string) bool {
	for _, line := range lines {
		if line == wanted {
			return true
		}
	}
	return false
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertGoImports(t *testing.T, root string, allowed func(string) bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return walkErr
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Decls {
			declaration, ok := spec.(*ast.GenDecl)
			if !ok || declaration.Tok != token.IMPORT {
				continue
			}
			for _, item := range declaration.Specs {
				path := strings.Trim(item.(*ast.ImportSpec).Path.Value, "\"")
				if !allowed(path) {
					return fmt.Errorf("%s imports forbidden package %q", file.Name.Name, path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func assertExactLines(t *testing.T, label string, got, want []string) {
	t.Helper()
	remaining := make(map[string]bool, len(want))
	for _, item := range want {
		remaining[item] = true
	}
	for _, item := range got {
		if !remaining[item] {
			t.Fatalf("%s includes unexpected %q (all: %v)", label, item, got)
		}
		delete(remaining, item)
	}
	if len(remaining) != 0 {
		t.Fatalf("%s omits %v (all: %v)", label, remaining, got)
	}
}
