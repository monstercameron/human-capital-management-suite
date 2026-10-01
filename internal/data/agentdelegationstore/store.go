// Package agentdelegationstore persists agent delegation grants and per-user
// revocation epochs in PostgreSQL. It implements agentdelegation.GrantStore
// through a tenant-scoped adapter (see Store.ForTenant): the port has neither
// a context nor a tenant on Get, so every adapter is bound to one tenant and a
// grant that belongs to another tenant is indistinguishable from a missing
// one. Row level security (tenant_isolation) enforces the same rule in the
// database.
package agentdelegationstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrInvalid reports a misconfigured store or tenant.
var ErrInvalid = errors.New("agentdelegationstore: invalid configuration")

// DB is the transaction opener the store needs.
type DB interface{ dbport.Beginner }

// Store is the tenant-agnostic factory for tenant-scoped grant stores.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

// New builds a Store. tenantUUID maps a tenant key to its canonical UUID and
// must return uuid.Nil for unknown tenants.
func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and canonical tenant mapper are required", ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

// ForTenant returns a GrantStore bound to one tenant. ctx is used for every
// call the adapter makes because the GrantStore port carries none; pass a
// context whose lifetime matches the adapter's.
func (s *Store) ForTenant(ctx context.Context, tenant values.TenantId) (agentdelegation.GrantStore, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil {
		return nil, fmt.Errorf("%w: nil store or context", ErrInvalid)
	}
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: unknown tenant %q", ErrInvalid, tenant)
	}
	return &tenantStore{db: s.db, ctx: ctx, tenant: tenant, tenantID: id}, nil
}

type tenantStore struct {
	db       DB
	ctx      context.Context
	tenant   values.TenantId
	tenantID uuid.UUID
}

var _ agentdelegation.GrantStore = (*tenantStore)(nil)

