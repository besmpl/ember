package ember

import (
	"context"
	"fmt"
)

// Source names Luau text analyzed by Ember.
type Source struct {
	Name string
	Text string
}

// SourceMode names the Luau typed-analysis mode selected by a source directive.
type SourceMode string

const (
	// SourceModeUnspecified means source did not select a typed-analysis mode.
	SourceModeUnspecified SourceMode = ""
	// SourceModeStrict is Luau's --!strict typed-analysis mode.
	SourceModeStrict SourceMode = "strict"
	// SourceModeNonStrict is Luau's --!nonstrict typed-analysis mode.
	SourceModeNonStrict SourceMode = "nonstrict"
	// SourceModeNoCheck is Luau's --!nocheck typed-analysis mode.
	SourceModeNoCheck SourceMode = "nocheck"
)

// Diagnostic describes one typed-analysis finding.
type Diagnostic struct {
	Code       string
	Message    string
	SourceName string
	Module     ModuleID
	Start      int
	End        int
	Path       []string
}

// ModuleExportKind names a public exported module fact.
type ModuleExportKind string

const (
	// ModuleExportTypeAlias is an exported Luau type alias.
	ModuleExportTypeAlias ModuleExportKind = "type-alias"
	// ModuleExportValue is the public value returned by a module.
	ModuleExportValue ModuleExportKind = "value"
)

// ModuleDependencyKind names a module dependency category.
type ModuleDependencyKind string

const (
	// ModuleDependencyLogical is a dependency resolved through Ember's logical module namespace.
	ModuleDependencyLogical ModuleDependencyKind = "logical"
	// ModuleDependencyHost is a dependency resolved through a host-provided module namespace.
	ModuleDependencyHost ModuleDependencyKind = "host"
)

// TypeSummaryKind names the public shape of a summarized type fact.
type TypeSummaryKind string

const (
	// TypeSummaryUnknown is an unresolved or intentionally hidden type.
	TypeSummaryUnknown TypeSummaryKind = "unknown"
	// TypeSummaryNever is an impossible type produced by normalization.
	TypeSummaryNever TypeSummaryKind = "never"
	// TypeSummaryName is a named type reference.
	TypeSummaryName TypeSummaryKind = "name"
	// TypeSummaryUnion is a union type.
	TypeSummaryUnion TypeSummaryKind = "union"
	// TypeSummaryIntersection is an intersection type.
	TypeSummaryIntersection TypeSummaryKind = "intersection"
	// TypeSummaryNilable is a nilable type.
	TypeSummaryNilable TypeSummaryKind = "nilable"
	// TypeSummaryTable is a table shape.
	TypeSummaryTable TypeSummaryKind = "table"
	// TypeSummaryFunction is a function type.
	TypeSummaryFunction TypeSummaryKind = "function"
	// TypeSummaryGenericFunction is a generic function type.
	TypeSummaryGenericFunction TypeSummaryKind = "generic-function"
	// TypeSummaryVariadic is a variadic type pack.
	TypeSummaryVariadic TypeSummaryKind = "variadic"
	// TypeSummaryGenericPack is a generic type pack.
	TypeSummaryGenericPack TypeSummaryKind = "generic-pack"
	// TypeSummarySingleton is a singleton literal type.
	TypeSummarySingleton TypeSummaryKind = "singleton"
	// TypeSummaryTypeof is a typeof query placeholder.
	TypeSummaryTypeof TypeSummaryKind = "typeof"
)

// ModuleSummary contains exported typed facts for a source module.
type ModuleSummary struct {
	Version             int
	SourceName          string
	Mode                SourceMode
	CompatibilityTarget string
	InvalidationHash    string
	Exports             []ModuleExport
	Dependencies        []ModuleDependencySummary
	Diagnostics         []Diagnostic
}

// ModuleExport describes one exported fact in a module summary.
type ModuleExport struct {
	Name      string
	Kind      ModuleExportKind
	Type      TypeSummary
	DiagCodes []string
}

// ModuleDependencySummary describes a module dependency that affects summary staleness.
type ModuleDependencySummary struct {
	Key              string
	Kind             ModuleDependencyKind
	Path             string
	InvalidationHash string
}

