package headcount

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func decimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func request() HeadcountRequest {
	return HeadcountRequest{Tenant: "tenant-a", ID: "hc-1", Requester: "manager-1", PlanRef: "plan-1", BudgetRef: "budget-1", OrganizationRef: "org-1", JobRef: "job-1", LocationRef: "loc-1", CostCenterRef: "cc-1", PositionCount: 1, Capacity: values.MustDecimal("1.00", 2, values.RoundingExactRequired), Unit: UnitFTE, State: HeadcountSubmitted, ProposalRevision: 1}
}
func approval() ApprovalCertificate {
	return ApprovalCertificate{PolicyVersion: "policy-v1", DecisionDigest: "decision-v1", Requester: "manager-1", ApproverRefs: []string{"hr-1", "finance-1"}, RequiredQuorum: 2, ResolvedByPolicy: true, SeparationOfDuties: true}
}
func snapshot(t *testing.T) CapacitySnapshot {
	return CapacitySnapshot{Capacity: decimal(t, "2.00"), Consumed: decimal(t, "0.00"), Reserved: decimal(t, "0.00"), BudgetAvailable: decimal(t, "2.00"), BaselineVersion: "baseline-v1", ReservationFence: 1}
}
func position() PositionProposal {
	return PositionProposal{Tenant: "tenant-a", ID: "pos-1", ParentRequestID: "hc-1", Requester: "manager-1", State: PositionProposed, BudgetReservationRef: "budget-hold-1", CapacityReservationRef: "position-hold-1"}
}
func TestHeadcountPositionAndRequisitionRemainDistinctGovernedTransactions(t *testing.T) {
	r, err := ApproveHeadcount(request(), approval())
	if err != nil {
		t.Fatal(err)
	}
	if r.State != HeadcountApproved {
		t.Fatalf("state = %s", r.State)
	}
	p, err := RequestPositionCreation(r, position(), approval(), snapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.State != PositionRequested {
		t.Fatalf("position state = %s", p.State)
	}
	if _, err := RequestRequisitionOpen(r, p, RequisitionProposal{Tenant: "tenant-a", ID: "req-1", ParentRequestID: "hc-1", PositionID: "pos-1", Requester: "manager-1", State: RequisitionProposed}, approval()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("requisition before position created = %v", err)
	}
}
func TestTodo_HEADCOUNT_001_Property(t *testing.T) {
	if err := CheckCapacity(snapshot(t), decimal(t, "2.01")); !errors.Is(err, ErrCapacityConflict) {
		t.Fatalf("capacity error = %v", err)
	}
}
func TestTodo_HEADCOUNT_001_Golden(t *testing.T) {
	r, _ := ApproveHeadcount(request(), approval())
	if r.Explain() == "" || r.State != HeadcountApproved {
		t.Fatalf("request = %#v", r)
	}
}
func TestTodo_HEADCOUNT_001_Race(t *testing.T) {
	s := snapshot(t)
	requested := decimal(t, "2.00")
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- CheckCapacity(s, requested)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent capacity check = %v", err)
		}
	}
	if err := CheckCapacity(s, decimal(t, "2.01")); !errors.Is(err, ErrCapacityConflict) {
		t.Fatalf("boundary capacity check = %v", err)
	}
}
func TestTodo_HEADCOUNT_001_Fault(t *testing.T) {
	r := request()
	r.State = HeadcountApproved
	if _, err := ApproveHeadcount(r, approval()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("fault = %v", err)
	}
}
func TestTodo_HEADCOUNT_001_Security(t *testing.T) {
	a := approval()
	a.ApproverRefs[0] = "manager-1"
	if _, err := ApproveHeadcount(request(), a); !errors.Is(err, ErrInvalidApproval) {
		t.Fatalf("requester approver = %v", err)
	}
}
func TestTodo_HEADCOUNT_001_Conformance(t *testing.T) {
	r, _ := ApproveHeadcount(request(), approval())
	p, _ := RequestPositionCreation(r, position(), approval(), snapshot(t))
	p, err := ObservePositionCreation(p, ExternalObservation{State: ExternalRequested, PayloadDigest: "payload"})
	if !errors.Is(err, ErrExternalNotApplied) || p.State != PositionRequested {
		t.Fatalf("requested observation = %#v, %v", p, err)
	}
}

