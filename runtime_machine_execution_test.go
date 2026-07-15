package ember

import (
	"context"
	"fmt"
	"testing"
)

type machineRuntimeTestLoader map[string]string

func (loader machineRuntimeTestLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	source, ok := loader[id.String()]
	if !ok {
		return Source{}, fmt.Errorf("missing module %s", id)
	}
	return Source{Name: id.String(), Text: source}, nil
}

func TestMachineRuntimeCapturesAndRepeatedlyCallsScriptCallback(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "machine")
	entry := LogicalModule("machine/runtime")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		entry.String(): `
local changed = false
return {
    startup = function()
        capture(function()
            if changed then
                changed = false
                return 2
            end
            changed = true
            return 1
        end)
    end,
}
`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: entry}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}

	var callback Callback
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{
			"capture": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("capture received %d arguments, want 1", len(args))
				}
				captured, err := CaptureCallback(ctx, args[0])
				if err != nil {
					return nil, err
				}
				callback = captured
				return nil, nil
			}),
		}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	report, err := runtime.RunHook(context.Background(), "startup")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Calls) != 1 || !report.Calls[0].Loaded || !report.Calls[0].Called {
		t.Fatalf("RunHook report = %#v, want one loaded and called entrypoint", report)
	}

	for index, want := range []float64{1, 2, 1} {
		values, err := callback.Call(context.Background())
		if err != nil {
			t.Fatalf("Callback.Call %d: %v", index+1, err)
		}
		if len(values) != 1 {
			t.Fatalf("Callback.Call %d returned %d values, want 1", index+1, len(values))
		}
		if got, ok := values[0].Number(); !ok || got != want {
			t.Fatalf("Callback.Call %d result = %v (%t), want %v", index+1, got, ok, want)
		}
	}
	if err := callback.Close(); err != nil {
		t.Fatal(err)
	}
}
