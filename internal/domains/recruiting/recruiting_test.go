package recruiting

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/headcount"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func recruitingCapacity(available string) headcount.RequisitionCapacity {
	return headcount.RequisitionCapacity{Tenant: "tenant-a", RequestID: "hc-1", Revision: 3, State: headcount.HeadcountApproved,
		Available: values.MustDecimal(available, 2, values.RoundingExactRequired), BaselineVersion: "baseline-3", ReservationFence: 4}
}

type recruitingMemoryAllocator struct {
	mu           sync.Mutex
	capacity     headcount.RequisitionCapacity
	used         values.Decimal
	reservations map[string]headcount.RequisitionCapacityReservation
}

func newRecruitingAllocator(capacity headcount.RequisitionCapacity) *recruitingMemoryAllocator {
	return &recruitingMemoryAllocator{capacity: capacity,
		used:         values.MustDecimal("0", capacity.Available.Scale(), values.RoundingExactRequired),
		reservations: make(map[string]headcount.RequisitionCapacityReservation)}
}

func (r *recruitingMemoryAllocator) ReserveRequisitionCapacity(ref headcount.RequisitionCapacityReference, requisitionID string, amount values.Decimal) (headcount.RequisitionCapacityReservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.capacity.Validate(); err != nil || ref.Tenant != r.capacity.Tenant || ref.RequestID != r.capacity.RequestID || !validID(requisitionID) {
		return headcount.RequisitionCapacityReservation{}, headcount.ErrCapacityConflict
	}
	if prior, ok := r.reservations[requisitionID]; ok {
		if !prior.Amount.Equal(amount) {
			return headcount.RequisitionCapacityReservation{}, headcount.ErrCapacityConflict
		}
		return prior, nil
	}
	remaining, err := r.capacity.Available.Sub(r.used)
	if err != nil || amount.Validate() != nil || amount.Sign() <= 0 || amount.Scale() != remaining.Scale() || amount.Cmp(remaining) > 0 {
		return headcount.RequisitionCapacityReservation{}, headcount.ErrCapacityConflict
	}
	r.used, err = r.used.Add(amount)
	if err != nil {
		return headcount.RequisitionCapacityReservation{}, headcount.ErrCapacityConflict
	}
	reservation := headcount.RequisitionCapacityReservation{ID: fmt.Sprintf("reservation:%s:%s", r.capacity.RequestID, requisitionID),
		Tenant: r.capacity.Tenant, RequestID: r.capacity.RequestID, RequisitionID: requisitionID,
		Amount: amount, CapacityRevision: r.capacity.Revision, ReservationFence: r.capacity.ReservationFence + uint64(len(r.reservations))}
	r.reservations[requisitionID] = reservation
	return reservation, nil
}

func openReq(t *testing.T, a *Aggregate, id string, capacity headcount.RequisitionCapacity, requested string) error {
	t.Helper()
	return openReqWithAllocator(t, a, id, newRecruitingAllocator(capacity), capacity, requested)
}

func openReqWithAllocator(t *testing.T, a *Aggregate, id string, allocator headcount.RequisitionCapacityAllocator, capacity headcount.RequisitionCapacity, requested string) error {
	t.Helper()
	return a.OpenRequisition(id, "job:registered-nurse", headcount.RequisitionCapacityReference{Tenant: "tenant-a", RequestID: capacity.RequestID}, allocator,
		values.MustDecimal(requested, 2, values.RoundingExactRequired), recruitInstant(t, 1), recruitKnown(t, 1))
}

func recruitInstant(t *testing.T, day int) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(2026, time.January, day, 12, 0, 0, 0, time.UTC))
}

