package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/widgetreg"
)

func TestTodo_WFPAGE_013_ProductRenderer(t *testing.T) {
	renderer := NewWorkflowInputRenderer()
	view, err := renderer.RenderWorkflowInput(widgetreg.InputRenderRequest{
		Widget: pagedef.WorkflowPageWidget{ID: "salary", Kind: pagedef.WorkflowWidgetMoney, Binding: "salary", Label: "Salary", Currency: "EUR", PayBasis: "annual"},
		Locale: "de-DE", Error: "Ungültiger Betrag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Currency != "EUR" || view.PayBasis != "annual" || view.ErrorID != "salary-error" || view.Locale != "de-de" {
		t.Fatalf("product renderer lost governed widget semantics: %+v", view)
	}
}
