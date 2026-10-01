package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	xhtml "golang.org/x/net/html"
)

func TestTodo_UXBLIND_109(t *testing.T) {
	markup, err := ui.RenderToString(WorkflowDesignerPage(WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Catalog: []WorkflowCatalogItem{
			{WorkflowID: "promotion.approval", Name: "Promotion approval", SemanticVersion: "2.0.0", Status: "ACTIVE"},
			{WorkflowID: "ironridge.work", DraftID: "draft-1", Name: "Ironridge field work order", SemanticVersion: "0.2.0", Status: "DRAFT"},
			{WorkflowID: "promotion.prototype", Name: "Prototype promotion approval", SemanticVersion: "1.0.0", Status: "ACTIVE"},
		},
		BaseHref: Path(PageWorkflowDesigner),
	}))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="workflow-list-published-heading">`,
		`id="workflow-list-drafts-heading">`,
		`New promotions run on Promotion approval`,
		`Show reference workflows`,
		`Ironridge field work order`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow list missing %q:\n%s", want, markup)
		}
	}
	for _, tc := range []struct {
		id, label, count string
	}{
		{"workflow-list-published-heading", "Published workflows", "1"},
		{"workflow-list-drafts-heading", "Draft workflows", "1"},
	} {
		heading := findElementByID(root, tc.id)
		if heading == nil || !strings.Contains(nodeText(heading), tc.label) {
			t.Fatalf("workflow group heading %q missing label %q", tc.id, tc.label)
		}
		badge := findClassToken(heading, "count-badge")
		if badge == nil || attr(badge, "aria-label") != tc.count || strings.TrimSpace(nodeText(badge)) != tc.count {
			t.Fatalf("workflow group heading %q has no accessible count %q", tc.id, tc.count)
		}
	}
	if strings.Contains(markup, "Ironridge field work order — Draft") {
		t.Fatal("draft title repeats its status suffix")
	}
	if strings.Contains(markup, "Prototype promotion approval") {
		t.Fatal("reference workflow is visible before the disclosure is enabled")
	}
}

func TestTodo_UXBLIND_109_Browser(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		markup, err := ui.RenderToString(WorkflowDesignerPage(WorkflowDesignerPageProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale(locale)},
			Catalog:   []WorkflowCatalogItem{{WorkflowID: "active", Name: "Promotion approval", Status: "ACTIVE"}, {WorkflowID: "reference", Name: "Prototype promotion approval", Status: "ACTIVE"}},
			BaseHref:  Path(PageWorkflowDesigner),
		}))
		if err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		if strings.Contains(markup, "⟦workflow_designer.") || !strings.Contains(markup, "workflow-list-references") {
			t.Fatalf("%s workflow list localization or collapsed reference state failed: %s", locale, markup)
		}
	}
}

func TestTodo_UXBLIND_110(t *testing.T) {
	chat := ApplyLocale(NewView(PageChat, "tenant", "person", "scope"), ResolveProductLocale("en-US"))
	chat.Chat = chatui.Model{State: chatui.StateError}
	chatMarkup := renderVV110(t, BuildPageContent(chat))
	for _, want := range []string{`id="page-title"`, ">Chat</h1>", "Talk with coworkers in authorized channels", "Not set up for this workspace", "Ask your workspace administrator"} {
		if !strings.Contains(chatMarkup, want) {
			t.Fatalf("chat unavailable frame missing %q: %s", want, chatMarkup)
		}
	}
	if strings.Contains(chatMarkup, "Open Admin") {
		t.Fatal("ordinary viewer received an administrator action")
	}

	adminChat := ApplyRoleVisibility(chat, []string{RoleHCMAdmin})
	adminChatMarkup := renderVV110(t, BuildPageContent(adminChat))
	if !strings.Contains(adminChatMarkup, `href="/workspace/app/admin`) || !strings.Contains(adminChatMarkup, "Open Admin") {
		t.Fatalf("administrator unavailable frame lacks Admin guidance: %s", adminChatMarkup)
	}

	docs := ApplyLocale(NewView(PageDocs, "tenant", "person", "scope"), ResolveProductLocale("en-US"))
	docsMarkup := renderVV110(t, BuildPageContent(docs))
	if !strings.Contains(docsMarkup, `id="page-title"`) || !strings.Contains(docsMarkup, ">Documents</h1>") || !strings.Contains(docsMarkup, "Browse documents you are authorized to read") {
		t.Fatalf("documents unavailable frame lost page identity: %s", docsMarkup)
	}

	projects := ApplyLocale(NewView(PageProjects, "tenant", "person", "scope"), ResolveProductLocale("en-US"))
	projects.ProjectsState = ProjectProjectionUnavailable
	projectsMarkup := renderVV110(t, BuildPageContent(projects))
	if !strings.Contains(projectsMarkup, `id="page-title"`) || !strings.Contains(projectsMarkup, ">Projects</h1>") || !strings.Contains(projectsMarkup, projects.Subtitle) {
		t.Fatalf("projects unavailable frame lost page identity: %s", projectsMarkup)
	}
	if strings.Contains(projectsMarkup, "project-page-tabs") {
		t.Fatal("projects unavailable state still renders empty tabs")
	}
}

func TestTodo_UXBLIND_110_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(NewView(PageProjects, "tenant", "person", "scope"), ResolveProductLocale(locale))
		view.ProjectsState = ProjectProjectionUnavailable
		markup := renderVV110(t, BuildPageContent(view))
		if strings.Contains(markup, "⟦") || strings.Contains(markup, "project-page-tabs") || !strings.Contains(markup, `id="page-title"`) {
			t.Fatalf("%s unavailable page has unresolved copy, tabs, or no heading: %s", locale, markup)
		}
	}
}

func renderVV110(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
