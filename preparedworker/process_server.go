package preparedworker

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	processReadyPayloadBytes      = 5*sha256DigestBytes + 2*8 + sha256DigestBytes + 7*8
	processMaxFailurePayloadBytes = 3*4 + 64 + 256 + processMaxFailureBytes
)

// processWorkerOptions fixes the identity, bounds, and encoded backend factory
// compiled into one static worker executable.
type processWorkerOptions struct {
	BuildID          contentDigest
	Contract         contentIdentity
	CheckpointSchema contentIdentity
	Limits           transactionLimits
	Preparer         backendPreparer
}

func serveProcessWorker(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	options processWorkerOptions,
	requireParentOwnership bool,
) error {
	ctx = normalizeContext(ctx)
	if err := validateProcessWorkerOptions(options); err != nil {
		return err
	}
	if input == nil || output == nil {
		return fmt.Errorf("prepared worker server: nil transport")
	}
	stopParentWatchdog, err := startWorkerParentWatchdog(requireParentOwnership)
	if err != nil {
		return err
	}
	defer stopParentWatchdog()
	helloLimit, requestLimit, responseLimit, err := processPayloadLimits(options.Limits)
	if err != nil {
		return err
	}
	frame, err := decodeProcessFrame(input, helloLimit)
	if err != nil {
		return fmt.Errorf("prepared worker server: read HELLO: %w", err)
	}
	if frame.Kind != processMessageHello || frame.Correlation != 0 {
		return processServerFailure(
			output,
			frame.Correlation,
			FailureProtocol,
			"hello",
			fmt.Errorf("first frame is not uncorrelated HELLO"),
		)
	}
	hello, err := decodeProcessHello(frame.Payload, options.Limits.MaxCheckpointBytes)
	if err != nil {
		return processServerFailure(output, 0, FailureProtocol, "hello", err)
	}
	if hello.BuildID != options.BuildID || hello.Contract != options.Contract ||
		hello.CheckpointSchema != options.CheckpointSchema || hello.Limits != options.Limits {
		return processServerFailure(
			output,
			0,
			FailureIdentity,
			"hello",
			fmt.Errorf("parent identity differs from compiled worker identity"),
		)
	}
	backendSpec := backendSpec{
		Artifact:         hello.Artifact,
		Contract:         hello.Contract,
		Stream:           hello.Stream,
		CheckpointSchema: hello.CheckpointSchema,
		Position:         hello.Position,
		CheckpointDigest: hello.CheckpointDigest,
		Checkpoint:       append([]byte(nil), hello.Checkpoint...),
		Limits:           hello.Limits,
		artifact:         artifactFromIdentity(hello.Artifact),
	}
	backend, ready, err := options.Preparer.Prepare(ctx, backendSpec)
	if err != nil {
		return processServerFailure(output, 0, classifyProcessBackendFailure(err), "prepare", err)
	}
	if backend == nil {
		return processServerFailure(output, 0, FailureProtocol, "prepare", fmt.Errorf("preparer returned nil backend"))
	}
	backendOwned := true
	defer func() {
		if backendOwned {
			_ = backend.Close(context.Background())
		}
	}()
	if ready != readyFor(backendSpec) {
		return processServerFailure(output, 0, FailureIdentity, "prepare", fmt.Errorf("READY identity differs"))
	}
	readyPayload, err := encodeProcessReady(processReady{
		Artifact: ready.Artifact, BuildID: options.BuildID, Contract: ready.Contract,
		Stream: ready.Stream, CheckpointSchema: ready.CheckpointSchema,
		Position: ready.Position, CheckpointDigest: ready.CheckpointDigest, Limits: ready.Limits,
	})
	if err != nil {
		return fmt.Errorf("prepared worker server: encode READY: %w", err)
	}
	if err := writeProcessFrame(output, processFrame{Kind: processMessageReady, Payload: readyPayload}, responseLimit); err != nil {
		return fmt.Errorf("prepared worker server: write READY: %w", err)
	}

	var lastCorrelation uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := decodeProcessFrame(input, requestLimit)
		if err != nil {
			return fmt.Errorf("prepared worker server: read request: %w", err)
		}
		if frame.Correlation == 0 || frame.Correlation <= lastCorrelation {
			return processServerFailure(
				output,
				frame.Correlation,
				FailureProtocol,
				"request",
				fmt.Errorf("correlation %d is not strictly increasing", frame.Correlation),
			)
		}
		lastCorrelation = frame.Correlation
		switch frame.Kind {
		case processMessageApply:
			operation, err := decodeProcessApply(frame.Payload, options.Limits.MaxRequestBytes)
			if err != nil {
				return processServerFailure(output, frame.Correlation, FailureProtocol, "apply", err)
			}
			decision, err := backend.Apply(ctx, operation)
			if err != nil {
				return processServerFailure(
					output,
					frame.Correlation,
					classifyProcessBackendFailure(err),
					"apply",
					err,
				)
			}
			if err := writeProcessDecision(output, frame.Correlation, decision, options.Limits, responseLimit); err != nil {
				return err
			}
		case processMessageReplay:
			replay, err := decodeProcessReplay(frame.Payload, options.Limits.MaxRequestBytes)
			if err != nil {
				return processServerFailure(output, frame.Correlation, FailureProtocol, "replay", err)
			}
			decision, err := backend.Apply(ctx, replay.Operation)
			if err != nil {
				return processServerFailure(
					output,
					frame.Correlation,
					classifyProcessBackendFailure(err),
					"replay",
					err,
				)
			}
			record, err := encodedReplayRecord(backendSpec, replay.Operation, decision)
			if err != nil {
				return processServerFailure(output, frame.Correlation, FailureProtocol, "replay", err)
			}
			if replayRecordDigest(record) != replay.ExpectedRecord {
				return processServerFailure(
					output,
					frame.Correlation,
					FailureProtocol,
					"replay",
					fmt.Errorf("candidate decision differs from committed record"),
				)
			}
			if err := writeProcessDecision(output, frame.Correlation, decision, options.Limits, responseLimit); err != nil {
				return err
			}
		case processMessageClose:
			if err := decodeProcessClose(frame.Payload); err != nil {
				return processServerFailure(output, frame.Correlation, FailureProtocol, "close", err)
			}
			if err := backend.Close(ctx); err != nil {
				return processServerFailure(output, frame.Correlation, FailureLost, "close", err)
			}
			backendOwned = false
			if err := writeProcessFrame(output, processFrame{
				Kind: processMessageClosed, Correlation: frame.Correlation,
			}, responseLimit); err != nil {
				return fmt.Errorf("prepared worker server: write CLOSED: %w", err)
			}
			return nil
		default:
			return processServerFailure(
				output,
				frame.Correlation,
				FailureProtocol,
				"request",
				fmt.Errorf("unexpected message kind %d", frame.Kind),
			)
		}
	}
}

