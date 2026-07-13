package ember

import (
	"fmt"
	"strings"
)

type TypeRef uint32
type PackRef uint32

type typeStore struct {
	tree      syntaxTree
	types     []typeNode
	packs     []typePackNode
	aliases   map[string]typeID
	resolving map[string]bool
}

type typeRelationBudget struct {
	remaining int
	exhausted bool
}

type typeNode struct {
	kind       TypeSummaryKind
	display    string
	name       []string
	typeParams []string
	typePacks  []string
	typeArgs   []TypeRef
	types      []TypeRef
	inner      TypeRef
	properties []typePropertyNode
	indexers   []typeIndexerNode
	metatable  TypeRef
	params     []TypeRef
	returnType TypeRef
	paramsPack PackRef
	returnPack PackRef
}

type typePropertyNode struct {
	name   string
	access string
	typ    TypeRef
}

type typeIndexerNode struct {
	access string
	key    TypeRef
	value  TypeRef
}

type typePackNode struct {
	kind    TypeSummaryKind
	display string
	head    []TypeRef
	tail    TypeRef
	name    []string
}

type loweredTypeAlias struct {
	name       string
	exported   bool
	start      int
	end        int
	nameStart  int
	nameEnd    int
	typeParams []string
	typePacks  []string
	typ        TypeRef
}

func newTypeStoreTree(tree syntaxTree) *typeStore {
	store := &typeStore{tree: tree}
	store.addType(typeNode{kind: TypeSummaryUnknown, display: "unknown"})
	store.addPack(typePackNode{kind: TypeSummaryUnknown, display: "unknown"})
	return store
}

func newTypeRelationBudget(limit int) typeRelationBudget {
	if limit <= 0 {
		limit = 64
	}
	return typeRelationBudget{remaining: limit}
}

func (b *typeRelationBudget) spend() bool {
	if b == nil {
		return true
	}
	if b.remaining <= 0 {
		b.exhausted = true
		return false
	}
	b.remaining--
	return true
}

func (s *typeStore) addType(node typeNode) TypeRef {
	s.types = append(s.types, node)
	return TypeRef(len(s.types))
}

func (s *typeStore) addPack(node typePackNode) PackRef {
	s.packs = append(s.packs, node)
	return PackRef(len(s.packs))
}

func lowerTypeAliases(store *typeStore, statements []statementID) []loweredTypeAlias {
	if store == nil {
		return nil
	}
	store.bindAliases(statements)
	var aliases []loweredTypeAlias
	for _, statement := range statements {
		alias, ok := store.tree.typeAliasArena(statement)
		if !ok {
			continue
		}
		name, _ := store.tree.stringValue(alias.name)
		aliases = append(aliases, loweredTypeAlias{
			name:       name,
			exported:   alias.exported,
			typeParams: consumerStatementStrings(store.tree, alias.typeParams),
			typePacks:  consumerStatementStrings(store.tree, alias.typePacks),
			typ:        store.lowerType(alias.value),
			start:      alias.start,
			end:        alias.end,
			nameStart:  alias.nameStart,
			nameEnd:    alias.nameEnd,
		})
	}
	return aliases
}

func (s *typeStore) bindAliases(statements []statementID) {
	if s.aliases == nil {
		s.aliases = make(map[string]typeID)
	}
	for _, statement := range statements {
		alias, ok := s.tree.typeAliasArena(statement)
		if !ok || alias.typeParams.count != 0 || alias.typePacks.count != 0 {
			continue
		}
		name, _ := s.tree.stringValue(alias.name)
		s.aliases[name] = alias.value
	}
}

func lowerExportedTypeAliases(store *typeStore, statements []statementID) []loweredTypeAlias {
	lowered := lowerTypeAliases(store, statements)
	exports := make([]loweredTypeAlias, 0, len(lowered))
	for _, item := range lowered {
		if item.exported {
			exports = append(exports, item)
		}
	}
	return exports
}

func consumerStatementStrings(tree syntaxTree, span nodeSpan) []string {
	ids, ok := tree.statementStrings(span)
	if !ok {
		return nil
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		if value, ok := tree.stringValue(id); ok {
			values[i] = value
		}
	}
	return values
}

func consumerStatementTypes(tree syntaxTree, span nodeSpan) []typeID {
	ids, ok := tree.statementTypes(span)
	if !ok {
		return nil
	}
	return ids
}

