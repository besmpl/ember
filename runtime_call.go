package ember

import (
	"context"
	"fmt"
)

// invocationScope is the private capability bundle for one runtime
// invocation.  It is carried through runtime-owned state rather than through
// a context wrapper.  ContextHostFuncValue adds the compatibility wrapper at
// the host boundary only when it actually needs to expose this scope.
type invocationScope struct {
	runtime               *Runtime
	ctx                   context.Context
	from                  moduleKey
	globals               map[string]Value
	controller            *executionController
	modulePath            []moduleKey
	resumable             bool
	inheritedScriptFrames []ScriptFrame
}

// runtimeRequireAdapter is the stable per-runtime/module identity behind a
// require value. It deliberately contains no invocation context, controller,
// or host-global snapshot; those capabilities are read from the active scope
// when the value is called.
type runtimeRequireAdapter struct {
	runtime *Runtime
	from    moduleKey
}

func (adapter *runtimeRequireAdapter) call(globals *globalEnv, args []Value) ([]Value, error) {
	if adapter == nil || adapter.runtime == nil {
		return nil, fmt.Errorf("require: nil runtime")
	}
	if err := adapter.runtime.owner.checkOpen(); err != nil {
		if err == errRuntimeOwnerClosed {
			return nil, fmt.Errorf("require: runtime is closed")
		}
		return nil, fmt.Errorf("require: runtime unavailable: %w", err)
	}
	scope, ok := invocationScopeFromGlobalEnv(globals)
	if !ok {
		return nil, fmt.Errorf("require: missing invocation scope")
	}
	if scope.runtime != nil && scope.runtime != adapter.runtime {
		return nil, fmt.Errorf("require: runtime mismatch")
	}
	scope.runtime = adapter.runtime
	scope.from = adapter.from
	return scope.require(globals, args)
}

func (adapter *runtimeRequireAdapter) callResumable(ctx context.Context, args []Value) HostResult {
	if adapter == nil || adapter.runtime == nil {
		return HostError(fmt.Errorf("require: nil runtime"))
	}
	adapter.runtime.closeMu.Lock()
	closed := adapter.runtime.closed
	adapter.runtime.closeMu.Unlock()
	if closed {
		return HostError(fmt.Errorf("require: runtime is closed"))
	}
	scope, ok := invocationScopeFromContext(ctx)
	if !ok {
		return HostError(fmt.Errorf("require: missing invocation scope"))
	}
	if scope.runtime != nil && scope.runtime != adapter.runtime {
		return HostError(fmt.Errorf("require: runtime mismatch"))
	}
	scope.runtime = adapter.runtime
	scope.from = adapter.from
	if !scope.resumable {
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
		export, err := adapter.runtime.requireCompletion(scope, required)
		if err != nil {
			return HostError(err)
		}
		return HostReturn(export)
	}
	return adapter.runtime.requireResumable(scope, args)
}

func (r *Runtime) requireAdapter(from moduleKey) Value {
	if r == nil {
		return Value{}
	}
	if r.requireAdapters == nil {
		r.requireAdapters = make(map[moduleKey]Value)
	}
	if value, ok := r.requireAdapters[from]; ok {
		return value
	}
	adapter := &runtimeRequireAdapter{runtime: r, from: from}
	var value Value
	if _, ok := r.execution.(vmRuntimeExecution); ok {
		value = yieldableHostFuncValue(func(globals *globalEnv, args []Value) vmHostCallResult {
			scope, resumable := invocationScopeFromGlobalEnv(globals)
			if !resumable || !scope.resumable {
				values, err := adapter.call(globals, args)
				return vmHostCallResult{values: values, err: err}
			}
			if globals != nil && globals.thread != nil && len(globals.thread.frames) != 0 {
				thread := globals.thread
				current := *thread.frames[len(thread.frames)-1]
				current.pc = previousWordcodeInstruction(current.proto, current.pc)
				frames := thread.captureScriptFrames(&current, 0)
				inherited := thread.inheritedScriptFrames
				if len(inherited) == 0 && thread.controller != nil {
					inherited = thread.controller.inheritedScriptFrames
				}
				scope.inheritedScriptFrames = append(frames, inherited...)
			}
			ctx := contextFromGlobalEnv(globals)
			ctx = contextWithInvocationScope(ctx, scope)
			return vmHostResult(adapter.callResumable(ctx, args))
		})
	} else {
		value = ResumableHostFuncValue(adapter.callResumable)
	}
	r.requireAdapters[from] = value
	return value
}

