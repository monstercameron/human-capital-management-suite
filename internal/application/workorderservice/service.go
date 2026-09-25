// Package workorderservice coordinates work order use cases. It keeps trusted
// identity, current authorization, template resolution, worker eligibility,
// and transactional persistence at one application boundary.
package workorderservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrInvalidPrincipal = errors.New("workorderservice: trusted principal required")
	ErrInvalidRequest   = errors.New("workorderservice: invalid request")
	ErrUnavailable      = errors.New("workorderservice: required port unavailable")
	ErrScopeMismatch    = errors.New("workorderservice: work order scope mismatch")
	ErrWorkerIneligible = errors.New("workorderservice: worker is not eligible")
)

// Authorizer must evaluate current project membership and work order grants on
// every call. It must fail closed and must not trust client supplied roles.
type Authorizer interface {
	Authorize(context.Context, *trust.Principal, string, string, workorderaccess.Capability) error
	AuthorizeCreate(context.Context, *trust.Principal, string) error
	AuthorizeList(context.Context, *trust.Principal, string) error
}

// Templates resolves an immutable, published template version within tenant.
type Templates interface {
	ResolvePublished(context.Context, string, string, string) (workordertemplate.Published, error)
}

// WorkerDirectory is backed by the authoritative HCM worker identity and
// eligibility source. Results must be tenant scoped and decision time current.
type WorkerDirectory interface {
	ResolveWorker(context.Context, string, string) (string, bool, error)
	ResolveEligible(context.Context, string, string) (bool, error)
}

// PhaseGate rechecks the running execution workflow, including approvals,
// evidence and other configured gates, against the latest durable instance.
// Implementations must fail closed when the runtime state is unavailable.
type PhaseGate interface {
	EvaluateTransition(context.Context, *trust.Principal, workorder.Snapshot, workorder.TransitionInput) error
}

// Repository owns tenant scoping, durable idempotency, optimistic revisions,
// and an atomic root snapshot + journal/outbox transaction. Execute checks
// prior idempotency before revision conflict and does not invoke mutate on a
// replay. Mutate must only be called while the repository holds its write lock.
type Repository interface {
	Create(context.Context, string, workorder.Snapshot, string, string) error
	Get(context.Context, string, string) (workorder.Snapshot, error)
	Execute(context.Context, string, string, string, string, uint64, string, func(workorder.Snapshot) (workorder.Snapshot, error)) (workorder.Snapshot, error)
}

type Service struct {
	Auth           Authorizer
	Templates      Templates
	Workers        WorkerDirectory
	Orders         Repository
	Phases         PhaseGate
	Reports        ReportEngine
	Pricing        PricingPolicies
	BillingSources BillingSourceProvider
	Artifacts      ArtifactRepository
	CursorKey      []byte
	Clock          func() time.Time
}

type CreateRequest struct {
	ID, ProjectID, Title, Scope, SupervisorID, TemplateID, TemplateVersion, IdempotencyKey string
	LinkedTaskIDs                                                                          []string
}
type ScopedRequest struct{ ProjectID, WorkOrderID, IdempotencyKey string }
type TransitionRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.TransitionInput
}
type InitiatorRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.InitiatorRequestInput
}
type DecisionRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.RequestDecisionInput
}
type NoteRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.NoteInput
}
type AssignmentRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.AssignmentInput
}
type ProgressRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.ProgressInput
}
type SpendRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.SpendInput
}
type CorrectionRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.CorrectionInput
}

