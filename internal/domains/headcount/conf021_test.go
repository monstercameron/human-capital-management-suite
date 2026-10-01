package headcount

// CONF-021 proves position and headcount requisition with capacity
// conservation: approval never conflates into creation, exact decimals
// conserve FTE/count/budget under competing proposals, approvals bind
// immutable revisions, revalidation fences scarce resources, and external
// creation reconciles identifiers and capacity before closure.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/resource/reservation"
)

func conf021Request() HeadcountRequest {
	return HeadcountRequest{Tenant: "tenant-a", ID: "hc-21", Requester: "manager-21", PlanRef: "plan-21", BudgetRef: "budget-21", OrganizationRef: "org-21", JobRef: "job-21", LocationRef: "loc-21", CostCenterRef: "cc-21", PositionCount: 2, Capacity: values.MustDecimal("2.00", 2, values.RoundingExactRequired), Unit: UnitFTE, State: HeadcountSubmitted, ProposalRevision: 1}
}

func conf021Approval() ApprovalCertificate {
	return ApprovalCertificate{PolicyVersion: "policy-v21", DecisionDigest: "decision-v21", Requester: "manager-21", ApproverRefs: []string{"hr-21", "finance-21"}, RequiredQuorum: 2, ResolvedByPolicy: true, SeparationOfDuties: true}
}

func conf021Snapshot() CapacitySnapshot {
	return CapacitySnapshot{Capacity: values.MustDecimal("2.00", 2, values.RoundingExactRequired), Consumed: values.MustDecimal("0.00", 2, values.RoundingExactRequired), Reserved: values.MustDecimal("0.00", 2, values.RoundingExactRequired), BudgetAvailable: values.MustDecimal("2.00", 2, values.RoundingExactRequired), BaselineVersion: "baseline-v21", ReservationFence: 1}
}

func conf021Position() PositionProposal {
	return PositionProposal{Tenant: "tenant-a", ID: "pos-21", ParentRequestID: "hc-21", Requester: "manager-21", State: PositionProposed, BudgetReservationRef: "budget-hold-21", CapacityReservationRef: "capacity-hold-21"}
}

func conf021Requisition() RequisitionProposal {
	return RequisitionProposal{Tenant: "tenant-a", ID: "req-21", ParentRequestID: "hc-21", PositionID: "pos-21", Requester: "manager-21", State: RequisitionProposed}
}

func conf021Applied(id string) ExternalObservation {
	return ExternalObservation{State: ExternalApplied, ExternalID: id, PayloadDigest: "payload-21"}
}

