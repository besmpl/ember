package ember

import (
	"context"
	"errors"
	"fmt"
)

type machineResumableTarget struct {
	execution *machineRuntimeExecution
	runtime   *Runtime
	from      moduleKey
	globals   map[string]Value
	handle    machineCoroutineHandle
	protected *machineProtectedSuspension
	closed    bool
}

func newMachineResumableTarget(
	execution *machineRuntimeExecution,
	runtime *Runtime,
	from moduleKey,
	globals map[string]Value,
	callable slot,
) (*machineResumableTarget, error) {
	if execution == nil || execution.owner == nil || runtime == nil {
		return nil, fmt.Errorf("resumable Machine call is unavailable")
	}
	closure, module, proto, err := execution.owner.scalarMachine.closureTarget(callable)
	if err != nil {
		return nil, err
	}
	handle, err := execution.owner.coroutines.createStopped(machineCoroutineRoot{
		module:  module,
		proto:   proto,
		closure: closure,
	})
	if err != nil {
		return nil, err
	}
	return &machineResumableTarget{
		execution: execution,
		runtime:   runtime,
		from:      from,
		globals:   copyGlobals(globals),
		handle:    handle,
	}, nil
}

func (target *machineResumableTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	if target == nil || target.closed || target.execution == nil || target.execution.owner == nil || target.runtime == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	owner := target.execution.owner
	lease, err := owner.beginRun()
	if errors.Is(err, errMachineOwnerClosed) {
		return resumableOutcome{}, fmt.Errorf("runtime: closed")
	}
	if errors.Is(err, errMachineOwnerBusy) {
		return resumableOutcome{}, fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
	}
	if err != nil {
		return resumableOutcome{}, err
	}
	defer lease.end()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return resumableOutcome{}, err
	}
	controller, err := newExecutionPolicy(ctx, target.runtime.limits)
	if err != nil {
		return resumableOutcome{}, err
	}
	if err := owner.importGlobalsStopped(target.globals); err != nil {
		return resumableOutcome{}, err
	}
	imported, err := importMachineValuesStopped(owner, values)
	if err != nil {
		return resumableOutcome{}, err
	}
	transfers, err := captureMachineCoroutineValuesStopped(&owner.scalarMachine, owner.scalarMachine.coroutineTransfer[:0], imported)
	if err != nil {
		return resumableOutcome{}, err
	}
	scope := target.runtime.newInvocationScope(ctx, target.from, target.globals, controller)
	effects := machineRunEffects{ctx: contextWithInvocationScope(ctx, scope)}
	var outcome resumableOutcome
	if target.protected != nil {
		outcome, err = owner.resumeProtectedHostCoroutineStopped(
			target.handle,
			target.protected,
			transfers,
			controller,
			effects,
			failure,
		)
	} else {
		outcome, err = owner.resumeHostCoroutineTransfersStopped(
			target.handle,
			transfers,
			controller,
			effects,
			failure,
		)
	}
	if err != nil {
		target.close()
		return resumableOutcome{}, err
	}
	if outcome.target != nil {
		if protected, ok := outcome.token.(*machineProtectedSuspension); ok {
			target.protected = protected
			outcome.token = protected.token
		} else {
			target.protected = nil
		}
		outcome.target = target
		return outcome, nil
	}
	target.protected = nil
	target.close()
	return outcome, nil
}

func (owner *machineOwner) resumeHostCoroutineStopped(
	handle machineCoroutineHandle,
	values []machineTransferValue,
	controller *executionController,
	effects machineRunEffects,
	failure error,
) (resumableOutcome, error) {
	return owner.resumeHostCoroutineTransfersStopped(handle, values, controller, effects, failure)
}

