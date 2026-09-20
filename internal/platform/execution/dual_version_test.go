package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// dualVersionRunner answers every node the drain reaches after the core
// commit: the commit succeeds and every observation holds.
type dualVersionRunner struct{ ran []string }

func (r *dualVersionRunner) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	r.ran = append(r.ran, req.Node.ID)
	out := frontier.NodeOutcome{NodeID: req.Node.ID, OutputDigest: "sha256:" + strings.Repeat("d", 64), Outcome: workflow.OutcomeSucceeded}
	if req.Node.Type == workflow.StepObserve {
		out.Outcome = workflow.OutcomePass
	}
	return out, runtime.GovernanceRefs{}, nil
}

// releaseVersion approves v on its own in-process fixture report and
// activates it through the durable registry, superseding the active one.
func releaseVersion(t *testing.T, ctx context.Context, store workflowversionstore.Store, v version.CompiledVersion, at time.Time) version.CompiledVersion {
	t.Helper()
	report := releasefixture.Run(ShippedFixtures(), v, "principal:release-engineer", at)
	if _, err := ApproveRelease(ctx, store, ShippedFixtures(), ReleaseApproval{
		CompiledPlanDigest: v.CompiledPlanDigest, ApprovedBy: "principal:release-manager", Authority: "authority:workflow-release-board",
		Reason: "release " + v.SemanticVersion, Report: report, ApprovedAt: at,
	}); err != nil {
		t.Fatalf("ApproveRelease(%s): %v", v.SemanticVersion, err)
	}
	active, err := store.ActivateApproved(ctx, v.CompiledPlanDigest, true)
	if err != nil {
		t.Fatalf("ActivateApproved(%s): %v", v.SemanticVersion, err)
	}
	return active
}

