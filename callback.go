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
	target callbackTarget
}

type callbackTarget interface {
	call(context.Context, []Value) ([]Value, error)
	callResumable(context.Context, []Value) (resumableOutcome, error)
	close() error
}

type vmCallbackTarget struct {
	scope invocationScope
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
	call, ok := invocationScopeFromContext(ctx)
	if !ok || call.runtime == nil {
		return Callback{}, fmt.Errorf("callback: missing runtime context")
	}
	execution, err := call.runtime.executionAdapter()
	if err != nil {
		return Callback{}, fmt.Errorf("callback: capture runtime: %w", err)
	}
	target, err := execution.captureCallback(call, value)
	if err != nil {
		return Callback{}, err
	}
	return Callback{target: target}, nil
}

func captureVMCallback(call invocationScope, value Value) (callbackTarget, error) {
	if err := call.runtime.owner.checkOpen(); err == errRuntimeOwnerClosed {
		return nil, fmt.Errorf("callback: runtime is closed")
	} else if err != nil {
		return nil, fmt.Errorf("callback: capture runtime: %w", err)
	}
	if _, ok := value.scriptFunction(); !ok {
		return nil, fmt.Errorf("callback: value is %s, want script function", value.Kind())
	}
	rootValues := make([]Value, 0, 1+len(call.globals))
	rootValues = append(rootValues, value)
	for _, global := range call.globals {
		rootValues = append(rootValues, global)
	}
	roots, err := call.runtime.owner.rootValues(rootValues)
	if err != nil {
		return nil, fmt.Errorf("callback: retain values: %w", err)
	}
	call.controller = nil
	return &vmCallbackTarget{
		scope: call,
		value: value,
		state: &callbackState{roots: roots},
	}, nil
}

// Call invokes the captured callback with args. The provided context controls
// cancellation for this invocation and is visible to ContextHostFuncValue host
// callbacks called by the script.
func (cb Callback) Call(ctx context.Context, args ...Value) ([]Value, error) {
	if cb.target == nil {
		return nil, fmt.Errorf("callback: not captured")
	}
	return cb.target.call(ctx, args)
}

// CallResumable invokes the captured callback until completion or host
// suspension.
func (cb Callback) CallResumable(ctx context.Context, args ...Value) (ExecutionResult, error) {
	if cb.target == nil {
		return ExecutionResult{}, fmt.Errorf("callback: not captured")
	}
	outcome, err := cb.target.callResumable(ctx, args)
	if err != nil {
		return ExecutionResult{}, err
	}
	var runtime *Runtime
	switch target := cb.target.(type) {
	case *vmCallbackTarget:
		runtime = target.scope.runtime
	case *machineCallbackTarget:
		runtime = target.runtime
	}
	return runtime.executionResult(outcome), nil
}

func (target *vmCallbackTarget) call(ctx context.Context, args []Value) ([]Value, error) {
	if target == nil || target.state == nil {
		return nil, fmt.Errorf("callback: not retained")
	}
	lease, err := target.scope.runtime.beginRun()
	if err == errRuntimeOwnerClosed {
		return nil, fmt.Errorf("callback: runtime is closed")
	}
	if err == errRuntimeOwnerBusy {
		return nil, fmt.Errorf("callback: begin call: %w", ErrRuntimeBusy)
	}
	if err != nil {
		return nil, fmt.Errorf("callback: begin call: %w", err)
	}
	defer lease.end()
	target.state.mu.Lock()
	released := target.state.released
	target.state.mu.Unlock()
	if released {
		return nil, fmt.Errorf("callback: released")
	}
	if _, ok := target.value.scriptFunction(); !ok {
		return nil, fmt.Errorf("callback: value is %s, want script function", target.value.Kind())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	call := target.scope
	call.ctx = ctx
	controller, err := newExecutionPolicy(ctx, target.scope.runtime.limits)
	if err != nil {
		return nil, fmt.Errorf("callback: create execution controller: %w", err)
	}
	call.controller = controller
	closure, ok := target.value.scriptFunction()
	if !ok {
		return nil, fmt.Errorf("callback: value is %s, want script function", target.value.Kind())
	}
	return executeProtoWithInvocationScope(ctx, closure.proto, call, executeOptions{
		args:           args,
		upvalues:       closure.upvalues,
		upvalueValues:  closure.upvalueValues,
		upvalueValueOK: closure.upvalueValueOK,
		controller:     controller,
	})
}

// Close releases the callback's collector roots. Callback copies share the
// same capture state, so closing any copy invalidates all of them. Hosts should
// serialize Close with Call, as they already must serialize callback calls
// with other work on the owning Runtime.
func (cb Callback) Close() error {
	if cb.target == nil {
		return nil
	}
	return cb.target.close()
}

func (target *vmCallbackTarget) close() error {
	if target == nil || target.state == nil {
		return nil
	}
	target.state.mu.Lock()
	defer target.state.mu.Unlock()
	if target.state.released {
		return nil
	}
	for _, root := range target.state.roots {
		root.release()
	}
	target.state.roots = nil
	target.state.released = true
	return nil
}

type invocationScopeContextKey struct{}

func contextWithInvocationScope(ctx context.Context, scope invocationScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, invocationScopeContextKey{}, scope)
}

func invocationScopeFromContext(ctx context.Context) (invocationScope, bool) {
	if ctx == nil {
		return invocationScope{}, false
	}
	scope, ok := ctx.Value(invocationScopeContextKey{}).(invocationScope)
	return scope, ok
}
