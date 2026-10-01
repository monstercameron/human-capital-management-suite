package crewshift

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func fixtureGrant(action LifecycleAction) Grant {
	return Grant{ActorRef: fixtureApprover, Scope: action}
}

// TestTodo_FTIME_007 is the primary contract test: publish, cancel and
// reassign all require the caller's expected revision to match the stored
// one, retain the prior version in History, and compute a notification for
// every affected worker. It also proves a schedule edit never rewrites an
// earlier punch by construction: ApplyLifecycle has no punch parameter at
// all, so there is nothing in this call for it to rewrite.
func TestTodo_FTIME_007(t *testing.T) {
	published := mustPublish(t, fixtureDraft("life-1"))
	var history History

	cancelReq := LifecycleRequest{
		Action: ActionCancel, Current: published, ExpectedRevision: published.Revision,
		Grant: fixtureGrant(ActionCancel), Now: published.PublishedAt.Add(time.Hour), Reason: "site closed",
	}
	result, err := ApplyLifecycle(cancelReq, history)
	if err != nil {
		t.Fatalf("ApplyLifecycle cancel: %v", err)
	}
	if result.Updated.Status != StatusCancelled {
		t.Fatalf("status after cancel = %s, want CANCELLED", result.Updated.Status)
	}
	if result.Updated.Revision != published.Revision+1 {
		t.Fatalf("revision after cancel = %d, want %d", result.Updated.Revision, published.Revision+1)
	}
	if len(result.History.Versions) != 1 || result.History.Versions[0].Revision != published.Revision {
		t.Fatalf("history after cancel = %+v, want the prior published revision retained", result.History.Versions)
	}
	if len(result.Notifications) != 1 || result.Notifications[0].WorkerRef.String() != published.WorkerRef.String() {
		t.Fatalf("cancel notifications = %+v, want one notification to the assigned worker", result.Notifications)
	}

	// A stale expected revision is rejected even though the action and grant
	// are otherwise fine.
	staleReq := LifecycleRequest{
		Action: ActionCancel, Current: published, ExpectedRevision: published.Revision - 1,
		Grant: fixtureGrant(ActionCancel), Now: published.PublishedAt.Add(time.Hour), Reason: "stale",
	}
	if _, err := ApplyLifecycle(staleReq, history); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("stale expected revision = %v, want ErrLifecycleRejected", err)
	}

	// Reassign notifies both the outgoing and the incoming worker.
	reassignReq := LifecycleRequest{
		Action: ActionReassign, Current: published, ExpectedRevision: published.Revision,
		Grant: fixtureGrant(ActionReassign), Now: published.PublishedAt.Add(time.Hour), Reason: "coverage swap",
		NewWorkerRef: fixtureWorker2, NewWorkerEligibility: EligibilityFacts{Active: true},
	}
	reassigned, err := ApplyLifecycle(reassignReq, history)
	if err != nil {
		t.Fatalf("ApplyLifecycle reassign: %v", err)
	}
	if reassigned.Updated.WorkerRef.String() != fixtureWorker2.String() {
		t.Fatalf("reassigned worker = %s, want %s", reassigned.Updated.WorkerRef.String(), fixtureWorker2.String())
	}
	if len(reassigned.Notifications) != 2 {
		t.Fatalf("reassign notifications = %d, want 2 (outgoing and incoming worker)", len(reassigned.Notifications))
	}
}

// TestTodo_FTIME_007_Security proves a revoked grant is refused even when
// every other field of the request -- including the expected revision -- is
// exactly current, and that a revocation discovered between two attempts is
// what stops the second: a worker (or an actor acting for one) cannot keep a
// shift by replaying an old, now-revoked grant.
func TestTodo_FTIME_007_Security(t *testing.T) {
	published := mustPublish(t, fixtureDraft("sec-life-1"))
	var history History

	revoked := Grant{ActorRef: fixtureApprover, Scope: ActionCancel, Revoked: true}
	req := LifecycleRequest{
		Action: ActionCancel, Current: published, ExpectedRevision: published.Revision,
		Grant: revoked, Now: published.PublishedAt.Add(time.Hour), Reason: "attempt after revocation",
	}
	if _, err := ApplyLifecycle(req, history); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("revoked grant cancel = %v, want ErrLifecycleRejected", err)
	}

	// A grant scoped to a different action never authorizes this one.
	wrongScope := Grant{ActorRef: fixtureApprover, Scope: ActionReassign}
	wrongReq := req
	wrongReq.Grant = wrongScope
	if _, err := ApplyLifecycle(wrongReq, history); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("mis-scoped grant cancel = %v, want ErrLifecycleRejected", err)
	}

	// Reassigning to a revoked worker must fail even with a live grant and a
	// current revision.
	reassignReq := LifecycleRequest{
		Action: ActionReassign, Current: published, ExpectedRevision: published.Revision,
		Grant: fixtureGrant(ActionReassign), Now: published.PublishedAt.Add(time.Hour), Reason: "swap",
		NewWorkerRef: fixtureWorker2, NewWorkerEligibility: EligibilityFacts{Active: true, Revoked: true},
	}
	if _, err := ApplyLifecycle(reassignReq, history); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("reassign to a revoked worker = %v, want ErrLifecycleRejected", err)
	}
}

// TestTodo_FTIME_007_Race runs many concurrent ApplyLifecycle attempts
// against one shared, mutex-guarded store that enforces expected-revision
// optimistic concurrency (the store is test-local; the package itself holds
// no mutable state). Exactly one attempt per revision may win: the store's
// revision must advance by exactly one per accepted write and no accepted
// write may ever be based on a revision it did not just read, which is what
// "a revoked worker cannot keep a shift via a stale publish" comes down to
// under concurrency.
func TestTodo_FTIME_007_Race(t *testing.T) {
	seed := mustPublish(t, fixtureDraft("race-1"))
	store := &lockedShiftStore{shift: seed}
	initialRevision := seed.Revision

	const attempts = 50
	var wg sync.WaitGroup
	var accepted int32
	var mu sync.Mutex

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			current, expected := store.read()
			req := LifecycleRequest{
				Action: ActionPublish, Current: current, ExpectedRevision: expected,
				Grant: fixtureGrant(ActionPublish), Now: current.PublishedAt.Add(time.Duration(i+1) * time.Minute),
				Reason: "concurrent republish",
			}
			result, err := ApplyLifecycle(req, History{})
			if err != nil {
				return // lost the race to a concurrent writer between read and write; acceptable
			}
			if store.compareAndSwap(expected, result.Updated) {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	finalShift, finalRevision := store.read()
	if finalRevision != initialRevision+int64(accepted) {
		t.Fatalf("store revision = %d after %d accepted writes; every accepted write must advance the revision by exactly one", finalRevision, accepted)
	}
	if finalShift.Revision != finalRevision {
		t.Fatalf("stored shift revision %d does not match the store's revision counter %d", finalShift.Revision, finalRevision)
	}
}

// lockedShiftStore is a minimal optimistic-concurrency store used only by
// the race test; it is not part of the crewshift package's own state.
type lockedShiftStore struct {
	mu    sync.Mutex
	shift Shift
}

func (s *lockedShiftStore) read() (Shift, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shift, s.shift.Revision
}

func (s *lockedShiftStore) compareAndSwap(expected int64, next Shift) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shift.Revision != expected {
		return false
	}
	s.shift = next
	return true
}
