package ember

import (
	"context"
	"fmt"
	"sync"
)

// PrepareExactGuestBatchForParityTest binds one checked-in generated guest
// batch by verified Program hash through Program, PreparedBundle, and Runtime.
// It is exported only in test builds so the frozen external parity harness can
// use the private generated registry without widening Ember's production API.
func PrepareExactGuestBatchForParityTest(source string) (Callback, error) {
	program, err := backendExactGuestBatchProgram(source)
	if err != nil {
		return Callback{}, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	image, err := program.preparedProgramImage()
	if err != nil {
		return Callback{}, fmt.Errorf("prepare exact guest batch: %w", err)
	}
	artifact, err := backendExactGuestBatchPreparedArtifactForImage(image)
	if err != nil {
		return Callback{}, err
	}
	bundle, err := backendExactGuestBatchPreparedBundle(program, artifact)
	if err != nil {
		return Callback{}, err
	}
	runtime, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		return Callback{}, err
	}
	module := LogicalModule("exact-guest-batch")
	return Callback{target: &backendExactGuestBatchCallbackTarget{
		runtime: runtime,
		module:  module,
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
