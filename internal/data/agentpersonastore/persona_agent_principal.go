package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PersonaAgentPrincipalBinding pins one persona version to a provisioned
// principal in the core trust store. The principal is validated by its owner.
type PersonaAgentPrincipalBinding struct {
	TenantID       string
	PersonaID      string
	PersonaVersion int64
	PrincipalID    uuid.UUID
	ProvisionedAt  time.Time
}

// RegisterPersonaAgentPrincipal records the explicit trust-principal binding
// for an immutable persona version. Callers must first provision and validate
// the principal through the core trust authority.
func (s *TenantStore) RegisterPersonaAgentPrincipal(ctx context.Context, personaID string, version int64, principalID uuid.UUID, provisionedAt time.Time) error {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" || version <= 0 || principalID == uuid.Nil || provisionedAt.IsZero() {
		return fmt.Errorf("%w: exact persona version, principal and provisioning time are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `INSERT INTO persona_agent_principal_binding
		(tenant_id, persona_id, persona_version, principal_id, provisioned_at)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, s.tenantID, strings.TrimSpace(personaID), version, principalID, provisionedAt.UTC())
	if err != nil {
		return fmt.Errorf("agentpersonastore: register persona agent principal: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: exact persona version already has an agent principal", ErrConflict)
	}
	return commit(ctx, tx)
}

// ResolvePersonaAgentPrincipal returns the exact tenant/persona/version
// binding. It never selects a latest version or synthesizes a principal ID.
func (s *TenantStore) ResolvePersonaAgentPrincipal(ctx context.Context, personaID string, version int64) (PersonaAgentPrincipalBinding, error) {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" || version <= 0 {
		return PersonaAgentPrincipalBinding{}, fmt.Errorf("%w: exact persona version is required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaAgentPrincipalBinding{}, err
	}
	defer tx.Rollback(ctx)
	var binding PersonaAgentPrincipalBinding
	err = tx.QueryRow(ctx, `SELECT persona_id, persona_version, principal_id, provisioned_at
		FROM persona_agent_principal_binding
		WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3`, s.tenantID, strings.TrimSpace(personaID), version).
		Scan(&binding.PersonaID, &binding.PersonaVersion, &binding.PrincipalID, &binding.ProvisionedAt)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return PersonaAgentPrincipalBinding{}, fmt.Errorf("%w: persona agent principal binding", ErrNotFound)
		}
		return PersonaAgentPrincipalBinding{}, fmt.Errorf("agentpersonastore: resolve persona agent principal: %w", err)
	}
	binding.TenantID = string(s.tenant)
	binding.ProvisionedAt = binding.ProvisionedAt.UTC()
	if binding.PrincipalID == uuid.Nil || binding.PersonaID != strings.TrimSpace(personaID) || binding.PersonaVersion != version {
		return PersonaAgentPrincipalBinding{}, fmt.Errorf("%w: stored principal binding is incomplete", ErrInvalid)
	}
	if err := commit(ctx, tx); err != nil {
		return PersonaAgentPrincipalBinding{}, err
	}
	return binding, nil
}
