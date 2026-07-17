package ember

import (
	"context"
	"fmt"
)

type vmResumableTarget struct {
	runtime   *Runtime
	scope     invocationScope
	coroutine *vmCoroutine
	inherited []ScriptFrame
	closed    bool
}

func newVMResumableTarget(runtime *Runtime, scope invocationScope, closure *closure) (*vmResumableTarget, error) {
	if runtime == nil || runtime.owner == nil || closure == nil {
		return nil, fmt.Errorf("resumable VM call is unavailable")
	}
	scope.resumable = true
	env := scope.envWithRequire()
	coroutine, err := newVMCoroutineChecked(env, closure)
	if err != nil {
		return nil, err
	}
	return &vmResumableTarget{
		runtime:   runtime,
		scope:     scope,
		coroutine: coroutine,
		inherited: append([]ScriptFrame(nil), scope.inheritedScriptFrames...),
	}, nil
}

func (target *vmResumableTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *vmResumableTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *vmResumableTarget) resumeRun(ctx context.Context, values []Value, failure error, acquireLease bool) (resumableOutcome, error) {
	if target == nil || target.closed || target.runtime == nil || target.coroutine == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if acquireLease {
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
	coroutine := target.coroutine
	coroutine.thread.inheritedScriptFrames = append(coroutine.thread.inheritedScriptFrames[:0], target.inherited...)
	coroutine.suspended.inheritedScriptFrames = append(coroutine.suspended.inheritedScriptFrames[:0], target.inherited...)
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
	target.inherited = nil
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
	call := &moduleCallTarget{runtime: target.scope.runtime, target: resumable}
	outcome, err := call.resume(ctx, args, nil)
	if err != nil {
		call.close()
		return resumableOutcome{}, fmt.Errorf("callback: call script function: %w", err)
	}
	return outcome, nil
}

type vmEntrypointStage uint8

const (
	vmEntrypointLoading vmEntrypointStage = iota
	vmEntrypointCallingHook
)

// vmEntrypointTarget keeps module initialization and the following exported
// function call inside one resumable operation. The host sees only the deepest
// host wait; the transition from a completed initializer into its call is
// automatic.
type vmEntrypointTarget struct {
	runtime    *Runtime
	entrypoint programEntrypoint
	hook       string
	args       []Value
	call       *HookCallReport
	globals    map[string]Value
	explicit   bool
	direct     bool
	required   bool
	inner      *vmResumableTarget
	moduleWait *runtimeModuleWait
	stage      vmEntrypointStage
	started    bool
	closed     bool
}

func (target *vmEntrypointTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *vmEntrypointTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *vmEntrypointTarget) resumeRun(ctx context.Context, values []Value, failure error, stopped bool) (resumableOutcome, error) {
	if target == nil || target.closed || target.runtime == nil || target.call == nil {
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

func (target *vmEntrypointTarget) startLoad(ctx context.Context, stopped bool) (resumableOutcome, error) {
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
	if export, ok := target.runtime.entrypoints[target.entrypoint.key]; ok {
		return target.startHook(ctx, export, stopped)
	}
	if export, ok := target.runtime.loaded[target.entrypoint.key]; ok {
		target.runtime.entrypoints[target.entrypoint.key] = export
		return target.startHook(ctx, export, stopped)
	}
	key := target.entrypoint.key
	loadGlobals, err := target.callGlobals(ctx, "")
	if err != nil {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: host globals for %s load: %w", target.entrypoint.name, err)
	}
	scope := target.runtime.newInvocationScope(ctx, key, loadGlobals, nil)
	export, wait, started, err := target.runtime.startModuleInitialization(scope, key, stopped)
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
	return target.startLoadedHook(ctx, export, stopped)
}

func (target *vmEntrypointTarget) acceptInner(ctx context.Context, outcome resumableOutcome, stopped bool) (resumableOutcome, error) {
	if outcome.target != nil {
		target.inner = outcome.target.(*vmResumableTarget)
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
	if target.stage == vmEntrypointCallingHook {
		target.call.Called = true
		target.finish()
		return resumableOutcome{values: outcome.values}, nil
	}
	return resumableOutcome{}, fmt.Errorf("runtime: entrypoint load completed outside module initializer")
}

func (target *vmEntrypointTarget) hostVisible() bool {
	if target == nil || target.closed {
		return false
	}
	if target.moduleWait == nil {
		return true
	}
	_, visible := target.moduleWait.visibleToken()
	return visible
}

func (target *vmEntrypointTarget) setModuleWait(wait *runtimeModuleWait) {
	target.moduleWait = wait
	if wait != nil {
		wait.bind(target.close)
	}
}

func (target *vmEntrypointTarget) bindDependencyState(state *suspensionState, onCancel func()) {
	if target == nil || target.moduleWait == nil {
		return
	}
	target.moduleWait.bindDependencyState(state)
	target.moduleWait.bind(onCancel)
}

func (target *vmEntrypointTarget) pump(ctx context.Context, stopped bool) (resumableOutcome, bool, error) {
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

func (target *vmEntrypointTarget) continueAfterModuleWait(ctx context.Context, initialization *runtimeModuleInitialization, stopped bool) (resumableOutcome, error) {
	if target.stage == vmEntrypointLoading {
		if initialization.err != nil {
			target.close()
			return resumableOutcome{}, target.wrapError(initialization.err)
		}
		return target.startLoadedHook(ctx, initialization.export, stopped)
	}
	outcome, err := resumeResumableTarget(target.inner, ctx, []Value{initialization.export}, initialization.err, stopped)
	if err != nil {
		target.close()
		return resumableOutcome{}, target.wrapError(err)
	}
	return target.acceptInner(ctx, outcome, stopped)
}

func (target *vmEntrypointTarget) startLoadedHook(ctx context.Context, export Value, stopped bool) (resumableOutcome, error) {
	if !target.explicit {
		target.runtime.entrypoints[target.entrypoint.key] = export
	}
	return target.startHook(ctx, export, stopped)
}

func (target *vmEntrypointTarget) startHook(ctx context.Context, export Value, stopped bool) (resumableOutcome, error) {
	target.stage = vmEntrypointCallingHook
	controller, err := newExecutionPolicy(ctx, target.runtime.limits)
	if err != nil {
		target.close()
		return resumableOutcome{}, err
	}
	hookGlobals, err := target.callGlobals(ctx, target.hook)
	if err != nil {
		target.close()
		return resumableOutcome{}, fmt.Errorf("runtime: host globals for %s.%s: %w", target.entrypoint.name, target.hook, err)
	}
	if export.IsNil() {
		if target.required {
			target.close()
			return resumableOutcome{}, fmt.Errorf("runtime: module %s returned nil, want callable export", target.call.Module.String())
		}
		target.call.Skipped = true
		target.finish()
		return resumableOutcome{}, nil
	}
	hookValue := export
	if !target.direct {
		table, ok := export.Table()
		if !ok {
			target.close()
			return resumableOutcome{}, fmt.Errorf("runtime: module %s returned %s, want table or nil", target.call.Module.String(), export.Kind())
		}
		hookEnv := globalEnv{host: hookGlobals, owner: target.runtime.owner}
		hookValue, err = runtimeTableAccess(&hookEnv).get(table, StringValue(target.hook))
		if err != nil {
			target.close()
			return resumableOutcome{}, err
		}
		if hookValue.IsNil() {
			if target.required {
				target.close()
				return resumableOutcome{}, fmt.Errorf("runtime: module %s has no export %q", target.call.Module.String(), target.hook)
			}
			target.call.Skipped = true
			target.finish()
			return resumableOutcome{}, nil
		}
	}
	closure, ok := hookValue.scriptFunction()
	if !ok {
		target.close()
		if target.explicit {
			if target.direct {
				return resumableOutcome{}, fmt.Errorf("runtime: module %s returned %s, want script function", target.call.Module.String(), hookValue.Kind())
			}
			return resumableOutcome{}, fmt.Errorf("runtime: module %s export %q is %s, want script function", target.call.Module.String(), target.hook, hookValue.Kind())
		}
		return resumableOutcome{}, fmt.Errorf("runtime: resumable operation %s.%s is %s, want script function", target.entrypoint.name, target.hook, hookValue.Kind())
	}
	scope := target.runtime.newInvocationScope(ctx, target.entrypoint.key, hookGlobals, controller)
	target.inner, err = newVMResumableTarget(target.runtime, scope, closure)
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

func (target *vmEntrypointTarget) wrapError(err error) error {
	if target.explicit {
		if target.stage != vmEntrypointCallingHook {
			return fmt.Errorf("runtime: load module %s: %w", target.call.Module.String(), err)
		}
		if target.direct {
			return fmt.Errorf("runtime: call module %s: %w", target.call.Module.String(), err)
		}
		return fmt.Errorf("runtime: call module %s export %q: %w", target.call.Module.String(), target.hook, err)
	}
	if target.stage == vmEntrypointCallingHook {
		return fmt.Errorf("runtime: call operation %s.%s: %w", target.entrypoint.name, target.hook, err)
	}
	return fmt.Errorf("runtime: load entrypoint %s: %w", target.entrypoint.name, err)
}

func (target *vmEntrypointTarget) finish() {
	target.closed = true
	target.inner = nil
	target.runtime = nil
	target.args = nil
	target.globals = nil
}

func (target *vmEntrypointTarget) callGlobals(ctx context.Context, operation string) (map[string]Value, error) {
	if target.explicit {
		return copyGlobals(target.globals), nil
	}
	return target.runtime.hostGlobals(ctx, newHostCall(target.entrypoint.name, target.call.Module, operation))
}

func (target *vmEntrypointTarget) close() {
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

type vmHookRun struct {
	runtime *Runtime
	hook    string
	args    []Value
	entries []vmHookEntry
	closed  bool
}

type vmHookEntry struct {
	call     HookCallReport
	target   resumableTarget
	token    any
	state    *suspensionState
	terminal bool
}

type vmHookSuspensionTarget struct {
	run    *vmHookRun
	index  int
	target resumableTarget
}

type vmHookDeferredTarget struct {
	run *vmHookRun
}

func (vmRuntimeExecution) runHookResumable(runtime *Runtime, ctx context.Context, hook string, args []Value) (resumableOutcome, error) {
	run := &vmHookRun{
		runtime: runtime,
		hook:    hook,
		args:    append([]Value(nil), args...),
		entries: make([]vmHookEntry, len(runtime.program.entrypoints)),
	}
	return run.start(ctx)
}

func (run *vmHookRun) start(ctx context.Context) (resumableOutcome, error) {
	if run == nil || run.runtime == nil || run.closed {
		return resumableOutcome{}, ErrSuspensionStale
	}
	for index, entrypoint := range run.runtime.program.entrypoints {
		entry := &run.entries[index]
		entry.call = newDispatchCallReport(entrypoint.name, moduleIDFromKey(entrypoint.key), run.hook)
		target := &vmEntrypointTarget{
			runtime:    run.runtime,
			entrypoint: entrypoint,
			hook:       run.hook,
			args:       append([]Value(nil), run.args...),
			call:       &entry.call,
		}
		entry.target = target
		outcome, err := target.resume(ctx, run.args, nil)
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
	if err := run.pump(ctx, false); err != nil {
		run.close()
		return resumableOutcome{}, err
	}
	return run.snapshot(), nil
}

func (target *vmHookSuspensionTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *vmHookSuspensionTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *vmHookSuspensionTarget) resumeRun(ctx context.Context, values []Value, failure error, stopped bool) (resumableOutcome, error) {
	if target == nil || target.run == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	return target.run.resumeEntry(ctx, target.index, target.target, values, failure, stopped)
}

func (target *vmHookSuspensionTarget) close() {
	if target == nil || target.run == nil {
		return
	}
	target.run.closeEntry(target.index, target.target)
	target.run = nil
	target.target = nil
}

func (target *vmHookDeferredTarget) resume(ctx context.Context, _ []Value, _ error) (resumableOutcome, error) {
	return target.resumeRun(ctx, false)
}

func (target *vmHookDeferredTarget) resumeStopped(ctx context.Context, _ []Value, _ error) (resumableOutcome, error) {
	return target.resumeRun(ctx, true)
}

func (target *vmHookDeferredTarget) resumeRun(ctx context.Context, stopped bool) (resumableOutcome, error) {
	if target == nil || target.run == nil || target.run.closed {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if err := target.run.pump(ctx, stopped); err != nil {
		target.run.close()
		return resumableOutcome{}, err
	}
	return target.run.snapshot(), nil
}

func (target *vmHookDeferredTarget) close() {
	if target == nil || target.run == nil {
		return
	}
	target.run.close()
	target.run = nil
}

func (target *vmHookDeferredTarget) bindSuspensionState(state *suspensionState) {
	if target == nil || target.run == nil {
		return
	}
	target.run.bindDeferredState(state)
}

func (run *vmHookRun) resumeEntry(
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

func (run *vmHookRun) applyEntryOutcome(index int, outcome resumableOutcome) {
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

func (run *vmHookRun) pump(ctx context.Context, stopped bool) error {
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

func (run *vmHookRun) bindDeferredState(state *suspensionState) {
	if run == nil || run.closed || state == nil {
		return
	}
	for index := range run.entries {
		entry := &run.entries[index]
		target, ok := entry.target.(*vmEntrypointTarget)
		if !ok || target.hostVisible() {
			continue
		}
		target.bindDependencyState(state, run.close)
	}
}

func (run *vmHookRun) snapshot() resumableOutcome {
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
		if target, ok := entry.target.(*vmEntrypointTarget); ok && target.closed {
			entry.target = nil
			entry.token = nil
			continue
		}
		if target, ok := entry.target.(quiescentResumableTarget); ok && !target.hostVisible() {
			hidden = true
			continue
		}
		entryIndex := index
		wrapper := &vmHookSuspensionTarget{run: run, index: index, target: entry.target}
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
		deferred := &vmHookDeferredTarget{run: run}
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
		run.args = nil
	}
	return resumableOutcome{hook: &report, pending: pending}
}

func (run *vmHookRun) close() {
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
	run.args = nil
}

func (run *vmHookRun) closeEntry(index int, target resumableTarget) {
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
