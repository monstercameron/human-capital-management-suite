package execution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// WF-STEP-003: the decision-time half of PROMOUX-015's approval routing.
//
// [promotionWorkItems.CreateAndRoute] resolves who holds each promotion
// approval when the item is raised. [PromotionApprovalAuthority] asks the same
// question again, through the same routing functions over the same
// configuration, when the approval is decided, so internal/intent/app can
// compare the authority the item was routed under with the authority that is
// current now.

// PromotionApprovalAuthority implements [app.ApprovalAuthoritySource] for the
// promotion plans this package composes.
type PromotionApprovalAuthority struct {
	routes promotionWorkItems
}

var _ app.ApprovalAuthoritySource = PromotionApprovalAuthority{}

// NewPromotionApprovalAuthority builds the authority source for the routing a
// [NewPromotionExecution] over cfg performs: the same approver, manager
// fallback, finance partner and manager resolver defaults.
func NewPromotionApprovalAuthority(cfg PromotionExecutionConfig) PromotionApprovalAuthority {
	approver := cfg.ApproverPrincipalID
	if approver == "" {
		approver = defaultApproverPrincipalID
	}
	managerApprover := cfg.ManagerApproverPrincipalID
	if managerApprover == "" {
		if cfg.Plan == PLAN_EXECUTE {
			managerApprover = defaultManagerApproverPrincipalID
		} else {
			managerApprover = approver
		}
	}
	return PromotionApprovalAuthority{routes: promotionWorkItems{
		approver: approver, managerApprover: managerApprover, financePartner: cfg.FinancePartnerPrincipalID,
		financeByTenant: cfg.FinancePartnerByTenant, managerByTenant: cfg.ManagerApproverByTenant,
		managers: managerFallback{base: cfg.Managers, fallback: managerApprover, byTenant: cfg.ManagerApproverByTenant}, plan: cfg.Plan,
	}}
}

// CurrentApprovalAuthority implements [app.ApprovalAuthoritySource].
//
// It re-resolves the principal who holds the item's authority class now:
// FinancePartnerFor for the finance approval, CurrentManagerOf(subject) read on
// ex for the manager approval. The answer is Active only when that principal is
// the one the decision relies on and, for the configured finance partner, the
// deciding principal still carries the requirement's authority-floor role. A
// graph subject whose manager no longer resolves has no current holder, which
// is an inactive answer, not an error: the approval is stale, not the lookup.
func (a PromotionApprovalAuthority) CurrentApprovalAuthority(
	ctx context.Context, ex workitem.Executor, q app.ApprovalAuthorityQuery,
) (ret0 promotionexec.CurrentApprovalAuthority, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.approval_authority", q)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	class := promotionexec.ApprovalAuthorityClass(q.Item.NodeID)
	current := promotionexec.CurrentApprovalAuthority{Class: class, Scope: app.ApprovalAuthorityScope(q.Item)}
	var holder routedApprover
	switch class {
	case promotionexec.AuthorityClassFinancePartner:
		route, err := a.routes.financeRoute(q.TenantID)
		if err != nil {
			return promotionexec.CurrentApprovalAuthority{}, err
		}
		holder = route
	case promotionexec.AuthorityClassCurrentManager:
		route, _, err := a.routes.managerRoute(ctx, ex, q.TenantID, q.SubjectID)
		switch {
		case errors.Is(err, ErrUnresolvedManager):
			return current, nil
		case err != nil:
			return promotionexec.CurrentApprovalAuthority{}, err
		}
		holder = route
	default:
		return promotionexec.CurrentApprovalAuthority{}, fmt.Errorf(
			"platform execution: node %q is not a promotion approval authority", q.Item.NodeID)
	}
	current.PrincipalID, current.AuthorityRef = holder.principal, holder.termRef
	current.Active = holder.principal != "" && holder.principal == q.AuthorityPrincipalID
	if current.Active && holder.termRef == termFinancePartner && q.DeciderPrincipalID == holder.principal &&
		!slices.Contains(q.DeciderRoles, promotionexec.FinanceApprovalAuthorityFloor) {
		// The configured finance partner decides as themselves: the verified
		// credential must still carry the finance authority floor. A
		// delegate's floor was checked when the delegation was resolved.
		current.Active = false
	}
	return current, nil
}
