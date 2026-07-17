package ember

import (
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestBackendGoExactBenchmarkCorporaCanGenerate(t *testing.T) {
	groups := []struct {
		name     string
		variable string
		cases    []string
	}{
		{
			name:     "scenario",
			variable: "scenarioLuauCases",
			cases: []string{
				"combat_tick", "inventory_value", "event_dispatch", "buff_stack_tick",
				"ability_resolution", "ai_utility_scoring", "cooldown_scheduler",
				"projectile_sweep", "quest_progress_update", "behavior_tree_tick",
				"threat_aggro_table", "economy_market_tick", "formation_layout_score",
				"dialogue_condition_eval", "procgen_room_scoring", "save_state_diff",
				"path_relaxation", "component_churn", "prototype_fallback",
				"signal_bus_callbacks", "state_machine_transitions", "sparse_grid_neighbors",
				"dirty_metatable_writes", "array_hole_compaction", "command_vararg_router",
			},
		},
		{
			name:     "top10",
			variable: "top10LuauCases",
			cases: []string{
				"arithmetic_for", "while_branching", "table_fields", "array_ops",
				"generic_iteration", "closures_upvalues", "method_calls",
				"metatable_index", "varargs_select", "coroutine_yield",
			},
		},
		{
			name:     "classic",
			variable: "classicLuauCases",
			cases:    []string{"recursive_fibonacci", "iterative_fibonacci"},
		},
	}
	for _, group := range groups {
		for _, tc := range loadLuauBenchmarkCases(t, group.variable, group.cases) {
			t.Run(group.name+"/"+tc.name, func(t *testing.T) {
				source := "local function kernel(seed)\n" + tc.source + "\nend\nreturn kernel\n"
				switch tc.name {
				case "event_dispatch":
					backendExactEventDispatchCanGenerate(t, source)
				case "prototype_fallback":
					backendExactPrototypeFallbackCanGenerate(t, source)
				case "signal_bus_callbacks":
					backendExactSignalBusCanGenerate(t, source)
				case "sparse_grid_neighbors":
					backendScenarioSparseGridCanGenerate(t, source)
				case "dirty_metatable_writes":
					backendDirtyMetatableGeneratedSources(t, source)
				case "command_vararg_router":
					backendScenarioCommandRouterCanGenerate(t, source)
				case "closures_upvalues":
					backendExactClosureCanGenerate(t, source)
				case "method_calls":
					backendExactMethodCanGenerate(t, source)
				case "varargs_select":
					backendExactVarargCanGenerate(t, source)
				case "coroutine_yield":
					backendExactCoroutineCanGenerate(t, source)
				case "recursive_fibonacci":
					backendExactRecursiveCanGenerate(t, source)
				default:
					backendExactSingleProtoCanGenerate(t, source)
				}
			})
		}
	}
}

func backendExactSingleProtoCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs, _ := backendExactCorpusIRs(t, source)
	if len(irs) != 2 {
		t.Fatalf("exact corpus Proto count = %d, want 2", len(irs))
	}
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedExactCorpusCoverage",
		preparedFunctionName: "backendGeneratedExactCorpusCoveragePrepared",
	}); err != nil {
		t.Fatal(err)
	}
}

func backendExactCorpusIRs(t *testing.T, source string) ([]*backendProtoIR, *codeImage) {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIRWithStrings(
			&image.prototypes[protoID], image.stringRecords, image.stringData,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs, image
}

func backendExactClosureCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs, _ := backendExactCorpusIRs(t, source)
	if len(irs) != 4 {
		t.Fatalf("exact closure Proto count = %d, want 4", len(irs))
	}
	targets := backendExactInferredTargets(t, irs, 2, 3)
	if _, err := emitBackendGoNumericProof(irs[3], backendGoNumericOptions{
		packageName: "ember", functionName: targets[3].functionName,
		receiverTable: targets[3].receiverTable, receiverTables: targets[3].receiverTables,
		fixedVarargCount:    targets[3].fixedVarargCount,
		capturedTableFields: targets[3].capturedTableFields,
	}); err != nil {
		t.Fatal(err)
	}
	backendExactCallerCanGenerate(t, irs[1], targets)
}

func backendExactEventDispatchCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs := backendEventDispatchProofIRs(t, source)
	targets := backendExactInferredTargets(t, irs, 2, 3, 4)
	for _, protoID := range []int32{2, 3, 4} {
		backendExactTargetCanGenerate(t, targets[protoID], targets)
	}
	backendExactCallerCanGenerate(t, irs[1], targets)
}

func backendExactPrototypeFallbackCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs := backendPrototypeFallbackProofIRs(t, source)
	targets := backendExactInferredTargets(t, irs, 2)
	backendExactTargetCanGenerate(t, targets[2], targets)
	backendExactCallerCanGenerate(t, irs[1], targets)
}

func backendExactSignalBusCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs := backendSignalBusProofIRs(t, source)
	targets := backendExactInferredTargets(t, irs, 2, 3)
	backendExactTargetCanGenerate(t, targets[3], targets)
	backendExactCallerCanGenerate(t, irs[1], targets)
}

func backendExactMethodCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs, _ := backendExactCorpusIRs(t, source)
	if len(irs) != 3 {
		t.Fatalf("exact method Proto count = %d, want 3", len(irs))
	}
	targets := backendExactInferredTargets(t, irs, 2)
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: targets[2].functionName,
		receiverTable: targets[2].receiverTable, receiverTables: targets[2].receiverTables,
	}); err != nil {
		t.Fatal(err)
	}
	backendExactCallerCanGenerate(t, irs[1], targets)
}

func backendExactVarargCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs, _ := backendExactCorpusIRs(t, source)
	if len(irs) != 3 {
		t.Fatalf("exact vararg Proto count = %d, want 3", len(irs))
	}
	targets := backendExactInferredTargets(t, irs, 2)
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: targets[2].functionName,
		fixedVarargCount: targets[2].fixedVarargCount,
	}); err != nil {
		t.Fatal(err)
	}
	backendExactCallerCanGenerate(t, irs[1], targets)
}

func backendExactRecursiveCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs, _ := backendExactCorpusIRs(t, source)
	if len(irs) != 3 {
		t.Fatalf("exact recursive Proto count = %d, want 3", len(irs))
	}
	targets := backendExactInferredTargets(t, irs, 2)
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: targets[2].functionName, selfRecursive: true,
	}); err != nil {
		t.Fatalf("emit exact recursive target: %v", err)
	}
	backendExactCallerCanGenerate(t, irs[1], targets)
}

func TestBackendGoOpenDirectTailReturnIsExactAndFailClosed(t *testing.T) {
	tc := loadLuauBenchmarkCases(t, "classicLuauCases", []string{"recursive_fibonacci"})[0]
	source := "local function kernel(seed)\n" + tc.source + "\nend\nreturn kernel\n"
	irs, _ := backendExactCorpusIRs(t, source)
	targets := backendExactInferredTargets(t, irs, 2)
	options := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedExactRecursiveTail",
		preparedFunctionName: "backendGeneratedExactRecursiveTailPrepared",
		directTargets:        targets,
	}
	generated, err := emitBackendGoNumericProof(irs[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "generated.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse exact recursive tail source: %v", err)
	}
	if !strings.Contains(string(generated), targets[2].functionName+"(") {
		t.Fatal("exact recursive tail source does not call its proven direct target")
	}

	for _, tc := range []struct {
		name   string
		mutate func(*backendProtoIR, *backendGoNumericOptions)
	}{
		{
			name: "unbound target",
			mutate: func(_ *backendProtoIR, options *backendGoNumericOptions) {
				options.directTargets = nil
			},
		},
		{
			name: "different open call marker",
			mutate: func(ir *backendProtoIR, _ *backendGoNumericOptions) {
				for pc := range ir.ops {
					if ir.ops[pc].op == opCall {
						ir.ops[pc].callResults = -2
						return
					}
				}
			},
		},
		{
			name: "prefixed open return",
			mutate: func(ir *backendProtoIR, _ *backendGoNumericOptions) {
				for pc := range ir.ops {
					if ir.ops[pc].op == opReturn {
						ir.ops[pc].returnCount = -2
						return
					}
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := *irs[1]
			mutated.ops = append([]backendOperationIR(nil), irs[1].ops...)
			mutatedOptions := options
			tc.mutate(&mutated, &mutatedOptions)
			if _, err := emitBackendGoNumericProof(&mutated, mutatedOptions); err == nil {
				t.Fatal("emitted an unproven open direct tail return")
			}
		})
	}
}

func backendExactCoroutineCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs, image := backendExactCorpusIRs(t, source)
	if len(irs) != 3 {
		t.Fatalf("exact coroutine Proto count = %d, want 3", len(irs))
	}
	targets := backendExactInferredTargets(t, irs, 2)
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedExactCoroutine",
		preparedFunctionName: "backendGeneratedExactCoroutinePrepared",
		directTargets:        targets,
		coroutineDeadString:  backendCoroutineDeadStringID(t, image),
	}); err != nil {
		t.Fatal(err)
	}
}

func backendExactCallerCanGenerate(t *testing.T, ir *backendProtoIR, targets []backendGoNumericTarget) {
	t.Helper()
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedExactNestedCorpus",
		preparedFunctionName: "backendGeneratedExactNestedCorpusPrepared", directTargets: targets,
	}); err != nil {
		t.Fatalf("emit exact nested caller: %v", err)
	}
}

func backendExactInferredTargets(t *testing.T, irs []*backendProtoIR, protoIDs ...int32) []backendGoNumericTarget {
	t.Helper()
	targets, err := inferBackendGoNumericTargets(irs, protoIDs, "backendGeneratedExactTarget")
	if err != nil {
		t.Fatal(err)
	}
	return targets
}

func backendExactTargetCanGenerate(
	t *testing.T,
	target backendGoNumericTarget,
	targets []backendGoNumericTarget,
) {
	t.Helper()
	if _, err := emitBackendGoNumericProof(target.ir, backendGoNumericOptions{
		packageName:         "ember",
		functionName:        target.functionName,
		directTargets:       targets,
		selfRecursive:       target.selfRecursive,
		fixedVarargCount:    target.fixedVarargCount,
		receiverTable:       target.receiverTable,
		receiverTables:      target.receiverTables,
		coroutineTarget:     backendGoNumericHasCoroutineYield(target.ir),
		capturedTableFields: target.capturedTableFields,
	}); err != nil {
		t.Fatal(err)
	}
}

func backendScenarioSparseGridCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs := backendStructuralStringKeyProofIRs(t, source)
	targets := backendExactInferredTargets(t, irs, 2)
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: targets[2].functionName,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedScenarioSparseGrid",
		preparedFunctionName: "backendGeneratedScenarioSparseGridPrepared", directTargets: targets,
	}); err != nil {
		t.Fatal(err)
	}
}

func backendScenarioCommandRouterCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs := backendCommandRouterProofIRsForSource(t, source)
	targets := backendExactInferredTargets(t, irs, 2)
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: targets[2].functionName,
		fixedVarargCount: targets[2].fixedVarargCount,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedScenarioCommandRouter",
		preparedFunctionName: "backendGeneratedScenarioCommandRouterPrepared", directTargets: targets,
	}); err != nil {
		t.Fatal(err)
	}
}
