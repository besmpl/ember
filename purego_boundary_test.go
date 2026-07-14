package ember

import (
	"bufio"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const pureGoPackageOwner = "<package>"

type pureGoExecAllowEntry struct {
	File    string
	Owner   string
	Class   string
	Purpose string
}

type pureGoBoundaryIssue struct {
	Path   string
	Detail string
}

type pureGoScanResult struct {
	Issues   []pureGoBoundaryIssue
	Launches map[string]int
}

func TestPureGoBoundary(t *testing.T) {
	root := pureGoRepositoryRoot(t)
	allowlist, err := loadPureGoExecAllowlist(filepath.Join(root, "testdata", "purego", "exec-allowlist-v1.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanPureGoRepository(root, allowlist)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) == 0 {
		return
	}
	sort.Slice(result.Issues, func(i, j int) bool {
		if result.Issues[i].Path != result.Issues[j].Path {
			return result.Issues[i].Path < result.Issues[j].Path
		}
		return result.Issues[i].Detail < result.Issues[j].Detail
	})
	var details []string
	for _, issue := range result.Issues {
		details = append(details, fmt.Sprintf("%s: %s", issue.Path, issue.Detail))
	}
	t.Fatalf("pure-Go boundary violations:\n%s", strings.Join(details, "\n"))
}

func TestPureGoBoundaryFixtures(t *testing.T) {
	root := pureGoRepositoryRoot(t)
	fixtureRoot := filepath.Join(root, "testdata", "purego")
	cases := []struct {
		name      string
		wantIssue string
	}{
		{name: "scanner_comments_and_strings.go"},
		{name: "allowed_go_assembly.s"},
		{name: "allowed_shadowed_identifiers.go"},
		{name: "rejected_cgo.go", wantIssue: `imports "C"`},
		{name: "rejected_foreign.c", wantIssue: "foreign source or object"},
		{name: "rejected_foreign.a", wantIssue: "foreign source or object"},
		{name: "rejected_foreign.o", wantIssue: "foreign source or object"},
		{name: "rejected_foreign.s", wantIssue: "assembly call-like instruction CALL has non-Go target"},
		{name: "rejected_assembly_alternate.s", wantIssue: "assembly call-like instruction BLR has non-Go target"},
		{name: "rejected_tailcall.s", wantIssue: "assembly call-like instruction JMP has non-Go target"},
		{name: "rejected_linkname.go", wantIssue: "private go:linkname directive"},
		{name: "rejected_dynamic.go", wantIssue: "forbidden foreign or executable-memory API"},
		{name: "rejected_alternate_apis.go", wantIssue: "forbidden process API"},
		{name: "rejected_exec.go", wantIssue: "os/exec launch is not allowlisted"},
		{name: "rejected_exec_escape.go", wantIssue: "os/exec constructor escapes without a direct call"},
		{name: "rejected_package_exec.go", wantIssue: "package-level process launch"},
		{name: "rejected_package_dynamic.go", wantIssue: "forbidden foreign or executable-memory API"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(fixtureRoot, tc.name)
			result, err := scanPureGoPath(root, path, map[string]pureGoExecAllowEntry{})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantIssue == "" {
				if len(result.Issues) != 0 {
					t.Fatalf("unexpected issues: %v", result.Issues)
				}
				return
			}
			for _, issue := range result.Issues {
				if strings.Contains(issue.Detail, tc.wantIssue) {
					return
				}
			}
			t.Fatalf("missing issue containing %q: %v", tc.wantIssue, result.Issues)
		})
	}
}

func TestPureGoBoundaryAllowlistFixtures(t *testing.T) {
	root := pureGoRepositoryRoot(t)
	fixtureRoot := filepath.Join(root, "testdata", "purego")
	for _, name := range []string{
		"malformed-allowlist.tsv",
		"invalid-class-allowlist.tsv",
		"non-test-allowlist.tsv",
		"empty-purpose-allowlist.tsv",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadPureGoExecAllowlist(filepath.Join(fixtureRoot, name)); err == nil {
				t.Fatal("malformed allowlist accepted")
			}
		})
	}

	t.Run("stale", func(t *testing.T) {
		entries, err := loadPureGoExecAllowlist(filepath.Join(fixtureRoot, "stale-allowlist.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		issues := validatePureGoExecAllowlist(entries, map[string]int{})
		if len(issues) != 1 || !strings.Contains(issues[0].Detail, "exactly one") {
			t.Fatalf("stale allowlist issues = %v", issues)
		}
	})

	t.Run("multiple-launches", func(t *testing.T) {
		entries := map[string]pureGoExecAllowEntry{
			"fixture_test.go::launch": {File: "fixture_test.go", Owner: "launch", Class: "generator-check", Purpose: "fixture"},
		}
		issues := validatePureGoExecAllowlist(entries, map[string]int{"fixture_test.go::launch": 2})
		if len(issues) != 1 || !strings.Contains(issues[0].Detail, "got 2") {
			t.Fatalf("multiple launch issues = %v", issues)
		}
	})
}

func TestPureGoLinkedBinary(t *testing.T) {
	path := os.Getenv("EMBER_PUREGO_LINKED_BINARY")
	if path == "" {
		t.Skip("set EMBER_PUREGO_LINKED_BINARY to inspect a linked test binary")
	}
	if err := checkPureGoLinkedBinarySegments(path); err != nil {
		t.Fatal(err)
	}
}

func pureGoRepositoryRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}

