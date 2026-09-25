package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAppearancePreviewIsAnOverlayNotANewRoute(t *testing.T) {
	view := testView(PageAppearance)
	markup, err := ui.RenderToString(appearancePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="appearance-preview-open"`, `aria-haspopup="dialog"`,
		`id="appearance-preview-dialog"`, `role="dialog"`, `aria-modal="true"`,
		`aria-hidden="true"`, `hidden="hidden"`, `data-hcm-preview-page="home"`,
		`data-hcm-preview-mode="current"`, `data-hcm-preview-mode="light"`, `data-hcm-preview-mode="dark"`,
		`data-hcm-preview-color-mode="current"`, `aria-pressed="true"`, `inert=""`,
		`Read-only preview`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("preview overlay missing %q", want)
		}
	}
	if strings.Contains(markup, `<iframe`) {
		t.Fatal("preview must reuse authorized Go components, not frame another site")
	}
	if preview, brand := strings.Index(markup, `id="appearance-preview-open"`), strings.Index(markup, `appearance-brand-name`); preview < 0 || brand < 0 || preview > brand {
		t.Fatal("preview action must be discoverable before the long appearance editor")
	}
}

func TestAppearancePreviewModeColorsAreScopedToTheScene(t *testing.T) {
	styles := Stylesheet()
	for _, mode := range []string{"light", "dark"} {
		selector := `:root[data-hcm-palette="evergreen"] .appearance-preview-scene[data-hcm-preview-color-mode="` + mode + `"]`
		if !strings.Contains(styles, selector) {
			t.Errorf("missing %s scene palette scope", mode)
		}
	}
	if !strings.Contains(styles, `--hcm-color-surface`) || !strings.Contains(styles, `--hcm-color-text`) {
		t.Fatal("preview mode must supply semantic tokens to embedded real page components")
	}
	for _, alias := range []string{"--surface-subtle:", "--control-border:", "--success-bg:", "--warning-bg:"} {
		if !strings.Contains(styles, alias) {
			t.Errorf("preview mode lacks derived color alias %s", alias)
		}
	}
	for _, rule := range []string{
		`.appearance-preview-scene .people-directory .data-table-scroll{max-height:none;overflow:visible;}`,
		`.appearance-preview-scene .count{background-color:var(--soft);color:var(--ink);}`,
	} {
		if !strings.Contains(styles, rule) {
			t.Errorf("missing preview-only presentation rule %q", rule)
		}
	}
}

func TestAppearancePreviewStartsWithoutUnrelatedDirectoryState(t *testing.T) {
	view := testView(PageAppearance)
	view.Query = "rafael"
	view.PeoplePage = 4
	view.PeopleTeam = "Operations"
	view.PeopleLocation = "Seattle"
	view.PeopleEligibleOnly = true
	view.WorkFilter = "blocked"
	view.SelectedWork = "work-42"
	view.Navigate = func(string) {}
	preview := appearancePreviewView(view, PagePeople)
	if preview.Page != PagePeople || preview.Navigate != nil || preview.Query != "" || preview.PeoplePage != 0 || preview.PeopleTeam != "" || preview.PeopleLocation != "" || preview.PeopleEligibleOnly || preview.WorkFilter != "" || preview.SelectedWork != "" {
		t.Fatalf("preview inherited unrelated live-page state: %+v", preview)
	}
	if view.Query != "rafael" || view.PeopleTeam != "Operations" || view.SelectedWork != "work-42" {
		t.Fatal("preview reset mutated the original live view")
	}
}

func TestAppearanceGlyphPresetsHaveDistinctGovernedStyles(t *testing.T) {
	styles := Stylesheet()
	for _, id := range []string{"fine-line", "square-bold", "soft-badge"} {
		if !hasAppearancePreset(glyphPresets, id) {
			t.Errorf("missing glyph option %q", id)
		}
		if !strings.Contains(styles, `data-hcm-glyphs="`+id+`"`) || !strings.Contains(styles, `data-hcm-glyph-sample="`+id+`"`) {
			t.Errorf("glyph %q has no live and choice-card styles", id)
		}
	}
	if !strings.Contains(styles, `:root .appearance-glyph-sample:not([data-hcm-glyph-sample="soft-badge"]) .nav-icon`) {
		t.Fatal("selected badge style would contaminate other glyph choice previews")
	}
	unknown := DefaultCustomerTheme()
	unknown.Glyphs = "arbitrary-script"
	if got := NormalizeCustomerTheme(unknown).Glyphs; got != DefaultCustomerTheme().Glyphs {
		t.Fatalf("unknown glyph set admitted: %q", got)
	}
}

func TestAppearanceBrandPickerUsesOnlyApprovedSameOriginChoices(t *testing.T) {
	markup, err := ui.RenderToString(BrandAssetPicker(BrandAssetPickerProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Editable: true,
		Approved: []BrandAssetOption{{Label: "Use approved logo", URL: "/workspace/assets/logo.svg"}, {Label: "Bad remote", URL: "https://example.test/logo.svg"}},
		OnChange: func(string) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-hcm-asset-action="choose"`) || !strings.Contains(markup, `Use approved logo`) {
		t.Fatal("approved logo choice missing")
	}
	if strings.Contains(markup, `name="brand_logo_url"`) {
		t.Fatal("appearance must not expose a free-form logo path input")
	}
	if strings.Contains(markup, "Bad remote") || strings.Contains(markup, "example.test") {
		t.Fatal("remote asset leaked into approved logo picker")
	}
}
