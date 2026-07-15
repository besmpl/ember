package ember

import (
	"fmt"
	"sort"
)

// codeImage is the immutable, owner-neutral form consumed by the compact
// scalar Machine. It contains no runtime owner, host capability, or mutable
// inline cache.
type codeImage struct {
	operations   []machineOperation
	constants    []machineConstant
	blocks       []machineBlock
	registers    int
	maxResults   int
	eligible     bool
	rejectReason string
	sourceName   string
	functionName string
}

type machineOperation struct {
	op          opcode
	guestCharge uint8
	errorClass  opcodeMachineErrorClass
	a           int32
	b           int32
	c           int32
	d           int32
	wordPC      int32
	line        int32
}

type machineConstant struct {
	kind ValueKind
	bits uint64
}

type machineBlock struct {
	first int32
	last  int32
}

func (proto *Proto) preparedCodeImage() (*codeImage, error) {
	if proto == nil {
		return nil, fmt.Errorf("prepare code image: nil prototype")
	}
	proto.codeImageOnce.Do(func() {
		proto.codeImage, proto.codeImageErr = prepareCodeImage(proto)
	})
	return proto.codeImage, proto.codeImageErr
}

func prepareCodeImage(proto *Proto) (*codeImage, error) {
	if proto == nil {
		return nil, fmt.Errorf("prepare code image: nil prototype")
	}
	if proto.verifyErr != nil {
		return nil, fmt.Errorf("prepare code image: invalid prototype: %w", proto.verifyErr)
	}
	if err := verifyProto(proto); err != nil {
		return nil, fmt.Errorf("prepare code image: invalid prototype: %w", err)
	}

	image := &codeImage{
		registers: proto.registers,
		eligible:  true,
	}
	if proto.debugInfo != nil {
		image.sourceName = proto.debugInfo.sourceName
		image.functionName = proto.debugInfo.functionName
	}
	if proto.params != 0 || proto.variadic || len(proto.prototypes) != 0 || len(proto.upvalues) != 0 {
		image.reject("prototype is not a parameterless leaf without upvalues")
	}

	image.constants = make([]machineConstant, len(proto.constants))
	for index, value := range proto.constants {
		kind := valueKind(value)
		descriptor := machineConstant{kind: kind}
		switch kind {
		case NilKind:
		case BoolKind:
			if valueBool(value) {
				descriptor.bits = 1
			}
		case NumberKind:
			descriptor.bits = value.bits
		default:
			image.reject(fmt.Sprintf("constant %d has unsupported kind %s", index, kind))
		}
		image.constants[index] = descriptor
	}

	decodedWords, _, err := wordcodeDecodeWords(proto.words, proto.cacheIndex)
	if err != nil {
		return nil, fmt.Errorf("prepare code image: %w", err)
	}
	code, err := decodeWordcode(proto.words, proto.cacheIndex)
	if err != nil {
		return nil, fmt.Errorf("prepare code image: %w", err)
	}
	if len(decodedWords) != len(code) {
		return nil, fmt.Errorf("prepare code image: decoded word count %d does not match instruction count %d", len(decodedWords), len(code))
	}
	if len(code) == 0 {
		image.reject("prototype has no executable instructions")
	}

	image.operations = make([]machineOperation, len(code))
	hasReturn := false
	for pc, ins := range code {
		meta, ok := opcodeMetadata(ins.op)
		if !ok {
			return nil, fmt.Errorf("prepare code image: instruction %d has unknown opcode %d", pc, ins.op)
		}
		if !meta.machine.eligible {
			image.reject(fmt.Sprintf("instruction %d uses unsupported opcode %s", pc, opcodeName(ins.op)))
		}
		if ins.op == opReturn && ins.b < 0 {
			image.reject(fmt.Sprintf("instruction %d uses an open return", pc))
		}
		if err := validateMachineRegisters(ins, proto.registers); err != nil {
			return nil, fmt.Errorf("prepare code image: instruction %d %s: %w", pc, opcodeName(ins.op), err)
		}
		line := protoLineAt(proto, decodedWords[pc].wordPC)
		image.operations[pc] = machineOperation{
			op:          ins.op,
			guestCharge: meta.machine.guestCharge,
			errorClass:  meta.machine.errorClass,
			a:           int32(ins.a),
			b:           int32(ins.b),
			c:           int32(ins.c),
			d:           int32(ins.d),
			wordPC:      int32(decodedWords[pc].wordPC),
			line:        int32(line),
		}
		switch ins.op {
		case opReturnOne:
			hasReturn = true
			if image.maxResults < 1 {
				image.maxResults = 1
			}
		case opReturn:
			hasReturn = true
			if ins.b > image.maxResults {
				image.maxResults = ins.b
			}
		}
	}
	if !hasReturn {
		image.reject("prototype has no fixed return")
	}
	image.blocks = machineBlocks(code)
	return image, nil
}

func (image *codeImage) reject(reason string) {
	if image == nil || !image.eligible {
		return
	}
	image.eligible = false
	image.rejectReason = reason
}

func validateMachineRegisters(ins instruction, registerCount int) error {
	for _, access := range []instructionRegisterAccess{instructionRegisterRead, instructionRegisterWrite} {
		registers := instructionRegistersBounded(ins, access, registerCount)
		for register, ok := registers.next(); ok; register, ok = registers.next() {
			if register < 0 || register >= registerCount {
				return fmt.Errorf("%s register %d out of range 0..%d", access, register, registerCount-1)
			}
		}
	}
	return nil
}

func machineBlocks(code []instruction) []machineBlock {
	if len(code) == 0 {
		return nil
	}
	leaders := map[int]bool{0: true}
	for pc, ins := range code {
		meta, _ := opcodeMetadata(ins.op)
		if target, ok := instructionJumpTarget(ins); ok && target >= 0 && target < len(code) {
			leaders[target] = true
		}
		if meta.controlFlow != opcodeControlNone && pc+1 < len(code) {
			leaders[pc+1] = true
		}
	}
	starts := make([]int, 0, len(leaders))
	for leader := range leaders {
		starts = append(starts, leader)
	}
	sort.Ints(starts)
	blocks := make([]machineBlock, len(starts))
	for index, first := range starts {
		last := len(code)
		if index+1 < len(starts) {
			last = starts[index+1]
		}
		blocks[index] = machineBlock{first: int32(first), last: int32(last)}
	}
	return blocks
}

func (image *codeImage) scriptFrame(operation machineOperation) ScriptFrame {
	return ScriptFrame{
		Source:   image.sourceName,
		Function: image.functionName,
		Line:     int(operation.line),
	}
}
