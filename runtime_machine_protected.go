package ember

import (
	"errors"
	"fmt"
)

type machineProtectedMode uint8

const (
	machineProtectedPCall machineProtectedMode = iota + 1
	machineProtectedXPCall
)

type machineProtectedStage uint8

const (
	machineProtectedFunction machineProtectedStage = iota + 1
	machineProtectedHandler
)

type machineProtectedSuspension struct {
	mode    machineProtectedMode
	stage   machineProtectedStage
	child   machineCoroutineHandle
	handler slot
	token   any
}

type machineProtectedStep struct {
	values    []machineTransferValue
	failure   error
	token     any
	suspended bool
}

func (machine *scalarMachine) callProtectedNativeStopped(
	nativeID nativeFuncID,
	arguments []slot,
	destination,
	resultCount,
	returnPC int,
) error {
	state, values, err := machine.newProtectedCallStopped(nativeID, arguments)
	if err != nil {
		return err
	}
	if machine.window.controller != nil {
		if err := machine.window.controller.enterCall(); err != nil {
			return err
		}
		defer machine.window.controller.leaveCall()
	}
	step, err := machine.runProtectedChildWithCallerStopped(state.child, values, nil)
	if err != nil {
		return err
	}
	outputs, suspended, err := machine.resolveProtectedStepWithCallerStopped(state, step)
	if err != nil {
		return err
	}
	if suspended {
		return machine.suspendHostCallStopped(state, destination, resultCount, returnPC)
	}
	return applyMachineCoroutineValuesStopped(machine, destination, resultCount, outputs)
}

func (machine *scalarMachine) newProtectedCallStopped(
	nativeID nativeFuncID,
	arguments []slot,
) (*machineProtectedSuspension, []machineTransferValue, error) {
	name := "pcall"
	mode := machineProtectedPCall
	if nativeID == nativeFuncXPCall {
		name = "xpcall"
		mode = machineProtectedXPCall
	}
	if len(arguments) == 0 {
		return nil, nil, fmt.Errorf("%s: missing function", name)
	}
	if slotTagOf(arguments[0]) != slotTagClosure {
		return nil, nil, fmt.Errorf("%s: argument is %s, want function", name, slotValueKind(arguments[0]))
	}
	argStart := 1
	handler := slotNil
	if mode == machineProtectedXPCall {
		if len(arguments) < 2 {
			return nil, nil, fmt.Errorf("xpcall: missing error handler")
		}
		if slotTagOf(arguments[1]) != slotTagClosure {
			return nil, nil, fmt.Errorf("xpcall: argument #2 is %s, want function", slotValueKind(arguments[1]))
		}
		handler = arguments[1]
		argStart = 2
	}
	child, err := machine.newProtectedChildStopped(arguments[0])
	if err != nil {
		return nil, nil, err
	}
	values, err := captureMachineCoroutineValuesStopped(machine, nil, arguments[argStart:])
	if err != nil {
		_ = machine.persistentOwner.coroutines.closeCoroutineStopped(child)
		return nil, nil, err
	}
	return &machineProtectedSuspension{
		mode:    mode,
		stage:   machineProtectedFunction,
		child:   child,
		handler: handler,
	}, values, nil
}

func (machine *scalarMachine) newProtectedChildStopped(callable slot) (machineCoroutineHandle, error) {
	if machine == nil || machine.persistentOwner == nil {
		return machineCoroutineHandle{}, errMachineOwnerInvalid
	}
	closure, module, proto, err := machine.closureTarget(callable)
	if err != nil {
		return machineCoroutineHandle{}, err
	}
	return machine.persistentOwner.coroutines.createStopped(machineCoroutineRoot{
		module:  module,
		proto:   proto,
		closure: closure,
	})
}

