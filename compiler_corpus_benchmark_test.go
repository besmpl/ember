package ember

import (
	"context"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// compilerCorpusFS contains repository-owned sources that are intentionally
// measured outside the fixed cold-core benchmark aggregate.
//
//go:embed testdata/compiler/*.luau
var compilerCorpusFS embed.FS

type compilerCorpusFixture struct {
	name          string
	source        string
	want          []Value
	malformed     bool
	wantErrorByte int
}

type compilerCorpusGraph struct {
	size        int
	loader      compilerCorpusLoader
	root        ModuleID
	sourceBytes int
}

type compilerCorpusLoader struct {
	sources map[string]string
}

func (loader compilerCorpusLoader) LoadModule(ctx context.Context, id ModuleID) (Source, error) {
	if err := ctx.Err(); err != nil {
		return Source{}, err
	}
	name := id.String()
	text, ok := loader.sources[name]
	if !ok {
		return Source{}, fmt.Errorf("missing corpus source %s", name)
	}
	return Source{Name: name, Text: text}, nil
}

func TestCompilerCorpusFixtures(t *testing.T) {
	fixtures := loadCompilerCorpusFixtures(t)
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			validateCompilerCorpusFixture(t, fixture)
		})
	}
}

func BenchmarkCompilerCorpus(b *testing.B) {
	fixtures := loadCompilerCorpusFixtures(b)
	for _, fixture := range fixtures {
		fixture := fixture
		b.Run(fixture.name, func(b *testing.B) {
			proto := validateCompilerCorpusFixture(b, fixture)
			metrics := CompilerBenchmarkMetricsForTest(proto)

			b.ReportAllocs()
			b.SetBytes(int64(len(fixture.source)))
			b.ResetTimer()
			for range b.N {
				compiled, err := Compile(fixture.source)
				if fixture.malformed {
					if err == nil {
						b.Fatal("Compile succeeded, want malformed-source error")
					}
					compilerCorpusErrorSink = err.Error()
					continue
				}
				if err != nil {
					b.Fatal(err)
				}
				compilerCorpusProtoSink = compiled
			}
			b.StopTimer()
			reportCompilerCorpusMetrics(b, fixture, metrics)
		})
	}
}

func validateCompilerCorpusFixture(tb testing.TB, fixture compilerCorpusFixture) *Proto {
	tb.Helper()
	proto, err := Compile(fixture.source)
	if fixture.malformed {
		if err == nil {
			tb.Fatal("Compile succeeded, want deterministic malformed-source error")
		}
		want := fmt.Sprintf("compile: byte %d:", fixture.wantErrorByte)
		if !strings.Contains(err.Error(), want) {
			tb.Fatalf("Compile error is %q, want byte prefix %q", err, want)
		}
		return nil
	}
	if err != nil {
		tb.Fatalf("Compile returned error: %v", err)
	}
	if fixture.want == nil {
		return proto
	}
	got, err := Run(proto)
	if err != nil {
		tb.Fatalf("Run returned error: %v", err)
	}
	if !equalCompilerCorpusValues(got, fixture.want) {
		tb.Fatalf("Run returned %#v, want %#v", got, fixture.want)
	}
	return proto
}

var (
	compilerCorpusProtoSink   *Proto
	compilerCorpusErrorSink   string
	compilerCorpusProgramSink *Program
)

func reportCompilerCorpusMetrics(b *testing.B, fixture compilerCorpusFixture, metrics CompilerBenchmarkMetrics) {
	b.ReportMetric(float64(len(fixture.source)), "source_B/op")
	b.ReportMetric(float64(metrics.Instructions), "instructions/op")
	b.ReportMetric(float64(metrics.Constants), "constants/op")
	b.ReportMetric(float64(metrics.RegisterSlots), "register_slots/op")
	b.ReportMetric(float64(metrics.ChildProtos), "child_protos/op")
	b.ReportMetric(float64(metrics.PackedBytes), "packed_B/op")
	b.ReportMetric(float64(metrics.ProtoOwnedBytes), "proto_owned_B/op")
	b.ReportMetric(float64(metrics.RetainedStringBytes), "retained_string_B/op")
}

