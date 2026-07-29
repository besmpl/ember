package preparedworker

import "context"

// This file exposes private test seams to the external behavior fixtures.
// None of these names are part of the built package's public interface.

type TestIdentity = contentIdentity
type TestWireContract = wireContract
type TestTransactionLimits = transactionLimits
type TestInitialState[C any] = initialState[C]
type TestOutboxRecord = outboxRecord
type TestEffectPublisher = effectPublisher
type TestTransactionJournal = transactionJournal
type TestTransactionModule[Q, R, C, E any] = transactionModule[Q, R, C, E]
type TestModuleConfig[Q, R, C, E any] = moduleConfig[Q, R, C, E]

func ExportIdentityFor(label string) contentIdentity {
	return identityFor(label)
}

func ExportArtifactFromIdentity(identity contentIdentity) preparedArtifact {
	return artifactFromIdentity(identity)
}

func NewTestMemoryJournal() *memoryJournal {
	return newMemoryJournal()
}

func NewTestMemoryPublisher() *memoryPublisher {
	return newMemoryPublisher()
}

func NewTestMemoryPreparer[Q, R, C, E any](
	requestCodec Codec[Q],
	resultCodec Codec[R],
	checkpointCodec Codec[C],
	effectCodec Codec[E],
	factory func(context.Context, Position, C) (Handler[Q, R, C, E], error),
) backendPreparer {
	return newMemoryPreparer(requestCodec, resultCodec, checkpointCodec, effectCodec, factory)
}

func NewTestTransactionModule[Q, R, C, E any](
	config moduleConfig[Q, R, C, E],
	initial initialState[C],
) (*transactionModule[Q, R, C, E], error) {
	return newTransactionModule(config, initial)
}

func OpenTestFileJournal(path string, maxBytes int64) (*fileJournal, error) {
	return openDurableJournal(path, maxBytes)
}
