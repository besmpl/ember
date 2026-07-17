package ember

import "fmt"

// directShadowWord keeps the immutable physical word beside owner-local
// dispatch state. The packed form is deliberately pointer-free:
//
//	bits  0..31  original wordcode word
//	bits 32..39  generated handler id
//	bits 40..47  saturating observation counter
//	bits 48..63  sparse cache index plus one (zero means no cache)
//
// One shadow word and, in the densest case, one cache cell consume sixteen
// bytes per four-byte source word: the hard 4x owner-Program budget.
type directShadowWord uint64

const (
	directShadowHandlerShift = 32
	directShadowCounterShift = 40
	directShadowCacheShift   = 48
	directShadowByteMask     = uint64(0xff)
	directShadowCacheMask    = uint64(0xffff)
	directShadowNoCache      = 0
	directShadowMaxCache     = 1<<16 - 2
	directShadowFixedBudget  = 64 << 10
)

func newDirectShadowWord(raw wordcodeWord, handler directHandlerID, cacheIndex int) directShadowWord {
	encodedCache := uint64(directShadowNoCache)
	if cacheIndex >= 0 {
		encodedCache = uint64(cacheIndex + 1)
	}
	return directShadowWord(uint64(raw) |
		uint64(handler)<<directShadowHandlerShift |
		encodedCache<<directShadowCacheShift)
}

func (word directShadowWord) raw() wordcodeWord {
	return wordcodeWord(uint64(word))
}

func (word directShadowWord) handler() directHandlerID {
	return directHandlerID(uint64(word) >> directShadowHandlerShift & directShadowByteMask)
}

func (word directShadowWord) counter() uint8 {
	return uint8(uint64(word) >> directShadowCounterShift & directShadowByteMask)
}

func (word directShadowWord) cacheIndex() (int, bool) {
	encoded := int(uint64(word) >> directShadowCacheShift & directShadowCacheMask)
	if encoded == directShadowNoCache {
		return 0, false
	}
	return encoded - 1, true
}

func (word directShadowWord) withHandler(handler directHandlerID) directShadowWord {
	bits := uint64(word)
	bits &^= directShadowByteMask << directShadowHandlerShift
	bits |= uint64(handler) << directShadowHandlerShift
	return directShadowWord(bits)
}

func (word directShadowWord) incrementCounter() directShadowWord {
	counter := word.counter()
	if counter == ^uint8(0) {
		return word
	}
	bits := uint64(word)
	bits &^= directShadowByteMask << directShadowCounterShift
	bits |= uint64(counter+1) << directShadowCounterShift
	return directShadowWord(bits)
}

// directAdaptiveCacheCell is a pointer-free generated cache payload. The low
// byte records its generated layout; the remaining 56 bits are family-owned
// observations such as kinds, shape versions, slots, or stable small ids.
// Semantic objects remain in canonical VM state and are never retained here.
type directAdaptiveCacheCell uint64

func newDirectAdaptiveCacheCell(layout directCacheLayout) directAdaptiveCacheCell {
	return directAdaptiveCacheCell(layout)
}

func (cell directAdaptiveCacheCell) layout() directCacheLayout {
	return directCacheLayout(uint64(cell) & directShadowByteMask)
}

const directAdaptiveGuardRegisterCap = 6

const directAdaptiveStableHitThreshold = 2

func (cell directAdaptiveCacheCell) withGuardRegisters(registers []uint8) (directAdaptiveCacheCell, bool) {
	if len(registers) > directAdaptiveGuardRegisterCap {
		return cell, false
	}
	bits := uint64(cell.layout()) | uint64(len(registers))<<8
	for index, register := range registers {
		bits |= uint64(register) << (16 + index*8)
	}
	return directAdaptiveCacheCell(bits), true
}

func (cell directAdaptiveCacheCell) guardCount() int {
	return int(uint64(cell) >> 8 & directShadowByteMask)
}

func (cell directAdaptiveCacheCell) guardRegister(index int) uint8 {
	if index < 0 || index >= cell.guardCount() || index >= directAdaptiveGuardRegisterCap {
		return 0
	}
	return uint8(uint64(cell) >> (16 + index*8) & directShadowByteMask)
}

func (cell directAdaptiveCacheCell) withPlanIndex(index int) (directAdaptiveCacheCell, bool) {
	if index < 0 || uint64(index) >= (uint64(1)<<56)-1 {
		return cell, false
	}
	return directAdaptiveCacheCell(uint64(cell.layout()) | uint64(index+1)<<8), true
}

func (cell directAdaptiveCacheCell) planIndex() (int, bool) {
	encoded := uint64(cell) >> 8
	if encoded == 0 || encoded-1 > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(encoded - 1), true
}

// observeDirectFixedSelfCall quickens a call site only after the same semantic
// self-call transition has succeeded twice. The shadow retains no closure or
// Proto pointer: the specialized handler rechecks current closure identity at
// every entry and dequickens at the original word PC on a miss.
func observeDirectFixedSelfCall(instance *vmFunctionInstance, pc int) {
	if instance == nil || pc < 0 || pc >= len(instance.shadow.words) {
		return
	}
	word := instance.shadow.words[pc]
	if word.handler() != directHandlerID(opCallUpvalueOne) {
		return
	}
	word = word.incrementCounter()
	if word.counter() >= directAdaptiveStableHitThreshold {
		word = word.withHandler(directHandlerFixedSelfCall)
	}
	instance.shadow.words[pc] = word
}