func loadPureGoExecAllowlist(path string) (map[string]pureGoExecAllowEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make(map[string]pureGoExecAllowEntry)
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("pure-Go exec allowlist %s:%d: want file, owner, class, purpose TSV", path, lineNumber)
		}
		entry := pureGoExecAllowEntry{
			File:    strings.TrimSpace(fields[0]),
			Owner:   strings.TrimSpace(fields[1]),
			Class:   strings.TrimSpace(fields[2]),
			Purpose: strings.TrimSpace(fields[3]),
		}
		if entry.File == "" || entry.Owner == "" || entry.Purpose == "" {
			return nil, fmt.Errorf("pure-Go exec allowlist %s:%d: empty file, owner, or purpose", path, lineNumber)
		}
		if !strings.HasSuffix(entry.File, "_test.go") {
			return nil, fmt.Errorf("pure-Go exec allowlist %s:%d: %q is not a _test.go file", path, lineNumber, entry.File)
		}
		if !pureGoExecClass(entry.Class) {
			return nil, fmt.Errorf("pure-Go exec allowlist %s:%d: unknown class %q", path, lineNumber, entry.Class)
		}
		key := pureGoAllowlistKey(entry.File, entry.Owner)
		if _, exists := entries[key]; exists {
			return nil, fmt.Errorf("pure-Go exec allowlist %s:%d: duplicate %s", path, lineNumber, key)
		}
		entries[key] = entry
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read pure-Go exec allowlist: %w", err)
	}
	return entries, nil
}

func pureGoExecClass(class string) bool {
	switch class {
	case "pinned-luau", "runner-fingerprint", "generator-check":
		return true
	default:
		return false
	}
}

func pureGoAllowlistKey(file, owner string) string {
	return filepath.ToSlash(file) + "::" + owner
}

func scanPureGoRepository(root string, allowlist map[string]pureGoExecAllowEntry) (pureGoScanResult, error) {
	result := pureGoScanResult{Launches: make(map[string]int)}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != root && filepath.Base(path) == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(filepath.Join(root, "testdata", "purego"))+"/") {
			return nil
		}
		pathResult, err := scanPureGoPath(root, path, allowlist)
		if err != nil {
			return err
		}
		result.Issues = append(result.Issues, pathResult.Issues...)
		for key, count := range pathResult.Launches {
			result.Launches[key] += count
		}
		return nil
	})
	if err != nil {
		return pureGoScanResult{}, fmt.Errorf("scan pure-Go repository: %w", err)
	}
	result.Issues = append(result.Issues, validatePureGoExecAllowlist(allowlist, result.Launches)...)
	return result, nil
}

func validatePureGoExecAllowlist(allowlist map[string]pureGoExecAllowEntry, launches map[string]int) []pureGoBoundaryIssue {
	var issues []pureGoBoundaryIssue
	for key, entry := range allowlist {
		if count := launches[key]; count != 1 {
			issues = append(issues, pureGoBoundaryIssue{
				Path:   entry.File,
				Detail: fmt.Sprintf("exec allowlist entry %s must match exactly one process launch, got %d", key, count),
			})
		}
	}
	return issues
}

func scanPureGoPath(root, path string, allowlist map[string]pureGoExecAllowEntry) (pureGoScanResult, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return pureGoScanResult{}, err
	}
	relative = filepath.ToSlash(relative)
	switch filepath.Ext(path) {
	case ".go":
		return scanPureGoFile(path, relative, allowlist)
	case ".c", ".cc", ".cpp", ".cxx", ".m", ".mm", ".h", ".hh", ".hpp", ".hxx", ".a", ".o", ".obj", ".so", ".dylib", ".dll", ".lib", ".syso", ".S":
		return pureGoScanResult{Issues: []pureGoBoundaryIssue{{Path: relative, Detail: "foreign source or object"}}, Launches: make(map[string]int)}, nil
	case ".s":
		return pureGoScanResult{Issues: scanPureGoAssembly(path, relative), Launches: make(map[string]int)}, nil
	default:
		return pureGoScanResult{Launches: make(map[string]int)}, nil
	}
}

