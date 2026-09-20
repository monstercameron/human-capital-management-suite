package productui

import (
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WF_UI_005_PaletteRendersAuthorizedCatalog(t *testing.T) {
	items := workflowPaletteFixture()
	before := append([]WorkflowPaletteItem(nil), items...)
	markup, err := ui.RenderToString(WorkflowPalette(WorkflowPaletteProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Items:     items,
		OnInsert:  func(WorkflowPaletteItem) {},
	}))
	if err != nil {
		t.Fatalf("render workflow palette: %v", err)
	}
	for _, want := range []string{
		`aria-labelledby="workflow-palette-heading"`,
		`id="workflow-palette-search"`,
		`aria-label="Search workflow blocks"`,
		`aria-label="Control flow"`,
		`aria-label="People"`,
		`data-kind="block"`,
		`data-kind="fragment"`,
		`data-kind="template"`,
		`class="workflow-palette-fragment"`,
		`data-insert-mode="group"`,
		`<summary aria-label="Show Promotion review fragment details"`,
		`Add Promotion review group`,
		`Use Promotion template`,
		`Internal Mutation`,
		`Compensation Required`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow palette missing %q:\n%s", want, markup)
		}
	}
	if !slices.Equal(items, before) {
		t.Fatalf("workflow palette mutated caller values: got %+v want %+v", items, before)
	}
}

func TestTodo_WF_UI_005_SearchMatchesMetadataWithoutMutatingEntries(t *testing.T) {
	items := workflowPaletteFixture()
	before := append([]WorkflowPaletteItem(nil), items...)
	groups := filterWorkflowPalette(items, "compensation", "Other")
	if len(groups) != 1 || len(groups["People"]) != 1 || groups["People"][0].ID != "template.promotion" {
		t.Fatalf("compensation search result = %+v", groups)
	}
	groups = filterWorkflowPalette(items, "kernel.approval", "Other")
	if len(groups) != 1 || len(groups["Control flow"]) != 1 || groups["Control flow"][0].ID != "kernel.approval" {
		t.Fatalf("step-type search result = %+v", groups)
	}
	if !slices.Equal(items, before) {
		t.Fatalf("search mutated caller values: got %+v want %+v", items, before)
	}
}

func TestTodo_WF_UI_005_Browser(t *testing.T) {
	css := workflowPaletteStylesheet()
	for _, want := range []string{
		`position:sticky`,
		`max-block-size:calc(100dvh - var(--hcm-space-4))`,
		`@media (max-width:1440px)`,
		`@media (max-width:640px)`,
		`grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))`,
		`:focus-within`,
		`var(--hcm-radius-control)`,
		`var(--hcm-motion-fast)`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("workflow palette stylesheet missing %q", want)
		}
	}
	for _, forbidden := range []string{"margin-left", "padding-left", "border-left", "#", "rgb(", "hsl("} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("workflow palette stylesheet contains fixed-direction or fixed-color declaration %q", forbidden)
		}
	}

	for _, test := range []struct{ locale, title, add string }{
		{locale: "de-DE", title: "Bausteinbibliothek", add: "Gruppe Promotion review hinzufügen"},
		{locale: "ar", title: "مكتبة الكتل", add: "إضافة مجموعة Promotion review"},
	} {
		markup, err := ui.RenderToString(WorkflowPalette(WorkflowPaletteProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale(test.locale)},
			Items:     workflowPaletteFixture(),
			OnInsert:  func(WorkflowPaletteItem) {},
		}))
		if err != nil {
			t.Fatalf("render %s workflow palette: %v", test.locale, err)
		}
		if !strings.Contains(markup, test.title) || !strings.Contains(markup, test.add) || strings.Contains(markup, "⟦workflow_palette.") {
			t.Fatalf("%s workflow palette localization is incomplete:\n%s", test.locale, markup)
		}
	}
}

func workflowPaletteFixture() []WorkflowPaletteItem {
	return []WorkflowPaletteItem{
		{ID: "kernel.approval", Version: 1, Name: "Approval", Kind: "BLOCK", Domain: "Control flow", Description: "Collect a governed decision.", EffectClass: "PURE", Reversal: "NO_EFFECT", Status: "ACTIVE", StepType: "APPROVAL"},
		{ID: "fragment.promotion-review", Version: 2, Name: "Promotion review", Kind: "FRAGMENT", Domain: "People", Description: "Manager and finance approval group.", EffectClass: "PURE", Reversal: "NO_EFFECT", Status: "ACTIVE"},
		{ID: "template.promotion", Version: 3, Name: "Promotion", Kind: "TEMPLATE", Domain: "People", Description: "Promotion with compensation controls.", EffectClass: "INTERNAL_MUTATION", Reversal: "COMPENSATION_REQUIRED", Status: "ACTIVE"},
	}
}
