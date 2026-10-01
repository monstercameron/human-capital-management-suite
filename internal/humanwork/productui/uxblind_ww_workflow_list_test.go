package productui

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_111(t *testing.T) {
	selected := workflowViewerFixture()
	props := WorkflowDesignerPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, BaseHref: "/workspace/app/admin/workflows?workflow=selected", Selected: &selected, Navigate: func(string) {}}
	catalog := []WorkflowCatalogItem{
		{WorkflowID: "people.promotion", Name: "Promotion approval", Version: 9, SemanticVersion: "2026.9.0", Status: "ACTIVE"},
		{WorkflowID: "people.onboarding", Name: "New hire onboarding", Version: 4, SemanticVersion: "2026.4.0", Status: "DRAFT"},
		{WorkflowID: "reference.prototype", Name: "Prototype reference", Version: 2, SemanticVersion: "0.2.0", Status: "ACTIVE"},
		{WorkflowID: "legacy.retired", Name: "Legacy workflow", Version: 8, SemanticVersion: "2025.8.0", Status: "RETIRED"},
	}
	markup, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, catalog, "people.promotion"))
	if err != nil {
		t.Fatalf("render scalable workflow list: %v", err)
	}
	for _, want := range []string{
		`type="search"`, "Name, workflow ID, or category", "Recently updated", "Active 1", "Draft 1", "Review only 1", "Retired 1",
		`id="workflow-catalog-table"`, "Workflow", "Version", "Status", "Category", "Updated", "Owner", "Promotion approval", "New hire onboarding",
		`workflow_status=draft`, `tabindex="0"`, `aria-activedescendant=`,
	} {
		if !strings.Contains(strings.ToLower(markup), strings.ToLower(want)) { // HTML attribute names are case-insensitive (GWC emits tabIndex).
			t.Fatalf("scalable workflow list missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Prototype reference") || strings.Contains(markup, "Legacy workflow") {
		t.Fatal("reference workflows were visible before the reference toggle was enabled")
	}
	if strings.Contains(markup, "— Draft") {
		t.Fatal("draft name gained a presentation suffix")
	}

	state := workflowListState{Query: "onboarding", Status: workflowListStatusDraft, Sort: workflowListSortName, Page: 3, ShowReferences: true}
	href := workflowListStateHref(props.BaseHref, "people.promotion", state)
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	if values.Get("workflow") != "people.promotion" || values.Get(workflowListQueryKey) != "onboarding" || values.Get(workflowListStatusKey) != workflowListStatusDraft || values.Get(workflowListSortKey) != workflowListSortName || values.Get(workflowListPageKey) != "3" || values.Get(workflowListReferences) != "1" {
		t.Fatalf("workflow list state was not kept in URL: %v", values)
	}
}

func TestTodo_UXBLIND_111_Browser(t *testing.T) {
	props := WorkflowDesignerPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("de-DE")}, BaseHref: "/workspace/app/admin/workflows", Navigate: func(string) {}}
	rows := []WorkflowCatalogItem{
		{WorkflowID: "custom.alpha", Name: "Alpha", Version: 1, SemanticVersion: "1.0.0", Status: "ACTIVE"},
		{WorkflowID: "custom.draft", Name: "Draft one", Version: 2, SemanticVersion: "0.2.0", Status: "DRAFT"},
	}
	markup, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, rows, "custom.draft"))
	if err != nil {
		t.Fatalf("render workflow browser contract: %v", err)
	}
	for _, want := range []string{`role="search"`, `role="group"`, `aria-label="Workflow-Katalog"`, `aria-current="page"`, `tabindex="0"`, `data-column="owner"`, "Workflows suchen", "Zuletzt aktualisiert", "Workflow-Katalog"} {
		if !strings.Contains(strings.ToLower(markup), strings.ToLower(want)) { // HTML attribute names are case-insensitive (GWC emits tabIndex).
			t.Fatalf("workflow browser contract missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "⟦workflow_list.") {
		t.Fatal("workflow list has an untranslated catalog key")
	}
}

func TestTodo_UXBLIND_111_Performance(t *testing.T) {
	props := WorkflowDesignerPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, BaseHref: "/workspace/app/admin/workflows"}
	catalog := make([]WorkflowCatalogItem, 500)
	for index := range catalog {
		catalog[index] = WorkflowCatalogItem{WorkflowID: "custom.workflow." + strconv.Itoa(index), Name: "Custom workflow " + strconv.Itoa(index), Version: uint32(index + 1), SemanticVersion: "1.0.0", Status: "ACTIVE"}
	}
	started := time.Now()
	markup, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, catalog, "custom.workflow.250"))
	if err != nil {
		t.Fatalf("render 500 workflow catalog entries: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("500 workflow catalog entries exceeded the one-second page budget: %v", elapsed)
	}
	if got := strings.Count(markup, `class="data-table-row workflow-list-data-row"`); got != workflowListPageSize {
		t.Fatalf("windowed workflow rows = %d, want %d", got, workflowListPageSize)
	}
}

func TestUXBLINDWorkflowCatalogAllCountMatchesReferenceVisibility(t *testing.T) {
	props := WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		BaseHref:  "/workspace/app/admin/workflows",
	}
	catalog := []WorkflowCatalogItem{
		{WorkflowID: "people.promotion", Name: "Promotion", Status: "ACTIVE"},
		{WorkflowID: "people.onboarding", Name: "Onboarding", Status: "ACTIVE"},
		{WorkflowID: "people.exit", Name: "Exit", Status: "ACTIVE"},
		{WorkflowID: "people.pay", Name: "Pay", Status: "ACTIVE"},
		{WorkflowID: "people.task", Name: "Task", Status: "DRAFT"},
		{WorkflowID: "reference.prototype", Name: "Prototype reference", Status: "ACTIVE"},
	}
	props.BaseHref = "/workspace/app/admin/workflows?workflow_references=0"
	hidden, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, catalog, ""))
	if err != nil {
		t.Fatalf("render catalog with references hidden: %v", err)
	}
	if !strings.Contains(hidden, `>All</span><span aria-label="5"`) || !strings.Contains(hidden, "5 of 5 workflows") {
		t.Fatalf("All count and visible catalog total diverged while references were hidden:\n%s", hidden)
	}
	if strings.Contains(hidden, "Prototype reference") {
		t.Fatal("reference workflow was visible while the reference toggle was off")
	}

	props.BaseHref = "/workspace/app/admin/workflows?workflow_references=1"
	visible, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, catalog, ""))
	if err != nil {
		t.Fatalf("render catalog with references visible: %v", err)
	}
	if !strings.Contains(visible, `>All</span><span aria-label="6"`) || !strings.Contains(visible, "6 of 6 workflows") {
		t.Fatalf("All count and visible catalog total diverged while references were shown:\n%s", visible)
	}
	if !strings.Contains(visible, "Prototype reference") {
		t.Fatal("reference workflow did not appear when the reference toggle was on")
	}
}
