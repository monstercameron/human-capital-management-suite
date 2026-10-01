package execution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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

// TestPromotionDriverCurrencyGuardPostCommit proves the real Driver runs the
// core promotion writer atomically, then checks exact durable assignment
// provenance again on each provider continuation.
func TestPromotionDriverCurrencyGuardPostCommit(t *testing.T) {
	t.Run("writer successor permits payroll access and reconciliation", func(t *testing.T) {
		f, started := newPhaseDriverRun(t, "driver-exact")
		driver := newPhaseDriver(t, f)
		result, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.driverStart})
		if err != nil {
			t.Fatalf("Driver.Execute: %v", err)
		}
		if result.Status != execute.StatusParked || len(result.Frontier) != 1 || result.Frontier[0] != promotionexec.NodeAwaitPayrollConfirmation {
			t.Fatalf("initial result = %+v, want parked on payroll confirmation", result)
		}
		result = confirmPhaseProvider(t, f, driver, started.InstanceID, promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll")
		if result.Status != execute.StatusParked || len(result.Frontier) != 1 || result.Frontier[0] != promotionexec.NodeAwaitAccessConfirmation {
			t.Fatalf("payroll result = %+v, want parked on access confirmation", result)
		}
		result = confirmPhaseProvider(t, f, driver, started.InstanceID, promotionexec.NodeAwaitAccessConfirmation, "hcmnext.integrations.iam")
		if result.Status != execute.StatusParked || len(result.Frontier) != 1 || result.Frontier[0] != promotionexec.NodeAcknowledgeRelease {
			t.Fatalf("access result = %+v, want parked on HRIS acknowledgement after reconciliation", result)
		}
		result = confirmPhaseProvider(t, f, driver, started.InstanceID, promotionexec.NodeAcknowledgeRelease, "hcmnext.integrations.hris")
		if result.Status != execute.StatusComplete {
			t.Fatalf("acknowledgement result = %+v, want COMPLETE after reconciliation", result)
		}
		got := loadPhaseWaitingSnapshot(t, f, started.InstanceID, "").instance
		if got.RuntimeStatus != runtime.InstanceCompleted {
			t.Fatalf("durable instance status = %s, want COMPLETED", got.RuntimeStatus)
		}
	})

	t.Run("unrelated same-grade successor refuses next continuation", func(t *testing.T) {
		f, started := newPhaseDriverRun(t, "driver-unrelated")
		driver := newPhaseDriver(t, f)
		parked, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.driverStart})
		if err != nil {
			t.Fatalf("Driver.Execute: %v", err)
		}
		if parked.Status != execute.StatusParked {
			t.Fatalf("initial result = %+v, want PARKED", parked)
		}
		before := loadPhaseWaitingSnapshot(t, f, started.InstanceID, promotionexec.NodeAwaitPayrollConfirmation)
		appendUnrelatedSameGradeSuccessor(t, f.promotionPhaseFixture)
		if _, err := confirmPhaseProviderErr(t, f, driver, started.InstanceID, promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll"); err == nil || !strings.Contains(err.Error(), "no exact provenance") {
			t.Fatalf("payroll continuation error = %v, want exact-provenance refusal", err)
		}
		after := loadPhaseWaitingSnapshot(t, f, started.InstanceID, promotionexec.NodeAwaitPayrollConfirmation)
		if after.instance.InstanceVersion != before.instance.InstanceVersion {
			t.Fatalf("durable instance version = %d after refusal, want unchanged %d", after.instance.InstanceVersion, before.instance.InstanceVersion)
		}
		if after.instance.RuntimeStatus != before.instance.RuntimeStatus {
			t.Fatalf("durable instance status = %s after refusal, want unchanged %s", after.instance.RuntimeStatus, before.instance.RuntimeStatus)
		}
		if strings.Join(after.instance.CurrentNodeIDs, "\x00") != strings.Join(before.instance.CurrentNodeIDs, "\x00") {
			t.Fatalf("durable frontier = %v after refusal, want unchanged %v", after.instance.CurrentNodeIDs, before.instance.CurrentNodeIDs)
		}
		if after.node.NodeExecutionID != before.node.NodeExecutionID || after.node.NodeID != before.node.NodeID || after.node.Attempt != before.node.Attempt {
			t.Fatalf("waiting node identity = %s/%s attempt %d after refusal, want unchanged %s/%s attempt %d", after.node.NodeExecutionID, after.node.NodeID, after.node.Attempt, before.node.NodeExecutionID, before.node.NodeID, before.node.Attempt)
		}
		if after.node.Status != before.node.Status {
			t.Fatalf("waiting node status = %s after refusal, want unchanged %s", after.node.Status, before.node.Status)
		}
		if !after.node.RecordedAt.Equal(before.node.RecordedAt) {
			t.Fatalf("waiting node recorded_at changed from %s to %s after refusal", before.node.RecordedAt, after.node.RecordedAt)
		}
	})
}

