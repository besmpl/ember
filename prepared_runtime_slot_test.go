package ember

import (
	"context"
	"errors"
	"math"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPreparedRuntimeSlotPreparesSourceUnknownAtHostBuild(t *testing.T) {
	program := preparedRuntimeSlotTestProgram(t, 41)
	var slot PreparedRuntimeSlot

	candidate, err := slot.Prepare(program, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := slot.Use(preparedRuntimeSlotNoop); !errors.Is(err, ErrPreparedRuntimeUnavailable) {
		t.Fatalf("Use before activation error = %v, want ErrPreparedRuntimeUnavailable", err)
	}
	if err := slot.Activate(candidate); err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 41 {
		t.Fatalf("reload-time prepared generation result = %v, want 41", got)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotRetiresReloadTimeNativeImages(t *testing.T) {
	if !preparedRuntimeNativeTestPlatform() {
		t.Skip("reload-time native execution requires Darwin, Linux, or Windows on ARM64 or x86-64")
	}
	firstProgram := preparedRuntimeSlotTestProgram(t, 41)
	secondProgram := preparedRuntimeSlotTestProgram(t, 42)
	var slot PreparedRuntimeSlot

	first, err := slot.Prepare(firstProgram, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	var firstExecutable interface {
		Call(...float64) (float64, bool, error)
	}
	for _, executable := range first.native.executables {
		if executable != nil {
			firstExecutable = executable
			break
		}
	}
	if firstExecutable == nil {
		t.Skip("current CPU or process policy selected canonical fallback")
	}
	if err := slot.Activate(first); err != nil {
		t.Fatal(err)
	}

	second, err := slot.Prepare(secondProgram, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := preparedRuntimeSlotResult(t, &slot); got != 41 {
		t.Fatalf("result before activation = %v, want 41", got)
	}
	if err := slot.Activate(second); err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotResult(t, &slot); got != 42 {
		t.Fatalf("result after activation = %v, want 42", got)
	}
	if _, _, err := firstExecutable.Call(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("retired native image call error = %v, want closed", err)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotRepeatedReloadsReclaimNativeImages(t *testing.T) {
	if !preparedRuntimeNativeTestPlatform() {
		t.Skip("reload-time native execution requires Darwin, Linux, or Windows on ARM64 or x86-64")
	}
	var slot PreparedRuntimeSlot
	var active interface {
		Call(...float64) (float64, bool, error)
	}

	for generation := 0; generation < 1024; generation++ {
		want := 100 + generation
		program := preparedRuntimeSlotTestProgram(t, want)
		candidate, err := slot.Prepare(program, RuntimeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var next interface {
			Call(...float64) (float64, bool, error)
		}
		for _, executable := range candidate.native.executables {
			if executable != nil {
				next = executable
				break
			}
		}
		if next == nil {
			_ = candidate.Close()
			t.Skip("current CPU or process policy selected canonical fallback")
		}
		if err := slot.Activate(candidate); err != nil {
			_ = candidate.Close()
			t.Fatal(err)
		}
		if active != nil {
			if _, _, err := active.Call(); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("generation %d retained old native image: %v", generation, err)
			}
		}
		active = next
		if got := preparedRuntimeSlotResult(t, &slot); got != float64(want) {
			t.Fatalf("generation %d result = %v, want %d", generation, got, want)
		}
	}

	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := active.Call(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("last native image after slot close error = %v, want closed", err)
	}
}

func TestPreparedRuntimeSlotExecutionPolicyUsesCanonicalMachine(t *testing.T) {
	module := LogicalModule("prepared/reload-native-limits")
	program := preparedRuntimeSlotSourceProgram(t, module, `
return function(n)
    local total = 0
    while n > 0 do
        total = total + 1
        n = n - 1
    end
    return total
end
`)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []backendNativeArchitecture{
		backendNativeArchitectureARM64,
		backendNativeArchitectureX8664,
	} {
		artifact, err := emitBackendNativeProgram(ir, architecture)
		if err != nil {
			t.Fatalf("architecture %d: %v", architecture, err)
		}
		if len(artifact.modules) != 1 || len(artifact.modules[0].functions) < 2 ||
			!artifact.modules[0].functions[1].prepared {
			t.Fatalf("architecture %d bounded-loop inventory = %#v", architecture, artifact.modules)
		}
	}

	if !preparedRuntimeNativeTestPlatform() {
		return
	}
	var slot PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, RuntimeOptions{
		Limits: ExecutionLimits{MaxInstructions: 256},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	native := false
	for _, executable := range candidate.native.executables {
		native = native || executable != nil
	}
	if !native {
		t.Skip("current CPU or process policy selected canonical fallback")
	}
	if err := slot.Activate(candidate); err != nil {
		t.Fatal(err)
	}
	if err := slot.Use(func(runtime *Runtime) error {
		_, invokeErr := runtime.Invoke(
			context.Background(),
			Invocation{Module: module},
			NumberValue(1),
		)
		return invokeErr
	}); err != nil {
		t.Fatalf("warm limited native-capable Invoke: %v", err)
	}
	err = slot.Use(func(runtime *Runtime) error {
		_, invokeErr := runtime.Invoke(
			context.Background(),
			Invocation{Module: module},
			NumberValue(100000),
		)
		return invokeErr
	})
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("limited native-capable Invoke error = %v, want ErrLimitExceeded", err)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}

	var cancellableSlot PreparedRuntimeSlot
	candidate, err = cancellableSlot.Prepare(program, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := cancellableSlot.Activate(candidate); err != nil {
		t.Fatal(err)
	}
	if err := cancellableSlot.Use(func(runtime *Runtime) error {
		_, invokeErr := runtime.Invoke(
			context.Background(),
			Invocation{Module: module},
			NumberValue(1),
		)
		return invokeErr
	}); err != nil {
		t.Fatalf("warm native-capable Invoke: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err = cancellableSlot.Use(func(runtime *Runtime) error {
		_, invokeErr := runtime.Invoke(
			canceled,
			Invocation{Module: module},
			NumberValue(100000),
		)
		return invokeErr
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellable native-capable Invoke error = %v, want context.Canceled", err)
	}
	if err := cancellableSlot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotReloadTimeNativeReplaysBeforeEffects(t *testing.T) {
	module := LogicalModule("prepared/reload-native-replay")
	program := preparedRuntimeSlotSourceProgram(t, module, `
return {
    numeric = function(x) return x % 3 end,
    effectful = function(x)
        record(x)
        return x + 1
    end,
}
`)
	var slot PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := slot.Activate(candidate); err != nil {
		t.Fatal(err)
	}

	var numeric []Value
	err = slot.Use(func(runtime *Runtime) error {
		var invokeErr error
		numeric, invokeErr = runtime.Invoke(
			context.Background(),
			Invocation{Module: module, Export: "numeric"},
			NumberValue(math.NaN()),
		)
		return invokeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(numeric) != 1 {
		t.Fatalf("numeric replay values = %v, want one NaN", numeric)
	}
	number, ok := numeric[0].Number()
	if !ok || !math.IsNaN(number) {
		t.Fatalf("numeric replay value = %v, want NaN", numeric[0])
	}

	recorded := 0
	var effectful []Value
	err = slot.Use(func(runtime *Runtime) error {
		var invokeErr error
		effectful, invokeErr = runtime.Invoke(
			context.Background(),
			Invocation{
				Module: module,
				Export: "effectful",
				Globals: map[string]Value{
					"record": HostFuncValue(func(arguments []Value) ([]Value, error) {
						recorded++
						return nil, nil
					}),
				},
			},
			NumberValue(41),
		)
		return invokeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if recorded != 1 {
		t.Fatalf("effectful fallback calls = %d, want 1", recorded)
	}
	if len(effectful) != 1 {
		t.Fatalf("effectful fallback values = %v, want one result", effectful)
	}
	result, ok := effectful[0].Number()
	if !ok || result != 42 {
		t.Fatalf("effectful fallback value = %v, want 42", effectful[0])
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRuntimeSlotReloadTimeNativeExecutesBoundedSelfRecursion(t *testing.T) {
	if !preparedRuntimeNativeTestPlatform() {
		t.Skip("reload-time native execution requires Darwin, Linux, or Windows on ARM64 or x86-64")
	}
	module := LogicalModule("prepared/reload-native-recursion")
	program := preparedRuntimeSlotSourceProgram(t, module, `
local function fib(n)
    if n < 2 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
return {fib = fib}
`)
	var slot PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if candidate.native == nil || len(candidate.native.executables) != 1 || candidate.native.executables[0] == nil {
		t.Fatal("bounded self-recursive function was not installed as native code")
	}
	if err := slot.Activate(candidate); err != nil {
		t.Fatal(err)
	}

	if got := preparedRuntimeSlotNumberInvoke(t, &slot, module, "fib", 20); got != 6765 {
		t.Fatalf("native fib(20) = %v, want 6765", got)
	}
	// Native entry admission is deliberately capped. The value above the cap
	// must replay the exact canonical Machine function, not fail or truncate.
	if got := preparedRuntimeSlotNumberInvoke(t, &slot, module, "fib", 25); got != 75025 {
		t.Fatalf("replayed fib(25) = %v, want 75025", got)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedNativeArchitecturesQualifyBoundedSelfRecursion(t *testing.T) {
	module := LogicalModule("prepared/native-recursion-inventory")
	program := preparedRuntimeSlotSourceProgram(t, module, `
local function descend(n)
    if n < 1 then
        return n
    end
    return descend(n - 1) + 1
end
return {descend = descend}
`)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []backendNativeArchitecture{
		backendNativeArchitectureARM64,
		backendNativeArchitectureX8664,
	} {
		artifact, err := emitBackendNativeProgram(ir, architecture)
		if err != nil {
			t.Fatalf("architecture %d: %v", architecture, err)
		}
		if len(artifact.modules) != 1 || len(artifact.modules[0].functions) < 2 ||
			!artifact.modules[0].functions[1].prepared {
			t.Fatalf("architecture %d recursion inventory = %#v", architecture, artifact.modules)
		}
	}
}

func TestPreparedRuntimeSlotNativeCarriesCapturedNumbersInsideGeneration(t *testing.T) {
	module := LogicalModule("prepared/reload-native-captured-recursion")
	program := preparedRuntimeSlotSourceProgram(t, module, `
local function run(n, seed)
    local function fib(x)
        if x < 2 + seed % 3 then
            return x
        end
        return fib(x - 1) + fib(x - 2)
    end
    return fib(n)
end
return run
`)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []backendNativeArchitecture{
		backendNativeArchitectureARM64,
		backendNativeArchitectureX8664,
	} {
		artifact, err := emitBackendNativeProgram(ir, architecture)
		if err != nil {
			t.Fatalf("architecture %d: %v", architecture, err)
		}
		if len(artifact.modules) != 1 || len(artifact.modules[0].functions) != 3 ||
			!artifact.modules[0].functions[1].prepared ||
			!artifact.modules[0].functions[2].prepared {
			t.Fatalf("architecture %d captured inventory = %#v", architecture, artifact.modules)
		}
	}

	if !preparedRuntimeNativeTestPlatform() {
		return
	}
	var slot PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	functions := candidate.native.bundle.program.modules[0].functions
	if len(functions) != 3 || functions[1] == nil || functions[2] != nil {
		t.Fatalf("captured prepared entries = %#v, want parent only", functions)
	}
	if err := slot.Activate(candidate); err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotNumberInvokeMany(t, &slot, module, "", 5, 0); got != 5 {
		t.Fatalf("native captured run(5, 0) = %v, want 5", got)
	}
	if got := preparedRuntimeSlotNumberInvokeMany(t, &slot, module, "", 5, 1); got != 8 {
		t.Fatalf("native captured run(5, 1) = %v, want 8", got)
	}
	if got := preparedRuntimeSlotNumberInvokeMany(t, &slot, module, "", 25, 0); got != 75025 {
		t.Fatalf("replayed captured run(25, 0) = %v, want 75025", got)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

const preparedRuntimeSlotMutableCaptureSource = `
local function run(x)
    local bias = 1
    local function read()
        return bias
    end
    if x > 0 then
        bias = 2
    end
    return read()
end
return run
`

func TestRuntimeMutableCaptureBranchSemantics(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv(runtimeEngineEnvironment, engine)
			module := LogicalModule("runtime/mutable-capture-" + engine)
			program := preparedRuntimeSlotSourceProgram(t, module, preparedRuntimeSlotMutableCaptureSource)
			owner, err := program.NewRuntime(RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			values, err := owner.Invoke(
				context.Background(),
				Invocation{Module: module},
				NumberValue(4),
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 {
				t.Fatalf("mutable-capture values = %v, want one number", values)
			}
			got, ok := values[0].Number()
			if !ok || got != 2 {
				t.Fatalf("mutable-capture run(4) = %v, want 2", values[0])
			}
		})
	}
}

func TestPreparedRuntimeSlotMutableCaptureUsesCanonicalFallback(t *testing.T) {
	module := LogicalModule("prepared/reload-native-mutable-capture")
	program := preparedRuntimeSlotSourceProgram(t, module, preparedRuntimeSlotMutableCaptureSource)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []backendNativeArchitecture{
		backendNativeArchitectureARM64,
		backendNativeArchitectureX8664,
	} {
		artifact, err := emitBackendNativeProgram(ir, architecture)
		if err != nil {
			t.Fatalf("architecture %d: %v", architecture, err)
		}
		if len(artifact.modules) != 1 || len(artifact.modules[0].functions) != 3 {
			t.Fatalf("architecture %d mutable-capture inventory = %#v", architecture, artifact.modules)
		}
		for protoIndex, function := range artifact.modules[0].functions {
			if function.prepared {
				t.Fatalf("architecture %d mutable-capture Proto %d was emitted: %#v", architecture, protoIndex, function)
			}
		}
	}

	if !preparedRuntimeNativeTestPlatform() {
		return
	}
	var slot PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := slot.Activate(candidate); err != nil {
		t.Fatal(err)
	}
	if got := preparedRuntimeSlotNumberInvokeMany(t, &slot, module, "", 4); got != 2 {
		t.Fatalf("mutable-capture fallback run(4) = %v, want 2", got)
	}
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
}

func preparedRuntimeNativeTestPlatform() bool {
	return (runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows") &&
		(runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64")
}

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

func preparedRuntimeSlotNumberInvoke(
	t *testing.T,
	slot *PreparedRuntimeSlot,
	module ModuleID,
	export string,
	argument float64,
) float64 {
	return preparedRuntimeSlotNumberInvokeMany(t, slot, module, export, argument)
}

func preparedRuntimeSlotNumberInvokeMany(
	t *testing.T,
	slot *PreparedRuntimeSlot,
	module ModuleID,
	export string,
	arguments ...float64,
) float64 {
	t.Helper()
	values := make([]Value, len(arguments))
	for index, argument := range arguments {
		values[index] = NumberValue(argument)
	}
	var result float64
	err := slot.Use(func(runtime *Runtime) error {
		results, err := runtime.Invoke(
			context.Background(),
			Invocation{Module: module, Export: export},
			values...,
		)
		if err != nil {
			return err
		}
		var ok bool
		if len(results) == 1 {
			result, ok = results[0].Number()
		}
		if !ok {
			t.Fatalf("Invoke values = %v, want one number", results)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
