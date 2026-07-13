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

func statementTypeExpressions(t *testing.T, tree syntaxTree, span nodeSpan) []typeID {
	t.Helper()

	typeIDs, ok := tree.statementTypes(span)
	if !ok {
		t.Fatalf("type span is %#v, want valid statement type span", span)
	}
	types := make([]typeID, len(typeIDs))
	for i, typeID := range typeIDs {
		types[i], _ = tree.statementType(typeID)
	}
	return types
}

func requireTypeName(t *testing.T, tree syntaxTree, value typeID, want string) {
	t.Helper()
	got := tree.typeName(value)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("type name is %#v, want %q", got, want)
	}
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
	if annotation == 0 {
		t.Fatal("local annotation is nil")
	}
	if got := result.tree.typeKind(annotation); got != typeKindUnion {
		t.Fatalf("annotation kind is %d, want union", got)
	}
	children := result.tree.typeChildren(annotation)
	if len(children) != 2 {
		t.Fatalf("union has %d options, want 2", len(children))
	}
	requireTypeName(t, result.tree, children[0], "number")
	if got := result.tree.typeKind(children[1]); got != typeKindNilable {
		t.Fatalf("second union option kind is %d, want nilable", got)
	}
	inner, ok := result.tree.typeInner(children[1])
	if !ok {
		t.Fatal("nilable inner is missing")
	}
	requireTypeName(t, result.tree, inner, "string")
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
	if value == 0 || result.tree.typeKind(value) != typeKindTable {
		t.Fatalf("alias value is %#v, want table type", value)
	}
	fields := result.tree.typeFields(value)
	if len(fields) != 1 {
		t.Fatalf("table has %d fields, want 1", len(fields))
	}
	field := fields[0]
	requireTypeName(t, result.tree, result.tree.typeFieldKey(field), "string")
	requireTypeName(t, result.tree, result.tree.typeFieldValue(field), "T")
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
	requireTypeName(t, result.tree, paramAnnotations[0], "T")
	ret, _ := result.tree.statementType(fn.returnAnnotation)
	requireTypeName(t, result.tree, ret, "T")
}

func TestParseKeepsTypeCastTree(t *testing.T) {
	_, result := checkTypedSource(t, `
local value = 1
return (value :: number) + 2
`)

	cast, ok := result.tree.termCast(firstReturnedGroupedTerm(t, result))
	if !ok || cast == 0 {
		t.Fatalf("cast type is %#v, want number", cast)
	}
	requireTypeName(t, result.tree, cast, "number")
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
	if value == 0 || result.tree.typeKind(value) != typeKindFunction {
		t.Fatalf("alias value is %#v, want function type", value)
	}
	params := result.tree.typeParams(value)
	if len(params) != 2 {
		t.Fatalf("function type has %d params, want 2", len(params))
	}
	packParam := result.tree.typeParamValue(params[1])
	if packParam == 0 || result.tree.typeKind(packParam) != typeKindGenericPack {
		t.Fatalf("second param is %#v, want U generic pack", packParam)
	}
	packInner, ok := result.tree.typeInner(packParam)
	if !ok {
		t.Fatal("generic pack has no inner type")
	}
	requireTypeName(t, result.tree, packInner, "U")
	ret, ok := result.tree.typeReturn(value)
	if !ok || result.tree.typeKind(ret) != typeKindVariadic {
		t.Fatalf("return type is %#v, want variadic T", ret)
	}
	inner, ok := result.tree.typeInner(ret)
	if !ok {
		t.Fatal("variadic return has no inner type")
	}
	requireTypeName(t, result.tree, inner, "T")
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
	requireTypeName(t, result.tree, typeArgs[0], "number")
	requireTypeName(t, result.tree, typeArgs[1], "string")
}

func TestCallTypeArgumentsRejectMalformedTypeIDs(t *testing.T) {
	if got := (syntaxTree{}).callTypeArgs(1); got != nil {
		t.Fatalf("nil arena resolved call type arguments as %#v", got)
	}
	_, result := checkTypedSource(t, `return make<<number>>()`)
	call := firstReturnedCall(t, result)
	node, ok := result.tree.arena.call(call)
	if !ok || node.typeArgs.count != 1 {
		t.Fatal("call type arguments are unavailable")
	}
	result.tree.arena.types.typeIDs[node.typeArgs.start] = typeID(len(result.tree.arena.types.nodes) + 1)
	if got := result.tree.callTypeArgs(call); got != nil {
		t.Fatalf("malformed call type arguments resolved as %#v", got)
	}
}

func TestParseKeepsTypeSourceRanges(t *testing.T) {
	source, result := checkTypedSource(t, `
local value: number | string? = 1
return value
`)

	local := firstLocalStatement(t, result)
	annotation := statementTypeExpressions(t, result.tree, local.annotations)[0]
	start, end := result.tree.typeRange(annotation)
	if got := source[start:end]; got != "number | string?" {
		t.Fatalf("annotation source is %q, want number | string?", got)
	}

	nilable := result.tree.typeChildren(annotation)[1]
	start, end = result.tree.typeRange(nilable)
	if got := source[start:end]; got != "string?" {
		t.Fatalf("nilable source is %q, want string?", got)
	}
	inner, _ := result.tree.typeInner(nilable)
	start, end = result.tree.typeRange(inner)
	if got := source[start:end]; got != "string" {
		t.Fatalf("nilable inner source is %q, want string", got)
	}
}

