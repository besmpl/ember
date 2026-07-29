package producer_test

import (
	"testing"

	producer "example.com/ember-external-producer"
	"github.com/besmpl/ember/preparedsource"
)

func TestCanonicalIdentitiesAndPlacementIndependence(t *testing.T) {
	ruby, sprig, err := producer.Sets(false)
	if err != nil {
		t.Fatal(err)
	}
	rubyPermuted, sprigPermuted, err := producer.Sets(true)
	if err != nil {
		t.Fatal(err)
	}
	if ruby.IsZero() || sprig.IsZero() || ruby.Digest() == sprig.Digest() {
		t.Fatalf("sets are not two distinct nonzero values: ruby=%x sprig=%x", ruby.Digest(), sprig.Digest())
	}
	if ruby.Digest() != rubyPermuted.Digest() || sprig.Digest() != sprigPermuted.Digest() {
		t.Fatal("file permutation changed a Set identity")
	}

	canonical, err := preparedsource.NewLayout([]preparedsource.Mount{
		{Path: "generated/ruby", Set: ruby},
		{Path: "generated/sprig", Set: sprig},
	})
	if err != nil {
		t.Fatal(err)
	}
	permuted, err := preparedsource.NewLayout([]preparedsource.Mount{
		{Path: "generated/sprig", Set: sprigPermuted},
		{Path: "generated/ruby", Set: rubyPermuted},
	})
	if err != nil {
		t.Fatal(err)
	}
	if canonical.IsZero() || canonical.Digest() != permuted.Digest() {
		t.Fatalf("mount permutation changed Layout identity: %x != %x", canonical.Digest(), permuted.Digest())
	}

	alternate, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "somewhere/else", Set: ruby}})
	if err != nil {
		t.Fatal(err)
	}
	if alternate.Mounts()[0].Set.Digest() != ruby.Digest() {
		t.Fatal("mount placement changed the producer-owned Set identity")
	}
}

func TestGeneratedShapesDoNotClaimLanguageCompatibility(t *testing.T) {
	ruby, sprig, err := producer.Sets(false)
	if err != nil {
		t.Fatal(err)
	}
	for name, set := range map[string]preparedsource.Set{"ruby": ruby, "sprig": sprig} {
		for _, file := range set.Files() {
			if file.Name == "scope.go" {
				continue
			}
			if name == "ruby" && file.Content != "" && !contains(file.Content, "not Ruby syntax or semantics") {
				t.Fatal("Ruby-shaped source does not disclaim compatibility")
			}
			if name == "sprig" && file.Content != "" && !contains(file.Content, "not Sprig syntax or semantics") {
				t.Fatal("Sprig-shaped source does not disclaim compatibility")
			}
		}
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
