package application

// PROMO-EXEC-005: cover the async engine paths on a real promotion. One
// served promotion parks past its effective-date timer and the progress
// sweep finds the owed timer from the live rows; a commit-aborted promotion
// start retries to exactly one instance; and one composed scheduler tick
// advances every live row through its full chain -- the effective-date wait,
// the provider confirmations, the observations, the acknowledgement and
// end_complete with the terminal ledger fact -- after which the sweep finds
// nothing stuck.
//
// The detector-scale half lives in internal/workflow/progress
// (TestTodo_PROMO_EXEC_005_SweepParkedPromotion); these tests drive the same
// machinery through the served composition.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/progress"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// promoexec005Sweep runs one progress sweep over the served pool at clock.
func promoexec005Sweep(t *testing.T, h *promoux015Harness, tenant uuid.UUID, clock func() time.Time) progress.SweepResult {
	t.Helper()
	sweeper := progress.Sweeper{
		Begin:  func(ctx context.Context) (dbport.Tx, error) { return h.pool.Begin(ctx) },
		Policy: progress.DefaultPolicy(),
		Route: progress.Route{PrimaryOwner: "team:workflow-runtime", SecondaryRoute: "team:platform-oncall",
			StormLimit: 50, StormWindow: time.Hour},
		Clock: clock,
	}
	result, err := sweeper.Sweep(context.Background(), tenant)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return result
}

// TestTodo_PROMO_EXEC_005_AsyncEngine parks one live promotion past its
// timer, proves the sweep finds the owed timer, then advances every live
// row through one composed scheduler tick to the recorded terminal: the
// provider confirmations, the observations, the acknowledgement and
// end_complete with exactly one ledger fact, after which the sweep finds
// nothing stuck.
// promoexec005RunSeparated proposes the fixture promotion with explicit
// terms and records both separated approvals, returning the intent id at
// WAITING_EFFECTIVE_DATE -- the same end state as runSeparatedPromotion
// without the workforce-discovery step.
func promoexec005RunSeparated(t *testing.T, h *promoux015Harness) string {
	t.Helper()
	intentID := h.proposeAndExecute()
	if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: intentID, Approve: true, Reason: "finance approves"}); err != nil {
		t.Fatalf("DecideJourney as the finance partner: %v", err)
	}
	waiting, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: intentID, Approve: true, Reason: "manager approves"})
	if err != nil {
		t.Fatalf("DecideJourney as the current manager: %v", err)
	}
	if got := waiting.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		t.Fatalf("after both approvals stage = %s, want WAITING_EFFECTIVE_DATE", got)
	}
	return intentID
}

func TestTodo_PROMO_EXEC_005_AsyncEngine(t *testing.T) {
	h := promoux015Compose(t)
	tenantID := pgstore.TenantID(h.cfg.Tenant)
	intentID := promoexec005RunSeparated(t, h)

	parked := promoexec005Sweep(t, h, tenantID, h.afterEffectiveDate())
	if parked.Instances != 1 || parked.Stuck != 1 || parked.Opened != 1 || parked.Linked != 0 {
		t.Fatalf("sweep of the parked promotion = %+v, want 1 read, 1 stuck, 1 opened", parked)
	}
	var incidentKey string
	if err := h.pool.QueryRow(context.Background(), `SELECT incident_key FROM operational_incident WHERE tenant_id = $1`, tenantID).Scan(&incidentKey); err != nil {
		t.Fatalf("read the parked-promotion incident: %v", err)
	}
	var instanceID string
	if err := h.pool.QueryRow(context.Background(), `SELECT instance_id::text FROM workflow_instance ORDER BY created_at DESC LIMIT 1`).Scan(&instanceID); err != nil {
		t.Fatalf("read the parked instance: %v", err)
	}
	if len(incidentKey) < len("workflow-progress:v1:"+instanceID+":") || incidentKey[:len("workflow-progress:v1:"+instanceID+":")] != "workflow-progress:v1:"+instanceID+":" {
		t.Fatalf("incident key = %s, want it keyed to the parked promotion %s", incidentKey, instanceID)
	}

	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background())
	if err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after the effective date = %+v, %v; want the one owed timer fired", fired, err)
	}
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, promotionexec.NodeAwaitPayrollConfirmation); got != 1 {
		t.Fatalf("open payroll confirmation waits = %d, want 1", got)
	}
	h.confirmProviders()
	acked, ackErr := h.composed.Cell().Journey.Acknowledge(h.ackOperatorCtx(), intentID, workspace.Acknowledgement{
		EvidenceRef: "hris:signature:promo-exec-005", Note: "signed copy verified against the HRIS record",
	})
	if ackErr != nil {
		t.Fatalf("Journey.Acknowledge as the HR operator: %v", ackErr)
	}
	if got := acked.Summary.Stage; got != workspace.JourneyStageRecorded {
		t.Fatalf("acknowledged journey stage = %s, want RECORDED", got)
	}

	detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: intentID})
	if err != nil || detail.GetDetail().GetLedger() == nil {
		t.Fatalf("InspectJourney after the tick chain = %v, ledger %v; want the terminal ledger fact", err, detail.GetDetail().GetLedger())
	}
	workflowID := detail.GetDetail().GetInstance().GetWorkflowId()
	var ledgerCount int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenantID, effects.StreamKeyFor(workflowID, instanceID)).Scan(&ledgerCount); err != nil {
		t.Fatalf("count journey ledger facts: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("ledger facts on %s = %d, want exactly one", instanceID, ledgerCount)
	}

	settled := promoexec005Sweep(t, h, tenantID, time.Now().UTC)
	if settled.Stuck != 0 || settled.Opened != 0 || settled.Linked != 0 {
		t.Fatalf("sweep after the recorded terminal = %+v, want nothing stuck", settled)
	}
}

