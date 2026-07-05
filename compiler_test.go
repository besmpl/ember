package ember_test

import (
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

func TestCompileAndRunAdditionCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "4" + 5`)
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunAdditionCoercesBothNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "1.5" + "2.25"`)
	if got != 3.75 {
		t.Fatalf("Run result is %v, want 3.75", got)
	}
}

func TestCompileAndRunSubtraction(t *testing.T) {
	got := compileAndRunNumber(t, "return 10 - 3 - 2")
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunSubtractionCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "9" - "4"`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunMultiplicationPrecedence(t *testing.T) {
	got := compileAndRunNumber(t, "return 2 + 3 * 4")
	if got != 14 {
		t.Fatalf("Run result is %v, want 14", got)
	}
}

func TestCompileAndRunMultiplicationCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "3" * "4"`)
	if got != 12 {
		t.Fatalf("Run result is %v, want 12", got)
	}
}

func TestCompileAndRunGroupedArithmeticPrecedence(t *testing.T) {
	got := compileAndRunNumber(t, "return (2 + 3) * 4")
	if got != 20 {
		t.Fatalf("Run result is %v, want 20", got)
	}
}

func TestCompileAndRunDivision(t *testing.T) {
	got := compileAndRunNumber(t, "return 8 / 2 + 1")
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunDivisionCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "8" / "2"`)
	if got != 4 {
		t.Fatalf("Run result is %v, want 4", got)
	}
}

func TestCompileAndRunModulo(t *testing.T) {
	got := compileAndRunNumber(t, "return 10 % 4")
	if got != 2 {
		t.Fatalf("Run result is %v, want 2", got)
	}
}

func TestCompileAndRunModuloPrecedence(t *testing.T) {
	got := compileAndRunNumber(t, "return 2 + 10 % 4 * 3")
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunModuloCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "10" % "4"`)
	if got != 2 {
		t.Fatalf("Run result is %v, want 2", got)
	}
}

func TestCompileAndRunModuloUsesLuaRemainderSign(t *testing.T) {
	got := compileAndRunNumber(t, "return -5 % 3")
	if got != 1 {
		t.Fatalf("Run result is %v, want 1", got)
	}
}

func TestCompileAndRunFloorDivision(t *testing.T) {
	got := compileAndRunNumber(t, "return 10 // 4")
	if got != 2 {
		t.Fatalf("Run result is %v, want 2", got)
	}
}

func TestCompileAndRunFloorDivisionRoundsTowardNegativeInfinity(t *testing.T) {
	got := compileAndRunNumber(t, "return -10 // 4")
	if got != -3 {
		t.Fatalf("Run result is %v, want -3", got)
	}
}

func TestCompileAndRunFloorDivisionCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "9" // "2"`)
	if got != 4 {
		t.Fatalf("Run result is %v, want 4", got)
	}
}

