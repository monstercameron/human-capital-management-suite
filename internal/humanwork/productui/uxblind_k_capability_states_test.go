package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

func renderUXBlindK(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_UXBLIND_029(t *testing.T) {
	chat := NewView(PageChat, "tenant", "person", "scope")
	chat.Chat = chatui.Model{State: chatui.StateError, Error: "service unavailable"}
	chatMarkup := renderUXBlindK(t, BuildPageContent(chat))
	if !strings.Contains(chatMarkup, "Not set up for this workspace") || !strings.Contains(chatMarkup, "An administrator can enable this capability") || strings.Contains(chatMarkup, "Browse channels") || strings.Contains(chatMarkup, "New section") || strings.Contains(chatMarkup, "Try again") {
		t.Fatalf("chat unavailable state still exposes recovery or creation controls: %s", chatMarkup)
	}

	docs := NewView(PageDocs, "tenant", "person", "scope")
	docsMarkup := renderUXBlindK(t, BuildPageContent(docs))
	if !strings.Contains(docsMarkup, "Not set up for this workspace") || strings.Contains(docsMarkup, "Documents are not connected to this workspace yet.") {
		t.Fatalf("docs did not use the shared unavailable state: %s", docsMarkup)
	}

	projects := NewView(PageProjects, "tenant", "person", "scope")
	projects.ProjectsState = ProjectProjectionUnavailable
	projects.ProjectCreateReady = true
	projectsMarkup := renderUXBlindK(t, BuildPageContent(projects))
	if !strings.Contains(projectsMarkup, "Not set up for this workspace") || strings.Contains(projectsMarkup, "New project") || strings.Contains(projectsMarkup, "Create project") || strings.Contains(projectsMarkup, "Try again") {
		t.Fatalf("projects unavailable state still exposes create or recovery controls: %s", projectsMarkup)
	}

	settings := NewView(PageChatSettings, "tenant", "person", "scope")
	settings.ChatRetentionError = "Chat retention settings could not be loaded."
	settingsMarkup := renderUXBlindK(t, BuildPageContent(settings))
	if !strings.Contains(settingsMarkup, "Not set up for this workspace") || strings.Contains(settingsMarkup, "No policy is configured") || strings.Contains(settingsMarkup, "Save policy") {
		t.Fatalf("chat settings unavailable state still exposes policy controls: %s", settingsMarkup)
	}
}

func TestTodo_UXBLIND_029_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(NewView(PageDocs, "tenant", "person", "scope"), ResolveProductLocale(locale))
		markup := renderUXBlindK(t, BuildPageContent(view))
		if strings.Contains(markup, "shell.") || strings.Contains(markup, "Try again") {
			t.Fatalf("%s unavailable component leaked a retry or unresolved catalog key: %s", locale, markup)
		}
	}
}

func TestTodo_UXBLIND_029_Golden(t *testing.T) {
	markup := renderUXBlindK(t, capabilityUnavailablePanel(ResolveProductLocale("en-US")))
	want := `<section aria-atomic="true" class="surface empty-state capability-unavailable" role="status"><h2>Not set up for this workspace</h2><p class="muted">An administrator can enable this capability for the workspace.</p></section>`
	if markup != want {
		t.Fatalf("capability unavailable golden changed:\n got %s\nwant %s", markup, want)
	}
}

func TestTodo_UXBLIND_030(t *testing.T) {
	view := View{Title: "Projects", ProjectsState: ProjectProjectionReady, ProjectBoard: &projectui.Model{HomeTab: "tickets", Tickets: &projectui.TicketList{}}}
	markup := renderUXBlindK(t, projectsPage(view))
	if !strings.Contains(markup, `id="project-tab-tickets"`) || !strings.Contains(markup, `aria-selected="true"`) || !strings.Contains(markup, `href="/workspace/app/projects?tab=tickets"`) || !strings.Contains(markup, `id="project-tabpanel-tickets"`) || strings.Contains(markup, `id="project-list"`) {
		t.Fatalf("tickets route did not select the tickets panel: %s", markup)
	}
	if got := projectDefaultTimeZone("America/New_York"); got != "America/New_York" {
		t.Fatalf("viewer time zone default = %q, want America/New_York", got)
	}
	if got := projectDefaultTimeZone("not/a-zone"); got != "UTC" {
		t.Fatalf("unknown time zone default = %q, want UTC", got)
	}
}

func TestTodo_UXBLIND_030_Browser(t *testing.T) {
	view := View{Title: "Projects", Locale: ResolveProductLocale("Europe/Berlin"), ProjectCreateReady: true}
	view.Locale.TimeZone = "Europe/Berlin"
	markup := renderUXBlindK(t, projectsPage(view))
	if !strings.Contains(markup, `<option selected value="Europe/Berlin">Europe/Berlin</option>`) || strings.Contains(markup, `<option selected value="UTC">UTC</option>`) {
		t.Fatalf("project create form ignored the viewer time zone: %s", markup)
	}
}
