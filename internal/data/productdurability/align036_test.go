package productdurability

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_ALIGN_036 proves optimistic concurrency on mutable coordination
// rows: writes name the revision they saw, and a stale writer loses with
// the current revision named.
func TestTodo_ALIGN_036(t *testing.T) {
	store := NewCoordinationStore()
	base := durabilityBase()
	created, err := store.Create(durabilityTenant, "payroll.run.lock", "sha256:payload-1", "principal-admin", base)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Revision != 1 {
		t.Fatalf("created revision = %d, want 1", created.Revision)
	}
	advanced, err := store.CompareAndSwap(durabilityTenant, "payroll.run.lock", 1, "sha256:payload-2", "principal-admin", base.Add(time.Second))
	if err != nil {
		t.Fatalf("CompareAndSwap: %v", err)
	}
	if advanced.Revision != 2 {
		t.Fatalf("advanced revision = %d, want 2", advanced.Revision)
	}
	// A writer still holding revision 1 loses with the current revision.
	_, err = store.CompareAndSwap(durabilityTenant, "payroll.run.lock", 1, "sha256:payload-3", "principal-late", base.Add(2*time.Second))
	var stale *StaleRevisionError
	if !errors.As(err, &stale) || stale.Current != 2 || stale.Want != 1 {
		t.Fatalf("stale write err = %v, want StaleRevisionError{want 1, current 2}", err)
	}
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale write err = %v, want errors.Is ErrStaleRevision", err)
	}
}

