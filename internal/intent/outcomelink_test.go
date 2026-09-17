package intent

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func fixtureOutcomeLink() OutcomeLink {
	return OutcomeLink{
		LinkID: "link-001", IntentID: "hcmnext.rewards.promote/v1",
		ResultRef: "result-abc", ProposalRevision: "proposal-7",
		ObservationRef: "obs-90d-retention", MetricDefinition: "retention_90d",
		MetricVersion: "v3",
		SubjectScope:  "worker:w-1",
		CohortScope:   "cohort:promoted-2026-q1",
		EffectiveAt:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		KnownAt:       time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		ObservedAt:    time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		Source:        "metrics-pipeline",
		Watermark:     "metrics@2026-07-02",
		Epistemic:     EpistemicObservedAssociation,
		Confidence:    0.62,
		Limitations:   "single cohort, no control group, confounders not adjusted",
	}
}

// TestIntentOutcomeLinkPreservesObservationProvenanceAndCausalLimits is the
// PRIMARY INTENT-029 contract test: append-only links bind exact
// provenance, correlation is never labeled causation, missing evidence
// never defaults positive and corrections supersede without rewriting.
func TestIntentOutcomeLinkPreservesObservationProvenanceAndCausalLimits(t *testing.T) {
	sealed, err := NewOutcomeLink(fixtureOutcomeLink())
	if err != nil {
		t.Fatalf("NewOutcomeLink: %v", err)
	}
	if sealed.LinkDigest == "" {
		t.Fatal("sealed link carries no digest")
	}

	t.Run("GREEN: full provenance survives the seal", func(t *testing.T) {
		for _, field := range []string{
			sealed.IntentID, sealed.ResultRef, sealed.ProposalRevision,
			sealed.ObservationRef, sealed.MetricDefinition, sealed.SubjectScope,
			sealed.CohortScope, sealed.Source, sealed.Watermark, sealed.Limitations,
		} {
			if strings.TrimSpace(field) == "" {
				t.Fatal("sealed link dropped a provenance dimension")
			}
		}
		if sealed.Epistemic != EpistemicObservedAssociation {
			t.Fatalf("epistemic = %s, want OBSERVED_ASSOCIATION", sealed.Epistemic)
		}
	})

	t.Run("GREEN: correction supersedes without rewriting", func(t *testing.T) {
		fix := fixtureOutcomeLink()
		fix.ObservationRef = "obs-120d-retention"
		fix.ObservedAt = sealed.ObservedAt.Add(30 * 24 * time.Hour)
		fix.KnownAt = fix.ObservedAt
		next, err := CorrectLink(sealed, fix)
		if err != nil {
			t.Fatalf("CorrectLink: %v", err)
		}
		if next.Supersedes != sealed.LinkDigest {
			t.Fatal("correction does not name the exact superseded digest")
		}
		if next.LinkDigest == sealed.LinkDigest {
			t.Fatal("correction digest equals the original: nothing was superseded")
		}
		if sealed.Supersedes != "" {
			t.Fatal("correction mutated the original link")
		}
	})

	t.Run("RED: correlation without a causal basis is never caused-by", func(t *testing.T) {
		causal := fixtureOutcomeLink()
		causal.Epistemic = EpistemicDeclaredCausal
		if _, err := NewOutcomeLink(causal); !errors.Is(err, ErrOutcomeLinkInvalid) {
			t.Fatalf("baseless DECLARED_CAUSAL must be refused, got %v", err)
		}
	})

	t.Run("RED: missing or stale evidence never defaults positive", func(t *testing.T) {
		zero := fixtureOutcomeLink()
		zero.Confidence = 0
		if _, err := NewOutcomeLink(zero); !errors.Is(err, ErrOutcomeLinkInvalid) {
			t.Fatalf("zero-confidence association must be UNKNOWN, got %v", err)
		}
		unknown := fixtureOutcomeLink()
		unknown.Epistemic = EpistemicUnknown
		unknown.Confidence = 0
		unknown.ObservationRef = ""
		if _, err := NewOutcomeLink(unknown); err != nil {
			t.Fatalf("honest UNKNOWN link must seal, got %v", err)
		}
		positive := fixtureOutcomeLink()
		positive.Epistemic = EpistemicUnknown
		positive.Confidence = 0.9
		if _, err := NewOutcomeLink(positive); !errors.Is(err, ErrOutcomeLinkInvalid) {
			t.Fatalf("positive UNKNOWN must be refused, got %v", err)
		}
	})

	t.Run("RED: model assessments never become domain facts", func(t *testing.T) {
		model := fixtureOutcomeLink()
		model.ModelGenerated = true
		model.AsDomainFact = true
		if _, err := NewOutcomeLink(model); !errors.Is(err, ErrOutcomeLinkInvalid) {
			t.Fatalf("model-as-fact must be refused, got %v", err)
		}
		model.AsDomainFact = false
		if _, err := NewOutcomeLink(model); err != nil {
			t.Fatalf("analytical model assessment must seal as non-fact, got %v", err)
		}
	})
}

