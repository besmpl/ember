package ember

import (
	"testing"
)

func TestSourceArtifactStoreDoesNotCacheEnvironmentDependentChecks(t *testing.T) {
	source := Source{Name: "logical:game/init", Text: "--!strict\nlocal value: number = HostValue\nreturn value"}
	identity := identifyModuleSource(source)
	store := newSourceArtifactStore()
	numberConfig := AnalysisConfig{Globals: map[string]TypeSummary{
		"HostValue": {Kind: TypeSummaryName, Display: "number"},
	}}
	stringConfig := AnalysisConfig{Globals: map[string]TypeSummary{
		"HostValue": {Kind: TypeSummaryName, Display: "string"},
	}}
	numberCheck, err := store.checkWithEnvWithLimits(
		source,
		identity,
		CompileLimits{},
		typeEnvFromAnalysisConfig(numberConfig),
		moduleSummaryEnv{},
	)
	if err != nil {
		t.Fatal(err)
	}
	stringCheck, err := store.checkWithEnvWithLimits(
		source,
		identity,
		CompileLimits{},
		typeEnvFromAnalysisConfig(stringConfig),
		moduleSummaryEnv{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(numberCheck.result.Diagnostics) != 0 {
		t.Fatalf("number catalog diagnostics = %#v", numberCheck.result.Diagnostics)
	}
	if len(stringCheck.result.Diagnostics) != 1 || stringCheck.result.Diagnostics[0].Code != "type-mismatch" {
		t.Fatalf("string catalog diagnostics = %#v", stringCheck.result.Diagnostics)
	}
}
