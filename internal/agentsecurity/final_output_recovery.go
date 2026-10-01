package agentsecurity

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// ErrFinalOutputRecoveryUnavailable indicates that durable recovery cannot
// prove the stored result was issued by the server-owned validation boundary.
var ErrFinalOutputRecoveryUnavailable = errors.New("agent output recovery unavailable")

// FinalOutputRecoveryRecord is the complete durable row covered by a recovery
// receipt. JSON fields are canonicalized before signing and verification.
type FinalOutputRecoveryRecord struct {
	Identity          FinalOutputIdentity
	AdmissionDigest   string
	SemanticDigest    string
	PersistenceDigest string
	RecoveryReceipt   []byte
	Materials         json.RawMessage
	Citations         json.RawMessage
	SealedPayload     json.RawMessage
}

// FinalOutputRecoveryAuthority owns the private key used to issue receipts.
// Keep this value in the server composition root and do not expose it to callers.
type FinalOutputRecoveryAuthority struct {
	keyID string
	key   ed25519.PrivateKey
}

// FinalOutputRecoveryVerifier verifies receipts against configured public
// keys. A verifier cannot mint receipts.
type FinalOutputRecoveryVerifier struct {
	keys map[string]ed25519.PublicKey
}

// NewFinalOutputRecoveryAuthority creates the server-side receipt authority.
// The caller must supply a persistent production key so receipts survive restart.
func NewFinalOutputRecoveryAuthority(keyID string, privateKey ed25519.PrivateKey) (*FinalOutputRecoveryAuthority, error) {
	if strings.TrimSpace(keyID) == "" || strings.TrimSpace(keyID) != keyID || len(privateKey) != ed25519.PrivateKeySize {
		return nil, ErrFinalOutputRecoveryUnavailable
	}
	return &FinalOutputRecoveryAuthority{keyID: keyID, key: append(ed25519.PrivateKey(nil), privateKey...)}, nil
}

// NewFinalOutputRecoveryVerifier creates a verifier with the trusted receipt
// public keys. Include retired keys only for the required recovery retention.
func NewFinalOutputRecoveryVerifier(keys map[string]ed25519.PublicKey) (*FinalOutputRecoveryVerifier, error) {
	if len(keys) == 0 {
		return nil, ErrFinalOutputRecoveryUnavailable
	}
	copyKeys := make(map[string]ed25519.PublicKey, len(keys))
	for id, key := range keys {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id || len(key) != ed25519.PublicKeySize {
			return nil, ErrFinalOutputRecoveryUnavailable
		}
		copyKeys[id] = append(ed25519.PublicKey(nil), key...)
	}
	return &FinalOutputRecoveryVerifier{keys: copyKeys}, nil
}

// IssueFinalOutputRecoveryReceipt signs the complete persisted row. The receipt
// itself is excluded from the signed bytes to avoid recursive encoding.
func (a *FinalOutputRecoveryAuthority) IssueFinalOutputRecoveryReceipt(record FinalOutputRecoveryRecord) ([]byte, error) {
	if a == nil || len(a.key) != ed25519.PrivateKeySize || !validRecoveryRecord(record) {
		return nil, ErrFinalOutputRecoveryUnavailable
	}
	message, err := canonicalRecoveryBytes(record)
	if err != nil {
		return nil, ErrFinalOutputRecoveryUnavailable
	}
	signature := ed25519.Sign(a.key, message)
	return []byte("v1." + base64.RawURLEncoding.EncodeToString([]byte(a.keyID)) + "." + base64.RawURLEncoding.EncodeToString(signature)), nil
}

// VerifyFinalOutputRecovery verifies a receipt using only configured public
// keys and binds all identity, digest, and persisted JSON content.
func (v *FinalOutputRecoveryVerifier) VerifyFinalOutputRecovery(_ context.Context, record FinalOutputRecoveryRecord) error {
	if v == nil || !validRecoveryRecord(record) {
		return ErrFinalOutputRecoveryUnavailable
	}
	parts := strings.Split(string(record.RecoveryReceipt), ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return ErrFinalOutputRecoveryUnavailable
	}
	keyIDBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrFinalOutputRecoveryUnavailable
	}
	key := v.keys[string(keyIDBytes)]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(key) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return ErrFinalOutputRecoveryUnavailable
	}
	message, err := canonicalRecoveryBytes(record)
	if err != nil || !ed25519.Verify(key, message, signature) {
		return ErrFinalOutputRecoveryUnavailable
	}
	return nil
}

