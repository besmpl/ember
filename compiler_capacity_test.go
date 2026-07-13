package ember

import (
	"reflect"
	"testing"
)

func TestEstimatedIRCapacity(t *testing.T) {
	for _, test := range []struct {
		name              string
		nodes, statements int
		want              int
	}{
		{name: "zero", want: 0},
		{name: "negative", nodes: -10, statements: -2, want: 0},
		{name: "small", nodes: 1, statements: 1, want: 1},
		{name: "node hint", nodes: 280, want: 70},
		{name: "statement fallback", statements: 7, want: 7},
		{name: "cap", nodes: 1 << 30, statements: 1, want: maxEstimatedIRCapacity},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := estimatedIRCapacity(test.nodes, test.statements); got != test.want {
				t.Fatalf("estimatedIRCapacity(%d, %d) = %d, want %d", test.nodes, test.statements, got, test.want)
			}
		})
	}
}

func TestEstimatedIRCapacityHasRestrainedMaximum(t *testing.T) {
	if maxEstimatedIRCapacity > 1<<14 {
		t.Fatalf("maxEstimatedIRCapacity = %d, want at most %d", maxEstimatedIRCapacity, 1<<14)
	}
}

func TestEstimatedIRCapacityDoesNotOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := estimatedIRCapacity(maxInt, maxInt); got != maxEstimatedIRCapacity {
		t.Fatalf("estimatedIRCapacity(maxInt, maxInt) = %d, want cap %d", got, maxEstimatedIRCapacity)
	}
}

func TestCompilerStageCapacityPreservesBytecode(t *testing.T) {
	const source = `local function add(value)
	return value + 2
end
return add(1), "stage"`
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	hinted, err := emitCompilerStage(artifact)
	if err != nil {
		t.Fatalf("hinted emission returned error: %v", err)
	}
	zero, err := emitCompilerStageWithCapacity(artifact, 0)
	if err != nil {
		t.Fatalf("zero-capacity emission returned error: %v", err)
	}
	if !reflect.DeepEqual(hinted.ir, zero.ir) || !reflect.DeepEqual(hinted.constants, zero.constants) {
		t.Fatalf("IR emission changed with capacity hint: hinted=(%#v, %#v), zero=(%#v, %#v)", hinted.ir, hinted.constants, zero.ir, zero.constants)
	}
	hintedOptimized := optimizeCompilerStageIR(hinted)
	zeroOptimized := optimizeCompilerStageIR(zero)
	if !reflect.DeepEqual(hintedOptimized.ir, zeroOptimized.ir) || !reflect.DeepEqual(hintedOptimized.constants, zeroOptimized.constants) {
		t.Fatalf("optimized IR changed with capacity hint: hinted=(%#v, %#v), zero=(%#v, %#v)", hintedOptimized.ir, hintedOptimized.constants, zeroOptimized.ir, zeroOptimized.constants)
	}
	hintedProto, err := assembleAndSealCompilerStage(hinted, hintedOptimized)
	if err != nil {
		t.Fatalf("assemble hinted stage: %v", err)
	}
	zeroProto, err := assembleAndSealCompilerStage(zero, zeroOptimized)
	if err != nil {
		t.Fatalf("assemble zero-capacity stage: %v", err)
	}
	hintedValues, err := Run(hintedProto)
	if err != nil {
		t.Fatalf("Run hinted stage: %v", err)
	}
	zeroValues, err := Run(zeroProto)
	if err != nil {
		t.Fatalf("Run zero-capacity stage: %v", err)
	}
	if !equalCompilerCorpusValues(hintedValues, zeroValues) {
		t.Fatalf("hinted stage result = %#v, zero-capacity stage result = %#v", hintedValues, zeroValues)
	}
}
