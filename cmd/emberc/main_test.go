package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/preparedsource"
)

const externalPreparedFixtureSource = `
return {
    update = function(value)
        return value + 1
    end,
}
`

const externalPreparedFixtureUpdateEnvironment = "EMBER_UPDATE_EXTERNAL_PREPARED_FIXTURE"

func TestRunGeneratesChecksAndFreshWriteIsNoOp(t *testing.T) {
	root := newCompilerTestModule(t)
	writeEmbercTestFile(t, filepath.Join(root, "main.luau"), `return {update = function() return 40 end}`)
	outputDir := filepath.Join(root, "generated")
	mkdirEmbercTest(t, outputDir)
	manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
	unrelatedPath := filepath.Join(outputDir, "keep.txt")
	writeEmbercTestFile(t, unrelatedPath, "unrelated")

	other := t.TempDir()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })

	if err := run([]string{manifest}); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(outputDir, preparedGeneratedName)
	generated, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		preparedGeneratedHeader,
		"package preparedfixture",
		`emberapi "github.com/besmpl/ember"`,
		"var Bundle = emberapi.NewPreparedBundle(",
		"func LoadProgram(ctx context.Context, options emberapi.ProgramOptions)",
		"var ProgramRecipeDigest = [32]byte{",
	} {
		if !bytes.Contains(generated, []byte(marker)) {
			t.Fatalf("generated source lacks %q:\n%s", marker, generated)
		}
	}
	infoBefore, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-check", manifest}); err != nil {
		t.Fatalf("check fresh output: %v", err)
	}
	if err := run([]string{manifest}); err != nil {
		t.Fatalf("fresh write: %v", err)
	}
	infoAfter, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(infoBefore, infoAfter) || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Fatal("fresh write replaced or touched the output")
	}

	stale := append(append([]byte(nil), generated...), []byte("// stale\n")...)
	writeEmbercTestFile(t, outputPath, string(stale))
	if err := run([]string{"-check", manifest}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("check stale output error = %v, want stale", err)
	}
	if got, err := os.ReadFile(outputPath); err != nil || !bytes.Equal(got, stale) {
		t.Fatalf("-check changed stale output: %q, %v", got, err)
	}
	if err := run([]string{manifest}); err != nil {
		t.Fatalf("replace stale output: %v", err)
	}
	if got, err := os.ReadFile(outputPath); err != nil || !bytes.Equal(got, generated) {
		t.Fatalf("replacement = %q, %v", got, err)
	}
	if got, err := os.ReadFile(unrelatedPath); err != nil || string(got) != "unrelated" {
		t.Fatalf("unrelated sibling = %q, %v", got, err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Fatalf("output mode = %v, want 0644", info.Mode())
	}
}

func TestRunRequiresOutputDirAndPreservesOnPreGenerationFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest map[string]any
		want     string
	}{
		{
			name: "legacy output field",
			manifest: map[string]any{
				"package": "preparedfixture", "output": "generated/prepared.go",
				"entrypoints": []map[string]string{{"name": "main", "module": "logical:main"}},
				"modules":     []map[string]string{{"id": "logical:main", "source": "main.luau"}},
			},
			want: "unknown field",
		},
		{
			name: "duplicate module",
			manifest: map[string]any{
				"package": "preparedfixture", "output_dir": "generated",
				"entrypoints": []map[string]string{{"name": "main", "module": "logical:main"}},
				"modules": []map[string]string{
					{"id": "logical:main", "source": "main.luau"},
					{"id": "logical:main", "source": "main.luau"},
				},
			},
			want: "duplicated",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := newCompilerTestModule(t)
			mkdirEmbercTest(t, filepath.Join(root, "generated"))
			writeEmbercTestFile(t, filepath.Join(root, "main.luau"), `return {update = function() return 40 end}`)
			outputPath := filepath.Join(root, "generated", preparedGeneratedName)
			writeEmbercTestFile(t, outputPath, "unchanged")
			manifest := writeEmbercTestManifest(t, root, test.manifest)
			if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run error = %v, want %q", err, test.want)
			}
			if got, err := os.ReadFile(outputPath); err != nil || string(got) != "unchanged" {
				t.Fatalf("rejected manifest changed output to %q: %v", got, err)
			}
		})
	}
}