func (owner *machineOwner) resumeHostCoroutineTransfersStopped(
	handle machineCoroutineHandle,
	values []machineTransferValue,
	controller *executionController,
	effects machineRunEffects,
	failure error,
) (resumableOutcome, error) {
	action, err := owner.coroutines.beginResumeStopped(machineCoroutineHandle{}, handle, controller, effects, values)
	if err != nil {
		return resumableOutcome{}, err
	}
	machine := &owner.scalarMachine
	if err := machine.enterCoroutineActionStopped(action, values); err != nil {
		_, _ = owner.coroutines.failStopped(handle, err)
		return resumableOutcome{}, err
	}
	if failure != nil {
		pc := int(action.frame.pc)
		if pc > 0 {
			pc--
		}
		wrapped := machine.wrapError(pc, fmt.Errorf("run: call failed: %w", failure))
		_, _ = owner.coroutines.failStopped(handle, wrapped)
		clear(machine.closures.openCells)
		machine.closures.openCells = machine.closures.openCells[:0]
		return resumableOutcome{}, wrapped
	}
	errorPC, runErr := runGeneratedScalarMachineLoop(machine)
	if signal := new(machineCoroutineLoopSignal); errors.As(runErr, &signal) {
		return resumableOutcome{
			target: machineResumableTargetMarker{},
			token:  signal.hostToken,
		}, nil
	}
	if runErr != nil {
		wrapped := machine.wrapError(errorPC, runErr)
		_, _ = owner.coroutines.failStopped(handle, wrapped)
		return resumableOutcome{}, wrapped
	}
	machine.window.commit()
	transfers, err := captureMachineCoroutineValuesStopped(machine, machine.coroutineTransfer[:0], machine.results[:machine.resultCount])
	if err != nil {
		_, _ = owner.coroutines.failStopped(handle, err)
		return resumableOutcome{}, err
	}
	if _, err := owner.coroutines.returnStopped(handle, transfers); err != nil {
		return resumableOutcome{}, err
	}
	results, err := owner.exportResults()
	if err != nil {
		return resumableOutcome{}, err
	}
	return resumableOutcome{values: results}, nil
}

// machineResumableTargetMarker is replaced by the owning target before the
// outcome crosses the engine boundary.
type machineResumableTargetMarker struct{}

func (machineResumableTargetMarker) resume(context.Context, []Value, error) (resumableOutcome, error) {
	return resumableOutcome{}, ErrSuspensionStale
}

func (machineResumableTargetMarker) close() {}

func (target *machineResumableTarget) close() {
	if target == nil || target.closed {
		return
	}
	target.closed = true
	if target.execution != nil && target.execution.owner != nil {
		if target.protected != nil {
			_ = target.execution.owner.coroutines.closeCoroutineStopped(target.protected.child)
		}
		_ = target.execution.owner.coroutines.closeCoroutineStopped(target.handle)
	}
	target.protected = nil
	target.globals = nil
	target.runtime = nil
	target.execution = nil
}

func (target *machineCallbackTarget) callResumable(ctx context.Context, args []Value) (resumableOutcome, error) {
	if target == nil || target.execution == nil || target.execution.owner == nil || target.runtime == nil || target.state == nil {
		return resumableOutcome{}, fmt.Errorf("callback: not retained")
	}
	target.state.mu.Lock()
	released := target.state.released
	target.state.mu.Unlock()
	if released {
		return resumableOutcome{}, fmt.Errorf("callback: released")
	}
	callable, err := slotPackHandle(slotTagClosure, target.callable.index, target.callable.generation)
	if err != nil {
		return resumableOutcome{}, err
	}
	resumable, err := newMachineResumableTarget(
		target.execution,
		target.runtime,
		target.from,
		target.globals,
		callable,
	)
	if err != nil {
		return resumableOutcome{}, fmt.Errorf("callback: create resumable call: %w", err)
	}
	outcome, err := resumable.resume(ctx, args, nil)
	if err != nil {
		resumable.close()
		return resumableOutcome{}, fmt.Errorf("callback: call script function: %w", err)
	}
	return outcome, nil
}

type machineHookRun struct {
	execution *machineRuntimeExecution
	runtime   *Runtime
	hook      string
	args      []Value
	report    HookReport
	index     int
	current   *machineResumableTarget
	call      HookCallReport
}

func (execution *machineRuntimeExecution) runHookResumable(runtime *Runtime, ctx context.Context, hook string, args []Value) (resumableOutcome, error) {
	run := &machineHookRun{
		execution: execution,
		runtime:   runtime,
		hook:      hook,
		args:      append([]Value(nil), args...),
		report:    HookReport{Hook: hook},
	}
	return run.resume(ctx, nil, nil)
}

