package ember

import "fmt"

type instructionRegisterAccess uint8

const (
	instructionRegisterRead instructionRegisterAccess = 1 << iota
	instructionRegisterWrite
	instructionRegisterReadWrite = instructionRegisterRead | instructionRegisterWrite
)

func (access instructionRegisterAccess) matches(reads bool, writes bool) bool {
	return access&instructionRegisterRead != 0 && reads || access&instructionRegisterWrite != 0 && writes
}

func (access instructionRegisterAccess) String() string {
	switch access {
	case instructionRegisterRead:
		return "read"
	case instructionRegisterWrite:
		return "write"
	case instructionRegisterReadWrite:
		return "read/write"
	default:
		return "none"
	}
}

type registerEffectSlot uint8

const (
	registerEffectSlotA registerEffectSlot = iota
	registerEffectSlotB
	registerEffectSlotC
	registerEffectSlotD
)

type registerEffectSpanMode uint8

const (
	registerEffectSpanPositiveCount registerEffectSpanMode = iota
	registerEffectSpanSignedCount
	registerEffectSpanOpenOrOne
)

type opcodeRegisterEffect struct {
	slot   registerEffectSlot
	offset int8
	access instructionRegisterAccess
}

type opcodeRegisterSpan struct {
	start  registerEffectSlot
	offset int8
	count  registerEffectSlot
	mode   registerEffectSpanMode
	access instructionRegisterAccess
}

type opcodeRegisterEffects struct {
	classified bool
	fixedCount uint8
	fixed      [4]opcodeRegisterEffect
	spanCount  uint8
	spans      [4]opcodeRegisterSpan
}

func registerEffect(slot registerEffectSlot, offset int8, access instructionRegisterAccess) opcodeRegisterEffect {
	return opcodeRegisterEffect{slot: slot, offset: offset, access: access}
}

func registerSpan(start registerEffectSlot, offset int8, count registerEffectSlot, mode registerEffectSpanMode, access instructionRegisterAccess) opcodeRegisterSpan {
	return opcodeRegisterSpan{start: start, offset: offset, count: count, mode: mode, access: access}
}

func newOpcodeRegisterEffects(fixed []opcodeRegisterEffect, spans []opcodeRegisterSpan) opcodeRegisterEffects {
	var effects opcodeRegisterEffects
	effects.classified = true
	for _, effect := range fixed {
		for index := 0; index < int(effects.fixedCount); index++ {
			if effects.fixed[index].slot != effect.slot || effects.fixed[index].offset != effect.offset {
				continue
			}
			effects.fixed[index].access |= effect.access
			effect = opcodeRegisterEffect{}
			break
		}
		if effect.access == 0 {
			continue
		}
		if effects.fixedCount >= uint8(len(effects.fixed)) {
			panic("too many fixed register effects")
		}
		effects.fixed[effects.fixedCount] = effect
		effects.fixedCount++
	}
	if len(spans) > len(effects.spans) {
		panic("too many register effect spans")
	}
	copy(effects.spans[:], spans)
	effects.spanCount = uint8(len(spans))
	return effects
}

func validateOpcodeRegisterEffects(effects opcodeRegisterEffects) error {
	if !effects.classified {
		return fmt.Errorf("register effects are unclassified")
	}
	if effects.fixedCount > uint8(len(effects.fixed)) {
		return fmt.Errorf("too many fixed register effects")
	}
	for index := 0; index < int(effects.fixedCount); index++ {
		effect := effects.fixed[index]
		if effect.slot > registerEffectSlotD || effect.access == 0 {
			return fmt.Errorf("invalid fixed register effect %d", index)
		}
		for previous := 0; previous < index; previous++ {
			if effects.fixed[previous].slot == effect.slot && effects.fixed[previous].offset == effect.offset {
				return fmt.Errorf("duplicate fixed register effect %d", index)
			}
		}
	}
	if effects.spanCount > uint8(len(effects.spans)) {
		return fmt.Errorf("too many register effect spans")
	}
	for index := 0; index < int(effects.spanCount); index++ {
		span := effects.spans[index]
		if span.start > registerEffectSlotD || span.count > registerEffectSlotD || span.access == 0 {
			return fmt.Errorf("invalid register effect span %d", index)
		}
		if span.mode > registerEffectSpanOpenOrOne {
			return fmt.Errorf("invalid register effect span mode %d", span.mode)
		}
	}
	return nil
}

