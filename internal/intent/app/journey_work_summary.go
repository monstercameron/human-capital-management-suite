package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// maxAssigneeNameLookups bounds how many distinct assignee principals one
// ListJourneys page resolves to a worker display name. Past the bound the
// name is left empty (the id is still disclosed): the list never performs an
// unbounded number of resolver reads.
const maxAssigneeNameLookups = 16

// journeyWorkItemSummary is UXAUDIT-017's viewer-scoped summary of a
// journey's current open work item, built from the work items the list read
// already loaded for the journey -- it performs no read of its own.
//
// The current item is the most recently created non-terminal one (the
// workflow frontier). Disclosure is decided entirely by
// internal/humanwork/workitem's read rules, the same ones WorkService applies:
//
//   - nothing at all unless workitem.Visible admits the viewer (membership, or
//     an ORGANIZATION_SCOPE item in the viewer's own organization scope). The
//     journey surface has no governance-visibility authorizer, so it passes
//     governed=false: it can only ever disclose less than WorkService would;
//   - the assignee only when workitem.ContextVisible admits the viewer;
//   - actions exactly workitem.PermittedActions for the viewer's membership.
//
// name resolves an assignee principal to a display name ("" when unknown).
func journeyWorkItemSummary(
	items []workitem.WorkItem, viewer, viewerOrganizationScope string, now time.Time, name func(string) string,
) *workspace.JourneyWorkItemSummary {
	current, ok := currentOpenWorkItem(items)
	if !ok {
		return nil
	}
	membership := workitem.MembershipOf(current, viewer, now)
	inScope := current.OrganizationScopeID != "" && current.OrganizationScopeID == viewerOrganizationScope
	if !workitem.Visible(current, membership, inScope, false) {
		return nil
	}
	out := &workspace.JourneyWorkItemSummary{
		Kind:             string(current.Kind),
		Status:           string(current.Status),
		DueAt:            current.DeadlineAt,
		ViewerMembership: membershipToken(membership),
	}
	if workitem.ContextVisible(membership) && current.OwnerKind == workitem.OwnerPrincipal {
		out.AssigneePrincipalID = current.OwnerRef
		if name != nil {
			out.AssigneeDisplayName = name(current.OwnerRef)
		}
	}
	for _, action := range workitem.PermittedActions(current, membership) {
		out.ViewerPermittedActions = append(out.ViewerPermittedActions, string(action))
	}
	sort.Strings(out.ViewerPermittedActions)
	return out
}

// currentOpenWorkItem picks the most recently created non-terminal item,
// breaking a creation-time tie by id so the choice is deterministic.
func currentOpenWorkItem(items []workitem.WorkItem) (workitem.WorkItem, bool) {
	var best workitem.WorkItem
	found := false
	for _, item := range items {
		if item.Status.Terminal() {
			continue
		}
		if !found || item.CreatedAt.After(best.CreatedAt) ||
			item.CreatedAt.Equal(best.CreatedAt) && item.WorkItemID.String() > best.WorkItemID.String() {
			best, found = item, true
		}
	}
	return best, found
}

func membershipToken(m workitem.Membership) string {
	switch m {
	case workitem.MembershipCandidate:
		return "CANDIDATE"
	case workitem.MembershipAssignee:
		return "ASSIGNEE"
	case workitem.MembershipClaimant:
		return "CLAIMANT"
	default:
		return "NONE"
	}
}

// assigneeNameResolver returns a memoized, bounded principal-to-display-name
// resolver for one list page. A principal names a worker only through a stable
// identity join -- an exact corpus worker id or key, or a created worker the
// cell's locator resolves -- never by guessing from the principal string.
func (e *journeyEngine) assigneeNameResolver(ctx context.Context, tenant values.TenantId) func(string) string {
	memo := map[string]string{}
	lookups := 0
	return func(principalID string) string {
		principalID = strings.TrimSpace(principalID)
		if principalID == "" {
			return ""
		}
		if cached, ok := memo[principalID]; ok {
			return cached
		}
		if lookups >= maxAssigneeNameLookups {
			return ""
		}
		lookups++
		resolved := corpusWorkerDisplayName(principalID)
		if resolved == "" && e.locate != nil {
			if location, found, err := e.locate(ctx, tenant, principalID); err == nil && found && location.Created != nil {
				resolved = location.Created.DisplayName()
			}
		}
		memo[principalID] = resolved
		return resolved
	}
}

// corpusWorkerDisplayName is the corpus worker whose id or key is exactly ref,
// or "".
func corpusWorkerDisplayName(ref string) string {
	profiles, err := fixtures.Workers()
	if err != nil {
		return ""
	}
	for _, profile := range profiles {
		if profile.ID == ref || profile.Key == ref {
			return profile.DisplayName()
		}
	}
	return ""
}
