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

func firstLocalStatement(t *testing.T, result checkResult) *localStatement {
	t.Helper()

	if len(result.program.statements) < 1 || result.program.statements[0].local == nil {
		t.Fatalf("first statement is %#v, want local statement", result.program.statements)
	}
	return result.program.statements[0].local
}

func firstTypeAlias(t *testing.T, result checkResult) *typeAliasStatement {
	t.Helper()

	if len(result.program.statements) < 1 || result.program.statements[0].typeAlias == nil {
		t.Fatalf("first statement is %#v, want type alias", result.program.statements)
	}
	return result.program.statements[0].typeAlias
}

func firstLocalFunction(t *testing.T, result checkResult) *localFunctionStatement {
	t.Helper()

	if len(result.program.statements) < 1 || result.program.statements[0].localFunc == nil {
		t.Fatalf("first statement is %#v, want local function", result.program.statements)
	}
	return result.program.statements[0].localFunc
}

func firstReturnStatement(t *testing.T, result checkResult) *returnStatement {
	t.Helper()

	for _, stmt := range result.program.statements {
		if stmt.ret != nil {
			return stmt.ret
		}
	}
	t.Fatalf("statements are %#v, want return statement", result.program.statements)
	return nil
}

func firstReturnedTerm(t *testing.T, result checkResult) *term {
	t.Helper()

	ret := firstReturnStatement(t, result)
	if len(ret.values) < 1 {
		t.Fatal("return statement has no values")
	}
	return &ret.values[0].terms[0].terms[0].left.first.first.first
}

func firstReturnedGroupedTerm(t *testing.T, result checkResult) *term {
	t.Helper()

	group := firstReturnedTerm(t, result).group
	if group == nil {
		t.Fatal("first returned term has no grouped expression")
	}
	return &group.terms[0].terms[0].left.first.first.first
}

func firstReturnedCallTerm(t *testing.T, result checkResult) *term {
	t.Helper()

	call := firstReturnedTerm(t, result)
	if call.call == nil {
		t.Fatal("first returned term call is nil")
	}
	return call
}

func TestParseKeepsLocalTypeAnnotationTree(t *testing.T) {
	_, result := checkTypedSource(t, `
local value: number | string? = 1
return value
`)

	annotations := firstLocalStatement(t, result).annotations
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
	if alias.name != "ScoreMap" {
		t.Fatalf("alias name is %q, want ScoreMap", alias.name)
	}
	if len(alias.typeParams) != 1 || alias.typeParams[0] != "T" {
		t.Fatalf("alias type params are %#v, want T", alias.typeParams)
	}
	if alias.value == nil || alias.value.kind != typeKindTable {
		t.Fatalf("alias value is %#v, want table type", alias.value)
	}
	if len(alias.value.fields) != 1 {
		t.Fatalf("table has %d fields, want 1", len(alias.value.fields))
	}
	field := alias.value.fields[0]
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
	if len(fn.typeParams) != 1 || fn.typeParams[0] != "T" {
		t.Fatalf("function type params are %#v, want T", fn.typeParams)
	}
	if len(fn.paramAnnotations) != 1 {
		t.Fatalf("function has %d param annotations, want 1", len(fn.paramAnnotations))
	}
	if param := fn.paramAnnotations[0]; param == nil || len(param.name) != 1 || param.name[0] != "T" {
		t.Fatalf("param annotation is %#v, want T", param)
	}
	if ret := fn.returnAnnotation; ret == nil || len(ret.name) != 1 || ret.name[0] != "T" {
		t.Fatalf("return annotation is %#v, want T", ret)
	}
}

func TestParseKeepsTypeCastTree(t *testing.T) {
	_, result := checkTypedSource(t, `
local value = 1
return (value :: number) + 2
`)

	castTerm := firstReturnedGroupedTerm(t, result)
	if castTerm.cast == nil || len(castTerm.cast.name) != 1 || castTerm.cast.name[0] != "number" {
		t.Fatalf("cast type is %#v, want number", castTerm.cast)
	}
}

func TestParseKeepsTypePackTrees(t *testing.T) {
	_, result := checkTypedSource(t, `
type Signal<T, U...> = (T, U...) -> (...T)
return 1
`)

	alias := firstTypeAlias(t, result)
	if len(alias.typeParams) != 1 || alias.typeParams[0] != "T" {
		t.Fatalf("alias type params are %#v, want T", alias.typeParams)
	}
	if len(alias.typePacks) != 1 || alias.typePacks[0] != "U" {
		t.Fatalf("alias type packs are %#v, want U", alias.typePacks)
	}
	if alias.value == nil || alias.value.kind != typeKindFunction {
		t.Fatalf("alias value is %#v, want function type", alias.value)
	}
	if len(alias.value.params) != 2 {
		t.Fatalf("function type has %d params, want 2", len(alias.value.params))
	}
	packParam := alias.value.params[1].value
	if packParam == nil || packParam.kind != typeKindGenericPack || len(packParam.name) != 1 || packParam.name[0] != "U" {
		t.Fatalf("second param is %#v, want U generic pack", packParam)
	}
	ret := alias.value.returnType
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

	call := firstReturnedCallTerm(t, result).call
	if len(call.typeArgs) != 2 {
		t.Fatalf("call has %d type args, want 2", len(call.typeArgs))
	}
	if got := call.typeArgs[0].name; len(got) != 1 || got[0] != "number" {
		t.Fatalf("first type arg is %#v, want number", got)
	}
	if got := call.typeArgs[1].name; len(got) != 1 || got[0] != "string" {
		t.Fatalf("second type arg is %#v, want string", got)
	}
}

func TestParseKeepsTypeSourceRanges(t *testing.T) {
	source, result := checkTypedSource(t, `
local value: number | string? = 1
return value
`)

	annotation := firstLocalStatement(t, result).annotations[0]
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
	if alias == nil || alias.value == nil || alias.value.kind != typeKindTable {
		t.Fatalf("alias value is %#v, want table type", alias)
	}
	fields := alias.value.fields
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
