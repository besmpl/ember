package ember

import (
	"context"
	"errors"
	"testing"
)

func TestNormalizeExecutionLimits(t *testing.T) {
	tests := []struct {
		name    string
		legacy  uint64
		modern  uint64
		want    uint64
		wantErr bool
	}{
		{name: "both zero", want: 0},
		{name: "legacy only", legacy: 7, want: 7},
		{name: "modern only", modern: 11, want: 11},
		{name: "equal", legacy: 13, modern: 13, want: 13},
		{name: "conflict", legacy: 17, modern: 19, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeExecutionLimits(tt.legacy, ExecutionLimits{MaxInstructions: tt.modern})
			if tt.wantErr {
				if err == nil {
					t.Fatal("normalizeExecutionLimits succeeded, want conflict")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeExecutionLimits returned error: %v", err)
			}
			if got.MaxInstructions != tt.want {
				t.Fatalf("normalized instructions = %d, want %d", got.MaxInstructions, tt.want)
			}
		})
	}
}

func TestRuntimeOptionsNormalizeLimits(t *testing.T) {
	program := limitsTestProgram(t)
	tests := []struct {
		name    string
		legacy  uint64
		modern  uint64
		want    uint64
		wantErr bool
	}{
		{name: "legacy", legacy: 5, want: 5},
		{name: "modern", modern: 7, want: 7},
		{name: "equal", legacy: 9, modern: 9, want: 9},
		{name: "conflict", legacy: 11, modern: 13, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime, err := program.NewRuntime(RuntimeOptions{
				MaxInstructions: tt.legacy,
				Limits:          ExecutionLimits{MaxInstructions: tt.modern},
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("NewRuntime succeeded, want conflict")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewRuntime returned error: %v", err)
			}
			defer runtime.Close()
			if runtime.limits.MaxInstructions != tt.want {
				t.Fatalf("runtime limits = %#v, want max instructions %d", runtime.limits, tt.want)
			}
		})
	}
}

func TestExecutionControllerUnlimited(t *testing.T) {
	controller, err := newExecutionController(nil, ExecutionLimits{})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	if controller.remaining != -1 {
		t.Fatalf("unlimited remaining = %d, want -1", controller.remaining)
	}
	if err := controller.chargeInstructions(^uint64(0)); err != nil {
		t.Fatalf("unlimited charge returned error: %v", err)
	}
	if got := controller.configuredLimits(); got != (ExecutionLimits{}) {
		t.Fatalf("controller limits = %#v, want zero limits", got)
	}
}

func TestExecutionControllerExactInstructionExhaustion(t *testing.T) {
	controller, err := newExecutionController(nil, ExecutionLimits{MaxInstructions: 3})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := controller.chargeInstructions(1); err != nil {
			t.Fatalf("charge %d returned error: %v", i+1, err)
		}
	}
	err = controller.chargeInstructions(1)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("exhausting charge error = %v, want ErrLimitExceeded", err)
	}
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("exhausting charge error = %v, want LimitError", err)
	}
	if limitErr.Kind != LimitInstructions || limitErr.Limit != 3 || limitErr.Used != 4 {
		t.Fatalf("limit error = %#v, want instructions/3/4", limitErr)
	}
}

func TestExecutionControllerContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	controller, err := newExecutionController(ctx, ExecutionLimits{})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	if got := controller.checkContext(); got != ctx.Err() {
		t.Fatalf("controller context error = %v, want %v", got, ctx.Err())
	}
}

func TestExecutionControllerRejectsInt64Overflow(t *testing.T) {
	_, err := newExecutionController(nil, ExecutionLimits{MaxInstructions: uint64(^uint64(0)>>1) + 1})
	if err == nil {
		t.Fatal("newExecutionController accepted value beyond int64")
	}
}

func TestNewExecutionPolicyElidesUnrestrictedBackground(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background()} {
		controller, err := newExecutionPolicy(ctx, ExecutionLimits{})
		if err != nil {
			t.Fatalf("newExecutionPolicy(%T) returned error: %v", ctx, err)
		}
		if controller != nil {
			t.Fatalf("newExecutionPolicy(%T) = %#v, want nil", ctx, controller)
		}
	}
}

