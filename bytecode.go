package ember

import (
	"fmt"
	"sort"
)

type opcode uint8

const (
	opLoadConst opcode = iota
	opLoadGlobal
	opSetGlobal
	opMove
	opNewTable
	opSetField
	opGetField
	opSetStringField
	opSetRowStringField
	opSetStringField2
	opSetStringFieldIndex
	opGetStringField
	opGetRowStringField
	opGetStringField2
	opGetStringFieldIndex
	opAddStringField
	opSubStringField
	opSubAddStringField
	opAddSubStringField2
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
	opAddK
	opSubK
	opMulK
	opDivK
	opModK
	opIDivK
	opAddNumericModK
	opEqual
	opNotEqual
	opLess
	opLessEqual
	opGreater
	opGreaterEqual
	opNumericForCheck
	opJumpIfNotEqualK
	opJumpIfNotLessK
	opJumpIfNotLess
	opJumpIfNotGreater
	opJumpIfModKNotEqualK
	opJumpIfTableHasMetatable
	opJumpIfStringFieldNotEqualK
	opJumpIfRowStringFieldNotEqualK
	opJumpIfRowStringFieldNotEqualField
	opJumpIfRowStringFieldEqualField
	opJumpIfStringFieldNotGreaterK
	opJumpIfStringFieldGreaterK
	opJumpIfRowStringFieldNotGreaterK
	opJumpIfRowStringFieldGreaterK
	opJumpIfStringFieldNotGreaterR
	opJumpIfRowStringFieldNotGreaterR
	opJumpIfRowStringFieldNotLessField
	opJumpIfStringFieldFalse
	opJumpIfStringFieldNil
	opJumpIfStringFieldTrue
	opJumpIfStringFieldNotNil
	opTableInsert
	opTableRemove
	opCoroutineResume
	opMathMin
	opSelectVarargCount
	opCall
	opCallOne
	opCallLocalOne
	opCallUpvalueOne
	opCallUpvalueSelfOne
	opCallUpvalueSelfKOne
	opCallUpvalueSelfAddKOne
	opCallMethodOne
	opCallTableFieldKeyOne
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
		opNewTable,
		opSetField,
		opGetField,
		opSetStringField,
		opSetRowStringField,
		opSetStringField2,
		opSetStringFieldIndex,
		opGetStringField,
		opGetRowStringField,
		opGetStringField2,
		opGetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSubAddStringField,
		opAddSubStringField2,
		opSetIndex,
		opGetIndex,
		opClosure,
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
		opAddNumericModK,
		opNeg,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField,
		opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK,
		opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
		opTableInsert,
		opTableRemove,
		opMathMin,
		opJumpIfFalse,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallTableFieldKeyOne,
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
	for _, op := range []opcode{opSetGlobal} {
		table[op].directFrameUnsupportedReason = "global writes require generic frame environment semantics"
	}
	for _, op := range []opcode{opGetUpvalue, opSetUpvalue, opCallUpvalueOne, opCallUpvalueSelfOne, opCallUpvalueSelfKOne, opCallUpvalueSelfAddKOne} {
		table[op].directFrameUnsupportedReason = "upvalue access requires generic frame closure semantics"
	}
	for _, op := range []opcode{opVararg, opSelectVarargCount} {
		table[op].directFrameUnsupportedReason = "vararg value lists require generic frame semantics"
	}
	for _, op := range []opcode{opPow, opLen, opConcat} {
		table[op].directFrameUnsupportedReason = "operation requires generic frame metamethod semantics"
	}
	for _, op := range []opcode{opCoroutineResume} {
		table[op].directFrameUnsupportedReason = "coroutine resume can yield across generic frame state"
	}
	for _, op := range []opcode{opCallMethodOne} {
		table[op].directFrameUnsupportedReason = "method calls require generic frame method lookup semantics"
	}
	for _, op := range []opcode{opJump} {
		table[op].controlFlow = opcodeControlJump
		table[op].jumpTarget = opcodeJumpTargetB
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
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField,
		opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK,
		opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
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
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallUpvalueSelfOne,
		opCallUpvalueSelfKOne,
		opCallUpvalueSelfAddKOne,
		opCallMethodOne,
		opCallTableFieldKeyOne,
	} {
		table[op].mayCall = true
		table[op].mayYield = true
	}
	for _, op := range []opcode{
		opSetIndex,
		opGetField,
		opGetStringField,
		opGetRowStringField,
		opGetStringField2,
		opGetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSubAddStringField,
		opAddSubStringField2,
		opGetIndex,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField,
		opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK,
		opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
		opTableInsert,
		opTableRemove,
		opCallMethodOne,
		opCallTableFieldKeyOne,
	} {
		table[op].readsTable = true
	}
	for _, op := range []opcode{
		opSetField,
		opSetStringField,
		opSetRowStringField,
		opSetStringField2,
		opSetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSubAddStringField,
		opAddSubStringField2,
		opSetIndex,
		opTableInsert,
		opTableRemove,
	} {
		table[op].writesTable = true
	}
	table[opLoadGlobal].readsGlobal = true
	table[opSetGlobal].writesGlobal = true
	for _, op := range []opcode{
		opNewTable,
		opClosure,
		opVararg,
		opConcat,
		opCoroutineResume,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallUpvalueSelfOne,
		opCallUpvalueSelfKOne,
		opCallUpvalueSelfAddKOne,
		opCallMethodOne,
		opCallTableFieldKeyOne,
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
	setOperands(opLoadConst, register, constant, unused, unused)
	setOperands(opLoadGlobal, register, constant, unused, unused)
	setOperands(opSetGlobal, constant, register, unused, unused)
	setOperands(opMove, register, register, unused, unused)
	setOperands(opNewTable, register, count, count, unused)
	setOperands(opSetField, register, constant, register, unused)
	setOperands(opGetField, register, register, constant, unused)
	setOperands(opSetStringField, register, constant, register, count)
	setOperands(opSetRowStringField, register, constant, register, count)
	setOperands(opSetStringField2, register, constant, constant, register)
	setOperands(opSetStringFieldIndex, register, constant, register, register)
	setOperands(opGetStringField, register, register, constant, unused)
	setOperands(opGetRowStringField, register, register, constant, count)
	setOperands(opGetStringField2, register, register, constant, constant)
	setOperands(opGetStringFieldIndex, register, register, constant, register)
	setOperands(opAddStringField, register, constant, register, unused)
	setOperands(opSubStringField, register, constant, register, unused)
	setOperands(opSubAddStringField, register, count, register, unused)
	setOperands(opAddSubStringField2, register, count, unused, unused)
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
	setOperands(opAddK, register, register, constant, unused)
	setOperands(opSubK, register, register, constant, unused)
	setOperands(opMulK, register, register, constant, unused)
	setOperands(opDivK, register, register, constant, unused)
	setOperands(opModK, register, register, constant, unused)
	setOperands(opIDivK, register, register, constant, unused)
	setOperands(opAddNumericModK, register, register, count, unused)
	setOperands(opEqual, register, register, register, unused)
	setOperands(opNotEqual, register, register, register, unused)
	setOperands(opLess, register, register, register, unused)
	setOperands(opLessEqual, register, register, register, unused)
	setOperands(opGreater, register, register, register, unused)
	setOperands(opGreaterEqual, register, register, register, unused)
	setOperands(opNumericForCheck, register, register, register, jumpTarget)
	setOperands(opJumpIfNotEqualK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotLessK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotLess, register, register, unused, jumpTarget)
	setOperands(opJumpIfNotGreater, register, register, unused, jumpTarget)
	setOperands(opJumpIfModKNotEqualK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfTableHasMetatable, register, unused, unused, jumpTarget)
	setOperands(opJumpIfStringFieldNotEqualK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfRowStringFieldNotEqualK, register, count, unused, jumpTarget)
	setOperands(opJumpIfRowStringFieldNotEqualField, register, count, register, jumpTarget)
	setOperands(opJumpIfRowStringFieldEqualField, register, count, register, jumpTarget)
	setOperands(opJumpIfStringFieldNotGreaterK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfStringFieldGreaterK, register, constant, constant, jumpTarget)
	setOperands(opJumpIfRowStringFieldNotGreaterK, register, count, unused, jumpTarget)
	setOperands(opJumpIfRowStringFieldGreaterK, register, count, unused, jumpTarget)
	setOperands(opJumpIfStringFieldNotGreaterR, register, constant, register, jumpTarget)
	setOperands(opJumpIfRowStringFieldNotGreaterR, register, count, register, jumpTarget)
	setOperands(opJumpIfRowStringFieldNotLessField, register, count, unused, jumpTarget)
	setOperands(opJumpIfStringFieldFalse, register, constant, count, jumpTarget)
	setOperands(opJumpIfStringFieldNil, register, constant, count, jumpTarget)
	setOperands(opJumpIfStringFieldTrue, register, constant, count, jumpTarget)
	setOperands(opJumpIfStringFieldNotNil, register, constant, count, jumpTarget)
	setOperands(opTableInsert, register, count, unused, count)
	setOperands(opTableRemove, register, count, unused, count)
	setOperands(opCoroutineResume, register, count, unused, count)
	setOperands(opMathMin, register, count, unused, count)
	setOperands(opSelectVarargCount, register, unused, unused, count)
	setOperands(opCall, register, register, count, count)
	setOperands(opCallOne, register, register, count, count)
	setOperands(opCallLocalOne, register, register, register, count)
	setOperands(opCallUpvalueOne, register, upvalue, register, count)
	setOperands(opCallUpvalueSelfOne, register, upvalue, register, count)
	setOperands(opCallUpvalueSelfKOne, register, upvalue, register, constant)
	setOperands(opCallUpvalueSelfAddKOne, register, upvalue, register, count)
	setOperands(opCallMethodOne, register, register, constant, count)
	setOperands(opCallTableFieldKeyOne, register, register, constant, count)
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
		if meta.operands == (opcodeOperandShape{}) {
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
}

type bytecodeBuilder struct {
	constants             []Value
	ir                    []bytecodeIRInstruction
	prototypes            []*Proto
	stringField2AddSubOps []stringField2AddSubOp
	rowFieldSubAddOps     []rowFieldSubAddOp
	rowFieldEqualOps      []rowFieldEqualOp
	rowFieldRegisterOps   []rowFieldRegisterOp
	rowFieldPairOps       []rowFieldPairOp
	numericAddModOps      []numericAddModOp
	selfCallAddOps        []selfCallAddOp
	source                sourceRange
	sourceText            string
}

func (b *bytecodeBuilder) addConstant(value Value) int {
	index := len(b.constants)
	b.constants = append(b.constants, value)
	return index
}

func (b *bytecodeBuilder) addPrototype(proto *Proto) int {
	index := len(b.prototypes)
	b.prototypes = append(b.prototypes, proto)
	return index
}

func (b *bytecodeBuilder) addStringField2AddSubOp(op stringField2AddSubOp) int {
	index := len(b.stringField2AddSubOps)
	b.stringField2AddSubOps = append(b.stringField2AddSubOps, op)
	return index
}

func (b *bytecodeBuilder) addRowFieldSubAddOp(op rowFieldSubAddOp) int {
	index := len(b.rowFieldSubAddOps)
	b.rowFieldSubAddOps = append(b.rowFieldSubAddOps, op)
	return index
}

func (b *bytecodeBuilder) addRowFieldEqualOp(op rowFieldEqualOp) int {
	index := len(b.rowFieldEqualOps)
	b.rowFieldEqualOps = append(b.rowFieldEqualOps, op)
	return index
}

func (b *bytecodeBuilder) addRowFieldRegisterOp(op rowFieldRegisterOp) int {
	index := len(b.rowFieldRegisterOps)
	b.rowFieldRegisterOps = append(b.rowFieldRegisterOps, op)
	return index
}

func (b *bytecodeBuilder) addRowFieldPairOp(op rowFieldPairOp) int {
	index := len(b.rowFieldPairOps)
	b.rowFieldPairOps = append(b.rowFieldPairOps, op)
	return index
}

func (b *bytecodeBuilder) addNumericAddModOp(op numericAddModOp) int {
	index := len(b.numericAddModOps)
	b.numericAddModOps = append(b.numericAddModOps, op)
	return index
}

func (b *bytecodeBuilder) addSelfCallAddOp(op selfCallAddOp) int {
	index := len(b.selfCallAddOps)
	b.selfCallAddOps = append(b.selfCallAddOps, op)
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
		constants:        b.constants,
		numericAddModOps: b.numericAddModOps,
	}, options)
}

func (b *bytecodeBuilder) proto(upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	proto := newProtoWithDescriptors(b.constants, b.assembledCode(), b.prototypes, b.stringField2AddSubOps, b.rowFieldSubAddOps, b.rowFieldEqualOps, b.rowFieldRegisterOps, b.rowFieldPairOps, b.numericAddModOps, b.selfCallAddOps, upvalues, registers, params, variadic)
	proto.lines = bytecodeIRLines(b.sourceText, b.ir)
	_ = finalizeProtoExecutionArtifact(proto)
	return proto
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
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opSetRowStringField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opSetStringField2:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.d},
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
	case opGetRowStringField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opGetStringField2:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.d},
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
	case opSubAddStringField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
		}
	case opAddSubStringField2:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
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
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return registerOperands(ins.a, ins.b, ins.c)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
		}
	case opAddNumericModK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandCount, value: ins.c},
		}
	case opNumericForCheck:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfNotEqualK, opJumpIfNotLessK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfNotLess, opJumpIfNotGreater:
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
	case opJumpIfRowStringFieldNotEqualK, opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfRowStringFieldNotEqualField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfRowStringFieldEqualField:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfRowStringFieldNotGreaterR:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfRowStringFieldNotLessField:
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
	case opTableInsert, opTableRemove, opCoroutineResume, opMathMin:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opSelectVarargCount:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
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
	case opCallUpvalueOne, opCallUpvalueSelfOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandUpvalue, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: ins.d},
		}
	case opCallUpvalueSelfKOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandUpvalue, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.d},
		}
	case opCallUpvalueSelfAddKOne:
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
	case opCallTableFieldKeyOne:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandCount, value: tableFieldKeyCallArgCount(ins.d)},
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
	if ins.op == opCallTableFieldKeyOne {
		return tableFieldKeyCallArgCount(ins.d)
	}
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

func assembleBytecodeIR(ir []bytecodeIRInstruction) []instruction {
	code := make([]instruction, len(ir))
	for i, ins := range ir {
		code[i] = instruction{
			op: ins.op,
			a:  ins.operands.a.value,
			b:  ins.operands.b.value,
			c:  ins.operands.c.value,
			d:  ins.operands.d.value,
		}
	}
	return code
}

func disassembleBytecodeIR(constants []Value, ir []bytecodeIRInstruction) []string {
	proto := &Proto{
		constants: constants,
		code:      assembleBytecodeIR(ir),
	}
	return disassembleProto(proto)
}

func disassembleBytecodeIRWithSource(constants []Value, ir []bytecodeIRInstruction) []string {
	lines := disassembleBytecodeIR(constants, ir)
	for i := range lines {
		source := ir[i].source
		lines[i] = fmt.Sprintf("%04d [%d,%d) %s", i, source.start, source.end, lines[i][5:])
	}
	return lines
}

