package recruiting

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

func assertNoAppend(t *testing.T, aggregate Aggregate, events, outbox int, what string) {
	t.Helper()
	if len(aggregate.Events) != events || len(aggregate.Outbox) != outbox {
		t.Fatalf("%s appended events/outbox: events=%d want %d outbox=%d want %d",
			what, len(aggregate.Events), events, len(aggregate.Outbox), outbox)
	}
}

func snapshotCounts(aggregate Aggregate) (entities, events, outbox int) {
	return len(aggregate.Requisitions) + len(aggregate.Postings) + len(aggregate.Applications) + len(aggregate.Candidacies),
		len(aggregate.Events), len(aggregate.Outbox)
}

// TestTodo_RECRUIT_001_Property: revisions advance by exactly one per
// accepted command, IDs never change across revisions, digests verify
// against their bodies, and rejected commands change nothing at all.
func TestTodo_RECRUIT_001_Property(t *testing.T) {
	aggregate := openRecruiting(t)
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	first := aggregate.Candidacies["cand-1"]
	if first.Revision != 1 || first.Stage != StageApplied {
		t.Fatalf("candidacy = %+v", first)
	}
	if err := aggregate.TransitionCandidacy("cand-1", 1, StageScreening, recruitInstant(t, 6), recruitKnown(t, 6)); err != nil {
		t.Fatal(err)
	}
	second := aggregate.Candidacies["cand-1"]
	if second.Revision != 2 || second.CandidacyID != first.CandidacyID || second.ApplicationID != first.ApplicationID {
		t.Fatalf("transition rewrote identity: %+v", second)
	}
	if second.CanonicalDigest == first.CanonicalDigest {
		t.Fatal("revision did not change the digest")
	}
	if want := canonicalbytes.Digest(second.body()); second.CanonicalDigest != want {
		t.Fatal("candidacy digest does not verify against its body")
	}

	// Rejected commands change nothing: no entities, events or outbox.
	entities, events, outbox := snapshotCounts(aggregate)
	rejected := []error{
		aggregate.SubmitApplication("app-9", "candidate-1", "req-1", 1, "post-1", 2, "source:ats", recruitInstant(t, 7), recruitKnown(t, 7)),
		aggregate.TransitionCandidacy("cand-1", 9, StageInterview, recruitInstant(t, 7), recruitKnown(t, 7)),
		aggregate.TransitionCandidacy("cand-x", 1, StageInterview, recruitInstant(t, 7), recruitKnown(t, 7)),
		aggregate.TransitionCandidacy("cand-1", 2, CandidacyStage("UNKNOWN"), recruitInstant(t, 7), recruitKnown(t, 7)),
	}
	for i, err := range rejected {
		if err == nil {
			t.Fatalf("rejected command %d accepted", i)
		}
	}
	if gotEntities, gotEvents, gotOutbox := snapshotCounts(aggregate); gotEntities != entities || gotEvents != events || gotOutbox != outbox {
		t.Fatal("rejected commands mutated the aggregate")
	}
}

// TestTodo_RECRUIT_001_Golden: a fixed lifecycle pins its exact event
// trace and requisition digest.
func TestTodo_RECRUIT_001_Golden(t *testing.T) {
	aggregate := openRecruiting(t)
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		stage CandidacyStage
		rev   uint64
	}{{StageScreening, 1}, {StageInterview, 2}, {StageOffer, 3}, {StageHired, 4}} {
		if err := aggregate.TransitionCandidacy("cand-1", step.rev, step.stage, recruitInstant(t, 6), recruitKnown(t, 6)); err != nil {
			t.Fatal(err)
		}
	}
	wantKinds := []string{
		"REQUISITION_OPENED", "POSTING_CREATED", "POSTING_PUBLISHED",
		"APPLICATION_SUBMITTED", "CANDIDACY_CREATED",
		"CANDIDACY_ADVANCED", "CANDIDACY_ADVANCED", "CANDIDACY_ADVANCED", "CANDIDACY_HIRED",
	}
	gotKinds := aggregate.EventKinds()
	if fmt.Sprintf("%v", gotKinds) != fmt.Sprintf("%v", wantKinds) {
		t.Fatalf("event trace = %v, want %v", gotKinds, wantKinds)
	}
	if len(aggregate.Outbox) != len(aggregate.Events) {
		t.Fatal("outbox does not mirror the event log")
	}
	t.Logf("golden requisition digest: %s", aggregate.Requisitions["req-1"].CanonicalDigest)
	if want := "sha256:2a8f75f054ac652dc48030535b0456244048f97dfbac4429a82b3515a9608cc4"; aggregate.Requisitions["req-1"].CanonicalDigest != want {
		t.Fatalf("requisition digest = %q, want golden %q", aggregate.Requisitions["req-1"].CanonicalDigest, want)
	}
}

