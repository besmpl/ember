package ember

import (
	"fmt"
	"strings"
)

// wordcodeWord is the storage unit for the verifier-first wordcode contract.
// The direct VM loops consume this stream in place; canonical instruction
// materialization is limited to verifier/disassembly paths.
type wordcodeWord = uint32

const (
	// Opcode bytes currently occupy the low byte of a word. The top bit is
	// reserved as the AUX marker; executable opcode ids are below 0x80.
	wordcodeOpcodeMask  wordcodeWord = 0x7f
	wordcodeAuxBit      wordcodeWord = 0x80
	wordcodeFieldMask   wordcodeWord = 0xff
	wordcodeMaxRegister              = 254
)

const (
	wordcodeFormatABC wordcodeFormat = iota + 1
	wordcodeFormatAD
	wordcodeFormatE
)

type wordcodeFormat uint8

// wordcodeAuxMode describes the fixed payload of the one optional/following
// AUX word. A mode is part of opcode metadata; the decoder never guesses the
// layout of a payload.
type wordcodeAuxMode uint8

const (
	wordcodeAuxNone wordcodeAuxMode = iota
	wordcodeAuxA                    // full 32-bit A
	wordcodeAuxB                    // full 32-bit B
	wordcodeAuxC                    // full 32-bit C
	wordcodeAuxD                    // full 32-bit D
	wordcodeAuxBC16
	wordcodeAuxAC16
	wordcodeAuxBD16
	wordcodeAuxCD16
)

type wordcodeOperandSlot uint8

const (
	wordcodeSlotA wordcodeOperandSlot = iota
	wordcodeSlotB
	wordcodeSlotC
	wordcodeSlotD
)

// wordcodeEncodingMetadata is intentionally kept beside opcode effects and
// operand metadata. One AUX word is the hard contract.
type wordcodeEncodingMetadata struct {
	format      wordcodeFormat
	adOperand   wordcodeOperandSlot
	aux         wordcodeAuxMode
	auxRequired bool
}

func wordcodeMetadataFor(op opcode) wordcodeEncodingMetadata {
	meta := wordcodeEncodingMetadata{}
	switch op {
	case opLoadConst:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotB, aux: wordcodeAuxB}
	case opLoadGlobal:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotB, aux: wordcodeAuxBC16, auxRequired: true}
	case opSetGlobal:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxAC16, auxRequired: true}
	case opMove,
		opPrepareIter,
		opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow,
		opConcat, opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual,
		opReturnOne:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC}
	case opNewTable:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxBC16}
	case opSetField:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxB}
	case opSetStringField, opGetStringField, opSetStringFieldIndex, opGetStringFieldIndex,
		opSetIndex, opGetIndex:
		// Cacheable table/index operations stay on their original single-word
		// primary layouts. Semantic string constants and cache site ids live in
		// the immutable Proto cache index, rather than in an AUX payload.
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC}
	case opClosure, opGetUpvalue:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotB, aux: wordcodeAuxB}
	case opSetUpvalue:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxA, auxRequired: true}
	case opVararg:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotB}
	case opArrayNext, opArrayNextJump2:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxD, auxRequired: true}
	case opNeg, opLen:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC}
	case opConcatChain:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxC}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxC}
	case opNumericForCheck, opNumericForLoop:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxD, auxRequired: true}
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotB, aux: wordcodeAuxD, auxRequired: true}
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxD, auxRequired: true}
	case opJumpIfTableHasMetatable:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotD, aux: wordcodeAuxD, auxRequired: true}
	case opFastCall:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxCD16, auxRequired: true}
	case opCall:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxCD16, auxRequired: true}
	case opCallOne:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxCD16, auxRequired: true}
	case opCallLocalOne, opCallUpvalueOne:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxD, auxRequired: true}
	case opCallMethodOne:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatABC, aux: wordcodeAuxCD16, auxRequired: true}
	case opJumpIfFalse:
		// Short conditional jumps stay one word; an out-of-range displacement
		// promotes the same AD form to one AUX word carrying the full i32 delta.
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotB, aux: wordcodeAuxB}
	case opJump:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatE}
	case opReturn:
		meta = wordcodeEncodingMetadata{format: wordcodeFormatAD, adOperand: wordcodeSlotB}
	default:
		return wordcodeEncodingMetadata{}
	}
	return meta
}

func (meta wordcodeEncodingMetadata) hasAux(ins instruction) bool {
	if meta.aux == wordcodeAuxNone {
		return false
	}
	if meta.auxRequired {
		return true
	}
	width := 8
	if meta.format == wordcodeFormatAD && (meta.aux == wordcodeAuxB || meta.aux == wordcodeAuxD) {
		width = 16
	}
	slotWide := func(slot wordcodeOperandSlot) bool {
		value := wordcodeSlotValue(ins, slot)
		return value < 0 || value >= 1<<width
	}
	switch meta.aux {
	case wordcodeAuxA, wordcodeAuxB, wordcodeAuxC, wordcodeAuxD:
		return slotWide(wordcodeAuxSlot(meta.aux))
	case wordcodeAuxBC16:
		return ins.b < 0 || ins.b > 255 || ins.c < 0 || ins.c > 255
	case wordcodeAuxAC16:
		return ins.a < 0 || ins.a > 255 || ins.c < 0 || ins.c > 255
	case wordcodeAuxBD16:
		return ins.b < 0 || ins.b > 255 || ins.d < 0 || ins.d > 255
	case wordcodeAuxCD16:
		return ins.c < -128 || ins.c > 127 || ins.d < -128 || ins.d > 127
	default:
		return false
	}
}

func wordcodeAuxSlot(mode wordcodeAuxMode) wordcodeOperandSlot {
	switch mode {
	case wordcodeAuxA:
		return wordcodeSlotA
	case wordcodeAuxB:
		return wordcodeSlotB
	case wordcodeAuxC:
		return wordcodeSlotC
	case wordcodeAuxD:
		return wordcodeSlotD
	default:
		return wordcodeSlotA
	}
}

func wordcodeSlotValue(ins instruction, slot wordcodeOperandSlot) int {
	switch slot {
	case wordcodeSlotA:
		return ins.a
	case wordcodeSlotB:
		return ins.b
	case wordcodeSlotC:
		return ins.c
	case wordcodeSlotD:
		return ins.d
	default:
		return 0
	}
}

func setWordcodeSlot(ins *instruction, slot wordcodeOperandSlot, value int) {
	switch slot {
	case wordcodeSlotA:
		ins.a = value
	case wordcodeSlotB:
		ins.b = value
	case wordcodeSlotC:
		ins.c = value
	case wordcodeSlotD:
		ins.d = value
	}
}