// TestPromotionStartedOn1_0ResumesAfter1_1IsActivated is the dual-version
// serving proof: an instance started while 1.0.0 was the ACTIVE version, and
// parked with its core commit READY, keeps advancing on the frozen 1.0.0
// graph after 1.1.0 is activated and supersedes 1.0.0 -- the core commit
// routes straight to the payroll observation, no provider wait is opened,
// and the run parks on 1.0.0's acknowledgement gate. The same continuation
// without its pin resolves 1.1.0 and is refused against the 1.0.0 instance,
// and a new start resolves 1.1.0.
func TestPromotionStartedOn1_0ResumesAfter1_1IsActivated(t *testing.T) {
	ctx := context.Background()
	database := pgtest.New(t)
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	tenant := uuid.New()
	database.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'dual-version','cell-local','Dual version tenant','ACTIVE',$2)`, tenant, at.Add(-time.Hour))
	store := workflowversionstore.Store{DB: database.Conn}
	published, err := PublishShippedVersions(store, at)
	if err != nil || len(published) != 3 {
		t.Fatalf("PublishShippedVersions = %d, %v", len(published), err)
	}
	v1, v11 := published[1], published[2]
	frozen, err := promotionexec.CompileV1_0()
	if err != nil {
		t.Fatal(err)
	}
	current, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if v1.CompiledPlanDigest != frozen.Digest() || v11.CompiledPlanDigest != current.Digest() {
		t.Fatalf("published execute versions %s/%s, want the frozen and current plans", v1.SemanticVersion, v11.SemanticVersion)
	}

	// The previous release: 1.0.0 is the ACTIVE version, served by a
	// resolver that knows only it. A run starts and reaches its core commit.
	releaseVersion(t, ctx, store, v1, at)
	intentID, revisionID := "intent:dual-version", "proposal:dual-version:1"
	revision := intent.ProposalRevision{
		ProposalRevisionID: revisionID, IntentID: intentID, Revision: 1,
		CreatedBy: intent.PrincipalReference{PrincipalID: "principal:test-initiator", Kind: intent.InitiatorHuman},
		Subjects:  []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:dual", AuthorityDomain: "PEOPLE"}},
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest: "sha256:" + strings.Repeat("a", 64), ScopeBindingDigest: "sha256:" + strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
	start := runtime.StartRequest{
		TenantID: tenant, CellID: "cell-local", StartIdempotencyKey: "start:dual-version",
		Resolver: effects.PolicyResolver{Entries: []effects.PolicyEntry{{WorkflowID: frozen.WorkflowID, Pin: version.Pin{CompiledPlanDigest: frozen.Digest()}, Plan: frozen}}},
		Versions: store,
		Proposal: runtime.ProposalBinding{Revision: revision}, ProposalFacts: runtime.MemoryProposalFacts{},
		ApprovalFacts: runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{revisionID: {{
			DecisionID: "decision:dual-version", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: revision.MaterialDigest.Digest,
		}}}},
		ExpectedIntentID: intentID, BusinessSubjectRefs: []string{"employment:dual"},
		ExecutionMode: workflow.ModeExecute, CorrelationID: "corr:dual-version", CreatedAt: at,
	}
	conn := database.NewConn(t)
	var instanceID uuid.UUID
	var instanceVersion int64
	inTenant := func(fn func(dbport.Tx) error) {
		t.Helper()
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			t.Fatal(err)
		}
		if err := fn(tx); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	inTenant(func(tx dbport.Tx) error {
		started, err := runtime.Start(ctx, tx, start)
		if err != nil {
			return err
		}
		instanceID, instanceVersion = started.InstanceID, started.InstanceVersion
		for _, route := range []struct {
			node    string
			outcome workflow.Outcome
		}{
			{promotionexec.NodeSnapshotWorker, "SUCCEEDED"}, {promotionexec.NodeSimulateCompensation, "SUCCEEDED"},
			{promotionexec.NodeEvaluateBand, "SUCCEEDED"}, {promotionexec.NodeRaiseThreshold, "WITHIN_THRESHOLD"},
			{promotionexec.NodeApproveManager, "APPROVED"}, {promotionexec.NodeWaitEffectiveDate, "SUCCEEDED"},
			{promotionexec.NodeRevalidate, "SUCCEEDED"}, {promotionexec.NodeStillValid, "VALID"},
		} {
			receipt, err := runtime.Advance(ctx, tx, runtime.AdvanceRequest{
				TenantID: tenant, InstanceID: instanceID, ExpectedInstanceVersion: instanceVersion, Attempt: 1, Plan: frozen,
				Outcome:    frontier.NodeOutcome{NodeID: route.node, Outcome: route.outcome, OutputDigest: "sha256:" + strings.Repeat("c", 64)},
				RecordedAt: at, Sink: runtime.ContinuationStore{},
			})
			if err != nil {
				return err
			}
			instanceVersion = receipt.NewInstanceVersion
		}
		return nil
	})

	// This release: 1.1.0 is activated and supersedes 1.0.0.
	later := at.Add(time.Hour)
	releaseVersion(t, ctx, store, v11, later)
	superseded, _, err := store.GetByDigest(frozen.Digest())
	if err != nil || superseded.Status != version.StatusQuarantined || !superseded.QuarantinedBySupersession() {
		t.Fatalf("1.0.0 after the 1.1.0 activation = %s (%v), want QUARANTINED by supersession", superseded.Status, err)
	}
	if policy, quarantined, err := store.LiveInstancePolicy(ctx, database.Conn, frozen.Digest()); err != nil || quarantined {
		t.Fatalf("supersession declared a live-instance policy %q (%v): it is not a governed quarantine", policy, err)
	}

	runner := &dualVersionRunner{}
	driver, err := execute.New(execute.Options{
		DB: conn, Steps: runner, Terminal: stubTerminal{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return later },
		Signals:   SignalSubscriptions{}, SignalReader: SignalSubscriptions{},
		Quarantine: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	continuation := start
	continuation.Resolver = promotionExecuteResolver(current, frozen)

	// Without its pin the continuation resolves 1.1.0, which is not the plan
	// the instance pinned: refused before any step runs.
	if _, err := driver.RedeliverReady(ctx, execute.RedeliverRequest{Start: continuation, InstanceID: instanceID, ExpectedInstanceVersion: instanceVersion}); err == nil {
		t.Fatal("an unpinned continuation advanced a 1.0.0 instance on the 1.1.0 plan")
	}
	if len(runner.ran) != 0 {
		t.Fatalf("a refused continuation ran %v", runner.ran)
	}

	// Pinned to the digest the instance carries, it advances on 1.0.0.
	continuation.PinnedCompiledPlanDigest = frozen.Digest()
	result, err := driver.RedeliverReady(ctx, execute.RedeliverRequest{Start: continuation, InstanceID: instanceID, ExpectedInstanceVersion: instanceVersion})
	if err != nil {
		t.Fatalf("RedeliverReady on the superseded 1.0.0 pin: %v", err)
	}
	if result.Status != execute.StatusParked {
		t.Fatalf("resumed 1.0.0 run = %+v, want it parked on the acknowledgement gate", result)
	}
	want := []string{promotionexec.NodeExecutePromotion, promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess, promotionexec.NodeObserveReconciliation}
	if strings.Join(runner.ran, ",") != strings.Join(want, ",") {
		t.Fatalf("1.0.0 run advanced through %v, want %v: the core commit routes straight to the payroll observation", runner.ran, want)
	}
	var waits, acks int
	if err := database.QueryRow(ctx, `SELECT count(*) FROM workflow_signal_subscription WHERE instance_id = $1 AND node_id IN ($2,$3)`,
		instanceID, promotionexec.NodeAwaitPayrollConfirmation, promotionexec.NodeAwaitAccessConfirmation).Scan(&waits); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(ctx, `SELECT count(*) FROM workflow_signal_subscription WHERE instance_id = $1 AND node_id = $2 AND subscription_state = 'OPEN'`,
		instanceID, promotionexec.NodeAcknowledgeRelease).Scan(&acks); err != nil {
		t.Fatal(err)
	}
	if waits != 0 || acks != 1 {
		t.Fatalf("1.0.0 run opened %d provider waits and %d acknowledgement waits, want 0 and 1", waits, acks)
	}
	var pinned string
	if err := database.QueryRow(ctx, `SELECT compiled_plan_hash FROM workflow_instance WHERE instance_id = $1`, instanceID).Scan(&pinned); err != nil || pinned != frozen.Digest() {
		t.Fatalf("instance plan = %q (%v), want still pinned to 1.0.0", pinned, err)
	}

	// A new start resolves the current version.
	selection, err := continuation.Resolver.ResolveWorkflow(ctx, runtime.StartRequest{})
	if err != nil || selection.Plan.Digest() != current.Digest() {
		t.Fatalf("new start resolves %v (%v), want 1.1.0", selection.Pin, err)
	}
}
