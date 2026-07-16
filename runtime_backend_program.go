package ember

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
)

const (
	backendPreparedABIVersion      uint32 = 1
	backendPreparedSemanticVersion uint32 = 1
)

type backendProgramIR struct {
	abiVersion      uint32
	semanticVersion uint32
	programHash     [sha256.Size]byte
	modules         []backendModuleIR
	entrypoints     []backendEntrypointIR
	globalNames     []string
}

type backendModuleIR struct {
	moduleID   programModuleID
	key        moduleKey
	sourceName string
	code       *codeImage
	protos     []*backendProtoIR
}

type backendEntrypointIR struct {
	name     string
	moduleID programModuleID
}

func buildBackendProgramIR(image *programImage) (*backendProgramIR, error) {
	if image == nil {
		return nil, fmt.Errorf("build backend program IR: nil Program image")
	}
	ir := &backendProgramIR{
		abiVersion:      backendPreparedABIVersion,
		semanticVersion: backendPreparedSemanticVersion,
		modules:         make([]backendModuleIR, len(image.modules)),
		entrypoints:     make([]backendEntrypointIR, len(image.entrypoints)),
		globalNames:     append([]string(nil), image.globalNames...),
	}
	for moduleIndex := range image.modules {
		source := &image.modules[moduleIndex]
		if source.code == nil {
			return nil, fmt.Errorf("build backend program IR: module %d has no CodeImage", moduleIndex)
		}
		if !source.code.eligible {
			return nil, fmt.Errorf("build backend program IR: module %d is ineligible: %s", moduleIndex, source.code.rejectReason)
		}
		module := &ir.modules[moduleIndex]
		module.moduleID = source.moduleID
		module.key = source.key
		module.sourceName = source.sourceName
		module.code = source.code
		module.protos = make([]*backendProtoIR, len(source.code.prototypes))
		for protoIndex := range source.code.prototypes {
			protoIR, err := buildBackendProtoIRWithStrings(
				&source.code.prototypes[protoIndex],
				source.code.stringRecords,
				source.code.stringData,
			)
			if err != nil {
				return nil, fmt.Errorf("build backend program IR: module %d Proto %d: %w", moduleIndex, protoIndex, err)
			}
			module.protos[protoIndex] = protoIR
		}
	}
	for entrypointIndex, source := range image.entrypoints {
		ir.entrypoints[entrypointIndex] = backendEntrypointIR{
			name:     source.name,
			moduleID: source.moduleID,
		}
	}
	ir.programHash = hashBackendProgramIR(ir)
	if err := verifyBackendProgramIR(ir); err != nil {
		return nil, err
	}
	return ir, nil
}

func verifyBackendProgramIR(ir *backendProgramIR) error {
	if ir == nil {
		return fmt.Errorf("verify backend program IR: nil IR")
	}
	if ir.abiVersion != backendPreparedABIVersion ||
		ir.semanticVersion != backendPreparedSemanticVersion ||
		len(ir.modules) == 0 ||
		len(ir.entrypoints) == 0 {
		return fmt.Errorf("verify backend program IR: invalid inventory or version")
	}
	for moduleIndex := range ir.modules {
		module := &ir.modules[moduleIndex]
		if module.moduleID != programModuleID(moduleIndex) ||
			module.key.String() == "" ||
			module.code == nil ||
			!module.code.eligible ||
			len(module.protos) != len(module.code.prototypes) {
			return fmt.Errorf("verify backend program IR: module %d has invalid inventory", moduleIndex)
		}
		if moduleIndex > 0 && ir.modules[moduleIndex-1].key.String() >= module.key.String() {
			return fmt.Errorf("verify backend program IR: module inventory is not strictly ordered")
		}
		for protoIndex := range module.protos {
			protoIR := module.protos[protoIndex]
			source := &module.code.prototypes[protoIndex]
			if err := verifyBackendProtoIR(protoIR); err != nil {
				return fmt.Errorf("verify backend program IR: module %d Proto %d: %w", moduleIndex, protoIndex, err)
			}
			if err := verifyBackendProtoMapping(protoIR, source, len(module.protos)); err != nil {
				return fmt.Errorf("verify backend program IR: module %d Proto %d: %w", moduleIndex, protoIndex, err)
			}
		}
	}
	for entrypointIndex, entrypoint := range ir.entrypoints {
		if entrypoint.name == "" || int(entrypoint.moduleID) >= len(ir.modules) {
			return fmt.Errorf("verify backend program IR: entrypoint %d has invalid inventory", entrypointIndex)
		}
		for prior := 0; prior < entrypointIndex; prior++ {
			if ir.entrypoints[prior].name == entrypoint.name {
				return fmt.Errorf("verify backend program IR: duplicate entrypoint %q", entrypoint.name)
			}
		}
	}
	for nameIndex, name := range ir.globalNames {
		if name == "" || nameIndex > 0 && ir.globalNames[nameIndex-1] >= name {
			return fmt.Errorf("verify backend program IR: global name inventory is not strictly ordered")
		}
	}
	if got := hashBackendProgramIR(ir); got != ir.programHash {
		return fmt.Errorf("verify backend program IR: program hash mismatch")
	}
	return nil
}

