package chatui

import (
	"regexp"
	"strings"
	"testing"
)

func issue3901Pane(t *testing.T, m Model, local localUI) string {
	t.Helper()
	return renderNode(t, threadPane(m, handlers{local: local}))
}

var issue3901Checkbox = regexp.MustCompile(`<input[^>]*id="thread-also-channel"[^>]*>`)

func issue3901Checked(t *testing.T, markup string) bool {
	t.Helper()
	box := issue3901Checkbox.FindString(markup)
	if box == "" {
		t.Fatalf("no \"Also send to\" checkbox in the thread pane")
	}
	return strings.Contains(box, "checked")
}

// TestIssue3901_AlsoChoiceBelongsToOneThread: the choice is kept by root
// message, so another thread opens unchecked and the same thread comes back
// checked, in every language.
func TestIssue3901_AlsoChoiceBelongsToOneThread(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3894Model(locale)
		m.ThreadAlso = map[string]bool{"p1": true}
		if !issue3901Checked(t, issue3901Pane(t, m, localUI{})) {
			t.Errorf("%s: the thread the choice was made in lost it", locale)
		}
		m.ThreadParentID = "p2"
		if issue3901Checked(t, issue3901Pane(t, m, localUI{})) {
			t.Errorf("%s: the choice leaked into another thread", locale)
		}
	}
}

// TestIssue3901_PageChoiceWinsOverAnOlderModel: a change made on this page shows
// at once, before the model has been redrawn.
func TestIssue3901_PageChoiceWinsOverAnOlderModel(t *testing.T) {
	m := issue3894Model("en-US")
	on := localUI{threadUI: threadChoiceUI{also: map[string]bool{"p1": true}}}
	if !issue3901Checked(t, issue3901Pane(t, m, on)) {
		t.Error("a box just ticked draws unchecked")
	}
	m.ThreadAlso = map[string]bool{"p1": true}
	off := localUI{threadUI: threadChoiceUI{also: map[string]bool{"p1": false}}}
	if issue3901Checked(t, issue3901Pane(t, m, off)) {
		t.Error("a box just cleared draws checked")
	}
}

// TestIssue3901_SetThreadAlsoReportsToTheModel: ticking the box tells the client,
// by thread, so the choice survives leaving the page.
func TestIssue3901_SetThreadAlsoReportsToTheModel(t *testing.T) {
	var gotParent string
	var gotAlso bool
	m := issue3894Model("en-US")
	m.Callbacks.ThreadAlsoChanged = func(p string, a bool) { gotParent, gotAlso = p, a }
	box := &localUI{}
	setThreadAlso(m, localStore{box: box}, "p1", true)
	if gotParent != "p1" || !gotAlso || !box.threadUI.also["p1"] {
		t.Fatalf("callback %q %v, page %v", gotParent, gotAlso, box.threadUI.also)
	}
	if threadAlsoChecked(m, *box) != true {
		t.Fatal("the open thread does not read its own choice")
	}
}
