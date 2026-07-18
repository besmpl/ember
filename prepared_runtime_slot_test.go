package ember

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestPreparedRuntimeSlotActivatesOnlyAtExplicitSafePoint(t *testing.T) {
	firstProgram := preparedRuntimeSlotTestProgram(t, 41)
	secondProgram := preparedRuntimeSlotTestProgram(t, 42)
	firstPreparedCalls := 0
	secondPreparedCalls := 0
	firstBundle := replayPreparedBundleForTest(t, firstProgram, func() { firstPreparedCalls++ })
	secondBundle := replayPreparedBundleForTest(t, secondProgram, func() { secondPreparedCalls++ })

	var slot PreparedRuntimeSlot
	first, err := slot.Prepare(firstProgram, RuntimeOptions{Prepared: firstBundle})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := slot.Use(func(*Runtime) error { return nil }); !errors.Is(err, ErrPreparedRuntimeUnavailable) {
		t.Fatalf("Use before activation error = %v, want ErrPreparedRuntimeUnavailable", err)
	}
	if err := slot.Activate(first); err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 41 {
		t.Fatalf("first generation result = %v, want 41", got)
	}

	second, err := slot.Prepare(secondProgram, RuntimeOptions{Prepared: secondBundle})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := preparedRuntimeSlotResult(t, &slot); got != 41 {
		t.Fatalf("result before activation = %v, want 41", got)
	}
	if secondPreparedCalls != 0 {
		t.Fatalf("second generation executed during preparation %d times", secondPreparedCalls)
	}
	if err := slot.Activate(second); err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 42 {
		t.Fatalf("second generation result = %v, want 42", got)
	}
	if firstPreparedCalls == 0 || secondPreparedCalls == 0 {
		t.Fatalf("prepared calls = first %d, second %d; want both nonzero", firstPreparedCalls, secondPreparedCalls)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotRejectsMismatchedAndStaleCandidatesAtomically(t *testing.T) {
	firstProgram := preparedRuntimeSlotTestProgram(t, 41)
	secondProgram := preparedRuntimeSlotTestProgram(t, 42)
	firstBundle := replayPreparedBundleForTest(t, firstProgram, nil)
	secondBundle := replayPreparedBundleForTest(t, secondProgram, nil)

	var slot PreparedRuntimeSlot
	first, err := slot.Prepare(firstProgram, RuntimeOptions{Prepared: firstBundle})
	if err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(first); err != nil {
		t.Fatal(err)
	}

	wrong, err := slot.Prepare(secondProgram, RuntimeOptions{Prepared: firstBundle})
	if wrong != nil || err == nil {
		t.Fatalf("Prepare mismatch = (%v, %v), want nil error candidate", wrong, err)
	}
	var mismatch *PreparedBundleError
	if !errors.As(err, &mismatch) {
		t.Fatalf("Prepare mismatch error = %T %v, want *PreparedBundleError", err, err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 41 {
		t.Fatalf("active result after mismatch = %v, want 41", got)
	}

	second, err := slot.Prepare(secondProgram, RuntimeOptions{Prepared: secondBundle})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stale, err := slot.Prepare(secondProgram, RuntimeOptions{Prepared: secondBundle})
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	if err := slot.Activate(second); err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(stale); !errors.Is(err, ErrPreparedRuntimeCandidateStale) {
		t.Fatalf("Activate stale candidate error = %v, want ErrPreparedRuntimeCandidateStale", err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 42 {
		t.Fatalf("active result after stale candidate = %v, want 42", got)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotActivationRequiresAnIdleSafePoint(t *testing.T) {
	firstProgram := preparedRuntimeSlotTestProgram(t, 41)
	secondProgram := preparedRuntimeSlotTestProgram(t, 42)
	var slot PreparedRuntimeSlot
	first, err := slot.Prepare(firstProgram, RuntimeOptions{
		Prepared: replayPreparedBundleForTest(t, firstProgram, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(first); err != nil {
		t.Fatal(err)
	}
	second, err := slot.Prepare(secondProgram, RuntimeOptions{
		Prepared: replayPreparedBundleForTest(t, secondProgram, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- slot.Use(func(runtime *Runtime) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	if err := slot.Activate(second); !errors.Is(err, ErrRuntimeBusy) {
		close(release)
		t.Fatalf("Activate during Use error = %v, want ErrRuntimeBusy", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 41 {
		t.Fatalf("active result after busy activation = %v, want 41", got)
	}
	if err := slot.Activate(second); err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 42 {
		t.Fatalf("active result after safe-point activation = %v, want 42", got)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotRetiresOldGenerationCallbacks(t *testing.T) {
	module := LogicalModule("prepared/reload-callback")
	firstProgram := preparedRuntimeSlotSourceProgram(t, module, `
return {
    capture = function()
        retain(function() return 41 end)
    end,
}
`)
	secondProgram := preparedRuntimeSlotSourceProgram(t, module, `
return {
    capture = function()
        retain(function() return 42 end)
    end,
}
`)
	var callback Callback
	host := RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{
			"retain": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				captured, err := CaptureCallback(ctx, args[0])
				if err == nil {
					callback = captured
				}
				return nil, err
			}),
		}, nil
	})

	var slot PreparedRuntimeSlot
	first, err := slot.Prepare(firstProgram, RuntimeOptions{
		Host:     host,
		Prepared: replayPreparedBundleForTest(t, firstProgram, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(first); err != nil {
		t.Fatal(err)
	}
	if err := slot.Use(func(runtime *Runtime) error {
		_, err := runtime.Dispatch(context.Background(), "capture")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	values, err := callback.Call(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("old callback values = %v, want one value", values)
	}

	second, err := slot.Prepare(secondProgram, RuntimeOptions{
		Host:     host,
		Prepared: replayPreparedBundleForTest(t, secondProgram, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := slot.Activate(second); err != nil {
		t.Fatal(err)
	}
	if _, err := callback.Call(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("old callback after activation error = %v, want closed generation", err)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotRetiresOldGenerationSuspensions(t *testing.T) {
	module := LogicalModule("prepared/reload-suspension")
	program := preparedRuntimeSlotSourceProgram(t, module, `
return {
    update = function()
        return wait()
    end,
}
`)
	bundle := replayPreparedBundleForTest(t, program, nil)
	host := RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{
			"wait": ResumableHostFuncValue(func(context.Context, []Value) HostResult {
				return HostSuspend("reload-token")
			}),
		}, nil
	})

	var slot PreparedRuntimeSlot
	first, err := slot.Prepare(program, RuntimeOptions{Host: host, Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(first); err != nil {
		t.Fatal(err)
	}
	var suspension *Suspension
	if err := slot.Use(func(runtime *Runtime) error {
		result, err := runtime.DispatchResumable(context.Background(), "update")
		if err == nil && len(result.Suspensions) == 1 {
			suspension = result.Suspensions[0]
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if suspension == nil {
		t.Fatal("DispatchResumable returned no suspension")
	}

	second, err := slot.Prepare(program, RuntimeOptions{Host: host, Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := slot.Activate(second); err != nil {
		t.Fatal(err)
	}
	if _, err := suspension.Resume(context.Background()); !errors.Is(err, ErrSuspensionStale) {
		t.Fatalf("old suspension after activation error = %v, want ErrSuspensionStale", err)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotUseAllocatesNothing(t *testing.T) {
	program := preparedRuntimeSlotTestProgram(t, 41)
	var slot PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, RuntimeOptions{
		Prepared: replayPreparedBundleForTest(t, program, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(candidate); err != nil {
		t.Fatal(err)
	}
	defer slot.Close()

	var useErr error
	allocations := testing.AllocsPerRun(1000, func() {
		useErr = slot.Use(preparedRuntimeSlotNoop)
	})
	if useErr != nil {
		t.Fatal(useErr)
	}
	if allocations != 0 {
		t.Fatalf("PreparedRuntimeSlot.Use allocations = %v, want 0", allocations)
	}
}

func TestPreparedRuntimeSlotCandidateAndCloseLifecycle(t *testing.T) {
	program := preparedRuntimeSlotTestProgram(t, 41)
	bundle := replayPreparedBundleForTest(t, program, nil)
	var slot PreparedRuntimeSlot

	discarded, err := slot.Prepare(program, RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	if err := discarded.Close(); err != nil {
		t.Fatal(err)
	}
	if err := discarded.Close(); err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(discarded); !errors.Is(err, ErrPreparedRuntimeCandidateUsed) {
		t.Fatalf("Activate closed candidate error = %v, want ErrPreparedRuntimeCandidateUsed", err)
	}

	active, err := slot.Prepare(program, RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	if err := slot.Activate(active); err != nil {
		t.Fatal(err)
	}
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
	if err := slot.Use(preparedRuntimeSlotNoop); !errors.Is(err, ErrPreparedRuntimeUnavailable) {
		t.Fatalf("Use closed slot error = %v, want ErrPreparedRuntimeUnavailable", err)
	}
	if candidate, err := slot.Prepare(program, RuntimeOptions{Prepared: bundle}); candidate != nil || !errors.Is(err, ErrPreparedRuntimeUnavailable) {
		t.Fatalf("Prepare closed slot = (%v, %v), want ErrPreparedRuntimeUnavailable", candidate, err)
	}
}

func BenchmarkPreparedRuntimeSlotUse(b *testing.B) {
	program := preparedRuntimeSlotTestProgram(b, 41)
	var slot PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, RuntimeOptions{
		Prepared: replayPreparedBundleForTest(b, program, nil),
	})
	if err != nil {
		b.Fatal(err)
	}
	if err := slot.Activate(candidate); err != nil {
		b.Fatal(err)
	}
	defer slot.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := slot.Use(preparedRuntimeSlotNoop); err != nil {
			b.Fatal(err)
		}
	}
}

func preparedRuntimeSlotNoop(*Runtime) error { return nil }

func preparedRuntimeSlotTestProgram(t testing.TB, result int) *Program {
	t.Helper()
	module := LogicalModule("prepared/reload")
	return preparedRuntimeSlotSourceProgram(t, module, "return { update = function() return "+strconv.Itoa(result)+" end }")
}

func preparedRuntimeSlotSourceProgram(t testing.TB, module ModuleID, source string) *Program {
	t.Helper()
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		module.String(): source,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func preparedRuntimeSlotResult(t *testing.T, slot *PreparedRuntimeSlot) float64 {
	t.Helper()
	var result float64
	err := slot.Use(func(runtime *Runtime) error {
		values, err := runtime.Invoke(context.Background(), Invocation{
			Module: LogicalModule("prepared/reload"),
			Export: "update",
		})
		if err != nil {
			return err
		}
		var ok bool
		if len(values) == 1 {
			result, ok = values[0].Number()
		}
		if !ok {
			t.Fatalf("Invoke values = %v, want one number", values)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
