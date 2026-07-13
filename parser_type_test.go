package ember

import (
	"strings"
	"testing"
)

func checkTypedSource(t *testing.T, body string) (string, checkResult) {
	t.Helper()

	source := "--!strict\n" + strings.TrimLeft(body, "\n")
	result, err := checkSource(Source{Text: source})
	if err != nil {
		t.Fatalf("checkSource returned error: %v", err)
	}
	return source, result
}

func firstStatementID(t *testing.T, result checkResult) statementID {
	t.Helper()

	statements, ok := result.tree.statementIDs()
	if !ok || len(statements) == 0 {
		t.Fatal("syntax tree has no statements")
	}
	return statements[0]
}

func statementTypeExpressions(t *testing.T, tree syntaxTree, span nodeSpan) []*typeExpression {
	t.Helper()

	typeIDs, ok := tree.statementTypes(span)
	if !ok {
		t.Fatalf("type span is %#v, want valid statement type span", span)
	}
	types := make([]*typeExpression, len(typeIDs))
	for i, typeID := range typeIDs {
		types[i], _ = tree.statementType(typeID)
	}
	return types
}

func statementStringValues(t *testing.T, tree syntaxTree, span nodeSpan) []string {
	t.Helper()

	stringIDs, ok := tree.statementStrings(span)
	if !ok {
		t.Fatalf("string span is %#v, want valid statement string span", span)
	}
	values := make([]string, len(stringIDs))
	for i, stringID := range stringIDs {
		values[i], _ = tree.stringValue(stringID)
	}
	return values
}

func firstLocalStatement(t *testing.T, result checkResult) arenaLocalStatement {
	t.Helper()

	statement := firstStatementID(t, result)
	local, ok := result.tree.localArena(statement)
	if !ok {
		t.Fatalf("first statement kind is %d, want local statement", result.tree.statementKindID(statement))
	}
	return local
}

func firstTypeAlias(t *testing.T, result checkResult) arenaTypeAliasStatement {
	t.Helper()

	statement := firstStatementID(t, result)
	alias, ok := result.tree.typeAliasArena(statement)
	if !ok {
		t.Fatalf("first statement kind is %d, want type alias", result.tree.statementKindID(statement))
	}
	return alias
}

func firstLocalFunction(t *testing.T, result checkResult) arenaFunctionStatement {
	t.Helper()

	statement := firstStatementID(t, result)
	function, ok := result.tree.localFunctionArena(statement)
	if !ok {
		t.Fatalf("first statement kind is %d, want local function", result.tree.statementKindID(statement))
	}
	return function
}

func firstReturnStatement(t *testing.T, result checkResult) arenaReturnStatement {
	t.Helper()

	statements, ok := result.tree.statementIDs()
	if !ok {
		t.Fatal("syntax tree has no statement arena")
	}
	for _, statement := range statements {
		if ret, ok := result.tree.returnArena(statement); ok {
			return ret
		}
	}
	t.Fatalf("statement IDs are %#v, want return statement", statements)
	return arenaReturnStatement{}
}

func firstReturnedTerm(t *testing.T, result checkResult) termID {
	t.Helper()

	ret := firstReturnStatement(t, result)
	values, ok := result.tree.statementExpressions(ret.values)
	if !ok || len(values) == 0 {
		t.Fatal("return statement has no values")
	}
	ands, ok := result.tree.expressionTerms(values[0])
	if !ok || len(ands) == 0 {
		t.Fatal("first returned expression has no or terms")
	}
	comparisons, ok := result.tree.andTerms(ands[0])
	if !ok || len(comparisons) == 0 {
		t.Fatal("first returned expression has no and terms")
	}
	concat := result.tree.comparisonLeft(comparisons[0])
	additive := result.tree.concatFirst(concat)
	multiplicative := result.tree.additiveFirst(additive)
	value := result.tree.multiplicativeFirst(multiplicative)
	if value == 0 {
		t.Fatal("first returned expression has no term")
	}
	return value
}

func firstReturnedGroupedTerm(t *testing.T, result checkResult) termID {
	t.Helper()

	group, ok := result.tree.termGroup(firstReturnedTerm(t, result))
	if !ok {
		t.Fatal("first returned term has no grouped expression")
	}
	value, ok := expressionSingleTerm(result.tree, group)
	if !ok {
		t.Fatal("grouped expression is not a single term")
	}
	return value
}

