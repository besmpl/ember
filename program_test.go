package ember_test

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/besmpl/ember"
)

func TestLoadProgramRejectsInvalidOptions(t *testing.T) {
	validLoader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `return {}`,
		},
	}
	validEntrypoint := ember.Entrypoint{Name: "main", Module: ember.LogicalModule("game/init")}

	tests := []struct {
		name       string
		loader     ember.ModuleLoader
		options    ember.ProgramOptions
		wantError  string
		wantReport []string
	}{
		{
			name:      "nil loader",
			loader:    nil,
			options:   ember.ProgramOptions{Entrypoints: []ember.Entrypoint{validEntrypoint}},
			wantError: "nil module loader",
		},
		{
			name:      "no entrypoints",
			loader:    validLoader,
			options:   ember.ProgramOptions{},
			wantError: "no entrypoints",
		},
		{
			name:      "negative parallelism",
			loader:    validLoader,
			options:   ember.ProgramOptions{Entrypoints: []ember.Entrypoint{validEntrypoint}, Parallelism: -1},
			wantError: "negative parallelism",
		},
		{
			name:      "empty entrypoint name",
			loader:    validLoader,
			options:   ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Module: ember.LogicalModule("game/init")}}},
			wantError: "empty entrypoint name",
		},
		{
			name:   "duplicate entrypoint name",
			loader: validLoader,
			options: ember.ProgramOptions{Entrypoints: []ember.Entrypoint{
				validEntrypoint,
				{Name: "main", Module: ember.LogicalModule("game/other")},
			}},
			wantError:  `duplicate entrypoint "main"`,
			wantReport: []string{"main=logical:game/init"},
		},
		{
			name:      "unconstructed module id",
			loader:    validLoader,
			options:   ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Name: "main"}}},
			wantError: "unknown kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, report, err := ember.LoadProgram(context.Background(), tt.loader, tt.options)
			if err == nil {
				t.Fatal("LoadProgram succeeded, want error")
			}
			if program != nil {
				t.Fatal("LoadProgram returned program for invalid options")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("LoadProgram error is %q, want %q", err, tt.wantError)
			}
			if tt.wantReport != nil {
				got := make([]string, 0, len(report.Entrypoints))
				for _, entrypoint := range report.Entrypoints {
					got = append(got, entrypoint.Name+"="+entrypoint.Module.String())
				}
				assertStrings(t, got, tt.wantReport)
			}
		})
	}
}

func TestLoadProgramBuildsSharedGraphForEntrypoints(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/server/init": `local config = require("../shared/config") return config`,
			"logical:game/client/init": `local config = require("../shared/config") return config`,
			"logical:game/shared/config": `
return {value = 1}
`,
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}

	assertEntrypointReports(t, report.Entrypoints, []string{"server", "client"})
	assertModuleReports(t, report.Modules, []string{
		"logical:game/client/init",
		"logical:game/server/init",
		"logical:game/shared/config",
	})
	if got := loader.loads["logical:game/shared/config"]; got != 1 {
		t.Fatalf("shared module loaded %d times, want once", got)
	}
}

func TestLoadProgramReportsDeterministicCyclePath(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/a": `return require("./b")`,
			"logical:game/b": `return require("./c")`,
			"logical:game/c": `return require("./a")`,
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/a")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned hard error: %v", err)
	}
	if program != nil {
		t.Fatal("LoadProgram returned program for cyclic graph")
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("LoadProgram returned %d diagnostics, want 1", len(report.Diagnostics))
	}
	diagnostic := report.Diagnostics[0]
	if diagnostic.Code != "module-cycle" {
		t.Fatalf("diagnostic code is %q, want module-cycle", diagnostic.Code)
	}
	wantPath := []string{
		"logical:game/a",
		"logical:game/b",
		"logical:game/c",
		"logical:game/a",
	}
	assertStrings(t, diagnostic.Path, wantPath)
}

func TestLoadProgramLoaderErrorIncludesModuleIdentity(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `return require("./missing")`,
		},
	}

	_, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err == nil {
		t.Fatal("LoadProgram succeeded, want loader error")
	}
	if !strings.Contains(err.Error(), "logical:game/missing") {
		t.Fatalf("LoadProgram error is %q, want missing module identity", err)
	}
}

func TestLoadProgramSupportsHostModuleNamespace(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `local clock = require("host:clock") return {clock = clock}`,
			"host:clock":        `return {now = 1}`,
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}

	assertModuleReports(t, report.Modules, []string{
		"host:clock",
		"logical:game/init",
	})
	if got := loader.loads["host:clock"]; got != 1 {
		t.Fatalf("host module loaded %d times, want once", got)
	}
}

