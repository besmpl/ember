package ember

import (
	"reflect"
	"testing"
)

func TestOpcodeEffectsCoverEveryOpcode(t *testing.T) {
	for _, op := range allOpcodes {
		if effect := opcodeEffect(op); !effect.classified {
			t.Fatalf("opcode effect for %s (%d) is not classified", opcodeName(op), op)
		}
	}

	for _, op := range []opcode{0, 7, 67, opcodeLimit, opcode(^uint8(0))} {
		if effect := opcodeEffect(op); effect != (opcodeEffects{}) {
			t.Fatalf("invalid opcode %d has effects %#v, want unclassified zero value", op, effect)
		}
	}
}

func TestMetamethodCapableOpcodeEffects(t *testing.T) {
	callbackEffects := opcodeEffects{
		classified:                  true,
		invokesScriptOrHostCode:     true,
		mayYield:                    true,
		mayError:                    true,
		allocatesOrObservesIdentity: true,
		readsGlobals:                true,
		writesGlobals:               true,
		readsUpvalues:               true,
		writesUpvalues:              true,
		readsTables:                 true,
		writesTables:                true,
		readsUnknownHeap:            true,
		writesUnknownHeap:           true,
	}
	callbackGroups := []struct {
		name string
		ops  []opcode
	}{
		{
			name: "table reads writes and iteration",
			ops: []opcode{
				opSetField, opGetStringField, opSetStringField,
				opGetStringFieldIndex, opSetStringFieldIndex, opAddStringField, opSubStringField,
				opGetIndex, opSetIndex, opPrepareIter, opArrayNext, opArrayNextJump2,
			},
		},
		{
			name: "arithmetic",
			ops: []opcode{
				opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg,
			},
		},
		{
			name: "constant arithmetic",
			ops: []opcode{
				opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
			},
		},
		{name: "length", ops: []opcode{opLen}},
		{name: "concatenation", ops: []opcode{opConcat, opConcatChain}},
		{
			name: "comparisons",
			ops: []opcode{
				opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual,
			},
		},
		{
			name: "comparison branches",
			ops: []opcode{
				opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK,
				opJumpIfLessK, opJumpIfGreaterK, opJumpIfNotLess, opJumpIfNotGreater,
				opJumpIfLess, opJumpIfGreater, opJumpIfModKNotEqualK,
				opJumpIfStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK,
				opJumpIfStringFieldGreaterK, opJumpIfStringFieldNotGreaterR,
			},
		},
		{
			name: "script and host calls",
			ops: []opcode{
				opFastCall, opCall, opCallOne,
				opCallLocalOne, opCallUpvalueOne, opCallMethodOne,
			},
		},
	}

	covered := make(map[opcode]string, opcodeCount)
	for _, group := range callbackGroups {
		t.Run(group.name, func(t *testing.T) {
			for _, op := range group.ops {
				if previous, ok := covered[op]; ok {
					t.Fatalf("%s appears in both %q and %q", opcodeName(op), previous, group.name)
				}
				covered[op] = group.name
				if got := opcodeEffect(op); got != callbackEffects {
					t.Errorf("%s effects are %#v, want callback effects %#v", opcodeName(op), got, callbackEffects)
				}
			}
		})
	}

	directCases := []struct {
		name string
		ops  []opcode
		want opcodeEffects
	}{
		{name: "read global", ops: []opcode{opLoadGlobal}, want: opcodeEffects{classified: true, readsGlobals: true}},
		{name: "write global", ops: []opcode{opSetGlobal}, want: opcodeEffects{classified: true, writesGlobals: true}},
		{name: "read upvalue", ops: []opcode{opGetUpvalue}, want: opcodeEffects{classified: true, readsUpvalues: true}},
		{name: "write upvalue", ops: []opcode{opSetUpvalue}, want: opcodeEffects{classified: true, writesUpvalues: true}},
		{name: "allocate table", ops: []opcode{opNewTable}, want: opcodeEffects{classified: true, allocatesOrObservesIdentity: true}},
		{
			name: "allocate closure with upvalues",
			ops:  []opcode{opClosure},
			want: opcodeEffects{classified: true, allocatesOrObservesIdentity: true, readsUpvalues: true},
		},
		{name: "allocate varargs", ops: []opcode{opVararg}, want: opcodeEffects{classified: true, allocatesOrObservesIdentity: true}},
		{name: "numeric for check may error", ops: []opcode{opNumericForCheck}, want: opcodeEffects{classified: true, mayError: true}},
		{name: "metatable guard reads table", ops: []opcode{opJumpIfTableHasMetatable}, want: opcodeEffects{classified: true, readsTables: true}},
		{
			name: "otherwise pure",
			ops: []opcode{
				opLoadConst, opMove, opNumericForLoop,
				opJumpIfFalse, opJump, opReturnOne, opReturn,
			},
			want: opcodeEffects{classified: true},
		},
	}

	for _, tc := range directCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, op := range tc.ops {
				if previous, ok := covered[op]; ok {
					t.Fatalf("%s appears in both %q and %q", opcodeName(op), previous, tc.name)
				}
				covered[op] = tc.name
				if got := opcodeEffect(op); got != tc.want {
					t.Errorf("%s effects are %#v, want %#v", opcodeName(op), got, tc.want)
				}
			}
		})
	}
	for _, op := range allOpcodes {
		if _, ok := covered[op]; !ok {
			t.Errorf("%s is missing from the exact callback/direct effect groups", opcodeName(op))
		}
	}
}

