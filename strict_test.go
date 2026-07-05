package ember_test

import (
	"context"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func summaryExport(t *testing.T, exports []ember.ModuleExport, name string, kind ember.ModuleExportKind) ember.ModuleExport {
	t.Helper()
	for _, item := range exports {
		if item.Name == name && item.Kind == kind {
			return item
		}
	}
	t.Fatalf("Summary exports are %#v, want %s %s export", exports, kind, name)
	return ember.ModuleExport{}
}

func countSummaryExports(exports []ember.ModuleExport, kind ember.ModuleExportKind) int {
	count := 0
	for _, item := range exports {
		if item.Kind == kind {
			count++
		}
	}
	return count
}

func summaryProperty(t *testing.T, summary ember.TypeSummary, name string) ember.TablePropertySummary {
	t.Helper()
	for _, property := range summary.Properties {
		if property.Name == name {
			return property
		}
	}
	t.Fatalf("Summary properties are %#v, want property %q", summary.Properties, name)
	return ember.TablePropertySummary{}
}

func TestCompileIgnoresLineComments(t *testing.T) {
	got := compileAndRunNumber(t, `
	-- ordinary file comment
	local value = 2 -- trailing comment
return value + 3
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileIgnoresMultilineComments(t *testing.T) {
	got := compileAndRunNumber(t, `
--[[
ordinary multiline comment
local ignored = "not code"
]]
local value = 2
--[[ inline multiline comment ]] return value + 3
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileRejectsUnterminatedMultilineComment(t *testing.T) {
	_, err := ember.Compile(`--[[ unfinished`)
	if err == nil {
		t.Fatal("Compile succeeded, want unterminated block comment error")
	}
	if !strings.Contains(err.Error(), "unterminated block comment") {
		t.Fatalf("Compile error is %q, want unterminated block comment", err)
	}
}

func TestCheckRequiresStrictDirective(t *testing.T) {
	err := ember.Check("return 1")
	if err == nil {
		t.Fatal("Check succeeded, want mode directive error")
	}
	if !strings.Contains(err.Error(), "--!strict") || !strings.Contains(err.Error(), "--!nonstrict") || !strings.Contains(err.Error(), "--!nocheck") {
		t.Fatalf("Check error is %q, want mode directive error", err)
	}
}

func TestCheckAcceptsNonStrictDirective(t *testing.T) {
	err := ember.Check(`
--!nonstrict
return 1
`)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckAcceptsNoCheckDirectiveWithoutTypeDiagnostics(t *testing.T) {
	err := ember.Check(`
--!nocheck
local value: number = "oops"
return value
`)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckAcceptsStrictDirective(t *testing.T) {
	err := ember.Check(`
--!strict
local value: number = 1
return value
`)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckReturnsTypeDiagnosticAsError(t *testing.T) {
	err := ember.Check(`
--!strict
local value: number = "oops"
return value
`)
	if err == nil {
		t.Fatal("Check succeeded, want type mismatch error")
	}
	if !strings.Contains(err.Error(), "number") || !strings.Contains(err.Error(), "string") {
		t.Fatalf("Check error is %q, want number/string mismatch", err)
	}
}

func TestAnalyzerCheckReturnsStrictTypedArtifact(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number = 1
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Mode != ember.SourceModeStrict {
		t.Fatalf("Check mode is %q, want %q", result.Mode, ember.SourceModeStrict)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReturnsNonStrictTypedArtifact(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "nonstrict.luau",
		Text: `
	--!nonstrict
	local value: number = 1
	return value
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Mode != ember.SourceModeNonStrict {
		t.Fatalf("Check mode is %q, want %q", result.Mode, ember.SourceModeNonStrict)
	}
	if result.Summary.Mode != ember.SourceModeNonStrict {
		t.Fatalf("Summary mode is %q, want %q", result.Summary.Mode, ember.SourceModeNonStrict)
	}
}

func TestAnalyzerCheckReturnsNoCheckTypedArtifactWithoutDiagnostics(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "nocheck.luau",
		Text: `
	--!nocheck
	local value: number = "oops"
	return value
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Mode != ember.SourceModeNoCheck {
		t.Fatalf("Check mode is %q, want %q", result.Mode, ember.SourceModeNoCheck)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none in nocheck: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if len(result.Summary.Diagnostics) != 0 {
		t.Fatalf("Summary has %d diagnostics, want none in nocheck: %#v", len(result.Summary.Diagnostics), result.Summary.Diagnostics)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Display != "number" {
		t.Fatalf("NoCheck return summary is %#v, want annotated number", value.Type)
	}
}

func TestAnalyzerCheckReportsUnknownNameInStrictMode(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = missing
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "unknown-name" {
		t.Fatalf("Diagnostic code is %q, want unknown-name", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "missing") {
		t.Fatalf("Diagnostic message is %q, want missing name", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "missing" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "missing")
	}
}

func TestAnalyzerCheckSuppressesUnknownNameInNonStrictMode(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "nonstrict.luau",
		Text: `
--!nonstrict
local value: number = missing
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none in nonstrict uncertainty: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsAnnotationMismatchInNonStrictMode(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "nonstrict.luau",
		Text: `
--!nonstrict
local value: number = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckAllowsKnownBaseGlobalsInStrictMode(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local kind: string = type(1)
local ok: boolean = coroutine.isyieldable()
local protected = pcall
local protectedWithHandler = xpcall
return kind
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsUnknownTypeNameInStrictMode(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: MissingType = 1
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "unknown-type" {
		t.Fatalf("Diagnostic code is %q, want unknown-type", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "MissingType") {
		t.Fatalf("Diagnostic message is %q, want MissingType", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "MissingType" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "MissingType")
	}
}

func TestAnalyzerCheckReportsUnknownFunctionAnnotationTypeName(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function load(value: MissingType): number
	return 1
end
return load(1)
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "unknown-type" {
		t.Fatalf("Diagnostic code is %q, want unknown-type", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "MissingType") {
		t.Fatalf("Diagnostic message is %q, want MissingType", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "MissingType" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "MissingType")
	}
}

func TestAnalyzerCheckAcceptsBuiltinTypeNames(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
export type RuntimeKinds = any | unknown | never | table | thread | userdata | buffer | vector
export type Callable = () -> number
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckDoesNotParseFunctionKeywordAsTypeName(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	_, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
export type Callable = function
return 1
`,
	})
	if err == nil {
		t.Fatalf("Check returned nil error, want parser error")
	}
}

func TestAnalyzerCheckAllowsAnyAcrossAssignments(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local anything: any = "text"
local amount: number = anything
return amount
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckDoesNotAllowUnknownIntoConcreteAnnotation(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local uncertain: unknown = "text"
local amount: number = uncertain
return amount
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "unknown") {
		t.Fatalf("Diagnostic message is %q, want number/unknown mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsLocalAnnotationMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsLocalAnnotationMismatchValueRange(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = "oops"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsMissingLocalValue(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local first: number, second: string = 1
return second
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "nil") {
		t.Fatalf("Diagnostic message is %q, want string/nil mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "second" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "second")
	}
}

func TestAnalyzerCheckResolvesScalarTypeAlias(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
type Count = number
local value: Count = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckInstantiatesGenericScalarTypeAlias(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
type Maybe<T> = T?
local value: Maybe<number> = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsNilableAnnotationMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number? = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesCastResultType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: string = 1 :: string
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsTableFieldMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string} = {name = 1}
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsTableFieldMismatchValueRange(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {name: string} = {name = 1}
return player
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "1" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "1")
	}
}

func TestAnalyzerCheckReportsMissingTableLiteralField(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {name: string} = {}
return player
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "missing-property" {
		t.Fatalf("Diagnostic code is %q, want missing-property", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "name") {
		t.Fatalf("Diagnostic message is %q, want missing name field", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "{}" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "{}")
	}
}

func TestAnalyzerCheckAllowsComputedKeyTableLiteralFieldFromIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string} = {["name"] = "ember"}
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsComputedKeyTableLiteralFieldFromIndexerMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {name: string} = {["name"] = 10}
return player
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `{["name"] = 10}` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `{["name"] = 10}`)
	}
}

func TestAnalyzerCheckReportsMissingFieldFromInferredEmptyTableAssignment(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local source = {}
local player: {name: string} = source
return player
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "missing-property" {
		t.Fatalf("Diagnostic code is %q, want missing-property", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "name") {
		t.Fatalf("Diagnostic message is %q, want missing name field", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "source" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "source")
	}
}

func TestAnalyzerCheckReportsAnnotatedTableAssignmentFieldMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {name: number} = {name = 1}
local target: {name: string} = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckAllowsAnnotatedTableAssignmentFieldFromIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {[string]: string} = {name = "ember"}
local target: {name: string} = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsAnnotatedTableAssignmentFieldFromIndexerMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {[string]: number} = {name = 10}
local target: {name: string} = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsAnnotatedTableAssignmentFromCallResultIndexerMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function make(): {[string]: number}
	return {name = 10}
end
local target: {name: string} = make()
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckAllowsAnnotatedTableAssignmentFromCallResultIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function make(): {[string]: string}
	return {name = "ember"}
end
local target: {name: string} = make()
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsAnnotatedTableReassignmentFromCallResultIndexerMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function make(): {[string]: number}
	return {name = 10}
end
local target: {name: string} = {name = "ember"}
target = make()
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsAnnotatedTableAssignmentIndexerMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {[string]: number} = {hp = 10}
local target: {[string]: string} = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsMissingIndexerFromInferredEmptyTableAssignment(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local source = {}
local target: {[string]: string} = source
return target
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "missing-property" {
		t.Fatalf("Diagnostic code is %q, want missing-property", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "indexer") {
		t.Fatalf("Diagnostic message is %q, want missing indexer", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "source" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "source")
	}
}

func TestAnalyzerCheckReportsAnnotatedTableAssignmentIndexerFieldMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {hp: number} = {hp = 10}
local target: {[string]: string} = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsTableLiteralFieldMismatchAgainstStringIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local target: {[string]: string} = {hp = 10}
return target
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "10" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "10")
	}
}

func TestAnalyzerCheckReportsAnnotatedTableReassignmentFieldMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {name: number} = {name = 1}
local target: {name: string} = {name = "ember"}
target = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckAllowsAnnotatedTableReassignmentFieldFromIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {[string]: string} = {name = "ember"}
local target: {name: string} = {name = "hearth"}
target = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsAnnotatedTableReassignmentIndexerMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {[string]: number} = {hp = 10}
local target: {[string]: string} = {name = "ember"}
target = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsAnnotatedTableReassignmentIndexerFieldMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local source: {hp: number} = {hp = 10}
local target: {[string]: string} = {name = "ember"}
target = source
return target
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckAllowsMissingNilableTableLiteralField(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {}
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckKeepsNilableTableFieldReadType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {}
local name: string = player.name
return name
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "string?") {
		t.Fatalf("Diagnostic message is %q, want string/string? mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckRefinesNilableTableFieldInTruthyBranch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {}
if player.name then
	local name: string = player.name
	return name
end
return ""
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckRefinesNilableTableFieldStringIndexInTruthyBranch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {}
if player["name"] then
	local name: string = player["name"]
	return name
end
return ""
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsStringIndexTableFieldMismatchInTruthyBranch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {name: string?} = {}
if player["name"] then
	local name: number = player["name"]
	return name
end
return 0
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `player["name"]` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `player["name"]`)
	}
}

func TestAnalyzerCheckRefinesNilableTableFieldInFalseyBranch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {}
if not player.name then
	local name: nil = player.name
	return name
end
return nil
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckRefinesNilableTableFieldWithNilComparison(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {name = "Ada"}
if player.name ~= nil then
	local name: string = player.name
	return name
end
return ""
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckRefinesNilableTableFieldElseBranchWithNilComparison(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {name = "Ada"}
if player.name == nil then
	local missing: nil = player.name
else
	local name: string = player.name
	return name
end
return ""
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckRefinesNilableTableFieldWithReversedNilComparison(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {name = "Ada"}
if nil == player.name then
	local missing: nil = player.name
else
	local name: string = player.name
	return name
end
return ""
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckRefinesUnionLocalWithStringSingletonComparison(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: string | number = "ember"
if value == "ember" then
	local name: string = value
	return name
end
return ""
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckRefinesUnionTableFieldWithBooleanSingletonComparison(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {flag: boolean | string} = {flag = true}
if player.flag == true then
	local flag: boolean = player.flag
	return flag
end
return false
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckRefinesUnionTableFieldElseBranchWithStringSingletonComparison(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string | number} = {name = "ember"}
if player.name ~= "ember" then
	return ""
else
	local name: string = player.name
	return name
end
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckResolvesTableTypeAlias(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
type Player = {name: string}
local player: Player = {name = 1}
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckInstantiatesGenericTableTypeAlias(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
type Box<T> = {value: T}
local box: Box<number> = {value = "oops"}
return box
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesGenericTableTypeAliasFieldFacts(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
type Box<T> = {value: T}
local box: Box<number> = {value = 1}
local value: string = box.value
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesGenericTableIndexerAliasFacts(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
type Dict<T> = {[string]: T}
local scores: Dict<number> = {}
local label: string = scores["hp"]
scores["mp"] = "oops"
return label
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "type-mismatch" {
			t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
		}
		if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
			t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
		}
	}
}

func TestAnalyzerCheckUsesTableFieldFacts(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string} = {name = "Ada"}
local function getName(): number
	return player.name
end
return getName()
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsAssignmentMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number = 1
value = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsAssignmentMismatchValueRange(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = 1
value = "oops"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsMissingAssignmentValue(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local first: number = 1
local second: string = "ok"
first, second = 2
return second
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "nil") {
		t.Fatalf("Diagnostic message is %q, want string/nil mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "second" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "second")
	}
}

func TestAnalyzerCheckReportsTableFieldAssignmentMismatchValueRange(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {name: string} = {name = "ember"}
player.name = 1
return player
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "1" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "1")
	}
}

func TestAnalyzerCheckReportsMissingTableFieldRead(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {name: string} = {name = "ember"}
local score = player.score
return score
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "unknown-property" {
		t.Fatalf("Diagnostic code is %q, want unknown-property", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "score") {
		t.Fatalf("Diagnostic message is %q, want missing score field", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "player.score" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "player.score")
	}
}

func TestAnalyzerCheckReportsMissingTableFieldWrite(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {name: string} = {name = "ember"}
player.score = 1
return player
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "unknown-property" {
		t.Fatalf("Diagnostic code is %q, want unknown-property", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "score") {
		t.Fatalf("Diagnostic message is %q, want missing score field", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "player.score" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "player.score")
	}
}

func TestAnalyzerCheckInfersTableLiteralFieldReadType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player = {name = "ember"}
local bad: number = player.name
return bad
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckInfersTableLiteralFactsForPairs(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player = {name = "ember", score = 10}
for key, value in pairs(player) do
	local badKey: number = key
	local badValue: boolean = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "boolean") || !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want boolean/string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckInfersArrayLiteralIndexerReadType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local values = {1, 2}
local bad: string = values[1]
return bad
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckInfersArrayLiteralFactsForIpairs(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local values = {1, 2}
for index, value in ipairs(values) do
	local badKey: string = index
	local badValue: string = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "type-mismatch" {
			t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
		}
		if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
			t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
		}
	}
}

func TestAnalyzerCheckInfersComputedKeyTableLiteralIndexerReadType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local scores = {["hp"] = 10}
local bad: string = scores["hp"]
return bad
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckInfersComputedKeyTableLiteralFactsForPairs(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local scores = {["hp"] = 10}
for key, value in pairs(scores) do
	local badKey: number = key
	local badValue: string = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckReportsReadOnlyTableFieldWrite(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {read Name: string} = {Name = "ember"}
player.Name = "hearth"
return player.Name
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "readonly-property" {
		t.Fatalf("Diagnostic code is %q, want readonly-property", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "Name") {
		t.Fatalf("Diagnostic message is %q, want read-only Name field", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "player.Name" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "player.Name")
	}
}

func TestAnalyzerCheckReportsWriteOnlyTableFieldRead(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local player: {write Name: string} = {Name = "ember"}
player.Name = "hearth"
return player.Name
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "writeonly-property" {
		t.Fatalf("Diagnostic code is %q, want writeonly-property", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "Name") {
		t.Fatalf("Diagnostic message is %q, want write-only Name field", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "player.Name" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "player.Name")
	}
}

func TestAnalyzerCheckUsesStringTableIndexerReadType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local scores: {[string]: number} = {}
local label: string = scores["hp"]
return label
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesStringTableIndexerDotReadType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {[string]: number} = {}
local label: string = scores.hp
return label
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `scores.hp` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `scores.hp`)
	}
}

func TestAnalyzerCheckReportsStringTableIndexerReadKeyMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {[string]: number} = {}
local value = scores[1]
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `1` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `1`)
	}
}

func TestAnalyzerCheckUsesStringTableIndexerWriteType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {[string]: number} = {}
scores["hp"] = "oops"
return scores
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckUsesStringTableIndexerDotWriteType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {[string]: number} = {}
scores.hp = "oops"
return scores
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsReadOnlyTableIndexerWrite(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {read [string]: number} = {}
scores["hp"] = 10
return scores
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "readonly-property" {
		t.Fatalf("Diagnostic code is %q, want readonly-property", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `scores["hp"]` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `scores["hp"]`)
	}
}

func TestAnalyzerCheckReportsReadOnlyTableIndexerDotWrite(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {read [string]: number} = {}
scores.hp = 10
return scores
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "readonly-property" {
		t.Fatalf("Diagnostic code is %q, want readonly-property", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `scores.hp` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `scores.hp`)
	}
}

func TestAnalyzerCheckReportsWriteOnlyTableIndexerRead(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {write [string]: number} = {}
scores["hp"] = 10
return scores["hp"]
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "writeonly-property" {
		t.Fatalf("Diagnostic code is %q, want writeonly-property", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `scores["hp"]` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `scores["hp"]`)
	}
}

func TestAnalyzerCheckReportsWriteOnlyTableIndexerDotRead(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {write [string]: number} = {}
scores.hp = 10
return scores.hp
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "writeonly-property" {
		t.Fatalf("Diagnostic code is %q, want writeonly-property", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `scores.hp` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `scores.hp`)
	}
}

func TestAnalyzerCheckUsesArrayShorthandReadType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local values: {number} = {1}
local label: string = values[1]
return label
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesArrayShorthandWriteType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local values: {number} = {1}
values[2] = "oops"
return values
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsArrayShorthandLiteralElementMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local values: {number} = {"oops"}
return values
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsComputedKeyTableLiteralValueMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {[string]: number} = {["hp"] = "oops"}
return scores
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsComputedKeyTableLiteralKeyMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {[string]: number} = {[1] = 2}
return scores
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `1` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `1`)
	}
}

func TestAnalyzerCheckReportsStringTableIndexerWriteKeyMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local scores: {[string]: number} = {}
scores[1] = 2
return scores
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `1` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `1`)
	}
}

func TestAnalyzerCheckUsesLexicalAssignmentTarget(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number = 1
while false do
	local value: string = "ok"
end
value = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("Check returned no diagnostics, want assignment mismatch")
	}
	diagnostic := result.Diagnostics[0]
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesLexicalExpressionRead(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number = 1
while false do
	local value: string = "ok"
end
local function read(): number
	return value
end
return read()
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned diagnostics, want none: %#v", result.Diagnostics)
	}
}

func TestAnalyzerCheckDoesNotUseClosedLoopLocalAsNarrowing(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = 1
while false do
	local value: string = "ok"
end
local function read(): string
	return value
end
return read()
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("Check returned no diagnostics, want return mismatch")
	}
	diagnostic := result.Diagnostics[0]
	if !strings.Contains(diagnostic.Message, "number|string") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want union/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsArithmeticOperandMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = 1 + "oops"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsConcatOperandMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: string = "hp:" .. true
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string|number") || !strings.Contains(diagnostic.Message, "boolean") {
		t.Fatalf("Diagnostic message is %q, want string|number/boolean mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `true` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `true`)
	}
}

func TestAnalyzerCheckReportsConcatResultMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = "hp:" .. 10
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"hp:" .. 10` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"hp:" .. 10`)
	}
}

func TestAnalyzerCheckReportsComparisonOperandMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: boolean = 1 < "oops"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsIfConditionComparisonOperandMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
if 1 < "oops" then
	return 1
end
return 0
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsComparisonResultMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: string = 1 < 2
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "boolean") {
		t.Fatalf("Diagnostic message is %q, want string/boolean mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `1 < 2` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `1 < 2`)
	}
}

func TestAnalyzerCheckReportsEqualityResultMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: string = "ember" == "ember"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "boolean") {
		t.Fatalf("Diagnostic message is %q, want string/boolean mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"ember" == "ember"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"ember" == "ember"`)
	}
}

func TestAnalyzerCheckReportsUnaryMinusOperandMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = -"oops"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsUnaryNotResultMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: string = not true
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "boolean") {
		t.Fatalf("Diagnostic message is %q, want string/boolean mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `not true` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `not true`)
	}
}

func TestAnalyzerCheckReportsUnaryLengthResultMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: string = #"ember"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `#"ember"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `#"ember"`)
	}
}

func TestAnalyzerCheckReportsUnaryLengthOperandMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = #true
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string|table") || !strings.Contains(diagnostic.Message, "boolean") {
		t.Fatalf("Diagnostic message is %q, want string|table/boolean mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `true` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `true`)
	}
}

func TestAnalyzerCheckReportsLogicalAndResultMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: string = true and 1
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `true and 1` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `true and 1`)
	}
}

func TestAnalyzerCheckReportsLogicalOrResultMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local value: number = false or "ready"
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `false or "ready"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `false or "ready"`)
	}
}

func TestAnalyzerCheckReportsArithmeticReturnMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function add(): number
	return 1 + "oops"
end
return add()
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesLexicalLocalFunctionFact(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function convert(value: number): number
	return value
end
while false do
	local function convert(value: string): string
		return value
	end
end
return convert("oops")
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("Check returned no diagnostics, want argument mismatch")
	}
	diagnostic := result.Diagnostics[0]
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckKeepsTypeAliasesScoped(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
type Alias = number
while false do
	type Alias = string
end
local value: Alias = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("Check returned no diagnostics, want alias assignment mismatch")
	}
	diagnostic := result.Diagnostics[0]
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckNarrowsNilableLocalInTruthyBranch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number? = 1
if value then
	local narrowed: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsNilableLocalInFalseyBranch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number? = nil
if value then
	local narrowed: number = value
else
	local narrowed: nil = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsNilableLocalThroughNotCondition(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number? = 1
if not value then
	local falsey: nil = value
else
	local truthy: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsNilableLocalWithNilComparison(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number? = 1
if value ~= nil then
	local narrowed: number = value
else
	local missing: nil = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsNilableLocalAfterAssert(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number? = 1
assert(value)
local narrowed: number = value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsNilableTableFieldAfterAssert(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string?} = {name = "Ada"}
assert(player.name)
local name: string = player.name
return name
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsTypeGuardAfterAssert(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = 1
assert(type(value) == "number")
local narrowed: number = value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsTableFieldTypeGuardAfterAssert(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {value: number | string} = {value = 1}
assert(type(player.value) == "number")
local narrowed: number = player.value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsNegatedGroupedTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = "Ada"
if not (type(value) == "number") then
	local narrowed: string = value
else
	local narrowed: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsAndComposedTypeGuardAfterAssert(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = 1
assert(type(value) == "number" and value > 0)
local narrowed: number = value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsLocalWithTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = 1
if type(value) == "number" then
	local narrowed: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsTableFieldWithTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {value: number | string} = {value = 1}
if type(player.value) == "number" then
	local narrowed: number = player.value
	return narrowed
end
return 0
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsTableFieldElseBranchWithTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {value: number | string} = {value = 1}
if type(player.value) == "number" then
	local narrowed: number = player.value
else
	local narrowed: string = player.value
	return narrowed
end
return ""
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsElseBranchWithTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = "Ada"
if type(value) == "number" then
	local narrowed: number = value
else
	local narrowed: string = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsNotEqualTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = "Ada"
if type(value) ~= "number" then
	local narrowed: string = value
else
	local narrowed: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsAndComposedTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = 1
if type(value) == "number" and value > 0 then
	local narrowed: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsWhileBodyWithTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = 1
while type(value) == "string" do
	local narrowed: string = value
	value = 1
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsAfterRepeatUntilTypeGuard(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = "ready"
repeat
	local tick = 1
until type(value) == "string"
local narrowed: string = value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckDoesNotLeakRepeatBodyLocalFromUntilRefinement(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
repeat
	local tick = 1
until type(tick) == "number"
local leaked: number = tick
return leaked
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "unknown-name" {
		t.Fatalf("Diagnostic code is %q, want unknown-name", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "tick") {
		t.Fatalf("Diagnostic message is %q, want missing tick name", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `tick` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `tick`)
	}
}

func TestAnalyzerCheckTypesNumericForLoopVariableAsNumber(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
for index = 1, 3 do
	local value: string = index
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsNumericForBoundMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
for index = 1, "stop" do
	return index
end
return 0
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"stop"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"stop"`)
	}
}

func TestAnalyzerCheckTypesIpairsLoopVariablesFromArrayIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local values: {number} = {1, 2}
for index, value in ipairs(values) do
	local key: string = index
	local item: string = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != "type-mismatch" {
			t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
		}
		if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
			t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
		}
	}
}

func TestAnalyzerCheckTypesIpairsLoopValueFromNumberIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local values: {[number]: string} = {}
for _, value in ipairs(values) do
	local item: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckTypesPairsLoopVariablesFromIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local scores: {[string]: number} = {}
for key, value in pairs(scores) do
	local badKey: number = key
	local badValue: string = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckTypesPairsLoopVariablesFromNumberIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local scores: {[number]: string} = {}
for key, value in pairs(scores) do
	local badKey: string = key
	local badValue: number = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "string") || !strings.Contains(result.Diagnostics[0].Message, "number") {
		t.Fatalf("First diagnostic message is %q, want string/number mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "number") || !strings.Contains(result.Diagnostics[1].Message, "string") {
		t.Fatalf("Second diagnostic message is %q, want number/string mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckTypesPairsLoopVariablesFromRecordFields(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string, score: number} = {name = "ember", score = 10}
for key, value in pairs(player) do
	local badKey: number = key
	local badValue: boolean = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "boolean") || !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want boolean/string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckTypesDirectTableLoopVariablesFromRecordFields(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string, score: number} = {name = "ember", score = 10}
for key, value in player do
	local badKey: number = key
	local badValue: boolean = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "boolean") || !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want boolean/string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckTypesDirectTableLoopVariablesFromIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local scores: {[string]: number} = {}
for key, value in scores do
	local badKey: number = key
	local badValue: string = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckTypesNextTableLoopVariablesFromIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local scores: {[string]: number} = {}
for key, value in next, scores do
	local badKey: number = key
	local badValue: string = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckTypesNextTableLoopVariablesFromRecordFields(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: string, score: number} = {name = "ember", score = 10}
for key, value in next, player do
	local badKey: number = key
	local badValue: boolean = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "boolean") || !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want boolean/string/number mismatch", result.Diagnostics[1].Message)
	}
}

