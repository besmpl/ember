package ember

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrSuspensionStale reports a suspension handle that was already consumed
	// or invalidated by Runtime.Close.
	ErrSuspensionStale = errors.New("suspension is stale")
)

// ExecutionResult is one completed or suspended resumable execution step.
// Values are populated for callback calls; Hook is populated for hook calls.
type ExecutionResult struct {
	Values []Value
	Hook   *HookReport
	// Suspensions contains every host-visible call still pending after the
	// execution step reaches quiescence. Hook suspensions are ordered by
	// declared entrypoint order.
	Suspensions []*Suspension
	// Suspension is the first pending suspension, retained for compatibility
	// with callers that drive one suspension chain at a time.
	Suspension *Suspension
}

// Suspension is an opaque, single-use handle for a suspended script
// invocation. Copies share the same state.
type Suspension struct {
	state *suspensionState
}

type resumableOutcome struct {
	values  []Value
	hook    *HookReport
	target  resumableTarget
	token   any
	pending []resumablePending
}

type resumablePending struct {
	target     resumableTarget
	token      any
	entrypoint string
	module     ModuleID
	hook       string
	state      *suspensionState
	bind       func(*suspensionState)
}

type resumableTarget interface {
	resume(context.Context, []Value, error) (resumableOutcome, error)
	close()
}

type quiescentResumableTarget interface {
	resumableTarget
	hostVisible() bool
	pump(context.Context) (resumableOutcome, bool, error)
}

type suspensionState struct {
	mu         sync.Mutex
	runtime    *Runtime
	target     resumableTarget
	token      any
	entrypoint string
	module     ModuleID
	hook       string
	used       bool
}