func validateProcessWorkerOptions(options processWorkerOptions) error {
	if options.BuildID == (contentDigest{}) || !options.Contract.valid() ||
		!options.CheckpointSchema.valid() || options.Preparer == nil {
		return fmt.Errorf("prepared worker server: incomplete worker options")
	}
	_, _, _, err := processPayloadLimits(options.Limits)
	return err
}

func processPayloadLimits(limits transactionLimits) (hello, request, response int, err error) {
	if err := limits.validate(); err != nil {
		return 0, 0, 0, err
	}
	values := []int64{
		int64(5*sha256DigestBytes+2*8+sha256DigestBytes+4+7*8) + int64(limits.MaxCheckpointBytes),
		int64(4 + 16 + 4 + limits.MaxRequestBytes + sha256DigestBytes),
		int64(processDecisionPayloadLimit(limits)),
		processMaxFailurePayloadBytes,
		processReadyPayloadBytes,
	}
	for _, value := range values {
		if value > processMaxFrameBytes {
			return 0, 0, 0, fmt.Errorf("prepared worker protocol: configured payload uses %d bytes, limit %d", value, processMaxFrameBytes)
		}
	}
	hello = int(values[0])
	request = max(int(values[1]), 0)
	response = max(int(values[2]), processMaxFailurePayloadBytes, processReadyPayloadBytes)
	return hello, request, response, nil
}

