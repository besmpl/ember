package ember

import (
	"reflect"
	"testing"
)

func TestInstructionRegisterIteratorMatchesPredicatesForEveryOpcode(t *testing.T) {
	for _, op := range allOpcodes {
		ins := instruction{op: op, a: 67, b: 71, c: 3, d: 2}
		for _, access := range []instructionRegisterAccess{instructionRegisterRead, instructionRegisterWrite, instructionRegisterReadWrite} {
			got := collectInstructionRegistersForTest(ins, access)
			var want []int
			for register := 0; register < instructionRegisterLimit(ins); register++ {
				reads := instructionReadsRegister(ins, register)
				writes := instructionWritesRegister(ins, register)
				if access.matches(reads, writes) {
					want = append(want, register)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s registers are %#v, want %#v", opcodeName(op), access, got, want)
			}
		}
	}
}

func TestInstructionRegisterIteratorCoversDynamicWindowsAbove64(t *testing.T) {
	tests := []struct {
		name   string
		ins    instruction
		access instructionRegisterAccess
		want   []int
	}{
		{name: "fixed call reads", ins: instruction{op: opCall, a: 90, b: 70, c: 3, d: 2}, access: instructionRegisterRead, want: []int{70, 71, 72, 73}},
		{name: "fixed call writes", ins: instruction{op: opCall, a: 90, b: 70, c: 3, d: 2}, access: instructionRegisterWrite, want: []int{90, 91}},
		{name: "open call prefix", ins: instruction{op: opCall, a: 90, b: 70, c: -4, d: 1}, access: instructionRegisterRead, want: []int{70, 71, 72, 73}},
		{name: "local call", ins: instruction{op: opCallLocalOne, a: 90, b: 68, c: 72, d: 3}, access: instructionRegisterRead, want: []int{68, 72, 73, 74}},
		{name: "upvalue call", ins: instruction{op: opCallUpvalueOne, a: 90, b: 2, c: 72, d: 3}, access: instructionRegisterRead, want: []int{72, 73, 74}},
		{name: "method call", ins: instruction{op: opCallMethodOne, a: 70, b: 88, c: 2, d: 3}, access: instructionRegisterRead, want: []int{72, 73, 74, 88}},
		{name: "fixed vararg writes", ins: instruction{op: opVararg, a: 70, b: 4}, access: instructionRegisterWrite, want: []int{70, 71, 72, 73}},
		{name: "concat reads", ins: instruction{op: opConcatChain, a: 90, b: 70, c: 4}, access: instructionRegisterRead, want: []int{70, 71, 72, 73}},
		{name: "array iterator writes", ins: instruction{op: opArrayNext, a: 70, b: 90, c: 91, d: 3}, access: instructionRegisterWrite, want: []int{70, 71, 72}},
		{name: "fixed return reads", ins: instruction{op: opReturn, a: 70, b: 4}, access: instructionRegisterRead, want: []int{70, 71, 72, 73}},
		{name: "open return prefix", ins: instruction{op: opReturn, a: 70, b: -4}, access: instructionRegisterRead, want: []int{70, 71, 72}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := collectInstructionRegistersForTest(test.ins, test.access); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("registers are %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestInstructionRegisterIteratorAllocatesNothing(t *testing.T) {
	ins := instruction{op: opCall, a: 90, b: 70, c: 8, d: 4}
	allocs := testing.AllocsPerRun(1000, func() {
		iterator := instructionRegisters(ins, instructionRegisterReadWrite)
		for {
			_, ok := iterator.next()
			if !ok {
				break
			}
		}
	})
	if allocs != 0 {
		t.Fatalf("instruction register iteration allocated %.0f objects, want 0", allocs)
	}
}

func collectInstructionRegistersForTest(ins instruction, access instructionRegisterAccess) []int {
	var registers []int
	iterator := instructionRegisters(ins, access)
	for {
		register, ok := iterator.next()
		if !ok {
			return registers
		}
		registers = append(registers, register)
	}
}

func registersMatching(ins instruction, matches func(int) bool) []int {
	var registers []int
	iterator := instructionRegisters(ins, instructionRegisterReadWrite)
	for {
		register, ok := iterator.next()
		if !ok {
			return registers
		}
		if matches(register) {
			registers = append(registers, register)
		}
	}
}
