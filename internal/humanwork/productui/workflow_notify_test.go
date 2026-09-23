package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
)

// TestTodo_WF_NOTIFY_001_Browser proves the editor tells an author who hears about
// the selected step, from the declaration runs publish from, in every locale.
func TestTodo_WF_NOTIFY_001_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		i18n := I18nProps{Locale: ResolveProductLocale(locale)}
		for selected, stepType := range map[string]string{"review": "APPROVAL", "collect": "TASK", "end_complete": "END"} {
			markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{I18nProps: i18n, Draft: workflowEditorFixture(), Palette: workflowPaletteFixture(), SelectedNodeID: selected}))
			if !strings.Contains(markup, "workflow-inspector-notify") || !strings.Contains(markup, i18n.Text("workflow_editor.notify_title")) || !strings.Contains(markup, i18n.Text("workflow_editor.notify_channel")) {
				t.Fatalf("%s %s: no notification section", locale, selected)
			}
			for _, notice := range notifyplan.ForStep(stepType) {
				key := workflowNotifyKey(notice)
				if key == "" || !strings.Contains(markup, i18n.Text(key)) {
					t.Fatalf("%s %s: declared notice %+v is not shown (key %q)", locale, selected, notice, key)
				}
			}
		}
	}
	// A step that routes work says where its owner comes from, because nothing
	// on the step sets one; an End routes nothing and says nothing about it.
	en := I18nProps{Locale: ResolveProductLocale("en-US")}
	for selected, want := range map[string]bool{"review": true, "collect": true, "end_complete": false} {
		markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{I18nProps: en, Draft: workflowEditorFixture(), Palette: workflowPaletteFixture(), SelectedNodeID: selected}))
		if strings.Contains(markup, "workflow-inspector-notify-routing") != want {
			t.Fatalf("%s: routing line shown = %v, want %v", selected, !want, want)
		}
	}
	// A step that tells nobody carries no section rather than an empty one.
	if workflowNotifySection(I18nProps{Locale: ResolveProductLocale("en-US")}, "DECISION") != nil {
		t.Fatal("a decision step claimed to notify someone")
	}
}

// TestTodo_WF_NOTIFY_001_Copy proves the bell words every status the server can
// send, each differently, in every locale.
func TestTodo_WF_NOTIFY_001_Copy(t *testing.T) {
	statuses := []string{notifyplan.StatusSentForReview, notifyplan.StatusFinishedApproved, notifyplan.StatusFinishedDeclined, notifyplan.StatusFinishedFailed, notifyplan.StatusFinished}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageWork)
		view.Locale = ResolveProductLocale(locale)
		seen := map[string]bool{}
		for _, status := range statuses {
			title, state, ok := workflowUpdateNotificationKeys(WorkflowNotification{Purpose: notifyplan.PurposeUpdate, Status: status})
			if !ok || state == "notifications.unknown" || seen[state] {
				t.Fatalf("%s: status %s has no copy of its own (%q)", locale, status, state)
			}
			seen[state] = true
			view.WorkflowNotifications = []WorkflowNotification{{ID: "status-1", JourneyID: "journey-9", WorkerName: "Adrian Cole", Purpose: notifyplan.PurposeUpdate, Status: status}}
			doc := renderNotificationSubtree(t, view)
			for _, want := range []string{view.Locale.Text(title), view.Locale.Text(state), "Adrian Cole", "journey=journey-9"} {
				if !strings.Contains(doc, want) {
					t.Fatalf("%s %s: missing %q in %s", locale, status, want, doc)
				}
			}
			if locale != "en-US" && (view.Locale.Text(state) == ResolveProductLocale("en-US").Text(state)) {
				t.Fatalf("%s: %s is untranslated", locale, state)
			}
		}
	}
	if _, _, ok := workflowUpdateNotificationKeys(WorkflowNotification{Purpose: "APPROVAL", Status: "ASSIGNED"}); ok {
		t.Fatal("an approval notice was worded as a status update")
	}
	if _, state, _ := workflowUpdateNotificationKeys(WorkflowNotification{Purpose: notifyplan.PurposeUpdate, Status: "FUTURE"}); state != "notifications.unknown" {
		t.Fatalf("unknown status worded as %q", state)
	}
}