func TestAnalyzerCheckNarrowsOrComposedTypeGuardFalseBranch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string | boolean = true
if type(value) == "number" or type(value) == "string" then
	local matched: number | string = value
else
	local narrowed: boolean = value
end
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsLocalAfterAssignment(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string = 1
value = "ember"
local narrowed: string = value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckNarrowsTableFieldAfterAssignment(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {name: number | string} = {name = 1}
player.name = "ember"
local name: string = player.name
return name
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckJoinsLocalAssignmentFactsAfterIf(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number | string | boolean = true
if coroutine.isyieldable() then
	value = "ready"
else
	value = 10
end
local narrowed: number | string = value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckJoinsTableFieldAssignmentFactsAfterIf(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local player: {score: number | string | boolean} = {score = true}
if coroutine.isyieldable() then
	player.score = "ready"
else
	player.score = 10
end
local narrowed: number | string = player.score
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckJoinsUnknownLocalAssignmentFactsAfterIf(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: unknown = nil
if coroutine.isyieldable() then
	value = "ready"
else
	value = 10
end
local narrowed: number | string = value
return narrowed
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckKeepsIfBranchLocalFactsScoped(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number = 1
if true then
	local value: string = "inner"
end
value = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckKeepsBlockLocalFactsScoped(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local value: number = 1
do
	local value: string = "inner"
end
value = "oops"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsFunctionReturnMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function make(): number
	return "oops"
end
return make()
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsFunctionTableReturnFieldFromIndexerMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function make(): {name: string}
	local source: {[string]: number} = {name = 10}
	return source
end
return make()
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckAllowsFunctionTableReturnFieldFromIndexer(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function make(): {name: string}
	local source: {[string]: string} = {name = "ember"}
	return source
end
return make()
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsFunctionReturnMismatchValueRange(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function make(): number
	return "oops"
end
return make()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsMissingFunctionReturnValue(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function make(): number
	return
end
return make()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "nil") {
		t.Fatalf("Diagnostic message is %q, want number/nil mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `return` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `return`)
	}
}

func TestAnalyzerCheckReportsMissingFunctionReturnStatement(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function make(): number
	local value: number = 1
end
return make()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "nil") {
		t.Fatalf("Diagnostic message is %q, want number/nil mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `number` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `number`)
	}
}

func TestAnalyzerCheckReportsPartialBranchMissingFunctionReturn(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function make(flag: boolean): number
	if flag then
		return 1
	end
end
return make(true)
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "nil") {
		t.Fatalf("Diagnostic message is %q, want number/nil mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `number` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `number`)
	}
}

func TestAnalyzerCheckAllowsAllBranchFunctionReturns(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function make(flag: boolean): number
	if flag then
		return 1
	else
		return 2
	end
end
return make(true)
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckReportsMultipleFunctionReturnMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local pair: () -> (number, string) = function()
	return 1, false
end
return pair()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "boolean") {
		t.Fatalf("Diagnostic message is %q, want string/boolean mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `false` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `false`)
	}
}

func TestAnalyzerCheckReportsMissingMultipleFunctionReturnValue(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local pair: () -> (number, string) = function()
	return 1
end
return pair()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "nil") {
		t.Fatalf("Diagnostic message is %q, want string/nil mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `return` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `return`)
	}
}

func TestAnalyzerCheckReportsFunctionArgumentMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function takesNumber(value: number): number
	return value
end
return takesNumber("oops")
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsFunctionArgumentMismatchValueRange(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function takesNumber(value: number): number
	return value
end
return takesNumber("oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckUsesLocalFunctionTypeAnnotationReturn(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local format: (number) -> string = function(value)
	return tostring(value)
end
local amount: number = format(1)
return amount
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckUsesLocalFunctionTypeAnnotationArguments(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local format: (number) -> string = function(value)
	return tostring(value)
end
return format("oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckUsesTableFunctionFieldAnnotationArguments(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local api: {format: (number) -> string} = {
	format = function(value)
		return tostring(value)
	end,
}
return api.format("oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckUsesTableFunctionFieldAnnotationReturn(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local api: {format: (number) -> string} = {
	format = function(value)
		return tostring(value)
	end,
}
local amount: number = api.format(1)
return amount
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckReportsWriteOnlyTableFunctionFieldCall(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local api: {write format: () -> number} = {
	format = function()
		return 1
	end,
}
return api.format()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "writeonly-property" {
		t.Fatalf("Diagnostic code is %q, want writeonly-property", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `api.format` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `api.format`)
	}
}

func TestAnalyzerCheckReportsWriteOnlyTableFunctionMethodCall(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local api: {write format: (table) -> number} = {
	format = function(self)
		return 1
	end,
}
return api:format()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "writeonly-property" {
		t.Fatalf("Diagnostic code is %q, want writeonly-property", diagnostic.Code)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `api:format` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `api:format`)
	}
}

func TestAnalyzerCheckUsesTableFunctionFieldMethodCallSelfArgument(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local api: {format: (table, number) -> string} = {
	format = function(self, value)
		return tostring(value)
	end,
}
return api:format("oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckAllowsTableFunctionFieldMethodCallSelfArgument(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local api: {format: (table, number) -> string} = {
	format = function(self, value)
		return tostring(value)
	end,
}
local text: string = api:format(1)
return text
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Check returned %d diagnostics, want none: %#v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestAnalyzerCheckChecksFunctionExpressionReturnAgainstLocalFunctionTypeAnnotation(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local make: () -> number = function()
	return "oops"
end
return make()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsFunctionExpressionReturnAnnotationMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local make = function(): number
	return "oops"
end
return make
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckUsesFunctionExpressionReturnAnnotationForCall(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local make = function(): string
	return "ok"
end
local bad: number = make()
return bad
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `make()` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `make()`)
	}
}

func TestAnalyzerCheckUsesLocalFunctionTypeAnnotationForFunctionExpressionParameters(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local convert: (number) -> number = function(value)
	local text: string = value
	return value
end
return convert(1)
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `value` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `value`)
	}
}

func TestAnalyzerCheckUsesFunctionTypeAliasAsCallableFact(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
type Formatter = (number) -> string
local format: Formatter = function(value)
	return tostring(value)
end
local amount: number = format(1)
return amount
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckInstantiatesGenericFunctionTypeAliasAsCallableFact(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
type Chooser = <T>(T, T) -> T
local choose: Chooser = function(left, right)
	return left
end
return choose(1, "oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckInstantiatesGenericFunctionReturnType(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: `
--!strict
local function identity<T>(value: T): T
	return value
end
local value: number = identity("oops")
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckKeepsRepeatedGenericFunctionArgumentsConsistent(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function choose<T>(left: T, right: T): T
	return left
end
return choose(1, "oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckUsesExplicitGenericFunctionTypeArgument(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function identity<T>(value: T): T
	return value
end
local value: number = identity<<number>>("oops")
return value
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsTypedVariadicFunctionArgumentMismatch(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function collect(prefix: string, ...: number): number
	return 1
end
return collect("hp", 10, "oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckInfersGenericTypedVariadicFunctionArguments(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function collect<T>(first: T, ...: T): T
	return first
end
return collect(1, 2, "oops")
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckReportsMissingFunctionArgument(t *testing.T) {
	analyzer := ember.NewAnalyzer()
	source := `
--!strict
local function takesNumber(value: number): number
	return value
end
return takesNumber()
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "nil") {
		t.Fatalf("Diagnostic message is %q, want number/nil mismatch", diagnostic.Message)
	}
	if diagnostic.Start < 0 || diagnostic.End > len(source) || diagnostic.Start >= diagnostic.End {
		t.Fatalf("Diagnostic range is [%d,%d), want valid source range", diagnostic.Start, diagnostic.End)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `takesNumber` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `takesNumber`)
	}
}

func TestAnalyzerCheckBuildsExportedTypeSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/model",
		Text: `
--!strict
type Private = string
export type Model<T, U...> = {
	read Name: string,
	write [number]: boolean,
}
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Summary.Version == 0 {
		t.Fatal("Summary version is 0, want versioned artifact")
	}
	if result.Summary.SourceName != "game/model" {
		t.Fatalf("Summary source name is %q, want game/model", result.Summary.SourceName)
	}
	if result.Summary.Mode != ember.SourceModeStrict {
		t.Fatalf("Summary mode is %q, want strict", result.Summary.Mode)
	}
	if result.Summary.InvalidationHash == "" {
		t.Fatal("Summary invalidation hash is empty")
	}
	if got, want := countSummaryExports(result.Summary.Exports, ember.ModuleExportTypeAlias), 1; got != want {
		t.Fatalf("Summary has %d type-alias exports, want %d: %#v", got, want, result.Summary.Exports)
	}
	exported := summaryExport(t, result.Summary.Exports, "Model", ember.ModuleExportTypeAlias)
	if exported.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Export type kind is %q, want table", exported.Type.Kind)
	}
	if len(exported.Type.TypeParams) != 1 || exported.Type.TypeParams[0] != "T" {
		t.Fatalf("Export type params are %#v, want T", exported.Type.TypeParams)
	}
	if len(exported.Type.TypePacks) != 1 || exported.Type.TypePacks[0] != "U" {
		t.Fatalf("Export type packs are %#v, want U", exported.Type.TypePacks)
	}
	if len(exported.Type.Properties) != 1 {
		t.Fatalf("Export type properties are %#v, want one property", exported.Type.Properties)
	}
	property := exported.Type.Properties[0]
	if property.Name != "Name" || property.Access != "read" || property.Type.Display != "string" {
		t.Fatalf("Export property is %#v, want read Name: string", property)
	}
	if len(exported.Type.Indexers) != 1 {
		t.Fatalf("Export type indexers are %#v, want one indexer", exported.Type.Indexers)
	}
	indexer := exported.Type.Indexers[0]
	if indexer.Access != "write" || indexer.Key.Display != "number" || indexer.Value.Display != "boolean" {
		t.Fatalf("Export indexer is %#v, want write [number]: boolean", indexer)
	}
}

func TestAnalyzerCheckBuildsExportedFunctionPackSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/signal",
		Text: `
	--!strict
	export type Signal<T, U...> = (T, U...) -> (...T)
	return 1
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	exported := summaryExport(t, result.Summary.Exports, "Signal", ember.ModuleExportTypeAlias)
	if exported.Type.Kind != ember.TypeSummaryFunction {
		t.Fatalf("Export type kind is %q, want function", exported.Type.Kind)
	}
	if len(exported.Type.ParamPack.Head) != 2 {
		t.Fatalf("Param pack head is %#v, want T and U...", exported.Type.ParamPack.Head)
	}
	if exported.Type.ParamPack.Head[0].Display != "T" || exported.Type.ParamPack.Head[1].Kind != ember.TypeSummaryGenericPack {
		t.Fatalf("Param pack head is %#v, want T and generic U...", exported.Type.ParamPack.Head)
	}
	if exported.Type.ReturnPack.Tail == nil || exported.Type.ReturnPack.Tail.Kind != ember.TypeSummaryVariadic || exported.Type.ReturnPack.Tail.Display != "...T" {
		t.Fatalf("Return pack tail is %#v, want variadic ...T", exported.Type.ReturnPack.Tail)
	}
}

func TestAnalyzerCheckBuildsGenericFunctionSignatureSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/mapper",
		Text: `
	--!strict
	export type Mapper = <T, U...>(T, U...) -> T
	return 1
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	exported := summaryExport(t, result.Summary.Exports, "Mapper", ember.ModuleExportTypeAlias)
	if exported.Type.Kind != ember.TypeSummaryGenericFunction {
		t.Fatalf("Export type kind is %q, want generic function", exported.Type.Kind)
	}
	if len(exported.Type.TypeParams) != 1 || exported.Type.TypeParams[0] != "T" {
		t.Fatalf("Generic function type params are %#v, want T", exported.Type.TypeParams)
	}
	if len(exported.Type.TypePacks) != 1 || exported.Type.TypePacks[0] != "U" {
		t.Fatalf("Generic function type packs are %#v, want U", exported.Type.TypePacks)
	}
	if len(exported.Type.ParamPack.Head) != 2 || exported.Type.ParamPack.Head[1].Kind != ember.TypeSummaryGenericPack {
		t.Fatalf("Generic function param pack is %#v, want T and U...", exported.Type.ParamPack)
	}
	if exported.Type.Return == nil || exported.Type.Return.Display != "T" {
		t.Fatalf("Generic function return is %#v, want T", exported.Type.Return)
	}
}

func TestAnalyzerCheckUsesHostGlobalFunctionType(t *testing.T) {
	numberType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "number"}
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	analyzer := ember.NewAnalyzer(ember.WithGlobalTypes(map[string]ember.TypeSummary{
		"scoreFor": {
			Kind:    ember.TypeSummaryFunction,
			Display: "function",
			Params:  []ember.TypeSummary{stringType},
			Return:  &numberType,
		},
	}))
	source := `
--!strict
local score: string = scoreFor(42)
return score
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if result.Diagnostics[0].Code != "type-mismatch" {
		t.Fatalf("First diagnostic code is %q, want type-mismatch", result.Diagnostics[0].Code)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "string") || !strings.Contains(result.Diagnostics[0].Message, "number") {
		t.Fatalf("First diagnostic message is %q, want string/number mismatch", result.Diagnostics[0].Message)
	}
	if got := source[result.Diagnostics[0].Start:result.Diagnostics[0].End]; got != "42" {
		t.Fatalf("First diagnostic range points at %q, want %q", got, "42")
	}
	if result.Diagnostics[1].Code != "type-mismatch" {
		t.Fatalf("Second diagnostic code is %q, want type-mismatch", result.Diagnostics[1].Code)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want string/number mismatch", result.Diagnostics[1].Message)
	}
	if got := source[result.Diagnostics[1].Start:result.Diagnostics[1].End]; got != "scoreFor(42)" {
		t.Fatalf("Second diagnostic range points at %q, want %q", got, "scoreFor(42)")
	}
}

func TestAnalyzerCheckUsesHostGlobalTableType(t *testing.T) {
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	analyzer := ember.NewAnalyzer(ember.WithGlobalTypes(map[string]ember.TypeSummary{
		"Player": {
			Kind:    ember.TypeSummaryTable,
			Display: "table",
			Properties: []ember.TablePropertySummary{
				{Name: "name", Type: stringType},
			},
		},
	}))
	source := `
--!strict
local name: number = Player.name
return name
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "Player.name" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "Player.name")
	}
}

func TestAnalyzerCheckUsesHostGlobalTableIndexerType(t *testing.T) {
	numberType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "number"}
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	analyzer := ember.NewAnalyzer(ember.WithGlobalTypes(map[string]ember.TypeSummary{
		"Scores": {
			Kind:    ember.TypeSummaryTable,
			Display: "table",
			Indexers: []ember.TableIndexerSummary{
				{Key: stringType, Value: numberType},
			},
		},
	}))
	source := `
--!strict
local score: string = Scores["hp"]
return score
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `Scores["hp"]` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `Scores["hp"]`)
	}
}

func TestAnalyzerCheckUsesHostGlobalMetatableIndexType(t *testing.T) {
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	indexType := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{
			{Name: "Name", Type: stringType},
		},
	}
	metatableType := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{
			{Name: "__index", Type: indexType},
		},
	}
	playerType := ember.TypeSummary{
		Kind:      ember.TypeSummaryTable,
		Display:   "table",
		Metatable: &metatableType,
	}
	analyzer := ember.NewAnalyzer(ember.WithGlobalTypes(map[string]ember.TypeSummary{
		"Player": playerType,
	}))
	source := `
--!strict
local name: number = Player.Name
return name
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "Player.Name" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "Player.Name")
	}
}

func TestAnalyzerCheckEnforcesHostGlobalTableAccessModifiers(t *testing.T) {
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	analyzer := ember.NewAnalyzer(ember.WithGlobalTypes(map[string]ember.TypeSummary{
		"Player": {
			Kind:    ember.TypeSummaryTable,
			Display: "table",
			Properties: []ember.TablePropertySummary{
				{Name: "Name", Access: "read", Type: stringType},
				{Name: "Secret", Access: "write", Type: stringType},
			},
		},
	}))
	source := `
--!strict
Player.Name = "Ada"
local secret = Player.Secret
return secret
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "strict.luau",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if result.Diagnostics[0].Code != "readonly-property" {
		t.Fatalf("First diagnostic code is %q, want readonly-property", result.Diagnostics[0].Code)
	}
	if got := source[result.Diagnostics[0].Start:result.Diagnostics[0].End]; got != "Player.Name" {
		t.Fatalf("First diagnostic range points at %q, want %q", got, "Player.Name")
	}
	if result.Diagnostics[1].Code != "writeonly-property" {
		t.Fatalf("Second diagnostic code is %q, want writeonly-property", result.Diagnostics[1].Code)
	}
	if got := source[result.Diagnostics[1].Start:result.Diagnostics[1].End]; got != "Player.Secret" {
		t.Fatalf("Second diagnostic range points at %q, want %q", got, "Player.Secret")
	}
}

func TestAnalyzerCheckUsesRequiredModuleReturnSummary(t *testing.T) {
	numberType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "number"}
	analyzer := ember.NewAnalyzer(ember.WithModuleSummaries(map[string]ember.ModuleSummary{
		"logical:game/shared": {
			SourceName: "game/shared",
			Exports: []ember.ModuleExport{
				{
					Name: "return",
					Kind: ember.ModuleExportValue,
					Type: ember.TypeSummary{
						Kind:    ember.TypeSummaryTable,
						Display: "table",
						Properties: []ember.TablePropertySummary{
							{Name: "count", Type: numberType},
						},
					},
				},
			},
		},
	}))
	source := `
--!strict
local shared = require("./shared")
local count: string = shared.count
return count
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "game/init",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "string") || !strings.Contains(diagnostic.Message, "number") {
		t.Fatalf("Diagnostic message is %q, want string/number mismatch", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "shared.count" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "shared.count")
	}
}

func TestAnalyzerCheckUsesRequiredModuleReturnedFunctionSummary(t *testing.T) {
	numberType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "number"}
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	analyzer := ember.NewAnalyzer(ember.WithModuleSummaries(map[string]ember.ModuleSummary{
		"logical:game/score": {
			SourceName: "game/score",
			Exports: []ember.ModuleExport{
				{
					Name: "return",
					Kind: ember.ModuleExportValue,
					Type: ember.TypeSummary{
						Kind:    ember.TypeSummaryFunction,
						Display: "function",
						Params:  []ember.TypeSummary{stringType},
						Return:  &numberType,
					},
				},
			},
		},
	}))
	source := `
--!strict
local makeScore = require("./score")
local score: string = makeScore(42)
return score
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "game/init",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if result.Diagnostics[0].Code != "type-mismatch" {
		t.Fatalf("First diagnostic code is %q, want type-mismatch", result.Diagnostics[0].Code)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "string") || !strings.Contains(result.Diagnostics[0].Message, "number") {
		t.Fatalf("First diagnostic message is %q, want string/number mismatch", result.Diagnostics[0].Message)
	}
	if got := source[result.Diagnostics[0].Start:result.Diagnostics[0].End]; got != "42" {
		t.Fatalf("First diagnostic range points at %q, want %q", got, "42")
	}
	if result.Diagnostics[1].Code != "type-mismatch" {
		t.Fatalf("Second diagnostic code is %q, want type-mismatch", result.Diagnostics[1].Code)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want string/number mismatch", result.Diagnostics[1].Message)
	}
	if got := source[result.Diagnostics[1].Start:result.Diagnostics[1].End]; got != "makeScore(42)" {
		t.Fatalf("Second diagnostic range points at %q, want %q", got, "makeScore(42)")
	}
}

func TestAnalyzerCheckUsesRequiredModuleExportedTypeAlias(t *testing.T) {
	numberType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "number"}
	analyzer := ember.NewAnalyzer(ember.WithModuleSummaries(map[string]ember.ModuleSummary{
		"logical:game/types": {
			SourceName: "game/types",
			Exports: []ember.ModuleExport{
				{
					Name: "Count",
					Kind: ember.ModuleExportTypeAlias,
					Type: numberType,
				},
			},
		},
	}))
	source := `
--!strict
local Types = require("./types")
local count: Types.Count = "oops"
return count
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "game/init",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "string") {
		t.Fatalf("Diagnostic message is %q, want number/string mismatch", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"oops"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"oops"`)
	}
}

func TestAnalyzerCheckUsesRequiredModuleExportedTableTypeAlias(t *testing.T) {
	numberType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "number"}
	analyzer := ember.NewAnalyzer(ember.WithModuleSummaries(map[string]ember.ModuleSummary{
		"logical:game/types": {
			SourceName: "game/types",
			Exports: []ember.ModuleExport{
				{
					Name: "Player",
					Kind: ember.ModuleExportTypeAlias,
					Type: ember.TypeSummary{
						Kind:    ember.TypeSummaryTable,
						Display: "table",
						Properties: []ember.TablePropertySummary{
							{Name: "score", Type: numberType},
						},
					},
				},
			},
		},
	}))
	source := `
--!strict
local Types = require("./types")
local player: Types.Player = {score = "oops"}
local score: string = player.score
return score
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "game/init",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("Check returned %d diagnostics, want 2: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if result.Diagnostics[0].Code != "type-mismatch" {
		t.Fatalf("First diagnostic code is %q, want type-mismatch", result.Diagnostics[0].Code)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "number") || !strings.Contains(result.Diagnostics[0].Message, "string") {
		t.Fatalf("First diagnostic message is %q, want number/string mismatch", result.Diagnostics[0].Message)
	}
	if got := source[result.Diagnostics[0].Start:result.Diagnostics[0].End]; got != `"oops"` {
		t.Fatalf("First diagnostic range points at %q, want %q", got, `"oops"`)
	}
	if result.Diagnostics[1].Code != "type-mismatch" {
		t.Fatalf("Second diagnostic code is %q, want type-mismatch", result.Diagnostics[1].Code)
	}
	if !strings.Contains(result.Diagnostics[1].Message, "string") || !strings.Contains(result.Diagnostics[1].Message, "number") {
		t.Fatalf("Second diagnostic message is %q, want string/number mismatch", result.Diagnostics[1].Message)
	}
	if got := source[result.Diagnostics[1].Start:result.Diagnostics[1].End]; got != "player.score" {
		t.Fatalf("Second diagnostic range points at %q, want %q", got, "player.score")
	}
}

func TestAnalyzerCheckReportsMissingRequiredModuleSummary(t *testing.T) {
	analyzer := ember.NewAnalyzer(ember.WithModuleSummaries(map[string]ember.ModuleSummary{}))
	source := `
--!strict
local shared = require("./missing")
return shared
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "game/init",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "missing-module-summary" {
		t.Fatalf("Diagnostic code is %q, want missing-module-summary", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "missing") {
		t.Fatalf("Diagnostic message is %q, want missing module", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"./missing"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"./missing"`)
	}
}

func TestAnalyzerCheckReportsStaleRequiredModuleSummary(t *testing.T) {
	analyzer := ember.NewAnalyzer(ember.WithModuleSummaries(map[string]ember.ModuleSummary{
		"logical:game/types": {
			SourceName:       "game/types",
			InvalidationHash: "types-v1",
			Dependencies: []ember.ModuleDependencySummary{
				{
					Key:              "logical:game/base",
					Kind:             ember.ModuleDependencyLogical,
					Path:             "game/base",
					InvalidationHash: "base-old",
				},
			},
		},
		"logical:game/base": {
			SourceName:       "game/base",
			InvalidationHash: "base-new",
		},
	}))
	source := `
--!strict
local Types = require("./types")
return Types
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "game/init",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "stale-module-summary" {
		t.Fatalf("Diagnostic code is %q, want stale-module-summary", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "game/types") || !strings.Contains(diagnostic.Message, "logical:game/base") {
		t.Fatalf("Diagnostic message is %q, want stale types/base summary", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != `"./types"` {
		t.Fatalf("Diagnostic range points at %q, want %q", got, `"./types"`)
	}
}

func TestAnalyzerCheckReportsMissingRequiredModuleExportedTypeAlias(t *testing.T) {
	analyzer := ember.NewAnalyzer(ember.WithModuleSummaries(map[string]ember.ModuleSummary{
		"logical:game/types": {
			SourceName: "game/types",
			Exports:    []ember.ModuleExport{},
		},
	}))
	source := `
--!strict
local Types = require("./types")
local count: Types.Missing = 1
return count
`
	result, err := analyzer.Check(context.Background(), ember.Source{
		Name: "game/init",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "unknown-type" {
		t.Fatalf("Diagnostic code is %q, want unknown-type", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "Missing") {
		t.Fatalf("Diagnostic message is %q, want Missing", diagnostic.Message)
	}
	if got := source[diagnostic.Start:diagnostic.End]; got != "Missing" {
		t.Fatalf("Diagnostic range points at %q, want %q", got, "Missing")
	}
}

func TestAnalyzerCheckBuildsModuleReturnValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/settings",
		Text: `
--!strict
export type Mode = string
return {count = 2, name = "ember"}
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	_ = summaryExport(t, result.Summary.Exports, "Mode", ember.ModuleExportTypeAlias)
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want table", value.Type.Kind)
	}
	if len(value.Type.Properties) != 2 {
		t.Fatalf("Return value properties are %#v, want count and name", value.Type.Properties)
	}
	if value.Type.Properties[0].Name != "count" || value.Type.Properties[0].Type.Display != "number" {
		t.Fatalf("First return property is %#v, want count: number", value.Type.Properties[0])
	}
	if value.Type.Properties[1].Name != "name" || value.Type.Properties[1].Type.Display != "string" {
		t.Fatalf("Second return property is %#v, want name: string", value.Type.Properties[1])
	}
}

func TestAnalyzerCheckBuildsSetMetatableReturnValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player",
		Text: `
	--!strict
	return setmetatable({hp = 10}, {__index = {name = "ember"}})
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want table", value.Type.Kind)
	}
	hp := summaryProperty(t, value.Type, "hp")
	if hp.Type.Display != "number" {
		t.Fatalf("hp property is %#v, want number", hp)
	}
	if value.Type.Metatable == nil {
		t.Fatalf("Return table metatable is nil, want metatable summary")
	}
	index := summaryProperty(t, *value.Type.Metatable, "__index")
	if index.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("__index type is %#v, want table", index.Type)
	}
	name := summaryProperty(t, index.Type, "name")
	if name.Type.Display != "string" {
		t.Fatalf("__index.name property is %#v, want string", name)
	}
}

func TestAnalyzerCheckBuildsGetMetatableReturnValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player-meta",
		Text: `
	--!strict
	return getmetatable(setmetatable({hp = 10}, {__index = {name = "ember"}}))
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want metatable table", value.Type.Kind)
	}
	index := summaryProperty(t, value.Type, "__index")
	if index.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("__index type is %#v, want table", index.Type)
	}
	name := summaryProperty(t, index.Type, "name")
	if name.Type.Display != "string" {
		t.Fatalf("__index.name property is %#v, want string", name)
	}
}

