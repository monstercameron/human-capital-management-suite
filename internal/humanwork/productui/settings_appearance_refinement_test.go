package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestSettingsSeparatesAccountSecurityFromPersonalPreferences(t *testing.T) {
	view := testView(PageSettings)
	view.LogoutHref = "/workspace/logout"
	markup, err := ui.RenderToString(settingsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="settings-page-stack" data-hcm-settings-scope="personal"`,
		`class="settings-group settings-account-group"`, `>Account &amp; security</h2>`,
		`class="settings-group settings-preferences-group"`, `>Personal preferences</h2>`,
		`class="surface settings-signout"`, `>Sign out</h3>`,
		`End this session on this device`, `data-hcm-setting-group="language"`, `data-hcm-setting-group="accessibility"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("settings grouping missing %q in %s", want, markup)
		}
	}
}

func TestGovernedBrandAssetPickerExposesSafeLifecycleActions(t *testing.T) {
	markup, err := ui.RenderToString(BrandAssetPicker(BrandAssetPickerProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Name: "Harborcare", Mark: "HC",
		LogoURL: "/workspace/assets/logo.svg", Editable: true,
		OnUpload: func(string) {}, OnPreview: func(string) {}, OnRemove: func() {}, OnRollback: func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-brand-asset-picker="true"`, `type="file"`, `accept="image/svg+xml,image/png,image/jpeg,image/webp"`,
		`>Upload image<input`, `>Preview</button>`, `>Remove</button>`, `>Undo changes</button>`,
		`id="appearance-brand-logo-help"`, `aria-describedby="appearance-brand-logo-help"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("brand asset lifecycle missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, `src="https://`) || strings.Contains(markup, `src="//`) {
		t.Fatal("brand asset picker emitted a remote image source")
	}
}

func TestAppearancePreviewShowsLightDarkAndCompactModesWithStickyActions(t *testing.T) {
	markup, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Theme: DefaultCustomerTheme(),
		ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(), Densities: DensityOptions(),
		Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-preview-mode="light"`, `data-hcm-preview-mode="dark"`, `data-hcm-preview-mode="compact"`,
		`data-hcm-preview-color-mode="dark"`, `data-hcm-preview-density="compact"`,
		`class="appearance-actions appearance-actions-sticky"`, `data-hcm-sticky-actions="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("appearance preview missing %q in %s", want, markup)
		}
	}
}
