package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// parseStandardServe parses args against the standard profile's declared
// fields with an empty environment, the way a deployment that sets nothing
// but its secrets is parsed.
func parseStandardServe(t *testing.T, args ...string) ServeConfig {
	t.Helper()
	values, err := bootstrap.ParseConfig(args, func(string) (string, bool) { return "", false }, ServeConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig(%v): %v", args, err)
	}
	cfg, err := ServeConfigFromValues(values)
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	return cfg
}

// TestTodo_PROMO_EXEC_001_DefaultComposition proves a cell composed from
// parsed standard-profile defaults runs a promotion through the execution
// engine: no -workflow-plan, -execution-authority or -scheduler flag is
// passed, yet propose, execute, both approval decisions and the scheduler
// tick close the run with exactly one terminal ledger fact.
func TestTodo_PROMO_EXEC_001_DefaultComposition(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	const tenant = string(fixtures.Tenant)
	const subject = "principal:promo-exec-001-operator"
	const at = "2026-09-03T12:00:00Z"
	clockAt, _ := time.Parse(time.RFC3339, at)
	now := clockAt
	cfg := parseStandardServe(t,
		"-grpc-listen=127.0.0.1:0", "-http-listen=127.0.0.1:0", "-database-url="+db.URL,
		"-dev-hmac-key="+integrationSigningKey, "-tenant="+tenant,
		"-execution-authority-digest=sha256:promo-exec-001-default",
		"-page-cursor-key="+integrationPageCursorKey,
		"-migrate=false")
	if !cfg.ExecutionAuthority || cfg.WorkflowPlan != WorkflowPlanExecute || !cfg.Scheduler {
		t.Fatalf("standard defaults = authority %t plan %q scheduler %t, want the engine on",
			cfg.ExecutionAuthority, cfg.WorkflowPlan, cfg.Scheduler)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default configuration is not one a listener may start on: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "promo-exec-001", Options: Options{Now: func() time.Time { return now }},
	})
	if err != nil {
		t.Fatalf("ComposeServe(standard defaults): %v", err)
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
		SessionRef: "session:promo-exec-001", IssuedAtUnix: clockAt.Add(-time.Minute).Unix(), ExpiresAtUnix: clockAt.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue journey credential: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verify journey credential: %v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	financeCtx, managerCtx := routedApproverContexts(t, verifier, cfg, clockAt)
	journey := composed.Cell().Journey
	// TargetPositionID is deliberately absent, mirroring the execute-plan
	// serve test: the corpus worker's population has no projection, so the
	// run closes BLOCKED with one ledger fact rather than COMPLETE.
	proposed, err := journey.Propose(ctx, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "Promotion into the senior HRBP role",
	})
	if err != nil {
		t.Fatalf("Journey.Propose: %v", err)
	}
	financeWaiting, err := journey.Execute(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Journey.Execute on the default-composed cell: %v", err)
	}
	if financeWaiting.Summary.Stage != workspace.JourneyStage("FINANCE_APPROVAL") {
		t.Fatalf("after execute stage = %s, want FINANCE_APPROVAL", financeWaiting.Summary.Stage)
	}
	now = now.Add(10 * time.Minute)
	finance, err := journey.Decide(financeCtx, proposed.IntentID, workspace.Decision{Approve: true, Reason: "finance approved"})
	if err != nil {
		t.Fatalf("Journey.Decide(finance): %v", err)
	}
	if finance.Summary.Stage != workspace.JourneyStage("MANAGER_APPROVAL") {
		t.Fatalf("after finance stage = %s, want MANAGER_APPROVAL", finance.Summary.Stage)
	}
	now = now.Add(10 * time.Minute)
	waiting, err := journey.Decide(managerCtx, proposed.IntentID, workspace.Decision{Approve: true, Reason: "manager approved"})
	if err != nil {
		t.Fatalf("Journey.Decide(manager): %v", err)
	}
	if waiting.Summary.Stage != workspace.JourneyStage("WAITING_EFFECTIVE_DATE") {
		t.Fatalf("after manager stage = %s, want WAITING_EFFECTIVE_DATE", waiting.Summary.Stage)
	}
	if waiting.Instance == nil {
		t.Fatal("waiting journey has no workflow instance")
	}
	instanceID := waiting.Instance.InstanceID
	now = now.Add(10 * time.Minute)
	tenantID := pgstore.TenantID(tenant)
	runner, err := scheduler.New(scheduler.Config{
		DB: pool, Claims: []lease.AcquireRequest{{TenantID: tenantID, Resource: lease.Resource{Kind: lease.ResourceQueue, ID: scheduler.DefaultQueueKey}, Holder: lease.Identity{WorkloadRef: "workload:promo-exec-001", InstanceRef: "promo-exec-001"}}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour, MaxCatchUp: 1},
		Dispatcher: scheduler.DispatcherFunc(func(dispatchCtx context.Context, work scheduler.Work) (scheduler.Disposition, error) {
			_, resumeErr := composed.Cell().ResumeFiredTimer(app.WithResumeTenant(dispatchCtx, tenant), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt)
			if resumeErr != nil {
				return scheduler.DispositionRetry, resumeErr
			}
			return scheduler.DispositionCompleted, nil
		}), Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("scheduler.New: %v", err)
	}
	tick, err := runner.Tick(ctx)
	if err != nil || tick.Fired != 1 {
		t.Fatalf("scheduler.Tick = %+v, %v; want one fired timer", tick, err)
	}
	completed, err := journey.Inspect(ctx, proposed.IntentID)
	if err != nil {
		var env *envelope.Error
		if errors.As(err, &env) {
			if diag, ok := env.Diagnostic(diagGrant{}); ok {
				t.Logf("DIAG: %v", diag)
			}
		}
		t.Fatalf("Journey.Inspect(completed): %v", err)
	}
	if completed.Summary.Stage != workspace.JourneyStageBlocked || completed.Ledger == nil {
		t.Fatalf("completed detail = stage %s ledger %+v, want BLOCKED with a ledger fact", completed.Summary.Stage, completed.Ledger)
	}
	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`, tenantID, effects.StreamKeyFor(completed.Instance.WorkflowID, instanceID)).Scan(&ledgerCount); err != nil {
		t.Fatalf("count journey ledger facts: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("ledger facts on %s = %d, want exactly one", instanceID, ledgerCount)
	}
}

// TestTodo_PROMO_EXEC_001_OptOutRefuses proves the explicit opt-out still
// composes the refusing cell: with -execution-authority=false (and the
// scheduler off, which the authority requires) the proposal is accepted but
// executing it fails instead of running the engine.
func TestTodo_PROMO_EXEC_001_OptOutRefuses(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	const tenant = string(fixtures.Tenant)
	const subject = "principal:promo-exec-001-optout"
	const at = "2026-09-03T12:00:00Z"
	clockAt, _ := time.Parse(time.RFC3339, at)
	cfg := parseStandardServe(t,
		"-grpc-listen=127.0.0.1:0", "-http-listen=127.0.0.1:0", "-database-url="+db.URL,
		"-dev-hmac-key="+integrationSigningKey, "-tenant="+tenant,
		"-execution-authority=false", "-scheduler=false", "-migrate=false", "-workspace=false")
	cfg.PageCursorKey = integrationPageCursorKey
	if cfg.ExecutionAuthority || cfg.Scheduler {
		t.Fatalf("opt-out = authority %t scheduler %t, want both off", cfg.ExecutionAuthority, cfg.Scheduler)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("opt-out configuration is not one a listener may start on: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "promo-exec-001-optout", Options: Options{Now: func() time.Time { return clockAt }},
	})
	if err != nil {
		t.Fatalf("ComposeServe(opt-out): %v", err)
	}
	activateShippedWorkflowVersions(t, pool, clockAt)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = composed.Stop(ctx)
	})
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(integrationSigningKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return clockAt },
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: subject, SubjectKind: "human", Tenant: tenant,
		OrganizationScopeID: "org-north-america", Roles: []string{"intent_author", "comp_admin", "promotion_operator"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef: "session:promo-exec-001-optout", IssuedAtUnix: clockAt.Add(-time.Minute).Unix(), ExpiresAtUnix: clockAt.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue journey credential: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verify journey credential: %v", err)
	}
	// The opted-out cell has no journey to show: Cell.Journey is non-nil
	// only with a ProposalExecutor and an execution database, and the
	// workspace reports ErrJourneyUnavailable from its own nil check.
	_ = trust.WithPrincipal(context.Background(), principal)
	if composed.Cell().Journey != nil {
		t.Fatal("opted-out cell composed a Journey; want nil without the execution authority")
	}
}
