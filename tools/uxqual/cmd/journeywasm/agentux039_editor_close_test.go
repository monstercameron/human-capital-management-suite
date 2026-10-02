package main

import (
	"strings"
	"testing"
)

func TestTodo_AGENTUX_039_EditorCancelAndEscape(t *testing.T) {
	// The first field focused is one a person can type in: not a hidden input
	// and not the read-only agent address that open the form.
	for _, excluded := range []string{"[type=hidden]", "[readonly]", "[disabled]"} {
		if !strings.Contains(personaEditorFirstField, ":not("+excluded+")") {
			t.Fatalf("the first-field selector admits %s inputs: %s", excluded, personaEditorFirstField)
		}
	}
	if strings.Contains(personaEditorFirstField, "button") {
		t.Fatal("opening the editor would focus a button instead of a field")
	}

	unchanged := []personaEditorField{{Value: "Policy Helper", Initial: "Policy Helper"}, {Value: "true", Initial: "true"}}
	changed := []personaEditorField{{Value: "Policy Helper", Initial: "Policy Helper"}, {Value: "Answers benefits questions.", Initial: "Answers policy questions."}}
	if personaEditorDirty(unchanged, 2, 2) || !personaEditorDirty(changed, 2, 2) || !personaEditorDirty(unchanged, 1, 2) || !personaEditorDirty(unchanged, 3, 2) || personaEditorDirty(nil, 0, 0) {
		t.Fatal("the editor does not tell a changed form from an untouched one")
	}

	// An untouched editor closes at once and never asks.
	asked := 0
	ask := func(answer bool) func() bool { return func() bool { asked++; return answer } }
	if !personaEditorClose(false, ask(false)) || asked != 0 {
		t.Fatalf("an untouched editor asked before closing (%d)", asked)
	}
	// A changed editor asks once, stays open on "no" and closes on "yes".
	if personaEditorClose(true, ask(false)) || asked != 1 {
		t.Fatalf("a changed editor closed without agreement (%d)", asked)
	}
	if !personaEditorClose(true, ask(true)) || asked != 2 {
		t.Fatalf("a changed editor did not close on agreement (%d)", asked)
	}
	if personaEditorClose(true, nil) {
		t.Fatal("a changed editor closed with no way to ask")
	}

	// Escape belongs to the editor only when no list inside it is open and no
	// other handler has taken it.
	for _, tc := range []struct {
		key               string
		listOpen, handled bool
		want              bool
	}{
		{"Escape", false, false, true}, {"Escape", true, false, false}, {"Escape", false, true, false},
		{"Enter", false, false, false}, {"Esc", false, false, false},
	} {
		if got := personaEditorEscapeCloses(tc.key, tc.listOpen, tc.handled); got != tc.want {
			t.Errorf("Escape rule for %+v = %v", tc, got)
		}
	}
}
