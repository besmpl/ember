package ember

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestB5RetainedEnginesShareLimits(t *testing.T) {
	compactProto := compileB5Proto(t, `
local function add(left, right)
	return left + right
end
return add(add(1, 2), add(3, 4))
`)
	if compactProto.compact == nil {
		t.Fatal("compact fixture did not build a compact program")
	}
	numericProto := compileB5Proto(t, `return 1 + 2`)
	if !numericProto.slotExecutionNumeric {
		t.Fatal("numeric fixture did not build a numeric slot program")
	}
	slotProto := compileB5Proto(t, `
local left = "alpha"
local right = "beta"
if left < right then
	return 7
end
return 9
`)
	if !slotProto.slotExecutionEligible {
		t.Fatal("slot fixture is not slot eligible")
	}

	tests := []struct {
		name string
		run  func(*executionController) ([]Value, bool, error)
	}{
		{
			name: "slot",
			run: func(controller *executionController) ([]Value, bool, error) {
				return runSlotExecutionWithController(slotProto, nil, controller)
			},
		},
		{
			name: "numeric-slot",
			run: func(controller *executionController) ([]Value, bool, error) {
				return runSlotExecutionWithController(numericProto, nil, controller)
			},
		},
		{
			name: "compact-call",
			run: func(controller *executionController) ([]Value, bool, error) {
				return runSlotExecutionWithController(compactProto, nil, controller)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name+"-success", func(t *testing.T) {
			controller := newB5Controller(t, context.Background(), 128)
			values, handled, err := test.run(controller)
			if err != nil || !handled || len(values) != 1 {
				t.Fatalf("engine result = (%#v, %t, %v), want handled success", values, handled, err)
			}
		})
		t.Run(test.name+"-limit", func(t *testing.T) {
			controller := newB5Controller(t, context.Background(), 1)
			values, handled, err := test.run(controller)
			if values != nil || !handled || !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("engine result = (%#v, %t, %v), want limit error", values, handled, err)
			}
		})
		t.Run(test.name+"-cancelled", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			controller := newB5Controller(t, ctx, 0)
			values, handled, err := test.run(controller)
			if values != nil || !handled || !errors.Is(err, context.Canceled) {
				t.Fatalf("engine result = (%#v, %t, %v), want cancellation", values, handled, err)
			}
		})
	}
}

func TestB5ProductionLoopHonorsSafetyWithoutSlowPathSelection(t *testing.T) {
	proto := compileB5Proto(t, `
local total = 0
for i = 1, 100 do
	total = total + i
end
return total
`)
	controller := newB5Controller(t, context.Background(), 64)
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	values, err := thread.run(proto, nil, nil)
	if values != nil || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("production loop result = (%#v, %v), want limit error", values, err)
	}
	if thread.directFrameInstrumented {
		t.Fatal("safety-enabled execution selected the instrumented slow loop")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	thread = newVMThreadWithContext(ctx, runtimeGlobals(nil))
	thread.controller = newB5Controller(t, ctx, 0)
	values, err = thread.run(proto, nil, nil)
	if values != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled production loop = (%#v, %v), want cancellation", values, err)
	}
}

func TestB5InstrumentedDirectLoopHonorsSafety(t *testing.T) {
	proto := compileB5Proto(t, `
local total = 0
while true do
	total = total + 1
end
`)
	controller := newB5Controller(t, context.Background(), 32)
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	thread.directFrameInstrumented = true
	values, err := thread.run(proto, nil, nil)
	if values != nil || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("instrumented loop result = (%#v, %v), want limit error", values, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	thread = newVMThreadWithContext(ctx, runtimeGlobals(nil))
	thread.controller = newB5Controller(t, ctx, 0)
	thread.directFrameInstrumented = true
	values, err = thread.run(proto, nil, nil)
	if values != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled instrumented loop = (%#v, %v), want cancellation", values, err)
	}
}

func TestB5DirectLoopKernelHonorsSafety(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 0, false)
	frame := newVMFrame(proto, nil, nil)
	kernel := &directLoopKernel{
		entryPC:  0,
		wordBase: 0,
		ops:      []directLoopKernelOp{{op: opJump, b: 0, originalPC: 0, nextOriginalPC: 1}},
		opAtWord: []int32{0},
	}
	controller := newB5Controller(t, context.Background(), 16)
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	window := newExecutionWindow(controller)
	exit := runDirectLoopKernel(&thread, frame, kernel, &window)
	window.commit()
	_, _, err := exit.frameResult()
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("kernel exit error = %v, want limit error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	controller = newB5Controller(t, ctx, 0)
	thread = newVMThreadWithContext(ctx, runtimeGlobals(nil))
	thread.controller = controller
	window = newExecutionWindow(controller)
	exit = runDirectLoopKernel(&thread, frame, kernel, &window)
	window.commit()
	_, _, err = exit.frameResult()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled kernel error = %v, want context cancellation", err)
	}
}

func TestB5CancellationPollBound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	controller := newB5Controller(t, ctx, 0)
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
			if count > 512 {
				t.Fatalf("cancellation observed after %d instructions, want <=512", count)
			}
			return
		}
	}
	t.Fatal("window did not observe cancellation within 512 instructions")
}

