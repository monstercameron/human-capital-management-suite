// Package recruiting owns the native ATS transaction semantics:
// authoritative requisition, posting, application and candidacy
// lifecycles. Person/prospect identity, job/position truth and CRM
// membership stay referenced owners — this package stores their refs,
// never their facts.
//
// Every entity is an immutable revision: a command validates all of
// its inputs first, then appends exactly one revision plus one event
// to both the event log and the outbox. An invalid command returns a
// typed MISSING_PARENT, DUPLICATE_APPLICATION, REQUISITION_CLOSED or
// INVALID_CANDIDACY_TRANSITION refusal and appends nothing.
package recruiting

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's vocabulary version.
func Version() int { return schemaVersion }

// Refusal codes. Every invalid command returns exactly one of these.
const (
	CodeMissingParent              = "MISSING_PARENT"
	CodeDuplicateApplication       = "DUPLICATE_APPLICATION"
	CodeRequisitionClosed          = "REQUISITION_CLOSED"
	CodeInvalidCandidacyTransition = "INVALID_CANDIDACY_TRANSITION"
)

// RecruitingError is the typed command refusal. Field names the
// offending input and State the offending condition; consent and
// other governed values are never echoed.
type RecruitingError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *RecruitingError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsRecruitingError unwraps a typed recruiting refusal.
func AsRecruitingError(err error) (*RecruitingError, bool) {
	if err == nil {
		return nil, false
	}
	if refused, ok := err.(*RecruitingError); ok && refused.Code != "" {
		return refused, true
	}
	return nil, false
}

// codeOf reports the refusal code of err, or "" when err is not a
// typed recruiting refusal.
func codeOf(err error) string {
	if refused, ok := AsRecruitingError(err); ok {
		return refused.Code
	}
	return ""
}

func missingParent(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeMissingParent, Field: field, State: state, Version: schemaVersion}
}

func duplicateApplication(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeDuplicateApplication, Field: field, State: state, Version: schemaVersion}
}

func requisitionClosed(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeRequisitionClosed, Field: field, State: state, Version: schemaVersion}
}

func invalidCandidacyTransition(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeInvalidCandidacyTransition, Field: field, State: state, Version: schemaVersion}
}

func validID(id string) bool { return strings.TrimSpace(id) != "" }

func validTimes(effective values.Instant, known values.KnownAt) bool {
	return effective.Validate() == nil && known.Canonical() != nil
}

// RequisitionStatus is the requisition lifecycle vocabulary.
type RequisitionStatus string

// The requisition lifecycle: DRAFT opens, OPEN closes, CLOSED ends.
const (
	RequisitionDraft  RequisitionStatus = "DRAFT"
	RequisitionOpen   RequisitionStatus = "OPEN"
	RequisitionClosed RequisitionStatus = "CLOSED"
)

func (s RequisitionStatus) Valid() bool {
	return s == RequisitionDraft || s == RequisitionOpen || s == RequisitionClosed
}

// Requisition is one immutable requisition revision.
type Requisition struct {
	RequisitionID   string
	Revision        uint64
	Status          RequisitionStatus
	SourceRef       string
	EffectiveAt     values.Instant
	KnownAt         values.KnownAt
	CanonicalDigest string
}

func (r Requisition) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.Requisition", schemaVersion).
		String("requisition_id", r.RequisitionID).Int("revision", int64(r.Revision)).
		String("status", string(r.Status)).String("source_ref", r.SourceRef).
		Value("effective_at", r.EffectiveAt).Value("known_at", r.KnownAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r Requisition) withDigest() Requisition {
	r.CanonicalDigest = canonicalbytes.Digest(r.body())
	return r
}

// PostingStatus is the posting lifecycle vocabulary.
type PostingStatus string

// The posting lifecycle: DRAFT publishes, PUBLISHED closes, CLOSED ends.
const (
	PostingDraft     PostingStatus = "DRAFT"
	PostingPublished PostingStatus = "PUBLISHED"
	PostingClosed    PostingStatus = "CLOSED"
)

