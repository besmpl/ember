package ember

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const runtimeEngineTestProcess = "EMBER_RUNTIME_ENGINE_TEST_PROCESS"

func TestRuntimeExecutionEngineSelection(t *testing.T) {
	if testCase := os.Getenv(runtimeEngineTestProcess); testCase != "" {
		runRuntimeExecutionEngineSelectionCase(t, testCase)
		return
	}

	for _, testCase := range []string{"default", "vm", "invalid", "machine", "pinned"} {
		t.Run(testCase, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestRuntimeExecutionEngineSelection$")
			command.Env = runtimeExecutionTestEnvironment(testCase)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("selection subprocess: %v\n%s", err, output)
			}
		})
	}
}

func runRuntimeExecutionEngineSelectionCase(t *testing.T, testCase string) {
	t.Helper()
	program := runtimeExecutionTestProgram(t)

	switch testCase {
	case "default", "vm":
		runtime, err := program.NewRuntime(RuntimeOptions{})
		if err != nil {
			t.Fatalf("NewRuntime returned error: %v", err)
		}
		if _, err := runtime.RunHook(context.Background(), "update"); err != nil {
			t.Fatalf("RunHook returned error: %v", err)
		}
		if err := runtime.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case "invalid":
		if _, err := program.NewRuntime(RuntimeOptions{}); err == nil || !strings.Contains(err.Error(), "invalid EMBER_RUNTIME_ENGINE") {
			t.Fatalf("NewRuntime error = %v, want invalid engine setting", err)
		}
	case "machine":
		runtime, err := program.NewRuntime(RuntimeOptions{})
		if err != nil {
			t.Fatalf("NewRuntime returned error: %v", err)
		}
		if program.programImage == nil {
			t.Fatal("forced machine selection did not prepare the whole Program image")
		}
		if _, ok := runtime.execution.(*machineRuntimeExecution); !ok {
			t.Fatalf("execution = %T, want *machineRuntimeExecution", runtime.execution)
		}
		if runtime.owner != nil || runtime.entrypoints != nil || runtime.loaded != nil || runtime.requireAdapters != nil || runtime.active != nil {
			t.Fatal("Machine runtime allocated legacy VM state")
		}
		report, err := runtime.RunHook(context.Background(), "update")
		if err != nil {
			t.Fatalf("RunHook returned error: %v", err)
		}
		if len(report.Calls) != 1 || !report.Calls[0].Loaded || !report.Calls[0].Called {
			t.Fatalf("RunHook report = %#v, want one loaded and called entrypoint", report)
		}
		if err := runtime.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
		invalid := runtimeExecutionTestProgram(t)
		invalid.protos = nil
		if _, err := invalid.NewRuntime(RuntimeOptions{}); err == nil || !strings.Contains(err.Error(), "prepare machine Program image") || !strings.Contains(err.Error(), "missing prototype") {
			t.Fatalf("invalid Program error = %v, want image preparation failure before adapter selection", err)
		}
	case "pinned":
		runtime, err := program.NewRuntime(RuntimeOptions{})
		if err != nil {
			t.Fatalf("NewRuntime returned error: %v", err)
		}
		if err := os.Setenv("EMBER_RUNTIME_ENGINE", "machine"); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.RunHook(context.Background(), "update"); err != nil {
			t.Fatalf("RunHook changed engine after construction: %v", err)
		}
		if err := runtime.Close(); err != nil {
			t.Fatalf("Close changed engine after construction: %v", err)
		}
	default:
		t.Fatalf("unknown helper case %q", testCase)
	}
}

func runtimeExecutionTestEnvironment(testCase string) []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, runtimeEngineTestProcess+"=") || strings.HasPrefix(item, "EMBER_RUNTIME_ENGINE=") {
			continue
		}
		environment = append(environment, item)
	}
	environment = append(environment, runtimeEngineTestProcess+"="+testCase)
	switch testCase {
	case "vm", "pinned":
		environment = append(environment, "EMBER_RUNTIME_ENGINE=vm")
	case "invalid":
		environment = append(environment, "EMBER_RUNTIME_ENGINE=other")
	case "machine":
		environment = append(environment, "EMBER_RUNTIME_ENGINE=machine")
	}
	return environment
}