func TestLoadProgramParseErrorIncludesModuleIdentity(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/good": `return {}`,
			"logical:game/bad":  `local value =`,
		},
	}

	_, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "good", Module: ember.LogicalModule("game/good")},
			{Name: "bad", Module: ember.LogicalModule("game/bad")},
		},
		Parallelism: 2,
	})
	if err == nil {
		t.Fatal("LoadProgram succeeded, want parse error")
	}
	if !strings.Contains(err.Error(), "logical:game/bad") {
		t.Fatalf("LoadProgram error is %q, want bad module identity", err)
	}
}

func TestLoadProgramParallelismLoadsEntrypointsConcurrentlyWithStableReport(t *testing.T) {
	loader := newParallelProgramTestLoader(map[string]string{
		"logical:game/server/init": "return {name = \"server\"}",
		"logical:game/client/init": "return {name = \"client\"}",
	}, 2)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	program, report, err := ember.LoadProgram(ctx, loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}

	assertModuleReports(t, report.Modules, []string{
		"logical:game/client/init",
		"logical:game/server/init",
	})
}

func TestLoadProgramDefaultParallelismUsesGOMAXPROCS(t *testing.T) {
	previous := goruntime.GOMAXPROCS(2)
	defer goruntime.GOMAXPROCS(previous)

	loader := newParallelProgramTestLoader(map[string]string{
		"logical:game/server/init": "return {name = \"server\"}",
		"logical:game/client/init": "return {name = \"client\"}",
	}, 2)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	program, report, err := ember.LoadProgram(ctx, loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}

	assertModuleReports(t, report.Modules, []string{
		"logical:game/client/init",
		"logical:game/server/init",
	})
}

