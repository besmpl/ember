// Package preparedworkerparity defines the closed typed transaction used by
// Ember's final static-AOT deployment admission observer.
package preparedworkerparity

import (
	"encoding/binary"
	"fmt"

	"github.com/besmpl/ember/preparedworker"
)

const (
	CaseCount     = 37
	MaxIterations = 64 << 20
	codecVersion  = 1
)

type Request struct {
	Case       uint16
	Iterations uint32
	Seed       int64
}

type Result struct {
	Checksum int64
}

type Checkpoint struct{}

type Effect struct{}

func Contract() preparedworker.Contract[Request, Result, Checkpoint, Effect] {
	return preparedworker.Contract[Request, Result, Checkpoint, Effect]{
		CodecVersion: codecVersion,
		Request: preparedworker.Schema[Request]{
			Name: "epw2-all37-request-v1", MaxBytes: 15, Codec: requestCodec{},
		},
		Result: preparedworker.Schema[Result]{
			Name: "epw2-all37-result-v1", MaxBytes: 9, Codec: resultCodec{},
		},
		Checkpoint: preparedworker.Schema[Checkpoint]{
			Name: "epw2-all37-checkpoint-v1", MaxBytes: 1, Codec: checkpointCodec{},
		},
		Effect: preparedworker.Schema[Effect]{
			Name: "epw2-all37-effect-v1", MaxBytes: 1, Codec: effectCodec{},
		},
		MaxEffects: 1,
	}
}

type requestCodec struct{}

func (requestCodec) Encode(value Request) ([]byte, error) {
	encoded := make([]byte, 15)
	encoded[0] = codecVersion
	binary.BigEndian.PutUint16(encoded[1:3], value.Case)
	binary.BigEndian.PutUint32(encoded[3:7], value.Iterations)
	binary.BigEndian.PutUint64(encoded[7:15], uint64(value.Seed))
	return encoded, nil
}

func (requestCodec) Decode(encoded []byte) (Request, error) {
	if err := validateEncoding(encoded, 15, "request"); err != nil {
		return Request{}, err
	}
	return Request{
		Case:       binary.BigEndian.Uint16(encoded[1:3]),
		Iterations: binary.BigEndian.Uint32(encoded[3:7]),
		Seed:       int64(binary.BigEndian.Uint64(encoded[7:15])),
	}, nil
}

type resultCodec struct{}

func (resultCodec) Encode(value Result) ([]byte, error) {
	encoded := make([]byte, 9)
	encoded[0] = codecVersion
	binary.BigEndian.PutUint64(encoded[1:9], uint64(value.Checksum))
	return encoded, nil
}

func (resultCodec) Decode(encoded []byte) (Result, error) {
	if err := validateEncoding(encoded, 9, "result"); err != nil {
		return Result{}, err
	}
	return Result{Checksum: int64(binary.BigEndian.Uint64(encoded[1:9]))}, nil
}

type checkpointCodec struct{}

func (checkpointCodec) Encode(Checkpoint) ([]byte, error) { return []byte{codecVersion}, nil }

func (checkpointCodec) Decode(encoded []byte) (Checkpoint, error) {
	if err := validateEncoding(encoded, 1, "checkpoint"); err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{}, nil
}

type effectCodec struct{}

func (effectCodec) Encode(Effect) ([]byte, error) { return []byte{codecVersion}, nil }

func (effectCodec) Decode(encoded []byte) (Effect, error) {
	if err := validateEncoding(encoded, 1, "effect"); err != nil {
		return Effect{}, err
	}
	return Effect{}, nil
}

func validateEncoding(encoded []byte, size int, name string) error {
	if len(encoded) != size || len(encoded) == 0 || encoded[0] != codecVersion {
		return fmt.Errorf("prepared worker parity: invalid %s encoding", name)
	}
	return nil
}