// TypeSummary describes an exported type fact without exposing parser nodes.
type TypeSummary struct {
	Kind       TypeSummaryKind
	Display    string
	TypeParams []string
	TypePacks  []string
	Types      []TypeSummary
	Inner      *TypeSummary
	Properties []TablePropertySummary
	Indexers   []TableIndexerSummary
	Metatable  *TypeSummary
	Params     []TypeSummary
	Return     *TypeSummary
	ParamPack  TypePackSummary
	ReturnPack TypePackSummary
}

// TypePackSummary describes a function argument or return type pack.
type TypePackSummary struct {
	Kind    TypeSummaryKind
	Display string
	Head    []TypeSummary
	Tail    *TypeSummary
}

// TablePropertySummary describes a named exported table property.
type TablePropertySummary struct {
	Name   string
	Access string
	Type   TypeSummary
}

// TableIndexerSummary describes an exported table indexer.
type TableIndexerSummary struct {
	Access string
	Key    TypeSummary
	Value  TypeSummary
}

// ToolingFacts contains optional source facts for editor and tooling callers.
type ToolingFacts struct {
	TypeAliases []ToolingTypeAliasFact
}

// ToolingTypeAliasFact describes a type alias fact for source tooling callers.
type ToolingTypeAliasFact struct {
	Name      string
	Exported  bool
	Start     int
	End       int
	NameStart int
	NameEnd   int
	Type      TypeSummary
	DiagCodes []string
}

// CheckResult is the typed artifact returned by Analyzer.Check.
type CheckResult struct {
	Mode        SourceMode
	Diagnostics []Diagnostic
	Summary     ModuleSummary
	Facts       ToolingFacts
}

// AnalyzerOption configures an Analyzer.
type AnalyzerOption func(*Analyzer)

// Analyzer performs typed analysis for Luau source.
type Analyzer struct {
	typeEnv         typeEnv
	moduleSummaries moduleSummaryEnv
}

// AnalysisConfig contains copy-owned host and dependency facts used by typed
// analysis. NewAnalyzer and LoadProgram both accept this same configuration.
type AnalysisConfig struct {
	Globals         map[string]TypeSummary
	ModuleSummaries map[string]ModuleSummary
}

type checkResult = checkArtifact

type checkArtifact struct {
	result CheckResult
	tree   syntaxTree
	bind   bindResult
}

type typedArtifactFacts struct {
	exports []ModuleExport
	tooling ToolingFacts
}

type modePolicy struct {
	mode SourceMode
}

func policyForMode(mode SourceMode) modePolicy {
	return modePolicy{mode: mode}
}

func (p modePolicy) analyzesTypes() bool {
	return p.mode != SourceModeNoCheck
}

func (p modePolicy) reportsUnknownNames() bool {
	return p.mode == SourceModeStrict
}

func (p modePolicy) reportsUnknownTypes() bool {
	return p.mode == SourceModeStrict
}

// NewAnalyzer returns an Analyzer configured for Ember's default typed analysis.
func NewAnalyzer(options ...AnalyzerOption) *Analyzer {
	analyzer := &Analyzer{typeEnv: defaultTypeEnv()}
	for _, option := range options {
		if option != nil {
			option(analyzer)
		}
	}
	return analyzer
}

// WithAnalysisConfig applies a copied analysis configuration.
func WithAnalysisConfig(config AnalysisConfig) AnalyzerOption {
	copied := cloneAnalysisConfig(config)
	return func(analyzer *Analyzer) {
		analyzer.typeEnv = typeEnvFromAnalysisConfig(copied)
		analyzer.moduleSummaries = moduleSummaryEnvFromMap(copied.ModuleSummaries)
	}
}

// WithGlobalTypes adds host-provided global type facts to typed analysis.
//
// The supplied map is copied. Host globals are analysis facts only; runtime
// globals still enter through RunWithGlobals or Program host adapters.
func WithGlobalTypes(globals map[string]TypeSummary) AnalyzerOption {
	copied := cloneTypeSummaryMap(globals)
	return func(analyzer *Analyzer) {
		if analyzer.typeEnv.globals == nil {
			analyzer.typeEnv = defaultTypeEnv()
		}
		for name, summary := range copied {
			analyzer.typeEnv.setGlobalSummary(name, summary)
		}
	}
}

// WithModuleSummaries adds trusted module summaries for single-source analyzer
// require facts. It does not load or execute modules.
func WithModuleSummaries(summaries map[string]ModuleSummary) AnalyzerOption {
	copied := cloneModuleSummaryMap(summaries)
	return func(analyzer *Analyzer) {
		analyzer.moduleSummaries = moduleSummaryEnvFromMap(copied)
	}
}

