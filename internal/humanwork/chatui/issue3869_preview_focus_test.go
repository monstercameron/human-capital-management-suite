package chatui

import "testing"

// TestIssue3869_PreviewToggleReturnsFocusToItself: flipping Preview redraws the
// composer, which replaces the button the keyboard user pressed. The flip asks
// for focus to return to the same composer's Preview toggle, so Tab carries on
// from there instead of restarting at the page's first link.
func TestIssue3869_PreviewToggleReturnsFocusToItself(t *testing.T) {
	cases := []struct {
		id       string
		selector string
		read     func(localUI) bool
	}{
		{threadComposerID, `form.thread-composer .composer-preview-toggle`, func(u localUI) bool { return u.threadComposerPreview }},
		{"composer", `form.chat-composer .composer-preview-toggle`, func(u localUI) bool { return u.composerPreview }},
	}
	for _, c := range cases {
		var ui localUI
		var focused []string
		update := func(change func(*localUI)) { change(&ui) }
		focus := func(selector string) { focused = append(focused, selector) }
		for i, wantOn := range []bool{true, false} {
			composerPreviewFlip(c.id, update, focus)
			if c.read(ui) != wantOn {
				t.Fatalf("%s flip %d: preview = %v, want %v", c.id, i+1, c.read(ui), wantOn)
			}
			if len(focused) != i+1 || focused[i] != c.selector {
				t.Fatalf("%s flip %d: focus requests = %q, want the toggle %q each time", c.id, i+1, focused, c.selector)
			}
		}
		other := localUI{}
		if c.id == threadComposerID && ui.composerPreview != other.composerPreview {
			t.Errorf("thread flip changed the channel composer's preview")
		}
	}
}

// TestIssue3869_PreviewFocusSelectorsMatchTheMarkup: the selectors the flip
// asks for must name the real toggle in the rendered thread form.
func TestIssue3869_PreviewFocusSelectorsMatchTheMarkup(t *testing.T) {
	root := issue3873Pane(t, "en-US", localUI{threadComposerPreview: true}, "Test **thread**")
	toggle := issue3873Toggle(t, root)
	if chatPolishAttr(toggle, "data-id") != threadComposerID {
		t.Fatalf("toggle data-id = %q, want %q", chatPolishAttr(toggle, "data-id"), threadComposerID)
	}
	if !chatPolishAncestor(toggle, "thread-composer") {
		t.Fatal("the Preview toggle is not inside the thread-composer form the focus selector names")
	}
	if got := composerPreviewFocusSelector(threadComposerID); got != `form.thread-composer .composer-preview-toggle` {
		t.Fatalf("thread selector = %q", got)
	}
}
