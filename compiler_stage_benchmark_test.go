package ember

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

type compilerStageFixture struct {
	name          string
	source        string
	artifact      sourceArtifact
	emission      compilerStageEmission
	optimized     compilerStageOptimization
	outputMetrics CompilerBenchmarkMetrics
}

type compilerStageOptimization struct {
	ir        []bytecodeIRInstruction
	constants []Value
}

type compilerStageEmission struct {
	ir                 []bytecodeIRInstruction
	constants          []Value
	children           []*functionDraft
	upvalues           []upvalueDesc
	allocatedRegisters int
	params             int
	variadic           bool
	sourceLines        sourceLineMap
}

type compilerStageMetrics struct {
	syntaxNodes         int
	irInstructions      int
	cfgBlocks           int
	peakRegisters       int
	packedBytes         int64
	protoOwnedBytes     int64
	retainedStringBytes int64
}

var (
	compilerStageTokensSink        []sourceToken
	compilerStageCommentsSink      []sourceComment
	compilerStageProgramSink       program
	compilerStageBindSink          bindResult
	compilerStageEmissionSink      compilerStageEmission
	compilerStageIRSink            []bytecodeIRInstruction
	compilerStageProtoSink         *Proto
	compilerStageProgramResultSink *Program
)

func TestCompilerStageSourceBuckets(t *testing.T) {
	for _, size := range []int{1 << 10, 4 << 10, 16 << 10, 64 << 10, 256 << 10} {
		source := compilerStageSource(size)
		if got := len(source); got != size {
			t.Fatalf("compiler stage source size = %d, want %d", got, size)
		}
		if _, err := parseSource(Source{Text: source}); err != nil {
			t.Fatalf("compiler stage source %d bytes did not parse: %v", size, err)
		}
	}
}

func TestCompilerStageOptimizationUsesMutableConstantPool(t *testing.T) {
	source := "local value = 1\nvalue = value + 2\nreturn value\n"
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	emission, err := emitCompilerStage(artifact)
	if err != nil {
		t.Fatalf("emitCompilerStage returned error: %v", err)
	}
	optimized := optimizeCompilerStageIR(emission)
	foundFoldedValue := false
	for _, constant := range optimized.constants {
		if valuesEqual(constant, NumberValue(3)) {
			foundFoldedValue = true
			break
		}
	}
	if !foundFoldedValue {
		t.Fatalf("optimized constants = %#v, want newly interned folded value 3 from %d source constants", optimized.constants, len(emission.constants))
	}
	proto, err := assembleAndSealCompilerStage(emission, optimized)
	if err != nil {
		t.Fatalf("assembleAndSealCompilerStage returned error: %v", err)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(values) != 1 || !valuesEqual(values[0], NumberValue(3)) {
		t.Fatalf("optimized stage result = %#v, want [3]", values)
	}
}

func BenchmarkCompilerStageMatrix(b *testing.B) {
	fixtures, err := prepareCompilerStageFixtures()
	if err != nil {
		b.Fatal(err)
	}

	for _, fixture := range fixtures {
		fixture := fixture
		b.Run(fixture.name+"/lex", func(b *testing.B) {
			benchmarkCompilerStageLex(b, fixture)
		})
		b.Run(fixture.name+"/parse", func(b *testing.B) {
			benchmarkCompilerStageParse(b, fixture)
		})
		b.Run(fixture.name+"/bind", func(b *testing.B) {
			benchmarkCompilerStageBind(b, fixture)
		})
		b.Run(fixture.name+"/emit", func(b *testing.B) {
			benchmarkCompilerStageEmit(b, fixture)
		})
		b.Run(fixture.name+"/optimize", func(b *testing.B) {
			benchmarkCompilerStageOptimize(b, fixture)
		})
		b.Run(fixture.name+"/assemble_seal", func(b *testing.B) {
			benchmarkCompilerStageAssembleSeal(b, fixture)
		})
		b.Run(fixture.name+"/compile", func(b *testing.B) {
			benchmarkCompilerStageCompile(b, fixture)
		})
		b.Run(fixture.name+"/load_program", func(b *testing.B) {
			benchmarkCompilerStageLoadProgram(b, fixture)
		})
	}
}

func benchmarkCompilerStageLex(b *testing.B, fixture compilerStageFixture) {
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		tokens, comments, _, err := lexSource(fixture.source)
		if err != nil {
			b.Fatal(err)
		}
		compilerStageTokensSink = tokens
		compilerStageCommentsSink = comments
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{})
}

