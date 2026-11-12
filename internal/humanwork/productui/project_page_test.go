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
	if !strings.Contains(markup, `href="/workspace/app/project?nav=expanded&amp;project=p-7&amp;view=list"`) || !strings.Contains(markup, "Switch to list") || !strings.Contains(markup, `data-projectui-action="create-task"`) || !strings.Contains(markup, `name="initial-status-id"`) || !strings.Contains(markup, `data-projectui-action="save-board-view"`) || !strings.Contains(markup, `data-view-id="view-1"`) || !strings.Contains(markup, `name="column-name-0"`) || !strings.Contains(markup, "Board view settings (unavailable)") || !strings.Contains(markup, `disabled`) {
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

// TestTodo_GH3424_ViewSwitchKeepsExplicitLocale proves a board/list switch
// keeps the explicit locale (German and Arabic RTL) and the project selector.
func TestTodo_GH3424_ViewSwitchKeepsExplicitLocale(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"de-DE", "de-DE"}, {"ar", "ar"}, {"en-US", "en-US"}} {
		view := ApplyLocale(View{Title: "Board", ProjectID: "p-7", ProjectBoardState: ProjectProjectionReady,
			ProjectBoard: &projectui.Model{Title: "Sprint board", Mode: projectui.ViewBoard, ViewID: "view-1", ViewRevision: 1}}, ResolveProductLocale(tc.locale))
		markup := renderProjectTest(t, projectPage(view))
		for _, mode := range []string{"board", "list"} {
			href := segmentHrefForTest(t, markup, mode)
			query := hrefQueryForTest(t, href)
			if query.Get("project") != "p-7" || query.Get("view") != map[string]string{"board": "", "list": "list"}[mode] {
				t.Fatalf("%s switch lost project or view selector: %s", tc.locale, href)
			}
			if query.Get("locale") != tc.want {
				t.Fatalf("%s switch to %s dropped explicit locale: got %q in %s", tc.locale, mode, query.Get("locale"), href)
			}
		}
	}
}

// TestTodo_GH3426_RestrictedBoardIsNotReportedAsUnconfigured proves an
// inaccessible private board gets a neutral message, not the capability
// setup advice, while a genuinely unavailable board keeps that panel.
func TestTodo_GH3426_RestrictedBoardIsNotReportedAsUnconfigured(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	restricted := renderProjectTest(t, projectPage(ApplyLocale(View{Title: "Board", ProjectID: "p-9", ProjectBoardState: ProjectProjectionRestricted}, locale)))
	if strings.Contains(restricted, "capability-unavailable") || strings.Contains(restricted, "Not set up for this workspace") {
		t.Fatalf("restricted board reported as an unconfigured capability: %s", restricted)
	}
	if !strings.Contains(restricted, `class="project-page-notice"`) || !strings.Contains(restricted, `href="/workspace/app/projects"`) {
		t.Fatalf("restricted board lacks a neutral notice and return link: %s", restricted)
	}
	unavailable := renderProjectTest(t, projectPage(ApplyLocale(View{Title: "Board", ProjectID: "p-9", ProjectBoardState: ProjectProjectionUnavailable}, locale)))
	if !strings.Contains(unavailable, "capability-unavailable") {
		t.Fatalf("unavailable board lost its capability panel: %s", unavailable)
	}
}

// TestTodo_GH3423_BoardToolbarWrapsLabelsAtNarrowWidths pins the 40rem
// (320px-class) toolbar rules: each board action is a full-row flex item and
// its summary text may wrap instead of overflowing its control.
func TestTodo_GH3423_BoardToolbarWrapsLabelsAtNarrowWidths(t *testing.T) {
	narrow := strings.Index(projectPageStyles, "@media (max-width:40rem){")
	if narrow < 0 {
		t.Fatal("project page has no narrow-width block")
	}
	block := projectPageStyles[narrow:]
	for _, rule := range []string{
		`.project-page-board .projectui-actions>.project-page-disclosure:is(.project-page-settings-disclosure,.project-page-create-disclosure){flex:1 1 100%`,
		`.project-page-board .projectui-actions .project-page-disclosure>summary{white-space:normal;max-inline-size:100%`,
		`.project-page-board .projectui-actions .project-page-segment{white-space:normal`,
	} {
		if !strings.Contains(block, rule) {
			t.Fatalf("narrow toolbar rule missing: %s", rule)
		}
	}
}

func segmentHrefForTest(t *testing.T, markup, mode string) string {
	t.Helper()
	marker := `class="project-page-segment" data-view="` + mode + `"`
	start := strings.Index(markup, marker)
	if start < 0 {
		t.Fatalf("no %s view segment: %s", mode, markup)
	}
	open := strings.LastIndex(markup[:start], "<a ")
	end := strings.Index(markup[start:], ">")
	if open < 0 || end < 0 {
		t.Fatalf("%s view segment is not a link: %s", mode, markup)
	}
	tag := markup[open : start+end]
	hrefAt := strings.Index(tag, `href="`)
	if hrefAt < 0 {
		t.Fatalf("%s segment has no href: %s", mode, tag)
	}
	rest := tag[hrefAt+len(`href="`):]
	return strings.ReplaceAll(rest[:strings.Index(rest, `"`)], "&amp;", "&")
}

func hrefQueryForTest(t *testing.T, href string) url.Values {
	t.Helper()
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query()
}

// TestTodo_3612_MyTicketsFilteredCopyIsLocalized proves the filtered-empty
// My tickets copy is wired and localized, not the English fallback.
func TestTodo_3612_MyTicketsFilteredCopyIsLocalized(t *testing.T) {
	english := projectBoardCopy(ResolveProductLocale("en-US")).Board.MyTicketsFiltered
	if english != "No assigned tickets match these filters." {
		t.Fatalf("en-US filtered-empty copy = %q", english)
	}
	for locale, want := range map[string]string{
		"de-DE": "Keine Ihnen zugewiesenen Tickets passen zu diesen Filtern.",
		"ar":    "لا توجد تذاكر مسندة إليك تطابق عوامل التصفية هذه.",
	} {
		got := projectBoardCopy(ResolveProductLocale(locale)).Board.MyTicketsFiltered
		if got != want || got == english {
			t.Fatalf("%s filtered-empty copy = %q, want localized %q", locale, got, want)
		}
	}
}

// TestTodo_GH3426_RestrictedHomeIsNotReportedAsUnconfigured gives a restricted
// Projects home the neutral notice, while an unavailable home keeps the panel.
func TestTodo_GH3426_RestrictedHomeIsNotReportedAsUnconfigured(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	restricted := renderProjectTest(t, projectsPage(ApplyLocale(View{Title: "Projects", ProjectsState: ProjectProjectionRestricted}, locale)))
	if strings.Contains(restricted, "capability-unavailable") || strings.Contains(restricted, "Not set up for this workspace") {
		t.Fatalf("restricted home reported as an unconfigured capability: %s", restricted)
	}
	if !strings.Contains(restricted, `class="project-page-notice"`) {
		t.Fatalf("restricted home lacks the neutral notice: %s", restricted)
	}
	unavailable := renderProjectTest(t, projectsPage(ApplyLocale(View{Title: "Projects", ProjectsState: ProjectProjectionUnavailable}, locale)))
	if !strings.Contains(unavailable, "capability-unavailable") {
		t.Fatalf("unavailable home lost its capability panel: %s", unavailable)
	}
}
