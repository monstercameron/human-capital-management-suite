// Outcome links (INTENT-029) bind observed outcomes to intents without
// fabricating causation or rewriting results.
//
// A link is append-only: the IntentResult it points at is never mutated by
// a later metric or observation. Correlation is recorded as
// OBSERVED_ASSOCIATION unless a declared causal basis exists; missing,
// confounded or stale evidence stays UNKNOWN and never defaults positive.
// A model-generated assessment can never be recorded as a domain fact. A
// correction supersedes a link with a new revision; the original revision
// and its digest survive. Analytical inspection is purpose-governed: cohort
// and outcome detail disclose only to an authorized purpose, otherwise the
// inspector receives a non-disclosing redaction.
package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrOutcomeLinkInvalid reports a link that cannot be authoritative.
	ErrOutcomeLinkInvalid = errors.New("intent: invalid outcome link")
	// ErrOutcomeLinkConflict reports a correction that does not supersede
	// the exact link revision it claims to replace.
	ErrOutcomeLinkConflict = errors.New("intent: outcome link correction conflict")
	// ErrOutcomeLinkDenied reports purpose-governed inspection that refuses
	// to disclose cohort or outcome detail.
	ErrOutcomeLinkDenied = errors.New("intent: outcome link inspection denied")
)

// EpistemicStatus is the closed causal vocabulary of an outcome link.
type EpistemicStatus string

const (
	// EpistemicObservedAssociation records correlation without a causal claim.
	EpistemicObservedAssociation EpistemicStatus = "OBSERVED_ASSOCIATION"
	// EpistemicDeclaredCausal records causation only with a declared basis.
	EpistemicDeclaredCausal EpistemicStatus = "DECLARED_CAUSAL"
	// EpistemicUnknown records missing, confounded or stale evidence.
	EpistemicUnknown EpistemicStatus = "UNKNOWN"
)

// Valid reports whether s is a declared epistemic status.
func (s EpistemicStatus) Valid() bool {
	switch s {
	case EpistemicObservedAssociation, EpistemicDeclaredCausal, EpistemicUnknown:
		return true
	}
	return false
}

// OutcomeLink is one append-only binding between an intent result and an
// observed outcome. LinkDigest binds every field; Supersedes names the
// prior revision a correction replaces, empty for an original link.
type OutcomeLink struct {
	LinkID           string
	IntentID         string
	ResultRef        string
	ProposalRevision string
	ObservationRef   string
	MetricDefinition string
	MetricVersion    string
	SubjectScope     string
	CohortScope      string
	EffectiveAt      time.Time
	KnownAt          time.Time
	ObservedAt       time.Time
	Source           string
	Watermark        string
	Epistemic        EpistemicStatus
	CausalBasis      string
	Confidence       float64
	Limitations      string
	ModelGenerated   bool
	AsDomainFact     bool
	Supersedes       string
	LinkDigest       string
}

