package execute_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/messaging"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type promotionVersionStore struct{ plan *workflow.CompiledWorkflow }

func (s promotionVersionStore) Put(version.CompiledVersion) error { return nil }
func (s promotionVersionStore) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || digest != s.plan.Digest() {
		return version.CompiledVersion{}, false, nil
	}
	return version.CompiledVersion{WorkflowID: s.plan.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: digest, Status: version.StatusActive}, true, nil
}
func (s promotionVersionStore) GetActiveForWorkflow(id string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || id != s.plan.WorkflowID {
		return version.CompiledVersion{}, false, nil
	}
	return s.GetByDigest(s.plan.Digest())
}
func (s promotionVersionStore) List(id string) ([]version.CompiledVersion, error) {
	v, ok, err := s.GetActiveForWorkflow(id)
	if err != nil || !ok {
		return nil, err
	}
	return []version.CompiledVersion{v}, nil
}

type promotionRunner struct {
	payroll workflow.Outcome
	recon   workflow.Outcome
}

func (r promotionRunner) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	out := frontier.NodeOutcome{NodeID: req.Node.ID, OutputDigest: "sha256:" + strings.Repeat("e", 64)}
	switch req.Node.ID {
	case promotionexec.NodeExecutePromotion:
		out.Outcome = workflow.OutcomeSucceeded
	case promotionexec.NodeObservePayroll:
		out.Outcome = r.payroll
	case promotionexec.NodeObserveAccess:
		out.Outcome = workflow.Outcome("PASS")
	case promotionexec.NodeObserveReconciliation:
		out.Outcome = r.recon
	case promotionexec.NodeCompensateHold:
		// The HoldReleasePort contract names the COMPENSATE route, never a
		// capability SUCCEEDED: the frontier rejects anything outside the
		// COMPENSATE vocabulary.
		out.Outcome = workflow.Outcome("COMPENSATED")
	case promotionexec.NodeEndComplete, promotionexec.NodeEndRepairPlan, promotionexec.NodeEndRejected,
		promotionexec.NodeEndInvalidated, promotionexec.NodeEndExpired, promotionexec.NodeEndCancelled,
		promotionexec.NodeEndBlocked:
		out.OutputDigest = ""
	default:
		out.Outcome = workflow.OutcomeSucceeded
	}
	return out, runtime.GovernanceRefs{}, nil
}

type repairRequester struct {
	mu    sync.Mutex
	calls []execute.RepairRequest
}

func (r *repairRequester) Request(_ context.Context, tx dbport.Tx, req execute.RepairRequest) error {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	r.mu.Unlock()
	_, _, err := (reconcile.PostgresStore{}).Create(context.Background(), tx, reconcile.Job{
		TenantID: req.TenantID, JobID: reconcile.JobID(req.TenantID, req.EffectRef, req.PolicyRef),
		EffectRef: req.EffectRef, EffectID: promotionexec.NodeObservePayroll, PolicyRef: req.PolicyRef,
		IntendedRef: req.IntendedRef, RequiredFreshness: observe.FreshnessFresh,
		NextCheckAt: req.RequestedAt, Deadline: req.RequestedAt.Add(24 * time.Hour), Owner: "workflow-execute",
		SLARef: "sla:promotion-repair", RepairPolicy: req.RepairPolicy, Status: reconcile.StatusPending,
		Version: 1, CreatedAt: req.RequestedAt, UpdatedAt: req.RequestedAt,
	})
	return err
}

func (r *repairRequester) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

type promotionFixture struct {
	db       *pgtest.DB
	conn     dbport.Beginner
	tenantID uuid.UUID
	plan     *workflow.CompiledWorkflow
	start    runtime.StartRequest
	at       time.Time
}

func newPromotionFixture(t *testing.T, key string) promotionFixture {
	f := newPromotionFixtureBase(t, key)
	preparePromotionAt(t, f, 8)
	return f
}

func newPromotionWaitingFixture(t *testing.T, key string, completedApproval bool) promotionFixture {
	f := newPromotionFixtureBase(t, key)
	if completedApproval {
		preparePromotionAt(t, f, 5)
	} else {
		preparePromotionAt(t, f, 4)
	}
	return f
}

