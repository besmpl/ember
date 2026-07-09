package ember_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

var compilerBenchmarkProtoSink *ember.Proto
var compilerBenchmarkProgramSink *ember.Program

func BenchmarkCompileMatrix(b *testing.B) {
	cases := []struct {
		name   string
		source string
	}{
		{name: "tiny_arithmetic", source: "local x = 1\nlocal y = 2\nreturn (x + y) * 3 - 4 / 2"},
		{name: "straight_line/100", source: straightLineCompileBenchmarkSource(100)},
		{name: "straight_line/1000", source: straightLineCompileBenchmarkSource(1000)},
		{name: "straight_line/10000", source: straightLineCompileBenchmarkSource(10000)},
		{name: "branch_dense_cfg", source: branchDenseCompileBenchmarkSource()},
		{name: "constants/unique", source: constantsCompileBenchmarkSource(false)},
		{name: "constants/repeated", source: constantsCompileBenchmarkSource(true)},
		{name: "closures_upvalues", source: "local base = 4\nlocal function add(x)\n return base + x\nend\nreturn add(3)"},
		{name: "varargs_multi_return", source: "local function collect(...)\n local a, b = ...\n return a, b, select(\"#\", ...)\nend\nreturn collect(1, 2, 3)"},
		{name: "table_string_fields", source: "local value = {name = \"ember\", hp = 10}\nvalue.hp = value.hp + 5\nreturn value.name, value.hp"},
	}
	for _, tc := range top10LuauCases {
		cases = append(cases, struct {
			name   string
			source string
		}{name: "top10/" + tc.name, source: tc.source})
	}
	for _, tc := range scenarioLuauCases {
		cases = append(cases, struct {
			name   string
			source string
		}{name: "scenario/" + tc.name, source: tc.source})
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkCompileSource(b, tc.source)
		})
	}
}

func benchmarkCompileSource(b *testing.B, source string) {
	proto, err := ember.Compile(source)
	if err != nil {
		b.Fatalf("validation Compile returned error: %v", err)
	}
	metrics := ember.CompilerBenchmarkMetricsForTest(proto)
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled, err := ember.Compile(source)
		if err != nil {
			b.Fatalf("Compile returned error: %v", err)
		}
		compilerBenchmarkProtoSink = compiled
	}
	b.StopTimer()
	reportCompilerBenchmarkMetrics(b, metrics)
}

func straightLineCompileBenchmarkSource(lines int) string {
	var source strings.Builder
	source.WriteString("local value = 0\n")
	for i := 1; i <= lines; i++ {
		source.WriteString("value = value + ")
		source.WriteString(strconv.Itoa(i % 7))
		source.WriteByte('\n')
	}
	source.WriteString("return value")
	return source.String()
}

func branchDenseCompileBenchmarkSource() string {
	var source strings.Builder
	source.WriteString("local value = 0\n")
	for i := 0; i < 256; i++ {
		source.WriteString("if flag then\nvalue = value + 1\nelse\nvalue = value + 2\nend\n")
	}
	source.WriteString("return value")
	return source.String()
}

func constantsCompileBenchmarkSource(repeated bool) string {
	var source strings.Builder
	source.WriteString("local total = 0\n")
	for i := 1; i <= 512; i++ {
		source.WriteString("total = total + ")
		if repeated {
			source.WriteByte('7')
		} else {
			source.WriteString(strconv.Itoa(i))
		}
		source.WriteByte('\n')
	}
	source.WriteString("return total")
	return source.String()
}

func BenchmarkLoadProgramCompile(b *testing.B) {
	for _, mode := range []string{"cold", "warm"} {
		b.Run(mode, func(b *testing.B) {
			for _, check := range []bool{false, true} {
				b.Run("check="+strconv.FormatBool(check), func(b *testing.B) {
					for _, parallelism := range []int{1, 2, 4} {
						b.Run("parallelism="+strconv.Itoa(parallelism), func(b *testing.B) {
							benchmarkLoadProgramCompile(b, mode, check, parallelism)
						})
					}
				})
			}
		})
	}
}

type compileBenchmarkLoader struct {
	sources map[string]string
}

