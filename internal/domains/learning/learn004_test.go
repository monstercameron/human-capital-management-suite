package learning

import (
	"sync"
	"testing"
	"time"
)

func completionEvent() CompletionEvent {
	return CompletionEvent{
		EventID: "evt-lms-1001", LearnerID: "worker-7",
		CourseID: "crs-safety-101", Version: 2,
		AssessmentRef:   "quiz-safety-101",
		CompletedAt:     time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
		SourceAuthority: "acme-lms",
		Tenant:          "tenant-acme",
	}
}

func TestTodo_LEARN_004(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	rec, err := r.AcceptCompletion(testCaller, completionEvent())
	if err != nil {
		t.Fatalf("AcceptCompletion: %v", err)
	}
	if rec.Digest == "" {
		t.Fatal("completion record carries no digest")
	}
	// GREEN: acceptance is distinct from verified completion.
	if rec.Status != CompletionAccepted {
		t.Fatalf("fresh intake status = %q, want ACCEPTED", rec.Status)
	}
	// Provider replays dedupe to the identical record.
	replay, err := r.AcceptCompletion(testCaller, completionEvent())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.Duplicate || replay.Digest != rec.Digest {
		t.Fatal("provider replay did not dedupe identically")
	}
	verified, err := r.VerifyCompletion(testCaller, rec.EventID, "evidence-proctor-77")
	if err != nil {
		t.Fatalf("VerifyCompletion: %v", err)
	}
	if verified.Status != CompletionVerified {
		t.Fatalf("verified status = %q, want VERIFIED", verified.Status)
	}
}

func TestTodo_LEARN_004_Race(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	const n = 16
	var wg sync.WaitGroup
	recs := make([]CompletionRecord, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recs[i], errs[i] = r.AcceptCompletion(testCaller, completionEvent())
		}(i)
	}
	wg.Wait()
	firsts := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if recs[i].Digest != recs[0].Digest {
			t.Fatal("concurrent intake diverged")
		}
		if !recs[i].Duplicate {
			firsts++
		}
	}
	if firsts != 1 {
		t.Fatalf("concurrent intake recorded %d firsts, want 1", firsts)
	}
}

func TestTodo_LEARN_004_Integration(t *testing.T) {
	store := NewMemoryCourseStore()
	log := NewMemoryCompletionLog()
	r := NewRegistryWithStores(store, log)
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	first, err := r.AcceptCompletion(testCaller, completionEvent())
	if err != nil {
		t.Fatalf("AcceptCompletion: %v", err)
	}
	// A second registry over the same stores dedupes: the adapters, not
	// process memory, are the source of idempotency. Trust roots stay
	// per-instance deployment config, so the second instance registers
	// its own.
	r2 := NewRegistryWithStores(store, log)
	r2.RegisterProvider("acme-lms")
	second, err := r2.AcceptCompletion(testCaller, completionEvent())
	if err != nil {
		t.Fatalf("cross-instance accept: %v", err)
	}
	if !second.Duplicate || second.Digest != first.Digest {
		t.Fatal("shared log did not dedupe across instances")
	}
}

func TestTodo_LEARN_004_Property(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	// Verification is idempotent and never rewrites acceptance evidence.
	rec, err := r.AcceptCompletion(testCaller, completionEvent())
	if err != nil {
		t.Fatalf("AcceptCompletion: %v", err)
	}
	for i := 0; i < 3; i++ {
		v, err := r.VerifyCompletion(testCaller, rec.EventID, "evidence-proctor-77")
		if err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
		if v.Digest != rec.Digest {
			t.Fatal("verification rewrote acceptance evidence")
		}
	}
}

func FuzzTodo_LEARN_004(f *testing.F) {
	f.Add([]byte("evt-1"), []byte("worker-7"), []byte("acme-lms"))
	f.Fuzz(func(t *testing.T, eventID, learner, source []byte) {
		ev := CompletionEvent{
			EventID: string(eventID), LearnerID: string(learner),
			CourseID: "crs-safety-101", Version: 2,
			AssessmentRef:   "quiz-safety-101",
			CompletedAt:     time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
			SourceAuthority: string(source), Tenant: "tenant-acme",
		}
		// Must never panic; empty identity/authority never validates.
		if err := ValidateCompletionEvent(ev); err == nil &&
			(len(eventID) == 0 || len(learner) == 0 || len(source) == 0) {
			t.Fatal("empty completion event validated")
		}
	})
}

func TestTodo_LEARN_004_Security(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	if _, err := r.AcceptCompletion(rival, completionEvent()); err == nil {
		t.Fatal("cross-tenant completion accepted")
	}
	if n := r.CompletionCount(); n != 0 {
		t.Fatalf("refused completion persisted: %d records", n)
	}
}

func TestTodo_LEARN_004_Mutation(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	// Mutant A: unknown source authority must be killed.
	ev := completionEvent()
	ev.EventID = "evt-unknown-src"
	ev.SourceAuthority = "shady-provider"
	if _, err := r.AcceptCompletion(testCaller, ev); err == nil {
		t.Fatal("unknown-authority mutant survived")
	}
	// Mutant B: assessment outside the version must be killed.
	ev = completionEvent()
	ev.EventID = "evt-wrong-quiz"
	ev.AssessmentRef = "quiz-forged"
	if _, err := r.AcceptCompletion(testCaller, ev); err == nil {
		t.Fatal("foreign-assessment mutant survived")
	}
	// Mutant C: verification without evidence must be killed.
	rec, err := r.AcceptCompletion(testCaller, completionEvent())
	if err != nil {
		t.Fatalf("AcceptCompletion: %v", err)
	}
	if _, err := r.VerifyCompletion(testCaller, rec.EventID, ""); err == nil {
		t.Fatal("evidenceless-verification mutant survived")
	}
}