func TestLoadProgramParallelismMatchesSequentialReport(t *testing.T) {
	sources := map[string]string{
		"logical:game/server/init": `
local config = require("../shared/config")
return {config = config}
`,
		"logical:game/client/init": `
local config = require("../shared/config")
return {config = config}
`,
		"logical:game/shared/config": `
return {value = 1}
`,
	}
	options := ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Check: true,
	}

	sequentialLoader := &programTestLoader{sources: sources}
	sequentialProgram, sequentialReport, err := ember.LoadProgram(context.Background(), sequentialLoader, ember.ProgramOptions{
		Entrypoints: options.Entrypoints,
		Check:       options.Check,
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("sequential LoadProgram returned error: %v", err)
	}
	if sequentialProgram == nil {
		t.Fatal("sequential LoadProgram returned nil program")
	}

	parallelLoader := &programTestLoader{sources: sources}
	parallelProgram, parallelReport, err := ember.LoadProgram(context.Background(), parallelLoader, ember.ProgramOptions{
		Entrypoints: options.Entrypoints,
		Check:       options.Check,
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("parallel LoadProgram returned error: %v", err)
	}
	if parallelProgram == nil {
		t.Fatal("parallel LoadProgram returned nil program")
	}

	assertLoadReportsEqual(t, parallelReport, sequentialReport)
}

func TestLoadProgramCheckDiagnosticsAreSortedByModuleThenRange(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/server/init": `
--!strict
local value: number = "oops"
return {}
`,
			"logical:game/client/init": `
--!strict


local player: {name: string} = {name = "ember"}
return player.score
`,
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Check:       true,
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}
	if len(report.Diagnostics) != 2 {
		t.Fatalf("LoadProgram returned %d diagnostics, want 2: %#v", len(report.Diagnostics), report.Diagnostics)
	}

	first := report.Diagnostics[0]
	second := report.Diagnostics[1]
	if first.Code != "unknown-property" || !strings.Contains(first.Message, "score") {
		t.Fatalf("first diagnostic is %#v, want client unknown-property for score", first)
	}
	if second.Code != "type-mismatch" || !strings.Contains(second.Message, "number") || !strings.Contains(second.Message, "string") {
		t.Fatalf("second diagnostic is %#v, want server number/string mismatch", second)
	}
}

func TestLoadProgramCheckSummariesIncludeRequiredModuleFacts(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init":   "--!strict\nlocal shared = require(\"./shared\") return shared",
			"logical:game/shared": "--!strict\nreturn {count = 2}",
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Check:       true,
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("LoadProgram returned diagnostics %#v, want none", report.Diagnostics)
	}

	initSummary := moduleReport(t, report.Modules, "logical:game/init").Summary
	if len(initSummary.Dependencies) != 1 {
		t.Fatalf("init summary dependencies are %#v, want one shared dependency", initSummary.Dependencies)
	}
	dependency := initSummary.Dependencies[0]
	if dependency.Key != "logical:game/shared" || dependency.Kind != ember.ModuleDependencyLogical || dependency.Path != "game/shared" {
		t.Fatalf("init dependency is %#v, want logical game/shared", dependency)
	}
	sharedSource := ember.Source{Name: "logical:game/shared", Text: "--!strict\nreturn {count = 2}"}
	if dependency.InvalidationHash == "" {
		t.Fatal("init dependency hash is empty")
	}
	if dependency.InvalidationHash == initSummary.InvalidationHash {
		t.Fatal("init dependency hash matched root hash, want required module hash")
	}

	value := moduleSummaryExport(t, initSummary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("init return export kind is %q, want table", value.Type.Kind)
	}
	if len(value.Type.Properties) != 1 || value.Type.Properties[0].Name != "count" || value.Type.Properties[0].Type.Display != "number" {
		t.Fatalf("init return properties are %#v, want count: number from shared module", value.Type.Properties)
	}
	sharedSummary := moduleReport(t, report.Modules, "logical:game/shared").Summary
	if sharedSummary.InvalidationHash == "" || sharedSummary.InvalidationHash != dependency.InvalidationHash {
		t.Fatalf("shared summary hash is %q, want dependency hash %q for %s", sharedSummary.InvalidationHash, dependency.InvalidationHash, sharedSource.Name)
	}
}

func TestLoadProgramCheckSummariesPropagateRequiredReturnFields(t *testing.T) {
	tests := []struct {
		name string
		root string
	}{
		{
			name: "return field",
			root: "--!strict\nlocal shared = require(\"./shared\") return shared.count",
		},
		{
			name: "field local",
			root: "--!strict\nlocal shared = require(\"./shared\") local count = shared.count return count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &programTestLoader{
				sources: map[string]string{
					"logical:game/init":   tt.root,
					"logical:game/shared": "--!strict\nreturn {count = 2}",
				},
			}

			program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
				Check:       true,
				Parallelism: 1,
			})
			if err != nil {
				t.Fatalf("LoadProgram returned error: %v", err)
			}
			if program == nil {
				t.Fatal("LoadProgram returned nil program")
			}

			value := moduleSummaryExport(t, moduleReport(t, report.Modules, "logical:game/init").Summary.Exports, "return", ember.ModuleExportValue)
			if value.Type.Display != "number" {
				t.Fatalf("root return type is %#v, want number from shared.count", value.Type)
			}
		})
	}
}

func TestLoadProgramCheckSummariesDoNotConsumeUntrustedDependency(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": "--!strict\nlocal shared = require(\"./shared\") return shared.count",
			"logical:game/shared": `
--!strict
local config: {count: number} = {count = "bad"}
return {count = 2}
`,
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Check:       true,
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "type-mismatch" {
		t.Fatalf("LoadProgram diagnostics are %#v, want shared type mismatch", report.Diagnostics)
	}

	shared := moduleReport(t, report.Modules, "logical:game/shared").Summary
	if len(shared.Diagnostics) != 1 || shared.Diagnostics[0].Code != "type-mismatch" {
		t.Fatalf("shared summary diagnostics are %#v, want type mismatch", shared.Diagnostics)
	}
	value := moduleSummaryExport(t, moduleReport(t, report.Modules, "logical:game/init").Summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Display != "unknown" {
		t.Fatalf("root return type is %#v, want unknown because dependency is untrusted", value.Type)
	}
}

func TestLoadProgramCheckSummariesIncludeHostDependencyFacts(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": "--!strict\nlocal clock = require(\"host:clock\") return clock",
			"host:clock":        "--!strict\nreturn {now = 1}",
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Check:       true,
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}

	summary := moduleReport(t, report.Modules, "logical:game/init").Summary
	if len(summary.Dependencies) != 1 {
		t.Fatalf("root summary dependencies are %#v, want one host dependency", summary.Dependencies)
	}
	dependency := summary.Dependencies[0]
	if dependency.Key != "host:clock" || dependency.Kind != ember.ModuleDependencyHost || dependency.Path != "clock" {
		t.Fatalf("root dependency is %#v, want host clock", dependency)
	}
	value := moduleSummaryExport(t, summary.Exports, "return", ember.ModuleExportValue)
	if value.Type.Kind != ember.TypeSummaryTable {
		t.Fatalf("root return kind is %q, want host table", value.Type.Kind)
	}
}

func TestLoadProgramParallelCompileErrorIncludesStableModuleIdentity(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/server/init": `
return {}
`,
			"logical:game/client/init": `
break
return {}
`,
		},
	}

	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Parallelism: 2,
	})
	if err == nil {
		t.Fatal("LoadProgram succeeded, want compile error")
	}
	if program != nil {
		t.Fatal("LoadProgram returned program after compile error")
	}
	if !strings.Contains(err.Error(), "load program: compile logical:game/client/init") {
		t.Fatalf("LoadProgram error is %q, want client module identity", err)
	}
}

func TestLoadProgramParallelismLoadsSharedDependencyOnce(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/server/init": `local config = require("../shared/config") return config`,
			"logical:game/client/init": `local config = require("../shared/config") return config`,
			"logical:game/shared/config": `
return {value = 1}
`,
		},
	}

	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil {
		t.Fatal("LoadProgram returned nil program")
	}

	assertModuleReports(t, report.Modules, []string{
		"logical:game/client/init",
		"logical:game/server/init",
		"logical:game/shared/config",
	})
	if got := loader.loads["logical:game/shared/config"]; got != 1 {
		t.Fatalf("shared module loaded %d times, want once", got)
	}
}

func TestLoadProgramParallelismReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loader := &cancelingProgramTestLoader{cancel: cancel}

	program, _, err := ember.LoadProgram(ctx, loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Parallelism: 2,
	})
	if err == nil {
		t.Fatal("LoadProgram succeeded, want context cancellation")
	}
	if program != nil {
		t.Fatal("LoadProgram returned program after cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadProgram error is %q, want context.Canceled", err)
	}
}

func TestRuntimeRunHookSharesRequireCacheAcrossEntrypoints(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/server/init": `
local counter = require("../shared/counter")
return {
	startup = function()
		counter.bump("server")
		return nil
	end,
}
`,
			"logical:game/client/init": `
local counter = require("../shared/counter")
return {
	startup = function()
		counter.bump("client")
		return nil
	end,
}
`,
			"logical:game/shared/counter": `
record("load")
local state = {count = 0}
return {
	bump = function(name)
		state.count = state.count + 1
		record(name .. ":" .. state.count)
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var records []string
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, _ ember.HostCall) (map[string]ember.Value, error) {
			return map[string]ember.Value{
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					value, ok := args[0].String()
					if !ok {
						t.Fatalf("record arg is %s, want string", args[0].Kind())
					}
					records = append(records, value)
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	report, err := runtime.RunHook(context.Background(), "startup")
	if err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}

	assertStrings(t, records, []string{"load", "server:1", "client:2"})
	assertHookCalls(t, report.Calls, []hookCallWant{
		{entrypoint: "server", called: true},
		{entrypoint: "client", called: true},
	})
}

func TestRuntimeRunHookCachesNilRequiredModuleResult(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
local first = require("./empty")
local second = require("./empty")
return {
	startup = function()
		record(type(first))
		record(type(second))
		return nil
	end,
}
`,
			"logical:game/empty": `
record("load")
return
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var records []string
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, _ ember.HostCall) (map[string]ember.Value, error) {
			return map[string]ember.Value{
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					value, ok := args[0].String()
					if !ok {
						t.Fatalf("record arg is %s, want string", args[0].Kind())
					}
					records = append(records, value)
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	assertStrings(t, records, []string{"load", "nil", "nil"})
}

func TestProgramNewRuntimeCreatesIsolatedRuntimeState(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
local counter = require("./counter")
return {
	startup = function()
		record("startup:" .. counter.bump())
		return nil
	end,
	update = function()
		record("update:" .. counter.bump())
		return nil
	end,
}
`,
			"logical:game/counter": `
record("load")
local count = 0
return {
	bump = function()
		count = count + 1
		return count
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var firstRecords []string
	firstRuntime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: recordingRuntimeHost(t, &firstRecords),
	})
	if err != nil {
		t.Fatalf("first NewRuntime returned error: %v", err)
	}
	var secondRecords []string
	secondRuntime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: recordingRuntimeHost(t, &secondRecords),
	})
	if err != nil {
		t.Fatalf("second NewRuntime returned error: %v", err)
	}

	if _, err := firstRuntime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("first startup returned error: %v", err)
	}
	if _, err := firstRuntime.RunHook(context.Background(), "update"); err != nil {
		t.Fatalf("first update returned error: %v", err)
	}
	if _, err := secondRuntime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("second startup returned error: %v", err)
	}
	if _, err := secondRuntime.RunHook(context.Background(), "update"); err != nil {
		t.Fatalf("second update returned error: %v", err)
	}

	assertStrings(t, firstRecords, []string{"load", "startup:1", "update:2"})
	assertStrings(t, secondRecords, []string{"load", "startup:1", "update:2"})
}