func wordcodeOperandKindFor(op opcode, slot wordcodeOperandSlot) bytecodeOperandKind {
	meta, ok := opcodeMetadata(op)
	if !ok {
		return bytecodeOperandUnused
	}
	switch slot {
	case wordcodeSlotA:
		return meta.operands.a
	case wordcodeSlotB:
		return meta.operands.b
	case wordcodeSlotC:
		return meta.operands.c
	case wordcodeSlotD:
		return meta.operands.d
	default:
		return bytecodeOperandUnused
	}
}

func wordcodeSlotSigned(op opcode, slot wordcodeOperandSlot) bool {
	if wordcodeOperandKindFor(op, slot) == bytecodeOperandJumpTarget {
		return true
	}
	switch op {
	case opCall, opCallOne:
		return slot == wordcodeSlotC || slot == wordcodeSlotD
	case opFastCall:
		return slot == wordcodeSlotD
	case opCallLocalOne, opCallUpvalueOne, opCallMethodOne:
		return slot == wordcodeSlotD
	case opReturn, opVararg:
		return slot == wordcodeSlotB
	default:
		return false
	}
}

func wordcodeEncodeByte(op opcode, slot wordcodeOperandSlot, value int) (wordcodeWord, error) {
	if wordcodeSlotSigned(op, slot) {
		if value < -128 || value > 127 {
			return 0, fmt.Errorf("%s operand %s value %d out of signed 8-bit range", opcodeName(op), wordcodeSlotName(slot), value)
		}
		return wordcodeWord(uint8(int8(value))), nil
	}
	if value < 0 || value > 255 {
		return 0, fmt.Errorf("%s operand %s value %d out of unsigned 8-bit range", opcodeName(op), wordcodeSlotName(slot), value)
	}
	return wordcodeWord(value), nil
}

func wordcodeDecodeByte(op opcode, slot wordcodeOperandSlot, value wordcodeWord) int {
	if wordcodeSlotSigned(op, slot) {
		return int(int8(uint8(value)))
	}
	return int(uint8(value))
}

func wordcodeEncodeAD(op opcode, slot wordcodeOperandSlot, value int) (wordcodeWord, error) {
	if wordcodeSlotSigned(op, slot) {
		if value < -32768 || value > 32767 {
			return 0, fmt.Errorf("%s operand %s value %d out of signed 16-bit range", opcodeName(op), wordcodeSlotName(slot), value)
		}
		return wordcodeWord(uint16(int16(value))), nil
	}
	if value < 0 || value > 65535 {
		return 0, fmt.Errorf("%s operand %s value %d out of unsigned 16-bit range", opcodeName(op), wordcodeSlotName(slot), value)
	}
	return wordcodeWord(uint16(value)), nil
}

func wordcodeDecodeAD(op opcode, slot wordcodeOperandSlot, value wordcodeWord) int {
	if wordcodeSlotSigned(op, slot) {
		return int(int16(uint16(value)))
	}
	return int(uint16(value))
}

func wordcodeEncodeE(value int) (wordcodeWord, error) {
	if value < -(1<<23) || value > (1<<23)-1 {
		return 0, fmt.Errorf("jump displacement %d out of signed 24-bit range", value)
	}
	return wordcodeWord(uint32(value) & 0x00ffffff), nil
}

func wordcodeDecodeE(value wordcodeWord) int {
	value &= 0x00ffffff
	if value&(1<<23) != 0 {
		value |= 0xff000000
	}
	return int(int32(value))
}

func wordcodeSlotName(slot wordcodeOperandSlot) string {
	return string([]byte{'a' + byte(slot)})
}

func wordcodeAuxContains(meta wordcodeEncodingMetadata, slot wordcodeOperandSlot) bool {
	switch meta.aux {
	case wordcodeAuxA:
		return slot == wordcodeSlotA
	case wordcodeAuxB:
		return slot == wordcodeSlotB
	case wordcodeAuxC:
		return slot == wordcodeSlotC
	case wordcodeAuxD:
		return slot == wordcodeSlotD
	case wordcodeAuxBC16:
		return slot == wordcodeSlotB || slot == wordcodeSlotC
	case wordcodeAuxAC16:
		return slot == wordcodeSlotA || slot == wordcodeSlotC
	case wordcodeAuxBD16:
		return slot == wordcodeSlotB || slot == wordcodeSlotD
	case wordcodeAuxCD16:
		return slot == wordcodeSlotC || slot == wordcodeSlotD
	default:
		return false
	}
}

func wordcodeCacheableOpcode(op opcode) bool {
	switch op {
	case opSetStringField, opGetStringField,
		opSetStringFieldIndex, opGetStringFieldIndex, opSetIndex, opGetIndex:
		return true
	default:
		return false
	}
}

func wordcodeCacheConstantSlot(op opcode) wordcodeOperandSlot {
	switch op {
	case opSetStringField, opSetStringFieldIndex:
		return wordcodeSlotB
	case opGetStringField, opGetStringFieldIndex:
		return wordcodeSlotC
	default:
		return wordcodeSlotA
	}
}

func wordcodeCacheSiteCount(code []instruction) int {
	count := 0
	for _, ins := range code {
		if wordcodeCacheableOpcode(ins.op) {
			count++
		}
	}
	return count
}

const wordcodeCacheDynamicConstant = -1

// wordcodeCacheIndex is the immutable physical-PC index for cacheable
// instructions in one Proto. sidecar stores cacheID+1 at each cache-primary
// word, leaving zero as the no-site marker. constants is ordered by cache id
// and stores semantic constant descriptors;
// wordcodeCacheDynamicConstant is the sentinel used by register-only index
// operations (SET_INDEX and GET_INDEX).
//
// The index is published with Proto and never mutated after finalization. VM
// function instances own the mutable cache pointer array separately.
type wordcodeCacheIndex struct {
	sidecar   []uint32
	constants []int
	wordCount int
}

func wordcodeCacheDescriptor(ins instruction) int {
	switch ins.op {
	case opSetStringField, opSetStringFieldIndex:
		return ins.b
	case opGetStringField, opGetStringFieldIndex:
		return ins.c
	case opSetIndex, opGetIndex:
		return wordcodeCacheDynamicConstant
	default:
		return wordcodeCacheDynamicConstant
	}
}

