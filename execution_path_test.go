package ember

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestForcedExecutionModesReportPathAndUnsupportedModes(t *testing.T) {
	proto, err := Compile("return 1 + 2")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	for _, test := range []struct {
		name  string
		mode  executionMode
		check func(*executionPathStats) uint64
	}{
		{name: "direct", mode: executionModeDirect, check: func(stats *executionPathStats) uint64 { return stats.directProductionInstructions }},
		{name: "slot", mode: executionModeSlot, check: func(stats *executionPathStats) uint64 { return stats.slotInstructions }},
		{name: "numeric slot", mode: executionModeNumericSlot, check: func(stats *executionPathStats) uint64 { return stats.numericSlotInstructions }},
	} {
		t.Run(test.name, func(t *testing.T) {
			stats := &executionPathStats{}
			values, err := executeProto(context.Background(), proto, nil, executeOptions{mode: test.mode, stats: stats})
			if err != nil {
				t.Fatalf("forced %s returned error: %v", test.name, err)
			}
			if len(values) != 1 || values[0] != NumberValue(3) {
				t.Fatalf("forced %s result = %#v, want [3]", test.name, values)
			}
			if count := test.check(stats); count == 0 {
				t.Fatalf("forced %s recorded no instructions: %#v", test.name, stats)
			}
		})
	}
	instrumentedStats := &executionPathStats{}
	values, err := executeProto(context.Background(), proto, nil, executeOptions{
		mode:         executionModeDirect,
		stats:        instrumentedStats,
		instrumented: true,
	})
	if err != nil || len(values) != 1 || values[0] != NumberValue(3) {
		t.Fatalf("forced instrumented direct result = (%#v, %v), want [3]", values, err)
	}
	if instrumentedStats.directInstrumentedInstructions == 0 || instrumentedStats.directProductionInstructions != 0 {
		t.Fatalf("forced instrumented direct path stats = %#v", instrumentedStats)
	}

	compact, err := Compile(`
local function add(left, right)
	return left + right
end
return add(add(1, 2), add(3, 4))
`)
	if err != nil {
		t.Fatalf("Compile compact fixture: %v", err)
	}
	compactStats := &executionPathStats{}
	values, err = executeProto(context.Background(), compact, nil, executeOptions{mode: executionModeCompactCall, stats: compactStats})
	if err != nil || len(values) != 1 || values[0] != NumberValue(10) {
		t.Fatalf("forced compact result = (%#v, %v), want [10]", values, err)
	}
	if compactStats.compactCallInstructions == 0 {
		t.Fatalf("forced compact call recorded no instructions: %#v", compactStats)
	}

	unsupported, err := Compile("return missingGlobal")
	if err != nil {
		t.Fatalf("Compile unsupported fixture: %v", err)
	}
	if _, err := executeProto(context.Background(), unsupported, nil, executeOptions{mode: executionModeNumericSlot}); err == nil || !strings.Contains(err.Error(), "forced numeric slot execution unsupported") {
		t.Fatalf("forced unsupported mode error = %v, want clear numeric-slot error", err)
	}
	if _, err := executeProto(context.Background(), unsupported, runtimeGlobals(nil), executeOptions{mode: executionModeSlot}); err == nil || !strings.Contains(err.Error(), "forced slot execution unsupported") {
		t.Fatalf("forced globals mode error = %v, want clear slot error", err)
	}
	if _, err := executeProto(context.Background(), proto, nil, executeOptions{mode: executionMode(255)}); err == nil || !strings.Contains(err.Error(), "forced unknown execution unsupported") {
		t.Fatalf("invalid forced mode error = %v, want clear unknown-mode error", err)
	}
}

