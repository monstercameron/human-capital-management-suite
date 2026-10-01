package clock

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const maxSignedRegistryBytes = 1 << 20

var (
	ErrSignedRegistrySignature = errors.New("clock: signed registry signature is invalid")
	ErrSignedRegistryValidity  = errors.New("clock: signed registry validity window is invalid")
	ErrSignedRegistryRevision  = errors.New("clock: signed registry revision is not pinned")
)

// SignedRegistryEnvelope carries a JSON payload and its detached Ed25519 signature.
type SignedRegistryEnvelope struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
	KeyID     string          `json:"key_id,omitempty"`
}

// SignedProfile describes the JSON form of an IntegrationProfile in a signed payload.
type SignedProfile struct {
	Class              SourceClass            `json:"class"`
	Transport          string                 `json:"transport"`
	Authentication     string                 `json:"authentication"`
	TrustCeiling       TrustCeiling           `json:"trust_ceiling"`
	PermittedMethods   []IdentificationMethod `json:"permitted_methods"`
	OfflineLimit       string                 `json:"offline_limit"`
	ClockTrustRequired bool                   `json:"clock_trust_required"`
	FirstPartner       string                 `json:"first_partner"`
	Version            string                 `json:"version"`
}

// SignedRegistryPayload is the signed, revisioned profile registry document.
type SignedRegistryPayload struct {
	Schema              string          `json:"schema"`
	Revision            string          `json:"revision"`
	ValidFrom           time.Time       `json:"valid_from"`
	ValidUntil          time.Time       `json:"valid_until"`
	Profiles            []SignedProfile `json:"profiles"`
	DefinitionDecisions map[string]any  `json:"definition_decisions,omitempty"`
}

// VerifiedProfileRegistry is the authenticated registry and its validity metadata.
type VerifiedProfileRegistry struct {
	Revision   string
	ValidFrom  time.Time
	ValidUntil time.Time
	Registry   ProfileRegistry
}

// VerifySignedProfileRegistry verifies a bounded, detached-signature registry.
func VerifySignedProfileRegistry(data []byte, trusted ed25519.PublicKey, now time.Time, pinnedRevision string) (VerifiedProfileRegistry, error) {
	if len(data) > maxSignedRegistryBytes {
		return VerifiedProfileRegistry{}, fmt.Errorf("clock: signed registry exceeds %d bytes", maxSignedRegistryBytes)
	}
	var envelope SignedRegistryEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return VerifiedProfileRegistry{}, fmt.Errorf("clock: decode signed registry: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return VerifiedProfileRegistry{}, errors.New("clock: signed registry has trailing JSON")
	}
	if len(trusted) != ed25519.PublicKeySize {
		return VerifiedProfileRegistry{}, errors.New("clock: trusted registry key has invalid length")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(trusted, envelope.Payload, signature) {
		return VerifiedProfileRegistry{}, ErrSignedRegistrySignature
	}
	var payload SignedRegistryPayload
	payloadDecoder := json.NewDecoder(bytes.NewReader(envelope.Payload))
	payloadDecoder.DisallowUnknownFields()
	if err := payloadDecoder.Decode(&payload); err != nil {
		return VerifiedProfileRegistry{}, fmt.Errorf("clock: decode signed registry payload: %w", err)
	}
	if err := payloadDecoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return VerifiedProfileRegistry{}, errors.New("clock: signed registry payload has trailing JSON")
	}
	if payload.Revision == "" || pinnedRevision == "" || payload.Revision != pinnedRevision {
		return VerifiedProfileRegistry{}, ErrSignedRegistryRevision
	}
	if payload.ValidFrom.IsZero() || payload.ValidUntil.IsZero() || !payload.ValidFrom.Before(payload.ValidUntil) || now.Before(payload.ValidFrom) || !now.Before(payload.ValidUntil) {
		return VerifiedProfileRegistry{}, ErrSignedRegistryValidity
	}
	profiles := make([]IntegrationProfile, len(payload.Profiles))
	for i, profile := range payload.Profiles {
		duration, err := time.ParseDuration(profile.OfflineLimit)
		if err != nil {
			return VerifiedProfileRegistry{}, fmt.Errorf("clock: profile %d offline limit: %w", i, err)
		}
		profiles[i] = IntegrationProfile{Class: profile.Class, Transport: profile.Transport, Authentication: profile.Authentication, TrustCeiling: profile.TrustCeiling, PermittedMethods: append([]IdentificationMethod(nil), profile.PermittedMethods...), OfflineLimit: duration, ClockTrustRequired: profile.ClockTrustRequired, FirstPartner: profile.FirstPartner, Version: profile.Version}
	}
	registry, err := NewProfileRegistry(profiles)
	if err != nil {
		return VerifiedProfileRegistry{}, err
	}
	return VerifiedProfileRegistry{Revision: payload.Revision, ValidFrom: payload.ValidFrom, ValidUntil: payload.ValidUntil, Registry: registry}, nil
}

// DecodeAndVerifySignedRegistry is an alias for VerifySignedProfileRegistry.
func DecodeAndVerifySignedRegistry(data []byte, trusted ed25519.PublicKey, now time.Time, pinnedRevision string) (VerifiedProfileRegistry, error) {
	return VerifySignedProfileRegistry(data, trusted, now, pinnedRevision)
}

// SignSignedRegistry signs the exact JSON payload with a build-time private key.
func SignSignedRegistry(payload SignedRegistryPayload, privateKey ed25519.PrivateKey, keyID string) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("clock: registry signing key has invalid length")
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("clock: encode signed registry payload: %w", err)
	}
	envelope := SignedRegistryEnvelope{Payload: payloadBytes, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payloadBytes)), KeyID: keyID}
	return json.Marshal(envelope)
}