// newPromotionFixtureV1_0 is [newPromotionFixture] on the frozen 1.0.0 plan,
// whose core commit routes straight to the payroll observation. The runtime
// mechanics tests (WF-RUN-003 redelivery, WF-RUN-006 retry, WF-RUN-007 poison
// work, WF-RUN-037 leg settlement) exercise that observation directly after
// the commit with drivers composed without signal ports; 1.0.0 is still a
// served version, and the 1.1.0 provider waits are driven by the conformance
// scenarios below and by test/workflow.
func newPromotionFixtureV1_0(t *testing.T, key string) promotionFixture {
	f := newPromotionFixtureOn(t, key, promotionexec.CompileV1_0)
	preparePromotionAt(t, f, 8)
	return f
}

func newPromotionFixtureBase(t *testing.T, key string) promotionFixture {
	return newPromotionFixtureOn(t, key, promotionexec.Compile)
}

func newPromotionFixtureOn(t *testing.T, key string, compile func(...workflow.Definition) (*workflow.CompiledWorkflow, error)) promotionFixture {
	t.Helper()
	db := pgtest.New(t)
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',$4)`, tenantID, "promotion-conformance-"+key, key, at.Add(-time.Hour))
	plan, err := compile()
	if err != nil {
		t.Fatalf("compile the promotion plan: %v", err)
	}
	intentID, revisionID := "intent:promotion:"+key, "proposal:promotion:"+key+":1"
	proposal := intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		CreatedBy:      intent.PrincipalReference{PrincipalID: "principal:test-initiator", Kind: intent.InitiatorHuman},
		MaterialDigest: digestReference(revisionID, intentID),
		Subjects:       []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:jane", AuthorityDomain: "PEOPLE"}},
	}
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan}}}
	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:" + key,
		Resolver: resolver, Versions: promotionVersionStore{plan: plan},
		Proposal:      runtime.ProposalBinding{Revision: proposal, ApprovalRef: "decision:promotion-start"},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: intentID, BusinessSubjectRefs: []string{"employment:jane"},
		ExecutionMode: workflow.ModeExecute, CorrelationID: "corr:" + key, CreatedAt: at,
	}
	f := promotionFixture{db: db, conn: appConn(t, db), tenantID: tenantID, plan: plan, start: start, at: at}
	return f
}

func digestReference(revisionID, intentID string) digest.Reference {
	return digest.Reference{ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1, AlgorithmID: "sha256", CanonicalLength: 42, Digest: "sha256:" + strings.Repeat("a", 64), ScopeBindingDigest: "sha256:" + strings.Repeat("b", 64), IntentID: &intentID, ProposalRevisionID: &revisionID}
}

func appConn(t *testing.T, db *pgtest.DB) dbport.Beginner {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	return conn
}

