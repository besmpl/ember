package ember

import "testing"

func TestDirectConstantStringFieldCacheUsesPointerHits(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 1}
local total = 0
for i = 1, 8 do
	 total = total + row.hp
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 8 {
		t.Fatalf("result is %v (%t), want 8", got, ok)
	}
	if snapshot.picCounts.pointerHits == 0 {
		t.Fatal("pointer hits = 0, want warmed constant-key loop to hit the live slot")
	}
	if snapshot.picCounts.hashByteFallbacks != 0 {
		t.Fatalf("hash/byte fallbacks = %d, want zero for warmed constant-key loop", snapshot.picCounts.hashByteFallbacks)
	}
}

func TestDirectConstantStringFieldCacheWritesKeepReadsLive(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 1, shield = 2}
local total = 0
for i = 1, 8 do
	local next = row.hp + 1
	row.hp = next
	total = total + row.shield
end
return total, row.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 16 {
		t.Fatalf("total is %v (%t), want 16", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 9 {
		t.Fatalf("current hp is %v (%t), want 9", got, ok)
	}
	if snapshot.picCounts.pointerHits == 0 {
		t.Fatal("pointer hits = 0, want warmed constant-key reads/writes to hit live slots")
	}
	if snapshot.picCounts.hashByteFallbacks != 0 {
		t.Fatalf("hash/byte fallbacks = %d, want zero for constant-key reads/writes", snapshot.picCounts.hashByteFallbacks)
	}
}

func TestDirectConstantStringFieldOverflowCacheUsesCurrentValue(t *testing.T) {
	proto, err := Compile(`
local row = {
	field0 = 0, field1 = 1, field2 = 2, field3 = 3,
	field4 = 4, field5 = 5, field6 = 6, field7 = 7,
	target = 1,
}
local total = 0
for i = 1, 4 do
	total = total + row.target
	row.target = row.target + 1
end
return total, row.target
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 10 {
		t.Fatalf("total is %v (%t), want 10", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 5 {
		t.Fatalf("current target is %v (%t), want 5", got, ok)
	}
	if snapshot.picCounts.indexedHashHits == 0 {
		t.Fatal("indexed hash hits = 0, want warmed overflow slot reads/writes")
	}
	if snapshot.picCounts.hashByteFallbacks != 0 {
		t.Fatalf("hash/byte fallbacks = %d, want zero for the interned constant key", snapshot.picCounts.hashByteFallbacks)
	}
}

func TestDirectConstantStringFieldDebugBudgetKeepsCurrentValue(t *testing.T) {
	proto, err := Compile(`
local row = {target = 3}
local total = 0
for i = 1, 4 do
	total = total + row.target
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts
	thread.debugLineHook = true
	thread.debugHook = func(_ *globalEnv, _ vmDebugEvent) error { return nil }
	thread.controller = testExecutionController(t, 10_000)
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("debug/budget run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 12 {
		t.Fatalf("debug/budget result is %v (%t), want 12", got, ok)
	}
}
