package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// WF-STEP-003: the decision-time authority recheck.
//
// Before this file the served decide path "rechecked" authority only against
// the routed assignment itself (ValidatePromotionJourneyApprover: is the
// caller still a recorded candidate?). The assignment is the fact being
// rechecked, so that comparison could never fail for the routed approver and
// the APPROVAL step's INVALIDATED route was unreachable in production. The
// recheck below re-resolves the authority the routed requirement stands on
// from current durable facts -- the relationship reader and configuration
// internal/platform/execution routed with, and the role facts the verified
// principal carries -- and compares it with the authority the item was routed
// under through promotionexec.ValidateCurrentAuthority.

// ErrProposalDecisionInvalidated is returned, after the approval has been
// durably routed INVALIDATED, when the deciding principal no longer holds the
// authority the approval was routed under.
var ErrProposalDecisionInvalidated = errors.New("app: proposal approval was invalidated: the approver's authority is no longer current")

// ApprovalAuthorityQuery is one decision-time authority question: who holds,
// right now, the authority class the routed approval item requires, and does
// the deciding principal still carry the facts that authority needs.
type ApprovalAuthorityQuery struct {
	TenantID uuid.UUID
	// Item is the open approval WorkItem, read inside the decision
	// transaction.
	Item workitem.WorkItem
	// SubjectID is the proposal's EMPLOYMENT subject, the worker whose
	// relationships CurrentManagerOf reads.
	SubjectID string
	// AuthorityPrincipalID is the principal whose authority the decision
	// relies on: the routed candidate, or the delegator a delegated candidate
	// borrows authority from.
	AuthorityPrincipalID string
	// DeciderPrincipalID and DeciderRoles are the verified principal's own
	// identity and role facts.
	DeciderPrincipalID string
	DeciderRoles       []string
	At                 time.Time
}

// ApprovalAuthoritySource answers [ApprovalAuthorityQuery] from current
// durable facts on the decision transaction. internal/platform/execution's
// PromotionApprovalAuthority is the production implementation; it shares the
// routing functions the work-item factory routed with.
type ApprovalAuthoritySource interface {
	CurrentApprovalAuthority(ctx context.Context, ex workitem.Executor, q ApprovalAuthorityQuery) (promotionexec.CurrentApprovalAuthority, error)
}

// authorityPrincipal is the principal whose authority a candidate exercises.
func authorityPrincipal(candidate humanwork.Candidate) string {
	if candidate.Via == humanwork.SourceDelegated && candidate.DelegatedFrom != "" {
		return candidate.DelegatedFrom
	}
	return candidate.PrincipalID
}

// ApprovalAuthorityScope is the scope a promotion approval authority is held
// in: the routed item's organization scope, or its tenant when the item was
// routed with none.
func ApprovalAuthorityScope(item workitem.WorkItem) string {
	if scope := strings.TrimSpace(item.OrganizationScopeID); scope != "" {
		return scope
	}
	return "tenant:" + item.TenantID.String()
}

// routedApprovalAuthority is the authority the item was routed under, built
// only from the durable item: the candidate's authority principal, the node's
// authority class, the term that produced the candidate and the item's scope.
//
// A delegated candidate's assignment was re-resolved by the reassignment and
// no longer records the term the delegator was originally routed by; the
// delegation's own validity is the reassignment's and MembershipOf's to
// enforce. For such a candidate the pinned term is taken from the current
// answer, so the recheck still requires the delegator to be the current holder
// of the authority class, and only the term comparison is waived.
func routedApprovalAuthority(
	item workitem.WorkItem, candidate humanwork.Candidate, revision intent.ProposalRevision, current promotionexec.CurrentApprovalAuthority,
) promotionexec.ApprovalAuthorityBinding {
	termRef := candidate.TermRef
	if candidate.Via == humanwork.SourceDelegated {
		termRef = current.AuthorityRef
	}
	return promotionexec.ApprovalAuthorityBinding{
		ProposalRevisionID: revision.ProposalRevisionID,
		MaterialDigest:     revision.MaterialDigest.Digest,
		RequirementID:      item.ApprovalRequirementRef,
		Authority: promotionexec.ApprovalAuthority{
			PrincipalID:  authorityPrincipal(candidate),
			Class:        promotionexec.ApprovalAuthorityClass(item.NodeID),
			AuthorityRef: termRef,
			Scope:        ApprovalAuthorityScope(item),
			Active:       true,
		},
	}
}

