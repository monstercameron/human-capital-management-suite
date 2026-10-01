package agentstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// VerifiedPersonaRouteEvaluation is the narrow verified-claim projection
// agentstore needs. Its application-layer adapter must source it from a
// signature-verifying evaluator authority.
type VerifiedPersonaRouteEvaluation struct {
	RunID, TenantID, PersonaID string
	PersonaVersion             int64
	ProfileDigest              string
	SuiteDigest, RunDigest     string
	Passed, Fresh              bool
}

// PersonaEvaluationEvidenceResolver verifies a durable evaluator claim in the
// supplied transaction. Production composition should provide
// an application adapter backed by agentpersonastore.EvaluationSealAuthority.
type PersonaEvaluationEvidenceResolver interface {
	ResolvePersonaEvaluation(context.Context, dbport.Tx, values.TenantId, string, string, int64, string) (VerifiedPersonaRouteEvaluation, error)
}

// PersonaRouteQualificationEvidenceAuthority adapts signed persona evaluation
// records into the route publisher contract. The model digest and expiry are
// read from the same tenant-scoped immutable row whose signature was verified.
type PersonaRouteQualificationEvidenceAuthority struct {
	evidence   PersonaEvaluationEvidenceResolver
	tenantUUID func(values.TenantId) uuid.UUID
}

var _ PersonaRouteQualificationAuthority = (*PersonaRouteQualificationEvidenceAuthority)(nil)

// NewPersonaRouteQualificationEvidenceAuthority requires the signed-evidence
// resolver and tenant mapper used by the route publication composition root.
func NewPersonaRouteQualificationEvidenceAuthority(evidence PersonaEvaluationEvidenceResolver, tenantUUID func(values.TenantId) uuid.UUID) (*PersonaRouteQualificationEvidenceAuthority, error) {
	if evidence == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: signed evaluation resolver and tenant mapper are required", ErrInvalidConfig)
	}
	return &PersonaRouteQualificationEvidenceAuthority{evidence: evidence, tenantUUID: tenantUUID}, nil
}

// ResolvePersonaRouteQualification verifies the signed pass claim, then reads
// its model digest and expiry through the same transaction. The publisher
// separately binds the exact route policy digest and payload model digest.
func (a *PersonaRouteQualificationEvidenceAuthority) ResolvePersonaRouteQualification(ctx context.Context, tx dbport.Tx, tenant values.TenantId, runID, personaID string, version int64, profileDigest string) (PersonaRouteQualification, error) {
	if a == nil || a.evidence == nil || a.tenantUUID == nil || ctx == nil || tx == nil ||
		strings.TrimSpace(string(tenant)) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(personaID) == "" || version <= 0 || !validStoreDigest(profileDigest) {
		return PersonaRouteQualification{}, fmt.Errorf("%w: incomplete qualification lookup", ErrPersonaRouteQualificationRequired)
	}
	if err := ctx.Err(); err != nil {
		return PersonaRouteQualification{}, err
	}
	verified, err := a.evidence.ResolvePersonaEvaluation(ctx, tx, tenant, runID, personaID, version, profileDigest)
	if err != nil {
		return PersonaRouteQualification{}, fmt.Errorf("%w: verify evaluator claim: %v", ErrPersonaRouteQualificationRequired, err)
	}
	if verified.RunID != runID || verified.TenantID != string(tenant) || verified.PersonaID != personaID ||
		verified.PersonaVersion != version || verified.ProfileDigest != profileDigest || !verified.Passed || !verified.Fresh ||
		!validStoreDigest(verified.SuiteDigest) || !validStoreDigest(verified.RunDigest) {
		return PersonaRouteQualification{}, ErrPersonaRouteQualificationRequired
	}
	tenantID := a.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return PersonaRouteQualification{}, fmt.Errorf("%w: unknown tenant", ErrPersonaRouteQualificationRequired)
	}
	var modelDigest string
	var expiry time.Time
	if err := tx.QueryRow(ctx, `SELECT model_digest,expires_at FROM persona_evaluation_evidence WHERE tenant_id=$1 AND run_id=$2`, tenantID, runID).Scan(&modelDigest, &expiry); err != nil {
		return PersonaRouteQualification{}, fmt.Errorf("%w: read verified evaluation details: %v", ErrPersonaRouteQualificationRequired, err)
	}
	if !validStoreDigest(modelDigest) || expiry.IsZero() {
		return PersonaRouteQualification{}, ErrPersonaRouteQualificationRequired
	}
	return PersonaRouteQualification{RunID: verified.RunID, TenantID: verified.TenantID, PersonaID: verified.PersonaID,
		PersonaVersion: verified.PersonaVersion, ProfileDigest: verified.ProfileDigest, ModelDigest: modelDigest,
		SuiteDigest: verified.SuiteDigest, RunDigest: verified.RunDigest, Passed: verified.Passed, Fresh: verified.Fresh,
		ExpiresAt: expiry.UTC()}, nil
}
