// Package workorder owns the pure business record for project field work.
// Persistence, authorization, workflow execution, reporting, and billing
// adapters belong outside this package.
package workorder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalid                   = errors.New("workorder: invalid input")
	ErrStale                     = errors.New("workorder: stale revision")
	ErrConflict                  = errors.New("workorder: idempotency conflict")
	ErrTransition                = errors.New("workorder: phase transition refused")
	ErrNotFound                  = errors.New("workorder: record not found")
	ErrNoteVisibilityUnsupported = errors.New("workorder: restricted note visibility unavailable")
)

type Phase string

const (
	PhaseDraft         Phase = "DRAFT"
	PhaseAuthorization Phase = "AUTHORIZATION"
	PhaseReady         Phase = "READY"
	PhaseExecution     Phase = "EXECUTION"
	PhaseInspection    Phase = "INSPECTION"
	PhaseAccepted      Phase = "ACCEPTED"
	PhaseClosed        Phase = "CLOSED"
	PhaseCancelled     Phase = "CANCELLED"
)

type RequestKind string

const (
	RequestBudget        RequestKind = "BUDGET"
	RequestBudgetChange  RequestKind = "BUDGET_CHANGE"
	RequestApproval      RequestKind = "APPROVAL"
	RequestCrew          RequestKind = "CREW"
	RequestMaterial      RequestKind = "MATERIAL"
	RequestEquipment     RequestKind = "EQUIPMENT"
	RequestInspection    RequestKind = "INSPECTION"
	RequestDocument      RequestKind = "DOCUMENT"
	RequestChangeOrder   RequestKind = "CHANGE_ORDER"
	RequestBillingReview RequestKind = "BILLING_REVIEW"
)

type RequestStatus string

const (
	RequestPending  RequestStatus = "PENDING"
	RequestApproved RequestStatus = "APPROVED"
	RequestRejected RequestStatus = "REJECTED"
	RequestReturned RequestStatus = "RETURNED"
)

type RecordKind string

const (
	RecordProgress RecordKind = "PROGRESS"
	RecordSpend    RecordKind = "SPEND"
)

type CreateInput struct {
	ID, TenantID, ProjectID, TemplateID, TemplateVersion, TemplateDigest string
	Title, Scope, SupervisorID                                           string
	InitiatorID, ActorID, CommandDigest                                  string
	WorkflowID, WorkflowVersion, WorkflowDigest, WorkflowInstanceID      string
	LinkedTaskIDs                                                        []string
	InitialPhase                                                         Phase
	Phases                                                               []Phase
	Transitions                                                          []PhaseTransition
	Now                                                                  time.Time
	IdempotencyKey                                                       string
}
type TransitionInput struct {
	ExpectedRevision        uint64
	Target                  Phase
	Reason                  string
	EvidenceRefs            []string
	ActorID, IdempotencyKey string
	CommandDigest           string
	Now                     time.Time
}
type InitiatorRequestInput struct {
	ID                                                      string
	DefinitionID                                            string
	FormValues                                              map[string]string
	Kind                                                    RequestKind
	Subject, Rationale, ActorID, IdempotencyKey             string
	Amount                                                  values.Decimal
	Currency, CostCategory, FundingSource, BaselineRevision string
	Unit, NeededUntil, EstimatedCostCurrency                string
	EstimatedCost, QuantityDelta, PriceDelta                values.Decimal
	EstimatedCostSpecified, PriceDeltaSpecified             bool
	QuantityDeltaUnit, PriceDeltaCurrency, ScheduleDelta    string
	Quantity                                                values.Decimal
	RoleOrItem, Location, DueWindow, Policy, ApproverClass  string
	ProposedAction, EvidenceRef                             string
	EvidenceRefs                                            []string
	ScopeDelta, PricingVersion, BillingPeriod               string
	Now                                                     time.Time
	ExpectedRevision                                        uint64
	CommandDigest                                           string
}

// MarshalJSON makes optional exact-decimal request values explicit as zero
// when the request kind does not use them, so the command remains digestible.
func (in InitiatorRequestInput) MarshalJSON() ([]byte, error) {
	type wire InitiatorRequestInput
	copy := wire(in)
	if copy.Amount.Validate() != nil {
		copy.Amount = zeroDecimal()
	}
	if copy.Quantity.Validate() != nil {
		copy.Quantity = zeroDecimal()
	}
	if copy.EstimatedCost.Validate() != nil {
		copy.EstimatedCost = zeroDecimal()
	}
	if copy.QuantityDelta.Validate() != nil {
		copy.QuantityDelta = zeroDecimal()
	}
	if copy.PriceDelta.Validate() != nil {
		copy.PriceDelta = zeroDecimal()
	}
	copy.EstimatedCostSpecified = in.EstimatedCostSpecified || in.EstimatedCost.Validate() == nil
	copy.PriceDeltaSpecified = in.PriceDeltaSpecified || in.PriceDelta.Validate() == nil
	return json.Marshal(copy)
}

