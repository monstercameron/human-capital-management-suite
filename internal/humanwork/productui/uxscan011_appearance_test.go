package productui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXSCAN_011_AppearanceKeepsEditContextAndActionsInReach(t *testing.T) {
	markup, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Theme: DefaultCustomerTheme(),
		ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(), Densities: DensityOptions(),
		Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="appearance-section-nav surface"`,
		`href="#appearance-section-brand"`, `href="#appearance-section-color_mode"`, `href="#appearance-section-shape"`,
		`id="appearance-section-brand"`, `id="appearance-section-color_mode"`, `id="appearance-section-shape"`,
		`data-hcm-theme-current="true"`, `data-hcm-theme-proposed="true"`,
		`class="button secondary appearance-edit-preview"`,
		`data-hcm-edit-dirty="false"`, `data-hcm-action="save-appearance"`, `data-hcm-editable="true"`,
		`id="appearance-status"`, `aria-live="polite"`, `disabled`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("appearance editor missing %q", want)
		}
	}
	if !regexp.MustCompile(`\.appearance-actions-sticky\{[^}]*position:sticky;`).MatchString(Stylesheet()) {
		t.Error("appearance save area is not sticky")
	}
	if !strings.Contains(Stylesheet(), `@media (max-width:680px){.appearance-edit-command{`) {
		t.Error("appearance save area has no compact phone layout")
	}
	if !strings.Contains(Stylesheet(), `.main-scroll:has(.appearance-page){scroll-padding-block-end:160px;}`) {
		t.Error("appearance editor scrollport does not reserve space for sticky actions")
	}
}

func TestTodo_UXSCAN_011_Browser(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.appearance-edit-command .appearance-edit-preview`,
		`@media (max-width:680px){.appearance-edit-command{`,
		`scroll-padding-block-end:220px`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("browser layout contract missing %q", want)
		}
	}
}

func TestTodo_UXSCAN_011_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Theme: DefaultCustomerTheme(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-label="Appearance sections"`,
		`aria-label="Preview workspace"`,
		`aria-describedby="appearance-status"`,
		`aria-live="polite"`,
		`disabled`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("appearance accessibility contract missing %q", want)
		}
	}
}

func TestTodo_UXSCAN_011_I18N(t *testing.T) {
	for _, locale := range []string{"en-US", "de", "ar"} {
		catalog := ResolveProductLocale(locale)
		for _, key := range []string{"appearance.current_saved", "appearance.current_saved_short", "appearance.proposed", "appearance.proposed_short", "appearance.save", "appearance.save_short", "appearance.no_changes", "appearance.open_preview"} {
			value := catalog.Text(key)
			if value == "" || strings.HasPrefix(value, "⟦") {
				t.Errorf("%s missing %s: %q", locale, key, value)
			}
		}
	}
}

func TestTodo_UXSCAN_011_Regression(t *testing.T) {
	draft := DefaultCustomerTheme()
	draft.Palette = "ocean"
	previewCalled := false
	resetAppearanceDraft(&draft, func() { previewCalled = true })
	if !reflect.DeepEqual(draft, DefaultCustomerTheme()) {
		t.Fatalf("reset left stale form draft: %+v", draft)
	}
	if !previewCalled {
		t.Fatal("reset did not notify preview controller")
	}
}
