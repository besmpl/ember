package ember

import (
	"errors"
	"reflect"
	"runtime"
	"testing"
)

func TestMachineOwnerRunsRootsWithSharedGlobalsAndCachedModules(t *testing.T) {
	image := machineOwnerProgramImage(t, []string{
		`shared = 42
sharedCall = function()
    return 42
end
return shared`,
		`return sharedCall()`,
		`return {value = 1}`,
	})
	owner, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()

	if err := owner.executeRoot(0, nil); err != nil {
		t.Fatalf("execute first root: %v", err)
	}
	if err := owner.executeRoot(1, nil); err != nil {
		t.Fatalf("execute second root: %v", err)
	}
	assertMachineOwnerNumberResult(t, owner, 42)

	first, err := owner.loadModule(2, nil)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}
	second, err := owner.loadModule(2, nil)
	if err != nil {
		t.Fatalf("load cached module: %v", err)
	}
	if first != second {
		t.Fatalf("cached export = %#x, want original %#x", second, first)
	}
	if _, err := owner.scalarMachine.tableID(second); err != nil {
		t.Fatalf("cached export is not the original table: %v", err)
	}
}

func TestMachineOwnerRetainsClosureCapturesAcrossRunsAndGrowth(t *testing.T) {
	image := machineOwnerProgramImage(t, []string{
		`local value = 0
return function()
    value = value + 1
    return value
end`,
		`local values = {}
for i = 1, 256 do
    values[i] = i
end
return values[256]`,
	})
	owner, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()

	if err := owner.executeRoot(0, nil); err != nil {
		t.Fatalf("create closure: %v", err)
	}
	closure, err := owner.resultAt(0)
	if err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 2; want++ {
		runtime.GC()
		if err := owner.executeClosure(closure, nil, nil); err != nil {
			t.Fatalf("call closure %d: %v", want, err)
		}
		assertMachineOwnerNumberResult(t, owner, float64(want))
		if err := owner.executeRoot(1, nil); err != nil {
			t.Fatalf("growth run %d: %v", want, err)
		}
	}
	if err := owner.executeClosure(closure, nil, nil); err != nil {
		t.Fatalf("call retained closure after growth: %v", err)
	}
	assertMachineOwnerNumberResult(t, owner, 3)
}

func TestMachineOwnerCaptureAfterSetGlobalMatchesVM(t *testing.T) {
	const source = `local value = 42
shared = value
local callback = function()
    return value
end
return callback()`
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	vmResults, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if !vmResults[0].IsNil() {
		t.Fatalf("VM result = %s, want nil for the compiler's shifted capture", vmResults[0].Kind())
	}
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{source}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if err := owner.executeRoot(0, nil); err != nil {
		t.Fatal(err)
	}
	value, err := owner.resultAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if value != slotNil {
		t.Fatalf("Machine result = %#x, want VM-compatible nil", value)
	}
}

func TestMachineOwnerExecutionRejectsBusyClosedStaleAndCycles(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`return 1`}))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := owner.beginRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.executeRoot(0, nil); !errors.Is(err, errMachineOwnerBusy) {
		t.Fatalf("execute while busy = %v, want busy", err)
	}
	lease.end()
	if _, _, err := owner.modules.begin(0); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.loadModule(0, nil); !errors.Is(err, errMachineModuleCycle) {
		t.Fatalf("load cycle = %v, want cycle", err)
	}
	if err := owner.modules.abort(0); err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.executeRoot(0, nil); !errors.Is(err, errMachineOwnerClosed) {
		t.Fatalf("execute after close = %v, want closed", err)
	}
	if err := owner.executeClosure(slotNil, nil, nil); !errors.Is(err, errMachineOwnerClosed) {
		t.Fatalf("stale call after close = %v, want closed", err)
	}
}