func TestParseTypeNameRangeExcludesTrailingWhitespace(t *testing.T) {
	source, result := checkTypedSource(t, `local value: Module.Name   = nil`)
	local := firstLocalStatement(t, result)
	annotation := statementTypeExpressions(t, result.tree, local.annotations)[0]
	start, end := result.tree.typeRange(annotation)
	if got := source[start:end]; got != "Module.Name" {
		t.Fatalf("annotation source is %q, want Module.Name", got)
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
	if value == 0 || result.tree.typeKind(value) != typeKindTable {
		t.Fatalf("alias value is %#v, want table type", value)
	}
	fields := result.tree.typeFields(value)
	if len(fields) != 2 {
		t.Fatalf("table has %d fields, want 2", len(fields))
	}

	if got := result.tree.typeFieldAccess(fields[0]); got != "read" {
		t.Fatalf("first field access is %q, want read", got)
	}
	if got := result.tree.typeFieldName(fields[0]); got != "Name" {
		t.Fatalf("first field name is %q, want Name", got)
	}
	requireTypeName(t, result.tree, result.tree.typeFieldValue(fields[0]), "string")

	if got := result.tree.typeFieldAccess(fields[1]); got != "write" {
		t.Fatalf("second field access is %q, want write", got)
	}
	requireTypeName(t, result.tree, result.tree.typeFieldKey(fields[1]), "number")
	requireTypeName(t, result.tree, result.tree.typeFieldValue(fields[1]), "boolean")
}

func TestParseTypeArenaCoversEveryTypeFamily(t *testing.T) {
	source := `
type Named = Package.Member<number>
type Combined = (A & B) | C?
type Table = {read Name: string, [number]: boolean}
type Function = (value: A, ...: B) -> C
type GenericFunction = <T, U...>(T, U...) -> typeof(value)
type Variadic = ...A
type Pack = A...
type Singleton = true | false | "text" | 1
return 1
`
	_, result := checkTypedSource(t, source)
	seen := make(map[typeKind]bool)
	var visit func(typeID)
	visit = func(value typeID) {
		if value == 0 {
			return
		}
		seen[result.tree.typeKind(value)] = true
		for _, child := range result.tree.typeArgs(value) {
			visit(child)
		}
		for _, child := range result.tree.typeChildren(value) {
			visit(child)
		}
		if inner, ok := result.tree.typeInner(value); ok {
			visit(inner)
		}
		for _, field := range result.tree.typeFields(value) {
			visit(result.tree.typeFieldKey(field))
			visit(result.tree.typeFieldValue(field))
		}
		for _, param := range result.tree.typeParams(value) {
			visit(result.tree.typeParamValue(param))
		}
		if ret, ok := result.tree.typeReturn(value); ok {
			visit(ret)
		}
	}
	statements, _ := result.tree.statementIDs()
	for _, statement := range statements {
		if alias, ok := result.tree.typeAliasArena(statement); ok {
			visit(alias.value)
		}
	}
	for _, kind := range []typeKind{
		typeKindName, typeKindUnion, typeKindIntersection, typeKindNilable,
		typeKindTable, typeKindFunction, typeKindGenericFunction, typeKindTypeof,
		typeKindVariadic, typeKindGenericPack, typeKindSingleton,
	} {
		if !seen[kind] {
			t.Errorf("type kind %d was not represented in the arena", kind)
		}
	}
}

func TestParseSingletonStringDoesNotBoxRuntimeValue(t *testing.T) {
	tree, err := (&parser{source: `type Empty = ""`}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	statements, _ := tree.statementIDs()
	alias, ok := tree.typeAliasArena(statements[0])
	if !ok {
		t.Fatal("first statement is not a type alias")
	}
	id, ok := tree.typeSingletonStringID(alias.value)
	if !ok || id == 0 {
		t.Fatalf("singleton string ID is %d, %v; want non-zero valid ID", id, ok)
	}
	text, _ := tree.stringValue(id)
	if text != "" {
		t.Fatalf("singleton string is %q, want empty", text)
	}
	if len(tree.arena.stringLiterals) != 0 {
		t.Fatalf("type parsing boxed %d runtime string Values, want 0", len(tree.arena.stringLiterals))
	}
}

func TestTypeSyntaxIDsFollowDepthFirstArenaOrder(t *testing.T) {
	tree, err := (&parser{source: `type X = A<B> | C?`}).parse()
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	statements, _ := tree.statementIDs()
	alias, _ := tree.typeAliasArena(statements[0])
	root := alias.value
	children := tree.typeChildren(root)
	args := tree.typeArgs(children[0])
	inner, _ := tree.typeInner(children[1])
	want := []typeID{root, children[0], args[0], children[1], inner}
	first := tree.typeID(root)
	for i, value := range want {
		if got := tree.typeID(value); got != first+syntaxID(i) {
			t.Fatalf("DFS type %d has syntax ID %d, want %d", i, got, first+syntaxID(i))
		}
	}
}

func TestTypeArenaChargesSyntaxLimitBeforeAppending(t *testing.T) {
	p := parser{
		source: `type T = A | B | C | D`,
		limits: CompileLimits{MaxSyntaxNodes: 3},
	}
	_, err := p.parse()
	assertCompileLimitKind(t, err, LimitSyntaxNodes)
	if got := len(p.arena.types.nodes); got != 3 {
		t.Fatalf("type arena contains %d nodes after limit crossing, want 3", got)
	}
}
