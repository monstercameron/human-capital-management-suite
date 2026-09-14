package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_023(t *testing.T) {
	view := testView(PageSettings)
	view.LogoutHref = "/workspace/logout"
	markup, err := ui.RenderToString(settingsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-settings-scope="personal"`,
		`data-hcm-setting-group="profile"`,
		`data-hcm-setting-group="language"`,
		`data-hcm-setting-group="accessibility"`,
		`data-hcm-setting-group="notifications"`,
		`data-hcm-setting-group="navigation"`,
		`data-hcm-setting-group="account-security"`,
		`data-hcm-setting-group="signout"`,
		`class="button danger settings-signout-action"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("settings task groups missing %q in %s", want, markup)
		}
	}
	if strings.Contains(strings.ToLower(markup), "storage") || strings.Contains(strings.ToLower(markup), "transport") {
		t.Fatal("settings exposed technical storage or transport terminology")
	}
}

func TestTodo_UXAUDIT_023_Regression_UnavailableAndSelectedNavigation(t *testing.T) {
	view := ApplyRequest(testView(PageSettings), PageRequest{NavCollapsed: true, Locale: "de-DE", FavoritePages: []PageID{PagePeople}})
	view.LogoutHref = "/workspace/logout"
	markup, err := ui.RenderToString(settingsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	notifications := strings.Index(markup, `data-hcm-setting-group="notifications"`)
	navigation := strings.Index(markup, `data-hcm-setting-group="navigation"`)
	if notifications < 0 || navigation <= notifications {
		t.Fatalf("Settings notifications or navigation section missing: %s", markup)
	}
	if strings.Contains(markup[notifications:navigation], "<button") || strings.Contains(markup[notifications:navigation], "Noch nicht verfügbar</button>") {
		t.Fatalf("Notifications advertises a fake action: %s", markup[notifications:navigation])
	}
	choices := markup[navigation:]
	if strings.Count(choices, `aria-current="true"`) != 1 || !strings.Contains(choices, `>Kompakt<span class="settings-navigation-current">Aktuell</span>`) {
		t.Fatalf("compact navigation selection is not visibly and accessibly marked: %s", choices)
	}
	for _, target := range []PageID{PageMyself, PageAppearance} {
		want := `href="` + strings.ReplaceAll(statefulHref(view, target), "&", "&amp;") + `"`
		if !strings.Contains(markup, want) {
			t.Fatalf("Settings link to %s lost shell state %q: %s", target, want, markup)
		}
	}
	css := Stylesheet()
	for _, want := range []string{`.settings-navigation-choices .button[aria-current=true]{`, `border-color:var(--accent)`, `background-color:var(--soft)`, `.settings-navigation-current{`, `min-height:44px`} {
		if !strings.Contains(css, want) {
			t.Fatalf("navigation selection treatment missing %q", want)
		}
	}
}
