package workflow_test

import (
	"io"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPromoUXRealServerPromotionContract drives one promotion as four
// personas through the actual gRPC server and PostgreSQL-backed cell. The
// proposer discovers the worker and closed target catalog, finance and the
// manager approve in order, the scheduler crosses the effective-date wait,
// and the proposer reviews the committed ledger fact. The employee is kept in
// the same test because an employee's refusal must not mutate that journey.
func TestPromoUXRealServerPromotionContract(t *testing.T) {
	h := newPromoUXServer(t)

	proposerCtx, proposerCancel := h.call(t, "proposer")
	workers, err := h.client.ListWorkers(proposerCtx, &journeyv1.ListWorkersRequest{})
	proposerCancel()
	if err != nil {
		t.Fatalf("ListWorkers as proposer: %v", err)
	}
	worker, targetJob, targetGrade := promoUXPosition(t, workers)
	if worker.GetWorkerRef() != "omar-reyes" {
		t.Fatalf("selected worker = %q, want the eligible corpus worker", worker.GetWorkerRef())
	}
	if worker.GetJobCode() != "OPS-HRBP2" || worker.GetGrade() != "P2" {
		t.Fatalf("selected worker current placement = %s/%s, want OPS-HRBP2/P2", worker.GetJobCode(), worker.GetGrade())
	}

	expectedRevision := worker.GetSubjectRevision()
	if expectedRevision == "" || expectedRevision != app.PromotionSubjectRevision(worker.GetWorkerRef()) {
		t.Fatalf("ListWorkers subject revision = %q, want the canonical current revision", expectedRevision)
	}

	t.Run("stale subject revision is refused before an intent is created", func(t *testing.T) {
		before := promoUXCount(t, h,
			`SELECT count(*) FROM intent_instance WHERE tenant_id = $1`, pgstore.TenantID(string(fixtures.Tenant)))
		stale := promoUXPropose(worker, targetJob, targetGrade, h.positionRef, "stale-subject", expectedRevision+"-stale")
		ctx, cancel := h.call(t, "proposer")
		defer cancel()
		_, err := h.client.ProposePromotion(ctx, stale)
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("stale ProposePromotion code = %s (%v), want FAILED_PRECONDITION", promoUXStatusCode(err), err)
		}
		after := promoUXCount(t, h,
			`SELECT count(*) FROM intent_instance WHERE tenant_id = $1`, pgstore.TenantID(string(fixtures.Tenant)))
		if after != before {
			t.Fatalf("stale proposal created %d intent rows (before %d), want no durable intent", after, before)
		}
	})

	var proposed *journeyv1.ProposePromotionResponse
	t.Run("duplicate client request replays one immutable proposal", func(t *testing.T) {
		req := promoUXPropose(worker, targetJob, targetGrade, h.positionRef, "duplicate-request", expectedRevision)
		ctx, cancel := h.call(t, "proposer")
		first, err := h.client.ProposePromotion(ctx, req)
		cancel()
		if err != nil {
			t.Fatalf("first duplicate probe: %v; details=%v", err, status.Convert(err).Details())
		}
		ctx, cancel = h.call(t, "proposer")
		second, err := h.client.ProposePromotion(ctx, req)
		cancel()
		if err != nil {
			t.Fatalf("duplicate replay: %v", err)
		}
		if first.GetIntentId() == "" || first.GetIntentId() != second.GetIntentId() ||
			first.GetMaterialDigest() != second.GetMaterialDigest() {
			t.Fatalf("duplicate responses differ: first=%+v second=%+v", first, second)
		}
		proposed = first
	})

	// Continue the idempotency probe's one immutable proposal. Starting a
	// second active promotion for the same worker is correctly refused by the
	// admission guard and would make this lifecycle test contradict the
	// product contract it is meant to verify.
	if proposed == nil {
		t.Fatal("duplicate replay did not retain the proposal to execute")
	}
	if proposed.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		detail := promoUXInspect(t, h, "proposer", proposed.GetIntentId())
		t.Fatalf("proposed stage = %s, want PROPOSED; findings=%v", proposed.GetStage(), detail.GetFindings())
	}
	if proposed.GetIntentId() == "" || proposed.GetProposalRevisionId() == "" || proposed.GetMaterialDigest() == "" {
		t.Fatalf("proposal omitted durable identity: %+v", proposed)
	}

	detail := promoUXInspect(t, h, "proposer", proposed.GetIntentId())
	if !detail.GetDiagnosticsAvailable() || detail.GetJourney().GetMaterialDigest() == "" {
		t.Fatal("authorized compensation administrator did not receive diagnostics")
	}
	if got := detail.GetJourney().GetProposedBase(); got != "98000.00" {
		t.Fatalf("proposed base = %q, want exact Money text 98000.00", got)
	}
	if got := detail.GetJourney().GetTarget(); got.GetJobCode() != targetJob || got.GetGrade() != targetGrade || got.GetPositionId() != h.positionRef {
		t.Fatalf("target placement = %+v, want %s/%s/issued position reference", got, targetJob, targetGrade)
	}
	if detail.GetJourney().GetCurrent().GetJobCode() != worker.GetJobCode() || detail.GetJourney().GetCurrent().GetGrade() != worker.GetGrade() {
		t.Fatalf("current placement was not read from the discovered worker: %+v", detail.GetJourney().GetCurrent())
	}

	ctx, cancel := h.call(t, "proposer")
	executed, err := h.client.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{IntentId: proposed.GetIntentId()})
	cancel()
	if err != nil {
		t.Fatalf("ExecuteJourney: %v", err)
	}
	if got := executed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("after ExecuteJourney stage = %s, want FINANCE_APPROVAL", got)
	}

	// Open the live review feed before either approval. This proves the served
	// stream sees the same durable state as Inspect and gives reconnect a real
	// cursor/sequence pair to resume from.
	watchCtx, watchCancel := h.ctx(t, "proposer", 30*time.Second)
	stream, err := h.client.WatchJourney(watchCtx, &journeyv1.WatchJourneyRequest{IntentId: proposed.GetIntentId()})
	if err != nil {
		watchCancel()
		t.Fatalf("WatchJourney: %v", err)
	}
	first, err := stream.Recv()
	if err != nil {
		watchCancel()
		t.Fatalf("WatchJourney initial Recv: %v", err)
	}
	if got := first.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		watchCancel()
		t.Fatalf("watch initial stage = %s, want FINANCE_APPROVAL", got)
	}
	if first.GetSequence() != 1 || first.GetCursor() == "" {
		watchCancel()
		t.Fatalf("watch initial cursor/sequence = %q/%d, want a numbered cursor", first.GetCursor(), first.GetSequence())
	}
	lastCursor, lastSequence := first.GetCursor(), first.GetSequence()

	ctx, cancel = h.call(t, "employee")
	_, err = h.client.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{IntentId: proposed.GetIntentId(), Approve: false, Reason: "employee must not approve"})
	cancel()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("employee decision code = %s (%v), want PERMISSION_DENIED", promoUXStatusCode(err), err)
	}
	stillFinance := promoUXInspect(t, h, "proposer", proposed.GetIntentId())
	if stillFinance.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("employee denial changed stage to %s", stillFinance.GetJourney().GetStage())
	}

	ctx, cancel = h.call(t, "finance")
	finance, err := h.client.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{IntentId: proposed.GetIntentId(), Approve: true, Reason: "finance approved exact compensation"})
	cancel()
	if err != nil {
		t.Fatalf("finance DecideJourney: %v", err)
	}
	if got := finance.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
		t.Fatalf("after finance approval stage = %s, want MANAGER_APPROVAL", got)
	}
	if finance.GetDetail().GetDiagnosticsAvailable() || finance.GetDetail().GetInstance() != nil ||
		finance.GetDetail().GetJourney().GetMaterialDigest() != "" {
		t.Fatal("ordinary finance reviewer received diagnostic execution data")
	}

	ctx, cancel = h.call(t, "manager")
	manager, err := h.client.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{IntentId: proposed.GetIntentId(), Approve: true, Reason: "manager approved promotion"})
	cancel()
	if err != nil {
		t.Fatalf("manager DecideJourney: %v", err)
	}
	managerDetail := manager.GetDetail()
	if managerDetail.GetDiagnosticsAvailable() || managerDetail.GetInstance() != nil ||
		managerDetail.GetJourney().GetMaterialDigest() != "" {
		t.Fatal("ordinary manager reviewer received diagnostic execution data")
	}
	if got := managerDetail.GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE && got != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		t.Fatalf("after manager approval stage = %s, want WAITING_EFFECTIVE_DATE or scheduler-closed BLOCKED", got)
	}
	// Closing the assigned item removes decision authority, but the recorded
	// reviewer retains narrow read-only access to the case they helped decide.
	// The historical projection must remain business-only for that reviewer.
	ctx, cancel = h.call(t, "manager")
	inspected, err := h.client.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{IntentId: proposed.GetIntentId()})
	cancel()
	if err != nil {
		t.Fatalf("manager post-decision InspectJourney: %v", err)
	}
	managerHistory := inspected.GetDetail()
	if managerHistory.GetDiagnosticsAvailable() || managerHistory.GetInstance() != nil ||
		managerHistory.GetJourney().GetMaterialDigest() != "" {
		t.Fatal("recorded manager reviewer received diagnostic execution data")
	}
	if got := managerHistory.GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE && got != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		t.Fatalf("manager historical stage = %s, want WAITING_EFFECTIVE_DATE or BLOCKED", got)
	}

	// Consume updates until the scheduler crosses the historical effective date
	// and the terminal writer records the promotion. Sequence must advance one
	// step per changed detail; a duplicate or gap is a reconnect-visible bug.
	for i := 0; i < 4 && managerDetail.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED; i++ {
		update, recvErr := stream.Recv()
		if recvErr != nil {
			watchCancel()
			t.Fatalf("WatchJourney update %d: %v", i, recvErr)
		}
		if update.GetSequence() != lastSequence+1 {
			watchCancel()
			t.Fatalf("watch sequence = %d after %d, want exactly one increment", update.GetSequence(), lastSequence)
		}
		lastSequence, lastCursor = update.GetSequence(), update.GetCursor()
		managerDetail = update.GetDetail()
	}
	if managerDetail.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		watchCancel()
		managerDetail = promoUXWaitForStage(t, h, "proposer", proposed.GetIntentId(), journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED)
	}
	watchCancel()

	if lastCursor == "" || lastSequence < 2 {
		t.Fatalf("completed watch did not retain a resumable cursor: %q/%d", lastCursor, lastSequence)
	}
	// A reconnect carrying both the last detail digest and its cursor must not
	// replay the terminal detail. The short deadline is the expected quiet
	// stream result, not a server failure.
	reconnectCtx, reconnectCancel := h.ctx(t, "proposer", 500*time.Millisecond)
	reconnected, err := h.client.WatchJourney(reconnectCtx, &journeyv1.WatchJourneyRequest{
		IntentId: proposed.GetIntentId(), SinceDigest: managerDetail.GetDetailDigest(), ResumeCursor: lastCursor,
	})
	if err != nil {
		reconnectCancel()
		t.Fatalf("WatchJourney reconnect open: %v", err)
	}
	_, err = reconnected.Recv()
	reconnectCancel()
	if err == nil || (status.Code(err) != codes.DeadlineExceeded && err != io.EOF) {
		t.Fatalf("WatchJourney reconnect replay result = %v, want quiet deadline/EOF", err)
	}

	review := promoUXInspect(t, h, "proposer", proposed.GetIntentId())
	if review.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED || review.GetLedger() == nil {
		t.Fatalf("final review = stage %s ledger=%+v, want BLOCKED (WF-RUN-034: revalidation cannot confirm) with ledger fact", review.GetJourney().GetStage(), review.GetLedger())
	}
	if len(review.GetEvidenceIds()) == 0 || len(review.GetTimeline()) == 0 {
		t.Fatalf("final review omitted evidence/timeline: evidence=%v timeline=%v", review.GetEvidenceIds(), review.GetTimeline())
	}
	if got := promoUXCount(t, h, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		pgstore.TenantID(string(fixtures.Tenant)), review.GetLedger().GetStreamKey()); got != 1 {
		t.Fatalf("promotion terminal stream contains %d ledger rows, want exactly one governed fact", got)
	}
}
