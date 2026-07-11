package ember

import (
	"strings"
	"testing"
)

var parserIdentifierStatementProtoSink *Proto

func TestCompilePlainAssignmentsAllocationBudget(t *testing.T) {
	source := plainAssignmentSource(1000)
	const maxAllocs = 7300

	allocs := testing.AllocsPerRun(10, func() {
		proto, err := Compile(source)
		if err != nil {
			t.Fatalf("Compile returned error: %v", err)
		}
		parserIdentifierStatementProtoSink = proto
	})
	if allocs > maxAllocs {
		t.Fatalf("plain assignment Compile used %.0f allocs/op, want at most %d after eliminating one speculative allocation per assignment", allocs, maxAllocs)
	}
}

func TestCompileRunIdentifierStatementAssignments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []float64
	}{
		{
			name: "plain assignment",
			source: `local value = 1
value = value + 2
return value`,
			want: []float64{3},
		},
		{
			name: "multiple assignment",
			source: `local first, second = 1, 2
first, second = second, first
return first, second`,
			want: []float64{2, 1},
		},
		{
			name: "computed selector evaluation order",
			source: `local values = {}
local calls = 0
local function key()
    calls = calls + 1
    return calls
end
values[key()], values[key()] = 10, 20
return values[1], values[2], calls`,
			want: []float64{10, 20, 2},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results := compileRunIdentifierStatementSource(t, test.source)
			if len(results) != len(test.want) {
				t.Fatalf("Run returned %d results, want %d", len(results), len(test.want))
			}
			for index, want := range test.want {
				got, ok := results[index].Number()
				if !ok || got != want {
					t.Fatalf("result %d is %v (%t), want %v", index, results[index], ok, want)
				}
			}
		})
	}
}

func TestCompileRunIdentifierStatementCalls(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   float64
	}{
		{
			name: "direct call statement",
			source: `local total = 0
local function add(value)
    total = total + value
end
add(2)
return total`,
			want: 2,
		},
		{
			name: "method call statement",
			source: `local value = {amount = 1}
function value:add(delta)
    self.amount = self.amount + delta
end
value:add(2)
return value.amount`,
			want: 3,
		},
		{
			name: "chained method call statement",
			source: `local value = {nested = {amount = 1}}
function value.nested:add(delta)
    self.amount = self.amount + delta
			end
			value.nested:add(2)
			return value.nested.amount`,
			want: 3,
		},
		{
			name: "call then chained method statement",
			source: `local holder = {nested = {amount = 1}}
local function make()
    return holder
end
function holder.nested:add(delta)
    self.amount = self.amount + delta
end
make().nested:add(2)
return holder.nested.amount`,
			want: 3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results := compileRunIdentifierStatementSource(t, test.source)
			if len(results) != 1 {
				t.Fatalf("Run returned %d results, want 1", len(results))
			}
			got, ok := results[0].Number()
			if !ok || got != test.want {
				t.Fatalf("Run result is %v (%t), want %v", results[0], ok, test.want)
			}
		})
	}
}

func TestCompileIdentifierStatementErrorsKeepLocations(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "missing assignment equals", source: "value", want: "compile: byte 5: expected ="},
		{name: "power is not an assignment target", source: "value ^ 2", want: "compile: byte 6: expected ="},
		{name: "cast is not an assignment target", source: "value :: number", want: "compile: byte 6: expected ="},
		{name: "missing call close", source: "value(1", want: "compile: byte 7: expected , or )"},
		{name: "missing computed selector close", source: "value[a", want: "compile: byte 7: expected ]"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Compile(test.source)
			if err == nil {
				t.Fatal("Compile succeeded, want error")
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("Compile error is %q, want %q", got, test.want)
			}
		})
	}
}

func compileRunIdentifierStatementSource(t *testing.T, source string) []Value {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return results
}

func plainAssignmentSource(lines int) string {
	var source strings.Builder
	source.WriteString("local value = 0\n")
	for i := 0; i < lines; i++ {
		source.WriteString("value = value + 1\n")
	}
	source.WriteString("return value\n")
	return source.String()
}
