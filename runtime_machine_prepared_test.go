package ember

import (
	"context"
	"strings"
	"testing"
)

func TestMachinePreparedBindingRunsGeneratedNumericFunctionAndReplaysFailedGuard(t *testing.T) {
	image := machinePreparedTestImage(t)
	calls := 0
	program := machinePreparedTestProgram(t, image, 0, 1, func(context machinePreparedContext) machinePreparedExit {
		calls++
		seed, ok := context.numberParameter(0)
		if !ok {
			return machinePreparedReplayEntry()
		}
		result, ok := backendGeneratedNumericFixture(seed)
		if !ok {
			return machinePreparedReplayEntry()
		}
		return machinePreparedReturnOneNumber(result)
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
		seed, ok := context.numberParameter(0)
		if !ok {
			return machinePreparedReplayEntry()
		}
		result, ok := backendGeneratedNumericFixture(seed)
		if !ok {
			return machinePreparedReplayEntry()
		}
		return machinePreparedReturnOneNumber(result)
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
		seed, ok := context.numberParameter(0)
		if !ok {
			return machinePreparedReplayEntry()
		}
		result, ok := backendGeneratedNumericFixture(seed)
		if !ok {
			return machinePreparedReplayEntry()
		}
		return machinePreparedReturnOneNumber(result)
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
	t.Helper()
	image := machineOwnerProgramImage(t, []string{backendNumericProofSource})
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
