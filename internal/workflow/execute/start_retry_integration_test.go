package execute_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type retryVersionStore map[string]version.CompiledVersion

func (s retryVersionStore) Put(version.CompiledVersion) error { return errors.New("read only") }
func (s retryVersionStore) GetByDigest(d string) (version.CompiledVersion, bool, error) {
	v, ok := s[d]
	return v, ok, nil
}
func (s retryVersionStore) GetActiveForWorkflow(id string) (version.CompiledVersion, bool, error) {
	for _, v := range s {
		if v.WorkflowID == id && v.Status == version.StatusActive {
			return v, true, nil
		}
	}
	return version.CompiledVersion{}, false, nil
}
func (s retryVersionStore) List(id string) ([]version.CompiledVersion, error) {
	var out []version.CompiledVersion
	for _, v := range s {
		if v.WorkflowID == id {
			out = append(out, v)
		}
	}
	return out, nil
}

type switchingResolver struct {
	plans map[int]*workflow.CompiledWorkflow
	seen  []int
	mu    sync.Mutex
}

func (r *switchingResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return runtime.WorkflowSelection{}, errors.New("resolution must use the START transaction")
}
func (r *switchingResolver) ResolveWorkflowInTx(ctx context.Context, tx dbport.Tx, _ runtime.StartRequest) (runtime.WorkflowSelection, error) {
	var chosen int
	if err := tx.QueryRow(ctx, `SELECT chosen FROM execute_start_retry_fact`).Scan(&chosen); err != nil {
		return runtime.WorkflowSelection{}, err
	}
	r.mu.Lock()
	r.seen = append(r.seen, chosen)
	r.mu.Unlock()
	plan := r.plans[chosen]
	return runtime.WorkflowSelection{WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan}, nil
}

type retryStartBeginner struct {
	conn, admin *pgxadapter.Conn
	commits     atomic.Int32
	isolation   []string
	mu          sync.Mutex
}

func (b *retryStartBeginner) Begin(ctx context.Context) (dbport.Tx, error) { return b.conn.Begin(ctx) }
func (b *retryStartBeginner) BeginSerializable(ctx context.Context) (dbport.Tx, error) {
	tx, err := b.conn.BeginSerializable(ctx)
	if err != nil {
		return nil, err
	}
	var isolation string
	if err := tx.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	b.mu.Lock()
	b.isolation = append(b.isolation, isolation)
	b.mu.Unlock()
	return &retryStartTx{Tx: tx, owner: b}, nil
}

type retryStartTx struct {
	dbport.Tx
	owner *retryStartBeginner
}

func (t *retryStartTx) Commit(ctx context.Context) error {
	if t.owner.commits.Add(1) == 1 {
		if err := t.Tx.Rollback(ctx); err != nil {
			return err
		}
		if _, err := t.owner.admin.Exec(ctx, `UPDATE execute_start_retry_fact SET chosen=2`); err != nil {
			return err
		}
		return &pgconn.PgError{Code: "40001", Message: "known test abort"}
	}
	return t.Tx.Commit(ctx)
}

type retryApprovalRunner struct{ calls atomic.Int32 }

func (r *retryApprovalRunner) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	r.calls.Add(1)
	return frontier.NodeOutcome{NodeID: req.Node.ID, Await: frontier.AwaitWorkItem, AwaitRef: prototype.ApprovalRequirementID}, runtime.GovernanceRefs{}, nil
}

type retryWorkItems struct{}

func (retryWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (workitem.WorkItem, error) {
	requirement, err := prototype.CompileApprovalRequirement("principal:retry-approver", req.CreatedAt.Add(24*time.Hour))
	if err != nil {
		return workitem.WorkItem{}, err
	}
	item, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
		TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
		WorkType: requirement.RequirementID, CorrelationID: req.CorrelationID,
		WorkflowInstanceID: req.Continuation.InstanceID, NodeID: req.Continuation.TargetNodeID,
		ProposalRef: req.Proposal.Revision.MaterialDigest.Digest, SubjectRefs: req.SubjectRefs,
		PolicyRouteRef: "route.retry-test", Visibility: workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: req.Proposal.Revision.OrganizationScopeID,
		DeadlineAt:          requirement.Deadline.Expiry.Time(), CreatedAt: req.CreatedAt,
	}, requirement.RequirementID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	store := workitem.Store{}
	meta := workitem.TransitionMeta{ActorPrincipalID: "system:retry-test", Reason: "retry_test.created", At: req.CreatedAt}
	created, err := store.Create(ctx, ex, item, meta)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	resolution := humanwork.Resolution{
		RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision,
		Outcome:    humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{{PrincipalID: "principal:retry-approver", Via: humanwork.SourceDirect, TermRef: "term:retry-test"}},
		ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
		DirectoryVersion: "directory.retry-test/1", ExpressionDigest: requirement.ExpressionDigest,
		RequirementDigest: requirement.Digest(), QuorumRequired: requirement.Quorum.MinApprovals,
	}
	return store.Route(ctx, ex, created.TenantID, created.WorkItemID, created.ItemVersion,
		workitem.Assignment{Resolution: resolution, GovernancePolicyRef: requirement.Source.GovernancePolicyRef, Trigger: workitem.TriggerInitialRouting, ChosenOwner: "principal:retry-approver"}, meta)
}

