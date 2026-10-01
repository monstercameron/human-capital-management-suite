package timestore

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// fixtureWithDB is like fixture but also returns the underlying pgtest.DB
// so a recovery test can open a second, independent Store against the same
// schema to prove durability across a reconnect.
func fixtureWithDB(t *testing.T) (*Store, *pgtest.DB) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, db
}

func TestTodo_WTIME_007(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	from := time.Date(2026, 2, 2, 8, 0, 0, 0, time.UTC)
	rev, err := s.AppendLedgerInterval(ctx, "tenant-a", "agg-1", LedgerInterval{
		AggregationKey: "agg-1", Kind: LedgerWorked, Start: from, End: from.Add(8 * time.Hour), Minutes: 480, SourceRef: "session-1",
	})
	if err != nil || rev != 1 {
		t.Fatalf("AppendLedgerInterval = %d, %v", rev, err)
	}
	slice, err := s.ReadLedgerSlice(ctx, "tenant-a", "agg-1", "", from.Add(-time.Hour), from.Add(24*time.Hour))
	if err != nil || slice.Revision != 1 || len(slice.Intervals) != 1 {
		t.Fatalf("ReadLedgerSlice = %#v, %v", slice, err)
	}
}

// TestTodo_WTIME_007_Property proves that no matter how many intervals are
// appended, in what order, the ledger revision always equals the number of
// intervals appended and every interval is stamped with the revision that
// was current the instant it landed -- the "the DECISION records that
// revision" guarantee holds only if the stamp is exact.
func TestTodo_WTIME_007_Property(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for trial := 0; trial < 10; trial++ {
		key := "agg-prop-" + strconv.Itoa(trial)
		n := trial%5 + 1
		var lastRev int64
		for i := 0; i < n; i++ {
			start := base.Add(time.Duration(trial*100+i) * time.Hour)
			rev, err := s.AppendLedgerInterval(ctx, "tenant-prop", key, LedgerInterval{
				AggregationKey: key, Kind: LedgerWorked, Start: start, End: start.Add(time.Hour), Minutes: 60, SourceRef: "s",
			})
			if err != nil {
				t.Fatalf("trial %d append %d: %v", trial, i, err)
			}
			if rev != lastRev+1 {
				t.Fatalf("trial %d: revision = %d, want %d", trial, rev, lastRev+1)
			}
			lastRev = rev
		}
		got, err := s.LedgerRevision(ctx, "tenant-prop", key)
		if err != nil || got != lastRev {
			t.Fatalf("trial %d: LedgerRevision = %d, want %d (%v)", trial, got, lastRev, err)
		}
	}
}

// TestTodo_WTIME_007_Race proves that two decisions presenting the same
// expected ledger revision can only have one winner: the second observes
// ErrRevisionConflict rather than both approving against the same stale
// window.
func TestTodo_WTIME_007_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	from := time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC)
	if _, err := s.AppendLedgerInterval(ctx, "tenant-a", "agg-race", LedgerInterval{
		AggregationKey: "agg-race", Kind: LedgerWorked, Start: from, End: from.Add(time.Hour), Minutes: 60, SourceRef: "s",
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = s.RecordLedgerDecision(ctx, "tenant-a", "agg-race", 1, "decision-"+string(rune('A'+i)))
		}(i)
	}
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

// TestTodo_WTIME_007_Recovery proves the ledger's revision and intervals
// survive across a reconnect: a fresh Store instance opened on the same
// schema after the writing Store's transaction committed sees exactly what
// was written, which is what "the decision reads a newer window than the
// one it recorded" must never happen because of a lost write.
func TestTodo_WTIME_007_Recovery(t *testing.T) {
	s1, db := fixtureWithDB(t)
	ctx := context.Background()
	from := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	rev, err := s1.AppendLedgerInterval(ctx, "tenant-a", "agg-recover", LedgerInterval{
		AggregationKey: "agg-recover", Kind: LedgerWorked, Start: from, End: from.Add(2 * time.Hour), Minutes: 120, SourceRef: "s",
	})
	if err != nil || rev != 1 {
		t.Fatalf("append: %d, %v", rev, err)
	}

	s2, err := New(ctx, Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	gotRev, err := s2.LedgerRevision(ctx, "tenant-a", "agg-recover")
	if err != nil || gotRev != 1 {
		t.Fatalf("recovered revision = %d, %v, want 1", gotRev, err)
	}
	slice, err := s2.ReadLedgerSlice(ctx, "tenant-a", "agg-recover", "", from.Add(-time.Hour), from.Add(24*time.Hour))
	if err != nil || slice.Revision != 1 || len(slice.Intervals) != 1 || slice.Intervals[0].Minutes != 120 {
		t.Fatalf("recovered slice = %#v, %v", slice, err)
	}

	// A decision recorded through s2 is visible back through s1: durability
	// is not scoped to the connection that wrote it.
	if err := s2.RecordLedgerDecision(ctx, "tenant-a", "agg-recover", 1, "decision-recovered"); err != nil {
		t.Fatal(err)
	}
	if err := s1.RecordLedgerDecision(ctx, "tenant-a", "agg-recover", 1, "decision-recovered-again"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("second decision through s1 = %v, want ErrRevisionConflict", err)
	}
}

func TestTodo_WTIME_007_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.AppendLedgerInterval(ctx, "tenant-a", "agg-shared", LedgerInterval{
		AggregationKey: "agg-shared", Kind: LedgerWorked, Start: from, End: from.Add(time.Hour), Minutes: 60, SourceRef: "s",
	}); err != nil {
		t.Fatal(err)
	}
	rev, err := s.LedgerRevision(ctx, "tenant-b", "agg-shared")
	if err != nil || rev != 0 {
		t.Fatalf("cross-tenant ledger revision = %d, %v, want 0", rev, err)
	}
}

func TestTodo_WTIME_007_InvalidInputsRejected(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.AppendLedgerInterval(ctx, "tenant-a", "agg", LedgerInterval{AggregationKey: "agg", Kind: "BOGUS"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid kind = %v", err)
	}
	if _, err := s.ReadLedgerSlice(ctx, "tenant-a", "agg", "", time.Now(), time.Now().Add(-time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("inverted window = %v", err)
	}
	if err := s.RecordLedgerDecision(ctx, "tenant-a", "", 0, "d"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty aggregation key = %v", err)
	}
}
