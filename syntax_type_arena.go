package ember

import (
	"math"
)

// typeKind is the compact discriminant for arenaType. Zero is deliberately
// invalid so a missing or malformed typeID cannot be mistaken for a real type.
type typeKind uint8

const (
	typeKindInvalid typeKind = iota
	typeKindName
	typeKindUnion
	typeKindIntersection
	typeKindNilable
	typeKindTable
	typeKindFunction
	typeKindGenericFunction
	typeKindTypeof
	typeKindVariadic
	typeKindGenericPack
	typeKindSingleton
)

type arenaType struct {
	id         syntaxID
	start, end int
	kind       typeKind
	scalarKind ValueKind
	payload    uint64
	children   nodeSpan // typeID for unions and intersections
}

type arenaNamedTypeID uint32
type arenaTableTypeID uint32
type arenaFunctionTypeID uint32

type arenaTypeFieldAccess uint8

const (
	arenaTypeFieldAccessNone arenaTypeFieldAccess = iota
	arenaTypeFieldAccessRead
	arenaTypeFieldAccessWrite
)

type arenaNamedType struct {
	parts nodeSpan // stringID
	args  nodeSpan // typeID
}

type arenaTableType struct {
	fields nodeSpan // arenaTypeField
}

type arenaFunctionType struct {
	typeParams  nodeSpan // stringID
	typeParamID syntaxID
	typePacks   nodeSpan // stringID
	typePackID  syntaxID
	params      nodeSpan // arenaTypeParam
	returnType  typeID
}

type arenaTypeField struct {
	access arenaTypeFieldAccess
	name   stringID
	key    typeID
	value  typeID
}

type arenaTypeParam struct {
	name     stringID
	value    typeID
	variadic bool
}

// syntaxTypeArena owns every type node and every variable-length type payload.
// All IDs are one-based and every resolver rejects malformed indexes and spans.
type syntaxTypeArena struct {
	nodes     []arenaType
	named     []arenaNamedType
	tables    []arenaTableType
	functions []arenaFunctionType
	typeIDs   []typeID
	stringIDs []stringID
	fields    []arenaTypeField
	params    []arenaTypeParam
}

func newSyntaxTypeArena(_ int) syntaxTypeArena {
	// Most scripts do not use type syntax. Keep the zero value ready for use so
	// those parses pay no backing-array allocations; the first append in each
	// type family grows only the storage that source actually needs.
	return syntaxTypeArena{}
}

func (a *syntaxTypeArena) append(node arenaType) (typeID, bool) {
	if a == nil || uint64(len(a.nodes)) >= math.MaxUint32 {
		return 0, false
	}
	a.nodes = append(a.nodes, node)
	return typeID(len(a.nodes)), true
}

func (a *syntaxTypeArena) node(id typeID) (arenaType, bool) {
	if a == nil {
		return arenaType{}, false
	}
	return arenaNode(a.nodes, id)
}

func (a *syntaxTypeArena) setNode(id typeID, node arenaType) bool {
	if a == nil || id == 0 || uint64(id) > uint64(len(a.nodes)) {
		return false
	}
	a.nodes[id-1] = node
	return true
}

func (a *syntaxTypeArena) appendNamed(node arenaNamedType) (arenaNamedTypeID, bool) {
	if a == nil || uint64(len(a.named)) >= math.MaxUint32 {
		return 0, false
	}
	a.named = append(a.named, node)
	return arenaNamedTypeID(len(a.named)), true
}

func (a *syntaxTypeArena) namedType(id arenaNamedTypeID) (arenaNamedType, bool) {
	if a == nil {
		return arenaNamedType{}, false
	}
	return arenaNode(a.named, id)
}

func (a *syntaxTypeArena) appendTable(node arenaTableType) (arenaTableTypeID, bool) {
	if a == nil || uint64(len(a.tables)) >= math.MaxUint32 {
		return 0, false
	}
	a.tables = append(a.tables, node)
	return arenaTableTypeID(len(a.tables)), true
}

func (a *syntaxTypeArena) tableType(id arenaTableTypeID) (arenaTableType, bool) {
	if a == nil {
		return arenaTableType{}, false
	}
	return arenaNode(a.tables, id)
}

func (a *syntaxTypeArena) appendFunction(node arenaFunctionType) (arenaFunctionTypeID, bool) {
	if a == nil || uint64(len(a.functions)) >= math.MaxUint32 {
		return 0, false
	}
	a.functions = append(a.functions, node)
	return arenaFunctionTypeID(len(a.functions)), true
}

func (a *syntaxTypeArena) functionType(id arenaFunctionTypeID) (arenaFunctionType, bool) {
	if a == nil {
		return arenaFunctionType{}, false
	}
	return arenaNode(a.functions, id)
}

func (a *syntaxTypeArena) spanTypeIDs(span nodeSpan) ([]typeID, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.typeIDs, span)
}

func (a *syntaxTypeArena) spanStringIDs(span nodeSpan) ([]stringID, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.stringIDs, span)
}

func (a *syntaxTypeArena) spanFields(span nodeSpan) ([]arenaTypeField, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.fields, span)
}

func (a *syntaxTypeArena) spanParams(span nodeSpan) ([]arenaTypeParam, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.params, span)
}
