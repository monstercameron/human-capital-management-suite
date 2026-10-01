package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_090(t *testing.T) {
	people := []Person{{ID: "worker-ana", Name: "Ana", PreferredName: "Ana", LegalName: "Ana Flores", Role: "Care coordinator"}}
	recent := []WorkItem{{PersonRef: "worker-ana", Person: "Ana"}}
	items := RecentPeople(people, recent)
	if len(items) != 1 || items[0].Name != "Ana Flores" {
		t.Fatalf("recent people = %+v, want Ana Flores", items)
	}
	markup, err := ui.RenderToString(RecentPeoplePanel(RecentPeopleProps{Title: "Recent people", Items: items}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Ana Flores") || strings.Contains(markup, ">Ana<") {
		t.Fatalf("recent people shortened the display name:\n%s", markup)
	}
}

func TestTodo_UXBLIND_090_Browser(t *testing.T) {
	view := testView(PagePerson)
	view.People = []Person{{ID: "worker-ana", Name: "Ana", PreferredName: "Ana", LegalName: "Ana Flores", Role: "Care coordinator"}}
	view.SelectedPerson = "worker-ana"
	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `<h2>Ana Flores</h2>`) {
		t.Fatalf("person page repeated its primary heading:\n%s", markup)
	}
}

func TestTodo_UXBLIND_090_Regression(t *testing.T) {
	person := Person{ID: "worker-ana", Name: "Ana", PreferredName: "Ana", LegalName: "Ana Flores"}
	if got := PreferredFamilyName(person); got != "Ana Flores" {
		t.Fatalf("shared display name = %q, want Ana Flores", got)
	}
	if got := RecentPeople([]Person{person}, []WorkItem{{Person: "Ana"}}); len(got) != 1 || got[0].Name != "Ana Flores" {
		t.Fatalf("name-only recent work projection = %+v", got)
	}
}

func TestTodo_UXBLIND_092(t *testing.T) {
	props := AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Theme:     DefaultCustomerTheme(), PublishedTheme: DefaultCustomerTheme(), PreviewTenant: "HarborCare Health Services",
		ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(), Densities: DensityOptions(),
		Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}
	markup, err := ui.RenderToString(AppearancePage(props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `id="appearance-brand-name"`) || !strings.Contains(markup, `value="HarborCare Health Services"`) {
		t.Fatalf("brand field did not default to tenant display name:\n%s", markup)
	}
}

func TestTodo_UXBLIND_092_Browser(t *testing.T) {
	for _, code := range []string{"de-DE", "ar"} {
		view := testView(PageJourneys)
		view.Locale = ResolveProductLocale(code)
		view.Title = "Journeys"
		view.Loading = true
		got := ResolveDocumentPageTitle(view)
		want := view.Locale.Text("page.journeys.title")
		if got != want || got == "Journeys" {
			t.Fatalf("%s interim title = %q, want localized %q", code, got, want)
		}
	}
}

func TestTodo_UXBLIND_092_Regression(t *testing.T) {
	theme := DefaultCustomerTheme()
	if got, _ := HeaderBrandIdentity(theme, "HarborCare"); got != "HarborCare" {
		t.Fatalf("tenant fallback brand = %q, want HarborCare", got)
	}
	configured := theme
	configured.BrandName = "Northstar People"
	if got := appearanceBrandEditorTheme(configured, "HarborCare").BrandName; got != "Northstar People" {
		t.Fatalf("explicit brand was replaced by tenant name: %q", got)
	}
}
