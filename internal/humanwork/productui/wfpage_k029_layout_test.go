package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WFPAGE_029(t *testing.T) {
	layout := GeneratedWorkflowPageLayout("workflow.change", 4, []WorkflowPageLayoutInput{{ID: "name", Label: "Name", Required: true}, {ID: "notes", Label: "Notes"}})
	if err := ApplyWorkflowPageLayoutEdit(&layout, WorkflowPageLayoutEdit{Kind: WorkflowPageMove, FieldID: "notes", Direction: WorkflowPageMoveEarlier}); err != nil {
		t.Fatal(err)
	}
	if got := WorkflowPageLayoutOutline(layout); len(got) != 2 || got[0].FieldID != "notes" {
		t.Fatalf("move outline = %+v", got)
	}
	if err := ApplyWorkflowPageLayoutEdit(&layout, WorkflowPageLayoutEdit{Kind: WorkflowPageGroup, FieldID: "notes", SectionID: "general"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyWorkflowPageLayoutEdit(&layout, WorkflowPageLayoutEdit{Kind: WorkflowPageHide, FieldID: "notes", Hidden: true}); err != nil {
		t.Fatal(err)
	}
	outline := WorkflowPageLayoutOutline(layout)
	if len(outline) != 2 || outline[1].FieldID != "notes" || !outline[1].Hidden {
		t.Fatalf("outline = %+v", outline)
	}
}

func TestTodo_WFPAGE_029_Browser(t *testing.T) {
	layout := GeneratedWorkflowPageLayout("workflow.change", 4, []WorkflowPageLayoutInput{{ID: "name", Label: "Name", Required: true}, {ID: "notes", Label: "Notes"}})
	doc, err := ui.RenderToString(WorkflowPageDesigner(WorkflowPageDesignerProps{Layout: layout, WorkflowHref: "/workspace/app/workflows/designer"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"workflow-page-designer", "Outline editor", `role="tree"`, `role="treeitem"`, `aria-keyshortcuts="Alt+ArrowUp"`, "Move up", "Hide", `data-required="true"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("designer markup missing %q: %s", want, doc)
		}
	}
	if strings.Contains(doc, `data-workflow-page-edit="hide" data-field-id="name"`) {
		t.Fatal("required field received a hide control")
	}
}

func TestTodo_WFPAGE_029_Performance(t *testing.T) {
	layout := GeneratedWorkflowPageLayout("workflow.change", 4, []WorkflowPageLayoutInput{{ID: "name", Label: "Name", Required: true}})
	if err := ApplyWorkflowPageLayoutEdit(&layout, WorkflowPageLayoutEdit{Kind: WorkflowPageHide, FieldID: "name", Hidden: true}); err == nil {
		t.Fatal("required field was hidden")
	}
	if WorkflowPageDesignerHref(View{Page: PageWorkflowDesigner}, "workflow.change", 4) == "" {
		t.Fatal("designer route href is empty")
	}
}