func (s *typeStore) lowerType(expr typeID) TypeRef {
	if expr == 0 {
		return TypeRef(1)
	}
	switch s.tree.typeKind(expr) {
	case typeKindName:
		if resolved, ok := s.lowerAliasReference(expr); ok {
			return resolved
		}
		args := s.tree.typeArgs(expr)
		typeArgs := make([]TypeRef, len(args))
		for i, arg := range args {
			typeArgs[i] = s.lowerType(arg)
		}
		nameIDs, _ := s.tree.typeNameIDs(expr)
		name := syntaxStrings(s.tree, nameIDs)
		return s.addType(typeNode{
			kind:     TypeSummaryName,
			display:  s.namedDisplay(name, typeArgs),
			name:     append([]string(nil), name...),
			typeArgs: typeArgs,
		})
	case typeKindUnion:
		refs := s.lowerTypes(s.tree.typeChildren(expr))
		refs = s.normalizeUnion(refs, newTypeRelationBudget(64))
		if len(refs) == 1 {
			return refs[0]
		}
		return s.addType(typeNode{kind: TypeSummaryUnion, display: s.joinDisplays(refs, " | "), types: refs})
	case typeKindIntersection:
		refs := s.lowerTypes(s.tree.typeChildren(expr))
		refs, impossible := s.normalizeIntersection(refs, newTypeRelationBudget(64))
		if impossible {
			return s.addType(typeNode{kind: TypeSummaryNever, display: "never"})
		}
		if len(refs) == 1 {
			return refs[0]
		}
		return s.addType(typeNode{kind: TypeSummaryIntersection, display: s.joinDisplays(refs, " & "), types: refs})
	case typeKindNilable:
		innerID, _ := s.tree.typeInner(expr)
		inner := s.lowerType(innerID)
		return s.addType(typeNode{kind: TypeSummaryNilable, display: s.display(inner) + "?", inner: inner})
	case typeKindTable:
		return s.lowerTableType(expr)
	case typeKindFunction, typeKindGenericFunction:
		return s.lowerFunctionType(expr)
	case typeKindVariadic:
		innerID, hasInner := s.tree.typeInner(expr)
		inner := s.lowerType(innerID)
		display := "..."
		if hasInner {
			display += s.display(inner)
		}
		return s.addType(typeNode{kind: TypeSummaryVariadic, display: display, inner: inner})
	case typeKindGenericPack:
		innerID, ok := s.tree.typeInner(expr)
		if !ok {
			return TypeRef(1)
		}
		inner := s.lowerType(innerID)
		nameIDs, _ := s.tree.typeNameIDs(innerID)
		name := syntaxStrings(s.tree, nameIDs)
		display := s.display(inner)
		if display == "" {
			display = "..."
		}
		return s.addType(typeNode{kind: TypeSummaryGenericPack, display: display + "...", name: name, inner: inner})
	case typeKindSingleton:
		return s.addType(typeNode{kind: TypeSummarySingleton, display: typeSingletonDisplay(s.tree, expr)})
	case typeKindTypeof:
		return s.addType(typeNode{kind: TypeSummaryTypeof, display: "typeof"})
	default:
		return TypeRef(1)
	}
}

func (s *typeStore) lowerAliasReference(expr typeID) (TypeRef, bool) {
	nameIDs, ok := s.tree.typeNameIDs(expr)
	if expr == 0 || !ok || len(nameIDs) != 1 || len(s.tree.typeArgs(expr)) != 0 {
		return 0, false
	}
	name, _ := s.tree.stringValue(nameIDs[0])
	alias, ok := s.aliases[name]
	if !ok {
		return 0, false
	}
	if s.resolving == nil {
		s.resolving = make(map[string]bool)
	}
	if s.resolving[name] {
		return 0, false
	}
	s.resolving[name] = true
	ref := s.lowerType(alias)
	delete(s.resolving, name)
	return ref, true
}

func (s *typeStore) lowerTypes(expressions []typeID) []TypeRef {
	refs := make([]TypeRef, len(expressions))
	for i, expr := range expressions {
		refs[i] = s.lowerType(expr)
	}
	return refs
}

