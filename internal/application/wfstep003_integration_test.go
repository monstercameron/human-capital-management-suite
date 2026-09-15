package application

// WF-STEP-003 on the served path: the APPROVAL step's stale-authority
// INVALIDATED route, its EXPIRED route, and the duplicate-vote rules, proven
// against the real composition (embedded PostgreSQL, ComposeServe with the
// local-dev routing configuration, the demo workforce and its personas) that
// PROMOUX-015's separation tests drive. Every decision here goes through the
// composed journey engine, i.e. through journey_decide.go's completeApproval
// and the platform execution driver's Resume.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// wfstep003Harness is PROMOUX-015's harness over a cell clock the test can
// move forward (the driver's and the journey engine's "now").
type wfstep003Harness struct {
	*promoux015Harness
	offset *atomic.Int64
}

func wfstep003Compose(t *testing.T) *wfstep003Harness {
	t.Helper()
	offset := &atomic.Int64{}
	now := func() time.Time { return time.Now().UTC().Add(time.Duration(offset.Load())) }
	return &wfstep003Harness{promoux015Harness: promoux015ComposeWith(t, Options{Now: now}), offset: offset}
}

// advance moves the cell clock forward by d.
func (h *wfstep003Harness) advance(d time.Duration) { h.offset.Add(int64(d)) }

// as returns a context for persona's own identity, issued afresh with the
// given roles (nil keeps the persona's roles) and a validity window wide
// enough for a moved cell clock. It is how a role revocation reaches the
// served path: the next credential the principal presents no longer carries
// the role.
func (h *wfstep003Harness) as(persona string, roles []string) context.Context {
	h.t.Helper()
	base, ok := h.principals[persona]
	if !ok {
		h.t.Fatalf("no persona %q", persona)
	}
	if roles == nil {
		roles = base.Roles()
	}
	now := time.Now()
	token, err := h.verifier.Issue(trust.Claims{
		Issuer: h.cfg.Issuer, Audience: h.cfg.Audience, Subject: base.Subject(), SubjectKind: "human", Tenant: h.cfg.Tenant,
		OrganizationScopeID: base.OrganizationScopeID(), Roles: roles, Purposes: base.Purposes(),
		AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-wfstep003-" + persona,
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(7 * 24 * time.Hour).Unix(),
	})
	if err != nil {
		h.t.Fatalf("issue %s credential: %v", persona, err)
	}
	principal, err := h.verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: h.cfg.Audience})
	if err != nil {
		h.t.Fatalf("verify %s credential: %v", persona, err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

// changeManager rewrites the subject's manager relationship to manager.
// journey_worker is append-only and this release has no product write that
// changes a reporting line, so the fixture lifts the append-only trigger for
// one statement to stand in for the HR directory change the recheck must
// observe. It is the one fact JourneyWorkerManagers.CurrentManagerOf reads.
func (h *wfstep003Harness) changeManager(manager string) {
	h.t.Helper()
	ctx := context.Background()
	for _, statement := range []string{
		`ALTER TABLE journey_worker DISABLE TRIGGER journey_worker_append_only`,
		`UPDATE journey_worker SET manager_relationship_ref = '` + manager + `' WHERE worker_key = '` + h.subject + `'`,
		`ALTER TABLE journey_worker ENABLE TRIGGER journey_worker_append_only`,
	} {
		if _, err := h.pool.Exec(ctx, statement); err != nil {
			h.t.Fatalf("change the subject's manager (%s): %v", statement, err)
		}
	}
}

// closure reads the transition that closed node's approval item.
func (h *wfstep003Harness) closure(node string) (status, reason, actor, detail string) {
	h.t.Helper()
	err := h.pool.QueryRow(context.Background(), `
		SELECT w.status, t.reason, t.actor_principal_id, t.detail
		FROM work_item w JOIN work_item_transition t ON t.tenant_id = w.tenant_id AND t.work_item_id = w.work_item_id AND t.to_status = w.status
		WHERE w.node_id = $1`, node).Scan(&status, &reason, &actor, &detail)
	if err != nil {
		h.t.Fatalf("read the %s closure: %v", node, err)
	}
	return status, reason, actor, detail
}

// terminal reports the instance's runtime status and whether endNode ran
// (a SUCCEEDED execution, not a SKIPPED placeholder).
func (h *wfstep003Harness) terminal(endNode string) (string, bool) {
	h.t.Helper()
	ctx := context.Background()
	var status string
	if err := h.pool.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance`).Scan(&status); err != nil {
		h.t.Fatalf("read the workflow instance: %v", err)
	}
	var reached int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_node_execution WHERE node_id = $1 AND status = $2`, endNode, string(runtime.NodeSucceeded)).Scan(&reached); err != nil {
		h.t.Fatalf("read %s: %v", endNode, err)
	}
	return status, reached > 0
}

// decisionRows counts the durable decisions recorded for node's approval.
func (h *wfstep003Harness) decisionRows(node, requirement string) (workItemDecisions, intentDecisions int) {
	h.t.Helper()
	ctx := context.Background()
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM work_item_decision d JOIN work_item w ON w.tenant_id = d.tenant_id AND w.work_item_id = d.work_item_id WHERE w.node_id = $1`, node).Scan(&workItemDecisions); err != nil {
		h.t.Fatalf("count work item decisions: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM intent_decision WHERE decision_kind = 'HUMAN_APPROVAL' AND requirement_id = $1`, requirement).Scan(&intentDecisions); err != nil {
		h.t.Fatalf("count intent decisions: %v", err)
	}
	return workItemDecisions, intentDecisions
}

