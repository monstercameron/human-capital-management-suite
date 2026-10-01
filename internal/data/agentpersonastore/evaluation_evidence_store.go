package agentpersonastore

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaEvaluationRecorder interface {
	RecordPersonaEvaluation(context.Context, dbport.Tx, SignedPersonaEvaluation) error
}

// RecordPersonaEvaluation verifies and durably retains signed evaluation
// evidence in the claim's tenant scope. The claim tenant must be explicit so
// callers cannot route a valid seal into another tenant's row-level scope.
func (s *Store) RecordPersonaEvaluation(ctx context.Context, tenant values.TenantId, evidence SignedPersonaEvaluation) error {
	if s == nil || ctx == nil || tenant.Validate() != nil || strings.TrimSpace(string(tenant)) != string(tenant) || evidence.Claim.TenantID != string(tenant) {
		return fmt.Errorf("%w: invalid evaluation evidence tenant", ErrInvalid)
	}
	recorder, ok := s.evaluations.(personaEvaluationRecorder)
	if !ok || recorder == nil {
		return fmt.Errorf("%w: durable evaluation writer is unavailable", ErrPublicationEvidenceRequired)
	}
	scoped, err := s.ForTenant(ctx, tenant)
	if err != nil {
		return fmt.Errorf("agentpersonastore: scope evaluation evidence: %w", err)
	}
	tx, err := scoped.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := recorder.RecordPersonaEvaluation(ctx, tx, evidence); err != nil {
		return err
	}
	return commit(ctx, tx)
}
