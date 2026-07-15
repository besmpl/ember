package ember

import (
	"context"
	"errors"
	"testing"
)

func TestMachineOwnerHostCallExportsAndReimportsTransientClosure(t *testing.T) {
	image := machineOwnerProgramImage(t, []string{`local callback = function()
	return 42
end
local returned = host(callback)
return returned()`})
	owner, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "visible")
	host := ContextHostFuncValue(func(got context.Context, args []Value) ([]Value, error) {
		if got.Value(contextKey{}) != "visible" {
			return nil, errors.New("host did not receive run context")
		}
		if len(args) != 1 || args[0].Kind() != FunctionKind {
			return nil, errors.New("host did not receive transient closure")
		}
		if _, err := decodeTransientScriptCallableValue(args[0], owner.closures.owner, owner.validateScriptCallableHandle); err != nil {
			return nil, err
		}
		return args, nil
	})

	lease, err := owner.beginRun()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.end()
	if err := owner.importGlobalsStopped(map[string]Value{"host": host}); err != nil {
		t.Fatal(err)
	}
	if err := owner.executeRootStopped(0, nil, machineRunEffects{ctx: ctx}); err != nil {
		t.Fatal(err)
	}
	value := owner.results[0]
	number, err := owner.number(value)
	if err != nil || number != 42 {
		t.Fatalf("result = (%v, %v), want 42", number, err)
	}
}

func TestMachineOwnerGlobalImportReusesHostSidecar(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`return host()`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	for index := 0; index < 10; index++ {
		want := float64(index)
		fn := ContextHostFuncValue(func(context.Context, []Value) ([]Value, error) {
			return []Value{NumberValue(want)}, nil
		})
		if err := owner.importGlobalsStopped(map[string]Value{"host": fn}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(owner.hosts.values); got != 1 {
		t.Fatalf("host sidecar records = %d, want 1 reused record", got)
	}
}
