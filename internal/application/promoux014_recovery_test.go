//go:build devtools

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/devclock"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// TestTodo_PROMOUX_014_Recovery is PROMOUX-014's RECOVERY matrix test, and
// the one that proves the driver half of GREEN: "a separately fenced
// local-dev clock/driver can advance a same-day fixture through END."
//
// It is built only with `-tags devtools` (`go test -tags devtools
// ./internal/application/...`) because the capability it exercises,
// internal/platform/devclock.Driver.Clock, does not exist in the default
// build at all -- see that package's own doc and
// TestTodo_PROMOUX_014_Security, which runs in the default build and
// proves the opposite half: that this same call is inert there.
//
// The fixture's effective date is real tomorrow (two days out, to absorb
// timezone and DST rounding), so the WAIT node's own resolved instant is
// genuinely in the future relative to the real host clock when this test
// runs -- proving the fixture would still be waiting without intervention
// -- and only advances through END once the scheduler's own clock is read
// through the fenced driver. Nothing about how a fired timer resumes is
// touched or re-implemented here: firing and resuming are the identical
// internal/platform/execution/scheduler.Scheduler.Tick /
// internal/intent/app.Cell.ResumeFiredTimer production path
// internal/application/scheduler_workload.go composes for the real server,
// with the driver supplying only how far ahead the clock it reads may run.
func TestTodo_PROMOUX_014_Recovery(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const tenant = string(fixtures.Tenant)
	const subject = "principal:promoux014-recovery"
	const approver = "principal:promoux014-approver"
	now := time.Now().UTC()
	// Two days out, not one: a fixture proposed late in the local day would
	// otherwise sometimes resolve to a "tomorrow" wake instant only hours
	// away, which a slow CI box could occasionally race past the recovery
	// clock's own scan latency. Two days keeps the "still waiting without
	// the driver" assertion below robust without loosening the driver's own
	// behavior.
	effectiveDate := now.AddDate(0, 0, 2).Format("2006-01-02")

	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: tenant, CellID: "cell-promoux014-recovery", MaxDeadline: 30 * time.Second,
		Migrate: false, Workspace: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:promoux014-recovery-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: approver,
		WorkflowPlan: WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("recovery configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "promoux014-recovery", Options: Options{Now: func() time.Time { return now }},
	})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
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
		SessionRef: "session:promoux014-recovery", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue journey credential: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verify journey credential: %v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	journey := composed.Cell().Journey

	proposed, err := journey.Propose(ctx, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: effectiveDate, BusinessReason: "promoux014_recovery_fixture",
	})
	if err != nil {
		t.Fatalf("Journey.Propose: %v", err)
	}
	if _, err := journey.Execute(ctx, proposed.IntentID); err != nil {
		t.Fatalf("Journey.Execute: %v", err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := journey.Decide(ctx, proposed.IntentID, workspace.Decision{Approve: true, Reason: "finance approved"}); err != nil {
		t.Fatalf("Journey.Decide(finance): %v", err)
	}
	now = now.Add(10 * time.Minute)
	waiting, err := journey.Decide(ctx, proposed.IntentID, workspace.Decision{Approve: true, Reason: "manager approved"})
	if err != nil {
		t.Fatalf("Journey.Decide(manager): %v", err)
	}
	if waiting.Summary.Stage != workspace.JourneyStage("WAITING_EFFECTIVE_DATE") {
		t.Fatalf("after manager stage = %s, want WAITING_EFFECTIVE_DATE", waiting.Summary.Stage)
	}
	// This is what PROMOUX-014's RED reproduced: the fixture is genuinely
	// stuck without the driver. See the same explanation the reader gets
	// (PROMOUX-014's other half) via waiting.Findings.
	instant, tz, action, checks, notify, intervene := findFinding(waiting.Findings)
	for label, got := range map[string]string{
		"effective instant": instant, "timezone": tz, "scheduled action": action,
		"remaining checks": checks, "notification": notify, "authorized intervention": intervene,
	} {
		if got == "" {
			t.Fatalf("waiting journey explains no %s; PROMOUX-014's RED reproduced", label)
		}
	}

	// --- Without the driver, a real tick still finds nothing due: the
	//     fixture is genuinely waiting, not accidentally already overdue. ---
	tenantID := pgstore.TenantID(tenant)
	realTick, err := newRecoveryScheduler(t, pool, tenantID, composed, func() time.Time { return time.Now().UTC() })
	if err != nil {
		t.Fatalf("build the real-clock scheduler: %v", err)
	}
	beforeResult, err := realTick.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (real clock): %v", err)
	}
	if beforeResult.Fired != 0 {
		t.Fatalf("Tick (real clock) fired %d timers, want 0: the fixture must still be waiting", beforeResult.Fired)
	}

	// --- With the fenced driver's clock, the same fixture advances through
	//     END, durably: re-Inspect-ing after the tick reads the recorded
	//     stage back from PostgreSQL, not from anything held in memory. ---
	driver, err := devclock.New(devprofile.Name)
	if err != nil {
		t.Fatalf("devclock.New(%q): %v (this test must run with -tags devtools)", devprofile.Name, err)
	}
	drivenClock, err := driver.Clock(96 * time.Hour)
	if err != nil {
		t.Fatalf("driver.Clock: %v", err)
	}
	drivenTick, err := newRecoveryScheduler(t, pool, tenantID, composed, drivenClock)
	if err != nil {
		t.Fatalf("build the driven scheduler: %v", err)
	}
	afterResult, err := drivenTick.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (driven clock): %v", err)
	}
	if afterResult.Fired != 1 {
		t.Fatalf("Tick (driven clock) fired %d timers, want exactly 1", afterResult.Fired)
	}

	completed, err := journey.Inspect(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Journey.Inspect(completed): %v", err)
	}
	if completed.Summary.Stage != workspace.JourneyStage("RECORDED") || completed.Ledger == nil {
		t.Fatalf("completed detail = stage %s ledger %+v, want RECORDED with a ledger fact", completed.Summary.Stage, completed.Ledger)
	}
}

