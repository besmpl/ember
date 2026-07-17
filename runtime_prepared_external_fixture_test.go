package ember

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"os"
	"testing"
)

const externalPreparedFixtureSource = `
return {
    update = function()
        local total = 0
        for index = 1, 64 do
            total = total + index
        end
        return total
    end,
}
`

func TestExternalPreparedFixtureIsFresh(t *testing.T) {
	generated := externalPreparedFixtureGeneratedSource(t)
	onDisk, err := os.ReadFile("internal/preparedfixture/prepared_generated.go")
	if err != nil {
		t.Fatalf("read external prepared fixture: %v\nexpected source:\n%s", err, generated)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatalf("external prepared fixture is stale\nexpected source:\n%s", generated)
	}
}

func externalPreparedFixtureGeneratedSource(t *testing.T) []byte {
	t.Helper()
	module := LogicalModule("prepared/external")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		module.String(): externalPreparedFixtureSource,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
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
	if len(ir.modules) != 1 || len(ir.modules[0].protos) != 2 {
		t.Fatalf("external fixture inventory = %d modules/%d Protos, want 1/2", len(ir.modules), len(ir.modules[0].protos))
	}
	generated, err := emitBackendGoNumericProof(ir.modules[0].protos[1], backendGoNumericOptions{
		packageName:          "preparedfixture",
		functionName:         "externalUpdate",
		preparedFunctionName: "externalUpdatePrepared",
		preparedImportPath:   "github.com/besmpl/ember",
		preparedQualifier:    "emberapi",
	})
	if err != nil {
		t.Fatal(err)
	}
	var descriptor bytes.Buffer
	fmt.Fprintf(&descriptor, "\nvar Bundle = emberapi.NewPreparedBundle(%d, %d, [32]byte{", ir.abiVersion, ir.semanticVersion)
	for index, value := range ir.programHash {
		if index != 0 {
			descriptor.WriteString(", ")
		}
		fmt.Fprintf(&descriptor, "0x%02x", value)
	}
	descriptor.WriteString("}, [][]emberapi.PreparedFunction{{nil, externalUpdatePrepared}})\n")
	generated = append(generated, descriptor.Bytes()...)
	formatted, err := format.Source(generated)
	if err != nil {
		t.Fatalf("format external prepared fixture: %v\n%s", err, generated)
	}
	return formatted
}
