package legal

import (
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrLegalContextUnknown is returned by [Resolve] whenever the supplied facts
// do not add up to one explicit jurisdiction governed by at least one
// registered rule-pack release as of the stated effective date. It is the one
// failure mode for every missing or contradictory input: a missing legal
// entity, a missing or contradictory work location, a missing employment
// jurisdiction, a missing effective or known time, and an unregistered rule
// release all report this same error, wrapped with the specific reason.
//
// [Resolve] never substitutes a default jurisdiction for LEGAL_CONTEXT_UNKNOWN;
// callers that receive it must route to human/legal review rather than guess.
var ErrLegalContextUnknown = errors.New("legal: LEGAL_CONTEXT_UNKNOWN")

// Confidence records how certain a resolved [LegalContext] is about the
// jurisdiction it resolved to. It never blocks [Resolve] itself — an
// ambiguous or unknown jurisdiction fails closed with [ErrLegalContextUnknown]
// long before a Confidence value exists — but it travels with a successfully
// resolved context so a capability whose legal contract requires certainty
// can refuse to rely on an asserted-but-unverified jurisdiction.
type Confidence uint8

// Confidence levels. The zero value is never carried by a resolved context;
// [Resolve] always sets one of the two concrete levels below.
const (
	// ConfidenceUnspecified is the zero value and is never legal on a
	// resolved context.
	ConfidenceUnspecified Confidence = iota
	// ConfidenceVerified means the worker's work location and the employer's
	// asserted employment jurisdiction agreed, or the transaction is not
	// remote work, so no cross-check was needed.
	ConfidenceVerified
	// ConfidenceAsserted means the resolution relied on the declared
	// remote-work policy (physical work location controls) where the
	// employer's separately asserted employment jurisdiction disagreed. The
	// jurisdiction is still resolved, not ambiguous, but the disagreement is
	// preserved as evidence.
	ConfidenceAsserted
)

var confidenceWire = map[Confidence]string{
	ConfidenceVerified: "VERIFIED",
	ConfidenceAsserted: "ASSERTED",
}

// String returns the stable wire token.
func (c Confidence) String() string {
	if w, ok := confidenceWire[c]; ok {
		return w
	}
	return "CONFIDENCE_UNSPECIFIED"
}

// Provenance records which facts and policy contributed to a resolved
// jurisdiction, so a reviewer can see why a LegalContext resolved the way it
// did instead of treating the resolution as unexplained system magic.
type Provenance struct {
	// WorkLocationBasis names the source of the work-location fact, e.g.
	// "worker_assignment.work_location".
	WorkLocationBasis string
	// EmploymentJurisdictionBasis names the source of the asserted employment
	// jurisdiction, e.g. "employer.designated_employment_jurisdiction".
	EmploymentJurisdictionBasis string
	// RemoteWorkPolicyApplied names the remote-work rule that was applied to
	// pick between work location and employment jurisdiction, e.g.
	// "physical_work_location_controls" or "not_remote".
	RemoteWorkPolicyApplied string
}

func (p Provenance) canonicalBytes(dst []byte) []byte {
	dst = appendField(dst, "work_location_basis", p.WorkLocationBasis)
	dst = appendField(dst, "employment_jurisdiction_basis", p.EmploymentJurisdictionBasis)
	dst = appendField(dst, "remote_work_policy_applied", p.RemoteWorkPolicyApplied)
	return dst
}

// LegalContextInput is the caller-supplied facts [Resolve] needs. Every field
// is a fact about the transaction, never a conclusion: Resolve derives the
// jurisdiction itself and refuses when the facts do not add up to exactly
// one.
type LegalContextInput struct {
	// Locale is a presentation hint only (e.g. "en-US" for UI language). It
	// is carried here purely so a caller cannot claim locale was unavailable;
	// Resolve never reads it to derive a jurisdiction. See
	// planning/specs/platform-architecture-catalog.md: "Locale resolution is
	// concern-specific... legal authority comes only from LegalContext... :
	// locale never selects law."
	Locale string
	// LegalEntityID identifies the employer legal entity the worker is
	// employed by. Required.
	LegalEntityID string
	// WorkLocation is the worker's physical work location jurisdiction.
	// Required.
	WorkLocation Jurisdiction
	// EmploymentJurisdiction is the jurisdiction the employer's records
	// assert governs the employment relationship. Required. For most
	// employment relationships this equals WorkLocation; the two are kept
	// distinct because a remote-work arrangement can make them disagree.
	EmploymentJurisdiction Jurisdiction
	// RemoteWork declares whether the worker performs the role remotely from
	// WorkLocation rather than at an employer-designated worksite in
	// EmploymentJurisdiction.
	RemoteWork bool
	// EffectiveDate is the business effective date of the transaction this
	// context will govern. Required.
	EffectiveDate values.LocalDate
	// KnownAt is the earliest instant these facts were available to HCM
	// Next. Required; see values.KnownAt.
	KnownAt values.KnownAt
	// FailClosedOnUnregisteredLocality applies the tenant's configured policy.
	// The default records the locality for receipt evidence and continues with
	// the exact registered subdivision release.
	FailClosedOnUnregisteredLocality bool
}

// RulePackRelease pins one applicable [RulePack] version. A [LegalContext]
// records the exact releases it resolved against so that a later republish of
// the same pack ID never silently changes which law a historical context
// claims to have evaluated under.
type RulePackRelease struct {
	PackID  string
	Version uint32
	// MinorVersion is the minor half of the contract's major.minor release
	// version. It is zero for every release LEGAL-001 pinned, and it is part
	// of the registration slot: a minor bump is a separate release, so a
	// pinned context keeps resolving to exactly the content it evaluated
	// under.
	MinorVersion uint32
	Jurisdiction Jurisdiction
}

// LegalContext is the resolved, immutable, signed jurisdiction and rule-pack
// binding for one transaction. It is never constructed directly; the only
// door is [Resolve], and every field is set once and never mutated
// afterwards.
type LegalContext struct {
	legalEntityID                    string
	workLocation                     Jurisdiction
	employmentJurisdiction           Jurisdiction
	remoteWork                       bool
	jurisdiction                     Jurisdiction
	effectiveDate                    values.LocalDate
	knownAt                          values.KnownAt
	recordedAt                       values.RecordedAt
	releases                         []RulePackRelease
	unregisteredLocalities           []Jurisdiction
	failClosedOnUnregisteredLocality bool
	attributionRule                  AttributionRule
	provenance                       Provenance
	confidence                       Confidence
	digest                           string
	signature                        Signature
}

// LegalEntityID returns the employer legal entity identifier.
func (c *LegalContext) LegalEntityID() string { return c.legalEntityID }

// WorkLocation returns the worker's physical work-location jurisdiction fact.
func (c *LegalContext) WorkLocation() Jurisdiction { return c.workLocation }

// EmploymentJurisdiction returns the employer-asserted employment
// jurisdiction fact.
func (c *LegalContext) EmploymentJurisdiction() Jurisdiction { return c.employmentJurisdiction }

// RemoteWork reports whether the transaction was resolved as remote work.
func (c *LegalContext) RemoteWork() bool { return c.remoteWork }

// Jurisdiction returns the resolved jurisdiction hierarchy that governs the
// transaction.
func (c *LegalContext) Jurisdiction() Jurisdiction { return c.jurisdiction }

// EffectiveDate returns the business effective date this context governs.
func (c *LegalContext) EffectiveDate() values.LocalDate { return c.effectiveDate }

// KnownAt returns the earliest instant the resolving facts were known.
func (c *LegalContext) KnownAt() values.KnownAt { return c.knownAt }

// RecordedAt returns the instant this context was resolved and signed.
func (c *LegalContext) RecordedAt() values.RecordedAt { return c.recordedAt }

// RulePackReleases returns a copy of the applicable, version-pinned rule-pack
// releases this context resolved against.
func (c *LegalContext) RulePackReleases() []RulePackRelease { return slices.Clone(c.releases) }

// UnregisteredLocalities returns locality work facts for which no exact,
// effective locality release was registered at resolution time.
func (c *LegalContext) UnregisteredLocalities() []Jurisdiction {
	return slices.Clone(c.unregisteredLocalities)
}

// AttributionRule returns the exact A1-A6 rule used by jurisdiction resolution.
func (c *LegalContext) AttributionRule() AttributionRule {
	if c.attributionRule != "" {
		return c.attributionRule
	}
	if c.remoteWork {
		return AttributionA3
	}
	return AttributionA1
}

// Provenance returns the recorded resolution provenance.
func (c *LegalContext) Provenance() Provenance { return c.provenance }

// Confidence returns how certain the jurisdiction resolution is.
func (c *LegalContext) Confidence() Confidence { return c.confidence }

// Digest returns the lowercase hex sha256 digest over the context's own
// canonical encoding.
func (c *LegalContext) Digest() string { return c.digest }

// Signature returns the ed25519 signature evidence over Digest.
func (c *LegalContext) Signature() Signature {
	return Signature{PublicKey: slices.Clone(c.signature.PublicKey), Bytes: slices.Clone(c.signature.Bytes)}
}

// canonicalBytes returns the deterministic encoding this context's digest and
// signature are computed over. It excludes the digest and signature
// themselves, and it is unexported: the only way to obtain it that matters to
// a caller is through Digest/Signature/Verify.
func (c *LegalContext) canonicalBytes() []byte {
	var b []byte
	b = append(b, 0x4c, 0x43, 0x31) // "LC1" — LegalContext, encoding version 1.
	b = appendField(b, "legal_entity_id", c.legalEntityID)
	b = c.workLocation.CanonicalBytes(appendField(b, "work_location", ""))
	b = c.employmentJurisdiction.CanonicalBytes(appendField(b, "employment_jurisdiction", ""))
	b = appendFieldBool(b, "remote_work", c.remoteWork)
	b = c.jurisdiction.CanonicalBytes(appendField(b, "jurisdiction", ""))
	b = appendField(b, "effective_date", c.effectiveDate.String())
	b = appendField(b, "known_at", c.knownAt.String())
	b = appendField(b, "recorded_at", c.recordedAt.String())
	b = appendUint32Field(b, "release_count", uint32(len(c.releases)))
	for _, r := range c.releases {
		b = appendField(b, "release_pack_id", r.PackID)
		b = appendUint32Field(b, "release_version", r.Version)
		b = r.Jurisdiction.CanonicalBytes(appendField(b, "release_jurisdiction", ""))
	}
	if len(c.unregisteredLocalities) > 0 {
		b = appendUint32Field(b, "unregistered_locality_count", uint32(len(c.unregisteredLocalities)))
		for _, locality := range c.unregisteredLocalities {
			b = locality.CanonicalBytes(appendField(b, "unregistered_locality", ""))
		}
	}
	if c.failClosedOnUnregisteredLocality {
		b = appendFieldBool(b, "fail_closed_on_unregistered_locality", true)
	}
	if c.attributionRule != "" && c.attributionRule != AttributionA1 {
		b = appendField(b, "attribution_rule", string(c.attributionRule))
	}
	b = c.provenance.canonicalBytes(b)
	b = appendField(b, "confidence", c.confidence.String())
	return b
}

// Verify recomputes the context's canonical digest and checks the embedded
// ed25519 signature against it. It fails closed on any tampering: a changed
// field, a swapped digest, or a signature over different bytes are all
// rejected.
func (c *LegalContext) Verify() error {
	return verifyDigest(c.canonicalBytes(), c.digest, c.signature)
}

// VerifyWithKey behaves like [LegalContext.Verify] and additionally requires
// that the embedded public key equal trustedKey, byte for byte. Use this when
// checking a context received from outside this process: Verify alone only
// proves internal self-consistency, not that a trusted authority signed it.
func (c *LegalContext) VerifyWithKey(trustedKey []byte) error {
	if len(trustedKey) != len(c.signature.PublicKey) || string(trustedKey) != string(c.signature.PublicKey) {
		return fmt.Errorf("%w: embedded key does not match the trusted key", ErrSignatureInvalid)
	}
	return c.Verify()
}

// Resolve derives an explicit jurisdiction from input, looks up the rule-pack
// releases that govern it as of input.EffectiveDate, and returns a signed,
// immutable LegalContext. Any missing required fact, any unresolved or
// contradictory jurisdiction, or the absence of a registered rule-pack
// release for the resolved jurisdiction returns [ErrLegalContextUnknown]
// rather than proceeding with a guessed or partial context.
func Resolve(input LegalContextInput, registry *Registry, signer *Signer, now values.Instant) (*LegalContext, error) {
	if input.LegalEntityID == "" {
		return nil, fmt.Errorf("%w: legal entity is required", ErrLegalContextUnknown)
	}
	if err := input.WorkLocation.Validate(); err != nil {
		return nil, fmt.Errorf("%w: work location: %v", ErrLegalContextUnknown, err)
	}
	if err := input.EmploymentJurisdiction.Validate(); err != nil {
		return nil, fmt.Errorf("%w: employment jurisdiction: %v", ErrLegalContextUnknown, err)
	}
	if err := input.EffectiveDate.Validate(); err != nil {
		return nil, fmt.Errorf("%w: effective date: %v", ErrLegalContextUnknown, err)
	}
	if err := input.KnownAt.Instant().Validate(); err != nil {
		return nil, fmt.Errorf("%w: known_at: %v", ErrLegalContextUnknown, err)
	}
	if err := now.Validate(); err != nil {
		return nil, fmt.Errorf("%w: recorded_at clock: %v", ErrLegalContextUnknown, err)
	}
	if registry == nil {
		return nil, fmt.Errorf("%w: no rule-pack registry supplied", ErrLegalContextUnknown)
	}
	if signer == nil {
		return nil, fmt.Errorf("%w: no signer supplied", ErrLegalContextUnknown)
	}

	set, err := ResolveJurisdictionSet(JurisdictionSetInput{
		EmploymentJurisdiction: input.EmploymentJurisdiction,
		RemoteWork:             input.RemoteWork,
		WorkLocations:          []ScheduledWorkLocation{{Jurisdiction: input.WorkLocation, Share: 1}},
		EffectiveDate:          input.EffectiveDate,
	}, registry)
	if err != nil {
		return nil, err
	}
	if input.FailClosedOnUnregisteredLocality && len(set.UnregisteredLocalities) > 0 {
		return nil, fmt.Errorf("%w: no exact locality rule-pack release for %s as of %s", ErrLegalContextUnknown, set.UnregisteredLocalities[0], input.EffectiveDate)
	}
	resolved := set.Primary
	pack, err := registry.LookupExact(resolved, input.EffectiveDate)
	if err != nil {
		return nil, fmt.Errorf("%w: no applicable rule-pack release for %s as of %s: %v", ErrLegalContextUnknown, resolved, input.EffectiveDate, err)
	}
	releases := []RulePackRelease{pack.Release()}
	for _, overlay := range set.Overlays {
		overlayPack, lookupErr := registry.LookupExact(overlay, input.EffectiveDate)
		if lookupErr != nil {
			return nil, fmt.Errorf("%w: no applicable locality rule-pack release for %s as of %s: %v", ErrLegalContextUnknown, overlay, input.EffectiveDate, lookupErr)
		}
		releases = append(releases, overlayPack.Release())
	}
	confidence, policy := set.Confidence, set.RemoteWorkPolicyApplied

	recordedAt, err := values.NewRecordedAt(now)
	if err != nil {
		return nil, fmt.Errorf("%w: recorded_at: %v", ErrLegalContextUnknown, err)
	}
	if err := values.ValidateKnowledgeOrder(input.KnownAt, recordedAt, false); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLegalContextUnknown, err)
	}

	ctx := &LegalContext{
		legalEntityID:                    input.LegalEntityID,
		workLocation:                     input.WorkLocation,
		employmentJurisdiction:           input.EmploymentJurisdiction,
		remoteWork:                       input.RemoteWork,
		jurisdiction:                     resolved,
		effectiveDate:                    input.EffectiveDate,
		knownAt:                          input.KnownAt,
		recordedAt:                       recordedAt,
		releases:                         releases,
		unregisteredLocalities:           slices.Clone(set.UnregisteredLocalities),
		failClosedOnUnregisteredLocality: input.FailClosedOnUnregisteredLocality,
		attributionRule:                  set.AttributionRule,
		provenance: Provenance{
			WorkLocationBasis:           "input.work_location",
			EmploymentJurisdictionBasis: "input.employment_jurisdiction",
			RemoteWorkPolicyApplied:     policy,
		},
		confidence: confidence,
	}
	canonicalBytes := ctx.canonicalBytes()
	digest, signature, signErr := signer.SignDigestChecked(canonicalBytes)
	if signErr != nil {
		return nil, fmt.Errorf("%w: signing context: %w", ErrLegalContextUnknown, signErr)
	}
	ctx.digest = digest
	ctx.signature = signature
	return ctx, nil
}
