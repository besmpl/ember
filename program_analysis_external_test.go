package ember_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/besmpl/ember"
)

func TestLoadProgramReportsStructuredSourceFailures(t *testing.T) {
	tests := []struct {
		name       string
		sources    map[string]string
		wantSource string
	}{
		{
			name: "entrypoint",
			sources: map[string]string{
				"logical:game/init": `return {`,
			},
			wantSource: "logical:game/init",
		},
		{
			name: "required module",
			sources: map[string]string{
				"logical:game/init": `return require("./broken")`,
				"logical:game/broken": `local value = "unterminated
return value`,
			},
			wantSource: "logical:game/broken",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := &programTestLoader{sources: test.sources}
			_, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
			})
			var sourceErr *ember.SourceError
			if !errors.As(err, &sourceErr) {
				t.Fatalf("LoadProgram error = %v, want *SourceError", err)
			}
			if sourceErr.Source.Name != test.wantSource {
				t.Fatalf("source name = %q, want %q", sourceErr.Source.Name, test.wantSource)
			}
			if sourceErr.Code != "syntax-error" {
				t.Fatalf("source code = %q, want syntax-error", sourceErr.Code)
			}
			if sourceErr.Start < 0 || sourceErr.End < sourceErr.Start || sourceErr.End > len(sourceErr.Source.Text) {
				t.Fatalf("source range = [%d,%d), text length %d", sourceErr.Start, sourceErr.End, len(sourceErr.Source.Text))
			}
			if sourceErr.Message == "" || sourceErr.Unwrap() == nil {
				t.Fatalf("structured source error = %#v", sourceErr)
			}
		})
	}
}

func TestLoadProgramAnalysisUsesHostAndTrustedDependencyFactsDeterministically(t *testing.T) {
	sources := map[string]string{
		"logical:game/init": `--!strict
local dependency = require("./dependency")
local hostNumber: number = HostNumber
local wrong: string = dependency
return {hostNumber = hostNumber, wrong = wrong}`,
		"logical:game/dependency": `--!strict
local hostNumber: number = HostNumber
local result: number = hostNumber
return result`,
	}
	load := func(parallelism int) ember.LoadReport {
		t.Helper()
		loader := &programTestLoader{sources: sources}
		_, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
			Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
			Check:       true,
			Parallelism: parallelism,
			Analysis: ember.AnalysisConfig{Globals: map[string]ember.TypeSummary{
				"HostNumber": {Kind: ember.TypeSummaryName, Display: "number"},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	sequential := load(1)
	parallel := load(8)
	if !reflect.DeepEqual(sequential, parallel) {
		t.Fatalf("sequential/parallel reports differ:\nsequential %#v\nparallel %#v", sequential, parallel)
	}
	if len(sequential.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one dependency return mismatch", sequential.Diagnostics)
	}
	diagnostic := sequential.Diagnostics[0]
	if diagnostic.Module.String() != "logical:game/init" || diagnostic.SourceName != "logical:game/init" {
		t.Fatalf("diagnostic identity = module %q source %q", diagnostic.Module.String(), diagnostic.SourceName)
	}
	if diagnostic.Code != "type-mismatch" || sources["logical:game/init"][diagnostic.Start:diagnostic.End] != "dependency" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestAnalyzerProjectsLiteralFactoryOverloadsAndTableReturns(t *testing.T) {
	number := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "number"}
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	receiver := ember.TypeSummary{Kind: ember.TypeSummaryTable, Display: "table"}
	method := func(params []ember.TypeSummary, result ember.TypeSummary) ember.TypeSummary {
		return ember.TypeSummary{
			Kind:       ember.TypeSummaryFunction,
			Display:    "function",
			Params:     params,
			ReturnPack: ember.TypePackSummary{Head: []ember.TypeSummary{result}},
		}
	}
	baseClass := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{
			{Name: "Name", Access: "read", Type: stringType},
			{Name: "Ping", Type: method([]ember.TypeSummary{receiver, number}, stringType)},
		},
	}
	metatable := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{
			{Name: "__index", Type: baseClass},
		},
	}
	widget := ember.TypeSummary{
		Kind:       ember.TypeSummaryTable,
		Display:    "table",
		Properties: []ember.TablePropertySummary{{Name: "Enabled", Type: ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "boolean"}}},
		Metatable:  &metatable,
	}
	players := ember.TypeSummary{
		Kind:       ember.TypeSummaryTable,
		Display:    "table",
		Properties: []ember.TablePropertySummary{{Name: "Count", Access: "read", Type: number}},
	}
	overload := func(literal string, result ember.TypeSummary, methodCall bool) ember.TypeSummary {
		params := []ember.TypeSummary{{Kind: ember.TypeSummarySingleton, Display: `"` + literal + `"`}}
		if methodCall {
			params = append([]ember.TypeSummary{receiver}, params...)
		}
		return method(params, result)
	}
	factory := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{{
			Name: "new",
			Type: ember.TypeSummary{
				Kind:  ember.TypeSummaryIntersection,
				Types: []ember.TypeSummary{overload("Widget", widget, false), overload("Players", players, false)},
			},
		}},
	}
	root := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{{
			Name: "GetService",
			Type: ember.TypeSummary{
				Kind:  ember.TypeSummaryIntersection,
				Types: []ember.TypeSummary{overload("Players", players, true), overload("Widget", widget, true)},
			},
		}},
	}
	source := `--!strict
local widget = Factory.new("Widget")
local enabled: boolean = widget.Enabled
local name: string = widget.Name
local pong: string = widget:Ping(1)
widget.Name = "renamed"
local players = root:GetService("Players")
local count: number = players.Count
players.Count = 2
local dynamicName: string = "Widget"
local dynamicValue = Factory.new(dynamicName)
local wrong = Factory.new("Missing")
return enabled`
	analyzer := ember.NewAnalyzer(ember.WithAnalysisConfig(ember.AnalysisConfig{
		Globals: map[string]ember.TypeSummary{
			"Factory": factory,
			"root":    root,
		},
	}))
	result, err := analyzer.Check(context.Background(), ember.Source{Name: "logical:game/init", Text: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %#v, want read-only Name, read-only Count, and wrong literal", result.Diagnostics)
	}
	got := make(map[string]string, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		got[source[diagnostic.Start:diagnostic.End]] = diagnostic.Code
	}
	for _, expected := range []string{"widget.Name", "players.Count", `"Missing"`} {
		if _, ok := got[expected]; !ok {
			t.Fatalf("diagnostic ranges = %#v, missing %q", got, expected)
		}
	}
}
