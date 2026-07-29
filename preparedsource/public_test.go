package preparedsource_test

import (
	"testing"

	"github.com/besmpl/ember/preparedsource"
)

func TestPublicSetAndLayoutOwnership(t *testing.T) {
	input := []preparedsource.File{
		{Name: "z.go", Kind: preparedsource.GoFile, Content: "package generated\nvar z = 1\n"},
		{Name: "a.go", Kind: preparedsource.GoFile, Content: "package generated\nvar a = 1\n"},
	}
	set, err := preparedsource.NewSet("generated", input)
	if err != nil {
		t.Fatal(err)
	}
	setDigest := set.Digest()
	input[0] = preparedsource.File{}
	returnedFiles := set.Files()
	returnedFiles[0] = preparedsource.File{}
	if set.IsZero() || set.PackageName() != "generated" || set.Digest() != setDigest {
		t.Fatalf("caller mutation changed Set metadata: %#v", set)
	}
	if fresh := set.Files(); len(fresh) != 2 || fresh[0].Name != "a.go" || fresh[1].Name != "z.go" {
		t.Fatalf("caller mutation changed Set files: %#v", fresh)
	}

	mountInput := []preparedsource.Mount{
		{Path: "z/generated", Set: set},
		{Path: "a/generated", Set: set},
	}
	layout, err := preparedsource.NewLayout(mountInput)
	if err != nil {
		t.Fatal(err)
	}
	layoutDigest := layout.Digest()
	mountInput[0] = preparedsource.Mount{}
	returnedMounts := layout.Mounts()
	returnedMounts[0] = preparedsource.Mount{}
	if layout.IsZero() || layout.Digest() != layoutDigest {
		t.Fatalf("caller mutation changed Layout metadata: %#v", layout)
	}
	if fresh := layout.Mounts(); len(fresh) != 2 || fresh[0].Path != "a/generated" || fresh[1].Path != "z/generated" {
		t.Fatalf("caller mutation changed Layout mounts: %#v", fresh)
	}
}

func TestPublicCanonicalPermutation(t *testing.T) {
	a := preparedsource.File{Name: "a.go", Kind: preparedsource.GoFile, Content: "package generated\nvar a = 1\n"}
	b := preparedsource.File{Name: "b.go", Kind: preparedsource.GoFile, Content: "package generated\nvar b = 2\n"}
	first, err := preparedsource.NewSet("generated", []preparedsource.File{a, b})
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparedsource.NewSet("generated", []preparedsource.File{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("Set permutation changed digest: %x != %x", first.Digest(), second.Digest())
	}
	firstLayout, err := preparedsource.NewLayout([]preparedsource.Mount{
		{Path: "b/generated", Set: first},
		{Path: "a/generated", Set: second},
	})
	if err != nil {
		t.Fatal(err)
	}
	secondLayout, err := preparedsource.NewLayout([]preparedsource.Mount{
		{Path: "a/generated", Set: second},
		{Path: "b/generated", Set: first},
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstLayout.Digest() != secondLayout.Digest() {
		t.Fatalf("Layout permutation changed digest: %x != %x", firstLayout.Digest(), secondLayout.Digest())
	}
}

func TestPublicZeroValues(t *testing.T) {
	var set preparedsource.Set
	var layout preparedsource.Layout
	if !set.IsZero() || set.PackageName() != "" || set.Files() != nil {
		t.Fatalf("zero Set = %#v", set)
	}
	if !layout.IsZero() || layout.Mounts() != nil {
		t.Fatalf("zero Layout = %#v", layout)
	}
	if _, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "generated", Set: set}}); err == nil {
		t.Fatal("NewLayout accepted a zero Set")
	}
}
