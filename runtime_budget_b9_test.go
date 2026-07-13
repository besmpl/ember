package ember

import (
	"context"
	"errors"
	"testing"
)

func TestB9TableEntryBoundaryAndDelete(t *testing.T) {
	table := newTableWithCapacity(0, 0)
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxTableEntriesPerTable: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := table.rawSetWithController(controller, StringValue("a"), NumberValue(1)); err != nil {
		t.Fatal(err)
	}
	if err := table.rawSetWithController(controller, NumberValue(1), NumberValue(2)); err != nil {
		t.Fatal(err)
	}
	if err := table.rawSetWithController(controller, BoolValue(true), NumberValue(3)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("third insert = %v", err)
	}
	if err := table.rawSetWithController(controller, StringValue("a"), NumberValue(4)); err != nil {
		t.Fatal(err)
	}
	if err := table.rawSetWithController(controller, StringValue("a"), NilValue()); err != nil {
		t.Fatal(err)
	}
	if err := table.rawSetWithController(controller, BoolValue(true), NumberValue(3)); err != nil {
		t.Fatal(err)
	}
}

func TestB9GeneratedStringLimit(t *testing.T) {
	proto, err := Compile(`local suffix = "runtime" return tostring(123456) .. suffix`)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxGeneratedStringBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	_, err = thread.run(proto, nil, nil)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitGeneratedStringBytes {
		t.Fatalf("concat = %v", err)
	}
}

func TestB9RuntimeObjectTableLimit(t *testing.T) {
	proto, err := Compile(`local first = {} local second = {} return first, second`)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxRuntimeObjects: 1})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	_, err = thread.run(proto, nil, nil)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitRuntimeObjects {
		t.Fatalf("objects = %v", err)
	}
}

func TestB9RuntimeObjectClosureAndCoroutineLimit(t *testing.T) {
	proto, err := Compile(`return coroutine.create(function() return 1 end)`)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxRuntimeObjects: 2})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	_, err = thread.run(proto, nil, nil)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitRuntimeObjects {
		t.Fatalf("closure/coroutine = %v", err)
	}
}

func TestB9RuntimeObjectCoroutineExactBoundary(t *testing.T) {
	if _, err := runB9WithLimits(t, `return coroutine.create(function() return 1 end)`, ExecutionLimits{MaxRuntimeObjects: 3}, nil); err != nil {
		t.Fatalf("closure/coroutine/userdata exact boundary = %v", err)
	}
}

func runB9WithLimits(t *testing.T, source string, limits ExecutionLimits, globals map[string]Value) ([]Value, error) {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newExecutionController(context.Background(), limits)
	if err != nil {
		t.Fatal(err)
	}
	env := runtimeGlobals(globals)
	thread := newVMThread(env)
	thread.controller = controller
	return thread.run(proto, nil, nil)
}

func assertB9LimitKind(t *testing.T, err error, kind LimitKind) {
	t.Helper()
	var limit *LimitError
	if !errors.Is(err, ErrLimitExceeded) || !errors.As(err, &limit) || limit.Kind != kind {
		t.Fatalf("error = %#v, want %s limit", err, kind)
	}
}

func TestB9GeneratedStringBoundariesAndCache(t *testing.T) {
	if _, err := runB9WithLimits(t, "local out = \"\"\nfor i = 1, 3 do\n out = out .. tostring(i)\nend\nreturn out", ExecutionLimits{MaxGeneratedStringBytes: 2}, nil); err == nil {
		t.Fatal("unique concat loop under byte limit succeeded")
	} else {
		assertB9LimitKind(t, err, LimitGeneratedStringBytes)
	}
	if _, err := runB9WithLimits(t, `return tostring(123456)`, ExecutionLimits{MaxGeneratedStringBytes: 5}, nil); err == nil {
		t.Fatal("numeric tostring under byte limit succeeded")
	} else {
		assertB9LimitKind(t, err, LimitGeneratedStringBytes)
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Limit != 5 || limit.Used != 6 {
			t.Fatalf("numeric tostring limit = %#v", limit)
		}
	}
	if _, err := runB9WithLimits(t, `return tostring(123456)`, ExecutionLimits{MaxGeneratedStringBytes: 6}, nil); err != nil {
		t.Fatalf("exact numeric tostring limit = %v", err)
	}
	if _, err := runB9WithLimits(t, `return tostring(123456), tostring(123456)`, ExecutionLimits{MaxGeneratedStringBytes: 6}, nil); err != nil {
		t.Fatalf("interned repeat = %v", err)
	}
	if _, err := runB9WithLimits(t, `return table.concat({"a", "bc"})`, ExecutionLimits{MaxGeneratedStringBytes: 2}, nil); err == nil {
		t.Fatal("table.concat under byte limit succeeded")
	} else {
		assertB9LimitKind(t, err, LimitGeneratedStringBytes)
	}
	if _, err := runB9WithLimits(t, `return table.concat({"a", "bc"})`, ExecutionLimits{MaxGeneratedStringBytes: 3}, nil); err != nil {
		t.Fatalf("exact table.concat limit = %v", err)
	}
	if _, err := runB9WithLimits(t, `return "source-constant-value"`, ExecutionLimits{MaxGeneratedStringBytes: 1}, nil); err != nil {
		t.Fatalf("source constant charged = %v", err)
	}
	if _, err := runB9WithLimits(t, `return tostring(s)`, ExecutionLimits{MaxGeneratedStringBytes: 1}, map[string]Value{"s": StringValue("host-value")}); err != nil {
		t.Fatalf("host string tostring charged = %v", err)
	}
}

