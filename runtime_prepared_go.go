package ember

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"io"
	"reflect"
	"sort"
	"strconv"

	"github.com/besmpl/ember/preparedsource"
)

const (
	defaultPreparedGoMaxBytes = 32 << 20
	preparedGeneratedGoName   = "prepared_generated.go"
)

// PreparedGoOptions configures deterministic generated-package output.
type PreparedGoOptions struct {
	// Package is the package name written into the generated Go source.
	Package string
	// MaxBytes rejects larger output before anything is written. Zero uses
	// Ember's conservative default limit.
	MaxBytes int
	// EmbedProgramRecipe emits an explicit LoadProgram helper containing the
	// exact ordered source inputs beside the PreparedBundle. Worker binaries use
	// it during preparation; steady execution still runs the generated bundle.
	EmbedProgramRecipe bool
}

// PreparedGoIdentity binds generated source to the exact prepared Program and
// optional reconstructible recipe emitted beside it.
type PreparedGoIdentity struct {
	Bundle              PreparedBundleIdentity
	ProgramRecipeDigest [sha256.Size]byte
	// GeneratedDigest is the legacy raw single-file digest retained for the V1
	// worker builder. New source composition uses PreparedGoArtifact.Sources.
	GeneratedDigest [sha256.Size]byte
}

// PreparedGoArtifact is one immutable generated source Set and its derived
// identity. Build systems can pass the artifact across an effect boundary
// without rediscovering metadata from source text or a linked bundle.
type PreparedGoArtifact struct {
	sources  preparedsource.Set
	identity PreparedGoIdentity
}

// Sources returns the immutable generated source Set. A nil artifact returns
// the invalid zero Set.
func (artifact *PreparedGoArtifact) Sources() preparedsource.Set {
	if artifact == nil {
		return preparedsource.Set{}
	}
	return artifact.sources
}

// Identity returns detached metadata for the generated artifact.
func (artifact *PreparedGoArtifact) Identity() PreparedGoIdentity {
	if artifact == nil {
		return PreparedGoIdentity{}
	}
	identity := artifact.identity
	identity.Bundle.ProtoCounts = append([]uint32(nil), identity.Bundle.ProtoCounts...)
	return identity
}

// WriteTo writes the immutable generated source and implements io.WriterTo.
func (artifact *PreparedGoArtifact) WriteTo(writer io.Writer) (int64, error) {
	if artifact == nil {
		return 0, fmt.Errorf("prepared Go artifact: nil artifact")
	}
	if preparedGoWriterIsNil(writer) {
		return 0, fmt.Errorf("prepared Go artifact: nil writer")
	}
	source, err := artifact.legacySource()
	if err != nil {
		return 0, fmt.Errorf("prepared Go artifact: %w", err)
	}
	written, err := io.WriteString(writer, source)
	if err != nil {
		return int64(written), fmt.Errorf("prepared Go artifact: %w", err)
	}
	if written != len(source) {
		return int64(written), fmt.Errorf("prepared Go artifact: %w", io.ErrShortWrite)
	}
	return int64(written), nil
}

// legacySource is the temporary singular-byte adapter retained for the V1
// worker builder and WritePreparedGo. The Set remains the sole retained owner.
func (artifact *PreparedGoArtifact) legacySource() (string, error) {
	files := artifact.sources.Files()
	if len(files) != 1 || files[0].Name != preparedGeneratedGoName || files[0].Kind != preparedsource.GoFile {
		return "", fmt.Errorf("invalid generated source Set")
	}
	return files[0].Content, nil
}

// GeneratePreparedGo derives one immutable generated source and identity from
// p. No filesystem or toolchain effect occurs.
func (p *Program) GeneratePreparedGo(options PreparedGoOptions) (*PreparedGoArtifact, error) {
	artifact, err := p.generatePreparedGo(options)
	if err != nil {
		return nil, fmt.Errorf("generate prepared Go: %w", err)
	}
	return artifact, nil
}