func firstReturnedCall(t *testing.T, result checkResult) arenaCallID {
	t.Helper()

	call, ok := result.tree.termCall(firstReturnedTerm(t, result))
	if !ok {
		t.Fatal("first returned term call is nil")
	}
	return call
}

func TestParseKeepsLocalTypeAnnotationTree(t *testing.T) {
	_, result := checkTypedSource(t, `
local value: number | string? = 1
return value
`)

	local := firstLocalStatement(t, result)
	annotations := statementTypeExpressions(t, result.tree, local.annotations)
	if len(annotations) != 1 {
		t.Fatalf("local has %d annotations, want 1", len(annotations))
	}

	annotation := annotations[0]
	if annotation == nil {
		t.Fatal("local annotation is nil")
	}
	if annotation.kind != typeKindUnion {
		t.Fatalf("annotation kind is %q, want union", annotation.kind)
	}
	if len(annotation.types) != 2 {
		t.Fatalf("union has %d options, want 2", len(annotation.types))
	}
	if got := annotation.types[0].name; len(got) != 1 || got[0] != "number" {
		t.Fatalf("first union option name is %#v, want number", got)
	}
	if annotation.types[1].kind != typeKindNilable {
		t.Fatalf("second union option kind is %q, want nilable", annotation.types[1].kind)
	}
	inner := annotation.types[1].inner
	if inner == nil || len(inner.name) != 1 || inner.name[0] != "string" {
		t.Fatalf("nilable inner is %#v, want string name", inner)
	}
}

func TestParseKeepsTypeAliasTree(t *testing.T) {
	_, result := checkTypedSource(t, `
export type ScoreMap<T> = {[string]: T}
return 1
`)

	alias := firstTypeAlias(t, result)
	if !alias.exported {
		t.Fatal("alias exported is false, want true")
	}
	name, _ := result.tree.stringValue(alias.name)
	if name != "ScoreMap" {
		t.Fatalf("alias name is %q, want ScoreMap", name)
	}
	typeParams := statementStringValues(t, result.tree, alias.typeParams)
	if len(typeParams) != 1 || typeParams[0] != "T" {
		t.Fatalf("alias type params are %#v, want T", typeParams)
	}
	value, _ := result.tree.statementType(alias.value)
	if value == nil || value.kind != typeKindTable {
		t.Fatalf("alias value is %#v, want table type", value)
	}
	if len(value.fields) != 1 {
		t.Fatalf("table has %d fields, want 1", len(value.fields))
	}
	field := value.fields[0]
	if field.key == nil || len(field.key.name) != 1 || field.key.name[0] != "string" {
		t.Fatalf("indexer key is %#v, want string", field.key)
	}
	if field.value == nil || len(field.value.name) != 1 || field.value.name[0] != "T" {
		t.Fatalf("indexer value is %#v, want T", field.value)
	}
}

func TestParseKeepsFunctionTypeAnnotations(t *testing.T) {
	_, result := checkTypedSource(t, `
local function identity<T>(value: T): T
	return value
end
return identity(1)
`)

	fn := firstLocalFunction(t, result)
	typeParams := statementStringValues(t, result.tree, fn.typeParams)
	if len(typeParams) != 1 || typeParams[0] != "T" {
		t.Fatalf("function type params are %#v, want T", typeParams)
	}
	paramAnnotations := statementTypeExpressions(t, result.tree, fn.paramAnnotations)
	if len(paramAnnotations) != 1 {
		t.Fatalf("function has %d param annotations, want 1", len(paramAnnotations))
	}
	if param := paramAnnotations[0]; param == nil || len(param.name) != 1 || param.name[0] != "T" {
		t.Fatalf("param annotation is %#v, want T", param)
	}
	ret, _ := result.tree.statementType(fn.returnAnnotation)
	if ret == nil || len(ret.name) != 1 || ret.name[0] != "T" {
		t.Fatalf("return annotation is %#v, want T", ret)
	}
}

func TestParseKeepsTypeCastTree(t *testing.T) {
	_, result := checkTypedSource(t, `
local value = 1
return (value :: number) + 2
`)

	cast, ok := result.tree.termCast(firstReturnedGroupedTerm(t, result))
	if !ok || cast == nil || len(cast.name) != 1 || cast.name[0] != "number" {
		t.Fatalf("cast type is %#v, want number", cast)
	}
}

