package ember_test

import (
	"context"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func TestInvokeSupportsRequestMiddlewareHost(t *testing.T) {
	loader := &programTestLoader{sources: map[string]string{
		"logical:middleware/audit": `return {
			handle = function(request)
				local value = "audit:" .. request
				record(value)
				return value
			end,
		}`,
		"logical:middleware/metrics": `return function(request)
			local value = "metrics:" .. request
			record(value)
			return value
		end`,
	}}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "audit", Module: ember.LogicalModule("middleware/audit")},
			{Name: "metrics", Module: ember.LogicalModule("middleware/metrics")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			var recorded []string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			globals := map[string]ember.Value{
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					value, _ := args[0].String()
					recorded = append(recorded, value)
					return nil, nil
				}),
			}
			audit, err := runtime.Invoke(context.Background(), ember.Invocation{
				Module:  ember.LogicalModule("middleware/audit"),
				Export:  "handle",
				Globals: globals,
			}, ember.StringValue("request-42"))
			if err != nil {
				t.Fatal(err)
			}
			metrics, err := runtime.Invoke(context.Background(), ember.Invocation{
				Module:  ember.LogicalModule("middleware/metrics"),
				Globals: globals,
			}, ember.StringValue("request-42"))
			if err != nil {
				t.Fatal(err)
			}
			if value, _ := audit[0].String(); value != "audit:request-42" {
				t.Fatalf("audit result = %#v", audit)
			}
			if value, _ := metrics[0].String(); value != "metrics:request-42" {
				t.Fatalf("metrics result = %#v", metrics)
			}
			if len(recorded) != 2 || recorded[0] != "audit:request-42" || recorded[1] != "metrics:request-42" {
				t.Fatalf("recorded = %v", recorded)
			}
		})
	}
}

func TestInvokeResumableSupportsBatchProcessorHost(t *testing.T) {
	loader := &programTestLoader{sources: map[string]string{
		"logical:workers/thumbnail": `return {
			process = function(job)
				local result = awaitResult(job)
				record(result)
			end,
		}`,
	}}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "thumbnail", Module: ember.LogicalModule("workers/thumbnail")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			var recorded string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			step, err := runtime.InvokeResumable(context.Background(), ember.Invocation{
				Module: ember.LogicalModule("workers/thumbnail"),
				Export: "process",
				Globals: map[string]ember.Value{
					"awaitResult": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
						job, _ := args[0].String()
						return ember.HostSuspend("queue:" + job)
					}),
					"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
						recorded, _ = args[0].String()
						return nil, nil
					}),
				},
			}, ember.StringValue("image-7"))
			if err != nil {
				t.Fatal(err)
			}
			if step.Dispatch != nil || step.Hook != nil {
				t.Fatalf("single invocation acquired dispatch policy = %#v", step)
			}
			if step.Suspension == nil || step.Suspension.Token() != "queue:image-7" {
				t.Fatalf("suspension = %#v", step.Suspension)
			}
			if step.Suspension.Operation() != "process" || step.Suspension.Hook() != step.Suspension.Operation() {
				t.Fatalf("suspension operation = %q legacy hook = %q", step.Suspension.Operation(), step.Suspension.Hook())
			}
			if step.Suspension.Entrypoint() != "" || step.Suspension.Module().String() != "logical:workers/thumbnail" {
				t.Fatalf("suspension attribution = entrypoint %q module %q", step.Suspension.Entrypoint(), step.Suspension.Module().String())
			}

			done, err := step.Suspension.Resume(context.Background(), ember.StringValue("stored:thumbnail.png"))
			if err != nil {
				t.Fatal(err)
			}
			if done.Dispatch != nil || done.Suspension != nil {
				t.Fatalf("completed invocation = %#v", done)
			}
			if recorded != "stored:thumbnail.png" {
				t.Fatalf("recorded = %q", recorded)
			}
		})
	}
}

