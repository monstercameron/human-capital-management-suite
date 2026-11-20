package chatui

import (
	"strings"
	"testing"
)

// TestIssue3895_PreviewShowsTheFieldAsItIs: the reply preview and the editor it
// returns to carry the text the field held (the bold the person just applied),
// not the older draft the page was last drawn with.
func TestIssue3895_PreviewShowsTheFieldAsItIs(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3894Model(locale)
		m.ThreadDrafts = map[string]string{"p1": "Review draft"}
		live := localUI{threadUI: threadChoiceUI{live: threadLiveDraft{parent: "p1", base: "Review draft", value: "**Review draft**", ok: true}}}

		editing := renderNode(t, threadPane(m, handlers{local: live}))
		if !strings.Contains(editing, `data-chat-value="**Review draft**"`) {
			t.Errorf("%s: the editor went back to the older text", locale)
		}
		live.threadComposerPreview = true
		previewing := renderNode(t, threadPane(m, handlers{local: live}))
		if !strings.Contains(previewing, "<strong>Review draft</strong>") {
			t.Errorf("%s: the preview does not show the bold just applied", locale)
		}
	}
}

// TestIssue3895_NewerModelDraftWins: once the model has a different draft the
// captured text is dropped, so a sent, cleared or restored reply is never
// overwritten by an old capture.
func TestIssue3895_NewerModelDraftWins(t *testing.T) {
	m := issue3894Model("en-US")
	m.ThreadDrafts = map[string]string{"p1": "Newer"}
	live := localUI{threadUI: threadChoiceUI{live: threadLiveDraft{parent: "p1", base: "Review draft", value: "**Review draft**", ok: true}}}
	if got := threadDraftNow(m, live); got != "Newer" {
		t.Fatalf("draft = %q", got)
	}
	m.ThreadDrafts = map[string]string{"p1": "Review draft"}
	m.ThreadParentID = "p2"
	if got := threadDraftNow(m, live); got != "" {
		t.Fatalf("a capture for p1 shows in p2: %q", got)
	}
}

// TestIssue3895_CaptureTellsTheModel: before the toolbar or the preview redraws
// the form the field's text is captured and reported, so the preview toggle is
// enabled for it and the model agrees.
func TestIssue3895_CaptureTellsTheModel(t *testing.T) {
	old := readThreadField
	defer func() { readThreadField = old }()
	readThreadField = func() (string, bool) { return "**Review draft**", true }
	m := issue3894Model("en-US")
	m.ThreadDrafts = map[string]string{"p1": "Review draft"}
	var reported string
	m.Callbacks.ThreadDraftChanged = func(p, v string) { reported = p + "=" + v }
	box := &localUI{}
	captureThreadDraft(m, localStore{box: box})
	if reported != "p1=**Review draft**" {
		t.Fatalf("model told %q", reported)
	}
	if got := threadDraftNow(m, *box); got != "**Review draft**" {
		t.Fatalf("draft after capture = %q", got)
	}
	readThreadField = func() (string, bool) { return "", false }
	before := *box
	captureThreadDraft(m, localStore{box: box})
	if box.threadUI.live != before.threadUI.live {
		t.Fatal("a preview with no field overwrote the capture")
	}
}
