package search

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func propagationInstant(sec int64) values.Instant {
	instant, err := values.NewInstantFromUnix(sec, 0)
	if err != nil {
		panic(err)
	}
	return instant
}

func propagationRecord(doc string, watermark int64) IndexRecord {
	return IndexRecord{
		DocID: doc, Tenant: "acme",
		SourceWatermark: propagationInstant(watermark),
		Classification:  dlp.ClassInternal,
		ACLEpoch:        7,
		ModelVersion:    "embed/v3",
	}
}

func propagationGet(doc string) GetRequest {
	return GetRequest{
		Tenant: "acme", DocID: doc,
		MinWatermark: propagationInstant(1_700_000_000),
		Clearance:    []dlp.DataClass{dlp.ClassPublic, dlp.ClassInternal},
		ACLEpoch:     7,
	}
}

// TestTodo_SEARCH_002 is the primary SEARCH-002 contract test: stale,
// restricted, deleted and quarantined sources are never retrievable, and a
// rebuild never reintroduces what the source removed.
func TestTodo_SEARCH_002(t *testing.T) {
	t.Run("active records retrieve within watermark and clearance", func(t *testing.T) {
		index := NewIndex()
		if err := index.Upsert(propagationRecord("doc-1", 1_700_000_100)); err != nil {
			t.Fatal(err)
		}
		got, err := index.Get(propagationGet("doc-1"))
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.DocID != "doc-1" || got.ModelVersion != "embed/v3" || got.ACLEpoch != 7 {
			t.Fatalf("record must carry watermark/classification/epoch/model: %+v", got)
		}
	})

	t.Run("stale index returns UNAVAILABLE_STALE", func(t *testing.T) {
		index := NewIndex()
		if err := index.Upsert(propagationRecord("doc-old", 1_600_000_000)); err != nil {
			t.Fatal(err)
		}
		_, err := index.Get(propagationGet("doc-old"))
		if !errors.Is(err, ErrIndexStale) {
			t.Fatalf("expected UNAVAILABLE_STALE, got %v", err)
		}
		if !isUnavailableStale(err) {
			t.Fatalf("stale error must be machine-readable: %v", err)
		}
	})

	t.Run("restricted deleted and quarantined sources are not retrievable", func(t *testing.T) {
		index := NewIndex()
		secret := propagationRecord("doc-secret", 1_700_000_100)
		secret.Classification = dlp.ClassMedical
		if err := index.Upsert(secret); err != nil {
			t.Fatal(err)
		}
		if _, err := index.Get(propagationGet("doc-secret")); !errors.Is(err, ErrIndexDenied) {
			t.Fatalf("over-clearance record must be denied, got %v", err)
		}
		gone := propagationRecord("doc-gone", 1_700_000_100)
		if err := index.Upsert(gone); err != nil {
			t.Fatal(err)
		}
		if err := index.ApplyTombstone(Tombstone{Tenant: "acme", DocID: "doc-gone", Watermark: propagationInstant(1_700_000_200), Reason: "source deleted"}); err != nil {
			t.Fatal(err)
		}
		if _, err := index.Get(propagationGet("doc-gone")); !errors.Is(err, ErrIndexGone) {
			t.Fatalf("tombstoned record must be gone, got %v", err)
		}
		held := propagationRecord("doc-hold", 1_700_000_100)
		held.Hold = true
		if err := index.Upsert(held); err != nil {
			t.Fatal(err)
		}
		if _, err := index.Get(propagationGet("doc-hold")); !errors.Is(err, ErrIndexHeld) {
			t.Fatalf("held record must be unavailable, got %v", err)
		}
		quarantined := propagationRecord("doc-q", 1_700_000_100)
		quarantined.Quarantined = true
		if err := index.Upsert(quarantined); err != nil {
			t.Fatal(err)
		}
		if _, err := index.Get(propagationGet("doc-q")); !errors.Is(err, ErrIndexQuarantined) {
			t.Fatalf("quarantined record must be unavailable, got %v", err)
		}
	})

	t.Run("tombstones apply idempotently and rebuilds honor them", func(t *testing.T) {
		index := NewIndex()
		if err := index.Upsert(propagationRecord("doc-1", 1_700_000_100)); err != nil {
			t.Fatal(err)
		}
		tomb := Tombstone{Tenant: "acme", DocID: "doc-1", Watermark: propagationInstant(1_700_000_200), Reason: "source deleted"}
		if err := index.ApplyTombstone(tomb); err != nil {
			t.Fatal(err)
		}
		if err := index.ApplyTombstone(tomb); err != nil {
			t.Fatalf("tombstones must apply idempotently: %v", err)
		}
		// A rebuild carrying the deleted source must not resurrect it.
		report, err := index.Rebuild([]SourceSnapshot{
			{Record: propagationRecord("doc-1", 1_700_000_100)},
			{Record: propagationRecord("doc-2", 1_700_000_100)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Resurrected != 0 {
			t.Fatalf("rebuild must never reintroduce tombstoned sources: %+v", report)
		}
		if _, err := index.Get(propagationGet("doc-1")); !errors.Is(err, ErrIndexGone) {
			t.Fatalf("tombstone must survive rebuild, got %v", err)
		}
		if _, err := index.Get(propagationGet("doc-2")); err != nil {
			t.Fatalf("live sources must rebuild: %v", err)
		}
	})

	t.Run("newer source watermarks advance, older ones do not regress", func(t *testing.T) {
		index := NewIndex()
		if err := index.Upsert(propagationRecord("doc-1", 1_700_000_100)); err != nil {
			t.Fatal(err)
		}
		if err := index.Upsert(propagationRecord("doc-1", 1_700_000_050)); err != nil {
			t.Fatal(err)
		}
		got, err := index.Get(propagationGet("doc-1"))
		if err != nil {
			t.Fatal(err)
		}
		if got.SourceWatermark != propagationInstant(1_700_000_100) {
			t.Fatalf("older watermark must not regress the index: %+v", got)
		}
	})
}

// TestTodo_SEARCH_002_Race hammers tombstones against reads: no torn state,
// no panic, and the tombstone always wins once applied.
func TestTodo_SEARCH_002_Race(t *testing.T) {
	index := NewIndex()
	if err := index.Upsert(propagationRecord("doc-race", 1_700_000_100)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = index.Get(propagationGet("doc-race"))
		}()
		go func(n int) {
			defer wg.Done()
			_ = index.ApplyTombstone(Tombstone{
				Tenant:    "acme",
				DocID:     "doc-race",
				Watermark: propagationInstant(int64(1_700_000_200 + n)),
				Reason:    "race",
			})
		}(i)
	}
	wg.Wait()
	if _, err := index.Get(propagationGet("doc-race")); !errors.Is(err, ErrIndexGone) {
		t.Fatalf("tombstone must win the race, got %v", err)
	}
}

// TestTodo_SEARCH_002_Security proves denial without existence leakage: an
// over-clearance miss is indistinguishable from a true miss, and tenant
// isolation holds across every operation.
func TestTodo_SEARCH_002_Security(t *testing.T) {
	index := NewIndex()
	secret := propagationRecord("doc-secret", 1_700_000_100)
	secret.Classification = dlp.ClassBank
	if err := index.Upsert(secret); err != nil {
		t.Fatal(err)
	}
	_, deniedErr := index.Get(propagationGet("doc-secret"))
	_, missingErr := index.Get(propagationGet("doc-missing"))
	if deniedErr == nil || missingErr == nil {
		t.Fatal("both unknown and denied docs must fail")
	}
	if !errors.Is(deniedErr, ErrIndexDenied) || !errors.Is(missingErr, ErrIndexDenied) {
		t.Fatalf("both cases must deny identically: %v vs %v", deniedErr, missingErr)
	}
	if errors.Is(deniedErr, ErrIndexGone) {
		t.Fatal("denial must not collapse into gone: existence would leak")
	}
	foreign := propagationGet("doc-secret")
	foreign.Tenant = "competitor"
	if _, err := index.Get(foreign); err == nil {
		t.Fatal("cross-tenant reads must fail")
	}
	foreignUpsert := propagationRecord("doc-secret", 1_700_000_300)
	foreignUpsert.Tenant = "competitor"
	if err := index.Upsert(foreignUpsert); err == nil {
		t.Fatal("cross-tenant upserts must fail")
	}
}

// TestTodo_SEARCH_002_Recovery proves the index is a rebuildable copy: a
// fresh index rebuilt from the source journal converges, tombstones included.
func TestTodo_SEARCH_002_Recovery(t *testing.T) {
	primary := NewIndex()
	if err := primary.Upsert(propagationRecord("doc-1", 1_700_000_100)); err != nil {
		t.Fatal(err)
	}
	if err := primary.Upsert(propagationRecord("doc-2", 1_700_000_100)); err != nil {
		t.Fatal(err)
	}
	if err := primary.ApplyTombstone(Tombstone{Tenant: "acme", DocID: "doc-2", Watermark: propagationInstant(1_700_000_200), Reason: "deleted"}); err != nil {
		t.Fatal(err)
	}
	journal := primary.Journal()
	replica := NewIndex()
	report, err := replica.Rebuild(journal)
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied != 1 || report.SkippedTombstoned != 1 {
		t.Fatalf("rebuild must converge with tombstones: %+v", report)
	}
	if _, err := replica.Get(propagationGet("doc-1")); err != nil {
		t.Fatalf("live record must survive failover: %v", err)
	}
	if _, err := replica.Get(propagationGet("doc-2")); !errors.Is(err, ErrIndexGone) {
		t.Fatalf("tombstone must survive failover, got %v", err)
	}
}