type phaseDriverFixture struct {
	*promotionPhaseFixture
	driverStart runtime.StartRequest
	plan        *workflow.CompiledWorkflow
}

func newPhaseDriverRun(t *testing.T, key string) (*phaseDriverFixture, runtime.StartReceipt) {
	t.Helper()
	f := &phaseDriverFixture{promotionPhaseFixture: newPromotionPhaseFixture(t)}
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile promotion plan: %v", err)
	}
	f.plan = plan
	f.driverStart = phaseDriverStart(f, plan, key)
	started := preparePhaseDriverRun(t, f)
	decision := f.decision
	decision.Attempt = 2
	decision.InstanceID = started.InstanceID
	digest, err := rules.InputDigest(decision.Input)
	if err != nil {
		t.Fatalf("digest threshold input: %v", err)
	}
	decision.InputDigest = digest
	evaluated, err := rules.EvaluatePromotionApproval(rules.PromotionApprovalThresholdTable(), decision.Input)
	if err != nil {
		t.Fatalf("evaluate threshold input: %v", err)
	}
	decision.Tier = string(evaluated.Tier)
	decision.MatchedRow = evaluated.MatchedRowID
	decision.TableID = evaluated.TableID
	decision.TableVersion = evaluated.TableVersion
	decision.TableDigest = evaluated.TableDigest
	if err := withPromotionPhaseTx(t, f.promotionPhaseFixture, func(tx dbport.Tx) error { return rulethreshold.Record(context.Background(), tx, decision) }); err != nil {
		t.Fatalf("record runtime threshold decision: %v", err)
	}
	return f, started
}

func phaseDriverStart(f *phaseDriverFixture, plan *workflow.CompiledWorkflow, key string) runtime.StartRequest {
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan}}}
	approval := runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{f.revision.ProposalRevisionID: {{
		DecisionID: "decision:phase-proof", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: f.revision.MaterialDigest.Digest,
	}}}}
	return runtime.StartRequest{
		TenantID: f.tenant, CellID: "cell-rule004-phase", StartIdempotencyKey: "driver-phase:" + key,
		Resolver: resolver, Versions: phaseDriverVersions{plan: plan},
		Proposal:      runtime.ProposalBinding{Revision: f.revision, ApprovalRef: "decision:phase-proof"},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approval,
		ExpectedIntentID: f.intent.String(), BusinessSubjectRefs: []string{f.worker.String()},
		ExecutionMode: workflow.ModeExecute, CorrelationID: "corr:driver-phase:" + key, CreatedAt: f.checkedAt.Add(-3 * time.Hour),
	}
}

func preparePhaseDriverRun(t *testing.T, f *phaseDriverFixture) runtime.StartReceipt {
	t.Helper()
	ctx := context.Background()
	conn := f.db.NewConn(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenant); err != nil {
		t.Fatal(err)
	}
	started, err := runtime.Start(ctx, tx, f.driverStart)
	if err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}
	routes := []struct {
		node    string
		outcome workflow.Outcome
	}{
		{promotionexec.NodeSnapshotWorker, "SUCCEEDED"}, {promotionexec.NodeSimulateCompensation, "SUCCEEDED"},
		{promotionexec.NodeEvaluateBand, "SUCCEEDED"}, {promotionexec.NodeRaiseThreshold, "WITHIN_THRESHOLD"},
		{promotionexec.NodeApproveManager, "APPROVED"}, {promotionexec.NodeWaitEffectiveDate, "SUCCEEDED"},
		{promotionexec.NodeRevalidate, "SUCCEEDED"}, {promotionexec.NodeStillValid, "VALID"},
	}
	versionNow := started.InstanceVersion
	for _, route := range routes {
		receipt, err := runtime.Advance(ctx, tx, runtime.AdvanceRequest{
			TenantID: f.tenant, InstanceID: started.InstanceID, ExpectedInstanceVersion: versionNow, Attempt: 1,
			Plan: f.plan, Outcome: frontier.NodeOutcome{NodeID: route.node, Outcome: route.outcome, OutputDigest: "sha256:" + strings.Repeat("c", 64)},
			RecordedAt: f.checkedAt, Sink: runtime.ContinuationStore{},
		})
		if err != nil {
			t.Fatalf("advance %s: %v", route.node, err)
		}
		versionNow = receipt.NewInstanceVersion
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return started
}