func (s PostingStatus) Valid() bool {
	return s == PostingDraft || s == PostingPublished || s == PostingClosed
}

// Posting is one immutable posting revision pinned to its parent
// requisition revision.
type Posting struct {
	PostingID           string
	Revision            uint64
	Status              PostingStatus
	RequisitionID       string
	RequisitionRevision uint64
	SourceRef           string
	EffectiveAt         values.Instant
	KnownAt             values.KnownAt
	CanonicalDigest     string
}

func (p Posting) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.Posting", schemaVersion).
		String("posting_id", p.PostingID).Int("revision", int64(p.Revision)).
		String("status", string(p.Status)).String("requisition_id", p.RequisitionID).
		Int("requisition_revision", int64(p.RequisitionRevision)).String("source_ref", p.SourceRef).
		Value("effective_at", p.EffectiveAt).Value("known_at", p.KnownAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p Posting) withDigest() Posting {
	p.CanonicalDigest = canonicalbytes.Digest(p.body())
	return p
}

// ApplicationStatus is the application lifecycle vocabulary.
type ApplicationStatus string

// The application lifecycle: SUBMITTED withdraws, WITHDRAWN ends.
const (
	ApplicationSubmitted ApplicationStatus = "SUBMITTED"
	ApplicationWithdrawn ApplicationStatus = "WITHDRAWN"
)

func (s ApplicationStatus) Valid() bool {
	return s == ApplicationSubmitted || s == ApplicationWithdrawn
}

// Application is one immutable application revision pinned to its
// candidate, requisition and posting revisions.
type Application struct {
	ApplicationID       string
	Revision            uint64
	Status              ApplicationStatus
	CandidateID         string
	RequisitionID       string
	RequisitionRevision uint64
	PostingID           string
	PostingRevision     uint64
	SourceRef           string
	EffectiveAt         values.Instant
	KnownAt             values.KnownAt
	CanonicalDigest     string
}