// findFinding pulls PROMOUX-014's six explanation facts out of a
// []workspace.JourneyFinding by code, for the assertion above. It returns
// "" for any code not present, exactly like a caller reading the wire would
// see.
func findFinding(findings []workspace.JourneyFinding) (instant, timezone, action, checks, notify, intervene string) {
	byCode := make(map[string]string, len(findings))
	for _, f := range findings {
		byCode[f.Code] = f.Message
	}
	// WAIT_EFFECTIVE_INSTANT carries both the instant and the timezone name
	// in one sentence (app.journeyWaitFindings), so both return slots read
	// the same finding; either being absent fails the same way.
	instant = byCode["WAIT_EFFECTIVE_INSTANT"]
	timezone = instant
	action = byCode["WAIT_SCHEDULED_ACTION"]
	checks = byCode["WAIT_REMAINING_CHECKS"]
	notify = byCode["WAIT_NOTIFICATION"]
	intervene = byCode["WAIT_INTERVENTION"]
	return
}

// newRecoveryScheduler composes the identical production scheduler
// internal/application/scheduler_workload.go builds for the real server,
// except for the Clock it is handed -- the one seam this test (and
// devclock) exists to control.
func newRecoveryScheduler(t *testing.T, pool *pgxadapter.Pool, tenantID uuid.UUID, composed *App, clock func() time.Time) (*scheduler.Scheduler, error) {
	t.Helper()
	dispatcher := scheduler.DispatcherFunc(func(dispatchCtx context.Context, work scheduler.Work) (scheduler.Disposition, error) {
		_, resumeErr := composed.Cell().ResumeFiredTimer(app.WithResumeTenant(dispatchCtx, string(fixtures.Tenant)), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt)
		if resumeErr != nil {
			return scheduler.DispositionRetry, resumeErr
		}
		return scheduler.DispositionCompleted, nil
	})
	return scheduler.New(scheduler.Config{
		DB: pool,
		Claims: []lease.AcquireRequest{{
			TenantID: tenantID, Resource: lease.Resource{Kind: lease.ResourceQueue, ID: scheduler.DefaultQueueKey},
			Holder: lease.Identity{WorkloadRef: "workload:promoux014-recovery", InstanceRef: "replica-1"},
		}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}},
		Misfire:    schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour, MaxCatchUp: 1},
		Dispatcher: dispatcher, Clock: clock,
	})
}
