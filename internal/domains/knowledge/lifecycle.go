package knowledge

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/i18n"
)

const lifecycleSchema = "hcmnext.domains.knowledge.KnowledgeLifecycle"

// LifecycleState is the append-only state of an ArticleRevision.
type LifecycleState string

const (
	StateDraft     LifecycleState = "DRAFT"
	StateInReview  LifecycleState = "IN_REVIEW"
	StateApproved  LifecycleState = "APPROVED"
	StatePublished LifecycleState = "PUBLISHED"
	StateRetired   LifecycleState = "RETIRED"

	// Short aliases make the state vocabulary convenient at call sites.
	Draft     = StateDraft
	InReview  = StateInReview
	Approved  = StateApproved
	Published = StatePublished
	Retired   = StateRetired
)

// Valid reports whether the state is in the closed lifecycle vocabulary.
func (s LifecycleState) Valid() bool {
	switch s {
	case StateDraft, StateInReview, StateApproved, StatePublished, StateRetired:
		return true
	default:
		return false
	}
}

// EventKind identifies a digested lifecycle transition.
type EventKind string

const (
	EventDrafted      EventKind = "DRAFTED"
	EventInReview     EventKind = "IN_REVIEW"
	EventApproved     EventKind = "APPROVED"
	EventPublished    EventKind = "PUBLISHED"
	EventActivated    EventKind = "ACTIVATED"
	EventRetired      EventKind = "RETIRED"
	EventLocalization EventKind = "LOCALIZATION_ADDED"
)

// TranslationProvenance records how localized content was produced.
type TranslationProvenance string

const (
	ProvenanceHuman           TranslationProvenance = "HUMAN"
	ProvenanceMachine         TranslationProvenance = "MACHINE"
	ProvenanceMachineReviewed TranslationProvenance = "MACHINE_REVIEWED"

	Human           = ProvenanceHuman
	Machine         = ProvenanceMachine
	MachineReviewed = ProvenanceMachineReviewed
)

var (
	ErrInvalidLifecycle        = errors.New("knowledge: invalid lifecycle record")
	ErrRevisionExists          = errors.New("knowledge: article revision already exists")
	ErrRevisionNotFound        = errors.New("knowledge: article revision not found")
	ErrInvalidTransition       = errors.New("knowledge: invalid lifecycle transition")
	ErrReviewRequired          = errors.New("knowledge: review is required")
	ErrApprovalRequired        = errors.New("knowledge: required approvals are missing")
	ErrDistinctApprovers       = errors.New("knowledge: approvals require distinct approvers")
	ErrStaleSource             = errors.New("knowledge: source revision is stale")
	ErrMachineLegalTranslation = errors.New("knowledge: machine-only legal translation cannot publish")
	ErrUnresolvedCitation      = errors.New("knowledge: unresolved citation cannot publish")
	ErrLocalizationRequired    = errors.New("knowledge: requested locale has no localization")
	ErrActivationRequired      = errors.New("knowledge: activation binding is incomplete")
	ErrActivationMismatch      = errors.New("knowledge: activation binding does not match publication")
	ErrAlreadyActivated        = errors.New("knowledge: locale is already activated")
	ErrOutsideEffectiveWindow  = errors.New("knowledge: revision is outside its effective window")
)

// ReviewPolicy is the approval requirement for moving a revision to APPROVED.
// A policy with MinApprovals zero is normalized to one by NewMemoryStore.
type ReviewPolicy struct {
	MinApprovals             uint32
	RequireDistinctApprovers bool
}

// ReviewApproval is the small, reference-only approval record accepted by the
// knowledge lifecycle. It carries no proposal or private approval payload.
type ReviewApproval struct {
	ApproverID  string
	PrincipalID string
	ApprovalRef string
	DecisionID  string
	ApprovedAt  time.Time
	Approved    bool
	// Outcome is a compatibility spelling for callers carrying an approval
	// decision without the local Approved boolean.
	Outcome string
}

// ReviewEvidence is a review submission with one or more approval decisions.
type ReviewEvidence struct {
	Reviewer   string
	ReviewedAt time.Time
	Approvals  []ReviewApproval
}

// ApprovalDecision is the local, reference-only decision shape used by this
// lifecycle. The intent approval package can be adapted into this shape by a
// caller without making the knowledge domain depend on an approval runtime.
type ApprovalDecision = ReviewApproval

// LocalizedRevision is an immutable translation bound to one exact locale and
// one source ArticleRevision digest. Body text is represented only by a digest.
type LocalizedRevision struct {
	ArticleID             string
	Revision              uint64
	Locale                string
	Reviewer              string
	SourceRevisionDigest  string
	TranslationProvenance TranslationProvenance
	// Provenance is a compatibility spelling for TranslationProvenance.
	Provenance          TranslationProvenance
	BodyDigest          string
	Title               string
	Summary             string
	Classification      string
	Jurisdiction        string
	Legal               bool
	UnresolvedCitations []string
	CitationRefs        []string
	CreatedAt           time.Time
	Digest              string
}

