package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_REV_092_01 proves density resolves user-override-then-tenant:
// the rendered document and the density-aware pages follow a person's own
// choice, a person with no choice keeps the organization density, and no
// personal value outside the qualified presets can reach the page.
func TestTodo_REV_092_01(t *testing.T) {
	organization := DefaultCustomerTheme()
	organization.Density = "spacious"

	if got := EffectiveAppearance(organization, "").Density; got != "spacious" {
		t.Fatalf("no personal choice resolved to %q, want the organization's spacious", got)
	}
	if got := EffectiveAppearance(organization, "compact").Density; got != "compact" {
		t.Fatalf("personal compact resolved to %q", got)
	}
	for _, unadmitted := range []string{"ultra-compact", "0.5", "compact;min-height:10px", "COMPACTER"} {
		if got := EffectiveAppearance(organization, unadmitted).Density; got != "spacious" {
			t.Fatalf("unadmitted personal density %q resolved to %q", unadmitted, got)
		}
	}
	// Every density a person can pick is one of the qualified presets, whose
	// compact rule keeps the 44px control floor (UIPOLISH-006).
	for _, option := range DensityOptions() {
		if NormalizePersonalDensity(option.ID) != option.ID {
			t.Fatalf("qualified preset %q is not selectable", option.ID)
		}
	}
	if !strings.Contains(Stylesheet(), "--hcm-control-height-compact:44px") {
		t.Fatal("compact density lost its 44px control floor")
	}

	personal := testView(PageHistory)
	personal.Appearance = organization
	personal.StoredPreferences.Density = "compact"
	doc, err := Render(personal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-hcm-density="compact"`) {
		t.Fatal("the document root does not carry the person's own density")
	}
	if historyDensityFromTheme(personal.EffectiveAppearance()) != HistoryDensityCompact {
		t.Fatal("history density does not follow the personal choice")
	}

	colleague := testView(PageHistory)
	colleague.Appearance = organization
	doc, err = Render(colleague)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-hcm-density="spacious"`) {
		t.Fatal("a colleague with no personal choice lost the organization density")
	}

	// The Settings page offers the control, bound to the personal value and
	// the save callback, with the organization default as an explicit option.
	settings := testView(PageSettings)
	settings.Appearance = organization
	settings.StoredPreferences.Density = "compact"
	settings.SaveDensity = func(string) {}
	html, err := ui.RenderToString(settingsPage(settings))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-hcm-setting-group="personal-density"`, `id="personal-density-compact"`, "Organization default (Spacious)", `value="organization"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("settings density control missing %q", want)
		}
	}
	if !regexpChecked(html, "personal-density-compact") || regexpChecked(html, "personal-density-organization") {
		t.Fatalf("the stored personal choice is not the checked option:\n%s", html)
	}
}

// regexpChecked reports whether the radio with id carries checked.
func regexpChecked(doc, id string) bool {
	at := strings.Index(doc, `id="`+id+`"`)
	if at < 0 {
		return false
	}
	start := strings.LastIndex(doc[:at], "<input")
	end := strings.Index(doc[at:], ">")
	if start < 0 || end < 0 {
		return false
	}
	return strings.Contains(doc[start:at+end], "checked")
}

// TestTodo_REV_092_01_Golden pins the density control's markup in each
// supported locale, so its names, grouping and labels cannot drift silently.
func TestTodo_REV_092_01_Golden(t *testing.T) {
	want := map[string]string{
		"en-US": `class="accessibility-group accessibility-group-density" data-hcm-setting-group="personal-density"`,
		"de-DE": `Standard der Organisation (Komfortabel)`,
		"ar":    `الإعداد الافتراضي للمؤسسة (مريح)`,
	}
	for tag, fragment := range want {
		doc, err := ui.RenderToString(personalDensityFieldsetFixture(tag))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, fragment) {
			t.Fatalf("%s density panel lacks %q:\n%s", tag, fragment, doc)
		}
		if strings.Count(doc, `name="personal_density"`) != 4 {
			t.Fatalf("%s density panel does not offer exactly the inherit option and three presets:\n%s", tag, doc)
		}
		if !regexpChecked(doc, "personal-density-organization") {
			t.Fatalf("%s: with no personal choice the organization default is not selected:\n%s", tag, doc)
		}
	}
	en, err := ui.RenderToString(personalDensityFieldsetFixture("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	const golden = `<legend>Layout density</legend><p class="muted" id="personal-density-help">This setting changes only your own view, not what other people see.</p>`
	if !strings.Contains(en, golden) {
		t.Fatalf("en-US density panel drifted from the pinned bytes:\n%s", en)
	}
}

// personalDensityFieldsetFixture renders the density group inside the
// accessibility form it belongs to, with no personal choice stored.
func personalDensityFieldsetFixture(tag string) ui.Node {
	return AccessibilityPreferencesPanel(AccessibilityPreferencesProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale(tag)},
		TextSizes: AccessibilityTextSizeOptions(), Contrasts: AccessibilityContrastOptions(),
		Motions: AccessibilityMotionOptions(), LinkStyles: AccessibilityLinkOptions(),
		Density: &PersonalDensityProps{Locale: ResolveProductLocale(tag), OrganizationDensity: "comfortable"},
	})
}
