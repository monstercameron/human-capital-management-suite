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
