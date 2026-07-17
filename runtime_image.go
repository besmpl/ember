package ember

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// codeImage is the immutable, owner-neutral form consumed by the compact
// scalar Machine. It contains no runtime owner, host capability, or mutable
// inline cache.
type codeImage struct {
	operations               []machineOperation
	constants                []machineConstant
	blocks                   []machineBlock
	prototypes               []machineProto
	registers                int
	maxResults               int
	eligible                 bool
	detachable               bool
	requiresOwner            bool
	requiresNumericCoercion  bool
	requiresGeneratedStrings bool
	rejectReason             string
	sourceName               string
	functionName             string
	stringRecords            []machineStringRecord
	stringData               []byte
	globalNames              []machineStringID
}

// machineProto is one immutable executable function in a CodeImage. Proto
// identity is represented by its position in codeImage.prototypes; no runtime
// pointer is retained in the image or in a continuation.
type machineProto struct {
	operations               []machineOperation
	constants                []machineConstant
	upvalues                 []machineUpvalue
	blocks                   []machineBlock
	registers                int
	params                   int
	variadic                 bool
	maxResults               int
	eligible                 bool
	detachable               bool
	requiresOwner            bool
	requiresNumericCoercion  bool
	requiresGeneratedStrings bool
	rejectReason             string
	sourceName               string
	functionName             string
}

type machineUpvalue struct {
	index uint32
	local uint8
	copy  uint8
}

type machineOperation struct {
	op           opcode
	guestCharge  uint8
	tailCharge   uint8
	errorClass   opcodeMachineErrorClass
	a            int32
	b            int32
	c            int32
	d            int32
	wordPC       int32
	line         int32
	targetProto  int32
	callArgStart int32
	callArgCount int32
	callPrefix   int32
	callResults  int32
	returnCount  int32
	tailCall     uint8
	globalIndex  int32
	nativeID     int32
	guardField   machineStringID
}

type machineConstant struct {
	kind  ValueKind
	flags uint8
	bits  uint64
}

const (
	machineConstantFlagIndexName    uint8 = 1 << 0
	machineConstantFlagNewIndexName uint8 = 1 << 1
)

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

	builder := machineImageBuilder{
		ids:    make(map[*Proto]int32),
		active: make(map[*Proto]bool),
	}
	if _, err := builder.add(proto); err != nil {
		return nil, err
	}
	root := builder.prototypes[0]
	image := &codeImage{
		operations:               root.operations,
		constants:                root.constants,
		blocks:                   root.blocks,
		prototypes:               builder.prototypes,
		registers:                root.registers,
		maxResults:               root.maxResults,
		eligible:                 root.eligible,
		detachable:               root.detachable,
		requiresOwner:            root.requiresOwner,
		requiresNumericCoercion:  root.requiresNumericCoercion,
		requiresGeneratedStrings: root.requiresGeneratedStrings,
		rejectReason:             root.rejectReason,
		stringRecords:            builder.strings.records,
		stringData:               builder.strings.data,
		globalNames:              builder.globalNames,
	}
	if proto.debugInfo != nil {
		image.sourceName = proto.debugInfo.sourceName
		image.functionName = proto.debugInfo.functionName
	}
	for index, prepared := range builder.prototypes {
		if prepared.registers < 0 {
			image.reject(fmt.Sprintf("prototype %d has negative register count", index))
		}
		if prepared.maxResults > image.maxResults {
			image.maxResults = prepared.maxResults
		}
		if !prepared.eligible {
			image.reject(fmt.Sprintf("prototype %d: %s", index, prepared.rejectReason))
		}
		if prepared.requiresOwner {
			image.requiresOwner = true
		}
		if prepared.requiresNumericCoercion {
			image.requiresNumericCoercion = true
		}
		if prepared.requiresGeneratedStrings {
			image.requiresGeneratedStrings = true
		}
	}
	if image.requiresGeneratedStrings {
		image.requiresOwner = true
		image.detachable = false
	}
	return image, nil
}

type machineImageBuilder struct {
	ids                map[*Proto]int32
	active             map[*Proto]bool
	prototypes         []machineProto
	strings            machineStringArena
	hasStringConstant  bool
	hasUnprovenNumeric bool
	globalNames        []machineStringID
	globalSlots        map[string]int32
}