func TestRuntimeRunHookRejectsActiveLoadingRequireCycle(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
local shared = require("./shared")
return {
	startup = function()
		return nil
	end,
}
`,
			"logical:game/shared": `
local path = "./init"
return require(path)
`,
		},
	}
	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("LoadProgram returned diagnostics %#v, want runtime cycle only", report.Diagnostics)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	_, err = runtime.RunHook(context.Background(), "startup")
	if err == nil {
		t.Fatal("RunHook succeeded, want active-loading cycle error")
	}
	if !strings.Contains(err.Error(), "active-loading") ||
		!strings.Contains(err.Error(), "logical:game/init") ||
		!strings.Contains(err.Error(), "logical:game/shared") {
		t.Fatalf("RunHook error is %q, want active-loading cycle path", err)
	}
}

func TestRuntimeCloseIsRepeatSafeAndRejectsLaterHooks(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = function()
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
	_, err = runtime.RunHook(context.Background(), "startup")
	if err == nil {
		t.Fatal("RunHook after Close succeeded, want error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("RunHook after Close error is %q, want closed", err)
	}
}

func TestRuntimeRunHookReportsMissingHooksAsSkipped(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = function()
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	report, err := runtime.RunHook(context.Background(), "update")
	if err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}

	assertHookCalls(t, report.Calls, []hookCallWant{
		{entrypoint: "main", skipped: true},
	})
}

func TestRuntimeRunHookFollowsDeclaredEntrypointOrder(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/z-last": `
return {
	startup = function()
		record("first")
		return nil
	end,
}
`,
			"logical:game/a-first": `
return {
	startup = function()
		record("second")
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "first", Module: ember.LogicalModule("game/z-last")},
			{Name: "second", Module: ember.LogicalModule("game/a-first")},
		},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var records []string
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: recordingRuntimeHost(t, &records),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	report, err := runtime.RunHook(context.Background(), "startup")
	if err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}

	assertStrings(t, records, []string{"first", "second"})
	assertHookCalls(t, report.Calls, []hookCallWant{
		{entrypoint: "first", called: true},
		{entrypoint: "second", called: true},
	})
}

