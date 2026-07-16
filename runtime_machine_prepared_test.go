package ember

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestMachinePreparedBindingRunsGeneratedNumericFunctionAndReplaysFailedGuard(t *testing.T) {
	image := machinePreparedTestImage(t)
	calls := 0
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		return backendGeneratedNumericPreparedFixture(context)
	})
	owner, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
	}()

	number, err := owner.importValueStopped(NumberValue(7))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, owner, 1, []slot{number}, nil)
	assertMachineOwnerNumberResult(t, owner, backendNumericProofExpected(7))
	if calls != 1 {
		t.Fatalf("prepared calls = %d, want 1", calls)
	}

	textID, err := owner.strings.internStringStopped("7")
	if err != nil {
		t.Fatal(err)
	}
	text, err := slotPackHandle(slotTagString, uint32(textID), 1)
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, owner, 1, []slot{text}, nil)
	assertMachineOwnerNumberResult(t, owner, backendNumericProofExpected(7))
	if calls != 2 {
		t.Fatalf("prepared guard calls = %d, want 2", calls)
	}

	if !checkptrInstrumentedTest() {
		lease, err := owner.beginRun()
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		allocations := testing.AllocsPerRun(1000, func() {
			runErr = owner.executeStopped(0, 1, machineClosureHandle{}, []slot{number}, nil, machineRunEffects{})
		})
		lease.end()
		if runErr != nil {
			t.Fatal(runErr)
		}
		if allocations != 0 {
			t.Fatalf("prepared stopped-call allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedControlledExecutionStaysGenericAndChargesExactly(t *testing.T) {
	image := machinePreparedTestImage(t)
	calls := 0
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		return backendGeneratedNumericPreparedFixture(context)
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	preparedArg, err := prepared.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 10000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 10000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 0 {
		t.Fatalf("prepared function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf("controlled remaining = %d, generic %d", preparedController.remaining, genericController.remaining)
	}
	assertMachineOwnerNumberResult(t, prepared, backendNumericProofExpected(29))
}

func TestMachinePreparedBindingRejectsMismatchBeforeExecutionAndCopiesInventory(t *testing.T) {
	image := machinePreparedTestImage(t)
	calls := 0
	function := func(context machinePreparedContext) machinePreparedExit {
		calls++
		return backendGeneratedNumericPreparedFixture(context)
	}
	program := machinePreparedTestProgram(t, image, 0, 1, function)

	badHash := *program
	badHash.programHash[0] ^= 0xff
	if owner, err := newMachineOwnerWithPrepared(image, &badHash); err == nil || owner != nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("bind bad hash = (%v, %v)", owner, err)
	}
	badVersion := *program
	badVersion.abiVersion++
	if owner, err := newMachineOwnerWithPrepared(image, &badVersion); err == nil || owner != nil || !strings.Contains(err.Error(), "ABI version") {
		t.Fatalf("bind bad ABI = (%v, %v)", owner, err)
	}
	if calls != 0 {
		t.Fatalf("mismatched binding executed generated code %d times", calls)
	}

	owner, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	program.modules[0].functions[1] = func(machinePreparedContext) machinePreparedExit {
		return machinePreparedReturnOneNumber(-1)
	}
	arg, err := owner.importValueStopped(NumberValue(1))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, owner, 1, []slot{arg}, nil)
	assertMachineOwnerNumberResult(t, owner, backendNumericProofExpected(1))
	if calls != 1 {
		t.Fatalf("copied prepared binding calls = %d, want 1", calls)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

func TestMachinePreparedExactNonEntryReplayMatchesGeneric(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendNumericExitProofSource)
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		observed = backendGeneratedNumericExitPreparedFixture(context)
		return observed
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	preparedArg, err := prepared.importValueStopped(NumberValue(math.NaN()))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(math.NaN()))
	if err != nil {
		t.Fatal(err)
	}
	preparedErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if observed.kind != machinePreparedExitReplayBeforeOperation ||
		observed.pc != 2 ||
		observed.spillCount != 1 {
		t.Fatalf("prepared replay exit = %#v, want PC 2 with one spill", observed)
	}
	if preparedErr == nil || genericErr == nil || preparedErr.Error() != genericErr.Error() {
		t.Fatalf("prepared/generic replay errors = %v / %v", preparedErr, genericErr)
	}
	if prepared.restartPC != 0 {
		t.Fatalf("prepared restart PC after replay = %d, want consumed by generic loop", prepared.restartPC)
	}
	spilled, err := prepared.number(prepared.registers[1])
	if err != nil || !math.IsNaN(spilled) {
		t.Fatalf("prepared spilled register = (%v, %v), want NaN", spilled, err)
	}
}

func TestMachinePreparedDirectCallRunsWithoutClosureAndReplaysCalleeGuard(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendNumericCallProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedNumericCallPreparedFixture(context)
		return observed
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	preparedArg, err := prepared.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, 93)
	assertMachineOwnerNumberResult(t, generic, 93)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared direct-call success = calls %d exit %#v", calls, observed)
	}

	preparedNaN, err := prepared.importValueStopped(NumberValue(math.NaN()))
	if err != nil {
		t.Fatal(err)
	}
	genericNaN, err := generic.importValueStopped(NumberValue(math.NaN()))
	if err != nil {
		t.Fatal(err)
	}
	preparedErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedNaN}, nil)
	genericErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericNaN}, nil)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared direct-call guard = calls %d exit %#v", calls, observed)
	}
	if preparedErr == nil || genericErr == nil || preparedErr.Error() != genericErr.Error() {
		t.Fatalf("prepared/generic direct-call replay errors = %v / %v", preparedErr, genericErr)
	}

	if !checkptrInstrumentedTest() {
		lease, err := prepared.beginRun()
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		allocations := testing.AllocsPerRun(1000, func() {
			runErr = prepared.executeStopped(0, 1, machineClosureHandle{}, []slot{preparedArg}, nil, machineRunEffects{})
		})
		lease.end()
		if runErr != nil {
			t.Fatal(runErr)
		}
		if allocations != 0 {
			t.Fatalf("prepared direct-call owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedScalarTableFieldsAvoidAllocationAndReplayEntry(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendTableFieldProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedTableFieldPreparedFixture(context)
		return observed
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	preparedArg, err := prepared.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, 1861)
	assertMachineOwnerNumberResult(t, generic, 1861)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared scalar table-field success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != 0 {
		t.Fatalf("prepared scalar table-field path materialized %d Machine tables", len(prepared.tables.tables))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions:   10_000,
		MaxRuntimeObjects: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions:   10_000,
		MaxRuntimeObjects: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	preparedLimitErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, preparedController)
	genericLimitErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared scalar table-field function ran under execution policy %d times", calls)
	}
	if preparedLimitErr == nil || genericLimitErr == nil || preparedLimitErr.Error() != genericLimitErr.Error() {
		t.Fatalf("prepared/generic scalar table-field limit errors = %v / %v", preparedLimitErr, genericLimitErr)
	}

	preparedStringID, err := prepared.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	preparedString, err := slotPackHandle(slotTagString, uint32(preparedStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	genericStringID, err := generic.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	genericString, err := slotPackHandle(slotTagString, uint32(genericStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedString}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericString}, nil)
	assertMachineOwnerNumberResult(t, prepared, 1861)
	assertMachineOwnerNumberResult(t, generic, 1861)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar table-field fallback = calls %d exit %#v", calls, observed)
	}

	if !checkptrInstrumentedTest() {
		lease, err := prepared.beginRun()
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		allocations := testing.AllocsPerRun(1000, func() {
			runErr = prepared.executeStopped(0, 1, machineClosureHandle{}, []slot{preparedArg}, nil, machineRunEffects{})
		})
		lease.end()
		if runErr != nil {
			t.Fatal(runErr)
		}
		if allocations != 0 {
			t.Fatalf("prepared scalar table-field owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedScalarArrayIterationAvoidsTablesAndReplaysEntry(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendArrayIterationProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedArrayIterationPreparedFixture(context)
		return observed
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	preparedArg, err := prepared.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, 228)
	assertMachineOwnerNumberResult(t, generic, 228)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared scalar array-iteration success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != 0 {
		t.Fatalf("prepared scalar array-iteration path materialized %d Machine tables", len(prepared.tables.tables))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 8})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 8})
	if err != nil {
		t.Fatal(err)
	}
	preparedLimitErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, preparedController)
	genericLimitErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared scalar array-iteration function ran under execution policy %d times", calls)
	}
	if preparedLimitErr == nil || genericLimitErr == nil || preparedLimitErr.Error() != genericLimitErr.Error() {
		t.Fatalf("prepared/generic scalar array-iteration limit errors = %v / %v", preparedLimitErr, genericLimitErr)
	}

	preparedStringID, err := prepared.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	preparedString, err := slotPackHandle(slotTagString, uint32(preparedStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	genericStringID, err := generic.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	genericString, err := slotPackHandle(slotTagString, uint32(genericStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedString}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericString}, nil)
	assertMachineOwnerNumberResult(t, prepared, 228)
	assertMachineOwnerNumberResult(t, generic, 228)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar array-iteration fallback = calls %d exit %#v", calls, observed)
	}

	if !checkptrInstrumentedTest() {
		lease, err := prepared.beginRun()
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		allocations := testing.AllocsPerRun(1000, func() {
			runErr = prepared.executeStopped(0, 1, machineClosureHandle{}, []slot{preparedArg}, nil, machineRunEffects{})
		})
		lease.end()
		if runErr != nil {
			t.Fatal(runErr)
		}
		if allocations != 0 {
			t.Fatalf("prepared scalar array-iteration owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedScalarArrayOpsAvoidLocalTablesAndGuardIntrinsics(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendArrayOpsProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedArrayOpsPreparedFixture(context)
		return observed
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	baseTables := len(prepared.tables.tables)
	preparedArg, err := prepared.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, backendArrayOpsExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendArrayOpsExpected(29))
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared scalar array-ops success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != baseTables {
		t.Fatalf("prepared scalar array-ops path materialized %d local Machine tables", len(prepared.tables.tables)-baseTables)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions:         10_000,
		MaxTableEntriesPerTable: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions:         10_000,
		MaxTableEntriesPerTable: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	preparedLimitErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, preparedController)
	genericLimitErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared scalar array-ops function ran under execution policy %d times", calls)
	}
	if preparedLimitErr == nil || genericLimitErr == nil || preparedLimitErr.Error() != genericLimitErr.Error() {
		t.Fatalf("prepared/generic scalar array-ops limit errors = %v / %v", preparedLimitErr, genericLimitErr)
	}

	preparedStringID, err := prepared.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	preparedString, err := slotPackHandle(slotTagString, uint32(preparedStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	genericStringID, err := generic.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	genericString, err := slotPackHandle(slotTagString, uint32(genericStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedString}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericString}, nil)
	assertMachineOwnerNumberResult(t, prepared, backendArrayOpsExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendArrayOpsExpected(29))
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar array-ops parameter fallback = calls %d exit %#v", calls, observed)
	}

	override := NewTable()
	if err := override.Set(StringValue("insert"), HostFuncValue(func([]Value) ([]Value, error) {
		return nil, errors.New("array insert override")
	})); err != nil {
		t.Fatal(err)
	}
	globals := map[string]Value{"table": TableValue(override)}
	if err := prepared.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	preparedOverrideErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericOverrideErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != 3 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar array-ops intrinsic fallback = calls %d exit %#v", calls, observed)
	}
	if preparedOverrideErr == nil || genericOverrideErr == nil ||
		preparedOverrideErr.Error() != genericOverrideErr.Error() ||
		!strings.Contains(preparedOverrideErr.Error(), "array insert override") {
		t.Fatalf("prepared/generic scalar array-ops override errors = %v / %v", preparedOverrideErr, genericOverrideErr)
	}
	if err := prepared.importGlobalsStopped(nil); err != nil {
		t.Fatal(err)
	}

	if !checkptrInstrumentedTest() {
		lease, err := prepared.beginRun()
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		allocations := testing.AllocsPerRun(1000, func() {
			runErr = prepared.executeStopped(0, 1, machineClosureHandle{}, []slot{preparedArg}, nil, machineRunEffects{})
		})
		lease.end()
		if runErr != nil {
			t.Fatal(runErr)
		}
		if allocations != 0 {
			t.Fatalf("prepared scalar array-ops owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedScalarClosureAvoidsClosureAndCellMaterialization(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendClosureProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedClosurePreparedFixture(context)
		return observed
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	baseClosures := len(prepared.closures.closures)
	baseCells := len(prepared.closures.cells)
	preparedArg, err := prepared.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, backendClosureExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendClosureExpected(29))
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared scalar closure success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.closures.closures) != baseClosures ||
		len(prepared.closures.cells) != baseCells {
		t.Fatalf(
			"prepared scalar closure materialized closures/cells = %d/%d, want %d/%d",
			len(prepared.closures.closures),
			len(prepared.closures.cells),
			baseClosures,
			baseCells,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared scalar closure function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf("controlled scalar closure remaining = %d, generic %d", preparedController.remaining, genericController.remaining)
	}

	preparedStringID, err := prepared.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	preparedString, err := slotPackHandle(slotTagString, uint32(preparedStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	genericStringID, err := generic.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	genericString, err := slotPackHandle(slotTagString, uint32(genericStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedString}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericString}, nil)
	assertMachineOwnerNumberResult(t, prepared, backendClosureExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendClosureExpected(29))
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar closure parameter fallback = calls %d exit %#v", calls, observed)
	}

	if !checkptrInstrumentedTest() {
		lease, err := prepared.beginRun()
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		allocations := testing.AllocsPerRun(1000, func() {
			runErr = prepared.executeStopped(0, 1, machineClosureHandle{}, []slot{preparedArg}, nil, machineRunEffects{})
		})
		lease.end()
		if runErr != nil {
			t.Fatal(runErr)
		}
		if allocations != 0 {
			t.Fatalf("prepared scalar closure owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedRecursiveCallAvoidsSelfClosureMaterialization(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendRecursiveProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedRecursivePreparedFixture(context)
		return observed
	})
	prepared, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(); err != nil {
			t.Fatal(err)
		}
		if err := generic.close(); err != nil {
			t.Fatal(err)
		}
	}()

	baseClosures := len(prepared.closures.closures)
	baseCells := len(prepared.closures.cells)
	preparedArg, err := prepared.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	genericArg, err := generic.importValueStopped(NumberValue(29))
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, backendRecursiveExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendRecursiveExpected(29))
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared recursive success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.closures.closures) != baseClosures ||
		len(prepared.closures.cells) != baseCells {
		t.Fatalf(
			"prepared recursion materialized closures/cells = %d/%d, want %d/%d",
			len(prepared.closures.closures),
			len(prepared.closures.cells),
			baseClosures,
			baseCells,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared recursive function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf("controlled recursive remaining = %d, generic %d", preparedController.remaining, genericController.remaining)
	}

	preparedStringID, err := prepared.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	preparedString, err := slotPackHandle(slotTagString, uint32(preparedStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	genericStringID, err := generic.strings.internStringStopped("29")
	if err != nil {
		t.Fatal(err)
	}
	genericString, err := slotPackHandle(slotTagString, uint32(genericStringID), 1)
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedString}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericString}, nil)
	assertMachineOwnerNumberResult(t, prepared, backendRecursiveExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendRecursiveExpected(29))
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared recursive parameter fallback = calls %d exit %#v", calls, observed)
	}

	if !checkptrInstrumentedTest() {
		lease, err := prepared.beginRun()
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		allocations := testing.AllocsPerRun(1000, func() {
			runErr = prepared.executeStopped(0, 1, machineClosureHandle{}, []slot{preparedArg}, nil, machineRunEffects{})
		})
		lease.end()
		if runErr != nil {
			t.Fatal(runErr)
		}
		if allocations != 0 {
			t.Fatalf("prepared recursive owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedRejectsMalformedReplayBeforeCanonicalMutation(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendNumericExitProofSource)
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		exit := context.replayBeforeOperation(2, 1)
		context.spillNumber(0, 99, math.NaN())
		return exit
	})
	owner, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
	}()
	arg, err := owner.importValueStopped(NumberValue(1))
	if err != nil {
		t.Fatal(err)
	}
	runErr := runMachinePreparedTestProtoError(t, owner, 1, []slot{arg}, nil)
	if runErr == nil || !strings.Contains(runErr.Error(), "invalid register 99") {
		t.Fatalf("malformed prepared replay = %v", runErr)
	}
	if owner.registers[1] != slotNil {
		t.Fatalf("malformed replay mutated canonical register 1 to %#x", owner.registers[1])
	}
}

