package ember

import "fmt"

// machineMetatableIntrinsicRequest is the pointer-free input for guarded base
// setmetatable/getmetatable calls. Dispatch supplies stable scalar arguments;
// a callee identity miss returns without validation or mutation.
type machineMetatableIntrinsicRequest struct {
	callee        slot
	first         slot
	second        slot
	argumentCount uint32
	nativeID      nativeFuncID
	_             [3]byte
}

type machineMetatableIntrinsicOutcome struct {
	value       slot
	resultCount uint8
	matched     bool
	_           [6]byte
}

func runMachineGuardedMetatableIntrinsicStopped(arena *machineTableArena, request machineMetatableIntrinsicRequest) (machineMetatableIntrinsicOutcome, error) {
	if request.callee != slotNativeID(request.nativeID) {
		return machineMetatableIntrinsicOutcome{}, nil
	}
	outcome := machineMetatableIntrinsicOutcome{value: slotNil, matched: true}
	if arena == nil {
		return outcome, fmt.Errorf("compact Machine metatable intrinsic is unavailable")
	}
	switch request.nativeID {
	case nativeFuncSetMetatable:
		plan, err := arena.prepareSetMetatable(request.first, request.second, request.argumentCount)
		if err != nil {
			return outcome, err
		}
		value, err := arena.applySetMetatableStopped(plan)
		if err != nil {
			return outcome, err
		}
		outcome.value = value
		outcome.resultCount = 1
		return outcome, nil
	case nativeFuncGetMetatable:
		value, err := arena.getMetatableValue(request.first, request.argumentCount)
		if err != nil {
			return outcome, err
		}
		outcome.value = value
		outcome.resultCount = 1
		return outcome, nil
	default:
		return outcome, fmt.Errorf("compact Machine metatable intrinsic ID %d is unsupported", request.nativeID)
	}
}
