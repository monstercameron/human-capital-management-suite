package widgetreg

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// TestTodo_WFPAGE_013 proves all scalar, choice, list and reference-picker
// kinds resolve through one registry contract with accessible relationships.
func TestTodo_WFPAGE_013(t *testing.T) {
	r := NewWorkflowInputRegistry()
	for _, kind := range []pagedef.WorkflowWidgetKind{
		pagedef.WorkflowWidgetText, pagedef.WorkflowWidgetInteger, pagedef.WorkflowWidgetDecimal,
		pagedef.WorkflowWidgetCheckbox, pagedef.WorkflowWidgetChoice, pagedef.WorkflowWidgetList,
		pagedef.WorkflowWidgetPersonPicker, pagedef.WorkflowWidgetPositionPicker,
		pagedef.WorkflowWidgetOrganizationPicker, pagedef.WorkflowWidgetCostCentrePicker,
		pagedef.WorkflowWidgetMoney, pagedef.WorkflowWidgetLocalDate, pagedef.WorkflowWidgetInstant,
	} {
		widget := pagedef.WorkflowPageWidget{ID: "field", Kind: kind, Binding: "input", Label: "Input", Currency: "EUR", PayBasis: "annual"}
		view, err := r.Render(InputRenderRequest{Widget: widget, Locale: "en-US", Error: "invalid", DisabledReason: "not allowed", ViewerMaySearch: true})
		if err != nil {
			t.Fatalf("kind %s: %v", kind, err)
		}
		if view.LabelID == "" || view.ErrorID == "" || view.DisabledReasonID == "" || view.Role == "" {
			t.Fatalf("kind %s lacks accessible associations: %+v", kind, view)
		}
	}
}

// TestTodo_WFPAGE_013_Browser is the component-level browser contract: the
// semantic result preserves RTL, picker search authorization, and effective
// dating without requiring a server or a real browser process.
func TestTodo_WFPAGE_013_Browser(t *testing.T) {
	r := NewWorkflowInputRegistry()
	widget := pagedef.WorkflowPageWidget{ID: "person", Kind: pagedef.WorkflowWidgetPersonPicker, Binding: "person_id", Label: "Person"}
	view, err := r.Render(InputRenderRequest{Widget: widget, Locale: "ar", ViewerMaySearch: false})
	if err != nil {
		t.Fatal(err)
	}
	if view.Direction != "rtl" || view.TypeAhead {
		t.Fatalf("RTL unauthorized picker projection = %+v", view)
	}
	widget = pagedef.WorkflowPageWidget{ID: "date", Kind: pagedef.WorkflowWidgetLocalDate, Binding: "start_date", Label: "Start date"}
	view, err = r.Render(InputRenderRequest{Widget: widget, Locale: "de-DE"})
	if err != nil || view.EffectiveDating == "" {
		t.Fatalf("effective-date projection = %+v, %v", view, err)
	}
}
