package ember

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

func TestLoadProgramPreparesEachSourceOnceAcrossGraphCompileAndCheck(t *testing.T) {
	loader := sourceArtifactTestLoader{
		"logical:game/server/init":   `local config = require("../shared/config") return config`,
		"logical:game/client/init":   `local config = require("../shared/config") return config`,
		"logical:game/shared/config": `return {value = 1}`,
	}

	var mu sync.Mutex
	preparations := make(map[string]int)
	artifacts := newSourceArtifactStoreWithPrepare(func(source Source) (sourceArtifact, error) {
		mu.Lock()
		preparations[source.Name]++
		mu.Unlock()
		return parseSource(source)
	})

	program, report, err := loadProgramWithArtifactStore(context.Background(), loader, ProgramOptions{
		Entrypoints: []Entrypoint{
			{Name: "server", Module: LogicalModule("game/server/init")},
			{Name: "client", Module: LogicalModule("game/client/init")},
		},
		Check:       true,
		Parallelism: 2,
	}, artifacts)
	if err != nil {
		t.Fatalf("loadProgramWithArtifactStore returned error: %v", err)
	}
	if program == nil {
		t.Fatal("loadProgramWithArtifactStore returned nil program")
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("loadProgramWithArtifactStore returned diagnostics: %#v", report.Diagnostics)
	}

	want := map[string]int{
		"logical:game/server/init":   1,
		"logical:game/client/init":   1,
		"logical:game/shared/config": 1,
	}
	mu.Lock()
	got := make(map[string]int, len(preparations))
	for name, count := range preparations {
		got[name] = count
	}
	mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source preparations = %#v, want %#v", got, want)
	}
}

func TestSourceArtifactStoreCoalescesConcurrentPreparation(t *testing.T) {
	source := Source{Name: "logical:game/shared/config", Text: `return {value = 1}`}
	identity := identifyModuleSource(source)
	started := make(chan struct{})
	release := make(chan struct{})

	var mu sync.Mutex
	preparations := 0
	artifacts := newSourceArtifactStoreWithPrepare(func(source Source) (sourceArtifact, error) {
		mu.Lock()
		preparations++
		if preparations == 1 {
			close(started)
		}
		mu.Unlock()
		<-release
		return parseSource(source)
	})

	const callers = 8
	gate := make(chan struct{})
	entered := make(chan struct{}, callers)
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-gate
			entered <- struct{}{}
			_, err := artifacts.parse(source, identity)
			results <- err
		}()
	}
	close(gate)
	for range callers {
		<-entered
	}
	<-started
	for range callers {
		runtime.Gosched()
	}

	mu.Lock()
	gotPreparations := preparations
	mu.Unlock()
	if gotPreparations != 1 {
		close(release)
		t.Fatalf("concurrent preparations = %d, want 1", gotPreparations)
	}
	close(release)
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("parse returned error: %v", err)
		}
	}
}

func TestSourceArtifactStoreRetriesPreparationAfterError(t *testing.T) {
	source := Source{Name: "logical:game/init", Text: `return 1`}
	identity := identifyModuleSource(source)
	attempts := 0
	artifacts := newSourceArtifactStoreWithPrepare(func(source Source) (sourceArtifact, error) {
		attempts++
		if attempts == 1 {
			return sourceArtifact{}, fmt.Errorf("temporary preparation failure")
		}
		return parseSource(source)
	})

	if _, err := artifacts.parse(source, identity); err == nil {
		t.Fatal("first parse returned nil error")
	}
	if _, err := artifacts.parse(source, identity); err != nil {
		t.Fatalf("second parse returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("preparation attempts = %d, want 2", attempts)
	}
}

func TestSourceArtifactStoreRetainsPreparedSourceAfterGraphError(t *testing.T) {
	root := Source{
		Name: "logical:game/init",
		Text: `local bad = require("./bad") return bad`,
	}
	loader := sourceArtifactTestLoader{
		root.Name:          root.Text,
		"logical:game/bad": `local value =`,
	}

	var mu sync.Mutex
	preparations := make(map[string]int)
	artifacts := newSourceArtifactStoreWithPrepare(func(source Source) (sourceArtifact, error) {
		mu.Lock()
		preparations[source.Name]++
		mu.Unlock()
		return parseSource(source)
	})
	key, err := logicalModuleKey("game/init")
	if err != nil {
		t.Fatalf("logicalModuleKey returned error: %v", err)
	}
	resolver := newProgramModuleResolver(context.Background(), loader)
	if _, err := buildModuleGraphWithStore(resolver, key, artifacts); err == nil {
		t.Fatal("buildModuleGraphWithStore returned nil error")
	}

	if _, err := artifacts.parse(root, identifyModuleSource(root)); err != nil {
		t.Fatalf("parse retained root source: %v", err)
	}
	mu.Lock()
	rootPreparations := preparations[root.Name]
	mu.Unlock()
	if rootPreparations != 1 {
		t.Fatalf("root source preparations = %d, want 1", rootPreparations)
	}
}

type sourceArtifactTestLoader map[string]string

func (l sourceArtifactTestLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	name := id.String()
	text, ok := l[name]
	if !ok {
		return Source{}, fmt.Errorf("missing source %s", name)
	}
	return Source{Name: name, Text: text}, nil
}
