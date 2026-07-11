package ember

import (
	"reflect"
	"testing"
)

func TestProtoFieldClassificationBudget(t *testing.T) {
	const runtimeSideTableCeiling = 1
	core := map[string]struct{}{
		"constants":               {},
		"constantKeys":            {},
		"constantKeyOK":           {},
		"constantNumbers":         {},
		"constantNumberOK":        {},
		"globalNames":             {},
		"sharedBaseGlobalSlots":   {},
		"code":                    {},
		"packedCode":              {},
		"lines":                   {},
		"prototypes":              {},
		"upvalues":                {},
		"registers":               {},
		"params":                  {},
		"variadic":                {},
		"capturedLocals":          {},
		"directFrameIndexCaches":  {},
		"reuseZeroCaptureClosure": {},
		"canonicalClosure":        {},
		"verifyErr":               {},
	}
	runtimeSideTables := map[string]struct{}{
		"entryNilRegisters": {},
	}
	if len(runtimeSideTables) != runtimeSideTableCeiling {
		t.Fatalf("runtime Proto side-table allowlist has %d fields, want exactly %d", len(runtimeSideTables), runtimeSideTableCeiling)
	}

	protoType := reflect.TypeOf(Proto{})
	sideTableCount := 0
	for fieldName := range runtimeSideTables {
		field, ok := protoType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("runtime Proto side-table %q is missing", fieldName)
		}
		if field.Type.Kind() != reflect.Slice {
			t.Fatalf("runtime Proto side-table %q has kind %s, want slice", fieldName, field.Type.Kind())
		}
	}
	for fieldIndex := 0; fieldIndex < protoType.NumField(); fieldIndex++ {
		field := protoType.Field(fieldIndex)
		_, coreOK := core[field.Name]
		_, sideTableOK := runtimeSideTables[field.Name]
		if coreOK == sideTableOK {
			t.Fatalf("Proto field %q has core=%t and runtimeSideTable=%t, want exactly one classification", field.Name, coreOK, sideTableOK)
		}
		if sideTableOK {
			sideTableCount++
		}
	}

	if sideTableCount > runtimeSideTableCeiling {
		t.Fatalf("Proto has %d runtime side tables, want at most %d", sideTableCount, runtimeSideTableCeiling)
	}
}
