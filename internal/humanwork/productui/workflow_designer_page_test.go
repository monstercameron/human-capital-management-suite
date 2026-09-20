package productui

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WF_UI_002_PageIsRegisteredAndDiscoverable(t *testing.T) {
	definition, ok := LookupPage(PageWorkflowDesigner)
	if !ok {
		t.Fatal("workflow designer page is not registered")
	}
	if definition.Route != "/workspace/app/admin/workflows" || !definition.Admitted || !definition.NavigationPublished || !definition.PrimaryNav {
		t.Fatalf("workflow designer registration = %+v", definition)
	}
	if !PageVisible(PageWorkflowDesigner, []string{RoleHCMAdmin}) {
		t.Fatal("workflow designer is not visible to the HCM administrator compatibility role")
	}
	if PageVisible(PageWorkflowDesigner, []string{"manager"}) || PageVisible(PageWorkflowDesigner, []string{"worker_self"}) {
		t.Fatal("workflow designer leaked into non-author roles")
	}

	items := navigationForPermissions(ResolveProductLocale("en-US"), []RolePagePermission{{RoleID: "intent_author", Page: PageWorkflowDesigner, View: true}})
	if !navigationContains(items, PageWorkflowDesigner) {
		t.Fatal("workflow author navigation omits Workflow editor")
	}
	designerIndex := navigationPageIndex(items, PageWorkflowDesigner)
	if got := items[designerIndex].Label; got != "Workflow editor" {
		t.Fatalf("workflow editor navigation label = %q", got)
	}
	adminItems := navigationForRoles(ResolveProductLocale("en-US"), []string{RoleHCMAdmin})
	designerIndex, workIndex := navigationPageIndex(adminItems, PageWorkflowDesigner), navigationPageIndex(adminItems, PageWork)
	if designerIndex < 0 || workIndex < 0 || designerIndex >= workIndex {
		t.Fatalf("Workflow editor must remain discoverable beside Journeys before My Work: designer=%d work=%d", designerIndex, workIndex)
	}
}

func navigationPageIndex(items []NavItem, page PageID) int {
	for index, item := range items {
		if item.Page == page {
			return index
		}
	}
	return -1
}

