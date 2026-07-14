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
	env.set("require", nativeFuncValue(call.require))
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