func (builder *machineImageBuilder) add(proto *Proto) (int32, error) {
	if proto == nil {
		return 0, fmt.Errorf("prepare code image: nil nested prototype")
	}
	if id, ok := builder.ids[proto]; ok {
		if builder.active[proto] {
			return 0, fmt.Errorf("prepare code image: cyclic prototype graph")
		}
		return id, nil
	}
	id := int32(len(builder.prototypes))
	builder.ids[proto] = id
	builder.prototypes = append(builder.prototypes, machineProto{})
	builder.active[proto] = true
	prepared, err := builder.prepare(proto, id)
	delete(builder.active, proto)
	if err != nil {
		return 0, err
	}
	builder.prototypes[id] = prepared
	return id, nil
}

func (builder *machineImageBuilder) prepare(proto *Proto, id int32) (machineProto, error) {
	if proto.verifyErr != nil {
		return machineProto{}, fmt.Errorf("prepare code image: invalid prototype: %w", proto.verifyErr)
	}
	if err := verifyProto(proto); err != nil {
		return machineProto{}, fmt.Errorf("prepare code image: invalid prototype: %w", err)
	}
	prepared := machineProto{
		registers:  proto.registers,
		params:     proto.params,
		variadic:   proto.variadic,
		eligible:   true,
		detachable: true,
	}
	prepared.upvalues = make([]machineUpvalue, len(proto.upvalues))
	for index, descriptor := range proto.upvalues {
		if descriptor.index < 0 || uint64(descriptor.index) > math.MaxUint32 {
			return machineProto{}, fmt.Errorf("prepare code image: upvalue %d index is out of range", index)
		}
		prepared.upvalues[index].index = uint32(descriptor.index)
		if descriptor.local {
			prepared.upvalues[index].local = 1
		}
		if descriptor.copy {
			prepared.upvalues[index].copy = 1
		}
	}
	if proto.debugInfo != nil {
		prepared.sourceName = proto.debugInfo.sourceName
		prepared.functionName = proto.debugInfo.functionName
	}
	prepared.constants = make([]machineConstant, len(proto.constants))
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
		case StringKind:
			builder.hasStringConstant = true
			text := value.stringText()
			if text == "__index" {
				descriptor.flags |= machineConstantFlagIndexName
			}
			if text == "__newindex" {
				descriptor.flags |= machineConstantFlagNewIndexName
			}
			id, err := builder.internString(text)
			if err != nil {
				return machineProto{}, fmt.Errorf("prepare code image: constant %d string: %w", index, err)
			}
			descriptor.bits = uint64(id)
		default:
			prepared.reject(fmt.Sprintf("constant %d has unsupported kind %s", index, kind))
		}
		prepared.constants[index] = descriptor
	}
	decodedWords, _, err := wordcodeDecodeWords(proto.words, proto.cacheIndex)
	if err != nil {
		return machineProto{}, fmt.Errorf("prepare code image: %w", err)
	}
	code, err := decodeWordcode(proto.words, proto.cacheIndex)
	if err != nil {
		return machineProto{}, fmt.Errorf("prepare code image: %w", err)
	}
	if len(decodedWords) != len(code) {
		return machineProto{}, fmt.Errorf("prepare code image: decoded word count %d does not match instruction count %d", len(decodedWords), len(code))
	}
	if len(code) == 0 {
		return machineProto{}, fmt.Errorf("prepare code image: prototype has no executable instructions")
	}
	numericFacts := detectNumericOperandFactPCs(proto)
	for pc, ins := range code {
		if machineOperationMayCoerceNumericString(ins.op) && (pc >= len(numericFacts) || !numericFacts[pc]) {
			builder.hasUnprovenNumeric = true
			prepared.requiresNumericCoercion = true
		}
		if ins.op == opConcat || ins.op == opConcatChain ||
			(ins.op == opFastCall && nativeFuncID(ins.b) == nativeFuncToString) {
			prepared.requiresGeneratedStrings = true
		}
	}
	if machineUsesGeneratedStringHelper(proto, code) {
		prepared.requiresGeneratedStrings = true
	}
	if reason := machineCoroutineStaticRejectReason(proto, code); reason != "" {
		prepared.reject(reason)
	}
	for _, child := range proto.prototypes {
		if _, err := builder.add(child); err != nil {
			return machineProto{}, err
		}
	}
	prepared.operations = make([]machineOperation, len(code))
	closureRegisters := make(map[int]bool)
	tableRegisters := make(map[int]bool)
	hasClosureTable := false
	for pc, ins := range code {
		meta, ok := opcodeMetadata(ins.op)
		if !ok {
			return machineProto{}, fmt.Errorf("prepare code image: instruction %d has unknown opcode %d", pc, ins.op)
		}
		operation := machineOperation{
			op:          ins.op,
			guestCharge: meta.machine.guestCharge,
			errorClass:  meta.machine.errorClass,
			a:           int32(ins.a),
			b:           int32(ins.b),
			c:           int32(ins.c),
			d:           int32(ins.d),
			wordPC:      int32(decodedWords[pc].wordPC),
			line:        int32(protoLineAt(proto, decodedWords[pc].wordPC)),
		}
		if !meta.machine.eligible {
			prepared.reject(fmt.Sprintf("instruction %d uses unsupported opcode %s", pc, opcodeName(ins.op)))
		}
		if err := validateMachineRegisters(ins, proto.registers); err != nil {
			return machineProto{}, fmt.Errorf("prepare code image: instruction %d %s: %w", pc, opcodeName(ins.op), err)
		}
		switch ins.op {
		case opClosure:
			if ins.b < 0 || ins.b >= len(proto.prototypes) || proto.prototypes[ins.b] == nil {
				prepared.reject(fmt.Sprintf("instruction %d has invalid closure prototype", pc))
				break
			}
			target, err := builder.add(proto.prototypes[ins.b])
			if err != nil {
				return machineProto{}, err
			}
			operation.targetProto = target
			closureRegisters[ins.a] = true
		case opLoadGlobal, opSetGlobal:
			if ins.c < 0 || ins.c >= len(proto.globalNames) {
				return machineProto{}, fmt.Errorf("prepare code image: instruction %d has invalid global slot %d", pc, ins.c)
			}
			if ins.op == opLoadGlobal && machineUnsupportedBaseGlobal(proto.globalNames[ins.c]) {
				prepared.reject(fmt.Sprintf("instruction %d loads unsupported base global %q", pc, proto.globalNames[ins.c]))
			}
			globalIndex, err := builder.internGlobal(proto.globalNames[ins.c])
			if err != nil {
				return machineProto{}, fmt.Errorf("prepare code image: instruction %d global: %w", pc, err)
			}
			operation.globalIndex = globalIndex
			prepared.requiresOwner = true
		case opNewTable:
			tableRegisters[ins.a] = true
		case opSetField, opSetStringField, opSetIndex:
			if closureRegisters[ins.c] {
				hasClosureTable = true
			}
		case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne, opCallMethodOne:
			shape, ok := machineCallShape(ins, proto.registers)
			if !ok {
				prepared.reject(fmt.Sprintf("instruction %d has an unsupported call shape", pc))
				break
			}
			operation.callArgStart = int32(shape.argStart)
			operation.callArgCount = int32(shape.argCount)
			operation.callPrefix = int32(shape.prefixCount)
			operation.callResults = int32(shape.resultCount)
			if shape.resultCount < 0 && pc+1 < len(code) && code[pc+1].op == opReturn && code[pc+1].a == ins.a && code[pc+1].b == -1 {
				operation.tailCall = 1
				returnMeta, _ := opcodeMetadata(opReturn)
				operation.tailCharge = returnMeta.machine.guestCharge
			}
		case opFastCall:
			prepared.requiresOwner = true
			nativeID := nativeFuncID(ins.b)
			if !machineFastCallSupported(nativeID) {
				prepared.reject(fmt.Sprintf("instruction %d uses unsupported FAST_CALL native %s", pc, nativeFuncName(nativeID)))
				break
			}
			globalName, field := fastCallIntrinsicNames(nativeID)
			if globalName == "" {
				prepared.reject(fmt.Sprintf("instruction %d FAST_CALL has no scalar guard", pc))
				break
			}
			globalIndex, err := builder.internGlobal(globalName)
			if err != nil {
				return machineProto{}, fmt.Errorf("prepare code image: instruction %d FAST_CALL global: %w", pc, err)
			}
			operation.globalIndex = globalIndex
			operation.nativeID = int32(nativeID)
			if field != "" {
				fieldID, err := builder.internString(field)
				if err != nil {
					return machineProto{}, fmt.Errorf("prepare code image: instruction %d FAST_CALL field: %w", pc, err)
				}
				operation.guardField = fieldID
			}
		case opReturnOne:
			operation.returnCount = 1
			if prepared.maxResults < 1 {
				prepared.maxResults = 1
			}
		case opReturn:
			count := ins.b
			operation.returnCount = int32(count)
			if count > prepared.maxResults {
				prepared.maxResults = count
			}
		}
		prepared.operations[pc] = operation
		if ins.op == opMove {
			if closureRegisters[ins.b] {
				closureRegisters[ins.a] = true
			} else {
				delete(closureRegisters, ins.a)
			}
			if tableRegisters[ins.b] {
				tableRegisters[ins.a] = true
			} else {
				delete(tableRegisters, ins.a)
			}
		} else if ins.op != opClosure {
			writes := instructionRegistersBounded(ins, instructionRegisterWrite, proto.registers)
			for written, ok := writes.next(); ok; written, ok = writes.next() {
				delete(closureRegisters, written)
				if ins.op != opNewTable {
					delete(tableRegisters, written)
				}
			}
		}
		if id == 0 && (ins.op == opReturnOne || ins.op == opReturn) {
			count := 1
			if ins.op == opReturn {
				count = ins.b
			}
			if count > 0 {
				for register := ins.a; register < ins.a+count; register++ {
					if closureRegisters[register] {
						prepared.detachable = false
					}
					if hasClosureTable && tableRegisters[register] {
						prepared.detachable = false
					}
				}
			}
		}
	}
	// The compiler can omit RETURN from a function whose reachable control flow
	// never terminates, such as a coroutine loop that yields forever. The
	// Machine can execute that shape; an invalid fallthrough still fails at the
	// loop boundary with errCompactMachineFellOffEnd.
	prepared.blocks = machineBlocks(code)
	return prepared, nil
}

