// TestRunAccessAuthorization proves the inspector renders run-access
// decisions: participants see the run, non-participants cannot learn it
// exists. TestRunWallConstrainsWorkItems proves the REFACTOR clause of
// WF-EXT-024: work-item walls derive from the run policy.
package inspect_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func runAccessWalled(t *testing.T) runtime.RunAccessPolicy {
	t.Helper()
	open, err := runtime.NewOpenRunAccess("CONFIDENTIAL_HR_CASE", "HR_CASE_INVESTIGATION")
	if err != nil {
		t.Fatalf("open run: %v", err)
	}
	walled, err := open.Wall(
		runtime.RunParticipant{Principal: "principal:case-manager", Role: "CASE_MANAGER"},
		runtime.RunParticipant{Principal: "principal:investigator", Role: "INVESTIGATOR"},
	)
	if err != nil {
		t.Fatalf("wall run: %v", err)
	}
	return walled
}

func TestRunAccessAuthorization(t *testing.T) {
	walled := runAccessWalled(t)

	participant, err := inspect.RunAuthorization(walled, "principal:case-manager", "HR_CASE_INVESTIGATION")
	if err != nil {
		t.Fatalf("participant authorization: %v", err)
	}
	if err := participant.Validate(); err != nil {
		t.Fatalf("participant decision is not well formed: %v", err)
	}
	if !participant.InstanceDisclosable {
		t.Fatal("a participant must learn the run exists")
	}
	if participant.PolicyVersion != walled.Digest() {
		t.Fatal("the decision must carry the run policy version")
	}
	for _, s := range inspect.TraversalOrder() {
		if !participant.AllowsSection(s) {
			t.Fatalf("a participant is denied section %s", s)
		}
	}
	for _, f := range inspect.RedactableFields() {
		section := inspect.SectionInstance
		if f == inspect.FieldNodeInput || f == inspect.FieldNodeOutput {
			section = inspect.SectionNode
		}
		if !participant.AllowsField(f, section) {
			t.Fatalf("a participant is denied field %s", f)
		}
	}

	stranger, err := inspect.RunAuthorization(walled, "principal:stranger", "HR_CASE_INVESTIGATION")
	if err != nil {
		t.Fatalf("stranger authorization: %v", err)
	}
	if err := stranger.Validate(); err != nil {
		t.Fatalf("denial decision is not well formed: %v", err)
	}
	if stranger.InstanceDisclosable {
		t.Fatal("a non-participant must not learn the run exists")
	}
	if stranger.DenialReason == "" {
		t.Fatal("the denial must carry a reason token")
	}
	for _, s := range inspect.TraversalOrder() {
		if stranger.AllowsSection(s) {
			t.Fatalf("a non-participant is admitted to section %s", s)
		}
	}

	open, err := runtime.NewOpenRunAccess("CONFIDENTIAL_HR_CASE", "HR_CASE_INVESTIGATION")
	if err != nil {
		t.Fatalf("open run: %v", err)
	}
	reader, err := inspect.RunAuthorization(open, "principal:any-reader", "HR_CASE_INVESTIGATION")
	if err != nil {
		t.Fatalf("open-run authorization: %v", err)
	}
	if !reader.InstanceDisclosable {
		t.Fatal("an open run stays visible to the read scope")
	}

	wrongPurpose, err := inspect.RunAuthorization(walled, "principal:case-manager", "WRONG_PURPOSE")
	if err != nil {
		t.Fatalf("wrong-purpose authorization: %v", err)
	}
	if wrongPurpose.InstanceDisclosable {
		t.Fatal("a participant here for the wrong purpose must not learn the run exists")
	}
	var zero runtime.RunAccessPolicy
	if _, err := inspect.RunAuthorization(zero, "principal:case-manager", "HR_CASE_INVESTIGATION"); !errors.Is(err, runtime.ErrRunAccessInvalid) {
		t.Fatalf("the zero policy must fail closed, got %v", err)
	}
}

func TestRunWallConstrainsWorkItems(t *testing.T) {
	walled := runAccessWalled(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	// An open run constrains nothing: items keep whatever visibility their
	// assignment derived.
	open, err := runtime.NewOpenRunAccess("CONFIDENTIAL_HR_CASE", "HR_CASE_INVESTIGATION")
	if err != nil {
		t.Fatalf("open run: %v", err)
	}
	if got := inspect.ConstrainVisibility(workitem.VisibilityTenantGovernance, open); got != workitem.VisibilityTenantGovernance {
		t.Fatalf("an open run narrowed item visibility to %s", got)
	}

	// A walled run narrows every item to its wall: no item of the run may
	// admit anyone the run itself would deny.
	for _, class := range []workitem.Visibility{
		workitem.VisibilityAssigneeOnly,
		workitem.VisibilityCandidateSet,
		workitem.VisibilityOrganizationScope,
		workitem.VisibilityTenantGovernance,
	} {
		if got := inspect.ConstrainVisibility(class, walled); got != workitem.VisibilityAssigneeOnly {
			t.Fatalf("a walled run left item visibility %s at %s", class, got)
		}
	}

	item := workitem.WorkItem{
		Visibility: inspect.ConstrainVisibility(workitem.VisibilityTenantGovernance, walled),
		OwnerKind:  workitem.OwnerPrincipal,
		OwnerRef:   "principal:investigator",
	}
	if m := workitem.MembershipOf(item, "principal:investigator", now); m != workitem.MembershipAssignee {
		t.Fatalf("participant membership = %v, want assignee", m)
	}
	if !workitem.Visible(item, workitem.MembershipOf(item, "principal:investigator", now), false, false) {
		t.Fatal("a run participant cannot see the run's item")
	}
	// The non-participant stands outside the organization scope and the
	// governance set in membership terms, and the wall admits them under
	// neither: even callers the endpoint would otherwise admit by scope or
	// governance see nothing of a walled run's items.
	stranger := workitem.MembershipOf(item, "principal:stranger", now)
	if stranger != workitem.MembershipNone {
		t.Fatalf("stranger membership = %v, want none", stranger)
	}
	if workitem.Visible(item, stranger, true, true) {
		t.Fatal("a non-participant sees a walled run's item through scope or governance")
	}

	// Candidacy stays inside the wall: a candidate set naming a stranger is
	// refused rather than routed into a leak.
	if err := inspect.CheckRunCandidates(walled, []string{"principal:case-manager", "principal:investigator"}); err != nil {
		t.Fatalf("participant candidates refused: %v", err)
	}
	if err := inspect.CheckRunCandidates(walled, []string{"principal:case-manager", "principal:stranger"}); err == nil {
		t.Fatal("a candidate set escaping the wall must be refused")
	}
	if err := inspect.CheckRunCandidates(open, []string{"principal:anyone"}); err != nil {
		t.Fatalf("an open run refuses candidates: %v", err)
	}
}
