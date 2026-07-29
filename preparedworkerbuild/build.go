// Package preparedworkerbuild owns deterministic construction and publication
// of supervised static-AOT worker executables. Runtime callers consume only the
// opaque manifest returned here through preparedworker.OpenBuild.
package preparedworkerbuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/besmpl/ember"
	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
	workercontract "github.com/besmpl/ember/internal/preparedworkercontract"
	"github.com/besmpl/ember/preparedworker"
)

const (
	maximumGeneratedBytes = int64(128 << 20)
	maximumSourceBytes    = int64(1 << 30)
	maximumGoOutputBytes  = 64 << 20
	maximumBuildLogBytes  = 1 << 20
	maximumModuleFile     = int64(4 << 20)
	maximumToolchainFile  = int64(512 << 20)
	maximumIncludeBytes   = int64(64 << 20)
	maximumIncludeFiles   = 4096
	maximumToolDirBytes   = int64(2 << 30)
	maximumToolDirFiles   = 4096
	maximumListedPackages = 65536
	maximumSourceEntries  = 262144
	maximumListedModules  = 16384
	maximumReplaceDepth   = 16
	maximumBuildTags      = 256
	maximumProtoCounts    = 65536
	workerBuildIDSymbol   = "github.com/besmpl/ember/preparedworker.workerBuildIDHex"
)

// Program binds one immutable in-memory generated artifact to the virtual Go
// source path imported by a worker package. Build overlays that path without
// mutating the application module.
type Program struct {
	Artifact    *ember.PreparedGoArtifact
	GeneratedGo string
}

// Target is one supported portable no-cgo worker target.
type Target struct {
	GOOS   string
	GOARCH string
}

// Options fixes every input and output of one deterministic worker build.
// Relative paths are resolved from ModuleDir; the process working directory is
// never an implicit input.
type Options[Q, R, C, E any] struct {
	// GoCommand names the exact Go executable used for discovery and building.
	// It is resolved to a real regular executable before any Go command runs.
	GoCommand          string
	ModuleDir          string
	Program            Program
	Contract           preparedworker.Contract[Q, R, C, E]
	WorkerPackage      string
	Target             Target
	Tags               []string
	OutputDir          string
	Name               string
	MaxExecutableBytes int64
}

// NewOptions preserves type inference while binding the actual application
// contract at construction. All remaining fields stay explicit.
func NewOptions[Q, R, C, E any](
	contract preparedworker.Contract[Q, R, C, E],
) Options[Q, R, C, E] {
	return Options[Q, R, C, E]{Contract: contract}
}

// Result reports only the selected manifest and whether an existing verified
// content-addressed executable satisfied the build.
type Result struct {
	Manifest string
	Cached   bool
}

type plan struct {
	moduleDir          string
	generatedGo        string
	workerPackage      string
	target             Target
	tags               []string
	outputDir          string
	manifest           string
	maxExecutableBytes int64
	artifact           *ember.PreparedGoArtifact
	bundle             ember.PreparedBundleIdentity
	recipeDigest       workerartifact.Digest
	generatedDigest    workerartifact.Digest
	contract           workerartifact.Digest
	policy             buildPolicy
	overlay            string
	generatedSource    string
}

// buildPolicy is the single immutable description of the Go build effect.
// Its argument prefixes are also the canonical strings stored in Descriptor.
type buildPolicy struct {
	goCommand   string
	environment []string
	listFlags   []string
	buildFlags  []string
	identity    []string
	goRoot      string
	goToolDir   string
	goVersion   string
}

type programOverlay struct {
	directory string
}

func createProgramOverlay(buildPlan *plan) (*programOverlay, error) {
	if buildPlan == nil || buildPlan.artifact == nil {
		return nil, fmt.Errorf("prepared worker build: generated artifact is required")
	}
	directory, err := os.MkdirTemp(buildPlan.outputDir, ".ember-worker-overlay-*")
	if err != nil {
		return nil, fmt.Errorf("prepared worker build: create generated overlay: %w", err)
	}
	overlay := &programOverlay{directory: directory}
	removeOnFailure := true
	defer func() {
		if removeOnFailure {
			_ = overlay.Close()
		}
	}()

	source := filepath.Join(directory, "prepared_generated.go")
	file, err := os.OpenFile(source, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("prepared worker build: create generated overlay source: %w", err)
	}
	_, writeErr := buildPlan.artifact.WriteTo(file)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return nil, fmt.Errorf("prepared worker build: write generated overlay source: %w", err)
	}
	digest, err := hashRegularFile(source, maximumGeneratedBytes)
	if err != nil {
		return nil, fmt.Errorf("prepared worker build: verify generated overlay source: %w", err)
	}
	if digest != buildPlan.generatedDigest {
		return nil, fmt.Errorf("prepared worker build: generated source differs from its immutable identity")
	}

	configuration, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: map[string]string{buildPlan.generatedGo: source}})
	if err != nil {
		return nil, fmt.Errorf("prepared worker build: encode generated overlay: %w", err)
	}
	config := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(config, configuration, 0o600); err != nil {
		return nil, fmt.Errorf("prepared worker build: write generated overlay: %w", err)
	}
	if err := syncRegularFile(config); err != nil {
		return nil, fmt.Errorf("prepared worker build: sync generated overlay: %w", err)
	}
	buildPlan.overlay = config
	buildPlan.generatedSource = source
	removeOnFailure = false
	return overlay, nil
}

func (overlay *programOverlay) Close() error {
	if overlay == nil || overlay.directory == "" {
		return nil
	}
	directory := overlay.directory
	overlay.directory = ""
	return os.RemoveAll(directory)
}

