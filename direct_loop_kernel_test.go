package ember

import "testing"

func TestDirectLoopKernelOpcodeKeepsEffectfulBoundariesCanonical(t *testing.T) {
	for _, op := range []opcode{
		opLoadConst,
		opMove,
		opSetField,
		opArrayNext,
		opArrayNextJump2,
		opGetStringField,
		opSetStringField,
		opGetStringFieldIndex,
		opSetStringFieldIndex,
		opGetIndex,
		opSetIndex,
		opAdd,
		opSub,
		opMul,
		opDiv,
		opMod,
		opIDiv,
		opAddK,
		opSubK,
		opMulK,
		opDivK,
		opModK,
		opIDivK,
		opNeg,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opJumpIfTableHasMetatable,
		opJumpIfFalse,
		opJump,
	} {
		if !directLoopKernelOpcode(op) {
			t.Fatalf("%s is not eligible for the direct loop kernel", opcodeName(op))
		}
	}
	for _, op := range []opcode{
		opLoadGlobal,
		opSetGlobal,
		opNewTable,
		opClosure,
		opVararg,
		opFastCall,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallMethodOne,
		opReturnOne,
		opReturn,
	} {
		if directLoopKernelOpcode(op) {
			t.Fatalf("%s must remain in the canonical frame runner", opcodeName(op))
		}
	}
}

func TestDirectLoopKernelPreservesDenseTableLoopBehavior(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{cooldown = 2},
	{cooldown = 0},
	{cooldown = 3},
}
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
		t.Fatal("dense table loop has no direct loop kernel entry")
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want one", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("Run result = %v (%t), want number 3", got, ok)
	}

	instrumented := newVMThread(runtimeGlobals(nil))
	instrumented.directFrameInstrumented = true
	instrumentedResults, err := instrumented.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("instrumented run returned error: %v", err)
	}
	if len(instrumentedResults) != 1 {
		t.Fatalf("instrumented run returned %d results, want one", len(instrumentedResults))
	}
	instrumentedValue, ok := instrumentedResults[0].Number()
	if !ok || instrumentedValue != 3 {
		t.Fatalf("instrumented result = %v (%t), want number 3", instrumentedValue, ok)
	}
}

func TestDirectLoopKernelPreservesChainedDynamicIndexOperands(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{stats = {hp = 1}},
	{stats = {hp = 2}},
	{stats = {hp = 3}},
}
local key = "hp"
local total = 0
for _, row in rows do
	total = total + row.stats[key]
	total = total + row.stats[key]
	total = total + row.stats[key]
	total = total + row.stats[key]
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.directLoopKernels == nil || len(proto.directLoopKernels.kernels) == 0 {
		t.Fatal("dynamic index loop has no direct loop kernel entry")
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want one", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 24 {
		t.Fatalf("Run result = %v (%t), want number 24", got, ok)
	}
}

func TestScenarioDirectLoopKernelCoverage(t *testing.T) {
	cases := loadScenarioBenchmarkCases(t, []string{
		"ability_resolution",
		"cooldown_scheduler",
		"dialogue_condition_eval",
		"formation_layout_score",
		"inventory_value",
		"path_relaxation",
		"projectile_sweep",
		"signal_bus_callbacks",
		"sparse_grid_neighbors",
		"threat_aggro_table",
	})
	covered := 0
	for _, tc := range cases {
		proto, err := Compile(tc.source)
		if err != nil {
			t.Fatalf("Compile %s returned error: %v", tc.name, err)
		}
		kernels := 0
		ops := 0
		if proto.directLoopKernels != nil {
			kernels = len(proto.directLoopKernels.kernels)
			for _, kernel := range proto.directLoopKernels.kernels {
				ops += len(kernel.ops)
			}
		}
		if kernels != 0 {
			covered++
		}
		t.Logf("%s kernels=%d ops=%d", tc.name, kernels, ops)
	}
	if covered < 5 {
		t.Fatalf("covered scenarios = %d, want at least 5", covered)
	}
}

func TestDirectLoopKernelPreservesChainedDynamicIndexStores(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{stats = {hp = 1}},
	{stats = {hp = 2}},
	{stats = {hp = 3}},
}
local key = "hp"
local total = 0
for _, row in rows do
	row.stats[key] = row.stats[key] + 1
	row.stats[key] = row.stats[key] + 1
	row.stats[key] = row.stats[key] + 1
	row.stats[key] = row.stats[key] + 1
	total = total + row.stats[key]
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.directLoopKernels == nil || len(proto.directLoopKernels.kernels) == 0 {
		t.Fatal("dynamic index store loop has no direct loop kernel entry")
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want one", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 18 {
		t.Fatalf("Run result = %v (%t), want number 18", got, ok)
	}
}

func TestDirectLoopKernelRejectsTinyRegions(t *testing.T) {
	proto, err := Compile(`
local total = 0
for _, value in {1, 2, 3, 4, 5, 6} do
	total = total + value
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.directLoopKernels != nil {
		t.Fatal("tiny array loop unexpectedly received a direct loop kernel")
	}
}
