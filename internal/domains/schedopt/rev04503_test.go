package schedopt

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rev04503Fixture(t *testing.T) (*ShiftSelfService, []WorkerStanding, values.EntityRef, values.EntityRef, time.Time) {
	t.Helper()
	p := hardConstraintProblem(t)
	p.DemandWindows = []DemandWindow{schedoptDemand(t, "window-1", 9, 10), schedoptDemand(t, "window-2", 11, 12)}
	p.HardConstraints = []Constraint{{Kind: ConstraintAvailability}, {Kind: ConstraintLocation, Location: "new-york"}, {Kind: ConstraintLegalAuthorization, AuthorizationRefs: []values.EntityRef{hardConstraintAuthRef()}}, {Kind: ConstraintFatigueLimit, MaxFatigueMinutes: 480}}
	problem, err := NewProblem(p)
	if err != nil {
		t.Fatal(err)
	}
	workerA, workerB := schedoptRef("candidate", "005"), schedoptRef("candidate", "006")
	base := CandidateSchedule{Tenant: "tenant-a", Revision: "self-service-fixture", RuleDigest: "rules-v1", ReviewLifecycle: ReviewPrepublication,
		Assignments: []ReviewAssignment{{AssignmentID: "a-1", WorkerRef: workerB.String(), DemandRef: "demand-1", WindowRef: "window-1"}, {AssignmentID: "a-2", WorkerRef: workerA.String(), DemandRef: "demand-2", WindowRef: "window-2"}},
		Rules:       []ReviewRule{{Kind: "MAX_PER_WINDOW", WindowRef: "window-1", Max: 1}, {Kind: "MAX_PER_WINDOW", WindowRef: "window-2", Max: 1}}}
	approved, err := ApplyPrepublicationReview(base, nil, "scheduler:test", time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	posted := time.Date(2026, 9, 10, 8, 1, 0, 0, time.UTC)
	publication, err := PublishSchedule(approved, "fixture-publication", map[string]string{}, posted)
	if err != nil {
		t.Fatal(err)
	}
	hours, err := values.NewQuantity("1.5", "HOUR", 1, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	money, err := values.NewMoney("20.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	rate, err := values.NewMoneyRate(money, "HOUR")
	if err != nil {
		t.Fatal(err)
	}
	policy := PublishedReviewPolicy{Jurisdiction: "TEST:JURISDICTION",
		Rules:        map[string]FairWorkweekRule{"TEST:JURISDICTION": {Jurisdiction: "TEST:JURISDICTION", RuleRef: "fixture:rule", RuleVersion: "1", CitationRef: "fixture:citation", MinimumNotice: 0, PremiumHours: hours}},
		RegularRates: map[string]values.Rate{workerA.String(): rate, workerB.String(): rate}}
	service, err := NewShiftSelfService(ShiftSelfServiceConfig{Approved: approved, Publication: publication, Rules: base.Rules, Problem: problem, Review: policy})
	if err != nil {
		t.Fatal(err)
	}
	standing := []WorkerStanding{{CandidateRef: workerA, LegalAuthorizations: []values.EntityRef{hardConstraintAuthRef()}, AccruedFatigueMinutes: 60}, {CandidateRef: workerB, LegalAuthorizations: []values.EntityRef{hardConstraintAuthRef()}, AccruedFatigueMinutes: 60}}
	return service, standing, workerA, workerB, posted.Add(24 * time.Hour)
}

func assertPublishedAndReconciled(t *testing.T, service *ShiftSelfService, publication Publication) {
	t.Helper()
	if publication.Digest == "" || publication.Revision == "" {
		t.Fatalf("new revision was not published: %+v", publication)
	}
	observed := make([]ObservedCoverage, 0, len(publication.Assignments))
	for _, assignment := range publication.Assignments {
		observed = append(observed, ObservedCoverage{WorkerRef: assignment.WorkerRef, WindowRef: assignment.WindowRef, DemandRef: assignment.DemandRef, Channel: "roster", State: "COVERED"})
	}
	result, err := service.Reconcile(observed)
	if err != nil || result.Matched != len(publication.Assignments) || len(result.Failures) != 0 {
		t.Fatalf("SCHED-OPT-007 reconciliation failed: result=%+v err=%v", result, err)
	}
}

func TestTodo_REV_045_03(t *testing.T) {
	t.Run("open claim", func(t *testing.T) {
		service, standings, workerA, workerB, changedAt := rev04503Fixture(t)
		offer, err := service.OfferOpenShift("offer-claim", "a-1", workerB, service.FencingToken(), "pickup available shift")
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.ClaimOpenShift(offer.ID, workerA, offer.FencingToken, standings, changedAt)
		if err != nil {
			t.Fatal(err)
		}
		assignment, _ := findReviewAssignment(result.Publication.Assignments, "a-1")
		if assignment.WorkerRef != workerA.String() {
			t.Fatalf("claim owner = %s, want %s", assignment.WorkerRef, workerA.String())
		}
		assertPublishedAndReconciled(t, service, result.Publication)
	})
	t.Run("trade offer and acceptance", func(t *testing.T) {
		service, standings, workerA, workerB, changedAt := rev04503Fixture(t)
		offer, err := service.OfferTrade("offer-trade", "a-2", "a-1", workerA, service.FencingToken(), "trade shifts")
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.AcceptTrade(offer.ID, workerB, offer.FencingToken, standings, changedAt)
		if err != nil {
			t.Fatal(err)
		}
		first, _ := findReviewAssignment(result.Publication.Assignments, "a-1")
		second, _ := findReviewAssignment(result.Publication.Assignments, "a-2")
		if first.WorkerRef != workerA.String() || second.WorkerRef != workerB.String() {
			t.Fatalf("trade not applied: a-1=%s a-2=%s", first.WorkerRef, second.WorkerRef)
		}
		assertPublishedAndReconciled(t, service, result.Publication)
	})
}

func TestTodo_REV_045_03_Property(t *testing.T) {
	service, standings, workerA, workerB, changedAt := rev04503Fixture(t)
	offer, err := service.OfferOpenShift("offer-property", "a-1", workerB, service.FencingToken(), "pickup available shift")
	if err != nil {
		t.Fatal(err)
	}
	blocked := append([]WorkerStanding(nil), standings...)
	blocked[0].LegalAuthorizations = nil
	if _, err := service.ClaimOpenShift(offer.ID, workerA, offer.FencingToken, blocked, changedAt); err == nil {
		t.Fatal("claim without required legal authorization must reject")
	} else {
		var rejection *ShiftSelfServiceRejection
		if !errors.As(err, &rejection) || rejection.State != "BLOCKED" || !errors.Is(err, ErrShiftSelfServiceRejected) {
			t.Fatalf("expected typed hard-constraint rejection, got %v", err)
		}
	}
	result, err := service.ClaimOpenShift(offer.ID, workerA, offer.FencingToken, standings, changedAt)
	if err != nil {
		t.Fatalf("failed rejected claim must leave offer claimable: %v", err)
	}
	if result.Publication.Digest == "" || workerB.Validate() != nil {
		t.Fatal("successful retry did not publish a valid revision")
	}
}

func TestTodo_REV_045_03_Race(t *testing.T) {
	service, standings, workerA, workerB, changedAt := rev04503Fixture(t)
	offer, err := service.OfferOpenShift("offer-race", "a-1", workerB, service.FencingToken(), "pickup available shift")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	type outcome struct {
		result ShiftSelfServiceResult
		err    error
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := service.ClaimOpenShift(offer.ID, workerA, offer.FencingToken, standings, changedAt)
			results <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins, losses := 0, 0
	var published Publication
	for got := range results {
		if got.err == nil {
			wins++
			published = got.result.Publication
			continue
		}
		var rejection *ShiftSelfServiceRejection
		if !errors.As(got.err, &rejection) || rejection.State != "STALE" || !errors.Is(got.err, ErrShiftSelfServiceRejected) {
			t.Fatalf("race loser needs typed stale rejection, got %v", got.err)
		}
		losses++
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("want one winner and one typed loser; wins=%d losses=%d", wins, losses)
	}
	assertPublishedAndReconciled(t, service, published)
}

type rev04503DurableHead struct {
	digest string
	fence  uint64
}

type rev04503DurableOffers struct {
	mu     sync.Mutex
	heads  map[string]rev04503DurableHead
	offers map[string]ShiftOffer
}

func (r *rev04503DurableOffers) LoadShiftScheduleFence(_ context.Context, tenant values.TenantId, scheduleID, digest string) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := tenant.String() + "\x00" + scheduleID
	head, ok := r.heads[key]
	if !ok {
		return 1, nil
	}
	if head.digest != digest {
		return 0, errors.New("stale digest")
	}
	return head.fence, nil
}

func (r *rev04503DurableOffers) CreateShiftOffer(_ context.Context, tenant values.TenantId, scheduleID string, expected uint64, offer ShiftOffer) (ShiftOffer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := tenant.String() + "\x00" + scheduleID
	head, ok := r.heads[key]
	if !ok {
		head = rev04503DurableHead{digest: offer.PublicationDigest, fence: 1}
	}
	if head.fence != expected || head.digest != offer.PublicationDigest {
		return ShiftOffer{}, errors.New("stale fence")
	}
	head.fence++
	if r.heads == nil {
		r.heads = make(map[string]rev04503DurableHead)
		r.offers = make(map[string]ShiftOffer)
	}
	r.heads[key] = head
	offer.FencingToken = head.fence
	r.offers[key+"\x00"+offer.ID] = offer
	return offer, nil
}

func (r *rev04503DurableOffers) LoadShiftOffer(_ context.Context, tenant values.TenantId, scheduleID, offerID string) (ShiftOffer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	offer, ok := r.offers[tenant.String()+"\x00"+scheduleID+"\x00"+offerID]
	if !ok {
		return ShiftOffer{}, errors.New("missing offer")
	}
	return offer, nil
}

func (r *rev04503DurableOffers) ClaimShiftOffer(_ context.Context, tenant values.TenantId, scheduleID, offerID string, expected uint64, digest string) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := tenant.String() + "\x00" + scheduleID
	head := r.heads[key]
	offerKey := key + "\x00" + offerID
	offer, ok := r.offers[offerKey]
	if !ok || offer.State != ShiftOfferActive || offer.FencingToken != expected || offer.PublicationDigest != head.digest {
		return 0, errors.New("stale claim")
	}
	if head.fence == 0 || head.digest != offer.PublicationDigest {
		return 0, errors.New("stale schedule")
	}
	head.fence++
	head.digest = digest
	r.heads[key] = head
	offer.State = ShiftOfferClaimed
	r.offers[offerKey] = offer
	return head.fence, nil
}

func TestTodo_REV_045_03_Race_DurableServiceInstances(t *testing.T) {
	initial, standings, workerA, workerB, changedAt := rev04503Fixture(t)
	repository := &rev04503DurableOffers{}
	config := func() ShiftSelfServiceConfig {
		return ShiftSelfServiceConfig{
			Approved: cloneApprovedSchedule(initial.approved), Publication: clonePublication(initial.publication),
			Rules: append([]ReviewRule(nil), initial.rules...), Problem: cloneOptimizationProblem(initial.problem),
			Review: clonePublishedReviewPolicy(initial.review), Repository: repository, ScheduleID: "schedule-durable-race",
		}
	}
	missingID := config()
	missingID.ScheduleID = ""
	if _, err := NewShiftSelfService(missingID); err == nil {
		t.Fatal("durable self-service must reject an unstable schedule identifier")
	}
	first, err := NewShiftSelfService(config())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewShiftSelfService(config())
	if err != nil {
		t.Fatal(err)
	}
	offer, err := first.OfferOpenShift("offer-durable-race", "a-1", workerB, first.FencingToken(), "pickup available shift")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	type outcome struct {
		service *ShiftSelfService
		result  ShiftSelfServiceResult
		err     error
	}
	results := make(chan outcome, 2)
	for _, service := range []*ShiftSelfService{first, second} {
		wg.Add(1)
		go func(service *ShiftSelfService) {
			defer wg.Done()
			<-start
			result, err := service.ClaimOpenShift(offer.ID, workerA, offer.FencingToken, standings, changedAt)
			results <- outcome{service: service, result: result, err: err}
		}(service)
	}
	close(start)
	wg.Wait()
	close(results)
	wins, losses := 0, 0
	for got := range results {
		if got.err == nil {
			wins++
			if got.result.FencingToken != 3 {
				t.Errorf("durable result fence = %d, want 3", got.result.FencingToken)
			}
			assertPublishedAndReconciled(t, got.service, got.result.Publication)
			continue
		}
		var rejection *ShiftSelfServiceRejection
		if !errors.As(got.err, &rejection) || rejection.State != "STALE" || !errors.Is(got.err, ErrShiftSelfServiceRejected) {
			t.Errorf("durable race loser needs typed stale refusal, got %v", got.err)
			continue
		}
		losses++
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("separate service instances must produce one durable winner and one typed loser; wins=%d losses=%d", wins, losses)
	}
}
