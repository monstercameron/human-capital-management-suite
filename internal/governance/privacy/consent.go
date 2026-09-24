package privacy

import (
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrConsentInvalid is returned by [OptionalProcessing.Validate] when a
// consent record is missing evidence, or its recorded EvidenceID no longer
// matches its own canonical digest.
var ErrConsentInvalid = errors.New("privacy: optional processing consent fails validation")

const consentEvidencePrefix = "ev:privacy:consent:"

// ConsentStatus is the resolved state of an [OptionalProcessing] consent at
// a given instant. It is always computed by [OptionalProcessing.StatusAt]
// from GrantedAt/ExpiresAt/WithdrawnAt relative to that instant -- never
// stored as an independent field that could drift from the timestamps that
// actually govern it.
type ConsentStatus string

// Consent status values.
const (
	// ConsentStatusUnspecified is returned for an instant before the
	// consent was granted, or for an otherwise-invalid record.
	ConsentStatusUnspecified ConsentStatus = ""
	ConsentStatusGranted     ConsentStatus = "GRANTED"
	ConsentStatusExpired     ConsentStatus = "EXPIRED"
	ConsentStatusWithdrawn   ConsentStatus = "WITHDRAWN"
)

// OptionalProcessing is one data subject's consent decision for one
// consent-governed purpose: which scopes it covers, which [Presentation] it
// was granted against, and its expiry/withdrawal state.
// [EvaluateAuthority] treats a missing, expired, or withdrawn consent
// identically: deny. There is no implicit-renewal or assume-granted path.
type OptionalProcessing struct {
	ID        string
	Principal string
	// Scope is the set of purposes this consent covers, e.g. "ai_assist",
	// "rag_retrieval", "analytics", "marketing_email". A purpose not in
	// Scope is never authorized by this consent, regardless of its status.
	Scope []string
	// PresentationID is the Presentation this consent was granted against.
	// EvaluateAuthority requires it to match the Presentation supplied in
	// the same authority request.
	PresentationID string
	GrantedAt      values.Instant
	// ExpiresAt is the exclusive expiry instant. The zero (unset) value
	// means no fixed expiry.
	ExpiresAt values.Instant
	// WithdrawnAt is set once the principal withdraws consent. The zero
	// (unset) value means never withdrawn. Withdrawal is recorded by
	// [OptionalProcessing.Withdraw], which returns a new record rather than
	// mutating this one.
	WithdrawnAt values.Instant
	EvidenceID  string
}

// NewOptionalProcessing builds an OptionalProcessing consent grant, validates
// it, and computes its EvidenceID from the record's own canonical digest.
func NewOptionalProcessing(id, principal string, scope []string, presentationID string, grantedAt, expiresAt values.Instant) (OptionalProcessing, error) {
	c := OptionalProcessing{
		ID:             id,
		Principal:      principal,
		Scope:          slices.Clone(scope),
		PresentationID: presentationID,
		GrantedAt:      grantedAt,
		ExpiresAt:      expiresAt,
	}
	if err := c.validateWithoutEvidence(); err != nil {
		return OptionalProcessing{}, err
	}
	c.EvidenceID = consentEvidencePrefix + c.canonicalDigest()
	return c, nil
}

// Withdraw returns a copy of c with WithdrawnAt set to at and a freshly
// computed EvidenceID. It never mutates c: PRIV-002's GREEN clause requires
// the withdrawal transition itself to be evidence, so the prior grant record
// must remain exactly as it was.
func (c OptionalProcessing) Withdraw(at values.Instant) (OptionalProcessing, error) {
	if err := c.Validate(); err != nil {
		return OptionalProcessing{}, fmt.Errorf("%w: cannot withdraw an invalid consent record: %v", ErrConsentInvalid, err)
	}
	if c.WithdrawnAt.IsSet() {
		return OptionalProcessing{}, fmt.Errorf("%w: consent %q has already been withdrawn", ErrConsentInvalid, c.ID)
	}
	if !at.IsSet() {
		return OptionalProcessing{}, fmt.Errorf("%w: withdrawal needs a withdrawn_at instant", ErrConsentInvalid)
	}
	if at.Before(c.GrantedAt) {
		return OptionalProcessing{}, fmt.Errorf("%w: consent %q cannot be withdrawn before it was granted", ErrConsentInvalid, c.ID)
	}
	withdrawn := c
	withdrawn.Scope = slices.Clone(c.Scope)
	withdrawn.WithdrawnAt = at
	withdrawn.EvidenceID = consentEvidencePrefix + withdrawn.canonicalDigest()
	return withdrawn, nil
}

func (c OptionalProcessing) validateWithoutEvidence() error {
	if c.ID == "" {
		return fmt.Errorf("%w: no id", ErrConsentInvalid)
	}
	if c.Principal == "" {
		return fmt.Errorf("%w: consent %q has no principal", ErrConsentInvalid, c.ID)
	}
	if len(c.Scope) == 0 {
		return fmt.Errorf("%w: consent %q covers no purpose", ErrConsentInvalid, c.ID)
	}
	for i, s := range c.Scope {
		if s == "" {
			return fmt.Errorf("%w: consent %q has an empty scope entry at index %d", ErrConsentInvalid, c.ID, i)
		}
	}
	if c.PresentationID == "" {
		return fmt.Errorf("%w: consent %q is not bound to a presentation", ErrConsentInvalid, c.ID)
	}
	if !c.GrantedAt.IsSet() {
		return fmt.Errorf("%w: consent %q has no granted_at", ErrConsentInvalid, c.ID)
	}
	if c.ExpiresAt.IsSet() && !c.GrantedAt.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: consent %q expires_at does not follow granted_at", ErrConsentInvalid, c.ID)
	}
	if c.WithdrawnAt.IsSet() && c.WithdrawnAt.Before(c.GrantedAt) {
		return fmt.Errorf("%w: consent %q withdrawn_at precedes granted_at", ErrConsentInvalid, c.ID)
	}
	return nil
}

