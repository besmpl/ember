package ember

import (
	"context"
	"math"
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

func TestPrepareCodeImageLowersArithmeticNaturalLoop(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 200 do
    total = total + ((i * 3 - i // 2) % 17)
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 1 {
		t.Fatalf("prepared prototypes = %d, want 1", len(image.prototypes))
	}
	regions := image.prototypes[0].burstRegions
	if len(regions) != 1 {
		t.Fatalf("burst regions = %#v, want one", regions)
	}
	region := regions[0]
	if region.checkPC != 4 || region.bodyPC != 5 || region.latchPC != 12 || region.exitPC != 13 {
		t.Fatalf("burst region PCs = check:%d body:%d latch:%d exit:%d, want 4/5/12/13", region.checkPC, region.bodyPC, region.latchPC, region.exitPC)
	}
	if region.operationCount != 9 || region.iterationCost != 9 {
		t.Fatalf("burst region costs = operations:%d iteration:%d, want 9/9", region.operationCount, region.iterationCost)
	}
	if got := 1 + 200*int(region.iterationCost); got != 1801 {
		t.Fatalf("dynamic primitive count = %d, want 1801", got)
	}
}

func TestMachineBurstBackendMatchesReferenceNumericEdges(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("static burst leaf is Darwin/arm64 only")
	}
	taggedNaN := math.Float64frombits(slotTaggedPrefix | 0x42)
	tests := []struct {
		name      string
		body      machineBurstOperation
		left      float64
		right     float64
		quantum   int
		base      int
		malformed bool
	}{
		{name: "move tagged NaN", body: machineBurstOperation{op: opMove, pc: 11, a: 3, b: 4}, left: taggedNaN},
		{name: "multiply infinity by zero", body: machineBurstOperation{op: opMulK, pc: 11, a: 3, b: 4, bits: math.Float64bits(0)}, left: math.Inf(1)},
		{name: "floor divide by negative zero", body: machineBurstOperation{op: opIDivK, pc: 11, a: 3, b: 4, bits: math.Float64bits(math.Copysign(0, -1))}, left: 1},
		{name: "modulo by zero", body: machineBurstOperation{op: opModK, pc: 11, a: 3, b: 4, bits: math.Float64bits(0)}, left: -1},
		{name: "subtract alias left", body: machineBurstOperation{op: opSub, pc: 11, a: 4, b: 4, c: 5}, left: math.SmallestNonzeroFloat64, right: -math.SmallestNonzeroFloat64},
		{name: "add alias right", body: machineBurstOperation{op: opAdd, pc: 11, a: 5, b: 4, c: 5}, left: math.MaxFloat64, right: math.Inf(-1)},
		{name: "quantum before latch", body: machineBurstOperation{op: opAdd, pc: 11, a: 3, b: 4, c: 5}, left: -0.0, right: math.Copysign(0, -1), quantum: 2},
		{name: "nonzero frame base", body: machineBurstOperation{op: opAdd, pc: 11, a: 3, b: 4, c: 5}, left: 2, right: 3, base: 3},
		{name: "malformed boxed live-in", body: machineBurstOperation{op: opMove, pc: 11, a: 3, b: 4}, malformed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quantum := test.quantum
			if quantum == 0 {
				quantum = machineBurstMaxOperations
			}
			region, operations, guards := burstNumericEdgeFixture(test.body)
			registers := make([]slot, test.base+6)
			numberBits := make([]uint64, len(registers)+2)
			machineBurstSetNumber(registers, numberBits, test.base, 1)
			machineBurstSetNumber(registers, numberBits, test.base+1, 1)
			machineBurstSetNumber(registers, numberBits, test.base+2, 1)
			machineBurstSetNumber(registers, numberBits, test.base+4, test.left)
			machineBurstSetNumber(registers, numberBits, test.base+5, test.right)
			if test.malformed {
				registers[test.base+4] = slot(slotTaggedPrefix | uint64(slotTagBoxedNumber)<<slotTagShift | uint64(2)<<slotGenerationShift | 1)
			}
			wantRegisters := append([]slot(nil), registers...)
			wantNumbers := append([]uint64(nil), numberBits...)
			want := runMachineBurstReference(region, operations, guards, wantRegisters, wantNumbers, test.base, quantum)
			got, supported := runMachineBurst(&region, operations, guards, registers, numberBits, test.base, quantum)
			if !supported {
				t.Fatal("Darwin/arm64 backend reported unsupported")
			}
			if got != want || !reflect.DeepEqual(registers, wantRegisters) || !reflect.DeepEqual(numberBits, wantNumbers) {
				t.Fatalf("backend differs from reference\ncontrol got=%#v want=%#v\nregisters got=%#v want=%#v\nnumbers got=%#v want=%#v", got, want, registers, wantRegisters, numberBits, wantNumbers)
			}
		})
	}
}

func burstNumericEdgeFixture(body machineBurstOperation) (machineBurstRegion, []machineBurstOperation, []machineBurstGuard) {
	operations := []machineBurstOperation{
		{op: opNumericForCheck, pc: 10, a: 0, b: 1, c: 2},
		body,
		{op: opNumericForLoop, pc: 12, a: 0, b: 2},
	}
	guards := []machineBurstGuard{{register: 0, firstPC: 10}, {register: 1, firstPC: 10}, {register: 2, firstPC: 10}}
	switch body.op {
	case opMove, opMulK, opIDivK, opModK:
		guards = append(guards, machineBurstGuard{register: body.b, firstPC: body.pc})
	case opSub, opAdd:
		guards = append(guards, machineBurstGuard{register: body.b, firstPC: body.pc}, machineBurstGuard{register: body.c, firstPC: body.pc})
	}
	return machineBurstRegion{operationCount: 3, guardCount: int32(len(guards)), checkPC: 10, bodyPC: 11, latchPC: 12, exitPC: 13, iterationCost: 3}, operations, guards
}

func TestMachineBurstDescriptorsArePointerFree(t *testing.T) {
	tests := []struct {
		name  string
		value any
		size  uintptr
	}{
		{name: "region", value: machineBurstRegion{}, size: 36},
		{name: "operation", value: machineBurstOperation{}, size: 32},
		{name: "guard", value: machineBurstGuard{}, size: 8},
		{name: "control", value: machineBurstControl{}, size: 24},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			typ := reflect.TypeOf(test.value)
			if machineTestTypeContainsPointers(typ) {
				t.Fatalf("%s contains a pointer", typ)
			}
			if got := typ.Size(); got != test.size {
				t.Fatalf("%s size = %d, want %d (unsafe=%d)", typ, got, test.size, unsafe.Sizeof(test.value))
			}
		})
	}
}