func machineFastCallSupported(nativeID nativeFuncID) bool {
	switch nativeID {
	case nativeFuncMathMin, nativeFuncRawLen, nativeFuncSelect,
		nativeFuncToString,
		nativeFuncTableInsert, nativeFuncTableRemove,
		nativeFuncSetMetatable, nativeFuncGetMetatable,
		nativeFuncCoroutineCreate, nativeFuncCoroutineStatus,
		nativeFuncCoroutineResume, nativeFuncCoroutineYield:
		return true
	default:
		return false
	}
}

func machineUnsupportedBaseGlobal(name string) bool {
	return false
}

const (
	machineCoroutineFactTable uint8 = 1 << iota
	machineCoroutineFactMember
	machineCoroutineFactConsumedTable
)

// machineCoroutineStaticRejectReason proves that the core coroutine table is
// used only as the receiver of one supported, statically named member access,
// and that the resulting function is called directly. The facts merge with
// OR at control-flow joins, so ambiguity fails closed instead of selecting the
// Machine on an unproven alias or escape.
func machineCoroutineStaticRejectReason(proto *Proto, code []instruction) string {
	if proto == nil || proto.registers <= 0 || len(code) == 0 {
		return ""
	}
	states := make([][]uint8, len(code))
	states[0] = make([]uint8, proto.registers)
	queued := make([]bool, len(code))
	queued[0] = true
	work := []int{0}
	for len(work) != 0 {
		pc := work[0]
		work = work[1:]
		queued[pc] = false
		state := append([]uint8(nil), states[pc]...)
		ins := code[pc]
		if ins.op == opSetGlobal && ins.c >= 0 && ins.c < len(proto.globalNames) && proto.globalNames[ins.c] == "coroutine" {
			return fmt.Sprintf("instruction %d rebinds the core coroutine global", pc)
		}
		field := ""
		if ins.op == opGetStringField && ins.c >= 0 && ins.c < len(proto.constants) && valueKind(proto.constants[ins.c]) == StringKind {
			field = proto.constants[ins.c].stringText()
		}
		reads := instructionRegistersBounded(ins, instructionRegisterRead, proto.registers)
		for register, ok := reads.next(); ok; register, ok = reads.next() {
			fact := state[register]
			if fact&machineCoroutineFactTable != 0 {
				if ins.op != opGetStringField || register != ins.b {
					return fmt.Sprintf("instruction %d lets the core coroutine table escape a direct field access", pc)
				}
				if !machineCoroutineFieldSupported(field) {
					return fmt.Sprintf("instruction %d accesses unsupported core coroutine field %q", pc, field)
				}
			}
			if fact&machineCoroutineFactMember != 0 && !machineCoroutineDirectCallTarget(ins, register) {
				return fmt.Sprintf("instruction %d lets a core coroutine function escape a direct call", pc)
			}
			if fact&machineCoroutineFactConsumedTable != 0 {
				return fmt.Sprintf("instruction %d reuses an aliased core coroutine table", pc)
			}
		}
		writes := instructionRegistersBounded(ins, instructionRegisterWrite, proto.registers)
		for register, ok := writes.next(); ok; register, ok = writes.next() {
			state[register] = 0
		}
		switch ins.op {
		case opLoadGlobal:
			if ins.c >= 0 && ins.c < len(proto.globalNames) && proto.globalNames[ins.c] == "coroutine" {
				state[ins.a] = machineCoroutineFactTable
			}
		case opGetStringField:
			if states[pc][ins.b]&machineCoroutineFactTable != 0 && machineCoroutineFieldSupported(field) {
				if ins.a != ins.b {
					state[ins.b] = machineCoroutineFactConsumedTable
				}
				state[ins.a] = machineCoroutineFactMember
			}
		}
		for _, successor := range instructionSuccessors(code, pc) {
			if successor < 0 || successor >= len(code) {
				continue
			}
			if states[successor] == nil {
				states[successor] = append([]uint8(nil), state...)
				if !queued[successor] {
					work = append(work, successor)
					queued[successor] = true
				}
				continue
			}
			changed := false
			for register := range state {
				merged := states[successor][register] | state[register]
				if merged != states[successor][register] {
					states[successor][register] = merged
					changed = true
				}
			}
			if changed && !queued[successor] {
				work = append(work, successor)
				queued[successor] = true
			}
		}
	}
	return ""
}