func loadCompilerCorpusFixtures(tb testing.TB) []compilerCorpusFixture {
	tb.Helper()
	names, err := compilerCorpusFS.ReadDir("testdata/compiler")
	if err != nil {
		tb.Fatalf("ReadDir compiler corpus: %v", err)
	}
	byName := map[string]struct {
		want          []Value
		malformed     bool
		wantErrorByte int
	}{
		"type_heavy.luau": {
			want: []Value{NumberValue(1)},
		},
		"comment_heavy.luau": {
			want: []Value{NumberValue(15)},
		},
		"strings.luau": {
			want: []Value{
				StringValue("plain"),
				StringValue("line\nfeed"),
				StringValue("tab\tvalue"),
				StringValue("quote\"value"),
				StringValue("slash\\value"),
			},
		},
		"deep_syntax.luau": {
			want: []Value{NumberValue(256)},
		},
		"nested_closures.luau": {
			want: []Value{NumberValue(14)},
		},
		"high_constants_registers.luau": {
			want: compilerCorpusNumbers(32),
		},
		"dense_control_flow.luau": {
			want: []Value{NumberValue(17)},
		},
		"malformed_error.luau": {
			malformed:     true,
			wantErrorByte: 14,
		},
	}

	fixtures := make([]compilerCorpusFixture, 0, len(names))
	for _, entry := range names {
		if entry.IsDir() || path.Ext(entry.Name()) != ".luau" {
			continue
		}
		spec, ok := byName[entry.Name()]
		if !ok {
			tb.Fatalf("compiler corpus fixture %q has no behavior specification", entry.Name())
		}
		source, err := compilerCorpusFS.ReadFile(path.Join("testdata/compiler", entry.Name()))
		if err != nil {
			tb.Fatalf("ReadFile compiler corpus %q: %v", entry.Name(), err)
		}
		fixtures = append(fixtures, compilerCorpusFixture{
			name:          strings.TrimSuffix(entry.Name(), path.Ext(entry.Name())),
			source:        string(source),
			want:          spec.want,
			malformed:     spec.malformed,
			wantErrorByte: spec.wantErrorByte,
		})
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].name < fixtures[j].name })
	return fixtures
}

func compilerCorpusNumbers(count int) []Value {
	values := make([]Value, count)
	for index := range values {
		values[index] = NumberValue(float64(index))
	}
	return values
}

func equalCompilerCorpusValues(got, want []Value) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if !valuesEqual(got[index], want[index]) {
			return false
		}
	}
	return true
}

func BenchmarkCompilerGraphMatrix(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		graph := prepareCompilerCorpusGraph(b, size)
		b.Run("modules_"+strconv.Itoa(size), func(b *testing.B) {
			b.Run("public_loader_reused_fresh_store", func(b *testing.B) {
				benchmarkCompilerCorpusGraphPublic(b, graph)
			})
			b.Run("private_store_unchanged_repeats", func(b *testing.B) {
				benchmarkCompilerCorpusGraphPrivateUnchanged(b, graph)
			})
			b.Run("private_store_one_edit", func(b *testing.B) {
				benchmarkCompilerCorpusGraphPrivateOneEdit(b, graph)
			})
			b.Run("private_store_edited_repeats", func(b *testing.B) {
				benchmarkCompilerCorpusGraphPrivateEditedRepeats(b, graph)
			})
			b.Run("concurrent_independent_compilation", func(b *testing.B) {
				benchmarkCompilerCorpusGraphConcurrent(b, graph)
			})
		})
	}
}

func prepareCompilerCorpusGraph(tb testing.TB, size int) compilerCorpusGraph {
	tb.Helper()
	sources := make(map[string]string, size)
	totalBytes := 0
	for index := size - 1; index >= 0; index-- {
		pathName := fmt.Sprintf("corpus/%d/module%04d", size, index)
		moduleName := LogicalModule(pathName).String()
		text := "return 1\n"
		if index+1 < size {
			text = fmt.Sprintf("local next = require(\"./module%04d\")\nreturn next\n", index+1)
		}
		sources[moduleName] = text
		totalBytes += len(text)
	}
	graph := compilerCorpusGraph{
		size:        size,
		loader:      compilerCorpusLoader{sources: sources},
		root:        LogicalModule(fmt.Sprintf("corpus/%d/module%04d", size, 0)),
		sourceBytes: totalBytes,
	}
	validateCompilerCorpusGraph(tb, graph)
	return graph
}

func validateCompilerCorpusGraph(tb testing.TB, graph compilerCorpusGraph) {
	tb.Helper()
	program, report, err := LoadProgram(context.Background(), graph.loader, ProgramOptions{
		Entrypoints: []Entrypoint{{Name: "root", Module: graph.root}},
		Parallelism: 1,
	})
	if err != nil {
		tb.Fatalf("LoadProgram graph %d returned error: %v", graph.size, err)
	}
	if program == nil {
		tb.Fatalf("LoadProgram graph %d returned nil program", graph.size)
	}
	if len(report.Modules) != graph.size {
		tb.Fatalf("LoadProgram graph %d reported %d modules, want %d", graph.size, len(report.Modules), graph.size)
	}
	if len(report.Diagnostics) != 0 {
		tb.Fatalf("LoadProgram graph %d reported diagnostics %#v", graph.size, report.Diagnostics)
	}
}