// Token returns the opaque token supplied by the host callback.
func (s *Suspension) Token() any {
	if s == nil || s.state == nil {
		return nil
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.used {
		return nil
	}
	return s.state.token
}

// Entrypoint returns the entrypoint owning this suspension, or an empty string
// for a retained callback call.
func (s *Suspension) Entrypoint() string {
	if s == nil || s.state == nil {
		return ""
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.entrypoint
}

// Module returns the module owning this suspension, or the zero ModuleID for
// a retained callback call.
func (s *Suspension) Module() ModuleID {
	if s == nil || s.state == nil {
		return ModuleID{}
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.module
}

// Hook returns the hook owning this suspension, or an empty string for a
// retained callback call.
func (s *Suspension) Hook() string {
	if s == nil || s.state == nil {
		return ""
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.hook
}

// Resume resumes the suspended host call with values.
func (s *Suspension) Resume(ctx context.Context, values ...Value) (ExecutionResult, error) {
	return s.consume(ctx, append([]Value(nil), values...), nil)
}

// Fail resumes the suspended host call by injecting err at the original call
// site.
func (s *Suspension) Fail(ctx context.Context, err error) (ExecutionResult, error) {
	if err == nil {
		err = errors.New("host resumed suspension with an error")
	}
	return s.consume(ctx, nil, err)
}

// Cancel abandons this pending invocation without resuming script execution.
// Other suspensions from the same hook step remain valid.
func (s *Suspension) Cancel() error {
	if s == nil || s.state == nil {
		return ErrSuspensionStale
	}
	state := s.state
	state.mu.Lock()
	if state.used || state.target == nil {
		state.mu.Unlock()
		return ErrSuspensionStale
	}
	state.used = true
	target := state.target
	runtime := state.runtime
	state.target = nil
	state.token = nil
	state.mu.Unlock()
	if runtime != nil {
		runtime.unregisterSuspension(state)
	}
	target.close()
	return nil
}

func (s *Suspension) consume(ctx context.Context, values []Value, failure error) (ExecutionResult, error) {
	if s == nil || s.state == nil {
		return ExecutionResult{}, ErrSuspensionStale
	}
	state := s.state
	state.mu.Lock()
	if state.used || state.target == nil {
		state.mu.Unlock()
		return ExecutionResult{}, ErrSuspensionStale
	}
	runtime := state.runtime
	state.mu.Unlock()
	if runtime != nil {
		if err := runtime.preflightSuspension(ctx); err != nil {
			return ExecutionResult{}, err
		}
	}
	state.mu.Lock()
	if state.used || state.target == nil {
		state.mu.Unlock()
		return ExecutionResult{}, ErrSuspensionStale
	}
	state.used = true
	target := state.target
	state.target = nil
	state.token = nil
	state.mu.Unlock()
	if runtime != nil {
		runtime.unregisterSuspension(state)
	}
	outcome, err := target.resume(ctx, values, failure)
	if err != nil {
		target.close()
		return ExecutionResult{}, err
	}
	return runtime.executionResult(outcome), nil
}

func (r *Runtime) preflightSuspension(ctx context.Context) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if r == nil {
		return fmt.Errorf("runtime: closed")
	}
	r.closeMu.Lock()
	closed := r.closed
	execution := r.execution
	r.closeMu.Unlock()
	if closed || execution == nil {
		return fmt.Errorf("runtime: closed")
	}
	switch current := execution.(type) {
	case vmRuntimeExecution:
		err := r.owner.preflightRun()
		if err == errRuntimeOwnerBusy {
			return fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
		}
		if err == errRuntimeOwnerClosed {
			return fmt.Errorf("runtime: closed")
		}
		return err
	case *machineRuntimeExecution:
		err := current.owner.preflightRun()
		if errors.Is(err, errMachineOwnerBusy) {
			return fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
		}
		if errors.Is(err, errMachineOwnerClosed) {
			return fmt.Errorf("runtime: closed")
		}
		return err
	default:
		return nil
	}
}

func (r *Runtime) executionResult(outcome resumableOutcome) ExecutionResult {
	result := ExecutionResult{
		Values: append([]Value(nil), outcome.values...),
		Hook:   outcome.hook,
	}
	if outcome.target != nil {
		outcome.pending = append(outcome.pending, resumablePending{
			target: outcome.target,
			token:  outcome.token,
		})
	}
	result.Suspensions = make([]*Suspension, 0, len(outcome.pending))
	for _, pending := range outcome.pending {
		state := pending.state
		if state == nil {
			state = &suspensionState{
				runtime:    r,
				target:     pending.target,
				token:      pending.token,
				entrypoint: pending.entrypoint,
				module:     pending.module,
				hook:       pending.hook,
			}
			if r != nil {
				r.registerSuspension(state)
			}
			if pending.bind != nil {
				pending.bind(state)
			}
		}
		result.Suspensions = append(result.Suspensions, &Suspension{state: state})
	}
	if len(result.Suspensions) != 0 {
		result.Suspension = result.Suspensions[0]
	}
	return result
}

func (r *Runtime) registerSuspension(state *suspensionState) {
	if r == nil || state == nil {
		return
	}
	r.suspensionMu.Lock()
	if r.suspensions == nil {
		r.suspensions = make(map[*suspensionState]struct{})
	}
	r.suspensions[state] = struct{}{}
	r.suspensionMu.Unlock()
}

func (r *Runtime) unregisterSuspension(state *suspensionState) {
	if r == nil || state == nil {
		return
	}
	r.suspensionMu.Lock()
	delete(r.suspensions, state)
	r.suspensionMu.Unlock()
}

func (r *Runtime) closeSuspensions() {
	if r == nil {
		return
	}
	r.suspensionMu.Lock()
	states := make([]*suspensionState, 0, len(r.suspensions))
	for state := range r.suspensions {
		states = append(states, state)
	}
	clear(r.suspensions)
	r.suspensionMu.Unlock()
	for _, state := range states {
		state.mu.Lock()
		target := state.target
		state.target = nil
		state.token = nil
		state.used = true
		state.mu.Unlock()
		if target != nil {
			target.close()
		}
	}
}

type hostResumeFailure struct {
	err error
}

func hostResumeFailureValue(err error) Value {
	return UserDataValue(NewUserData(hostResumeFailure{err: err}))
}

func hostResumeFailureFromValues(values []Value) (error, bool) {
	if len(values) != 1 {
		return nil, false
	}
	userdata, ok := values[0].UserData()
	if !ok {
		return nil, false
	}
	failure, ok := userdata.Payload().(hostResumeFailure)
	if !ok || failure.err == nil {
		return nil, false
	}
	return failure.err, true
}
