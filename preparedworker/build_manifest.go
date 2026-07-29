package preparedworker

import workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"

// OpenBuild verifies a builder-published manifest and its immutable worker
// executable, enforces host-target compatibility, and returns opaque launch
// authority. Descriptor, digest, and publication mechanics remain private.
func OpenBuild(manifestPath string, maxExecutableBytes int64) (Build, error) {
	published, err := workerartifact.Open(manifestPath, maxExecutableBytes)
	if err != nil {
		return Build{}, err
	}
	artifact, err := openVerifiedProcessArtifact(
		published.Executable,
		published.Descriptor,
		published.ExecutableDigest,
		maxExecutableBytes,
	)
	if err != nil {
		return Build{}, err
	}
	return buildFromArtifact(artifact), nil
}