func bytecodeIRLines(source string, ir []bytecodeIRInstruction) []int {
	if source == "" || len(ir) == 0 {
		return nil
	}
	lines := make([]int, len(ir))
	hasLine := false
	for i, ins := range ir {
		line := sourceRangeLine(source, ins.source)
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
	raw := assembleBytecodeIR([]bytecodeIRInstruction{ins})[0]
	return registersMatching(raw, func(register int) bool {
		return instructionReadsRegister(raw, register)
	})
}

func bytecodeIRWrittenRegisters(ins bytecodeIRInstruction) []int {
	raw := assembleBytecodeIR([]bytecodeIRInstruction{ins})[0]
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
	if ins.op == opCallUpvalueOne || ins.op == opCallUpvalueSelfOne {
		for register := ins.c; register < ins.c+ins.d; register++ {
			addNonNegativeRegisterCandidate(candidates, register)
		}
	}
	if ins.op == opCallUpvalueSelfKOne {
		addNonNegativeRegisterCandidate(candidates, ins.c)
	}
	if ins.op == opCallUpvalueSelfAddKOne {
		addNonNegativeRegisterCandidate(candidates, ins.c)
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
	if ins.op == opTableInsert || ins.op == opTableRemove || ins.op == opCoroutineResume || ins.op == opMathMin {
		for register := ins.a; register <= ins.a+ins.b; register++ {
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
	if ins.op == opSetStringField2 {
		addNonNegativeRegisterCandidate(candidates, ins.d)
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
	constants              []Value
	constantKeys           []tableKey
	constantKeyOK          []bool
	constantNumbers        []float64
	constantNumberOK       []bool
	code                   []instruction
	lines                  []int
	prototypes             []*Proto
	stringField2AddSubOps  []stringField2AddSubOp
	rowFieldSubAddOps      []rowFieldSubAddOp
	rowFieldEqualOps       []rowFieldEqualOp
	rowFieldRegisterOps    []rowFieldRegisterOp
	rowFieldPairOps        []rowFieldPairOp
	numericAddModOps       []numericAddModOp
	numericForLoops        []numericForLoopDesc
	intrinsicOps           []intrinsicOpDesc
	constantKindFacts      []constantKindFactDesc
	registerKindFacts      []registerKindFactDesc
	numericOperandFacts    []numericOperandFactDesc
	numericOperandFactPCs  []bool
	slotKindFacts          []slotKindFactDesc
	pathKindFacts          []pathKindFactDesc
	predicateBranches      []predicateBranchDesc
	branchRefinements      []branchRefinementDesc
	finiteTagRefinements   []finiteTagRefinementDesc
	reductionFacts         []reductionFactDesc
	directBlockPlans       []directBlockPlanDesc
	directBlockPlanPCs     []int
	blockPlans             []blockPlanDesc
	blockPlanPCs           []int
	regionExecutionPlans   []regionExecutionPlanDesc
	regionExecutionPlanPCs []int
	verifiedPlans          []verifiedPlanDesc
	verifiedPlanPCs        []int
	verifiedPlanRejections []verifiedPlanRejectionDesc
	pathFacts              []pathFactDesc
	pathFactRejections     []pathFactRejectionDesc
	pathPlans              []pathPlanDesc
	selfCallAddOps         []selfCallAddOp
	upvalues               []upvalueDesc
	registers              int
	params                 int
	variadic               bool
	capturedLocals         []bool
	directRegisters        bool
	directFrameDispatch    bool
	directFrameIndexCache  bool
	directLeafCallOne      bool
	entryNilRegisters      []int
	fastMethodFieldAdd     int
	hasFastMethodFieldAdd  bool
	fastUpvalueAdd         int
	hasFastUpvalueAdd      bool
	fastVariadicWeights    []int
	hasFastVariadicSum     bool
	verifyErr              error
}

type stringField2AddSubOp struct {
	targetFirst  int
	targetSecond int
	addFirst     int
	addSecond    int
	subFirst     int
	subSecond    int
}

type rowFieldSubAddOp struct {
	target     int
	add        int
	targetSlot int
	addSlot    int
}

type rowFieldEqualOp struct {
	field int
	value int
	slot  int
}

type rowFieldRegisterOp struct {
	field int
	slot  int
}

type rowFieldPairOp struct {
	leftField  int
	rightField int
	leftSlot   int
	rightSlot  int
}

type numericAddModOp struct {
	mul  int
	idiv int
	mod  int
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

type pathKindFactDesc struct {
	loopStart int
	loopEnd   int
	base      int
	field     int
	second    int
	dynamic   bool
	kind      ValueKind
	source    string
	guarded   bool
}

type predicateBranchDesc struct {
	pc      int
	target  int
	source  string
	op      string
	base    int
	field   int
	second  int
	value   int
	other   int
	slot    int
	guarded bool
}

type branchRefinementDesc struct {
	pc      int
	edge    string
	target  int
	source  string
	fact    string
	base    int
	field   int
	second  int
	value   int
	other   int
	slot    int
	guarded bool
}

type finiteTagRefinementDesc struct {
	pc      int
	source  string
	base    int
	field   int
	second  int
	value   int
	slot    int
	ordinal int
	count   int
	guarded bool
}

type reductionFactDesc struct {
	pc            int
	kind          string
	accumulator   int
	candidate     int
	predicatePC   int
	mutationPC    int
	mutationCount int
}

type directBlockPlanDesc struct {
	pc            int
	kind          string
	startPC       int
	resumePC      int
	register      int
	candidate     int
	field         int
	slot          int
	mutationPC    int
	mutationCount int
}

type blockPlanKind uint8

const (
	blockPlanKindInvalid blockPlanKind = iota
	blockPlanKindAbsoluteDelta
	blockPlanKindMax
	blockPlanKindPairedRowDiff
	blockPlanKindRowFieldAddStore
	blockPlanKindRowFieldBranchStore
	blockPlanKindDynamicPathAddStore
	blockPlanKindDynamicPathSub
	blockPlanKindDynamicPathSubIDivK
	blockPlanKindRowFieldAddFieldStore
)

type blockPlanDesc struct {
	pc          int
	kind        blockPlanKind
	startPC     int
	resumePC    int
	fallbackPC  int
	directBlock directBlockPlanDesc
	dynamicPath dynamicPathAddStoreBlockDesc
	dynamicSub  dynamicPathSubIDivKBlockDesc
	rowField    rowFieldAddFieldStoreBlockDesc
}

type dynamicPathAddStoreBlockDesc struct {
	base       int
	field      int
	key        int
	delta      int
	deltaBase  int
	deltaField int
	deltaSlot  int
	result     int
	op         opcode
	storePC    int
}

type dynamicPathSubIDivKBlockDesc struct {
	leftBase   int
	rightBase  int
	leftField  int
	rightField int
	key        int
	divisor    int
	result     int
}

type rowFieldAddFieldStoreBlockDesc struct {
	base     int
	field    int
	slot     int
	addField int
	addSlot  int
	constant int
	result   int
	constOp  opcode
	op       opcode
	storePC  int
}

type arrayRowLoopRegionDesc struct {
	iterator         int
	array            int
	index            int
	row              int
	accumulator      int
	prefixExitPC     int
	actionBranch     arrayRowLoopActionBranchDesc
	dynamicMap       arrayRowLoopDynamicMapUpdateDesc
	indexedMapBranch arrayRowLoopIndexedMapBranchDesc
	predicate        arrayRowLoopPredicateDesc
	mutations        []arrayRowLoopFieldMutationDesc
	fields           []arrayRowLoopFieldAddDesc
}

type arrayRowLoopPredicateDesc struct {
	pc      int
	op      opcode
	field   int
	value   int
	slot    int
	skipPC  int
	enabled bool
}

type arrayRowLoopFieldAddDesc struct {
	loadPC       int
	addPC        int
	loadRegister int
	field        int
	slot         int
}

type arrayRowLoopActionBranchDesc struct {
	enabled     bool
	actor       int
	accumulator int
	energyField int
	energySlot  int
	costField   int
	costSlot    int
	resetField  int
	resetSlot   int
	usesField   int
	usesSlot    int
	oneConstant int
}

type arrayRowLoopDynamicMapUpdateDesc struct {
	enabled          bool
	adjustedGain     bool
	base             int
	field            int
	keyRegister      int
	storeKeyRegister int
	keyField         int
	keySlot          int
	deltaRegister    int
	deltaOperand     int
	deltaField       int
	deltaSlot        int
	extraResult      int
	extraRegister    int
	extraOp          opcode
	extraConstant    int
	branchField      int
	branchSlot       int
	multiplyKind     int
	multiplyConstant int
	divideKind       int
	divideConstant   int
	divideAdd        int
	bonusBase        int
	bonusField       int
	bonusSlot        int
	bonusConstant    int
	result           int
	op               opcode
}

type arrayRowLoopIndexedMapBranchDesc struct {
	enabled         bool
	base            int
	accumulator     int
	control         int
	keyRegister     int
	valueRegister   int
	thenDelta       int
	elseDelta       int
	thenMapResult   int
	elseMapResult   int
	finalMapResult  int
	keyField        int
	keySlot         int
	deltaField      int
	deltaSlot       int
	branchField     int
	branchSlot      int
	thenValue       int
	leftMapField    int
	mutableMapField int
	finalMapField   int
	divisor         int
	lowerBound      int
	thenModulo      int
	elseModulo      int
	finalModulo     int
}

type arrayRowLoopFieldMutationKind uint8

const (
	arrayRowLoopFieldMutationKindInvalid arrayRowLoopFieldMutationKind = iota
	arrayRowLoopFieldMutationKindConstStore
	arrayRowLoopFieldMutationKindComputedStore
	arrayRowLoopFieldMutationKindClampLowerBound
)

type arrayRowLoopFieldMutationDesc struct {
	kind           arrayRowLoopFieldMutationKind
	loadPC         int
	storePC        int
	loadRegister   int
	valueRegister  int
	valueConstant  int
	field          int
	slot           int
	constantOp     opcode
	sourceRegister int
	sourceBase     int
	sourceField    int
	sourceSlot     int
	op             opcode
	threshold      int
	clamp          int
}

type verifiedPlanKind uint8

const (
	verifiedPlanKindInvalid verifiedPlanKind = iota
	verifiedPlanKindDirectBlock
)

type verifiedPlanCandidate struct {
	kind        verifiedPlanKind
	directBlock directBlockPlanDesc
}

type verifiedPlanDesc struct {
	pc          int
	kind        verifiedPlanKind
	startPC     int
	resumePC    int
	directBlock directBlockPlanDesc
}

type verifiedPlanRejectionDesc struct {
	pc     int
	reason string
}

type pathFactDesc struct {
	loopStart  int
	loopEnd    int
	birthPC    int
	backedgePC int
	fallbackPC int
	killPC     int
	killKind   string
	base       int
	field      int
	second     int
	dynamic    bool
	hits       int
}

type pathFactRejectionDesc struct {
	loopStart  int
	loopEnd    int
	birthPC    int
	killPC     int
	fallbackPC int
	killKind   string
	reason     string
}

type pathPlanDesc struct {
	pc          int
	access      string
	loopStart   int
	loopEnd     int
	base        int
	field       int
	second      int
	dynamic     bool
	keySource   int
	valueSource int
	fallbackPC  int
}

type pathPlanLoopRange struct {
	start int
	end   int
}

func (loop pathPlanLoopRange) valid() bool {
	return loop.start >= 0 && loop.end >= 0
}

type directFrameRejection struct {
	pc     int
	op     opcode
	reason string
}

type selfCallAddOp struct {
	baseLess  int
	firstSub  int
	secondSub int
}

type executionArtifact struct {
	constantKeys           []tableKey
	constantKeyOK          []bool
	constantNumbers        []float64
	constantNumberOK       []bool
	numericForLoops        []numericForLoopDesc
	intrinsicOps           []intrinsicOpDesc
	constantKindFacts      []constantKindFactDesc
	registerKindFacts      []registerKindFactDesc
	numericOperandFacts    []numericOperandFactDesc
	numericOperandFactPCs  []bool
	slotKindFacts          []slotKindFactDesc
	pathKindFacts          []pathKindFactDesc
	predicateBranches      []predicateBranchDesc
	branchRefinements      []branchRefinementDesc
	finiteTagRefinements   []finiteTagRefinementDesc
	reductionFacts         []reductionFactDesc
	directBlockPlans       []directBlockPlanDesc
	directBlockPlanPCs     []int
	blockPlans             []blockPlanDesc
	blockPlanPCs           []int
	regionExecutionPlans   []regionExecutionPlanDesc
	regionExecutionPlanPCs []int
	verifiedPlans          []verifiedPlanDesc
	verifiedPlanPCs        []int
	verifiedPlanRejections []verifiedPlanRejectionDesc
	pathFacts              []pathFactDesc
	pathFactRejections     []pathFactRejectionDesc
	pathPlans              []pathPlanDesc
	capturedLocals         []bool
	directRegisters        bool
	directFrameDispatch    bool
	directFrameIndexCache  bool
	directLeafCallOne      bool
	entryNilRegisters      []int
	fastMethodFieldAdd     int
	hasFastMethodFieldAdd  bool
	fastUpvalueAdd         int
	hasFastUpvalueAdd      bool
	fastVariadicWeights    []int
	hasFastVariadicSum     bool
}

func newProto(constants []Value, code []instruction, prototypes []*Proto, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	return newProtoWithDescriptors(constants, code, prototypes, nil, nil, nil, nil, nil, nil, nil, upvalues, registers, params, variadic)
}

func newProtoWithDescriptors(constants []Value, code []instruction, prototypes []*Proto, stringField2AddSubOps []stringField2AddSubOp, rowFieldSubAddOps []rowFieldSubAddOp, rowFieldEqualOps []rowFieldEqualOp, rowFieldRegisterOps []rowFieldRegisterOp, rowFieldPairOps []rowFieldPairOp, numericAddModOps []numericAddModOp, selfCallAddOps []selfCallAddOp, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	proto := &Proto{
		constants:             constants,
		code:                  code,
		prototypes:            prototypes,
		stringField2AddSubOps: stringField2AddSubOps,
		rowFieldSubAddOps:     rowFieldSubAddOps,
		rowFieldEqualOps:      rowFieldEqualOps,
		rowFieldRegisterOps:   rowFieldRegisterOps,
		rowFieldPairOps:       rowFieldPairOps,
		numericAddModOps:      numericAddModOps,
		selfCallAddOps:        selfCallAddOps,
		upvalues:              upvalues,
		registers:             registers,
		params:                params,
		variadic:              variadic,
	}
	_ = finalizeProtoExecutionArtifact(proto)
	return proto
}

func finalizeProtoExecutionArtifact(proto *Proto) error {
	if proto == nil {
		return nil
	}
	artifact := buildExecutionArtifact(proto)
	artifact.apply(proto)
	proto.verifyErr = verifyProto(proto)
	return proto.verifyErr
}

func buildExecutionArtifact(proto *Proto) executionArtifact {
	constantKeys, constantKeyOK := protoConstantTableKeys(proto.constants)
	constantNumbers, constantNumberOK := protoConstantNumbers(proto.constants)
	capturedLocals := capturedLocalRegisters(proto)
	directRegisters := len(capturedLocals) == 0
	directFrameDispatch := directRegisters && codeSupportsDirectFrame(proto.code)
	directFrameIndexCache := directFrameDispatch && codeUsesDirectFrameIndexCache(proto.code)
	directLeafCallOne := detectDirectLeafCallOne(proto, directFrameDispatch, directFrameIndexCache, capturedLocals)
	fastMethodFieldAdd, hasFastMethodFieldAdd := detectFastMethodFieldAdd(proto)
	fastUpvalueAdd, hasFastUpvalueAdd := detectFastUpvalueAdd(proto)
	fastVariadicWeights, hasFastVariadicSum := detectFastVariadicWeightedSum(proto)
	pathFacts, pathFactRejections := detectLoopLocalPathFacts(proto)
	pathPlans := detectPathPlans(proto, pathFacts)
	slotKindFacts := detectSlotKindFacts(proto)
	predicateBranches := detectPredicateBranches(proto, pathFacts)
	numericOperandFacts := detectNumericOperandFacts(proto)
	reductionFacts := detectReductionFacts(proto)
	directBlockPlans := detectDirectBlockPlans(proto, reductionFacts)
	blockPlans := detectBlockPlans(proto, directBlockPlans, pathPlans)
	regionExecutionPlans := detectRegionExecutionPlans(proto)
	verifiedPlans, verifiedPlanRejections := detectVerifiedPlans(proto, directBlockPlans)
	return executionArtifact{
		constantKeys:           constantKeys,
		constantKeyOK:          constantKeyOK,
		constantNumbers:        constantNumbers,
		constantNumberOK:       constantNumberOK,
		numericForLoops:        detectNumericForLoops(proto.code),
		intrinsicOps:           detectIntrinsicOps(proto.code),
		constantKindFacts:      detectConstantKindFacts(proto.constants),
		registerKindFacts:      detectRegisterKindFacts(proto),
		numericOperandFacts:    numericOperandFacts,
		numericOperandFactPCs:  numericOperandFactPCs(len(proto.code), numericOperandFacts),
		slotKindFacts:          slotKindFacts,
		pathKindFacts:          detectPathKindFacts(pathFacts),
		predicateBranches:      predicateBranches,
		branchRefinements:      detectBranchRefinements(predicateBranches),
		finiteTagRefinements:   detectFiniteTagRefinements(proto, predicateBranches),
		reductionFacts:         reductionFacts,
		directBlockPlans:       directBlockPlans,
		directBlockPlanPCs:     directBlockPlanPCs(len(proto.code), directBlockPlans),
		blockPlans:             blockPlans,
		blockPlanPCs:           blockPlanPCs(len(proto.code), blockPlans),
		regionExecutionPlans:   regionExecutionPlans,
		regionExecutionPlanPCs: regionExecutionPlanPCs(len(proto.code), regionExecutionPlans),
		verifiedPlans:          verifiedPlans,
		verifiedPlanPCs:        verifiedPlanPCs(len(proto.code), verifiedPlans),
		verifiedPlanRejections: verifiedPlanRejections,
		pathFacts:              pathFacts,
		pathFactRejections:     pathFactRejections,
		pathPlans:              pathPlans,
		capturedLocals:         capturedLocals,
		directRegisters:        directRegisters,
		directFrameDispatch:    directFrameDispatch,
		directFrameIndexCache:  directFrameIndexCache,
		directLeafCallOne:      directLeafCallOne,
		entryNilRegisters:      protoEntryNilRegisters(proto.code, proto.params, proto.registers),
		fastMethodFieldAdd:     fastMethodFieldAdd,
		hasFastMethodFieldAdd:  hasFastMethodFieldAdd,
		fastUpvalueAdd:         fastUpvalueAdd,
		hasFastUpvalueAdd:      hasFastUpvalueAdd,
		fastVariadicWeights:    fastVariadicWeights,
		hasFastVariadicSum:     hasFastVariadicSum,
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
	proto.pathKindFacts = artifact.pathKindFacts
	proto.predicateBranches = artifact.predicateBranches
	proto.branchRefinements = artifact.branchRefinements
	proto.finiteTagRefinements = artifact.finiteTagRefinements
	proto.reductionFacts = artifact.reductionFacts
	proto.directBlockPlans = artifact.directBlockPlans
	proto.directBlockPlanPCs = artifact.directBlockPlanPCs
	proto.blockPlans = artifact.blockPlans
	proto.blockPlanPCs = artifact.blockPlanPCs
	proto.regionExecutionPlans = artifact.regionExecutionPlans
	proto.regionExecutionPlanPCs = artifact.regionExecutionPlanPCs
	proto.verifiedPlans = artifact.verifiedPlans
	proto.verifiedPlanPCs = artifact.verifiedPlanPCs
	proto.verifiedPlanRejections = artifact.verifiedPlanRejections
	proto.pathFacts = artifact.pathFacts
	proto.pathFactRejections = artifact.pathFactRejections
	proto.pathPlans = artifact.pathPlans
	proto.capturedLocals = artifact.capturedLocals
	proto.directRegisters = artifact.directRegisters
	proto.directFrameDispatch = artifact.directFrameDispatch
	proto.directFrameIndexCache = artifact.directFrameIndexCache
	proto.directLeafCallOne = artifact.directLeafCallOne
	proto.entryNilRegisters = artifact.entryNilRegisters
	proto.fastMethodFieldAdd = artifact.fastMethodFieldAdd
	proto.hasFastMethodFieldAdd = artifact.hasFastMethodFieldAdd
	proto.fastUpvalueAdd = artifact.fastUpvalueAdd
	proto.hasFastUpvalueAdd = artifact.hasFastUpvalueAdd
	proto.fastVariadicWeights = artifact.fastVariadicWeights
	proto.hasFastVariadicSum = artifact.hasFastVariadicSum
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

func detectDirectLeafCallOne(proto *Proto, directFrameDispatch bool, directFrameIndexCache bool, capturedLocals []bool) bool {
	if proto == nil || !directFrameDispatch || directFrameIndexCache {
		return false
	}
	if proto.variadic || len(proto.upvalues) != 0 || len(capturedLocals) != 0 {
		return false
	}
	if proto.registers <= 0 {
		return false
	}

	sawOneResultReturn := false
	for _, ins := range proto.code {
		meta, ok := opcodeMetadata(ins.op)
		if !ok || meta.mayCall || meta.mayYield {
			return false
		}
		switch ins.op {
		case opClosure, opGetUpvalue, opSetUpvalue, opVararg, opSelectVarargCount, opCoroutineResume:
			return false
		case opReturnOne:
			sawOneResultReturn = true
		case opReturn:
			if ins.b != 1 {
				return false
			}
			sawOneResultReturn = true
		}
	}
	return sawOneResultReturn
}

func detectFastMethodFieldAdd(proto *Proto) (int, bool) {
	if proto == nil || proto.variadic || proto.params < 2 {
		return 0, false
	}
	start := 0
	addend := 1
	if len(proto.code) == 4 &&
		proto.code[0].op == opMove &&
		proto.code[0].b == 1 {
		start = 1
		addend = proto.code[0].a
	} else if len(proto.code) != 3 {
		return 0, false
	}
	add := proto.code[start]
	get := proto.code[start+1]
	ret := proto.code[start+2]
	if add.op != opAddStringField ||
		add.a != 0 ||
		add.c != addend ||
		get.op != opGetStringField ||
		get.b != 0 ||
		ret.op != opReturnOne ||
		ret.a != get.a {
		return 0, false
	}
	if err := verifyStringConstant(proto, add.b); err != nil {
		return 0, false
	}
	if err := verifyStringConstant(proto, get.c); err != nil {
		return 0, false
	}
	if proto.constants[add.b].str != proto.constants[get.c].str {
		return 0, false
	}
	return add.b, true
}

func detectFastUpvalueAdd(proto *Proto) (int, bool) {
	if proto == nil || proto.variadic || proto.params != 1 || len(proto.upvalues) == 0 {
		return 0, false
	}
	if len(proto.code) == 6 {
		get := proto.code[0]
		move := proto.code[1]
		add := proto.code[2]
		set := proto.code[3]
		getReturn := proto.code[4]
		ret := proto.code[5]
		if get.op == opGetUpvalue &&
			move.op == opMove &&
			move.b == 0 &&
			add.op == opAdd &&
			add.a == get.a &&
			add.b == get.a &&
			add.c == move.a &&
			set.op == opSetUpvalue &&
			set.a == get.b &&
			set.b == add.a &&
			getReturn.op == opGetUpvalue &&
			getReturn.b == get.b &&
			ret.op == opReturnOne &&
			ret.a == getReturn.a {
			return get.b, true
		}
	}
	if len(proto.code) == 5 {
		get := proto.code[0]
		add := proto.code[1]
		set := proto.code[2]
		getReturn := proto.code[3]
		ret := proto.code[4]
		if get.op == opGetUpvalue &&
			add.op == opAdd &&
			add.a == get.a &&
			add.b == get.a &&
			add.c == 0 &&
			set.op == opSetUpvalue &&
			set.a == get.b &&
			set.b == add.a &&
			getReturn.op == opGetUpvalue &&
			getReturn.b == get.b &&
			ret.op == opReturnOne &&
			ret.a == getReturn.a {
			return get.b, true
		}
	}
	return 0, false
}

func detectFastVariadicWeightedSum(proto *Proto) ([]int, bool) {
	if proto == nil ||
		!proto.variadic ||
		proto.params != 0 ||
		len(proto.code) < 6 ||
		proto.code[0].op != opSelectVarargCount ||
		proto.code[0].d != 1 ||
		proto.code[1].op != opVararg ||
		proto.code[1].b <= 0 ||
		proto.code[2].op != opMove ||
		proto.code[2].b != proto.code[0].a {
		return nil, false
	}
	count := proto.code[1].b
	if len(proto.code) != 4+count*3 {
		return nil, false
	}
	varargStart := proto.code[1].a
	accumulator := proto.code[2].a
	weights := make([]int, count)
	pc := 3
	for i := 0; i < count; i++ {
		move := proto.code[pc]
		mul := proto.code[pc+1]
		add := proto.code[pc+2]
		if move.op != opMove ||
			move.b != varargStart+i ||
			mul.op != opMulK ||
			mul.a != move.a ||
			mul.b != move.a ||
			add.op != opAdd ||
			add.a != accumulator ||
			add.b != accumulator ||
			add.c != move.a {
			return nil, false
		}
		if err := verifyNumberConstant(proto, mul.c); err != nil {
			return nil, false
		}
		weights[i] = mul.c
		pc += 3
	}
	ret := proto.code[pc]
	if ret.op != opReturnOne || ret.a != accumulator {
		return nil, false
	}
	return weights, true
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
		case opTableInsert, opTableRemove, opCoroutineResume, opMathMin:
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
		case opSelectVarargCount:
			ops = append(ops, intrinsicOpDesc{
				pc:         pc,
				op:         ins.op,
				base:       ins.a,
				args:       0,
				results:    ins.d,
				globalName: "select",
				nativeID:   nativeFuncSelect,
			})
		}
	}
	return ops
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

func (proto *Proto) numericOperandsProvenAt(pc int, ins instruction) bool {
	return proto != nil &&
		pc >= 0 &&
		pc < len(proto.numericOperandFactPCs) &&
		proto.numericOperandFactPCs[pc] &&
		numericOperandInstructionSupported(ins.op)
}

func numericOperandInstructionSupported(op opcode) bool {
	switch op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv,
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
		opNeg,
		opLess, opLessEqual, opGreater, opGreaterEqual:
		return true
	default:
		return false
	}
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
	case opMathMin:
		if ins.d == 1 {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "guarded_intrinsic", guarded: true}, true
		}
	case opSelectVarargCount:
		if ins.d == 1 {
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
	case opSetRowStringField:
		if ins.d < 0 {
			return slotKindFactDesc{}, false
		}
		if _, ok := stringConstantText(proto, ins.b); !ok {
			return slotKindFactDesc{}, false
		}
		value, ok := registerKindAt(registerKinds, ins.c)
		if !ok || !kindFactSupportedKind(value.kind) {
			return slotKindFactDesc{}, false
		}
		return slotKindFactDesc{
			pc:      pc,
			table:   ins.a,
			field:   ins.b,
			slot:    ins.d,
			kind:    value.kind,
			source:  "row_store",
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

func detectPathKindFacts(pathFacts []pathFactDesc) []pathKindFactDesc {
	var facts []pathKindFactDesc
	for _, fact := range pathFacts {
		if fact.second < 0 && !fact.dynamic {
			continue
		}
		facts = append(facts, pathKindFactDesc{
			loopStart: fact.loopStart,
			loopEnd:   fact.loopEnd,
			base:      fact.base,
			field:     fact.field,
			second:    -1,
			dynamic:   false,
			kind:      TableKind,
			source:    "path_parent",
			guarded:   true,
		})
	}
	return facts
}

func stringConstantText(proto *Proto, constant int) (string, bool) {
	if proto == nil || constant < 0 || constant >= len(proto.constants) {
		return "", false
	}
	value := proto.constants[constant]
	if value.kind != StringKind {
		return "", false
	}
	return value.str, true
}

func detectPredicateBranches(proto *Proto, pathFacts []pathFactDesc) []predicateBranchDesc {
	if proto == nil || len(proto.code) == 0 {
		return nil
	}
	var descs []predicateBranchDesc
	for pc, ins := range proto.code {
		desc, ok := predicateBranchForInstruction(proto, pathFacts, pc, ins)
		if ok {
			descs = append(descs, desc)
		}
	}
	return descs
}

func predicateBranchForInstruction(proto *Proto, pathFacts []pathFactDesc, pc int, ins instruction) (predicateBranchDesc, bool) {
	switch ins.op {
	case opJumpIfFalse:
		if path, ok := predicatePathComparisonSource(proto, pathFacts, pc, ins.a); ok {
			path.target = ins.b
			return path, true
		}
		return predicateBranchDesc{pc: pc, target: ins.b, source: "register", op: "truthy", base: ins.a, field: -1, second: -1, value: -1, other: -1, slot: -1}, true
	case opJumpIfNotEqualK:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "register", op: "equal_const", base: ins.a, field: -1, second: -1, value: ins.b, other: -1, slot: -1}, true
	case opJumpIfNotLessK:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "register", op: "numeric_compare", base: ins.a, field: -1, second: -1, value: ins.b, other: -1, slot: -1}, true
	case opJumpIfNotLess, opJumpIfNotGreater:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "register", op: "numeric_compare", base: ins.a, field: -1, second: -1, value: -1, other: ins.b, slot: -1}, true
	case opJumpIfModKNotEqualK:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "register", op: "numeric_compare", base: ins.a, field: -1, second: -1, value: ins.c, other: ins.b, slot: -1}, true
	case opJumpIfStringFieldNotEqualK:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "field", op: "equal_const", base: ins.a, field: ins.b, second: -1, value: ins.c, other: -1, slot: -1, guarded: true}, true
	case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
		if path, ok := predicatePathFieldSource(proto, pathFacts, pc, ins.a, ins.b); ok {
			path.op = "numeric_compare"
			path.value = ins.c
			path.target = ins.d
			return path, true
		}
		return predicateBranchDesc{pc: pc, target: ins.d, source: "field", op: "numeric_compare", base: ins.a, field: ins.b, second: -1, value: ins.c, other: -1, slot: -1, guarded: true}, true
	case opJumpIfStringFieldNotGreaterR:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "field", op: "numeric_compare", base: ins.a, field: ins.b, second: -1, value: -1, other: ins.c, slot: -1, guarded: true}, true
	case opJumpIfStringFieldFalse:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "row_field", op: "truthy", base: ins.a, field: ins.b, second: -1, value: -1, other: -1, slot: ins.c, guarded: ins.c >= 0}, true
	case opJumpIfStringFieldTrue:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "row_field", op: "falsey", base: ins.a, field: ins.b, second: -1, value: -1, other: -1, slot: ins.c, guarded: ins.c >= 0}, true
	case opJumpIfStringFieldNil:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "row_field", op: "not_nil", base: ins.a, field: ins.b, second: -1, value: -1, other: -1, slot: ins.c, guarded: ins.c >= 0}, true
	case opJumpIfStringFieldNotNil:
		return predicateBranchDesc{pc: pc, target: ins.d, source: "row_field", op: "nil", base: ins.a, field: ins.b, second: -1, value: -1, other: -1, slot: ins.c, guarded: ins.c >= 0}, true
	case opJumpIfRowStringFieldNotEqualK, opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
		desc, ok := rowFieldEqualDesc(proto, ins.b)
		if !ok {
			return predicateBranchDesc{}, false
		}
		op := "equal_const"
		if ins.op == opJumpIfRowStringFieldNotGreaterK || ins.op == opJumpIfRowStringFieldGreaterK {
			op = "numeric_compare"
		}
		return predicateBranchDesc{pc: pc, target: ins.d, source: "row_field", op: op, base: ins.a, field: desc.field, second: -1, value: desc.value, other: -1, slot: desc.slot, guarded: desc.slot >= 0}, true
	case opJumpIfRowStringFieldNotGreaterR:
		desc, ok := rowFieldRegisterDesc(proto, ins.b)
		if !ok {
			return predicateBranchDesc{}, false
		}
		return predicateBranchDesc{pc: pc, target: ins.d, source: "row_field", op: "numeric_compare", base: ins.a, field: desc.field, second: -1, value: -1, other: ins.c, slot: desc.slot, guarded: desc.slot >= 0}, true
	case opJumpIfRowStringFieldNotEqualField, opJumpIfRowStringFieldEqualField, opJumpIfRowStringFieldNotLessField:
		desc, ok := rowFieldPairDesc(proto, ins.b)
		if !ok {
			return predicateBranchDesc{}, false
		}
		op := "equal_field"
		if ins.op == opJumpIfRowStringFieldEqualField {
			op = "not_equal_field"
		}
		if ins.op == opJumpIfRowStringFieldNotLessField {
			op = "numeric_compare"
		}
		return predicateBranchDesc{pc: pc, target: ins.d, source: "row_field_pair", op: op, base: ins.a, field: desc.leftField, second: desc.rightField, value: -1, other: ins.c, slot: desc.leftSlot, guarded: desc.leftSlot >= 0 && desc.rightSlot >= 0}, true
	default:
		return predicateBranchDesc{}, false
	}
}

func predicatePathComparisonSource(proto *Proto, pathFacts []pathFactDesc, pc int, condition int) (predicateBranchDesc, bool) {
	if proto == nil || pc <= 0 || pc > len(proto.code) {
		return predicateBranchDesc{}, false
	}
	compare := proto.code[pc-1]
	if compare.a != condition || !predicateComparisonOpcode(compare.op) {
		return predicateBranchDesc{}, false
	}
	for _, source := range []int{compare.b, compare.c} {
		load, ok := previousPathLoad(proto.code, pc-1, source)
		if !ok {
			continue
		}
		for _, fact := range pathFacts {
			if fact.second < 0 || fact.dynamic {
				continue
			}
			if pc < fact.loopStart || pc > fact.loopEnd {
				continue
			}
			if load.b == fact.base && sameStringConstant(proto, load.c, fact.field) && sameStringConstant(proto, load.d, fact.second) {
				other := compare.c
				if source == compare.c {
					other = compare.b
				}
				return predicateBranchDesc{
					pc:      pc,
					source:  "path_field",
					op:      "numeric_compare",
					base:    fact.base,
					field:   fact.field,
					second:  fact.second,
					value:   -1,
					other:   other,
					slot:    -1,
					guarded: true,
				}, true
			}
		}
	}
	return predicateBranchDesc{}, false
}

func sameStringConstant(proto *Proto, left int, right int) bool {
	leftText, leftOK := stringConstantText(proto, left)
	rightText, rightOK := stringConstantText(proto, right)
	return leftOK && rightOK && leftText == rightText
}

func predicateComparisonOpcode(op opcode) bool {
	switch op {
	case opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return true
	default:
		return false
	}
}

func previousPathLoad(code []instruction, before int, register int) (instruction, bool) {
	for pc := before - 1; pc >= 0 && pc >= before-4; pc-- {
		ins := code[pc]
		if ins.a != register {
			continue
		}
		if ins.op == opGetStringField2 {
			return ins, true
		}
		if instructionWritesRegister(ins, register) {
			return instruction{}, false
		}
	}
	return instruction{}, false
}

func predicatePathFieldSource(proto *Proto, pathFacts []pathFactDesc, pc int, branchBase int, branchField int) (predicateBranchDesc, bool) {
	if proto == nil || pc <= 0 || branchField < 0 {
		return predicateBranchDesc{}, false
	}
	load := proto.code[pc-1]
	if load.op != opGetStringField && load.op != opGetRowStringField {
		return predicateBranchDesc{}, false
	}
	if load.a != branchBase {
		return predicateBranchDesc{}, false
	}
	for _, fact := range pathFacts {
		if fact.second != branchField {
			continue
		}
		if pc < fact.loopStart || pc > fact.loopEnd {
			continue
		}
		if load.b != fact.base || load.c != fact.field {
			continue
		}
		return predicateBranchDesc{
			pc:      pc,
			source:  "path_field",
			base:    fact.base,
			field:   fact.field,
			second:  fact.second,
			other:   -1,
			slot:    -1,
			guarded: true,
		}, true
	}
	return predicateBranchDesc{}, false
}

func rowFieldEqualDesc(proto *Proto, index int) (rowFieldEqualOp, bool) {
	if proto == nil || index < 0 || index >= len(proto.rowFieldEqualOps) {
		return rowFieldEqualOp{}, false
	}
	return proto.rowFieldEqualOps[index], true
}

func rowFieldSubAddDesc(proto *Proto, index int) (rowFieldSubAddOp, bool) {
	if proto == nil || index < 0 || index >= len(proto.rowFieldSubAddOps) {
		return rowFieldSubAddOp{}, false
	}
	return proto.rowFieldSubAddOps[index], true
}

func rowFieldRegisterDesc(proto *Proto, index int) (rowFieldRegisterOp, bool) {
	if proto == nil || index < 0 || index >= len(proto.rowFieldRegisterOps) {
		return rowFieldRegisterOp{}, false
	}
	return proto.rowFieldRegisterOps[index], true
}

func rowFieldPairDesc(proto *Proto, index int) (rowFieldPairOp, bool) {
	if proto == nil || index < 0 || index >= len(proto.rowFieldPairOps) {
		return rowFieldPairOp{}, false
	}
	return proto.rowFieldPairOps[index], true
}

