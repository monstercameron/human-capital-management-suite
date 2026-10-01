package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

var (
	errMissingPunchApprovalConfig  = errors.New("missing punch approval: incomplete composition")
	errMissingPunchApprovalBinding = errors.New("missing punch approval: incomplete durable binding")
)

// MissingPunchApprovalBinding is the server-owned resume context for one
// parked supervisor approval. It is loaded from durable workflow state by
// the composition root; no field is accepted from a decision request.
type MissingPunchApprovalBinding struct {
	TenantID        uuid.UUID
	RequestID       string
	InstanceID      uuid.UUID
	WorkItemID      uuid.UUID
	ItemVersion     int64
	InstanceVersion int64
	Start           runtime.StartRequest
	Continuation    stepapproval.Continuation
	Decision        intentapproval.ApprovalDecision
}

// MissingPunchApprovalLoader reads the original start, pinned plan and the
// real routed WorkItem in one authoritative, tenant-scoped read. A loader is
// deliberately required: the approval path must never reconstruct metadata
// from a caller request.
type MissingPunchApprovalLoader interface {
	LoadMissingPunchApproval(context.Context, clockservice.MissingPunchWorkflowRequest) (MissingPunchApprovalBinding, error)
}

// PostgresMissingPunchApprovalLoader reads the time-plane request and then the
// pinned instance and routed supervisor WorkItem in a separate core transaction.
type PostgresMissingPunchApprovalLoader struct {
	// RequestStore is the time-plane database. DB is the core workflow
	// database; they are intentionally separate transactions and are never
	// joined by a cross-database query.
	RequestStore  *timestore.Store
	DB            execute.Beginner
	ResolveTenant func(string) (uuid.UUID, error)
	Start         func(context.Context, clockservice.MissingPunchWorkflowRequest, runtime.Instance) (runtime.StartRequest, error)
	Continuation  func(context.Context, runtime.StartRequest, runtime.Instance, workitem.WorkItem) (stepapproval.Continuation, error)
	Decision      func(context.Context, clockservice.MissingPunchWorkflowRequest, runtime.StartRequest, workitem.WorkItem) (intentapproval.ApprovalDecision, error)
}

// LoadMissingPunchApproval loads only server-owned approval metadata.
func (l PostgresMissingPunchApprovalLoader) LoadMissingPunchApproval(ctx context.Context, req clockservice.MissingPunchWorkflowRequest) (MissingPunchApprovalBinding, error) {
	if l.RequestStore == nil || l.DB == nil || l.ResolveTenant == nil || l.Start == nil || l.Continuation == nil || l.Decision == nil {
		return MissingPunchApprovalBinding{}, errMissingPunchApprovalConfig
	}
	tenant, err := l.ResolveTenant(req.TenantID)
	if err != nil || tenant == uuid.Nil {
		return MissingPunchApprovalBinding{}, errMissingPunchApprovalBinding
	}
	request, err := (timeclockstore.Adapter{Store: l.RequestStore}).GetMissingPunchRequest(ctx, req.TenantID, req.RequestID)
	if err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	if request.TenantID != req.TenantID || request.ID != req.RequestID || request.Revision != req.ExpectedRevision || request.WorkerRef == "" || request.RequestedBy == "" || request.WorkflowInstanceID == "" || req.Actor == request.WorkerRef || req.Actor == request.RequestedBy {
		return MissingPunchApprovalBinding{}, errMissingPunchApprovalBinding
	}
	instanceID, err := uuid.Parse(request.WorkflowInstanceID)
	if err != nil || instanceID == uuid.Nil {
		return MissingPunchApprovalBinding{}, errMissingPunchApprovalBinding
	}
	tx, err := l.DB.Begin(ctx)
	if err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	inst, err := (runtime.Store{}).LoadInstance(ctx, tx, tenant, instanceID)
	if err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	items, err := (workitem.Store{}).ListForInstance(ctx, tx, tenant, instanceID)
	if err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	var item workitem.WorkItem
	for _, candidate := range items {
		if candidate.Kind == workitem.KindApproval && candidate.NodeID == "supervisor_approval" {
			if item.WorkItemID != uuid.Nil {
				return MissingPunchApprovalBinding{}, errMissingPunchApprovalBinding
			}
			item = candidate
		}
	}
	if item.WorkItemID == uuid.Nil {
		return MissingPunchApprovalBinding{}, errMissingPunchApprovalBinding
	}
	start, err := l.Start(ctx, req, inst)
	if err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	if start.TenantID != tenant || start.PinnedCompiledPlanDigest == "" || start.PinnedCompiledPlanDigest != inst.CompiledPlanHash || start.Proposal.Revision.MaterialDigest.Digest == "" || item.ProposalRef != start.Proposal.Revision.MaterialDigest.Digest {
		return MissingPunchApprovalBinding{}, errMissingPunchApprovalBinding
	}
	continuation, err := l.Continuation(ctx, start, inst, item)
	if err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	decision, err := l.Decision(ctx, req, start, item)
	if err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MissingPunchApprovalBinding{}, err
	}
	return MissingPunchApprovalBinding{TenantID: tenant, RequestID: request.ID, InstanceID: instanceID, WorkItemID: item.WorkItemID, ItemVersion: item.ItemVersion, InstanceVersion: inst.InstanceVersion, Start: start, Continuation: continuation, Decision: decision}, nil
}