// Build validates all inputs, derives the complete build identity, reuses or
// constructs one immutable cache entry, and publishes the canonical manifest
// last. It performs no runtime activation or cache eviction.
func Build[Q, R, C, E any](
	ctx context.Context,
	options Options[Q, R, C, E],
) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("prepared worker build: nil context")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	buildPlan, err := makePlan(ctx, options)
	if err != nil {
		return Result{}, err
	}
	overlay, err := createProgramOverlay(&buildPlan)
	if err != nil {
		return Result{}, err
	}
	defer overlay.Close()

	inputs, err := captureBuildInputs(ctx, buildPlan)
	if err != nil {
		return Result{}, err
	}
	descriptor := workerartifact.Descriptor{
		FormatVersion:       workerartifact.DescriptorVersion,
		ProtocolVersion:     workerartifact.ProtocolVersion,
		Contract:            buildPlan.contract,
		ProgramRecipeDigest: buildPlan.recipeDigest,
		PreparedABIVersion:  buildPlan.bundle.ABIVersion,
		SemanticVersion:     buildPlan.bundle.SemanticVersion,
		ProgramHash:         workerartifact.Digest(buildPlan.bundle.ProgramHash),
		ProtoCounts:         append([]uint32(nil), buildPlan.bundle.ProtoCounts...),
		GeneratedDigest:     inputs.generated,
		WrapperDigest:       inputs.wrapper,
		EmberVersion:        "module-graph-sha256:" + inputs.modules.String(),
		Toolchain:           inputs.toolchain,
		TargetOS:            buildPlan.target.GOOS,
		TargetArch:          buildPlan.target.GOARCH,
		BuildTags:           append([]string(nil), buildPlan.tags...),
		BuildFlags:          append([]string(nil), buildPlan.policy.identity...),
	}
	buildID, err := descriptor.BuildID()
	if err != nil {
		return Result{}, err
	}

	cacheBase := filepath.Join(buildPlan.outputDir, ".ember-worker-cache")
	if err := ensureRealDirectory(cacheBase); err != nil {
		return Result{}, fmt.Errorf("prepared worker build: create cache root: %w", err)
	}
	cacheRoot := filepath.Join(cacheBase, buildPlan.target.GOOS+"-"+buildPlan.target.GOARCH)
	if err := ensureRealDirectory(cacheRoot); err != nil {
		return Result{}, fmt.Errorf("prepared worker build: create target cache: %w", err)
	}
	entry := filepath.Join(cacheRoot, buildID.String())
	executableName := "worker"
	if buildPlan.target.GOOS == "windows" {
		executableName += ".exe"
	}
	executable := filepath.Join(entry, executableName)
	entryManifest := filepath.Join(entry, "build.json")

	exists, err := cacheEntryExists(entry)
	if err != nil {
		return Result{}, err
	}
	if exists {
		if err := verifyCacheEntry(entryManifest, executable, descriptor, buildPlan.maxExecutableBytes); err != nil {
			return Result{}, err
		}
		if err := publishSelection(buildPlan, executable, descriptor); err != nil {
			return Result{}, err
		}
		return Result{Manifest: buildPlan.manifest, Cached: true}, nil
	}

	temporary, err := os.MkdirTemp(cacheRoot, ".worker-build-*")
	if err != nil {
		return Result{}, fmt.Errorf("prepared worker build: create cache temporary: %w", err)
	}
	temporaryOwned := true
	defer func() {
		if temporaryOwned {
			_ = os.Chmod(temporary, 0o755)
			_ = os.RemoveAll(temporary)
		}
	}()
	temporaryExecutable := filepath.Join(temporary, executableName)
	if err := buildExecutable(ctx, buildPlan, buildID, temporaryExecutable); err != nil {
		return Result{}, err
	}
	if err := verifyBuildInputs(ctx, buildPlan, descriptor); err != nil {
		return Result{}, err
	}
	if err := inspectExecutable(temporaryExecutable, buildPlan, descriptor.Toolchain); err != nil {
		return Result{}, err
	}
	if _, err := hashRegularFile(temporaryExecutable, buildPlan.maxExecutableBytes); err != nil {
		return Result{}, fmt.Errorf("prepared worker build: built executable: %w", err)
	}
	if err := syncRegularFile(temporaryExecutable); err != nil {
		return Result{}, fmt.Errorf("prepared worker build: sync executable: %w", err)
	}
	if err := os.Chmod(temporaryExecutable, 0o555); err != nil {
		return Result{}, fmt.Errorf("prepared worker build: make executable immutable: %w", err)
	}
	temporaryManifest := filepath.Join(temporary, "build.json")
	if err := workerartifact.Publish(
		temporaryManifest,
		temporaryExecutable,
		descriptor,
		buildPlan.maxExecutableBytes,
	); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(temporaryManifest, 0o444); err != nil {
		return Result{}, fmt.Errorf("prepared worker build: make cache manifest immutable: %w", err)
	}
	cached := false
	if err := os.Rename(temporary, entry); err != nil {
		exists, queryErr := cacheEntryExists(entry)
		if queryErr != nil {
			return Result{}, errors.Join(
				fmt.Errorf("prepared worker build: publish cache entry: %w", err),
				queryErr,
			)
		}
		if !exists {
			return Result{}, fmt.Errorf("prepared worker build: publish cache entry: %w", err)
		}
		if verifyErr := verifyCacheEntry(entryManifest, executable, descriptor, buildPlan.maxExecutableBytes); verifyErr != nil {
			return Result{}, errors.Join(
				fmt.Errorf("prepared worker build: concurrent cache publication: %w", err),
				verifyErr,
			)
		}
		temporaryDigest, digestErr := hashRegularFile(temporaryExecutable, buildPlan.maxExecutableBytes)
		if digestErr != nil {
			return Result{}, digestErr
		}
		publishedDigest, digestErr := hashRegularFile(executable, buildPlan.maxExecutableBytes)
		if digestErr != nil {
			return Result{}, digestErr
		}
		if temporaryDigest != publishedDigest {
			return Result{}, fmt.Errorf("prepared worker build: identical build identity produced another executable")
		}
		cached = true
	} else {
		temporaryOwned = false
		if err := syncDirectory(cacheRoot); err != nil {
			return Result{}, fmt.Errorf("prepared worker build: sync cache directory: %w", err)
		}
	}

	if err := publishSelection(buildPlan, executable, descriptor); err != nil {
		return Result{}, err
	}
	return Result{Manifest: buildPlan.manifest, Cached: cached}, nil
}

