package ember

import (
	"context"
	"strings"
	"testing"
	"unsafe"
)

func TestCompileAttachesPrototypeDebugMetadata(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		function string
		children []string
	}{
		{name: "root", source: `return 1`, function: "<module>"},
		{name: "local named", source: `local function tick() return 1 end return tick()`, function: "<module>", children: []string{"tick"}},
		{name: "dotted declaration", source: `local target = {} function target.child() return 1 end return target.child()`, function: "<module>", children: []string{"target.child"}},
		{name: "method declaration", source: `local target = {} function target:run() return 1 end return target:run()`, function: "<module>", children: []string{"target:run"}},
		{name: "dotted method declaration", source: `local world = {system = {}} function world.system:update() return 1 end return world.system:update()`, function: "<module>", children: []string{"world.system:update"}},
		{name: "anonymous", source: `return function() return 1 end`, function: "<module>", children: []string{"<anonymous>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto, err := Compile(tt.source)
			if err != nil {
				t.Fatal(err)
			}
			if proto.debugInfo == nil {
				t.Fatal("root prototype has no debug metadata")
			}
			if proto.debugInfo.sourceName != "<string>" || proto.debugInfo.functionName != tt.function {
				t.Fatalf("root debug metadata = %#v", proto.debugInfo)
			}
			if len(proto.prototypes) != len(tt.children) {
				t.Fatalf("child prototype count = %d, want %d", len(proto.prototypes), len(tt.children))
			}
			for index, child := range proto.prototypes {
				if child == nil || child.debugInfo == nil {
					t.Fatal("child prototype has no debug metadata")
				}
				if child.debugInfo.sourceName != "<string>" {
					t.Fatalf("child source name = %q", child.debugInfo.sourceName)
				}
				if child.debugInfo.functionName != tt.children[index] {
					t.Fatalf("child function name = %q, want %q", child.debugInfo.functionName, tt.children[index])
				}
			}
		})
	}
}

func TestCompileAttachesNestedPrototypeDebugMetadata(t *testing.T) {
	proto, err := Compile(`local function outer() return function() return 1 end end return outer`)
	if err != nil {
		t.Fatal(err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("root child count = %d, want 1", len(proto.prototypes))
	}
	if len(proto.prototypes[0].prototypes) != 1 {
		t.Fatalf("outer child count = %d, want 1", len(proto.prototypes[0].prototypes))
	}
	wantFunctions := []string{"<module>", "outer", "<anonymous>"}
	for index, current := range []*Proto{proto, proto.prototypes[0], proto.prototypes[0].prototypes[0]} {
		if current.debugInfo == nil {
			t.Fatalf("prototype %d has no debug metadata", index)
		}
		if current.debugInfo.sourceName != "<string>" || current.debugInfo.functionName != wantFunctions[index] {
			t.Fatalf("prototype %d debug metadata = %#v", index, current.debugInfo)
		}
	}
}

func TestCompileOwnsRetainedFunctionNames(t *testing.T) {
	source := strings.Repeat(" ", 256) + `local function retained() return 1 end return retained`
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatal(err)
	}
	proto, err := compileProgram(artifact)
	if err != nil {
		t.Fatal(err)
	}
	nameStart := strings.Index(source, "retained")
	if nameStart < 0 || len(proto.prototypes) != 1 {
		t.Fatal("compiled function name fixture is invalid")
	}
	got := proto.prototypes[0].debugInfo.functionName
	if got != "retained" {
		t.Fatalf("function name = %q", got)
	}
	if unsafe.StringData(got) == unsafe.StringData(source[nameStart:nameStart+len(got)]) {
		t.Fatal("prototype function name retains the source backing string")
	}
}

type protoDebugMetadataLoader map[string]Source

func (l protoDebugMetadataLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	return l[id.String()], nil
}

func TestLoadProgramUsesModuleSourceNameForPrototypeMetadata(t *testing.T) {
	tests := []struct {
		name   string
		loader protoDebugMetadataLoader
	}{
		{
			name: "explicit source names",
			loader: protoDebugMetadataLoader{
				"logical:root":  {Name: "root-script", Text: `local function load() return require("child") end return load()`},
				"logical:child": {Name: "child-script", Text: `return function() return 1 end`},
			},
		},
		{
			name: "module ID fallback",
			loader: protoDebugMetadataLoader{
				"logical:root":  {Text: `return require("child")`},
				"logical:child": {Text: `return 1`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, _, err := LoadProgram(context.Background(), tt.loader, ProgramOptions{
				Entrypoints: []Entrypoint{{Name: "main", Module: LogicalModule("root")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			for key, proto := range program.protos {
				if proto == nil || proto.debugInfo == nil {
					t.Fatalf("prototype %s has no debug metadata", key)
				}
				want := tt.loader[key.String()].Name
				if want == "" {
					want = key.String()
				}
				if got := program.graph.Nodes[key].Source.Name; got != want {
					t.Fatalf("module %s source name = %q, want %q", key, got, want)
				}
				if proto.debugInfo.sourceName != want {
					t.Fatalf("prototype %s source = %q, want %q", key, proto.debugInfo.sourceName, want)
				}
				if proto.debugInfo.functionName != "<module>" {
					t.Fatalf("prototype %s function = %q", key, proto.debugInfo.functionName)
				}
				assertPrototypeSourceName(t, proto, want)
			}
		})
	}
}

func assertPrototypeSourceName(t *testing.T, proto *Proto, want string) {
	t.Helper()
	if proto == nil || proto.debugInfo == nil {
		t.Fatal("prototype has no debug metadata")
	}
	if proto.debugInfo.sourceName != want {
		t.Fatalf("prototype source = %q, want %q", proto.debugInfo.sourceName, want)
	}
	for _, child := range proto.prototypes {
		assertPrototypeSourceName(t, child, want)
	}
}
