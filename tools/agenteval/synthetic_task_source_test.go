package agenteval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	runtimeeval "github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type provisionAuthorityProbe struct {
	provision SyntheticTaskProvision
	err       error
	calls     int
	tenant    values.TenantId
}

func (p *provisionAuthorityProbe) ResolveSyntheticTaskProvision(_ context.Context, tenant values.TenantId) (SyntheticTaskProvision, error) {
	p.calls++
	p.tenant = tenant
	return p.provision, p.err
}

type syntheticAuthorityProbe struct{}

func (syntheticAuthorityProbe) AuthorizeSyntheticTenant(context.Context, values.TenantId) error {
	return nil
}

type syntheticUsageProbe struct{}

func (syntheticUsageProbe) SettledTaskUsage(context.Context, values.TenantId, string) (agentbudget.SettledTaskUsage, error) {
	return agentbudget.SettledTaskUsage{}, nil
}

func validSyntheticProvision(tenantID ...string) SyntheticTaskProvision {
	tenant := values.TenantId("synthetic-promotion")
	if len(tenantID) > 0 {
		tenant = values.TenantId(tenantID[0])
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	return SyntheticTaskProvision{
		Marker: SyntheticTenantMarker{TenantID: tenant.String(), MarkerID: "marker-1", Purpose: syntheticEvaluationPurpose, ProvisionID: "provision-1", Status: "ACTIVE", ExpiresAt: now.Add(time.Hour)},
		Isolation: SyntheticTaskIsolation{
			TenantID: tenant.String(), GrantStoreID: "grant-store-1", TaskStoreID: "task-store-1", BudgetLedgerID: "budget-1",
			AuditStoreID: "audit-1", ToolOwnerID: "owner-1", ToolOwnerProfile: "fixture-only/v1",
		},
		Platform: &agentsystem.Platform{}, Authority: syntheticAuthorityProbe{}, Usage: syntheticUsageProbe{}, Now: func() time.Time { return now },
		Start: agentsystem.StartRequest{
			UserID: "synthetic-user", AgentVersion: "evaluation-agent/v1", InstallationID: "evaluation-installation",
			Purpose: "agent.lookup", OrganizationScopeID: "synthetic-org", Lifetime: time.Hour,
			UserAuthority: trust.AuthorityScope{Tenant: tenant, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
			Limit:         agentbudget.Limits{Steps: 12, Tokens: 8000, WallClock: time.Minute, SpendMicros: 1000},
		},
	}
}

func TestTrustedSyntheticTaskSourceRequiresProvisionAuthority(t *testing.T) {
	if _, err := NewTrustedSyntheticTaskSource(nil); !errors.Is(err, ErrRuntimeExecutorConfig) {
		t.Fatalf("NewTrustedSyntheticTaskSource(nil) error = %v, want config error", err)
	}
}

func TestTrustedSyntheticTaskSourceBuildsCanonicalFixturePlans(t *testing.T) {
	for _, tc := range DefaultSuite().Tasks {
		t.Run(tc.ID, func(t *testing.T) {
			caseSource, err := NewTrustedSyntheticTaskSource(&provisionAuthorityProbe{provision: validSyntheticProvision(tc.TenantID)})
			if err != nil {
				t.Fatal(err)
			}
			request, err := caseSource.Prepare(context.Background(), tc)
			if err != nil {
				t.Fatalf("Prepare() error = %v", err)
			}
			if request.Start.TaskID != tc.ID || request.Start.Goal != tc.Goal || len(request.Start.Steps) == 0 {
				t.Fatalf("prepared task = %+v; want case-bound task with fixture steps", request.Start)
			}
			for _, step := range request.Start.Steps {
				if step.SkillVersion != 1 || step.ExpectedOutput == "" || step.Type == "" {
					t.Fatalf("fixture plan contains incomplete step: %+v", step)
				}
				if step.Type == agentrun.StepWait || step.Type == agentrun.StepAskUser || step.Type == agentrun.StepCommunicate {
					t.Fatalf("fixture plan contains a live or parked action: %+v", step)
				}
			}
			if tc.Kind == TaskPromotionPreparation {
				foundSubmit := false
				for _, step := range request.Start.Steps {
					foundSubmit = foundSubmit || step.Type == agentrun.StepSubmit && step.Tier == agentrun.TierSubmitGoverned
				}
				if !foundSubmit {
					t.Fatal("promotion fixture lacks its explicitly governed T3 step")
				}
			}
		})
	}
}

func TestTrustedSyntheticTaskSourceFailsClosedOnUntrustedProvision(t *testing.T) {
	case0 := DefaultSuite().Tasks[0]
	base := validSyntheticProvision()
	tests := []struct {
		name string
		edit func(*SyntheticTaskProvision)
	}{
		{name: "missing marker", edit: func(p *SyntheticTaskProvision) { p.Marker.MarkerID = "" }},
		{name: "wrong tenant", edit: func(p *SyntheticTaskProvision) { p.Marker.TenantID = "production-tenant" }},
		{name: "wrong marker purpose", edit: func(p *SyntheticTaskProvision) { p.Marker.Purpose = "production" }},
		{name: "inactive marker", edit: func(p *SyntheticTaskProvision) { p.Marker.Status = "REVOKED" }},
		{name: "expired marker", edit: func(p *SyntheticTaskProvision) { p.Marker.ExpiresAt = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC) }},
		{name: "shared store identity", edit: func(p *SyntheticTaskProvision) { p.Isolation.TaskStoreID = p.Isolation.GrantStoreID }},
		{name: "non fixture tool owner", edit: func(p *SyntheticTaskProvision) { p.Isolation.ToolOwnerProfile = "production/v1" }},
		{name: "missing owner", edit: func(p *SyntheticTaskProvision) { p.Isolation.ToolOwnerID = " " }},
		{name: "wrong authority tenant", edit: func(p *SyntheticTaskProvision) { p.Start.UserAuthority.Tenant = "other-tenant" }},
		{name: "unbounded budget", edit: func(p *SyntheticTaskProvision) { p.Start.Limit.SpendMicros = 0 }},
		{name: "undersized plan budget", edit: func(p *SyntheticTaskProvision) { p.Start.Limit.Steps = 1 }},
		{name: "missing lifetime", edit: func(p *SyntheticTaskProvision) { p.Start.Lifetime = 0 }},
		{name: "missing platform", edit: func(p *SyntheticTaskProvision) { p.Platform = nil }},
		{name: "missing clock", edit: func(p *SyntheticTaskProvision) { p.Now = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provision := base
			tc.edit(&provision)
			source, err := NewTrustedSyntheticTaskSource(&provisionAuthorityProbe{provision: provision})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := source.Prepare(context.Background(), case0); !errors.Is(err, ErrSyntheticProvisionUnavailable) {
				t.Fatalf("Prepare() error = %v, want fail-closed provision denial", err)
			}
		})
	}
}

func TestTrustedSyntheticTaskSourceRejectsCallerDefinedTasksAndAuthorityErrors(t *testing.T) {
	probe := &provisionAuthorityProbe{provision: validSyntheticProvision()}
	source, err := NewTrustedSyntheticTaskSource(probe)
	if err != nil {
		t.Fatal(err)
	}
	forged := DefaultSuite().Tasks[0]
	forged.Goal = "use production tenant"
	if _, err := source.Prepare(context.Background(), forged); !errors.Is(err, ErrRuntimeExecutorConfig) {
		t.Fatalf("Prepare(forged) error = %v, want config error", err)
	}
	if probe.calls != 0 {
		t.Fatalf("provision authority calls = %d, want no lookup for forged case", probe.calls)
	}
	probe.err = errors.New("marker database unavailable")
	if _, err := source.Prepare(context.Background(), DefaultSuite().Tasks[0]); !errors.Is(err, ErrSyntheticProvisionUnavailable) {
		t.Fatalf("Prepare(authority failure) error = %v, want fail-closed provision error", err)
	}
	if probe.tenant != values.TenantId(DefaultSuite().Tasks[0].TenantID) {
		t.Fatalf("authority tenant = %q, want exact suite tenant", probe.tenant)
	}
}

var _ runtimeeval.SettledUsageReader = syntheticUsageProbe{}
