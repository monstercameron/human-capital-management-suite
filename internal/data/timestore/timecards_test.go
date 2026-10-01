package timestore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func timecardFixture(t *testing.T, s *Store, tenant, id, worker string) Timecard {
	t.Helper()
	tc := Timecard{TenantID: tenant, ID: id, WorkerRef: worker,
		PeriodStart: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
		State:       TimecardOpen, Payload: []byte(`{"lines":[]}`)}
	if err := s.CreateTimecard(context.Background(), tenant, tc); err != nil {
		t.Fatalf("CreateTimecard: %v", err)
	}
	return tc
}

func TestTodo_FTIME_004(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	timecardFixture(t, s, "tenant-a", "tc-1", "worker-1")
	got, err := s.GetTimecard(ctx, "tenant-a", "tc-1")
	if err != nil || got.Revision != 1 || got.State != TimecardOpen {
		t.Fatalf("GetTimecard = %#v, %v", got, err)
	}
}

// TestTodo_FTIME_004_Integration exercises the full review path against a
// real store: a line is recorded, a reasoned correction appends evidence
// without overwriting the original, and approval pins the reviewed
// revision.
func TestTodo_FTIME_004_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	timecardFixture(t, s, "tenant-a", "tc-2", "worker-1")

	afterLine, err := s.AppendTimecardEvent(ctx, "tenant-a", 1, "", TimecardEvent{
		TenantID: "tenant-a", TimecardID: "tc-2", Kind: EventLine, ActorID: "worker-1", IdempotencyKey: "line-1",
		Payload: []byte(`{"start":"2026-01-05T09:00:00Z","end":"2026-01-05T17:00:00Z"}`),
	})
	if err != nil || afterLine.Revision != 2 {
		t.Fatalf("append line: %#v, %v", afterLine, err)
	}

	afterCorrection, err := s.AppendTimecardEvent(ctx, "tenant-a", 2, "", TimecardEvent{
		TenantID: "tenant-a", TimecardID: "tc-2", Kind: EventCorrection, ActorID: "supervisor-1", IdempotencyKey: "corr-1",
		Payload: []byte(`{"reason":"missing out punch","new_end":"2026-01-05T17:30:00Z"}`),
	})
	if err != nil || afterCorrection.Revision != 3 {
		t.Fatalf("append correction: %#v, %v", afterCorrection, err)
	}

	approved, err := s.AppendTimecardEvent(ctx, "tenant-a", 3, TimecardApproved, TimecardEvent{
		TenantID: "tenant-a", TimecardID: "tc-2", Kind: EventApproval, ActorID: "supervisor-1", IdempotencyKey: "appr-1",
		Payload: []byte(`{"applicable_rules":["FLSA"]}`),
	})
	if err != nil || approved.Revision != 4 || approved.State != TimecardApproved {
		t.Fatalf("approve: %#v, %v", approved, err)
	}

	history, err := s.TimecardHistory(ctx, "tenant-a", "tc-2")
	if err != nil || len(history) != 3 {
		t.Fatalf("TimecardHistory = %#v, %v", history, err)
	}
	if history[0].Kind != EventLine || history[1].Kind != EventCorrection || history[2].Kind != EventApproval {
		t.Fatalf("history kinds out of order: %#v", history)
	}
	// The original line event is untouched -- the correction is a new event,
	// never a rewrite.
	if !jsonEqual(t, history[0].Payload, []byte(`{"start":"2026-01-05T09:00:00Z","end":"2026-01-05T17:00:00Z"}`)) {
		t.Fatalf("original line mutated: %s", history[0].Payload)
	}

	// A typed reopen path is available after approval.
	reopened, err := s.AppendTimecardEvent(ctx, "tenant-a", 4, TimecardReopened, TimecardEvent{
		TenantID: "tenant-a", TimecardID: "tc-2", Kind: EventReopen, ActorID: "supervisor-1", IdempotencyKey: "reopen-1",
		Payload: []byte(`{"reason":"payroll correction"}`),
	})
	if err != nil || reopened.State != TimecardReopened || reopened.Revision != 5 {
		t.Fatalf("reopen: %#v, %v", reopened, err)
	}
}

