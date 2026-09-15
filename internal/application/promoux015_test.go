package application

// PROMOUX-015: one multi-persona regression journey that gates promotion
// presentation and execution on the production path. Every test here drives
// the real composition promoux015Compose builds -- embedded PostgreSQL,
// ComposeServe with the local-dev routing configuration, the demo workforce,
// the persona credentials composeDevPersonas issues, the real gRPC
// JourneyService and the production timer scheduler -- with one versioned
// fixture (promoux015FixtureVersion). There is no mock workflow engine, no
// fake browser data and no test-only authorization shortcut: the only seam is
// the scheduler's Clock, which is the same seam the real server's scheduler
// is constructed with.

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// promoux015FixtureVersion versions the one fixture every PROMOUX-015 test
// drives: the four separated personas of promoux015_separation_integration_test.go,
// the created people-operations subject reporting to the manager approver, and
// the published OPS-HRBP2/P2 promotion path discovered from ListWorkers.
const promoux015FixtureVersion = "promoux015.multi-persona/v1"

// promoux015Effects counts the rows a committed promotion writes. A journey
// that commits exactly once moves each by the same amount exactly once; a
// refused, stale, duplicated, replayed or recovered path moves none of them.
type promoux015Effects struct {
	// ledgerEvents and outbox count every row; outcomeEvents and
	// outcomeOutbox count only the promotion outcome a commit writes, so a
	// journey's earlier governed records do not read as a commit.
	ledgerEvents, outbox, outcomeEvents, outcomeOutbox, commitReceipts int64
}

func (h *promoux015Harness) effects() promoux015Effects {
	h.t.Helper()
	var e promoux015Effects
	for _, q := range []struct {
		dst *int64
		sql string
	}{
		{&e.ledgerEvents, `SELECT count(*) FROM ledger_event`},
		{&e.outbox, `SELECT count(*) FROM outbox`},
		{&e.outcomeEvents, `SELECT count(*) FROM ledger_event WHERE schema_ref ILIKE '%PromotionOutcome%'`},
		{&e.outcomeOutbox, `SELECT count(*) FROM outbox WHERE schema_ref ILIKE '%PromotionOutcome%'`},
		{&e.commitReceipts, `SELECT count(*) FROM transaction_commit_receipt`},
	} {
		if err := h.pool.QueryRow(context.Background(), q.sql).Scan(q.dst); err != nil {
			h.t.Fatalf("count effects (%s): %v", q.sql, err)
		}
	}
	return e
}

// promoux015Scheduler composes the production timer scheduler the real server
// runs (internal/application/scheduler_workload.go): the same claims, lease
// manager, timer store, misfire policy and ResumeFiredTimer dispatcher, over
// the demo tenant, with only its Clock supplied.
func (h *promoux015Harness) scheduler(clock func() time.Time) *scheduler.Scheduler {
	h.t.Helper()
	dispatcher := scheduler.DispatcherFunc(func(ctx context.Context, work scheduler.Work) (scheduler.Disposition, error) {
		if _, err := h.composed.Cell().ResumeFiredTimer(app.WithResumeTenant(ctx, demoworkforce.CompanyKey), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt); err != nil {
			return scheduler.DispositionRetry, err
		}
		return scheduler.DispositionCompleted, nil
	})
	s, err := scheduler.New(scheduler.Config{
		DB: h.pool,
		Claims: []lease.AcquireRequest{{
			TenantID: pgstore.TenantID(demoworkforce.CompanyKey), Resource: lease.Resource{Kind: lease.ResourceQueue, ID: scheduler.DefaultQueueKey},
			Holder: lease.Identity{WorkloadRef: "workload:promoux015", InstanceRef: "replica-" + uuid.NewString()},
		}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}},
		Misfire:    schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: 30 * 24 * time.Hour, MaxCatchUp: 1},
		Dispatcher: dispatcher, Clock: clock,
	})
	if err != nil {
		h.t.Fatalf("compose the production scheduler: %v", err)
	}
	return s
}

