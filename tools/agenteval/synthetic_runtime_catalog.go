package agenteval

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	runtimeeval "github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrSyntheticRuntimeCatalog = errors.New("agenteval: synthetic runtime catalog is incomplete")

// SyntheticToolOwnerBinding is trusted deployment configuration that binds a
// fixture-only owner instance to the exact ID stored in the provision marker.
type SyntheticToolOwnerBinding struct {
	ID      string
	Profile string
	Owner   agentsystem.ToolOwner
}

// SyntheticTaskRuntimeCatalog contains isolated runtime components indexed by
// their provisioned IDs. Entries are exact keys; there is no default or
// production fallback when a marker names an unknown component.
type SyntheticTaskRuntimeCatalog struct {
	Scope             SyntheticTenantStorageScopeResolver
	Reader            agentstore.SyntheticTenantProvisionReader
	PlatformBase      agentsystem.Config
	GrantStores       map[string]agentsystem.GrantScoper
	TaskStores        map[string]agentsystem.TaskScoper
	Budgets           map[string]*agentbudget.Ledger
	Audits            map[string]agentaudit.Store
	ToolOwners        map[string]SyntheticToolOwnerBinding
	UsageReaders      map[string]runtimeeval.SettledUsageReader
	Starts            map[values.TenantId]agentsystem.StartRequest
	ConnectionProfile string
	Now               func() time.Time
}

// DatabaseSyntheticTaskRuntimeResolver composes a real Platform from the
// exact grant, task, budget, audit and fixture-owner components named by the
// active persisted marker. Its authority method re-reads that marker before
// any tenant-scoped store is opened.
type DatabaseSyntheticTaskRuntimeResolver struct{ catalog SyntheticTaskRuntimeCatalog }

// NewDatabaseSyntheticTaskRuntimeResolver copies and validates trusted
// deployment bindings. The PlatformBase must provide the shared, non-store
// policy dependencies; this constructor replaces its budget, audit, grant,
// task and owner fields with marker-selected components.
func NewDatabaseSyntheticTaskRuntimeResolver(c SyntheticTaskRuntimeCatalog) (*DatabaseSyntheticTaskRuntimeResolver, error) {
	if c.Scope == nil || c.Reader == nil || c.Now == nil || len(c.Starts) == 0 ||
		len(c.GrantStores) == 0 || len(c.TaskStores) == 0 || len(c.Budgets) == 0 ||
		len(c.Audits) == 0 || len(c.ToolOwners) == 0 || len(c.UsageReaders) == 0 ||
		c.PlatformBase.Skills == nil || c.PlatformBase.Redactor == nil || c.PlatformBase.Egress == nil ||
		c.PlatformBase.Authority == nil || c.PlatformBase.Audience == "" || c.PlatformBase.Workload == "" ||
		len(c.PlatformBase.TokenSecret) < 16 || c.PlatformBase.ModelEstimate.Steps <= 0 {
		return nil, ErrSyntheticRuntimeCatalog
	}
	if c.PlatformBase.Connections != nil && c.ConnectionProfile != "fixture-only/v1" {
		return nil, fmt.Errorf("%w: synthetic task runtime requires fixture-only connection bindings", ErrSyntheticRuntimeCatalog)
	}
	c.GrantStores = cloneComponentMap(c.GrantStores)
	c.TaskStores = cloneComponentMap(c.TaskStores)
	c.Budgets = cloneComponentMap(c.Budgets)
	c.Audits = cloneComponentMap(c.Audits)
	c.ToolOwners = cloneComponentMap(c.ToolOwners)
	c.UsageReaders = cloneComponentMap(c.UsageReaders)
	starts := make(map[values.TenantId]agentsystem.StartRequest, len(c.Starts))
	for tenant, start := range c.Starts {
		starts[tenant] = cloneStartRequest(start)
	}
	c.Starts = starts
	c.PlatformBase.TokenSecret = append([]byte(nil), c.PlatformBase.TokenSecret...)
	return &DatabaseSyntheticTaskRuntimeResolver{catalog: c}, nil
}

// AuthorizeSyntheticTenant validates the active durable marker before
// agentsystem.Platform.ForTenant can scope its grant and task stores.
func (r *DatabaseSyntheticTaskRuntimeResolver) AuthorizeSyntheticTenant(ctx context.Context, tenant values.TenantId) error {
	if r == nil || ctx == nil || tenant.Validate() != nil {
		return ErrSyntheticProvisionUnavailable
	}
	record, err := r.readProvision(ctx, tenant)
	if err != nil || !validPersistedProvision(record, tenant, r.catalog.Now()) {
		return ErrSyntheticProvisionUnavailable
	}
	return nil
}

