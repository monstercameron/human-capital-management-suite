package conformance_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

func align062Invalidation(t *testing.T, sequence uint64) conformance.Invalidation {
	t.Helper()
	message, err := conformance.AuthorizeInvalidation(
		executeScope(t), queryInstant, "worker-projection.v7", sequence,
		[]values.EntityRef{subject(managedID)},
	)
	if err != nil {
		t.Fatalf("AuthorizeInvalidation(%d): %v", sequence, err)
	}
	return message
}

// TestTodo_ALIGN_062 proves duplicate, stale, and concurrent action
// safety: duplicate queries share one key, duplicate or stale
// invalidations require no refetch, and a newer invalidation does.
func TestTodo_ALIGN_062(t *testing.T) {
	envelope := execute(t, false)
	// The envelope trails the source: a newer invalidation requires a
	// refetch, the current one does not.
	required, err := conformance.RefetchRequired(envelope, align062Invalidation(t, 43))
	if err != nil {
		t.Fatalf("RefetchRequired(newer): %v", err)
	}
	if !required {
		t.Fatal("newer invalidation did not require a refetch")
	}
	// A duplicate invalidation at the applied position is ignorable.
	quiet, err := conformance.RefetchRequired(envelope, align062Invalidation(t, 42))
	if err != nil {
		t.Fatalf("RefetchRequired(current): %v", err)
	}
	if quiet {
		t.Fatal("duplicate invalidation required a refetch")
	}
	// Duplicate queries share one deduplication key.
	first, err := conformance.RequestKey(request(t, false, false))
	if err != nil {
		t.Fatalf("RequestKey: %v", err)
	}
	second, err := conformance.RequestKey(request(t, false, true))
	if err != nil {
		t.Fatalf("RequestKey(reversed): %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("duplicate query keys %q and %q", first, second)
	}
}

func TestTodo_ALIGN_062_Property(t *testing.T) {
	first, err := conformance.RequestKey(request(t, true, false))
	if err != nil {
		t.Fatal(err)
	}
	second, err := conformance.RequestKey(request(t, true, true))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("request key is order-dependent: %s != %s", first, second)
	}
	narrow, err := conformance.RequestKey(request(t, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if narrow == first {
		t.Fatal("different subject sets share a request key")
	}
}

func TestTodo_ALIGN_062_Golden(t *testing.T) {
	key, err := conformance.RequestKey(request(t, false, false))
	if err != nil {
		t.Fatalf("RequestKey: %v", err)
	}
	const wantKey = "sha256:e5678f25c83e414fb12a139e04217fc4cbbcfaa231d6f4d5afe790f3573789c6"
	if key != wantKey {
		t.Fatalf("request key=%q want=%q", key, wantKey)
	}
}

func TestTodo_ALIGN_062_Security(t *testing.T) {
	envelope := execute(t, false)
	// A foreign-tenant invalidation is refused, never judged.
	foreign := align062Invalidation(t, 43)
	foreign.Tenant = values.TenantId("vendor")
	if _, err := conformance.RefetchRequired(envelope, foreign); !errors.Is(err, conformance.ErrInvalidationRejected) {
		t.Fatalf("RefetchRequired(foreign) = %v, want ErrInvalidationRejected", err)
	}
	// A projection-mismatched invalidation is refused.
	other := align062Invalidation(t, 43)
	other.ProjectionVersion = "other-projection.v1"
	if _, err := conformance.RefetchRequired(envelope, other); !errors.Is(err, conformance.ErrInvalidationRejected) {
		t.Fatalf("RefetchRequired(mismatched) = %v, want ErrInvalidationRejected", err)
	}
	// A tampered envelope answers no safety question.
	tampered := envelope
	tampered.SemanticDigest = "sha256:forged"
	if _, err := conformance.RefetchRequired(tampered, align062Invalidation(t, 43)); err == nil {
		t.Fatal("tampered envelope was judged safe")
	}
}

func TestTodo_ALIGN_062_Integration(t *testing.T) {
	// Sixteen concurrent clients racing the same envelope and invalidation
	// decide identically: safety is a pure function, not a race.
	envelope := execute(t, false)
	message := align062Invalidation(t, 43)
	const clients = 16
	var wg sync.WaitGroup
	verdicts := make([]bool, clients)
	errs := make([]error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			verdicts[i], errs[i] = conformance.RefetchRequired(envelope, message)
		}(i)
	}
	wg.Wait()
	for i := 0; i < clients; i++ {
		if errs[i] != nil {
			t.Fatalf("client %d: %v", i, errs[i])
		}
		if !verdicts[i] {
			t.Fatalf("client %d decided no refetch against a newer invalidation", i)
		}
	}
	// Duplicate Execute calls ask the same question under one key.
	first, err := conformance.RequestKey(request(t, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.Execute(context.Background(), request(t, false, false)); err != nil {
		t.Fatal(err)
	}
	second, err := conformance.RequestKey(request(t, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("repeated execution changed the request key")
	}
}

func TestTodo_ALIGN_062_Fault(t *testing.T) {
	envelope := execute(t, false)
	// A stale invalidation (older than the applied position) requires no
	// refetch and no error: it is safely ignorable.
	quiet, err := conformance.RefetchRequired(envelope, align062Invalidation(t, 41))
	if err != nil {
		t.Fatalf("RefetchRequired(stale): %v", err)
	}
	if quiet {
		t.Fatal("stale invalidation required a refetch")
	}
	// A zero-sequence invalidation is invalid before any comparison.
	broken := align062Invalidation(t, 43)
	broken.SourceSequence = 0
	if _, err := conformance.RefetchRequired(envelope, broken); !errors.Is(err, conformance.ErrInvalidationRejected) {
		t.Fatalf("RefetchRequired(zero sequence) = %v, want ErrInvalidationRejected", err)
	}
}

func TestTodo_ALIGN_062_Conformance(t *testing.T) {
	envelope := execute(t, false)
	// The safety verdict tracks the projection position exactly: at, and
	// only past, the applied sequence does the client refetch.
	for _, sequence := range []uint64{41, 42} {
		required, err := conformance.RefetchRequired(envelope, align062Invalidation(t, sequence))
		if err != nil {
			t.Fatal(err)
		}
		if required {
			t.Fatalf("sequence %d required a refetch at position 42", sequence)
		}
	}
	required, err := conformance.RefetchRequired(envelope, align062Invalidation(t, 43))
	if err != nil {
		t.Fatal(err)
	}
	if !required {
		t.Fatal("sequence 43 did not require a refetch at position 42")
	}
}

func FuzzTodo_ALIGN_062_Fuzz(f *testing.F) {
	f.Add(uint64(42))
	f.Fuzz(func(t *testing.T, sequence uint64) {
		envelope := execute(t, false)
		message := align062Invalidation(t, 43)
		message.SourceSequence = sequence
		if sequence == 0 {
			t.Skip("zero sequences are refused by contract")
		}
		first, firstErr := conformance.RefetchRequired(envelope, message)
		second, secondErr := conformance.RefetchRequired(envelope, message)
		if (firstErr == nil) != (secondErr == nil) || first != second {
			t.Fatalf("safety verdict is not deterministic at sequence %d", sequence)
		}
	})
}
