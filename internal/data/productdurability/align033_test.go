package productdurability

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var durabilityTenant = values.TenantId("acme")

func durabilityBase() time.Time {
	return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
}

// TestTodo_ALIGN_033 proves material history is append-oriented: entries
// chain in sequence order, the chain verifies, replay is ordered, and no
// update or delete operation exists to rewrite a fact.
func TestTodo_ALIGN_033(t *testing.T) {
	journal := NewHistoryJournal()
	base := durabilityBase()
	var appended []HistoryEntry
	for i, payload := range []string{"sha256:fact-1", "sha256:fact-2", "sha256:fact-3"} {
		entry, err := journal.Append(durabilityTenant, "worker.lifecycle", payload, base.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		appended = append(appended, entry)
	}
	for i, entry := range appended {
		if entry.Sequence != uint64(i+1) {
			t.Fatalf("entry %d has sequence %d", i, entry.Sequence)
		}
		if i > 0 && entry.PrevDigest != appended[i-1].Digest {
			t.Fatalf("entry %d does not chain its predecessor", entry.Sequence)
		}
		if entry.Digest == "" {
			t.Fatalf("entry %d has no digest", entry.Sequence)
		}
	}
	if err := journal.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	replayed, err := journal.Replay(durabilityTenant, "worker.lifecycle")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(replayed) != 3 {
		t.Fatalf("replayed %d entries, want 3", len(replayed))
	}
}

func TestTodo_ALIGN_033_Property(t *testing.T) {
	build := func() *HistoryJournal {
		journal := NewHistoryJournal()
		base := durabilityBase()
		for i, payload := range []string{"sha256:fact-1", "sha256:fact-2"} {
			if _, err := journal.Append(durabilityTenant, "worker.lifecycle", payload, base.Add(time.Duration(i)*time.Second)); err != nil {
				t.Fatalf("Append: %v", err)
			}
		}
		return journal
	}
	left, right := build(), build()
	leftReplay, _ := left.Replay(durabilityTenant, "worker.lifecycle")
	rightReplay, _ := right.Replay(durabilityTenant, "worker.lifecycle")
	for i := range leftReplay {
		if leftReplay[i].Digest != rightReplay[i].Digest {
			t.Fatalf("journal is not deterministic at %d", i)
		}
	}
}

func TestTodo_ALIGN_033_Golden(t *testing.T) {
	journal := NewHistoryJournal()
	entry, err := journal.Append(durabilityTenant, "worker.lifecycle", "sha256:fact-1", durabilityBase())
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	const wantDigest = "sha256:856e9886a9387520bde9be8484bed10546ae72afbc770d8f1e39a760173c331f"
	if entry.Digest != wantDigest {
		t.Fatalf("genesis digest=%q want=%q", entry.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_033_Security(t *testing.T) {
	journal := NewHistoryJournal()
	if _, err := journal.Append(durabilityTenant, "worker.lifecycle", "sha256:fact-1", durabilityBase()); err != nil {
		t.Fatal(err)
	}
	// Replayed entries are copies: mutating one cannot rewrite history.
	replayed, err := journal.Replay(durabilityTenant, "worker.lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	replayed[0].PayloadDigest = "sha256:forged"
	if err := journal.Verify(); err != nil {
		t.Fatalf("journal changed through a replayed copy: %v", err)
	}
	// Another tenant sees none of this tenant's stream.
	foreign, err := journal.Replay(values.TenantId("vendor"), "worker.lifecycle")
	if err != nil {
		t.Fatalf("Replay(foreign): %v", err)
	}
	if len(foreign) != 0 {
		t.Fatalf("foreign tenant replayed %d entries", len(foreign))
	}
	// History never goes backwards: a backdated append is refused.
	if _, err := journal.Append(durabilityTenant, "worker.lifecycle", "sha256:fact-0", durabilityBase().Add(-time.Hour)); err == nil {
		t.Fatal("backdated append was accepted")
	}
}

func TestTodo_ALIGN_033_Conformance(t *testing.T) {
	journal := NewHistoryJournal()
	base := durabilityBase()
	if _, err := journal.Append(durabilityTenant, "worker.lifecycle", "sha256:fact-1", base); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(durabilityTenant, "payroll.run", "sha256:run-1", base); err != nil {
		t.Fatal(err)
	}
	// Streams names streams only: no payload or digest crosses into the
	// inventory surface.
	for _, stream := range journal.Streams() {
		if len(stream) == 0 {
			t.Fatal("empty stream name")
		}
		for _, leaked := range []string{"sha256", "fact-1", "run-1"} {
			if strings.Contains(stream, leaked) {
				t.Fatalf("stream inventory leaked %q: %q", leaked, stream)
			}
		}
	}
	if len(journal.Streams()) != 2 {
		t.Fatalf("streams = %v, want 2", journal.Streams())
	}
}

func FuzzTodo_ALIGN_033_Fuzz(f *testing.F) {
	f.Add("sha256:seed", int64(0))
	f.Fuzz(func(t *testing.T, payload string, skewSeconds int64) {
		journal := NewHistoryJournal()
		at := durabilityBase().Add(time.Duration(skewSeconds%3600) * time.Second)
		first, firstErr := journal.Append(durabilityTenant, "fuzz.stream", payload, at)
		second, secondErr := journal.Append(durabilityTenant, "fuzz.stream", payload, at)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("append is not deterministic: %v vs %v", firstErr, secondErr)
		}
		if firstErr == nil && second.Sequence != first.Sequence+1 {
			t.Fatalf("sequences do not advance: %d then %d", first.Sequence, second.Sequence)
		}
	})
}
