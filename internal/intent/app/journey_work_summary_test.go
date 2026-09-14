package app

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var summaryClock = time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)

func summaryItem(status workitem.Status, visibility workitem.Visibility, created time.Time) workitem.WorkItem {
	return workitem.WorkItem{
		WorkItemID: uuid.New(), Kind: workitem.KindApproval, Status: status,
		OwnerKind: workitem.OwnerPrincipal, OwnerRef: "principal:approver",
		Visibility: visibility, OrganizationScopeID: "org-north",
		DeadlineAt: summaryClock.Add(72 * time.Hour), CreatedAt: created, ProposalRef: "sha256:proposal",
	}
}

// countingNames records every principal it is asked to name.
type countingNames struct{ asked []string }

func (c *countingNames) name(ref string) string {
	c.asked = append(c.asked, ref)
	return "Named " + ref
}

func TestJourneyWorkItemSummaryDisclosesTheAssigneeToTheAssignee(t *testing.T) {
	item := summaryItem(workitem.StatusAssigned, workitem.VisibilityAssigneeOnly, summaryClock)
	names := &countingNames{}
	got := journeyWorkItemSummary([]workitem.WorkItem{item}, "principal:approver", "org-elsewhere", summaryClock, names.name)
	if got == nil {
		t.Fatal("the assignee received no summary")
	}
	if got.AssigneePrincipalID != "principal:approver" || got.AssigneeDisplayName != "Named principal:approver" {
		t.Fatalf("assignee = %q / %q", got.AssigneePrincipalID, got.AssigneeDisplayName)
	}
	if got.ViewerMembership != "ASSIGNEE" || !slices.Equal(got.ViewerPermittedActions, []string{"claim"}) {
		t.Fatalf("membership/actions = %s %v, want ASSIGNEE [claim]", got.ViewerMembership, got.ViewerPermittedActions)
	}
	if !got.DueAt.Equal(item.DeadlineAt) || got.Kind != "APPROVAL" || got.Status != "ASSIGNED" {
		t.Fatalf("summary = %+v", got)
	}
}

func TestJourneyWorkItemSummaryGivesACandidateTheClaimButNoAssignee(t *testing.T) {
	item := summaryItem(workitem.StatusAvailable, workitem.VisibilityCandidateSet, summaryClock)
	item.OwnerKind, item.OwnerRef = workitem.OwnerCandidateSet, "queue:finance"
	item.Assignment.Resolution.Candidates = []humanwork.Candidate{{PrincipalID: "principal:finance-1", Via: humanwork.SourceDirect}}
	names := &countingNames{}
	got := journeyWorkItemSummary([]workitem.WorkItem{item}, "principal:finance-1", "", summaryClock, names.name)
	if got == nil || got.ViewerMembership != "CANDIDATE" || !slices.Equal(got.ViewerPermittedActions, []string{"claim"}) {
		t.Fatalf("candidate summary = %+v", got)
	}
	if got.AssigneePrincipalID != "" || len(names.asked) != 0 {
		t.Fatalf("a candidate-set item has no single assignee to name: %+v asked=%v", got, names.asked)
	}
}

// TestJourneyWorkItemSummarySecurity is the unit half of
// TestTodo_UXAUDIT_017_Security: a viewer the work item rules admit only by
// organization scope sees the item's existence, status and deadline but never
// its assignee (and the name resolver is never even consulted), and a viewer
// the rules do not admit at all gets no summary.
func TestJourneyWorkItemSummarySecurity(t *testing.T) {
	scoped := summaryItem(workitem.StatusAssigned, workitem.VisibilityOrganizationScope, summaryClock)
	names := &countingNames{}
	got := journeyWorkItemSummary([]workitem.WorkItem{scoped}, "principal:colleague", "org-north", summaryClock, names.name)
	if got == nil {
		t.Fatal("an in-scope viewer of an ORGANIZATION_SCOPE item is admitted by the rules and should see the item")
	}
	if got.AssigneePrincipalID != "" || got.AssigneeDisplayName != "" || len(names.asked) != 0 {
		t.Fatalf("a scope-only viewer learned the assignee: %+v asked=%v", got, names.asked)
	}
	if got.ViewerMembership != "NONE" || len(got.ViewerPermittedActions) != 0 || !got.DueAt.Equal(scoped.DeadlineAt) {
		t.Fatalf("scope-only summary = %+v", got)
	}

	for _, c := range []struct {
		name       string
		visibility workitem.Visibility
		scope      string
	}{
		{"organization scope, viewer outside it", workitem.VisibilityOrganizationScope, "org-south"},
		{"assignee only", workitem.VisibilityAssigneeOnly, "org-north"},
		{"candidate set", workitem.VisibilityCandidateSet, "org-north"},
		// The journey surface has no governance authorizer: it must never
		// widen to TENANT_GOVERNANCE visibility on its own.
		{"tenant governance", workitem.VisibilityTenantGovernance, "org-north"},
	} {
		item := summaryItem(workitem.StatusAssigned, c.visibility, summaryClock)
		if got := journeyWorkItemSummary([]workitem.WorkItem{item}, "principal:outsider", c.scope, summaryClock, names.name); got != nil {
			t.Errorf("%s: a non-admitted viewer received %+v", c.name, got)
		}
	}
}

