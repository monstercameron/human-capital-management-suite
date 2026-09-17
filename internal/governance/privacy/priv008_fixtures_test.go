package privacy

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
)

// --- PRIV-008 shared fixtures ------------------------------------------------

const (
	fxFTIReviewedAt  = 1_800_100_000
	fxFTIDisclosedAt = 1_800_200_000
	// fxFTIPayloadDigest is a fixed sha256-shaped digest standing in for a
	// disclosed FTI payload. The log carries digests, never payload.
	fxFTIPayloadDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// fixtureFTIActivity returns a real, validatable APPROVED processing
// activity that explicitly names FTI data: the tax-return-processing
// activity every PRIV-008 matrix test classifies first.
func fixtureFTIActivity(t *testing.T) inventory.ProcessingActivity {
	t.Helper()
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	return inventory.ProcessingActivity{
		ID: "tax-return-processing", Version: "2026-01", Status: inventory.StatusApproved,
		Controller: "acme", Processor: "tax-engine",
		Purpose:          "tax_return_processing",
		DataSubjects:     []string{"worker"},
		DataCategories:   []string{"FTI", "contact"},
		Systems:          []string{"hr", "irs-mef"},
		Recipients:       []string{"irs-mef", "state-revenue-agency"},
		Regions:          []string{"US"},
		LawfulBasis:      "legal-obligation",
		Retention:        "tax-7y",
		SecurityControls: []string{"pub1075-encryption", "audit-logging"},
		DPIARef:          "dpia/tax/1",
		Obligations: []inventory.ObligationDeadline{
			{Kind: inventory.MonitoringConsent, Jurisdiction: "US", RulePackRelease: "us-2026.1", DeadlineHours: 24, EffectiveFrom: from},
			{Kind: inventory.BreachNotification, Jurisdiction: "US", RulePackRelease: "us-2026.1", DeadlineHours: 72, EffectiveFrom: from},
		},
	}
}

// fixtureFTIClassification classifies the fixture activity, failing the
// test on any error so matrix tests start from a known-valid boundary.
func fixtureFTIClassification(t *testing.T) FTIClassification {
	t.Helper()
	c, err := ClassifyFTI(fixtureFTIActivity(t))
	if err != nil {
		t.Fatalf("ClassifyFTI (fixture): %v", err)
	}
	return c
}

// fixtureFTIBoundary builds the complete GREEN boundary: classification,
// a two-entry disclosure log, and a current safeguard review.
func fixtureFTIBoundary(t *testing.T) FTIBoundary {
	t.Helper()
	classification := fixtureFTIClassification(t)
	log := NewFTIDisclosureLog(classification)
	var err error
	log, err = log.Append(FTIDisclosureSpec{
		DisclosureID: "fti-disclosure-1", At: mustInstant(t, fxFTIDisclosedAt),
		Recipient: "irs-mef", Authority: "pub1075-s9-disclosure-auth-2026-001",
		PayloadDigest: fxFTIPayloadDigest,
	})
	if err != nil {
		t.Fatalf("Append (fixture disclosure): %v", err)
	}
	log, err = log.Append(FTIDisclosureSpec{
		DisclosureID: "fti-redisclosure-1", At: mustInstant(t, fxFTIDisclosedAt+3600),
		Recipient: "state-revenue-agency", Authority: "pub1075-s9-redisclosure-auth-2026-002",
		Redisclosure: true, PriorDisclosure: "fti-disclosure-1",
		PayloadDigest: fxFTIPayloadDigest,
	})
	if err != nil {
		t.Fatalf("Append (fixture redisclosure): %v", err)
	}
	review, err := RecordSafeguardReview(SafeguardReviewSpec{
		ReviewID: "fti-safeguard-2026", ActivityRef: classification.ActivityDigest,
		ReviewedAt: mustInstant(t, fxFTIReviewedAt), Reviewer: "safeguard-reviewer-3",
		Findings: "NO_FINDINGS", NextDue: mustInstant(t, fxFTIReviewedAt+365*86400),
	})
	if err != nil {
		t.Fatalf("RecordSafeguardReview (fixture): %v", err)
	}
	return FTIBoundary{Classification: classification, Log: log, Review: review}
}