func TestAutoExecutionStatsExposeFallbackReasons(t *testing.T) {
	proto, err := Compile("return math.min(1, 2)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	stats := &executionPathStats{}
	if _, err := executeProto(context.Background(), proto, nil, executeOptions{stats: stats}); err != nil {
		t.Fatalf("auto execution returned error: %v", err)
	}
	if stats.fallbacks[executionFallbackSlotIneligible] == 0 {
		t.Fatalf("auto execution did not record slot ineligibility: %#v", stats)
	}
	if stats.directProductionInstructions == 0 {
		t.Fatalf("auto execution did not reach direct loop: %#v", stats)
	}
}

func TestExecutionStatsDistinguishDirectSubpaths(t *testing.T) {
	proto, err := Compile(`
local rows = {{cooldown = 2}, {cooldown = 0}, {cooldown = 3}}
local total = 0
for _, row in rows do
	if row.cooldown > 0 then
		row.cooldown = row.cooldown - 1
	end
	total = total + row.cooldown
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.directLoopKernels == nil || len(proto.directLoopKernels.kernels) == 0 {
		t.Fatal("fixture has no direct loop kernel")
	}
	stats := &executionPathStats{}
	values, err := executeProto(context.Background(), proto, nil, executeOptions{mode: executionModeDirect, stats: stats})
	if err != nil || len(values) != 1 || values[0] != NumberValue(3) {
		t.Fatalf("direct kernel result = (%#v, %v), want [3]", values, err)
	}
	if stats.directProductionInstructions == 0 || stats.loopKernelInstructions == 0 {
		t.Fatalf("direct kernel paths were not distinguished: %#v", stats)
	}
}

func TestExecutionStatsCountColdSideExits(t *testing.T) {
	proto, err := Compile(`
local value = setmetatable({}, {__index = function() return 7 end})
return value.missing
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	stats := &executionPathStats{}
	values, err := executeProto(context.Background(), proto, nil, executeOptions{mode: executionModeDirect, stats: stats})
	if err != nil || len(values) != 1 || values[0] != NumberValue(7) {
		t.Fatalf("cold-side-exit result = (%#v, %v), want [7]", values, err)
	}
	if stats.coldInstructions == 0 || stats.directProductionInstructions == 0 {
		t.Fatalf("cold and production paths were not distinguished: %#v", stats)
	}
}

func TestExecutionStatsRestoreUnlimitedControllerAndHonorLimits(t *testing.T) {
	proto, err := Compile("return 1 + 2")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	unlimited, err := newExecutionController(context.Background(), ExecutionLimits{})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	stats := &executionPathStats{}
	if _, err := executeProto(context.Background(), proto, nil, executeOptions{
		controller: unlimited,
		mode:       executionModeNumericSlot,
		stats:      stats,
	}); err != nil {
		t.Fatalf("unlimited observed execution returned error: %v", err)
	}
	if unlimited.remaining != -1 || unlimited.limits.MaxInstructions != 0 || unlimited.speculativeInstructions != 0 || unlimited.trackSpeculativeInstructions {
		t.Fatalf("unlimited controller was not restored: remaining=%d limit=%d speculative=%d tracking=%t", unlimited.remaining, unlimited.limits.MaxInstructions, unlimited.speculativeInstructions, unlimited.trackSpeculativeInstructions)
	}
	if stats.numericSlotInstructions == 0 {
		t.Fatalf("unlimited observed execution recorded no instructions: %#v", stats)
	}

	limited, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	stats = &executionPathStats{}
	_, runErr := executeProto(context.Background(), proto, nil, executeOptions{
		controller: limited,
		mode:       executionModeNumericSlot,
		stats:      stats,
	})
	var limitErr *LimitError
	if !errors.As(runErr, &limitErr) || limitErr.Kind != LimitInstructions {
		t.Fatalf("limited observed execution error = %v, want instruction LimitError", runErr)
	}
	if stats.numericSlotInstructions != 1 || limited.remaining != 0 {
		t.Fatalf("limited observed execution stats/controller = (%#v, %d), want one charged instruction", stats, limited.remaining)
	}
}

func TestExecutionStatsPreserveCancellationAndRestoreController(t *testing.T) {
	proto, err := Compile("while true do end")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	controller, err := newExecutionController(ctx, ExecutionLimits{})
	if err != nil {
		t.Fatalf("newExecutionController returned error: %v", err)
	}
	_, runErr := executeProto(ctx, proto, nil, executeOptions{
		controller: controller,
		mode:       executionModeDirect,
		stats:      &executionPathStats{},
	})
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("cancelled observed execution error = %v, want context cancellation", runErr)
	}
	if controller.remaining != -1 || controller.limits.MaxInstructions != 0 || controller.speculativeInstructions != 0 || controller.trackSpeculativeInstructions {
		t.Fatalf("cancelled controller was not restored: %#v", controller)
	}
}

func TestForcedOwnerAndRuntimeMismatchModesDoNotFallback(t *testing.T) {
	owner := newRuntimeOwner()
	defer func() {
		if err := owner.close(); err != nil {
			t.Fatalf("close owner: %v", err)
		}
	}()
	proto, err := Compile("return 1 + 2")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	stats := &executionPathStats{}
	values, err := executeProto(context.Background(), proto, runtimeGlobalsWithOwner(nil, owner), executeOptions{
		mode:  executionModeNumericSlot,
		stats: stats,
	})
	if err != nil || len(values) != 1 || values[0] != NumberValue(3) || stats.numericSlotInstructions == 0 {
		t.Fatalf("forced owner numeric execution = (%#v, %v, %#v), want numeric [3]", values, err, stats)
	}

	mismatch := newProto(
		nil,
		[]instruction{{op: opAdd, a: 0, b: 0, c: 1}, {op: opReturnOne, a: 0}},
		nil, nil, 2, 2, false,
	)
	_, err = executeProto(context.Background(), mismatch, nil, executeOptions{
		args:  []Value{StringValue("not a number"), NumberValue(2)},
		mode:  executionModeNumericSlot,
		stats: &executionPathStats{},
	})
	if err == nil || !strings.Contains(err.Error(), "forced numeric slot execution unsupported") {
		t.Fatalf("forced runtime mismatch error = %v, want explicit unsupported error", err)
	}
}

func TestAutoExecutionStatsKeepSpeculativeFallbackWork(t *testing.T) {
	const signalingNaN = uint64(0x7ff0_0000_0000_0001)
	proto := newProto(
		[]Value{
			NumberValue(math.Float64frombits(signalingNaN)),
			NumberValue(math.Float64frombits(signalingNaN)),
		},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opLoadConst, a: 1, b: 1},
			{op: opAdd, a: 0, b: 0, c: 1},
			{op: opReturnOne, a: 0},
		},
		nil, nil, 2, 0, false,
	)
	stats := &executionPathStats{}
	values, err := executeProto(context.Background(), proto, nil, executeOptions{stats: stats})
	if err != nil || len(values) != 1 {
		t.Fatalf("speculative fallback result = (%#v, %v), want one NaN", values, err)
	}
	if stats.numericSlotInstructions == 0 || stats.slotInstructions == 0 || stats.directProductionInstructions == 0 {
		t.Fatalf("speculative and final paths were not all counted: %#v (numeric=%t slot=%t)", stats, proto.slotExecutionNumeric, proto.slotExecutionEligible)
	}
	if stats.fallbacks[executionFallbackNumericUnsupported] == 0 || stats.fallbacks[executionFallbackSlotUnsupported] == 0 {
		t.Fatalf("speculative fallback reasons were not recorded: %#v", stats.fallbacks)
	}
}