func benchmarkCompilerCorpusGraphPublic(b *testing.B, graph compilerCorpusGraph) {
	metrics := compilerCorpusGraphMetrics(b, graph)
	b.ReportAllocs()
	b.SetBytes(int64(graph.sourceBytes))
	b.ResetTimer()
	for range b.N {
		program, _, err := LoadProgram(context.Background(), graph.loader, ProgramOptions{
			Entrypoints: []Entrypoint{{Name: "root", Module: graph.root}},
			Parallelism: 1,
		})
		if err != nil {
			b.Fatal(err)
		}
		compilerCorpusProgramSink = program
	}
	b.StopTimer()
	metrics.report(b, graph)
}

func benchmarkCompilerCorpusGraphPrivateUnchanged(b *testing.B, graph compilerCorpusGraph) {
	store := newSourceArtifactStore()
	if _, _, err := loadCompilerCorpusGraphWithStore(graph, store); err != nil {
		b.Fatal(err)
	}
	metrics := compilerCorpusGraphMetricsFromStore(b, graph, store)
	b.ReportAllocs()
	b.SetBytes(int64(graph.sourceBytes))
	b.ResetTimer()
	for range b.N {
		program, _, err := loadCompilerCorpusGraphWithStore(graph, store)
		if err != nil {
			b.Fatal(err)
		}
		compilerCorpusProgramSink = program
	}
	b.StopTimer()
	metrics.report(b, graph)
}

const compilerCorpusOneModuleEdit = "-- one-module edit\n"

func compilerCorpusEditedGraph(tb testing.TB, graph compilerCorpusGraph) compilerCorpusGraph {
	tb.Helper()
	editedSources := make(map[string]string, len(graph.loader.sources))
	for name, source := range graph.loader.sources {
		editedSources[name] = source
	}
	rootName := graph.root.String()
	editedSources[rootName] += compilerCorpusOneModuleEdit
	edited := graph
	edited.loader = compilerCorpusLoader{sources: editedSources}
	edited.sourceBytes += len(compilerCorpusOneModuleEdit)
	validateCompilerCorpusGraph(tb, edited)
	return edited
}

func benchmarkCompilerCorpusGraphPrivateOneEdit(b *testing.B, graph compilerCorpusGraph) {
	edited := compilerCorpusEditedGraph(b, graph)

	// Prepare a representative store and metrics outside the timed region.
	metricsStore := newSourceArtifactStore()
	if _, _, err := loadCompilerCorpusGraphWithStore(graph, metricsStore); err != nil {
		b.Fatal(err)
	}
	if _, _, err := loadCompilerCorpusGraphWithStore(edited, metricsStore); err != nil {
		b.Fatal(err)
	}
	metrics := compilerCorpusGraphMetricsFromStore(b, edited, metricsStore)

	b.ReportAllocs()
	b.SetBytes(int64(edited.sourceBytes))
	b.ResetTimer()
	for range b.N {
		// Store setup is intentionally excluded. Each timed operation is the
		// first load after exactly one root-source identity change.
		b.StopTimer()
		store := newSourceArtifactStore()
		if _, _, err := loadCompilerCorpusGraphWithStore(graph, store); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		program, _, err := loadCompilerCorpusGraphWithStore(edited, store)
		if err != nil {
			b.Fatal(err)
		}
		compilerCorpusProgramSink = program
	}
	b.StopTimer()
	metrics.report(b, edited)
}

func benchmarkCompilerCorpusGraphPrivateEditedRepeats(b *testing.B, graph compilerCorpusGraph) {
	edited := compilerCorpusEditedGraph(b, graph)
	store := newSourceArtifactStore()
	if _, _, err := loadCompilerCorpusGraphWithStore(graph, store); err != nil {
		b.Fatal(err)
	}

	if _, _, err := loadCompilerCorpusGraphWithStore(edited, store); err != nil {
		b.Fatal(err)
	}
	// This measures repeats after one identity-changing edit. The current
	// private store has no dependency invalidation API; unchanged artifacts
	// remain reusable by source identity while the edited module hits on later
	// iterations.
	metrics := compilerCorpusGraphMetricsFromStore(b, edited, store)
	b.ReportAllocs()
	b.SetBytes(int64(edited.sourceBytes))
	b.ResetTimer()
	for range b.N {
		program, _, err := loadCompilerCorpusGraphWithStore(edited, store)
		if err != nil {
			b.Fatal(err)
		}
		compilerCorpusProgramSink = program
	}
	b.StopTimer()
	metrics.report(b, edited)
}