func (r *Runtime) requireCompletion(scope invocationScope, key moduleKey) (Value, error) {
	switch execution := r.execution.(type) {
	case vmRuntimeExecution:
		results, err := scope.runModule(key)
		return firstRuntimeResult(results), err
	case *machineRuntimeExecution:
		moduleID, ok := execution.image.moduleIDs[key]
		if !ok {
			return NilValue(), fmt.Errorf("module runtime: missing module %s", key.String())
		}
		childScope := scope
		childScope.from = key
		export, _, err := execution.owner.loadModuleStopped(
			moduleID,
			scope.controller,
			machineRunEffects{ctx: contextWithInvocationScope(scope.ctx, childScope)},
		)
		if err != nil {
			return NilValue(), err
		}
		exporter := machineTableExporter{
			machine: &execution.owner.scalarMachine,
			tables:  make(map[machineTableID]machineExportedTable),
		}
		return exporter.value(export)
	default:
		return NilValue(), fmt.Errorf("module runtime: execution is unavailable")
	}
}

func missingRuntimeRequire(*globalEnv, []Value) ([]Value, error) {
	return nil, fmt.Errorf("require: nil runtime")
}

func (r *Runtime) newInvocationScope(ctx context.Context, from moduleKey, globals map[string]Value, controller *executionController) invocationScope {
	if ctx == nil {
		ctx = context.Background()
	}
	return invocationScope{
		runtime:    r,
		ctx:        ctx,
		from:       from,
		globals:    globals,
		controller: controller,
	}
}

func (call invocationScope) envWithRequire() *globalEnv {
	var owner *runtimeOwner
	if call.runtime != nil {
		owner = call.runtime.owner
	}
	env := runtimeGlobalsWithInvocation(call.globals, owner, call)
	if call.runtime != nil {
		env.setRequire(call.runtime.requireAdapter(call.from))
	} else {
		env.setRequire(nativeFuncValue(missingRuntimeRequire))
	}
	return env
}

func (call invocationScope) require(globals *globalEnv, args []Value) ([]Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("require: missing module path")
	}
	request, ok := args[0].String()
	if !ok {
		return nil, fmt.Errorf("require: module path is %s, want string", args[0].Kind())
	}
	required, err := normalizeRequireKey(call.from, request)
	if err != nil {
		return nil, err
	}
	controller := call.controller
	ctx := call.ctx
	var inherited []ScriptFrame
	if controller == nil && globals != nil && globals.thread != nil {
		controller = globals.thread.controller
	}
	if globals != nil && globals.thread != nil {
		if globals.thread.ctx != nil {
			ctx = globals.thread.ctx
		}
		parentFrames := make([]ScriptFrame, 0)
		if len(globals.thread.frames) != 0 && globals.thread.frames[len(globals.thread.frames)-1] != nil {
			thread := globals.thread
			current := *thread.frames[len(thread.frames)-1]
			current.pc = previousWordcodeInstruction(current.proto, current.pc)
			parentFrames = thread.captureScriptFrames(&current, 0)
		}
		if len(globals.thread.inheritedScriptFrames) != 0 {
			parentFrames = append(parentFrames, globals.thread.inheritedScriptFrames...)
		} else if controller != nil {
			parentFrames = append(parentFrames, controller.inheritedScriptFrames...)
		}
		inherited = parentFrames
		if controller != nil {
			restore := controller.pushInheritedScriptFrames(parentFrames)
			defer restore()
		}
	}
	call.ctx = ctx
	results, err := call.runModuleWithInheritedFrames(required, inherited)
	if err != nil {
		return nil, err
	}
	return []Value{firstRuntimeResult(results)}, nil
}

func (call invocationScope) runModule(key moduleKey) ([]Value, error) {
	return call.runModuleWithInheritedFrames(key, nil)
}

func (call invocationScope) runModuleWithInheritedFrames(key moduleKey, inherited []ScriptFrame) ([]Value, error) {
	if call.runtime == nil {
		return nil, fmt.Errorf("module runtime: nil runtime")
	}
	return call.runtime.runModuleWithContextGlobalsController(call.ctx, key, call.globals, call.controller, inherited)
}

func firstRuntimeResult(results []Value) Value {
	if len(results) == 0 {
		return NilValue()
	}
	return results[0]
}
