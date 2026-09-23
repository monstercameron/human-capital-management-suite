package execution

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	transactioncoordinator "github.com/monstercameron/human-capital-management-suite/internal/transaction/coordinator"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// stubBeginner is a non-nil placeholder satisfying execute.Beginner.
// NewPromotionExecution only validates that a Beginner exists; it never calls
// Begin at construction time, so the stub refuses to be used.
type stubBeginner struct{}

func (stubBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("stubBeginner: Begin must not be called at construction")
}

// stubTerminal is a non-nil placeholder satisfying execute.TerminalWriter,
// for the same reason as stubBeginner.
type stubTerminal struct{}

func (stubTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return idempotency.ResultIdentity{}, errors.New("stubTerminal: Write must not be called at construction")
}

// TestNewPromotionExecutionValidation proves the two required ports are
// enforced before any composition happens at all.
func TestNewPromotionExecutionValidation(t *testing.T) {
	if _, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: stubBeginner{}, Terminal: stubTerminal{},
	}); err != nil {
		// sanity: the happy path below depends on these stubs being accepted
		t.Fatalf("NewPromotionExecution with a stub DB/Terminal: %v", err)
	}
	if _, err := NewPromotionExecution(PromotionExecutionConfig{Terminal: stubTerminal{}}); err == nil {
		t.Fatal("NewPromotionExecution without a DB Beginner: expected an error")
	}
	if _, err := NewPromotionExecution(PromotionExecutionConfig{DB: stubBeginner{}}); err == nil {
		t.Fatal("NewPromotionExecution without a TerminalWriter: expected an error")
	}
	if _, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: stubBeginner{}, Terminal: stubTerminal{},
		StartRetry: &transactioncommit.RetryOptions{MaxAttempts: 2},
	}); err == nil {
		t.Fatal("StartRetry without caller-owned admission: expected an error")
	}
	for name, mutate := range map[string]func(*transactioncommit.RetryOptions){
		"prepare": func(o *transactioncommit.RetryOptions) {
			o.Prepare = func(context.Context, dbport.Tx) (transactioncoordinator.CommitRequest, error) {
				return transactioncoordinator.CommitRequest{}, nil
			}
		},
		"resolve ambiguous": func(o *transactioncommit.RetryOptions) {
			o.ResolveAmbiguous = func(context.Context, transactioncoordinator.Receipt) (transactioncoordinator.Receipt, error) {
				return transactioncoordinator.Receipt{}, nil
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			opts := &transactioncommit.RetryOptions{Admit: func(context.Context) error { return nil }}
			mutate(opts)
			if _, err := NewPromotionExecution(PromotionExecutionConfig{DB: stubBeginner{}, Terminal: stubTerminal{}, StartRetry: opts}); err == nil {
				t.Fatal("commit-plan-only retry callback was silently ignored")
			}
		})
	}
}

// TestNewPromotionExecutionDefaults proves the composition root's asserted
// P1B authority and its default configuration land where the docs say they
// land: the closed intent-type set, the required role (default and override),
// the carried-through authority digest, and a ready-but-empty evidence sink.
func TestNewPromotionExecutionDefaults(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }
	base := PromotionExecutionConfig{DB: stubBeginner{}, Terminal: stubTerminal{}, Clock: clock}

	custom, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: stubBeginner{}, Terminal: stubTerminal{}, Clock: clock,
		AuthorityDigest: "sha256:customauthoritydigest", ApproverPrincipalID: "principal:custom",
		RequiredRole: "custom_operator",
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}

	baseGet, err := NewPromotionExecution(base)
	if err != nil {
		t.Fatalf("NewPromotionExecution (defaults): %v", err)
	}

	if got := baseGet.Authority.RequiredRole; got != "promotion_operator" {
		t.Errorf("default RequiredRole = %q, want %q", got, "promotion_operator")
	}
	if got := custom.Authority.RequiredRole; got != "custom_operator" {
		t.Errorf("override RequiredRole = %q, want %q", got, "custom_operator")
	}
	if len(baseGet.Authority.AdmittedIntentTypes) != 1 || !baseGet.Authority.AdmittedIntentTypes[promotion.IntentType] {
		t.Errorf("AdmittedIntentTypes = %#v, want exactly {%s: true}", baseGet.Authority.AdmittedIntentTypes, promotion.IntentType)
	}
	if custom.Authority.AuthorityDigest != "sha256:customauthoritydigest" {
		t.Errorf("AuthorityDigest = %q, want it carried through unchanged", custom.Authority.AuthorityDigest)
	}

	for name, ex := range map[string]*PromotionExecution{"defaults": baseGet, "custom": custom} {
		if ex.Executor == nil {
			t.Errorf("%s: Executor is nil", name)
		}
		if ex.Resolver == nil {
			t.Errorf("%s: Resolver is nil", name)
		}
		if ex.Versions == nil {
			t.Errorf("%s: Versions is nil", name)
		}
		if ex.Evidence == nil {
			t.Fatalf("%s: Evidence is nil", name)
		}
		if n := len(ex.Evidence.(*app.MemoryEvidenceSink).Records()); n != 0 {
			t.Errorf("%s: Evidence.Len() = %d at construction, want 0", name, n)
		}
	}
}

