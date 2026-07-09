package ember

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var compilerComplexityProtoSink *Proto

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
			source: `local base = 4
local function add(x)
    return base + x
end
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
			source: `local function collect(...)
    local a, b = ...
    return a, b, select("#", ...)
end
return collect(1, 2, 3)`,
			want:                  []Value{NumberValue(1), NumberValue(2), NumberValue(3)},
			maxInstructions:       11,
			maxConstants:          3,
			maxRegisterSlots:      10,
			wantChildProtos:       1,
			maxPackedInstructions: 11,
		},
		{
			name: "table_string_fields",
			source: `local value = {name = "ember", hp = 10}
value.hp = value.hp + 5
return value.name, value.hp`,
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

func TestCompileNestedClosuresAllocationBudget(t *testing.T) {
	source := nestedClosureCompileSource(12)
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 2 {
		t.Fatalf("Run result is %v (%t), want number 2", got, ok)
	}

	const maxAllocsPerCompile = 3800
	allocs := testing.AllocsPerRun(25, func() {
		compiled, err := Compile(source)
		if err != nil {
			t.Fatalf("Compile returned error: %v", err)
		}
		compilerComplexityProtoSink = compiled
	})
	if allocs > maxAllocsPerCompile {
		t.Fatalf("nested closure Compile used %.0f allocs/op, want at most %d", allocs, maxAllocsPerCompile)
	}
}

func TestCompileNestedClosurePreservesChildMetadata(t *testing.T) {
	proto, err := Compile(`local function read(row)
    return row.hp
end
return read({hp = 7})`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled root has %d child prototypes, want 1", len(proto.prototypes))
	}
	child := proto.prototypes[0]
	if len(child.lines) != len(child.code) {
		t.Fatalf("child line table has %d entries for %d instructions", len(child.lines), len(child.code))
	}
	if symbol := constantStringSymbolFor(t, child, "hp"); symbol == 0 {
		t.Fatal("child field name symbol is zero, want interned symbol")
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want number 7", got, ok)
	}
}

func TestCompileNestedClosuresPreservesParentUpvalues(t *testing.T) {
	proto, err := Compile(`local base = 4
local function outer(x)
    local function middle(y)
        local function inner(z)
            return base + x + y + z
        end
        return inner(3)
    end
    return middle(2)
end
return outer(1)`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 10 {
		t.Fatalf("Run result is %v (%t), want number 10", got, ok)
	}
}

func nestedClosureCompileSource(depth int) string {
	var source strings.Builder
	for index := range depth {
		source.WriteString(strings.Repeat("    ", index))
		source.WriteString("local function f")
		source.WriteString(strconv.Itoa(index))
		source.WriteString("(x)\n")
	}
	source.WriteString(strings.Repeat("    ", depth))
	source.WriteString("return x + 1\n")
	for index := depth - 1; index >= 0; index-- {
		source.WriteString(strings.Repeat("    ", index))
		source.WriteString("end\n")
		source.WriteString(strings.Repeat("    ", index))
		source.WriteString("return f")
		source.WriteString(strconv.Itoa(index))
		if index == 0 {
			source.WriteString("(1)\n")
		} else {
			source.WriteString("(x)\n")
		}
	}
	return source.String()
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