func TestMachineOwnerWarmedGlobalCallAndModulePathsAreFlat(t *testing.T) {
	image := machineOwnerProgramImage(t, []string{
		`shared = 42
return shared`,
		`local value = 0
return function()
    value = value + 1
    return value
end`,
		`return 7`,
	})
	owner, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if err := owner.executeRoot(1, nil); err != nil {
		t.Fatal(err)
	}
	closure, err := owner.resultAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.loadModule(2, nil); err != nil {
		t.Fatal(err)
	}
	lease, err := owner.beginRun()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.end()

	var runErr error
	runOnce := func() {
		if runErr != nil {
			return
		}
		if err := owner.executeRootStopped(0, nil, machineRunEffects{}); err != nil {
			runErr = err
			return
		}
		if err := owner.executeClosureStopped(closure, nil, nil, machineRunEffects{}); err != nil {
			runErr = err
			return
		}
		_, _, runErr = owner.loadModuleStopped(2, nil, machineRunEffects{})
	}
	if checkptrInstrumentedTest() {
		runOnce()
		if runErr != nil {
			t.Fatal(runErr)
		}
		return
	}
	allocations := testing.AllocsPerRun(100, func() {
		runOnce()
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if allocations != 0 {
		t.Fatalf("warmed global/call/module allocations = %v, want 0", allocations)
	}
}

func machineOwnerProgramImage(t *testing.T, sources []string) *programImage {
	t.Helper()
	modules := make([]programImageModule, len(sources))
	for index, source := range sources {
		proto, err := Compile(source)
		if err != nil {
			t.Fatalf("compile module %d: %v", index, err)
		}
		code, err := proto.preparedCodeImage()
		if err != nil {
			t.Fatalf("prepare module %d: %v", index, err)
		}
		modules[index] = programImageModule{
			moduleID: programModuleID(index),
			key:      moduleKey{kind: moduleKeyLogical, path: string(rune('a' + index))},
			code:     code,
		}
	}
	return &programImage{modules: modules}
}

func assertMachineOwnerNumberResult(t *testing.T, owner *machineOwner, want float64) {
	t.Helper()
	value, err := owner.resultAt(0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := owner.scalarMachine.number(value)
	if err != nil || got != want {
		t.Fatalf("result = (%v, %v), want %v", got, err, want)
	}
}

func TestMachineOwnerBindsProgramImageStringsPerModule(t *testing.T) {
	firstProto, err := Compile(`return "shared"`)
	if err != nil {
		t.Fatal(err)
	}
	firstCode, err := firstProto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	secondProto, err := Compile(`return "shared", "local"`)
	if err != nil {
		t.Fatal(err)
	}
	secondCode, err := secondProto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	image := &programImage{modules: []programImageModule{
		{moduleID: 0, key: moduleKey{kind: moduleKeyLogical, path: "first"}, code: firstCode},
		{moduleID: 1, key: moduleKey{kind: moduleKeyLogical, path: "second"}, code: secondCode},
	}}

	owner, err := newMachineOwner(image)
	if err != nil {
		t.Fatalf("newMachineOwner: %v", err)
	}
	if owner.image != image {
		t.Fatal("owner did not retain the immutable Program image")
	}
	firstID, err := owner.translateImageStringID(0, 1)
	if err != nil {
		t.Fatalf("translate first module string: %v", err)
	}
	secondSharedID, err := owner.translateImageStringID(1, 1)
	if err != nil {
		t.Fatalf("translate second module shared string: %v", err)
	}
	if firstID != secondSharedID {
		t.Fatalf("shared image strings received owner IDs %d and %d, want deduplication", firstID, secondSharedID)
	}
	secondLocalID, err := owner.translateImageStringID(1, 2)
	if err != nil {
		t.Fatalf("translate second module local string: %v", err)
	}
	if secondLocalID == firstID {
		t.Fatalf("distinct image strings shared owner ID %d", secondLocalID)
	}
}

func TestMachineOwnerLeaseAndCloseLifecycle(t *testing.T) {
	image := machineOwnerTestImage(t)
	first, err := newMachineOwner(image)
	if err != nil {
		t.Fatalf("new first owner: %v", err)
	}
	second, err := newMachineOwner(image)
	if err != nil {
		t.Fatalf("new second owner: %v", err)
	}
	firstID, err := first.translateImageStringID(0, 1)
	if err != nil {
		t.Fatalf("first owner string translation: %v", err)
	}
	secondID, err := second.translateImageStringID(0, 1)
	if err != nil {
		t.Fatalf("second owner string translation: %v", err)
	}
	if firstID != secondID {
		t.Fatalf("identical image string IDs differ across independent owners: %d, %d", firstID, secondID)
	}

	lease, err := first.beginRun()
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if _, err := first.beginRun(); !errors.Is(err, errMachineOwnerBusy) {
		t.Fatalf("second run error = %v, want busy", err)
	}
	if err := first.close(); !errors.Is(err, errMachineOwnerActive) {
		t.Fatalf("close with active run = %v, want active", err)
	}
	lease.end()
	lease.end()
	if err := first.close(); err != nil {
		t.Fatalf("close after idle run: %v", err)
	}
	if err := first.close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if _, err := first.beginRun(); !errors.Is(err, errMachineOwnerClosed) {
		t.Fatalf("begin after close = %v, want closed", err)
	}
	if _, err := first.translateImageStringID(0, 1); !errors.Is(err, errMachineOwnerClosed) {
		t.Fatalf("translation after close = %v, want closed", err)
	}
	if first.image != nil || first.stringTranslations != nil || first.stringRanges != nil {
		t.Fatal("close retained immutable image or string translations")
	}
	if first.strings.records != nil || first.strings.data != nil || first.strings.index != nil ||
		first.globals.names != nil || first.tables.tables != nil || first.modules.states != nil {
		t.Fatal("close retained owner arena storage")
	}
	if _, err := second.translateImageStringID(0, 1); err != nil {
		t.Fatalf("independent owner was invalidated by first close: %v", err)
	}
}

func TestMachineOwnerRejectsStaleAndInvalidStringTranslations(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerTestImage(t))
	if err != nil {
		t.Fatalf("new owner: %v", err)
	}
	for _, test := range []struct {
		name     string
		moduleID programModuleID
		stringID machineStringID
	}{
		{name: "invalid module", moduleID: 99, stringID: 1},
		{name: "invalid image string", moduleID: 0, stringID: 99},
		{name: "zero image string", moduleID: 0, stringID: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := owner.translateImageStringID(test.moduleID, test.stringID); err == nil {
				t.Fatal("invalid translation succeeded")
			}
		})
	}
	owner.stringTranslations[owner.stringRanges[0].offset+1] = invalidMachineStringID
	if _, err := owner.translateImageStringID(0, 1); err == nil {
		t.Fatal("stale translation succeeded")
	}
}

func TestMachineOwnerRejectsInvalidProgramImages(t *testing.T) {
	valid := machineOwnerTestImage(t)
	tests := []struct {
		name  string
		image *programImage
	}{
		{name: "nil image"},
		{name: "nil module code", image: &programImage{modules: []programImageModule{{moduleID: 0}}}},
		{name: "non-dense module ID", image: &programImage{modules: []programImageModule{{moduleID: 1, code: valid.modules[0].code}}}},
		{name: "invalid string span", image: &programImage{modules: []programImageModule{{moduleID: 0, code: &codeImage{stringRecords: []machineStringRecord{{offset: 1, length: 1}}}}}}},
		{name: "inconsistent module index", image: &programImage{
			modules:   []programImageModule{{moduleID: 0, key: valid.modules[0].key, code: valid.modules[0].code}},
			moduleIDs: map[moduleKey]programModuleID{valid.modules[0].key: 1},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if owner, err := newMachineOwner(test.image); err == nil || owner != nil {
				t.Fatalf("newMachineOwner = (%v, %v), want validation error", owner, err)
			}
		})
	}
}

func TestMachineOwnerMutableRecordsContainNoLegacyPointers(t *testing.T) {
	for _, fieldName := range []string{"stringRanges", "stringTranslations", "strings", "globals", "tables", "modules"} {
		field, ok := reflect.TypeOf(machineOwner{}).FieldByName(fieldName)
		if !ok {
			t.Fatalf("machineOwner field %q is missing", fieldName)
		}
		if typeContainsLegacyMachinePointer(field.Type, make(map[reflect.Type]bool)) {
			t.Fatalf("machineOwner field %q retains a legacy pointer", fieldName)
		}
	}
}

func typeContainsLegacyMachinePointer(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if typ == nil || seen[typ] {
		return false
	}
	seen[typ] = true
	if typ.Kind() == reflect.Pointer {
		switch typ.Elem() {
		case reflect.TypeOf(runtimeHeap{}), reflect.TypeOf(vmThread{}), reflect.TypeOf(vmFrame{}), reflect.TypeOf(Proto{}):
			return true
		}
		return typeContainsLegacyMachinePointer(typ.Elem(), seen)
	}
	switch typ.Kind() {
	case reflect.Array, reflect.Slice:
		return typeContainsLegacyMachinePointer(typ.Elem(), seen)
	case reflect.Map:
		return typeContainsLegacyMachinePointer(typ.Key(), seen) || typeContainsLegacyMachinePointer(typ.Elem(), seen)
	case reflect.Struct:
		for index := 0; index < typ.NumField(); index++ {
			if typeContainsLegacyMachinePointer(typ.Field(index).Type, seen) {
				return true
			}
		}
	}
	return false
}

func machineOwnerTestImage(t *testing.T) *programImage {
	t.Helper()
	proto, err := Compile(`return "owner"`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	return &programImage{modules: []programImageModule{{moduleID: 0, key: moduleKey{kind: moduleKeyLogical, path: "owner"}, code: code}}}
}
