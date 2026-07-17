package ember

import (
	"context"
	"fmt"
)

// runtimeModuleInitialization owns one in-flight module initializer. Waiters
// refer to this record instead of starting another initializer.
type runtimeModuleInitialization struct {
	runtime *Runtime
	key     moduleKey
	target  resumableTarget
	token   any
	export  Value
	err     error
	done    bool
	commit  func([]Value) (Value, error)
	abort   func()
	waiters map[*runtimeModuleWait]struct{}
}

// runtimeModuleWait is an internal coroutine handoff. owner is true only for
// the invocation that started the initializer; unrelated waiters never expose
// a duplicate host handle for the initializer's underlying host wait.
type runtimeModuleWait struct {
	initialization *runtimeModuleInitialization
	owner          bool
	onCancel       func()
}

func (initialization *runtimeModuleInitialization) newWait(owner bool) *runtimeModuleWait {
	wait := &runtimeModuleWait{initialization: initialization, owner: owner}
	if initialization != nil && !initialization.done {
		if initialization.waiters == nil {
			initialization.waiters = make(map[*runtimeModuleWait]struct{})
		}
		initialization.waiters[wait] = struct{}{}
	}
	return wait
}

func (wait *runtimeModuleWait) bind(onCancel func()) {
	if wait == nil || wait.initialization == nil || wait.initialization.done {
		return
	}
	wait.onCancel = onCancel
}

func (wait *runtimeModuleWait) close() {
	if wait == nil || wait.initialization == nil {
		return
	}
	initialization := wait.initialization
	if wait.owner {
		initialization.cancel(wait)
		return
	}
	delete(initialization.waiters, wait)
	wait.onCancel = nil
}

func (wait *runtimeModuleWait) visibleToken() (any, bool) {
	if wait == nil || !wait.owner || wait.initialization == nil {
		return nil, false
	}
	return wait.initialization.visibleToken()
}

type runtimeModuleRequest struct {
	scope invocationScope
	key   moduleKey
}

// moduleCallTarget adds resumable require handling to a single callback
// coroutine without exposing internal module handoffs as host tokens.
type moduleCallTarget struct {
	runtime *Runtime
	target  resumableTarget
	wait    *runtimeModuleWait
	closed  bool
}

func (target *moduleCallTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *moduleCallTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *moduleCallTarget) resumeRun(ctx context.Context, values []Value, failure error, stopped bool) (resumableOutcome, error) {
	if target == nil || target.closed || target.runtime == nil || target.target == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if target.wait != nil {
		initialization := target.wait.initialization
		if err := initialization.resumeHost(ctx, values, failure, stopped); err != nil {
			target.close()
			return resumableOutcome{}, err
		}
		if !initialization.done {
			token, _ := target.wait.visibleToken()
			return resumableOutcome{target: target, token: token}, nil
		}
		target.wait = nil
		values = []Value{initialization.export}
		failure = initialization.err
	}
	outcome, err := resumeResumableTarget(target.target, ctx, values, failure, stopped)
	if err != nil {
		target.close()
		return resumableOutcome{}, err
	}
	return target.accept(ctx, outcome, stopped)
}

func (target *moduleCallTarget) accept(ctx context.Context, outcome resumableOutcome, stopped bool) (resumableOutcome, error) {
	if outcome.target == nil {
		target.closed = true
		target.target = nil
		target.runtime = nil
		return outcome, nil
	}
	target.target = outcome.target
	if request, ok := outcome.token.(*runtimeModuleRequest); ok {
		export, wait, _, err := target.runtime.startModuleInitialization(request.scope, request.key, stopped)
		if err != nil {
			outcome, err = resumeResumableTarget(target.target, ctx, nil, err, stopped)
		} else if wait != nil {
			target.setWait(wait)
			token, _ := wait.visibleToken()
			return resumableOutcome{target: target, token: token}, nil
		} else {
			outcome, err = resumeResumableTarget(target.target, ctx, []Value{export}, nil, stopped)
		}
		if err != nil {
			target.close()
			return resumableOutcome{}, err
		}
		return target.accept(ctx, outcome, stopped)
	}
	if wait, ok := outcome.token.(*runtimeModuleWait); ok {
		target.setWait(wait)
		token, _ := wait.visibleToken()
		return resumableOutcome{target: target, token: token}, nil
	}
	return resumableOutcome{target: target, token: outcome.token}, nil
}

