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
}

// runtimeModuleWait is an internal coroutine handoff. owner is true only for
// the invocation that started the initializer; unrelated waiters never expose
// a duplicate host handle for the initializer's underlying host wait.
type runtimeModuleWait struct {
	initialization *runtimeModuleInitialization
	owner          bool
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
	if target == nil || target.closed || target.runtime == nil || target.target == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	if target.wait != nil {
		initialization := target.wait.initialization
		_ = initialization.resumeHost(ctx, values, failure)
		if !initialization.done {
			token, _ := target.wait.visibleToken()
			return resumableOutcome{target: target, token: token}, nil
		}
		target.wait = nil
		values = []Value{initialization.export}
		failure = initialization.err
	}
	outcome, err := target.target.resume(ctx, values, failure)
	if err != nil {
		target.close()
		return resumableOutcome{}, err
	}
	return target.accept(ctx, outcome)
}

func (target *moduleCallTarget) accept(ctx context.Context, outcome resumableOutcome) (resumableOutcome, error) {
	if outcome.target == nil {
		target.closed = true
		target.target = nil
		target.runtime = nil
		return outcome, nil
	}
	target.target = outcome.target
	if request, ok := outcome.token.(*runtimeModuleRequest); ok {
		export, wait, err := target.runtime.startModuleInitialization(request.scope, request.key)
		if err != nil {
			outcome, err = target.target.resume(ctx, nil, err)
		} else if wait != nil {
			target.wait = wait
			token, _ := wait.visibleToken()
			return resumableOutcome{target: target, token: token}, nil
		} else {
			outcome, err = target.target.resume(ctx, []Value{export}, nil)
		}
		if err != nil {
			target.close()
			return resumableOutcome{}, err
		}
		return target.accept(ctx, outcome)
	}
	if wait, ok := outcome.token.(*runtimeModuleWait); ok {
		target.wait = wait
		token, _ := wait.visibleToken()
		return resumableOutcome{target: target, token: token}, nil
	}
	return resumableOutcome{target: target, token: outcome.token}, nil
}

func (target *moduleCallTarget) close() {
	if target == nil || target.closed {
		return
	}
	target.closed = true
	if target.target != nil {
		target.target.close()
	}
	target.target = nil
	target.runtime = nil
	target.wait = nil
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
		return HostSuspend(&runtimeModuleWait{initialization: initialization})
	}
	return HostSuspend(&runtimeModuleRequest{scope: scope, key: required})
}

func (r *Runtime) startModuleInitialization(scope invocationScope, key moduleKey) (Value, *runtimeModuleWait, error) {
	if r == nil {
		return NilValue(), nil, fmt.Errorf("module runtime: nil runtime")
	}
	if export, ok, err := r.cachedResumableModule(key); err != nil {
		return NilValue(), nil, err
	} else if ok {
		return export, nil, nil
	}
	if modulePathContains(scope.modulePath, key) {
		return NilValue(), nil, fmt.Errorf("module runtime: active-loading cycle %s", runtimeModuleCyclePath(scope.modulePath, key))
	}
	if initialization := r.moduleInitializers[key]; initialization != nil {
		return NilValue(), &runtimeModuleWait{initialization: initialization}, nil
	}
	initialization, err := r.newModuleInitialization(scope, key)
	if err != nil {
		return NilValue(), nil, err
	}
	if r.moduleInitializers == nil {
		r.moduleInitializers = make(map[moduleKey]*runtimeModuleInitialization)
	}
	r.moduleInitializers[key] = initialization
	outcome, err := initialization.target.resume(scope.ctx, nil, nil)
	if err != nil {
		initialization.fail(err)
		return NilValue(), nil, err
	}
	if err := initialization.accept(scope.ctx, outcome); err != nil {
		return NilValue(), nil, err
	}
	if initialization.done {
		return initialization.export, nil, initialization.err
	}
	return NilValue(), &runtimeModuleWait{initialization: initialization, owner: true}, nil
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

func (initialization *runtimeModuleInitialization) resumeHost(ctx context.Context, values []Value, failure error) error {
	if initialization == nil || initialization.done || initialization.target == nil {
		return ErrSuspensionStale
	}
	var outcome resumableOutcome
	var err error
	if wait, ok := initialization.token.(*runtimeModuleWait); ok {
		if !wait.owner || wait.initialization == nil {
			return fmt.Errorf("module runtime: initializer is waiting for unrelated module %s", initialization.key.String())
		}
		if err := wait.initialization.resumeHost(ctx, values, failure); err != nil {
			return err
		}
		if !wait.initialization.done {
			return nil
		}
		outcome, err = initialization.target.resume(ctx, []Value{wait.initialization.export}, wait.initialization.err)
	} else {
		outcome, err = initialization.target.resume(ctx, values, failure)
	}
	if err != nil {
		initialization.fail(err)
		return err
	}
	return initialization.accept(ctx, outcome)
}

func (initialization *runtimeModuleInitialization) accept(ctx context.Context, outcome resumableOutcome) error {
	if outcome.target != nil {
		initialization.target = outcome.target
		if request, ok := outcome.token.(*runtimeModuleRequest); ok {
			export, wait, err := initialization.runtime.startModuleInitialization(request.scope, request.key)
			if err != nil {
				outcome, err = initialization.target.resume(ctx, nil, err)
			} else if wait != nil {
				initialization.token = wait
				return nil
			} else {
				outcome, err = initialization.target.resume(ctx, []Value{export}, nil)
			}
			if err != nil {
				initialization.fail(err)
				return err
			}
			return initialization.accept(ctx, outcome)
		}
		initialization.token = outcome.token
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
	if initialization.runtime != nil {
		delete(initialization.runtime.moduleInitializers, initialization.key)
	}
}

func modulePathContains(path []moduleKey, key moduleKey) bool {
	for _, candidate := range path {
		if candidate == key {
			return true
		}
	}
	return false
}
