package ember

import (
	"errors"
	"math"
	"testing"
)

func TestMachineGuardedScalarIntrinsicsMatchBaseBuiltins(t *testing.T) {
	tests := []struct {
		name     string
		nativeID nativeFuncID
		args     []Value
		varargs  []Value
		base     func([]Value) ([]Value, error)
	}{
		{name: "math min", nativeID: nativeFuncMathMin, args: []Value{NumberValue(4), NumberValue(-2), NumberValue(9)}, base: baseMathMin},
		{name: "math min missing", nativeID: nativeFuncMathMin, base: baseMathMin},
		{name: "math min wrong second", nativeID: nativeFuncMathMin, args: []Value{NumberValue(4), BoolValue(true)}, base: baseMathMin},
		{name: "rawlen string", nativeID: nativeFuncRawLen, args: []Value{StringValue("a\x00bc")}, base: baseRawLen},
		{name: "rawlen table", nativeID: nativeFuncRawLen, args: []Value{TableValue(machineIntrinsicPublicSequence(t, []float64{1, 2, 3}))}, base: baseRawLen},
		{name: "rawlen missing", nativeID: nativeFuncRawLen, base: baseRawLen},
		{name: "rawlen wrong kind", nativeID: nativeFuncRawLen, args: []Value{NumberValue(1)}, base: baseRawLen},
		{name: "select vararg count", nativeID: nativeFuncSelect, varargs: []Value{NumberValue(1), NilValue(), StringValue("three")}, base: func(args []Value) ([]Value, error) {
			return baseSelect(append([]Value{StringValue("#")}, args...))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			owner := newMachineIntrinsicTestOwner(t)
			defer owner.close()
			args := importMachineIntrinsicTestValues(t, owner, test.args)
			outcome, gotErr := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
				nativeID:          test.nativeID,
				callee:            slotNativeID(test.nativeID),
				args:              args,
				selectVarargCount: len(test.varargs),
			})
			want, wantErr := test.base(append([]Value(nil), append(test.args, test.varargs...)...))
			assertMachineIntrinsicError(t, gotErr, wantErr)
			if gotErr != nil {
				return
			}
			if !outcome.matched {
				t.Fatal("exact builtin identity did not match guard")
			}
			got := exportMachineIntrinsicTestOutcome(t, owner, outcome)
			if !valuesSliceEquivalent(want, got) {
				t.Fatalf("Machine intrinsic = %s, base builtin = %s", valuesDiagnostic(got), valuesDiagnostic(want))
			}
		})
	}
}

func TestMachineGuardedIntrinsicOverrideMissDoesNotMutate(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	table := NewTable()
	if err := table.Set(NumberValue(1), NumberValue(10)); err != nil {
		t.Fatal(err)
	}
	args := importMachineIntrinsicTestValues(t, owner, []Value{TableValue(table), NumberValue(20)})

	outcome, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID: nativeFuncTableInsert,
		callee:   slotNativeID(nativeFuncTableRemove),
		args:     args,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.matched {
		t.Fatal("overridden table.insert identity matched guarded intrinsic")
	}
	tableID, err := owner.tableID(args[0])
	if err != nil {
		t.Fatal(err)
	}
	if length, err := owner.tables.rawLen(tableID); err != nil || length != 1 {
		t.Fatalf("table length after guard miss = %d, %v; want 1", length, err)
	}
}

func TestMachineGuardedTableIntrinsicArgumentErrorsMatchBaseBuiltins(t *testing.T) {
	tests := []struct {
		name     string
		nativeID nativeFuncID
		args     []Value
		base     func([]Value) ([]Value, error)
	}{
		{name: "insert missing table", nativeID: nativeFuncTableInsert, base: baseTableInsert},
		{name: "insert wrong table", nativeID: nativeFuncTableInsert, args: []Value{BoolValue(true), NumberValue(1)}, base: baseTableInsert},
		{name: "remove missing table", nativeID: nativeFuncTableRemove, base: baseTableRemove},
		{name: "remove wrong table", nativeID: nativeFuncTableRemove, args: []Value{StringValue("no")}, base: baseTableRemove},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			owner := newMachineIntrinsicTestOwner(t)
			defer owner.close()
			_, gotErr := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
				nativeID: test.nativeID,
				callee:   slotNativeID(test.nativeID),
				args:     importMachineIntrinsicTestValues(t, owner, test.args),
			})
			_, wantErr := test.base(test.args)
			assertMachineIntrinsicError(t, gotErr, wantErr)
		})
	}
}

