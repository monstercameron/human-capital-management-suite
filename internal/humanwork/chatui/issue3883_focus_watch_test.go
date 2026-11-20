package chatui

import (
	"os"
	"regexp"
	"testing"
)

// Issue 3883: at 1280 px the header's Pinned and Members shortcuts have no
// width while the details panel is open, so the focus call made as Close
// details is pressed lands on nothing. The watch keeps trying until the
// shortcut is back, and never fights a deliberate move.
func TestIssue3883_FocusWatchWaitsForTheOpenerToReturn(t *testing.T) {
	// Focus fell to the page, the opener has no layout yet: keep waiting, do not focus.
	if focus, done := issue3883WatchStep(5, true, false, false, false); focus || done {
		t.Fatalf("opener not laid out: focus=%v done=%v, want neither", focus, done)
	}
	// The opener is back and focus is still on the page: focus it.
	if focus, _ := issue3883WatchStep(30, true, true, false, false); !focus {
		t.Fatal("opener is back and focus is lost, but it was not focused")
	}
	// A slow render: the pressed Close button is still focused, keep watching.
	if _, done := issue3883WatchStep(100, false, false, false, true); done {
		t.Fatal("stopped while focus was still on the pressed control")
	}
	// Focus is on the opener already: nothing to do, keep watching for a late blur.
	if focus, _ := issue3883WatchStep(2, false, true, true, false); focus {
		t.Fatal("refocused an opener that already has focus")
	}
	// The person moved focus elsewhere: stop, never steal it back.
	if focus, done := issue3883WatchStep(3, false, true, false, false); focus || !done {
		t.Fatalf("person moved focus: focus=%v done=%v, want done without focusing", focus, done)
	}
	// It gives up eventually.
	if _, done := issue3883WatchStep(2000, true, false, false, false); !done {
		t.Fatal("never gave up")
	}
}

func TestIssue3883_CloseDetailsClickStartsTheFocusWatch(t *testing.T) {
	raw, err := os.ReadFile("render.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?s)issue3883CloseLayerKind\(action\).*?watchChatLayerOpenerFocus\(kind`).Match(raw) {
		t.Fatal("the Close details click path does not start the opener focus watch")
	}
}