func (s Service) Create(ctx context.Context, p *trust.Principal, req CreateRequest) (workorder.Snapshot, error) {
	if err := validPrincipal(p); err != nil {
		return workorder.Snapshot{}, err
	}
	if s.Auth == nil || s.Templates == nil || s.Orders == nil {
		return workorder.Snapshot{}, ErrUnavailable
	}
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.TemplateID) == "" || strings.TrimSpace(req.TemplateVersion) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return workorder.Snapshot{}, ErrInvalidRequest
	}
	tenantID, actor := tenant(p), p.Subject()
	if err := s.Auth.AuthorizeCreate(ctx, p, req.ProjectID); err != nil {
		return workorder.Snapshot{}, err
	}
	pub, err := s.Templates.ResolvePublished(ctx, tenantID, req.TemplateID, req.TemplateVersion)
	if err != nil {
		return workorder.Snapshot{}, err
	}
	if err = pub.Verify(); err != nil {
		return workorder.Snapshot{}, err
	}
	draft := pub.Snapshot()
	phases := make([]workorder.Phase, 0, len(draft.Phases))
	transitions := make([]workorder.PhaseTransition, 0)
	for _, phase := range draft.Phases {
		phases = append(phases, workorder.Phase(phase.ID))
		for _, exit := range phase.AllowedExits {
			transitions = append(transitions, workorder.PhaseTransition{From: workorder.Phase(phase.ID), To: workorder.Phase(exit)})
		}
	}
	if len(phases) == 0 {
		return workorder.Snapshot{}, ErrInvalidRequest
	}
	initial := workorder.PhaseDraft
	foundInitial := false
	for _, phase := range phases {
		if phase == initial {
			foundInitial = true
			break
		}
	}
	if !foundInitial {
		return workorder.Snapshot{}, fmt.Errorf("%w: template must define DRAFT as the initial phase", ErrInvalidRequest)
	}
	id := req.ID
	if id == "" {
		id = stableID(tenantID, actor, "workorder.create", req.IdempotencyKey)
	}
	now := s.now()
	createCommand, err := json.Marshal(struct {
		Operation, ID, TenantID, ActorID, ProjectID, Title, Scope, SupervisorID, TemplateID, TemplateVersion, IdempotencyKey string
		LinkedTaskIDs                                                                                                        []string
	}{"create", id, tenantID, actor, req.ProjectID, req.Title, req.Scope, req.SupervisorID, req.TemplateID, req.TemplateVersion, req.IdempotencyKey, req.LinkedTaskIDs})
	if err != nil {
		return workorder.Snapshot{}, err
	}
	createDigest := workorder.DigestCommandJSON(createCommand)
	agg, err := workorder.NewWorkOrder(workorder.CreateInput{ID: id, TenantID: tenantID, ProjectID: req.ProjectID, Title: req.Title, Scope: req.Scope, SupervisorID: req.SupervisorID, LinkedTaskIDs: req.LinkedTaskIDs, InitiatorID: actor, ActorID: actor, TemplateDigest: pub.Digest(), CommandDigest: createDigest, TemplateID: req.TemplateID, TemplateVersion: req.TemplateVersion, InitialPhase: initial, Phases: phases, Transitions: transitions, Now: now, IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		return workorder.Snapshot{}, err
	}
	created := agg.Snapshot()
	if err = s.Orders.Create(ctx, tenantID, created, actor, req.IdempotencyKey); err != nil {
		return workorder.Snapshot{}, err
	}
	persisted, err := s.Orders.Get(ctx, tenantID, id)
	if err != nil {
		return workorder.Snapshot{}, err
	}
	if persisted.TenantID != tenantID || persisted.ID != id || persisted.ProjectID != req.ProjectID || persisted.TemplateDigest != pub.Digest() {
		return workorder.Snapshot{}, workorder.ErrConflict
	}
	validated, err := workorder.Restore(persisted)
	if err != nil {
		return workorder.Snapshot{}, err
	}
	persisted = validated.Snapshot()
	return s.projectSnapshot(ctx, p, persisted)
}

// Get authorizes the exact tenant/project/order scope before reading the order.
func (s Service) Get(ctx context.Context, p *trust.Principal, req ScopedRequest) (workorder.Snapshot, error) {
	if err := validPrincipal(p); err != nil {
		return workorder.Snapshot{}, err
	}
	if s.Auth == nil || s.Orders == nil {
		return workorder.Snapshot{}, ErrUnavailable
	}
	if err := validScope(req); err != nil {
		return workorder.Snapshot{}, err
	}
	snap, err := s.Orders.Get(ctx, tenant(p), req.WorkOrderID)
	if err != nil {
		return workorder.Snapshot{}, err
	}
	if snap.TenantID != tenant(p) || snap.ID != req.WorkOrderID || (req.ProjectID != "" && snap.ProjectID != req.ProjectID) {
		return workorder.Snapshot{}, workorder.ErrNotFound
	}
	validated, err := workorder.Restore(snap)
	if err != nil {
		return workorder.Snapshot{}, err
	}
	snap = validated.Snapshot()
	if err := s.Auth.Authorize(ctx, p, snap.ProjectID, req.WorkOrderID, workorderaccess.Read); err != nil {
		return workorder.Snapshot{}, err
	}
	return s.projectSnapshot(ctx, p, snap)
}