type RequestDecisionInput struct {
	RequestID                       string
	ExpectedRevision                uint64
	Decision                        RequestStatus
	ActorID, Reason, IdempotencyKey string
	CommandDigest                   string
	Now                             time.Time
}
type NoteInput struct {
	ID, AuthorID, Body, Visibility, Classification, Anchor, IdempotencyKey string
	AttachmentRefs                                                         []string
	ExpectedRevision                                                       uint64
	CommandDigest                                                          string
	Now                                                                    time.Time
}
type AssignmentInput struct {
	ID, WorkerID, Role, Location, ActorID, IdempotencyKey string
	ExpectedRevision                                      uint64
	Start, End                                            time.Time
	CommandDigest                                         string
	Now                                                   time.Time
}
type ProgressInput struct {
	ID, LineID, Unit, ActorID, EvidenceRef, IdempotencyKey string
	EvidenceRefs                                           []string
	Quantity                                               values.Decimal
	ExpectedRevision                                       uint64
	CommandDigest                                          string
	Now                                                    time.Time
}
type SpendInput struct {
	ID, Category, Description, Currency, ActorID, SourceRef, IdempotencyKey string
	Unit                                                                    string
	Quantity                                                                values.Decimal
	QuantitySpecified                                                       bool
	Disposition                                                             SpendDisposition
	Amount                                                                  values.Decimal
	ExpectedRevision                                                        uint64
	CommandDigest                                                           string
	Now                                                                     time.Time
}
type WorkEntryInput struct {
	ID, WorkerID, Kind, WorkDate, TimeZone, LineID, Unit, Description, SourceRef, CorrectionOfEntryID, ActorID, IdempotencyKey string
	DurationMinutes                                                                                                            uint32
	Quantity                                                                                                                   values.Decimal
	QuantitySpecified                                                                                                          bool
	ExpectedRevision                                                                                                           uint64
	CommandDigest                                                                                                              string
	Now                                                                                                                        time.Time
}

func (in WorkEntryInput) MarshalJSON() ([]byte, error) {
	type wire WorkEntryInput
	copy := wire(in)
	if copy.Quantity.Validate() != nil {
		copy.Quantity = zeroDecimal()
	}
	copy.QuantitySpecified = in.QuantitySpecified || in.Quantity.Validate() == nil
	return json.Marshal(copy)
}

func (in SpendInput) MarshalJSON() ([]byte, error) {
	type wire SpendInput
	copy := wire(in)
	if copy.Quantity.Validate() != nil {
		copy.Quantity = zeroDecimal()
	}
	copy.QuantitySpecified = in.QuantitySpecified || in.Quantity.Validate() == nil
	return json.Marshal(copy)
}

type CorrectionInput struct {
	ID, TargetRecordID, Reason, ActorID, IdempotencyKey string
	ReplacementAmount                                   *values.Decimal
	ExpectedRevision                                    uint64
	CommandDigest                                       string
	Now                                                 time.Time
}

type InitiatorRequest struct {
	ID                                                                                                                             string
	DefinitionID                                                                                                                   string
	FormValues                                                                                                                     map[string]string
	Kind                                                                                                                           RequestKind
	Status                                                                                                                         RequestStatus
	Subject, Rationale, RequesterID                                                                                                string
	Amount                                                                                                                         values.Decimal
	Currency, CostCategory, FundingSource, BaselineRevision                                                                        string
	Unit, NeededUntil, EstimatedCostCurrency, QuantityDeltaUnit, PriceDeltaCurrency, ScheduleDelta                                 string
	EstimatedCost, QuantityDelta, PriceDelta                                                                                       values.Decimal
	EstimatedCostSpecified, PriceDeltaSpecified                                                                                    bool
	Quantity                                                                                                                       values.Decimal
	RoleOrItem, Location, DueWindow, Policy, ApproverClass, ProposedAction, EvidenceRef, ScopeDelta, PricingVersion, BillingPeriod string
	EvidenceRefs                                                                                                                   []string
	CreatedAt, DecidedAt                                                                                                           time.Time
	DecisionActor, DecisionReason                                                                                                  string
}
type Note struct {
	ID, AuthorID, Body, Visibility, Classification, Anchor string
	AttachmentRefs                                         []string
	At                                                     time.Time
}
type Assignment struct {
	ID, WorkerID, Role, Location string
	Start, End, CreatedAt        time.Time
}
type Progress struct {
	ID, LineID, Unit, ActorID, EvidenceRef string
	EvidenceRefs                           []string
	Quantity                               values.Decimal
	At                                     time.Time
}
type Spend struct {
	ID, Category, Description, Currency, ActorID, SourceRef string
	Unit                                                    string
	Quantity                                                values.Decimal
	QuantitySpecified                                       bool
	Disposition                                             SpendDisposition
	Amount                                                  values.Decimal
	At                                                      time.Time
}
type SpendDisposition string

const (
	SpendCommitment SpendDisposition = "COMMITMENT"
	SpendIncurred   SpendDisposition = "INCURRED"
)

type WorkEntry struct {
	ID, WorkerID, Kind, WorkDate, TimeZone, LineID, Unit, Description, SourceRef, CorrectionOfEntryID, ActorID string
	DurationMinutes                                                                                            uint32
	Quantity                                                                                                   values.Decimal
	QuantitySpecified                                                                                          bool
	At                                                                                                         time.Time
}
type Correction struct {
	ID, TargetRecordID, Reason, ActorID string
	ReplacementAmount                   *values.Decimal
	At                                  time.Time
}
type PhaseTransition struct{ From, To Phase }
type Event struct {
	Revision                                                       uint64
	Type, ActorID, RecordID, Detail, IdempotencyKey, CommandDigest string
	FromPhase, ToPhase                                             Phase
	At                                                             time.Time
}
type Snapshot struct {
	ID, TenantID, ProjectID, TemplateID, TemplateVersion, TemplateDigest string
	Title, Scope, SupervisorID, InitiatorID                              string
	WorkflowID, WorkflowVersion, WorkflowDigest, WorkflowInstanceID      string
	CreatedAt, UpdatedAt                                                 time.Time
	LinkedTaskIDs                                                        []string
	Phase                                                                Phase
	Revision                                                             uint64
	Transitions                                                          []PhaseTransition
	Requests                                                             []InitiatorRequest
	Notes                                                                []Note
	Assignments                                                          []Assignment
	Progress                                                             []Progress
	Spending                                                             []Spend
	WorkEntries                                                          []WorkEntry
	Corrections                                                          []Correction
	Journal                                                              []Event
}