func scanPureGoAssembly(path, relative string) []pureGoBoundaryIssue {
	contents, err := os.ReadFile(path)
	if err != nil {
		return []pureGoBoundaryIssue{{Path: relative, Detail: fmt.Sprintf("read assembly: %v", err)}}
	}
	lines := strings.Split(string(contents), "\n")
	labels := make(map[string]bool)
	for _, line := range lines {
		fields := strings.Fields(strings.SplitN(line, "//", 2)[0])
		if len(fields) > 0 && strings.HasSuffix(fields[0], ":") {
			labels[strings.TrimSuffix(fields[0], ":")] = true
		}
	}
	var issues []pureGoBoundaryIssue
	for lineNumber, line := range lines {
		line = strings.SplitN(line, "//", 2)[0]
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		fields := strings.Fields(line)
		for index, field := range fields {
			opcode := strings.ToUpper(strings.Trim(field, " \t,:"))
			if !pureGoAssemblyCallOpcode(opcode) {
				continue
			}
			if index+1 < len(fields) && pureGoAssemblyTarget(fields[index+1], labels) {
				break
			}
			issues = append(issues, pureGoBoundaryIssue{Path: relative, Detail: fmt.Sprintf("assembly call-like instruction %s has non-Go target at line %d", opcode, lineNumber+1)})
			break
		}
	}
	return issues
}

func pureGoAssemblyCallOpcode(opcode string) bool {
	switch opcode {
	case "CALL", "JMP", "B", "BR", "BL", "BLR", "J", "JR", "JAL", "JALR", "BX":
		return true
	default:
		return false
	}
}

func pureGoAssemblyTarget(field string, labels map[string]bool) bool {
	target := strings.Trim(field, " \t,")
	if labels[target] {
		return true
	}
	return strings.HasSuffix(target, "(SB)") && (strings.Contains(target, "·") || strings.Contains(target, "<>"))
}

func scanPureGoFile(path, relative string, allowlist map[string]pureGoExecAllowEntry) (pureGoScanResult, error) {
	fileSet := token.NewFileSet()
	file, err := goparser.ParseFile(fileSet, path, nil, goparser.ParseComments)
	if err != nil {
		return pureGoScanResult{}, fmt.Errorf("parse %s: %w", relative, err)
	}
	result := pureGoScanResult{Launches: make(map[string]int)}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := strings.TrimSpace(comment.Text)
			if strings.HasPrefix(text, "//go:linkname") || strings.HasPrefix(text, "/*go:linkname") {
				result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: "private go:linkname directive"})
			}
		}
	}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err == nil && importPath == "C" {
			result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: `imports "C"`})
		}
	}

	info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
	config := types.Config{
		Importer: importer.Default(),
		Error:    func(error) {},
	}
	_, _ = config.Check("purego/scan", fileSet, []*ast.File{file}, info)

	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			scanPureGoASTNode(relative, declaration.Name.Name, declaration, info, allowlist, &result)
		case *ast.GenDecl:
			scanPureGoASTNode(relative, pureGoPackageOwner, declaration, info, allowlist, &result)
		}
	}
	return result, nil
}

func scanPureGoASTNode(relative, owner string, node ast.Node, info *types.Info, allowlist map[string]pureGoExecAllowEntry, result *pureGoScanResult) {
	called := make(map[ast.Expr]bool)
	selected := make(map[*ast.Ident]bool)
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			called[node.Fun] = true
		case *ast.SelectorExpr:
			selected[node.Sel] = true
		}
		return true
	})
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			scanPureGoCall(relative, owner, node.Fun, info, allowlist, result)
		case *ast.SelectorExpr:
			if called[node] {
				return true
			}
			identifier, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			packageName, ok := info.Uses[identifier].(*types.PkgName)
			if !ok {
				return true
			}
			scanPureGoPackageUse(relative, owner, packageName.Imported().Path(), node.Sel.Name, false, allowlist, result)
		case *ast.Ident:
			if called[node] || selected[node] {
				return true
			}
			object := info.Uses[node]
			if object == nil || object.Pkg() == nil {
				return true
			}
			scanPureGoPackageUse(relative, owner, object.Pkg().Path(), object.Name(), false, allowlist, result)
		}
		return true
	})
}

