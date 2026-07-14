package ember

import (
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// compilerE8LayoutCaps are representation gates for the arena-backed compiler.
// They intentionally leave a small amount of room for architecture-neutral
// padding while preventing a return to pointer-heavy node records.
func TestCompilerE8LayoutBudgets(t *testing.T) {
	cases := []struct {
		name   string
		typeOf reflect.Type
		max    uintptr
	}{
		{name: "syntaxID", typeOf: reflect.TypeOf(syntaxID(0)), max: 8},
		{name: "expressionID", typeOf: reflect.TypeOf(expressionID(0)), max: 4},
		{name: "statementID", typeOf: reflect.TypeOf(statementID(0)), max: 4},
		{name: "typeID", typeOf: reflect.TypeOf(typeID(0)), max: 4},
		{name: "nodeSpan", typeOf: reflect.TypeOf(nodeSpan{}), max: 8},
		{name: "sourceRange", typeOf: reflect.TypeOf(sourceRange{}), max: 16},
		{name: "arenaExpression", typeOf: reflect.TypeOf(arenaExpression{}), max: 16},
		{name: "arenaAndExpression", typeOf: reflect.TypeOf(arenaAndExpression{}), max: 16},
		{name: "arenaComparisonExpression", typeOf: reflect.TypeOf(arenaComparisonExpression{}), max: 16},
		{name: "arenaConcatExpression", typeOf: reflect.TypeOf(arenaConcatExpression{}), max: 16},
		{name: "arenaAdditiveExpression", typeOf: reflect.TypeOf(arenaAdditiveExpression{}), max: 16},
		{name: "arenaMultiplicativeExpression", typeOf: reflect.TypeOf(arenaMultiplicativeExpression{}), max: 16},
		{name: "arenaTerm", typeOf: reflect.TypeOf(arenaTerm{}), max: 64},
		{name: "arenaCall", typeOf: reflect.TypeOf(arenaCall{}), max: 32},
		{name: "arenaFunction", typeOf: reflect.TypeOf(arenaFunction{}), max: 96},
		{name: "arenaIfExpression", typeOf: reflect.TypeOf(arenaIfExpression{}), max: 16},
		{name: "arenaStatement", typeOf: reflect.TypeOf(arenaStatement{}), max: 16},
		{name: "arenaLocalStatement", typeOf: reflect.TypeOf(arenaLocalStatement{}), max: 48},
		{name: "arenaFunctionStatement", typeOf: reflect.TypeOf(arenaFunctionStatement{}), max: 128},
		{name: "arenaType", typeOf: reflect.TypeOf(arenaType{}), max: 64},
		{name: "arenaNamedType", typeOf: reflect.TypeOf(arenaNamedType{}), max: 16},
		{name: "arenaTableType", typeOf: reflect.TypeOf(arenaTableType{}), max: 16},
		{name: "arenaFunctionType", typeOf: reflect.TypeOf(arenaFunctionType{}), max: 64},
		{name: "arenaTypeField", typeOf: reflect.TypeOf(arenaTypeField{}), max: 16},
		{name: "arenaTypeParam", typeOf: reflect.TypeOf(arenaTypeParam{}), max: 16},
		{name: "bytecodeIRInstruction", typeOf: reflect.TypeOf(bytecodeIRInstruction{}), max: 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.typeOf.Size()
			t.Logf("%s=%d bytes (cap %d)", tc.name, got, tc.max)
			if got > tc.max {
				t.Fatalf("%s=%d bytes, want at most %d", tc.name, got, tc.max)
			}
		})
	}
}

type compilerE8AllocationSample struct {
	bytes  uint64
	allocs uint64
}

var (
	compilerE8ProtoSink *Proto
	compilerE8TreeSink  syntaxTree
	compilerE8ErrorSink error
	compilerE8IRSink    []bytecodeIRInstruction
)

func TestCompilerE8OptimizerAllocationBudget(t *testing.T) {
	source := compilerStageSource(256 << 10)
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	emission, err := emitCompilerStage(artifact)
	if err != nil {
		t.Fatalf("emit returned error: %v", err)
	}
	optimize := func() {
		optimized := optimizeCompilerStageIR(emission)
		compilerE8IRSink = optimized.ir
	}
	optimize()
	if checkptrInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	runtime.GC()
	sample := measureCompilerE8Runs(optimize)
	t.Logf("source=%d bytes, optimizer alloc=%d B/op, %d allocs/op", len(source), sample.bytes, sample.allocs)
	if sample.bytes >= 10<<20 {
		t.Fatalf("optimizer allocation bytes=%d, want below %d", sample.bytes, 10<<20)
	}
}