type WorkOrder struct {
	s           Snapshot
	phases      map[Phase]map[Phase]bool
	keys        map[string]string
	records     map[string]bool
	correctable map[string]bool
}

func NewWorkOrder(in CreateInput) (*WorkOrder, error) {
	if !nonempty(in.ID, in.TenantID, in.ProjectID, in.TemplateID, in.TemplateVersion, in.TemplateDigest, in.ActorID, in.IdempotencyKey) || in.Now.IsZero() {
		return nil, ErrInvalid
	}
	workflowBound := nonempty(in.WorkflowID, in.WorkflowVersion, in.WorkflowDigest, in.WorkflowInstanceID)
	if !workflowBound && (in.WorkflowID != "" || in.WorkflowVersion != "" || in.WorkflowDigest != "" || in.WorkflowInstanceID != "") {
		return nil, ErrInvalid
	}
	phases := append([]Phase(nil), in.Phases...)
	if len(phases) == 0 {
		phases = []Phase{PhaseDraft, PhaseAuthorization, PhaseReady, PhaseExecution, PhaseInspection, PhaseAccepted, PhaseClosed, PhaseCancelled}
	}
	allowed := map[Phase]bool{}
	for _, p := range phases {
		if p == "" || allowed[p] {
			return nil, ErrInvalid
		}
		allowed[p] = true
	}
	initial := in.InitialPhase
	if initial == "" {
		initial = PhaseDraft
	}
	if !allowed[initial] {
		return nil, ErrInvalid
	}
	transitions := map[Phase]map[Phase]bool{}
	for _, p := range phases {
		transitions[p] = map[Phase]bool{}
	}
	edges := append([]PhaseTransition(nil), in.Transitions...)
	if len(edges) == 0 {
		for i, p := range phases {
			if i+1 < len(phases) && phases[i+1] != PhaseCancelled {
				edges = append(edges, PhaseTransition{From: p, To: phases[i+1]})
			}
			if p != PhaseClosed && p != PhaseCancelled && allowed[PhaseCancelled] {
				edges = append(edges, PhaseTransition{From: p, To: PhaseCancelled})
			}
		}
	}
	seenTaskIDs := map[string]bool{}
	for _, taskID := range in.LinkedTaskIDs {
		if strings.TrimSpace(taskID) == "" || seenTaskIDs[taskID] {
			return nil, ErrInvalid
		}
		seenTaskIDs[taskID] = true
	}
	seenEdges := map[string]bool{}
	for _, edge := range edges {
		key := string(edge.From) + "\x00" + string(edge.To)
		if !allowed[edge.From] || !allowed[edge.To] || edge.From == edge.To || seenEdges[key] {
			return nil, ErrInvalid
		}
		seenEdges[key] = true
		transitions[edge.From][edge.To] = true
	}
	digest := in.CommandDigest
	if digest == "" {
		digest = "CREATE\x00" + in.ID
	}
	w := &WorkOrder{s: Snapshot{ID: in.ID, TenantID: in.TenantID, ProjectID: in.ProjectID, TemplateID: in.TemplateID, TemplateVersion: in.TemplateVersion, TemplateDigest: in.TemplateDigest, Title: in.Title, Scope: in.Scope, SupervisorID: in.SupervisorID, InitiatorID: in.InitiatorID, WorkflowID: in.WorkflowID, WorkflowVersion: in.WorkflowVersion, WorkflowDigest: in.WorkflowDigest, WorkflowInstanceID: in.WorkflowInstanceID, LinkedTaskIDs: append([]string(nil), in.LinkedTaskIDs...), Phase: initial, Revision: 1, CreatedAt: in.Now.UTC(), UpdatedAt: in.Now.UTC(), Transitions: edges}, phases: transitions, keys: map[string]string{}, records: map[string]bool{}, correctable: map[string]bool{}}
	w.s.Journal = append(w.s.Journal, Event{Revision: 1, Type: "CREATED", ActorID: in.ActorID, Detail: string(initial), ToPhase: initial, IdempotencyKey: in.IdempotencyKey, CommandDigest: digest, At: in.Now.UTC()})
	w.keys[scopedIdempotencyKey(in.ActorID, in.IdempotencyKey)] = digest
	return w, nil
}