func detectBranchRefinements(branches []predicateBranchDesc) []branchRefinementDesc {
	var refinements []branchRefinementDesc
	for _, branch := range branches {
		fallthroughFact, targetFact, ok := predicateBranchEdgeFacts(branch.op)
		if !ok {
			continue
		}
		refinements = append(refinements,
			branchRefinementFromPredicate(branch, "fallthrough", branch.pc+1, fallthroughFact),
			branchRefinementFromPredicate(branch, "target", branch.target, targetFact),
		)
	}
	return refinements
}

func branchRefinementFromPredicate(branch predicateBranchDesc, edge string, target int, fact string) branchRefinementDesc {
	return branchRefinementDesc{
		pc:      branch.pc,
		edge:    edge,
		target:  target,
		source:  branch.source,
		fact:    fact,
		base:    branch.base,
		field:   branch.field,
		second:  branch.second,
		value:   branch.value,
		other:   branch.other,
		slot:    branch.slot,
		guarded: branch.guarded,
	}
}

func predicateBranchEdgeFacts(op string) (string, string, bool) {
	switch op {
	case "truthy":
		return "truthy", "falsey", true
	case "falsey":
		return "falsey", "truthy", true
	case "nil":
		return "nil", "not_nil", true
	case "not_nil":
		return "not_nil", "nil", true
	case "equal_const":
		return "equal_const", "not_equal_const", true
	case "equal_field":
		return "equal_field", "not_equal_field", true
	case "not_equal_field":
		return "not_equal_field", "equal_field", true
	case "numeric_compare":
		return "numeric_compare", "not_numeric_compare", true
	default:
		return "", "", false
	}
}

type finiteTagRefinementKey struct {
	source string
	base   int
	field  int
	second int
	slot   int
}

func detectFiniteTagRefinements(proto *Proto, branches []predicateBranchDesc) []finiteTagRefinementDesc {
	groups := make(map[finiteTagRefinementKey][]predicateBranchDesc)
	var order []finiteTagRefinementKey
	for _, branch := range branches {
		if branch.op != "equal_const" || branch.value < 0 || !constantHasKind(proto, branch.value, StringKind) {
			continue
		}
		key := finiteTagRefinementKey{
			source: branch.source,
			base:   branch.base,
			field:  branch.field,
			second: branch.second,
			slot:   branch.slot,
		}
		if len(groups[key]) == 0 {
			order = append(order, key)
		}
		groups[key] = append(groups[key], branch)
	}
	var refinements []finiteTagRefinementDesc
	for _, key := range order {
		group := groups[key]
		if len(group) < 2 {
			continue
		}
		for index, branch := range group {
			refinements = append(refinements, finiteTagRefinementDesc{
				pc:      branch.pc,
				source:  branch.source,
				base:    branch.base,
				field:   branch.field,
				second:  branch.second,
				value:   branch.value,
				slot:    branch.slot,
				ordinal: index + 1,
				count:   len(group),
				guarded: branch.guarded,
			})
		}
	}
	return refinements
}

func detectReductionFacts(proto *Proto) []reductionFactDesc {
	if proto == nil || len(proto.code) == 0 {
		return nil
	}
	var facts []reductionFactDesc
	for pc, ins := range proto.code {
		if fact, ok := maxReductionFactForInstruction(proto.code, pc, ins); ok {
			facts = append(facts, fact)
		}
		if fact, ok := pairedRowDiffReductionFactForInstruction(proto, pc, ins); ok {
			facts = append(facts, fact)
		}
		if fact, ok := absoluteDeltaReductionFactForInstruction(proto, pc, ins); ok {
			facts = append(facts, fact)
		}
		if fact, ok := allCompleteReductionFactForInstruction(proto, pc, ins); ok {
			facts = append(facts, fact)
		}
	}
	return facts
}

func detectDirectBlockPlans(proto *Proto, reductions []reductionFactDesc) []directBlockPlanDesc {
	if proto == nil || len(proto.code) == 0 {
		return nil
	}
	var plans []directBlockPlanDesc
	for _, reduction := range reductions {
		switch reduction.kind {
		case "absolute_delta":
			if plan, ok := absoluteDeltaDirectBlockPlan(proto, reduction); ok {
				plans = append(plans, plan)
			}
		case "max":
			if plan, ok := maxDirectBlockPlan(proto, reduction); ok {
				plans = append(plans, plan)
			}
		case "paired_row_diff":
			if plan, ok := pairedRowDiffDirectBlockPlan(proto, reduction); ok {
				plans = append(plans, plan)
			}
		}
	}
	for pc, ins := range proto.code {
		if plan, ok := rowFieldAddStoreDirectBlockPlan(proto, pc, ins); ok {
			plans = append(plans, plan)
		}
		if plan, ok := rowFieldBranchStoreDirectBlockPlan(proto, pc, ins); ok {
			plans = append(plans, plan)
		}
	}
	return plans
}

func absoluteDeltaDirectBlockPlan(proto *Proto, reduction reductionFactDesc) (directBlockPlanDesc, bool) {
	if reduction.pc < 0 || reduction.pc >= len(proto.code) {
		return directBlockPlanDesc{}, false
	}
	ins := proto.code[reduction.pc]
	if ins.op != opJumpIfNotLessK || ins.a != reduction.accumulator || ins.d <= reduction.pc {
		return directBlockPlanDesc{}, false
	}
	return directBlockPlanDesc{
		pc:            reduction.pc,
		kind:          "absolute_delta",
		startPC:       reduction.pc,
		resumePC:      ins.d,
		register:      reduction.accumulator,
		candidate:     reduction.candidate,
		field:         -1,
		slot:          -1,
		mutationPC:    reduction.mutationPC,
		mutationCount: reduction.mutationCount,
	}, true
}

func maxDirectBlockPlan(proto *Proto, reduction reductionFactDesc) (directBlockPlanDesc, bool) {
	if reduction.pc < 0 || reduction.pc >= len(proto.code) {
		return directBlockPlanDesc{}, false
	}
	ins := proto.code[reduction.pc]
	if ins.op != opJumpIfNotGreater || ins.a != reduction.candidate || ins.b != reduction.accumulator || ins.d <= reduction.pc {
		return directBlockPlanDesc{}, false
	}
	return directBlockPlanDesc{
		pc:            reduction.pc,
		kind:          "max",
		startPC:       reduction.pc,
		resumePC:      ins.d,
		register:      reduction.accumulator,
		candidate:     reduction.candidate,
		field:         -1,
		slot:          -1,
		mutationPC:    reduction.mutationPC,
		mutationCount: reduction.mutationCount,
	}, true
}

func pairedRowDiffDirectBlockPlan(proto *Proto, reduction reductionFactDesc) (directBlockPlanDesc, bool) {
	if reduction.pc < 0 || reduction.mutationPC < 0 || reduction.mutationPC >= len(proto.code) {
		return directBlockPlanDesc{}, false
	}
	get := proto.code[reduction.pc]
	diff := proto.code[reduction.mutationPC]
	if get.op != opGetIndex || diff.op != opSub || reduction.mutationPC != reduction.pc+3 {
		return directBlockPlanDesc{}, false
	}
	return directBlockPlanDesc{
		pc:            reduction.pc,
		kind:          "paired_row_diff",
		startPC:       reduction.pc,
		resumePC:      reduction.mutationPC + 1,
		register:      diff.a,
		candidate:     reduction.candidate,
		field:         -1,
		slot:          -1,
		mutationPC:    reduction.mutationPC,
		mutationCount: 3,
	}, true
}

func rowFieldAddStoreDirectBlockPlan(proto *Proto, pc int, ins instruction) (directBlockPlanDesc, bool) {
	if ins.op != opAddStringField || ins.d < 0 || pc < 0 || pc >= len(proto.code) {
		return directBlockPlanDesc{}, false
	}
	if _, ok := stringConstantText(proto, ins.b); !ok {
		return directBlockPlanDesc{}, false
	}
	return directBlockPlanDesc{
		pc:            pc,
		kind:          "row_field_add_store",
		startPC:       pc,
		resumePC:      pc + 1,
		register:      ins.a,
		candidate:     ins.c,
		field:         ins.b,
		slot:          ins.d,
		mutationPC:    pc,
		mutationCount: 1,
	}, true
}

func rowFieldBranchStoreDirectBlockPlan(proto *Proto, pc int, ins instruction) (directBlockPlanDesc, bool) {
	if pc < 0 || pc+2 >= len(proto.code) || ins.d <= pc+2 || ins.d > len(proto.code) {
		return directBlockPlanDesc{}, false
	}
	first := proto.code[pc+1]
	store := proto.code[pc+2]
	field := -1
	slot := -1
	candidate := -1
	switch ins.op {
	case opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
		desc, ok := rowFieldEqualDesc(proto, ins.b)
		if !ok || desc.slot < 0 {
			return directBlockPlanDesc{}, false
		}
		bodyCandidate, ok := rowFieldBranchStoreBodyCandidate(proto, first, store, ins.a, desc.field, desc.slot)
		if !ok {
			return directBlockPlanDesc{}, false
		}
		field = desc.field
		slot = desc.slot
		candidate = bodyCandidate
	case opJumpIfRowStringFieldNotGreaterR:
		desc, ok := rowFieldRegisterDesc(proto, ins.b)
		if !ok || desc.slot < 0 {
			return directBlockPlanDesc{}, false
		}
		if !rowFieldRegisterBranchStoreBodyMatches(proto, first, store, ins.a, desc.field, desc.slot, ins.c) {
			return directBlockPlanDesc{}, false
		}
		field = desc.field
		slot = desc.slot
		candidate = ins.c
	default:
		return directBlockPlanDesc{}, false
	}
	if _, ok := stringConstantText(proto, field); !ok {
		return directBlockPlanDesc{}, false
	}
	mutationCount := 2
	if pc+3 < ins.d {
		jump := proto.code[pc+3]
		if pc+4 != ins.d || jump.op != opJump || jump.b != ins.d {
			return directBlockPlanDesc{}, false
		}
		mutationCount = 3
	}
	return directBlockPlanDesc{
		pc:            pc,
		kind:          "row_field_branch_store",
		startPC:       pc,
		resumePC:      ins.d,
		register:      ins.a,
		candidate:     candidate,
		field:         field,
		slot:          slot,
		mutationPC:    pc + 2,
		mutationCount: mutationCount,
	}, true
}

func rowFieldBranchStoreBodyCandidate(proto *Proto, first instruction, store instruction, table int, field int, slot int) (int, bool) {
	if first.op == opLoadConst && rowFieldBranchStoreMutationMatches(proto, store, table, first.a, field, slot) {
		return first.a, true
	}
	if first.op != opMove || store.op != opSubAddStringField || store.a != table || store.c != first.a {
		return -1, false
	}
	desc, ok := rowFieldSubAddDesc(proto, store.b)
	if !ok || desc.targetSlot != slot || desc.addSlot < 0 || !sameStringConstant(proto, desc.target, field) {
		return -1, false
	}
	return first.b, true
}

func rowFieldRegisterBranchStoreBodyMatches(proto *Proto, first instruction, store instruction, table int, field int, slot int, source int) bool {
	return first.op == opMove &&
		first.b == source &&
		rowFieldBranchStoreMutationMatches(proto, store, table, first.a, field, slot)
}

func rowFieldBranchStoreMutationMatches(proto *Proto, store instruction, table int, source int, field int, slot int) bool {
	if store.a != table || store.c != source || store.d != slot || !sameStringConstant(proto, store.b, field) {
		return false
	}
	switch store.op {
	case opSetRowStringField, opAddStringField, opSubStringField:
		return true
	default:
		return false
	}
}

func directBlockPlanPCs(codeLen int, plans []directBlockPlanDesc) []int {
	if codeLen <= 0 {
		return nil
	}
	pcs := make([]int, codeLen)
	for i := range pcs {
		pcs[i] = -1
	}
	for index, plan := range plans {
		if plan.pc >= 0 && plan.pc < len(pcs) {
			pcs[plan.pc] = index
		}
	}
	return pcs
}

func (proto *Proto) directBlockPlanAt(pc int) (directBlockPlanDesc, bool) {
	if proto == nil || pc < 0 || pc >= len(proto.directBlockPlanPCs) {
		return directBlockPlanDesc{}, false
	}
	index := proto.directBlockPlanPCs[pc]
	if index < 0 || index >= len(proto.directBlockPlans) {
		return directBlockPlanDesc{}, false
	}
	return proto.directBlockPlans[index], true
}

func detectBlockPlans(proto *Proto, directBlocks []directBlockPlanDesc, pathPlans []pathPlanDesc) []blockPlanDesc {
	if proto == nil {
		return nil
	}
	plans := make([]blockPlanDesc, 0, len(directBlocks))
	for _, directBlock := range directBlocks {
		plan, ok := blockPlanFromDirectBlock(directBlock)
		if !ok {
			continue
		}
		plans = append(plans, plan)
	}
	plans = append(plans, detectDynamicPathAddStoreBlockPlans(proto, pathPlans)...)
	plans = append(plans, detectDynamicPathSubBlockPlans(proto, pathPlans)...)
	plans = append(plans, detectDynamicPathSubIDivKBlockPlans(proto, pathPlans)...)
	plans = append(plans, detectRowFieldAddFieldStoreBlockPlans(proto)...)
	return plans
}

func detectDynamicPathAddStoreBlockPlans(proto *Proto, pathPlans []pathPlanDesc) []blockPlanDesc {
	if proto == nil || len(pathPlans) == 0 {
		return nil
	}
	var plans []blockPlanDesc
	code := proto.code
	for pc := 1; pc+4 < len(code); pc++ {
		get := code[pc]
		if get.op != opGetStringFieldIndex {
			continue
		}
		if !pathPlanAllowsDynamicAccess(proto, pathPlans, pc, "read", get.b, get.c) {
			continue
		}
		keyMove := code[pc-1]
		deltaMove := code[pc+1]
		arithmetic := code[pc+2]
		storeKeyMove := code[pc+3]
		store := code[pc+4]
		if store.op != opSetStringFieldIndex ||
			store.a != get.b ||
			!sameStringConstant(proto, store.b, get.c) ||
			store.d != get.a {
			continue
		}
		if !dynamicPathAddStoreKeysMatch(proto, keyMove, get, storeKeyMove, store) {
			continue
		}
		delta, deltaBase, deltaField, deltaSlot, ok := dynamicPathAddStoreDeltaSource(deltaMove, arithmetic)
		if !ok {
			continue
		}
		if arithmetic.op != opAdd && arithmetic.op != opSub {
			continue
		}
		if arithmetic.a != get.a || arithmetic.b != get.a {
			continue
		}
		if !pathPlanAllowsDynamicAccess(proto, pathPlans, pc+4, "write", get.b, get.c) {
			continue
		}
		plans = append(plans, blockPlanDesc{
			pc:         pc,
			kind:       blockPlanKindDynamicPathAddStore,
			startPC:    pc,
			resumePC:   pc + 5,
			fallbackPC: pc,
			dynamicPath: dynamicPathAddStoreBlockDesc{
				base:       get.b,
				field:      get.c,
				key:        get.d,
				delta:      delta,
				deltaBase:  deltaBase,
				deltaField: deltaField,
				deltaSlot:  deltaSlot,
				result:     get.a,
				op:         arithmetic.op,
				storePC:    pc + 4,
			},
		})
	}
	return plans
}

func dynamicPathAddStoreKeysMatch(proto *Proto, keyMove instruction, get instruction, storeKeyMove instruction, store instruction) bool {
	if keyMove.op == opMove && keyMove.a == get.d &&
		storeKeyMove.op == opMove &&
		storeKeyMove.b == keyMove.b &&
		store.c == storeKeyMove.a {
		return true
	}
	if keyMove.op != opGetRowStringField || storeKeyMove.op != opGetRowStringField {
		return false
	}
	return keyMove.a == get.d &&
		store.c == storeKeyMove.a &&
		keyMove.b == storeKeyMove.b &&
		keyMove.d == storeKeyMove.d &&
		sameStringConstant(proto, keyMove.c, storeKeyMove.c)
}

func dynamicPathAddStoreDeltaSource(load instruction, arithmetic instruction) (delta int, base int, field int, slot int, ok bool) {
	if arithmetic.c != load.a {
		return 0, 0, 0, 0, false
	}
	if load.op == opMove {
		return load.b, -1, -1, -1, true
	}
	if load.op == opGetRowStringField {
		return load.a, load.b, load.c, load.d, true
	}
	return 0, 0, 0, 0, false
}

func detectDynamicPathSubBlockPlans(proto *Proto, pathPlans []pathPlanDesc) []blockPlanDesc {
	if proto == nil || len(pathPlans) == 0 {
		return nil
	}
	var plans []blockPlanDesc
	code := proto.code
	for pc := 1; pc+3 < len(code); pc++ {
		leftGet := code[pc]
		if leftGet.op != opGetStringFieldIndex {
			continue
		}
		if !pathPlanAllowsDynamicAccess(proto, pathPlans, pc, "read", leftGet.b, leftGet.c) {
			continue
		}
		keyMove := code[pc-1]
		rightKeyMove := code[pc+1]
		rightGet := code[pc+2]
		subtract := code[pc+3]
		if keyMove.op != opMove ||
			keyMove.a != leftGet.d ||
			rightKeyMove.op != opMove ||
			rightKeyMove.b != keyMove.b ||
			rightGet.op != opGetStringFieldIndex ||
			rightGet.d != rightKeyMove.a ||
			subtract.op != opSub ||
			subtract.a != leftGet.a ||
			subtract.b != leftGet.a ||
			subtract.c != rightGet.a {
			continue
		}
		if !pathPlanAllowsDynamicAccess(proto, pathPlans, pc+2, "read", rightGet.b, rightGet.c) {
			continue
		}
		plans = append(plans, blockPlanDesc{
			pc:         pc,
			kind:       blockPlanKindDynamicPathSub,
			startPC:    pc,
			resumePC:   pc + 4,
			fallbackPC: pc,
			dynamicSub: dynamicPathSubIDivKBlockDesc{
				leftBase:   leftGet.b,
				rightBase:  rightGet.b,
				leftField:  leftGet.c,
				rightField: rightGet.c,
				key:        leftGet.d,
				divisor:    -1,
				result:     leftGet.a,
			},
		})
	}
	return plans
}

func detectDynamicPathSubIDivKBlockPlans(proto *Proto, pathPlans []pathPlanDesc) []blockPlanDesc {
	if proto == nil || len(pathPlans) == 0 {
		return nil
	}
	var plans []blockPlanDesc
	code := proto.code
	for pc := 1; pc+4 < len(code); pc++ {
		leftGet := code[pc]
		if leftGet.op != opGetStringFieldIndex {
			continue
		}
		if !pathPlanAllowsDynamicAccess(proto, pathPlans, pc, "read", leftGet.b, leftGet.c) {
			continue
		}
		keyMove := code[pc-1]
		rightKeyMove := code[pc+1]
		rightGet := code[pc+2]
		divide := code[pc+3]
		subtract := code[pc+4]
		if keyMove.op != opMove ||
			keyMove.a != leftGet.d ||
			rightKeyMove.op != opMove ||
			rightKeyMove.b != keyMove.b ||
			rightGet.op != opGetStringFieldIndex ||
			rightGet.d != rightKeyMove.a ||
			divide.op != opIDivK ||
			divide.a != rightGet.a ||
			divide.b != rightGet.a ||
			subtract.op != opSub ||
			subtract.a != leftGet.a ||
			subtract.b != leftGet.a ||
			subtract.c != divide.a {
			continue
		}
		if !pathPlanAllowsDynamicAccess(proto, pathPlans, pc+2, "read", rightGet.b, rightGet.c) {
			continue
		}
		plans = append(plans, blockPlanDesc{
			pc:         pc,
			kind:       blockPlanKindDynamicPathSubIDivK,
			startPC:    pc,
			resumePC:   pc + 5,
			fallbackPC: pc,
			dynamicSub: dynamicPathSubIDivKBlockDesc{
				leftBase:   leftGet.b,
				rightBase:  rightGet.b,
				leftField:  leftGet.c,
				rightField: rightGet.c,
				key:        leftGet.d,
				divisor:    divide.c,
				result:     leftGet.a,
			},
		})
	}
	return plans
}

func pathPlanAllowsDynamicAccess(proto *Proto, pathPlans []pathPlanDesc, pc int, access string, base int, field int) bool {
	for _, plan := range pathPlans {
		if plan.pc != pc ||
			plan.access != access ||
			!plan.dynamic ||
			plan.loopStart < 0 ||
			plan.base != base {
			continue
		}
		if sameStringConstant(proto, plan.field, field) {
			return true
		}
	}
	return false
}

func detectRowFieldAddFieldStoreBlockPlans(proto *Proto) []blockPlanDesc {
	if proto == nil {
		return nil
	}
	code := proto.code
	var plans []blockPlanDesc
	for pc := 0; pc+4 < len(code); pc++ {
		getTarget := code[pc]
		if getTarget.op != opGetRowStringField || getTarget.d < 0 {
			continue
		}
		constArith := code[pc+1]
		getAdd := code[pc+2]
		arith := code[pc+3]
		store := code[pc+4]
		if constArith.op != opAddK && constArith.op != opSubK {
			continue
		}
		if constArith.a != getTarget.a || constArith.b != getTarget.a || !constantHasKind(proto, constArith.c, NumberKind) {
			continue
		}
		if getAdd.op != opGetRowStringField ||
			getAdd.b != getTarget.b ||
			getAdd.d < 0 {
			continue
		}
		if arith.op != opAdd && arith.op != opSub {
			continue
		}
		if arith.a != getTarget.a || arith.b != getTarget.a || arith.c != getAdd.a {
			continue
		}
		if store.op != opSetRowStringField ||
			store.a != getTarget.b ||
			store.c != getTarget.a ||
			store.d != getTarget.d ||
			!sameStringConstant(proto, store.b, getTarget.c) {
			continue
		}
		plans = append(plans, blockPlanDesc{
			pc:         pc,
			kind:       blockPlanKindRowFieldAddFieldStore,
			startPC:    pc,
			resumePC:   pc + 5,
			fallbackPC: pc,
			rowField: rowFieldAddFieldStoreBlockDesc{
				base:     getTarget.b,
				field:    getTarget.c,
				slot:     getTarget.d,
				addField: getAdd.c,
				addSlot:  getAdd.d,
				constant: constArith.c,
				result:   getTarget.a,
				constOp:  constArith.op,
				op:       arith.op,
				storePC:  pc + 4,
			},
		})
	}
	return plans
}

func blockPlanFromDirectBlock(plan directBlockPlanDesc) (blockPlanDesc, bool) {
	kind, ok := blockPlanKindFromDirectBlock(plan.kind)
	if !ok {
		return blockPlanDesc{}, false
	}
	return blockPlanDesc{
		pc:          plan.pc,
		kind:        kind,
		startPC:     plan.startPC,
		resumePC:    plan.resumePC,
		fallbackPC:  plan.startPC,
		directBlock: plan,
	}, true
}

func blockPlanKindFromDirectBlock(kind string) (blockPlanKind, bool) {
	switch kind {
	case "absolute_delta":
		return blockPlanKindAbsoluteDelta, true
	case "max":
		return blockPlanKindMax, true
	case "paired_row_diff":
		return blockPlanKindPairedRowDiff, true
	case "row_field_add_store":
		return blockPlanKindRowFieldAddStore, true
	case "row_field_branch_store":
		return blockPlanKindRowFieldBranchStore, true
	default:
		return blockPlanKindInvalid, false
	}
}

func blockPlanKindName(kind blockPlanKind) string {
	switch kind {
	case blockPlanKindAbsoluteDelta:
		return "absolute_delta"
	case blockPlanKindMax:
		return "max"
	case blockPlanKindPairedRowDiff:
		return "paired_row_diff"
	case blockPlanKindRowFieldAddStore:
		return "row_field_add_store"
	case blockPlanKindRowFieldBranchStore:
		return "row_field_branch_store"
	case blockPlanKindDynamicPathAddStore:
		return "dynamic_path_add_store"
	case blockPlanKindDynamicPathSub:
		return "dynamic_path_sub"
	case blockPlanKindDynamicPathSubIDivK:
		return "dynamic_path_sub_idiv_k"
	case blockPlanKindRowFieldAddFieldStore:
		return "row_field_add_field_store"
	default:
		return "invalid"
	}
}

func blockPlanPCs(codeLen int, plans []blockPlanDesc) []int {
	if codeLen <= 0 {
		return nil
	}
	pcs := make([]int, codeLen)
	for i := range pcs {
		pcs[i] = -1
	}
	for index, plan := range plans {
		if plan.pc >= 0 && plan.pc < len(pcs) {
			pcs[plan.pc] = index
		}
	}
	return pcs
}

func (proto *Proto) blockPlanAt(pc int) (blockPlanDesc, bool) {
	if proto == nil || pc < 0 || pc >= len(proto.blockPlanPCs) {
		return blockPlanDesc{}, false
	}
	index := proto.blockPlanPCs[pc]
	if index < 0 || index >= len(proto.blockPlans) {
		return blockPlanDesc{}, false
	}
	return proto.blockPlans[index], true
}

func detectVerifiedPlans(proto *Proto, directBlocks []directBlockPlanDesc) ([]verifiedPlanDesc, []verifiedPlanRejectionDesc) {
	if proto == nil || len(proto.code) == 0 {
		return nil, nil
	}
	var plans []verifiedPlanDesc
	var rejections []verifiedPlanRejectionDesc
	for _, block := range directBlocks {
		plan, rejection, ok := verifyRegion(proto, block.pc, verifiedPlanCandidate{
			kind:        verifiedPlanKindDirectBlock,
			directBlock: block,
		})
		if !ok {
			rejections = append(rejections, rejection)
			continue
		}
		plans = append(plans, plan)
	}
	return plans, rejections
}

