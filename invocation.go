package ember

import (
	"context"
	"fmt"
)

// Invocation identifies one callable module export. If Export is empty, the
// module's return value is invoked directly. Globals are copied and used for
// both first-time module initialization and the exported function call.
type Invocation struct {
	Module  ModuleID
	Export  string
	Globals map[string]Value
}

// Invoke initializes one module if necessary and calls one of its exports. It
// returns every value produced by the script function. Use InvokeResumable when
// the function or its module dependencies may suspend.
func (r *Runtime) Invoke(ctx context.Context, invocation Invocation, args ...Value) ([]Value, error) {
	result, err := r.InvokeResumable(ctx, invocation, args...)
	if err != nil {
		return nil, err
	}
	if len(result.Suspensions) != 0 {
		for _, suspension := range result.Suspensions {
			_ = suspension.Cancel()
		}
		return nil, fmt.Errorf("runtime: invocation suspended; use InvokeResumable")
	}
	return result.Values, nil
}

// InvokeResumable initializes one module if necessary and calls one of its
// exports until completion or host suspension. Unlike DispatchResumable, it
// applies no entrypoint fan-out, ordering, skip, or reporting policy.
func (r *Runtime) InvokeResumable(ctx context.Context, invocation Invocation, args ...Value) (ExecutionResult, error) {
	key, module, execution, err := r.resolveInvocation(invocation.Module)
	if err != nil {
		return ExecutionResult{}, err
	}
	call := newDispatchCallReport(module.String(), module, invocation.Export)
	globals := copyGlobals(invocation.Globals)
	var target resumableTarget
	switch current := execution.(type) {
	case vmRuntimeExecution:
		target = &vmEntrypointTarget{
			runtime:    r,
			entrypoint: programEntrypoint{name: module.String(), key: key},
			hook:       invocation.Export,
			args:       append([]Value(nil), args...),
			call:       &call,
			globals:    globals,
			explicit:   true,
			direct:     invocation.Export == "",
			required:   true,
		}
	case *machineRuntimeExecution:
		moduleID, ok := current.image.moduleIDs[key]
		if !ok {
			return ExecutionResult{}, fmt.Errorf("runtime: invoke module %s: unavailable in execution image", module.String())
		}
		target = &machineEntrypointTarget{
			execution:  current,
			runtime:    r,
			entrypoint: programImageEntrypoint{name: module.String(), moduleID: moduleID},
			hook:       invocation.Export,
			args:       append([]Value(nil), args...),
			call:       &call,
			globals:    globals,
			explicit:   true,
			direct:     invocation.Export == "",
			required:   true,
		}
	default:
		return ExecutionResult{}, fmt.Errorf("runtime: invoke module %s: execution is unavailable", module.String())
	}
	outcome, err := resumeResumableTarget(target, ctx, args, nil, false)
	if err != nil {
		target.close()
		return ExecutionResult{}, fmt.Errorf("runtime: invoke module %s: %w", module.String(), err)
	}
	wrapper := &invocationResumableTarget{
		target:    target,
		module:    module,
		operation: invocation.Export,
	}
	return r.executionResult(wrapper.wrap(outcome)), nil
}

func (r *Runtime) resolveInvocation(id ModuleID) (moduleKey, ModuleID, runtimeExecution, error) {
	if r == nil {
		return moduleKey{}, ModuleID{}, nil, fmt.Errorf("runtime: invoke: nil runtime")
	}
	key, err := moduleKeyFromID(id)
	if err != nil {
		return moduleKey{}, ModuleID{}, nil, fmt.Errorf("runtime: invoke: %w", err)
	}
	module := moduleIDFromKey(key)
	r.closeMu.Lock()
	closed := r.closed
	program := r.program
	execution := r.execution
	r.closeMu.Unlock()
	if closed || program == nil || execution == nil {
		return moduleKey{}, ModuleID{}, nil, fmt.Errorf("runtime: closed")
	}
	if _, ok := program.graph.Nodes[key]; !ok {
		return moduleKey{}, ModuleID{}, nil, fmt.Errorf("runtime: invoke module %s: not in program", module.String())
	}
	return key, module, execution, nil
}

// invocationResumableTarget adds stable module/operation attribution while
// leaving scheduling and continuation ownership inside the engine target.
type invocationResumableTarget struct {
	target    resumableTarget
	module    ModuleID
	operation string
	blocked   bool
}

func (target *invocationResumableTarget) resume(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, false)
}

func (target *invocationResumableTarget) resumeStopped(ctx context.Context, values []Value, failure error) (resumableOutcome, error) {
	return target.resumeRun(ctx, values, failure, true)
}

func (target *invocationResumableTarget) resumeRun(ctx context.Context, values []Value, failure error, stopped bool) (resumableOutcome, error) {
	if target == nil || target.target == nil {
		return resumableOutcome{}, ErrSuspensionStale
	}
	var outcome resumableOutcome
	var err error
	if target.blocked {
		quiescent, ok := target.target.(quiescentResumableTarget)
		if !ok {
			return resumableOutcome{}, ErrSuspensionPending
		}
		var advanced bool
		outcome, advanced, err = quiescent.pump(ctx, stopped)
		if err == nil && !advanced {
			return resumableOutcome{}, ErrSuspensionPending
		}
	} else {
		outcome, err = resumeResumableTarget(target.target, ctx, values, failure, stopped)
	}
	if err != nil {
		target.close()
		return resumableOutcome{}, err
	}
	return target.wrap(outcome), nil
}

func (target *invocationResumableTarget) wrap(outcome resumableOutcome) resumableOutcome {
	if outcome.target == nil {
		target.target = nil
		target.blocked = false
		return outcome
	}
	target.target = outcome.target
	blocked := false
	if quiescent, ok := target.target.(quiescentResumableTarget); ok {
		blocked = !quiescent.hostVisible()
	}
	target.blocked = blocked
	pending := resumablePending{
		target:  target,
		token:   outcome.token,
		module:  target.module,
		hook:    target.operation,
		blocked: blocked,
	}
	if blocked {
		pending.bind = target.bindSuspensionState
	}
	outcome.target = nil
	outcome.token = nil
	outcome.blocked = false
	outcome.pending = append(outcome.pending, pending)
	return outcome
}

func (target *invocationResumableTarget) bindSuspensionState(state *suspensionState) {
	switch current := target.target.(type) {
	case *vmEntrypointTarget:
		current.bindDependencyState(state, target.close)
	case *machineEntrypointTarget:
		current.bindDependencyState(state, target.close)
	}
}

func (target *invocationResumableTarget) close() {
	if target == nil || target.target == nil {
		return
	}
	target.target.close()
	target.target = nil
}