func TestMachineGuardedTableIntrinsicsMatchBaseMutationAndErrors(t *testing.T) {
	tests := []struct {
		name     string
		nativeID nativeFuncID
		initial  []float64
		args     []Value
		base     func([]Value) ([]Value, error)
	}{
		{name: "insert append", nativeID: nativeFuncTableInsert, initial: []float64{10, 20}, args: []Value{NumberValue(30)}, base: baseTableInsert},
		{name: "insert middle", nativeID: nativeFuncTableInsert, initial: []float64{10, 30}, args: []Value{NumberValue(2), NumberValue(20)}, base: baseTableInsert},
		{name: "insert ignores extras", nativeID: nativeFuncTableInsert, initial: []float64{10}, args: []Value{NumberValue(2), NumberValue(20), NumberValue(99)}, base: baseTableInsert},
		{name: "insert missing value", nativeID: nativeFuncTableInsert, initial: []float64{10}, base: baseTableInsert},
		{name: "insert position error", nativeID: nativeFuncTableInsert, initial: []float64{10}, args: []Value{NumberValue(3), NumberValue(20)}, base: baseTableInsert},
		{name: "remove middle", nativeID: nativeFuncTableRemove, initial: []float64{10, 20, 30}, args: []Value{NumberValue(2)}, base: baseTableRemove},
		{name: "remove default", nativeID: nativeFuncTableRemove, initial: []float64{10, 20}, base: baseTableRemove},
		{name: "remove out of range", nativeID: nativeFuncTableRemove, initial: []float64{10}, args: []Value{NumberValue(3)}, base: baseTableRemove},
		{name: "remove position type", nativeID: nativeFuncTableRemove, initial: []float64{10}, args: []Value{BoolValue(true)}, base: baseTableRemove},
		{name: "remove ignores extras", nativeID: nativeFuncTableRemove, initial: []float64{10, 20}, args: []Value{NumberValue(1), NumberValue(99)}, base: baseTableRemove},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseTable := machineIntrinsicPublicSequence(t, test.initial)
			baseArgs := append([]Value{TableValue(baseTable)}, test.args...)
			want, wantErr := test.base(baseArgs)

			owner := newMachineIntrinsicTestOwner(t)
			defer owner.close()
			machineTable := machineIntrinsicPublicSequence(t, test.initial)
			machineArgs := importMachineIntrinsicTestValues(t, owner, append([]Value{TableValue(machineTable)}, test.args...))
			outcome, gotErr := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
				nativeID: test.nativeID,
				callee:   slotNativeID(test.nativeID),
				args:     machineArgs,
			})
			assertMachineIntrinsicError(t, gotErr, wantErr)
			if gotErr != nil {
				return
			}
			got := exportMachineIntrinsicTestOutcome(t, owner, outcome)
			if !valuesSliceEquivalent(want, got) {
				t.Fatalf("Machine results = %s, base results = %s", valuesDiagnostic(got), valuesDiagnostic(want))
			}
			exported, err := (&machineTableExporter{machine: &owner.scalarMachine, tables: make(map[machineTableID]machineExportedTable)}).value(machineArgs[0])
			if err != nil {
				t.Fatal(err)
			}
			gotTable, _ := exported.Table()
			assertMachineIntrinsicSequencesEqual(t, gotTable, baseTable)
		})
	}
}

func TestMachineGuardedTableInsertUsesExplicitEntryLimitAtomically(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	table := machineIntrinsicPublicSequence(t, []float64{1, 2})
	args := importMachineIntrinsicTestValues(t, owner, []Value{TableValue(table), NumberValue(3)})
	_, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID:        nativeFuncTableInsert,
		callee:          slotNativeID(nativeFuncTableInsert),
		args:            args,
		tableEntryLimit: 2,
	})
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Kind != LimitTableEntriesPerTable || limitErr.Limit != 2 || limitErr.Used != 3 {
		t.Fatalf("entry limit error = %#v, want table-entry 2/3", err)
	}
	tableID, idErr := owner.tableID(args[0])
	if idErr != nil {
		t.Fatal(idErr)
	}
	if length, lenErr := owner.tables.rawLen(tableID); lenErr != nil || length != 2 {
		t.Fatalf("length after rejected insert = %d, %v; want 2", length, lenErr)
	}
	if record, ok := owner.tables.lookup(tableID); !ok || record.entryCount != 2 {
		t.Fatalf("record after rejected insert = %#v (%t), want two entries", record, ok)
	}
	remove, removeErr := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID:        nativeFuncTableRemove,
		callee:          slotNativeID(nativeFuncTableRemove),
		args:            []slot{args[0], machineIntrinsicNumber(&owner.scalarMachine, 1)},
		tableEntryLimit: 2,
	})
	if removeErr != nil || remove.resultCount != 1 {
		t.Fatalf("remove before quota reuse = %#v, %v", remove, removeErr)
	}
	if _, insertErr := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID:        nativeFuncTableInsert,
		callee:          slotNativeID(nativeFuncTableInsert),
		args:            args,
		tableEntryLimit: 2,
	}); insertErr != nil {
		t.Fatalf("insert after deletion did not reuse quota: %v", insertErr)
	}
	if record, ok := owner.tables.lookup(tableID); !ok || record.entryCount != 2 {
		t.Fatalf("record after quota reuse = %#v (%t), want two entries", record, ok)
	}
}