func TestTodo_ALIGN_036_Property(t *testing.T) {
	store := NewCoordinationStore()
	base := durabilityBase()
	row, err := store.Create(durabilityTenant, "k", "sha256:a", "writer", base)
	if err != nil {
		t.Fatal(err)
	}
	first := row.Digest()
	for i := 0; i < 3; i++ {
		row, err = store.CompareAndSwap(durabilityTenant, "k", row.Revision, "sha256:b", "writer", base.Add(time.Duration(i+1)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
	}
	if row.Revision != 4 {
		t.Fatalf("revision = %d, want 4 after create plus 3 swaps", row.Revision)
	}
	if row.Digest() == first {
		t.Fatal("digest did not change across revisions")
	}
	loaded, ok := store.Load(durabilityTenant, "k")
	if !ok || loaded != row {
		t.Fatalf("Load = %+v, %v, want the current row", loaded, ok)
	}
}

func TestTodo_ALIGN_036_Golden(t *testing.T) {
	row := CoordinationRow{
		Tenant: durabilityTenant, Key: "payroll.run.lock", Revision: 1,
		PayloadDigest: "sha256:payload-1", UpdatedAt: durabilityBase(), UpdatedBy: "principal-admin",
	}
	const wantDigest = "sha256:d2e3026eae2dac39b4bee034b072e1ae08751ca8670aecef5d70e96ffd4cad9d"
	if got := row.Digest(); got != wantDigest {
		t.Fatalf("row digest=%q want=%q", got, wantDigest)
	}
}

func TestTodo_ALIGN_036_Security(t *testing.T) {
	store := NewCoordinationStore()
	base := durabilityBase()
	if _, err := store.Create(durabilityTenant, "k", "sha256:a", "writer", base); err != nil {
		t.Fatal(err)
	}
	// Tenants hold disjoint coordination state.
	if _, ok := store.Load(values.TenantId("vendor"), "k"); ok {
		t.Fatal("foreign tenant loaded another tenant's row")
	}
	if _, err := store.CompareAndSwap(values.TenantId("vendor"), "k", 1, "sha256:b", "writer", base); err == nil {
		t.Fatal("foreign tenant swapped another tenant's row")
	}
	// Creating twice is a conflict, never an overwrite.
	if _, err := store.Create(durabilityTenant, "k", "sha256:b", "writer", base); !errors.Is(err, ErrCoordinationConflict) {
		t.Fatalf("Create(existing) = %v, want ErrCoordinationConflict", err)
	}
	// Revision zero names nothing.
	if _, err := store.CompareAndSwap(durabilityTenant, "k", 0, "sha256:b", "writer", base); !errors.Is(err, ErrCoordinationInvalid) {
		t.Fatalf("CompareAndSwap(rev 0) = %v, want ErrCoordinationInvalid", err)
	}
}

func TestTodo_ALIGN_036_Integration(t *testing.T) {
	store := NewCoordinationStore()
	base := durabilityBase()
	if _, err := store.Create(durabilityTenant, "leader.election", "sha256:term-1", "cell-a", base); err != nil {
		t.Fatal(err)
	}
	// Sixteen cells race from the same revision: exactly one wins the
	// term, the rest learn the current revision from the refusal.
	const racers = 16
	var wg sync.WaitGroup
	won := make([]bool, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.CompareAndSwap(durabilityTenant, "leader.election", 1, "sha256:term-2", "cell", base.Add(time.Second))
			won[i] = err == nil
		}(i)
	}
	wg.Wait()
	winners := 0
	for _, w := range won {
		if w {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d racers won the swap, want exactly 1", winners)
	}
	current, ok := store.Load(durabilityTenant, "leader.election")
	if !ok || current.Revision != 2 {
		t.Fatalf("current = %+v, %v, want revision 2", current, ok)
	}
}

func TestTodo_ALIGN_036_Fault(t *testing.T) {
	store := NewCoordinationStore()
	base := durabilityBase()
	// Swapping a missing row is invalid, not a creation.
	if _, err := store.CompareAndSwap(durabilityTenant, "ghost", 1, "sha256:a", "writer", base); !errors.Is(err, ErrCoordinationInvalid) {
		t.Fatalf("CompareAndSwap(missing) = %v, want ErrCoordinationInvalid", err)
	}
	// An empty payload and an empty writer are refused before any
	// revision check.
	if _, err := store.Create(durabilityTenant, "k", "sha256:a", "writer", base); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompareAndSwap(durabilityTenant, "k", 1, "", "writer", base); !errors.Is(err, ErrCoordinationInvalid) {
		t.Fatalf("CompareAndSwap(empty payload) = %v, want ErrCoordinationInvalid", err)
	}
	if _, err := store.CompareAndSwap(durabilityTenant, "k", 1, "sha256:b", "", base); !errors.Is(err, ErrCoordinationInvalid) {
		t.Fatalf("CompareAndSwap(empty writer) = %v, want ErrCoordinationInvalid", err)
	}
}

func TestTodo_ALIGN_036_Conformance(t *testing.T) {
	store := NewCoordinationStore()
	base := durabilityBase()
	row, err := store.Create(durabilityTenant, "k", "sha256:a", "writer", base)
	if err != nil {
		t.Fatal(err)
	}
	// Revisions advance by exactly 1 per write: no skips, no reuse.
	digests := map[string]bool{row.Digest(): true}
	for want := uint64(2); want <= 5; want++ {
		row, err = store.CompareAndSwap(durabilityTenant, "k", want-1, "sha256:b", "writer", base.Add(time.Duration(want)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if row.Revision != want {
			t.Fatalf("revision = %d, want %d", row.Revision, want)
		}
		if digests[row.Digest()] {
			t.Fatal("two revisions share a digest")
		}
		digests[row.Digest()] = true
	}
}

func FuzzTodo_ALIGN_036_Fuzz(f *testing.F) {
	f.Add("fuzz.key", "sha256:fuzz", "writer")
	f.Fuzz(func(t *testing.T, key, payload, writer string) {
		store := NewCoordinationStore()
		base := durabilityBase()
		created, createErr := store.Create(durabilityTenant, key, payload, writer, base)
		if createErr != nil {
			return
		}
		swapped, swapErr := store.CompareAndSwap(durabilityTenant, key, created.Revision, payload, writer, base.Add(time.Second))
		if swapErr != nil {
			t.Fatalf("swap of the just-created revision failed: %v", swapErr)
		}
		if swapped.Revision != created.Revision+1 {
			t.Fatalf("revision advanced by %d", swapped.Revision-created.Revision)
		}
	})
}
