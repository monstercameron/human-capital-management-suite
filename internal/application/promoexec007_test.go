package application

// PROMO-EXEC-007: land the commit. A promotion for a worker created through
// the real CreateWorker path (not a seeded corpus worker) must close
// end_complete with the terminal ledger fact and the committed assignment,
// occupancy, pay and budget rows; and when the commit itself fails after a
// VALID revalidation, the failure cause must be recorded on the node
// execution or repair record instead of collapsing to a bare PORT_FAILURE.

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// promoexec007SubjectWorker reads the created subject's aggregate worker
// entity out of the journey projection: journey_worker.worker_id is the
// aggregate worker entity the commit reads.
func promoexec007SubjectWorker(t *testing.T, h *promoux015Harness) uuid.UUID {
	t.Helper()
	var workerID uuid.UUID
	if err := h.pool.QueryRow(context.Background(),
		`SELECT worker_id FROM journey_worker WHERE tenant_id = $1 AND worker_key = $2`,
		pgstore.TenantID(demoworkforce.CompanyKey), h.subject).Scan(&workerID); err != nil {
		t.Fatalf("read the created subject worker: %v", err)
	}
	return workerID
}

// TestTodo_PROMO_EXEC_007_CommitComplete drives the created worker's promotion
// through the served 1.1.0 graph to end_complete and pins every fact the
// commit owes: the PROMOTION_COMPLETE terminal with its ledger fact, and the
// committed assignment, occupancy, pay and budget rows.
func TestTodo_PROMO_EXEC_007_CommitComplete(t *testing.T) {
	h := promoux015Compose(t)
	ctx := context.Background()
	id := h.runSeparatedPromotion()
	approved := h.effects()

	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(ctx)
	if err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after the effective date = %+v, %v; want one timer fired", fired, err)
	}
	h.acknowledgeParkedPromotion(id)
	after := h.effects()
	if err := promoux015CommittedOnce(approved, after); err != nil {
		t.Fatalf("committed effects: %v", err)
	}

	routes := h.wfrun034Routes()
	if !wfrun034Took(routes, promotionexec.NodeAcknowledgeRelease, promotionexec.NodeEndComplete) {
		t.Fatalf("after the acknowledgement the run did not advance %s -> %s; routes %+v",
			promotionexec.NodeAcknowledgeRelease, promotionexec.NodeEndComplete, routes)
	}
	completed := false
	for _, r := range routes {
		completed = completed || (r.kind == "COMPLETE" && r.terminal == "PROMOTION_COMPLETE")
	}
	if !completed {
		t.Fatalf("no PROMOTION_COMPLETE terminal: %+v", routes)
	}
	detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil || detail.GetDetail().GetLedger() == nil {
		t.Fatalf("InspectJourney after the commit = %v, ledger %v; want the terminal ledger fact", err, detail.GetDetail().GetLedger())
	}

	tenantID := pgstore.TenantID(demoworkforce.CompanyKey)
	workerID := promoexec007SubjectWorker(t, h)
	promoted := h.committedPlacement(h.afterEffectiveDate()())
	if promoted.JobCode == "" || promoted.Grade == "" || promoted.BasePay == "" {
		t.Fatalf("committed placement at the effective date = %+v, want the target placement and pay", promoted)
	}

	// The committed assignment names the target job and grade. The worker's
	// pre-promotion assignment row stays current as history (bitemporal), so
	// the assertion scopes to the approved target rather than counting rows.
	var assignmentID, assignmentPosition string
	if err := h.pool.QueryRow(ctx, `
		SELECT a.entity_id::text, coalesce(a.position_ref::text, '') FROM assignment a JOIN employment e
		  ON e.tenant_id = a.tenant_id AND e.entity_id = a.employment_ref
		WHERE a.tenant_id = $1 AND e.worker_ref = $2 AND a.superseded_at IS NULL AND a.primary_flag
		  AND a.job_code = $3 AND a.grade = $4`,
		tenantID, workerID, promoted.JobCode, promoted.Grade).Scan(&assignmentID, &assignmentPosition); err != nil {
		t.Fatalf("read the committed assignment at %s/%s: %v", promoted.JobCode, promoted.Grade, err)
	}
	if assignmentID == "" || assignmentPosition == "" {
		t.Fatalf("committed assignment = %q on position %q, want identities for both", assignmentID, assignmentPosition)
	}

	// The committed occupancy seats the promoted worker on the assignment's
	// target position exactly once, effective on the promotion's date.
	var occupancyDate string
	var occupancies int
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*), max(to_char(o.effective_from AT TIME ZONE 'UTC', 'YYYY-MM-DD')) FROM position_occupancy o
		WHERE o.tenant_id = $1 AND o.worker_ref = $2 AND o.position_ref = $3 AND o.superseded_at IS NULL`,
		tenantID, workerID, assignmentPosition).Scan(&occupancies, &occupancyDate); err != nil {
		t.Fatalf("read the committed occupancy: %v", err)
	}
	if occupancies != 1 || occupancyDate != h.effective {
		t.Fatalf("committed occupancies on the target position = %d effective %s, want exactly one on %s",
			occupancies, occupancyDate, h.effective)
	}

	// The committed base pay carries the approved amount. The pre-promotion
	// component stays current as history, so the assertion scopes to the
	// revision effective on the promotion's date.
	var payAmount, payCurrency string
	if err := h.pool.QueryRow(ctx, `
		SELECT c.amount::text, c.currency FROM compensation_component c
		  JOIN compensation_package p ON p.tenant_id = c.tenant_id AND p.entity_id = c.package_ref AND p.superseded_at IS NULL
		WHERE c.tenant_id = $1 AND p.worker_ref = $2 AND c.component_type = 'BASE_PAY' AND c.superseded_at IS NULL
		  AND c.effective_from >= ($3 || 'T00:00:00Z')::timestamptz`,
		tenantID, workerID, h.effective).Scan(&payAmount, &payCurrency); err != nil {
		t.Fatalf("read the committed base pay: %v", err)
	}
	gotPay, ok := new(big.Rat).SetString(payAmount)
	wantPay, ok2 := new(big.Rat).SetString(promoted.BasePay)
	if !ok || !ok2 || gotPay.Cmp(wantPay) != 0 || payCurrency != promoted.Currency {
		t.Fatalf("committed base pay = %s %s, want the approved %s %s", payAmount, payCurrency, promoted.BasePay, promoted.Currency)
	}

	// The budget hold transitioned to COMMITTED exactly once.
	if n := h.wfrun034Count(`SELECT count(*) FROM budget_reservation WHERE status = 'COMMITTED' AND superseded_at IS NULL`); n != 1 {
		t.Fatalf("committed budget reservations = %d, want 1", n)
	}
	if n := h.wfrun034Count(`SELECT count(*) FROM workflow_node_execution WHERE node_id = $1 AND status = 'SUCCEEDED'`, promotionexec.NodeExecutePromotion); n != 1 {
		t.Fatalf("succeeded execute_promotion attempts = %d, want exactly one", n)
	}
}

// promoexec007TerminateSubject appends a TERMINATED worker revision for the
// created subject, effective now (before the promotion's effective date) and
// recorded now (after the approvals). Revalidation never reads the worker
// lifecycle row, so the run still revalidates VALID and the failure lands in
// the commit itself, exactly like a termination processed between approval
// and effectivity on a live run.
func promoexec007TerminateSubject(t *testing.T, h *promoux015Harness, workerID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tenantID := pgstore.TenantID(demoworkforce.CompanyKey)
	var personRef uuid.UUID
	var workerNumber, workerType string
	if err := h.pool.QueryRow(ctx, `SELECT person_ref, worker_number, worker_type FROM worker
		WHERE tenant_id = $1 AND entity_id = $2 AND superseded_at IS NULL`,
		tenantID, workerID).Scan(&personRef, &workerNumber, &workerType); err != nil {
		t.Fatalf("read the subject worker revision: %v", err)
	}
	now := time.Now().UTC()
	terminated, err := aggregates.NewWorker(tenantID, workerID, personRef, now, nil, now, workerNumber, workerType, "TERMINATED")
	if err != nil {
		t.Fatalf("build the termination revision: %v", err)
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if _, err := (aggregates.PeopleStore{}).PutWorker(ctx, tx, terminated); err != nil {
		t.Fatalf("record the termination: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit the termination: %v", err)
	}
}

// TestTodo_PROMO_EXEC_007_CommitComplete_Recovery is the RECOVERY matrix: the
// worker is terminated after both approvals, so revalidation still confirms
// but the commit refuses. The run must close PROMOTION_REPAIR_REQUIRED with
// the failed execute_promotion attempt carrying its PORT_FAILURE class, and
// the underlying cause must be recorded on the node execution or repair
// record instead of dying in the process log.
func TestTodo_PROMO_EXEC_007_CommitComplete_Recovery(t *testing.T) {
	h := promoux015Compose(t)
	ctx := context.Background()
	id := h.runSeparatedPromotion()
	workerID := promoexec007SubjectWorker(t, h)
	promoexec007TerminateSubject(t, h, workerID)

	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(ctx)
	if err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after the effective date = %+v, %v; want one timer fired", fired, err)
	}
	routes := h.wfrun034Routes()
	if !wfrun034Took(routes, promotionexec.NodeStillValid, promotionexec.NodeExecutePromotion) {
		t.Fatalf("a termination the revalidation cannot see did not reach the commit; routes %+v", routes)
	}
	if wfrun034Took(routes, promotionexec.NodeStillValid, promotionexec.NodeEndBlocked) {
		t.Fatalf("still_valid blocked on a world it confirms; routes %+v", routes)
	}
	repaired := false
	for _, r := range routes {
		repaired = repaired || r.terminal == "PROMOTION_REPAIR_REQUIRED"
	}
	if !repaired {
		t.Fatalf("no PROMOTION_REPAIR_REQUIRED terminal: %+v", routes)
	}
	// The failed attempt keeps its stable PORT_FAILURE class and records the
	// concrete cause on the durable node execution: an operator repairing
	// from this row can tell a baseline refusal from an authorization,
	// resolution or participant failure without replaying the run.
	var status, class string
	if err := h.pool.QueryRow(ctx, `SELECT status, coalesce(error_class,'') FROM workflow_node_execution
		WHERE node_id = $1 ORDER BY attempt DESC LIMIT 1`, promotionexec.NodeExecutePromotion).Scan(&status, &class); err != nil {
		t.Fatalf("read the execute_promotion attempt: %v", err)
	}
	if status != "FAILED" || !strings.HasPrefix(class, "PORT_FAILURE") {
		t.Fatalf("execute_promotion attempt = %s/%s, want a FAILED attempt under the PORT_FAILURE class", status, class)
	}
	if !strings.Contains(class, "authoritative baseline changed") || !strings.Contains(class, "people.worker") {
		t.Fatalf("execute_promotion cause = %q, want the worker baseline refusal recorded on the node execution", class)
	}
	if _, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id}); err != nil {
		t.Fatalf("InspectJourney after the repair close: %v", err)
	}
}
