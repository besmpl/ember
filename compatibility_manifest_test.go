package ember

import (
	"fmt"
	"go/ast"
	stdparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var compatibilityTestReferencePattern = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")

// TestCompatibilityManifestReferencesExistingBehaviorTests keeps the
// documented compatibility claim tied to concrete package tests. It checks
// names and table shape; the referenced tests remain the independent semantic
// proof for each feature.
func TestCompatibilityManifestReferencesExistingBehaviorTests(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed while locating compatibility manifest")
	}
	root := filepath.Dir(sourceFile)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "compatibility.md"))
	if err != nil {
		t.Fatalf("read compatibility manifest: %v", err)
	}

	testNames, err := packageTestNames(root)
	if err != nil {
		t.Fatalf("collect package test names: %v", err)
	}
	rows, err := parseCompatibilityManifest(string(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("compatibility manifest has no feature rows")
	}

	seenIDs := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, duplicate := seenIDs[row.id]; duplicate {
			t.Errorf("compatibility manifest repeats feature ID %q", row.id)
		}
		seenIDs[row.id] = struct{}{}
		if row.feature == "" {
			t.Errorf("compatibility manifest row %q has no feature", row.id)
		}
		if !validCompatibilityLevel(row.level) {
			t.Errorf("compatibility manifest row %q has unknown level %q", row.id, row.level)
		}
		if len(row.tests) == 0 {
			t.Errorf("compatibility manifest row %q has no behavior tests", row.id)
		}
		for _, testName := range row.tests {
			if _, found := testNames[testName]; !found {
				t.Errorf("compatibility manifest row %q references missing test %s", row.id, testName)
			}
		}
	}
	if t.Failed() {
		t.Logf("manifest rows: %d", len(rows))
	}
}

type compatibilityManifestRow struct {
	id      string
	feature string
	level   string
	tests   []string
}

func parseCompatibilityManifest(doc string) ([]compatibilityManifestRow, error) {
	const header = "| ID | Feature | Level | Behavior test(s) |"
	lines := strings.Split(doc, "\n")
	headerIndex := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			headerIndex = i
			break
		}
	}
	if headerIndex < 0 {
		return nil, fmt.Errorf("compatibility manifest table header not found")
	}

	var rows []compatibilityManifestRow
	for _, line := range lines[headerIndex+2:] {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := splitManifestRow(line)
		if len(cells) != 4 {
			return nil, fmt.Errorf("compatibility manifest row has %d cells: %q", len(cells), line)
		}
		if cells[0] == "" || cells[1] == "" || cells[2] == "" {
			return nil, fmt.Errorf("compatibility manifest row has an empty required cell: %q", line)
		}
		tests := compatibilityTestReferencePattern.FindAllStringSubmatch(cells[3], -1)
		row := compatibilityManifestRow{id: cells[0], feature: cells[1], level: cells[2]}
		for _, match := range tests {
			row.tests = append(row.tests, match[1])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func splitManifestRow(line string) []string {
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func packageTestNames(root string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	testNames := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := stdparser.ParseFile(token.NewFileSet(), filepath.Join(root, entry.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			testNames[function.Name.Name] = struct{}{}
		}
	}
	return testNames, nil
}

func validCompatibilityLevel(level string) bool {
	switch level {
	case "Parsed", "Compiled", "Executed", "Conformant", "Embedded":
		return true
	default:
		return false
	}
}