func TestParseKeepsTypePackTrees(t *testing.T) {
	_, result := checkTypedSource(t, `
type Signal<T, U...> = (T, U...) -> (...T)
return 1
`)

	alias := firstTypeAlias(t, result)
	typeParams := statementStringValues(t, result.tree, alias.typeParams)
	if len(typeParams) != 1 || typeParams[0] != "T" {
		t.Fatalf("alias type params are %#v, want T", typeParams)
	}
	typePacks := statementStringValues(t, result.tree, alias.typePacks)
	if len(typePacks) != 1 || typePacks[0] != "U" {
		t.Fatalf("alias type packs are %#v, want U", typePacks)
	}
	value, _ := result.tree.statementType(alias.value)
	if value == nil || value.kind != typeKindFunction {
		t.Fatalf("alias value is %#v, want function type", value)
	}
	if len(value.params) != 2 {
		t.Fatalf("function type has %d params, want 2", len(value.params))
	}
	packParam := value.params[1].value
	if packParam == nil || packParam.kind != typeKindGenericPack || len(packParam.name) != 1 || packParam.name[0] != "U" {
		t.Fatalf("second param is %#v, want U generic pack", packParam)
	}
	ret := value.returnType
	if ret == nil || ret.kind != typeKindVariadic || ret.inner == nil || len(ret.inner.name) != 1 || ret.inner.name[0] != "T" {
		t.Fatalf("return type is %#v, want variadic T", ret)
	}
}

func TestParseKeepsCallTypeArguments(t *testing.T) {
	_, result := checkTypedSource(t, `
local function make()
	return 1
end
return make<<number, string>>()
`)

	typeArgs := result.tree.callTypeArgs(firstReturnedCall(t, result))
	if len(typeArgs) != 2 {
		t.Fatalf("call has %d type args, want 2", len(typeArgs))
	}
	if got := typeArgs[0].name; len(got) != 1 || got[0] != "number" {
		t.Fatalf("first type arg is %#v, want number", got)
	}
	if got := typeArgs[1].name; len(got) != 1 || got[0] != "string" {
		t.Fatalf("second type arg is %#v, want string", got)
	}
}

func TestParseKeepsTypeSourceRanges(t *testing.T) {
	source, result := checkTypedSource(t, `
local value: number | string? = 1
return value
`)

	local := firstLocalStatement(t, result)
	annotation := statementTypeExpressions(t, result.tree, local.annotations)[0]
	if got := source[annotation.start:annotation.end]; got != "number | string?" {
		t.Fatalf("annotation source is %q, want number | string?", got)
	}

	nilable := annotation.types[1]
	if got := source[nilable.start:nilable.end]; got != "string?" {
		t.Fatalf("nilable source is %q, want string?", got)
	}
	if got := source[nilable.inner.start:nilable.inner.end]; got != "string" {
		t.Fatalf("nilable inner source is %q, want string", got)
	}
}

func TestParseKeepsTableTypeAccessModifiers(t *testing.T) {
	_, result := checkTypedSource(t, `
export type Model = {
	read Name: string,
	write [number]: boolean,
}
return 1
`)

	alias := firstTypeAlias(t, result)
	value, _ := result.tree.statementType(alias.value)
	if value == nil || value.kind != typeKindTable {
		t.Fatalf("alias value is %#v, want table type", value)
	}
	fields := value.fields
	if len(fields) != 2 {
		t.Fatalf("table has %d fields, want 2", len(fields))
	}

	if fields[0].access != "read" {
		t.Fatalf("first field access is %q, want read", fields[0].access)
	}
	if fields[0].name != "Name" {
		t.Fatalf("first field name is %q, want Name", fields[0].name)
	}
	if fields[0].value == nil || len(fields[0].value.name) != 1 || fields[0].value.name[0] != "string" {
		t.Fatalf("first field value is %#v, want string", fields[0].value)
	}

	if fields[1].access != "write" {
		t.Fatalf("second field access is %q, want write", fields[1].access)
	}
	if fields[1].key == nil || len(fields[1].key.name) != 1 || fields[1].key.name[0] != "number" {
		t.Fatalf("second field key is %#v, want number", fields[1].key)
	}
	if fields[1].value == nil || len(fields[1].value.name) != 1 || fields[1].value.name[0] != "boolean" {
		t.Fatalf("second field value is %#v, want boolean", fields[1].value)
	}
}
