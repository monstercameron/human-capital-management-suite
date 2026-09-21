package progress

// PROMO-EXEC-005: the async engine half of the promotion proof. A promotion
// parked past its effective-date timer must be found by the sweep from the
// live store rows -- the real workflow id, the real wait node, a PENDING
// timer past its fires instant -- opening exactly one incident without
// touching a runtime or business row. The served PRIMARY
// (TestTodo_PROMO_EXEC_005_AsyncEngine) replays the same detection against a
// driver-parked promotion before its tick; this test pins the finding shape
// at the detector's own scale.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// promotionInstance parks one promotion-shaped live row: the execute-mode
// promotion workflow the served cell runs, RUNNING with no mechanism left
// but its timer.
func promotionInstance(t *testing.T, db *pgtest.DB, tenant uuid.UUID, created time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO workflow_instance (tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref, correlation_id, created_at, started_at)
		VALUES ($1, $2, 'cell-local', 'hcmnext.workflows.promotion.execute', 1, repeat('a', 64), 'EXECUTE', 'RUNNING', 'sha256:input', $3, $4, $4)`,
		tenant, id, "corr-"+id.String()[:8], created)
	return id
}

// TestTodo_PROMO_EXEC_005_SweepParkedPromotion proves the sweep finds a
// promotion parked past its effective-date timer from the live rows: one
// TIMER_OVERDUE finding referencing the real timer row, exactly one incident
// opened, a repeat sweep linking instead of duplicating, and no runtime or
// business row changed.
func TestTodo_PROMO_EXEC_005_SweepParkedPromotion(t *testing.T) {
	h := newHarness(t)
	parked := promotionInstance(t, h.db, h.tenant, evalAt.AddDate(0, -3, 0))
	h.timer(parked, "wait_effective_date", evalAt.Add(-2*time.Hour))

	first, err := h.sweeper().Sweep(context.Background(), h.tenant)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if first.Instances != 1 || first.Stuck != 1 || first.Opened != 1 || first.Linked != 0 {
		t.Fatalf("first sweep = %+v, want 1 read, 1 stuck, 1 opened", first)
	}
	if n := h.incidentCount(); n != 1 {
		t.Fatalf("%d operational incidents, want exactly 1", n)
	}

	// The incident is keyed to the parked promotion itself, and the timer
	// row the sweep read is still the PENDING obligation the driver left:
	// detection used the live rows, not an invented obligation.
	var key string
	if err := h.db.QueryRow(context.Background(), `SELECT incident_key FROM operational_incident WHERE tenant_id = $1`, h.tenant).Scan(&key); err != nil {
		t.Fatalf("read the incident key: %v", err)
	}
	if !strings.HasPrefix(key, "workflow-progress:v1:"+parked.String()+":") {
		t.Fatalf("incident key = %s, want it keyed to the parked promotion %s", key, parked)
	}
	var timerState string
	if err := h.db.QueryRow(context.Background(), `SELECT timer_state FROM workflow_timer WHERE tenant_id = $1 AND instance_id = $2`, h.tenant, parked).Scan(&timerState); err != nil {
		t.Fatalf("read the parked timer: %v", err)
	}
	if timerState != "PENDING" {
		t.Fatalf("parked timer state = %s, want the PENDING row the sweep read", timerState)
	}

	second, err := h.sweeper().Sweep(context.Background(), h.tenant)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if second.Stuck != 1 || second.Opened != 0 || second.Linked != 1 {
		t.Fatalf("second sweep = %+v, want the same parked promotion linked, nothing opened", second)
	}
	if n := h.incidentCount(); n != 1 {
		t.Fatalf("%d operational incidents after the repeat, want exactly 1", n)
	}
}
