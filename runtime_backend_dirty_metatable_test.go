package ember

import (
	"bytes"
	"context"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

const backendDirtyMetatableProofSource = `
local function kernel(seed)
    local dirty = {}
    local backing = {hp = 100 + seed % 3, mana = 30, xp = 0, gold = 5, flags = 1}
    local tracked = setmetatable({}, {
        __index = function(_, key)
            return backing[key] or 0
        end,
        __newindex = function(_, key, value)
            dirty[key] = (dirty[key] or 0) + 1
            backing[key] = value
        end,
    })
    local keys = {"hp", "mana", "xp", "gold", "flags"}
    local total = 0
    for tick = 1, 100 + seed % 2 do
        local key = keys[tick % rawlen(keys) + 1]
        tracked[key] = tracked[key] + tick % 9
        if tick % 7 == 0 then
            tracked[key] = tracked[key] - tracked.hp % 3
        end
        total = total + tracked[key] + dirty[key]
    end
    return total + tracked.hp + tracked.mana + tracked.xp + tracked.gold + tracked.flags
end
return kernel
`

func backendDirtyMetatableProofIRs(t *testing.T, source string) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 4 {
		t.Fatalf("dirty-metatable Proto count = %d, want 4", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendDirtyMetatableProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	closures := make(map[int32]*backendOperationIR)
	for pc := range irs[1].ops {
		closure := &irs[1].ops[pc]
		if closure.op != opClosure || closure.targetProto < 2 || closure.targetProto > 3 {
			continue
		}
		closures[closure.targetProto] = closure
	}
	backingFields, ok := backendGoCapturedTableFieldsForClosure(irs[1], irs[2], closures[2])
	if !ok || len(backingFields[0]) == 0 {
		return targets
	}
	targets[2] = backendGoNumericTarget{
		ir: irs[2], functionName: "backendGeneratedDirtyMetatableIndex", receiverTable: true,
		capturedTableFields: backingFields,
	}
	dirtyFields := append([]backendGoCapturedTableField(nil), backingFields[0]...)
	targets[3] = backendGoNumericTarget{
		ir: irs[3], functionName: "backendGeneratedDirtyMetatableNewIndex", receiverTable: true,
		capturedTableFields: map[int32][]backendGoCapturedTableField{
			0: dirtyFields,
			1: append([]backendGoCapturedTableField(nil), backingFields[0]...),
		},
	}
	return targets
}

func backendDirtyMetatableGeneratedSources(t *testing.T, source string) [3][]byte {
	t.Helper()
	irs := backendDirtyMetatableProofIRs(t, source)
	targets := backendDirtyMetatableProofTargets(irs)
	var generated [3][]byte
	var err error
	generated[0], err = emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: targets[2].functionName, receiverTable: true,
		capturedTableFields: targets[2].capturedTableFields,
	})
	if err != nil {
		t.Fatal(err)
	}
	generated[1], err = emitBackendGoNumericProof(irs[3], backendGoNumericOptions{
		packageName: "ember", functionName: targets[3].functionName, receiverTable: true,
		capturedTableFields: targets[3].capturedTableFields,
	})
	if err != nil {
		t.Fatal(err)
	}
	generated[2], err = emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedDirtyMetatableWrites",
		preparedFunctionName: "backendGeneratedDirtyMetatableWritesPreparedFixture",
		directTargets:        targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	return generated
}

func TestBackendGoDirtyMetatableWritesCanGenerate(t *testing.T) {
	irs := backendDirtyMetatableProofIRs(t, backendDirtyMetatableProofSource)
	targets := backendDirtyMetatableProofTargets(irs)
	for protoID := 2; protoID <= 3; protoID++ {
		target := targets[protoID]
		if _, err := emitBackendGoNumericProof(target.ir, backendGoNumericOptions{
			packageName: "ember", functionName: target.functionName, receiverTable: true,
			capturedTableFields: target.capturedTableFields,
		}); err != nil {
			t.Fatalf("emit dirty-metatable target Proto %d: %v", protoID, err)
		}
	}
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedDirtyMetatableWrites",
		preparedFunctionName: "backendGeneratedDirtyMetatableWritesPreparedFixture",
		directTargets:        targets,
	}); err != nil {
		t.Fatalf("emit dirty-metatable caller: %v", err)
	}
	plan, err := buildBackendGoNumericPlan(irs[1], backendGoNumericOptions{directTargets: targets})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.mutationMetatable.enabled || len(plan.mutationMetatable.fields) != 5 ||
		len(plan.mutationMetatable.backingSetByPC) != 5 || len(plan.mutationMetatable.getByPC) != 10 ||
		len(plan.mutationMetatable.setByPC) != 2 {
		t.Fatalf(
			"dirty-metatable inventory = enabled %t fields/init/get/set %d/%d/%d/%d",
			plan.mutationMetatable.enabled, len(plan.mutationMetatable.fields),
			len(plan.mutationMetatable.backingSetByPC), len(plan.mutationMetatable.getByPC),
			len(plan.mutationMetatable.setByPC),
		)
	}
}

func TestBackendGoCapturedDynamicTableKeyParametersAreStrings(t *testing.T) {
	irs := backendDirtyMetatableProofIRs(t, backendDirtyMetatableProofSource)
	for protoID := 2; protoID <= 3; protoID++ {
		tags, ok := backendGoNumericParameterTags(irs[protoID], 0)
		if !ok || len(tags) < 2 || tags[1] != backendTagString {
			t.Fatalf("dirty-metatable target Proto %d parameter tags = %x, want string key", protoID, tags)
		}
	}
	if count, ok := backendGoNumericFixedResultCount(irs[3]); !ok || count != 0 {
		t.Fatalf("dirty-metatable __newindex result count = %d, %t; want 0, true", count, ok)
	}
}