func (s Service) Transition(ctx context.Context, p *trust.Principal, req TransitionRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.Advance, req.ExpectedRevision, req.IdempotencyKey, req.Input, func(a *workorder.WorkOrder, digest string) error {
		in := req.Input
		in.ActorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.RequestPhaseTransition(in)
	})
}
func (s Service) SubmitRequest(ctx context.Context, p *trust.Principal, req InitiatorRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.Request, req.ExpectedRevision, req.IdempotencyKey, &req.Input, func(a *workorder.WorkOrder, digest string) error {
		in := req.Input
		in.ID = stableID(tenant(p), p.Subject(), "workorder.request:"+req.WorkOrderID, req.IdempotencyKey)
		in.ActorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.SubmitRequest(in)
	})
}
func (s Service) DecideRequest(ctx context.Context, p *trust.Principal, req DecisionRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.Approve, req.ExpectedRevision, req.IdempotencyKey, req.Input, func(a *workorder.WorkOrder, digest string) error {
		in := req.Input
		var requester string
		for _, r := range a.Snapshot().Requests {
			if r.ID == in.RequestID {
				requester = r.RequesterID
				break
			}
		}
		if requester == "" {
			return workorder.ErrNotFound
		}
		if d := workorderaccess.CanDecide(workorderaccess.UserID(requester), workorderaccess.UserID(p.Subject()), true); !d.Allowed {
			return workorderaccess.ErrSelfApproval
		}
		in.ActorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.DecideRequest(in)
	})
}
func (s Service) AddNote(ctx context.Context, p *trust.Principal, req NoteRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.AddNote, req.ExpectedRevision, req.IdempotencyKey, req.Input, func(a *workorder.WorkOrder, digest string) error {
		in := req.Input
		in.ID = stableID(tenant(p), p.Subject(), "workorder.note:"+req.WorkOrderID, req.IdempotencyKey)
		in.AuthorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.AddNote(in)
	})
}
func (s Service) Assign(ctx context.Context, p *trust.Principal, req AssignmentRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.Request, req.ExpectedRevision, req.IdempotencyKey, req.Input, func(a *workorder.WorkOrder, digest string) error {
		if s.Workers == nil {
			return ErrUnavailable
		}
		workerID, found, err := s.Workers.ResolveWorker(ctx, tenant(p), req.Input.WorkerID)
		if err != nil {
			return err
		}
		if !found || workerID == "" {
			return ErrWorkerIneligible
		}
		ok, err := s.Workers.ResolveEligible(ctx, tenant(p), workerID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrWorkerIneligible
		}
		in := req.Input
		in.WorkerID = workerID
		in.ID = stableID(tenant(p), p.Subject(), "workorder.assignment:"+req.WorkOrderID, req.IdempotencyKey)
		in.ActorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.Assign(in)
	})
}
func (s Service) RecordProgress(ctx context.Context, p *trust.Principal, req ProgressRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.Request, req.ExpectedRevision, req.IdempotencyKey, req.Input, func(a *workorder.WorkOrder, digest string) error {
		in := req.Input
		in.ID = stableID(tenant(p), p.Subject(), "workorder.progress:"+req.WorkOrderID, req.IdempotencyKey)
		in.ActorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.RecordProgress(in)
	})
}
func (s Service) RecordSpend(ctx context.Context, p *trust.Principal, req SpendRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.RecordCost, req.ExpectedRevision, req.IdempotencyKey, req.Input, func(a *workorder.WorkOrder, digest string) error {
		in := req.Input
		in.ID = stableID(tenant(p), p.Subject(), "workorder.spend:"+req.WorkOrderID, req.IdempotencyKey)
		in.ActorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.RecordSpend(in)
	})
}
func (s Service) CorrectRecord(ctx context.Context, p *trust.Principal, req CorrectionRequest) (workorder.Snapshot, error) {
	return s.mutate(ctx, p, req.ScopedRequest, workorderaccess.RecordCost, req.ExpectedRevision, req.IdempotencyKey, req.Input, func(a *workorder.WorkOrder, digest string) error {
		in := req.Input
		in.ID = stableID(tenant(p), p.Subject(), "workorder.correction:"+req.WorkOrderID, req.IdempotencyKey)
		in.ActorID = p.Subject()
		in.ExpectedRevision = req.ExpectedRevision
		in.IdempotencyKey = req.IdempotencyKey
		in.Now = s.now()
		in.CommandDigest = digest
		return a.CorrectRecord(in)
	})
}