func verifyRegion(proto *Proto, pc int, candidate verifiedPlanCandidate) (verifiedPlanDesc, verifiedPlanRejectionDesc, bool) {
	if proto == nil {
		return verifiedPlanDesc{}, verifiedPlanRejectionDesc{pc: pc, reason: "nil proto"}, false
	}
	switch candidate.kind {
	case verifiedPlanKindDirectBlock:
		block := candidate.directBlock
		if block.pc != pc {
			return verifiedPlanDesc{}, verifiedPlanRejectionDesc{pc: pc, reason: "candidate pc mismatch"}, false
		}
		if block.kind == "" {
			return verifiedPlanDesc{}, verifiedPlanRejectionDesc{pc: pc, reason: "missing direct block kind"}, false
		}
		if !knownDirectBlockPlanKind(block.kind) {
			return verifiedPlanDesc{}, verifiedPlanRejectionDesc{pc: pc, reason: "unknown direct block kind"}, false
		}
		if block.startPC < 0 || block.startPC >= len(proto.code) || block.resumePC <= block.startPC || block.resumePC > len(proto.code) {
			return verifiedPlanDesc{}, verifiedPlanRejectionDesc{pc: pc, reason: "direct block pc range invalid"}, false
		}
		if rejection, ok := rejectUnsafeVerifiedRegion(proto, block.startPC, block.resumePC); ok {
			return verifiedPlanDesc{}, rejection, false
		}
		return verifiedPlanDesc{
			pc:          pc,
			kind:        verifiedPlanKindDirectBlock,
			startPC:     block.startPC,
			resumePC:    block.resumePC,
			directBlock: block,
		}, verifiedPlanRejectionDesc{}, true
	default:
		return verifiedPlanDesc{}, verifiedPlanRejectionDesc{pc: pc, reason: "unknown verified plan candidate"}, false
	}
}

func knownDirectBlockPlanKind(kind string) bool {
	switch kind {
	case "absolute_delta", "max", "paired_row_diff", "row_field_add_store", "row_field_branch_store":
		return true
	default:
		return false
	}
}

func rejectUnsafeVerifiedRegion(proto *Proto, startPC int, resumePC int) (verifiedPlanRejectionDesc, bool) {
	for pc := startPC; pc < resumePC; pc++ {
		ins := proto.code[pc]
		if opcodeMayCall(ins.op) {
			return verifiedPlanRejectionDesc{pc: pc, reason: fmt.Sprintf("%s has call risk", opcodeName(ins.op))}, true
		}
		if opcodeMayYield(ins.op) {
			return verifiedPlanRejectionDesc{pc: pc, reason: fmt.Sprintf("%s has yield risk", opcodeName(ins.op))}, true
		}
		if opcodeControlFlow(ins.op) == opcodeControlReturn {
			return verifiedPlanRejectionDesc{pc: pc, reason: fmt.Sprintf("%s returns from region", opcodeName(ins.op))}, true
		}
	}
	return verifiedPlanRejectionDesc{}, false
}

func verifiedPlanPCs(codeLen int, plans []verifiedPlanDesc) []int {
	if codeLen <= 0 {
		return nil
	}
	pcs := make([]int, codeLen)
	for i := range pcs {
		pcs[i] = -1
	}
	for index, plan := range plans {
		if plan.pc >= 0 && plan.pc < len(pcs) {
			pcs[plan.pc] = index
		}
	}
	return pcs
}

func (proto *Proto) verifiedPlanAt(pc int) (verifiedPlanDesc, bool) {
	if proto == nil || pc < 0 || pc >= len(proto.verifiedPlanPCs) {
		return verifiedPlanDesc{}, false
	}
	index := proto.verifiedPlanPCs[pc]
	if index < 0 || index >= len(proto.verifiedPlans) {
		return verifiedPlanDesc{}, false
	}
	return proto.verifiedPlans[index], true
}

func detectRegionExecutionPlans(proto *Proto) []regionExecutionPlanDesc {
	if proto == nil || len(proto.code) == 0 {
		return nil
	}
	var plans []regionExecutionPlanDesc
	for pc, ins := range proto.code {
		if ins.op != opArrayNextJump2 {
			continue
		}
		plan, ok := detectArrayRowLoopExecutionPlan(proto, pc, ins)
		if !ok {
			plan, ok = detectArrayRowLoopActionBranchExecutionPlan(proto, pc, ins)
		}
		if !ok {
			plan, ok = detectArrayRowLoopIndexedMapBranchExecutionPlan(proto, pc, ins)
		}
		if !ok {
			plan, ok = detectArrayRowLoopDynamicMapUpdateExecutionPlan(proto, pc, ins)
		}
		if !ok {
			plan, ok = detectArrayRowLoopPrefixExecutionPlan(proto, pc, ins)
		}
		if ok {
			plans = append(plans, plan)
		}
	}
	return plans
}

func detectArrayRowLoopExecutionPlan(proto *Proto, pc int, ins instruction) (regionExecutionPlanDesc, bool) {
	if proto == nil ||
		ins.d <= pc+1 ||
		ins.d > len(proto.code) ||
		!arrayRowLoopHasBackJump(proto.code, pc, ins.d) ||
		arrayRowLoopHasNestedIterator(proto.code, pc+1, ins.d-1) ||
		len(regionCallsOrIntrinsics(proto, pc, ins.d)) != 0 {
		return regionExecutionPlanDesc{}, false
	}
	bodyEnd := ins.d - 1
	loads := make(map[int]arrayRowLoopFieldAddDesc)
	desc := arrayRowLoopRegionDesc{
		iterator:    ins.b,
		array:       ins.c,
		index:       ins.a,
		row:         ins.a + 1,
		accumulator: -1,
	}
	for bodyPC := pc + 1; bodyPC < bodyEnd; bodyPC++ {
		body := proto.code[bodyPC]
		switch body.op {
		case opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldNotNil, opJumpIfStringFieldTrue,
			opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
			if body.d <= bodyPC || body.d > bodyEnd {
				return regionExecutionPlanDesc{}, false
			}
			predicate, ok := arrayRowLoopPredicate(proto, desc.row, bodyPC, body, body.d)
			if !ok || desc.predicate.enabled {
				return regionExecutionPlanDesc{}, false
			}
			desc.predicate = predicate
		case opGetRowStringField:
			if bodyPC+4 < bodyEnd {
				mutation, ok := arrayRowLoopComputedFieldMutation(proto, desc.row, bodyPC, proto.code)
				if ok {
					desc.mutations = append(desc.mutations, mutation)
					bodyPC += 4
					continue
				}
			}
			if bodyPC+3 < bodyEnd {
				mutation, ok := arrayRowLoopClampFieldMutation(proto, desc.row, bodyPC, proto.code)
				if ok {
					desc.mutations = append(desc.mutations, mutation)
					bodyPC += 3
					continue
				}
			}
			if bodyPC+1 < bodyEnd {
				predicate, ok := arrayRowLoopLoadedPredicate(proto, desc.row, bodyPC, body, proto.code[bodyPC+1], bodyEnd)
				if ok {
					if desc.predicate.enabled {
						return regionExecutionPlanDesc{}, false
					}
					desc.predicate = predicate
					bodyPC++
					continue
				}
			}
			if body.b != desc.row || body.c < 0 || body.c >= len(proto.constants) || body.d < 0 {
				return regionExecutionPlanDesc{}, false
			}
			if proto.constants[body.c].kind != StringKind {
				return regionExecutionPlanDesc{}, false
			}
			loads[body.a] = arrayRowLoopFieldAddDesc{
				loadPC:       bodyPC,
				loadRegister: body.a,
				field:        body.c,
				slot:         body.d,
			}
		case opLoadConst:
			if bodyPC+1 >= bodyEnd {
				return regionExecutionPlanDesc{}, false
			}
			mutation, ok := arrayRowLoopFieldMutation(proto, desc.row, bodyPC, body, proto.code[bodyPC+1])
			if !ok {
				return regionExecutionPlanDesc{}, false
			}
			desc.mutations = append(desc.mutations, mutation)
			bodyPC++
		case opAdd:
			field, accumulator, ok := arrayRowLoopAddFieldOperand(loads, body)
			if !ok {
				return regionExecutionPlanDesc{}, false
			}
			if desc.accumulator < 0 {
				desc.accumulator = accumulator
			}
			if desc.accumulator != accumulator || body.a != desc.accumulator {
				return regionExecutionPlanDesc{}, false
			}
			field.addPC = bodyPC
			desc.fields = append(desc.fields, field)
			delete(loads, field.loadRegister)
		case opJump:
			if body.b == bodyPC+1 {
				continue
			}
			if bodyPC != bodyEnd-1 || body.b != bodyEnd {
				return regionExecutionPlanDesc{}, false
			}
		default:
			return regionExecutionPlanDesc{}, false
		}
	}
	if len(desc.fields) == 0 && len(desc.mutations) == 0 {
		return regionExecutionPlanDesc{}, false
	}
	if len(desc.fields) != 0 && desc.accumulator < 0 {
		return regionExecutionPlanDesc{}, false
	}
	return regionExecutionPlanDesc{
		kind:       regionExecutionPlanKindArrayRowLoop,
		entryPC:    pc,
		exitPC:     ins.d,
		fallbackPC: pc,
		arrayLoop:  desc,
	}, true
}

func detectArrayRowLoopDynamicMapUpdateExecutionPlan(proto *Proto, pc int, ins instruction) (regionExecutionPlanDesc, bool) {
	if proto == nil ||
		ins.d <= pc+1 ||
		ins.d > len(proto.code) ||
		!arrayRowLoopHasBackJump(proto.code, pc, ins.d) ||
		arrayRowLoopHasNestedIterator(proto.code, pc+1, ins.d-1) ||
		len(regionCallsOrIntrinsics(proto, pc, ins.d)) != 0 {
		return regionExecutionPlanDesc{}, false
	}
	if plan, ok := detectArrayRowLoopAdjustedDynamicMapUpdateExecutionPlan(proto, pc, ins); ok {
		return plan, true
	}
	bodyEnd := ins.d - 1
	if pc+7 != bodyEnd {
		return regionExecutionPlanDesc{}, false
	}
	row := ins.a + 1
	keyLoad := proto.code[pc+1]
	get := proto.code[pc+2]
	deltaLoad := proto.code[pc+3]
	arithmetic := proto.code[pc+4]
	storeKeyLoad := proto.code[pc+5]
	store := proto.code[pc+6]
	if keyLoad.op != opGetRowStringField ||
		keyLoad.b != row ||
		keyLoad.c < 0 ||
		keyLoad.c >= len(proto.constants) ||
		proto.constants[keyLoad.c].kind != StringKind ||
		keyLoad.d < 0 ||
		get.op != opGetStringFieldIndex ||
		get.d != keyLoad.a ||
		get.c < 0 ||
		get.c >= len(proto.constants) ||
		proto.constants[get.c].kind != StringKind ||
		deltaLoad.op != opGetRowStringField ||
		deltaLoad.b != row ||
		deltaLoad.c < 0 ||
		deltaLoad.c >= len(proto.constants) ||
		proto.constants[deltaLoad.c].kind != StringKind ||
		deltaLoad.d < 0 ||
		(arithmetic.op != opAdd && arithmetic.op != opSub) ||
		arithmetic.a != get.a ||
		arithmetic.b != get.a ||
		arithmetic.c != deltaLoad.a ||
		storeKeyLoad.op != opGetRowStringField ||
		storeKeyLoad.b != row ||
		storeKeyLoad.a != store.c ||
		storeKeyLoad.d != keyLoad.d ||
		!sameStringConstant(proto, storeKeyLoad.c, keyLoad.c) ||
		store.op != opSetStringFieldIndex ||
		store.a != get.b ||
		store.d != get.a ||
		!sameStringConstant(proto, store.b, get.c) {
		return regionExecutionPlanDesc{}, false
	}
	return regionExecutionPlanDesc{
		kind:       regionExecutionPlanKindArrayRowLoop,
		entryPC:    pc,
		exitPC:     ins.d,
		fallbackPC: pc,
		arrayLoop: arrayRowLoopRegionDesc{
			iterator:    ins.b,
			array:       ins.c,
			index:       ins.a,
			row:         row,
			accumulator: -1,
			dynamicMap: arrayRowLoopDynamicMapUpdateDesc{
				enabled:          true,
				base:             get.b,
				field:            get.c,
				keyRegister:      keyLoad.a,
				storeKeyRegister: storeKeyLoad.a,
				keyField:         keyLoad.c,
				keySlot:          keyLoad.d,
				deltaRegister:    deltaLoad.a,
				deltaOperand:     deltaLoad.a,
				deltaField:       deltaLoad.c,
				deltaSlot:        deltaLoad.d,
				result:           get.a,
				op:               arithmetic.op,
			},
		},
	}, true
}

func detectArrayRowLoopAdjustedDynamicMapUpdateExecutionPlan(proto *Proto, pc int, ins instruction) (regionExecutionPlanDesc, bool) {
	bodyEnd := ins.d - 1
	row := ins.a + 1
	amountLoad := proto.code[pc+1]
	extraFirst := proto.code[pc+2]
	gainAddPC := pc + 3
	extraResult := extraFirst.a
	extraRegister := extraFirst.b
	extraOp := extraFirst.op
	extraConstant := extraFirst.c
	if pc+21 == bodyEnd {
		extraSecond := proto.code[pc+3]
		if extraFirst.op != opMove ||
			extraSecond.op != opModK ||
			extraSecond.a != extraFirst.a ||
			extraSecond.b != extraFirst.a ||
			!arrayRowLoopNumberConstantOK(proto, extraSecond.c) {
			return regionExecutionPlanDesc{}, false
		}
		gainAddPC = pc + 4
		extraResult = extraSecond.a
		extraRegister = extraFirst.b
		extraOp = extraSecond.op
		extraConstant = extraSecond.c
	} else if pc+20 != bodyEnd {
		return regionExecutionPlanDesc{}, false
	}
	gainAdd := proto.code[gainAddPC]
	multiplyBranch := proto.code[gainAddPC+1]
	multiply := proto.code[gainAddPC+2]
	multiplyJump := proto.code[gainAddPC+3]
	divideBranch := proto.code[gainAddPC+4]
	divide := proto.code[gainAddPC+5]
	divideAdd := proto.code[gainAddPC+6]
	divideJump := proto.code[gainAddPC+7]
	bonusBranch := proto.code[gainAddPC+8]
	bonusAdd := proto.code[gainAddPC+9]
	bonusJump := proto.code[gainAddPC+10]
	keyLoad := proto.code[gainAddPC+11]
	get := proto.code[gainAddPC+12]
	deltaMove := proto.code[gainAddPC+13]
	arithmetic := proto.code[gainAddPC+14]
	storeKeyLoad := proto.code[gainAddPC+15]
	store := proto.code[gainAddPC+16]
	backJump := proto.code[bodyEnd]
	multiplyKind, ok := rowFieldEqualDesc(proto, multiplyBranch.b)
	if !ok {
		return regionExecutionPlanDesc{}, false
	}
	divideKind, ok := rowFieldEqualDesc(proto, divideBranch.b)
	if !ok {
		return regionExecutionPlanDesc{}, false
	}
	if amountLoad.op != opGetRowStringField ||
		amountLoad.b != row ||
		amountLoad.c < 0 ||
		amountLoad.c >= len(proto.constants) ||
		proto.constants[amountLoad.c].kind != StringKind ||
		amountLoad.d < 0 ||
		!arrayRowLoopDynamicMapExtraLoadOK(proto, extraFirst) ||
		gainAdd.op != opAdd ||
		gainAdd.a != amountLoad.a ||
		gainAdd.b != amountLoad.a ||
		gainAdd.c != extraResult ||
		multiplyBranch.op != opJumpIfRowStringFieldNotEqualK ||
		multiplyBranch.a != row ||
		multiplyBranch.d != gainAddPC+4 ||
		multiplyKind.slot < 0 ||
		multiplyKind.field < 0 ||
		multiplyKind.field >= len(proto.constants) ||
		proto.constants[multiplyKind.field].kind != StringKind ||
		multiplyKind.value < 0 ||
		multiplyKind.value >= len(proto.constants) ||
		proto.constants[multiplyKind.value].kind != StringKind ||
		multiply.op != opMulK ||
		multiply.a != amountLoad.a ||
		multiply.b != amountLoad.a ||
		!arrayRowLoopNumberConstantOK(proto, multiply.c) ||
		multiplyJump.op != opJump ||
		multiplyJump.b != gainAddPC+8 ||
		divideBranch.op != opJumpIfRowStringFieldNotEqualK ||
		divideBranch.a != row ||
		divideBranch.d != gainAddPC+8 ||
		divideKind.slot != multiplyKind.slot ||
		!sameStringConstant(proto, divideKind.field, multiplyKind.field) ||
		divideKind.value < 0 ||
		divideKind.value >= len(proto.constants) ||
		proto.constants[divideKind.value].kind != StringKind ||
		divide.op != opIDivK ||
		divide.a != amountLoad.a ||
		divide.b != amountLoad.a ||
		!arrayRowLoopNumberConstantOK(proto, divide.c) ||
		divideAdd.op != opAddK ||
		divideAdd.a != amountLoad.a ||
		divideAdd.b != amountLoad.a ||
		!arrayRowLoopNumberConstantOK(proto, divideAdd.c) ||
		divideJump.op != opJump ||
		divideJump.b != gainAddPC+8 ||
		bonusBranch.op != opJumpIfStringFieldFalse ||
		bonusBranch.d != gainAddPC+11 ||
		bonusBranch.b < 0 ||
		bonusBranch.b >= len(proto.constants) ||
		proto.constants[bonusBranch.b].kind != StringKind ||
		bonusAdd.op != opAddK ||
		bonusAdd.a != amountLoad.a ||
		bonusAdd.b != amountLoad.a ||
		!arrayRowLoopNumberConstantOK(proto, bonusAdd.c) ||
		bonusJump.op != opJump ||
		bonusJump.b != gainAddPC+11 ||
		keyLoad.op != opGetRowStringField ||
		keyLoad.b != row ||
		keyLoad.c < 0 ||
		keyLoad.c >= len(proto.constants) ||
		proto.constants[keyLoad.c].kind != StringKind ||
		keyLoad.d < 0 ||
		get.op != opGetStringFieldIndex ||
		get.d != keyLoad.a ||
		get.c < 0 ||
		get.c >= len(proto.constants) ||
		proto.constants[get.c].kind != StringKind ||
		deltaMove.op != opMove ||
		deltaMove.b != amountLoad.a ||
		arithmetic.op != opAdd && arithmetic.op != opSub ||
		arithmetic.a != get.a ||
		arithmetic.b != get.a ||
		arithmetic.c != deltaMove.a ||
		storeKeyLoad.op != opGetRowStringField ||
		storeKeyLoad.b != row ||
		storeKeyLoad.a != store.c ||
		storeKeyLoad.d != keyLoad.d ||
		!sameStringConstant(proto, storeKeyLoad.c, keyLoad.c) ||
		store.op != opSetStringFieldIndex ||
		store.a != get.b ||
		store.d != get.a ||
		!sameStringConstant(proto, store.b, get.c) ||
		backJump.op != opJump ||
		backJump.b != pc {
		return regionExecutionPlanDesc{}, false
	}
	return regionExecutionPlanDesc{
		kind:       regionExecutionPlanKindArrayRowLoop,
		entryPC:    pc,
		exitPC:     ins.d,
		fallbackPC: pc,
		arrayLoop: arrayRowLoopRegionDesc{
			iterator:    ins.b,
			array:       ins.c,
			index:       ins.a,
			row:         row,
			accumulator: -1,
			dynamicMap: arrayRowLoopDynamicMapUpdateDesc{
				enabled:          true,
				adjustedGain:     true,
				base:             get.b,
				field:            get.c,
				keyRegister:      keyLoad.a,
				storeKeyRegister: storeKeyLoad.a,
				keyField:         keyLoad.c,
				keySlot:          keyLoad.d,
				deltaRegister:    amountLoad.a,
				deltaOperand:     deltaMove.a,
				deltaField:       amountLoad.c,
				deltaSlot:        amountLoad.d,
				extraResult:      extraResult,
				extraRegister:    extraRegister,
				extraOp:          extraOp,
				extraConstant:    extraConstant,
				branchField:      multiplyKind.field,
				branchSlot:       multiplyKind.slot,
				multiplyKind:     multiplyKind.value,
				multiplyConstant: multiply.c,
				divideKind:       divideKind.value,
				divideConstant:   divide.c,
				divideAdd:        divideAdd.c,
				bonusBase:        bonusBranch.a,
				bonusField:       bonusBranch.b,
				bonusSlot:        bonusBranch.c,
				bonusConstant:    bonusAdd.c,
				result:           get.a,
				op:               arithmetic.op,
			},
		},
	}, true
}

func arrayRowLoopDynamicMapExtraLoadOK(proto *Proto, ins instruction) bool {
	if ins.op == opMove {
		return true
	}
	return ins.op == opModK && arrayRowLoopNumberConstantOK(proto, ins.c)
}

