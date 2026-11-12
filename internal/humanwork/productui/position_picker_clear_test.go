package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_ISSUE3565_PickerOffersANoTargetPositionChoice proves the picker
// renders a labelled choice that names no position: its radio carries an
// empty value, and it is the checked choice when nothing is selected.
func TestTodo_ISSUE3565_PickerOffersANoTargetPositionChoice(t *testing.T) {
	props := PositionPickerProps{
		Name:   "position",
		Legend: "Target position",
		Options: []PositionPickerOptionProps{{
			Reference: "ref-foreman-denver", Code: "IR-FMN", Title: "Foreman", Organization: "Field Operations", VacancyWindow: "Open", ReservationState: "Available",
		}},
		NoneLabel:  "No target position",
		NoneDetail: "Propose the move without naming a position.",
		OnSelect:   func(string) {},
	}
	out, err := ui.RenderToString(PositionPicker(props))
	if err != nil {
		t.Fatalf("render picker: %v", err)
	}
	if !strings.Contains(out, "No target position") {
		t.Fatalf("picker omitted the clear choice:\n%s", out)
	}
	if !strings.Contains(out, `value=""`) {
		t.Fatalf("clear choice has no empty radio value:\n%s", out)
	}
	if !strings.Contains(out, `checked`) {
		t.Fatalf("no choice is checked for an unselected position:\n%s", out)
	}
}

// TestTodo_ISSUE3565_PickerWithoutNoneLabelIsUnchanged keeps every other picker
// caller as it was: no clear choice unless the caller asks for one.
func TestTodo_ISSUE3565_PickerWithoutNoneLabelIsUnchanged(t *testing.T) {
	props := PositionPickerProps{
		Name:    "position",
		Legend:  "Target position",
		Options: []PositionPickerOptionProps{{Reference: "ref-a", Title: "Foreman"}},
	}
	out, err := ui.RenderToString(PositionPicker(props))
	if err != nil {
		t.Fatalf("render picker: %v", err)
	}
	if strings.Contains(out, "position-picker-option-none") {
		t.Fatalf("picker without a NoneLabel rendered a clear choice:\n%s", out)
	}
}
