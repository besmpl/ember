package ember

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/besmpl/ember/internal/parityfixture"
)

const (
	backendExactRecursiveGuestBatchGeneratedPrefix   = "runtime_backend_exact_recursive_guest_batch_proto_"
	backendExactRecursiveGuestBatchMaxGeneratedBytes = 16 << 10
)

func TestBackendGoExactRecursiveGuestBatchFixturesAreFresh(t *testing.T) {
	files, _ := backendExactRecursiveGuestBatchFiles(t)
	expected := make(map[string][]byte, len(files))
	generatedBytes := 0
	for _, file := range files {
		path := backendExactRecursiveGuestBatchGeneratedPrefix + strconv.Itoa(int(file.protoID)) + "_generated_test.go"
		expected[path] = file.source
		generatedBytes += len(file.source)
	}
	if generatedBytes > backendExactRecursiveGuestBatchMaxGeneratedBytes {
		t.Fatalf("generated exact recursive fixture bytes = %d, want at most %d", generatedBytes, backendExactRecursiveGuestBatchMaxGeneratedBytes)
	}
	if os.Getenv("EMBER_UPDATE_EXACT_GUEST_BATCH_FIXTURES") == "1" {
		for path, source := range expected {
			if err := os.WriteFile(path, source, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	paths, err := filepath.Glob(backendExactRecursiveGuestBatchGeneratedPrefix + "*_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(expected) {
		t.Fatalf("generated exact recursive fixture count = %d, want %d", len(paths), len(expected))
	}
	for _, path := range paths {
		want, ok := expected[path]
		if !ok {
			t.Fatalf("unexpected generated exact recursive fixture %s", path)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("generated exact recursive fixture %s is stale", path)
		}
	}
}

func TestMachinePreparedExactRecursiveGuestBatchMatchesGeneric(t *testing.T) {
	_, source := backendExactRecursiveGuestBatchFiles(t)
	image := machinePreparedTestImageForSource(t, source)
	preparedCalls := 0
	program := machinePreparedTestProgram(t, image, 0, 3, func(context machinePreparedContext) machinePreparedExit {
		preparedCalls++
		return backendGeneratedExactRecursiveGuestBatchPrepared(context)
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := prepared.close(); err != nil {
			t.Error(err)
		}
		if err := generic.close(); err != nil {
			t.Error(err)
		}
	})
	preparedClosure := machineExactRecursiveGuestBatchClosure(t, prepared)
	genericClosure := machineExactRecursiveGuestBatchClosure(t, generic)
	preparedArgs := machineExactGuestBatchArgs(t, prepared, 3, 17)
	genericArgs := machineExactGuestBatchArgs(t, generic, 3, 17)
	preparedClosureCount := len(prepared.closures.closures)

	runMachinePreparedTestClosure(t, prepared, 3, preparedClosure, preparedArgs, nil)
	runMachinePreparedTestClosure(t, generic, 3, genericClosure, genericArgs, nil)
	const want = 90152
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if preparedCalls != 1 {
		t.Fatalf("prepared exact recursive batch calls = %d, want 1", preparedCalls)
	}
	if len(prepared.closures.closures) != preparedClosureCount {
		t.Fatalf("prepared exact recursive batch materialized %d closures, want %d", len(prepared.closures.closures), preparedClosureCount)
	}

	if checkptrInstrumentedTest() {
		return
	}
	lease, err := prepared.beginRun()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.end()
	var runErr error
	allocations := testing.AllocsPerRun(1000, func() {
		runErr = prepared.executeStopped(0, 3, preparedClosure, preparedArgs, nil, machineRunEffects{})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if allocations != 0 {
		t.Fatalf("prepared exact recursive guest-batch allocations = %v, want 0", allocations)
	}
}

func BenchmarkMachinePreparedExactRecursiveGuestBatchOwner(b *testing.B) {
	source := backendExactRecursiveGuestBatchSource(b)
	image := machinePreparedBenchmarkImage(b, source)
	program := machinePreparedBenchmarkProgramAt(
		b, image, 3, backendGeneratedExactRecursiveGuestBatchPrepared,
	)
	benchmarkMachineExactRecursiveGuestBatchOwner(b, image, program)
}

func BenchmarkMachineGenericExactRecursiveGuestBatchOwner(b *testing.B) {
	source := backendExactRecursiveGuestBatchSource(b)
	image := machinePreparedBenchmarkImage(b, source)
	benchmarkMachineExactRecursiveGuestBatchOwner(b, image, nil)
}

func benchmarkMachineExactRecursiveGuestBatchOwner(
	b *testing.B,
	image *programImage,
	program *machinePreparedProgram,
) {
	b.Helper()
	owner, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := owner.close(); err != nil {
			b.Error(err)
		}
	})
	closure := machineExactRecursiveGuestBatchClosure(b, owner)
	args := machineExactGuestBatchArgs(b, owner, 1000, 17)
	lease, err := owner.beginRun()
	if err != nil {
		b.Fatal(err)
	}
	defer lease.end()
	if err := owner.executeStopped(0, 3, closure, args, nil, machineRunEffects{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := owner.executeStopped(0, 3, closure, args, nil, machineRunEffects{}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	result, err := owner.number(owner.results[0])
	if err != nil {
		b.Fatal(err)
	}
	backendGeneratedNumericSink = result
}

func machineExactRecursiveGuestBatchClosure(tb testing.TB, owner *machineOwner) machineClosureHandle {
	tb.Helper()
	caseClosure, err := owner.closures.createClosureStopped(0, 1, nil)
	if err != nil {
		tb.Fatal(err)
	}
	caseValue, err := slotPackHandle(slotTagClosure, caseClosure.index, caseClosure.generation)
	if err != nil {
		tb.Fatal(err)
	}
	batch, err := owner.closures.createClosureStopped(0, 3, []machineCaptureDescriptor{{
		mode: machineCaptureByValue, value: caseValue,
	}})
	if err != nil {
		tb.Fatal(err)
	}
	return batch
}

func machineExactGuestBatchArgs(tb testing.TB, owner *machineOwner, count, seed float64) []slot {
	tb.Helper()
	countValue, err := owner.importValueStopped(NumberValue(count))
	if err != nil {
		tb.Fatal(err)
	}
	seedValue, err := owner.importValueStopped(NumberValue(seed))
	if err != nil {
		tb.Fatal(err)
	}
	return []slot{countValue, seedValue}
}

func backendExactRecursiveGuestBatchFiles(t *testing.T) ([]backendGoNumericProgramFile, string) {
	t.Helper()
	source := backendExactRecursiveGuestBatchSource(t)
	irs, image := backendExactCorpusIRs(t, source)
	files, err := emitBackendGoNumericProgram(irs, backendGoNumericProgramOptions{
		packageName:          "ember",
		functionPrefix:       "backendGeneratedExactRecursiveGuestBatch",
		preparedFunctionName: "backendGeneratedExactRecursiveGuestBatchPrepared",
		entryProto:           3,
		coroutineDeadString:  backendCoroutineDeadStringID(t, image),
	})
	if err != nil {
		t.Fatal(err)
	}
	return files, source
}

func backendExactRecursiveGuestBatchSource(tb testing.TB) string {
	tb.Helper()
	tc := loadLuauBenchmarkCases(tb, "classicLuauCases", []string{"recursive_fibonacci"})[0]
	fixture, err := parityfixture.BuildGuestBatch(tc.source, parityfixture.GuestBatchVariant{
		CaseName: "__case", BatchName: "__batch",
	})
	if err != nil {
		tb.Fatal(err)
	}
	return fixture.Program + "return __batch\n"
}