func detectArrayRowLoopIndexedMapBranchExecutionPlan(proto *Proto, pc int, ins instruction) (regionExecutionPlanDesc, bool) {
	if proto == nil ||
		ins.d <= pc+1 ||
		ins.d > len(proto.code) ||
		!arrayRowLoopHasBackJump(proto.code, pc, ins.d) ||
		arrayRowLoopHasNestedIterator(proto.code, pc+1, ins.d-1) {
		return regionExecutionPlanDesc{}, false
	}
	bodyEnd := ins.d - 1
	if pc+54 != bodyEnd {
		return regionExecutionPlanDesc{}, false
	}
	row := ins.a + 1
	code := proto.code
	keyLoad := code[pc+1]
	leftKeyMove := code[pc+2]
	leftMapGet := code[pc+3]
	mutableInputKeyMove := code[pc+4]
	mutableInputGet := code[pc+5]
	mutableDivide := code[pc+6]
	adjustmentSub := code[pc+7]
	finalKeyMove := code[pc+8]
	finalMapGet := code[pc+9]
	adjustmentMove := code[pc+10]
	valueAdd := code[pc+11]
	lowerBoundBranch := code[pc+12]
	lowerBoundLoad := code[pc+13]
	lowerBoundJump := code[pc+14]
	branchGuard := code[pc+15]
	thenDelta := code[pc+16]
	thenControlMove := code[pc+17]
	thenControlMod := code[pc+18]
	thenDeltaAdd := code[pc+19]
	thenLimitKeyMove := code[pc+20]
	thenLimitGet := code[pc+21]
	thenDeltaClamp := code[pc+22]
	thenMutableKeyMove := code[pc+23]
	thenMutableGet := code[pc+24]
	thenDeltaMove := code[pc+25]
	thenMutableSub := code[pc+26]
	thenStoreKeyMove := code[pc+27]
	thenMutableStore := code[pc+28]
	thenAccumulatorDeltaMove := code[pc+29]
	thenAccumulatorValueMove := code[pc+30]
	thenAccumulatorProduct := code[pc+31]
	thenAccumulatorUpdate := code[pc+32]
	thenJump := code[pc+33]
	elseDelta := code[pc+34]
	elseControlMove := code[pc+35]
	elseControlMod := code[pc+36]
	elseDeltaAdd := code[pc+37]
	elseMutableKeyMove := code[pc+38]
	elseMutableGet := code[pc+39]
	elseDeltaMove := code[pc+40]
	elseMutableAdd := code[pc+41]
	elseStoreKeyMove := code[pc+42]
	elseMutableStore := code[pc+43]
	elseAccumulatorDeltaMove := code[pc+44]
	elseAccumulatorValueMove := code[pc+45]
	elseAccumulatorProduct := code[pc+46]
	elseAccumulatorUpdate := code[pc+47]
	finalValueMove := code[pc+48]
	finalControlMove := code[pc+49]
	finalControlMod := code[pc+50]
	finalValueAdd := code[pc+51]
	finalStoreKeyMove := code[pc+52]
	finalStore := code[pc+53]
	backJump := code[bodyEnd]
	branch, ok := rowFieldEqualDesc(proto, branchGuard.b)
	if !ok {
		return regionExecutionPlanDesc{}, false
	}
	if keyLoad.op != opGetRowStringField ||
		keyLoad.b != row ||
		keyLoad.c < 0 ||
		keyLoad.c >= len(proto.constants) ||
		proto.constants[keyLoad.c].kind != StringKind ||
		keyLoad.d < 0 ||
		leftKeyMove.op != opMove ||
		leftKeyMove.b != keyLoad.a ||
		leftMapGet.op != opGetStringFieldIndex ||
		leftMapGet.d != leftKeyMove.a ||
		mutableInputKeyMove.op != opMove ||
		mutableInputKeyMove.b != keyLoad.a ||
		mutableInputGet.op != opGetStringFieldIndex ||
		mutableInputGet.b != leftMapGet.b ||
		mutableInputGet.d != mutableInputKeyMove.a ||
		mutableDivide.op != opIDivK ||
		mutableDivide.a != mutableInputGet.a ||
		mutableDivide.b != mutableInputGet.a ||
		!arrayRowLoopNumberConstantOK(proto, mutableDivide.c) ||
		proto.constants[mutableDivide.c].number == 0 ||
		adjustmentSub.op != opSub ||
		adjustmentSub.a != leftMapGet.a ||
		adjustmentSub.b != leftMapGet.a ||
		adjustmentSub.c != mutableDivide.a ||
		finalKeyMove.op != opMove ||
		finalKeyMove.b != keyLoad.a ||
		finalMapGet.op != opGetStringFieldIndex ||
		finalMapGet.b != leftMapGet.b ||
		finalMapGet.d != finalKeyMove.a ||
		adjustmentMove.op != opMove ||
		adjustmentMove.b != adjustmentSub.a ||
		valueAdd.op != opAdd ||
		valueAdd.a != finalMapGet.a ||
		valueAdd.b != finalMapGet.a ||
		valueAdd.c != adjustmentMove.a ||
		lowerBoundBranch.op != opJumpIfNotLessK ||
		lowerBoundBranch.a != finalMapGet.a ||
		lowerBoundBranch.d != pc+15 ||
		!arrayRowLoopNumberConstantOK(proto, lowerBoundBranch.b) ||
		lowerBoundLoad.op != opLoadConst ||
		lowerBoundLoad.a != finalMapGet.a ||
		!arrayRowLoopNumberConstantOK(proto, lowerBoundLoad.b) ||
		proto.constants[lowerBoundLoad.b].number != proto.constants[lowerBoundBranch.b].number ||
		lowerBoundJump.op != opJump ||
		lowerBoundJump.b != pc+15 ||
		branchGuard.op != opJumpIfRowStringFieldNotEqualK ||
		branchGuard.a != row ||
		branchGuard.d != pc+34 ||
		branch.field < 0 ||
		branch.field >= len(proto.constants) ||
		proto.constants[branch.field].kind != StringKind ||
		branch.value < 0 ||
		branch.value >= len(proto.constants) ||
		proto.constants[branch.value].kind != StringKind ||
		branch.slot < 0 {
		return regionExecutionPlanDesc{}, false
	}
	if thenDelta.op != opGetRowStringField ||
		thenDelta.b != row ||
		thenDelta.c < 0 ||
		thenDelta.c >= len(proto.constants) ||
		proto.constants[thenDelta.c].kind != StringKind ||
		thenDelta.d < 0 ||
		thenControlMove.op != opMove ||
		thenControlMod.op != opModK ||
		thenControlMod.a != thenControlMove.a ||
		thenControlMod.b != thenControlMove.a ||
		!arrayRowLoopNumberConstantOK(proto, thenControlMod.c) ||
		proto.constants[thenControlMod.c].number == 0 ||
		thenDeltaAdd.op != opAdd ||
		thenDeltaAdd.a != thenDelta.a ||
		thenDeltaAdd.b != thenDelta.a ||
		thenDeltaAdd.c != thenControlMod.a ||
		thenLimitKeyMove.op != opMove ||
		thenLimitKeyMove.b != keyLoad.a ||
		thenLimitGet.op != opGetStringFieldIndex ||
		thenLimitGet.b != leftMapGet.b ||
		thenLimitGet.d != thenLimitKeyMove.a ||
		thenLimitGet.a != thenDelta.a+1 ||
		thenDeltaClamp.op != opMathMin ||
		thenDeltaClamp.a != thenDelta.a ||
		thenDeltaClamp.b != 2 ||
		thenDeltaClamp.d != 1 ||
		thenMutableKeyMove.op != opMove ||
		thenMutableKeyMove.b != keyLoad.a ||
		thenMutableGet.op != opGetStringFieldIndex ||
		thenMutableGet.b != leftMapGet.b ||
		thenMutableGet.d != thenMutableKeyMove.a ||
		thenDeltaMove.op != opMove ||
		thenDeltaMove.b != thenDelta.a ||
		thenMutableSub.op != opSub ||
		thenMutableSub.a != thenMutableGet.a ||
		thenMutableSub.b != thenMutableGet.a ||
		thenMutableSub.c != thenDeltaMove.a ||
		thenStoreKeyMove.op != opMove ||
		thenStoreKeyMove.b != keyLoad.a ||
		thenMutableStore.op != opSetStringFieldIndex ||
		thenMutableStore.a != leftMapGet.b ||
		thenMutableStore.c != thenStoreKeyMove.a ||
		thenMutableStore.d != thenMutableSub.a ||
		thenAccumulatorDeltaMove.op != opMove ||
		thenAccumulatorDeltaMove.b != thenDelta.a ||
		thenAccumulatorValueMove.op != opMove ||
		thenAccumulatorValueMove.b != finalMapGet.a ||
		thenAccumulatorProduct.op != opMul ||
		thenAccumulatorProduct.a != thenAccumulatorDeltaMove.a ||
		thenAccumulatorProduct.b != thenAccumulatorDeltaMove.a ||
		thenAccumulatorProduct.c != thenAccumulatorValueMove.a ||
		thenAccumulatorUpdate.op != opSub ||
		thenAccumulatorUpdate.a != thenAccumulatorUpdate.b ||
		thenAccumulatorUpdate.c != thenAccumulatorProduct.a ||
		thenJump.op != opJump ||
		thenJump.b != pc+48 {
		return regionExecutionPlanDesc{}, false
	}
	if elseDelta.op != opGetRowStringField ||
		elseDelta.b != row ||
		elseDelta.d != thenDelta.d ||
		!sameStringConstant(proto, elseDelta.c, thenDelta.c) ||
		elseControlMove.op != opMove ||
		elseControlMove.b != thenControlMove.b ||
		elseControlMod.op != opModK ||
		elseControlMod.a != elseControlMove.a ||
		elseControlMod.b != elseControlMove.a ||
		!arrayRowLoopNumberConstantOK(proto, elseControlMod.c) ||
		proto.constants[elseControlMod.c].number == 0 ||
		elseDeltaAdd.op != opAdd ||
		elseDeltaAdd.a != elseDelta.a ||
		elseDeltaAdd.b != elseDelta.a ||
		elseDeltaAdd.c != elseControlMod.a ||
		elseMutableKeyMove.op != opMove ||
		elseMutableKeyMove.b != keyLoad.a ||
		elseMutableGet.op != opGetStringFieldIndex ||
		elseMutableGet.b != leftMapGet.b ||
		elseMutableGet.d != elseMutableKeyMove.a ||
		elseDeltaMove.op != opMove ||
		elseDeltaMove.b != elseDelta.a ||
		elseMutableAdd.op != opAdd ||
		elseMutableAdd.a != elseMutableGet.a ||
		elseMutableAdd.b != elseMutableGet.a ||
		elseMutableAdd.c != elseDeltaMove.a ||
		elseStoreKeyMove.op != opMove ||
		elseStoreKeyMove.b != keyLoad.a ||
		elseMutableStore.op != opSetStringFieldIndex ||
		elseMutableStore.a != leftMapGet.b ||
		elseMutableStore.c != elseStoreKeyMove.a ||
		elseMutableStore.d != elseMutableAdd.a ||
		elseAccumulatorDeltaMove.op != opMove ||
		elseAccumulatorDeltaMove.b != elseDelta.a ||
		elseAccumulatorValueMove.op != opMove ||
		elseAccumulatorValueMove.b != finalMapGet.a ||
		elseAccumulatorProduct.op != opMul ||
		elseAccumulatorProduct.a != elseAccumulatorDeltaMove.a ||
		elseAccumulatorProduct.b != elseAccumulatorDeltaMove.a ||
		elseAccumulatorProduct.c != elseAccumulatorValueMove.a ||
		elseAccumulatorUpdate.op != opAdd ||
		elseAccumulatorUpdate.a != thenAccumulatorUpdate.a ||
		elseAccumulatorUpdate.b != thenAccumulatorUpdate.a ||
		elseAccumulatorUpdate.c != elseAccumulatorProduct.a {
		return regionExecutionPlanDesc{}, false
	}
	if finalValueMove.op != opMove ||
		finalValueMove.b != finalMapGet.a ||
		finalControlMove.op != opMove ||
		finalControlMove.b != thenControlMove.b ||
		finalControlMod.op != opModK ||
		finalControlMod.a != finalControlMove.a ||
		finalControlMod.b != finalControlMove.a ||
		!arrayRowLoopNumberConstantOK(proto, finalControlMod.c) ||
		proto.constants[finalControlMod.c].number == 0 ||
		finalValueAdd.op != opAdd ||
		finalValueAdd.a != finalValueMove.a ||
		finalValueAdd.b != finalValueMove.a ||
		finalValueAdd.c != finalControlMod.a ||
		finalStoreKeyMove.op != opMove ||
		finalStoreKeyMove.b != keyLoad.a ||
		finalStore.op != opSetStringFieldIndex ||
		finalStore.a != leftMapGet.b ||
		finalStore.c != finalStoreKeyMove.a ||
		finalStore.d != finalValueAdd.a ||
		backJump.op != opJump ||
		backJump.b != pc {
		return regionExecutionPlanDesc{}, false
	}
	if leftMapGet.c < 0 ||
		leftMapGet.c >= len(proto.constants) ||
		proto.constants[leftMapGet.c].kind != StringKind ||
		mutableInputGet.c < 0 ||
		mutableInputGet.c >= len(proto.constants) ||
		proto.constants[mutableInputGet.c].kind != StringKind ||
		finalMapGet.c < 0 ||
		finalMapGet.c >= len(proto.constants) ||
		proto.constants[finalMapGet.c].kind != StringKind ||
		!sameStringConstant(proto, thenLimitGet.c, mutableInputGet.c) ||
		!sameStringConstant(proto, thenMutableGet.c, mutableInputGet.c) ||
		!sameStringConstant(proto, thenMutableStore.b, mutableInputGet.c) ||
		!sameStringConstant(proto, elseMutableGet.c, mutableInputGet.c) ||
		!sameStringConstant(proto, elseMutableStore.b, mutableInputGet.c) ||
		!sameStringConstant(proto, finalStore.b, finalMapGet.c) {
		return regionExecutionPlanDesc{}, false
	}
	return regionExecutionPlanDesc{
		kind:       regionExecutionPlanKindArrayRowLoop,
		entryPC:    pc,
		exitPC:     ins.d,
		fallbackPC: pc,
		arrayLoop: arrayRowLoopRegionDesc{
			iterator:    ins.b,
			array:       ins.c,
			index:       ins.a,
			row:         row,
			accumulator: -1,
			indexedMapBranch: arrayRowLoopIndexedMapBranchDesc{
				enabled:         true,
				base:            leftMapGet.b,
				accumulator:     thenAccumulatorUpdate.a,
				control:         thenControlMove.b,
				keyRegister:     keyLoad.a,
				valueRegister:   finalMapGet.a,
				thenDelta:       thenDelta.a,
				elseDelta:       elseDelta.a,
				thenMapResult:   thenMutableGet.a,
				elseMapResult:   elseMutableGet.a,
				finalMapResult:  finalValueAdd.a,
				keyField:        keyLoad.c,
				keySlot:         keyLoad.d,
				deltaField:      thenDelta.c,
				deltaSlot:       thenDelta.d,
				branchField:     branch.field,
				branchSlot:      branch.slot,
				thenValue:       branch.value,
				leftMapField:    leftMapGet.c,
				mutableMapField: mutableInputGet.c,
				finalMapField:   finalMapGet.c,
				divisor:         mutableDivide.c,
				lowerBound:      lowerBoundBranch.b,
				thenModulo:      thenControlMod.c,
				elseModulo:      elseControlMod.c,
				finalModulo:     finalControlMod.c,
			},
		},
	}, true
}

func detectArrayRowLoopActionBranchExecutionPlan(proto *Proto, pc int, ins instruction) (regionExecutionPlanDesc, bool) {
	prefix, ok := detectArrayRowLoopPrefixExecutionPlan(proto, pc, ins)
	if !ok {
		return regionExecutionPlanDesc{}, false
	}
	desc := prefix.arrayLoop
	action, ok := arrayRowLoopActionBranch(proto, desc, desc.prefixExitPC, ins.d-1)
	if !ok {
		return regionExecutionPlanDesc{}, false
	}
	desc.actionBranch = action
	desc.accumulator = action.accumulator
	return regionExecutionPlanDesc{
		kind:       regionExecutionPlanKindArrayRowLoop,
		entryPC:    pc,
		exitPC:     ins.d,
		fallbackPC: pc,
		arrayLoop:  desc,
	}, true
}

func arrayRowLoopActionBranch(proto *Proto, desc arrayRowLoopRegionDesc, pc int, bodyEnd int) (arrayRowLoopActionBranchDesc, bool) {
	if proto == nil ||
		!desc.predicate.enabled ||
		len(desc.mutations) != 2 ||
		pc+27 >= len(proto.code) ||
		bodyEnd <= pc ||
		bodyEnd >= len(proto.code) {
		return arrayRowLoopActionBranchDesc{}, false
	}
	predicate := desc.predicate
	computed := desc.mutations[0]
	clamp := desc.mutations[1]
	if predicate.op != opJumpIfRowStringFieldNotGreaterK ||
		!arrayRowLoopNumberConstantOK(proto, predicate.value) ||
		proto.constants[predicate.value].number != 0 ||
		computed.kind != arrayRowLoopFieldMutationKindComputedStore ||
		clamp.kind != arrayRowLoopFieldMutationKindClampLowerBound ||
		!sameStringConstant(proto, predicate.field, computed.field) ||
		!sameStringConstant(proto, predicate.field, clamp.field) ||
		predicate.slot != computed.slot ||
		predicate.slot != clamp.slot ||
		computed.constantOp != opSubK ||
		computed.op != opSub ||
		!arrayRowLoopNumberConstantOK(proto, computed.valueConstant) ||
		proto.constants[computed.valueConstant].number != 1 ||
		!arrayRowLoopNumberConstantOK(proto, clamp.threshold) ||
		proto.constants[clamp.threshold].number != 0 ||
		!arrayRowLoopNumberConstantOK(proto, clamp.clamp) ||
		proto.constants[clamp.clamp].number != 0 {
		return arrayRowLoopActionBranchDesc{}, false
	}
	cooldownLoad := proto.code[pc]
	zeroLoad := proto.code[pc+1]
	equal := proto.code[pc+2]
	firstJump := proto.code[pc+3]
	energyLoad := proto.code[pc+4]
	costLoad := proto.code[pc+5]
	greaterEqual := proto.code[pc+6]
	secondJump := proto.code[pc+7]
	elsePC := secondJump.b
	if cooldownLoad.op != opGetRowStringField ||
		cooldownLoad.b != desc.row ||
		!sameStringConstant(proto, cooldownLoad.c, predicate.field) ||
		cooldownLoad.d != predicate.slot ||
		zeroLoad.op != opLoadConst ||
		!arrayRowLoopNumberConstantOK(proto, zeroLoad.b) ||
		proto.constants[zeroLoad.b].number != 0 ||
		equal.op != opEqual ||
		equal.a != cooldownLoad.a ||
		equal.b != cooldownLoad.a ||
		equal.c != zeroLoad.a ||
		firstJump.op != opJumpIfFalse ||
		firstJump.a != equal.a ||
		firstJump.b != pc+7 ||
		energyLoad.op != opGetRowStringField ||
		energyLoad.b != computed.sourceBase ||
		costLoad.op != opGetRowStringField ||
		costLoad.b != desc.row ||
		greaterEqual.op != opGreaterEqual ||
		greaterEqual.a != equal.a ||
		greaterEqual.b != energyLoad.a ||
		greaterEqual.c != costLoad.a ||
		secondJump.op != opJumpIfFalse ||
		secondJump.a != greaterEqual.a ||
		elsePC <= pc+8 ||
		elsePC >= bodyEnd {
		return arrayRowLoopActionBranchDesc{}, false
	}
	energySetLoad := proto.code[pc+8]
	costSetLoad := proto.code[pc+9]
	energySub := proto.code[pc+10]
	energyStore := proto.code[pc+11]
	oneLoad := proto.code[pc+12]
	usesAdd := proto.code[pc+13]
	resetLoad := proto.code[pc+14]
	cooldownStore := proto.code[pc+15]
	scoreEnergyLoad := proto.code[pc+16]
	scoreEnergyAdd := proto.code[pc+17]
	usesLoad := proto.code[pc+18]
	costScoreLoad := proto.code[pc+19]
	usesCostMul := proto.code[pc+20]
	scoreUsesAdd := proto.code[pc+21]
	thenJump := proto.code[pc+22]
	if elsePC != pc+23 ||
		energySetLoad.op != opGetRowStringField ||
		energySetLoad.b != computed.sourceBase ||
		!sameStringConstant(proto, energySetLoad.c, energyLoad.c) ||
		energySetLoad.d != energyLoad.d ||
		costSetLoad.op != opGetRowStringField ||
		costSetLoad.b != desc.row ||
		!sameStringConstant(proto, costSetLoad.c, costLoad.c) ||
		costSetLoad.d != costLoad.d ||
		energySub.op != opSub ||
		energySub.a != energySetLoad.a ||
		energySub.b != energySetLoad.a ||
		energySub.c != costSetLoad.a ||
		energyStore.op != opSetRowStringField ||
		energyStore.a != computed.sourceBase ||
		energyStore.c != energySub.a ||
		energyStore.d != energyLoad.d ||
		!sameStringConstant(proto, energyStore.b, energyLoad.c) ||
		oneLoad.op != opLoadConst ||
		!arrayRowLoopNumberConstantOK(proto, oneLoad.b) ||
		proto.constants[oneLoad.b].number != 1 ||
		usesAdd.op != opAddStringField ||
		usesAdd.a != desc.row ||
		usesAdd.c != oneLoad.a ||
		resetLoad.op != opGetRowStringField ||
		resetLoad.b != desc.row ||
		cooldownStore.op != opSetRowStringField ||
		cooldownStore.a != desc.row ||
		cooldownStore.c != resetLoad.a ||
		cooldownStore.d != predicate.slot ||
		!sameStringConstant(proto, cooldownStore.b, predicate.field) ||
		scoreEnergyLoad.op != opGetRowStringField ||
		scoreEnergyLoad.b != computed.sourceBase ||
		!sameStringConstant(proto, scoreEnergyLoad.c, energyLoad.c) ||
		scoreEnergyLoad.d != energyLoad.d ||
		scoreEnergyAdd.op != opAdd ||
		scoreEnergyAdd.a != scoreEnergyAdd.b ||
		scoreEnergyAdd.c != scoreEnergyLoad.a ||
		usesLoad.op != opGetRowStringField ||
		usesLoad.b != desc.row ||
		costScoreLoad.op != opGetRowStringField ||
		costScoreLoad.b != desc.row ||
		!sameStringConstant(proto, costScoreLoad.c, costLoad.c) ||
		costScoreLoad.d != costLoad.d ||
		usesCostMul.op != opMul ||
		usesCostMul.a != usesLoad.a ||
		usesCostMul.b != usesLoad.a ||
		usesCostMul.c != costScoreLoad.a ||
		scoreUsesAdd.op != opAdd ||
		scoreUsesAdd.a != scoreEnergyAdd.a ||
		scoreUsesAdd.b != scoreEnergyAdd.a ||
		scoreUsesAdd.c != usesCostMul.a ||
		thenJump.op != opJump ||
		thenJump.b != bodyEnd {
		return arrayRowLoopActionBranchDesc{}, false
	}
	elseCooldownLoad := proto.code[elsePC]
	elseCooldownAdd := proto.code[elsePC+1]
	elseEnergyLoad := proto.code[elsePC+2]
	elseEnergyAdd := proto.code[elsePC+3]
	backJump := proto.code[bodyEnd]
	if elsePC+4 != bodyEnd ||
		elseCooldownLoad.op != opGetRowStringField ||
		elseCooldownLoad.b != desc.row ||
		!sameStringConstant(proto, elseCooldownLoad.c, predicate.field) ||
		elseCooldownLoad.d != predicate.slot ||
		elseCooldownAdd.op != opAdd ||
		elseCooldownAdd.a != scoreEnergyAdd.a ||
		elseCooldownAdd.b != scoreEnergyAdd.a ||
		elseCooldownAdd.c != elseCooldownLoad.a ||
		elseEnergyLoad.op != opGetRowStringField ||
		elseEnergyLoad.b != computed.sourceBase ||
		!sameStringConstant(proto, elseEnergyLoad.c, energyLoad.c) ||
		elseEnergyLoad.d != energyLoad.d ||
		elseEnergyAdd.op != opAdd ||
		elseEnergyAdd.a != scoreEnergyAdd.a ||
		elseEnergyAdd.b != scoreEnergyAdd.a ||
		elseEnergyAdd.c != elseEnergyLoad.a ||
		backJump.op != opJump {
		return arrayRowLoopActionBranchDesc{}, false
	}
	if !sameStringConstant(proto, usesAdd.b, usesLoad.c) {
		return arrayRowLoopActionBranchDesc{}, false
	}
	return arrayRowLoopActionBranchDesc{
		enabled:     true,
		actor:       computed.sourceBase,
		accumulator: scoreEnergyAdd.a,
		energyField: energyLoad.c,
		energySlot:  energyLoad.d,
		costField:   costLoad.c,
		costSlot:    costLoad.d,
		resetField:  resetLoad.c,
		resetSlot:   resetLoad.d,
		usesField:   usesLoad.c,
		usesSlot:    usesLoad.d,
		oneConstant: oneLoad.b,
	}, true
}

func detectArrayRowLoopPrefixExecutionPlan(proto *Proto, pc int, ins instruction) (regionExecutionPlanDesc, bool) {
	if proto == nil ||
		ins.d <= pc+1 ||
		ins.d > len(proto.code) ||
		!arrayRowLoopHasBackJump(proto.code, pc, ins.d) {
		return regionExecutionPlanDesc{}, false
	}
	bodyEnd := ins.d - 1
	desc := arrayRowLoopRegionDesc{
		iterator:     ins.b,
		array:        ins.c,
		index:        ins.a,
		row:          ins.a + 1,
		accumulator:  -1,
		prefixExitPC: -1,
	}
	bodyPC := pc + 1
	if bodyPC >= bodyEnd {
		return regionExecutionPlanDesc{}, false
	}
	body := proto.code[bodyPC]
	prefixLimit := bodyEnd
	switch body.op {
	case opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldNotNil, opJumpIfStringFieldTrue,
		opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
		if body.d <= bodyPC || body.d >= bodyEnd {
			return regionExecutionPlanDesc{}, false
		}
		predicate, ok := arrayRowLoopPredicate(proto, desc.row, bodyPC, body, body.d)
		if !ok {
			return regionExecutionPlanDesc{}, false
		}
		desc.predicate = predicate
		prefixLimit = body.d
		bodyPC++
	case opGetRowStringField:
		if bodyPC+1 < bodyEnd {
			predicate, ok := arrayRowLoopLoadedPredicate(proto, desc.row, bodyPC, body, proto.code[bodyPC+1], proto.code[bodyPC+1].d)
			if ok {
				if predicate.skipPC <= bodyPC+1 || predicate.skipPC >= bodyEnd {
					return regionExecutionPlanDesc{}, false
				}
				desc.predicate = predicate
				prefixLimit = predicate.skipPC
				bodyPC += 2
			}
		}
	}
	exitPC, mutations, ok := arrayRowLoopMutationPrefix(proto, desc.row, bodyPC, prefixLimit, desc.predicate.enabled)
	if !ok || len(mutations) == 0 || exitPC <= pc+1 || exitPC >= bodyEnd {
		return regionExecutionPlanDesc{}, false
	}
	if desc.predicate.enabled && desc.predicate.skipPC != exitPC {
		return regionExecutionPlanDesc{}, false
	}
	if arrayRowLoopHasNestedIterator(proto.code, pc+1, exitPC) ||
		arrayRowLoopHasNestedIterator(proto.code, exitPC, bodyEnd) ||
		len(regionCallsOrIntrinsics(proto, pc, exitPC)) != 0 {
		return regionExecutionPlanDesc{}, false
	}
	desc.prefixExitPC = exitPC
	desc.mutations = mutations
	return regionExecutionPlanDesc{
		kind:       regionExecutionPlanKindArrayRowLoop,
		entryPC:    pc,
		exitPC:     ins.d,
		fallbackPC: pc,
		arrayLoop:  desc,
	}, true
}

func arrayRowLoopMutationPrefix(proto *Proto, rowRegister int, pc int, limit int, conditional bool) (int, []arrayRowLoopFieldMutationDesc, bool) {
	var mutations []arrayRowLoopFieldMutationDesc
	for pc < limit {
		ins := proto.code[pc]
		switch ins.op {
		case opGetRowStringField:
			if pc+4 < limit {
				mutation, ok := arrayRowLoopComputedFieldMutation(proto, rowRegister, pc, proto.code)
				if ok {
					mutations = append(mutations, mutation)
					pc += 5
					continue
				}
			}
			if pc+3 < limit {
				mutation, ok := arrayRowLoopClampFieldMutation(proto, rowRegister, pc, proto.code)
				if ok {
					mutations = append(mutations, mutation)
					pc += 4
					continue
				}
			}
			if conditional {
				return 0, nil, false
			}
			return pc, mutations, len(mutations) != 0
		case opLoadConst:
			if pc+1 >= limit {
				return 0, nil, false
			}
			mutation, ok := arrayRowLoopFieldMutation(proto, rowRegister, pc, ins, proto.code[pc+1])
			if !ok {
				if conditional {
					return 0, nil, false
				}
				return pc, mutations, len(mutations) != 0
			}
			mutations = append(mutations, mutation)
			pc += 2
		case opJump:
			if ins.b == pc+1 {
				pc++
				continue
			}
			if ins.b == limit {
				pc = limit
				continue
			}
			return 0, nil, false
		default:
			if conditional {
				return 0, nil, false
			}
			return pc, mutations, len(mutations) != 0
		}
	}
	return pc, mutations, len(mutations) != 0
}

func arrayRowLoopFieldMutation(proto *Proto, rowRegister int, pc int, load instruction, store instruction) (arrayRowLoopFieldMutationDesc, bool) {
	if load.b < 0 ||
		load.b >= len(proto.constants) ||
		proto.constants[load.b].kind != NumberKind ||
		store.a != rowRegister ||
		store.c != load.a ||
		store.b < 0 ||
		store.b >= len(proto.constants) ||
		proto.constants[store.b].kind != StringKind ||
		store.d < 0 {
		return arrayRowLoopFieldMutationDesc{}, false
	}
	switch store.op {
	case opAddStringField, opSubStringField:
	default:
		return arrayRowLoopFieldMutationDesc{}, false
	}
	return arrayRowLoopFieldMutationDesc{
		kind:          arrayRowLoopFieldMutationKindConstStore,
		loadPC:        pc,
		storePC:       pc + 1,
		loadRegister:  load.a,
		valueRegister: load.a,
		valueConstant: load.b,
		field:         store.b,
		slot:          store.d,
		op:            store.op,
	}, true
}