func (s Service) mutate(ctx context.Context, p *trust.Principal, scope ScopedRequest, cap workorderaccess.Capability, revision uint64, key string, command any, apply func(*workorder.WorkOrder, string) error) (workorder.Snapshot, error) {
	if err := validPrincipal(p); err != nil {
		return workorder.Snapshot{}, err
	}
	if s.Auth == nil || s.Orders == nil {
		return workorder.Snapshot{}, ErrUnavailable
	}
	if err := validScope(scope); err != nil {
		return workorder.Snapshot{}, err
	}
	if key == "" {
		return workorder.Snapshot{}, ErrInvalidRequest
	}
	current, err := s.Orders.Get(ctx, tenant(p), scope.WorkOrderID)
	if err != nil {
		return workorder.Snapshot{}, err
	}
	if current.TenantID != tenant(p) || current.ID != scope.WorkOrderID || (scope.ProjectID != "" && current.ProjectID != scope.ProjectID) {
		return workorder.Snapshot{}, workorder.ErrNotFound
	}
	projectID := current.ProjectID
	if err := s.Auth.Authorize(ctx, p, projectID, scope.WorkOrderID, cap); err != nil {
		return workorder.Snapshot{}, err
	}
	if revision == 0 {
		return workorder.Snapshot{}, ErrInvalidRequest
	}
	var pinned workordertemplate.Published
	switch command.(type) {
	case workorder.TransitionInput, *workorder.InitiatorRequestInput:
		if s.Templates == nil {
			return workorder.Snapshot{}, ErrUnavailable
		}
		pinned, err = s.Templates.ResolvePublished(ctx, tenant(p), current.TemplateID, current.TemplateVersion)
		if err != nil {
			return workorder.Snapshot{}, err
		}
		if err = pinned.Verify(); err != nil {
			return workorder.Snapshot{}, err
		}
		if pinned.Digest() != current.TemplateDigest || pinned.Version() != current.TemplateVersion {
			return workorder.Snapshot{}, workorder.ErrConflict
		}
	}
	if _, ok := command.(workorder.TransitionInput); ok && s.Phases == nil {
		return workorder.Snapshot{}, ErrUnavailable
	}
	commandJSON, err := canonicalJSON(struct {
		Action    workorderaccess.Capability `json:"action"`
		Tenant    string                     `json:"tenant"`
		Project   string                     `json:"project"`
		WorkOrder string                     `json:"work_order"`
		Revision  uint64                     `json:"expected_revision"`
		Input     any                        `json:"input"`
	}{Action: cap, Tenant: tenant(p), Project: projectID, WorkOrder: scope.WorkOrderID, Revision: revision, Input: canonicalCommand(command)})
	if err != nil {
		return workorder.Snapshot{}, err
	}
	digest := workorder.DigestCommandJSON(commandJSON)
	result, err := s.Orders.Execute(ctx, tenant(p), scope.WorkOrderID, p.Subject(), key, revision, digest, func(current workorder.Snapshot) (workorder.Snapshot, error) {
		if current.TenantID != tenant(p) || current.ProjectID != projectID || current.ID != scope.WorkOrderID {
			return workorder.Snapshot{}, ErrScopeMismatch
		}
		if err := s.Auth.Authorize(ctx, p, projectID, scope.WorkOrderID, cap); err != nil {
			return workorder.Snapshot{}, err
		}
		agg, err := workorder.Restore(current)
		if err != nil {
			return workorder.Snapshot{}, err
		}
		switch input := command.(type) {
		case workorder.TransitionInput:
			input.ExpectedRevision, input.ActorID, input.IdempotencyKey = revision, p.Subject(), key
			if err = s.Phases.EvaluateTransition(ctx, p, current, input); err != nil {
				return workorder.Snapshot{}, err
			}
		case *workorder.InitiatorRequestInput:
			if pinned.Digest() != current.TemplateDigest || pinned.Version() != current.TemplateVersion {
				return workorder.Snapshot{}, workorder.ErrConflict
			}
			definitionID, e := templateRequestDefinition(pinned, current.Phase, input.Kind, input.FormValues)
			if e != nil {
				return workorder.Snapshot{}, e
			}
			input.DefinitionID = definitionID
		}
		if err = apply(agg, digest); err != nil {
			return workorder.Snapshot{}, err
		}
		return agg.Snapshot(), nil
	})
	if err != nil {
		return workorder.Snapshot{}, err
	}
	return s.projectSnapshot(ctx, p, result)
}

