package ember

import (
	"reflect"
	"testing"
)

func TestCompilerLayoutBudgets(t *testing.T) {
	if got := reflect.TypeOf(bytecodeIRInstruction{}).Size(); got != 88 {
		t.Fatalf("bytecodeIRInstruction=%d, want exactly 88 bytes for E1 baseline", got)
	}
	for _, tc := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{name: "sourceToken", got: reflect.TypeOf(sourceToken{}).Size(), want: 24},
		{name: "boundNodeFacts", got: reflect.TypeOf(boundNodeFacts{}).Size(), want: 96},
		// E2 will compact this representation to <=32 bytes; E1 freezes the
		// current 88-byte baseline while compiler logic moves behind accessors.
		{name: "bytecodeIRInstruction", got: reflect.TypeOf(bytecodeIRInstruction{}).Size(), want: 88},
		{name: "instruction", got: reflect.TypeOf(instruction{}).Size(), want: 40},
		{name: "wordcodeWord", got: reflect.TypeOf(wordcodeWord(0)).Size(), want: 4},
	} {
		if tc.got > tc.want {
			t.Errorf("%s=%d, want at most %d bytes", tc.name, tc.got, tc.want)
		}
	}
}
