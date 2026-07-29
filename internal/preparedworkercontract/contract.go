// Package preparedworkercontract derives the private canonical identity and
// record limits of a typed transaction contract without depending on runtime,
// process, or build behavior.
package preparedworkercontract

import (
	"bytes"
	"encoding/binary"
	"fmt"

	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
)

// Schema is the identity-bearing boundary shape of one typed record.
type Schema struct {
	Name         string
	MaxBytes     int
	CodecPresent bool
}

// Specification is the complete identity-bearing application contract.
type Specification struct {
	CodecVersion uint32
	Request      Schema
	Result       Schema
	Checkpoint   Schema
	Effect       Schema
	MaxEffects   int
}

// Description is the validated canonical contract used independently by the
// runtime and build modules.
type Description struct {
	RequestSchema      workerartifact.Digest
	ResultSchema       workerartifact.Digest
	CheckpointSchema   workerartifact.Digest
	EffectSchema       workerartifact.Digest
	Contract           workerartifact.Digest
	MaxRequestBytes    int
	MaxResultBytes     int
	MaxCheckpointBytes int
	MaxEffects         int
	MaxEffectBytes     int
}

// Derive validates a specification and returns its canonical identities and
// bounds. Candidate and retirement limits are lifecycle policy and are not
// part of the typed application identity.
func Derive(specification Specification) (Description, error) {
	if specification.CodecVersion == 0 || specification.MaxEffects <= 0 || specification.MaxEffects > 1<<20 {
		return Description{}, fmt.Errorf("prepared worker: incomplete contract limits")
	}
	for _, schema := range []struct {
		label string
		value Schema
	}{
		{label: "request", value: specification.Request},
		{label: "result", value: specification.Result},
		{label: "checkpoint", value: specification.Checkpoint},
		{label: "effect", value: specification.Effect},
	} {
		if err := validateSchema(schema.value); err != nil {
			return Description{}, fmt.Errorf("prepared worker %s schema: %w", schema.label, err)
		}
	}
	description := Description{
		RequestSchema:      schemaIdentity(specification.Request.Name),
		ResultSchema:       schemaIdentity(specification.Result.Name),
		CheckpointSchema:   schemaIdentity(specification.Checkpoint.Name),
		EffectSchema:       schemaIdentity(specification.Effect.Name),
		MaxRequestBytes:    specification.Request.MaxBytes,
		MaxResultBytes:     specification.Result.MaxBytes,
		MaxCheckpointBytes: specification.Checkpoint.MaxBytes,
		MaxEffects:         specification.MaxEffects,
		MaxEffectBytes:     specification.Effect.MaxBytes,
	}
	description.Contract = contractIdentity(description, specification.CodecVersion)
	return description, nil
}

func schemaIdentity(name string) workerartifact.Digest {
	return workerartifact.Hash([]byte("prepared-worker-schema-v1:" + name))
}

func contractIdentity(description Description, codecVersion uint32) workerartifact.Digest {
	var encoded bytes.Buffer
	_, _ = encoded.WriteString("ember-prepared-worker-contract-v1")
	for _, digest := range []workerartifact.Digest{
		description.RequestSchema,
		description.ResultSchema,
		description.CheckpointSchema,
		description.EffectSchema,
	} {
		_, _ = encoded.Write(digest[:])
	}
	_ = binary.Write(&encoded, binary.BigEndian, codecVersion)
	for _, value := range []int{
		description.MaxRequestBytes,
		description.MaxResultBytes,
		description.MaxCheckpointBytes,
		description.MaxEffects,
		description.MaxEffectBytes,
	} {
		_ = binary.Write(&encoded, binary.BigEndian, uint64(value))
	}
	return workerartifact.Hash(encoded.Bytes())
}

func validateSchema(schema Schema) error {
	if schema.Name == "" || len(schema.Name) > 4096 {
		return fmt.Errorf("name is required and must be at most 4096 bytes")
	}
	if schema.MaxBytes <= 0 || schema.MaxBytes > 1<<30 || !schema.CodecPresent {
		return fmt.Errorf("positive bounded byte limit and codec are required")
	}
	return nil
}
