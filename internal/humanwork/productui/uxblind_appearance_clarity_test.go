package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_020_ClarifiesOrganizationAndDeviceAppearanceScope(t *testing.T) {
	doc, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Theme:     DefaultCustomerTheme(), ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "workspace on this device") {
		t.Fatal("organization-wide color choices must not claim to apply to one device")
	}
	for _, want := range []string{
		`class="surface appearance-scope-guidance"`,
		`id="appearance-scope-title">Organization-wide appearance</h2>`,
		`Saved appearance settings apply to this organization and every signed-in user.`,
		`Use system setting lets each device resolve light or dark from its own system preference`,
		`Light and Dark are organization-wide choices.`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("appearance scope guidance missing %q in %s", want, doc)
		}
	}
}

func TestTodo_UXBLIND_021_ExplainsBrandAssetPickerWithoutDeadEndActions(t *testing.T) {
	doc, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Theme:     DefaultCustomerTheme(), ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-brand-asset-picker="true"`, `id="appearance-brand-logo-help"`,
		`Choose an existing revision or upload a PNG, JPEG, or WebP image. Save appearance to publish your selection.`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("logo guidance missing %q in %s", want, doc)
		}
	}
	for _, action := range []string{"upload", "preview", "remove", "rollback"} {
		if strings.Contains(doc, `data-hcm-asset-action="`+action+`"`) {
			t.Errorf("appearance advertised unwired %s action", action)
		}
	}
	if strings.Contains(strings.ToLower(doc), "upload endpoint") {
		t.Fatal("appearance guidance invented an unavailable upload capability")
	}
}