func machinePreparedTestProgram(
	t *testing.T,
	image *programImage,
	moduleID programModuleID,
	protoID int32,
	function machinePreparedFunction,
) *machinePreparedProgram {
	t.Helper()
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	modules := make([]machinePreparedModule, len(ir.modules))
	for index := range ir.modules {
		modules[index] = machinePreparedModule{
			moduleID:  programModuleID(index),
			functions: make([]machinePreparedFunction, len(ir.modules[index].protos)),
		}
	}
	modules[moduleID].functions[protoID] = function
	return &machinePreparedProgram{
		abiVersion:      ir.abiVersion,
		semanticVersion: ir.semanticVersion,
		programHash:     ir.programHash,
		modules:         modules,
	}
}

func machinePreparedTestImage(t *testing.T) *programImage {
	return machinePreparedTestImageForSource(t, backendNumericProofSource)
}

func machinePreparedTestImageForSource(t *testing.T, source string) *programImage {
	t.Helper()
	image := machineOwnerProgramImage(t, []string{source})
	image.entrypoints = []programImageEntrypoint{{name: "main", moduleID: 0}}
	globalNames, err := programImageGlobalNames(image.modules)
	if err != nil {
		t.Fatal(err)
	}
	image.globalNames = globalNames
	return image
}

func runMachinePreparedTestProto(
	t *testing.T,
	owner *machineOwner,
	protoID int32,
	args []slot,
	controller *executionController,
) {
	t.Helper()
	lease, err := owner.beginRun()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.end()
	if err := owner.executeStopped(0, protoID, machineClosureHandle{}, args, controller, machineRunEffects{}); err != nil {
		t.Fatal(err)
	}
}

