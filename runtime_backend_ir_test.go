package ember

import (
	"reflect"
	"strings"
	"testing"
)

func TestBackendProtoIRBuildsDeterministicCFGEffectsAndLiveness(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 8 do
    if i % 2 == 0 then
        total = total + i
    else
        total = total - 1
    end
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	first, err := buildBackendProtoIR(&image.prototypes[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildBackendProtoIR(&image.prototypes[0])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("backend IR is not deterministic")
	}
	if len(first.blocks) < 4 {
		t.Fatalf("backend block count = %d, want a branch and loop CFG", len(first.blocks))
	}
	loopHeaders := 0
	effectExits := 0
	for blockIndex := range first.blocks {
		block := &first.blocks[blockIndex]
		if block.loopHeader {
			loopHeaders++
		}
		if block.reachable && blockIndex != 0 && block.immediateDominator < 0 {
			t.Fatalf("reachable block %d has no immediate dominator", blockIndex)
		}
	}
	for pc := range first.ops {
		operation := &first.ops[pc]
		if operation.effects != 0 {
			effectExits++
			if operation.exit != backendExitBeforeOperation || !operation.spill.equal(operation.liveBefore) {
				t.Fatalf("PC %d effect exit/spill = %d/%v, want exact pre-operation spill", pc, operation.exit, operation.spill)
			}
		}
	}
	if loopHeaders == 0 {
		t.Fatal("backend IR did not identify a natural loop")
	}
	if effectExits == 0 {
		t.Fatal("backend IR test program did not exercise effect exits")
	}
}

func TestBackendProtoIRRejectsBrokenProofFacts(t *testing.T) {
	proto, err := Compile("local value = { field = 1 }\nreturn value.field")
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProtoIR(&image.prototypes[0])
	if err != nil {
		t.Fatal(err)
	}
	effectPC := -1
	for pc := range ir.ops {
		if ir.ops[pc].effects != 0 {
			effectPC = pc
			break
		}
	}
	if effectPC < 0 {
		t.Fatal("test program has no effectful operation")
	}
	ir.ops[effectPC].exit = backendExitNone
	if err := verifyBackendProtoIR(ir); err == nil || !strings.Contains(err.Error(), "effect has no pre-operation exit") {
		t.Fatalf("verify broken exit = %v", err)
	}
}

func TestBackendRegisterSetRejectsOutOfRangeBits(t *testing.T) {
	set := newBackendRegisterSet(65)
	set[1] = uint64(1) << 63
	if err := verifyBackendRegisterSet(65, set); err == nil {
		t.Fatal("accepted register outside the verified frame")
	}
}
