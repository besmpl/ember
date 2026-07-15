package ember

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestLoadProgramPreparesWholeProgramImageWithoutExecution(t *testing.T) {
	loader := &programImageTestLoader{sources: map[string]string{
		"logical:game/z":      `return require("./shared")`,
		"logical:game/a":      `return require("./shared")`,
		"logical:game/shared": `return error("must not execute while loading")`,
	}}
	program, _, err := LoadProgram(context.Background(), loader, ProgramOptions{
		Entrypoints: []Entrypoint{
			{Name: "z", Module: LogicalModule("game/z")},
			{Name: "a", Module: LogicalModule("game/a")},
		},
		Parallelism: 2,
	})
	if err != nil {
		t.Fatalf("LoadProgram returned error: %v", err)
	}
	if program == nil || program.programImage == nil {
		t.Fatal("LoadProgram did not cache a Program image")
	}
	if got, want := len(program.programImage.modules), 3; got != want {
		t.Fatalf("module count = %d, want %d", got, want)
	}
	for index, module := range program.programImage.modules {
		if module.moduleID != programModuleID(index) {
			t.Fatalf("module %d id = %d, want %d", index, module.moduleID, index)
		}
	}
	if got, want := program.programImage.modules[0].key.String(), "logical:game/a"; got != want {
		t.Fatalf("module 0 key = %q, want %q", got, want)
	}
	if got, want := program.programImage.modules[1].key.String(), "logical:game/shared"; got != want {
		t.Fatalf("module 1 key = %q, want %q", got, want)
	}
	if got, want := program.programImage.modules[2].key.String(), "logical:game/z"; got != want {
		t.Fatalf("module 2 key = %q, want %q", got, want)
	}
	if got, want := program.programImage.entrypoints[0].moduleID, programModuleID(2); got != want {
		t.Fatalf("z entrypoint module id = %d, want %d", got, want)
	}
	if got, want := program.programImage.entrypoints[1].moduleID, programModuleID(0); got != want {
		t.Fatalf("a entrypoint module id = %d, want %d", got, want)
	}
	loader.mu.Lock()
	gotLoads := loader.loads
	loader.mu.Unlock()
	if got := gotLoads; got != 3 {
		t.Fatalf("loader calls = %d, want 3 and no runtime execution", got)
	}
	if _, err := program.NewRuntime(RuntimeOptions{}); err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
}

func TestPrepareProgramImageFailsClosedForMissingOrInvalidProto(t *testing.T) {
	key := moduleKey{kind: moduleKeyLogical, path: "main"}
	graph := moduleGraph{Nodes: map[moduleKey]moduleGraphNode{key: {Source: Source{Name: "main"}}}}

	missing := &Program{entrypoints: []programEntrypoint{{name: "main", key: key}}, graph: graph}
	if _, err := missing.preparedProgramImage(); err == nil || !strings.Contains(err.Error(), "missing prototype") {
		t.Fatalf("missing prototype error = %v, want missing prototype", err)
	}

	invalid := &Program{
		entrypoints: []programEntrypoint{{name: "main", key: key}},
		graph:       graph,
		protos:      map[moduleKey]*Proto{key: {registers: 1, words: []wordcodeWord{wordcodeOpcodeMask}}},
	}
	if _, err := invalid.preparedProgramImage(); err == nil || !strings.Contains(err.Error(), "module logical:main") {
		t.Fatalf("invalid prototype error = %v, want module context", err)
	}
}

func TestPrepareProgramImageRejectsOrphansAndMissingEntrypointModules(t *testing.T) {
	mainKey := moduleKey{kind: moduleKeyLogical, path: "main"}
	otherKey := moduleKey{kind: moduleKeyLogical, path: "other"}
	mainProto, err := Compile("return 1")
	if err != nil {
		t.Fatal(err)
	}
	otherProto, err := Compile("return 2")
	if err != nil {
		t.Fatal(err)
	}

	orphan := &Program{
		graph:  moduleGraph{Nodes: map[moduleKey]moduleGraphNode{mainKey: {}}},
		protos: map[moduleKey]*Proto{mainKey: mainProto, otherKey: otherProto},
	}
	if _, err := orphan.preparedProgramImage(); err == nil || !strings.Contains(err.Error(), "unknown module logical:other") {
		t.Fatalf("orphan prototype error = %v, want unknown module", err)
	}

	missingEntrypoint := &Program{
		entrypoints: []programEntrypoint{{name: "other", key: otherKey}},
		graph:       moduleGraph{Nodes: map[moduleKey]moduleGraphNode{mainKey: {}}},
		protos:      map[moduleKey]*Proto{mainKey: mainProto},
	}
	if _, err := missingEntrypoint.preparedProgramImage(); err == nil || !strings.Contains(err.Error(), "entrypoint \"other\" references missing module") {
		t.Fatalf("missing entrypoint module error = %v, want missing module", err)
	}
}