func runtimeExecutionTestProgram(t *testing.T) *Program {
	t.Helper()
	proto, err := Compile(`return { update = function() return 7 end }`)
	if err != nil {
		t.Fatal(err)
	}
	key := moduleKey{kind: moduleKeyLogical, path: "runtime-engine-test"}
	return &Program{
		entrypoints: []programEntrypoint{{name: "main", key: key}},
		graph: moduleGraph{Nodes: map[moduleKey]moduleGraphNode{
			key: {Source: Source{Name: "runtime-engine-test"}},
		}},
		protos: map[moduleKey]*Proto{key: proto},
	}
}

func TestRuntimeExecutionRoutingIsIdentityBlind(t *testing.T) {
	contents, err := os.ReadFile("runtime_execution.go")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(contents))
	for _, forbidden := range []string{"benchmark", "workload", "source"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("runtime execution routing contains forbidden identity %q", forbidden)
		}
	}
}

type runtimeExecutionRoutingProbe struct {
	initialized   int
	runHooks      int
	captures      int
	runtimeClose  int
	callbackCalls int
	callbackClose int
}

func (probe *runtimeExecutionRoutingProbe) initialize(*Runtime) error {
	probe.initialized++
	return nil
}

func (probe *runtimeExecutionRoutingProbe) runHook(_ *Runtime, _ context.Context, _ string, _ []Value, _ *HookReport) error {
	probe.runHooks++
	return nil
}

func (probe *runtimeExecutionRoutingProbe) captureCallback(invocationScope, Value) (callbackTarget, error) {
	probe.captures++
	return runtimeExecutionCallbackProbe{probe: probe}, nil
}

func (probe *runtimeExecutionRoutingProbe) close(*Runtime) error {
	probe.runtimeClose++
	return nil
}

type runtimeExecutionCallbackProbe struct {
	probe *runtimeExecutionRoutingProbe
}

func (target runtimeExecutionCallbackProbe) call(context.Context, []Value) ([]Value, error) {
	target.probe.callbackCalls++
	return []Value{NumberValue(7)}, nil
}

func (target runtimeExecutionCallbackProbe) close() error {
	target.probe.callbackClose++
	return nil
}

func TestRuntimeExecutionOwnsCallbackAndRuntimeLifetime(t *testing.T) {
	probe := &runtimeExecutionRoutingProbe{}
	runtime := &Runtime{execution: probe}
	if err := probe.initialize(runtime); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RunHook(context.Background(), "update"); err != nil {
		t.Fatal(err)
	}
	scope := invocationScope{runtime: runtime}
	callback, err := CaptureCallback(contextWithInvocationScope(context.Background(), scope), NilValue())
	if err != nil {
		t.Fatal(err)
	}
	values, err := callback.Call(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0] != NumberValue(7) {
		t.Fatalf("callback values = %#v, want [7]", values)
	}
	if err := callback.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if probe.initialized != 1 || probe.runHooks != 1 || probe.captures != 1 || probe.callbackCalls != 1 || probe.callbackClose != 1 || probe.runtimeClose != 1 {
		t.Fatalf("routing counts = %+v, want every operation exactly once", probe)
	}
}

func TestRuntimeExecutionNeverFallsBackWhenOwnerIsMissing(t *testing.T) {
	runtime := &Runtime{}
	if _, err := runtime.RunHook(context.Background(), "update"); err == nil || !strings.Contains(err.Error(), "execution owner is unavailable") {
		t.Fatalf("RunHook error = %v, want unavailable execution owner", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close error = %v, want zero-value success", err)
	}
	if _, err := runtime.RunHook(context.Background(), "update"); err == nil || !strings.Contains(err.Error(), "runtime: closed") {
		t.Fatalf("RunHook after Close error = %v, want closed runtime", err)
	}
}
