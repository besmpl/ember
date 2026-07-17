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
				backendExactProgramCanGenerate(t, source)
			})
		}
	}
}

func backendExactProgramCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs, image := backendExactCorpusIRs(t, source)
	files, err := emitBackendGoNumericProgram(irs, backendGoNumericProgramOptions{
		packageName:          "ember",
		functionPrefix:       "backendGeneratedExactProgram",
		preparedFunctionName: "backendGeneratedExactProgramPrepared",
		entryProto:           1,
		coroutineDeadString:  backendCoroutineDeadStringID(t, image),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || files[0].protoID != 1 {
		t.Fatalf("exact program files = %#v", files)
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

func backendExactInferredTargets(t *testing.T, irs []*backendProtoIR, protoIDs ...int32) []backendGoNumericTarget {
	t.Helper()
	targets, err := inferBackendGoNumericTargets(irs, protoIDs, "backendGeneratedExactTarget")
	if err != nil {
		t.Fatal(err)
	}
	return targets
}
