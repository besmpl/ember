package ember

import (
	"fmt"
	"math"
)

// directLoopKernelOpcode is deliberately limited to direct loop families.
// Calls, returns, yields, closure creation, varargs, globals, allocation, and
// frame transitions remain in the canonical frame runner.
func directLoopKernelOpcode(op opcode) bool {
	switch op {
	case opLoadConst,
		opMove,
		opSetField,
		opSetStringField,
		opSetStringFieldIndex,
		opGetStringField,
		opGetStringFieldIndex,
		opSetIndex,
		opGetIndex,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
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
		opNeg,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opJumpIfNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opJumpIfFalse,
		opJump:
		return true
	default:
		return false
	}
}

type directLoopKernelOp struct {
	op             opcode
	a, b, c, d     int
	cacheID        uint32
	originalPC     int
	nextOriginalPC int
}

type directLoopKernel struct {
	entryPC  int
	wordBase int
	ops      []directLoopKernelOp
	opAtWord []int32
}

func (kernel *directLoopKernel) opIndexAt(pc int) (int32, bool) {
	if kernel == nil || pc < kernel.wordBase || pc-kernel.wordBase >= len(kernel.opAtWord) {
		return -1, false
	}
	index := kernel.opAtWord[pc-kernel.wordBase]
	if index < 0 || int(index) >= len(kernel.ops) {
		return -1, false
	}
	return index, true
}

type directLoopKernelSet struct {
	kernels []directLoopKernel
	atPC    []int32
}

const directLoopKernelMinOps = 8

func (set *directLoopKernelSet) kernelAt(pc int) *directLoopKernel {
	if set == nil || pc < 0 || pc >= len(set.atPC) {
		return nil
	}
	index := set.atPC[pc]
	if index < 0 || int(index) >= len(set.kernels) {
		return nil
	}
	return &set.kernels[index]
}

func buildDirectLoopKernels(code []instruction, boundaries []int, words []wordcodeWord, cacheIndex *wordcodeCacheIndex) (*directLoopKernelSet, error) {
	if len(code) == 0 || len(boundaries) != len(code)+1 || len(words) == 0 {
		return nil, nil
	}
	claimed := make([]bool, len(code))
	set := &directLoopKernelSet{atPC: make([]int32, len(words))}
	for pc := range set.atPC {
		set.atPC[pc] = -1
	}
	for entry, ins := range code {
		if claimed[entry] || ins.op != opArrayNextJump2 || ins.d <= entry || ins.d > len(code) {
			continue
		}
		exit := ins.d
		if exit-entry < directLoopKernelMinOps {
			continue
		}
		sawBackedge := false
		eligible := true
		for pc := entry; pc < exit; pc++ {
			candidate := code[pc]
			if !directLoopKernelOpcode(candidate.op) {
				eligible = false
				break
			}
			var target int
			switch opcodeJumpTarget(candidate.op) {
			case opcodeJumpTargetB:
				target = candidate.b
			case opcodeJumpTargetD:
				target = candidate.d
			default:
				continue
			}
			if target < entry || target > exit {
				eligible = false
				break
			}
			if candidate.op == opJump && target == entry {
				sawBackedge = true
			}
		}
		if !eligible || !sawBackedge {
			continue
		}

		kernel := directLoopKernel{
			entryPC:  boundaries[entry],
			wordBase: boundaries[entry],
			ops:      make([]directLoopKernelOp, 0, exit-entry),
			opAtWord: make([]int32, boundaries[exit]-boundaries[entry]),
		}
		for pc := range kernel.opAtWord {
			kernel.opAtWord[pc] = -1
		}
		for logicalPC := entry; logicalPC < exit; logicalPC++ {
			physicalPC := boundaries[logicalPC]
			dispatch, err := decodeWordcodeDispatch(words, physicalPC, cacheIndex)
			if err != nil {
				return nil, err
			}
			index := len(kernel.ops)
			kernel.ops = append(kernel.ops, directLoopKernelOp{
				op:             dispatch.op,
				a:              dispatch.a,
				b:              dispatch.b,
				c:              dispatch.c,
				d:              dispatch.d,
				cacheID:        dispatch.cacheID,
				originalPC:     physicalPC,
				nextOriginalPC: dispatch.nextWord,
			})
			kernel.opAtWord[physicalPC-kernel.wordBase] = int32(index)
		}
		kernelIndex := len(set.kernels)
		set.kernels = append(set.kernels, kernel)
		set.atPC[kernel.entryPC] = int32(kernelIndex)
		for pc := entry; pc < exit; pc++ {
			claimed[pc] = true
		}
	}
	if len(set.kernels) == 0 {
		return nil, nil
	}
	return set, nil
}

