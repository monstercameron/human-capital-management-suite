package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// PROMOUX-015: approval routing for the executable promotion plan.
//
// planning/reference-workflows/promote-into-management.md and
// promotionexec.Definition name the two approvals this plan raises and who
// resolves them: CurrentManagerOf(worker) for the manager approval and
// FinancePartnerFor(cost_center) for the finance approval, both with
// separation of duties (the compiled requirement's RequesterMayNotApprove and
// SubjectMayNotApprove). Before this file both approvals were routed to
// class-scoped derivations of one configured identity, so nothing routed by a
// real relationship and a single operator could decide everything.

// Candidate term references and directory versions recorded on a routed
// assignment, naming which rule produced the candidate.
const (
	termConfiguredApprover      = "term:execution-authority-approver"
	termFinancePartner          = "term:finance-partner-for-cost-center"
	termCurrentManager          = "term:current-manager-of-worker"
	termSkipLevelManager        = "term:current-manager-of-requester"
	directoryConfiguredApprover = "directory.execution-authority/1"
	directoryFinancePartner     = "directory.finance-partner.configured/1"
	directoryCurrentManager     = "directory.journey_worker.manager_relationship/1"
	directorySkipLevelManager   = "directory.journey_worker.manager_relationship.skip_requester/1"
)

// ErrUnresolvedManager is the reference workflow's UNRESOLVED_MANAGER: the
// subject is part of the relationship graph but its manager relationship does
// not name another worker (a vacant or sentinel manager). Routing refuses
// rather than guessing an organization leader.
var ErrUnresolvedManager = errors.New("platform execution: the worker's current manager does not resolve")

// ErrApproverSeparation wraps an [approverclass.RequireSeparated] refusal so a
// caller can classify the routing refusal without importing approverclass.
var ErrApproverSeparation = errors.New("platform execution: the resolved approvers violate separation of duties")

// Routing refusal codes. The workflow telemetry seam logs an error's
// classified code and never its message (internal/workflow/observe), so an
// uncoded routing refusal reached the log as a bare "ERROR" and hid which
// rule refused the route (UXBLIND-001). Each refusal carries its own code.
const (
	CodeUnresolvedManager            = "APPROVAL_ROUTE_UNRESOLVED_MANAGER"
	CodeApproverSharedOwner          = "APPROVAL_ROUTE_SHARED_OWNER"
	CodeApproverIsRequester          = "APPROVAL_ROUTE_REQUESTER_APPROVER"
	CodeApproverIsSubject            = "APPROVAL_ROUTE_SUBJECT_APPROVER"
	CodeApproverSeparationUnresolved = "APPROVAL_ROUTE_SEPARATION_UNRESOLVED"
)

// routingRefusal is a routing refusal with a stable telemetry code. It
// unwraps to its cause, so errors.Is against [ErrUnresolvedManager],
// [ErrApproverSeparation] and the approverclass sentinels is unchanged.
type routingRefusal struct {
	code string
	err  error
}

func (e *routingRefusal) Error() string     { return e.err.Error() }
func (e *routingRefusal) Unwrap() error     { return e.err }
func (e *routingRefusal) ErrorCode() string { return e.code }

// separationRefusal codes an [approverclass.RequireSeparated] refusal.
func separationRefusal(err error) error {
	code := CodeApproverSeparationUnresolved
	switch {
	case errors.Is(err, approverclass.ErrSharedOwner):
		code = CodeApproverSharedOwner
	case errors.Is(err, approverclass.ErrRequesterApprover):
		code = CodeApproverIsRequester
	case errors.Is(err, approverclass.ErrSubjectApprover):
		code = CodeApproverIsSubject
	}
	return &routingRefusal{code: code, err: fmt.Errorf("%w: %w", ErrApproverSeparation, err)}
}

// unresolvedManager codes an [ErrUnresolvedManager] refusal for worker.
func unresolvedManager(worker string) error {
	return &routingRefusal{code: CodeUnresolvedManager, err: fmt.Errorf("%w: %s", ErrUnresolvedManager, worker)}
}

// ManagerOf is one CurrentManagerOf(worker) answer.
type ManagerOf struct {
	// InGraph reports whether the subject is a worker of the relationship
	// graph at all. A subject outside it (the fixed conformance corpus) has
	// no manager relationship to resolve.
	InGraph bool
	// SubjectKey is the subject worker's own key, the identity a signed-in
	// principal carries, so separation can compare against it.
	SubjectKey string
	// ManagerPrincipal is the manager worker's key, or empty when the
	// relationship does not resolve to another worker.
	ManagerPrincipal string
}

