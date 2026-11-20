package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

func issue3953Model() Model {
	m := chatbug071Model()
	m.Draft = "my version"
	m.DraftConflicts = map[string]string{"room": "the other tab's version"}
	m.Callbacks.SendMessage = func(string, string) {}
	return m
}

// The other tab's draft is kept beside the composer, with a choice, instead of
// being dropped silently; it is not drawn for another room's composer.
func TestIssue3953_ConflictIsShownWithAChoice(t *testing.T) {
	m := issue3953Model()
	note := stdhtml.UnescapeString(renderNode(t, draftSyncNote(m, "chat-composer")))
	for _, want := range []string{
		"Another tab or device saved a different draft for this conversation. Nothing was replaced.",
		"the other tab's version",
		`data-action="draft-conflict-keep"`, ">Keep mine<",
		`data-action="draft-conflict-use"`, ">Use the other draft<",
		`data-action="draft-conflict-both"`, ">Keep both<",
		`aria-live="polite"`, `dir="auto"`,
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the conflict note lacks %q: %s", want, note)
		}
	}
	if draftSyncNote(m, "thread-composer") != nil {
		t.Error("the thread composer shows the main composer's conflict")
	}
	m.SelectedID = "dm"
	if draftSyncNote(m, "chat-composer") != nil {
		t.Error("another conversation shows this conversation's conflict")
	}
	m = issue3953Model()
	m.Draft = "the other tab's version"
	if draftSyncNote(m, "chat-composer") != nil {
		t.Error("a conflict is shown although both drafts are the same")
	}
}

// The page draws the note under the composer through the workspace's own
// binding.
func TestIssue3953_ComposerDrawsTheConflict(t *testing.T) {
	m := issue3953Model()
	page := chatbug071Page(t, m, &mentionBox{conversationID: m.SelectedID})
	form := chatbug071Composer(t, page)
	if !strings.Contains(form, `data-action="draft-conflict-both"`) {
		t.Fatalf("the composer does not offer the other draft: %s", form)
	}
}

func TestIssue3953_ChoicesNeverLoseEitherVersionUnlessAsked(t *testing.T) {
	cases := []struct {
		action, want string
	}{
		{actionDraftConflictKeep, "my version  \n"},
		{actionDraftConflictUse, "other"},
		{actionDraftConflictBoth, "my version\n\nother"},
	}
	for _, tc := range cases {
		got, ok := issue3953Resolution(tc.action, "my version  \n", "other")
		if !ok || got != tc.want {
			t.Errorf("%s = %q, want %q", tc.action, got, tc.want)
		}
	}
	if got, _ := issue3953Resolution(actionDraftConflictBoth, "  ", "other"); got != "other" {
		t.Errorf("keeping both over an empty composer = %q", got)
	}

	var drafted, resolved string
	m := issue3953Model()
	m.Callbacks.DraftChanged = func(_ string, v string) { drafted = v }
	m.Callbacks.DraftConflictResolved = func(id string) { resolved = id }
	if !draftSyncAction(m, actionDraftConflictBoth) || drafted != "my version\n\nthe other tab's version" || resolved != "room" {
		t.Errorf("keep both: drafted %q resolved %q", drafted, resolved)
	}
	drafted, resolved = "", ""
	draftSyncAction(m, actionDraftConflictKeep)
	if drafted != "" || resolved != "room" {
		t.Errorf("keep mine changed the draft %q or did not settle (%q)", drafted, resolved)
	}
}

// Issue 3950: edits that could not be saved are said, in every language, with a
// retry that is a real button.
func TestIssue3950_UnsavedEditsAreSaidWithARetry(t *testing.T) {
	retried := false
	for locale, want := range map[string]string{
		"en-US": "Your latest changes are not saved yet.",
		"de-DE": "Deine letzten Änderungen sind noch nicht gespeichert.",
		"ar":    "لم تُحفظ آخر تغييراتك بعد.",
	} {
		m := chatbug071Model()
		m.Locale, m.DraftUnsaved = locale, true
		m.Callbacks.RetryDraftSave = func() { retried = true }
		note := stdhtml.UnescapeString(renderNode(t, draftSyncNote(m, "chat-composer")))
		if !strings.Contains(note, want) || !strings.Contains(note, `data-action="draft-save-retry"`) || !strings.Contains(note, `role="status"`) {
			t.Errorf("%s: unsaved note = %s", locale, note)
		}
		if locale != "en-US" && strings.Contains(note, "Save now") {
			t.Errorf("%s: retry button not translated: %s", locale, note)
		}
	}
	m := chatbug071Model()
	m.Callbacks.RetryDraftSave = func() { retried = true }
	retried = false
	if draftSyncNote(m, "chat-composer") != nil {
		t.Error("a note is shown when everything is saved")
	}
	draftSyncAction(m, actionDraftSaveRetry)
	if !retried {
		t.Error("Save now did not ask for a write")
	}
}

func TestIssue3953_CopyIsComplete(t *testing.T) {
	for key := range issue3953Copy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if strings.TrimSpace(issue3953Copy[locale][key]) == "" {
				t.Errorf("%s lacks %s", locale, key)
			}
		}
	}
}
