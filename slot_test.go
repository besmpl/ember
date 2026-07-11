package ember

import (
	"math"
	"reflect"
	"testing"
	"unsafe"
)

func TestSlotLayoutAndExplicitNil(t *testing.T) {
	var zero slot
	if got := unsafe.Sizeof(zero); got != 8 {
		t.Fatalf("slot size = %d, want 8", got)
	}
	if got := reflect.TypeOf(zero).Kind(); got != reflect.Uint64 {
		t.Fatalf("slot kind = %v, want uint64", got)
	}

	bits, err := slotNumberBits(zero, nil)
	if err != nil {
		t.Fatalf("decode zero slot: %v", err)
	}
	if bits != math.Float64bits(0) {
		t.Fatalf("zero slot bits = %#x, want +0 bits %#x", bits, math.Float64bits(0))
	}
	if slotNil == zero {
		t.Fatal("slotNil must be explicit; zero slot must remain +0")
	}
	if got := slotTagOf(slotNil); got != slotTagNil {
		t.Fatalf("slotNil tag = %v, want nil tag", got)
	}
	if got := slotValueKind(slotNil); got != NilKind {
		t.Fatalf("slotNil kind = %v, want nil", got)
	}
}

func TestSlotScalarRoundTripPreservesExactBits(t *testing.T) {
	if got := slotValueKind(slotNil); got != NilKind {
		t.Fatalf("nil slot kind = %v, want nil", got)
	}
	for _, want := range []bool{false, true} {
		encoded := slotBool(want)
		if got := slotValueKind(encoded); got != BoolKind {
			t.Fatalf("bool(%v) slot kind = %v, want boolean", want, got)
		}
		got, err := slotBoolValue(encoded)
		if err != nil {
			t.Fatalf("decode bool(%v): %v", want, err)
		}
		if got != want {
			t.Fatalf("decoded bool = %v, want %v", got, want)
		}
	}

	var heap runtimeHeap
	for _, want := range []uint64{
		0x0000_0000_0000_0000, // +0
		0x8000_0000_0000_0000, // -0
		0x7ff0_0000_0000_0000, // +Inf
		0xfff0_0000_0000_0000, // -Inf
		0x7ff0_0000_0000_0001, // signaling NaN
		0x7ff8_0000_0000_0001, // quiet NaN, collides with the tag prefix
		0x7ff8_0000_0000_1234, // another colliding quiet NaN
	} {
		encoded, err := slotFromNumberBits(want, &heap)
		if err != nil {
			t.Fatalf("encode number bits %#x: %v", want, err)
		}
		got, err := slotNumberBits(encoded, &heap)
		if err != nil {
			t.Fatalf("decode number bits %#x: %v", want, err)
		}
		if got != want {
			t.Fatalf("round trip bits = %#x, want %#x", got, want)
		}
		if want&slotTaggedMask == slotTaggedPrefix && slotTagOf(encoded) != slotTagBoxedNumber {
			t.Fatalf("colliding number %#x encoded with tag %v, want boxed number", want, slotTagOf(encoded))
		}
	}
}

func TestSlotHandlePackingValidationAndTypedReferences(t *testing.T) {
	const index = uint32(0x0a_bc_de)
	const generation = uint16(0x1234)
	handleTags := []slotTag{
		slotTagString,
		slotTagTable,
		slotTagClosure,
		slotTagUpvalue,
		slotTagUserdata,
		slotTagHostCallable,
		slotTagBoxedNumber,
	}
	for _, wantTag := range handleTags {
		encoded, err := slotPackHandle(wantTag, index, generation)
		if err != nil {
			t.Fatalf("pack %v handle: %v", wantTag, err)
		}
		wantBits := slotTaggedPrefix |
			uint64(wantTag)<<slotTagShift |
			uint64(generation)<<slotGenerationShift |
			uint64(index)
		if uint64(encoded) != wantBits {
			t.Fatalf("packed %v handle = %#x, want %#x", wantTag, encoded, wantBits)
		}
		gotTag, gotIndex, gotGeneration, err := slotUnpackHandle(encoded)
		if err != nil {
			t.Fatalf("unpack %v handle: %v", wantTag, err)
		}
		if gotTag != wantTag || gotIndex != index || gotGeneration != generation {
			t.Fatalf("unpacked handle = (%v, %d, %d), want (%v, %d, %d)", gotTag, gotIndex, gotGeneration, wantTag, index, generation)
		}
	}

	for _, wantTag := range []slotTag{slotTagString, slotTagTable, slotTagClosure, slotTagUpvalue, slotTagUserdata, slotTagHostCallable} {
		typedNil, err := slotPackTypedNil(wantTag)
		if err != nil {
			t.Fatalf("pack typed nil %v: %v", wantTag, err)
		}
		gotTag, gotIndex, gotGeneration, err := slotUnpackHandle(typedNil)
		if err != nil {
			t.Fatalf("unpack typed nil %v: %v", wantTag, err)
		}
		if gotTag != wantTag || gotIndex != 0 || gotGeneration != 0 {
			t.Fatalf("typed nil = (%v, %d, %d), want (%v, 0, 0)", gotTag, gotIndex, gotGeneration, wantTag)
		}
	}

	for _, wantTag := range []slotTag{slotTagNil, slotTagFalse, slotTagTrue, slotTagNativeID} {
		if _, err := slotPackHandle(wantTag, index, generation); err == nil {
			t.Fatalf("pack %v as a heap handle succeeded", wantTag)
		}
	}
	if _, err := slotPackHandle(slotTagString, uint32(slotIndexMask+1), generation); err == nil {
		t.Fatal("pack handle with an overflowing index succeeded")
	}
	if _, err := slotPackHandle(slotTagString, index, 0); err == nil {
		t.Fatal("pack handle with zero generation succeeded")
	}
	if _, err := slotPackHandle(slotTagString, 0, generation); err == nil {
		t.Fatal("pack handle with zero index succeeded")
	}

	id := nativeFuncID(0xa5)
	native := slotNativeID(id)
	if slotTagOf(native) != slotTagNativeID || slotValueKind(native) != HostFuncKind {
		t.Fatalf("native immediate = tag %v, kind %v", slotTagOf(native), slotValueKind(native))
	}
	gotID, err := slotNativeIDValue(native)
	if err != nil {
		t.Fatalf("decode native immediate: %v", err)
	}
	if gotID != id {
		t.Fatalf("native immediate id = %d, want %d", gotID, id)
	}

	if got := reflect.TypeOf(slot(0)).Kind(); got == reflect.Uintptr {
		t.Fatal("slot must not use uintptr encoding")
	}
}