func TestTodo_WF_UI_002_PageRendersCatalogAndSelectedWorkflow(t *testing.T) {
	view := ApplyRoleVisibility(NewView(PageWorkflowDesigner, "HarborCare", "Avery", "author"), []string{"intent_author"})
	projection := workflowViewerFixture()
	view.PublishedWorkflows = []WorkflowCatalogItem{
		{WorkflowID: "z.workflow", Name: "Zeta workflow", Version: 1, SemanticVersion: "2026.1.0", Status: "DRAFT"},
		{WorkflowID: projection.WorkflowID, Name: projection.Name, Version: projection.Version, SemanticVersion: projection.SemanticVersion, Status: projection.PublicationStatus, ActiveRuns: 1},
	}
	view.WorkflowView = &projection
	view.SelectedWorkflowID = projection.WorkflowID

	markup, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatalf("render workflow designer: %v", err)
	}
	for _, want := range []string{
		`aria-labelledby="workflow-designer-heading"`,
		`Published workflows`,
		`aria-current="page"`,
		`workflow=promotion.approval`,
		`id="workflow-designer-viewer-title"`,
		`Promotion approval`,
		`Workflow outline`,
		`Published · read-only`,
		`Create newer version`,
		`name="semantic_version"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow designer missing %q:\n%s", want, markup)
		}
	}
	if strings.Index(markup, "Promotion approval") > strings.Index(markup, "Zeta workflow") {
		t.Fatal("workflow catalog is not sorted by display name")
	}
}

func TestTodo_WF_UI_002_PageEmptyStateIsHonest(t *testing.T) {
	view := ApplyRoleVisibility(NewView(PageWorkflowDesigner, "HarborCare", "Avery", "author"), []string{"intent_author"})
	if view.Subtitle == ordinaryUnavailableCopy("en-US") || !strings.Contains(view.Subtitle, "workflow") {
		t.Fatalf("workflow designer subtitle was replaced by generic unavailable copy: %q", view.Subtitle)
	}
	markup, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatalf("render empty workflow designer: %v", err)
	}
	for _, want := range []string{"No published workflows available", "Choose a workflow"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("empty workflow designer missing %q", want)
		}
	}
	if strings.Contains(markup, `id="workflow-designer-viewer-title"`) {
		t.Fatal("empty workflow designer invented a selected workflow")
	}
}

func TestTodo_WF_UI_002_PageRouteState(t *testing.T) {
	profile, _, ok := PageProfiles(PageWorkflowDesigner)
	if !ok || profile != RouteProfileWorkflow {
		t.Fatalf("workflow designer route profile = %q, ok=%t", profile, ok)
	}
	wantKeys := []string{"draft", "favorites", "locale", "menu_q", "nav", "node", "run", "workflow"}
	gotKeys := append([]string(nil), profile.QueryKeys()...)
	slices.Sort(gotKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("workflow designer query keys = %v, want %v", gotKeys, wantKeys)
	}

	request := PageRequest{Page: PageWorkflowDesigner, WorkflowID: "promotion.approval", WorkflowRunID: "run-1042", WorkflowDraftID: "draft-42", WorkflowNodeID: "await_payroll_confirmation"}
	values := profile.CanonicalValues(request, map[string]bool{"workflow": true, "run": true, "draft": true, "node": true})
	if values.Get("workflow") != request.WorkflowID || values.Get("run") != request.WorkflowRunID || values.Get("draft") != request.WorkflowDraftID || values.Get("node") != request.WorkflowNodeID {
		t.Fatalf("canonical workflow route = %v", values)
	}
	view := ApplyRequest(NewView(PageWorkflowDesigner, "", "", ""), request)
	address := url.Values{}
	profile.AddressValues(address, view)
	if address.Get("workflow") != request.WorkflowID || address.Get("run") != request.WorkflowRunID || address.Get("draft") != request.WorkflowDraftID || address.Get("node") != request.WorkflowNodeID {
		t.Fatalf("workflow address state = %v", address)
	}
	if !profile.IdentityMatches(view, request) || profile.IdentityMatches(view, PageRequest{WorkflowDraftID: "other"}) {
		t.Fatal("workflow draft route identity does not fence selected drafts")
	}
}

func TestTodo_WF_UI_005_WorkflowRouteIdentityUsesTheRenderedSubject(t *testing.T) {
	profile, _, ok := PageProfiles(PageWorkflowDesigner)
	if !ok {
		t.Fatal("workflow designer route profile is not registered")
	}

	tests := []struct {
		name    string
		view    View
		request PageRequest
		want    bool
	}{
		{
			name:    "same draft ignores its derived publication",
			view:    View{Page: PageWorkflowDesigner, SelectedWorkflowID: "workflow.resolved", SelectedWorkflowDraftID: "draft-42"},
			request: PageRequest{Page: PageWorkflowDesigner, WorkflowDraftID: "draft-42"},
			want:    true,
		},
		{
			name:    "different draft is fenced",
			view:    View{Page: PageWorkflowDesigner, SelectedWorkflowID: "workflow.resolved", SelectedWorkflowDraftID: "draft-42"},
			request: PageRequest{Page: PageWorkflowDesigner, WorkflowDraftID: "draft-43"},
		},
		{
			name:    "same live run ignores its derived publication",
			view:    View{Page: PageWorkflowDesigner, SelectedWorkflowID: "workflow.resolved", SelectedWorkflowRunID: "run-7"},
			request: PageRequest{Page: PageWorkflowDesigner, WorkflowRunID: "run-7"},
			want:    true,
		},
		{
			name:    "run cannot retain a draft",
			view:    View{Page: PageWorkflowDesigner, SelectedWorkflowDraftID: "draft-42", SelectedWorkflowRunID: "run-7"},
			request: PageRequest{Page: PageWorkflowDesigner, WorkflowRunID: "run-7"},
		},
		{
			name:    "same explicit publication",
			view:    View{Page: PageWorkflowDesigner, SelectedWorkflowID: "workflow.resolved"},
			request: PageRequest{Page: PageWorkflowDesigner, WorkflowID: "workflow.resolved"},
			want:    true,
		},
		{
			name:    "publication cannot retain a live run",
			view:    View{Page: PageWorkflowDesigner, SelectedWorkflowID: "workflow.resolved", SelectedWorkflowRunID: "run-7"},
			request: PageRequest{Page: PageWorkflowDesigner, WorkflowID: "workflow.resolved"},
		},
		{
			name:    "default publication accepts its server-resolved identity",
			view:    View{Page: PageWorkflowDesigner, SelectedWorkflowID: "workflow.default"},
			request: PageRequest{Page: PageWorkflowDesigner},
			want:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := profile.IdentityMatches(tc.view, tc.request); got != tc.want {
				t.Fatalf("IdentityMatches() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestTodo_WF_UI_005_DesignerRendersDurableCollapsibleFragment(t *testing.T) {
	view := ApplyRoleVisibility(NewView(PageWorkflowDesigner, "HarborCare", "Avery", "author"), []string{"intent_author"})
	view.WorkflowPalette = workflowPaletteFixture()
	view.WorkflowDraft = &WorkflowDraftView{
		DraftID: "draft-42", WorkflowID: "customer.workflow.42", Name: "Employee change", SemanticVersion: "0.2.0", Revision: 7,
		Nodes:  []WorkflowDraftNode{{ID: "group_review_1__manager", StepType: "APPROVAL", GroupID: "group_review_1"}, {ID: "group_review_1__finance", StepType: "APPROVAL", GroupID: "group_review_1"}},
		Groups: []WorkflowDraftGroup{{ID: "group_review_1", Name: "Promotion review", EntryID: "hcmnext.fragments.promotion_review", EntryVersion: 1, Collapsed: true, NodeIDs: []string{"group_review_1__manager", "group_review_1__finance"}}},
	}
	view.CreateWorkflowDraft = func(WorkflowDraftCreateRequest) {}
	view.InsertWorkflowPaletteEntry = func(WorkflowPaletteItem) {}
	markup, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`Draft · version 0.2.0`, `Employee change`, `Saved · revision 7`, `data-group-id="group_review_1"`, `<details`, `Promotion review`, `group_review_1__manager`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("draft workspace missing %q:\n%s", want, markup)
		}
	}
	assertWorkflowDraftGroupOpenState(t, markup, "group_review_1", false)

	view.WorkflowDraft.Groups[0].Collapsed = false
	expanded, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatal(err)
	}
	assertWorkflowDraftGroupOpenState(t, expanded, "group_review_1", true)
}

func assertWorkflowDraftGroupOpenState(t *testing.T, markup, groupID string, wantOpen bool) {
	t.Helper()
	needle := `data-group-id="` + groupID + `"`
	groupAt := strings.Index(markup, needle)
	if groupAt < 0 {
		t.Fatalf("draft workspace missing group %q", groupID)
	}
	startAt := strings.LastIndex(markup[:groupAt], "<details")
	endOffset := strings.Index(markup[groupAt:], ">")
	if startAt < 0 || endOffset < 0 {
		t.Fatalf("group %q is not rendered by a details element", groupID)
	}
	startTag := markup[startAt : groupAt+endOffset+1]
	gotOpen := strings.Contains(startTag, " open")
	if gotOpen != wantOpen {
		t.Fatalf("group %q open state = %t, want %t: %s", groupID, gotOpen, wantOpen, startTag)
	}
}

func TestTodo_WF_UI_002_PageResponsiveThemeAndLocales(t *testing.T) {
	css := workflowDesignerStylesheet()
	for _, want := range []string{
		`grid-template-columns:minmax(15rem,19rem) minmax(0,1fr) minmax(15rem,18rem)`,
		`@media (max-width:900px)`,
		`@media (max-width:640px)`,
		`var(--hcm-radius-control)`,
		`var(--hcm-motion-fast)`,
		`var(--soft)`,
		`.workflow-designer-header .button-icon{inline-size:1rem;block-size:1rem;flex:none}`,
		`.workflow-designer-header .button{align-self:flex-start}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("workflow designer stylesheet missing %q", want)
		}
	}
	for _, forbidden := range []string{"margin-left", "padding-left", "border-left", "#", "rgb(", "hsl("} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("workflow designer stylesheet contains non-logical or fixed-color declaration %q", forbidden)
		}
	}

	for _, test := range []struct{ locale, want string }{
		{"de-DE", "Workflows entwerfen und prüfen"},
		{"ar", "تصميم مسارات العمل وفحصها"},
	} {
		view := ApplyLocale(NewView(PageWorkflowDesigner, "", "", ""), ResolveProductLocale(test.locale))
		markup, err := ui.RenderToString(BuildPageContent(view))
		if err != nil {
			t.Fatalf("render %s workflow designer: %v", test.locale, err)
		}
		if !strings.Contains(markup, test.want) || strings.Contains(markup, "⟦workflow_designer.") {
			t.Fatalf("%s workflow designer localization is incomplete:\n%s", test.locale, markup)
		}
	}
}