// ManagerResolver answers CurrentManagerOf(worker) on the routing
// transaction.
type ManagerResolver interface {
	CurrentManagerOf(ctx context.Context, ex workitem.Executor, tenantID uuid.UUID, workerRef string) (ManagerOf, error)
}

// JourneyWorkerManagers resolves CurrentManagerOf(worker) from journey_worker's
// manager_relationship_ref, the one manager fact that population carries (the
// same row internal/data/orgfacts reads). A reference that does not name
// another worker row in the tenant -- the seeded "board:harborcare" sentinel
// or a dangling key -- resolves to no manager.
type JourneyWorkerManagers struct{}

var _ ManagerResolver = JourneyWorkerManagers{}

// CurrentManagerOf implements [ManagerResolver].
func (JourneyWorkerManagers) CurrentManagerOf(ctx context.Context, ex workitem.Executor, tenantID uuid.UUID, workerRef string) (ManagerOf, error) {
	var store workforce.Store
	row, found, err := store.Get(ctx, ex, tenantID, strings.TrimSpace(workerRef))
	if err != nil {
		return ManagerOf{}, fmt.Errorf("platform execution: read the promotion subject: %w", err)
	}
	if !found {
		return ManagerOf{}, nil
	}
	out := ManagerOf{InGraph: true, SubjectKey: row.WorkerKey}
	if row.ManagerRelationshipRef == "" {
		return out, nil
	}
	manager, managerFound, err := store.Get(ctx, ex, tenantID, row.ManagerRelationshipRef)
	if err != nil {
		return ManagerOf{}, fmt.Errorf("platform execution: read the subject's manager: %w", err)
	}
	if managerFound && manager.WorkerID != row.WorkerID {
		out.ManagerPrincipal = manager.WorkerKey
	}
	return out, nil
}

// routedApprover is one resolved approval owner and the rule that produced it.
type routedApprover struct {
	principal        string
	termRef          string
	directoryVersion string
}

// approverRoute is both approvals of one proposal, resolved together so the
// separation constraint is checked before either WorkItem exists.
type approverRoute struct {
	finance routedApprover
	manager routedApprover
}

