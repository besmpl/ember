package ember

import (
	"context"
	"errors"
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
	Values     []Value
	Hook       *HookReport
	Suspension *Suspension
}

// Suspension is an opaque, single-use handle for a suspended script
// invocation. Copies share the same state.
type Suspension struct {
	state *suspensionState
}

type resumableOutcome struct {
	values []Value
	hook   *HookReport
	target resumableTarget
	token  any
}

type resumableTarget interface {
	resume(context.Context, []Value, error) (resumableOutcome, error)
	close()
}

type suspensionState struct {
	mu      sync.Mutex
	runtime *Runtime
	target  resumableTarget
	token   any
	used    bool
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
	state.used = true
	target := state.target
	runtime := state.runtime
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

func (r *Runtime) executionResult(outcome resumableOutcome) ExecutionResult {
	result := ExecutionResult{
		Values: append([]Value(nil), outcome.values...),
		Hook:   outcome.hook,
	}
	if outcome.target != nil {
		state := &suspensionState{
			runtime: r,
			target:  outcome.target,
			token:   outcome.token,
		}
		result.Suspension = &Suspension{state: state}
		if r != nil {
			r.registerSuspension(state)
		}
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
