package application

// WF-STEP-018 on the served path: the promotion journey decides its approvals
// through the generic approval kernel, so the approver's decision rows and the
// workflow advancement they cause commit in one PostgreSQL transaction or not
// at all.

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// approvalTransactions reads the creating transaction of every row one served
// approval of node writes: its work_item_decision, its intent_decision, the
// node's SUCCEEDED execution and the next approval item the advancement
// raised (when nextNode is not empty).
func (h *wfstep003Harness) approvalTransactions(node, requirement, nextNode string) map[string]string {
	h.t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	for name, query := range map[string]string{
		"work_item_decision": `SELECT d.xmin::text FROM work_item_decision d JOIN work_item w ON w.tenant_id = d.tenant_id AND w.work_item_id = d.work_item_id WHERE w.node_id = '` + node + `'`,
		"intent_decision":    `SELECT xmin::text FROM intent_decision WHERE decision_kind = 'HUMAN_APPROVAL' AND requirement_id = '` + requirement + `'`,
		"node_advance":       `SELECT xmin::text FROM workflow_node_execution WHERE node_id = '` + node + `' AND status = 'SUCCEEDED'`,
	} {
		var xmin string
		if err := h.pool.QueryRow(ctx, query).Scan(&xmin); err != nil {
			h.t.Fatalf("read the %s transaction of %s: %v", name, node, err)
		}
		out[name] = xmin
	}
	if nextNode != "" {
		var xmin string
		if err := h.pool.QueryRow(ctx, `SELECT min(xmin::text) FROM work_item WHERE node_id = $1`, nextNode).Scan(&xmin); err != nil {
			h.t.Fatalf("read the %s work item transaction: %v", nextNode, err)
		}
		out["next_work_item"] = xmin
	}
	return out
}

func (h *wfstep003Harness) exec(statements ...string) {
	h.t.Helper()
	for _, statement := range statements {
		if _, err := h.pool.Exec(context.Background(), statement); err != nil {
			h.t.Fatalf("%s: %v", statement, err)
		}
	}
}

// TestTodo_WF_STEP_018_ServedKernel proves the promotion journey uses the
// generic approval kernel: a failpoint that aborts the finance approval's
// workflow advancement leaves no decision behind at all, and once it is
// removed the same decision, both decision rows, the node advancement and the
// manager approval it raises share one transaction.
func TestTodo_WF_STEP_018_ServedKernel(t *testing.T) {
	h := wfstep003Compose(t)
	id := h.proposeAndExecute()
	engine := h.composed.Cell().Journey
	before := h.items(id)[promotionexec.NodeApproveFinance]

	// Failpoint: the advancement of approve_finance raises inside the vote
	// transaction, after the kernel has completed the WorkItem and written
	// both decision rows.
	h.exec(
		`CREATE FUNCTION wfstep018_failpoint() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.node_id = 'approve_finance' AND NEW.status = 'SUCCEEDED' THEN
				RAISE EXCEPTION 'wfstep018 failpoint: approval advancement';
			END IF;
			RETURN NEW;
		END $$`,
		`CREATE TRIGGER wfstep018_failpoint BEFORE INSERT OR UPDATE ON workflow_node_execution
			FOR EACH ROW EXECUTE FUNCTION wfstep018_failpoint()`,
	)
	if _, err := engine.Decide(h.as("finance-partner", nil), id, workspace.Decision{Approve: true, Reason: "finance approves"}); err == nil {
		t.Fatal("Decide(finance) succeeded although its workflow advancement failed")
	}
	after := h.items(id)
	if got := after[promotionexec.NodeApproveFinance]; got.status != before.status || got.version != before.version || got.transitions != before.transitions {
		t.Fatalf("the failed decision left writes on the finance approval: %+v -> %+v", before, got)
	}
	if wid, iid := h.decisionRows(promotionexec.NodeApproveFinance, promotionexec.ApprovalFinance); wid != 0 || iid != 0 {
		t.Fatalf("the failed decision left work_item_decision=%d intent_decision=%d, want neither", wid, iid)
	}
	if manager := after[promotionexec.NodeApproveManager]; manager.id != "" {
		t.Fatalf("the failed decision raised the manager approval: %+v", manager)
	}

	h.exec(`DROP TRIGGER wfstep018_failpoint ON workflow_node_execution`, `DROP FUNCTION wfstep018_failpoint()`)
	if _, err := engine.Decide(h.as("finance-partner", nil), id, workspace.Decision{Approve: true, Reason: "finance approves"}); err != nil {
		t.Fatalf("Decide(finance) after the failpoint was removed: %v", err)
	}
	finance := h.approvalTransactions(promotionexec.NodeApproveFinance, promotionexec.ApprovalFinance, promotionexec.NodeApproveManager)
	for name, xmin := range finance {
		if xmin != finance["node_advance"] {
			t.Fatalf("finance approval transactions = %v: %s committed apart from the node advancement", finance, name)
		}
	}
	if got := h.items(id)[promotionexec.NodeApproveFinance]; got.status != string(workitem.StatusCompleted) || got.decisionActor != promoux015Finance {
		t.Fatalf("finance approval = %+v, want COMPLETED and decided by %s", got, promoux015Finance)
	}

	if _, err := engine.Decide(h.as("admin", nil), id, workspace.Decision{Approve: true, Reason: "manager approves"}); err != nil {
		t.Fatalf("Decide(manager): %v", err)
	}
	manager := h.approvalTransactions(promotionexec.NodeApproveManager, promotionexec.ApprovalManager, "")
	for name, xmin := range manager {
		if xmin != manager["node_advance"] {
			t.Fatalf("manager approval transactions = %v: %s committed apart from the node advancement", manager, name)
		}
	}
}