func TestResolveCompilerOutputDirectoryPreservesRawLayoutMount(t *testing.T) {
	root := newCompilerTestModule(t)
	internal := filepath.Join(root, "internal")
	mkdirEmbercTest(t, internal)
	output := filepath.Join(internal, "generated")
	mkdirEmbercTest(t, output)
	const rawMount = "internal/generated"
	resolved, mount, err := resolveCompilerOutputDirectory(compilerPaths{
		manifestDir: root,
		moduleRoot:  root,
	}, rawMount)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != output || mount != rawMount {
		t.Fatalf("output/mount = %q/%q, want %q/%q", resolved, mount, output, rawMount)
	}
}

func TestRunGenerationFailurePreservesOutput(t *testing.T) {
	root := newCompilerTestModule(t)
	mkdirEmbercTest(t, filepath.Join(root, "generated"))
	outputPath := filepath.Join(root, "generated", preparedGeneratedName)
	writeEmbercTestFile(t, outputPath, "unchanged")
	writeEmbercTestFile(t, filepath.Join(root, "main.luau"), `return {update = function() return 40 end}`)
	manifestData := validCompilerManifest("generated", "main.luau")
	manifestData["max_bytes"] = 1
	manifest := writeEmbercTestManifest(t, root, manifestData)
	if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "exceeds MaxBytes") {
		t.Fatalf("over-limit generation error = %v", err)
	}
	if got, err := os.ReadFile(outputPath); err != nil || string(got) != "unchanged" {
		t.Fatalf("failed generation changed output to %q: %v", got, err)
	}
}

func TestRunRequiresRegularManifestAndCanonicalModuleRoot(t *testing.T) {
	t.Run("nested manifest cannot select parent module", func(t *testing.T) {
		root := newCompilerTestModule(t)
		manifestDir := filepath.Join(root, "config")
		mkdirEmbercTest(t, manifestDir)
		mkdirEmbercTest(t, filepath.Join(manifestDir, "generated"))
		writeEmbercTestFile(t, filepath.Join(manifestDir, "main.luau"), `return {update = function() return 40 end}`)
		manifest := writeEmbercTestManifest(t, manifestDir, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "directly inside") {
			t.Fatalf("error = %v, want direct module-root placement rejection", err)
		}
		if _, err := os.Stat(filepath.Join(manifestDir, "generated", preparedGeneratedName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("nested manifest created output: %v", err)
		}
	})

	t.Run("missing go.mod", func(t *testing.T) {
		root := t.TempDir()
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "go.mod") {
			t.Fatalf("error = %v, want go.mod", err)
		}
	})

	t.Run("manifest case alias", func(t *testing.T) {
		root := newCompilerTestModule(t)
		data, _ := json.Marshal(validCompilerManifest("generated", "main.luau"))
		writeEmbercTestFile(t, filepath.Join(root, "Manifest.JSON"), string(data))
		err := run([]string{filepath.Join(root, "manifest.json")})
		if err == nil || !strings.Contains(err.Error(), "case alias") {
			t.Fatalf("error = %v, want case alias", err)
		}
	})

	t.Run("manifest symlink", func(t *testing.T) {
		root := newCompilerTestModule(t)
		target := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		link := filepath.Join(root, "linked.json")
		if err := os.Symlink(filepath.Base(target), link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := run([]string{link}); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v, want symlink manifest rejection", err)
		}
	})

	t.Run("go.mod symlink", func(t *testing.T) {
		root := t.TempDir()
		writeEmbercTestFile(t, filepath.Join(root, "real.mod"), "module example.test/app\n\ngo 1.26\n")
		if err := os.Symlink("real.mod", filepath.Join(root, "go.mod")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "non-symlink go.mod") {
			t.Fatalf("error = %v, want real go.mod rejection", err)
		}
	})
}