func TestAnalyzerCheckBuildsAnnotatedLocalReturnValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/config",
		Text: `
--!strict
local config: {count: number, name: string} = {count = 2, name = "ember"}
return config
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want table", value.Type.Kind)
	}
	if len(value.Type.Properties) != 2 {
		t.Fatalf("Return value properties are %#v, want count and name", value.Type.Properties)
	}
	if value.Type.Properties[0].Name != "count" || value.Type.Properties[0].Type.Display != "number" {
		t.Fatalf("First return property is %#v, want count: number", value.Type.Properties[0])
	}
	if value.Type.Properties[1].Name != "name" || value.Type.Properties[1].Type.Display != "string" {
		t.Fatalf("Second return property is %#v, want name: string", value.Type.Properties[1])
	}
}

func TestAnalyzerCheckBuildsAssignedLocalReturnValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/assigned-local",
		Text: `
--!strict
local value = nil :: any
value = "ember"
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryName || value.Type.Display != "string" {
		t.Fatalf("Return value type is %#v, want string", value.Type)
	}
}

func TestAnalyzerCheckBuildsBranchAssignedLocalReturnValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/branch-assigned-local",
		Text: `
--!strict
local value = nil :: any
if coroutine.isyieldable() then
	value = "ready"
else
	value = 10
end
return value
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryUnion {
		t.Fatalf("Return value type kind is %q, want union: %#v", value.Type.Kind, value.Type)
	}
	if value.Type.Display != "string | number" {
		t.Fatalf("Return value display is %q, want string | number", value.Type.Display)
	}
	if len(value.Type.Types) != 2 || value.Type.Types[0].Display != "string" || value.Type.Types[1].Display != "number" {
		t.Fatalf("Return union members are %#v, want string and number", value.Type.Types)
	}
}

func TestAnalyzerCheckBuildsAssignedLocalTableFieldSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player-assignment",
		Text: `