func TestNewExecutionPolicyRetainsRequiredController(t *testing.T) {
	cancelable, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadline, cancelDeadline := context.WithTimeout(context.Background(), 0)
	defer cancelDeadline()
	errorOnly := executionPolicyErrorContext{Context: context.Background(), err: context.Canceled}

	tests := []struct {
		name   string
		ctx    context.Context
		limits ExecutionLimits
	}{
		{name: "instruction limit", ctx: context.Background(), limits: ExecutionLimits{MaxInstructions: 1}},
		{name: "object limit", ctx: context.Background(), limits: ExecutionLimits{MaxRuntimeObjects: 1}},
		{name: "cancelable", ctx: cancelable},
		{name: "deadline", ctx: deadline},
		{name: "current error", ctx: errorOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, err := newExecutionPolicy(tt.ctx, tt.limits)
			if err != nil {
				t.Fatalf("newExecutionPolicy returned error: %v", err)
			}
			if controller == nil {
				t.Fatal("newExecutionPolicy returned nil controller")
			}
		})
	}
}

func TestExecutionControllerNilReceiverIsNoOp(t *testing.T) {
	var controller *executionController
	if restore := controller.pushInheritedScriptFrames([]ScriptFrame{{}}); restore == nil {
		t.Fatal("nil pushInheritedScriptFrames returned nil restore")
	} else {
		restore()
	}
	if err := controller.enterCall(); err != nil {
		t.Fatalf("nil enterCall = %v", err)
	}
	if err := controller.enterCalls(3); err != nil {
		t.Fatalf("nil enterCalls = %v", err)
	}
	controller.leaveCall()
	if err := controller.chargeModuleInitialization(); err != nil {
		t.Fatalf("nil chargeModuleInitialization = %v", err)
	}
	if err := controller.chargeGeneratedStringBytes(4); err != nil {
		t.Fatalf("nil chargeGeneratedStringBytes = %v", err)
	}
	if err := controller.chargeRuntimeObject(); err != nil {
		t.Fatalf("nil chargeRuntimeObject = %v", err)
	}
	if err := controller.chargeRuntimeObjects(2); err != nil {
		t.Fatalf("nil chargeRuntimeObjects = %v", err)
	}
	controller.releaseRuntimeObjects(2)
	if err := controller.checkContext(); err != nil {
		t.Fatalf("nil checkContext = %v", err)
	}
	if err := controller.chargeInstructions(4); err != nil {
		t.Fatalf("nil chargeInstructions = %v", err)
	}
	if got := controller.configuredLimits(); got != (ExecutionLimits{}) {
		t.Fatalf("nil configuredLimits = %#v, want zero limits", got)
	}
	var window *executionWindow
	if err := window.stepInstruction(); err != nil {
		t.Fatalf("nil window stepInstruction = %v", err)
	}
	if err := window.limitError(1); err != nil {
		t.Fatalf("nil window limitError = %v", err)
	}
	window.flush()
	window.commit()
	window.refresh()
}

type executionPolicyErrorContext struct {
	context.Context
	err error
}

func (ctx executionPolicyErrorContext) Done() <-chan struct{} { return nil }

func (ctx executionPolicyErrorContext) Err() error { return ctx.err }

func limitsTestProgram(t *testing.T) *Program {
	t.Helper()
	proto, err := Compile(`return { startup = function() end }`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	key := moduleKey{kind: moduleKeyLogical, path: "limits-test"}
	return &Program{
		entrypoints: []programEntrypoint{{name: "main", key: key}},
		protos:      map[moduleKey]*Proto{key: proto},
	}
}

func testExecutionController(t testing.TB, remaining int64) *executionController {
	t.Helper()
	if remaining < 0 {
		return nil
	}
	return &executionController{
		limits:    ExecutionLimits{MaxInstructions: uint64(remaining)},
		remaining: remaining,
	}
}
