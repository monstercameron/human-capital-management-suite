package privacy

import (
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PRIV_002 is the PRIMARY test for planning/todos.md PRIV-002:
// "Prove notice presentation and optional-processing authority."
//
// RED (todos.md PRIV-002): "missing/expired notice, unsupported
// locale/accessibility presentation or withdrawn authority permits optional
// AI/RAG/analytics/communication use."
//
// GREEN (todos.md PRIV-002): "presentation/acknowledgement binds exact
// version/language/scope; withdrawal returns PROCESSING_BLOCKED and creates
// applicable restrict/delete obligations."
func TestTodo_PRIV_002(t *testing.T) {
	t.Run("GREEN: complete evidence chain authorizes and binds exact version/locale/scope", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		got := EvaluateAuthority(in)

		if !got.Allowed || got.Code != AuthorityAllowed {
			t.Fatalf("EvaluateAuthority(complete chain) = %+v, want Allowed=true Code=%s", got, AuthorityAllowed)
		}
		if got.NoticeVersion != in.Notice.Version {
			t.Errorf("decision.NoticeVersion = %q, want %q (bound to the presented notice version)", got.NoticeVersion, in.Notice.Version)
		}
		if got.Locale != in.Presentation.Locale {
			t.Errorf("decision.Locale = %q, want %q (bound to the presentation's actual locale)", got.Locale, in.Presentation.Locale)
		}
		if !slices.Equal(got.Scope, in.Consent.Scope) {
			t.Errorf("decision.Scope = %v, want %v (bound to the consent's actual scope)", got.Scope, in.Consent.Scope)
		}
		if len(got.Obligations) != 0 {
			t.Errorf("an ALLOWED decision should carry no obligations, got %v", got.Obligations)
		}
		if got.InputsDigest == "" || got.EvidenceID == "" {
			t.Errorf("decision carries no digest/evidence id: %+v", got)
		}
	})

	t.Run("RED: no presentation ever recorded denies", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		in.Presentation = nil
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityNoticeNotPresented {
			t.Fatalf("EvaluateAuthority(no presentation) = %+v, want denied with %s", got, AuthorityNoticeNotPresented)
		}
	})

	t.Run("RED: expired notice denies even with full presentation/consent evidence", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		in.EffectiveAt = mustInstant(t, fxNoticeEffectiveTo+1) // past EffectiveTo
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityNoticeExpired {
			t.Fatalf("EvaluateAuthority(expired notice) = %+v, want denied with %s", got, AuthorityNoticeExpired)
		}
	})

	t.Run("RED: notice not yet effective denies", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		in.EffectiveAt = mustInstant(t, fxNoticeEffectiveFrom-1)
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityNoticeExpired {
			t.Fatalf("EvaluateAuthority(not-yet-effective notice) = %+v, want denied with %s", got, AuthorityNoticeExpired)
		}
	})

	t.Run("RED: unsupported locale presentation denies", func(t *testing.T) {
		notice := fixtureNotice(t)
		presentation, err := NewPresentation("presentation-fr", notice, fixturePrincipal, "fr-FR", true,
			mustInstant(t, fxPresentedAt), mustInstant(t, fxAcknowledgedAt))
		if err != nil {
			t.Fatalf("NewPresentation: %v", err)
		}
		consent := fixtureConsent(t, presentation.ID)
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &presentation, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityUnsupportedLocale {
			t.Fatalf("EvaluateAuthority(unsupported locale) = %+v, want denied with %s", got, AuthorityUnsupportedLocale)
		}
	})

	t.Run("RED: inaccessible presentation channel denies", func(t *testing.T) {
		notice := fixtureNotice(t)
		presentation, err := NewPresentation("presentation-inaccessible", notice, fixturePrincipal, "en-US", false,
			mustInstant(t, fxPresentedAt), mustInstant(t, fxAcknowledgedAt))
		if err != nil {
			t.Fatalf("NewPresentation: %v", err)
		}
		consent := fixtureConsent(t, presentation.ID)
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &presentation, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityPresentationNotA11y {
			t.Fatalf("EvaluateAuthority(inaccessible presentation) = %+v, want denied with %s", got, AuthorityPresentationNotA11y)
		}
	})

	t.Run("RED: withdrawn consent returns PROCESSING_BLOCKED with restrict/delete obligations", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		withdrawn, err := in.Consent.Withdraw(mustInstant(t, fxEvaluateAt-1))
		if err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		in.Consent = &withdrawn

		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityProcessingBlocked {
			t.Fatalf("EvaluateAuthority(withdrawn consent) = %+v, want denied with %s", got, AuthorityProcessingBlocked)
		}
		want := []string{"REQUIRE_DELETION", "RESTRICT_PROCESSING"}
		if !slices.Equal(got.Obligations, want) {
			t.Errorf("withdrawn-consent obligations = %v, want %v", got.Obligations, want)
		}
	})

	t.Run("RED: consent granted for a different purpose denies (out of scope)", func(t *testing.T) {
		in := fixtureAuthorityInput(t)
		in.Purpose = "marketing_email" // fixture consent only covers ai_assist/analytics
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityPurposeOutOfScope {
			t.Fatalf("EvaluateAuthority(out-of-scope purpose) = %+v, want denied with %s", got, AuthorityPurposeOutOfScope)
		}
	})

	t.Run("RED: expired consent denies", func(t *testing.T) {
		notice := fixtureNotice(t)
		presentation := fixturePresentation(t, notice)
		consent, err := NewOptionalProcessing("consent-expiring", fixturePrincipal, []string{"ai_assist"},
			presentation.ID, mustInstant(t, fxGrantedAt), mustInstant(t, fxEvaluateAt-1))
		if err != nil {
			t.Fatalf("NewOptionalProcessing: %v", err)
		}
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &presentation, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityConsentExpired {
			t.Fatalf("EvaluateAuthority(expired consent) = %+v, want denied with %s", got, AuthorityConsentExpired)
		}
	})

	t.Run("REFACTOR: a mandatory-basis notice is never authorized through consent, even with full evidence", func(t *testing.T) {
		notice := fixtureMandatoryNotice(t)
		presentation, err := NewPresentation("presentation-mandatory", notice, fixturePrincipal, "en-US", true,
			mustInstant(t, fxPresentedAt), mustInstant(t, fxAcknowledgedAt))
		if err != nil {
			t.Fatalf("NewPresentation: %v", err)
		}
		consent, err := NewOptionalProcessing("consent-mandatory", fixturePrincipal, []string{"tax_reporting"},
			presentation.ID, mustInstant(t, fxGrantedAt), values.Instant{})
		if err != nil {
			t.Fatalf("NewOptionalProcessing: %v", err)
		}
		in := AuthorityInput{
			Purpose: "tax_reporting", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &presentation, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityMandatoryBasisNotConsent {
			t.Fatalf("EvaluateAuthority(mandatory notice, full evidence) = %+v, want denied with %s", got, AuthorityMandatoryBasisNotConsent)
		}
	})
}