func makePlan[Q, R, C, E any](ctx context.Context, options Options[Q, R, C, E]) (plan, error) {
	moduleDir, err := canonicalDirectory(options.ModuleDir, "module")
	if err != nil {
		return plan{}, err
	}
	goMod := filepath.Join(moduleDir, "go.mod")
	if err := requireRegularFile(goMod); err != nil {
		return plan{}, fmt.Errorf("prepared worker build: module go.mod: %w", err)
	}
	generatedGo, err := resolveInputFile(moduleDir, options.Program.GeneratedGo)
	if err != nil {
		return plan{}, fmt.Errorf("prepared worker build: generated source: %w", err)
	}
	if !within(moduleDir, generatedGo) {
		return plan{}, fmt.Errorf("prepared worker build: generated source must be inside ModuleDir")
	}
	if !canonicalLocalPackage(options.WorkerPackage) {
		return plan{}, fmt.Errorf("prepared worker build: worker package must be one canonical local package")
	}
	if err := validateTarget(options.Target); err != nil {
		return plan{}, err
	}
	tags, err := canonicalTags(options.Tags)
	if err != nil {
		return plan{}, err
	}
	if options.MaxExecutableBytes <= 0 || options.MaxExecutableBytes > 1<<30 {
		return plan{}, fmt.Errorf("prepared worker build: executable byte limit is out of bounds")
	}
	if !canonicalName(options.Name) {
		return plan{}, fmt.Errorf("prepared worker build: name must contain only letters, digits, '.', '_', or '-'")
	}
	outputDir, err := resolveExistingDirectory(moduleDir, options.OutputDir, "output")
	if err != nil {
		return plan{}, err
	}
	if options.Program.Artifact == nil {
		return plan{}, fmt.Errorf("prepared worker build: generated artifact is required")
	}
	identity := options.Program.Artifact.Identity()
	bundle := identity.Bundle
	if bundle.ABIVersion == 0 || bundle.SemanticVersion == 0 ||
		bundle.ProgramHash == ([sha256.Size]byte{}) || len(bundle.ProtoCounts) == 0 {
		return plan{}, fmt.Errorf("prepared worker build: prepared bundle identity is incomplete")
	}
	for _, count := range bundle.ProtoCounts {
		if count == 0 {
			return plan{}, fmt.Errorf("prepared worker build: prepared bundle contains an empty module inventory")
		}
	}
	if identity.ProgramRecipeDigest == ([sha256.Size]byte{}) {
		return plan{}, fmt.Errorf("prepared worker build: program recipe digest is zero")
	}
	if identity.GeneratedDigest == ([sha256.Size]byte{}) {
		return plan{}, fmt.Errorf("prepared worker build: generated source digest is zero")
	}
	if len(bundle.ProtoCounts) > maximumProtoCounts {
		return plan{}, fmt.Errorf("prepared worker build: prepared bundle inventory is too large")
	}
	contract, err := workercontract.Derive(buildContractSpecification(options.Contract))
	if err != nil {
		return plan{}, fmt.Errorf("prepared worker build: typed contract: %w", err)
	}
	policy, err := newBuildPolicy(ctx, moduleDir, options.GoCommand, options.Target, tags)
	if err != nil {
		return plan{}, err
	}
	return plan{
		moduleDir:          moduleDir,
		generatedGo:        generatedGo,
		workerPackage:      options.WorkerPackage,
		target:             options.Target,
		tags:               tags,
		outputDir:          outputDir,
		manifest:           filepath.Join(outputDir, options.Name+".json"),
		maxExecutableBytes: options.MaxExecutableBytes,
		artifact:           options.Program.Artifact,
		bundle:             bundle,
		recipeDigest:       workerartifact.Digest(identity.ProgramRecipeDigest),
		generatedDigest:    workerartifact.Digest(identity.GeneratedDigest),
		contract:           contract.Contract,
		policy:             policy,
	}, nil
}

func buildContractSpecification[Q, R, C, E any](
	contract preparedworker.Contract[Q, R, C, E],
) workercontract.Specification {
	return workercontract.Specification{
		CodecVersion: contract.CodecVersion,
		Request: workercontract.Schema{
			Name: contract.Request.Name, MaxBytes: contract.Request.MaxBytes, CodecPresent: contract.Request.Codec != nil,
		},
		Result: workercontract.Schema{
			Name: contract.Result.Name, MaxBytes: contract.Result.MaxBytes, CodecPresent: contract.Result.Codec != nil,
		},
		Checkpoint: workercontract.Schema{
			Name: contract.Checkpoint.Name, MaxBytes: contract.Checkpoint.MaxBytes, CodecPresent: contract.Checkpoint.Codec != nil,
		},
		Effect: workercontract.Schema{
			Name: contract.Effect.Name, MaxBytes: contract.Effect.MaxBytes, CodecPresent: contract.Effect.Codec != nil,
		},
		MaxEffects: contract.MaxEffects,
	}
}