func arrayRowLoopComputedFieldMutation(proto *Proto, rowRegister int, pc int, code []instruction) (arrayRowLoopFieldMutationDesc, bool) {
	if pc+4 >= len(code) {
		return arrayRowLoopFieldMutationDesc{}, false
	}
	load := code[pc]
	constArith := code[pc+1]
	sourceLoad := code[pc+2]
	arith := code[pc+3]
	store := code[pc+4]
	if load.op != opGetRowStringField ||
		load.b != rowRegister ||
		load.c < 0 ||
		load.c >= len(proto.constants) ||
		proto.constants[load.c].kind != StringKind ||
		load.d < 0 ||
		(constArith.op != opAddK && constArith.op != opSubK) ||
		constArith.a != load.a ||
		constArith.b != load.a ||
		constArith.c < 0 ||
		constArith.c >= len(proto.constants) ||
		proto.constants[constArith.c].kind != NumberKind ||
		sourceLoad.op != opGetRowStringField ||
		sourceLoad.c < 0 ||
		sourceLoad.c >= len(proto.constants) ||
		proto.constants[sourceLoad.c].kind != StringKind ||
		sourceLoad.d < 0 ||
		(arith.op != opAdd && arith.op != opSub) ||
		arith.a != load.a ||
		arith.b != load.a ||
		arith.c != sourceLoad.a ||
		store.op != opSetRowStringField ||
		store.a != rowRegister ||
		store.c != load.a ||
		store.d != load.d ||
		!sameStringConstant(proto, store.b, load.c) {
		return arrayRowLoopFieldMutationDesc{}, false
	}
	return arrayRowLoopFieldMutationDesc{
		kind:           arrayRowLoopFieldMutationKindComputedStore,
		loadPC:         pc,
		storePC:        pc + 4,
		loadRegister:   load.a,
		valueRegister:  load.a,
		valueConstant:  constArith.c,
		field:          load.c,
		slot:           load.d,
		constantOp:     constArith.op,
		sourceRegister: sourceLoad.a,
		sourceBase:     sourceLoad.b,
		sourceField:    sourceLoad.c,
		sourceSlot:     sourceLoad.d,
		op:             arith.op,
	}, true
}

func arrayRowLoopClampFieldMutation(proto *Proto, rowRegister int, pc int, code []instruction) (arrayRowLoopFieldMutationDesc, bool) {
	if pc+3 >= len(code) {
		return arrayRowLoopFieldMutationDesc{}, false
	}
	load := code[pc]
	branch := code[pc+1]
	clampLoad := code[pc+2]
	store := code[pc+3]
	if load.op != opGetRowStringField ||
		load.b != rowRegister ||
		load.c < 0 ||
		load.c >= len(proto.constants) ||
		proto.constants[load.c].kind != StringKind ||
		load.d < 0 ||
		branch.op != opJumpIfNotLessK ||
		branch.a != load.a ||
		branch.b < 0 ||
		branch.b >= len(proto.constants) ||
		proto.constants[branch.b].kind != NumberKind ||
		clampLoad.op != opLoadConst ||
		clampLoad.b < 0 ||
		clampLoad.b >= len(proto.constants) ||
		proto.constants[clampLoad.b].kind != NumberKind ||
		store.op != opSetRowStringField ||
		store.a != rowRegister ||
		store.c != clampLoad.a ||
		store.d != load.d ||
		!sameStringConstant(proto, store.b, load.c) ||
		(branch.d != pc+4 && branch.d != pc+5) {
		return arrayRowLoopFieldMutationDesc{}, false
	}
	return arrayRowLoopFieldMutationDesc{
		kind:          arrayRowLoopFieldMutationKindClampLowerBound,
		loadPC:        pc,
		storePC:       pc + 3,
		loadRegister:  load.a,
		valueRegister: clampLoad.a,
		field:         load.c,
		slot:          load.d,
		threshold:     branch.b,
		clamp:         clampLoad.b,
	}, true
}

func arrayRowLoopLoadedPredicate(proto *Proto, rowRegister int, pc int, load instruction, branch instruction, skipPC int) (arrayRowLoopPredicateDesc, bool) {
	if load.b != rowRegister ||
		load.c < 0 ||
		load.c >= len(proto.constants) ||
		load.d < 0 ||
		proto.constants[load.c].kind != StringKind ||
		branch.a != load.a ||
		branch.d != skipPC {
		return arrayRowLoopPredicateDesc{}, false
	}
	switch branch.op {
	case opJumpIfNotLessK:
		if branch.b < 0 || branch.b >= len(proto.constants) || proto.constants[branch.b].kind != NumberKind {
			return arrayRowLoopPredicateDesc{}, false
		}
	default:
		return arrayRowLoopPredicateDesc{}, false
	}
	return arrayRowLoopPredicateDesc{
		pc:      pc + 1,
		op:      branch.op,
		field:   load.c,
		value:   branch.b,
		slot:    load.d,
		skipPC:  skipPC,
		enabled: true,
	}, true
}

func arrayRowLoopPredicate(proto *Proto, rowRegister int, pc int, ins instruction, skipPC int) (arrayRowLoopPredicateDesc, bool) {
	if ins.a != rowRegister || ins.d != skipPC {
		return arrayRowLoopPredicateDesc{}, false
	}
	switch ins.op {
	case opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldNotNil, opJumpIfStringFieldTrue:
		if ins.b < 0 ||
			ins.b >= len(proto.constants) ||
			proto.constants[ins.b].kind != StringKind ||
			ins.c < 0 {
			return arrayRowLoopPredicateDesc{}, false
		}
		return arrayRowLoopPredicateDesc{
			pc:      pc,
			op:      ins.op,
			field:   ins.b,
			value:   -1,
			slot:    ins.c,
			skipPC:  skipPC,
			enabled: true,
		}, true
	}
	desc, ok := rowFieldEqualDesc(proto, ins.b)
	if !ok ||
		desc.field < 0 ||
		desc.field >= len(proto.constants) ||
		proto.constants[desc.field].kind != StringKind ||
		desc.value < 0 ||
		desc.value >= len(proto.constants) ||
		proto.constants[desc.value].kind != NumberKind ||
		desc.slot < 0 {
		return arrayRowLoopPredicateDesc{}, false
	}
	return arrayRowLoopPredicateDesc{
		pc:      pc,
		op:      ins.op,
		field:   desc.field,
		value:   desc.value,
		slot:    desc.slot,
		skipPC:  skipPC,
		enabled: true,
	}, true
}

func arrayRowLoopAddFieldOperand(loads map[int]arrayRowLoopFieldAddDesc, ins instruction) (arrayRowLoopFieldAddDesc, int, bool) {
	left, leftField := loads[ins.b]
	right, rightField := loads[ins.c]
	if leftField == rightField {
		return arrayRowLoopFieldAddDesc{}, 0, false
	}
	if leftField {
		return left, ins.c, true
	}
	return right, ins.b, true
}

func regionExecutionPlanPCs(codeLen int, plans []regionExecutionPlanDesc) []int {
	if codeLen <= 0 {
		return nil
	}
	pcs := make([]int, codeLen)
	for i := range pcs {
		pcs[i] = -1
	}
	for index, plan := range plans {
		if plan.entryPC >= 0 && plan.entryPC < len(pcs) {
			pcs[plan.entryPC] = index
		}
	}
	return pcs
}

type regionCoverageReport struct {
	candidates       []regionCandidateDesc
	retiredBytecodes uint64
	coveredBytecodes uint64
}

func (report regionCoverageReport) candidateByKind(kind string) (regionCandidateDesc, bool) {
	for _, candidate := range report.candidates {
		if candidate.kind == kind {
			return candidate, true
		}
	}
	return regionCandidateDesc{}, false
}

type regionCandidateDesc struct {
	kind              string
	entryPC           int
	exitPC            int
	fallbackPC        int
	entries           uint64
	retiredBytecodes  uint64
	requiredGuards    []string
	sideExitPCs       []int
	repairRegisters   []int
	tableSlots        []regionTableSlotDesc
	callsOrIntrinsics []int
	cost              regionCostEstimate
}

type regionTableSlotDesc struct {
	base    int
	field   int
	slot    int
	dynamic bool
}

type regionCostEstimate struct {
	guardCost         int
	repairCost        int
	expectedSavedWork int
	profitable        bool
	reason            string
}

func candidateRegions(proto *Proto, snapshot directFrameMechanismSnapshot) regionCoverageReport {
	if proto == nil {
		return regionCoverageReport{}
	}
	report := regionCoverageReport{
		retiredBytecodes: regionRetiredBytecodes(proto, snapshot),
	}
	coveredPCs := make([]bool, len(proto.code))
	for _, plan := range proto.blockPlans {
		candidate, ok := regionCandidateFromBlockPlan(proto, snapshot, plan)
		if !ok {
			continue
		}
		addRegionCandidate(proto, snapshot, &report, coveredPCs, candidate)
	}
	for _, candidate := range detectArrayRowLoopRegionCandidates(proto, snapshot) {
		addRegionCandidate(proto, snapshot, &report, coveredPCs, candidate)
	}
	sort.Slice(report.candidates, func(i, j int) bool {
		if report.candidates[i].entryPC == report.candidates[j].entryPC {
			return report.candidates[i].kind < report.candidates[j].kind
		}
		return report.candidates[i].entryPC < report.candidates[j].entryPC
	})
	return report
}

func addRegionCandidate(proto *Proto, snapshot directFrameMechanismSnapshot, report *regionCoverageReport, coveredPCs []bool, candidate regionCandidateDesc) {
	if candidate.retiredBytecodes == 0 {
		candidate.retiredBytecodes = regionRetiredBytecodesInSpan(proto, snapshot, candidate.entryPC, candidate.exitPC)
	}
	report.candidates = append(report.candidates, candidate)
	observed := regionHasObservedCounts(proto, snapshot)
	for pc := candidate.entryPC; pc < candidate.exitPC && pc < len(coveredPCs); pc++ {
		if pc < 0 || coveredPCs[pc] {
			continue
		}
		count := snapshot.pcCount(proto, pc)
		if count == 0 {
			if observed {
				continue
			}
			count = 1
		}
		report.coveredBytecodes += count
		coveredPCs[pc] = true
	}
}

func regionRetiredBytecodes(proto *Proto, snapshot directFrameMechanismSnapshot) uint64 {
	var total uint64
	for pc := range proto.code {
		total += snapshot.pcCount(proto, pc)
	}
	if total == 0 {
		return uint64(len(proto.code))
	}
	return total
}

func regionHasObservedCounts(proto *Proto, snapshot directFrameMechanismSnapshot) bool {
	if proto == nil {
		return false
	}
	for pc := range proto.code {
		if snapshot.pcCount(proto, pc) != 0 {
			return true
		}
	}
	return false
}

func regionRetiredBytecodesInSpan(proto *Proto, snapshot directFrameMechanismSnapshot, startPC int, exitPC int) uint64 {
	if proto == nil || startPC < 0 || exitPC <= startPC {
		return 0
	}
	if exitPC > len(proto.code) {
		exitPC = len(proto.code)
	}
	var total uint64
	for pc := startPC; pc < exitPC; pc++ {
		total += snapshot.pcCount(proto, pc)
	}
	if total != 0 {
		return total
	}
	return uint64(exitPC - startPC)
}

func regionCandidateFromBlockPlan(proto *Proto, snapshot directFrameMechanismSnapshot, plan blockPlanDesc) (regionCandidateDesc, bool) {
	if plan.kind == blockPlanKindInvalid ||
		plan.startPC < 0 ||
		plan.startPC >= len(proto.code) ||
		plan.resumePC <= plan.startPC ||
		plan.resumePC > len(proto.code) {
		return regionCandidateDesc{}, false
	}
	entries := snapshot.pcCount(proto, plan.startPC)
	if entries == 0 {
		entries = 1
	}
	candidate := regionCandidateDesc{
		kind:              blockPlanKindName(plan.kind),
		entryPC:           plan.startPC,
		exitPC:            plan.resumePC,
		fallbackPC:        plan.fallbackPC,
		entries:           entries,
		retiredBytecodes:  regionRetiredBytecodesInSpan(proto, snapshot, plan.startPC, plan.resumePC),
		requiredGuards:    regionRequiredGuards(plan),
		sideExitPCs:       regionSideExitPCs(plan),
		repairRegisters:   regionRepairRegisters(proto, plan.startPC, plan.resumePC),
		tableSlots:        regionTableSlots(plan),
		callsOrIntrinsics: regionCallsOrIntrinsics(proto, plan.startPC, plan.resumePC),
	}
	candidate.cost = estimateRegionCost(candidate)
	return candidate, true
}

func detectArrayRowLoopRegionCandidates(proto *Proto, snapshot directFrameMechanismSnapshot) []regionCandidateDesc {
	if proto == nil {
		return nil
	}
	var candidates []regionCandidateDesc
	for pc, ins := range proto.code {
		if ins.op != opArrayNextJump2 ||
			ins.d <= pc+1 ||
			ins.d > len(proto.code) ||
			!arrayRowLoopHasBackJump(proto.code, pc, ins.d) ||
			arrayRowLoopHasNestedIterator(proto.code, pc+1, ins.d-1) {
			continue
		}
		callsOrIntrinsics := regionCallsOrIntrinsics(proto, pc, ins.d)
		if len(callsOrIntrinsics) != 0 {
			continue
		}
		tableSlots := arrayRowLoopTableSlots(proto, pc+1, ins.d)
		if len(tableSlots) == 0 {
			continue
		}
		entries := snapshot.pcCount(proto, pc)
		if entries == 0 {
			entries = 1
		}
		candidate := regionCandidateDesc{
			kind:              "array_row_loop",
			entryPC:           pc,
			exitPC:            ins.d,
			fallbackPC:        pc,
			entries:           entries,
			retiredBytecodes:  regionRetiredBytecodesInSpan(proto, snapshot, pc, ins.d),
			requiredGuards:    []string{"array iterator", "row tables", "row slots"},
			sideExitPCs:       []int{pc},
			repairRegisters:   regionRepairRegisters(proto, pc, ins.d),
			tableSlots:        tableSlots,
			callsOrIntrinsics: callsOrIntrinsics,
		}
		candidate.cost = estimateRegionCost(candidate)
		candidates = append(candidates, candidate)
	}
	return candidates
}

func arrayRowLoopHasBackJump(code []instruction, entryPC int, exitPC int) bool {
	backJumpPC := exitPC - 1
	return backJumpPC >= 0 &&
		backJumpPC < len(code) &&
		code[backJumpPC].op == opJump &&
		code[backJumpPC].b == entryPC
}

func arrayRowLoopHasNestedIterator(code []instruction, startPC int, exitPC int) bool {
	for pc := startPC; pc < exitPC && pc < len(code); pc++ {
		switch code[pc].op {
		case opPrepareIter, opArrayNext, opArrayNextJump2, opNumericForCheck:
			return true
		}
	}
	return false
}

func arrayRowLoopTableSlots(proto *Proto, startPC int, exitPC int) []regionTableSlotDesc {
	seen := make(map[regionTableSlotDesc]bool)
	var slots []regionTableSlotDesc
	add := func(base int, field int, slot int) {
		if base < 0 || field < 0 || slot < 0 {
			return
		}
		desc := regionTableSlotDesc{base: base, field: field, slot: slot}
		if seen[desc] {
			return
		}
		seen[desc] = true
		slots = append(slots, desc)
	}
	for pc := startPC; pc < exitPC && pc < len(proto.code); pc++ {
		ins := proto.code[pc]
		switch ins.op {
		case opGetRowStringField:
			add(ins.b, ins.c, ins.d)
		case opSetRowStringField, opAddStringField, opSubStringField:
			add(ins.a, ins.b, ins.d)
		case opSubAddStringField:
			if desc, ok := rowFieldSubAddDesc(proto, ins.b); ok {
				add(ins.a, desc.target, desc.targetSlot)
				add(ins.a, desc.add, desc.addSlot)
			}
		case opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil:
			add(ins.a, ins.b, ins.c)
		case opJumpIfRowStringFieldNotEqualK, opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
			if desc, ok := rowFieldEqualDesc(proto, ins.b); ok {
				add(ins.a, desc.field, desc.slot)
			}
		case opJumpIfRowStringFieldNotGreaterR:
			if desc, ok := rowFieldRegisterDesc(proto, ins.b); ok {
				add(ins.a, desc.field, desc.slot)
			}
		case opJumpIfRowStringFieldNotEqualField, opJumpIfRowStringFieldEqualField:
			if desc, ok := rowFieldPairDesc(proto, ins.b); ok {
				add(ins.a, desc.leftField, desc.leftSlot)
				add(ins.c, desc.rightField, desc.rightSlot)
			}
		case opJumpIfRowStringFieldNotLessField:
			if desc, ok := rowFieldPairDesc(proto, ins.b); ok {
				add(ins.a, desc.leftField, desc.leftSlot)
				add(ins.a, desc.rightField, desc.rightSlot)
			}
		}
	}
	return slots
}

func regionRequiredGuards(plan blockPlanDesc) []string {
	switch plan.kind {
	case blockPlanKindAbsoluteDelta, blockPlanKindMax:
		return []string{"numeric operands"}
	case blockPlanKindPairedRowDiff:
		return []string{"array tables", "numeric operands"}
	case blockPlanKindRowFieldAddStore:
		return []string{"base table", "row slot", "numeric operands"}
	case blockPlanKindRowFieldBranchStore:
		return []string{"base table", "row slot", "numeric predicate"}
	case blockPlanKindDynamicPathAddStore:
		return []string{"base table", "parent slot", "child table", "dynamic string key", "numeric operands"}
	case blockPlanKindDynamicPathSub:
		return []string{"base tables", "parent slots", "child tables", "dynamic string key", "numeric operands"}
	case blockPlanKindDynamicPathSubIDivK:
		return []string{"base table", "parent slots", "child tables", "dynamic string key", "numeric operands"}
	case blockPlanKindRowFieldAddFieldStore:
		return []string{"base table", "row slot", "add slot", "numeric fields"}
	default:
		return nil
	}
}

func regionSideExitPCs(plan blockPlanDesc) []int {
	if plan.fallbackPC < 0 {
		return nil
	}
	return []int{plan.fallbackPC}
}

func regionRepairRegisters(proto *Proto, startPC int, resumePC int) []int {
	writes := make(registerSet)
	for pc := startPC; pc < resumePC && pc < len(proto.code); pc++ {
		ins := proto.code[pc]
		for register := 0; register < proto.registers; register++ {
			if instructionWritesRegister(ins, register) {
				writes.add(register)
			}
		}
	}
	return writes.values()
}

func regionTableSlots(plan blockPlanDesc) []regionTableSlotDesc {
	switch plan.kind {
	case blockPlanKindRowFieldAddStore, blockPlanKindRowFieldBranchStore:
		return []regionTableSlotDesc{{
			base:  plan.directBlock.register,
			field: plan.directBlock.field,
			slot:  plan.directBlock.slot,
		}}
	case blockPlanKindDynamicPathAddStore:
		return []regionTableSlotDesc{{
			base:  plan.dynamicPath.base,
			field: plan.dynamicPath.field,
			slot:  -1,
		}, {
			base:    plan.dynamicPath.base,
			field:   -1,
			slot:    -1,
			dynamic: true,
		}}
	case blockPlanKindDynamicPathSub, blockPlanKindDynamicPathSubIDivK:
		return []regionTableSlotDesc{{
			base:  plan.dynamicSub.leftBase,
			field: plan.dynamicSub.leftField,
			slot:  -1,
		}, {
			base:  plan.dynamicSub.rightBase,
			field: plan.dynamicSub.rightField,
			slot:  -1,
		}, {
			base:    plan.dynamicSub.leftBase,
			field:   -1,
			slot:    -1,
			dynamic: true,
		}}
	case blockPlanKindRowFieldAddFieldStore:
		return []regionTableSlotDesc{{
			base:  plan.rowField.base,
			field: plan.rowField.field,
			slot:  plan.rowField.slot,
		}, {
			base:  plan.rowField.base,
			field: plan.rowField.addField,
			slot:  plan.rowField.addSlot,
		}}
	default:
		return nil
	}
}

func regionCallsOrIntrinsics(proto *Proto, startPC int, resumePC int) []int {
	var pcs []int
	for pc := startPC; pc < resumePC && pc < len(proto.code); pc++ {
		ins := proto.code[pc]
		if opcodeMayCall(ins.op) || opcodeMayYield(ins.op) || regionOpcodeIsIntrinsic(ins.op) {
			pcs = append(pcs, pc)
		}
	}
	return pcs
}

func regionOpcodeIsIntrinsic(op opcode) bool {
	switch op {
	case opTableInsert, opTableRemove, opCoroutineResume, opMathMin, opSelectVarargCount:
		return true
	default:
		return false
	}
}

func estimateRegionCost(candidate regionCandidateDesc) regionCostEstimate {
	guardCost := len(candidate.requiredGuards) + len(candidate.tableSlots)
	repairCost := len(candidate.repairRegisters)
	entryCost := int(candidate.entries)
	tableWorkSaved := int(candidate.entries) * len(candidate.tableSlots) * 4
	callPenalty := len(candidate.callsOrIntrinsics) * 100
	expectedSaved := int(candidate.retiredBytecodes) + tableWorkSaved - entryCost - guardCost - repairCost - callPenalty
	estimate := regionCostEstimate{
		guardCost:         guardCost,
		repairCost:        repairCost,
		expectedSavedWork: expectedSaved,
	}
	if len(candidate.callsOrIntrinsics) != 0 {
		estimate.reason = "contains call or intrinsic risk"
		return estimate
	}
	if candidate.exitPC-candidate.entryPC < 4 && candidate.entries < 8 {
		estimate.reason = "too little observed coverage"
		return estimate
	}
	if expectedSaved <= 0 {
		estimate.reason = "estimated guard and repair cost exceeds saved work"
		return estimate
	}
	estimate.profitable = true
	estimate.reason = "estimated saved dispatch and table work exceeds guard and repair cost"
	return estimate
}

func (proto *Proto) verifiedDirectBlockPlanAt(pc int, kind string) (directBlockPlanDesc, bool) {
	plan, ok := proto.verifiedPlanAt(pc)
	if !ok || plan.kind != verifiedPlanKindDirectBlock || plan.directBlock.kind != kind {
		return directBlockPlanDesc{}, false
	}
	return plan.directBlock, true
}

func maxReductionFactForInstruction(code []instruction, pc int, ins instruction) (reductionFactDesc, bool) {
	if ins.op != opJumpIfNotGreater || ins.d <= pc+1 || ins.d > len(code) {
		return reductionFactDesc{}, false
	}
	mutationPC := -1
	mutationCount := 0
	for bodyPC := pc + 1; bodyPC < ins.d; bodyPC++ {
		body := code[bodyPC]
		if body.op == opJump && body.b == ins.d && bodyPC == ins.d-1 {
			continue
		}
		if body.op != opMove {
			return reductionFactDesc{}, false
		}
		mutationCount++
		if body.a == ins.b && body.b == ins.a {
			mutationPC = bodyPC
		}
	}
	if mutationPC < 0 {
		return reductionFactDesc{}, false
	}
	return reductionFactDesc{
		pc:            pc,
		kind:          "max",
		accumulator:   ins.b,
		candidate:     ins.a,
		predicatePC:   pc,
		mutationPC:    mutationPC,
		mutationCount: mutationCount,
	}, true
}

func pairedRowDiffReductionFactForInstruction(proto *Proto, pc int, ins instruction) (reductionFactDesc, bool) {
	if proto == nil || ins.op != opGetIndex || pc < 2 || pc+3 >= len(proto.code) {
		return reductionFactDesc{}, false
	}
	keyMove := proto.code[pc-1]
	iter := proto.code[pc-2]
	if keyMove.op != opMove || keyMove.a != ins.c || iter.op != opArrayNextJump2 || keyMove.b != iter.a {
		return reductionFactDesc{}, false
	}
	leftRow := iter.a + 1
	rightRow := ins.a
	leftLoad := proto.code[pc+1]
	rightLoad := proto.code[pc+2]
	diff := proto.code[pc+3]
	if leftLoad.op != opGetRowStringField || rightLoad.op != opGetRowStringField || diff.op != opSub {
		return reductionFactDesc{}, false
	}
	if leftLoad.b != leftRow || rightLoad.b != rightRow || !sameStringConstant(proto, leftLoad.c, rightLoad.c) {
		return reductionFactDesc{}, false
	}
	if diff.b != leftLoad.a || diff.c != rightLoad.a {
		return reductionFactDesc{}, false
	}
	return reductionFactDesc{
		pc:            pc,
		kind:          "paired_row_diff",
		accumulator:   leftRow,
		candidate:     rightRow,
		predicatePC:   pc - 2,
		mutationPC:    pc + 3,
		mutationCount: 1,
	}, true
}

func absoluteDeltaReductionFactForInstruction(proto *Proto, pc int, ins instruction) (reductionFactDesc, bool) {
	if proto == nil || ins.op != opJumpIfNotLessK || !constantIsNumberValue(proto, ins.b, 0) || ins.d <= pc+1 || ins.d > len(proto.code) {
		return reductionFactDesc{}, false
	}
	mutationPC := -1
	mutationCount := 0
	for bodyPC := pc + 1; bodyPC < ins.d; bodyPC++ {
		body := proto.code[bodyPC]
		if body.op == opJump && body.b == ins.d && bodyPC == ins.d-1 {
			continue
		}
		if body.op != opNeg || body.a != ins.a || body.b != ins.a {
			return reductionFactDesc{}, false
		}
		if mutationPC >= 0 {
			return reductionFactDesc{}, false
		}
		mutationPC = bodyPC
		mutationCount++
	}
	if mutationPC < 0 {
		return reductionFactDesc{}, false
	}
	return reductionFactDesc{
		pc:            pc,
		kind:          "absolute_delta",
		accumulator:   ins.a,
		candidate:     ins.a,
		predicatePC:   pc,
		mutationPC:    mutationPC,
		mutationCount: mutationCount,
	}, true
}

func allCompleteReductionFactForInstruction(proto *Proto, pc int, ins instruction) (reductionFactDesc, bool) {
	if proto == nil {
		return reductionFactDesc{}, false
	}
	target, ok := instructionJumpTarget(ins)
	if !ok || target <= pc+1 || target > len(proto.code) {
		return reductionFactDesc{}, false
	}
	mutationPC := -1
	accumulator := -1
	mutationCount := 0
	for bodyPC := pc + 1; bodyPC < target; bodyPC++ {
		body := proto.code[bodyPC]
		if body.op == opJump && body.b == target && bodyPC == target-1 {
			continue
		}
		if body.op != opLoadConst || !constantIsBool(proto, body.b, false) {
			return reductionFactDesc{}, false
		}
		if mutationPC >= 0 {
			return reductionFactDesc{}, false
		}
		mutationPC = bodyPC
		accumulator = body.a
		mutationCount++
	}
	if mutationPC < 0 {
		return reductionFactDesc{}, false
	}
	return reductionFactDesc{
		pc:            pc,
		kind:          "all_complete",
		accumulator:   accumulator,
		candidate:     reductionPredicateCandidate(ins),
		predicatePC:   pc,
		mutationPC:    mutationPC,
		mutationCount: mutationCount,
	}, true
}

