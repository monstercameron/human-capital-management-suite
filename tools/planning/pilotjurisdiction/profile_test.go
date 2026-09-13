package pilotjurisdiction

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// TestPilotJurisdictionSelectionHasAuthoritativeSourcesReviewScopeAndStopConditions
// is SELECT-001's PRIMARY test. It loads the real, checked-in
// definitions/planning/gates/select-001-jurisdiction-profile.yaml and proves
// every RED element is structurally present, that the exclusions are exact
// (not merely non-empty), and that the profile is honestly marked
// unreviewed rather than claiming a review that never happened: Validate
// reports exactly one violation, on reviewer.name, and none other.
func TestPilotJurisdictionSelectionHasAuthoritativeSourcesReviewScopeAndStopConditions(t *testing.T) {
	p := mustLoadProfile(t)

	violations := p.Validate()
	if len(violations) != 1 {
		t.Fatalf("expected exactly one violation (the empty reviewer.name), got %d: %v", len(violations), violations)
	}
	if violations[0].Field != "reviewer.name" {
		t.Fatalf("expected the one violation to be reviewer.name, got %v", violations[0])
	}

	// RED: authoritative source citations, pinned with exact versions.
	if len(p.Sources) == 0 {
		t.Fatal("profile carries no source citations")
	}
	var foundResearch, foundPack bool
	for _, s := range p.Sources {
		if s.ContentDigest == "" {
			t.Fatalf("source %s has no pinned content digest", s.Path)
		}
		switch s.Path {
		case "planning/research/state-employment-law/california.md":
			foundResearch = true
		case "definitions/legal/packs/states/us-ca.json":
			foundPack = true
			if s.PackID == "" || s.VersionMajor == 0 || s.VocabularyVersion == 0 {
				t.Fatalf("pack source is not version-pinned: %+v", s)
			}
		}
	}
	if !foundResearch || !foundPack {
		t.Fatalf("profile must cite both the research memo and the pinned pack definition, got %+v", p.Sources)
	}

	// RED: qualified legal owner/reviewer is a structural field, honestly
	// empty on the checked-in profile (see doc.go).
	if p.Reviewer.Name != "" {
		t.Fatalf("expected the checked-in profile's reviewer to be honestly empty, got %q", p.Reviewer.Name)
	}

	// RED: included intent/rule/filing set.
	if len(p.ScopeItems) == 0 {
		t.Fatal("profile names no included intents")
	}
	if len(p.ObligationMappings) == 0 {
		t.Fatal("profile maps no obligation kinds")
	}
	for _, m := range p.ObligationMappings {
		if _, ok := m.KindSpec(); !ok {
			t.Errorf("obligation mapping %s does not resolve against legal.ObligationKindSpecFor", m.Kind)
		}
	}

	// RED: effective and known-at interval.
	if p.Window.EffectiveStart == "" || p.Window.KnownAtStart == "" {
		t.Fatalf("profile is missing its effective/known-at interval: %+v", p.Window)
	}

	// RED: collective/company interaction.
	if p.Collective.CBAAssumption == "" || p.Collective.MultiEntityHandling == "" {
		t.Fatalf("profile is missing collective/company interaction: %+v", p.Collective)
	}

	// RED/GREEN: uncertainty behavior is declared using exactly the closed
	// vocabulary GREEN names.
	if p.Uncertainty.AmbiguousInputStatus != UncertaintyHumanReviewRequired && p.Uncertainty.AmbiguousInputStatus != UncertaintyUnknown {
		t.Fatalf("uncertainty.ambiguous_input_status is not a declared status: %q", p.Uncertainty.AmbiguousInputStatus)
	}
	if p.Uncertainty.OutOfScopeStatus != UncertaintyHumanReviewRequired && p.Uncertainty.OutOfScopeStatus != UncertaintyUnknown {
		t.Fatalf("uncertainty.out_of_scope_status is not a declared status: %q", p.Uncertainty.OutOfScopeStatus)
	}

	// RED: update SLA.
	if p.UpdateSLA.ReviewCadenceDays <= 0 || len(p.UpdateSLA.TriggerEvents) == 0 {
		t.Fatalf("profile is missing an update SLA: %+v", p.UpdateSLA)
	}

	// RED: prohibited legal-advice boundary.
	if !strings.Contains(strings.ToLower(p.LegalAdviceDisclaimer), "not legal advice") {
		t.Fatalf("profile is missing the prohibited legal-advice boundary: %q", p.LegalAdviceDisclaimer)
	}

	// RED: stop/reselect threshold.
	if len(p.StopReselectThresholds) == 0 {
		t.Fatal("profile names no stop/reselect thresholds")
	}

	// GREEN: exact exclusions - every legal obligation kind is accounted for
	// exactly once, mapped or excluded, never both, never neither. This is
	// re-derived here (not just trusted from Validate) against the compiled
	// vocabulary so the PRIMARY test itself proves exactness.
	mapped := map[string]bool{}
	for _, m := range p.ObligationMappings {
		mapped[m.Kind] = true
	}
	excluded := map[string]bool{}
	for _, e := range p.Exclusions {
		if e.Kind == ExclusionKindObligation {
			excluded[e.Value] = true
		}
	}
	for _, kind := range legal.AllObligationTypes() {
		token := kind.String()
		if mapped[token] == excluded[token] {
			t.Errorf("obligation kind %s must be mapped XOR excluded, mapped=%v excluded=%v", token, mapped[token], excluded[token])
		}
	}

	// Signature verifies.
	ok, err := VerifyProfileSignature(p)
	if err != nil {
		t.Fatalf("VerifyProfileSignature: %v", err)
	}
	if !ok {
		t.Fatal("the checked-in profile must verify against its own signature")
	}
}