func TestTodo_WF_UI_005_DesignerComposesPaletteBesideGraph(t *testing.T) {
	view := ApplyRoleVisibility(NewView(PageWorkflowDesigner, "HarborCare", "Avery", "author"), []string{"intent_author"})
	projection := workflowViewerFixture()
	view.WorkflowView = &projection
	view.WorkflowPalette = workflowPaletteFixture()

	markup, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatalf("render workflow designer with palette: %v", err)
	}
	for _, want := range []string{"Published workflows", "Workflow map", "Block library", "Promotion review", `data-insert-mode="group"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow designer composition missing %q:\n%s", want, markup)
		}
	}
	if strings.Index(markup, "Block library") < strings.Index(markup, "Workflow map") {
		t.Fatal("palette precedes the primary workflow workspace in reading order")
	}
}

func TestTodo_WF_UI_002_PageDoesNotMutateCatalog(t *testing.T) {
	catalog := []WorkflowCatalogItem{
		{WorkflowID: "z", Name: "Zeta"},
		{WorkflowID: "a", Name: "Alpha"},
	}
	before := append([]WorkflowCatalogItem(nil), catalog...)
	_, err := ui.RenderToString(WorkflowDesignerPage(WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Catalog: catalog, BaseHref: Path(PageWorkflowDesigner),
	}))
	if err != nil {
		t.Fatalf("render workflow designer: %v", err)
	}
	if !slices.Equal(catalog, before) {
		t.Fatalf("renderer mutated caller catalog: got %+v want %+v", catalog, before)
	}
}