// employmentSubjectOf is the intent's EMPLOYMENT subject, falling back to the
// item's first subject reference -- the same precedence routing used.
func employmentSubjectOf(inst intent.Instance, item workitem.WorkItem) string {
	for _, subject := range inst.Subjects {
		if subject.Kind == "EMPLOYMENT" && subject.SubjectID != "" {
			return subject.SubjectID
		}
	}
	if len(item.SubjectRefs) > 0 {
		return item.SubjectRefs[0]
	}
	return ""
}

// validateRoutedJourneyApprover preserves credential scope checks except for
// a directly assigned, currently verified manager of this proposal's worker.
// The proposal scope belongs to its requester, not necessarily its manager.
// This grants no directory access and never substitutes an elevated principal.
func (e *journeyEngine) validateRoutedJourneyApprover(
	ctx context.Context, ex workitem.Executor, principal *trust.Principal, inst intent.Instance,
	item workitem.WorkItem, candidate humanwork.Candidate, revision intent.ProposalRevision, at time.Time,
) error {
	err := ValidatePromotionJourneyApprover(principal, item, candidate.PrincipalID, at)
	if !errors.Is(err, promotionAuthorityError("APPROVER_ORGANIZATION_SCOPE_MISMATCH")) {
		return err
	}
	if revision.Tenant != principal.Tenant() || inst.Tenant != principal.Tenant() ||
		item.Kind != workitem.KindApproval || item.NodeID != promotionexec.NodeApproveManager ||
		candidate.Via != humanwork.SourceDirect || candidate.TermRef != "term:current-manager-of-worker" {
		return err
	}
	stale, lookupErr := e.recheckApprovalAuthority(ctx, ex, principal, inst, item, candidate, revision, at)
	if lookupErr != nil {
		return lookupErr
	}
	if stale != nil {
		return stale
	}
	return nil
}

// recheckApprovalAuthority re-resolves the routed approval's authority on ex
// and compares it with the authority the item was routed under.
//
// It returns (nil, nil) when the authority is still current or the item is not
// a promotion approval; (stale, nil) when the authority is no longer current,
// which the caller routes INVALIDATED; and (nil, err) when the question could
// not be answered, which refuses the decision without any write. A cell with
// no authority source cannot answer it and fails closed.
func (e *journeyEngine) recheckApprovalAuthority(
	ctx context.Context, ex workitem.Executor, principal *trust.Principal, inst intent.Instance,
	item workitem.WorkItem, candidate humanwork.Candidate, revision intent.ProposalRevision, at time.Time,
) (stale error, err error) {
	if item.Kind != workitem.KindApproval || promotionexec.ApprovalAuthorityClass(item.NodeID) == "" {
		return nil, nil
	}
	if e.authority == nil {
		return nil, fmt.Errorf("%w: this cell was composed with no approval authority source to recheck the approver against",
			ErrProposalDecisionUnavailable)
	}
	current, err := e.authority.CurrentApprovalAuthority(ctx, ex, ApprovalAuthorityQuery{
		TenantID: item.TenantID, Item: item, SubjectID: employmentSubjectOf(inst, item),
		AuthorityPrincipalID: authorityPrincipal(candidate),
		DeciderPrincipalID:   principal.Subject(), DeciderRoles: principal.Roles(), At: at,
	})
	if err != nil {
		return nil, fmt.Errorf("app: journey: recheck the approval authority: %w", err)
	}
	binding := routedApprovalAuthority(item, candidate, revision, current)
	if err := promotionexec.ValidateDistinctApprovers([]promotionexec.ApprovalAuthority{binding.Authority}); err != nil {
		// The routed record itself cannot state an authority: a routing
		// defect, never a reason to invalidate someone's approval.
		return nil, fmt.Errorf("app: journey: the routed approval records no complete authority: %w", err)
	}
	if staleErr := RevalidatePromotionApproverAuthority(binding, current, at); staleErr != nil {
		return staleErr, nil
	}
	return nil, nil
}
