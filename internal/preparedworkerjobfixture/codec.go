package preparedworkerjobfixture

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	requestTag    = 0xa2
	resultTag     = 0xb2
	checkpointTag = 0xc2
	effectTag     = 0xd2
)

type requestCodec struct{}

func (requestCodec) Encode(request Request) ([]byte, error) {
	if len(request.Jobs) > math.MaxUint16 {
		return nil, fmt.Errorf("prepared job fixture: job count %d exceeds wire limit", len(request.Jobs))
	}
	encoded := make([]byte, 0, 6+len(request.Jobs)*8)
	encoded = append(encoded, requestTag, codecVersion)
	encoded = appendUint16(encoded, request.Queue)
	encoded = appendUint16(encoded, uint16(len(request.Jobs)))
	for _, job := range request.Jobs {
		encoded = appendUint32(encoded, job.ID)
		encoded = appendUint16(encoded, job.Seed)
		encoded = appendUint16(encoded, job.Work)
	}
	return encoded, nil
}

func (requestCodec) Decode(encoded []byte) (Request, error) {
	if len(encoded) < 6 || encoded[0] != requestTag || encoded[1] != codecVersion {
		return Request{}, fmt.Errorf("prepared job fixture: invalid request header")
	}
	count := int(binary.BigEndian.Uint16(encoded[4:6]))
	if len(encoded) != 6+count*8 {
		return Request{}, fmt.Errorf("prepared job fixture: request length %d does not match count %d", len(encoded), count)
	}
	request := Request{Queue: binary.BigEndian.Uint16(encoded[2:4]), Jobs: make([]Job, count)}
	offset := 6
	for index := range request.Jobs {
		request.Jobs[index] = Job{
			ID:   binary.BigEndian.Uint32(encoded[offset : offset+4]),
			Seed: binary.BigEndian.Uint16(encoded[offset+4 : offset+6]),
			Work: binary.BigEndian.Uint16(encoded[offset+6 : offset+8]),
		}
		offset += 8
	}
	return request, nil
}

type resultCodec struct{}

func (resultCodec) Encode(result Result) ([]byte, error) {
	encoded := []byte{resultTag, codecVersion}
	encoded = appendUint16(encoded, result.Queue)
	encoded = appendUint16(encoded, result.Accepted)
	encoded = appendUint32(encoded, result.Completed)
	encoded = appendInt64(encoded, result.Checksum)
	encoded = appendUint64(encoded, result.Cursor)
	return encoded, nil
}

func (resultCodec) Decode(encoded []byte) (Result, error) {
	if len(encoded) != 26 || encoded[0] != resultTag || encoded[1] != codecVersion {
		return Result{}, fmt.Errorf("prepared job fixture: invalid result record")
	}
	return Result{
		Queue:     binary.BigEndian.Uint16(encoded[2:4]),
		Accepted:  binary.BigEndian.Uint16(encoded[4:6]),
		Completed: binary.BigEndian.Uint32(encoded[6:10]),
		Checksum:  int64(binary.BigEndian.Uint64(encoded[10:18])),
		Cursor:    binary.BigEndian.Uint64(encoded[18:26]),
	}, nil
}

type checkpointCodec struct{}

func (checkpointCodec) Encode(checkpoint Checkpoint) ([]byte, error) {
	if err := validateCheckpoint(checkpoint); err != nil {
		return nil, err
	}
	encoded := []byte{checkpointTag, codecVersion, checkpoint.Format}
	encoded = appendUint64(encoded, checkpoint.Sequence)
	encoded = appendUint64(encoded, checkpoint.Revision)
	encoded = appendUint16(encoded, checkpoint.Queue)
	encoded = appendUint32(encoded, checkpoint.Completed)
	encoded = appendInt64(encoded, checkpoint.Checksum)
	encoded = appendUint64(encoded, checkpoint.Cursor)
	return encoded, nil
}

func (checkpointCodec) Decode(encoded []byte) (Checkpoint, error) {
	if len(encoded) != 41 || encoded[0] != checkpointTag || encoded[1] != codecVersion {
		return Checkpoint{}, fmt.Errorf("prepared job fixture: invalid checkpoint record")
	}
	checkpoint := Checkpoint{
		Format:    encoded[2],
		Sequence:  binary.BigEndian.Uint64(encoded[3:11]),
		Revision:  binary.BigEndian.Uint64(encoded[11:19]),
		Queue:     binary.BigEndian.Uint16(encoded[19:21]),
		Completed: binary.BigEndian.Uint32(encoded[21:25]),
		Checksum:  int64(binary.BigEndian.Uint64(encoded[25:33])),
		Cursor:    binary.BigEndian.Uint64(encoded[33:41]),
	}
	if err := validateCheckpoint(checkpoint); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

type effectCodec struct{}

func (effectCodec) Encode(effect Effect) ([]byte, error) {
	encoded := []byte{effectTag, codecVersion}
	encoded = appendUint32(encoded, effect.JobID)
	encoded = appendUint64(encoded, effect.Ticket)
	encoded = appendInt64(encoded, effect.Checksum)
	return encoded, nil
}

func (effectCodec) Decode(encoded []byte) (Effect, error) {
	if len(encoded) != 22 || encoded[0] != effectTag || encoded[1] != codecVersion {
		return Effect{}, fmt.Errorf("prepared job fixture: invalid effect record")
	}
	return Effect{
		JobID:    binary.BigEndian.Uint32(encoded[2:6]),
		Ticket:   binary.BigEndian.Uint64(encoded[6:14]),
		Checksum: int64(binary.BigEndian.Uint64(encoded[14:22])),
	}, nil
}

func appendUint16(encoded []byte, value uint16) []byte {
	var field [2]byte
	binary.BigEndian.PutUint16(field[:], value)
	return append(encoded, field[:]...)
}

func appendUint32(encoded []byte, value uint32) []byte {
	var field [4]byte
	binary.BigEndian.PutUint32(field[:], value)
	return append(encoded, field[:]...)
}

func appendUint64(encoded []byte, value uint64) []byte {
	var field [8]byte
	binary.BigEndian.PutUint64(field[:], value)
	return append(encoded, field[:]...)
}

func appendInt64(encoded []byte, value int64) []byte {
	return appendUint64(encoded, uint64(value))
}
