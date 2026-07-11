package ember

import "testing"

func TestGlobalEnvVersionTracksHostOverridesAndAssignments(t *testing.T) {
	base := runtimeGlobals(nil)
	if base.version != 0 {
		t.Fatalf("base global version = %d, want 0", base.version)
	}

	host := map[string]Value{"score": NumberValue(7)}
	env := runtimeGlobals(host)
	if env.version == 0 {
		t.Fatal("host override global version is 0, want distinct override state")
	}

	initial := env.version
	env.set("score", NumberValue(12))
	if env.version != initial+1 {
		t.Fatalf("global version after host write = %d, want %d", env.version, initial+1)
	}
	if got, ok := host["score"].Number(); !ok || got != 12 {
		t.Fatalf("host score after write is %v (%t), want 12", got, ok)
	}

	env.set("status", StringValue("ready"))
	if env.version != initial+2 {
		t.Fatalf("global version after new write = %d, want %d", env.version, initial+2)
	}
	if got, ok := host["status"].String(); !ok || got != "ready" {
		t.Fatalf("host status after write is %q (%t), want ready", got, ok)
	}
}

func TestGlobalEnvVersionIgnoresBaseGlobalCache(t *testing.T) {
	env := runtimeGlobals(nil)
	initial := env.version
	if _, ok := env.get("math"); !ok {
		t.Fatal("get(math) failed, want base global")
	}
	if env.version != initial {
		t.Fatalf("global version after base cache = %d, want %d", env.version, initial)
	}
}

func TestBaseFieldIntrinsicCalleeRefreshesNativeOwnField(t *testing.T) {
	mathTable := NewTable()
	mathTable.setRawStringField("min", nativeFuncValueWithID(baseMathMinNative, nativeFuncMathMin))
	env := runtimeGlobals(map[string]Value{"math": TableValue(mathTable)})

	callee, fast, err := mathIntrinsicCallee(env, "min")
	if err != nil {
		t.Fatalf("mathIntrinsicCallee returned error: %v", err)
	}
	if !fast || callee.nativeID != nativeFuncMathMin {
		t.Fatalf("first callee = %#v fast %t, want native math.min", callee, fast)
	}

	mathTable.setRawStringField("min", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{StringValue("overridden")}, nil
	}))
	callee, fast, err = mathIntrinsicCallee(env, "min")
	if err != nil {
		t.Fatalf("mathIntrinsicCallee after override returned error: %v", err)
	}
	if fast {
		t.Fatal("mathIntrinsicCallee override was fast, want ordinary call fallback")
	}
	results, err := callValue(callee, env, nil)
	if err != nil {
		t.Fatalf("override call returned error: %v", err)
	}
	if got, ok := results[0].String(); !ok || got != "overridden" {
		t.Fatalf("override result = %q (%t), want overridden", got, ok)
	}

	mathTable.setRawStringField("min", nativeFuncValueWithID(baseMathMinNative, nativeFuncMathMin))
	callee, fast, err = mathIntrinsicCallee(env, "min")
	if err != nil {
		t.Fatalf("mathIntrinsicCallee after restore returned error: %v", err)
	}
	if !fast || callee.nativeID != nativeFuncMathMin {
		t.Fatalf("restored callee = %#v fast %t, want native math.min", callee, fast)
	}
}

func TestBaseFieldIntrinsicCalleeKeepsMetatableLookupVisible(t *testing.T) {
	mathTable := NewTable()
	metatable := NewTable()
	lookups := 0
	metatable.setRawStringField("__index", HostFuncValue(func(args []Value) ([]Value, error) {
		lookups++
		return []Value{nativeFuncValueWithID(baseMathMinNative, nativeFuncMathMin)}, nil
	}))
	mathTable.setMetatable(metatable)
	env := runtimeGlobals(map[string]Value{"math": TableValue(mathTable)})

	for i := 1; i <= 2; i++ {
		callee, fast, err := mathIntrinsicCallee(env, "min")
		if err != nil {
			t.Fatalf("mathIntrinsicCallee call %d returned error: %v", i, err)
		}
		if !fast || callee.nativeID != nativeFuncMathMin {
			t.Fatalf("callee %d = %#v fast %t, want native math.min", i, callee, fast)
		}
		if lookups != i {
			t.Fatalf("metatable lookups after call %d = %d, want %d", i, lookups, i)
		}
	}
}

