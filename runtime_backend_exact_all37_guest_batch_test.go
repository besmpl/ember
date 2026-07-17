package ember

import (
	"bytes"
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
	declarations []byte
	caseProto    int32
	entryProto   int32
	function     string
}

func TestBackendGoExactAll37GuestBatchFixtureIsFresh(t *testing.T) {
	standard := backendExactAll37GuestBatchArtifact(t, false)
	holdout := backendExactAll37GuestBatchArtifact(t, true)
	if !bytes.Equal(standard, holdout) {
		t.Fatal("identity holdout changed the all-37 generated guest-batch artifact")
	}
	if len(standard) > backendExactAll37GuestBatchMaxGeneratedBytes {
		t.Fatalf(
			"generated exact all-37 guest-batch bytes = %d, want at most %d",
			len(standard), backendExactAll37GuestBatchMaxGeneratedBytes,
		)
	}
	if os.Getenv(backendExactAll37GuestBatchUpdateEnvironment) == "1" {
		if err := os.WriteFile(backendExactAll37GuestBatchGeneratedPath, standard, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(backendExactAll37GuestBatchGeneratedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, standard) {
		t.Fatalf(
			"generated exact all-37 guest-batch fixture is stale; rerun with %s=1",
			backendExactAll37GuestBatchUpdateEnvironment,
		)
	}
}

func TestMachinePreparedExactAll37GuestBatchMatchesGeneric(t *testing.T) {
	cases := backendExactGuestBatchCases(t)
	if got := len(backendGeneratedExactAll37GuestBatchFunctions); got != len(cases) {
		t.Fatalf("generated exact guest-batch function inventory = %d, want %d", got, len(cases))
	}
	hash := sha256.New()
	for index, tc := range cases {
		for _, holdout := range []bool{false, true} {
			variant := "standard"
			if holdout {
				variant = "holdout"
			}
			t.Run(tc.corpus+"/"+tc.name+"/"+variant, func(t *testing.T) {
				source := backendExactGuestBatchSource(t, tc.source, holdout)
				irs, _ := backendExactCorpusIRs(t, source)
				caseProto, entryProto := backendExactGuestBatchProtos(t, irs)
				preparedCalls := 0
				preparedReplays := 0
				generated := backendGeneratedExactAll37GuestBatchFunctions[index]
				programImage := machinePreparedTestImageForSource(t, source)
				program := machinePreparedTestProgram(t, programImage, 0, entryProto, func(context machinePreparedContext) machinePreparedExit {
					preparedCalls++
					exit := generated(context)
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
					fmt.Fprintf(hash, "%s/%s/%s/%g\t%016x\n", tc.corpus, tc.name, variant, seed, math.Float64bits(genericResult))
				}
				if preparedCalls != 4 {
					t.Fatalf("prepared calls = %d, want 4", preparedCalls)
				}
				if preparedReplays != 0 {
					t.Fatalf("prepared replay exits = %d, want 0", preparedReplays)
				}
				if got := len(prepared.closures.closures); got != preparedClosureCount {
					t.Fatalf("prepared execution materialized %d closures, want %d", got, preparedClosureCount)
				}
			})
		}
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != backendExactAll37GuestBatchResultHash {
		t.Fatalf("exact all-37 guest-batch result hash = %s, want %s", got, backendExactAll37GuestBatchResultHash)
	}
}

func backendExactAll37GuestBatchArtifact(t *testing.T, holdout bool) []byte {
	t.Helper()
	cases := backendExactGuestBatchCases(t)
	var declarations bytes.Buffer
	imports := make(map[string]bool)
	functions := make([]string, 0, len(cases))
	for index, tc := range cases {
		generated := backendExactGuestBatchGenerateCase(t, tc, index, holdout, imports)
		if len(generated.declarations) > backendExactAll37GuestBatchMaxCaseBytes {
			t.Fatalf(
				"%s/%s generated bytes = %d, want at most %d",
				tc.corpus, tc.name, len(generated.declarations), backendExactAll37GuestBatchMaxCaseBytes,
			)
		}
		declarations.Write(generated.declarations)
		declarations.WriteString("\n\n")
		functions = append(functions, generated.function)
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
	source.WriteString("var backendGeneratedExactAll37GuestBatchFunctions = [...]machinePreparedFunction{\n")
	for _, function := range functions {
		fmt.Fprintf(&source, "\t%s,\n", function)
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
	}
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
			CaseName: "holdoutCase", BatchName: "holdoutBatch", Holdout: true,
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