func benchmarkCompilerCorpusGraphConcurrent(b *testing.B, graph compilerCorpusGraph) {
	sources := make([]string, 0, len(graph.loader.sources))
	for _, source := range graph.loader.sources {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		if _, err := Compile(source); err != nil {
			b.Fatalf("graph %d independent source validation failed: %v", graph.size, err)
		}
	}
	if len(sources) == 0 {
		b.Fatal("graph has no independent module sources")
	}

	var next atomic.Uint64
	var firstErr error
	var errOnce sync.Once
	b.ReportAllocs()
	b.SetBytes(int64(graph.sourceBytes / graph.size))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			index := (next.Add(1) - 1) % uint64(len(sources))
			proto, err := Compile(sources[int(index)])
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				continue
			}
			runtimeKeepCompilerCorpusProto(proto)
		}
	})
	b.StopTimer()
	if firstErr != nil {
		b.Fatal(firstErr)
	}
	b.ReportMetric(float64(graph.sourceBytes), "graph_source_B/op")
	b.ReportMetric(float64(graph.size), "graph_modules/op")
}

func loadCompilerCorpusGraphWithStore(graph compilerCorpusGraph, store *sourceArtifactStore) (*Program, LoadReport, error) {
	return loadProgramWithArtifactStore(context.Background(), graph.loader, ProgramOptions{
		Entrypoints: []Entrypoint{{Name: "root", Module: graph.root}},
		Parallelism: 1,
	}, store)
}

type compilerCorpusGraphReport struct {
	program  CompilerBenchmarkMetrics
	modules  int
	diagnose int
}

func compilerCorpusGraphMetrics(b *testing.B, graph compilerCorpusGraph) compilerCorpusGraphReport {
	b.Helper()
	program, report, err := LoadProgram(context.Background(), graph.loader, ProgramOptions{
		Entrypoints: []Entrypoint{{Name: "root", Module: graph.root}},
		Parallelism: 1,
	})
	if err != nil {
		b.Fatal(err)
	}
	return compilerCorpusGraphReport{
		program:  CompilerProgramBenchmarkMetricsForTest(program),
		modules:  len(report.Modules),
		diagnose: len(report.Diagnostics),
	}
}

func compilerCorpusGraphMetricsFromStore(b *testing.B, graph compilerCorpusGraph, store *sourceArtifactStore) compilerCorpusGraphReport {
	b.Helper()
	program, report, err := loadCompilerCorpusGraphWithStore(graph, store)
	if err != nil {
		b.Fatal(err)
	}
	return compilerCorpusGraphReport{
		program:  CompilerProgramBenchmarkMetricsForTest(program),
		modules:  len(report.Modules),
		diagnose: len(report.Diagnostics),
	}
}

func (metrics compilerCorpusGraphReport) report(b *testing.B, graph compilerCorpusGraph) {
	b.ReportMetric(float64(graph.sourceBytes), "source_B/op")
	b.ReportMetric(float64(metrics.modules), "modules/op")
	b.ReportMetric(float64(metrics.diagnose), "diagnostics/op")
	b.ReportMetric(float64(metrics.program.Instructions), "instructions/op")
	b.ReportMetric(float64(metrics.program.Constants), "constants/op")
	b.ReportMetric(float64(metrics.program.RegisterSlots), "register_slots/op")
	b.ReportMetric(float64(metrics.program.ChildProtos), "child_protos/op")
	b.ReportMetric(float64(metrics.program.PackedBytes), "packed_B/op")
	b.ReportMetric(float64(metrics.program.ProtoOwnedBytes), "proto_owned_B/op")
	b.ReportMetric(float64(metrics.program.RetainedStringBytes), "retained_string_B/op")
}

func BenchmarkCompilerCorpusConcurrentCompile(b *testing.B) {
	fixtures := loadCompilerCorpusFixtures(b)
	valid := make([]compilerCorpusFixture, 0, len(fixtures))
	totalBytes := 0
	for _, fixture := range fixtures {
		if fixture.malformed {
			continue
		}
		valid = append(valid, fixture)
		totalBytes += len(fixture.source)
	}
	if len(valid) == 0 {
		b.Fatal("compiler corpus has no valid fixtures")
	}
	for _, fixture := range valid {
		validateCompilerCorpusFixture(b, fixture)
	}
	var next atomic.Uint64
	var firstErr error
	var errOnce sync.Once
	b.ReportAllocs()
	b.SetBytes(int64(totalBytes / len(valid)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			index := (next.Add(1) - 1) % uint64(len(valid))
			fixture := valid[int(index)]
			proto, err := Compile(fixture.source)
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				continue
			}
			runtimeKeepCompilerCorpusProto(proto)
		}
	})
	b.StopTimer()
	if firstErr != nil {
		b.Fatal(firstErr)
	}
}

func runtimeKeepCompilerCorpusProto(proto *Proto) {
	if proto == nil {
		panic("compiler corpus Compile returned nil proto")
	}
	// Keep the result observable without sharing a mutable sink between
	// benchmark workers.
	if proto.registers < 0 {
		panic("compiler corpus proto has negative register count")
	}
}
