package ember

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPureLoopObservesContextCancellation(t *testing.T) {
	proto := compileB4TestProto(t, `while true do end`)
	ctx, cancel := context.WithCancel(context.Background())
	controller, err := newExecutionController(ctx, ExecutionLimits{})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	started := make(chan struct{})
	controller.onStep = func() { close(started) }
	done := make(chan error, 1)
	go func() {
		_, err := executeProto(ctx, proto, nil, executeOptions{controller: controller})
		done <- err
	}()
	waitForB4Execution(t, started)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pure loop error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pure loop did not observe cancellation")
	}
}

func TestPureLoopObservesDeadlineExceeded(t *testing.T) {
	proto := compileB4TestProto(t, `while true do end`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	controller, err := newExecutionController(ctx, ExecutionLimits{})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	started := make(chan struct{})
	controller.onStep = func() { close(started) }
	done := make(chan error, 1)
	go func() {
		_, err := executeProto(ctx, proto, nil, executeOptions{controller: controller})
		done <- err
	}()
	waitForB4Execution(t, started)
	<-ctx.Done()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("pure loop error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pure loop did not observe deadline")
	}
}

func TestInstructionLimitStopsPureLoop(t *testing.T) {
	proto := compileB4TestProto(t, `while true do end`)
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 64})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	_, err = executeProto(context.Background(), proto, nil, executeOptions{controller: controller})
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("pure loop error = %v, want ErrLimitExceeded", err)
	}
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("pure loop error = %v, want *LimitError", err)
	}
	if limitErr.Kind != LimitInstructions || limitErr.Limit != 64 {
		t.Fatalf("limit error = %#v, want instructions limit 64", limitErr)
	}
	if isVMHostInterrupt(err) {
		t.Fatalf("pure loop error = %v, must not be vmHostInterrupt", err)
	}
}

func TestInstructionLimitDoesNotChargeAuxWordsTwice(t *testing.T) {
	constants := make([]Value, 65_537)
	for index := range constants {
		constants[index] = NumberValue(float64(index))
	}
	proto := newProto(
		constants,
		[]instruction{{op: opLoadConst, a: 0, b: 65_536}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	if len(proto.words) != 3 {
		t.Fatalf("AUX fixture has %d words, want 3", len(proto.words))
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 2})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	values, err := executeProto(context.Background(), proto, nil, executeOptions{controller: controller})
	if err != nil {
		t.Fatalf("AUX fixture returned error: %v", err)
	}
	if len(values) != 1 || values[0] != NumberValue(65_536) {
		t.Fatalf("AUX fixture values = %#v, want [65536]", values)
	}
}

func TestContextHostFunctionReceivesInvocationContext(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	want := "invocation-value"
	observed := make(chan any, 1)
	runtime := newRuntimeOwnerIntegrationRuntimeSource(t, `
return { startup = function() checkContext() end }
`, RuntimeHostFunc(func(_ context.Context, _ HostCall) (map[string]Value, error) {
		return map[string]Value{
			"checkContext": ContextHostFuncValue(func(ctx context.Context, _ []Value) ([]Value, error) {
				observed <- ctx.Value(key)
				return nil, nil
			}),
		}, nil
	}))
	defer runtime.Close()
	ctx := context.WithValue(context.Background(), key, want)
	if _, err := runtime.RunHook(ctx, "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	select {
	case got := <-observed:
		if got != want {
			t.Fatalf("host context value = %#v, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host function did not observe invocation context")
	}
}

func compileB4TestProto(t *testing.T, source string) *Proto {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	return proto
}

func waitForB4Execution(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("script execution did not start")
	}
}