type directShadowCode struct {
	words                []directShadowWord
	caches               []directAdaptiveCacheCell
	numericTraces        []directNumericTracePlan
	fixedSelfCallTraces  []directFixedSelfCallTracePlan
	compactSelfFunctions []directCompactSelfFunctionPlan
}

func (code directShadowCode) retainedBytes() int64 {
	return directShadowStateBytes(cap(code.words), cap(code.caches)) +
		int64(cap(code.numericTraces))*directNumericTracePlanBytes +
		int64(cap(code.fixedSelfCallTraces))*directFixedSelfCallTracePlanBytes +
		int64(cap(code.compactSelfFunctions))*directCompactSelfFunctionPlanBytes
}

func directShadowStateBytes(wordCount int, cacheCount int) int64 {
	if wordCount < 0 || cacheCount < 0 {
		return 0
	}
	return int64(wordCount+cacheCount) * 8
}

func directShadowStateLimit(wordCount int) int64 {
	if wordCount < 0 {
		return 0
	}
	return int64(wordCount)*4*4 + directShadowFixedBudget
}

// buildDirectShadow validates and copies physical wordcode without decoding it
// into a second semantic representation. Instruction starts receive generated
// generic handlers and sparse cache cells; AUX words remain non-executable.
func buildDirectShadow(words []wordcodeWord, table [opcodeLimit]directSemanticMetadata) (directShadowCode, error) {
	if err := validateDirectSemanticMetadata(table); err != nil {
		return directShadowCode{}, err
	}
	cacheCount := 0
	for pc := 0; pc < len(words); {
		metadata, hasAux, err := directShadowInstructionMetadata(words, pc, table)
		if err != nil {
			return directShadowCode{}, err
		}
		if metadata.cache != directCacheNone {
			cacheCount++
			if cacheCount > directShadowMaxCache+1 {
				return directShadowCode{}, fmt.Errorf("shadow cache sites exceed %d", directShadowMaxCache+1)
			}
		}
		pc++
		if hasAux {
			pc++
		}
	}
	shadow := directShadowCode{
		words:  make([]directShadowWord, len(words)),
		caches: make([]directAdaptiveCacheCell, 0, cacheCount),
	}
	for pc := 0; pc < len(words); {
		raw := words[pc]
		metadata, hasAux, err := directShadowInstructionMetadata(words, pc, table)
		if err != nil {
			return directShadowCode{}, err
		}

		cacheIndex := -1
		if metadata.cache != directCacheNone {
			cacheIndex = len(shadow.caches)
			shadow.caches = append(shadow.caches, newDirectAdaptiveCacheCell(metadata.cache))
		}
		shadow.words[pc] = newDirectShadowWord(raw, metadata.genericHandler, cacheIndex)
		pc++
		if hasAux {
			shadow.words[pc] = newDirectShadowWord(words[pc], directHandlerInvalid, -1)
			pc++
		}
	}
	if shadow.retainedBytes() > directShadowStateLimit(len(words)) {
		return directShadowCode{}, fmt.Errorf("shadow state uses %d bytes, limit is %d", shadow.retainedBytes(), directShadowStateLimit(len(words)))
	}
	return shadow, nil
}

// tileDirectNumericForTraces recognizes only canonical, effect-free numeric
// loops. It changes owner-local dispatch state, never immutable Proto wordcode.
// Every interior instruction remains independently executable for dequickening
// and diagnostic fallback.
func tileDirectNumericForTraces(proto *Proto, shadow *directShadowCode) error {
	decoded, _, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		return err
	}
	for index, entry := range decoded {
		if entry.ins.op != opNumericForCheck {
			continue
		}
		loopIndex, ok := directNumericTraceLoop(decoded, index, proto)
		if !ok || directTraceHasInteriorEntry(decoded, index, loopIndex) {
			continue
		}
		guards, ok := directNumericTraceGuardRegisters(decoded[index:loopIndex+1], proto.registers)
		if !ok {
			continue
		}
		plan, ok := buildDirectNumericTracePlan(decoded[index : loopIndex+1])
		if !ok {
			continue
		}
		word := shadow.words[entry.wordPC]
		cacheIndex, ok := word.cacheIndex()
		if !ok || cacheIndex < 0 || cacheIndex >= len(shadow.caches) {
			return fmt.Errorf("numeric trace at word %d has no cache cell", entry.wordPC)
		}
		cache := shadow.caches[cacheIndex]
		if cache.layout() != directCacheType {
			return fmt.Errorf("numeric trace at word %d has cache layout %d, want %d", entry.wordPC, cache.layout(), directCacheType)
		}
		encoded, ok := cache.withGuardRegisters(guards)
		if !ok {
			continue
		}
		shadow.numericTraces = append(shadow.numericTraces, plan)
		if shadow.retainedBytes() > directShadowStateLimit(len(proto.words)) {
			shadow.numericTraces = append([]directNumericTracePlan(nil), shadow.numericTraces[:len(shadow.numericTraces)-1]...)
			continue
		}
		shadow.caches[cacheIndex] = encoded
		shadow.words[entry.wordPC] = word.withHandler(directHandlerNumericForTrace)
	}
	return nil
}