func registerEffectSlotValue(ins instruction, slot registerEffectSlot) int {
	switch slot {
	case registerEffectSlotA:
		return ins.a
	case registerEffectSlotB:
		return ins.b
	case registerEffectSlotC:
		return ins.c
	case registerEffectSlotD:
		return ins.d
	default:
		return 0
	}
}

func registerEffectAccessMatches(effect instructionRegisterAccess, requested instructionRegisterAccess) bool {
	return effect&requested != 0
}

func registerEffectSpanBounds(ins instruction, span opcodeRegisterSpan, bound int, clamp bool) (start int, end int, ok bool) {
	start = registerEffectSlotValue(ins, span.start) + int(span.offset)
	count := registerEffectSlotValue(ins, span.count)
	open := false
	switch span.mode {
	case registerEffectSpanPositiveCount:
		if count <= 0 {
			return 0, 0, false
		}
	case registerEffectSpanSignedCount:
		if count < 0 {
			count, _ = decodeFixedCallCount(count)
		}
		if count <= 0 {
			return 0, 0, false
		}
	case registerEffectSpanOpenOrOne:
		if count < 0 {
			open = true
			count = 0
		} else if count == 0 {
			count = 1
		}
	default:
		return 0, 0, false
	}
	if start < 0 {
		start = 0
	}
	if open {
		if !clamp {
			return start, 0, true
		}
		end = bound
	} else {
		end = registerEffectAddCount(start, count)
		if clamp && end > bound {
			end = bound
		}
	}
	return start, end, end > start
}

func registerEffectAddCount(start int, count int) int {
	if count <= 0 {
		return start
	}
	maxInt := int(^uint(0) >> 1)
	if start > maxInt-count {
		return maxInt
	}
	return start + count
}

func opcodeRegisterEffectsPtr(op opcode) *opcodeRegisterEffects {
	if op >= opcodeLimit {
		return nil
	}
	meta := &opcodeMetadataTable[op]
	if meta.name == "" {
		return nil
	}
	return &meta.registerEffects
}

func instructionHasRegisterEffect(ins instruction, register int, access instructionRegisterAccess) bool {
	if register < 0 || access == 0 {
		return false
	}
	effects := opcodeRegisterEffectsPtr(ins.op)
	if effects == nil {
		return false
	}
	for index := 0; index < int(effects.fixedCount); index++ {
		effect := effects.fixed[index]
		if registerEffectAccessMatches(effect.access, access) && registerEffectAddCount(registerEffectSlotValue(ins, effect.slot), int(effect.offset)) == register {
			return true
		}
	}
	for index := 0; index < int(effects.spanCount); index++ {
		span := effects.spans[index]
		if !registerEffectAccessMatches(span.access, access) {
			continue
		}
		start, end, ok := registerEffectSpanBounds(ins, span, 0, false)
		if !ok {
			continue
		}
		if end == 0 {
			return register >= start
		}
		if register >= start && register < end {
			return true
		}
	}
	return false
}

type instructionRegisterIterator struct {
	ins         instruction
	effects     *opcodeRegisterEffects
	bound       int
	spanCurrent int
	spanEnd     int
	access      instructionRegisterAccess
	fixedIndex  uint8
	spanIndex   uint8
	spanActive  bool
}

// instructionRegisters enumerates the statically named portion of an effect.
// Callers that own a frame or state bound should use instructionRegistersBounded
// so open call and vararg spans cover the complete bounded register window.
func instructionRegisters(ins instruction, access instructionRegisterAccess) instructionRegisterIterator {
	return instructionRegisterIterator{
		ins:     ins,
		access:  access,
		effects: opcodeRegisterEffectsPtr(ins.op),
		bound:   -1,
	}
}

func instructionRegistersBounded(ins instruction, access instructionRegisterAccess, bound int) instructionRegisterIterator {
	return instructionRegisterIterator{
		ins:     ins,
		access:  access,
		effects: opcodeRegisterEffectsPtr(ins.op),
		bound:   bound,
	}
}

func (iterator *instructionRegisterIterator) next() (int, bool) {
	if iterator.effects == nil || iterator.access == 0 {
		return 0, false
	}
	for iterator.fixedIndex < iterator.effects.fixedCount {
		index := iterator.fixedIndex
		effect := iterator.effects.fixed[index]
		iterator.fixedIndex++
		if !registerEffectAccessMatches(effect.access, iterator.access) {
			continue
		}
		register := registerEffectAddCount(registerEffectSlotValue(iterator.ins, effect.slot), int(effect.offset))
		if register < 0 || (iterator.bound >= 0 && register >= iterator.bound) || iterator.fixedEffectBefore(register, index) {
			continue
		}
		return register, true
	}
	return iterator.nextSpan()
}

