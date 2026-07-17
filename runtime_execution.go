package ember

import (
	"context"
	"fmt"
	"os"
)

const runtimeEngineEnvironment = "EMBER_RUNTIME_ENGINE"

// runtimeExecution owns the entry operations whose mutable state must stay on
// one execution engine for the lifetime of a Runtime.
type runtimeExecution interface {
	initialize(*Runtime) error
	runHook(*Runtime, context.Context, string, []Value, *HookReport) error
	runHookResumable(*Runtime, context.Context, string, []Value) (resumableOutcome, error)
	captureCallback(invocationScope, Value) (callbackTarget, error)
	close(*Runtime) error
}

type vmRuntimeExecution struct{}

func selectRuntimeExecution(program *Program, prepared *PreparedBundle) (runtimeExecution, error) {
	if prepared != nil {
		return selectMachineRuntimeExecution(program, prepared.machineProgram())
	}
	switch engine := os.Getenv(runtimeEngineEnvironment); engine {
	case "", "vm":
		return vmRuntimeExecution{}, nil
	case "machine":
		return selectMachineRuntimeExecution(program, nil)
	default:
		return nil, fmt.Errorf("runtime: invalid %s %q (want vm or machine)", runtimeEngineEnvironment, engine)
	}
}

func selectMachineRuntimeExecution(program *Program, prepared *machinePreparedProgram) (runtimeExecution, error) {
	image, err := program.preparedProgramImage()
	if err != nil {
		return nil, fmt.Errorf("runtime: prepare machine Program image: %w", err)
	}
	for _, module := range image.modules {
		if module.code == nil || !module.code.eligible {
			reason := "missing code image"
			if module.code != nil && module.code.rejectReason != "" {
				reason = module.code.rejectReason
			}
			return nil, fmt.Errorf("runtime: machine Program is ineligible: module %s: %s", module.key.String(), reason)
		}
	}
	return &machineRuntimeExecution{image: image, prepared: prepared}, nil
}

func (r *Runtime) executionAdapter() (runtimeExecution, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime: nil runtime")
	}
	r.closeMu.Lock()
	execution := r.execution
	closed := r.closed
	r.closeMu.Unlock()
	if execution == nil && closed {
		return nil, fmt.Errorf("runtime: closed")
	}
	if execution == nil {
		return nil, fmt.Errorf("runtime: execution owner is unavailable")
	}
	return execution, nil
}

func (vmRuntimeExecution) initialize(r *Runtime) error {
	if r == nil {
		return fmt.Errorf("runtime: initialize VM execution: nil runtime")
	}
	owner := newRuntimeOwner()
	owner.coroutineLimit = r.limits.MaxCoroutines
	r.owner = owner
	r.entrypoints = make(map[moduleKey]Value)
	r.loaded = make(map[moduleKey]Value)
	r.requireAdapters = make(map[moduleKey]Value)
	r.moduleInitializers = make(map[moduleKey]*runtimeModuleInitialization)
	r.active = make(map[moduleKey]bool)
	return nil
}

func (vmRuntimeExecution) runHook(r *Runtime, ctx context.Context, hook string, args []Value, report *HookReport) error {
	return r.runVMHook(ctx, hook, args, report)
}

func (vmRuntimeExecution) captureCallback(call invocationScope, value Value) (callbackTarget, error) {
	return captureVMCallback(call, value)
}

func (vmRuntimeExecution) close(r *Runtime) error {
	return r.closeVM()
}
