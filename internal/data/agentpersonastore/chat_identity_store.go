package agentpersonastore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaChatIdentity is the exact tenant-scoped binding between a chat
// Agent.ID and a persona. It contains no display-derived identity fields.
type PersonaChatIdentity struct {
	TenantID     values.TenantId
	AgentID      string
	PersonaID    string
	Active       bool
	RegisteredAt time.Time
	RevokedAt    *time.Time
}

const maxPersonaChatIdentityList = 100

// ListPersonaChatIdentities returns the bounded active canonical identities
// for tenant. The tenant is resolved through Scoped, so the query remains
// protected by the identity store's tenant transaction and RLS policy.
func (s *Store) ListPersonaChatIdentities(ctx context.Context, tenant string) ([]PersonaChatIdentity, error) {
	if s == nil || ctx == nil || strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("%w: tenant and context are required", ErrInvalid)
	}
	scoped, err := s.Scoped(values.TenantId(strings.TrimSpace(tenant)))
	if err != nil {
		return nil, err
	}
	return scoped.ListActivePersonaChatIdentities(ctx, maxPersonaChatIdentityList)
}

// ListActivePersonaChatIdentities returns at most limit active identities in
// canonical Agent.ID order. Revoked rows are intentionally excluded.
func (s *TenantStore) ListActivePersonaChatIdentities(ctx context.Context, limit int) ([]PersonaChatIdentity, error) {
	if s == nil || ctx == nil || limit <= 0 || limit > maxPersonaChatIdentityList {
		return nil, fmt.Errorf("%w: list limit must be between 1 and %d", ErrInvalid, maxPersonaChatIdentityList)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT agent_id,persona_id,registered_at
		FROM persona_chat_identities
		WHERE tenant_id=$1 AND state='ACTIVE'
		ORDER BY agent_id COLLATE "C"
		LIMIT $2`, s.tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: list active persona chat identities: %w", err)
	}
	defer rows.Close()
	out := make([]PersonaChatIdentity, 0, limit)
	for rows.Next() {
		var item PersonaChatIdentity
		if err := rows.Scan(&item.AgentID, &item.PersonaID, &item.RegisteredAt); err != nil {
			return nil, fmt.Errorf("agentpersonastore: scan persona chat identity: %w", err)
		}
		item.TenantID, item.Active = s.tenant, true
		item.RegisteredAt = item.RegisteredAt.UTC()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentpersonastore: read persona chat identities: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterPersonaChatIdentity permanently reserves agentID and personaID in
// the tenant namespace and creates their active canonical binding.
func (s *TenantStore) RegisterPersonaChatIdentity(ctx context.Context, agentID, personaID string, registeredAt time.Time) error {
	if s == nil || strings.TrimSpace(agentID) == "" || strings.TrimSpace(personaID) == "" || registeredAt.IsZero() {
		return fmt.Errorf("%w: agent id, persona id and registration time are required", ErrInvalid)
	}
	agentID, personaID = strings.TrimSpace(agentID), strings.TrimSpace(personaID)
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `INSERT INTO persona_chat_identities
		(tenant_id,agent_id,persona_id,state,registered_at)
		VALUES ($1,$2,$3,'ACTIVE',$4) ON CONFLICT DO NOTHING`,
		s.tenantID, agentID, personaID, registeredAt.UTC())
	if err != nil {
		return fmt.Errorf("agentpersonastore: register persona chat identity: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: chat identity or persona is already registered", ErrConflict)
	}
	return commit(ctx, tx)
}

// LookupPersonaChatIdentity returns the exact registered binding for agentID.
// It fails closed for an unknown ID and never searches by persona or display.
func (s *TenantStore) LookupPersonaChatIdentity(ctx context.Context, agentID string) (PersonaChatIdentity, error) {
	if s == nil || strings.TrimSpace(agentID) == "" {
		return PersonaChatIdentity{}, fmt.Errorf("%w: agent id is required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaChatIdentity{}, err
	}
	defer tx.Rollback(ctx)
	var out PersonaChatIdentity
	var state string
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT agent_id,persona_id,state,registered_at,revoked_at
		FROM persona_chat_identities WHERE tenant_id=$1 AND agent_id=$2`,
		s.tenantID, strings.TrimSpace(agentID)).Scan(&out.AgentID, &out.PersonaID, &state, &out.RegisteredAt, &revokedAt)
	if err == dbport.ErrNoRows {
		return PersonaChatIdentity{}, fmt.Errorf("%w: chat agent identity", ErrNotFound)
	}
	if err != nil {
		return PersonaChatIdentity{}, fmt.Errorf("agentpersonastore: lookup persona chat identity: %w", err)
	}
	out.TenantID, out.Active, out.RevokedAt = s.tenant, state == "ACTIVE", revokedAt
	out.RegisteredAt = out.RegisteredAt.UTC()
	if out.RevokedAt != nil {
		at := out.RevokedAt.UTC()
		out.RevokedAt = &at
	}
	if err := commit(ctx, tx); err != nil {
		return PersonaChatIdentity{}, err
	}
	return out, nil
}

// RevokePersonaChatIdentity irreversibly revokes the exact registered agent
// identity. The row remains reserved so the binding cannot be rebound.
func (s *TenantStore) RevokePersonaChatIdentity(ctx context.Context, agentID string, revokedAt time.Time) error {
	if s == nil || strings.TrimSpace(agentID) == "" || revokedAt.IsZero() {
		return fmt.Errorf("%w: agent id and revocation time are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `UPDATE persona_chat_identities
		SET state='REVOKED', revoked_at=$3
		WHERE tenant_id=$1 AND agent_id=$2 AND state='ACTIVE'`,
		s.tenantID, strings.TrimSpace(agentID), revokedAt.UTC())
	if err != nil {
		return fmt.Errorf("agentpersonastore: revoke persona chat identity: %w", err)
	}
	if n == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM persona_chat_identities WHERE tenant_id=$1 AND agent_id=$2)`, s.tenantID, strings.TrimSpace(agentID)).Scan(&exists); err != nil {
			return fmt.Errorf("agentpersonastore: check persona chat identity: %w", err)
		}
		if exists {
			return fmt.Errorf("%w: chat identity is already revoked", ErrConflict)
		}
		return fmt.Errorf("%w: chat identity", ErrNotFound)
	}
	return commit(ctx, tx)
}
