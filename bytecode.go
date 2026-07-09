package ember

import (
	"fmt"
	"sort"
)

type opcode uint8

const (
	opNoop opcode = iota
	opLoadConst
	opLoadGlobal
	opSetGlobal
	opMove
	opNewTable
	opSetField
	opGetField
	opSetStringField
	opSetStringFieldIndex
	opGetStringField
	opGetStringFieldIndex
	opAddStringField
	opSubStringField
	opSetIndex
	opGetIndex
	opClosure
	opGetUpvalue
	opSetUpvalue
	opVararg
	opPrepareIter
	opArrayNext
	opArrayNextJump2
	opAdd
	opSub
	opMul
	opDiv
	opMod
	opIDiv
	opPow
	opNeg
	opLen
	opConcat
	opConcatChain
	opAddK
	opSubK
	opMulK
	opDivK
	opModK
	opIDivK
	opEqual
	opNotEqual
	opLess
	opLessEqual
	opGreater
	opGreaterEqual
	opNumericForCheck
	opNumericForLoop
	opJumpIfNotEqualK
	opJumpIfNotLessK
	opJumpIfNotGreaterK
	opJumpIfLessK
	opJumpIfGreaterK
	opJumpIfNotLess
	opJumpIfNotGreater
	opJumpIfLess
	opJumpIfGreater
	opJumpIfModKNotEqualK
	opJumpIfTableHasMetatable
	opJumpIfStringFieldNotEqualK
	opJumpIfStringFieldNotGreaterK
	opJumpIfStringFieldGreaterK
	opJumpIfStringFieldNotGreaterR
	opJumpIfStringFieldFalse
	opJumpIfStringFieldNil
	opJumpIfStringFieldTrue
	opJumpIfStringFieldNotNil
	opCoroutineResume
	opFastCall
	opCall
	opCallOne
	opCallLocalOne
	opCallUpvalueOne
	opCallMethodOne
	opJumpIfFalse
	opJump
	opReturnOne
	opReturn
	opcodeCount
)

type opcodeMetadataEntry struct {
	name                         string
	directFrame                  bool
	controlFlow                  opcodeControlFlowKind
	jumpTarget                   opcodeJumpTargetSlot
	operands                     opcodeOperandShape
	mayCall                      bool
	mayYield                     bool
	readsTable                   bool
	writesTable                  bool
	readsGlobal                  bool
	writesGlobal                 bool
	allocates                    bool
	directFrameUnsupportedReason string
}

type opcodeOperandShape struct {
	a bytecodeOperandKind
	b bytecodeOperandKind
	c bytecodeOperandKind
	d bytecodeOperandKind
}

var opcodeMetadataTable = func() [opcodeCount]opcodeMetadataEntry {
	var table [opcodeCount]opcodeMetadataEntry
	for op := opcode(0); op < opcodeCount; op++ {
		table[op].name = opcodeName(op)
	}
	for _, op := range []opcode{
		opLoadConst,
		opLoadGlobal,
		opSetGlobal,
		opNewTable,
		opSetField,
		opGetField,
		opSetStringField,
		opSetStringFieldIndex,
		opGetStringField,
		opGetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSetIndex,
		opGetIndex,
		opClosure,
		opGetUpvalue,
		opSetUpvalue,
		opVararg,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opMove,
		opAdd,
		opSub,
		opMul,
		opDiv,
		opMod,
		opIDiv,
		opAddK,
		opSubK,
		opMulK,
		opDivK,
		opModK,
		opIDivK,
		opPow,
		opNeg,
		opLen,
		opConcat,
		opConcatChain,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opNumericForCheck,
		opNumericForLoop,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
		opCoroutineResume,
		opFastCall,
		opJumpIfFalse,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallMethodOne,
		opJump,
		opReturnOne,
		opReturn,
	} {
		table[op].directFrame = true
	}
	for op := opcode(0); op < opcodeCount; op++ {
		if !table[op].directFrame {
			table[op].directFrameUnsupportedReason = "opcode is not handled by the direct-frame runner"
		}
	}
	for _, op := range []opcode{opJump} {
		table[op].controlFlow = opcodeControlJump
		table[op].jumpTarget = opcodeJumpTargetB
	}
	for _, op := range []opcode{opNumericForLoop} {
		table[op].controlFlow = opcodeControlJump
		table[op].jumpTarget = opcodeJumpTargetD
	}
	for _, op := range []opcode{
		opJumpIfFalse,
	} {
		table[op].controlFlow = opcodeControlBranch
		table[op].jumpTarget = opcodeJumpTargetB
	}
	for _, op := range []opcode{
		opArrayNextJump2,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
	} {
		table[op].controlFlow = opcodeControlBranch
		table[op].jumpTarget = opcodeJumpTargetD
	}
	for _, op := range []opcode{opReturnOne, opReturn} {
		table[op].controlFlow = opcodeControlReturn
	}
	for _, op := range []opcode{
		opCoroutineResume,
		opFastCall,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallMethodOne,
	} {
		table[op].mayCall = true
		table[op].mayYield = true
	}
	for _, op := range []opcode{
		opSetIndex,
		opGetField,
		opGetStringField,
		opGetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opGetIndex,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
		opFastCall,
		opCallMethodOne,
	} {
		table[op].readsTable = true
	}
	for _, op := range []opcode{
		opSetField,
		opSetStringField,
		opSetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSetIndex,
		opFastCall,
	} {
		table[op].writesTable = true
	}
	table[opLoadGlobal].readsGlobal = true
	table[opFastCall].readsGlobal = true
	table[opSetGlobal].writesGlobal = true
	for _, op := range []opcode{
		opNewTable,
		opClosure,
		opVararg,
		opConcat,
		opConcatChain,
		opCoroutineResume,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallMethodOne,
	} {
		table[op].allocates = true
	}

	unused := bytecodeOperandUnused
	register := bytecodeOperandRegister
	constant := bytecodeOperandConstant
	prototype := bytecodeOperandPrototype
	upvalue := bytecodeOperandUpvalue
	jumpTarget := bytecodeOperandJumpTarget
	count := bytecodeOperandCount
	setOperands := func(op opcode, a, b, c, d bytecodeOperandKind) {
		table[op].operands = opcodeOperandShape{a: a, b: b, c: c, d: d}
	}
	setOperands(opNoop, count, unused, unused, unused)
	setOperands(opLoadConst, register, constant, unused, unused)
	setOperands(opLoadGlobal, register, constant, unused, unused)
	setOperands(opSetGlobal, constant, register, unused, unused)
	setOperands(opMove, register, register, unused, unused)
	setOperands(opNewTable, register, count, count, unused)
	setOperands(opSetField, register, constant, register, unused)
	setOperands(opGetField, register, register, constant, unused)
	setOperands(opSetStringField, register, constant, register, unused)
	setOperands(opSetStringFieldIndex, register, constant, register, register)
	setOperands(opGetStringField, register, register, constant, unused)
	setOperands(opGetStringFieldIndex, register, register, constant, register)
	setOperands(opAddStringField, register, constant, register, unused)
	setOperands(opSubStringField, register, constant, register, unused)
	setOperands(opSetIndex, register, register, register, unused)
	setOperands(opGetIndex, register, register, register, unused)
	setOperands(opClosure, register, prototype, unused, unused)
	setOperands(opGetUpvalue, register, upvalue, unused, unused)
	setOperands(opSetUpvalue, upvalue, register, unused, unused)
	setOperands(opVararg, register, count, unused, unused)
	setOperands(opPrepareIter, register, register, register, unused)
	setOperands(opArrayNext, register, register, register, count)
	setOperands(opArrayNextJump2, register, register, register, jumpTarget)
	setOperands(opAdd, register, register, register, unused)
	setOperands(opSub, register, register, register, unused)
	setOperands(opMul, register, register, register, unused)
	setOperands(opDiv, register, register, register, unused)
	setOperands(opMod, register, register, register, unused)
	setOperands(opIDiv, register, register, register, unused)
	setOperands(opPow, register, register, register, unused)
	setOperands(opNeg, register, register, unused, unused)
	setOperands(opLen, register, register, unused, unused)
	setOperands(opConcat, register, register, register, unused)
	setOperands(opConcatChain, register, register, count, unused)
	setOperands(opAddK, register, register, constant, unused)
	setOperands(opSubK, register, register, constant, unused)
	setOperands(opMulK, register, register, constant, unused)
	setOperands(opDivK, register, register, constant, unused)
	setOperands(opModK, register, register, constant, unused)
	setOperands(opIDivK, register, register, constant, unused)
	setOperands(opEqual, register, register, register, unused)
	setOperands(opNotEqual, register, register, register, unused)
	setOperands(opLess, register, register, register, unused)
	setOperands(opLessEqual, register, register, register, unused)
	setOperands(opGreater, register, register, register, unused)
	setOperands(opGreaterEqual, register, register, register, unused)
	setOperands(opNumericForCheck, register, register, register, jumpTarget)
	setOperands(opNumericForLoop, register, register, unused, jumpTarget)
	setOperands(opJumpIfNotEqualK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotLessK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotGreaterK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfLessK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfGreaterK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotLess, register, register, unused, jumpTarget)
	setOperands(opJumpIfNotGreater, register, register, unused, jumpTarget)
	setOperands(opJumpIfLess, register, register, unused, jumpTarget)
	setOperands(opJumpIfGreater, register, register, unused, jumpTarget)
	setOperands(opJumpIfModKNotEqualK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfTableHasMetatable, register, unused, unused, jumpTarget)
	setOperands(opJumpIfStringFieldNotEqualK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfStringFieldNotGreaterK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfStringFieldGreaterK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfStringFieldNotGreaterR, register, constant, register, jumpTarget)
	setOperands(opJumpIfStringFieldFalse, register, constant, count, jumpTarget)
	setOperands(opJumpIfStringFieldNil, register, constant, count, jumpTarget)
	setOperands(opJumpIfStringFieldTrue, register, constant, count, jumpTarget)
	setOperands(opJumpIfStringFieldNotNil, register, constant, count, jumpTarget)
	setOperands(opCoroutineResume, register, count, unused, count)
	setOperands(opFastCall, register, count, count, count)
	setOperands(opCall, register, register, count, count)
	setOperands(opCallOne, register, register, count, count)
	setOperands(opCallLocalOne, register, register, register, count)
	setOperands(opCallUpvalueOne, register, upvalue, register, count)
	setOperands(opCallMethodOne, register, register, constant, count)
	setOperands(opJumpIfFalse, register, jumpTarget, unused, unused)
	setOperands(opJump, unused, jumpTarget, unused, unused)
	setOperands(opReturnOne, register, unused, unused, unused)
	setOperands(opReturn, register, count, unused, unused)
	return table
}()

func init() {
	if err := validateOpcodeMetadataTable(opcodeMetadataTable); err != nil {
		panic(err)
	}
}

func opcodeMetadata(op opcode) (opcodeMetadataEntry, bool) {
	if op >= opcodeCount {
		return opcodeMetadataEntry{}, false
	}
	meta := opcodeMetadataTable[op]
	return meta, meta.name != ""
}

func validateOpcodeMetadataTable(table [opcodeCount]opcodeMetadataEntry) error {
	for op := opcode(0); op < opcodeCount; op++ {
		meta := table[op]
		if meta.name == "" {
			return fmt.Errorf("%s metadata missing name", opcodeName(op))
		}
		if meta.directFrame && meta.directFrameUnsupportedReason != "" {
			return fmt.Errorf("%s direct-frame metadata has unsupported reason", opcodeName(op))
		}
		if !meta.directFrame && meta.directFrameUnsupportedReason == "" {
			return fmt.Errorf("%s direct-frame metadata missing unsupported reason", opcodeName(op))
		}
		if op != opNoop && meta.operands == (opcodeOperandShape{}) {
			return fmt.Errorf("%s metadata missing operand shape", opcodeName(op))
		}
		if (meta.controlFlow == opcodeControlJump || meta.controlFlow == opcodeControlBranch) && meta.jumpTarget == opcodeJumpTargetNone {
			return fmt.Errorf("%s control flow without jump target", opcodeName(op))
		}
		if meta.controlFlow == opcodeControlReturn && meta.jumpTarget != opcodeJumpTargetNone {
			return fmt.Errorf("%s return has jump target", opcodeName(op))
		}
		if meta.mayYield && !meta.mayCall {
			return fmt.Errorf("%s may yield without call risk", opcodeName(op))
		}
		if !opcodeMetadataJumpTargetMatchesOperands(meta) {
			return fmt.Errorf("%s jump target metadata does not match operand shape", opcodeName(op))
		}
	}
	return nil
}

