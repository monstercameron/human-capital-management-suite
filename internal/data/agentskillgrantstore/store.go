package agentskillgrantstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrInvalid = errors.New("agentskillgrantstore: invalid request")

// DB opens transactions used to set and enforce tenant scope for each query.
type DB interface{ dbport.Beginner }

// Store creates tenant-bound current-grant readers.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

// TenantStore reads grants for one tenant only.
type TenantStore struct {
	db       DB
	tenant   values.TenantId
	tenantID uuid.UUID
}

// New constructs a store using the application's tenant identifier mapping.
func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and tenant mapper are required", ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

// Scoped returns a reader whose SQL and RLS scope are fixed to tenant.
func (s *Store) Scoped(tenant values.TenantId) (*TenantStore, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || strings.TrimSpace(string(tenant)) == "" || strings.TrimSpace(string(tenant)) != string(tenant) {
		return nil, fmt.Errorf("%w: invalid store or tenant", ErrInvalid)
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: unknown tenant", ErrInvalid)
	}
	return &TenantStore{db: s.db, tenant: tenant, tenantID: id}, nil
}

var _ agentgate.GrantProvider = (*TenantStore)(nil)

// Grants returns only unrevoked grants currently in force for this tenant and
// the exact immutable skill version. ConsentRequired is metadata for the gate;
// consent lifecycle decisions are evaluated separately at call time.
func (s *TenantStore) Grants(ctx context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	if s == nil || s.db == nil || ctx == nil || tenant != s.tenant || tenant.Validate() != nil || strings.TrimSpace(key.ID) == "" || key.ID != strings.TrimSpace(key.ID) || key.Version == 0 {
		return nil, fmt.Errorf("%w: tenant, context, and exact skill version are required", ErrInvalid)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("agentskillgrantstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, s.tenantID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT grant_id, roles, population, organization_scopes, purposes, consent_required
		FROM agent_skill_grant
		WHERE tenant_id=$1 AND skill_id=$2 AND skill_version=$3
		  AND not_before <= CURRENT_TIMESTAMP
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		  AND revoked_at IS NULL
		ORDER BY grant_id`, s.tenantID, key.ID, int64(key.Version))
	if err != nil {
		return nil, fmt.Errorf("agentskillgrantstore: query grants: %w", err)
	}
	defer rows.Close()
	var grants []agentgate.SkillGrant
	for rows.Next() {
		grant := agentgate.SkillGrant{Tenant: tenant, Skill: key}
		if err := rows.Scan(&grant.ID, &grant.Roles, &grant.Population, &grant.OrganizationScopes, &grant.Purposes, &grant.ConsentRequired); err != nil {
			return nil, fmt.Errorf("agentskillgrantstore: scan grant: %w", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentskillgrantstore: read grants: %w", err)
	}
	return grants, nil
}