func (machine *scalarMachine) runProtectedChildWithCallerStopped(
	handle machineCoroutineHandle,
	values []machineTransferValue,
	failure error,
) (machineProtectedStep, error) {
	parent := machine.activeCoroutine
	action, err := machine.persistentOwner.coroutines.beginResumeStopped(
		parent,
		handle,
		machine.window.controller,
		machine.effects,
		values,
	)
	if err != nil {
		return machineProtectedStep{}, err
	}
	callerController := machine.window.controller
	callerEffects := machine.effects
	callerCoroutine := machine.activeCoroutine
	var caller machineCoroutineSnapshot
	if err := captureMachineCoroutineStopped(machine, machineCoroutineFrameState{}, &caller); err != nil {
		_, _ = machine.persistentOwner.coroutines.failStopped(handle, err)
		return machineProtectedStep{}, err
	}
	step, runErr := machine.runProtectedChildActionStopped(action, values, failure)
	clear(machine.closures.openCells)
	machine.closures.openCells = machine.closures.openCells[:0]
	if err := restoreMachineCoroutineStopped(machine, caller); err != nil {
		return machineProtectedStep{}, err
	}
	machine.window = newExecutionWindow(callerController)
	machine.effects = callerEffects
	machine.activeCoroutine = callerCoroutine
	machine.restartPC = 0
	return step, runErr
}

func (machine *scalarMachine) runProtectedChildStopped(
	handle machineCoroutineHandle,
	values []machineTransferValue,
	controller *executionController,
	effects machineRunEffects,
	failure error,
) (machineProtectedStep, error) {
	action, err := machine.persistentOwner.coroutines.beginResumeStopped(
		machineCoroutineHandle{},
		handle,
		controller,
		effects,
		values,
	)
	if err != nil {
		return machineProtectedStep{}, err
	}
	return machine.runProtectedChildActionStopped(action, values, failure)
}

func (machine *scalarMachine) runProtectedChildActionStopped(
	action machineCoroutineAction,
	values []machineTransferValue,
	failure error,
) (machineProtectedStep, error) {
	handle := action.handle
	if err := machine.enterCoroutineActionStopped(action, values); err != nil {
		_, _ = machine.persistentOwner.coroutines.failStopped(handle, err)
		return machineProtectedStep{}, err
	}
	if failure != nil {
		pc := int(action.frame.pc)
		if pc > 0 {
			pc--
		}
		wrapped := machine.wrapError(pc, fmt.Errorf("run: call failed: %w", failure))
		exit, _ := machine.persistentOwner.coroutines.failStopped(handle, wrapped)
		return machineProtectedStep{failure: exit.err}, nil
	}
	errorPC, runErr := runGeneratedScalarMachineLoop(machine)
	if signal := new(machineCoroutineLoopSignal); errors.As(runErr, &signal) {
		if signal.hostToken == nil {
			_ = machine.persistentOwner.coroutines.closeCoroutineStopped(handle)
			return machineProtectedStep{failure: errors.New("protected call yielded without a host suspension")}, nil
		}
		return machineProtectedStep{
			token:     signal.hostToken,
			suspended: true,
		}, nil
	}
	if runErr != nil {
		wrapped := machine.wrapError(errorPC, runErr)
		exit, _ := machine.persistentOwner.coroutines.failStopped(handle, wrapped)
		return machineProtectedStep{failure: exit.err}, nil
	}
	machine.window.commit()
	transfers, err := captureMachineCoroutineValuesStopped(machine, nil, machine.results[:machine.resultCount])
	if err != nil {
		_, _ = machine.persistentOwner.coroutines.failStopped(handle, err)
		return machineProtectedStep{}, err
	}
	if _, err := machine.persistentOwner.coroutines.returnStopped(handle, transfers); err != nil {
		return machineProtectedStep{}, err
	}
	return machineProtectedStep{values: transfers}, nil
}

