package ember_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/besmpl/ember"
)

type allocationMeasure struct {
	bytes         uint64
	allocs        uint64
	ns            uint64
	arenaRetained uint64
	rootRetained  uint64
}

type allocationLoader map[string]string

var runtimeAllocationProtoSink *ember.Proto

func (loader allocationLoader) LoadModule(ctx context.Context, id ember.ModuleID) (ember.Source, error) {
	if err := ctx.Err(); err != nil {
		return ember.Source{}, err
	}
	text, ok := loader[id.String()]
	if !ok {
		return ember.Source{}, fmt.Errorf("allocation capture: missing module %s", id)
	}
	return ember.Source{Name: id.String(), Text: text}, nil
}

func measureAllocation(tb testing.TB, fn func() error) allocationMeasure {
	tb.Helper()
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := fn(); err != nil {
				b.Fatal(err)
			}
		}
	})
	return allocationMeasure{
		bytes:  uint64(result.AllocedBytesPerOp()),
		allocs: uint64(result.AllocsPerOp()),
		ns:     uint64(result.NsPerOp()),
	}
}

func measureAllocationOnce(tb testing.TB, fn func() error) allocationMeasure {
	tb.Helper()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	if err := fn(); err != nil {
		tb.Fatal(err)
	}
	elapsed := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return allocationMeasure{
		bytes:  after.TotalAlloc - before.TotalAlloc,
		allocs: after.Mallocs - before.Mallocs,
		ns:     uint64(elapsed.Nanoseconds()),
	}
}

func loadAllocationProgram(tb testing.TB, modules allocationLoader, entry ember.ModuleID, host ember.RuntimeHost) (*ember.Program, *ember.Runtime) {
	tb.Helper()
	program, _, err := ember.LoadProgram(context.Background(), modules, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "allocation", Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		tb.Fatal(err)
	}
	var owner *ember.Runtime
	if host != nil {
		owner, err = program.NewRuntime(ember.RuntimeOptions{Host: host})
		if err != nil {
			tb.Fatal(err)
		}
	}
	return program, owner
}

func newAllocationRuntime(tb testing.TB, source string, options ember.RuntimeOptions) (*ember.Program, *ember.Runtime) {
	tb.Helper()
	entry := ember.LogicalModule("allocation/init")
	program, _, err := ember.LoadProgram(context.Background(), allocationLoader{entry.String(): source}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "allocation", Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		tb.Fatal(err)
	}
	owner, err := program.NewRuntime(options)
	if err != nil {
		tb.Fatal(err)
	}
	return program, owner
}