func TestRunRejectsUnsafeOutputDirectoryAndTargetForCheckAndWrite(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
		want  string
	}{
		{
			name: "missing output directory",
			want: "does not exist",
		},
		{
			name: "non-directory output",
			setup: func(t *testing.T, root string) {
				writeEmbercTestFile(t, filepath.Join(root, "generated"), "not a directory")
			},
			want: "not a directory",
		},
		{
			name: "case-alias output component",
			setup: func(t *testing.T, root string) {
				mkdirEmbercTest(t, filepath.Join(root, "Generated"))
			},
			want: "case alias",
		},
		{
			name: "unmanaged target",
			setup: func(t *testing.T, root string) {
				mkdirEmbercTest(t, filepath.Join(root, "generated"))
				writeEmbercTestFile(t, filepath.Join(root, "generated", preparedGeneratedName), "package application\n")
			},
			want: "unmanaged",
		},
		{
			name: "nonregular target",
			setup: func(t *testing.T, root string) {
				mkdirEmbercTest(t, filepath.Join(root, "generated"))
				mkdirEmbercTest(t, filepath.Join(root, "generated", preparedGeneratedName))
			},
			want: "not a regular file",
		},
		{
			name: "target case alias",
			setup: func(t *testing.T, root string) {
				mkdirEmbercTest(t, filepath.Join(root, "generated"))
				writeEmbercTestFile(t, filepath.Join(root, "generated", "PREPARED_GENERATED.GO"), preparedGeneratedHeader+"package old\n")
			},
			want: "case alias",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := newCompilerTestModule(t)
			writeEmbercTestFile(t, filepath.Join(root, "main.luau"), `return {update = function() return 40 end}`)
			if test.setup != nil {
				test.setup(t, root)
			}
			manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
			for _, arguments := range [][]string{{manifest}, {"-check", manifest}} {
				if err := run(arguments); err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("run(%v) error = %v, want %q", arguments, err, test.want)
				}
			}
		})
	}

	t.Run("symlink output directory", func(t *testing.T) {
		root := newCompilerTestModule(t)
		mkdirEmbercTest(t, filepath.Join(root, "real"))
		if err := os.Symlink("real", filepath.Join(root, "generated")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		writeEmbercTestFile(t, filepath.Join(root, "main.luau"), `return {update = function() return 40 end}`)
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v, want symlink", err)
		}
	})

	t.Run("symlink target", func(t *testing.T) {
		root := newCompilerTestModule(t)
		mkdirEmbercTest(t, filepath.Join(root, "generated"))
		writeEmbercTestFile(t, filepath.Join(root, "main.luau"), `return {update = function() return 40 end}`)
		writeEmbercTestFile(t, filepath.Join(root, "victim"), "unchanged")
		if err := os.Symlink("../victim", filepath.Join(root, "generated", preparedGeneratedName)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v, want symlink", err)
		}
		if got, err := os.ReadFile(filepath.Join(root, "victim")); err != nil || string(got) != "unchanged" {
			t.Fatalf("symlink victim changed to %q: %v", got, err)
		}
	})
}

func TestRunRejectsProtectedAndUnsafeModuleSources(t *testing.T) {
	t.Run("target is module source", func(t *testing.T) {
		root := newCompilerTestModule(t)
		mkdirEmbercTest(t, filepath.Join(root, "generated"))
		target := filepath.Join(root, "generated", preparedGeneratedName)
		writeEmbercTestFile(t, target, `return {update = function() return 40 end}`)
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "generated/"+preparedGeneratedName))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "protected module source") {
			t.Fatalf("error = %v, want protected source", err)
		}
		if got, err := os.ReadFile(target); err != nil || !bytes.HasPrefix(got, []byte("return")) {
			t.Fatalf("protected source changed to %q: %v", got, err)
		}
	})

	t.Run("source case alias", func(t *testing.T) {
		root := newCompilerTestModule(t)
		mkdirEmbercTest(t, filepath.Join(root, "generated"))
		writeEmbercTestFile(t, filepath.Join(root, "Main.luau"), `return {update = function() return 40 end}`)
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "case alias") {
			t.Fatalf("error = %v, want case alias", err)
		}
	})

	t.Run("source directory", func(t *testing.T) {
		root := newCompilerTestModule(t)
		mkdirEmbercTest(t, filepath.Join(root, "generated"))
		mkdirEmbercTest(t, filepath.Join(root, "main.luau"))
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("error = %v, want regular source", err)
		}
	})

	t.Run("source symlink", func(t *testing.T) {
		root := newCompilerTestModule(t)
		mkdirEmbercTest(t, filepath.Join(root, "generated"))
		writeEmbercTestFile(t, filepath.Join(root, "real.luau"), `return {update = function() return 40 end}`)
		if err := os.Symlink("real.luau", filepath.Join(root, "main.luau")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		manifest := writeEmbercTestManifest(t, root, validCompilerManifest("generated", "main.luau"))
		if err := run([]string{manifest}); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v, want symlink source", err)
		}
	})
}