func opcodeMetadataJumpTargetMatchesOperands(meta opcodeMetadataEntry) bool {
	switch meta.jumpTarget {
	case opcodeJumpTargetNone:
		return meta.operands.b != bytecodeOperandJumpTarget && meta.operands.d != bytecodeOperandJumpTarget
	case opcodeJumpTargetB:
		return meta.operands.b == bytecodeOperandJumpTarget && meta.operands.d != bytecodeOperandJumpTarget
	case opcodeJumpTargetD:
		return meta.operands.d == bytecodeOperandJumpTarget && meta.operands.b != bytecodeOperandJumpTarget
	default:
		return false
	}
}

type instruction struct {
	op opcode
	a  int
	b  int
	c  int
	d  int
}

type packedInstruction struct {
	op opcode
	a  int16
	b  int16
	c  int16
	d  int32
	_  uint32
}

func packInstruction(ins instruction) (packedInstruction, error) {
	a, err := packInstructionOperand16(ins.a, "a")
	if err != nil {
		return packedInstruction{}, err
	}
	b, err := packInstructionOperand16(ins.b, "b")
	if err != nil {
		return packedInstruction{}, err
	}
	c, err := packInstructionOperand16(ins.c, "c")
	if err != nil {
		return packedInstruction{}, err
	}
	d, err := packInstructionOperand32(ins.d, "d")
	if err != nil {
		return packedInstruction{}, err
	}
	return packedInstruction{op: ins.op, a: a, b: b, c: c, d: d}, nil
}

func packInstructionOperand16(value int, name string) (int16, error) {
	if value < -32768 || value > 32767 {
		return 0, fmt.Errorf("operand %s value %d out of int16 range", name, value)
	}
	return int16(value), nil
}

func packInstructionOperand32(value int, name string) (int32, error) {
	if int(int32(value)) != value {
		return 0, fmt.Errorf("operand %s value %d out of int32 range", name, value)
	}
	return int32(value), nil
}

func (ins packedInstruction) unpack() instruction {
	return instruction{
		op: ins.op,
		a:  int(ins.a),
		b:  int(ins.b),
		c:  int(ins.c),
		d:  int(ins.d),
	}
}

const tableFieldKeyCallArgMask = 1<<16 - 1

func encodeTableFieldKeyCall(argCount int, keySlot int) int {
	return argCount | ((keySlot + 1) << 16)
}

func tableFieldKeyCallArgCount(encoded int) int {
	return encoded & tableFieldKeyCallArgMask
}

func tableFieldKeyCallKeySlot(encoded int) int {
	return (encoded >> 16) - 1
}

type bytecodeOperandKind int

const (
	bytecodeOperandUnused bytecodeOperandKind = iota
	bytecodeOperandRegister
	bytecodeOperandConstant
	bytecodeOperandPrototype
	bytecodeOperandUpvalue
	bytecodeOperandJumpTarget
	bytecodeOperandCount
)

type bytecodeOperand struct {
	kind  bytecodeOperandKind
	value int
}

type bytecodeOperands struct {
	a bytecodeOperand
	b bytecodeOperand
	c bytecodeOperand
	d bytecodeOperand
}

type bytecodeIRInstruction struct {
	op       opcode
	operands bytecodeOperands
	source   sourceRange
}

type bytecodeIRBlock struct {
	id    int
	start int
	end   int
}

type bytecodeIRLivenessBlock struct {
	block   bytecodeIRBlock
	use     registerSet
	def     registerSet
	liveIn  registerSet
	liveOut registerSet
}

type registerSet map[int]bool

type upvalueDesc struct {
	local bool
	index int
	copy  bool
}

type bytecodeBuilder struct {
	constants             []Value
	constantStringSymbols []int
	ir                    []bytecodeIRInstruction
	prototypes            []*Proto
	source                sourceRange
	sourceText            string
}

func (b *bytecodeBuilder) addConstant(value Value) int {
	for index, existing := range b.constants {
		if bytecodeConstantsEqual(existing, value) {
			return index
		}
	}
	index := len(b.constants)
	b.constants = append(b.constants, value)
	return index
}

func (b *bytecodeBuilder) setConstantStringSymbol(index int, symbol int) {
	if index < 0 || symbol == 0 {
		return
	}
	for len(b.constantStringSymbols) <= index {
		b.constantStringSymbols = append(b.constantStringSymbols, 0)
	}
	b.constantStringSymbols[index] = symbol
}

func bytecodeConstantsEqual(left Value, right Value) bool {
	if left.kind != right.kind {
		return false
	}
	switch left.kind {
	case NilKind, BoolKind, NumberKind, StringKind, TableKind, UserDataKind, FunctionKind:
		return valuesEqual(left, right)
	case HostFuncKind:
		return left.nativeID != nativeFuncUnknown && left.nativeID == right.nativeID
	default:
		return false
	}
}

func (b *bytecodeBuilder) addPrototype(proto *Proto) int {
	index := len(b.prototypes)
	b.prototypes = append(b.prototypes, proto)
	return index
}

func (b *bytecodeBuilder) emit(ins instruction) int {
	return b.emitWithSource(ins, b.source)
}

func (b *bytecodeBuilder) emitWithSource(ins instruction, source sourceRange) int {
	index := len(b.ir)
	b.ir = append(b.ir, lowerInstructionToBytecodeIR(ins, source))
	return index
}

func (b *bytecodeBuilder) emitLoadConst(target int, value Value) int {
	constant := b.addConstant(value)
	return b.emit(instruction{op: opLoadConst, a: target, b: constant})
}

func (b *bytecodeBuilder) emitJump() int {
	return b.emit(instruction{op: opJump})
}

func (b *bytecodeBuilder) emitJumpIfFalse(condition int) int {
	return b.emit(instruction{op: opJumpIfFalse, a: condition})
}

func (b *bytecodeBuilder) patchJump(at int, target int) {
	if b.ir[at].operands.b.kind == bytecodeOperandJumpTarget {
		b.ir[at].operands.b = bytecodeOperand{kind: bytecodeOperandJumpTarget, value: target}
		return
	}
	if b.ir[at].operands.d.kind == bytecodeOperandJumpTarget {
		b.ir[at].operands.d = bytecodeOperand{kind: bytecodeOperandJumpTarget, value: target}
		return
	}
	b.ir[at].operands.b = bytecodeOperand{kind: bytecodeOperandJumpTarget, value: target}
}

func (b *bytecodeBuilder) patchJumpD(at int, target int) {
	b.ir[at].operands.d = bytecodeOperand{kind: bytecodeOperandJumpTarget, value: target}
}

func (b *bytecodeBuilder) pc() int {
	return len(b.ir)
}

func (b *bytecodeBuilder) withSourceRange(source sourceRange, emit func() error) error {
	previous := b.source
	b.source = source
	defer func() {
		b.source = previous
	}()
	return emit()
}

func (b *bytecodeBuilder) assembledCode() []instruction {
	return assembleBytecodeIR(b.ir)
}

func (b *bytecodeBuilder) optimize(options optimizationOptions) {
	b.ir = optimizeBytecodeIRWithFacts(b.ir, bytecodeIROptimizationFacts{
		constants:         b.constants,
		capturedRegisters: bytecodeBuilderCapturedRegisters(b.prototypes),
	}, options)
}

func bytecodeBuilderCapturedRegisters(prototypes []*Proto) []bool {
	var captured []bool
	for _, proto := range prototypes {
		if proto == nil {
			continue
		}
		for _, desc := range proto.upvalues {
			if !desc.local || desc.copy || desc.index < 0 {
				continue
			}
			for len(captured) <= desc.index {
				captured = append(captured, false)
			}
			captured[desc.index] = true
		}
	}
	return captured
}

func (b *bytecodeBuilder) proto(upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	proto := newProtoWithDescriptors(b.constants, b.assembledCode(), b.prototypes, upvalues, registers, params, variadic)
	proto.constantStringSymbols = copyConstantStringSymbols(b.constantStringSymbols, len(proto.constants))
	proto.lines = bytecodeIRLines(b.sourceText, b.ir)
	_ = finalizeProtoExecutionArtifact(proto)
	return proto
}

func copyConstantStringSymbols(symbols []int, count int) []int {
	if count == 0 || len(symbols) == 0 {
		return nil
	}
	copied := make([]int, count)
	copy(copied, symbols)
	return copied
}

func (b *bytecodeBuilder) finalizeProto(upvalues []upvalueDesc, registers int, params int, variadic bool) (*Proto, error) {
	proto := b.proto(upvalues, registers, params, variadic)
	if proto.verifyErr != nil {
		return nil, fmt.Errorf("invalid finalized prototype: %w", proto.verifyErr)
	}
	return proto, nil
}

func lowerInstructionToBytecodeIR(ins instruction, source sourceRange) bytecodeIRInstruction {
	return bytecodeIRInstruction{
		op:       ins.op,
		operands: classifyInstructionOperands(ins),
		source:   source,
	}
}

func lowerInstructionsToBytecodeIR(code []instruction) []bytecodeIRInstruction {
	ir := make([]bytecodeIRInstruction, len(code))
	for i, ins := range code {
		ir[i] = lowerInstructionToBytecodeIR(ins, sourceRange{})
	}
	return ir
}

func classifyInstructionOperands(ins instruction) bytecodeOperands {
	if meta, ok := opcodeMetadata(ins.op); ok {
		return classifyInstructionOperandsFromMetadata(ins, meta.operands)
	}

	switch ins.op {
	case opNoop:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandCount, value: ins.a},
		}
	case opLoadConst:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
		}
	case opLoadGlobal:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
		}
	case opSetGlobal:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
		}
	case opMove:
		return registerOperands(ins.a, ins.b)
	case opNewTable:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandCount, value: ins.c},
		}
	case opSetField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
		}
	case opGetField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
		}
	case opSetStringField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
		}
	case opSetStringFieldIndex:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.d},
		}
	case opGetStringField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
		}
	case opGetStringFieldIndex:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.d},
		}
	case opAddStringField, opSubStringField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
		}
	case opSetIndex, opGetIndex, opPrepareIter:
		return registerOperands(ins.a, ins.b, ins.c)
	case opArrayNext:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opArrayNextJump2:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opClosure:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandPrototype, value: ins.b},
		}
	case opGetUpvalue:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandUpvalue, value: ins.b},
		}
	case opSetUpvalue:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandUpvalue, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
		}
	case opVararg:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
		}
	case opConcatChain:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandCount, value: ins.c},
		}
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return registerOperands(ins.a, ins.b, ins.c)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
		}
	case opNumericForCheck:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opNumericForLoop:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfModKNotEqualK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfTableHasMetatable:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfStringFieldNotGreaterR:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandCount, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opCoroutineResume:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opFastCall:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandCount, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opNeg, opLen:
		return registerOperands(ins.a, ins.b)
	case opCall, opCallOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandCount, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opCallLocalOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opCallUpvalueOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandUpvalue, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opCallMethodOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opJumpIfFalse:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.b},
		}
	case opJump:
		return bytecodeOperands{
			b: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.b},
		}
	case opReturnOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
		}
	case opReturn:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
		}
	default:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandCount, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandCount, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	}
}

func classifyInstructionOperandsFromMetadata(ins instruction, shape opcodeOperandShape) bytecodeOperands {
	return bytecodeOperands{
		a: bytecodeOperandFromMetadata(shape.a, ins.a),
		b: bytecodeOperandFromMetadata(shape.b, ins.b),
		c: bytecodeOperandFromMetadata(shape.c, ins.c),
		d: bytecodeOperandFromMetadata(shape.d, metadataDOperandValue(ins)),
	}
}

func bytecodeOperandFromMetadata(kind bytecodeOperandKind, value int) bytecodeOperand {
	if kind == bytecodeOperandUnused {
		return bytecodeOperand{}
	}
	return bytecodeOperand{kind: kind, value: value}
}

func metadataDOperandValue(ins instruction) int {
	return ins.d
}

func registerOperands(values ...int) bytecodeOperands {
	var operands bytecodeOperands
	slots := []*bytecodeOperand{&operands.a, &operands.b, &operands.c, &operands.d}
	for i, value := range values {
		*slots[i] = bytecodeOperand{kind: bytecodeOperandRegister, value: value}
	}
	return operands
}

type assembledBytecodeIR struct {
	code    []instruction
	sources []sourceRange
}

func assembleBytecodeIR(ir []bytecodeIRInstruction) []instruction {
	return assembleBytecodeIRResult(ir).code
}

