package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestReadyRedelivererRefusesAnotherTenant pins the adapter's tenant fence and
// what it forwards: an orphan of another tenant never reaches the cell, and a
// configured tenant's orphan arrives with its instance and version.
func TestReadyRedelivererRefusesAnotherTenant(t *testing.T) {
	configured := pgstore.TenantID("tenant-a")
	var (
		calls    int
		instance string
		version  int64
	)
	redeliver := readyRedeliverer(configured.String(), "tenant-a", func(_ context.Context, instanceID string, expected int64) (app.ExecutionResult, error) {
		calls++
		instance, version = instanceID, expected
		return app.ExecutionResult{}, nil
	})
	foreign := wfrecover.Redelivery{Orphan: wfrecover.Orphan{TenantID: pgstore.TenantID("tenant-b"), InstanceID: uuid.New(), InstanceVersion: 3}}
	if err := redeliver.Redeliver(context.Background(), foreign); err == nil || calls != 0 {
		t.Fatalf("foreign orphan = %v (calls %d), want a refusal before the cell", err, calls)
	}
	own := wfrecover.Redelivery{Orphan: wfrecover.Orphan{TenantID: configured, InstanceID: uuid.New(), InstanceVersion: 7}}
	if err := redeliver.Redeliver(context.Background(), own); err != nil || calls != 1 ||
		instance != own.Orphan.InstanceID.String() || version != 7 {
		t.Fatalf("own orphan = %v: calls %d instance %s version %d", err, calls, instance, version)
	}
	failing := readyRedeliverer(configured.String(), "tenant-a", func(context.Context, string, int64) (app.ExecutionResult, error) {
		return app.ExecutionResult{}, errors.New("cell refused")
	})
	if err := failing.Redeliver(context.Background(), own); err == nil {
		t.Fatal("a failed cell redelivery was reported as success")
	}
	if _, err := composeRecoveryRole(nil, "replica", configured.String(), "tenant-a", nil); err == nil {
		t.Fatal("a recovery role without a pool was composed")
	}
	var cell *app.Cell
	if _, err := cell.RedeliverReady(app.WithResumeTenant(context.Background(), "tenant-a"), uuid.NewString(), 1); err == nil {
		t.Fatal("a nil cell redelivered")
	}
}

