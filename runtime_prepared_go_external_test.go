package ember_test

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func TestProgramWritePreparedGoIsDeterministicAndSelfContained(t *testing.T) {
	program := loadPreparedGoProgram(t, `
local function add(value)
    return value + 1
end
return {
    first = function() return add(40) end,
    second = function() return add(41) end,
}
`)

	var first bytes.Buffer
	if err := program.WritePreparedGo(&first, ember.PreparedGoOptions{Package: "generatedfixture"}); err != nil {
		t.Fatal(err)
	}
	var second bytes.Buffer
	if err := program.WritePreparedGo(&second, ember.PreparedGoOptions{Package: "generatedfixture"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("WritePreparedGo output is not deterministic")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "prepared_generated.go", first.Bytes(), parser.AllErrors); err != nil {
		t.Fatalf("parse generated source: %v\n%s", err, first.Bytes())
	}
	source := first.String()
	for _, required := range []string{
		"package generatedfixture",
		`emberapi "github.com/besmpl/ember"`,
		"var Bundle = emberapi.NewPreparedBundle(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("generated source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"machinePrepared",
		"go:linkname",
		"plugin",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generated source contains forbidden marker %q", forbidden)
		}
	}
	if got := strings.Count(source, "func emberPreparedM0Proto1("); got != 1 {
		t.Fatalf("shared helper body count = %d, want 1", got)
	}
	if got := strings.Count(source, "emberPreparedM0Proto1("); got != 5 {
		t.Fatalf("shared helper definition/call count = %d, want 5", got)
	}
}

func TestProgramWritePreparedGoIdentityAndLimits(t *testing.T) {
	firstProgram := loadPreparedGoProgram(t, `return {update = function() return 40 end}`)
	secondProgram := loadPreparedGoProgram(t, `return {update = function() return 41 end}`)
	generate := func(program *ember.Program) []byte {
		t.Helper()
		var output bytes.Buffer
		if err := program.WritePreparedGo(&output, ember.PreparedGoOptions{Package: "identityfixture"}); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	first := generate(firstProgram)
	second := generate(secondProgram)
	if bytes.Equal(first, second) {
		t.Fatal("distinct Program identities generated equal source")
	}

	var limited bytes.Buffer
	limited.WriteString("unchanged")
	err := firstProgram.WritePreparedGo(&limited, ember.PreparedGoOptions{
		Package:  "identityfixture",
		MaxBytes: len(first) - 1,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds MaxBytes") {
		t.Fatalf("limited generation error = %v, want MaxBytes rejection", err)
	}
	if limited.String() != "unchanged" {
		t.Fatalf("limited generation mutated writer: %q", limited.String())
	}
}

func TestProgramWritePreparedGoValidatesPublicBoundary(t *testing.T) {
	program := loadPreparedGoProgram(t, `return {update = function() return 40 end}`)
	for _, test := range []struct {
		name    string
		writer  *bytes.Buffer
		options ember.PreparedGoOptions
	}{
		{name: "missing package", writer: &bytes.Buffer{}},
		{name: "keyword package", writer: &bytes.Buffer{}, options: ember.PreparedGoOptions{Package: "func"}},
		{name: "negative limit", writer: &bytes.Buffer{}, options: ember.PreparedGoOptions{Package: "valid", MaxBytes: -1}},
		{name: "nil writer", writer: nil, options: ember.PreparedGoOptions{Package: "valid"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := program.WritePreparedGo(test.writer, test.options); err == nil {
				t.Fatal("WritePreparedGo succeeded")
			}
		})
	}

	var nilProgram *ember.Program
	if err := nilProgram.WritePreparedGo(&bytes.Buffer{}, ember.PreparedGoOptions{Package: "valid"}); err == nil {
		t.Fatal("nil Program WritePreparedGo succeeded")
	}
	if err := program.WritePreparedGo(preparedGoShortWriter{}, ember.PreparedGoOptions{Package: "valid"}); err == nil || !strings.Contains(err.Error(), "short write") {
		t.Fatalf("short writer error = %v, want short write", err)
	}
}

func TestProgramWritePreparedGoRejectsMutableFunctionUpvalueBeforeWrite(t *testing.T) {
	program := loadPreparedGoProgram(t, `
local add = function(value) return value + 1 end
return {
    update = function()
        add = function(value) return value + 2 end
        return add(40)
    end,
}
`)
	var output bytes.Buffer
	output.WriteString("unchanged")
	if err := program.WritePreparedGo(&output, ember.PreparedGoOptions{Package: "mutablefixture"}); err == nil {
		t.Fatal("mutable function upvalue generation succeeded")
	}
	if output.String() != "unchanged" {
		t.Fatalf("rejected generation mutated writer: %q", output.String())
	}
}

func TestProgramWritePreparedGoSiblingGrowthIsLinear(t *testing.T) {
	generate := func(count int) []byte {
		t.Helper()
		program := loadPreparedGoProgram(t, preparedGoSiblingSource(count))
		var output bytes.Buffer
		if err := program.WritePreparedGo(&output, ember.PreparedGoOptions{Package: "growthfixture"}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(output.String(), "func emberPreparedM0Proto1("); got != 1 {
			t.Fatalf("%d siblings emitted %d shared helper bodies, want 1", count, got)
		}
		return output.Bytes()
	}
	sixteen := generate(16)
	thirtyTwo := generate(32)
	if len(thirtyTwo) >= len(sixteen)*5/2 {
		t.Fatalf("generated growth = %d -> %d bytes, want bounded linear growth", len(sixteen), len(thirtyTwo))
	}
}

func loadPreparedGoProgram(t *testing.T, source string) *ember.Program {
	t.Helper()
	module := ember.LogicalModule("prepared/generate")
	program, _, err := ember.LoadProgram(context.Background(), externalPreparedLoader{
		module.String(): source,
	}, ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func preparedGoSiblingSource(count int) string {
	var source strings.Builder
	source.WriteString("local function add(value) return value + 1 end\nreturn {\n")
	for index := 0; index < count; index++ {
		fmt.Fprintf(&source, "hook%d = function() return add(%d) end,\n", index, index)
	}
	source.WriteString("}\n")
	return source.String()
}

type preparedGoShortWriter struct{}

func (preparedGoShortWriter) Write(buffer []byte) (int, error) {
	return len(buffer) - 1, nil
}
