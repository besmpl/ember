package ember

import (
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrPreparedRuntimeUnavailable reports a slot with no active generation or
	// a slot that has already been closed.
	ErrPreparedRuntimeUnavailable = errors.New("ember: prepared runtime unavailable")
	// ErrPreparedRuntimeCandidateStale reports a candidate prepared from a
	// generation that is no longer active.
	ErrPreparedRuntimeCandidateStale = errors.New("ember: prepared runtime candidate is stale")
	// ErrPreparedRuntimeCandidateUsed reports a candidate that was already
	// activated or closed.
	ErrPreparedRuntimeCandidateUsed = errors.New("ember: prepared runtime candidate is used")
)

// PreparedRuntimeSlot owns one active prepared Runtime generation. Its zero
// value is ready for use. A slot must not be copied after first use.
//
// Prepare may run while the active generation is in use. Activate is a
// host-chosen safe-point operation: it fails with ErrRuntimeBusy rather than
// replacing a generation during Use. Runtime references passed to Use must not
// escape the callback.
type PreparedRuntimeSlot struct {
	mu         sync.Mutex
	active     *Runtime
	generation uint64
	running    bool
	closed     bool
}

// PreparedRuntimeCandidate is one fully bound, inert Runtime generation. A
// candidate belongs to the slot that prepared it. It may be activated once or
// closed; Close is idempotent. A candidate must not be copied.
type PreparedRuntimeCandidate struct {
	mu             sync.Mutex
	slot           *PreparedRuntimeSlot
	runtime        *Runtime
	baseGeneration uint64
	used           bool
}

// Prepare binds program to an exact generated bundle without changing or
// executing the active generation. options.Prepared must be non-nil.
func (slot *PreparedRuntimeSlot) Prepare(program *Program, options RuntimeOptions) (*PreparedRuntimeCandidate, error) {
	if slot == nil {
		return nil, fmt.Errorf("prepare runtime generation: %w", ErrPreparedRuntimeUnavailable)
	}
	if options.Prepared == nil {
		return nil, fmt.Errorf("prepare runtime generation: RuntimeOptions.Prepared is required")
	}

	slot.mu.Lock()
	if slot.closed {
		slot.mu.Unlock()
		return nil, fmt.Errorf("prepare runtime generation: %w", ErrPreparedRuntimeUnavailable)
	}
	baseGeneration := slot.generation
	slot.mu.Unlock()

	runtime, err := program.NewRuntime(options)
	if err != nil {
		return nil, fmt.Errorf("prepare runtime generation: %w", err)
	}

	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.closed {
		_ = runtime.Close()
		return nil, fmt.Errorf("prepare runtime generation: %w", ErrPreparedRuntimeUnavailable)
	}
	if slot.generation != baseGeneration {
		_ = runtime.Close()
		return nil, fmt.Errorf("prepare runtime generation: %w", ErrPreparedRuntimeCandidateStale)
	}
	return &PreparedRuntimeCandidate{
		slot:           slot,
		runtime:        runtime,
		baseGeneration: baseGeneration,
	}, nil
}

// Activate retires the current generation and publishes candidate. It does no
// compilation, loading, binding, or guest execution.
func (slot *PreparedRuntimeSlot) Activate(candidate *PreparedRuntimeCandidate) error {
	if slot == nil {
		return fmt.Errorf("activate runtime generation: %w", ErrPreparedRuntimeUnavailable)
	}
	if candidate == nil {
		return fmt.Errorf("activate runtime generation: nil candidate")
	}

	candidate.mu.Lock()
	defer candidate.mu.Unlock()
	if candidate.used || candidate.runtime == nil {
		return fmt.Errorf("activate runtime generation: %w", ErrPreparedRuntimeCandidateUsed)
	}
	if candidate.slot != slot {
		return fmt.Errorf("activate runtime generation: candidate belongs to another slot")
	}

	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.closed {
		return fmt.Errorf("activate runtime generation: %w", ErrPreparedRuntimeUnavailable)
	}
	if slot.generation != candidate.baseGeneration {
		return fmt.Errorf("activate runtime generation: %w", ErrPreparedRuntimeCandidateStale)
	}
	if slot.running {
		return fmt.Errorf("activate runtime generation: %w", ErrRuntimeBusy)
	}
	if slot.active != nil {
		if err := slot.active.Close(); err != nil {
			return fmt.Errorf("activate runtime generation: retire active generation: %w", err)
		}
	}

	slot.active = candidate.runtime
	slot.generation++
	candidate.runtime = nil
	candidate.used = true
	return nil
}

// Use calls fn synchronously with the active generation. Use serializes calls
// and prevents activation while fn is running. fn must not retain the Runtime.
func (slot *PreparedRuntimeSlot) Use(fn func(*Runtime) error) error {
	if slot == nil {
		return fmt.Errorf("use runtime generation: %w", ErrPreparedRuntimeUnavailable)
	}
	if fn == nil {
		return fmt.Errorf("use runtime generation: nil function")
	}

	slot.mu.Lock()
	if slot.closed || slot.active == nil {
		slot.mu.Unlock()
		return fmt.Errorf("use runtime generation: %w", ErrPreparedRuntimeUnavailable)
	}
	if slot.running {
		slot.mu.Unlock()
		return fmt.Errorf("use runtime generation: %w", ErrRuntimeBusy)
	}
	slot.running = true
	runtime := slot.active
	slot.mu.Unlock()

	defer func() {
		slot.mu.Lock()
		slot.running = false
		slot.mu.Unlock()
	}()
	return fn(runtime)
}

// Close retires the active generation. It is safe to call repeatedly. Close
// returns ErrRuntimeBusy while Use is running.
func (slot *PreparedRuntimeSlot) Close() error {
	if slot == nil {
		return nil
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.closed {
		return nil
	}
	if slot.running {
		return fmt.Errorf("close prepared runtime: %w", ErrRuntimeBusy)
	}
	if slot.active != nil {
		if err := slot.active.Close(); err != nil {
			return fmt.Errorf("close prepared runtime: %w", err)
		}
	}
	slot.active = nil
	slot.closed = true
	return nil
}

// Close releases an unactivated candidate. It is safe to call repeatedly and
// has no effect after successful activation.
func (candidate *PreparedRuntimeCandidate) Close() error {
	if candidate == nil {
		return nil
	}
	candidate.mu.Lock()
	defer candidate.mu.Unlock()
	if candidate.used {
		return nil
	}
	if candidate.runtime != nil {
		if err := candidate.runtime.Close(); err != nil {
			return fmt.Errorf("close prepared runtime candidate: %w", err)
		}
	}
	candidate.runtime = nil
	candidate.used = true
	return nil
}
