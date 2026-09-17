package app

import (
	"errors"
	"sync"
	"testing"
)

func align031Submission() ProductSubmission {
	return SubmissionFromAccepted(align030Action())
}

// TestTodo_ALIGN_031 proves semantic idempotency for product submissions:
// the first submission is accepted, an identical resubmission replays the
// stored record with no second effect, and a conflicting reuse of the key
// is refused.
func TestTodo_ALIGN_031(t *testing.T) {
	registry := NewSubmissionRegistry()
	first, outcome, err := registry.Submit(align031Submission())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if outcome != SubmissionAccepted {
		t.Fatalf("outcome = %s, want ACCEPTED", outcome)
	}
	if first.Digest == "" {
		t.Fatal("accepted submission has no digest")
	}
	second, outcome, err := registry.Submit(align031Submission())
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if outcome != SubmissionReplayed {
		t.Fatalf("resubmit outcome = %s, want REPLAYED", outcome)
	}
	if second != first {
		t.Fatalf("replay diverged: %+v != %+v", second, first)
	}
	stored, ok := registry.Lookup(align030Tenant, "idem-1")
	if !ok || stored != first {
		t.Fatalf("lookup = %+v, %v, want the accepted record", stored, ok)
	}
}

func TestTodo_ALIGN_031_Property(t *testing.T) {
	left := NewSubmissionRegistry()
	right := NewSubmissionRegistry()
	first, _, err := left.Submit(align031Submission())
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := right.Submit(align031Submission())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("submission digest is not deterministic: %s != %s", first.Digest, second.Digest)
	}
}

func TestTodo_ALIGN_031_Golden(t *testing.T) {
	registry := NewSubmissionRegistry()
	record, _, err := registry.Submit(align031Submission())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	const wantDigest = "sha256:e34094a0478bb31b5c9147bebc79e039df90675e0050c6ebffc4fb1a88f2d829"
	if record.Digest != wantDigest {
		t.Fatalf("submission digest=%q want=%q", record.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_031_Security(t *testing.T) {
	registry := NewSubmissionRegistry()
	if _, _, err := registry.Submit(align031Submission()); err != nil {
		t.Fatal(err)
	}
	// The same key with different content is a conflict, not a replay.
	conflict := align031Submission()
	conflict.ProposalDigest = "sha256:forged"
	if _, _, err := registry.Submit(conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("Submit(conflict) = %v, want ErrIdempotencyConflict", err)
	}
	// Keys are tenant-scoped: another tenant's same key is a distinct
	// submission, never a replay of this tenant's record.
	foreign := align031Submission()
	foreign.Tenant = "vendor"
	record, outcome, err := registry.Submit(foreign)
	if err != nil || outcome != SubmissionAccepted {
		t.Fatalf("Submit(foreign) = %+v, %s, %v, want ACCEPTED", record, outcome, err)
	}
	if _, ok := registry.Lookup(align030Tenant, "idem-1"); !ok {
		t.Fatal("original tenant record was disturbed by a foreign submission")
	}
	// An empty key is invalid, never a wildcard.
	empty := align031Submission()
	empty.IdempotencyKey = ""
	if _, _, err := registry.Submit(empty); !errors.Is(err, ErrSubmissionInvalid) {
		t.Fatalf("Submit(empty key) = %v, want ErrSubmissionInvalid", err)
	}
}

func TestTodo_ALIGN_031_Integration(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	binding, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatalf("BindAcceptedAction: %v", err)
	}
	// The submission carries exactly the bound content onto the
	// idempotency registry: acceptance, plan binding, and submission agree
	// on tenant, revision, digest, and key.
	submission := SubmissionFromAccepted(align030Action())
	if submission.ProposalDigest != binding.ProposalDigest || submission.IdempotencyKey != binding.IdempotencyKey {
		t.Fatalf("submission %+v does not carry the binding %+v", submission, binding)
	}
	registry := NewSubmissionRegistry()
	first, outcome, err := registry.Submit(submission)
	if err != nil || outcome != SubmissionAccepted {
		t.Fatalf("Submit(bound) = %+v, %s, %v", first, outcome, err)
	}
	second, outcome, err := registry.Submit(submission)
	if err != nil || outcome != SubmissionReplayed || second != first {
		t.Fatalf("resubmit(bound) = %+v, %s, %v", second, outcome, err)
	}
}

func TestTodo_ALIGN_031_Fault(t *testing.T) {
	registry := NewSubmissionRegistry()
	// Sixteen concurrent identical submissions admit exactly one effect.
	const writers = 16
	var wg sync.WaitGroup
	outcomes := make([]SubmissionOutcome, writers)
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, outcomes[i], errs[i] = registry.Submit(align031Submission())
		}(i)
	}
	wg.Wait()
	accepted := 0
	for i := 0; i < writers; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent submit %d: %v", i, errs[i])
		}
		if outcomes[i] == SubmissionAccepted {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("%d concurrent submissions accepted, want exactly 1", accepted)
	}
	// A zero tenant is invalid before any registry access.
	zero := align031Submission()
	zero.Tenant = ""
	if _, _, err := registry.Submit(zero); !errors.Is(err, ErrSubmissionInvalid) {
		t.Fatalf("Submit(zero tenant) = %v, want ErrSubmissionInvalid", err)
	}
}

func TestTodo_ALIGN_031_Conformance(t *testing.T) {
	registry := NewSubmissionRegistry()
	if _, _, err := registry.Submit(align031Submission()); err != nil {
		t.Fatal(err)
	}
	if got := registry.Explain(); got == "" {
		t.Fatal("idempotency registry has no explanation")
	}
	if _, ok := registry.Lookup(align030Tenant, "no-such-key"); ok {
		t.Fatal("unknown key resolved to a record")
	}
	// Distinct keys are independent submissions.
	other := align031Submission()
	other.IdempotencyKey = "idem-2"
	if _, outcome, err := registry.Submit(other); err != nil || outcome != SubmissionAccepted {
		t.Fatalf("Submit(second key) = %s, %v, want ACCEPTED", outcome, err)
	}
}

func FuzzTodo_ALIGN_031_Fuzz(f *testing.F) {
	f.Add("idem-fuzz", "intent-fuzz", "sha256:fuzz")
	f.Fuzz(func(t *testing.T, key, intent, digest string) {
		sub := align031Submission()
		sub.IdempotencyKey = key
		sub.IntentID = intent
		sub.ProposalDigest = digest
		sub.MaterialDigest = digest
		registry := NewSubmissionRegistry()
		first, firstOutcome, firstErr := registry.Submit(sub)
		second, secondOutcome, secondErr := registry.Submit(sub)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("submission is not deterministic: %v vs %v", firstErr, secondErr)
		}
		if firstErr == nil && (secondOutcome != SubmissionReplayed || second != first) {
			t.Fatalf("resubmission diverged: %+v, %s", second, secondOutcome)
		}
		_ = firstOutcome
	})
}