func buildExecutable(ctx context.Context, buildPlan plan, buildID workerartifact.Digest, output string) error {
	arguments := append([]string{"build"}, buildPlan.policy.buildFlags...)
	arguments = append(arguments, "-overlay="+buildPlan.overlay)
	arguments = append(
		arguments,
		"-ldflags=-X="+workerBuildIDSymbol+"="+buildID.String(),
		"-o", output,
		buildPlan.workerPackage,
	)
	_, err := runGo(ctx, buildPlan, maximumBuildLogBytes, arguments...)
	if err != nil {
		return fmt.Errorf("prepared worker build: compile worker: %w", err)
	}
	return nil
}

type buildInputs struct {
	toolchain string
	wrapper   workerartifact.Digest
	modules   workerartifact.Digest
	generated workerartifact.Digest
}

func captureBuildInputs(ctx context.Context, buildPlan plan) (buildInputs, error) {
	wrapper, standard, err := sourceClosureDigests(ctx, buildPlan)
	if err != nil {
		return buildInputs{}, err
	}
	toolchain, err := goToolchain(ctx, buildPlan, standard)
	if err != nil {
		return buildInputs{}, err
	}
	modules, err := moduleGraphDigest(ctx, buildPlan)
	if err != nil {
		return buildInputs{}, err
	}
	generated, err := hashRegularFile(buildPlan.generatedSource, maximumGeneratedBytes)
	if err != nil {
		return buildInputs{}, fmt.Errorf("prepared worker build: generated source: %w", err)
	}
	if generated != buildPlan.generatedDigest {
		return buildInputs{}, fmt.Errorf("prepared worker build: generated source differs from its immutable identity")
	}
	return buildInputs{toolchain: toolchain, wrapper: wrapper, modules: modules, generated: generated}, nil
}

func verifyBuildInputs(ctx context.Context, buildPlan plan, descriptor workerartifact.Descriptor) error {
	inputs, err := captureBuildInputs(ctx, buildPlan)
	if err != nil {
		return err
	}
	if inputs.toolchain != descriptor.Toolchain || inputs.wrapper != descriptor.WrapperDigest ||
		inputs.generated != descriptor.GeneratedDigest ||
		"module-graph-sha256:"+inputs.modules.String() != descriptor.EmberVersion {
		return fmt.Errorf("prepared worker build: inputs changed while the worker was compiling")
	}
	return nil
}

func inspectExecutable(executable string, buildPlan plan, toolchain string) error {
	info, err := buildinfo.ReadFile(executable)
	if err != nil {
		return fmt.Errorf("prepared worker build: inspect executable metadata: %w", err)
	}
	version, _, found := strings.Cut(toolchain, "+sha256:")
	if !found || toolchainDigestSuffix(toolchain) == "" || info.GoVersion != version || info.Path == "" {
		return fmt.Errorf("prepared worker build: executable toolchain or package identity differs")
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != buildPlan.target.GOOS || settings["GOARCH"] != buildPlan.target.GOARCH ||
		settings["CGO_ENABLED"] != "0" {
		return fmt.Errorf("prepared worker build: executable target or cgo setting differs")
	}
	featureKey, baseline := "GOAMD64", "v1"
	if buildPlan.target.GOARCH == "arm64" {
		featureKey, baseline = "GOARM64", "v8.0"
	}
	if settings[featureKey] != baseline {
		return fmt.Errorf("prepared worker build: executable target feature baseline differs")
	}
	if settings["-buildmode"] != "exe" || settings["-compiler"] != "gc" || settings["-trimpath"] != "true" {
		return fmt.Errorf("prepared worker build: executable build policy differs")
	}
	wantTags := strings.Join(buildPlan.tags, ",")
	if tags, present := settings["-tags"]; (wantTags == "" && present) || (wantTags != "" && tags != wantTags) {
		return fmt.Errorf("prepared worker build: executable build tags differ")
	}
	return nil
}

func toolchainDigestSuffix(identity string) string {
	_, digest, found := strings.Cut(identity, "+sha256:")
	if !found || len(digest) != sha256.Size*2 {
		return ""
	}
	for _, character := range digest {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return ""
		}
	}
	return digest
}

func goToolchain(ctx context.Context, buildPlan plan, standard workerartifact.Digest) (string, error) {
	policy := buildPlan.policy
	current, err := queryGoInstallation(ctx, policy.goCommand, buildPlan.moduleDir, policy.environment)
	if err != nil {
		return "", fmt.Errorf("prepared worker build: identify Go toolchain: %w", err)
	}
	if current.goRoot != policy.goRoot || current.goToolDir != policy.goToolDir || current.goVersion != policy.goVersion {
		return "", fmt.Errorf("prepared worker build: Go installation changed while deriving its identity")
	}
	digest, err := hashToolchain(policy, standard)
	if err != nil {
		return "", err
	}
	return policy.goVersion + "+sha256:" + digest.String(), nil
}

type listedPackage struct {
	Dir          string
	ImportPath   string
	Standard     bool
	Incomplete   bool
	GoFiles      []string
	CgoFiles     []string
	CFiles       []string
	CXXFiles     []string
	MFiles       []string
	FFiles       []string
	HFiles       []string
	SFiles       []string
	SwigFiles    []string
	SwigCXXFiles []string
	SysoFiles    []string
	EmbedFiles   []string
	Error        *struct {
		Err string
	}
}

type sourceEntry struct {
	key  string
	path string
}

type listedSourceGroup struct {
	kind  string
	files []string
}

func listedSourceGroups(listed listedPackage) []listedSourceGroup {
	return []listedSourceGroup{
		{kind: "go", files: listed.GoFiles},
		{kind: "asm", files: listed.SFiles},
		{kind: "header", files: listed.HFiles},
		{kind: "embed", files: listed.EmbedFiles},
	}
}