func TestPrepareCodeImageRejectsNonStraightNumericLoop(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 10 do
    if i % 2 == 0 then
        total = total + i
    end
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(image.prototypes[0].burstRegions); got != 0 {
		t.Fatalf("non-straight loop lowered to %d burst regions", got)
	}
}

func TestMachineBurstBackendMatchesReferenceArithmeticLoop(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 200 do
    total = total + ((i * 3 - i // 2) % 17)
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	region := &image.prototypes[0].burstRegions[0]
	reference := bindBurstTestMachine(t, image, *region)
	defer releaseScalarMachine(reference)
	backend := bindBurstTestMachine(t, image, *region)
	defer releaseScalarMachine(backend)

	want := runMachineBurstReference(*region, image.prototypes[0].burstOperations, image.prototypes[0].burstGuards, reference.registers, reference.numberBits, 0, machineBurstMaxOperations)
	got, supported := runMachineBurst(region, image.prototypes[0].burstOperations, image.prototypes[0].burstGuards, backend.registers, backend.numberBits, 0, machineBurstMaxOperations)
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		if supported {
			t.Fatal("portable backend reported assembly support")
		}
		return
	}
	if !supported {
		t.Fatal("Darwin/arm64 backend reported unsupported")
	}
	if got != want || !reflect.DeepEqual(backend.registers, reference.registers) || !reflect.DeepEqual(backend.numberBits, reference.numberBits) {
		t.Fatalf("backend differs from reference\ncontrol got=%#v want=%#v\nregisters got=%#v want=%#v\nnumbers got=%#v want=%#v", got, want, backend.registers, reference.registers, backend.numberBits, reference.numberBits)
	}
}

func TestMachineBurstReferenceMatchesGenericArithmeticLoop(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 200 do
    total = total + ((i * 3 - i // 2) % 17)
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	region := image.prototypes[0].burstRegions[0]

	reference := bindBurstTestMachine(t, image, region)
	defer releaseScalarMachine(reference)
	generic := bindBurstTestMachine(t, image, region)
	defer releaseScalarMachine(generic)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller, err := newExecutionPolicy(ctx, ExecutionLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if controller == nil {
		t.Fatal("generic oracle controller is nil")
	}
	generic.window = newExecutionWindow(controller)

	control := runMachineBurstReference(
		region,
		image.prototypes[0].burstOperations,
		image.prototypes[0].burstGuards,
		reference.registers,
		reference.numberBits,
		0,
		machineBurstMaxOperations,
	)
	if control.status != machineBurstComplete || control.nextPC != 13 || control.failingPC != -1 || control.retired != 1801 {
		t.Fatalf("reference control = %#v, want complete at PC 13 after 1801 operations; registers=%#v operations=%#v", control, reference.registers, image.prototypes[0].burstOperations)
	}
	reference.restartPC = int(control.nextPC)
	if pc, err := runGeneratedScalarMachineLoop(reference); err != nil {
		t.Fatalf("finish reference at PC %d: %v", pc, err)
	}
	generic.restartPC = int(region.checkPC)
	if pc, err := runGeneratedScalarMachineLoop(generic); err != nil {
		t.Fatalf("run generic at PC %d: %v", pc, err)
	}
	if !reflect.DeepEqual(reference.registers, generic.registers) || !reflect.DeepEqual(reference.numberBits, generic.numberBits) || !reflect.DeepEqual(reference.results, generic.results) {
		t.Fatalf("reference state differs from generic\nreference registers=%#v numbers=%#v results=%#v\ngeneric registers=%#v numbers=%#v results=%#v", reference.registers, reference.numberBits, reference.results, generic.registers, generic.numberBits, generic.results)
	}
}

func bindBurstTestMachine(t *testing.T, image *codeImage, region machineBurstRegion) *scalarMachine {
	t.Helper()
	machine, err := bindScalarMachine(image, nil)
	if err != nil {
		t.Fatal(err)
	}
	for pc := 0; pc < int(region.checkPC); pc++ {
		operation := image.prototypes[0].operations[pc]
		if operation.op != opLoadConst {
			releaseScalarMachine(machine)
			t.Fatalf("preheader operation %d = %s, want LOAD_CONST", pc, opcodeName(operation.op))
		}
		if err := machine.loadConstant(int(operation.a), int(operation.b)); err != nil {
			releaseScalarMachine(machine)
			t.Fatalf("load preheader operation %d: %v", pc, err)
		}
	}
	return machine
}