func TestHeadcount_PositionRequiresBothReservationReferences(t *testing.T) {
	r, err := ApproveHeadcount(request(), approval())
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*PositionProposal){
		"missing budget reservation":   func(p *PositionProposal) { p.BudgetReservationRef = "" },
		"missing capacity reservation": func(p *PositionProposal) { p.CapacityReservationRef = "" },
	} {
		t.Run(name, func(t *testing.T) {
			p := position()
			mutate(&p)
			if _, err := RequestPositionCreation(r, p, approval(), snapshot(t)); !errors.Is(err, ErrInvalidRelationship) {
				t.Fatalf("reservation validation err = %v, want ErrInvalidRelationship", err)
			}
		})
	}
}

func TestHeadcount_ObserveRequisition_RecordsEveryExternalOutcome(t *testing.T) {
	q := RequisitionProposal{Tenant: "tenant-a", ID: "req-1", ParentRequestID: "hc-1", PositionID: "pos-1", Requester: "manager-1", State: RequisitionRequested}
	for name, tc := range map[string]struct {
		observation ExternalObservation
		wantState   RequisitionState
		wantErr     error
	}{
		"not applied": {ExternalObservation{State: ExternalRequested, PayloadDigest: "payload"}, RequisitionRequested, ErrExternalNotApplied},
		"ambiguous":   {ExternalObservation{State: ExternalAmbiguous, PayloadDigest: "payload"}, RequisitionReconciliationRequired, ErrExternalNotApplied},
		"applied":     {ExternalObservation{State: ExternalApplied, ExternalID: "external-req-1", PayloadDigest: "payload"}, RequisitionOpen, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ObserveRequisition(q, tc.observation)
			if !errors.Is(err, tc.wantErr) || got.State != tc.wantState || got.External != tc.observation {
				t.Fatalf("observation = %+v, err=%v; want state=%s err=%v", got, err, tc.wantState, tc.wantErr)
			}
		})
	}
	if _, err := ObserveRequisition(q, ExternalObservation{State: ExternalApplied, PayloadDigest: "payload"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing external id err = %v, want ErrInvalidRequest", err)
	}
}
func TestTodo_HEADCOUNT_001_Mutation(t *testing.T) {
	r, _ := ApproveHeadcount(request(), approval())
	p, _ := RequestPositionCreation(r, position(), approval(), snapshot(t))
	p, err := ObservePositionCreation(p, ExternalObservation{State: ExternalApplied, ExternalID: "external-pos-1", PayloadDigest: "payload"})
	if err != nil || p.State != PositionCreated {
		t.Fatalf("applied observation = %#v, %v", p, err)
	}
}

func TestHeadcount_RequisitionCapacityRequiresApprovalAndPinsCurrentFence(t *testing.T) {
	r, err := ApproveHeadcount(request(), approval())
	if err != nil {
		t.Fatal(err)
	}
	s := snapshot(t)
	s.Consumed = decimal(t, "0.10")
	s.Reserved = decimal(t, "0.15")
	proof, err := ApprovedRequisitionCapacity(r, s)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Tenant != r.Tenant || proof.RequestID != r.ID || proof.Revision != r.ProposalRevision || proof.State != HeadcountApproved ||
		proof.Available.String() != "0.75" || proof.BaselineVersion != s.BaselineVersion || proof.ReservationFence != s.ReservationFence {
		t.Fatalf("capacity proof = %+v", proof)
	}
	if _, err := ApprovedRequisitionCapacity(request(), s); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unapproved request err = %v", err)
	}
	s.Consumed = decimal(t, "1.20")
	if proof, err := ApprovedRequisitionCapacity(r, s); err != nil || proof.Available.String() != "0.00" {
		t.Fatalf("fully consumed capacity proof = %+v err=%v", proof, err)
	}
	s.Reserved = values.MustDecimal("0.0", 1, values.RoundingExactRequired)
	if _, err := ApprovedRequisitionCapacity(r, s); !errors.Is(err, ErrCapacityConflict) {
		t.Fatalf("mismatched scale err = %v", err)
	}
}

func TestHeadcount_RequisitionCapacityReferenceAndReservationValidation(t *testing.T) {
	if err := (RequisitionCapacityReference{}).Validate(); !errors.Is(err, ErrInvalidRelationship) {
		t.Fatalf("empty reference err = %v", err)
	}
	if err := (RequisitionCapacityReference{Tenant: "tenant-a", RequestID: "hc-1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	reservation := RequisitionCapacityReservation{ID: "hold-1", Tenant: "tenant-a", RequestID: "hc-1", RequisitionID: "req-1",
		Amount: decimal(t, "0.25"), CapacityRevision: 3, ReservationFence: 9}
	if err := reservation.Validate(); err != nil {
		t.Fatal(err)
	}
	reservation.ReservationFence = 0
	if err := reservation.Validate(); !errors.Is(err, ErrInvalidRelationship) {
		t.Fatalf("unfenced reservation err = %v", err)
	}
}