func assembleBytecodeIRRaw(ir []bytecodeIRInstruction) []instruction {
	code := make([]instruction, len(ir))
	for i, ins := range ir {
		code[i] = assembleBytecodeIRInstruction(ins)
	}
	return code
}

func assembleBytecodeIRResult(ir []bytecodeIRInstruction) assembledBytecodeIR {
	if len(ir) == 0 {
		return assembledBytecodeIR{}
	}
	drop := bytecodeIRJumpToNextInstructions(ir)
	oldToNew := make([]int, len(ir)+1)
	kept := 0
	for pc := range ir {
		oldToNew[pc] = kept
		if !drop[pc] {
			kept++
		}
	}
	oldToNew[len(ir)] = kept

	assembled := assembledBytecodeIR{
		code:    make([]instruction, 0, kept),
		sources: make([]sourceRange, 0, kept),
	}
	for pc, ins := range ir {
		if drop[pc] {
			continue
		}
		ins = remapAssembledBytecodeIRJumpTargets(ins, oldToNew)
		assembled.code = append(assembled.code, assembleBytecodeIRInstruction(ins))
		assembled.sources = append(assembled.sources, ins.source)
	}
	return assembled
}

func assembleBytecodeIRInstruction(ins bytecodeIRInstruction) instruction {
	return instruction{
		op: ins.op,
		a:  ins.operands.a.value,
		b:  ins.operands.b.value,
		c:  ins.operands.c.value,
		d:  ins.operands.d.value,
	}
}

func bytecodeIRJumpToNextInstructions(ir []bytecodeIRInstruction) []bool {
	drop := make([]bool, len(ir))
	for pc, ins := range ir {
		if ins.op != opJump {
			continue
		}
		if ins.operands.b.kind == bytecodeOperandJumpTarget && ins.operands.b.value == pc+1 {
			drop[pc] = true
		}
	}
	return drop
}

func remapAssembledBytecodeIRJumpTargets(ins bytecodeIRInstruction, oldToNew []int) bytecodeIRInstruction {
	remap := func(operand *bytecodeOperand) {
		if operand.kind != bytecodeOperandJumpTarget || operand.value < 0 || operand.value >= len(oldToNew) {
			return
		}
		operand.value = oldToNew[operand.value]
	}
	remap(&ins.operands.b)
	remap(&ins.operands.d)
	return ins
}

func disassembleBytecodeIR(constants []Value, ir []bytecodeIRInstruction) []string {
	proto := &Proto{
		constants: constants,
		code:      assembleBytecodeIR(ir),
	}
	return disassembleProto(proto)
}

func disassembleBytecodeIRWithSource(constants []Value, ir []bytecodeIRInstruction) []string {
	assembled := assembleBytecodeIRResult(ir)
	lines := disassembleProto(&Proto{constants: constants, code: assembled.code})
	for i := range lines {
		source := assembled.sources[i]
		lines[i] = fmt.Sprintf("%04d [%d,%d) %s", i, source.start, source.end, lines[i][5:])
	}
	return lines
}

func bytecodeIRLines(source string, ir []bytecodeIRInstruction) []int {
	if source == "" || len(ir) == 0 {
		return nil
	}
	assembled := assembleBytecodeIRResult(ir)
	if len(assembled.sources) == 0 {
		return nil
	}
	lines := make([]int, len(assembled.sources))
	hasLine := false
	for i, sourceRange := range assembled.sources {
		line := sourceRangeLine(source, sourceRange)
		lines[i] = line
		if line > 0 {
			hasLine = true
		}
	}
	if !hasLine {
		return nil
	}
	return lines
}

func sourceRangeLine(source string, span sourceRange) int {
	if span.end <= span.start || span.start < 0 || span.start >= len(source) {
		return -1
	}
	line := 1
	for index := 0; index < span.start; index++ {
		if source[index] == '\n' {
			line++
		}
	}
	return line
}

func bytecodeIRBlockOrder(ir []bytecodeIRInstruction) []bytecodeIRBlock {
	if len(ir) == 0 {
		return nil
	}

	leaders := make([]bool, len(ir))
	leaders[0] = true
	for pc, ins := range ir {
		if bytecodeIRInstructionTransfersControl(ins) && pc+1 < len(ir) {
			leaders[pc+1] = true
		}
		if target, ok := bytecodeIRJumpTarget(ins); ok && target >= 0 && target < len(ir) {
			leaders[target] = true
		}
	}

	blocks := make([]bytecodeIRBlock, 0, len(ir))
	for start := 0; start < len(ir); {
		end := start + 1
		for end < len(ir) && !leaders[end] {
			end++
		}
		blocks = append(blocks, bytecodeIRBlock{id: len(blocks), start: start, end: end})
		start = end
	}
	return blocks
}

func bytecodeIRInstructionTransfersControl(ins bytecodeIRInstruction) bool {
	return opcodeTransfersControl(ins.op)
}

func bytecodeIRJumpTarget(ins bytecodeIRInstruction) (int, bool) {
	switch opcodeJumpTarget(ins.op) {
	case opcodeJumpTargetB:
		target := ins.operands.b
		return target.value, target.kind == bytecodeOperandJumpTarget
	case opcodeJumpTargetD:
		target := ins.operands.d
		return target.value, target.kind == bytecodeOperandJumpTarget
	default:
		return 0, false
	}
}

func bytecodeIRLiveness(ir []bytecodeIRInstruction) []bytecodeIRLivenessBlock {
	blocks := bytecodeIRBlockOrder(ir)
	liveness := make([]bytecodeIRLivenessBlock, len(blocks))
	for i, block := range blocks {
		use, def := bytecodeIRBlockUseDef(ir, block)
		liveness[i] = bytecodeIRLivenessBlock{
			block:   block,
			use:     use,
			def:     def,
			liveIn:  make(registerSet),
			liveOut: make(registerSet),
		}
	}

	successors := bytecodeIRBlockSuccessors(ir, blocks)
	changed := true
	for changed {
		changed = false
		for i := len(liveness) - 1; i >= 0; i-- {
			out := make(registerSet)
			for _, successor := range successors[i] {
				out.addAll(liveness[successor].liveIn)
			}

			in := liveness[i].use.copy()
			outWithoutDefs := out.copy()
			outWithoutDefs.removeAll(liveness[i].def)
			in.addAll(outWithoutDefs)

			if !liveness[i].liveOut.equal(out) || !liveness[i].liveIn.equal(in) {
				liveness[i].liveOut = out
				liveness[i].liveIn = in
				changed = true
			}
		}
	}
	return liveness
}

func bytecodeIRBlockUseDef(ir []bytecodeIRInstruction, block bytecodeIRBlock) (registerSet, registerSet) {
	use := make(registerSet)
	def := make(registerSet)
	for pc := block.start; pc < block.end; pc++ {
		for _, register := range bytecodeIRReadRegisters(ir[pc]) {
			if !def[register] {
				use.add(register)
			}
		}
		for _, register := range bytecodeIRWrittenRegisters(ir[pc]) {
			def.add(register)
		}
	}
	return use, def
}

func bytecodeIRBlockSuccessors(ir []bytecodeIRInstruction, blocks []bytecodeIRBlock) [][]int {
	successors := make([][]int, len(blocks))
	blockByStart := make(map[int]int, len(blocks))
	for _, block := range blocks {
		blockByStart[block.start] = block.id
	}
	for _, block := range blocks {
		if block.end == 0 {
			continue
		}
		last := ir[block.end-1]
		switch opcodeControlFlow(last.op) {
		case opcodeControlJump:
			if target, ok := bytecodeIRJumpTarget(last); ok {
				if successor, ok := blockByStart[target]; ok {
					successors[block.id] = append(successors[block.id], successor)
				}
			}
		case opcodeControlBranch:
			if target, ok := bytecodeIRJumpTarget(last); ok {
				if successor, ok := blockByStart[target]; ok {
					successors[block.id] = append(successors[block.id], successor)
				}
			}
			if successor, ok := blockByStart[block.end]; ok {
				successors[block.id] = append(successors[block.id], successor)
			}
		case opcodeControlReturn:
		default:
			if successor, ok := blockByStart[block.end]; ok {
				successors[block.id] = append(successors[block.id], successor)
			}
		}
	}
	return successors
}

func bytecodeIRReadRegisters(ins bytecodeIRInstruction) []int {
	raw := assembleBytecodeIRInstruction(ins)
	return registersMatching(raw, func(register int) bool {
		return instructionReadsRegister(raw, register)
	})
}

func bytecodeIRWrittenRegisters(ins bytecodeIRInstruction) []int {
	raw := assembleBytecodeIRInstruction(ins)
	return registersMatching(raw, func(register int) bool {
		return instructionWritesRegister(raw, register)
	})
}

func registersMatching(ins instruction, matches func(int) bool) []int {
	candidates := registerCandidates(ins)
	registers := make([]int, 0, len(candidates))
	for _, register := range candidates {
		if matches(register) {
			registers = append(registers, register)
		}
	}
	return registers
}

