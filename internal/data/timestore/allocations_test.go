package timestore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestTodo_FTIME_005(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	a, err := s.Allocate(ctx, "tenant-a", WorkOrderAllocation{
		TenantID: "tenant-a", ID: uuid.NewString(), TimecardID: "tc-1", TimecardRevision: 1,
		AllocationKey: "line-1", WorkOrderRef: "wo-1", ProjectRef: "proj-1", Minutes: 480,
		SourceRef: "tc-1:line-1", PayloadDigest: "digest-1", Payload: []byte(`{"minutes":480}`),
	})
	if err != nil || a.Minutes != 480 {
		t.Fatalf("Allocate = %#v, %v", a, err)
	}
}

// TestTodo_FTIME_005_Integration proves the idempotent-per-revision
// contract end to end: a replay with the same digest returns the original
// row, a changed digest under the same key conflicts, and a correction's
// new timecard revision allocates as an independent delta rather than
// overwriting the prior revision's rows.
func TestTodo_FTIME_005_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	first, err := s.Allocate(ctx, "tenant-a", WorkOrderAllocation{
		TenantID: "tenant-a", ID: uuid.NewString(), TimecardID: "tc-int", TimecardRevision: 1,
		AllocationKey: "line-1", WorkOrderRef: "wo-int", ProjectRef: "proj-1", Minutes: 480,
		SourceRef: "tc-int:line-1", PayloadDigest: "digest-1", Payload: []byte(`{"minutes":480}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Allocate(ctx, "tenant-a", WorkOrderAllocation{
		TenantID: "tenant-a", ID: uuid.NewString(), TimecardID: "tc-int", TimecardRevision: 1,
		AllocationKey: "line-1", WorkOrderRef: "wo-int", ProjectRef: "proj-1", Minutes: 480,
		SourceRef: "tc-int:line-1", PayloadDigest: "digest-1", Payload: []byte(`{"minutes":480}`),
	})
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay = %#v, %v, want the original row %q", replay, err, first.ID)
	}
	if _, err := s.Allocate(ctx, "tenant-a", WorkOrderAllocation{
		TenantID: "tenant-a", ID: uuid.NewString(), TimecardID: "tc-int", TimecardRevision: 1,
		AllocationKey: "line-1", WorkOrderRef: "wo-int", ProjectRef: "proj-1", Minutes: 999,
		SourceRef: "tc-int:line-1", PayloadDigest: "digest-2", Payload: []byte(`{"minutes":999}`),
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed digest same key = %v, want ErrIdempotencyConflict", err)
	}
	// The corrected timecard's new revision allocates a delta as an
	// independent row set; revision 1's allocation is untouched.
	if _, err := s.Allocate(ctx, "tenant-a", WorkOrderAllocation{
		TenantID: "tenant-a", ID: uuid.NewString(), TimecardID: "tc-int", TimecardRevision: 2,
		AllocationKey: "line-1", WorkOrderRef: "wo-int", ProjectRef: "proj-1", Minutes: 510,
		SourceRef: "tc-int:line-1", PayloadDigest: "digest-r2", Payload: []byte(`{"minutes":510}`),
	}); err != nil {
		t.Fatal(err)
	}
	rev1, err := s.AllocationsForTimecardRevision(ctx, "tenant-a", "tc-int", 1)
	if err != nil || len(rev1) != 1 || rev1[0].Minutes != 480 {
		t.Fatalf("revision 1 allocations = %#v, %v", rev1, err)
	}
	rev2, err := s.AllocationsForTimecardRevision(ctx, "tenant-a", "tc-int", 2)
	if err != nil || len(rev2) != 1 || rev2[0].Minutes != 510 {
		t.Fatalf("revision 2 allocations = %#v, %v", rev2, err)
	}
	byOrder, err := s.AllocationsForWorkOrder(ctx, "tenant-a", "wo-int")
	if err != nil || len(byOrder) != 2 {
		t.Fatalf("AllocationsForWorkOrder = %#v, %v", byOrder, err)
	}
}

func TestTodo_FTIME_005_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.Allocate(ctx, "tenant-a", WorkOrderAllocation{
		TenantID: "tenant-a", ID: uuid.NewString(), TimecardID: "tc-shared", TimecardRevision: 1,
		AllocationKey: "line-1", WorkOrderRef: "wo-shared", ProjectRef: "proj-1", Minutes: 60,
		SourceRef: "src", PayloadDigest: "d1", Payload: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	other, err := s.AllocationsForWorkOrder(ctx, "tenant-b", "wo-shared")
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-tenant allocations visible: %#v, %v", other, err)
	}
}

// TestTodo_FTIME_005_Race proves that concurrent allocation attempts with
// the same key and digest never charge one approved minute twice: exactly
// one row is ever created for the key, no matter how many callers race.
func TestTodo_FTIME_005_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	const n = 5
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	results := make([]WorkOrderAllocation, n)
	for i := 0; i < n; i++ {
		ids[i] = uuid.NewString()
	}
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.Allocate(ctx, "tenant-a", WorkOrderAllocation{
				TenantID: "tenant-a", ID: ids[i], TimecardID: "tc-race", TimecardRevision: 1,
				AllocationKey: "line-1", WorkOrderRef: "wo-race", ProjectRef: "proj-1", Minutes: 120,
				SourceRef: "src", PayloadDigest: "same-digest", Payload: []byte(`{"minutes":120}`),
			})
		}(i)
	}
	wg.Wait()
	firstID := ""
	for i, err := range errs {
		if err != nil {
			t.Fatalf("race allocate %d failed: %v", i, err)
		}
		if firstID == "" {
			firstID = results[i].ID
		} else if results[i].ID != firstID {
			t.Fatalf("race allocate produced two different rows: %q and %q", firstID, results[i].ID)
		}
	}
	rows, err := s.AllocationsForTimecardRevision(ctx, "tenant-a", "tc-race", 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows after race = %#v, %v, want exactly one", rows, err)
	}
}

func TestTodo_FTIME_005_InvalidInputsRejected(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.Allocate(ctx, "tenant-a", WorkOrderAllocation{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty allocation = %v", err)
	}
	if _, err := s.AllocationsForWorkOrder(ctx, "tenant-a", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty work order ref = %v", err)
	}
}