func (s *tenantStore) begin() (dbport.Tx, error) {
	tx, err := s.db.Begin(s.ctx)
	if err != nil {
		return nil, fmt.Errorf("agentdelegationstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(s.ctx, tx, s.tenantID); err != nil {
		_ = tx.Rollback(s.ctx)
		return nil, err
	}
	return tx, nil
}

// Save inserts one grant. Semantics mirror MemoryGrantStore: structural
// validation, refusal to overwrite an existing grant, and refusal when the
// grant was minted under a stale revocation epoch.
func (s *tenantStore) Save(g agentdelegation.Grant) error {
	if err := agentdelegation.ValidateGrant(g); err != nil {
		return err
	}
	if g.Tenant != s.tenant {
		return fmt.Errorf("%w: grant tenant does not match the store tenant", agentdelegation.ErrInvalidGrant)
	}
	if err := reconcileSkillAuthorities(&g); err != nil {
		return err
	}
	scopes, err := json.Marshal(g.SkillScopes)
	if err != nil {
		return fmt.Errorf("%w: encoding skill scopes: %v", agentdelegation.ErrInvalidGrant, err)
	}
	authority, err := json.Marshal(g.Authority)
	if err != nil {
		return fmt.Errorf("%w: encoding authority: %v", agentdelegation.ErrInvalidGrant, err)
	}
	parentID, parentActor, err := encodeGrantLineage(g)
	if err != nil {
		return err
	}
	tx, err := s.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback(s.ctx)
	var exists bool
	if err := tx.QueryRow(s.ctx, `SELECT EXISTS (SELECT 1 FROM agent_delegation_grant WHERE tenant_id=$1 AND grant_id=$2)`, s.tenantID, g.GrantID).Scan(&exists); err != nil {
		return fmt.Errorf("agentdelegationstore: check grant: %w", err)
	}
	if exists {
		return fmt.Errorf("%w: grant %q already exists", agentdelegation.ErrInvalidGrant, g.GrantID)
	}
	// Lock the epoch row shared so a concurrent bump cannot commit between
	// this comparison and the insert.
	current, err := lockEpoch(s.ctx, tx, s.tenantID, g.UserID)
	if err != nil {
		return err
	}
	if g.RevocationEpoch != current {
		return fmt.Errorf("%w: stale revocation epoch", agentdelegation.ErrGrantRevoked)
	}
	var revokedAt, revokedReason any
	if g.Revoked {
		revokedAt, revokedReason = time.Now().UTC(), "saved revoked"
	}
	affected, err := tx.Exec(s.ctx, `INSERT INTO agent_delegation_grant
		(tenant_id,grant_id,user_id,agent_version,target_agent_id,installation_id,task_id,plan_skill_set_digest,purpose,organization_scope_id,
		 skills,skill_scopes,authority,not_before,expires_at,revocation_epoch,revoked,revoked_reason,revoked_at,parent_grant_id,parent_actor,common_admission_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14,$15,$16,$17,$18,$19,$20,$21::jsonb,$22)
		ON CONFLICT (tenant_id, grant_id) DO NOTHING`,
		s.tenantID, g.GrantID, g.UserID, g.AgentVersion, g.TargetAgentID, g.InstallationID, g.TaskID, g.PlanSkillSetDigest, g.Purpose, g.OrganizationScopeID,
		g.Skills, string(scopes), string(authority), g.NotBefore.UTC(), g.ExpiresAt.UTC(), int64(g.RevocationEpoch), g.Revoked, revokedReason, revokedAt, parentID, parentActor, nullableAdmissionID(g.CommonAdmissionID))
	if err != nil {
		return fmt.Errorf("agentdelegationstore: insert grant: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: grant %q already exists", agentdelegation.ErrInvalidGrant, g.GrantID)
	}
	if err := tx.Commit(s.ctx); err != nil {
		return fmt.Errorf("agentdelegationstore: commit grant: %w", err)
	}
	return nil
}

// lockEpoch returns the user's current epoch (creating the initial row of one)
// and holds a shared row lock until the transaction ends.
func lockEpoch(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, userID string) (uint64, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO agent_delegation_epoch (tenant_id,user_id,epoch) VALUES ($1,$2,1) ON CONFLICT (tenant_id,user_id) DO NOTHING`, tenantID, userID); err != nil {
		return 0, fmt.Errorf("agentdelegationstore: initialise epoch: %w", err)
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT epoch FROM agent_delegation_epoch WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenantID, userID).Scan(&epoch); err != nil {
		return 0, fmt.Errorf("agentdelegationstore: read epoch: %w", err)
	}
	return uint64(epoch), nil
}

// Get returns the grant only when it belongs to this adapter's tenant.
func (s *tenantStore) Get(grantID string) (agentdelegation.Grant, error) {
	tx, err := s.begin()
	if err != nil {
		return agentdelegation.Grant{}, err
	}
	defer tx.Rollback(s.ctx)
	var (
		g                  agentdelegation.Grant
		scopes, authorText string
		epoch              int64
		parentID           *string
		parentActor        []byte
		admissionID        *string
	)
	err = tx.QueryRow(s.ctx, `SELECT grant_id,user_id,agent_version,target_agent_id,installation_id,task_id,plan_skill_set_digest,purpose,organization_scope_id,
		skills,skill_scopes::text,authority::text,not_before,expires_at,revocation_epoch,revoked,parent_grant_id,parent_actor,common_admission_id
		FROM agent_delegation_grant WHERE tenant_id=$1 AND grant_id=$2`, s.tenantID, grantID).
		Scan(&g.GrantID, &g.UserID, &g.AgentVersion, &g.TargetAgentID, &g.InstallationID, &g.TaskID, &g.PlanSkillSetDigest, &g.Purpose, &g.OrganizationScopeID,
			&g.Skills, &scopes, &authorText, &g.NotBefore, &g.ExpiresAt, &epoch, &g.Revoked, &parentID, &parentActor, &admissionID)
	if errors.Is(err, dbport.ErrNoRows) {
		return agentdelegation.Grant{}, fmt.Errorf("%w: %s", agentdelegation.ErrGrantNotFound, grantID)
	}
	if err != nil {
		return agentdelegation.Grant{}, fmt.Errorf("agentdelegationstore: read grant: %w", err)
	}
	if err := json.Unmarshal([]byte(scopes), &g.SkillScopes); err != nil {
		return agentdelegation.Grant{}, fmt.Errorf("agentdelegationstore: decode skill scopes: %w", err)
	}
	var authority trust.DelegationGrant
	if err := json.Unmarshal([]byte(authorText), &authority); err != nil {
		return agentdelegation.Grant{}, fmt.Errorf("agentdelegationstore: decode authority: %w", err)
	}
	g.Authority = authority
	g.SkillAuthorities = trust.CloneSkillAuthorities(authority.SkillAuthorities)
	g.Tenant = s.tenant
	if admissionID != nil {
		g.CommonAdmissionID = *admissionID
	}
	g.RevocationEpoch = uint64(epoch)
	g.NotBefore, g.ExpiresAt = g.NotBefore.UTC(), g.ExpiresAt.UTC()
	if err := decodeGrantLineage(parentID, parentActor, &g); err != nil {
		return agentdelegation.Grant{}, fmt.Errorf("agentdelegationstore: invalid persisted lineage: %w", err)
	}
	if err := agentdelegation.ValidateGrant(g); err != nil {
		return agentdelegation.Grant{}, fmt.Errorf("agentdelegationstore: invalid persisted grant: %w", err)
	}
	return g, nil
}

// reconcileSkillAuthorities keeps the grant's compatibility field and the
// trust-layer authority document at one canonical value. The document is the
// durable representation, so a caller may omit the compatibility field when
// writing an older-shaped grant; conflicting values are rejected rather than
// allowing a replay to widen one skill's authority after restoration.
func reconcileSkillAuthorities(g *agentdelegation.Grant) error {
	if g == nil {
		return fmt.Errorf("%w: nil grant", agentdelegation.ErrInvalidGrant)
	}
	if g.SkillAuthorities == nil {
		g.SkillAuthorities = trust.CloneSkillAuthorities(g.Authority.SkillAuthorities)
		return nil
	}
	if g.Authority.SkillAuthorities == nil {
		g.Authority.SkillAuthorities = trust.CloneSkillAuthorities(g.SkillAuthorities)
		return nil
	}
	if !sameSkillAuthorities(g.SkillAuthorities, g.Authority.SkillAuthorities) {
		return fmt.Errorf("%w: top-level and embedded skill authorities differ", agentdelegation.ErrInvalidGrant)
	}
	return nil
}

func sameSkillAuthorities(left, right trust.SkillAuthorities) bool {
	if len(left) != len(right) {
		return false
	}
	for skill, want := range left {
		got, ok := right[skill]
		if !ok || !sameStrings(want.Capabilities, got.Capabilities) || !sameStrings(want.Resources, got.Resources) || !sameStrings(want.Fields, got.Fields) || !sameStrings(want.Purposes, got.Purposes) {
			return false
		}
	}
	return true
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// Revoke durably marks one grant revoked. It is idempotent: the first reason
// and time are kept.
func (s *tenantStore) Revoke(grantID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: revocation reason is required", agentdelegation.ErrInvalidRequest)
	}
	tx, err := s.begin()
	if err != nil {
		return err
	}
	defer tx.Rollback(s.ctx)
	affected, err := tx.Exec(s.ctx, `UPDATE agent_delegation_grant
		SET revoked=true, revoked_reason=$3, revoked_at=now(), authority=jsonb_set(authority,'{Revoked}','true')
		WHERE tenant_id=$1 AND grant_id=$2 AND NOT revoked`, s.tenantID, grantID, reason)
	if err != nil {
		return fmt.Errorf("agentdelegationstore: revoke grant: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := tx.QueryRow(s.ctx, `SELECT EXISTS (SELECT 1 FROM agent_delegation_grant WHERE tenant_id=$1 AND grant_id=$2)`, s.tenantID, grantID).Scan(&exists); err != nil {
			return fmt.Errorf("agentdelegationstore: check grant: %w", err)
		}
		if !exists {
			return fmt.Errorf("%w: %s", agentdelegation.ErrGrantNotFound, grantID)
		}
	}
	if err := tx.Commit(s.ctx); err != nil {
		return fmt.Errorf("agentdelegationstore: commit revoke: %w", err)
	}
	return nil
}

// CurrentRevocationEpoch returns the user's epoch, or one when none has been
// recorded. The port cannot return an error, so a database failure or a tenant
// mismatch fails closed with the maximum epoch: any grant compares as revoked
// and no new grant can be saved under it.
func (s *tenantStore) CurrentRevocationEpoch(tenant values.TenantId, userID string) uint64 {
	if tenant != s.tenant || strings.TrimSpace(userID) == "" {
		return math.MaxUint64
	}
	tx, err := s.begin()
	if err != nil {
		return math.MaxUint64
	}
	defer tx.Rollback(s.ctx)
	var epoch int64
	err = tx.QueryRow(s.ctx, `SELECT epoch FROM agent_delegation_epoch WHERE tenant_id=$1 AND user_id=$2`, s.tenantID, userID).Scan(&epoch)
	if errors.Is(err, dbport.ErrNoRows) {
		return 1
	}
	if err != nil {
		return math.MaxUint64
	}
	return uint64(epoch)
}

// BumpRevocationEpoch atomically increments the user's epoch with a single
// upsert and returns the new value.
func (s *tenantStore) BumpRevocationEpoch(tenant values.TenantId, userID, reason string) (uint64, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(reason) == "" {
		return 0, fmt.Errorf("%w: epoch subject and reason are required", agentdelegation.ErrInvalidRequest)
	}
	if err := tenant.Validate(); err != nil {
		return 0, fmt.Errorf("%w: %v", agentdelegation.ErrInvalidRequest, err)
	}
	if tenant != s.tenant {
		return 0, fmt.Errorf("%w: tenant does not match the store tenant", agentdelegation.ErrInvalidRequest)
	}
	tx, err := s.begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(s.ctx)
	var epoch int64
	if err := tx.QueryRow(s.ctx, `INSERT INTO agent_delegation_epoch (tenant_id,user_id,epoch,last_reason) VALUES ($1,$2,2,$3)
		ON CONFLICT (tenant_id,user_id) DO UPDATE SET epoch=agent_delegation_epoch.epoch+1, last_reason=EXCLUDED.last_reason, updated_at=now()
		RETURNING epoch`, s.tenantID, userID, reason).Scan(&epoch); err != nil {
		return 0, fmt.Errorf("agentdelegationstore: bump epoch: %w", err)
	}
	if err := tx.Commit(s.ctx); err != nil {
		return 0, fmt.Errorf("agentdelegationstore: commit epoch: %w", err)
	}
	return uint64(epoch), nil
}