func TestTodo_PROMO_EXEC_SERVE_SelectsAndPublishesBothPlans(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }
	execution, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: stubBeginner{}, Terminal: stubTerminal{}, Clock: clock, Plan: PLAN_EXECUTE,
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution(execute): %v", err)
	}
	if execution.Plan != PLAN_EXECUTE {
		t.Fatalf("selected plan = %q, want %q", execution.Plan, PLAN_EXECUTE)
	}
	if _, err := execution.Versions.List("hcmnext.workflows.promotion.approval"); err != nil {
		t.Fatalf("list prototype versions: %v", err)
	}
	if _, err := execution.Versions.List("hcmnext.workflows.promotion.execute"); err != nil {
		t.Fatalf("list execute versions: %v", err)
	}
}

// TestPromotionStepRunner proves the two bounded node types the composed
// promote_worker graph uses: APPROVAL parks on exactly the one approval
// requirement the prototype plan names, END completes bare, and any other
// node type is a composition error, not a silent no-op.
func TestPromotionStepRunner(t *testing.T) {
	runner := promotionStepRunner{}
	ctx := context.Background()

	t.Run("approval parks on the prototype requirement", func(t *testing.T) {
		outcome, refs, err := runner.Run(ctx, execute.StepRequest{
			Node: workflow.CompiledNode{ID: "approve", Type: workflow.StepApproval},
		})
		if err != nil {
			t.Fatalf("Run(APPROVAL): %v", err)
		}
		if outcome.NodeID != "approve" {
			t.Errorf("NodeID = %q, want approve", outcome.NodeID)
		}
		if outcome.Await != frontier.AwaitWorkItem {
			t.Errorf("Await = %q, want %q", outcome.Await, frontier.AwaitWorkItem)
		}
		if outcome.AwaitRef != prototype.ApprovalRequirementID {
			t.Errorf("AwaitRef = %q, want %q", outcome.AwaitRef, prototype.ApprovalRequirementID)
		}
		if outcome.Outcome != "" {
			t.Errorf("Outcome = %q at an APPROVAL, want empty", outcome.Outcome)
		}
		if len(refs.EffectRefs) != 0 || refs.ProposalRef != "" || refs.PolicyRef != "" || refs.HumanTaskID != "" {
			t.Errorf("GovernanceRefs = %#v at an APPROVAL, want zero", refs)
		}
	})

	t.Run("end completes bare", func(t *testing.T) {
		outcome, _, err := runner.Run(ctx, execute.StepRequest{
			Node: workflow.CompiledNode{ID: "finish", Type: workflow.StepEnd},
		})
		if err != nil {
			t.Fatalf("Run(END): %v", err)
		}
		if outcome.NodeID != "finish" {
			t.Errorf("NodeID = %q, want finish", outcome.NodeID)
		}
		if outcome.Await != "" || outcome.AwaitRef != "" {
			t.Errorf("END outcome must carry no await, got Await=%q AwaitRef=%q", outcome.Await, outcome.AwaitRef)
		}
	})

	t.Run("anything else is a composition error", func(t *testing.T) {
		for _, nodeType := range []workflow.StepType{
			workflow.StepCapability, workflow.StepTask, workflow.StepWait, workflow.StepSignal,
		} {
			if _, _, err := runner.Run(ctx, execute.StepRequest{
				Node: workflow.CompiledNode{ID: "other", Type: nodeType},
			}); err == nil {
				t.Errorf("Run(%s): expected an error for an uncomposable node type", nodeType)
			}
		}
	})
}

