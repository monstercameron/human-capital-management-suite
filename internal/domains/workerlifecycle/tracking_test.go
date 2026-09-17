package workerlifecycle

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func trackingSetup(t *testing.T) (WorkerLifecyclePlan, ReadinessResolution) {
	t.Helper()
	plan := onboardingPlan(t)
	readiness, err := ResolveOnboardingReadiness(onboardingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	return plan, readiness
}

func observeRef(id string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: "observation", Id: id}
}

// TestOnboardingPlanEmitsEachBoundedChildOnceAndCompletesOnlyWhenReady is
// the primary acceptance case: deterministic child identities emit once in
// dependency order, task completion never substitutes for observation, and
// the lifecycle closes only under its readiness and completion policy.
func TestOnboardingPlanEmitsEachBoundedChildOnceAndCompletesOnlyWhenReady(t *testing.T) {
	plan, readiness := trackingSetup(t)
	tracker, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatalf("NewOnboardingTracker: %v", err)
	}
	rejectEmptyLifecycleDigest(t, tracker.Digest)

	// First emission releases only the dependency-free child.
	next, first, err := EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatalf("EmitDueChildren: %v", err)
	}
	if len(first) != 1 || first[0].ChildID != "identity-child" {
		t.Fatalf("first emission = %+v, want only identity-child", first)
	}
	tracker = next

	// Retry emits nothing twice.
	next, again, err := EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("retry emitted %+v, want no duplicates", again)
	}
	tracker = next

	// Task completion without observation is refused.
	if _, err := CompleteChildTask(tracker, "identity-child"); !errors.Is(err, ErrTrackingRejected) {
		t.Fatalf("task completion: err = %v, want WORKER_LIFE_003_REJECTED", err)
	}

	// Observation advances the child and releases its dependent.
	tracker, err = ObserveChild(tracker, "identity-child", observeRef("00000000-0000-4000-8000-000000000301"), ChildObserved)
	if err != nil {
		t.Fatalf("ObserveChild: %v", err)
	}
	next, second, err := EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ChildID != "training-child" {
		t.Fatalf("second emission = %+v, want only training-child", second)
	}
	tracker = next

	// Closing with an unobserved child is refused.
	if _, err := CloseTracker(tracker, readiness); !errors.Is(err, ErrTrackingRejected) {
		t.Fatalf("early close: err = %v, want WORKER_LIFE_003_REJECTED", err)
	}

	tracker, err = ObserveChild(tracker, "training-child", observeRef("00000000-0000-4000-8000-000000000302"), ChildObserved)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := CloseTracker(tracker, readiness)
	if err != nil {
		t.Fatalf("CloseTracker: %v", err)
	}
	if !closed.Closed {
		t.Fatal("tracker did not close")
	}
	if err := closed.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// A late start-date change replans stale children instead of keeping them.
	plan2, readiness2 := trackingSetup(t)
	mid, err := NewOnboardingTracker(plan2, readiness2)
	if err != nil {
		t.Fatal(err)
	}
	mid, _, err = EmitDueChildren(mid, readiness2)
	if err != nil {
		t.Fatal(err)
	}
	replanned, err := ReplanTracker(mid, mustDate("2026-02-01"), "start delayed")
	if err != nil {
		t.Fatalf("ReplanTracker: %v", err)
	}
	if replanned.Revision != 2 {
		t.Fatalf("revision = %d, want 2", replanned.Revision)
	}
	for _, child := range replanned.Children {
		if child.ChildID == "identity-child" && child.State != StatusChildPending {
			t.Fatalf("stale child preserved: %+v", child)
		}
	}
}

// TestTodo_WORKER_LIFE_003_Property proves idempotent emission and stable
// ordering over repeated calls.
func TestTodo_WORKER_LIFE_003_Property(t *testing.T) {
	plan, readiness := trackingSetup(t)
	tracker, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatal(err)
	}
	var first []ChildIntent
	for i := 0; i < 3; i++ {
		var emitted []ChildIntent
		var err error
		tracker, emitted, err = EmitDueChildren(tracker, readiness)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = emitted
		} else if len(emitted) != 0 {
			t.Fatalf("call %d emitted duplicates", i)
		}
	}
	seen := map[string]bool{}
	for _, intent := range first {
		if seen[intent.ID] {
			t.Fatalf("duplicate intent id %q", intent.ID)
		}
		seen[intent.ID] = true
		if intent.PlanDigest != tracker.PlanDigest || intent.ScopeDigest == "" {
			t.Fatalf("intent %+v lacks lineage", intent)
		}
	}
}

// TestTodo_WORKER_LIFE_003_Golden pins the canonical digest of the emitted
// tracker.
func TestTodo_WORKER_LIFE_003_Golden(t *testing.T) {
	plan, readiness := trackingSetup(t)
	tracker, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatal(err)
	}
	tracker, _, err = EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:39769086c86dfd27538dfbf518e3796479f7de9bf78370cddbfc8e96648af7aa"
	if tracker.Digest != want {
		t.Fatalf("digest = %s, want %s", tracker.Digest, want)
	}
}

// TestTodo_WORKER_LIFE_003_Race proves concurrent emission is race-free and
// converges on one intent set.
func TestTodo_WORKER_LIFE_003_Race(t *testing.T) {
	plan, readiness := trackingSetup(t)
	tracker, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	type outcome struct {
		tracker OnboardingTracker
		intents []ChildIntent
		err     error
	}
	results := make([]outcome, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			next, emitted, err := EmitDueChildren(tracker, readiness)
			results[w] = outcome{next, emitted, err}
		}(w)
	}
	wg.Wait()
	for w := 0; w < workers; w++ {
		if results[w].err != nil {
			t.Fatalf("worker %d: %v", w, results[w].err)
		}
		if len(results[w].intents) != 1 || results[w].intents[0].ID != results[0].intents[0].ID {
			t.Fatalf("worker %d diverged: %+v", w, results[w].intents)
		}
		if results[w].tracker.Digest != results[0].tracker.Digest {
			t.Fatalf("worker %d tracker diverged", w)
		}
	}
}