func TestCompilerE8AllocationBudgets(t *testing.T) {
	tests := []struct {
		name           string
		source         string
		parse          bool
		options        CompileOptions
		wantError      bool
		maxBytes       uint64
		maxAllocations uint64
	}{
		{
			name:           "straight_line_256KiB_parse",
			source:         compilerStageSource(256 << 10),
			parse:          true,
			maxBytes:       30 << 20,
			maxAllocations: 30000,
		},
		{
			name:           "straight_line_256KiB_compile",
			source:         compilerStageSource(256 << 10),
			maxBytes:       30 << 20,
			maxAllocations: 30000,
		},
		{
			name:           "branch_heavy",
			source:         compilerE8BranchHeavySource(512),
			maxBytes:       30 << 20,
			maxAllocations: 30000,
		},
		{
			name:           "type_heavy",
			source:         compilerE8TypeHeavySource(512),
			maxBytes:       30 << 20,
			maxAllocations: 30000,
		},
		{
			name:           "malformed_deep",
			source:         compilerE8MalformedDeepSource(10000),
			options:        CompileOptions{Limits: CompileLimits{MaxNesting: 64}},
			wantError:      true,
			maxBytes:       10 << 20,
			maxAllocations: 30000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Always exercise the fixture's observable compiler behavior before
			// deciding whether its allocation measurement is meaningful. The
			// checkptr lane deliberately skips only the measurement and budget
			// assertions below; parse, compile, and expected-error checks remain
			// active in every instrumentation mode.
			if tc.parse {
				probeCompilerE8Parse(t, tc.source, tc.options, tc.wantError)
			} else {
				probeCompilerE8Compile(t, tc.source, tc.options, tc.wantError)
			}
			if checkptrInstrumentedTest() {
				t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
			}

			var sample compilerE8AllocationSample
			if tc.parse {
				sample = measureCompilerE8Parse(t, tc.source, tc.options, tc.wantError)
			} else {
				sample = measureCompilerE8Compile(t, tc.source, tc.options, tc.wantError)
			}
			t.Logf("source=%d bytes, alloc=%d B/op, %d allocs/op", len(tc.source), sample.bytes, sample.allocs)
			if sample.bytes >= tc.maxBytes {
				t.Fatalf("allocation bytes=%d, want below %d", sample.bytes, tc.maxBytes)
			}
			if sample.allocs >= tc.maxAllocations {
				t.Fatalf("allocations=%d, want below %d", sample.allocs, tc.maxAllocations)
			}
		})
	}
}

func measureCompilerE8Parse(t *testing.T, source string, options CompileOptions, wantError bool) compilerE8AllocationSample {
	t.Helper()
	parse := func() {
		probeCompilerE8Parse(t, source, options, wantError)
	}
	runtime.GC()
	return measureCompilerE8Runs(parse)
}

func measureCompilerE8Compile(t *testing.T, source string, options CompileOptions, wantError bool) compilerE8AllocationSample {
	t.Helper()
	compile := func() {
		probeCompilerE8Compile(t, source, options, wantError)
	}
	runtime.GC()
	return measureCompilerE8Runs(compile)
}

func probeCompilerE8Parse(t *testing.T, source string, options CompileOptions, wantError bool) {
	t.Helper()
	parsed, err := (&parser{source: source, limits: options.Limits}).parse()
	if wantError {
		if err == nil || !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("parse error = %v, want a limit error", err)
		}
		compilerE8ErrorSink = err
		return
	}
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	compilerE8TreeSink = parsed
}

func probeCompilerE8Compile(t *testing.T, source string, options CompileOptions, wantError bool) {
	t.Helper()
	proto, err := CompileWithOptions(source, options)
	if wantError {
		if err == nil || !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("compile error = %v, want a limit error", err)
		}
		compilerE8ErrorSink = err
		return
	}
	if err != nil {
		t.Fatalf("CompileWithOptions returned error: %v", err)
	}
	compilerE8ProtoSink = proto
}

func measureCompilerE8Runs(run func()) compilerE8AllocationSample {
	const runs = 3
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		run()
	}
	runtime.ReadMemStats(&after)
	return compilerE8AllocationSample{
		bytes:  (after.TotalAlloc - before.TotalAlloc) / runs,
		allocs: (after.Mallocs - before.Mallocs) / runs,
	}
}

func compilerE8BranchHeavySource(branches int) string {
	var source strings.Builder
	source.WriteString("local value = 0\n")
	for index := 0; index < branches; index++ {
		source.WriteString("if flag then\nvalue = value + 1\nelse\nvalue = value + 2\nend\n")
	}
	source.WriteString("return value\n")
	return source.String()
}

func compilerE8TypeHeavySource(aliases int) string {
	var source strings.Builder
	source.WriteString("--!strict\n")
	for index := 0; index < aliases; index++ {
		source.WriteString("type T")
		source.WriteString(strconv.Itoa(index))
		source.WriteString(" = {left: number, right: string?} | ((number) -> string)?\n")
	}
	source.WriteString("return 1\n")
	return source.String()
}

func compilerE8MalformedDeepSource(depth int) string {
	if depth < 1 {
		depth = 1
	}
	return "return " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth-1)
}
