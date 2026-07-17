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

type directShadowCode struct {
	words  []directShadowWord
	caches []directAdaptiveCacheCell
}

func (code directShadowCode) retainedBytes() int64 {
	return directShadowStateBytes(cap(code.words), cap(code.caches))
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
