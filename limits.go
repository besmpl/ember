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
	// LimitSourceBytes identifies the source byte limit during compilation.
	LimitSourceBytes LimitKind = "source-bytes"
	// LimitTokens identifies the lexer token limit during compilation.
	LimitTokens LimitKind = "tokens"
	// LimitNesting identifies the parser nesting limit during compilation.
	LimitNesting LimitKind = "nesting"
	// LimitSyntaxNodes identifies the syntax node limit during compilation.
	LimitSyntaxNodes LimitKind = "syntax-nodes"
	// LimitModules identifies the program module-count limit.
	LimitModules LimitKind = "modules"
	// LimitTotalSourceBytes identifies the aggregate program source limit.
	LimitTotalSourceBytes LimitKind = "total-source-bytes"
	// LimitCallDepth identifies the aggregate script call depth limit.
	LimitCallDepth LimitKind = "call-depth"
	// LimitModuleInitializations identifies the lazy module initialization limit.
	LimitModuleInitializations LimitKind = "module-initializations"
	// LimitCoroutines identifies the live runtime-owned coroutine limit.
	LimitCoroutines LimitKind = "coroutines"
)

// ErrLimitExceeded reports that a configured execution or compilation
// resource limit was exhausted.
var ErrLimitExceeded = errors.New("ember: execution limit exceeded")

// LimitError describes an exhausted execution or compilation limit.
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
	MaxInstructions          uint64
	MaxCallDepth             uint32
	MaxModuleInitializations uint32
	MaxCoroutines            uint32
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