func buildWordcodeCacheIndex(code []instruction, boundaries []int, wordCount int) (*wordcodeCacheIndex, error) {
	if len(boundaries) != len(code)+1 {
		return nil, fmt.Errorf("cache boundary count %d does not match code length %d", len(boundaries), len(code))
	}
	if wordcodeCacheSiteCount(code) == 0 {
		return nil, nil
	}
	if wordCount < 0 {
		return nil, fmt.Errorf("negative cache word count %d", wordCount)
	}
	index := &wordcodeCacheIndex{
		sidecar:   make([]uint32, wordCount),
		wordCount: wordCount,
	}
	index.constants = make([]int, 0, wordcodeCacheSiteCount(code))
	for logical, ins := range code {
		if !wordcodeCacheableOpcode(ins.op) {
			continue
		}
		if logical < 0 || logical >= len(boundaries)-1 {
			return nil, fmt.Errorf("cache logical pc %d out of range", logical)
		}
		wordPC := boundaries[logical]
		if wordPC < 0 || wordPC >= wordCount {
			return nil, fmt.Errorf("cache logical pc %d maps to physical pc %d outside wordcode length %d", logical, wordPC, wordCount)
		}
		if uint64(len(index.constants)) >= uint64(^uint32(0)) {
			return nil, fmt.Errorf("cache site count %d exceeds direct sidecar marker range", len(index.constants))
		}
		if index.sidecar[wordPC] != 0 {
			return nil, fmt.Errorf("cache logical pc %d maps to duplicate physical pc %d", logical, wordPC)
		}
		index.sidecar[wordPC] = uint32(len(index.constants) + 1)
		index.constants = append(index.constants, wordcodeCacheDescriptor(ins))
	}
	return index, nil
}

func (index *wordcodeCacheIndex) validate(wordCount, expectedSites int) error {
	if index == nil {
		if expectedSites == 0 {
			return nil
		}
		return fmt.Errorf("cache index is missing for %d cache sites", expectedSites)
	}
	if index.wordCount != wordCount {
		return fmt.Errorf("cache index word count %d, want %d", index.wordCount, wordCount)
	}
	if len(index.sidecar) != wordCount {
		return fmt.Errorf("cache index direct sidecar has %d entries, want %d", len(index.sidecar), wordCount)
	}
	markerCount := 0
	for wordPC, marker := range index.sidecar {
		if marker > uint32(len(index.constants)) {
			return fmt.Errorf("cache index direct marker at physical pc %d is %d, want at most %d", wordPC, marker, len(index.constants))
		}
		if marker != 0 {
			want := uint32(markerCount + 1)
			if marker != want {
				return fmt.Errorf("cache index direct marker at physical pc %d is %d, want %d", wordPC, marker, want)
			}
			markerCount++
		}
	}
	if markerCount != len(index.constants) {
		return fmt.Errorf("cache index direct sidecar contains %d markers, want %d descriptors", markerCount, len(index.constants))
	}
	if expectedSites >= 0 && markerCount != expectedSites {
		return fmt.Errorf("cache index direct sidecar contains %d cache sites, want %d", markerCount, expectedSites)
	}
	for id, descriptor := range index.constants {
		if descriptor < wordcodeCacheDynamicConstant {
			return fmt.Errorf("cache descriptor %d is %d, want non-negative constant or %d sentinel", id, descriptor, wordcodeCacheDynamicConstant)
		}
	}
	return nil
}

func (index *wordcodeCacheIndex) marksWord(wordPC int) bool {
	if index == nil || wordPC < 0 || wordPC >= index.wordCount {
		return false
	}
	return wordPC < len(index.sidecar) && index.sidecar[wordPC] != 0
}

func (index *wordcodeCacheIndex) validateWords(words []wordcodeWord, expectedSites, constantCount int) error {
	if err := index.validate(len(words), expectedSites); err != nil {
		return err
	}
	for wordPC := 0; wordPC < len(words); {
		raw := words[wordPC]
		op := opcode(uint8(raw) & uint8(wordcodeOpcodeMask))
		meta, ok := opcodeMetadata(op)
		if !ok {
			return fmt.Errorf("wordcode pc %d has unknown opcode %d", wordPC, op)
		}
		cacheable := wordcodeCacheableOpcode(op)
		if marked := index.marksWord(wordPC); marked != cacheable {
			return fmt.Errorf("wordcode pc %d %s cache mark is %t, want %t", wordPC, opcodeName(op), marked, cacheable)
		}
		if cacheable {
			_, descriptor, complete := index.cacheSiteAt(wordPC)
			if !complete {
				return fmt.Errorf("wordcode pc %d %s has incomplete cache metadata", wordPC, opcodeName(op))
			}
			switch op {
			case opSetIndex, opGetIndex:
				if descriptor != wordcodeCacheDynamicConstant {
					return fmt.Errorf("wordcode pc %d %s cache descriptor is %d, want dynamic sentinel", wordPC, opcodeName(op), descriptor)
				}
			default:
				if descriptor < 0 || constantCount >= 0 && descriptor >= constantCount {
					return fmt.Errorf("wordcode pc %d %s cache constant %d out of range", wordPC, opcodeName(op), descriptor)
				}
			}
		}
		nextWord := wordPC + 1
		if raw&wordcodeAuxBit != 0 {
			if meta.wordcode.aux == wordcodeAuxNone || nextWord >= len(words) {
				return fmt.Errorf("wordcode pc %d %s has invalid AUX", wordPC, opcodeName(op))
			}
			if index.marksWord(nextWord) {
				return fmt.Errorf("wordcode AUX pc %d is marked as a cache primary", nextWord)
			}
			nextWord++
		}
		wordPC = nextWord
	}
	return nil
}

func (index *wordcodeCacheIndex) cacheIDAt(wordPC int) (uint32, bool) {
	if index == nil || wordPC < 0 || wordPC >= index.wordCount {
		return 0, false
	}
	if wordPC >= len(index.sidecar) || index.sidecar[wordPC] == 0 {
		return 0, false
	}
	return index.sidecar[wordPC] - 1, true
}

func (index *wordcodeCacheIndex) descriptorAt(wordPC int) (int, bool) {
	id, ok := index.cacheIDAt(wordPC)
	if !ok || int(id) >= len(index.constants) {
		return wordcodeCacheDynamicConstant, false
	}
	return index.constants[id], true
}

func (index *wordcodeCacheIndex) cacheSiteAt(wordPC int) (uint32, int, bool) {
	id, ok := index.cacheIDAt(wordPC)
	if !ok || int(id) >= len(index.constants) {
		return 0, wordcodeCacheDynamicConstant, false
	}
	return id, index.constants[id], true
}

