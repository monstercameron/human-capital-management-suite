package privacy

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_008 is the PRIMARY test for planning/todos.md PRIV-008:
// "Enforce a federal-tax-information processing boundary under Publication
// 1075."
//
// RED (todos.md PRIV-008): "federal tax information flows through the same
// storage, access and transfer path as ordinary employee PII, with no
// validated-cryptography requirement, no strong remote-authentication
// requirement and no disclosure log distinguishing it."
//
// GREEN (todos.md PRIV-008): "PRIV-001's processing-activity inventory gains
// an FTI classification that forces TRUST-028 envelope keys scoped to FTI,
// remote access gated behind step-up assurance, an append-only
// disclosure/redisclosure log and an annual-safeguard-review record."
func TestTodo_PRIV_008(t *testing.T) {
	t.Run("GREEN: an FTI activity classifies, grants stepped-up remote access, logs disclosure and records review", func(t *testing.T) {
		classification, err := ClassifyFTI(fixtureFTIActivity(t))
		if err != nil {
			t.Fatalf("ClassifyFTI: %v", err)
		}
		if err := classification.Validate(); err != nil {
			t.Fatalf("ClassifyFTI produced an invalid classification: %v", err)
		}

		grant, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: classification,
			Principal:      "tax-admin-7",
			Assurance:      trust.AssuranceHigh,
			Remote:         true,
			KeyScope:       classification.KeyScope,
			Purpose:        "tax_return_processing",
		})
		if err != nil {
			t.Fatalf("AuthorizeFTIAccess (remote, high assurance, FTI key scope): %v", err)
		}
		if grant.EvidenceID == "" {
			t.Error("access grant carries no evidence id")
		}

		log := NewFTIDisclosureLog(classification)
		log, err = log.Append(FTIDisclosureSpec{
			DisclosureID:  "fti-disclosure-1",
			At:            mustInstant(t, fxFTIDisclosedAt),
			Recipient:     "irs-mef",
			Authority:     "pub1075-s9-disclosure-auth-2026-001",
			PayloadDigest: fxFTIPayloadDigest,
		})
		if err != nil {
			t.Fatalf("Append (disclosure): %v", err)
		}
		log, err = log.Append(FTIDisclosureSpec{
			DisclosureID:    "fti-redisclosure-1",
			At:              mustInstant(t, fxFTIDisclosedAt+3600),
			Recipient:       "state-revenue-agency",
			Authority:       "pub1075-s9-redisclosure-auth-2026-002",
			Redisclosure:    true,
			PriorDisclosure: "fti-disclosure-1",
			PayloadDigest:   fxFTIPayloadDigest,
		})
		if err != nil {
			t.Fatalf("Append (redisclosure): %v", err)
		}
		if err := log.Verify(); err != nil {
			t.Fatalf("disclosure log does not verify: %v", err)
		}

		review, err := RecordSafeguardReview(SafeguardReviewSpec{
			ReviewID:    "fti-safeguard-2026",
			ActivityRef: classification.ActivityDigest,
			ReviewedAt:  mustInstant(t, fxFTIReviewedAt),
			Reviewer:    "safeguard-reviewer-3",
			Findings:    "NO_FINDINGS",
			NextDue:     mustInstant(t, fxFTIReviewedAt+365*86400),
		})
		if err != nil {
			t.Fatalf("RecordSafeguardReview: %v", err)
		}

		if err := VerifyBoundary(FTIBoundary{Classification: classification, Log: log, Review: review}, mustInstant(t, fxFTIReviewedAt)); err != nil {
			t.Errorf("complete FTI boundary does not verify: %v", err)
		}
	})

	t.Run("RED: an activity that never names FTI data takes the ordinary-PII path and cannot classify", func(t *testing.T) {
		activity := fixtureFTIActivity(t)
		activity.DataCategories = []string{"work_email", "job_title"} // ordinary employee PII only
		if _, err := ClassifyFTI(activity); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("ClassifyFTI(ordinary PII) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("RED: an ordinary tenant key scope is refused for FTI access", func(t *testing.T) {
		classification, err := ClassifyFTI(fixtureFTIActivity(t))
		if err != nil {
			t.Fatalf("ClassifyFTI: %v", err)
		}
		_, err = AuthorizeFTIAccess(FTIAccessSpec{
			Classification: classification,
			Principal:      "tax-admin-7",
			Assurance:      trust.AssuranceHigh,
			Remote:         true,
			KeyScope:       "TENANT_STANDARD", // the ordinary-PII envelope scope
			Purpose:        "tax_return_processing",
		})
		if !errors.Is(err, ErrFTIAccessDenied) {
			t.Fatalf("AuthorizeFTIAccess(ordinary key scope) = %v, want %v", err, ErrFTIAccessDenied)
		}
	})

	t.Run("RED: remote access without step-up assurance is denied", func(t *testing.T) {
		classification, err := ClassifyFTI(fixtureFTIActivity(t))
		if err != nil {
			t.Fatalf("ClassifyFTI: %v", err)
		}
		_, err = AuthorizeFTIAccess(FTIAccessSpec{
			Classification: classification,
			Principal:      "tax-admin-7",
			Assurance:      trust.AssuranceSubstantial, // strong, but not stepped-up
			Remote:         true,
			KeyScope:       classification.KeyScope,
			Purpose:        "tax_return_processing",
		})
		if !errors.Is(err, ErrFTIAccessDenied) {
			t.Fatalf("AuthorizeFTIAccess(remote, substantial) = %v, want %v", err, ErrFTIAccessDenied)
		}
	})

	t.Run("RED: a disclosure with no Pub 1075 authority is refused", func(t *testing.T) {
		classification, err := ClassifyFTI(fixtureFTIActivity(t))
		if err != nil {
			t.Fatalf("ClassifyFTI: %v", err)
		}
		log := NewFTIDisclosureLog(classification)
		if _, err := log.Append(FTIDisclosureSpec{
			DisclosureID:  "fti-disclosure-rogue",
			At:            mustInstant(t, fxFTIDisclosedAt),
			Recipient:     "irs-mef",
			PayloadDigest: fxFTIPayloadDigest,
		}); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("Append(authority-less disclosure) = %v, want %v", err, ErrFTIBlocked)
		}
	})
}
