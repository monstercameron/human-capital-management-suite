package application

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// uxaudit017Composed builds the same full production composition
// TestTodo_PROMOUX_013_Integration uses (real PostgreSQL via pgtest, the real
// journey engine and P1B execution authority) and returns the live engine
// plus a function that authenticates any subject in any organization scope
// against it.
func uxaudit017Composed(t *testing.T, now *time.Time) (workspace.JourneyEngine, func(subject, orgScope string) context.Context) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const tenant = string(fixtures.Tenant)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: tenant, CellID: "cell-uxaudit017-integration", MaxDeadline: 30 * time.Second,
		Migrate: false, Workspace: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:uxaudit017-integration-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:uxaudit017-integration-approver",
		WorkflowPlan: WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("integration configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "uxaudit017-integration", Options: Options{Now: func() time.Time { return *now }},
	})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	activateShippedWorkflowVersions(t, pool, *now)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = composed.Stop(ctx)
	})
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(integrationSigningKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	mint := func(subject, orgScope string) context.Context {
		t.Helper()
		token, err := verifier.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: subject, SubjectKind: "human", Tenant: tenant,
			OrganizationScopeID: orgScope, Roles: []string{"intent_author", "comp_admin", "promotion_operator"},
			Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "session:" + subject, IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(48 * time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("issue credential for %s: %v", subject, err)
		}
		principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
		if err != nil {
			t.Fatalf("verify credential for %s: %v", subject, err)
		}
		return trust.WithPrincipal(context.Background(), principal)
	}
	return composed.Cell().Journey, mint
}

func uxaudit017ListedSummary(t *testing.T, ctx context.Context, journey workspace.JourneyEngine, intentID string) workspace.JourneySummary {
	t.Helper()
	listed, err := journey.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	for _, summary := range listed {
		if summary.IntentID == intentID {
			return summary
		}
	}
	t.Fatalf("ListJourneys did not return %s", intentID)
	return workspace.JourneySummary{}
}

// TestTodo_UXAUDIT_017_Integration reaches a real PostgreSQL server through
// the production composition and proves the journey list's server-side
// current-work-item summary against real durable work items: the routed
// assignee sees themself as assignee with their deadline and exactly the
// actions the work item rules grant them, and the summary is built from the
// work items the list already read (no per-row detail call is needed by the
// client). The companion TestTodo_UXAUDIT_017_Security proves the
// non-entitled half.
func TestTodo_UXAUDIT_017_Integration(t *testing.T) {
	now := mustParseRFC3339(t, "2026-01-05T09:00:00Z")
	journey, mint := uxaudit017Composed(t, &now)
	author := mint("principal:uxaudit017-author", "org-north-america")

	proposed, err := journey.Propose(author, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "uxaudit017_integration_fixture",
	})
	if err != nil {
		t.Fatalf("Journey.Propose: %v", err)
	}
	if before := uxaudit017ListedSummary(t, author, journey, proposed.IntentID); before.CurrentWorkItem != nil {
		t.Fatalf("an unexecuted journey has no work item, yet the list summarized one: %+v", before.CurrentWorkItem)
	}
	detail, err := journey.Execute(author, proposed.IntentID)
	if err != nil {
		t.Fatalf("Journey.Execute: %v", err)
	}
	var open *workitem.WorkItem
	for i := range detail.WorkItems {
		if !detail.WorkItems[i].Status.Terminal() {
			open = &detail.WorkItems[i]
		}
	}
	if open == nil {
		t.Fatalf("an executed FINANCE_APPROVAL journey carries no open work item: %+v", detail.WorkItems)
	}
	if open.OwnerKind != workitem.OwnerPrincipal {
		t.Fatalf("fixture assumption broken: the finance approval is owned by %s, not a principal", open.OwnerKind)
	}

	assignee := mint(open.OwnerRef, "org-north-america")
	summary := uxaudit017ListedSummary(t, assignee, journey, proposed.IntentID).CurrentWorkItem
	if summary == nil {
		t.Fatalf("the routed assignee %s received no current work item summary", open.OwnerRef)
	}
	if summary.AssigneePrincipalID != open.OwnerRef || summary.ViewerMembership != "ASSIGNEE" {
		t.Fatalf("assignee summary = %+v, want assignee %s with membership ASSIGNEE", summary, open.OwnerRef)
	}
	if !summary.DueAt.Equal(open.DeadlineAt) {
		t.Fatalf("DueAt = %v, want the work item's deadline %v", summary.DueAt, open.DeadlineAt)
	}
	if summary.Kind != string(open.Kind) || summary.Status != string(open.Status) {
		t.Fatalf("kind/status = %s/%s, want %s/%s", summary.Kind, summary.Status, open.Kind, open.Status)
	}
	want := []string{}
	for _, action := range workitem.PermittedActions(*open, workitem.MembershipAssignee) {
		want = append(want, string(action))
	}
	sort.Strings(want)
	if !slices.Equal(summary.ViewerPermittedActions, want) {
		t.Fatalf("ViewerPermittedActions = %v, want the work item rules' %v", summary.ViewerPermittedActions, want)
	}
}

// TestTodo_UXAUDIT_017_Security proves a viewer the work item rules do not
// make a member never learns who the item is assigned to, nor gains an
// action, through the journey list -- whether they share the item's
// organization scope (the author) or not (an outsider).
func TestTodo_UXAUDIT_017_Security(t *testing.T) {
	now := mustParseRFC3339(t, "2026-01-05T09:00:00Z")
	journey, mint := uxaudit017Composed(t, &now)
	author := mint("principal:uxaudit017-author", "org-north-america")
	proposed, err := journey.Propose(author, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "uxaudit017_security_fixture",
	})
	if err != nil {
		t.Fatalf("Journey.Propose: %v", err)
	}
	detail, err := journey.Execute(author, proposed.IntentID)
	if err != nil {
		t.Fatalf("Journey.Execute: %v", err)
	}
	owner := ""
	var openVisibility workitem.Visibility
	for _, item := range detail.WorkItems {
		if !item.Status.Terminal() {
			owner, openVisibility = item.OwnerRef, item.Visibility
		}
	}
	if owner == "" || owner == "principal:uxaudit017-author" || owner == "principal:uxaudit017-outsider" {
		t.Fatalf("fixture assumption broken: open item owner %q must be a third principal", owner)
	}

	for _, viewer := range []struct {
		name string
		ctx  context.Context
	}{
		{"author in the same organization scope", author},
		{"outsider in another organization scope", mint("principal:uxaudit017-outsider", "org-elsewhere")},
	} {
		t.Run(viewer.name, func(t *testing.T) {
			summary := uxaudit017ListedSummary(t, viewer.ctx, journey, proposed.IntentID).CurrentWorkItem
			if openVisibility == workitem.VisibilityAssigneeOnly {
				// The promotion approval is ASSIGNEE_ONLY: a non-member is not
				// admitted to the item at all, so even its existence, status and
				// deadline must be absent -- not merely the assignee.
				if summary != nil {
					t.Fatalf("a non-member received a summary of an ASSIGNEE_ONLY item: %+v", summary)
				}
				return
			}
			if summary == nil {
				return
			}
			if summary.AssigneePrincipalID != "" || summary.AssigneeDisplayName != "" {
				t.Fatalf("a non-member learned the assignee: %+v", summary)
			}
			if summary.ViewerMembership != "NONE" || len(summary.ViewerPermittedActions) != 0 {
				t.Fatalf("a non-member was granted standing or actions: %+v", summary)
			}
		})
	}
}
