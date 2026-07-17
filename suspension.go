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
	// ErrSuspensionPending reports a retained invocation that is waiting for
	// another suspension owned by the same runtime. The handle remains usable.
	ErrSuspensionPending = errors.New("suspension is waiting for another suspension")
)

// ExecutionResult is one completed or suspended resumable execution step.
// Values are populated for invocation and callback calls; Dispatch is populated
// for dispatch calls.
type ExecutionResult struct {
	Values   []Value
	Dispatch *DispatchReport
	// Hook is the legacy name for Dispatch.
	//
	// Deprecated: use Dispatch.
	Hook *HookReport
	// Suspensions contains every host-visible call still pending after the
	// execution step reaches quiescence. If an independently started operation
	// is waiting on another operation's suspension, it instead contains one
	// tokenless dependency continuation. Dispatch suspensions are ordered by
	// declared entrypoint order.
	Suspensions []*Suspension
	// Suspension is the first pending suspension, retained for compatibility
	// with callers that drive one suspension chain at a time.
	Suspension *Suspension
}

// Suspension is an opaque, single-use handle for a suspended script invocation
// or a tokenless continuation waiting on another invocation. Copies share the
// same state.
type Suspension struct {
	state *suspensionState
}

type resumableOutcome struct {
	values  []Value
	hook    *HookReport
	target  resumableTarget
	token   any
	blocked bool
	pending []resumablePending
}

type resumablePending struct {
	target     resumableTarget
	token      any
	entrypoint string
	module     ModuleID
	hook       string
	blocked    bool
	state      *suspensionState
	bind       func(*suspensionState)
}

type resumableTarget interface {
	resume(context.Context, []Value, error) (resumableOutcome, error)
	close()
}

type stoppedResumableTarget interface {
	resumeStopped(context.Context, []Value, error) (resumableOutcome, error)
}

func resumeResumableTarget(target resumableTarget, ctx context.Context, values []Value, failure error, stopped bool) (resumableOutcome, error) {
	if !stopped {
		return target.resume(ctx, values, failure)
	}
	stoppedTarget, ok := target.(stoppedResumableTarget)
	if !ok {
		return resumableOutcome{}, fmt.Errorf("runtime: resumable target cannot use admitted run")
	}
	return stoppedTarget.resumeStopped(ctx, values, failure)
}

type quiescentResumableTarget interface {
	resumableTarget
	hostVisible() bool
	pump(context.Context, bool) (resumableOutcome, bool, error)
}

type blockedResumableTarget interface {
	bindSuspensionState(*suspensionState)
}

type suspensionState struct {
	mu         sync.Mutex
	runtime    *Runtime
	target     resumableTarget
	token      any
	entrypoint string
	module     ModuleID
	hook       string
	blocked    bool
	ready      bool
	used       bool
	canceled   bool
}

// Token returns the opaque token supplied by the host callback. It returns nil
// for a dependency continuation.
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

// Ready reports whether Resume or Fail can advance this handle now. Host
// suspensions are immediately ready. A tokenless dependency continuation
// becomes ready after the suspension it depends on completes or fails.
func (s *Suspension) Ready() bool {
	if s == nil || s.state == nil {
		return false
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return !s.state.used && s.state.target != nil && (!s.state.blocked || s.state.ready)
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

// Operation returns the named operation owning this suspension, or an empty
// string for a direct module invocation or retained callback call.
func (s *Suspension) Operation() string {
	if s == nil || s.state == nil {
		return ""
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.hook
}

// Hook is the legacy name for Operation.
//
// Deprecated: use Operation.
func (s *Suspension) Hook() string {
	return s.Operation()
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
// Other suspensions from the same dispatch step remain valid.
func (s *Suspension) Cancel() error {
	if s == nil || s.state == nil {
		return ErrSuspensionStale
	}
	state := s.state
	state.mu.Lock()
	if state.canceled {
		state.mu.Unlock()
		return nil
	}
	if state.used || state.target == nil {
		state.mu.Unlock()
		return ErrSuspensionStale
	}
	state.used = true
	state.canceled = true
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
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		state.mu.Unlock()
		return ExecutionResult{}, err
	}
	if state.blocked && !state.ready {
		state.mu.Unlock()
		return ExecutionResult{}, ErrSuspensionPending
	}
	runtime := state.runtime
	admission, err := runtime.beginSuspensionRun()
	if err != nil {
		state.mu.Unlock()
		return ExecutionResult{}, err
	}
	state.used = true
	target := state.target
	state.target = nil
	state.token = nil
	state.mu.Unlock()
	defer admission.end()
	if runtime != nil {
		runtime.unregisterSuspension(state)
	}
	outcome, err := resumeResumableTarget(target, ctx, values, failure, true)
	if err != nil {
		target.close()
		return ExecutionResult{}, err
	}
	return runtime.executionResult(outcome), nil
}

type suspensionRunAdmission struct {
	endRun func()
}

func (admission *suspensionRunAdmission) end() {
	if admission != nil && admission.endRun != nil {
		admission.endRun()
		admission.endRun = nil
	}
}

func (r *Runtime) beginSuspensionRun() (*suspensionRunAdmission, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime: closed")
	}
	r.closeMu.Lock()
	closed := r.closed
	execution := r.execution
	r.closeMu.Unlock()
	if closed || execution == nil {
		return nil, fmt.Errorf("runtime: closed")
	}
	switch current := execution.(type) {
	case vmRuntimeExecution:
		lease, err := r.beginRun()
		if err == errRuntimeOwnerBusy {
			return nil, fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
		}
		if err == errRuntimeOwnerClosed {
			return nil, fmt.Errorf("runtime: closed")
		}
		if err != nil {
			return nil, err
		}
		return &suspensionRunAdmission{endRun: lease.end}, nil
	case *machineRuntimeExecution:
		lease, err := current.owner.beginRun()
		if errors.Is(err, errMachineOwnerBusy) {
			return nil, fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
		}
		if errors.Is(err, errMachineOwnerClosed) {
			return nil, fmt.Errorf("runtime: closed")
		}
		if err != nil {
			return nil, err
		}
		return &suspensionRunAdmission{endRun: lease.end}, nil
	default:
		return nil, fmt.Errorf("runtime: resumable execution is unavailable")
	}
}

func (r *Runtime) executionResult(outcome resumableOutcome) ExecutionResult {
	result := ExecutionResult{
		Values:   append([]Value(nil), outcome.values...),
		Dispatch: outcome.hook,
		Hook:     outcome.hook,
	}
	if outcome.target != nil {
		pending := resumablePending{
			target:  outcome.target,
			token:   outcome.token,
			blocked: outcome.blocked,
		}
		if target, ok := outcome.target.(blockedResumableTarget); ok && outcome.blocked {
			pending.bind = target.bindSuspensionState
		}
		outcome.pending = append(outcome.pending, pending)
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
				blocked:    pending.blocked,
				ready:      !pending.blocked,
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

func (state *suspensionState) markReady() {
	if state == nil {
		return
	}
	state.mu.Lock()
	if !state.used && state.target != nil {
		state.ready = true
	}
	state.mu.Unlock()
}

func (state *suspensionState) invalidate() {
	if state == nil {
		return
	}
	state.mu.Lock()
	if state.used {
		state.mu.Unlock()
		return
	}
	runtime := state.runtime
	state.used = true
	state.target = nil
	state.token = nil
	state.mu.Unlock()
	if runtime != nil {
		runtime.unregisterSuspension(state)
	}
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