func wordcodeJumpSlot(op opcode) (wordcodeOperandSlot, bool) {
	switch target := opcodeJumpTarget(op); target {
	case opcodeJumpTargetB:
		return wordcodeSlotB, true
	case opcodeJumpTargetD:
		return wordcodeSlotD, true
	default:
		return wordcodeSlotA, false
	}
}

func wordcodeValueForAux(op opcode, ins instruction, slot wordcodeOperandSlot, displacement int) int {
	if jumpSlot, ok := wordcodeJumpSlot(op); ok && jumpSlot == slot {
		return displacement
	}
	return wordcodeSlotValue(ins, slot)
}

func wordcodeSigned16ForAux(op opcode, slot wordcodeOperandSlot, value int) (uint16, error) {
	if wordcodeSlotSigned(op, slot) {
		if value < -32768 || value > 32767 {
			return 0, fmt.Errorf("%s operand %s value %d out of signed 16-bit range", opcodeName(op), wordcodeSlotName(slot), value)
		}
		return uint16(int16(value)), nil
	}
	if value < 0 || value > 65535 {
		return 0, fmt.Errorf("%s operand %s value %d out of unsigned 16-bit range", opcodeName(op), wordcodeSlotName(slot), value)
	}
	return uint16(value), nil
}

func wordcodeBuildAux(op opcode, meta wordcodeEncodingMetadata, ins instruction, displacement int) (wordcodeWord, error) {
	value := func(slot wordcodeOperandSlot) int { return wordcodeValueForAux(op, ins, slot, displacement) }
	switch meta.aux {
	case wordcodeAuxA, wordcodeAuxB, wordcodeAuxC, wordcodeAuxD:
		v := value(wordcodeAuxSlot(meta.aux))
		if v < -1<<31 || v > 1<<31-1 {
			return 0, fmt.Errorf("%s AUX value %d out of signed 32-bit range", opcodeName(op), v)
		}
		return wordcodeWord(uint32(int32(v))), nil
	case wordcodeAuxBC16, wordcodeAuxAC16, wordcodeAuxBD16, wordcodeAuxCD16:
		var lowSlot, highSlot wordcodeOperandSlot
		switch meta.aux {
		case wordcodeAuxBC16:
			lowSlot, highSlot = wordcodeSlotB, wordcodeSlotC
		case wordcodeAuxAC16:
			lowSlot, highSlot = wordcodeSlotA, wordcodeSlotC
		case wordcodeAuxBD16:
			lowSlot, highSlot = wordcodeSlotB, wordcodeSlotD
		case wordcodeAuxCD16:
			lowSlot, highSlot = wordcodeSlotC, wordcodeSlotD
		}
		low, err := wordcodeSigned16ForAux(op, lowSlot, value(lowSlot))
		if err != nil {
			return 0, err
		}
		high, err := wordcodeSigned16ForAux(op, highSlot, value(highSlot))
		if err != nil {
			return 0, err
		}
		return wordcodeWord(low) | wordcodeWord(high)<<16, nil
	default:
		return 0, fmt.Errorf("%s declares unsupported AUX mode %d", opcodeName(op), meta.aux)
	}
}

// wordcodeSetAux writes decoded AUX values into the instruction. The pointer
// is deliberate: mutating a value copy silently discards extended operands.
func wordcodeSetAux(ins *instruction, meta wordcodeEncodingMetadata, aux wordcodeWord) error {
	if ins == nil {
		return fmt.Errorf("nil instruction for AUX decode")
	}
	switch meta.aux {
	case wordcodeAuxA, wordcodeAuxB, wordcodeAuxC, wordcodeAuxD:
		setWordcodeSlot(ins, wordcodeAuxSlot(meta.aux), int(int32(aux)))
		return nil
	case wordcodeAuxBC16, wordcodeAuxAC16, wordcodeAuxBD16, wordcodeAuxCD16:
		var lowSlot, highSlot wordcodeOperandSlot
		switch meta.aux {
		case wordcodeAuxBC16:
			lowSlot, highSlot = wordcodeSlotB, wordcodeSlotC
		case wordcodeAuxAC16:
			lowSlot, highSlot = wordcodeSlotA, wordcodeSlotC
		case wordcodeAuxBD16:
			lowSlot, highSlot = wordcodeSlotB, wordcodeSlotD
		case wordcodeAuxCD16:
			lowSlot, highSlot = wordcodeSlotC, wordcodeSlotD
		}
		setWordcodeSlot(ins, lowSlot, wordcodeDecodeAD(ins.op, lowSlot, aux))
		setWordcodeSlot(ins, highSlot, wordcodeDecodeAD(ins.op, highSlot, aux>>16))
		return nil
	default:
		return fmt.Errorf("%s declares unsupported AUX mode %d", opcodeName(ins.op), meta.aux)
	}
}

func wordcodeValidateMetadata(op opcode, meta opcodeMetadataEntry) error {
	encoding := meta.wordcode
	if encoding.format == 0 {
		return fmt.Errorf("wordcode format is missing")
	}
	if encoding.aux == wordcodeAuxNone && encoding.auxRequired {
		return fmt.Errorf("marks AUX required without an AUX mode")
	}
	if encoding.format != wordcodeFormatAD && encoding.adOperand != 0 {
		return fmt.Errorf("declares an AD operand for non-AD format")
	}
	if encoding.format == wordcodeFormatE && (encoding.aux != wordcodeAuxNone || encoding.auxRequired) {
		return fmt.Errorf("E format cannot carry AUX")
	}
	return nil
}

func wordcodePrimaryByte(value int, aux bool) (wordcodeWord, error) {
	if value < 0 || value > 255 {
		if !aux || value < -1<<31 || value > 1<<31-1 {
			return 0, fmt.Errorf("value %d out of unsigned 8-bit range", value)
		}
		return wordcodeWord(uint8(value)), nil
	}
	return wordcodeWord(uint8(value)), nil
}

func wordcodePrimaryAD(value int, signed bool, aux bool) (wordcodeWord, error) {
	if signed {
		if value < -32768 || value > 32767 {
			if !aux || value < -1<<31 || value > 1<<31-1 {
				return 0, fmt.Errorf("value %d out of signed 16-bit range", value)
			}
			return wordcodeWord(uint16(value)), nil
		}
		return wordcodeWord(uint16(int16(value))), nil
	}
	if value < 0 || value > 65535 {
		if !aux || value > 1<<31-1 {
			return 0, fmt.Errorf("value %d out of unsigned 16-bit range", value)
		}
		return wordcodeWord(uint16(value)), nil
	}
	return wordcodeWord(uint16(value)), nil
}

