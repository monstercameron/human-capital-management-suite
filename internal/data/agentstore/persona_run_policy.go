package agentstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	// ErrPersonaRunPolicyNotFound indicates no policy is active for this tenant and legal entity.
	ErrPersonaRunPolicyNotFound = errors.New("agentstore: current persona run policy not found")
	// ErrPersonaRunPolicyAmbiguous indicates overlapping active policy periods require repair.
	ErrPersonaRunPolicyAmbiguous = errors.New("agentstore: multiple current persona run policies")
)

// PersonaRunPolicy is a tenant-owned, legal-entity-scoped immutable policy
// revision that bounds an individual model run.
type PersonaRunPolicy struct {
	TenantID        uuid.UUID
	LegalEntityID   string
	Revision        int64
	EffectiveFrom   time.Time
	EffectiveUntil  *time.Time
	MaxCostMicros   int64
	MaxInputTokens  int64
	MaxOutputTokens int64
	MaxRunDuration  time.Duration
}

// CurrentPersonaRunPolicy returns the unique policy active at effectiveAt.
// Missing and overlapping policies fail closed instead of selecting a default.
func (s *Store) CurrentPersonaRunPolicy(ctx context.Context, tenantID uuid.UUID, legalEntityID string, effectiveAt time.Time) (PersonaRunPolicy, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(legalEntityID) == "" || strings.TrimSpace(legalEntityID) != legalEntityID || effectiveAt.IsZero() {
		return PersonaRunPolicy{}, fmt.Errorf("%w: tenant, legal entity and effective time are required", ErrInvalidConfig)
	}
	var policy PersonaRunPolicy
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision,effective_from,effective_until,
			max_cost_micros,max_input_tokens,max_output_tokens,max_run_duration_ms
			FROM persona_run_policy
			WHERE tenant_id=$1 AND legal_entity_id=$2
			  AND effective_from <= $3 AND (effective_until IS NULL OR effective_until > $3)
			ORDER BY effective_from DESC LIMIT 2`, tenantID, legalEntityID, effectiveAt.UTC())
		if err != nil {
			return fmt.Errorf("agentstore: query current persona run policy: %w", err)
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return fmt.Errorf("agentstore: read current persona run policy: %w", err)
			}
			return ErrPersonaRunPolicyNotFound
		}
		var until *time.Time
		var durationMillis int64
		if err := rows.Scan(&policy.Revision, &policy.EffectiveFrom, &until, &policy.MaxCostMicros,
			&policy.MaxInputTokens, &policy.MaxOutputTokens, &durationMillis); err != nil {
			return fmt.Errorf("agentstore: scan current persona run policy: %w", err)
		}
		if rows.Next() {
			return ErrPersonaRunPolicyAmbiguous
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("agentstore: read current persona run policy: %w", err)
		}
		if durationMillis > math.MaxInt64/int64(time.Millisecond) {
			return fmt.Errorf("%w: run duration overflows", ErrInvalidConfig)
		}
		policy.TenantID = tenantID
		policy.LegalEntityID = legalEntityID
		policy.EffectiveFrom = policy.EffectiveFrom.UTC()
		if until != nil {
			value := until.UTC()
			policy.EffectiveUntil = &value
		}
		policy.MaxRunDuration = time.Duration(durationMillis) * time.Millisecond
		return nil
	})
	if err != nil {
		return PersonaRunPolicy{}, err
	}
	return policy, nil
}