func runMachinePreparedTestProtoError(
	t *testing.T,
	owner *machineOwner,
	protoID int32,
	args []slot,
	controller *executionController,
) error {
	t.Helper()
	lease, err := owner.beginRun()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.end()
	return owner.executeStopped(0, protoID, machineClosureHandle{}, args, controller, machineRunEffects{})
}

func backendNumericProofExpected(seed float64) float64 {
	total := seed
	for index := 1.0; index <= 64; index++ {
		if int(index)%2 == 0 {
			total += index * seed
		} else {
			total--
		}
	}
	return total
}

func BenchmarkMachinePreparedNumericOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendNumericProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedNumericPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericNumericOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendNumericProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedNumericDirectCallOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendNumericCallProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedNumericCallPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericNumericDirectCallOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendNumericCallProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedScalarTableFieldOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendTableFieldProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedTableFieldPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericScalarTableFieldOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendTableFieldProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedScalarArrayIterationOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendArrayIterationProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedArrayIterationPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericScalarArrayIterationOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendArrayIterationProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedScalarArrayOpsOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendArrayOpsProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedArrayOpsPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericScalarArrayOpsOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendArrayOpsProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedScalarClosureOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendClosureProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedClosurePreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericScalarClosureOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendClosureProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedRecursiveOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendRecursiveProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedRecursivePreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericRecursiveOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendRecursiveProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func benchmarkMachineNumericOwner(b *testing.B, image *programImage, program *machinePreparedProgram) {
	b.Helper()
	owner, err := newMachineOwnerWithPrepared(image, program)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := owner.close(); err != nil {
			b.Fatal(err)
		}
	})
	arg, err := owner.importValueStopped(NumberValue(29))
	if err != nil {
		b.Fatal(err)
	}
	args := []slot{arg}
	lease, err := owner.beginRun()
	if err != nil {
		b.Fatal(err)
	}
	defer lease.end()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := owner.executeStopped(0, 1, machineClosureHandle{}, args, nil, machineRunEffects{}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	value, err := owner.number(owner.results[0])
	if err != nil {
		b.Fatal(err)
	}
	backendGeneratedNumericSink = value
}