func wordcodeEncodeInstruction(ins instruction, pc int, nextWordPC int, boundaries []int) ([]wordcodeWord, error) {
	meta, ok := opcodeMetadata(ins.op)
	if !ok {
		return nil, fmt.Errorf("unknown opcode %d", ins.op)
	}
	encoding := meta.wordcode
	displacement := 0
	if target, ok := instructionJumpTarget(ins); ok {
		if target < 0 || target >= len(boundaries) {
			return nil, fmt.Errorf("%s jump target %d out of range", opcodeName(ins.op), target)
		}
		displacement = boundaries[target] - nextWordPC
	}
	hasAux := encoding.hasAux(ins) || wordcodeNeedsAuxForDisplacement(ins, displacement)
	// The boundary pass may conservatively promote a conditional jump even
	// after a later boundary shift makes the displacement short. Matching the
	// planned span keeps the decoder and boundary map in lock-step.
	if pc >= 0 && pc+1 < len(boundaries) && boundaries[pc+1]-boundaries[pc] > 1 {
		hasAux = true
	}
	if encoding.auxRequired {
		hasAux = true
	}
	if encoding.aux == wordcodeAuxNone && hasAux {
		return nil, fmt.Errorf("%s does not permit an AUX word", opcodeName(ins.op))
	}
	word := wordcodeWord(uint32(ins.op))
	if hasAux {
		word |= wordcodeAuxBit
	}
	encodePrimary := func(slot wordcodeOperandSlot, value int) (wordcodeWord, error) {
		if wordcodeCacheableOpcode(ins.op) &&
			ins.op != opSetIndex && ins.op != opGetIndex &&
			slot == wordcodeCacheConstantSlot(ins.op) {
			// The low byte remains a useful compact hint for disassembly, but
			// the immutable cache descriptor is authoritative for wide indexes.
			return wordcodePrimaryByte(value, true)
		}
		if wordcodeAuxContains(encoding, slot) {
			return wordcodePrimaryByte(value, true)
		}
		return wordcodeEncodeByte(ins.op, slot, value)
	}
	switch encoding.format {
	case wordcodeFormatABC:
		primary := [3]int{ins.a, ins.b, ins.c}
		if wordcodeCacheableOpcode(ins.op) {
			// Chained string-field operations have four semantic operands but
			// retain a single primary ABC word. Their full constant descriptor is
			// carried by the immutable Proto cache index; the remaining register
			// operands occupy the primary B/C bytes.
			switch ins.op {
			case opSetStringFieldIndex:
				primary = [3]int{ins.a, ins.c, ins.d}
			case opGetStringFieldIndex:
				primary = [3]int{ins.a, ins.b, ins.d}
			}
		}
		for index, slot := range []wordcodeOperandSlot{wordcodeSlotA, wordcodeSlotB, wordcodeSlotC} {
			value := primary[index]
			field, err := encodePrimary(slot, value)
			if err != nil {
				return nil, fmt.Errorf("%s operand %s: %w", opcodeName(ins.op), wordcodeSlotName(slot), err)
			}
			word |= field << (8 + 8*index)
		}
	case wordcodeFormatAD:
		a, err := encodePrimary(wordcodeSlotA, ins.a)
		if err != nil {
			return nil, fmt.Errorf("%s operand a: %w", opcodeName(ins.op), err)
		}
		displayValue := wordcodeSlotValue(ins, encoding.adOperand)
		if jumpSlot, ok := wordcodeJumpSlot(ins.op); ok && jumpSlot == encoding.adOperand {
			displayValue = displacement
		}
		d, err := wordcodePrimaryAD(displayValue, wordcodeSlotSigned(ins.op, encoding.adOperand), wordcodeAuxContains(encoding, encoding.adOperand))
		if err != nil {
			return nil, fmt.Errorf("%s operand %s: %w", opcodeName(ins.op), wordcodeSlotName(encoding.adOperand), err)
		}
		word |= a << 8
		word |= d << 16
	case wordcodeFormatE:
		d, err := wordcodeEncodeE(displacement)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", opcodeName(ins.op), err)
		}
		word |= d << 8
	default:
		return nil, fmt.Errorf("%s has unsupported wordcode format %d", opcodeName(ins.op), encoding.format)
	}
	if !hasAux {
		return []wordcodeWord{word}, nil
	}
	aux, err := wordcodeBuildAux(ins.op, encoding, ins, displacement)
	if err != nil {
		return nil, err
	}
	return []wordcodeWord{word, aux}, nil
}

func opcodeJumpTargetSlotToWordcode(target opcodeJumpTargetSlot) wordcodeOperandSlot {
	if target == opcodeJumpTargetB {
		return wordcodeSlotB
	}
	return wordcodeSlotD
}

func wordcodeNeedsAuxForDisplacement(ins instruction, displacement int) bool {
	if ins.op == opJumpIfFalse {
		return displacement < -32768 || displacement > 32767
	}
	return false
}

// wordcodeBoundaryMap is the stable logical-PC/word-PC seam. AUX words are
// not logical instructions and therefore never receive a logical index.
type wordcodeBoundaryMap struct {
	logicalToWord []int
	wordToLogical map[int]int
}

func (m wordcodeBoundaryMap) logicalPC(logical int) (int, bool) {
	if logical < 0 || logical >= len(m.logicalToWord) {
		return 0, false
	}
	return m.logicalToWord[logical], true
}

func (m wordcodeBoundaryMap) wordPC(word int) (int, bool) {
	logical, ok := m.wordToLogical[word]
	return logical, ok
}

func wordcodeMapLines(lines []int, boundaries []int) []int {
	if len(boundaries) == 0 {
		return nil
	}
	total := boundaries[len(boundaries)-1]
	mapped := make([]int, total)
	for logical := 0; logical+1 < len(boundaries); logical++ {
		line := 0
		if logical < len(lines) {
			line = lines[logical]
		}
		// AUX is an implementation detail, not a logical instruction. Leave
		// its line entry zero so hooks only observe primary words.
		mapped[boundaries[logical]] = line
	}
	return mapped
}