// WritePreparedGo writes one deterministic Go file containing a PreparedBundle
// for p. Generation is a build-time operation; Runtime never invokes the Go
// toolchain or loads code dynamically.
func (p *Program) WritePreparedGo(writer io.Writer, options PreparedGoOptions) error {
	if preparedGoWriterIsNil(writer) {
		return fmt.Errorf("write prepared Go: nil writer")
	}
	artifact, err := p.GeneratePreparedGo(options)
	if err != nil {
		return fmt.Errorf("write prepared Go: %w", err)
	}
	if _, err := artifact.WriteTo(writer); err != nil {
		return fmt.Errorf("write prepared Go: %w", err)
	}
	return nil
}

func (p *Program) generatePreparedGo(options PreparedGoOptions) (*PreparedGoArtifact, error) {
	if p == nil {
		return nil, fmt.Errorf("nil Program")
	}
	if !token.IsIdentifier(options.Package) || token.Lookup(options.Package).IsKeyword() {
		return nil, fmt.Errorf("invalid package name %q", options.Package)
	}
	maxBytes := options.MaxBytes
	if maxBytes < 0 {
		return nil, fmt.Errorf("negative MaxBytes %d", maxBytes)
	}
	if maxBytes == 0 {
		maxBytes = defaultPreparedGoMaxBytes
	}
	image, err := p.preparedProgramImage()
	if err != nil {
		return nil, fmt.Errorf("prepare Program image: %w", err)
	}
	programIR, err := buildBackendProgramIR(image)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("module %d: %w", moduleIndex, err)
		}
		modules[moduleIndex] = generated
		files = append(files, generated.files...)
	}
	var recipe *preparedProgramRecipeEmission
	if options.EmbedProgramRecipe {
		recipe, err = buildPreparedProgramRecipeEmission(p)
		if err != nil {
			return nil, fmt.Errorf("build Program recipe: %w", err)
		}
	}
	source, err := assemblePreparedGoSource(options.Package, programIR, modules, files, recipe)
	if err != nil {
		return nil, err
	}
	if len(source) > maxBytes {
		return nil, fmt.Errorf("generated source is %d bytes, exceeds MaxBytes %d", len(source), maxBytes)
	}
	identity := PreparedGoIdentity{
		Bundle: PreparedBundleIdentity{
			ABIVersion:      programIR.abiVersion,
			SemanticVersion: programIR.semanticVersion,
			ProgramHash:     programIR.programHash,
			ProtoCounts:     make([]uint32, len(modules)),
		},
		GeneratedDigest: sha256.Sum256(source),
	}
	for index, module := range modules {
		identity.Bundle.ProtoCounts[index] = uint32(len(module.functions))
	}
	if recipe != nil {
		identity.ProgramRecipeDigest = recipe.digest
	}
	sources, err := preparedsource.NewSet(options.Package, []preparedsource.File{{
		Name:    preparedGeneratedGoName,
		Kind:    preparedsource.GoFile,
		Content: string(source),
	}})
	if err != nil {
		return nil, fmt.Errorf("construct generated source Set: %w", err)
	}
	return &PreparedGoArtifact{sources: sources, identity: identity}, nil
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
	recipes ...*preparedProgramRecipeEmission,
) ([]byte, error) {
	var recipe *preparedProgramRecipeEmission
	if len(recipes) != 0 {
		recipe = recipes[0]
	}
	fileSet := token.NewFileSet()
	imports := map[string]*ast.ImportSpec{
		"emberapi\x00\"github.com/besmpl/ember\"": {
			Name: ast.NewIdent("emberapi"),
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"github.com/besmpl/ember"`},
		},
	}
	if recipe != nil {
		imports["\x00\"context\""] = &ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: `"context"`},
		}
	}
	declarations := make([]ast.Decl, 0)
	needsPreparedStringKey := false
	for index, generated := range files {
		parsed, err := goparser.ParseFile(fileSet, generated.name+strconv.Itoa(index), generated.source, 0)
		if err != nil {
			return nil, fmt.Errorf("parse generated Proto %d: %w", generated.protoID, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && identifier.Name == "backendPreparedStringKey" {
				needsPreparedStringKey = true
			}
			return true
		})
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
	if needsPreparedStringKey {
		declarations = append([]ast.Decl{preparedStringKeyDeclaration()}, declarations...)
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
	if recipe != nil {
		writePreparedProgramRecipeDeclaration(&source, recipe)
	}
	formatted, err := format.Source(source.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated package: %w", err)
	}
	return append([]byte("// Code generated by Ember's prepared compiler; DO NOT EDIT.\n\n"), formatted...), nil
}

func preparedStringKeyDeclaration() ast.Decl {
	return &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{&ast.TypeSpec{
			Name: ast.NewIdent("backendPreparedStringKey"),
			Type: &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
				{Names: []*ast.Ident{ast.NewIdent("first")}, Type: ast.NewIdent("int32")},
				{Names: []*ast.Ident{ast.NewIdent("second")}, Type: ast.NewIdent("int32")},
			}}},
		}},
	}
}

type preparedProgramRecipeEmission struct {
	modules     []PreparedProgramModule
	entrypoints []Entrypoint
	digest      [32]byte
}

func buildPreparedProgramRecipeEmission(program *Program) (*preparedProgramRecipeEmission, error) {
	keys := make([]moduleKey, 0, len(program.graph.Nodes))
	for key := range program.graph.Nodes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(first, second int) bool {
		return keys[first].String() < keys[second].String()
	})
	modules := make([]PreparedProgramModule, len(keys))
	for index, key := range keys {
		node := program.graph.Nodes[key]
		modules[index] = PreparedProgramModule{
			Module:     moduleIDFromKey(key),
			SourceName: node.Source.Name,
			SourceText: node.Source.Text,
		}
	}
	entrypoints := make([]Entrypoint, len(program.entrypoints))
	for index, entrypoint := range program.entrypoints {
		entrypoints[index] = Entrypoint{
			Name:   entrypoint.name,
			Module: moduleIDFromKey(entrypoint.key),
		}
	}
	recipe, err := NewPreparedProgramRecipe(modules, entrypoints)
	if err != nil {
		return nil, err
	}
	return &preparedProgramRecipeEmission{
		modules: modules, entrypoints: entrypoints, digest: recipe.Identity().Digest,
	}, nil
}

func writePreparedProgramRecipeDeclaration(
	source *bytes.Buffer,
	recipe *preparedProgramRecipeEmission,
) {
	source.WriteString("\nfunc LoadProgram(ctx context.Context, options emberapi.ProgramOptions) (*emberapi.Program, emberapi.LoadReport, error) {\n")
	source.WriteString("recipe, err := emberapi.NewPreparedProgramRecipe([]emberapi.PreparedProgramModule{")
	for _, module := range recipe.modules {
		source.WriteString("{Module: ")
		writePreparedProgramModuleID(source, module.Module)
		fmt.Fprintf(source, ", SourceName: %q, SourceText: %q},", module.SourceName, module.SourceText)
	}
	source.WriteString("}, []emberapi.Entrypoint{")
	for _, entrypoint := range recipe.entrypoints {
		fmt.Fprintf(source, "{Name: %q, Module: ", entrypoint.Name)
		writePreparedProgramModuleID(source, entrypoint.Module)
		source.WriteString("},")
	}
	source.WriteString("})\nif err != nil { return nil, emberapi.LoadReport{}, err }\nreturn recipe.Load(ctx, options)\n}\n")
	source.WriteString("\nvar ProgramRecipeDigest = [32]byte{")
	for index, value := range recipe.digest {
		if index != 0 {
			source.WriteString(", ")
		}
		fmt.Fprintf(source, "0x%02x", value)
	}
	source.WriteString("}\n")
}

func writePreparedProgramModuleID(source *bytes.Buffer, module ModuleID) {
	switch module.kind {
	case ModuleLogical:
		fmt.Fprintf(source, "emberapi.LogicalModule(%q)", module.path)
	case ModuleHost:
		fmt.Fprintf(source, "emberapi.HostModule(%q)", module.path)
	default:
		panic("invalid prepared Program recipe module")
	}
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
