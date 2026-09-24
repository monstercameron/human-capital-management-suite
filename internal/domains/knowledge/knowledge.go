package knowledge

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	articleRevisionSchema = "hcmnext.domains.knowledge.ArticleRevision"
	schemaVersion         = 2
)

// Knowledge errors.
var (
	// ErrArticleID is returned when an article is missing its identity.
	ErrArticleID = errors.New("knowledge: article requires an ID")
	// ErrRevision is returned when an article is missing a revision.
	ErrRevision = errors.New("knowledge: article requires a revision number")
	// ErrLocale is returned when an article is missing its locale.
	ErrLocale = errors.New("knowledge: article requires a locale")
	// ErrAudienceScope is returned when an article is missing audience scope.
	ErrAudienceScope = errors.New("knowledge: article requires an audience scope")
	// ErrClassification is returned when an article is missing classification.
	ErrClassification = errors.New("knowledge: article requires a classification")
	// ErrOwner is returned when an article is missing its owner.
	ErrOwner = errors.New("knowledge: article requires an owner")
	// ErrSourceAuthority is returned when an article is missing source authority.
	ErrSourceAuthority = errors.New("knowledge: article requires source authority")
	// ErrSourceRef is returned when an article is missing source references.
	ErrSourceRef = errors.New("knowledge: article requires at least one source reference")
	// ErrJurisdiction is returned when an article is missing jurisdiction.
	ErrJurisdiction = errors.New("knowledge: article requires a jurisdiction")
	// ErrEffectiveInterval is returned when an article has invalid effective dates.
	ErrEffectiveInterval = errors.New("knowledge: article requires a valid effective interval")
	// ErrKnownInterval is returned when an article has invalid known dates.
	ErrKnownInterval = errors.New("knowledge: article requires a valid known interval")
	// ErrBodyDigest is returned when an article is missing its body digest.
	ErrBodyDigest = errors.New("knowledge: article requires a body digest artifact reference")
	// ErrTitle is returned when an article is missing a title.
	ErrTitle = errors.New("knowledge: article requires a title")
	// ErrReview is returned when an article is missing review metadata.
	ErrReview = errors.New("knowledge: article requires review metadata")
	// ErrSupersession is returned when an article's supersession is invalid.
	ErrSupersession = errors.New("knowledge: article cannot supersede itself")
)

// EffectiveInterval is a time window when an article applies.
type EffectiveInterval struct {
	// EffectiveFrom is when the article becomes authoritative.
	EffectiveFrom values.Instant
	// EffectiveTo is when the article expires or is retired (zero for unbounded).
	EffectiveTo values.Instant
}

// Validate reports whether the interval is self-consistent.
func (e EffectiveInterval) Validate() error {
	if e.EffectiveFrom.Canonical() == nil {
		return fmt.Errorf("%w: effective_from is unset", ErrEffectiveInterval)
	}
	// EffectiveTo can be zero for unbounded future
	if e.EffectiveTo.Canonical() != nil {
		// If both are set, from must be before or equal to to
		fromSeconds := e.EffectiveFrom.Canonical()
		toSeconds := e.EffectiveTo.Canonical()
		if fromSeconds != nil && toSeconds != nil {
			// Simple byte comparison for time ordering
			fromVal := int64(0)
			toVal := int64(0)
			fmt.Sscanf(string(fromSeconds), "%d", &fromVal)
			fmt.Sscanf(string(toSeconds), "%d", &toVal)
			if fromVal > toVal && toVal > 0 {
				return fmt.Errorf("%w: effective_from after effective_to", ErrEffectiveInterval)
			}
		}
	}
	return nil
}

// KnownInterval is when an article became and ceased to be authoritative
// in the source system of record.
type KnownInterval struct {
	// KnownFrom is when the article was first recorded in the source.
	KnownFrom values.Instant
	// KnownTo is when the article ceased to be authoritative in source (zero for current).
	KnownTo values.Instant
}

// Validate reports whether the interval is self-consistent.
func (k KnownInterval) Validate() error {
	if k.KnownFrom.Canonical() == nil {
		return fmt.Errorf("%w: known_from is unset", ErrKnownInterval)
	}
	// KnownTo can be zero for unbounded
	if k.KnownTo.Canonical() != nil {
		fromSeconds := k.KnownFrom.Canonical()
		toSeconds := k.KnownTo.Canonical()
		if fromSeconds != nil && toSeconds != nil {
			fromVal := int64(0)
			toVal := int64(0)
			fmt.Sscanf(string(fromSeconds), "%d", &fromVal)
			fmt.Sscanf(string(toSeconds), "%d", &toVal)
			if fromVal > toVal && toVal > 0 {
				return fmt.Errorf("%w: known_from after known_to", ErrKnownInterval)
			}
		}
	}
	return nil
}