--!strict
local player = {hp = 10}
player.name = "ember"
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want table", value.Type.Kind)
	}
	hp := summaryProperty(t, value.Type, "hp")
	if hp.Type.Display != "number" {
		t.Fatalf("hp property is %#v, want number", hp)
	}
	name := summaryProperty(t, value.Type, "name")
	if name.Type.Display != "string" {
		t.Fatalf("name property is %#v, want string", name)
	}
}

func TestAnalyzerCheckBuildsNestedAssignedLocalTableFieldSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player-nested-assignment",
		Text: `
--!strict
local player = {stats = {}}
player.stats.level = 7
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want table", value.Type.Kind)
	}
	stats := summaryProperty(t, value.Type, "stats")
	if stats.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("stats property is %#v, want table", stats.Type)
	}
	level := summaryProperty(t, stats.Type, "level")
	if level.Type.Display != "number" {
		t.Fatalf("stats.level property is %#v, want number", level)
	}
}

func TestAnalyzerCheckBuildsNestedLocalTableFieldReturnSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player-nested-return",
		Text: `
--!strict
local player = {stats = {}}
player.stats.level = 7
return player.stats.level
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Display != "number" {
		t.Fatalf("Return value type is %#v, want number", value.Type)
	}
}

func TestAnalyzerCheckBuildsBranchAssignedLocalTableFieldSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player-branch-assignment",
		Text: `
--!strict
local player = {}
if true then
	player.name = "ember"
else
	player.name = "hearth"
end
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want table", value.Type.Kind)
	}
	name := summaryProperty(t, value.Type, "name")
	if name.Type.Display != "string" {
		t.Fatalf("name property is %#v, want string", name)
	}
}

func TestAnalyzerCheckBuildsBranchAssignedUnionTableFieldSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player-branch-union-assignment",
		Text: `
