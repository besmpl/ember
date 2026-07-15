package ember_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/besmpl/ember"
)

const runtimeLaneArithmeticSource = `
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2
`

var runtimeLaneResultsSink []ember.Value
var runtimeLaneRetainedResultSink ember.Value

type runtimeLaneWorkload struct {
	name       string
	stateless  string
	persistent string
}

var runtimeLaneWorkloads = []runtimeLaneWorkload{
	{
		name: "scalar_arithmetic",
		stateless: `
local total = 0
for i = 1, 200 do
    total = total + ((i * 3 - i // 2) % 17)
end
return total
`,
		persistent: `
local state = {ticks = 0, total = 0}
return {
    startup = function()
        state.ticks = 0
        state.total = 0
    end,
    update = function(delta)
        state.ticks = state.ticks + delta
        local total = 0
        for i = 1, 30 do
            total = total + ((i * 3 - i // 2) % 17)
        end
        state.total = state.total + total
    end,
    verify = function()
        record(state.ticks, state.total)
    end,
}
`,
	},
	{
		name: "recursive_calls",
		stateless: `
local function fib(n)
    if n < 2 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
return fib(18)
`,
		persistent: `
local state = {ticks = 0, total = 0}
local function fib(n)
    if n < 2 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
return {
    startup = function()
        state.ticks = 0
        state.total = 0
    end,
    update = function(delta)
        state.ticks = state.ticks + delta
        state.total = state.total + fib(12)
    end,
    verify = function()
        record(state.ticks, state.total)
    end,
}
`,
	},
	{
		name: "nested_table_mutation",
		stateless: `
local state = {outer = {inner = {value = 0}}}
for i = 1, 100 do
    state.outer.inner.value = state.outer.inner.value + i
end
return state.outer.inner.value
`,
		persistent: `
local state = {ticks = 0, root = {outer = {inner = {value = 0}}}}
return {
    startup = function()
        state.ticks = 0
        state.root.outer.inner.value = 0
    end,
    update = function(delta)
        state.ticks = state.ticks + delta
        local inner = state.root.outer.inner
        for i = 1, 8 do
            inner.value = inner.value + i + delta
        end
    end,
    verify = function()
        record(state.ticks, state.root.outer.inner.value)
    end,
}
`,
	},
	{
		name: "dynamic_string_growth",
		stateless: `
local cells = {["0:0"] = {heat = 1}}
local total = 0
for x = 0, 12 do
    for y = 0, 12 do
        local key = tostring(x) .. ":" .. tostring(y)
        local cell = cells[key]
        if cell == nil then
            cells[key] = {heat = x + y}
            total = total + cells[key].heat
        else
            cell.heat = cell.heat + 1
            total = total + cell.heat
        end
    end
end
return total
`,
		persistent: `
local state = {ticks = 0, cells = {["0:0"] = {heat = 1}}, total = 0}
return {
    startup = function()
        state.ticks = 0
        state.total = 0
    end,
    update = function(delta)
        state.ticks = state.ticks + delta
        for x = 0, 4 do
            for y = 0, 4 do
                local key = tostring(x) .. ":" .. tostring(y)
                local cell = state.cells[key]
                if cell == nil then
                    state.cells[key] = {heat = x + y + delta}
                    state.total = state.total + state.cells[key].heat
                else
                    cell.heat = cell.heat + delta
                    state.total = state.total + cell.heat
                end
            end
        end
    end,
    verify = function()
        record(state.ticks, state.total)
    end,
}
`,
	},
}

// BenchmarkRuntimeLaneStatelessRun measures one-shot execution after compile.
func BenchmarkRuntimeLaneStatelessRun(b *testing.B) {
	for _, workload := range runtimeLaneWorkloads {
		b.Run(workload.name, func(b *testing.B) {
			proto := benchmarkCompile(b, workload.stateless)
			results, err := ember.Run(proto)
			if err != nil {
				b.Fatal(err)
			}
			runtimeLaneResultsSink = results
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				results, err := ember.Run(proto)
				if err != nil {
					b.Fatal(err)
				}
				runtimeLaneResultsSink = results
			}
		})
	}
}