// resolveApprovers resolves the finance and manager approvers for one
// proposal and refuses a route that violates separation of duties.
//
// Finance routes to the configured finance partner; with none configured it
// keeps PROMOUX-003's class-scoped derivation of the configured approver.
// Manager routes to CurrentManagerOf(worker); a subject outside the
// relationship graph (the fixed corpus, which carries no manager fact) keeps
// the class-scoped configured approver, and a subject inside it whose manager
// does not resolve is refused with [ErrUnresolvedManager]. Both approvals are
// resolved on every routing call, so the manager approval's constraint is
// enforced when the finance approval is raised, not after finance has decided.
func (f promotionWorkItems) resolveApprovers(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (approverRoute, error) {
	var route approverRoute
	finance, err := f.financeRoute(req.Continuation.TenantID)
	if err != nil {
		return approverRoute{}, err
	}
	route.finance = finance

	subjectID := employmentSubject(req)
	requester := req.Proposal.Revision.CreatedBy.PrincipalID
	manager, managerOf, err := f.managerRoute(ctx, ex, req.Continuation.TenantID, subjectID, requester)
	if err != nil {
		return approverRoute{}, err
	}
	route.manager = manager

	subjects := append([]string{subjectID, managerOf.SubjectKey}, req.SubjectRefs...)
	if err := approverclass.RequireSeparated(requester, subjects, route.finance.principal, route.manager.principal); err != nil {
		return approverRoute{}, separationRefusal(err)
	}
	return route, nil
}

// financeRoute is the FinancePartnerFor(cost_center) answer: the configured
// finance partner, or PROMOUX-003's class-scoped derivation of the configured
// approver when none is configured. It reads no durable fact, so routing and
// the decision-time authority recheck ([PromotionApprovalAuthority]) derive it
// through this one function.
func (f promotionWorkItems) financeRoute(tenant uuid.UUID) (routedApprover, error) {
	if partner := f.financeByTenant[tenant]; partner != "" {
		return routedApprover{principal: partner, termRef: termFinancePartner, directoryVersion: directoryFinancePartner}, nil
	}
	if f.financePartner != "" {
		return routedApprover{principal: f.financePartner, termRef: termFinancePartner, directoryVersion: directoryFinancePartner}, nil
	}
	derived, err := promotionexec.FinanceApproverFor(f.approver)
	if err != nil {
		return routedApprover{}, fmt.Errorf("platform execution: derive the finance approver: %w", err)
	}
	return routedApprover{principal: derived, termRef: termConfiguredApprover, directoryVersion: directoryConfiguredApprover}, nil
}

// managerRoute is the CurrentManagerOf(worker) answer read on ex: the
// subject's current manager, the class-scoped configured approver for a
// subject outside the relationship graph, or [ErrUnresolvedManager] for a graph
// subject whose manager does not resolve. Routing and the decision-time
// authority recheck both call it, so the two can never disagree about which
// relationship grants the manager approval.
//
// UXBLIND-001: when the subject's current manager is the proposal's requester
// -- a manager proposing their own report, the ordinary case -- the manager
// approval cannot go to them (RequesterMayNotApprove), and refusing the route
// left every such promotion unable to start. The approval escalates one level
// up the requester's own reporting line, CurrentManagerOf(requester), recorded
// under its own term so the rechecks re-derive the same rule. A requester
// whose own manager does not resolve is refused with [ErrUnresolvedManager].
func (f promotionWorkItems) managerRoute(ctx context.Context, ex workitem.Executor, tenantID uuid.UUID, subjectID, requester string) (routedApprover, ManagerOf, error) {
	managers := f.managerResolver()
	managerOf, err := managers.CurrentManagerOf(ctx, ex, tenantID, subjectID)
	if err != nil {
		return routedApprover{}, ManagerOf{}, err
	}
	switch {
	case managerOf.ManagerPrincipal != "" && requester != "" && managerOf.ManagerPrincipal == requester:
		skip, err := managers.CurrentManagerOf(ctx, ex, tenantID, requester)
		if err != nil {
			return routedApprover{}, ManagerOf{}, err
		}
		if !skip.InGraph || skip.ManagerPrincipal == "" || skip.ManagerPrincipal == requester {
			return routedApprover{}, managerOf, unresolvedManager(requester)
		}
		return routedApprover{principal: skip.ManagerPrincipal, termRef: termSkipLevelManager, directoryVersion: directorySkipLevelManager}, managerOf, nil
	case managerOf.ManagerPrincipal != "":
		return routedApprover{principal: managerOf.ManagerPrincipal, termRef: termCurrentManager, directoryVersion: directoryCurrentManager}, managerOf, nil
	case managerOf.InGraph:
		return routedApprover{}, managerOf, unresolvedManager(subjectID)
	default:
		derived, deriveErr := promotionexec.ManagerApproverFor(f.approver)
		if deriveErr != nil {
			return routedApprover{}, managerOf, fmt.Errorf("platform execution: derive the manager approver: %w", deriveErr)
		}
		return routedApprover{principal: derived, termRef: termConfiguredApprover, directoryVersion: directoryConfiguredApprover}, managerOf, nil
	}
}

// managerResolver is the configured CurrentManagerOf source, defaulting to
// journey_worker's manager relationships.
func (f promotionWorkItems) managerResolver() ManagerResolver {
	if f.managers == nil {
		return JourneyWorkerManagers{}
	}
	return f.managers
}

// routedRequesterOf recovers, for the audience recheck that holds only the
// routed item, the requester a skip-level manager route skipped: an item
// routed under [termSkipLevelManager] was escalated past the subject's current
// manager because that manager was the requester, so re-deriving the route
// treats that same manager as the requester. Any other item skipped nobody.
func (f promotionWorkItems) routedRequesterOf(ctx context.Context, ex workitem.Executor, item workitem.WorkItem, subjectID string) (string, error) {
	skipped := false
	for _, candidate := range item.Assignment.Resolution.Candidates {
		if candidate.TermRef == termSkipLevelManager {
			skipped = true
		}
	}
	if !skipped {
		return "", nil
	}
	managerOf, err := f.managerResolver().CurrentManagerOf(ctx, ex, item.TenantID, subjectID)
	if err != nil {
		return "", err
	}
	return managerOf.ManagerPrincipal, nil
}

// employmentSubject is the proposal's EMPLOYMENT subject id, falling back to
// the first subject reference the driver carried.
func employmentSubject(req execute.WorkItemRequest) string {
	for _, subject := range req.Proposal.Revision.Subjects {
		if subject.Kind == "EMPLOYMENT" && subject.SubjectID != "" {
			return subject.SubjectID
		}
	}
	if len(req.SubjectRefs) > 0 {
		return req.SubjectRefs[0]
	}
	return ""
}
