package ember

import (
	"reflect"
	"testing"
)

func TestCompilerLayoutBudgets(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{name: "sourceToken", got: reflect.TypeOf(sourceToken{}).Size(), want: 24},
		{name: "boundNodeFacts", got: reflect.TypeOf(boundNodeFacts{}).Size(), want: 96},
		{name: "bytecodeIRInstruction", got: reflect.TypeOf(bytecodeIRInstruction{}).Size(), want: 88},
		{name: "instruction", got: reflect.TypeOf(instruction{}).Size(), want: 40},
		{name: "packedInstruction", got: reflect.TypeOf(packedInstruction{}).Size(), want: 16},
	} {
		if tc.got > tc.want {
			t.Errorf("%s=%d, want at most %d bytes", tc.name, tc.got, tc.want)
		}
	}
}
