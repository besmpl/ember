package ember

import (
	"errors"
	"testing"
)

func TestTransientScriptCallableValueIsFunctionWithoutLegacyConfusion(t *testing.T) {
	handle := scriptCallableHandle{owner: 11, index: 7, generation: 3}
	value, err := transientScriptCallableValue(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Kind(); got != FunctionKind {
		t.Fatalf("Kind = %s, want function", got)
	}
	if closure, ok := value.scriptFunction(); ok || closure != nil {
		t.Fatalf("legacy scriptFunction decoded transient handle as %#v", closure)
	}
}

func TestDecodeTransientScriptCallableValueValidatesOwnerAndGeneration(t *testing.T) {
	want := scriptCallableHandle{owner: 11, index: 7, generation: 3}
	value, err := transientScriptCallableValue(want)
	if err != nil {
		t.Fatal(err)
	}
	validated := false
	got, err := decodeTransientScriptCallableValue(value, want.owner, func(handle scriptCallableHandle) bool {
		validated = true
		return handle.index == want.index && handle.generation == want.generation
	})
	if err != nil {
		t.Fatal(err)
	}
	if !validated {
		t.Fatal("decode did not validate handle liveness")
	}
	if got != want {
		t.Fatalf("decoded handle = %#v, want %#v", got, want)
	}
}

func TestDecodeTransientScriptCallableValueRejectsCrossOwnerAndStaleHandles(t *testing.T) {
	handle := scriptCallableHandle{owner: 11, index: 7, generation: 3}
	value, err := transientScriptCallableValue(handle)
	if err != nil {
		t.Fatal(err)
	}

	validatorCalled := false
	if _, err := decodeTransientScriptCallableValue(value, 12, func(scriptCallableHandle) bool {
		validatorCalled = true
		return true
	}); !errors.Is(err, errScriptCallableValueCrossOwner) {
		t.Fatalf("cross-owner decode error = %v", err)
	}
	if validatorCalled {
		t.Fatal("cross-owner value reached the generation validator")
	}

	if _, err := decodeTransientScriptCallableValue(value, handle.owner, func(candidate scriptCallableHandle) bool {
		return candidate.index == handle.index && candidate.generation == handle.generation+1
	}); !errors.Is(err, errScriptCallableValueStale) {
		t.Fatalf("stale decode error = %v", err)
	}
	if _, err := decodeTransientScriptCallableValue(value, handle.owner, nil); !errors.Is(err, errScriptCallableValueStale) {
		t.Fatalf("unvalidated decode error = %v", err)
	}
}

func TestTransientScriptCallableValueRejectsMalformedHandles(t *testing.T) {
	for _, handle := range []scriptCallableHandle{
		{index: 1, generation: 1},
		{owner: 1, generation: 1},
		{owner: 1, index: 1},
	} {
		if _, err := transientScriptCallableValue(handle); !errors.Is(err, errScriptCallableValueInvalid) {
			t.Fatalf("handle %#v error = %v", handle, err)
		}
	}
}

func TestLegacyScriptFunctionValueRemainsUnchanged(t *testing.T) {
	want := &closure{proto: &Proto{}}
	value := closureFunctionValue(want)
	if got := value.Kind(); got != FunctionKind {
		t.Fatalf("Kind = %s, want function", got)
	}
	got, ok := value.scriptFunction()
	if !ok || got != want {
		t.Fatalf("legacy scriptFunction = %#v, %t; want original closure", got, ok)
	}
	validatorCalled := false
	if _, err := decodeTransientScriptCallableValue(value, 1, func(scriptCallableHandle) bool {
		validatorCalled = true
		return true
	}); !errors.Is(err, errScriptCallableValueInvalid) {
		t.Fatalf("transient decode of legacy closure error = %v", err)
	}
	if validatorCalled {
		t.Fatal("legacy closure reached transient handle validator")
	}
}
