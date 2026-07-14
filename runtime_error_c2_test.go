package ember

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRuntimeErrorCapturesNestedFrames(t *testing.T) {
	proto, err := Compile("local function inner()\n  return missing.value\nend\nlocal function outer()\n  return inner()\nend\nreturn outer()")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(proto)
	if err == nil {
		t.Fatal("Run succeeded")
	}
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error %T = %v, want RuntimeError", err, err)
	}
	if len(runtimeErr.Frames) != 3 {
		t.Fatalf("frames = %#v, want exactly nested script stack without duplicates", runtimeErr.Frames)
	}
	if runtimeErr.Frames[0].Function != "inner" || runtimeErr.Frames[1].Function != "outer" || runtimeErr.Frames[2].Function != "<module>" {
		t.Fatalf("frames = %#v, want inner/outer/<module>", runtimeErr.Frames)
	}
	if runtimeErr.Frames[0].Line != 2 {
		t.Fatalf("innermost line = %d, want failing line 2", runtimeErr.Frames[0].Line)
	}
	if strings.Count(runtimeErr.Error(), " in inner") != 1 {
		t.Fatalf("formatted error = %q, want one inner frame", runtimeErr.Error())
	}
}

func TestRuntimeErrorCapturesDirectCallDepthStack(t *testing.T) {
	proto, err := Compile(`
local function recurse(n)
  if n == 0 then return 0 end
  record(n)
  return recurse(n - 1)
end
return recurse(100)
`)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(map[string]Value{
		"record": HostFuncValue(func([]Value) ([]Value, error) { return nil, nil }),
	}))
	thread.controller = controller
	_, err = thread.run(proto, nil, nil)
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError", err)
	}
	if len(runtimeErr.Frames) < 2 || runtimeErr.Frames[0].Function != "recurse" {
		t.Fatalf("frames = %#v, want recursive direct frames", runtimeErr.Frames)
	}
}

func TestRuntimeErrorPreservesHostCause(t *testing.T) {
	hostErr := errors.New("host sentinel")
	proto, err := Compile("return host()")
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunWithGlobals(proto, map[string]Value{
		"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, fmt.Errorf("adapter: %w", hostErr) }),
	})
	if err == nil || !errors.Is(err, hostErr) {
		t.Fatalf("error = %v, want host cause", err)
	}
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error %T = %v, want RuntimeError", err, err)
	}
}

func TestRuntimeErrorCapturesAnonymousFrame(t *testing.T) {
	proto, err := Compile("return (function()\n  return missing.value\nend)()")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(proto)
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError", err)
	}
	if len(runtimeErr.Frames) < 2 || runtimeErr.Frames[0].Function != "<anonymous>" {
		t.Fatalf("frames = %#v, want anonymous then module", runtimeErr.Frames)
	}
}

func TestRuntimeErrorMapsAUXInstructionToLogicalLine(t *testing.T) {
	hostErr := errors.New("aux host sentinel")
	proto, err := Compile("local x = 1\nreturn host(x)\nreturn 2\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunWithGlobals(proto, map[string]Value{
		"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, hostErr }),
	})
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError", err)
	}
	wantLine := 2
	if runtimeErr.Frames[0].Line != wantLine {
		t.Fatalf("AUX failing line = %d, want %d", runtimeErr.Frames[0].Line, wantLine)
	}
	auxPrimary := false
	for index, word := range proto.words {
		if opcode(uint8(word)&uint8(wordcodeOpcodeMask)) == opCall && word&wordcodeAuxBit != 0 {
			if index+1 >= len(proto.words) || proto.wordLines[index+1] != 0 {
				t.Fatalf("AUX line mapping = %#v, want zero payload line", proto.wordLines)
			}
			auxPrimary = true
		}
	}
	if !auxPrimary {
		t.Fatal("fixture did not emit an AUX-bearing call")
	}
}