func TestB5NestedCompactCallsConsumeAggregateBudget(t *testing.T) {
	proto := compileB5Proto(t, `
local function add(left, right) return left + right end
return add(add(1, 2), add(3, 4))
`)
	if proto.compact == nil {
		t.Fatal("nested compact fixture did not build a compact program")
	}
	controller := newB5Controller(t, context.Background(), 5)
	values, handled, err := runSlotExecutionWithController(proto, nil, controller)
	if values != nil || !handled || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("nested compact result = (%#v, %t, %v), want aggregate limit error", values, handled, err)
	}
}

func TestB5DirectMetamethodCallConsumesAggregateBudget(t *testing.T) {
	proto := compileB5Proto(t, `
local value = setmetatable({amount = 1}, {
	__add = function(left, right)
		return left.amount + right
	end,
})
return value + 2
`)
	const budget = int64(128)
	controller := newB5Controller(t, context.Background(), uint64(budget))
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

	limited := newB5Controller(t, context.Background(), uint64(consumed-1))
	thread = newVMThread(runtimeGlobals(nil))
	thread.controller = limited
	values, err = thread.run(proto, nil, nil)
	if values != nil || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("limited metamethod result = (%#v, %v), want aggregate limit error", values, err)
	}
}

func TestB5SlotFallbackRollsBackSpeculativeBudget(t *testing.T) {
	child := newProto(nil, []instruction{
		{op: opAdd, a: 1, b: 0, c: 0},
		{op: opReturnOne, a: 1},
	}, nil, nil, 2, 1, false)
	proto := newProto(nil, []instruction{
		{op: opClosure, a: 1, b: 0},
		{op: opMove, a: 2, b: 0},
		{op: opCallLocalOne, a: 2, b: 1, c: 2, d: 1},
		{op: opReturnOne, a: 2},
	}, []*Proto{child}, nil, 3, 1, false)
	controller := newB5Controller(t, context.Background(), 32)
	values, handled, err := runSlotExecutionWithController(proto, []Value{StringValue("wrong")}, controller)
	if handled || values != nil || err != nil {
		t.Fatalf("speculative fallback = (%#v, %t, %v), want clean fallback", values, handled, err)
	}
	if controller.remaining != 32 {
		t.Fatalf("fallback consumed %d instructions, want rollback", 32-controller.remaining)
	}
}

func TestB5PostExecutionBoxFallbackRollsBackBudget(t *testing.T) {
	proto := newProto([]Value{NumberValue(math.Float64frombits(0x7ff8_0000_0000_0042))}, []instruction{
		{op: opLoadConst, a: 0, b: 0},
		{op: opReturnOne, a: 0},
	}, nil, nil, 1, 0, false)
	if !proto.slotExecutionNumeric {
		t.Fatal("NaN fixture did not reach numeric slot tier")
	}
	controller := newB5Controller(t, context.Background(), 16)
	state := acquireSlotExecutionState(0, 0)
	defer releaseSlotExecutionState(state)
	values, handled, err := runNumericSlotExecutionWithController(proto, nil, state, controller)
	if values != nil || handled || err != nil {
		t.Fatalf("post-export fallback = (%#v, %t, %v), want clean fallback", values, handled, err)
	}
	if controller.remaining != 16 {
		t.Fatalf("post-export fallback consumed %d instructions, want rollback", 16-controller.remaining)
	}
}

func TestB5CoroutineResumeUsesFreshInvocationController(t *testing.T) {
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
	body := compileB5Proto(t, `
check()
coroutine.yield()
check()
while true do end
`)
	coroutine := newVMCoroutine(globals, &closure{proto: body})
	ctxA := context.WithValue(context.Background(), key, "A")
	controllerA := newB5Controller(t, ctxA, 64)
	parent := newVMThreadWithContext(ctxA, globals)
	parent.controller = controllerA
	previousThread := globals.thread
	globals.thread = &parent
	defer func() { globals.thread = previousThread }()
	first, err := baseCoroutineResume(globals, []Value{UserDataValue(coroutine.userdata)})
	if err != nil || len(first) < 1 {
		t.Fatalf("first resume = (%#v, %v), want yielded result", first, err)
	}
	if ok, _ := first[0].Bool(); !ok {
		t.Fatalf("first resume ok = %v, want true", first[0])
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
	controllerB := newB5Controller(t, ctxB, 16)
	parent.ctx = ctxB
	parent.controller = controllerB
	second, err := baseCoroutineResume(globals, []Value{UserDataValue(coroutine.userdata)})
	if err != nil || len(second) < 2 {
		t.Fatalf("second resume = (%#v, %v), want limit result", second, err)
	}
	if ok, _ := second[0].Bool(); ok {
		t.Fatalf("second resume ok = %v, want false", second[0])
	}
	message, _ := second[1].String()
	if !strings.Contains(message, "instructions limit") {
		t.Fatalf("second resume message = %q, want instruction limit", message)
	}
	if controllerA.remaining != remainingA {
		t.Fatalf("invocation A budget changed from %d to %d", remainingA, controllerA.remaining)
	}
	if controllerB.remaining != 0 {
		t.Fatalf("invocation B remaining budget = %d, want exhaustion", controllerB.remaining)
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

func compileB5Proto(t *testing.T, source string) *Proto {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	return proto
}

func newB5Controller(t *testing.T, ctx context.Context, max uint64) *executionController {
	t.Helper()
	controller, err := newExecutionController(ctx, ExecutionLimits{MaxInstructions: max})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	return controller
}
