// Package sprigproof is a provisional, proof-only functional language.
//
// Its syntax, diagnostics, and API are intentionally internal while ADR 0012's
// Sprig semantic baseline is evaluated. Integer addition is checked, division
// truncates toward zero, and divide-by-zero plus MinInt64/-1 are typed domain
// results. It does not use Ember's Luau runtime.
package sprigproof

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/besmpl/ember/preparedsource"
)

const (
	maximumSourceBytes  = 64 << 10
	maximumTokens       = 8192
	maximumNodes        = 4096
	maximumDiagnostics  = 128
	maximumInput        = 4096
	maximumListElements = 4096
	maximumImportBytes  = 512
)

// Span is a half-open byte range with one-based line and column positions.
type Span struct {
	Start, End             int
	StartLine, StartColumn int
	EndLine, EndColumn     int
}

// Diagnostic is one deterministic source diagnostic.
type Diagnostic struct {
	Span    Span
	Message string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d-%d:%d: %s", d.Span.StartLine, d.Span.StartColumn, d.Span.EndLine, d.Span.EndColumn, d.Message)
}

// Reading is a detached immutable-by-contract source record.
type Reading struct {
	Value   int64
	Divisor int64
}

// ResultTag identifies the Result union variant.
type ResultTag uint8

const (
	ResultOK ResultTag = iota + 1
	ResultError
)

// DomainCode is a language-level failure value, not a Go execution error.
type DomainCode int64

const (
	DomainOverflow     DomainCode = 1
	DomainDivideByZero DomainCode = 2
)

// Result is the detached return-by-value projection of the Result union.
type Result struct {
	Tag      ResultTag
	Total    int64
	Rejected []int64
	Code     DomainCode
}

// ErrStepLimit is returned when one call exhausts its independent budget.
var ErrStepLimit = errors.New("sprig: step limit exceeded")

// ErrListLimit is returned before an append would exceed the per-call list
// bound.
var ErrListLimit = errors.New("sprig: list element limit exceeded")

// Program is an immutable, language-owned checked program. Its representation
// is private; it is not a shared compiler or runtime IR.
type Program struct{ checked *checkedProgram }

// PackageID is the nominal, canonical lowercase identity written in a source
// package header. It is deliberately independent of application placement.
type PackageID string

// DeclID, VariantID, FieldID, and BindingID are package-local nominal IDs.
// They are assigned from canonical declaration and lexical order, never from
// user spelling.
type DeclID uint32
type VariantID uint32
type FieldID uint32
type BindingID uint32

// SourcePackage is one effect-free project input. ID must exactly match the
// source package header.
type SourcePackage struct {
	ID     PackageID
	Source string
}

// PackageBinding assigns one checked package its final application-owned Go
// import path. Bindings affect linked generated source, never checked project
// or package identity. The application still owns preparedsource.Layout and
// all module and mount policy.
type PackageBinding struct {
	ID         PackageID
	ImportPath string
}

// Package is an immutable checked package record.
type Package struct {
	id             PackageID
	sourceIdentity [sha256.Size]byte
	identity       [sha256.Size]byte
}

func (p Package) ID() PackageID                     { return p.id }
func (p Package) Identity() [sha256.Size]byte       { return p.identity }
func (p Package) SourceIdentity() [sha256.Size]byte { return p.sourceIdentity }

// PreparedPackage is one concrete generated package result. Its Set carries no
// mount, module, build, or cache policy.
type PreparedPackage struct {
	pkg Package
	set preparedsource.Set
}

func (p PreparedPackage) Package() Package            { return p.pkg }
func (p PreparedPackage) Set() preparedsource.Set     { return p.set }
func (p PreparedPackage) Identity() [sha256.Size]byte { return p.pkg.identity }

// Project is an immutable, canonically ordered checked package graph.
type Project struct{ checked *checkedProject }

func (p Project) IsZero() bool { return p.checked == nil }
func (p Project) Identity() [sha256.Size]byte {
	if p.checked == nil {
		return [sha256.Size]byte{}
	}
	return p.checked.identity
}
func (p Project) Packages() []Package {
	if p.checked == nil {
		return nil
	}
	out := make([]Package, len(p.checked.order))
	for i, id := range p.checked.order {
		pkg := p.checked.packages[id]
		out[i] = Package{id: id, sourceIdentity: pkg.sourceIdentity, identity: pkg.identity}
	}
	return out
}

// Program returns the canonical evaluator adapter for a package's exported
// Reduce entry when present.
func (p Project) Program(id PackageID) (Program, bool) {
	if p.checked == nil {
		return Program{}, false
	}
	pkg := p.checked.packages[id]
	if pkg == nil {
		return Program{}, false
	}
	fn, ok := pkg.functions["Reduce"]
	if !ok {
		return Program{}, false
	}
	return Program{checked: &checkedProgram{project: p.checked, pkg: pkg, module: pkg.module, function: fn, variants: pkg.variants, expressionTypes: pkg.expressionTypes}}, true
}

