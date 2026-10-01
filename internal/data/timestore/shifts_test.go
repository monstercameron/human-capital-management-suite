package timestore

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
)

func draftShiftFixture(t *testing.T, s *Store, tenant, id, worker string) Shift {
	t.Helper()
	sh := Shift{TenantID: tenant, ID: id, WorkerRef: worker, SiteRef: "site-1", ProjectRef: "project-1",
		Status: ShiftDraft, WorkStart: time.Date(2026, 3, 8, 8, 0, 0, 0, time.UTC), WorkEnd: time.Date(2026, 3, 8, 16, 0, 0, 0, time.UTC),
		Payload: []byte(`{"breaks":[]}`)}
	if err := s.CreateShift(context.Background(), tenant, sh, "create-"+id); err != nil {
		t.Fatalf("CreateShift: %v", err)
	}
	return sh
}

func TestTodo_FTIME_006(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draftShiftFixture(t, s, "tenant-a", "shift-1", "worker-1")
	got, err := s.GetShift(ctx, "tenant-a", "shift-1")
	if err != nil || got.Revision != 1 || got.Status != ShiftDraft {
		t.Fatalf("GetShift = %#v, %v", got, err)
	}
}

// TestTodo_FTIME_006_Golden pins that a draft's history entry is never
// altered by a later publish: the DRAFTED history row keeps its original
// payload bytes.
func TestTodo_FTIME_006_Golden(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draftShiftFixture(t, s, "tenant-a", "shift-golden", "worker-1")
	if _, err := s.PublishShift(ctx, "tenant-a", "shift-golden", 1, "supervisor-1", "publish-golden", func(cur Shift) (Shift, error) {
		cur.Payload = []byte(`{"breaks":[{"kind":"MEAL"}]}`)
		return cur, nil
	}); err != nil {
		t.Fatal(err)
	}
	history, err := s.ShiftHistory(ctx, "tenant-a", "shift-golden")
	if err != nil || len(history) != 2 {
		t.Fatalf("history = %#v, %v", history, err)
	}
	if !jsonEqual(t, history[0].Payload, []byte(`{"breaks":[]}`)) {
		t.Fatalf("drafted history mutated: %s", history[0].Payload)
	}
	if history[1].Kind != ShiftHistoryPublished || history[1].ApprovedBy != "supervisor-1" {
		t.Fatalf("published history entry = %#v", history[1])
	}
}

// TestTodo_FTIME_006_Integration and TestTodo_FTIME_007_Integration together
// exercise the full lifecycle: publish, reassign and cancel, each retaining
// its own history entry and none rewriting an earlier one.
func TestTodo_FTIME_006_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draftShiftFixture(t, s, "tenant-a", "shift-lifecycle", "worker-1")

	published, err := s.PublishShift(ctx, "tenant-a", "shift-lifecycle", 1, "supervisor-1", "publish-1", func(cur Shift) (Shift, error) { return cur, nil })
	if err != nil || published.Status != ShiftPublished || published.Revision != 2 {
		t.Fatalf("publish = %#v, %v", published, err)
	}
	// A repeated publish with the same idempotency key never double-applies.
	replay, err := s.PublishShift(ctx, "tenant-a", "shift-lifecycle", 1, "supervisor-1", "publish-1", func(Shift) (Shift, error) {
		t.Fatal("mutate ran on replay")
		return Shift{}, nil
	})
	if err != nil || replay.Revision != 2 {
		t.Fatalf("replay publish = %#v, %v", replay, err)
	}

	cancelled, err := s.CancelShift(ctx, "tenant-a", "shift-lifecycle", 2, "supervisor-1", "cancel-1")
	if err != nil || cancelled.Status != ShiftCancelled || cancelled.Revision != 3 {
		t.Fatalf("cancel = %#v, %v", cancelled, err)
	}

	history, err := s.ShiftHistory(ctx, "tenant-a", "shift-lifecycle")
	if err != nil || len(history) != 3 {
		t.Fatalf("history = %#v, %v", history, err)
	}
	kinds := []ShiftHistoryKind{history[0].Kind, history[1].Kind, history[2].Kind}
	if kinds[0] != ShiftHistoryDrafted || kinds[1] != ShiftHistoryPublished || kinds[2] != ShiftHistoryCancelled {
		t.Fatalf("history kinds = %v", kinds)
	}
}

