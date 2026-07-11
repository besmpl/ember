package ember

import (
	"fmt"
)

// coldInstructionAction is the small result protocol between the direct
// dispatcher and the side-exit interpreter.  The direct loop has already
// charged the current instruction and run its hooks; the cold island only
// executes the operation that caused the exit and publishes the next pc.
type coldInstructionActionKind uint8

const (
	coldInstructionActionResume coldInstructionActionKind = iota
	coldInstructionActionContinue
	coldInstructionActionCall
	coldInstructionActionYield
	coldInstructionActionReturn
	coldInstructionActionError
)

type coldInstructionAction struct {
	kind   coldInstructionActionKind
	result vmFrameResult
	err    error
}

func coldInstructionResume(frame *vmFrame) coldInstructionAction {
	frame.pc++
	return coldInstructionAction{kind: coldInstructionActionResume}
}

func coldInstructionContinue(frame *vmFrame, pc int) coldInstructionAction {
	frame.pc = pc
	return coldInstructionAction{kind: coldInstructionActionContinue}
}

func coldInstructionError(err error) coldInstructionAction {
	return coldInstructionAction{kind: coldInstructionActionError, err: err}
}

func coldInstructionResult(result vmFrameResult) coldInstructionAction {
	return coldInstructionAction{kind: coldInstructionActionReturn, result: result}
}

func coldInstructionCallResult(result vmFrameResult, done bool, err error) coldInstructionAction {
	if err != nil {
		return coldInstructionError(err)
	}
	if done {
		if result.state == vmCallStateYielded {
			return coldInstructionAction{kind: coldInstructionActionYield, result: result}
		}
		return coldInstructionResult(result)
	}
	return coldInstructionAction{kind: coldInstructionActionCall}
}

