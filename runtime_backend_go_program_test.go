package ember

import (
	"bytes"
	goparser "go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

const backendCapturedBatchProofSource = `
local kernel = function(value)
    return value + 1
end
local batch = function(count, seed)
    local checksum = 0
    for index = 1, count do
        local value = kernel(seed + index)
        checksum = checksum + value * (index % 7 + 1)
    end
    return checksum
end
return batch
`

func TestBackendGoNumericProgramOwnsTargetRolesAndFileOrder(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantProtos []int32
		wantText   string
	}{
		{
			name:       "ordinary targets",
			source:     backendDirtyMetatableProofSource,
			wantProtos: []int32{1, 2, 3},
		},
		{
			name:       "descriptor-only closure factory",
			source:     backendSignalBusProofSource,
			wantProtos: []int32{1, 3},
		},
		{
			name:       "embedded coroutine target",
			source:     backendCoroutineProofSource,
			wantProtos: []int32{1},
			wantText:   "type backendGeneratedProgramTargetProto2State struct",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			irs, image := backendExactCorpusIRs(t, tc.source)
			options := backendGoNumericProgramOptions{
				packageName:          "ember",
				functionPrefix:       "backendGeneratedProgram",
				preparedFunctionName: "backendGeneratedProgramPrepared",
				entryProto:           1,
				coroutineDeadString:  backendCoroutineDeadStringID(t, image),
			}
			first, err := emitBackendGoNumericProgram(irs, options)
			if err != nil {
				t.Fatal(err)
			}
			second, err := emitBackendGoNumericProgram(irs, options)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatal("program generation is not deterministic")
			}
			if len(first) != len(tc.wantProtos) {
				t.Fatalf("generated file count = %d, want %d", len(first), len(tc.wantProtos))
			}
			for index, file := range first {
				if file.protoID != tc.wantProtos[index] {
					t.Fatalf("generated file %d Proto = %d, want %d", index, file.protoID, tc.wantProtos[index])
				}
				if _, err := goparser.ParseFile(token.NewFileSet(), file.name, file.source, goparser.AllErrors); err != nil {
					t.Fatalf("parse generated file %q: %v", file.name, err)
				}
			}
			if tc.wantText != "" && !bytes.Contains(first[0].source, []byte(tc.wantText)) {
				t.Fatalf("generated entry does not contain %q", tc.wantText)
			}
		})
	}
}

func TestBackendGoNumericProgramFailsClosedWithoutPartialOutput(t *testing.T) {
	t.Run("missing target", func(t *testing.T) {
		irs, _ := backendExactCorpusIRs(t, backendDirtyMetatableProofSource)
		irs[3] = nil
		files, err := emitBackendGoNumericProgram(irs, backendGoNumericProgramOptions{
			packageName:          "ember",
			functionPrefix:       "backendGeneratedProgram",
			preparedFunctionName: "backendGeneratedProgramPrepared",
			entryProto:           1,
		})
		if err == nil || len(files) != 0 || !strings.Contains(err.Error(), "Proto 3") {
			t.Fatalf("missing target result = %d files, %v", len(files), err)
		}
	})

	t.Run("package name collision", func(t *testing.T) {
		irs, _ := backendExactCorpusIRs(t, backendDirtyMetatableProofSource)
		files, err := emitBackendGoNumericProgram(irs, backendGoNumericProgramOptions{
			packageName:          "ember",
			functionPrefix:       "backendGeneratedProgram",
			preparedFunctionName: "backendGeneratedProgramTargetProto2",
			entryProto:           1,
		})
		if err == nil || len(files) != 0 || !strings.Contains(err.Error(), "duplicate generated name") {
			t.Fatalf("colliding name result = %d files, %v", len(files), err)
		}
	})
}

func TestBackendGoNumericProgramBindsStaticCallableUpvalue(t *testing.T) {
	irs, _ := backendExactCorpusIRs(t, backendCapturedBatchProofSource)
	entryProto := int32(2)
	if targetProto, ok := backendGoNumericStaticUpvalueTargetProto(irs, entryProto, 0); !ok || targetProto != 1 {
		t.Fatalf("static callable upvalue target = %d, %t, want Proto 1", targetProto, ok)
	}
	files, err := emitBackendGoNumericProgram(irs, backendGoNumericProgramOptions{
		packageName:          "ember",
		functionPrefix:       "backendGeneratedCaptured",
		preparedFunctionName: "backendGeneratedCapturedPrepared",
		entryProto:           entryProto,
	})
	if err != nil {
		t.Fatal(err)
	}
	var generated []byte
	for _, file := range files {
		generated = append(generated, file.source...)
	}
	if !bytes.Contains(generated, []byte("backendGeneratedCapturedTargetProto1(")) {
		t.Fatal("generated program does not call the statically captured target")
	}
	if !bytes.Contains(generated, []byte("var ok9 bool")) {
		t.Fatal("generated program does not declare the captured-call guard")
	}
	wantPaths := map[int32]string{
		1: "runtime_backend_captured_target_generated_test.go",
		2: "runtime_backend_captured_batch_generated_test.go",
	}
	for _, file := range files {
		want, err := os.ReadFile(wantPaths[file.protoID])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(file.source, want) {
			t.Fatalf("generated captured-call fixture Proto %d is stale", file.protoID)
		}
	}
}

func TestBackendGoNumericStaticUpvalueTargetFailsClosed(t *testing.T) {
	const source = `
local kernel = function(value)
    return value + 1
end
local batch = function(count)
    return kernel(count)
end
return batch
`
	irs, _ := backendExactCorpusIRs(t, source)
	const entryProto = int32(2)
	targetProto, ok := backendGoNumericStaticUpvalueTargetProto(irs, entryProto, 0)
	if !ok || targetProto != 1 {
		t.Fatalf("static callable upvalue target = %d, %t, want Proto 1", targetProto, ok)
	}

	mutated := append([]*backendProtoIR(nil), irs...)
	root := *irs[0]
	root.values = append([]backendValueIR(nil), irs[0].values...)
	mutated[0] = &root
	for pc := range root.ops {
		closure := &root.ops[pc]
		if closure.op != opClosure || closure.targetProto != entryProto {
			continue
		}
		descriptor := irs[entryProto].upvalues[0]
		captured := backendValueBeforeOperation(&root, closure, int32(descriptor.index))
		if !root.validBackendValue(captured) {
			t.Fatal("captured callable value is unavailable")
		}
		root.values[captured-1].targetProtos = []int32{1, entryProto}
		if targetProto, ok := backendGoNumericStaticUpvalueTargetProto(mutated, entryProto, 0); ok {
			t.Fatalf("ambiguous callable upvalue resolved to Proto %d", targetProto)
		}
		return
	}
	t.Fatal("entry closure is unavailable")
}
