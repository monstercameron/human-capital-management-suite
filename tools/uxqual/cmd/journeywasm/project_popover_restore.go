package main

// projectControlKind names how a project form control holds its value: a
// checkbox by its checked state, anything else by its value.
type projectControlKind string

const (
	projectControlCheckbox projectControlKind = "checkbox"
	projectControlValue    projectControlKind = "value"
)

// projectRestoredControl is the state a control returns to when its popover
// is closed with Cancel or Close.
type projectRestoredControl struct {
	Value   string
	Checked bool
}

// projectRestoreControl returns the state a control must hold after its form
// is canceled. saved is the value the control was rendered with (the saved or
// initial value, in data-saved-value or data-saved-checked); current is what it
// holds now. For a checkbox both strings are "true" or "false". changed reports
// whether the control must be written at all.
func projectRestoreControl(kind projectControlKind, current, saved string) (projectRestoredControl, bool) {
	if kind == projectControlCheckbox {
		want := saved == "true"
		return projectRestoredControl{Checked: want}, (current == "true") != want
	}
	return projectRestoredControl{Value: saved}, current != saved
}
