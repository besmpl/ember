package ember_test

import (
	"context"
	"fmt"

	"github.com/besmpl/ember"
)

type exampleLoader map[string]string

func (loader exampleLoader) LoadModule(_ context.Context, id ember.ModuleID) (ember.Source, error) {
	text, ok := loader[id.String()]
	if !ok {
		return ember.Source{}, fmt.Errorf("missing module %s", id.String())
	}
	return ember.Source{Name: id.String(), Text: text}, nil
}

func ExampleRuntime_Invoke() {
	ctx := context.Background()
	module := ember.LogicalModule("format/greeting")
	program, _, err := ember.LoadProgram(ctx, exampleLoader{
		module.String(): `return function(name) return decorate("hello " .. name) end`,
	}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "greeting", Module: module}},
	})
	if err != nil {
		panic(err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		panic(err)
	}
	defer runtime.Close()

	values, err := runtime.Invoke(ctx, ember.Invocation{
		Module: module,
		Globals: map[string]ember.Value{
			"decorate": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
				text, _ := args[0].String()
				return []ember.Value{ember.StringValue("[" + text + "]")}, nil
			}),
		},
	}, ember.StringValue("Ember"))
	if err != nil {
		panic(err)
	}
	message, _ := values[0].String()
	fmt.Println(message)

	// Output: [hello Ember]
}
