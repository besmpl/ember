// Package preparedworkerjobfixture is a closed binary job-processing
// application used to prove that the prepared-worker seam is independent of
// the rich-game JSON contract.
package preparedworkerjobfixture

import "github.com/besmpl/ember/preparedworker"

const codecVersion = 1

// Job is one deterministic static-AOT computation in a submitted batch.
type Job struct {
	ID   uint32
	Seed uint16
	Work uint16
}

// Request submits one ordered batch to a logical queue.
type Request struct {
	Queue uint16
	Jobs  []Job
}

// Result is the complete observable queue position after a batch.
type Result struct {
	Queue     uint16
	Accepted  uint16
	Completed uint32
	Checksum  int64
	Cursor    uint64
}

// Checkpoint is the detached state needed to restore another worker process.
type Checkpoint struct {
	Format    uint8
	Sequence  uint64
	Revision  uint64
	Queue     uint16
	Completed uint32
	Checksum  int64
	Cursor    uint64
}

// Effect is one durable completion notification.
type Effect struct {
	JobID    uint32
	Ticket   uint64
	Checksum int64
}

// RunnerContract is the complete caller-facing application contract shared by
// the embedded and process deployment adapters.
func RunnerContract() preparedworker.Contract[Request, Result, Checkpoint, Effect] {
	return preparedworker.Contract[Request, Result, Checkpoint, Effect]{
		CodecVersion: codecVersion,
		Request: preparedworker.Schema[Request]{
			Name: "prepared-job-request-v1", MaxBytes: 16 << 10, Codec: requestCodec{},
		},
		Result: preparedworker.Schema[Result]{
			Name: "prepared-job-result-v1", MaxBytes: 256, Codec: resultCodec{},
		},
		Checkpoint: preparedworker.Schema[Checkpoint]{
			Name: "prepared-job-checkpoint-v1", MaxBytes: 256, Codec: checkpointCodec{},
		},
		Effect: preparedworker.Schema[Effect]{
			Name: "prepared-job-effect-v1", MaxBytes: 16 << 10, Codec: effectCodec{},
		},
		MaxEffects: 256,
	}
}
