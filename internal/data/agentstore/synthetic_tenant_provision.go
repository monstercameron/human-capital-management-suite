package agentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// SyntheticTenantProvisionerRole is the non-login evaluator-only issuer role.
const SyntheticTenantProvisionerRole = "hcmnext_agent_eval_provisioner"

var (
	ErrSyntheticProvisionInvalid = errors.New("agentstore: invalid synthetic tenant provision")
	ErrSyntheticProvisionAbsent  = errors.New("agentstore: synthetic tenant provision not found")
	ErrSyntheticProvisionRevoked = errors.New("agentstore: synthetic tenant provision already revoked")
)

// SyntheticTenantProvisionRecord is the durable, tenant-scoped isolation
// marker. Runtime objects are composed separately from these persisted IDs.
type SyntheticTenantProvisionRecord struct {
	TenantID         uuid.UUID
	SuiteTenantID    string
	ProvisionID      string
	MarkerID         string
	Purpose          string
	Status           string
	ExpiresAt        time.Time
	GrantStoreID     string
	TaskStoreID      string
	BudgetLedgerID   string
	AuditStoreID     string
	ToolOwnerID      string
	ToolOwnerProfile string
	IssuedAt         time.Time
	RevokedAt        *time.Time
	RevokeReason     string
}

// SyntheticTenantProvisionIssue is accepted only by the dedicated evaluator
// issuer pool. It contains identifiers for independently composed runtime
// stores and the fixture-only tool owner.
type SyntheticTenantProvisionIssue struct {
	Record  SyntheticTenantProvisionRecord
	ActorID string
}

// SyntheticTenantProvisionReader loads one provision under tenant RLS.
// agentstore.Store implements this with its read-only application role.
type SyntheticTenantProvisionReader interface {
	SyntheticTenantProvision(context.Context, uuid.UUID, string) (SyntheticTenantProvisionRecord, error)
}

// SyntheticTenantProvisionIssuerConfig isolates the evaluator issuer login
// from the tenant-serving agent, core, chat and document credentials.
type SyntheticTenantProvisionIssuerConfig struct {
	DSN, AgentDSN, CoreDSN, ChatDSN, DocumentDSN string
	MaxConns, MinConns                           int32
}

// SyntheticTenantProvisionIssuerStore owns the restricted provision writer
// pool and is intended only for the evaluator provisioning service.
type SyntheticTenantProvisionIssuerStore struct{ pool *pgxadapter.Pool }

// NewSyntheticTenantProvisionIssuerStore opens a separately credentialed
// connection that assumes the evaluator-only provisioner role.
func NewSyntheticTenantProvisionIssuerStore(ctx context.Context, cfg SyntheticTenantProvisionIssuerConfig) (*SyntheticTenantProvisionIssuerStore, error) {
	if ctx == nil || strings.TrimSpace(cfg.DSN) == "" || strings.TrimSpace(cfg.AgentDSN) == "" || strings.TrimSpace(cfg.CoreDSN) == "" {
		return nil, fmt.Errorf("%w: evaluator, agent, and core DSNs are required", ErrInvalidConfig)
	}
	if !sameDatabase(cfg.DSN, cfg.AgentDSN) {
		return nil, fmt.Errorf("%w: evaluator issuer must use the isolated agent database", ErrInvalidConfig)
	}
	if sameCredential(cfg.DSN, cfg.AgentDSN) {
		return nil, ErrSharedCredential
	}
	for _, other := range []string{cfg.CoreDSN, cfg.ChatDSN, cfg.DocumentDSN} {
		if strings.TrimSpace(other) == "" {
			continue
		}
		if sameDatabase(cfg.DSN, other) {
			return nil, ErrSharedDatabase
		}
		if sameCredential(cfg.DSN, other) {
			return nil, ErrSharedCredential
		}
	}
	dsn, err := withPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	pool, err := pgxadapter.NewPool(ctx, dsn, map[string]string{"role": SyntheticTenantProvisionerRole})
	if err != nil {
		return nil, fmt.Errorf("agentstore: open evaluator provision issuer pool: %w", err)
	}
	return &SyntheticTenantProvisionIssuerStore{pool: pool}, nil
}

// IssueSyntheticTenantProvision writes an active explicit marker and its
// immutable issuance audit fact atomically.
func (s *SyntheticTenantProvisionIssuerStore) IssueSyntheticTenantProvision(ctx context.Context, issue SyntheticTenantProvisionIssue) error {
	r := issue.Record
	if s == nil || s.pool == nil || ctx == nil || !validSyntheticTenantProvision(r) || strings.TrimSpace(issue.ActorID) == "" {
		return ErrSyntheticProvisionInvalid
	}
	if r.Status != "ACTIVE" || r.RevokedAt != nil || r.RevokeReason != "" || r.IssuedAt.IsZero() {
		return ErrSyntheticProvisionInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agentstore: begin synthetic provision issue: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO synthetic_tenant_provision
		(tenant_id,suite_tenant_id,provision_id,marker_id,purpose,status,expires_at,grant_store_id,task_store_id,
		 budget_ledger_id,audit_store_id,tool_owner_id,tool_owner_profile,issued_at)
		VALUES ($1,$2,$3,$4,'agent-evaluation','ACTIVE',$5,$6,$7,$8,$9,$10,'fixture-only/v1',$11)`,
		r.TenantID, r.SuiteTenantID, r.ProvisionID, r.MarkerID, r.ExpiresAt, r.GrantStoreID, r.TaskStoreID,
		r.BudgetLedgerID, r.AuditStoreID, r.ToolOwnerID, r.IssuedAt)
	if err != nil {
		return fmt.Errorf("agentstore: persist synthetic provision: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO synthetic_tenant_provision_audit
		(tenant_id,provision_id,action,actor_id,occurred_at) VALUES ($1,$2,'ISSUED',$3,$4)`,
		r.TenantID, r.ProvisionID, issue.ActorID, r.IssuedAt); err != nil {
		return fmt.Errorf("agentstore: audit synthetic provision issuance: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentstore: commit synthetic provision issue: %w", err)
	}
	return nil
}

