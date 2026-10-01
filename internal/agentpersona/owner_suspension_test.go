package agentpersona

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type ownerSuspensionStoreFake struct {
	mu         sync.Mutex
	candidates []OwnerSuspensionCandidate
	changes    int
	clears     int
	marked     int
}

func (s *ownerSuspensionStoreFake) RecordOwnerChange(_ context.Context, c OwnerSuspensionCandidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changes++
	s.candidates = []OwnerSuspensionCandidate{c}
	return nil
}
func (s *ownerSuspensionStoreFake) ClearOwnerChange(_ context.Context, _, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clears++
	return nil
}
func (s *ownerSuspensionStoreFake) ListOwnerSuspensionCandidates(context.Context) ([]OwnerSuspensionCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]OwnerSuspensionCandidate(nil), s.candidates...), nil
}
func (s *ownerSuspensionStoreFake) MarkSuspended(_ context.Context, candidate OwnerSuspensionCandidate, _ uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked++
	filtered := s.candidates[:0]
	for _, item := range s.candidates {
		if item.TenantID != candidate.TenantID || item.PersonaID != candidate.PersonaID || item.PersonaVersion != candidate.PersonaVersion || item.InstallationID != candidate.InstallationID {
			filtered = append(filtered, item)
		}
	}
	s.candidates = filtered
	return nil
}

type ownerSuspensionFake struct {
	mu    sync.Mutex
	calls []OwnerSuspensionRequest
	err   error
}

func (f *ownerSuspensionFake) Suspend(_ context.Context, req OwnerSuspensionRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return f.err
}

type ownerFenceFake struct {
	mu    sync.Mutex
	calls []OwnerSuspensionRequest
	err   error
}

func (f *ownerFenceFake) Fence(_ context.Context, req OwnerSuspensionRequest) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return uint64(len(f.calls)), f.err
}

type ownerNotifierFake struct {
	calls int
	err   error
}

func (f *ownerNotifierFake) NotifyOwnerSuspended(context.Context, OwnerSuspensionCandidate, string) error {
	f.calls++
	return f.err
}

func ownerCoordinatorFixture(t *testing.T, store *ownerSuspensionStoreFake, suspender *ownerSuspensionFake, fence *ownerFenceFake) *OwnerSuspensionCoordinator {
	t.Helper()
	c, err := NewOwnerSuspensionCoordinator(OwnerSuspensionConfig{Store: store, Suspender: suspender, Fence: fence, Clock: func() time.Time { return time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTodo_AGENTP_016_ApplyOwnerChangeStartsWindow(t *testing.T) {
	store := &ownerSuspensionStoreFake{}
	c := ownerCoordinatorFixture(t, store, &ownerSuspensionFake{}, &ownerFenceFake{})
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := c.ApplyOwnerChange(context.Background(), OwnerChange{TenantID: "tenant", PersonaID: "persona", PersonaVersion: 2, OwnerID: "owner", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	if len(store.candidates) != 1 || !store.candidates[0].ReassignmentDue.Equal(at.Add(OwnerReassignmentGrace)) {
		t.Fatalf("window = %+v", store.candidates)
	}
}

func TestTodo_AGENTP_016_SweepFencesBeforeSuspending(t *testing.T) {
	at := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	store := &ownerSuspensionStoreFake{candidates: []OwnerSuspensionCandidate{{TenantID: "tenant", PersonaID: "persona", PersonaVersion: 2, OwnerID: "owner", ReassignmentDue: at}}}
	suspender, fence := &ownerSuspensionFake{}, &ownerFenceFake{}
	c := ownerCoordinatorFixture(t, store, suspender, fence)
	if err := c.Sweep(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if len(fence.calls) != 1 || len(suspender.calls) != 1 || store.marked != 1 {
		t.Fatalf("fence=%d suspend=%d marked=%d", len(fence.calls), len(suspender.calls), store.marked)
	}
}

func TestTodo_AGENTP_016_FaultFenceLeavesCandidateForRetry(t *testing.T) {
	at := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	store := &ownerSuspensionStoreFake{candidates: []OwnerSuspensionCandidate{{TenantID: "tenant", PersonaID: "persona", PersonaVersion: 1, OwnerID: "owner", ReassignmentDue: at}}}
	fence := &ownerFenceFake{err: errors.New("epoch store down")}
	c := ownerCoordinatorFixture(t, store, &ownerSuspensionFake{}, fence)
	if err := c.Sweep(context.Background(), at); err == nil || store.marked != 0 {
		t.Fatalf("sweep=%v marked=%d", err, store.marked)
	}
}

func TestTodo_AGENTP_016_ExplicitTerminationFencesBeforeSuspend(t *testing.T) {
	suspender, fence := &ownerSuspensionFake{}, &ownerFenceFake{}
	c := ownerCoordinatorFixture(t, &ownerSuspensionStoreFake{}, suspender, fence)
	err := c.Suspend(context.Background(), OwnerSuspensionRequest{Scope: OwnerSuspensionTenant, TenantID: "tenant", Reason: "OWNER_TERMINATED"})
	if err != nil || len(fence.calls) != 1 || len(suspender.calls) != 1 {
		t.Fatalf("err=%v fence=%d suspend=%d", err, len(fence.calls), len(suspender.calls))
	}
}

func TestTodo_AGENTP_016_RaceConcurrentSweepsSuspendOncePerProcess(t *testing.T) {
	at := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	store := &ownerSuspensionStoreFake{candidates: []OwnerSuspensionCandidate{{TenantID: "tenant", PersonaID: "persona", PersonaVersion: 1, OwnerID: "owner", ReassignmentDue: at}}}
	suspender, fence := &ownerSuspensionFake{}, &ownerFenceFake{}
	c := ownerCoordinatorFixture(t, store, suspender, fence)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Sweep(context.Background(), at) }()
	}
	wg.Wait()
	if len(suspender.calls) != 1 {
		t.Fatalf("calls=%d; concurrent sweeps must claim the candidate once", len(suspender.calls))
	}
}

func TestTodo_AGENTP_016_FailClosedWiring(t *testing.T) {
	if _, err := NewOwnerSuspensionCoordinator(OwnerSuspensionConfig{}); !errors.Is(err, ErrOwnerSuspensionUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
