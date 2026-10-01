package worktimerules

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %s: %v", s, err)
	}
	return tm
}

func baseLedger(t *testing.T) Ledger {
	t.Helper()
	// Worker w1 worked 8h/day Mon-Fri, tenant t1. Also seed worker w2 with a
	// much larger set of entries to prove scope isolation.
	var entries []LedgerEntry
	day := mustTime(t, "2026-09-21T09:00:00Z") // Monday
	for i := 0; i < 5; i++ {
		start := day.AddDate(0, 0, i)
		entries = append(entries, LedgerEntry{
			ID: "w1-work-" + string(rune('a'+i)), TenantID: "t1", WorkerID: "w1",
			Kind: KindWork, Start: start, End: start.Add(8 * time.Hour), Source: "punch",
		})
	}
	entries = append(entries, LedgerEntry{
		ID: "w1-rest-1", TenantID: "t1", WorkerID: "w1", Kind: KindRest,
		Start: mustTime(t, "2026-09-25T17:00:00Z"), End: mustTime(t, "2026-09-26T09:00:00Z"), Source: "schedule",
	})
	// Worker w2 (different tenant scope entirely) has enormous hours that
	// must never leak into w1's totals.
	entries = append(entries, LedgerEntry{
		ID: "w2-work-1", TenantID: "t2", WorkerID: "w2", Kind: KindWork,
		Start: mustTime(t, "2026-09-21T00:00:00Z"), End: mustTime(t, "2026-09-27T00:00:00Z"), Source: "punch",
	})
	return Ledger{Entries: entries}
}