// Restore reconstructs the pure aggregate from its persisted snapshot. The
// pinned transitions and journal keys are part of the snapshot so replays
// preserve the same workflow contract and deduplication decisions.
func Restore(s Snapshot) (*WorkOrder, error) {
	if !nonempty(s.ID, s.TenantID, s.ProjectID, s.TemplateID, s.TemplateVersion, s.TemplateDigest) || s.Revision == 0 || s.Phase == "" || s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() || len(s.Journal) == 0 || s.Journal[len(s.Journal)-1].Revision != s.Revision {
		return nil, ErrInvalid
	}
	workflowBound := nonempty(s.WorkflowID, s.WorkflowVersion, s.WorkflowDigest, s.WorkflowInstanceID)
	if !workflowBound && (s.WorkflowID != "" || s.WorkflowVersion != "" || s.WorkflowDigest != "" || s.WorkflowInstanceID != "") {
		return nil, ErrInvalid
	}
	allowed := map[Phase]bool{s.Phase: true}
	for _, edge := range s.Transitions {
		allowed[edge.From] = true
		allowed[edge.To] = true
	}
	transitions := map[Phase]map[Phase]bool{}
	for p := range allowed {
		transitions[p] = map[Phase]bool{}
	}
	for _, edge := range s.Transitions {
		if edge.From == "" || edge.To == "" || edge.From == edge.To || !allowed[edge.From] || !allowed[edge.To] || transitions[edge.From][edge.To] {
			return nil, ErrInvalid
		}
		transitions[edge.From][edge.To] = true
	}
	if !allowed[s.Phase] {
		return nil, ErrInvalid
	}
	w := &WorkOrder{s: cloneSnapshot(s), phases: transitions, keys: map[string]string{}, records: map[string]bool{}, correctable: map[string]bool{}}
	phase := s.Journal[0].ToPhase
	if s.Journal[0].Type != "CREATED" || s.Journal[0].Revision != 1 || s.Journal[0].ActorID == "" || phase == "" || !allowed[phase] {
		return nil, ErrInvalid
	}
	for i, e := range s.Journal {
		if e.Revision != uint64(i+1) || e.IdempotencyKey == "" || e.CommandDigest == "" || e.ActorID == "" || e.At.IsZero() || e.Type == "" {
			return nil, ErrInvalid
		}
		if i > 0 && e.Type == "PHASE_TRANSITION" {
			if e.FromPhase != phase || !transitions[e.FromPhase][e.ToPhase] {
				return nil, ErrInvalid
			}
			phase = e.ToPhase
		}
		key := scopedIdempotencyKey(e.ActorID, e.IdempotencyKey)
		if _, ok := w.keys[key]; ok {
			return nil, ErrInvalid
		}
		w.keys[key] = e.CommandDigest
	}
	if phase != s.Phase || !s.CreatedAt.Equal(s.Journal[0].At) || !s.UpdatedAt.Equal(s.Journal[len(s.Journal)-1].At) {
		return nil, ErrInvalid
	}
	for _, r := range s.Requests {
		if r.ID == "" || r.DefinitionID == "" || w.records[r.ID] {
			return nil, ErrInvalid
		}
		if r.Amount.Validate() != nil || r.Quantity.Validate() != nil || r.EstimatedCost.Validate() != nil || r.QuantityDelta.Validate() != nil || r.PriceDelta.Validate() != nil {
			return nil, ErrInvalid
		}
		w.records[r.ID] = true
	}
	for _, n := range s.Notes {
		if n.ID == "" || w.records[n.ID] {
			return nil, ErrInvalid
		}
		if n.Visibility != "PARTICIPANTS" {
			return nil, ErrNoteVisibilityUnsupported
		}
		w.records[n.ID] = true
	}
	for _, a := range s.Assignments {
		if a.ID == "" || w.records[a.ID] {
			return nil, ErrInvalid
		}
		w.records[a.ID] = true
	}
	for _, p := range s.Progress {
		if p.ID == "" || w.records[p.ID] {
			return nil, ErrInvalid
		}
		if p.Quantity.Validate() != nil || p.Quantity.Sign() <= 0 {
			return nil, ErrInvalid
		}
		w.records[p.ID] = true
		w.correctable[p.ID] = true
	}
	for _, p := range s.Spending {
		if p.ID == "" || w.records[p.ID] {
			return nil, ErrInvalid
		}
		if p.Amount.Validate() != nil || p.Amount.Sign() <= 0 || p.Quantity.Validate() != nil || (p.Disposition != SpendCommitment && p.Disposition != SpendIncurred) {
			return nil, ErrInvalid
		}
		w.records[p.ID] = true
		w.correctable[p.ID] = true
	}
	for _, entry := range s.WorkEntries {
		if entry.ID == "" || w.records[entry.ID] {
			return nil, ErrInvalid
		}
		if entry.Quantity.Validate() != nil || !validWorkEntry(entry) {
			return nil, ErrInvalid
		}
		w.records[entry.ID] = true
		w.correctable[entry.ID] = true
	}
	for _, c := range s.Corrections {
		if c.ID == "" || w.records[c.ID] {
			return nil, ErrInvalid
		}
		w.records[c.ID] = true
	}
	return w, nil
}

func validWorkEntry(entry WorkEntry) bool {
	if entry.QuantitySpecified && entry.Quantity.Sign() <= 0 {
		return false
	}
	switch entry.Kind {
	case "LABOR":
		return entry.DurationMinutes > 0 && (!entry.QuantitySpecified || entry.Unit != "")
	case "MATERIAL", "EQUIPMENT", "SUBCONTRACT":
		return entry.QuantitySpecified && entry.Quantity.Sign() > 0 && entry.Unit != "" && entry.SourceRef != ""
	case "OTHER":
		return entry.DurationMinutes > 0 || entry.QuantitySpecified && entry.Quantity.Sign() > 0
	default:
		return false
	}
}

func (w *WorkOrder) Snapshot() Snapshot {
	return cloneSnapshot(w.s)
}