type phaseDriverVersions struct{ plan *workflow.CompiledWorkflow }

func (s phaseDriverVersions) Put(version.CompiledVersion) error { return nil }
func (s phaseDriverVersions) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || digest != s.plan.Digest() {
		return version.CompiledVersion{}, false, nil
	}
	return version.CompiledVersion{WorkflowID: s.plan.WorkflowID, SemanticVersion: promotionexec.SemanticVersion, CompiledPlanDigest: digest, Status: version.StatusActive}, true, nil
}
func (s phaseDriverVersions) GetActiveForWorkflow(id string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || id != s.plan.WorkflowID {
		return version.CompiledVersion{}, false, nil
	}
	return s.GetByDigest(s.plan.Digest())
}
func (s phaseDriverVersions) List(id string) ([]version.CompiledVersion, error) {
	v, ok, err := s.GetActiveForWorkflow(id)
	if err != nil || !ok {
		return nil, err
	}
	return []version.CompiledVersion{v}, nil
}

type phaseDriverRunner struct{ fixture *phaseDriverFixture }

func (r phaseDriverRunner) RunsInTransaction(node workflow.CompiledNode) bool {
	return node.ID == promotionexec.NodeExecutePromotion
}

func (r phaseDriverRunner) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return phaseDriverOutcome(req), runtime.GovernanceRefs{}, nil
}

func (r phaseDriverRunner) RunInTx(ctx context.Context, ex runtime.Executor, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	tx, ok := ex.(dbport.Tx)
	if !ok {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("promotion proof: execute_promotion did not receive the advance transaction")
	}
	if req.Node.ID != promotionexec.NodeExecutePromotion {
		return phaseDriverOutcome(req), runtime.GovernanceRefs{}, nil
	}
	if err := writePhaseSuccessor(ctx, tx, r.fixture); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	return phaseDriverOutcome(req), runtime.GovernanceRefs{}, nil
}

func phaseDriverOutcome(req execute.StepRequest) frontier.NodeOutcome {
	out := frontier.NodeOutcome{NodeID: req.Node.ID, OutputDigest: "sha256:" + strings.Repeat("d", 64), Outcome: workflow.OutcomeSucceeded}
	switch req.Node.ID {
	case promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess, promotionexec.NodeObserveReconciliation:
		out.Outcome = workflow.OutcomePass
	case promotionexec.NodeEndComplete, promotionexec.NodeEndRepairPlan, promotionexec.NodeEndRejected, promotionexec.NodeEndInvalidated, promotionexec.NodeEndExpired, promotionexec.NodeEndCancelled, promotionexec.NodeEndBlocked:
		out.OutputDigest = ""
	}
	return out
}