// BenchmarkRuntimeLaneRetainedResult keeps one table returned by a stateless
// run alive while later runs execute. This models a host retaining a result
// beyond the call that produced it without retaining every transient result.
func BenchmarkRuntimeLaneRetainedResult(b *testing.B) {
	proto := benchmarkCompile(b, `
return {
    nested = {value = 42},
    cells = {alpha = {score = 7}},
}
`)
	preflight, err := ember.Run(proto)
	if err != nil {
		b.Fatal(err)
	}
	if len(preflight) != 1 {
		b.Fatalf("retained result count = %d, want 1", len(preflight))
	}
	retained := preflight[0]
	retainedTable, ok := retained.Table()
	if !ok || retainedTable == nil {
		b.Fatalf("retained result = %v, want table", retained)
	}
	runtimeLaneRetainedResultSink = retained

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		results, err := ember.Run(proto)
		if err != nil {
			b.Fatal(err)
		}
		runtimeLaneResultsSink = results
	}
	b.StopTimer()

	nested, err := retainedTable.Get(ember.StringValue("nested"))
	if err != nil {
		b.Fatal(err)
	}
	nestedTable, ok := nested.Table()
	if !ok || nestedTable == nil {
		b.Fatalf("retained nested result = %v, want table", nested)
	}
	value, err := nestedTable.Get(ember.StringValue("value"))
	if err != nil {
		b.Fatal(err)
	}
	if got, ok := value.Number(); !ok || got != 42 {
		b.Fatalf("retained nested value = %v, want 42", value)
	}
}

// BenchmarkRuntimeLanePersistentRuntimeRunHook measures updates on one
// Runtime after loading the program and running startup outside timing.
func BenchmarkRuntimeLanePersistentRuntimeRunHook(b *testing.B) {
	for _, workload := range runtimeLaneWorkloads {
		b.Run(workload.name, func(b *testing.B) {
			benchmarkRuntimeLaneRunHook(b, workload, ember.RuntimeOptions{}, context.Background())
		})
	}
}

// BenchmarkRuntimeLaneBoundedRuntimeRunHook measures the same persistent
// updates with a high instruction budget that never trips.
func BenchmarkRuntimeLaneBoundedRuntimeRunHook(b *testing.B) {
	for _, workload := range runtimeLaneWorkloads {
		b.Run(workload.name, func(b *testing.B) {
			benchmarkRuntimeLaneRunHook(b, workload, ember.RuntimeOptions{MaxInstructions: 1 << 60}, context.Background())
		})
	}
}

// BenchmarkRuntimeLaneCancelableRuntimeRunHook measures the same persistent
// updates with a live, non-cancelled context that has a Done channel.
func BenchmarkRuntimeLaneCancelableRuntimeRunHook(b *testing.B) {
	for _, workload := range runtimeLaneWorkloads {
		b.Run(workload.name, func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.Background())
			_ = ctx.Done()
			defer cancel()
			benchmarkRuntimeLaneRunHook(b, workload, ember.RuntimeOptions{}, ctx)
		})
	}
}

type runtimeLaneVerifier struct {
	values []ember.Value
}

func newRuntimeLaneVerifier() (ember.RuntimeHost, *runtimeLaneVerifier) {
	verifier := &runtimeLaneVerifier{}
	host := ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
		if call.Hook != "verify" {
			return nil, nil
		}
		return map[string]ember.Value{
			"record": ember.HostFuncValue(verifier.record),
		}, nil
	})
	return host, verifier
}

func (verifier *runtimeLaneVerifier) record(args []ember.Value) ([]ember.Value, error) {
	verifier.values = append(verifier.values[:0], args...)
	return nil, nil
}

func (verifier *runtimeLaneVerifier) validate(tb testing.TB, updates int) {
	tb.Helper()
	if len(verifier.values) < 1 {
		tb.Fatalf("verify hook recorded %d values, want at least 1", len(verifier.values))
	}
	ticks, ok := verifier.values[0].Number()
	if !ok || ticks != float64(updates) {
		tb.Fatalf("persistent ticks = %v, want %d", verifier.values[0], updates)
	}
}

func benchmarkRuntimeLaneRunHook(b *testing.B, workload runtimeLaneWorkload, options ember.RuntimeOptions, ctx context.Context) {
	b.Helper()
	host, verifier := newRuntimeLaneVerifier()
	options.Host = host
	runtime := newRuntimeLaneWithOptions(b, workload.persistent, options)
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	updates := 0
	for b.Loop() {
		if _, err := runtime.RunHook(ctx, "update", ember.NumberValue(1)); err != nil {
			b.Fatal(err)
		}
		updates++
	}
	b.StopTimer()
	if _, err := runtime.RunHook(context.Background(), "verify"); err != nil {
		b.Fatal(err)
	}
	verifier.validate(b, updates)
}

