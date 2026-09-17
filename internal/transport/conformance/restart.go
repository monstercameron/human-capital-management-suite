package conformance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// ALIGN-061: product state survives restart and restore. The canonical
// restart path serializes an envelope to bytes and restores it in a fresh
// process; the restored envelope must validate and carry the exact same
// semantic digest. Decode refuses unversioned payloads, version skew, and
// anything whose digest no longer matches — a corrupted or forged backup
// never becomes product state.

// RestartEncodingVersion versions the restart envelope.
const RestartEncodingVersion = 1

// restartEnvelope is the versioned restore container.
type restartEnvelope struct {
	Version int           `json:"version"`
	Payload QueryEnvelope `json:"payload"`
	Digest  string        `json:"digest"`
}

// EncodeEnvelope serializes an envelope for restart or backup. The
// envelope must validate first, and the container pins its semantic
// digest so restore can prove identity.
func EncodeEnvelope(envelope QueryEnvelope) ([]byte, error) {
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	if envelope.SemanticDigest == "" {
		envelope.SemanticDigest = envelope.CanonicalDigest()
	}
	container := restartEnvelope{Version: RestartEncodingVersion, Payload: envelope, Digest: envelope.SemanticDigest}
	raw, err := json.Marshal(container)
	if err != nil {
		return nil, fmt.Errorf("%w: encode envelope: %v", ErrInvalidQuery, err)
	}
	return raw, nil
}

// DecodeEnvelope restores an envelope from restart bytes. It refuses
// unversioned or version-skewed payloads, revalidates the envelope, and
// requires the pinned digest to match the restored content: a backup that
// fails any check is not product state.
func DecodeEnvelope(raw []byte) (QueryEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var container restartEnvelope
	if err := decoder.Decode(&container); err != nil {
		return QueryEnvelope{}, fmt.Errorf("%w: decode envelope: %v", ErrInvalidQuery, err)
	}
	if container.Version != RestartEncodingVersion {
		return QueryEnvelope{}, fmt.Errorf("%w: restart version %d is not %d", ErrInvalidQuery, container.Version, RestartEncodingVersion)
	}
	if err := container.Payload.Validate(); err != nil {
		return QueryEnvelope{}, err
	}
	if container.Digest == "" || container.Payload.CanonicalDigest() != container.Digest {
		return QueryEnvelope{}, fmt.Errorf("%w: restored digest does not match the envelope", ErrInvalidQuery)
	}
	restored := container.Payload
	restored.SemanticDigest = container.Digest
	return restored, nil
}

// RestartDigest identifies a restart payload without parsing it as a
// query. Operators compare digests across backup, transfer, and restore.
func RestartDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
