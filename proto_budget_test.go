package ember

import (
	"reflect"
	"testing"
)

func TestProtoFieldClassificationBudget(t *testing.T) {
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
		"numericOperandFactPCs": {},
		"entryNilRegisters":     {},
	}

	protoType := reflect.TypeOf(Proto{})
	sideTableCount := 0
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

	if sideTableCount > 2 {
		t.Fatalf("Proto has %d runtime side tables, want at most 2", sideTableCount)
	}
}
