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
	opGetStringField
	opGetRowStringField
	opGetStringField2
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
	opJumpIfModKNotEqualK
	opJumpIfStringFieldNotEqualK
	opJumpIfRowStringFieldNotEqualK
	opJumpIfStringFieldNotGreaterK
	opJumpIfStringFieldGreaterK
	opJumpIfStringFieldFalse
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
)

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
	b.ir = optimizeBytecodeIR(b.ir, options)
}

func (b *bytecodeBuilder) proto(upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	proto := newProtoWithDescriptors(b.constants, b.assembledCode(), b.prototypes, b.stringField2AddSubOps, b.rowFieldSubAddOps, b.rowFieldEqualOps, b.numericAddModOps, b.selfCallAddOps, upvalues, registers, params, variadic)
	proto.lines = bytecodeIRLines(b.sourceText, b.ir)
	proto.verifyErr = verifyProto(proto)
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
	case opJumpIfModKNotEqualK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.b},
			c: bytecodeOperand{kind: bytecodeOperandConstant, value: ins.c},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfRowStringFieldNotEqualK:
		return bytecodeOperands{
			a: bytecodeOperand{kind: bytecodeOperandRegister, value: ins.a},
			b: bytecodeOperand{kind: bytecodeOperandCount, value: ins.b},
			d: bytecodeOperand{kind: bytecodeOperandJumpTarget, value: ins.d},
		}
	case opJumpIfStringFieldFalse:
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
	switch ins.op {
	case opJump, opJumpIfFalse, opNumericForCheck, opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfModKNotEqualK, opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK, opJumpIfStringFieldFalse, opReturnOne, opReturn:
		return true
	default:
		return false
	}
}

