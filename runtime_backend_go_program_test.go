package ember

import (
	"bytes"
	goparser "go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

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
