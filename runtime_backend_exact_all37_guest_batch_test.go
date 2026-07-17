package ember

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/besmpl/ember/internal/parityfixture"
)

const (
	backendExactAll37GuestBatchGeneratedPath     = "runtime_backend_exact_all37_guest_batch_generated_test.go"
	backendExactAll37GuestBatchUpdateEnvironment = "EMBER_UPDATE_EXACT_ALL37_GUEST_BATCH_FIXTURE"
	backendExactAll37GuestBatchCaseCount         = 37
	backendExactAll37GuestBatchMaxCaseBytes      = 32 << 10
	backendExactAll37GuestBatchMaxGeneratedBytes = backendExactAll37GuestBatchCaseCount * backendExactAll37GuestBatchMaxCaseBytes
	backendExactAll37GuestBatchResultHash        = "337ddd482408423cf270f0b6e4376f70740cbe4480fc7ba8d437b17aaf314a7b"
)

type backendExactGuestBatchCase struct {
	corpus string
	name   string
	source string
}

type backendExactGuestBatchGeneratedCase struct {
	declarations       []byte
	caseProto          int32
	entryProto         int32
	function           string
	programHash        [32]byte
	holdoutProgramHash [32]byte
}

type backendExactGuestBatchPreparedArtifact struct {
	programHashes [2][32]byte
	caseProto     int32
	entryProto    int32
	function      machinePreparedFunction
}

