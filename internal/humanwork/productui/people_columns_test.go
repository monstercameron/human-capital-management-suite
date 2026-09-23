package productui

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UICOLS_001(t *testing.T) {
	if got := NormalizePeopleColumns("company,base_pay,company,job_code"); got != "name,job_code,company" {
		t.Fatalf("unsafe/duplicate columns admitted: %q", got)
	}
	if got := NormalizePeopleColumns(""); got != "name,role,team,manager,location" {
		t.Fatalf("defaults = %q", got)
	}
	people := []Person{{ID: "b", Name: "B", JobCode: "Z2", Company: "Zephyr", WorkerNumber: "W2", Grade: "P4", BusinessUnit: "Zulu", CostCenter: "CC2"}, {ID: "a", Name: "A", JobCode: "A1", Company: "Acme", WorkerNumber: "W1", Grade: "P2", BusinessUnit: "Alpha", CostCenter: "CC1"}, {ID: "c", Name: "C"}}
	IndexPeople(people)
	for _, field := range []string{"worker_number", "job_code", "grade", "company", "business_unit", "cost_center"} {
		for _, direction := range []string{"asc", "desc"} {
			got := sortedPeople(people, field, direction)
			want := []string{"a", "b", "c"}
			if direction == "desc" {
				want = []string{"b", "a", "c"}
			}
			if !slices.Equal([]string{got[0].ID, got[1].ID, got[2].ID}, want) {
				t.Fatalf("%s %s: %+v", field, direction, got)
			}
		}
	}
	hits := SearchPeopleDirectory(people, BuildPeopleQuery("zephyr", "", "", "", "", 1, 20))
	if len(hits) != 1 || hits[0].ID != "b" {
		t.Fatalf("company not searchable: %+v", hits)
	}
}

func TestTodo_UICOLS_001_Components(t *testing.T) {
	view := View{Page: PagePeople, Locale: ResolveProductLocale("en-US"), PeopleColumns: "name,job_code,company", PeopleSort: "company", PeopleDirection: "desc"}
	var target string
	view.Navigate = func(href string) { target = href }
	view.Query, view.PeopleTeam, view.PeopleLocation, view.PeoplePageSize = "Acme", "Finance", "Boston", 50
	chooser := peopleColumnChooserProps(view)
	chooser.Apply("name,job_code")
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("columns") != "name,job_code" || parsed.Query().Get("sort") != "name" || parsed.Query().Get("dir") != "asc" {
		t.Fatalf("hidden sort not reset: %s", target)
	}
	if parsed.Query().Get("q") != "Acme" || parsed.Query().Get("team") != "Finance" || parsed.Query().Get("location") != "Boston" || parsed.Query().Get("page_size") != "50" {
		t.Fatalf("customizing columns lost filters: %s", target)
	}
	for _, href := range []string{peopleDirectoryHref(view, 2, view.Query, view.PeopleTeam, view.PeopleLocation, false, "company", "desc"), peoplePersonHref(view, "worker", 2)} {
		parsed, err := url.Parse(href)
		if err != nil || parsed.Query().Get("columns") != "name,job_code,company" {
			t.Fatalf("column choice dropped from navigation: %s (%v)", href, err)
		}
	}
	chooser.Reset()
	if !strings.Contains(target, "columns=name%2Crole%2Cteam%2Cmanager%2Clocation") {
		t.Fatalf("reset did not explicitly override saved choices: %s", target)
	}
	if toggleColumnChoice("name,company", "company") != "name" || toggleColumnChoice("name", "company") != "name,company" {
		t.Fatal("toggle failed")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view.Locale = ResolveProductLocale(locale)
		markup, err := ui.RenderToString(ui.CreateElement(ColumnChooser, peopleColumnChooserProps(view)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, `id="people-columns-name"`) || !strings.Contains(markup, "disabled") || !strings.Contains(markup, view.Locale.Text("table.columns_apply")) || strings.Contains(markup, "table.columns_help") {
			t.Fatalf("inaccessible or untranslated chooser: %s", markup)
		}
	}
	markup, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{I18nProps: I18nProps{Locale: view.Locale}, Columns: peopleSortColumns(view), Rows: []PeopleRowProps{{ID: "a", Name: "A", Team: "HIDDEN-TEAM", ExtraValues: [6]string{"W1", "ENG2", "L2", "Acme"}}}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "ENG2") || !strings.Contains(markup, "Acme") || strings.Contains(markup, "HIDDEN-TEAM") {
		t.Fatalf("selected columns not projected: %s", markup)
	}
	if !strings.Contains(Stylesheet(), ".column-choices") {
		t.Fatal("chooser styles absent")
	}
	if !strings.Contains(columnChooserStylesheet(), "background:var(--surface)") {
		t.Fatal("chooser must use resolved light/dark surface")
	}
	first := PeopleDirectoryProps{Rows: []PeopleRowProps{{ID: "a"}}}
	before := peopleDirectoryInputKey(first)
	first.Rows[0].ExtraValues[1] = "CHANGED"
	if before == peopleDirectoryInputKey(first) {
		t.Fatal("cached table would retain stale column data")
	}
}

func BenchmarkPeopleConfiguredSort(b *testing.B) {
	people := make([]Person, 10000)
	for i := range people {
		people[i] = Person{ID: fmt.Sprint(i), Name: fmt.Sprintf("Person %05d", i), Company: fmt.Sprintf("Company %03d", i%100)}
	}
	IndexPeople(people)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sortedPeople(people, "company", "asc")
	}
}

func TestColumnChooserDraftSurvivesLiveDataRemount(t *testing.T) {
	draft := &ColumnChooserDraft{}
	draft.save(columnChooserState{Input: "name", Selected: "name,company", Open: true})
	props := ColumnChooserProps{ID: "test-columns", Selected: "name", Draft: draft, Options: []ColumnChoice{{ID: "company", Label: "Company"}}, Apply: func(string) {}}
	for range 2 {
		markup, err := ui.RenderToString(ui.CreateElement(ColumnChooser, props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, "open") || !strings.Contains(markup, "checked") {
			t.Fatalf("live data remount discarded open draft: %s", markup)
		}
	}
	props.Selected = "name,job_code"
	if _, err := ui.RenderToString(ui.CreateElement(ColumnChooser, props)); err != nil {
		t.Fatal(err)
	}
	if got := draft.load(); got.Open || got.Selected != props.Selected {
		t.Fatalf("new saved selection did not supersede draft: %+v", got)
	}
}

func TestPeopleColumnsBelongToSearchPanel(t *testing.T) {
	view := testView(PagePeople)
	markup, err := ui.RenderToString(peoplePage(view))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(markup, `class="people-search-panel"`)
	if start < 0 {
		t.Fatal("search and columns need a single shared panel")
	}
	end := strings.Index(markup[start:], "</section>")
	if end < 0 {
		t.Fatal("missing panel boundary")
	}
	panel := markup[start : start+end]
	if !strings.Contains(panel, `class="people-filter"`) || !strings.Contains(panel, `class="column-chooser"`) {
		t.Fatal("column chooser is outside the employee search section")
	}
	if strings.Count(markup, `class="column-chooser"`) != 1 {
		t.Fatal("duplicate chooser")
	}
}
