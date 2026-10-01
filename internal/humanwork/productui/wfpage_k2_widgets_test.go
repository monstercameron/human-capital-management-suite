package productui

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WFPAGE_014(t *testing.T) {
	rows, err := AddRepeatingRow(nil, 2, "emergency-1")
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Values["name"] = "Avery"
	rows[0].Errors["name"] = "Name is required"
	markup, err := ui.RenderToString(RepeatingGroup(RepeatingGroupProps{ID: "contacts", Label: "Emergency contact", AddLabel: "Add contact", RemoveLabel: "Remove contact", MaxRows: 2, Rows: rows}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Emergency contact 1") || !strings.Contains(markup, "Name is required") || !strings.Contains(markup, "workflow-page-remove-row") {
		t.Fatalf("repeating markup = %s", markup)
	}
	computed, err := ui.RenderToString(ComputedWidget(ComputedWidgetProps{ID: "total", Label: "Total", Value: "100.00", Description: "Calculated from declared inputs"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(computed, "readonly") || strings.Contains(computed, "name=\"total\" type=\"text\"") {
		t.Fatalf("computed widget is editable: %s", computed)
	}
}

func TestTodo_WFPAGE_014_Browser(t *testing.T) {
	rows, err := AddRepeatingRow(nil, 1, "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddRepeatingRow(rows, 1, "two"); !errors.Is(err, ErrRepeatingGroupCap) {
		t.Fatalf("cap error = %v", err)
	}
	rows = RemoveRepeatingRow(rows, "one")
	if len(rows) != 0 {
		t.Fatalf("rows after remove = %+v", rows)
	}
	markup, err := ui.RenderToString(SignatureWidget(SignatureWidgetProps{ID: "ack", Label: "Acknowledgement", AssuranceLevel: "ENHANCED", State: "SIGNED"}))
	if err != nil || !strings.Contains(markup, "Assurance level: ENHANCED") {
		t.Fatalf("signature markup = %s, %v", markup, err)
	}
}

func TestTodo_WFPAGE_014_Security(t *testing.T) {
	rows := []RepeatingRow{{ID: "one", Values: map[string]string{"secret": "private"}, Errors: map[string]string{}}}
	cloned, err := AddRepeatingRow(rows, 2, "two")
	if err != nil {
		t.Fatal(err)
	}
	cloned[0].Values["secret"] = "changed"
	if rows[0].Values["secret"] != "private" {
		t.Fatal("row helper aliased caller state")
	}
	markup, err := ui.RenderToString(ComputedWidget(ComputedWidgetProps{ID: "total", Label: "Total", Value: "10", Error: "Server recalculated this value"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `name="total"`) || !strings.Contains(markup, "Server recalculated") {
		t.Fatalf("computed value can be posted or error missing: %s", markup)
	}
}