func buildWordcodeBoundaryMap(code []instruction) (wordcodeBoundaryMap, error) {
	boundaries := make([]int, len(code)+1)
	forcedJumpAux := make([]bool, len(code))
	for iteration := 0; iteration <= len(code)+1; iteration++ {
		next := make([]int, len(code)+1)
		for pc, ins := range code {
			meta, ok := opcodeMetadata(ins.op)
			if !ok {
				return wordcodeBoundaryMap{}, fmt.Errorf("instruction %d: unknown opcode %d", pc, ins.op)
			}
			words := 1
			if meta.wordcode.auxRequired || meta.wordcode.hasAux(ins) || forcedJumpAux[pc] {
				words++
			}
			next[pc+1] = next[pc] + words
			if target, ok := instructionJumpTarget(ins); ok && (target < 0 || target >= len(next)) {
				return wordcodeBoundaryMap{}, fmt.Errorf("instruction %d %s jump target %d out of range", pc, opcodeName(ins.op), target)
			}
		}
		boundaries = next
		changed := false
		for pc, ins := range code {
			target, ok := instructionJumpTarget(ins)
			if !ok || target < 0 || target >= len(boundaries) {
				continue
			}
			displacement := boundaries[target] - boundaries[pc+1]
			if wordcodeNeedsAuxForDisplacement(ins, displacement) && !forcedJumpAux[pc] {
				forcedJumpAux[pc] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	wordToLogical := make(map[int]int, len(boundaries))
	for logical, word := range boundaries {
		wordToLogical[word] = logical
	}
	return wordcodeBoundaryMap{logicalToWord: boundaries, wordToLogical: wordToLogical}, nil
}

func wordcodeBoundaries(code []instruction) ([]int, error) {
	m, err := buildWordcodeBoundaryMap(code)
	if err != nil {
		return nil, err
	}
	return append([]int(nil), m.logicalToWord...), nil
}

func wordcodeLogicalLineMap(lines []int, boundaries []int) []int {
	return wordcodeMapLines(lines, boundaries)
}

// encodeWordcode converts logical instruction PCs to word PCs before writing
// relative jumps. The optional limits are register and constant counts.
func encodeWordcode(code []instruction, limits ...int) ([]wordcodeWord, error) {
	registerCount, constantCount := wordcodeLimits(limits)
	for pc, ins := range code {
		if err := verifyWordcodeInstruction(ins, registerCount, constantCount, -1, -1, len(code)); err != nil {
			return nil, fmt.Errorf("instruction %d %s: %w", pc, opcodeName(ins.op), err)
		}
	}
	boundaryMap, err := buildWordcodeBoundaryMap(code)
	if err != nil {
		return nil, err
	}
	boundaries := boundaryMap.logicalToWord
	words := make([]wordcodeWord, 0, boundaries[len(code)])
	for pc, ins := range code {
		encoded, err := wordcodeEncodeInstruction(ins, pc, boundaries[pc+1], boundaries)
		if err != nil {
			return nil, fmt.Errorf("instruction %d %s: %w", pc, opcodeName(ins.op), err)
		}
		words = append(words, encoded...)
	}
	return words, nil
}

func wordcodeLimits(limits []int) (registerCount, constantCount int) {
	registerCount, constantCount = -1, -1
	if len(limits) > 0 {
		registerCount = limits[0]
	}
	if len(limits) > 1 {
		constantCount = limits[1]
	}
	return registerCount, constantCount
}

type wordcodeDecoded struct {
	ins      instruction
	wordPC   int
	nextWord int
}

func wordcodeDecodeWords(words []wordcodeWord, indexes ...*wordcodeCacheIndex) ([]wordcodeDecoded, []int, error) {
	if len(indexes) > 1 {
		return nil, nil, fmt.Errorf("wordcode decoder received %d cache indexes", len(indexes))
	}
	var cacheIndex *wordcodeCacheIndex
	if len(indexes) != 0 && indexes[0] != nil {
		cacheIndex = indexes[0]
		if err := cacheIndex.validate(len(words), -1); err != nil {
			return nil, nil, err
		}
	}
	decoded := make([]wordcodeDecoded, 0)
	boundaries := []int{0}
	for wordPC := 0; wordPC < len(words); {
		raw := words[wordPC]
		rawOp := uint8(raw)
		hasAux := wordcodeWord(rawOp)&wordcodeAuxBit != 0
		op := opcode(rawOp & uint8(wordcodeOpcodeMask))
		meta, ok := opcodeMetadata(op)
		if !ok {
			return nil, nil, fmt.Errorf("word %d: unknown opcode byte 0x%02x", wordPC, rawOp)
		}
		if meta.wordcode.aux == wordcodeAuxNone && hasAux {
			return nil, nil, fmt.Errorf("word %d %s has an unexpected AUX word", wordPC, opcodeName(op))
		}
		if meta.wordcode.auxRequired && !hasAux {
			return nil, nil, fmt.Errorf("word %d %s is missing its required AUX word", wordPC, opcodeName(op))
		}
		ins := instruction{op: op}
		switch meta.wordcode.format {
		case wordcodeFormatABC:
			ins.a = wordcodeDecodeByte(op, wordcodeSlotA, raw>>8)
			ins.b = wordcodeDecodeByte(op, wordcodeSlotB, raw>>16)
			ins.c = wordcodeDecodeByte(op, wordcodeSlotC, raw>>24)
			switch op {
			case opSetStringFieldIndex:
				ins.d = ins.c
				ins.c = ins.b
				ins.b = 0
			case opGetStringFieldIndex:
				ins.d = ins.c
				ins.c = 0
			}
		case wordcodeFormatAD:
			ins.a = wordcodeDecodeByte(op, wordcodeSlotA, raw>>8)
			setWordcodeSlot(&ins, meta.wordcode.adOperand, wordcodeDecodeAD(op, meta.wordcode.adOperand, raw>>16))
		case wordcodeFormatE:
			setWordcodeSlot(&ins, opcodeJumpTargetSlotToWordcode(opcodeJumpTarget(op)), wordcodeDecodeE(raw>>8))
		default:
			return nil, nil, fmt.Errorf("word %d %s has unsupported format %d", wordPC, opcodeName(op), meta.wordcode.format)
		}
		if wordcodeCacheableOpcode(op) && cacheIndex != nil {
			_, descriptor, ok := cacheIndex.cacheSiteAt(wordPC)
			if !ok {
				return nil, nil, fmt.Errorf("word %d %s is not a complete cache primary", wordPC, opcodeName(op))
			}
			if wordcodeCacheConstantSlot(op) != wordcodeSlotA && descriptor != wordcodeCacheDynamicConstant {
				setWordcodeSlot(&ins, wordcodeCacheConstantSlot(op), descriptor)
			}
		}
		nextWord := wordPC + 1
		if hasAux {
			if nextWord >= len(words) {
				return nil, nil, fmt.Errorf("word %d %s AUX word is truncated", wordPC, opcodeName(op))
			}
			aux := words[nextWord]
			if err := wordcodeSetAux(&ins, meta.wordcode, aux); err != nil {
				return nil, nil, err
			}
			nextWord++
		}
		decoded = append(decoded, wordcodeDecoded{ins: ins, wordPC: wordPC, nextWord: nextWord})
		boundaries = append(boundaries, nextWord)
		wordPC = nextWord
	}
	return decoded, boundaries, nil
}

func decodeWordcode(words []wordcodeWord, indexes ...*wordcodeCacheIndex) ([]instruction, error) {
	decoded, boundaries, err := wordcodeDecodeWords(words, indexes...)
	if err != nil {
		return nil, err
	}
	wordToLogical := make(map[int]int, len(boundaries))
	for logical, wordPC := range boundaries {
		wordToLogical[wordPC] = logical
	}
	result := make([]instruction, len(decoded))
	for logical, entry := range decoded {
		ins := entry.ins
		if target, ok := instructionJumpTarget(ins); ok {
			targetWord := boundaries[logical+1] + target
			targetLogical, valid := wordToLogical[targetWord]
			if !valid {
				return nil, fmt.Errorf("word %d %s jump displacement %d lands inside an instruction", entry.wordPC, opcodeName(ins.op), target)
			}
			setWordcodeSlot(&ins, opcodeJumpTargetSlotToWordcode(opcodeJumpTarget(ins.op)), targetLogical)
		}
		result[logical] = ins
	}
	return result, nil
}

func verifyWordcodeInstruction(ins instruction, registerCount, constantCount, prototypeCount, upvalueCount, codeLen int) error {
	_, ok := opcodeMetadata(ins.op)
	if !ok {
		return fmt.Errorf("unknown opcode %d", ins.op)
	}
	if registerCount < 0 {
		registerCount = 255
	}
	if registerCount > 255 {
		return fmt.Errorf("register count %d exceeds wordcode limit 255", registerCount)
	}
	checkRegister := func(slot wordcodeOperandSlot, value int) error {
		if value < 0 || value > wordcodeMaxRegister || value >= registerCount {
			return fmt.Errorf("register %s index %d out of range 0..%d", wordcodeSlotName(slot), value, registerCount-1)
		}
		return nil
	}
	checkConstant := func(slot wordcodeOperandSlot, value int) error {
		if value < 0 || (constantCount >= 0 && value >= constantCount) {
			return fmt.Errorf("constant %s index %d out of range", wordcodeSlotName(slot), value)
		}
		return nil
	}
	for _, slot := range []wordcodeOperandSlot{wordcodeSlotA, wordcodeSlotB, wordcodeSlotC, wordcodeSlotD} {
		value := wordcodeSlotValue(ins, slot)
		if ins.op == opReturn && ins.b == 0 && slot == wordcodeSlotA {
			continue
		}
		switch wordcodeOperandKindFor(ins.op, slot) {
		case bytecodeOperandRegister:
			if err := checkRegister(slot, value); err != nil {
				return err
			}
		case bytecodeOperandConstant:
			if err := checkConstant(slot, value); err != nil {
				return err
			}
		case bytecodeOperandPrototype:
			if value < 0 || (prototypeCount >= 0 && value >= prototypeCount) {
				return fmt.Errorf("prototype %s index %d out of range", wordcodeSlotName(slot), value)
			}
		case bytecodeOperandUpvalue:
			if value < 0 || (upvalueCount >= 0 && value >= upvalueCount) {
				return fmt.Errorf("upvalue %s index %d out of range", wordcodeSlotName(slot), value)
			}
		case bytecodeOperandGlobalSlot, bytecodeOperandNativeID:
			if value < 0 || value > 65535 {
				return fmt.Errorf("%s %s value %d out of range", opcodeName(ins.op), wordcodeSlotName(slot), value)
			}
		case bytecodeOperandCount:
			if value < -32768 || value > 65535 {
				return fmt.Errorf("count %s value %d out of range", wordcodeSlotName(slot), value)
			}
		case bytecodeOperandJumpTarget:
			if value < 0 || (codeLen >= 0 && value > codeLen) {
				return fmt.Errorf("jump target %d out of range", value)
			}
		case bytecodeOperandUnused:
			if value != 0 && !(ins.op == opCallOne && slot == wordcodeSlotD && value == 1) {
				return fmt.Errorf("unused operand %s is %d", wordcodeSlotName(slot), value)
			}
		}
	}
	if ins.op == opCallOne && ins.d != 1 {
		return fmt.Errorf("CALL_ONE d must be canonical result count 1, got %d", ins.d)
	}
	if ins.op == opNewTable && (ins.b < 0 || ins.c < 0) {
		return fmt.Errorf("NEW_TABLE capacities must be non-negative, got (%d, %d)", ins.b, ins.c)
	}
	if ins.op == opFastCall {
		if _, ok := nativeFuncByID(nativeFuncID(ins.b)); !ok {
			return fmt.Errorf("unknown fast call native id %d", ins.b)
		}
		if ins.c < 0 || ins.c > wordcodeMaxRegister || ins.a+ins.c > registerCount {
			return fmt.Errorf("fast call argument register span is invalid")
		}
		if ins.d > 0 && (ins.d > wordcodeMaxRegister || ins.a+ins.d > registerCount) {
			return fmt.Errorf("fast call result register span is invalid")
		}
	}
	if ins.op == opArrayNext && (ins.d <= 0 || ins.a+ins.d > registerCount) {
		return fmt.Errorf("array next result range is invalid")
	}
	if ins.op == opConcatChain && (ins.c <= 0 || ins.b+ins.c > registerCount) {
		return fmt.Errorf("concat chain register span is invalid")
	}
	if ins.op == opCall {
		if decodeOpenResultCallMarker(ins.d) {
			prefixCount, openArguments := decodeOpenArgumentCallMarker(ins.c)
			if ins.c < 0 && !openArguments {
				return fmt.Errorf("open-result call marker requires fixed arguments")
			}
			if openArguments {
				if registerCount >= 0 && ins.b+1+prefixCount >= registerCount {
					return fmt.Errorf("call argument register range out of range")
				}
			} else if registerCount >= 0 && ins.b+ins.c >= registerCount {
				return fmt.Errorf("call argument register range out of range")
			}
			return nil
		}
		if prefixCount, marked := decodeOpenArgumentCallMarker(ins.c); marked {
			openArgStart := ins.b + 1 + prefixCount
			if openArgStart < 0 || openArgStart >= registerCount {
				return fmt.Errorf("open call argument register %d out of range", openArgStart)
			}
		} else if ins.c < 0 {
			prefixCount := -ins.c - 1
			openArgStart := ins.b + 1 + prefixCount
			if prefixCount < 0 || openArgStart < 0 || openArgStart >= registerCount {
				return fmt.Errorf("open call argument register %d out of range", openArgStart)
			}
		} else if ins.b+ins.c >= registerCount {
			return fmt.Errorf("call argument register range out of range")
		}
		resultCount := ins.d
		if decoded, marked := decodeFixedMultiResultCount(ins.d, registerCount); marked {
			resultCount = decoded
		}
		if resultCount > 0 && (resultCount > registerCount-ins.a) {
			return fmt.Errorf("call result register range out of range")
		}
	}
	for _, op := range []opcode{opCallOne, opCallLocalOne, opCallUpvalueOne, opCallMethodOne} {
		if ins.op != op {
			continue
		}
		count := ins.d
		if op == opCallOne {
			count = ins.c
		}
		decoded, _, err := verifyFixedCallCount(count, opcodeName(op))
		if err != nil {
			return err
		}
		if op == opCallOne && ins.b+1+decoded > registerCount {
			return fmt.Errorf("call argument register range out of range")
		}
		if op == opCallLocalOne && ins.c+decoded > registerCount {
			return fmt.Errorf("local call argument register range out of range")
		}
		if op == opCallUpvalueOne && ins.c+decoded > registerCount {
			return fmt.Errorf("upvalue call argument register range out of range")
		}
		if op == opCallMethodOne && ins.a+decoded+1 > registerCount {
			return fmt.Errorf("method call argument register range out of range")
		}
	}
	if ins.op == opReturn && ins.b < 0 {
		prefixCount := -ins.b - 1
		if prefixCount < 0 || ins.a+prefixCount >= registerCount {
			return fmt.Errorf("open return register range out of range")
		}
	}
	if target, ok := instructionJumpTarget(ins); ok && (target < 0 || (codeLen >= 0 && target > codeLen)) {
		return fmt.Errorf("jump target %d out of range", target)
	}
	return nil
}

// verifyWordcode validates a decoded stream. Optional limits are registers,
// constants, prototypes, and upvalues respectively.
func verifyWordcode(words []wordcodeWord, limits ...int) error {
	_, _, err := wordcodeDecodeWords(words)
	if err != nil {
		return err
	}
	code, err := decodeWordcode(words)
	if err != nil {
		return err
	}
	registerCount, constantCount := -1, -1
	prototypeCount, upvalueCount := -1, -1
	if len(limits) > 0 {
		registerCount = limits[0]
	}
	if len(limits) > 1 {
		constantCount = limits[1]
	}
	if len(limits) > 2 {
		prototypeCount = limits[2]
	}
	if len(limits) > 3 {
		upvalueCount = limits[3]
	}
	for pc, ins := range code {
		if err := verifyWordcodeInstruction(ins, registerCount, constantCount, prototypeCount, upvalueCount, len(code)); err != nil {
			return fmt.Errorf("instruction %d %s: %w", pc, opcodeName(ins.op), err)
		}
		if ins.op == opCall && decodeOpenResultCallMarker(ins.d) && !openResultCallHasImmediateConsumer(code, pc, ins.a) {
			return fmt.Errorf("instruction %d CALL open-result marker is not consumed by a matching open RETURN", pc)
		}
		if ins.op == opCall {
			prefixCount, open := decodeOpenArgumentCallMarker(ins.c)
			if !open && ins.c < 0 {
				prefixCount = -ins.c - 1
				open = true
			}
			if open && !wordcodeOpenResultProducer(code, pc, ins.b+1+prefixCount, registerCount) {
				return fmt.Errorf("instruction %d CALL open call has no preceding open-result producer", pc)
			}
		}
		if ins.op == opReturn && ins.b < 0 && !wordcodeOpenResultProducer(code, pc, ins.a+(-ins.b-1), registerCount) {
			return fmt.Errorf("instruction %d RETURN open result has no preceding open-result producer", pc)
		}
	}
	return nil
}

func wordcodeOpenResultProducer(code []instruction, pc, start, registerCount int) bool {
	if pc <= 0 || start < 0 {
		return false
	}
	prev := code[pc-1]
	switch prev.op {
	case opVararg:
		return prev.b < 0 && prev.a == start
	case opCall:
		if prev.d < 0 && prev.a == start {
			if registerCount >= 0 {
				if _, marked := decodeFixedMultiResultCount(prev.d, registerCount); marked {
					return false
				}
			}
			return true
		}
		return false
	case opFastCall:
		return prev.d < 0 && prev.a == start
	default:
		return false
	}
}

func verifyWordcodeForProto(proto *Proto, words []wordcodeWord) error {
	if proto == nil {
		return fmt.Errorf("nil prototype")
	}
	if proto.cacheSiteCount < 0 {
		return fmt.Errorf("negative cache site count %d", proto.cacheSiteCount)
	}
	if err := verifyWordcode(words, proto.registers, len(proto.constants), len(proto.prototypes), len(proto.upvalues)); err != nil {
		return err
	}
	if err := proto.cacheIndex.validateWords(words, proto.cacheSiteCount, len(proto.constants)); err != nil {
		return err
	}
	code, err := decodeWordcode(words, proto.cacheIndex)
	if err != nil {
		return err
	}
	candidate := *proto
	for pc, ins := range code {
		if err := verifyInstruction(&candidate, pc, ins, len(code)); err != nil {
			return fmt.Errorf("instruction %d %s: %w", pc, opcodeName(ins.op), err)
		}
	}
	return nil
}

func disassembleWordcodeChecked(words []wordcodeWord, indexes ...*wordcodeCacheIndex) ([]string, error) {
	code, err := decodeWordcode(words, indexes...)
	if err != nil {
		return nil, err
	}
	lines := make([]string, len(code))
	for pc, ins := range code {
		parts := []string{opcodeName(ins.op)}
		for _, value := range []int{ins.a, ins.b, ins.c, ins.d} {
			parts = append(parts, fmt.Sprintf("%d", value))
		}
		lines[pc] = fmt.Sprintf("%04d %s", pc, strings.Join(parts, " "))
	}
	return lines, nil
}

func disassembleWordcode(words []wordcodeWord, indexes ...*wordcodeCacheIndex) []string {
	lines, err := disassembleWordcodeChecked(words, indexes...)
	if err != nil {
		return []string{fmt.Sprintf("<invalid wordcode: %v>", err)}
	}
	return lines
}
