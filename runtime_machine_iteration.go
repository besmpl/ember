package ember

import (
	"fmt"
	"math"
)

type machineIteratorMode uint8

const (
	machineIteratorInvalid machineIteratorMode = iota
	machineIteratorArray
	machineIteratorGeneric
)

// machineIteratorPlan is the pointer-free PREPARE_ITER outcome. A ready plan
// can be written directly to the generator/state/control registers. A call
// action asks the outer execution layer to invoke __iter instead.
type machineIteratorPlan struct {
	action    machineTableAction
	generator slot
	state     slot
	control   slot
	mode      machineIteratorMode
	ready     uint8
	_         [6]byte
}

// machineIteratorStep is the native ARRAY_NEXT result before fixed-result
// adjustment or ARRAY_NEXT_JUMP2 branching is applied by dispatch.
type machineIteratorStep struct {
	cursor machineTableCursor
	key    slot
	value  slot
	count  uint8
	done   uint8
	_      [6]byte
}

type machineStringFieldIndexOperation uint8

const (
	machineStringFieldIndexInvalid machineStringFieldIndexOperation = iota
	machineStringFieldIndexGet
	machineStringFieldIndexSet
)

// machineStringFieldIndexContinuation resumes the second half of a fused
// field/index operation after a callable __index resolves the first field.
type machineStringFieldIndexContinuation struct {
	key          machineTableKey
	value        slot
	indexName    machineStringID
	newIndexName machineStringID
	operation    machineStringFieldIndexOperation
	active       uint8
	_            [2]byte
}

func (arena *machineTableArena) prepareIterator(value slot, iterName machineStringID) (machineIteratorPlan, error) {
	if slotValueKind(value) != TableKind {
		return machineIteratorPlan{}, nil
	}
	id, err := arena.tableIDFromSlot(value)
	if err != nil {
		return machineIteratorPlan{}, err
	}
	record, ok := arena.lookup(id)
	if !ok {
		return machineIteratorPlan{}, errMachineTableInvalidID
	}
	mode := machineIteratorGeneric
	generator := slotNativeID(nativeFuncTableNext)
	if record.metatable != invalidMachineTableID {
		if iterName == invalidMachineStringID {
			return machineIteratorPlan{}, errMachineTableInvalidKey
		}
		handler, found, err := arena.metamethod(id, iterName)
		if err != nil {
			return machineIteratorPlan{}, err
		}
		if found {
			return machineIteratorPlan{action: machineTableAction{
				kind:     machineTableActionCall,
				event:    machineTableEventIter,
				table:    id,
				value:    value,
				callable: handler,
			}}, nil
		}
		generator = slotNativeID(nativeFuncNext)
	} else if record.fieldCount == 0 && record.entryCount == record.arrayLength {
		mode = machineIteratorArray
		generator = slotNativeID(nativeFuncArrayNext)
	}
	return machineIteratorPlan{
		generator: generator,
		state:     value,
		control:   slotNil,
		mode:      mode,
		ready:     1,
	}, nil
}

func (arena *machineTableArena) arrayNext(id machineTableID, cursor machineTableCursor) (machineIteratorStep, error) {
	record, ok := arena.lookup(id)
	if !ok {
		return machineIteratorStep{}, arena.tableError()
	}
	if cursor.set > 1 || (cursor.set != 0 && cursor.key.kind != machineTableKeyArray) {
		return machineIteratorStep{}, errMachineTableInvalidKey
	}
	previous := uint32(0)
	if cursor.set != 0 {
		previous = cursor.key.id
	}
	if previous == math.MaxUint32 || previous+1 > record.arrayLength {
		return machineIteratorStep{key: slotNil, value: slotNil, count: 1, done: 1}, nil
	}
	index := previous + 1
	key := machineTableArrayKey(index)
	return machineIteratorStep{
		cursor: machineTableCursor{key: key, index: index - 1, set: 1},
		key:    slot(math.Float64bits(float64(index))),
		value:  arena.arrays[int(record.arrayOffset+index-1)],
		count:  2,
	}, nil
}

func (arena *machineTableArena) genericNext(id machineTableID, cursor machineTableCursor) (machineIteratorStep, error) {
	key, value, next, ok, err := arena.next(id, cursor)
	if err != nil {
		return machineIteratorStep{}, err
	}
	if !ok {
		return machineIteratorStep{key: slotNil, value: slotNil, count: 1, done: 1}, nil
	}
	keyValue, err := machineTableKeyValue(key)
	if err != nil {
		return machineIteratorStep{}, err
	}
	return machineIteratorStep{cursor: next, key: keyValue, value: value, count: 2}, nil
}