func machineCoroutineFieldSupported(field string) bool {
	switch field {
	case "create", "status", "resume", "yield":
		return true
	default:
		return false
	}
}

func machineCoroutineDirectCallTarget(ins instruction, register int) bool {
	switch ins.op {
	case opCall, opCallOne, opCallLocalOne:
		return ins.b == register
	default:
		return false
	}
}

func machineUsesGeneratedStringHelper(proto *Proto, code []instruction) bool {
	if proto == nil || proto.registers <= 0 {
		return false
	}
	tostringRegisters := make([]bool, proto.registers)
	for _, ins := range code {
		if ins.op == opFastCall && nativeFuncID(ins.b) == nativeFuncToString {
			return true
		}
		if (ins.op == opCall || ins.op == opCallOne) &&
			ins.b >= 0 && ins.b < len(tostringRegisters) && tostringRegisters[ins.b] {
			return true
		}
		switch ins.op {
		case opLoadGlobal:
			tostringRegisters[ins.a] = ins.c >= 0 && ins.c < len(proto.globalNames) && proto.globalNames[ins.c] == "tostring"
		case opMove:
			tostringRegisters[ins.a] = tostringRegisters[ins.b]
		default:
			writes := instructionRegistersBounded(ins, instructionRegisterWrite, proto.registers)
			for register, ok := writes.next(); ok; register, ok = writes.next() {
				tostringRegisters[register] = false
			}
		}
	}
	return false
}

