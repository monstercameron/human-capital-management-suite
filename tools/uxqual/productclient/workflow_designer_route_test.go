package productclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_WF_UI_002_WorkflowDesignerRouteRoundTrip(t *testing.T) {
	path := productui.Path(productui.PageWorkflowDesigner)
	state, err := ParseState(path, "workflow=promotion.approval&run=run-1042&locale=ar&nav=collapsed")
	if err != nil {
		t.Fatalf("ParseState workflow designer: %v", err)
	}
	if state.Page != productui.PageWorkflowDesigner || state.Request.WorkflowID != "promotion.approval" || state.Request.WorkflowRunID != "run-1042" || state.Request.Locale != "ar" || !state.Request.NavCollapsed {
		t.Fatalf("workflow designer state = %+v", state)
	}
	canonical := CanonicalHref(state)
	resolved, err := ParseState(path, canonical[strings.IndexByte(canonical, '?')+1:])
	if err != nil {
		t.Fatalf("parse canonical workflow designer route: %v", err)
	}
	if resolved.Request.WorkflowID != state.Request.WorkflowID || resolved.Request.WorkflowRunID != state.Request.WorkflowRunID {
		t.Fatalf("round-trip workflow designer state = %+v", resolved.Request)
	}
}

func TestTodo_WF_UI_002_WorkflowDesignerRouteRejectsUnownedState(t *testing.T) {
	path := productui.Path(productui.PageWorkflowDesigner)
	state, err := ParseState(path, "workflow=promotion.approval&person=worker-secret&token=credential")
	if err != nil {
		t.Fatalf("unowned safe route keys should be discarded, not echoed: %v", err)
	}
	if state.Request.SelectedPerson != "" || state.Provided["person"] || state.Provided["token"] {
		t.Fatalf("workflow designer retained unowned route state: %+v", state)
	}
}
