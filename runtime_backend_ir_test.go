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
	phis := 0
	for blockIndex := range first.blocks {
		phis += len(first.blocks[blockIndex].phis)
	}
	if phis == 0 {
		t.Fatal("backend IR did not create SSA phis for merged control flow")
	}
	for pc := range first.ops {
		for _, use := range first.ops[pc].uses {
			if use.value == invalidBackendValueID {
				t.Fatalf("PC %d register %d has an unresolved SSA use", pc, use.register)
			}
		}
	}
	for blockIndex := range first.blocks {
		for register, value := range first.blocks[blockIndex].entryValues {
			if value == invalidBackendValueID {
				t.Fatalf("block %d register %d has an unresolved SSA entry", blockIndex, register)
			}
		}
		for register, value := range first.blocks[blockIndex].exitValues {
			if value == invalidBackendValueID {
				t.Fatalf("block %d register %d has an unresolved SSA exit", blockIndex, register)
			}
		}
	}
}

func TestBackendProtoIRRejectsBrokenPhiInput(t *testing.T) {
	proto, err := Compile(`
local value = 0
for index = 1, 4 do
    value = value + index
end
return value
`)
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
	for blockIndex := range ir.blocks {
		if len(ir.blocks[blockIndex].phis) == 0 {
			continue
		}
		ir.blocks[blockIndex].phis[0].inputs[0] = invalidBackendValueID
		if err := verifyBackendProtoIR(ir); err == nil || !strings.Contains(err.Error(), "phi") {
			t.Fatalf("verify broken phi = %v", err)
		}
		return
	}
	t.Fatal("test program produced no SSA phi")
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

func TestBackendProtoIRRejectsUnresolvedSSAFrame(t *testing.T) {
	proto, err := Compile("local value = 1\nreturn value")
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
	ir.blocks[0].entryValues[0] = invalidBackendValueID
	if err := verifyBackendProtoIR(ir); err == nil || !strings.Contains(err.Error(), "unresolved SSA entry") {
		t.Fatalf("verify unresolved SSA frame = %v", err)
	}
}

func TestBackendProtoIRDerivesRepresentationsEscapesCallsAndAccesses(t *testing.T) {
	proto, err := Compile(`
local function add_one(value)
    return value + 1
end
local record = { value = 41 }
local result = add_one(record.value)
return result
`)
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
	foundTable := false
	foundDirectCall := false
	foundStaticAccess := false
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opNewTable:
			if len(operation.defs) != 1 {
				t.Fatalf("NEWTABLE definitions = %d, want 1", len(operation.defs))
			}
			value := ir.values[operation.defs[0].value-1]
			if value.tags != backendTagTable ||
				value.representation != backendRepresentationTable ||
				value.object != backendObjectTable ||
				value.escapes {
				t.Fatalf("NEWTABLE facts = tags %x repr %d object %d escapes %t", value.tags, value.representation, value.object, value.escapes)
			}
			foundTable = true
		case opCall, opCallOne, opCallLocalOne:
			if operation.call.kind != backendCallDirectProto || operation.call.targetProto <= 0 {
				t.Fatalf("call classification = %+v, want direct Proto", operation.call)
			}
			foundDirectCall = true
		case opGetStringField, opGetStringFieldIndex:
			if operation.access.kind != backendAccessStaticProperty || operation.access.constant < 0 {
				t.Fatalf("property classification = %+v, want static property", operation.access)
			}
			foundStaticAccess = true
		}
	}
	if !foundTable || !foundDirectCall || !foundStaticAccess {
		t.Fatalf("derived facts table/direct-call/static-access = %t/%t/%t", foundTable, foundDirectCall, foundStaticAccess)
	}
}