// TestAdaptExecutionResult proves the projection from execute.Driver's own
// Result onto the port-owned app.ExecutionResult: visited nodes in order, the
// deprecated work-item named ParkedContinuations, the correctly-named typed
// lists (WF-RUN-032), continuation filtering down to the kinds that actually
// park (READY and COMPLETE excluded), and an EvidenceIDs copy that cannot be
// corrupted by the driver's own retry logic.
func TestAdaptExecutionResult(t *testing.T) {
	tenant := uuid.New()
	instance := uuid.New()
	itemID := uuid.New()

	workItemReq := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "request", SourceAttempt: 1, TargetNodeID: "approve",
		Kind: frontier.IntentWorkItemRequired,
	}
	signalReq := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "request", SourceAttempt: 1, TargetNodeID: "observe",
		Kind: frontier.IntentSignalSubscriptionRequired,
	}
	readyWork := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "request", SourceAttempt: 1,
		Kind: frontier.IntentReady,
	}
	completeRecord := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "finish", SourceAttempt: 2, TargetNodeID: "finish",
		Kind: frontier.IntentComplete,
	}

	result := execute.Result{
		Status: execute.StatusParked, InstanceVersion: 3,
		Advances: []runtime.AdvanceReceipt{
			{
				NodeID: "request",
				Continuations: []runtime.ContinuationRecord{
					workItemReq, signalReq, readyWork,
				},
			},
			{NodeID: "finish", Continuations: []runtime.ContinuationRecord{completeRecord}},
		},
		WorkItems: []workitem.WorkItem{{
			WorkItemID: itemID, Kind: workitem.KindApproval,
			WorkType: prototype.ApprovalRequirementID, NodeID: "approve",
		}},
		EvidenceIDs: []string{"obs-024-1", "obs-024-2"},
	}

	got := adaptExecutionResult(result, instance.String())

	if !got.Parked {
		t.Error("Parked = false, want true for a PARKED driver result")
	}
	if got.InstanceID != instance.String() {
		t.Errorf("InstanceID = %q, want the passed string form %q", got.InstanceID, instance.String())
	}
	if got.InstanceVersion != 3 {
		t.Errorf("InstanceVersion = %d, want 3", got.InstanceVersion)
	}
	if len(got.VisitedNodes) != 2 || got.VisitedNodes[0] != "request" || got.VisitedNodes[1] != "finish" {
		t.Errorf("VisitedNodes = %#v, want [request finish] in order", got.VisitedNodes)
	}

	// The deprecated field keeps its historical "<work_type>:<work_item_id>"
	// naming for every raised item.
	wantDeprecated := prototype.ApprovalRequirementID + ":" + itemID.String()
	//lint:ignore SA1019 wire compatibility: this assertion pins the deprecated compat population the wire still carries.
	if len(got.ParkedContinuations) != 1 || got.ParkedContinuations[0] != wantDeprecated {
		//lint:ignore SA1019 wire compatibility: same compat pin as above.
		t.Errorf("ParkedContinuations = %#v, want [%s]", got.ParkedContinuations, wantDeprecated)
	}

	// The typed refs carry only the genuinely-parking kinds, with the exact
	// runtime-derived continuation identity.
	wantWorkItemRef := runtime.ContinuationID(workItemReq.TenantID, workItemReq.InstanceID,
		workItemReq.SourceNodeID, workItemReq.SourceAttempt, workItemReq.TargetNodeID, workItemReq.Kind).String()
	wantSignalRef := runtime.ContinuationID(signalReq.TenantID, signalReq.InstanceID,
		signalReq.SourceNodeID, signalReq.SourceAttempt, signalReq.TargetNodeID, signalReq.Kind).String()
	wantRef := []app.ContinuationRef{
		{ContinuationID: wantWorkItemRef, Kind: "WORK_ITEM_REQUIRED", TargetNodeID: "approve"},
		{ContinuationID: wantSignalRef, Kind: "SIGNAL_SUBSCRIPTION_REQUIRED", TargetNodeID: "observe"},
	}
	if len(got.ParkedContinuationRefs) != 2 {
		t.Fatalf("ParkedContinuationRefs = %#v, want exactly 2 (READY and COMPLETE filtered out)", got.ParkedContinuationRefs)
	}
	for i, want := range wantRef {
		if got.ParkedContinuationRefs[i] != want {
			t.Errorf("ParkedContinuationRefs[%d] = %#v, want %#v", i, got.ParkedContinuationRefs[i], want)
		}
	}

	wantItems := []app.WorkItemRef{{WorkItemID: itemID.String(), Kind: string(workitem.KindApproval), NodeID: "approve"}}
	if len(got.ParkedWorkItems) != 1 || got.ParkedWorkItems[0] != wantItems[0] {
		t.Errorf("ParkedWorkItems = %#v, want %#v", got.ParkedWorkItems, wantItems)
	}

	if len(got.EvidenceIDs) != 2 || got.EvidenceIDs[0] != "obs-024-1" || got.EvidenceIDs[1] != "obs-024-2" {
		t.Errorf("EvidenceIDs = %#v, want [obs-024-1 obs-024-2]", got.EvidenceIDs)
	}
	result.EvidenceIDs = append(result.EvidenceIDs, "corrupted-after-adapt")
	if len(got.EvidenceIDs) != 2 {
		t.Errorf("EvidencedIDs changed after mutating the source: %#v", got.EvidenceIDs)
	}

	// A COMPLETE result parks on nothing.
	complete := execute.Result{Status: execute.StatusComplete, InstanceVersion: 7}
	completeGot := adaptExecutionResult(complete, instance.String())
	if completeGot.Parked || len(completeGot.ParkedContinuationRefs) != 0 || len(completeGot.ParkedWorkItems) != 0 {
		t.Errorf("complete result must project to an unparked result, got %#v", completeGot)
	}
	if completeGot.InstanceVersion != 7 {
		t.Errorf("complete InstanceVersion = %d, want 7", completeGot.InstanceVersion)
	}
}