func (l OutcomeLink) body() string {
	times := []string{
		l.EffectiveAt.UTC().Format(time.RFC3339Nano),
		l.KnownAt.UTC().Format(time.RFC3339Nano),
		l.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
	parts := []string{
		l.LinkID, l.IntentID, l.ResultRef, l.ProposalRevision,
		l.ObservationRef, l.MetricDefinition, l.MetricVersion,
		l.SubjectScope, l.CohortScope,
		strings.Join(times, ","),
		l.Source, l.Watermark, string(l.Epistemic), l.CausalBasis,
		fmt.Sprintf("%.6f", l.Confidence), l.Limitations,
		fmt.Sprintf("%v/%v", l.ModelGenerated, l.AsDomainFact),
		l.Supersedes,
	}
	return strings.Join(parts, "\x00")
}

func (l OutcomeLink) computedDigest() string {
	sum := sha256.Sum256([]byte(l.body()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Validate rejects a link that fabricates causation, defaults missing
// evidence positive, promotes a model assessment to domain fact or breaks
// the append-only time order.
func (l OutcomeLink) Validate() error {
	for field, value := range map[string]string{
		"link_id": l.LinkID, "intent_id": l.IntentID, "result_ref": l.ResultRef,
		"proposal_revision": l.ProposalRevision, "metric_definition": l.MetricDefinition,
		"metric_version": l.MetricVersion, "subject_scope": l.SubjectScope,
		"source": l.Source, "watermark": l.Watermark, "limitations": l.Limitations,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrOutcomeLinkInvalid, field)
		}
	}
	if !l.Epistemic.Valid() {
		return fmt.Errorf("%w: epistemic status %q is not declared", ErrOutcomeLinkInvalid, l.Epistemic)
	}
	if l.EffectiveAt.IsZero() || l.KnownAt.IsZero() || l.ObservedAt.IsZero() {
		return fmt.Errorf("%w: effective, known and observed times are required", ErrOutcomeLinkInvalid)
	}
	if l.KnownAt.Before(l.EffectiveAt) || l.ObservedAt.Before(l.KnownAt) {
		return fmt.Errorf("%w: observed_at >= known_at >= effective_at is required", ErrOutcomeLinkInvalid)
	}
	if l.Confidence < 0 || l.Confidence > 1 {
		return fmt.Errorf("%w: confidence %v is outside [0,1]", ErrOutcomeLinkInvalid, l.Confidence)
	}
	switch l.Epistemic {
	case EpistemicDeclaredCausal:
		if strings.TrimSpace(l.CausalBasis) == "" {
			return fmt.Errorf("%w: DECLARED_CAUSAL requires a declared causal basis, not correlation", ErrOutcomeLinkInvalid)
		}
		if strings.TrimSpace(l.ObservationRef) == "" {
			return fmt.Errorf("%w: DECLARED_CAUSAL requires the supporting observation", ErrOutcomeLinkInvalid)
		}
	case EpistemicObservedAssociation:
		if strings.TrimSpace(l.ObservationRef) == "" {
			return fmt.Errorf("%w: OBSERVED_ASSOCIATION requires the correlated observation", ErrOutcomeLinkInvalid)
		}
	case EpistemicUnknown:
		if l.Confidence > 0 {
			return fmt.Errorf("%w: UNKNOWN evidence must not carry positive confidence", ErrOutcomeLinkInvalid)
		}
	}
	if l.Confidence == 0 && l.Epistemic != EpistemicUnknown {
		return fmt.Errorf("%w: zero-confidence evidence must be UNKNOWN, never default positive", ErrOutcomeLinkInvalid)
	}
	if l.ModelGenerated && l.AsDomainFact {
		return fmt.Errorf("%w: a model-generated assessment is analytical, never a domain fact", ErrOutcomeLinkInvalid)
	}
	return nil
}

// NewOutcomeLink validates and seals one append-only link.
func NewOutcomeLink(l OutcomeLink) (OutcomeLink, error) {
	if strings.TrimSpace(l.Supersedes) != "" {
		return OutcomeLink{}, fmt.Errorf("%w: an original link supersedes nothing; use CorrectLink", ErrOutcomeLinkInvalid)
	}
	l.LinkDigest = ""
	if err := l.Validate(); err != nil {
		return OutcomeLink{}, err
	}
	l.LinkDigest = l.computedDigest()
	return l, nil
}

// CorrectLink appends a correction revision: the successor names the exact
// digest it supersedes and the original is preserved, never rewritten.
func CorrectLink(previous OutcomeLink, correction OutcomeLink) (OutcomeLink, error) {
	if previous.LinkDigest == "" {
		return OutcomeLink{}, fmt.Errorf("%w: previous link is unsealed", ErrOutcomeLinkConflict)
	}
	if err := previous.Validate(); err != nil {
		return OutcomeLink{}, fmt.Errorf("%w: previous link is invalid: %v", ErrOutcomeLinkConflict, err)
	}
	correction.Supersedes = previous.LinkDigest
	// A correction is a new observation of the same outcome, made later.
	if !correction.ObservedAt.After(previous.ObservedAt) && !correction.ObservedAt.Equal(previous.ObservedAt) {
		return OutcomeLink{}, fmt.Errorf("%w: correction observation predates the original", ErrOutcomeLinkConflict)
	}
	if correction.IntentID != previous.IntentID || correction.ResultRef != previous.ResultRef {
		return OutcomeLink{}, fmt.Errorf("%w: correction must bind the same intent and result", ErrOutcomeLinkConflict)
	}
	correction.LinkDigest = ""
	if err := correction.Validate(); err != nil {
		return OutcomeLink{}, err
	}
	// A correction that changes nothing but the supersede pointer carries
	// no new observation: compare content bodies without the supersede
	// edge on either side, so no-op revisions fail at any chain depth.
	rebodied := correction
	rebodied.Supersedes = ""
	unlinked := previous
	unlinked.Supersedes = ""
	if rebodied.body() == unlinked.body() {
		return OutcomeLink{}, fmt.Errorf("%w: correction is identical to the original", ErrOutcomeLinkConflict)
	}
	correction.LinkDigest = correction.computedDigest()
	return correction, nil
}

// Inspector is the purpose-governed analytical accessor.
type Inspector struct {
	Purpose           string
	AuthorizedCohorts []string
}

// RedactedLink is the non-disclosing view returned when the inspector's
// purpose or cohort scope does not authorize detail.
type RedactedLink struct {
	LinkID    string
	IntentID  string
	Epistemic EpistemicStatus
	Withheld  bool
	Reason    string
}

// Inspect returns the full link only when the inspector declares a purpose
// and holds the link's cohort scope; otherwise it returns a redacted view
// that discloses neither cohort membership nor outcome detail.
func (l OutcomeLink) Inspect(in Inspector) (OutcomeLink, RedactedLink, error) {
	if strings.TrimSpace(in.Purpose) == "" {
		return OutcomeLink{}, RedactedLink{}, fmt.Errorf("%w: analytical access requires a declared purpose", ErrOutcomeLinkDenied)
	}
	authorized := false
	for _, cohort := range in.AuthorizedCohorts {
		if cohort == l.CohortScope {
			authorized = true
			break
		}
	}
	if !authorized {
		return OutcomeLink{}, RedactedLink{
			LinkID: l.LinkID, IntentID: l.IntentID, Epistemic: l.Epistemic,
			Withheld: true, Reason: "cohort scope is not authorized for this purpose",
		}, nil
	}
	return l, RedactedLink{}, nil
}

// ChainDigest folds an ordered link history (originals then corrections)
// into one digest a reconstructor can compare.
func ChainDigest(history []OutcomeLink) string {
	digests := make([]string, 0, len(history))
	for _, l := range history {
		digests = append(digests, l.LinkDigest)
	}
	sort.Strings(digests)
	sum := sha256.Sum256([]byte(strings.Join(digests, "\x01")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
