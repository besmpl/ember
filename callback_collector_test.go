package ember

import (
	"context"
	"strings"
	"testing"
)

func TestCapturedCallbackRootsEscapedValuesUntilClose(t *testing.T) {
	owner := newRuntimeOwner()
	runtime := &Runtime{owner: owner, program: &Program{}}
	closure := &closure{proto: &Proto{}}
	globalTable := NewTable()
	call := runtimeCallContext{
		runtime: runtime,
		ctx:     context.Background(),
		globals: map[string]Value{"state": TableValue(globalTable)},
	}
	callback, err := CaptureCallback(contextWithRuntimeCallContext(context.Background(), call), closureFunctionValue(closure))
	if err != nil {
		t.Fatalf("capture callback: %v", err)
	}
	if len(owner.roots) != 2 {
		t.Fatalf("callback roots = %d, want closure and captured global", len(owner.roots))
	}
	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("collect captured callback: %v", err)
	}
	closureHandle, found, _ := owner.heap.lookupExistingValue(closureFunctionValue(closure))
	if !found {
		t.Fatal("captured callback closure was not imported")
	}
	tableHandle, found, _ := owner.heap.lookupExistingValue(TableValue(globalTable))
	if !found {
		t.Fatal("captured callback global was not imported")
	}

	copyOfCallback := callback
	if err := copyOfCallback.Close(); err != nil {
		t.Fatalf("close callback copy: %v", err)
	}
	if err := callback.Close(); err != nil {
		t.Fatalf("idempotent callback close: %v", err)
	}
	if len(owner.roots) != 0 {
		t.Fatalf("callback close left %d roots", len(owner.roots))
	}
	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("collect released callback: %v", err)
	}
	if err := owner.heap.validateSlot(closureHandle); err == nil {
		t.Fatal("released callback closure survived collection")
	}
	if err := owner.heap.validateSlot(tableHandle); err == nil {
		t.Fatal("released callback global survived collection")
	}
	if _, err := callback.Call(context.Background()); err == nil || !strings.Contains(err.Error(), "released") {
		t.Fatalf("call released callback = %v, want released error", err)
	}
}
