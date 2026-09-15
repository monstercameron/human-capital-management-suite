package execute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// WF-STEP-018 kernel tests: multi-approver quorum, invalidators and the
// atomic decision-advance, against PostgreSQL through the real driver.

const (
	wfstep018A = "principal:approver-a"
	wfstep018B = "principal:approver-b"
	wfstep018C = "principal:approver-c"
)

// wfstep018Slots declares one requirement of the fixture: its quorum and how
// many work item slots it is materialized with.
type wfstep018Slots struct {
	id     string
	quorum uint32
	slots  int
}

type wfstep018Fixture struct {
	db           *pgtest.DB
	conn         *pgxadapter.Conn
	tenantID     uuid.UUID
	instanceID   uuid.UUID
	plan         *workflow.CompiledWorkflow
	proposal     intent.ProposalRevision
	set          humanwork.RequirementSet
	slots        map[string][]uuid.UUID
	continuation stepapproval.Continuation
	at           time.Time
}

func newWfStep018Fixture(t *testing.T, reqs ...wfstep018Slots) *wfstep018Fixture {
	t.Helper()
	db := pgtest.New(t)
	f := &wfstep018Fixture{
		db: db, tenantID: uuid.New(), instanceID: uuid.New(), slots: map[string][]uuid.UUID{},
		at: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
	}
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'WF-STEP-018 tenant', 'ACTIVE', $3)`,
		f.tenantID, "wf-step-018-"+f.tenantID.String(), f.at.Add(-time.Hour))
	f.conn = f.appConn(t)
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}
	f.plan = plan
	intentID, revisionID := "intent:promotion:wf-step-018", "proposal:promotion:wf-step-018:1"
	f.proposal = intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest: strings.Repeat("a", 64), ScopeBindingDigest: strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
	for _, spec := range reqs {
		f.set.Requirements = append(f.set.Requirements, humanwork.ApprovalRequirement{
			RequirementID: spec.id, Revision: 1, Stage: 1,
			Quorum: humanwork.Quorum{MinApprovals: spec.quorum, RequireDistinctPrincipals: true},
			Deadline: humanwork.Deadline{
				DecideBy: values.NewInstant(f.at.Add(time.Hour)), Expiry: values.NewInstant(f.at.Add(2 * time.Hour)),
			},
			Separation:       humanwork.SeparationConstraints{OneRequirementPerPrincipal: true, RuleID: "rule:sod"},
			Invalidators:     []humanwork.Invalidator{{Kind: humanwork.InvalidatorMaterialProposalChange, RuleID: "rule:material"}},
			ExpressionDigest: "sha256:" + strings.Repeat("c", 64),
			Source: humanwork.RequirementSource{
				TableID: "promotion.threshold", TableVersion: "1", TableDigest: "sha256:" + strings.Repeat("d", 64),
				MatchedRowID: "board", GovernancePolicyRef: "policy:promotion-approval/v1",
			},
		})
	}

	var items []workitem.WorkItem
	f.tx(t, func(tx dbport.Tx) error {
		inst, err := runtime.NewInstance(f.tenantID, f.instanceID, "cell-local", plan, workflow.ModeExecute,
			"sha256:"+strings.Repeat("1", 64), "correlation:wf-step-018", f.at.Add(-time.Hour))
		if err != nil {
			return err
		}
		inst.RuntimeStatus = runtime.InstanceWaiting
		started := f.at.Add(-time.Hour)
		inst.StartedAt = &started
		if _, err := (runtime.Store{}).CreateInstance(context.Background(), tx, inst); err != nil {
			return err
		}
		node := runtime.NewNodeExecution(f.tenantID, f.instanceID, prototype.NodeApproval, 1, workflow.StepApproval, runtime.NodeWaiting)
		if _, _, err := (runtime.Store{}).RecordNodeExecution(context.Background(), tx, node, 1); err != nil {
			return err
		}
		store := workitem.Store{}
		for _, req := range f.set.Requirements {
			spec := reqs[0]
			for _, s := range reqs {
				if s.id == req.RequirementID {
					spec = s
				}
			}
			for range spec.slots {
				item, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
					TenantID: f.tenantID, WorkType: req.RequirementID, CorrelationID: "correlation:wf-step-018",
					WorkflowInstanceID: f.instanceID, NodeID: prototype.NodeApproval,
					ProposalRef: f.proposal.MaterialDigest.Digest, SubjectRefs: []string{"worker:jane"},
					PolicyRouteRef: "route:board/v1", Visibility: workitem.VisibilityCandidateSet,
					OrganizationScopeID: "org:acme/people", DeadlineAt: f.at.Add(time.Hour), CreatedAt: f.at.Add(-time.Hour),
				}, req.RequirementID)
				if err != nil {
					return err
				}
				if item, err = store.Create(context.Background(), tx, item, work006Meta("system:workflow", "created", f.at.Add(-time.Hour))); err != nil {
					return err
				}
				candidates := []humanwork.Candidate{}
				for _, p := range []string{wfstep018A, wfstep018B, wfstep018C} {
					candidates = append(candidates, humanwork.Candidate{PrincipalID: p, Via: humanwork.SourceDirect, TermRef: "role:board"})
				}
				item, err = store.Route(context.Background(), tx, f.tenantID, item.WorkItemID, item.ItemVersion, workitem.Assignment{
					Resolution: humanwork.Resolution{
						RequirementID: req.RequirementID, RequirementRevision: req.Revision, Outcome: humanwork.OutcomeResolved,
						Candidates: candidates, ResolvedAt: values.NewInstant(f.at.Add(-time.Hour)), EffectiveAt: values.NewInstant(f.at.Add(-time.Hour)),
						DirectoryVersion: "directory:1", ExpressionDigest: req.ExpressionDigest, RequirementDigest: req.Digest(),
						QuorumRequired: req.Quorum.MinApprovals,
					},
					GovernancePolicyRef: req.Source.GovernancePolicyRef, Trigger: workitem.TriggerInitialRouting,
				}, work006Meta("system:workflow", "routed", f.at.Add(-50*time.Minute)))
				if err != nil {
					return err
				}
				f.slots[req.RequirementID] = append(f.slots[req.RequirementID], item.WorkItemID)
				items = append(items, item)
			}
		}
		return nil
	})
	continuation, err := stepapproval.NewContinuation(f.instanceID, prototype.NodeApproval, f.proposal, f.set, items)
	if err != nil {
		t.Fatalf("NewContinuation: %v", err)
	}
	f.continuation = continuation
	return f
}

func (f *wfstep018Fixture) appConn(t *testing.T) *pgxadapter.Conn {
	t.Helper()
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	return conn
}

func (f *wfstep018Fixture) tx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	work006Tx(t, f.conn, f.tenantID, fn)
}

func (f *wfstep018Fixture) driverOn(t *testing.T, conn *pgxadapter.Conn, advance AdvanceFunc) *Driver {
	t.Helper()
	d, err := New(Options{
		DB: conn, Steps: work006EndRunner{}, Advance: advance,
		Terminal: work006Terminal{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
		Clock:     func() time.Time { return f.at.Add(time.Minute) },
	})
	if err != nil {
		t.Fatalf("New driver: %v", err)
	}
	return d
}

func (f *wfstep018Fixture) start() runtime.StartRequest {
	selection := runtime.WorkflowSelection{WorkflowID: f.plan.WorkflowID, Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: f.plan}
	record := version.CompiledVersion{WorkflowID: f.plan.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: f.plan.Digest(), Status: version.StatusActive}
	return runtime.StartRequest{
		TenantID: f.tenantID, CellID: "cell-local", StartIdempotencyKey: "start:wf-step-018",
		Resolver: staticResolver{selection: selection}, Versions: staticVersions{record: record},
		Proposal:      runtime.ProposalBinding{Revision: f.proposal, ApprovalRef: "approval:proposal:1"},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedApprovalFacts(f.proposal),
		BusinessSubjectRefs: []string{"worker:jane"}, ExecutionMode: workflow.ModeExecute,
		CorrelationID: "correlation:wf-step-018",
	}
}

// versions reads the current instance and WorkItem versions a caller presents.
func (f *wfstep018Fixture) versions(t *testing.T, slot uuid.UUID) (instanceVersion, itemVersion int64) {
	t.Helper()
	f.tx(t, func(tx dbport.Tx) error {
		inst, err := (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		if err != nil {
			return err
		}
		item, err := (workitem.Store{}).Load(context.Background(), tx, f.tenantID, slot)
		if err != nil {
			return err
		}
		instanceVersion, itemVersion = inst.InstanceVersion, item.ItemVersion
		return nil
	})
	return instanceVersion, itemVersion
}

func (f *wfstep018Fixture) decision(slot uuid.UUID, requirementID, principal string, outcome intentapproval.Outcome) intentapproval.ApprovalDecision {
	req, _ := f.set.Find(requirementID)
	return intentapproval.ApprovalDecision{
		DecisionID: "decision:" + slot.String() + ":" + principal,
		Binding: intentapproval.DecisionBinding{
			RequirementID: req.RequirementID, RequirementRevision: req.Revision,
			IntentID: f.proposal.IntentID, ProposalRevisionID: f.proposal.ProposalRevisionID,
			ProposalDigest: f.proposal.MaterialDigest, TaskVersion: 1,
			RenderedProjectionDigest: "sha256:" + strings.Repeat("2", 64),
			RequirementDigest:        req.Digest(), ResolutionExpressionDigest: req.ExpressionDigest,
		},
		Outcome: outcome,
		Approver: intentapproval.ApproverReference{
			PrincipalID: principal, IdentityAssuranceRef: "assurance:mfa:1", SessionRef: "session:" + principal, Via: humanwork.SourceDirect,
		},
		AuthorityDecisionRef: "authz:current:wf-step-018", Reason: "reason:" + string(outcome),
		DecidedAt: values.NewInstant(f.at), VoteDigest: "sha256:" + strings.Repeat("3", 64),
	}
}

// request is one approver's vote on slot, claiming and starting the slot on
// the approver's behalf inside the vote transaction.
func (f *wfstep018Fixture) request(t *testing.T, slot uuid.UUID, requirementID, principal string, outcome intentapproval.Outcome) ApprovalCompletionRequest {
	t.Helper()
	instanceVersion, itemVersion := f.versions(t, slot)
	return ApprovalCompletionRequest{
		Start: f.start(), InstanceID: f.instanceID, ExpectedInstanceVersion: instanceVersion,
		WorkItemID: slot, ExpectedWorkItemVersion: itemVersion, Continuation: f.continuation,
		Decision: f.decision(slot, requirementID, principal, outcome), RecordedAt: f.at,
		Meta: work006Meta(principal, "decided", f.at), Authority: &work006Authority{allowed: true},
		Prepare: func(ctx context.Context, ex workitem.Executor, item workitem.WorkItem) (workitem.WorkItem, error) {
			store := workitem.Store{}
			claimed, err := store.Claim(ctx, ex, workitem.ClaimInput{
				TenantID: item.TenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: principal, ClaimExpiresAt: f.at.Add(time.Hour), Now: f.at,
				Meta: work006Meta(principal, "claimed", f.at),
			})
			if err != nil {
				return workitem.WorkItem{}, err
			}
			return store.Start(ctx, ex, item.TenantID, item.WorkItemID, claimed.ItemVersion, f.at, work006Meta(principal, "started", f.at))
		},
	}
}

func (f *wfstep018Fixture) item(t *testing.T, slot uuid.UUID) workitem.WorkItem {
	t.Helper()
	var item workitem.WorkItem
	f.tx(t, func(tx dbport.Tx) error {
		var err error
		item, err = (workitem.Store{}).Load(context.Background(), tx, f.tenantID, slot)
		return err
	})
	return item
}

func (f *wfstep018Fixture) node(t *testing.T, nodeID string) (runtime.NodeExecution, bool) {
	t.Helper()
	var node runtime.NodeExecution
	found := false
	f.tx(t, func(tx dbport.Tx) error {
		rows, err := (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, f.tenantID, f.instanceID)
		for _, row := range rows {
			if row.NodeID == nodeID {
				node, found = row, true
			}
		}
		return err
	})
	return node, found
}

func (f *wfstep018Fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	f.tx(t, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), query, args...).Scan(&n)
	})
	return n
}

func (f *wfstep018Fixture) xmin(t *testing.T, query string, args ...any) string {
	t.Helper()
	var x string
	f.tx(t, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), query, args...).Scan(&x)
	})
	return x
}

// assertVoteAbsent proves slot carries no vote and the approval node has not
// moved.
func (f *wfstep018Fixture) assertVoteAbsent(t *testing.T, slot uuid.UUID) {
	t.Helper()
	if item := f.item(t, slot); item.Status == workitem.StatusCompleted || item.Status == workitem.StatusInProgress {
		t.Fatalf("slot %s = %s, want its routed state", slot, item.Status)
	}
	if n := f.count(t, `SELECT count(*) FROM work_item_decision WHERE tenant_id=$1 AND work_item_id=$2`, f.tenantID, slot); n != 0 {
		t.Fatalf("slot %s has %d decision rows, want none", slot, n)
	}
	if node, _ := f.node(t, prototype.NodeApproval); node.Status != runtime.NodeWaiting {
		t.Fatalf("approval node = %s, want WAITING", node.Status)
	}
	if n := f.count(t, `SELECT count(*) FROM workflow_continuation WHERE tenant_id=$1 AND instance_id=$2`, f.tenantID, f.instanceID); n != 0 {
		t.Fatalf("%d continuations recorded, want none", n)
	}
}

// TestTodo_WF_STEP_018 proves a quorum of two distinct approvers: the first
// vote is durable and leaves the node waiting, the second completes the
// requirement and advances the node in the very transaction that records it,
// and a rejection or an invalidator takes its own route.
func TestTodo_WF_STEP_018(t *testing.T) {
	const board = "approval.board"
	t.Run("two distinct approvers complete and advance in one transaction", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 3})
		driver := f.driverOn(t, f.conn, nil)
		slots := f.slots[board]
		first, err := driver.CompleteApproval(context.Background(), f.request(t, slots[0], board, wfstep018A, intentapproval.OutcomeApproved))
		if err != nil {
			t.Fatalf("first vote: %v", err)
		}
		if !first.Pending || first.Status != StatusParked || first.Resolution.Outcome != "" || len(first.Advances) != 0 {
			t.Fatalf("first vote = %+v, want a pending vote with no advancement", first)
		}
		if item := f.item(t, slots[0]); item.Status != workitem.StatusCompleted || item.CompletedBy != wfstep018A {
			t.Fatalf("first slot = %+v, want COMPLETED by approver A", item)
		}
		if node, _ := f.node(t, prototype.NodeApproval); node.Status != runtime.NodeWaiting {
			t.Fatalf("approval node after one vote = %s, want WAITING", node.Status)
		}

		second, err := driver.CompleteApproval(context.Background(), f.request(t, slots[1], board, wfstep018B, intentapproval.OutcomeApproved))
		if err != nil {
			t.Fatalf("second vote: %v", err)
		}
		if second.Pending || second.Status != StatusComplete || second.Resolution.Outcome != "APPROVED" {
			t.Fatalf("second vote = %+v, want APPROVED and complete", second)
		}
		if len(second.Resolution.DecisionRefs) != 2 {
			t.Fatalf("resolution counted %v, want both durable votes", second.Resolution.DecisionRefs)
		}
		if len(second.Closed) != 1 || second.Closed[0].WorkItemID != slots[2] || second.Closed[0].Status != workitem.StatusCancelled {
			t.Fatalf("closed slots = %+v, want the unneeded third slot cancelled", second.Closed)
		}
		node, _ := f.node(t, prototype.NodeApproval)
		if node.Status != runtime.NodeSucceeded || node.OutputArtifactRef != second.Resolution.Digest {
			t.Fatalf("approval node = %+v, want SUCCEEDED with the resolution digest", node)
		}
		if _, reached := f.node(t, prototype.NodeApproved); !reached {
			t.Fatal("the APPROVED route was not taken")
		}
		// One transaction: the quorum-reaching decision row, the cancelled
		// slot and the advanced node carry the same creating transaction id.
		decisionTx := f.xmin(t, `SELECT xmin::text FROM work_item_decision WHERE tenant_id=$1 AND work_item_id=$2`, f.tenantID, slots[1])
		nodeTx := f.xmin(t, `SELECT xmin::text FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2 AND node_id=$3 AND attempt=1`, f.tenantID, f.instanceID, prototype.NodeApproval)
		closedTx := f.xmin(t, `SELECT xmin::text FROM work_item WHERE tenant_id=$1 AND work_item_id=$2`, f.tenantID, slots[2])
		firstTx := f.xmin(t, `SELECT xmin::text FROM work_item_decision WHERE tenant_id=$1 AND work_item_id=$2`, f.tenantID, slots[0])
		if decisionTx != nodeTx || closedTx != nodeTx {
			t.Fatalf("decision tx %s, cancelled slot tx %s, node advance tx %s: want one transaction", decisionTx, closedTx, nodeTx)
		}
		if firstTx == nodeTx {
			t.Fatal("the pending vote was not committed on its own")
		}

		replayed, err := driver.CompleteApproval(context.Background(), f.request(t, slots[1], board, wfstep018B, intentapproval.OutcomeApproved))
		if err != nil || !replayed.Replay {
			t.Fatalf("replayed final vote = %+v, %v; want a replay", replayed, err)
		}
		if n := f.count(t, `SELECT count(*) FROM work_item_transition WHERE tenant_id=$1 AND to_status='COMPLETED'`, f.tenantID); n != 2 {
			t.Fatalf("COMPLETED transitions after replay = %d, want 2", n)
		}
		if _, err := driver.CompleteApproval(context.Background(), f.request(t, slots[2], board, wfstep018C, intentapproval.OutcomeApproved)); !errors.Is(err, ErrApprovalCompletionConflict) {
			t.Fatalf("vote on the cancelled slot = %v, want ErrApprovalCompletionConflict", err)
		}
	})

	t.Run("a rejection rejects before quorum", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 2})
		driver := f.driverOn(t, f.conn, nil)
		slots := f.slots[board]
		if _, err := driver.CompleteApproval(context.Background(), f.request(t, slots[0], board, wfstep018A, intentapproval.OutcomeApproved)); err != nil {
			t.Fatalf("first vote: %v", err)
		}
		rejected, err := driver.CompleteApproval(context.Background(), f.request(t, slots[1], board, wfstep018B, intentapproval.OutcomeRejected))
		if err != nil {
			t.Fatalf("rejection: %v", err)
		}
		if rejected.Resolution.Outcome != workflow.OutcomeRejected {
			t.Fatalf("resolution = %q, want REJECTED", rejected.Resolution.Outcome)
		}
		if _, reached := f.node(t, prototype.NodeRejected); !reached {
			t.Fatal("the REJECTED route was not taken")
		}
	})

	t.Run("a material change withdraws pending votes and routes INVALIDATED", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 3})
		driver := f.driverOn(t, f.conn, nil)
		slots := f.slots[board]
		if _, err := driver.CompleteApproval(context.Background(), f.request(t, slots[0], board, wfstep018A, intentapproval.OutcomeApproved)); err != nil {
			t.Fatalf("pending vote: %v", err)
		}
		instanceVersion, _ := f.versions(t, slots[0])
		out, err := driver.InvalidateApproval(context.Background(), f.invalidation(instanceVersion, humanwork.InvalidatorMaterialProposalChange))
		if err != nil {
			t.Fatalf("InvalidateApproval: %v", err)
		}
		if out.Resolution.Outcome != "INVALIDATED" || out.Invalidator.RuleID != "rule:material" || out.Status != StatusComplete {
			t.Fatalf("invalidation = %+v, want INVALIDATED under rule:material", out)
		}
		if len(out.Withdrawn) != 1 || out.Withdrawn[0].WorkItemID != slots[0] || out.Withdrawn[0].DecidedBy != wfstep018A {
			t.Fatalf("withdrawn = %+v, want approver A's pending vote", out.Withdrawn)
		}
		if len(out.Cancelled) != 2 {
			t.Fatalf("cancelled = %+v, want both open slots", out.Cancelled)
		}
		if _, reached := f.node(t, prototype.NodeInvalidated); !reached {
			t.Fatal("the INVALIDATED route was not taken")
		}
		var withdrawals []stepapproval.Withdrawal
		f.tx(t, func(tx dbport.Tx) error {
			var err error
			withdrawals, err = stepapproval.LoadWithdrawals(context.Background(), tx, f.tenantID, f.instanceID)
			return err
		})
		if len(withdrawals) != 1 || withdrawals[0].Invalidator.Kind != humanwork.InvalidatorMaterialProposalChange {
			t.Fatalf("durable withdrawals = %+v", withdrawals)
		}
		withdrawalTx := f.xmin(t, `SELECT xmin::text FROM workflow_approval_withdrawal WHERE tenant_id=$1`, f.tenantID)
		nodeTx := f.xmin(t, `SELECT xmin::text FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2 AND node_id=$3 AND attempt=1`, f.tenantID, f.instanceID, prototype.NodeApproval)
		if withdrawalTx != nodeTx {
			t.Fatalf("withdrawal tx %s, node tx %s: want one transaction", withdrawalTx, nodeTx)
		}
		if _, err := driver.InvalidateApproval(context.Background(), f.invalidation(instanceVersion, humanwork.InvalidatorMaterialProposalChange)); !errors.Is(err, ErrApprovalCompletionConflict) {
			t.Fatalf("second invalidation = %v, want ErrApprovalCompletionConflict", err)
		}
	})
}

func (f *wfstep018Fixture) invalidation(instanceVersion int64, kind humanwork.InvalidatorKind) ApprovalInvalidationRequest {
	return ApprovalInvalidationRequest{
		Start: f.start(), InstanceID: f.instanceID, ExpectedInstanceVersion: instanceVersion,
		Continuation: f.continuation, Requirements: f.set, Change: kind,
		Reason: "the proposal was revised", EvidenceRef: "proposal:promotion:wf-step-018:2", RecordedAt: f.at,
		Meta: workitem.TransitionMeta{ActorPrincipalID: "system:invalidator", Reason: "workflow.approval.invalidated"},
	}
}

// TestTodo_WF_STEP_018_Security proves a vote that would break the approval
// policy is refused with nothing written: a duplicate approver on a distinct
// requirement, a separation-of-duties conflict across requirements, a denied
// authority recheck, and a change the requirement does not declare as an
// invalidator.
func TestTodo_WF_STEP_018_Security(t *testing.T) {
	const board = "approval.board"
	t.Run("the same approver twice does not complete a distinct quorum", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 2})
		driver := f.driverOn(t, f.conn, nil)
		slots := f.slots[board]
		if _, err := driver.CompleteApproval(context.Background(), f.request(t, slots[0], board, wfstep018A, intentapproval.OutcomeApproved)); err != nil {
			t.Fatalf("first vote: %v", err)
		}
		_, err := driver.CompleteApproval(context.Background(), f.request(t, slots[1], board, wfstep018A, intentapproval.OutcomeApproved))
		if !errors.Is(err, stepapproval.ErrDuplicateApprover) {
			t.Fatalf("duplicate vote = %v, want ErrDuplicateApprover", err)
		}
		f.assertVoteAbsent(t, slots[1])
		// The slot stays open for a distinct approver, who completes the quorum.
		done, err := driver.CompleteApproval(context.Background(), f.request(t, slots[1], board, wfstep018B, intentapproval.OutcomeApproved))
		if err != nil || done.Resolution.Outcome != "APPROVED" {
			t.Fatalf("distinct second vote = %+v, %v; want APPROVED", done.Resolution, err)
		}
	})

	t.Run("one principal may not fill two requirements", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: "approval.finance", quorum: 1, slots: 1}, wfstep018Slots{id: "approval.manager", quorum: 1, slots: 1})
		driver := f.driverOn(t, f.conn, nil)
		finance, manager := f.slots["approval.finance"][0], f.slots["approval.manager"][0]
		pending, err := driver.CompleteApproval(context.Background(), f.request(t, finance, "approval.finance", wfstep018A, intentapproval.OutcomeApproved))
		if err != nil || !pending.Pending {
			t.Fatalf("finance vote = %+v, %v; want pending on the manager requirement", pending, err)
		}
		_, err = driver.CompleteApproval(context.Background(), f.request(t, manager, "approval.manager", wfstep018A, intentapproval.OutcomeApproved))
		if !errors.Is(err, stepapproval.ErrSeparationConflict) {
			t.Fatalf("second requirement by the same principal = %v, want ErrSeparationConflict", err)
		}
		f.assertVoteAbsent(t, manager)
	})

	t.Run("a denied authority recheck writes nothing", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 2})
		req := f.request(t, f.slots[board][0], board, wfstep018A, intentapproval.OutcomeApproved)
		req.Authority = &work006Authority{allowed: false, reason: "role revoked"}
		if _, err := f.driverOn(t, f.conn, nil).CompleteApproval(context.Background(), req); !errors.Is(err, ErrApprovalAuthorityDenied) {
			t.Fatalf("denied vote = %v, want ErrApprovalAuthorityDenied", err)
		}
		f.assertVoteAbsent(t, f.slots[board][0])
	})

	t.Run("an undeclared change withdraws nothing", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 2})
		driver := f.driverOn(t, f.conn, nil)
		if _, err := driver.CompleteApproval(context.Background(), f.request(t, f.slots[board][0], board, wfstep018A, intentapproval.OutcomeApproved)); err != nil {
			t.Fatalf("pending vote: %v", err)
		}
		instanceVersion, _ := f.versions(t, f.slots[board][0])
		if _, err := driver.InvalidateApproval(context.Background(), f.invalidation(instanceVersion, humanwork.InvalidatorMandatoryDeny)); !errors.Is(err, stepapproval.ErrInvalidatorNotDeclared) {
			t.Fatalf("undeclared invalidator = %v, want ErrInvalidatorNotDeclared", err)
		}
		if n := f.count(t, `SELECT count(*) FROM workflow_approval_withdrawal WHERE tenant_id=$1`, f.tenantID); n != 0 {
			t.Fatalf("an undeclared change withdrew %d decisions", n)
		}
		f.assertVoteAbsent(t, f.slots[board][1])
		if item := f.item(t, f.slots[board][0]); item.Status != workitem.StatusCompleted {
			t.Fatalf("the pending vote = %s, want still COMPLETED and counted", item.Status)
		}
	})
}

// TestTodo_WF_STEP_018_Fault proves the quorum-reaching decision and the
// node advancement commit together or not at all: a fault after the decision
// is written, before or after runtime.Advance has written its own rows, rolls
// every write back, and the same vote then succeeds.
func TestTodo_WF_STEP_018_Fault(t *testing.T) {
	const board = "approval.board"
	boom := errors.New("failpoint")
	for name, failpoint := range map[string]AdvanceFunc{
		"between decision and advance": func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			return runtime.AdvanceReceipt{}, boom
		},
		"after advance, before commit": func(ctx context.Context, ex runtime.Executor, req runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			if _, err := runtime.Advance(ctx, ex, req); err != nil {
				return runtime.AdvanceReceipt{}, err
			}
			return runtime.AdvanceReceipt{}, boom
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 2})
			slots := f.slots[board]
			if _, err := f.driverOn(t, f.conn, nil).CompleteApproval(context.Background(), f.request(t, slots[0], board, wfstep018A, intentapproval.OutcomeApproved)); err != nil {
				t.Fatalf("pending vote: %v", err)
			}
			_, err := f.driverOn(t, f.conn, failpoint).CompleteApproval(context.Background(), f.request(t, slots[1], board, wfstep018B, intentapproval.OutcomeApproved))
			if !errors.Is(err, boom) {
				t.Fatalf("final vote under the failpoint = %v, want the injected fault", err)
			}
			f.assertVoteAbsent(t, slots[1])
			if n := f.count(t, `SELECT count(*) FROM intent_decision`); n != 0 {
				t.Fatalf("intent decisions = %d, want none", n)
			}
			done, err := f.driverOn(t, f.conn, nil).CompleteApproval(context.Background(), f.request(t, slots[1], board, wfstep018B, intentapproval.OutcomeApproved))
			if err != nil || done.Resolution.Outcome != "APPROVED" || done.Status != StatusComplete {
				t.Fatalf("retried final vote = %+v, %v; want APPROVED", done.Resolution, err)
			}
		})
	}

	t.Run("a fault in the caller's own evidence write rolls the vote back", func(t *testing.T) {
		f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 1, slots: 1})
		slot := f.slots[board][0]
		req := f.request(t, slot, board, wfstep018A, intentapproval.OutcomeApproved)
		req.Record = func(context.Context, workitem.Executor, workitem.WorkItem) error { return boom }
		if _, err := f.driverOn(t, f.conn, nil).CompleteApproval(context.Background(), req); !errors.Is(err, boom) {
			t.Fatalf("vote with a failing Record = %v, want the injected fault", err)
		}
		f.assertVoteAbsent(t, slot)
	})
}

// TestTodo_WF_STEP_018_Race proves two approvers casting the final vote at
// the same instant advance the node exactly once.
func TestTodo_WF_STEP_018_Race(t *testing.T) {
	const board = "approval.board"
	f := newWfStep018Fixture(t, wfstep018Slots{id: board, quorum: 2, slots: 3})
	slots := f.slots[board]
	if _, err := f.driverOn(t, f.conn, nil).CompleteApproval(context.Background(), f.request(t, slots[0], board, wfstep018A, intentapproval.OutcomeApproved)); err != nil {
		t.Fatalf("pending vote: %v", err)
	}
	requests := []ApprovalCompletionRequest{
		f.request(t, slots[1], board, wfstep018B, intentapproval.OutcomeApproved),
		f.request(t, slots[2], board, wfstep018C, intentapproval.OutcomeApproved),
	}
	drivers := []*Driver{f.driverOn(t, f.appConn(t), nil), f.driverOn(t, f.appConn(t), nil)}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := range drivers {
		go func(i int) {
			<-start
			_, err := drivers[i].CompleteApproval(context.Background(), requests[i])
			errs <- err
		}(i)
	}
	close(start)
	first, second := <-errs, <-errs
	if (first == nil) == (second == nil) {
		t.Fatalf("racing final votes: %v; %v -- want exactly one to succeed", first, second)
	}
	loser := first
	if loser == nil {
		loser = second
	}
	if !errors.Is(loser, ErrApprovalCompletionConflict) {
		t.Fatalf("losing final vote = %v, want ErrApprovalCompletionConflict", loser)
	}
	if n := f.count(t, `SELECT count(*) FROM work_item_transition WHERE tenant_id=$1 AND to_status='COMPLETED'`, f.tenantID); n != 2 {
		t.Fatalf("COMPLETED transitions = %d, want the pending vote and exactly one final vote", n)
	}
	if n := f.count(t, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1 AND node_id=$2 AND status='SUCCEEDED'`, f.tenantID, prototype.NodeApproved); n != 1 {
		t.Fatalf("APPROVED terminal executions = %d, want exactly one", n)
	}
	if node, _ := f.node(t, prototype.NodeApproval); node.Status != runtime.NodeSucceeded {
		t.Fatalf("approval node = %s, want SUCCEEDED once", node.Status)
	}
}