func (s *typeStore) normalizeUnion(refs []TypeRef, budget typeRelationBudget) []TypeRef {
	if len(refs) < 2 {
		return refs
	}
	seen := make(map[string]struct{}, len(refs))
	normalized := make([]TypeRef, 0, len(refs))
	var visit func(TypeRef)
	visit = func(ref TypeRef) {
		if budget.exhausted || !budget.spend() {
			return
		}
		node, ok := s.typeNode(ref)
		if ok && node.kind == TypeSummaryUnion {
			for _, child := range node.types {
				visit(child)
			}
			return
		}
		key := s.normalizationKey(ref)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		normalized = append(normalized, ref)
	}
	for _, ref := range refs {
		visit(ref)
	}
	if budget.exhausted {
		return refs
	}
	return normalized
}

func (s *typeStore) normalizeIntersection(refs []TypeRef, budget typeRelationBudget) ([]TypeRef, bool) {
	if len(refs) < 2 {
		return refs, false
	}
	seen := make(map[string]struct{}, len(refs))
	primitive := ""
	normalized := make([]TypeRef, 0, len(refs))
	var impossible bool
	var visit func(TypeRef)
	visit = func(ref TypeRef) {
		if impossible || budget.exhausted || !budget.spend() {
			return
		}
		node, ok := s.typeNode(ref)
		if ok && node.kind == TypeSummaryNever {
			impossible = true
			return
		}
		if ok && node.kind == TypeSummaryIntersection {
			for _, child := range node.types {
				visit(child)
			}
			return
		}
		key := s.normalizationKey(ref)
		if _, ok := seen[key]; ok {
			return
		}
		if name, ok := primitiveTypeName(node, ok); ok {
			if primitive != "" && primitive != name {
				impossible = true
				return
			}
			primitive = name
		}
		seen[key] = struct{}{}
		normalized = append(normalized, ref)
	}
	for _, ref := range refs {
		visit(ref)
	}
	if budget.exhausted {
		return refs, false
	}
	return normalized, impossible
}

func primitiveTypeName(node typeNode, ok bool) (string, bool) {
	if !ok || node.kind != TypeSummaryName || len(node.name) != 1 {
		return "", false
	}
	switch node.name[0] {
	case "nil", "boolean", "number", "string", "thread", "userdata", "buffer", "vector":
		return node.name[0], true
	default:
		return "", false
	}
}

func (s *typeStore) normalizationKey(ref TypeRef) string {
	node, ok := s.typeNode(ref)
	if !ok {
		return "unknown"
	}
	return string(node.kind) + ":" + node.display
}

func (s *typeStore) lowerTableType(expr typeID) TypeRef {
	node := typeNode{kind: TypeSummaryTable, display: "table"}
	for _, field := range s.tree.typeFields(expr) {
		value := s.lowerType(s.tree.typeFieldValue(field))
		if s.tree.typeFieldName(field) != "" {
			node.properties = append(node.properties, typePropertyNode{
				name:   s.tree.typeFieldName(field),
				access: s.tree.typeFieldAccess(field),
				typ:    value,
			})
			continue
		}
		if key := s.tree.typeFieldKey(field); key != 0 {
			node.indexers = append(node.indexers, typeIndexerNode{
				access: s.tree.typeFieldAccess(field),
				key:    s.lowerType(key),
				value:  value,
			})
		}
	}
	return s.addType(node)
}

func (s *typeStore) lowerFunctionType(expr typeID) TypeRef {
	kind := TypeSummaryFunction
	if s.tree.typeKind(expr) == typeKindGenericFunction {
		kind = TypeSummaryGenericFunction
	}
	params := make([]TypeRef, 0, len(s.tree.typeParams(expr)))
	paramsSyntax := s.tree.typeParams(expr)
	for _, param := range paramsSyntax {
		params = append(params, s.lowerType(s.tree.typeParamValue(param)))
	}
	returnSyntax, _ := s.tree.typeReturn(expr)
	ret := s.lowerType(returnSyntax)
	paramsPack := s.lowerTypePackValues(paramsSyntax)
	returnPack := s.lowerReturnTypePack(returnSyntax)
	typeParamIDs, _ := s.tree.typeTypeParamIDs(expr)
	typePackIDs, _ := s.tree.typePackIDs(expr)
	return s.addType(typeNode{
		kind:       kind,
		display:    "(" + s.joinDisplays(params, ", ") + ") -> " + s.display(ret),
		typeParams: syntaxStrings(s.tree, typeParamIDs),
		typePacks:  syntaxStrings(s.tree, typePackIDs),
		params:     params,
		returnType: ret,
		paramsPack: paramsPack,
		returnPack: returnPack,
	})
}

