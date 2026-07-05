package ember

import (
	"fmt"
	"strings"
)

type TypeRef uint32
type PackRef uint32

type typeStore struct {
	types     []typeNode
	packs     []typePackNode
	aliases   map[string]*typeExpression
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
	literal    *Value
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

func newTypeStore() *typeStore {
	store := &typeStore{}
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

func lowerTypeAliases(store *typeStore, statements []statement) []loweredTypeAlias {
	if store == nil {
		store = newTypeStore()
	}
	store.bindAliases(statements)
	var aliases []loweredTypeAlias
	for _, stmt := range statements {
		if stmt.typeAlias == nil {
			continue
		}
		aliases = append(aliases, loweredTypeAlias{
			name:       stmt.typeAlias.name,
			exported:   stmt.typeAlias.exported,
			start:      stmt.typeAlias.start,
			end:        stmt.typeAlias.end,
			nameStart:  stmt.typeAlias.nameStart,
			nameEnd:    stmt.typeAlias.nameEnd,
			typeParams: append([]string(nil), stmt.typeAlias.typeParams...),
			typePacks:  append([]string(nil), stmt.typeAlias.typePacks...),
			typ:        store.lowerType(stmt.typeAlias.value),
		})
	}
	return aliases
}

func (s *typeStore) bindAliases(statements []statement) {
	if s.aliases == nil {
		s.aliases = make(map[string]*typeExpression)
	}
	for _, stmt := range statements {
		if stmt.typeAlias == nil || len(stmt.typeAlias.typeParams) != 0 || len(stmt.typeAlias.typePacks) != 0 {
			continue
		}
		s.aliases[stmt.typeAlias.name] = stmt.typeAlias.value
	}
}

func lowerExportedTypeAliases(store *typeStore, statements []statement) []loweredTypeAlias {
	lowered := lowerTypeAliases(store, statements)
	exports := make([]loweredTypeAlias, 0, len(lowered))
	for _, item := range lowered {
		if item.exported {
			exports = append(exports, item)
		}
	}
	return exports
}

func (s *typeStore) lowerType(expr *typeExpression) TypeRef {
	if expr == nil {
		return TypeRef(1)
	}
	switch expr.kind {
	case typeKindName:
		if resolved, ok := s.lowerAliasReference(expr); ok {
			return resolved
		}
		typeArgs := make([]TypeRef, len(expr.typeArgs))
		for i, arg := range expr.typeArgs {
			typeArgs[i] = s.lowerType(arg)
		}
		return s.addType(typeNode{
			kind:     TypeSummaryName,
			display:  s.namedDisplay(expr.name, typeArgs),
			name:     append([]string(nil), expr.name...),
			typeArgs: typeArgs,
		})
	case typeKindUnion:
		refs := s.lowerTypes(expr.types)
		refs = s.normalizeUnion(refs, newTypeRelationBudget(64))
		if len(refs) == 1 {
			return refs[0]
		}
		return s.addType(typeNode{kind: TypeSummaryUnion, display: s.joinDisplays(refs, " | "), types: refs})
	case typeKindIntersection:
		refs := s.lowerTypes(expr.types)
		refs, impossible := s.normalizeIntersection(refs, newTypeRelationBudget(64))
		if impossible {
			return s.addType(typeNode{kind: TypeSummaryNever, display: "never"})
		}
		if len(refs) == 1 {
			return refs[0]
		}
		return s.addType(typeNode{kind: TypeSummaryIntersection, display: s.joinDisplays(refs, " & "), types: refs})
	case typeKindNilable:
		inner := s.lowerType(expr.inner)
		return s.addType(typeNode{kind: TypeSummaryNilable, display: s.display(inner) + "?", inner: inner})
	case typeKindTable:
		return s.lowerTableType(expr)
	case typeKindFunction, typeKindGenericFunction:
		return s.lowerFunctionType(expr)
	case typeKindVariadic:
		inner := s.lowerType(expr.inner)
		display := "..."
		if expr.inner != nil {
			display += s.display(inner)
		}
		return s.addType(typeNode{kind: TypeSummaryVariadic, display: display, inner: inner})
	case typeKindGenericPack:
		display := strings.Join(expr.name, ".")
		if display == "" {
			display = "..."
		}
		return s.addType(typeNode{kind: TypeSummaryGenericPack, display: display + "...", name: append([]string(nil), expr.name...)})
	case typeKindSingleton:
		display := "unknown"
		if expr.literal != nil {
			display = valueSummaryDisplay(*expr.literal)
		}
		return s.addType(typeNode{kind: TypeSummarySingleton, display: display, literal: expr.literal})
	case typeKindTypeof:
		return s.addType(typeNode{kind: TypeSummaryTypeof, display: "typeof"})
	default:
		return TypeRef(1)
	}
}

func (s *typeStore) lowerAliasReference(expr *typeExpression) (TypeRef, bool) {
	if expr == nil || len(expr.name) != 1 || len(expr.typeArgs) != 0 {
		return 0, false
	}
	alias, ok := s.aliases[expr.name[0]]
	if !ok {
		return 0, false
	}
	if s.resolving == nil {
		s.resolving = make(map[string]bool)
	}
	if s.resolving[expr.name[0]] {
		return 0, false
	}
	s.resolving[expr.name[0]] = true
	ref := s.lowerType(alias)
	delete(s.resolving, expr.name[0])
	return ref, true
}

func (s *typeStore) lowerTypes(expressions []*typeExpression) []TypeRef {
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

func (s *typeStore) lowerTableType(expr *typeExpression) TypeRef {
	node := typeNode{kind: TypeSummaryTable, display: "table"}
	for _, field := range expr.fields {
		value := s.lowerType(field.value)
		if field.name != "" {
			node.properties = append(node.properties, typePropertyNode{
				name:   field.name,
				access: field.access,
				typ:    value,
			})
			continue
		}
		if field.key != nil {
			node.indexers = append(node.indexers, typeIndexerNode{
				access: field.access,
				key:    s.lowerType(field.key),
				value:  value,
			})
		}
	}
	return s.addType(node)
}

func (s *typeStore) lowerFunctionType(expr *typeExpression) TypeRef {
	kind := TypeSummaryFunction
	if expr.kind == typeKindGenericFunction {
		kind = TypeSummaryGenericFunction
	}
	params := make([]TypeRef, 0, len(expr.params))
	for _, param := range expr.params {
		params = append(params, s.lowerType(param.value))
	}
	ret := s.lowerType(expr.returnType)
	paramsPack := s.lowerTypePackValues(expr.params)
	returnPack := s.lowerReturnTypePack(expr.returnType)
	return s.addType(typeNode{
		kind:       kind,
		display:    "(" + s.joinDisplays(params, ", ") + ") -> " + s.display(ret),
		typeParams: append([]string(nil), expr.typeParams...),
		typePacks:  append([]string(nil), expr.typePacks...),
		params:     params,
		returnType: ret,
		paramsPack: paramsPack,
		returnPack: returnPack,
	})
}

func (s *typeStore) lowerTypePackValues(values []typeFunctionParam) PackRef {
	head := make([]TypeRef, 0, len(values))
	for _, value := range values {
		head = append(head, s.lowerType(value.value))
	}
	return s.addPack(typePackNode{
		kind:    TypeSummaryFunction,
		display: s.joinDisplays(head, ", "),
		head:    head,
	})
}

func (s *typeStore) lowerReturnTypePack(expr *typeExpression) PackRef {
	if expr != nil && expr.kind == typeKindVariadic {
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
