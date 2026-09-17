// CLOCK-004 RED: an offline device log must preserve local sequence and the
// signed occurred time across power loss, reorder, replay, clock drift and
// tamper; sync must mark confidence/skew without rewriting receipt order.
package clock

import (
	"errors"
	"testing"
	"time"
)

func offlineEntries(device string, base time.Time, n int) []OfflineEntry {
	entries := make([]OfflineEntry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, OfflineEntry{
			Sequence:      uint64(i + 1),
			EventType:     EventClockIn,
			DeviceRef:     device,
			WorkerRef:     "worker-1",
			OccurredAt:    base.Add(time.Duration(i) * time.Minute),
			PayloadDigest: "sha256:offline-payload-" + device + "-" + string(rune('0'+i)),
		})
	}
	return entries
}

func offlineBase(t *testing.T) time.Time {
	t.Helper()
	base, err := time.Parse(time.RFC3339, captureTestTime)
	if err != nil {
		t.Fatal(err)
	}
	return base.Add(-2 * time.Hour)
}

// TestTodo_CLOCK_004 is the PRIMARY contract: buffering preserves local
// sequence and signed occurred time, and sync marks confidence/skew while
// never rewriting receipt order.
func TestTodo_CLOCK_004(t *testing.T) {
	base := offlineBase(t)

	t.Run("buffer preserves local sequence and signed occurred time", func(t *testing.T) {
		entries := offlineEntries("device-1", base, 3)
		buf, err := BufferOffline("device-1", entries)
		if err != nil {
			t.Fatalf("BufferOffline: %v", err)
		}
		if buf.DeviceRef != "device-1" || len(buf.Entries) != len(entries) {
			t.Fatalf("buffer=%+v want 3 entries for device-1", buf)
		}
		for i, got := range buf.Entries {
			want := entries[i]
			if got.Sequence != want.Sequence || !got.OccurredAt.Equal(want.OccurredAt) || got.PayloadDigest != want.PayloadDigest {
				t.Fatalf("entry %d=%+v want %+v", i, got, want)
			}
		}
	})

	t.Run("reordered log is rejected", func(t *testing.T) {
		entries := offlineEntries("device-1", base, 3)
		entries[0], entries[2] = entries[2], entries[0]
		_, err := BufferOffline("device-1", entries)
		var rej *OfflineRejection
		if !errors.As(err, &rej) || !errors.Is(err, ErrOfflineRejected) {
			t.Fatalf("reorder err=%v, want CLOCK_004_REJECTED", err)
		}
		if rej.Field != "entries.sequence" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
	})

	t.Run("replayed sequence is rejected", func(t *testing.T) {
		entries := offlineEntries("device-1", base, 3)
		entries[2].Sequence = 2
		if _, err := BufferOffline("device-1", entries); !errors.Is(err, ErrOfflineRejected) {
			t.Fatalf("replay err=%v, want CLOCK_004_REJECTED", err)
		}
	})

	t.Run("sequence gap is rejected", func(t *testing.T) {
		entries := offlineEntries("device-1", base, 3)
		entries = entries[:2]
		entries[1].Sequence = 3
		if _, err := BufferOffline("device-1", entries); !errors.Is(err, ErrOfflineRejected) {
			t.Fatalf("gap err=%v, want CLOCK_004_REJECTED", err)
		}
	})

	t.Run("tampered entry is rejected", func(t *testing.T) {
		entries := offlineEntries("device-1", base, 2)
		entries[1].PayloadDigest = ""
		if _, err := BufferOffline("device-1", entries); !errors.Is(err, ErrOfflineRejected) {
			t.Fatalf("tamper err=%v, want CLOCK_004_REJECTED", err)
		}
		entries = offlineEntries("device-1", base, 2)
		entries[1].OccurredAt = time.Time{}
		if _, err := BufferOffline("device-1", entries); !errors.Is(err, ErrOfflineRejected) {
			t.Fatalf("zero occurred err=%v, want CLOCK_004_REJECTED", err)
		}
	})

	t.Run("sync marks confidence and skew without rewriting receipt order", func(t *testing.T) {
		entries := offlineEntries("device-1", base, 3)
		buf, err := BufferOffline("device-1", entries)
		if err != nil {
			t.Fatal(err)
		}
		now, _ := time.Parse(time.RFC3339, captureTestTime)
		sync, err := SyncOffline(buf, now)
		if err != nil {
			t.Fatalf("SyncOffline: %v", err)
		}
		if len(sync.Entries) != 3 || sync.ReceiptDigest == "" {
			t.Fatalf("sync=%+v", sync)
		}
		for i, got := range sync.Entries {
			if got.ReceiptOrder != uint64(i+1) || got.Sequence != uint64(i+1) {
				t.Fatalf("entry %d receipt=%d seq=%d: receipt order rewrote local sequence", i, got.ReceiptOrder, got.Sequence)
			}
			if !got.OccurredAt.Equal(entries[i].OccurredAt) {
				t.Fatalf("entry %d occurred=%v want %v", i, got.OccurredAt, entries[i].OccurredAt)
			}
			if want := now.Sub(entries[i].OccurredAt); got.Skew != want {
				t.Fatalf("entry %d skew=%v want %v", i, got.Skew, want)
			}
			if got.Confidence != SyncConfidenceHigh {
				t.Fatalf("entry %d confidence=%q want HIGH", i, got.Confidence)
			}
		}
	})

	t.Run("clock drift marks degraded confidence in order", func(t *testing.T) {
		now, _ := time.Parse(time.RFC3339, captureTestTime)
		entries := offlineEntries("device-1", now.Add(time.Hour), 2)
		buf, err := BufferOffline("device-1", entries)
		if err != nil {
			t.Fatal(err)
		}
		sync, err := SyncOffline(buf, now)
		if err != nil {
			t.Fatalf("SyncOffline: %v", err)
		}
		for i, got := range sync.Entries {
			if got.Confidence != SyncConfidenceDegraded {
				t.Fatalf("entry %d confidence=%q want DEGRADED", i, got.Confidence)
			}
			if got.ReceiptOrder != uint64(i+1) {
				t.Fatalf("entry %d receipt order rewritten: %d", i, got.ReceiptOrder)
			}
		}
	})
}