func processDecisionPayloadLimit(limits transactionLimits) int {
	total := int64(16 + 4 + 1 + 4 + 1 + 4)
	total += int64(limits.MaxResultBytes)
	total += int64(limits.MaxCheckpointBytes)
	total += int64(limits.MaxOutboxBytes)
	total += int64(limits.MaxOutboxItems) * 4
	if total > int64(processMaxFrameBytes) {
		return processMaxFrameBytes + 1
	}
	return int(total)
}

func writeProcessDecision(
	output io.Writer,
	correlation uint64,
	decision wireDecision,
	limits transactionLimits,
	maxPayload int,
) error {
	payload, err := encodeProcessDecision(decision, limits)
	if err != nil {
		return processServerFailure(output, correlation, FailureProtocol, "decision", err)
	}
	if err := writeProcessFrame(output, processFrame{
		Kind: processMessageDecision, Correlation: correlation, Payload: payload,
	}, maxPayload); err != nil {
		return fmt.Errorf("prepared worker server: write DECISION: %w", err)
	}
	return nil
}

func processServerFailure(
	output io.Writer,
	correlation uint64,
	kind FailureKind,
	operation string,
	cause error,
) error {
	if cause == nil {
		cause = errors.New("worker failure")
	}
	message := cause.Error()
	if len(message) > processMaxFailureBytes {
		message = message[:processMaxFailureBytes]
	}
	payload, encodeErr := encodeProcessFailure(processFailure{
		Kind: kind, Operation: operation, Message: message,
	}, processMaxFailureBytes)
	if encodeErr != nil {
		return errors.Join(cause, encodeErr)
	}
	writeErr := writeProcessFrame(output, processFrame{
		Kind: processMessageFailure, Correlation: correlation, Payload: payload,
	}, processMaxFailurePayloadBytes)
	return errors.Join(cause, writeErr)
}

func classifyProcessBackendFailure(err error) FailureKind {
	var failure *Failure
	if errors.As(err, &failure) && validFailureKind(failure.Kind) {
		return failure.Kind
	}
	return FailureGuest
}

func validFailureKind(kind FailureKind) bool {
	switch kind {
	case FailureGuest, FailureIdentity, FailureProtocol, FailureCorrupt, FailureStale, FailureBehind,
		FailureLost, FailureConflict, FailureGap, FailureUnknownCommit, FailureDelivery,
		FailureBusy, FailureClosed, FailureLimit, FailureNotQuiescent:
		return true
	default:
		return false
	}
}

func encodedReplayRecord(
	spec backendSpec,
	operation encodedOperation,
	decision wireDecision,
) (transactionRecord, error) {
	if err := validateProcessDecision(decision, spec.Limits); err != nil {
		return transactionRecord{}, err
	}
	if decision.Position != (Position{
		Sequence: operation.Sequence,
		Revision: operation.BaseRevision + 1,
	}) {
		return transactionRecord{}, fmt.Errorf("replayed decision position differs")
	}
	record := transactionRecord{
		Stream:   spec.Stream,
		Sequence: operation.Sequence, BaseRevision: operation.BaseRevision,
		Revision:      decision.Position.Revision,
		RequestDigest: hashContent(operation.Request), Request: append([]byte(nil), operation.Request...),
		ResultDigest: hashContent(decision.Result), Result: append([]byte(nil), decision.Result...),
		Quiescent: decision.Quiescent,
	}
	if decision.HasCheckpoint {
		record.Checkpoint = &checkpointRecord{
			Schema: spec.CheckpointSchema, Position: decision.Position,
			Payload: append([]byte(nil), decision.Checkpoint...), Digest: hashContent(decision.Checkpoint),
		}
	}
	for index, payload := range decision.Effects {
		record.Outbox = append(record.Outbox, outboxRecord{
			ID:      outboxID{Stream: spec.Stream, Sequence: operation.Sequence, Ordinal: uint32(index + 1)},
			Payload: append([]byte(nil), payload...), Digest: hashContent(payload),
		})
	}
	record.Digest = recordDigest(record)
	return record, nil
}

func replayRecordDigest(record transactionRecord) contentDigest {
	return recordDigest(record)
}
