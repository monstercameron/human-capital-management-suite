package recruiting

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func recruitInstant(t *testing.T, day int) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(2026, time.January, day, 12, 0, 0, 0, time.UTC))
}

func recruitKnown(t *testing.T, day int) values.KnownAt {
	t.Helper()
	known, err := values.NewKnownAt(recruitInstant(t, day))
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func openRecruiting(t *testing.T) Aggregate {
	t.Helper()
	aggregate, err := NewAggregate("candidate+requisition")
	if err != nil {
		t.Fatal(err)
	}
	if err := aggregate.OpenRequisition("req-1", "job:registered-nurse", recruitInstant(t, 1), recruitKnown(t, 1)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreatePosting("post-1", "req-1", 1, "job:registered-nurse", recruitInstant(t, 2), recruitKnown(t, 2)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.PublishPosting("post-1", 1, recruitInstant(t, 3), recruitKnown(t, 3)); err != nil {
		t.Fatal(err)
	}
	return aggregate
}

func submitRecruiting(t *testing.T, aggregate Aggregate, appID, candidateID string) Aggregate {
	t.Helper()
	if err := aggregate.SubmitApplication(appID, candidateID, "req-1", 1, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); err != nil {
		t.Fatal(err)
	}
	return aggregate
}

// TestRecruitingAggregateLifecyclesRejectMissingIdentityAndIllegalTransitions:
// every seeded defect — an application without exact parent revisions, a
// duplicate application, a posting after requisition closure, a candidacy
// without consent/purpose, and any transition from a terminal candidacy —
// is rejected with its typed code and appends zero events/outbox entries.
func TestRecruitingAggregateLifecyclesRejectMissingIdentityAndIllegalTransitions(t *testing.T) {
	aggregate := openRecruiting(t)

	// Application without candidate/requisition/posting revisions.
	before, beforeOutbox := len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.SubmitApplication("app-x", "", "req-1", 1, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("missing candidate err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-x", "candidate-1", "req-x", 1, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("missing requisition err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-x", "candidate-1", "req-1", 1, "post-x", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("missing posting err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-x", "candidate-1", "req-1", 9, "post-1", 2, "source:ats", recruitInstant(t, 4), recruitKnown(t, 4)); codeOf(err) != CodeMissingParent {
		t.Fatalf("stale requisition revision err = %v", err)
	}
	assertNoAppend(t, aggregate, before, beforeOutbox, "missing parents")

	// Duplicate application under the declared uniqueness policy.
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	before, beforeOutbox = len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.SubmitApplication("app-2", "candidate-1", "req-1", 1, "post-1", 2, "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); codeOf(err) != CodeDuplicateApplication {
		t.Fatalf("duplicate application err = %v", err)
	}
	assertNoAppend(t, aggregate, before, beforeOutbox, "duplicate application")
	aggregate = submitRecruiting(t, aggregate, "app-2", "candidate-2")
	aggregate = submitRecruiting(t, aggregate, "app-3", "candidate-3")

	// Posting after requisition closure.
	if err := aggregate.CloseRequisition("req-1", 1, recruitInstant(t, 6), recruitKnown(t, 6)); err != nil {
		t.Fatal(err)
	}
	before, beforeOutbox = len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.CreatePosting("post-2", "req-1", 2, "job:registered-nurse", recruitInstant(t, 7), recruitKnown(t, 7)); codeOf(err) != CodeRequisitionClosed {
		t.Fatalf("post-closure posting err = %v", err)
	}
	if err := aggregate.SubmitApplication("app-9", "candidate-9", "req-1", 2, "post-1", 2, "source:ats", recruitInstant(t, 7), recruitKnown(t, 7)); codeOf(err) != CodeRequisitionClosed {
		t.Fatalf("post-closure application err = %v", err)
	}
	assertNoAppend(t, aggregate, before, beforeOutbox, "post-closure commands")

	// Candidacy without consent/purpose.
	before, beforeOutbox = len(aggregate.Events), len(aggregate.Outbox)
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); codeOf(err) != CodeInvalidCandidacyTransition {
		t.Fatalf("missing consent err = %v", err)
	}
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); codeOf(err) != CodeInvalidCandidacyTransition {
		t.Fatalf("missing purpose err = %v", err)
	}

	// Transitions from terminal WITHDRAWN|REJECTED|HIRED are rejected.
	assertNoAppend(t, aggregate, before, beforeOutbox, "missing consent/purpose")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreateCandidacy("cand-w", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.TransitionCandidacy("cand-w", 1, StageWithdrawn, recruitInstant(t, 9), recruitKnown(t, 9)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreateCandidacy("cand-r", "app-2", "candidate-2", "consent:2", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.TransitionCandidacy("cand-r", 1, StageRejected, recruitInstant(t, 9), recruitKnown(t, 9)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.CreateCandidacy("cand-h", "app-3", "candidate-3", "consent:3", "hiring", "source:ats", recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		stage CandidacyStage
		rev   uint64
	}{{StageScreening, 1}, {StageInterview, 2}, {StageOffer, 3}} {
		if err := aggregate.TransitionCandidacy("cand-h", step.rev, step.stage, recruitInstant(t, 9), recruitKnown(t, 9)); err != nil {
			t.Fatal(err)
		}
	}
	if err := aggregate.TransitionCandidacy("cand-h", 4, StageHired, recruitInstant(t, 10), recruitKnown(t, 10)); err != nil {
		t.Fatal(err)
	}
	before = len(aggregate.Events)
	beforeOutbox = len(aggregate.Outbox)
	for id, rev := range map[string]uint64{"cand-w": 2, "cand-r": 2, "cand-h": 5} {
		if err := aggregate.TransitionCandidacy(id, rev, StageScreening, recruitInstant(t, 11), recruitKnown(t, 11)); codeOf(err) != CodeInvalidCandidacyTransition {
			t.Fatalf("terminal transition %s err = %v", id, err)
		}
	}
	if len(aggregate.Events) != before || len(aggregate.Outbox) != beforeOutbox {
		t.Fatal("rejected terminal transitions appended events/outbox entries")
	}
}
