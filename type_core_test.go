package ember

import "testing"

func TestTypeStoreLowersTypeSyntaxToOpaqueReferences(t *testing.T) {
	artifact, err := parseSource(Source{Text: `
--!strict
export type Model<T, U...> = {
	read Name: string,
	write [number]: boolean,
}
return 1
`})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}

	store := newTypeStoreTree(artifact.tree)
	exports := lowerExportedTypeAliases(store, artifact.tree.statements())
	if len(exports) != 1 {
		t.Fatalf("lowered %d exports, want 1", len(exports))
	}
	exported := exports[0]
	if exported.name != "Model" {
		t.Fatalf("export name is %q, want Model", exported.name)
	}
	if exported.typ == 0 {
		t.Fatal("export type ref is zero, want opaque type ref")
	}
	if len(exported.typeParams) != 1 || exported.typeParams[0] != "T" {
		t.Fatalf("export type params are %#v, want T", exported.typeParams)
	}
	if len(exported.typePacks) != 1 || exported.typePacks[0] != "U" {
		t.Fatalf("export type packs are %#v, want U", exported.typePacks)
	}

	summary := store.summary(exported.typ)
	if summary.Kind != TypeSummaryTable {
		t.Fatalf("summary kind is %q, want table", summary.Kind)
	}
	if len(summary.Properties) != 1 || summary.Properties[0].Name != "Name" {
		t.Fatalf("summary properties are %#v, want Name", summary.Properties)
	}
	if len(summary.Indexers) != 1 || summary.Indexers[0].Key.Display != "number" {
		t.Fatalf("summary indexers are %#v, want [number]", summary.Indexers)
	}
}

func TestTypeStoreLowersFunctionPacksToOpaqueReferences(t *testing.T) {
	artifact, err := parseSource(Source{Text: `
	--!strict
	export type Signal<T, U...> = (T, U...) -> (...T)
return 1
`})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}

	store := newTypeStoreTree(artifact.tree)
	exports := lowerExportedTypeAliases(store, artifact.tree.statements())
	if len(exports) != 1 {
		t.Fatalf("lowered %d exports, want 1", len(exports))
	}
	node, ok := store.typeNode(exports[0].typ)
	if !ok {
		t.Fatalf("missing type node for ref %d", exports[0].typ)
	}
	if node.paramsPack == 0 {
		t.Fatal("function params pack ref is zero, want opaque pack ref")
	}
	if node.returnPack == 0 {
		t.Fatal("function return pack ref is zero, want opaque pack ref")
	}
	params, ok := store.packNode(node.paramsPack)
	if !ok {
		t.Fatalf("missing params pack node for ref %d", node.paramsPack)
	}
	if len(params.head) != 2 {
		t.Fatalf("params pack has %d head refs, want 2", len(params.head))
	}
	returns, ok := store.packNode(node.returnPack)
	if !ok {
		t.Fatalf("missing return pack node for ref %d", node.returnPack)
	}
	if returns.tail == 0 {
		t.Fatal("return pack tail ref is zero, want variadic tail type ref")
	}
}

func TestTypeStoreKeepsGenericFunctionParameters(t *testing.T) {
	artifact, err := parseSource(Source{Text: `
	--!strict
	export type Mapper = <T, U...>(T, U...) -> T
	return 1
	`})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}

	store := newTypeStoreTree(artifact.tree)
	exports := lowerExportedTypeAliases(store, artifact.tree.statements())
	if len(exports) != 1 {
		t.Fatalf("lowered %d exports, want 1", len(exports))
	}
	node, ok := store.typeNode(exports[0].typ)
	if !ok {
		t.Fatalf("missing type node for ref %d", exports[0].typ)
	}
	if node.kind != TypeSummaryGenericFunction {
		t.Fatalf("node kind is %q, want generic function", node.kind)
	}
	if len(node.typeParams) != 1 || node.typeParams[0] != "T" {
		t.Fatalf("node type params are %#v, want T", node.typeParams)
	}
	if len(node.typePacks) != 1 || node.typePacks[0] != "U" {
		t.Fatalf("node type packs are %#v, want U", node.typePacks)
	}

	summary := store.summary(exports[0].typ)
	if len(summary.TypeParams) != 1 || summary.TypeParams[0] != "T" {
		t.Fatalf("summary type params are %#v, want T", summary.TypeParams)
	}
	if len(summary.TypePacks) != 1 || summary.TypePacks[0] != "U" {
		t.Fatalf("summary type packs are %#v, want U", summary.TypePacks)
	}
}

func TestTypeStoreSummarizesTableMetatable(t *testing.T) {
	artifact, err := parseSource(Source{Text: "return 1"})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	store := newTypeStoreTree(artifact.tree)
	stringRef := store.lowerType(&typeExpression{kind: typeKindName, name: []string{"string"}})
	indexRef := store.addType(typeNode{
		kind:    TypeSummaryTable,
		display: "table",
		properties: []typePropertyNode{{
			name: "__index",
			typ:  stringRef,
		}},
	})
	tableRef := store.addType(typeNode{
		kind:      TypeSummaryTable,
		display:   "table",
		metatable: indexRef,
	})

	summary := store.summary(tableRef)
	if summary.Metatable == nil {
		t.Fatal("summary metatable is nil, want metatable summary")
	}
	if len(summary.Metatable.Properties) != 1 || summary.Metatable.Properties[0].Name != "__index" {
		t.Fatalf("metatable properties are %#v, want __index", summary.Metatable.Properties)
	}
	if summary.Metatable.Properties[0].Type.Display != "string" {
		t.Fatalf("metatable __index type is %#v, want string", summary.Metatable.Properties[0].Type)
	}
}

func TestTypeStoreNormalizesNestedIntersectionDuplicates(t *testing.T) {
	artifact, err := parseSource(Source{Text: `
	--!strict
	export type Text = (string & string) & string
return 1
`})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}

	store := newTypeStoreTree(artifact.tree)
	exports := lowerExportedTypeAliases(store, artifact.tree.statements())
	if len(exports) != 1 {
		t.Fatalf("lowered %d exports, want 1", len(exports))
	}
	summary := store.summary(exports[0].typ)
	if summary.Kind != TypeSummaryName || summary.Display != "string" {
		t.Fatalf("summary is %#v, want normalized string name", summary)
	}
}