func TestTodo_DB_EDGE_003_IntegrationExecuteRetriesChangedSelectionInFreshSnapshot(t *testing.T) {
	db := pgtest.New(t)
	// The deadline starts once PostgreSQL is up, so a loaded sweep's
	// start-up time does not consume the budget the test bounds.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tenant, at := uuid.New(), time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','Retry selection','ACTIVE',$3)`, tenant, "execute-retry-"+tenant.String(), at.Add(-time.Hour))
	db.Exec(t, `CREATE TABLE execute_start_retry_fact (chosen integer NOT NULL)`)
	db.Exec(t, `INSERT INTO execute_start_retry_fact VALUES (1)`)

	def1 := prototype.ApprovalDefinition()
	plan1, err := workflow.Compile(def1, workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatal(err)
	}
	def2 := prototype.ApprovalDefinition()
	def2.Version = 2
	def2.Name = "Prototype promotion approval revised"
	plan2, err := workflow.Compile(def2, workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatal(err)
	}
	if plan1.Digest() == plan2.Digest() {
		t.Fatal("changed workflow version did not change plan digest")
	}
	resolver := &switchingResolver{plans: map[int]*workflow.CompiledWorkflow{1: plan1, 2: plan2}}
	versions := retryVersionStore{
		plan1.Digest(): {WorkflowID: plan1.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: plan1.Digest(), Status: version.StatusActive},
		plan2.Digest(): {WorkflowID: plan2.WorkflowID, SemanticVersion: "2.0.0", CompiledPlanDigest: plan2.Digest(), Status: version.StatusActive},
	}
	conn, admin := db.NewConn(t), db.NewConn(t)
	defer conn.Close(ctx)
	defer admin.Close(ctx)
	beginner := &retryStartBeginner{conn: conn, admin: admin}
	runner := &retryApprovalRunner{}
	driver, err := execute.New(execute.Options{DB: beginner, Steps: runner, WorkItems: retryWorkItems{}, Clock: func() time.Time { return at }, StartRetry: &transactioncommit.RetryOptions{MaxAttempts: 2, BaseDelay: time.Microsecond, MaxDelay: time.Microsecond, Admit: func(context.Context) error { return nil }, Sleep: func(context.Context, time.Duration) error { return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	intentID, revisionID := "intent:retry-selection", "proposal:retry-selection:1"
	rev := intent.ProposalRevision{IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1, Tenant: values.TenantId("retry-selection"), OrganizationScopeID: "organization:retry", Subjects: []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:retry", AuthorityDomain: "PEOPLE"}}, CreatedBy: intent.PrincipalReference{PrincipalID: "principal:test-initiator", Kind: intent.InitiatorHuman}, MaterialDigest: digestReference(revisionID, intentID)}
	result, err := driver.Execute(ctx, execute.ExecuteRequest{Start: runtime.StartRequest{TenantID: tenant, CellID: "cell-local", StartIdempotencyKey: "start:retry-selection", Resolver: resolver, Versions: versions, Proposal: runtime.ProposalBinding{Revision: rev}, ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(rev), ExpectedIntentID: intentID, ExpectedTenant: rev.Tenant, BusinessSubjectRefs: []string{"employment:retry"}, ExecutionMode: workflow.ModeExecute, CorrelationID: "correlation:retry-selection", CreatedAt: at}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execute.StatusParked || result.Start.CompiledPlanDigest != plan2.Digest() {
		t.Fatalf("result=%+v want parked on revised plan %s", result, plan2.Digest())
	}
	if runner.calls.Load() != 1 || beginner.commits.Load() != 2 {
		t.Fatalf("runner calls=%d commits=%d", runner.calls.Load(), beginner.commits.Load())
	}
	resolver.mu.Lock()
	seen := append([]int(nil), resolver.seen...)
	resolver.mu.Unlock()
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("resolver snapshots=%v", seen)
	}
	beginner.mu.Lock()
	isolation := append([]string(nil), beginner.isolation...)
	beginner.mu.Unlock()
	if len(isolation) != 2 || isolation[0] != "serializable" || isolation[1] != "serializable" {
		t.Fatalf("isolation=%v", isolation)
	}
	var instances, items int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1 AND compiled_plan_hash=$2`, tenant, plan2.Digest()).Scan(&instances); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM work_item WHERE tenant_id=$1`, tenant).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if instances != 1 || items != 1 {
		t.Fatalf("revised instances=%d work items=%d, want 1/1", instances, items)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1 AND compiled_plan_hash=$2`, tenant, plan1.Digest()).Scan(&instances); err != nil || instances != 0 {
		t.Fatalf("aborted original-plan instances=%d err=%v", instances, err)
	}
}
