package runtime_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/workload"
)

func generousLimits() workload.Limits {
	return workload.Limits{MaxBranches: 64, MaxChildren: 16, MaxPayloadBytes: 1 << 20, MaxCostUnits: 10_000, MaxConcurrentInstances: 1000}
}

func gate(limits workload.Limits, payload int, seen *[]workload.Verdict) *runtime.WorkloadGate {
	return &runtime.WorkloadGate{
		Snapshot:     workload.ControlSnapshot{Version: "limits/test/1", Default: limits},
		Criticality:  "P2",
		PayloadBytes: payload,
		Observe: func(_ context.Context, _, _ string, v workload.Verdict) {
			if seen != nil {
				*seen = append(*seen, v)
			}
		},
	}
}

func runtimeCode(err error) string {
	var rerr *runtime.Error
	if errors.As(err, &rerr) {
		return rerr.Code
	}
	return ""
}

func instanceCount(t *testing.T, db *pgtest.DB, tenant uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_instance WHERE tenant_id = $1`, tenant).Scan(&n); err != nil {
		t.Fatalf("count instances: %v", err)
	}
	return n
}

// TestTodo_WF_RUN_021 is the PRIMARY. RED: a branch, child, payload,
// concurrent-activity or cost demand beyond the declared limit is admitted or
// partially scheduled. GREEN: the start is refused OVERLOADED or
// ADMISSION_DEFERRED before any row is written, so the business intent stays
// visible for retry; limits resolve by tenant, capability and criticality.
func TestTodo_WF_RUN_021(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun021")
	pf := newPromotionFixture(t, values.TenantId("wfrun021-tenant"), "intent:wf-run-021")

	start := func(key string, g *runtime.WorkloadGate) error {
		req := pf.baseStartRequest(tenantID, key)
		req.Workload = g
		return inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.Start(context.Background(), tx, req)
			return err
		})
	}

	t.Run("a start inside every limit is admitted and recorded", func(t *testing.T) {
		var seen []workload.Verdict
		if err := start("admitted-1", gate(generousLimits(), 512, &seen)); err != nil {
			t.Fatalf("an in-limit start was refused: %v", err)
		}
		if len(seen) != 1 || seen[0].Outcome != workload.OutcomeAdmitted {
			t.Fatalf("verdicts = %+v, want one ADMITTED", seen)
		}
	})

	for _, tc := range []struct {
		name    string
		limits  func(workload.Limits) workload.Limits
		payload int
		dim     workload.Dimension
	}{
		{"cost", func(l workload.Limits) workload.Limits { l.MaxCostUnits = 1; return l }, 10, workload.DimensionCostUnits},
		{"payload", func(l workload.Limits) workload.Limits { l.MaxPayloadBytes = 100; return l }, 101, workload.DimensionPayloadBytes},
		{"branches", func(l workload.Limits) workload.Limits { l.MaxBranches = 1; return l }, 10, workload.DimensionBranches},
	} {
		t.Run("a start over its "+tc.name+" limit is OVERLOADED and writes nothing", func(t *testing.T) {
			before := instanceCount(t, db, tenantID)
			var seen []workload.Verdict
			err := start("overloaded-"+tc.name, gate(tc.limits(generousLimits()), tc.payload, &seen))
			if runtimeCode(err) != runtime.CodeOverloaded {
				t.Fatalf("start = %v, want %s", err, runtime.CodeOverloaded)
			}
			if after := instanceCount(t, db, tenantID); after != before {
				t.Fatalf("a refused start wrote %d instance rows", after-before)
			}
			if len(seen) != 1 || !strings.Contains(seen[0].Reason(), string(tc.dim)) {
				t.Fatalf("verdict = %+v, want the %s violation named", seen, tc.dim)
			}
		})
	}

	t.Run("a start beyond the tenant's concurrency limit is ADMISSION_DEFERRED, then admitted once a slot frees", func(t *testing.T) {
		limits := generousLimits()
		limits.MaxConcurrentInstances = instanceCount(t, db, tenantID) + 1
		if err := start("concurrency-fill", gate(limits, 10, nil)); err != nil {
			t.Fatalf("filling the last slot: %v", err)
		}
		before := instanceCount(t, db, tenantID)
		err := start("concurrency-over", gate(limits, 10, nil))
		if runtimeCode(err) != runtime.CodeAdmissionDeferred {
			t.Fatalf("start at the concurrency limit = %v, want %s", err, runtime.CodeAdmissionDeferred)
		}
		if after := instanceCount(t, db, tenantID); after != before {
			t.Fatal("a deferred start wrote an instance row")
		}
		db.Exec(t, `UPDATE workflow_instance SET runtime_status = 'COMPLETED', completed_at = now() WHERE tenant_id = $1 AND correlation_id = 'corr-concurrency-fill'`, tenantID)
		if err := start("concurrency-over", gate(limits, 10, nil)); err != nil {
			t.Fatalf("the deferred start retried after a slot freed = %v, want admitted", err)
		}
	})

	t.Run("an idempotent retry of an admitted start is never deferred by its own row", func(t *testing.T) {
		limits := generousLimits()
		limits.MaxConcurrentInstances = instanceCount(t, db, tenantID)
		if err := start("admitted-1", gate(limits, 512, nil)); err != nil {
			t.Fatalf("replaying an admitted start at the concurrency limit = %v, want the original receipt", err)
		}
	})

	t.Run("limits resolve by tenant, capability and criticality, most specific first", func(t *testing.T) {
		snap := workload.ControlSnapshot{Version: "limits/test/2", Default: generousLimits(), Rules: []workload.Rule{
			{CapabilityID: pf.Plan.WorkflowID, Limits: generousLimits()},
			{TenantID: tenantID.String(), CapabilityID: pf.Plan.WorkflowID, Criticality: "P2", Limits: func() workload.Limits { l := generousLimits(); l.MaxCostUnits = 1; return l }()},
		}}
		g := &runtime.WorkloadGate{Snapshot: snap, Criticality: "P2", PayloadBytes: 10}
		if code := runtimeCode(start("specific-rule", g)); code != runtime.CodeOverloaded {
			t.Fatalf("the tenant+capability+criticality rule was not applied: code %q", code)
		}
		g.Criticality = "P0"
		if err := start("general-rule", g); err != nil {
			t.Fatalf("a P0 start fell through to the capability rule and was refused: %v", err)
		}
	})
}

// TestTodo_WF_RUN_021_Race starts more workflows concurrently than the
// concurrency limit allows and proves the advisory lock keeps admission exact:
// exactly the limit are admitted and the rest are deferred, never overrun.
func TestTodo_WF_RUN_021_Race(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "wfrun021-race")
	pf := newPromotionFixture(t, values.TenantId("wfrun021-race-tenant"), "intent:race-021")
	const workers, limit = 8, 3
	limits := generousLimits()
	limits.MaxConcurrentInstances = limit

	errs := make([]error, workers)
	var ready, done sync.WaitGroup
	ready.Add(1)
	for i := 0; i < workers; i++ {
		conn := appConn(t, db)
		done.Add(1)
		go func(i int) {
			defer done.Done()
			ready.Wait()
			req := pf.baseStartRequest(tenantID, "race-021-"+strconv.Itoa(i))
			req.Workload = gate(limits, 10, nil)
			errs[i] = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
				_, err := runtime.Start(context.Background(), tx, req)
				return err
			})
		}(i)
	}
	ready.Done()
	done.Wait()

	admitted, deferred := 0, 0
	for i, err := range errs {
		switch code := runtimeCode(err); {
		case err == nil:
			admitted++
		case code == runtime.CodeAdmissionDeferred:
			deferred++
		default:
			t.Fatalf("concurrent start %d failed unexpectedly: %v", i, err)
		}
	}
	if admitted != limit || deferred != workers-limit {
		t.Fatalf("admitted %d and deferred %d of %d concurrent starts, want exactly %d admitted", admitted, deferred, workers, limit)
	}
	if n := instanceCount(t, db, tenantID); n != limit {
		t.Fatalf("%d instances recorded, want exactly the limit %d", n, limit)
	}
}

// TestTodo_WF_RUN_021_Fault proves a start whose limits cannot be resolved is
// refused rather than admitted: an unversioned snapshot, a non-positive limit
// and ambiguous equally specific rules each fail the start with no row.
func TestTodo_WF_RUN_021_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun021-fault")
	pf := newPromotionFixture(t, values.TenantId("wfrun021-fault-tenant"), "intent:fault-021")
	zero := generousLimits()
	zero.MaxChildren = 0
	half := generousLimits()
	half.MaxCostUnits = 5
	for name, snap := range map[string]workload.ControlSnapshot{
		"unversioned snapshot": {Default: generousLimits()},
		"non-positive limit":   {Version: "v", Default: zero},
		"ambiguous rules": {Version: "v", Default: generousLimits(), Rules: []workload.Rule{
			{TenantID: tenantID.String(), Limits: generousLimits()},
			{CapabilityID: pf.Plan.WorkflowID, Limits: half},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			req := pf.baseStartRequest(tenantID, "fault-"+strings.ReplaceAll(name, " ", "-"))
			req.Workload = &runtime.WorkloadGate{Snapshot: snap, Criticality: "P2"}
			err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
				_, startErr := runtime.Start(context.Background(), tx, req)
				return startErr
			})
			if err == nil {
				t.Fatal("a start with unresolvable limits was admitted")
			}
			if n := instanceCount(t, db, tenantID); n != 0 {
				t.Fatalf("%d instances written by a refused start", n)
			}
		})
	}
}

// TestTodo_WF_RUN_021_Mutation proves the verdict is not vacuous: a demand one
// unit over each limit is refused with that dimension named, and one unit
// under is admitted; the structural/concurrency classification cannot swap.
func TestTodo_WF_RUN_021_Mutation(t *testing.T) {
	limits := workload.Limits{MaxBranches: 3, MaxChildren: 2, MaxPayloadBytes: 100, MaxCostUnits: 50, MaxConcurrentInstances: 4}
	r := workload.Resolved{Limits: limits, SnapshotVersion: "v", Source: "default"}
	at := workload.Demand{Branches: 3, Children: 2, PayloadBytes: 100, CostUnits: 50, ActiveInstances: 3}
	if v := workload.Evaluate(r, at); v.Outcome != workload.OutcomeAdmitted {
		t.Fatalf("a demand exactly at every limit = %s, want ADMITTED", v.Reason())
	}
	for dim, over := range map[workload.Dimension]workload.Demand{
		workload.DimensionBranches:     {Branches: 4, Children: 2, PayloadBytes: 100, CostUnits: 50, ActiveInstances: 3},
		workload.DimensionChildren:     {Branches: 3, Children: 3, PayloadBytes: 100, CostUnits: 50, ActiveInstances: 3},
		workload.DimensionPayloadBytes: {Branches: 3, Children: 2, PayloadBytes: 101, CostUnits: 50, ActiveInstances: 3},
		workload.DimensionCostUnits:    {Branches: 3, Children: 2, PayloadBytes: 100, CostUnits: 51, ActiveInstances: 3},
	} {
		if v := workload.Evaluate(r, over); v.Outcome != workload.OutcomeOverloaded || !strings.Contains(v.Reason(), string(dim)) {
			t.Errorf("one unit over %s = %s, want OVERLOADED naming it", dim, v.Reason())
		}
	}
	concurrent := at
	concurrent.ActiveInstances = 4
	if v := workload.Evaluate(r, concurrent); v.Outcome != workload.OutcomeAdmissionDeferred {
		t.Errorf("one instance over the concurrency limit = %s, want ADMISSION_DEFERRED", v.Reason())
	}
	both := concurrent
	both.CostUnits = 51
	if v := workload.Evaluate(r, both); v.Outcome != workload.OutcomeOverloaded {
		t.Errorf("structural and concurrency violations together = %s, want OVERLOADED (waiting would not help)", v.Reason())
	}
}