func (code *directShadowCode) numericTraceAt(checkPC int) (*directNumericTracePlan, bool) {
	for index := range code.numericTraces {
		if int(code.numericTraces[index].checkPC) == checkPC {
			return &code.numericTraces[index], true
		}
	}
	return nil, false
}

func directNumericTraceLoop(decoded []wordcodeDecoded, checkIndex int, proto *Proto) (int, bool) {
	check := decoded[checkIndex]
	exitPC := check.nextWord + check.ins.d
	if exitPC <= check.wordPC {
		return 0, false
	}
	for index := checkIndex + 1; index < len(decoded); index++ {
		entry := decoded[index]
		if index-checkIndex+1 > directNumericForTraceInstructionCap || entry.wordPC >= exitPC {
			return 0, false
		}
		if entry.ins.op == opNumericForLoop {
			targetPC := entry.nextWord + entry.ins.d
			if targetPC != check.wordPC || entry.ins.a != check.ins.a || entry.ins.b != check.ins.c || entry.nextWord != exitPC {
				return 0, false
			}
			return index, true
		}
		if !directNumericTraceBodyInstruction(entry.ins, proto) ||
			directNumericTraceWritesController(entry.ins, check.ins, proto.registers) {
			return 0, false
		}
	}
	return 0, false
}

func directNumericTraceWritesController(ins instruction, check instruction, registerCount int) bool {
	writes := instructionRegistersBounded(ins, instructionRegisterWrite, registerCount)
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		if register == check.a || register == check.b || register == check.c {
			return true
		}
	}
	return false
}

func directNumericTraceBodyInstruction(ins instruction, proto *Proto) bool {
	switch ins.op {
	case opMove, opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg:
		return true
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return ins.c >= 0 && ins.c < len(proto.constantNumberOK) && proto.constantNumberOK[ins.c]
	default:
		return false
	}
}

func directTraceHasInteriorEntry(decoded []wordcodeDecoded, checkIndex int, loopIndex int) bool {
	startPC := decoded[checkIndex].wordPC
	loopPC := decoded[loopIndex].wordPC
	for index, entry := range decoded {
		if index >= checkIndex && index <= loopIndex {
			continue
		}
		displacement, ok := instructionJumpTarget(entry.ins)
		if !ok {
			continue
		}
		targetPC := entry.nextWord + displacement
		if targetPC > startPC && targetPC <= loopPC {
			return true
		}
	}
	return false
}

func directNumericTraceGuardRegisters(trace []wordcodeDecoded, registerCount int) ([]uint8, bool) {
	if registerCount < 0 || registerCount > 1<<8 {
		return nil, false
	}
	written := make([]bool, registerCount)
	guarded := make([]bool, registerCount)
	guards := make([]uint8, 0, directAdaptiveGuardRegisterCap)
	for _, entry := range trace {
		reads := instructionRegistersBounded(entry.ins, instructionRegisterRead, registerCount)
		for register, ok := reads.next(); ok; register, ok = reads.next() {
			if written[register] || guarded[register] {
				continue
			}
			if len(guards) == directAdaptiveGuardRegisterCap {
				return nil, false
			}
			guarded[register] = true
			guards = append(guards, uint8(register))
		}
		writes := instructionRegistersBounded(entry.ins, instructionRegisterWrite, registerCount)
		for register, ok := writes.next(); ok; register, ok = writes.next() {
			written[register] = true
		}
	}
	return guards, true
}

func directShadowInstructionMetadata(words []wordcodeWord, pc int, table [opcodeLimit]directSemanticMetadata) (directSemanticMetadata, bool, error) {
	raw := words[pc]
	rawOpcode := uint8(raw)
	hasAux := wordcodeWord(rawOpcode)&wordcodeAuxBit != 0
	op := opcode(rawOpcode & uint8(wordcodeOpcodeMask))
	if op >= opcodeLimit || !table[op].classified {
		return directSemanticMetadata{}, false, fmt.Errorf("shadow word %d: unknown opcode byte 0x%02x", pc, rawOpcode)
	}
	metadata := table[op]
	encoding := metadata.wordcode
	if encoding.aux == wordcodeAuxNone && hasAux {
		return directSemanticMetadata{}, false, fmt.Errorf("shadow word %d %s has an unexpected AUX word", pc, opcodeName(op))
	}
	if encoding.auxRequired && !hasAux {
		return directSemanticMetadata{}, false, fmt.Errorf("shadow word %d %s is missing its required AUX word", pc, opcodeName(op))
	}
	if hasAux && pc+1 >= len(words) {
		return directSemanticMetadata{}, false, fmt.Errorf("shadow word %d %s has a truncated AUX word", pc, opcodeName(op))
	}
	return metadata, hasAux, nil
}