// projectSnapshot clones a durable value before applying caller-specific
// field visibility. The projected value is only returned to the caller; it is
// never sent to the repository.
func (s Service) projectSnapshot(ctx context.Context, p *trust.Principal, snapshot workorder.Snapshot) (workorder.Snapshot, error) {
	view := cloneSnapshotForProjection(snapshot)
	canCost := true
	if snapshotHasCost(view) {
		canCost = s.Auth.Authorize(ctx, p, view.ProjectID, view.ID, workorderaccess.ViewCost) == nil
	}
	canPay := true
	if snapshotHasPayDetails(view) {
		canPay = s.Auth.Authorize(ctx, p, view.ProjectID, view.ID, workorderaccess.ViewPay) == nil
	}
	if !canCost {
		for i := range view.Requests {
			r := &view.Requests[i]
			if financialRequestKind(r.Kind) {
				// Configurable request content is untrusted free text: rationale,
				// item labels, supplier references, and evidence can all disclose
				// prices. Keep only the workflow receipt/status metadata.
				r.DefinitionID = ""
				r.FormValues = nil
				r.Subject, r.Rationale = "", ""
				r.Amount, r.EstimatedCost = values.Decimal{}, values.Decimal{}
				r.QuantityDelta, r.PriceDelta, r.Quantity = values.Decimal{}, values.Decimal{}, values.Decimal{}
				r.Currency, r.CostCategory, r.FundingSource, r.BaselineRevision = "", "", "", ""
				r.Unit, r.NeededUntil, r.EstimatedCostCurrency = "", "", ""
				r.EstimatedCostSpecified, r.PriceDeltaSpecified = false, false
				r.QuantityDeltaUnit, r.PriceDeltaCurrency, r.ScheduleDelta = "", "", ""
				r.RoleOrItem, r.Location, r.DueWindow, r.Policy = "", "", "", ""
				r.ApproverClass, r.ProposedAction, r.EvidenceRef = "", "", ""
				r.EvidenceRefs = nil
				r.ScopeDelta, r.PricingVersion, r.BillingPeriod = "", "", ""
				r.DecisionReason = ""
			}
		}
		view.Spending = nil
		for i := range view.Corrections {
			view.Corrections[i].ReplacementAmount = nil
			view.Corrections[i].Reason = ""
		}
	}
	if !canPay {
		for i := range view.WorkEntries {
			if strings.EqualFold(view.WorkEntries[i].Kind, "LABOR") {
				view.WorkEntries[i].WorkerID = ""
				view.WorkEntries[i].DurationMinutes = 0
			}
		}
	}
	filteredNotes := view.Notes[:0]
	for _, note := range view.Notes {
		switch note.Visibility {
		case "PARTICIPANTS":
			filteredNotes = append(filteredNotes, note)
		case "SUPERVISORS":
			if note.AuthorID == p.Subject() || view.SupervisorID == p.Subject() {
				filteredNotes = append(filteredNotes, note)
			}
		case "FINANCE":
			if canCost && (note.AuthorID == p.Subject() || view.SupervisorID == p.Subject()) {
				filteredNotes = append(filteredNotes, note)
			}
		}
	}
	view.Notes = filteredNotes
	for i := range view.Journal {
		view.Journal[i].Detail = ""
		view.Journal[i].CommandDigest = ""
		view.Journal[i].IdempotencyKey = ""
	}
	return view, nil
}

func financialRequestKind(kind workorder.RequestKind) bool {
	switch kind {
	case workorder.RequestBudget, workorder.RequestBudgetChange, workorder.RequestCrew, workorder.RequestMaterial, workorder.RequestEquipment, workorder.RequestChangeOrder, workorder.RequestBillingReview:
		return true
	default:
		return false
	}
}