func benchmarkCompilerStageParse(b *testing.B, fixture compilerStageFixture) {
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		parsed, err := (&parser{source: fixture.source}).parse()
		if err != nil {
			b.Fatal(err)
		}
		compilerStageProgramSink = parsed
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{
		syntaxNodes: fixture.artifact.program.nodeCount,
	})
}

func benchmarkCompilerStageBind(b *testing.B, fixture compilerStageFixture) {
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		bound := bindProgram(fixture.artifact.program)
		compilerStageBindSink = bound
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{
		syntaxNodes: fixture.artifact.program.nodeCount,
	})
}

func benchmarkCompilerStageEmit(b *testing.B, fixture compilerStageFixture) {
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		emission, err := emitCompilerStage(fixture.artifact)
		if err != nil {
			b.Fatal(err)
		}
		compilerStageEmissionSink = emission
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{
		syntaxNodes:    fixture.artifact.program.nodeCount,
		irInstructions: len(fixture.emission.ir),
		cfgBlocks:      len(bytecodeIRBlockOrder(fixture.emission.ir)),
		peakRegisters:  fixture.emission.allocatedRegisters,
	})
}

func benchmarkCompilerStageOptimize(b *testing.B, fixture compilerStageFixture) {
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		optimized := optimizeCompilerStageIR(fixture.emission)
		compilerStageIRSink = optimized.ir
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{
		syntaxNodes:    fixture.artifact.program.nodeCount,
		irInstructions: len(fixture.optimized.ir),
		cfgBlocks:      len(bytecodeIRBlockOrder(fixture.optimized.ir)),
		peakRegisters:  compilerStagePeakRegisters(fixture.optimized.ir),
	})
}

func benchmarkCompilerStageAssembleSeal(b *testing.B, fixture compilerStageFixture) {
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		proto, err := assembleAndSealCompilerStage(fixture.emission, fixture.optimized)
		if err != nil {
			b.Fatal(err)
		}
		compilerStageProtoSink = proto
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{
		syntaxNodes:         fixture.artifact.program.nodeCount,
		irInstructions:      len(fixture.optimized.ir),
		cfgBlocks:           len(bytecodeIRBlockOrder(fixture.optimized.ir)),
		peakRegisters:       compilerStagePeakRegisters(fixture.optimized.ir),
		packedBytes:         fixture.outputMetrics.PackedBytes,
		protoOwnedBytes:     fixture.outputMetrics.ProtoOwnedBytes,
		retainedStringBytes: fixture.outputMetrics.RetainedStringBytes,
	})
}

func benchmarkCompilerStageCompile(b *testing.B, fixture compilerStageFixture) {
	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		proto, err := Compile(fixture.source)
		if err != nil {
			b.Fatal(err)
		}
		compilerStageProtoSink = proto
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{
		syntaxNodes:         fixture.artifact.program.nodeCount,
		irInstructions:      fixture.outputMetrics.Instructions,
		cfgBlocks:           len(bytecodeIRBlockOrder(fixture.optimized.ir)),
		peakRegisters:       fixture.outputMetrics.RegisterSlots,
		packedBytes:         fixture.outputMetrics.PackedBytes,
		protoOwnedBytes:     fixture.outputMetrics.ProtoOwnedBytes,
		retainedStringBytes: fixture.outputMetrics.RetainedStringBytes,
	})
}

func benchmarkCompilerStageLoadProgram(b *testing.B, fixture compilerStageFixture) {
	loader := compilerStageModuleLoader{
		source: Source{Name: LogicalModule(fixture.name).String(), Text: fixture.source},
	}
	options := ProgramOptions{
		Entrypoints: []Entrypoint{{Name: "stage", Module: LogicalModule(fixture.name)}},
		Parallelism: 1,
	}
	if _, _, err := LoadProgram(context.Background(), loader, options); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.source)))
	b.ResetTimer()
	for range b.N {
		program, _, err := LoadProgram(context.Background(), loader, options)
		if err != nil {
			b.Fatal(err)
		}
		compilerStageProgramResultSink = program
	}
	b.StopTimer()
	reportCompilerStageMetrics(b, fixture, compilerStageMetrics{
		syntaxNodes:         fixture.artifact.program.nodeCount,
		irInstructions:      fixture.outputMetrics.Instructions,
		cfgBlocks:           len(bytecodeIRBlockOrder(fixture.optimized.ir)),
		peakRegisters:       fixture.outputMetrics.RegisterSlots,
		packedBytes:         fixture.outputMetrics.PackedBytes,
		protoOwnedBytes:     fixture.outputMetrics.ProtoOwnedBytes,
		retainedStringBytes: fixture.outputMetrics.RetainedStringBytes,
	})
}