func verifyBackendProtoMapping(ir *backendProtoIR, source *machineProto, protoCount int) error {
	if ir == nil || source == nil ||
		ir.registers != source.registers ||
		ir.params != source.params ||
		ir.variadic != source.variadic ||
		ir.maxResults != source.maxResults ||
		ir.detachable != source.detachable ||
		ir.requiresOwner != source.requiresOwner ||
		ir.requiresNumericCoercion != source.requiresNumericCoercion ||
		ir.requiresGeneratedStrings != source.requiresGeneratedStrings ||
		ir.sourceName != source.sourceName ||
		ir.functionName != source.functionName ||
		len(ir.constants) != len(source.constants) ||
		len(ir.upvalues) != len(source.upvalues) ||
		len(ir.blocks) != len(source.blocks) ||
		len(ir.ops) != len(source.operations) {
		return fmt.Errorf("invalid CodeImage mapping")
	}
	for constantIndex := range ir.constants {
		if ir.constants[constantIndex] != source.constants[constantIndex] {
			return fmt.Errorf("constant %d mismatches CodeImage", constantIndex)
		}
	}
	for upvalueIndex := range ir.upvalues {
		if ir.upvalues[upvalueIndex] != source.upvalues[upvalueIndex] {
			return fmt.Errorf("upvalue %d mismatches CodeImage", upvalueIndex)
		}
	}
	for blockIndex := range ir.blocks {
		if ir.blocks[blockIndex].first != source.blocks[blockIndex].first ||
			ir.blocks[blockIndex].last != source.blocks[blockIndex].last {
			return fmt.Errorf("block %d mismatches CodeImage", blockIndex)
		}
	}
	for pc := range ir.ops {
		if !backendOperationMatchesMachine(&ir.ops[pc], &source.operations[pc]) {
			return fmt.Errorf("PC %d mismatches CodeImage", pc)
		}
		if ir.ops[pc].op == opClosure &&
			(ir.ops[pc].targetProto <= 0 || int(ir.ops[pc].targetProto) >= protoCount) {
			return fmt.Errorf("PC %d targets invalid Proto %d", pc, ir.ops[pc].targetProto)
		}
	}
	for valueIndex := range ir.values {
		for _, targetProto := range ir.values[valueIndex].targetProtos {
			if targetProto <= 0 || int(targetProto) >= protoCount {
				return fmt.Errorf("SSA value %d carries invalid target Proto fact %d", valueIndex+1, targetProto)
			}
		}
	}
	return nil
}

func backendOperationMatchesMachine(operation *backendOperationIR, source *machineOperation) bool {
	if operation == nil || source == nil {
		return false
	}
	targetPC := int32(-1)
	switch opcodeJumpTarget(source.op) {
	case opcodeJumpTargetB:
		targetPC = source.b
	case opcodeJumpTargetD:
		targetPC = source.d
	}
	return operation.op == source.op &&
		operation.wordPC == source.wordPC &&
		operation.line == source.line &&
		operation.targetPC == targetPC &&
		operation.a == source.a &&
		operation.b == source.b &&
		operation.c == source.c &&
		operation.d == source.d &&
		operation.targetProto == source.targetProto &&
		operation.callArgStart == source.callArgStart &&
		operation.callArgCount == source.callArgCount &&
		operation.callPrefix == source.callPrefix &&
		operation.callResults == source.callResults &&
		operation.returnCount == source.returnCount &&
		operation.tailCall == source.tailCall &&
		operation.globalIndex == source.globalIndex &&
		operation.nativeID == source.nativeID &&
		operation.guardField == source.guardField &&
		operation.guestCharge == source.guestCharge &&
		operation.tailCharge == source.tailCharge &&
		operation.errorClass == source.errorClass
}

