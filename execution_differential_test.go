package ember

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

// executionDifferentialCase describes one source-level behavior slice. The
// source is compiled once, then production and instrumented direct loops
// receive fresh host state so comparisons include observable effects without
// sharing state.
type executionDifferentialCase struct {
	name           string
	source         string
	args           []Value
	withGlobals    bool
	withOwner      bool
	coroutineLimit uint32
	limits         ExecutionLimits
	cancel         bool
	check          func(*testing.T, differentialRun)
	wantOps        []string
}

type differentialRun struct {
	values  []Value
	err     error
	globals map[string]Value
	events  []string
}

var errDifferentialHost = errors.New("differential host failure")

func TestExecutionDifferentialCorpus(t *testing.T) {
	corpus := executionDifferentialCorpus()
	for _, test := range corpus {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			if missing := missingDifferentialOps(proto, test.wantOps); len(missing) != 0 {
				t.Fatalf("compiled source is missing opcode families %v\ndisassembly:\n%s", missing, strings.Join(differentialDisassembly(proto), "\n"))
			}
			direct := runDifferentialCase(proto, test, false)
			instrumented := runDifferentialCase(proto, test, true)
			assertDifferentialEquivalent(t, test, proto, "direct-instrumented", direct, instrumented)
			if test.check != nil {
				test.check(t, direct)
			}
		})
	}
}

func executionDifferentialCorpus() []executionDifferentialCase {
	return []executionDifferentialCase{
		{
			name: "scalar arithmetic and control",
			source: `
local total = 0
for i = 1, 6 do
	if i % 2 == 0 then total = total + i * 2 else total = total + i end
end
return total > 20, total
`,
		},
		{
			name:      "owner backed scalar execution",
			source:    `local total = 0 for i = 1, 8 do total = total + i end return total`,
			withOwner: true,
		},
		{
			name: "direct loop behavior",
			source: `
local rows = {{cooldown = 2}, {cooldown = 0}, {cooldown = 3}}
local total = 0
for _, row in rows do
	if row.cooldown > 0 then row.cooldown = row.cooldown - 1 end
	total = total + row.cooldown
end
return total
`,
		},
		{
			name:    "scalar arithmetic operators",
			source:  `local function calculate(left, right) local difference = left - right local quotient = left / right local floor = left // right local power = left ^ right local negated = -left return difference, quotient, floor, power, negated end return calculate(9, 3)`,
			wantOps: []string{"SUB", "DIV", "IDIV", "POW", "NEG"},
		},
		{
			name:    "scalar logic length and concat",
			source:  `local function inspect(text, flag) return not flag, (flag or true), #text, text .. "!" end return inspect("ember", false)`,
			wantOps: []string{"JUMP_IF_FALSE", "LEN", "CONCAT"},
		},
		{
			name: "calls returns and varargs",
			source: `
local function collect(first, ...)
		local sum = first
		for i = 1, select("#", ...) do sum = sum + select(i, ...) end
		return sum, ...
	end
local total, second, third = collect(2, 3, 4)
return total, second, third
`,
		},
		{
			name:   "nested calls",
			source: `local function add(left, right) return left + right end return add(add(1, 2), add(3, 4))`,
		},
		{
			name:   "self recursive calls",
			source: `local function loop(value) if value <= 0 then return 0 end return loop(value - 1) end return loop(3)`,
		},
		{
			name:        "globals and upvalues",
			source:      `local function bump() seed = seed + 1 return seed end return bump(), bump(), seed`,
			withGlobals: true,
			check: func(t *testing.T, run differentialRun) {
				if got := run.globals["seed"]; !valuesEquivalent(got, NumberValue(3), newTableComparison()) {
					t.Fatalf("global seed = %s, want 3", valueDiagnostic(got))
				}
			},
		},
		{
			name: "tables metatables and identity",
			source: `
local fallback = {missing = 7}
local value = setmetatable({left = 2}, {__index = fallback})
value.right = value.left + value.missing
return value, value, value.right, getmetatable(value) == getmetatable(value)
`,
			check: func(t *testing.T, run differentialRun) {
				if len(run.values) < 2 {
					t.Fatalf("table result count = %d, want at least 2", len(run.values))
				}
				left, leftOK := run.values[0].Table()
				right, rightOK := run.values[1].Table()
				if !leftOK || !rightOK || left != right {
					t.Fatalf("returned table identity not preserved: %#v", run.values[:2])
				}
			},
		},
		{
			name: "protected calls",
			source: `
local ok, value = pcall(function() return 2 + 3 end)
local failed, message = pcall(function() return "bad" + 1 end)
return ok, value, failed, type(message)
`,
		},
		{
			name: "coroutines",
			source: `
local co = coroutine.create(function(seed)
		local resumed = coroutine.yield("pause", seed + 1)
		return resumed + 2
end)
local ok1, label, first = coroutine.resume(co, 4)
local ok2, final = coroutine.resume(co, 8)
return ok1, label, first, ok2, final, coroutine.status(co)
`,
		},
		{
			name:        "host callback sequence and side effects",
			source:      `record("start", seed) local value = record("finish", seed + 1) return value`,
			withGlobals: true,
		},
		{
			name:   "runtime error line stack",
			source: "local function boom()\n\treturn \"bad\" + 1\nend\nreturn boom()",
		},
		{
			name:        "host error cause",
			source:      `return fail()`,
			withGlobals: true,
		},
		{
			name:   "instruction limit",
			source: `local total = 0 for i = 1, 100 do total = total + i end return total`,
			limits: ExecutionLimits{MaxInstructions: 8},
		},
		{
			name:   "call depth limit",
			source: `local function loop() return loop() end return loop()`,
			limits: ExecutionLimits{MaxCallDepth: 4},
		},
		{
			name:   "generated string limit",
			source: `local suffix = "runtime" return tostring(123456) .. suffix`,
			limits: ExecutionLimits{MaxGeneratedStringBytes: 4},
		},
		{
			name:   "table entries limit",
			source: `local value = {} value.a = 1 value.b = 2 return value`,
			limits: ExecutionLimits{MaxTableEntriesPerTable: 1},
		},
		{
			name:   "runtime objects limit",
			source: `local first = {} local second = {} return first, second`,
			limits: ExecutionLimits{MaxRuntimeObjects: 1},
		},
		{
			name:           "coroutine capacity limit",
			source:         `local f = function() return 1 end local a = coroutine.create(f) local b = coroutine.create(f) return a, b`,
			coroutineLimit: 1,
		},
		{
			name:   "cancellation",
			source: `return 1 + 2`,
			cancel: true,
		},
	}
}

