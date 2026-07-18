package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGeneratesAndChecksManifestRelativePreparedSource(t *testing.T) {
	directory := t.TempDir()
	writeEmbercTestFile(t, filepath.Join(directory, "main.luau"), `return {update = function() return 40 end}`)
	manifest := writeEmbercTestManifest(t, directory, map[string]any{
		"package": "preparedfixture",
		"output":  "generated/prepared.go",
		"entrypoints": []map[string]string{
			{"name": "main", "module": "logical:main"},
		},
		"modules": []map[string]string{
			{"id": "logical:main", "source": "main.luau"},
		},
	})
	if err := os.Mkdir(filepath.Join(directory, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	outputPath := filepath.Join(directory, "generated", "prepared.go")
	generated, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"package preparedfixture",
		`emberapi "github.com/besmpl/ember"`,
		"var Bundle = emberapi.NewPreparedBundle(",
	} {
		if !bytes.Contains(generated, []byte(marker)) {
			t.Fatalf("generated source lacks %q:\n%s", marker, generated)
		}
	}
	if err := run([]string{"-check", manifest}); err != nil {
		t.Fatalf("check fresh output: %v", err)
	}

	stale := append([]byte(nil), generated...)
	stale = append(stale, []byte("// stale\n")...)
	writeEmbercTestFile(t, outputPath, string(stale))
	if err := run([]string{"-check", manifest}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("check stale output error = %v, want stale", err)
	}
	after, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, stale) {
		t.Fatal("-check rewrote stale output")
	}
}

func TestRunRejectsInvalidManifestAndPreservesOutput(t *testing.T) {
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "prepared.go")
	writeEmbercTestFile(t, outputPath, "unchanged")
	writeEmbercTestFile(t, filepath.Join(directory, "main.luau"), `return {update = function() return "unsupported" end}`)

	for _, test := range []struct {
		name     string
		manifest map[string]any
	}{
		{
			name: "unknown field",
			manifest: map[string]any{
				"package": "preparedfixture", "output": "prepared.go", "unknown": true,
				"entrypoints": []map[string]string{{"name": "main", "module": "logical:main"}},
				"modules":     []map[string]string{{"id": "logical:main", "source": "main.luau"}},
			},
		},
		{
			name: "duplicate module",
			manifest: map[string]any{
				"package": "preparedfixture", "output": "prepared.go",
				"entrypoints": []map[string]string{{"name": "main", "module": "logical:main"}},
				"modules": []map[string]string{
					{"id": "logical:main", "source": "main.luau"},
					{"id": "logical:main", "source": "main.luau"},
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := writeEmbercTestManifest(t, directory, test.manifest)
			if err := run([]string{manifest}); err == nil {
				t.Fatal("run succeeded")
			}
			got, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "unchanged" {
				t.Fatalf("rejected manifest changed output to %q", got)
			}
		})
	}
}

func TestRunGenerationFailurePreservesOutput(t *testing.T) {
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "prepared.go")
	writeEmbercTestFile(t, outputPath, "unchanged")
	writeEmbercTestFile(t, filepath.Join(directory, "main.luau"), `return {update = function() return 40 end}`)
	manifest := writeEmbercTestManifest(t, directory, map[string]any{
		"package":   "preparedfixture",
		"output":    "prepared.go",
		"max_bytes": 1,
		"entrypoints": []map[string]string{
			{"name": "main", "module": "logical:main"},
		},
		"modules": []map[string]string{
			{"id": "logical:main", "source": "main.luau"},
		},
	})
	if err := run([]string{manifest}); err == nil {
		t.Fatal("over-limit generation succeeded")
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "unchanged" {
		t.Fatalf("failed generation changed output to %q", got)
	}
}

func writeEmbercTestManifest(t *testing.T, directory string, manifest map[string]any) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, strings.ReplaceAll(t.Name(), "/", "_")+".json")
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
