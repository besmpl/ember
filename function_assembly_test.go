package ember

import (
	"reflect"
	"testing"
)

var functionAssemblyProtoSink *Proto

func TestFunctionAssemblyOwnsCodeMappingLinesAndPackedCode(t *testing.T) {
	source := "local value = 1\nvalue = value + 1\nreturn value\n"
	ir := []bytecodeIRInstruction{
		lowerInstructionToBytecodeIR(
			instruction{op: opLoadConst, a: 0, b: 0},
			sourceRange{start: 0, end: 15},
		),
		lowerInstructionToBytecodeIR(
			instruction{op: opJump, b: 2},
			sourceRange{start: 16, end: 33},
		),
		lowerInstructionToBytecodeIR(
			instruction{op: opReturnOne, a: 0},
			sourceRange{start: 34, end: 46},
		),
	}

	assembly := assembleFunctionBytecode(newSourceLineMap(source), ir)
	if err := assembly.pack(); err != nil {
		t.Fatalf("assembly.pack returned error: %v", err)
	}

	wantCode := []instruction{
		{op: opLoadConst, a: 0, b: 0},
		{op: opReturnOne, a: 0},
	}
	if !reflect.DeepEqual(assembly.code, wantCode) {
		t.Fatalf("assembled code is %#v, want %#v", assembly.code, wantCode)
	}
	if want := []int{0, 1, 1, 2}; !reflect.DeepEqual(assembly.oldToNew, want) {
		t.Fatalf("old-to-new PC map is %#v, want %#v", assembly.oldToNew, want)
	}
	if want := []sourceRange{{start: 0, end: 15}, {start: 34, end: 46}}; !reflect.DeepEqual(assembly.sources, want) {
		t.Fatalf("source anchors are %#v, want %#v", assembly.sources, want)
	}
	if want := []int{1, 3}; !reflect.DeepEqual(assembly.lines, want) {
		t.Fatalf("source lines are %#v, want %#v", assembly.lines, want)
	}
	if len(assembly.packedCode) != len(assembly.code) {
		t.Fatalf("packed code has %d instructions for %d executable instructions", len(assembly.packedCode), len(assembly.code))
	}
	for pc := range assembly.code {
		if got := assembly.packedCode[pc].unpack(); got != assembly.code[pc] {
			t.Fatalf("packed instruction %d is %#v, want %#v", pc, got, assembly.code[pc])
		}
	}
}

func TestSourceLineMapMatchesSourceRangeLines(t *testing.T) {
	source := "first\nsecond\nthird"
	lines := newSourceLineMap(source)
	tests := []struct {
		span sourceRange
		want int
	}{
		{span: sourceRange{start: 0, end: 5}, want: 1},
		{span: sourceRange{start: 6, end: 12}, want: 2},
		{span: sourceRange{start: 13, end: 18}, want: 3},
		{span: sourceRange{}, want: -1},
		{span: sourceRange{start: -1, end: 1}, want: -1},
		{span: sourceRange{start: len(source), end: len(source) + 1}, want: -1},
	}

	for _, test := range tests {
		if got := lines.line(test.span); got != test.want {
			t.Errorf("line(%#v) = %d, want %d", test.span, got, test.want)
		}
	}
}

func TestCompileFinalAssemblyAllocationBudget(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	tests := []struct {
		name      string
		source    string
		maxAllocs int
	}{
		{
			name: "tiny_arithmetic",
			source: `local x = 1
local y = 2
return (x + y) * 3 - 4 / 2`,
			maxAllocs: 158,
		},
		{
			name: "closure_upvalue",
			source: `local base = 4
local function add(x)
    return base + x
end
return add(3)`,
			maxAllocs: 205,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(25, func() {
				proto, err := Compile(test.source)
				if err != nil {
					t.Fatalf("Compile returned error: %v", err)
				}
				functionAssemblyProtoSink = proto
			})
			if allocs > float64(test.maxAllocs) {
				t.Fatalf("Compile used %.0f allocs/op, want at most %d", allocs, test.maxAllocs)
			}
		})
	}
}