func TestBackendGoExactAll37GuestBatchFixtureIsFresh(t *testing.T) {
	generated := backendExactAll37GuestBatchArtifact(t)
	if len(generated) > backendExactAll37GuestBatchMaxGeneratedBytes {
		t.Fatalf(
			"generated exact all-37 guest-batch bytes = %d, want at most %d",
			len(generated), backendExactAll37GuestBatchMaxGeneratedBytes,
		)
	}
	if os.Getenv(backendExactAll37GuestBatchUpdateEnvironment) == "1" {
		if err := os.WriteFile(backendExactAll37GuestBatchGeneratedPath, generated, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(backendExactAll37GuestBatchGeneratedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, generated) {
		t.Fatalf(
			"generated exact all-37 guest-batch fixture is stale; rerun with %s=1",
			backendExactAll37GuestBatchUpdateEnvironment,
		)
	}
}

func TestMachinePreparedExactAll37GuestBatchMatchesGeneric(t *testing.T) {
	cases := backendExactGuestBatchCases(t)
	if got := len(backendGeneratedExactAll37GuestBatchArtifacts); got != len(cases) {
		t.Fatalf("generated exact guest-batch function inventory = %d, want %d", got, len(cases))
	}
	hash := sha256.New()
	for _, tc := range cases {
		for _, holdout := range []bool{false, true} {
			variant := "standard"
			if holdout {
				variant = "holdout"
			}
			t.Run(tc.corpus+"/"+tc.name+"/"+variant, func(t *testing.T) {
				source := backendExactGuestBatchSource(t, tc.source, holdout)
				publicCallback, err := PrepareExactGuestBatchForParityTest(source)
				if err != nil {
					t.Fatal(err)
				}
				publicClosed := false
				t.Cleanup(func() {
					if !publicClosed {
						if err := publicCallback.Close(); err != nil {
							t.Error(err)
						}
					}
				})
				irs, _ := backendExactCorpusIRs(t, source)
				caseProto, entryProto := backendExactGuestBatchProtos(t, irs)
				preparedCalls := 0
				preparedReplays := 0
				programImage, err := backendExactGuestBatchProgramImage(source)
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := backendExactGuestBatchPreparedArtifactForImage(programImage)
				if err != nil {
					t.Fatal(err)
				}
				if artifact.caseProto != caseProto || artifact.entryProto != entryProto {
					t.Fatalf("selected Proto inventory = %d/%d, want %d/%d", artifact.caseProto, artifact.entryProto, caseProto, entryProto)
				}
				program := machinePreparedTestProgram(t, programImage, 0, entryProto, func(context machinePreparedContext) machinePreparedExit {
					preparedCalls++
					exit := artifact.function(context)
					if exit.kind != machinePreparedExitReturnOneNumber {
						preparedReplays++
					}
					return exit
				})
				prepared, err := newMachineOwnerWithPrepared(programImage, program)
				if err != nil {
					t.Fatal(err)
				}
				generic, err := newMachineOwner(programImage)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := prepared.close(); err != nil {
						t.Error(err)
					}
					if err := generic.close(); err != nil {
						t.Error(err)
					}
				}()
				preparedClosure := machineExactGuestBatchClosureAt(t, prepared, caseProto, entryProto)
				genericClosure := machineExactGuestBatchClosureAt(t, generic, caseProto, entryProto)
				preparedClosureCount := len(prepared.closures.closures)
				for _, seed := range []float64{0, 1, 7, 29} {
					preparedArgs := machineExactGuestBatchArgs(t, prepared, 3, seed)
					genericArgs := machineExactGuestBatchArgs(t, generic, 3, seed)
					runMachinePreparedTestClosure(t, prepared, entryProto, preparedClosure, preparedArgs, nil)
					runMachinePreparedTestClosure(t, generic, entryProto, genericClosure, genericArgs, nil)
					preparedResult := machineExactGuestBatchResult(t, prepared)
					genericResult := machineExactGuestBatchResult(t, generic)
					if math.Float64bits(preparedResult) != math.Float64bits(genericResult) {
						t.Fatalf("seed %g result = %g, canonical Machine = %g", seed, preparedResult, genericResult)
					}
					publicValues, err := publicCallback.Call(
						context.Background(),
						NumberValue(3),
						NumberValue(seed),
					)
					if err != nil {
						t.Fatalf("seed %g public prepared Runtime: %v", seed, err)
					}
					if len(publicValues) != 1 {
						t.Fatalf("seed %g public prepared Runtime results = %d, want 1", seed, len(publicValues))
					}
					publicResult, ok := publicValues[0].Number()
					if !ok || math.Float64bits(publicResult) != math.Float64bits(genericResult) {
						t.Fatalf("seed %g public prepared Runtime result = %g/%t, canonical Machine = %g", seed, publicResult, ok, genericResult)
					}
					fmt.Fprintf(hash, "%s/%s/%s/%g\t%016x\n", tc.corpus, tc.name, variant, seed, math.Float64bits(genericResult))
				}
				if err := publicCallback.Close(); err != nil {
					t.Fatal(err)
				}
				publicClosed = true
				if preparedCalls != 4 {
					t.Fatalf("prepared calls = %d, want 4", preparedCalls)
				}
				if preparedReplays != 0 {
					t.Fatalf("prepared replay exits = %d, want 0", preparedReplays)
				}
				if got := len(prepared.closures.closures); got != preparedClosureCount {
					t.Fatalf("prepared execution materialized %d closures, want %d", got, preparedClosureCount)
				}
				if !holdout && !checkptrInstrumentedTest() {
					args := machineExactGuestBatchArgs(t, prepared, 3, 17)
					lease, err := prepared.beginRun()
					if err != nil {
						t.Fatal(err)
					}
					var runErr error
					allocations := testing.AllocsPerRun(10, func() {
						runErr = prepared.executeStopped(0, entryProto, preparedClosure, args, nil, machineRunEffects{})
					})
					lease.end()
					if runErr != nil {
						t.Fatal(runErr)
					}
					if allocations != 0 {
						t.Fatalf("warmed prepared guest-batch allocations = %v, want 0", allocations)
					}
					if preparedReplays != 0 {
						t.Fatalf("allocation probe prepared replay exits = %d, want 0", preparedReplays)
					}
				}
			})
		}
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != backendExactAll37GuestBatchResultHash {
		t.Fatalf("exact all-37 guest-batch result hash = %s, want %s", got, backendExactAll37GuestBatchResultHash)
	}
}

func TestBackendGoExactAll37GuestBatchInventoryRejectsUnknownProgram(t *testing.T) {
	image := machinePreparedTestImageForSource(t, "return 1\n")
	if _, err := backendExactGuestBatchPreparedArtifactForImage(image); err == nil {
		t.Fatal("selected a prepared guest-batch artifact for an unknown Program")
	}
}

func TestPrepareExactGuestBatchForParityTestUsesVerifiedPreparedCallback(t *testing.T) {
	tc := loadLuauBenchmarkCases(t, "classicLuauCases", []string{"recursive_fibonacci"})[0]
	t.Setenv("EMBER_RUNTIME_ENGINE", "invalid-but-overridden")
	for _, holdout := range []bool{false, true} {
		variant := "standard"
		if holdout {
			variant = "holdout"
		}
		t.Run(variant, func(t *testing.T) {
			callback, err := PrepareExactGuestBatchForParityTest(backendExactGuestBatchSource(t, tc.source, holdout))
			if err != nil {
				t.Fatal(err)
			}
			target, ok := callback.target.(*backendExactGuestBatchCallbackTarget)
			if !ok || target.runtime == nil {
				t.Fatalf("prepared parity callback target = %T, want public Runtime target", callback.target)
			}
			execution, ok := target.runtime.execution.(*machineRuntimeExecution)
			if !ok || execution.prepared == nil {
				t.Fatalf("prepared parity execution = %T, want bound prepared Machine", target.runtime.execution)
			}
			values, err := callback.Call(context.Background(), NumberValue(3), NumberValue(17))
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 {
				t.Fatalf("prepared parity callback results = %d, want 1", len(values))
			}
			result, ok := values[0].Number()
			if !ok || result != 90152 {
				t.Fatalf("prepared parity callback result = %v/%t, want 90152", result, ok)
			}
			if err := callback.Close(); err != nil {
				t.Fatal(err)
			}
			if err := callback.Close(); err != nil {
				t.Fatalf("repeat close: %v", err)
			}
			if _, err := callback.Call(context.Background(), NumberValue(3), NumberValue(17)); err == nil {
				t.Fatal("closed prepared parity callback accepted a call")
			}
		})
	}
}

func backendExactAll37GuestBatchArtifact(t *testing.T) []byte {
	t.Helper()
	cases := backendExactGuestBatchCases(t)
	var declarations bytes.Buffer
	imports := make(map[string]bool)
	generatedCases := make([]backendExactGuestBatchGeneratedCase, 0, len(cases))
	for index, tc := range cases {
		standardImports := make(map[string]bool)
		standard := backendExactGuestBatchGenerateCase(t, tc, index, false, standardImports)
		holdoutImports := make(map[string]bool)
		holdout := backendExactGuestBatchGenerateCase(t, tc, index, true, holdoutImports)
		if !bytes.Equal(standard.declarations, holdout.declarations) ||
			standard.caseProto != holdout.caseProto ||
			standard.entryProto != holdout.entryProto ||
			standard.function != holdout.function ||
			!mapsEqual(standardImports, holdoutImports) {
			t.Fatalf("%s/%s identity holdout changed generated executable inventory", tc.corpus, tc.name)
		}
		for path := range standardImports {
			imports[path] = true
		}
		standard.holdoutProgramHash = holdout.programHash
		if len(standard.declarations) > backendExactAll37GuestBatchMaxCaseBytes {
			t.Fatalf(
				"%s/%s generated bytes = %d, want at most %d",
				tc.corpus, tc.name, len(standard.declarations), backendExactAll37GuestBatchMaxCaseBytes,
			)
		}
		declarations.Write(standard.declarations)
		declarations.WriteString("\n\n")
		generatedCases = append(generatedCases, standard)
	}
	var source bytes.Buffer
	source.WriteString("// Code generated by Ember's private prepared proof compiler; DO NOT EDIT.\n\npackage ember\n")
	if len(imports) != 0 {
		source.WriteString("\nimport (\n")
		for _, path := range []string{"math"} {
			if imports[path] {
				fmt.Fprintf(&source, "\t%q\n", path)
				delete(imports, path)
			}
		}
		if len(imports) != 0 {
			t.Fatalf("generated exact guest-batch imports are unsupported: %v", imports)
		}
		source.WriteString(")\n")
	}
	source.WriteString("\n")
	source.Write(declarations.Bytes())
	source.WriteString("var backendGeneratedExactAll37GuestBatchArtifacts = [...]backendExactGuestBatchPreparedArtifact{\n")
	for _, generated := range generatedCases {
		source.WriteString("\t{programHashes: [2][32]byte{")
		for hashIndex, hash := range [2][32]byte{generated.programHash, generated.holdoutProgramHash} {
			if hashIndex != 0 {
				source.WriteString(", ")
			}
			source.WriteString("{")
			for index, value := range hash {
				if index != 0 {
					source.WriteString(", ")
				}
				fmt.Fprintf(&source, "0x%02x", value)
			}
			source.WriteString("}")
		}
		fmt.Fprintf(
			&source,
			"}, caseProto: %d, entryProto: %d, function: %s},\n",
			generated.caseProto,
			generated.entryProto,
			generated.function,
		)
	}
	source.WriteString("}\n")
	formatted, err := format.Source(source.Bytes())
	if err != nil {
		t.Fatalf("format generated exact all-37 guest-batch artifact: %v", err)
	}
	return formatted
}

func backendExactGuestBatchGenerateCase(
	t *testing.T,
	tc backendExactGuestBatchCase,
	index int,
	holdout bool,
	imports map[string]bool,
) backendExactGuestBatchGeneratedCase {
	t.Helper()
	source := backendExactGuestBatchSource(t, tc.source, holdout)
	irs, image := backendExactCorpusIRs(t, source)
	caseProto, entryProto := backendExactGuestBatchProtos(t, irs)
	programImage, err := backendExactGuestBatchProgramImage(source)
	if err != nil {
		t.Fatal(err)
	}
	programIR, err := buildBackendProgramIR(programImage)
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("backendGeneratedExactAll37GuestBatchCase%02d", index)
	preparedFunction := prefix + "Prepared"
	files, err := emitBackendGoNumericProgram(irs, backendGoNumericProgramOptions{
		packageName:          "ember",
		functionPrefix:       prefix,
		preparedFunctionName: preparedFunction,
		entryProto:           entryProto,
		coroutineDeadString:  backendCoroutineDeadStringID(t, image),
	})
	if err != nil {
		t.Fatalf("%s/%s: %v", tc.corpus, tc.name, err)
	}
	var declarations bytes.Buffer
	for _, file := range files {
		backendExactGuestBatchAppendDeclarations(t, &declarations, imports, file.source)
	}
	return backendExactGuestBatchGeneratedCase{
		declarations: declarations.Bytes(),
		caseProto:    caseProto,
		entryProto:   entryProto,
		function:     preparedFunction,
		programHash:  programIR.programHash,
	}
}

func backendExactGuestBatchPreparedArtifactForImage(
	image *programImage,
) (backendExactGuestBatchPreparedArtifact, error) {
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		return backendExactGuestBatchPreparedArtifact{}, fmt.Errorf("select prepared guest batch: %w", err)
	}
	var selected backendExactGuestBatchPreparedArtifact
	found := false
	for _, artifact := range backendGeneratedExactAll37GuestBatchArtifacts {
		matches := false
		for _, programHash := range artifact.programHashes {
			matches = matches || programHash == ir.programHash
		}
		if !matches {
			continue
		}
		if found {
			return backendExactGuestBatchPreparedArtifact{}, fmt.Errorf("select prepared guest batch: ambiguous Program hash")
		}
		selected = artifact
		found = true
	}
	if !found {
		return backendExactGuestBatchPreparedArtifact{}, fmt.Errorf("select prepared guest batch: unknown Program hash")
	}
	if len(image.modules) != 1 || image.modules[0].code == nil ||
		selected.caseProto <= 0 || selected.entryProto <= 0 ||
		int(selected.caseProto) >= len(image.modules[0].code.prototypes) ||
		int(selected.entryProto) >= len(image.modules[0].code.prototypes) ||
		selected.function == nil {
		return backendExactGuestBatchPreparedArtifact{}, fmt.Errorf("select prepared guest batch: invalid generated inventory")
	}
	return selected, nil
}

func mapsEqual(left, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func backendExactGuestBatchProgramImage(source string) (*programImage, error) {
	program, err := backendExactGuestBatchProgram(source)
	if err != nil {
		return nil, err
	}
	return program.preparedProgramImage()
}

func backendExactGuestBatchAppendDeclarations(
	t *testing.T,
	destination *bytes.Buffer,
	imports map[string]bool,
	source []byte,
) {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := goparser.ParseFile(fileSet, "generated.go", source, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		imports[path] = true
	}
	for _, declaration := range file.Decls {
		if general, ok := declaration.(*ast.GenDecl); ok && general.Tok == token.IMPORT {
			continue
		}
		if err := format.Node(destination, fileSet, declaration); err != nil {
			t.Fatal(err)
		}
		destination.WriteString("\n\n")
	}
}

func backendExactGuestBatchCases(t testing.TB) []backendExactGuestBatchCase {
	t.Helper()
	var cases []backendExactGuestBatchCase
	for _, group := range backendExactBenchmarkGroups() {
		for _, tc := range loadLuauBenchmarkCases(t, group.variable, group.cases) {
			cases = append(cases, backendExactGuestBatchCase{
				corpus: group.name,
				name:   tc.name,
				source: tc.source,
			})
		}
	}
	if len(cases) != backendExactAll37GuestBatchCaseCount {
		t.Fatalf("exact guest-batch cases = %d, want %d", len(cases), backendExactAll37GuestBatchCaseCount)
	}
	return cases
}

func backendExactGuestBatchSource(t testing.TB, caseSource string, holdout bool) string {
	t.Helper()
	variant := parityfixture.GuestBatchVariant{CaseName: "__case", BatchName: "__batch"}
	if holdout {
		variant = parityfixture.GuestBatchVariant{
			CaseName: "__holdout_case", BatchName: "__holdout_batch", Holdout: true,
		}
	}
	fixture, err := parityfixture.BuildGuestBatch(caseSource, variant)
	if err != nil {
		t.Fatal(err)
	}
	return fixture.Program + "return " + variant.BatchName + "\n"
}

func backendExactGuestBatchProtos(t testing.TB, irs []*backendProtoIR) (int32, int32) {
	t.Helper()
	if len(irs) == 0 || irs[0] == nil {
		t.Fatal("guest-batch root Proto is unavailable")
	}
	var protos []int32
	for pc := range irs[0].ops {
		operation := &irs[0].ops[pc]
		if operation.op == opClosure {
			protos = append(protos, operation.targetProto)
		}
	}
	if len(protos) != 2 {
		t.Fatalf("guest-batch root closure inventory = %v, want case and batch", protos)
	}
	return protos[0], protos[1]
}

func machineExactGuestBatchClosureAt(
	t *testing.T,
	owner *machineOwner,
	caseProto int32,
	entryProto int32,
) machineClosureHandle {
	t.Helper()
	caseClosure, err := owner.closures.createClosureStopped(0, machineProtoID(caseProto), nil)
	if err != nil {
		t.Fatal(err)
	}
	caseValue, err := slotPackHandle(slotTagClosure, caseClosure.index, caseClosure.generation)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := owner.closures.createClosureStopped(0, machineProtoID(entryProto), []machineCaptureDescriptor{{
		mode: machineCaptureByValue, value: caseValue,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func machineExactGuestBatchResult(t *testing.T, owner *machineOwner) float64 {
	t.Helper()
	value, err := owner.resultAt(0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.number(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
