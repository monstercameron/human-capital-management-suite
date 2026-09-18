package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// repairWireTenant seeds the one tenant row the repair record is scoped to and
// returns the pgtest database plus that tenant's id.
func repairWireTenant(t *testing.T, key string) (*pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, key, "workflow repair wire test tenant "+key)
	return db, tenantID
}

func repairWireRecord(tenantID uuid.UUID, stage execute.RepairRecordStage) execute.RepairRecord {
	return execute.RepairRecord{
		TenantID: tenantID, FenceKey: "repair:wire-plan-1:effect:payroll", Stage: stage,
		FenceID: "repair-fence:sha256:wire", PlanDigest: "sha256:wire-plan-1",
		OriginalSemanticKey: "promotion:worker-1:proposal-1", FailedEffectKey: "effect:payroll",
		Status: execute.RepairUnknown, ConsistencyState: "UNKNOWN",
		RecordedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	}
}

// TestPostgresRepairRecordsCommitsEachStageOnItsOwn proves the composition's
// adapter satisfies the workflow engine's port against real PostgreSQL: each
// append runs and commits in its own tenant-scoped transaction -- which is
// what makes a claim written before the corrective effect survive whatever the
// caller does next -- and a repeated stage is refused rather than overwritten.
func TestPostgresRepairRecordsCommitsEachStageOnItsOwn(t *testing.T) {
	db, tenantID := repairWireTenant(t, "wfrun016-wire-commit")
	ctx := context.Background()
	records := PostgresRepairRecords(db.Conn)

	claimed, err := records.AppendRepairRecord(ctx, repairWireRecord(tenantID, execute.RepairStageClaimed))
	if err != nil || !claimed {
		t.Fatalf("claim = %t, %v, want the first writer to win", claimed, err)
	}
	// Committed on its own: a brand-new adapter over the same database reads it.
	reread := PostgresRepairRecords(db.Conn)
	stored, err := reread.LoadRepairRecords(ctx, tenantID, repairWireRecord(tenantID, execute.RepairStageClaimed).FenceKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored) != 1 || stored[0].Stage != execute.RepairStageClaimed {
		t.Fatalf("stored = %+v, want the committed claim", stored)
	}
	if stored[0].Status != execute.RepairUnknown || stored[0].ConsistencyState != "UNKNOWN" ||
		stored[0].OriginalSemanticKey != "promotion:worker-1:proposal-1" || stored[0].FenceID != "repair-fence:sha256:wire" {
		t.Fatalf("round-tripped record lost an identity: %+v", stored[0])
	}
	again, err := records.AppendRepairRecord(ctx, repairWireRecord(tenantID, execute.RepairStageClaimed))
	if err != nil || again {
		t.Fatalf("second claim = %t, %v, want false without an error", again, err)
	}

	settled := repairWireRecord(tenantID, execute.RepairStageSettled)
	settled.Status, settled.Executed, settled.ConsistencyState = execute.RepairCompleted, true, "CONSISTENT"
	settled.EffectRef, settled.EffectResultRef = "operation:payroll-1", "payroll:accepted"
	settled.ObservationState, settled.ObservationComplete = "PAYROLL_EXPECTED", true
	settled.ReconciliationStatus, settled.ReconciliationRoute = "PASS", "CONSISTENT"
	if ok, err := records.AppendRepairRecord(ctx, settled); err != nil || !ok {
		t.Fatalf("settle = %t, %v", ok, err)
	}
	final, err := records.LoadRepairRecords(ctx, tenantID, settled.FenceKey)
	if err != nil {
		t.Fatalf("load settled: %v", err)
	}
	if len(final) != 2 || final[1].Stage != execute.RepairStageSettled || final[1].Status != execute.RepairCompleted ||
		!final[1].Executed || final[1].ConsistencyState != "CONSISTENT" || final[1].ReconciliationRoute != "CONSISTENT" {
		t.Fatalf("settled record = %+v", final)
	}
	empty, err := records.LoadRepairRecords(ctx, tenantID, "repair:never-claimed:effect:none")
	if err != nil || len(empty) != 0 {
		t.Fatalf("unclaimed fence = %d rows, %v", len(empty), err)
	}
}

