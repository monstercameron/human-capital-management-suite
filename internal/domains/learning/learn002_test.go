package learning

import (
	"strings"
	"testing"
)

func TestTodo_LEARN_002(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	// Satisfied prerequisites resolve ELIGIBLE.
	res, err := r.ResolveEligibility(competentLearner(), "crs-safety-101", 2)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if res.Verdict != VerdictEligible {
		t.Fatalf("verdict = %q, want ELIGIBLE", res.Verdict)
	}
	// Missing prerequisite returns CONDITIONAL with named blockers.
	res, err = r.ResolveEligibility(LearnerEvidence{LearnerID: "worker-9"}, "crs-safety-101", 2)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if res.Verdict != VerdictConditional {
		t.Fatalf("verdict = %q, want CONDITIONAL", res.Verdict)
	}
	if len(res.BlockerRefs) == 0 || !strings.Contains(res.Explanation, "crs-orientation@1") {
		t.Fatalf("conditional omits blockers/explanation: %+v", res)
	}
	// Unknown course returns UNKNOWN with explanation.
	res, err = r.ResolveEligibility(competentLearner(), "crs-nope", 1)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if res.Verdict != VerdictUnknown {
		t.Fatalf("verdict = %q, want UNKNOWN", res.Verdict)
	}
	// Expired prerequisite blocks with explanation.
	res, err = r.ResolveEligibility(LearnerEvidence{
		LearnerID: "worker-10", CompletedCourseRefs: []string{"crs-orientation@1"},
		ExpiredRefs: []string{"crs-orientation@1"},
	}, "crs-safety-101", 2)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if res.Verdict != VerdictBlocked {
		t.Fatalf("verdict = %q, want BLOCKED", res.Verdict)
	}
}

func TestTodo_LEARN_002_Property(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	// Equivalencies reference the exact rule and evidence.
	r.RegisterEquivalency(EquivalencyRule{
		CourseID: "crs-safety-101", Version: 2,
		Satisfies: "crs-orientation@1",
		RuleRef:   "equiv-rule-3", EvidenceKind: "external-cert",
	})
	res, err := r.ResolveEligibility(LearnerEvidence{
		LearnerID:      "worker-11",
		CredentialRefs: []string{"external-cert:osha-10"},
	}, "crs-safety-101", 2)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if res.Verdict != VerdictEligible {
		t.Fatalf("equivalency verdict = %q: %+v", res.Verdict, res)
	}
	if len(res.Equivalencies) != 1 || res.Equivalencies[0].RuleRef != "equiv-rule-3" ||
		res.Equivalencies[0].EvidenceRef != "external-cert:osha-10" {
		t.Fatalf("equivalency cites wrong rule/evidence: %+v", res.Equivalencies)
	}
	// Determinism: identical evidence resolves identically.
	again, err := r.ResolveEligibility(LearnerEvidence{
		LearnerID:      "worker-11",
		CredentialRefs: []string{"external-cert:osha-10"},
	}, "crs-safety-101", 2)
	if err != nil || again.Verdict != res.Verdict {
		t.Fatal("eligibility is not deterministic")
	}
}

func FuzzTodo_LEARN_002(f *testing.F) {
	f.Add([]byte("worker-7"), []byte("crs-safety-101@2"))
	f.Fuzz(func(t *testing.T, learner, ref []byte) {
		// Must never panic; empty learner ids never resolve eligible.
		r := NewRegistry()
		res, err := r.ResolveEligibility(LearnerEvidence{
			LearnerID: string(learner), CompletedCourseRefs: []string{string(ref)},
		}, "crs-safety-101", 2)
		if err != nil {
			t.Fatalf("ResolveEligibility: %v", err)
		}
		if len(learner) == 0 && res.Verdict == VerdictEligible {
			t.Fatal("empty learner resolved eligible")
		}
	})
}

func TestTodo_LEARN_002_Security(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	// Eligibility never discloses other learners' evidence.
	res, err := r.ResolveEligibility(LearnerEvidence{LearnerID: "worker-9"}, c.ID, 2)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if strings.Contains(res.Explanation, "worker-7") {
		t.Fatalf("explanation leaks another learner: %q", res.Explanation)
	}
}

func TestTodo_LEARN_002_Mutation(t *testing.T) {
	r := NewRegistry()
	c := mustCourse(t, r)
	mustVersion(t, r, c.ID)
	// Mutant A: expired prerequisite treated as satisfied must be killed.
	res, err := r.ResolveEligibility(LearnerEvidence{
		LearnerID: "worker-10", CompletedCourseRefs: []string{"crs-orientation@1"},
		ExpiredRefs: []string{"crs-orientation@1"},
	}, c.ID, 2)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if res.Verdict == VerdictEligible {
		t.Fatal("expired-prerequisite mutant survived")
	}
	// Mutant B: equivalency without a registered rule must be killed
	// (credential alone satisfies nothing).
	res, err = r.ResolveEligibility(LearnerEvidence{
		LearnerID: "worker-12", CredentialRefs: []string{"random-badge"},
	}, c.ID, 2)
	if err != nil {
		t.Fatalf("ResolveEligibility: %v", err)
	}
	if res.Verdict == VerdictEligible {
		t.Fatal("rule-less equivalency mutant survived")
	}
}