func cloneSnapshotForProjection(source workorder.Snapshot) workorder.Snapshot {
	view := source
	view.LinkedTaskIDs = append([]string(nil), source.LinkedTaskIDs...)
	view.Transitions = append([]workorder.PhaseTransition(nil), source.Transitions...)
	view.Requests = append([]workorder.InitiatorRequest(nil), source.Requests...)
	for i := range view.Requests {
		view.Requests[i].EvidenceRefs = append([]string(nil), source.Requests[i].EvidenceRefs...)
		if source.Requests[i].FormValues != nil {
			view.Requests[i].FormValues = make(map[string]string, len(source.Requests[i].FormValues))
			for k, v := range source.Requests[i].FormValues {
				view.Requests[i].FormValues[k] = v
			}
		}
	}
	view.Notes = append([]workorder.Note(nil), source.Notes...)
	for i := range view.Notes {
		view.Notes[i].AttachmentRefs = append([]string(nil), source.Notes[i].AttachmentRefs...)
	}
	view.Assignments = append([]workorder.Assignment(nil), source.Assignments...)
	view.Progress = append([]workorder.Progress(nil), source.Progress...)
	for i := range view.Progress {
		view.Progress[i].EvidenceRefs = append([]string(nil), source.Progress[i].EvidenceRefs...)
	}
	view.Spending = append([]workorder.Spend(nil), source.Spending...)
	view.WorkEntries = append([]workorder.WorkEntry(nil), source.WorkEntries...)
	view.Corrections = append([]workorder.Correction(nil), source.Corrections...)
	for i, correction := range source.Corrections {
		if correction.ReplacementAmount != nil {
			amount := *correction.ReplacementAmount
			view.Corrections[i].ReplacementAmount = &amount
		}
	}
	view.Journal = append([]workorder.Event(nil), source.Journal...)
	return view
}

func snapshotHasCost(s workorder.Snapshot) bool {
	if len(s.Spending) > 0 || len(s.Corrections) > 0 {
		return true
	}
	for _, r := range s.Requests {
		switch r.Kind {
		case workorder.RequestBudget, workorder.RequestBudgetChange, workorder.RequestCrew, workorder.RequestMaterial, workorder.RequestEquipment, workorder.RequestChangeOrder:
			return true
		}
	}
	for _, n := range s.Notes {
		if n.Visibility == "FINANCE" {
			return true
		}
	}
	return false
}

func snapshotHasPayDetails(s workorder.Snapshot) bool {
	for _, e := range s.WorkEntries {
		if strings.EqualFold(e.Kind, "LABOR") {
			return true
		}
	}
	return false
}

func templateRequestDefinition(pub workordertemplate.Published, phase workorder.Phase, kind workorder.RequestKind, values map[string]string) (string, error) {
	var found string
	for _, def := range pub.Snapshot().Requests {
		if workorder.RequestKind(def.Kind) != kind {
			continue
		}
		if len(def.AllowedPhases) == 0 || !pub.AllowsRequest(def.ID, string(phase)) {
			continue
		}
		form, ok := pub.Form(def.FormRef)
		if !ok {
			return "", fmt.Errorf("%w: request form %s is missing", ErrInvalidRequest, def.FormRef)
		}
		known := make(map[string]workordertemplate.FormField, len(form.Fields))
		if len(values) > 64 {
			return "", fmt.Errorf("%w: form has more than 64 values", ErrInvalidRequest)
		}
		totalBytes := 0
		for _, field := range form.Fields {
			known[field.ID] = field
			if field.Required && strings.TrimSpace(values[field.ID]) == "" {
				return "", fmt.Errorf("%w: required form field %s is missing", ErrInvalidRequest, field.ID)
			}
		}
		for fieldID := range values {
			field, ok := known[fieldID]
			if !ok {
				return "", fmt.Errorf("%w: field %s is not defined by pinned form", ErrInvalidRequest, fieldID)
			}
			value := values[fieldID]
			totalBytes += len(fieldID) + len(value)
			if len(fieldID) > 128 || len(value) > 4096 || totalBytes > 65536 {
				return "", fmt.Errorf("%w: form value exceeds size bounds", ErrInvalidRequest)
			}
			if err := validateFormValue(field, value); err != nil {
				return "", err
			}
		}
		if found != "" {
			return "", fmt.Errorf("%w: ambiguous request definition for kind %s", ErrInvalidRequest, kind)
		}
		found = def.ID
	}
	if found == "" {
		return "", fmt.Errorf("%w: request kind %s is not defined in phase %s of pinned template", ErrInvalidRequest, kind, phase)
	}
	return found, nil
}

