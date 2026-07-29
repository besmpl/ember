package ember

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// PreparedProgramModule is one ordered source input retained by a
// PreparedProgramRecipe.
type PreparedProgramModule struct {
	Module     ModuleID
	SourceName string
	SourceText string
}

// PreparedProgramModuleIdentity is deterministic source identity metadata for
// one ordered recipe module. It does not expose the source text.
type PreparedProgramModuleIdentity struct {
	Module       ModuleID
	SourceName   string
	SourceBytes  uint64
	SourceDigest [sha256.Size]byte
}

// PreparedProgramIdentity describes the ordered inputs bound by a recipe.
// Digest changes when module order, source identity, or entrypoint order
// changes. Returned slices are detached from the recipe.
type PreparedProgramIdentity struct {
	Digest      [sha256.Size]byte
	Modules     []PreparedProgramModuleIdentity
	Entrypoints []Entrypoint
}

// PreparedProgramRecipe is an immutable, reconstructible set of Program
// inputs suitable for embedding beside a generated PreparedBundle.
type PreparedProgramRecipe struct {
	modules     []PreparedProgramModule
	entrypoints []Entrypoint
	identity    PreparedProgramIdentity
}

// NewPreparedProgramRecipe copies the ordered module and entrypoint inputs.
func NewPreparedProgramRecipe(
	modules []PreparedProgramModule,
	entrypoints []Entrypoint,
) (*PreparedProgramRecipe, error) {
	if len(modules) == 0 {
		return nil, fmt.Errorf("prepared program recipe: no modules")
	}
	if len(entrypoints) == 0 {
		return nil, fmt.Errorf("prepared program recipe: no entrypoints")
	}
	copiedModules := make([]PreparedProgramModule, len(modules))
	declared := make(map[ModuleID]struct{}, len(modules))
	for index, module := range modules {
		key, err := moduleKeyFromID(module.Module)
		if err != nil {
			return nil, fmt.Errorf("prepared program recipe: module %d: %w", index, err)
		}
		module.Module = moduleIDFromKey(key)
		if module.SourceName == "" {
			return nil, fmt.Errorf("prepared program recipe: module %s has empty source name", module.Module)
		}
		if _, ok := declared[module.Module]; ok {
			return nil, fmt.Errorf("prepared program recipe: duplicate module %s", module.Module)
		}
		declared[module.Module] = struct{}{}
		copiedModules[index] = module
	}
	copiedEntrypoints := make([]Entrypoint, len(entrypoints))
	names := make(map[string]struct{}, len(entrypoints))
	for index, entrypoint := range entrypoints {
		if entrypoint.Name == "" {
			return nil, fmt.Errorf("prepared program recipe: empty entrypoint name at index %d", index)
		}
		if _, ok := names[entrypoint.Name]; ok {
			return nil, fmt.Errorf("prepared program recipe: duplicate entrypoint %q", entrypoint.Name)
		}
		key, err := moduleKeyFromID(entrypoint.Module)
		if err != nil {
			return nil, fmt.Errorf("prepared program recipe: entrypoint %q: %w", entrypoint.Name, err)
		}
		entrypoint.Module = moduleIDFromKey(key)
		if _, ok := declared[entrypoint.Module]; !ok {
			return nil, fmt.Errorf("prepared program recipe: entrypoint %q module %s is not declared", entrypoint.Name, entrypoint.Module)
		}
		names[entrypoint.Name] = struct{}{}
		copiedEntrypoints[index] = entrypoint
	}
	return &PreparedProgramRecipe{
		modules:     copiedModules,
		entrypoints: copiedEntrypoints,
		identity:    preparedProgramRecipeIdentity(copiedModules, copiedEntrypoints),
	}, nil
}

// Identity returns deterministic, detached metadata for the recipe's ordered
// source and entrypoint inputs.
func (recipe *PreparedProgramRecipe) Identity() PreparedProgramIdentity {
	if recipe == nil {
		return PreparedProgramIdentity{}
	}
	identity := recipe.identity
	identity.Modules = append([]PreparedProgramModuleIdentity(nil), identity.Modules...)
	identity.Entrypoints = append([]Entrypoint(nil), identity.Entrypoints...)
	return identity
}

// Load reconstructs the exact Program described by the recipe. Entrypoints
// belong to the recipe and must not also be supplied in options.
func (recipe *PreparedProgramRecipe) Load(
	ctx context.Context,
	options ProgramOptions,
) (*Program, LoadReport, error) {
	if recipe == nil {
		return nil, LoadReport{}, fmt.Errorf("load prepared program recipe: nil recipe")
	}
	if len(options.Entrypoints) != 0 {
		return nil, LoadReport{}, fmt.Errorf("load prepared program recipe: options entrypoints must be empty")
	}
	loader := preparedProgramRecipeLoader{sources: make(map[ModuleID]Source, len(recipe.modules))}
	for _, module := range recipe.modules {
		loader.sources[module.Module] = Source{Name: module.SourceName, Text: module.SourceText}
	}
	options.Entrypoints = append([]Entrypoint(nil), recipe.entrypoints...)
	program, report, err := LoadProgram(ctx, loader, options)
	if err != nil {
		return nil, report, fmt.Errorf("load prepared program recipe: %w", err)
	}
	return program, report, nil
}

type preparedProgramRecipeLoader struct {
	sources map[ModuleID]Source
}

func (loader preparedProgramRecipeLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	source, ok := loader.sources[id]
	if !ok {
		return Source{}, fmt.Errorf("module %s is not declared in prepared program recipe", id)
	}
	return source, nil
}

func preparedProgramRecipeIdentity(
	modules []PreparedProgramModule,
	entrypoints []Entrypoint,
) PreparedProgramIdentity {
	identity := PreparedProgramIdentity{
		Modules:     make([]PreparedProgramModuleIdentity, len(modules)),
		Entrypoints: append([]Entrypoint(nil), entrypoints...),
	}
	digest := sha256.New()
	writePreparedProgramIdentityString(digest, "ember-prepared-program-recipe-v1")
	writePreparedProgramIdentityUint64(digest, uint64(len(modules)))
	for index, module := range modules {
		sourceDigest := sha256.Sum256([]byte(module.SourceText))
		identity.Modules[index] = PreparedProgramModuleIdentity{
			Module:       module.Module,
			SourceName:   module.SourceName,
			SourceBytes:  uint64(len(module.SourceText)),
			SourceDigest: sourceDigest,
		}
		writePreparedProgramIdentityString(digest, module.Module.String())
		writePreparedProgramIdentityString(digest, module.SourceName)
		writePreparedProgramIdentityUint64(digest, uint64(len(module.SourceText)))
		_, _ = digest.Write(sourceDigest[:])
	}
	writePreparedProgramIdentityUint64(digest, uint64(len(entrypoints)))
	for _, entrypoint := range entrypoints {
		writePreparedProgramIdentityString(digest, entrypoint.Name)
		writePreparedProgramIdentityString(digest, entrypoint.Module.String())
	}
	copy(identity.Digest[:], digest.Sum(nil))
	return identity
}

type preparedProgramIdentityWriter interface {
	Write([]byte) (int, error)
}

func writePreparedProgramIdentityString(writer preparedProgramIdentityWriter, value string) {
	writePreparedProgramIdentityUint64(writer, uint64(len(value)))
	_, _ = writer.Write([]byte(value))
}

func writePreparedProgramIdentityUint64(writer preparedProgramIdentityWriter, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}