func TestRuntimeRunHookRejectsNonTableEntrypointExport(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return 42
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	_, err = runtime.RunHook(context.Background(), "startup")
	if err == nil {
		t.Fatal("RunHook succeeded, want non-table entrypoint export error")
	}
	if !strings.Contains(err.Error(), "main") ||
		!strings.Contains(err.Error(), "number") ||
		!strings.Contains(err.Error(), "table") {
		t.Fatalf("RunHook error is %q, want entrypoint export type context", err)
	}
}

func TestRuntimeRunHookRejectsNonCallableHookWithContext(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = 12,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	_, err = runtime.RunHook(context.Background(), "startup")
	if err == nil {
		t.Fatal("RunHook succeeded, want non-callable hook error")
	}
	if !strings.Contains(err.Error(), "main.startup") {
		t.Fatalf("RunHook error is %q, want entrypoint hook context", err)
	}
}

func TestRuntimeRunHookPreservesEntrypointStateAcrossHooks(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
local state = {ready = false, frames = 0}
return {
	startup = function()
		state.ready = true
		record("startup:" .. tostring(state.ready))
		return nil
	end,
	update = function()
		if state.ready then
			state.frames = state.frames + 1
		end
		record("update:" .. state.frames)
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var records []string
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, _ ember.HostCall) (map[string]ember.Value, error) {
			return map[string]ember.Value{
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					value, ok := args[0].String()
					if !ok {
						t.Fatalf("record arg is %s, want string", args[0].Kind())
					}
					records = append(records, value)
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook startup returned error: %v", err)
	}
	if _, err := runtime.RunHook(context.Background(), "update"); err != nil {
		t.Fatalf("RunHook first update returned error: %v", err)
	}
	if _, err := runtime.RunHook(context.Background(), "update"); err != nil {
		t.Fatalf("RunHook second update returned error: %v", err)
	}

	assertStrings(t, records, []string{
		"startup:true",
		"update:1",
		"update:2",
	})
}

func TestRuntimeHostReceivesEntrypointLoadAndHookCalls(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
record("load-global")
return {
	startup = function()
		record("hook-global")
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var calls []string
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
			hook := call.Hook
			if hook == "" {
				hook = "<load>"
			}
			calls = append(calls, call.Entrypoint+":"+call.Module.String()+":"+hook)
			return map[string]ember.Value{
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}

	assertStrings(t, calls, []string{
		"main:logical:game/init:<load>",
		"main:logical:game/init:startup",
	})
}

func TestRuntimeHostGlobalOverridesBaseGlobal(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = function()
		record(type(12))
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var records []string
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
			if call.Hook == "" {
				return nil, nil
			}
			return map[string]ember.Value{
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					value, ok := args[0].String()
					if !ok {
						t.Fatalf("record arg is %s, want string", args[0].Kind())
					}
					records = append(records, value)
					return nil, nil
				}),
				"type": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					return []ember.Value{ember.StringValue("host-type")}, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	assertStrings(t, records, []string{"host-type"})
}

func TestRuntimeHostGlobalsStayLocalToEntrypoint(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/server/init": `
return {
	startup = function()
		record(scriptName)
		return nil
	end,
}
`,
			"logical:game/client/init": `
return {
	startup = function()
		record(scriptName)
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "server", Module: ember.LogicalModule("game/server/init")},
			{Name: "client", Module: ember.LogicalModule("game/client/init")},
		},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var records []string
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
			if call.Hook == "" {
				return nil, nil
			}
			return map[string]ember.Value{
				"scriptName": ember.StringValue(call.Entrypoint),
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					value, ok := args[0].String()
					if !ok {
						t.Fatalf("record arg is %s, want string", args[0].Kind())
					}
					records = append(records, value)
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	report, err := runtime.RunHook(context.Background(), "startup")
	if err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}

	assertStrings(t, records, []string{"server", "client"})
	assertHookCalls(t, report.Calls, []hookCallWant{
		{entrypoint: "server", called: true},
		{entrypoint: "client", called: true},
	})
}

