package backfill

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// recordingComputer computes fixed-size batches over a virtual record range.
// Every served range is logged so resume tests can prove no range recomputes.
type recordingComputer struct {
	total  int
	ranges [][2]int
	failOn int // batch index that fails, -1 for none
	calls  int
}

func (c *recordingComputer) ComputeBatch(_ context.Context, _ ElementRef, cursor string, limit int) (Batch, error) {
	batch := c.calls
	c.calls++
	if batch == c.failOn {
		return Batch{}, fmt.Errorf("computer: injected failure at batch %d", batch)
	}
	offset := 0
	if rest, ok := strings.CutPrefix(cursor, "offset:"); ok {
		n, err := strconv.Atoi(rest)
		if err != nil {
			return Batch{}, fmt.Errorf("computer: bad cursor %q", cursor)
		}
		offset = n
	} else if cursor != "" {
		return Batch{}, fmt.Errorf("computer: bad cursor %q", cursor)
	}
	if offset > c.total {
		return Batch{}, fmt.Errorf("computer: cursor %d past total %d", offset, c.total)
	}
	end := offset + limit
	if end > c.total {
		end = c.total
	}
	c.ranges = append(c.ranges, [2]int{offset, end})
	return Batch{NextCursor: fmt.Sprintf("offset:%d", end), Records: end - offset, Done: end == c.total}, nil
}

func testElement() ElementRef {
	return ElementRef{Tenant: "acme", Kind: KindDerivedField, Namespace: "worker", Name: "tenure_band", Version: 1}
}

// TestTodo_WF_DATA_028 proves the PRIMARY matrix of WF-DATA-028: publishing
// a derived element starts a resumable, throttled backfill; the element is
// BACKFILLING until complete and then READY; consumers refuse anything that
// is not READY.
func TestTodo_WF_DATA_028(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []ElementKind{KindDerivedField, KindRollup, KindResolution, KindSearchableField} {
		element := testElement()
		element.Kind = kind
		if err := element.Validate(); err != nil {
			t.Fatalf("element %s: %v", kind, err)
		}
	}
	if err := (ElementRef{}).Validate(); !errors.Is(err, ErrInvalidElement) {
		t.Fatalf("empty element = %v, want ErrInvalidElement", err)
	}

	store := NewMemoryStore()
	element := testElement()
	job, err := store.Start(ctx, element, 25, 100)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if job.State != StateBackfilling || job.Cursor != "offset:0" {
		t.Fatalf("published job = %+v, want BACKFILLING at offset:0", job)
	}
	if err := RequireReady(ctx, store, element); !errors.Is(err, ErrElementNotReady) {
		t.Fatalf("BACKFILLING element admitted: %v", err)
	}
	if err := RequireReady(ctx, store, ElementRef{Tenant: "acme", Kind: KindRollup, Namespace: "w", Name: "x", Version: 1}); !errors.Is(err, ErrNoBackfill) {
		t.Fatalf("unpublished element = %v, want ErrNoBackfill", err)
	}

	computer := &recordingComputer{total: 100, failOn: -1}
	// Throttled: two batches of 25 per run even though 100 records await.
	job, err = Run(ctx, store, computer, element, BatchBudget{MaxBatches: 2, MaxRecords: 1000})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if job.State != StateBackfilling || job.Cursor != "offset:50" || len(computer.ranges) != 2 {
		t.Fatalf("throttled job = %+v ranges=%v", job, computer.ranges)
	}
	job, err = Run(ctx, store, computer, element, BatchBudget{MaxBatches: 10, MaxRecords: 1000})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if job.State != StateReady || job.Cursor != "offset:100" {
		t.Fatalf("completed job = %+v, want READY at offset:100", job)
	}
	if err := RequireReady(ctx, store, element); err != nil {
		t.Fatalf("READY element refused: %v", err)
	}
	// READY is terminal: further runs are no-ops that recompute nothing.
	before := len(computer.ranges)
	job, err = Run(ctx, store, computer, element, BatchBudget{MaxBatches: 10, MaxRecords: 1000})
	if err != nil || job.State != StateReady || len(computer.ranges) != before {
		t.Fatalf("READY rerun = %+v ranges=%d, want no-op", job, len(computer.ranges))
	}
}

// TestTodo_WF_DATA_028_Recovery proves a republished element keeps its
// cursor and a fresh driver resumes from the store rather than restarting.
func TestTodo_WF_DATA_028_Recovery(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	element := testElement()
	if _, err := store.Start(ctx, element, 10, 30); err != nil {
		t.Fatal(err)
	}
	computer := &recordingComputer{total: 30, failOn: -1}
	if _, err := Run(ctx, store, computer, element, BatchBudget{MaxBatches: 1, MaxRecords: 1000}); err != nil {
		t.Fatal(err)
	}
	// Republishing mid-backfill is idempotent: the cursor survives.
	job, err := store.Start(ctx, element, 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if job.Cursor != "offset:10" || job.State != StateBackfilling {
		t.Fatalf("republished job = %+v, want resume at offset:10", job)
	}
	// A fresh driver (new computer, same store) continues without recompute.
	fresh := &recordingComputer{total: 30, failOn: -1}
	job, err = Run(ctx, store, fresh, element, BatchBudget{MaxBatches: 10, MaxRecords: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if job.State != StateReady {
		t.Fatalf("resumed job = %+v, want READY", job)
	}
	for _, r := range fresh.ranges {
		if r[0] < 10 {
			t.Fatalf("fresh driver recomputed range %v", r)
		}
	}
}

// TestTodo_WF_DATA_028_Fault proves an interrupted backfill resumes rather
// than restarting: a computer failure leaves the cursor at the last commit
// and the next run continues without recomputing completed ranges.
func TestTodo_WF_DATA_028_Fault(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	element := testElement()
	if _, err := store.Start(ctx, element, 10, 30); err != nil {
		t.Fatal(err)
	}
	computer := &recordingComputer{total: 30, failOn: 1}
	if _, err := Run(ctx, store, computer, element, BatchBudget{MaxBatches: 10, MaxRecords: 1000}); err == nil {
		t.Fatal("failing run returned no error")
	}
	job, err := store.Load(ctx, element)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != StateBackfilling || job.Cursor != "offset:10" {
		t.Fatalf("interrupted job = %+v, want BACKFILLING at offset:10", job)
	}
	computer.failOn = -1
	rangesBefore := len(computer.ranges)
	job, err = Run(ctx, store, computer, element, BatchBudget{MaxBatches: 10, MaxRecords: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if job.State != StateReady {
		t.Fatalf("resumed job = %+v, want READY", job)
	}
	for _, r := range computer.ranges[rangesBefore:] {
		if r[0] < 10 {
			t.Fatalf("resumed run recomputed range %v", r)
		}
	}
}
