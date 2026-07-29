package preparedsource

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"
)

const validPackageSource = "package generated\n\nvar value = 1\n"

func validSet(t *testing.T) Set {
	t.Helper()
	set, err := NewSet("generated", []File{{Name: "generated.go", Kind: GoFile, Content: validPackageSource}})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func sourceFiles(count int, packageName string) []File {
	files := make([]File, count)
	for index := range files {
		files[index] = File{
			Name:    fmt.Sprintf("f%03d.go", index),
			Kind:    GoFile,
			Content: "package " + packageName + "\n",
		}
	}
	return files
}

func embeddedFiles(asset string, content string) []File {
	return []File{
		{
			Name: "embed.go", Kind: GoFile,
			Content: "package generated\nimport _ \"embed\"\n//go:embed " + asset + "\nvar data string\n",
		},
		{Name: asset, Kind: AssetFile, Content: content},
	}
}

func TestSetZeroAndDetachedCopies(t *testing.T) {
	var zero Set
	if !zero.IsZero() || zero.PackageName() != "" || zero.Digest() != ([sha256.Size]byte{}) || zero.Files() != nil {
		t.Fatalf("zero Set accessors = %t, %q, %x, %#v", zero.IsZero(), zero.PackageName(), zero.Digest(), zero.Files())
	}

	input := []File{
		{Name: "z.go", Kind: GoFile, Content: "package generated\nvar z = 1\n"},
		{Name: "a.go", Kind: GoFile, Content: "package generated\nvar a = 1\n"},
	}
	set, err := NewSet("generated", input)
	if err != nil {
		t.Fatal(err)
	}
	digest := set.Digest()
	input[0] = File{}
	returned := set.Files()
	if len(returned) != 2 || returned[0].Name != "a.go" || returned[1].Name != "z.go" {
		t.Fatalf("canonical Files = %#v", returned)
	}
	returned[0] = File{}
	fresh := set.Files()
	if fresh[0].Name != "a.go" || fresh[1].Name != "z.go" || set.PackageName() != "generated" || set.Digest() != digest {
		t.Fatalf("mutation escaped into Set: %#v", fresh)
	}
	copied := set
	if copied.IsZero() || copied.Digest() != digest {
		t.Fatalf("copied Set = %#v", copied)
	}
}

func TestSetCanonicalIdentityAndGolden(t *testing.T) {
	a := File{Name: "a.go", Kind: GoFile, Content: "package generated\nvar a = 1\n"}
	b := File{Name: "b.go", Kind: GoFile, Content: "package generated\nvar b = 2\n"}
	first, err := NewSet("generated", []File{a, b})
	if err != nil {
		t.Fatal(err)
	}
	permuted, err := NewSet("generated", []File{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != permuted.Digest() {
		t.Fatalf("permuted digest = %x, want %x", permuted.Digest(), first.Digest())
	}

	golden, err := NewSet("generated", []File{{Name: "generated.go", Kind: GoFile, Content: validPackageSource}})
	if err != nil {
		t.Fatal(err)
	}
	const wantSetDigest = "0423a2edf921c640954b5a34387794382ce30cb5923d25b682f10d5e2f063603"
	goldenDigest := golden.Digest()
	if got := hex.EncodeToString(goldenDigest[:]); got != wantSetDigest {
		t.Fatalf("Set digest = %s, want %s", got, wantSetDigest)
	}

	assertChanged := func(name string, changed Set, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if changed.Digest() == first.Digest() {
			t.Fatalf("%s change retained digest", name)
		}
	}
	changedPackage, err := NewSet("other", []File{
		{Name: "a.go", Kind: GoFile, Content: "package other\nvar a = 1\n"},
		{Name: "b.go", Kind: GoFile, Content: "package other\nvar b = 2\n"},
	})
	assertChanged("package", changedPackage, err)
	changedName, err := NewSet("generated", []File{{Name: "c.go", Kind: GoFile, Content: a.Content}, b})
	assertChanged("name", changedName, err)
	changedContent, err := NewSet("generated", []File{
		{Name: "a.go", Kind: GoFile, Content: "package generated\nvar a = 3\n"}, b,
	})
	assertChanged("content", changedContent, err)

	nameBoundaryLeft := digestSet("p", []File{{Name: "a", Kind: GoFile, Content: "bc"}})
	nameBoundaryRight := digestSet("p", []File{{Name: "ab", Kind: GoFile, Content: "c"}})
	if nameBoundaryLeft == nameBoundaryRight {
		t.Fatal("name/content frames are ambiguous")
	}
	goKind := digestSet("p", []File{{Name: "x", Kind: GoFile, Content: "same"}})
	assetKind := digestSet("p", []File{{Name: "x", Kind: AssetFile, Content: "same"}})
	if goKind == assetKind {
		t.Fatal("FileKind is absent from Set digest")
	}
}

func TestPackageNameGrammarBoundaries(t *testing.T) {
	valid := []string{"a", "generated", "a0_b", strings.Repeat("p", maximumPackageBytes)}
	for _, packageName := range valid {
		_, err := NewSet(packageName, []File{{Name: "x.go", Kind: GoFile, Content: "package " + packageName + "\n"}})
		if err != nil {
			t.Errorf("valid package %q: %v", packageName, err)
		}
	}
	invalid := []string{"", "_", "0bad", "Bad", "bad-name", "bad.name", "func", strings.Repeat("p", maximumPackageBytes+1), "café"}
	for _, packageName := range invalid {
		_, err := NewSet(packageName, []File{{Name: "x.go", Kind: GoFile, Content: "package x\n"}})
		if err == nil || !strings.Contains(err.Error(), "invalid package") {
			t.Errorf("invalid package %q error = %v", packageName, err)
		}
	}
}

func TestSetRejectsEmptyAndAssetOnlyInput(t *testing.T) {
	if _, err := NewSet("generated", nil); err == nil || !strings.Contains(err.Error(), "set is empty") {
		t.Fatalf("empty Set error = %v", err)
	}
	if _, err := NewSet("generated", []File{{Name: "data.bin", Kind: AssetFile, Content: "x"}}); err == nil || !strings.Contains(err.Error(), "no Go file") {
		t.Fatalf("asset-only Set error = %v", err)
	}
}

func TestGoAndAssetBasenameGrammarBoundaries(t *testing.T) {
	maximumGoName := "g" + strings.Repeat("x", maximumBasenameBytes-len("g.go")) + ".go"
	for _, name := range []string{"a.go", "generated_01.go", maximumGoName} {
		_, err := NewSet("generated", []File{{Name: name, Kind: GoFile, Content: validPackageSource}})
		if err != nil {
			t.Errorf("valid Go basename %q: %v", name, err)
		}
	}
	invalidGo := []string{
		"", "a", "A.go", "0a.go", "a-b.go", "a.test.go", "a_test.go",
		"a_linux.go", "a_amd64.go", "a_linux_amd64.go", "a_wasip1_wasm.go",
		"_a.go", ".a.go", "con.go", "com1.go", strings.Repeat("g", maximumBasenameBytes-len(".go")+1) + ".go",
	}
	for _, name := range invalidGo {
		_, err := NewSet("generated", []File{{Name: name, Kind: GoFile, Content: validPackageSource}})
		if err == nil {
			t.Errorf("invalid Go basename %q accepted", name)
		}
	}

	maximumAsset := "a" + strings.Repeat("x", maximumBasenameBytes-2) + "0"
	for _, asset := range []string{"a", "0", "data.bin", "data-file_1.bin", maximumAsset} {
		_, err := NewSet("generated", embeddedFiles(asset, "\x00\xffarbitrary"))
		if err != nil {
			t.Errorf("valid asset basename %q: %v", asset, err)
		}
	}
	invalidAssets := []string{
		"", "-a", "a-", ".a", "a.", "A.bin", "data file", "data/bin", "data\\bin",
		"data?.bin", "go.mod", "go.sum", "con", "con.bin", "lpt9.asset", "data.go", "data.c", "data.cpp", "data.h",
		"data.s", "data.syso", "data.a", "data.obj", "data.exe", "default.pgo", "café.bin",
		"a" + strings.Repeat("x", maximumBasenameBytes),
	}
	for _, asset := range invalidAssets {
		files := []File{{Name: "generated.go", Kind: GoFile, Content: validPackageSource}, {Name: asset, Kind: AssetFile, Content: "x"}}
		_, err := NewSet("generated", files)
		if err == nil {
			t.Errorf("invalid asset basename %q accepted", asset)
		}
	}

	_, err := NewSet("generated", []File{
		{Name: "same.go", Kind: GoFile, Content: validPackageSource},
		{Name: "same.go", Kind: GoFile, Content: validPackageSource},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate file error = %v", err)
	}
	_, err = NewSet("generated", []File{{Name: "x.go", Kind: FileKind(99), Content: validPackageSource}})
	if err == nil || !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("invalid FileKind error = %v", err)
	}
}

func TestGoSyntaxPackageImportAndDirectivePolicy(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "syntax", content: "package generated\nfunc (", want: "unclosed delimiter"},
		{name: "package mismatch", content: "package other\n", want: "declares package"},
		{name: "C import", content: "package generated\nimport \"C\"\n", want: "imports C"},
		{name: "import control", content: "package generated\nimport _ \"bad\\x01path\"\n", want: "invalid import"},
		{name: "import space", content: "package generated\nimport _ \"bad path\"\n", want: "invalid import"},
		{name: "import punctuation", content: "package generated\nimport _ \"bad:path\"\n", want: "invalid import"},
		{name: "import empty component", content: "package generated\nimport _ \"bad//path\"\n", want: "invalid import"},
		{name: "init", content: "package generated\nfunc init() {}\n", want: "declares init"},
		{name: "go build", content: "//go:build linux\n\npackage generated\n", want: "build constraint"},
		{name: "plus build spaces", content: "// +build linux darwin\n\npackage generated\n", want: "build constraint"},
		{name: "plus build tabs", content: "// +build linux\tdarwin\n\npackage generated\n", want: "build constraint"},
		{name: "line comment", content: "//line generated.go:1\npackage generated\n", want: "line directive"},
		{name: "line block", content: "/*line generated.go:1*/package generated\n", want: "line directive"},
		{name: "linkname", content: "package generated\n//go:linkname local remote\nvar local int\n", want: "unsupported directive"},
		{name: "generate", content: "package generated\n//go:generate false\nvar value int\n", want: "unsupported directive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSet("generated", []File{{Name: "generated.go", Kind: GoFile, Content: test.content}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}

	validImport := "package generated\nimport _ \"github.com/besmpl/ember/preparedsource\"\nvar value = 1\n"
	if _, err := NewSet("generated", []File{{Name: "generated.go", Kind: GoFile, Content: validImport}}); err != nil {
		t.Fatalf("valid import rejected: %v", err)
	}
}

func TestExactEmbedDeclarationPolicy(t *testing.T) {
	if _, err := NewSet("generated", embeddedFiles("data.bin", "\x00\xff")); err != nil {
		t.Fatalf("valid embed rejected: %v", err)
	}
	withOrdinaryDoc := []File{
		{Name: "embed.go", Kind: GoFile, Content: "package generated\nimport _ \"embed\"\n// data owns one asset.\n//go:embed data.bin\nvar data string\n"},
		{Name: "data.bin", Kind: AssetFile, Content: "x"},
	}
	if _, err := NewSet("generated", withOrdinaryDoc); err != nil {
		t.Fatalf("ordinary doc plus sole directive rejected: %v", err)
	}
	splitImport := []File{
		{Name: "embed.go", Kind: GoFile, Content: "package generated\nimport _ \"embed\"\n"},
		{Name: "generated.go", Kind: GoFile, Content: "package generated\n//go:embed data.bin\nvar data string\n"},
		{Name: "data.bin", Kind: AssetFile, Content: "x"},
	}
	if _, err := NewSet("generated", splitImport); err == nil || !strings.Contains(err.Error(), "blank embed import") {
		t.Fatalf("split-file embed import error = %v", err)
	}

	tests := []struct {
		name    string
		content string
		assets  []string
		want    string
	}{
		{name: "missing blank import", content: "package generated\n//go:embed data.bin\nvar data string\n", assets: []string{"data.bin"}, want: "blank embed import"},
		{name: "named embed import", content: "package generated\nimport \"embed\"\n//go:embed data.bin\nvar data string\n", assets: []string{"data.bin"}, want: "blank identifier"},
		{name: "escaped embed import", content: "package generated\nimport _ \"em\\x62ed\"\n//go:embed data.bin\nvar data string\n", assets: []string{"data.bin"}, want: "blank identifier"},
		{name: "raw embed import", content: "package generated\nimport _ `embed`\n//go:embed data.bin\nvar data string\n", assets: []string{"data.bin"}, want: "blank identifier"},
		{name: "duplicate embed import", content: "package generated\nimport (\n_ \"embed\"\n_ \"embed\"\n)\n//go:embed data.bin\nvar data string\n", assets: []string{"data.bin"}, want: "exactly one blank embed import"},
		{name: "import without asset", content: "package generated\nimport _ \"embed\"\nvar data string\n", want: "requires an asset"},
		{name: "quoted", content: "package generated\nimport _ \"embed\"\n//go:embed \"data.bin\"\nvar data string\n", assets: []string{"data.bin"}, want: "malformed embed directive"},
		{name: "glob", content: "package generated\nimport _ \"embed\"\n//go:embed *.bin\nvar data string\n", assets: []string{"data.bin"}, want: "malformed embed directive"},
		{name: "two patterns", content: "package generated\nimport _ \"embed\"\n//go:embed a.bin b.bin\nvar data string\n", assets: []string{"a.bin", "b.bin"}, want: "malformed embed directive"},
		{name: "trailing space", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin \nvar data string\n", assets: []string{"data.bin"}, want: "malformed embed directive"},
		{name: "undeclared", content: "package generated\nimport _ \"embed\"\n//go:embed missing.bin\nvar data string\n", assets: []string{"data.bin"}, want: "undeclared asset"},
		{name: "unattached function", content: "package generated\nimport _ \"embed\"\nfunc f() {\n//go:embed data.bin\nvar data string\n_ = data\n}\n", assets: []string{"data.bin"}, want: "unattached"},
		{name: "value spec doc", content: "package generated\nimport _ \"embed\"\nvar (\n//go:embed data.bin\ndata string\n)\n", assets: []string{"data.bin"}, want: "unattached"},
		{name: "multiple directives", content: "package generated\nimport _ \"embed\"\n//go:embed a.bin\n//go:embed b.bin\nvar data string\n", assets: []string{"a.bin", "b.bin"}, want: "one directive"},
		{name: "multiple specs", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar (\ndata string\nother string\n)\n", assets: []string{"data.bin"}, want: "one variable"},
		{name: "multiple names", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar data, other string\n", assets: []string{"data.bin"}, want: "one explicit"},
		{name: "blank name", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar _ string\n", assets: []string{"data.bin"}, want: "one explicit"},
		{name: "inferred type", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar data\n", assets: []string{"data.bin"}, want: "parse Go file"},
		{name: "initializer", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar data string = \"\"\n", assets: []string{"data.bin"}, want: "uninitialized string"},
		{name: "wrong type", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar data []byte\n", assets: []string{"data.bin"}, want: "uninitialized string"},
		{name: "duplicate reference", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar data string\n//go:embed data.bin\nvar other string\n", assets: []string{"data.bin"}, want: "more than once"},
		{name: "unused asset", content: "package generated\nimport _ \"embed\"\n//go:embed data.bin\nvar data string\n", assets: []string{"data.bin", "other.bin"}, want: "not referenced"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := []File{{Name: "generated.go", Kind: GoFile, Content: test.content}}
			for _, asset := range test.assets {
				files = append(files, File{Name: asset, Kind: AssetFile, Content: "x"})
			}
			_, err := NewSet("generated", files)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestScannerPreflightBoundaries(t *testing.T) {
	if err := scanGoSource("x.go", "package p\nvar x = ((1))\n", 20, 2); err != nil {
		t.Fatalf("exact depth boundary rejected: %v", err)
	}
	if err := scanGoSource("x.go", "package p\nvar x = 1\n", 3, 10); err == nil || !strings.Contains(err.Error(), "token limit") {
		t.Fatalf("token limit error = %v", err)
	}
	if err := scanGoSource("x.go", "package p\nvar x = (((1)))\n", 50, 2); err == nil || !strings.Contains(err.Error(), "delimiter depth") {
		t.Fatalf("depth limit error = %v", err)
	}
	if err := scanGoSource("x.go", "package p\nvar x = ([1)]\n", 50, 10); err == nil || !strings.Contains(err.Error(), "mismatched delimiter") {
		t.Fatalf("mismatched delimiter error = %v", err)
	}
	if err := scanGoSource("empty.go", "", 1, 1); err != nil {
		t.Fatalf("exact token boundary rejected: %v", err)
	}
	if err := scanGoSource("empty.go", "", 0, 1); err == nil || !strings.Contains(err.Error(), "token limit") {
		t.Fatalf("token boundary error = %v", err)
	}
	if err := scanGoSource("x.go", "package p\nvar x = \"\\xzz\"\n", 50, 10); err == nil || !strings.Contains(err.Error(), "scan Go file") {
		t.Fatalf("scan error = %v", err)
	}
	if maximumGoTokens != 4_000_000 || maximumDelimiterDepth != 1024 {
		t.Fatalf("production scan limits = %d/%d", maximumGoTokens, maximumDelimiterDepth)
	}
}

func TestSetLimits(t *testing.T) {
	if _, err := NewSet("generated", sourceFiles(maximumFileCount, "generated")); err != nil {
		t.Fatalf("maximum file count rejected: %v", err)
	}
	if _, err := NewSet("generated", sourceFiles(maximumFileCount+1, "generated")); err == nil || !strings.Contains(err.Error(), "file count") {
		t.Fatalf("file-count error = %v", err)
	}
	if got, err := addContentBytes(0, "maximum.bin", maximumFileBytes); err != nil || got != maximumFileBytes {
		t.Fatalf("per-file boundary = %d, %v", got, err)
	}
	if _, err := addContentBytes(0, "large.bin", maximumFileBytes+1); err == nil || !strings.Contains(err.Error(), "file") {
		t.Fatalf("per-file error = %v", err)
	}
	if got, err := addContentBytes(maximumSetBytes-1, "last.bin", 1); err != nil || got != maximumSetBytes {
		t.Fatalf("aggregate boundary = %d, %v", got, err)
	}
	if _, err := addContentBytes(maximumSetBytes, "overflow.bin", 1); err == nil || !strings.Contains(err.Error(), "aggregate") {
		t.Fatalf("aggregate error = %v", err)
	}
	if _, err := addContentBytes(math.MaxUint64, "overflow.bin", 1); err == nil {
		t.Fatal("overflowing aggregate accepted")
	}
}

func TestLayoutZeroCanonicalCopiesIdentityAndGolden(t *testing.T) {
	var zero Layout
	if !zero.IsZero() || zero.Digest() != ([sha256.Size]byte{}) || zero.Mounts() != nil {
		t.Fatalf("zero Layout accessors = %t, %x, %#v", zero.IsZero(), zero.Digest(), zero.Mounts())
	}
	set := validSet(t)
	input := []Mount{{Path: "z/generated", Set: set}, {Path: "a/generated", Set: set}}
	layout, err := NewLayout(input)
	if err != nil {
		t.Fatal(err)
	}
	digest := layout.Digest()
	input[0] = Mount{}
	returned := layout.Mounts()
	if len(returned) != 2 || returned[0].Path != "a/generated" || returned[1].Path != "z/generated" {
		t.Fatalf("canonical Mounts = %#v", returned)
	}
	returned[0] = Mount{}
	fresh := layout.Mounts()
	if fresh[0].Path != "a/generated" || fresh[1].Set.IsZero() || layout.Digest() != digest {
		t.Fatalf("mutation escaped into Layout: %#v", fresh)
	}
	copied := layout
	if copied.IsZero() || copied.Digest() != digest {
		t.Fatalf("copied Layout = %#v", copied)
	}

	permuted, err := NewLayout([]Mount{{Path: "a/generated", Set: set}, {Path: "z/generated", Set: set}})
	if err != nil {
		t.Fatal(err)
	}
	if permuted.Digest() != layout.Digest() {
		t.Fatal("mount permutation changed Layout digest")
	}
	one, err := NewLayout([]Mount{{Path: "internal/generated", Set: set}})
	if err != nil {
		t.Fatal(err)
	}
	const wantLayoutDigest = "2045be4413cefa8eb91875b3e7c95880f7b72febc829081b49e190c12013bf71"
	oneDigest := one.Digest()
	if got := hex.EncodeToString(oneDigest[:]); got != wantLayoutDigest {
		t.Fatalf("Layout digest = %s, want %s", got, wantLayoutDigest)
	}
	if one.Digest() == set.Digest() {
		t.Fatal("Set and Layout digest domains are not separated")
	}
	changedPath, err := NewLayout([]Mount{{Path: "internal/other", Set: set}})
	if err != nil {
		t.Fatal(err)
	}
	if changedPath.Digest() == one.Digest() {
		t.Fatal("mount path change retained Layout digest")
	}
	other, err := NewSet("generated", []File{{Name: "generated.go", Kind: GoFile, Content: "package generated\nvar value = 2\n"}})
	if err != nil {
		t.Fatal(err)
	}
	changedSet, err := NewLayout([]Mount{{Path: "internal/generated", Set: other}})
	if err != nil {
		t.Fatal(err)
	}
	if changedSet.Digest() == one.Digest() {
		t.Fatal("mounted Set change retained Layout digest")
	}
}

func TestMountGrammarBoundaries(t *testing.T) {
	set := validSet(t)
	component64 := "a" + strings.Repeat("b", 62) + "0"
	components := make([]string, maximumMountComponents)
	for index := range components {
		components[index] = "a"
	}
	components32 := strings.Join(components, "/")
	component60 := "a" + strings.Repeat("b", 58) + "0"
	component59 := "a" + strings.Repeat("b", 57) + "0"
	path240 := strings.Join([]string{component60, component59, component59, component59}, "/")
	if len(path240) != maximumMountBytes {
		t.Fatalf("test path length = %d", len(path240))
	}
	for _, mountPath := range []string{"a", "a0_b-c", "internal/generated", component64, components32, path240} {
		if _, err := NewLayout([]Mount{{Path: mountPath, Set: set}}); err != nil {
			t.Errorf("valid mount path %q: %v", mountPath, err)
		}
	}

	components33 := append(append([]string(nil), components...), "a")
	invalid := []string{
		"", ".", "..", "/absolute", "a/", "/a", "a//b", "a/./b", "a/../b",
		"A", "a.b", "-a", "a-", "_a", "a_", "a\\b", "a:b", "café",
		"con", "prn", "aux", "nul", "com1", "lpt9", "conin$", "conout$",
		strings.Repeat("a", maximumBasenameBytes+1), strings.Join(components33, "/"), path240 + "a",
	}
	for _, mountPath := range invalid {
		if _, err := NewLayout([]Mount{{Path: mountPath, Set: set}}); err == nil {
			t.Errorf("invalid mount path %q accepted", mountPath)
		}
	}

	if _, err := NewLayout([]Mount{{Path: "same", Set: set}, {Path: "same", Set: set}}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate mount error = %v", err)
	}
	if _, err := NewLayout(nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty Layout error = %v", err)
	}
	if _, err := NewLayout([]Mount{{Path: "generated"}}); err == nil || !strings.Contains(err.Error(), "zero Set") {
		t.Fatalf("zero Set error = %v", err)
	}
}

func TestLayoutLimits(t *testing.T) {
	one := validSet(t)
	mounts := make([]Mount, maximumMountCount)
	for index := range mounts {
		mounts[index] = Mount{Path: fmt.Sprintf("p%02d", index), Set: one}
	}
	if _, err := NewLayout(mounts); err != nil {
		t.Fatalf("maximum mount count rejected: %v", err)
	}
	tooMany := append(append([]Mount(nil), mounts...), Mount{Path: "p64", Set: one})
	if _, err := NewLayout(tooMany); err == nil || !strings.Contains(err.Error(), "mount count") {
		t.Fatalf("mount-count error = %v", err)
	}

	set64, err := NewSet("generated", sourceFiles(64, "generated"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range mounts {
		mounts[index].Set = set64
	}
	if _, err := NewLayout(mounts); err != nil {
		t.Fatalf("maximum repeated leaves rejected: %v", err)
	}
	set65, err := NewSet("generated", sourceFiles(65, "generated"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range mounts {
		mounts[index].Set = set65
	}
	if _, err := NewLayout(mounts); err == nil || !strings.Contains(err.Error(), "mounted leaf") {
		t.Fatalf("repeated leaf error = %v", err)
	}

	if leaves, bytes, err := addMountedTotals(maximumMountedLeaves-1, maximumMountedBytes-1, 1, 1); err != nil || leaves != maximumMountedLeaves || bytes != maximumMountedBytes {
		t.Fatalf("mounted aggregate boundary = %d/%d, %v", leaves, bytes, err)
	}
	if _, _, err := addMountedTotals(maximumMountedLeaves, 0, 1, 0); err == nil || !strings.Contains(err.Error(), "leaf") {
		t.Fatalf("mounted leaf overflow error = %v", err)
	}
	if _, _, err := addMountedTotals(0, maximumMountedBytes, 0, 1); err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("mounted byte overflow error = %v", err)
	}
	if _, _, err := addMountedTotals(math.MaxUint64, math.MaxUint64, 1, 1); err == nil {
		t.Fatal("integer-overflowing mounted totals accepted")
	}
}