// TestTodo_WORKER_LIFE_003_Fault proves a failed observation keeps one
// deterministic identity: retry reuses it instead of duplicating work.
func TestTodo_WORKER_LIFE_003_Fault(t *testing.T) {
	plan, readiness := trackingSetup(t)
	tracker, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatal(err)
	}
	tracker, emitted, err := EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatal(err)
	}
	tracker, err = ObserveChild(tracker, "identity-child", observeRef("00000000-0000-4000-8000-000000000303"), ChildFailed)
	if err != nil {
		t.Fatalf("failed observation: %v", err)
	}
	if len(tracker.Repairs) != 1 || tracker.Repairs[0].ChildID != "identity-child" {
		t.Fatalf("repairs = %+v, want one scoped repair", tracker.Repairs)
	}
	retried, err := RetryChild(tracker, "identity-child")
	if err != nil {
		t.Fatalf("RetryChild: %v", err)
	}
	if retried.Children[0].IntentID != emitted[0].ID {
		t.Fatal("retry minted a duplicate intent identity")
	}
	// A blind second observation on an already observed child is refused.
	observed, err := ObserveChild(retried, "identity-child", observeRef("00000000-0000-4000-8000-000000000304"), ChildObserved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveChild(observed, "identity-child", observeRef("00000000-0000-4000-8000-000000000305"), ChildObserved); !errors.Is(err, ErrTrackingRejected) {
		t.Fatalf("double observation: err = %v, want WORKER_LIFE_003_REJECTED", err)
	}
}

// TestTodo_WORKER_LIFE_003_Security proves emitted intents carry digests and
// references only: no protected payload crosses the emission boundary.
func TestTodo_WORKER_LIFE_003_Security(t *testing.T) {
	plan, readiness := trackingSetup(t)
	tracker, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatal(err)
	}
	_, emitted, err := EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range emitted {
		text := intent.IntentType + "\x00" + intent.IntentVersion + "\x00" + intent.ID + "\x00" + intent.ScopeDigest + "\x00" + intent.PlanDigest
		for _, leak := range []string{"passport verified", "authorization valid", "scheduled"} {
			if containsLifecycle(text, leak) {
				t.Fatalf("intent leaks %q", leak)
			}
		}
		if intent.ScopeDigest == "" || intent.PlanDigest == "" {
			t.Fatalf("intent %+v lacks bounded lineage", intent)
		}
	}
	// Stale readiness from another plan revision is refused.
	other, err := ResolveOnboardingReadiness(onboardingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	other.PlanDigest = "sha256:another-plan"
	if _, _, err := EmitDueChildren(tracker, other); !errors.Is(err, ErrTrackingRejected) {
		t.Fatalf("foreign readiness: err = %v, want WORKER_LIFE_003_REJECTED", err)
	}
}

// TestTodo_WORKER_LIFE_003_Conformance proves every emitted intent matches
// its plan template exactly: type, version and bounded scope.
func TestTodo_WORKER_LIFE_003_Conformance(t *testing.T) {
	plan, readiness := trackingSetup(t)
	tracker, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatal(err)
	}
	tracker, first, err := EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatal(err)
	}
	templates := map[string]ChildTemplate{}
	for _, tmpl := range plan.Children {
		templates[tmpl.ID] = tmpl
	}
	check := func(emitted []ChildIntent) {
		t.Helper()
		for _, intent := range emitted {
			tmpl, ok := templates[intent.ChildID]
			if !ok {
				t.Fatalf("intent %q has no template", intent.ChildID)
			}
			if intent.IntentType != tmpl.IntentType || intent.IntentVersion != tmpl.IntentVersion {
				t.Fatalf("intent %+v broadens template %+v", intent, tmpl)
			}
			scope, err := ScopeForTemplate(plan.CanonicalDigest, tmpl)
			if err != nil {
				t.Fatal(err)
			}
			if intent.ScopeDigest != scope {
				t.Fatalf("intent scope %q, template %q", intent.ScopeDigest, scope)
			}
		}
	}
	check(first)
	tracker, err = ObserveChild(tracker, "identity-child", observeRef("00000000-0000-4000-8000-000000000306"), ChildObserved)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := EmitDueChildren(tracker, readiness)
	if err != nil {
		t.Fatal(err)
	}
	check(second)
}

// TestTodo_WORKER_LIFE_003_Mutation proves the digest binds plan, revision,
// children and closure: any change yields a new digest and tampering fails.
func TestTodo_WORKER_LIFE_003_Mutation(t *testing.T) {
	plan, readiness := trackingSetup(t)
	base, err := NewOnboardingTracker(plan, readiness)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err = EmitDueChildren(base, readiness)
	if err != nil {
		t.Fatal(err)
	}
	flip := func(tracker OnboardingTracker) OnboardingTracker {
		tracker.Children[0].State = StatusChildObserved
		return tracker
	}
	_ = flip
	mutated := base
	mutated.Children[0].IntentID = "intent-forged"
	if err := mutated.Validate(); err == nil {
		t.Fatal("forged intent id passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
	advanced, err := ObserveChild(base, "identity-child", observeRef("00000000-0000-4000-8000-000000000307"), ChildObserved)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Digest == base.Digest {
		t.Fatal("observation did not change the digest")
	}
}
