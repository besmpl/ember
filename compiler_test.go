package ember_test

import (
	"math"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func TestCompileAndRunReturnAddition(t *testing.T) {
	proto, err := ember.Compile("return 1 + 2")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}

	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func compileAndRunNumber(t *testing.T, source string) float64 {
	t.Helper()

	result := compileAndRunValue(t, source)
	got, ok := result.Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", result.Kind())
	}

	return got
}

func compileAndRunValue(t *testing.T, source string) ember.Value {
	t.Helper()

	proto, err := ember.Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}

	return results[0]
}

func TestCompileAndRunScalarLiterals(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		got := compileAndRunValue(t, "return nil")
		if !got.IsNil() {
			t.Fatalf("Run result is %s, want nil", got.Kind())
		}
	})

	t.Run("true", func(t *testing.T) {
		got := compileAndRunValue(t, "return true")
		value, ok := got.Bool()
		if !ok {
			t.Fatalf("Run result is %s, want boolean", got.Kind())
		}
		if value != true {
			t.Fatalf("Run result is %v, want true", value)
		}
	})

	t.Run("false", func(t *testing.T) {
		got := compileAndRunValue(t, "return false")
		value, ok := got.Bool()
		if !ok {
			t.Fatalf("Run result is %s, want boolean", got.Kind())
		}
		if value != false {
			t.Fatalf("Run result is %v, want false", value)
		}
	})

	t.Run("string", func(t *testing.T) {
		got := compileAndRunValue(t, `return "ember"`)
		value, ok := got.String()
		if !ok {
			t.Fatalf("Run result is %s, want string", got.Kind())
		}
		if value != "ember" {
			t.Fatalf("Run result is %q, want ember", value)
		}
	})
}

func TestCompileAndRunScalarLocalBindings(t *testing.T) {
	t.Run("nil local", func(t *testing.T) {
		got := compileAndRunValue(t, `
local missing = nil
return missing
`)
		if !got.IsNil() {
			t.Fatalf("Run result is %s, want nil", got.Kind())
		}
	})

	t.Run("string local", func(t *testing.T) {
		got := compileAndRunValue(t, `
local label = "ember"
return label
`)
		value, ok := got.String()
		if !ok {
			t.Fatalf("Run result is %s, want string", got.Kind())
		}
		if value != "ember" {
			t.Fatalf("Run result is %q, want ember", value)
		}
	})

	t.Run("boolean local", func(t *testing.T) {
		got := compileAndRunValue(t, `
local enabled = false
return enabled
`)
		value, ok := got.Bool()
		if !ok {
			t.Fatalf("Run result is %s, want boolean", got.Kind())
		}
		if value != false {
			t.Fatalf("Run result is %v, want false", value)
		}
	})

	t.Run("escaped string", func(t *testing.T) {
		got := compileAndRunValue(t, `return "a\n\"b\""`)
		value, ok := got.String()
		if !ok {
			t.Fatalf("Run result is %s, want string", got.Kind())
		}
		if value != "a\n\"b\"" {
			t.Fatalf("Run result is %q, want escaped string", value)
		}
	})
}

