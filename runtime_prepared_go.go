package ember

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"io"
	"reflect"
	"sort"
	"strconv"
)

const defaultPreparedGoMaxBytes = 32 << 20

// PreparedGoOptions configures deterministic generated-package output.
type PreparedGoOptions struct {
	// Package is the package name written into the generated Go source.
	Package string
	// MaxBytes rejects larger output before anything is written. Zero uses
	// Ember's conservative default limit.
	MaxBytes int
}

// WritePreparedGo writes one deterministic Go file containing a PreparedBundle
// for p. Generation is a build-time operation; Runtime never invokes the Go
// toolchain or loads code dynamically.
func (p *Program) WritePreparedGo(writer io.Writer, options PreparedGoOptions) error {
	if p == nil {
		return fmt.Errorf("write prepared Go: nil Program")
	}
	if preparedGoWriterIsNil(writer) {
		return fmt.Errorf("write prepared Go: nil writer")
	}
	if !token.IsIdentifier(options.Package) || token.Lookup(options.Package).IsKeyword() {
		return fmt.Errorf("write prepared Go: invalid package name %q", options.Package)
	}
	maxBytes := options.MaxBytes
	if maxBytes < 0 {
		return fmt.Errorf("write prepared Go: negative MaxBytes %d", maxBytes)
	}
	if maxBytes == 0 {
		maxBytes = defaultPreparedGoMaxBytes
	}
	image, err := p.preparedProgramImage()
	if err != nil {
		return fmt.Errorf("write prepared Go: prepare Program image: %w", err)
	}
	programIR, err := buildBackendProgramIR(image)
	if err != nil {
		return fmt.Errorf("write prepared Go: %w", err)
	}
	modules := make([]backendGoNumericModule, len(programIR.modules))
	files := make([]backendGoNumericProgramFile, 0)
	for moduleIndex := range programIR.modules {
		moduleIR := &programIR.modules[moduleIndex]
		generated, err := emitBackendGoNumericModule(moduleIR.protos, backendGoNumericModuleOptions{
			packageName:         options.Package,
			functionPrefix:      fmt.Sprintf("emberPreparedM%d", moduleIndex),
			preparedImportPath:  "github.com/besmpl/ember",
			preparedQualifier:   "emberapi",
			coroutineDeadString: backendGoCoroutineDeadString(moduleIR.code),
		})
		if err != nil {
			return fmt.Errorf("write prepared Go: module %d: %w", moduleIndex, err)
		}
		modules[moduleIndex] = generated
		files = append(files, generated.files...)
	}
	source, err := assemblePreparedGoSource(options.Package, programIR, modules, files)
	if err != nil {
		return fmt.Errorf("write prepared Go: %w", err)
	}
	if len(source) > maxBytes {
		return fmt.Errorf("write prepared Go: generated source is %d bytes, exceeds MaxBytes %d", len(source), maxBytes)
	}
	written, err := writer.Write(source)
	if err != nil {
		return fmt.Errorf("write prepared Go: %w", err)
	}
	if written != len(source) {
		return fmt.Errorf("write prepared Go: %w", io.ErrShortWrite)
	}
	return nil
}

func preparedGoWriterIsNil(writer io.Writer) bool {
	if writer == nil {
		return true
	}
	value := reflect.ValueOf(writer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func backendGoCoroutineDeadString(code *codeImage) machineStringID {
	if code == nil {
		return invalidMachineStringID
	}
	for index, record := range code.stringRecords {
		start := uint64(record.offset)
		end := start + uint64(record.length)
		if end > uint64(len(code.stringData)) {
			return invalidMachineStringID
		}
		if string(code.stringData[int(start):int(end)]) == "dead" {
			return machineStringID(index + 1)
		}
	}
	return invalidMachineStringID
}

func assemblePreparedGoSource(
	packageName string,
	program *backendProgramIR,
	modules []backendGoNumericModule,
	files []backendGoNumericProgramFile,
) ([]byte, error) {
	fileSet := token.NewFileSet()
	imports := map[string]*ast.ImportSpec{
		"emberapi\x00\"github.com/besmpl/ember\"": {
			Name: ast.NewIdent("emberapi"),
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"github.com/besmpl/ember"`},
		},
	}
	declarations := make([]ast.Decl, 0)
	for index, generated := range files {
		parsed, err := goparser.ParseFile(fileSet, generated.name+strconv.Itoa(index), generated.source, 0)
		if err != nil {
			return nil, fmt.Errorf("parse generated Proto %d: %w", generated.protoID, err)
		}
		for _, spec := range parsed.Imports {
			name := ""
			if spec.Name != nil {
				name = spec.Name.Name
			}
			key := name + "\x00" + spec.Path.Value
			imports[key] = &ast.ImportSpec{Name: ast.NewIdent(name), Path: &ast.BasicLit{Kind: token.STRING, Value: spec.Path.Value}}
			if name == "" {
				imports[key].Name = nil
			}
		}
		for _, declaration := range parsed.Decls {
			if group, ok := declaration.(*ast.GenDecl); ok && group.Tok == token.IMPORT {
				continue
			}
			declarations = append(declarations, declaration)
		}
	}
	keys := make([]string, 0, len(imports))
	for key := range imports {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) != 0 {
		specs := make([]ast.Spec, len(keys))
		for index, key := range keys {
			specs[index] = imports[key]
		}
		declarations = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: specs}}, declarations...)
	}
	file := &ast.File{Name: ast.NewIdent(packageName), Decls: declarations}
	var source bytes.Buffer
	if err := format.Node(&source, fileSet, file); err != nil {
		return nil, fmt.Errorf("format generated declarations: %w", err)
	}
	writePreparedBundleDeclaration(&source, program, modules)
	formatted, err := format.Source(source.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated package: %w", err)
	}
	return append([]byte("// Code generated by Ember's prepared compiler; DO NOT EDIT.\n\n"), formatted...), nil
}

func writePreparedBundleDeclaration(source *bytes.Buffer, program *backendProgramIR, modules []backendGoNumericModule) {
	fmt.Fprintf(source, "\nvar Bundle = emberapi.NewPreparedBundle(%d, %d, [32]byte{", program.abiVersion, program.semanticVersion)
	for index, value := range program.programHash {
		if index != 0 {
			source.WriteString(", ")
		}
		fmt.Fprintf(source, "0x%02x", value)
	}
	source.WriteString("}, [][]emberapi.PreparedFunction{")
	for _, module := range modules {
		source.WriteString("{")
		for index, function := range module.functions {
			if index != 0 {
				source.WriteString(", ")
			}
			if function == "" {
				source.WriteString("nil")
			} else {
				source.WriteString(function)
			}
		}
		source.WriteString("},")
	}
	source.WriteString("})\n")
}
