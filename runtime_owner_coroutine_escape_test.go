package ember

import "testing"

func TestStatelessRunSuspendedCoroutineRetainsDetachedGlobals(t *testing.T) {
	proto, err := Compile(`
shared = 41
local co = coroutine.create(function()
	coroutine.yield("pause")
	return shared + 1
end)
local ok, value = coroutine.resume(co)
return co, ok, value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	coroutine := testCoroutineValue(t, results, 0)
	if len(results) != 3 {
		t.Fatalf("initial resume = %v, want three values", results)
	}
	initial, initialOK := results[2].String()
	if results[1] != BoolValue(true) || !initialOK || initial != "pause" {
		t.Fatalf("initial resume = %v, want coroutine,true,pause", results)
	}

	resumed, err := baseCoroutineResume(runtimeGlobals(nil), []Value{UserDataValue(coroutine.userdata)})
	if err != nil {
		t.Fatalf("resume escaped coroutine returned Go error: %v", err)
	}
	if len(resumed) != 2 || resumed[0] != BoolValue(true) || resumed[1] != NumberValue(42) {
		t.Fatalf("escaped coroutine resume = %v, want true,42", resumed)
	}
}

func TestStatelessRunCreatedCoroutineRetainsDetachedGlobals(t *testing.T) {
	proto, err := Compile(`
shared = 41
return coroutine.create(function()
	return shared + 1
end)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	coroutine := testCoroutineValue(t, results, 0)
	resumed, err := baseCoroutineResume(runtimeGlobals(nil), []Value{UserDataValue(coroutine.userdata)})
	if err != nil {
		t.Fatalf("resume escaped coroutine returned Go error: %v", err)
	}
	if len(resumed) != 2 || resumed[0] != BoolValue(true) || resumed[1] != NumberValue(42) {
		t.Fatalf("escaped coroutine resume = %v, want true,42", resumed)
	}
}

func TestStatelessRunWrappedCoroutineRetainsDetachedGlobals(t *testing.T) {
	proto, err := Compile(`
shared = 41
return coroutine.wrap(function()
	return shared + 1
end)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d values, want one", len(results))
	}
	wrapped, ok := results[0].nativeFunction()
	if !ok || wrapped == nil {
		t.Fatalf("Run result = %v, want wrapped host function", results[0])
	}

	// Exercise the stateless VM pool before invoking the escaped wrapper. The
	// wrapper must retain the coroutine's detached defining environment rather
	// than the pooled environment used by its creating Run call.
	if _, err := Run(proto); err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	resumed, err := wrapped(nil, nil)
	if err != nil {
		t.Fatalf("escaped wrapped coroutine returned error: %v", err)
	}
	if len(resumed) != 1 || resumed[0] != NumberValue(42) {
		t.Fatalf("escaped wrapped coroutine = %v, want 42", resumed)
	}
}

func TestStatelessNestedWrappedCoroutineObservesNormalParent(t *testing.T) {
	proto, err := Compile(`
local outer = nil
local wrapped = coroutine.wrap(function()
	return coroutine.status(outer)
end)
outer = coroutine.create(function()
	return wrapped()
end)
local ok, status = coroutine.resume(outer)
return ok, status
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 2 || results[0] != BoolValue(true) {
		t.Fatalf("nested wrapped resume = %v, want true,normal", results)
	}
	status, ok := results[1].String()
	if !ok || status != string(vmCoroutineNormal) {
		t.Fatalf("nested wrapped parent status = %v, want normal", results[1])
	}
}