// FinalOutputRecoveryRehydrator rebuilds a projection from authoritative
// sources after the receipt has been verified.
type FinalOutputRecoveryRehydrator interface {
	RehydrateFinalOutput(context.Context, FinalOutputRecoveryRecord) (FinalOutputPersistence, error)
}

// RecoverFinalOutputPersistence verifies and rehydrates one persisted output
// after a process restart. Every authority is required and untrusted JSON is
// never accepted without a valid server receipt.
func RecoverFinalOutputPersistence(ctx context.Context, record FinalOutputRecoveryRecord, verifier *FinalOutputRecoveryVerifier, rehydrator FinalOutputRecoveryRehydrator) (FinalOutputPersistence, error) {
	if ctx == nil || !validRecoveryRecord(record) || verifier == nil || rehydrator == nil {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	if err := verifier.VerifyFinalOutputRecovery(ctx, record); err != nil {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	projection, err := rehydrator.RehydrateFinalOutput(ctx, record)
	if err != nil || !recoveredProjectionMatches(projection, record) {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	return projection, nil
}

func canonicalRecoveryBytes(record FinalOutputRecoveryRecord) ([]byte, error) {
	canonical := func(raw json.RawMessage) (json.RawMessage, error) {
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(value)
		return json.RawMessage(encoded), err
	}
	materials, err := canonical(record.Materials)
	if err != nil {
		return nil, err
	}
	citations, err := canonical(record.Citations)
	if err != nil {
		return nil, err
	}
	payload, err := canonical(record.SealedPayload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Version           string              `json:"version"`
		Identity          FinalOutputIdentity `json:"identity"`
		AdmissionDigest   string              `json:"admission_digest"`
		SemanticDigest    string              `json:"semantic_digest"`
		PersistenceDigest string              `json:"persistence_digest"`
		Materials         json.RawMessage     `json:"materials"`
		Citations         json.RawMessage     `json:"citations"`
		SealedPayload     json.RawMessage     `json:"sealed_payload"`
	}{"hcm-next-agent-output-recovery/v1", record.Identity, record.AdmissionDigest, record.SemanticDigest, record.PersistenceDigest, materials, citations, payload})
}

func validRecoveryRecord(record FinalOutputRecoveryRecord) bool {
	i := record.Identity
	return strings.TrimSpace(i.TenantID) != "" && strings.TrimSpace(i.OutputID) != "" &&
		strings.TrimSpace(i.InvocationID) != "" && strings.TrimSpace(i.AdmissionID) != "" && strings.TrimSpace(i.RunID) != "" && strings.TrimSpace(i.InvokerID) != "" &&
		strings.TrimSpace(i.ConversationID) != "" && strings.TrimSpace(i.ThreadID) != "" &&
		strings.TrimSpace(i.PostID) != "" && strings.TrimSpace(i.PersonaID) != "" &&
		strings.TrimSpace(i.PersonaVersion) != "" && strings.TrimSpace(i.InstallationID) != "" &&
		strings.TrimSpace(record.AdmissionDigest) != "" && strings.TrimSpace(record.SemanticDigest) != "" &&
		strings.TrimSpace(record.PersistenceDigest) != "" && validJSON(record.Materials, '[') &&
		validJSON(record.Citations, '[') && validJSON(record.SealedPayload, '{')
}

func validJSON(raw json.RawMessage, first byte) bool {
	trimmed := strings.TrimSpace(string(raw))
	return len(trimmed) > 0 && trimmed[0] == first && json.Valid([]byte(trimmed))
}

func recoveredProjectionMatches(projection FinalOutputPersistence, record FinalOutputRecoveryRecord) bool {
	return projection.Identity() == record.Identity && projection.AdmissionDigest() == record.AdmissionDigest &&
		projection.SemanticDigest() == record.SemanticDigest && projection.Digest() == record.PersistenceDigest
}