// Validate checks the localization contract without checking source freshness.
func (l LocalizedRevision) Validate() error {
	if strings.TrimSpace(l.ArticleID) == "" || l.Revision == 0 {
		return fmt.Errorf("%w: article id and positive revision are required", ErrInvalidLifecycle)
	}
	if strings.TrimSpace(l.Locale) == "" {
		return fmt.Errorf("%w: locale is required", ErrInvalidLifecycle)
	}
	ctx, err := i18n.NewLocaleContext(l.Locale, []string{l.Locale}, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidLifecycle, err)
	}
	if ctx.Locale != l.Locale {
		return fmt.Errorf("%w: locale %q is not canonical exact locale %q", ErrInvalidLifecycle, l.Locale, ctx.Locale)
	}
	if strings.TrimSpace(l.Reviewer) == "" && l.effectiveProvenance() != ProvenanceMachine {
		return fmt.Errorf("%w: reviewer is required for reviewed translation", ErrReviewRequired)
	}
	if strings.TrimSpace(l.SourceRevisionDigest) == "" || strings.TrimSpace(l.BodyDigest) == "" {
		return fmt.Errorf("%w: source and body digests are required", ErrInvalidLifecycle)
	}
	if l.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidLifecycle)
	}
	provenance := l.effectiveProvenance()
	if provenance != ProvenanceHuman && provenance != ProvenanceMachine && provenance != ProvenanceMachineReviewed {
		return fmt.Errorf("%w: unknown translation provenance %q", ErrInvalidLifecycle, provenance)
	}
	if l.Digest != "" && l.Digest != l.contentDigest() {
		return fmt.Errorf("%w: localization digest does not match content", ErrInvalidLifecycle)
	}
	return nil
}

func (l LocalizedRevision) effectiveProvenance() TranslationProvenance {
	if l.TranslationProvenance != "" {
		return l.TranslationProvenance
	}
	return l.Provenance
}

