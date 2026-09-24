package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_016(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(ui.CreateElement(EmploymentDetails, EmploymentDetailsProps{
		I18nProps: I18nProps{Locale: locale}, Title: "Employment overview", Description: "Current details",
		Facts: []ProfileFactProps{
			{Label: "Worker number", Value: "HC-21059", Status: WorkerFactPresent},
			{Label: "Employment type", Value: "Unavailable", Status: WorkerFactMissing},
			{Label: "Time type", Value: "Not supplied", Status: WorkerFactMissing},
			{Label: "Manager", Value: "Restricted", Status: WorkerFactWithheld},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-fact-status="PRESENT"`) ||
		!strings.Contains(markup, `<details class="profile-missing-details">`) ||
		strings.Count(markup, `data-fact-status="MISSING"`) != 2 ||
		!strings.Contains(markup, `data-fact-status="WITHHELD"`) ||
		strings.Contains(markup, `data-fact-status="UNKNOWN"`) {
		t.Fatalf("profile does not provide concise, distinct states: %s", markup)
	}
	if strings.Index(markup, "Worker number") > strings.Index(markup, `<details class="profile-missing-details">`) {
		t.Fatalf("present fact does not lead the section: %s", markup)
	}
}

func TestTodo_UXAUDIT_016_Accessibility(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(ui.CreateElement(EmploymentDetails, EmploymentDetailsProps{
		I18nProps: I18nProps{Locale: locale}, Title: "Employment overview", Description: "Current details",
		Facts: []ProfileFactProps{
			{Label: "Worker number", Value: "HC-21059", Status: WorkerFactPresent},
			{Label: "Job level", Value: "Level 4", Status: WorkerFactUnknown},
			{Label: "Manager", Value: "Restricted", Status: WorkerFactWithheld},
			{Label: "Employment type", Value: "Not reported", Status: WorkerFactMissing},
			{Label: "Time type", Value: "Not reported", Status: WorkerFactMissing},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, semantic := range []string{
		`<section class="surface person-details">`,
		`<h2>Employment overview</h2>`,
		`<dl class="person-fact-grid">`,
		`<details class="profile-missing-details">`,
		`<summary class="profile-missing-summary">2 fields not reported`,
		`<dt>Worker number</dt>`, `<dd>HC-21059</dd>`,
		`<span class="profile-fact-status status fact-unknown">Unknown</span>`,
		`<span class="profile-fact-status status fact-withheld">Restricted</span>`,
	} {
		if !strings.Contains(markup, semantic) {
			t.Errorf("profile is missing accessible semantic %q: %s", semantic, markup)
		}
	}
	if strings.Contains(markup, `class="profile-fact-status status fact-present"`) ||
		strings.Contains(markup, `class="profile-fact-status status fact-missing"`) {
		t.Fatalf("available and missing values gained redundant status text: %s", markup)
	}
}

func TestTodo_UXAUDIT_016_Security(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "worker-secure", Name: "Authorized Name", LegalName: "Raw Legal Name", Role: "Engineer"}
	view := testView(PagePerson)
	view.People = []Person{person}
	view.SelectedPerson = person.ID
	view.RecordVerdicts = map[string]AuthorizedRecord{person.ID: {ID: person.ID, Disclosable: false, Fields: map[string]AuthorizedField{
		"legal_name": {Effect: PresentationAllow},
	}}}
	profile := personProfileProps(view, person, PagePerson)
	for _, fact := range profile.Personal.Facts {
		if fact.Status != WorkerFactWithheld {
			t.Fatalf("non-disclosable personal fact %q has state %q", fact.Label, fact.Status)
		}
		if strings.Contains(fact.Value, "Raw Legal Name") {
			t.Fatalf("raw restricted identity leaked through fact %q", fact.Label)
		}
	}
	markup, err := ui.RenderToString(ui.CreateElement(PersonProfileHeader, PersonHeroProps{
		I18nProps: I18nProps{Locale: locale}, Name: "Authorized Name", Role: "Engineer",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Authorized Name") || strings.Contains(markup, "Raw Legal Name") {
		t.Fatalf("identity header exposed unauthorized data: %s", markup)
	}
}

func TestTodo_UXAUDIT_016_I18N(t *testing.T) {
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		t.Run(tag, func(t *testing.T) {
			locale := ResolveProductLocale(tag)
			markup, err := ui.RenderToString(ui.CreateElement(EmploymentDetails, EmploymentDetailsProps{
				I18nProps: I18nProps{Locale: locale}, Facts: []ProfileFactProps{
					{Label: "One", Value: locale.Text("common.not_reported"), Status: WorkerFactMissing},
					{Label: "Two", Value: locale.Text("common.not_reported"), Status: WorkerFactMissing},
				},
			}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, locale.Plural("person.unreported_fields", 2)) || strings.Contains(markup, "2 fields not reported") && tag != "en-US" {
				t.Fatalf("missing summary is not localized for %s: %s", tag, markup)
			}
		})
	}
}

func TestTodo_UXAUDIT_016_Regression(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(ui.CreateElement(ProfileFact, ProfileFactProps{
		I18nProps: I18nProps{Locale: locale}, Label: "Manager", Value: "Unknown", Status: WorkerFactUnknown,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-fact-status="UNKNOWN"`) || !strings.Contains(markup, locale.Text("person.fact_status.unknown")) {
		t.Fatalf("unknown state lost its accessible distinction: %s", markup)
	}
}

func TestTodo_UXAUDIT_016_ProfileChromeUsesOnlyAdmittedIdentity(t *testing.T) {
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		t.Run(tag, func(t *testing.T) {
			view := testView(PagePerson)
			view = ApplyLocale(view, ResolveProductLocale(tag))
			person := Person{ID: "worker-sofia", Name: "Sofia", LegalName: "Sofia PrivateSurname", WorkerNumber: "HC-21010"}
			view.People = []Person{person}
			view.SelectedPerson = person.ID
			view.RecordVerdicts = map[string]AuthorizedRecord{person.ID: {
				ID: person.ID, Disclosable: true, Fields: map[string]AuthorizedField{
					"name": {Effect: PresentationAllow}, "worker_number": {Effect: PresentationAllow},
					"legal_name": {Effect: PresentationWithheld},
				},
			}}
			want := view.Locale.Text("person.profile_identity", map[string]string{"name": "Sofia", "worker": "HC-21010"})
			if got := ResolvePageIdentity(view).Title; got != want {
				t.Fatalf("H1 label = %q, want %q", got, want)
			}
			crumbs := ResolveBreadcrumbs(view)
			if got := crumbs[len(crumbs)-1].Label; got != want {
				t.Fatalf("breadcrumb = %q, want %q", got, want)
			}
			if tag == "ar" && !strings.Contains(want, "\u2068HC-21010\u2069") {
				t.Fatalf("Arabic worker number lacks bidi isolation: %q", want)
			}
			markup, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, "<title>"+want+" · ") || !strings.Contains(markup, `id="page-title"`) || strings.Count(markup, "<h1") != 1 {
				t.Fatalf("document title or single H1 missing governed identity for %s", tag)
			}
			if strings.Contains(markup, "PrivateSurname") {
				t.Fatalf("withheld legal name leaked into rendered document for %s", tag)
			}
		})
	}
}

func TestTodo_UXAUDIT_016_ProfileChromeFailsClosed(t *testing.T) {
	person := Person{ID: "worker-sofia", Name: "Sofia", WorkerNumber: "HC-21010"}
	base := testView(PagePerson)
	base.People = []Person{person}
	base.SelectedPerson = person.ID
	for _, tc := range []struct {
		name       string
		fields     map[string]AuthorizedField
		disclose   bool
		wantPerson bool
		wantNumber bool
	}{
		{"worker number unknown", map[string]AuthorizedField{"name": {Effect: PresentationAllow}}, true, true, false},
		{"worker number withheld", map[string]AuthorizedField{"name": {Effect: PresentationAllow}, "worker_number": {Effect: PresentationWithheld}}, true, true, false},
		{"name unknown", map[string]AuthorizedField{"worker_number": {Effect: PresentationAllow}}, true, false, false},
		{"name withheld", map[string]AuthorizedField{"name": {Effect: PresentationWithheld}, "worker_number": {Effect: PresentationAllow}}, true, false, false},
		{"record withheld", map[string]AuthorizedField{"name": {Effect: PresentationAllow}, "worker_number": {Effect: PresentationAllow}}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := base
			view.RecordVerdicts = map[string]AuthorizedRecord{person.ID: {ID: person.ID, Disclosable: tc.disclose, Fields: tc.fields}}
			label := ResolvePageIdentity(view).Title
			if strings.Contains(label, "Sofia") != tc.wantPerson || strings.Contains(label, "HC-21010") != tc.wantNumber {
				t.Fatalf("unsafe identity label %q", label)
			}
			crumbs := ResolveBreadcrumbs(view)
			if crumbs[len(crumbs)-1].Label != label && tc.wantPerson {
				t.Fatalf("H1 and breadcrumb diverged: %q vs %q", label, crumbs[len(crumbs)-1].Label)
			}
			if !tc.wantPerson && label != view.Locale.Text("page.person.title") {
				t.Fatalf("hidden name did not use generic fallback: %q", label)
			}
		})
	}
	// The current ListWorkers transport admits a whole worker summary, with
	// no field-verdict map. Its worker number is already an overview fact.
	if got := ResolvePageIdentity(base).Title; got != "Sofia · HC-21010" {
		t.Fatalf("admitted ListWorkers identity = %q, want display name and worker number", got)
	}
	loading := base
	loading.ContentLoading = true
	if got := ResolveDocumentPageTitle(loading); got != loading.Locale.Text("page.person.title") {
		t.Fatalf("cross-subject loading title exposed a previous subject: %q", got)
	}
}
