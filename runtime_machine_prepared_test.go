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

func TestMachinePreparedScalarMetatableIndexAvoidsTablesAndReplaysEntry(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendMetatableIndexProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedMetatableIndexPreparedFixture(context)
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
	assertMachineOwnerNumberResult(t, prepared, 1170)
	assertMachineOwnerNumberResult(t, generic, 1170)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared scalar metatable success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != 0 {
		t.Fatalf("prepared scalar metatable path materialized %d Machine tables", len(prepared.tables.tables))
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
		t.Fatalf("prepared scalar metatable function ran under execution policy %d times", calls)
	}
	if preparedLimitErr == nil || genericLimitErr == nil || preparedLimitErr.Error() != genericLimitErr.Error() {
		t.Fatalf("prepared/generic scalar metatable limit errors = %v / %v", preparedLimitErr, genericLimitErr)
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
	assertMachineOwnerNumberResult(t, prepared, 1170)
	assertMachineOwnerNumberResult(t, generic, 1170)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar metatable parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared scalar metatable owner allocations = %v, want 0", allocations)
		}
	}

	operation := &image.modules[0].code.prototypes[1].operations[14]
	if operation.op != opFastCall || nativeFuncID(operation.nativeID) != nativeFuncSetMetatable {
		t.Fatalf("metatable proof PC 14 = %s native %d, want setmetatable FAST_CALL", opcodeName(operation.op), operation.nativeID)
	}
	for _, owner := range []*machineOwner{prepared, generic} {
		dense, err := owner.globalIndexStopped(0, operation.globalIndex)
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.globals.setAt(dense, slotBool(false)); err != nil {
			t.Fatal(err)
		}
	}
	callsBeforeRebind := calls
	preparedIntrinsicErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericIntrinsicErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != callsBeforeRebind+1 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared rebound setmetatable fallback = calls %d exit %#v", calls, observed)
	}
	if preparedIntrinsicErr == nil || genericIntrinsicErr == nil ||
		preparedIntrinsicErr.Error() != genericIntrinsicErr.Error() {
		t.Fatalf("prepared/generic rebound setmetatable errors = %v / %v", preparedIntrinsicErr, genericIntrinsicErr)
	}
}