func machineOperationMayCoerceNumericString(op opcode) bool {
	switch op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow,
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
		opNeg, opNumericForCheck, opNumericForLoop:
		return true
	default:
		return false
	}
}

// machineStaticRejectReason keeps image selection conservative where the old
// VM performs coercions the scalar kernel does not yet implement. The compact
// state walk intentionally favors false negatives for Machine selection over
// selecting a program and changing its Luau-visible result.
func machineStaticRejectReason(proto *Proto, code []instruction) string {
	if proto == nil || proto.registers <= 0 {
		return "prototype has no scalar register state"
	}
	state := make([]registerKindState, proto.registers)
	for pc, ins := range code {
		if machineInstructionNeedsStringNumberCoercion(proto, state, ins) {
			return fmt.Sprintf("instruction %d %s requires numeric string coercion", pc, opcodeName(ins.op))
		}
		fact, ok := registerKindFactForInstruction(proto, state, pc, ins)
		clearInstructionRegisterKinds(state, ins)
		if ok && fact.register >= 0 && fact.register < len(state) {
			state[fact.register] = registerKindState{kind: fact.kind, ok: true, guarded: fact.guarded}
		}
	}
	return ""
}

func machineInstructionNeedsStringNumberCoercion(proto *Proto, state []registerKindState, ins instruction) bool {
	hasString := func(register int) bool {
		kind, ok := registerKindAt(state, register)
		return ok && kind.kind == StringKind
	}
	switch ins.op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		return hasString(ins.b) || hasString(ins.c)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return hasString(ins.b) || constantHasKind(proto, ins.c, StringKind)
	case opNeg:
		return hasString(ins.b)
	case opNumericForCheck:
		return hasString(ins.a) || hasString(ins.b) || hasString(ins.c)
	case opNumericForLoop:
		return hasString(ins.a) || hasString(ins.b)
	default:
		return false
	}
}

