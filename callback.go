package ember

import (
	"context"
	"fmt"
)

// Callback is a script function captured from an active runtime call.
// Host adapters can store it and invoke it later with Call.
// A Callback shares mutable state with its owning Runtime; hosts should
// serialize calls with other work on that Runtime.
type Callback struct {
	call  runtimeCallContext
	value Value
}

// CaptureCallback validates value as a callback and captures the active
// runtime call scope from ctx. Pass the context received by a
// ContextHostFuncValue while Ember is executing script code.
func CaptureCallback(ctx context.Context, value Value) (Callback, error) {
	call, ok := runtimeCallContextFromContext(ctx)
	if !ok || call.runtime == nil {
		return Callback{}, fmt.Errorf("callback: missing runtime context")
	}
	lease, err := call.runtime.beginRun()
	if err == errRuntimeOwnerClosed {
		return Callback{}, fmt.Errorf("callback: runtime is closed")
	}
	if err != nil {
		return Callback{}, fmt.Errorf("callback: capture runtime: %w", err)
	}
	lease.end()
	if _, ok := value.scriptFunction(); !ok {
		return Callback{}, fmt.Errorf("callback: value is %s, want script function", value.Kind())
	}
	return Callback{
		call:  call,
		value: value,
	}, nil
}

// Call invokes the captured callback with args. The provided context controls
// cancellation for this invocation and is visible to ContextHostFuncValue host
// callbacks called by the script.
func (cb Callback) Call(ctx context.Context, args ...Value) ([]Value, error) {
	if cb.call.runtime == nil {
		return nil, fmt.Errorf("callback: not captured")
	}
	lease, err := cb.call.runtime.beginRun()
	if err == errRuntimeOwnerClosed {
		return nil, fmt.Errorf("callback: runtime is closed")
	}
	if err != nil {
		return nil, fmt.Errorf("callback: begin call: %w", err)
	}
	defer lease.end()
	if _, ok := cb.value.scriptFunction(); !ok {
		return nil, fmt.Errorf("callback: value is %s, want script function", cb.value.Kind())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	call := cb.call
	call.ctx = ctx
	callCtx := contextWithRuntimeCallContext(ctx, call)
	return callValueWithContextBudget(callCtx, cb.value, call.envWithRequire(), args, call.maxInstructions)
}

type runtimeCallContextKey struct{}

func contextWithRuntimeCallContext(ctx context.Context, call runtimeCallContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runtimeCallContextKey{}, call)
}

func runtimeCallContextFromContext(ctx context.Context) (runtimeCallContext, bool) {
	if ctx == nil {
		return runtimeCallContext{}, false
	}
	call, ok := ctx.Value(runtimeCallContextKey{}).(runtimeCallContext)
	return call, ok
}