// ComposeSyntheticTaskRuntime selects each dependency by the exact persisted
// ID, rejects accidental reuse across isolation roles, and constructs the
// tenant task runner configuration. No ID is accepted from the task case.
func (r *DatabaseSyntheticTaskRuntimeResolver) ComposeSyntheticTaskRuntime(ctx context.Context, tenant values.TenantId, record agentstore.SyntheticTenantProvisionRecord) (SyntheticTaskProvision, error) {
	if r == nil || ctx == nil || tenant.Validate() != nil || record.SuiteTenantID != tenant.String() ||
		!validPersistedProvision(record, tenant, r.catalog.Now()) {
		return SyntheticTaskProvision{}, ErrSyntheticRuntimeCatalog
	}
	tenantUUID, err := r.catalog.Scope(ctx, tenant)
	if err != nil || tenantUUID == uuid.Nil || tenantUUID != record.TenantID {
		return SyntheticTaskProvision{}, fmt.Errorf("%w: trusted suite tenant mapping does not match the marker", ErrSyntheticRuntimeCatalog)
	}
	if err := ctx.Err(); err != nil {
		return SyntheticTaskProvision{}, err
	}
	grant, okGrant := r.catalog.GrantStores[record.GrantStoreID]
	tasks, okTasks := r.catalog.TaskStores[record.TaskStoreID]
	budget, okBudget := r.catalog.Budgets[record.BudgetLedgerID]
	audit, okAudit := r.catalog.Audits[record.AuditStoreID]
	ownerBinding, okOwner := r.catalog.ToolOwners[record.ToolOwnerID]
	usage, okUsage := r.catalog.UsageReaders[record.BudgetLedgerID]
	start, okStart := r.catalog.Starts[tenant]
	if !okGrant || !okTasks || !okBudget || !okAudit || !okOwner || !okUsage || !okStart ||
		grant == nil || tasks == nil || budget == nil || audit == nil || ownerBinding.Owner == nil || usage == nil ||
		ownerBinding.ID != record.ToolOwnerID || ownerBinding.Profile != record.ToolOwnerProfile || ownerBinding.Profile != "fixture-only/v1" {
		return SyntheticTaskProvision{}, ErrSyntheticRuntimeCatalog
	}
	if !distinctRuntimeObjects(grant, tasks, budget, audit, ownerBinding.Owner) {
		return SyntheticTaskProvision{}, fmt.Errorf("%w: one runtime object is registered for multiple isolation roles", ErrSyntheticRuntimeCatalog)
	}
	if start.UserAuthority.Tenant != tenant || strings.TrimSpace(start.UserID) == "" {
		return SyntheticTaskProvision{}, fmt.Errorf("%w: suite task principal is not tenant-bound", ErrSyntheticRuntimeCatalog)
	}
	base := r.catalog.PlatformBase
	base.Grants, base.Tasks, base.Budget, base.Audit, base.Owner = grant, tasks, budget, audit, ownerBinding.Owner
	platform, err := agentsystem.NewPlatform(base)
	if err != nil {
		return SyntheticTaskProvision{}, fmt.Errorf("%w: construct isolated agent platform: %v", ErrSyntheticRuntimeCatalog, err)
	}
	now := r.catalog.Now
	return SyntheticTaskProvision{
		Marker: SyntheticTenantMarker{TenantID: tenant.String(), MarkerID: record.MarkerID, Purpose: record.Purpose,
			ProvisionID: record.ProvisionID, Status: record.Status, ExpiresAt: record.ExpiresAt},
		Isolation: SyntheticTaskIsolation{TenantID: tenant.String(), GrantStoreID: record.GrantStoreID,
			TaskStoreID: record.TaskStoreID, BudgetLedgerID: record.BudgetLedgerID, AuditStoreID: record.AuditStoreID,
			ToolOwnerID: record.ToolOwnerID, ToolOwnerProfile: record.ToolOwnerProfile},
		Platform: platform, Authority: r, Usage: usage, Now: now, Start: cloneStartRequest(start),
	}, nil
}

func (r *DatabaseSyntheticTaskRuntimeResolver) readProvision(ctx context.Context, tenant values.TenantId) (agentstore.SyntheticTenantProvisionRecord, error) {
	id, err := r.catalog.Scope(ctx, tenant)
	if err != nil || id == uuid.Nil {
		return agentstore.SyntheticTenantProvisionRecord{}, ErrSyntheticProvisionUnavailable
	}
	record, err := r.catalog.Reader.SyntheticTenantProvision(ctx, id, tenant.String())
	if err != nil || record.TenantID != id || record.SuiteTenantID != tenant.String() {
		return agentstore.SyntheticTenantProvisionRecord{}, ErrSyntheticProvisionUnavailable
	}
	return record, nil
}

func distinctRuntimeObjects(objects ...any) bool {
	seen := make(map[uintptr]struct{}, len(objects))
	for _, object := range objects {
		v := reflect.ValueOf(object)
		if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
			return false
		}
		id := v.Pointer()
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func cloneComponentMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStartRequest(in agentsystem.StartRequest) agentsystem.StartRequest {
	in.Constraints = append([]string(nil), in.Constraints...)
	in.Steps = append([]agentrun.PlanStep(nil), in.Steps...)
	in.UserAuthority.Capabilities = append([]string(nil), in.UserAuthority.Capabilities...)
	in.UserAuthority.Resources = append([]string(nil), in.UserAuthority.Resources...)
	in.UserAuthority.Fields = append([]string(nil), in.UserAuthority.Fields...)
	in.UserAuthority.Purposes = append([]string(nil), in.UserAuthority.Purposes...)
	in.UserAuthority.SkillAuthorities = trust.CloneSkillAuthorities(in.UserAuthority.SkillAuthorities)
	return in
}

var _ SyntheticTaskRuntimeResolver = (*DatabaseSyntheticTaskRuntimeResolver)(nil)
var _ agentsystem.SyntheticTenantAuthority = (*DatabaseSyntheticTaskRuntimeResolver)(nil)
