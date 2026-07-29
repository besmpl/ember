package preparedworkerartifact

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	manifestVersion = uint32(1)
	maximumManifest = 1 << 20
)

// Published is one fully verified target-neutral worker publication.
type Published struct {
	Manifest         string
	Executable       string
	Descriptor       Descriptor
	BuildID          Digest
	ExecutableDigest Digest
}

// Open verifies a builder-published manifest, descriptor, and executable. It
// does not decide whether the target can run on the current host.
func Open(manifestPath string, maxExecutableBytes int64) (Published, error) {
	if maxExecutableBytes <= 0 {
		return Published{}, fmt.Errorf("prepared worker build: executable byte limit must be positive")
	}
	resolvedManifest, err := filepath.Abs(manifestPath)
	if err != nil {
		return Published{}, fmt.Errorf("prepared worker build: resolve manifest: %w", err)
	}
	data, err := readManifest(resolvedManifest)
	if err != nil {
		return Published{}, err
	}
	manifest, err := decodeManifest(data)
	if err != nil {
		return Published{}, err
	}
	descriptor, err := manifest.Descriptor.descriptor()
	if err != nil {
		return Published{}, err
	}
	buildID, err := descriptor.BuildID()
	if err != nil {
		return Published{}, err
	}
	wantBuildID, err := parseDigest("build ID", manifest.BuildID)
	if err != nil {
		return Published{}, err
	}
	if buildID != wantBuildID {
		return Published{}, fmt.Errorf("prepared worker build: descriptor build ID differs from manifest")
	}
	wantExecutableDigest, err := parseDigest("executable digest", manifest.ExecutableDigest)
	if err != nil {
		return Published{}, err
	}
	executable, err := resolveExecutable(resolvedManifest, manifest.Executable)
	if err != nil {
		return Published{}, err
	}
	executableDigest, err := HashExecutable(executable, maxExecutableBytes)
	if err != nil {
		return Published{}, err
	}
	if executableDigest != wantExecutableDigest {
		return Published{}, fmt.Errorf("prepared worker build: executable digest differs from manifest")
	}
	return Published{
		Manifest: resolvedManifest, Executable: executable,
		Descriptor: descriptor, BuildID: buildID, ExecutableDigest: executableDigest,
	}, nil
}

// Verify checks one exact requested publication without host-target policy.
func Verify(
	manifestPath string,
	executable string,
	descriptor Descriptor,
	maxExecutableBytes int64,
) error {
	published, err := Open(manifestPath, maxExecutableBytes)
	if err != nil {
		return err
	}
	wantBuildID, err := descriptor.BuildID()
	if err != nil {
		return err
	}
	if published.BuildID != wantBuildID {
		return fmt.Errorf("prepared worker build: published descriptor differs from requested build")
	}
	wantExecutable, err := filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("prepared worker build: resolve expected executable: %w", err)
	}
	if published.Executable != wantExecutable {
		return fmt.Errorf("prepared worker build: published executable path differs from requested build")
	}
	return nil
}

// Publish atomically writes a manifest selecting one already-built immutable
// executable. It publishes no executable and performs no activation.
func Publish(
	manifestPath string,
	executable string,
	descriptor Descriptor,
	maxExecutableBytes int64,
) error {
	resolvedManifest, err := filepath.Abs(manifestPath)
	if err != nil {
		return fmt.Errorf("prepared worker build: resolve output manifest: %w", err)
	}
	resolvedExecutable, err := filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("prepared worker build: resolve output executable: %w", err)
	}
	relative, err := filepath.Rel(filepath.Dir(resolvedManifest), resolvedExecutable)
	if err != nil {
		return fmt.Errorf("prepared worker build: relate output executable: %w", err)
	}
	relative = filepath.ToSlash(relative)
	if _, err := resolveExecutable(resolvedManifest, relative); err != nil {
		return err
	}
	manifest, err := newManifest(resolvedExecutable, descriptor, maxExecutableBytes)
	if err != nil {
		return err
	}
	manifest.Executable = relative
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("prepared worker build: encode manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maximumManifest {
		return fmt.Errorf("prepared worker build: encoded manifest exceeds %d bytes", maximumManifest)
	}
	return replaceManifest(resolvedManifest, encoded)
}

type manifest struct {
	FormatVersion    uint32             `json:"format_version"`
	Executable       string             `json:"executable"`
	ExecutableDigest string             `json:"executable_sha256"`
	BuildID          string             `json:"build_id"`
	Descriptor       manifestDescriptor `json:"descriptor"`
}

type manifestDescriptor struct {
	FormatVersion       uint32   `json:"format_version"`
	ProtocolVersion     uint32   `json:"protocol_version"`
	Contract            string   `json:"contract_sha256"`
	ProgramRecipeDigest string   `json:"program_recipe_sha256"`
	PreparedABIVersion  uint32   `json:"prepared_abi_version"`
	SemanticVersion     uint32   `json:"semantic_version"`
	ProgramHash         string   `json:"program_sha256"`
	ProtoCounts         []uint32 `json:"proto_counts"`
	GeneratedDigest     string   `json:"generated_sha256"`
	WrapperDigest       string   `json:"wrapper_sha256"`
	EmberVersion        string   `json:"ember_version"`
	Toolchain           string   `json:"toolchain"`
	TargetOS            string   `json:"target_os"`
	TargetArch          string   `json:"target_arch"`
	BuildTags           []string `json:"build_tags"`
	BuildFlags          []string `json:"build_flags"`
}