func TestCompileAndRunHostFunctionCall(t *testing.T) {
	proto, err := ember.Compile("return add(1, 2)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"add": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			if len(args) != 2 {
				t.Fatalf("host function received %d args, want 2", len(args))
			}
			left, ok := args[0].Number()
			if !ok {
				t.Fatalf("left arg is %s, want number", args[0].Kind())
			}
			right, ok := args[1].Number()
			if !ok {
				t.Fatalf("right arg is %s, want number", args[1].Kind())
			}
			return []ember.Value{ember.NumberValue(left + right)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunHostFunctionCallWithLocalArguments(t *testing.T) {
	proto, err := ember.Compile(`
local left = 2
local right = 3
return add(left, right)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"add": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			left, _ := args[0].Number()
			right, _ := args[1].Number()
			return []ember.Value{ember.NumberValue(left + right)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunHostFunctionWithNoResults(t *testing.T) {
	proto, err := ember.Compile("return noop()")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"noop": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	if !results[0].IsNil() {
		t.Fatalf("Run result is %s, want nil", results[0].Kind())
	}
}

func TestCompileAndRunTableFieldReads(t *testing.T) {
	got := compileAndRunNumber(t, `
local point = {x = 2, y = 3}
return point.x + point.y
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunTableScalarFields(t *testing.T) {
	t.Run("string field", func(t *testing.T) {
		got := compileAndRunValue(t, `
local item = {name = "ember",}
return item.name
`)
		value, ok := got.String()
		if !ok {
			t.Fatalf("Run result is %s, want string", got.Kind())
		}
		if value != "ember" {
			t.Fatalf("Run result is %q, want ember", value)
		}
	})

	t.Run("missing field", func(t *testing.T) {
		got := compileAndRunValue(t, `
local item = {name = "ember"}
return item.missing
`)
		if !got.IsNil() {
			t.Fatalf("Run result is %s, want nil", got.Kind())
		}
	})
}

func TestCompileAndRunTableFieldAssignment(t *testing.T) {
	got := compileAndRunNumber(t, `
local point = {x = 2, y = 3}
point.x = point.x + point.y
return point.x
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunArrayTableIndexReads(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {10, 20, 30}
return values[1] + values[2]
`)
	if got != 30 {
		t.Fatalf("Run result is %v, want 30", got)
	}
}

func TestCompileAndRunMixedTableArrayIndexReads(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {name = "scores", 10, 20,}
return values[1] + values[2]
`)
	if got != 30 {
		t.Fatalf("Run result is %v, want 30", got)
	}
}

func TestCompileAndRunTableBracketReads(t *testing.T) {
	t.Run("string key", func(t *testing.T) {
		got := compileAndRunValue(t, `
local item = {name = "ember"}
return item["name"]
`)
		value, ok := got.String()
		if !ok {
			t.Fatalf("Run result is %s, want string", got.Kind())
		}
		if value != "ember" {
			t.Fatalf("Run result is %q, want ember", value)
		}
	})

	t.Run("missing array index", func(t *testing.T) {
		got := compileAndRunValue(t, `
local values = {10}
return values[2]
`)
		if !got.IsNil() {
			t.Fatalf("Run result is %s, want nil", got.Kind())
		}
	})
}

func TestCompileAndRunTableBracketAssignment(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {10, 20}
values[2] = values[1] + 5
return values[2]
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestCompileAndRunTableBooleanKeys(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {}
values[true] = 7
values[false] = 11
return values[true] + values[false]
`)
	if got != 18 {
		t.Fatalf("Run result is %v, want 18", got)
	}
}

func TestCompileAndRunTableIdentityKeys(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {}
local key = {}
local other = {}
values[key] = 40
values[other] = 2
return values[key] + values[other]
`)
	if got != 42 {
		t.Fatalf("Run result is %v, want 42", got)
	}
}

func TestCompileAndRunTableFieldScalarAssignment(t *testing.T) {
	got := compileAndRunValue(t, `
local item = {name = "ember"}
item.name = "hearth"
return item.name
`)
	value, ok := got.String()
	if !ok {
		t.Fatalf("Run result is %s, want string", got.Kind())
	}
	if value != "hearth" {
		t.Fatalf("Run result is %q, want hearth", value)
	}
}

func TestRunWithGlobalsMutatesHostTable(t *testing.T) {
	player := ember.NewTable()
	if err := player.Set(ember.StringValue("hp"), ember.NumberValue(100)); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	proto, err := ember.Compile(`
player.hp = player.hp + 25
return player.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"player": ember.TableValue(player),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 125 {
		t.Fatalf("Run result is %v, want 125", got)
	}

	stored, err := player.Get(ember.StringValue("hp"))
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	storedHP, ok := stored.Number()
	if !ok {
		t.Fatalf("Stored hp is %s, want number", stored.Kind())
	}
	if storedHP != 125 {
		t.Fatalf("Stored hp is %v, want 125", storedHP)
	}
}

func TestRunReturnsScriptTableToHost(t *testing.T) {
	result := compileAndRunValue(t, `
local item = {name = "ember", 10}
return item
`)

	table, ok := result.Table()
	if !ok {
		t.Fatalf("Run result is %s, want table", result.Kind())
	}

	name, err := table.Get(ember.StringValue("name"))
	if err != nil {
		t.Fatalf("Get name returned error: %v", err)
	}
	gotName, ok := name.String()
	if !ok {
		t.Fatalf("name is %s, want string", name.Kind())
	}
	if gotName != "ember" {
		t.Fatalf("name is %q, want ember", gotName)
	}

	first, err := table.Get(ember.NumberValue(1))
	if err != nil {
		t.Fatalf("Get array value returned error: %v", err)
	}
	gotFirst, ok := first.Number()
	if !ok {
		t.Fatalf("array value is %s, want number", first.Kind())
	}
	if gotFirst != 10 {
		t.Fatalf("array value is %v, want 10", gotFirst)
	}
}

func TestTableNilAssignmentDeletesKey(t *testing.T) {
	item := ember.NewTable()
	if err := item.Set(ember.StringValue("name"), ember.StringValue("ember")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	proto, err := ember.Compile(`
item.name = nil
return item.name
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"item": ember.TableValue(item),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if !results[0].IsNil() {
		t.Fatalf("Run result is %s, want nil", results[0].Kind())
	}

	stored, err := item.Get(ember.StringValue("name"))
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !stored.IsNil() {
		t.Fatalf("Stored value is %s, want nil", stored.Kind())
	}
}

func TestTableSupportsHostTableIdentityKeys(t *testing.T) {
	table := ember.NewTable()
	key := ember.NewTable()
	other := ember.NewTable()

	if err := table.Set(ember.TableValue(key), ember.StringValue("value")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	got, err := table.Get(ember.TableValue(key))
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	gotString, ok := got.String()
	if !ok {
		t.Fatalf("Get returned %s, want string", got.Kind())
	}
	if gotString != "value" {
		t.Fatalf("Get returned %q, want value", gotString)
	}

	missing, err := table.Get(ember.TableValue(other))
	if err != nil {
		t.Fatalf("Get other returned error: %v", err)
	}
	if !missing.IsNil() {
		t.Fatalf("Get other returned %s, want nil", missing.Kind())
	}
}

func TestTableRejectsNilPublicKeys(t *testing.T) {
	table := ember.NewTable()

	if err := table.Set(ember.NilValue(), ember.NumberValue(1)); err == nil {
		t.Fatal("Set succeeded, want error")
	} else if !strings.Contains(err.Error(), "key is nil") {
		t.Fatalf("Set error is %q, want nil key error", err)
	}

	_, err := table.Get(ember.NilValue())
	if err == nil {
		t.Fatal("Get succeeded, want error")
	}
	if !strings.Contains(err.Error(), "key is nil") {
		t.Fatalf("Get error is %q, want nil key error", err)
	}
}

func TestTableRejectsNaNNumberKeys(t *testing.T) {
	table := ember.NewTable()
	key := ember.NumberValue(math.NaN())

	if err := table.Set(key, ember.NumberValue(1)); err == nil {
		t.Fatal("Set succeeded, want error")
	} else if !strings.Contains(err.Error(), "NaN") {
		t.Fatalf("Set error is %q, want NaN key error", err)
	}

	_, err := table.Get(key)
	if err == nil {
		t.Fatal("Get succeeded, want error")
	}
	if !strings.Contains(err.Error(), "NaN") {
		t.Fatalf("Get error is %q, want NaN key error", err)
	}
}

func TestCompileAndRunSupportedReturns(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   float64
	}{
		{
			name:   "number literal",
			source: "return 42",
			want:   42,
		},
		{
			name:   "chained addition without spaces",
			source: "return 1+2+3",
			want:   6,
		},
		{
			name:   "decimal addition",
			source: "return 1.5 + 2.25",
			want:   3.75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunNumber(t, tt.source)
			if got != tt.want {
				t.Fatalf("Run result is %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileAndRunLocalBindings(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   float64
	}{
		{
			name: "local references",
			source: `
local x = 1
local y = x + 2
return y + x
`,
			want: 4,
		},
		{
			name: "local shadows after initializer",
			source: `
local x = 1
local x = x + 1
return x
`,
			want: 2,
		},
		{
			name: "local assignment",
			source: `
local x = 1
x = x + 4
return x
`,
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunNumber(t, tt.source)
			if got != tt.want {
				t.Fatalf("Run result is %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileAndRunIfThenElseReturnsSelectedBranch(t *testing.T) {
	got := compileAndRunNumber(t, `
local enabled = true
if enabled then
	return 10
else
	return 20
end
`)
	if got != 10 {
		t.Fatalf("Run result is %v, want 10", got)
	}
}

func TestCompileAndRunIfUsesLuauTruthiness(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		want      float64
	}{
		{name: "nil is falsey", condition: "nil", want: 2},
		{name: "false is falsey", condition: "false", want: 2},
		{name: "zero is truthy", condition: "0", want: 1},
		{name: "empty string is truthy", condition: `""`, want: 1},
		{name: "table is truthy", condition: "{}", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunNumber(t, `
if `+tt.condition+` then
	return 1
else
	return 2
end
`)
			if got != tt.want {
				t.Fatalf("Run result is %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileAndRunIfBranchesAssignOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local score = 1
local enabled = false
if enabled then
	score = 10
else
	score = 20
end
return score + 2
`)
	if got != 22 {
		t.Fatalf("Run result is %v, want 22", got)
	}
}

func TestCompileAndRunIfWithoutElseContinues(t *testing.T) {
	got := compileAndRunNumber(t, `
local score = 4
if false then
	score = 99
end
return score
`)
	if got != 4 {
		t.Fatalf("Run result is %v, want 4", got)
	}
}

func TestCompileRejectsBranchLocalOutsideIf(t *testing.T) {
	_, err := ember.Compile(`
if true then
	local branch = 1
end
return branch
`)
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined local "branch"`) {
		t.Fatalf("Compile error is %q, want undefined branch local", err)
	}
}

func TestCompileRejectsUnsupportedSource(t *testing.T) {
	_, err := ember.Compile("x = 1")
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), "expected return statement") {
		t.Fatalf("Compile error is %q, want expected return statement", err)
	}
}

func TestCompileRejectsUndefinedLocal(t *testing.T) {
	_, err := ember.Compile("return missing + 1")
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined local "missing"`) {
		t.Fatalf("Compile error is %q, want undefined local", err)
	}
}

func TestCompileRejectsAssignmentToUndefinedLocal(t *testing.T) {
	_, err := ember.Compile(`
missing = 1
return missing
`)
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined local "missing"`) {
		t.Fatalf("Compile error is %q, want undefined local", err)
	}
}

func TestRunRejectsAddingNonNumbers(t *testing.T) {
	proto, err := ember.Compile(`return "a" + 1`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "add left operand is string") {
		t.Fatalf("Run error is %q, want string add error", err)
	}
}

func TestCompileRejectsUnterminatedString(t *testing.T) {
	_, err := ember.Compile(`return "ember`)
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), "unterminated string") {
		t.Fatalf("Compile error is %q, want unterminated string", err)
	}
}

func TestRunRejectsUndefinedGlobalCall(t *testing.T) {
	proto, err := ember.Compile("return missing()")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.RunWithGlobals(proto, nil)
	if err == nil {
		t.Fatal("RunWithGlobals succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined global "missing"`) {
		t.Fatalf("RunWithGlobals error is %q, want undefined global", err)
	}
}

func TestRunRejectsCallingNonFunction(t *testing.T) {
	proto, err := ember.Compile("return add(1)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.RunWithGlobals(proto, map[string]ember.Value{
		"add": ember.NumberValue(1),
	})
	if err == nil {
		t.Fatal("RunWithGlobals succeeded, want error")
	}
	if !strings.Contains(err.Error(), "call target is number") {
		t.Fatalf("RunWithGlobals error is %q, want call target error", err)
	}
}

func TestRunRejectsFieldReadOnNonTable(t *testing.T) {
	proto, err := ember.Compile(`
local value = 1
return value.x
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "get field target is number") {
		t.Fatalf("Run error is %q, want non-table field error", err)
	}
}

func TestRunRejectsFieldAssignmentOnNonTable(t *testing.T) {
	proto, err := ember.Compile(`
local value = 1
value.x = 2
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "set field target is number") {
		t.Fatalf("Run error is %q, want non-table field assignment error", err)
	}
}

func TestRunRejectsBracketReadOnNonTable(t *testing.T) {
	proto, err := ember.Compile(`
local value = 1
return value[1]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "get index target is number") {
		t.Fatalf("Run error is %q, want non-table index error", err)
	}
}

func TestRunRejectsNilTableIndexKey(t *testing.T) {
	proto, err := ember.Compile(`
local values = {10}
return values[nil]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "key is nil") {
		t.Fatalf("Run error is %q, want nil index key error", err)
	}
}
