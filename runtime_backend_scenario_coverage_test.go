package ember

import "testing"

func TestBackendGoExactScenarioCorpusCanGenerate(t *testing.T) {
	cases := loadScenarioBenchmarkCases(t, []string{
		"combat_tick",
		"inventory_value",
		"event_dispatch",
		"buff_stack_tick",
		"ability_resolution",
		"ai_utility_scoring",
		"cooldown_scheduler",
		"projectile_sweep",
		"quest_progress_update",
		"behavior_tree_tick",
		"threat_aggro_table",
		"economy_market_tick",
		"formation_layout_score",
		"dialogue_condition_eval",
		"procgen_room_scoring",
		"save_state_diff",
		"path_relaxation",
		"component_churn",
		"prototype_fallback",
		"signal_bus_callbacks",
		"state_machine_transitions",
		"sparse_grid_neighbors",
		"dirty_metatable_writes",
		"array_hole_compaction",
		"command_vararg_router",
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := "local function kernel(seed)\n" + tc.source + "\nend\nreturn kernel\n"
			switch tc.name {
			case "event_dispatch":
				backendEventDispatchGeneratedSources(t, source)
			case "prototype_fallback":
				backendPrototypeFallbackGeneratedSources(t, source)
			case "signal_bus_callbacks":
				backendSignalBusGeneratedSources(t, source)
			case "sparse_grid_neighbors":
				backendScenarioSparseGridCanGenerate(t, source)
			case "dirty_metatable_writes":
				backendDirtyMetatableGeneratedSources(t, source)
			case "command_vararg_router":
				backendScenarioCommandRouterCanGenerate(t, source)
			default:
				backendScenarioSingleProtoCanGenerate(t, source)
			}
		})
	}
}

func backendScenarioSingleProtoCanGenerate(t *testing.T, source string) {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("Scenario Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIRWithStrings(
		&image.prototypes[1], image.stringRecords, image.stringData,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedScenarioCoverage",
		preparedFunctionName: "backendGeneratedScenarioCoveragePrepared",
	}); err != nil {
		t.Fatal(err)
	}
}

func backendScenarioSparseGridCanGenerate(t *testing.T, source string) {
	t.Helper()
	irs := backendStructuralStringKeyProofIRs(t, source)
	targets := backendStructuralStringKeyProofTargets(irs)
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
	targets := backendCommandRouterProofTargets(irs)
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
