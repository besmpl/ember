package ember

import "fmt"

type machineMethodOneActionKind uint8

const (
	machineMethodOneInvalid machineMethodOneActionKind = iota
	machineMethodOneLookup
	machineMethodOneScript
	machineMethodOneHost
	machineMethodOneMetamethod
)

// machineMethodOneAction is the pointer-free CALL_METHOD_ONE contract. A
// lookup action must be resumed before the receiver is inserted. Ready call
// actions retain the original callee so the call engine can preserve nested
// __call argument insertion and cycle errors.
type machineMethodOneAction struct {
	lookup     machineTableAction
	callee     slot
	metamethod slot
	receiver   slot
	callName   machineStringID
	kind       machineMethodOneActionKind
	_          [3]byte
}

func (arena *machineTableArena) prepareMethodOne(receiver slot, key, indexName, callName machineStringID) (machineMethodOneAction, error) {
	if slotValueKind(receiver) != TableKind {
		return machineMethodOneAction{}, fmt.Errorf("run: get field target is %s, want table", slotValueKind(receiver))
	}
	table, err := arena.tableIDFromSlot(receiver)
	if err != nil {
		return machineMethodOneAction{}, err
	}
	lookup, err := arena.decideIndex(table, machineTableStringKey(key), indexName)
	if err != nil {
		return machineMethodOneAction{}, fmt.Errorf("run: get field failed: %w", err)
	}
	action := machineMethodOneAction{lookup: lookup, receiver: receiver, callName: callName}
	if lookup.kind == machineTableActionCall {
		action.kind = machineMethodOneLookup
		return action, nil
	}
	if lookup.kind != machineTableActionReturn {
		return machineMethodOneAction{}, errMachineTableInvalidKey
	}
	return arena.finishMethodOne(action, lookup.value)
}

func (arena *machineTableArena) resumeMethodOneLookup(action machineMethodOneAction, first slot, resultCount uint32) (machineMethodOneAction, error) {
	if action.kind != machineMethodOneLookup || action.lookup.kind != machineTableActionCall {
		return machineMethodOneAction{}, errMachineTableInvalidKey
	}
	result, err := resumeMachineTableAction(action.lookup, first, resultCount)
	if err != nil {
		return machineMethodOneAction{}, err
	}
	return arena.finishMethodOne(action, result.value)
}

func (arena *machineTableArena) finishMethodOne(action machineMethodOneAction, callee slot) (machineMethodOneAction, error) {
	action.lookup = machineTableAction{}
	action.callee = callee
	action.metamethod = slotNil
	switch slotValueKind(callee) {
	case FunctionKind:
		if !machineTableCallable(callee) {
			return machineMethodOneAction{}, fmt.Errorf("call target is %s, want function", slotValueKind(callee))
		}
		action.kind = machineMethodOneScript
		return action, nil
	case HostFuncKind:
		if !machineTableCallable(callee) {
			return machineMethodOneAction{}, fmt.Errorf("call target is %s, want function", slotValueKind(callee))
		}
		action.kind = machineMethodOneHost
		return action, nil
	case TableKind:
		table, err := arena.tableIDFromSlot(callee)
		if err != nil {
			return machineMethodOneAction{}, err
		}
		metamethod, found, err := arena.metamethod(table, action.callName)
		if err != nil {
			return machineMethodOneAction{}, err
		}
		if !found {
			return machineMethodOneAction{}, fmt.Errorf("call target is table, want function")
		}
		if !machineTableCallable(metamethod) {
			if slotValueKind(metamethod) != TableKind {
				return machineMethodOneAction{}, fmt.Errorf("__call is %s, want function", slotValueKind(metamethod))
			}
			nested, err := arena.tableIDFromSlot(metamethod)
			if err != nil {
				return machineMethodOneAction{}, err
			}
			if _, nestedFound, err := arena.metamethod(nested, action.callName); err != nil {
				return machineMethodOneAction{}, err
			} else if !nestedFound {
				return machineMethodOneAction{}, fmt.Errorf("__call is table, want function")
			}
		}
		action.metamethod = metamethod
		action.kind = machineMethodOneMetamethod
		return action, nil
	default:
		return machineMethodOneAction{}, fmt.Errorf("call target is %s, want function", slotValueKind(callee))
	}
}

func machineMethodOneResult(first slot, resultCount uint32) slot {
	if resultCount == 0 {
		return slotNil
	}
	return first
}