func TestJourneyWorkItemSummaryPicksTheNewestOpenItem(t *testing.T) {
	done := summaryItem(workitem.StatusCompleted, workitem.VisibilityAssigneeOnly, summaryClock.Add(2*time.Hour))
	older := summaryItem(workitem.StatusAssigned, workitem.VisibilityAssigneeOnly, summaryClock)
	newer := summaryItem(workitem.StatusAssigned, workitem.VisibilityAssigneeOnly, summaryClock.Add(time.Hour))
	newer.DeadlineAt = summaryClock.Add(240 * time.Hour)
	got := journeyWorkItemSummary([]workitem.WorkItem{newer, done, older}, "principal:approver", "", summaryClock, nil)
	if got == nil || !got.DueAt.Equal(newer.DeadlineAt) {
		t.Fatalf("current item is not the newest open one: %+v", got)
	}
	if journeyWorkItemSummary([]workitem.WorkItem{done}, "principal:approver", "", summaryClock, nil) != nil {
		t.Fatal("a journey whose work items are all terminal has no current work item")
	}
	if journeyWorkItemSummary(nil, "principal:approver", "", summaryClock, nil) != nil {
		t.Fatal("no work items, no summary")
	}
	for m, want := range map[workitem.Membership]string{workitem.MembershipNone: "NONE", workitem.MembershipCandidate: "CANDIDATE", workitem.MembershipAssignee: "ASSIGNEE", workitem.MembershipClaimant: "CLAIMANT"} {
		if got := membershipToken(m); got != want {
			t.Errorf("membershipToken(%d) = %q, want %q", m, got, want)
		}
	}
}

func TestAssigneeNameResolverIsExactMemoizedAndBounded(t *testing.T) {
	calls := 0
	engine := &journeyEngine{locate: func(context.Context, values.TenantId, string) (WorkerLocation, bool, error) {
		calls++
		return WorkerLocation{}, false, nil
	}}
	resolve := engine.assigneeNameResolver(context.Background(), fixtures.Tenant)

	profiles, err := fixtures.Workers()
	if err != nil || len(profiles) == 0 {
		t.Fatalf("corpus: %v", err)
	}
	if got := resolve(profiles[0].ID); got != profiles[0].DisplayName() {
		t.Fatalf("corpus worker id resolved to %q, want %q", got, profiles[0].DisplayName())
	}
	if calls != 0 {
		t.Fatalf("an exact corpus match must not reach the locator, calls=%d", calls)
	}
	if got := resolve("principal:not-a-worker"); got != "" {
		t.Fatalf("an unresolvable principal was named %q", got)
	}
	resolve("principal:not-a-worker")
	if calls != 1 {
		t.Fatalf("repeat lookups are not memoized: calls=%d", calls)
	}
	for i := 0; i < 3*maxAssigneeNameLookups; i++ {
		resolve(fmt.Sprintf("principal:distinct-%d", i))
	}
	if calls > maxAssigneeNameLookups {
		t.Fatalf("resolver made %d locator reads, bound is %d", calls, maxAssigneeNameLookups)
	}
	if resolve("") != "" {
		t.Fatal("empty principal named")
	}
}