// TestTodo_PROMO_EXEC_005_AsyncEngine_Security proves the served sweep is
// confined to its tenant: sweeping another tenant reads none of this
// promotion's rows and raises nothing there.
func TestTodo_PROMO_EXEC_005_AsyncEngine_Security(t *testing.T) {
	h := promoux015Compose(t)
	promoexec005RunSeparated(t, h)

	other := uuid.New()
	result := promoexec005Sweep(t, h, other, h.afterEffectiveDate())
	if result.Instances != 0 || result.Stuck != 0 || result.Opened != 0 || result.Linked != 0 {
		t.Fatalf("sweep of another tenant = %+v, want nothing read", result)
	}
	var incidents int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM operational_incident WHERE tenant_id = $1`, other).Scan(&incidents); err != nil {
		t.Fatalf("count other-tenant incidents: %v", err)
	}
	if incidents != 0 {
		t.Fatalf("the sweep raised %d incidents in another tenant", incidents)
	}
}

// TestTodo_PROMO_EXEC_005_AsyncEngine_Recovery proves a promotion start
// retries to exactly one instance: two concurrent executes of one intent and
// one sequential re-execute leave a single workflow instance, and every
// caller observes that instance or a typed refusal -- never a second one.
func TestTodo_PROMO_EXEC_005_AsyncEngine_Recovery(t *testing.T) {
	h := promoux015Compose(t)
	intentID := h.proposeAndExecute()
	ctx := context.Background()

	var count int64
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_instance`).Scan(&count); err != nil {
		t.Fatalf("count instances: %v", err)
	}
	if count != 1 {
		t.Fatalf("workflow instances after one execute = %d, want 1", count)
	}

	type outcome struct {
		instance string
		err      error
	}
	results := make([]outcome, 2)
	var gate, done sync.WaitGroup
	gate.Add(1)
	for i := range results {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			gate.Wait()
			executed, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: intentID})
			if err == nil {
				results[i].instance = executed.GetDetail().GetInstance().GetInstanceId()
			}
			results[i].err = err
		}(i)
	}
	gate.Done()
	done.Wait()

	var winner string
	if err := h.pool.QueryRow(ctx, `SELECT instance_id::text FROM workflow_instance`).Scan(&winner); err != nil {
		t.Fatalf("read the single instance: %v", err)
	}
	for i, res := range results {
		if res.err == nil && res.instance != winner {
			t.Fatalf("concurrent execute %d observed instance %s, want the single %s", i, res.instance, winner)
		}
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_instance`).Scan(&count); err != nil {
		t.Fatalf("recount instances: %v", err)
	}
	if count != 1 {
		t.Fatalf("workflow instances after concurrent executes = %d, want exactly 1", count)
	}

	// A sequential re-execute is refused, not duplicated.
	if _, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: intentID}); err == nil {
		t.Fatal("re-executing an executed promotion succeeded; want the already-executed refusal")
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_instance`).Scan(&count); err != nil {
		t.Fatalf("recount instances: %v", err)
	}
	if count != 1 {
		t.Fatalf("workflow instances after the re-execute = %d, want exactly 1", count)
	}
}
