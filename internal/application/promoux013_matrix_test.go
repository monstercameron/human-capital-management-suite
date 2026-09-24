package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PROMOUX_013_Mutation sends a stale edit after a valid edit has
// cancelled the original proposal. Refusal must leave the successor list
// unchanged, so a stale write cannot create a dangling successor record.
func TestTodo_PROMOUX_013_Mutation(t *testing.T) {
	now := mustParseRFC3339(t, "2026-01-05T09:00:00Z")
	ctx, journey, _ := promoux013Composed(t, &now)
	proposed := promoux013Propose(t, ctx, journey, "2026-06-20")
	successor, _, err := journey.EditProposal(ctx, proposed.IntentID, proposed.GovernanceVersion,
		"idem:promoux013:mutation:valid", "first authorized correction", workspace.EditProposalInput{
			TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "99000.00",
			EffectiveDate: "2026-06-20", BusinessReason: "valid correction",
		})
	if err != nil {
		t.Fatalf("valid EditProposal: %v", err)
	}
	if successor.IntentID == proposed.IntentID || successor.Stage != workspace.JourneyStageProposed {
		t.Fatalf("valid edit returned successor %+v for original %s", successor, proposed.IntentID)
	}

	before, err := journey.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys before stale edit: %v", err)
	}
	if _, _, err := journey.EditProposal(ctx, proposed.IntentID, proposed.GovernanceVersion,
		"idem:promoux013:mutation:stale", "stale correction", workspace.EditProposalInput{
			TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "101000.00",
			EffectiveDate: "2026-06-20", BusinessReason: "stale correction must be refused",
		}); err == nil {
		t.Fatal("EditProposal accepted the original proposal's stale governance version")
	}
	after, err := journey.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys after stale edit: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("stale edit changed journey count from %d to %d; a partial successor write occurred", len(before), len(after))
	}
}

// TestTodo_PROMOUX_013_Recovery stops and recomposes the journey service over
// the same database after an edit. The cancelled original and fresh proposed
// successor must both survive process-local state loss with their identities
// and stages intact.
func TestTodo_PROMOUX_013_Recovery(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	now := mustParseRFC3339(t, "2026-01-05T09:00:00Z")
	const tenant = string(fixtures.Tenant)
	const subject = "principal:promoux013-recovery"
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, PageCursorKey: integrationPageCursorKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: tenant, CellID: "cell-promoux013-recovery", MaxDeadline: 30 * time.Second,
		Migrate: false, Workspace: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:promoux013-recovery-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promoux013-recovery-approver",
		ExecutionManagerApprover: "principal:promoux013-recovery-manager",
		WorkflowPlan:             WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("recovery configuration: %v", err)
	}
	compose := func(identity string) *App {
		t.Helper()
		composed, err := ComposeServe(context.Background(), ServeInput{
			Config: cfg, Pool: pool, Identity: identity, Options: Options{Now: func() time.Time { return now }},
		})
		if err != nil {
			t.Fatalf("ComposeServe(%s): %v", identity, err)
		}
		return composed
	}
	first := compose("promoux013-recovery-first")
	stopped := map[*App]bool{}
	stop := func(composed *App) {
		t.Helper()
		if stopped[composed] {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Fatalf("stop composed journey service: %v", err)
		}
		stopped[composed] = true
	}
	t.Cleanup(func() { stop(first) })
	activateShippedWorkflowVersions(t, pool, now)
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(integrationSigningKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: subject, SubjectKind: "human", Tenant: tenant,
		OrganizationScopeID: "org-north-america", Roles: []string{"intent_author", "comp_admin", "promotion_operator"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef: "session:promoux013-recovery", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(48 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue recovery credential: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verify recovery credential: %v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	journey := first.Cell().Journey
	proposed := promoux013Propose(t, ctx, journey, "2026-06-20")
	successor, superseded, err := journey.EditProposal(ctx, proposed.IntentID, proposed.GovernanceVersion,
		"idem:promoux013:recovery:edit", "recovery correction", workspace.EditProposalInput{
			TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "99000.00",
			EffectiveDate: "2026-06-20", BusinessReason: "recovery fixture correction",
		})
	if err != nil {
		t.Fatalf("EditProposal before restart: %v", err)
	}
	if superseded != proposed.IntentID || successor.IntentID == proposed.IntentID {
		t.Fatalf("edited identities successor=%s superseded=%s original=%s", successor.IntentID, superseded, proposed.IntentID)
	}
	stop(first)

	second := compose("promoux013-recovery-restarted")
	t.Cleanup(func() { stop(second) })
	recovered, err := second.Cell().Journey.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys after recomposition: %v", err)
	}
	byID := make(map[string]workspace.JourneySummary, len(recovered))
	for _, item := range recovered {
		byID[item.IntentID] = item
	}
	original, ok := byID[proposed.IntentID]
	if !ok || original.Stage != workspace.JourneyStage("CANCELLED") {
		t.Fatalf("recovered original = %+v, present=%v; want durable CANCELLED original", original, ok)
	}
	restoredSuccessor, ok := byID[successor.IntentID]
	if !ok || restoredSuccessor.Stage != workspace.JourneyStageProposed || restoredSuccessor.ProposedBase != "99000.00" {
		t.Fatalf("recovered successor = %+v, present=%v; want fresh PROPOSED revision at 99000.00", restoredSuccessor, ok)
	}
}
