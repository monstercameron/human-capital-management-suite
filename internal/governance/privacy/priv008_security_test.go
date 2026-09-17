package privacy

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_008_Security is the SECURITY matrix test for PRIV-008: the
// FTI path must deny without leaking, the disclosure log must be
// tamper-evident, and raw payload must never enter the evidence.
func TestTodo_PRIV_008_Security(t *testing.T) {
	t.Run("a forged classification with a rewritten key scope never validates", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		c.KeyScope = "TENANT_STANDARD"
		if err := c.Validate(); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("Validate(forged scope) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a classification with a rewritten recipient set never validates", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		c.Recipients = append(c.Recipients, "rogue-broker")
		if err := c.Validate(); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("Validate(rewritten recipients) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a tampered log entry breaks chain verification", func(t *testing.T) {
		boundary := fixtureFTIBoundary(t)
		boundary.Log.Entries[0].Recipient = "rogue-broker"
		if err := boundary.Log.Verify(); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("Verify(tampered entry) = %v, want %v", err, ErrFTIBlocked)
		}
		if err := VerifyBoundary(boundary, mustInstant(t, fxFTIReviewedAt)); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("VerifyBoundary(tampered log) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a log bound to another activity never verifies as this boundary", func(t *testing.T) {
		boundary := fixtureFTIBoundary(t)
		otherActivity := fixtureFTIActivity(t)
		otherActivity.ID = "state-tax-processing"
		other, err := ClassifyFTI(otherActivity)
		if err != nil {
			t.Fatalf("ClassifyFTI (other activity): %v", err)
		}
		foreign, err := NewFTIDisclosureLog(other).Append(FTIDisclosureSpec{
			DisclosureID: "fti-disclosure-x", At: mustInstant(t, fxFTIDisclosedAt),
			Recipient: "irs-mef", Authority: "pub1075-s9-disclosure-auth-2026-001",
			PayloadDigest: fxFTIPayloadDigest,
		})
		if err != nil {
			t.Fatalf("Append (foreign log): %v", err)
		}
		boundary.Log = foreign // a well-formed log, but for another activity
		if err := VerifyBoundary(boundary, mustInstant(t, fxFTIReviewedAt)); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("VerifyBoundary(foreign log) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("a disclosure to an unapproved recipient is refused", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		log := NewFTIDisclosureLog(c)
		_, err := log.Append(FTIDisclosureSpec{
			DisclosureID: "fti-disclosure-rogue", At: mustInstant(t, fxFTIDisclosedAt),
			Recipient: "rogue-broker", Authority: "pub1075-s9-disclosure-auth-2026-001",
			PayloadDigest: fxFTIPayloadDigest,
		})
		if !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("Append(unapproved recipient) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("raw payload is refused: only a digest may stand in for disclosed data", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		log := NewFTIDisclosureLog(c)
		_, err := log.Append(FTIDisclosureSpec{
			DisclosureID: "fti-disclosure-raw", At: mustInstant(t, fxFTIDisclosedAt),
			Recipient: "irs-mef", Authority: "pub1075-s9-disclosure-auth-2026-001",
			PayloadDigest: "1040-John-Doe-123-45-6789",
		})
		if !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("Append(raw payload) = %v, want %v", err, ErrFTIBlocked)
		}
	})

	t.Run("local access below substantial assurance is denied", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		_, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: c, Principal: "tax-admin-7",
			Assurance: trust.AssuranceLow, Remote: false,
			KeyScope: c.KeyScope, Purpose: "tax_return_processing",
		})
		if !errors.Is(err, ErrFTIAccessDenied) {
			t.Fatalf("AuthorizeFTIAccess(local, low) = %v, want %v", err, ErrFTIAccessDenied)
		}
	})

	t.Run("an unnamed principal or purpose is denied without an evidence trail", func(t *testing.T) {
		c := fixtureFTIClassification(t)
		for name, mutate := range map[string]func(*FTIAccessSpec){
			"empty principal": func(s *FTIAccessSpec) { s.Principal = "  " },
			"empty purpose":   func(s *FTIAccessSpec) { s.Purpose = "" },
		} {
			spec := FTIAccessSpec{
				Classification: c, Principal: "tax-admin-7",
				Assurance: trust.AssuranceHigh, Remote: true,
				KeyScope: c.KeyScope, Purpose: "tax_return_processing",
			}
			mutate(&spec)
			grant, err := AuthorizeFTIAccess(spec)
			if !errors.Is(err, ErrFTIAccessDenied) {
				t.Errorf("%s: AuthorizeFTIAccess = (%+v, %v), want %v", name, grant, err, ErrFTIAccessDenied)
			}
			if grant.EvidenceID != "" {
				t.Errorf("%s: a denied access still carries EvidenceID %q", name, grant.EvidenceID)
			}
		}
	})

	t.Run("no FTI payload or subject PII reaches the grant evidence", func(t *testing.T) {
		boundary := fixtureFTIBoundary(t)
		grant, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: boundary.Classification, Principal: "tax-admin-7",
			Assurance: trust.AssuranceHigh, Remote: true,
			KeyScope: boundary.Classification.KeyScope, Purpose: "tax_return_processing",
		})
		if err != nil {
			t.Fatalf("AuthorizeFTIAccess: %v", err)
		}
		for _, field := range []string{grant.EvidenceID, grant.Principal, grant.Purpose} {
			if strings.Contains(strings.ToLower(field), "1040") || strings.Contains(field, "123-45") {
				t.Errorf("grant evidence field %q carries payload-shaped data", field)
			}
		}
	})
}