func cloneSnapshot(source Snapshot) Snapshot {
	s := source
	s.LinkedTaskIDs = append([]string(nil), source.LinkedTaskIDs...)
	s.Transitions = append([]PhaseTransition(nil), s.Transitions...)
	s.Requests = append([]InitiatorRequest(nil), s.Requests...)
	s.Notes = append([]Note(nil), s.Notes...)
	for i := range s.Notes {
		s.Notes[i].AttachmentRefs = append([]string(nil), s.Notes[i].AttachmentRefs...)
	}
	for i := range s.Requests {
		if s.Requests[i].FormValues != nil {
			form := make(map[string]string, len(s.Requests[i].FormValues))
			for k, v := range s.Requests[i].FormValues {
				form[k] = v
			}
			s.Requests[i].FormValues = form
		}
		s.Requests[i].EvidenceRefs = append([]string(nil), source.Requests[i].EvidenceRefs...)
	}
	s.Assignments = append([]Assignment(nil), s.Assignments...)
	s.Progress = append([]Progress(nil), s.Progress...)
	for i := range s.Progress {
		s.Progress[i].EvidenceRefs = append([]string(nil), source.Progress[i].EvidenceRefs...)
	}
	s.Spending = append([]Spend(nil), s.Spending...)
	s.WorkEntries = append([]WorkEntry(nil), source.WorkEntries...)
	s.Corrections = append([]Correction(nil), s.Corrections...)
	for i := range s.Corrections {
		if source.Corrections[i].ReplacementAmount != nil {
			amount := *source.Corrections[i].ReplacementAmount
			s.Corrections[i].ReplacementAmount = &amount
		}
	}
	s.Journal = append([]Event(nil), s.Journal...)
	return s
}
func (w *WorkOrder) RequestPhaseTransition(in TransitionInput) error {
	d := join("TRANSITION", string(in.Target), in.Reason, in.ActorID, strings.Join(in.EvidenceRefs, "\x1e"))
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.ActorID, in.Reason) || !w.phases[w.s.Phase][in.Target] {
		return ErrTransition
	}
	for _, r := range in.EvidenceRefs {
		if strings.TrimSpace(r) == "" {
			return ErrInvalid
		}
	}
	from := w.s.Phase
	w.s.Phase = in.Target
	w.commit(in.IdempotencyKey, d, Event{Type: "PHASE_TRANSITION", ActorID: in.ActorID, FromPhase: from, ToPhase: in.Target, Detail: string(from) + " -> " + string(in.Target) + "; " + in.Reason, At: in.Now.UTC()})
	return nil
}
func (w *WorkOrder) SubmitRequest(in InitiatorRequestInput) error {
	formValues := canonicalFormValues(in.FormValues)
	d := join("REQUEST", in.ID, in.DefinitionID, formValues, string(in.Kind), in.Subject, in.Rationale, in.ActorID, in.Amount.String(), in.Currency, in.CostCategory, in.FundingSource, in.BaselineRevision, in.Quantity.String(), in.Unit, in.NeededUntil, in.EstimatedCost.String(), in.EstimatedCostCurrency, fmt.Sprint(in.EstimatedCostSpecified), in.QuantityDelta.String(), in.QuantityDeltaUnit, in.PriceDelta.String(), in.PriceDeltaCurrency, fmt.Sprint(in.PriceDeltaSpecified), in.ScheduleDelta, in.RoleOrItem, in.Location, in.DueWindow, in.Policy, in.ApproverClass, in.ProposedAction, in.EvidenceRef, canonicalStringSlice(in.EvidenceRefs), in.ScopeDelta, in.PricingVersion, in.BillingPeriod)
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if !nonempty(in.ID, in.DefinitionID, in.ActorID, in.Subject, in.Rationale) || in.Now.IsZero() || !validRequestKind(in.Kind) {
		return ErrInvalid
	}
	for _, r := range w.s.Requests {
		if r.ID == in.ID {
			return ErrConflict
		}
	}
	if in.Kind == RequestBudget || in.Kind == RequestBudgetChange {
		if err := positive(in.Amount); err != nil || !validCurrency(in.Currency) || !nonempty(in.CostCategory, in.FundingSource, in.BaselineRevision) {
			return ErrInvalid
		}
	}
	if in.Kind == RequestCrew || in.Kind == RequestMaterial || in.Kind == RequestEquipment {
		if err := positive(in.Quantity); err != nil || !nonempty(in.RoleOrItem, in.Location, in.Unit) {
			return ErrInvalid
		}
		if in.NeededUntil != "" {
			if _, err := time.Parse(time.RFC3339, in.NeededUntil); err != nil {
				return ErrInvalid
			}
		}
		if (in.EstimatedCostSpecified && in.EstimatedCost.Validate() != nil) || (in.EstimatedCost.Validate() == nil && (!validCurrency(in.EstimatedCostCurrency) || in.EstimatedCost.Sign() < 0)) {
			return ErrInvalid
		}
	}
	if in.Kind == RequestApproval {
		if !nonempty(in.Policy, in.ApproverClass, in.ProposedAction) {
			return ErrInvalid
		}
	}
	if in.Kind == RequestChangeOrder && !nonempty(in.ScopeDelta, in.BaselineRevision) {
		return ErrInvalid
	}
	if in.Kind == RequestBillingReview && !nonempty(in.BillingPeriod, in.PricingVersion) {
		return ErrInvalid
	}
	amount, quantity := in.Amount, in.Quantity
	if amount.Validate() != nil {
		amount = zeroDecimal()
	}
	if quantity.Validate() != nil {
		quantity = zeroDecimal()
	}
	estimatedCost, quantityDelta, priceDelta := in.EstimatedCost, in.QuantityDelta, in.PriceDelta
	if estimatedCost.Validate() != nil {
		estimatedCost = zeroDecimal()
	}
	if quantityDelta.Validate() != nil {
		quantityDelta = zeroDecimal()
	}
	if priceDelta.Validate() != nil {
		priceDelta = zeroDecimal()
	}
	for _, ref := range in.EvidenceRefs {
		if strings.TrimSpace(ref) == "" {
			return ErrInvalid
		}
	}
	r := InitiatorRequest{ID: in.ID, DefinitionID: in.DefinitionID, FormValues: cloneStringMap(in.FormValues), Kind: in.Kind, Status: RequestPending, Subject: in.Subject, Rationale: in.Rationale, RequesterID: in.ActorID, Amount: amount, Currency: in.Currency, CostCategory: in.CostCategory, FundingSource: in.FundingSource, BaselineRevision: in.BaselineRevision, Quantity: quantity, Unit: in.Unit, NeededUntil: in.NeededUntil, EstimatedCost: estimatedCost, EstimatedCostCurrency: in.EstimatedCostCurrency, EstimatedCostSpecified: in.EstimatedCostSpecified || in.EstimatedCost.Validate() == nil, QuantityDelta: quantityDelta, QuantityDeltaUnit: in.QuantityDeltaUnit, PriceDelta: priceDelta, PriceDeltaCurrency: in.PriceDeltaCurrency, PriceDeltaSpecified: in.PriceDeltaSpecified || in.PriceDelta.Validate() == nil, ScheduleDelta: in.ScheduleDelta, RoleOrItem: in.RoleOrItem, Location: in.Location, DueWindow: in.DueWindow, Policy: in.Policy, ApproverClass: in.ApproverClass, ProposedAction: in.ProposedAction, EvidenceRef: in.EvidenceRef, EvidenceRefs: append([]string(nil), in.EvidenceRefs...), ScopeDelta: in.ScopeDelta, PricingVersion: in.PricingVersion, BillingPeriod: in.BillingPeriod, CreatedAt: in.Now.UTC()}
	w.s.Requests = append(w.s.Requests, r)
	w.records[in.ID] = true
	w.commit(in.IdempotencyKey, d, Event{Type: "REQUEST_SUBMITTED", ActorID: in.ActorID, RecordID: in.ID, Detail: string(in.Kind), At: in.Now.UTC()})
	return nil
}
func (w *WorkOrder) DecideRequest(in RequestDecisionInput) error {
	digest := join("DECISION", in.RequestID, string(in.Decision), in.Reason, in.ActorID)
	if in.CommandDigest != "" {
		digest = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, digest); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.RequestID, in.ActorID, in.Reason) || (in.Decision != RequestApproved && in.Decision != RequestRejected && in.Decision != RequestReturned) {
		return ErrInvalid
	}
	i := -1
	for n := range w.s.Requests {
		if w.s.Requests[n].ID == in.RequestID {
			i = n
			break
		}
	}
	if i < 0 {
		return ErrNotFound
	}
	if w.s.Requests[i].Status != RequestPending {
		return ErrTransition
	}
	if w.s.Requests[i].RequesterID == in.ActorID {
		return ErrTransition
	}
	w.s.Requests[i].Status = in.Decision
	w.s.Requests[i].DecisionActor = in.ActorID
	w.s.Requests[i].DecisionReason = in.Reason
	w.s.Requests[i].DecidedAt = in.Now.UTC()
	w.commit(in.IdempotencyKey, digest, Event{Type: "REQUEST_DECIDED", ActorID: in.ActorID, RecordID: in.RequestID, Detail: string(in.Decision) + "; " + in.Reason, At: in.Now.UTC()})
	return nil
}
func (w *WorkOrder) AddNote(in NoteInput) error {
	d := join("NOTE", in.ID, in.AuthorID, in.Body, in.Visibility, in.Classification, in.Anchor, strings.Join(in.AttachmentRefs, "\x1e"))
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.AuthorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.ID, in.AuthorID, in.Body, in.Visibility, in.Classification) {
		return ErrInvalid
	}
	if in.Visibility != "PARTICIPANTS" {
		return ErrNoteVisibilityUnsupported
	}
	for _, n := range w.s.Notes {
		if n.ID == in.ID {
			return ErrConflict
		}
	}
	w.s.Notes = append(w.s.Notes, Note{ID: in.ID, AuthorID: in.AuthorID, Body: in.Body, Visibility: in.Visibility, Classification: in.Classification, Anchor: in.Anchor, AttachmentRefs: append([]string(nil), in.AttachmentRefs...), At: in.Now.UTC()})
	w.records[in.ID] = true
	w.commit(in.IdempotencyKey, d, Event{Type: "NOTE_ADDED", ActorID: in.AuthorID, RecordID: in.ID, Detail: in.Visibility, At: in.Now.UTC()})
	return nil
}
func (w *WorkOrder) Assign(in AssignmentInput) error {
	d := join("ASSIGN", in.ID, in.WorkerID, in.Role, in.Location, in.Start.UTC().Format(time.RFC3339Nano), in.End.UTC().Format(time.RFC3339Nano))
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.ID, in.WorkerID, in.Role, in.ActorID) || (!in.Start.IsZero() && !in.End.IsZero() && !in.End.After(in.Start)) {
		return ErrInvalid
	}
	for _, a := range w.s.Assignments {
		if a.ID == in.ID {
			return ErrConflict
		}
	}
	w.s.Assignments = append(w.s.Assignments, Assignment{ID: in.ID, WorkerID: in.WorkerID, Role: in.Role, Location: in.Location, Start: in.Start, End: in.End, CreatedAt: in.Now.UTC()})
	w.records[in.ID] = true
	w.commit(in.IdempotencyKey, d, Event{Type: "ASSIGNED", ActorID: in.ActorID, RecordID: in.ID, Detail: in.Role, At: in.Now.UTC()})
	return nil
}
func (w *WorkOrder) RecordProgress(in ProgressInput) error {
	d := join("PROGRESS", in.ID, in.LineID, in.Unit, in.ActorID, in.EvidenceRef, canonicalStringSlice(in.EvidenceRefs), in.Quantity.String())
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.ID, in.LineID, in.Unit, in.ActorID) {
		return ErrInvalid
	}
	if err := positive(in.Quantity); err != nil {
		return ErrInvalid
	}
	if w.records[in.ID] {
		return ErrConflict
	}
	for _, ref := range in.EvidenceRefs {
		if strings.TrimSpace(ref) == "" {
			return ErrInvalid
		}
	}
	w.s.Progress = append(w.s.Progress, Progress{ID: in.ID, LineID: in.LineID, Unit: in.Unit, ActorID: in.ActorID, EvidenceRef: in.EvidenceRef, EvidenceRefs: append([]string(nil), in.EvidenceRefs...), Quantity: in.Quantity, At: in.Now.UTC()})
	w.records[in.ID] = true
	w.correctable[in.ID] = true
	w.commit(in.IdempotencyKey, d, Event{Type: "PROGRESS_RECORDED", ActorID: in.ActorID, RecordID: in.ID, Detail: in.Quantity.String() + " " + in.Unit, At: in.Now.UTC()})
	return nil
}
func (w *WorkOrder) RecordSpend(in SpendInput) error {
	d := join("SPEND", in.ID, in.Category, in.Description, in.Currency, in.ActorID, in.SourceRef, string(in.Disposition), in.Unit, in.Quantity.String(), fmt.Sprint(in.QuantitySpecified), in.Amount.String())
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.ID, in.Category, in.Currency, in.ActorID, in.SourceRef) || !validCurrency(in.Currency) || (in.Disposition != SpendCommitment && in.Disposition != SpendIncurred) {
		return ErrInvalid
	}
	if err := positive(in.Amount); err != nil {
		return ErrInvalid
	}
	quantity := in.Quantity
	quantitySpecified := in.QuantitySpecified || in.Quantity.Validate() == nil
	if quantitySpecified && quantity.Validate() != nil {
		return ErrInvalid
	}
	if quantity.Validate() == nil {
		if quantity.Sign() <= 0 || in.Unit == "" {
			return ErrInvalid
		}
	} else {
		quantity = zeroDecimal()
	}
	if w.records[in.ID] {
		return ErrConflict
	}
	w.s.Spending = append(w.s.Spending, Spend{ID: in.ID, Category: in.Category, Description: in.Description, Currency: in.Currency, ActorID: in.ActorID, SourceRef: in.SourceRef, Disposition: in.Disposition, Unit: in.Unit, Quantity: quantity, QuantitySpecified: quantitySpecified, Amount: in.Amount, At: in.Now.UTC()})
	w.records[in.ID] = true
	w.correctable[in.ID] = true
	w.commit(in.IdempotencyKey, d, Event{Type: "SPEND_RECORDED", ActorID: in.ActorID, RecordID: in.ID, Detail: in.Amount.String() + " " + in.Currency, At: in.Now.UTC()})
	return nil
}
func (w *WorkOrder) RecordWorkEntry(in WorkEntryInput) error {
	d := join("WORK_ENTRY", in.ID, in.WorkerID, in.Kind, in.WorkDate, in.TimeZone, in.LineID, in.Unit, in.Description, in.SourceRef, in.CorrectionOfEntryID, fmt.Sprint(in.DurationMinutes), in.Quantity.String(), fmt.Sprint(in.QuantitySpecified), in.ActorID)
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.ID, in.WorkerID, in.Kind, in.WorkDate, in.TimeZone, in.LineID, in.ActorID) || w.records[in.ID] {
		return ErrInvalid
	}
	if _, err := time.Parse("2006-01-02", in.WorkDate); err != nil {
		return ErrInvalid
	}
	quantitySpecified := in.QuantitySpecified || in.Quantity.Validate() == nil
	if quantitySpecified && in.Quantity.Validate() != nil {
		return ErrInvalid
	}
	switch in.Kind {
	case "LABOR":
		if in.DurationMinutes == 0 || (quantitySpecified && (in.Quantity.Sign() <= 0 || in.Unit == "")) {
			return ErrInvalid
		}
	case "MATERIAL", "EQUIPMENT", "SUBCONTRACT":
		if !quantitySpecified || in.Quantity.Validate() != nil || in.Quantity.Sign() <= 0 || !nonempty(in.Unit, in.SourceRef) {
			return ErrInvalid
		}
	case "OTHER":
		if in.DurationMinutes == 0 && (!quantitySpecified || in.Quantity.Validate() != nil || in.Quantity.Sign() <= 0) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if in.CorrectionOfEntryID != "" {
		target := -1
		for i := range w.s.WorkEntries {
			if w.s.WorkEntries[i].ID == in.CorrectionOfEntryID {
				target = i
				break
			}
		}
		if target < 0 || w.s.WorkEntries[target].WorkerID != in.WorkerID || w.s.WorkEntries[target].Kind != in.Kind {
			return ErrInvalid
		}
	}
	quantity := in.Quantity
	if quantity.Validate() != nil {
		quantity = zeroDecimal()
	}
	w.s.WorkEntries = append(w.s.WorkEntries, WorkEntry{ID: in.ID, WorkerID: in.WorkerID, Kind: in.Kind, WorkDate: in.WorkDate, TimeZone: in.TimeZone, LineID: in.LineID, Unit: in.Unit, Description: in.Description, SourceRef: in.SourceRef, CorrectionOfEntryID: in.CorrectionOfEntryID, ActorID: in.ActorID, DurationMinutes: in.DurationMinutes, Quantity: quantity, QuantitySpecified: quantitySpecified, At: in.Now.UTC()})
	w.records[in.ID] = true
	w.correctable[in.ID] = true
	w.commit(in.IdempotencyKey, d, Event{Type: "WORK_ENTRY_RECORDED", ActorID: in.ActorID, RecordID: in.ID, Detail: in.Kind + " " + in.WorkDate, At: in.Now.UTC()})
	return nil
}