func (l LocalizedRevision) contentDigest() string {
	w := canonicalbytes.New("hcmnext.domains.knowledge.LocalizedRevision", 1).
		String("article_id", l.ArticleID).
		Int("revision", int64(l.Revision)).
		String("locale", l.Locale).
		String("reviewer", l.Reviewer).
		String("source_revision_digest", l.SourceRevisionDigest).
		String("translation_provenance", string(l.effectiveProvenance())).
		String("body_digest", l.BodyDigest).
		String("title", l.Title).
		String("summary", l.Summary).
		String("classification", l.Classification).
		String("jurisdiction", l.Jurisdiction).
		Bool("legal", l.Legal).
		SortedStrings("unresolved_citation", l.UnresolvedCitations).
		SortedStrings("citation_ref", l.CitationRefs).
		String("created_at", l.CreatedAt.UTC().Format(time.RFC3339Nano))
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Digest returns the localization content digest, independent of Digest.
func (l LocalizedRevision) DigestValue() string { return l.contentDigest() }

// ActivationBinding is the exact locale/reviewer/source/config-bundle binding
// retained when a published locale becomes active.
type ActivationBinding struct {
	ArticleID            string
	Revision             uint64
	Locale               string
	Reviewer             string
	SourceRevisionDigest string
	BundleID             string
	BundleDigest         string
	ActivationEpoch      uint64
	ReceiptDigest        string
	ActivatedAt          time.Time
}

// Activation is a descriptive alias for ActivationBinding.
type Activation = ActivationBinding

// ActivationRequest is the caller-facing activation request. Receipt is
// optional only for compatibility; when present all of its identity fields
// must agree with the explicit bundle and epoch fields.
type ActivationRequest struct {
	ArticleID            string
	Revision             uint64
	Locale               string
	Reviewer             string
	SourceRevisionDigest string
	BundleID             string
	BundleDigest         string
	ActivationEpoch      uint64
	ReceiptDigest        string
	// Receipt accepts a configbundle.ActivationReceipt without importing the
	// platform package. Its public BundleID, BundleDigest, Epoch, and Digest
	// fields are checked when present.
	Receipt any
	At      time.Time
}

// PublishRequest identifies one locale to publish after approval.
type PublishRequest struct {
	ArticleID            string
	Revision             uint64
	Locale               string
	Reviewer             string
	SourceRevisionDigest string
	At                   time.Time
}

// RetireRequest identifies one source revision to retire.
type RetireRequest struct {
	ArticleID string
	Revision  uint64
	At        time.Time
}

// LifecycleEvent is an append-only, audit-safe transition record. It never
// contains article or translation body text, signatures, or private evidence.
type LifecycleEvent struct {
	EventID              string
	Kind                 EventKind
	PreviousState        LifecycleState
	State                LifecycleState
	ArticleID            string
	Revision             uint64
	Locale               string
	Reviewer             string
	SourceRevisionDigest string
	BundleID             string
	BundleDigest         string
	ReceiptDigest        string
	ActivationEpoch      uint64
	EvidenceDigest       string
	At                   time.Time
	Digest               string
}

// Canonical returns the deterministic, body-free event encoding.
func (e LifecycleEvent) Canonical() []byte {
	w := canonicalbytes.New(lifecycleSchema, 1).
		String("event_id", e.EventID).
		String("kind", string(e.Kind)).
		String("previous_state", string(e.PreviousState)).
		String("state", string(e.State)).
		String("article_id", e.ArticleID).
		Int("revision", int64(e.Revision)).
		String("locale", e.Locale).
		String("reviewer", e.Reviewer).
		String("source_revision_digest", e.SourceRevisionDigest).
		String("bundle_id", e.BundleID).
		String("bundle_digest", e.BundleDigest).
		String("receipt_digest", e.ReceiptDigest).
		Int("activation_epoch", int64(e.ActivationEpoch)).
		String("evidence_digest", e.EvidenceDigest).
		String("at", e.At.UTC().Format(time.RFC3339Nano))
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// DigestValue recomputes the event digest without trusting Digest.
func (e LifecycleEvent) DigestValue() string {
	raw := e.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Explain returns bounded audit facts and excludes body and signature data.
func (e LifecycleEvent) Explain() string {
	return fmt.Sprintf("knowledge event %s article=%s revision=%d locale=%s state=%s reviewer=%s epoch=%d digest=%s", e.Kind, e.ArticleID, e.Revision, e.Locale, e.State, e.Reviewer, e.ActivationEpoch, e.Digest)
}

type storedRevision struct {
	article       ArticleRevision
	state         LifecycleState
	approvals     []ReviewApproval
	localizations map[string]LocalizedRevision
	publications  map[string]bool
	activations   map[string]ActivationBinding
}

// RevisionView is a defensive read model for a revision and its history.
type RevisionView struct {
	Article       ArticleRevision
	State         LifecycleState
	Approvals     []ReviewApproval
	Localizations []LocalizedRevision
	Publications  []string
	Activations   []ActivationBinding
}

// MemoryStore is a concurrency-safe, kernel-pure lifecycle store. It stores
// immutable copies and has no database, clock, network, or side effects.
type MemoryStore struct {
	mu      sync.RWMutex
	policy  ReviewPolicy
	records map[string]*storedRevision
	latest  map[string]uint64
	events  []LifecycleEvent
}

// NewMemoryStore creates an in-memory store. It accepts ReviewPolicy or
// *ReviewPolicy as an optional approval requirement.
func NewMemoryStore(options ...any) *MemoryStore {
	policy := ReviewPolicy{MinApprovals: 1, RequireDistinctApprovers: true}
	for _, option := range options {
		switch value := option.(type) {
		case ReviewPolicy:
			policy = value
		case *ReviewPolicy:
			if value != nil {
				policy = *value
			}
		}
	}
	if policy.MinApprovals == 0 {
		policy.MinApprovals = 1
	}
	return &MemoryStore{policy: policy, records: map[string]*storedRevision{}, latest: map[string]uint64{}}
}

// NewStore is a descriptive constructor alias.
func NewStore(options ...any) *MemoryStore { return NewMemoryStore(options...) }

func revisionKey(articleID string, revision uint64) string {
	return articleID + "\x00" + fmt.Sprintf("%d", revision)
}

// Add stores a valid ArticleRevision in DRAFT and emits a digested DRAFTED
// event. Revisions are immutable and a duplicate exact revision is refused.
func (s *MemoryStore) Add(article ArticleRevision) (LifecycleEvent, error) {
	if s == nil {
		return LifecycleEvent{}, fmt.Errorf("%w: nil store", ErrInvalidLifecycle)
	}
	if err := article.Validate(); err != nil {
		return LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := revisionKey(article.ArticleID, article.Revision)
	if _, exists := s.records[key]; exists {
		return LifecycleEvent{}, ErrRevisionExists
	}
	s.records[key] = &storedRevision{article: cloneArticle(article), state: StateDraft, localizations: map[string]LocalizedRevision{}, publications: map[string]bool{}, activations: map[string]ActivationBinding{}}
	if article.Revision > s.latest[article.ArticleID] {
		s.latest[article.ArticleID] = article.Revision
	}
	return s.appendEventLocked(LifecycleEvent{Kind: EventDrafted, State: StateDraft, ArticleID: article.ArticleID, Revision: article.Revision, Locale: article.Locale, Reviewer: article.Review.ReviewedBy, SourceRevisionDigest: article.Digest()}), nil
}

// Put and Register are descriptive aliases for Add.
func (s *MemoryStore) Put(article ArticleRevision) (LifecycleEvent, error) { return s.Add(article) }
func (s *MemoryStore) Register(article ArticleRevision) (LifecycleEvent, error) {
	return s.Add(article)
}

// SubmitForReview moves DRAFT to IN_REVIEW.
func (s *MemoryStore) SubmitForReview(articleID string, revision uint64, reviewer string, at time.Time) (LifecycleEvent, error) {
	if strings.TrimSpace(reviewer) == "" || at.IsZero() {
		return LifecycleEvent{}, fmt.Errorf("%w: reviewer and review time are required", ErrReviewRequired)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.recordLocked(articleID, revision)
	if err != nil {
		return LifecycleEvent{}, err
	}
	if record.state != StateDraft {
		return LifecycleEvent{}, transitionError(record.state, StateInReview)
	}
	event := s.appendEventLocked(LifecycleEvent{Kind: EventInReview, PreviousState: record.state, State: StateInReview, ArticleID: articleID, Revision: revision, Locale: record.article.Locale, Reviewer: reviewer, SourceRevisionDigest: record.article.Digest(), At: at})
	record.state = StateInReview
	return event, nil
}

// Approve records approvals and moves IN_REVIEW to APPROVED once its quorum
// is satisfied. It accepts ReviewEvidence, []ReviewApproval, or
// []approval.ApprovalDecision in args, plus an optional time.Time.
func (s *MemoryStore) Approve(articleID string, revision uint64, args ...any) (LifecycleEvent, error) {
	evidence, at, err := normalizeReviewEvidence(args...)
	if err != nil {
		return LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.recordLocked(articleID, revision)
	if err != nil {
		return LifecycleEvent{}, err
	}
	if record.state != StateInReview {
		return LifecycleEvent{}, transitionError(record.state, StateApproved)
	}
	if evidence.Reviewer == "" {
		evidence.Reviewer = record.article.Review.ReviewedBy
	}
	if evidence.Reviewer == "" {
		return LifecycleEvent{}, ErrReviewRequired
	}
	approvals := append([]ReviewApproval(nil), evidence.Approvals...)
	if err := validateApprovals(approvals, s.policy); err != nil {
		return LifecycleEvent{}, err
	}
	record.approvals = append([]ReviewApproval(nil), approvals...)
	event := LifecycleEvent{Kind: EventApproved, PreviousState: record.state, State: StateApproved, ArticleID: articleID, Revision: revision, Locale: record.article.Locale, Reviewer: evidence.Reviewer, SourceRevisionDigest: record.article.Digest(), EvidenceDigest: approvalsDigest(approvals), At: at}
	result := s.appendEventLocked(event)
	record.state = StateApproved
	return result, nil
}

func normalizeReviewEvidence(args ...any) (ReviewEvidence, time.Time, error) {
	var evidence ReviewEvidence
	var at time.Time
	for _, arg := range args {
		switch value := arg.(type) {
		case ReviewEvidence:
			evidence = value
		case *ReviewEvidence:
			if value != nil {
				evidence = *value
			}
		case ReviewApproval:
			evidence.Approvals = append(evidence.Approvals, value)
		case []ReviewApproval:
			evidence.Approvals = append(evidence.Approvals, value...)
		case string:
			if evidence.Reviewer == "" {
				evidence.Reviewer = value
			}
		case time.Time:
			at = value
		default:
			return ReviewEvidence{}, time.Time{}, fmt.Errorf("%w: unsupported approval argument %T", ErrInvalidLifecycle, arg)
		}
	}
	if at.IsZero() {
		at = evidence.ReviewedAt
	}
	if at.IsZero() {
		return ReviewEvidence{}, time.Time{}, fmt.Errorf("%w: approval time is required", ErrReviewRequired)
	}
	if evidence.ReviewedAt.IsZero() {
		evidence.ReviewedAt = at
	}
	return evidence, at, nil
}

func validateApprovals(approvals []ReviewApproval, policy ReviewPolicy) error {
	if uint32(len(approvals)) < policy.MinApprovals {
		return fmt.Errorf("%w: got %d approvals, need %d", ErrApprovalRequired, len(approvals), policy.MinApprovals)
	}
	seen := map[string]bool{}
	for _, item := range approvals {
		id := item.ApproverID
		if id == "" {
			id = item.PrincipalID
		}
		approved := item.Approved || item.ApprovalRef != ""
		if strings.TrimSpace(item.Outcome) != "" {
			approved = strings.EqualFold(item.Outcome, "APPROVED")
		}
		if id == "" || !approved {
			return fmt.Errorf("%w: every approval must name an approving principal", ErrApprovalRequired)
		}
		if policy.RequireDistinctApprovers && seen[id] {
			return fmt.Errorf("%w: approver %q appears more than once", ErrDistinctApprovers, id)
		}
		seen[id] = true
	}
	return nil
}

// AddLocalization records a validated translation bound to the exact source
// digest. It emits a digested localization event but does not publish it.
func (s *MemoryStore) AddLocalization(localized LocalizedRevision) (LifecycleEvent, error) {
	if err := localized.Validate(); err != nil {
		return LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.recordLocked(localized.ArticleID, localized.Revision)
	if err != nil {
		return LifecycleEvent{}, err
	}
	if localized.SourceRevisionDigest != record.article.Digest() {
		return LifecycleEvent{}, fmt.Errorf("%w: localization cites %s, source is %s", ErrActivationMismatch, localized.SourceRevisionDigest, record.article.Digest())
	}
	if _, exists := record.localizations[localized.Locale]; exists {
		return LifecycleEvent{}, ErrRevisionExists
	}
	if localized.Digest == "" {
		localized.Digest = localized.contentDigest()
	}
	record.localizations[localized.Locale] = cloneLocalization(localized)
	return s.appendEventLocked(LifecycleEvent{Kind: EventLocalization, PreviousState: record.state, State: record.state, ArticleID: localized.ArticleID, Revision: localized.Revision, Locale: localized.Locale, Reviewer: localized.Reviewer, SourceRevisionDigest: localized.SourceRevisionDigest, EvidenceDigest: localized.Digest}), nil
}

// AddLocalizedRevision is an explicit-name alias for AddLocalization.
func (s *MemoryStore) AddLocalizedRevision(localized LocalizedRevision) (LifecycleEvent, error) {
	return s.AddLocalization(localized)
}

// Publish publishes the requested source locale or a stored localization. It
// accepts PublishRequest or positional article id, revision, locale, reviewer,
// source digest, and time values.
func (s *MemoryStore) Publish(request any, args ...any) (LifecycleEvent, error) {
	req, err := normalizePublishRequest(request, args...)
	if err != nil {
		return LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.recordLocked(req.ArticleID, req.Revision)
	if err != nil {
		return LifecycleEvent{}, err
	}
	if record.state != StateApproved {
		return LifecycleEvent{}, fmt.Errorf("%w: revision is %s", ErrReviewRequired, record.state)
	}
	if s.latest[req.ArticleID] != req.Revision || record.article.Supersession != nil {
		return LifecycleEvent{}, ErrStaleSource
	}
	if !req.At.IsZero() && !req.At.Before(record.article.Review.ExpiresAt.Time()) {
		return LifecycleEvent{}, fmt.Errorf("%w: review expired", ErrReviewRequired)
	}
	locale, err := exactLocale(req.Locale, record.article.Locale)
	if err != nil {
		return LifecycleEvent{}, err
	}
	reviewer := req.Reviewer
	sourceDigest := record.article.Digest()
	if locale != record.article.Locale {
		localized, ok := record.localizations[locale]
		if !ok {
			return LifecycleEvent{}, ErrLocalizationRequired
		}
		if localized.SourceRevisionDigest != sourceDigest {
			return LifecycleEvent{}, ErrStaleSource
		}
		if len(localized.UnresolvedCitations) != 0 {
			return LifecycleEvent{}, ErrUnresolvedCitation
		}
		if localized.effectiveProvenance() == ProvenanceMachine && isLegalLocalization(localized) {
			return LifecycleEvent{}, ErrMachineLegalTranslation
		}
		if reviewer == "" {
			reviewer = localized.Reviewer
		}
		if reviewer != localized.Reviewer {
			return LifecycleEvent{}, fmt.Errorf("%w: reviewer %q does not match localization reviewer %q", ErrActivationMismatch, reviewer, localized.Reviewer)
		}
	} else {
		if len(req.SourceRevisionDigest) != 0 && req.SourceRevisionDigest != sourceDigest {
			return LifecycleEvent{}, ErrStaleSource
		}
		if reviewer == "" {
			reviewer = record.article.Review.ReviewedBy
		}
		if reviewer != record.article.Review.ReviewedBy {
			return LifecycleEvent{}, fmt.Errorf("%w: reviewer %q does not match source reviewer %q", ErrActivationMismatch, reviewer, record.article.Review.ReviewedBy)
		}
	}
	record.publications[locale] = true
	event := LifecycleEvent{Kind: EventPublished, PreviousState: record.state, State: StatePublished, ArticleID: req.ArticleID, Revision: req.Revision, Locale: locale, Reviewer: reviewer, SourceRevisionDigest: sourceDigest, At: req.At}
	result := s.appendEventLocked(event)
	record.state = StatePublished
	return result, nil
}

// PublishRevision is the typed form of Publish.
func (s *MemoryStore) PublishRevision(request PublishRequest) (LifecycleEvent, error) {
	return s.Publish(request)
}

func normalizePublishRequest(request any, args ...any) (PublishRequest, error) {
	var req PublishRequest
	switch value := request.(type) {
	case PublishRequest:
		req = value
	case *PublishRequest:
		if value == nil {
			return PublishRequest{}, ErrInvalidLifecycle
		}
		req = *value
	case string:
		req.ArticleID = value
		for _, arg := range args {
			switch v := arg.(type) {
			case uint64:
				req.Revision = v
			case int:
				req.Revision = uint64(v)
			case string:
				if req.Locale == "" {
					req.Locale = v
				} else if req.Reviewer == "" {
					req.Reviewer = v
				} else {
					req.SourceRevisionDigest = v
				}
			case time.Time:
				req.At = v
			}
		}
	default:
		return PublishRequest{}, fmt.Errorf("%w: unsupported publish request %T", ErrInvalidLifecycle, request)
	}
	if req.At.IsZero() {
		return PublishRequest{}, fmt.Errorf("%w: publish time is required", ErrInvalidLifecycle)
	}
	if req.ArticleID == "" || req.Revision == 0 {
		return PublishRequest{}, fmt.Errorf("%w: article id and revision are required", ErrInvalidLifecycle)
	}
	return req, nil
}

// Activate binds a published locale to an exact source/reviewer/config bundle
// and epoch. It accepts ActivationRequest, ActivationBinding, or a receipt.
func (s *MemoryStore) Activate(request any, args ...any) (LifecycleEvent, error) {
	req, err := normalizeActivationRequest(request, args...)
	if err != nil {
		return LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.recordLocked(req.ArticleID, req.Revision)
	if err != nil {
		return LifecycleEvent{}, err
	}
	if record.state != StatePublished {
		return LifecycleEvent{}, fmt.Errorf("%w: revision is %s", ErrInvalidTransition, record.state)
	}
	locale, err := exactLocale(req.Locale, record.article.Locale)
	if err != nil {
		return LifecycleEvent{}, err
	}
	if !record.publications[locale] {
		return LifecycleEvent{}, ErrLocalizationRequired
	}
	if req.Reviewer == "" {
		return LifecycleEvent{}, fmt.Errorf("%w: reviewer is required", ErrActivationRequired)
	}
	if req.SourceRevisionDigest != record.article.Digest() || req.SourceRevisionDigest == "" {
		return LifecycleEvent{}, ErrActivationMismatch
	}
	if req.BundleID == "" || req.BundleDigest == "" || req.ActivationEpoch == 0 || req.ReceiptDigest == "" || req.At.IsZero() {
		return LifecycleEvent{}, ErrActivationRequired
	}
	receiptBundleID, receiptBundleDigest, receiptEpoch, receiptDigest, hasReceipt := receiptIdentity(req.Receipt)
	if hasReceipt {
		if receiptBundleID != req.BundleID || receiptBundleDigest != req.BundleDigest || receiptEpoch != req.ActivationEpoch || receiptDigest == "" {
			return LifecycleEvent{}, ErrActivationMismatch
		}
		if req.ReceiptDigest != "" && req.ReceiptDigest != receiptDigest {
			return LifecycleEvent{}, ErrActivationMismatch
		}
		req.ReceiptDigest = receiptDigest
	}
	if locale != record.article.Locale {
		localized, ok := record.localizations[locale]
		if !ok || localized.Reviewer != req.Reviewer || localized.SourceRevisionDigest != req.SourceRevisionDigest {
			return LifecycleEvent{}, ErrActivationMismatch
		}
	} else if req.Reviewer != record.article.Review.ReviewedBy {
		return LifecycleEvent{}, ErrActivationMismatch
	}
	if prior, ok := record.activations[locale]; ok {
		if sameActivation(prior, req) {
			return eventForActivation(prior, EventActivated, StatePublished), nil
		}
		return LifecycleEvent{}, ErrAlreadyActivated
	}
	binding := ActivationBinding{ArticleID: req.ArticleID, Revision: req.Revision, Locale: locale, Reviewer: req.Reviewer, SourceRevisionDigest: req.SourceRevisionDigest, BundleID: req.BundleID, BundleDigest: req.BundleDigest, ActivationEpoch: req.ActivationEpoch, ReceiptDigest: req.ReceiptDigest, ActivatedAt: req.At}
	record.activations[locale] = binding
	return s.appendEventLocked(LifecycleEvent{Kind: EventActivated, PreviousState: record.state, State: record.state, ArticleID: req.ArticleID, Revision: req.Revision, Locale: locale, Reviewer: req.Reviewer, SourceRevisionDigest: req.SourceRevisionDigest, BundleID: req.BundleID, BundleDigest: req.BundleDigest, ReceiptDigest: req.ReceiptDigest, ActivationEpoch: req.ActivationEpoch, At: req.At}), nil
}

// ActivateRevision is the typed form of Activate.
func (s *MemoryStore) ActivateRevision(request ActivationRequest) (LifecycleEvent, error) {
	return s.Activate(request)
}

func normalizeActivationRequest(request any, args ...any) (ActivationRequest, error) {
	switch value := request.(type) {
	case ActivationRequest:
		return value, nil
	case *ActivationRequest:
		if value == nil {
			return ActivationRequest{}, ErrActivationRequired
		}
		return *value, nil
	case ActivationBinding:
		return ActivationRequest{ArticleID: value.ArticleID, Revision: value.Revision, Locale: value.Locale, Reviewer: value.Reviewer, SourceRevisionDigest: value.SourceRevisionDigest, BundleID: value.BundleID, BundleDigest: value.BundleDigest, ActivationEpoch: value.ActivationEpoch, ReceiptDigest: value.ReceiptDigest, At: value.ActivatedAt}, nil
	default:
		return ActivationRequest{}, fmt.Errorf("%w: unsupported activation request %T", ErrActivationRequired, request)
	}
}

// receiptIdentity extracts only the public identity fields of a configbundle
// activation receipt. Reflection keeps this domain decoupled from the
// platform package while requiring the same exact bundle/epoch reference.
func receiptIdentity(receipt any) (string, string, uint64, string, bool) {
	if receipt == nil {
		return "", "", 0, "", false
	}
	v := reflect.ValueOf(receipt)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", "", 0, "", false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", "", 0, "", false
	}
	stringField := func(name string) string {
		field := v.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
		return ""
	}
	uintField := func(name string) uint64 {
		field := v.FieldByName(name)
		if field.IsValid() && (field.Kind() == reflect.Uint || field.Kind() == reflect.Uint64 || field.Kind() == reflect.Uint32) {
			return field.Uint()
		}
		return 0
	}
	bundleID := stringField("BundleID")
	bundleDigest := stringField("BundleDigest")
	epoch := uintField("Epoch")
	digest := stringField("Digest")
	return bundleID, bundleDigest, epoch, digest, bundleID != "" || bundleDigest != "" || epoch != 0 || digest != ""
}

// Retire moves PUBLISHED to RETIRED while retaining the source and all event
// history. It does not delete or mutate the ArticleRevision.
func (s *MemoryStore) Retire(request any, args ...any) (LifecycleEvent, error) {
	req, err := normalizeRetireRequest(request, args...)
	if err != nil {
		return LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.recordLocked(req.ArticleID, req.Revision)
	if err != nil {
		return LifecycleEvent{}, err
	}
	if record.state != StatePublished {
		return LifecycleEvent{}, transitionError(record.state, StateRetired)
	}
	event := LifecycleEvent{Kind: EventRetired, PreviousState: record.state, State: StateRetired, ArticleID: req.ArticleID, Revision: req.Revision, Locale: record.article.Locale, Reviewer: record.article.Review.ReviewedBy, SourceRevisionDigest: record.article.Digest(), At: req.At}
	result := s.appendEventLocked(event)
	record.state = StateRetired
	return result, nil
}

// RetireRevision is the typed form of Retire.
func (s *MemoryStore) RetireRevision(request RetireRequest) (LifecycleEvent, error) {
	return s.Retire(request)
}

func normalizeRetireRequest(request any, args ...any) (RetireRequest, error) {
	switch value := request.(type) {
	case RetireRequest:
		return value, nil
	case *RetireRequest:
		if value == nil {
			return RetireRequest{}, ErrInvalidLifecycle
		}
		return *value, nil
	case string:
		req := RetireRequest{ArticleID: value}
		for _, arg := range args {
			switch v := arg.(type) {
			case uint64:
				req.Revision = v
			case int:
				req.Revision = uint64(v)
			case time.Time:
				req.At = v
			}
		}
		return req, nil
	default:
		return RetireRequest{}, fmt.Errorf("%w: unsupported retire request %T", ErrInvalidLifecycle, request)
	}
}

// View returns a defensive snapshot including localized and activation history.
func (s *MemoryStore) View(articleID string, revision uint64) (RevisionView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, err := s.recordLocked(articleID, revision)
	if err != nil {
		return RevisionView{}, err
	}
	return cloneView(record), nil
}

// Revision is a concise alias for View.
func (s *MemoryStore) Revision(articleID string, revision uint64) (RevisionView, error) {
	return s.View(articleID, revision)
}

// ReadAsOf returns the immutable source revision when its declared effective
// interval contains at. Retired revisions remain readable within that window.
func (s *MemoryStore) ReadAsOf(articleID string, revision uint64, at time.Time) (ArticleRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, err := s.recordLocked(articleID, revision)
	if err != nil {
		return ArticleRevision{}, err
	}
	from := record.article.EffectiveInterval.EffectiveFrom.Time()
	var to time.Time
	if record.article.EffectiveInterval.EffectiveTo.IsSet() {
		to = record.article.EffectiveInterval.EffectiveTo.Time()
	}
	if at.IsZero() || at.Before(from) || (!to.IsZero() && !at.Before(to)) {
		return ArticleRevision{}, ErrOutsideEffectiveWindow
	}
	return cloneArticle(record.article), nil
}

// RevisionAsOf is a descriptive alias for ReadAsOf.
func (s *MemoryStore) RevisionAsOf(articleID string, revision uint64, at time.Time) (ArticleRevision, error) {
	return s.ReadAsOf(articleID, revision, at)
}

// Events returns a defensive copy of all digested lifecycle events.
func (s *MemoryStore) Events() []LifecycleEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]LifecycleEvent(nil), s.events...)
}

// History is a descriptive alias for Events.
func (s *MemoryStore) History() []LifecycleEvent { return s.Events() }

// Explain gives a bounded store-level audit summary.
func (s *MemoryStore) Explain() string {
	if s == nil {
		return "knowledge lifecycle store nil"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("knowledge lifecycle store revisions=%d events=%d approval_quorum=%d distinct=%t", len(s.records), len(s.events), s.policy.MinApprovals, s.policy.RequireDistinctApprovers)
}

func (s *MemoryStore) recordLocked(articleID string, revision uint64) (*storedRevision, error) {
	if s == nil {
		return nil, ErrRevisionNotFound
	}
	record, ok := s.records[revisionKey(articleID, revision)]
	if !ok {
		return nil, ErrRevisionNotFound
	}
	return record, nil
}

func (s *MemoryStore) appendEventLocked(event LifecycleEvent) LifecycleEvent {
	event.EventID = fmt.Sprintf("knowledge-event-%06d", len(s.events)+1)
	if event.At.IsZero() {
		event.At = time.Unix(0, 0).UTC()
	}
	event.Digest = event.DigestValue()
	s.events = append(s.events, event)
	return event
}

func transitionError(from, to LifecycleState) error {
	return fmt.Errorf("%w: %s cannot move to %s", ErrInvalidTransition, from, to)
}

func exactLocale(requested, source string) (string, error) {
	if requested == "" {
		requested = source
	}
	ctx, err := i18n.NewLocaleContext(requested, []string{requested}, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidLifecycle, err)
	}
	if ctx.Locale != requested {
		return "", fmt.Errorf("%w: locale %q is not exact canonical locale %q", ErrActivationMismatch, requested, ctx.Locale)
	}
	return requested, nil
}

func isLegalLocalization(l LocalizedRevision) bool {
	return l.Legal || strings.EqualFold(l.Classification, "LEGAL") || strings.EqualFold(l.Classification, "LEGAL_TEXT")
}

func sameActivation(prior ActivationBinding, req ActivationRequest) bool {
	return prior.Reviewer == req.Reviewer && prior.SourceRevisionDigest == req.SourceRevisionDigest && prior.BundleID == req.BundleID && prior.BundleDigest == req.BundleDigest && prior.ActivationEpoch == req.ActivationEpoch && prior.ReceiptDigest == req.ReceiptDigest
}

func eventForActivation(binding ActivationBinding, kind EventKind, state LifecycleState) LifecycleEvent {
	event := LifecycleEvent{Kind: kind, State: state, ArticleID: binding.ArticleID, Revision: binding.Revision, Locale: binding.Locale, Reviewer: binding.Reviewer, SourceRevisionDigest: binding.SourceRevisionDigest, BundleID: binding.BundleID, BundleDigest: binding.BundleDigest, ReceiptDigest: binding.ReceiptDigest, ActivationEpoch: binding.ActivationEpoch, At: binding.ActivatedAt}
	event.EventID = "activation-replay"
	event.Digest = event.DigestValue()
	return event
}

func cloneArticle(article ArticleRevision) ArticleRevision {
	out := article
	out.SourceRefs = append([]SourceRef(nil), article.SourceRefs...)
	out.AuthorizedRoles = append([]string(nil), article.AuthorizedRoles...)
	if article.Supersession != nil {
		supersession := *article.Supersession
		out.Supersession = &supersession
	}
	return out
}

func approvalsDigest(approvals []ReviewApproval) string {
	ordered := append([]ReviewApproval(nil), approvals...)
	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i].ApproverID
		if left == "" {
			left = ordered[i].PrincipalID
		}
		right := ordered[j].ApproverID
		if right == "" {
			right = ordered[j].PrincipalID
		}
		return left < right
	})
	w := canonicalbytes.New("hcmnext.domains.knowledge.ReviewApprovalSet", 1).
		Count("approvals", len(ordered))
	for _, item := range ordered {
		approver := item.ApproverID
		if approver == "" {
			approver = item.PrincipalID
		}
		w.String("approver", approver).
			String("approval_ref", item.ApprovalRef).
			String("decision_id", item.DecisionID).
			String("approved_at", item.ApprovedAt.UTC().Format(time.RFC3339Nano)).
			Bool("approved", item.Approved).
			String("outcome", item.Outcome)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func cloneLocalization(localized LocalizedRevision) LocalizedRevision {
	out := localized
	out.UnresolvedCitations = append([]string(nil), localized.UnresolvedCitations...)
	out.CitationRefs = append([]string(nil), localized.CitationRefs...)
	return out
}

func cloneView(record *storedRevision) RevisionView {
	out := RevisionView{Article: cloneArticle(record.article), State: record.state, Approvals: append([]ReviewApproval(nil), record.approvals...)}
	for _, localized := range record.localizations {
		out.Localizations = append(out.Localizations, cloneLocalization(localized))
	}
	sort.Slice(out.Localizations, func(i, j int) bool { return out.Localizations[i].Locale < out.Localizations[j].Locale })
	for locale := range record.publications {
		out.Publications = append(out.Publications, locale)
	}
	sort.Strings(out.Publications)
	for _, activation := range record.activations {
		out.Activations = append(out.Activations, activation)
	}
	sort.Slice(out.Activations, func(i, j int) bool { return out.Activations[i].Locale < out.Activations[j].Locale })
	return out
}
