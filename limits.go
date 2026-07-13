package ember

import (
	"errors"
	"fmt"
)

// LimitKind identifies the execution resource constrained by a LimitError.
type LimitKind string

const (
	// LimitInstructions identifies the instruction execution limit.
	LimitInstructions LimitKind = "instructions"
)

// ErrLimitExceeded reports that an execution resource limit was exhausted.
var ErrLimitExceeded = errors.New("ember: execution limit exceeded")

// LimitError describes an exhausted execution limit.
type LimitError struct {
	Kind  LimitKind
	Limit uint64
	Used  uint64
}

// Error describes the exhausted limit and observed usage.
func (e *LimitError) Error() string {
	if e == nil {
		return "ember: execution limit exceeded"
	}
	return fmt.Sprintf("ember: %s limit exceeded (limit %d, used %d)", e.Kind, e.Limit, e.Used)
}

// Is allows callers to classify the error with errors.Is.
func (e *LimitError) Is(target error) bool {
	return target == ErrLimitExceeded
}

// ExecutionLimits configures resource limits for one runtime invocation.
// Zero means unlimited.
type ExecutionLimits struct {
	MaxInstructions uint64
}

func normalizeExecutionLimits(legacy uint64, limits ExecutionLimits) (ExecutionLimits, error) {
	modern := limits.MaxInstructions
	if legacy != 0 && modern != 0 && legacy != modern {
		return ExecutionLimits{}, fmt.Errorf("runtime: conflicting max instructions %d and %d", legacy, modern)
	}
	if modern != 0 {
		return limits, nil
	}
	limits.MaxInstructions = legacy
	return limits, nil
}