func registerCandidates(ins instruction) []int {
	candidates := make(registerSet)
	addNonNegativeRegisterCandidate(candidates, ins.a)
	addNonNegativeRegisterCandidate(candidates, ins.b)
	addNonNegativeRegisterCandidate(candidates, ins.c)
	addNonNegativeRegisterCandidate(candidates, ins.d)
	if ins.op == opCall || ins.op == opCallOne {
		if ins.c >= 0 {
			for register := ins.b; register <= ins.b+ins.c; register++ {
				addNonNegativeRegisterCandidate(candidates, register)
			}
		} else {
			prefixCount := -ins.c - 1
			for register := ins.b; register <= ins.b+prefixCount; register++ {
				addNonNegativeRegisterCandidate(candidates, register)
			}
		}
		if ins.d > 0 {
			for register := ins.a; register < ins.a+ins.d; register++ {
				addNonNegativeRegisterCandidate(candidates, register)
			}
		}
	}
	if ins.op == opCallUpvalueOne {
		for register := ins.c; register < ins.c+ins.d; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opCallLocalOne {
		for register := ins.c; register < ins.c+ins.d; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opCallMethodOne {
		for register := ins.a + 1; register <= ins.a+1+ins.d; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opCoroutineResume {
		for register := ins.a; register <= ins.a+ins.b; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opFastCall {
		count := ins.c
		if ins.d > count {
			count = ins.d
		}
		for register := ins.a; register < ins.a+count; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opArrayNext {
		for register := ins.a; register < ins.a+ins.d; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opArrayNextJump2 {
		addNonNegativeRegisterCandidate(candidates, ins.a+1)
	}
	if ins.op == opVararg && ins.b > 0 {
		for register := ins.a; register < ins.a+ins.b; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opConcatChain {
		for register := ins.b; register < ins.b+ins.c; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opReturn && ins.b > 0 {
		for register := ins.a; register < ins.a+ins.b; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opReturn && ins.b < 0 {
		prefixCount := -ins.b - 1
		for register := ins.a; register < ins.a+prefixCount; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	return candidates.values()
}

func addNonNegativeRegisterCandidate(registers registerSet, register int) {
	if register >= 0 {
		registers.add(register)
	}
}

func (s registerSet) add(register int) {
	s[register] = true
}

func (s registerSet) addAll(other registerSet) {
	for register := range other {
		s.add(register)
	}
}

func (s registerSet) removeAll(other registerSet) {
	for register := range other {
		delete(s, register)
	}
}

func (s registerSet) copy() registerSet {
	copied := make(registerSet, len(s))
	copied.addAll(s)
	return copied
}

func (s registerSet) equal(other registerSet) bool {
	if len(s) != len(other) {
		return false
	}
	for register := range s {
		if !other[register] {
			return false
		}
	}
	return true
}

func (s registerSet) values() []int {
	values := make([]int, 0, len(s))
	for register := range s {
		values = append(values, register)
	}
	sort.Ints(values)
	return values
}

// Proto is an executable Ember function prototype.
type Proto struct {
	constants               []Value
	constantKeys            []tableKey
	constantKeyOK           []bool
	constantStringSymbols   []int
	constantNumbers         []float64
	constantNumberOK        []bool
	globalNames             []string
	code                    []instruction
	packedCode              []packedInstruction
	lines                   []int
	prototypes              []*Proto
	numericForLoops         []numericForLoopDesc
	intrinsicOps            []intrinsicOpDesc
	constantKindFacts       []constantKindFactDesc
	registerKindFacts       []registerKindFactDesc
	numericOperandFacts     []numericOperandFactDesc
	numericOperandFactPCs   []bool
	slotKindFacts           []slotKindFactDesc
	upvalues                []upvalueDesc
	registers               int
	params                  int
	variadic                bool
	capturedLocals          []bool
	directFrameDispatch     bool
	directFrameIndexCache   bool
	directFrameIndexCaches  []dynamicStringIndexCache
	entryNilRegisters       []int
	reuseZeroCaptureClosure bool
	canonicalClosure        *closure
	verifyErr               error
}

func (proto *Proto) constantStringSymbol(index int) int {
	if proto == nil || index < 0 || index >= len(proto.constantStringSymbols) {
		return 0
	}
	return proto.constantStringSymbols[index]
}

func (proto *Proto) globalSlot(slot int, name string) int {
	if proto == nil || slot < 0 || slot >= len(proto.globalNames) || proto.globalNames[slot] != name {
		return -1
	}
	return slot
}

type numericForLoopDesc struct {
	checkPC     int
	loop        int
	limit       int
	step        int
	exitPC      int
	incrementPC int
}

type intrinsicOpDesc struct {
	pc         int
	op         opcode
	base       int
	args       int
	results    int
	globalName string
	field      string
	nativeID   nativeFuncID
}

type constantKindFactDesc struct {
	constant int
	kind     ValueKind
}

type registerKindFactDesc struct {
	pc       int
	register int
	kind     ValueKind
	source   string
	guarded  bool
}

type numericOperandFactDesc struct {
	pc            int
	op            opcode
	left          int
	right         int
	rightConstant bool
}

type slotKindFactDesc struct {
	pc      int
	table   int
	field   int
	slot    int
	kind    ValueKind
	source  string
	guarded bool
}

type directFrameRejection struct {
	pc     int
	op     opcode
	reason string
}

type executionArtifact struct {
	constantKeys          []tableKey
	constantKeyOK         []bool
	constantNumbers       []float64
	constantNumberOK      []bool
	numericForLoops       []numericForLoopDesc
	intrinsicOps          []intrinsicOpDesc
	constantKindFacts     []constantKindFactDesc
	registerKindFacts     []registerKindFactDesc
	numericOperandFacts   []numericOperandFactDesc
	numericOperandFactPCs []bool
	slotKindFacts         []slotKindFactDesc
	capturedLocals        []bool
	directFrameDispatch   bool
	directFrameIndexCache bool
	entryNilRegisters     []int
}

func newProto(constants []Value, code []instruction, prototypes []*Proto, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	return newProtoWithDescriptors(constants, code, prototypes, upvalues, registers, params, variadic)
}

func newProtoWithDescriptors(constants []Value, code []instruction, prototypes []*Proto, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	proto := &Proto{
		constants:  constants,
		code:       code,
		prototypes: prototypes,
		upvalues:   upvalues,
		registers:  registers,
		params:     params,
		variadic:   variadic,
	}
	_ = finalizeProtoExecutionArtifact(proto)
	return proto
}

func finalizeProtoExecutionArtifact(proto *Proto) error {
	if proto == nil {
		return nil
	}
	assignProtoGlobalSlots(proto)
	artifact := buildExecutionArtifact(proto)
	artifact.apply(proto)
	markReusableZeroCaptureClosures(proto)
	if err := packProtoCode(proto); err != nil {
		proto.verifyErr = err
		return proto.verifyErr
	}
	proto.verifyErr = verifyProto(proto)
	return proto.verifyErr
}

func assignProtoGlobalSlots(proto *Proto) {
	if proto == nil {
		return
	}
	slots := make(map[string]int)
	names := make([]string, 0)
	slotFor := func(name string) int {
		if slot, ok := slots[name]; ok {
			return slot
		}
		slot := len(names)
		slots[name] = slot
		names = append(names, name)
		return slot
	}
	for pc, ins := range proto.code {
		var constant int
		switch ins.op {
		case opLoadGlobal:
			constant = ins.b
		case opSetGlobal:
			constant = ins.a
		default:
			continue
		}
		if constant < 0 || constant >= len(proto.constants) {
			proto.code[pc].c = -1
			continue
		}
		name, ok := proto.constants[constant].String()
		if !ok {
			proto.code[pc].c = -1
			continue
		}
		proto.code[pc].c = slotFor(name)
	}
	proto.globalNames = names
}

func packProtoCode(proto *Proto) error {
	if proto == nil {
		return nil
	}
	packed := make([]packedInstruction, len(proto.code))
	for pc, ins := range proto.code {
		packedIns, err := packInstruction(ins)
		if err != nil {
			return fmt.Errorf("instruction %d %s: %w", pc, opcodeName(ins.op), err)
		}
		packed[pc] = packedIns
	}
	proto.packedCode = packed
	return nil
}

func buildExecutionArtifact(proto *Proto) executionArtifact {
	constantKeys, constantKeyOK := protoConstantTableKeys(proto.constants)
	constantNumbers, constantNumberOK := protoConstantNumbers(proto.constants)
	capturedLocals := capturedLocalRegisters(proto)
	directFrameDispatch := true
	directFrameIndexCache := directFrameDispatch && codeUsesDirectFrameIndexCache(proto.code)
	slotKindFacts := detectSlotKindFacts(proto)
	numericOperandFacts := detectNumericOperandFacts(proto)
	return executionArtifact{
		constantKeys:          constantKeys,
		constantKeyOK:         constantKeyOK,
		constantNumbers:       constantNumbers,
		constantNumberOK:      constantNumberOK,
		numericForLoops:       detectNumericForLoops(proto.code),
		intrinsicOps:          detectIntrinsicOps(proto.code),
		constantKindFacts:     detectConstantKindFacts(proto.constants),
		registerKindFacts:     detectRegisterKindFacts(proto),
		numericOperandFacts:   numericOperandFacts,
		numericOperandFactPCs: numericOperandFactPCs(len(proto.code), numericOperandFacts),
		slotKindFacts:         slotKindFacts,
		capturedLocals:        capturedLocals,
		directFrameDispatch:   directFrameDispatch,
		directFrameIndexCache: directFrameIndexCache,
		entryNilRegisters:     protoEntryNilRegisters(proto.code, proto.params, proto.registers),
	}
}

func (artifact executionArtifact) apply(proto *Proto) {
	proto.constantKeys = artifact.constantKeys
	proto.constantKeyOK = artifact.constantKeyOK
	proto.constantNumbers = artifact.constantNumbers
	proto.constantNumberOK = artifact.constantNumberOK
	proto.numericForLoops = artifact.numericForLoops
	proto.intrinsicOps = artifact.intrinsicOps
	proto.constantKindFacts = artifact.constantKindFacts
	proto.registerKindFacts = artifact.registerKindFacts
	proto.numericOperandFacts = artifact.numericOperandFacts
	proto.numericOperandFactPCs = artifact.numericOperandFactPCs
	proto.slotKindFacts = artifact.slotKindFacts
	proto.capturedLocals = artifact.capturedLocals
	proto.directFrameDispatch = artifact.directFrameDispatch
	proto.directFrameIndexCache = artifact.directFrameIndexCache
	if proto.directFrameIndexCache {
		if len(proto.directFrameIndexCaches) != len(proto.code) {
			proto.directFrameIndexCaches = make([]dynamicStringIndexCache, len(proto.code))
		} else {
			clear(proto.directFrameIndexCaches)
		}
	} else {
		proto.directFrameIndexCaches = nil
	}
	proto.entryNilRegisters = artifact.entryNilRegisters
}

func codeSupportsDirectFrame(code []instruction) bool {
	for _, ins := range code {
		if !directFrameOpcodeSupported(ins.op) {
			return false
		}
	}
	return true
}

func codeUsesDirectFrameIndexCache(code []instruction) bool {
	for _, ins := range code {
		switch ins.op {
		case opSetStringFieldIndex, opGetStringFieldIndex, opSetIndex, opGetIndex:
			return true
		}
	}
	return false
}

func markReusableZeroCaptureClosures(proto *Proto) {
	if proto == nil {
		return
	}
	for _, child := range proto.prototypes {
		child.reuseZeroCaptureClosure = false
		child.canonicalClosure = nil
	}
	for pc, ins := range proto.code {
		if ins.op != opClosure || ins.b < 0 || ins.b >= len(proto.prototypes) {
			continue
		}
		child := proto.prototypes[ins.b]
		if child == nil || len(child.upvalues) != 0 {
			continue
		}
		if closureValueImmediatelyCalled(proto.code, pc, ins.a) {
			child.reuseZeroCaptureClosure = true
		}
	}
}

func closureValueImmediatelyCalled(code []instruction, pc int, register int) bool {
	if pc+1 >= len(code) {
		return false
	}
	next := code[pc+1]
	switch next.op {
	case opCall, opCallOne, opCallLocalOne:
		return next.b == register
	default:
		return false
	}
}

func protoEntryNilRegisters(code []instruction, params int, registers int) []int {
	if len(code) == 0 || registers == 0 {
		return nil
	}
	if registers > 64 {
		return protoEntryNilRegistersFromLiveness(code, params)
	}

	start := uint64(0)
	for register := 0; register < params && register < registers; register++ {
		start |= uint64(1) << register
	}

	for {
		missing := protoEntryMissingRegisterMask(code, registers, start)
		next := start | missing
		if next == start {
			break
		}
		start = next
	}

	nilMask := start
	for register := 0; register < params && register < registers; register++ {
		nilMask &^= uint64(1) << register
	}
	return registerMaskValues(nilMask, registers)
}

func detectNumericForLoops(code []instruction) []numericForLoopDesc {
	var loops []numericForLoopDesc
	for pc, ins := range code {
		if ins.op != opNumericForCheck {
			continue
		}
		loops = append(loops, numericForLoopDesc{
			checkPC:     pc,
			loop:        ins.a,
			limit:       ins.b,
			step:        ins.c,
			exitPC:      ins.d,
			incrementPC: numericForIncrementPC(code, pc, ins),
		})
	}
	return loops
}

func numericForIncrementPC(code []instruction, checkPC int, check instruction) int {
	for pc, ins := range code {
		if ins.op == opNumericForLoop &&
			ins.a == check.a &&
			ins.b == check.c &&
			ins.d == checkPC {
			return pc
		}
		if pc == 0 || ins.op != opJump || ins.b != checkPC {
			continue
		}
		increment := code[pc-1]
		if increment.op == opAdd &&
			increment.a == check.a &&
			increment.b == check.a &&
			increment.c == check.c {
			return pc - 1
		}
	}
	return -1
}

func detectIntrinsicOps(code []instruction) []intrinsicOpDesc {
	var ops []intrinsicOpDesc
	for pc, ins := range code {
		switch ins.op {
		case opCoroutineResume:
			intrinsic, ok := baseFieldIntrinsicForOpcode(ins.op)
			if !ok {
				continue
			}
			ops = append(ops, intrinsicOpDesc{
				pc:         pc,
				op:         ins.op,
				base:       ins.a,
				args:       ins.b,
				results:    ins.d,
				globalName: intrinsic.globalName,
				field:      intrinsic.field,
				nativeID:   intrinsic.nativeID,
			})
		case opFastCall:
			nativeID := nativeFuncID(ins.b)
			globalName, field := fastCallIntrinsicNames(nativeID)
			ops = append(ops, intrinsicOpDesc{
				pc:         pc,
				op:         ins.op,
				base:       ins.a,
				args:       ins.c,
				results:    ins.d,
				globalName: globalName,
				field:      field,
				nativeID:   nativeID,
			})
		}
	}
	return ops
}

func fastCallIntrinsicNames(nativeID nativeFuncID) (string, string) {
	for _, intrinsic := range baseFieldIntrinsics() {
		if intrinsic.nativeID == nativeID {
			return intrinsic.globalName, intrinsic.field
		}
	}
	switch nativeID {
	case nativeFuncRawLen:
		return "rawlen", ""
	case nativeFuncSelect:
		return "select", ""
	default:
		return "", ""
	}
}

func detectConstantKindFacts(constants []Value) []constantKindFactDesc {
	var facts []constantKindFactDesc
	for index, constant := range constants {
		if !kindFactSupportedKind(constant.kind) {
			continue
		}
		facts = append(facts, constantKindFactDesc{constant: index, kind: constant.kind})
	}
	return facts
}

type registerKindState struct {
	kind    ValueKind
	ok      bool
	guarded bool
}

func detectRegisterKindFacts(proto *Proto) []registerKindFactDesc {
	if proto == nil || len(proto.code) == 0 || proto.registers <= 0 {
		return nil
	}
	blockStarts := registerKindBlockStarts(proto.code)
	state := make([]registerKindState, proto.registers)
	var facts []registerKindFactDesc
	for pc, ins := range proto.code {
		if pc > 0 && blockStarts[pc] {
			clearRegisterKindState(state)
		}
		fact, ok := registerKindFactForInstruction(proto, state, pc, ins)
		clearInstructionRegisterKinds(state, ins)
		if ok && fact.register >= 0 && fact.register < len(state) {
			state[fact.register] = registerKindState{
				kind:    fact.kind,
				ok:      true,
				guarded: fact.guarded,
			}
			facts = append(facts, fact)
		}
		if opcodeControlFlow(ins.op) != opcodeControlNone {
			clearRegisterKindState(state)
		}
	}
	return facts
}

func detectNumericOperandFacts(proto *Proto) []numericOperandFactDesc {
	if proto == nil || len(proto.code) == 0 || proto.registers <= 0 {
		return nil
	}
	blockStarts := registerKindBlockStarts(proto.code)
	state := make([]registerKindState, proto.registers)
	var facts []numericOperandFactDesc
	for pc, ins := range proto.code {
		if pc > 0 && blockStarts[pc] {
			clearRegisterKindState(state)
		}
		if fact, ok := numericOperandFactForInstruction(proto, state, pc, ins); ok {
			facts = append(facts, fact)
		}
		fact, ok := registerKindFactForInstruction(proto, state, pc, ins)
		clearInstructionRegisterKinds(state, ins)
		if ok && fact.register >= 0 && fact.register < len(state) {
			state[fact.register] = registerKindState{
				kind:    fact.kind,
				ok:      true,
				guarded: fact.guarded,
			}
		}
		if opcodeControlFlow(ins.op) != opcodeControlNone {
			clearRegisterKindState(state)
		}
	}
	return facts
}

func numericOperandFactForInstruction(proto *Proto, state []registerKindState, pc int, ins instruction) (numericOperandFactDesc, bool) {
	switch ins.op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && registerHasUnguardedKind(state, ins.c, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: ins.c}, true
		}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && constantHasKind(proto, ins.c, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: ins.c, rightConstant: true}, true
		}
	case opNeg:
		if registerHasUnguardedKind(state, ins.b, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: -1}, true
		}
	case opLess, opLessEqual, opGreater, opGreaterEqual:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && registerHasUnguardedKind(state, ins.c, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: ins.c}, true
		}
	}
	return numericOperandFactDesc{}, false
}

func numericOperandFactPCs(codeLen int, facts []numericOperandFactDesc) []bool {
	if codeLen <= 0 {
		return nil
	}
	pcs := make([]bool, codeLen)
	for _, fact := range facts {
		if fact.pc >= 0 && fact.pc < len(pcs) {
			pcs[fact.pc] = true
		}
	}
	return pcs
}

func (proto *Proto) numericOperandsProvenAt(pc int, _ instruction) bool {
	return proto != nil &&
		pc >= 0 &&
		pc < len(proto.numericOperandFactPCs) &&
		proto.numericOperandFactPCs[pc]
}

func registerKindFactForInstruction(proto *Proto, state []registerKindState, pc int, ins instruction) (registerKindFactDesc, bool) {
	switch ins.op {
	case opLoadConst:
		if ins.b < 0 || ins.b >= len(proto.constants) {
			return registerKindFactDesc{}, false
		}
		kind := proto.constants[ins.b].kind
		if !kindFactSupportedKind(kind) {
			return registerKindFactDesc{}, false
		}
		return registerKindFactDesc{pc: pc, register: ins.a, kind: kind, source: "constant"}, true
	case opMove:
		if source, ok := registerKindAt(state, ins.b); ok {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: source.kind, source: "move", guarded: source.guarded}, true
		}
	case opNewTable:
		return registerKindFactDesc{pc: pc, register: ins.a, kind: TableKind, source: "table_literal"}, true
	case opNeg:
		if registerHasUnguardedKind(state, ins.b, NumberKind) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "numeric"}, true
		}
	case opLen:
		return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "length"}, true
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && registerHasUnguardedKind(state, ins.c, NumberKind) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "numeric"}, true
		}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && constantHasKind(proto, ins.c, NumberKind) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "numeric"}, true
		}
	case opEqual, opNotEqual:
		if equalityOperandsHaveSimpleKinds(state, ins.b, ins.c) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: BoolKind, source: "comparison"}, true
		}
	case opLess, opLessEqual, opGreater, opGreaterEqual:
		if orderedComparisonOperandsHaveSimpleKinds(state, ins.b, ins.c) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: BoolKind, source: "comparison"}, true
		}
	case opFastCall:
		if ins.d == 1 && (nativeFuncID(ins.b) == nativeFuncMathMin || nativeFuncID(ins.b) == nativeFuncSelect || nativeFuncID(ins.b) == nativeFuncRawLen) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "guarded_intrinsic", guarded: true}, true
		}
	}
	return registerKindFactDesc{}, false
}

