package ember

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// PrepareExactGuestBatchForParityTest binds one checked-in generated guest
// batch by verified Program hash. It is exported only in test builds so the
// frozen external parity harness can measure the private prepared seam without
// widening Ember's production API before the integration contract is proved.
func PrepareExactGuestBatchForParityTest(source string) (Callback, error) {
	image, err := backendExactGuestBatchProgramImage(source)
	if err != nil {
		return Callback{}, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	artifact, err := backendExactGuestBatchPreparedArtifactForImage(image)
	if err != nil {
		return Callback{}, err
	}
	program, err := backendExactGuestBatchPreparedProgram(image, artifact)
	if err != nil {
		return Callback{}, err
	}
	owner, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		return Callback{}, err
	}
	closure, err := backendExactGuestBatchClosure(owner, artifact.caseProto, artifact.entryProto)
	if err != nil {
		_ = owner.close()
		return Callback{}, err
	}
	return Callback{target: &backendExactGuestBatchCallbackTarget{
		owner: owner, closure: closure, entryProto: artifact.entryProto,
	}}, nil
}

type backendExactGuestBatchCallbackTarget struct {
	mu         sync.Mutex
	owner      *machineOwner
	closure    machineClosureHandle
	entryProto int32
	released   bool
}

func (target *backendExactGuestBatchCallbackTarget) call(
	ctx context.Context,
	args []Value,
) ([]Value, error) {
	if target == nil || target.owner == nil {
		return nil, fmt.Errorf("callback: not retained")
	}
	lease, err := target.owner.beginRun()
	if errors.Is(err, errMachineOwnerClosed) {
		return nil, fmt.Errorf("callback: runtime is closed")
	}
	if errors.Is(err, errMachineOwnerBusy) {
		return nil, fmt.Errorf("callback: begin call: %w", ErrRuntimeBusy)
	}
	if err != nil {
		return nil, fmt.Errorf("callback: begin call: %w", err)
	}
	defer lease.end()
	target.mu.Lock()
	released := target.released
	target.mu.Unlock()
	if released {
		return nil, fmt.Errorf("callback: released")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tableCheckpoint := target.owner.tables.checkpointStopped()
	defer target.owner.recycleTransientTablesStopped(tableCheckpoint)
	machineArgs, err := importMachineValuesStopped(target.owner, args)
	if err != nil {
		return nil, fmt.Errorf("callback: import arguments: %w", err)
	}
	if err := target.owner.executeStopped(
		0,
		target.entryProto,
		target.closure,
		machineArgs,
		nil,
		machineRunEffects{ctx: ctx},
	); err != nil {
		return nil, fmt.Errorf("callback: call script function: %w", err)
	}
	values, err := target.owner.exportResults()
	if err != nil {
		return nil, fmt.Errorf("callback: export results: %w", err)
	}
	return values, nil
}

func (target *backendExactGuestBatchCallbackTarget) callResumable(
	ctx context.Context,
	args []Value,
) (resumableOutcome, error) {
	values, err := target.call(ctx, args)
	if err != nil {
		return resumableOutcome{}, err
	}
	return resumableOutcome{values: values}, nil
}

func (target *backendExactGuestBatchCallbackTarget) close() error {
	if target == nil || target.owner == nil {
		return nil
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.released {
		return nil
	}
	if err := target.owner.close(); err != nil {
		return err
	}
	target.released = true
	return nil
}

func backendExactGuestBatchPreparedProgram(
	image *programImage,
	artifact backendExactGuestBatchPreparedArtifact,
) (*machinePreparedProgram, error) {
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		return nil, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	modules := make([]machinePreparedModule, len(ir.modules))
	for index := range ir.modules {
		modules[index] = machinePreparedModule{
			moduleID:  programModuleID(index),
			functions: make([]machinePreparedFunction, len(ir.modules[index].protos)),
		}
	}
	modules[0].functions[artifact.entryProto] = artifact.function
	return &machinePreparedProgram{
		abiVersion:      ir.abiVersion,
		semanticVersion: ir.semanticVersion,
		programHash:     ir.programHash,
		modules:         modules,
	}, nil
}

func backendExactGuestBatchClosure(
	owner *machineOwner,
	caseProto int32,
	entryProto int32,
) (machineClosureHandle, error) {
	caseClosure, err := owner.closures.createClosureStopped(0, machineProtoID(caseProto), nil)
	if err != nil {
		return machineClosureHandle{}, err
	}
	caseValue, err := slotPackHandle(slotTagClosure, caseClosure.index, caseClosure.generation)
	if err != nil {
		return machineClosureHandle{}, err
	}
	return owner.closures.createClosureStopped(0, machineProtoID(entryProto), []machineCaptureDescriptor{{
		mode: machineCaptureByValue, value: caseValue,
	}})
}