// RevokeSyntheticTenantProvision atomically revokes a marker and appends its
// immutable audit fact. A second revocation is rejected.
func (s *SyntheticTenantProvisionIssuerStore) RevokeSyntheticTenantProvision(ctx context.Context, tenantID uuid.UUID, provisionID, actorID, reason string, at time.Time) error {
	if s == nil || s.pool == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(provisionID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(reason) == "" || at.IsZero() {
		return ErrSyntheticProvisionInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agentstore: begin synthetic provision revocation: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	updated, err := tx.Exec(ctx, `UPDATE synthetic_tenant_provision SET status='REVOKED',revoked_at=$1,revoke_reason=$2
		WHERE tenant_id=$3 AND provision_id=$4 AND status='ACTIVE' AND issued_at <= $1`, at, reason, tenantID, provisionID)
	if err != nil {
		return fmt.Errorf("agentstore: revoke synthetic provision: %w", err)
	}
	if updated != 1 {
		return ErrSyntheticProvisionRevoked
	}
	if _, err := tx.Exec(ctx, `INSERT INTO synthetic_tenant_provision_audit
		(tenant_id,provision_id,action,actor_id,occurred_at,reason) VALUES ($1,$2,'REVOKED',$3,$4,$5)`,
		tenantID, provisionID, actorID, at, reason); err != nil {
		return fmt.Errorf("agentstore: audit synthetic provision revocation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentstore: commit synthetic provision revocation: %w", err)
	}
	return nil
}

// SyntheticTenantProvision reads the explicit marker and isolation bindings
// for exactly one tenant. The application role has SELECT only.
func (s *Store) SyntheticTenantProvision(ctx context.Context, tenantID uuid.UUID, suiteTenantID string) (SyntheticTenantProvisionRecord, error) {
	if s == nil || s.pool == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(suiteTenantID) == "" {
		return SyntheticTenantProvisionRecord{}, ErrInvalidConfig
	}
	var record SyntheticTenantProvisionRecord
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var revokedAt sqlNullableTime
		err := tx.QueryRow(ctx, `SELECT tenant_id,suite_tenant_id,provision_id,marker_id,purpose,status,expires_at,
			grant_store_id,task_store_id,budget_ledger_id,audit_store_id,tool_owner_id,tool_owner_profile,
			issued_at,revoked_at,revoke_reason FROM synthetic_tenant_provision
			WHERE tenant_id=$1 AND suite_tenant_id=$2 ORDER BY issued_at DESC,provision_id DESC LIMIT 1`, tenantID, suiteTenantID).Scan(
			&record.TenantID, &record.SuiteTenantID, &record.ProvisionID, &record.MarkerID, &record.Purpose, &record.Status, &record.ExpiresAt,
			&record.GrantStoreID, &record.TaskStoreID, &record.BudgetLedgerID, &record.AuditStoreID,
			&record.ToolOwnerID, &record.ToolOwnerProfile, &record.IssuedAt, &revokedAt, &record.RevokeReason)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrSyntheticProvisionAbsent
		}
		if err != nil {
			return fmt.Errorf("agentstore: read synthetic tenant provision: %w", err)
		}
		if revokedAt.Valid {
			t := revokedAt.Time
			record.RevokedAt = &t
		}
		return nil
	})
	if err != nil {
		return SyntheticTenantProvisionRecord{}, err
	}
	return record, nil
}

// Close releases the dedicated evaluator issuer pool.
func (s *SyntheticTenantProvisionIssuerStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

type sqlNullableTime struct {
	Time  time.Time
	Valid bool
}

func (n *sqlNullableTime) Scan(value any) error {
	if value == nil {
		n.Time = time.Time{}
		n.Valid = false
		return nil
	}
	t, ok := value.(time.Time)
	if !ok {
		return fmt.Errorf("agentstore: unexpected revoked_at type %T", value)
	}
	n.Time = t
	n.Valid = true
	return nil
}

func validSyntheticTenantProvision(r SyntheticTenantProvisionRecord) bool {
	return r.TenantID != uuid.Nil && strings.TrimSpace(r.SuiteTenantID) != "" && strings.TrimSpace(r.ProvisionID) != "" && strings.TrimSpace(r.MarkerID) != "" &&
		r.Purpose == "agent-evaluation" && r.Status != "" && r.ExpiresAt.After(r.IssuedAt) && !r.IssuedAt.IsZero() &&
		strings.TrimSpace(r.ToolOwnerProfile) == "fixture-only/v1" && distinctProvisionIDs(r.GrantStoreID, r.TaskStoreID,
		r.BudgetLedgerID, r.AuditStoreID, r.ToolOwnerID)
}

func distinctProvisionIDs(ids ...string) bool {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}
