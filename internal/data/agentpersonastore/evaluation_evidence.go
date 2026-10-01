package agentpersonastore

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errInvalidEvaluationEvidence = errors.New("agentpersonastore: invalid evaluation evidence")

// PersonaEvaluationClaim is the complete signed statement made by the trusted
// persona evaluator. Callers cannot turn an arbitrary pass boolean into
// evidence because both persistence and resolution verify its Ed25519 seal.
type PersonaEvaluationClaim struct {
	TenantID       string    `json:"tenant_id"`
	RunID          string    `json:"run_id"`
	PersonaID      string    `json:"persona_id"`
	PersonaVersion int64     `json:"persona_version"`
	ProfileDigest  string    `json:"profile_digest"`
	SuiteDigest    string    `json:"suite_digest"`
	RunDigest      string    `json:"run_digest"`
	ModelDigest    string    `json:"model_digest"`
	Passed         bool      `json:"passed"`
	IssuedAt       time.Time `json:"issued_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	KeyID          string    `json:"key_id"`
}

// SignedPersonaEvaluation is an immutable evaluator claim and its authority
// signature. The signature covers every field in Claim.
type SignedPersonaEvaluation struct {
	Claim     PersonaEvaluationClaim
	Signature []byte
}

// SignPersonaEvaluationClaim seals a complete publication claim with the
// evaluator authority key. Production callers keep the private key in the
// composed evaluation issuer; readers use EvaluationSealAuthority.
func SignPersonaEvaluationClaim(privateKey ed25519.PrivateKey, claim PersonaEvaluationClaim) (SignedPersonaEvaluation, error) {
	if len(privateKey) != ed25519.PrivateKeySize || strings.TrimSpace(claim.KeyID) == "" || strings.TrimSpace(claim.TenantID) == "" || strings.TrimSpace(claim.RunID) == "" || strings.TrimSpace(claim.PersonaID) == "" || claim.PersonaVersion <= 0 ||
		!validSHA256(claim.ProfileDigest) || !validSHA256(claim.SuiteDigest) || !validSHA256(claim.RunDigest) || !validSHA256(claim.ModelDigest) || claim.IssuedAt.IsZero() || !claim.ExpiresAt.After(claim.IssuedAt) {
		return SignedPersonaEvaluation{}, fmt.Errorf("%w: incomplete claim or signing key", errInvalidEvaluationEvidence)
	}
	claim.IssuedAt, claim.ExpiresAt = claim.IssuedAt.UTC(), claim.ExpiresAt.UTC()
	encoded, err := json.Marshal(claim)
	if err != nil {
		return SignedPersonaEvaluation{}, fmt.Errorf("%w: encode claim: %v", errInvalidEvaluationEvidence, err)
	}
	return SignedPersonaEvaluation{Claim: claim, Signature: ed25519.Sign(privateKey, evaluationSealMessage(encoded))}, nil
}

// EvaluationSealAuthority verifies durable persona evaluation claims. The
// public keys and tenant mapper are supplied by the trusted composition root.
type EvaluationSealAuthority struct {
	keys       map[string]ed25519.PublicKey
	tenantUUID func(values.TenantId) uuid.UUID
	now        func() time.Time
}

// NewEvaluationSealAuthority constructs an evaluator authority using pinned
// public keys. Private signing keys remain in the evaluation service.
func NewEvaluationSealAuthority(keys map[string]ed25519.PublicKey, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) (*EvaluationSealAuthority, error) {
	if len(keys) == 0 || tenantUUID == nil || now == nil {
		return nil, fmt.Errorf("%w: keys, tenant mapper and clock are required", errInvalidEvaluationEvidence)
	}
	cloned := make(map[string]ed25519.PublicKey, len(keys))
	for id, key := range keys {
		if strings.TrimSpace(id) == "" || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: invalid evaluator key", errInvalidEvaluationEvidence)
		}
		cloned[id] = append(ed25519.PublicKey(nil), key...)
	}
	return &EvaluationSealAuthority{keys: cloned, tenantUUID: tenantUUID, now: now}, nil
}

// RecordPersonaEvaluation validates and inserts a signed claim inside the
// caller-owned tenant transaction. Duplicate run IDs are rejected.
func (a *EvaluationSealAuthority) RecordPersonaEvaluation(ctx context.Context, tx dbport.Tx, evidence SignedPersonaEvaluation) error {
	if a == nil || tx == nil {
		return fmt.Errorf("%w: authority and transaction are required", errInvalidEvaluationEvidence)
	}
	if err := a.verify(evidence, "", "", 0, "", false); err != nil {
		return err
	}
	claim := evidence.Claim
	tenant := values.TenantId(claim.TenantID)
	tenantID := a.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: unknown tenant", errInvalidEvaluationEvidence)
	}
	rows, err := tx.Exec(ctx, `INSERT INTO persona_evaluation_evidence
		(tenant_id,run_id,persona_id,persona_version,profile_digest,suite_digest,run_digest,model_digest,passed,issued_at,expires_at,seal_key_id,seal)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT DO NOTHING`, tenantID,
		claim.RunID, claim.PersonaID, claim.PersonaVersion, claim.ProfileDigest, claim.SuiteDigest, claim.RunDigest,
		claim.ModelDigest, claim.Passed, claim.IssuedAt.UTC(), claim.ExpiresAt.UTC(), claim.KeyID, evidence.Signature)
	if err != nil {
		return fmt.Errorf("agentpersonastore: record persona evaluation: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: run already exists", ErrConflict)
	}
	return nil
}

// ResolvePersonaEvaluation implements EvaluationEvidenceSource. The evidence
// row and persona version are read through the same transaction as Publish.
func (a *EvaluationSealAuthority) ResolvePersonaEvaluation(ctx context.Context, tx dbport.Tx, tenant values.TenantId, runID, personaID string, version int64, profileDigest string) (VerifiedEvaluation, error) {
	if a == nil || tx == nil || strings.TrimSpace(string(tenant)) == "" || strings.TrimSpace(runID) == "" {
		return VerifiedEvaluation{}, fmt.Errorf("%w: tenant, run and transaction are required", errInvalidEvaluationEvidence)
	}
	tenantID := a.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return VerifiedEvaluation{}, fmt.Errorf("%w: unknown tenant", errInvalidEvaluationEvidence)
	}
	var claim PersonaEvaluationClaim
	var signature []byte
	err := tx.QueryRow(ctx, `SELECT run_id,persona_id,persona_version,profile_digest,suite_digest,run_digest,model_digest,passed,issued_at,expires_at,seal_key_id,seal
		FROM persona_evaluation_evidence WHERE tenant_id=$1 AND run_id=$2`, tenantID, runID).Scan(
		&claim.RunID, &claim.PersonaID, &claim.PersonaVersion, &claim.ProfileDigest, &claim.SuiteDigest,
		&claim.RunDigest, &claim.ModelDigest, &claim.Passed, &claim.IssuedAt, &claim.ExpiresAt, &claim.KeyID, &signature)
	if errors.Is(err, dbport.ErrNoRows) {
		return VerifiedEvaluation{}, fmt.Errorf("%w: evaluation run not found", ErrNotFound)
	}
	if err != nil {
		return VerifiedEvaluation{}, fmt.Errorf("agentpersonastore: resolve persona evaluation: %w", err)
	}
	claim.TenantID = string(tenant)
	claim.IssuedAt, claim.ExpiresAt = claim.IssuedAt.UTC(), claim.ExpiresAt.UTC()
	evidence := SignedPersonaEvaluation{Claim: claim, Signature: signature}
	if err := a.verify(evidence, string(tenant), personaID, version, profileDigest, true); err != nil {
		return VerifiedEvaluation{}, err
	}
	return VerifiedEvaluation{RunID: claim.RunID, TenantID: claim.TenantID, PersonaID: claim.PersonaID,
		PersonaVersion: claim.PersonaVersion, ProfileDigest: claim.ProfileDigest, SuiteDigest: claim.SuiteDigest,
		RunDigest: claim.RunDigest, Passed: claim.Passed, Fresh: true}, nil
}

func (a *EvaluationSealAuthority) verify(evidence SignedPersonaEvaluation, tenant, personaID string, version int64, profileDigest string, requirePass bool) error {
	claim := evidence.Claim
	if claim.TenantID == "" || claim.RunID == "" || claim.PersonaID == "" || claim.PersonaVersion <= 0 ||
		!validSHA256(claim.ProfileDigest) || !validSHA256(claim.SuiteDigest) || !validSHA256(claim.RunDigest) ||
		!validSHA256(claim.ModelDigest) || claim.IssuedAt.IsZero() || claim.ExpiresAt.IsZero() || !claim.ExpiresAt.After(claim.IssuedAt) ||
		claim.IssuedAt.After(a.now().UTC()) || !claim.ExpiresAt.After(a.now().UTC()) || strings.TrimSpace(claim.KeyID) == "" {
		return fmt.Errorf("%w: incomplete, not-yet-valid or expired claim", errInvalidEvaluationEvidence)
	}
	if tenant != "" && claim.TenantID != tenant || personaID != "" && claim.PersonaID != personaID ||
		version > 0 && claim.PersonaVersion != version || profileDigest != "" && claim.ProfileDigest != profileDigest {
		return fmt.Errorf("%w: claim does not match publication target", errInvalidEvaluationEvidence)
	}
	key := a.keys[claim.KeyID]
	encoded, err := json.Marshal(claim)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, evaluationSealMessage(encoded), evidence.Signature) {
		return fmt.Errorf("%w: evaluator signature is invalid", errInvalidEvaluationEvidence)
	}
	if requirePass && !claim.Passed {
		return fmt.Errorf("%w: failed evaluation cannot authorize publication", ErrPublicationEvidenceRequired)
	}
	return nil
}

func evaluationSealMessage(claim []byte) []byte {
	return append([]byte("hcm-next-persona-evaluation/v1\x00"), claim...)
}

func validSHA256(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