func TestExecutionResultInstanceID_ResolvedStartUsesDurableProofWithoutFabricatingReceipt(t *testing.T) {
	instanceID := uuid.MustParse("4bca79c1-c393-493a-a798-f01c48c0e28f")
	resolved := execute.Result{
		Status: execute.StatusResolved,
		// Start is intentionally zero: outcome resolution proves current
		// durable state and never reconstructs the original StartReceipt.
		StartResolution: &runtime.StartResolution{Instance: runtime.Instance{
			InstanceID:      instanceID,
			InstanceVersion: 9,
			CurrentNodeIDs:  []string{"approve"},
			RuntimeStatus:   runtime.InstanceRunning, WorkflowID: "workflow.promotion", WorkflowVersion: 4,
			CompiledPlanHash: "sha256:plan",
		}, SemanticVersion: "4.2.0"},
		// Deliberately contradictory duplicate: the adapter must use the
		// verified resolution payload as the single source of current state.
		InstanceVersion: 99,
		Frontier:        []string{"approve"},
	}

	got, err := executionResultInstanceID(resolved, resolved.Start.InstanceID.String())
	if err != nil || got != instanceID.String() {
		t.Fatalf("resolved instance id result = %q, %v; want durable proof id %s", got, err, instanceID)
	}
	if resolved.Start.InstanceID != uuid.Nil {
		t.Fatalf("test fixture unexpectedly fabricated StartReceipt instance %s", resolved.Start.InstanceID)
	}
	adapted := adaptExecutionResult(resolved, got)
	if adapted.Status != app.ExecutionResultResolved || adapted.Parked || adapted.InstanceID != instanceID.String() || adapted.InstanceVersion != 9 || adapted.ResolvedStart == nil {
		t.Fatalf("resolved mapping = %+v", adapted)
	}
	state := adapted.ResolvedStart
	if state.RuntimeStatus != runtime.InstanceRunning || state.WorkflowID != "workflow.promotion" || state.WorkflowVersion != 4 || state.CompiledPlanDigest != "sha256:plan" || state.SemanticVersion != "4.2.0" || len(state.CurrentNodeIDs) != 1 || state.CurrentNodeIDs[0] != "approve" {
		t.Fatalf("resolved current state = %+v", state)
	}
	resolved.StartResolution.Instance.CurrentNodeIDs[0] = "mutated"
	if state.CurrentNodeIDs[0] != "approve" {
		t.Fatal("resolved frontier aliases driver result")
	}

	legacyID := uuid.New().String()
	for _, status := range []execute.Status{execute.StatusParked, execute.StatusComplete} {
		got, err := executionResultInstanceID(execute.Result{Status: status}, legacyID)
		if err != nil || got != legacyID {
			t.Fatalf("legacy %s mapping = %q, %v; want %q, nil", status, got, err, legacyID)
		}
	}
}