func TestBaseFieldIntrinsicCalleeHoistsAbsentHostGlobalGuard(t *testing.T) {
	env := runtimeGlobals(map[string]Value{"score": NumberValue(1)})
	thread := newVMThread(env)
	restore := thread.activate()
	defer restore()
	var counts directFramePICCounts
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts

	for i := 0; i < 4; i++ {
		callee, fast, err := mathIntrinsicCallee(env, "min")
		if err != nil {
			t.Fatalf("mathIntrinsicCallee call %d returned error: %v", i+1, err)
		}
		if !fast || callee.nativeID != nativeFuncMathMin {
			t.Fatalf("callee %d = %#v fast %t, want base math.min", i+1, callee, fast)
		}
	}
	if thread.intrinsicGuards == nil {
		t.Fatal("thread intrinsic guards are nil, want hoisted guard")
	}
	if thread.intrinsicGuards.resolutions != 1 {
		t.Fatalf("intrinsic guard resolutions = %d, want 1", thread.intrinsicGuards.resolutions)
	}
	if thread.intrinsicGuards.hits != 3 {
		t.Fatalf("intrinsic guard hits = %d, want 3", thread.intrinsicGuards.hits)
	}
	if counts.intrinsicGuardChecks != 4 {
		t.Fatalf("intrinsic guard checks = %d, want 4", counts.intrinsicGuardChecks)
	}
	if counts.intrinsicGuardHits != 3 {
		t.Fatalf("intrinsic guard hits = %d, want 3", counts.intrinsicGuardHits)
	}
	if counts.intrinsicGuardMisses != 1 {
		t.Fatalf("intrinsic guard misses = %d, want 1", counts.intrinsicGuardMisses)
	}
}

func TestBaseFieldIntrinsicCalleeHoistedGuardInvalidatesOnGlobalWrite(t *testing.T) {
	env := runtimeGlobals(map[string]Value{"score": NumberValue(1)})
	thread := newVMThread(env)
	restore := thread.activate()
	defer restore()

	if _, fast, err := mathIntrinsicCallee(env, "min"); err != nil || !fast {
		t.Fatalf("initial mathIntrinsicCallee fast = %t err = %v, want fast nil", fast, err)
	}
	mathTable := NewTable()
	if err := mathTable.Set(StringValue("min"), HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{StringValue("mutated")}, nil
	})); err != nil {
		t.Fatalf("Set min returned error: %v", err)
	}
	env.set("math", TableValue(mathTable))

	callee, fast, err := mathIntrinsicCallee(env, "min")
	if err != nil {
		t.Fatalf("mathIntrinsicCallee after global write returned error: %v", err)
	}
	if fast {
		t.Fatal("mathIntrinsicCallee after global write was fast, want ordinary call")
	}
	results, err := callValue(callee, env, nil)
	if err != nil {
		t.Fatalf("mutated callee returned error: %v", err)
	}
	if got, ok := results[0].String(); !ok || got != "mutated" {
		t.Fatalf("mutated result = %q (%t), want mutated", got, ok)
	}
}

func TestBaseFieldIntrinsicCalleeHoistedGuardInvalidatesOnTableMutation(t *testing.T) {
	mathTable := NewTable()
	mathTable.setRawStringField("min", nativeFuncValueWithID(baseMathMinNative, nativeFuncMathMin))
	env := runtimeGlobals(map[string]Value{"math": TableValue(mathTable)})
	thread := newVMThread(env)
	restore := thread.activate()
	defer restore()

	if _, fast, err := mathIntrinsicCallee(env, "min"); err != nil || !fast {
		t.Fatalf("initial mathIntrinsicCallee fast = %t err = %v, want fast nil", fast, err)
	}
	mathTable.setRawStringField("min", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{StringValue("changed")}, nil
	}))

	callee, fast, err := mathIntrinsicCallee(env, "min")
	if err != nil {
		t.Fatalf("mathIntrinsicCallee after table mutation returned error: %v", err)
	}
	if fast {
		t.Fatal("mathIntrinsicCallee after table mutation was fast, want ordinary call")
	}
	results, err := callValue(callee, env, nil)
	if err != nil {
		t.Fatalf("changed callee returned error: %v", err)
	}
	if got, ok := results[0].String(); !ok || got != "changed" {
		t.Fatalf("changed result = %q (%t), want changed", got, ok)
	}
}
