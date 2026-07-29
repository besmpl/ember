package preparedworker

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// workerBuildIDHex is fixed by the worker build command after it hashes the
// descriptor. It has no runtime setter; ServeWorker rejects an uninjected or
// malformed value before touching the process protocol.
var workerBuildIDHex string

// ServeWorker binds one typed application contract to the current process's
// supervised stdin/stdout worker protocol. Worker programs need not know
// build identities, descriptors, protocol frames, codecs, or parent-lifetime
// mechanics.
func ServeWorker[Q, R, C, E any](
	ctx context.Context,
	contract Contract[Q, R, C, E],
	factory HandlerFactory[Q, R, C, E],
) error {
	return serveWorker(ctx, os.Stdin, os.Stdout, workerBuildIDHex, contract, factory, true)
}

func serveWorker[Q, R, C, E any](
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	buildIDHex string,
	contract Contract[Q, R, C, E],
	factory HandlerFactory[Q, R, C, E],
	requireParentOwnership bool,
) error {
	if factory == nil {
		return fmt.Errorf("prepared worker: handler factory is required")
	}
	buildID, err := parseWorkerBuildID(buildIDHex)
	if err != nil {
		return err
	}
	wire, limits, err := contract.wire()
	if err != nil {
		return err
	}
	contractID, err := wire.Identity(limits)
	if err != nil {
		return err
	}
	preparer := newMemoryPreparer(
		contract.Request.Codec,
		contract.Result.Codec,
		contract.Checkpoint.Codec,
		contract.Effect.Codec,
		func(
			ctx context.Context,
			position Position,
			checkpoint C,
		) (Handler[Q, R, C, E], error) {
			return factory(ctx, Restore[C]{Position: position, Checkpoint: checkpoint})
		},
	)
	return serveProcessWorker(ctx, input, output, processWorkerOptions{
		BuildID: buildID, Contract: contractID,
		CheckpointSchema: wire.CheckpointSchema,
		Limits:           limits,
		Preparer:         preparer,
	}, requireParentOwnership)
}

func parseWorkerBuildID(encoded string) (contentDigest, error) {
	if len(encoded) != hex.EncodedLen(len(contentDigest{})) {
		return contentDigest{}, fmt.Errorf("prepared worker: compiled build ID is missing or invalid")
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return contentDigest{}, fmt.Errorf("prepared worker: compiled build ID is invalid")
	}
	var buildID contentDigest
	copy(buildID[:], decoded)
	if buildID == (contentDigest{}) {
		return contentDigest{}, fmt.Errorf("prepared worker: compiled build ID is zero")
	}
	return buildID, nil
}
