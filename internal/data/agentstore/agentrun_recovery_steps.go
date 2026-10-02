package agentstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PersonaRunModelCallMayHaveStarted reports whether any model step of the run
// was ever begun under its security lease. A step is recorded before the model
// is called, so a worker that died inside the call still leaves this evidence,
// and a model call that may have been paid for is never repeated on its own.
func (s *Store) PersonaRunModelCallMayHaveStarted(ctx context.Context, tenantID uuid.UUID, runID string) (bool, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(runID) == "" {
		return false, personaSecurityLeaseInvalidHere()
	}
	started := false
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM persona_security_lease l JOIN persona_security_step st
			  ON st.tenant_id=l.tenant_id AND st.lease_id=l.lease_id
			WHERE l.tenant_id=$1 AND l.run_id=$2 AND st.step_id LIKE 'persona-model-%')`, tenantID, runID).Scan(&started)
	})
	return started, err
}

// RecoverPersonaRunSteps marks the steps a dead worker left STARTED as
// INTERRUPTED so a worker taking the run over is not refused by the fence. It
// does nothing for a run that has no security lease yet.
func (s *Store) RecoverPersonaRunSteps(ctx context.Context, tenantID uuid.UUID, runID string, at time.Time) (int64, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(runID) == "" || at.IsZero() {
		return 0, personaSecurityLeaseInvalidHere()
	}
	var leaseID string
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT lease_id FROM persona_security_lease WHERE tenant_id=$1 AND run_id=$2`, tenantID, runID).Scan(&leaseID)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return s.RecoverPersonaSecuritySteps(ctx, tenantID, leaseID, at)
}