func bytecodeIRJumpTarget(ins bytecodeIRInstruction) (int, bool) {
	switch ins.op {
	case opJump, opJumpIfFalse:
		target := ins.operands.b
		return target.value, target.kind == bytecodeOperandJumpTarget
	case opNumericForCheck:
		target := ins.operands.d
		return target.value, target.kind == bytecodeOperandJumpTarget
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfModKNotEqualK:
		target := ins.operands.d
		return target.value, target.kind == bytecodeOperandJumpTarget
	case opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK, opJumpIfStringFieldFalse:
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
		switch last.op {
		case opJump:
			if target, ok := bytecodeIRJumpTarget(last); ok {
				if successor, ok := blockByStart[target]; ok {
					successors[block.id] = append(successors[block.id], successor)
				}
			}
		case opJumpIfFalse, opNumericForCheck, opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfModKNotEqualK, opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK, opJumpIfStringFieldFalse:
			if target, ok := bytecodeIRJumpTarget(last); ok {
				if successor, ok := blockByStart[target]; ok {
					successors[block.id] = append(successors[block.id], successor)
				}
			}
			if successor, ok := blockByStart[block.end]; ok {
				successors[block.id] = append(successors[block.id], successor)
			}
		case opReturnOne, opReturn:
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
	if ins.op == opSetStringField2 {
		addNonNegativeRegisterCandidate(candidates, ins.d)
	}
	if ins.op == opReturn && ins.b > 0 {
		for register := ins.a; register < ins.a+ins.b; register++ {
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
	constants             []Value
	constantKeys          []tableKey
	constantKeyOK         []bool
	constantNumbers       []float64
	constantNumberOK      []bool
	code                  []instruction
	lines                 []int
	prototypes            []*Proto
	stringField2AddSubOps []stringField2AddSubOp
	rowFieldSubAddOps     []rowFieldSubAddOp
	rowFieldEqualOps      []rowFieldEqualOp
	numericAddModOps      []numericAddModOp
	numericForLoops       []numericForLoopDesc
	intrinsicOps          []intrinsicOpDesc
	selfCallAddOps        []selfCallAddOp
	upvalues              []upvalueDesc
	registers             int
	params                int
	variadic              bool
	capturedLocals        []bool
	directRegisters       bool
	directFrameDispatch   bool
	entryNilRegisters     []int
	fastMethodFieldAdd    int
	hasFastMethodFieldAdd bool
	fastUpvalueAdd        int
	hasFastUpvalueAdd     bool
	fastVariadicWeights   []int
	hasFastVariadicSum    bool
	verifyErr             error
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
	pc      int
	op      opcode
	base    int
	args    int
	results int
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

func newProto(constants []Value, code []instruction, prototypes []*Proto, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	return newProtoWithDescriptors(constants, code, prototypes, nil, nil, nil, nil, nil, upvalues, registers, params, variadic)
}

func newProtoWithDescriptors(constants []Value, code []instruction, prototypes []*Proto, stringField2AddSubOps []stringField2AddSubOp, rowFieldSubAddOps []rowFieldSubAddOp, rowFieldEqualOps []rowFieldEqualOp, numericAddModOps []numericAddModOp, selfCallAddOps []selfCallAddOp, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	proto := &Proto{
		constants:             constants,
		code:                  code,
		prototypes:            prototypes,
		stringField2AddSubOps: stringField2AddSubOps,
		rowFieldSubAddOps:     rowFieldSubAddOps,
		rowFieldEqualOps:      rowFieldEqualOps,
		numericAddModOps:      numericAddModOps,
		selfCallAddOps:        selfCallAddOps,
		upvalues:              upvalues,
		registers:             registers,
		params:                params,
		variadic:              variadic,
	}
	proto.constantKeys, proto.constantKeyOK = protoConstantTableKeys(proto.constants)
	proto.constantNumbers, proto.constantNumberOK = protoConstantNumbers(proto.constants)
	proto.numericForLoops = detectNumericForLoops(proto.code)
	proto.intrinsicOps = detectIntrinsicOps(proto.code)
	proto.capturedLocals = capturedLocalRegisters(proto)
	proto.directRegisters = len(proto.capturedLocals) == 0
	_, directFrameRejected := protoDirectFrameRejection(proto)
	proto.directFrameDispatch = proto.directRegisters && !directFrameRejected
	proto.entryNilRegisters = protoEntryNilRegisters(proto.code, proto.params, proto.registers)
	proto.fastMethodFieldAdd, proto.hasFastMethodFieldAdd = detectFastMethodFieldAdd(proto)
	proto.fastUpvalueAdd, proto.hasFastUpvalueAdd = detectFastUpvalueAdd(proto)
	proto.fastVariadicWeights, proto.hasFastVariadicSum = detectFastVariadicWeightedSum(proto)
	proto.verifyErr = verifyProto(proto)
	return proto
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
			ops = append(ops, intrinsicOpDesc{
				pc:      pc,
				op:      ins.op,
				base:    ins.a,
				args:    ins.b,
				results: ins.d,
			})
		case opSelectVarargCount:
			ops = append(ops, intrinsicOpDesc{
				pc:      pc,
				op:      ins.op,
				base:    ins.a,
				args:    0,
				results: ins.d,
			})
		}
	}
	return ops
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
	switch ins.op {
	case opJump:
		return []int{ins.b}
	case opJumpIfFalse:
		return []int{pc + 1, ins.b}
	case opNumericForCheck, opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfModKNotEqualK, opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK, opJumpIfStringFieldFalse:
		return []int{pc + 1, ins.d}
	case opReturnOne, opReturn:
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
	if proto.directFrameDispatch && !protoSupportsDirectFrame(proto) {
		return fmt.Errorf("direct-frame prototype contains unsupported opcode")
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
				reason: "unsupported opcode",
			}, true
		}
	}
	return directFrameRejection{}, false
}

func directFrameOpcodeSupported(op opcode) bool {
	switch op {
	case opLoadConst,
		opNewTable,
		opSetField,
		opGetField,
		opSetStringField,
		opSetRowStringField,
		opGetStringField,
		opGetRowStringField,
		opAddStringField,
		opSubStringField,
		opSubAddStringField,
		opSetIndex,
		opClosure,
		opPrepareIter,
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
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfModKNotEqualK,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldFalse,
		opMathMin,
		opJumpIfFalse,
		opCall,
		opCallTableFieldKeyOne,
		opJump,
		opReturnOne,
		opReturn:
		return true
	default:
		return false
	}
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
	case opAddStringField, opSubStringField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
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
		if err := verifyStringConstant(proto, ins.c); err != nil {
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
	case opJumpIfStringFieldFalse:
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
	for _, constant := range []int{desc.field, desc.value} {
		if err := verifyConstant(proto, constant); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, constant); err != nil {
			return err
		}
	}
	if desc.slot < -1 {
		return fmt.Errorf("row field equality descriptor %d has invalid slot %d", index, desc.slot)
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
		lines = append(lines, fmt.Sprintf(
			"intrinsic pc%d %s r%d args %d results %d",
			intrinsic.pc,
			opcodeName(intrinsic.op),
			intrinsic.base,
			intrinsic.args,
			intrinsic.results,
		))
	}
	return lines
}

func nativeFuncName(nativeID nativeFuncID) string {
	switch nativeID {
	case nativeFuncMathMin:
		return "MATH_MIN"
	case nativeFuncArrayNext:
		return "ARRAY_NEXT"
	case nativeFuncRawLen:
		return "RAW_LEN"
	case nativeFuncTableInsert:
		return "TABLE_INSERT"
	case nativeFuncTableRemove:
		return "TABLE_REMOVE"
	case nativeFuncCoroutineResume:
		return "COROUTINE_RESUME"
	default:
		return "UNKNOWN"
	}
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
	case opGetStringField:
		return "GET_STRING_FIELD"
	case opGetRowStringField:
		return "GET_ROW_STRING_FIELD"
	case opGetStringField2:
		return "GET_STRING_FIELD2"
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
	case opJumpIfModKNotEqualK:
		return "JUMP_IF_MOD_K_NOT_EQUAL_K"
	case opJumpIfStringFieldNotEqualK:
		return "JUMP_IF_STRING_FIELD_NOT_EQUAL_K"
	case opJumpIfRowStringFieldNotEqualK:
		return "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K"
	case opJumpIfStringFieldNotGreaterK:
		return "JUMP_IF_STRING_FIELD_NOT_GREATER_K"
	case opJumpIfStringFieldGreaterK:
		return "JUMP_IF_STRING_FIELD_GREATER_K"
	case opJumpIfStringFieldFalse:
		return "JUMP_IF_STRING_FIELD_FALSE"
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
	case opGetStringField:
		return fmt.Sprintf("GET_STRING_FIELD r%d r%d %s", ins.a, ins.b, disassembleConstant(proto, ins.c))
	case opGetRowStringField:
		return fmt.Sprintf("GET_ROW_STRING_FIELD r%d r%d %s slot %d", ins.a, ins.b, disassembleConstant(proto, ins.c), ins.d)
	case opGetStringField2:
		return fmt.Sprintf("GET_STRING_FIELD2 r%d r%d %s %s", ins.a, ins.b, disassembleConstant(proto, ins.c), disassembleConstant(proto, ins.d))
	case opAddStringField:
		return fmt.Sprintf("ADD_STRING_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
	case opSubStringField:
		return fmt.Sprintf("SUB_STRING_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
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
	case opJumpIfModKNotEqualK:
		return fmt.Sprintf("JUMP_IF_MOD_K_NOT_EQUAL_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfStringFieldNotEqualK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_EQUAL_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfRowStringFieldNotEqualK:
		if ins.b < 0 || ins.b >= len(proto.rowFieldEqualOps) {
			return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K r%d descriptor %d %d", ins.a, ins.b, ins.d)
		}
		desc := proto.rowFieldEqualOps[ins.b]
		return fmt.Sprintf("JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K r%d %s %s slot %d %d", ins.a, disassembleConstant(proto, desc.field), disassembleConstant(proto, desc.value), desc.slot, ins.d)
	case opJumpIfStringFieldNotGreaterK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_NOT_GREATER_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfStringFieldGreaterK:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_GREATER_K r%d %s %s %d", ins.a, disassembleConstant(proto, ins.b), disassembleConstant(proto, ins.c), ins.d)
	case opJumpIfStringFieldFalse:
		return fmt.Sprintf("JUMP_IF_STRING_FIELD_FALSE r%d %s slot %d %d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
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
