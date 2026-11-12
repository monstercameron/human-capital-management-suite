package productclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestUXBLIND070_WorkflowReferencesRouteRoundTrip(t *testing.T) {
	state, err := ParseState("/workspace/app/admin/workflows", "locale=en-US&workflow=workflows.clock&workflow_references=1")
	if err != nil {
		t.Fatalf("parse workflow reference route: %v", err)
	}
	if !state.Request.WorkflowShowReferences || state.Request.WorkflowID != "workflows.clock" {
		t.Fatalf("parsed workflow reference state = %+v", state.Request)
	}
	if got := CanonicalHref(state); got != "/workspace/app/admin/workflows?locale=en-US&workflow=workflows.clock&workflow_references=1" {
		t.Fatalf("canonical reference route = %q", got)
	}

	home, err := ParseState(string(productui.Path(productui.PageHome)), "workflow_references=1")
	if err != nil {
		t.Fatalf("parse unrelated route: %v", err)
	}
	if home.Request.WorkflowShowReferences {
		t.Fatal("workflow reference toggle leaked into another page")
	}
}

func TestIssue3567_WorkflowCatalogListRouteRoundTrip(t *testing.T) {
	state, err := ParseState("/workspace/app/admin/workflows", "locale=de-DE&workflow=workflows.clock&workflow_q=clock&workflow_status=draft&workflow_sort=name&workflow_page=2")
	if err != nil {
		t.Fatalf("parse workflow catalog list route: %v", err)
	}
	if state.Request.WorkflowQuery != "clock" || state.Request.WorkflowListStatus != "draft" || state.Request.WorkflowListSort != "name" || state.Request.WorkflowListPage != 2 {
		t.Fatalf("parsed workflow catalog list state = %+v", state.Request)
	}
	want := "/workspace/app/admin/workflows?locale=de-DE&workflow=workflows.clock&workflow_page=2&workflow_q=clock&workflow_sort=name&workflow_status=draft"
	if got := CanonicalHref(state); got != want {
		t.Fatalf("canonical workflow catalog list route = %q, want %q", got, want)
	}

	home, err := ParseState(string(productui.Path(productui.PageHome)), "workflow_status=draft&workflow_q=clock")
	if err != nil {
		t.Fatalf("parse unrelated route: %v", err)
	}
	if home.Request.WorkflowListStatus != "" || home.Request.WorkflowQuery != "" {
		t.Fatal("workflow catalog list state leaked into another page")
	}
}
