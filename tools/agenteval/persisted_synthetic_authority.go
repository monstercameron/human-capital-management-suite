package agenteval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrSyntheticAuthorityConfig = errors.New("agenteval: persisted synthetic authority is not configured")

// SyntheticTaskRuntimeResolver composes the live evaluator-only runtime from
// trusted deployment configuration and the durable store identities. It must
// bind each returned store and tool owner to the exact persisted ID.
type SyntheticTaskRuntimeResolver interface {
	ComposeSyntheticTaskRuntime(context.Context, values.TenantId, agentstore.SyntheticTenantProvisionRecord) (SyntheticTaskProvision, error)
}

// SyntheticTenantStorageScopeResolver maps an exact logical suite tenant to
// its provisioned agent-database UUID. Implementations must use explicit
// trusted bindings, never tenant-name prefixes or caller input conventions.
type SyntheticTenantStorageScopeResolver func(context.Context, values.TenantId) (uuid.UUID, error)

// PersistedSyntheticTaskProvisionAuthority resolves explicit durable markers
// and asks a trusted composition root to bind the isolated runtime objects.
type PersistedSyntheticTaskProvisionAuthority struct {
	reader  agentstore.SyntheticTenantProvisionReader
	runtime SyntheticTaskRuntimeResolver
	scope   SyntheticTenantStorageScopeResolver
	now     func() time.Time
}

// NewPersistedSyntheticTaskProvisionAuthority requires a tenant-RLS reader, a
// trusted runtime composer and a server-owned clock.
func NewPersistedSyntheticTaskProvisionAuthority(reader agentstore.SyntheticTenantProvisionReader, runtime SyntheticTaskRuntimeResolver, scope SyntheticTenantStorageScopeResolver, now func() time.Time) (*PersistedSyntheticTaskProvisionAuthority, error) {
	if reader == nil || runtime == nil || scope == nil || now == nil {
		return nil, ErrSyntheticAuthorityConfig
	}
	return &PersistedSyntheticTaskProvisionAuthority{reader: reader, runtime: runtime, scope: scope, now: now}, nil
}

// AuthorizeSyntheticTenant checks the current persisted marker before the
// platform binds any tenant-scoped grant or task stores.
func (a *PersistedSyntheticTaskProvisionAuthority) AuthorizeSyntheticTenant(ctx context.Context, tenant values.TenantId) error {
	if a == nil || a.reader == nil || ctx == nil || tenant.Validate() != nil {
		return ErrSyntheticProvisionUnavailable
	}
	record, err := a.readActive(ctx, tenant)
	if err != nil {
		return err
	}
	if !validPersistedProvision(record, tenant, a.now()) {
		return ErrSyntheticProvisionUnavailable
	}
	return nil
}

// ResolveSyntheticTaskProvision validates the durable marker, composes the
// evaluator-only runtime, and pins every isolation identity from storage.
func (a *PersistedSyntheticTaskProvisionAuthority) ResolveSyntheticTaskProvision(ctx context.Context, tenant values.TenantId) (SyntheticTaskProvision, error) {
	if a == nil || a.reader == nil || a.runtime == nil || ctx == nil || tenant.Validate() != nil {
		return SyntheticTaskProvision{}, ErrSyntheticProvisionUnavailable
	}
	record, err := a.readActive(ctx, tenant)
	if err != nil {
		return SyntheticTaskProvision{}, err
	}
	now := a.now()
	if !validPersistedProvision(record, tenant, now) {
		return SyntheticTaskProvision{}, ErrSyntheticProvisionUnavailable
	}
	provision, err := a.runtime.ComposeSyntheticTaskRuntime(ctx, tenant, record)
	if err != nil {
		return SyntheticTaskProvision{}, fmt.Errorf("%w: compose isolated evaluator runtime: %v", ErrSyntheticProvisionUnavailable, err)
	}
	wantMarker := SyntheticTenantMarker{
		TenantID: tenant.String(), MarkerID: record.MarkerID, Purpose: record.Purpose,
		ProvisionID: record.ProvisionID, Status: record.Status, ExpiresAt: record.ExpiresAt,
	}
	wantIsolation := SyntheticTaskIsolation{
		TenantID: tenant.String(), GrantStoreID: record.GrantStoreID, TaskStoreID: record.TaskStoreID,
		BudgetLedgerID: record.BudgetLedgerID, AuditStoreID: record.AuditStoreID,
		ToolOwnerID: record.ToolOwnerID, ToolOwnerProfile: record.ToolOwnerProfile,
	}
	if provision.Marker != wantMarker || provision.Isolation != wantIsolation {
		return SyntheticTaskProvision{}, ErrSyntheticProvisionUnavailable
	}
	if provision.Now == nil || !provision.Now().Before(record.ExpiresAt) {
		return SyntheticTaskProvision{}, ErrSyntheticProvisionUnavailable
	}
	if err := validateSyntheticProvision(tenant, provision, now); err != nil {
		return SyntheticTaskProvision{}, err
	}
	return provision, nil
}

func (a *PersistedSyntheticTaskProvisionAuthority) readActive(ctx context.Context, tenant values.TenantId) (agentstore.SyntheticTenantProvisionRecord, error) {
	tenantUUID, err := a.scope(ctx, tenant)
	if err != nil || tenantUUID == uuid.Nil {
		return agentstore.SyntheticTenantProvisionRecord{}, ErrSyntheticProvisionUnavailable
	}
	record, err := a.reader.SyntheticTenantProvision(ctx, tenantUUID, tenant.String())
	if err != nil {
		return agentstore.SyntheticTenantProvisionRecord{}, fmt.Errorf("%w: read durable marker: %v", ErrSyntheticProvisionUnavailable, err)
	}
	if record.TenantID != tenantUUID || record.SuiteTenantID != tenant.String() {
		return agentstore.SyntheticTenantProvisionRecord{}, ErrSyntheticProvisionUnavailable
	}
	return record, nil
}

func validPersistedProvision(record agentstore.SyntheticTenantProvisionRecord, tenant values.TenantId, now time.Time) bool {
	return record.SuiteTenantID == tenant.String() && strings.TrimSpace(record.ProvisionID) != "" &&
		strings.TrimSpace(record.MarkerID) != "" && record.Purpose == syntheticEvaluationPurpose &&
		record.Status == "ACTIVE" && record.RevokedAt == nil && record.RevokeReason == "" &&
		!record.IssuedAt.After(now) && record.ExpiresAt.After(now) &&
		record.ToolOwnerProfile == "fixture-only/v1" && distinctNonempty(record.GrantStoreID,
		record.TaskStoreID, record.BudgetLedgerID, record.AuditStoreID, record.ToolOwnerID)
}

var _ SyntheticTaskProvisionAuthority = (*PersistedSyntheticTaskProvisionAuthority)(nil)
