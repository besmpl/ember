package ember

import (
	"reflect"
	"testing"
)

func TestCompilerComplexityBudgets(t *testing.T) {
	tests := []struct {
		name                  string
		source                string
		globals               map[string]Value
		want                  []Value
		maxInstructions       int
		maxConstants          int
		maxRegisterSlots      int
		wantChildProtos       int
		maxPackedInstructions int64
	}{
		{
			name: "branch_dense",
			source: `local x = 1
if flag then
 x = x + 2
else
 x = x + 3
end
return x`,
			globals:               map[string]Value{"flag": BoolValue(false)},
			want:                  []Value{NumberValue(4)},
			maxInstructions:       7,
			maxConstants:          4,
			maxRegisterSlots:      2,
			wantChildProtos:       0,
			maxPackedInstructions: 7,
		},
		{
			name: "closure_upvalue",
			source: `local base=4
local function add(x) return base+x end
return add(3)`,
			want:                  []Value{NumberValue(7)},
			maxInstructions:       9,
			maxConstants:          2,
			maxRegisterSlots:      7,
			wantChildProtos:       1,
			maxPackedInstructions: 9,
		},
		{
			name: "vararg_multi_return",
			source: `local function collect(...) local a,b=... return a,b,select("#",...) end
return collect(1,2,3)`,
			want:                  []Value{NumberValue(1), NumberValue(2), NumberValue(3)},
			maxInstructions:       11,
			maxConstants:          3,
			maxRegisterSlots:      10,
			wantChildProtos:       1,
			maxPackedInstructions: 11,
		},
		{
			name: "table_string_fields",
			source: `local value={name="ember",hp=10}
value.hp=value.hp+5
return value.name,value.hp`,
			want:                  []Value{StringValue("ember"), NumberValue(15)},
			maxInstructions:       10,
			maxConstants:          6,
			maxRegisterSlots:      4,
			wantChildProtos:       0,
			maxPackedInstructions: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto, err := Compile(tt.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			results, err := RunWithGlobals(proto, tt.globals)
			if err != nil {
				t.Fatalf("RunWithGlobals returned error: %v", err)
			}
			assertCompilerComplexityResults(t, results, tt.want)

			metrics := CompilerBenchmarkMetricsForTest(proto)
			if metrics.Instructions > tt.maxInstructions {
				t.Fatalf("%s has %d instructions, want at most %d", tt.name, metrics.Instructions, tt.maxInstructions)
			}
			if metrics.Constants > tt.maxConstants {
				t.Fatalf("%s has %d constants, want at most %d", tt.name, metrics.Constants, tt.maxConstants)
			}
			if metrics.RegisterSlots > tt.maxRegisterSlots {
				t.Fatalf("%s has %d register slots, want at most %d", tt.name, metrics.RegisterSlots, tt.maxRegisterSlots)
			}
			if metrics.ChildProtos != tt.wantChildProtos {
				t.Fatalf("%s has %d child protos, want %d", tt.name, metrics.ChildProtos, tt.wantChildProtos)
			}
			packedInstructionBytes := int64(reflect.TypeOf(packedInstruction{}).Size())
			if got := metrics.PackedBytes / packedInstructionBytes; got > tt.maxPackedInstructions {
				t.Fatalf("%s has %d packed instructions, want at most %d", tt.name, got, tt.maxPackedInstructions)
			}
		})
	}
}

func assertCompilerComplexityResults(t *testing.T, got []Value, want []Value) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Run results have length %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index].Kind() != want[index].Kind() {
			t.Fatalf("Run result %d has kind %s, want %s", index, got[index].Kind(), want[index].Kind())
		}
		switch want[index].Kind() {
		case NumberKind:
			gotNumber, _ := got[index].Number()
			wantNumber, _ := want[index].Number()
			if gotNumber != wantNumber {
				t.Fatalf("Run result %d is number %v, want %v", index, gotNumber, wantNumber)
			}
		case StringKind:
			gotString, _ := got[index].String()
			wantString, _ := want[index].String()
			if gotString != wantString {
				t.Fatalf("Run result %d is string %q, want %q", index, gotString, wantString)
			}
		default:
			t.Fatalf("Run result %d uses unsupported expected kind %s", index, want[index].Kind())
		}
	}
}