// TestHeadcountRequisitionConformancePreventsApprovalCreationConflationAndOvercommit
// is the CONF-021 primary test.
//
// RED: approving capacity creates a position or requisition implicitly, a
// requester-chosen approver passes, quorum/SoD evidence is missing, external
// REQUESTED counts as created capacity, or a competing proposal overcommits.
// GREEN: approval authorizes capacity only; position and requisition open
// through their own fenced transactions; overcommit is refused.
func TestHeadcountRequisitionConformancePreventsApprovalCreationConflationAndOvercommit(t *testing.T) {
	approved, err := ApproveHeadcount(conf021Request(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != HeadcountApproved {
		t.Fatalf("headcount state = %s", approved.State)
	}

	// Approval authorizes capacity only: opening a requisition straight off
	// the approval, with no position lifecycle, is refused.
	if _, err := RequestRequisitionOpen(approved, PositionProposal{}, conf021Requisition(), conf021Approval()); err == nil {
		t.Fatal("requisition opened directly from approval: approval/creation conflated")
	}

	// Requester-chosen approvers never pass policy.
	chosen := conf021Approval()
	chosen.ApproverRefs = []string{"manager-21", "hr-21"}
	if _, err := ApproveHeadcount(conf021Request(), chosen); !errors.Is(err, ErrInvalidApproval) {
		t.Fatalf("requester-chosen approver = %v, want ErrInvalidApproval", err)
	}
	// Missing quorum and separation evidence never pass.
	for name, mutate := range map[string]func(*ApprovalCertificate){
		"quorum unmet":         func(a *ApprovalCertificate) { a.RequiredQuorum = 3 },
		"unresolved by policy": func(a *ApprovalCertificate) { a.ResolvedByPolicy = false },
		"no separation":        func(a *ApprovalCertificate) { a.SeparationOfDuties = false },
	} {
		cert := conf021Approval()
		mutate(&cert)
		if _, err := ApproveHeadcount(conf021Request(), cert); !errors.Is(err, ErrInvalidApproval) {
			t.Fatalf("%s = %v, want ErrInvalidApproval", name, err)
		}
	}

	position, err := RequestPositionCreation(approved, conf021Position(), conf021Approval(), conf021Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	// External REQUESTED is evidence of a sent request, never of created
	// capacity: it must not complete the position.
	position, err = ObservePositionCreation(position, ExternalObservation{State: ExternalRequested, PayloadDigest: "payload-21"})
	if !errors.Is(err, ErrExternalNotApplied) || position.State != PositionRequested {
		t.Fatalf("requested observation = %#v, %v; external requested must not count as created", position, err)
	}
	position, err = ObservePositionCreation(position, conf021Applied("external-pos-21"))
	if err != nil || position.State != PositionCreated {
		t.Fatalf("applied observation = %#v, %v", position, err)
	}

	requisition, err := RequestRequisitionOpen(approved, position, conf021Requisition(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	requisition, err = ObserveRequisition(requisition, ExternalObservation{State: ExternalRequested, PayloadDigest: "payload-21"})
	if !errors.Is(err, ErrExternalNotApplied) || requisition.State != RequisitionRequested {
		t.Fatalf("requested requisition observation = %#v, %v", requisition, err)
	}
	requisition, err = ObserveRequisition(requisition, conf021Applied("external-req-21"))
	if err != nil || requisition.State != RequisitionOpen {
		t.Fatalf("applied requisition observation = %#v, %v", requisition, err)
	}

	// A competing proposal for the same 2 FTE against fully consumed
	// capacity overcommits and is refused.
	consumed := conf021Snapshot()
	consumed.Consumed = values.MustDecimal("2.00", 2, values.RoundingExactRequired)
	competing := conf021Request()
	competing.ID = "hc-22"
	competing, err = ApproveHeadcount(competing, conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	competingPosition := conf021Position()
	competingPosition.ID = "pos-22"
	competingPosition.ParentRequestID = "hc-22"
	if _, err := RequestPositionCreation(competing, competingPosition, conf021Approval(), consumed); !errors.Is(err, ErrCapacityConflict) {
		t.Fatalf("competing overcommit = %v, want ErrCapacityConflict", err)
	}
}

func TestTodo_CONF_021_Property(t *testing.T) {
	exact := func(text string) values.Decimal {
		return values.MustDecimal(text, 2, values.RoundingExactRequired)
	}
	// Exact decimal conservation: available always equals capacity minus
	// consumed minus reserved; float arithmetic cannot state this.
	for i := int64(0); i <= 200; i += 25 {
		consumed := exact(fmt.Sprintf("%d.%02d", i/100, i%100))
		snap := conf021Snapshot()
		snap.Consumed = consumed
		available, err := snap.Available()
		if err != nil {
			t.Fatal(err)
		}
		recombined, err := available.Add(consumed)
		if err != nil {
			t.Fatal(err)
		}
		if !recombined.Equal(snap.Capacity) {
			t.Fatalf("consumed %s: available + consumed != capacity", consumed)
		}
	}
	// 0.10 + 0.20 is exactly 0.30 in decimals; float64 cannot say that.
	// (The float operands are variables so Go evaluates the sum at
	// runtime instead of constant-folding it to exactly 0.3.)
	sum, err := exact("0.10").Add(exact("0.20"))
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Equal(exact("0.30")) {
		t.Fatalf("decimal 0.10 + 0.20 = %s, want 0.30", sum)
	}
	a, b, c := 0.1, 0.2, 0.3
	if a+b == c {
		t.Fatal("float64 0.1 + 0.2 equals 0.3: float FTE would silently corrupt capacity")
	}
	// Wrong-scale quantities are refused: thousandths never fence hundredths.
	if err := CheckCapacity(conf021Snapshot(), values.MustDecimal("1.000", 3, values.RoundingExactRequired)); !errors.Is(err, ErrCapacityConflict) {
		t.Fatalf("wrong-scale capacity = %v, want ErrCapacityConflict", err)
	}
}

func conf021Transcript(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	r := conf021Request()
	fmt.Fprintf(&b, "request: %s\n", r.Explain())
	approved, err := ApproveHeadcount(r, conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "headcount: %s -> %s\n", HeadcountSubmitted, approved.State)
	p, err := RequestPositionCreation(approved, conf021Position(), conf021Approval(), conf021Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "position: %s -> %s\n", PositionProposed, p.State)
	p, err = ObservePositionCreation(p, conf021Applied("external-pos-21"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "position: %s -> %s external=%s\n", PositionRequested, p.State, p.External.ExternalID)
	q, err := RequestRequisitionOpen(approved, p, conf021Requisition(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "requisition: %s -> %s\n", RequisitionProposed, q.State)
	q, err = ObserveRequisition(q, conf021Applied("external-req-21"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "requisition: %s -> %s external=%s\n", RequisitionRequested, q.State, q.External.ExternalID)
	fmt.Fprintf(&b, "capacity: requested=%s available=%s\n", approved.Capacity, values.MustDecimal("2.00", 2, values.RoundingExactRequired))
	return b.String()
}

func TestTodo_CONF_021_Golden(t *testing.T) {
	got := conf021Transcript(t)
	path := filepath.Join("testdata", "conf021_lifecycle.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestTodo_CONF_021_Race(t *testing.T) {
	const workers = 16
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := conf021Request()
			r.ID = fmt.Sprintf("hc-race-%d", i)
			approved, err := ApproveHeadcount(r, conf021Approval())
			if err != nil {
				results <- err
				return
			}
			p := conf021Position()
			p.ID = fmt.Sprintf("pos-race-%d", i)
			p.ParentRequestID = r.ID
			// Competing proposals share one snapshot view: each 2 FTE
			// request against 2 FTE available passes its own check, so
			// the fence (not the check) must serialize commits. The
			// pure check itself is deterministic under concurrency.
			if err := CheckCapacity(conf021Snapshot(), approved.Capacity); err != nil {
				results <- err
				return
			}
			if _, err := RequestPositionCreation(approved, p, conf021Approval(), conf021Snapshot()); err != nil {
				results <- err
				return
			}
			results <- nil
		}(i)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent lifecycle = %v", err)
		}
	}
}

func conf021ReservationRequest(resource, owner, proposal, authority string, qty reservation.Quantity, now time.Time) reservation.Request {
	return reservation.Request{
		Resource: resource, Version: 1, Quantity: qty,
		Interval: reservation.Interval{From: now, To: now.Add(30 * 24 * time.Hour)},
		Owner:    owner, Priority: 1, ExpiresAt: now.Add(time.Hour),
		ProposalDigest: reservation.Digest([]byte(proposal)), AuthorityDigest: reservation.Digest([]byte(authority)),
		IdempotencyKey: resource + "-key",
	}
}

func TestTodo_CONF_021_Integration(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	store := reservation.NewStore()
	// FTE as integer thousandths: exact, never float.
	fte := reservation.Quantity{Value: 2000, Scale: 3}
	budgetHold, err := store.Acquire(conf021ReservationRequest("budget-21", "manager-21", "proposal-budget", "authority-21", fte, now), fte, now)
	if err != nil {
		t.Fatal(err)
	}
	capacityHold, err := store.Acquire(conf021ReservationRequest("capacity-21", "manager-21", "proposal-capacity", "authority-21", fte, now), fte, now)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := ApproveHeadcount(conf021Request(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	position := conf021Position()
	position.BudgetReservationRef = budgetHold.ID.String()
	position.CapacityReservationRef = capacityHold.ID.String()
	position, err = RequestPositionCreation(approved, position, conf021Approval(), conf021Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if position.State != PositionRequested {
		t.Fatalf("position state = %s", position.State)
	}
	// Replaying the capacity-hold consumption is idempotent, but scarce
	// resources fence exactly once: one Held-to-Consumed event exists, and
	// a consumed hold never transitions elsewhere.
	if _, err := store.Consume(capacityHold.ID, capacityHold.Fence, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(capacityHold.ID, capacityHold.Fence, now); err != nil {
		t.Fatalf("consume replay = %v, want idempotent nil", err)
	}
	consumed := 0
	for _, event := range store.Events(capacityHold.ID) {
		if event.From == reservation.Held && event.To == reservation.Consumed {
			consumed++
		}
	}
	if consumed != 1 {
		t.Fatalf("capacity hold consumed %d times, want exactly 1", consumed)
	}
	if _, err := store.Release(capacityHold.ID, capacityHold.Fence, now); !errors.Is(err, reservation.ErrInvalidTransition) {
		t.Fatalf("release after consume = %v, want ErrInvalidTransition", err)
	}
	// A stale fencing token never consumes.
	if _, err := store.Consume(budgetHold.ID, budgetHold.Fence+1, now); !errors.Is(err, reservation.ErrFence) {
		t.Fatalf("stale fence consume = %v, want ErrFence", err)
	}
}

func TestTodo_CONF_021_Fault(t *testing.T) {
	approved, err := ApproveHeadcount(conf021Request(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	// Approving an approved request is not a transition.
	if _, err := ApproveHeadcount(approved, conf021Approval()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("double approve = %v, want ErrInvalidTransition", err)
	}
	// Position creation requires the approved state.
	draft := conf021Request()
	draft.State = HeadcountDraft
	if _, err := RequestPositionCreation(draft, conf021Position(), conf021Approval(), conf021Snapshot()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("position on draft = %v, want ErrInvalidTransition", err)
	}
	// Requisition requires a created position with complete external evidence.
	requested, err := RequestPositionCreation(approved, conf021Position(), conf021Approval(), conf021Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RequestRequisitionOpen(approved, requested, conf021Requisition(), conf021Approval()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("requisition on requested position = %v, want ErrInvalidTransition", err)
	}
	// Snapshots without baseline and fence prove nothing.
	bare := conf021Snapshot()
	bare.BaselineVersion = ""
	bare.ReservationFence = 0
	if _, err := RequestPositionCreation(approved, conf021Position(), conf021Approval(), bare); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("fenceless snapshot = %v, want ErrInvalidRequest", err)
	}
	// Zero capacity and empty identity are refused.
	zero := conf021Request()
	zero.Capacity = values.MustDecimal("0.00", 2, values.RoundingExactRequired)
	if _, err := ApproveHeadcount(zero, conf021Approval()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero capacity = %v, want ErrInvalidRequest", err)
	}
	anonymous := conf021Request()
	anonymous.Requester = ""
	if _, err := ApproveHeadcount(anonymous, conf021Approval()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("anonymous request = %v, want ErrInvalidRequest", err)
	}
}

func TestTodo_CONF_021_Security(t *testing.T) {
	// The requester never approves their own capacity.
	self := conf021Approval()
	self.ApproverRefs = []string{"manager-21", "hr-21"}
	if _, err := ApproveHeadcount(conf021Request(), self); !errors.Is(err, ErrInvalidApproval) {
		t.Fatalf("self approval = %v, want ErrInvalidApproval", err)
	}
	// Duplicate approvers never satisfy quorum.
	dup := conf021Approval()
	dup.ApproverRefs = []string{"hr-21", "hr-21"}
	if _, err := ApproveHeadcount(conf021Request(), dup); !errors.Is(err, ErrInvalidApproval) {
		t.Fatalf("duplicate approvers = %v, want ErrInvalidApproval", err)
	}
	// Tenants never cross: a position filed under another tenant is foreign.
	approved, err := ApproveHeadcount(conf021Request(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	foreign := conf021Position()
	foreign.Tenant = "tenant-b"
	if _, err := RequestPositionCreation(approved, foreign, conf021Approval(), conf021Snapshot()); !errors.Is(err, ErrInvalidRelationship) {
		t.Fatalf("foreign tenant position = %v, want ErrInvalidRelationship", err)
	}
	// Explain is reference-only: no plan, budget, cost-center or capacity values.
	explanation := conf021Request().Explain()
	for _, leak := range []string{"plan-21", "budget-21", "cc-21", "2.00"} {
		if strings.Contains(explanation, leak) {
			t.Fatalf("Explain leaks %q: %q", leak, explanation)
		}
	}
}

func TestTodo_CONF_021_Conformance(t *testing.T) {
	approved, err := ApproveHeadcount(conf021Request(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	var states []string
	states = append(states, string(approved.State))
	position, err := RequestPositionCreation(approved, conf021Position(), conf021Approval(), conf021Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	states = append(states, string(position.State))
	// ACCEPTED is progress, never completion.
	position, err = ObservePositionCreation(position, ExternalObservation{State: ExternalAccepted, PayloadDigest: "payload-21"})
	if !errors.Is(err, ErrExternalNotApplied) || position.State != PositionRequested {
		t.Fatalf("accepted observation = %#v, %v", position, err)
	}
	// AMBIGUOUS creates the reconciliation obligation, never silent loss.
	position, err = ObservePositionCreation(position, ExternalObservation{State: ExternalAmbiguous, PayloadDigest: "payload-21"})
	if !errors.Is(err, ErrExternalNotApplied) || position.State != PositionReconciliationRequired {
		t.Fatalf("ambiguous observation = %#v, %v", position, err)
	}
	// A fresh request completes the lifecycle for the golden path.
	position, err = RequestPositionCreation(approved, conf021Position(), conf021Approval(), conf021Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	position, err = ObservePositionCreation(position, conf021Applied("external-pos-21"))
	if err != nil {
		t.Fatal(err)
	}
	states = append(states, string(position.State))
	requisition, err := RequestRequisitionOpen(approved, position, conf021Requisition(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	states = append(states, string(requisition.State))
	requisition, err = ObserveRequisition(requisition, conf021Applied("external-req-21"))
	if err != nil {
		t.Fatal(err)
	}
	states = append(states, string(requisition.State))
	want := []string{"APPROVED", "REQUESTED", "CREATED", "REQUESTED", "OPEN"}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("lifecycle[%d] = %s, want %s (full %v)", i, states[i], want[i], states)
		}
	}
}

func TestTodo_CONF_021_Mutation(t *testing.T) {
	approved, err := ApproveHeadcount(conf021Request(), conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	// Mutant 1: free-text canonical dimensions are refused.
	freetext := conf021Request()
	freetext.JobRef = "senior widget wrangler (free text)"
	freetext.Unit = "FULL_TIME"
	if _, err := ApproveHeadcount(freetext, conf021Approval()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("free-text unit = %v, want ErrInvalidRequest", err)
	}
	// Mutant 2: float-scale FTE never fences exact capacity.
	floatScale := conf021Request()
	floatScale.ID = "hc-float"
	floatScale.Capacity = values.MustDecimal("2.000", 3, values.RoundingExactRequired)
	floatApproved, err := ApproveHeadcount(floatScale, conf021Approval())
	if err != nil {
		t.Fatal(err)
	}
	floatPosition := conf021Position()
	floatPosition.ID = "pos-float"
	floatPosition.ParentRequestID = "hc-float"
	if _, err := RequestPositionCreation(floatApproved, floatPosition, conf021Approval(), conf021Snapshot()); !errors.Is(err, ErrCapacityConflict) {
		t.Fatalf("float-scale fence = %v, want ErrCapacityConflict", err)
	}
	// Mutant 3: a position parented to another request is foreign.
	foreign := conf021Position()
	foreign.ParentRequestID = "hc-99"
	if _, err := RequestPositionCreation(approved, foreign, conf021Approval(), conf021Snapshot()); !errors.Is(err, ErrInvalidRelationship) {
		t.Fatalf("foreign parent = %v, want ErrInvalidRelationship", err)
	}
	// Mutant 4: APPLIED without an external id reconciles nothing.
	if _, err := ObservePositionCreation(freshRequestedPosition(t, approved), ExternalObservation{State: ExternalApplied, PayloadDigest: "payload-21"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("idless applied = %v, want ErrInvalidRequest", err)
	}
	// Mutant 5: budget overcommit is refused even when headcount fits.
	broke := conf021Snapshot()
	broke.BudgetAvailable = values.MustDecimal("1.00", 2, values.RoundingExactRequired)
	if _, err := RequestPositionCreation(approved, conf021Position(), conf021Approval(), broke); !errors.Is(err, ErrBudgetConflict) {
		t.Fatalf("budget overcommit = %v, want ErrBudgetConflict", err)
	}
}

func freshRequestedPosition(t *testing.T, approved HeadcountRequest) PositionProposal {
	t.Helper()
	p, err := RequestPositionCreation(approved, conf021Position(), conf021Approval(), conf021Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_CONF_021_ServedPath(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("served contract id is empty")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("serving contract: %v", err)
	}
}
