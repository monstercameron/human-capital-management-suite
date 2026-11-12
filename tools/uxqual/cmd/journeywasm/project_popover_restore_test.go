package main

import "testing"

// Issue #3615: Cancel must return each project form control to the value it
// was rendered with, for checkboxes and for value-bearing controls.
func TestProjectRestoreControl_Issue3615(t *testing.T) {
	cases := []struct {
		name           string
		kind           projectControlKind
		current, saved string
		wantValue      string
		wantChecked    bool
		wantChanged    bool
	}{
		{name: "text edit is discarded", kind: projectControlValue, current: "Unsaved name", saved: "Task board", wantValue: "Task board", wantChanged: true},
		{name: "unchanged text is not written", kind: projectControlValue, current: "Task board", saved: "Task board", wantValue: "Task board", wantChanged: false},
		{name: "new task blank title is restored to empty", kind: projectControlValue, current: "Draft title", saved: "", wantValue: "", wantChanged: true},
		{name: "non-ASCII saved title is restored exactly", kind: projectControlValue, current: "x", saved: "Bögen — Ärger ✓", wantValue: "Bögen — Ärger ✓", wantChanged: true},
		{name: "select returns to saved option", kind: projectControlValue, current: "BOARD_SWIMLANE_GROUPING_PRIORITY", saved: "BOARD_SWIMLANE_GROUPING_NONE", wantValue: "BOARD_SWIMLANE_GROUPING_NONE", wantChanged: true},
		{name: "checkbox unchecked by edit is checked again", kind: projectControlCheckbox, current: "false", saved: "true", wantChecked: true, wantChanged: true},
		{name: "checkbox checked by edit is cleared again", kind: projectControlCheckbox, current: "true", saved: "false", wantChecked: false, wantChanged: true},
		{name: "unchanged checkbox is not written", kind: projectControlCheckbox, current: "true", saved: "true", wantChecked: true, wantChanged: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := projectRestoreControl(tc.kind, tc.current, tc.saved)
			if got.Value != tc.wantValue || got.Checked != tc.wantChecked || changed != tc.wantChanged {
				t.Fatalf("projectRestoreControl(%q, %q, %q) = (%+v, changed=%v), want (value=%q checked=%v changed=%v)",
					tc.kind, tc.current, tc.saved, got, changed, tc.wantValue, tc.wantChecked, tc.wantChanged)
			}
		})
	}
}
