package productui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

func renderProjectTest(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestProjectsPageShowsOnlyExplicitAuthorizedRowsAndEscapes(t *testing.T) {
	loading := renderProjectTest(t, projectsPage(View{Title: "Projects"}))
	if !strings.Contains(loading, `role="status"`) || strings.Contains(loading, "Create project") && !strings.Contains(loading, "disabled") {
		t.Fatalf("loading page must remain explicit and creation disabled: %s", loading)
	}
	ready := View{Title: "Projects", ProjectsState: ProjectProjectionReady, Projects: []ProjectSummaryProjection{{ID: "p-1", Name: `<script>alert("x")</script>`, Description: `" onmouseover="alert(1)`, OwnerID: "owner-1", TaskCount: 2, TaskCountKnown: true}}}
	markup := renderProjectTest(t, projectsPage(ready))
	if strings.Contains(markup, `<script>`) || !strings.Contains(markup, "&lt;script&gt;") || !strings.Contains(markup, `href="/workspace/app/project?project=p-1"`) || !strings.Contains(markup, `role="list"`) {
		t.Fatalf("authorized project row not safely rendered: %s", markup)
	}
	if !strings.Contains(markup, `<details class="project-page-disclosure project-page-create-disclosure">`) || strings.Index(markup, `<summary>Create project`) > strings.Index(markup, `class="project-list"`) {
		t.Fatalf("create form should be collapsed before the project list: %s", markup)
	}
	empty := renderProjectTest(t, projectsPage(View{Title: "Projects", ProjectsState: ProjectProjectionReady}))
	if !strings.Contains(empty, "No projects are available") {
		t.Fatalf("ready empty response was not shown: %s", empty)
	}
	restricted := renderProjectTest(t, projectsPage(View{Title: "Projects", ProjectsState: ProjectProjectionRestricted, Projects: []ProjectSummaryProjection{{ID: "hidden", Name: "Secret"}}}))
	if strings.Contains(restricted, "Secret") || !strings.Contains(restricted, "unavailable") {
		t.Fatalf("restricted projection disclosed rows: %s", restricted)
	}
}

func TestProjectBoardProvidesCanonicalSwitchAndHonestUnavailableActions(t *testing.T) {
	view := View{
		Title: "Board", ProjectID: "p-7", ProjectBoardState: ProjectProjectionReady,
		ProjectBoard: &projectui.Model{Title: "Sprint board", Mode: projectui.ViewBoard, ViewID: "view-1", ViewRevision: 4, SwimlaneGrouping: "BOARD_SWIMLANE_GROUPING_NONE", Columns: []projectui.Column{{ID: "todo", Label: "To do", Statuses: []projectui.Status{{ID: "todo", Label: "To do"}}}}, Cards: []projectui.Card{{ID: "t-1", Title: "<Roadmap>", ColumnID: "todo", StatusID: "todo", CanMoveStatus: true, CanMoveLane: true, StatusOptions: []projectui.Status{{ID: "todo", Label: "To do"}}}}},
	}
	markup := renderProjectTest(t, projectPage(view))
	if !strings.Contains(markup, `href="/workspace/app/project?project=p-7&amp;view=list"`) || !strings.Contains(markup, "Switch to list") || !strings.Contains(markup, `data-projectui-action="create-task"`) || !strings.Contains(markup, `name="initial-status-id"`) || !strings.Contains(markup, `data-projectui-action="save-board-view"`) || !strings.Contains(markup, `data-view-id="view-1"`) || !strings.Contains(markup, `name="column-name-0"`) || !strings.Contains(markup, "Board view settings (unavailable)") || !strings.Contains(markup, `disabled`) {
		t.Fatalf("board navigation or honest unavailable actions missing: %s", markup)
	}
	if strings.Contains(markup, `data-projectui-action="move-status"`) || strings.Contains(markup, `data-projectui-action="move-lane"`) {
		t.Fatalf("move controls appeared before the host dispatcher is ready: %s", markup)
	}
	if !strings.Contains(markup, `<summary>Board view settings (unavailable)</summary>`) || !strings.Contains(markup, `<summary>Create task (unavailable)</summary>`) || strings.Contains(markup, `<details open`) {
		t.Fatalf("board action panels should be compact, labeled disclosures: %s", markup)
	}
	heading := strings.Index(markup, `<h1>Sprint board</h1>`)
	columns := strings.Index(markup, `class="projectui-columns"`)
	if heading < 0 || columns < 0 || heading > columns {
		t.Fatalf("board heading must precede its Kanban columns: %s", markup)
	}
	if !strings.Contains(markup, "&lt;Roadmap&gt;") || strings.Contains(markup, "<Roadmap>") {
		t.Fatalf("task title was not escaped: %s", markup)
	}
}

func TestProjectTaskCreateDefaultsToFirstAuthorizedBoardStatus(t *testing.T) {
	view := View{
		Title: "Board", ProjectID: "p-7", ProjectTaskCreateReady: true, ProjectBoardState: ProjectProjectionReady,
		ProjectBoard: &projectui.Model{Mode: projectui.ViewBoard, Columns: []projectui.Column{
			{ID: "todo", Label: "To do", Statuses: []projectui.Status{{ID: "todo", Label: "To do"}}},
			{ID: "doing", Label: "In progress", Statuses: []projectui.Status{{ID: "doing", Label: "In progress"}}},
		}},
	}
	markup := renderProjectTest(t, projectPage(view))
	if !strings.Contains(markup, `<option selected value="todo">To do</option>`) || strings.Contains(markup, `<option value="">Use project default</option>`) {
		t.Fatalf("task creation must start with a valid authorized status selected: %s", markup)
	}
}

func TestProjectCreateFormHasStableDispatcherFieldsAndUnavailableSemantics(t *testing.T) {
	markup := renderProjectTest(t, projectsPage(View{Title: "Projects"}))
	for _, want := range []string{`data-projectui-action="create-project"`, `name="name"`, `name="timezone"`, `value="UTC"`, `required`, `Project creation is not available`, `disabled`, `<summary>Create project (unavailable)</summary>`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("create-project form missing %q: %s", want, markup)
		}
	}
	ready := renderProjectTest(t, projectsPage(View{Title: "Projects", ProjectCreateReady: true}))
	if strings.Contains(ready, `name="name" type="text" required="" disabled=""`) || !strings.Contains(ready, `>Create project</button>`) || strings.Contains(ready, "Create project (unavailable)") {
		t.Fatalf("ready create form is disabled or retains unavailable label: %s", ready)
	}
}