func TestAdmitCompilerOutputRejectsProtectedHardLink(t *testing.T) {
	root := newCompilerTestModule(t)
	outputDir := filepath.Join(root, "generated")
	mkdirEmbercTest(t, outputDir)
	content := preparedGeneratedHeader + "\npackage generated\n"
	protected := filepath.Join(root, "protected.go")
	writeEmbercTestFile(t, protected, content)
	target := filepath.Join(outputDir, preparedGeneratedName)
	if err := os.Link(protected, target); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	plan := compilerMaterializationPlan{
		moduleRoot: root,
		origin:     filepath.Join(root, "ember-prepared.json"),
		file: embeddedFilePlan{
			relativePath: "generated/" + preparedGeneratedName,
			content:      content,
		},
		protected: []string{protected},
	}
	if _, err := admitCompilerOutput(plan); err == nil || !strings.Contains(err.Error(), "same file") {
		t.Fatalf("hard-linked target error = %v, want same-file rejection", err)
	}
	assertEmbercTestContent(t, protected, content)
}

func TestNewEmbeddedFilePlanIsPureAndS3aBounded(t *testing.T) {
	validSet := newEmbercTestSet(t, "generated", []preparedsource.File{{
		Name: preparedGeneratedName, Kind: preparedsource.GoFile,
		Content: preparedGeneratedHeader + "\npackage generated\nvar Bundle = 1\n",
	}})
	layout, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "internal/generated", Set: validSet}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := newEmbeddedFilePlan(layout)
	if err != nil {
		t.Fatal(err)
	}
	if plan.relativePath != "internal/generated/"+preparedGeneratedName ||
		plan.content != validSet.Files()[0].Content || plan.layoutDigest != layout.Digest() {
		t.Fatalf("plan = %#v", plan)
	}

	wrongName := newEmbercTestSet(t, "generated", []preparedsource.File{{
		Name: "other.go", Kind: preparedsource.GoFile,
		Content: preparedGeneratedHeader + "\npackage generated\n",
	}})
	multiple := newEmbercTestSet(t, "generated", []preparedsource.File{
		{Name: preparedGeneratedName, Kind: preparedsource.GoFile, Content: preparedGeneratedHeader + "\npackage generated\n"},
		{Name: "other.go", Kind: preparedsource.GoFile, Content: "package generated\n"},
	})
	withoutHeader := newEmbercTestSet(t, "generated", []preparedsource.File{{
		Name: preparedGeneratedName, Kind: preparedsource.GoFile, Content: "package generated\n",
	}})
	withAsset := newEmbercTestSet(t, "generated", []preparedsource.File{
		{
			Name: preparedGeneratedName, Kind: preparedsource.GoFile,
			Content: preparedGeneratedHeader + "\npackage generated\nimport _ \"embed\"\n\n//go:embed data.bin\nvar data string\n",
		},
		{Name: "data.bin", Kind: preparedsource.AssetFile, Content: "data"},
	})
	for _, test := range []struct {
		name   string
		layout preparedsource.Layout
	}{
		{name: "zero"},
		{name: "wrong leaf", layout: newEmbercTestLayout(t, []preparedsource.Mount{{Path: "generated", Set: wrongName}})},
		{name: "multiple leaves", layout: newEmbercTestLayout(t, []preparedsource.Mount{{Path: "generated", Set: multiple}})},
		{name: "asset", layout: newEmbercTestLayout(t, []preparedsource.Mount{{Path: "generated", Set: withAsset}})},
		{name: "missing exact header", layout: newEmbercTestLayout(t, []preparedsource.Mount{{Path: "generated", Set: withoutHeader}})},
		{name: "multiple mounts", layout: newEmbercTestLayout(t, []preparedsource.Mount{{Path: "a/generated", Set: validSet}, {Path: "b/generated", Set: validSet}})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newEmbeddedFilePlan(test.layout); err == nil {
				t.Fatal("plan succeeded")
			}
		})
	}
}

func TestCompilerOutputPrecommitFailuresPreserveDestination(t *testing.T) {
	root := newCompilerTestModule(t)
	outputDir := filepath.Join(root, "generated")
	mkdirEmbercTest(t, outputDir)
	target := filepath.Join(outputDir, preparedGeneratedName)
	old := preparedGeneratedHeader + "\npackage old\n"
	want := preparedGeneratedHeader + "\npackage replacement\n"
	writeEmbercTestFile(t, target, old)
	plan := compilerMaterializationPlan{
		moduleRoot: root,
		origin:     filepath.Join(root, "ember-prepared.json"),
		file: embeddedFilePlan{
			relativePath: "generated/" + preparedGeneratedName,
			content:      want,
		},
		protected: []string{filepath.Join(root, "go.mod")},
	}

	admission, err := admitCompilerOutput(plan)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected precommit failure")
	if err := writeCompilerOutputBeforeCommit(admission, func() error { return injected }); !errors.Is(err, injected) {
		t.Fatalf("precommit error = %v", err)
	}
	assertEmbercTestContent(t, target, old)
	assertNoEmbercTemps(t, outputDir)

	admission, err = admitCompilerOutput(plan)
	if err != nil {
		t.Fatal(err)
	}
	concurrent := preparedGeneratedHeader + "\npackage concurrent\n"
	err = writeCompilerOutputBeforeCommit(admission, func() error {
		return os.WriteFile(target, []byte(concurrent), 0o644)
	})
	if err == nil || !strings.Contains(err.Error(), "destination changed") {
		t.Fatalf("destination-race error = %v", err)
	}
	assertEmbercTestContent(t, target, concurrent)
	assertNoEmbercTemps(t, outputDir)
}

