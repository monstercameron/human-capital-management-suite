package timesession

import (
	"errors"
	"sync"
	"testing"
)

func baseMissedPunchRequest() MissedPunchRequest {
	return MissedPunchRequest{
		Tenant: "acme", Worker: "worker-1", SessionID: "session-1", Claimed: PunchIn,
		ClaimedTime: t0(0), Reason: "forgot to clock in at the gate", RequestedBy: "worker-1",
	}
}

// TestTodo_TCLOCK_011 is the PRIMARY acceptance test: a pending request
// never changes approved time on its own, approval sets it exactly to the
// claimed time, rejection records a reason without touching approved time,
// and a request against a closed period routes to reopen instead of being
// decided.
func TestTodo_TCLOCK_011(t *testing.T) {
	req, err := NewMissedPunchRequest(baseMissedPunchRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.State != RequestPending || !req.ApprovedTime.IsZero() {
		t.Fatalf("new request = %+v, want PENDING with zero approved time", req)
	}

	// PENDING never changes approved time by itself, no matter how many
	// times it is re-validated.
	again, err := NewMissedPunchRequest(req)
	if err != nil || !again.ApprovedTime.IsZero() {
		t.Fatalf("re-validating a pending request changed approved time: %+v", again)
	}

	approver := Approver{Actor: "supervisor-1", HasApprovalScope: true}
	approved, err := Decide(req, approver, PeriodOpen, true, "matches badge log", t0(120))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved.State != RequestApproved || !approved.ApprovedTime.Equal(req.ClaimedTime) {
		t.Fatalf("approved request = %+v, want APPROVED with approved time = claimed time %v", approved, req.ClaimedTime)
	}

	// A rejected request records the reason and leaves approved time zero.
	rejected, err := Decide(req, approver, PeriodOpen, false, "no supporting evidence", t0(120))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rejected.State != RequestRejected || !rejected.ApprovedTime.IsZero() || rejected.Decision != "no supporting evidence" {
		t.Fatalf("rejected request = %+v", rejected)
	}

	// A closed period is routed to reopen, not decided directly, whichever
	// way approve was asked.
	closedPeriodResult, err := Decide(req, approver, PeriodClosed, true, "", t0(120))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if closedPeriodResult.State != RequestRoutedToReopen || !closedPeriodResult.ApprovedTime.IsZero() {
		t.Fatalf("closed-period decision = %+v, want ROUTED_TO_REOPEN with untouched approved time", closedPeriodResult)
	}

	// A request that already left PENDING cannot be decided again.
	if _, err := Decide(approved, approver, PeriodOpen, true, "", t0(200)); !errors.Is(err, ErrRequestNotPending) {
		t.Fatalf("re-deciding an approved request: err = %v, want ErrRequestNotPending", err)
	}
}

// TestTodo_TCLOCK_011_Security is the SECURITY matrix entry: a supervisor
// cannot approve or reject their own missed-punch request, whether they
// are the requester or the affected worker, and out-of-scope approvers are
// refused.
func TestTodo_TCLOCK_011_Security(t *testing.T) {
	req, err := NewMissedPunchRequest(baseMissedPunchRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	selfAsRequester := Approver{Actor: "worker-1", HasApprovalScope: true}
	if _, err := Decide(req, selfAsRequester, PeriodOpen, true, "", t0(1)); !errors.Is(err, ErrSelfApprovalForbidden) {
		t.Fatalf("self-approval by requester: err = %v, want ErrSelfApprovalForbidden", err)
	}

	// Self-approval is refused even against a closed period: it is a
	// security invariant, not a routing decision.
	if _, err := Decide(req, selfAsRequester, PeriodClosed, true, "", t0(1)); !errors.Is(err, ErrSelfApprovalForbidden) {
		t.Fatalf("self-approval against a closed period: err = %v, want ErrSelfApprovalForbidden", err)
	}

	outOfScope := Approver{Actor: "supervisor-2", HasApprovalScope: false}
	if _, err := Decide(req, outOfScope, PeriodOpen, true, "", t0(1)); !errors.Is(err, ErrApproverOutOfScope) {
		t.Fatalf("out-of-scope approver: err = %v, want ErrApproverOutOfScope", err)
	}
}

// TestTodo_TCLOCK_011_Race proves concurrent Decide calls against
// independent copies of the same pending request are deterministic.
func TestTodo_TCLOCK_011_Race(t *testing.T) {
	req, err := NewMissedPunchRequest(baseMissedPunchRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	approver := Approver{Actor: "supervisor-1", HasApprovalScope: true}

	const n = 64
	results := make([]MissedPunchRequest, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = Decide(req, approver, PeriodOpen, true, "matches badge log", t0(120))
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, errs[i])
		}
		if results[i].State != results[0].State || !results[i].ApprovedTime.Equal(results[0].ApprovedTime) {
			t.Fatalf("goroutine %d result = %+v, want %+v", i, results[i], results[0])
		}
	}
}

func TestMissedPunchRequestValidation(t *testing.T) {
	cases := []struct {
		name string
		req  MissedPunchRequest
	}{
		{"missing tenant", MissedPunchRequest{Worker: "w1", SessionID: "s1", Claimed: PunchIn, ClaimedTime: t0(0), Reason: "x", RequestedBy: "w1"}},
		{"undeclared claimed kind", MissedPunchRequest{Tenant: "acme", Worker: "w1", SessionID: "s1", Claimed: "BOGUS", ClaimedTime: t0(0), Reason: "x", RequestedBy: "w1"}},
		{"missing claimed time", MissedPunchRequest{Tenant: "acme", Worker: "w1", SessionID: "s1", Claimed: PunchIn, Reason: "x", RequestedBy: "w1"}},
		{"missing reason", MissedPunchRequest{Tenant: "acme", Worker: "w1", SessionID: "s1", Claimed: PunchIn, ClaimedTime: t0(0), RequestedBy: "w1"}},
		{"missing requested by", MissedPunchRequest{Tenant: "acme", Worker: "w1", SessionID: "s1", Claimed: PunchIn, ClaimedTime: t0(0), Reason: "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewMissedPunchRequest(c.req); !errors.Is(err, ErrInvalidPunch) {
				t.Fatalf("%s: err = %v, want ErrInvalidPunch", c.name, err)
			}
		})
	}
}

func TestDecideRejectsUndeclaredPeriodState(t *testing.T) {
	req, err := NewMissedPunchRequest(baseMissedPunchRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	approver := Approver{Actor: "supervisor-1", HasApprovalScope: true}
	if _, err := Decide(req, approver, "BOGUS", true, "", t0(1)); !errors.Is(err, ErrInvalidPunch) {
		t.Fatalf("undeclared period state: err = %v, want ErrInvalidPunch", err)
	}
}