func (arena *machineTableArena) iteratorNext(mode machineIteratorMode, id machineTableID, cursor machineTableCursor) (machineIteratorStep, error) {
	switch mode {
	case machineIteratorArray:
		return arena.arrayNext(id, cursor)
	case machineIteratorGeneric:
		return arena.genericNext(id, cursor)
	default:
		return machineIteratorStep{}, errMachineTableInvalidKey
	}
}

// arrayNextJump2 supplies ARRAY_NEXT_JUMP2's fused branch predicate. Dispatch
// writes key/value from the step and jumps exactly when jump is true.
func (arena *machineTableArena) arrayNextJump2(mode machineIteratorMode, id machineTableID, cursor machineTableCursor) (machineIteratorStep, bool, error) {
	step, err := arena.iteratorNext(mode, id, cursor)
	if err != nil {
		return machineIteratorStep{}, false, err
	}
	return step, step.done != 0, nil
}

func machineTableKeyValue(key machineTableKey) (slot, error) {
	switch key.kind {
	case machineTableKeyArray:
		if key.id == 0 {
			return slotNil, errMachineTableInvalidKey
		}
		return slot(math.Float64bits(float64(key.id))), nil
	case machineTableKeySlot:
		if key.value == slotNil {
			return slotNil, errMachineTableInvalidKey
		}
		return key.value, nil
	case machineTableKeyString:
		if key.id == 0 {
			return slotNil, errMachineTableInvalidKey
		}
		return slotPackHandle(slotTagString, key.id, 1)
	default:
		return slotNil, errMachineTableInvalidKey
	}
}

func (arena *machineTableArena) decideGetStringFieldIndex(base machineTableID, field machineStringID, key machineTableKey, indexName machineStringID) (machineTableAction, machineStringFieldIndexContinuation, error) {
	first, err := arena.decideIndex(base, machineTableStringKey(field), indexName)
	if err != nil {
		return machineTableAction{}, machineStringFieldIndexContinuation{}, err
	}
	if first.kind == machineTableActionCall {
		return first, machineStringFieldIndexContinuation{
			key:       key,
			indexName: indexName,
			operation: machineStringFieldIndexGet,
			active:    1,
		}, nil
	}
	if first.kind != machineTableActionReturn {
		return machineTableAction{}, machineStringFieldIndexContinuation{}, errMachineTableInvalidKey
	}
	action, err := arena.finishStringFieldIndex(first.value, machineStringFieldIndexContinuation{
		key:       key,
		indexName: indexName,
		operation: machineStringFieldIndexGet,
		active:    1,
	})
	return action, machineStringFieldIndexContinuation{}, err
}

func (arena *machineTableArena) decideSetStringFieldIndex(base machineTableID, field machineStringID, key machineTableKey, value slot, indexName, newIndexName machineStringID) (machineTableAction, machineStringFieldIndexContinuation, error) {
	first, err := arena.decideIndex(base, machineTableStringKey(field), indexName)
	if err != nil {
		return machineTableAction{}, machineStringFieldIndexContinuation{}, err
	}
	continuation := machineStringFieldIndexContinuation{
		key:          key,
		value:        value,
		indexName:    indexName,
		newIndexName: newIndexName,
		operation:    machineStringFieldIndexSet,
		active:       1,
	}
	if first.kind == machineTableActionCall {
		return first, continuation, nil
	}
	if first.kind != machineTableActionReturn {
		return machineTableAction{}, machineStringFieldIndexContinuation{}, errMachineTableInvalidKey
	}
	action, err := arena.finishStringFieldIndex(first.value, continuation)
	return action, machineStringFieldIndexContinuation{}, err
}

func (arena *machineTableArena) resumeStringFieldIndex(continuation machineStringFieldIndexContinuation, first slot) (machineTableAction, error) {
	if continuation.active == 0 {
		return machineTableAction{}, errMachineTableInvalidKey
	}
	return arena.finishStringFieldIndex(first, continuation)
}

func (arena *machineTableArena) finishStringFieldIndex(first slot, continuation machineStringFieldIndexContinuation) (machineTableAction, error) {
	nested, err := arena.tableIDFromSlot(first)
	if err != nil {
		operation := "get"
		if continuation.operation == machineStringFieldIndexSet {
			operation = "set"
		}
		return machineTableAction{}, fmt.Errorf("%s index target is %s, want table", operation, slotValueKind(first))
	}
	switch continuation.operation {
	case machineStringFieldIndexGet:
		return arena.decideIndex(nested, continuation.key, continuation.indexName)
	case machineStringFieldIndexSet:
		return arena.decideNewIndex(nested, continuation.key, continuation.value, continuation.newIndexName)
	default:
		return machineTableAction{}, errMachineTableInvalidKey
	}
}