func preparePromotionAt(t *testing.T, f promotionFixture, count int) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenantID); err != nil {
		t.Fatal(err)
	}
	started, err := runtime.Start(ctx, tx, f.start)
	if err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}
	versionNow := started.InstanceVersion
	routes := []struct {
		node    string
		outcome workflow.Outcome
	}{
		{promotionexec.NodeSnapshotWorker, "SUCCEEDED"}, {promotionexec.NodeSimulateCompensation, "SUCCEEDED"},
		{promotionexec.NodeEvaluateBand, "SUCCEEDED"}, {promotionexec.NodeRaiseThreshold, "WITHIN_THRESHOLD"},
		{promotionexec.NodeApproveManager, "APPROVED"}, {promotionexec.NodeWaitEffectiveDate, "SUCCEEDED"},
		{promotionexec.NodeRevalidate, "SUCCEEDED"}, {promotionexec.NodeStillValid, "VALID"},
	}
	if count > len(routes) {
		count = len(routes)
	}
	for _, route := range routes[:count] {
		receipt, advanceErr := runtime.Advance(ctx, tx, runtime.AdvanceRequest{
			TenantID: f.tenantID, InstanceID: started.InstanceID, ExpectedInstanceVersion: versionNow,
			Attempt: 1, Plan: f.plan, Outcome: frontier.NodeOutcome{NodeID: route.node, Outcome: route.outcome, OutputDigest: "sha256:" + strings.Repeat("c", 64)}, RecordedAt: f.at,
			Sink: runtime.ContinuationStore{},
		})
		if advanceErr != nil {
			t.Fatalf("advance %s: %v", route.node, advanceErr)
		}
		versionNow = receipt.NewInstanceVersion
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func newPromotionDriver(t *testing.T, f promotionFixture, runner promotionRunner, repair *repairRequester) *execute.Driver {
	t.Helper()
	registry, err := datalogger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	terminal := &effects.LedgerTerminalWriter{Appender: datalogger.NewAppender(registry), ProjectionName: "promotion_conformance_outcome", SourceRef: "hcmnext:test:promotion-conformance"}
	// Promotion 1.1.0 parks on the providers' confirmations after the core
	// commit through the production durable subscription adapter, exactly as
	// the served composition does.
	driver, err := execute.New(execute.Options{DB: f.conn, Steps: runner, Terminal: terminal, Repair: repair, Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}, Clock: func() time.Time { return f.at },
		Signals: platformexecution.SignalSubscriptions{}, SignalReader: platformexecution.SignalSubscriptions{}})
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

// acceptingProviderVerifier stands in for a provider's signature check: the
// conformance scenarios prove the park/receive/resume mechanics.
type acceptingProviderVerifier struct{}

func (acceptingProviderVerifier) Verify(stepSignal.Signal) error { return nil }

// confirmProviderWait receives the provider's confirmation against the run's
// open 1.1.0 wait on node (correlated on the proposal revision, accepted only
// from source) and resumes the driver from the matched receipt. The resumed
// wait always succeeds; the observation after it judges the provider.
func confirmProviderWait(t *testing.T, f promotionFixture, driver *execute.Driver, instanceID uuid.UUID, node, source string) execute.Result {
	t.Helper()
	ctx := context.Background()
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenantID); err != nil {
		t.Fatal(err)
	}
	revision := f.start.Proposal.Revision.ProposalRevisionID
	sub, err := (signals.Store{}).OpenSubscriptionForCorrelation(ctx, tx, f.tenantID, node, revision)
	if err != nil {
		t.Fatalf("open %s wait: %v", node, err)
	}
	receipt, err := (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{
		Signal: stepSignal.Signal{
			Tenant: values.TenantId(f.tenantID.String()), Source: source,
			EventType: sub.EventType, SchemaRef: sub.ExpectedSchemaRef,
			CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue,
			IdempotencyKey: "test:" + node + ":" + revision,
			Payload:        []byte(`{"proposal_revision_id":"` + revision + `"}`),
			ReceivedAt:     values.NewInstant(f.at),
		},
		ReceivedAt: f.at,
	}, acceptingProviderVerifier{})
	if err != nil {
		t.Fatalf("receive the %s confirmation: %v", node, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	current := instance(t, f, instanceID)
	result, err := driver.ResumeSignal(ctx, execute.ResumeSignalRequest{
		Start: f.start, InstanceID: instanceID, ExpectedInstanceVersion: current.InstanceVersion,
		SignalID: receipt.SignalID, SubscriptionID: sub.ID, RecordedAt: f.at,
	})
	if err != nil {
		t.Fatalf("ResumeSignal(%s): %v", node, err)
	}
	return result
}

func instance(t *testing.T, f promotionFixture, id uuid.UUID) runtime.Instance {
	t.Helper()
	var got runtime.Instance
	tx, err := f.conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tenancy.WithTenant(context.Background(), tx, f.tenantID); err != nil {
		t.Fatal(err)
	}
	got, err = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestPromotionCoreCommitSurvivesAPermanentDownstreamFailure proves the
// authoritative fact and downstream repair terminal are separate writes.
func TestPromotionCoreCommitSurvivesAPermanentDownstreamFailure(t *testing.T) {
	f := newPromotionFixture(t, "scenario-07")
	repair := &repairRequester{}
	driver := newPromotionDriver(t, f, promotionRunner{payroll: "FAIL"}, repair)
	parked, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatal(err)
	}
	// The committed run parks on the payroll provider's confirmation (1.1.0);
	// the confirmation resumes it into the observation that reports FAIL.
	if parked.Status != execute.StatusParked {
		t.Fatalf("status = %s, want PARKED on the payroll confirmation", parked.Status)
	}
	result := confirmProviderWait(t, f, driver, parked.Start.InstanceID, promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll")
	result.Start = parked.Start
	if result.Status != execute.StatusComplete {
		t.Fatalf("status = %s, want COMPLETE", result.Status)
	}
	got := instance(t, f, result.Start.InstanceID)
	if got.RuntimeStatus != runtime.InstanceRepairRequired || got.CompletionDimensions.ConsistencyState != "DEGRADED" {
		t.Fatalf("instance = %+v, want REPAIR_REQUIRED/DEGRADED", got)
	}
	if repair.count() != 1 {
		t.Fatalf("repair requests = %d, want 1", repair.count())
	}
	var ledgerCount, repairCount int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1`, f.tenantID).Scan(&repairCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 || repairCount != 1 {
		t.Fatalf("ledger events = %d, repair jobs = %d, want 1/1", ledgerCount, repairCount)
	}
}

// TestAnalyticsUnavailabilityDoesNotBlockTheBusinessTransaction proves a
// degraded derived-observation route leaves the committed core fact intact.
func TestAnalyticsUnavailabilityDoesNotBlockTheBusinessTransaction(t *testing.T) {
	f := newPromotionFixture(t, "scenario-12")
	driver := newPromotionDriver(t, f, promotionRunner{payroll: "PASS", recon: "PARTIAL"}, &repairRequester{})
	result, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatal(err)
	}
	// Both providers confirm (1.1.0) before reconciliation degrades.
	instanceID := result.Start.InstanceID
	if parked := confirmProviderWait(t, f, driver, instanceID, promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll"); parked.Status != execute.StatusParked {
		t.Fatalf("after the payroll confirmation status = %s, want PARKED on the access confirmation", parked.Status)
	}
	if done := confirmProviderWait(t, f, driver, instanceID, promotionexec.NodeAwaitAccessConfirmation, "hcmnext.integrations.iam"); done.Status != execute.StatusComplete {
		t.Fatalf("after the access confirmation status = %s, want COMPLETE", done.Status)
	}
	got := instance(t, f, result.Start.InstanceID)
	if got.RuntimeStatus != runtime.InstanceRepairRequired || got.CompletionDimensions.ExecutionState != "REPAIR_REQUIRED" || got.CompletionDimensions.ConsistencyState != "DEGRADED" {
		t.Fatalf("instance = %+v, want repair with degraded consistency", got)
	}
	var ledgerCount int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 {
		t.Fatalf("core ledger events = %d, want 1", ledgerCount)
	}
}

func validMessageIntent(audience string, at time.Time) messaging.MessageIntent {
	return messaging.MessageIntent{IntentID: "message:promotion:" + strings.ToLower(audience), TenantID: "tenant", OrganizationScope: "acme/engineering", Purpose: messaging.PurposeEmployeeMessage, AudienceExpression: audience, AudienceResolutionPolicy: "DIRECTORY_V1", ContentRef: "content:promotion/v1", ParametersRef: "params:promotion/v1", Classification: "CONFIDENTIAL_HR", Urgency: "NORMAL", DeliveryRequirement: messaging.RequirementSubmitted, ReplyMode: messaging.ReplyOptional, CorrelationID: "corr:promotion", AvailableAt: ptr(at), ExpiresAt: at.Add(time.Hour)}
}
func ptr(t time.Time) *time.Time { return &t }

func TestTeamNotificationCannotReleaseBeforeTheEffectivePoint(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	team := validMessageIntent("TEAM", at)
	if err := execute.AuthorizeMessageRelease(execute.MessageReleaseRequest{Intent: team, EffectivePoint: at, Now: at.Add(-time.Second), EmployeeNotificationRecorded: true}); !errors.Is(err, execute.ErrMessageBeforeEffectivePoint) {
		t.Fatalf("early release error = %v, want effective-point refusal", err)
	}
	if err := execute.AuthorizeMessageRelease(execute.MessageReleaseRequest{Intent: team, EffectivePoint: at, Now: at, EmployeeNotificationRecorded: false}); !errors.Is(err, execute.ErrTeamNotificationOrdering) {
		t.Fatalf("unordered release error = %v, want ordering refusal", err)
	}
	if err := execute.AuthorizeMessageRelease(execute.MessageReleaseRequest{Intent: team, EffectivePoint: at, Now: at, EmployeeNotificationRecorded: true}); err != nil {
		t.Fatalf("ordered release: %v", err)
	}
}

func TestDuplicateOutboxDeliveryNeverDuplicatesAnyEffect(t *testing.T) {
	db := pgtest.New(t)
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local','duplicate effects','ACTIVE',$3)`, tenantID, "duplicate-effects-"+tenantID.String(), at)
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,'hcmnext.test.effect/v1','hcmnext.test.effect',1,'hcmnext.test.effect','PROTOBUF','LEDGER_EVENT')`, tenantID)
	conn := appConn(t, db)
	ctx := context.Background()
	types := []string{"payroll", "access", "learning", "document", "message", "usage", "billing"}
	for _, kind := range types {
		kind := kind
		var outboxID uuid.UUID
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			t.Fatal(err)
		}
		rec, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{Tenant: tenantID, EffectIdentity: "promotion:" + kind + ":semantic-1", OrderingKey: "promotion:semantic-1", SchemaRef: "hcmnext.test.effect/v1", Payload: []byte(kind)})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		outboxID = rec.OutboxID
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		clock := time.Now().UTC().Add(24 * time.Hour)
		consumer := outbox.NewConsumer(db.Conn, outbox.WithLease(time.Second), outbox.WithClock(func() time.Time { return clock }))
		count := 0
		handler := func(ctx context.Context, msg outbox.Record) error {
			tx, err := conn.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
				return err
			}
			scope := idempotency.Scope{Tenant: tenantID, Capability: "promotion", EffectScope: "downstream:" + kind, Key: "semantic-1"}
			_, err = idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, scope, strings.Repeat("d", 64), idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour}, at, func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
				count++
				return idempotency.ResultIdentity{EffectIdentity: msg.EffectIdentity}, nil
			})
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		batch, err := consumer.Poll(ctx, tenantID)
		if err != nil || len(batch) != 1 || batch[0].OutboxID != outboxID {
			t.Fatalf("%s first poll = %v, %v", kind, batch, err)
		}
		if err := handler(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(2 * time.Second)
		batch, err = consumer.Poll(ctx, tenantID)
		if err != nil || len(batch) != 1 {
			t.Fatalf("%s redelivery poll = %v, %v", kind, batch, err)
		}
		if err := handler(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s effect count = %d, want exactly 1", kind, count)
		}
	}
}

func cancellationDriver(t *testing.T, f promotionFixture) *execute.Driver {
	t.Helper()
	d, err := execute.New(execute.Options{DB: f.conn, Steps: promotionRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func cancelRequest(f promotionFixture, current runtime.Instance) execute.CancellationRequest {
	return execute.CancellationRequest{TenantID: f.tenantID, InstanceID: current.InstanceID, ExpectedInstanceVersion: current.InstanceVersion, Plan: f.plan, Reason: "PROMOTION_WITHDRAWN", RequestedBy: "principal:requester", RecordedAt: f.at.Add(time.Minute)}
}

func markCoreEffectExecuted(t *testing.T, f promotionFixture) {
	t.Helper()
	current := instanceByTenant(t, f)
	tx, err := f.conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tenancy.WithTenant(context.Background(), tx, f.tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Advance(context.Background(), tx, runtime.AdvanceRequest{TenantID: f.tenantID, InstanceID: current.InstanceID, ExpectedInstanceVersion: current.InstanceVersion, Attempt: 1, Plan: f.plan, Outcome: frontier.NodeOutcome{NodeID: promotionexec.NodeExecutePromotion, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:" + strings.Repeat("f", 64)}, RecordedAt: f.at, Sink: runtime.ContinuationStore{}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCancellationBeforeApprovalWritesNothing(t *testing.T) {
	f := newPromotionWaitingFixture(t, "cancel-before-approval", false)
	current := instanceByTenant(t, f)
	result, err := cancellationDriver(t, f).Cancel(context.Background(), cancelRequest(f, current))
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("status = %s, want CANCELLED", result.Instance.RuntimeStatus)
	}
	assertNoPromotionFact(t, f)
}

func TestCancellationAfterApprovalBeforeExecutionWritesNothing(t *testing.T) {
	f := newPromotionWaitingFixture(t, "cancel-after-approval", true)
	current := instanceByTenant(t, f)
	result, err := cancellationDriver(t, f).Cancel(context.Background(), cancelRequest(f, current))
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("status = %s, want CANCELLED", result.Instance.RuntimeStatus)
	}
	assertNoPromotionFact(t, f)
}

func TestCancellationAfterAnAmbiguousExternalEffectIsRefusedUntilObserved(t *testing.T) {
	f := newPromotionFixture(t, "cancel-ambiguous-effect")
	markCoreEffectExecuted(t, f)
	current := instanceByTenant(t, f)
	_, err := cancellationDriver(t, f).Cancel(context.Background(), cancelRequest(f, current))
	if !errors.Is(err, execute.ErrCancellationAmbiguous) {
		t.Fatalf("Cancel error = %v, want ambiguous-effect refusal", err)
	}
	after := instanceByTenant(t, f)
	if after.RuntimeStatus == runtime.InstanceCancelled || after.RuntimeStatus == runtime.InstanceCompleted {
		t.Fatalf("ambiguous cancellation claimed terminal status %s", after.RuntimeStatus)
	}
	assertNoPromotionFact(t, f)
}

func instanceByTenant(t *testing.T, f promotionFixture) runtime.Instance {
	t.Helper()
	var id uuid.UUID
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT instance_id FROM workflow_instance WHERE tenant_id = $1 ORDER BY created_at LIMIT 1`, f.tenantID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return instance(t, f, id)
}

func assertNoPromotionFact(t *testing.T, f promotionFixture) {
	t.Helper()
	var n int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("ledger events = %d, want 0", n)
	}
}
