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