func reportCompilerStageMetrics(b *testing.B, fixture compilerStageFixture, metrics compilerStageMetrics) {
	b.ReportMetric(float64(len(fixture.source)), "source_B/op")
	b.ReportMetric(float64(metrics.syntaxNodes), "syntax_nodes/op")
	b.ReportMetric(float64(metrics.irInstructions), "ir_instructions/op")
	b.ReportMetric(float64(metrics.cfgBlocks), "cfg_blocks/op")
	b.ReportMetric(float64(metrics.peakRegisters), "peak_registers/op")
	b.ReportMetric(float64(metrics.packedBytes), "packed_B/op")
	b.ReportMetric(float64(metrics.protoOwnedBytes), "proto_owned_B/op")
	b.ReportMetric(float64(metrics.retainedStringBytes), "retained_string_B/op")
}

func prepareCompilerStageFixtures() ([]compilerStageFixture, error) {
	sizes := []int{1 << 10, 4 << 10, 16 << 10, 64 << 10, 256 << 10}
	fixtures := make([]compilerStageFixture, 0, len(sizes))
	for _, size := range sizes {
		name := strconv.Itoa(size/1024) + "KiB"
		source := compilerStageSource(size)
		artifact, err := parseSource(Source{Name: name, Text: source})
		if err != nil {
			return nil, fmt.Errorf("prepare %s: parse: %w", name, err)
		}
		emission, err := emitCompilerStage(artifact)
		if err != nil {
			return nil, fmt.Errorf("prepare %s: emit: %w", name, err)
		}
		optimized := optimizeCompilerStageIR(emission)
		proto, err := assembleAndSealCompilerStage(emission, optimized)
		if err != nil {
			return nil, fmt.Errorf("prepare %s: seal: %w", name, err)
		}
		fixtures = append(fixtures, compilerStageFixture{
			name:          name,
			source:        source,
			artifact:      artifact,
			emission:      emission,
			optimized:     optimized,
			outputMetrics: CompilerBenchmarkMetricsForTest(proto),
		})
	}
	return fixtures, nil
}

func compilerStageSource(size int) string {
	const prefix = "local value = 0\n"
	const statement = "value = value + 1\n"
	const suffix = `return value, "stage"`
	if size < len(prefix)+len(suffix) {
		size = len(prefix) + len(suffix)
	}

	var source strings.Builder
	source.Grow(size)
	source.WriteString(prefix)
	for source.Len()+len(statement)+len(suffix) <= size {
		source.WriteString(statement)
	}
	source.WriteString(suffix)
	if source.Len() < size {
		source.WriteString(strings.Repeat(" ", size-source.Len()))
	}
	return source.String()
}

func emitCompilerStage(artifact sourceArtifact) (compilerStageEmission, error) {
	c := compiler{
		bind:               artifact.bind,
		sourceLines:        newSourceLineMap(artifact.source.Text),
		symbolRegisters:    newDenseSymbolSlots(len(artifact.bind.symbols)),
		locals:             make(map[string]int),
		selfFunctionSymbol: -1,
		options: compilerOptions{
			optimizations: optimizationOptions{disableAll: true},
		},
	}
	c.sourceText = artifact.source.Text
	if err := c.compileStatements(artifact.program.statements); err != nil {
		return compilerStageEmission{}, err
	}
	if !statementsHaveReturn(artifact.program.statements) {
		c.emit(instruction{op: opReturn})
	}
	return compilerStageEmission{
		ir:                 append([]bytecodeIRInstruction(nil), c.ir...),
		constants:          append([]Value(nil), c.constants...),
		children:           append([]*functionDraft(nil), c.prototypeDrafts...),
		upvalues:           append([]upvalueDesc(nil), c.upvalueDescs...),
		allocatedRegisters: c.nextReg,
		sourceLines:        c.sourceLines,
	}, nil
}