// DurableMissingPunchApprovalStore completes the real supervisor WorkItem and
// resumes its pinned workflow instance. The Driver must be built with the
// production execute.Options, including Options.WorkItems and
// Options.Items; this type never creates a fake WorkItem or an allow-all
// authority adapter.
type DurableMissingPunchApprovalStore struct {
	Driver *execute.Driver
	// DriverFactory must bind the DECIDE request to a fresh StepRunner. A
	// shared driver whose runner still carries REQUEST data can apply the
	// correction under the worker actor, so production composition should use
	// this field whenever the graph has request-scoped effects.
	DriverFactory func(context.Context, clockservice.MissingPunchWorkflowRequest) (*execute.Driver, error)
	Loader        MissingPunchApprovalLoader
	Authority     execute.CurrentApprovalAuthority
	Clock         func() time.Time
}

// NewDurableMissingPunchApprovalStore validates the concrete approval composition.
// WorkItem routing and durable loading remain in the caller-owned driver and
// loader so the root can use the existing platform execution adapter.
func NewDurableMissingPunchApprovalStore(driver *execute.Driver, loader MissingPunchApprovalLoader, authority execute.CurrentApprovalAuthority, clock func() time.Time) (*DurableMissingPunchApprovalStore, error) {
	if driver == nil || loader == nil || authority == nil || clock == nil {
		return nil, errMissingPunchApprovalConfig
	}
	return &DurableMissingPunchApprovalStore{Driver: driver, Loader: loader, Authority: authority, Clock: clock}, nil
}

// NewDurableMissingPunchApprovalStoreFactory composes approval with a
// request-scoped engine driver. The factory must use the production
// execute.Options.WorkItems and Items adapters and bind DECIDE's actor/data
// into its StepRunner.
func NewDurableMissingPunchApprovalStoreFactory(factory func(context.Context, clockservice.MissingPunchWorkflowRequest) (*execute.Driver, error), loader MissingPunchApprovalLoader, authority execute.CurrentApprovalAuthority, clock func() time.Time) (*DurableMissingPunchApprovalStore, error) {
	if factory == nil || loader == nil || authority == nil || clock == nil {
		return nil, errMissingPunchApprovalConfig
	}
	return &DurableMissingPunchApprovalStore{DriverFactory: factory, Loader: loader, Authority: authority, Clock: clock}, nil
}

