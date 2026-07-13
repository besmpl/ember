package ember

import (
	"math"
	"testing"
)

func TestSyntaxTypeArenaRejectsMalformedIDsAndSpans(t *testing.T) {
	arena := newSyntaxTypeArena(4)
	name, ok := arena.append(arenaType{kind: typeKindName})
	if !ok || name == 0 {
		t.Fatal("append type failed")
	}
	if _, ok := arena.node(0); ok {
		t.Fatal("zero type ID resolved")
	}
	if _, ok := arena.node(typeID(math.MaxUint32)); ok {
		t.Fatal("out-of-range type ID resolved")
	}

	arena.typeIDs = append(arena.typeIDs, name)
	if got, ok := arena.spanTypeIDs(nodeSpan{count: 1}); !ok || len(got) != 1 || got[0] != name {
		t.Fatalf("valid type span resolved to %#v, %v", got, ok)
	}
	for _, span := range []nodeSpan{
		{start: 2},
		{start: 1, count: 1},
		{start: math.MaxUint32, count: math.MaxUint32},
	} {
		if got, ok := arena.spanTypeIDs(span); ok || got != nil {
			t.Fatalf("malformed span %#v resolved to %#v, %v", span, got, ok)
		}
	}
}

func TestSyntaxTreeTypeFacadeRejectsMalformedPayloads(t *testing.T) {
	arena := newSyntaxArena(4)
	arena.types.named = append(arena.types.named, arenaNamedType{})
	badName, _ := arena.types.append(arenaType{kind: typeKindName, payload: math.MaxUint32})
	wrappedName, _ := arena.types.append(arenaType{kind: typeKindName, payload: uint64(math.MaxUint32) + 2})
	badTable, _ := arena.types.append(arenaType{kind: typeKindTable, payload: math.MaxUint32})
	badFunction, _ := arena.types.append(arenaType{kind: typeKindFunction, payload: math.MaxUint32})
	badInner, _ := arena.types.append(arenaType{kind: typeKindNilable, payload: math.MaxUint32})
	badChildrenSpan := nodeSpan{start: uint32(len(arena.types.typeIDs)), count: 1}
	arena.types.typeIDs = append(arena.types.typeIDs, typeID(math.MaxUint32))
	badUnion, _ := arena.types.append(arenaType{kind: typeKindUnion, children: badChildrenSpan})
	tree := syntaxTree{arena: arena}

	if _, ok := tree.typeNameIDs(badName); ok {
		t.Fatal("malformed named payload resolved")
	}
	if _, ok := tree.typeNameIDs(wrappedName); ok {
		t.Fatal("overflowing named payload wrapped to a valid ID")
	}
	if _, ok := tree.typeFieldSpan(badTable); ok {
		t.Fatal("malformed table payload resolved")
	}
	if _, ok := tree.typeParamSpan(badFunction); ok {
		t.Fatal("malformed function payload resolved")
	}
	if _, ok := tree.typeInner(badInner); ok {
		t.Fatal("malformed inner type ID resolved")
	}
	if _, ok := tree.typeChildIDs(badUnion); ok {
		t.Fatal("malformed child type ID resolved")
	}
}

func TestSyntaxTreeStatementTypeViewRejectsMalformedIDs(t *testing.T) {
	arena := newSyntaxArena(4)
	valid, _ := arena.types.append(arenaType{kind: typeKindName})
	arena.statements.typeIDs = append(arena.statements.typeIDs, valid, 0, typeID(math.MaxUint32))
	tree := syntaxTree{arena: arena}

	if values, ok := tree.statementTypes(nodeSpan{count: 2}); !ok || len(values) != 2 {
		t.Fatalf("valid annotations resolved to %#v, %v", values, ok)
	}
	if values, ok := tree.statementTypes(nodeSpan{count: 3}); ok || values != nil {
		t.Fatalf("malformed annotations resolved to %#v, %v", values, ok)
	}
}

func TestSyntaxTypeArenaStoresSingletonScalarsInline(t *testing.T) {
	arena := newSyntaxArena(4)
	number, _ := arena.types.append(arenaType{
		kind:       typeKindSingleton,
		scalarKind: NumberKind,
		payload:    math.Float64bits(42.5),
	})
	tree := syntaxTree{arena: arena}

	kind, payload, ok := tree.typeSingletonScalar(number)
	if !ok || kind != NumberKind || payload != math.Float64bits(42.5) {
		t.Fatalf("singleton scalar is (%d, %x, %v), want inline number", kind, payload, ok)
	}
	value, ok := tree.typeLiteral(number)
	got, numberOK := value.Number()
	if !ok || value.Kind() != NumberKind || !numberOK || got != 42.5 {
		t.Fatalf("compatibility Value is %#v, %v; want 42.5", value, ok)
	}
}