// runDirectLoopKernel executes one statically verified physical-wordcode loop
// region through a predecoded production-only dispatcher. These case bodies
// are the canonical production implementations, including PIC, metatable,
// nil, open-result, and type-check behavior.
func runDirectLoopKernel(thread *vmThread, frame *vmFrame, kernel *directLoopKernel) directFrameSideExit {
	proto := frame.proto
	constants := proto.constants
	constantKeys := proto.constantKeys
	constantKeyOK := proto.constantKeyOK
	constantNumbers := proto.constantNumbers
	constantNumberOK := proto.constantNumberOK
	registers := frame.registers
	var functionInstance *vmFunctionInstance
	pc := kernel.entryPC
	opIndex := int32(0)

	for {
		if opIndex < 0 {
			var ok bool
			opIndex, ok = kernel.opIndexAt(pc)
			if !ok {
				return directFrameExitAt(frame, pc, directFrameResume())
			}
		}
		ins := kernel.ops[opIndex]
		pc = ins.originalPC
		op := ins.op
		a, b, c, d := ins.a, ins.b, ins.c, ins.d
		nextWord := ins.nextOriginalPC
		switch op {
		case opLoadConst:
			registers[a] = constants[b]
		case opMove:
			registers[a] = registers[b]
		case opSetField:
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind())))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[b], registers[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				break
			}
			if constantKeyOK[b] {
				keyValue := constants[b]
				var err error
				if valueKind(keyValue) == StringKind {
					table.setRawStringFieldBox(keyValue.stringText(), keyValue.stringBox(), registers[c])
				} else {
					err = table.rawSetKey(constantKeys[b], registers[c])
				}
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
				}
				break
			}
			if err := table.rawSet(constants[b], registers[c]); err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
			}
		case opSetStringField:
			cacheID := ins.cacheID
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind())))
			}
			keyValue := constants[b]
			key := constantKeys[b].str
			value := registers[c]
			var fieldCache *propertyIC
			if valueKind(keyValue) == StringKind && !value.IsNil() {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				fieldCache = functionInstance.fieldCacheAt(cacheID)
				if fieldCache.write(table, value) || fieldCache.resolveWrite(table, key, keyValue.stringBox(), value) {
					break
				}
				if table.needsDynamicStringFieldCache() {
					cache := functionInstance.cacheAt(cacheID)
					if cache.hasValueKey(table, keyValue) {
						if cache.writeValue(table, keyValue, value) {
							break
						}
					} else {
						slot, slotOK := table.rawStringFieldSlotBox(keyValue.stringBox())
						if !slotOK {
							slot, slotOK = table.rawStringFieldSlot(key)
						}
						if slotOK {
							cache.storeValue(table, keyValue, slot)
							if cache.writeValue(table, keyValue, value) {
								break
							}
							if table.setRawStringFieldAtSlot(slot, key, value) {
								break
							}
						}
					}
				}
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, constants[b], value)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				break
			}
			// The boxed constant is retained for new keys and for the nil/delete
			// path; host-facing raw-string adapters remain text-only.
			table.setRawStringFieldBox(key, keyValue.stringBox(), value)
			fieldCache.observe(table, key, keyValue.stringBox())
		case opSetStringFieldIndex:
			cacheID := ins.cacheID
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set field target is %s, want table", base.Kind())))
			}
			if functionInstance == nil {
				functionInstance = thread.functionInstance(proto)
			}
			firstKey := constantKeys[b].str
			firstBox := constants[b].stringBox()
			fieldCache := functionInstance.fieldCacheAt(cacheID)
			first, ok := fieldCache.get(table)
			if !ok {
				first, ok = fieldCache.resolve(table, firstKey, firstBox)
			}
			if !ok {
				first, ok = table.rawStringFieldBox(firstBox)
				if firstBox == nil {
					first, ok = directFrameRawStringField(table, firstKey)
				}
			}
			if !ok {
				if table.metatable != nil {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
				}
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index target is %s, want table", NilValue().Kind())))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index target is %s, want table", first.Kind())))
			}
			if nextTable.metatable != nil {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
			}
			key := registers[c]
			if valueKind(key) == StringKind {
				cache := functionInstance.cacheAt(cacheID)
				value := registers[d]
				if cache.writeValue(nextTable, key, value) {
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok && nextTable.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(nextTable, key, slot)
					break
				}
			}
			if err := nextTable.rawSet(key, registers[d]); err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index failed: %w", err)))
			}
		case opGetStringField:
			cacheID := ins.cacheID
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind())))
			}
			key := constants[c]
			keyText := constantKeys[c].str
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				fieldCache := functionInstance.fieldCacheAt(cacheID)
				if value, ok := fieldCache.get(table); ok {
					registers[a] = value
					break
				}
				if value, ok := fieldCache.resolve(table, keyText, key.stringBox()); ok {
					registers[a] = value
					break
				}
				if table.needsDynamicStringFieldCache() {
					cache := functionInstance.cacheAt(cacheID)
					if value, ok := cache.getValue(table, key); ok {
						registers[a] = value
						break
					}
					slot, slotOK := table.rawStringFieldSlotBox(key.stringBox())
					if !slotOK {
						slot, slotOK = table.rawStringFieldSlot(keyText)
					}
					if slotOK {
						if value, ok := table.rawStringFieldAtSlot(slot, keyText); ok {
							cache.storeValue(table, key, slot)
							registers[a] = value
							break
						}
					}
				}
			} else if value, ok := directFrameRawStringField(table, keyText); ok {
				// Keep the existing raw-string adapter as a defensive fallback for
				// malformed hand-built prototypes.
				registers[a] = value
				break
			}
			if table.metatable != nil {
				value, ok, err := directFrameTableGetIsland(thread.globals, table, constants[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get field failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				registers[a] = value
				break
			}
			registers[a] = NilValue()
		case opGetStringFieldIndex:
			cacheID := ins.cacheID
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get field target is %s, want table", base.Kind())))
			}
			if functionInstance == nil {
				functionInstance = thread.functionInstance(proto)
			}
			firstKey := constantKeys[c].str
			firstBox := constants[c].stringBox()
			fieldCache := functionInstance.fieldCacheAt(cacheID)
			first, ok := fieldCache.get(table)
			if !ok {
				first, ok = fieldCache.resolve(table, firstKey, firstBox)
			}
			if !ok {
				first, ok = table.rawStringFieldBox(firstBox)
				if firstBox == nil {
					first, ok = directFrameRawStringField(table, firstKey)
				}
			}
			if !ok {
				if table.metatable != nil {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
				}
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index target is %s, want table", NilValue().Kind())))
			}
			nextTable := first.tableRef()
			if nextTable == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index target is %s, want table", first.Kind())))
			}
			if nextTable.metatable != nil {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrameFor(directFrameSideExitReasonMetatable))
			}
			key := registers[d]
			if valueKind(key) == StringKind {
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValue(nextTable, key); ok {
					registers[a] = value
					break
				}
				if slot, ok := nextTable.rawStringFieldSlot(key.stringText()); ok {
					value, ok := nextTable.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(nextTable, key, slot)
						registers[a] = value
						break
					}
				}
			}
			value, err := nextTable.rawGet(key)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index failed: %w", err)))
			}
			registers[a] = value
		case opSetIndex:
			cacheID := ins.cacheID
			base := registers[a]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index target is %s, want table", base.Kind())))
			}
			if table.metatable != nil {
				ok, err := directFrameTableSetIsland(thread.globals, table, registers[b], registers[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				break
			}
			key := registers[b]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				value := registers[c]
				if cache.writeValue(table, key, value) {
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok && table.setRawStringFieldAtSlot(slot, key.stringText(), value) {
					cache.storeValue(table, key, slot)
					break
				}
			}
			if err := table.rawSet(registers[b], registers[c]); err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: set index failed: %w", err)))
			}
		case opGetIndex:
			cacheID := ins.cacheID
			base := registers[b]
			table := base.tableRef()
			if table == nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index target is %s, want table", base.Kind())))
			}
			if table.metatable != nil {
				value, ok, err := directFrameTableGetIsland(thread.globals, table, registers[c])
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index failed: %w", err)))
				}
				if !ok {
					return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
				}
				registers[a] = value
				break
			}
			key := registers[c]
			if valueKind(key) == StringKind {
				if functionInstance == nil {
					functionInstance = thread.functionInstance(proto)
				}
				cache := functionInstance.cacheAt(cacheID)
				if value, ok := cache.getValue(table, key); ok {
					registers[a] = value
					break
				}
				if slot, ok := table.rawStringFieldSlot(key.stringText()); ok {
					value, ok := table.rawStringFieldAtSlot(slot, key.stringText())
					if ok {
						cache.storeValue(table, key, slot)
						registers[a] = value
						break
					}
				}
			} else if valueKind(key) == NumberKind {
				if index, ok := directFrameArrayIndexInBounds(valueNumber(key), len(table.array)); ok {
					registers[a] = table.array[index-1]
					break
				}
			}
			value, err := table.rawGet(key)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: get index failed: %w", err)))
			}
			registers[a] = value
		case opPrepareIter:
			iterValue := registers[a]
			iterTable := iterValue.tableRef()
			if iterTable != nil && iterTable.metatable == nil {
				if tableCanIterateCleanArray(iterTable) {
					registers[a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext)
					registers[b] = iterValue
					registers[c] = NilValue()
					break
				}
				registers[a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncTableNext)
				registers[b] = iterValue
				registers[c] = NilValue()
				break
			}
			generator, state, control, ok, err := prepareIterator(iterValue, thread.globals)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: prepare iterator failed: %w", err)))
			}
			if ok {
				registers[a] = generator
				registers[b] = state
				registers[c] = control
			}
		case opArrayNext:
			callee := registers[b]
			var first Value
			var second Value
			var count int
			var ok bool
			var err error
			if valueNativeID(callee) == nativeFuncArrayNext {
				ok = true
				tableValue := registers[c]
				table := tableValue.tableRef()
				if table == nil {
					err = fmt.Errorf("array iterator: argument #1 is %s, want table", tableValue.Kind())
				} else {
					controlValue := registers[a]
					index := 0
					if valueKind(controlValue) != NilKind {
						if valueKind(controlValue) != NumberKind {
							err = fmt.Errorf("array iterator: index is %s, want number or nil", controlValue.Kind())
						} else {
							index = int(valueNumber(controlValue))
							if float64(index) != valueNumber(controlValue) {
								err = fmt.Errorf("array iterator: index is %s, want integer", controlValue.Kind())
							}
						}
					}
					if err == nil {
						next := index + 1
						if next < 1 || next > len(table.array) {
							first = NilValue()
							count = 1
						} else {
							first = NumberValue(float64(next))
							second = table.array[next-1]
							count = 2
						}
					}
				}
			} else {
				first, second, count, ok, err = directFrameIteratorNext(callee, registers[c], registers[a])
			}
			if !ok {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
			}
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
			}
			frame.clearOpenResultState()
			for i := 0; i < d; i++ {
				if i >= count {
					registers[a+i] = NilValue()
					continue
				}
				if i == 0 {
					registers[a+i] = first
				} else {
					registers[a+i] = second
				}
			}
		case opArrayNextJump2:
			callee := registers[b]
			if valueNativeID(callee) == nativeFuncArrayNext {
				tableValue := registers[c]
				table := tableValue.tableRef()
				if table == nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: argument #1 is %s, want table", tableValue.Kind())))
				}
				controlValue := registers[a]
				index := 0
				if valueKind(controlValue) != NilKind {
					if valueKind(controlValue) != NumberKind {
						return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want number or nil", controlValue.Kind())))
					}
					index = int(valueNumber(controlValue))
					if float64(index) != valueNumber(controlValue) {
						return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: array iterator: index is %s, want integer", controlValue.Kind())))
					}
				}
				if frame.openRangeOwner != nil {
					frame.clearOpenResultRange()
				}
				frame.openResultStart = -1
				frame.openResults = vmResultWindow{}
				next := index + 1
				if next < 1 || next > len(table.array) {
					registers[a] = NilValue()
					registers[a+1] = NilValue()
					pc = d
					opIndex = -1
					continue
				}
				registers[a] = NumberValue(float64(next))
				registers[a+1] = table.array[next-1]
				break
			}
			first, second, count, ok, err := directFrameIteratorNext(callee, registers[c], registers[a])
			if !ok {
				return directFrameExitAt(frame, pc, directFrameEnterGenericFrame())
			}
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: call failed: host function failed: %w", err)))
			}
			if frame.openRangeOwner != nil {
				frame.clearOpenResultRange()
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			if count < 1 || first.IsNil() {
				registers[a] = NilValue()
				registers[a+1] = NilValue()
				pc = d
				opIndex = -1
				continue
			}
			registers[a] = first
			if count > 1 {
				registers[a+1] = second
			} else {
				registers[a+1] = NilValue()
			}
		case opAdd:
			left := registers[b]
			right := registers[c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__add",
					"add",
					func(left float64, right float64) float64 { return left + right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: add failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) + valueNumber(right))
		case opSub:
			left := registers[b]
			right := registers[c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__sub",
					"subtract",
					func(left float64, right float64) float64 { return left - right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: subtract failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - valueNumber(right))
		case opMul:
			left := registers[b]
			right := registers[c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mul",
					"multiply",
					func(left float64, right float64) float64 { return left * right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: multiply failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) * valueNumber(right))
		case opDiv:
			left := registers[b]
			right := registers[c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__div",
					"divide",
					func(left float64, right float64) float64 { return left / right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) / valueNumber(right))
		case opMod:
			left := registers[b]
			right := registers[c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mod",
					"modulo",
					math.Mod,
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: modulo failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/valueNumber(right))*valueNumber(right))
		case opIDiv:
			left := registers[b]
			right := registers[c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__idiv",
					"floor divide",
					func(left float64, right float64) float64 { return math.Floor(left / right) },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: floor divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Floor(valueNumber(left) / valueNumber(right)))
		case opAddK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__add",
					"add",
					func(left float64, right float64) float64 { return left + right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: add failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) + constantNumbers[c])
		case opSubK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__sub",
					"subtract",
					func(left float64, right float64) float64 { return left - right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: subtract failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) - constantNumbers[c])
		case opMulK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mul",
					"multiply",
					func(left float64, right float64) float64 { return left * right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: multiply failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) * constantNumbers[c])
		case opDivK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__div",
					"divide",
					func(left float64, right float64) float64 { return left / right },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(valueNumber(left) / constantNumbers[c])
		case opModK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__mod",
					"modulo",
					math.Mod,
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: modulo failed: %w", err)))
				}
				registers[a] = value
				break
			}
			right := constantNumbers[c]
			registers[a] = NumberValue(valueNumber(left) - math.Floor(valueNumber(left)/right)*right)
		case opIDivK:
			left := registers[b]
			if valueKind(left) != NumberKind || !constantNumberOK[c] {
				right := constants[c]
				value, err := directFrameBinaryArithmeticValueUncounted(
					thread.globals,
					left,
					right,
					"__idiv",
					"floor divide",
					func(left float64, right float64) float64 { return math.Floor(left / right) },
				)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: floor divide failed: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(math.Floor(valueNumber(left) / constantNumbers[c]))
		case opNeg:
			operand := registers[b]
			if valueKind(operand) != NumberKind {
				value, err := directFrameUnaryArithmeticValueUncounted(thread.globals, operand, negateValue)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: %w", err)))
				}
				registers[a] = value
				break
			}
			registers[a] = NumberValue(-valueNumber(operand))
		case opEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: equal failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valuesEqual(left, right))
		case opNotEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == TableKind || valueKind(right) == TableKind || valueKind(left) == UserDataKind || valueKind(right) == UserDataKind {
				value, err := equalValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: equal failed: %w", err)))
				}
				registers[a] = BoolValue(!value)
				break
			}
			registers[a] = BoolValue(!valuesEqual(left, right))
		case opLess:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() < right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) < valueNumber(right))
		case opLessEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() <= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessEqualValue(left, right, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less equal failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) <= valueNumber(right))
		case opGreater:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() > right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessValue(right, left, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) > valueNumber(right))
		case opGreaterEqual:
			left := registers[b]
			right := registers[c]
			if valueKind(left) == StringKind && valueKind(right) == StringKind {
				registers[a] = BoolValue(left.stringText() >= right.stringText())
				break
			}
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind || math.IsNaN(valueNumber(left)) || math.IsNaN(valueNumber(right)) {
				value, err := lessEqualValue(right, left, thread.globals)
				if err != nil {
					return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater equal failed: %w", err)))
				}
				registers[a] = BoolValue(value)
				break
			}
			registers[a] = BoolValue(valueNumber(left) >= valueNumber(right))
		case opJumpIfNotEqualK:
			left := registers[a]
			if valueKind(left) == NumberKind && constantNumberOK[b] {
				if valueNumber(left) != constantNumbers[b] {
					pc = d
					opIndex = -1
					continue
				}
				break
			}
			if valueKind(left) == StringKind && constantKeyOK[b] {
				if left.stringText() != constantKeys[b].str {
					pc = d
					opIndex = -1
					continue
				}
				break
			}
			right := constants[b]
			equal, err := equalValue(left, right, thread.globals)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: equal failed: %w", err)))
			}
			if !equal {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfTableHasMetatable:
			base := registers[a]
			if table := base.tableRef(); table != nil && table.metatable != nil {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfNotLessK:
			left := registers[a]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, constants[b])
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if !less {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfNotGreaterK:
			left := registers[a]
			greater, err := directFrameLessForBranchUncounted(thread.globals, constants[b], left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if !greater {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfLessK:
			left := registers[a]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, constants[b])
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if less {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfGreaterK:
			left := registers[a]
			greater, err := directFrameLessForBranchUncounted(thread.globals, constants[b], left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if greater {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfNotLess:
			left := registers[a]
			right := registers[b]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, right)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if !less {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfNotGreater:
			left := registers[a]
			right := registers[b]
			greater, err := directFrameLessForBranchUncounted(thread.globals, right, left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if !greater {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfLess:
			left := registers[a]
			right := registers[b]
			less, err := directFrameLessForBranchUncounted(thread.globals, left, right)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: less failed: %w", err)))
			}
			if less {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfGreater:
			left := registers[a]
			right := registers[b]
			greater, err := directFrameLessForBranchUncounted(thread.globals, right, left)
			if err != nil {
				return directFrameExitAt(frame, pc, directFrameFail(fmt.Errorf("run: greater failed: %w", err)))
			}
			if greater {
				pc = d
				opIndex = -1
				continue
			}
		case opJumpIfFalse:
			if !registers[a].truthy() {
				pc = b
				opIndex = -1
				continue
			}
		case opJump:
			pc = b
			opIndex = -1
			continue
		}
		opIndex++
		if int(opIndex) >= len(kernel.ops) {
			return directFrameExitAt(frame, nextWord, directFrameResume())
		}
	}
}