func TestBackendGoDirtyMetatableFixturesAreFreshAndCorrect(t *testing.T) {
	generated := backendDirtyMetatableGeneratedSources(t, backendDirtyMetatableProofSource)
	paths := [3]string{
		"runtime_backend_dirty_metatable_index_generated_test.go",
		"runtime_backend_dirty_metatable_newindex_generated_test.go",
		"runtime_backend_dirty_metatable_generated_test.go",
	}
	for index, path := range paths {
		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated[index], onDisk) {
			t.Fatalf("generated dirty-metatable fixture %s is stale", path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), path, generated[index], goparser.AllErrors); err != nil {
			t.Fatalf("parse generated dirty-metatable source %s: %v", path, err)
		}
	}
	text := string(bytes.Join(generated[:], nil))
	for _, required := range []string{
		"func backendGeneratedDirtyMetatableIndex(",
		"func backendGeneratedDirtyMetatableNewIndex(",
		"var dm0 float64", "var bm4 float64",
		"backendGeneratedDirtyMetatableIndex(&bm0",
		"backendGeneratedDirtyMetatableNewIndex(&dm0",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated dirty-metatable source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "SET_INDEX", "SET_METATABLE", "FAST_CALL",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated dirty-metatable source contains runtime materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendDirtyMetatableProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedDirtyMetatableWrites(seed)
		if !ok {
			t.Fatalf("generated dirty-metatable fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("dirty-metatable oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle dirty-metatable seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
		if seed == 0 && got != 8487 {
			t.Fatalf("canonical dirty-metatable result = %v, want 8487", got)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedDirtyMetatableWrites(29)
		}); allocations != 0 {
			t.Fatalf("generated dirty-metatable allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoDirtyMetatableTargetsRejectUnknownKeysWithoutMutation(t *testing.T) {
	var dirty, backing [5]float64
	backing = [5]float64{100, 30, 0, 5, 1}
	if value, ok := backendGeneratedDirtyMetatableIndex(
		&backing[0], &backing[1], &backing[2], &backing[3], &backing[4], 999,
	); !ok || value != 0 {
		t.Fatalf("dirty-metatable index target unknown key = %v, %t; want 0, true", value, ok)
	}
	if ok := backendGeneratedDirtyMetatableNewIndex(
		&dirty[0], &dirty[1], &dirty[2], &dirty[3], &dirty[4],
		&backing[0], &backing[1], &backing[2], &backing[3], &backing[4], 999, 42,
	); ok {
		t.Fatal("dirty-metatable newindex target accepted an unknown key")
	}
	if dirty != [5]float64{} || backing != [5]float64{100, 30, 0, 5, 1} {
		t.Fatalf("unknown dirty-metatable key mutated state: dirty %v backing %v", dirty, backing)
	}
}

func TestBackendGoDirtyMetatableIsIdentityBlindAndFailClosed(t *testing.T) {
	renamed := strings.Replace(backendDirtyMetatableProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	originalGenerated := backendDirtyMetatableGeneratedSources(t, backendDirtyMetatableProofSource)
	renamedGenerated := backendDirtyMetatableGeneratedSources(t, renamed)
	for index := range originalGenerated {
		if !bytes.Equal(originalGenerated[index], renamedGenerated[index]) {
			t.Fatalf("dirty-metatable fixture %d depends on private function identity", index)
		}
	}

	for name, source := range map[string]string{
		"changed newindex field": strings.Replace(
			backendDirtyMetatableProofSource, "__newindex =", "__call =", 1,
		),
		"extra metatable field": strings.Replace(
			backendDirtyMetatableProofSource, "__index = function", "__metatable = \"locked\",\n        __index = function", 1,
		),
		"observed metatable": strings.Replace(
			backendDirtyMetatableProofSource, "local keys =", "local observed = getmetatable(tracked)\n    local keys =", 1,
		),
		"observed backing": strings.Replace(
			backendDirtyMetatableProofSource, "return total + tracked.hp", "return total + backing.hp + tracked.hp", 1,
		),
		"observed dirty table": strings.Replace(
			backendDirtyMetatableProofSource, "return total + tracked.hp", "return total + rawlen(dirty) + tracked.hp", 1,
		),
		"changed dirty update": strings.Replace(
			backendDirtyMetatableProofSource, "dirty[key] = (dirty[key] or 0) + 1", "dirty[key] = value", 1,
		),
		"changed dirty increment": strings.Replace(
			backendDirtyMetatableProofSource, "dirty[key] = (dirty[key] or 0) + 1", "dirty[key] = (dirty[key] or 0) + 2", 1,
		),
		"changed dirty fallback": strings.Replace(
			backendDirtyMetatableProofSource, "dirty[key] = (dirty[key] or 0) + 1", "dirty[key] = (dirty[key] or 5) + 1", 1,
		),
		"changed backing value": strings.Replace(
			backendDirtyMetatableProofSource, "backing[key] = value", "backing[key] = value + 1", 1,
		),
		"mixed backing field": strings.Replace(
			backendDirtyMetatableProofSource, "mana = 30", "mana = true", 1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Fatalf("dirty-metatable compiler panicked for %s", name)
				}
			}()
			irs := backendDirtyMetatableProofIRs(t, source)
			targets := backendDirtyMetatableProofTargets(irs)
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName: "ember", functionName: "rejectDirtyMetatable", directTargets: targets,
			}); err == nil {
				t.Fatalf("dirty-metatable compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedDirtyMetatableWrites(b *testing.B) {
	for b.Loop() {
		_, _ = backendGeneratedDirtyMetatableWrites(29)
	}
}