--!strict
local player = {}
if true then
	player.score = "ready"
else
	player.score = 10
end
return player
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want table", value.Type.Kind)
	}
	score := summaryProperty(t, value.Type, "score")
	if score.Type.Kind != ember.TypeSummaryUnion {
		t.Fatalf("score type kind is %q, want union: %#v", score.Type.Kind, score.Type)
	}
	if score.Type.Display != "string | number" {
		t.Fatalf("score type display is %q, want string | number", score.Type.Display)
	}
	if len(score.Type.Types) != 2 || score.Type.Types[0].Display != "string" || score.Type.Types[1].Display != "number" {
		t.Fatalf("score union members are %#v, want string and number", score.Type.Types)
	}
}

func TestAnalyzerCheckBuildsOptionalBranchAssignedTableFieldSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/player-optional-branch-assignment",
		Text: `
--!strict
local player = {}
if coroutine.isyieldable() then
	player.nickname = "ember"
end
return player.nickname
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryNilable {
		t.Fatalf("Return value type kind is %q, want nilable: %#v", value.Type.Kind, value.Type)
	}
	if value.Type.Display != "string?" {
		t.Fatalf("Return value display is %q, want string?", value.Type.Display)
	}
	if value.Type.Inner == nil || value.Type.Inner.Display != "string" {
		t.Fatalf("Return value inner is %#v, want string", value.Type.Inner)
	}
}

func TestAnalyzerCheckBuildsAnnotatedLocalFieldReturnValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/count",
		Text: `
	--!strict