func TestWriteCompilerContentRejectsShortWrite(t *testing.T) {
	if err := writeCompilerContent(embercShortWriter{}, "generated"); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

type embercShortWriter struct{}

func (embercShortWriter) Write(payload []byte) (int, error) {
	return len(payload) / 2, nil
}

func TestExternalPreparedFixtureIsFresh(t *testing.T) {
	moduleRoot, goModPath := embercFixtureRepositoryRoot(t)
	module := ember.LogicalModule("prepared/external")
	program, _, err := ember.LoadProgram(context.Background(), compilerLoader{
		module.String(): {Name: module.String(), Text: externalPreparedFixtureSource},
	}, ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := program.GeneratePreparedGo(ember.PreparedGoOptions{Package: "preparedfixture"})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := preparedsource.NewLayout([]preparedsource.Mount{{
		Path: "internal/preparedfixture",
		Set:  artifact.Sources(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	filePlan, err := newEmbeddedFilePlan(layout)
	if err != nil {
		t.Fatal(err)
	}
	plan := compilerMaterializationPlan{
		moduleRoot: moduleRoot,
		origin:     "external prepared fixture update",
		file:       filePlan,
		protected:  []string{goModPath},
	}
	check := os.Getenv(externalPreparedFixtureUpdateEnvironment) != "1"
	if err := materializeCompilerPlan(plan, check); err != nil {
		t.Fatalf("external prepared fixture: %v", err)
	}
}

func embercFixtureRepositoryRoot(t *testing.T) (string, string) {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate emberc fixture test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))
	goModPath := filepath.Join(root, "go.mod")
	info, err := os.Lstat(goModPath)
	if err != nil {
		t.Fatalf("locate fixture module root: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		t.Fatalf("fixture module root has nonregular go.mod: %v", info.Mode())
	}
	return root, goModPath
}

func validCompilerManifest(outputDir, source string) map[string]any {
	return map[string]any{
		"package":    "preparedfixture",
		"output_dir": outputDir,
		"entrypoints": []map[string]string{
			{"name": "main", "module": "logical:main"},
		},
		"modules": []map[string]string{
			{"id": "logical:main", "source": source},
		},
	}
}

func newCompilerTestModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeEmbercTestFile(t, filepath.Join(root, "go.mod"), "module example.test/application\n\ngo 1.26\n")
	return root
}

func writeEmbercTestManifest(t *testing.T, directory string, manifest map[string]any) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fileName := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_")) + ".json"
	path := filepath.Join(directory, fileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeEmbercTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mkdirEmbercTest(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func newEmbercTestSet(t *testing.T, packageName string, files []preparedsource.File) preparedsource.Set {
	t.Helper()
	set, err := preparedsource.NewSet(packageName, files)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func newEmbercTestLayout(t *testing.T, mounts []preparedsource.Mount) preparedsource.Layout {
	t.Helper()
	layout, err := preparedsource.NewLayout(mounts)
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

func assertEmbercTestContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertNoEmbercTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".emberc-") {
			t.Fatalf("temporary file was not removed: %s", entry.Name())
		}
	}
}

func TestCompilerPathPolicyRejectsNonportableForms(t *testing.T) {
	forms := []string{"", ".", "..", "../generated", "generated/../other", "/absolute", "generated\\other", "C:/absolute"}
	if runtime.GOOS == "windows" {
		forms = append(forms, `C:\absolute`)
	}
	for _, form := range forms {
		if err := validatePortableRelativePath(form); err == nil {
			t.Errorf("validatePortableRelativePath(%q) succeeded", form)
		}
	}
	for _, form := range []string{"generated", "internal/generated", "a-b_c/file.luau"} {
		if err := validatePortableRelativePath(form); err != nil {
			t.Errorf("validatePortableRelativePath(%q): %v", form, err)
		}
	}
}