// PrepareStandalone emits one independently mountable immutable Set per requested
// guest package in canonical PackageID order. Each Set privately includes the
// full reachable imported function closure because application-owned mount
// paths are intentionally unknown to the producer. Unrequested packages are
// checked but do not produce code.
func (p Project) PrepareStandalone(entries []PackageID) ([]PreparedPackage, error) {
	if p.checked == nil {
		return nil, errors.New("sprig: zero project")
	}
	ids, err := p.canonicalEntries(entries)
	if err != nil {
		return nil, err
	}
	out := make([]PreparedPackage, 0, len(ids))
	for _, id := range ids {
		pkg := p.checked.packages[id]
		set, err := emitPackage(p.checked, pkg, string(id))
		if err != nil {
			return nil, err
		}
		out = append(out, PreparedPackage{pkg: Package{id: id, sourceIdentity: pkg.sourceIdentity, identity: pkg.identity}, set: set})
	}
	return slices.Clone(out), nil
}

// PrepareLinked emits the requested entries and their complete transitive
// package closure as ordinary mutually importing Go packages. The caller must
// bind every package in that closure to a distinct final Go import path. A
// validated project-wide binding table is also accepted so callers need not
// rediscover the private dependency graph; only requested reachable packages
// emit code. Results are in canonical PackageID order. This is an explicit
// application-bound product, not an adaptive mode.
func (p Project) PrepareLinked(entries []PackageID, bindings []PackageBinding) ([]PreparedPackage, error) {
	if p.checked == nil {
		return nil, errors.New("sprig: zero project")
	}
	requested, err := p.canonicalEntries(entries)
	if err != nil {
		return nil, err
	}
	reachable := p.reachablePackages(requested)
	paths, err := p.canonicalBindings(reachable, bindings)
	if err != nil {
		return nil, err
	}
	out := make([]PreparedPackage, 0, len(reachable))
	for _, id := range reachable {
		pkg := p.checked.packages[id]
		set, err := emitLinkedPackage(p.checked, pkg, string(id), paths)
		if err != nil {
			return nil, err
		}
		out = append(out, PreparedPackage{pkg: Package{id: id, sourceIdentity: pkg.sourceIdentity, identity: pkg.identity}, set: set})
	}
	return slices.Clone(out), nil
}

func (p Project) canonicalEntries(entries []PackageID) ([]PackageID, error) {
	if len(entries) == 0 {
		return nil, errors.New("sprig: no package entries")
	}
	ids := slices.Clone(entries)
	slices.Sort(ids)
	for i, id := range ids {
		if i > 0 && ids[i-1] == id {
			return nil, fmt.Errorf("sprig: duplicate package entry %q", id)
		}
		if p.checked.packages[id] == nil {
			return nil, fmt.Errorf("sprig: unknown package entry %q", id)
		}
	}
	return ids, nil
}

func (p Project) reachablePackages(entries []PackageID) []PackageID {
	reachable := make(map[PackageID]bool, len(entries))
	var visit func(PackageID)
	visit = func(id PackageID) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, target := range p.checked.packages[id].imports {
			visit(target)
		}
	}
	for _, id := range entries {
		visit(id)
	}
	out := make([]PackageID, 0, len(reachable))
	for _, id := range p.checked.order {
		if reachable[id] {
			out = append(out, id)
		}
	}
	return out
}

func (p Project) canonicalBindings(reachable []PackageID, bindings []PackageBinding) (map[PackageID]string, error) {
	canonical := slices.Clone(bindings)
	slices.SortFunc(canonical, func(a, b PackageBinding) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		if a.ImportPath < b.ImportPath {
			return -1
		}
		if a.ImportPath > b.ImportPath {
			return 1
		}
		return 0
	})
	paths := make(map[PackageID]string, len(canonical))
	owners := make(map[string]PackageID, len(canonical))
	for _, binding := range canonical {
		if _, exists := paths[binding.ID]; exists {
			return nil, fmt.Errorf("sprig: duplicate package binding %q", binding.ID)
		}
		if p.checked.packages[binding.ID] == nil {
			return nil, fmt.Errorf("sprig: unknown package binding %q", binding.ID)
		}
		if !validLinkedImportPath(binding.ImportPath) {
			return nil, fmt.Errorf("sprig: invalid Go import path %q for package %q", binding.ImportPath, binding.ID)
		}
		if owner, exists := owners[binding.ImportPath]; exists {
			return nil, fmt.Errorf("sprig: Go import path %q is bound to both %q and %q", binding.ImportPath, owner, binding.ID)
		}
		paths[binding.ID] = binding.ImportPath
		owners[binding.ImportPath] = binding.ID
	}
	for _, id := range reachable {
		if _, exists := paths[id]; !exists {
			return nil, fmt.Errorf("sprig: missing package binding %q", id)
		}
	}
	return paths, nil
}

func validLinkedImportPath(path string) bool {
	if path == "" || len(path) > maximumImportBytes || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	switch path {
	case "C", "context", "embed", "errors", "fmt", "math":
		return false
	}
	for i := 0; i < len(path); i++ {
		character := path[i]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '/' || character == '.' ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	for _, component := range strings.Split(path, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

// Reduce evaluates the checked program canonically. Inputs are observed only,
// outputs are detached, and ctx cancellation is returned directly.
func (p Program) Reduce(ctx context.Context, readings []Reading, stepLimit uint64) (Result, error) {
	if p.checked == nil {
		return Result{}, errors.New("sprig: zero program")
	}
	return evaluate(ctx, p.checked, readings, stepLimit)
}