func TestOpcodeEffectsRejectYieldWithoutInvocation(t *testing.T) {
	table := opcodeMetadataTable
	table[opAdd].effects.invokesScriptOrHostCode = false
	if err := validateOpcodeMetadataTable(table); err == nil {
		t.Fatal("validateOpcodeMetadataTable accepted an opcode that may yield without invoking code")
	}
}

func TestLoopInvariantLoadTreatsMetamethodOperationsAsBarriers(t *testing.T) {
	tests := []struct {
		name string
		body instruction
	}{
		{name: "arithmetic", body: instruction{op: opAdd, a: 5, b: 3, c: 4}},
		{name: "comparison", body: instruction{op: opLess, a: 5, b: 3, c: 4}},
		{name: "length", body: instruction{op: opLen, a: 5, b: 3}},
		{name: "concat", body: instruction{op: opConcat, a: 5, b: 3, c: 4}},
		{name: "table", body: instruction{op: opGetIndex, a: 5, b: 3, c: 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder bytecodeBuilder
			field := builder.addConstant(StringValue("value"))
			metatableFallback := builder.emit(instruction{op: opJumpIfTableHasMetatable, a: 0})
			loopStart := builder.pc()
			builder.emit(instruction{op: opGetStringField, a: 2, b: 0, c: field})
			builder.emit(tt.body)
			builder.emit(instruction{op: opJump, b: loopStart})
			fallback := builder.pc()
			builder.patchJump(metatableFallback, fallback)
			builder.emit(instruction{op: opReturnOne, a: 2})

			optimized := hoistBytecodeIRLoopInvariantHeaderLoads(builder.ir)
			code := assembleBytecodeIRRaw(optimized)
			backedge := code[fallback-1]
			if backedge.op != opJump {
				t.Fatalf("backedge opcode is %s, want JUMP", opcodeName(backedge.op))
			}
			if backedge.b != loopStart {
				t.Fatalf("backedge target is %d, want guarded header load at %d", backedge.b, loopStart)
			}
		})
	}
}

func TestLoopInvariantFieldLoadObservesArithmeticMetamethodMutation(t *testing.T) {
	assertPeepholeVariantsReturnNumber(t, `
local state = {value = 1}
local operand = setmetatable({}, {
 __add = function()
  state.value = state.value + 1
  return 0
 end,
})
local total = 0
for i = 1, 2 do
 total = total + state.value
 local ignored = operand + 0
end
return total
`, 3)
}

func TestLoopInvariantFieldLoadObservesIndexMetamethodMutation(t *testing.T) {
	assertPeepholeVariantsReturnNumber(t, `
local state = {value = 1}
local proxy = setmetatable({}, {
 __index = function()
  state.value = state.value + 1
  return 0
 end,
})
local total = 0
for i = 1, 2 do
 total = total + state.value
 local ignored = proxy.missing
end
return total
`, 3)
}

func assertPeepholeVariantsReturnNumber(t *testing.T, source string, want float64) {
	t.Helper()
	optimized, err := Compile(source)
	if err != nil {
		t.Fatalf("optimized Compile returned error: %v", err)
	}

	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	disabled, err := compileProgramWithOptions(artifact, compilerOptions{
		optimizations: optimizationOptions{
			disabledCategories: map[optimizationCategory]bool{
				optimizationBytecodePeephole: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("peephole-disabled Compile returned error: %v", err)
	}

	optimizedResults, optimizedErr := Run(optimized)
	disabledResults, disabledErr := Run(disabled)
	if !equalTestErrors(optimizedErr, disabledErr) {
		t.Fatalf("optimized Run error is %v, peephole-disabled Run error is %v", optimizedErr, disabledErr)
	}
	if optimizedErr != nil {
		t.Fatalf("Run returned error: %v", optimizedErr)
	}
	if !reflect.DeepEqual(optimizedResults, disabledResults) {
		t.Fatalf("optimized Run results are %#v, want peephole-disabled results %#v", optimizedResults, disabledResults)
	}
	if len(optimizedResults) != 1 {
		t.Fatalf("Run returned %d results, want 1: %#v", len(optimizedResults), optimizedResults)
	}
	got, ok := optimizedResults[0].Number()
	if !ok || got != want {
		t.Fatalf("Run result is %v (%t), want number %v", optimizedResults[0], ok, want)
	}
}

func equalTestErrors(left error, right error) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Error() == right.Error()
}