func (builder *machineImageBuilder) internString(value string) (machineStringID, error) {
	return builder.strings.internStringStopped(value)
}

func (builder *machineImageBuilder) internGlobal(value string) (int32, error) {
	if builder.globalSlots == nil {
		builder.globalSlots = make(map[string]int32)
	}
	if index, ok := builder.globalSlots[value]; ok {
		return index, nil
	}
	if len(builder.globalNames) >= math.MaxInt32 {
		return 0, errors.New("global inventory exceeds int32")
	}
	id, err := builder.internString(value)
	if err != nil {
		return 0, err
	}
	index := int32(len(builder.globalNames))
	builder.globalSlots[value] = index
	builder.globalNames = append(builder.globalNames, id)
	return index, nil
}

func (prepared *machineProto) reject(reason string) {
	if prepared == nil || !prepared.eligible {
		return
	}
	prepared.eligible = false
	prepared.rejectReason = reason
}

type machineCallDescriptor struct {
	argStart    int
	argCount    int
	prefixCount int
	resultCount int
}

func machineCallShape(ins instruction, registers int) (machineCallDescriptor, bool) {
	shape := machineCallDescriptor{resultCount: 1}
	switch ins.op {
	case opCall:
		shape.argStart = ins.b + 1
		if prefix, marked := decodeOpenArgumentCallMarker(ins.c); marked {
			shape.argCount = -1
			shape.prefixCount = prefix
		} else if ins.c < 0 {
			shape.argCount = -1
			shape.prefixCount = -ins.c - 1
		} else {
			shape.argCount = ins.c
		}
		if count, marked := decodeFixedMultiResultCount(ins.d, registers); marked {
			shape.resultCount = count
		} else if ins.d < 0 {
			shape.resultCount = -1
		} else {
			shape.resultCount = ins.d
		}
	case opCallOne:
		shape.argCount, _ = decodeFixedCallCount(ins.c)
		shape.argStart = ins.b + 1
	case opCallLocalOne:
		shape.argCount, _ = decodeFixedCallCount(ins.d)
		shape.argStart = ins.c
	case opCallUpvalueOne:
		shape.argCount, _ = decodeFixedCallCount(ins.d)
		shape.argStart = ins.c
	case opCallMethodOne:
		explicit, _ := decodeFixedCallCount(ins.d)
		shape.argStart = ins.a + 1
		shape.argCount = explicit + 1
	default:
		return machineCallDescriptor{}, false
	}
	if shape.argStart < 0 || shape.argStart > registers || shape.resultCount < -1 {
		return machineCallDescriptor{}, false
	}
	if shape.argCount >= 0 && shape.argStart+shape.argCount > registers {
		return machineCallDescriptor{}, false
	}
	if shape.argCount < 0 && shape.argStart+shape.prefixCount >= registers {
		return machineCallDescriptor{}, false
	}
	return shape, true
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
