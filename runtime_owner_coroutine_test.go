package ember

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOwnedCoroutineInheritsOwnerWithoutKeepingItActive(t *testing.T) {
	owner := newRuntimeOwner()
	proto, err := Compile(`return coroutine.create(function() return 1 end)`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := executeProto(context.Background(), proto, runtimeGlobalsWithOwner(nil, owner), executeOptions{maxInstructions: -1})
	if err != nil {
		t.Fatalf("executeProto returned error: %v", err)
	}
	coroutine := testCoroutineValue(t, results, 0)
	if coroutine.owner != owner {
		t.Fatalf("coroutine owner = %p, want %p", coroutine.owner, owner)
	}
	if coroutine.thread.owner != owner {
		t.Fatalf("coroutine thread owner = %p, want %p", coroutine.thread.owner, owner)
	}
	if got := owner.coroutineCount(); got != 0 {
		t.Fatalf("suspended coroutine count = %d, want 0", got)
	}
	if got := owner.threadCount(); got != 0 {
		t.Fatalf("owner thread count after creation = %d, want 0", got)
	}
}

func TestOwnedCoroutineResumeRegistrationTracksYieldAndReturn(t *testing.T) {
	owner := newRuntimeOwner()
	observed := make([]int, 0, 2)
	globals := runtimeGlobalsWithOwner(map[string]Value{
		"observe": nativeFuncValue(func(env *globalEnv, _ []Value) ([]Value, error) {
			if env == nil || env.thread == nil || env.thread.owner != owner {
				return nil, errors.New("coroutine lost runtime owner")
			}
			observed = append(observed, owner.coroutineCount())
			return nil, nil
		}),
	}, owner)
	proto, err := Compile(`
local co = coroutine.create(function()
	observe()
	coroutine.yield("pause")
	observe()
	return "done"
end)
local ok1, first = coroutine.resume(co)
local ok2, second = coroutine.resume(co)
return ok1, first, ok2, second, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := executeProto(context.Background(), proto, globals, executeOptions{maxInstructions: -1})
	if err != nil {
		t.Fatalf("executeProto returned error: %v", err)
	}
	if len(observed) != 2 || observed[0] != 1 || observed[1] != 1 {
		t.Fatalf("coroutine registration observations = %v, want [1 1]", observed)
	}
	if got := owner.coroutineCount(); got != 0 {
		t.Fatalf("coroutine count after yield/resume = %d, want 0", got)
	}
	if len(results) != 5 {
		t.Fatalf("resume results = %v, want five values", results)
	}
	first, firstOK := results[1].String()
	second, secondOK := results[3].String()
	status, statusOK := results[4].String()
	if results[0] != BoolValue(true) || !firstOK || first != "pause" ||
		results[2] != BoolValue(true) || !secondOK || second != "done" || !statusOK || status != "dead" {
		t.Fatalf("resume results = %v, want true,pause,true,done,dead", results)
	}
}

func TestOwnedCoroutineResumeRegistrationTracksErrorExit(t *testing.T) {
	owner := newRuntimeOwner()
	seen := 0
	globals := runtimeGlobalsWithOwner(map[string]Value{
		"observe": nativeFuncValue(func(env *globalEnv, _ []Value) ([]Value, error) {
			if env == nil || env.thread == nil || env.thread.owner != owner {
				return nil, errors.New("coroutine lost runtime owner")
			}
			seen = owner.coroutineCount()
			return nil, nil
		}),
		"fail": nativeFuncValue(func(*globalEnv, []Value) ([]Value, error) {
			return nil, errors.New("expected coroutine failure")
		}),
	}, owner)
	proto, err := Compile(`
local co = coroutine.create(function()
	observe()
	fail()
end)
local ok, message = coroutine.resume(co)
return ok, message, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := executeProto(context.Background(), proto, globals, executeOptions{maxInstructions: -1})
	if err != nil {
		t.Fatalf("executeProto returned error: %v", err)
	}
	if seen != 1 {
		t.Fatalf("coroutine registration during error exit = %d, want 1", seen)
	}
	if got := owner.coroutineCount(); got != 0 {
		t.Fatalf("coroutine count after error exit = %d, want 0", got)
	}
	if len(results) != 3 {
		t.Fatalf("error resume results = %v, want three values", results)
	}
	status, statusOK := results[2].String()
	if results[0] != BoolValue(false) || !statusOK || status != "dead" {
		t.Fatalf("error resume results = %v, want false,message,dead", results)
	}
	message, ok := results[1].String()
	if !ok || !strings.Contains(message, "expected coroutine failure") {
		t.Fatalf("error resume message = %v, want expected coroutine failure", results[1])
	}
}

func TestOwnedCoroutineRejectsCrossOwnerAndUnownedResume(t *testing.T) {
	ownerA := newRuntimeOwner()
	ownerB := newRuntimeOwner()
	proto, err := Compile(`return coroutine.create(function() coroutine.yield("pause") end)`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := executeProto(context.Background(), proto, runtimeGlobalsWithOwner(nil, ownerA), executeOptions{maxInstructions: -1})
	if err != nil {
		t.Fatalf("executeProto returned error: %v", err)
	}
	coroutine := testCoroutineValue(t, results, 0)

	for name, globals := range map[string]*globalEnv{
		"owner B": runtimeGlobalsWithOwner(nil, ownerB),
		"unowned": runtimeGlobals(nil),
	} {
		resumed, err := baseCoroutineResume(globals, []Value{UserDataValue(coroutine.userdata)})
		if err != nil {
			t.Fatalf("%s resume returned Go error: %v", name, err)
		}
		if len(resumed) < 2 || resumed[0] != BoolValue(false) {
			t.Fatalf("%s resume = %v, want false,message", name, resumed)
		}
		message, ok := resumed[1].String()
		if !ok || !strings.Contains(message, "runtime owner") {
			t.Fatalf("%s resume message = %v, want runtime-owner rejection", name, resumed[1])
		}
		if coroutine.status != vmCoroutineSuspended {
			t.Fatalf("%s resume changed status to %q", name, coroutine.status)
		}
		if got := ownerA.coroutineCount(); got != 0 {
			t.Fatalf("owner A coroutine count after %s rejection = %d, want 0", name, got)
		}
	}
}

func TestSuspendedOwnedCoroutineDoesNotBlockOwnerClose(t *testing.T) {
	owner := newRuntimeOwner()
	proto, err := Compile(`return coroutine.create(function() coroutine.yield("pause") end)`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := executeProto(context.Background(), proto, runtimeGlobalsWithOwner(nil, owner), executeOptions{maxInstructions: -1})
	if err != nil {
		t.Fatalf("executeProto returned error: %v", err)
	}
	coroutine := testCoroutineValue(t, results, 0)
	if got := owner.coroutineCount(); got != 0 {
		t.Fatalf("suspended coroutine count = %d, want 0", got)
	}
	if err := owner.close(); err != nil {
		t.Fatalf("close with suspended coroutine: %v", err)
	}
	if len(coroutine.suspended.frames) != 0 || coroutine.suspended.owner != nil || coroutine.thread.stackOwner != nil {
		t.Fatalf("close retained suspended execution state: suspended=%+v stackOwner=%p", coroutine.suspended, coroutine.thread.stackOwner)
	}
	if coroutine.root != nil || len(coroutine.thread.frameSlots) != 0 {
		t.Fatalf("close retained coroutine root/frame slots: root=%p frameSlots=%d", coroutine.root, len(coroutine.thread.frameSlots))
	}

	resumed, err := baseCoroutineResume(runtimeGlobalsWithOwner(nil, owner), []Value{UserDataValue(coroutine.userdata)})
	if err != nil {
		t.Fatalf("resume after close returned Go error: %v", err)
	}
	if len(resumed) < 2 || resumed[0] != BoolValue(false) {
		t.Fatalf("resume after close = %v, want false,message", resumed)
	}
	message, ok := resumed[1].String()
	if !ok || !strings.Contains(message, "closed") {
		t.Fatalf("resume after close message = %v, want closed", resumed[1])
	}
	if coroutine.status != vmCoroutineSuspended {
		t.Fatalf("resume after close changed status to %q", coroutine.status)
	}
}

func testCoroutineValue(t *testing.T, values []Value, index int) *vmCoroutine {
	t.Helper()
	if index < 0 || index >= len(values) {
		t.Fatalf("missing coroutine result at %d in %v", index, values)
	}
	userdata, ok := values[index].UserData()
	if !ok || userdata == nil {
		t.Fatalf("result %d = %v, want coroutine userdata", index, values[index])
	}
	coroutine, ok := userdata.Payload().(*vmCoroutine)
	if !ok || coroutine == nil {
		t.Fatalf("userdata payload = %T, want *vmCoroutine", userdata.Payload())
	}
	return coroutine
}