func TestTodo_WTIME_007(t *testing.T) {
	ledger := baseLedger(t)
	asOf := mustTime(t, "2026-09-25T18:00:00Z") // Friday 6pm, after the 5th 8h shift
	w, err := ComputeWindows(ledger, WindowOptions{
		TenantID: "t1", WorkerID: "w1", AsOf: asOf, ReferencePeriod: 14 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ComputeWindows: %v", err)
	}
	if w.DayMinutes != 8*60 { // trailing 24h from Fri 18:00 fully covers Friday's 09:00-17:00 shift
		t.Fatalf("day minutes = %d, want %d", w.DayMinutes, 8*60)
	}
	if w.WeekMinutes != 5*8*60 {
		t.Fatalf("week minutes = %d, want %d", w.WeekMinutes, 5*8*60)
	}
	if w.ConsecutiveDaysWorked != 5 {
		t.Fatalf("consecutive days worked = %d, want 5", w.ConsecutiveDaysWorked)
	}
	if w.Revision == "" {
		t.Fatal("revision must not be empty")
	}
	// Worker w2's enormous entries must never appear in w1's window even
	// though they share a ledger.
	if w.ReferencePeriodMinutes > 14*24*60 {
		t.Fatalf("reference period minutes %d exceed the physically possible ceiling; w2 leaked in", w.ReferencePeriodMinutes)
	}

	// Same-scope, different worker: verify isolation explicitly.
	w2, err := ComputeWindows(ledger, WindowOptions{TenantID: "t2", WorkerID: "w2", AsOf: asOf})
	if err != nil {
		t.Fatalf("ComputeWindows w2: %v", err)
	}
	if w2.WeekMinutes == w.WeekMinutes {
		t.Fatalf("w2 week minutes unexpectedly equal to w1's: isolation likely broken")
	}
}

func TestTodo_WTIME_007_Property(t *testing.T) {
	// Property: for any set of non-overlapping work intervals within a
	// worker's scope, the day window never exceeds 1440 minutes and the
	// week window never exceeds 7*1440, regardless of how many entries or
	// how they are split.
	asOf := mustTime(t, "2026-09-25T23:59:00Z")
	for trial := 1; trial <= 25; trial++ {
		var entries []LedgerEntry
		cursor := asOf.Add(-24 * time.Hour)
		for i := 0; i < trial; i++ {
			dur := time.Duration(trial%37+1) * time.Minute
			if cursor.Add(dur).After(asOf) {
				break
			}
			entries = append(entries, LedgerEntry{
				ID: "e" + string(rune('a'+(i%26))), TenantID: "t1", WorkerID: "w1",
				Kind: KindWork, Start: cursor, End: cursor.Add(dur), Source: "punch",
			})
			cursor = cursor.Add(dur + time.Minute)
		}
		w, err := ComputeWindows(Ledger{Entries: entries}, WindowOptions{TenantID: "t1", WorkerID: "w1", AsOf: asOf})
		if err != nil {
			t.Fatalf("trial %d: ComputeWindows: %v", trial, err)
		}
		if w.DayMinutes < 0 || w.DayMinutes > 24*60 {
			t.Fatalf("trial %d: day minutes %d out of bounds [0,1440]", trial, w.DayMinutes)
		}
		if w.WeekMinutes < w.DayMinutes {
			t.Fatalf("trial %d: week minutes %d less than day minutes %d", trial, w.WeekMinutes, w.DayMinutes)
		}
	}
}

func TestTodo_WTIME_007_Race(t *testing.T) {
	// Race: many goroutines concurrently compute windows and check the
	// expected revision against the same immutable ledger snapshot. Two
	// "sessions" each observing the same 46-hour week must both pass a
	// check against the revision they read; a session using a stale
	// revision (from before a correction) must fail CheckRevision.
	ledger := baseLedger(t)
	asOf := mustTime(t, "2026-09-25T18:00:00Z")
	rev, err := ledger.Revision("t1", "w1")
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := ComputeWindows(ledger, WindowOptions{TenantID: "t1", WorkerID: "w1", AsOf: asOf})
			if err != nil {
				errs <- err
				return
			}
			if w.Revision != rev {
				errs <- fmt.Errorf("goroutine saw revision %s, want %s", w.Revision, rev)
				return
			}
			if err := CheckRevision(ledger, "t1", "w1", w.Revision); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// A session holding the pre-correction revision must fail once the
	// ledger (a fresh copy, simulating a correction) changes.
	corrected := ledger
	corrected.Entries = append(append([]LedgerEntry{}, ledger.Entries...), LedgerEntry{
		ID: "w1-late-add", TenantID: "t1", WorkerID: "w1", Kind: KindWork,
		Start: asOf.Add(-2 * time.Hour), End: asOf.Add(-time.Hour), Source: "correction",
	})
	if err := CheckRevision(corrected, "t1", "w1", rev); err == nil {
		t.Fatal("expected ErrRevisionMismatch after a correction, got nil")
	}
}

func TestTodo_WTIME_007_Recovery(t *testing.T) {
	// Recovery: recomputing from the same ledger after a correction yields
	// the corrected window and a new revision, and the caller can detect
	// the old revision is stale before recording anything against it.
	ledger := baseLedger(t)
	asOf := mustTime(t, "2026-09-25T18:00:00Z")
	before, err := ComputeWindows(ledger, WindowOptions{TenantID: "t1", WorkerID: "w1", AsOf: asOf})
	if err != nil {
		t.Fatalf("before: %v", err)
	}

	// A correction: the Wednesday punch was missing 4 extra hours.
	corrected := ledger
	fixedEntries := append([]LedgerEntry{}, ledger.Entries...)
	for i, e := range fixedEntries {
		if e.ID == "w1-work-c" { // Wednesday
			fixedEntries[i].End = e.End.Add(4 * time.Hour)
		}
	}
	corrected.Entries = fixedEntries

	after, err := ComputeWindows(corrected, WindowOptions{TenantID: "t1", WorkerID: "w1", AsOf: asOf})
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	if after.Revision == before.Revision {
		t.Fatal("expected a new revision after correction")
	}
	if after.WeekMinutes != before.WeekMinutes+4*60 {
		t.Fatalf("week minutes after correction = %d, want %d", after.WeekMinutes, before.WeekMinutes+4*60)
	}
	if err := CheckRevision(corrected, "t1", "w1", before.Revision); err == nil {
		t.Fatal("expected the pre-correction revision to be stale against the corrected ledger")
	}
	if err := CheckRevision(corrected, "t1", "w1", after.Revision); err != nil {
		t.Fatalf("expected the fresh revision to check out: %v", err)
	}
}

func TestWeeksWithHirer(t *testing.T) {
	var entries []LedgerEntry
	start := mustTime(t, "2026-01-05T09:00:00Z") // Monday
	for i := 0; i < 13; i++ {
		s := start.AddDate(0, 0, i*7)
		entries = append(entries, LedgerEntry{
			ID: "wk" + string(rune('a'+i)), TenantID: "t1", WorkerID: "w1", HirerID: "hirer-1",
			Kind: KindWork, Start: s, End: s.Add(8 * time.Hour), Source: "punch",
		})
	}
	ledger := Ledger{Entries: entries}
	asOf := start.AddDate(0, 0, 13*7)
	weeks, parity, err := AWRWeeksWithHirer(ledger, "t1", "w1", "hirer-1", asOf, 0)
	if err != nil {
		t.Fatalf("AWRWeeksWithHirer: %v", err)
	}
	if weeks < 12 || !parity {
		t.Fatalf("weeks=%d parity=%v, want weeks>=12 and parity=true", weeks, parity)
	}

	// A break longer than tolerance resets the count.
	var brokenEntries []LedgerEntry
	brokenEntries = append(brokenEntries, entries[:3]...)
	resumeStart := start.AddDate(0, 0, 3*7+90) // long gap
	brokenEntries = append(brokenEntries, LedgerEntry{
		ID: "wk-resume", TenantID: "t1", WorkerID: "w1", HirerID: "hirer-1",
		Kind: KindWork, Start: resumeStart, End: resumeStart.Add(8 * time.Hour), Source: "punch",
	})
	brokenLedger := Ledger{Entries: brokenEntries}
	weeksBroken, parityBroken, err := AWRWeeksWithHirer(brokenLedger, "t1", "w1", "hirer-1", resumeStart.Add(24*time.Hour), 0)
	if err != nil {
		t.Fatalf("AWRWeeksWithHirer broken: %v", err)
	}
	if weeksBroken != 1 || parityBroken {
		t.Fatalf("weeks=%d parity=%v after a long break, want weeks=1 parity=false", weeksBroken, parityBroken)
	}
}