// Validate reports whether the consent carries every required field and
// whether its EvidenceID still matches its own canonical digest.
func (c OptionalProcessing) Validate() error {
	if err := c.validateWithoutEvidence(); err != nil {
		return err
	}
	if c.EvidenceID == "" {
		return fmt.Errorf("%w: consent %q has no evidence id", ErrConsentInvalid, c.ID)
	}
	if c.EvidenceID != consentEvidencePrefix+c.canonicalDigest() {
		return fmt.Errorf("%w: consent %q evidence id does not match its own canonical digest", ErrConsentInvalid, c.ID)
	}
	return nil
}

func (c OptionalProcessing) canonicalDigest() string {
	dst := appendFields(nil,
		"id", c.ID,
		"principal", c.Principal,
		"presentation_id", c.PresentationID,
		"granted_at", c.GrantedAt.String(),
		"expires_at", c.ExpiresAt.String(),
		"withdrawn_at", c.WithdrawnAt.String(),
	)
	dst = appendStringSlice(dst, "scope", c.Scope)
	return digestHex(dst)
}

// HasScope reports whether purpose is covered by this consent's scope.
func (c OptionalProcessing) HasScope(purpose string) bool {
	return slices.Contains(c.Scope, purpose)
}

// StatusAt resolves GRANTED/EXPIRED/WITHDRAWN as of asOf. Withdrawal is
// checked before expiry: a withdrawn consent is withdrawn even past its
// original expiry instant. Both withdrawn_at and expires_at are treated as
// taking effect exactly at the boundary instant (inclusive), matching
// [values.EffectiveInterval]'s half-open convention used throughout this
// codebase.
func (c OptionalProcessing) StatusAt(asOf values.Instant) ConsentStatus {
	if c.Validate() != nil || !asOf.IsSet() {
		return ConsentStatusUnspecified
	}
	if asOf.Before(c.GrantedAt) {
		return ConsentStatusUnspecified
	}
	if c.WithdrawnAt.IsSet() && !asOf.Before(c.WithdrawnAt) {
		return ConsentStatusWithdrawn
	}
	if c.ExpiresAt.IsSet() && !asOf.Before(c.ExpiresAt) {
		return ConsentStatusExpired
	}
	return ConsentStatusGranted
}
