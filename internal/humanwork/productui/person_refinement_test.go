package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestPersonWorkerFactStatesRemainDistinct(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "worker-1", WorkerNumber: "WN-1", JobCode: ""}
	silent := ResolveWorkerOverview(locale, person, nil)
	if silent.Facts[0].Status != WorkerFactPresent || silent.Facts[3].Status != WorkerFactMissing {
		t.Fatalf("silent fact states = %#v", silent.Facts)
	}
	governed := map[string]AuthorizedRecord{"worker-1": {ID: "worker-1", Disclosable: true, Fields: map[string]AuthorizedField{
		"worker_number": {Effect: PresentationAllow},
		"job_code":      {Effect: PresentationAllow},
		"job_level":     {Effect: PresentationDenied},
		"hire_date":     {Disposition: FieldHide},
	}}}
	section := ResolveWorkerOverview(locale, person, governed)
	states := map[string]WorkerFactStatus{}
	for _, fact := range section.Facts {
		states[fact.Name] = fact.Status
	}
	if states["worker_number"] != WorkerFactPresent || states["job_code"] != WorkerFactMissing || states["job_level"] != WorkerFactWithheld {
		t.Fatalf("governed fact states = %#v", states)
	}
	if _, hidden := states["hire_date"]; hidden {
		t.Fatal("HIDE fact survives the section boundary")
	}
	// A denied field must remain distinguishable from an omitted verdict.
	governed["worker-1"].Fields["hire_date"] = AuthorizedField{Effect: PresentationWithheld}
	section = ResolveWorkerOverview(locale, person, governed)
	for _, fact := range section.Facts {
		if fact.Name == "hire_date" && fact.Status != WorkerFactWithheld {
			t.Fatalf("withheld fact state = %q", fact.Status)
		}
	}
}

func TestPersonPageSeparatesActiveAndPastWork(t *testing.T) {
	view := testView(PagePerson)
	view.Work = append(view.Work,
		WorkItem{ID: "intent-active", PersonRef: "worker-avery", Person: "Avery Patel", Title: "Transfer", Status: "Awaiting approval", Href: "/workspace/app/journeys?journey=intent-active"},
	)
	person, ok := exactPerson(view)
	if !ok {
		t.Fatal("fixture resolves no person")
	}
	profile := personProfileProps(view, person, PagePerson)
	if len(profile.Active.Items) != 1 || profile.Active.Items[0].ID != "intent-active" {
		t.Fatalf("active work = %#v", profile.Active.Items)
	}
	markup, err := ui.RenderToString(ui.CreateElement(PersonPage, PersonPageProps{I18nProps: I18nProps{Locale: view.Locale}, Profile: &profile}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Active workflows") || !strings.Contains(markup, "Transfer") || !strings.Contains(markup, "Past workflows") {
		t.Fatalf("person workflow sections missing: %s", markup)
	}
	if strings.Count(markup, "intent-active") != 1 {
		t.Fatalf("active work rendered more than once: %s", markup)
	}
}
