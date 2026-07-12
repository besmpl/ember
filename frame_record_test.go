package ember

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestVMFrameRecordFitsCompactCallStateBudget(t *testing.T) {
	typeOfRecord := reflect.TypeOf(vmFrameRecord{})
	if got, want := unsafe.Sizeof(vmFrameRecord{}), uintptr(48); got != want {
		t.Fatalf("vmFrameRecord size = %d bytes, want exactly %d", got, want)
	}

	pointers := 0
	for fieldIndex := 0; fieldIndex < typeOfRecord.NumField(); fieldIndex++ {
		if typeOfRecord.Field(fieldIndex).Type.Kind() == reflect.Pointer {
			pointers++
		}
	}
	if pointers > 1 {
		t.Fatalf("vmFrameRecord has %d pointer fields, want at most one", pointers)
	}

	proto := &Proto{}
	record := vmFrameRecord{
		proto:             proto,
		returnPC:          0x10203040,
		base:              17,
		top:               31,
		resultDestination: 5,
		resultCount:       0xffff_ffff,
		argumentBase:      8,
		argumentCount:     13,
		varargBase:        21,
		varargCount:       34,
		protectedDepth:    7,
		flags:             vmFrameRecordFlagProtected | vmFrameRecordFlagOpenResults,
	}

	if record.proto != proto {
		t.Fatal("vmFrameRecord lost its proto reference")
	}
	if record.returnPC != 0x10203040 || record.base != 17 || record.top != 31 {
		t.Fatalf("vmFrameRecord control state = %#v", record)
	}
	if record.resultDestination != 5 || record.resultCount != 0xffff_ffff {
		t.Fatalf("vmFrameRecord result state = %#v", record)
	}
	if record.argumentBase != 8 || record.argumentCount != 13 ||
		record.varargBase != 21 || record.varargCount != 34 {
		t.Fatalf("vmFrameRecord argument state = %#v", record)
	}
	if record.protectedDepth != 7 || record.flags != vmFrameRecordFlagProtected|vmFrameRecordFlagOpenResults {
		t.Fatalf("vmFrameRecord protection state = %#v", record)
	}
	if _, ok := reflect.TypeOf(vmFrame{}).FieldByName("caller"); ok {
		t.Fatal("vmFrame retains a pointer-linked caller field")
	}
}
