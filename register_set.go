package ember

import "math/bits"

type registerSet struct {
	inline   uint64
	overflow []uint64
}

func (set *registerSet) add(register int) {
	if register < 0 {
		return
	}
	if register < 64 {
		set.inline |= uint64(1) << register
		return
	}
	word := register/64 - 1
	set.ensureOverflow(word + 1)
	set.overflow[word] |= uint64(1) << (register % 64)
}

func (set registerSet) contains(register int) bool {
	if register < 0 {
		return false
	}
	if register < 64 {
		return set.inline&(uint64(1)<<register) != 0
	}
	word := register/64 - 1
	return word < len(set.overflow) && set.overflow[word]&(uint64(1)<<(register%64)) != 0
}

func (set *registerSet) remove(register int) {
	if register < 0 {
		return
	}
	if register < 64 {
		set.inline &^= uint64(1) << register
		return
	}
	word := register/64 - 1
	if word < len(set.overflow) {
		set.overflow[word] &^= uint64(1) << (register % 64)
	}
}

func (set *registerSet) clear() {
	set.inline = 0
	clear(set.overflow)
}

func (set *registerSet) addAll(other registerSet) {
	set.inline |= other.inline
	set.ensureOverflow(len(other.overflow))
	for word, value := range other.overflow {
		set.overflow[word] |= value
	}
}

func (set *registerSet) removeAll(other registerSet) {
	set.inline &^= other.inline
	for word := 0; word < len(set.overflow) && word < len(other.overflow); word++ {
		set.overflow[word] &^= other.overflow[word]
	}
}

func (set registerSet) copy() registerSet {
	copied := registerSet{inline: set.inline}
	if len(set.overflow) != 0 {
		copied.overflow = append([]uint64(nil), set.overflow...)
	}
	return copied
}

func (set *registerSet) assign(other registerSet) {
	set.inline = other.inline
	set.ensureOverflow(len(other.overflow))
	copy(set.overflow, other.overflow)
	clear(set.overflow[len(other.overflow):])
}

func (set registerSet) equal(other registerSet) bool {
	if set.inline != other.inline {
		return false
	}
	words := len(set.overflow)
	if len(other.overflow) > words {
		words = len(other.overflow)
	}
	for word := 0; word < words; word++ {
		if set.overflowWord(word) != other.overflowWord(word) {
			return false
		}
	}
	return true
}

func (set registerSet) values() []int {
	count := bits.OnesCount64(set.inline)
	for _, word := range set.overflow {
		count += bits.OnesCount64(word)
	}
	if count == 0 {
		return []int{}
	}
	values := make([]int, 0, count)
	values = appendRegisterWordValues(values, set.inline, 0)
	for word, value := range set.overflow {
		values = appendRegisterWordValues(values, value, (word+1)*64)
	}
	return values
}

func (set *registerSet) ensureOverflow(words int) {
	if words <= len(set.overflow) {
		return
	}
	set.overflow = append(set.overflow, make([]uint64, words-len(set.overflow))...)
}

func (set registerSet) overflowWord(word int) uint64 {
	if word < len(set.overflow) {
		return set.overflow[word]
	}
	return 0
}

func appendRegisterWordValues(values []int, word uint64, base int) []int {
	for word != 0 {
		bit := bits.TrailingZeros64(word)
		values = append(values, base+bit)
		word &^= uint64(1) << bit
	}
	return values
}