func (machine *scalarMachine) resolveProtectedStepWithCallerStopped(
	state *machineProtectedSuspension,
	step machineProtectedStep,
) ([]machineTransferValue, bool, error) {
	for {
		if step.suspended {
			state.token = step.token
			return nil, true, nil
		}
		if step.failure == nil {
			return machine.protectedSuccessValues(state, step.values), false, nil
		}
		if isProtectedBoundaryError(step.failure) {
			return nil, false, step.failure
		}
		if state.mode != machineProtectedXPCall || state.stage == machineProtectedHandler {
			values, err := machine.protectedFailureValues(step.failure)
			return values, false, err
		}
		child, err := machine.newProtectedChildStopped(state.handler)
		if err != nil {
			return nil, false, err
		}
		state.stage = machineProtectedHandler
		state.child = child
		argument, err := machine.protectedFailureArgument(step.failure)
		if err != nil {
			return nil, false, err
		}
		step, err = machine.runProtectedChildWithCallerStopped(child, argument, nil)
		if err != nil {
			return nil, false, err
		}
	}
}

func (machine *scalarMachine) protectedSuccessValues(
	state *machineProtectedSuspension,
	values []machineTransferValue,
) []machineTransferValue {
	success := state.stage == machineProtectedFunction
	result := make([]machineTransferValue, 1, len(values)+1)
	result[0] = machineTransferValue{value: slotBool(success)}
	return append(result, values...)
}

func (machine *scalarMachine) protectedFailureValues(failure error) ([]machineTransferValue, error) {
	argument, err := machine.protectedFailureArgument(failure)
	if err != nil {
		return nil, err
	}
	return append([]machineTransferValue{{value: slotBool(false)}}, argument...), nil
}

func (machine *scalarMachine) protectedFailureArgument(failure error) ([]machineTransferValue, error) {
	id, err := machine.strings.internStringStopped(failure.Error())
	if err != nil {
		return nil, err
	}
	value, err := slotPackHandle(slotTagString, uint32(id), 1)
	if err != nil {
		return nil, err
	}
	return []machineTransferValue{{value: value}}, nil
}

func (owner *machineOwner) resumeProtectedHostCoroutineStopped(
	parent machineCoroutineHandle,
	state *machineProtectedSuspension,
	values []machineTransferValue,
	controller *executionController,
	effects machineRunEffects,
	failure error,
) (resumableOutcome, error) {
	if owner == nil || state == nil || state.child.index == 0 {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if controller != nil {
		if err := controller.enterCall(); err != nil {
			return resumableOutcome{}, err
		}
		defer controller.leaveCall()
	}
	step, err := owner.scalarMachine.runProtectedChildStopped(
		state.child,
		values,
		controller,
		effects,
		failure,
	)
	if err != nil {
		return resumableOutcome{}, err
	}
	for {
		if step.suspended {
			state.token = step.token
			return resumableOutcome{
				target: machineResumableTargetMarker{},
				token:  state,
			}, nil
		}
		if step.failure == nil {
			outputs := owner.scalarMachine.protectedSuccessValues(state, step.values)
			return owner.resumeHostCoroutineTransfersStopped(parent, outputs, controller, effects, nil)
		}
		if isProtectedBoundaryError(step.failure) {
			return resumableOutcome{}, step.failure
		}
		if state.mode != machineProtectedXPCall || state.stage == machineProtectedHandler {
			outputs, err := owner.scalarMachine.protectedFailureValues(step.failure)
			if err != nil {
				return resumableOutcome{}, err
			}
			return owner.resumeHostCoroutineTransfersStopped(parent, outputs, controller, effects, nil)
		}
		child, err := owner.scalarMachine.newProtectedChildStopped(state.handler)
		if err != nil {
			return resumableOutcome{}, err
		}
		state.stage = machineProtectedHandler
		state.child = child
		argument, err := owner.scalarMachine.protectedFailureArgument(step.failure)
		if err != nil {
			return resumableOutcome{}, err
		}
		step, err = owner.scalarMachine.runProtectedChildStopped(child, argument, controller, effects, nil)
		if err != nil {
			return resumableOutcome{}, err
		}
	}
}