// Module initialization is charged by Program/Runtime require-graph
// orchestration outside executeProto. The direct-loop differential corpus
// cannot observe that boundary; runtime_budget_b8_test.go covers it.
func TestExecutionDifferentialCorpusScope(t *testing.T) {
	t.Log("MaxModuleInitializations is covered by runtime orchestration tests")
}

func differentialDisassembly(proto *Proto) []string {
	if proto == nil {
		return nil
	}
	lines := disassembleProto(proto)
	for _, child := range proto.prototypes {
		lines = append(lines, differentialDisassembly(child)...)
	}
	return lines
}

func missingDifferentialOps(proto *Proto, want []string) []string {
	if len(want) == 0 {
		return nil
	}
	joined := strings.Join(differentialDisassembly(proto), "\n")
	missing := make([]string, 0, len(want))
	for _, op := range want {
		if !strings.Contains(joined, op) {
			missing = append(missing, op)
		}
	}
	return missing
}

func runDifferentialCase(proto *Proto, test executionDifferentialCase, instrumented bool) differentialRun {
	var events []string
	globals := map[string]Value(nil)
	if test.withGlobals {
		globals = map[string]Value{
			"seed": NumberValue(1),
			"fail": HostFuncValue(func([]Value) ([]Value, error) {
				return nil, errDifferentialHost
			}),
			"record": HostFuncValue(func(args []Value) ([]Value, error) {
				parts := make([]string, 0, len(args))
				for _, arg := range args {
					parts = append(parts, valueDiagnostic(arg))
				}
				events = append(events, strings.Join(parts, ":"))
				return []Value{NumberValue(float64(len(events)))}, nil
			}),
		}
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if test.cancel {
		ctx, cancel = context.WithCancel(ctx)
		cancel()
	}
	var controller *executionController
	var owner *runtimeOwner
	if test.withOwner || test.coroutineLimit != 0 {
		owner = newRuntimeOwner()
		owner.coroutineLimit = test.coroutineLimit
		defer owner.close()
	}
	if test.limits != (ExecutionLimits{}) || test.cancel {
		controller, _ = newExecutionController(ctx, test.limits)
	}
	var env *globalEnv
	if owner != nil {
		env = runtimeGlobalsWithOwner(globals, owner)
	} else {
		env = runtimeGlobalsOrNil(globals)
	}
	values, err := executeProto(ctx, proto, env, executeOptions{
		args:         test.args,
		controller:   controller,
		instrumented: instrumented,
	})
	return differentialRun{values: values, err: err, globals: globals, events: events}
}

func runtimeGlobalsOrNil(globals map[string]Value) *globalEnv {
	if globals == nil {
		return nil
	}
	return runtimeGlobals(globals)
}

func assertDifferentialEquivalent(t *testing.T, test executionDifferentialCase, proto *Proto, mode string, want, got differentialRun) {
	t.Helper()
	if difference := differentialDifference(want, got); difference != "" {
		t.Fatalf("execution mismatch mode=%s source=%q\n%s\ndisassembly:\n%s\nreference values=%s error=%s\nactual values=%s error=%s", mode, test.name, difference, strings.Join(differentialDisassembly(proto), "\n"), valuesDiagnostic(want.values), errorDiagnostic(want.err), valuesDiagnostic(got.values), errorDiagnostic(got.err))
	}
}

func differentialDifference(want, got differentialRun) string {
	if !valuesSliceEquivalent(want.values, got.values) {
		return "result values differ"
	}
	if difference := errorsEquivalent(want.err, got.err); difference != "" {
		return "errors differ: " + difference
	}
	if !valuesMapEquivalent(want.globals, got.globals) {
		return "global side effects differ"
	}
	if !reflect.DeepEqual(want.events, got.events) {
		return fmt.Sprintf("host callback sequence differs: want %v got %v", want.events, got.events)
	}
	return ""
}

func errorsEquivalent(want, got error) string {
	if want == nil || got == nil {
		if want == nil && got == nil {
			return ""
		}
		return fmt.Sprintf("want %T, got %T", want, got)
	}
	if reflect.TypeOf(want) != reflect.TypeOf(got) {
		return fmt.Sprintf("want %T, got %T", want, got)
	}
	for _, sentinel := range []error{ErrLimitExceeded, context.Canceled, errDifferentialHost} {
		if errors.Is(want, sentinel) != errors.Is(got, sentinel) {
			return fmt.Sprintf("errors.Is(%v) differs", sentinel)
		}
	}
	var wantLimit, gotLimit *LimitError
	if errors.As(want, &wantLimit) && errors.As(got, &gotLimit) {
		if wantLimit.Kind != gotLimit.Kind || wantLimit.Limit != gotLimit.Limit || wantLimit.Used != gotLimit.Used {
			return fmt.Sprintf("limit kind/limit/used want %s/%d/%d got %s/%d/%d", wantLimit.Kind, wantLimit.Limit, wantLimit.Used, gotLimit.Kind, gotLimit.Limit, gotLimit.Used)
		}
		return ""
	}
	var wantRuntime, gotRuntime *RuntimeError
	if errors.As(want, &wantRuntime) && errors.As(got, &gotRuntime) {
		if wantRuntime.Message != gotRuntime.Message || !reflect.DeepEqual(wantRuntime.Frames, gotRuntime.Frames) {
			return fmt.Sprintf("runtime stacks want %v got %v", wantRuntime.Frames, gotRuntime.Frames)
		}
		if (wantRuntime.Cause == nil) != (gotRuntime.Cause == nil) || (wantRuntime.Cause != nil && reflect.TypeOf(wantRuntime.Cause) != reflect.TypeOf(gotRuntime.Cause)) {
			return fmt.Sprintf("runtime causes want %T got %T", wantRuntime.Cause, gotRuntime.Cause)
		}
		return ""
	}
	if want.Error() != got.Error() {
		return fmt.Sprintf("want %q, got %q", want.Error(), got.Error())
	}
	return ""
}

type tablePair struct {
	want *Table
	got  *Table
}

type tableComparison struct {
	pairs     map[tablePair]bool
	wantToGot map[*Table]*Table
	gotToWant map[*Table]*Table
}

func newTableComparison() *tableComparison {
	return &tableComparison{
		pairs:     make(map[tablePair]bool),
		wantToGot: make(map[*Table]*Table),
		gotToWant: make(map[*Table]*Table),
	}
}

func valuesSliceEquivalent(want, got []Value) bool {
	if len(want) != len(got) {
		return false
	}
	comparison := newTableComparison()
	for index := range want {
		if !valuesEquivalent(want[index], got[index], comparison) {
			return false
		}
	}
	return true
}

func valuesMapEquivalent(want, got map[string]Value) bool {
	if len(want) != len(got) {
		return false
	}
	comparison := newTableComparison()
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok || !valuesEquivalent(wantValue, gotValue, comparison) {
			return false
		}
	}
	return true
}

