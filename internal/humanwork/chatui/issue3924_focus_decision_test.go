package chatui

import "testing"

func TestIssue3924_FocusWatchOutlivesThePressedControl(t *testing.T) {
	// Focus still on the pressed button for a frame: keep watching.
	if focusFieldDone(1, false, false, true) {
		t.Fatal("stopped watching while focus was still on the pressed control")
	}
	// The person moved focus elsewhere: stop.
	if !focusFieldDone(1, false, false, false) {
		t.Fatal("kept fighting a deliberate focus move")
	}
	// Focus fell to the body: keep going so it can be restored.
	if focusFieldDone(1, true, false, false) {
		t.Fatal("gave up while focus was lost")
	}
	// Bounded.
	if !focusFieldDone(45, true, false, false) {
		t.Fatal("never stops")
	}
}

// Issue 3882: on a busy page the re-render that removes the pressed Cancel
// button can land well after 45 frames; the watch must still be running then.
func TestIssue3882_FocusWatchOutlastsASlowRender(t *testing.T) {
	if focusFieldDone(60, false, false, true) {
		t.Fatal("stopped watching while focus was still on the pressed control after 60 frames")
	}
	if !focusFieldDone(45, true, false, false) {
		t.Fatal("must still give up after 45 frames when focus is on the page and nothing was pressed")
	}
	if !focusFieldDone(2000, false, false, true) {
		t.Fatal("must stop eventually")
	}
}