// TestTodo_WF_STEP_003_ServedAuthorityRecheck proves the decision-time
// authority recheck is real on the served path: the routed approver whose
// authority no longer holds cannot approve, and the approval takes its
// INVALIDATED route durably instead.
func TestTodo_WF_STEP_003_ServedAuthorityRecheck(t *testing.T) {
	t.Run("a manager change invalidates the routed manager's approval", func(t *testing.T) {
		h := wfstep003Compose(t)
		id := h.proposeAndExecute()
		engine := h.composed.Cell().Journey
		if _, err := engine.Decide(h.as("finance-partner", nil), id, workspace.Decision{Approve: true, Reason: "finance approves"}); err != nil {
			t.Fatalf("Decide(finance): %v", err)
		}
		before := h.items(id)[promotionexec.NodeApproveManager]
		if before.owner != promoux015Manager || before.status != string(workitem.StatusAssigned) {
			t.Fatalf("manager approval = %+v, want ASSIGNED to %s", before, promoux015Manager)
		}

		// RED: a wrong candidate is refused before any write.
		if _, err := engine.Decide(h.as("finance-partner", nil), id, workspace.Decision{Approve: true, Reason: "not mine"}); !errors.Is(err, app.ErrProposalDecisionRoute) {
			t.Fatalf("Decide(manager) as a non-candidate = %v, want ErrProposalDecisionRoute", err)
		}
		if after := h.items(id)[promotionexec.NodeApproveManager]; after.version != before.version || after.transitions != before.transitions {
			t.Fatalf("a refused wrong-candidate decision wrote to the approval: %+v -> %+v", before, after)
		}

		// The subject now reports to someone else: Rafael no longer holds
		// CurrentManagerOf(subject).
		h.changeManager(promoux015Employee)
		_, err := engine.Decide(h.as("admin", nil), id, workspace.Decision{Approve: true, Reason: "manager approves"})
		if !errors.Is(err, app.ErrProposalDecisionInvalidated) || !errors.Is(err, workspace.ErrJourneyStage) {
			t.Fatalf("Decide(manager) after the manager changed = %v, want ErrProposalDecisionInvalidated (stage)", err)
		}
		status, reason, actor, detail := h.closure(promotionexec.NodeApproveManager)
		if status != string(workitem.StatusCancelled) || reason != "journey.approval.invalidated" || actor != promoux015Manager || detail == "" {
			t.Fatalf("manager approval closure = %s/%s by %s (%q), want CANCELLED/journey.approval.invalidated by %s with the stale reason",
				status, reason, actor, detail, promoux015Manager)
		}
		if wid, iid := h.decisionRows(promotionexec.NodeApproveManager, promotionexec.ApprovalManager); wid != 0 || iid != 0 {
			t.Fatalf("an invalidated approval recorded decisions: work_item_decision=%d intent_decision=%d, want none", wid, iid)
		}
		runtimeStatus, invalidated := h.terminal(promotionexec.NodeEndInvalidated)
		if !invalidated || runtimeStatus != string(runtime.InstanceSuperseded) {
			t.Fatalf("the workflow did not take the INVALIDATED route: end_invalidated reached=%v, runtime status %s", invalidated, runtimeStatus)
		}
		if _, reachedComplete := h.terminal(promotionexec.NodeWaitEffectiveDate); reachedComplete {
			t.Fatal("an invalidated approval still advanced to the effective-date wait")
		}

		// A retry is not a second chance: nothing is open and nothing moves.
		settled := h.items(id)[promotionexec.NodeApproveManager]
		if _, err := engine.Decide(h.as("admin", nil), id, workspace.Decision{Approve: true, Reason: "again"}); err == nil {
			t.Fatal("a retried decision after invalidation succeeded")
		}
		if after := h.items(id)[promotionexec.NodeApproveManager]; after.version != settled.version || after.transitions != settled.transitions {
			t.Fatalf("a retried decision wrote to the closed approval: %+v -> %+v", settled, after)
		}
	})

	t.Run("a revoked finance role invalidates the finance partner's approval", func(t *testing.T) {
		h := wfstep003Compose(t)
		id := h.proposeAndExecute()
		_, err := h.composed.Cell().Journey.Decide(h.as("finance-partner", []string{"comp_admin"}), id,
			workspace.Decision{Approve: true, Reason: "finance approves without the role"})
		if !errors.Is(err, app.ErrProposalDecisionInvalidated) {
			t.Fatalf("Decide(finance) without the finance_partner role = %v, want ErrProposalDecisionInvalidated", err)
		}
		status, reason, _, _ := h.closure(promotionexec.NodeApproveFinance)
		if status != string(workitem.StatusCancelled) || reason != "journey.approval.invalidated" {
			t.Fatalf("finance approval closure = %s/%s, want CANCELLED/journey.approval.invalidated", status, reason)
		}
		if _, invalidated := h.terminal(promotionexec.NodeEndInvalidated); !invalidated {
			t.Fatal("the workflow did not take the INVALIDATED route")
		}
		if items := h.items(id); items[promotionexec.NodeApproveManager].id != "" {
			t.Fatalf("an invalidated finance approval still raised the manager approval: %+v", items[promotionexec.NodeApproveManager])
		}
	})
}

