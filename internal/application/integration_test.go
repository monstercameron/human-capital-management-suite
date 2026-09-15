package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"net/http"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// integrationSigningKey is a fixed test key. It never leaves this package and
// it authenticates nothing outside a test process.
const integrationSigningKey = "hcm-next-application-composition-signing-key"

// TestTodo_ARCH_GO_020_Integration is the ARCH-GO-020 INTEGRATION test: the
// composition root composed against a real PostgreSQL, with a governed read
// run through the cell it composed.
//
// Nothing here is stubbed. The store is the PostgreSQL adapter the binary
// composes, the verifier is the HMAC verifier it composes, the listeners are
// real sockets and the two transports are the ones internal/transport/cell
// publishes. What the test then checks is the three things the composition
// actually claims: the configured tenant reached the database, the governed
// capability gateway answered a read and recorded evidence for it, and the
// HTTP edge the same composition published serves the discovery document.
func TestTodo_ARCH_GO_020_Integration(t *testing.T) {
	db := pgtest.New(t)

	// The migration tree is schema-relative; pinning search_path on every
	// pooled connection is what keeps this composition inside its own schema.
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	cfg := ServeConfig{
		GRPCListen:              "127.0.0.1:0",
		HTTPListen:              "127.0.0.1:0",
		DatabaseURL:             db.URL,
		DevHMACKey:              integrationSigningKey,
		Issuer:                  DefaultIssuer,
		Audience:                DefaultAudience,
		Tenant:                  string(fixtures.Tenant),
		CellID:                  "cell-application-integration",
		MaxDeadline:             30 * time.Second,
		Migrate:                 false, // pgtest already brought the schema to head
		Workspace:               true,
		OTelExporter:            OTelExporterNone,
		LegalEvidenceIssuerKeys: base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)),
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the integration configuration is not one a listener may start on: %v", err)
	}

	logger := &recordingLogger{}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config:   cfg,
		Pool:     pool,
		Logger:   logger,
		Identity: "instance-application-integration",
	})
	if err != nil {
		t.Fatalf("ComposeServe against pgtest: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	// The composition built the production adapters, not a test double.
	store, ok := composed.Graph().Component(ComponentIntentStore)
	if !ok || store.Impl != "*pgstore.Store" {
		t.Fatalf("the composed store is %+v, want the PostgreSQL adapter", store)
	}
	legalVerifier, ok := composed.Graph().Component(ComponentLegalEvidenceVerifier)
	if !ok || legalVerifier.Impl != "*application.legalEvidenceAdapter" {
		t.Fatalf("the composed legal verifier is %+v, want the real application adapter", legalVerifier)
	}

	// 1. The configured tenant reached the database through the composed
	//    store and the composed pool.
	if !logger.saw("hcmnext.tenant_registered") {
		t.Error("the composition did not report registering the configured tenant")
	}
	var registered int
	tenantID := pgstore.TenantID(string(fixtures.Tenant))
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM tenant WHERE tenant_id = $1`, tenantID).Scan(&registered); err != nil {
		t.Fatalf("read the tenant row the composition wrote: %v", err)
	}
	if registered != 1 {
		t.Fatalf("the composed store wrote %d tenant rows for %s, want 1", registered, fixtures.Tenant)
	}

	// 2. A governed read through the composed cell: the composed capability
	//    registry resolves the capability, the composed gateway enforces the
	//    authorization and the effect class, the composed domain handler
	//    answers from the composed read port, and the composed evidence sink
	//    records it.
	cell := composed.Cell()
	before := cell.Evidence.Len()
	result, err := cell.Gateway.Invoke(context.Background(), governedWorkerRead(t, cell))
	if err != nil {
		t.Fatalf("governed read through the composed gateway: %v", err)
	}
	if result.EvidenceID == "" {
		t.Error("the governed read recorded no evidence reference")
	}
	explanation, ok := result.Response.(people.Explanation)
	if !ok {
		t.Fatalf("the governed read answered %T, want a people.Explanation", result.Response)
	}
	if explanation.Worker.Id == "" {
		t.Error("the explanation names no worker; the composed read port answered nothing")
	}
	if cell.Evidence.Len() <= before {
		t.Errorf("the evidence sink holds %d records, was %d: the governed read left no chronology",
			cell.Evidence.Len(), before)
	}

	// 3. The HTTP edge this same composition published is the one serving.
	if err := composed.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	url := "http://" + composed.HTTPAddr() + app.DiscoveryPath
	var resp *http.Response
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Get(url)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("close body: %v", closeErr)
	}
	if resp.StatusCode >= 500 {
		t.Errorf("GET %s = %d; the composed edge did not answer", url, resp.StatusCode)
	}
}

func TestTodo_PROMO_EXEC_SERVE_ExecutePlanJourneyOverPGTest(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	const tenant = string(fixtures.Tenant)
	const subject = "principal:application-execute-plan"
	const approver = "principal:promotion-approver"
	const managerApprover = "principal:promotion-manager-approver"
	const at = "2026-09-03T12:00:00Z"
	clockAt, _ := time.Parse(time.RFC3339, at)
	now := clockAt
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: tenant, CellID: "cell-application-execute-plan", MaxDeadline: 30 * time.Second,
		Migrate: false, Workspace: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:application-execute-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: approver,
		ExecutionManagerApprover: managerApprover,
		WorkflowPlan:             WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("execute-plan configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: "application-execute-plan", Options: Options{Now: func() time.Time { return now }},
	})
	if err != nil {
		t.Fatalf("ComposeServe(execute plan): %v", err)
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
	contextFor := func(subjectID, session string) context.Context {
		t.Helper()
		token, issueErr := verifier.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: subjectID, SubjectKind: "human", Tenant: tenant,
			OrganizationScopeID: "org-north-america", Roles: []string{"intent_author", "comp_admin", "promotion_operator"},
			Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: session, IssuedAtUnix: clockAt.Add(-time.Minute).Unix(), ExpiresAtUnix: clockAt.Add(time.Hour).Unix(),
		})
		if issueErr != nil {
			t.Fatalf("issue %s journey credential: %v", subjectID, issueErr)
		}
		principal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify %s journey credential: %v", subjectID, verifyErr)
		}
		return trust.WithPrincipal(context.Background(), principal)
	}
	ctx := contextFor(subject, "session:application-execute-plan")
	financeCtx, managerCtx := routedApproverContexts(t, verifier, cfg, clockAt)
	journey := composed.Cell().Journey
	// TargetPositionID is deliberately absent: PROMOUX-004 checks a
	// non-empty value against the real Position domain, and this
	// environment has no job_position row for any corpus fixture.
	proposed, err := journey.Propose(ctx, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "promotion_into_senior_hrbp",
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
	tenantID := pgstore.TenantID(tenant)
	var originalPayload []byte
	var originalProducedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT payload,produced_at FROM proposal_revision WHERE tenant_id=$1 AND intent_id=$2 AND revision=1`, tenantID, proposed.IntentID).Scan(&originalPayload, &originalProducedAt); err != nil {
		t.Fatalf("load original proposal snapshot: %v", err)
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
	var replayedPayload []byte
	var replayedProducedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT payload,produced_at FROM proposal_revision WHERE tenant_id=$1 AND intent_id=$2 AND revision=1`, tenantID, proposed.IntentID).Scan(&replayedPayload, &replayedProducedAt); err != nil {
		t.Fatalf("reload immutable proposal snapshot: %v", err)
	}
	if !bytes.Equal(replayedPayload, originalPayload) || !replayedProducedAt.Equal(originalProducedAt) {
		t.Fatalf("resimulation rewrote proposal snapshot: produced_at %s -> %s", originalProducedAt, replayedProducedAt)
	}
	now = now.Add(10 * time.Minute)
	runner, err := scheduler.New(scheduler.Config{
		DB: pool, Claims: []lease.AcquireRequest{{TenantID: tenantID, Resource: lease.Resource{Kind: lease.ResourceQueue, ID: scheduler.DefaultQueueKey}, Holder: lease.Identity{WorkloadRef: "workload:application-test", InstanceRef: "application-execute-plan"}}},
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
	// WF-RUN-034: revalidation now runs for real. No durable GOVERN-002
	// historical decision exists to confirm against, so still_valid routes
	// BLOCKED and the run closes PROMOTION_BLOCKED with its one ledger fact,
	// rather than the fabricated VALID -> COMPLETE it used to record.
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

// governedWorkerRead builds one explain_worker_state invocation against the
// capability the composed registry actually published, including the exact
// AuthZ scope that capability declares. Reading the scope back off the
// registry rather than writing a literal is deliberate: a test that invented
// its own scope would pass while the published one drifted.
func governedWorkerRead(t *testing.T, cell *app.Cell) capability.InvokeRequest {
	t.Helper()
	key := capability.Key{ID: people.ExplainWorkerStateIntentType, Version: 1}
	record, found := cell.Capabilities.Lookup(key)
	if !found {
		t.Fatalf("the composed capability registry does not publish %s", key)
	}

	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("resolve the corpus worker: %v", err)
	}
	effective, err := values.NewLocalDate(2026, time.June, 1)
	if err != nil {
		t.Fatalf("effective date: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("known-at: %v", err)
	}
	fields := []people.FieldID{people.FieldWorkerNumber, people.FieldLifecycleStatus}
	rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
	for _, field := range fields {
		rulings[field] = people.FieldRuling{Effect: people.EffectAllow}
	}

	return capability.InvokeRequest{
		Capability: key,
		Payload: people.ExplainWorkerStateRequest{
			Tenant: fixtures.Tenant,
			Worker: worker,
			AsOf:   people.AsOf{EffectiveOn: effective, KnownAt: knownAt},
			Fields: fields,
			Authorization: people.AuthorizationDecision{
				PolicyVersion:      app.AuthorizationPolicyVersion,
				Purpose:            "compensation_review",
				SubjectDisclosable: true,
				Fields:             rulings,
			},
		},
		Authorization: capability.Authorization{
			Decision:   capability.Allow,
			Scopes:     []string{record.Definition.AuthZScopeRef},
			SubjectRef: "principal:application-integration",
		},
	}
}

type diagGrant struct{}

func (diagGrant) AllowsInternalDiagnostics() bool { return true }