func (target *moduleCallTarget) setWait(wait *runtimeModuleWait) {
	target.wait = wait
	if wait != nil {
		wait.bind(target.close)
	}
}

func (target *moduleCallTarget) close() {
	if target == nil || target.closed {
		return
	}
	target.closed = true
	if target.target != nil {
		target.target.close()
	}
	if target.wait != nil {
		wait := target.wait
		target.wait = nil
		wait.close()
	}
	target.target = nil
	target.runtime = nil
}

func (r *Runtime) requireResumable(scope invocationScope, args []Value) HostResult {
	if len(args) == 0 {
		return HostError(fmt.Errorf("require: missing module path"))
	}
	request, ok := args[0].String()
	if !ok {
		return HostError(fmt.Errorf("require: module path is %s, want string", args[0].Kind()))
	}
	required, err := normalizeRequireKey(scope.from, request)
	if err != nil {
		return HostError(err)
	}
	if export, ok, err := r.cachedResumableModule(required); err != nil {
		return HostError(err)
	} else if ok {
		return HostReturn(export)
	}
	if modulePathContains(scope.modulePath, required) {
		return HostError(fmt.Errorf("module runtime: active-loading cycle %s", runtimeModuleCyclePath(scope.modulePath, required)))
	}
	if initialization := r.moduleInitializers[required]; initialization != nil {
		return HostSuspend(initialization.newWait(false))
	}
	return HostSuspend(&runtimeModuleRequest{scope: scope, key: required})
}

func (r *Runtime) startModuleInitialization(scope invocationScope, key moduleKey, stopped bool) (Value, *runtimeModuleWait, bool, error) {
	if r == nil {
		return NilValue(), nil, false, fmt.Errorf("module runtime: nil runtime")
	}
	if export, ok, err := r.cachedResumableModule(key); err != nil {
		return NilValue(), nil, false, err
	} else if ok {
		return export, nil, false, nil
	}
	if modulePathContains(scope.modulePath, key) {
		return NilValue(), nil, false, fmt.Errorf("module runtime: active-loading cycle %s", runtimeModuleCyclePath(scope.modulePath, key))
	}
	if initialization := r.moduleInitializers[key]; initialization != nil {
		return NilValue(), initialization.newWait(false), false, nil
	}
	initialization, err := r.newModuleInitialization(scope, key)
	if err != nil {
		return NilValue(), nil, false, err
	}
	if r.moduleInitializers == nil {
		r.moduleInitializers = make(map[moduleKey]*runtimeModuleInitialization)
	}
	r.moduleInitializers[key] = initialization
	outcome, err := resumeResumableTarget(initialization.target, scope.ctx, nil, nil, stopped)
	if err != nil {
		initialization.fail(err)
		return NilValue(), nil, true, err
	}
	if err := initialization.accept(scope.ctx, outcome, stopped); err != nil {
		return NilValue(), nil, true, err
	}
	if initialization.done {
		return initialization.export, nil, true, initialization.err
	}
	return NilValue(), initialization.newWait(true), true, nil
}

func (r *Runtime) cachedResumableModule(key moduleKey) (Value, bool, error) {
	switch execution := r.execution.(type) {
	case vmRuntimeExecution:
		export, ok := r.loaded[key]
		return export, ok, nil
	case *machineRuntimeExecution:
		moduleID, ok := execution.image.moduleIDs[key]
		if !ok {
			return NilValue(), false, fmt.Errorf("module runtime: missing module %s", key.String())
		}
		state, err := execution.owner.modules.state(moduleID)
		if err != nil {
			return NilValue(), false, err
		}
		if state != machineModuleLoaded {
			return NilValue(), false, nil
		}
		export, ok, err := execution.owner.modules.export(moduleID)
		if err != nil || !ok {
			return NilValue(), false, err
		}
		exporter := machineTableExporter{
			machine: &execution.owner.scalarMachine,
			tables:  make(map[machineTableID]machineExportedTable),
		}
		value, err := exporter.value(export)
		return value, err == nil, err
	default:
		return NilValue(), false, fmt.Errorf("module runtime: resumable execution is unavailable")
	}
}