// TestTodo_WF_RUN_003_ServeRedelivery runs WF-RUN-003's recovery role on the
// production serve composition. A promotion is driven through both separated
// approvals to its effective-date wait; a driver then commits the wait's
// advancement and dies before draining the READY revalidation it derived,
// holding its instance lease, with no ready-work row left behind. The
// composed sweep takes the lapsed lease over, reconstructs the pinned start
// from durable rows, and redelivers the drain through the served driver: the
// promotion reaches its terminal record exactly once, and nothing is
// redelivered twice.
func TestTodo_WF_RUN_003_ServeRedelivery(t *testing.T) {
	h := promoux015Compose(t)
	ctx := context.Background()
	intentID := h.runSeparatedPromotion()
	instanceID := uuid.MustParse(h.latestInstance())
	tenantID := pgstore.TenantID(demoworkforce.CompanyKey)
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}

	dead := lease.Identity{WorkloadRef: "workload:hcmnext-execution", InstanceRef: "replica:dies-after-wait"}
	diedAt := time.Now().UTC()
	var deadToken uint64
	inServeTenantTx(t, h, tenantID, func(tx dbport.Tx) error {
		inst, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, instanceID)
		if err != nil {
			return err
		}
		if inst.CompiledPlanHash != plan.Digest() {
			t.Fatalf("served instance pins %s, the executable plan digests to %s", inst.CompiledPlanHash, plan.Digest())
		}
		// The dead driver's committed wait advancement: revalidation is READY
		// and nothing ran it.
		if _, err := runtime.Advance(ctx, tx, runtime.AdvanceRequest{
			TenantID: tenantID, InstanceID: instanceID, ExpectedInstanceVersion: inst.InstanceVersion, Attempt: 1, Plan: plan,
			Outcome:    frontier.NodeOutcome{NodeID: promotionexec.NodeWaitEffectiveDate, Outcome: workflow.OutcomeSucceeded},
			RecordedAt: diedAt, Sink: runtime.ContinuationStore{},
		}); err != nil {
			return err
		}
		grant, err := lease.Manager{}.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenantID, Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: instanceID.String()},
			Holder: dead, Now: diedAt, TTL: time.Second,
		})
		deadToken = grant.Fence.Token
		return err
	})
	if status := serveNodeStatus(t, h, instanceID, promotionexec.NodeRevalidate); status != string(runtime.NodeReady) {
		t.Fatalf("revalidation before redelivery = %s, want READY: the fixture did not lose the drain", status)
	}
	effectsBefore := h.effects()

	sweeper, err := composeRecoveryRole(h.pool, "wfrun003-serve", tenantID.String(), demoworkforce.CompanyKey, h.composed.Cell().RedeliverReady)
	if err != nil {
		t.Fatal(err)
	}
	now := diedAt.Add(time.Minute)
	receipt, err := sweeper.Sweep(ctx, tenantID, now)
	if err != nil || receipt.Count(wfrecover.SweepRedelivered) != 1 {
		t.Fatalf("serve recovery sweep = %+v, %v; want one redelivery", receipt, err)
	}
	// The redelivered drain parks on the acknowledgement gate; the HR
	// operator's attestation clears it and the promotion commits exactly
	// once.
	h.acknowledgeParkedPromotion(intentID)
	if err := promoux015CommittedOnce(effectsBefore, h.effects()); err != nil {
		t.Fatalf("the redelivered drain's commit: %v", err)
	}
	var state, holder string
	var token int64
	if err := h.pool.QueryRow(ctx, `SELECT lease_state, holder_id, fence_token FROM workflow_lease
		WHERE resource_kind = 'WORKFLOW_INSTANCE' AND resource_id = $1 ORDER BY fence_token DESC LIMIT 1`,
		instanceID.String()).Scan(&state, &holder, &token); err != nil {
		t.Fatal(err)
	}
	// The lease chain is dead driver, sweeper takeover, then the payroll and
	// identity providers' confirmation resumes (promotion 1.1.0) and the
	// acknowledgement's own fenced resume: the final release belongs to the
	// execution workload four tokens above the dead driver's.
	if state != "RELEASED" || !strings.HasPrefix(holder, "workload:hcmnext-execution#") || token != int64(deadToken)+4 {
		t.Fatalf("instance lease after redelivery = %s/%s/%d, want RELEASED by the acknowledgement resume at %d", state, holder, token, deadToken+4)
	}
	detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("InspectJourney after redelivery: %v", err)
	}
	// WF-RUN-034: the served promotion runs real steps and its recorded
	// GOVERN-002 approval confirms at the effective date, so a redelivered run
	// commits and closes RECORDED with its one outcome fact.
	if detail.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED || detail.GetDetail().GetLedger() == nil {
		t.Fatalf("redelivered promotion = stage %s ledger %v, want RECORDED with its one outcome fact",
			detail.GetDetail().GetJourney().GetStage(), detail.GetDetail().GetLedger())
	}

	committed := h.effects()
	again, err := sweeper.Sweep(ctx, tenantID, now.Add(time.Hour))
	if err != nil || len(again.Items) != 0 || h.effects() != committed {
		t.Fatalf("second serve sweep = %+v, %v (effects %+v -> %+v); want nothing to redeliver", again, err, committed, h.effects())
	}
}

func serveNodeStatus(t *testing.T, h *promoux015Harness, instanceID uuid.UUID, nodeID string) string {
	t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(), `SELECT status FROM workflow_node_execution
		WHERE instance_id = $1 AND node_id = $2 ORDER BY attempt DESC LIMIT 1`, instanceID, nodeID).Scan(&status); err != nil {
		t.Fatalf("read %s status: %v", nodeID, err)
	}
	return status
}

func inServeTenantTx(t *testing.T, h *promoux015Harness, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("serve tenant transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
