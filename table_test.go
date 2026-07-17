package ember_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func TestCompileAndRunTableFieldReads(t *testing.T) {
	got := compileAndRunNumber(t, `
local point = {x = 2, y = 3}
return point.x + point.y
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestValueKindFormatsPublicNames(t *testing.T) {
	values := map[string]ember.Value{
		"nil":           ember.NilValue(),
		"boolean":       ember.BoolValue(true),
		"number":        ember.NumberValue(1),
		"string":        ember.StringValue("x"),
		"table":         ember.TableValue(ember.NewTable()),
		"userdata":      ember.UserDataValue(ember.NewUserData("x")),
		"host_function": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) { return nil, nil }),
	}
	for want, value := range values {
		if got := fmt.Sprintf("%s", value.Kind()); got != want {
			t.Fatalf("Kind formats as %q, want %q", got, want)
		}
	}
}

func TestCompileAndRunNestedTableFieldRead(t *testing.T) {
	got := compileAndRunNumber(t, `
local player = {stats = {hp = 25}}
return player.stats.hp
`)
	if got != 25 {
		t.Fatalf("Run result is %v, want 25", got)
	}
}

func TestCompileAndRunTableIndexThenFieldRead(t *testing.T) {
	got := compileAndRunValue(t, `
local players = {{name = "ember"}, {name = "forge"}}
return players[2].name
`)
	value, ok := got.String()
	if !ok {
		t.Fatalf("Run result is %s, want string", got.Kind())
	}
	if value != "forge" {
		t.Fatalf("Run result is %q, want forge", value)
	}
}

func TestCompileAndRunParenthesizedTableFieldRead(t *testing.T) {
	got := compileAndRunNumber(t, `
return ({stats = {hp = 55}}).stats.hp
`)
	if got != 55 {
		t.Fatalf("Run result is %v, want 55", got)
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

func TestCompileAndRunMetatableIndexTableFieldRead(t *testing.T) {
	got := compileAndRunNumber(t, `
local defaults = {hp = 25}
local player = {}
setmetatable(player, {__index = defaults})
return player.hp
`)
	if got != 25 {
		t.Fatalf("Run result is %v, want 25", got)
	}
}

func TestCompileAndRunGetMetatable(t *testing.T) {
	got := compileAndRunValue(t, `
local object = {}
local meta = {}
setmetatable(object, meta)
return getmetatable(object) == meta
`)
	value, ok := got.Bool()
	if !ok {
		t.Fatalf("Run result is %s, want boolean", got.Kind())
	}
	if !value {
		t.Fatal("Run result is false, want true")
	}
}

func TestCompileAndRunProtectedGetMetatable(t *testing.T) {
	got := compileAndRunString(t, `
local object = {}
setmetatable(object, {__metatable = "locked"})
return getmetatable(object)
`)
	if got != "locked" {
		t.Fatalf("Run result is %q, want locked", got)
	}
}

func TestCompileAndRunCallUsesMetamethod(t *testing.T) {
	got := compileAndRunNumber(t, `
local object = {base = 10}
setmetatable(object, {
	__call = function(self, amount)
		return self.base + amount
	end,
})
return object(5)
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestRunRejectsNonFunctionCallMetamethod(t *testing.T) {
	proto, err := ember.Compile(`
local object = {}
setmetatable(object, {__call = 10})
return object()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run returned nil error, want __call type error")
	}
	if !strings.Contains(err.Error(), "__call is number, want function") {
		t.Fatalf("Run error is %q, want __call type error", err)
	}
}

func TestRunRejectsCyclicCallMetamethod(t *testing.T) {
	proto, err := ember.Compile(`
local object = {}
setmetatable(object, {__call = object})
return object()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run returned nil error, want cyclic __call error")
	}
	if !strings.Contains(err.Error(), "cyclic __call chain") {
		t.Fatalf("Run error is %q, want cyclic __call detail", err)
	}
}

func TestCompileAndRunAddUsesMetamethod(t *testing.T) {
	got := compileAndRunNumber(t, `
local left = {hp = 10}
local right = {hp = 5}
setmetatable(left, {
	__add = function(a, b)
		return {hp = a.hp + b.hp}
	end,
})
local result = left + right
return result.hp
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestCompileAndRunArithmeticOperatorsUseMetamethods(t *testing.T) {
	tests := []struct {
		name      string
		metafield string
		operator  string
		left      float64
		right     float64
		want      float64
	}{
		{name: "subtract", metafield: "__sub", operator: "-", left: 10, right: 3, want: 7},
		{name: "multiply", metafield: "__mul", operator: "*", left: 10, right: 3, want: 30},
		{name: "divide", metafield: "__div", operator: "/", left: 12, right: 3, want: 4},
		{name: "floor divide", metafield: "__idiv", operator: "//", left: 11, right: 3, want: 3},
		{name: "modulo", metafield: "__mod", operator: "%", left: 11, right: 3, want: 2},
		{name: "power", metafield: "__pow", operator: "^", left: 2, right: 3, want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunNumber(t, fmt.Sprintf(`
local left = {value = %g}
local right = {value = %g}
setmetatable(left, {
	%s = function(a, b)
		return {value = a.value %s b.value}
	end,
})
local result = left %s right
return result.value
`, tt.left, tt.right, tt.metafield, tt.operator, tt.operator))
			if got != tt.want {
				t.Fatalf("Run result is %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileAndRunConcatUsesMetamethod(t *testing.T) {
	got := compileAndRunString(t, `
local item = {name = "ember"}
setmetatable(item, {
	__concat = function(left, right)
		return left.name .. right
	end,
})
return item .. "-runtime"
`)
	if got != "ember-runtime" {
		t.Fatalf("Run result is %q, want ember-runtime", got)
	}
}

func TestCompileAndRunLessUsesMetamethod(t *testing.T) {
	got := compileAndRunValue(t, `
local left = {rank = 2}
local right = {rank = 5}
setmetatable(left, {
	__lt = function(a, b)
		return a.rank < b.rank
	end,
})
return left < right
`)
	value, ok := got.Bool()
	if !ok {
		t.Fatalf("Run result is %s, want boolean", got.Kind())
	}
	if !value {
		t.Fatal("Run result is false, want true")
	}
}

func TestCompileAndRunRelationalOperatorsUseMetamethods(t *testing.T) {
	results := compileAndRunValues(t, `
local meta = {
	__lt = function(a, b)
		return a.rank < b.rank
	end,
	__le = function(a, b)
		return a.rank <= b.rank
	end,
}
local low = {rank = 2}
local equal = {rank = 2}
local high = {rank = 5}
setmetatable(low, meta)
setmetatable(equal, meta)
setmetatable(high, meta)
return low <= equal, high > low, high >= equal
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	for index, result := range results {
		value, ok := result.Bool()
		if !ok {
			t.Fatalf("result %d is %s, want boolean", index+1, result.Kind())
		}
		if !value {
			t.Fatalf("result %d is false, want true", index+1)
		}
	}
}

func TestRunRejectsNonBooleanComparisonMetamethodResult(t *testing.T) {
	proto, err := ember.Compile(`
local left = {}
local right = {}
setmetatable(left, {
	__lt = function()
		return "yes"
	end,
})
return left < right
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run returned nil error, want __lt result error")
	}
	if !strings.Contains(err.Error(), "__lt returned string, want boolean") {
		t.Fatalf("Run error is %q, want __lt result error", err)
	}
}

func TestCompileAndRunEqualUsesMetamethodEvenForSameTable(t *testing.T) {
	got := compileAndRunValue(t, `
local object = {}
setmetatable(object, {
	__eq = function()
		return false
	end,
})
return object == object
`)
	value, ok := got.Bool()
	if !ok {
		t.Fatalf("Run result is %s, want boolean", got.Kind())
	}
	if value {
		t.Fatal("Run result is true, want false")
	}
}

func TestCompileAndRunEqualityOperatorsUseMetamethod(t *testing.T) {
	results := compileAndRunValues(t, `
local meta = {
	__eq = function(a, b)
		return a.id == b.id
	end,
}
local left = {id = "ember"}
local same = {id = "ember"}
local other = {id = "forge"}
setmetatable(left, meta)
setmetatable(same, meta)
setmetatable(other, meta)
return left == same, left ~= other
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	for index, result := range results {
		value, ok := result.Bool()
		if !ok {
			t.Fatalf("result %d is %s, want boolean", index+1, result.Kind())
		}
		if !value {
			t.Fatalf("result %d is false, want true", index+1)
		}
	}
}

func TestCompileAndRunUnaryMinusUsesMetamethod(t *testing.T) {
	got := compileAndRunNumber(t, `
local vector = {x = 3}
setmetatable(vector, {
	__unm = function(value)
		return {x = -value.x}
	end,
})
local result = -vector
return result.x
`)
	if got != -3 {
		t.Fatalf("Run result is %v, want -3", got)
	}
}

func TestRunRejectsNonFunctionUnaryMinusMetamethod(t *testing.T) {
	proto, err := ember.Compile(`
local object = {}
setmetatable(object, {__unm = 10})
return -object
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run returned nil error, want __unm type error")
	}
	if !strings.Contains(err.Error(), "__unm is number, want function") {
		t.Fatalf("Run error is %q, want __unm type error", err)
	}
}

func TestRunRejectsReplacingProtectedMetatable(t *testing.T) {
	proto, err := ember.Compile(`
local object = {}
setmetatable(object, {__metatable = "locked"})
setmetatable(object, {})
return 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "cannot change protected metatable") {
		t.Fatalf("Run error is %q, want protected metatable error", err)
	}
}

func TestRunRejectsClearingProtectedMetatable(t *testing.T) {
	proto, err := ember.Compile(`
local object = {}
setmetatable(object, {__metatable = "locked"})
setmetatable(object, nil)
return 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "cannot change protected metatable") {
		t.Fatalf("Run error is %q, want protected metatable error", err)
	}
}

func TestCompileAndRunMetatableIndexDoesNotOverrideExistingField(t *testing.T) {
	got := compileAndRunNumber(t, `
local defaults = {hp = 25}
local player = {hp = 10}
setmetatable(player, {__index = defaults})
return player.hp
`)
	if got != 10 {
		t.Fatalf("Run result is %v, want 10", got)
	}
}

func TestCompileAndRunMetatableRawHitsStayLocal(t *testing.T) {
	got := compileAndRunString(t, `
local backing = {fallback = "index"}
local object = {direct = "local"}
setmetatable(object, {
	__index = backing,
	__newindex = backing,
})
object.direct = object.direct .. ":updated"
object.missing = "stored"
return object.direct .. "|" .. object.fallback .. "|" .. backing.missing
`)
	if got != "local:updated|index|stored" {
		t.Fatalf("Run result is %q, want local:updated|index|stored", got)
	}
}

func TestCompileAndRunMetatableIndexTableBracketRead(t *testing.T) {
	got := compileAndRunString(t, `
local defaults = {fallback = "yes"}
local item = {}
setmetatable(item, {__index = defaults})
return item["fallback"]
`)
	if got != "yes" {
		t.Fatalf("Run result is %q, want yes", got)
	}
}

func TestCompileAndRunMetatableIndexTableMutationChangesFallback(t *testing.T) {
	results := compileAndRunValues(t, `
local firstFallback = {hp = 10}
local secondFallback = {hp = 25}
local meta = {__index = firstFallback}
local player = {}
setmetatable(player, meta)
local first = player.hp
meta.__index = secondFallback
local second = player.hp
return first, second
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	first, ok := results[0].Number()
	if !ok || first != 10 {
		t.Fatalf("first result is %v (%t), want 10", first, ok)
	}
	second, ok := results[1].Number()
	if !ok || second != 25 {
		t.Fatalf("second result is %v (%t), want 25", second, ok)
	}
}

func TestTablePublicGetFollowsTableValuedIndexChain(t *testing.T) {
	results := compileAndRunValues(t, `
local defaults = {hp = 25}
local parent = {}
local object = {}
setmetatable(parent, {__index = defaults})
setmetatable(object, {__index = parent})
return object
`)
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	object, ok := results[0].Table()
	if !ok {
		t.Fatalf("Run result is %s, want table", results[0].Kind())
	}
	got, err := object.Get(ember.StringValue("hp"))
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	gotNumber, ok := got.Number()
	if !ok {
		t.Fatalf("Get returned %s, want number", got.Kind())
	}
	if gotNumber != 25 {
		t.Fatalf("Get returned %v, want 25", gotNumber)
	}
}

func TestCompileAndRunStringAndGenericTableKeysCoexist(t *testing.T) {
	results := compileAndRunValues(t, `
local key = {}
local values = {}
values["name"] = "string"
values[key] = "table"
values["name"] = nil
return values["name"], values[key]
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	if !results[0].IsNil() {
		t.Fatalf("first result is %s, want nil", results[0].Kind())
	}
	got, ok := results[1].String()
	if !ok || got != "table" {
		t.Fatalf("second result is %v (%t), want table", got, ok)
	}
}

func TestCompileAndRunRepeatedStringFieldAccessSeesUpdates(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {count = 1}
local total = 0
local i = 0
while i < 4 do
	i = i + 1
	total = total + values.count
	values.count = values.count + 1
end
return total, values.count
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	total, ok := results[0].Number()
	if !ok || total != 10 {
		t.Fatalf("total is %v (%t), want 10", total, ok)
	}
	count, ok := results[1].Number()
	if !ok || count != 5 {
		t.Fatalf("count is %v (%t), want 5", count, ok)
	}
}

func TestCompileAndRunRepeatedStringFieldAccessSeesDeleteAndFallback(t *testing.T) {
	results := compileAndRunValues(t, `
local fallback = {name = "fallback"}
local values = setmetatable({name = "own"}, {__index = fallback})
local first = values.name
values.name = nil
local second = values.name
return first, second
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	first, ok := results[0].String()
	if !ok || first != "own" {
		t.Fatalf("first value is %q (%t), want own", first, ok)
	}
	second, ok := results[1].String()
	if !ok || second != "fallback" {
		t.Fatalf("second value is %q (%t), want fallback", second, ok)
	}
}

func TestCompileAndRunMetatableIndexCycleReturnsError(t *testing.T) {
	proto, err := ember.Compile(`
local left = {}
local right = {}
setmetatable(left, {__index = right})
setmetatable(right, {__index = left})
return left.missing
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run returned nil error, want cyclic __index error")
	}
	if !strings.Contains(err.Error(), "cyclic __index chain") {
		t.Fatalf("Run error is %q, want cyclic __index detail", err)
	}
}

func TestCompileAndRunRawGetBypassesIndexMetamethod(t *testing.T) {
	results := compileAndRunValues(t, `
local defaults = {hp = 25}
local player = {}
setmetatable(player, {__index = defaults})
return player.hp, rawget(player, "hp")
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	value, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if value != 25 {
		t.Fatalf("first result is %v, want 25", value)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
}

func TestCompileAndRunSetMetatableNilClearsMetatable(t *testing.T) {
	got := compileAndRunValue(t, `
local defaults = {hp = 25}
local player = {}
setmetatable(player, {__index = defaults})
setmetatable(player, nil)
return player.hp
`)
	if !got.IsNil() {
		t.Fatalf("Run result is %s, want nil", got.Kind())
	}
}

func TestCompileAndRunMetatableIndexFunctionRead(t *testing.T) {
	got := compileAndRunNumber(t, `
local item = {base = 20}
setmetatable(item, {__index = function(self, key)
	return self.base + key
end})
return item[5]
`)
	if got != 25 {
		t.Fatalf("Run result is %v, want 25", got)
	}
}

func TestCompileAndRunMetatableNewIndexTableFieldAssignment(t *testing.T) {
	results := compileAndRunValues(t, `
local backing = {}
local object = {}
setmetatable(object, {__newindex = backing})
object.hp = 25
return backing.hp, object.hp
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	backingHP, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if backingHP != 25 {
		t.Fatalf("first result is %v, want 25", backingHP)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
}

func TestCompileAndRunMetatableNewIndexTableBracketAssignment(t *testing.T) {
	got := compileAndRunString(t, `
local backing = {}
local object = {}
setmetatable(object, {__newindex = backing})
object["name"] = "ember"
return backing.name
`)
	if got != "ember" {
		t.Fatalf("Run result is %q, want ember", got)
	}
}

func TestCompileAndRunRawSetBypassesNewIndexMetamethod(t *testing.T) {
	results := compileAndRunValues(t, `
local backing = {}
local object = {}
setmetatable(object, {__newindex = backing})
local returned = rawset(object, "hp", 25)
return object.hp, backing.hp, returned == object
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	objectHP, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if objectHP != 25 {
		t.Fatalf("first result is %v, want 25", objectHP)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
	returned, ok := results[2].Bool()
	if !ok {
		t.Fatalf("third result is %s, want boolean", results[2].Kind())
	}
	if !returned {
		t.Fatal("third result is false, want true")
	}
}

func TestCompileAndRunRuntimeTableAccessCallsFunctionValuedMetamethods(t *testing.T) {
	results := compileAndRunValues(t, `
local log = {}
local indexed = {base = 20}
setmetatable(indexed, {__index = function(self, key)
	return self.base + key
end})
local assigned = {}
setmetatable(assigned, {__newindex = function(self, key, value)
	log[key] = value + 1
	return nil
end})
assigned[5] = 26
return indexed[5], log[5], rawget(assigned, 5)
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	indexValue, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if indexValue != 25 {
		t.Fatalf("first result is %v, want 25", indexValue)
	}
	logValue, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if logValue != 27 {
		t.Fatalf("second result is %v, want 27", logValue)
	}
	if !results[2].IsNil() {
		t.Fatalf("third result is %s, want nil", results[2].Kind())
	}
}

func TestTablePublicAccessRejectsFunctionValuedMetamethods(t *testing.T) {
	results := compileAndRunValues(t, `
local indexed = {}
setmetatable(indexed, {__index = function()
	return 1
end})
local assigned = {}
setmetatable(assigned, {__newindex = function()
	return nil
end})
return indexed, assigned
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	indexed, ok := results[0].Table()
	if !ok {
		t.Fatalf("first result is %s, want table", results[0].Kind())
	}
	assigned, ok := results[1].Table()
	if !ok {
		t.Fatalf("second result is %s, want table", results[1].Kind())
	}
	_, err := indexed.Get(ember.StringValue("missing"))
	if err == nil {
		t.Fatal("Get succeeded, want function-valued __index error")
	}
	if !strings.Contains(err.Error(), "__index is function, want table") {
		t.Fatalf("Get error is %q, want public __index type error", err)
	}
	err = assigned.Set(ember.StringValue("hp"), ember.NumberValue(25))
	if err == nil {
		t.Fatal("Set succeeded, want function-valued __newindex error")
	}
	if !strings.Contains(err.Error(), "__newindex is function, want table") {
		t.Fatalf("Set error is %q, want public __newindex type error", err)
	}
}

func TestCompileAndRunTableLengthOperatorCountsArrayPrefix(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {10, 20, 30}
return #values
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunRawLenBypassesLenMetamethod(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 20}
setmetatable(values, {__len = function(value)
	return 99
end})
return #values, rawlen(values)
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	lenValue, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if lenValue != 99 {
		t.Fatalf("first result is %v, want 99", lenValue)
	}
	rawLen, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if rawLen != 2 {
		t.Fatalf("second result is %v, want 2", rawLen)
	}
}

func TestCompileAndRunLenMetamethodCannotYield(t *testing.T) {
	proto, err := ember.Compile(`
local co = coroutine.create(function()
	local values = {}
	setmetatable(values, {__len = function(value)
		local protectedOK, message = pcall(function()
			return coroutine.yield("bad")
		end)
		record(coroutine.isyieldable(), protectedOK, type(message))
		return 7
	end})
	local length = #values
	return length
end)
local ok, length = coroutine.resume(co)
return ok, length, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	var observed []ember.Value
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			observed = append([]ember.Value(nil), args...)
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	if ok, boolOK := results[0].Bool(); !boolOK || !ok {
		t.Fatalf("resume ok is %#v, want true", results[0])
	}
	if length, numberOK := results[1].Number(); !numberOK || length != 7 {
		t.Fatalf("length is %v, want 7", length)
	}
	if status, stringOK := results[2].String(); !stringOK || status != "dead" {
		t.Fatalf("coroutine status is %q, want dead", status)
	}
	if len(observed) != 3 {
		t.Fatalf("record observed %d values, want 3", len(observed))
	}
	if yieldable, boolOK := observed[0].Bool(); !boolOK || yieldable {
		t.Fatalf("metamethod isyieldable is %#v, want false", observed[0])
	}
	if protectedOK, boolOK := observed[1].Bool(); !boolOK || protectedOK {
		t.Fatalf("protected yield ok is %#v, want false", observed[1])
	}
	if messageType, stringOK := observed[2].String(); !stringOK || messageType != "string" {
		t.Fatalf("protected yield message type is %q, want string", messageType)
	}
}

func TestCompileAndRunRuntimeMetamethodsCannotYield(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantResult string
	}{
		{
			name: "__index",
			source: `
local object = {}
setmetatable(object, {__index = function(self, key)
	local protectedOK, message = pcall(function()
		return coroutine.yield("bad")
	end)
	record(coroutine.isyieldable(), protectedOK, type(message))
	return "indexed"
end})
return object.missing
`,
			wantResult: "indexed",
		},
		{
			name: "__newindex",
			source: `
local object = {}
setmetatable(object, {__newindex = function(self, key, value)
	local protectedOK, message = pcall(function()
		return coroutine.yield("bad")
	end)
	record(coroutine.isyieldable(), protectedOK, type(message))
	return nil
end})
object.hp = 10
return "assigned"
`,
			wantResult: "assigned",
		},
		{
			name: "__call",
			source: `
local object = {}
setmetatable(object, {__call = function(self)
	local protectedOK, message = pcall(function()
		return coroutine.yield("bad")
	end)
	record(coroutine.isyieldable(), protectedOK, type(message))
	return "called"
end})
return object()
`,
			wantResult: "called",
		},
		{
			name: "arithmetic",
			source: `
local left = {}
local right = {}
setmetatable(left, {__add = function(a, b)
	local protectedOK, message = pcall(function()
		return coroutine.yield("bad")
	end)
	record(coroutine.isyieldable(), protectedOK, type(message))
	return "added"
end})
return left + right
`,
			wantResult: "added",
		},
		{
			name: "concat",
			source: `
local left = {}
setmetatable(left, {__concat = function(a, b)
	local protectedOK, message = pcall(function()
		return coroutine.yield("bad")
	end)
	record(coroutine.isyieldable(), protectedOK, type(message))
	return "concatenated"
end})
return left .. "!"
`,
			wantResult: "concatenated",
		},
		{
			name: "comparison",
			source: `
local left = {}
local right = {}
setmetatable(left, {__lt = function(a, b)
	local protectedOK, message = pcall(function()
		return coroutine.yield("bad")
	end)
	record(coroutine.isyieldable(), protectedOK, type(message))
	return true
end})
if left < right then
	return "less"
end
return "not less"
`,
			wantResult: "less",
		},
		{
			name: "__eq",
			source: `
local left = {}
local right = {}
local meta = {__eq = function(a, b)
	local protectedOK, message = pcall(function()
		return coroutine.yield("bad")
	end)
	record(coroutine.isyieldable(), protectedOK, type(message))
	return true
end}
setmetatable(left, meta)
setmetatable(right, meta)
if left == right then
	return "equal"
end
return "not equal"
`,
			wantResult: "equal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto, err := ember.Compile(`
local co = coroutine.create(function()
` + tt.source + `
end)
local ok, result = coroutine.resume(co)
return ok, result, coroutine.status(co)
`)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}

			var observed []ember.Value
			results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					observed = append([]ember.Value(nil), args...)
					return nil, nil
				}),
			})
			if err != nil {
				t.Fatalf("RunWithGlobals returned error: %v", err)
			}
			if len(results) != 3 {
				t.Fatalf("Run returned %d results, want 3", len(results))
			}
			if ok, boolOK := results[0].Bool(); !boolOK || !ok {
				t.Fatalf("resume ok is %#v, want true", results[0])
			}
			if result, stringOK := results[1].String(); !stringOK || result != tt.wantResult {
				t.Fatalf("result is %q, want %q", result, tt.wantResult)
			}
			if status, stringOK := results[2].String(); !stringOK || status != "dead" {
				t.Fatalf("coroutine status is %q, want dead", status)
			}
			if len(observed) != 3 {
				t.Fatalf("record observed %d values, want 3", len(observed))
			}
			if yieldable, boolOK := observed[0].Bool(); !boolOK || yieldable {
				t.Fatalf("metamethod isyieldable is %#v, want false", observed[0])
			}
			if protectedOK, boolOK := observed[1].Bool(); !boolOK || protectedOK {
				t.Fatalf("protected yield ok is %#v, want false", observed[1])
			}
			if messageType, stringOK := observed[2].String(); !stringOK || messageType != "string" {
				t.Fatalf("protected yield message type is %q, want string", messageType)
			}
		})
	}
}

func TestCompileAndRunArrayPartPreservesRawSequenceBehavior(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 20, 40}
table.insert(values, 3, 30)
local removed = table.remove(values, 2)
return rawlen(values), values[1], values[2], values[3], values[4], removed, table.unpack(values)
`)
	if len(results) != 9 {
		t.Fatalf("Run returned %d results, want 9", len(results))
	}
	wants := []float64{3, 10, 30, 40}
	for i, want := range wants {
		got, ok := results[i].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want %v", i, got, ok, want)
		}
	}
	if !results[4].IsNil() {
		t.Fatalf("values[4] is %s, want nil", results[4].Kind())
	}
	removed, ok := results[5].Number()
	if !ok || removed != 20 {
		t.Fatalf("removed value is %v (%t), want 20", removed, ok)
	}
	unpacked := []float64{10, 30, 40}
	for i, want := range unpacked {
		got, ok := results[6+i].Number()
		if !ok || got != want {
			t.Fatalf("unpacked result %d is %v (%t), want %v", i, got, ok, want)
		}
	}
}

func TestCompileAndRunTableSequenceOperationsPreserveResults(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 30, 40, name = "kept only until clear"}
table.insert(values, 2, 20)
local removed = table.remove(values, 3)
local beforeClear = values[1] + values[2] + values[3] + removed
table.clear(values)
local afterClearLength = rawlen(values)
local afterClearName = values.name
table.insert(values, 5)
return beforeClear, afterClearLength, afterClearName, values[1], rawlen(values)
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	beforeClear, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if beforeClear != 100 {
		t.Fatalf("first result is %v, want 100", beforeClear)
	}
	afterClearLength, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if afterClearLength != 0 {
		t.Fatalf("second result is %v, want 0", afterClearLength)
	}
	if !results[2].IsNil() {
		t.Fatalf("third result is %s, want nil", results[2].Kind())
	}
	inserted, ok := results[3].Number()
	if !ok {
		t.Fatalf("fourth result is %s, want number", results[3].Kind())
	}
	if inserted != 5 {
		t.Fatalf("fourth result is %v, want 5", inserted)
	}
	length, ok := results[4].Number()
	if !ok {
		t.Fatalf("fifth result is %s, want number", results[4].Kind())
	}
	if length != 1 {
		t.Fatalf("fifth result is %v, want 1", length)
	}
}

func TestRunRejectsLengthOfNumber(t *testing.T) {
	proto, err := ember.Compile(`return #10`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "length operand is number") {
		t.Fatalf("Run error is %q, want number length error", err)
	}
}

func TestRunRejectsRawSetNilKey(t *testing.T) {
	proto, err := ember.Compile(`
local object = {}
rawset(object, nil, 1)
return 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "key is nil") {
		t.Fatalf("Run error is %q, want nil key error", err)
	}
}

func TestCompileAndRunMetatableNewIndexDoesNotOverrideExistingField(t *testing.T) {
	results := compileAndRunValues(t, `
local backing = {}
local object = {hp = 10}
setmetatable(object, {__newindex = backing})
object.hp = 25
return object.hp, backing.hp
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	objectHP, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if objectHP != 25 {
		t.Fatalf("first result is %v, want 25", objectHP)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
}

func TestCompileAndRunMetatableNewIndexFunctionAssignment(t *testing.T) {
	results := compileAndRunValues(t, `
local log = {}
local object = {}
setmetatable(object, {__newindex = function(self, key, value)
	log[key] = value + 1
	return nil
end})
object.hp = 24
return log.hp, object.hp
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	logHP, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if logHP != 25 {
		t.Fatalf("first result is %v, want 25", logHP)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
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

func TestCompileAndRunNestedTableFieldAssignment(t *testing.T) {
	got := compileAndRunNumber(t, `
local player = {stats = {hp = 25}}
player.stats.hp = 40
return player.stats.hp
`)
	if got != 40 {
		t.Fatalf("Run result is %v, want 40", got)
	}
}

func TestCompileAndRunTableIndexThenFieldAssignment(t *testing.T) {
	got := compileAndRunNumber(t, `
local players = {{hp = 10}, {hp = 20}}
players[2].hp = 35
return players[2].hp
`)
	if got != 35 {
		t.Fatalf("Run result is %v, want 35", got)
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

func TestCompileAndRunComputedKeyTableLiteralStringKey(t *testing.T) {
	got := compileAndRunNumber(t, `
local item = {["hp"] = 25}
return item.hp
`)
	if got != 25 {
		t.Fatalf("Run result is %v, want 25", got)
	}
}

func TestCompileAndRunComputedKeyTableLiteralExpressionKey(t *testing.T) {
	got := compileAndRunString(t, `
local suffix = "name"
local item = {["display_" .. suffix] = "ember"}
return item.display_name
`)
	if got != "ember" {
		t.Fatalf("Run result is %q, want ember", got)
	}
}

func TestCompileAndRunComputedKeyTableLiteralTableIdentityKey(t *testing.T) {
	got := compileAndRunString(t, `
local key = {}
local values = {[key] = "stored"}
return values[key]
`)
	if got != "stored" {
		t.Fatalf("Run result is %q, want stored", got)
	}
}

func TestRunRejectsComputedKeyTableLiteralNilKey(t *testing.T) {
	proto, err := ember.Compile(`
local values = {[nil] = 1}
return values
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "key is nil") {
		t.Fatalf("Run error is %q, want nil key error", err)
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
item.name = "forge"
return item.name
`)
	value, ok := got.String()
	if !ok {
		t.Fatalf("Run result is %s, want string", got.Kind())
	}
	if value != "forge" {
		t.Fatalf("Run result is %q, want forge", value)
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

func TestCompileAndRunHostFunctionStoredInTable(t *testing.T) {
	api := ember.NewTable()
	if err := api.Set(ember.StringValue("add"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		left, _ := args[0].Number()
		right, _ := args[1].Number()
		return []ember.Value{ember.NumberValue(left + right)}, nil
	})); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	proto, err := ember.Compile("return api.add(2, 3)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"api": ember.TableValue(api),
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

func TestCompileAndRunTableHostFunctionCallStatement(t *testing.T) {
	api := ember.NewTable()
	var logged string
	if err := api.Set(ember.StringValue("log"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		var ok bool
		logged, ok = args[0].String()
		if !ok {
			t.Fatalf("log arg is %s, want string", args[0].Kind())
		}
		return nil, nil
	})); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	proto, err := ember.Compile(`
api.log("spawned")
return 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"api": ember.TableValue(api),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if logged != "spawned" {
		t.Fatalf("logged value is %q, want spawned", logged)
	}
	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 1 {
		t.Fatalf("Run result is %v, want 1", got)
	}
}

func TestCompileAndRunHostFunctionSelectedByBracket(t *testing.T) {
	api := ember.NewTable()
	if err := api.Set(ember.StringValue("add"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		left, _ := args[0].Number()
		right, _ := args[1].Number()
		return []ember.Value{ember.NumberValue(left + right)}, nil
	})); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	proto, err := ember.Compile(`return api["add"](4, 6)`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"api": ember.TableValue(api),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 10 {
		t.Fatalf("Run result is %v, want 10", got)
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

func TestHostTableSetFollowsNewIndexTable(t *testing.T) {
	results := compileAndRunValues(t, `
local backing = {}
local object = {}
setmetatable(object, {__newindex = backing})
return object, backing
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	object, ok := results[0].Table()
	if !ok {
		t.Fatalf("first result is %s, want table", results[0].Kind())
	}
	backing, ok := results[1].Table()
	if !ok {
		t.Fatalf("second result is %s, want table", results[1].Kind())
	}

	if err := object.Set(ember.StringValue("hp"), ember.NumberValue(25)); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	got, err := backing.Get(ember.StringValue("hp"))
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	gotNumber, ok := got.Number()
	if !ok {
		t.Fatalf("backing hp is %s, want number", got.Kind())
	}
	if gotNumber != 25 {
		t.Fatalf("backing hp is %v, want 25", gotNumber)
	}

	raw, err := object.Get(ember.StringValue("hp"))
	if err != nil {
		t.Fatalf("Get object hp returned error: %v", err)
	}
	if !raw.IsNil() {
		t.Fatalf("object hp is %s, want nil", raw.Kind())
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
