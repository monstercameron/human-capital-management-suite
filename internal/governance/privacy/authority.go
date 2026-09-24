package privacy

import (
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AuthorityCode is a stable, uniform reason token an [AuthorityDecision]
// carries in place of a raw internal error. [AuthorityProcessingBlocked] is
// the one code PRIV-002's GREEN clause names explicitly for a withdrawn
// consent; the rest are this package's own deny-by-default reasons for every
// other way notice/consent evidence can be incomplete.
type AuthorityCode string

// Authority decision codes.
const (
	AuthorityAllowed AuthorityCode = "ALLOWED"

	// AuthorityMandatoryBasisNotConsent is returned unconditionally for a
	// [Notice] with Mandatory set: this function never authorizes mandatory
	// legal-basis processing, regardless of evidence quality.
	AuthorityMandatoryBasisNotConsent AuthorityCode = "MANDATORY_BASIS_NOT_CONSENT_GOVERNED"

	AuthorityNoticeInvalid        AuthorityCode = "NOTICE_INVALID"
	AuthorityNoticeExpired        AuthorityCode = "NOTICE_EXPIRED"
	AuthorityNoticeNotPresented   AuthorityCode = "NOTICE_NOT_PRESENTED"
	AuthorityPresentationInvalid  AuthorityCode = "PRESENTATION_INVALID"
	AuthorityNoticeVersionStale   AuthorityCode = "NOTICE_VERSION_MISMATCH"
	AuthorityPresentationNotAck   AuthorityCode = "PRESENTATION_NOT_ACKNOWLEDGED"
	AuthorityUnsupportedLocale    AuthorityCode = "UNSUPPORTED_LOCALE"
	AuthorityPresentationNotA11y  AuthorityCode = "PRESENTATION_NOT_ACCESSIBLE"
	AuthorityPresentationWrongWho AuthorityCode = "PRESENTATION_PRINCIPAL_MISMATCH"

	AuthorityConsentNotGranted  AuthorityCode = "CONSENT_NOT_GRANTED"
	AuthorityConsentInvalid     AuthorityCode = "CONSENT_INVALID"
	AuthorityConsentWrongWho    AuthorityCode = "CONSENT_PRINCIPAL_MISMATCH"
	AuthorityConsentWrongNotice AuthorityCode = "CONSENT_PRESENTATION_MISMATCH"
	AuthorityPurposeOutOfScope  AuthorityCode = "PURPOSE_OUT_OF_SCOPE"
	AuthorityConsentExpired     AuthorityCode = "CONSENT_EXPIRED"
	// AuthorityProcessingBlocked is PRIV-002's named GREEN-clause code for a
	// withdrawn consent: EvaluateAuthority returns it together with
	// restrict/delete obligations.
	AuthorityProcessingBlocked AuthorityCode = "PROCESSING_BLOCKED"
)

// AuthorityInput is everything [EvaluateAuthority] needs to decide whether
// one declared optional-processing purpose is authorized for one principal
// at one instant. Nothing is fetched by this package; the caller resolves
// the current Notice, Presentation and OptionalProcessing records and hands
// them in, exactly as
// [github.com/monstercameron/human-capital-management-suite/internal/trust/authz.Request] hands
// its own pre-resolved projections to authz.Evaluate.
type AuthorityInput struct {
	// Purpose is the optional-processing purpose being requested, e.g.
	// "ai_assist", "rag_retrieval", "analytics", "communication".
	Purpose     string
	Principal   string
	EffectiveAt values.Instant
	Notice      Notice
	// Presentation is the evidence that Notice was shown to Principal.
	// A nil Presentation always denies.
	Presentation *Presentation
	// Consent is the principal's optional-processing consent grant. A nil
	// Consent always denies.
	Consent *OptionalProcessing
	// SupportedLocales names the locales this notice's presentation channel
	// is accessible in. A Presentation.Locale absent from this list denies.
	SupportedLocales []string
}

// AuthorityDecision is the explainable, evidence-bearing result
// [EvaluateAuthority] returns. See the package doc for why its shape
// mirrors internal/trust/authz.Decision.
type AuthorityDecision struct {
	Allowed     bool
	Code        AuthorityCode
	Purpose     string
	Principal   string
	EvaluatedAt values.Instant
	// NoticeVersion, Locale and Scope are populated whenever a decision
	// reaches the point of having a Notice, a matched Presentation and a
	// matched Consent to read them from (nil/empty before that point,
	// since there is nothing yet to bind). Their presence on an ALLOWED
	// decision is PRIV-002's GREEN clause made concrete: "presentation/
	// acknowledgement binds exact version/language/scope" is not a prose
	// claim here, it is these three fields actually being populated from
	// the evidence records EvaluateAuthority matched.
	NoticeVersion string
	Locale        string
	Scope         []string
	// Obligations is the sorted set of obligation tokens the decision
	// attaches. A withdrawn consent's PROCESSING_BLOCKED decision always
	// carries "RESTRICT_PROCESSING" and "REQUIRE_DELETION" here.
	Obligations []string
	// InputsDigest is a canonical digest over every input this decision was
	// computed from. Two calls to [EvaluateAuthority] with the same inputs
	// always produce the same digest; any difference in input produces a
	// different one.
	InputsDigest string
	// EvidenceID is the durable identifier for this decision's evidence
	// record, derived from InputsDigest.
	EvidenceID string
}

// Explain renders a redaction-safe, deterministic summary of the decision,
// in the same spirit as authz.Decision.Explain: reason tokens and counts
// only, never a reproduced notice/consent value.
func (d AuthorityDecision) Explain() string {
	return fmt.Sprintf("privacy authority decision purpose=%s allowed=%v code=%s obligations=%v evidence=%s",
		d.Purpose, d.Allowed, d.Code, d.Obligations, d.EvidenceID)
}

func localeSupported(locale string, supported []string) bool {
	return slices.Contains(supported, locale)
}

// canonicalAuthorityInputsDigest hashes every input the decision is computed
// from. It is deliberately independent of the Allowed/Code result: two
// requests with identical inputs always hash identically regardless of
// which branch of EvaluateAuthority produced the answer.
func canonicalAuthorityInputsDigest(in AuthorityInput) string {
	presPart := "none"
	if in.Presentation != nil {
		presPart = in.Presentation.canonicalDigest()
	}
	consentPart := "none"
	if in.Consent != nil {
		consentPart = in.Consent.canonicalDigest()
	}
	dst := appendFields(nil,
		"purpose", in.Purpose,
		"principal", in.Principal,
		"effective_at", in.EffectiveAt.String(),
		"notice_digest", in.Notice.Digest(),
		"presentation_digest", presPart,
		"consent_digest", consentPart,
	)
	dst = appendStringSlice(dst, "supported_locale", in.SupportedLocales)
	return digestHex(dst)
}

func finish(in AuthorityInput, allowed bool, code AuthorityCode, obligations []string) AuthorityDecision {
	digest := canonicalAuthorityInputsDigest(in)
	sortedObligations := slices.Clone(obligations)
	slices.Sort(sortedObligations)

	locale := ""
	if in.Presentation != nil {
		locale = in.Presentation.Locale
	}
	var scope []string
	if in.Consent != nil {
		scope = slices.Clone(in.Consent.Scope)
	}

	return AuthorityDecision{
		Allowed:       allowed,
		Code:          code,
		Purpose:       in.Purpose,
		Principal:     in.Principal,
		EvaluatedAt:   in.EffectiveAt,
		NoticeVersion: in.Notice.Version,
		Locale:        locale,
		Scope:         scope,
		Obligations:   sortedObligations,
		InputsDigest:  digest,
		EvidenceID:    "ev:privacy:authority:" + digest,
	}
}

func deny(in AuthorityInput, code AuthorityCode, obligations ...string) AuthorityDecision {
	return finish(in, false, code, obligations)
}

// EvaluateAuthority is the PRIV-002 authority evaluation function: deny by
// default, and allow only when every one of the following holds at
// in.EffectiveAt:
//
//   - in.Notice is not Mandatory (mandatory legal basis is never
//     consent-governed by this function);
//   - in.Notice is valid and currently in force;
//   - in.Presentation is present, internally valid (including tamper
//     evidence -- see [Presentation.Validate]), bound to exactly in.Notice's
//     current id/version/digest, shown to in.Principal, in a supported
//     locale, through an accessible channel;
//   - in.Consent is present, internally valid, bound to in.Presentation,
//     granted to in.Principal, in scope for in.Purpose, and currently
//     GRANTED (neither expired nor withdrawn).
//
// A withdrawn consent returns [AuthorityProcessingBlocked] with
// "RESTRICT_PROCESSING" and "REQUIRE_DELETION" obligations attached, per
// PRIV-002's GREEN clause. Every other failure returns a more specific deny
// code but carries no obligation: only a withdrawal creates a downstream
// restrict/delete obligation, not a routine absence of evidence.
func EvaluateAuthority(in AuthorityInput) AuthorityDecision {
	if in.Notice.Mandatory {
		return deny(in, AuthorityMandatoryBasisNotConsent)
	}
	if err := in.Notice.Validate(); err != nil {
		return deny(in, AuthorityNoticeInvalid)
	}
	if !in.Notice.ActiveAt(in.EffectiveAt) {
		return deny(in, AuthorityNoticeExpired)
	}
	if in.Presentation == nil {
		return deny(in, AuthorityNoticeNotPresented)
	}
	if err := in.Presentation.Validate(); err != nil {
		return deny(in, AuthorityPresentationInvalid)
	}
	if !in.Presentation.MatchesNotice(in.Notice) {
		return deny(in, AuthorityNoticeVersionStale)
	}
	if in.Presentation.Principal != in.Principal {
		return deny(in, AuthorityPresentationWrongWho)
	}
	if in.Presentation.PresentedAt.After(in.EffectiveAt) {
		return deny(in, AuthorityNoticeNotPresented)
	}
	if !in.Presentation.AcknowledgedAt.IsSet() || in.Presentation.AcknowledgedAt.After(in.EffectiveAt) {
		return deny(in, AuthorityPresentationNotAck)
	}
	if !localeSupported(in.Presentation.Locale, in.SupportedLocales) {
		return deny(in, AuthorityUnsupportedLocale)
	}
	if !in.Presentation.Accessible {
		return deny(in, AuthorityPresentationNotA11y)
	}
	if in.Consent == nil {
		return deny(in, AuthorityConsentNotGranted)
	}
	if err := in.Consent.Validate(); err != nil {
		return deny(in, AuthorityConsentInvalid)
	}
	if in.Consent.Principal != in.Principal {
		return deny(in, AuthorityConsentWrongWho)
	}
	if in.Consent.PresentationID != in.Presentation.ID {
		return deny(in, AuthorityConsentWrongNotice)
	}
	if in.Notice.Purpose != in.Purpose {
		return deny(in, AuthorityPurposeOutOfScope)
	}
	if !in.Consent.HasScope(in.Purpose) {
		return deny(in, AuthorityPurposeOutOfScope)
	}
	switch in.Consent.StatusAt(in.EffectiveAt) {
	case ConsentStatusWithdrawn:
		return deny(in, AuthorityProcessingBlocked, "RESTRICT_PROCESSING", "REQUIRE_DELETION")
	case ConsentStatusExpired:
		return deny(in, AuthorityConsentExpired)
	case ConsentStatusGranted:
		return finish(in, true, AuthorityAllowed, nil)
	default:
		return deny(in, AuthorityConsentNotGranted)
	}
}