func TestExecutionResultInstanceID_RejectsMalformedResolvedProof(t *testing.T) {
	for name, result := range map[string]execute.Result{
		"missing resolution": {Status: execute.StatusResolved},
		"zero instance":      {Status: execute.StatusResolved, StartResolution: &runtime.StartResolution{}},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := executionResultInstanceID(result, uuid.New().String()); err == nil || got != "" {
				t.Fatalf("mapping = %q, %v; want empty result and error", got, err)
			}
		})
	}
}

type retryApprovalFacts struct{}

func (retryApprovalFacts) Decisions(ctx context.Context, ex runtime.Executor, tenant uuid.UUID, rev intent.ProposalRevision) ([]runtime.ApprovalDecisionFact, error) {
	var approved bool
	if err := ex.QueryRow(ctx, `SELECT approved FROM execution_retry_approval WHERE tenant_id=$1`, tenant).Scan(&approved); err != nil {
		return nil, err
	}
	outcome := runtime.ApprovalOutcomeRejected
	if approved {
		outcome = runtime.ApprovalOutcomeApproved
	}
	return []runtime.ApprovalDecisionFact{{DecisionID: "decision:retry-current", Outcome: outcome, ProposalDigest: rev.MaterialDigest.Digest}}, nil
}

// retryCurrency is the memory-backed currency guard the retry-composition
// harnesses run under. These harnesses exercise START transaction shape
// with synthetic intent ids the durable intent-control tables cannot key
// on, so the served durable guard cannot read them; the memory ports
// re-check the same synthetic approval the start itself was validated
// against, and the nil Rules port keeps the pre-REV-010-01 verdict
// exactly. RULE-004's served re-evaluation is covered by REV-010-01's own
// tests instead.
func retryCurrency() *execute.CurrencyGuard {
	return &execute.CurrencyGuard{Proposal: runtime.MemoryProposalFacts{}, Approval: retryApprovalFacts{}}
}

type observedSerializableBeginner struct {
	conn       *pgxadapter.Conn
	admin      *pgxadapter.Conn
	tenant     uuid.UUID
	failFirst  atomic.Bool
	beginCount atomic.Int32
	mu         sync.Mutex
	isolation  []string
}

func (b *observedSerializableBeginner) Begin(ctx context.Context) (dbport.Tx, error) {
	return b.conn.Begin(ctx)
}

func (b *observedSerializableBeginner) BeginSerializable(ctx context.Context) (dbport.Tx, error) {
	tx, err := b.conn.BeginSerializable(ctx)
	if err != nil {
		return nil, err
	}
	var isolation string
	if err := tx.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	b.beginCount.Add(1)
	b.mu.Lock()
	b.isolation = append(b.isolation, isolation)
	b.mu.Unlock()
	return &serializationOnceTx{Tx: tx, owner: b}, nil
}

type serializationOnceTx struct {
	dbport.Tx
	owner *observedSerializableBeginner
}

func (t *serializationOnceTx) Commit(ctx context.Context) error {
	if t.owner.failFirst.CompareAndSwap(false, true) {
		if err := t.Tx.Rollback(ctx); err != nil {
			return err
		}
		if _, err := t.owner.admin.Exec(ctx, `UPDATE execution_retry_approval SET approved=false WHERE tenant_id=$1`, t.owner.tenant); err != nil {
			return err
		}
		return &pgconn.PgError{Code: "40001", Message: "test serialization abort after current approval read"}
	}
	return t.Tx.Commit(ctx)
}