func (iterator *instructionRegisterIterator) nextSpan() (int, bool) {
	for {
		if iterator.spanActive {
			for iterator.spanCurrent < iterator.spanEnd {
				register := iterator.spanCurrent
				iterator.spanCurrent++
				current := int(iterator.spanIndex) - 1
				if iterator.fixedEffectContains(register) || iterator.spanEffectBefore(register, current) {
					continue
				}
				return register, true
			}
			iterator.spanActive = false
		}
		if iterator.spanIndex >= iterator.effects.spanCount {
			return 0, false
		}

		span := iterator.effects.spans[iterator.spanIndex]
		iterator.spanIndex++
		if !registerEffectAccessMatches(span.access, iterator.access) {
			continue
		}
		if iterator.bound < 0 && span.mode == registerEffectSpanOpenOrOne && registerEffectSlotValue(iterator.ins, span.count) < 0 {
			iterator.bound = instructionRegisterStaticBound(iterator.ins)
		}
		clamp := iterator.bound >= 0
		start, end, ok := registerEffectSpanBounds(iterator.ins, span, iterator.bound, clamp)
		if !ok || (clamp && iterator.bound <= start) {
			continue
		}
		iterator.spanCurrent = start
		iterator.spanEnd = end
		iterator.spanActive = true
	}
}

func (iterator *instructionRegisterIterator) fixedEffectBefore(register int, current uint8) bool {
	for index := uint8(0); index < current; index++ {
		effect := iterator.effects.fixed[index]
		if registerEffectAccessMatches(effect.access, iterator.access) && registerEffectAddCount(registerEffectSlotValue(iterator.ins, effect.slot), int(effect.offset)) == register {
			return true
		}
	}
	return false
}

func (iterator *instructionRegisterIterator) fixedEffectContains(register int) bool {
	for index := uint8(0); index < iterator.effects.fixedCount; index++ {
		effect := iterator.effects.fixed[index]
		if registerEffectAccessMatches(effect.access, iterator.access) && registerEffectAddCount(registerEffectSlotValue(iterator.ins, effect.slot), int(effect.offset)) == register {
			return true
		}
	}
	return false
}

func (iterator *instructionRegisterIterator) spanEffectBefore(register int, current int) bool {
	for index := 0; index < current; index++ {
		span := iterator.effects.spans[index]
		if !registerEffectAccessMatches(span.access, iterator.access) {
			continue
		}
		start, end, ok := registerEffectSpanBounds(iterator.ins, span, iterator.bound, iterator.bound >= 0)
		if ok && register >= start && register < end {
			return true
		}
	}
	return false
}

func instructionRegisterStaticBound(ins instruction) int {
	if ins.op >= opcodeLimit {
		return 0
	}
	meta := &opcodeMetadataTable[ins.op]
	if meta.name == "" {
		return 0
	}

	limit := 0
	operands := [...]struct {
		kind  bytecodeOperandKind
		value int
	}{
		{kind: meta.operands.a, value: ins.a},
		{kind: meta.operands.b, value: ins.b},
		{kind: meta.operands.c, value: ins.c},
		{kind: meta.operands.d, value: ins.d},
	}
	for _, operand := range operands {
		if operand.kind == bytecodeOperandRegister {
			limit = maxRegisterBound(limit, operand.value+1)
		}
	}
	for index := 0; index < int(meta.registerEffects.fixedCount); index++ {
		effect := meta.registerEffects.fixed[index]
		register := registerEffectAddCount(registerEffectSlotValue(ins, effect.slot), int(effect.offset))
		limit = maxRegisterBound(limit, register+1)
	}
	for index := 0; index < int(meta.registerEffects.spanCount); index++ {
		span := meta.registerEffects.spans[index]
		_, end, bounded := registerEffectSpanBounds(ins, span, 0, false)
		if bounded && end > 0 {
			limit = maxRegisterBound(limit, end)
		}
	}
	return limit
}

func maxRegisterBound(current int, candidate int) int {
	if candidate > current {
		return candidate
	}
	return current
}
