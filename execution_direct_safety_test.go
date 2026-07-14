package ember

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDirectLoopsHonorInstructionLimits(t *testing.T) {
	proto, err := Compile(`local total = 0 for i = 1, 100 do total = total + i end return total`)
	if err != nil {
		t.Fatal(err)
	}
	for _, instrumented := range []bool{false, true} {
		controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 64})
		if err != nil {
			t.Fatal(err)
		}
		thread := newVMThread(runtimeGlobals(nil))
		thread.controller = controller
		thread.directFrameInstrumented = instrumented
		values, runErr := thread.run(proto, nil, nil)
		if values != nil || !errors.Is(runErr, ErrLimitExceeded) {
			t.Fatalf("instrumented=%t result=(%#v, %v), want instruction limit", instrumented, values, runErr)
		}
	}
}

func TestDirectCancellationPollBound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	controller, err := newExecutionController(ctx, ExecutionLimits{})
	if err != nil {
		t.Fatal(err)
	}
	window := newExecutionWindow(controller)
	if err := window.stepInstruction(); err != nil {
		t.Fatalf("initial window step = %v", err)
	}
	cancel()
	for count := 1; count <= 512; count++ {
		if err := window.stepInstruction(); err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("poll error = %v, want context cancellation", err)
			}
			return
		}
	}
	t.Fatal("window did not observe cancellation within 512 instructions")
}

func TestDirectMetamethodCallConsumesAggregateBudget(t *testing.T) {
	proto, err := Compile(`
local value = setmetatable({amount = 1}, {__add = function(left, right) return left.amount + right end})
return value + 2
`)
	if err != nil {
		t.Fatal(err)
	}
	const budget = int64(128)
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: uint64(budget)})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	values, err := thread.run(proto, nil, nil)
	if err != nil || len(values) != 1 {
		t.Fatalf("metamethod result = (%#v, %v), want success", values, err)
	}
	consumed := budget - controller.remaining
	if consumed < 2 {
		t.Fatalf("metamethod consumed %d instructions, want nested call charge", consumed)
	}
	limited, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: uint64(consumed - 1)})
	if err != nil {
		t.Fatal(err)
	}
	thread = newVMThread(runtimeGlobals(nil))
	thread.controller = limited
	values, err = thread.run(proto, nil, nil)
	if values != nil || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("limited metamethod result = (%#v, %v), want aggregate limit error", values, err)
	}
}

func TestDirectCoroutineResumeUsesFreshInvocationController(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	observed := make(chan string, 2)
	globals := runtimeGlobals(map[string]Value{
		"check": ContextHostFuncValue(func(ctx context.Context, _ []Value) ([]Value, error) {
			value, _ := ctx.Value(key).(string)
			observed <- value
			return nil, nil
		}),
	})
	body, err := Compile(`
check()
coroutine.yield()
check()
while true do end
`)
	if err != nil {
		t.Fatal(err)
	}
	coroutine := newVMCoroutine(globals, &closure{proto: body})
	ctxA := context.WithValue(context.Background(), key, "A")
	controllerA, err := newExecutionController(ctxA, ExecutionLimits{MaxInstructions: 64})
	if err != nil {
		t.Fatal(err)
	}
	parent := newVMThreadWithContext(ctxA, globals)
	parent.controller = controllerA
	previousThread := globals.thread
	globals.thread = &parent
	defer func() { globals.thread = previousThread }()
	first, err := baseCoroutineResume(globals, []Value{UserDataValue(coroutine.userdata)})
	if err != nil || len(first) < 1 {
		t.Fatalf("first resume = (%#v, %v)", first, err)
	}
	remainingA := controllerA.remaining
	if coroutine.thread.controller != nil || coroutine.suspended.controller != nil {
		t.Fatal("yielded coroutine retained invocation controller")
	}
	select {
	case got := <-observed:
		if got != "A" {
			t.Fatalf("first invocation context = %q, want A", got)
		}
	default:
		t.Fatal("first invocation did not call context-aware host function")
	}

	ctxB := context.WithValue(context.Background(), key, "B")
	controllerB, err := newExecutionController(ctxB, ExecutionLimits{MaxInstructions: 16})
	if err != nil {
		t.Fatal(err)
	}
	parent.ctx = ctxB
	parent.controller = controllerB
	second, err := baseCoroutineResume(globals, []Value{UserDataValue(coroutine.userdata)})
	if err != nil || len(second) < 2 {
		t.Fatalf("second resume = (%#v, %v)", second, err)
	}
	ok, _ := second[0].Bool()
	if ok {
		t.Fatalf("second resume = %#v, want limit failure", second)
	}
	message, _ := second[1].String()
	if !strings.Contains(message, "instructions limit") {
		t.Fatalf("second resume message = %q, want instruction limit", message)
	}
	if controllerA.remaining != remainingA || controllerB.remaining != 0 {
		t.Fatalf("budgets after resume A=%d (want %d), B=%d (want 0)", controllerA.remaining, remainingA, controllerB.remaining)
	}
	select {
	case got := <-observed:
		if got != "B" {
			t.Fatalf("second invocation context = %q, want B", got)
		}
	default:
		t.Fatal("second invocation did not call context-aware host function")
	}
	if coroutine.thread.controller != nil {
		t.Fatal("yielded coroutine retained invocation controller after limit")
	}
}