func TestTodo_FTIME_007_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draftShiftFixture(t, s, "tenant-a", "shift-reassign", "worker-1")
	if _, err := s.PublishShift(ctx, "tenant-a", "shift-reassign", 1, "supervisor-1", "publish-r", func(cur Shift) (Shift, error) { return cur, nil }); err != nil {
		t.Fatal(err)
	}
	reassigned, err := s.ReassignShift(ctx, "tenant-a", "shift-reassign", 2, "worker-2", "supervisor-1", "reassign-1")
	if err != nil || reassigned.WorkerRef != "worker-2" || reassigned.Revision != 3 {
		t.Fatalf("reassign = %#v, %v", reassigned, err)
	}
	// A stale expected revision is refused rather than silently reassigning
	// again from an outdated read.
	if _, err := s.ReassignShift(ctx, "tenant-a", "shift-reassign", 2, "worker-3", "supervisor-1", "reassign-2"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale reassign = %v, want ErrRevisionConflict", err)
	}
}

// TestTodo_FTIME_007_Race proves a concurrent cancel and reassign racing on
// the same shift revision can only have one winner.
func TestTodo_FTIME_007_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draftShiftFixture(t, s, "tenant-a", "shift-race", "worker-1")
	if _, err := s.PublishShift(ctx, "tenant-a", "shift-race", 1, "supervisor-1", "publish-race", func(cur Shift) (Shift, error) { return cur, nil }); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = s.CancelShift(ctx, "tenant-a", "shift-race", 2, "supervisor-1", "cancel-race")
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = s.ReassignShift(ctx, "tenant-a", "shift-race", 2, "worker-2", "supervisor-1", "reassign-race")
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
}

func TestTodo_FTIME_006_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draftShiftFixture(t, s, "tenant-a", "shift-shared", "worker-1")
	if _, err := s.GetShift(ctx, "tenant-b", "shift-shared"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read = %v, want ErrNotFound", err)
	}
	if _, err := s.PublishShift(ctx, "tenant-b", "shift-shared", 1, "intruder", "k", func(cur Shift) (Shift, error) { return cur, nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant publish = %v, want ErrNotFound", err)
	}
}

func TestTodo_FTIME_007_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draftShiftFixture(t, s, "tenant-a", "shift-shared-2", "worker-1")
	if _, err := s.ReassignShift(ctx, "tenant-b", "shift-shared-2", 1, "worker-x", "intruder", "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant reassign = %v, want ErrNotFound", err)
	}
}

// TestTodo_FTIME_006_Property runs many random publish/cancel/reassign
// sequences and checks two invariants that must hold no matter the
// sequence: the aggregate's revision always equals the number of retained
// history rows, and history is always ordered 1..N with no gaps.
func TestTodo_FTIME_006_Property(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 25; trial++ {
		id := "shift-prop-" + strconv.Itoa(trial)
		draftShiftFixture(t, s, "tenant-prop", id, "worker-1")
		revision := int64(1)
		steps := rng.Intn(5) + 1
		published := false
		cancelled := false
		for i := 0; i < steps && !cancelled; i++ {
			key := "k" + strconv.Itoa(trial) + "-" + strconv.Itoa(i)
			switch rng.Intn(3) {
			case 0:
				if published {
					continue
				}
				if _, err := s.PublishShift(ctx, "tenant-prop", id, revision, "sup", key, func(cur Shift) (Shift, error) { return cur, nil }); err != nil {
					t.Fatalf("publish: %v", err)
				}
				revision++
				published = true
			case 1:
				if _, err := s.ReassignShift(ctx, "tenant-prop", id, revision, "worker-2", "sup", key); err != nil {
					t.Fatalf("reassign: %v", err)
				}
				revision++
			case 2:
				if _, err := s.CancelShift(ctx, "tenant-prop", id, revision, "sup", key); err != nil {
					t.Fatalf("cancel: %v", err)
				}
				revision++
				cancelled = true
			}
		}
		got, err := s.GetShift(ctx, "tenant-prop", id)
		if err != nil || got.Revision != revision {
			t.Fatalf("trial %d: revision = %d, want %d (%v)", trial, got.Revision, revision, err)
		}
		history, err := s.ShiftHistory(ctx, "tenant-prop", id)
		if err != nil || int64(len(history)) != revision {
			t.Fatalf("trial %d: history len = %d, want %d (%v)", trial, len(history), revision, err)
		}
		for i, h := range history {
			if h.Revision != int64(i+1) {
				t.Fatalf("trial %d: history not sequential at %d: %#v", trial, i, h)
			}
		}
	}
}

func TestTodo_FTIME_006_InvalidInputsRejected(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.CreateShift(ctx, "tenant-a", Shift{}, "k"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty shift = %v", err)
	}
	draftShiftFixture(t, s, "tenant-a", "shift-invalid", "worker-1")
	if _, err := s.PublishShift(ctx, "tenant-a", "shift-invalid", 1, "", "k", func(cur Shift) (Shift, error) { return cur, nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty approver = %v", err)
	}
	if _, err := s.ReassignShift(ctx, "tenant-a", "shift-invalid", 1, "", "sup", "k"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty new worker = %v", err)
	}
}