func optimizeCompilerStageIR(emission compilerStageEmission) compilerStageOptimization {
	constantPool := bytecodeBuilder{}
	constantPool.resetConstants(append([]Value(nil), emission.constants...))
	optimized := optimizeBytecodeIRWithFacts(
		cloneCompilerStageIR(emission.ir),
		bytecodeIROptimizationFacts{
			constants:         constantPool.constants,
			capturedRegisters: functionDraftCapturedRegisters(emission.children),
			constantPool:      &constantPool,
		},
		defaultCompilerOptions().optimizations,
	)
	return compilerStageOptimization{
		ir:        optimized,
		constants: append([]Value(nil), constantPool.constants...),
	}
}

func cloneCompilerStageIR(ir []bytecodeIRInstruction) []bytecodeIRInstruction {
	cloned := make([]bytecodeIRInstruction, len(ir))
	copy(cloned, ir)
	return cloned
}

func assembleAndSealCompilerStage(emission compilerStageEmission, optimized compilerStageOptimization) (*Proto, error) {
	assembly := assembleFunctionBytecode(emission.sourceLines, optimized.ir)
	registers := compactedCompiledRegisterCount(
		assembly.code,
		emission.children,
		emission.allocatedRegisters,
		emission.params,
	)
	draft := newFunctionDraft(
		append([]Value(nil), optimized.constants...),
		assembly,
		emission.children,
		emission.upvalues,
		registers,
		emission.params,
		emission.variadic,
	)
	return sealFunctionDraft(draft)
}

func compilerStagePeakRegisters(ir []bytecodeIRInstruction) int {
	peak := 0
	for _, item := range assembleBytecodeIR(ir) {
		iterator := instructionRegisters(item, instructionRegisterReadWrite)
		for register, ok := iterator.next(); ok; register, ok = iterator.next() {
			if register+1 > peak {
				peak = register + 1
			}
		}
	}
	return peak
}

type compilerStageModuleLoader struct {
	source Source
}

func (loader compilerStageModuleLoader) LoadModule(ctx context.Context, id ModuleID) (Source, error) {
	if err := ctx.Err(); err != nil {
		return Source{}, err
	}
	if id.String() != loader.source.Name {
		return Source{}, fmt.Errorf("missing source %s", id.String())
	}
	return loader.source, nil
}

func BenchmarkSourceArtifactStoreHits(b *testing.B) {
	source := Source{Name: "logical:compiler/stage/hit", Text: compilerStageSource(16 << 10)}
	identity := identifyModuleSource(source)
	for _, operation := range []string{"parse", "compile"} {
		b.Run(operation, func(b *testing.B) {
			store := newSourceArtifactStore()
			var metrics CompilerBenchmarkMetrics
			switch operation {
			case "parse":
				if _, err := store.parse(source, identity); err != nil {
					b.Fatal(err)
				}
			case "compile":
				proto, err := store.compile(source, identity)
				if err != nil {
					b.Fatal(err)
				}
				metrics = CompilerBenchmarkMetricsForTest(proto)
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(source.Text)))
			b.ResetTimer()
			for range b.N {
				switch operation {
				case "parse":
					artifact, err := store.parse(source, identity)
					if err != nil {
						b.Fatal(err)
					}
					compilerStageProgramSink = artifact.program
				case "compile":
					proto, err := store.compile(source, identity)
					if err != nil {
						b.Fatal(err)
					}
					compilerStageProtoSink = proto
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(len(source.Text)), "source_B/op")
			b.ReportMetric(float64(metrics.Instructions), "instructions/op")
			b.ReportMetric(float64(metrics.Constants), "constants/op")
			b.ReportMetric(float64(metrics.RegisterSlots), "register_slots/op")
			b.ReportMetric(float64(metrics.PackedBytes), "packed_B/op")
			b.ReportMetric(float64(metrics.ProtoOwnedBytes), "proto_owned_B/op")
			b.ReportMetric(float64(metrics.RetainedStringBytes), "retained_string_B/op")
		})
	}
}
