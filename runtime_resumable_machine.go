package ember

import (
	"context"
	"errors"
	"fmt"
)

type machineResumableTarget struct {
	execution  *machineRuntimeExecution
	runtime    *Runtime
	from       moduleKey
	modulePath []moduleKey
	inherited  []ScriptFrame
	globals    map[string]Value
	handle     machineCoroutineHandle
	protected  *machineProtectedSuspension
	closed     bool
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
	targetGlobals := copyGlobals(globals)
	if targetGlobals == nil {
		targetGlobals = make(map[string]Value, 1)
	}
	targetGlobals["require"] = runtime.requireAdapter(from)
	return &machineResumableTarget{
		execution: execution,
		runtime:   runtime,
		from:      from,
		globals:   targetGlobals,
		handle:    handle,
	}, nil
}

func (target *machineResumableTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *machineResumableTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *machineResumableTarget) resumeRun(ctx context.Context, values []Value, failure error, acquireLease bool) (resumableOutcome, error) {
	if target == nil || target.closed || target.execution == nil || target.execution.owner == nil || target.runtime == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	owner := target.execution.owner
	if acquireLease {
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
	}
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
	if len(target.inherited) != 0 {
		if controller == nil {
			controller, err = newExecutionController(ctx, target.runtime.limits)
			if err != nil {
				return resumableOutcome{}, err
			}
		}
		controller.inheritedScriptFrames = append(controller.inheritedScriptFrames[:0], target.inherited...)
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
	scope.modulePath = append([]moduleKey(nil), target.modulePath...)
	scope.resumable = true
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
	return target.callResumableRun(ctx, args, false)
}

func (target *machineCallbackTarget) callResumableRun(ctx context.Context, args []Value, stopped bool) (resumableOutcome, error) {
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
	call := &moduleCallTarget{runtime: target.runtime, target: resumable}
	outcome, err := resumeResumableTarget(call, ctx, args, nil, stopped)
	if err != nil {
		call.close()
		return resumableOutcome{}, fmt.Errorf("callback: call script function: %w", err)
	}
	return outcome, nil
}

func newMachineModuleResumableTarget(
	execution *machineRuntimeExecution,
	runtime *Runtime,
	moduleID programModuleID,
	from moduleKey,
	globals map[string]Value,
) (*machineResumableTarget, error) {
	if execution == nil || execution.owner == nil || runtime == nil {
		return nil, fmt.Errorf("resumable Machine module is unavailable")
	}
	closure, err := execution.owner.scalarMachine.closures.createClosureStopped(moduleID, 0, nil)
	if err != nil {
		return nil, err
	}
	handle, err := execution.owner.coroutines.createStopped(machineCoroutineRoot{
		module:  moduleID,
		proto:   0,
		closure: closure,
	})
	if err != nil {
		return nil, err
	}
	targetGlobals := copyGlobals(globals)
	if targetGlobals == nil {
		targetGlobals = make(map[string]Value, 1)
	}
	targetGlobals["require"] = runtime.requireAdapter(from)
	return &machineResumableTarget{
		execution: execution,
		runtime:   runtime,
		from:      from,
		globals:   targetGlobals,
		handle:    handle,
	}, nil
}

type machineEntrypointStage uint8

const (
	machineEntrypointLoading machineEntrypointStage = iota
	machineEntrypointCallingHook
)

// machineEntrypointTarget is the Machine counterpart of vmEntrypointTarget:
// module initialization and the exported function call form one host-driven
// operation.
type machineEntrypointTarget struct {
	execution  *machineRuntimeExecution
	runtime    *Runtime
	entrypoint programImageEntrypoint
	hook       string
	args       []Value
	call       *HookCallReport
	globals    map[string]Value
	explicit   bool
	direct     bool
	required   bool
	inner      *machineResumableTarget
	moduleWait *runtimeModuleWait
	stage      machineEntrypointStage
	started    bool
	closed     bool
}

func (target *machineEntrypointTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *machineEntrypointTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *machineEntrypointTarget) resumeRun(ctx context.Context, values []Value, failure error, stopped bool) (resumableOutcome, error) {
	if target == nil || target.closed || target.execution == nil || target.execution.owner == nil || target.runtime == nil || target.call == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if !target.started {
		target.started = true
		return target.startLoad(ctx, stopped)
	}
	if target.moduleWait != nil {
		initialization := target.moduleWait.initialization
		if target.moduleWait.owner {
			if err := initialization.resumeHost(ctx, values, failure, stopped); err != nil {
				target.close()
				return resumableOutcome{}, target.wrapError(err)
			}
			if !initialization.done {
				token, _ := target.moduleWait.visibleToken()
				return resumableOutcome{target: target, token: token}, nil
			}
		} else if !initialization.done {
			return resumableOutcome{}, ErrSuspensionPending
		}
		target.moduleWait = nil
		return target.continueAfterModuleWait(ctx, initialization, stopped)
	}
	if target.inner == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	outcome, err := resumeResumableTarget(target.inner, ctx, values, failure, stopped)
	if err != nil {
		target.close()
		return resumableOutcome{}, target.wrapError(err)
	}
	return target.acceptInner(ctx, outcome, stopped)
}

func (target *machineEntrypointTarget) startLoad(ctx context.Context, stopped bool) (resumableOutcome, error) {
	if target.hook == "" && !target.direct {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: empty operation")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		target.close()
		return resumableOutcome{}, err
	}
	if _, cached, err := target.runtime.cachedResumableModule(target.execution.image.modules[target.entrypoint.moduleID].key); err != nil {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: load entrypoint %s: %w", target.entrypoint.name, err)
	} else if cached {
		return target.startLoadedHook(ctx, stopped)
	}
	loadGlobals, err := target.callGlobals(ctx, "")
	if err != nil {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: host globals for %s load: %w", target.entrypoint.name, err)
	}
	module := target.execution.image.modules[target.entrypoint.moduleID]
	scope := target.runtime.newInvocationScope(ctx, module.key, loadGlobals, nil)
	_, wait, started, err := target.runtime.startModuleInitialization(scope, module.key, stopped)
	if err != nil {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: load entrypoint %s: %w", target.entrypoint.name, err)
	}
	target.call.Loaded = started
	if wait != nil {
		target.setModuleWait(wait)
		token, _ := wait.visibleToken()
		return resumableOutcome{target: target, token: token}, nil
	}
	return target.startLoadedHook(ctx, stopped)
}

func (target *machineEntrypointTarget) acceptInner(ctx context.Context, outcome resumableOutcome, stopped bool) (resumableOutcome, error) {
	if outcome.target != nil {
		target.inner = outcome.target.(*machineResumableTarget)
		token := outcome.token
		if request, ok := token.(*runtimeModuleRequest); ok {
			export, wait, _, err := target.runtime.startModuleInitialization(request.scope, request.key, stopped)
			if err != nil {
				outcome, err = resumeResumableTarget(target.inner, ctx, nil, err, stopped)
			} else if wait != nil {
				target.setModuleWait(wait)
				token, _ = wait.visibleToken()
				return resumableOutcome{target: target, token: token}, nil
			} else {
				outcome, err = resumeResumableTarget(target.inner, ctx, []Value{export}, nil, stopped)
			}
			if err != nil {
				target.close()
				return resumableOutcome{}, target.wrapError(err)
			}
			return target.acceptInner(ctx, outcome, stopped)
		}
		if wait, ok := token.(*runtimeModuleWait); ok {
			target.setModuleWait(wait)
			token, _ = wait.visibleToken()
		}
		return resumableOutcome{target: target, token: token}, nil
	}
	if target.stage == machineEntrypointCallingHook {
		target.call.Called = true
		target.finish()
		return resumableOutcome{values: outcome.values}, nil
	}
	return resumableOutcome{}, fmt.Errorf("runtime: entrypoint load completed outside module initializer")
}

func (target *machineEntrypointTarget) hostVisible() bool {
	if target == nil || target.closed {
		return false
	}
	if target.moduleWait == nil {
		return true
	}
	_, visible := target.moduleWait.visibleToken()
	return visible
}

func (target *machineEntrypointTarget) setModuleWait(wait *runtimeModuleWait) {
	target.moduleWait = wait
	if wait != nil {
		wait.bind(target.close)
	}
}

func (target *machineEntrypointTarget) bindDependencyState(state *suspensionState, onCancel func()) {
	if target == nil || target.moduleWait == nil {
		return
	}
	target.moduleWait.bindDependencyState(state)
	target.moduleWait.bind(onCancel)
}

func (target *machineEntrypointTarget) pump(ctx context.Context, stopped bool) (resumableOutcome, bool, error) {
	if target == nil || target.moduleWait == nil || target.moduleWait.initialization == nil {
		return resumableOutcome{}, false, nil
	}
	initialization := target.moduleWait.initialization
	if !initialization.done {
		advanced, err := initialization.pump(ctx, stopped)
		if err != nil {
			target.close()
			return resumableOutcome{}, true, target.wrapError(err)
		}
		if !advanced || !initialization.done {
			return resumableOutcome{}, advanced, nil
		}
	}
	target.moduleWait = nil
	outcome, err := target.continueAfterModuleWait(ctx, initialization, stopped)
	return outcome, true, err
}

func (target *machineEntrypointTarget) continueAfterModuleWait(ctx context.Context, initialization *runtimeModuleInitialization, stopped bool) (resumableOutcome, error) {
	if target.stage == machineEntrypointLoading {
		if initialization.err != nil {
			target.close()
			return resumableOutcome{}, target.wrapError(initialization.err)
		}
		return target.startLoadedHook(ctx, stopped)
	}
	outcome, err := resumeResumableTarget(target.inner, ctx, []Value{initialization.export}, initialization.err, stopped)
	if err != nil {
		target.close()
		return resumableOutcome{}, target.wrapError(err)
	}
	return target.acceptInner(ctx, outcome, stopped)
}

func (target *machineEntrypointTarget) startLoadedHook(ctx context.Context, stopped bool) (resumableOutcome, error) {
	export, ok, err := target.execution.owner.modules.export(target.entrypoint.moduleID)
	if err != nil {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: load entrypoint %s: %w", target.entrypoint.name, err)
	}
	if !ok {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: load entrypoint %s: module is not loaded", target.entrypoint.name)
	}
	return target.startHook(ctx, export, stopped)
}

func (target *machineEntrypointTarget) startHook(ctx context.Context, export slot, stopped bool) (resumableOutcome, error) {
	target.stage = machineEntrypointCallingHook
	hookGlobals, err := target.callGlobals(ctx, target.hook)
	if err != nil {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: host globals for %s.%s: %w", target.entrypoint.name, target.hook, err)
	}
	hookValue := export
	if !target.direct {
		var skipped bool
		hookValue, skipped, err = machineHookValueStopped(target.execution.owner, export, target.hook)
		if err != nil {
			target.close()
			return resumableOutcome{}, fmt.Errorf("runtime: module %s returned %s, want table or nil", target.call.Module.String(), slotValueKind(export))
		}
		if skipped {
			if target.required {
				target.close()
				return resumableOutcome{}, fmt.Errorf("runtime: module %s has no export %q", target.call.Module.String(), target.hook)
			}
			target.call.Skipped = true
			target.finish()
			return resumableOutcome{}, nil
		}
	}
	if slotValueKind(hookValue) != FunctionKind {
		target.close()
		if target.explicit {
			if target.direct {
				return resumableOutcome{}, fmt.Errorf("runtime: module %s returned %s, want script function", target.call.Module.String(), slotValueKind(hookValue))
			}
			return resumableOutcome{}, fmt.Errorf("runtime: module %s export %q is %s, want script function", target.call.Module.String(), target.hook, slotValueKind(hookValue))
		}
		return resumableOutcome{}, fmt.Errorf("runtime: resumable operation %s.%s is %s, want script function", target.entrypoint.name, target.hook, slotValueKind(hookValue))
	}
	module := target.execution.image.modules[target.entrypoint.moduleID]
	target.inner, err = newMachineResumableTarget(target.execution, target.runtime, module.key, hookGlobals, hookValue)
	if err != nil {
		target.close()
		return resumableOutcome{}, err
	}
	outcome, err := resumeResumableTarget(target.inner, ctx, target.args, nil, stopped)
	if err != nil {
		target.close()
		return resumableOutcome{}, target.wrapError(err)
	}
	return target.acceptInner(ctx, outcome, stopped)
}

func (target *machineEntrypointTarget) wrapError(err error) error {
	if target.explicit {
		if target.stage != machineEntrypointCallingHook {
			return fmt.Errorf("runtime: load module %s: %w", target.call.Module.String(), err)
		}
		if target.direct {
			return fmt.Errorf("runtime: call module %s: %w", target.call.Module.String(), err)
		}
		return fmt.Errorf("runtime: call module %s export %q: %w", target.call.Module.String(), target.hook, err)
	}
	if target.stage == machineEntrypointCallingHook {
		return fmt.Errorf("runtime: call operation %s.%s: %w", target.entrypoint.name, target.hook, err)
	}
	return fmt.Errorf("runtime: load entrypoint %s: %w", target.entrypoint.name, err)
}

func (target *machineEntrypointTarget) finish() {
	target.closed = true
	target.inner = nil
	target.runtime = nil
	target.execution = nil
	target.args = nil
	target.globals = nil
}

func (target *machineEntrypointTarget) callGlobals(ctx context.Context, operation string) (map[string]Value, error) {
	if target.explicit {
		return copyGlobals(target.globals), nil
	}
	return target.runtime.hostGlobals(ctx, newHostCall(target.entrypoint.name, target.call.Module, operation))
}

func (target *machineEntrypointTarget) close() {
	if target == nil || target.closed {
		return
	}
	if target.inner != nil {
		target.inner.close()
	}
	if target.moduleWait != nil {
		wait := target.moduleWait
		target.moduleWait = nil
		wait.close()
	}
	target.finish()
}

type machineHookRun struct {
	execution *machineRuntimeExecution
	runtime   *Runtime
	hook      string
	args      []Value
	entries   []machineHookEntry
	closed    bool
}

type machineHookEntry struct {
	call     HookCallReport
	target   resumableTarget
	token    any
	state    *suspensionState
	terminal bool
}

type machineHookSuspensionTarget struct {
	run    *machineHookRun
	index  int
	target resumableTarget
}

type machineHookDeferredTarget struct {
	run *machineHookRun
}

func (execution *machineRuntimeExecution) runHookResumable(runtime *Runtime, ctx context.Context, hook string, args []Value) (resumableOutcome, error) {
	return execution.runHookResumableRun(runtime, ctx, hook, args, false)
}

func (execution *machineRuntimeExecution) runHookResumableRun(runtime *Runtime, ctx context.Context, hook string, args []Value, stopped bool) (resumableOutcome, error) {
	run := &machineHookRun{
		execution: execution,
		runtime:   runtime,
		hook:      hook,
		args:      append([]Value(nil), args...),
		entries:   make([]machineHookEntry, len(execution.image.entrypoints)),
	}
	return run.startRun(ctx, stopped)
}

func (run *machineHookRun) start(ctx context.Context) (resumableOutcome, error) {
	return run.startRun(ctx, false)
}

func (run *machineHookRun) startRun(ctx context.Context, stopped bool) (resumableOutcome, error) {
	if run == nil || run.runtime == nil || run.execution == nil || run.closed {
		return resumableOutcome{}, ErrSuspensionStale
	}
	for index, entrypoint := range run.execution.image.entrypoints {
		entry := &run.entries[index]
		module := run.execution.image.modules[entrypoint.moduleID]
		entry.call = newDispatchCallReport(entrypoint.name, moduleIDFromKey(module.key), run.hook)
		target := &machineEntrypointTarget{
			execution:  run.execution,
			runtime:    run.runtime,
			entrypoint: entrypoint,
			hook:       run.hook,
			args:       append([]Value(nil), run.args...),
			call:       &entry.call,
		}
		entry.target = target
		outcome, err := resumeResumableTarget(target, ctx, run.args, nil, stopped)
		if err != nil {
			run.close()
			return resumableOutcome{}, err
		}
		if outcome.target != nil {
			entry.target = outcome.target
			entry.token = outcome.token
			continue
		}
		entry.target = nil
		entry.terminal = true
	}
	if err := run.pump(ctx, stopped); err != nil {
		run.close()
		return resumableOutcome{}, err
	}
	return run.snapshot(), nil
}

func (target *machineHookSuspensionTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *machineHookSuspensionTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *machineHookSuspensionTarget) resumeRun(ctx context.Context, values []Value, failure error, stopped bool) (resumableOutcome, error) {
	if target == nil || target.run == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	return target.run.resumeEntry(ctx, target.index, target.target, values, failure, stopped)
}

func (target *machineHookSuspensionTarget) close() {
	if target == nil || target.run == nil {
		return
	}
	target.run.closeEntry(target.index, target.target)
	target.run = nil
	target.target = nil
}

func (target *machineHookDeferredTarget) resume(ctx context.Context, _ []Value, _ error) (resumableOutcome, error) {
	return target.resumeRun(ctx, false)
}

func (target *machineHookDeferredTarget) resumeStopped(ctx context.Context, _ []Value, _ error) (resumableOutcome, error) {
	return target.resumeRun(ctx, true)
}

func (target *machineHookDeferredTarget) resumeRun(ctx context.Context, stopped bool) (resumableOutcome, error) {
	if target == nil || target.run == nil || target.run.closed {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if err := target.run.pump(ctx, stopped); err != nil {
		target.run.close()
		return resumableOutcome{}, err
	}
	return target.run.snapshot(), nil
}

func (target *machineHookDeferredTarget) close() {
	if target == nil || target.run == nil {
		return
	}
	target.run.close()
	target.run = nil
}

func (target *machineHookDeferredTarget) bindSuspensionState(state *suspensionState) {
	if target == nil || target.run == nil {
		return
	}
	target.run.bindDeferredState(state)
}

func (run *machineHookRun) resumeEntry(
	ctx context.Context,
	index int,
	target resumableTarget,
	values []Value,
	failure error,
	stopped bool,
) (resumableOutcome, error) {
	if run == nil || run.closed || index < 0 || index >= len(run.entries) {
		return resumableOutcome{}, ErrSuspensionStale
	}
	entry := &run.entries[index]
	if entry.target == nil || entry.target != target {
		return resumableOutcome{}, ErrSuspensionStale
	}
	entry.state = nil
	outcome, err := resumeResumableTarget(entry.target, ctx, values, failure, stopped)
	if err != nil {
		run.close()
		return resumableOutcome{}, fmt.Errorf("runtime: call operation %s.%s: %w", entry.call.Entrypoint, run.hook, err)
	}
	run.applyEntryOutcome(index, outcome)
	if err := run.pump(ctx, stopped); err != nil {
		run.close()
		return resumableOutcome{}, err
	}
	return run.snapshot(), nil
}

func (run *machineHookRun) applyEntryOutcome(index int, outcome resumableOutcome) {
	entry := &run.entries[index]
	if outcome.target != nil {
		entry.target = outcome.target
		entry.token = outcome.token
		return
	}
	entry.target = nil
	entry.token = nil
	entry.terminal = true
}

func (run *machineHookRun) pump(ctx context.Context, stopped bool) error {
	for {
		progressed := false
		for index := range run.entries {
			entry := &run.entries[index]
			target, ok := entry.target.(quiescentResumableTarget)
			if !ok {
				continue
			}
			outcome, advanced, err := target.pump(ctx, stopped)
			if err != nil {
				return err
			}
			if !advanced {
				continue
			}
			progressed = true
			run.applyEntryOutcome(index, outcome)
		}
		if !progressed {
			return nil
		}
	}
}

func (run *machineHookRun) bindDeferredState(state *suspensionState) {
	if run == nil || run.closed || state == nil {
		return
	}
	for index := range run.entries {
		entry := &run.entries[index]
		target, ok := entry.target.(*machineEntrypointTarget)
		if !ok || target.hostVisible() {
			continue
		}
		target.bindDependencyState(state, run.close)
	}
}

func (run *machineHookRun) snapshot() resumableOutcome {
	report := newDispatchReport(run.hook)
	pending := make([]resumablePending, 0, len(run.entries))
	hidden := false
	for index := range run.entries {
		entry := &run.entries[index]
		if entry.terminal {
			report.Calls = append(report.Calls, entry.call)
			continue
		}
		if entry.target == nil {
			continue
		}
		if target, ok := entry.target.(*machineEntrypointTarget); ok && target.closed {
			entry.target = nil
			entry.token = nil
			continue
		}
		if target, ok := entry.target.(quiescentResumableTarget); ok && !target.hostVisible() {
			hidden = true
			continue
		}
		entryIndex := index
		wrapper := &machineHookSuspensionTarget{run: run, index: index, target: entry.target}
		pending = append(pending, resumablePending{
			target:     wrapper,
			token:      entry.token,
			entrypoint: entry.call.Entrypoint,
			module:     entry.call.Module,
			hook:       run.hook,
			state:      entry.state,
			bind: func(state *suspensionState) {
				run.entries[entryIndex].state = state
			},
		})
	}
	if len(pending) == 0 && hidden {
		deferred := &machineHookDeferredTarget{run: run}
		pending = append(pending, resumablePending{
			target:  deferred,
			hook:    run.hook,
			blocked: true,
			bind:    deferred.bindSuspensionState,
		})
	}
	if len(pending) == 0 && !hidden {
		run.closed = true
		run.runtime = nil
		run.execution = nil
		run.args = nil
	}
	return resumableOutcome{hook: &report, pending: pending}
}

func (run *machineHookRun) close() {
	if run == nil || run.closed {
		return
	}
	run.closed = true
	for index := range run.entries {
		entry := &run.entries[index]
		if entry.target != nil {
			entry.target.close()
		}
		entry.target = nil
		entry.token = nil
		entry.state = nil
	}
	run.runtime = nil
	run.execution = nil
	run.args = nil
}

func (run *machineHookRun) closeEntry(index int, target resumableTarget) {
	if run == nil || run.closed || index < 0 || index >= len(run.entries) {
		return
	}
	entry := &run.entries[index]
	if entry.target != target {
		return
	}
	entry.target.close()
	entry.target = nil
	entry.token = nil
	entry.state = nil
}