// TestTodo_CLOCK_004_Property holds the sync invariants: receipt order always
// equals local sequence order, the receipt digest is stable per input and
// sensitive to the log, and skew tracks the injected server clock.
func TestTodo_CLOCK_004_Property(t *testing.T) {
	base := offlineBase(t)
	now, _ := time.Parse(time.RFC3339, captureTestTime)

	for _, n := range []int{1, 5, 25} {
		buf, err := BufferOffline("device-1", offlineEntries("device-1", base, n))
		if err != nil {
			t.Fatal(err)
		}
		first, err := SyncOffline(buf, now)
		if err != nil {
			t.Fatal(err)
		}
		second, err := SyncOffline(buf, now)
		if err != nil {
			t.Fatal(err)
		}
		if first.ReceiptDigest != second.ReceiptDigest {
			t.Fatalf("n=%d: sync digest unstable", n)
		}
		for i, got := range first.Entries {
			if got.ReceiptOrder != got.Sequence || got.Sequence != uint64(i+1) {
				t.Fatalf("n=%d entry %d: receipt order diverged from local sequence", n, i)
			}
		}
		moved, err := BufferOffline("device-1", offlineEntries("device-1", base.Add(time.Second), n))
		if err != nil {
			t.Fatal(err)
		}
		other, err := SyncOffline(moved, now)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 && other.ReceiptDigest == first.ReceiptDigest {
			t.Fatalf("n=%d: different occurred times must produce different receipt digests", n)
		}
	}

	t.Run("skew tracks the injected server clock", func(t *testing.T) {
		buf, err := BufferOffline("device-1", offlineEntries("device-1", base, 2))
		if err != nil {
			t.Fatal(err)
		}
		for _, skew := range []time.Duration{0, time.Hour, 24 * time.Hour} {
			server := now.Add(skew)
			sync, err := SyncOffline(buf, server)
			if err != nil {
				t.Fatalf("skew %v: %v", skew, err)
			}
			for i, got := range sync.Entries {
				if want := server.Sub(buf.Entries[i].OccurredAt); got.Skew != want {
					t.Fatalf("skew %v entry %d: got %v want %v", skew, i, got.Skew, want)
				}
			}
		}
	})
}

// TestTodo_CLOCK_004_Recovery proves the outage contract: a power loss loses
// nothing the device had buffered — re-buffering the same local log yields
// the identical buffer — and a post-outage sync preserves sequence and signed
// occurred time with honest skew.
func TestTodo_CLOCK_004_Recovery(t *testing.T) {
	base := offlineBase(t)
	now, _ := time.Parse(time.RFC3339, captureTestTime)
	entries := offlineEntries("device-1", base, 4)

	before, err := BufferOffline("device-1", entries)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate power loss: only the raw local log survives; re-buffer it.
	after, err := BufferOffline("device-1", entries)
	if err != nil {
		t.Fatalf("post-outage re-buffer: %v", err)
	}
	if len(after.Entries) != len(before.Entries) {
		t.Fatalf("outage lost entries: before=%d after=%d", len(before.Entries), len(after.Entries))
	}
	for i := range before.Entries {
		if after.Entries[i] != before.Entries[i] {
			t.Fatalf("entry %d changed across outage: %+v vs %+v", i, after.Entries[i], before.Entries[i])
		}
	}

	sync, err := SyncOffline(after, now)
	if err != nil {
		t.Fatalf("post-outage sync: %v", err)
	}
	for i, got := range sync.Entries {
		if got.Sequence != uint64(i+1) || got.ReceiptOrder != uint64(i+1) {
			t.Fatalf("entry %d order lost after outage", i)
		}
		if !got.OccurredAt.Equal(entries[i].OccurredAt) {
			t.Fatalf("entry %d occurred time changed after outage", i)
		}
	}
}