func TestRuntimeContextHostFuncReceivesHookContextCancellation(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = function()
		checkContext()
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sawCanceled bool
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
			if call.Hook == "" {
				return nil, nil
			}
			return map[string]ember.Value{
				"checkContext": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
					cancel()
					sawCanceled = ctx.Err() == context.Canceled
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(ctx, "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	if !sawCanceled {
		t.Fatal("context-aware host callback did not observe canceled hook context")
	}
}

func TestRuntimeContextHostFuncReceivesContextDuringRequiredModuleLoad(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
local shared = require("./shared")
return {
	startup = function()
		return nil
	end,
}
`,
			"logical:game/shared": `
checkContext()
return {}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sawCanceled bool
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, _ ember.HostCall) (map[string]ember.Value, error) {
			return map[string]ember.Value{
				"checkContext": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
					cancel()
					sawCanceled = ctx.Err() == context.Canceled
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(ctx, "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	if !sawCanceled {
		t.Fatal("context-aware host callback in required module did not observe canceled context")
	}
}

func TestRuntimeCallbackCanBeStoredAndCalledAfterHook(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
local count = 0

return {
	startup = function()
		connect(function(amount)
			count = count + amount
			record(count)
			return count, "ok"
		end)
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var callback ember.Callback
	var records []float64
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
			if call.Hook == "" {
				return nil, nil
			}
			return map[string]ember.Value{
				"connect": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
					if len(args) != 1 {
						t.Fatalf("connect received %d args, want 1", len(args))
					}
					captured, err := ember.CaptureCallback(ctx, args[0])
					if err != nil {
						return nil, err
					}
					callback = captured
					return nil, nil
				}),
				"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
					if len(args) != 1 {
						t.Fatalf("record received %d args, want 1", len(args))
					}
					value, ok := args[0].Number()
					if !ok {
						t.Fatalf("record received %s, want number", args[0].Kind())
					}
					records = append(records, value)
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}

	results, err := callback.Call(context.Background(), ember.NumberValue(2))
	if err != nil {
		t.Fatalf("Callback.Call returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Callback.Call returned %v, want number and string", results)
	}
	assertValueNumbers(t, results[:1], []float64{2})
	assertValueStrings(t, results[1:], []string{"ok"})

	results, err = callback.Call(context.Background(), ember.NumberValue(3))
	if err != nil {
		t.Fatalf("second Callback.Call returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("second Callback.Call returned %v, want number and string", results)
	}
	assertValueNumbers(t, results[:1], []float64{5})
	assertValueStrings(t, results[1:], []string{"ok"})
	assertFloat64s(t, records, []float64{2, 5})
}

func TestRuntimeCallbackUsesCapturedModuleRequireContext(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = function()
		connect(function()
			local shared = require("./shared")
			return shared.name
		end)
	end,
}
`,
			"logical:game/shared": `
return {
	name = "shared module",
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}

	var callback ember.Callback
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Host: ember.RuntimeHostFunc(func(_ context.Context, call ember.HostCall) (map[string]ember.Value, error) {
			if call.Hook == "" {
				return nil, nil
			}
			return map[string]ember.Value{
				"connect": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
					captured, err := ember.CaptureCallback(ctx, args[0])
					if err != nil {
						return nil, err
					}
					callback = captured
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	results, err := callback.Call(context.Background())
	if err != nil {
		t.Fatalf("Callback.Call returned error: %v", err)
	}
	assertValueStrings(t, results, []string{"shared module"})
}

func TestRuntimeInstructionBudgetExhaustionDoesNotPoisonNextHook(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = function()
		while true do
		end
		return nil
	end,
	update = function()
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{MaxInstructions: 20})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	_, err = runtime.RunHook(context.Background(), "startup")
	if err == nil {
		t.Fatal("RunHook startup succeeded, want instruction budget error")
	}
	if !strings.Contains(err.Error(), "instruction budget") {
		t.Fatalf("RunHook startup error is %q, want instruction budget", err)
	}

	report, err := runtime.RunHook(context.Background(), "update")
	if err != nil {
		t.Fatalf("RunHook update returned error after budget exhaustion: %v", err)
	}
	assertHookCalls(t, report.Calls, []hookCallWant{
		{entrypoint: "main", called: true},
	})
}

func TestRuntimeInstructionBudgetCoversCoroutineResume(t *testing.T) {
	loader := &programTestLoader{
		sources: map[string]string{
			"logical:game/init": `
return {
	startup = function()
		local co = coroutine.create(function()
			while true do
			end
			return nil
		end)
		local ok, message = coroutine.resume(co)
		if ok then
			error("coroutine unexpectedly completed")
		end
		error(message)
		return nil
	end,
}
`,
		},
	}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{MaxInstructions: 30})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}

	_, err = runtime.RunHook(context.Background(), "startup")
	if err == nil {
		t.Fatal("RunHook succeeded, want coroutine budget error")
	}
	if !strings.Contains(err.Error(), "instruction budget") {
		t.Fatalf("RunHook error is %q, want instruction budget", err)
	}
}

type programTestLoader struct {
	sources map[string]string
	mu      sync.Mutex
	loads   map[string]int
}

type parallelProgramTestLoader struct {
	sources map[string]string
	target  int

	mu      sync.Mutex
	started int
	release chan struct{}
	once    sync.Once
}

type cancelingProgramTestLoader struct {
	cancel context.CancelFunc
	once   sync.Once
}

func newParallelProgramTestLoader(sources map[string]string, target int) *parallelProgramTestLoader {
	return &parallelProgramTestLoader{
		sources: sources,
		target:  target,
		release: make(chan struct{}),
	}
}

func (l *cancelingProgramTestLoader) LoadModule(ctx context.Context, id ember.ModuleID) (ember.Source, error) {
	l.once.Do(l.cancel)
	<-ctx.Done()
	return ember.Source{}, ctx.Err()
}

func (l *parallelProgramTestLoader) LoadModule(ctx context.Context, id ember.ModuleID) (ember.Source, error) {
	l.mu.Lock()
	l.started++
	if l.started == l.target {
		l.once.Do(func() {
			close(l.release)
		})
	}
	l.mu.Unlock()

	select {
	case <-l.release:
	case <-ctx.Done():
		return ember.Source{}, ctx.Err()
	}

	key := id.String()
	text, ok := l.sources[key]
	if !ok {
		return ember.Source{}, fmt.Errorf("missing source")
	}
	return ember.Source{Name: key, Text: text}, nil
}

func (l *programTestLoader) LoadModule(_ context.Context, id ember.ModuleID) (ember.Source, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.loads == nil {
		l.loads = make(map[string]int)
	}
	key := id.String()
	l.loads[key]++
	text, ok := l.sources[key]
	if !ok {
		return ember.Source{}, fmt.Errorf("missing source")
	}
	return ember.Source{Name: key, Text: text}, nil
}

func recordingRuntimeHost(t *testing.T, records *[]string) ember.RuntimeHost {
	t.Helper()
	return ember.RuntimeHostFunc(func(_ context.Context, _ ember.HostCall) (map[string]ember.Value, error) {
		return map[string]ember.Value{
			"record": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
				value, ok := args[0].String()
				if !ok {
					t.Fatalf("record arg is %s, want string", args[0].Kind())
				}
				*records = append(*records, value)
				return nil, nil
			}),
		}, nil
	})
}

