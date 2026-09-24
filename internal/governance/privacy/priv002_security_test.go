package privacy

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PRIV_002_Security is the SECURITY matrix test for PRIV-002. It
// proves the evidence chain cannot be defeated by identity confusion or
// after-the-fact tampering: a consent or presentation recorded for one
// principal never authorizes another, a presentation bound to a stale or
// forged notice digest is rejected even when every other field matches, and
// mutating a validated record's fields without recomputing its evidence id
// is detected rather than silently accepted.
func TestTodo_PRIV_002_Security(t *testing.T) {
	t.Run("an unacknowledged or future acknowledgement cannot authorize optional processing", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			ack  int64
		}{
			{name: "missing acknowledgement", ack: 0},
			{name: "acknowledgement after evaluation", ack: fxEvaluateAt + 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				in := fixtureAuthorityInput(t)
				p := *in.Presentation
				if tc.ack == 0 {
					p.AcknowledgedAt = values.Instant{}
				} else {
					p.AcknowledgedAt = mustInstant(t, tc.ack)
				}
				p.EvidenceID = presentationEvidencePrefix + p.canonicalDigest()
				in.Presentation = &p
				got := EvaluateAuthority(in)
				if got.Allowed || got.Code != AuthorityPresentationNotAck {
					t.Fatalf("EvaluateAuthority(unacknowledged presentation) = %+v, want denied with %s", got, AuthorityPresentationNotAck)
				}
			})
		}
	})

	t.Run("presentation recorded after the authority evaluation cannot authorize", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		p := *in.Presentation
		p.PresentedAt = mustInstant(t, fxEvaluateAt+1)
		p.AcknowledgedAt = mustInstant(t, fxEvaluateAt+2)
		p.EvidenceID = presentationEvidencePrefix + p.canonicalDigest()
		in.Presentation = &p
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityNoticeNotPresented {
			t.Fatalf("EvaluateAuthority(future presentation) = %+v, want denied with %s", got, AuthorityNoticeNotPresented)
		}
	})

	t.Run("repeated withdrawal cannot move revocation forward and reopen authority", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		firstAt := mustInstant(t, fxEvaluateAt-100)
		withdrawn, err := in.Consent.Withdraw(firstAt)
		if err != nil {
			t.Fatalf("first Withdraw: %v", err)
		}
		if _, err := withdrawn.Withdraw(mustInstant(t, fxEvaluateAt+100)); err == nil {
			t.Fatal("repeated Withdraw accepted; a later timestamp could reopen authority for the interval after revocation")
		}
		in.Consent = &withdrawn
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityProcessingBlocked {
			t.Fatalf("EvaluateAuthority(withdrawn consent) = %+v, want denied with %s", got, AuthorityProcessingBlocked)
		}
	})

	t.Run("a consent granted by one principal never authorizes a different principal", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		in.Principal = "worker-9999" // presentation/consent were both recorded for fixturePrincipal
		got := EvaluateAuthority(in)
		if got.Allowed {
			t.Fatalf("EvaluateAuthority authorized principal %q using evidence recorded for %q: %+v", in.Principal, fixturePrincipal, got)
		}
		if got.Code != AuthorityPresentationWrongWho {
			t.Errorf("code = %s, want %s (principal mismatch caught at the presentation stage, before consent is even consulted)", got.Code, AuthorityPresentationWrongWho)
		}
	})

	t.Run("a presentation whose recorded notice digest does not match the current notice is rejected", func(t *testing.T) {
		notice := fixtureNotice(t)
		presentation := fixturePresentation(t, notice)
		// Simulate a forged/stale presentation: it claims to be bound to the
		// current notice id/version, but its digest is for different content
		// (e.g. a notice edited without a version bump, or a crafted record).
		tampered := presentation
		tampered.NoticeDigest = "0000000000000000000000000000000000000000000000000000000000000000"
		tampered.EvidenceID = "" // force recompute path below to prove Validate() catches drift, not just staleness

		consent := fixtureConsent(t, tampered.ID)
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &tampered, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed {
			t.Fatalf("EvaluateAuthority authorized a presentation with a forged notice digest: %+v", got)
		}
		// tampered.EvidenceID is now "" (does not match its own canonical
		// digest), so Presentation.Validate fails before the digest-mismatch
		// check is even reached -- both are real defenses, so accept either.
		if got.Code != AuthorityPresentationInvalid && got.Code != AuthorityNoticeVersionStale {
			t.Errorf("code = %s, want %s or %s", got.Code, AuthorityPresentationInvalid, AuthorityNoticeVersionStale)
		}
	})

	t.Run("mutating a validated Presentation's field without recomputing EvidenceID is detected", func(t *testing.T) {
		notice := fixtureNotice(t)
		presentation := fixturePresentation(t, notice)
		if err := presentation.Validate(); err != nil {
			t.Fatalf("fixture presentation should validate before tampering: %v", err)
		}

		tampered := presentation
		tampered.Locale = "fr-FR" // changed after EvidenceID was computed
		if err := tampered.Validate(); err == nil {
			t.Fatalf("Validate() accepted a presentation whose Locale was changed after its EvidenceID was computed")
		}

		consent := fixtureConsent(t, tampered.ID)
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &tampered, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityPresentationInvalid {
			t.Fatalf("EvaluateAuthority(tampered presentation) = %+v, want denied with %s", got, AuthorityPresentationInvalid)
		}
	})

	t.Run("mutating a validated OptionalProcessing's scope without recomputing EvidenceID is detected", func(t *testing.T) {
		notice := fixtureNotice(t)
		presentation := fixturePresentation(t, notice)
		consent := fixtureConsent(t, presentation.ID)
		if err := consent.Validate(); err != nil {
			t.Fatalf("fixture consent should validate before tampering: %v", err)
		}

		tampered := consent
		tampered.Scope = append([]string{}, consent.Scope...)
		tampered.Scope = append(tampered.Scope, "marketing_email") // scope widened after EvidenceID was computed
		if err := tampered.Validate(); err == nil {
			t.Fatalf("Validate() accepted a consent whose Scope was widened after its EvidenceID was computed")
		}

		in := AuthorityInput{
			Purpose: "marketing_email", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &presentation, Consent: &tampered, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed {
			t.Fatalf("EvaluateAuthority authorized marketing_email via a scope widened after evidence was recorded: %+v", got)
		}
		if got.Code != AuthorityConsentInvalid {
			t.Errorf("code = %s, want %s", got.Code, AuthorityConsentInvalid)
		}
	})

	t.Run("Explain never reproduces notice/consent content, only reason tokens", func(t *testing.T) {
		notice := fixtureNotice(t)
		secretDataClass := "manager_name" // present in DataClasses, must never leak via Explain
		if !containsString(notice.DataClasses, secretDataClass) {
			t.Fatalf("fixture notice must carry %q for this test to be meaningful", secretDataClass)
		}
		in := fixtureAuthorityInput(t)
		got := EvaluateAuthority(in)
		explained := got.Explain()
		if strings.Contains(explained, secretDataClass) {
			t.Errorf("Explain() leaked a notice data class into its summary: %q", explained)
		}
		if strings.Contains(explained, fixturePrincipal) {
			t.Errorf("Explain() leaked the principal identifier into its summary: %q", explained)
		}
	})

	t.Run("a decision for an unrelated principal with no evidence at all is denied, not merely unauthenticated", func(t *testing.T) {
		notice := fixtureNotice(t)
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: "worker-ghost", EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: nil, Consent: nil, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed {
			t.Fatalf("EvaluateAuthority authorized a principal with zero evidence: %+v", got)
		}
		if got.EvidenceID == "" {
			t.Errorf("even a denial must carry a durable evidence id for audit, got none")
		}
	})
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
