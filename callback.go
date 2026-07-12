package ember

import (
	"context"
	"fmt"
	"sync"
)

// Callback is a script function captured from an active runtime call.
// Host adapters can store it and invoke it later with Call.
// A Callback shares mutable state with its owning Runtime; hosts should
// serialize calls with other work on that Runtime.
type Callback struct {
	call  runtimeCallContext
	value Value
	state *callbackState
}

type callbackState struct {
	mu       sync.Mutex
	roots    []*runtimeRoot
	released bool
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
	rootValues := make([]Value, 0, 1+len(call.globals))
	rootValues = append(rootValues, value)
	for _, global := range call.globals {
		rootValues = append(rootValues, global)
	}
	roots, err := call.runtime.owner.rootValues(rootValues)
	if err != nil {
		return Callback{}, fmt.Errorf("callback: retain values: %w", err)
	}
	return Callback{
		call:  call,
		value: value,
		state: &callbackState{roots: roots},
	}, nil
}

// Call invokes the captured callback with args. The provided context controls
// cancellation for this invocation and is visible to ContextHostFuncValue host
// callbacks called by the script.
func (cb Callback) Call(ctx context.Context, args ...Value) ([]Value, error) {
	if cb.call.runtime == nil {
		return nil, fmt.Errorf("callback: not captured")
	}
	if cb.state == nil {
		return nil, fmt.Errorf("callback: not retained")
	}
	lease, err := cb.call.runtime.beginRun()
	if err == errRuntimeOwnerClosed {
		return nil, fmt.Errorf("callback: runtime is closed")
	}
	if err != nil {
		return nil, fmt.Errorf("callback: begin call: %w", err)
	}
	defer lease.end()
	cb.state.mu.Lock()
	released := cb.state.released
	cb.state.mu.Unlock()
	if released {
		return nil, fmt.Errorf("callback: released")
	}
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

// Close releases the callback's collector roots. Callback copies share the
// same capture state, so closing any copy invalidates all of them. Hosts should
// serialize Close with Call, as they already must serialize callback calls
// with other work on the owning Runtime.
func (cb Callback) Close() error {
	if cb.state == nil {
		return nil
	}
	cb.state.mu.Lock()
	defer cb.state.mu.Unlock()
	if cb.state.released {
		return nil
	}
	for _, root := range cb.state.roots {
		root.release()
	}
	cb.state.roots = nil
	cb.state.released = true
	return nil
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