// SourceRef is a citation to the source of the knowledge article.
type SourceRef struct {
	// System is the authoritative system identifier.
	System string
	// Identifier is the source-local reference or document ID.
	Identifier string
	// Authority is the policy-backed decision granting source authority.
	Authority string
}

// ReviewMetadata tracks the approval and review state of an article.
type ReviewMetadata struct {
	// ReviewedBy is the principal who approved the article.
	ReviewedBy string
	// ReviewedAt is when the article was reviewed.
	ReviewedAt values.Instant
	// ExpiresAt is when the article review expires and must be renewed.
	ExpiresAt values.Instant
	// ApprovalRef is the approval decision ID or binding.
	ApprovalRef string
}

// Validate reports whether the review metadata is complete.
func (r ReviewMetadata) Validate() error {
	if r.ReviewedBy == "" {
		return fmt.Errorf("%w: reviewed_by is required", ErrReview)
	}
	if r.ReviewedAt.Canonical() == nil {
		return fmt.Errorf("%w: reviewed_at is required", ErrReview)
	}
	if r.ExpiresAt.Canonical() == nil {
		return fmt.Errorf("%w: expires_at is required", ErrReview)
	}
	if r.ApprovalRef == "" {
		return fmt.Errorf("%w: approval_ref is required", ErrReview)
	}
	return nil
}

// Supersession links an older article to the newer one that replaces it.
type Supersession struct {
	// SupersedingArticleID is the ID of the article that replaces this one.
	SupersedingArticleID string
	// SupersedingRevision is the revision of the replacing article.
	SupersedingRevision uint64
	// SupersededAt is when the article was replaced.
	SupersededAt values.Instant
}

// Validate reports whether the supersession is valid.
func (s Supersession) Validate(articleID string) error {
	if s.SupersedingArticleID == articleID {
		return fmt.Errorf("%w: article %q cannot supersede itself", ErrSupersession, articleID)
	}
	if s.SupersedingArticleID == "" {
		return fmt.Errorf("%w: superseding_article_id is required", ErrSupersession)
	}
	if s.SupersedingRevision == 0 {
		return fmt.Errorf("%w: superseding_revision must be > 0", ErrSupersession)
	}
	if s.SupersededAt.Canonical() == nil {
		return fmt.Errorf("%w: superseded_at is required", ErrSupersession)
	}
	return nil
}

// ArticleRevision is an immutable, versioned knowledge article with full
// binding: ownership, authority, audience scope, classification, effective
// dates, source citations, and review approval.
type ArticleRevision struct {
	// ArticleID is the unique identifier of the article.
	ArticleID string
	// Revision is the version number, starting from 1.
	Revision uint64
	// Locale is the language/region code (e.g., "en-US").
	Locale string
	// AudienceScope is the intended audience (e.g., "EMPLOYEES", "MANAGERS", "PUBLIC").
	AudienceScope string
	// AuthorizedRoles lists the roles from the governed authorization vocabulary
	// that may read this revision. It is always checked in addition to audience.
	AuthorizedRoles []string
	// RetentionScheduleRef identifies the approved records schedule for this
	// article revision. Search excludes undeclared records.
	RetentionScheduleRef string
	// RetainUntil is an optional schedule cutoff after which this revision is
	// no longer eligible for search; zero means the schedule has no cutoff.
	RetainUntil values.Instant
	// Classification is the data classification (e.g., "INTERNAL", "CONFIDENTIAL", "PUBLIC").
	Classification string
	// Owner is the principal responsible for the article's accuracy and lifecycle.
	Owner string
	// SourceAuthority is the policy decision granting source authority.
	SourceAuthority string
	// SourceRefs is a list of citations backing the article claims.
	SourceRefs []SourceRef
	// Jurisdiction is the legal jurisdiction (e.g., "US", "EU", "GLOBAL").
	Jurisdiction string
	// EffectiveInterval is when the article is in effect.
	EffectiveInterval EffectiveInterval
	// KnownInterval is when the article was known in the source system.
	KnownInterval KnownInterval
	// BodyDigest is the artifact reference (hash/ref) of the article body.
	// The body itself is never inline; it is stored separately and referenced here.
	BodyDigest string
	// Title is a short human-readable title.
	Title string
	// Summary is a short description (without full article text).
	Summary string
	// Review is the approval metadata.
	Review ReviewMetadata
	// Supersession, if set, indicates this article has been replaced.
	// It is nil for active articles.
	Supersession *Supersession
}