func sourceClosureDigests(ctx context.Context, buildPlan plan) (workerartifact.Digest, workerartifact.Digest, error) {
	arguments := append([]string{"list", "-deps", "-json"}, buildPlan.policy.listFlags...)
	arguments = append(arguments, "-overlay="+buildPlan.overlay)
	arguments = append(arguments, buildPlan.workerPackage)
	output, err := runGo(ctx, buildPlan, maximumGoOutputBytes, arguments...)
	if err != nil {
		return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: discover worker sources: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	entries := make([]sourceEntry, 0, 128)
	standardEntries := make([]sourceEntry, 0, 128)
	seenKey := make(map[string]struct{})
	foundGenerated := false
	packageCount := 0
	for {
		var listed listedPackage
		if err := decoder.Decode(&listed); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: decode go list output: %w", err)
		}
		if listed.Error != nil || listed.Incomplete {
			message := "incomplete package"
			if listed.Error != nil && listed.Error.Err != "" {
				message = listed.Error.Err
			}
			return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: package %q: %s", listed.ImportPath, message)
		}
		packageCount++
		if packageCount > maximumListedPackages {
			return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: dependency closure exceeds %d packages", maximumListedPackages)
		}
		if listed.ImportPath == "" || listed.Dir == "" {
			return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: package metadata is incomplete")
		}
		if len(listed.CgoFiles)+len(listed.CFiles)+len(listed.CXXFiles)+len(listed.MFiles)+
			len(listed.FFiles)+len(listed.SwigFiles)+len(listed.SwigCXXFiles)+len(listed.SysoFiles) != 0 {
			return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: package %q requires foreign or prebuilt sources", listed.ImportPath)
		}
		for _, group := range listedSourceGroups(listed) {
			for _, name := range group.files {
				cleaned := filepath.Clean(name)
				if cleaned == "." || cleaned != name || filepath.IsAbs(name) ||
					cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
					return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: package %q returned an unsafe file", listed.ImportPath)
				}
				virtualFile := filepath.Join(listed.Dir, cleaned)
				file := virtualFile
				if filepath.Clean(virtualFile) == buildPlan.generatedGo {
					file = buildPlan.generatedSource
					foundGenerated = true
				}
				if err := requireRegularFile(file); err != nil {
					return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: source %q: %w", listed.ImportPath+"/"+name, err)
				}
				key := listed.ImportPath + "\x00" + group.kind + "\x00" + filepath.ToSlash(cleaned)
				if _, exists := seenKey[key]; exists {
					continue
				}
				seenKey[key] = struct{}{}
				if len(seenKey) > maximumSourceEntries {
					return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: dependency closure exceeds %d source entries", maximumSourceEntries)
				}
				entry := sourceEntry{key: key, path: file}
				if listed.Standard {
					standardEntries = append(standardEntries, entry)
				} else {
					entries = append(entries, entry)
				}
			}
		}
	}
	if !foundGenerated {
		return workerartifact.Digest{}, workerartifact.Digest{}, fmt.Errorf("prepared worker build: generated source is not compiled into the worker dependency closure")
	}
	wrapper, err := hashSourceEntries(entries, maximumSourceBytes)
	if err != nil {
		return workerartifact.Digest{}, workerartifact.Digest{}, err
	}
	standard, err := hashSourceEntries(standardEntries, maximumSourceBytes)
	if err != nil {
		return workerartifact.Digest{}, workerartifact.Digest{}, err
	}
	return wrapper, standard, nil
}