func (wire manifestDescriptor) descriptor() (Descriptor, error) {
	contract, err := parseDigest("contract digest", wire.Contract)
	if err != nil {
		return Descriptor{}, err
	}
	programRecipe, err := parseDigest("program recipe digest", wire.ProgramRecipeDigest)
	if err != nil {
		return Descriptor{}, err
	}
	program, err := parseDigest("program digest", wire.ProgramHash)
	if err != nil {
		return Descriptor{}, err
	}
	generated, err := parseDigest("generated source digest", wire.GeneratedDigest)
	if err != nil {
		return Descriptor{}, err
	}
	wrapper, err := parseDigest("wrapper source digest", wire.WrapperDigest)
	if err != nil {
		return Descriptor{}, err
	}
	descriptor := Descriptor{
		FormatVersion: wire.FormatVersion, ProtocolVersion: wire.ProtocolVersion,
		Contract: contract, ProgramRecipeDigest: programRecipe,
		PreparedABIVersion: wire.PreparedABIVersion, SemanticVersion: wire.SemanticVersion,
		ProgramHash: program, ProtoCounts: append([]uint32(nil), wire.ProtoCounts...),
		GeneratedDigest: generated, WrapperDigest: wrapper,
		EmberVersion: wire.EmberVersion, Toolchain: wire.Toolchain,
		TargetOS: wire.TargetOS, TargetArch: wire.TargetArch,
		BuildTags:  append([]string(nil), wire.BuildTags...),
		BuildFlags: append([]string(nil), wire.BuildFlags...),
	}
	if _, err := descriptor.BuildID(); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

func newManifest(executable string, descriptor Descriptor, maxExecutableBytes int64) (manifest, error) {
	buildID, err := descriptor.BuildID()
	if err != nil {
		return manifest{}, err
	}
	executableDigest, err := HashExecutable(executable, maxExecutableBytes)
	if err != nil {
		return manifest{}, err
	}
	return manifest{
		FormatVersion: manifestVersion, ExecutableDigest: executableDigest.String(), BuildID: buildID.String(),
		Descriptor: manifestDescriptor{
			FormatVersion: descriptor.FormatVersion, ProtocolVersion: descriptor.ProtocolVersion,
			Contract: descriptor.Contract.String(), ProgramRecipeDigest: descriptor.ProgramRecipeDigest.String(),
			PreparedABIVersion: descriptor.PreparedABIVersion, SemanticVersion: descriptor.SemanticVersion,
			ProgramHash: descriptor.ProgramHash.String(), ProtoCounts: append([]uint32(nil), descriptor.ProtoCounts...),
			GeneratedDigest: descriptor.GeneratedDigest.String(), WrapperDigest: descriptor.WrapperDigest.String(),
			EmberVersion: descriptor.EmberVersion, Toolchain: descriptor.Toolchain,
			TargetOS: descriptor.TargetOS, TargetArch: descriptor.TargetArch,
			BuildTags:  append([]string(nil), descriptor.BuildTags...),
			BuildFlags: append([]string(nil), descriptor.BuildFlags...),
		},
	}, nil
}

func decodeManifest(data []byte) (manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded manifest
	if err := decoder.Decode(&decoded); err != nil {
		return manifest{}, fmt.Errorf("prepared worker build: decode manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return manifest{}, fmt.Errorf("prepared worker build: decode manifest: %w", err)
	}
	if decoded.FormatVersion != manifestVersion {
		return manifest{}, fmt.Errorf("prepared worker build: manifest version %d, want %d", decoded.FormatVersion, manifestVersion)
	}
	if decoded.Executable == "" {
		return manifest{}, fmt.Errorf("prepared worker build: executable path is required")
	}
	return decoded, nil
}

func readManifest(manifestPath string) ([]byte, error) {
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("prepared worker build: open manifest: %w", err)
	}
	defer file.Close()
	metadata, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("prepared worker build: stat manifest: %w", err)
	}
	if !metadata.Mode().IsRegular() || metadata.Size() <= 0 || metadata.Size() > maximumManifest {
		return nil, fmt.Errorf("prepared worker build: manifest size or type is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumManifest+1))
	if err != nil {
		return nil, fmt.Errorf("prepared worker build: read manifest: %w", err)
	}
	if len(data) > maximumManifest {
		return nil, fmt.Errorf("prepared worker build: manifest exceeds %d bytes", maximumManifest)
	}
	return data, nil
}

func resolveExecutable(manifestPath, name string) (string, error) {
	if filepath.IsAbs(name) || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return "", fmt.Errorf("prepared worker build: executable path must be canonical and relative")
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned != name || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("prepared worker build: executable path must stay beside the manifest")
	}
	resolved, err := filepath.Abs(filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(cleaned)))
	if err != nil {
		return "", fmt.Errorf("prepared worker build: resolve executable: %w", err)
	}
	return resolved, nil
}

func parseDigest(label, encoded string) (Digest, error) {
	if len(encoded) != hex.EncodedLen(len(Digest{})) {
		return Digest{}, fmt.Errorf("prepared worker build: %s is not a SHA-256 digest", label)
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return Digest{}, fmt.Errorf("prepared worker build: %s is not a SHA-256 digest", label)
	}
	if hex.EncodeToString(decoded) != encoded {
		return Digest{}, fmt.Errorf("prepared worker build: %s is not canonical lowercase hex", label)
	}
	var digest Digest
	copy(digest[:], decoded)
	if digest == (Digest{}) {
		return Digest{}, fmt.Errorf("prepared worker build: %s is zero", label)
	}
	return digest, nil
}

func replaceManifest(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".ember-worker-manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("prepared worker build: create manifest temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("prepared worker build: set manifest permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("prepared worker build: write manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("prepared worker build: sync manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("prepared worker build: close manifest: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("prepared worker build: publish manifest: %w", err)
	}
	if err := syncParent(path); err != nil {
		return fmt.Errorf("prepared worker build: sync manifest directory: %w", err)
	}
	return nil
}