func (r *Runtime) newModuleInitialization(scope invocationScope, key moduleKey) (*runtimeModuleInitialization, error) {
	if scope.ctx == nil {
		scope.ctx = context.Background()
	}
	controller := scope.controller
	if controller == nil {
		var err error
		controller, err = newExecutionPolicy(scope.ctx, r.limits)
		if err != nil {
			return nil, err
		}
	}
	if err := controller.chargeModuleInitialization(); err != nil {
		return nil, err
	}
	childScope := scope
	childScope.from = key
	childScope.modulePath = append(append([]moduleKey(nil), scope.modulePath...), key)
	switch execution := r.execution.(type) {
	case vmRuntimeExecution:
		proto, ok := r.program.protos[key]
		if !ok {
			return nil, fmt.Errorf("module runtime: missing proto for %s", key.String())
		}
		target, err := newVMResumableTarget(r, childScope, &closure{proto: proto})
		if err != nil {
			return nil, err
		}
		r.active[key] = true
		r.stack = append(r.stack, key)
		return &runtimeModuleInitialization{
			runtime: r,
			key:     key,
			target:  target,
			commit: func(values []Value) (Value, error) {
				export := firstRuntimeResult(values)
				r.loaded[key] = export
				delete(r.active, key)
				r.stack = removeRuntimeModuleStackKey(r.stack, key)
				return export, nil
			},
			abort: func() {
				delete(r.active, key)
				r.stack = removeRuntimeModuleStackKey(r.stack, key)
			},
		}, nil
	case *machineRuntimeExecution:
		moduleID, ok := execution.image.moduleIDs[key]
		if !ok {
			return nil, fmt.Errorf("module runtime: missing module %s", key.String())
		}
		_, cached, err := execution.owner.modules.begin(moduleID)
		if err != nil {
			return nil, err
		}
		if cached {
			return nil, fmt.Errorf("module runtime: module %s became cached while starting", key.String())
		}
		target, err := newMachineModuleResumableTarget(execution, r, moduleID, key, childScope.globals)
		if err != nil {
			_ = execution.owner.modules.abort(moduleID)
			return nil, err
		}
		target.modulePath = append([]moduleKey(nil), childScope.modulePath...)
		target.inherited = append([]ScriptFrame(nil), childScope.inheritedScriptFrames...)
		return &runtimeModuleInitialization{
			runtime: r,
			key:     key,
			target:  target,
			commit: func(values []Value) (Value, error) {
				export := slotNil
				if len(values) != 0 {
					imported, err := importMachineValuesStopped(execution.owner, values[:1])
					if err != nil {
						return NilValue(), err
					}
					export, err = execution.owner.stableMachineValueStopped(imported[0])
					if err != nil {
						return NilValue(), err
					}
				}
				if err := execution.owner.modules.finish(moduleID, export); err != nil {
					return NilValue(), err
				}
				return firstRuntimeResult(values), nil
			},
			abort: func() {
				_ = execution.owner.modules.abort(moduleID)
			},
		}, nil
	default:
		return nil, fmt.Errorf("module runtime: resumable execution is unavailable")
	}
}

func (initialization *runtimeModuleInitialization) visibleToken() (any, bool) {
	if initialization == nil || initialization.done {
		return nil, false
	}
	if wait, ok := initialization.token.(*runtimeModuleWait); ok {
		if !wait.owner || wait.initialization == nil {
			return nil, false
		}
		return wait.initialization.visibleToken()
	}
	return initialization.token, true
}

func (initialization *runtimeModuleInitialization) resumeHost(ctx context.Context, values []Value, failure error, stopped bool) error {
	if initialization == nil || initialization.done || initialization.target == nil {
		return ErrSuspensionStale
	}
	var outcome resumableOutcome
	var err error
	if wait, ok := initialization.token.(*runtimeModuleWait); ok {
		if !wait.owner || wait.initialization == nil {
			return fmt.Errorf("module runtime: initializer is waiting for unrelated module %s", initialization.key.String())
		}
		if err := wait.initialization.resumeHost(ctx, values, failure, stopped); err != nil {
			return err
		}
		if !wait.initialization.done {
			return nil
		}
		outcome, err = resumeResumableTarget(initialization.target, ctx, []Value{wait.initialization.export}, wait.initialization.err, stopped)
	} else {
		outcome, err = resumeResumableTarget(initialization.target, ctx, values, failure, stopped)
	}
	if err != nil {
		initialization.fail(err)
		return err
	}
	return initialization.accept(ctx, outcome, stopped)
}

