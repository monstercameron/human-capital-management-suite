package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type issue3805Roles struct {
	snapshot roleaccess.Snapshot
	err      error
	scope    string
}

func (r *issue3805Roles) Load(_ context.Context, _ values.TenantId, scope string) (roleaccess.Snapshot, error) {
	r.scope = scope
	return r.snapshot, r.err
}

func issue3805Starts(ctx context.Context, tenant values.TenantId, canStart bool) ([]workspace.WorkflowStartConfig, error) {
	if tenant != "tenant-a" || !canStart {
		return nil, errors.New("unexpected read")
	}
	return []workspace.WorkflowStartConfig{
		{WorkflowID: "wf-bank", Name: "Change of bank details", Availability: "available", Keywords: []string{"payroll", "chat-offerable", "chat-cue:bank details"}},
		{WorkflowID: "wf-untagged", Name: "New hire onboarding", Availability: "available", Keywords: []string{"onboarding", "chat-cue:new hire"}},
		{WorkflowID: "wf-quarantined", Name: "Leave request", Availability: "quarantined", Keywords: []string{"chat-offerable", "chat-cue:time off"}},
		{WorkflowID: "wf-nocue", Name: "Transfer", Availability: "available", Keywords: []string{"chat-offerable"}},
	}, nil
}

func TestIssue3805_WorkflowCatalogOffersOnlyTaggedStartableWorkflows_Security(t *testing.T) {
	roles := &issue3805Roles{snapshot: roleaccess.Snapshot{
		Roles:       []roleaccess.Role{{ID: "employee", Active: true}, {ID: "contractor", Active: true}},
		Assignments: []roleaccess.Assignment{{WorkerRef: "ada", RoleIDs: []string{"employee"}}, {WorkerRef: "kim", RoleIDs: []string{"contractor"}}},
		PagePermissions: []roleaccess.PagePermission{
			{RoleID: "employee", PageID: "workflow-start", View: true},
			{RoleID: "employee", PageID: "journeys", View: true, Create: true},
			{RoleID: "contractor", PageID: "workflow-start", View: true},
		},
	}}
	catalog := NewAgentUXAmbientWorkflowCatalog(issue3805Starts, roles)
	if catalog == nil {
		t.Fatal("catalog not composed")
	}
	got, err := catalog.OfferableWorkflows(t.Context(), "tenant-a", "ada")
	if err != nil || len(got) != 1 || got[0].ID != "wf-bank" || got[0].Name != "Change of bank details" || len(got[0].Cues) != 1 {
		t.Fatalf("offerable for a person who may start workflows: %+v %v", got, err)
	}
	if roles.scope != agentOrgScope("tenant-a") {
		t.Fatalf("role facts read under %q", roles.scope)
	}
	// Who may not start workflows is offered none, and an unknown person none.
	for _, person := range []string{"kim", "nobody", ""} {
		if got, err = catalog.OfferableWorkflows(t.Context(), "tenant-a", person); err != nil || len(got) != 0 {
			t.Fatalf("%q was offered %+v %v", person, got, err)
		}
	}
	// An unreadable policy fails closed rather than offering from stale facts.
	roles.err = errors.New("offline")
	if got, err = catalog.OfferableWorkflows(t.Context(), "tenant-a", "ada"); err == nil || len(got) != 0 {
		t.Fatalf("offered with unreadable roles: %+v %v", got, err)
	}
	if NewAgentUXAmbientWorkflowCatalog(nil, roles) != nil || NewAgentUXAmbientWorkflowCatalog(issue3805Starts, nil) != nil {
		t.Fatal("a catalog without a source or role facts was composed")
	}
}