func TestMachinePreparedScalarMethodAvoidsTablesClosuresAndReplaysEntry(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendMethodProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedMethodPreparedFixture(context)
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
	baseClosures := len(prepared.closures.closures)
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
	assertMachineOwnerNumberResult(t, prepared, 5040)
	assertMachineOwnerNumberResult(t, generic, 5040)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared scalar method success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != baseTables ||
		len(prepared.closures.closures) != baseClosures {
		t.Fatalf(
			"prepared scalar method materialized tables/closures = %d/%d, want %d/%d",
			len(prepared.tables.tables),
			len(prepared.closures.closures),
			baseTables,
			baseClosures,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions:   10,
		MaxRuntimeObjects: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions:   10,
		MaxRuntimeObjects: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	preparedLimitErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, preparedController)
	genericLimitErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared scalar method function ran under execution policy %d times", calls)
	}
	if preparedLimitErr == nil || genericLimitErr == nil || preparedLimitErr.Error() != genericLimitErr.Error() {
		t.Fatalf("prepared/generic scalar method limit errors = %v / %v", preparedLimitErr, genericLimitErr)
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
	assertMachineOwnerNumberResult(t, prepared, 5040)
	assertMachineOwnerNumberResult(t, generic, 5040)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar method fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared scalar method owner allocations = %v, want 0", allocations)
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

func TestMachinePreparedFiniteStringStateAvoidsRuntimeStringsAndReplaysEntry(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendFiniteStringStateProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedFiniteStringStatePreparedFixture(context)
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
	want, ok := backendGeneratedFiniteStringStateFixture(29)
	if !ok {
		t.Fatal("direct finite-string state fixture exited")
	}
	stringCount := len(prepared.strings.records)
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared finite-string state success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != 0 {
		t.Fatalf("prepared finite-string state path materialized %d Machine tables", len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared finite-string state path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
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
		t.Fatalf("prepared finite-string state function ran under execution policy %d times", calls)
	}
	if preparedLimitErr == nil || genericLimitErr == nil || preparedLimitErr.Error() != genericLimitErr.Error() {
		t.Fatalf("prepared/generic finite-string state limit errors = %v / %v", preparedLimitErr, genericLimitErr)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared finite-string state fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared finite-string state owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedStructuralStringKeysAvoidRuntimeStringsAndGuardToString(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendStructuralStringKeyProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedStructuralStringKeyPreparedFixture(context)
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
	stringCount := len(prepared.strings.records)
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, 30)
	assertMachineOwnerNumberResult(t, generic, 30)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared structural string-key success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared structural string-key path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
	}

	override := map[string]Value{
		"tostring": HostFuncValue(func([]Value) ([]Value, error) {
			return nil, errors.New("rebound tostring")
		}),
	}
	if err := prepared.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	preparedErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared structural string-key guard fallback = calls %d exit %#v", calls, observed)
	}
	if preparedErr == nil || genericErr == nil ||
		preparedErr.Error() != genericErr.Error() ||
		!strings.Contains(preparedErr.Error(), "rebound tostring") {
		t.Fatalf("prepared/generic structural string-key override errors = %v / %v", preparedErr, genericErr)
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
			t.Fatalf("prepared structural string-key owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedSparseGridAvoidsRuntimeTablesAndStrings(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendSparseGridProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedSparseGridPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedSparseGrid(29)
	if !ok {
		t.Fatal("generated sparse-grid oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared sparse-grid success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf(
			"prepared sparse-grid path changed owner table count from %d to %d",
			tableCount,
			len(prepared.tables.tables),
		)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared sparse-grid path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
	}

	override := map[string]Value{
		"tostring": HostFuncValue(func([]Value) ([]Value, error) {
			return nil, errors.New("rebound tostring")
		}),
	}
	if err := prepared.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	preparedErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared sparse-grid guard fallback = calls %d exit %#v", calls, observed)
	}
	if preparedErr == nil || genericErr == nil ||
		preparedErr.Error() != genericErr.Error() ||
		!strings.Contains(preparedErr.Error(), "rebound tostring") {
		t.Fatalf("prepared/generic sparse-grid override errors = %v / %v", preparedErr, genericErr)
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
			t.Fatalf("prepared sparse-grid owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedProjectileSweepAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendProjectileSweepProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedProjectileSweepPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedProjectileSweep(29)
	if !ok {
		t.Fatal("generated projectile-sweep oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared projectile-sweep success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf(
			"prepared projectile-sweep path changed owner table count from %d to %d",
			tableCount,
			len(prepared.tables.tables),
		)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared projectile-sweep path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
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
			t.Fatalf("prepared projectile-sweep owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedCombatTickGuardsMathMinBeforeMutation(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendCombatTickProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedCombatTickPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedCombatTick(29)
	if !ok {
		t.Fatal("generated combat-tick oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared combat-tick success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf(
			"prepared combat-tick path changed owner table count from %d to %d",
			tableCount,
			len(prepared.tables.tables),
		)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared combat-tick path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
	}

	mathOverride := NewTable()
	if err := mathOverride.Set(StringValue("min"), HostFuncValue(func([]Value) ([]Value, error) {
		return []Value{NumberValue(99)}, nil
	})); err != nil {
		t.Fatal(err)
	}
	override := map[string]Value{"math": TableValue(mathOverride)}
	if err := prepared.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	preparedResult, err := prepared.number(prepared.results[0])
	if err != nil {
		t.Fatal(err)
	}
	genericResult, err := generic.number(generic.results[0])
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared combat-tick guard fallback = calls %d exit %#v", calls, observed)
	}
	if preparedResult != genericResult || preparedResult == want {
		t.Fatalf(
			"prepared/generic rebound math.min results = %v/%v, canonical %v",
			preparedResult,
			genericResult,
			want,
		)
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
			t.Fatalf("prepared combat-tick owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedAbilityResolutionAvoidsRuntimeTablesAndGuardsMathMin(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendAbilityResolutionProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedAbilityResolutionPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedAbilityResolution(29)
	if !ok {
		t.Fatal("generated ability-resolution oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared ability-resolution success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf(
			"prepared ability-resolution path changed owner table count from %d to %d",
			tableCount,
			len(prepared.tables.tables),
		)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared ability-resolution path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
	}

	mathOverride := NewTable()
	if err := mathOverride.Set(StringValue("min"), HostFuncValue(func([]Value) ([]Value, error) {
		return []Value{NumberValue(99)}, nil
	})); err != nil {
		t.Fatal(err)
	}
	override := map[string]Value{"math": TableValue(mathOverride)}
	if err := prepared.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	preparedResult, err := prepared.number(prepared.results[0])
	if err != nil {
		t.Fatal(err)
	}
	genericResult, err := generic.number(generic.results[0])
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared ability-resolution guard fallback = calls %d exit %#v", calls, observed)
	}
	if preparedResult != genericResult || preparedResult == want {
		t.Fatalf(
			"prepared/generic rebound math.min results = %v/%v, canonical %v",
			preparedResult,
			genericResult,
			want,
		)
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
			t.Fatalf("prepared ability-resolution owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedAIUtilityScoringAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendAIUtilityScoringProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedAIUtilityScoringPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedAIUtilityScoring(29)
	if !ok {
		t.Fatal("generated AI utility-scoring oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared AI utility-scoring success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf(
			"prepared AI utility-scoring path changed owner table count from %d to %d",
			tableCount,
			len(prepared.tables.tables),
		)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared AI utility-scoring path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared AI utility-scoring function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled AI utility-scoring remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared AI utility-scoring parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared AI utility-scoring owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedCooldownSchedulerAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendCooldownSchedulerProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedCooldownSchedulerPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedCooldownScheduler(29)
	if !ok {
		t.Fatal("generated cooldown-scheduler oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared cooldown-scheduler success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf(
			"prepared cooldown-scheduler path changed owner table count from %d to %d",
			tableCount,
			len(prepared.tables.tables),
		)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared cooldown-scheduler path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared cooldown-scheduler function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled cooldown-scheduler remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared cooldown-scheduler parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared cooldown-scheduler owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedSaveStateDiffAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendSaveStateDiffProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedSaveStateDiffPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedSaveStateDiff(29)
	if !ok {
		t.Fatal("generated save-state oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared save-state success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared save-state path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared save-state path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared save-state function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf("controlled save-state remaining = %d, generic %d", preparedController.remaining, genericController.remaining)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared save-state parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared save-state owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedThreatAggroAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendThreatAggroProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedThreatAggroPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedThreatAggro(29)
	if !ok {
		t.Fatal("generated threat-aggro oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared threat-aggro success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared threat-aggro path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared threat-aggro path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared threat-aggro function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled threat-aggro remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared threat-aggro parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared threat-aggro owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedDialogueConditionAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendDialogueConditionProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedDialogueConditionPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedDialogueCondition(29)
	if !ok {
		t.Fatal("generated dialogue-condition oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared dialogue-condition success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared dialogue-condition path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared dialogue-condition path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared dialogue-condition function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled dialogue-condition remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared dialogue-condition parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared dialogue-condition owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedComponentChurnAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendComponentChurnProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedComponentChurnPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedComponentChurn(29)
	if !ok {
		t.Fatal("generated component-churn oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared component-churn success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared component-churn path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared component-churn path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared component-churn function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled component-churn remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared component-churn parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared component-churn owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedBuffStackAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendBuffStackProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedBuffStackPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedBuffStack(29)
	if !ok {
		t.Fatal("generated buff-stack oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared buff-stack success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared buff-stack path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared buff-stack path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared buff-stack function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled buff-stack remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared buff-stack parameter fallback = calls %d exit %#v", calls, observed)
	}

	override := baseTable()
	if err := override.Set(StringValue("remove"), HostFuncValue(func([]Value) ([]Value, error) {
		return nil, errors.New("buff remove override")
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
		t.Fatalf("prepared buff-stack intrinsic fallback = calls %d exit %#v", calls, observed)
	}
	if preparedOverrideErr == nil || genericOverrideErr == nil ||
		preparedOverrideErr.Error() != genericOverrideErr.Error() ||
		!strings.Contains(preparedOverrideErr.Error(), "buff remove override") {
		t.Fatalf("prepared/generic buff-stack override errors = %v / %v", preparedOverrideErr, genericOverrideErr)
	}
	if err := prepared.importGlobalsStopped(nil); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(nil); err != nil {
		t.Fatal(err)
	}
	globals = map[string]Value{
		"rawlen": HostFuncValue(func([]Value) ([]Value, error) {
			return nil, errors.New("buff rawlen override")
		}),
	}
	if err := prepared.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	preparedOverrideErr = runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericOverrideErr = runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != 4 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared buff-stack rawlen fallback = calls %d exit %#v", calls, observed)
	}
	if preparedOverrideErr == nil || genericOverrideErr == nil ||
		preparedOverrideErr.Error() != genericOverrideErr.Error() ||
		!strings.Contains(preparedOverrideErr.Error(), "buff rawlen override") {
		t.Fatalf("prepared/generic buff-stack rawlen errors = %v / %v", preparedOverrideErr, genericOverrideErr)
	}
	if err := prepared.importGlobalsStopped(nil); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(nil); err != nil {
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
			t.Fatalf("prepared buff-stack owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedStateMachineAvoidsRuntimeTablesAndStrings(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendStateMachineProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedStateMachinePreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedStateMachine(29)
	if !ok {
		t.Fatal("generated state-machine oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared state-machine success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared state-machine path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared state-machine path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared state-machine function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled state-machine remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared state-machine parameter fallback = calls %d exit %#v", calls, observed)
	}

	globals := map[string]Value{
		"rawlen": HostFuncValue(func([]Value) ([]Value, error) {
			return nil, errors.New("state-machine rawlen override")
		}),
	}
	if err := prepared.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	preparedOverrideErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericOverrideErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != 3 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared state-machine intrinsic fallback = calls %d exit %#v", calls, observed)
	}
	if preparedOverrideErr == nil || genericOverrideErr == nil ||
		preparedOverrideErr.Error() != genericOverrideErr.Error() ||
		!strings.Contains(preparedOverrideErr.Error(), "state-machine rawlen override") {
		t.Fatalf("prepared/generic state-machine override errors = %v / %v", preparedOverrideErr, genericOverrideErr)
	}
	if err := prepared.importGlobalsStopped(nil); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(nil); err != nil {
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
			t.Fatalf("prepared state-machine owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedCommandRouterOwnsCapturedStateAndReplaysExactly(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendCommandRouterProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedCommandRouterPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedCommandRouter(29)
	if !ok {
		t.Fatal("generated command-router oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared command-router success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount || len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared command-router materialized owner objects: tables %d/%d strings %d/%d",
			len(prepared.tables.tables), tableCount, len(prepared.strings.records), stringCount,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 || preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled command-router fallback = calls %d remaining %d/%d",
			calls, preparedController.remaining, genericController.remaining,
		)
	}

	globals := map[string]Value{
		"select": HostFuncValue(func([]Value) ([]Value, error) {
			return nil, errors.New("command-router select override")
		}),
	}
	if err := prepared.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	preparedOverrideErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericOverrideErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared command-router intrinsic fallback = calls %d exit %#v", calls, observed)
	}
	if preparedOverrideErr == nil || genericOverrideErr == nil ||
		preparedOverrideErr.Error() != genericOverrideErr.Error() ||
		!strings.Contains(preparedOverrideErr.Error(), "command-router select override") {
		t.Fatalf("prepared/generic command-router override errors = %v / %v", preparedOverrideErr, genericOverrideErr)
	}
	if err := prepared.importGlobalsStopped(nil); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(nil); err != nil {
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
			t.Fatalf("prepared command-router owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedEventDispatchOwnsFiniteCallSetAndReplaysExactly(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendEventDispatchProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedEventDispatchPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedEventDispatch(29)
	if !ok {
		t.Fatal("generated event-dispatch oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared event-dispatch success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount || len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared event-dispatch materialized owner objects: tables %d/%d strings %d/%d",
			len(prepared.tables.tables), tableCount, len(prepared.strings.records), stringCount,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 || preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled event-dispatch fallback = calls %d remaining %d/%d",
			calls, preparedController.remaining, genericController.remaining,
		)
	}

	mathOverride := NewTable()
	if err := mathOverride.Set(StringValue("min"), HostFuncValue(func([]Value) ([]Value, error) {
		return []Value{NumberValue(99)}, nil
	})); err != nil {
		t.Fatal(err)
	}
	override := map[string]Value{"math": TableValue(mathOverride)}
	if err := prepared.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(override); err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	preparedResult, err := prepared.number(prepared.results[0])
	if err != nil {
		t.Fatal(err)
	}
	genericResult, err := generic.number(generic.results[0])
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared event-dispatch intrinsic fallback = calls %d exit %#v", calls, observed)
	}
	if preparedResult != genericResult || preparedResult == want {
		t.Fatalf(
			"prepared/generic rebound event-dispatch results = %v/%v, canonical %v",
			preparedResult, genericResult, want,
		)
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
			t.Fatalf("prepared event-dispatch owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedPathRelaxationAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendPathRelaxationProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedPathRelaxationPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedPathRelaxation(29)
	if !ok {
		t.Fatal("generated path-relaxation oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared path-relaxation success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared path-relaxation path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared path-relaxation path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared path-relaxation function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled path-relaxation remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared path-relaxation parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared path-relaxation owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedEconomyMarketAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendEconomyMarketProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedEconomyMarketPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedEconomyMarket(29)
	if !ok {
		t.Fatal("generated economy-market oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared economy-market success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared economy-market path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared economy-market path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared economy-market function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled economy-market remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared economy-market parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared economy-market owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedBehaviorTreeAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendBehaviorTreeProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedBehaviorTreePreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedBehaviorTree(29)
	if !ok {
		t.Fatal("generated behavior-tree oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared behavior-tree success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf("prepared behavior-tree path changed owner table count from %d to %d", tableCount, len(prepared.tables.tables))
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf("prepared behavior-tree path changed owner string count from %d to %d", stringCount, len(prepared.strings.records))
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared behavior-tree function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled behavior-tree remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared behavior-tree parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared behavior-tree owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedProcgenRoomScoringAvoidsRuntimeTables(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendProcgenRoomScoringProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedProcgenRoomScoringPreparedFixture(context)
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
	tableCount := len(prepared.tables.tables)
	stringCount := len(prepared.strings.records)
	want, ok := backendGeneratedProcgenRoomScoring(29)
	if !ok {
		t.Fatal("generated procgen room-scoring oracle exited")
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, nil)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, nil)
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared procgen room-scoring success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.tables.tables) != tableCount {
		t.Fatalf(
			"prepared procgen room-scoring path changed owner table count from %d to %d",
			tableCount,
			len(prepared.tables.tables),
		)
	}
	if len(prepared.strings.records) != stringCount {
		t.Fatalf(
			"prepared procgen room-scoring path changed owner string count from %d to %d",
			stringCount,
			len(prepared.strings.records),
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared procgen room-scoring function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf(
			"controlled procgen room-scoring remaining = %d, generic %d",
			preparedController.remaining,
			genericController.remaining,
		)
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
	assertMachineOwnerNumberResult(t, prepared, want)
	assertMachineOwnerNumberResult(t, generic, want)
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared procgen room-scoring parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared procgen room-scoring owner allocations = %v, want 0", allocations)
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

func TestMachinePreparedFixedVarargsAvoidPackAndClosureMaterialization(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendVarargProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedVarargPreparedFixture(context)
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
	assertMachineOwnerNumberResult(t, prepared, backendVarargExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendVarargExpected(29))
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared fixed-vararg success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.closures.closures) != baseClosures {
		t.Fatalf(
			"prepared fixed-vararg path materialized %d local closures",
			len(prepared.closures.closures)-baseClosures,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared fixed-vararg function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf("controlled fixed-vararg remaining = %d, generic %d", preparedController.remaining, genericController.remaining)
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
	assertMachineOwnerNumberResult(t, prepared, backendVarargExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendVarargExpected(29))
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared fixed-vararg parameter fallback = calls %d exit %#v", calls, observed)
	}

	globals := map[string]Value{
		"select": HostFuncValue(func([]Value) ([]Value, error) {
			return nil, errors.New("select override")
		}),
	}
	if err := prepared.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	if err := generic.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	preparedOverrideErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
	genericOverrideErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
	if calls != 3 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared fixed-vararg intrinsic fallback = calls %d exit %#v", calls, observed)
	}
	if preparedOverrideErr == nil || genericOverrideErr == nil ||
		preparedOverrideErr.Error() != genericOverrideErr.Error() ||
		!strings.Contains(preparedOverrideErr.Error(), "select override") {
		t.Fatalf("prepared/generic fixed-vararg override errors = %v / %v", preparedOverrideErr, genericOverrideErr)
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
			t.Fatalf("prepared fixed-vararg owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedFixedTuplesAvoidResultPackAndClosureMaterialization(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendTupleProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedTuplePreparedFixture(context)
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
	assertMachineOwnerNumberResult(t, prepared, backendTupleExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendTupleExpected(29))
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared fixed-tuple success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.closures.closures) != baseClosures {
		t.Fatalf(
			"prepared fixed-tuple path materialized %d local closures",
			len(prepared.closures.closures)-baseClosures,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared fixed-tuple function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf("controlled fixed-tuple remaining = %d, generic %d", preparedController.remaining, genericController.remaining)
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
	assertMachineOwnerNumberResult(t, prepared, backendTupleExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendTupleExpected(29))
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared fixed-tuple parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared fixed-tuple owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedScalarCoroutineAvoidsRuntimeObjectsAndReplaysEntry(t *testing.T) {
	image := machinePreparedTestImageForSource(t, backendCoroutineProofSource)
	calls := 0
	var observed machinePreparedExit
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		observed = backendGeneratedCoroutinePreparedFixture(context)
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
	baseCoroutineRecords := len(prepared.coroutines.arena.records)
	baseLiveCoroutines := prepared.coroutines.arena.live
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
	assertMachineOwnerNumberResult(t, prepared, backendCoroutineExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendCoroutineExpected(29))
	if calls != 1 || observed.kind != machinePreparedExitReturnOneNumber {
		t.Fatalf("prepared scalar coroutine success = calls %d exit %#v", calls, observed)
	}
	if len(prepared.closures.closures) != baseClosures ||
		len(prepared.coroutines.arena.records) != baseCoroutineRecords ||
		prepared.coroutines.arena.live != baseLiveCoroutines {
		t.Fatalf(
			"prepared scalar coroutine materialized closures/coroutines = %d/%d live %d, want %d/%d live %d",
			len(prepared.closures.closures),
			len(prepared.coroutines.arena.records),
			prepared.coroutines.arena.live,
			baseClosures,
			baseCoroutineRecords,
			baseLiveCoroutines,
		)
	}

	preparedController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	genericController, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxInstructions: 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	runMachinePreparedTestProto(t, prepared, 1, []slot{preparedArg}, preparedController)
	runMachinePreparedTestProto(t, generic, 1, []slot{genericArg}, genericController)
	if calls != 1 {
		t.Fatalf("prepared scalar coroutine function ran under execution policy %d times", calls)
	}
	if preparedController.remaining != genericController.remaining {
		t.Fatalf("controlled scalar coroutine remaining = %d, generic %d", preparedController.remaining, genericController.remaining)
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
	assertMachineOwnerNumberResult(t, prepared, backendCoroutineExpected(29))
	assertMachineOwnerNumberResult(t, generic, backendCoroutineExpected(29))
	if calls != 2 || observed.kind != machinePreparedExitReplayEntry {
		t.Fatalf("prepared scalar coroutine parameter fallback = calls %d exit %#v", calls, observed)
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
			t.Fatalf("prepared scalar coroutine owner allocations = %v, want 0", allocations)
		}
	}
}

func TestMachinePreparedScalarCoroutineGuardsEveryCoreIntrinsic(t *testing.T) {
	for _, field := range []string{"create", "resume", "status", "yield"} {
		t.Run(field, func(t *testing.T) {
			image := machinePreparedTestImageForSource(t, backendCoroutineProofSource)
			calls := 0
			var observed machinePreparedExit
			program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
				calls++
				observed = backendGeneratedCoroutinePreparedFixture(context)
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

			override := func() map[string]Value {
				table := baseCoroutine()
				if err := table.Set(StringValue(field), HostFuncValue(func([]Value) ([]Value, error) {
					return nil, errors.New("rebound coroutine intrinsic")
				})); err != nil {
					t.Fatal(err)
				}
				return map[string]Value{"coroutine": TableValue(table)}
			}
			if err := prepared.importGlobalsStopped(override()); err != nil {
				t.Fatal(err)
			}
			if err := generic.importGlobalsStopped(override()); err != nil {
				t.Fatal(err)
			}
			preparedArg, err := prepared.importValueStopped(NumberValue(29))
			if err != nil {
				t.Fatal(err)
			}
			genericArg, err := generic.importValueStopped(NumberValue(29))
			if err != nil {
				t.Fatal(err)
			}
			preparedErr := runMachinePreparedTestProtoError(t, prepared, 1, []slot{preparedArg}, nil)
			genericErr := runMachinePreparedTestProtoError(t, generic, 1, []slot{genericArg}, nil)
			if calls != 1 || observed.kind != machinePreparedExitReplayEntry {
				t.Fatalf("prepared rebound coroutine.%s = calls %d exit %#v", field, calls, observed)
			}
			if preparedErr == nil || genericErr == nil || preparedErr.Error() != genericErr.Error() {
				t.Fatalf("prepared/generic rebound coroutine.%s errors = %v / %v", field, preparedErr, genericErr)
			}
		})
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

func BenchmarkMachinePreparedScalarMetatableIndexOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendMetatableIndexProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedMetatableIndexPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericScalarMetatableIndexOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendMetatableIndexProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedScalarMethodOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendMethodProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedMethodPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericScalarMethodOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendMethodProofSource)
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

func BenchmarkMachinePreparedFiniteStringStateOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendFiniteStringStateProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedFiniteStringStatePreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericFiniteStringStateOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendFiniteStringStateProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedStructuralStringKeyOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendStructuralStringKeyProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedStructuralStringKeyPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericStructuralStringKeyOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendStructuralStringKeyProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedSparseGridOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendSparseGridProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedSparseGridPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericSparseGridOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendSparseGridProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedProjectileSweepOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendProjectileSweepProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedProjectileSweepPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericProjectileSweepOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendProjectileSweepProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedCombatTickOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCombatTickProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedCombatTickPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericCombatTickOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCombatTickProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedAbilityResolutionOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendAbilityResolutionProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedAbilityResolutionPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericAbilityResolutionOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendAbilityResolutionProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedAIUtilityScoringOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendAIUtilityScoringProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedAIUtilityScoringPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericAIUtilityScoringOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendAIUtilityScoringProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedCooldownSchedulerOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCooldownSchedulerProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedCooldownSchedulerPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericCooldownSchedulerOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCooldownSchedulerProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedSaveStateDiffOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendSaveStateDiffProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedSaveStateDiffPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericSaveStateDiffOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendSaveStateDiffProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedThreatAggroOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendThreatAggroProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedThreatAggroPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericThreatAggroOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendThreatAggroProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedDialogueConditionOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendDialogueConditionProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedDialogueConditionPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericDialogueConditionOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendDialogueConditionProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedComponentChurnOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendComponentChurnProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedComponentChurnPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericComponentChurnOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendComponentChurnProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedBuffStackOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendBuffStackProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedBuffStackPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericBuffStackOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendBuffStackProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedStateMachineOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendStateMachineProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedStateMachinePreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericStateMachineOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendStateMachineProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedCommandRouterOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCommandRouterProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedCommandRouterPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericCommandRouterOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCommandRouterProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedEventDispatchOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendEventDispatchProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedEventDispatchPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericEventDispatchOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendEventDispatchProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedPathRelaxationOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendPathRelaxationProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedPathRelaxationPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericPathRelaxationOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendPathRelaxationProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedEconomyMarketOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendEconomyMarketProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedEconomyMarketPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericEconomyMarketOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendEconomyMarketProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedBehaviorTreeOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendBehaviorTreeProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedBehaviorTreePreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericBehaviorTreeOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendBehaviorTreeProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedProcgenRoomScoringOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendProcgenRoomScoringProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedProcgenRoomScoringPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericProcgenRoomScoringOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendProcgenRoomScoringProofSource)
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

func BenchmarkMachinePreparedFixedVarargOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendVarargProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedVarargPreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericFixedVarargOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendVarargProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedFixedTupleOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendTupleProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedTuplePreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericFixedTupleOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendTupleProofSource)
	benchmarkMachineNumericOwner(b, image, nil)
}

func BenchmarkMachinePreparedScalarCoroutineOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCoroutineProofSource)
	program := machinePreparedBenchmarkProgram(b, image, backendGeneratedCoroutinePreparedFixture)
	benchmarkMachineNumericOwner(b, image, program)
}

func BenchmarkMachineGenericScalarCoroutineOwner(b *testing.B) {
	image := machinePreparedBenchmarkImage(b, backendCoroutineProofSource)
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