func registerKindBlockStarts(code []instruction) []bool {
	if len(code) == 0 {
		return nil
	}
	starts := make([]bool, len(code))
	starts[0] = true
	for pc, ins := range code {
		switch opcodeControlFlow(ins.op) {
		case opcodeControlJump, opcodeControlBranch:
			if target, ok := instructionJumpTarget(ins); ok && target >= 0 && target < len(code) {
				starts[target] = true
			}
			if pc+1 < len(code) {
				starts[pc+1] = true
			}
		case opcodeControlReturn:
			if pc+1 < len(code) {
				starts[pc+1] = true
			}
		}
	}
	return starts
}

func clearInstructionRegisterKinds(state []registerKindState, ins instruction) {
	for register := range state {
		if instructionWritesRegister(ins, register) {
			state[register] = registerKindState{}
		}
	}
}

func clearRegisterKindState(state []registerKindState) {
	for register := range state {
		state[register] = registerKindState{}
	}
}

func registerKindAt(state []registerKindState, register int) (registerKindState, bool) {
	if register < 0 || register >= len(state) || !state[register].ok {
		return registerKindState{}, false
	}
	return state[register], true
}

func registerHasUnguardedKind(state []registerKindState, register int, kind ValueKind) bool {
	fact, ok := registerKindAt(state, register)
	return ok && !fact.guarded && fact.kind == kind
}

func constantHasKind(proto *Proto, constant int, kind ValueKind) bool {
	return proto != nil &&
		constant >= 0 &&
		constant < len(proto.constants) &&
		proto.constants[constant].kind == kind
}

func equalityOperandsHaveSimpleKinds(state []registerKindState, left int, right int) bool {
	leftKind, ok := registerKindAt(state, left)
	if !ok || leftKind.guarded || !kindFactSimpleEqualityKind(leftKind.kind) {
		return false
	}
	rightKind, ok := registerKindAt(state, right)
	return ok && !rightKind.guarded && kindFactSimpleEqualityKind(rightKind.kind)
}

func orderedComparisonOperandsHaveSimpleKinds(state []registerKindState, left int, right int) bool {
	leftKind, ok := registerKindAt(state, left)
	if !ok || leftKind.guarded {
		return false
	}
	rightKind, ok := registerKindAt(state, right)
	if !ok || rightKind.guarded {
		return false
	}
	return (leftKind.kind == NumberKind && rightKind.kind == NumberKind) ||
		(leftKind.kind == StringKind && rightKind.kind == StringKind)
}

func kindFactSimpleEqualityKind(kind ValueKind) bool {
	switch kind {
	case NilKind, BoolKind, NumberKind, StringKind:
		return true
	default:
		return false
	}
}

func kindFactSupportedKind(kind ValueKind) bool {
	switch kind {
	case NilKind, BoolKind, NumberKind, StringKind, TableKind:
		return true
	default:
		return false
	}
}

type slotKindLiteralState struct {
	nextSlot int
	fields   map[string]int
	ok       bool
}

func detectSlotKindFacts(proto *Proto) []slotKindFactDesc {
	if proto == nil || len(proto.code) == 0 || proto.registers <= 0 {
		return nil
	}
	blockStarts := registerKindBlockStarts(proto.code)
	registerKinds := make([]registerKindState, proto.registers)
	literalSlots := make([]slotKindLiteralState, proto.registers)
	var facts []slotKindFactDesc
	for pc, ins := range proto.code {
		if pc > 0 && blockStarts[pc] {
			clearRegisterKindState(registerKinds)
			clearSlotKindLiteralState(literalSlots)
		}
		if fact, ok := registerKindFactForInstruction(proto, registerKinds, pc, ins); ok && fact.register >= 0 && fact.register < len(registerKinds) {
			clearInstructionRegisterKinds(registerKinds, ins)
			clearInstructionSlotKindLiterals(literalSlots, ins)
			registerKinds[fact.register] = registerKindState{
				kind:    fact.kind,
				ok:      true,
				guarded: fact.guarded,
			}
			if ins.op == opNewTable {
				literalSlots[ins.a] = slotKindLiteralState{
					fields: make(map[string]int),
					ok:     true,
				}
			}
		} else {
			clearInstructionRegisterKinds(registerKinds, ins)
			clearInstructionSlotKindLiterals(literalSlots, ins)
		}
		if fact, ok := slotKindFactForInstruction(proto, registerKinds, literalSlots, pc, ins); ok {
			facts = append(facts, fact)
		}
		if opcodeControlFlow(ins.op) != opcodeControlNone {
			clearRegisterKindState(registerKinds)
			clearSlotKindLiteralState(literalSlots)
		}
	}
	return facts
}

func slotKindFactForInstruction(proto *Proto, registerKinds []registerKindState, literalSlots []slotKindLiteralState, pc int, ins instruction) (slotKindFactDesc, bool) {
	switch ins.op {
	case opSetStringField:
		field, ok := stringConstantText(proto, ins.b)
		if !ok || ins.a < 0 || ins.a >= len(literalSlots) {
			return slotKindFactDesc{}, false
		}
		value, ok := registerKindAt(registerKinds, ins.c)
		if !ok || !kindFactSupportedKind(value.kind) {
			return slotKindFactDesc{}, false
		}
		slot, ok := literalSlotForField(literalSlots, ins.a, field)
		if !ok {
			return slotKindFactDesc{}, false
		}
		return slotKindFactDesc{
			pc:      pc,
			table:   ins.a,
			field:   ins.b,
			slot:    slot,
			kind:    value.kind,
			source:  "table_literal",
			guarded: true,
		}, true
	default:
		return slotKindFactDesc{}, false
	}
}

func literalSlotForField(slots []slotKindLiteralState, register int, field string) (int, bool) {
	if register < 0 || register >= len(slots) || !slots[register].ok {
		return -1, false
	}
	state := &slots[register]
	if slot, ok := state.fields[field]; ok {
		return slot, true
	}
	slot := state.nextSlot
	state.fields[field] = slot
	state.nextSlot++
	return slot, true
}

func clearInstructionSlotKindLiterals(slots []slotKindLiteralState, ins instruction) {
	for register := range slots {
		if instructionWritesRegister(ins, register) {
			slots[register] = slotKindLiteralState{}
		}
	}
}

func clearSlotKindLiteralState(slots []slotKindLiteralState) {
	for register := range slots {
		slots[register] = slotKindLiteralState{}
	}
}

func stringConstantText(proto *Proto, constant int) (string, bool) {
	if proto == nil || constant < 0 || constant >= len(proto.constants) {
		return "", false
	}
	value := proto.constants[constant]
	if value.kind != StringKind {
		return "", false
	}
	return value.stringText(), true
}

func protoEntryMissingRegisterMask(code []instruction, registers int, start uint64) uint64 {
	states := make([]uint64, len(code))
	seen := make([]bool, len(code))
	work := []int{0}
	states[0] = start
	seen[0] = true
	missing := uint64(0)

	for len(work) > 0 {
		pc := work[len(work)-1]
		work = work[:len(work)-1]
		state := states[pc]
		ins := code[pc]
		read := instructionReadMask(ins, registers)
		missingRead := read &^ state
		missing |= missingRead
		state |= missingRead
		state |= instructionWriteMask(ins, registers)

		for _, successor := range instructionSuccessors(code, pc) {
			if successor < 0 || successor >= len(code) {
				continue
			}
			if !seen[successor] {
				seen[successor] = true
				states[successor] = state
				work = append(work, successor)
				continue
			}
			merged := states[successor] & state
			if merged != states[successor] {
				states[successor] = merged
				work = append(work, successor)
			}
		}
	}
	return missing
}

func instructionReadMask(ins instruction, registers int) uint64 {
	mask := uint64(0)
	for register := 0; register < registers; register++ {
		if instructionReadsRegister(ins, register) {
			mask |= uint64(1) << register
		}
	}
	return mask
}