func hashSourceEntries(entries []sourceEntry, limit int64) (workerartifact.Digest, error) {
	sort.Slice(entries, func(left, right int) bool { return entries[left].key < entries[right].key })
	hasher := sha256.New()
	var total int64
	for _, entry := range entries {
		metadata, err := os.Stat(entry.path)
		if err != nil {
			return workerartifact.Digest{}, fmt.Errorf("prepared worker build: stat source %q: %w", entry.key, err)
		}
		if metadata.Size() < 0 || metadata.Size() > limit-total {
			return workerartifact.Digest{}, fmt.Errorf("prepared worker build: source closure exceeds %d bytes", limit)
		}
		total += metadata.Size()
		writeFrameLength(hasher, uint64(len(entry.key)))
		_, _ = io.WriteString(hasher, entry.key)
		writeFrameLength(hasher, uint64(metadata.Size()))
		if err := copyExactFile(hasher, entry.path, metadata.Size()); err != nil {
			return workerartifact.Digest{}, fmt.Errorf("prepared worker build: hash source %q: %w", entry.key, err)
		}
	}
	var digest workerartifact.Digest
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

type listedModule struct {
	Path      string
	Version   string
	Main      bool
	GoVersion string
	Sum       string
	GoMod     string
	Replace   *listedModule
}

func moduleGraphDigest(ctx context.Context, buildPlan plan) (workerartifact.Digest, error) {
	arguments := append([]string{"list", "-m", "-json"}, buildPlan.policy.listFlags...)
	arguments = append(arguments, "all")
	output, err := runGo(ctx, buildPlan, maximumGoOutputBytes, arguments...)
	if err != nil {
		return workerartifact.Digest{}, fmt.Errorf("prepared worker build: discover module graph: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	modules := make([]listedModule, 0, 16)
	for {
		var module listedModule
		if err := decoder.Decode(&module); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return workerartifact.Digest{}, fmt.Errorf("prepared worker build: decode module graph: %w", err)
		}
		if module.Path == "" {
			return workerartifact.Digest{}, fmt.Errorf("prepared worker build: module graph contains an empty path")
		}
		modules = append(modules, module)
		if len(modules) > maximumListedModules {
			return workerartifact.Digest{}, fmt.Errorf("prepared worker build: module graph exceeds %d modules", maximumListedModules)
		}
	}
	sort.Slice(modules, func(left, right int) bool { return modules[left].Path < modules[right].Path })
	hasher := sha256.New()
	for _, module := range modules {
		if err := hashModule(hasher, module, 0); err != nil {
			return workerartifact.Digest{}, err
		}
	}
	var digest workerartifact.Digest
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func hashModule(writer io.Writer, module listedModule, depth int) error {
	if depth > maximumReplaceDepth {
		return fmt.Errorf("prepared worker build: module replacement chain exceeds %d entries", maximumReplaceDepth)
	}
	modulePath := module.Path
	if !module.Main && module.Version == "" && module.GoMod != "" {
		modulePath = "local-replacement"
	}
	fields := []string{modulePath, module.Version, module.GoVersion, module.Sum}
	if module.Main {
		fields = append(fields, "main")
	} else {
		fields = append(fields, "dependency")
	}
	for _, field := range fields {
		writeFrameLength(writer, uint64(len(field)))
		_, _ = io.WriteString(writer, field)
	}
	goModDigest := workerartifact.Digest{}
	if module.GoMod != "" {
		digest, err := hashRegularFile(module.GoMod, maximumModuleFile)
		if err != nil {
			return fmt.Errorf("prepared worker build: hash go.mod for %q: %w", module.Path, err)
		}
		goModDigest = digest
	}
	_, _ = writer.Write(goModDigest[:])
	if module.Replace == nil {
		writeFrameLength(writer, 0)
		return nil
	}
	writeFrameLength(writer, 1)
	return hashModule(writer, *module.Replace, depth+1)
}

func runGo(ctx context.Context, buildPlan plan, limit int, arguments ...string) ([]byte, error) {
	return runGoCommand(
		ctx,
		buildPlan.policy.goCommand,
		buildPlan.moduleDir,
		buildPlan.policy.environment,
		limit,
		arguments...,
	)
}

func runGoCommand(
	ctx context.Context,
	goCommand string,
	directory string,
	environment []string,
	limit int,
	arguments ...string,
) ([]byte, error) {
	command := exec.CommandContext(ctx, goCommand, arguments...)
	command.Dir = directory
	command.Env = environment
	command.WaitDelay = 5 * time.Second
	output := &boundedBuffer{remaining: limit}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if output.overflow {
		return nil, fmt.Errorf("go %s output exceeded %d bytes", arguments[0], limit)
	}
	if err != nil {
		message := strings.TrimSpace(output.String())
		if message == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, message)
	}
	return append([]byte(nil), output.Bytes()...), nil
}

type boundedBuffer struct {
	bytes.Buffer
	remaining int
	overflow  bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > buffer.remaining {
		buffer.overflow = true
		return 0, fmt.Errorf("bounded command output exceeded")
	}
	buffer.remaining -= len(data)
	return buffer.Buffer.Write(data)
}

type goInstallation struct {
	goRoot    string
	goToolDir string
	goVersion string
}

func newBuildPolicy(ctx context.Context, moduleDir, command string, target Target, tags []string) (buildPolicy, error) {
	goCommand, err := canonicalExecutable(command)
	if err != nil {
		return buildPolicy{}, fmt.Errorf("prepared worker build: GoCommand: %w", err)
	}
	environment := seedBuildEnvironment(target)
	installation, err := queryGoInstallation(ctx, goCommand, moduleDir, environment)
	if err != nil {
		return buildPolicy{}, fmt.Errorf("prepared worker build: identify Go installation: %w", err)
	}
	environment = append(environment, "GOROOT="+installation.goRoot)
	listFlags := []string{"-mod=readonly", "-pgo=off"}
	buildFlags := []string{"-mod=readonly", "-trimpath", "-buildvcs=false", "-pgo=off", "-buildmode=exe"}
	if len(tags) != 0 {
		tagFlag := "-tags=" + strings.Join(tags, ",")
		listFlags = append(listFlags, tagFlag)
		buildFlags = append(buildFlags, tagFlag)
	}
	fixedEnvironment := fixedBuildEnvironment(target)
	identity := canonicalPolicyIdentity(buildFlags, fixedEnvironment)
	identity = append(identity,
		"-overlay=prepared-go-artifact",
		"-ldflags=-X="+workerBuildIDSymbol+"=<build-id>",
	)
	return buildPolicy{
		goCommand: goCommand, environment: environment,
		listFlags: listFlags, buildFlags: buildFlags, identity: identity,
		goRoot: installation.goRoot, goToolDir: installation.goToolDir, goVersion: installation.goVersion,
	}, nil
}

func canonicalPolicyIdentity(buildFlags, fixedEnvironment []string) []string {
	identity := make([]string, 0, 1+len(buildFlags)+len(fixedEnvironment))
	identity = append(identity, "ember-go-build-policy-v1")
	identity = append(identity, buildFlags...)
	for _, variable := range fixedEnvironment {
		identity = append(identity, "env:"+variable)
	}
	return identity
}

func canonicalExecutable(value string) (string, error) {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("explicit executable is required")
	}
	resolved := value
	if !filepath.IsAbs(resolved) {
		var err error
		resolved, err = exec.LookPath(resolved)
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	metadata, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !metadata.Mode().IsRegular() || (runtime.GOOS != "windows" && metadata.Mode().Perm()&0o111 == 0) {
		return "", fmt.Errorf("not a real executable file")
	}
	return real, nil
}

func queryGoInstallation(ctx context.Context, command, moduleDir string, environment []string) (goInstallation, error) {
	output, err := runGoCommand(
		ctx,
		command,
		moduleDir,
		environment,
		16<<10,
		"env", "-json", "GOVERSION", "GOROOT", "GOTOOLDIR",
	)
	if err != nil {
		return goInstallation{}, fmt.Errorf("go env: %w", err)
	}
	var raw struct {
		GOVERSION string
		GOROOT    string
		GOTOOLDIR string
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return goInstallation{}, fmt.Errorf("decode go env: %w", err)
	}
	if raw.GOVERSION == "" || len(raw.GOVERSION) > 4096 || strings.ContainsAny(raw.GOVERSION, "\r\n\x00") {
		return goInstallation{}, fmt.Errorf("Go version is invalid")
	}
	goRoot, err := canonicalDirectory(raw.GOROOT, "GOROOT")
	if err != nil {
		return goInstallation{}, err
	}
	goToolDir, err := canonicalDirectory(raw.GOTOOLDIR, "GOTOOLDIR")
	if err != nil {
		return goInstallation{}, err
	}
	if !within(goRoot, goToolDir) {
		return goInstallation{}, fmt.Errorf("prepared worker build: GOTOOLDIR is outside GOROOT")
	}
	return goInstallation{goRoot: goRoot, goToolDir: goToolDir, goVersion: raw.GOVERSION}, nil
}

func hashToolchain(policy buildPolicy, standard workerartifact.Digest) (workerartifact.Digest, error) {
	hasher := sha256.New()
	for _, field := range []string{"go-toolchain-v1", policy.goVersion} {
		writeFrameLength(hasher, uint64(len(field)))
		_, _ = io.WriteString(hasher, field)
	}
	goDigest, err := hashRegularFile(policy.goCommand, maximumToolchainFile)
	if err != nil {
		return workerartifact.Digest{}, fmt.Errorf("prepared worker build: hash Go executable: %w", err)
	}
	_, _ = hasher.Write(goDigest[:])
	toolDirDigest, err := hashDirectoryTree(policy.goToolDir, maximumToolDirFiles, maximumToolDirBytes)
	if err != nil {
		return workerartifact.Digest{}, fmt.Errorf("prepared worker build: hash GOTOOLDIR: %w", err)
	}
	_, _ = hasher.Write(toolDirDigest[:])
	_, _ = hasher.Write(standard[:])
	includeDigest, err := hashDirectoryTree(filepath.Join(policy.goRoot, "pkg", "include"), maximumIncludeFiles, maximumIncludeBytes)
	if err != nil {
		return workerartifact.Digest{}, fmt.Errorf("prepared worker build: hash GOROOT include tree: %w", err)
	}
	_, _ = hasher.Write(includeDigest[:])
	var digest workerartifact.Digest
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func hashDirectoryTree(root string, maximumFiles int, maximumBytes int64) (workerartifact.Digest, error) {
	if err := requireRealDirectory(root); err != nil {
		return workerartifact.Digest{}, err
	}
	entries := make([]sourceEntry, 0, 16)
	err := filepath.WalkDir(root, func(file string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if file == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("tree contains a symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("tree contains a non-regular file")
		}
		if len(entries) >= maximumFiles {
			return fmt.Errorf("tree exceeds %d files", maximumFiles)
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		entries = append(entries, sourceEntry{key: filepath.ToSlash(relative), path: file})
		return nil
	})
	if err != nil {
		return workerartifact.Digest{}, err
	}
	return hashSourceEntries(entries, maximumBytes)
}

func requireRealDirectory(path string) error {
	metadata, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
		return fmt.Errorf("not a real directory")
	}
	return nil
}

func seedBuildEnvironment(target Target) []string {
	blocked := map[string]struct{}{
		"CGO_ENABLED": {}, "GO111MODULE": {}, "GOARCH": {}, "GOENV": {},
		"GOEXPERIMENT": {}, "GOFLAGS": {}, "GOOS": {}, "GOWORK": {},
		"GOAMD64": {}, "GOARM64": {}, "GOFIPS140": {}, "GO_EXTLINK_ENABLED": {},
		"GOROOT": {}, "GOROOT_FINAL": {}, "GODEBUG": {}, "GOTOOLCHAIN": {},
		"GOCACHEPROG": {}, "GOTOOLCHAIN_INTERNAL_SWITCH_VERSION": {},
		"GOTOOLCHAIN_INTERNAL_SWITCH_COUNT": {},
	}
	environment := make([]string, 0, len(os.Environ())+10)
	for _, variable := range os.Environ() {
		name, _, found := strings.Cut(variable, "=")
		name = strings.ToUpper(name)
		if _, remove := blocked[name]; found && remove {
			continue
		}
		environment = append(environment, variable)
	}
	environment = append(environment, fixedBuildEnvironment(target)...)
	return environment
}

func fixedBuildEnvironment(target Target) []string {
	environment := []string{
		"CGO_ENABLED=0",
		"GO111MODULE=on",
		"GOARCH=" + target.GOARCH,
		"GOENV=off",
		"GOEXPERIMENT=",
		"GOFLAGS=",
		"GOFIPS140=off",
		"GO_EXTLINK_ENABLED=0",
		"GOCACHEPROG=",
		"GOOS=" + target.GOOS,
		"GODEBUG=",
		"GOTOOLCHAIN=local",
		"GOTOOLCHAIN_INTERNAL_SWITCH_VERSION=",
		"GOTOOLCHAIN_INTERNAL_SWITCH_COUNT=",
		"GOROOT_FINAL=",
		"GOWORK=off",
	}
	if target.GOARCH == "amd64" {
		environment = append(environment, "GOAMD64=v1")
	} else {
		environment = append(environment, "GOARM64=v8.0")
	}
	return environment
}

func publishSelection(buildPlan plan, executable string, descriptor workerartifact.Descriptor) error {
	if err := workerartifact.Publish(
		buildPlan.manifest,
		executable,
		descriptor,
		buildPlan.maxExecutableBytes,
	); err != nil {
		return fmt.Errorf("prepared worker build: publish selected manifest: %w", err)
	}
	return nil
}

func verifyCacheEntry(
	manifest string,
	executable string,
	descriptor workerartifact.Descriptor,
	maxExecutableBytes int64,
) error {
	for _, path := range []string{manifest, executable} {
		metadata, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("prepared worker build: inspect cache entry: %w", err)
		}
		if metadata.Mode()&os.ModeSymlink != 0 || !metadata.Mode().IsRegular() {
			return fmt.Errorf("prepared worker build: cache entry contains a non-regular file")
		}
	}
	if err := workerartifact.Verify(
		manifest,
		executable,
		descriptor,
		maxExecutableBytes,
	); err != nil {
		return fmt.Errorf("prepared worker build: verify cache entry: %w", err)
	}
	return nil
}

func cacheEntryExists(entry string) (bool, error) {
	metadata, err := os.Lstat(entry)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("prepared worker build: inspect cache entry: %w", err)
	}
	if metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
		return false, fmt.Errorf("prepared worker build: cache entry is not a real directory")
	}
	return true, nil
}

