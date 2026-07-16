package ember_test

import (
	"context"
	"testing"

	"github.com/besmpl/ember"
)

func TestHearthShapedEmbeddingContract(t *testing.T) {
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	receiver := ember.TypeSummary{Kind: ember.TypeSummaryTable, Display: "table"}
	childType := ember.TypeSummary{
		Kind:       ember.TypeSummaryTable,
		Display:    "table",
		Properties: []ember.TablePropertySummary{{Name: "Name", Access: "read", Type: stringType}},
	}
	widgetType := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{{
			Name: "WaitForChild",
			Type: ember.TypeSummary{
				Kind:   ember.TypeSummaryFunction,
				Params: []ember.TypeSummary{receiver, stringType},
				ReturnPack: ember.TypePackSummary{
					Head: []ember.TypeSummary{childType},
				},
			},
		}},
	}
	factoryType := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{{
			Name: "new",
			Type: ember.TypeSummary{
				Kind: ember.TypeSummaryIntersection,
				Types: []ember.TypeSummary{{
					Kind: ember.TypeSummaryFunction,
					Params: []ember.TypeSummary{{
						Kind:    ember.TypeSummarySingleton,
						Display: `"Widget"`,
					}},
					ReturnPack: ember.TypePackSummary{Head: []ember.TypeSummary{widgetType}},
				}},
			},
		}},
	}
	source := `--!strict
return {
    startup = function()
        local widget = Factory.new("Widget")
        local child = widget:WaitForChild("Button")
        local name: string = child.Name
        observe(name)
    end,
}`
	loader := &programTestLoader{sources: map[string]string{
		"logical:game/init": source,
	}}
	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
		Check:       true,
		Parallelism: 4,
		Analysis: ember.AnalysisConfig{Globals: map[string]ember.TypeSummary{
			"Factory": factoryType,
			"observe": {
				Kind:   ember.TypeSummaryFunction,
				Params: []ember.TypeSummary{stringType},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("typed load diagnostics = %#v", report.Diagnostics)
	}

	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			var observed string
			widget := ember.NewTable()
			_ = widget.Set(ember.StringValue("WaitForChild"), ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
				name, _ := args[1].String()
				return ember.HostSuspend("child:" + name)
			}))
			factory := ember.NewTable()
			_ = factory.Set(ember.StringValue("new"), ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
				return []ember.Value{ember.TableValue(widget)}, nil
			}))
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"Factory": ember.TableValue(factory),
						"observe": ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
							observed, _ = args[0].String()
							return nil, nil
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			step, err := runtime.RunHookResumable(context.Background(), "startup")
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, step, "child:Button")
			child := ember.NewTable()
			_ = child.Set(ember.StringValue("Name"), ember.StringValue("Button"))
			done, err := step.Suspension.Resume(context.Background(), ember.TableValue(child))
			if err != nil {
				t.Fatal(err)
			}
			if done.Hook == nil || done.Suspension != nil || observed != "Button" {
				t.Fatalf("completed result = %#v observed %q", done, observed)
			}
		})
	}
}