// TestTodo_FTIME_004_Race proves the revision guard: a concurrent approve
// and correct racing on the same timecard revision can only have one
// winner, and the loser observes ErrRevisionConflict rather than silently
// clobbering the other's write.
func TestTodo_FTIME_004_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	timecardFixture(t, s, "tenant-a", "tc-race", "worker-1")
	if _, err := s.AppendTimecardEvent(ctx, "tenant-a", 1, "", TimecardEvent{
		TenantID: "tenant-a", TimecardID: "tc-race", Kind: EventLine, ActorID: "worker-1", IdempotencyKey: "line-1",
		Payload: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = s.AppendTimecardEvent(ctx, "tenant-a", 2, TimecardApproved, TimecardEvent{
			TenantID: "tenant-a", TimecardID: "tc-race", Kind: EventApproval, ActorID: "supervisor-1", IdempotencyKey: "approve-race",
			Payload: []byte(`{}`),
		})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = s.AppendTimecardEvent(ctx, "tenant-a", 2, "", TimecardEvent{
			TenantID: "tenant-a", TimecardID: "tc-race", Kind: EventCorrection, ActorID: "supervisor-1", IdempotencyKey: "correct-race",
			Payload: []byte(`{"reason":"late punch"}`),
		})
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
	final, err := s.GetTimecard(ctx, "tenant-a", "tc-race")
	if err != nil || final.Revision != 3 {
		t.Fatalf("final = %#v, %v", final, err)
	}
}

func TestTodo_FTIME_004_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	timecardFixture(t, s, "tenant-a", "tc-shared", "worker-1")
	if _, err := s.GetTimecard(ctx, "tenant-b", "tc-shared"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read = %v, want ErrNotFound", err)
	}
	if _, err := s.AppendTimecardEvent(ctx, "tenant-b", 1, "", TimecardEvent{
		TenantID: "tenant-b", TimecardID: "tc-shared", Kind: EventLine, ActorID: "intruder", IdempotencyKey: "k",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant append = %v, want ErrNotFound", err)
	}
}

func TestTodo_FTIME_004_Golden(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	timecardFixture(t, s, "tenant-a", "tc-golden", "worker-1")
	// Replaying the same actor+key+kind is idempotent: it returns the
	// existing state rather than appending a duplicate event.
	first, err := s.AppendTimecardEvent(ctx, "tenant-a", 1, "", TimecardEvent{
		TenantID: "tenant-a", TimecardID: "tc-golden", Kind: EventLine, ActorID: "worker-1", IdempotencyKey: "dup-key",
		Payload: []byte(`{"n":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AppendTimecardEvent(ctx, "tenant-a", 1, "", TimecardEvent{
		TenantID: "tenant-a", TimecardID: "tc-golden", Kind: EventLine, ActorID: "worker-1", IdempotencyKey: "dup-key",
		Payload: []byte(`{"n":1}`),
	})
	if err != nil || second.Revision != first.Revision {
		t.Fatalf("replay = %#v, %v, want revision %d", second, err, first.Revision)
	}
	history, err := s.TimecardHistory(ctx, "tenant-a", "tc-golden")
	if err != nil || len(history) != 1 {
		t.Fatalf("history after replay = %#v, %v, want exactly one event", history, err)
	}
}

func TestTodo_FTIME_004_InvalidInputsRejected(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.CreateTimecard(ctx, "tenant-a", Timecard{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty timecard = %v", err)
	}
	if _, err := s.AppendTimecardEvent(ctx, "tenant-a", 0, "", TimecardEvent{TenantID: "tenant-a", TimecardID: "x", ActorID: "a", IdempotencyKey: "k", Kind: EventLine}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero expected revision = %v", err)
	}
	if _, err := s.AppendTimecardEvent(ctx, "tenant-a", 1, "", TimecardEvent{TenantID: "tenant-a", TimecardID: "x", ActorID: "a", IdempotencyKey: "k", Kind: "BOGUS"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid kind = %v", err)
	}
}
