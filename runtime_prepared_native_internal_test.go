package ember

import (
	"context"
	"strings"
	"testing"
)

func TestValidateBackendNativeProgramRejectsBodyAtBoundaryAdapter(t *testing.T) {
	entry := LogicalModule("prepared-native/offset-validation")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		entry.String(): `
local function add(x)
    return x + 1
end
return add
`,
	}, ProgramOptions{
		Entrypoints: []Entrypoint{{Name: "main", Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}

	for _, architecture := range []backendNativeArchitecture{
		backendNativeArchitectureARM64,
		backendNativeArchitectureX8664,
	} {
		t.Run(backendNativeArchitectureTestName(architecture), func(t *testing.T) {
			artifact, err := emitBackendNativeProgram(ir, architecture)
			if err != nil {
				t.Fatal(err)
			}
			function := &artifact.modules[0].functions[1]
			if !function.prepared || function.bodyOffset >= function.offset {
				t.Fatalf("emitted body/adapter metadata = %#v", *function)
			}
			function.bodyOffset = function.offset
			err = validateBackendNativeProgram(ir, artifact, architecture)
			if err == nil || !strings.Contains(err.Error(), "body/entry offsets") {
				t.Fatalf("validate malformed body offset = %v, want body/entry error", err)
			}
		})
	}
}

func backendNativeArchitectureTestName(architecture backendNativeArchitecture) string {
	switch architecture {
	case backendNativeArchitectureARM64:
		return "arm64"
	case backendNativeArchitectureX8664:
		return "x86-64"
	default:
		return "unknown"
	}
}
