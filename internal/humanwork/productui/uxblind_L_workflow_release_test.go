package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXBLIND-034 audit (2026-10-23): the release panel named its next step
// ("Next: Run test") while Run test, Submit for approval and Publish and
// activate were plain spans no one could act on. The panel now renders an
// action it is authorized for as a real control, and explains an action it
// is not authorized for instead of presenting a dead next step.
func TestTodo_UXBLIND_034_ReleaseActionsUnavailable(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	draft := WorkflowDraftView{DraftID: "draft-1", Name: "Promotion path", SemanticVersion: "1.1.0"}
	markup, err := ui.RenderToString(workflowReleasePanel(i18n, draft, nil, workflowReleaseActions{}))
	if err != nil {
		t.Fatal(err)
	}
	// The required release path stays visible with its states.
	for _, want := range []string{"From draft to live", "Test", "Ready", "Approval", "Activation", "Run test", "Submit for approval", "Publish and activate"} {
		if !strings.Contains(markup, want) {
			t.Errorf("release panel omitted %q:\n%s", want, markup)
		}
	}
	// ...but no dead control, and an honest explanation per action.
	for _, want := range []string{
		"Run test is not available for this draft.",
		"Submit for approval is not available for this draft.",
		"Publish and activate is not available for this draft.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("release panel omitted unavailability note %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "<button") {
		t.Errorf("release panel exposed a control without an authorized action:\n%s", markup)
	}
	if strings.Contains(markup, "Next: Run test") {
		t.Errorf("release panel named a dead next action:\n%s", markup)
	}
}

func TestTodo_UXBLIND_034_ReleaseActionsUnavailableLocales(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		i18n := I18nProps{Locale: ResolveProductLocale(locale)}
		draft := WorkflowDraftView{DraftID: "draft-1", Name: "Promotion path", SemanticVersion: "1.1.0"}
		markup, err := ui.RenderToString(workflowReleasePanel(i18n, draft, nil, workflowReleaseActions{}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "⟦") {
			t.Errorf("%s release panel leaked an untranslated key:\n%s", locale, markup)
		}
		if strings.Contains(markup, "<button") {
			t.Errorf("%s release panel exposed a control without an authorized action:\n%s", locale, markup)
		}
	}
}

func TestTodo_UXBLIND_034_ReleaseActionsAvailable(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	draft := WorkflowDraftView{DraftID: "draft-1", Name: "Promotion path", SemanticVersion: "1.1.0"}
	called := map[string]bool{}
	release := workflowReleaseActions{
		test:    func() { called["test"] = true },
		submit:  func() { called["submit"] = true },
		publish: func() { called["publish"] = true },
	}
	markup, err := ui.RenderToString(workflowReleasePanel(i18n, draft, nil, release))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<button", "Run test", "Submit for approval", "Publish and activate", "Next: Run test"} {
		if !strings.Contains(markup, want) {
			t.Errorf("authorized release panel omitted %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "is not available for this draft") {
		t.Errorf("authorized release panel kept an unavailability note:\n%s", markup)
	}
}

func TestTodo_UXBLIND_034_ReleaseSaveAction(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	unsaved := WorkflowDraftView{Name: "Untitled", SemanticVersion: "0.1.0"}

	withSave, err := ui.RenderToString(workflowReleasePanel(i18n, unsaved, nil, workflowReleaseActions{save: func() {}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withSave, "<button") || !strings.Contains(withSave, "Save draft") || !strings.Contains(withSave, "Next: Save draft") {
		t.Errorf("unsaved draft with a save command did not offer it:\n%s", withSave)
	}

	withoutSave, err := ui.RenderToString(workflowReleasePanel(i18n, unsaved, nil, workflowReleaseActions{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutSave, "<button") || !strings.Contains(withoutSave, "Save draft is not available for this draft.") {
		t.Errorf("unsaved draft without a save command was not honest:\n%s", withoutSave)
	}
}

func TestTodo_UXBLIND_034_ReleaseBlockedKeepsProblemGuidance(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	draft := WorkflowDraftView{DraftID: "draft-1", Name: "Promotion path", SemanticVersion: "1.1.0"}
	problems := []workflowPathProblem{{Key: "workflow_editor.problem_task_assignee"}}
	markup, err := ui.RenderToString(workflowReleasePanel(i18n, draft, problems, workflowReleaseActions{test: func() {}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Blocked") {
		t.Errorf("blocked release panel lost its state:\n%s", markup)
	}
	if strings.Contains(markup, "<button") {
		t.Errorf("blocked release panel offered an action before the problems are fixed:\n%s", markup)
	}
}

func TestTodo_UXBLIND_034_EditorThreadsReleaseActions(t *testing.T) {
	draft := WorkflowDraftView{DraftID: "draft-1", Name: "Promotion path", SemanticVersion: "1.1.0"}
	authorized := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{
		Draft: draft, OnReleaseTest: func() {}, OnReleaseSubmit: func() {}, OnReleasePublish: func() {},
	}))
	if !strings.Contains(authorized, "<button") || !strings.Contains(authorized, "Next: Run test") {
		t.Errorf("editor did not thread authorized release actions:\n%s", authorized)
	}
	silent := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft}))
	if !strings.Contains(silent, "Run test is not available for this draft.") {
		t.Errorf("editor without release actions did not explain:\n%s", silent)
	}

	unsaved := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{
		Draft: WorkflowDraftView{Name: "Untitled", SemanticVersion: "0.1.0"}, OnCreate: func(WorkflowDraftCreateRequest) {},
	}))
	if !strings.Contains(unsaved, "Next: Save draft") {
		t.Errorf("editor did not offer the save it is authorized for:\n%s", unsaved)
	}
}