func recruitKnown(t *testing.T, day int) values.KnownAt {
	t.Helper()
	known, err := values.NewKnownAt(recruitInstant(t, day))
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func openRecruiting(t *testing.T) Aggregate {
	t.Helper()
	aggregate, err := NewAggregate("candidate+requisition")
	if err != nil {
		t.Fatal(err)
	}
	if err := openReq(t, &aggregate, "req-1", recruitingCapacity("1.00"), "1.00"); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreatePosting("post-1", "req-1", 1, "job:registered-nurse", recruitInstant(t, 2), recruitKnown(t, 2)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.PublishPosting("post-1", 1, recruitInstant(t, 3), recruitKnown(t, 3)); err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func submitRecruiting(t *testing.T, aggregate Aggregate, appID, candidateID string) Aggregate {
	t.Helper()
	if err := aggregate.SubmitApplication(appID, candidateID, "req-1", 1, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func TestTodo_REV_051_01(t *testing.T) {
	aggregate, err := NewAggregate("candidate+requisition")
	if err != nil {
		t.Fatal(err)
	}
	capacity := recruitingCapacity("1.00")
	allocator := newRecruitingAllocator(capacity)
	if err := openReqWithAllocator(t, &aggregate, "req-1", allocator, capacity, "0.75"); err != nil {
		t.Fatalf("approved capacity should permit a within-capacity requisition: %v", err)
	}
	opened := aggregate.Requisitions["req-1"]
	if opened.HeadcountRequestID != "hc-1" || opened.HeadcountRevision != 3 || opened.CapacityAllocated.String() != "0.75" || opened.Tenant != "tenant-a" {
		t.Fatalf("requisition binding = %+v", opened)
	}
	if opened.CanonicalDigest == "" || opened.CanonicalDigest != opened.withDigest().CanonicalDigest {
		t.Fatal("capacity binding is not represented in canonical digest")
	}
	beforeEvents, beforeOutbox := len(aggregate.Events), len(aggregate.Outbox)
	if err := openReqWithAllocator(t, &aggregate, "req-2", allocator, capacity, "0.50"); codeOf(err) != CodeCapacityUnavailable {
		t.Fatalf("opening beyond remaining headcount capacity err = %v", err)
	}
	if len(aggregate.Events) != beforeEvents || len(aggregate.Outbox) != beforeOutbox || len(aggregate.Requisitions) != 1 {
		t.Fatal("oversubscribed requisition changed ATS state")
	}
}

func TestTodo_REV_051_01_Golden(t *testing.T) {
	a, _ := NewAggregate("candidate+requisition")
	if err := openReq(t, &a, "req-1", recruitingCapacity("1.00"), "0.75"); err != nil {
		t.Fatal(err)
	}
	first := a.Requisitions["req-1"].CanonicalDigest
	if want := "sha256:5f8e25673356d1c32de4c71d1a2484ac3f6274b366f81ecf2e79687d8785c575"; first != want {
		t.Fatalf("capacity-bound requisition digest = %q, want %q", first, want)
	}
	if err := openReq(t, &a, "req-2", recruitingCapacity("1.00"), "0.25"); err != nil {
		t.Fatal(err)
	}
	second := a.Requisitions["req-2"].CanonicalDigest
	if first == "" || second == "" || first == second {
		t.Fatalf("capacity-bound requisition digests should be stable and identity-specific: %q %q", first, second)
	}
	changedAggregate, _ := NewAggregate("candidate+requisition")
	changed := recruitingCapacity("1.00")
	changed.Revision++
	if err := openReq(t, &changedAggregate, "req-3", changed, "0.25"); err != nil {
		t.Fatal(err)
	}
	if a.Requisitions["req-2"].CanonicalDigest == changedAggregate.Requisitions["req-3"].CanonicalDigest {
		t.Fatal("headcount revision is not pinned by the requisition digest")
	}
}

func TestTodo_REV_051_01_Race(t *testing.T) {
	capacity := recruitingCapacity("1.00")
	allocator := newRecruitingAllocator(capacity)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			aggregate, err := NewAggregate("candidate+requisition")
			if err == nil {
				err = openReqWithAllocator(t, &aggregate, fmt.Sprintf("req-race-%d", i), allocator, capacity, "0.25")
			}
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	accepted, refused := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if codeOf(err) == CodeCapacityUnavailable {
			refused++
		} else {
			t.Fatalf("unexpected concurrent open result: %v", err)
		}
	}
	if accepted != 4 || refused != 12 {
		t.Fatalf("concurrent allocation accepted=%d refused=%d, want 4 and 12", accepted, refused)
	}
}

func TestTodo_REV_051_01_Integration(t *testing.T) {
	r, err := headcount.ApproveHeadcount(headcount.HeadcountRequest{Tenant: "tenant-a", ID: "hc-1", Requester: "manager-1",
		OrganizationRef: "org-1", JobRef: "job-1", LocationRef: "loc-1", CostCenterRef: "cc-1", PositionCount: 1,
		Capacity: values.MustDecimal("1.00", 2, values.RoundingExactRequired), Unit: headcount.UnitFTE,
		State: headcount.HeadcountSubmitted, ProposalRevision: 3}, headcount.ApprovalCertificate{PolicyVersion: "policy-1", DecisionDigest: "decision-1",
		Requester: "manager-1", ApproverRefs: []string{"hr-1"}, RequiredQuorum: 1, ResolvedByPolicy: true, SeparationOfDuties: true})
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := headcount.ApprovedRequisitionCapacity(r, recruitingSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := NewAggregate("candidate+requisition")
	if err := openReq(t, &a, "req-1", capacity, "1.00"); err != nil {
		t.Fatalf("approved headcount capacity did not authorize ATS opening: %v", err)
	}
	if a.Requisitions["req-1"].HeadcountRequestID != r.ID {
		t.Fatal("ATS requisition is not bound to approved headcount request")
	}
}

func recruitingSnapshot() headcount.CapacitySnapshot {
	return headcount.CapacitySnapshot{Capacity: values.MustDecimal("1.00", 2, values.RoundingExactRequired),
		Consumed: values.MustDecimal("0.00", 2, values.RoundingExactRequired), Reserved: values.MustDecimal("0.00", 2, values.RoundingExactRequired),
		BudgetAvailable: values.MustDecimal("1.00", 2, values.RoundingExactRequired), BaselineVersion: "baseline-1", ReservationFence: 1}
}

func TestTodo_REV_051_01_Fault(t *testing.T) {
	bad := []struct {
		name     string
		capacity headcount.RequisitionCapacity
		tenant   string
	}{
		{"unknown", headcount.RequisitionCapacity{}, "tenant-a"},
		{"foreign tenant", recruitingCapacity("1.00"), "tenant-b"},
		{"closed", func() headcount.RequisitionCapacity {
			c := recruitingCapacity("1.00")
			c.State = headcount.HeadcountRejected
			return c
		}(), "tenant-a"},
		{"exhausted", recruitingCapacity("0.00"), "tenant-a"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := NewAggregate("candidate+requisition")
			err := a.OpenRequisition("req-1", "job:rn", headcount.RequisitionCapacityReference{Tenant: tc.tenant, RequestID: tc.capacity.RequestID}, newRecruitingAllocator(tc.capacity), values.MustDecimal("0.25", 2, values.RoundingExactRequired), recruitInstant(t, 1), recruitKnown(t, 1))
			if codeOf(err) != CodeCapacityUnavailable || len(a.Events) != 0 || len(a.Outbox) != 0 || len(a.Requisitions) != 0 {
				t.Fatalf("invalid capacity was not refused atomically: err=%v aggregate=%+v", err, a)
			}
		})
	}
}

// TestRecruitingAggregateLifecyclesRejectMissingIdentityAndIllegalTransitions:
// every seeded defect — an application without exact parent revisions, a
// duplicate application, a posting after requisition closure, a candidacy
// without consent/purpose, and any transition from a terminal candidacy —
// is rejected with its typed code and appends zero events/outbox entries.
func TestRecruitingAggregateLifecyclesRejectMissingIdentityAndIllegalTransitions(t *testing.T) {
	aggregate := openRecruiting(t)

	// Application without candidate/requisition/posting revisions.
	before, beforeOutbox := len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.SubmitApplication("app-x", "", "req-1", 1, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("missing candidate err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-x", "candidate-1", "req-x", 1, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("missing requisition err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-x", "candidate-1", "req-1", 1, "post-x", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("missing posting err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-x", "candidate-1", "req-1", 9, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("stale requisition revision err = %v", err)
	}
	assertNoAppend(t, aggregate, before, beforeOutbox, "missing parents")

	// Duplicate application under the declared uniqueness policy.
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	before, beforeOutbox = len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.SubmitApplication("app-2", "candidate-1", "req-1", 1, "post-1", 2, "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); codeOf(err) != CodeDuplicateApplication {
		t.Fatalf("duplicate application err = %v", err)
	}
	assertNoAppend(t, aggregate, before, beforeOutbox, "duplicate application")
	aggregate = submitRecruiting(t, aggregate, "app-2", "candidate-2")
	aggregate = submitRecruiting(t, aggregate, "app-3", "candidate-3")

	// Posting after requisition closure.
	if err := aggregate.CloseRequisition("req-1", 1, recruitInstant(t, 6), recruitKnown(t, 6)); err != nil {
		t.Fatal(err)
	}
	before, beforeOutbox = len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.CreatePosting("post-2", "req-1", 2, "job:registered-nurse", recruitInstant(t, 7), recruitKnown(t, 7)); codeOf(err) != CodeRequisitionClosed {
		t.Fatalf("post-closure posting err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-9", "candidate-9", "req-1", 2, "post-1", 2, "source:ats", recruitInstant(t, 7), recruitKnown(t, 7)); codeOf(err) != CodeRequisitionClosed {
		t.Fatalf("post-closure application err = %v", err)
	}
	assertNoAppend(t, aggregate, before, beforeOutbox, "post-closure commands")

	// Candidacy without consent/purpose.
	before, beforeOutbox = len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); codeOf(err) != CodeInvalidCandidacyTransition {
		t.Fatalf("missing consent err = %v", err)
	}
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); codeOf(err) != CodeInvalidCandidacyTransition {
		t.Fatalf("missing purpose err = %v", err)
	}

	// Transitions from terminal WITHDRAWN|REJECTED|HIRED are rejected.
	assertNoAppend(t, aggregate, before, beforeOutbox, "missing consent/purpose")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreateCandidacy("cand-w", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.TransitionCandidacy("cand-w", 1, StageWithdrawn, recruitInstant(t, 9), recruitKnown(t, 9)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreateCandidacy("cand-r", "app-2", "candidate-2", "consent:2", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.TransitionCandidacy("cand-r", 1, StageRejected, recruitInstant(t, 9), recruitKnown(t, 9)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreateCandidacy("cand-h", "app-3", "candidate-3", "consent:3", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		stage CandidacyStage
		rev   uint64
	}{{StageScreening, 1}, {StageInterview, 2}, {StageOffer, 3}} {
		if err := aggregate.TransitionCandidacy("cand-h", step.rev, step.stage, recruitInstant(t, 9), recruitKnown(t, 9)); err != nil {
			t.Fatal(err)
		}
	}
	if err := aggregate.TransitionCandidacy("cand-h", 4, StageHired, recruitInstant(t, 10), recruitKnown(t, 10)); err != nil {
		t.Fatal(err)
	}
	before = len(aggregate.Events)
	beforeOutbox = len(aggregate.Outbox)
	for id, rev := range map[string]uint64{"cand-w": 2, "cand-r": 2, "cand-h": 5} {
		if err := aggregate.TransitionCandidacy(id, rev, StageScreening, recruitInstant(t, 11), recruitKnown(t, 11)); codeOf(err) != CodeInvalidCandidacyTransition {
			t.Fatalf("terminal transition %s err = %v", id, err)
		}
	}
	if len(aggregate.Events) != before || len(aggregate.Outbox) != beforeOutbox {
		t.Fatal("rejected terminal transitions appended events/outbox entries")
	}
}