func constantIsBool(proto *Proto, constant int, want bool) bool {
	return proto != nil &&
		constant >= 0 &&
		constant < len(proto.constants) &&
		proto.constants[constant].kind == BoolKind &&
		proto.constants[constant].bool == want
}

func constantIsNumberValue(proto *Proto, constant int, want float64) bool {
	return proto != nil &&
		constant >= 0 &&
		constant < len(proto.constants) &&
		proto.constants[constant].kind == NumberKind &&
		proto.constants[constant].number == want
}

func reductionPredicateCandidate(ins instruction) int {
	switch ins.op {
	case opJumpIfFalse, opJumpIfNotLessK, opJumpIfNotLess, opJumpIfNotGreater,
		opJumpIfModKNotEqualK, opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField, opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR, opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
		opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil:
		return ins.a
	default:
		return -1
	}
}

func detectLoopLocalPathFacts(proto *Proto) ([]pathFactDesc, []pathFactRejectionDesc) {
	if proto == nil {
		return nil, nil
	}
	code := proto.code
	var facts []pathFactDesc
	var rejections []pathFactRejectionDesc
	seen := make(map[pathFactDesc]bool)
	for loopEnd, ins := range code {
		if ins.op != opJump || ins.b < 0 || ins.b >= loopEnd {
			continue
		}
		loopStart := ins.b
		counts, rejection := loopLocalPathCounts(proto, code[loopStart:loopEnd])
		if rejection.valid() {
			if loopLocalPathHasRepeatedCandidate(counts) {
				birthPC := loopStart + loopLocalPathFirstRepeatedPC(counts)
				killPC := loopStart + rejection.pc
				rejections = append(rejections, pathFactRejectionDesc{
					loopStart:  loopStart,
					loopEnd:    loopEnd,
					birthPC:    birthPC,
					killPC:     killPC,
					fallbackPC: killPC,
					killKind:   rejection.kind,
					reason:     rejection.reason,
				})
			}
			continue
		}
		for key, count := range counts {
			if count.hits < 2 {
				continue
			}
			fact := pathFactDesc{
				loopStart:  loopStart,
				loopEnd:    loopEnd,
				birthPC:    loopStart + count.firstPC,
				backedgePC: loopEnd,
				fallbackPC: loopStart + count.firstPC,
				killPC:     -1,
				killKind:   "none",
				base:       key.base,
				field:      count.index,
				second:     count.secondIndex,
				dynamic:    key.dynamic,
				hits:       count.hits,
			}
			if seen[fact] {
				continue
			}
			seen[fact] = true
			facts = append(facts, fact)
		}
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].loopStart != facts[j].loopStart {
			return facts[i].loopStart < facts[j].loopStart
		}
		if facts[i].loopEnd != facts[j].loopEnd {
			return facts[i].loopEnd < facts[j].loopEnd
		}
		if facts[i].base != facts[j].base {
			return facts[i].base < facts[j].base
		}
		if facts[i].field != facts[j].field {
			return facts[i].field < facts[j].field
		}
		if facts[i].second != facts[j].second {
			return facts[i].second < facts[j].second
		}
		return !facts[i].dynamic && facts[j].dynamic
	})
	return facts, rejections
}

func detectPathPlans(proto *Proto, pathFacts []pathFactDesc) []pathPlanDesc {
	if proto == nil {
		return nil
	}
	loopRanges := pathPlanLoopRanges(proto.code)
	var plans []pathPlanDesc
	for pc, ins := range proto.code {
		loopRange := pathPlanLoopRangeAt(loopRanges, pc)
		switch ins.op {
		case opGetStringField2:
			fact, ok := pathFactForStringField2(proto, pathFacts, pc, ins.b, ins.c, ins.d)
			plans = append(plans, pathPlanFromFact(pc, "read", fact, ok, loopRange, ins.b, ins.c, ins.d, false, -1, -1))
		case opSetStringField2:
			fact, ok := pathFactForStringField2(proto, pathFacts, pc, ins.a, ins.b, ins.c)
			plans = append(plans, pathPlanFromFact(pc, "write", fact, ok, loopRange, ins.a, ins.b, ins.c, false, -1, ins.d))
		case opGetStringFieldIndex:
			fact, ok := pathFactForStringFieldIndex(proto, pathFacts, pc, ins.b, ins.c)
			plans = append(plans, pathPlanFromFact(pc, "read", fact, ok, loopRange, ins.b, ins.c, -1, true, ins.d, -1))
		case opSetStringFieldIndex:
			fact, ok := pathFactForStringFieldIndex(proto, pathFacts, pc, ins.a, ins.b)
			plans = append(plans, pathPlanFromFact(pc, "write", fact, ok, loopRange, ins.a, ins.b, -1, true, ins.c, ins.d))
		case opAddSubStringField2:
			if ins.b < 0 || ins.b >= len(proto.stringField2AddSubOps) {
				continue
			}
			desc := proto.stringField2AddSubOps[ins.b]
			fact, ok := pathFactForStringField2(proto, pathFacts, pc, ins.a, desc.targetFirst, desc.targetSecond)
			plans = append(plans, pathPlanFromFact(pc, "read_modify_write", fact, ok, loopRange, ins.a, desc.targetFirst, desc.targetSecond, false, -1, -1))
			fact, ok = pathFactForStringField2(proto, pathFacts, pc, ins.a, desc.addFirst, desc.addSecond)
			plans = append(plans, pathPlanFromFact(pc, "read", fact, ok, loopRange, ins.a, desc.addFirst, desc.addSecond, false, -1, -1))
			fact, ok = pathFactForStringField2(proto, pathFacts, pc, ins.a, desc.subFirst, desc.subSecond)
			plans = append(plans, pathPlanFromFact(pc, "read", fact, ok, loopRange, ins.a, desc.subFirst, desc.subSecond, false, -1, -1))
		}
	}
	return plans
}

func pathPlanFromFact(pc int, access string, fact pathFactDesc, hasFact bool, loopRange pathPlanLoopRange, base int, field int, second int, dynamic bool, keySource int, valueSource int) pathPlanDesc {
	loopStart := -1
	loopEnd := -1
	if hasFact {
		loopStart = fact.loopStart
		loopEnd = fact.loopEnd
	} else if loopRange.valid() {
		loopStart = loopRange.start
		loopEnd = loopRange.end
	}
	return pathPlanDesc{
		pc:          pc,
		access:      access,
		loopStart:   loopStart,
		loopEnd:     loopEnd,
		base:        base,
		field:       field,
		second:      second,
		dynamic:     dynamic,
		keySource:   keySource,
		valueSource: valueSource,
		fallbackPC:  pc,
	}
}

func pathPlanLoopRanges(code []instruction) []pathPlanLoopRange {
	ranges := make([]pathPlanLoopRange, len(code))
	for pc := range ranges {
		ranges[pc] = pathPlanLoopRange{start: -1, end: -1}
	}
	for loopEnd, ins := range code {
		if ins.op != opJump || ins.b < 0 || ins.b >= loopEnd {
			continue
		}
		loopStart := ins.b
		width := loopEnd - loopStart
		for pc := loopStart; pc < loopEnd; pc++ {
			current := ranges[pc]
			if !current.valid() || width < current.end-current.start {
				ranges[pc] = pathPlanLoopRange{start: loopStart, end: loopEnd}
			}
		}
	}
	return ranges
}

func pathPlanLoopRangeAt(ranges []pathPlanLoopRange, pc int) pathPlanLoopRange {
	if pc < 0 || pc >= len(ranges) {
		return pathPlanLoopRange{start: -1, end: -1}
	}
	return ranges[pc]
}

func pathFactForStringField2(proto *Proto, pathFacts []pathFactDesc, pc int, base int, field int, second int) (pathFactDesc, bool) {
	for _, fact := range pathFacts {
		if fact.dynamic || fact.second < 0 || pc < fact.loopStart || pc >= fact.loopEnd || fact.base != base {
			continue
		}
		if sameStringConstant(proto, fact.field, field) && sameStringConstant(proto, fact.second, second) {
			return fact, true
		}
	}
	return pathFactDesc{}, false
}

func pathFactForStringFieldIndex(proto *Proto, pathFacts []pathFactDesc, pc int, base int, field int) (pathFactDesc, bool) {
	for _, fact := range pathFacts {
		if !fact.dynamic || fact.second >= 0 || pc < fact.loopStart || pc >= fact.loopEnd || fact.base != base {
			continue
		}
		if sameStringConstant(proto, fact.field, field) {
			return fact, true
		}
	}
	return pathFactDesc{}, false
}

type loopLocalPathKey struct {
	base    int
	field   string
	second  string
	dynamic bool
}

type loopLocalPathCount struct {
	index       int
	secondIndex int
	firstPC     int
	hits        int
}

type loopLocalPathRejection struct {
	pc     int
	kind   string
	reason string
}

func (rejection loopLocalPathRejection) valid() bool {
	return rejection.reason != ""
}

func loopLocalPathCounts(proto *Proto, code []instruction) (map[loopLocalPathKey]loopLocalPathCount, loopLocalPathRejection) {
	counts := make(map[loopLocalPathKey]loopLocalPathCount)
	var rejection loopLocalPathRejection
	for pc, ins := range code {
		if barrier := loopLocalPathFactBarrier(ins); barrier.valid() && !rejection.valid() {
			rejection = loopLocalPathRejection{
				pc:     pc,
				kind:   barrier.kind,
				reason: barrier.reason,
			}
		}
		if ins.op == opGetStringField || ins.op == opGetRowStringField {
			if ins.c < 0 || ins.c >= len(proto.constants) || proto.constants[ins.c].kind != StringKind {
				continue
			}
			key := loopLocalPathKey{base: ins.b, field: proto.constants[ins.c].str}
			count := counts[key]
			if count.hits == 0 {
				count.index = ins.c
				count.secondIndex = -1
				count.firstPC = pc
			}
			count.hits++
			counts[key] = count
			continue
		}
		if ins.op == opGetStringField2 {
			if ins.c < 0 || ins.c >= len(proto.constants) || proto.constants[ins.c].kind != StringKind ||
				ins.d < 0 || ins.d >= len(proto.constants) || proto.constants[ins.d].kind != StringKind {
				continue
			}
			key := loopLocalPathKey{base: ins.b, field: proto.constants[ins.c].str, second: proto.constants[ins.d].str}
			count := counts[key]
			if count.hits == 0 {
				count.index = ins.c
				count.secondIndex = ins.d
				count.firstPC = pc
			}
			count.hits++
			counts[key] = count
			continue
		}
		if ins.op == opGetStringFieldIndex {
			if ins.c < 0 || ins.c >= len(proto.constants) || proto.constants[ins.c].kind != StringKind {
				continue
			}
			key := loopLocalPathKey{base: ins.b, field: proto.constants[ins.c].str, dynamic: true}
			count := counts[key]
			if count.hits == 0 {
				count.index = ins.c
				count.secondIndex = -1
				count.firstPC = pc
			}
			count.hits++
			counts[key] = count
		}
	}
	return counts, rejection
}

func loopLocalPathHasRepeatedCandidate(counts map[loopLocalPathKey]loopLocalPathCount) bool {
	for _, count := range counts {
		if count.hits >= 2 {
			return true
		}
	}
	return false
}

func loopLocalPathFirstRepeatedPC(counts map[loopLocalPathKey]loopLocalPathCount) int {
	first := -1
	for _, count := range counts {
		if count.hits < 2 {
			continue
		}
		if first < 0 || count.firstPC < first {
			first = count.firstPC
		}
	}
	return first
}