// Validate reports whether the article is fully declared and consistent.
func (a ArticleRevision) Validate() error {
	if a.ArticleID == "" {
		return fmt.Errorf("%w", ErrArticleID)
	}
	if a.Revision == 0 {
		return fmt.Errorf("%w", ErrRevision)
	}
	if a.Locale == "" {
		return fmt.Errorf("%w", ErrLocale)
	}
	if a.AudienceScope == "" {
		return fmt.Errorf("%w", ErrAudienceScope)
	}
	if a.Classification == "" {
		return fmt.Errorf("%w", ErrClassification)
	}
	if a.Owner == "" {
		return fmt.Errorf("%w", ErrOwner)
	}
	if a.SourceAuthority == "" {
		return fmt.Errorf("%w", ErrSourceAuthority)
	}
	if len(a.SourceRefs) == 0 {
		return fmt.Errorf("%w", ErrSourceRef)
	}
	for i, ref := range a.SourceRefs {
		if ref.System == "" || ref.Identifier == "" || ref.Authority == "" {
			return fmt.Errorf("%w: source_ref[%d] incomplete (system=%q id=%q auth=%q)",
				ErrSourceRef, i, ref.System, ref.Identifier, ref.Authority)
		}
	}
	if a.Jurisdiction == "" {
		return fmt.Errorf("%w", ErrJurisdiction)
	}
	if err := a.EffectiveInterval.Validate(); err != nil {
		return err
	}
	if err := a.KnownInterval.Validate(); err != nil {
		return err
	}
	if a.BodyDigest == "" {
		return fmt.Errorf("%w", ErrBodyDigest)
	}
	if a.Title == "" {
		return fmt.Errorf("%w", ErrTitle)
	}
	if err := a.Review.Validate(); err != nil {
		return err
	}
	if a.Supersession != nil {
		if err := a.Supersession.Validate(a.ArticleID); err != nil {
			return err
		}
	}
	return nil
}

// Canonical returns the deterministic byte encoding, or nil when invalid.
func (a ArticleRevision) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New(articleRevisionSchema, schemaVersion).
		String("article_id", a.ArticleID).
		Int("revision", int64(a.Revision)).
		String("locale", a.Locale).
		String("audience_scope", a.AudienceScope).
		Count("authorized_roles", len(a.AuthorizedRoles))
	for i, role := range a.AuthorizedRoles {
		w.String(fmt.Sprintf("authorized_roles[%d]", i), role)
	}
	w.String("retention_schedule_ref", a.RetentionScheduleRef).
		Optional("retain_until", a.RetainUntil.IsSet(), a.RetainUntil).
		String("classification", a.Classification).
		String("owner", a.Owner).
		String("source_authority", a.SourceAuthority).
		Count("source_refs", len(a.SourceRefs))
	for i, ref := range a.SourceRefs {
		w.String(fmt.Sprintf("source_ref[%d].system", i), ref.System).
			String(fmt.Sprintf("source_ref[%d].identifier", i), ref.Identifier).
			String(fmt.Sprintf("source_ref[%d].authority", i), ref.Authority)
	}
	w.String("jurisdiction", a.Jurisdiction).
		Value("effective_interval.from", a.EffectiveInterval.EffectiveFrom).
		Value("effective_interval.to", a.EffectiveInterval.EffectiveTo).
		Value("known_interval.from", a.KnownInterval.KnownFrom).
		Value("known_interval.to", a.KnownInterval.KnownTo).
		String("body_digest", a.BodyDigest).
		String("title", a.Title).
		String("summary", a.Summary).
		String("review.reviewed_by", a.Review.ReviewedBy).
		Value("review.reviewed_at", a.Review.ReviewedAt).
		Value("review.expires_at", a.Review.ExpiresAt).
		String("review.approval_ref", a.Review.ApprovalRef)
	if a.Supersession != nil {
		w.String("supersession.superseding_article_id", a.Supersession.SupersedingArticleID).
			Int("supersession.superseding_revision", int64(a.Supersession.SupersedingRevision)).
			Value("supersession.superseded_at", a.Supersession.SupersededAt)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical encoding.
func (a ArticleRevision) Digest() string {
	raw := a.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}