func TestMachineGuardedTableInsertQuotaChecksLogicalInsertionPosition(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	args := importMachineIntrinsicTestValues(t, owner, []Value{
		TableValue(machineIntrinsicPublicSequence(t, []float64{1, 2})),
		NumberValue(2),
		NilValue(),
	})
	if _, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID:        nativeFuncTableInsert,
		callee:          slotNativeID(nativeFuncTableInsert),
		args:            args,
		tableEntryLimit: 2,
	}); err != nil {
		t.Fatalf("nil middle insert at quota: %v", err)
	}
	tableID, err := owner.tableID(args[0])
	if err != nil {
		t.Fatal(err)
	}
	record, ok := owner.tables.lookup(tableID)
	if !ok || record.entryCount != 2 {
		t.Fatalf("record after nil insert = %#v (%t), want two live entries", record, ok)
	}
	if value, err := owner.tables.rawGet(tableID, machineTableArrayKey(2)); err != nil || value != slotNil {
		t.Fatalf("inserted position = %#x, %v; want nil", value, err)
	}
	if value, err := owner.tables.rawGet(tableID, machineTableArrayKey(3)); err != nil || value == slotNil {
		t.Fatalf("shifted position = %#x, %v; want retained value", value, err)
	}
}

func TestMachineGuardedTableInsertRejectsMiddleGrowthBeforeMutation(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	args := importMachineIntrinsicTestValues(t, owner, []Value{
		TableValue(machineIntrinsicPublicSequence(t, []float64{10, 30})),
		NumberValue(2),
		NumberValue(20),
	})
	tableID, err := owner.tableID(args[0])
	if err != nil {
		t.Fatal(err)
	}

	_, err = runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID:        nativeFuncTableInsert,
		callee:          slotNativeID(nativeFuncTableInsert),
		args:            args,
		tableEntryLimit: 2,
	})
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Kind != LimitTableEntriesPerTable || limitErr.Used != 3 {
		t.Fatalf("middle insert error = %#v, want table-entry limit used 3", err)
	}
	for index, want := range []float64{10, 30} {
		value, getErr := owner.tables.rawGet(tableID, machineTableArrayKey(uint32(index+1)))
		if getErr != nil {
			t.Fatal(getErr)
		}
		if got, numberErr := owner.number(value); numberErr != nil || got != want {
			t.Fatalf("item %d after rejected insert = %v, %v; want %v", index+1, got, numberErr, want)
		}
	}
	if value, getErr := owner.tables.rawGet(tableID, machineTableArrayKey(3)); getErr != nil || value != slotNil {
		t.Fatalf("tail after rejected insert = %#x, %v; want nil", value, getErr)
	}
}

func TestMachineGuardedTableIntrinsicsPreserveArrayOrderAndDeletion(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	args := importMachineIntrinsicTestValues(t, owner, []Value{
		TableValue(machineIntrinsicPublicSequence(t, []float64{10, 30})),
		NumberValue(2),
		NumberValue(20),
	})
	if _, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID: nativeFuncTableInsert,
		callee:   slotNativeID(nativeFuncTableInsert),
		args:     args,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID: nativeFuncTableRemove,
		callee:   slotNativeID(nativeFuncTableRemove),
		args:     []slot{args[0], machineIntrinsicNumber(&owner.scalarMachine, 1)},
	}); err != nil {
		t.Fatal(err)
	}
	tableID, err := owner.tableID(args[0])
	if err != nil {
		t.Fatal(err)
	}
	var cursor machineTableCursor
	var keys []uint32
	for {
		key, _, next, present, err := owner.tables.next(tableID, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if !present {
			break
		}
		if key.kind != machineTableKeyArray {
			t.Fatalf("iteration key = %#v, want array key", key)
		}
		keys = append(keys, key.id)
		cursor = next
	}
	if len(keys) != 2 || keys[0] != 1 || keys[1] != 2 {
		t.Fatalf("iteration keys = %v, want [1 2]", keys)
	}
	record, ok := owner.tables.lookup(tableID)
	if !ok || record.entryCount != 2 || record.arrayLength != 2 {
		t.Fatalf("table record = %#v (%t), want two live array entries", record, ok)
	}
}