func loopLocalPathFactBarrier(ins instruction) loopLocalPathRejection {
	if opcodeWritesTable(ins.op) {
		return loopLocalPathRejection{kind: "table_local", reason: "table write"}
	}
	if opcodeWritesGlobal(ins.op) {
		return loopLocalPathRejection{kind: "global", reason: "global write"}
	}
	switch ins.op {
	case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne,
		opCallUpvalueSelfOne, opCallUpvalueSelfKOne, opCallUpvalueSelfAddKOne,
		opCallMethodOne, opCallTableFieldKeyOne, opCoroutineResume,
		opTableInsert, opTableRemove:
		return loopLocalPathRejection{kind: "call", reason: "call"}
	default:
		return loopLocalPathRejection{}
	}
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
	if proto.directRegisters && len(proto.capturedLocals) != 0 {
		return fmt.Errorf("direct-register prototype has captured locals")
	}
	if proto.directFrameDispatch && !proto.directRegisters {
		return fmt.Errorf("direct-frame prototype is not direct-register")
	}
	if proto.directFrameDispatch {
		if rejection, rejected := protoDirectFrameRejection(proto); rejected {
			if rejection.op != 0 {
				return fmt.Errorf("direct-frame prototype contains unsupported opcode %s at pc %d: %s", opcodeName(rejection.op), rejection.pc, rejection.reason)
			}
			return fmt.Errorf("direct-frame prototype rejected: %s", rejection.reason)
		}
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
	wantPathFacts, wantPathFactRejections := detectLoopLocalPathFacts(proto)
	if want := detectSlotKindFacts(proto); !equalSlotKindFactDescs(proto.slotKindFacts, want) {
		return fmt.Errorf("slot kind facts %v do not match finalized plan %v", proto.slotKindFacts, want)
	}
	if want := detectPathKindFacts(wantPathFacts); !equalPathKindFactDescs(proto.pathKindFacts, want) {
		return fmt.Errorf("path kind facts %v do not match finalized plan %v", proto.pathKindFacts, want)
	}
	if want := detectPredicateBranches(proto, wantPathFacts); !equalPredicateBranchDescs(proto.predicateBranches, want) {
		return fmt.Errorf("predicate branch descriptors %v do not match finalized plan %v", proto.predicateBranches, want)
	}
	if want := detectBranchRefinements(proto.predicateBranches); !equalBranchRefinementDescs(proto.branchRefinements, want) {
		return fmt.Errorf("branch refinements %v do not match finalized plan %v", proto.branchRefinements, want)
	}
	if want := detectFiniteTagRefinements(proto, proto.predicateBranches); !equalFiniteTagRefinementDescs(proto.finiteTagRefinements, want) {
		return fmt.Errorf("finite tag refinements %v do not match finalized plan %v", proto.finiteTagRefinements, want)
	}
	if want := detectReductionFacts(proto); !equalReductionFactDescs(proto.reductionFacts, want) {
		return fmt.Errorf("reduction facts %v do not match finalized plan %v", proto.reductionFacts, want)
	}
	if want := detectDirectBlockPlans(proto, proto.reductionFacts); !equalDirectBlockPlanDescs(proto.directBlockPlans, want) {
		return fmt.Errorf("direct block plans %v do not match finalized plan %v", proto.directBlockPlans, want)
	}
	if want := directBlockPlanPCs(len(proto.code), proto.directBlockPlans); !equalIntSlices(proto.directBlockPlanPCs, want) {
		return fmt.Errorf("direct block plan pc map %v does not match finalized plan %v", proto.directBlockPlanPCs, want)
	}
	if want := detectBlockPlans(proto, proto.directBlockPlans, proto.pathPlans); !equalBlockPlanDescs(proto.blockPlans, want) {
		return fmt.Errorf("block plans %v do not match finalized plan %v", proto.blockPlans, want)
	}
	if want := blockPlanPCs(len(proto.code), proto.blockPlans); !equalIntSlices(proto.blockPlanPCs, want) {
		return fmt.Errorf("block plan pc map %v does not match finalized plan %v", proto.blockPlanPCs, want)
	}
	if want := detectRegionExecutionPlans(proto); !equalRegionExecutionPlanDescs(proto.regionExecutionPlans, want) {
		return fmt.Errorf("region execution plans %v do not match finalized plan %v", proto.regionExecutionPlans, want)
	}
	if want := regionExecutionPlanPCs(len(proto.code), proto.regionExecutionPlans); !equalIntSlices(proto.regionExecutionPlanPCs, want) {
		return fmt.Errorf("region execution plan pc map %v does not match finalized plan %v", proto.regionExecutionPlanPCs, want)
	}
	wantVerifiedPlans, wantVerifiedPlanRejections := detectVerifiedPlans(proto, proto.directBlockPlans)
	if !equalVerifiedPlanDescs(proto.verifiedPlans, wantVerifiedPlans) {
		return fmt.Errorf("verified plans %v do not match finalized plan %v", proto.verifiedPlans, wantVerifiedPlans)
	}
	if want := verifiedPlanPCs(len(proto.code), proto.verifiedPlans); !equalIntSlices(proto.verifiedPlanPCs, want) {
		return fmt.Errorf("verified plan pc map %v does not match finalized plan %v", proto.verifiedPlanPCs, want)
	}
	if !equalVerifiedPlanRejectionDescs(proto.verifiedPlanRejections, wantVerifiedPlanRejections) {
		return fmt.Errorf("verified plan rejections %v do not match finalized plan %v", proto.verifiedPlanRejections, wantVerifiedPlanRejections)
	}
	if !equalPathFactDescs(proto.pathFacts, wantPathFacts) {
		return fmt.Errorf("path facts %v do not match finalized plan %v", proto.pathFacts, wantPathFacts)
	}
	if !equalPathFactRejectionDescs(proto.pathFactRejections, wantPathFactRejections) {
		return fmt.Errorf("path fact rejections %v do not match finalized plan %v", proto.pathFactRejections, wantPathFactRejections)
	}
	if want := detectPathPlans(proto, wantPathFacts); !equalPathPlanDescs(proto.pathPlans, want) {
		return fmt.Errorf("path plans %v do not match finalized plan %v", proto.pathPlans, want)
	}
	for index, upvalue := range proto.upvalues {
		if upvalue.index < 0 {
			return fmt.Errorf("upvalue %d has negative index %d", index, upvalue.index)
		}
	}
	for pc, ins := range proto.code {
		if err := verifyInstruction(proto, pc, ins); err != nil {
			return fmt.Errorf("instruction %d: %w", pc, err)
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
	_, rejected := protoDirectFrameRejection(proto)
	return !rejected
}

func protoDirectFrameRejection(proto *Proto) (directFrameRejection, bool) {
	if proto == nil {
		return directFrameRejection{pc: -1, reason: "nil prototype"}, true
	}
	if !proto.directRegisters {
		return directFrameRejection{pc: -1, reason: "prototype has captured locals"}, true
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

func equalPathKindFactDescs(left []pathKindFactDesc, right []pathKindFactDesc) bool {
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

func equalPredicateBranchDescs(left []predicateBranchDesc, right []predicateBranchDesc) bool {
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

func equalBranchRefinementDescs(left []branchRefinementDesc, right []branchRefinementDesc) bool {
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

func equalFiniteTagRefinementDescs(left []finiteTagRefinementDesc, right []finiteTagRefinementDesc) bool {
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

func equalReductionFactDescs(left []reductionFactDesc, right []reductionFactDesc) bool {
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

func equalDirectBlockPlanDescs(left []directBlockPlanDesc, right []directBlockPlanDesc) bool {
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

func equalBlockPlanDescs(left []blockPlanDesc, right []blockPlanDesc) bool {
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

func equalRegionExecutionPlanDescs(left []regionExecutionPlanDesc, right []regionExecutionPlanDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].kind != right[i].kind ||
			left[i].entryPC != right[i].entryPC ||
			left[i].exitPC != right[i].exitPC ||
			left[i].fallbackPC != right[i].fallbackPC ||
			left[i].arrayLoop.iterator != right[i].arrayLoop.iterator ||
			left[i].arrayLoop.array != right[i].arrayLoop.array ||
			left[i].arrayLoop.index != right[i].arrayLoop.index ||
			left[i].arrayLoop.row != right[i].arrayLoop.row ||
			left[i].arrayLoop.accumulator != right[i].arrayLoop.accumulator ||
			left[i].arrayLoop.prefixExitPC != right[i].arrayLoop.prefixExitPC ||
			left[i].arrayLoop.actionBranch != right[i].arrayLoop.actionBranch ||
			left[i].arrayLoop.dynamicMap != right[i].arrayLoop.dynamicMap ||
			left[i].arrayLoop.indexedMapBranch != right[i].arrayLoop.indexedMapBranch ||
			left[i].arrayLoop.predicate != right[i].arrayLoop.predicate ||
			!equalArrayRowLoopFieldMutationDescs(left[i].arrayLoop.mutations, right[i].arrayLoop.mutations) ||
			!equalArrayRowLoopFieldAddDescs(left[i].arrayLoop.fields, right[i].arrayLoop.fields) {
			return false
		}
	}
	return true
}

func equalArrayRowLoopFieldMutationDescs(left []arrayRowLoopFieldMutationDesc, right []arrayRowLoopFieldMutationDesc) bool {
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

func equalArrayRowLoopFieldAddDescs(left []arrayRowLoopFieldAddDesc, right []arrayRowLoopFieldAddDesc) bool {
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

func equalVerifiedPlanDescs(left []verifiedPlanDesc, right []verifiedPlanDesc) bool {
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

func equalVerifiedPlanRejectionDescs(left []verifiedPlanRejectionDesc, right []verifiedPlanRejectionDesc) bool {
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

func equalPathFactDescs(left []pathFactDesc, right []pathFactDesc) bool {
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

func equalPathFactRejectionDescs(left []pathFactRejectionDesc, right []pathFactRejectionDesc) bool {
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

func equalPathPlanDescs(left []pathPlanDesc, right []pathPlanDesc) bool {
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
	case opSetField, opSetStringField, opSetRowStringField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		if ins.op == opSetStringField || ins.op == opSetRowStringField {
			if err := verifyStringConstant(proto, ins.b); err != nil {
				return err
			}
			if ins.op == opSetRowStringField && ins.d < 0 {
				return fmt.Errorf("negative row string field slot %d", ins.d)
			}
		}
		return nil
	case opSetStringField2:
		if err := verifyRegisters(proto, ins.a, ins.d); err != nil {
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
		return verifyStringConstant(proto, ins.c)
	case opSetStringFieldIndex:
		if err := verifyRegisters(proto, ins.a, ins.c, ins.d); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
	case opGetField, opGetStringField, opGetRowStringField:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		if ins.op == opGetStringField || ins.op == opGetRowStringField {
			if err := verifyStringConstant(proto, ins.c); err != nil {
				return err
			}
			if ins.op == opGetRowStringField && ins.d < 0 {
				return fmt.Errorf("negative row string field slot %d", ins.d)
			}
		}
		return nil
	case opGetStringField2:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.c); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.d); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.d)
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
	case opSubAddStringField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		return verifyRowFieldSubAddOp(proto, ins.b)
	case opAddNumericModK:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyNumericAddModOp(proto, ins.c)
	case opAddSubStringField2:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		return verifyStringField2AddSubOp(proto, ins.b)
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
	case opJumpIfNotEqualK, opJumpIfNotLessK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfNotLess, opJumpIfNotGreater:
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
	case opJumpIfRowStringFieldNotEqualK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyRowFieldEqualOp(proto, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfRowStringFieldNotEqualField:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyRowFieldPairOp(proto, ins.b); err != nil {
			return err
		}
		if err := verifyRegister(proto, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfRowStringFieldEqualField:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyRowFieldPairOp(proto, ins.b); err != nil {
			return err
		}
		if err := verifyRegister(proto, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyRowFieldNumericOp(proto, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfRowStringFieldNotGreaterR:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyRowFieldRegisterOp(proto, ins.b); err != nil {
			return err
		}
		if err := verifyRegister(proto, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d)
	case opJumpIfRowStringFieldNotLessField:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyRowFieldPairOp(proto, ins.b); err != nil {
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
	case opTableInsert, opTableRemove, opCoroutineResume, opMathMin:
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
	case opSelectVarargCount:
		if !proto.variadic {
			return fmt.Errorf("select vararg count in non-variadic prototype")
		}
		if ins.d == 0 {
			return fmt.Errorf("select vararg count has zero result count")
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
	case opCallUpvalueOne, opCallUpvalueSelfOne:
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
	case opCallUpvalueSelfKOne:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		if err := verifyUpvalue(proto, ins.b); err != nil {
			return err
		}
		return verifyConstant(proto, ins.d)
	case opCallUpvalueSelfAddKOne:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		if err := verifyUpvalue(proto, ins.b); err != nil {
			return err
		}
		return verifySelfCallAddOp(proto, ins.d)
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
	case opCallTableFieldKeyOne:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.c); err != nil {
			return err
		}
		argCount := tableFieldKeyCallArgCount(ins.d)
		keySlot := tableFieldKeyCallKeySlot(ins.d)
		if argCount < 0 {
			return fmt.Errorf("table field-key one-result call has negative argument count %d", argCount)
		}
		if keySlot < -1 {
			return fmt.Errorf("table field-key one-result call has invalid key slot %d", keySlot)
		}
		if err := verifyRegisterSpan(proto, ins.a+1, argCount); err != nil {
			return err
		}
		return verifyRegister(proto, ins.a+argCount+1)
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

func verifyStringField2AddSubOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.stringField2AddSubOps) {
		return fmt.Errorf("string field update descriptor %d out of range", index)
	}
	desc := proto.stringField2AddSubOps[index]
	for _, constant := range []int{
		desc.targetFirst,
		desc.targetSecond,
		desc.addFirst,
		desc.addSecond,
		desc.subFirst,
		desc.subSecond,
	} {
		if err := verifyStringConstant(proto, constant); err != nil {
			return err
		}
	}
	return nil
}

func verifyRowFieldSubAddOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.rowFieldSubAddOps) {
		return fmt.Errorf("row field sub-add descriptor %d out of range", index)
	}
	desc := proto.rowFieldSubAddOps[index]
	for _, constant := range []int{desc.target, desc.add} {
		if err := verifyConstant(proto, constant); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, constant); err != nil {
			return err
		}
	}
	if desc.targetSlot < -1 {
		return fmt.Errorf("row field sub-add descriptor %d has invalid target slot %d", index, desc.targetSlot)
	}
	if desc.addSlot < -1 {
		return fmt.Errorf("row field sub-add descriptor %d has invalid add slot %d", index, desc.addSlot)
	}
	return nil
}

func verifyRowFieldEqualOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.rowFieldEqualOps) {
		return fmt.Errorf("row field equality descriptor %d out of range", index)
	}
	desc := proto.rowFieldEqualOps[index]
	if err := verifyConstant(proto, desc.field); err != nil {
		return err
	}
	if err := verifyStringConstant(proto, desc.field); err != nil {
		return err
	}
	if err := verifyConstant(proto, desc.value); err != nil {
		return err
	}
	if desc.slot < -1 {
		return fmt.Errorf("row field equality descriptor %d has invalid slot %d", index, desc.slot)
	}
	return nil
}

func verifyRowFieldNumericOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.rowFieldEqualOps) {
		return fmt.Errorf("row field numeric descriptor %d out of range", index)
	}
	desc := proto.rowFieldEqualOps[index]
	if err := verifyConstant(proto, desc.field); err != nil {
		return err
	}
	if err := verifyStringConstant(proto, desc.field); err != nil {
		return err
	}
	if err := verifyConstant(proto, desc.value); err != nil {
		return err
	}
	if err := verifyNumberConstant(proto, desc.value); err != nil {
		return err
	}
	if desc.slot < -1 {
		return fmt.Errorf("row field numeric descriptor %d has invalid slot %d", index, desc.slot)
	}
	return nil
}

func verifyRowFieldRegisterOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.rowFieldRegisterOps) {
		return fmt.Errorf("row field register descriptor %d out of range", index)
	}
	desc := proto.rowFieldRegisterOps[index]
	if err := verifyConstant(proto, desc.field); err != nil {
		return err
	}
	if err := verifyStringConstant(proto, desc.field); err != nil {
		return err
	}
	if desc.slot < -1 {
		return fmt.Errorf("row field register descriptor %d has invalid slot %d", index, desc.slot)
	}
	return nil
}

func verifyRowFieldPairOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.rowFieldPairOps) {
		return fmt.Errorf("row field pair descriptor %d out of range", index)
	}
	desc := proto.rowFieldPairOps[index]
	for _, constant := range []int{desc.leftField, desc.rightField} {
		if err := verifyConstant(proto, constant); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, constant); err != nil {
			return err
		}
	}
	if desc.leftSlot < -1 {
		return fmt.Errorf("row field pair descriptor %d has invalid left slot %d", index, desc.leftSlot)
	}
	if desc.rightSlot < -1 {
		return fmt.Errorf("row field pair descriptor %d has invalid right slot %d", index, desc.rightSlot)
	}
	return nil
}

func verifyNumericAddModOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.numericAddModOps) {
		return fmt.Errorf("numeric add-mod descriptor %d out of range", index)
	}
	desc := proto.numericAddModOps[index]
	for _, constant := range []int{desc.mul, desc.idiv, desc.mod} {
		if err := verifyConstant(proto, constant); err != nil {
			return err
		}
		if err := verifyNumberConstant(proto, constant); err != nil {
			return err
		}
	}
	return nil
}

func verifySelfCallAddOp(proto *Proto, index int) error {
	if index < 0 || index >= len(proto.selfCallAddOps) {
		return fmt.Errorf("self-call add descriptor %d out of range", index)
	}
	desc := proto.selfCallAddOps[index]
	for _, constant := range []int{desc.baseLess, desc.firstSub, desc.secondSub} {
		if err := verifyConstant(proto, constant); err != nil {
			return err
		}
		if err := verifyNumberConstant(proto, constant); err != nil {
			return err
		}
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
		fmt.Sprintf("direct_registers %t", proto.directRegisters),
		fmt.Sprintf("direct_frame_dispatch %t", proto.directFrameDispatch),
		fmt.Sprintf("direct_leaf_call_one %t", proto.directLeafCallOne),
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
	for _, fact := range proto.pathKindFacts {
		field := fmt.Sprintf("k%d", fact.field)
		if text, ok := stringConstantText(proto, fact.field); ok {
			field = text
		}
		if fact.second >= 0 {
			if text, ok := stringConstantText(proto, fact.second); ok {
				field += "." + text
			} else {
				field += fmt.Sprintf(".k%d", fact.second)
			}
		}
		if fact.dynamic {
			field += " dynamic_key"
		}
		line := fmt.Sprintf(
			"path_kind loop %d..%d base r%d field %s %s source %s",
			fact.loopStart,
			fact.loopEnd,
			fact.base,
			field,
			fact.kind.String(),
			fact.source,
		)
		if fact.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	for _, branch := range proto.predicateBranches {
		line := fmt.Sprintf(
			"predicate_branch pc%d target %d source %s op %s",
			branch.pc,
			branch.target,
			branch.source,
			branch.op,
		)
		if branch.base >= 0 {
			line += fmt.Sprintf(" base r%d", branch.base)
		}
		if branch.field >= 0 {
			line += " field " + disassemblePredicateBranchField(proto, branch.field, branch.second)
		}
		if branch.value >= 0 {
			line += " value " + disassembleConstant(proto, branch.value)
		}
		if branch.other >= 0 {
			line += fmt.Sprintf(" other r%d", branch.other)
		}
		if branch.slot >= 0 {
			line += fmt.Sprintf(" slot %d", branch.slot)
		}
		if branch.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	for _, refinement := range proto.branchRefinements {
		line := fmt.Sprintf(
			"branch_refinement pc%d edge %s target %d source %s fact %s",
			refinement.pc,
			refinement.edge,
			refinement.target,
			refinement.source,
			refinement.fact,
		)
		line += disassembleRefinementDetail(proto, refinement.base, refinement.field, refinement.second, refinement.value, refinement.other, refinement.slot)
		if refinement.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	for _, refinement := range proto.finiteTagRefinements {
		line := fmt.Sprintf(
			"finite_tag_refinement pc%d source %s option %d/%d",
			refinement.pc,
			refinement.source,
			refinement.ordinal,
			refinement.count,
		)
		line += disassembleRefinementDetail(proto, refinement.base, refinement.field, refinement.second, refinement.value, -1, refinement.slot)
		if refinement.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	for _, fact := range proto.reductionFacts {
		lines = append(lines, fmt.Sprintf(
			"reduction pc%d kind %s accumulator r%d candidate r%d predicate pc%d mutation pc%d mutations %d",
			fact.pc,
			fact.kind,
			fact.accumulator,
			fact.candidate,
			fact.predicatePC,
			fact.mutationPC,
			fact.mutationCount,
		))
	}
	for _, plan := range proto.directBlockPlans {
		line := fmt.Sprintf(
			"direct_block_plan pc%d kind %s start pc%d resume pc%d register r%d candidate r%d mutation pc%d mutations %d",
			plan.pc,
			plan.kind,
			plan.startPC,
			plan.resumePC,
			plan.register,
			plan.candidate,
			plan.mutationPC,
			plan.mutationCount,
		)
		if plan.field >= 0 {
			line += " field " + disassembleConstant(proto, plan.field)
		}
		if plan.slot >= 0 {
			line += fmt.Sprintf(" slot %d", plan.slot)
		}
		lines = append(lines, line)
	}
	for _, plan := range proto.blockPlans {
		line := fmt.Sprintf(
			"block_plan pc%d family %s start pc%d resume pc%d fallback pc%d",
			plan.pc,
			blockPlanKindName(plan.kind),
			plan.startPC,
			plan.resumePC,
			plan.fallbackPC,
		)
		if plan.directBlock.field >= 0 {
			line += " field " + disassembleConstant(proto, plan.directBlock.field)
		}
		if plan.directBlock.slot >= 0 {
			line += fmt.Sprintf(" slot %d", plan.directBlock.slot)
		}
		if plan.kind == blockPlanKindDynamicPathAddStore {
			field := fmt.Sprintf("k%d", plan.dynamicPath.field)
			if value, ok := stringConstantText(proto, plan.dynamicPath.field); ok {
				field = value
			}
			line += fmt.Sprintf(
				" base r%d field %s dynamic_key key r%d delta r%d result r%d op %s store pc%d",
				plan.dynamicPath.base,
				field,
				plan.dynamicPath.key,
				plan.dynamicPath.delta,
				plan.dynamicPath.result,
				opcodeName(plan.dynamicPath.op),
				plan.dynamicPath.storePC,
			)
		}
		if plan.kind == blockPlanKindDynamicPathSub || plan.kind == blockPlanKindDynamicPathSubIDivK {
			left := fmt.Sprintf("k%d", plan.dynamicSub.leftField)
			if value, ok := stringConstantText(proto, plan.dynamicSub.leftField); ok {
				left = value
			}
			right := fmt.Sprintf("k%d", plan.dynamicSub.rightField)
			if value, ok := stringConstantText(proto, plan.dynamicSub.rightField); ok {
				right = value
			}
			line += fmt.Sprintf(
				" left_base r%d right_base r%d left %s right %s dynamic_key key r%d result r%d",
				plan.dynamicSub.leftBase,
				plan.dynamicSub.rightBase,
				left,
				right,
				plan.dynamicSub.key,
				plan.dynamicSub.result,
			)
			if plan.dynamicSub.divisor >= 0 {
				line += " divisor " + disassembleConstant(proto, plan.dynamicSub.divisor)
			}
		}
		if plan.kind == blockPlanKindRowFieldAddFieldStore {
			field := fmt.Sprintf("k%d", plan.rowField.field)
			if value, ok := stringConstantText(proto, plan.rowField.field); ok {
				field = value
			}
			addField := fmt.Sprintf("k%d", plan.rowField.addField)
			if value, ok := stringConstantText(proto, plan.rowField.addField); ok {
				addField = value
			}
			line += fmt.Sprintf(
				" base r%d field %s slot %d add_field %s add_slot %d const k%d const_op %s op %s result r%d store pc%d",
				plan.rowField.base,
				field,
				plan.rowField.slot,
				addField,
				plan.rowField.addSlot,
				plan.rowField.constant,
				opcodeName(plan.rowField.constOp),
				opcodeName(plan.rowField.op),
				plan.rowField.result,
				plan.rowField.storePC,
			)
		}
		lines = append(lines, line)
	}
	for _, fact := range proto.pathFacts {
		field := fmt.Sprintf("k%d", fact.field)
		if fact.field >= 0 && fact.field < len(proto.constants) && proto.constants[fact.field].kind == StringKind {
			field = proto.constants[fact.field].str
		}
		if fact.second >= 0 && fact.second < len(proto.constants) && proto.constants[fact.second].kind == StringKind {
			field += "." + proto.constants[fact.second].str
		}
		if fact.dynamic {
			field += " dynamic_key"
		}
		lines = append(lines, fmt.Sprintf(
			"path_fact loop %d..%d base r%d field %s hits %d birth pc%d backedge pc%d kill %s fallback pc%d",
			fact.loopStart,
			fact.loopEnd,
			fact.base,
			field,
			fact.hits,
			fact.birthPC,
			fact.backedgePC,
			fact.killKind,
			fact.fallbackPC,
		))
	}
	for _, rejection := range proto.pathFactRejections {
		lines = append(lines, fmt.Sprintf(
			"path_fact_rejection loop %d..%d birth pc%d kill %s kill pc%d fallback pc%d %s",
			rejection.loopStart,
			rejection.loopEnd,
			rejection.birthPC,
			rejection.killKind,
			rejection.killPC,
			rejection.fallbackPC,
			rejection.reason,
		))
	}
	for _, plan := range proto.pathPlans {
		field := fmt.Sprintf("k%d", plan.field)
		if value, ok := stringConstantText(proto, plan.field); ok {
			field = value
		}
		if plan.second >= 0 {
			if value, ok := stringConstantText(proto, plan.second); ok {
				field += "." + value
			} else {
				field += fmt.Sprintf(".k%d", plan.second)
			}
		}
		if plan.dynamic {
			field += " dynamic_key"
		}
		line := fmt.Sprintf(
			"path_plan pc%d access %s loop %d..%d base r%d field %s fallback pc%d",
			plan.pc,
			plan.access,
			plan.loopStart,
			plan.loopEnd,
			plan.base,
			field,
			plan.fallbackPC,
		)
		if plan.keySource >= 0 {
			line += fmt.Sprintf(" key r%d", plan.keySource)
		}
		if plan.valueSource >= 0 {
			line += fmt.Sprintf(" value r%d", plan.valueSource)
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
	case opSetRowStringField:
		return "SET_ROW_STRING_FIELD"
	case opSetStringField2:
		return "SET_STRING_FIELD2"
	case opSetStringFieldIndex:
		return "SET_STRING_FIELD_INDEX"
	case opGetStringField:
		return "GET_STRING_FIELD"
	case opGetRowStringField:
		return "GET_ROW_STRING_FIELD"
	case opGetStringField2:
		return "GET_STRING_FIELD2"
	case opGetStringFieldIndex:
		return "GET_STRING_FIELD_INDEX"
	case opAddStringField:
		return "ADD_STRING_FIELD"
	case opSubStringField:
		return "SUB_STRING_FIELD"
	case opSubAddStringField:
		return "SUB_ADD_STRING_FIELD"
	case opAddSubStringField2:
		return "ADD_SUB_STRING_FIELD2"
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
	case opAddNumericModK:
		return "ADD_NUMERIC_MOD_K"
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
	case opJumpIfNotEqualK:
		return "JUMP_IF_NOT_EQUAL_K"
	case opJumpIfNotLessK:
		return "JUMP_IF_NOT_LESS_K"
	case opJumpIfNotLess:
		return "JUMP_IF_NOT_LESS"
	case opJumpIfNotGreater:
		return "JUMP_IF_NOT_GREATER"
	case opJumpIfModKNotEqualK:
		return "JUMP_IF_MOD_K_NOT_EQUAL_K"
	case opJumpIfTableHasMetatable:
		return "JUMP_IF_TABLE_HAS_METATABLE"
	case opJumpIfStringFieldNotEqualK:
		return "JUMP_IF_STRING_FIELD_NOT_EQUAL_K"
	case opJumpIfRowStringFieldNotEqualK:
		return "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K"
	case opJumpIfRowStringFieldNotEqualField:
		return "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_FIELD"
	case opJumpIfRowStringFieldEqualField:
		return "JUMP_IF_ROW_STRING_FIELD_EQUAL_FIELD"
	case opJumpIfStringFieldNotGreaterK:
		return "JUMP_IF_STRING_FIELD_NOT_GREATER_K"
	case opJumpIfStringFieldGreaterK:
		return "JUMP_IF_STRING_FIELD_GREATER_K"
	case opJumpIfRowStringFieldNotGreaterK:
		return "JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K"
	case opJumpIfRowStringFieldGreaterK:
		return "JUMP_IF_ROW_STRING_FIELD_GREATER_K"
	case opJumpIfStringFieldNotGreaterR:
		return "JUMP_IF_STRING_FIELD_NOT_GREATER_R"
	case opJumpIfRowStringFieldNotGreaterR:
		return "JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_R"
	case opJumpIfRowStringFieldNotLessField:
		return "JUMP_IF_ROW_STRING_FIELD_NOT_LESS_FIELD"
	case opJumpIfStringFieldFalse:
		return "JUMP_IF_STRING_FIELD_FALSE"
	case opJumpIfStringFieldNil:
		return "JUMP_IF_STRING_FIELD_NIL"
	case opJumpIfStringFieldTrue:
		return "JUMP_IF_STRING_FIELD_TRUE"
	case opJumpIfStringFieldNotNil:
		return "JUMP_IF_STRING_FIELD_NOT_NIL"
	case opTableInsert:
		return "TABLE_INSERT"
	case opTableRemove:
		return "TABLE_REMOVE"
	case opCoroutineResume:
		return "COROUTINE_RESUME"
	case opMathMin:
		return "MATH_MIN"
	case opSelectVarargCount:
		return "SELECT_VARARG_COUNT"
	case opCall:
		return "CALL"
	case opCallOne:
		return "CALL_ONE"
	case opCallLocalOne:
		return "CALL_LOCAL_ONE"
	case opCallUpvalueOne:
		return "CALL_UPVALUE_ONE"
	case opCallUpvalueSelfOne:
		return "CALL_UPVALUE_SELF_ONE"
	case opCallUpvalueSelfKOne:
		return "CALL_UPVALUE_SELF_K_ONE"
	case opCallUpvalueSelfAddKOne:
		return "CALL_UPVALUE_SELF_ADD_K_ONE"
	case opCallMethodOne:
		return "CALL_METHOD_ONE"
	case opCallTableFieldKeyOne:
		return "CALL_TABLE_FIELD_KEY_ONE"
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
	case opSetRowStringField:
		return fmt.Sprintf("SET_ROW_STRING_FIELD r%d %s r%d slot %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opSetStringField2:
		return fmt.Sprintf("SET_STRING_FIELD2 r%d %s %s r%d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opSetStringFieldIndex:
		return fmt.Sprintf("SET_STRING_FIELD_INDEX r%d %s r%d r%d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opGetStringField:
		return fmt.Sprintf("GET_STRING_FIELD r%d r%d %s", ins.a, ins.b, disassembleConstant(proto, ins.c))
	case opGetRowStringField:
		return fmt.Sprintf("GET_ROW_STRING_FIELD r%d r%d %s slot %d", ins.a, ins.b, disassembleConstant(proto, ins.c), ins.d)
	case opGetStringField2:
		return fmt.Sprintf("GET_STRING_FIELD2 r%d r%d %s %s", ins.a, ins.b, disassembleConstant(proto, ins.c), disassembleConstant(proto, ins.d))
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
	case opSubAddStringField:
		if ins.b < 0 || ins.b >= len(proto.rowFieldSubAddOps) {
			return fmt.Sprintf("SUB_ADD_STRING_FIELD r%d descriptor %d r%d", ins.a, ins.b, ins.c)
		}
		desc := proto.rowFieldSubAddOps[ins.b]
		return fmt.Sprintf(
			"SUB_ADD_STRING_FIELD r%d %s r%d %s slots %d %d",
			ins.a,
			disassembleConstant(proto, desc.target),
			ins.c,
			disassembleConstant(proto, desc.add),
			desc.targetSlot,
			desc.addSlot,
		)
	case opAddSubStringField2:
		if ins.b < 0 || ins.b >= len(proto.stringField2AddSubOps) {
			return fmt.Sprintf("ADD_SUB_STRING_FIELD2 r%d descriptor %d", ins.a, ins.b)
		}
		desc := proto.stringField2AddSubOps[ins.b]
		return fmt.Sprintf(
			"ADD_SUB_STRING_FIELD2 r%d %s %s %s %s %s %s",
			ins.a,
			disassembleConstant(proto, desc.targetFirst),
			disassembleConstant(proto, desc.targetSecond),
			disassembleConstant(proto, desc.addFirst),
			disassembleConstant(proto, desc.addSecond),
			disassembleConstant(proto, desc.subFirst),
			disassembleConstant(proto, desc.subSecond),
		)
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
	case opAddNumericModK:
		if ins.c < 0 || ins.c >= len(proto.numericAddModOps) {
			return fmt.Sprintf("ADD_NUMERIC_MOD_K r%d r%d descriptor %d", ins.a, ins.b, ins.c)
		}
		desc := proto.numericAddModOps[ins.c]
		return fmt.Sprintf(
			"ADD_NUMERIC_MOD_K r%d r%d %s %s %s",
			ins.a,
			ins.b,
			disassembleConstant(proto, desc.mul),
			disassembleConstant(proto, desc.idiv),
			disassembleConstant(proto, desc.mod),
		)
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
	case opJumpIfNotEqualK:
		return fmt.Sprintf("JUMP_IF_NOT_EQUAL_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotLessK:
		return fmt.Sprintf("JUMP_IF_NOT_LESS_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotLess:
		return fmt.Sprintf("JUMP_IF_NOT_LESS r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfNotGreater:
		return fmt.Sprintf("JUMP_IF_NOT_GREATER r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfModKNotEqualK:
		return fmt.Sprintf("JUMP_IF_MOD_K_NOT_EQUAL_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfTableHasMetatable:
		return fmt.Sprintf("JUMP_IF_TABLE_HAS_METATABLE r%d %d", ins.a, ins.d)
	case opJumpIfStringFieldNotEqualK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_EQUAL_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfRowStringFieldNotEqualK:
		if ins.b < 0 || ins.b >= len(proto.rowFieldEqualOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K r%d descriptor %d %d", ins.a, ins.b, ins.d)
		}
		desc := proto.rowFieldEqualOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K r%d %s %s slot %d %d", ins.a, disassembleConstant(proto, desc.field), disassembleConstant(proto, desc.value), desc.slot, ins.d)
	case opJumpIfRowStringFieldNotEqualField:
		if ins.b < 0 || ins.b >= len(proto.rowFieldPairOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_FIELD r%d descriptor %d r%d %d", ins.a, ins.b, ins.c, ins.d)
		}
		desc := proto.rowFieldPairOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_FIELD r%d %s r%d %s slots %d %d %d", ins.a, disassembleConstant(proto, desc.leftField), ins.c, disassembleConstant(proto, desc.rightField), desc.leftSlot, desc.rightSlot, ins.d)
	case opJumpIfRowStringFieldEqualField:
		if ins.b < 0 || ins.b >= len(proto.rowFieldPairOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_EQUAL_FIELD r%d descriptor %d r%d %d", ins.a, ins.b, ins.c, ins.d)
		}
		desc := proto.rowFieldPairOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_EQUAL_FIELD r%d %s r%d %s slots %d %d %d", ins.a, disassembleConstant(proto, desc.leftField), ins.c, disassembleConstant(proto, desc.rightField), desc.leftSlot, desc.rightSlot, ins.d)
	case opJumpIfStringFieldNotGreaterK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_GREATER_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfStringFieldGreaterK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_GREATER_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfRowStringFieldNotGreaterK:
		if ins.b < 0 || ins.b >= len(proto.rowFieldEqualOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K r%d descriptor %d %d", ins.a, ins.b, ins.d)
		}
		desc := proto.rowFieldEqualOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K r%d %s %s slot %d %d", ins.a, disassembleConstant(proto, desc.field), disassembleConstant(proto, desc.value), desc.slot, ins.d)
	case opJumpIfRowStringFieldGreaterK:
		if ins.b < 0 || ins.b >= len(proto.rowFieldEqualOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_GREATER_K r%d descriptor %d %d", ins.a, ins.b, ins.d)
		}
		desc := proto.rowFieldEqualOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_GREATER_K r%d %s %s slot %d %d", ins.a, disassembleConstant(proto, desc.field), disassembleConstant(proto, desc.value), desc.slot, ins.d)
	case opJumpIfStringFieldNotGreaterR:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_GREATER_R r%d %s r%d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfRowStringFieldNotGreaterR:
		if ins.b < 0 || ins.b >= len(proto.rowFieldRegisterOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_R r%d descriptor %d r%d %d", ins.a, ins.b, ins.c, ins.d)
		}
		desc := proto.rowFieldRegisterOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_R r%d %s r%d slot %d %d", ins.a, disassembleConstant(proto, desc.field), ins.c, desc.slot, ins.d)
	case opJumpIfRowStringFieldNotLessField:
		if ins.b < 0 || ins.b >= len(proto.rowFieldPairOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_LESS_FIELD r%d descriptor %d %d", ins.a, ins.b, ins.d)
		}
		desc := proto.rowFieldPairOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_LESS_FIELD r%d %s %s slots %d %d %d", ins.a, disassembleConstant(proto, desc.leftField), disassembleConstant(proto, desc.rightField), desc.leftSlot, desc.rightSlot, ins.d)
	case opJumpIfStringFieldFalse:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_FALSE r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfStringFieldNil:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NIL r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfStringFieldTrue:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_TRUE r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opJumpIfStringFieldNotNil:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_NIL r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opTableInsert:
		return fmt.Sprintf("TABLE_INSERT r%d %d %d", ins.a, ins.b, ins.d)
	case opTableRemove:
		return fmt.Sprintf("TABLE_REMOVE r%d %d %d", ins.a, ins.b, ins.d)
	case opCoroutineResume:
		return fmt.Sprintf("COROUTINE_RESUME r%d %d %d", ins.a, ins.b, ins.d)
	case opMathMin:
		return fmt.Sprintf("MATH_MIN r%d %d %d", ins.a, ins.b, ins.d)
	case opSelectVarargCount:
		return fmt.Sprintf("SELECT_VARARG_COUNT r%d %d", ins.a, ins.d)
	case opCall:
		return fmt.Sprintf("CALL r%d r%d %d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallOne:
		return fmt.Sprintf("CALL_ONE r%d r%d %d", ins.a, ins.b, ins.c)
	case opCallLocalOne:
		return fmt.Sprintf("CALL_LOCAL_ONE r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallUpvalueOne:
		return fmt.Sprintf("CALL_UPVALUE_ONE r%d u%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallUpvalueSelfOne:
		return fmt.Sprintf("CALL_UPVALUE_SELF_ONE r%d u%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallUpvalueSelfKOne:
		return fmt.Sprintf("CALL_UPVALUE_SELF_K_ONE r%d u%d r%d %s", ins.a, ins.b, ins.c, disassembleConstant(proto, ins.d))
	case opCallUpvalueSelfAddKOne:
		if ins.d < 0 || ins.d >= len(proto.selfCallAddOps) {
			return fmt.Sprintf("CALL_UPVALUE_SELF_ADD_K_ONE r%d u%d r%d descriptor %d", ins.a, ins.b, ins.c, ins.d)
		}
		desc := proto.selfCallAddOps[ins.d]
		return fmt.Sprintf(
			"CALL_UPVALUE_SELF_ADD_K_ONE r%d u%d r%d base %s subtract %s %s",
			ins.a,
			ins.b,
			ins.c,
			disassembleConstant(proto, desc.baseLess),
			disassembleConstant(proto, desc.firstSub),
			disassembleConstant(proto, desc.secondSub),
		)
	case opCallMethodOne:
		return fmt.Sprintf("CALL_METHOD_ONE r%d r%d %s %d", ins.a, ins.b, disassembleConstant(proto, ins.c), ins.d)
	case opCallTableFieldKeyOne:
		return fmt.Sprintf("CALL_TABLE_FIELD_KEY_ONE r%d r%d %s args %d keyslot %d", ins.a, ins.b, disassembleConstant(proto, ins.c), tableFieldKeyCallArgCount(ins.d), tableFieldKeyCallKeySlot(ins.d))
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
	return value.str
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
		return fmt.Sprintf("k%d(string %q)", index, value.str)
	default:
		return fmt.Sprintf("k%d(%s)", index, value.Kind())
	}
}