func TestRuntimeErrorUsesFailingLineNotFollowingLine(t *testing.T) {
	proto, err := Compile("local x = 1\nreturn x.missing\nreturn 2\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(proto)
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError", err)
	}
	if runtimeErr.Frames[0].Line != 2 {
		t.Fatalf("failing line = %d, want 2", runtimeErr.Frames[0].Line)
	}
}

func TestRuntimeErrorAPIOwnsFramesAndPreservesExisting(t *testing.T) {
	cause := errors.New("cause")
	input := []ScriptFrame{{Source: "a", Function: "f", Line: 1}}
	err := newRuntimeError(cause, input)
	input[0].Source = "mutated"
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Frames[0].Source != "a" {
		t.Fatalf("frames = %#v, want copied source", runtimeErr.Frames)
	}
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "a:1 in f") {
		t.Fatalf("error = %v, want stable formatting and cause", err)
	}
	existing := &RuntimeError{Message: "existing", Cause: cause}
	if got := newRuntimeError(existing, input); got != existing {
		t.Fatal("existing RuntimeError was wrapped again")
	}
}

type runtimeErrorModuleLoader map[string]string

func (loader runtimeErrorModuleLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	text, ok := loader[id.String()]
	if !ok {
		return Source{}, fmt.Errorf("missing module %s", id.String())
	}
	return Source{Name: id.String(), Text: text}, nil
}

func TestRuntimeErrorCapturesRequiredModuleSource(t *testing.T) {
	program, _, err := LoadProgram(context.Background(), runtimeErrorModuleLoader{
		"logical:game/main":  `local child = require("./child") return {startup = function() end}`,
		"logical:game/child": "return missing.value",
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: LogicalModule("game/main")}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := program.NewRuntime(RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunHook(context.Background(), "startup")
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError", err)
	}
	if len(runtimeErr.Frames) != 2 {
		t.Fatalf("frames = %#v, want child and requiring module only", runtimeErr.Frames)
	}
	if runtimeErr.Frames[0].Source != "logical:game/child" || runtimeErr.Frames[0].Function != "<module>" || runtimeErr.Frames[0].Line != 1 {
		t.Fatalf("child frame = %#v, want child module line 1", runtimeErr.Frames[0])
	}
	if runtimeErr.Frames[1].Source != "logical:game/main" || runtimeErr.Frames[1].Function != "<module>" || runtimeErr.Frames[1].Line != 1 {
		t.Fatalf("parent frame = %#v, want requiring main line 1", runtimeErr.Frames[1])
	}
}

func TestRuntimeErrorCapturesNestedRequiredModuleStack(t *testing.T) {
	program, _, err := LoadProgram(context.Background(), runtimeErrorModuleLoader{
		"logical:game/main":  `local child = require("./child") return {startup = function() end}`,
		"logical:game/child": `local grand = require("./grand") return grand`,
		"logical:game/grand": "return missing.value",
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: LogicalModule("game/main")}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := program.NewRuntime(RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunHook(context.Background(), "startup")
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError", err)
	}
	if len(runtimeErr.Frames) != 3 {
		t.Fatalf("frames = %#v, want grand/child/main", runtimeErr.Frames)
	}
	wantSources := []string{"logical:game/grand", "logical:game/child", "logical:game/main"}
	for index, want := range wantSources {
		if runtimeErr.Frames[index].Source != want || runtimeErr.Frames[index].Function != "<module>" {
			t.Fatalf("frame %d = %#v, want %s module", index, runtimeErr.Frames[index], want)
		}
	}
}

func TestRuntimeErrorRequireContextRestoresAfterSuccess(t *testing.T) {
	program, _, err := LoadProgram(context.Background(), runtimeErrorModuleLoader{
		"logical:game/main":  `local child = require("./child") return {startup = function() return missing.value end}`,
		"logical:game/child": "return {}",
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: LogicalModule("game/main")}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := program.NewRuntime(RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunHook(context.Background(), "startup")
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError", err)
	}
	if len(runtimeErr.Frames) != 1 || runtimeErr.Frames[0].Source != "logical:game/main" {
		t.Fatalf("frames = %#v, want only main after successful require", runtimeErr.Frames)
	}
}
