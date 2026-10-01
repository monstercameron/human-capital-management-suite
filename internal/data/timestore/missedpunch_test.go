package timestore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTodo_TCLOCK_011(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{
		TenantID: "tenant-a", ID: "mp-1", WorkerRef: "worker-1", ClaimedTime: time.Now().UTC(),
		Reason: "forgot to clock in", RequestedBy: "worker-1", SupervisorRef: "supervisor-1",
	})
	if err != nil || r.Status != MissedPunchPending || r.Revision != 1 {
		t.Fatalf("submit = %#v, %v", r, err)
	}
}

// TestTodo_TCLOCK_011_Integration proves the whole request/decision path:
// approval appends a decision, rejection records a reason, and a request
// against a closed period is refused without a typed reopen reference.
func TestTodo_TCLOCK_011_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{
		TenantID: "tenant-a", ID: "mp-approve", WorkerRef: "worker-1", ClaimedTime: time.Now().UTC(),
		Reason: "forgot to clock out", RequestedBy: "worker-1", SupervisorRef: "supervisor-1",
	}); err != nil {
		t.Fatal(err)
	}
	approved, err := s.DecideMissedPunch(ctx, "tenant-a", "mp-approve", MissedPunchApproved, "supervisor-1", "confirmed with badge log", "")
	if err != nil || approved.Status != MissedPunchApproved || approved.Revision != 2 {
		t.Fatalf("decide approve = %#v, %v", approved, err)
	}
	decisions, err := s.MissedPunchDecisions(ctx, "tenant-a", "mp-approve")
	if err != nil || len(decisions) != 1 || decisions[0].Decision != MissedPunchApproved {
		t.Fatalf("decisions = %#v, %v", decisions, err)
	}

	if _, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{
		TenantID: "tenant-a", ID: "mp-reject", WorkerRef: "worker-1", ClaimedTime: time.Now().UTC(),
		Reason: "claimed extra hours", RequestedBy: "worker-1", SupervisorRef: "supervisor-1",
	}); err != nil {
		t.Fatal(err)
	}
	rejected, err := s.DecideMissedPunch(ctx, "tenant-a", "mp-reject", MissedPunchRejected, "supervisor-1", "no supporting evidence", "")
	if err != nil || rejected.Status != MissedPunchRejected {
		t.Fatalf("decide reject = %#v, %v", rejected, err)
	}

	if _, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{
		TenantID: "tenant-a", ID: "mp-closed", WorkerRef: "worker-1", ClaimedTime: time.Now().UTC(),
		Reason: "closed period claim", RequestedBy: "worker-1", SupervisorRef: "supervisor-1", PeriodClosed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecideMissedPunch(ctx, "tenant-a", "mp-closed", MissedPunchApproved, "supervisor-1", "ok", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("decide against closed period without reopen = %v, want ErrInvalid", err)
	}
	viaReopen, err := s.DecideMissedPunch(ctx, "tenant-a", "mp-closed", MissedPunchApproved, "supervisor-1", "ok", "reopen-ref-1")
	if err != nil || viaReopen.Status != MissedPunchApproved {
		t.Fatalf("decide via reopen = %#v, %v", viaReopen, err)
	}
}

func TestTodo_TCLOCK_011_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{
		TenantID: "tenant-a", ID: "mp-self", WorkerRef: "worker-1", ClaimedTime: time.Now().UTC(),
		Reason: "x", RequestedBy: "worker-1", SupervisorRef: "worker-1",
	}); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("requestor == supervisor at submit = %v, want ErrSelfApproval", err)
	}
	if _, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{
		TenantID: "tenant-a", ID: "mp-shared", WorkerRef: "worker-1", ClaimedTime: time.Now().UTC(),
		Reason: "x", RequestedBy: "worker-1", SupervisorRef: "supervisor-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecideMissedPunch(ctx, "tenant-a", "mp-shared", MissedPunchApproved, "worker-1", "self approving", ""); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self decision = %v, want ErrSelfApproval", err)
	}
	if _, err := s.GetMissedPunchRequest(ctx, "tenant-b", "mp-shared"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read = %v, want ErrNotFound", err)
	}
}

// TestTodo_TCLOCK_011_Race proves two concurrent decisions on the same
// PENDING request can only have one winner.
func TestTodo_TCLOCK_011_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{
		TenantID: "tenant-a", ID: "mp-race", WorkerRef: "worker-1", ClaimedTime: time.Now().UTC(),
		Reason: "x", RequestedBy: "worker-1", SupervisorRef: "supervisor-1",
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = s.DecideMissedPunch(ctx, "tenant-a", "mp-race", MissedPunchApproved, "supervisor-1", "approve", "")
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = s.DecideMissedPunch(ctx, "tenant-a", "mp-race", MissedPunchRejected, "supervisor-2", "reject", "")
	}()
	wg.Wait()
	successes, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want exactly one winner", successes, conflicts)
	}
	decisions, err := s.MissedPunchDecisions(ctx, "tenant-a", "mp-race")
	if err != nil || len(decisions) != 1 {
		t.Fatalf("decisions after race = %#v, %v, want exactly one recorded decision", decisions, err)
	}
}

func TestTodo_TCLOCK_011_InvalidInputsRejected(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.SubmitMissedPunchRequest(ctx, "tenant-a", MissedPunchRequest{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty request = %v", err)
	}
	if _, err := s.DecideMissedPunch(ctx, "tenant-a", "missing", MissedPunchApproved, "sup", "r", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing request decision = %v", err)
	}
	if _, err := s.DecideMissedPunch(ctx, "tenant-a", "x", "BOGUS", "sup", "r", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid decision value = %v", err)
	}
}