func TestSlotHandleLifetimeValidationAndGenerationRetirement(t *testing.T) {
	var heap runtimeHeap
	first, err := slotFromNumberBits(0x7ff8_0000_0000_0042, &heap)
	if err != nil {
		t.Fatalf("allocate first boxed number: %v", err)
	}
	tag, index, generation, err := slotUnpackHandle(first)
	if err != nil {
		t.Fatalf("unpack first boxed number: %v", err)
	}
	if tag != slotTagBoxedNumber {
		t.Fatalf("first handle tag = %v, want boxed number", tag)
	}

	wrongKind, err := slotPackHandle(slotTagTable, index, generation)
	if err != nil {
		t.Fatalf("pack wrong-kind handle: %v", err)
	}
	if _, err := slotNumberBits(wrongKind, &heap); err == nil {
		t.Fatal("boxed-number decode accepted a table handle")
	}
	badIndex, err := slotPackHandle(slotTagBoxedNumber, uint32(slotIndexMask), generation)
	if err != nil {
		t.Fatalf("pack out-of-range slab handle: %v", err)
	}
	if _, err := slotNumberBits(badIndex, &heap); err == nil {
		t.Fatal("boxed-number decode accepted an unknown index")
	}

	if err := heap.releaseBoxedNumber(first); err != nil {
		t.Fatalf("release first boxed number: %v", err)
	}
	if _, err := slotNumberBits(first, &heap); err == nil {
		t.Fatal("released boxed-number handle still resolved")
	}
	next, err := slotFromNumberBits(0x7ff8_0000_0000_0043, &heap)
	if err != nil {
		t.Fatalf("allocate reused boxed number: %v", err)
	}
	_, nextIndex, nextGeneration, err := slotUnpackHandle(next)
	if err != nil {
		t.Fatalf("unpack reused boxed number: %v", err)
	}
	if nextIndex != index || nextGeneration == generation {
		t.Fatalf("reused handle = (index %d, generation %d), want index %d and a new generation", nextIndex, nextGeneration, index)
	}

	overflow, err := slotFromNumberBits(0x7ff8_0000_0000_0044, &heap)
	if err != nil {
		t.Fatalf("allocate overflow candidate: %v", err)
	}
	_, overflowIndex, _, err := slotUnpackHandle(overflow)
	if err != nil {
		t.Fatalf("unpack overflow candidate: %v", err)
	}
	heap.boxedNumbers.entries[overflowIndex].generation = slotMaxGeneration
	overflow, err = slotPackHandle(slotTagBoxedNumber, overflowIndex, slotMaxGeneration)
	if err != nil {
		t.Fatalf("pack max-generation handle: %v", err)
	}
	if err := heap.releaseBoxedNumber(overflow); err != nil {
		t.Fatalf("release max-generation handle: %v", err)
	}
	replacement, err := slotFromNumberBits(0x7ff8_0000_0000_0045, &heap)
	if err != nil {
		t.Fatalf("allocate after generation retirement: %v", err)
	}
	_, replacementIndex, _, err := slotUnpackHandle(replacement)
	if err != nil {
		t.Fatalf("unpack replacement handle: %v", err)
	}
	if replacementIndex == overflowIndex {
		t.Fatalf("generation-overflow index %d was reused", replacementIndex)
	}
	if _, err := slotNumberBits(overflow, &heap); err == nil {
		t.Fatal("retired max-generation handle still resolved")
	}
}
