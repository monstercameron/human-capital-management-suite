package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// ErrPromotionAuthorityStale is returned before claiming or completing a
// promotion approval when the verified caller's credential is no longer valid.
// Relationship and grant freshness are represented by the routed assignment;
// a new assignment is required when those server-side facts change.
var ErrPromotionAuthorityStale = errors.New("app: promotion approval authority is stale")

// promotionAuthorityError exposes a bounded diagnostic code, never identity
// or credential contents. Callers retain errors.Is against the public sentinel.
type promotionAuthorityError string

func (e promotionAuthorityError) Error() string {
	return ErrPromotionAuthorityStale.Error() + ": " + string(e)
}
func (e promotionAuthorityError) Unwrap() error     { return ErrPromotionAuthorityStale }
func (e promotionAuthorityError) ErrorCode() string { return string(e) }

// RevalidatePromotionApproverAuthority is the application boundary for the
// promotionexec authority contract. It is intentionally pure: callers provide
// the pinned binding and a fresh directory result, and no lifecycle or
// WorkItem mutation occurs until this returns nil.
func RevalidatePromotionApproverAuthority(binding promotionexec.ApprovalAuthorityBinding, current promotionexec.CurrentApprovalAuthority, at time.Time) error {
	if err := promotionexec.ValidateCurrentAuthority(binding, current, at); err != nil {
		return fmt.Errorf("%w: %w", ErrPromotionAuthorityStale, err)
	}
	return nil
}

// ValidatePromotionJourneyApprover is the live decision boundary. It checks
// the current authenticated credential before any WorkItem side effect and
// confirms that the approval node is a known authority class and that the
// principal remains in the server-resolved candidate set. Proposal binding
// checks remain owned by workflow/steps/approval.Complete.
func ValidatePromotionJourneyApprover(principal *trust.Principal, item workitem.WorkItem, approver string, at time.Time) error {
	if principal == nil || strings.TrimSpace(approver) == "" || principal.Subject() != approver || at.IsZero() {
		return promotionAuthorityError("APPROVER_IDENTITY_MISMATCH")
	}
	// This helper is intentionally a no-op for non-promotion approval nodes;
	// the journey engine can be composed alongside other intent families.
	if promotionexec.ApprovalAuthorityClass(item.NodeID) == "" {
		return nil
	}
	if at.Before(principal.IssuedAt()) || !at.Before(principal.ExpiresAt()) {
		return promotionAuthorityError("APPROVER_CREDENTIAL_EXPIRED_OR_NOT_YET_VALID")
	}
	if _, ok := item.Assignment.Resolution.Authorizes(approver); !ok {
		return promotionAuthorityError("APPROVER_NOT_ROUTED")
	}
	if item.OrganizationScopeID != "" && item.OrganizationScopeID != principal.OrganizationScopeID() {
		return promotionAuthorityError("APPROVER_ORGANIZATION_SCOPE_MISMATCH")
	}
	return nil
}

// ValidatePromotionApprovalHistory enforces SoD across the complete live
// approval route. The same principal may not decide finance and current-
// manager requirements, even when two WorkItems independently route to it.
func ValidatePromotionApprovalHistory(items []workitem.WorkItem, current workitem.WorkItem, approver string) error {
	currentClass := promotionexec.ApprovalAuthorityClass(current.NodeID)
	if currentClass == "" || strings.TrimSpace(approver) == "" {
		return nil
	}
	for _, prior := range items {
		if prior.WorkItemID == current.WorkItemID || prior.Kind != workitem.KindApproval ||
			prior.Status != workitem.StatusCompleted || prior.CompletedBy != approver {
			continue
		}
		priorClass := promotionexec.ApprovalAuthorityClass(prior.NodeID)
		if priorClass != "" && priorClass != currentClass {
			return fmt.Errorf("%w: principal %q already decided the %s approval and cannot decide %s", ErrProposalDecisionSeparation, approver, priorClass, currentClass)
		}
	}
	return nil
}
