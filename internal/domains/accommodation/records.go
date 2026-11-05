// Package accommodation models the disability-accommodation interactive
// process (WF-CAP-006): the domain that receives return-to-work's
// Accommodation follow-up. Request, dialogue, options, decision and periodic
// review are typed sealed records; every mutation runs behind a registered
// capability; medical detail is held under the case's run access policy and
// disclosed only where field policy grants it.
package accommodation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// FollowUpKindAccommodation mirrors leave.FollowUpAccommodation: the
// return-to-work follow-up kind this domain receives. The suite pins the
// two spellings together so a rename breaks loudly instead of silently
// dropping the handoff.
const FollowUpKindAccommodation = "Accommodation"

// Functional limitation codes: the closed vocabulary for what a worker is
// limited in. Limitations are functional and operational ("cannot stand for
// long"), never diagnoses; diagnosis travels separately under medical
// disclosure rules.
var limitationCodes = map[string]bool{
	"LIFT_LIMIT": true, "STAND_LIMIT": true, "SIT_LIMIT": true,
	"SCHEDULE_LIMIT": true, "VISION_AID": true, "HEARING_AID": true,
	"MOBILITY_AID": true, "BREAK_SUPPORT": true, "REMOTE_WORK": true,
	"ENVIRONMENT": true, "EQUIPMENT": true,
}

// Request states.
const (
	RequestOpen    = "OPEN"
	RequestDecided = "DECIDED"
	RequestClosed  = "CLOSED"
)

// Request is one worker's accommodation request. Diagnosis and the medical
// evidence reference are medical-gated at projection: they exist on the
// record, but a viewer without a medical grant never receives them. A
// diagnosis without evidence provenance is refused: unattributed medical
// claims never enter the interactive process.
type Request struct {
	ID                 string
	WorkerID           string
	SubmittedAt        int64
	Limitations        []string
	Diagnosis          string
	MedicalEvidenceRef string
	Status             string
	Digest             string
}

// NewRequest validates and seals one request.
func NewRequest(id, workerID string, submittedAt int64, limitations []string, diagnosis, evidenceRef string) (Request, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(workerID) == "" {
		return Request{}, fmt.Errorf("%w: request id and worker are required", ErrInvalidRecord)
	}
	if submittedAt <= 0 {
		return Request{}, fmt.Errorf("%w: submitted tick must be positive", ErrInvalidRecord)
	}
	if len(limitations) == 0 {
		return Request{}, fmt.Errorf("%w: at least one functional limitation is required", ErrInvalidRecord)
	}
	for _, l := range limitations {
		if !limitationCodes[l] {
			return Request{}, fmt.Errorf("%w: limitation %q is not in the closed vocabulary", ErrInvalidRecord, l)
		}
	}
	if strings.TrimSpace(diagnosis) != "" && strings.TrimSpace(evidenceRef) == "" {
		return Request{}, fmt.Errorf("%w: a diagnosis requires medical evidence provenance", ErrInvalidRecord)
	}
	r := Request{ID: id, WorkerID: workerID, SubmittedAt: submittedAt,
		Limitations: append([]string(nil), limitations...),
		Diagnosis:   diagnosis, MedicalEvidenceRef: evidenceRef, Status: RequestOpen}
	r.Digest = requestDigest(r)
	return r, nil
}

