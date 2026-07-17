package ember

import (
	"errors"
	"fmt"
)

// ErrRuntimeBusy reports that a Runtime already has an active execution, or
// that a completion-only call cannot start while module initialization is
// suspended.
var ErrRuntimeBusy = errors.New("ember: runtime busy")

func (r *Runtime) pendingModuleInitializationError(operation string) error {
	if r == nil || len(r.moduleInitializers) == 0 {
		return nil
	}
	return fmt.Errorf("%s: module initialization is suspended: %w", operation, ErrRuntimeBusy)
}