func assertEntrypointReports(t *testing.T, got []ember.EntrypointReport, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("entrypoint reports are %#v, want %v", got, want)
	}
	for i, item := range got {
		if item.Name != want[i] {
			t.Fatalf("entrypoint report %d is %q, want %q", i, item.Name, want[i])
		}
	}
}

func assertModuleReports(t *testing.T, got []ember.ModuleReport, want []string) {
	t.Helper()
	keys := make([]string, 0, len(got))
	for _, item := range got {
		keys = append(keys, item.Module.String())
	}
	assertStrings(t, keys, want)
}

func moduleReport(t *testing.T, reports []ember.ModuleReport, module string) ember.ModuleReport {
	t.Helper()
	for _, report := range reports {
		if report.Module.String() == module {
			return report
		}
	}
	t.Fatalf("module reports are %#v, want %s", reports, module)
	return ember.ModuleReport{}
}

func moduleSummaryExport(t *testing.T, exports []ember.ModuleExport, name string, kind ember.ModuleExportKind) ember.ModuleExport {
	t.Helper()
	for _, item := range exports {
		if item.Name == name && item.Kind == kind {
			return item
		}
	}
	t.Fatalf("summary exports are %#v, want %s %s export", exports, kind, name)
	return ember.ModuleExport{}
}