// TestTodo_INTENT_029_Golden pins the fixture digest and the correction
// chain digest.
func TestTodo_INTENT_029_Golden(t *testing.T) {
	sealed, err := NewOutcomeLink(fixtureOutcomeLink())
	if err != nil {
		t.Fatal(err)
	}
	const goldenLink = "sha256:5b63cafb8cd3071d267a50fa6c2cb3f73606d55103647a12e124e138a922a3ce"
	if sealed.LinkDigest != goldenLink {
		t.Fatalf("link digest drifted: got %s, want %s", sealed.LinkDigest, goldenLink)
	}
	fix := fixtureOutcomeLink()
	fix.ObservationRef = "obs-120d-retention"
	fix.ObservedAt = sealed.ObservedAt.Add(30 * 24 * time.Hour)
	fix.KnownAt = fix.ObservedAt
	next, err := CorrectLink(sealed, fix)
	if err != nil {
		t.Fatal(err)
	}
	chain := ChainDigest([]OutcomeLink{next, sealed})
	const goldenChain = "sha256:69d8d0e0156b22698be648c4396bc29f6bdbfaafe4401cd4e806b630ad02abe9"
	if chain != goldenChain {
		t.Fatalf("chain digest drifted: got %s, want %s", chain, goldenChain)
	}
}

// TestTodo_INTENT_029_Property proves seal stability and supersede-chain
// integrity: identical links seal identically, and every correction names
// its exact predecessor.
func TestTodo_INTENT_029_Property(t *testing.T) {
	first, err := NewOutcomeLink(fixtureOutcomeLink())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOutcomeLink(fixtureOutcomeLink())
	if err != nil {
		t.Fatal(err)
	}
	if first.LinkDigest != second.LinkDigest {
		t.Fatal("identical links seal to different digests")
	}
	prev := first
	for i := 0; i < 5; i++ {
		fix := fixtureOutcomeLink()
		fix.ObservationRef = "obs-rev-" + string(rune('a'+i))
		fix.ObservedAt = prev.ObservedAt.Add(time.Hour)
		fix.KnownAt = fix.ObservedAt
		next, err := CorrectLink(prev, fix)
		if err != nil {
			t.Fatal(err)
		}
		if next.Supersedes != prev.LinkDigest {
			t.Fatalf("revision %d does not name its predecessor", i)
		}
		prev = next
	}
}

// TestTodo_INTENT_029_Security proves analytical access stays
// purpose-governed: no purpose or unauthorized cohort discloses nothing.
func TestTodo_INTENT_029_Security(t *testing.T) {
	sealed, err := NewOutcomeLink(fixtureOutcomeLink())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sealed.Inspect(Inspector{}); !errors.Is(err, ErrOutcomeLinkDenied) {
		t.Fatalf("purposeless inspection must be denied, got %v", err)
	}
	_, redacted, err := sealed.Inspect(Inspector{Purpose: "research", AuthorizedCohorts: []string{"cohort:other"}})
	if err != nil {
		t.Fatal(err)
	}
	if !redacted.Withheld {
		t.Fatal("unauthorized cohort must receive a redacted view")
	}
	if strings.Contains(redacted.Reason, sealed.CohortScope) || strings.Contains(redacted.Reason, sealed.ObservationRef) {
		t.Fatal("redaction reason leaks cohort or outcome detail")
	}
	full, red, err := sealed.Inspect(Inspector{Purpose: "research", AuthorizedCohorts: []string{"cohort:promoted-2026-q1"}})
	if err != nil {
		t.Fatal(err)
	}
	if red.Withheld || full.LinkDigest != sealed.LinkDigest {
		t.Fatal("authorized purpose must receive the full link")
	}
}

