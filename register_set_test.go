package ember

import (
	"reflect"
	"testing"
)

func TestRegisterSetUsesInlineAndOverflowWords(t *testing.T) {
	var set registerSet
	for _, register := range []int{0, 1, 63, 64, 65, 130} {
		set.add(register)
	}

	if want := []int{0, 1, 63, 64, 65, 130}; !reflect.DeepEqual(set.values(), want) {
		t.Fatalf("register set values are %#v, want %#v", set.values(), want)
	}
	for _, register := range []int{0, 1, 63, 64, 65, 130} {
		if !set.contains(register) {
			t.Errorf("register set does not contain %d", register)
		}
	}
	for _, register := range []int{-1, 2, 66, 129, 131} {
		if set.contains(register) {
			t.Errorf("register set unexpectedly contains %d", register)
		}
	}
}

func TestRegisterSetCopyUnionAndSubtractAreIndependent(t *testing.T) {
	var left registerSet
	left.add(1)
	left.add(70)
	copy := left.copy()
	copy.add(130)
	if left.contains(130) {
		t.Fatal("adding to copied register set mutated original")
	}

	var right registerSet
	right.add(2)
	right.add(70)
	copy.addAll(right)
	if want := []int{1, 2, 70, 130}; !reflect.DeepEqual(copy.values(), want) {
		t.Fatalf("union values are %#v, want %#v", copy.values(), want)
	}
	copy.removeAll(right)
	if want := []int{1, 130}; !reflect.DeepEqual(copy.values(), want) {
		t.Fatalf("subtracted values are %#v, want %#v", copy.values(), want)
	}
	if !copy.equal(copy.copy()) || copy.equal(left) {
		t.Fatal("register set equality does not match contents")
	}
}

func TestRegisterSetInlineOperationsAllocateNothing(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		var set registerSet
		set.add(1)
		set.add(63)
		set.remove(1)
		_ = set.contains(63)
		set.clear()
	})
	if allocs != 0 {
		t.Fatalf("inline register set operations allocated %.0f objects, want 0", allocs)
	}
}

func TestBytecodeIRLivenessTracksRegistersAbove64(t *testing.T) {
	ir := []bytecodeIRInstruction{
		lowerInstructionToBytecodeIR(instruction{op: opReturnOne, a: 70}, sourceRange{}),
	}
	liveness := bytecodeIRLiveness(ir)
	if len(liveness) != 1 {
		t.Fatalf("liveness has %d blocks, want 1", len(liveness))
	}
	if want := []int{70}; !reflect.DeepEqual(liveness[0].liveIn.values(), want) {
		t.Fatalf("live-in registers are %#v, want %#v", liveness[0].liveIn.values(), want)
	}
}