func valuesEquivalent(want, got Value, comparison *tableComparison) bool {
	if want.Kind() != got.Kind() {
		return false
	}
	switch want.Kind() {
	case NilKind:
		return true
	case BoolKind:
		left, lok := want.Bool()
		right, rok := got.Bool()
		return lok && rok && left == right
	case NumberKind:
		left, lok := want.Number()
		right, rok := got.Number()
		return lok && rok && math.Float64bits(left) == math.Float64bits(right)
	case StringKind:
		left, lok := want.String()
		right, rok := got.String()
		return lok && rok && left == right
	case TableKind:
		left, lok := want.Table()
		right, rok := got.Table()
		if !lok || !rok {
			return false
		}
		if mapped, ok := comparison.wantToGot[left]; ok && mapped != right {
			return false
		}
		if mapped, ok := comparison.gotToWant[right]; ok && mapped != left {
			return false
		}
		comparison.wantToGot[left] = right
		comparison.gotToWant[right] = left
		pair := tablePair{want: left, got: right}
		if comparison.pairs[pair] {
			return true
		}
		comparison.pairs[pair] = true
		if len(left.array) != len(right.array) || len(left.stringFields) != len(right.stringFields) || left.hashFieldCount() != right.hashFieldCount() {
			return false
		}
		for index := range left.array {
			if !valuesEquivalent(left.array[index], right.array[index], comparison) {
				return false
			}
		}
		for index := range left.stringFields {
			if left.stringFields[index].key != right.stringFields[index].key || !valuesEquivalent(left.stringFields[index].value, right.stringFields[index].value, comparison) {
				return false
			}
		}
		leftFields, rightFields := left.hashFields(), right.hashFields()
		if (leftFields == nil) != (rightFields == nil) {
			return false
		}
		if leftFields != nil {
			for _, entry := range leftFields.entries {
				if entry.state != tableHashFull {
					continue
				}
				value, err := right.rawGetKey(entry.key)
				if err != nil || value.IsNil() || !valuesEquivalent(entry.value, value, comparison) {
					return false
				}
			}
		}
		if (left.metatable == nil) != (right.metatable == nil) {
			return false
		}
		if left.metatable != nil && !valuesEquivalent(TableValue(left.metatable), TableValue(right.metatable), comparison) {
			return false
		}
		return true
	default:
		// Functions, userdata, and host callbacks are opaque. Their kind is the
		// observable contract for the differential corpus.
		return true
	}
}

func valueDiagnostic(value Value) string {
	switch value.Kind() {
	case NilKind:
		return "nil"
	case BoolKind:
		v, _ := value.Bool()
		return fmt.Sprintf("bool(%t)", v)
	case NumberKind:
		v, _ := value.Number()
		return fmt.Sprintf("number(%g)", v)
	case StringKind:
		v, _ := value.String()
		return fmt.Sprintf("string(%q)", v)
	case TableKind:
		v, _ := value.Table()
		return fmt.Sprintf("table(%p array=%d string=%d hash=%d metatable=%t)", v, len(v.array), len(v.stringFields), v.hashFieldCount(), v.metatable != nil)
	default:
		return value.Kind().String()
	}
}

func valuesDiagnostic(values []Value) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = valueDiagnostic(value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func errorDiagnostic(err error) string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T(%v)", err, err)
}