func writePhaseSuccessor(ctx context.Context, tx dbport.Tx, f *phaseDriverFixture) error {
	people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
	worker, err := people.CurrentWorker(ctx, tx, f.tenant, f.worker, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	employment, err := people.CurrentEmployment(ctx, tx, f.tenant, f.employment, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	assignment, err := people.CurrentAssignment(ctx, tx, f.tenant, f.assignment, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	job, err := organization.CurrentJob(ctx, tx, f.tenant, f.job, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	position, err := organization.CurrentJobPosition(ctx, tx, f.tenant, f.position, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	pkg, err := compensation.CurrentCompensationPackage(ctx, tx, f.tenant, f.pkg, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	base, err := compensation.CurrentCompensationComponent(ctx, tx, f.tenant, f.base, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	budget, err := compensation.CurrentBudgetReservation(ctx, tx, f.tenant, f.reservation, f.baseline.EffectiveFrom)
	if err != nil {
		return err
	}
	cmd := phaseCommand(f.promotionPhaseFixture, worker.Digest, employment.Digest, assignment.Digest, job.Digest, position.Digest, pkg.Digest, base.Digest, budget.Digest)
	_, err = (promotioncommit.Writer{People: people, Organization: organization, Compensation: compensation}).Write(ctx, tx, cmd)
	return err
}

type phaseDriverTerminal struct{}

func (phaseDriverTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return idempotency.ResultIdentity{ResultRef: "promotion-proof-terminal"}, nil
}

func newPhaseDriver(t *testing.T, f *phaseDriverFixture) *execute.Driver {
	t.Helper()
	guard := &execute.CurrencyGuard{Proposal: runtime.MemoryProposalFacts{}, Approval: f.driverStart.ApprovalFacts, Rules: &ServedRuleFacts{
		Thresholds: &stubRuleDeriver{input: f.decision.Input}, Approval: f.driverStart.ApprovalFacts,
	}}
	driver, err := execute.New(execute.Options{
		DB: f.db.Conn, Steps: phaseDriverRunner{fixture: f}, Terminal: phaseDriverTerminal{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}, Clock: func() time.Time { return f.checkedAt },
		Currency: guard, Signals: SignalSubscriptions{}, SignalReader: SignalSubscriptions{},
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	return driver
}

func confirmPhaseProvider(t *testing.T, f *phaseDriverFixture, driver *execute.Driver, instanceID uuid.UUID, node, source string) execute.Result {
	result, err := confirmPhaseProviderErr(t, f, driver, instanceID, node, source)
	if err != nil {
		t.Fatalf("ResumeSignal(%s): %v", node, err)
	}
	return result
}

func confirmPhaseProviderErr(t *testing.T, f *phaseDriverFixture, driver *execute.Driver, instanceID uuid.UUID, node, source string) (execute.Result, error) {
	t.Helper()
	ctx := context.Background()
	conn := f.db.NewConn(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return execute.Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenant); err != nil {
		return execute.Result{}, err
	}
	correlation := f.revision.ProposalRevisionID
	if node == promotionexec.NodeAcknowledgeRelease {
		correlation = f.revision.IntentID
	}
	sub, err := (signals.Store{}).OpenSubscriptionForCorrelation(ctx, tx, f.tenant, node, correlation)
	if err != nil {
		return execute.Result{}, err
	}
	receipt, err := (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{Signal: stepSignal.Signal{
		Tenant: values.TenantId(f.tenant.String()), Source: source, EventType: sub.EventType, SchemaRef: sub.ExpectedSchemaRef,
		CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue, IdempotencyKey: "driver-proof:" + node + ":" + f.revision.ProposalRevisionID,
		Payload: []byte(`{"proposal_revision_id":"` + f.revision.ProposalRevisionID + `"}`), ReceivedAt: values.NewInstant(f.checkedAt),
	}, ReceivedAt: f.checkedAt}, phaseAcceptingVerifier{})
	if err != nil {
		return execute.Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return execute.Result{}, err
	}
	current := loadPhaseWaitingSnapshot(t, f, instanceID, "").instance
	return driver.ResumeSignal(ctx, execute.ResumeSignalRequest{Start: f.driverStart, InstanceID: instanceID,
		ExpectedInstanceVersion: current.InstanceVersion, SignalID: receipt.SignalID, SubscriptionID: sub.ID, RecordedAt: f.checkedAt})
}

type phaseAcceptingVerifier struct{}

func (phaseAcceptingVerifier) Verify(stepSignal.Signal) error { return nil }

type phaseWaitingSnapshot struct {
	instance runtime.Instance
	node     runtime.NodeExecution
}

func loadPhaseWaitingSnapshot(t *testing.T, f *phaseDriverFixture, instanceID uuid.UUID, nodeID string) phaseWaitingSnapshot {
	t.Helper()
	ctx := context.Background()
	conn := f.db.NewConn(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenant); err != nil {
		t.Fatal(err)
	}
	store := runtime.Store{}
	instance, err := store.LoadInstance(ctx, tx, f.tenant, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	var waiting runtime.NodeExecution
	if nodeID != "" {
		waiting, err = store.LoadNodeExecution(ctx, tx, f.tenant, instanceID, nodeID, 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	if nodeID != "" && waiting.Status != runtime.NodeWaiting {
		t.Fatalf("durable waiting node %q status = %s, want WAITING", nodeID, waiting.Status)
	}
	return phaseWaitingSnapshot{instance: instance, node: waiting}
}
