package app

// WF-RUN-034: the governed standing a promotion's GOVERN-002 record and its
// GOVERN-003 revalidation are composed from.
//
// Revalidation compares a decision recorded at approval with one recomposed
// from current facts. Two of those facts are the cell's own: whether the
// pinned executing delegation still carries the execution role at all, and
// the control versions (policy bundle, legal context, classification taxonomy,
// source authority) the decision was taken under. Both are answered here,
// where the role assignment and the control snapshot live, and handed to the
// execution adapter that reads the remaining facts from the durable stores.

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// GovernanceStanding is the cell's answer about one pinned delegation at one
// instant: whether it still authorizes the promotion's execution, and the
// control versions its governance decision is composed against.
type GovernanceStanding struct {
	// Authorized is true only when the delegation still resolves to a
	// principal holding the cell's execution role.
	Authorized bool
	// Refusal names why an unauthorized delegation was refused. Empty when
	// Authorized.
	Refusal string
	// Subject, SessionRef and Assurance identify the acting delegation.
	Subject, SessionRef, Assurance string
	// Roles are the delegated roles that survive the current assignment.
	Roles []string
	// RequiredRole is the execution role the cell demands.
	RequiredRole string
	// Purpose is the purpose of processing the execution was authorized for.
	Purpose string
	// PolicyBundleDigest, LegalContextDigest, ClassificationDigest,
	// CapabilityDigest and ControlDigest are the current control versions.
	PolicyBundleDigest, LegalContextDigest, ClassificationDigest string
	CapabilityDigest, ControlDigest                              string
	// SourceAuthorityDigest is the cell's current source-authority decision
	// digest, and RiskClass the promotion definition's declared risk class.
	SourceAuthorityDigest, RiskClass string
	// ObservedAt is when this standing was taken.
	ObservedAt time.Time
}

// GovernanceStanding answers [GovernanceStanding] for one pinned delegation.
// A delegation the current role assignment no longer authorizes is reported
// unauthorized with its reason, never as an error: refusing to answer would
// make a revoked role indistinguishable from an unavailable store, and
// revalidation must be able to record the refusal as a fact.
func (p *PromotionStepServices) GovernanceStanding(ctx context.Context, call PromotionStepCall) (ret0 GovernanceStanding, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.governance_standing", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	standing := GovernanceStanding{
		Subject: call.Delegation.Subject, SessionRef: call.Delegation.SessionRef, Assurance: call.Delegation.Assurance,
		RequiredRole: p.requiredRole, ObservedAt: p.now().UTC(),
	}
	if len(call.Delegation.Purposes) > 0 {
		standing.Purpose = call.Delegation.Purposes[0]
	}
	snapshots := p.svc.controls.Snapshots
	standing.PolicyBundleDigest = snapshots.PolicyBundleDigest
	standing.LegalContextDigest = snapshots.LegalContextDigest
	standing.ClassificationDigest = snapshots.ClassificationTaxonomyDigest
	standing.CapabilityDigest = snapshots.CapabilityRegistryDigest
	standing.ControlDigest = controlSnapshotDigest(snapshots)
	standing.SourceAuthorityDigest = p.svc.controls.SourceAuthorityDigest
	if def, err := p.svc.defs.Resolve(intent.Ref{TypeID: promotion.IntentType, Version: 1}); err == nil {
		standing.RiskClass = def.RiskClass
	}
	principal, _, err := p.principal(ctx, call)
	if err != nil {
		standing.Refusal = err.Error()
		return standing, nil
	}
	standing.Authorized = true
	standing.Roles = principal.Roles()
	return standing, nil
}
