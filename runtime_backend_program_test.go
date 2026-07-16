package ember

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestBackendProgramIRBuildsDeterministicOrderedInventoryAndHash(t *testing.T) {
	program := loadBackendProgramTest(t, backendProgramTestLoader{
		"logical:game/z": {Name: "source/z", Text: "return 7 * 6"},
		"logical:game/a": {Name: "source/a", Text: "local function inc(value) return value + 1 end return inc(4)"},
	}, []Entrypoint{
		{Name: "z", Module: LogicalModule("game/z")},
		{Name: "a", Module: LogicalModule("game/a")},
	})
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	first, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("backend Program IR is not deterministic")
	}
	if first.abiVersion != backendPreparedABIVersion ||
		first.semanticVersion != backendPreparedSemanticVersion ||
		first.programHash == ([32]byte{}) {
		t.Fatalf("backend Program versions/hash = %d/%d/%x", first.abiVersion, first.semanticVersion, first.programHash)
	}
	if len(first.modules) != 2 ||
		first.modules[0].key.String() != "logical:game/a" ||
		first.modules[1].key.String() != "logical:game/z" {
		t.Fatalf("backend module inventory = %#v", first.modules)
	}
	if len(first.entrypoints) != 2 ||
		first.entrypoints[0].name != "z" ||
		first.entrypoints[0].moduleID != 1 ||
		first.entrypoints[1].name != "a" ||
		first.entrypoints[1].moduleID != 0 {
		t.Fatalf("backend entrypoint inventory = %#v", first.entrypoints)
	}
}

func TestBackendProgramIRHashBindsProgramAndEntrypointIdentity(t *testing.T) {
	baseSources := backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: "return 41 + 1"},
	}
	base := buildBackendProgramTest(t, baseSources, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})

	changedSource := backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: "return 41 + 2"},
	}
	if changed := buildBackendProgramTest(t, changedSource, []Entrypoint{{Name: "main", Module: LogicalModule("main")}}); changed.programHash == base.programHash {
		t.Fatal("semantic source change did not change Program hash")
	}

	changedEntrypoint := buildBackendProgramTest(t, baseSources, []Entrypoint{{Name: "other", Module: LogicalModule("main")}})
	if changedEntrypoint.programHash == base.programHash {
		t.Fatal("entrypoint identity change did not change Program hash")
	}

	changedSourceName := backendProgramTestLoader{
		"logical:main": {Name: "renamed/source", Text: "return 41 + 1"},
	}
	changed := buildBackendProgramTest(t, changedSourceName, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	if changed.programHash == base.programHash {
		t.Fatal("source identity change did not change Program hash")
	}
	baseProto := base.modules[0].protos[0]
	changedProto := changed.modules[0].protos[0]
	if !reflect.DeepEqual(baseProto.ops, changedProto.ops) ||
		!reflect.DeepEqual(baseProto.blocks, changedProto.blocks) ||
		!reflect.DeepEqual(baseProto.values, changedProto.values) {
		t.Fatal("source identity changed executable backend classifications")
	}
}

func TestBackendProgramIRRejectsOperationAndHashMismatch(t *testing.T) {
	ir := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: "return 1 + 2"},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	ir.modules[0].protos[0].ops[0].a++
	if err := verifyBackendProgramIR(ir); err == nil || !strings.Contains(err.Error(), "mismatches CodeImage") {
		t.Fatalf("verify operation mismatch = %v", err)
	}

	ir = buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: "return 1 + 2"},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	ir.programHash[0] ^= 0xff
	if err := verifyBackendProgramIR(ir); err == nil || !strings.Contains(err.Error(), "program hash mismatch") {
		t.Fatalf("verify hash mismatch = %v", err)
	}
}

func buildBackendProgramTest(t *testing.T, loader backendProgramTestLoader, entrypoints []Entrypoint) *backendProgramIR {
	t.Helper()
	program := loadBackendProgramTest(t, loader, entrypoints)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func loadBackendProgramTest(t *testing.T, loader backendProgramTestLoader, entrypoints []Entrypoint) *Program {
	t.Helper()
	program, _, err := LoadProgram(context.Background(), loader, ProgramOptions{
		Entrypoints: entrypoints,
		Parallelism: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return program
}

type backendProgramTestLoader map[string]Source

func (loader backendProgramTestLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	source, ok := loader[id.String()]
	if !ok {
		return Source{}, context.Canceled
	}
	return source, nil
}
