package agentsystem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type syntheticTenantAuthorityFunc func(context.Context, values.TenantId) error

func (f syntheticTenantAuthorityFunc) AuthorizeSyntheticTenant(ctx context.Context, tenant values.TenantId) error {
	return f(ctx, tenant)
}

func TestForSyntheticTenantRejectsBeforeScopingStores(t *testing.T) {
	f := newFixture(t, nil)
	tenant := values.TenantId("synthetic-unknown")
	authorityCalls := 0
	runner, err := f.platform.ForSyntheticTenant(context.Background(), tenant, syntheticTenantAuthorityFunc(func(_ context.Context, got values.TenantId) error {
		authorityCalls++
		if got != tenant {
			t.Fatalf("authority tenant = %q, want %q", got, tenant)
		}
		return errors.New("tenant is not provisioned for evaluation")
	}))
	if !errors.Is(err, ErrSyntheticTenantDenied) || runner != nil {
		t.Fatalf("ForSyntheticTenant() = runner %v, error %v; want denial without a runner", runner, err)
	}
	if authorityCalls != 1 {
		t.Fatalf("synthetic authority calls = %d, want 1", authorityCalls)
	}
	f.scopers.mu.Lock()
	_, grantScoped := f.scopers.grants[tenant]
	_, taskScoped := f.scopers.tasks[tenant]
	f.scopers.mu.Unlock()
	if grantScoped || taskScoped {
		t.Fatalf("denied tenant reached tenant stores: grant=%v task=%v", grantScoped, taskScoped)
	}
}

func TestStartSyntheticTaskUsesIsolatedTenantBoundRunner(t *testing.T) {
	base := newFixture(t, nil)
	scopes := &memoryScopers{grants: map[values.TenantId]*agentdelegation.MemoryGrantStore{}, tasks: map[values.TenantId]*agentrun.MemoryStore{}}
	ledger, err := agentbudget.NewWithPersistence(testPolicy(), func() time.Time { return fixedNow }, nil)
	if err != nil {
		t.Fatal(err)
	}
	const tenant values.TenantId = "synthetic-promotion"
	cfg := base.platform.cfg
	cfg.Budget = ledger
	cfg.Audit = agentaudit.NewMemoryStore()
	cfg.Grants = grantScoper{scopes}
	cfg.Tasks = taskScoper{scopes}
	cfg.Owner = &fakeOwner{}
	cfg.Connections = nil
	cfg.Authority = agentdelegation.ResolverFunc(func(userID string, resolvedTenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		current := userAuthority(true)
		current.UserID = userID
		current.Authority.Tenant = resolvedTenant
		return current, nil
	})
	platform, err := NewPlatform(cfg)
	if err != nil {
		t.Fatalf("NewPlatform() error = %v", err)
	}
	requestAuthority := userAuthority(true).Authority
	requestAuthority.Tenant = tenant
	runner, task, err := platform.StartSyntheticTask(context.Background(), tenant,
		syntheticTenantAuthorityFunc(func(_ context.Context, got values.TenantId) error {
			if got != tenant {
				t.Fatalf("authorized tenant = %q, want %q", got, tenant)
			}
			return nil
		}), StartRequest{
			TaskID: "eval-promotion-001", UserID: "user-42", AgentVersion: "eval-agent/v1", InstallationID: "eval-install",
			Purpose: purposeKey, OrganizationScopeID: "org-west", Goal: "prepare and verify a synthetic promotion",
			Steps:         []agentrun.PlanStep{planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead)},
			UserAuthority: requestAuthority, Lifetime: time.Hour,
		})
	if err != nil {
		t.Fatalf("StartSyntheticTask() error = %v", err)
	}
	if runner.TenantID() != tenant || task.TenantID != tenant.String() || task.ID != "eval-promotion-001" {
		t.Fatalf("bound runner/task = tenant %q, %+v; want %q", runner.TenantID(), task, tenant)
	}
	stored, err := runner.Runtime.GetTask(context.Background(), task.ID)
	if err != nil || stored.TenantID != tenant.String() {
		t.Fatalf("bound runtime task = %+v, error %v; want synthetic tenant", stored, err)
	}
	if scopes.tasks[tenantKey] != nil || scopes.grants[tenantKey] != nil {
		t.Fatalf("synthetic start touched production tenant stores")
	}
	if scopes.tasks[tenant] == nil || scopes.grants[tenant] == nil {
		t.Fatalf("synthetic start did not allocate both tenant-scoped stores")
	}
}

func TestStartSyntheticTaskRejectsMismatchedAuthorityBeforeBinding(t *testing.T) {
	f := newFixture(t, nil)
	const tenant values.TenantId = "synthetic-policy"
	called := false
	runner, task, err := f.platform.StartSyntheticTask(context.Background(), tenant, syntheticTenantAuthorityFunc(func(context.Context, values.TenantId) error {
		called = true
		return nil
	}), StartRequest{TaskID: "eval-policy-001", UserAuthority: userAuthority(true).Authority})
	if !errors.Is(err, ErrSyntheticTenantDenied) || runner != nil || task.ID != "" || called {
		t.Fatalf("StartSyntheticTask() = runner %v task %+v error %v authorityCalled=%v; want pre-bind refusal", runner, task, err, called)
	}
	f.scopers.mu.Lock()
	_, scoped := f.scopers.tasks[tenant]
	f.scopers.mu.Unlock()
	if scoped {
		t.Fatal("mismatched authority tenant reached task scoper")
	}
}