// BenchmarkRuntimeLaneCompileRun measures the cold compile-and-run lifecycle.
// Compilation is intentionally inside the timed region; this is the lane a
// script loader pays when it has no retained prototype.
func BenchmarkRuntimeLaneCompileRun(b *testing.B) {
	for b.Loop() {
		proto, err := ember.Compile(runtimeLaneArithmeticSource)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRuntimeLaneProto measures the public Proto lifecycle with compile
// outside timing. Run still includes public entry, globals, and root-frame
// setup, so it is not a pure dispatcher measurement.
func BenchmarkRuntimeLaneProto(b *testing.B) {
	proto := benchmarkCompile(b, runtimeLaneArithmeticSource)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRuntimeLanePersistent measures repeated hooks on one Runtime. The
// Program, Runtime, module graph, and startup state are all constructed before
// timing, so update calls observe the same script-owned state.
func BenchmarkRuntimeLanePersistent(b *testing.B) {
	runtime := newRuntimeLane(b, `
local state = {ready = false, frames = 0}
return {
    startup = function()
        state.ready = true
    end,
    update = function(delta)
        if state.ready then
            state.frames = state.frames + delta
        end
    end,
}
`, nil)
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := runtime.RunHook(context.Background(), "update", ember.NumberValue(1)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRuntimeLaneSteadyState measures a wrapper that invokes the same
// case function N times inside one Ember execution. The sub-benchmarks retain
// the four parity points used by runtime_parity_test.go.
func BenchmarkRuntimeLaneSteadyState(b *testing.B) {
	for _, iterations := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", iterations), func(b *testing.B) {
			proto := benchmarkCompile(b, fmt.Sprintf(`
local function caseFn()
    local x = 1
    local y = 2
    return (x + y) * 3 - 4 / 2
end
local result = nil
for i = 1, %d do
    result = caseFn()
end
return result
`, iterations))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := ember.Run(proto); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRuntimeLaneHostBoundary keeps host callback costs separate from
// the scalar VM lanes. The callback, context, target table, and Runtime are
// all created outside their timed loops.
func BenchmarkRuntimeLaneHostBoundary(b *testing.B) {
	b.Run("RunWithGlobals/host_func", func(b *testing.B) {
		proto := benchmarkCompile(b, `return host(41)`)
		host := ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("host: got %d args", len(args))
			}
			return []ember.Value{args[0]}, nil
		})
		globals := map[string]ember.Value{"host": host}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := ember.RunWithGlobals(proto, globals); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RuntimeRunHook/runtime_host", func(b *testing.B) {
		host := ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
			return map[string]ember.Value{
				"host": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					return []ember.Value{ember.NumberValue(float64(len(args)))}, nil
				}),
			}, nil
		})
		runtime := newRuntimeLane(b, `
return {
    update = function()
        return host(1, 2)
    end,
}
`, host)
		defer runtime.Close()
		if _, err := runtime.RunHook(context.Background(), "update"); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := runtime.RunHook(context.Background(), "update"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RunWithGlobals/context_host_func", func(b *testing.B) {
		proto := benchmarkCompile(b, `return host()`)
		host := ember.ContextHostFuncValue(func(ctx context.Context, _ []ember.Value) ([]ember.Value, error) {
			if ctx == nil {
				return nil, errors.New("host: nil context")
			}
			return []ember.Value{ember.NumberValue(1)}, nil
		})
		globals := map[string]ember.Value{"host": host}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := ember.RunWithGlobals(proto, globals); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RunWithGlobals/error_host_func", func(b *testing.B) {
		proto := benchmarkCompile(b, `return host()`)
		hostErr := errors.New("expected host error")
		host := ember.HostFuncValue(func([]ember.Value) ([]ember.Value, error) {
			return nil, hostErr
		})
		globals := map[string]ember.Value{"host": host}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := ember.RunWithGlobals(proto, globals); !errors.Is(err, hostErr) {
				b.Fatalf("RunWithGlobals error = %v, want %v", err, hostErr)
			}
		}
	})

	b.Run("RunWithGlobals/table_mutation", func(b *testing.B) {
		proto := benchmarkCompile(b, `return mutate(target)`)
		target := ember.NewTable()
		if err := target.Set(ember.StringValue("value"), ember.NumberValue(0)); err != nil {
			b.Fatal(err)
		}
		mutate := ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("mutate: got %d args", len(args))
			}
			table, ok := args[0].Table()
			if !ok || table == nil {
				return nil, errors.New("mutate: target is not a table")
			}
			current, _ := table.Get(ember.StringValue("value"))
			number, _ := current.Number()
			if err := table.Set(ember.StringValue("value"), ember.NumberValue(number+1)); err != nil {
				return nil, err
			}
			return nil, nil
		})
		globals := map[string]ember.Value{
			"target": ember.TableValue(target),
			"mutate": mutate,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := ember.RunWithGlobals(proto, globals); err != nil {
				b.Fatal(err)
			}
		}
	})
}