// Check parses and analyzes source, returning a typed artifact when analysis can
// continue. Luau type findings are reported as diagnostics inside the result.
func (a *Analyzer) Check(ctx context.Context, source Source) (*CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	artifact, err := checkSourceWithEnv(source, a.typeEnv, a.moduleSummaries)
	if err != nil {
		return nil, err
	}
	return &artifact.result, nil
}

// Check parses source for Ember's future typed-analysis path.
//
// It requires an explicit Luau check-mode directive and returns the first typed
// diagnostic as an error.
// Runtime compilation remains handled by Compile.
func Check(source string) error {
	sourceArtifact, err := parseSource(Source{Text: source})
	if err != nil {
		return err
	}
	if err := requireCheckMode(sourceArtifact.tree); err != nil {
		return err
	}
	artifact, err := buildCheckArtifact(sourceArtifact)
	if err != nil {
		return err
	}
	if len(artifact.result.Diagnostics) > 0 {
		return fmt.Errorf("check: %s", artifact.result.Diagnostics[0].Message)
	}
	return nil
}

func checkSource(source Source) (checkArtifact, error) {
	return checkSourceWithEnv(source, defaultTypeEnv(), moduleSummaryEnv{})
}

func checkSourceWithEnv(source Source, env typeEnv, summaries moduleSummaryEnv) (checkArtifact, error) {
	artifact, err := parseSource(source)
	if err != nil {
		return checkArtifact{}, err
	}
	return buildCheckArtifactWithEnv(artifact, env, summaries)
}

func buildCheckArtifact(artifact sourceArtifact) (checkArtifact, error) {
	return buildCheckArtifactWithEnv(artifact, defaultTypeEnv(), moduleSummaryEnv{})
}

func buildCheckArtifactWithEnv(artifact sourceArtifact, env typeEnv, summaries moduleSummaryEnv) (checkArtifact, error) {
	tree := artifact.tree
	mode := sourceModePublic(tree.mode())
	diagnostics := analyzeSyntaxTreeWithMode(artifact.source, tree, artifact.bind, mode, env, summaries)
	facts := buildTypedArtifactFactsTree(tree, diagnostics)
	return checkArtifact{
		result: CheckResult{
			Mode:        mode,
			Diagnostics: diagnostics,
			Summary:     buildModuleSummary(artifact.source, mode, diagnostics, facts.exports),
			Facts:       facts.tooling,
		},
		tree: tree,
		bind: artifact.bind,
	}, nil
}

func analyzeSyntaxTreeWithMode(source Source, tree syntaxTree, bind bindResult, mode SourceMode, env typeEnv, summaries moduleSummaryEnv) []Diagnostic {
	if !policyForMode(mode).analyzesTypes() {
		return nil
	}
	diagnostics := analyzeSyntaxTree(source, tree, bind, mode, env, summaries)
	for i := range diagnostics {
		diagnostics[i].SourceName = source.Name
	}
	return diagnostics
}

func cloneAnalysisConfig(config AnalysisConfig) AnalysisConfig {
	return AnalysisConfig{
		Globals:         cloneTypeSummaryMap(config.Globals),
		ModuleSummaries: cloneModuleSummaryMap(config.ModuleSummaries),
	}
}

func cloneModuleSummaryMap(summaries map[string]ModuleSummary) map[string]ModuleSummary {
	if len(summaries) == 0 {
		return nil
	}
	cloned := make(map[string]ModuleSummary, len(summaries))
	for name, summary := range summaries {
		cloned[name] = cloneModuleSummary(summary)
	}
	return cloned
}

func typeEnvFromAnalysisConfig(config AnalysisConfig) typeEnv {
	env := defaultTypeEnv()
	for name, summary := range config.Globals {
		env.setGlobalSummary(name, summary)
	}
	return env
}

func requireCheckMode(tree syntaxTree) error {
	switch tree.mode() {
	case sourceModeStrict, sourceModeNonStrict, sourceModeNoCheck:
		return nil
	default:
		return fmt.Errorf("check: --!strict, --!nonstrict, or --!nocheck directive required")
	}
}

func sourceModePublic(mode sourceMode) SourceMode {
	switch mode {
	case sourceModeStrict:
		return SourceModeStrict
	case sourceModeNonStrict:
		return SourceModeNonStrict
	case sourceModeNoCheck:
		return SourceModeNoCheck
	default:
		return SourceModeUnspecified
	}
}
