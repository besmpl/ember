package ember

import (
	"context"
	"fmt"
	"strings"
)

func (r *Runtime) runModuleWithContextGlobalsBudget(ctx context.Context, key moduleKey, globals map[string]Value, maxInstructions int) ([]Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if value, ok := r.loaded[key]; ok {
		return []Value{value}, nil
	}
	if r.active[key] {
		return nil, fmt.Errorf("module runtime: active-loading cycle %s", runtimeModuleCyclePath(r.stack, key))
	}
	proto, ok := r.program.protos[key]
	if !ok {
		return nil, fmt.Errorf("module runtime: missing proto for %s", key.String())
	}

	r.active[key] = true
	r.stack = append(r.stack, key)
	defer func() {
		delete(r.active, key)
		r.stack = r.stack[:len(r.stack)-1]
	}()

	env := runtimeGlobals(globals)
	env.set("require", nativeFuncValue(func(_ *globalEnv, args []Value) ([]Value, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("require: missing module path")
		}
		request, ok := args[0].String()
		if !ok {
			return nil, fmt.Errorf("require: module path is %s, want string", args[0].Kind())
		}
		required, err := normalizeRequireKey(key, request)
		if err != nil {
			return nil, err
		}
		results, err := r.runModuleWithContextGlobalsBudget(ctx, required, globals, maxInstructions)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return []Value{NilValue()}, nil
		}
		return []Value{results[0]}, nil
	}))

	results, err := executeProto(ctx, proto, env, executeOptions{
		maxInstructions: maxInstructions,
	})
	if err != nil {
		return nil, err
	}
	value := NilValue()
	if len(results) > 0 {
		value = results[0]
	}
	r.loaded[key] = value
	return results, nil
}

func runtimeModuleCyclePath(stack []moduleKey, key moduleKey) string {
	start := 0
	for i, active := range stack {
		if active == key {
			start = i
			break
		}
	}
	path := make([]string, 0, len(stack)-start+1)
	for _, active := range stack[start:] {
		path = append(path, active.String())
	}
	path = append(path, key.String())
	return strings.Join(path, " -> ")
}