func assertLoadReportsEqual(t *testing.T, got ember.LoadReport, want ember.LoadReport) {
	t.Helper()
	gotEntrypoints := make([]string, 0, len(got.Entrypoints))
	for _, item := range got.Entrypoints {
		gotEntrypoints = append(gotEntrypoints, item.Name+"="+item.Module.String())
	}
	wantEntrypoints := make([]string, 0, len(want.Entrypoints))
	for _, item := range want.Entrypoints {
		wantEntrypoints = append(wantEntrypoints, item.Name+"="+item.Module.String())
	}
	assertStrings(t, gotEntrypoints, wantEntrypoints)

	gotModules := make([]string, 0, len(got.Modules))
	for _, item := range got.Modules {
		gotModules = append(gotModules, item.Module.String()+"="+item.SourceName)
	}
	wantModules := make([]string, 0, len(want.Modules))
	for _, item := range want.Modules {
		wantModules = append(wantModules, item.Module.String()+"="+item.SourceName)
	}
	assertStrings(t, gotModules, wantModules)

	gotDiagnostics := make([]string, 0, len(got.Diagnostics))
	for _, item := range got.Diagnostics {
		gotDiagnostics = append(gotDiagnostics, fmt.Sprintf("%s:%d:%d:%s:%v", item.Code, item.Start, item.End, item.Message, item.Path))
	}
	wantDiagnostics := make([]string, 0, len(want.Diagnostics))
	for _, item := range want.Diagnostics {
		wantDiagnostics = append(wantDiagnostics, fmt.Sprintf("%s:%d:%d:%s:%v", item.Code, item.Start, item.End, item.Message, item.Path))
	}
	assertStrings(t, gotDiagnostics, wantDiagnostics)
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("strings are %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("strings are %v, want %v", got, want)
		}
	}
}

func assertValueNumbers(t *testing.T, got []ember.Value, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("number values are %v, want %v", got, want)
	}
	for i, item := range got {
		number, ok := item.Number()
		if !ok || number != want[i] {
			t.Fatalf("number values are %v, want %v", got, want)
		}
	}
}

func assertValueStrings(t *testing.T, got []ember.Value, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("string values are %v, want %v", got, want)
	}
	for i, item := range got {
		text, ok := item.String()
		if !ok || text != want[i] {
			t.Fatalf("string values are %v, want %v", got, want)
		}
	}
}

func assertFloat64s(t *testing.T, got []float64, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("float64s are %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("float64s are %v, want %v", got, want)
		}
	}
}

type hookCallWant struct {
	entrypoint string
	called     bool
	skipped    bool
}

func assertHookCalls(t *testing.T, got []ember.HookCallReport, want []hookCallWant) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("hook calls are %#v, want %#v", got, want)
	}
	for i, item := range got {
		if item.Entrypoint != want[i].entrypoint ||
			item.Called != want[i].called ||
			item.Skipped != want[i].skipped {
			t.Fatalf("hook call %d is %#v, want %#v", i, item, want[i])
		}
	}
}