func (run *machineHookRun) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	if run == nil || run.runtime == nil || run.execution == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if run.current != nil {
		outcome, err := run.current.resume(ctx, values, failure)
		if err != nil {
			run.close()
			return resumableOutcome{}, fmt.Errorf("runtime: call hook %s.%s: %w", run.call.Entrypoint, run.hook, err)
		}
		if outcome.target != nil {
			return resumableOutcome{target: run, token: outcome.token}, nil
		}
		run.call.Called = true
		run.report.Calls = append(run.report.Calls, run.call)
		run.current = nil
		run.index++
	}
	for run.index < len(run.execution.image.entrypoints) {
		entrypoint := run.execution.image.entrypoints[run.index]
		call, target, err := run.prepareEntrypoint(ctx, entrypoint)
		if err != nil {
			run.close()
			return resumableOutcome{}, err
		}
		if target == nil {
			run.report.Calls = append(run.report.Calls, call)
			run.index++
			continue
		}
		run.call = call
		run.current = target
		outcome, err := target.resume(ctx, run.args, nil)
		if err != nil {
			run.close()
			return resumableOutcome{}, fmt.Errorf("runtime: call hook %s.%s: %w", call.Entrypoint, run.hook, err)
		}
		if outcome.target != nil {
			return resumableOutcome{target: run, token: outcome.token}, nil
		}
		run.call.Called = true
		run.report.Calls = append(run.report.Calls, run.call)
		run.current = nil
		run.index++
	}
	report := run.report
	run.close()
	return resumableOutcome{hook: &report}, nil
}

func (run *machineHookRun) prepareEntrypoint(
	ctx context.Context,
	entrypoint programImageEntrypoint,
) (HookCallReport, *machineResumableTarget, error) {
	module := run.execution.image.modules[entrypoint.moduleID]
	moduleID := moduleIDFromKey(module.key)
	call := HookCallReport{
		Entrypoint: entrypoint.name,
		Module:     moduleID,
		Hook:       run.hook,
	}
	lease, err := run.execution.owner.beginRun()
	if err != nil {
		return call, nil, err
	}
	defer lease.end()
	if ctx == nil {
		ctx = context.Background()
	}
	if run.hook == "" {
		return call, nil, fmt.Errorf("runtime: empty hook")
	}
	controller, err := newExecutionPolicy(ctx, run.runtime.limits)
	if err != nil {
		return call, nil, err
	}
	loadGlobals, err := run.runtime.hostGlobals(ctx, HostCall{Entrypoint: entrypoint.name, Module: moduleID})
	if err != nil {
		return call, nil, err
	}
	if err := run.execution.owner.importGlobalsStopped(loadGlobals); err != nil {
		return call, nil, err
	}
	loadScope := run.runtime.newInvocationScope(ctx, module.key, loadGlobals, controller)
	export, loaded, err := run.execution.owner.loadModuleStopped(
		entrypoint.moduleID,
		controller,
		machineRunEffects{ctx: contextWithInvocationScope(ctx, loadScope)},
	)
	if err != nil {
		return call, nil, err
	}
	call.Loaded = loaded
	hookGlobals, err := run.runtime.hostGlobals(ctx, HostCall{
		Entrypoint: entrypoint.name,
		Module:     moduleID,
		Hook:       run.hook,
	})
	if err != nil {
		return call, nil, err
	}
	if err := run.execution.owner.importGlobalsStopped(hookGlobals); err != nil {
		return call, nil, err
	}
	hookValue, skipped, err := machineHookValueStopped(run.execution.owner, export, run.hook)
	if err != nil {
		return call, nil, err
	}
	if skipped {
		call.Skipped = true
		return call, nil, nil
	}
	if slotValueKind(hookValue) != FunctionKind {
		return call, nil, fmt.Errorf("runtime: resumable hook %s.%s is %s, want function", entrypoint.name, run.hook, slotValueKind(hookValue))
	}
	target, err := newMachineResumableTarget(run.execution, run.runtime, module.key, hookGlobals, hookValue)
	return call, target, err
}

func (run *machineHookRun) close() {
	if run == nil {
		return
	}
	if run.current != nil {
		run.current.close()
	}
	run.current = nil
	run.runtime = nil
	run.execution = nil
	run.args = nil
}