// CompleteApproval atomically completes the durable supervisor WorkItem and
// resumes the exact instance/version/plan selected by the loader. The caller
// supplies only the business decision; the loader supplies all binding data.
func (s *DurableMissingPunchApprovalStore) CompleteApproval(ctx context.Context, req clockservice.MissingPunchWorkflowRequest) (MissingPunchApproval, error) {
	if s == nil || s.Loader == nil || s.Authority == nil || s.Clock == nil || s.Driver == nil && s.DriverFactory == nil {
		return MissingPunchApproval{}, errMissingPunchApprovalConfig
	}
	if strings.TrimSpace(req.Action) != "DECIDE" || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.RequestID) == "" || strings.TrimSpace(req.Actor) == "" || req.At.IsZero() {
		return MissingPunchApproval{}, fmt.Errorf("%w: decision identity is incomplete", errMissingPunchApprovalBinding)
	}
	binding, err := s.Loader.LoadMissingPunchApproval(ctx, req)
	if err != nil {
		return MissingPunchApproval{}, err
	}
	if binding.TenantID == uuid.Nil || binding.InstanceID == uuid.Nil || binding.WorkItemID == uuid.Nil || binding.ItemVersion < 1 || binding.InstanceVersion < 1 || binding.Start.Resolver == nil || binding.Start.Versions == nil || binding.Start.TenantID != binding.TenantID || binding.Start.PinnedCompiledPlanDigest == "" || binding.Continuation.WorkflowInstanceID != binding.InstanceID || binding.Continuation.NodeID == "" || !binding.Decision.Outcome.Valid() {
		return MissingPunchApproval{}, errMissingPunchApprovalBinding
	}
	if binding.RequestID != req.RequestID {
		return MissingPunchApproval{}, errMissingPunchApprovalBinding
	}
	if binding.Decision.Approver.PrincipalID != req.Actor {
		return MissingPunchApproval{}, errMissingPunchApprovalBinding
	}
	decision := binding.Decision
	if req.Approve && decision.Outcome != intentapproval.OutcomeApproved || !req.Approve && decision.Outcome != intentapproval.OutcomeRejected {
		return MissingPunchApproval{}, errMissingPunchApprovalBinding
	}
	at := s.Clock().UTC()
	if at.IsZero() || at.Before(req.At.UTC()) {
		return MissingPunchApproval{}, errMissingPunchApprovalBinding
	}
	driver := s.Driver
	if s.DriverFactory != nil {
		driver, err = s.DriverFactory(ctx, req)
		if err != nil || driver == nil {
			if err == nil {
				err = errMissingPunchApprovalConfig
			}
			return MissingPunchApproval{}, err
		}
	}
	result, err := driver.CompleteApproval(ctx, execute.ApprovalCompletionRequest{
		Start: binding.Start, InstanceID: binding.InstanceID, ExpectedInstanceVersion: binding.InstanceVersion,
		WorkItemID: binding.WorkItemID, ExpectedWorkItemVersion: binding.ItemVersion,
		Continuation: binding.Continuation, Decision: decision, RecordedAt: at, Authority: s.Authority,
		Meta: workitem.TransitionMeta{ActorPrincipalID: req.Actor, Reason: strings.TrimSpace(req.DecisionNote), At: at},
	})
	if err != nil {
		return MissingPunchApproval{}, err
	}
	if result.CompletedItem.WorkItemID != binding.WorkItemID || result.CompletedItem.ItemVersion < binding.ItemVersion || result.CompletedItem.Status != workitem.StatusCompleted {
		return MissingPunchApproval{}, errMissingPunchApprovalBinding
	}
	return MissingPunchApproval{TenantID: binding.TenantID, RequestID: binding.RequestID, InstanceID: binding.InstanceID, PlanDigest: binding.Start.PinnedCompiledPlanDigest, WorkItemID: binding.WorkItemID, ItemVersion: result.CompletedItem.ItemVersion, InstanceVersion: result.InstanceVersion, Start: binding.Start, Outcome: result.Resolution.ToNodeOutcome(binding.Continuation.NodeID), Refs: runtime.GovernanceRefs{AuthorizationDecisionID: result.AuthorityRef, DecisionID: decision.DecisionID, HumanTaskID: binding.WorkItemID.String(), ProposalRef: binding.Start.Proposal.Revision.MaterialDigest.Digest}, Result: result.Result}, nil
}

var _ MissingPunchApprovalStore = (*DurableMissingPunchApprovalStore)(nil)