// TestTodo_INTENT_029_Conformance walks the epistemic matrix: every status
// seals exactly when its evidence contract holds and is refused otherwise.
func TestTodo_INTENT_029_Conformance(t *testing.T) {
	validCausal := fixtureOutcomeLink()
	validCausal.Epistemic = EpistemicDeclaredCausal
	validCausal.CausalBasis = "randomized rollout ref rollout-42 with pre-registered analysis"
	if _, err := NewOutcomeLink(validCausal); err != nil {
		t.Fatalf("declared causal basis must seal, got %v", err)
	}
	rows := []struct {
		name    string
		mutate  func(*OutcomeLink)
		wantErr bool
	}{
		{"association needs observation", func(l *OutcomeLink) { l.ObservationRef = "" }, true},
		{"causal needs basis", func(l *OutcomeLink) { l.Epistemic = EpistemicDeclaredCausal }, true},
		{"unknown forbids confidence", func(l *OutcomeLink) { l.Epistemic = EpistemicUnknown }, true},
		{"confidence bounds", func(l *OutcomeLink) { l.Confidence = 1.5 }, true},
		{"time order", func(l *OutcomeLink) { l.ObservedAt = l.EffectiveAt.Add(-time.Hour) }, true},
		{"original supersedes nothing", func(l *OutcomeLink) { l.Supersedes = "sha256:other" }, true},
	}
	for _, row := range rows {
		link := fixtureOutcomeLink()
		row.mutate(&link)
		_, err := NewOutcomeLink(link)
		if row.wantErr && !errors.Is(err, ErrOutcomeLinkInvalid) {
			t.Fatalf("%s must be refused, got %v", row.name, err)
		}
		if !row.wantErr && err != nil {
			t.Fatalf("%s must seal, got %v", row.name, err)
		}
	}
}

// TestTodo_INTENT_029_Mutation kills the fabrication mutants: a rewritten
// original, a rebinding correction and a silent predecessor swap must each
// be detected.
func TestTodo_INTENT_029_Mutation(t *testing.T) {
	sealed, err := NewOutcomeLink(fixtureOutcomeLink())
	if err != nil {
		t.Fatal(err)
	}
	rebind := fixtureOutcomeLink()
	rebind.ResultRef = "result-other"
	rebind.ObservedAt = sealed.ObservedAt.Add(time.Hour)
	rebind.KnownAt = rebind.ObservedAt
	if _, err := CorrectLink(sealed, rebind); !errors.Is(err, ErrOutcomeLinkConflict) {
		t.Fatalf("result-rebinding correction must be refused, got %v", err)
	}
	backdate := fixtureOutcomeLink()
	backdate.ObservationRef = "obs-older"
	backdate.ObservedAt = sealed.ObservedAt.Add(-time.Hour)
	backdate.KnownAt = sealed.KnownAt
	if _, err := CorrectLink(sealed, backdate); !errors.Is(err, ErrOutcomeLinkConflict) {
		t.Fatalf("backdated correction must be refused, got %v", err)
	}
	identical := fixtureOutcomeLink()
	identical.ObservedAt = sealed.ObservedAt
	identical.KnownAt = sealed.KnownAt
	if _, err := CorrectLink(sealed, identical); !errors.Is(err, ErrOutcomeLinkConflict) {
		t.Fatalf("identical correction must be refused, got %v", err)
	}
	// The original survives every correction attempt untouched.
	if sealed.Supersedes != "" {
		t.Fatal("failed corrections mutated the original link")
	}
}

// FuzzTodo_INTENT_029 proves arbitrary epistemic labels, confidences and
// scope strings either seal under the exact evidence contract or fail
// closed, and never fabricate causation.
func FuzzTodo_INTENT_029(f *testing.F) {
	f.Add("OBSERVED_ASSOCIATION", 0.62, "obs-1", "basis", false)
	f.Add("DECLARED_CAUSAL", 0.9, "obs-1", "", true)
	f.Add("UNKNOWN", 0.0, "", "", false)
	f.Add("CAUSED_BY", 1.0, "", "", true)
	f.Fuzz(func(t *testing.T, epistemic string, confidence float64, observation, basis string, asFact bool) {
		link := fixtureOutcomeLink()
		link.Epistemic = EpistemicStatus(epistemic)
		link.Confidence = confidence
		link.ObservationRef = observation
		link.CausalBasis = basis
		link.AsDomainFact = asFact
		link.ModelGenerated = asFact
		sealed, err := NewOutcomeLink(link)
		if err != nil {
			if !errors.Is(err, ErrOutcomeLinkInvalid) {
				t.Fatalf("NewOutcomeLink must fail closed, got %v", err)
			}
			return
		}
		if sealed.Epistemic == EpistemicDeclaredCausal && strings.TrimSpace(sealed.CausalBasis) == "" {
			t.Fatal("fuzzed link fabricated causation without a basis")
		}
		if sealed.Epistemic == EpistemicUnknown && sealed.Confidence > 0 {
			t.Fatal("fuzzed link defaulted missing evidence positive")
		}
		if sealed.ModelGenerated && sealed.AsDomainFact {
			t.Fatal("fuzzed link promoted a model assessment to domain fact")
		}
	})
}