func (initialization *runtimeModuleInitialization) pump(ctx context.Context, stopped bool) (bool, error) {
	if initialization == nil || initialization.done || initialization.target == nil {
		return false, nil
	}
	wait, ok := initialization.token.(*runtimeModuleWait)
	if !ok || wait.initialization == nil || !wait.initialization.done {
		return false, nil
	}
	outcome, err := resumeResumableTarget(initialization.target, ctx, []Value{wait.initialization.export}, wait.initialization.err, stopped)
	if err != nil {
		initialization.fail(err)
		return true, err
	}
	return true, initialization.accept(ctx, outcome, stopped)
}

func (initialization *runtimeModuleInitialization) accept(ctx context.Context, outcome resumableOutcome, stopped bool) error {
	if outcome.target != nil {
		initialization.target = outcome.target
		if request, ok := outcome.token.(*runtimeModuleRequest); ok {
			export, wait, _, err := initialization.runtime.startModuleInitialization(request.scope, request.key, stopped)
			if err != nil {
				outcome, err = resumeResumableTarget(initialization.target, ctx, nil, err, stopped)
			} else if wait != nil {
				initialization.token = wait
				wait.bind(func() { initialization.cancel(nil) })
				return nil
			} else {
				outcome, err = resumeResumableTarget(initialization.target, ctx, []Value{export}, nil, stopped)
			}
			if err != nil {
				initialization.fail(err)
				return err
			}
			return initialization.accept(ctx, outcome, stopped)
		}
		initialization.token = outcome.token
		if wait, ok := outcome.token.(*runtimeModuleWait); ok {
			wait.bind(func() { initialization.cancel(nil) })
		}
		return nil
	}
	export, err := initialization.commit(outcome.values)
	if err != nil {
		initialization.fail(err)
		return err
	}
	initialization.export = export
	initialization.done = true
	initialization.target = nil
	initialization.token = nil
	initialization.releaseWaiters()
	if initialization.runtime != nil {
		delete(initialization.runtime.moduleInitializers, initialization.key)
	}
	return nil
}

func (initialization *runtimeModuleInitialization) fail(err error) {
	if initialization == nil || initialization.done {
		return
	}
	if initialization.target != nil {
		initialization.target.close()
	}
	if initialization.abort != nil {
		initialization.abort()
	}
	initialization.err = err
	initialization.done = true
	initialization.target = nil
	initialization.token = nil
	initialization.releaseWaiters()
	if initialization.runtime != nil {
		delete(initialization.runtime.moduleInitializers, initialization.key)
	}
}

func (initialization *runtimeModuleInitialization) cancel(origin *runtimeModuleWait) {
	if initialization == nil || initialization.done {
		return
	}
	target := initialization.target
	child, _ := initialization.token.(*runtimeModuleWait)
	abort := initialization.abort
	waiters := initialization.waiters
	initialization.err = ErrSuspensionStale
	initialization.done = true
	initialization.target = nil
	initialization.token = nil
	initialization.waiters = nil
	if initialization.runtime != nil {
		delete(initialization.runtime.moduleInitializers, initialization.key)
	}
	if child != nil {
		child.close()
	}
	if target != nil {
		target.close()
	}
	if abort != nil {
		abort()
	}
	for wait := range waiters {
		onCancel := wait.onCancel
		wait.onCancel = nil
		if wait != origin && onCancel != nil {
			onCancel()
		}
	}
}

func (initialization *runtimeModuleInitialization) releaseWaiters() {
	if initialization == nil {
		return
	}
	for wait := range initialization.waiters {
		wait.onCancel = nil
	}
	initialization.waiters = nil
}

func modulePathContains(path []moduleKey, key moduleKey) bool {
	for _, candidate := range path {
		if candidate == key {
			return true
		}
	}
	return false
}