func instructionWriteMask(ins instruction, registers int) uint64 {
	mask := uint64(0)
	for register := 0; register < registers; register++ {
		if instructionWritesRegister(ins, register) {
			mask |= uint64(1) << register
		}
	}
	return mask
}

func instructionSuccessors(code []instruction, pc int) []int {
	ins := code[pc]
	switch opcodeControlFlow(ins.op) {
	case opcodeControlJump:
		target, _ := instructionJumpTarget(ins)
		return []int{target}
	case opcodeControlBranch:
		target, _ := instructionJumpTarget(ins)
		return []int{pc + 1, target}
	case opcodeControlReturn:
		return nil
	default:
		return []int{pc + 1}
	}
}

func registerMaskValues(mask uint64, registers int) []int {
	if mask == 0 {
		return nil
	}
	values := make([]int, 0)
	for register := 0; register < registers; register++ {
		if mask&(uint64(1)<<register) != 0 {
			values = append(values, register)
		}
	}
	return values
}

func protoEntryNilRegistersFromLiveness(code []instruction, params int) []int {
	ir := make([]bytecodeIRInstruction, len(code))
	for i, ins := range code {
		ir[i] = lowerInstructionToBytecodeIR(ins, sourceRange{})
	}
	liveness := bytecodeIRLiveness(ir)
	if len(liveness) == 0 {
		return nil
	}
	liveIn := liveness[0].liveIn.values()
	nilRegisters := liveIn[:0]
	for _, register := range liveIn {
		if register >= params {
			nilRegisters = append(nilRegisters, register)
		}
	}
	if len(nilRegisters) == 0 {
		return nil
	}
	return append([]int(nil), nilRegisters...)
}

func protoConstantTableKeys(constants []Value) ([]tableKey, []bool) {
	keys := make([]tableKey, len(constants))
	ok := make([]bool, len(constants))
	for i, constant := range constants {
		if key, keyOK := constant.String(); keyOK {
			keys[i] = tableKey{kind: StringKind, str: key}
			ok[i] = true
		}
	}
	return keys, ok
}

func protoConstantNumbers(constants []Value) ([]float64, []bool) {
	numbers := make([]float64, len(constants))
	ok := make([]bool, len(constants))
	for i, constant := range constants {
		if number, numberOK := constant.Number(); numberOK {
			numbers[i] = number
			ok[i] = true
		}
	}
	return numbers, ok
}

func verifyProto(proto *Proto) error {
	return verifyProtoSeen(proto, make(map[*Proto]bool))
}

func verifyProtoSeen(proto *Proto, seen map[*Proto]bool) error {
	if proto == nil {
		return fmt.Errorf("nil prototype")
	}
	if seen[proto] {
		return nil
	}
	seen[proto] = true
	if proto.registers < 0 {
		return fmt.Errorf("negative register count %d", proto.registers)
	}
	if proto.params < 0 {
		return fmt.Errorf("negative parameter count %d", proto.params)
	}
	if proto.params > proto.registers {
		return fmt.Errorf("parameter count %d exceeds register count %d", proto.params, proto.registers)
	}
	if want := protoEntryNilRegisters(proto.code, proto.params, proto.registers); !equalIntSlices(proto.entryNilRegisters, want) {
		return fmt.Errorf("entry nil registers %v do not match finalized plan %v", proto.entryNilRegisters, want)
	}
	if want := detectNumericForLoops(proto.code); !equalNumericForLoopDescs(proto.numericForLoops, want) {
		return fmt.Errorf("numeric for descriptors %v do not match finalized plan %v", proto.numericForLoops, want)
	}
	if want := detectIntrinsicOps(proto.code); !equalIntrinsicOpDescs(proto.intrinsicOps, want) {
		return fmt.Errorf("intrinsic descriptors %v do not match finalized plan %v", proto.intrinsicOps, want)
	}
	if want := detectConstantKindFacts(proto.constants); !equalConstantKindFactDescs(proto.constantKindFacts, want) {
		return fmt.Errorf("constant kind facts %v do not match finalized plan %v", proto.constantKindFacts, want)
	}
	if want := detectRegisterKindFacts(proto); !equalRegisterKindFactDescs(proto.registerKindFacts, want) {
		return fmt.Errorf("register kind facts %v do not match finalized plan %v", proto.registerKindFacts, want)
	}
	if want := detectNumericOperandFacts(proto); !equalNumericOperandFactDescs(proto.numericOperandFacts, want) {
		return fmt.Errorf("numeric operand facts %v do not match finalized plan %v", proto.numericOperandFacts, want)
	}
	if want := numericOperandFactPCs(len(proto.code), proto.numericOperandFacts); !equalBoolSlices(proto.numericOperandFactPCs, want) {
		return fmt.Errorf("numeric operand fact pc map %v does not match finalized plan %v", proto.numericOperandFactPCs, want)
	}
	if want := detectSlotKindFacts(proto); !equalSlotKindFactDescs(proto.slotKindFacts, want) {
		return fmt.Errorf("slot kind facts %v do not match finalized plan %v", proto.slotKindFacts, want)
	}
	for index, upvalue := range proto.upvalues {
		if upvalue.index < 0 {
			return fmt.Errorf("upvalue %d has negative index %d", index, upvalue.index)
		}
	}
	for pc, ins := range proto.code {
		if err := verifyInstruction(proto, pc, ins); err != nil {
			return fmt.Errorf("instruction %d %s(%d,%d,%d,%d): %w", pc, opcodeName(ins.op), ins.a, ins.b, ins.c, ins.d, err)
		}
	}
	if proto.lines != nil && len(proto.lines) != len(proto.code) {
		return fmt.Errorf("line table length %d does not match code length %d", len(proto.lines), len(proto.code))
	}
	for index, child := range proto.prototypes {
		if err := verifyChildUpvalues(proto, child); err != nil {
			return fmt.Errorf("prototype %d: %w", index, err)
		}
		if err := verifyProtoSeen(child, seen); err != nil {
			return fmt.Errorf("prototype %d: %w", index, err)
		}
	}
	return nil
}

func protoSupportsDirectFrame(proto *Proto) bool {
	return proto != nil && proto.directFrameDispatch
}

func protoDirectFrameRejection(proto *Proto) (directFrameRejection, bool) {
	if proto == nil {
		return directFrameRejection{pc: -1, reason: "nil prototype"}, true
	}
	for pc := 0; pc < len(proto.code); pc++ {
		ins := proto.code[pc]
		if !directFrameOpcodeSupported(ins.op) {
			return directFrameRejection{
				pc:     pc,
				op:     ins.op,
				reason: directFrameOpcodeUnsupportedReason(ins.op),
			}, true
		}
	}
	return directFrameRejection{}, false
}

func directFrameOpcodeSupported(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.directFrame
}

func directFrameOpcodeUnsupportedReason(op opcode) string {
	meta, ok := opcodeMetadata(op)
	if !ok {
		return "unknown opcode"
	}
	return meta.directFrameUnsupportedReason
}

func equalIntSlices(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalNumericForLoopDescs(left []numericForLoopDesc, right []numericForLoopDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalIntrinsicOpDescs(left []intrinsicOpDesc, right []intrinsicOpDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalConstantKindFactDescs(left []constantKindFactDesc, right []constantKindFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalRegisterKindFactDescs(left []registerKindFactDesc, right []registerKindFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalNumericOperandFactDescs(left []numericOperandFactDesc, right []numericOperandFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalBoolSlices(left []bool, right []bool) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalSlotKindFactDescs(left []slotKindFactDesc, right []slotKindFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func verifyChildUpvalues(parent *Proto, child *Proto) error {
	if child == nil {
		return fmt.Errorf("nil prototype")
	}
	for index, desc := range child.upvalues {
		if desc.local {
			if desc.index < 0 || desc.index >= parent.registers {
				return fmt.Errorf("upvalue %d local register index %d out of range", index, desc.index)
			}
			continue
		}
		if desc.index < 0 || desc.index >= len(parent.upvalues) {
			return fmt.Errorf("upvalue %d parent upvalue index %d out of range", index, desc.index)
		}
	}
	return nil
}

func verifyInstruction(proto *Proto, pc int, ins instruction) error {
	switch ins.op {
	case opLoadConst:
		return verifyRegisterAndConstant(proto, ins.a, ins.b)
	case opLoadGlobal:
		if err := verifyRegisterAndConstant(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
	case opSetGlobal:
		if err := verifyConstant(proto, ins.a); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.a); err != nil {
			return err
		}
		return verifyRegister(proto, ins.b)
	case opMove:
		return verifyRegisters(proto, ins.a, ins.b)
	case opNewTable:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.b < 0 {
			return fmt.Errorf("negative table array capacity %d", ins.b)
		}
		if ins.c < 0 {
			return fmt.Errorf("negative table field capacity %d", ins.c)
		}
		return nil
	case opSetField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		return verifyConstant(proto, ins.b)
	case opGetField:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyConstant(proto, ins.c)
	case opSetStringField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
	case opSetStringFieldIndex:
		if err := verifyRegisters(proto, ins.a, ins.c, ins.d); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
	case opGetStringField:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.c)
	case opGetStringFieldIndex:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.d); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.c)
	case opAddStringField, opSubStringField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.b); err != nil {
			return err
		}
		if ins.d < -1 {
			return fmt.Errorf("invalid string field store-back slot %d", ins.d)
		}
		return nil
	case opSetIndex, opGetIndex, opPrepareIter:
		return verifyRegisters(proto, ins.a, ins.b, ins.c)
	case opArrayNext:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		if ins.d <= 0 {
			return fmt.Errorf("array next result count %d is not positive", ins.d)
		}
		if ins.a+ins.d > proto.registers {
			return fmt.Errorf("array next result range r%d..r%d out of range", ins.a, ins.a+ins.d-1)
		}
		return nil
	case opArrayNextJump2:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		if ins.a+1 >= proto.registers {
			return fmt.Errorf("array next jump2 result range r%d..r%d out of range", ins.a, ins.a+1)
		}
		return verifyJumpTarget(proto, ins.d)
	case opClosure:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.b < 0 || ins.b >= len(proto.prototypes) {
			return fmt.Errorf("prototype index %d out of range", ins.b)
		}
		return nil
	case opGetUpvalue:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		return verifyUpvalue(proto, ins.b)
	case opSetUpvalue:
		if err := verifyUpvalue(proto, ins.a); err != nil {
			return err
		}
		return verifyRegister(proto, ins.b)
	case opVararg:
		if !proto.variadic {
			return fmt.Errorf("vararg in non-variadic prototype")
		}
		if ins.b > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.b)
		}
		return verifyRegister(proto, ins.a)
	case opConcatChain:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.c <= 0 {
			return fmt.Errorf("concat chain operand count %d must be positive", ins.c)
		}
		return verifyRegisterSpan(proto, ins.b, ins.c)
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return verifyRegisters(proto, ins.a, ins.b, ins.c)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyConstant(proto, ins.c)
	case opNumericForCheck:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opNumericForLoop:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfModKNotEqualK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyNumberConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		if err := verifyNumberConstant(proto, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfTableHasMetatable:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfStringFieldNotEqualK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		if err := verifyNumberConstant(proto, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfStringFieldNotGreaterR:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyRegister(proto, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.b); err != nil {
			return err
		}
		if ins.c < -1 {
			return fmt.Errorf("negative string field slot %d", ins.c)
		}
		return verifyJumpTarget(proto, ins.d)
	case opCoroutineResume:
		if ins.b < 0 {
			return fmt.Errorf("negative intrinsic argument count %d", ins.b)
		}
		if ins.b > 0 {
			if err := verifyRegisterSpan(proto, ins.a, ins.b); err != nil {
				return err
			}
		} else if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.d > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.d)
		}
		return verifyRegister(proto, ins.a)
	case opFastCall:
		nativeID := nativeFuncID(ins.b)
		if _, ok := nativeFuncByID(nativeID); !ok {
			return fmt.Errorf("unknown fast call native id %d", ins.b)
		}
		if nativeID == nativeFuncSelect && !proto.variadic {
			return fmt.Errorf("select fast call in non-variadic prototype")
		}
		if ins.c < 0 {
			return fmt.Errorf("negative fast call argument count %d", ins.c)
		}
		if ins.c > 0 {
			if err := verifyRegisterSpan(proto, ins.a, ins.c); err != nil {
				return err
			}
		} else if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.d > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.d)
		}
		return verifyRegister(proto, ins.a)
	case opNeg, opLen:
		return verifyRegisters(proto, ins.a, ins.b)
	case opCall:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if ins.c < 0 {
			prefixCount := -ins.c - 1
			openArgStart := ins.b + 1 + prefixCount
			if openArgStart < 0 || openArgStart >= proto.registers {
				return fmt.Errorf("open call argument register %d out of range", openArgStart)
			}
		} else if ins.b+ins.c >= proto.registers {
			return fmt.Errorf("call argument register range out of range")
		}
		if ins.d > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.d)
		}
		return verifyRegister(proto, ins.a)
	case opCallOne:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if ins.c < 0 {
			return fmt.Errorf("fixed one-result call has open argument count %d", ins.c)
		}
		if ins.b+ins.c >= proto.registers {
			return fmt.Errorf("call argument register range out of range")
		}
		return verifyRegister(proto, ins.a)
	case opCallLocalOne:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		if ins.d < 0 {
			return fmt.Errorf("local one-result call has negative argument count %d", ins.d)
		}
		if ins.d > 0 && ins.c+ins.d > proto.registers {
			return fmt.Errorf("local call argument register range out of range")
		}
		return nil
	case opCallUpvalueOne:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyUpvalue(proto, ins.b); err != nil {
			return err
		}
		if ins.d < 0 {
			return fmt.Errorf("upvalue one-result call has negative argument count %d", ins.d)
		}
		if ins.d > 0 {
			return verifyRegisterSpan(proto, ins.c, ins.d)
		}
		return verifyRegister(proto, ins.c)
	case opCallMethodOne:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.c); err != nil {
			return err
		}
		if ins.d < 0 {
			return fmt.Errorf("method one-result call has negative argument count %d", ins.d)
		}
		return verifyRegisterSpan(proto, ins.a+1, ins.d+1)
	case opJumpIfFalse:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.b)
	case opJump:
		return verifyJumpTarget(proto, ins.b)
	case opReturnOne:
		return verifyRegister(proto, ins.a)
	case opReturn:
		if ins.b == 0 {
			return nil
		}
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.b < 0 {
			prefixCount := -ins.b - 1
			if ins.a+prefixCount >= proto.registers {
				return fmt.Errorf("open return register range out of range")
			}
			return nil
		}
		if ins.b > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.b)
		}
		return nil
	default:
		return fmt.Errorf("unknown opcode %d", ins.op)
	}
}