// TestPostgresRepairRecordsRefusesAnUnscopedCall proves the adapter passes the
// data package's preconditions straight through rather than swallowing them.
func TestPostgresRepairRecordsRefusesAnUnscopedCall(t *testing.T) {
	db, tenantID := repairWireTenant(t, "wfrun016-wire-guard")
	ctx := context.Background()
	records := PostgresRepairRecords(db.Conn)
	if _, err := records.LoadRepairRecords(ctx, uuid.Nil, "repair:plan"); err == nil {
		t.Fatal("an unscoped load was accepted")
	}
	bad := repairWireRecord(tenantID, execute.RepairStageClaimed)
	bad.Status = ""
	if _, err := records.AppendRepairRecord(ctx, bad); err == nil {
		t.Fatal("a record with no status was accepted")
	}
}

// TestComposeWorkflowRepairNeedsADatabaseAndTenantMapping pins the same
// contract composeWorkflowControl has: no execution database or tenant mapping
// means no repair door at all, reported as absence rather than as an error.
func TestComposeWorkflowRepairNeedsADatabaseAndTenantMapping(t *testing.T) {
	db, tenantID := repairWireTenant(t, "wfrun016-wire-compose")
	mapper := func(values.TenantId) uuid.UUID { return tenantID }
	clock := func() time.Time { return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC) }

	withoutDB, _, err := composeWorkflowRepair(nil, mapper, clock, nil, nil, nil, nil, nil)
	if err != nil || withoutDB != nil {
		t.Fatalf("no execution database composed %v, %v; want no repair door and no error", withoutDB, err)
	}
	withoutTenants, _, err := composeWorkflowRepair(db.Conn, nil, clock, nil, nil, nil, nil, nil)
	if err != nil || withoutTenants != nil {
		t.Fatalf("no tenant mapping composed %v, %v; want no repair door and no error", withoutTenants, err)
	}
	controller, composedAuthority, err := composeWorkflowRepair(db.Conn, mapper, clock, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if controller == nil {
		t.Fatal("a cell with a database and tenant mapping composed no repair door")
	}
	// UXLIVE-006: the same resolver the door consults is handed back, so a
	// surface that only asks "could this viewer open it" cannot form a
	// second opinion about authority.
	if composedAuthority == nil {
		t.Fatal("the composed repair door exposed no authority resolver")
	}
}

// TestRepairDefaultsRefuseRatherThanPretend proves the three defaults a cell
// with no external-system adapter composes: the effect refuses, the
// observation reports nothing observed, and the verifier never returns PASS.
// A repair on such a cell stays open, which is the true answer.
func TestRepairDefaultsRefuseRatherThanPretend(t *testing.T) {
	ctx := context.Background()
	if _, err := (unavailableRepairEffect{}).ExecuteRepairEffect(ctx, execute.RepairEffectRequest{}); !errors.Is(err, ErrRepairEffectUnavailable) {
		t.Fatalf("default effect = %v, want ErrRepairEffectUnavailable", err)
	}
	observation, err := (unavailableRepairObservation{}).ObserveRepair(ctx, operationrepair.RepairPlan{}, execute.RepairEffectResult{})
	if err != nil {
		t.Fatal(err)
	}
	if observation.Observed || observation.Complete || observation.State != "UNOBSERVED" {
		t.Fatalf("default observation = %+v, want an explicit non-observation", observation)
	}
	for name, req := range map[string]execute.RepairReconciliationRequest{
		"unobserved": {},
		"observed":   {Observation: execute.RepairObservation{Observed: true, Complete: true, State: "PAYROLL_EXPECTED"}},
	} {
		decision, err := (unverifiableRepair{}).ReconcileRepair(ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if decision.Status == reconcile.CompletionPass || decision.Route == reconcile.RouteConsistent || decision.Terminal {
			t.Fatalf("%s: default verifier closed a repair it cannot verify: %+v", name, decision)
		}
		if decision.NextAction != reconcile.ActionObserve || decision.Reason == "" {
			t.Fatalf("%s: default verifier = %+v, want an observable next action and a reason", name, decision)
		}
	}
	// The two reasons differ, so an operator can tell "nothing was observed"
	// from "nothing can compare it".
	unobserved, _ := (unverifiableRepair{}).ReconcileRepair(ctx, execute.RepairReconciliationRequest{})
	observed, _ := (unverifiableRepair{}).ReconcileRepair(ctx, execute.RepairReconciliationRequest{
		Observation: execute.RepairObservation{Observed: true}})
	if unobserved.Reason == observed.Reason {
		t.Fatalf("both default verdicts read %q", unobserved.Reason)
	}
}