func (w *WorkOrder) CorrectRecord(in CorrectionInput) error {
	replacement := ""
	if in.ReplacementAmount != nil {
		replacement = in.ReplacementAmount.String()
	}
	d := join("CORRECTION", in.ID, in.TargetRecordID, in.Reason, in.ActorID, replacement)
	if in.CommandDigest != "" {
		d = in.CommandDigest
	}
	if err := w.check(in.ExpectedRevision, in.ActorID, in.IdempotencyKey, d); err != nil {
		if errors.Is(err, errReplay) {
			return nil
		}
		return err
	}
	if in.Now.IsZero() || !nonempty(in.ID, in.TargetRecordID, in.Reason, in.ActorID) || !w.correctable[in.TargetRecordID] {
		return ErrInvalid
	}
	if w.records[in.ID] {
		return ErrConflict
	}
	if in.ReplacementAmount != nil && in.ReplacementAmount.Validate() != nil {
		return ErrInvalid
	}
	w.s.Corrections = append(w.s.Corrections, Correction{ID: in.ID, TargetRecordID: in.TargetRecordID, Reason: in.Reason, ActorID: in.ActorID, ReplacementAmount: in.ReplacementAmount, At: in.Now.UTC()})
	w.records[in.ID] = true
	w.commit(in.IdempotencyKey, d, Event{Type: "RECORD_CORRECTED", ActorID: in.ActorID, RecordID: in.ID, Detail: "corrects " + in.TargetRecordID + ": " + in.Reason, At: in.Now.UTC()})
	return nil
}

