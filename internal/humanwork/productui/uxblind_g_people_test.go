package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_021(t *testing.T) {
	view := testView(PagePeople)
	view.PeopleEligibleOnly = true
	if !peoplePromotionMode(view) {
		t.Fatal("eligible directory did not retain promotion mode")
	}
	label := peopleCountLabel(view.Locale, true, 56, 60, true)
	if label != "56 eligible of 60 you can see" {
		t.Fatalf("promotion count = %q", label)
	}
	markup, err := ui.RenderToString(ui.CreateElement(PeopleSummary, PeopleSummaryProps{
		Heading: view.Locale.Text("people.promotion_title"), Description: view.Locale.Text("people.promotion_detail"), CountLabel: label, ScopeLabel: view.Locale.Text("people.scope"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Choose an employee to promote", "56 eligible of 60 you can see", "Clear the eligibility filter"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("promotion context omitted %q: %s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_021_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.PeopleEligibleOnly = true
	markup, err := ui.RenderToString(peoplePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="directory-context-title"`) || !strings.Contains(markup, "eligible of") {
		t.Fatalf("promotion directory lacks task heading/count: %s", markup)
	}
}

func TestTodo_UXBLIND_022(t *testing.T) {
	person := Person{Name: "Linh", PreferredName: "Linh", LegalName: "Linh Nguyen"}
	if got := PreferredFamilyName(person); got != "Linh Nguyen" {
		t.Fatalf("display name = %q, want preferred plus family name", got)
	}
	view := testView(PagePerson)
	view.People = []Person{{ID: "worker-linh", Name: person.Name, PreferredName: person.PreferredName, LegalName: person.LegalName, BasePay: testMoney("132000", "USD"), BonusTarget: "0.12"}}
	profile := personProfileProps(view, view.People[0], PagePerson)
	if profile.Hero.Name != "Linh Nguyen" {
		t.Fatalf("profile hero = %q", profile.Hero.Name)
	}
	for _, fact := range profile.Personal.Facts {
		if fact.Label == view.Locale.Text("person.worker_id") || fact.Label == view.Locale.Text("person.worker_ref") {
			t.Fatalf("internal identifier reached disclosure: %+v", fact)
		}
	}
	if len(profile.Compensation.Facts) != 0 {
		t.Fatalf("compensation remained open: %+v", profile.Compensation.Facts)
	}
}

func TestTodo_UXBLIND_022_Browser(t *testing.T) {
	view := testView(PagePerson)
	view.People = []Person{{ID: "worker-linh", WorkerID: "33a89e1a-e1f0-5cf3-ab09-e0a442557ad1", Name: "Linh", PreferredName: "Linh", LegalName: "Linh Nguyen", BasePay: testMoney("132000", "USD"), BonusTarget: "0.12"}}
	profile := personProfileProps(view, view.People[0], PagePerson)
	markup, err := ui.RenderToString(ui.CreateElement(PersonProfileComposition, PersonProfileCompositionProps{I18nProps: I18nProps{Locale: view.Locale}, Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Linh Nguyen") || (view.People[0].WorkerID != "" && strings.Contains(markup, view.People[0].WorkerID)) {
		t.Fatalf("profile identity/sensitivity projection is unsafe: %s", markup)
	}
}

func TestTodo_UXBLIND_022_Security(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	projected := ProjectAuthorizedRecord(locale, "worker-linh", map[string]string{
		"name": "Linh Nguyen", "base_pay": "USD 132,000.00", "worker_id": "33a89e1a-e1f0-5cf3-ab09-e0a442557ad1",
	}, &AuthorizedRecord{ID: "worker-linh", Disclosable: true, Fields: map[string]AuthorizedField{
		"name": {Effect: PresentationAllow}, "base_pay": {Effect: PresentationWithheld}, "worker_id": {Effect: PresentationAllow},
	}})
	if projected.Values["name"].Text != "Linh Nguyen" || projected.Values["base_pay"].Text == "USD 132,000.00" {
		t.Fatalf("field sensitivity did not distinguish display name and pay: %+v", projected.Values)
	}
}

func TestTodo_UXBLIND_023(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{
		StickyFirst: true,
		Columns:     []DataTableColumnProps{{ID: "person", Label: "Person"}, {ID: "location", Label: "Location"}, {ID: "actions", Label: "Actions"}},
		Rows:        []DataTableRowProps{{ID: "worker-1", Cells: []DataTableCellProps{{ColumnID: "person", Text: "Linh Nguyen", RowHeader: true}, {ColumnID: "location", Text: "San Francisco"}, {ColumnID: "actions", Text: "Start promotion"}}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "data-table-sticky-first") || !strings.Contains(markup, "Start promotion") {
		t.Fatalf("table did not preserve identity/action affordances: %s", markup)
	}
}

func TestTodo_UXBLIND_023_Browser(t *testing.T) {
	view := testView(PagePeople)
	markup, err := ui.RenderToString(peoplePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "data-table-sticky-first") || !strings.Contains(markup, `id="people-directory-table-viewport"`) {
		t.Fatalf("people table lacks sticky identity scroll contract: %s", markup)
	}
}

func TestTodo_UXBLIND_026_G_Copy(t *testing.T) {
	view := testView(PagePeople)
	markup, err := ui.RenderToString(ui.CreateElement(PeopleSummary, PeopleSummaryProps{CountLabel: "1 person", ScopeLabel: view.Locale.Text("people.scope")}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Start Promotion") {
		t.Fatalf("summary unexpectedly reintroduced title-case action: %s", markup)
	}
}
