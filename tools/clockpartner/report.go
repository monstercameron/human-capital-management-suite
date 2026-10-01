package clockpartner

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PassReport is a signed, profile-bound conformance result. A failed run
// cannot be marshalled as a pass report.
type PassReport struct {
	Version         int       `json:"version"`
	Profile         string    `json:"profile"`
	TenantID        string    `json:"tenant_id"`
	Passed          bool      `json:"passed"`
	ResultDigest    string    `json:"result_digest"`
	SignedAt        time.Time `json:"signed_at"`
	SignerPublicKey string    `json:"signer_public_key"`
	Signature       string    `json:"signature"`
}

func mandatoryObservationNames() []string {
	return []string{"enrollment-code", "tenant-isolation", "enrollment", "heartbeat", "roster-sync", "identify", "offline-replay", "sequence-gap", "duplicate-replay", "clock-drift", "webhook-receipt", "revocation"}
}

func resultDigest(result Result) [32]byte {
	b, _ := json.Marshal(result)
	return sha256.Sum256(b)
}

// SignPassReport signs a successful observed result with Ed25519.
func SignPassReport(result Result, profile string, key ed25519.PrivateKey, at time.Time) (PassReport, error) {
	if !result.Passed || result.Profile != profile || len(key) != ed25519.PrivateKeySize || at.IsZero() || result.provenance == [32]byte{} || resultDigest(result) != result.provenance {
		return PassReport{}, fmt.Errorf("%w: only a passed result with valid profile, key and time may be signed", ErrConformance)
	}
	seen := make(map[string]bool, len(result.Observations))
	for _, observation := range result.Observations {
		if observation.Passed {
			seen[observation.Name] = true
		}
	}
	mandatory := mandatoryObservationNames()
	if len(result.Observations) != len(mandatory) {
		return PassReport{}, fmt.Errorf("%w: observation set is incomplete or duplicated", ErrConformance)
	}
	for _, name := range mandatory {
		if !seen[name] {
			return PassReport{}, fmt.Errorf("%w: mandatory observation %s is missing", ErrConformance, name)
		}
	}
	b, err := json.Marshal(result)
	if err != nil {
		return PassReport{}, err
	}
	digest := sha256.Sum256(b)
	payload := []byte(hex.EncodeToString(digest[:]) + "|" + profile + "|" + result.TenantID + "|" + at.UTC().Format(time.RFC3339Nano))
	sig := ed25519.Sign(key, payload)
	pub := key.Public().(ed25519.PublicKey)
	return PassReport{Version: 1, Profile: profile, TenantID: result.TenantID, Passed: true, ResultDigest: "sha256:" + hex.EncodeToString(digest[:]), SignedAt: at.UTC(), SignerPublicKey: hex.EncodeToString(pub), Signature: hex.EncodeToString(sig)}, nil
}

// Verify checks the signature and rejects a report that is not an explicit pass.
func (r PassReport) Verify(expected ed25519.PublicKey) error {
	if !r.Passed || r.Version != 1 || r.Profile == "" || r.TenantID == "" || r.ResultDigest == "" || r.SignedAt.IsZero() {
		return fmt.Errorf("%w: invalid pass report", ErrConformance)
	}
	if len(expected) != ed25519.PublicKeySize || strings.TrimSpace(r.SignerPublicKey) == "" {
		return fmt.Errorf("%w: invalid trusted signer key", ErrConformance)
	}
	if !strings.HasPrefix(r.ResultDigest, "sha256:") || len(r.ResultDigest) != len("sha256:")+64 {
		return fmt.Errorf("%w: invalid result digest", ErrConformance)
	}
	if _, err := hex.DecodeString(r.ResultDigest[len("sha256:"):]); err != nil {
		return fmt.Errorf("%w: invalid result digest", ErrConformance)
	}
	pub, err := hex.DecodeString(r.SignerPublicKey)
	if err != nil || !bytes.Equal(pub, expected) {
		return fmt.Errorf("%w: invalid signer key encoding", ErrConformance)
	}
	sig, err := hex.DecodeString(r.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(expected, []byte(r.ResultDigest[len("sha256:"):]+"|"+r.Profile+"|"+r.TenantID+"|"+r.SignedAt.UTC().Format(time.RFC3339Nano)), sig) {
		return fmt.Errorf("%w: signature mismatch", ErrConformance)
	}
	return nil
}
