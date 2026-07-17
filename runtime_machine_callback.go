package ember

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type machineCallbackState struct {
	mu       sync.Mutex
	released bool
}

type machineCallbackTarget struct {
	execution *machineRuntimeExecution
	runtime   *Runtime
	callable  machineClosureHandle
	from      moduleKey
	globals   map[string]Value
	state     *machineCallbackState
}

func captureMachineCallback(execution *machineRuntimeExecution, call invocationScope, value Value) (callbackTarget, error) {
	if execution == nil || execution.owner == nil || call.runtime == nil {
		return nil, fmt.Errorf("callback: Machine runtime is unavailable")
	}
	selected, err := call.runtime.executionAdapter()
	if err != nil {
		return nil, fmt.Errorf("callback: capture runtime: %w", err)
	}
	if selected != execution {
		return nil, fmt.Errorf("callback: runtime mismatch")
	}
	owner := execution.owner
	owner.mu.Lock()
	closed := owner.state == machineOwnerClosed
	active := owner.activeRuns != 0
	owner.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("callback: runtime is closed")
	}
	if !active {
		return nil, fmt.Errorf("callback: capture requires an active runtime call")
	}

	ownerCookie := owner.closures.owner
	handle, err := decodeTransientScriptCallableValue(value, ownerCookie, func(candidate scriptCallableHandle) bool {
		if candidate.generation > uint32(^uint16(0)) {
			return false
		}
		_, recordErr := owner.closures.closureRecord(machineClosureHandle{
			owner:      candidate.owner,
			index:      candidate.index,
			generation: uint16(candidate.generation),
		})
		return recordErr == nil
	})
	if err != nil {
		return nil, fmt.Errorf("callback: capture script function: %w", err)
	}
	callable := machineClosureHandle{
		owner:      handle.owner,
		index:      handle.index,
		generation: uint16(handle.generation),
	}
	if err := owner.pinCallbackRoot(callable); err != nil {
		return nil, fmt.Errorf("callback: retain script function: %w", err)
	}
	return &machineCallbackTarget{
		execution: execution,
		runtime:   call.runtime,
		callable:  callable,
		from:      call.from,
		globals:   copyGlobals(call.globals),
		state:     &machineCallbackState{},
	}, nil
}

func (target *machineCallbackTarget) call(ctx context.Context, args []Value) ([]Value, error) {
	if target == nil || target.execution == nil || target.execution.owner == nil || target.runtime == nil || target.state == nil {
		return nil, fmt.Errorf("callback: not retained")
	}
	if !target.execution.usesResumableRequire() {
		return target.callCompletionStopped(ctx, args)
	}
	outcome, err := target.callResumable(ctx, args)
	if err != nil {
		return nil, err
	}
	if outcome.target != nil || len(outcome.pending) != 0 {
		if outcome.target != nil {
			outcome.target.close()
		}
		for _, pending := range outcome.pending {
			if pending.target != nil {
				pending.target.close()
			}
		}
		return nil, fmt.Errorf("callback: suspended in completion-only Call")
	}
	return outcome.values, nil
}

func (target *machineCallbackTarget) callCompletionStopped(ctx context.Context, args []Value) ([]Value, error) {
	lease, err := target.execution.owner.beginRun()
	if errors.Is(err, errMachineOwnerClosed) {
		return nil, fmt.Errorf("callback: runtime is closed")
	}
	if errors.Is(err, errMachineOwnerBusy) {
		return nil, fmt.Errorf("callback: begin call: %w", ErrRuntimeBusy)
	}
	if err != nil {
		return nil, fmt.Errorf("callback: begin call: %w", err)
	}
	defer lease.end()

	target.state.mu.Lock()
	released := target.state.released
	target.state.mu.Unlock()
	if released {
		return nil, fmt.Errorf("callback: released")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	controller, err := newExecutionPolicy(ctx, target.runtime.limits)
	if err != nil {
		return nil, fmt.Errorf("callback: create execution controller: %w", err)
	}
	return target.execution.callCallbackStopped(target.runtime, ctx, target.from, target.globals, target.callable, args, controller)
}

func (target *machineCallbackTarget) close() error {
	if target == nil || target.state == nil {
		return nil
	}
	target.state.mu.Lock()
	if target.state.released {
		target.state.mu.Unlock()
		return nil
	}
	target.state.released = true
	target.state.mu.Unlock()
	if target.execution != nil && target.execution.owner != nil {
		target.execution.owner.unpinCallbackRoot(target.callable)
	}
	return nil
}

func (execution *machineRuntimeExecution) callCallbackStopped(
	runtime *Runtime,
	ctx context.Context,
	from moduleKey,
	globals map[string]Value,
	callable machineClosureHandle,
	args []Value,
	controller *executionController,
) ([]Value, error) {
	owner := execution.owner
	if err := owner.importGlobalsStopped(globals); err != nil {
		return nil, fmt.Errorf("callback: import captured globals: %w", err)
	}
	tableCheckpoint := owner.tables.checkpointStopped()
	defer owner.recycleTransientTablesStopped(tableCheckpoint)
	machineArgs, err := importMachineValuesStopped(owner, args)
	if err != nil {
		return nil, fmt.Errorf("callback: import arguments: %w", err)
	}
	callableSlot, err := slotPackHandle(slotTagClosure, callable.index, callable.generation)
	if err != nil {
		return nil, fmt.Errorf("callback: invalid script function: %w", err)
	}
	callScope := runtime.newInvocationScope(ctx, from, globals, controller)
	if err := owner.executeClosureStopped(
		callableSlot,
		machineArgs,
		controller,
		machineRunEffects{ctx: contextWithInvocationScope(ctx, callScope)},
	); err != nil {
		return nil, fmt.Errorf("callback: call script function: %w", err)
	}
	values, err := owner.exportResults()
	if err != nil {
		return nil, fmt.Errorf("callback: export results: %w", err)
	}
	return values, nil
}