func canonicalDirectory(value, label string) (string, error) {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("prepared worker build: %s directory is required", label)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("prepared worker build: resolve %s directory: %w", label, err)
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("prepared worker build: resolve %s directory links: %w", label, err)
	}
	metadata, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("prepared worker build: stat %s directory: %w", label, err)
	}
	if !metadata.IsDir() {
		return "", fmt.Errorf("prepared worker build: %s path is not a directory", label)
	}
	return real, nil
}

func resolveInputFile(root, value string) (string, error) {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if err := requireRegularFile(real); err != nil {
		return "", err
	}
	return real, nil
}

func resolveExistingDirectory(root, value, label string) (string, error) {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("prepared worker build: %s directory is required", label)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("prepared worker build: resolve %s directory: %w", label, err)
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("prepared worker build: resolve %s directory links: %w", label, err)
	}
	metadata, err := os.Lstat(real)
	if err != nil {
		return "", fmt.Errorf("prepared worker build: stat %s directory: %w", label, err)
	}
	if metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
		return "", fmt.Errorf("prepared worker build: %s directory is not real", label)
	}
	return real, nil
}

func ensureRealDirectory(path string) error {
	err := os.Mkdir(path, 0o755)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	metadata, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
		return fmt.Errorf("path is not a real directory")
	}
	return nil
}