func requestDigest(r Request) string {
	parts := append([]string{"accommodation-request", r.ID, r.WorkerID,
		fmt.Sprint(r.SubmittedAt), r.Diagnosis, r.MedicalEvidenceRef, r.Status}, r.Limitations...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify recomputes the request seal.
func (r Request) Verify() error {
	if r.Digest == "" || requestDigest(r) != r.Digest {
		return fmt.Errorf("%w: request seal is broken", ErrSealBroken)
	}
	return nil
}

// Dialogue entry classifications.
const (
	EntryOperational = "OPERATIONAL"
	EntryMedical     = "MEDICAL"
)

// Dialogue author roles.
var dialogueRoles = map[string]bool{
	"WORKER": true, "MANAGER": true, "HR_PARTNER": true,
	"CASE_MANAGER": true, "MEDICAL_REVIEWER": true,
}

// DialogueEntry is one timestamped interactive-process note. Medical entries
// are withheld from viewers without a medical grant; operational entries go
// to every run-authorized viewer. Digest chains to the previous entry, so
// the transcript is tamper-evident in content and order.
type DialogueEntry struct {
	ID             string
	CaseID         string
	At             int64
	AuthorRole     string
	Author         string
	Classification string
	Body           string
	Digest         string
}

// Dialogue is the ordered interactive-process record. Entries append in
// non-decreasing tick order and are never rewritten: the dialogue is the
// process evidence.
type Dialogue []DialogueEntry

// Append validates, seals and appends one entry. The seal chains to the
// previous entry's digest, binding content, order and case together.
func (d Dialogue) Append(e DialogueEntry) (Dialogue, error) {
	if strings.TrimSpace(e.ID) == "" {
		return nil, fmt.Errorf("%w: dialogue entry id is required", ErrInvalidRecord)
	}
	if strings.TrimSpace(e.CaseID) == "" {
		return nil, fmt.Errorf("%w: dialogue entry %q names its case", ErrInvalidRecord, e.ID)
	}
	prev := ""
	for _, prior := range d {
		if prior.ID == e.ID {
			return nil, fmt.Errorf("%w: dialogue entry %q is already recorded", ErrInvalidRecord, e.ID)
		}
		if prior.CaseID != e.CaseID {
			return nil, fmt.Errorf("%w: dialogue entry %q belongs to another case", ErrInvalidRecord, e.ID)
		}
		prev = prior.Digest
	}
	if !dialogueRoles[e.AuthorRole] {
		return nil, fmt.Errorf("%w: author role %q is not governed", ErrInvalidRecord, e.AuthorRole)
	}
	if e.Classification != EntryOperational && e.Classification != EntryMedical {
		return nil, fmt.Errorf("%w: entry classification %q is not governed", ErrInvalidRecord, e.Classification)
	}
	if strings.TrimSpace(e.Author) == "" || strings.TrimSpace(e.Body) == "" {
		return nil, fmt.Errorf("%w: dialogue entry carries an author and a body", ErrInvalidRecord)
	}
	if len(d) > 0 && e.At < d[len(d)-1].At {
		return nil, fmt.Errorf("%w: dialogue entries append in tick order", ErrInvalidRecord)
	}
	e.Digest = dialogueDigest(e, prev)
	return append(d, e), nil
}

func dialogueDigest(e DialogueEntry, prev string) string {
	parts := []string{"accommodation-dialogue-entry", prev, e.ID, e.CaseID,
		fmt.Sprint(e.At), e.AuthorRole, e.Author, e.Classification, e.Body}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify recomputes the transcript chain: every seal and its link.
func (d Dialogue) Verify() error {
	prev := ""
	for i, e := range d {
		if e.Digest == "" || dialogueDigest(e, prev) != e.Digest {
			return fmt.Errorf("%w: dialogue entry %d seal is broken", ErrSealBroken, i)
		}
		prev = e.Digest
	}
	return nil
}

// Option is one accommodation option the process considered. An option that
// does not work carries its reason: the interactive process must show its
// work, especially for refusals.
type Option struct {
	ID              string
	CaseID          string
	Description     string
	Effective       bool
	UndueHardship   bool
	RejectionReason string
	Digest          string
}

// NewOption validates and seals one considered option.
func NewOption(id, caseID, description string, effective, undueHardship bool, reason string) (Option, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(caseID) == "" || strings.TrimSpace(description) == "" {
		return Option{}, fmt.Errorf("%w: option id, case and description are required", ErrInvalidRecord)
	}
	if !effective && strings.TrimSpace(reason) == "" {
		return Option{}, fmt.Errorf("%w: an option that does not work carries its reason", ErrInvalidRecord)
	}
	o := Option{ID: id, CaseID: caseID, Description: description,
		Effective: effective, UndueHardship: undueHardship, RejectionReason: reason}
	o.Digest = optionDigest(o)
	return o, nil
}

func optionDigest(o Option) string {
	parts := []string{"accommodation-option", o.ID, o.CaseID, o.Description,
		fmt.Sprint(o.Effective), fmt.Sprint(o.UndueHardship), o.RejectionReason}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify recomputes the option seal.
func (o Option) Verify() error {
	if o.Digest == "" || optionDigest(o) != o.Digest {
		return fmt.Errorf("%w: option seal is broken", ErrSealBroken)
	}
	return nil
}

// Decision outcomes.
const (
	DecisionGranted = "GRANTED"
	DecisionDenied  = "DENIED"
	DecisionPartial = "PARTIAL"
)

// Decision is the interactive-process outcome. Granted entries name
// limitation codes from the closed vocabulary. Rationale is operational: it
// explains what was granted and why in functional terms, never a diagnosis.
type Decision struct {
	ID               string
	CaseID           string
	Outcome          string
	Granted          []string
	EffectiveFrom    int64
	EffectiveThrough int64
	Rationale        string
	DecidedAt        int64
	Digest           string
}

// NewDecision validates and seals one decision. An EffectiveThrough of zero
// means ongoing.
func NewDecision(id, caseID, outcome string, granted []string, from, through int64, rationale string, decidedAt int64) (Decision, error) {
	switch outcome {
	case DecisionGranted, DecisionDenied, DecisionPartial:
	default:
		return Decision{}, fmt.Errorf("%w: outcome %q is not governed", ErrInvalidRecord, outcome)
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(caseID) == "" {
		return Decision{}, fmt.Errorf("%w: decision id and case are required", ErrInvalidRecord)
	}
	if strings.TrimSpace(rationale) == "" {
		return Decision{}, fmt.Errorf("%w: a decision carries its rationale", ErrInvalidRecord)
	}
	if outcome != DecisionDenied && len(granted) == 0 {
		return Decision{}, fmt.Errorf("%w: a granted decision names what it grants", ErrInvalidRecord)
	}
	for _, g := range granted {
		if !limitationCodes[g] {
			return Decision{}, fmt.Errorf("%w: granted %q is not in the closed vocabulary", ErrInvalidRecord, g)
		}
	}
	if from <= 0 || decidedAt <= 0 {
		return Decision{}, fmt.Errorf("%w: effective-from and decided ticks must be positive", ErrInvalidRecord)
	}
	if through != 0 && through <= from {
		return Decision{}, fmt.Errorf("%w: effective-through must follow effective-from", ErrInvalidRecord)
	}
	d := Decision{ID: id, CaseID: caseID, Outcome: outcome,
		Granted:       append([]string(nil), granted...),
		EffectiveFrom: from, EffectiveThrough: through,
		Rationale: rationale, DecidedAt: decidedAt}
	d.Digest = decisionDigest(d)
	return d, nil
}

func decisionDigest(d Decision) string {
	parts := append([]string{"accommodation-decision", d.ID, d.CaseID, d.Outcome,
		fmt.Sprint(d.EffectiveFrom), fmt.Sprint(d.EffectiveThrough), d.Rationale,
		fmt.Sprint(d.DecidedAt)}, d.Granted...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify recomputes the decision seal.
func (d Decision) Verify() error {
	if d.Digest == "" || decisionDigest(d) != d.Digest {
		return fmt.Errorf("%w: decision seal is broken", ErrSealBroken)
	}
	return nil
}

// Review outcomes.
const (
	ReviewContinue = "CONTINUE"
	ReviewModify   = "MODIFY"
	ReviewEnd      = "END"
)

// Review is one periodic check that the accommodation still fits. A review
// schedules first (DueAt) and completes later; completion sets the next due
// tick unless the accommodation ends.
type Review struct {
	ID          string
	CaseID      string
	DueAt       int64
	CompletedAt int64
	Outcome     string
	NextDueAt   int64
	Digest      string
}

// NewReview schedules one review.
func NewReview(id, caseID string, dueAt int64) (Review, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(caseID) == "" {
		return Review{}, fmt.Errorf("%w: review id and case are required", ErrInvalidRecord)
	}
	if dueAt <= 0 {
		return Review{}, fmt.Errorf("%w: review due tick must be positive", ErrInvalidRecord)
	}
	r := Review{ID: id, CaseID: caseID, DueAt: dueAt}
	r.Digest = reviewDigest(r)
	return r, nil
}

// Complete finishes a scheduled review. A completed review never reports
// overdue, whatever the clock says afterwards.
func (r Review) Complete(outcome string, completedAt, nextDue int64) (Review, error) {
	switch outcome {
	case ReviewContinue, ReviewModify, ReviewEnd:
	default:
		return Review{}, fmt.Errorf("%w: review outcome %q is not governed", ErrInvalidRecord, outcome)
	}
	if r.CompletedAt != 0 {
		return Review{}, fmt.Errorf("%w: review %q is already complete", ErrInvalidRecord, r.ID)
	}
	if completedAt <= 0 {
		return Review{}, fmt.Errorf("%w: completion tick must be positive", ErrInvalidRecord)
	}
	if outcome != ReviewEnd && nextDue <= completedAt {
		return Review{}, fmt.Errorf("%w: a continuing review schedules its successor", ErrInvalidRecord)
	}
	r.CompletedAt = completedAt
	r.Outcome = outcome
	r.NextDueAt = nextDue
	r.Digest = reviewDigest(r)
	return r, nil
}

// Overdue reports whether a scheduled review is past due at now. Completed
// reviews are never overdue.
func (r Review) Overdue(now int64) bool {
	return r.CompletedAt == 0 && now > r.DueAt
}

func reviewDigest(r Review) string {
	parts := []string{"accommodation-review", r.ID, r.CaseID,
		fmt.Sprint(r.DueAt), fmt.Sprint(r.CompletedAt), r.Outcome, fmt.Sprint(r.NextDueAt)}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify recomputes the review seal.
func (r Review) Verify() error {
	if r.Digest == "" || reviewDigest(r) != r.Digest {
		return fmt.Errorf("%w: review seal is broken", ErrSealBroken)
	}
	return nil
}
