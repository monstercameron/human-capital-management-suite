package productdurability

import (
	"errors"
	"testing"
	"time"
)

func align040Chronology(t *testing.T) ([]HistoryEntry, *HistoryJournal) {
	t.Helper()
	journal := NewHistoryJournal()
	base := durabilityBase()
	for i, payload := range []string{"sha256:fact-1", "sha256:fact-2", "sha256:fact-3"} {
		if _, err := journal.Append(durabilityTenant, "worker.lifecycle", payload, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	entries, err := journal.Replay(durabilityTenant, "worker.lifecycle")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return entries, journal
}

// TestTodo_ALIGN_040 proves projection rebuild from the authoritative
// chronology: replaying the journal arrives at a fully determined
// projection, and gaps, foreign entries, or tampering break the rebuild.
func TestTodo_ALIGN_040(t *testing.T) {
	entries, _ := align040Chronology(t)
	projection, err := Rebuild(durabilityTenant, "worker.lifecycle", entries)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if projection.AppliedThrough != 3 || projection.Entries != 3 || projection.Digest == "" {
		t.Fatalf("projection = %+v", projection)
	}
	if projection.Tenant != durabilityTenant || projection.Stream != "worker.lifecycle" {
		t.Fatalf("projection identity = %+v", projection)
	}
}

func TestTodo_ALIGN_040_Property(t *testing.T) {
	entries, _ := align040Chronology(t)
	first, err := Rebuild(durabilityTenant, "worker.lifecycle", entries)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Rebuild(durabilityTenant, "worker.lifecycle", entries)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("rebuild is not deterministic: %s != %s", first.Digest, second.Digest)
	}
	// Order is content: swapping two payloads rebuilds a different
	// projection.
	swapped := append([]HistoryEntry(nil), entries...)
	swapped[0].PayloadDigest, swapped[1].PayloadDigest = swapped[1].PayloadDigest, swapped[0].PayloadDigest
	for i := range swapped {
		swapped[i].Digest = swapped[i].computeDigest()
		if i > 0 {
			swapped[i].PrevDigest = swapped[i-1].Digest
			swapped[i].Digest = swapped[i].computeDigest()
		}
	}
	other, err := Rebuild(durabilityTenant, "worker.lifecycle", swapped)
	if err != nil {
		t.Fatalf("Rebuild(swapped): %v", err)
	}
	if other.Digest == first.Digest {
		t.Fatal("reordered chronology rebuilt the same projection")
	}
}

func TestTodo_ALIGN_040_Golden(t *testing.T) {
	entries, _ := align040Chronology(t)
	projection, err := Rebuild(durabilityTenant, "worker.lifecycle", entries)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	const wantDigest = "sha256:40daec08a2e2cd99ceb0b65e67073b86c268405a2cac977160329ce6b06637c3"
	if projection.Digest != wantDigest {
		t.Fatalf("rebuilt digest=%q want=%q", projection.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_040_Security(t *testing.T) {
	entries, _ := align040Chronology(t)
	// A foreign entry smuggled into the chronology breaks the rebuild.
	foreign := append([]HistoryEntry(nil), entries...)
	foreign[1].Tenant = "vendor"
	if _, err := Rebuild(durabilityTenant, "worker.lifecycle", foreign); !errors.Is(err, ErrRebuildInvalid) {
		t.Fatalf("Rebuild(foreign) = %v, want ErrRebuildInvalid", err)
	}
	// A tampered payload breaks the rebuild even when the sequence is
	// intact.
	tampered := append([]HistoryEntry(nil), entries...)
	tampered[2].PayloadDigest = "sha256:forged"
	if _, err := Rebuild(durabilityTenant, "worker.lifecycle", tampered); !errors.Is(err, ErrRebuildInvalid) {
		t.Fatalf("Rebuild(tampered) = %v, want ErrRebuildInvalid", err)
	}
	// An empty chronology rebuilds nothing.
	if _, err := Rebuild(durabilityTenant, "worker.lifecycle", nil); !errors.Is(err, ErrRebuildInvalid) {
		t.Fatalf("Rebuild(empty) = %v, want ErrRebuildInvalid", err)
	}
}

func TestTodo_ALIGN_040_Integration(t *testing.T) {
	entries, _ := align040Chronology(t)
	// The live projector follows the same journal entry by entry; the
	// rebuilt projection must equal the live one exactly.
	live := NewProjector(durabilityTenant, "worker.lifecycle")
	for _, entry := range entries {
		if err := live.Apply(entry); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	rebuilt, err := Rebuild(durabilityTenant, "worker.lifecycle", entries)
	if err != nil {
		t.Fatal(err)
	}
	if live.State() != rebuilt {
		t.Fatalf("live %+v != rebuilt %+v", live.State(), rebuilt)
	}
}

func TestTodo_ALIGN_040_Fault(t *testing.T) {
	entries, _ := align040Chronology(t)
	// A sequence gap breaks the rebuild: the projection cannot skip a
	// fact it never saw.
	gapped := []HistoryEntry{entries[0], entries[2]}
	if _, err := Rebuild(durabilityTenant, "worker.lifecycle", gapped); !errors.Is(err, ErrRebuildGap) {
		t.Fatalf("Rebuild(gap) = %v, want ErrRebuildGap", err)
	}
	// Unordered input breaks the rebuild rather than being silently
	// reordered: chronology order is a contract input.
	reversed := []HistoryEntry{entries[2], entries[1], entries[0]}
	if _, err := Rebuild(durabilityTenant, "worker.lifecycle", reversed); !errors.Is(err, ErrRebuildGap) {
		t.Fatalf("Rebuild(reversed) = %v, want ErrRebuildGap", err)
	}
	// The live projector refuses out-of-order application the same way.
	live := NewProjector(durabilityTenant, "worker.lifecycle")
	if err := live.Apply(entries[1]); !errors.Is(err, ErrRebuildGap) {
		t.Fatalf("Apply(skip) = %v, want ErrRebuildGap", err)
	}
}

func TestTodo_ALIGN_040_Conformance(t *testing.T) {
	_, journal := align040Chronology(t)
	// Rebuild-from-replay is the restart path: whatever the journal holds
	// is exactly what a fresh process refolds, with no live state.
	replayed, err := journal.Replay(durabilityTenant, "worker.lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Rebuild(durabilityTenant, "worker.lifecycle", replayed)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Entries != len(replayed) || rebuilt.AppliedThrough != uint64(len(replayed)) {
		t.Fatalf("rebuilt = %+v for %d entries", rebuilt, len(replayed))
	}
	live := NewProjector(durabilityTenant, "worker.lifecycle")
	for _, entry := range replayed {
		if err := live.Apply(entry); err != nil {
			t.Fatal(err)
		}
	}
	if live.State().Digest != rebuilt.Digest {
		t.Fatalf("restart digest %s != live digest %s", rebuilt.Digest, live.State().Digest)
	}
}

func FuzzTodo_ALIGN_040_Fuzz(f *testing.F) {
	f.Add("sha256:seed-1", "sha256:seed-2")
	f.Fuzz(func(t *testing.T, first, second string) {
		journal := NewHistoryJournal()
		base := durabilityBase()
		for i, payload := range []string{first, second} {
			if payload == "" {
				t.Skip("empty payloads are refused by contract")
			}
			if _, err := journal.Append(durabilityTenant, "fuzz.stream", payload, base.Add(time.Duration(i)*time.Second)); err != nil {
				t.Skip("invalid fuzz payload")
			}
		}
		entries, err := journal.Replay(durabilityTenant, "fuzz.stream")
		if err != nil {
			t.Fatal(err)
		}
		one, oneErr := Rebuild(durabilityTenant, "fuzz.stream", entries)
		two, twoErr := Rebuild(durabilityTenant, "fuzz.stream", entries)
		if (oneErr == nil) != (twoErr == nil) {
			t.Fatalf("rebuild is not deterministic: %v vs %v", oneErr, twoErr)
		}
		if oneErr == nil && one.Digest != two.Digest {
			t.Fatalf("rebuild digest is not deterministic: %s != %s", one.Digest, two.Digest)
		}
	})
}
