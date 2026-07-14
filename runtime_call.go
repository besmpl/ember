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
	runtime    *Runtime
	ctx        context.Context
	from       moduleKey
	globals    map[string]Value
	controller *executionController
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
	value := nativeFuncValue(adapter.call)
	r.requireAdapters[from] = value
	return value
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
		env.set("require", call.runtime.requireAdapter(call.from))
	} else {
		env.set("require", nativeFuncValue(missingRuntimeRequire))
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
