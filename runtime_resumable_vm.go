package ember

import (
	"context"
	"fmt"
)

type vmResumableTarget struct {
	runtime   *Runtime
	scope     invocationScope
	coroutine *vmCoroutine
	closed    bool
}

func newVMResumableTarget(runtime *Runtime, scope invocationScope, closure *closure) (*vmResumableTarget, error) {
	if runtime == nil || runtime.owner == nil || closure == nil {
		return nil, fmt.Errorf("resumable VM call is unavailable")
	}
	env := scope.envWithRequire()
	coroutine, err := newVMCoroutineChecked(env, closure)
	if err != nil {
		return nil, err
	}
	return &vmResumableTarget{
		runtime:   runtime,
		scope:     scope,
		coroutine: coroutine,
	}, nil
}

func (target *vmResumableTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	if target == nil || target.closed || target.runtime == nil || target.coroutine == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	lease, err := target.runtime.beginRun()
	if err == errRuntimeOwnerClosed {
		return resumableOutcome{}, fmt.Errorf("runtime: closed")
	}
	if err == errRuntimeOwnerBusy {
		return resumableOutcome{}, fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
	}
	if err != nil {
		return resumableOutcome{}, fmt.Errorf("runtime: begin run: %w", err)
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
	coroutine := target.coroutine
	if err := coroutine.owner.registerCoroutine(coroutine); err != nil {
		return resumableOutcome{}, err
	}
	defer coroutine.owner.unregisterCoroutine(coroutine)
	coroutine.status = vmCoroutineRunning
	scope := target.scope
	scope.ctx = ctx
	scope.controller = controller
	coroutine.thread.ctx = ctx
	coroutine.thread.controller = controller
	coroutine.thread.scope = scope
	coroutine.thread.hasScope = true
	if coroutine.thread.globals != nil {
		coroutine.thread.globals.scope = scope
		coroutine.thread.globals.hasScope = true
		coroutine.thread.globals.controller = controller
	}
	if failure != nil {
		values = []Value{hostResumeFailureValue(failure)}
	}
	results, runErr := resumeCoroutine(coroutine, coroutine.thread.globals, values)
	if yield, ok := runErr.(vmYieldRequest); ok {
		token := yield.hostToken
		if token == nil {
			token = vmPendingHostToken(&coroutine.thread)
		}
		coroutine.status = vmCoroutineSuspended
		coroutine.suspended = coroutine.thread.suspendFrames()
		clearVMSuspendedInvocation(coroutine)
		return resumableOutcome{
			target: target,
			token:  token,
		}, nil
	}
	if runErr != nil {
		target.close()
		return resumableOutcome{}, runErr
	}
	coroutine.status = vmCoroutineDead
	target.close()
	return resumableOutcome{values: results}, nil
}

func vmPendingHostToken(thread *vmThread) any {
	if thread == nil {
		return nil
	}
	for index := len(thread.frames) - 1; index >= 0; index-- {
		frame := thread.frames[index]
		if frame != nil && frame.hasPendingCall && frame.pendingCall.host != nil {
			return frame.pendingCall.host.token
		}
	}
	return nil
}

func clearVMSuspendedInvocation(coroutine *vmCoroutine) {
	if coroutine == nil {
		return
	}
	coroutine.suspended.ctx = context.Background()
	coroutine.suspended.controller = nil
	coroutine.suspended.scope = invocationScope{}
	coroutine.suspended.hasScope = false
	coroutine.suspended.inheritedScriptFrames = nil
	coroutine.thread.ctx = context.Background()
	coroutine.thread.controller = nil
	coroutine.thread.scope = invocationScope{}
	coroutine.thread.hasScope = false
	coroutine.thread.inheritedScriptFrames = nil
	if coroutine.thread.globals != nil {
		coroutine.thread.globals.scope = invocationScope{}
		coroutine.thread.globals.hasScope = false
		coroutine.thread.globals.controller = nil
	}
}

func (target *vmResumableTarget) close() {
	if target == nil || target.closed {
		return
	}
	target.closed = true
	if target.coroutine != nil {
		target.coroutine.disposeFrames()
		target.coroutine.releaseOwner()
	}
	target.coroutine = nil
	target.scope = invocationScope{}
	target.runtime = nil
}

func (target *vmCallbackTarget) callResumable(ctx context.Context, args []Value) (resumableOutcome, error) {
	if target == nil || target.state == nil || target.scope.runtime == nil {
		return resumableOutcome{}, fmt.Errorf("callback: not retained")
	}
	target.state.mu.Lock()
	released := target.state.released
	target.state.mu.Unlock()
	if released {
		return resumableOutcome{}, fmt.Errorf("callback: released")
	}
	closure, ok := target.value.scriptFunction()
	if !ok {
		return resumableOutcome{}, fmt.Errorf("callback: value is %s, want script function", target.value.Kind())
	}
	resumable, err := newVMResumableTarget(target.scope.runtime, target.scope, closure)
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

type vmHookRun struct {
	runtime *Runtime
	hook    string
	args    []Value
	report  HookReport
	index   int
	current *vmResumableTarget
	call    HookCallReport
}

func (vmRuntimeExecution) runHookResumable(runtime *Runtime, ctx context.Context, hook string, args []Value) (resumableOutcome, error) {
	run := &vmHookRun{
		runtime: runtime,
		hook:    hook,
		args:    append([]Value(nil), args...),
		report:  HookReport{Hook: hook},
	}
	return run.resume(ctx, nil, nil)
}

func (run *vmHookRun) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	if run == nil || run.runtime == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if run.current != nil {
		outcome, err := run.current.resume(ctx, values, failure)
		if err != nil {
			run.close()
			return resumableOutcome{}, fmt.Errorf("runtime: call hook %s.%s: %w", run.call.Entrypoint, run.hook, err)
		}
		if outcome.target != nil {
			run.current = outcome.target.(*vmResumableTarget)
			return resumableOutcome{target: run, token: outcome.token}, nil
		}
		run.call.Called = true
		run.report.Calls = append(run.report.Calls, run.call)
		run.current = nil
		run.index++
	}
	for run.index < len(run.runtime.program.entrypoints) {
		entrypoint := run.runtime.program.entrypoints[run.index]
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
			run.current = outcome.target.(*vmResumableTarget)
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

func (run *vmHookRun) prepareEntrypoint(ctx context.Context, entrypoint programEntrypoint) (HookCallReport, *vmResumableTarget, error) {
	call := HookCallReport{
		Entrypoint: entrypoint.name,
		Module:     moduleIDFromKey(entrypoint.key),
		Hook:       run.hook,
	}
	lease, err := run.runtime.beginRun()
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
	loadGlobals, err := run.runtime.hostGlobals(ctx, HostCall{Entrypoint: entrypoint.name, Module: call.Module})
	if err != nil {
		return call, nil, fmt.Errorf("runtime: host globals for %s load: %w", entrypoint.name, err)
	}
	export, loaded, err := run.runtime.loadEntrypoint(ctx, entrypoint, loadGlobals, controller)
	if err != nil {
		return call, nil, fmt.Errorf("runtime: load entrypoint %s: %w", entrypoint.name, err)
	}
	call.Loaded = loaded
	hookGlobals, err := run.runtime.hostGlobals(ctx, HostCall{
		Entrypoint: entrypoint.name,
		Module:     call.Module,
		Hook:       run.hook,
	})
	if err != nil {
		return call, nil, fmt.Errorf("runtime: host globals for %s.%s: %w", entrypoint.name, run.hook, err)
	}
	table, ok := export.Table()
	if export.IsNil() {
		call.Skipped = true
		return call, nil, nil
	}
	if !ok {
		return call, nil, fmt.Errorf("runtime: entrypoint %s returned %s, want table or nil", entrypoint.name, export.Kind())
	}
	hookEnv := globalEnv{host: hookGlobals, owner: run.runtime.owner}
	hookValue, err := runtimeTableAccess(&hookEnv).get(table, StringValue(run.hook))
	if err != nil {
		return call, nil, err
	}
	if hookValue.IsNil() {
		call.Skipped = true
		return call, nil, nil
	}
	closure, ok := hookValue.scriptFunction()
	if !ok {
		return call, nil, fmt.Errorf("runtime: resumable hook %s.%s is %s, want script function", entrypoint.name, run.hook, hookValue.Kind())
	}
	scope := run.runtime.newInvocationScope(ctx, entrypoint.key, hookGlobals, controller)
	target, err := newVMResumableTarget(run.runtime, scope, closure)
	return call, target, err
}

func (run *vmHookRun) close() {
	if run == nil {
		return
	}
	if run.current != nil {
		run.current.close()
	}
	run.current = nil
	run.runtime = nil
	run.args = nil
}
