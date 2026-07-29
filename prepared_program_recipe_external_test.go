package ember_test

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func TestPreparedProgramRecipeLoadsCopiedSourcesAndEntrypoints(t *testing.T) {
	main := ember.LogicalModule("recipe/main")
	dependency := ember.LogicalModule("recipe/dependency")
	modules := []ember.PreparedProgramModule{
		{Module: main, SourceName: "src/main.luau", SourceText: `local dependency = require("./dependency") return function() return dependency + 1 end`},
		{Module: dependency, SourceName: "src/dependency.luau", SourceText: "return 41"},
	}
	entrypoints := []ember.Entrypoint{{Name: "main", Module: main}}

	recipe, err := ember.NewPreparedProgramRecipe(modules, entrypoints)
	if err != nil {
		t.Fatal(err)
	}
	modules[0].SourceText = "return 0"
	entrypoints[0] = ember.Entrypoint{Name: "changed", Module: dependency}

	program, report, err := recipe.Load(context.Background(), ember.ProgramOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entrypoints) != 1 || report.Entrypoints[0].Name != "main" || report.Entrypoints[0].Module != main {
		t.Fatalf("entrypoints = %#v, want copied main entrypoint", report.Entrypoints)
	}
	if len(report.Modules) != 2 || report.Modules[0].SourceName != "src/dependency.luau" || report.Modules[1].SourceName != "src/main.luau" {
		t.Fatalf("module reports = %#v, want copied source names", report.Modules)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	values, err := runtime.Invoke(context.Background(), ember.Invocation{Module: main})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("values = %#v, want one value", values)
	}
	number, ok := values[0].Number()
	if !ok || number != 42 {
		t.Fatalf("values = %#v, want 42", values)
	}
}

func TestPreparedProgramRecipeIdentityIsDeterministicAndDetached(t *testing.T) {
	main := ember.LogicalModule("recipe/main")
	dependency := ember.HostModule("recipe/dependency")
	modules := []ember.PreparedProgramModule{
		{Module: main, SourceName: "main.luau", SourceText: "return 41"},
		{Module: dependency, SourceName: "dependency.luau", SourceText: "return nil"},
	}
	entrypoints := []ember.Entrypoint{{Name: "main", Module: main}}
	recipe, err := ember.NewPreparedProgramRecipe(modules, entrypoints)
	if err != nil {
		t.Fatal(err)
	}
	identical, err := ember.NewPreparedProgramRecipe(modules, entrypoints)
	if err != nil {
		t.Fatal(err)
	}

	identity := recipe.Identity()
	if identity.Digest == ([32]byte{}) || identity.Digest != identical.Identity().Digest {
		t.Fatalf("recipe digest = %x, identical = %x", identity.Digest, identical.Identity().Digest)
	}
	if len(identity.Modules) != 2 || identity.Modules[0].Module != main || identity.Modules[0].SourceName != "main.luau" || identity.Modules[0].SourceBytes != 9 {
		t.Fatalf("module identities = %#v", identity.Modules)
	}
	wantSourceDigest, err := hex.DecodeString("a116ca3f9b3f12f341f5c6cadf066ee59e28ec46cd4f13df0b157593e4fd0a3d")
	if err != nil {
		t.Fatal(err)
	}
	if string(identity.Modules[0].SourceDigest[:]) != string(wantSourceDigest) {
		t.Fatalf("source digest = %x, want %x", identity.Modules[0].SourceDigest, wantSourceDigest)
	}
	if len(identity.Entrypoints) != 1 || identity.Entrypoints[0] != entrypoints[0] {
		t.Fatalf("entrypoints = %#v", identity.Entrypoints)
	}

	identity.Modules[0].SourceName = "changed"
	identity.Entrypoints[0].Name = "changed"
	if fresh := recipe.Identity(); fresh.Modules[0].SourceName != "main.luau" || fresh.Entrypoints[0].Name != "main" {
		t.Fatalf("mutated identity escaped into recipe: %#v", fresh)
	}
	changedModules := append([]ember.PreparedProgramModule(nil), modules...)
	changedModules[0].SourceText = "return 42"
	changed, err := ember.NewPreparedProgramRecipe(changedModules, entrypoints)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Identity().Digest == recipe.Identity().Digest {
		t.Fatal("changed source retained recipe digest")
	}
}

func TestNewPreparedProgramRecipeRejectsInvalidInputs(t *testing.T) {
	main := ember.LogicalModule("recipe/main")
	validModule := ember.PreparedProgramModule{
		Module: main, SourceName: "main.luau", SourceText: "return nil",
	}
	validEntrypoint := ember.Entrypoint{Name: "main", Module: main}
	tests := []struct {
		name        string
		modules     []ember.PreparedProgramModule
		entrypoints []ember.Entrypoint
		want        string
	}{
		{name: "no modules", entrypoints: []ember.Entrypoint{validEntrypoint}, want: "no modules"},
		{name: "invalid module", modules: []ember.PreparedProgramModule{{SourceName: "main.luau"}}, entrypoints: []ember.Entrypoint{validEntrypoint}, want: "module 0"},
		{name: "empty source name", modules: []ember.PreparedProgramModule{{Module: main}}, entrypoints: []ember.Entrypoint{validEntrypoint}, want: "source name"},
		{name: "duplicate module", modules: []ember.PreparedProgramModule{validModule, validModule}, entrypoints: []ember.Entrypoint{validEntrypoint}, want: "duplicate module"},
		{name: "no entrypoints", modules: []ember.PreparedProgramModule{validModule}, want: "no entrypoints"},
		{name: "empty entrypoint name", modules: []ember.PreparedProgramModule{validModule}, entrypoints: []ember.Entrypoint{{Module: main}}, want: "empty entrypoint name"},
		{name: "duplicate entrypoint", modules: []ember.PreparedProgramModule{validModule}, entrypoints: []ember.Entrypoint{validEntrypoint, validEntrypoint}, want: "duplicate entrypoint"},
		{name: "undeclared entrypoint module", modules: []ember.PreparedProgramModule{validModule}, entrypoints: []ember.Entrypoint{{Name: "other", Module: ember.LogicalModule("recipe/other")}}, want: "not declared"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ember.NewPreparedProgramRecipe(test.modules, test.entrypoints)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}
