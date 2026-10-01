package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_080(t *testing.T) {
	appearance, ok := LookupPage(PageAppearance)
	if !ok || appearance.Route != "/workspace/app/admin/appearance" || appearance.ParentNav != PageAdmin {
		t.Fatalf("appearance registry = %+v", appearance)
	}
	if page, _, _, ok := RouteProfiles("/workspace/app/appearance"); !ok || page != PageAppearance {
		t.Fatal("legacy appearance route did not resolve to the canonical page")
	}
	if canonical := Path(PageAppearance); canonical != "/workspace/app/admin/appearance" {
		t.Fatalf("appearance canonical path = %q", canonical)
	}
	if icon := iconPath("info"); icon == fallbackIconPath {
		t.Fatal("information icon is not registered")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		if text := ResolveProductLocale(locale).Text("page.myself.subtitle"); strings.Contains(strings.ToLower(text), "payroll") || strings.Contains(text, "الرواتب") {
			t.Fatalf("%s Myself subtitle still promises payroll: %q", locale, text)
		}
	}
}

func TestTodo_UXBLIND_080_Browser(t *testing.T) {
	markup, err := ui.RenderToString(SelfServiceBoundary(SelfServiceBoundaryProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="self-service-boundary-glyph"`) || strings.Contains(markup, `d="`+fallbackIconPath+`"`) {
		t.Fatalf("view-only banner is not using the shared information icon: %s", markup)
	}
}

func TestTodo_UXBLIND_081(t *testing.T) {
	called := false
	empty := WorkflowDraftView{DraftID: "draft-1", Name: "Untitled workflow", SemanticVersion: "0.1.0"}
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowEditor, WorkflowEditorProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Draft: empty,
		OnDelete: func() { called = true },
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `workflow-action="delete-draft"`) || called {
		t.Fatalf("empty draft delete affordance = %q, callback called=%t", markup, called)
	}
	nonEmpty := empty
	nonEmpty.Nodes = []WorkflowDraftNode{{ID: "step-1", Label: "Step"}}
	nonEmptyMarkup, err := ui.RenderToString(ui.CreateElement(WorkflowEditor, WorkflowEditorProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Draft: nonEmpty,
		OnDelete: func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nonEmptyMarkup, `workflow-action="delete-draft"`) {
		t.Fatal("authored draft exposes the empty-draft delete action")
	}
}

func TestTodo_UXBLIND_081_Browser(t *testing.T) {
	markup, err := ui.RenderToString(workflowDraftDeleteConfirmation(
		I18nProps{Locale: ResolveProductLocale("en-US")}, true, ui.Handler{}, ui.Handler{},
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="alertdialog"`, `aria-modal="true"`, `workflow-draft-delete-title`, `workflow-action="delete-draft-cancel"`, `workflow-action="delete-draft-confirm"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("delete confirmation missing %q: %s", want, markup)
		}
	}
}
