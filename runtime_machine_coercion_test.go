package ember

import (
	"math"
	"reflect"
	"testing"
)

func TestMachineNumericCoercionMatchesOldVM(t *testing.T) {
	texts := []string{
		"0", "-0", "42", "-17.5", ".25", "1e3", "-2.5E-2",
		"0x1p2", "0x1.8p+1", "NaN", "Inf", "+Inf", "-Inf",
		"", " 42", "42 ", "0x10", "1e", "forty-two",
	}
	arena := new(machineStringArena)
	for _, text := range texts {
		id, err := arena.internStringStopped(text)
		if err != nil {
			t.Fatalf("intern %q: %v", text, err)
		}
		value, err := slotPackHandle(slotTagString, uint32(id), 1)
		if err != nil {
			t.Fatalf("slot %q: %v", text, err)
		}
		action, err := machinePrepareNumericOperand(machineScalarOperand{value: value}, arena)
		if err != nil {
			t.Fatalf("prepare %q: %v", text, err)
		}
		got, gotErr := machineNumericOperandResult(action, "left", "add")
		want, wantErr := numericOperand(StringValue(text), "left", "add")
		if (gotErr != nil) != (wantErr != nil) {
			t.Fatalf("coerce %q error = %v, want %v", text, gotErr, wantErr)
		}
		if gotErr != nil {
			if gotErr.Error() != wantErr.Error() {
				t.Fatalf("coerce %q error = %q, want %q", text, gotErr, wantErr)
			}
			continue
		}
		if math.Float64bits(got) != math.Float64bits(want) && !(math.IsNaN(got) && math.IsNaN(want)) {
			t.Fatalf("coerce %q = %v (%x), want %v (%x)", text, got, math.Float64bits(got), want, math.Float64bits(want))
		}
	}
}

func TestMachineNumericCoercionPreservesNumbersAndErrors(t *testing.T) {
	for _, number := range []float64{0, math.Copysign(0, -1), 1.25, math.Inf(1), math.Inf(-1), math.NaN()} {
		operand := machineTestScalarNumber(t, number)
		action, err := machinePrepareNumericOperand(operand, nil)
		if err != nil {
			t.Fatalf("prepare %v: %v", number, err)
		}
		got, err := machineNumericOperandResult(action, "", "negate")
		if err != nil {
			t.Fatalf("number %v: %v", number, err)
		}
		if math.Float64bits(got) != math.Float64bits(number) && !(math.IsNaN(got) && math.IsNaN(number)) {
			t.Fatalf("number = %v (%x), want %v (%x)", got, math.Float64bits(got), number, math.Float64bits(number))
		}
	}

	action, err := machinePrepareNumericOperand(machineScalarOperand{value: slotBool(true)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machineNumericOperandResult(action, "right", "multiply"); err == nil || err.Error() != "multiply right operand is boolean, want number" {
		t.Fatalf("boolean error = %v", err)
	}
}

func TestMachineNumericCoercionActionIsPointerFree(t *testing.T) {
	if machineTestTypeContainsPointers(reflect.TypeOf(machineNumericAction{})) {
		t.Fatal("machineNumericAction contains pointers")
	}
}

func TestMachineNumericStringCoercionDoesNotAllocate(t *testing.T) {
	if checkptrInstrumentedTest() {
		t.Skip("allocation budget runs only without checkptr instrumentation")
	}
	arena := new(machineStringArena)
	id, err := arena.internStringStopped("123.5")
	if err != nil {
		t.Fatal(err)
	}
	value, err := slotPackHandle(slotTagString, uint32(id), 1)
	if err != nil {
		t.Fatal(err)
	}
	operand := machineScalarOperand{value: value}
	if _, err := machinePrepareNumericOperand(operand, arena); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(1000, func() {
		action, err := machinePrepareNumericOperand(operand, arena)
		if err != nil || action.kind != machineNumericActionReturn {
			panic("numeric-string coercion failed")
		}
	})
	if allocations != 0 {
		t.Fatalf("warmed numeric-string coercion allocations = %v, want 0", allocations)
	}
}

func machineTestTypeContainsPointers(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Array:
		return machineTestTypeContainsPointers(typ.Elem())
	case reflect.Struct:
		for index := range typ.NumField() {
			if machineTestTypeContainsPointers(typ.Field(index).Type) {
				return true
			}
		}
		return false
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return false
	default:
		return true
	}
}

func machineTestScalarNumber(t *testing.T, number float64) machineScalarOperand {
	t.Helper()
	bits := math.Float64bits(number)
	value := slot(bits)
	if bits&slotTaggedMask != slotTaggedPrefix {
		return machineScalarOperand{value: value}
	}
	boxed, err := slotPackHandle(slotTagBoxedNumber, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	return machineScalarOperand{value: boxed, numberBits: bits}
}