func TestProgramImageTypeDoesNotRetainProtoPointers(t *testing.T) {
	if typ := reflect.TypeOf(programImage{}); typeContainsProtoPointer(typ, make(map[reflect.Type]bool)) {
		t.Fatalf("program image type retains a *Proto pointer: %v", typ)
	}
}

func typeContainsProtoPointer(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if typ == nil || seen[typ] {
		return false
	}
	seen[typ] = true
	if typ.Kind() == reflect.Pointer && typ.Elem() == reflect.TypeOf(Proto{}) {
		return true
	}
	switch typ.Kind() {
	case reflect.Array, reflect.Pointer, reflect.Slice:
		return typeContainsProtoPointer(typ.Elem(), seen)
	case reflect.Map:
		return typeContainsProtoPointer(typ.Key(), seen) || typeContainsProtoPointer(typ.Elem(), seen)
	case reflect.Struct:
		for field := 0; field < typ.NumField(); field++ {
			if typeContainsProtoPointer(typ.Field(field).Type, seen) {
				return true
			}
		}
	}
	return false
}

type programImageTestLoader struct {
	sources map[string]string
	mu      sync.Mutex
	loads   int
}

func (loader *programImageTestLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	loader.loads++
	text, ok := loader.sources[id.String()]
	if !ok {
		return Source{}, fmt.Errorf("missing source %s", id.String())
	}
	return Source{Name: id.String(), Text: text}, nil
}

func TestPreparedProgramImageAssignsDeterministicDenseModuleIDs(t *testing.T) {
	left, right := moduleKey{kind: moduleKeyLogical, path: "z"}, moduleKey{kind: moduleKeyLogical, path: "a"}
	leftProto, err := Compile("return 1")
	if err != nil {
		t.Fatal(err)
	}
	rightProto, err := Compile("return 2")
	if err != nil {
		t.Fatal(err)
	}
	first := &Program{
		entrypoints: []programEntrypoint{{name: "left", key: left}, {name: "right", key: right}},
		graph: moduleGraph{Nodes: map[moduleKey]moduleGraphNode{
			left:  {Source: Source{Name: "z"}},
			right: {Source: Source{Name: "a"}},
		}},
		protos: map[moduleKey]*Proto{left: leftProto, right: rightProto},
	}
	second := &Program{
		entrypoints: append([]programEntrypoint(nil), first.entrypoints...),
		graph: moduleGraph{Nodes: map[moduleKey]moduleGraphNode{
			right: {Source: Source{Name: "a"}},
			left:  {Source: Source{Name: "z"}},
		}},
		protos: map[moduleKey]*Proto{right: rightProto, left: leftProto},
	}

	firstImage, err := first.preparedProgramImage()
	if err != nil {
		t.Fatalf("first image: %v", err)
	}
	secondImage, err := second.preparedProgramImage()
	if err != nil {
		t.Fatalf("second image: %v", err)
	}
	if !reflect.DeepEqual(firstImage, secondImage) {
		t.Fatalf("program image depends on map order:\nfirst=%#v\nsecond=%#v", firstImage, secondImage)
	}
	if got, want := len(firstImage.modules), 2; got != want {
		t.Fatalf("module count = %d, want %d", got, want)
	}
	if firstImage.modules[0].key != right || firstImage.modules[0].moduleID != 0 {
		t.Fatalf("first module = %#v, want sorted module a with id 0", firstImage.modules[0])
	}
	if firstImage.modules[1].key != left || firstImage.modules[1].moduleID != 1 {
		t.Fatalf("second module = %#v, want sorted module z with id 1", firstImage.modules[1])
	}
	if got, want := firstImage.entrypoints[0].moduleID, programModuleID(1); got != want {
		t.Fatalf("left entrypoint module id = %d, want %d", got, want)
	}
	if got, want := firstImage.entrypoints[1].moduleID, programModuleID(0); got != want {
		t.Fatalf("right entrypoint module id = %d, want %d", got, want)
	}
}

func TestPreparedProgramImageIsCachedConcurrently(t *testing.T) {
	proto, err := Compile("return 1")
	if err != nil {
		t.Fatal(err)
	}
	key := moduleKey{kind: moduleKeyLogical, path: "main"}
	program := &Program{
		entrypoints: []programEntrypoint{{name: "main", key: key}},
		graph:       moduleGraph{Nodes: map[moduleKey]moduleGraphNode{key: {Source: Source{Name: "main"}}}},
		protos:      map[moduleKey]*Proto{key: proto},
	}

	const workers = 32
	images := make(chan *programImage, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			image, err := program.preparedProgramImage()
			images <- image
			errs <- err
		}()
	}
	group.Wait()
	close(images)
	close(errs)

	var first *programImage
	for image := range images {
		if image == nil {
			t.Fatal("prepared image is nil")
		}
		if first == nil {
			first = image
			continue
		}
		if image != first {
			t.Fatalf("concurrent preparation returned image %p, want cached image %p", image, first)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent preparation error: %v", err)
		}
	}
}
