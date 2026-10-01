package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_068(t *testing.T) {
	view := View{Locale: ResolveProductLocale("en-US"), PeopleColumns: defaultPeopleColumns}
	markup, err := ui.RenderToString(ui.CreateElement(ColumnChooser, peopleColumnChooserProps(view)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="column-chooser"`, `data-hcm-transient-popover="column-chooser"`,
		`data-hcm-popover-grace-ms="180"`, `class="popover-surface column-chooser-panel"`,
		`role="dialog"`, `id="people-columns-label"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("column chooser missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `<details open`) {
		t.Fatal("column chooser should stay closed until its anchored trigger is activated")
	}
	if !strings.Contains(columnChooserStylesheet(), ".column-chooser-panel{position:absolute") {
		t.Fatal("chooser choices must be in an anchored floating panel")
	}
	if got := formattedPeopleRole("Journeyman Carpenter · C3", "C3"); got != "Journeyman Carpenter" {
		t.Fatalf("role duplicate was not removed: %q", got)
	}
	if got := formattedPeopleLocation("Aurora, CO jobsite"); got != "Aurora, CO" {
		t.Fatalf("location was not normalized: %q", got)
	}
	if got := NormalizePeopleColumns(""); got != defaultPeopleColumns || strings.Contains(","+got+",", ",grade,") {
		t.Fatalf("default columns retain a redundant job level: %q", got)
	}
}

func TestTodo_UXBLIND_068_Browser(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(PeoplePage, PeoplePageProps{
		I18nProps:     I18nProps{Locale: ResolveProductLocale("en-US")},
		ColumnChooser: ColumnChooserProps{ID: "people-columns", Selected: defaultPeopleColumns, Options: []ColumnChoice{{ID: "name", Label: "Person", Required: true}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `class="column-chooser"`) != 1 || !strings.Contains(markup, `class="people-search-panel"`) {
		t.Fatalf("browser baseline must mount one anchored chooser in the search panel: %s", markup)
	}
}

func TestTodo_UXBLIND_069(t *testing.T) {
	cases := map[string]string{
		"Site Lead":         "site_lead",
		"  123 Field Lead ": "role_123_field_lead",
		"Already__ready":    "already_ready",
	}
	for input, want := range cases {
		if got := deriveRoleID(input); got != want {
			t.Errorf("deriveRoleID(%q) = %q, want %q", input, got, want)
		}
	}
	if got := validateRoleCreateDraft(AccessRole{ID: "Site Lead"}); got.ID != "roles.create_id_invalid" || got.Name != "roles.create_name_required" {
		t.Fatalf("invalid role draft = %+v", got)
	}
	if got := validateRoleCreateDraft(AccessRole{ID: "site_lead", Name: "Site Lead"}); !got.valid() {
		t.Fatalf("valid role draft reported errors: %+v", got)
	}
	view := RolesPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, CanCreate: true, OnSaveRole: func(AccessRole) {}}
	markup, err := ui.RenderToString(ui.CreateElement(RolesPage, view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, "Create a role") != 1 || !strings.Contains(markup, "Suggested from the display name") || !strings.Contains(markup, "After creation, configure page and feature access") {
		t.Fatalf("role creation form does not explain its workflow: %s", markup)
	}
	if !strings.Contains(markup, `pattern="[a-z][a-z0-9_]{1,62}"`) || !strings.Contains(markup, `required`) {
		t.Fatal("role creation form lost native constraints")
	}
}

func TestTodo_UXBLIND_069_Browser(t *testing.T) {
	markup, err := ui.RenderToString(createRoleForm(I18nProps{Locale: ResolveProductLocale("en-US")}, true, func(AccessRole) {}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `name="role_id"`) || !strings.Contains(markup, `name="role_name"`) || strings.Contains(markup, "roles.create_id_help") {
		t.Fatalf("role form did not render editable, translated fields: %s", markup)
	}
	if !strings.Contains(markup, `<form`) || !strings.Contains(markup, `novalidate`) || !strings.Contains(markup, `pattern="[a-z][a-z0-9_]{1,62}"`) || !strings.Contains(markup, `required`) {
		t.Fatalf("role form must route invalid submissions through inline validation while retaining field constraints: %s", markup)
	}
}

func TestTodo_UXBLIND_069_Accessibility(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	markup, err := ui.RenderToString(roleCreateErrorNode(i18n, "new-role-id-error", "roles.create_id_invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `id="new-role-id-error"`) || !strings.Contains(markup, `role="alert"`) || !strings.Contains(markup, "Use lowercase letters") {
		t.Fatalf("role validation error is not an accessible inline message: %s", markup)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		text := ResolveProductLocale(locale).Text("roles.create_id_help")
		if text == "roles.create_id_help" || strings.TrimSpace(text) == "" {
			t.Fatalf("missing role creation translation for %s", locale)
		}
	}
}
