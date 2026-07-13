package ember

import "errors"

// ErrRuntimeBusy reports that a Runtime already has an active RunHook or
// Callback.Call execution.
var ErrRuntimeBusy = errors.New("ember: runtime busy")
