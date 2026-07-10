package ember

import (
	"reflect"
	"sort"
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
				writes := instructionWritesRegisterExpected(ins, register)
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

func TestInstructionRegisterEffectsDifferentialAcrossOperandPatterns(t *testing.T) {
	patterns := []instruction{
		{a: 0, b: 0, c: 0, d: 0},
		{a: 1, b: 1, c: 1, d: 1},
		{a: 2, b: 3, c: 4, d: 5},
		{a: 7, b: 2, c: -3, d: -2},
		{a: 20_000, b: 19_998, c: 3, d: 4},
	}
	for _, op := range allOpcodes {
		for patternIndex, pattern := range patterns {
			pattern.op = op
			for _, access := range []instructionRegisterAccess{instructionRegisterRead, instructionRegisterWrite, instructionRegisterReadWrite} {
				got := collectInstructionRegistersForTest(pattern, access)
				want := collectOracleInstructionRegisters(pattern, access)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s pattern %d %s registers are %#v, want %#v", opcodeName(op), patternIndex, access, got, want)
				}
			}
		}
	}
}

func TestInstructionRegisterEffectsDeduplicateOverlappingOperands(t *testing.T) {
	tests := []struct {
		name string
		ins  instruction
		want []int
	}{
		{name: "move same register", ins: instruction{op: opMove, a: 7, b: 7}, want: []int{7}},
		{name: "prepare iterator same register", ins: instruction{op: opPrepareIter, a: 7, b: 7, c: 7}, want: []int{7}},
		{name: "call result overlaps arguments", ins: instruction{op: opCall, a: 7, b: 7, c: 2, d: 2}, want: []int{7, 8, 9}},
		{name: "array next result overlaps inputs", ins: instruction{op: opArrayNext, a: 7, b: 7, c: 7, d: 2}, want: []int{7, 8}},
		{name: "method receiver overlaps result", ins: instruction{op: opCallMethodOne, a: 7, b: 7, d: 2}, want: []int{7, 8, 9, 10}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectInstructionRegistersForTest(test.ins, instructionRegisterReadWrite)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("registers are %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestInstructionRegisterEffectsCoverOpenAndFixedSpans(t *testing.T) {
	tests := []struct {
		name   string
		ins    instruction
		access instructionRegisterAccess
		bound  int
		want   []int
	}{
		{name: "open call results", ins: instruction{op: opCall, a: 3, b: 1, c: -3, d: -1}, access: instructionRegisterWrite, bound: 8, want: []int{3, 4, 5, 6, 7}},
		{name: "open vararg results", ins: instruction{op: opVararg, a: 3, b: -1}, access: instructionRegisterWrite, bound: 8, want: []int{3, 4, 5, 6, 7}},
		{name: "fixed vararg results", ins: instruction{op: opVararg, a: 3, b: 3}, access: instructionRegisterWrite, bound: 8, want: []int{3, 4, 5}},
		{name: "open call arguments", ins: instruction{op: opCall, a: 9, b: 3, c: -4, d: 1}, access: instructionRegisterRead, bound: 8, want: []int{3, 4, 5, 6}},
		{name: "concat span", ins: instruction{op: opConcatChain, a: 9, b: 3, c: 4}, access: instructionRegisterRead, bound: 8, want: []int{3, 4, 5, 6}},
		{name: "open return prefix", ins: instruction{op: opReturn, a: 3, b: -4}, access: instructionRegisterRead, bound: 8, want: []int{3, 4, 5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectInstructionRegistersBoundedForTest(test.ins, test.access, test.bound)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("registers are %#v, want %#v", got, test.want)
			}
		})
	}
}

func collectOracleInstructionRegisters(ins instruction, access instructionRegisterAccess) []int {
	var registers []int
	for register := 0; register < instructionRegisterLimit(ins); register++ {
		reads := instructionReadsRegister(ins, register)
		writes := instructionWritesRegisterExpected(ins, register)
		if access.matches(reads, writes) {
			registers = append(registers, register)
		}
	}
	return registers
}

func collectInstructionRegistersBoundedForTest(ins instruction, access instructionRegisterAccess, bound int) []int {
	var registers []int
	iterator := instructionRegistersBounded(ins, access, bound)
	for register, ok := iterator.next(); ok; register, ok = iterator.next() {
		registers = append(registers, register)
	}
	sort.Ints(registers)
	return registers
}

func instructionWritesRegisterExpected(ins instruction, register int) bool {
	if instructionWritesRegister(ins, register) {
		return true
	}
	switch ins.op {
	case opGetIndex:
		return ins.a == register
	case opFastCall:
		return ins.d > 0 && register >= ins.a && register < ins.a+ins.d
	case opCallMethodOne:
		return register == ins.a+1
	default:
		return false
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

func TestInstructionRegisterEffectsCoverSparseHighDestinations(t *testing.T) {
	ins := instruction{op: opGetIndex, a: 20_000, b: 2, c: 3}
	if !instructionHasRegisterEffect(ins, 20_000, instructionRegisterWrite) {
		t.Fatal("GET_INDEX destination r20000 is not classified as a write")
	}
	if instructionHasRegisterEffect(ins, 19_999, instructionRegisterWrite) {
		t.Fatal("GET_INDEX falsely writes a neighboring register")
	}

	iterator := instructionRegistersBounded(ins, instructionRegisterWrite, 20_001)
	got, ok := iterator.next()
	if !ok || got != 20_000 {
		t.Fatalf("bounded sparse iterator returned (%d, %t), want (20000, true)", got, ok)
	}
	if _, ok := iterator.next(); ok {
		t.Fatal("bounded sparse iterator returned a duplicate register")
	}
}

func TestOpcodeRegisterEffectMetadataIsExhaustive(t *testing.T) {
	for _, op := range allOpcodes {
		meta, ok := opcodeMetadata(op)
		if !ok {
			t.Fatalf("missing metadata for %s", opcodeName(op))
		}
		if err := validateOpcodeRegisterEffects(meta.registerEffects); err != nil {
			t.Fatalf("%s register effects failed validation: %v", opcodeName(op), err)
		}
	}
	invalid := opcodeMetadataTable
	invalid[opAdd].registerEffects.classified = false
	if err := validateOpcodeMetadataTable(invalid); err == nil {
		t.Fatal("metadata validation accepted unclassified register effects")
	}
}

func BenchmarkInstructionRegisterEffectsSparseIDs(b *testing.B) {
	for _, test := range []struct {
		name     string
		register int
	}{
		{name: "r2", register: 2},
		{name: "r20000", register: 20_000},
	} {
		b.Run(test.name, func(b *testing.B) {
			ins := instruction{op: opGetIndex, a: test.register, b: 1, c: 2}
			b.ReportMetric(float64(test.register), "register_id")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				iterator := instructionRegisters(ins, instructionRegisterWrite)
				register, ok := iterator.next()
				if !ok || register != test.register {
					b.Fatalf("sparse iterator returned (%d, %t), want (%d, true)", register, ok, test.register)
				}
				if _, ok := iterator.next(); ok {
					b.Fatal("sparse iterator returned a duplicate register")
				}
			}
		})
	}
}

func collectInstructionRegistersForTest(ins instruction, access instructionRegisterAccess) []int {
	var registers []int
	iterator := instructionRegisters(ins, access)
	for register, ok := iterator.next(); ok; register, ok = iterator.next() {
		registers = append(registers, register)
	}
	sort.Ints(registers)
	return registers
}

func registersMatching(ins instruction, matches func(int) bool) []int {
	var registers []int
	iterator := instructionRegisters(ins, instructionRegisterReadWrite)
	for register, ok := iterator.next(); ok; register, ok = iterator.next() {
		if matches(register) {
			registers = append(registers, register)
		}
	}
	sort.Ints(registers)
	return registers
}

// These intentionally slow predicates are retained only as a differential-test
// oracle while production consumers use the central register-effect metadata.
func instructionRegisterLimit(ins instruction) int {
	limit := 0
	if meta, ok := opcodeMetadata(ins.op); ok {
		operands := [...]struct {
			kind  bytecodeOperandKind
			value int
		}{
			{kind: meta.operands.a, value: ins.a},
			{kind: meta.operands.b, value: ins.b},
			{kind: meta.operands.c, value: ins.c},
			{kind: meta.operands.d, value: ins.d},
		}
		for _, operand := range operands {
			if operand.kind == bytecodeOperandRegister && operand.value+1 > limit {
				limit = operand.value + 1
			}
		}
	}
	switch ins.op {
	case opCall, opCallOne:
		argumentCount := ins.c
		if argumentCount < 0 {
			argumentCount = -argumentCount - 1
		}
		if ins.b+argumentCount+1 > limit {
			limit = ins.b + argumentCount + 1
		}
		if ins.d > 0 && ins.a+ins.d > limit {
			limit = ins.a + ins.d
		}
	case opCallLocalOne, opCallUpvalueOne:
		if ins.c+ins.d > limit {
			limit = ins.c + ins.d
		}
	case opCallMethodOne:
		if ins.a+2 > limit {
			limit = ins.a + 2
		}
		if ins.d > 0 && ins.a+ins.d+2 > limit {
			limit = ins.a + ins.d + 2
		}
	case opFastCall:
		candidate := ins.c
		if ins.d > candidate {
			candidate = ins.d
		}
		if ins.a+candidate > limit {
			limit = ins.a + candidate
		}
	case opArrayNext:
		if ins.a+ins.d > limit {
			limit = ins.a + ins.d
		}
	case opArrayNextJump2:
		if ins.a+2 > limit {
			limit = ins.a + 2
		}
	case opVararg:
		if ins.b > 0 && ins.a+ins.b > limit {
			limit = ins.a + ins.b
		}
	case opConcatChain:
		if ins.b+ins.c > limit {
			limit = ins.b + ins.c
		}
	case opReturn:
		count := ins.b
		if count < 0 {
			count = -count - 1
		}
		if ins.a+count > limit {
			limit = ins.a + count
		}
	}
	if limit < 0 {
		return 0
	}
	return limit
}

func instructionReadsRegister(ins instruction, register int) bool {
	switch ins.op {
	case opMove:
		return ins.b == register
	case opSetGlobal:
		return ins.b == register
	case opSetField, opSetStringField:
		return ins.a == register || ins.c == register
	case opGetStringField:
		return ins.b == register
	case opSetStringFieldIndex:
		return ins.a == register || ins.c == register || ins.d == register
	case opGetStringFieldIndex:
		return ins.b == register || ins.d == register
	case opAddStringField, opSubStringField:
		return ins.a == register || ins.c == register
	case opSetIndex:
		return ins.a == register || ins.b == register || ins.c == register
	case opGetIndex:
		return ins.b == register || ins.c == register
	case opSetUpvalue:
		return ins.b == register
	case opPrepareIter:
		return ins.a == register
	case opArrayNext, opArrayNextJump2:
		return ins.a == register || ins.b == register || ins.c == register
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return ins.b == register || ins.c == register
	case opConcatChain:
		return register >= ins.b && register < ins.b+ins.c
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return ins.b == register
	case opNumericForCheck:
		return ins.a == register || ins.b == register || ins.c == register
	case opNumericForLoop:
		return ins.a == register || ins.b == register
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		return ins.a == register || ins.b == register
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK,
		opJumpIfModKNotEqualK, opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
		return ins.a == register
	case opJumpIfStringFieldNotGreaterR:
		return ins.a == register || ins.c == register
	case opNeg, opLen:
		return ins.b == register
	case opFastCall:
		return register >= ins.a && register < ins.a+ins.c
	case opCall, opCallOne:
		if ins.b == register {
			return true
		}
		if ins.c < 0 {
			prefixCount := -ins.c - 1
			return register > ins.b && register <= ins.b+prefixCount
		}
		return register > ins.b && register <= ins.b+ins.c
	case opCallLocalOne:
		return ins.b == register || register >= ins.c && register < ins.c+ins.d
	case opCallUpvalueOne:
		return register >= ins.c && register < ins.c+ins.d
	case opCallMethodOne:
		return ins.b == register || register >= ins.a+2 && register <= ins.a+1+ins.d
	case opJumpIfFalse, opReturnOne:
		return ins.a == register
	case opReturn:
		if ins.b < 0 {
			prefixCount := -ins.b - 1
			return register >= ins.a && register < ins.a+prefixCount
		}
		return register >= ins.a && register < ins.a+ins.b
	default:
		return false
	}
}

func instructionWritesRegister(ins instruction, register int) bool {
	switch ins.op {
	case opLoadConst, opLoadGlobal, opMove, opNewTable, opGetStringField, opGetStringFieldIndex,
		opClosure, opGetUpvalue, opVararg, opAdd, opSub, opMul, opDiv, opMod,
		opIDiv, opPow, opNeg, opLen, opConcat, opConcatChain, opEqual, opNotEqual, opLess,
		opLessEqual, opGreater, opGreaterEqual, opAddK, opSubK, opMulK,
		opDivK, opModK, opIDivK, opFastCall:
		if ins.op == opVararg && ins.b > 0 {
			return register >= ins.a && register < ins.a+ins.b
		}
		return ins.a == register
	case opNumericForLoop:
		return register == ins.a
	case opPrepareIter:
		return ins.a == register || ins.b == register || ins.c == register
	case opArrayNext:
		return register >= ins.a && register < ins.a+ins.d
	case opArrayNextJump2:
		return register == ins.a || register == ins.a+1
	case opCall:
		resultCount := ins.d
		if resultCount == 0 {
			resultCount = 1
		}
		if resultCount < 0 {
			return register >= ins.a
		}
		return register >= ins.a && register < ins.a+resultCount
	case opCallOne, opCallLocalOne, opCallUpvalueOne:
		return register == ins.a
	case opCallMethodOne:
		return register == ins.a || register == ins.a+1
	default:
		return false
	}
}