// TestTodo_RECRUIT_001_Security: consent and purpose unlock a
// candidacy but never leak — refusals name fields only, events carry
// identity and revision only, and the explanation carries counts only.
func TestTodo_RECRUIT_001_Security(t *testing.T) {
	const secretConsent = "consent-secret-7c2"
	const secretPurpose = "purpose-secret-4aa"
	aggregate := openRecruiting(t)
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", secretConsent, secretPurpose, "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	// Refusals name the field, never the governed value.
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", secretConsent, secretPurpose, "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err == nil {
		t.Fatal("duplicate candidacy accepted")
	} else if strings.Contains(err.Error(), secretConsent) || strings.Contains(err.Error(), secretPurpose) {
		t.Fatalf("refusal leaks governed values: %v", err)
	}
	for _, event := range append(append([]RecruitingEvent(nil), aggregate.Events...), aggregate.Outbox...) {
		if strings.Contains(fmt.Sprintf("%+v", event), secretConsent) || strings.Contains(fmt.Sprintf("%+v", event), secretPurpose) {
			t.Fatalf("event leaks governed values: %+v", event)
		}
	}
	if strings.Contains(aggregate.Explain(), secretConsent) || strings.Contains(aggregate.Explain(), secretPurpose) {
		t.Fatalf("explanation leaks governed values: %q", aggregate.Explain())
	}
}

// TestTodo_RECRUIT_001_Conformance: the four lifecycles compose — one
// requisition carries a posting, an application and a candidacy to a
// terminal stage with the full event history preserved.
func TestTodo_RECRUIT_001_Conformance(t *testing.T) {
	aggregate := openRecruiting(t)
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	if err := aggregate.TransitionCandidacy("cand-1", 1, StageWithdrawn, recruitInstant(t, 6), recruitKnown(t, 6)); err != nil {
		t.Fatal(err)
	}
	candidacy := aggregate.Candidacies["cand-1"]
	if candidacy.Stage != StageWithdrawn || candidacy.Revision != 2 {
		t.Fatalf("withdrawn candidacy = %+v", candidacy)
	}
	// Terminal history is preserved: the record, its parents and every
	// event remain addressable after withdrawal.
	if _, ok := aggregate.Applications["app-1"]; !ok {
		t.Fatal("parent application missing after withdrawal")
	}
	if _, ok := aggregate.Postings["post-1"]; !ok {
		t.Fatal("parent posting missing after withdrawal")
	}
	if aggregate.EventKinds()[len(aggregate.Events)-1] != "CANDIDACY_WITHDRAWN" {
		t.Fatalf("event trace = %v", aggregate.EventKinds())
	}
	ids := aggregate.SortedIDs()
	if fmt.Sprintf("%v", ids["candidacy"]) != "[cand-1]" || fmt.Sprintf("%v", ids["application"]) != "[app-1]" {
		t.Fatalf("sorted ids = %v", ids)
	}
}

// TestTodo_RECRUIT_001_Mutation: lifecycle edges resolve on the
// documented side.
func TestTodo_RECRUIT_001_Mutation(t *testing.T) {
	aggregate := openRecruiting(t)
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAggregate(""); err == nil {
		t.Fatal("empty uniqueness policy accepted")
	}
	cases := map[string]struct {
		run  func() error
		code string
	}{
		"unknown stage": {
			func() error {
				return aggregate.TransitionCandidacy("cand-1", 1, CandidacyStage("UNKNOWN"), recruitInstant(t, 6), recruitKnown(t, 6))
			},
			CodeInvalidCandidacyTransition,
		},
		"hire skips offer": {
			func() error {
				return aggregate.TransitionCandidacy("cand-1", 1, StageHired, recruitInstant(t, 6), recruitKnown(t, 6))
			},
			CodeInvalidCandidacyTransition,
		},
		"candidate mismatch": {
			func() error {
				return aggregate.CreateCandidacy("cand-2", "app-1", "candidate-x", "consent:1", "hiring", "source:ats", recruitInstant(t, 6), recruitKnown(t, 6))
			},
			CodeMissingParent,
		},
		"requisition revision mismatch": {
			func() error { return aggregate.CloseRequisition("req-1", 9, recruitInstant(t, 6), recruitKnown(t, 6)) },
			CodeMissingParent,
		},
		"unknown requisition": {
			func() error { return aggregate.CloseRequisition("req-x", 1, recruitInstant(t, 6), recruitKnown(t, 6)) },
			CodeMissingParent,
		},
		"republish posting": {
			func() error { return aggregate.PublishPosting("post-1", 2, recruitInstant(t, 6), recruitKnown(t, 6)) },
			CodeMissingParent,
		},
		"double withdraw application": {
			func() error {
				if err := aggregate.WithdrawApplication("app-1", 1, recruitInstant(t, 6), recruitKnown(t, 6)); err != nil {
					return err
				}
				return aggregate.WithdrawApplication("app-1", 2, recruitInstant(t, 7), recruitKnown(t, 7))
			},
			CodeMissingParent,
		},
	}
	for name, tc := range cases {
		if err := tc.run(); codeOf(err) != tc.code {
			t.Fatalf("%s: err = %v, want code %s", name, err, tc.code)
		}
	}
	// Backward transitions are rejected once the candidacy advances.
	forward := openRecruiting(t)
	forward = submitRecruiting(t, forward, "app-1", "candidate-1")
	if err := forward.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	if err := forward.TransitionCandidacy("cand-1", 1, StageInterview, recruitInstant(t, 8), recruitKnown(t, 8)); err != nil {
		t.Fatal(err)
	}
	if err := forward.TransitionCandidacy("cand-1", 2, StageScreening, recruitInstant(t, 9), recruitKnown(t, 9)); codeOf(err) != CodeInvalidCandidacyTransition {
		t.Fatalf("backward transition err = %v", err)
	}
}