func (a Application) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.Application", schemaVersion).
		String("application_id", a.ApplicationID).Int("revision", int64(a.Revision)).
		String("status", string(a.Status)).String("candidate_id", a.CandidateID).
		String("requisition_id", a.RequisitionID).Int("requisition_revision", int64(a.RequisitionRevision)).
		String("posting_id", a.PostingID).Int("posting_revision", int64(a.PostingRevision)).
		String("source_ref", a.SourceRef).
		Value("effective_at", a.EffectiveAt).Value("known_at", a.KnownAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (a Application) withDigest() Application {
	a.CanonicalDigest = canonicalbytes.Digest(a.body())
	return a
}

// CandidacyStage is the candidacy lifecycle vocabulary.
type CandidacyStage string

// The candidacy lifecycle moves forward through APPLIED, SCREENING,
// INTERVIEW and OFFER to HIRED; WITHDRAWN and REJECTED end it from
// any non-terminal stage. Terminal stages have no exits.
const (
	StageApplied   CandidacyStage = "APPLIED"
	StageScreening CandidacyStage = "SCREENING"
	StageInterview CandidacyStage = "INTERVIEW"
	StageOffer     CandidacyStage = "OFFER"
	StageHired     CandidacyStage = "HIRED"
	StageWithdrawn CandidacyStage = "WITHDRAWN"
	StageRejected  CandidacyStage = "REJECTED"
)

func (s CandidacyStage) Valid() bool {
	switch s {
	case StageApplied, StageScreening, StageInterview, StageOffer, StageHired, StageWithdrawn, StageRejected:
		return true
	default:
		return false
	}
}

func (s CandidacyStage) terminal() bool {
	return s == StageHired || s == StageWithdrawn || s == StageRejected
}

func (s CandidacyStage) rank() int {
	switch s {
	case StageApplied:
		return 0
	case StageScreening:
		return 1
	case StageInterview:
		return 2
	case StageOffer:
		return 3
	case StageHired:
		return 4
	default:
		return -1
	}
}

// Candidacy is one immutable candidacy revision. ConsentRef and Purpose
// record that processing consent was captured; their values never
// leave this record for errors or events.
type Candidacy struct {
	CandidacyID     string
	Revision        uint64
	Stage           CandidacyStage
	ApplicationID   string
	CandidateID     string
	ConsentRef      string
	Purpose         string
	SourceRef       string
	EffectiveAt     values.Instant
	KnownAt         values.KnownAt
	CanonicalDigest string
}

func (c Candidacy) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.Candidacy", schemaVersion).
		String("candidacy_id", c.CandidacyID).Int("revision", int64(c.Revision)).
		String("stage", string(c.Stage)).String("application_id", c.ApplicationID).
		String("candidate_id", c.CandidateID).String("consent_ref", c.ConsentRef).
		String("purpose", c.Purpose).String("source_ref", c.SourceRef).
		Value("effective_at", c.EffectiveAt).Value("known_at", c.KnownAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (c Candidacy) withDigest() Candidacy {
	c.CanonicalDigest = canonicalbytes.Digest(c.body())
	return c
}

// RecruitingEvent is the appended fact for one accepted command. It
// carries aggregate identity and revision only — never consent content.
type RecruitingEvent struct {
	Kind        string
	AggregateID string
	Revision    uint64
	EffectiveAt values.Instant
	KnownAt     values.KnownAt
}

// Aggregate is the authoritative ATS boundary: current revisions plus
// the append-only event log and outbox mirror.
type Aggregate struct {
	UniquenessPolicy string
	Requisitions     map[string]Requisition
	Postings         map[string]Posting
	Applications     map[string]Application
	ApplicationKeys  map[string]string
	Candidacies      map[string]Candidacy
	Events           []RecruitingEvent
	Outbox           []RecruitingEvent
}

// NewAggregate opens an empty ATS boundary under the declared
// application uniqueness policy (for example "candidate+requisition").
func NewAggregate(uniquenessPolicy string) (Aggregate, error) {
	if strings.TrimSpace(uniquenessPolicy) == "" {
		return Aggregate{}, missingParent("uniqueness-policy", "missing")
	}
	return Aggregate{
		UniquenessPolicy: uniquenessPolicy,
		Requisitions:     make(map[string]Requisition),
		Postings:         make(map[string]Posting),
		Applications:     make(map[string]Application),
		ApplicationKeys:  make(map[string]string),
		Candidacies:      make(map[string]Candidacy),
	}, nil
}

func (a *Aggregate) append(event RecruitingEvent) {
	a.Events = append(a.Events, event)
	a.Outbox = append(a.Outbox, event)
}

// OpenRequisition creates requisition id as OPEN at revision 1.
func (a *Aggregate) OpenRequisition(id, sourceRef string, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	if !validID(id) {
		return missingParent("requisition", "missing-id")
	}
	if _, exists := a.Requisitions[id]; exists {
		return duplicateApplication("requisition", "already-open")
	}
	if !validID(sourceRef) {
		return missingParent("source", "missing")
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	a.Requisitions[id] = Requisition{
		RequisitionID: id, Revision: 1, Status: RequisitionOpen,
		SourceRef: sourceRef, EffectiveAt: effective, KnownAt: known,
	}.withDigest()
	a.append(RecruitingEvent{Kind: "REQUISITION_OPENED", AggregateID: id, Revision: 1, EffectiveAt: effective, KnownAt: known})
	return nil
}

// CloseRequisition moves an OPEN requisition to CLOSED.
func (a *Aggregate) CloseRequisition(id string, expectedRevision uint64, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	current, ok := a.Requisitions[id]
	if !ok {
		return missingParent("requisition", "unknown")
	}
	if current.Revision != expectedRevision {
		return missingParent("requisition", "revision-mismatch")
	}
	if current.Status != RequisitionOpen {
		return requisitionClosed("requisition", string(current.Status))
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	current.Revision++
	current.Status = RequisitionClosed
	current.EffectiveAt = effective
	current.KnownAt = known
	a.Requisitions[id] = current.withDigest()
	a.append(RecruitingEvent{Kind: "REQUISITION_CLOSED", AggregateID: id, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// CreatePosting drafts a posting pinned to an open requisition revision.
func (a *Aggregate) CreatePosting(postingID, requisitionID string, requisitionRevision uint64, sourceRef string, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	if !validID(postingID) {
		return missingParent("posting", "missing-id")
	}
	if _, exists := a.Postings[postingID]; exists {
		return duplicateApplication("posting", "already-exists")
	}
	parent, ok := a.Requisitions[requisitionID]
	if !ok || parent.Revision != requisitionRevision {
		return missingParent("requisition", "unknown-revision")
	}
	if parent.Status != RequisitionOpen {
		return requisitionClosed("requisition", string(parent.Status))
	}
	if !validID(sourceRef) {
		return missingParent("source", "missing")
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	a.Postings[postingID] = Posting{
		PostingID: postingID, Revision: 1, Status: PostingDraft,
		RequisitionID: requisitionID, RequisitionRevision: requisitionRevision,
		SourceRef: sourceRef, EffectiveAt: effective, KnownAt: known,
	}.withDigest()
	a.append(RecruitingEvent{Kind: "POSTING_CREATED", AggregateID: postingID, Revision: 1, EffectiveAt: effective, KnownAt: known})
	return nil
}

// PublishPosting moves a DRAFT posting to PUBLISHED while its parent
// requisition stays open.
func (a *Aggregate) PublishPosting(postingID string, expectedRevision uint64, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	current, ok := a.Postings[postingID]
	if !ok {
		return missingParent("posting", "unknown")
	}
	if current.Revision != expectedRevision {
		return missingParent("posting", "revision-mismatch")
	}
	if current.Status != PostingDraft {
		return missingParent("posting", "not-draft")
	}
	parent, ok := a.Requisitions[current.RequisitionID]
	if !ok || parent.Revision != current.RequisitionRevision {
		return missingParent("requisition", "unknown-revision")
	}
	if parent.Status != RequisitionOpen {
		return requisitionClosed("requisition", string(parent.Status))
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	current.Revision++
	current.Status = PostingPublished
	current.EffectiveAt = effective
	current.KnownAt = known
	a.Postings[postingID] = current.withDigest()
	a.append(RecruitingEvent{Kind: "POSTING_PUBLISHED", AggregateID: postingID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// ClosePosting moves a PUBLISHED posting to CLOSED.
func (a *Aggregate) ClosePosting(postingID string, expectedRevision uint64, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	current, ok := a.Postings[postingID]
	if !ok {
		return missingParent("posting", "unknown")
	}
	if current.Revision != expectedRevision {
		return missingParent("posting", "revision-mismatch")
	}
	if current.Status != PostingPublished {
		return missingParent("posting", "not-published")
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	current.Revision++
	current.Status = PostingClosed
	current.EffectiveAt = effective
	current.KnownAt = known
	a.Postings[postingID] = current.withDigest()
	a.append(RecruitingEvent{Kind: "POSTING_CLOSED", AggregateID: postingID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// uniquenessKey binds one application to the declared policy scope.
func (a *Aggregate) uniquenessKey(candidateID, requisitionID string) string {
	return a.UniquenessPolicy + "\x00" + candidateID + "\x00" + requisitionID
}

// SubmitApplication files one application pinned to exact candidate,
// requisition and posting revisions.
func (a *Aggregate) SubmitApplication(appID, candidateID, requisitionID string, requisitionRevision uint64, postingID string, postingRevision uint64, sourceRef string, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	if !validID(appID) {
		return missingParent("application", "missing-id")
	}
	if _, exists := a.Applications[appID]; exists {
		return duplicateApplication("application", "already-exists")
	}
	if !validID(candidateID) {
		return missingParent("candidate", "missing-id")
	}
	parent, ok := a.Requisitions[requisitionID]
	if !ok || parent.Revision != requisitionRevision {
		return missingParent("requisition", "unknown-revision")
	}
	if parent.Status != RequisitionOpen {
		return requisitionClosed("requisition", string(parent.Status))
	}
	posting, ok := a.Postings[postingID]
	if !ok || posting.Revision != postingRevision || posting.RequisitionID != requisitionID {
		return missingParent("posting", "unknown-revision")
	}
	if posting.Status != PostingPublished {
		return missingParent("posting", "not-published")
	}
	if _, exists := a.ApplicationKeys[a.uniquenessKey(candidateID, requisitionID)]; exists {
		return duplicateApplication("application", "duplicate-under-policy")
	}
	if !validID(sourceRef) {
		return missingParent("source", "missing")
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	a.Applications[appID] = Application{
		ApplicationID: appID, Revision: 1, Status: ApplicationSubmitted,
		CandidateID: candidateID, RequisitionID: requisitionID, RequisitionRevision: requisitionRevision,
		PostingID: postingID, PostingRevision: postingRevision,
		SourceRef: sourceRef, EffectiveAt: effective, KnownAt: known,
	}.withDigest()
	a.ApplicationKeys[a.uniquenessKey(candidateID, requisitionID)] = appID
	a.append(RecruitingEvent{Kind: "APPLICATION_SUBMITTED", AggregateID: appID, Revision: 1, EffectiveAt: effective, KnownAt: known})
	return nil
}

// WithdrawApplication moves a SUBMITTED application to WITHDRAWN.
func (a *Aggregate) WithdrawApplication(appID string, expectedRevision uint64, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	current, ok := a.Applications[appID]
	if !ok {
		return missingParent("application", "unknown")
	}
	if current.Revision != expectedRevision {
		return missingParent("application", "revision-mismatch")
	}
	if current.Status != ApplicationSubmitted {
		return missingParent("application", "not-submitted")
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	current.Revision++
	current.Status = ApplicationWithdrawn
	current.EffectiveAt = effective
	current.KnownAt = known
	a.Applications[appID] = current.withDigest()
	a.append(RecruitingEvent{Kind: "APPLICATION_WITHDRAWN", AggregateID: appID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// CreateCandidacy opens one APPLIED candidacy for an application. The
// candidate must match the application and processing consent plus
// purpose must be captured — otherwise the candidacy cannot start.
func (a *Aggregate) CreateCandidacy(candidacyID, applicationID, candidateID, consentRef, purpose, sourceRef string, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	if !validID(candidacyID) {
		return missingParent("candidacy", "missing-id")
	}
	if _, exists := a.Candidacies[candidacyID]; exists {
		return invalidCandidacyTransition("candidacy", "already-exists")
	}
	application, ok := a.Applications[applicationID]
	if !ok {
		return missingParent("application", "unknown")
	}
	if application.CandidateID != candidateID {
		return missingParent("candidate", "candidate-mismatch")
	}
	if !validID(consentRef) {
		return invalidCandidacyTransition("consent", "missing")
	}
	if !validID(purpose) {
		return invalidCandidacyTransition("purpose", "missing")
	}
	if !validID(sourceRef) {
		return missingParent("source", "missing")
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	a.Candidacies[candidacyID] = Candidacy{
		CandidacyID: candidacyID, Revision: 1, Stage: StageApplied,
		ApplicationID: applicationID, CandidateID: candidateID,
		ConsentRef: consentRef, Purpose: purpose,
		SourceRef: sourceRef, EffectiveAt: effective, KnownAt: known,
	}.withDigest()
	a.append(RecruitingEvent{Kind: "CANDIDACY_CREATED", AggregateID: candidacyID, Revision: 1, EffectiveAt: effective, KnownAt: known})
	return nil
}

// TransitionCandidacy moves one candidacy forward. WITHDRAWN, REJECTED
// and HIRED are terminal and have no exits.
func (a *Aggregate) TransitionCandidacy(candidacyID string, expectedRevision uint64, toStage CandidacyStage, effective values.Instant, known values.KnownAt) error {
	if a == nil {
		return missingParent("aggregate", "missing")
	}
	current, ok := a.Candidacies[candidacyID]
	if !ok {
		return missingParent("candidacy", "unknown")
	}
	if current.Revision != expectedRevision {
		return missingParent("candidacy", "revision-mismatch")
	}
	if !toStage.Valid() {
		return invalidCandidacyTransition("stage", "unknown-stage")
	}
	if current.Stage.terminal() {
		return invalidCandidacyTransition("stage", "terminal-"+string(current.Stage))
	}
	switch toStage {
	case StageWithdrawn, StageRejected:
		// Terminal exits are reachable from any non-terminal stage.
	case StageHired:
		if current.Stage != StageOffer {
			return invalidCandidacyTransition("stage", "hire-requires-offer")
		}
	default:
		if toStage.rank() <= current.Stage.rank() {
			return invalidCandidacyTransition("stage", "backward-transition")
		}
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	current.Revision++
	current.Stage = toStage
	current.EffectiveAt = effective
	current.KnownAt = known
	a.Candidacies[candidacyID] = current.withDigest()
	kind := "CANDIDACY_ADVANCED"
	switch toStage {
	case StageWithdrawn:
		kind = "CANDIDACY_WITHDRAWN"
	case StageRejected:
		kind = "CANDIDACY_REJECTED"
	case StageHired:
		kind = "CANDIDACY_HIRED"
	}
	a.append(RecruitingEvent{Kind: kind, AggregateID: candidacyID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// Explain renders the bounded human-readable account: entity and
// event counts only — never candidate, consent or purpose content.
func (a *Aggregate) Explain() string {
	if a == nil {
		return "recruiting: missing aggregate"
	}
	return fmt.Sprintf("recruiting requisitions=%d postings=%d applications=%d candidacies=%d events=%d outbox=%d",
		len(a.Requisitions), len(a.Postings), len(a.Applications), len(a.Candidacies), len(a.Events), len(a.Outbox))
}

// EventKinds returns the ordered event-kind trace for audit and tests.
func (a *Aggregate) EventKinds() []string {
	if a == nil {
		return nil
	}
	kinds := make([]string, 0, len(a.Events))
	for _, event := range a.Events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

// SortedIDs returns the sorted entity IDs per aggregate family for
// deterministic inspection.
func (a *Aggregate) SortedIDs() map[string][]string {
	out := make(map[string][]string)
	if a == nil {
		return out
	}
	collect := func(ids map[string][]string, family string, keys []string) map[string][]string {
		sort.Strings(keys)
		ids[family] = keys
		return ids
	}
	requisitions := make([]string, 0, len(a.Requisitions))
	for id := range a.Requisitions {
		requisitions = append(requisitions, id)
	}
	out = collect(out, "requisition", requisitions)
	postings := make([]string, 0, len(a.Postings))
	for id := range a.Postings {
		postings = append(postings, id)
	}
	out = collect(out, "posting", postings)
	applications := make([]string, 0, len(a.Applications))
	for id := range a.Applications {
		applications = append(applications, id)
	}
	out = collect(out, "application", applications)
	candidacies := make([]string, 0, len(a.Candidacies))
	for id := range a.Candidacies {
		candidacies = append(candidacies, id)
	}
	out = collect(out, "candidacy", candidacies)
	return out
}