func TestRuntimeAllocationCapture(t *testing.T) {
	output := os.Getenv("EMBER_ALLOCATION_OUTPUT")
	if output == "" {
		t.Skip("set EMBER_ALLOCATION_OUTPUT to capture allocation evidence")
	}
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Fatalf("allocation capture requires Go-runtime GOMAXPROCS=1, got %d", got)
	}
	if _, err := os.Stat(output); err == nil {
		t.Fatal("allocation output already exists")
	}
	role := os.Getenv("RUNTIME_ALLOCATION_CAPTURE_ROLE")
	pair := os.Getenv("RUNTIME_ALLOCATION_CAPTURE_PAIR")
	captureID := os.Getenv("RUNTIME_ALLOCATION_CAPTURE_ID")
	sourceCommit := os.Getenv("RUNTIME_ALLOCATION_SOURCE_COMMIT")
	environmentHash := os.Getenv("RUNTIME_ALLOCATION_ENVIRONMENT_SHA256")
	command := os.Getenv("RUNTIME_ALLOCATION_COMMAND")
	if (role != "frozen-current" && role != "candidate") || (pair != "a" && pair != "b") || captureID == "" || sourceCommit == "" || environmentHash == "" || command == "" {
		t.Fatal("allocation capture metadata is incomplete or invalid")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fmt.Fprintln(file, "schema_version\tcapture_role\tcapture_pair\tcapture_id\tsource_commit\tcorpus\tname\tlifecycle\tbytes_per_op\tallocs_per_op\timage_prepare_ns\tmachine_bind_ns\tarena_retained_bytes\troot_retained_bytes\tcommand\tenvironment_sha256")
	write := func(corpus, name, lifecycle string, metric allocationMeasure, imagePrepare, machineBind uint64) {
		fmt.Fprintf(file, "1\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n", role, pair, captureID, sourceCommit, corpus, name, lifecycle, metric.bytes, metric.allocs, imagePrepare, machineBind, metric.arenaRetained, metric.rootRetained, command, environmentHash)
	}

	for _, group := range []struct {
		corpus string
		cases  []top10LuauCase
	}{{"top10", top10LuauCases}, {"classic", classicLuauCases}, {"scenario", scenarioLuauCases}} {
		for _, tc := range group.cases {
			callable, err := prepareParityEmberRuntime(parityCaseSource(tc.source, 1))
			if err != nil {
				t.Fatalf("%s/%s prepare warmed callable: %v", group.corpus, tc.name, err)
			}
			preflight, err := callable.callback.Call(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			validateTop10EmberResult(t, preflight, tc.want)
			metric := measureAllocation(t, func() error {
				results, err := callable.callback.Call(context.Background())
				benchmarkEmberResultsSink = results
				return err
			})
			metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(callable.owner)
			write(group.corpus, tc.name, "warm_call", metric, 0, 0)
			if err := callable.close(); err != nil {
				t.Fatalf("%s/%s close warmed callable: %v", group.corpus, tc.name, err)
			}
		}
	}

	const scalarSource = "return 1 + 2"
	metric := measureAllocationOnce(t, func() error { _, err := ember.Compile(scalarSource); return err })
	write("public", "Compile", "Compile", metric, 0, 0)
	metric = measureAllocationOnce(t, func() error { _, err := ember.CompileWithOptions(scalarSource, ember.CompileOptions{}); return err })
	write("public", "CompileWithOptions", "CompileWithOptions", metric, 0, 0)

	entry := ember.LogicalModule("allocation/load")
	loader := allocationLoader{entry.String(): "return { update = function() return 3 end }"}
	metric = measureAllocationOnce(t, func() error {
		_, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Name: "allocation", Module: entry}}, Parallelism: 1})
		return err
	})
	write("public", "LoadProgram_ModuleLoader", "LoadProgram_ModuleLoader", metric, 0, 0)

	proto, err := ember.Compile(scalarSource)
	if err != nil {
		t.Fatal(err)
	}
	metric = measureAllocationOnce(t, func() error { results, err := ember.Run(proto); benchmarkEmberResultsSink = results; return err })
	write("public", "Run", "Run", metric, 0, 0)
	globals := map[string]ember.Value{"input": ember.NumberValue(2)}
	globalProto, err := ember.Compile("output = input + 1\nreturn output")
	if err != nil {
		t.Fatal(err)
	}
	metric = measureAllocationOnce(t, func() error {
		results, err := ember.RunWithGlobals(globalProto, globals)
		benchmarkEmberResultsSink = results
		return err
	})
	write("public", "RunWithGlobals", "RunWithGlobals", metric, 0, 0)
	metric = measureAllocationOnce(t, func() error {
		var err error
		runtimeAllocationProtoSink, err = ember.RuntimeAllocationPrepareImageForTest(proto)
		return err
	})
	write("public", "image_prepare", "image_prepare", metric, metric.ns, 0)
	machine := ember.NewRuntimeAllocationMachineForTest()
	metric = measureAllocationOnce(t, machine.Bind)
	write("public", "machine_bind_ephemeral", "machine_bind_ephemeral", metric, 0, metric.ns)
	metric = measureAllocationOnce(t, func() error { machine.Detach(); return nil })
	write("public", "machine_detach", "machine_detach", metric, 0, 0)
	metric = measureAllocationOnce(t, machine.Close)
	write("public", "machine_close", "machine_close", metric, 0, 0)

	program, owner := newAllocationRuntime(t, "return { update = function() return 1 end }", ember.RuntimeOptions{})
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	var created *ember.Runtime
	metric = measureAllocationOnce(t, func() error {
		var err error
		created, err = program.NewRuntime(ember.RuntimeOptions{})
		return err
	})
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(created)
	write("public", "NewRuntime", "NewRuntime", metric, 0, 0)
	if err := created.Close(); err != nil {
		t.Fatal(err)
	}

	persistentSource := `
local n = 0
return {
    update = function(x)
        n = n + x
        return n
    end,
}
`
	_, persistent := newAllocationRuntime(t, persistentSource, ember.RuntimeOptions{})
	defer persistent.Close()
	metric = measureAllocationOnce(t, func() error {
		_, err := persistent.RunHook(context.Background(), "update", ember.NumberValue(1))
		return err
	})
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(persistent)
	write("public", "RunHook_persistent", "RunHook_persistent", metric, 0, 0)
	_, bounded := newAllocationRuntime(t, persistentSource, ember.RuntimeOptions{MaxInstructions: 1 << 60})
	defer bounded.Close()
	metric = measureAllocationOnce(t, func() error {
		_, err := bounded.RunHook(context.Background(), "update", ember.NumberValue(1))
		return err
	})
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(bounded)
	write("public", "RunHook_bounded", "RunHook_bounded", metric, 0, 0)
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, cancelable := newAllocationRuntime(t, persistentSource, ember.RuntimeOptions{})
	defer cancelable.Close()
	metric = measureAllocationOnce(t, func() error { _, err := cancelable.RunHook(cancelCtx, "update", ember.NumberValue(1)); return err })
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(cancelable)
	write("public", "RunHook_cancelable", "RunHook_cancelable", metric, 0, 0)

	root := ember.LogicalModule("allocation/root")
	dep := ember.LogicalModule("allocation/dep")
	moduleProgram, _ := loadAllocationProgram(t, allocationLoader{
		root.String(): "return { update = function() return require(\"./dep\") end }",
		dep.String():  "return 7",
	}, root, nil)
	moduleRuntime, err := moduleProgram.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer moduleRuntime.Close()
	if _, err := moduleRuntime.RunHook(context.Background(), "update"); err != nil {
		t.Fatal(err)
	}
	metric = measureAllocationOnce(t, func() error { _, err := moduleRuntime.RunHook(context.Background(), "update"); return err })
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(moduleRuntime)
	write("public", "module_require", "module_require", metric, 0, 0)

	host := ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
		return map[string]ember.Value{"host": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) { return args, nil })}, nil
	})
	_, hosted := newAllocationRuntime(t, "return { update = function() return host(1) end }", ember.RuntimeOptions{Host: host})
	defer hosted.Close()
	metric = measureAllocationOnce(t, func() error { _, err := hosted.RunHook(context.Background(), "update"); return err })
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(hosted)
	write("public", "RuntimeHost", "RuntimeHost", metric, 0, 0)

	var callback ember.Callback
	captureHost := ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
		return map[string]ember.Value{"capture": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
			var err error
			callback, err = ember.CaptureCallback(ctx, args[0])
			return nil, err
		})}, nil
	})
	_, callbackRuntime := newAllocationRuntime(t, "return { startup = function() capture(function(x) return x + 1 end) end }", ember.RuntimeOptions{Host: captureHost})
	defer callbackRuntime.Close()
	if _, err := callbackRuntime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatal(err)
	}
	defer callback.Close()
	metric = measureAllocationOnce(t, func() error {
		results, err := callback.Call(context.Background(), ember.NumberValue(1))
		benchmarkEmberResultsSink = results
		return err
	})
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(callbackRuntime)
	write("public", "Callback", "Callback", metric, 0, 0)

	coroutineSource := `
local co = nil
return {
startup = function()
    co = coroutine.create(function()
        while true do
            coroutine.yield(1)
        end
    end)
    coroutine.resume(co)
end,
update = function()
    return coroutine.resume(co)
end,
}`
	_, coroutineRuntime := newAllocationRuntime(t, coroutineSource, ember.RuntimeOptions{})
	defer coroutineRuntime.Close()
	if _, err := coroutineRuntime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatal(err)
	}
	metric = measureAllocationOnce(t, func() error { _, err := coroutineRuntime.RunHook(context.Background(), "update"); return err })
	metric.arenaRetained, metric.rootRetained = ember.RuntimeAllocationRetainedBytesForTest(coroutineRuntime)
	write("public", "coroutine_resume", "coroutine_resume", metric, 0, 0)
	detachProto, err := ember.Compile("return 1, 2, 3, 4")
	if err != nil {
		t.Fatal(err)
	}
	detachResults, err := ember.Run(detachProto)
	if err != nil {
		t.Fatal(err)
	}
	metric = measureAllocationOnce(t, func() error {
		benchmarkEmberResultsSink = ember.RuntimeAllocationDetachValuesForTest(detachResults)
		return nil
	})
	write("public", "result_detach", "result_detach", metric, 0, 0)
	closeOwner, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	metric = measureAllocationOnce(t, closeOwner.Close)
	write("public", "close", "close", metric, 0, 0)
}
