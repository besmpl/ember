// Package preparedworkerartifact owns the private canonical representation,
// hashing, and validation of supervised static-AOT worker artifacts. It has no
// runtime, process, or application-contract policy.
package preparedworkerartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const (
	DescriptorVersion = uint32(1)
	ProtocolVersion   = uint32(1)
)

// Digest is one SHA-256 content digest.
type Digest [sha256.Size]byte

// Hash returns the SHA-256 digest of data.
func Hash(data []byte) Digest { return sha256.Sum256(data) }

func (digest Digest) String() string { return hex.EncodeToString(digest[:]) }

// Descriptor is every pre-link input attested by one worker build ID. The
// executable digest is recorded separately in its manifest to avoid a
// self-hash cycle.
type Descriptor struct {
	FormatVersion       uint32
	ProtocolVersion     uint32
	Contract            Digest
	ProgramRecipeDigest Digest
	PreparedABIVersion  uint32
	SemanticVersion     uint32
	ProgramHash         Digest
	ProtoCounts         []uint32
	GeneratedDigest     Digest
	WrapperDigest       Digest
	EmberVersion        string
	Toolchain           string
	TargetOS            string
	TargetArch          string
	BuildTags           []string
	BuildFlags          []string
}

// BuildID returns the descriptor's canonical content identity.
func (descriptor Descriptor) BuildID() (Digest, error) {
	encoded, err := encodeDescriptor(descriptor)
	if err != nil {
		return Digest{}, err
	}
	return Hash(encoded), nil
}

// Clone returns a detached descriptor.
func (descriptor Descriptor) Clone() Descriptor {
	cloned := descriptor
	cloned.ProtoCounts = append([]uint32(nil), descriptor.ProtoCounts...)
	cloned.BuildTags = append([]string(nil), descriptor.BuildTags...)
	cloned.BuildFlags = append([]string(nil), descriptor.BuildFlags...)
	return cloned
}

// HashExecutable hashes one bounded real regular file and rejects concurrent
// size changes.
func HashExecutable(path string, limit int64) (Digest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Digest{}, fmt.Errorf("prepared worker artifact: open executable: %w", err)
	}
	defer file.Close()
	metadata, err := file.Stat()
	if err != nil {
		return Digest{}, fmt.Errorf("prepared worker artifact: stat executable: %w", err)
	}
	if !metadata.Mode().IsRegular() {
		return Digest{}, fmt.Errorf("prepared worker artifact: executable is not a regular file")
	}
	if metadata.Size() <= 0 || metadata.Size() > limit {
		return Digest{}, fmt.Errorf(
			"prepared worker artifact: executable uses %d bytes, limit %d",
			metadata.Size(),
			limit,
		)
	}
	hasher := sha256.New()
	written, err := io.CopyN(hasher, file, metadata.Size())
	if err != nil {
		return Digest{}, fmt.Errorf("prepared worker artifact: hash executable: %w", err)
	}
	if written != metadata.Size() {
		return Digest{}, fmt.Errorf("prepared worker artifact: executable changed while hashing")
	}
	var extra [1]byte
	if read, err := file.Read(extra[:]); err != io.EOF || read != 0 {
		return Digest{}, fmt.Errorf("prepared worker artifact: executable changed while hashing")
	}
	var digest Digest
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func encodeDescriptor(descriptor Descriptor) ([]byte, error) {
	if err := validateDescriptor(descriptor); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	writeUint32 := func(value uint32) { _ = binary.Write(&encoded, binary.BigEndian, value) }
	writeBytes := func(value []byte) {
		writeUint32(uint32(len(value)))
		_, _ = encoded.Write(value)
	}
	writeString := func(value string) { writeBytes([]byte(value)) }
	writeDigest := func(value Digest) { _, _ = encoded.Write(value[:]) }
	writeUint32(descriptor.FormatVersion)
	writeUint32(descriptor.ProtocolVersion)
	writeDigest(descriptor.Contract)
	writeDigest(descriptor.ProgramRecipeDigest)
	writeUint32(descriptor.PreparedABIVersion)
	writeUint32(descriptor.SemanticVersion)
	writeDigest(descriptor.ProgramHash)
	writeUint32(uint32(len(descriptor.ProtoCounts)))
	for _, count := range descriptor.ProtoCounts {
		writeUint32(count)
	}
	writeDigest(descriptor.GeneratedDigest)
	writeDigest(descriptor.WrapperDigest)
	writeString(descriptor.EmberVersion)
	writeString(descriptor.Toolchain)
	writeString(descriptor.TargetOS)
	writeString(descriptor.TargetArch)
	writeUint32(uint32(len(descriptor.BuildTags)))
	for _, tag := range descriptor.BuildTags {
		writeString(tag)
	}
	writeUint32(uint32(len(descriptor.BuildFlags)))
	for _, flag := range descriptor.BuildFlags {
		writeString(flag)
	}
	return encoded.Bytes(), nil
}

func validateDescriptor(descriptor Descriptor) error {
	if descriptor.FormatVersion != DescriptorVersion {
		return fmt.Errorf("prepared worker artifact: descriptor version %d, want %d", descriptor.FormatVersion, DescriptorVersion)
	}
	if descriptor.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("prepared worker artifact: protocol version %d, want %d", descriptor.ProtocolVersion, ProtocolVersion)
	}
	if descriptor.Contract == (Digest{}) || descriptor.ProgramRecipeDigest == (Digest{}) ||
		descriptor.ProgramHash == (Digest{}) || descriptor.GeneratedDigest == (Digest{}) ||
		descriptor.WrapperDigest == (Digest{}) {
		return fmt.Errorf("prepared worker artifact: descriptor identities are incomplete")
	}
	if descriptor.PreparedABIVersion == 0 || descriptor.SemanticVersion == 0 {
		return fmt.Errorf("prepared worker artifact: prepared ABI and semantics are required")
	}
	if len(descriptor.ProtoCounts) == 0 || len(descriptor.ProtoCounts) > 1<<20 {
		return fmt.Errorf("prepared worker artifact: Proto inventory is out of bounds")
	}
	for _, count := range descriptor.ProtoCounts {
		if count == 0 {
			return fmt.Errorf("prepared worker artifact: empty Proto inventory module")
		}
	}
	if descriptor.EmberVersion == "" || descriptor.Toolchain == "" ||
		descriptor.TargetOS == "" || descriptor.TargetArch == "" {
		return fmt.Errorf("prepared worker artifact: build identity strings are incomplete")
	}
	if len(descriptor.EmberVersion) > 4096 || len(descriptor.Toolchain) > 4096 ||
		len(descriptor.TargetOS) > 64 || len(descriptor.TargetArch) > 64 {
		return fmt.Errorf("prepared worker artifact: build identity string is too large")
	}
	if err := validateCanonicalStrings("build tags", descriptor.BuildTags); err != nil {
		return err
	}
	if err := validateStrings("build flags", descriptor.BuildFlags); err != nil {
		return err
	}
	return nil
}

func validateCanonicalStrings(label string, values []string) error {
	if err := validateStrings(label, values); err != nil {
		return err
	}
	for index := 1; index < len(values); index++ {
		if values[index-1] >= values[index] {
			return fmt.Errorf("prepared worker artifact: %s must be sorted and unique", label)
		}
	}
	return nil
}

func validateStrings(label string, values []string) error {
	if len(values) > 4096 {
		return fmt.Errorf("prepared worker artifact: %s count is out of bounds", label)
	}
	for _, value := range values {
		if value == "" || len(value) > 4096 {
			return fmt.Errorf("prepared worker artifact: %s item is invalid", label)
		}
	}
	return nil
}