// TestTodo_WF_STEP_003_ServedExpiry proves a decision after the routed
// deadline takes the EXPIRED route durably instead of approving.
func TestTodo_WF_STEP_003_ServedExpiry(t *testing.T) {
	h := wfstep003Compose(t)
	id := h.proposeAndExecute()
	h.advance(49 * time.Hour)
	_, err := h.composed.Cell().Journey.Decide(h.as("finance-partner", nil), id, workspace.Decision{Approve: true, Reason: "too late"})
	if !errors.Is(err, app.ErrProposalDecisionExpired) || !errors.Is(err, workspace.ErrJourneyStage) {
		t.Fatalf("Decide(finance) after the deadline = %v, want ErrProposalDecisionExpired (stage)", err)
	}
	status, reason, _, _ := h.closure(promotionexec.NodeApproveFinance)
	if status != string(workitem.StatusExpired) || reason != "journey.approval.expired" {
		t.Fatalf("finance approval closure = %s/%s, want EXPIRED/journey.approval.expired", status, reason)
	}
	if wid, iid := h.decisionRows(promotionexec.NodeApproveFinance, promotionexec.ApprovalFinance); wid != 0 || iid != 0 {
		t.Fatalf("an expired approval recorded decisions: work_item_decision=%d intent_decision=%d", wid, iid)
	}
	if runtimeStatus, expired := h.terminal(promotionexec.NodeEndExpired); !expired || runtimeStatus != string(runtime.InstanceCancelled) {
		t.Fatalf("the workflow did not take the EXPIRED route: end_expired reached=%v, runtime status %s", expired, runtimeStatus)
	}
}

