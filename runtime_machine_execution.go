package ember

import (
	"context"
	"errors"
	"fmt"
)

// machineRuntimeExecution is the one persistent-Machine adapter selected for
// a Runtime. Mutable Machine state stays here rather than occupying the VM
// fields on Runtime.
type machineRuntimeExecution struct {
	image *programImage
	owner *machineOwner
}

func (execution *machineRuntimeExecution) initialize(runtime *Runtime) error {
	if execution == nil || execution.image == nil {
		return fmt.Errorf("runtime: initialize Machine execution: missing Program image")
	}
	if runtime == nil {
		return fmt.Errorf("runtime: initialize Machine execution: nil runtime")
	}
	owner, err := newMachineOwner(execution.image)
	if err != nil {
		return fmt.Errorf("runtime: initialize Machine execution: %w", err)
	}
	execution.owner = owner
	owner.coroutines.arena.limit = runtime.limits.MaxCoroutines
	runtime.moduleInitializers = make(map[moduleKey]*runtimeModuleInitialization)
	return nil
}

func (execution *machineRuntimeExecution) runHook(runtime *Runtime, ctx context.Context, hook string, args []Value, report *HookReport) error {
	if !execution.usesResumableRequire() {
		return execution.runMachineHook(runtime, ctx, hook, args, report)
	}
	outcome, err := execution.runHookResumable(runtime, ctx, hook, args)
	if err != nil {
		return err
	}
	if len(outcome.pending) != 0 || outcome.target != nil {
		for _, pending := range outcome.pending {
			if pending.target != nil {
				pending.target.close()
			}
		}
		if outcome.target != nil {
			outcome.target.close()
		}
		return fmt.Errorf("runtime: hook %s suspended in completion-only RunHook", hook)
	}
	if report != nil && outcome.hook != nil {
		*report = *outcome.hook
	}
	return nil
}

func (execution *machineRuntimeExecution) usesResumableRequire() bool {
	if execution == nil || execution.image == nil {
		return false
	}
	for _, name := range execution.image.globalNames {
		if name == "require" {
			return true
		}
	}
	return false
}

func (execution *machineRuntimeExecution) captureCallback(call invocationScope, value Value) (callbackTarget, error) {
	return captureMachineCallback(execution, call, value)
}

func (execution *machineRuntimeExecution) close(runtime *Runtime) error {
	if execution == nil || execution.owner == nil {
		return nil
	}
	if err := execution.owner.close(); err != nil {
		if errors.Is(err, errMachineOwnerActive) {
			return fmt.Errorf("runtime: active")
		}
		return fmt.Errorf("runtime: close: %w", err)
	}
	if runtime != nil {
		runtime.closeMu.Lock()
		runtime.closed = true
		runtime.program = nil
		runtime.host = nil
		runtime.moduleInitializers = nil
		runtime.closeMu.Unlock()
	}
	return nil
}