func TestBackendProtoIRMarksReturnedObjectEscaping(t *testing.T) {
	proto, err := Compile("local value = { field = 1 }\nreturn value")
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
	for pc := range ir.ops {
		if ir.ops[pc].op != opNewTable {
			continue
		}
		value := ir.values[ir.ops[pc].defs[0].value-1]
		if !value.escapes {
			t.Fatal("returned table was classified as nonescaping")
		}
		return
	}
	t.Fatal("test program produced no table allocation")
}

func TestBackendProtoIRUsesConservativeUnionAtPhi(t *testing.T) {
	proto, err := Compile(`
local function choose(flag)
    local value = nil
    if flag then
        value = {}
    else
        value = function() return 1 end
    end
    return value
end
return choose(true)
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) < 2 {
		t.Fatal("test program produced no child prototype")
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	wantTags := backendTagTable | backendTagFunction
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.kind != backendValuePhi || value.tags != wantTags {
			continue
		}
		if value.representation != backendRepresentationGeneric || value.object != backendObjectMixed || !value.escapes {
			t.Fatalf("union phi facts = repr %d object %d escapes %t", value.representation, value.object, value.escapes)
		}
		return
	}
	t.Fatal("test program produced no table/function union phi")
}

func TestBackendProtoIRRejectsBrokenCallClassification(t *testing.T) {
	proto, err := Compile(`
local function identity(value)
    return value
end
return identity(1)
`)
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
	for pc := range ir.ops {
		if ir.ops[pc].call.kind == backendCallNone {
			continue
		}
		ir.ops[pc].call = backendCallIR{kind: backendCallDynamic, targetProto: -1, nativeID: -1}
		if err := verifyBackendProtoIR(ir); err == nil || !strings.Contains(err.Error(), "call classification") {
			t.Fatalf("verify broken call classification = %v", err)
		}
		return
	}
	t.Fatal("test program produced no call")
}

func TestBackendProtoIRMaterializesCriticalEdgePhiCopies(t *testing.T) {
	proto, err := Compile(`
local function choose(flag)
    local value = 1
    if flag then
        value = value + 1
    end
    return value
end
return choose(true)
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) < 2 {
		t.Fatal("test program produced no child prototype")
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	for edgeIndex := range ir.edges {
		edge := &ir.edges[edgeIndex]
		if !edge.critical {
			continue
		}
		if len(edge.phiCopies) != ir.registers {
			t.Fatalf("critical edge %d phi copies = %d, want %d", edgeIndex, len(edge.phiCopies), ir.registers)
		}
		edge.phiCopies[0].source = invalidBackendValueID
		if err := verifyBackendProtoIR(ir); err == nil || !strings.Contains(err.Error(), "phi copy") {
			t.Fatalf("verify broken critical edge = %v", err)
		}
		return
	}
	t.Fatal("test program produced no critical edge")
}

func TestBackendRegisterSetRejectsOutOfRangeBits(t *testing.T) {
	set := newBackendRegisterSet(65)
	set[1] = uint64(1) << 63
	if err := verifyBackendRegisterSet(65, set); err == nil {
		t.Fatal("accepted register outside the verified frame")
	}
}

func FuzzBackendIRDeterministicAndNeverPanics(f *testing.F) {
	for _, source := range []string{
		"return 1 + 2",
		"local total = 0 for index = 1, 8 do total = total + index end return total",
		"local function read(value) return value.field end return read({ field = 7 })",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		proto, err := Compile(source)
		if err != nil {
			return
		}
		image, err := proto.preparedCodeImage()
		if err != nil {
			return
		}
		for protoIndex := range image.prototypes {
			prepared := &image.prototypes[protoIndex]
			if !prepared.eligible {
				continue
			}
			first, err := buildBackendProtoIR(prepared)
			if err != nil {
				t.Fatalf("build eligible prototype %d: %v", protoIndex, err)
			}
			second, err := buildBackendProtoIR(prepared)
			if err != nil {
				t.Fatalf("rebuild eligible prototype %d: %v", protoIndex, err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("prototype %d produced nondeterministic backend IR", protoIndex)
			}
		}
	})
}
