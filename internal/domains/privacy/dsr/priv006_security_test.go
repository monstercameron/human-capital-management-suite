package dsr

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_006_Security is the SECURITY matrix test for PRIV-006:
// resolution must never cross a tenant or subject boundary, never emit a
// certificate from a request that cannot advance, never honor an
// authority-less exception, and never leak subject PII into its evidence.
func TestTodo_PRIV_006_Security(t *testing.T) {
	t.Run("a cross-tenant copy is refused and emits no certificate", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-sec-tenant", KindAccess, trust.AssuranceSubstantial)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative})
		spec.Copies[0].Tenant = fxOtherTenant
		res, err := Resolve(spec)
		if !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(cross-tenant copy) = (%+v, %v), want %v", res, err, ErrResolutionBlocked)
		}
		if res.EvidenceID != "" {
			t.Errorf("a refused resolution still carries EvidenceID %q", res.EvidenceID)
		}
	})

	t.Run("a copy about another subject is refused", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-sec-subject", KindAccess, trust.AssuranceSubstantial)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative})
		spec.Copies[0].SubjectKey = "someone.else@example.com"
		if _, err := Resolve(spec); !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(cross-subject copy) = %v, want %v", err, ErrResolutionBlocked)
		}
	})

	t.Run("a forged VERIFIED record below its kind's floor is refused", func(t *testing.T) {
		// Bypass Verify entirely: a forged or mis-migrated record claiming
		// VERIFIED with low assurance must be refused by Resolve's own
		// CanAdvance gate, not just by Verify upstream.
		req := fixtureRequest(t) // KindAccess: floor is AssuranceSubstantial
		req.VerificationState = VerificationVerified
		req.IdentityEvidenceRef = "ev:forged"
		req.IdentityAssurance = trust.AssuranceLow
		req.VerifiedAt = mustInstant(t, fxVerifiedAt)
		req = req.withEvidenceID()
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative})
		if _, err := Resolve(spec); !errors.Is(err, ErrResolutionRefused) {
			t.Fatalf("Resolve(forged low-assurance record) = %v, want %v", err, ErrResolutionRefused)
		}
	})

	t.Run("an access-redacting exception partially fulfills with its reason and a DPO appeal route", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-sec-redact", KindAccess, trust.AssuranceSubstantial)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative, CopyExport})
		spec.Exceptions = []LegalException{{
			CopyIDs:       []string{"copy-export"},
			Authority:     "in-re-matter-4417",
			Basis:         ExceptionLitigation,
			RedactsAccess: true,
		}}
		res, err := Resolve(spec)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		byID := resolutionByID(res)
		redacted := byID["copy-export"]
		if redacted.Outcome != OutcomePartial {
			t.Errorf("redacted copy outcome = %s, want %s", redacted.Outcome, OutcomePartial)
		}
		if redacted.RedactionReason != RedactionPrivilege {
			t.Errorf("redacted copy reason = %s, want %s", redacted.RedactionReason, RedactionPrivilege)
		}
		if redacted.AppealRoute != AppealDPO {
			t.Errorf("redacted copy appeal = %s, want %s", redacted.AppealRoute, AppealDPO)
		}
		if byID["copy-authoritative"].Outcome != OutcomeFulfill {
			t.Errorf("unexcepted copy outcome = %s, want %s", byID["copy-authoritative"].Outcome, OutcomeFulfill)
		}
	})

	t.Run("no subject PII reaches the certificate's evidence fields", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-sec-pii", KindErasure, trust.AssuranceHigh)
		res, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		for _, item := range res.Items {
			for _, field := range []string{item.Authority, item.Detail, string(item.AppealRoute)} {
				if strings.Contains(strings.ToLower(field), "alex.example") {
					t.Errorf("item %q evidence field %q leaks subject PII", item.CopyID, field)
				}
			}
		}
	})

	t.Run("a hand-edited certificate no longer validates", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-sec-tamper", KindErasure, trust.AssuranceHigh)
		res, err := Resolve(fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		res.Items[0].Outcome = OutcomeRetain // attacker flips FULFILL to RETAIN
		if err := res.Validate(); err == nil {
			t.Fatal("Validate(tampered outcome) succeeded, want a digest-mismatch failure")
		}
	})
}