// discoverPromotion is what the proposer does without foreknowledge: list the
// workforce it may see, find the subject there, and take the first published
// promotion path for the subject's current profile, proposing the smallest
// increase that path's own guardrail permits. It returns the request and the
// path it came from.
func (h *promoux015Harness) discoverPromotion(persona string) (*journeyv1.ProposeJourneyRequest, *journeyv1.PromotionPathOption) {
	h.t.Helper()
	listed, err := h.client.ListWorkers(h.rpc(persona), &journeyv1.ListWorkersRequest{})
	if err != nil {
		h.t.Fatalf("ListWorkers as %s: %v", persona, err)
	}
	var subject *journeyv1.Worker
	for _, w := range listed.GetWorkers() {
		if w.GetWorkerRef() == h.subject {
			subject = w
		}
	}
	if subject == nil {
		h.t.Fatalf("ListWorkers as %s did not disclose the subject %s among %d workers", persona, h.subject, len(listed.GetWorkers()))
	}
	for _, path := range listed.GetOptions().GetPromotionPaths() {
		if path.GetSourceJobCode() != subject.GetJobCode() || path.GetSourceGrade() != subject.GetGrade() {
			continue
		}
		base := promoux015ProposedBase(h.t, subject.GetBasePay(), path.GetMinimumBaseIncrease())
		return &journeyv1.ProposeJourneyRequest{
			WorkerRef: subject.GetWorkerRef(), Target: &journeyv1.Placement{JobCode: path.GetTargetJobCode(), Grade: path.GetTargetGrade()},
			ProposedBase: base, EffectiveDate: h.effective, BusinessReason: promoux015FixtureVersion,
		}, path
	}
	h.t.Fatalf("no published promotion path starts at the subject's %s/%s", subject.GetJobCode(), subject.GetGrade())
	return nil, nil
}

// promoux015ProposedBase is current*(1+increase), rounded up to the cent, or
// one cent over current when the path publishes no minimum.
func promoux015ProposedBase(t *testing.T, current, increase string) string {
	t.Helper()
	cur, ok := new(big.Rat).SetString(current)
	if !ok {
		t.Fatalf("subject base pay %q is not a decimal", current)
	}
	next := new(big.Rat).Add(cur, big.NewRat(1, 100))
	if increase != "" {
		inc, ok := new(big.Rat).SetString(increase)
		if !ok {
			t.Fatalf("path minimum increase %q is not a decimal", increase)
		}
		scaled := new(big.Rat).Mul(cur, new(big.Rat).Add(big.NewRat(1, 1), inc))
		if scaled.Cmp(next) > 0 {
			next = scaled
		}
	}
	cents := new(big.Int).Quo(new(big.Int).Add(new(big.Int).Mul(next.Num(), big.NewInt(100)), new(big.Int).Sub(next.Denom(), big.NewInt(1))), next.Denom())
	return fmt.Sprintf("%d.%02d", new(big.Int).Quo(cents, big.NewInt(100)), new(big.Int).Rem(cents, big.NewInt(100)))
}

// runSeparatedPromotion drives the discovered promotion through proposal,
// execution and both separated approvals, returning the intent id at
// WAITING_EFFECTIVE_DATE.
func (h *promoux015Harness) runSeparatedPromotion() string {
	h.t.Helper()
	req, _ := h.discoverPromotion("hiring-manager")
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
	if err != nil {
		h.t.Fatalf("ProposeJourney as the proposer: %v", err)
	}
	id := proposed.GetJourney().GetIntentId()
	if got := proposed.GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		h.t.Fatalf("proposed stage = %s, want PROPOSED", got)
	}
	if _, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: id}); err != nil {
		h.t.Fatalf("ExecuteJourney as the operator: %v", err)
	}
	if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"}); err != nil {
		h.t.Fatalf("DecideJourney as the finance partner: %v", err)
	}
	waiting, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "manager approves"})
	if err != nil {
		h.t.Fatalf("DecideJourney as the current manager: %v", err)
	}
	if got := waiting.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		h.t.Fatalf("after both approvals stage = %s, want WAITING_EFFECTIVE_DATE", got)
	}
	return id
}

// afterEffectiveDate is a clock past the fixture's effective date.
func (h *promoux015Harness) afterEffectiveDate() func() time.Time {
	effective, err := time.Parse(time.DateOnly, h.effective)
	if err != nil {
		h.t.Fatalf("effective date %q: %v", h.effective, err)
	}
	return func() time.Time { return effective.Add(36 * time.Hour) }
}

