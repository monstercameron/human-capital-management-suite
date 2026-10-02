package runstate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func recoveryService(t *testing.T) (*Service, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func TestTodo_AGENTRUN_003_InterruptOrphan(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	t.Run("a run nobody claimed", func(t *testing.T) {
		service, _ := recoveryService(t)
		run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "ready"))
		if err != nil {
			t.Fatal(err)
		}
		done, err := service.InterruptOrphan(ctx, run.ID, run.Version, now.Add(time.Minute))
		if err != nil || done.State != StateFailed || done.TerminalCode != InterruptedCode || !done.Retryable || done.Lease != nil || done.Fence != run.Fence+1 || done.FailureGate != string(FailureGateModelCall) {
			t.Fatalf("interrupted ready run = %+v, %v", done, err)
		}
		if _, err := service.InterruptOrphan(ctx, run.ID, done.Version, now.Add(2*time.Minute)); !errors.Is(err, ErrTerminal) {
			t.Fatalf("interrupting a final run = %v, want ErrTerminal", err)
		}
	})

	t.Run("a run whose worker is gone", func(t *testing.T) {
		service, _ := recoveryService(t)
		run, _ := service.Start(ctx, acceptedAdmissionWithKey(now, "running"))
		claimed, err := service.Claim(ctx, run.ID, "worker-gone", now, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.InterruptOrphan(ctx, run.ID, claimed.Version, now); !errors.Is(err, ErrLease) {
			t.Fatalf("interrupting a run under a live lease = %v, want ErrLease", err)
		}
		if _, err := service.InterruptOrphan(ctx, run.ID, claimed.Version-1, now.Add(time.Minute)); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale revision = %v, want ErrConflict", err)
		}
		done, err := service.InterruptOrphan(ctx, run.ID, claimed.Version, now.Add(time.Minute))
		if err != nil || done.State != StateFailed || done.Fence != claimed.Fence+1 {
			t.Fatalf("interrupted expired run = %+v, %v", done, err)
		}
		// The old holder is fenced out: its late checkpoint is refused.
		if _, err := service.Checkpoint(ctx, run.ID, "worker-gone", claimed.Fence, done.Version, PhaseContext, 0, "late", testDigest("late"), now.Add(time.Second)); err == nil {
			t.Fatal("a worker that lost its run could still write")
		}
	})

	t.Run("an unresolved effect and other states are refused", func(t *testing.T) {
		service, _ := recoveryService(t)
		run, _ := service.Start(ctx, acceptedAdmissionWithKey(now, "effect"))
		claimed, _ := service.Claim(ctx, run.ID, "worker", now, time.Second)
		effect, err := service.BeginEffect(ctx, run.ID, "worker", "effect-1", "key-1", testDigest("args"), claimed.Fence, claimed.Version, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.InterruptOrphan(ctx, run.ID, effect.Version, now.Add(time.Minute)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("an unresolved effect was hidden: %v", err)
		}
		if _, err := service.InterruptOrphan(ctx, "missing", 1, now); err == nil {
			t.Fatal("interrupted a run that does not exist")
		}
		if _, err := service.InterruptOrphan(ctx, run.ID, effect.Version, time.Time{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("no clock = %v", err)
		}
		waiting, _ := service.Start(ctx, acceptedAdmissionWithKey(now, "waiting"))
		claimedWaiting, _ := service.Claim(ctx, waiting.ID, "worker", now, time.Minute)
		parked, err := service.Park(ctx, waiting.ID, "worker", claimedWaiting.Fence, claimedWaiting.Version, WaitSignal, "signal", now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.InterruptOrphan(ctx, waiting.ID, parked.Version, now.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a run waiting for its owner was interrupted: %v", err)
		}
	})
}

func TestTodo_AGENTRUN_001_MemoryRenewLease(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	service, store := recoveryService(t)
	run, _ := service.Start(ctx, acceptedAdmissionWithKey(now, "renew"))
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Minute)
	if err := store.RenewLease(ctx, run.ID, "worker", claimed.Fence, until, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	renewed, _ := store.Get(ctx, run.ID)
	if !renewed.Lease.Until.Equal(until) || renewed.Version != claimed.Version {
		t.Fatalf("renewal moved the lease to %v at revision %d, want %v at %d", renewed.Lease.Until, renewed.Version, until, claimed.Version)
	}
	// Renewal never shortens a lease.
	if err := store.RenewLease(ctx, run.ID, "worker", claimed.Fence, now.Add(time.Second), now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if again, _ := store.Get(ctx, run.ID); !again.Lease.Until.Equal(until) {
		t.Fatalf("renewal shortened the lease to %v", again.Lease.Until)
	}
	for name, call := range map[string]func() error{
		"another owner": func() error { return store.RenewLease(ctx, run.ID, "other", claimed.Fence, until, now) },
		"another fence": func() error { return store.RenewLease(ctx, run.ID, "worker", claimed.Fence+1, until, now) },
		"after expiry": func() error {
			return store.RenewLease(ctx, run.ID, "worker", claimed.Fence, until.Add(time.Hour), until.Add(time.Second))
		},
		"another run":    func() error { return store.RenewLease(ctx, "missing", "worker", claimed.Fence, until, now) },
		"a nil store":    func() error { return (*MemoryStore)(nil).RenewLease(ctx, run.ID, "worker", claimed.Fence, until, now) },
		"a finished run": func() error { return finishedRenewal(t, ctx) },
	} {
		if err := call(); err == nil {
			t.Fatalf("renewal succeeded for %s", name)
		}
	}
}

func finishedRenewal(t *testing.T, ctx context.Context) error {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	service, store := recoveryService(t)
	run, _ := service.Start(ctx, acceptedAdmissionWithKey(now, "finished"))
	claimed, _ := service.Claim(ctx, run.ID, "worker", now, time.Minute)
	if _, err := service.Fail(ctx, run.ID, "worker", "SOME_CODE", false, claimed.Fence, claimed.Version, now); err != nil {
		t.Fatal(err)
	}
	return store.RenewLease(ctx, run.ID, "worker", claimed.Fence, now.Add(time.Hour), now)
}

// Two processes finishing the same orphan write one terminal state.
func TestTodo_AGENTRUN_003_InterruptRace(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	service, store := recoveryService(t)
	run, _ := service.Start(ctx, acceptedAdmissionWithKey(now, "race"))
	var wins, losses int
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.InterruptOrphan(ctx, run.ID, run.Version, now.Add(time.Minute))
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if errors.Is(err, ErrConflict) || errors.Is(err, ErrTerminal) {
				losses++
			}
		}()
	}
	close(start)
	wg.Wait()
	done, _ := store.Get(ctx, run.ID)
	if wins != 1 || losses != 7 || done.Fence != run.Fence+1 || done.State != StateFailed {
		t.Fatalf("wins=%d losses=%d run=%+v: want exactly one finisher", wins, losses, done)
	}
}
