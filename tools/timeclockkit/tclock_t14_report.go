package timeclockkit

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

// PassReport is a profile- and tenant-bound certification result.
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

func resultDigest(result Result) [32]byte {
	b, _ := json.Marshal(result)
	return sha256.Sum256(b)
}

// SignPassReport signs only a complete successful Run result. The private
// provenance marker prevents callers from constructing a passing report by
// setting Result.Passed directly.
func SignPassReport(result Result, profile string, key ed25519.PrivateKey, at time.Time) (PassReport, error) {
	if !result.Passed || result.Profile != profile || len(key) != ed25519.PrivateKeySize || at.IsZero() || result.provenance == [32]byte{} || resultDigest(result) != result.provenance {
		return PassReport{}, fmt.Errorf("%w: only a complete passed result may be signed", ErrConformance)
	}
	seen := make(map[string]bool, len(result.Observations))
	for _, observation := range result.Observations {
		if !observation.Passed || seen[observation.Name] {
			return PassReport{}, fmt.Errorf("%w: observation set is incomplete", ErrConformance)
		}
		seen[observation.Name] = true
	}
	for _, name := range mandatoryObservations {
		if !seen[name] {
			return PassReport{}, fmt.Errorf("%w: mandatory observation %s is missing", ErrConformance, name)
		}
	}
	digest := resultDigest(result)
	digestText := hex.EncodeToString(digest[:])
	payload := []byte(digestText + "|" + profile + "|" + result.TenantID + "|" + at.UTC().Format(time.RFC3339Nano))
	public := key.Public().(ed25519.PublicKey)
	return PassReport{
		Version: 1, Profile: profile, TenantID: result.TenantID, Passed: true,
		ResultDigest: "sha256:" + digestText, SignedAt: at.UTC(),
		SignerPublicKey: hex.EncodeToString(public), Signature: hex.EncodeToString(ed25519.Sign(key, payload)),
	}, nil
}

// Verify validates the trusted signer, profile binding, and Ed25519 signature.
func (report PassReport) Verify(expected ed25519.PublicKey) error {
	if !report.Passed || report.Version != 1 || report.Profile == "" || report.TenantID == "" || report.SignedAt.IsZero() || len(expected) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: invalid pass report", ErrConformance)
	}
	if !strings.HasPrefix(report.ResultDigest, "sha256:") || len(report.ResultDigest) != len("sha256:")+64 {
		return fmt.Errorf("%w: invalid result digest", ErrConformance)
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(report.ResultDigest, "sha256:"))
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("%w: invalid result digest", ErrConformance)
	}
	public, err := hex.DecodeString(report.SignerPublicKey)
	if err != nil || !bytes.Equal(public, expected) {
		return fmt.Errorf("%w: unexpected signer", ErrConformance)
	}
	signature, err := hex.DecodeString(report.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: invalid signature", ErrConformance)
	}
	payload := []byte(hex.EncodeToString(digest) + "|" + report.Profile + "|" + report.TenantID + "|" + report.SignedAt.UTC().Format(time.RFC3339Nano))
	if !ed25519.Verify(expected, payload, signature) {
		return fmt.Errorf("%w: signature mismatch", ErrConformance)
	}
	return nil
}
