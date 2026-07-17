package ember

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// PrepareExactGuestBatchForParityTest binds one checked-in generated guest
// batch by verified Program hash through Program, PreparedBundle, and Runtime.
// It is exported only in test builds so the frozen external parity harness can
// use the private generated registry without widening Ember's production API.
func PrepareExactGuestBatchForParityTest(source string) (Callback, error) {
	runtime, _, err := backendExactGuestBatchPreparedRuntime(source)
	if err != nil {
		return Callback{}, err
	}
	module := LogicalModule("exact-guest-batch")
	return Callback{target: &backendExactGuestBatchCallbackTarget{
		runtime: runtime,
		module:  module,
	}}, nil
}

func backendExactGuestBatchPreparedRuntime(source string) (*Runtime, backendExactGuestBatchPreparedArtifact, error) {
	program, err := backendExactGuestBatchProgram(source)
	if err != nil {
		return nil, backendExactGuestBatchPreparedArtifact{}, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	image, err := program.preparedProgramImage()
	if err != nil {
		return nil, backendExactGuestBatchPreparedArtifact{}, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	artifact, err := backendExactGuestBatchPreparedArtifactForImage(image)
	if err != nil {
		return nil, backendExactGuestBatchPreparedArtifact{}, err
	}
	bundle, err := backendExactGuestBatchPreparedBundle(program, artifact)
	if err != nil {
		return nil, backendExactGuestBatchPreparedArtifact{}, err
	}
	runtime, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		return nil, backendExactGuestBatchPreparedArtifact{}, err
	}
	return runtime, artifact, nil
}

// PrepareExactGuestBatchThroughputForParityTest binds the same verified bundle
// to one owner entry. The parity observer uses this test-only seam so
// guest_batch_v1 measures N guest calls without Runtime.Invoke lifecycle work.
func PrepareExactGuestBatchThroughputForParityTest(source string) (Callback, error) {
	runtime, artifact, err := backendExactGuestBatchPreparedRuntime(source)
	if err != nil {
		return Callback{}, err
	}
	execution, ok := runtime.execution.(*machineRuntimeExecution)
	if !ok || execution.owner == nil || execution.prepared == nil {
		_ = runtime.Close()
		return Callback{}, fmt.Errorf("prepare exact guest batch throughput: prepared Machine unavailable")
	}
	owner := execution.owner
	closure, err := backendExactGuestBatchClosure(owner, artifact.caseProto, artifact.entryProto)
	if err != nil {
		_ = runtime.Close()
		return Callback{}, err
	}
	return Callback{target: &backendExactGuestBatchThroughputTarget{
		runtime: runtime, owner: owner, closure: closure, entryProto: artifact.entryProto,
	}}, nil
}

func backendExactGuestBatchProgram(source string) (*Program, error) {
	module := LogicalModule("exact-guest-batch")
	program, _, err := LoadProgram(context.Background(), backendExactGuestBatchLoader{
		module.String(): {Name: module.String(), Text: source},
	}, ProgramOptions{
		Entrypoints: []Entrypoint{{Name: "main", Module: module}},
		Parallelism: 1,
	})
	return program, err
}

type backendExactGuestBatchLoader map[string]Source

func (loader backendExactGuestBatchLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	source, ok := loader[id.String()]
	if !ok {
		return Source{}, fmt.Errorf("missing module %s", id)
	}
	return source, nil
}

type backendExactGuestBatchCallbackTarget struct {
	mu       sync.Mutex
	runtime  *Runtime
	module   ModuleID
	released bool
}

type backendExactGuestBatchThroughputTarget struct {
	mu         sync.Mutex
	runtime    *Runtime
	owner      *machineOwner
	closure    machineClosureHandle
	entryProto int32
	released   bool
}

func (target *backendExactGuestBatchThroughputTarget) call(
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

func (target *backendExactGuestBatchThroughputTarget) callResumable(
	ctx context.Context,
	args []Value,
) (resumableOutcome, error) {
	values, err := target.call(ctx, args)
	if err != nil {
		return resumableOutcome{}, err
	}
	return resumableOutcome{values: values}, nil
}

func (target *backendExactGuestBatchThroughputTarget) close() error {
	if target == nil || target.runtime == nil {
		return nil
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.released {
		return nil
	}
	if err := target.runtime.Close(); err != nil {
		return err
	}
	target.released = true
	return nil
}

func (target *backendExactGuestBatchCallbackTarget) call(
	ctx context.Context,
	args []Value,
) ([]Value, error) {
	if target == nil || target.runtime == nil {
		return nil, fmt.Errorf("callback: not retained")
	}
	target.mu.Lock()
	released := target.released
	target.mu.Unlock()
	if released {
		return nil, fmt.Errorf("callback: released")
	}
	values, err := target.runtime.Invoke(ctx, Invocation{Module: target.module}, args...)
	if err != nil {
		return nil, fmt.Errorf("callback: %w", err)
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
	if target == nil || target.runtime == nil {
		return nil
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.released {
		return nil
	}
	if err := target.runtime.Close(); err != nil {
		return err
	}
	target.released = true
	return nil
}

func backendExactGuestBatchPreparedBundle(
	program *Program,
	artifact backendExactGuestBatchPreparedArtifact,
) (*PreparedBundle, error) {
	image, err := program.preparedProgramImage()
	if err != nil {
		return nil, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		return nil, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	functions := make([][]PreparedFunction, len(ir.modules))
	for index := range ir.modules {
		functions[index] = make([]PreparedFunction, len(ir.modules[index].protos))
	}
	functions[0][artifact.entryProto] = PreparedFunction(artifact.function)
	return NewPreparedBundle(ir.abiVersion, ir.semanticVersion, ir.programHash, functions), nil
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
