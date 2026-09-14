package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXSCAN_001(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	a := ResolveWorkerIdentity(locale, Person{ID: "a", Name: "Alex", WorkerNumber: "HC-101"}, nil)
	b := ResolveWorkerIdentity(locale, Person{ID: "b", Name: "Alex", WorkerNumber: "HC-102"}, nil)
	if a.Label != "Alex · HC-101" || b.Label != "Alex · HC-102" || a.Label == b.Label {
		t.Fatalf("same-name workers are not distinguished: %q, %q", a.Label, b.Label)
	}
}

func TestTodo_UXSCAN_001_Security(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "a", Name: "Alex", LegalName: "Alex Confidential", WorkerNumber: "HC-101"}
	verdicts := map[string]AuthorizedRecord{"a": {ID: "a", Disclosable: true, Fields: map[string]AuthorizedField{
		"name": {Effect: PresentationAllow}, "worker_number": {Effect: PresentationWithheld}, "legal_name": {Effect: PresentationWithheld},
	}}}
	identity := ResolveWorkerIdentity(locale, person, verdicts)
	if identity.Label != "Alex" || strings.Contains(identity.Label, "101") || strings.Contains(identity.Label, "Confidential") {
		t.Fatalf("restricted identity leaked: %+v", identity)
	}
	verdicts["a"] = AuthorizedRecord{ID: "a", Disclosable: true, Fields: map[string]AuthorizedField{
		"name": {Effect: PresentationWithheld}, "worker_number": {Effect: PresentationAllow},
	}}
	identity = ResolveWorkerIdentity(locale, person, verdicts)
	if identity.Label != "HC-101" || strings.Contains(identity.Label, "Alex") {
		t.Fatalf("restricted name leaked: %+v", identity)
	}
}

func TestTodo_UXSCAN_001_Accessibility(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	identity := ResolveWorkerIdentity(locale, Person{ID: "a", Name: "Alex", WorkerNumber: "HC-101"}, nil)
	row := peopleDataTableRow(PeopleRowProps{I18nProps: I18nProps{Locale: locale}, ID: "a", Name: identity.Name, WorkerNumber: identity.WorkerNumber})
	markup, err := ui.RenderToString(ui.CreateElement(dataTableRow, dataTableRowRenderProps{Columns: []DataTableColumnProps{{ID: peopleSortName, Label: "Person"}}, Row: row, Cells: row.Cells}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Alex") || !strings.Contains(markup, "HC-101") {
		t.Fatalf("directory row lacks visible disambiguation: %s", markup)
	}
	view := testView(PagePerson)
	view.People = []Person{{ID: "a", Name: "Alex", WorkerNumber: "HC-101", PromotionAvailability: PromotionEligible}}
	view.SelectedPerson = "a"
	markup, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-label="Start Promotion for Alex · HC-101"`) {
		t.Fatalf("profile action omits its employee identity: %s", markup)
	}
}

func TestTodo_UXSCAN_001_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.People = []Person{{ID: "a", Name: "Alex", WorkerNumber: "HC-101", PromotionAvailability: PromotionEligible}, {ID: "b", Name: "Alex", WorkerNumber: "HC-102", PromotionAvailability: PromotionEligible}}
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HC-101", "HC-102", `aria-label="Choose a workflow for Alex · HC-101"`, `aria-label="Choose a workflow for Alex · HC-102"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("directory lacks %q", want)
		}
	}
	search := globalSearchItems(view)
	for _, person := range []struct{ id, number string }{{"a", "HC-101"}, {"b", "HC-102"}} {
		found := false
		for _, item := range search {
			if item.ID == "person:"+person.id && strings.Contains(item.Label, person.number) {
				found = true
			}
		}
		if !found {
			t.Fatalf("search result for %s lacks worker number", person.id)
		}
	}
}

func TestTodo_UXSCAN_001_Regression(t *testing.T) {
	locale := ResolveProductLocale("ar")
	identity := ResolveWorkerIdentity(locale, Person{ID: "a", Name: "Alex", WorkerNumber: "HC-101"}, nil)
	if !strings.Contains(identity.Label, "\u2068HC-101\u2069") {
		t.Fatalf("RTL identity loses bidi isolation: %q", identity.Label)
	}
	view := testView(PagePeople)
	view.RecordVerdicts = map[string]AuthorizedRecord{"intent-1": {ID: "intent-1", Disclosable: true}}
	if got := ResolveWorkerIdentity(view.Locale, view.People[0], workerIdentityVerdicts(view)).Name; got != view.People[0].Name {
		t.Fatalf("work-only verdict changed worker identity: %q", got)
	}
}
