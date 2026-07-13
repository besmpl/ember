package ember

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestRuntimeRejectsOverlappingRunHook(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := closeOnce(release)
	runtime := newRuntimeOwnerIntegrationRuntimeSource(t, `
return { startup = function() block() end }
	`, blockingRuntimeHost(entered, release))
	defer runtime.Close()
	defer unblock()

	firstDone := make(chan error, 1)
	go func() {
		_, err := runtime.RunHook(context.Background(), "startup")
		firstDone <- err
	}()
	<-entered

	if _, err := runtime.RunHook(context.Background(), "startup"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("overlapping RunHook error = %v, want ErrRuntimeBusy", err)
	}
	unblock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunHook returned error: %v", err)
	}
}

func TestCallbackRejectsOverlappingCall(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := closeOnce(release)
	runtime, callback := newBlockingCallback(t, entered, release)
	defer runtime.Close()
	defer callback.Close()
	defer unblock()

	firstDone := make(chan error, 1)
	go func() {
		_, err := callback.Call(context.Background())
		firstDone <- err
	}()
	<-entered

	if _, err := callback.Call(context.Background()); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("overlapping Callback.Call error = %v, want ErrRuntimeBusy", err)
	}
	unblock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Callback.Call returned error: %v", err)
	}
}

func TestRunHookRejectsCallWhileCallbackActive(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := closeOnce(release)
	runtime, callback := newBlockingCallback(t, entered, release)
	defer runtime.Close()
	defer callback.Close()
	defer unblock()

	firstDone := make(chan error, 1)
	go func() {
		_, err := callback.Call(context.Background())
		firstDone <- err
	}()
	<-entered

	if _, err := runtime.RunHook(context.Background(), "startup"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("RunHook during Callback.Call error = %v, want ErrRuntimeBusy", err)
	}
	unblock()
	if err := <-firstDone; err != nil {
		t.Fatalf("Callback.Call returned error: %v", err)
	}
}

func TestCaptureCallbackWorksInsideActiveHostCall(t *testing.T) {
	var callback Callback
	runtime := newRuntimeOwnerIntegrationRuntimeSource(t, `
return { startup = function() capture(function() return 7 end) end }
`, RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{
			"capture": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				var err error
				callback, err = CaptureCallback(ctx, args[0])
				return nil, err
			}),
		}, nil
	}))
	defer runtime.Close()

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	defer callback.Close()
	if callback.state == nil {
		t.Fatal("host call did not capture callback")
	}
	values, err := callback.Call(context.Background())
	if err != nil {
		t.Fatalf("captured callback call returned error: %v", err)
	}
	if len(values) != 1 || values[0] != NumberValue(7) {
		t.Fatalf("captured callback values = %#v, want [7]", values)
	}
}

func blockingRuntimeHost(entered, release chan struct{}) RuntimeHost {
	return RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{
			"block": ContextHostFuncValue(func(context.Context, []Value) ([]Value, error) {
				close(entered)
				<-release
				return nil, nil
			}),
		}, nil
	})
}

func closeOnce(channel chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() { close(channel) })
	}
}

func newBlockingCallback(t *testing.T, entered, release chan struct{}) (*Runtime, Callback) {
	t.Helper()
	var callback Callback
	runtime := newRuntimeOwnerIntegrationRuntimeSource(t, `
return { startup = function() connect(function() block() end) end }
`, RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{
			"connect": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				var err error
				callback, err = CaptureCallback(ctx, args[0])
				return nil, err
			}),
			"block": ContextHostFuncValue(func(context.Context, []Value) ([]Value, error) {
				close(entered)
				<-release
				return nil, nil
			}),
		}, nil
	}))
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		runtime.Close()
		t.Fatalf("RunHook returned error: %v", err)
	}
	if callback.state == nil {
		runtime.Close()
		t.Fatal("host call did not capture callback")
	}
	return runtime, callback
}