func verifyRegisterAndConstant(proto *Proto, register int, constant int) error {
	if err := verifyRegister(proto, register); err != nil {
		return err
	}
	if err := verifyConstant(proto, constant); err != nil {
		return err
	}
	return nil
}

func verifyRegisters(proto *Proto, registers ...int) error {
	for _, register := range registers {
		if err := verifyRegister(proto, register); err != nil {
			return err
		}
	}
	return nil
}

func verifyRegister(proto *Proto, register int) error {
	if register < 0 || register >= proto.registers {
		return fmt.Errorf("register index %d out of range", register)
	}
	return nil
}

func verifyRegisterSpan(proto *Proto, start int, count int) error {
	if count < 0 {
		return fmt.Errorf("negative register span %d", count)
	}
	if count == 0 {
		return verifyRegister(proto, start)
	}
	if start < 0 || start+count > proto.registers {
		return fmt.Errorf("register range %d..%d out of range", start, start+count-1)
	}
	return nil
}

func verifyConstant(proto *Proto, constant int) error {
	if constant < 0 || constant >= len(proto.constants) {
		return fmt.Errorf("constant index %d out of range", constant)
	}
	return nil
}

func verifyStringConstant(proto *Proto, constant int) error {
	value := proto.constants[constant]
	if _, ok := value.String(); !ok {
		return fmt.Errorf("constant index %d is %s, want string", constant, value.Kind())
	}
	return nil
}

func verifyNumberConstant(proto *Proto, constant int) error {
	value := proto.constants[constant]
	if _, ok := value.Number(); !ok {
		return fmt.Errorf("constant index %d is %s, want number", constant, value.Kind())
	}
	return nil
}

func verifyUpvalue(proto *Proto, upvalue int) error {
	if upvalue < 0 || upvalue >= len(proto.upvalues) {
		return fmt.Errorf("upvalue index %d out of range", upvalue)
	}
	return nil
}

func verifyJumpTarget(proto *Proto, target int) error {
	if target < 0 || target > len(proto.code) {
		return fmt.Errorf("jump target %d out of range", target)
	}
	return nil
}

func disassembleProto(proto *Proto) []string {
	if proto == nil {
		return nil
	}

	lines := make([]string, len(proto.code))
	for pc, ins := range proto.code {
		lines[pc] = fmt.Sprintf("%04d %s", pc, disassembleInstruction(proto, ins))
	}
	return lines
}