func retryStart(execution *PromotionExecution, tenant uuid.UUID, key string, at time.Time) runtime.StartRequest {
	intentID := "intent:retry:" + key
	revisionID := "proposal:retry:" + key
	rev := intent.ProposalRevision{
		ProposalRevisionID:  revisionID,
		IntentID:            intentID,
		Revision:            1,
		CreatedBy:           intent.PrincipalReference{PrincipalID: "principal:test-initiator", Kind: intent.InitiatorHuman},
		Tenant:              values.TenantId("retry-tenant"),
		OrganizationScopeID: "organization:retry",
		Subjects:            []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:retry", AuthorityDomain: "PEOPLE"}},
		// This harness exercises START transaction composition, not intent
		// materialization. The structurally valid reference is deliberately
		// local; proposal codec/seal verification is covered by its owner.
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest: "sha256:" + strings.Repeat("a", 64), ScopeBindingDigest: "sha256:" + strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
	return runtime.StartRequest{
		TenantID: tenant, CellID: "cell-local", StartIdempotencyKey: "start:" + key,
		Resolver: execution.Resolver, Versions: execution.Versions,
		Proposal: runtime.ProposalBinding{Revision: rev}, ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: retryApprovalFacts{},
		ExpectedIntentID: intentID, ExpectedTenant: rev.Tenant,
		BusinessSubjectRefs: []string{"employment:retry"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "correlation:" + key, CreatedAt: at,
	}
}

func TestTodo_DB_EDGE_003_IntegrationPromotionCompositionRetriesSerializableAndRechecksApproval(t *testing.T) {
	database := pgtest.New(t)
	tenant := uuid.New()
	at := time.Date(2026, 9, 8, 12, 0, 0, 123456000, time.UTC)
	database.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'retry-tenant','cell-local','Retry tenant','ACTIVE',$2)`, tenant, at.Add(-time.Hour))
	database.Exec(t, `CREATE TABLE execution_retry_approval (tenant_id uuid PRIMARY KEY, approved boolean NOT NULL)`)
	database.Exec(t, `INSERT INTO execution_retry_approval (tenant_id,approved) VALUES ($1,true)`, tenant)
	conn, admin := database.NewConn(t), database.NewConn(t)
	// The deadline protects the transaction exercise, not the one-time
	// embedded PostgreSQL bootstrap. Slow Windows hosts can legitimately spend
	// longer than this preparing the fixture before the first query runs.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	defer conn.Close(ctx)
	defer admin.Close(ctx)
	beginner := &observedSerializableBeginner{conn: conn, admin: admin, tenant: tenant}
	var admissions atomic.Int32
	retry := &transactioncommit.RetryOptions{
		MaxAttempts: 2, BaseDelay: time.Microsecond, MaxDelay: time.Microsecond,
		Admit: func(context.Context) error { admissions.Add(1); return nil },
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	execution, err := NewPromotionExecution(PromotionExecutionConfig{DB: beginner, Terminal: stubTerminal{}, Clock: func() time.Time { return at }, StartRetry: retry, Currency: retryCurrency()})
	if err != nil {
		t.Fatal(err)
	}
	// Configuration is owned by the composed driver after construction.
	retry.MaxAttempts = 1
	retry.Admit = nil
	_, refusalErr := execution.Executor.Execute(ctx, retryStart(execution, tenant, "approval-changes", at))
	if refusalErr == nil {
		t.Fatal("retry accepted an approval withdrawn after the serialization abort")
	}
	var runtimeErr *runtime.Error
	if !errors.As(refusalErr, &runtimeErr) || runtimeErr.Code != runtime.CodeUnapprovedProposal {
		t.Fatalf("withdrawn approval error=%v, want typed %s", refusalErr, runtime.CodeUnapprovedProposal)
	}
	if beginner.beginCount.Load() != 2 || admissions.Load() != 2 {
		t.Fatalf("attempts=%d admissions=%d, want two complete admitted START attempts; err=%v", beginner.beginCount.Load(), admissions.Load(), refusalErr)
	}
	beginner.mu.Lock()
	isolation := append([]string(nil), beginner.isolation...)
	beginner.mu.Unlock()
	if len(isolation) != 2 || isolation[0] != "serializable" || isolation[1] != "serializable" {
		t.Fatalf("START isolation = %v", isolation)
	}
	var instances int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, tenant).Scan(&instances); err != nil || instances != 0 {
		t.Fatalf("refused retry instances=%d err=%v", instances, err)
	}
	var workItems int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM work_item WHERE tenant_id=$1`, tenant).Scan(&workItems); err != nil || workItems != 0 {
		t.Fatalf("refused retry work items=%d err=%v", workItems, err)
	}
	if _, err := admin.Exec(ctx, `UPDATE execution_retry_approval SET approved=true WHERE tenant_id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	result, err := execution.Executor.Execute(ctx, retryStart(execution, tenant, "approved-current", at))
	if err != nil || !result.Parked || len(result.ParkedWorkItems) != 1 {
		t.Fatalf("approved START result=%+v err=%v", result, err)
	}
	if beginner.beginCount.Load() != 3 || admissions.Load() != 3 {
		t.Fatalf("successful START attempts=%d admissions=%d", beginner.beginCount.Load(), admissions.Load())
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, tenant).Scan(&instances); err != nil || instances != 1 {
		t.Fatalf("approved instances=%d err=%v", instances, err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM work_item WHERE tenant_id=$1`, tenant).Scan(&workItems); err != nil || workItems != 1 {
		t.Fatalf("approved work items=%d err=%v", workItems, err)
	}
}
