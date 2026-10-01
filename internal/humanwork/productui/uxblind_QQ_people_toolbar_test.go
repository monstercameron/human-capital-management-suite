package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_098(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(ui.CreateElement(PeopleFilter, PeopleFilterProps{
		I18nProps: I18nProps{Locale: locale},
		Teams:     []PeopleFilterOption{{Value: "care", Label: "Care Operations"}},
		Locations: []PeopleFilterOption{{Value: "aurora", Label: "Aurora"}},
		OnFilter:  func(string, string, string, bool) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="people-filter-control"`, `id="people-filter"`, `id="people-team-filter"`, `id="people-location-filter"`, `id="people-eligible-filter"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("live People toolbar missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `type="submit"`) || strings.Contains(markup, `>Filter<`) {
		t.Fatalf("live search retained a redundant Filter action: %s", markup)
	}

	manager := Person{ID: "manager-ref", WorkerID: "manager-canonical", Name: "Luis", PreferredName: "Luis", LegalName: "Luis Ortega"}
	worker := Person{ID: "worker-ref", Name: "Nabil Farouk", Manager: "Luis", ManagerWorkerRef: manager.ID}
	view := testView(PagePeople)
	view.People = []Person{manager, worker}
	rows := peopleRowProps(view, peoplePageWindow{People: []Person{worker}, Page: 1, Total: 1})
	if len(rows) != 1 || rows[0].Manager != "Luis Ortega" {
		t.Fatalf("manager display name = %+v, want Luis Ortega", rows)
	}
	rowMarkup, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{
		I18nProps: I18nProps{Locale: locale}, Rows: rows,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rowMarkup, "Luis Ortega") || strings.Contains(rowMarkup, ">Luis</td>") {
		t.Fatalf("People manager column did not use the full display name: %s", rowMarkup)
	}
}

func TestTodo_UXBLIND_098_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.People = []Person{
		{ID: "manager-ref", WorkerID: "manager-canonical", Name: "Nabil", PreferredName: "Nabil", LegalName: "Nabil Farouk"},
		{ID: "worker-ref", Name: "Luis Ortega", Manager: "Nabil", ManagerWorkerRef: "manager-ref"},
	}
	view.Navigate = func(string) {}
	markup, err := ui.RenderToString(peoplePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="people-filter-control"`, `id="people-filter"`, "Nabil Farouk"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("People browser projection missing %q: %s", want, markup)
		}
	}
	panelStart := strings.Index(markup, `class="people-search-panel"`)
	if panelStart < 0 {
		t.Fatalf("People browser projection has no search panel: %s", markup)
	}
	panelEnd := strings.Index(markup[panelStart:], `</section>`)
	if panelEnd < 0 {
		t.Fatalf("People browser projection has no search panel: %s", markup)
	}
	panel := markup[panelStart : panelStart+panelEnd]
	if strings.Contains(panel, `type="submit"`) {
		t.Fatalf("browser live People projection still renders Filter: %s", markup)
	}
	css := Stylesheet()
	for _, want := range []string{
		"grid-template-columns:minmax(220px,2fr)",
		"@media (max-width:1120px){.people-filter-control",
		"@media (max-width:900px){.people-filter-control",
		".people-page .people-search-panel>.column-chooser>summary",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("People toolbar stylesheet missing %q: %s", want, css)
		}
	}
}
