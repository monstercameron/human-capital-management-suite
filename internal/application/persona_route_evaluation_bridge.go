package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaRouteEvaluationBridge adapts the signature-verifying persona
// evaluation authority to the narrow agentstore qualification projection.
type PersonaRouteEvaluationBridge struct {
	authority *agentpersonastore.EvaluationSealAuthority
}

var _ agentstore.PersonaEvaluationEvidenceResolver = (*PersonaRouteEvaluationBridge)(nil)

// NewPersonaRouteEvaluationBridge requires the configured signature verifier;
// a caller-supplied evaluation result cannot be used in its place.
func NewPersonaRouteEvaluationBridge(authority *agentpersonastore.EvaluationSealAuthority) (*PersonaRouteEvaluationBridge, error) {
	if authority == nil {
		return nil, fmt.Errorf("application: persona evaluation seal authority is required")
	}
	return &PersonaRouteEvaluationBridge{authority: authority}, nil
}

// ResolvePersonaEvaluation returns only fields verified by the durable
// evaluation seal authority, translated to the agentstore-owned projection.
func (b *PersonaRouteEvaluationBridge) ResolvePersonaEvaluation(ctx context.Context, tx dbport.Tx, tenant values.TenantId, runID, personaID string, version int64, profileDigest string) (agentstore.VerifiedPersonaRouteEvaluation, error) {
	if b == nil || b.authority == nil {
		return agentstore.VerifiedPersonaRouteEvaluation{}, fmt.Errorf("application: persona evaluation bridge is unavailable")
	}
	verified, err := b.authority.ResolvePersonaEvaluation(ctx, tx, tenant, runID, personaID, version, profileDigest)
	if err != nil {
		return agentstore.VerifiedPersonaRouteEvaluation{}, err
	}
	return agentstore.VerifiedPersonaRouteEvaluation{
		RunID: verified.RunID, TenantID: verified.TenantID, PersonaID: verified.PersonaID,
		PersonaVersion: verified.PersonaVersion, ProfileDigest: verified.ProfileDigest,
		SuiteDigest: verified.SuiteDigest, RunDigest: verified.RunDigest,
		Passed: verified.Passed, Fresh: verified.Fresh,
	}, nil
}