func TestMachineGuardedIntrinsicOutcomeLeavesPublicationAndGuestChargeToDispatch(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	insertArgs := importMachineIntrinsicTestValues(t, owner, []Value{TableValue(NewTable()), NumberValue(1)})
	insert, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID: nativeFuncTableInsert,
		callee:   slotNativeID(nativeFuncTableInsert),
		args:     insertArgs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if insert.resultCount != 0 || insert.additionalGuestCharge != 0 {
		t.Fatalf("insert outcome = %#v, want zero semantic results and no additional charge", insert)
	}
	selectCount, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
		nativeID:          nativeFuncSelect,
		callee:            slotNativeID(nativeFuncSelect),
		selectVarargCount: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selectCount.resultCount != 1 || selectCount.additionalGuestCharge != 0 {
		t.Fatalf("select outcome = %#v, want one semantic result and no additional charge", selectCount)
	}
	if number, err := owner.number(selectCount.value); err != nil || number != 4 {
		t.Fatalf("select count = %v, %v; want 4", number, err)
	}
}

func newMachineIntrinsicTestOwner(t *testing.T) *machineOwner {
	t.Helper()
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`return 1`}))
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func importMachineIntrinsicTestValues(t *testing.T, owner *machineOwner, values []Value) []slot {
	t.Helper()
	result := make([]slot, len(values))
	for index, value := range values {
		imported, err := owner.importValueStopped(value)
		if err != nil {
			t.Fatalf("import argument %d: %v", index+1, err)
		}
		result[index] = imported
	}
	return result
}

func exportMachineIntrinsicTestOutcome(t *testing.T, owner *machineOwner, outcome machineIntrinsicOutcome) []Value {
	t.Helper()
	if outcome.resultCount == 0 {
		return nil
	}
	value, err := (&machineTableExporter{machine: &owner.scalarMachine, tables: make(map[machineTableID]machineExportedTable)}).value(outcome.value)
	if err != nil {
		t.Fatal(err)
	}
	return []Value{value}
}

func machineIntrinsicPublicSequence(t *testing.T, values []float64) *Table {
	t.Helper()
	table := NewTable()
	for index, value := range values {
		if err := table.Set(NumberValue(float64(index+1)), NumberValue(value)); err != nil {
			t.Fatal(err)
		}
	}
	return table
}

func assertMachineIntrinsicSequencesEqual(t *testing.T, got, want *Table) {
	t.Helper()
	gotLength, gotErr := got.rawLen()
	wantLength, wantErr := want.rawLen()
	if gotErr != nil || wantErr != nil || gotLength != wantLength {
		t.Fatalf("sequence lengths = (%d, %v), (%d, %v)", gotLength, gotErr, wantLength, wantErr)
	}
	for index := 1; index <= wantLength; index++ {
		gotValue, _ := got.rawGet(NumberValue(float64(index)))
		wantValue, _ := want.rawGet(NumberValue(float64(index)))
		if !valuesEqual(gotValue, wantValue) {
			t.Fatalf("sequence item %d = %v, want %v", index, gotValue, wantValue)
		}
	}
}

func assertMachineIntrinsicError(t *testing.T, got, want error) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("Machine error = %v, base error = %v", got, want)
	}
	if got != nil && got.Error() != want.Error() {
		t.Fatalf("Machine error = %q, base error = %q", got, want)
	}
}

func TestMachineGuardedMathMinPreservesNaNAndSignedZero(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	for _, args := range [][]Value{
		{NumberValue(math.NaN()), NumberValue(1)},
		{NumberValue(math.Copysign(0, -1)), NumberValue(0)},
	} {
		outcome, err := runMachineGuardedIntrinsicStopped(&owner.scalarMachine, machineIntrinsicRequest{
			nativeID: nativeFuncMathMin,
			callee:   slotNativeID(nativeFuncMathMin),
			args:     importMachineIntrinsicTestValues(t, owner, args),
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := owner.number(outcome.value)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := baseMathMinValue(args)
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("math.min bits = %x, want %x", math.Float64bits(got), math.Float64bits(want))
		}
	}
}