func requireRegularFile(path string) error {
	metadata, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if metadata.Mode()&os.ModeSymlink != 0 || !metadata.Mode().IsRegular() {
		return fmt.Errorf("not a real regular file")
	}
	return nil
}

func within(root, child string) bool {
	relative, err := filepath.Rel(root, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func canonicalLocalPackage(value string) bool {
	if !strings.HasPrefix(value, "./") || strings.ContainsAny(value, "\\\x00\r\n\t ") ||
		strings.Contains(value, "...") {
		return false
	}
	relative := strings.TrimPrefix(value, "./")
	return relative != "" && path.Clean(relative) == relative && relative != ".." &&
		!strings.HasPrefix(relative, "../")
}

func canonicalName(value string) bool {
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validateTarget(target Target) error {
	supportedOS := target.GOOS == "darwin" || target.GOOS == "linux" || target.GOOS == "windows"
	supportedArch := target.GOARCH == "amd64" || target.GOARCH == "arm64"
	if !supportedOS || !supportedArch {
		return fmt.Errorf("prepared worker build: unsupported target %q/%q", target.GOOS, target.GOARCH)
	}
	return nil
}

func canonicalTags(values []string) ([]string, error) {
	if len(values) > maximumBuildTags {
		return nil, fmt.Errorf("prepared worker build: too many build tags")
	}
	tags := append([]string(nil), values...)
	for _, tag := range tags {
		if tag == "" || len(tag) > 128 {
			return nil, fmt.Errorf("prepared worker build: invalid build tag")
		}
		for _, character := range tag {
			if (character >= 'a' && character <= 'z') ||
				(character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') ||
				character == '_' || character == '.' {
				continue
			}
			return nil, fmt.Errorf("prepared worker build: invalid build tag %q", tag)
		}
	}
	sort.Strings(tags)
	for index := 1; index < len(tags); index++ {
		if tags[index-1] == tags[index] {
			return nil, fmt.Errorf("prepared worker build: duplicate build tag %q", tags[index])
		}
	}
	return tags, nil
}

func hashRegularFile(path string, limit int64) (workerartifact.Digest, error) {
	file, err := os.Open(path)
	if err != nil {
		return workerartifact.Digest{}, err
	}
	defer file.Close()
	metadata, err := file.Stat()
	if err != nil {
		return workerartifact.Digest{}, err
	}
	if !metadata.Mode().IsRegular() || metadata.Size() <= 0 || metadata.Size() > limit {
		return workerartifact.Digest{}, fmt.Errorf("file size or type is invalid")
	}
	hasher := sha256.New()
	if err := copyExactReader(hasher, file, metadata.Size()); err != nil {
		return workerartifact.Digest{}, err
	}
	var digest workerartifact.Digest
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func copyExactFile(writer io.Writer, path string, size int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return copyExactReader(writer, file, size)
}

func copyExactReader(writer io.Writer, reader io.Reader, size int64) error {
	written, err := io.CopyN(writer, reader, size)
	if err != nil {
		return err
	}
	if written != size {
		return fmt.Errorf("file changed while hashing")
	}
	var extra [1]byte
	read, err := reader.Read(extra[:])
	if err != io.EOF || read != 0 {
		return fmt.Errorf("file changed while hashing")
	}
	return nil
}

func writeFrameLength(writer io.Writer, value uint64) {
	_ = binary.Write(writer, binary.BigEndian, value)
}

func syncRegularFile(path string) error {
	// FlushFileBuffers requires a write-capable handle on Windows. Keep files
	// writable through this durability boundary and seal them only afterward.
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
