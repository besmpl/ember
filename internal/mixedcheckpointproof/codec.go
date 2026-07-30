package mixedcheckpointproof

import (
	"encoding/binary"
	"fmt"

	ruby "github.com/besmpl/ember/internal/rubyproof/generated"
	sprig "github.com/besmpl/ember/internal/sprigproof/generated"
)

const (
	requestBytes    = 17
	resultBytes     = 41
	checkpointBytes = 28
	effectBytes     = 24
)

type requestCodec struct{}

func (requestCodec) Encode(value Request) ([]byte, error) {
	data := make([]byte, requestBytes)
	putInt64(data[0:8], value.Reading.Value)
	putInt64(data[8:16], value.Reading.Divisor)
	if value.AdvanceRuby {
		data[16] = 1
	}
	return data, nil
}

func (requestCodec) Decode(data []byte) (Request, error) {
	if len(data) != requestBytes {
		return Request{}, fmt.Errorf("mixed checkpoint proof: request bytes %d, want %d", len(data), requestBytes)
	}
	if data[16] > 1 {
		return Request{}, fmt.Errorf("mixed checkpoint proof: non-canonical advance byte %d", data[16])
	}
	return Request{
		Reading:     sprig.Reading{Value: readInt64(data[0:8]), Divisor: readInt64(data[8:16])},
		AdvanceRuby: data[16] == 1,
	}, nil
}

type resultCodec struct{}

func (resultCodec) Encode(value Result) ([]byte, error) {
	if err := validateResult(value); err != nil {
		return nil, err
	}
	data := make([]byte, resultBytes)
	putInt64(data[0:8], int64(value.Domain))
	if value.Rejected {
		data[8] = 1
	}
	putInt64(data[9:17], value.RejectionCode)
	putInt64(data[17:25], value.Ruby.Value)
	putInt64(data[25:33], value.Ruby.Trace)
	putInt64(data[33:41], value.SprigTotal)
	return data, nil
}

func (resultCodec) Decode(data []byte) (Result, error) {
	if len(data) != resultBytes {
		return Result{}, fmt.Errorf("mixed checkpoint proof: result bytes %d, want %d", len(data), resultBytes)
	}
	if data[8] > 1 {
		return Result{}, fmt.Errorf("mixed checkpoint proof: non-canonical rejected byte %d", data[8])
	}
	value := Result{
		Domain:        sprig.DomainCode(readInt64(data[0:8])),
		Rejected:      data[8] == 1,
		RejectionCode: readInt64(data[9:17]),
		Ruby:          rubyState(readInt64(data[17:25]), readInt64(data[25:33])),
		SprigTotal:    readInt64(data[33:41]),
	}
	if err := validateResult(value); err != nil {
		return Result{}, err
	}
	return value, nil
}

func validateResult(value Result) error {
	if value.Ruby.Trace < 0 {
		return fmt.Errorf("mixed checkpoint proof: negative Ruby trace")
	}
	if value.Domain != 0 && value.Domain != sprig.DomainOverflow && value.Domain != sprig.DomainDivideByZero {
		return fmt.Errorf("mixed checkpoint proof: invalid Sprig domain %d", value.Domain)
	}
	if value.Domain != 0 && (value.Rejected || value.RejectionCode != 0) {
		return fmt.Errorf("mixed checkpoint proof: domain result also carries rejection")
	}
	if !value.Rejected && value.RejectionCode != 0 {
		return fmt.Errorf("mixed checkpoint proof: rejection code without rejection")
	}
	return nil
}

type checkpointCodec struct{}

func (checkpointCodec) Encode(value Checkpoint) ([]byte, error) {
	if err := validateCheckpoint(value); err != nil {
		return nil, err
	}
	data := make([]byte, checkpointBytes)
	binary.LittleEndian.PutUint32(data[0:4], value.Version)
	putInt64(data[4:12], value.Ruby.Value)
	putInt64(data[12:20], value.Ruby.Trace)
	putInt64(data[20:28], value.SprigTotal)
	return data, nil
}

func (checkpointCodec) Decode(data []byte) (Checkpoint, error) {
	if len(data) != checkpointBytes {
		return Checkpoint{}, fmt.Errorf("mixed checkpoint proof: checkpoint bytes %d, want %d", len(data), checkpointBytes)
	}
	value := Checkpoint{
		Version:    binary.LittleEndian.Uint32(data[0:4]),
		Ruby:       rubyState(readInt64(data[4:12]), readInt64(data[12:20])),
		SprigTotal: readInt64(data[20:28]),
	}
	if err := validateCheckpoint(value); err != nil {
		return Checkpoint{}, err
	}
	return value, nil
}

func validateCheckpoint(value Checkpoint) error {
	if value.Version != CheckpointVersion {
		return fmt.Errorf("%w: version %d", ErrCheckpoint, value.Version)
	}
	if value.Ruby.Trace < 0 {
		return fmt.Errorf("%w: negative Ruby trace", ErrCheckpoint)
	}
	return nil
}

type effectCodec struct{}

func (effectCodec) Encode(value Effect) ([]byte, error) {
	if value.RubyTrace < 0 {
		return nil, fmt.Errorf("mixed checkpoint proof: negative effect trace")
	}
	data := make([]byte, effectBytes)
	putInt64(data[0:8], value.RubyValue)
	putInt64(data[8:16], value.RubyTrace)
	putInt64(data[16:24], value.SprigTotal)
	return data, nil
}

func (effectCodec) Decode(data []byte) (Effect, error) {
	if len(data) != effectBytes {
		return Effect{}, fmt.Errorf("mixed checkpoint proof: effect bytes %d, want %d", len(data), effectBytes)
	}
	value := Effect{RubyValue: readInt64(data[0:8]), RubyTrace: readInt64(data[8:16]), SprigTotal: readInt64(data[16:24])}
	if value.RubyTrace < 0 {
		return Effect{}, fmt.Errorf("mixed checkpoint proof: negative effect trace")
	}
	return value, nil
}

func putInt64(destination []byte, value int64) {
	binary.LittleEndian.PutUint64(destination, uint64(value))
}

func readInt64(source []byte) int64 {
	return int64(binary.LittleEndian.Uint64(source))
}

func rubyState(value, trace int64) ruby.State {
	return ruby.State{Value: value, Trace: trace}
}