func disassembleProtoFacts(proto *Proto) []string {
	if proto == nil {
		return nil
	}

	lines := []string{
		fmt.Sprintf("direct_frame_dispatch %t", proto.directFrameDispatch),
		disassembleCapturedLocals(proto.capturedLocals),
		disassembleEntryNilRegisters(proto.entryNilRegisters),
	}
	if rejection, ok := protoDirectFrameRejection(proto); ok {
		if rejection.pc >= 0 && rejection.pc < len(proto.code) {
			lines = append(lines, fmt.Sprintf(
				"direct_frame_rejection pc%d %s: %s",
				rejection.pc,
				disassembleInstruction(proto, proto.code[rejection.pc]),
				rejection.reason,
			))
		} else {
			lines = append(lines, fmt.Sprintf("direct_frame_rejection %s", rejection.reason))
		}
	}
	for index, ok := range proto.constantKeyOK {
		if ok {
			lines = append(lines, fmt.Sprintf("constant_key k%d %s", index, disassembleTableKey(proto.constantKeys[index])))
		}
	}
	for index, ok := range proto.constantNumberOK {
		if ok {
			lines = append(lines, fmt.Sprintf("constant_number k%d %g", index, proto.constantNumbers[index]))
		}
	}
	for _, loop := range proto.numericForLoops {
		lines = append(lines, fmt.Sprintf(
			"numeric_for pc%d r%d limit r%d step r%d exit %d increment %d",
			loop.checkPC,
			loop.loop,
			loop.limit,
			loop.step,
			loop.exitPC,
			loop.incrementPC,
		))
	}
	for _, intrinsic := range proto.intrinsicOps {
		line := fmt.Sprintf(
			"intrinsic pc%d %s r%d args %d results %d",
			intrinsic.pc,
			opcodeName(intrinsic.op),
			intrinsic.base,
			intrinsic.args,
			intrinsic.results,
		)
		if intrinsic.globalName != "" {
			line += " global " + intrinsic.globalName
		}
		if intrinsic.field != "" {
			line += " field " + intrinsic.field
		}
		if nativeName, ok := baseNativeFuncName(intrinsic.nativeID); ok {
			line += " native " + nativeName
		}
		lines = append(lines, line)
	}
	for _, fact := range proto.constantKindFacts {
		lines = append(lines, fmt.Sprintf(
			"constant_kind k%d %s",
			fact.constant,
			fact.kind.String(),
		))
	}
	for _, fact := range proto.registerKindFacts {
		line := fmt.Sprintf(
			"register_kind pc%d r%d %s source %s",
			fact.pc,
			fact.register,
			fact.kind.String(),
			fact.source,
		)
		if fact.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	for _, fact := range proto.numericOperandFacts {
		right := fmt.Sprintf("right r%d", fact.right)
		if fact.rightConstant {
			right = fmt.Sprintf("right k%d", fact.right)
		}
		lines = append(lines, fmt.Sprintf(
			"numeric_operand pc%d %s left r%d %s",
			fact.pc,
			opcodeName(fact.op),
			fact.left,
			right,
		))
	}
	for _, fact := range proto.slotKindFacts {
		field := fmt.Sprintf("k%d", fact.field)
		if text, ok := stringConstantText(proto, fact.field); ok {
			field = text
		}
		line := fmt.Sprintf(
			"slot_kind pc%d r%d field %s slot %d %s source %s",
			fact.pc,
			fact.table,
			field,
			fact.slot,
			fact.kind.String(),
			fact.source,
		)
		if fact.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	return lines
}

func disassembleRefinementDetail(proto *Proto, base int, field int, second int, value int, other int, slot int) string {
	detail := ""
	if base >= 0 {
		detail += fmt.Sprintf(" base r%d", base)
	}
	if field >= 0 {
		detail += " field " + disassemblePredicateBranchField(proto, field, second)
	}
	if value >= 0 {
		detail += " value " + disassembleConstant(proto, value)
	}
	if other >= 0 {
		detail += fmt.Sprintf(" other r%d", other)
	}
	if slot >= 0 {
		detail += fmt.Sprintf(" slot %d", slot)
	}
	return detail
}

func disassemblePredicateBranchField(proto *Proto, field int, second int) string {
	text := fmt.Sprintf("k%d", field)
	if value, ok := stringConstantText(proto, field); ok {
		text = value
	}
	if second >= 0 {
		if value, ok := stringConstantText(proto, second); ok {
			text += "." + value
		} else {
			text += fmt.Sprintf(".k%d", second)
		}
	}
	return text
}

func nativeFuncName(nativeID nativeFuncID) string {
	if name, ok := baseNativeFuncName(nativeID); ok {
		return name
	}
	return "UNKNOWN"
}

func opcodeName(op opcode) string {
	switch op {
	case opNoop:
		return "NOOP"
	case opLoadConst:
		return "LOAD_CONST"
	case opLoadGlobal:
		return "LOAD_GLOBAL"
	case opSetGlobal:
		return "SET_GLOBAL"
	case opMove:
		return "MOVE"
	case opNewTable:
		return "NEW_TABLE"
	case opSetField:
		return "SET_FIELD"
	case opGetField:
		return "GET_FIELD"
	case opSetStringField:
		return "SET_STRING_FIELD"
	case opSetStringFieldIndex:
		return "SET_STRING_FIELD_INDEX"
	case opGetStringField:
		return "GET_STRING_FIELD"
	case opGetStringFieldIndex:
		return "GET_STRING_FIELD_INDEX"
	case opAddStringField:
		return "ADD_STRING_FIELD"
	case opSubStringField:
		return "SUB_STRING_FIELD"
	case opSetIndex:
		return "SET_INDEX"
	case opGetIndex:
		return "GET_INDEX"
	case opClosure:
		return "CLOSURE"
	case opGetUpvalue:
		return "GET_UPVALUE"
	case opSetUpvalue:
		return "SET_UPVALUE"
	case opVararg:
		return "VARARG"
	case opPrepareIter:
		return "PREPARE_ITER"
	case opArrayNext:
		return "ARRAY_NEXT"
	case opArrayNextJump2:
		return "ARRAY_NEXT_JUMP2"
	case opAdd:
		return "ADD"
	case opSub:
		return "SUB"
	case opMul:
		return "MUL"
	case opDiv:
		return "DIV"
	case opMod:
		return "MOD"
	case opIDiv:
		return "IDIV"
	case opPow:
		return "POW"
	case opNeg:
		return "NEG"
	case opLen:
		return "LEN"
	case opConcat:
		return "CONCAT"
	case opConcatChain:
		return "CONCAT_CHAIN"
	case opAddK:
		return "ADD_K"
	case opSubK:
		return "SUB_K"
	case opMulK:
		return "MUL_K"
	case opDivK:
		return "DIV_K"
	case opModK:
		return "MOD_K"
	case opIDivK:
		return "IDIV_K"
	case opEqual:
		return "EQUAL"
	case opNotEqual:
		return "NOT_EQUAL"
	case opLess:
		return "LESS"
	case opLessEqual:
		return "LESS_EQUAL"
	case opGreater:
		return "GREATER"
	case opGreaterEqual:
		return "GREATER_EQUAL"
	case opNumericForCheck:
		return "NUMERIC_FOR_CHECK"
	case opNumericForLoop:
		return "NUMERIC_FOR_LOOP"
	case opJumpIfNotEqualK:
		return "JUMP_IF_NOT_EQUAL_K"
	case opJumpIfNotLessK:
		return "JUMP_IF_NOT_LESS_K"
	case opJumpIfNotGreaterK:
		return "JUMP_IF_NOT_GREATER_K"
	case opJumpIfLessK:
		return "JUMP_IF_LESS_K"
	case opJumpIfGreaterK:
		return "JUMP_IF_GREATER_K"
	case opJumpIfNotLess:
		return "JUMP_IF_NOT_LESS"
	case opJumpIfNotGreater:
		return "JUMP_IF_NOT_GREATER"
	case opJumpIfLess:
		return "JUMP_IF_LESS"
	case opJumpIfGreater:
		return "JUMP_IF_GREATER"
	case opJumpIfModKNotEqualK:
		return "JUMP_IF_MOD_K_NOT_EQUAL_K"
	case opJumpIfTableHasMetatable:
		return "JUMP_IF_TABLE_HAS_METATABLE"
	case opJumpIfStringFieldNotEqualK:
		return "JUMP_IF_STRING_FIELD_NOT_EQUAL_K"
		return "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K"
		return "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_FIELD"
		return "JUMP_IF_ROW_STRING_FIELD_EQUAL_FIELD"
	case opJumpIfStringFieldNotGreaterK:
		return "JUMP_IF_STRING_FIELD_NOT_GREATER_K"
	case opJumpIfStringFieldGreaterK:
		return "JUMP_IF_STRING_FIELD_GREATER_K"
		return "JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K"
		return "JUMP_IF_ROW_STRING_FIELD_GREATER_K"
	case opJumpIfStringFieldNotGreaterR:
		return "JUMP_IF_STRING_FIELD_NOT_GREATER_R"
		return "JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_R"
		return "JUMP_IF_ROW_STRING_FIELD_NOT_LESS_FIELD"
	case opJumpIfStringFieldFalse:
		return "JUMP_IF_STRING_FIELD_FALSE"
	case opJumpIfStringFieldNil:
		return "JUMP_IF_STRING_FIELD_NIL"
	case opJumpIfStringFieldTrue:
		return "JUMP_IF_STRING_FIELD_TRUE"
	case opJumpIfStringFieldNotNil:
		return "JUMP_IF_STRING_FIELD_NOT_NIL"
	case opCoroutineResume:
		return "COROUTINE_RESUME"
	case opFastCall:
		return "FAST_CALL"
	case opCall:
		return "CALL"
	case opCallOne:
		return "CALL_ONE"
	case opCallLocalOne:
		return "CALL_LOCAL_ONE"
	case opCallUpvalueOne:
		return "CALL_UPVALUE_ONE"
	case opCallMethodOne:
		return "CALL_METHOD_ONE"
	case opJumpIfFalse:
		return "JUMP_IF_FALSE"
	case opJump:
		return "JUMP"
	case opReturnOne:
		return "RETURN_ONE"
	case opReturn:
		return "RETURN"
	default:
		return fmt.Sprintf("UNKNOWN_%d", op)
	}
}

func disassembleEntryNilRegisters(entryNilRegisters []int) string {
	line := "entry_nil"
	if len(entryNilRegisters) == 0 {
		return line + " none"
	}
	for _, register := range entryNilRegisters {
		line += fmt.Sprintf(" r%d", register)
	}
	return line
}

func disassembleCapturedLocals(capturedLocals []bool) string {
	line := "captured_locals"
	hasCaptured := false
	for index, captured := range capturedLocals {
		if captured {
			line += fmt.Sprintf(" r%d", index)
			hasCaptured = true
		}
	}
	if !hasCaptured {
		return line + " none"
	}
	return line
}

func disassembleTableKey(key tableKey) string {
	switch key.kind {
	case BoolKind:
		return fmt.Sprintf("boolean %t", key.bool)
	case NumberKind:
		return fmt.Sprintf("number %g", key.number)
	case StringKind:
		return fmt.Sprintf("string %q", key.str)
	case TableKind:
		return "table"
	case UserDataKind:
		return "userdata"
	default:
		return key.kind.String()
	}
}

func disassembleInstruction(proto *Proto, ins instruction) string {
	switch ins.op {
	case opNoop:
		return "NOOP"
	case opLoadConst:
		return fmt.Sprintf("LOAD_CONST r%d %s", ins.a, disassembleConstant(proto, ins.b))
	case opLoadGlobal:
		return fmt.Sprintf("LOAD_GLOBAL r%d %s", ins.a, disassembleConstant(proto, ins.b))
	case opSetGlobal:
		return fmt.Sprintf("SET_GLOBAL %s r%d", disassembleConstant(proto, ins.a), ins.b)
	case opMove:
		return fmt.Sprintf("MOVE r%d r%d", ins.a, ins.b)
	case opNewTable:
		return fmt.Sprintf("NEW_TABLE r%d %d %d", ins.a, ins.b, ins.c)
	case opSetField:
		return fmt.Sprintf("SET_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
	case opGetField:
		return fmt.Sprintf("GET_FIELD r%d r%d %s", ins.a, ins.b, disassembleConstant(proto, ins.c))
	case opSetStringField:
		return fmt.Sprintf("SET_STRING_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
	case opSetStringFieldIndex:
		return fmt.Sprintf("SET_STRING_FIELD_INDEX r%d %s r%d r%d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opGetStringField:
		return fmt.Sprintf("GET_STRING_FIELD r%d r%d %s", ins.a, ins.b, disassembleConstant(proto, ins.c))
	case opGetStringFieldIndex:
		return fmt.Sprintf("GET_STRING_FIELD_INDEX r%d r%d %s r%d", ins.a, ins.b, disassembleConstant(proto, ins.c), ins.d)
	case opAddStringField:
		line := fmt.Sprintf("ADD_STRING_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
		if ins.d >= 0 {
			line += fmt.Sprintf(" slot %d", ins.d)
		}
		return line
	case opSubStringField:
		line := fmt.Sprintf("SUB_STRING_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
		if ins.d >= 0 {
			line += fmt.Sprintf(" slot %d", ins.d)
		}
		return line
	case opSetIndex:
		return fmt.Sprintf("SET_INDEX r%d r%d r%d", ins.a, ins.b, ins.c)
	case opGetIndex:
		return fmt.Sprintf("GET_INDEX r%d r%d r%d", ins.a, ins.b, ins.c)
	case opClosure:
		return fmt.Sprintf("CLOSURE r%d p%d", ins.a, ins.b)
	case opGetUpvalue:
		return fmt.Sprintf("GET_UPVALUE r%d u%d", ins.a, ins.b)
	case opSetUpvalue:
		return fmt.Sprintf("SET_UPVALUE u%d r%d", ins.a, ins.b)
	case opVararg:
		return fmt.Sprintf("VARARG r%d %d", ins.a, ins.b)
	case opPrepareIter:
		return fmt.Sprintf("PREPARE_ITER r%d r%d r%d", ins.a, ins.b, ins.c)
	case opArrayNext:
		return fmt.Sprintf("ARRAY_NEXT r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opArrayNextJump2:
		return fmt.Sprintf("ARRAY_NEXT_JUMP2 r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opAdd:
		return disassembleABC("ADD", ins)
	case opSub:
		return disassembleABC("SUB", ins)
	case opMul:
		return disassembleABC("MUL", ins)
	case opDiv:
		return disassembleABC("DIV", ins)
	case opMod:
		return disassembleABC("MOD", ins)
	case opIDiv:
		return disassembleABC("IDIV", ins)
	case opPow:
		return disassembleABC("POW", ins)
	case opNeg:
		return fmt.Sprintf("NEG r%d r%d", ins.a, ins.b)
	case opLen:
		return fmt.Sprintf("LEN r%d r%d", ins.a, ins.b)
	case opConcat:
		return disassembleABC("CONCAT", ins)
	case opConcatChain:
		return fmt.Sprintf("CONCAT_CHAIN r%d r%d %d", ins.a, ins.b, ins.c)
	case opAddK:
		return disassembleABK("ADD_K", proto, ins)
	case opSubK:
		return disassembleABK("SUB_K", proto, ins)
	case opMulK:
		return disassembleABK("MUL_K", proto, ins)
	case opDivK:
		return disassembleABK("DIV_K", proto, ins)
	case opModK:
		return disassembleABK("MOD_K", proto, ins)
	case opIDivK:
		return disassembleABK("IDIV_K", proto, ins)
	case opEqual:
		return disassembleABC("EQUAL", ins)
	case opNotEqual:
		return disassembleABC("NOT_EQUAL", ins)
	case opLess:
		return disassembleABC("LESS", ins)
	case opLessEqual:
		return disassembleABC("LESS_EQUAL", ins)
	case opGreater:
		return disassembleABC("GREATER", ins)
	case opGreaterEqual:
		return disassembleABC("GREATER_EQUAL", ins)
	case opNumericForCheck:
		return fmt.Sprintf("NUMERIC_FOR_CHECK r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opNumericForLoop:
		return fmt.Sprintf("NUMERIC_FOR_LOOP r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfNotEqualK:
		return fmt.Sprintf("JUMP_IF_NOT_EQUAL_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotLessK:
		return fmt.Sprintf("JUMP_IF_NOT_LESS_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotGreaterK:
		return fmt.Sprintf("JUMP_IF_NOT_GREATER_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfLessK:
		return fmt.Sprintf("JUMP_IF_LESS_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfGreaterK:
		return fmt.Sprintf("JUMP_IF_GREATER_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotLess:
		return fmt.Sprintf("JUMP_IF_NOT_LESS r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfNotGreater:
		return fmt.Sprintf("JUMP_IF_NOT_GREATER r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfLess:
		return fmt.Sprintf("JUMP_IF_LESS r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfGreater:
		return fmt.Sprintf("JUMP_IF_GREATER r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfModKNotEqualK:
		return fmt.Sprintf("JUMP_IF_MOD_K_NOT_EQUAL_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfTableHasMetatable:
		return fmt.Sprintf("JUMP_IF_TABLE_HAS_METATABLE r%d %d", ins.a, ins.d)
	case opJumpIfStringFieldNotEqualK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_EQUAL_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfStringFieldNotGreaterK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_GREATER_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfStringFieldGreaterK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_GREATER_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfStringFieldNotGreaterR:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_GREATER_R r%d %s r%d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfStringFieldFalse:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_FALSE r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfStringFieldNil:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NIL r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfStringFieldTrue:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_TRUE r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfStringFieldNotNil:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_NIL r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opCoroutineResume:
		return fmt.Sprintf("COROUTINE_RESUME r%d %d %d", ins.a, ins.b, ins.d)
	case opFastCall:
		return fmt.Sprintf("FAST_CALL r%d %s args %d results %d", ins.a, nativeFuncName(nativeFuncID(ins.b)), ins.c, ins.d)
	case opCall:
		return fmt.Sprintf("CALL r%d r%d %d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallOne:
		return fmt.Sprintf("CALL_ONE r%d r%d %d", ins.a, ins.b, ins.c)
	case opCallLocalOne:
		return fmt.Sprintf("CALL_LOCAL_ONE r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallUpvalueOne:
		return fmt.Sprintf("CALL_UPVALUE_ONE r%d u%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallMethodOne:
		return fmt.Sprintf("CALL_METHOD_ONE r%d r%d %s %d", ins.a, ins.b, disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfFalse:
		return fmt.Sprintf("JUMP_IF_FALSE r%d %d", ins.a, ins.b)
	case opJump:
		return fmt.Sprintf("JUMP %d", ins.b)
	case opReturnOne:
		return fmt.Sprintf("RETURN_ONE r%d", ins.a)
	case opReturn:
		return fmt.Sprintf("RETURN r%d %d", ins.a, ins.b)
	default:
		return fmt.Sprintf("UNKNOWN_%d %d %d %d %d", ins.op, ins.a, ins.b, ins.c, ins.d)
	}
}

func disassembleABC(name string, ins instruction) string {
	return fmt.Sprintf("%s r%d r%d r%d", name, ins.a, ins.b, ins.c)
}

func disassembleABK(name string, proto *Proto, ins instruction) string {
	return fmt.Sprintf("%s r%d r%d %s", name, ins.a, ins.b, disassembleConstant(proto, ins.c))
}

func disassembleConstantString(proto *Proto, index int) string {
	if proto == nil || index < 0 || index >= len(proto.constants) {
		return fmt.Sprintf("k%d", index)
	}
	value := proto.constants[index]
	if value.kind != StringKind {
		return fmt.Sprintf("k%d", index)
	}
	return value.stringText()
}

func disassembleConstant(proto *Proto, index int) string {
	if proto == nil || index < 0 || index >= len(proto.constants) {
		return fmt.Sprintf("k%d(<invalid>)", index)
	}
	value := proto.constants[index]
	switch value.kind {
	case NilKind:
		return fmt.Sprintf("k%d(nil)", index)
	case BoolKind:
		return fmt.Sprintf("k%d(boolean %t)", index, value.bool)
	case NumberKind:
		return fmt.Sprintf("k%d(number %g)", index, value.number)
	case StringKind:
		return fmt.Sprintf("k%d(string %q)", index, value.stringText())
	default:
		return fmt.Sprintf("k%d(%s)", index, value.Kind())
	}
}
