package preparedworker

import (
	"fmt"
	"path/filepath"
	"runtime"

	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
)

type processArtifact struct {
	executable         string
	descriptor         workerartifact.Descriptor
	buildID            contentDigest
	binaryDigest       contentDigest
	maxExecutableBytes int64
}

// openProcessArtifact verifies a runnable static executable against one exact
// private descriptor. The executable is rehashed immediately before launch.
func openProcessArtifact(
	executable string,
	descriptor workerartifact.Descriptor,
	maxExecutableBytes int64,
) (preparedArtifact, error) {
	if executable == "" {
		return preparedArtifact{}, fmt.Errorf("prepared worker artifact: empty executable path")
	}
	if maxExecutableBytes <= 0 {
		return preparedArtifact{}, fmt.Errorf("prepared worker artifact: executable byte limit must be positive")
	}
	resolved, err := filepath.Abs(executable)
	if err != nil {
		return preparedArtifact{}, fmt.Errorf("prepared worker artifact: resolve executable: %w", err)
	}
	binaryDigest, err := workerartifact.HashExecutable(resolved, maxExecutableBytes)
	if err != nil {
		return preparedArtifact{}, err
	}
	return openVerifiedProcessArtifact(resolved, descriptor, binaryDigest, maxExecutableBytes)
}

func openVerifiedProcessArtifact(
	executable string,
	descriptor workerartifact.Descriptor,
	binaryDigest contentDigest,
	maxExecutableBytes int64,
) (preparedArtifact, error) {
	buildID, err := descriptor.BuildID()
	if err != nil {
		return preparedArtifact{}, err
	}
	if descriptor.TargetOS != runtime.GOOS || descriptor.TargetArch != runtime.GOARCH {
		return preparedArtifact{}, fmt.Errorf(
			"prepared worker artifact: target %s/%s cannot run on %s/%s",
			descriptor.TargetOS,
			descriptor.TargetArch,
			runtime.GOOS,
			runtime.GOARCH,
		)
	}
	if binaryDigest == (contentDigest{}) {
		return preparedArtifact{}, fmt.Errorf("prepared worker artifact: executable digest is zero")
	}
	identityBytes := make([]byte, 0, len("ember-worker-artifact-v1")+len(buildID)+len(binaryDigest))
	identityBytes = append(identityBytes, "ember-worker-artifact-v1"...)
	identityBytes = append(identityBytes, buildID[:]...)
	identityBytes = append(identityBytes, binaryDigest[:]...)
	identity, err := identityFromDigest(hashContent(identityBytes))
	if err != nil {
		return preparedArtifact{}, err
	}
	return preparedArtifact{
		identity: identity,
		process: &processArtifact{
			executable: executable, descriptor: descriptor.Clone(),
			buildID: buildID, binaryDigest: binaryDigest,
			maxExecutableBytes: maxExecutableBytes,
		},
	}, nil
}

func (artifact *processArtifact) verifyExecutable() error {
	if artifact == nil {
		return fmt.Errorf("prepared worker artifact: missing process metadata")
	}
	digest, err := workerartifact.HashExecutable(artifact.executable, artifact.maxExecutableBytes)
	if err != nil {
		return err
	}
	if digest != artifact.binaryDigest {
		return fmt.Errorf("prepared worker artifact: executable digest changed before launch")
	}
	return nil
}