func (loader *compileBenchmarkLoader) LoadModule(ctx context.Context, id ember.ModuleID) (ember.Source, error) {
	if err := ctx.Err(); err != nil {
		return ember.Source{}, err
	}
	name := id.String()
	text, ok := loader.sources[name]
	if !ok {
		return ember.Source{}, fmt.Errorf("missing source %s", name)
	}
	return ember.Source{Name: name, Text: text}, nil
}

func benchmarkLoadProgramCompile(b *testing.B, mode string, check bool, parallelism int) {
	options := ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Check:       check,
		Parallelism: parallelism,
	}
	warmLoader := &compileBenchmarkLoader{sources: compileBenchmarkProgramSources()}
	validationLoader := ember.ModuleLoader(warmLoader)
	if mode == "cold" {
		validationLoader = &compileBenchmarkLoader{sources: compileBenchmarkProgramSources()}
	}
	program, report, err := ember.LoadProgram(context.Background(), validationLoader, options)
	if err != nil {
		b.Fatalf("validation LoadProgram returned error: %v", err)
	}
	validateCompileBenchmarkProgram(b, program, report)
	metrics := ember.CompilerProgramBenchmarkMetricsForTest(program)
	b.ReportAllocs()
	b.SetBytes(int64(compileBenchmarkProgramSourceBytes()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loader := ember.ModuleLoader(warmLoader)
		if mode == "cold" {
			loader = &compileBenchmarkLoader{sources: compileBenchmarkProgramSources()}
		}
		loaded, _, err := ember.LoadProgram(context.Background(), loader, options)
		if err != nil {
			b.Fatalf("LoadProgram returned error: %v", err)
		}
		if loaded == nil {
			b.Fatal("LoadProgram returned nil program")
		}
		compilerBenchmarkProgramSink = loaded
	}
	b.StopTimer()
	reportCompilerBenchmarkMetrics(b, metrics)
}

func compileBenchmarkProgramSources() map[string]string {
	return map[string]string{
		"logical:game/server/init":   `local config = require("../shared/config") return {config = config, side = "server"}`,
		"logical:game/client/init":   `local config = require("../shared/config") return {config = config, side = "client"}`,
		"logical:game/shared/config": `return {value = 1}`,
	}
}

func compileBenchmarkProgramSourceBytes() int {
	total := 0
	for _, source := range compileBenchmarkProgramSources() {
		total += len(source)
	}
	return total
}

func validateCompileBenchmarkProgram(b *testing.B, program *ember.Program, report ember.LoadReport) {
	b.Helper()
	if program == nil {
		b.Fatal("LoadProgram returned nil program")
	}
	wantEntrypoints := []string{"server:logical:game/server/init", "client:logical:game/client/init"}
	if len(report.Entrypoints) != len(wantEntrypoints) {
		b.Fatalf("entrypoint report count is %d, want %d", len(report.Entrypoints), len(wantEntrypoints))
	}
	for index, entrypoint := range report.Entrypoints {
		got := entrypoint.Name + ":" + entrypoint.Module.String()
		if got != wantEntrypoints[index] {
			b.Fatalf("entrypoint report %d is %q, want %q", index, got, wantEntrypoints[index])
		}
	}
	wantModules := []string{
		"logical:game/client/init",
		"logical:game/server/init",
		"logical:game/shared/config",
	}
	if len(report.Modules) != len(wantModules) {
		b.Fatalf("module report count is %d, want %d", len(report.Modules), len(wantModules))
	}
	for index, module := range report.Modules {
		if got := module.Module.String(); got != wantModules[index] {
			b.Fatalf("module report %d is %q, want %q", index, got, wantModules[index])
		}
	}
	if len(report.Diagnostics) != 0 {
		b.Fatalf("LoadProgram returned diagnostics %#v, want none", report.Diagnostics)
	}
}

func reportCompilerBenchmarkMetrics(b *testing.B, metrics ember.CompilerBenchmarkMetrics) {
	b.Helper()
	b.ReportMetric(float64(metrics.Instructions), "instructions/op")
	b.ReportMetric(float64(metrics.Constants), "constants/op")
	b.ReportMetric(float64(metrics.RegisterSlots), "register_slots/op")
	b.ReportMetric(float64(metrics.ChildProtos), "child_protos/op")
	b.ReportMetric(float64(metrics.PackedBytes), "packed_B/op")
	b.ReportMetric(float64(metrics.ProtoOwnedBytes), "proto_owned_B/op")
}