func (w *WorkOrder) check(rev uint64, actor, key, digest string) error {
	if key == "" {
		return ErrInvalid
	}
	if actor == "" {
		return ErrInvalid
	}
	if old, ok := w.keys[scopedIdempotencyKey(actor, key)]; ok {
		if old == digest {
			return errReplay
		}
		return ErrConflict
	}
	if rev != w.s.Revision {
		return ErrStale
	}
	return nil
}

var errReplay = errors.New("workorder: replay")

func scopedIdempotencyKey(actor, key string) string { return join(actor, key) }

func (w *WorkOrder) commit(key, digest string, e Event) {
	w.s.Revision++
	w.s.UpdatedAt = e.At.UTC()
	e.Revision = w.s.Revision
	e.IdempotencyKey = key
	e.CommandDigest = digest
	w.s.Journal = append(w.s.Journal, e)
	w.keys[scopedIdempotencyKey(e.ActorID, key)] = digest
}
func positive(d values.Decimal) error {
	if d.Validate() != nil || d.Sign() <= 0 {
		return ErrInvalid
	}
	return nil
}
func nonempty(ss ...string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}
func validCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
func join(parts ...string) string { return strings.Join(parts, "\x00") }

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func canonicalFormValues(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var encoded strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&encoded, "%d:%s%d:%s", len(key), key, len(values[key]), values[key])
	}
	return encoded.String()
}
func canonicalStringSlice(values []string) string {
	var encoded strings.Builder
	for _, value := range values {
		fmt.Fprintf(&encoded, "%d:%s", len(value), value)
	}
	return encoded.String()
}

// DigestCommandJSON returns the SHA-256 digest used by the application/store
// boundary for the exact command bytes supplied to the transactional command.
func DigestCommandJSON(command []byte) string {
	digest := sha256.Sum256(command)
	return hex.EncodeToString(digest[:])
}
func validRequestKind(k RequestKind) bool {
	switch k {
	case RequestBudget, RequestBudgetChange, RequestApproval, RequestCrew, RequestMaterial, RequestEquipment, RequestInspection, RequestDocument, RequestChangeOrder, RequestBillingReview:
		return true
	}
	return false
}

func (w *WorkOrder) String() string { return fmt.Sprintf("%s@%d[%s]", w.s.ID, w.s.Revision, w.s.Phase) }

func zeroDecimal() values.Decimal { return values.MustDecimal("0", 0, values.RoundingExactRequired) }
