package preparedworker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
)

func TestOpenBuildLoadsPublishedManifestAndRejectsChangedExecutable(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "worker")
	if err := os.WriteFile(executable, []byte("static worker a"), 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := artifactTestDescriptor()
	manifest := filepath.Join(directory, "worker.ember-build.json")
	if err := workerartifact.Publish(manifest, executable, descriptor, 1024); err != nil {
		t.Fatal(err)
	}

	build, err := OpenBuild(manifest, 1024)
	if err != nil {
		t.Fatal(err)
	}
	wantArtifact, err := openProcessArtifact(executable, descriptor, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !build.valid() || build.artifact.Identity() != wantArtifact.Identity() {
		t.Fatalf("opened build identity = %v, want %v", build.artifact.Identity(), wantArtifact.Identity())
	}

	if err := os.WriteFile(executable, []byte("static worker b"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBuild(manifest, 1024); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("OpenBuild after executable replacement error = %v, want digest failure", err)
	}
}

func TestOpenBuildRejectsNoncanonicalDigestText(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "worker")
	if err := os.WriteFile(executable, []byte("static worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "worker.ember-build.json")
	if err := workerartifact.Publish(manifestPath, executable, artifactTestDescriptor(), 1024); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	buildID, ok := manifest["build_id"].(string)
	if !ok {
		t.Fatal("manifest build_id is not a string")
	}
	manifest["build_id"] = strings.ToUpper(buildID)
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenBuild(manifestPath, 1024); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("OpenBuild uppercase digest error = %v, want canonical digest failure", err)
	}
}

func TestRunnerContractIdentityBindsEffectByteBound(t *testing.T) {
	contract := Contract[string, string, string, string]{
		CodecVersion: 1,
		Request:      Schema[string]{Name: "request-v1", MaxBytes: 64, Codec: manifestStringCodec{}},
		Result:       Schema[string]{Name: "result-v1", MaxBytes: 64, Codec: manifestStringCodec{}},
		Checkpoint:   Schema[string]{Name: "checkpoint-v1", MaxBytes: 64, Codec: manifestStringCodec{}},
		Effect:       Schema[string]{Name: "effect-v1", MaxBytes: 64, Codec: manifestStringCodec{}},
		MaxEffects:   4,
	}
	wire, limits, err := contract.wire()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := wire.Identity(limits)
	if err != nil {
		t.Fatal(err)
	}
	contract.Effect.MaxBytes++
	changedWire, changedLimits, err := contract.wire()
	if err != nil {
		t.Fatal(err)
	}
	changedIdentity, err := changedWire.Identity(changedLimits)
	if err != nil {
		t.Fatal(err)
	}
	if changedIdentity == identity {
		t.Fatal("changed effect byte bound retained runner contract identity")
	}
}

type manifestStringCodec struct{}

func (manifestStringCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }
func (manifestStringCodec) Decode(value []byte) (string, error) { return string(value), nil }

func TestBuildDescriptorAndExecutableBothBindArtifactIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker")
	if err := os.WriteFile(path, []byte("static worker a"), 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := artifactTestDescriptor()
	first, err := openProcessArtifact(path, descriptor, 1024)
	if err != nil {
		t.Fatal(err)
	}
	again, err := openProcessArtifact(path, descriptor, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity() != again.Identity() || !first.valid() {
		t.Fatalf("identical artifact identities = %v/%v", first.Identity(), again.Identity())
	}

	changedDescriptor := descriptor
	changedDescriptor.Contract = identityFor("artifact-contract-v2").Digest()
	changedBuild, err := openProcessArtifact(path, changedDescriptor, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if changedBuild.Identity() == first.Identity() {
		t.Fatal("changed descriptor retained artifact identity")
	}
	if err := os.WriteFile(path, []byte("static worker b"), 0o700); err != nil {
		t.Fatal(err)
	}
	changedBinary, err := openProcessArtifact(path, descriptor, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if changedBinary.Identity() == first.Identity() {
		t.Fatal("changed executable retained artifact identity")
	}
}

func TestOpenProcessArtifactRejectsWrongTargetAndExecutableLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker")
	if err := os.WriteFile(path, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := artifactTestDescriptor()
	descriptor.TargetOS = "not-" + runtime.GOOS
	if _, err := openProcessArtifact(path, descriptor, 1024); err == nil {
		t.Fatal("opened artifact for another target")
	}
	descriptor = artifactTestDescriptor()
	if _, err := openProcessArtifact(path, descriptor, 1); err == nil {
		t.Fatal("opened artifact beyond executable limit")
	}
}

func TestBuildDescriptorRejectsAmbiguousCollections(t *testing.T) {
	descriptor := artifactTestDescriptor()
	descriptor.BuildTags = []string{"z", "a"}
	if _, err := descriptor.BuildID(); err == nil {
		t.Fatal("accepted noncanonical build tags")
	}
	descriptor = artifactTestDescriptor()
	descriptor.ProtoCounts = []uint32{1, 0}
	if _, err := descriptor.BuildID(); err == nil {
		t.Fatal("accepted empty Proto inventory module")
	}
}

func TestContractIdentityBindsEverySchemaAndLimit(t *testing.T) {
	contract := wireContract{
		RequestSchema:    identityFor("contract-request-v1"),
		ResultSchema:     identityFor("contract-result-v1"),
		CheckpointSchema: identityFor("contract-checkpoint-v1"),
		EffectSchema:     identityFor("contract-effect-v1"),
		CodecVersion:     1,
	}
	limits := transactionLimits{
		MaxRequestBytes: 1024, MaxResultBytes: 2048, MaxCheckpointBytes: 4096,
		MaxOutboxItems: 8, MaxOutboxBytes: 8192, MaxCandidates: 2, MaxRetired: 2,
	}
	identity, err := contract.Identity(limits)
	if err != nil {
		t.Fatal(err)
	}
	changed := contract
	changed.EffectSchema = identityFor("contract-effect-v2")
	changedIdentity, err := changed.Identity(limits)
	if err != nil {
		t.Fatal(err)
	}
	if changedIdentity == identity {
		t.Fatal("changed schema retained contract identity")
	}
	limits.MaxResultBytes++
	changedIdentity, err = contract.Identity(limits)
	if err != nil {
		t.Fatal(err)
	}
	if changedIdentity == identity {
		t.Fatal("changed limit retained contract identity")
	}
}

func artifactTestDescriptor() workerartifact.Descriptor {
	return workerartifact.Descriptor{
		FormatVersion:       workerartifact.DescriptorVersion,
		ProtocolVersion:     workerartifact.ProtocolVersion,
		Contract:            identityFor("artifact-contract-v1").Digest(),
		ProgramRecipeDigest: hashContent([]byte("program recipe")),
		PreparedABIVersion:  1,
		SemanticVersion:     1,
		ProgramHash:         hashContent([]byte("program")),
		ProtoCounts:         []uint32{1, 2},
		GeneratedDigest:     hashContent([]byte("generated Go")),
		WrapperDigest:       hashContent([]byte("worker wrapper")),
		EmberVersion:        "test",
		Toolchain:           runtime.Version(),
		TargetOS:            runtime.GOOS,
		TargetArch:          runtime.GOARCH,
		BuildTags:           []string{"production", "worker"},
		BuildFlags:          []string{"-trimpath"},
	}
}