func TestB9TableMutationPathsAndSharedHostTable(t *testing.T) {
	if _, err := runB9WithLimits(t, "local t = {}\nt.a = 1\nt[true] = 2\nreturn t", ExecutionLimits{MaxTableEntriesPerTable: 1}, nil); err == nil {
		t.Fatal("second direct property insert succeeded")
	} else {
		assertB9LimitKind(t, err, LimitTableEntriesPerTable)
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Limit != 1 || limit.Used != 2 {
			t.Fatalf("table mutation limit = %#v", limit)
		}
	}
	if _, err := runB9WithLimits(t, "local t = {}\nt.a = 1\nt.a = 2\nt.a = nil\nt[true] = 3\nreturn t", ExecutionLimits{MaxTableEntriesPerTable: 1}, nil); err != nil {
		t.Fatalf("update/delete reuse = %v", err)
	}
	table := NewTable()
	limited, err := newExecutionController(context.Background(), ExecutionLimits{MaxTableEntriesPerTable: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := table.rawSetWithController(limited, StringValue("a"), NumberValue(1)); err != nil {
		t.Fatal(err)
	}
	if err := table.Set(StringValue("b"), NumberValue(2)); err != nil {
		t.Fatalf("host Set inherited invocation limit = %v", err)
	}
	if err := table.rawSetWithController(limited, StringValue("c"), NumberValue(3)); err == nil {
		t.Fatal("limited shared table accepted third entry")
	} else {
		assertB9LimitKind(t, err, LimitTableEntriesPerTable)
	}
}

func TestB9SharedHostTableAcrossRuntimeLimits(t *testing.T) {
	shared := NewTable()
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: "return { startup = function() shared.a = 1 end }"}})
	host := RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{"shared": TableValue(shared)}, nil
	})
	limited, err := program.NewRuntime(RuntimeOptions{Host: host, Limits: ExecutionLimits{MaxTableEntriesPerTable: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limited.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("first limited runtime = %v", err)
	}
	limited.Close()
	if err := shared.Set(StringValue("host"), NumberValue(2)); err != nil {
		t.Fatalf("host mutation after limited runtime = %v", err)
	}
	unlimited, err := program.NewRuntime(RuntimeOptions{Host: host})
	if err != nil {
		t.Fatal(err)
	}
	defer unlimited.Close()
	if _, err := unlimited.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("unlimited runtime shared table = %v", err)
	}
}

func TestB9TableLibraryMutationAccounting(t *testing.T) {
	if _, err := runB9WithLimits(t, "local t = {}\ntable.insert(t, 1)\ntable.insert(t, 2)\ntable.remove(t, 1)\ntable.insert(t, 3)\ntable.clear(t)\ntable.insert(t, 4)\nreturn t", ExecutionLimits{MaxTableEntriesPerTable: 2}, nil); err != nil {
		t.Fatalf("insert/remove/clear reuse = %v", err)
	}
	if _, err := runB9WithLimits(t, "return table.pack(1, 2)", ExecutionLimits{MaxTableEntriesPerTable: 2}, nil); err == nil {
		t.Fatal("table.pack exceeded entry limit without error")
	} else {
		assertB9LimitKind(t, err, LimitTableEntriesPerTable)
	}
}

func TestB9RuntimeObjectBoundariesAndSharedCells(t *testing.T) {
	if _, err := runB9WithLimits(t, "local function f() return 1 end\nreturn f, f", ExecutionLimits{MaxRuntimeObjects: 1}, nil); err != nil {
		t.Fatalf("canonical closure reuse = %v", err)
	}
	if _, err := runB9WithLimits(t, "local x = 1\nlocal function a() x = x + 1 return x end\nlocal function b() return x end\nreturn a, b", ExecutionLimits{MaxRuntimeObjects: 2}, nil); err == nil {
		t.Fatal("captured closures under object limit succeeded")
	} else {
		assertB9LimitKind(t, err, LimitRuntimeObjects)
	}
	if _, err := runB9WithLimits(t, "local x = 1\nlocal function a() x = x + 1 return x end\nlocal function b() return x end\nreturn a, b", ExecutionLimits{MaxRuntimeObjects: 3}, nil); err != nil {
		t.Fatalf("shared captured cell exact boundary = %v", err)
	}
}
