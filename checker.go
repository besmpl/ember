package ember

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
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
	Code    string
	Message string
	Start   int
	End     int
	Path    []string
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

type checkResult = checkArtifact

type checkArtifact struct {
	result  CheckResult
	program program
	bind    bindResult
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

// WithGlobalTypes adds host-provided global type facts to typed analysis.
//
// The supplied map is copied. Host globals are analysis facts only; runtime
// globals still enter through RunWithGlobals or Program host adapters.
func WithGlobalTypes(globals map[string]TypeSummary) AnalyzerOption {
	return func(analyzer *Analyzer) {
		if analyzer.typeEnv.globals == nil {
			analyzer.typeEnv = defaultTypeEnv()
		}
		for name, summary := range globals {
			analyzer.typeEnv.setGlobalSummary(name, summary)
		}
	}
}

// WithModuleSummaries adds trusted module summaries for single-source analyzer
// require facts. It does not load or execute modules.
func WithModuleSummaries(summaries map[string]ModuleSummary) AnalyzerOption {
	return func(analyzer *Analyzer) {
		analyzer.moduleSummaries = moduleSummaryEnvFromMap(summaries)
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
	if err := requireCheckMode(sourceArtifact.program); err != nil {
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
	prog := artifact.program
	mode := sourceModePublic(prog.mode)
	diagnostics := analyzeProgramWithMode(artifact.source, prog, artifact.bind, mode, env, summaries)
	facts := buildTypedArtifactFacts(prog, diagnostics)
	return checkArtifact{
		result: CheckResult{
			Mode:        mode,
			Diagnostics: diagnostics,
			Summary:     buildModuleSummary(artifact.source, mode, diagnostics, facts.exports),
			Facts:       facts.tooling,
		},
		program: prog,
		bind:    artifact.bind,
	}, nil
}

func analyzeProgramWithMode(source Source, prog program, bind bindResult, mode SourceMode, env typeEnv, summaries moduleSummaryEnv) []Diagnostic {
	if !policyForMode(mode).analyzesTypes() {
		return nil
	}
	return analyzeProgram(source, prog, bind, mode, env, summaries)
}

func requireCheckMode(prog program) error {
	switch prog.mode {
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

func buildModuleSummary(source Source, mode SourceMode, diagnostics []Diagnostic, exports []ModuleExport) ModuleSummary {
	return ModuleSummary{
		Version:             1,
		SourceName:          source.Name,
		Mode:                mode,
		CompatibilityTarget: "luau-0.728",
		InvalidationHash:    sourceInvalidationHash(source),
		Exports:             exports,
		Diagnostics:         append([]Diagnostic(nil), diagnostics...),
	}
}

func sourceInvalidationHash(source Source) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(source.Text)))
}

func buildTypedArtifactFacts(prog program, diagnostics []Diagnostic) typedArtifactFacts {
	diagCodes := diagnosticCodes(diagnostics)
	store := newTypeStore()
	lowered := lowerTypeAliases(store, prog.statements)
	exports := make([]ModuleExport, 0, len(lowered))
	aliases := make([]ToolingTypeAliasFact, 0, len(lowered))
	for _, item := range lowered {
		summary := store.summary(item.typ)
		applyAliasTypeParameters(&summary, item)
		aliases = append(aliases, ToolingTypeAliasFact{
			Name:      item.name,
			Exported:  item.exported,
			Start:     item.start,
			End:       item.end,
			NameStart: item.nameStart,
			NameEnd:   item.nameEnd,
			Type:      summary,
			DiagCodes: diagCodes,
		})
		if item.exported {
			exports = append(exports, ModuleExport{
				Name:      item.name,
				Kind:      ModuleExportTypeAlias,
				Type:      summary,
				DiagCodes: diagCodes,
			})
		}
	}
	values := moduleLocalValueSummaries(prog, store)
	if value, ok := moduleReturnExport(prog, diagCodes, values); ok {
		exports = append(exports, value)
	}
	return typedArtifactFacts{
		exports: exports,
		tooling: ToolingFacts{
			TypeAliases: aliases,
		},
	}
}

func applyAliasTypeParameters(summary *TypeSummary, item loweredTypeAlias) {
	if len(item.typeParams) != 0 {
		summary.TypeParams = append([]string(nil), item.typeParams...)
	}
	if len(item.typePacks) != 0 {
		summary.TypePacks = append([]string(nil), item.typePacks...)
	}
}

func moduleLocalValueSummaries(prog program, store *typeStore) map[string]TypeSummary {
	values := baseGlobalValueSummaries()
	applyStatementValueSummaries(prog.statements, store, values)
	return values
}

func applyAssignmentValueSummaries(stmt assignStatement, values map[string]TypeSummary) {
	for i, target := range stmt.targets {
		if i >= len(stmt.values) {
			continue
		}
		if len(target.selectors) == 0 {
			if _, ok := values[target.name]; ok {
				values[target.name] = valueTypeSummary(stmt.values[i], values)
			}
			continue
		}
		table, ok := values[target.name]
		if !ok || table.Kind != TypeSummaryTable {
			continue
		}
		if setTableSummaryPath(&table, target.selectors, valueTypeSummary(stmt.values[i], values)) {
			values[target.name] = table
		}
	}
}

func applyIfValueSummaries(stmt ifStatement, store *typeStore, values map[string]TypeSummary) {
	if len(stmt.thenStatements) == 0 {
		return
	}
	thenValues := cloneTypeSummaryMap(values)
	applyStatementValueSummaries(stmt.thenStatements, store, thenValues)
	elseValues := cloneTypeSummaryMap(values)
	if len(stmt.elseStatements) != 0 {
		applyStatementValueSummaries(stmt.elseStatements, store, elseValues)
	}
	mergeAgreedValueSummaries(values, thenValues, elseValues)
}

func applyStatementValueSummaries(statements []statement, store *typeStore, values map[string]TypeSummary) {
	for _, stmt := range statements {
		switch {
		case stmt.local != nil:
			for i, name := range stmt.local.names {
				if i < len(stmt.local.annotations) && stmt.local.annotations[i] != nil {
					values[name] = store.summary(store.lowerType(stmt.local.annotations[i]))
					continue
				}
				if i < len(stmt.local.values) {
					values[name] = valueTypeSummary(stmt.local.values[i], values)
				}
			}
		case stmt.assign != nil:
			applyAssignmentValueSummaries(*stmt.assign, values)
		case stmt.ifStmt != nil:
			applyIfValueSummaries(*stmt.ifStmt, store, values)
		}
	}
}

func cloneTypeSummaryMap(values map[string]TypeSummary) map[string]TypeSummary {
	clone := make(map[string]TypeSummary, len(values))
	for name, summary := range values {
		clone[name] = cloneTypeSummary(summary)
	}
	return clone
}

func mergeAgreedValueSummaries(values, left, right map[string]TypeSummary) {
	for name, leftSummary := range left {
		rightSummary, ok := right[name]
		if !ok {
			continue
		}
		merged, ok := mergeBranchTypeSummaries(leftSummary, rightSummary)
		if ok {
			values[name] = merged
		}
	}
}

func typeSummarySameShape(left, right TypeSummary) bool {
	return reflect.DeepEqual(left, right)
}

func mergeBranchTypeSummaries(left, right TypeSummary) (TypeSummary, bool) {
	if typeSummarySameShape(left, right) {
		return cloneTypeSummary(left), true
	}
	if left.Kind == TypeSummaryTable && right.Kind == TypeSummaryTable {
		return mergeBranchTableSummaries(left, right), true
	}
	if left.Kind == TypeSummaryUnknown || right.Kind == TypeSummaryUnknown {
		return TypeSummary{}, false
	}
	return unionTypeSummary(left, right), true
}

func mergeBranchTableSummaries(left, right TypeSummary) TypeSummary {
	merged := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	seen := make(map[string]struct{}, len(left.Properties)+len(right.Properties))
	for _, leftProperty := range left.Properties {
		seen[leftProperty.Name] = struct{}{}
		propertyType := optionalTypeSummary(leftProperty.Type)
		if rightType, ok := tableSummaryProperty(right, leftProperty.Name); ok {
			mergedType, ok := mergeBranchTypeSummaries(leftProperty.Type, rightType)
			if !ok {
				continue
			}
			propertyType = mergedType
		}
		if propertyType.Kind == TypeSummaryUnknown {
			continue
		}
		merged.Properties = append(merged.Properties, TablePropertySummary{
			Name:   leftProperty.Name,
			Access: leftProperty.Access,
			Type:   propertyType,
		})
	}
	for _, rightProperty := range right.Properties {
		if _, ok := seen[rightProperty.Name]; ok {
			continue
		}
		propertyType := optionalTypeSummary(rightProperty.Type)
		if propertyType.Kind == TypeSummaryUnknown {
			continue
		}
		merged.Properties = append(merged.Properties, TablePropertySummary{
			Name:   rightProperty.Name,
			Access: rightProperty.Access,
			Type:   propertyType,
		})
	}
	if left.Metatable != nil && right.Metatable != nil {
		metatable, ok := mergeBranchTypeSummaries(*left.Metatable, *right.Metatable)
		if ok {
			merged.Metatable = &metatable
		}
	}
	return merged
}

func optionalTypeSummary(summary TypeSummary) TypeSummary {
	if summary.Kind == TypeSummaryUnknown {
		return unknownSummary()
	}
	if summary.Kind == TypeSummaryNilable {
		return cloneTypeSummary(summary)
	}
	inner := cloneTypeSummary(summary)
	return TypeSummary{
		Kind:    TypeSummaryNilable,
		Display: inner.Display + "?",
		Inner:   &inner,
	}
}

func unionTypeSummary(left, right TypeSummary) TypeSummary {
	members := make([]TypeSummary, 0, 2)
	members = appendUnionTypeMembers(members, left)
	members = appendUnionTypeMembers(members, right)
	if len(members) == 1 {
		return members[0]
	}
	return TypeSummary{
		Kind:    TypeSummaryUnion,
		Display: joinTypeDisplays(members, " | "),
		Types:   members,
	}
}

func appendUnionTypeMembers(members []TypeSummary, summary TypeSummary) []TypeSummary {
	if summary.Kind == TypeSummaryUnion {
		for _, member := range summary.Types {
			members = appendUnionTypeMembers(members, member)
		}
		return members
	}
	clone := cloneTypeSummary(summary)
	for _, member := range members {
		if typeSummarySameShape(member, clone) {
			return members
		}
	}
	return append(members, clone)
}

func cloneTypeSummary(summary TypeSummary) TypeSummary {
	clone := summary
	clone.TypeParams = append([]string(nil), summary.TypeParams...)
	clone.TypePacks = append([]string(nil), summary.TypePacks...)
	clone.Types = cloneTypeSummarySlice(summary.Types)
	if summary.Inner != nil {
		inner := cloneTypeSummary(*summary.Inner)
		clone.Inner = &inner
	}
	clone.Properties = make([]TablePropertySummary, len(summary.Properties))
	for i, property := range summary.Properties {
		clone.Properties[i] = TablePropertySummary{
			Name:   property.Name,
			Access: property.Access,
			Type:   cloneTypeSummary(property.Type),
		}
	}
	clone.Indexers = make([]TableIndexerSummary, len(summary.Indexers))
	for i, indexer := range summary.Indexers {
		clone.Indexers[i] = TableIndexerSummary{
			Access: indexer.Access,
			Key:    cloneTypeSummary(indexer.Key),
			Value:  cloneTypeSummary(indexer.Value),
		}
	}
	if summary.Metatable != nil {
		metatable := cloneTypeSummary(*summary.Metatable)
		clone.Metatable = &metatable
	}
	clone.Params = cloneTypeSummarySlice(summary.Params)
	if summary.Return != nil {
		ret := cloneTypeSummary(*summary.Return)
		clone.Return = &ret
	}
	clone.ParamPack = cloneTypePackSummary(summary.ParamPack)
	clone.ReturnPack = cloneTypePackSummary(summary.ReturnPack)
	return clone
}

func cloneTypeSummarySlice(summaries []TypeSummary) []TypeSummary {
	if len(summaries) == 0 {
		return nil
	}
	clone := make([]TypeSummary, len(summaries))
	for i, summary := range summaries {
		clone[i] = cloneTypeSummary(summary)
	}
	return clone
}

func cloneTypePackSummary(summary TypePackSummary) TypePackSummary {
	clone := summary
	clone.Head = cloneTypeSummarySlice(summary.Head)
	if summary.Tail != nil {
		tail := cloneTypeSummary(*summary.Tail)
		clone.Tail = &tail
	}
	return clone
}

func moduleReturnExport(prog program, diagCodes []string, values map[string]TypeSummary) (ModuleExport, bool) {
	for _, stmt := range prog.statements {
		if stmt.ret == nil || len(stmt.ret.values) == 0 {
			continue
		}
		return ModuleExport{
			Name:      "return",
			Kind:      ModuleExportValue,
			Type:      valueTypeSummary(stmt.ret.values[0], values),
			DiagCodes: diagCodes,
		}, true
	}
	return ModuleExport{}, false
}

func valueTypeSummary(expr expression, values map[string]TypeSummary) TypeSummary {
	value, ok := expressionSingleTerm(expr)
	if !ok {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	if value.name != "" {
		if len(value.selectors) != 0 {
			if summary, ok := values[value.name]; ok {
				if field, ok := tableSummaryPath(summary, value.selectors); ok {
					return field
				}
			}
			return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
		}
		if summary, ok := values[value.name]; ok {
			return summary
		}
	}
	if len(value.selectors) != 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	if value.call != nil {
		return callValueTypeSummary(*value.call, values)
	}
	if value.table != nil {
		return tableValueTypeSummary(*value.table, values)
	}
	return simpleTypeSummary(simpleTypeFromTerm(value))
}

func callValueTypeSummary(call callExpression, values map[string]TypeSummary) TypeSummary {
	if len(call.target.selectors) != 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	switch call.target.name {
	case "setmetatable":
		return setMetatableCallTypeSummary(call, values)
	case "getmetatable":
		return getMetatableCallTypeSummary(call, values)
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func setMetatableCallTypeSummary(call callExpression, values map[string]TypeSummary) TypeSummary {
	if len(call.args) < 2 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	table := valueTypeSummary(call.args[0], values)
	if table.Kind != TypeSummaryTable {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	metatable := valueTypeSummary(call.args[1], values)
	if metatable.Kind != TypeSummaryTable {
		return table
	}
	table.Metatable = &metatable
	return table
}

func getMetatableCallTypeSummary(call callExpression, values map[string]TypeSummary) TypeSummary {
	if len(call.args) == 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	table := valueTypeSummary(call.args[0], values)
	if table.Metatable == nil {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	return *table.Metatable
}

func tableValueTypeSummary(table tableExpression, values map[string]TypeSummary) TypeSummary {
	summary := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	for _, field := range table.fields {
		if field.name == "" {
			continue
		}
		summary.Properties = append(summary.Properties, TablePropertySummary{
			Name: field.name,
			Type: valueTypeSummary(field.value, values),
		})
	}
	return summary
}

func simpleTypeSummary(typ simpleType) TypeSummary {
	switch typ {
	case simpleTypeNil:
		return TypeSummary{Kind: TypeSummaryName, Display: "nil"}
	case simpleTypeBoolean:
		return TypeSummary{Kind: TypeSummaryName, Display: "boolean"}
	case simpleTypeNumber:
		return TypeSummary{Kind: TypeSummaryName, Display: "number"}
	case simpleTypeString:
		return TypeSummary{Kind: TypeSummaryName, Display: "string"}
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func diagnosticCodes(diagnostics []Diagnostic) []string {
	if len(diagnostics) == 0 {
		return nil
	}
	codes := make([]string, 0, len(diagnostics))
	seen := make(map[string]bool, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "" || seen[diagnostic.Code] {
			continue
		}
		seen[diagnostic.Code] = true
		codes = append(codes, diagnostic.Code)
	}
	return codes
}

func typeSummaryFromAlias(alias typeAliasStatement) TypeSummary {
	summary := typeSummaryFromExpression(alias.value)
	summary.TypeParams = append([]string(nil), alias.typeParams...)
	summary.TypePacks = append([]string(nil), alias.typePacks...)
	return summary
}

func typeSummaryFromExpression(expr *typeExpression) TypeSummary {
	if expr == nil {
		return TypeSummary{
			Kind:    TypeSummaryUnknown,
			Display: "unknown",
		}
	}
	switch expr.kind {
	case typeKindName:
		display := strings.Join(expr.name, ".")
		if display == "" {
			display = "unknown"
		}
		if len(expr.typeArgs) > 0 {
			display += typeArgumentSummaryDisplay(expr.typeArgs)
		}
		return TypeSummary{Kind: TypeSummaryName, Display: display}
	case typeKindUnion:
		types := typeSummariesFromExpressions(expr.types)
		return TypeSummary{Kind: TypeSummaryUnion, Display: joinTypeDisplays(types, " | "), Types: types}
	case typeKindIntersection:
		types := typeSummariesFromExpressions(expr.types)
		return TypeSummary{Kind: TypeSummaryIntersection, Display: joinTypeDisplays(types, " & "), Types: types}
	case typeKindNilable:
		inner := typeSummaryFromExpression(expr.inner)
		return TypeSummary{Kind: TypeSummaryNilable, Display: inner.Display + "?", Inner: &inner}
	case typeKindTable:
		return tableTypeSummary(expr)
	case typeKindFunction, typeKindGenericFunction:
		return functionTypeSummary(expr)
	case typeKindVariadic:
		inner := typeSummaryFromExpression(expr.inner)
		display := "..."
		if expr.inner != nil {
			display += inner.Display
		}
		return TypeSummary{Kind: TypeSummaryVariadic, Display: display, Inner: &inner}
	case typeKindGenericPack:
		display := strings.Join(expr.name, ".")
		if display == "" {
			display = "..."
		}
		return TypeSummary{Kind: TypeSummaryGenericPack, Display: display + "..."}
	case typeKindSingleton:
		if expr.literal == nil {
			return TypeSummary{Kind: TypeSummarySingleton, Display: "unknown"}
		}
		return TypeSummary{Kind: TypeSummarySingleton, Display: valueSummaryDisplay(*expr.literal)}
	case typeKindTypeof:
		return TypeSummary{Kind: TypeSummaryTypeof, Display: "typeof"}
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func typeSummariesFromExpressions(expressions []*typeExpression) []TypeSummary {
	summaries := make([]TypeSummary, len(expressions))
	for i, expr := range expressions {
		summaries[i] = typeSummaryFromExpression(expr)
	}
	return summaries
}

func tableTypeSummary(expr *typeExpression) TypeSummary {
	summary := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	for _, field := range expr.fields {
		value := typeSummaryFromExpression(field.value)
		if field.name != "" {
			summary.Properties = append(summary.Properties, TablePropertySummary{
				Name:   field.name,
				Access: field.access,
				Type:   value,
			})
			continue
		}
		if field.key != nil {
			summary.Indexers = append(summary.Indexers, TableIndexerSummary{
				Access: field.access,
				Key:    typeSummaryFromExpression(field.key),
				Value:  value,
			})
		}
	}
	return summary
}

func functionTypeSummary(expr *typeExpression) TypeSummary {
	kind := TypeSummaryFunction
	if expr.kind == typeKindGenericFunction {
		kind = TypeSummaryGenericFunction
	}
	params := typeSummariesFromExpressions(typePackValueExpressions(expr.params))
	ret := typeSummaryFromExpression(expr.returnType)
	summary := TypeSummary{
		Kind:       kind,
		Display:    "(" + joinTypeDisplays(params, ", ") + ") -> " + ret.Display,
		TypeParams: append([]string(nil), expr.typeParams...),
		TypePacks:  append([]string(nil), expr.typePacks...),
		Params:     params,
		Return:     &ret,
		ParamPack:  typePackSummary(params, nil),
		ReturnPack: typePackSummary([]TypeSummary{ret}, nil),
	}
	return summary
}

func typePackValueExpressions(values []typeFunctionParam) []*typeExpression {
	expressions := make([]*typeExpression, 0, len(values))
	for _, value := range values {
		expressions = append(expressions, value.value)
	}
	return expressions
}

func typeArgumentSummaryDisplay(args []*typeExpression) string {
	summaries := typeSummariesFromExpressions(args)
	return "<" + joinTypeDisplays(summaries, ", ") + ">"
}

func joinTypeDisplays(types []TypeSummary, separator string) string {
	if len(types) == 0 {
		return ""
	}
	parts := make([]string, len(types))
	for i, typ := range types {
		parts[i] = typ.Display
	}
	return strings.Join(parts, separator)
}

func valueSummaryDisplay(value Value) string {
	if text, ok := value.String(); ok {
		return fmt.Sprintf("%q", text)
	}
	if number, ok := value.Number(); ok {
		return fmt.Sprintf("%g", number)
	}
	if boolean, ok := value.Bool(); ok {
		if boolean {
			return "true"
		}
		return "false"
	}
	if value.IsNil() {
		return "nil"
	}
	return string(value.Kind())
}
