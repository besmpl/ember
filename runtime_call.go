package ember

import (
	"context"
	"fmt"
)

type runtimeCallContext struct {
	runtime    *Runtime
	ctx        context.Context
	from       moduleKey
	globals    map[string]Value
	controller *executionController
}

func (r *Runtime) newRuntimeCallContext(ctx context.Context, from moduleKey, globals map[string]Value, controller *executionController) runtimeCallContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return runtimeCallContext{
		runtime:    r,
		ctx:        ctx,
		from:       from,
		globals:    globals,
		controller: controller,
	}
}

func (call runtimeCallContext) envWithRequire() *globalEnv {
	var owner *runtimeOwner
	if call.runtime != nil {
		owner = call.runtime.owner
	}
	env := runtimeGlobalsWithOwner(call.globals, owner)
	env.set("require", nativeFuncValue(call.require))
	return env
}

func (call runtimeCallContext) require(_ *globalEnv, args []Value) ([]Value, error) {
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
	results, err := call.runModule(required)
	if err != nil {
		return nil, err
	}
	return []Value{firstRuntimeResult(results)}, nil
}

func (call runtimeCallContext) runModule(key moduleKey) ([]Value, error) {
	if call.runtime == nil {
		return nil, fmt.Errorf("module runtime: nil runtime")
	}
	return call.runtime.runModuleWithContextGlobalsController(call.ctx, key, call.globals, call.controller)
}

func firstRuntimeResult(results []Value) Value {
	if len(results) == 0 {
		return NilValue()
	}
	return results[0]
}