local config: {count: number, name: string} = {count = 2, name = "ember"}
return config.count
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Display != "number" {
		t.Fatalf("Return value type is %#v, want number", value.Type)
	}
}

func TestAnalyzerCheckBuildsCoroutineLibraryValueSummary(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/coroutines",
		Text: `
	--!strict
	return coroutine
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	value := summaryExport(t, result.Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Return value type kind is %q, want coroutine table", value.Type.Kind)
	}
	for _, name := range []string{"create", "resume", "yield", "status", "close", "running", "isyieldable", "wrap"} {
		property := summaryProperty(t, value.Type, name)
		if property.Type.Kind != ember.TypeSummaryFunction {
			t.Fatalf("coroutine.%s type is %#v, want function summary", name, property.Type)
		}
	}
	status := summaryProperty(t, value.Type, "status")
	if status.Type.Return == nil || status.Type.Return.Display != "string" {
		t.Fatalf("coroutine.status return is %#v, want string", status.Type.Return)
	}
	isyieldable := summaryProperty(t, value.Type, "isyieldable")
	if isyieldable.Type.Return == nil || isyieldable.Type.Return.Display != "boolean" {
		t.Fatalf("coroutine.isyieldable return is %#v, want boolean", isyieldable.Type.Return)
	}
	resume := summaryProperty(t, value.Type, "resume")
	if len(resume.Type.ParamPack.Head) != 1 || resume.Type.ParamPack.Head[0].Display != "thread" {
		t.Fatalf("coroutine.resume param pack is %#v, want thread head", resume.Type.ParamPack)
	}
	if resume.Type.ParamPack.Tail == nil || resume.Type.ParamPack.Tail.Display != "unknown" {
		t.Fatalf("coroutine.resume param pack tail is %#v, want unknown variadic resume args", resume.Type.ParamPack.Tail)
	}
	if len(resume.Type.ReturnPack.Head) != 1 || resume.Type.ReturnPack.Head[0].Display != "boolean" {
		t.Fatalf("coroutine.resume return pack is %#v, want boolean head", resume.Type.ReturnPack)
	}
	if resume.Type.ReturnPack.Tail == nil || resume.Type.ReturnPack.Tail.Display != "unknown" {
		t.Fatalf("coroutine.resume return pack tail is %#v, want unknown yielded/final values", resume.Type.ReturnPack.Tail)
	}
	yield := summaryProperty(t, value.Type, "yield")
	if yield.Type.ParamPack.Tail == nil || yield.Type.ParamPack.Tail.Display != "unknown" {
		t.Fatalf("coroutine.yield param pack tail is %#v, want unknown yielded values", yield.Type.ParamPack.Tail)
	}
	if yield.Type.ReturnPack.Tail == nil || yield.Type.ReturnPack.Tail.Display != "unknown" {
		t.Fatalf("coroutine.yield return pack tail is %#v, want unknown resumed values", yield.Type.ReturnPack.Tail)
	}
}

func TestAnalyzerCheckUsesCoroutineLibraryCallReturnTypes(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/coroutine-call",
		Text: `
	--!strict
	local value: number = coroutine.isyieldable()
	return value
	`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "type-mismatch" {
		t.Fatalf("Diagnostic code is %q, want type-mismatch", diagnostic.Code)
	}
	if !strings.Contains(diagnostic.Message, "number") || !strings.Contains(diagnostic.Message, "boolean") {
		t.Fatalf("Diagnostic message is %q, want number/boolean mismatch", diagnostic.Message)
	}
}

func TestAnalyzerCheckBuildsToolingTypeAliasFacts(t *testing.T) {
	source := `
	--!strict
	type LocalName = string | string
export type Model = {Name: LocalName}
return 1
`
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/tooling",
		Text: source,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if got, want := countSummaryExports(result.Summary.Exports, ember.ModuleExportTypeAlias), 1; got != want {
		t.Fatalf("Summary has %d type-alias exports, want %d: %#v", got, want, result.Summary.Exports)
	}
	if got, want := len(result.Facts.TypeAliases), 2; got != want {
		t.Fatalf("Tooling type aliases has %d facts, want %d: %#v", got, want, result.Facts.TypeAliases)
	}
	local := result.Facts.TypeAliases[0]
	if local.Name != "LocalName" || local.Exported {
		t.Fatalf("First tooling alias is %#v, want private LocalName", local)
	}
	wantLocalStart := strings.Index(source, "type LocalName")
	wantLocalNameStart := strings.Index(source, "LocalName")
	wantLocalEnd := strings.Index(source, "string | string") + len("string | string")
	if local.Start != wantLocalStart || local.End != wantLocalEnd || local.NameStart != wantLocalNameStart || local.NameEnd != wantLocalNameStart+len("LocalName") {
		t.Fatalf("LocalName ranges are start=%d end=%d name=%d:%d, want start=%d end=%d name=%d:%d", local.Start, local.End, local.NameStart, local.NameEnd, wantLocalStart, wantLocalEnd, wantLocalNameStart, wantLocalNameStart+len("LocalName"))
	}
	if local.Type.Kind != ember.TypeSummaryName || local.Type.Display != "string" || len(local.Type.Types) != 0 {
		t.Fatalf("LocalName type summary is %#v, want normalized string name", local.Type)
	}
	model := result.Facts.TypeAliases[1]
	if model.Name != "Model" || !model.Exported {
		t.Fatalf("Second tooling alias is %#v, want exported Model", model)
	}
	wantModelStart := strings.Index(source, "export type Model")
	wantModelNameStart := strings.Index(source, "Model")
	wantModelEnd := strings.Index(source, "{Name: LocalName}") + len("{Name: LocalName}")
	if model.Start != wantModelStart || model.End != wantModelEnd || model.NameStart != wantModelNameStart || model.NameEnd != wantModelNameStart+len("Model") {
		t.Fatalf("Model ranges are start=%d end=%d name=%d:%d, want start=%d end=%d name=%d:%d", model.Start, model.End, model.NameStart, model.NameEnd, wantModelStart, wantModelEnd, wantModelNameStart, wantModelNameStart+len("Model"))
	}
	if model.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("Model tooling type kind is %q, want table", model.Type.Kind)
	}
	if len(model.Type.Properties) != 1 {
		t.Fatalf("Model tooling properties are %#v, want Name property", model.Type.Properties)
	}
	if model.Type.Properties[0].Name != "Name" || model.Type.Properties[0].Type.Display != "string" {
		t.Fatalf("Model tooling property is %#v, want Name: string", model.Type.Properties[0])
	}
}

func TestAnalyzerCheckSummaryCarriesDiagnostics(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/bad-model",
		Text: `
--!strict
export type Model = {Name: string}
local model: Model = {Name = 123}
return model
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("Check returned %d diagnostics, want 1: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	if len(result.Summary.Diagnostics) != 1 {
		t.Fatalf("Summary has %d diagnostics, want 1: %#v", len(result.Summary.Diagnostics), result.Summary.Diagnostics)
	}
	if result.Summary.Diagnostics[0].Code != result.Diagnostics[0].Code {
		t.Fatalf("Summary diagnostic code is %q, want %q", result.Summary.Diagnostics[0].Code, result.Diagnostics[0].Code)
	}
	exported := summaryExport(t, result.Summary.Exports, "Model", ember.ModuleExportTypeAlias)
	if len(exported.DiagCodes) != 1 || exported.DiagCodes[0] != "type-mismatch" {
		t.Fatalf("Export diagnostic codes are %#v, want type-mismatch", exported.DiagCodes)
	}
}