func TestProjectActionDisclosuresHaveLocalizedAccessibleSummaries(t *testing.T) {
	for _, test := range []struct{ locale, create, task, settings string }{
		{"en-US", "Create project (unavailable)", "Create task (unavailable)", "Board view settings (unavailable)"},
		{"de-DE", "Projekt erstellen (nicht verfügbar)", "Aufgabe erstellen (nicht verfügbar)", "Board-Einstellungen (nicht verfügbar)"},
		{"ar", "إنشاء مشروع (غير متاح)", "إنشاء مهمة (غير متاح)", "إعدادات عرض اللوحة (غير متاحة)"},
	} {
		locale := ResolveProductLocale(test.locale)
		home := renderProjectTest(t, projectsPage(View{Locale: locale, Title: "Projects"}))
		board := renderProjectTest(t, projectPage(View{Locale: locale, Title: "Board", ProjectID: "p-1", ProjectBoardState: ProjectProjectionReady, ProjectBoard: &projectui.Model{Mode: projectui.ViewBoard, ViewID: "v-1", ViewRevision: 1}}))
		if !strings.Contains(home, `<summary>`+test.create+`</summary>`) || !strings.Contains(board, `<summary>`+test.task+`</summary>`) || !strings.Contains(board, `<summary>`+test.settings+`</summary>`) {
			t.Fatalf("localized disclosure summary missing for %s: home=%s board=%s", test.locale, home, board)
		}
	}
}

func TestProjectPageIDsAndPermissionNavigation(t *testing.T) {
	for _, test := range []struct {
		id    PageID
		route string
	}{{PageProjects, "/workspace/app/projects"}, {PageProject, "/workspace/app/project"}} {
		definition, ok := LookupPage(test.id)
		if !ok || definition.Route != test.route {
			t.Fatalf("definition for %q = %+v, %v", test.id, definition, ok)
		}
	}
	denied := ApplyPagePermissions(NewView(PageHome, "tenant", "person", "scope"), []RolePagePermission{{Page: PageProjects, View: false}})
	for _, item := range denied.Navigation {
		if item.Page == PageProjects {
			t.Fatal("denied projects page appeared in effective navigation")
		}
	}
	allowed := ApplyPagePermissions(NewView(PageHome, "tenant", "person", "scope"), []RolePagePermission{{Page: PageProjects, View: true}})
	found := false
	for _, item := range allowed.Navigation {
		found = found || item.Page == PageProjects
	}
	if !found {
		t.Fatal("authorized projects page missing from navigation")
	}
}

func TestProjectRoutesRejectUnsafeIDsAndRetainSelectors(t *testing.T) {
	if ProjectBoardHref("../private") != "" || ProjectTaskHref("p1", "javascript:bad") != "" {
		t.Fatal("unsafe selector was accepted")
	}
	got := ProjectViewHref("p-1", "t-2", "view-3", "sign-abc_2", projectui.ViewList)
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Path != "/workspace/app/project" || query.Get("project") != "p-1" || query.Get("task") != "t-2" || query.Get("board_view") != "view-3" || query.Get("cursor") != "sign-abc_2" || query.Get("view") != "list" {
		t.Fatalf("canonical selectors missing: %s", got)
	}
}
