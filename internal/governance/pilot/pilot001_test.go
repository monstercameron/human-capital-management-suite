package pilot

import (
	"testing"
)

func pilotEvidence(criterion string, status EvidenceStatus) Evidence {
	return Evidence{
		Criterion: criterion, Status: status,
		Numerator: 95, Denominator: 100,
		Digest:            "sha256:abc123",
		Outcome:           "pilot cohort met the business outcome",
		ObservedUnixMilli: 1756684800000,
		MaxAgeMillis:      86400000,
	}
}

func pilotAllPass() []Evidence {
	return []Evidence{
		pilotEvidence(CriterionSafety, EvidencePass),
		pilotEvidence(CriterionCorrectness, EvidencePass),
		pilotEvidence(CriterionSLO, EvidencePass),
		pilotEvidence(CriterionAdoption, EvidencePass),
		pilotEvidence(CriterionValue, EvidencePass),
		pilotEvidence(CriterionCost, EvidencePass),
		pilotEvidence(CriterionSupport, EvidencePass),
	}
}

func pilotInput(evidence []Evidence) ReviewInput {
	return ReviewInput{
		PilotRef:       "pilot-1",
		Evidence:       evidence,
		NowUnixMilli:   1756684800000,
		RiskTier:       RiskMedium,
		AllowExpansion: false,
	}
}

// TestPilotGoNoGoRejectsMissingStaleWaivedOrBelowThresholdEvidence:
// the review never averages away a critical failure and never treats
// missing, stale, expired or waived evidence as a pass.
func TestPilotGoNoGoRejectsMissingStaleWaivedOrBelowThresholdEvidence(t *testing.T) {
	// Missing support evidence blocks: no averaging around it.
	missing := pilotAllPass()
	missing[6].Status = EvidenceMissing
	verdict, err := Review(pilotInput(missing))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionNoGo {
		t.Fatalf("missing evidence decided %v", verdict.Decision)
	}
	if len(verdict.Blockers) == 0 {
		t.Fatal("missing evidence named no blockers")
	}
	// Stale correctness evidence blocks even when marked pass.
	stale := pilotAllPass()
	stale[1].ObservedUnixMilli = 1756684800000 - 2*86400000
	verdict, err = Review(pilotInput(stale))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionNoGo {
		t.Fatalf("stale evidence decided %v", verdict.Decision)
	}
	// An expired waiver is not a pass.
	waived := pilotAllPass()
	waived[4] = Evidence{
		Criterion: CriterionValue, Status: EvidenceWaived,
		Numerator: 40, Denominator: 100,
		Digest:            "sha256:abc123",
		Outcome:           "value realization below bar",
		ObservedUnixMilli: 1756684800000, MaxAgeMillis: 86400000,
		Waiver: &Waiver{By: "sponsor", Reason: "stale waiver", ExpiresUnixMilli: 1000},
	}
	verdict, err = Review(pilotInput(waived))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionNoGo {
		t.Fatalf("expired waiver decided %v", verdict.Decision)
	}
	// Below-threshold value blocks expansion even when the pilot is
	// otherwise acceptable.
	belowBar := pilotAllPass()
	belowBar[4].Status = EvidenceFail
	belowBar[4].Numerator = 40
	input := pilotInput(belowBar)
	input.AllowExpansion = true
	input.ExpansionManifest = "manifest:expansion-2"
	input.ManifestApproved = true
	verdict, err = Review(input)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Expansion.Permitted {
		t.Fatal("below-threshold value authorized expansion")
	}
	if verdict.Digest == "" {
		t.Fatal("decision package is not digested")
	}
}