func scanPureGoCall(relative, owner string, function ast.Expr, info *types.Info, allowlist map[string]pureGoExecAllowEntry, result *pureGoScanResult) {
	switch function := function.(type) {
	case *ast.SelectorExpr:
		identifier, ok := function.X.(*ast.Ident)
		if !ok {
			return
		}
		packageName, ok := info.Uses[identifier].(*types.PkgName)
		if ok {
			scanPureGoPackageUse(relative, owner, packageName.Imported().Path(), function.Sel.Name, true, allowlist, result)
		}
	case *ast.Ident:
		object := info.Uses[function]
		if object != nil && object.Pkg() != nil {
			scanPureGoPackageUse(relative, owner, object.Pkg().Path(), object.Name(), true, allowlist, result)
		}
	}
}

func scanPureGoPackageUse(relative, owner, importPath, name string, called bool, allowlist map[string]pureGoExecAllowEntry, result *pureGoScanResult) {
	if importPath == "os/exec" {
		if name == "LookPath" {
			return
		}
		if name != "Command" && name != "CommandContext" {
			result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: fmt.Sprintf("os/exec use %s is not an approved launch constructor", name)})
			return
		}
		if !called {
			result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: fmt.Sprintf("os/exec constructor escapes without a direct call: %s.%s", importPath, name)})
			return
		}
		key := pureGoAllowlistKey(relative, owner)
		result.Launches[key]++
		if owner == pureGoPackageOwner {
			result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: "package-level process launch"})
			return
		}
		if _, allowed := allowlist[key]; !allowed {
			result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: fmt.Sprintf("os/exec launch is not allowlisted: %s", key)})
		}
		return
	}
	if pureGoProcessAPI(importPath, name) {
		result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: fmt.Sprintf("forbidden process API %s.%s", importPath, name)})
		if owner == pureGoPackageOwner {
			result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: "package-level process launch"})
		}
		return
	}
	if pureGoForeignAPI(importPath, name) {
		result.Issues = append(result.Issues, pureGoBoundaryIssue{Path: relative, Detail: fmt.Sprintf("forbidden foreign or executable-memory API %s.%s", importPath, name)})
	}
}

func pureGoProcessAPI(importPath, name string) bool {
	if importPath == "os" {
		return name == "StartProcess"
	}
	if importPath == "syscall" || strings.HasSuffix(importPath, "/unix") {
		switch name {
		case "ForkExec", "StartProcess", "Exec":
			return true
		}
	}
	return false
}

func pureGoForeignAPI(importPath, name string) bool {
	if importPath == "plugin" {
		return name == "Open"
	}
	if importPath == "syscall" || strings.HasSuffix(importPath, "/unix") {
		if strings.HasPrefix(name, "Syscall") || strings.HasPrefix(name, "RawSyscall") {
			return true
		}
		switch name {
		case "Mmap", "Mprotect", "PROT_EXEC", "MAP_JIT":
			return true
		}
	}
	if strings.HasSuffix(importPath, "/windows") || importPath == "syscall" {
		if strings.HasPrefix(name, "LoadLibrary") || strings.HasPrefix(name, "VirtualAlloc") || strings.HasPrefix(name, "VirtualProtect") || strings.Contains(name, "LazyDLL") {
			return true
		}
		switch name {
		case "LoadDLL", "PAGE_EXECUTE", "PAGE_EXECUTE_READ", "PAGE_EXECUTE_READWRITE":
			return true
		}
	}
	if importPath == "github.com/ebitengine/purego" || strings.Contains(importPath, "wasmtime") || strings.Contains(importPath, "wasmer") || strings.Contains(importPath, "wazero") {
		return true
	}
	return false
}

func checkPureGoLinkedBinarySegments(path string) error {
	if file, err := elf.Open(path); err == nil {
		defer file.Close()
		for _, program := range file.Progs {
			if program.Type == elf.PT_LOAD && program.Flags&elf.PF_W != 0 && program.Flags&elf.PF_X != 0 {
				return fmt.Errorf("linked binary %s has writable+executable ELF load segment", path)
			}
		}
		return nil
	}
	if file, err := macho.Open(path); err == nil {
		defer file.Close()
		for _, load := range file.Loads {
			segment, ok := load.(*macho.Segment)
			if ok && segment.Prot&0x2 != 0 && segment.Prot&0x4 != 0 {
				return fmt.Errorf("linked binary %s has writable+executable Mach-O segment %s", path, segment.Name)
			}
		}
		return nil
	}
	if file, err := pe.Open(path); err == nil {
		defer file.Close()
		for _, section := range file.Sections {
			const (
				peExecutable = uint32(0x20000000)
				peWritable   = uint32(0x80000000)
			)
			if section.Characteristics&peExecutable != 0 && section.Characteristics&peWritable != 0 {
				return fmt.Errorf("linked binary %s has writable+executable PE section %s", path, section.Name)
			}
		}
		return nil
	}
	return fmt.Errorf("linked binary %s is not a recognized ELF, Mach-O, or PE executable", path)
}
