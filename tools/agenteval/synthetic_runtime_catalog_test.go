package agenteval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	runtimeeval "github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

type syntheticCatalogRedactor struct{}

func (syntheticCatalogRedactor) Redact(_ context.Context, text string, _ []string) (agentmodel.Redaction, error) {
	return agentmodel.Redaction{Text: text}, nil
}

func TestDatabaseSyntheticTaskRuntimeResolverRequiresCompleteTrustedComposition(t *testing.T) {
	if _, err := NewDatabaseSyntheticTaskRuntimeResolver(SyntheticTaskRuntimeCatalog{}); !errors.Is(err, ErrSyntheticRuntimeCatalog) {
		t.Fatalf("empty resolver config error = %v, want catalog refusal", err)
	}

	const tenant values.TenantId = "synthetic-promotion"
	tenantUUID := uuid.New()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	record := agentstoreRecordForTest(tenantUUID, tenant, now)
	reader := syntheticProvisionReaderProbe{record: record}
	base := syntheticCatalogBase(t, tenant, now)
	scope, err := NewStaticSyntheticTenantStorageScopeResolver(map[values.TenantId]uuid.UUID{tenant: tenantUUID})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewDatabaseSyntheticTaskRuntimeResolver(SyntheticTaskRuntimeCatalog{
		Scope: scope, Reader: reader, PlatformBase: base,
		GrantStores:  map[string]agentsystem.GrantScoper{record.GrantStoreID: nil},
		TaskStores:   map[string]agentsystem.TaskScoper{record.TaskStoreID: nil},
		Budgets:      map[string]*agentbudget.Ledger{record.BudgetLedgerID: nil},
		Audits:       map[string]agentaudit.Store{record.AuditStoreID: nil},
		ToolOwners:   map[string]SyntheticToolOwnerBinding{record.ToolOwnerID: {ID: record.ToolOwnerID, Profile: record.ToolOwnerProfile}},
		UsageReaders: map[string]runtimeeval.SettledUsageReader{record.BudgetLedgerID: nil},
		Starts:       map[values.TenantId]agentsystem.StartRequest{tenant: {UserID: "fixture-user", UserAuthority: trust.AuthorityScope{Tenant: tenant}}},
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewDatabaseSyntheticTaskRuntimeResolver() error = %v", err)
	}
	if err := resolver.AuthorizeSyntheticTenant(context.Background(), tenant); err != nil {
		t.Fatalf("AuthorizeSyntheticTenant() error = %v", err)
	}
	if _, err := resolver.ComposeSyntheticTaskRuntime(context.Background(), tenant, record); !errors.Is(err, ErrSyntheticRuntimeCatalog) {
		t.Fatalf("ComposeSyntheticTaskRuntime() error = %v, want missing component denial", err)
	}

	owner := &syntheticCatalogOwner{id: record.ToolOwnerID}
	grantStore := &syntheticCatalogGrantScoper{id: record.GrantStoreID}
	taskStore := &syntheticCatalogTaskScoper{id: record.TaskStoreID}
	budget, err := agentbudget.NewWithClock(agentbudget.Policy{TaskDefault: agentbudget.Limits{Steps: 10, Tokens: 1000, WallClock: time.Minute, SpendMicros: 1000}, UserDaily: agentbudget.Limits{Steps: 10, Tokens: 1000, WallClock: time.Minute, SpendMicros: 1000}, TenantMonthly: agentbudget.Limits{Steps: 10, Tokens: 1000, WallClock: time.Minute, SpendMicros: 1000}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	usage := &syntheticCatalogUsage{}
	resolver.catalog.GrantStores[record.GrantStoreID] = grantStore
	resolver.catalog.TaskStores[record.TaskStoreID] = taskStore
	resolver.catalog.Budgets[record.BudgetLedgerID] = budget
	resolver.catalog.Audits[record.AuditStoreID] = agentaudit.NewMemoryStore()
	resolver.catalog.ToolOwners[record.ToolOwnerID] = SyntheticToolOwnerBinding{ID: record.ToolOwnerID, Profile: record.ToolOwnerProfile, Owner: owner}
	resolver.catalog.UsageReaders[record.BudgetLedgerID] = usage
	provision, err := resolver.ComposeSyntheticTaskRuntime(context.Background(), tenant, record)
	if err != nil {
		t.Fatalf("ComposeSyntheticTaskRuntime() error = %v", err)
	}
	if provision.Platform == nil || provision.Authority != resolver || provision.Usage != usage || provision.Marker.MarkerID != record.MarkerID ||
		provision.Isolation.GrantStoreID != record.GrantStoreID || provision.Isolation.TaskStoreID != record.TaskStoreID ||
		provision.Isolation.BudgetLedgerID != record.BudgetLedgerID || provision.Isolation.AuditStoreID != record.AuditStoreID ||
		provision.Isolation.ToolOwnerID != record.ToolOwnerID {
		t.Fatalf("composed provision did not bind the marker-selected runtime: %+v", provision)
	}

	wrongScope, err := NewStaticSyntheticTenantStorageScopeResolver(map[values.TenantId]uuid.UUID{tenant: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	resolver.catalog.Scope = wrongScope
	if _, err := resolver.ComposeSyntheticTaskRuntime(context.Background(), tenant, record); !errors.Is(err, ErrSyntheticRuntimeCatalog) {
		t.Fatalf("mismatched persisted UUID error = %v, want mapping refusal", err)
	}
	record.Status = "REVOKED"
	reader.record = record
	if err := resolver.AuthorizeSyntheticTenant(context.Background(), tenant); !errors.Is(err, ErrSyntheticProvisionUnavailable) {
		t.Fatalf("revoked marker authorization error = %v, want fail-closed refusal", err)
	}
}

func syntheticCatalogBase(t *testing.T, tenant values.TenantId, now time.Time) agentsystem.Config {
	t.Helper()
	outboundPolicy, err := outbound.NewPolicy()
	if err != nil {
		t.Fatal(err)
	}
	dlpPolicy, err := dlp.NewPolicy(outboundPolicy)
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := dlp.NewInspector()
	if err != nil {
		t.Fatal(err)
	}
	egress, err := agentegress.NewEvaluator(dlpPolicy, inspector, dlp.NewReceiptLog())
	if err != nil {
		t.Fatal(err)
	}
	limits := agentbudget.Limits{Steps: 10, Tokens: 1000, WallClock: time.Minute, SpendMicros: 1000}
	budget, err := agentbudget.NewWithClock(agentbudget.Policy{TaskDefault: limits, UserDaily: limits, TenantMonthly: limits}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return agentsystem.Config{
		Skills: agentskills.NewRegistry(nil), Budget: budget, Audit: agentaudit.NewMemoryStore(),
		Redactor: syntheticCatalogRedactor{}, Egress: egress,
		Authority: agentdelegation.ResolverFunc(func(userID string, resolved values.TenantId, purpose string, at time.Time) (agentdelegation.UserAuthority, error) {
			return agentdelegation.UserAuthority{UserID: userID, Active: true, Authority: trust.AuthorityScope{Tenant: resolved, Purposes: []string{purpose}, NotBefore: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour)}}, nil
		}),
		TokenSecret: []byte("synthetic-runtime-secret-0123456789"), Audience: "fixture-tool-gateway", Workload: "fixture/evaluator",
		ModelEstimate: agentbudget.Usage{Steps: 1}, Clock: func() time.Time { return now },
	}
}

func agentstoreRecordForTest(tenantID uuid.UUID, tenant values.TenantId, now time.Time) agentstore.SyntheticTenantProvisionRecord {
	return agentstore.SyntheticTenantProvisionRecord{
		TenantID: tenantID, SuiteTenantID: tenant.String(), ProvisionID: "provision-test", MarkerID: "marker-test",
		Purpose: syntheticEvaluationPurpose, Status: "ACTIVE", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		GrantStoreID: "grants-test", TaskStoreID: "tasks-test", BudgetLedgerID: "budget-test", AuditStoreID: "audit-test",
		ToolOwnerID: "owner-test", ToolOwnerProfile: "fixture-only/v1",
	}
}

type syntheticCatalogGrantScoper struct{ id string }

func (*syntheticCatalogGrantScoper) ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error) {
	return agentdelegation.NewMemoryGrantStore(), nil
}

type syntheticCatalogTaskScoper struct{ id string }

func (*syntheticCatalogTaskScoper) ForTenant(context.Context, values.TenantId) (agentrun.TaskStore, error) {
	return agentrun.NewMemoryStore(), nil
}

type syntheticCatalogUsage struct{}

func (*syntheticCatalogUsage) SettledTaskUsage(_ context.Context, tenant values.TenantId, taskID string) (agentbudget.SettledTaskUsage, error) {
	return agentbudget.SettledTaskUsage{TenantID: tenant.String(), TaskID: taskID}, nil
}

type syntheticCatalogOwner struct{ id string }

func (*syntheticCatalogOwner) Verify(context.Context, agentrun.AgentTask, agentrun.PlanStep, agentrun.StepResult) error {
	return nil
}

func (*syntheticCatalogOwner) Prepare(context.Context, agentsystem.PrepareRequest) (agentsystem.Prepared, error) {
	return agentsystem.Prepared{}, nil
}

func (*syntheticCatalogOwner) Invoke(context.Context, agentsystem.Invocation) (agentsystem.Result, error) {
	return agentsystem.Result{}, nil
}

func (*syntheticCatalogOwner) Retain(context.Context, agentrun.AgentTask, agentrun.PlanStep, string, agentsecurity.QuarantineExtraction) error {
	return nil
}

var _ runtimeeval.SettledUsageReader = (*syntheticCatalogUsage)(nil)