func TestAnalyzerCheckSummaryNormalizesExportedUnions(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/types",
		Text: `
--!strict
export type MaybeName = string | nil | string
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	exported := summaryExport(t, result.Summary.Exports, "MaybeName", ember.ModuleExportTypeAlias)
	if exported.Type.Kind != ember.TypeSummaryUnion {
		t.Fatalf("Export type kind is %q, want union", exported.Type.Kind)
	}
	if exported.Type.Display != "string | nil" {
		t.Fatalf("Export display is %q, want normalized string | nil", exported.Type.Display)
	}
	if len(exported.Type.Types) != 2 {
		t.Fatalf("Export union has %d types, want 2: %#v", len(exported.Type.Types), exported.Type.Types)
	}
}

func TestAnalyzerCheckSummaryNormalizesImpossiblePrimitiveIntersections(t *testing.T) {
	result, err := ember.NewAnalyzer().Check(context.Background(), ember.Source{
		Name: "game/impossible",
		Text: `
--!strict
export type Impossible = string & number
return 1
`,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	exported := summaryExport(t, result.Summary.Exports, "Impossible", ember.ModuleExportTypeAlias)
	if exported.Type.Kind != ember.TypeSummaryNever {
		t.Fatalf("Export type kind is %q, want never", exported.Type.Kind)
	}
	if exported.Type.Display != "never" {
		t.Fatalf("Export display is %q, want never", exported.Type.Display)
	}
	if len(exported.Type.Types) != 0 {
		t.Fatalf("Export never summary has %d child types, want 0: %#v", len(exported.Type.Types), exported.Type.Types)
	}
}

func TestCheckAcceptsTableTypeAccessModifiers(t *testing.T) {
	err := ember.Check(`
--!strict
export type Model = {
	read Name: string,
	write [number]: boolean,
}
return 1
`)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckAcceptsStrictTypedSyntax(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "local annotation",
			source: `
--!strict
local value: number | string? = 1
return value
`,
		},
		{
			name: "type alias",
			source: `
--!strict
type Pair<T> = {left: T, right: T}
return 1
`,
		},
		{
			name: "function annotations",
			source: `
--!strict
local function identity<T>(value: T): T
	return value
end
return identity(1)
`,
		},
		{
			name: "cast",
			source: `
--!strict
local value = 1
return (value :: number) + 2
`,
		},
		{
			name: "call type arguments",
			source: `
--!strict
local function make()
	return 1
end
return make<<number, string>>()
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ember.Check(tt.source); err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
		})
	}
}
