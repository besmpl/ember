package ember

import (
	"context"
	"strings"
	"testing"
)

func TestRuntimeCloseStillReportsActiveRun(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := closeOnce(release)
	runtime := newRuntimeOwnerIntegrationRuntime(t, RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		return nil, nil
	}))
	defer unblock()
	defer runtime.Close()
	if runtime.owner == nil {
		t.Fatal("NewRuntime did not create a runtime owner")
	}

	runDone := make(chan error, 1)
	go func() {
		_, err := runtime.RunHook(context.Background(), "startup")
		runDone <- err
	}()
	<-entered

	if err := runtime.Close(); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("Close during RunHook returned %v, want active error", err)
	}
	unblock()
	if err := <-runDone; err != nil {
		t.Fatalf("RunHook after rejected Close returned error: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close after RunHook returned error: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("repeated Close returned error: %v", err)
	}
	if _, err := runtime.RunHook(context.Background(), "startup"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("RunHook after Close returned %v, want closed error", err)
	}
}

func TestZeroRuntimeCloseRemainsRepeatSafe(t *testing.T) {
	var runtime Runtime
	if err := runtime.Close(); err != nil {
		t.Fatalf("zero Runtime Close returned error: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("zero Runtime repeated Close returned error: %v", err)
	}
	if _, err := runtime.RunHook(context.Background(), "startup"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("zero Runtime RunHook after Close returned %v, want closed error", err)
	}
}

func TestRuntimeCloseRejectsActiveCapturedCallback(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := closeOnce(release)
	var callback Callback
	runtime := newRuntimeOwnerIntegrationRuntimeSource(t, `
return {
	startup = function()
		connect(function()
			block()
			return 1
		end)
	end,
}
`, RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{
			"connect": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				captured, err := CaptureCallback(ctx, args[0])
				if err != nil {
					return nil, err
				}
				callback = captured
				return nil, nil
			}),
			"block": HostFuncValue(func([]Value) ([]Value, error) {
				close(entered)
				<-release
				return nil, nil
			}),
		}, nil
	}))
	defer unblock()
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := callback.Call(context.Background())
		callDone <- err
	}()
	<-entered
	if err := runtime.Close(); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("Close during Callback.Call returned %v, want active error", err)
	}
	unblock()
	if err := <-callDone; err != nil {
		t.Fatalf("Callback.Call after rejected Close returned error: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close after Callback.Call returned error: %v", err)
	}
	if _, err := callback.Call(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Callback.Call after Close returned %v, want closed error", err)
	}
}

func newRuntimeOwnerIntegrationRuntime(t *testing.T, host RuntimeHost) *Runtime {
	return newRuntimeOwnerIntegrationRuntimeSource(t, `return { startup = function() return nil end }`, host)
}

func newRuntimeOwnerIntegrationRuntimeSource(t *testing.T, source string, host RuntimeHost) *Runtime {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	key := moduleKey{kind: moduleKeyLogical, path: "test"}
	program := &Program{
		entrypoints: []programEntrypoint{{name: "main", key: key}},
		protos:      map[moduleKey]*Proto{key: proto},
	}
	runtime, err := program.NewRuntime(RuntimeOptions{Host: host})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	return runtime
}