func TestCompileAndRunFloorDivisionPrecedence(t *testing.T) {
	got := compileAndRunNumber(t, "return 2 + 10 // 4 * 3")
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunExponentiation(t *testing.T) {
	got := compileAndRunNumber(t, "return 2 ^ 4")
	if got != 16 {
		t.Fatalf("Run result is %v, want 16", got)
	}
}

func TestCompileAndRunExponentiationPrecedence(t *testing.T) {
	got := compileAndRunNumber(t, "return 2 + 3 ^ 2 * 4")
	if got != 38 {
		t.Fatalf("Run result is %v, want 38", got)
	}
}

func TestCompileAndRunExponentiationIsRightAssociative(t *testing.T) {
	got := compileAndRunNumber(t, "return 2 ^ 3 ^ 2")
	if got != 512 {
		t.Fatalf("Run result is %v, want 512", got)
	}
}

func TestCompileAndRunExponentiationBindsTighterThanUnaryMinus(t *testing.T) {
	got := compileAndRunNumber(t, "return -2 ^ 2")
	if got != -4 {
		t.Fatalf("Run result is %v, want -4", got)
	}
}

func TestCompileAndRunParenthesizedNegativeExponentBase(t *testing.T) {
	got := compileAndRunNumber(t, "return (-2) ^ 2")
	if got != 4 {
		t.Fatalf("Run result is %v, want 4", got)
	}
}

func TestCompileAndRunExponentiationCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `return "2" ^ "3"`)
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunUnaryMinus(t *testing.T) {
	got := compileAndRunNumber(t, "return 4 * -2 + 11")
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunUnaryMinusCoercesNumericString(t *testing.T) {
	got := compileAndRunNumber(t, `return -"4"`)
	if got != -4 {
		t.Fatalf("Run result is %v, want -4", got)
	}
}

func TestCompileAndRunLocalTypeAnnotationIsErased(t *testing.T) {
	got := compileAndRunNumber(t, `
local n: number = 1
return n + 2
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunLocalTypeAnnotationsSupportQualifiedNilableNames(t *testing.T) {
	results := compileAndRunValues(t, `
local count: number, material: Enum.Material? = 2, "Wood"
return count, material
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}

	count, ok := results[0].Number()
	if !ok {
		t.Fatalf("First result is %s, want number", results[0].Kind())
	}
	if count != 2 {
		t.Fatalf("First result is %v, want 2", count)
	}

	material, ok := results[1].String()
	if !ok {
		t.Fatalf("Second result is %s, want string", results[1].Kind())
	}
	if material != "Wood" {
		t.Fatalf("Second result is %q, want Wood", material)
	}
}

func TestCompileAndRunTableTypeAnnotationIsErased(t *testing.T) {
	got := compileAndRunString(t, `
local stats: {hp: number, name: string} = {hp = 10, name = "ember"}
return stats.name .. ":" .. stats.hp
`)
	if got != "ember:10" {
		t.Fatalf("Run result is %q, want ember:10", got)
	}
}

func TestCompileAndRunTypeAliasDeclarationsAreErased(t *testing.T) {
	got := compileAndRunNumber(t, `
export type ScoreMap<T> = {[string]: T}
type Formatter = (number | string, string?) -> string
local scores: ScoreMap<number> = {hp = 10}
return scores.hp + 5
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestCompileAndRunTypeofTypeAnnotationIsErased(t *testing.T) {
	got := compileAndRunString(t, `
local template = {name = "ember"}
type Template = typeof(template)
local clone: typeof(template) = template
return clone.name
`)
	if got != "ember" {
		t.Fatalf("Run result is %q, want ember", got)
	}
}

func TestCompileAndRunTypeofTypeAliasDoesNotEvaluateExpression(t *testing.T) {
	got := compileAndRunNumber(t, `
type Player = typeof(game:GetService("Players").LocalPlayer)
return 1
`)
	if got != 1 {
		t.Fatalf("Run result is %v, want 1", got)
	}
}

func TestCompileAndRunTypeCastIsErased(t *testing.T) {
	got := compileAndRunNumber(t, `
local value = 4
return (value :: number) + 3
`)
	if got != 7 {
		t.Fatalf("Run result is %v, want 7", got)
	}
}

func TestCompileAndRunFunctionTypeAnnotationsAreErased(t *testing.T) {
	got := compileAndRunNumber(t, `
local function double(n: number): number
	return n * 2
end
return double(4)
`)
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunGenericFunctionTypeParametersAreErased(t *testing.T) {
	got := compileAndRunNumber(t, `
local function identity<T>(value: T): T
	return value
end
return identity(9)
`)
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunGenericFunctionTypeAnnotationIsErased(t *testing.T) {
	got := compileAndRunString(t, `
local identity: <T>(T) -> T = function<T>(value: T): T
	return value
end
return identity("ember")
`)
	if got != "ember" {
		t.Fatalf("Run result is %q, want ember", got)
	}
}

func TestCompileAndRunCallTypeArgumentsAreErased(t *testing.T) {
	got := compileAndRunNumber(t, `
local function identity<T>(value: T): T
	return value
end
return identity<<number>>(7)
`)
	if got != 7 {
		t.Fatalf("Run result is %v, want 7", got)
	}
}

func TestCompileAndRunNamedFunctionTypeArgumentsAreErased(t *testing.T) {
	got := compileAndRunString(t, `
type Formatter = (amount: number, label: string?) -> string
local format: Formatter = function(amount: number, label: string?): string
	return label .. amount
end
return format(5, "hp")
`)
	if got != "hp5" {
		t.Fatalf("Run result is %q, want hp5", got)
	}
}

func TestCompileAndRunRecursiveArithmeticFunction(t *testing.T) {
	got := compileAndRunNumber(t, `
local function factorial(n)
	if n <= 1 then
		return 1
	else
		return n * factorial(n - 1)
	end
end
return factorial(5)
`)
	if got != 120 {
		t.Fatalf("Run result is %v, want 120", got)
	}
}

func TestCompileAndRunBaseTypeFunction(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "nil", source: "return type(nil)", want: "nil"},
		{name: "missing argument", source: "return type()", want: "nil"},
		{name: "boolean", source: "return type(false)", want: "boolean"},
		{name: "number", source: "return type(42)", want: "number"},
		{name: "string", source: `return type("ember")`, want: "string"},
		{name: "table", source: "return type({hp = 10})", want: "table"},
		{name: "script function", source: `
local function heal()
	return 1
end
return type(heal)
`, want: "function"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunString(t, tt.source)
			if got != tt.want {
				t.Fatalf("Run result is %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunWithGlobalsBaseTypeReportsHostFunction(t *testing.T) {
	proto, err := ember.Compile("return type(callback)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"callback": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return []ember.Value{ember.NilValue()}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].String()
	if !ok {
		t.Fatalf("Run result is %s, want string", results[0].Kind())
	}
	if got != "function" {
		t.Fatalf("Run result is %q, want function", got)
	}
}

func TestRunWithGlobalsBaseTypeReportsUserData(t *testing.T) {
	proto, err := ember.Compile("return type(instance)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"instance": ember.UserDataValue(ember.NewUserData("workspace")),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].String()
	if !ok {
		t.Fatalf("Run result is %s, want string", results[0].Kind())
	}
	if got != "userdata" {
		t.Fatalf("Run result is %q, want userdata", got)
	}
}

func TestRunWithGlobalsPassesUserDataPayloadToHostFunction(t *testing.T) {
	type hostInstance struct {
		name string
	}
	instance := &hostInstance{name: "Workspace"}
	proto, err := ember.Compile("return describe(instance)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"instance": ember.UserDataValue(ember.NewUserData(instance)),
		"describe": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			if len(args) != 1 {
				t.Fatalf("Host function received %d args, want 1", len(args))
			}
			userdata, ok := args[0].UserData()
			if !ok {
				t.Fatalf("Host function arg is %s, want userdata", args[0].Kind())
			}
			payload, ok := userdata.Payload().(*hostInstance)
			if !ok {
				t.Fatalf("UserData payload is %T, want *hostInstance", userdata.Payload())
			}
			return []ember.Value{ember.StringValue(payload.name)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].String()
	if !ok {
		t.Fatalf("Run result is %s, want string", results[0].Kind())
	}
	if got != "Workspace" {
		t.Fatalf("Run result is %q, want Workspace", got)
	}
}

func TestRunWithGlobalsComparesUserDataByIdentity(t *testing.T) {
	proto, err := ember.Compile("return first == first, first == second, first ~= second")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"first":  ember.UserDataValue(ember.NewUserData("first")),
		"second": ember.UserDataValue(ember.NewUserData("second")),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	wants := []bool{true, false, true}
	if len(results) != len(wants) {
		t.Fatalf("RunWithGlobals returned %d results, want %d", len(results), len(wants))
	}
	for index, want := range wants {
		got, ok := results[index].Bool()
		if !ok {
			t.Fatalf("result %d is %s, want boolean", index+1, results[index].Kind())
		}
		if got != want {
			t.Fatalf("result %d is %v, want %v", index+1, got, want)
		}
	}
}

func TestRunWithGlobalsUsesUserDataAsTableKeys(t *testing.T) {
	proto, err := ember.Compile(`
local values = {}
values[first] = "first"
values[second] = "second"
return values[first], values[second]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"first":  ember.UserDataValue(ember.NewUserData("first")),
		"second": ember.UserDataValue(ember.NewUserData("second")),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	wants := []string{"first", "second"}
	if len(results) != len(wants) {
		t.Fatalf("RunWithGlobals returned %d results, want %d", len(results), len(wants))
	}
	for index, want := range wants {
		got, ok := results[index].String()
		if !ok {
			t.Fatalf("result %d is %s, want string", index+1, results[index].Kind())
		}
		if got != want {
			t.Fatalf("result %d is %q, want %q", index+1, got, want)
		}
	}
}

func TestRunWithGlobalsCanOverrideBaseType(t *testing.T) {
	proto, err := ember.Compile("return type(nil)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"type": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return []ember.Value{ember.StringValue("override")}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].String()
	if !ok {
		t.Fatalf("Run result is %s, want string", results[0].Kind())
	}
	if got != "override" {
		t.Fatalf("Run result is %q, want override", got)
	}
}

func TestCompileAndRunBaseMathAbs(t *testing.T) {
	got := compileAndRunNumber(t, "return math.abs(-7)")
	if got != 7 {
		t.Fatalf("Run result is %v, want 7", got)
	}
}

func TestCompileAndRunBaseMathFloor(t *testing.T) {
	got := compileAndRunNumber(t, "return math.floor(-1.2)")
	if got != -2 {
		t.Fatalf("Run result is %v, want -2", got)
	}
}

func TestCompileAndRunBaseMathMinAndMax(t *testing.T) {
	results := compileAndRunValues(t, "return math.min(4, -2, 9), math.max(4, -2, 9)")
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	minimum, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	maximum, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if minimum != -2 || maximum != 9 {
		t.Fatalf("Run returned %v and %v, want -2 and 9", minimum, maximum)
	}
}

func TestRunWithGlobalsCanOverrideMathMinFastPath(t *testing.T) {
	proto, err := ember.Compile(`
local value = math.min(4, 2)
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	mathTable := ember.NewTable()
	if err := mathTable.Set(ember.StringValue("min"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		return []ember.Value{ember.StringValue("overridden")}, nil
	})); err != nil {
		t.Fatalf("Set min returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"math": ember.TableValue(mathTable),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].String()
	if !ok || got != "overridden" {
		t.Fatalf("RunWithGlobals result is %v (%t), want overridden", got, ok)
	}
}

func TestCompileAndRunBaseMathPi(t *testing.T) {
	got := compileAndRunNumber(t, "return math.pi")
	if got < 3.14159 || got > 3.14160 {
		t.Fatalf("Run result is %v, want pi", got)
	}
}

func TestCompileAndRunStringLengthOperator(t *testing.T) {
	got := compileAndRunNumber(t, `return #"ember"`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunStringConcatenation(t *testing.T) {
	got := compileAndRunString(t, `return "ember" .. " hearth"`)
	if got != "ember hearth" {
		t.Fatalf("Run result is %q, want ember hearth", got)
	}
}

func TestCompileAndRunConcatenationCoercesNumbers(t *testing.T) {
	got := compileAndRunString(t, `return "hp=" .. 10 .. "/" .. (5 + 10)`)
	if got != "hp=10/15" {
		t.Fatalf("Run result is %q, want hp=10/15", got)
	}
}

func TestRunRejectsConcatenatingTable(t *testing.T) {
	proto, err := ember.Compile(`return "item=" .. {name = "ember"}`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "concat right operand is table") {
		t.Fatalf("Run error is %q, want table concat error", err)
	}
}

func TestRunRejectsBaseMathNonNumberArgument(t *testing.T) {
	proto, err := ember.Compile(`return math.abs("7")`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "math.abs: argument #1 is string, want number") {
		t.Fatalf("Run error is %q, want math.abs type error", err)
	}
}

func TestRunWithGlobalsReturnsBareGlobalValue(t *testing.T) {
	proto, err := ember.Compile("return greeting")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"greeting": ember.StringValue("hello"),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].String()
	if !ok {
		t.Fatalf("Run result is %s, want string", results[0].Kind())
	}
	if got != "hello" {
		t.Fatalf("Run result is %q, want hello", got)
	}
}

func TestRunWithGlobalsBindsBareGlobalFunctionValue(t *testing.T) {
	proto, err := ember.Compile(`
local callback = makeValue
return callback()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"makeValue": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return []ember.Value{ember.NumberValue(9)}, nil
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
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunAssignsGlobalValue(t *testing.T) {
	got := compileAndRunString(t, `
status = "ready"
return status
`)
	if got != "ready" {
		t.Fatalf("Run result is %q, want ready", got)
	}
}

func TestRunWithGlobalsAssignsHostGlobalValue(t *testing.T) {
	proto, err := ember.Compile(`
score = score + 5
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	globals := map[string]ember.Value{
		"score": ember.NumberValue(7),
	}
	results, err := ember.RunWithGlobals(proto, globals)
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
	if got != 12 {
		t.Fatalf("Run result is %v, want 12", got)
	}

	stored, ok := globals["score"].Number()
	if !ok {
		t.Fatalf("Stored score is %s, want number", globals["score"].Kind())
	}
	if stored != 12 {
		t.Fatalf("Stored score is %v, want 12", stored)
	}
}

func TestRunWithGlobalsAssignsNewHostGlobalValue(t *testing.T) {
	proto, err := ember.Compile(`
status = "ready"
return status
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	globals := map[string]ember.Value{}
	results, err := ember.RunWithGlobals(proto, globals)
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	got, ok := results[0].String()
	if !ok {
		t.Fatalf("Run result is %s, want string", results[0].Kind())
	}
	if got != "ready" {
		t.Fatalf("Run result is %q, want ready", got)
	}

	stored, ok := globals["status"].String()
	if !ok {
		t.Fatalf("Stored status is %s, want string", globals["status"].Kind())
	}
	if stored != "ready" {
		t.Fatalf("Stored status is %q, want ready", stored)
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

func compileAndRunString(t *testing.T, source string) string {
	t.Helper()

	result := compileAndRunValue(t, source)
	got, ok := result.String()
	if !ok {
		t.Fatalf("Run result is %s, want string", result.Kind())
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

func compileAndRunValues(t *testing.T, source string) []ember.Value {
	t.Helper()

	proto, err := ember.Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	return results
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

func TestCompileAndRunMultipleReturnValues(t *testing.T) {
	results := compileAndRunValues(t, `return 1, "ember", true`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}

	number, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if number != 1 {
		t.Fatalf("first result is %v, want 1", number)
	}

	text, ok := results[1].String()
	if !ok {
		t.Fatalf("second result is %s, want string", results[1].Kind())
	}
	if text != "ember" {
		t.Fatalf("second result is %q, want ember", text)
	}

	boolean, ok := results[2].Bool()
	if !ok {
		t.Fatalf("third result is %s, want boolean", results[2].Kind())
	}
	if !boolean {
		t.Fatal("third result is false, want true")
	}
}

func TestCompileAndRunEmptyReturnReturnsNoValues(t *testing.T) {
	results := compileAndRunValues(t, `return`)
	if len(results) != 0 {
		t.Fatalf("Run returned %d results, want none", len(results))
	}
}

func TestCompileAndRunImplicitTopLevelReturnReturnsNoValues(t *testing.T) {
	results := compileAndRunValues(t, `local ready = true`)
	if len(results) != 0 {
		t.Fatalf("Run returned %d results, want none", len(results))
	}
}

func TestCompileAndRunImplicitFunctionReturnReturnsNoValues(t *testing.T) {
	results := compileAndRunValues(t, `
local function mark()
	local ready = true
end
mark()
`)
	if len(results) != 0 {
		t.Fatalf("Run returned %d results, want none", len(results))
	}
}

func TestCompileAndRunReturnedFunctionResults(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
return pair()
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	left, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	right, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if left != 2 || right != 3 {
		t.Fatalf("Run returned %v and %v, want 2 and 3", left, right)
	}
}

func TestCompileAndRunReturnedHostFunctionResults(t *testing.T) {
	proto, err := ember.Compile("return pair()")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"pair": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return []ember.Value{ember.StringValue("left"), ember.NumberValue(7)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("RunWithGlobals returned %d results, want 2", len(results))
	}
	left, ok := results[0].String()
	if !ok {
		t.Fatalf("first result is %s, want string", results[0].Kind())
	}
	right, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if left != "left" || right != 7 {
		t.Fatalf("RunWithGlobals returned %q and %v, want left and 7", left, right)
	}
}

func TestCompileAndRunMultiResultCallAsSingleReturnExpression(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
return pair(), 4
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	left, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	right, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if left != 2 || right != 4 {
		t.Fatalf("Run returned %v and %v, want 2 and 4", left, right)
	}
}

func TestCompileAndRunFixedOneResultCallUsesNilForMissingReturn(t *testing.T) {
	proto, err := ember.Compile(`
local value = none()
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"none": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	if !results[0].IsNil() {
		t.Fatalf("RunWithGlobals returned %s, want nil", results[0].Kind())
	}
}

func TestCompileAndRunReturnListExpandsFinalCall(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
return 1, pair()
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	if first != 1 || second != 2 || third != 3 {
		t.Fatalf("Run returned %v, %v, and %v, want 1, 2, and 3", first, second, third)
	}
}

func TestCompileAndRunParenthesizedFinalCallUsesSingleResult(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
return 1, (pair())
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if first != 1 || second != 2 {
		t.Fatalf("Run returned %v and %v, want 1 and 2", first, second)
	}
}

func TestCompileAndRunReturnListExpandsFinalCastedCall(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
return 1, pair() :: any
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	if first != 1 || second != 2 || third != 3 {
		t.Fatalf("Run returned %v, %v, and %v, want 1, 2, and 3", first, second, third)
	}
}

func TestCompileAndRunMultipleLocalBindingsFromReturnedCall(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
local left, right = pair()
return left, right
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	left, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	right, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if left != 2 || right != 3 {
		t.Fatalf("Run returned %v and %v, want 2 and 3", left, right)
	}
}

func TestCompileAndRunMultipleLocalBindingAdjustsValues(t *testing.T) {
	results := compileAndRunValues(t, `
local first, missing = 1
local left, right = 2, 3, 4
return first, missing, left, right
`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if first != 1 {
		t.Fatalf("first result is %v, want 1", first)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
	left, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	right, ok := results[3].Number()
	if !ok {
		t.Fatalf("fourth result is %s, want number", results[3].Kind())
	}
	if left != 2 || right != 3 {
		t.Fatalf("Run returned %v and %v, want 2 and 3", left, right)
	}
}

func TestCompileAndRunMultipleLocalBindingUsesOuterNamesInInitializers(t *testing.T) {
	results := compileAndRunValues(t, `
local value = 1
local value, next = value + 1, value + 2
return value, next
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	value, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	next, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if value != 2 || next != 3 {
		t.Fatalf("Run returned %v and %v, want 2 and 3", value, next)
	}
}

func TestCompileAndRunMultipleAssignmentSwapsLocals(t *testing.T) {
	results := compileAndRunValues(t, `
local left = 1
local right = 2
left, right = right, left
return left, right
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	left, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	right, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if left != 2 || right != 1 {
		t.Fatalf("Run returned %v and %v, want 2 and 1", left, right)
	}
}

func TestCompileAndRunMultipleAssignmentFromReturnedCall(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 4, 5
end
local left = 0
local right = 0
left, right = pair()
return left, right
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	left, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	right, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if left != 4 || right != 5 {
		t.Fatalf("Run returned %v and %v, want 4 and 5", left, right)
	}
}

func TestCompileAndRunMultipleAssignmentAdjustsValues(t *testing.T) {
	results := compileAndRunValues(t, `
local first = 0
local missing = 0
local left = 0
local right = 0
first, missing = 1
left, right = 2, 3, 4
return first, missing, left, right
`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if first != 1 {
		t.Fatalf("first result is %v, want 1", first)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
	left, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	right, ok := results[3].Number()
	if !ok {
		t.Fatalf("fourth result is %s, want number", results[3].Kind())
	}
	if left != 2 || right != 3 {
		t.Fatalf("Run returned %v and %v, want 2 and 3", left, right)
	}
}

func TestCompileAndRunMultipleAssignmentSwapsTableFields(t *testing.T) {
	results := compileAndRunValues(t, `
local stats = {hp = 10, mp = 20}
stats.hp, stats.mp = stats.mp, stats.hp
return stats.hp, stats.mp
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	hp, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	mp, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if hp != 20 || mp != 10 {
		t.Fatalf("Run returned %v and %v, want 20 and 10", hp, mp)
	}
}

func TestCompileAndRunVarargFunctionReturnsArguments(t *testing.T) {
	results := compileAndRunValues(t, `
local function echo(...)
	return ...
end
return echo(1, "two", true)
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if first != 1 {
		t.Fatalf("first result is %v, want 1", first)
	}
	second, ok := results[1].String()
	if !ok {
		t.Fatalf("second result is %s, want string", results[1].Kind())
	}
	if second != "two" {
		t.Fatalf("second result is %q, want two", second)
	}
	third, ok := results[2].Bool()
	if !ok {
		t.Fatalf("third result is %s, want boolean", results[2].Kind())
	}
	if !third {
		t.Fatal("third result is false, want true")
	}
}

func TestCompileAndRunVarargFunctionWithNamedParameters(t *testing.T) {
	results := compileAndRunValues(t, `
local function prepend(head, ...)
	return head, ...
end
return prepend("first", 2, 3)
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].String()
	if !ok {
		t.Fatalf("first result is %s, want string", results[0].Kind())
	}
	if first != "first" {
		t.Fatalf("first result is %q, want first", first)
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	if second != 2 || third != 3 {
		t.Fatalf("Run returned %v and %v, want 2 and 3", second, third)
	}
}

func TestCompileAndRunSelectCountsVarargs(t *testing.T) {
	got := compileAndRunNumber(t, `
local function count(...)
	return select("#", ...)
end
return count(1, nil, "three")
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestRunWithGlobalsCanOverrideBaseSelectCountFastPath(t *testing.T) {
	proto, err := ember.Compile(`
local function count(...)
	return select("#", ...)
end
return count(1, 2, 3)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"select": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return []ember.Value{ember.NumberValue(99)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 99 {
		t.Fatalf("RunWithGlobals result is %#v, want number 99", results[0])
	}
}

func TestCompileAndRunCanAssignOverBaseSelectAfterRead(t *testing.T) {
	results := compileAndRunValues(t, `
local before = select("#", 1, 2, 3)
select = function()
	return 99
end
return before, select()
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	before, ok := results[0].Number()
	if !ok || before != 3 {
		t.Fatalf("before result is %v (%t), want 3", before, ok)
	}
	after, ok := results[1].Number()
	if !ok || after != 99 {
		t.Fatalf("after result is %v (%t), want 99", after, ok)
	}
}

func TestRunWithGlobalsCanOverrideRawLenFastPath(t *testing.T) {
	proto, err := ember.Compile(`
local values = {1, 2, 3}
return rawlen(values)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"rawlen": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return []ember.Value{ember.NumberValue(99)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 99 {
		t.Fatalf("RunWithGlobals result is %#v, want number 99", results[0])
	}
}

func TestCompileAndRunSelectReturnsValuesFromPositiveIndex(t *testing.T) {
	results := compileAndRunValues(t, `
local function tail(...)
	return select(2, ...)
end
return tail("drop", 20, 30)
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if first != 20 || second != 30 {
		t.Fatalf("Run returned %v and %v, want 20 and 30", first, second)
	}
}

func TestCompileAndRunSelectReturnsValuesFromNegativeIndex(t *testing.T) {
	results := compileAndRunValues(t, `
local function lastTwo(...)
	return select(-2, ...)
end
return lastTwo(10, 20, 30)
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if first != 20 || second != 30 {
		t.Fatalf("Run returned %v and %v, want 20 and 30", first, second)
	}
}

func TestCompileAndRunToNumberConvertsDecimalString(t *testing.T) {
	got := compileAndRunNumber(t, `return tonumber("42.5")`)
	if got != 42.5 {
		t.Fatalf("Run result is %v, want 42.5", got)
	}
}

func TestCompileAndRunToNumberConvertsStringWithBase(t *testing.T) {
	got := compileAndRunNumber(t, `return tonumber("ff", 16)`)
	if got != 255 {
		t.Fatalf("Run result is %v, want 255", got)
	}
}

func TestCompileAndRunToNumberReturnsNilForInvalidString(t *testing.T) {
	got := compileAndRunValue(t, `return tonumber("nope")`)
	if !got.IsNil() {
		t.Fatalf("Run result is %s, want nil", got.Kind())
	}
}

func TestCompileAndRunToStringConvertsScalarValues(t *testing.T) {
	results := compileAndRunValues(t, `return tostring(nil), tostring(true), tostring(12.5), tostring("ember")`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	wants := []string{"nil", "true", "12.5", "ember"}
	for i, want := range wants {
		got, ok := results[i].String()
		if !ok {
			t.Fatalf("result %d is %s, want string", i+1, results[i].Kind())
		}
		if got != want {
			t.Fatalf("result %d is %q, want %q", i+1, got, want)
		}
	}
}

func TestCompileAndRunToStringUsesMetamethod(t *testing.T) {
	got := compileAndRunString(t, `
local object = {name = "ember"}
setmetatable(object, {
	__tostring = function(self)
		return "object:" .. self.name
	end,
})
return tostring(object)
`)
	if got != "object:ember" {
		t.Fatalf("Run result is %q, want object:ember", got)
	}
}

func TestCompileAndRunToStringRejectsNonStringMetamethodResult(t *testing.T) {
	proto, err := ember.Compile(`
local object = {}
setmetatable(object, {
	__tostring = function()
		return 42
	end,
})
return tostring(object)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run returned nil error, want __tostring result error")
	}
	if !strings.Contains(err.Error(), "__tostring returned number, want string") {
		t.Fatalf("Run error is %q, want __tostring result error", err)
	}
}

func TestCompileAndRunTablePackStoresVarargsAndCount(t *testing.T) {
	results := compileAndRunValues(t, `
local packed = table.pack(10, nil, 30)
return packed[1], packed[2], packed[3], packed.n
`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	count, ok := results[3].Number()
	if !ok {
		t.Fatalf("fourth result is %s, want number", results[3].Kind())
	}
	if first != 10 || third != 30 || count != 3 {
		t.Fatalf("Run returned %v, %v, and %v, want 10, 30, and 3", first, third, count)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
}

func TestCompileAndRunTableUnpackExpandsExplicitRange(t *testing.T) {
	results := compileAndRunValues(t, `
local packed = table.pack(10, nil, 30)
return table.unpack(packed, 1, packed.n)
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	if first != 10 || third != 30 {
		t.Fatalf("Run returned %v and %v, want 10 and 30", first, third)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
}

func TestCompileAndRunUnpackExpandsArrayPrefix(t *testing.T) {
	results := compileAndRunValues(t, `return unpack({4, 5, 6})`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	for i, result := range results {
		got, ok := result.Number()
		if !ok {
			t.Fatalf("result %d is %s, want number", i+1, result.Kind())
		}
		want := float64(i + 4)
		if got != want {
			t.Fatalf("result %d is %v, want %v", i+1, got, want)
		}
	}
}

func TestCompileAndRunTableInsertAppendsValue(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 20}
table.insert(values, 30)
return values[1], values[2], values[3], #values
`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	for i, result := range results {
		got, ok := result.Number()
		if !ok {
			t.Fatalf("result %d is %s, want number", i+1, result.Kind())
		}
		want := []float64{10, 20, 30, 3}[i]
		if got != want {
			t.Fatalf("result %d is %v, want %v", i+1, got, want)
		}
	}
}

func TestCompileAndRunTableInsertShiftsValues(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 30, 40}
table.insert(values, 2, 20)
return values[1], values[2], values[3], values[4], #values
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	for i, result := range results {
		got, ok := result.Number()
		if !ok {
			t.Fatalf("result %d is %s, want number", i+1, result.Kind())
		}
		want := []float64{10, 20, 30, 40, 4}[i]
		if got != want {
			t.Fatalf("result %d is %v, want %v", i+1, got, want)
		}
	}
}

func TestCompileAndRunTableRemoveShiftsValues(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 20, 30, 40}
local removed = table.remove(values, 2)
return removed, values[1], values[2], values[3], values[4], #values
`)
	if len(results) != 6 {
		t.Fatalf("Run returned %d results, want 6", len(results))
	}
	wantNumbers := map[int]float64{
		0: 20,
		1: 10,
		2: 30,
		3: 40,
		5: 3,
	}
	for index, want := range wantNumbers {
		got, ok := results[index].Number()
		if !ok {
			t.Fatalf("result %d is %s, want number", index+1, results[index].Kind())
		}
		if got != want {
			t.Fatalf("result %d is %v, want %v", index+1, got, want)
		}
	}
	if !results[4].IsNil() {
		t.Fatalf("result 5 is %s, want nil", results[4].Kind())
	}
}

func TestCompileAndRunTableRemoveRepeatedlyFromFront(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {}
for i = 1, 80 do
	table.insert(values, i % 9)
end
local removed = 0
for i = 1, 20 do
	removed = removed + table.remove(values, 1)
end
return removed + rawlen(values)
`)
	if got != 135 {
		t.Fatalf("Run result is %v, want 135", got)
	}
}

func TestCompileAndRunCallExpressionRHSDoesNotClobberLoopState(t *testing.T) {
	got := compileAndRunNumber(t, `
local function id(value)
	return value
end
local total = 0
for i = 1, 20 do
	total = total + id(i)
end
return total
`)
	if got != 210 {
		t.Fatalf("Run result is %v, want 210", got)
	}
}

func TestCompileAndRunTableRemoveFrontViaLocalBeforeAdding(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {}
for i = 1, 80 do
	table.insert(values, i % 9)
end
local removed = 0
for i = 1, 20 do
	local value = table.remove(values, 1)
	removed = removed + value
end
return removed + rawlen(values)
`)
	if got != 135 {
		t.Fatalf("Run result is %v, want 135", got)
	}
}

func TestCompileAndRunTableRemoveFrontPreservesZeroValues(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {}
for i = 1, 10 do
	table.insert(values, i % 9)
end
return table.remove(values, 1), table.remove(values, 1), table.remove(values, 1),
	table.remove(values, 1), table.remove(values, 1), table.remove(values, 1),
	table.remove(values, 1), table.remove(values, 1), table.remove(values, 1),
	table.remove(values, 1), rawlen(values)
`)
	if len(results) != 11 {
		t.Fatalf("Run returned %d results, want 11", len(results))
	}
	wants := []float64{1, 2, 3, 4, 5, 6, 7, 8, 0, 1, 0}
	for i, want := range wants {
		got, ok := results[i].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want %v", i+1, got, ok, want)
		}
	}
}

func TestCompileAndRunTableRemoveDefaultsToLastValue(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 20, 30}
local removed = table.remove(values)
return removed, values[1], values[2], values[3], #values
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	wantNumbers := map[int]float64{
		0: 30,
		1: 10,
		2: 20,
		4: 2,
	}
	for index, want := range wantNumbers {
		got, ok := results[index].Number()
		if !ok {
			t.Fatalf("result %d is %s, want number", index+1, results[index].Kind())
		}
		if got != want {
			t.Fatalf("result %d is %v, want %v", index+1, got, want)
		}
	}
	if !results[3].IsNil() {
		t.Fatalf("result 4 is %s, want nil", results[3].Kind())
	}
}

func TestRunWithGlobalsCanOverrideTableRemoveFastPath(t *testing.T) {
	proto, err := ember.Compile(`
local values = {10, 20}
return table.remove(values, 1)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	table := ember.NewTable()
	if err := table.Set(ember.StringValue("remove"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		return []ember.Value{ember.NumberValue(99)}, nil
	})); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"table": ember.TableValue(table),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 99 {
		t.Fatalf("RunWithGlobals result is %#v, want number 99", results[0])
	}
}

func TestRunWithGlobalsCanOverrideTableInsertFastPath(t *testing.T) {
	proto, err := ember.Compile(`
local values = {}
return table.insert(values, 10)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	table := ember.NewTable()
	if err := table.Set(ember.StringValue("insert"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		return []ember.Value{ember.NumberValue(99)}, nil
	})); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"table": ember.TableValue(table),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 99 {
		t.Fatalf("RunWithGlobals result is %#v, want number 99", results[0])
	}
}

func TestCompileAndRunTableConcatJoinsArrayPrefix(t *testing.T) {
	got := compileAndRunString(t, `
local values = {"he", "ar", "th"}
return table.concat(values)
`)
	if got != "hearth" {
		t.Fatalf("Run result is %q, want hearth", got)
	}
}

func TestCompileAndRunTableConcatUsesSeparatorAndRange(t *testing.T) {
	got := compileAndRunString(t, `
local values = {"skip", "a", "b", "c", "skip"}
return table.concat(values, "-", 2, 4)
`)
	if got != "a-b-c" {
		t.Fatalf("Run result is %q, want a-b-c", got)
	}
}

func TestCompileAndRunTableFindReturnsFirstMatchingIndex(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {10, 20, 20}
return table.find(values, 20)
`)
	if got != 2 {
		t.Fatalf("Run result is %v, want 2", got)
	}
}

func TestCompileAndRunTableFindStartsAtIndex(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {10, 20, 20}
return table.find(values, 20, 3)
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunTableFindStopsAtFirstNil(t *testing.T) {
	got := compileAndRunValue(t, `
local values = {10, [3] = 20}
return table.find(values, 20)
`)
	if !got.IsNil() {
		t.Fatalf("Run result is %s, want nil", got.Kind())
	}
}

func TestCompileAndRunCoroutineCreateResumeAndStatus(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	return "done", 42
end)
local before = coroutine.status(co)
local ok, label, amount = coroutine.resume(co)
local after = coroutine.status(co)
return before, ok, label, amount, after
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	before, ok := results[0].String()
	if !ok || before != "suspended" {
		t.Fatalf("before status is %v (%t), want suspended", before, ok)
	}
	resumed, ok := results[1].Bool()
	if !ok || !resumed {
		t.Fatalf("resume result is %v (%t), want true", resumed, ok)
	}
	label, ok := results[2].String()
	if !ok || label != "done" {
		t.Fatalf("label is %v (%t), want done", label, ok)
	}
	amount, ok := results[3].Number()
	if !ok || amount != 42 {
		t.Fatalf("amount is %v (%t), want 42", amount, ok)
	}
	after, ok := results[4].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestRunWithGlobalsCanOverrideCoroutineStatusFastPath(t *testing.T) {
	proto, err := ember.Compile(`
local co = coroutine.create(function()
	return "done"
end)
return coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	coroutine := ember.NewTable()
	if err := coroutine.Set(ember.StringValue("create"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		return []ember.Value{ember.UserDataValue(ember.NewUserData("fake"))}, nil
	})); err != nil {
		t.Fatalf("Set create returned error: %v", err)
	}
	if err := coroutine.Set(ember.StringValue("status"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		return []ember.Value{ember.StringValue("overridden")}, nil
	})); err != nil {
		t.Fatalf("Set status returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"coroutine": ember.TableValue(coroutine),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].String()
	if !ok || got != "overridden" {
		t.Fatalf("RunWithGlobals result is %v (%t), want overridden", got, ok)
	}
}

func TestRunWithGlobalsCanOverrideCoroutineResumeFastPath(t *testing.T) {
	proto, err := ember.Compile(`
local ok, value = coroutine.resume("fake", 1)
return ok, value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	coroutine := ember.NewTable()
	if err := coroutine.Set(ember.StringValue("resume"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		return []ember.Value{ember.StringValue("overridden"), ember.NumberValue(99)}, nil
	})); err != nil {
		t.Fatalf("Set resume returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"coroutine": ember.TableValue(coroutine),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	got, ok := results[0].String()
	if !ok || got != "overridden" {
		t.Fatalf("RunWithGlobals first result is %v (%t), want overridden", got, ok)
	}
	amount, ok := results[1].Number()
	if !ok || amount != 99 {
		t.Fatalf("RunWithGlobals second result is %v (%t), want 99", amount, ok)
	}
}

func TestCompileAndRunCoroutineYieldAndResumeValues(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function(first)
	local resumed = coroutine.yield("yielded", first + 1)
	return "done", resumed + 2
end)
local ok1, label, amount = coroutine.resume(co, 4)
local middle = coroutine.status(co)
local ok2, done, final = coroutine.resume(co, 8)
local after = coroutine.status(co)
return ok1, label, amount, middle, ok2, done, final, after
`)
	if len(results) != 8 {
		t.Fatalf("Run returned %d results, want 8", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	label, ok := results[1].String()
	if !ok || label != "yielded" {
		t.Fatalf("yield label is %v (%t), want yielded", label, ok)
	}
	amount, ok := results[2].Number()
	if !ok || amount != 5 {
		t.Fatalf("yield amount is %v (%t), want 5", amount, ok)
	}
	middle, ok := results[3].String()
	if !ok || middle != "suspended" {
		t.Fatalf("middle status is %v (%t), want suspended", middle, ok)
	}
	ok2, ok := results[4].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	done, ok := results[5].String()
	if !ok || done != "done" {
		t.Fatalf("done label is %v (%t), want done", done, ok)
	}
	final, ok := results[6].Number()
	if !ok || final != 10 {
		t.Fatalf("final value is %v (%t), want 10", final, ok)
	}
	after, ok := results[7].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestCompileAndRunCoroutineAggregateYieldedValues(t *testing.T) {
	got := compileAndRunNumber(t, `
local co = coroutine.create(function(limit)
	local total = 0
	for i = 1, limit do
		total = total + i
		if i % 10 == 0 then
			coroutine.yield(total)
		end
	end
	return total
end)
local total = 0
local ok, value = coroutine.resume(co, 45)
while coroutine.status(co) ~= "dead" do
	total = total + value
	ok, value = coroutine.resume(co)
end
return total + value
`)
	if got != 2585 {
		t.Fatalf("Run result is %v, want 2585", got)
	}
}

func TestCompileAndRunCoroutineRepeatedYieldResumeValues(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	coroutine.yield(10)
	coroutine.yield(20)
	return 30
end)
local ok1, value1 = coroutine.resume(co)
local status1 = coroutine.status(co)
local ok2, value2 = coroutine.resume(co)
local status2 = coroutine.status(co)
local ok3, value3 = coroutine.resume(co)
local status3 = coroutine.status(co)
return ok1, value1, status1, ok2, value2, status2, ok3, value3, status3
`)
	if len(results) != 9 {
		t.Fatalf("Run returned %d results, want 9", len(results))
	}
	wantBools := map[int]bool{0: true, 3: true, 6: true}
	for index, want := range wantBools {
		got, ok := results[index].Bool()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want %v; results are %#v", index+1, got, ok, want, results)
		}
	}
	wantNumbers := map[int]float64{1: 10, 4: 20, 7: 30}
	for index, want := range wantNumbers {
		got, ok := results[index].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want %v", index+1, got, ok, want)
		}
	}
	wantStrings := map[int]string{2: "suspended", 5: "suspended", 8: "dead"}
	for index, want := range wantStrings {
		got, ok := results[index].String()
		if !ok || got != want {
			t.Fatalf("result %d is %q (%t), want %q", index+1, got, ok, want)
		}
	}
}

func TestCompileAndRunCoroutineCloseSuspendedCoroutine(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	coroutine.yield("paused")
	return "unreachable"
end)
local ok1, label = coroutine.resume(co)
local before = coroutine.status(co)
local closed = coroutine.close(co)
local after = coroutine.status(co)
local ok2, message = coroutine.resume(co)
return ok1, label, before, closed, after, ok2, message
`)
	if len(results) != 7 {
		t.Fatalf("Run returned %d results, want 7", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	label, ok := results[1].String()
	if !ok || label != "paused" {
		t.Fatalf("yield label is %v (%t), want paused", label, ok)
	}
	before, ok := results[2].String()
	if !ok || before != "suspended" {
		t.Fatalf("before close status is %v (%t), want suspended", before, ok)
	}
	closed, ok := results[3].Bool()
	if !ok || !closed {
		t.Fatalf("close result is %v (%t), want true", closed, ok)
	}
	after, ok := results[4].String()
	if !ok || after != "dead" {
		t.Fatalf("after close status is %v (%t), want dead", after, ok)
	}
	ok2, ok := results[5].Bool()
	if !ok || ok2 {
		t.Fatalf("second resume is %v (%t), want false", ok2, ok)
	}
	message, ok := results[6].String()
	if !ok || message != "cannot resume dead coroutine" {
		t.Fatalf("dead resume message is %v (%t), want cannot resume dead coroutine", message, ok)
	}
}

func TestCompileAndRunCoroutineRunningReturnsCurrentCoroutine(t *testing.T) {
	results := compileAndRunValues(t, `
local main = coroutine.running()
local co = nil
co = coroutine.create(function()
	local current = coroutine.running()
	return main == nil, coroutine.status(current), current == co
end)
local ok, mainMissing, status, same = coroutine.resume(co)
return ok, mainMissing, status, same
`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	okResult, ok := results[0].Bool()
	if !ok || !okResult {
		t.Fatalf("resume result is %v (%t), want true", okResult, ok)
	}
	mainMissing, ok := results[1].Bool()
	if !ok || !mainMissing {
		t.Fatalf("main running result is %v (%t), want true", mainMissing, ok)
	}
	status, ok := results[2].String()
	if !ok || status != "running" {
		t.Fatalf("running status is %v (%t), want running", status, ok)
	}
	same, ok := results[3].Bool()
	if !ok || !same {
		t.Fatalf("running identity is %v (%t), want true", same, ok)
	}
}

func TestCompileAndRunCoroutineIsYieldableUsesActiveCoroutineState(t *testing.T) {
	results := compileAndRunValues(t, `
local main = coroutine.isyieldable()
local co = coroutine.create(function()
	return coroutine.isyieldable()
end)
local ok, inside = coroutine.resume(co)
return main, ok, inside
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	main, ok := results[0].Bool()
	if !ok || main {
		t.Fatalf("main isyieldable is %v (%t), want false", main, ok)
	}
	resumed, ok := results[1].Bool()
	if !ok || !resumed {
		t.Fatalf("resume result is %v (%t), want true", resumed, ok)
	}
	inside, ok := results[2].Bool()
	if !ok || !inside {
		t.Fatalf("coroutine isyieldable is %v (%t), want true", inside, ok)
	}
}

func TestCompileAndRunCoroutineWrapResumesAndReturnsValues(t *testing.T) {
	results := compileAndRunValues(t, `
local wrapped = coroutine.wrap(function(first)
	local resumed = coroutine.yield("yielded", first + 1)
	return "done", resumed + 2
end)
local label, amount = wrapped(4)
local done, final = wrapped(8)
return label, amount, done, final
`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	label, ok := results[0].String()
	if !ok || label != "yielded" {
		t.Fatalf("yield label is %v (%t), want yielded", label, ok)
	}
	amount, ok := results[1].Number()
	if !ok || amount != 5 {
		t.Fatalf("yield amount is %v (%t), want 5", amount, ok)
	}
	done, ok := results[2].String()
	if !ok || done != "done" {
		t.Fatalf("done label is %v (%t), want done", done, ok)
	}
	final, ok := results[3].Number()
	if !ok || final != 10 {
		t.Fatalf("final value is %v (%t), want 10", final, ok)
	}
}

func TestCompileAndRunCoroutineStatusReportsNormalDuringNestedResume(t *testing.T) {
	results := compileAndRunValues(t, `
local outer = nil
local inner = nil
inner = coroutine.create(function()
	return coroutine.status(outer)
end)
outer = coroutine.create(function()
	local ok, status = coroutine.resume(inner)
	return ok, status
end)
local ok, innerOK, outerStatus = coroutine.resume(outer)
return ok, innerOK, outerStatus, coroutine.status(outer), coroutine.status(inner)
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	okResult, ok := results[0].Bool()
	if !ok || !okResult {
		t.Fatalf("outer resume result is %v (%t), want true", okResult, ok)
	}
	innerOK, ok := results[1].Bool()
	if !ok || !innerOK {
		t.Fatalf("inner resume result is %v (%t), want true", innerOK, ok)
	}
	outerStatus, ok := results[2].String()
	if !ok || outerStatus != "normal" {
		t.Fatalf("outer nested status is %v (%t), want normal", outerStatus, ok)
	}
	finalOuter, ok := results[3].String()
	if !ok || finalOuter != "dead" {
		t.Fatalf("outer final status is %v (%t), want dead", finalOuter, ok)
	}
	finalInner, ok := results[4].String()
	if !ok || finalInner != "dead" {
		t.Fatalf("inner final status is %v (%t), want dead", finalInner, ok)
	}
}

func TestCompileAndRunCoroutineResumeRejectsNormalCoroutine(t *testing.T) {
	results := compileAndRunValues(t, `
local outer = nil
local inner = nil
inner = coroutine.create(function()
	local ok, message = coroutine.resume(outer)
	return ok, type(message), coroutine.status(outer)
end)
outer = coroutine.create(function()
	local ok, resumed, messageType, status = coroutine.resume(inner)
	return ok, resumed, messageType, status
end)
local ok, innerOK, resumed, messageType, status = coroutine.resume(outer)
return ok, innerOK, resumed, messageType, status
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	okResult, ok := results[0].Bool()
	if !ok || !okResult {
		t.Fatalf("outer resume result is %v (%t), want true", okResult, ok)
	}
	innerOK, ok := results[1].Bool()
	if !ok || !innerOK {
		t.Fatalf("inner resume result is %v (%t), want true", innerOK, ok)
	}
	resumed, ok := results[2].Bool()
	if !ok || resumed {
		t.Fatalf("normal coroutine resume result is %v (%t), want false", resumed, ok)
	}
	messageType, ok := results[3].String()
	if !ok || messageType != "string" {
		t.Fatalf("normal coroutine resume message type is %v (%t), want string", messageType, ok)
	}
	status, ok := results[4].String()
	if !ok || status != "normal" {
		t.Fatalf("outer nested status is %v (%t), want normal", status, ok)
	}
}

func TestCompileAndRunCoroutineCloseReportsErrorStoppedCoroutine(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	return missingGlobal + 1
end)
local ok1, err1 = coroutine.resume(co)
local status = coroutine.status(co)
local ok2, err2 = coroutine.close(co)
return ok1, type(err1), status, ok2, type(err2)
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || ok1 {
		t.Fatalf("error resume result is %v (%t), want false", ok1, ok)
	}
	err1Type, ok := results[1].String()
	if !ok || err1Type != "string" {
		t.Fatalf("resume error type is %v (%t), want string", err1Type, ok)
	}
	status, ok := results[2].String()
	if !ok || status != "dead" {
		t.Fatalf("error-stopped status is %v (%t), want dead", status, ok)
	}
	ok2, ok := results[3].Bool()
	if !ok || ok2 {
		t.Fatalf("close result is %v (%t), want false", ok2, ok)
	}
	err2Type, ok := results[4].String()
	if !ok || err2Type != "string" {
		t.Fatalf("close error type is %v (%t), want string", err2Type, ok)
	}
}

func TestCompileAndRunCoroutineCloseSucceedsForDeadReturnedCoroutine(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	return "done"
end)
local ok1, value = coroutine.resume(co)
local status = coroutine.status(co)
local ok2, extra = coroutine.close(co)
return ok1, value, status, ok2, extra
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("resume result is %v (%t), want true", ok1, ok)
	}
	value, ok := results[1].String()
	if !ok || value != "done" {
		t.Fatalf("resume value is %v (%t), want done", value, ok)
	}
	status, ok := results[2].String()
	if !ok || status != "dead" {
		t.Fatalf("status is %v (%t), want dead", status, ok)
	}
	ok2, ok := results[3].Bool()
	if !ok || !ok2 {
		t.Fatalf("close result is %v (%t), want true", ok2, ok)
	}
	if !results[4].IsNil() {
		t.Fatalf("close extra result is %s, want nil", results[4].Kind())
	}
}

func TestRunCoroutineWrapPropagatesCoroutineErrors(t *testing.T) {
	proto, err := ember.Compile(`
local wrapped = coroutine.wrap(function()
	return missingGlobal + 1
end)
return wrapped()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want wrapped coroutine error")
	}
	if !strings.Contains(err.Error(), "missingGlobal") {
		t.Fatalf("Run error is %q, want missing global cause", err)
	}
}

func TestCompileAndRunCoroutineYieldThroughNestedScriptCall(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local function inner(first)
		local resumed = coroutine.yield("pause", first + 1)
		return "inner", resumed + 2
	end
	local function outer(seed)
		local label, amount = inner(seed)
		return label, amount + 3
	end
	return outer(4)
end)
local ok1, yielded, amount = coroutine.resume(co)
local middle = coroutine.status(co)
local ok2, label, final = coroutine.resume(co, 8)
local after = coroutine.status(co)
return ok1, yielded, amount, middle, ok2, label, final, after
`)
	if len(results) != 8 {
		t.Fatalf("Run returned %d results, want 8", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	yielded, ok := results[1].String()
	if !ok || yielded != "pause" {
		t.Fatalf("yield label is %v (%t), want pause", yielded, ok)
	}
	amount, ok := results[2].Number()
	if !ok || amount != 5 {
		t.Fatalf("yield amount is %v (%t), want 5", amount, ok)
	}
	middle, ok := results[3].String()
	if !ok || middle != "suspended" {
		t.Fatalf("middle status is %v (%t), want suspended", middle, ok)
	}
	ok2, ok := results[4].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	label, ok := results[5].String()
	if !ok || label != "inner" {
		t.Fatalf("final label is %v (%t), want inner", label, ok)
	}
	final, ok := results[6].Number()
	if !ok || final != 13 {
		t.Fatalf("final value is %v (%t), want 13", final, ok)
	}
	after, ok := results[7].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestCompileAndRunCoroutineYieldThroughReturnCallChain(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local function inner(first)
		local resumed = coroutine.yield("pause", first + 1)
		return "done", resumed, resumed + 1
	end
	local function outer(seed)
		return inner(seed)
	end
	return outer(4)
end)
local ok1, yielded, amount = coroutine.resume(co)
local ok2, label, first, second = coroutine.resume(co, 8)
return ok1, yielded, amount, ok2, label, first, second, coroutine.status(co)
`)
	if len(results) != 8 {
		t.Fatalf("Run returned %d results, want 8", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	yielded, ok := results[1].String()
	if !ok || yielded != "pause" {
		t.Fatalf("yield label is %v (%t), want pause", yielded, ok)
	}
	amount, ok := results[2].Number()
	if !ok || amount != 5 {
		t.Fatalf("yield amount is %v (%t), want 5", amount, ok)
	}
	ok2, ok := results[3].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	label, ok := results[4].String()
	if !ok || label != "done" {
		t.Fatalf("final label is %v (%t), want done", label, ok)
	}
	first, ok := results[5].Number()
	if !ok || first != 8 {
		t.Fatalf("first final value is %v (%t), want 8", first, ok)
	}
	second, ok := results[6].Number()
	if !ok || second != 9 {
		t.Fatalf("second final value is %v (%t), want 9", second, ok)
	}
	after, ok := results[7].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestCompileAndRunCoroutineYieldThroughRecursiveScriptCalls(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local function descend(n)
		if n == 0 then
			return coroutine.yield("bottom")
		end
		return descend(n - 1) + 1
	end
	return descend(3)
end)
local ok1, label = coroutine.resume(co)
local ok2, value = coroutine.resume(co, 10)
return ok1, label, ok2, value, coroutine.status(co)
`)
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	label, ok := results[1].String()
	if !ok || label != "bottom" {
		t.Fatalf("yield label is %v (%t), want bottom", label, ok)
	}
	ok2, ok := results[2].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	value, ok := results[3].Number()
	if !ok || value != 13 {
		t.Fatalf("final value is %v (%t), want 13", value, ok)
	}
	after, ok := results[4].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestCompileAndRunCoroutineYieldThroughFinalCallArgumentList(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local function inner(seed)
		local resumed = coroutine.yield("pause", seed + 1)
		return resumed, resumed + 1
	end
	local function collect(prefix, first, second)
		return prefix, first, second
	end
	return collect("head", inner(4))
end)
local ok1, label, amount = coroutine.resume(co)
local ok2, prefix, first, second = coroutine.resume(co, 8)
return ok1, label, amount, ok2, prefix, first, second, coroutine.status(co)
`)
	if len(results) != 8 {
		t.Fatalf("Run returned %d results, want 8", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	label, ok := results[1].String()
	if !ok || label != "pause" {
		t.Fatalf("yield label is %v (%t), want pause", label, ok)
	}
	amount, ok := results[2].Number()
	if !ok || amount != 5 {
		t.Fatalf("yield amount is %v (%t), want 5", amount, ok)
	}
	ok2, ok := results[3].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	prefix, ok := results[4].String()
	if !ok || prefix != "head" {
		t.Fatalf("prefix is %v (%t), want head", prefix, ok)
	}
	first, ok := results[5].Number()
	if !ok || first != 8 {
		t.Fatalf("first expanded value is %v (%t), want 8", first, ok)
	}
	second, ok := results[6].Number()
	if !ok || second != 9 {
		t.Fatalf("second expanded value is %v (%t), want 9", second, ok)
	}
	after, ok := results[7].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestCompileAndRunProtectedCallReturnsSuccessAndValues(t *testing.T) {
	results := compileAndRunValues(t, `
local ok, sum, label = pcall(function(left, right)
	return left + right, "done"
end, 2, 3)
return ok, sum, label
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	okResult, ok := results[0].Bool()
	if !ok || !okResult {
		t.Fatalf("pcall result is %v (%t), want true", okResult, ok)
	}
	sum, ok := results[1].Number()
	if !ok || sum != 5 {
		t.Fatalf("pcall sum is %v (%t), want 5", sum, ok)
	}
	label, ok := results[2].String()
	if !ok || label != "done" {
		t.Fatalf("pcall label is %v (%t), want done", label, ok)
	}
}

func TestCompileAndRunProtectedCallCapturesRuntimeError(t *testing.T) {
	results := compileAndRunValues(t, `
local ok, message = pcall(function()
	return missingGlobal + 1
end)
return ok, type(message), message ~= nil
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	okResult, ok := results[0].Bool()
	if !ok || okResult {
		t.Fatalf("pcall result is %v (%t), want false", okResult, ok)
	}
	messageType, ok := results[1].String()
	if !ok || messageType != "string" {
		t.Fatalf("pcall error type is %v (%t), want string", messageType, ok)
	}
	hasMessage, ok := results[2].Bool()
	if !ok || !hasMessage {
		t.Fatalf("pcall message presence is %v (%t), want true", hasMessage, ok)
	}
}

func TestCompileAndRunProtectedXCallReturnsSuccessAndValues(t *testing.T) {
	results := compileAndRunValues(t, `
local ok, sum, label = xpcall(function(left, right)
	return left + right, "done"
end, function(message)
	return "handled"
end, 2, 3)
return ok, sum, label
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	okResult, ok := results[0].Bool()
	if !ok || !okResult {
		t.Fatalf("xpcall result is %v (%t), want true", okResult, ok)
	}
	sum, ok := results[1].Number()
	if !ok || sum != 5 {
		t.Fatalf("xpcall sum is %v (%t), want 5", sum, ok)
	}
	label, ok := results[2].String()
	if !ok || label != "done" {
		t.Fatalf("xpcall label is %v (%t), want done", label, ok)
	}
}

func TestCompileAndRunProtectedXCallUsesErrorHandler(t *testing.T) {
	results := compileAndRunValues(t, `
local ok, handled, kind = xpcall(function()
	return missingGlobal + 1
end, function(message)
	return "handled", type(message)
end)
return ok, handled, kind
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	okResult, ok := results[0].Bool()
	if !ok || okResult {
		t.Fatalf("xpcall result is %v (%t), want false", okResult, ok)
	}
	handled, ok := results[1].String()
	if !ok || handled != "handled" {
		t.Fatalf("xpcall handler result is %v (%t), want handled", handled, ok)
	}
	kind, ok := results[2].String()
	if !ok || kind != "string" {
		t.Fatalf("xpcall error kind is %v (%t), want string", kind, ok)
	}
}

func TestCompileAndRunProtectedCallTargetCanYield(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local ok, label, amount = pcall(function(seed)
		local resumed = coroutine.yield("pause", seed + 1)
		return "done", resumed + 2
	end, 4)
	return ok, label, amount
end)
local ok1, yielded, first = coroutine.resume(co)
local status = coroutine.status(co)
local ok2, protectedOK, label, amount = coroutine.resume(co, 8)
return ok1, yielded, first, status, ok2, protectedOK, label, amount, coroutine.status(co)
`)
	if len(results) != 9 {
		t.Fatalf("Run returned %d results, want 9", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	yielded, ok := results[1].String()
	if !ok || yielded != "pause" {
		t.Fatalf("yield label is %v (%t), want pause", yielded, ok)
	}
	first, ok := results[2].Number()
	if !ok || first != 5 {
		t.Fatalf("yield value is %v (%t), want 5", first, ok)
	}
	status, ok := results[3].String()
	if !ok || status != "suspended" {
		t.Fatalf("middle status is %v (%t), want suspended", status, ok)
	}
	ok2, ok := results[4].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	protectedOK, ok := results[5].Bool()
	if !ok || !protectedOK {
		t.Fatalf("pcall result is %v (%t), want true", protectedOK, ok)
	}
	label, ok := results[6].String()
	if !ok || label != "done" {
		t.Fatalf("pcall label is %v (%t), want done", label, ok)
	}
	amount, ok := results[7].Number()
	if !ok || amount != 10 {
		t.Fatalf("pcall amount is %v (%t), want 10", amount, ok)
	}
	after, ok := results[8].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestCompileAndRunProtectedXCallTargetCanYield(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local ok, label, amount = xpcall(function(seed)
		local resumed = coroutine.yield("pause", seed + 1)
		return "done", resumed + 2
	end, function(message)
		return "handled"
	end, 4)
	return ok, label, amount
end)
local ok1, yielded, first = coroutine.resume(co)
local status = coroutine.status(co)
local ok2, protectedOK, label, amount = coroutine.resume(co, 8)
return ok1, yielded, first, status, ok2, protectedOK, label, amount, coroutine.status(co)
`)
	if len(results) != 9 {
		t.Fatalf("Run returned %d results, want 9", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	yielded, ok := results[1].String()
	if !ok || yielded != "pause" {
		t.Fatalf("yield label is %v (%t), want pause", yielded, ok)
	}
	first, ok := results[2].Number()
	if !ok || first != 5 {
		t.Fatalf("yield value is %v (%t), want 5", first, ok)
	}
	status, ok := results[3].String()
	if !ok || status != "suspended" {
		t.Fatalf("middle status is %v (%t), want suspended", status, ok)
	}
	ok2, ok := results[4].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	protectedOK, ok := results[5].Bool()
	if !ok || !protectedOK {
		t.Fatalf("xpcall result is %v (%t), want true", protectedOK, ok)
	}
	label, ok := results[6].String()
	if !ok || label != "done" {
		t.Fatalf("xpcall label is %v (%t), want done", label, ok)
	}
	amount, ok := results[7].Number()
	if !ok || amount != 10 {
		t.Fatalf("xpcall amount is %v (%t), want 10", amount, ok)
	}
	after, ok := results[8].String()
	if !ok || after != "dead" {
		t.Fatalf("after status is %v (%t), want dead", after, ok)
	}
}

func TestCompileAndRunProtectedCallTargetErrorAfterYieldIsCaptured(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local ok, message = pcall(function()
		coroutine.yield("pause")
		return missingGlobal + 1
	end)
	return ok, type(message), "after"
end)
local ok1, label = coroutine.resume(co)
local ok2, protectedOK, messageType, after = coroutine.resume(co)
return ok1, label, ok2, protectedOK, messageType, after, coroutine.status(co)
`)
	if len(results) != 7 {
		t.Fatalf("Run returned %d results, want 7", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	label, ok := results[1].String()
	if !ok || label != "pause" {
		t.Fatalf("yield label is %v (%t), want pause", label, ok)
	}
	ok2, ok := results[2].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	protectedOK, ok := results[3].Bool()
	if !ok || protectedOK {
		t.Fatalf("pcall result is %v (%t), want false", protectedOK, ok)
	}
	messageType, ok := results[4].String()
	if !ok || messageType != "string" {
		t.Fatalf("pcall message type is %v (%t), want string", messageType, ok)
	}
	after, ok := results[5].String()
	if !ok || after != "after" {
		t.Fatalf("continuation value is %v (%t), want after", after, ok)
	}
	status, ok := results[6].String()
	if !ok || status != "dead" {
		t.Fatalf("after status is %v (%t), want dead", status, ok)
	}
}

func TestCompileAndRunProtectedXCallTargetErrorAfterYieldUsesHandler(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local ok, handled, kind, yieldable = xpcall(function()
		coroutine.yield("pause")
		return missingGlobal + 1
	end, function(message)
		return "handled", type(message), coroutine.isyieldable()
	end)
	return ok, handled, kind, yieldable, "after"
end)
local ok1, label = coroutine.resume(co)
local ok2, protectedOK, handled, kind, yieldable, after = coroutine.resume(co)
return ok1, label, ok2, protectedOK, handled, kind, yieldable, after, coroutine.status(co)
`)
	if len(results) != 9 {
		t.Fatalf("Run returned %d results, want 9", len(results))
	}
	ok1, ok := results[0].Bool()
	if !ok || !ok1 {
		t.Fatalf("first resume is %v (%t), want true", ok1, ok)
	}
	label, ok := results[1].String()
	if !ok || label != "pause" {
		t.Fatalf("yield label is %v (%t), want pause", label, ok)
	}
	ok2, ok := results[2].Bool()
	if !ok || !ok2 {
		t.Fatalf("second resume is %v (%t), want true", ok2, ok)
	}
	protectedOK, ok := results[3].Bool()
	if !ok || protectedOK {
		t.Fatalf("xpcall result is %v (%t), want false", protectedOK, ok)
	}
	handled, ok := results[4].String()
	if !ok || handled != "handled" {
		t.Fatalf("handler result is %v (%t), want handled", handled, ok)
	}
	kind, ok := results[5].String()
	if !ok || kind != "string" {
		t.Fatalf("handler kind is %v (%t), want string", kind, ok)
	}
	yieldable, ok := results[6].Bool()
	if !ok || yieldable {
		t.Fatalf("handler isyieldable is %v (%t), want false", yieldable, ok)
	}
	after, ok := results[7].String()
	if !ok || after != "after" {
		t.Fatalf("continuation value is %v (%t), want after", after, ok)
	}
	status, ok := results[8].String()
	if !ok || status != "dead" {
		t.Fatalf("after status is %v (%t), want dead", status, ok)
	}
}

func TestCompileAndRunProtectedXCallErrorHandlerCannotYield(t *testing.T) {
	results := compileAndRunValues(t, `
local co = coroutine.create(function()
	local ok, handlerYieldable, handlerOK, handlerMessageType = xpcall(function()
		return missingGlobal + 1
	end, function(message)
		local handlerYieldable = coroutine.isyieldable()
		local ok, yielded = pcall(function()
			return coroutine.yield("bad")
		end)
		return handlerYieldable, ok, type(yielded)
	end)
	return ok, handlerYieldable, handlerOK, handlerMessageType, "after"
end)
local ok, protectedOK, handlerYieldable, handlerOK, handlerMessageType, after = coroutine.resume(co)
return ok, protectedOK, handlerYieldable, handlerOK, handlerMessageType, after, coroutine.status(co)
`)
	if len(results) != 7 {
		t.Fatalf("Run returned %d results, want 7", len(results))
	}
	resumed, ok := results[0].Bool()
	if !ok || !resumed {
		t.Fatalf("resume result is %v (%t), want true", resumed, ok)
	}
	protectedOK, ok := results[1].Bool()
	if !ok || protectedOK {
		t.Fatalf("xpcall result is %v (%t), want false", protectedOK, ok)
	}
	handlerYieldable, ok := results[2].Bool()
	if !ok || handlerYieldable {
		t.Fatalf("handler isyieldable is %v (%t), want false", handlerYieldable, ok)
	}
	handlerOK, ok := results[3].Bool()
	if !ok || handlerOK {
		t.Fatalf("handler pcall result is %v (%t), want false", handlerOK, ok)
	}
	messageType, ok := results[4].String()
	if !ok || messageType != "string" {
		t.Fatalf("handler yield error type is %v (%t), want string", messageType, ok)
	}
	after, ok := results[5].String()
	if !ok || after != "after" {
		t.Fatalf("continuation value is %v (%t), want after", after, ok)
	}
	status, ok := results[6].String()
	if !ok || status != "dead" {
		t.Fatalf("after status is %v (%t), want dead", status, ok)
	}
}

func TestCompileAndRunTableSortOrdersArrayPrefix(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {30, 10, 20}
table.sort(values)
return values[1], values[2], values[3]
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	wants := []float64{10, 20, 30}
	for index, want := range wants {
		got, ok := results[index].Number()
		if !ok {
			t.Fatalf("result %d is %s, want number", index+1, results[index].Kind())
		}
		if got != want {
			t.Fatalf("result %d is %v, want %v", index+1, got, want)
		}
	}
}

func TestCompileAndRunTableSortUsesComparator(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 30, 20}
table.sort(values, function(left, right)
	return left > right
end)
return values[1], values[2], values[3]
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	wants := []float64{30, 20, 10}
	for index, want := range wants {
		got, ok := results[index].Number()
		if !ok {
			t.Fatalf("result %d is %s, want number", index+1, results[index].Kind())
		}
		if got != want {
			t.Fatalf("result %d is %v, want %v", index+1, got, want)
		}
	}
}

func TestRunRejectsTableSortComparatorReturningNonBoolean(t *testing.T) {
	proto, err := ember.Compile(`
local values = {2, 1}
table.sort(values, function()
	return "yes"
end)
return values[1]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run returned nil error, want comparator result error")
	}
	if !strings.Contains(err.Error(), "table.sort: comparison returned string, want boolean") {
		t.Fatalf("Run error is %q, want comparator result error", err)
	}
}

func TestCompileAndRunTableClearRemovesAllEntries(t *testing.T) {
	results := compileAndRunValues(t, `
local values = {10, 20, name = "ember"}
table.clear(values)
values[1] = 30
return values[1], values[2], values.name, #values
`)
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if first != 30 {
		t.Fatalf("first result is %v, want 30", first)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
	if !results[2].IsNil() {
		t.Fatalf("third result is %s, want nil", results[2].Kind())
	}
	length, ok := results[3].Number()
	if !ok {
		t.Fatalf("fourth result is %s, want number", results[3].Kind())
	}
	if length != 1 {
		t.Fatalf("fourth result is %v, want 1", length)
	}
}

func TestCompileAndRunVarargLocalBindingAdjustsValues(t *testing.T) {
	results := compileAndRunValues(t, `
local function firstTwo(...)
	local first, second, missing = ...
	return first, second, missing
end
return firstTwo(4, 5)
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if first != 4 || second != 5 {
		t.Fatalf("Run returned %v and %v, want 4 and 5", first, second)
	}
	if !results[2].IsNil() {
		t.Fatalf("third result is %s, want nil", results[2].Kind())
	}
}

func TestCompileAndRunVarargAssignmentAdjustsValues(t *testing.T) {
	results := compileAndRunValues(t, `
local function firstTwo(...)
	local first = 0
	local second = 0
	local missing = 0
	first, second, missing = ...
	return first, second, missing
end
return firstTwo(6, 7)
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if first != 6 || second != 7 {
		t.Fatalf("Run returned %v and %v, want 6 and 7", first, second)
	}
	if !results[2].IsNil() {
		t.Fatalf("third result is %s, want nil", results[2].Kind())
	}
}

func TestCompileAndRunVarargAsSingleNonFinalReturnValue(t *testing.T) {
	results := compileAndRunValues(t, `
local function firstThenLabel(...)
	return ..., "done"
end
return firstThenLabel(4, 5)
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	if first != 4 {
		t.Fatalf("first result is %v, want 4", first)
	}
	second, ok := results[1].String()
	if !ok {
		t.Fatalf("second result is %s, want string", results[1].Kind())
	}
	if second != "done" {
		t.Fatalf("second result is %q, want done", second)
	}
}

func TestCompileAndRunFinalCallArgumentExpandsResults(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
local function collect(...)
	return ...
end
return collect(1, pair())
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	if first != 1 || second != 2 || third != 3 {
		t.Fatalf("Run returned %v, %v, and %v, want 1, 2, and 3", first, second, third)
	}
}

func TestCompileAndRunFinalVarargArgumentExpandsResults(t *testing.T) {
	results := compileAndRunValues(t, `
local function collect(...)
	return ...
end
local function forward(...)
	return collect("head", ...)
end
return forward(2, 3)
`)
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	head, ok := results[0].String()
	if !ok {
		t.Fatalf("first result is %s, want string", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	if head != "head" || second != 2 || third != 3 {
		t.Fatalf("Run returned %q, %v, and %v, want head, 2, and 3", head, second, third)
	}
}

func TestCompileAndRunNonFinalCallArgumentUsesFirstResult(t *testing.T) {
	results := compileAndRunValues(t, `
local function pair()
	return 2, 3
end
local function collect(...)
	return ...
end
return collect(pair(), 4)
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if first != 2 || second != 4 {
		t.Fatalf("Run returned %v and %v, want 2 and 4", first, second)
	}
}

func TestCompileAndRunHostCallExpandsFinalScriptCallArgument(t *testing.T) {
	proto, err := ember.Compile(`
local function pair()
	return 2, 3
end
return collect(1, pair())
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"collect": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			if len(args) != 3 {
				t.Fatalf("host function received %d args, want 3", len(args))
			}
			return args, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("RunWithGlobals returned %d results, want 3", len(results))
	}
	first, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	second, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	third, ok := results[2].Number()
	if !ok {
		t.Fatalf("third result is %s, want number", results[2].Kind())
	}
	if first != 1 || second != 2 || third != 3 {
		t.Fatalf("RunWithGlobals returned %v, %v, and %v, want 1, 2, and 3", first, second, third)
	}
}

func TestCompileRejectsVarargOutsideVariadicFunction(t *testing.T) {
	_, err := ember.Compile(`
local function bad()
	return ...
end
return bad()
`)
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), "vararg outside variadic function") {
		t.Fatalf("Compile error is %q, want vararg error", err)
	}
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

func TestRunWithGlobalsHostFunctionArgsAreIsolatedFromRegisterReuse(t *testing.T) {
	proto, err := ember.Compile(`
local value = 1
capture(value)
value = 2
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	var captured []ember.Value
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"capture": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			captured = args
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("RunWithGlobals returned %d results, want 1", len(results))
	}
	if len(captured) != 1 {
		t.Fatalf("host function captured %d args, want 1", len(captured))
	}
	got, ok := captured[0].Number()
	if !ok || got != 1 {
		t.Fatalf("captured host arg is %#v, want number 1", captured[0])
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

func TestCompileAndRunHostFunctionCallStatement(t *testing.T) {
	proto, err := ember.Compile(`
log(7)
return 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	var logged float64
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"log": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			var ok bool
			logged, ok = args[0].Number()
			if !ok {
				t.Fatalf("log arg is %s, want number", args[0].Kind())
			}
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if logged != 7 {
		t.Fatalf("logged value is %v, want 7", logged)
	}
	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunLocalFunctionCall(t *testing.T) {
	got := compileAndRunNumber(t, `
local function add(left, right)
	return left + right
end
return add(2, 3)
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunLocalFunctionCallStatement(t *testing.T) {
	got := compileAndRunNumber(t, `
local function setHP(player, hp)
	player.hp = hp
	return nil
end
local player = {}
setHP(player, 35)
return player.hp
`)
	if got != 35 {
		t.Fatalf("Run result is %v, want 35", got)
	}
}

func TestCompileAndRunAnonymousFunctionLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local add = function(left, right)
	return left + right
end
return add(2, 3)
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunAnonymousFunctionInTable(t *testing.T) {
	got := compileAndRunNumber(t, `
local tools = {
	add = function(left, right)
		return left + right
	end,
}
return tools.add(4, 6)
`)
	if got != 10 {
		t.Fatalf("Run result is %v, want 10", got)
	}
}

func TestCompileAndRunAnonymousFunctionCapturesOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local base = 2
local addBase = function(value)
	return value + base
end
return addBase(3)
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunClosureSeesReassignedOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local base = 2
local addBase = function(value)
	return value + base
end
base = 5
return addBase(3)
`)
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunReturnedClosureKeepsOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local function makeAdder(base)
	return function(value)
		return value + base
	end
end
local addTwo = makeAdder(2)
return addTwo(3)
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunReturnedClosureCapturesShadowedOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local value = 1
local function make()
	local value = 2
	return function()
		return value
	end
end
local get = make()
return get() + value
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunClosureAssignsShadowedOuterLocal(t *testing.T) {
	results := compileAndRunValues(t, `
local value = 1
local function make()
	local value = 2
	return function()
		value = value + 3
		return value
	end
end
local bump = make()
return bump(), value
`)
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	inner, ok := results[0].Number()
	if !ok || inner != 5 {
		t.Fatalf("first result is %v, want 5", results[0])
	}
	outer, ok := results[1].Number()
	if !ok || outer != 1 {
		t.Fatalf("second result is %v, want 1", results[1])
	}
}

func TestCompileAndRunClosureMutatesOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local value = 1
local function bump()
	value = value + 1
	return value
end
bump()
return bump()
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunLocalFunctionCanReferenceItself(t *testing.T) {
	got := compileAndRunValue(t, `
local function self()
	return self
end
return self() == self
`)
	value, ok := got.Bool()
	if !ok {
		t.Fatalf("Run result is %s, want boolean", got.Kind())
	}
	if !value {
		t.Fatal("Run result is false, want true")
	}
}

func TestCompileAndRunMethodCallPassesReceiver(t *testing.T) {
	got := compileAndRunNumber(t, `
local player = {
	hp = 10,
	heal = function(self, amount)
		self.hp = self.hp + amount
		return self.hp
	end,
}
return player:heal(5)
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestCompileAndRunHostMethodCallPassesReceiver(t *testing.T) {
	api := ember.NewTable()
	if err := api.Set(ember.StringValue("add"), ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
		if len(args) != 3 {
			t.Fatalf("host method received %d args, want 3", len(args))
		}
		receiver, ok := args[0].Table()
		if !ok {
			t.Fatalf("self arg is %s, want table", args[0].Kind())
		}
		if receiver != api {
			t.Fatal("self arg is not the api table")
		}
		left, _ := args[1].Number()
		right, _ := args[2].Number()
		return []ember.Value{ember.NumberValue(left + right)}, nil
	})); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	proto, err := ember.Compile("return api:add(4, 6)")
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

func TestCompileAndRunMethodCallStatement(t *testing.T) {
	got := compileAndRunNumber(t, `
local player = {
	hp = 10,
	heal = function(self, amount)
		self.hp = self.hp + amount
		return nil
	end,
}
player:heal(5)
return player.hp
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestCompileAndRunMethodFunctionDeclaration(t *testing.T) {
	got := compileAndRunNumber(t, `
local player = {hp = 10}
function player:heal(amount)
	self.hp = self.hp + amount
	return self.hp
end
return player:heal(5)
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestCompileAndRunFieldFunctionDeclaration(t *testing.T) {
	got := compileAndRunNumber(t, `
local tools = {}
function tools.add(left, right)
	return left + right
end
return tools.add(2, 3)
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunNestedFieldFunctionDeclaration(t *testing.T) {
	got := compileAndRunNumber(t, `
local tools = {math = {}}
function tools.math.add(left, right)
	return left + right
end
return tools.math.add(4, 6)
`)
	if got != 10 {
		t.Fatalf("Run result is %v, want 10", got)
	}
}

func TestCompileAndRunReturnedHostFunctionCall(t *testing.T) {
	proto, err := ember.Compile("return makeAdd()(4, 5)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"makeAdd": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			return []ember.Value{ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
				left, _ := args[0].Number()
				right, _ := args[1].Number()
				return []ember.Value{ember.NumberValue(left + right)}, nil
			})}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("Run result is %s, want number", results[0].Kind())
	}
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
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

func TestCompileAndRunIfWithEqualityComparison(t *testing.T) {
	got := compileAndRunNumber(t, `
local score = 10
if score == 10 then
	return 1
else
	return 2
end
`)
	if got != 1 {
		t.Fatalf("Run result is %v, want 1", got)
	}
}

func TestCompileAndRunIfStatementSelectsElseIfBranch(t *testing.T) {
	got := compileAndRunString(t, `
local hp = 0
if hp > 0 then
	return "alive"
elseif hp == 0 then
	return "stunned"
else
	return "down"
end
`)
	if got != "stunned" {
		t.Fatalf("Run result is %q, want stunned", got)
	}
}

func TestCompileAndRunIfStatementSkipsLaterElseIfBranches(t *testing.T) {
	got := compileAndRunString(t, `
local hp = 10
if hp > 0 then
	return "alive"
elseif missing() then
	return "missing"
else
	return "down"
end
`)
	if got != "alive" {
		t.Fatalf("Run result is %q, want alive", got)
	}
}

func TestCompileAndRunIfExpressionSelectsThenBranch(t *testing.T) {
	got := compileAndRunString(t, `
local hp = 10
local state = if hp > 0 then "alive" else "down"
return state
`)
	if got != "alive" {
		t.Fatalf("Run result is %q, want alive", got)
	}
}

func TestCompileAndRunIfExpressionSelectsElseBranch(t *testing.T) {
	got := compileAndRunString(t, `
local hp = 0
return if hp > 0 then "alive" else "down"
`)
	if got != "down" {
		t.Fatalf("Run result is %q, want down", got)
	}
}

func TestCompileAndRunIfExpressionSupportsElseIf(t *testing.T) {
	got := compileAndRunString(t, `
local hp = 0
return if hp > 0 then "alive" elseif hp == 0 then "stunned" else "down"
`)
	if got != "stunned" {
		t.Fatalf("Run result is %q, want stunned", got)
	}
}

func TestCompileAndRunIfExpressionDoesNotEvaluateUnselectedBranch(t *testing.T) {
	got := compileAndRunNumber(t, `
local function fail()
	return missing()
end
return if true then 7 else fail()
`)
	if got != 7 {
		t.Fatalf("Run result is %v, want 7", got)
	}
}

func TestCompileAndRunEqualityComparisons(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "nil equals nil", source: "return nil == nil", want: true},
		{name: "true equals true", source: "return true == true", want: true},
		{name: "false not equal true", source: "return false ~= true", want: true},
		{name: "numbers equal", source: "return 1 + 2 == 3", want: true},
		{name: "strings equal", source: `return "ember" == "ember"`, want: true},
		{name: "different kinds not equal", source: `return "1" ~= 1`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunValue(t, tt.source)
			value, ok := got.Bool()
			if !ok {
				t.Fatalf("Run result is %s, want boolean", got.Kind())
			}
			if value != tt.want {
				t.Fatalf("Run result is %v, want %v", value, tt.want)
			}
		})
	}
}

func TestCompileAndRunLogicalOrReturnsSelectedValue(t *testing.T) {
	got := compileAndRunNumber(t, "return false or 7")
	if got != 7 {
		t.Fatalf("Run result is %v, want 7", got)
	}
}

func TestCompileAndRunLogicalOrShortCircuitsTruthyLeft(t *testing.T) {
	proto, err := ember.Compile("return true or fail()")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	called := false
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"fail": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			called = true
			return []ember.Value{ember.NumberValue(1)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if called {
		t.Fatal("fail was called, want short-circuit")
	}
	value, ok := results[0].Bool()
	if !ok {
		t.Fatalf("Run result is %s, want boolean", results[0].Kind())
	}
	if value != true {
		t.Fatalf("Run result is %v, want true", value)
	}
}

func TestCompileAndRunLogicalAndReturnsSelectedValue(t *testing.T) {
	got := compileAndRunNumber(t, "return 5 and 8")
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunLogicalAndShortCircuitsFalseyLeft(t *testing.T) {
	proto, err := ember.Compile("return false and fail()")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	called := false
	results, err := ember.RunWithGlobals(proto, map[string]ember.Value{
		"fail": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			called = true
			return []ember.Value{ember.NumberValue(1)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if called {
		t.Fatal("fail was called, want short-circuit")
	}
	value, ok := results[0].Bool()
	if !ok {
		t.Fatalf("Run result is %s, want boolean", results[0].Kind())
	}
	if value != false {
		t.Fatalf("Run result is %v, want false", value)
	}
}

func TestCompileAndRunLogicalAndBindsTighterThanOr(t *testing.T) {
	got := compileAndRunNumber(t, "return false or true and 9")
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunNotReturnsBooleanInverse(t *testing.T) {
	got := compileAndRunValue(t, "return not false")
	value, ok := got.Bool()
	if !ok {
		t.Fatalf("Run result is %s, want boolean", got.Kind())
	}
	if value != true {
		t.Fatalf("Run result is %v, want true", value)
	}
}

func TestCompileAndRunNotUsesLuauTruthiness(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "nil", source: "return not nil", want: true},
		{name: "true", source: "return not true", want: false},
		{name: "zero", source: "return not 0", want: false},
		{name: "empty string", source: `return not ""`, want: false},
		{name: "table", source: "return not {}", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunValue(t, tt.source)
			value, ok := got.Bool()
			if !ok {
				t.Fatalf("Run result is %s, want boolean", got.Kind())
			}
			if value != tt.want {
				t.Fatalf("Run result is %v, want %v", value, tt.want)
			}
		})
	}
}

func TestCompileAndRunNotComposesWithLogicalExpressions(t *testing.T) {
	got := compileAndRunNumber(t, "return not false and 7")
	if got != 7 {
		t.Fatalf("Run result is %v, want 7", got)
	}
}

func TestCompileAndRunParenthesizedExpression(t *testing.T) {
	got := compileAndRunNumber(t, "return (1 + 2) + 3")
	if got != 6 {
		t.Fatalf("Run result is %v, want 6", got)
	}
}

func TestCompileAndRunParenthesesOverrideLogicalPrecedence(t *testing.T) {
	got := compileAndRunNumber(t, "return (true or false) and 9")
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunParenthesizedTableIndex(t *testing.T) {
	got := compileAndRunNumber(t, `
local values = {10, 20}
return values[(1 + 1)]
`)
	if got != 20 {
		t.Fatalf("Run result is %v, want 20", got)
	}
}

func TestCompileAndRunIfWithRelationalComparison(t *testing.T) {
	got := compileAndRunNumber(t, `
local score = 9
if score < 10 then
	return 1
else
	return 2
end
`)
	if got != 1 {
		t.Fatalf("Run result is %v, want 1", got)
	}
}

func TestCompileAndRunNumericForLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for i = 1, 3 do
	total = total + i
end
return total
`)
	if got != 6 {
		t.Fatalf("Run result is %v, want 6", got)
	}
}

func TestCompileAndRunNumericForLoopWithStep(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for i = 1, 5, 2 do
	total = total + i
end
return total
`)
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunNumericForLoopWithNegativeStep(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for i = 5, 1, -2 do
	total = total + i
end
return total
`)
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunNumericForLoopVariableIsLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local i = 100
local total = 0
for i = 1, 3 do
	total = total + i
end
return i + total
`)
	if got != 106 {
		t.Fatalf("Run result is %v, want 106", got)
	}
}

func TestCompileAndRunBreakExitsNumericForLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for i = 1, 5 do
	if i == 4 then
		break
	end
	total = total + i
end
return total
`)
	if got != 6 {
		t.Fatalf("Run result is %v, want 6", got)
	}
}

func TestCompileAndRunContinueSkipsNumericForLoopBody(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for i = 1, 5 do
	if i == 3 then
		continue
	end
	total = total + i
end
return total
`)
	if got != 12 {
		t.Fatalf("Run result is %v, want 12", got)
	}
}

func TestCompileAndRunNumericForLoopCoercesNumericStrings(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for i = "1", "5", "2" do
	total = total + i
end
return total
`)
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunRepeatUntilLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
repeat
	count = count + 1
until count == 3
return count
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunRepeatUntilRunsBeforeCondition(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
repeat
	count = count + 1
until true
return count
`)
	if got != 1 {
		t.Fatalf("Run result is %v, want 1", got)
	}
}

func TestCompileAndRunBreakExitsRepeatUntilLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
repeat
	count = count + 1
	if count == 3 then
		break
	end
until false
return count
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunRepeatUntilConditionSeesBodyLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
repeat
	local done = count == 2
	count = count + 1
until done
return count
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunDoBlock(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
do
	local bonus = 4
	total = bonus + 2
end
return total
`)
	if got != 6 {
		t.Fatalf("Run result is %v, want 6", got)
	}
}

func TestCompileAndRunDoBlockLocalShadowsOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local value = 10
local total = 0
do
	local value = 3
	total = value
end
return value + total
`)
	if got != 13 {
		t.Fatalf("Run result is %v, want 13", got)
	}
}

func TestRunRejectsDoBlockLocalOutsideBlock(t *testing.T) {
	proto, err := ember.Compile(`
do
	local scoped = 7
end
return scoped
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined global "scoped"`) {
		t.Fatalf("Run error is %q, want undefined scoped global", err)
	}
}

func TestCompileAndRunBreakInsideDoBlockExitsLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
while true do
	count = count + 1
	do
		if count == 3 then
			break
		end
	end
end
return count
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunGenericForPairsLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
local values = {first = 2, second = 3}
for key, value in pairs(values) do
	total = total + value
end
return total
`)
	if got != 5 {
		t.Fatalf("Run result is %v, want 5", got)
	}
}

func TestCompileAndRunGenericForIPairsLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for index, value in ipairs({2, 3, 4}) do
	total = total + index * value
end
return total
`)
	if got != 20 {
		t.Fatalf("Run result is %v, want 20", got)
	}
}

func TestCompileAndRunGenericForIPairsStopsAtFirstNil(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
local values = {10, [3] = 30}
for index, value in ipairs(values) do
	total = total + value
end
return total
`)
	if got != 10 {
		t.Fatalf("Run result is %v, want 10", got)
	}
}

func TestCompileAndRunGenericForNextTableLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
local values = {2, 3, 4}
for index, value in next, values do
	total = total + value
end
return total
`)
	if got != 9 {
		t.Fatalf("Run result is %v, want 9", got)
	}
}

func TestCompileAndRunGenericForDirectTableLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for index, value in {2, 3, 4} do
	total = total + index * value
end
return total
`)
	if got != 20 {
		t.Fatalf("Run result is %v, want 20", got)
	}
}

func TestCompileAndRunGenericForDirectLocalTableLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
local values = {hp = 10, mp = 5}
for key, value in values do
	total = total + value
end
return total
`)
	if got != 15 {
		t.Fatalf("Run result is %v, want 15", got)
	}
}

func TestCompileAndRunGenericForUsesIterMetamethod(t *testing.T) {
	got := compileAndRunNumber(t, `
local box = {items = {2, 3, 4}}
setmetatable(box, {
	__iter = function(self)
		return next, self.items
	end
})
local total = 0
for index, value in box do
	total = total + index * value
end
return total
`)
	if got != 20 {
		t.Fatalf("Run result is %v, want 20", got)
	}
}

func TestCompileAndRunGenericForPairsBypassesIterMetamethod(t *testing.T) {
	got := compileAndRunNumber(t, `
local box = {items = {2, 3}, own = 7}
setmetatable(box, {
	__iter = function(self)
		return next, self.items
	end
})
local total = 0
for key, value in pairs(box) do
	if key == "own" then
		total = total + value
	end
end
return total
`)
	if got != 7 {
		t.Fatalf("Run result is %v, want 7", got)
	}
}

func TestCompileAndRunGenericForSingleLoopVariable(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
local values = {10, 20, 30}
for index in pairs(values) do
	total = total + index
end
return total
`)
	if got != 6 {
		t.Fatalf("Run result is %v, want 6", got)
	}
}

func TestCompileAndRunBreakExitsGenericForLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
local values = {1, 2, 3, 4}
for index, value in pairs(values) do
	if index == 3 then
		break
	end
	total = total + value
end
return total
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunContinueSkipsGenericForLoopBody(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
local values = {1, 2, 3, 4}
for index, value in pairs(values) do
	if index == 2 then
		continue
	end
	total = total + value
end
return total
`)
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunGenericForLoopVariablesAreLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local index = 100
local total = 0
local values = {1, 2, 3}
for index, value in pairs(values) do
	total = total + value
end
return index + total
`)
	if got != 106 {
		t.Fatalf("Run result is %v, want 106", got)
	}
}

func TestCompileAndRunGenericForSkipsEmptyTable(t *testing.T) {
	got := compileAndRunNumber(t, `
local total = 0
for key, value in pairs({}) do
	total = total + 1
end
return total
`)
	if got != 0 {
		t.Fatalf("Run result is %v, want 0", got)
	}
}

func TestRunRejectsRepeatUntilBodyLocalOutsideLoop(t *testing.T) {
	proto, err := ember.Compile(`
repeat
	local done = true
until done
return done
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined global "done"`) {
		t.Fatalf("Run error is %q, want undefined done global", err)
	}
}

func TestCompileAndRunContinueSkipsRepeatUntilBody(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
local total = 0
repeat
	count = count + 1
	if count == 2 then
		continue
	end
	total = total + count
until count == 4
return total
`)
	if got != 8 {
		t.Fatalf("Run result is %v, want 8", got)
	}
}

func TestCompileAndRunWhileLoopMutatesOuterLocal(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
while count < 3 do
	count = count + 1
end
return count
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunWhileLoopCanRunZeroTimes(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
while count < 0 do
	count = count + 1
end
return count
`)
	if got != 0 {
		t.Fatalf("Run result is %v, want 0", got)
	}
}

func TestCompileAndRunBreakExitsWhileLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
while true do
	count = count + 1
	if count == 3 then
		break
	end
end
return count
`)
	if got != 3 {
		t.Fatalf("Run result is %v, want 3", got)
	}
}

func TestCompileAndRunBreakExitsInnermostLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local outer = 0
local inner = 0
while outer < 2 do
	outer = outer + 1
	while true do
		inner = inner + 1
		break
	end
end
return inner
`)
	if got != 2 {
		t.Fatalf("Run result is %v, want 2", got)
	}
}

func TestCompileAndRunContinueSkipsRestOfWhileBody(t *testing.T) {
	got := compileAndRunNumber(t, `
local count = 0
local total = 0
while count < 5 do
	count = count + 1
	if count == 3 then
		continue
	end
	total = total + count
end
return total
`)
	if got != 12 {
		t.Fatalf("Run result is %v, want 12", got)
	}
}

func TestCompileAndRunContinueTargetsInnermostLoop(t *testing.T) {
	got := compileAndRunNumber(t, `
local outer = 0
local hits = 0
while outer < 2 do
	outer = outer + 1
	local inner = 0
	while inner < 3 do
		inner = inner + 1
		if inner == 2 then
			continue
		end
		hits = hits + 1
	end
end
return hits
`)
	if got != 4 {
		t.Fatalf("Run result is %v, want 4", got)
	}
}

func TestCompileAndRunRelationalComparisons(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "number less", source: "return 1 < 2", want: true},
		{name: "number less equal", source: "return 2 <= 2", want: true},
		{name: "number greater", source: "return 3 > 2", want: true},
		{name: "number greater equal", source: "return 3 >= 3", want: true},
		{name: "number comparison false", source: "return 3 < 2", want: false},
		{name: "string less", source: `return "a" < "b"`, want: true},
		{name: "string greater equal", source: `return "b" >= "b"`, want: true},
		{name: "addition before relation", source: "return 1 + 2 <= 3", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunValue(t, tt.source)
			value, ok := got.Bool()
			if !ok {
				t.Fatalf("Run result is %s, want boolean", got.Kind())
			}
			if value != tt.want {
				t.Fatalf("Run result is %v, want %v", value, tt.want)
			}
		})
	}
}

func TestRunRejectsRelationalComparisonAcrossKinds(t *testing.T) {
	proto, err := ember.Compile(`return "1" < 2`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), "compare operands are string and number") {
		t.Fatalf("Run error is %q, want compare type error", err)
	}
}

func TestCompileAndRunTableIdentityEquality(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name: "same table local equals itself",
			source: `
local value = {}
return value == value
`,
			want: true,
		},
		{
			name: "different table literals are not equal",
			source: `
local left = {}
local right = {}
return left ~= right
`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileAndRunValue(t, tt.source)
			value, ok := got.Bool()
			if !ok {
				t.Fatalf("Run result is %s, want boolean", got.Kind())
			}
			if value != tt.want {
				t.Fatalf("Run result is %v, want %v", value, tt.want)
			}
		})
	}
}

func TestRunRejectsBranchLocalOutsideIfAsUndefinedGlobal(t *testing.T) {
	proto, err := ember.Compile(`
if true then
	local branch = 1
end
return branch
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined global "branch"`) {
		t.Fatalf("Run error is %q, want undefined branch global", err)
	}
}

func TestRunRejectsLoopLocalOutsideWhileAsUndefinedGlobal(t *testing.T) {
	proto, err := ember.Compile(`
while false do
	local inside = 1
end
return inside
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined global "inside"`) {
		t.Fatalf("Run error is %q, want undefined loop global", err)
	}
}

func TestCompileRejectsBreakOutsideLoop(t *testing.T) {
	_, err := ember.Compile(`
break
return 1
`)
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), "break outside loop") {
		t.Fatalf("Compile error is %q, want break outside loop", err)
	}
}

func TestCompileRejectsContinueOutsideLoop(t *testing.T) {
	_, err := ember.Compile(`
continue
return 1
`)
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), "continue outside loop") {
		t.Fatalf("Compile error is %q, want continue outside loop", err)
	}
}

func TestCompileAndRunImplicitAssignmentReturnReturnsNoValues(t *testing.T) {
	results := compileAndRunValues(t, "x = 1")
	if len(results) != 0 {
		t.Fatalf("Run returned %d results, want none", len(results))
	}
}

func TestCompileRejectsInvalidSource(t *testing.T) {
	_, err := ember.Compile("local")
	if err == nil {
		t.Fatal("Compile succeeded, want error")
	}
	if !strings.Contains(err.Error(), "expected identifier") {
		t.Fatalf("Compile error is %q, want expected identifier", err)
	}
}

func TestRunRejectsUndefinedBareGlobal(t *testing.T) {
	proto, err := ember.Compile("return missing + 1")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined global "missing"`) {
		t.Fatalf("Run error is %q, want undefined global", err)
	}
}

func TestRunRejectsSelectorAssignmentToUndefinedGlobal(t *testing.T) {
	proto, err := ember.Compile(`
missing.value = 1
return 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = ember.Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want error")
	}
	if !strings.Contains(err.Error(), `undefined global "missing"`) {
		t.Fatalf("Run error is %q, want undefined global", err)
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