func (execution *machineRuntimeExecution) runMachineHook(runtime *Runtime, ctx context.Context, hook string, args []Value, report *HookReport) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if runtime == nil || execution == nil || execution.owner == nil || execution.image == nil {
		return fmt.Errorf("runtime: Machine execution is unavailable")
	}
	lease, err := execution.owner.beginRun()
	if errors.Is(err, errMachineOwnerClosed) {
		return fmt.Errorf("runtime: closed")
	}
	if errors.Is(err, errMachineOwnerBusy) {
		return fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
	}
	if err != nil {
		return fmt.Errorf("runtime: begin run: %w", err)
	}
	defer lease.end()

	if hook == "" {
		return fmt.Errorf("runtime: empty hook")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	controller, err := newExecutionPolicy(ctx, runtime.limits)
	if err != nil {
		return fmt.Errorf("runtime: create execution controller: %w", err)
	}

	for _, entrypoint := range execution.image.entrypoints {
		module := execution.image.modules[entrypoint.moduleID]
		moduleID := moduleIDFromKey(module.key)
		call := HookCallReport{
			Entrypoint: entrypoint.name,
			Module:     moduleID,
			Hook:       hook,
		}

		loadGlobals, err := runtime.hostGlobals(ctx, HostCall{
			Entrypoint: entrypoint.name,
			Module:     moduleID,
		})
		if err != nil {
			return fmt.Errorf("runtime: host globals for %s load: %w", entrypoint.name, err)
		}
		if err := execution.owner.importGlobalsStopped(loadGlobals); err != nil {
			return fmt.Errorf("runtime: import host globals for %s load: %w", entrypoint.name, err)
		}
		loadScope := runtime.newInvocationScope(ctx, module.key, loadGlobals, controller)
		export, loaded, err := execution.owner.loadModuleStopped(
			entrypoint.moduleID,
			controller,
			machineRunEffects{ctx: contextWithInvocationScope(ctx, loadScope)},
		)
		if err != nil {
			return fmt.Errorf("runtime: load entrypoint %s: %w", entrypoint.name, err)
		}
		call.Loaded = loaded

		hookGlobals, err := runtime.hostGlobals(ctx, HostCall{
			Entrypoint: entrypoint.name,
			Module:     moduleID,
			Hook:       hook,
		})
		if err != nil {
			return fmt.Errorf("runtime: host globals for %s.%s: %w", entrypoint.name, hook, err)
		}
		if err := execution.owner.importGlobalsStopped(hookGlobals); err != nil {
			return fmt.Errorf("runtime: import host globals for %s.%s: %w", entrypoint.name, hook, err)
		}

		hookValue, skipped, err := machineHookValueStopped(execution.owner, export, hook)
		if err != nil {
			return fmt.Errorf("runtime: entrypoint %s returned %s, want table or nil", entrypoint.name, slotValueKind(export))
		}
		if skipped {
			call.Skipped = true
			appendHookCallReport(report, call)
			continue
		}
		if slotValueKind(hookValue) != FunctionKind {
			return fmt.Errorf("runtime: hook %s.%s is %s, want function", entrypoint.name, hook, slotValueKind(hookValue))
		}
		machineArgs, err := importMachineValuesStopped(execution.owner, args)
		if err != nil {
			return fmt.Errorf("runtime: import hook %s.%s arguments: %w", entrypoint.name, hook, err)
		}
		hookScope := runtime.newInvocationScope(ctx, module.key, hookGlobals, controller)
		if err := execution.owner.executeClosureStopped(
			hookValue,
			machineArgs,
			controller,
			machineRunEffects{ctx: contextWithInvocationScope(ctx, hookScope)},
		); err != nil {
			return fmt.Errorf("runtime: call hook %s.%s: %w", entrypoint.name, hook, err)
		}
		call.Called = true
		appendHookCallReport(report, call)
	}
	return nil
}

func machineHookValueStopped(owner *machineOwner, export slot, hook string) (slot, bool, error) {
	if export == slotNil {
		return slotNil, true, nil
	}
	if slotValueKind(export) != TableKind {
		return slotNil, false, fmt.Errorf("entrypoint export is not a table")
	}
	tableID, err := owner.tableID(export)
	if err != nil {
		return slotNil, false, err
	}
	nameID, err := owner.strings.internStringStopped(hook)
	if err != nil {
		return slotNil, false, err
	}
	value, err := owner.tables.rawGet(tableID, machineTableStringKey(nameID))
	if err != nil {
		return slotNil, false, err
	}
	return value, value == slotNil, nil
}

func importMachineValuesStopped(owner *machineOwner, values []Value) ([]slot, error) {
	if len(values) == 0 {
		return nil, nil
	}
	imported := make([]slot, len(values))
	for index, value := range values {
		item, err := owner.importValueStopped(value)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", index+1, err)
		}
		imported[index] = item
	}
	return imported, nil
}
