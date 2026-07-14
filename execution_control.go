package ember

import (
	"context"
	"fmt"
)

const maxInt64Uint = uint64(1<<63 - 1)

const executionPollInterval uint32 = 256

// executionWindow keeps the hot-loop safety state local to the active direct
// loop. The controller remains the shared ownership boundary; the window
// commits charged state whenever the loop yields control.
type executionWindow struct {
	controller *executionController
	remaining  int64
	pollLeft   uint32
}

func newExecutionWindow(controller *executionController) executionWindow {
	window := executionWindow{controller: controller, remaining: -1}
	if controller != nil {
		window.remaining = controller.remaining
		if controller.onStep != nil {
			onStep := controller.onStep
			controller.onStep = nil
			onStep()
		}
	}
	return window
}

func (window *executionWindow) stepInstruction() error {
	if window == nil || window.controller == nil {
		return nil
	}
	controller := window.controller
	if window.pollLeft == 0 {
		window.pollLeft = executionPollInterval
		if err := controller.checkContext(); err != nil {
			return err
		}
	}
	if window.remaining >= 0 {
		if window.remaining == 0 {
			return window.limitError(1)
		}
		window.remaining--
	}
	window.pollLeft--
	return nil
}

func (window *executionWindow) limitError(count uint64) error {
	if window == nil || window.controller == nil {
		return nil
	}
	controller := window.controller
	used := controller.limits.MaxInstructions - uint64(window.remaining)
	if count > ^uint64(0)-used {
		used = ^uint64(0)
	} else {
		used += count
	}
	return &LimitError{Kind: LimitInstructions, Limit: controller.limits.MaxInstructions, Used: used}
}

func (window *executionWindow) flush() {
	if window == nil || window.controller == nil || window.remaining < 0 {
		return
	}
	window.controller.remaining = window.remaining
}

func (window *executionWindow) commit() {
	if window == nil {
		return
	}
	window.flush()
}

func (window *executionWindow) refresh() {
	if window == nil || window.controller == nil {
		return
	}
	window.remaining = window.controller.remaining
	window.pollLeft = executionPollInterval
}

type executionController struct {
	ctx                   context.Context
	limits                ExecutionLimits
	remaining             int64
	onStep                func()
	callDepth             uint32
	moduleInitializations uint32
	generatedStringBytes  uint64
	runtimeObjects        uint64
	inheritedScriptFrames []ScriptFrame
}

func (controller *executionController) pushInheritedScriptFrames(frames []ScriptFrame) func() {
	if controller == nil {
		return func() {}
	}
	previous := controller.inheritedScriptFrames
	controller.inheritedScriptFrames = append([]ScriptFrame(nil), frames...)
	return func() {
		controller.inheritedScriptFrames = previous
	}
}

func (controller *executionController) enterCall() error {
	if controller == nil {
		return nil
	}
	if controller.limits.MaxCallDepth != 0 && controller.callDepth >= controller.limits.MaxCallDepth {
		return &LimitError{Kind: LimitCallDepth, Limit: uint64(controller.limits.MaxCallDepth), Used: uint64(controller.callDepth) + 1}
	}
	controller.callDepth++
	return nil
}

func (controller *executionController) enterCalls(count uint32) error {
	if controller == nil || count == 0 {
		return nil
	}
	if controller.limits.MaxCallDepth != 0 && (controller.callDepth > controller.limits.MaxCallDepth || count > controller.limits.MaxCallDepth-controller.callDepth) {
		return &LimitError{Kind: LimitCallDepth, Limit: uint64(controller.limits.MaxCallDepth), Used: uint64(controller.callDepth) + uint64(count)}
	}
	controller.callDepth += count
	return nil
}

func (controller *executionController) leaveCall() {
	if controller != nil && controller.callDepth > 0 {
		controller.callDepth--
	}
}

func (controller *executionController) chargeModuleInitialization() error {
	if controller == nil {
		return nil
	}
	if controller.limits.MaxModuleInitializations != 0 && controller.moduleInitializations >= controller.limits.MaxModuleInitializations {
		return &LimitError{Kind: LimitModuleInitializations, Limit: uint64(controller.limits.MaxModuleInitializations), Used: uint64(controller.moduleInitializations) + 1}
	}
	controller.moduleInitializations++
	return nil
}

func (controller *executionController) chargeGeneratedStringBytes(count uint64) error {
	if controller == nil || count == 0 {
		return nil
	}
	used := controller.generatedStringBytes
	if count > ^uint64(0)-used {
		used = ^uint64(0)
	} else {
		used += count
	}
	if controller.limits.MaxGeneratedStringBytes != 0 && used > controller.limits.MaxGeneratedStringBytes {
		return &LimitError{Kind: LimitGeneratedStringBytes, Limit: controller.limits.MaxGeneratedStringBytes, Used: used}
	}
	controller.generatedStringBytes = used
	return nil
}

func (controller *executionController) chargeRuntimeObject() error {
	return controller.chargeRuntimeObjects(1)
}

func (controller *executionController) chargeRuntimeObjects(count uint64) error {
	if controller == nil {
		return nil
	}
	if count == 0 {
		return nil
	}
	used := controller.runtimeObjects
	if count > ^uint64(0)-used {
		used = ^uint64(0)
	} else {
		used += count
	}
	if controller.limits.MaxRuntimeObjects != 0 && used > controller.limits.MaxRuntimeObjects {
		return &LimitError{Kind: LimitRuntimeObjects, Limit: controller.limits.MaxRuntimeObjects, Used: used}
	}
	controller.runtimeObjects = used
	return nil
}

func (controller *executionController) releaseRuntimeObjects(count uint64) {
	if controller != nil && count <= controller.runtimeObjects {
		controller.runtimeObjects -= count
	}
}

func newExecutionController(ctx context.Context, limits ExecutionLimits) (*executionController, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized, err := normalizeExecutionLimits(0, limits)
	if err != nil {
		return nil, err
	}
	remaining, err := executionControllerRemaining(normalized)
	if err != nil {
		return nil, err
	}
	return &executionController{
		ctx:       ctx,
		limits:    normalized,
		remaining: remaining,
	}, nil
}

func validateExecutionLimits(limits ExecutionLimits) error {
	_, err := executionControllerRemaining(limits)
	return err
}

func executionControllerRemaining(limits ExecutionLimits) (int64, error) {
	if limits.MaxInstructions == 0 {
		return -1, nil
	}
	if limits.MaxInstructions > maxInt64Uint {
		return 0, fmt.Errorf("execution controller: max instructions %d exceeds int64", limits.MaxInstructions)
	}
	return int64(limits.MaxInstructions), nil
}

func (controller *executionController) checkContext() error {
	if controller == nil || controller.ctx == nil {
		return nil
	}
	return controller.ctx.Err()
}

func (controller *executionController) chargeInstructions(count uint64) error {
	if controller == nil || controller.remaining < 0 || count == 0 {
		return nil
	}
	if count <= uint64(controller.remaining) {
		controller.remaining -= int64(count)
		return nil
	}
	used := controller.limits.MaxInstructions - uint64(controller.remaining)
	if count > ^uint64(0)-used {
		used = ^uint64(0)
	} else {
		used += count
	}
	controller.remaining = 0
	return &LimitError{
		Kind:  LimitInstructions,
		Limit: controller.limits.MaxInstructions,
		Used:  used,
	}
}

func (controller *executionController) configuredLimits() ExecutionLimits {
	if controller == nil {
		return ExecutionLimits{}
	}
	return controller.limits
}