// TestTodo_WF_STEP_003_ServedCancellation proves an approval left open on a
// proposal that has been cancelled (here: edited away by its proposer) can
// never approve it. Since WF-RUN-010 the proposer's cancel is a governed
// workflow cancellation: it cancels the bound instance and closes its open
// approval in the same transaction, so the approver's later decision is a
// stage refusal that records nothing.
func TestTodo_WF_STEP_003_ServedCancellation(t *testing.T) {
	h := wfstep003Compose(t)
	id := h.proposeAndExecute()
	engine := h.composed.Cell().Journey
	proposer := h.as("hiring-manager", nil)
	detail, err := engine.Inspect(h.as("admin", nil), id)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if _, _, err := engine.EditProposal(proposer, id, detail.Summary.GovernanceVersion, "idem:wfstep003:edit", "correcting the proposal",
		workspace.EditProposalInput{
			TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", ProposedBase: "99000.00",
			EffectiveDate: h.effective, BusinessReason: "wfstep003 edited",
		}); err != nil {
		t.Fatalf("EditProposal: %v", err)
	}
	_, err = engine.Decide(h.as("finance-partner", nil), id, workspace.Decision{Approve: true, Reason: "finance approves the old proposal"})
	if !errors.Is(err, workspace.ErrJourneyStage) {
		t.Fatalf("Decide(finance) on the cancelled proposal = %v, want a stage refusal", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("Decide(finance) on the cancelled proposal reported a routing failure beside its refusal: %v", err)
	}
	status, reason, _, _ := h.closure(promotionexec.NodeApproveFinance)
	if status != string(workitem.StatusCancelled) || reason != cancellation.WorkItemCancelledReason {
		t.Fatalf("finance approval closure = %s/%s, want CANCELLED/%s", status, reason, cancellation.WorkItemCancelledReason)
	}
	if wid, iid := h.decisionRows(promotionexec.NodeApproveFinance, promotionexec.ApprovalFinance); wid != 0 || iid != 0 {
		t.Fatalf("a cancelled approval recorded decisions: work_item_decision=%d intent_decision=%d", wid, iid)
	}
	if runtimeStatus, _ := h.terminal(promotionexec.NodeEndCancelled); runtimeStatus != string(runtime.InstanceCancelled) {
		t.Fatalf("the governed cancellation left the workflow %s, want CANCELLED", runtimeStatus)
	}
	var decisions int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM workflow_cancellation_decision WHERE decision = 'CANCELLED'`).Scan(&decisions); err != nil || decisions != 1 {
		t.Fatalf("governed cancellation decisions = %d (%v), want one CANCELLED decision", decisions, err)
	}
}

// TestTodo_WF_STEP_003_ServedDuplicateVote proves a duplicate identical vote
// returns the original decision without a write, and a conflicting duplicate
// is refused.
func TestTodo_WF_STEP_003_ServedDuplicateVote(t *testing.T) {
	h := wfstep003Compose(t)
	id := h.proposeAndExecute()
	engine := h.composed.Cell().Journey
	if _, err := engine.Decide(h.as("finance-partner", nil), id, workspace.Decision{Approve: true, Reason: "finance approves"}); err != nil {
		t.Fatalf("Decide(finance): %v", err)
	}
	first, err := engine.Decide(h.as("admin", nil), id, workspace.Decision{Approve: true, Reason: "manager approves"})
	if err != nil {
		t.Fatalf("Decide(manager): %v", err)
	}
	decided := h.items(id)
	manager := decided[promotionexec.NodeApproveManager]
	if manager.status != string(workitem.StatusCompleted) || manager.decidedBy != promoux015Manager {
		t.Fatalf("manager approval = %+v, want COMPLETED and recorded as %s", manager, promoux015Manager)
	}

	again, err := engine.Decide(h.as("admin", nil), id, workspace.Decision{Approve: true, Reason: "manager approves"})
	if err != nil {
		t.Fatalf("a duplicate identical vote = %v, want the original decision", err)
	}
	if again.Summary.Stage != first.Summary.Stage {
		t.Fatalf("duplicate vote stage = %s, want the original %s", again.Summary.Stage, first.Summary.Stage)
	}
	if after := h.items(id)[promotionexec.NodeApproveManager]; after.version != manager.version || after.transitions != manager.transitions {
		t.Fatalf("a duplicate identical vote wrote to the approval: %+v -> %+v", manager, after)
	}
	if wid, iid := h.decisionRows(promotionexec.NodeApproveManager, promotionexec.ApprovalManager); wid != 1 || iid != 1 {
		t.Fatalf("after a duplicate vote work_item_decision=%d intent_decision=%d, want exactly the original one each", wid, iid)
	}

	if _, err := engine.Decide(h.as("admin", nil), id, workspace.Decision{Approve: false, Reason: "changed my mind"}); !errors.Is(err, app.ErrProposalDecisionConflict) {
		t.Fatalf("a conflicting duplicate vote = %v, want ErrProposalDecisionConflict", err)
	}
	if after := h.items(id)[promotionexec.NodeApproveManager]; after.version != manager.version || after.transitions != manager.transitions {
		t.Fatalf("a conflicting duplicate vote wrote to the approval: %+v -> %+v", manager, after)
	}
}