func TestIndependentInvocationsShareSuspendedModuleInitialization(t *testing.T) {
	loader := &programTestLoader{sources: map[string]string{
		"logical:pipeline/a": `local shared = require("./shared")
			return { process = function() return "a:" .. shared end }`,
		"logical:pipeline/b": `local shared = require("./shared")
			return { process = function() return "b:" .. shared end }`,
		"logical:pipeline/shared": `return awaitReady()`,
	}}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "a", Module: ember.LogicalModule("pipeline/a")},
			{Name: "b", Module: ember.LogicalModule("pipeline/b")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			globals := map[string]ember.Value{
				"awaitReady": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
					return ember.HostSuspend("shared-ready")
				}),
			}
			runtime, err := program.NewRuntime(ember.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			first, err := runtime.InvokeResumable(context.Background(), ember.Invocation{
				Module: ember.LogicalModule("pipeline/a"), Export: "process", Globals: globals,
			})
			if err != nil {
				t.Fatal(err)
			}
			second, err := runtime.InvokeResumable(context.Background(), ember.Invocation{
				Module: ember.LogicalModule("pipeline/b"), Export: "process", Globals: globals,
			})
			if err != nil {
				t.Fatal(err)
			}
			if first.Suspension.Token() != "shared-ready" || !first.Suspension.Ready() {
				t.Fatalf("owner suspension = token %#v ready %v", first.Suspension.Token(), first.Suspension.Ready())
			}
			if second.Suspension.Token() != nil || second.Suspension.Ready() {
				t.Fatalf("dependent suspension = token %#v ready %v", second.Suspension.Token(), second.Suspension.Ready())
			}

			firstDone, err := first.Suspension.Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			if value, _ := firstDone.Values[0].String(); value != "a:ready" {
				t.Fatalf("first result = %#v", firstDone.Values)
			}
			if !second.Suspension.Ready() {
				t.Fatal("dependent invocation did not become ready")
			}
			secondDone, err := second.Suspension.Resume(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if value, _ := secondDone.Values[0].String(); value != "b:ready" {
				t.Fatalf("second result = %#v", secondDone.Values)
			}
		})
	}
}

func TestDispatchMirrorsOperationMetadataForLegacyHosts(t *testing.T) {
	program := loadSingleProgram(t, `return { handle = function() end }`)
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			var calls []ember.HostCall
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
					calls = append(calls, call)
					return nil, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			report, err := runtime.Dispatch(context.Background(), "handle")
			if err != nil {
				t.Fatal(err)
			}
			if report.Operation != "handle" || report.Hook != report.Operation || len(report.Calls) != 1 {
				t.Fatalf("dispatch report = %#v", report)
			}
			if report.Calls[0].Operation != "handle" || report.Calls[0].Hook != report.Calls[0].Operation {
				t.Fatalf("dispatch call = %#v", report.Calls[0])
			}
			if len(calls) != 2 || calls[0].Operation != "" || calls[1].Operation != "handle" || calls[1].Hook != calls[1].Operation {
				t.Fatalf("host calls = %#v", calls)
			}
		})
	}
}

func TestInvokeRejectsMissingOrNonCallableTargets(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		invocation ember.Invocation
		want       string
	}{
		{
			name:   "module outside program",
			source: `return function() end`,
			invocation: ember.Invocation{
				Module: ember.LogicalModule("other/module"),
			},
			want: "not in program",
		},
		{
			name:   "missing named export",
			source: `return {}`,
			invocation: ember.Invocation{
				Module: ember.LogicalModule("game/init"),
				Export: "process",
			},
			want: `has no export "process"`,
		},
		{
			name:   "non-callable direct export",
			source: `return 42`,
			invocation: ember.Invocation{
				Module: ember.LogicalModule("game/init"),
			},
			want: "want script function",
		},
	}

	for _, engine := range []string{"vm", "machine"} {
		for _, test := range tests {
			t.Run(engine+"/"+test.name, func(t *testing.T) {
				t.Setenv("EMBER_RUNTIME_ENGINE", engine)
				program := loadSingleProgram(t, test.source)
				runtime, err := program.NewRuntime(ember.RuntimeOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.Close()

				_, err = runtime.Invoke(context.Background(), test.invocation)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("Invoke error = %v, want substring %q", err, test.want)
				}
			})
		}
	}
}