func (thread *vmThread) runColdInstruction(frame *vmFrame) coldInstructionAction {
	if frame == nil || frame.proto == nil || frame.pc < 0 || frame.pc >= len(frame.proto.packedCode) {
		return coldInstructionError(fmt.Errorf("run: cold instruction pc %d out of range", frame.pc))
	}

	proto := frame.proto
	globals := thread.globals
	packed := proto.packedCode[frame.pc]
	a, b, c, d := int(packed.a), int(packed.b), int(packed.c), int(packed.d)

	switch packed.op {
	case opSetField:
		table, ok := frame.register(a).Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: set field target is %s, want table", frame.register(a).Kind()))
		}
		if table.metatable == nil && proto.constantKeyOK[b] {
			key := proto.constants[b]
			var err error
			if valueKind(key) == StringKind {
				table.setRawStringFieldBox(key.stringText(), key.stringBox(), frame.register(c))
			} else {
				err = table.rawSetKey(proto.constantKeys[b], frame.register(c))
			}
			if err != nil {
				return coldInstructionError(fmt.Errorf("run: set field failed: %w", err))
			}
			return coldInstructionResume(frame)
		}
		if err := runtimeTableAccess(globals).set(table, proto.constants[b], frame.register(c)); err != nil {
			return coldInstructionError(fmt.Errorf("run: set field failed: %w", err))
		}
		return coldInstructionResume(frame)

	case opSetStringField:
		table, ok := frame.register(a).Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: set field target is %s, want table", frame.register(a).Kind()))
		}
		key := proto.constants[b]
		if table.metatable == nil {
			table.setRawStringFieldBox(key.stringText(), key.stringBox(), frame.register(c))
			return coldInstructionResume(frame)
		}
		if err := runtimeTableAccess(globals).set(table, key, frame.register(c)); err != nil {
			return coldInstructionError(fmt.Errorf("run: set field failed: %w", err))
		}
		return coldInstructionResume(frame)

	case opSetStringFieldIndex:
		table, ok := frame.register(a).Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: set field target is %s, want table", frame.register(a).Kind()))
		}
		first, err := runtimeTableAccess(globals).getString(table, proto.constantKeys[b].str, proto.constants[b])
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: get field failed: %w", err))
		}
		nextTable, ok := first.Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: set index target is %s, want table", first.Kind()))
		}
		if err := runtimeTableAccess(globals).set(nextTable, frame.register(c), frame.register(d)); err != nil {
			return coldInstructionError(fmt.Errorf("run: set index failed: %w", err))
		}
		return coldInstructionResume(frame)

	case opGetStringField:
		table, ok := frame.register(b).Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: get field target is %s, want table", frame.register(b).Kind()))
		}
		value, err := runtimeTableAccess(globals).get(table, proto.constants[c])
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: get field failed: %w", err))
		}
		frame.setRegister(a, value)
		return coldInstructionResume(frame)

	case opGetStringFieldIndex:
		table, ok := frame.register(b).Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: get field target is %s, want table", frame.register(b).Kind()))
		}
		first, err := runtimeTableAccess(globals).getString(table, proto.constantKeys[c].str, proto.constants[c])
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: get field failed: %w", err))
		}
		nextTable, ok := first.Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: get index target is %s, want table", first.Kind()))
		}
		value, err := runtimeTableAccess(globals).get(nextTable, frame.register(d))
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: get index failed: %w", err))
		}
		frame.setRegister(a, value)
		return coldInstructionResume(frame)

	case opSetIndex:
		table, ok := frame.register(a).Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: set index target is %s, want table", frame.register(a).Kind()))
		}
		if err := runtimeTableAccess(globals).set(table, frame.register(b), frame.register(c)); err != nil {
			return coldInstructionError(fmt.Errorf("run: set index failed: %w", err))
		}
		return coldInstructionResume(frame)

	case opGetIndex:
		table, ok := frame.register(b).Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: get index target is %s, want table", frame.register(b).Kind()))
		}
		value, err := runtimeTableAccess(globals).get(table, frame.register(c))
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: get index failed: %w", err))
		}
		frame.setRegister(a, value)
		return coldInstructionResume(frame)

	case opArrayNext, opArrayNextJump2:
		callee := frame.register(b)
		destination := vmResultDestination{register: a, count: d}
		if packed.op == opArrayNextJump2 {
			destination.count = 2
		}
		results, count, ok, err := inlineNativeIteratorNext(callee, frame.register(c), frame.register(a))
		if ok {
			if err != nil {
				return coldInstructionError(fmt.Errorf("run: call failed: host function failed: %w", err))
			}
			frame.openResultStart = -1
			frame.openResults = vmResultWindow{}
			for i := 0; i < destination.count; i++ {
				if i >= count {
					frame.setRegister(a+i, NilValue())
				} else {
					frame.setRegister(a+i, results[i])
				}
			}
		} else {
			callResult, done, callErr := frame.callValueToDestination(callee, globals, frame.scriptCallArgs(c, 2), destination)
			if done || callErr != nil {
				return coldInstructionCallResult(callResult, done, callErr)
			}
		}
		if packed.op == opArrayNextJump2 && frame.register(a).IsNil() {
			return coldInstructionContinue(frame, d)
		}
		return coldInstructionResume(frame)

	case opNumericForLoop:
		loopValue := frame.register(a)
		stepValue := frame.register(b)
		if valueKind(loopValue) != NumberKind {
			return coldInstructionError(fmt.Errorf("run: numeric for loop value is %s, want number", loopValue.Kind()))
		}
		if valueKind(stepValue) != NumberKind {
			return coldInstructionError(fmt.Errorf("run: numeric for step is %s, want number", stepValue.Kind()))
		}
		frame.setRegister(a, NumberValue(valueNumber(loopValue)+valueNumber(stepValue)))
		return coldInstructionContinue(frame, d)

	case opFastCall:
		result, done, err := thread.runColdFastCall(frame, nativeFuncID(b), a, c, d)
		if done || err != nil {
			return coldInstructionCallResult(result, done, err)
		}
		return coldInstructionResume(frame)

	case opCall:
		callee := frame.register(b)
		destination := vmResultDestination{register: a, count: d}
		if c >= 0 {
			done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, b+1, c, destination)
			if err != nil {
				return coldInstructionError(fmt.Errorf("run: call failed: %w", err))
			}
			if done {
				return coldInstructionResume(frame)
			}
		}
		var args []Value
		if c < 0 {
			prefixCount := -c - 1
			if frame.openResultStart == b+1+prefixCount {
				if _, ok := callee.scriptFunction(); ok && prefixCount == 0 && globals != nil && globals.thread != nil {
					args = frame.openResults.borrowedValues()
				} else {
					args = make([]Value, 0, prefixCount+frame.openResults.len())
					for register := b + 1; register <= b+prefixCount; register++ {
						args = append(args, frame.register(register))
					}
					args = frame.openResults.appendTo(args)
				}
			} else {
				args = frame.retainedFixedCallArgs(b+1, prefixCount).values
			}
		} else if _, ok := callee.scriptFunction(); ok && globals != nil && globals.thread != nil {
			args = frame.borrowedFixedCallArgs(b+1, c).values
		} else {
			args = frame.retainedFixedCallArgs(b+1, c).values
		}
		result, done, err := frame.callValueToDestination(callee, globals, args, destination)
		if done || err != nil {
			return coldInstructionCallResult(result, done, err)
		}
		return coldInstructionResume(frame)

	case opCallOne:
		callee := frame.register(b)
		argCount, _ := decodeFixedCallCount(c)
		destination := vmResultDestination{register: a, count: 1}
		done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, b+1, argCount, destination)
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: call failed: %w", err))
		}
		if done {
			return coldInstructionResume(frame)
		}
		args := frame.retainedFixedCallArgs(b+1, argCount).values
		result, done, err := frame.callValueToDestination(callee, globals, args, destination)
		if done || err != nil {
			return coldInstructionCallResult(result, done, err)
		}
		return coldInstructionResume(frame)

	case opCallLocalOne:
		callee := frame.register(b)
		argCount, _ := decodeFixedCallCount(d)
		destination := vmResultDestination{register: a, count: 1}
		done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, c, argCount, destination)
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: call failed: %w", err))
		}
		if done {
			return coldInstructionResume(frame)
		}
		args := frame.retainedFixedCallArgs(c, argCount).values
		result, done, err := frame.callValueToDestination(callee, globals, args, destination)
		if done || err != nil {
			return coldInstructionCallResult(result, done, err)
		}
		return coldInstructionResume(frame)

	case opCallUpvalueOne:
		callee, err := frame.upvalue(b)
		if err != nil {
			return coldInstructionError(err)
		}
		argCount, _ := decodeFixedCallCount(d)
		destination := vmResultDestination{register: a, count: 1}
		done, err := frame.callFixedTableScriptCallMetamethod(callee, globals, c, argCount, destination)
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: call failed: %w", err))
		}
		if done {
			return coldInstructionResume(frame)
		}
		args := frame.retainedFixedCallArgs(c, argCount).values
		result, done, err := frame.callValueToDestination(callee, globals, args, destination)
		if done || err != nil {
			return coldInstructionCallResult(result, done, err)
		}
		return coldInstructionResume(frame)

	case opCallMethodOne:
		receiver := frame.register(b)
		table, ok := receiver.Table()
		if !ok {
			return coldInstructionError(fmt.Errorf("run: get field target is %s, want table", receiver.Kind()))
		}
		callee, err := runtimeTableAccess(globals).get(table, proto.constants[c])
		if err != nil {
			return coldInstructionError(fmt.Errorf("run: get field failed: %w", err))
		}
		frame.setRegister(a+1, receiver)
		explicitCount, _ := decodeFixedCallCount(d)
		argCount := explicitCount + 1
		destination := vmResultDestination{register: a, count: 1}
		args := frame.retainedFixedCallArgs(a+1, argCount).values
		result, done, err := frame.callValueToDestination(callee, globals, args, destination)
		if done || err != nil {
			return coldInstructionCallResult(result, done, err)
		}
		return coldInstructionResume(frame)

	default:
		return coldInstructionError(fmt.Errorf("run: unsupported cold opcode %d", packed.op))
	}
}

func (frame *vmFrame) scriptCallArgs(start int, count int) []Value {
	return frame.borrowedFixedCallArgs(start, count).values
}

type vmFixedArgWindow struct {
	values   []Value
	borrowed bool
}

func (frame *vmFrame) borrowedFixedCallArgs(start int, count int) vmFixedArgWindow {
	if count == 0 {
		return vmFixedArgWindow{}
	}
	if !frame.hasCellsInRange(start, count) {
		return vmFixedArgWindow{values: frame.registers[start : start+count], borrowed: true}
	}
	return frame.retainedFixedCallArgs(start, count)
}

func (frame *vmFrame) retainedFixedCallArgs(start int, count int) vmFixedArgWindow {
	return vmFixedArgWindow{values: frame.copiedCallArgs(start, count)}
}

func (frame *vmFrame) copiedCallArgs(start int, count int) []Value {
	args := make([]Value, count)
	for i := range args {
		args[i] = frame.register(start + i)
	}
	return args
}

func (frame *vmFrame) hasCellsInRange(start int, count int) bool {
	if len(frame.cells) == 0 {
		return false
	}
	for i := 0; i < count; i++ {
		index := start + i
		if index < len(frame.cells) && frame.cells[index] != nil {
			return true
		}
	}
	return false
}
