package pilotjurisdiction

import "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"

// caPresentKinds is the exact set of legal.ObligationType wire tokens
// definitions/legal/packs/states/us-ca.json declares (verified live by
// conformance_test.go's TestTodo_SELECT_001_Conformance). validFixture uses
// the same set so the unit fixture and the real checked-in file describe the
// same shape of profile even though the fixture is not read from disk.
var caPresentKinds = map[string]bool{
	"NOTICE":              true,
	"FIELD_RESTRICTION":   true,
	"RETENTION":           true,
	"LEAVE_INTERACTION":   true,
	"PAY_FREQUENCY":       true,
	"FINAL_PAY_DEADLINE":  true,
	"PAY_TRANSPARENCY":    true,
	"NON_COMPETE":         true,
	"MINI_WARN":           true,
	"WAGE_FLOOR":          true,
	"PAY_EQUITY_REVIEW":   true,
	"PAY_STATEMENT":       true,
	"CLASSIFICATION":      true,
	"PERSONNEL_FILE":      true,
	"ANTI_RETALIATION":    true,
	"DRUG_TESTING":        true,
	"BREACH_NOTIFICATION": true,
	"MONITORING_CONSENT":  true,
}

const pilotIntentID = "hcmnext.people.promote_worker/v1"

// validFixture returns a minimal, structurally complete JurisdictionProfile:
// every obligation kind is mapped or excluded, every RED element is present,
// and — unlike the real checked-in profile — a fixture-only reviewer is
// named so property_test.go and mutation_test.go can exercise Validate
// itself rather than the one known, documented gap. profile_test.go and
// security_test.go exercise the real signed file directly.
func validFixture() JurisdictionProfile {
	p := JurisdictionProfile{
		SchemaVersion: 1,
		TodoID:        "SELECT-001",
		SignedDate:    "2026-09-13",
		Jurisdiction:  JurisdictionRef{Country: "US", State: "CA"},
		ReviewStatus:  "UNREVIEWED",
		Sources: []SourcePin{
			{
				Kind: "RESEARCH_MEMO", Path: "planning/research/state-employment-law/california.md",
				ReviewStatus: "UNREVIEWED", ContentDigest: "fixture-digest-research",
			},
			{
				Kind: "RULE_PACK_DEFINITION", Path: "definitions/legal/packs/states/us-ca.json",
				PackID: "us-ca-promotion-base-pay-change-draft", VersionMajor: 1, VersionMinor: 0,
				VocabularyVersion: 2, ReviewStatus: "UNREVIEWED", ContentDigest: "fixture-digest-pack",
			},
		},
		Reviewer: Reviewer{
			Name: "fixture-only reviewer (never checked in)", Qualification: "fixture qualification", AssignedDate: "2026-09-13",
		},
		ScopeItems: []ScopeItem{
			{IntentID: pilotIntentID, Disposition: "INCLUDE", Rationale: "fixture scope item"},
		},
		Window:     EffectiveWindow{EffectiveStart: "2026-01-01", KnownAtStart: "2026-09-13T00:00:00Z"},
		Collective: CollectiveInteraction{CBAAssumption: "NONE_ASSUMED", MultiEntityHandling: "fixture multi-entity handling"},
		Uncertainty: UncertaintyPolicy{
			AmbiguousInputStatus: UncertaintyHumanReviewRequired,
			OutOfScopeStatus:     UncertaintyUnknown,
		},
		UpdateSLA:              UpdateSLA{ReviewCadenceDays: 90, TriggerEvents: []string{"STATUTE_AMENDMENT"}},
		LegalAdviceDisclaimer:  "This profile is not legal advice; fixture text.",
		StopReselectThresholds: []StopReselectThreshold{{Metric: "fixture_metric", Threshold: "fixture_threshold", Action: ActionProceed}},
		Signature: &Signature{
			Algorithm: "ed25519", PublicKey: "fixture-public-key", Value: "fixture-signature-value",
			KeyFixture: "tools/planning/gateevidence/testdata/dev-signing-key.yaml",
		},
	}

	for _, t := range legal.AllObligationTypes() {
		if caPresentKinds[t.String()] {
			p.ObligationMappings = append(p.ObligationMappings, ObligationMapping{
				Kind: t.String(), IntentID: pilotIntentID, EvidencePath: "legal.EvaluationResult.Obligations",
			})
		} else {
			p.Exclusions = append(p.Exclusions, Exclusion{
				Kind: ExclusionKindObligation, Value: t.String(), Reason: "fixture: kind absent from the pinned release",
			})
		}
	}
	p.Exclusions = append(p.Exclusions,
		Exclusion{Kind: ExclusionKindJurisdiction, Value: "every jurisdiction other than US-CA", Reason: "fixture: reviewed scope is US-CA only"},
		Exclusion{Kind: ExclusionKindLocality, Value: "every California locality ordinance", Reason: "fixture: pinned pack is state-level only"},
	)
	return p
}

// deepCopy returns an independent copy of p so a mutation in one subtest
// cannot leak into another (slices in Go share backing arrays on a plain
// struct copy).
func deepCopy(p JurisdictionProfile) JurisdictionProfile {
	c := p
	c.Sources = append([]SourcePin(nil), p.Sources...)
	c.ScopeItems = append([]ScopeItem(nil), p.ScopeItems...)
	c.ObligationMappings = append([]ObligationMapping(nil), p.ObligationMappings...)
	c.Exclusions = append([]Exclusion(nil), p.Exclusions...)
	c.UpdateSLA.TriggerEvents = append([]string(nil), p.UpdateSLA.TriggerEvents...)
	c.StopReselectThresholds = append([]StopReselectThreshold(nil), p.StopReselectThresholds...)
	if p.Signature != nil {
		sig := *p.Signature
		c.Signature = &sig
	}
	return c
}