type runtimeLaneLoader map[string]string

func (loader runtimeLaneLoader) LoadModule(ctx context.Context, id ember.ModuleID) (ember.Source, error) {
	if err := ctx.Err(); err != nil {
		return ember.Source{}, err
	}
	name := id.String()
	source, ok := loader[name]
	if !ok {
		return ember.Source{}, fmt.Errorf("runtime lane: missing module %s", name)
	}
	return ember.Source{Name: name, Text: source}, nil
}

func newRuntimeLane(tb testing.TB, source string, host ember.RuntimeHost) *ember.Runtime {
	return newRuntimeLaneWithOptions(tb, source, ember.RuntimeOptions{Host: host})
}

func newRuntimeLaneWithOptions(tb testing.TB, source string, options ember.RuntimeOptions) *ember.Runtime {
	tb.Helper()
	module := ember.LogicalModule("benchmark/init")
	program, _, err := ember.LoadProgram(context.Background(), runtimeLaneLoader{
		module.String(): source,
	}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "benchmark", Module: module}},
		Parallelism: 1,
	})
	if err != nil {
		tb.Fatal(err)
	}
	runtime, err := program.NewRuntime(options)
	if err != nil {
		tb.Fatal(err)
	}
	return runtime
}

func BenchmarkCompileArithmetic(b *testing.B) {
	for b.Loop() {
		if _, err := ember.Compile(`
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2
`); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunArithmeticProduction(b *testing.B) {
	proto := benchmarkCompile(b, `
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableFields(b *testing.B) {
	proto := benchmarkCompile(b, `
local player = {stats = {hp = 10, shield = 4}}
player.stats.hp = player.stats.hp + player.stats.shield
return player.stats.hp
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunFunctionCalls(b *testing.B) {
	proto := benchmarkCompile(b, `
local function add(left, right)
    return left + right
end
return add(add(1, 2), add(3, 4))
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunRecursiveScriptCalls(b *testing.B) {
	proto := benchmarkCompile(b, `
local function sum(n)
    if n == 0 then
        return 0
    end
    return n + sum(n - 1)
end
return sum(12)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunWhileLoopProduction(b *testing.B) {
	proto := benchmarkCompile(b, `
local i = 0
local total = 0
while i < 20 do
    i = i + 1
    total = total + i
end
return total
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunMetatableIndex(b *testing.B) {
	proto := benchmarkCompile(b, `
local fallback = {hp = 10}
local player = setmetatable({}, {__index = fallback})
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + player.hp
end
return total
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunArrayLiteral(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6, 7, 8}
return values[1] + values[8]
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableInsert(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {}
local i = 0
while i < 20 do
    i = i + 1
    table.insert(values, i)
end
return rawlen(values)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableRemove(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6, 7, 8}
table.remove(values, 4)
return values[4], rawlen(values)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableUnpack(b *testing.B) {
	proto := benchmarkCompile(b, `
local a, b, c, d = table.unpack({1, 2, 3, 4})
return a + b + c + d
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunRawLength(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6, 7, 8}
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + rawlen(values)
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunStringFieldReads(b *testing.B) {
	proto := benchmarkCompile(b, `
local player = {hp = 10, shield = 4}
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + player.hp + player.shield
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunGlobalAccess(b *testing.B) {
	proto := benchmarkCompile(b, `
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + math.abs(-i)
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunMethodCalls(b *testing.B) {
	proto := benchmarkCompile(b, `
local counter = {value = 0}
function counter:add(amount)
    self.value = self.value + amount
    return self.value
end
return counter:add(1) + counter:add(2)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunIteration(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6}
local total = 0
for _, value in values do
    total = total + value
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkCompile(tb testing.TB, source string) *ember.Proto {
	tb.Helper()
	proto, err := ember.Compile(source)
	if err != nil {
		tb.Fatal(err)
	}
	return proto
}
