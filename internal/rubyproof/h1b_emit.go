package rubyproof

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/format"
	"text/template"

	"github.com/besmpl/ember/preparedsource"
)

// PreparedSource emits the exact H1b product as one dependency-free Go
// package. The generated owner has its own fixed-array representation; it
// shares checked scalar facts with the canonical product, not runtime types.
func (p H1bProgram) PreparedSource(packageName string) (preparedsource.Set, error) {
	if p.checked == nil {
		return preparedsource.Set{}, fmt.Errorf("ruby: zero H1b program")
	}
	if !validGeneratedPackageName(packageName) {
		return preparedsource.Set{}, fmt.Errorf("ruby: invalid generated Go package name %q", packageName)
	}
	if err := validateH1bEmissionPlan(p.checked); err != nil {
		return preparedsource.Set{}, err
	}
	parsed, err := template.New("ruby-h1b-generated").Parse(h1bPreparedTemplate)
	if err != nil {
		return preparedsource.Set{}, fmt.Errorf("ruby: parse private H1b prepared template: %w", err)
	}
	var raw bytes.Buffer
	if err := parsed.Execute(&raw, struct{ Package string }{Package: packageName}); err != nil {
		return preparedsource.Set{}, fmt.Errorf("ruby: execute private H1b prepared template: %w", err)
	}
	formatted, err := format.Source(raw.Bytes())
	if err != nil {
		return preparedsource.Set{}, fmt.Errorf("ruby: format generated H1b Go: %w\n%s", err, raw.String())
	}
	return preparedsource.NewSet(packageName, []preparedsource.File{{
		Name: "ruby_h1b_generated.go", Kind: preparedsource.GoFile, Content: string(formatted),
	}})
}

func validateH1bEmissionPlan(plan *checkedH1bPlan) error {
	if plan == nil || plan.sourceHash != sha256.Sum256([]byte(H1bSource)) ||
		plan.sourceBytes != 718 || plan.sourceLineFeeds != 35 ||
		plan.class.dispatch != 1 || plan.class.shape != 1 || plan.class.ordinaryValue != 7 ||
		plan.call.site != 1 || plan.call.selector != h1SidecarSelector ||
		plan.call.singletonTarget != h1SidecarTarget || plan.call.visibility != lookupVisibilityPublic ||
		plan.capture.cell != 0 || plan.capture.initial != 40 ||
		plan.capture.bodyIncrement != 1 || plan.capture.helperIncrement != 2 ||
		plan.roots != (h1bRootPlan{topSlots: 2, installObjectRoots: 1, maximumSlots: 3}) ||
		plan.capacity.objects != 4 || plan.capacity.environments != 2 ||
		plan.capacity.roots != 8 || plan.capacity.markWork != 6 ||
		plan.capacity.referenceIndexBits != 8 ||
		plan.capacity.maximumGeneration != h1SidecarMaximumGeneration ||
		plan.result != [7]int64{7, 7, 7, 43, 44, 7, 7} {
		return fmt.Errorf("ruby: checked H1b program is outside prepared proof shape")
	}
	wantCollections := [3]h1bCollectionPlan{
		{id: h1bHelperCollection, markedObjects: 2, markedEnvironments: 1, markWork: 3},
		{id: h1bReceiverCollection, markedObjects: 1, reclaimedObjects: 1, reclaimedEnvironments: 1, markWork: 1},
		{id: h1bPeerCollection, reclaimedObjects: 1},
	}
	if plan.collections != wantCollections {
		return fmt.Errorf("ruby: checked H1b collection plan is outside prepared proof shape")
	}
	return nil
}