func machinePreparedBenchmarkImage(tb testing.TB, source string) *programImage {
	tb.Helper()
	proto, err := Compile(source)
	if err != nil {
		tb.Fatal(err)
	}
	code, err := proto.preparedCodeImage()
	if err != nil {
		tb.Fatal(err)
	}
	image := &programImage{
		modules: []programImageModule{{
			moduleID: 0,
			key:      moduleKey{kind: moduleKeyLogical, path: "benchmark"},
			code:     code,
		}},
		entrypoints: []programImageEntrypoint{{name: "main", moduleID: 0}},
	}
	image.globalNames, err = programImageGlobalNames(image.modules)
	if err != nil {
		tb.Fatal(err)
	}
	return image
}

func machinePreparedBenchmarkProgram(
	tb testing.TB,
	image *programImage,
	function machinePreparedFunction,
) *machinePreparedProgram {
	tb.Helper()
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		tb.Fatal(err)
	}
	functions := make([]machinePreparedFunction, len(ir.modules[0].protos))
	functions[1] = function
	return &machinePreparedProgram{
		abiVersion:      ir.abiVersion,
		semanticVersion: ir.semanticVersion,
		programHash:     ir.programHash,
		modules: []machinePreparedModule{{
			moduleID:  0,
			functions: functions,
		}},
	}
}