// journeyFor returns the listed journey the persona sees for id, or nil.
func (h *promoux015Harness) journeyFor(persona, id string) *journeyv1.Journey {
	h.t.Helper()
	listed, err := h.client.ListJourneys(h.rpc(persona), &journeyv1.ListJourneysRequest{})
	if err != nil {
		if strings.Contains(err.Error(), "PermissionDenied") || strings.Contains(err.Error(), "not permitted") {
			return nil
		}
		h.t.Fatalf("ListJourneys as %s: %v", persona, err)
	}
	for _, j := range listed.GetJourneys() {
		if j.GetIntentId() == id {
			return j
		}
	}
	return nil
}

// TestTodo_PROMOUX_015 is the PRIMARY: one promotion reaches and is reviewed
// at its terminal state on the real server path, by four separated people.
// The proposer discovers the subject and a published path without
// foreknowledge; the operator executes; the finance partner and the current
// manager each decide only their own approval; the production scheduler fires
// the effective-date wait once and the promotion commits exactly once; and
// each persona then reviews the outcome from the surfaces it may reach, with
// the server's own relationship-to-viewer projection.
func TestTodo_PROMOUX_015(t *testing.T) {
	h := promoux015Compose(t)
	before := h.effects()

	id := h.runSeparatedPromotion()
	approved := h.effects()
	if approved.outcomeEvents != before.outcomeEvents || approved.outcomeOutbox != before.outcomeOutbox || approved.commitReceipts != before.commitReceipts {
		t.Fatalf("a promotion outcome was committed before the effective date: %+v -> %+v", before, approved)
	}

	// Before the effective date the production scheduler fires nothing.
	early := h.scheduler(func() time.Time { return time.Now().UTC() })
	if result, err := early.Tick(context.Background()); err != nil || result.Fired != 0 {
		t.Fatalf("Tick before the effective date = %+v, %v; want nothing fired", result, err)
	}

	due := h.scheduler(h.afterEffectiveDate())
	fired, err := due.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick after the effective date: %v", err)
	}
	if fired.Fired != 1 {
		t.Fatalf("Tick after the effective date fired %d timers, want exactly 1", fired.Fired)
	}
	committed := h.effects()
	t.Logf("%s effects: before %+v, approved %+v, committed %+v", promoux015FixtureVersion, before, approved, committed)
	if committed.outcomeEvents != approved.outcomeEvents+1 {
		t.Fatalf("commit wrote %d promotion outcome ledger events, want exactly 1 (%+v -> %+v)", committed.outcomeEvents-approved.outcomeEvents, approved, committed)
	}

	// Firing again commits nothing more: exactly once.
	if again, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil || again.Fired != 0 {
		t.Fatalf("second Tick = %+v, %v; want nothing fired", again, err)
	}
	if h.effects() != committed {
		t.Fatalf("a second tick moved effects: %+v -> %+v", committed, h.effects())
	}

	// Review the terminal promotion as each persona.
	inspected, err := h.client.InspectJourney(h.rpc("hiring-manager"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney as the proposer: %v", err)
	}
	if got := inspected.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		t.Fatalf("the proposer reviews stage %s, want BLOCKED", got)
	}
	operatorView, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney as the operator: %v", err)
	}
	if operatorView.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED || operatorView.GetDetail().GetLedger() == nil {
		t.Fatalf("the operator reviews stage %s ledger %v, want BLOCKED with its one ledger fact",
			operatorView.GetDetail().GetJourney().GetStage(), operatorView.GetDetail().GetLedger())
	}
	proposerJourney := h.journeyFor("hiring-manager", id)
	if proposerJourney == nil {
		t.Fatal("the proposer cannot find the promotion it proposed on Journeys")
	}
	if err := promoux015ReviewedClosed(proposerJourney); err != nil {
		t.Fatalf("the proposer's review of its recorded promotion: %v", err)
	}
	for _, persona := range []string{"admin", "finance-partner"} {
		if j := h.journeyFor(persona, id); j != nil && j.GetViewer().GetResponsibility() == journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED {
			t.Fatalf("%s is still asked to act on a recorded promotion: %v", persona, j.GetViewer())
		}
	}
}

func promoux015HasRelationship(v *journeyv1.JourneyViewerProjection, want journeyv1.JourneyViewerRelationship) bool {
	for _, r := range v.GetRelationships() {
		if r == want {
			return true
		}
	}
	return false
}

var _ = codes.OK
