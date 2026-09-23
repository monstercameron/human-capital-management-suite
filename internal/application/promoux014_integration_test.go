package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PROMOUX_014_Integration is the INTEGRATION matrix test: it
// reaches a real PostgreSQL server, drives one promotion journey through
// both approvals via the composed cell exactly as
// TestTodo_PROMO_EXEC_SERVE_ExecutePlanJourneyOverPGTest does, and checks
// what journeyEngine.Inspect actually attached to the durable
// WAITING_EFFECTIVE_DATE journey's Findings -- the wire path a real reader
// of /workspace/journey goes through, not a table of Go values.
//
// It also proves the regression boundary the other direction: a journey at
// an earlier stage (FINANCE_APPROVAL, still short of the wait) carries none
// of the six WAIT_* codes, so the explanation cannot leak onto a stage it
// does not describe.
func TestTodo_PROMOUX_014_Integration(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const tenant = string(fixtures.Tenant)
	const subject = "principal:promoux014-integration"
	const approver = "principal:promoux014-integration-approver"
	at, _ := time.Parse(time.RFC3339, "2026-01-05T09:00:00Z")
	now := at
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, PageCursorKey: integrationPageCursorKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: tenant, CellID: "cell-promoux014-integration", MaxDeadline: 30 * time.Second,
		Migrate: false, Workspace: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:promoux014-integration-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: approver,
		ExecutionManagerApprover: approver + "-manager",
		WorkflowPlan:             WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("integration configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "promoux014-integration", Options: Options{Now: func() time.Time { return now }},
	})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	activateShippedWorkflowVersions(t, pool, now)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = composed.Stop(ctx)
	})

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
		SessionRef: "session:promoux014-integration", IssuedAtUnix: at.Add(-time.Minute).Unix(), ExpiresAtUnix: at.Add(48 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue journey credential: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verify journey credential: %v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	// PROMOUX-015: each approval is decided by its routed assignee.
	financeCtx, managerCtx := routedApproverContexts(t, verifier, cfg, at)
	journey := composed.Cell().Journey

	proposed, err := journey.Propose(ctx, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "promoux014 integration fixture",
	})
	if err != nil {
		t.Fatalf("Journey.Propose: %v", err)
	}
	financeWaiting, err := journey.Execute(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Journey.Execute: %v", err)
	}
	if financeWaiting.Summary.Stage != workspace.JourneyStage("FINANCE_APPROVAL") {
		t.Fatalf("after execute stage = %s, want FINANCE_APPROVAL", financeWaiting.Summary.Stage)
	}
	if got := waitCodesOf(financeWaiting.Findings); len(got) != 0 {
		t.Fatalf("FINANCE_APPROVAL journey already carries wait-explanation codes %v; the explanation leaked onto the wrong stage", got)
	}

	now = now.Add(10 * time.Minute)
	finance, err := journey.Decide(financeCtx, proposed.IntentID, workspace.Decision{Approve: true, Reason: "finance approved"})
	if err != nil {
		t.Fatalf("Journey.Decide(finance): %v", err)
	}
	if got := waitCodesOf(finance.Findings); len(got) != 0 {
		t.Fatalf("MANAGER_APPROVAL journey already carries wait-explanation codes %v", got)
	}

	now = now.Add(10 * time.Minute)
	waiting, err := journey.Decide(managerCtx, proposed.IntentID, workspace.Decision{Approve: true, Reason: "manager approved"})
	if err != nil {
		t.Fatalf("Journey.Decide(manager): %v", err)
	}
	if waiting.Summary.Stage != workspace.JourneyStage("WAITING_EFFECTIVE_DATE") {
		t.Fatalf("after manager stage = %s, want WAITING_EFFECTIVE_DATE", waiting.Summary.Stage)
	}

	byCode := make(map[string]string, len(waiting.Findings))
	for _, f := range waiting.Findings {
		byCode[f.Code] = f.Message
	}
	wantCodes := []string{
		"WAIT_EFFECTIVE_INSTANT", "WAIT_OWNER", "WAIT_SCHEDULED_ACTION",
		"WAIT_REMAINING_CHECKS", "WAIT_NOTIFICATION", "WAIT_INTERVENTION",
	}
	for _, code := range wantCodes {
		msg, ok := byCode[code]
		if !ok || msg == "" {
			t.Errorf("WAITING_EFFECTIVE_DATE journey carries no non-empty %s finding", code)
		}
	}
	// The effective date was proposed as 2026-06-01; June 1 is Eastern
	// Daylight Time (UTC-4), so the wait node's own declared zone
	// (America/New_York) resolves the start of that local day to
	// 2026-06-01T04:00:00Z. Asserting the exact instant, not merely that
	// one is present, is what proves this is read from the engine's real
	// timer rather than a plausible-looking guess.
	if instant := byCode["WAIT_EFFECTIVE_INSTANT"]; !strings.Contains(instant, "2026-06-01T04:00:00Z") {
		t.Errorf("WAIT_EFFECTIVE_INSTANT = %q, want the resolved instant 2026-06-01T04:00:00Z", instant)
	}
	if instant := byCode["WAIT_EFFECTIVE_INSTANT"]; !strings.Contains(instant, "America/New_York") {
		t.Errorf("WAIT_EFFECTIVE_INSTANT = %q, want the declared zone America/New_York", instant)
	}
	// The approver every Decide call in this composition claims and
	// completes as is cfg.ExecutionApprover.
	if owner := byCode["WAIT_OWNER"]; !strings.Contains(owner, approver) {
		t.Errorf("WAIT_OWNER = %q, want it to name the approver %s", owner, approver)
	}
}

func waitCodesOf(findings []workspace.JourneyFinding) []string {
	var out []string
	for _, f := range findings {
		if strings.HasPrefix(f.Code, "WAIT_") {
			out = append(out, f.Code)
		}
	}
	return out
}