func hashBackendProgramIR(ir *backendProgramIR) [sha256.Size]byte {
	digest := sha256.New()
	backendHashString(digest, "ember-program-image-v1")
	backendHashUint64(digest, uint64(len(ir.modules)))
	for moduleIndex := range ir.modules {
		module := &ir.modules[moduleIndex]
		backendHashUint32(digest, uint32(module.moduleID))
		backendHashString(digest, string(module.key.kind))
		backendHashString(digest, module.key.path)
		backendHashString(digest, module.sourceName)
		backendHashCodeImage(digest, module.code)
	}
	backendHashUint64(digest, uint64(len(ir.entrypoints)))
	for _, entrypoint := range ir.entrypoints {
		backendHashString(digest, entrypoint.name)
		backendHashUint32(digest, uint32(entrypoint.moduleID))
	}
	backendHashUint64(digest, uint64(len(ir.globalNames)))
	for _, name := range ir.globalNames {
		backendHashString(digest, name)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func backendHashCodeImage(digest hash.Hash, image *codeImage) {
	if image == nil {
		backendHashUint8(digest, 0)
		return
	}
	backendHashUint8(digest, 1)
	backendHashInt(digest, image.registers)
	backendHashInt(digest, image.maxResults)
	backendHashBool(digest, image.eligible)
	backendHashBool(digest, image.detachable)
	backendHashBool(digest, image.requiresOwner)
	backendHashBool(digest, image.requiresNumericCoercion)
	backendHashBool(digest, image.requiresGeneratedStrings)
	backendHashString(digest, image.rejectReason)
	backendHashString(digest, image.sourceName)
	backendHashString(digest, image.functionName)
	backendHashUint64(digest, uint64(len(image.stringRecords)))
	for _, record := range image.stringRecords {
		backendHashUint32(digest, record.offset)
		backendHashUint32(digest, record.length)
		backendHashUint64(digest, record.hash)
	}
	backendHashBytes(digest, image.stringData)
	backendHashUint64(digest, uint64(len(image.globalNames)))
	for _, name := range image.globalNames {
		backendHashUint32(digest, uint32(name))
	}
	backendHashUint64(digest, uint64(len(image.prototypes)))
	for protoIndex := range image.prototypes {
		backendHashMachineProto(digest, &image.prototypes[protoIndex])
	}
}

func backendHashMachineProto(digest hash.Hash, proto *machineProto) {
	backendHashInt(digest, proto.registers)
	backendHashInt(digest, proto.params)
	backendHashBool(digest, proto.variadic)
	backendHashInt(digest, proto.maxResults)
	backendHashBool(digest, proto.eligible)
	backendHashBool(digest, proto.detachable)
	backendHashBool(digest, proto.requiresOwner)
	backendHashBool(digest, proto.requiresNumericCoercion)
	backendHashBool(digest, proto.requiresGeneratedStrings)
	backendHashString(digest, proto.rejectReason)
	backendHashString(digest, proto.sourceName)
	backendHashString(digest, proto.functionName)
	backendHashUint64(digest, uint64(len(proto.constants)))
	for _, constant := range proto.constants {
		backendHashUint8(digest, uint8(constant.kind))
		backendHashUint64(digest, constant.bits)
	}
	backendHashUint64(digest, uint64(len(proto.upvalues)))
	for _, upvalue := range proto.upvalues {
		backendHashUint32(digest, upvalue.index)
		backendHashUint8(digest, upvalue.local)
		backendHashUint8(digest, upvalue.copy)
	}
	backendHashUint64(digest, uint64(len(proto.blocks)))
	for _, block := range proto.blocks {
		backendHashInt32(digest, block.first)
		backendHashInt32(digest, block.last)
	}
	backendHashUint64(digest, uint64(len(proto.operations)))
	for operationIndex := range proto.operations {
		backendHashMachineOperation(digest, &proto.operations[operationIndex])
	}
}

func backendHashMachineOperation(digest hash.Hash, operation *machineOperation) {
	backendHashUint8(digest, uint8(operation.op))
	backendHashUint8(digest, operation.guestCharge)
	backendHashUint8(digest, operation.tailCharge)
	backendHashUint8(digest, uint8(operation.errorClass))
	backendHashInt32(digest, operation.a)
	backendHashInt32(digest, operation.b)
	backendHashInt32(digest, operation.c)
	backendHashInt32(digest, operation.d)
	backendHashInt32(digest, operation.wordPC)
	backendHashInt32(digest, operation.line)
	backendHashInt32(digest, operation.targetProto)
	backendHashInt32(digest, operation.callArgStart)
	backendHashInt32(digest, operation.callArgCount)
	backendHashInt32(digest, operation.callPrefix)
	backendHashInt32(digest, operation.callResults)
	backendHashInt32(digest, operation.returnCount)
	backendHashUint8(digest, operation.tailCall)
	backendHashInt32(digest, operation.globalIndex)
	backendHashInt32(digest, operation.nativeID)
	backendHashUint32(digest, uint32(operation.guardField))
}

func backendHashBool(digest hash.Hash, value bool) {
	if value {
		backendHashUint8(digest, 1)
		return
	}
	backendHashUint8(digest, 0)
}

func backendHashInt(digest hash.Hash, value int) {
	backendHashUint64(digest, uint64(value))
}

func backendHashInt32(digest hash.Hash, value int32) {
	backendHashUint32(digest, uint32(value))
}

func backendHashUint8(digest hash.Hash, value uint8) {
	_, _ = digest.Write([]byte{value})
}

func backendHashUint32(digest hash.Hash, value uint32) {
	var buffer [4]byte
	binary.LittleEndian.PutUint32(buffer[:], value)
	_, _ = digest.Write(buffer[:])
}

func backendHashUint64(digest hash.Hash, value uint64) {
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], value)
	_, _ = digest.Write(buffer[:])
}

func backendHashString(digest hash.Hash, value string) {
	backendHashBytes(digest, []byte(value))
}

func backendHashBytes(digest hash.Hash, value []byte) {
	backendHashUint64(digest, uint64(len(value)))
	_, _ = digest.Write(value)
}