func validateFormValue(field workordertemplate.FormField, value string) error {
	if value == "" {
		return nil
	}
	switch field.Type {
	case workordertemplate.FieldText, workordertemplate.FieldEvidence:
		return nil
	case workordertemplate.FieldDecimal:
		if _, err := values.NewDecimal(value, values.MaxScale, values.RoundingHalfEven); err != nil {
			return fmt.Errorf("%w: field %s must be a decimal", ErrInvalidRequest, field.ID)
		}
	case workordertemplate.FieldBoolean:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%w: field %s must be boolean", ErrInvalidRequest, field.ID)
		}
	case workordertemplate.FieldDate:
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return fmt.Errorf("%w: field %s must be an ISO date", ErrInvalidRequest, field.ID)
		}
	case workordertemplate.FieldEnum:
		for _, option := range field.Options {
			if value == option {
				return nil
			}
		}
		return fmt.Errorf("%w: field %s is not an allowed option", ErrInvalidRequest, field.ID)
	default:
		return fmt.Errorf("%w: field %s has an unsupported type", ErrInvalidRequest, field.ID)
	}
	return nil
}
func validPrincipal(p *trust.Principal) error {
	if p == nil || p.Subject() == "" || tenant(p) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

// canonicalCommand removes fields owned by the service so caller-supplied
// actor IDs, revisions, timestamps, record IDs and digests cannot change the
// command receipt while being ignored by the actual mutation.
func canonicalCommand(command any) any {
	switch in := command.(type) {
	case workorder.TransitionInput:
		in.ActorID, in.IdempotencyKey, in.CommandDigest = "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	case *workorder.InitiatorRequestInput:
		copy := *in
		copy.ID, copy.DefinitionID, copy.ActorID, copy.IdempotencyKey, copy.CommandDigest = "", "", "", "", ""
		copy.ExpectedRevision = 0
		copy.Now = time.Time{}
		return copy
	case workorder.RequestDecisionInput:
		in.ActorID, in.IdempotencyKey, in.CommandDigest = "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	case workorder.NoteInput:
		in.ID, in.AuthorID, in.IdempotencyKey, in.CommandDigest = "", "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	case workorder.AssignmentInput:
		in.ID, in.ActorID, in.IdempotencyKey, in.CommandDigest = "", "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	case workorder.ProgressInput:
		in.ID, in.ActorID, in.IdempotencyKey, in.CommandDigest = "", "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	case workorder.SpendInput:
		in.ID, in.ActorID, in.IdempotencyKey, in.CommandDigest = "", "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	case workorder.CorrectionInput:
		in.ID, in.ActorID, in.IdempotencyKey, in.CommandDigest = "", "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	case workorder.WorkEntryInput:
		in.ID, in.ActorID, in.IdempotencyKey, in.CommandDigest = "", "", "", ""
		in.ExpectedRevision = 0
		in.Now = time.Time{}
		return in
	default:
		return command
	}
}

func canonicalJSON(value any) ([]byte, error) {
	return json.Marshal(toCanonical(reflect.ValueOf(value)))
}

func toCanonical(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return toCanonical(v.Elem())
	}
	if v.Type() == reflect.TypeOf(time.Time{}) {
		t := v.Interface().(time.Time)
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	if v.Type() == reflect.TypeOf(values.Decimal{}) {
		d := v.Interface().(values.Decimal)
		b, err := d.MarshalText()
		if err != nil {
			return nil
		}
		return string(b)
	}
	if v.Type() == reflect.TypeOf(values.Money{}) {
		m := v.Interface().(values.Money)
		b, err := m.MarshalText()
		if err != nil {
			return nil
		}
		return string(b)
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{}
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := field.Name
			if tag := field.Tag.Get("json"); tag != "" {
				name = strings.Split(tag, ",")[0]
				if name == "-" {
					continue
				}
				if name == "" {
					name = field.Name
				}
			}
			out[name] = toCanonical(v.Field(i))
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		iter := v.MapRange()
		for iter.Next() {
			out[fmt.Sprint(iter.Key().Interface())] = toCanonical(iter.Value())
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = toCanonical(v.Index(i))
		}
		return out
	default:
		return v.Interface()
	}
}
func tenant(p *trust.Principal) string { return string(p.Tenant()) }
func validScope(r ScopedRequest) error {
	if strings.TrimSpace(r.WorkOrderID) == "" {
		return ErrInvalidRequest
	}
	return nil
}
func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func stableID(tenantID, actorID, operation, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join([]string{tenantID, actorID, operation, key}, "\x00"))).String()
}