func (s *typeStore) lowerTypePackValues(values []arenaTypeParam) PackRef {
	head := make([]TypeRef, 0, len(values))
	for _, value := range values {
		head = append(head, s.lowerType(s.tree.typeParamValue(value)))
	}
	return s.addPack(typePackNode{
		kind:    TypeSummaryFunction,
		display: s.joinDisplays(head, ", "),
		head:    head,
	})
}

func (s *typeStore) lowerReturnTypePack(expr typeID) PackRef {
	if expr != 0 && s.tree.typeKind(expr) == typeKindVariadic {
		tail := s.lowerType(expr)
		return s.addPack(typePackNode{
			kind:    TypeSummaryVariadic,
			display: s.display(tail),
			tail:    tail,
		})
	}
	head := []TypeRef{s.lowerType(expr)}
	return s.addPack(typePackNode{
		kind:    TypeSummaryFunction,
		display: s.joinDisplays(head, ", "),
		head:    head,
	})
}

func (s *typeStore) namedDisplay(name []string, args []TypeRef) string {
	display := strings.Join(name, ".")
	if display == "" {
		display = "unknown"
	}
	if len(args) == 0 {
		return display
	}
	return display + "<" + s.joinDisplays(args, ", ") + ">"
}

func (s *typeStore) display(ref TypeRef) string {
	node, ok := s.typeNode(ref)
	if !ok || node.display == "" {
		return "unknown"
	}
	return node.display
}

func (s *typeStore) joinDisplays(refs []TypeRef, separator string) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, len(refs))
	for i, ref := range refs {
		parts[i] = s.display(ref)
	}
	return strings.Join(parts, separator)
}

func (s *typeStore) typeNode(ref TypeRef) (typeNode, bool) {
	index := int(ref) - 1
	if s == nil || index < 0 || index >= len(s.types) {
		return typeNode{}, false
	}
	return s.types[index], true
}

func (s *typeStore) packNode(ref PackRef) (typePackNode, bool) {
	index := int(ref) - 1
	if s == nil || index < 0 || index >= len(s.packs) {
		return typePackNode{}, false
	}
	return s.packs[index], true
}

func (s *typeStore) summary(ref TypeRef) TypeSummary {
	node, ok := s.typeNode(ref)
	if !ok {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	summary := TypeSummary{
		Kind:       node.kind,
		Display:    node.display,
		TypeParams: append([]string(nil), node.typeParams...),
		TypePacks:  append([]string(nil), node.typePacks...),
	}
	for _, ref := range node.types {
		summary.Types = append(summary.Types, s.summary(ref))
	}
	if node.inner != 0 {
		inner := s.summary(node.inner)
		summary.Inner = &inner
	}
	for _, property := range node.properties {
		summary.Properties = append(summary.Properties, TablePropertySummary{
			Name:   property.name,
			Access: property.access,
			Type:   s.summary(property.typ),
		})
	}
	for _, indexer := range node.indexers {
		summary.Indexers = append(summary.Indexers, TableIndexerSummary{
			Access: indexer.access,
			Key:    s.summary(indexer.key),
			Value:  s.summary(indexer.value),
		})
	}
	if node.metatable != 0 {
		metatable := s.summary(node.metatable)
		summary.Metatable = &metatable
	}
	for _, param := range node.params {
		summary.Params = append(summary.Params, s.summary(param))
	}
	if node.returnType != 0 {
		ret := s.summary(node.returnType)
		summary.Return = &ret
	}
	if node.paramsPack != 0 {
		summary.ParamPack = s.packSummary(node.paramsPack)
	}
	if node.returnPack != 0 {
		summary.ReturnPack = s.packSummary(node.returnPack)
	}
	return summary
}

func (s *typeStore) packSummary(ref PackRef) TypePackSummary {
	node, ok := s.packNode(ref)
	if !ok {
		return TypePackSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	summary := TypePackSummary{
		Kind:    node.kind,
		Display: node.display,
	}
	for _, head := range node.head {
		summary.Head = append(summary.Head, s.summary(head))
	}
	if node.tail != 0 {
		tail := s.summary(node.tail)
		summary.Tail = &tail
	}
	return summary
}

func (s *typeStore) debugType(ref TypeRef) string {
	if node, ok := s.typeNode(ref); ok {
		return fmt.Sprintf("%s(%s)", node.kind, node.display)
	}
	return "unknown"
}
