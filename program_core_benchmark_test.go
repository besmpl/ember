package ember

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkRuntimeHookCore separates the public report sink from the private
// hook execution path. The discard lanes are the measurement seam for the
// error-only path; they intentionally do not change public Runtime behavior.
func BenchmarkRuntimeHookCore(b *testing.B) {
	cases := []struct {
		name               string
		report             bool
		options            RuntimeOptions
		context            func() context.Context
		changingGlobals    bool
		multipleEntrypoint bool
	}{
		{name: "report_background", report: true},
		{name: "discard_background"},
		{name: "discard_changing_globals", changingGlobals: true},
		{name: "discard_multiple_entrypoints", multipleEntrypoint: true},
		{
			name:    "discard_bounded",
			options: RuntimeOptions{Limits: ExecutionLimits{MaxInstructions: 1 << 60}},
		},
		{
			name: "discard_cancelable",
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				b.Cleanup(cancel)
				return ctx
			},
		},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			runtime := newHookCoreBenchmarkRuntime(b, tc.options, tc.changingGlobals, tc.multipleEntrypoint)
			defer runtime.Close()
			if err := runtime.runHook(context.Background(), "startup", nil, nil); err != nil {
				b.Fatal(err)
			}
			ctx := context.Background()
			if tc.context != nil {
				ctx = tc.context()
			}
			args := []Value{NumberValue(1)}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var report *HookReport
				if tc.report {
					report = &HookReport{Hook: "update"}
				}
				if err := runtime.runHook(ctx, "update", args, report); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type hookCoreBenchmarkLoader map[string]string

func (loader hookCoreBenchmarkLoader) LoadModule(ctx context.Context, id ModuleID) (Source, error) {
	if err := ctx.Err(); err != nil {
		return Source{}, err
	}
	text, ok := loader[id.String()]
	if !ok {
		return Source{}, fmt.Errorf("hook core benchmark: missing module %s", id.String())
	}
	return Source{Name: id.String(), Text: text}, nil
}

func newHookCoreBenchmarkRuntime(tb testing.TB, options RuntimeOptions, changingGlobals, multipleEntrypoint bool) *Runtime {
	tb.Helper()
	source := `
local state = 0
return {
    startup = function()
        state = 0
    end,
    update = function(delta)
        state = state + delta
    end,
}
`
	if changingGlobals {
		source = `
local state = 0
return {
    startup = function()
        state = 0
    end,
    update = function(delta)
        state = state + delta + host_tick
    end,
}
`
	}
	modules := map[string]string{}
	entrypoints := []Entrypoint{}
	if multipleEntrypoint {
		for _, name := range []string{"a", "b"} {
			module := LogicalModule("benchmark/hook-core/" + name)
			modules[module.String()] = source
			entrypoints = append(entrypoints, Entrypoint{Name: name, Module: module})
		}
	} else {
		module := LogicalModule("benchmark/hook-core")
		modules[module.String()] = source
		entrypoints = append(entrypoints, Entrypoint{Name: "benchmark", Module: module})
	}
	var host RuntimeHost
	if changingGlobals {
		next := 0.0
		host = RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
			next++
			return map[string]Value{"host_tick": NumberValue(next)}, nil
		})
	}
	program, _, err := LoadProgram(context.Background(), hookCoreBenchmarkLoader(modules), ProgramOptions{
		Entrypoints: entrypoints,
		Parallelism: 1,
	})
	if err != nil {
		tb.Fatal(err)
	}
	options.Host = host
	runtime, err := program.NewRuntime(options)
	if err != nil {
		tb.Fatal(err)
	}
	return runtime
}
