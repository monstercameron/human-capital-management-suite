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
		LogoURL: "/workspace/brand-assets/1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", PublishedLogoURL: "/workspace/brand-assets/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Editable: true,
		Approved: []BrandAssetOption{{Label: "Logo · revision 2", URL: "/workspace/brand-assets/1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Revision: 2, HeadRevision: 2, CanRemove: true, Digest: "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Width: 320, Height: 120}, {Label: "Logo · revision 1", URL: "/workspace/brand-assets/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Revision: 1, HeadRevision: 2, Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Width: 180, Height: 68, CanRollback: true}},
		OnUpload: func(string, []byte, func(string, error)) {}, OnLoad: func(int, func([]BrandAssetOption, int, error)) {}, OnPreview: func(string) {}, OnRemove: func(int, func(error)) {}, OnRollback: func(int, int, func(string, error)) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-brand-asset-picker="true"`, `data-hcm-asset-action="upload"`,
		`>Upload image</button>`, `>Preview</button>`, `>Remove</button>`, `>Undo changes · 1</button>`,
		`id="appearance-brand-logo-help"`, `aria-describedby="appearance-brand-logo-help"`,
		`data-hcm-asset-variants="shell favicon compact contrast"`, `data-hcm-asset-variant="favicon"`, `data-hcm-brand-asset-diff="changed"`, `hcm-brand-asset-diff-field="digest"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("brand asset lifecycle missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, `src="https://`) || strings.Contains(markup, `src="//`) {
		t.Fatal("brand asset picker emitted a remote image source")
	}
}

func TestBrandAssetPickerCannotRemoveHistoricalSelection(t *testing.T) {
	markup, err := ui.RenderToString(BrandAssetPicker(BrandAssetPickerProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Name: "Harborcare", Mark: "HC", Editable: true,
		LogoURL: "/workspace/brand-assets/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Approved: []BrandAssetOption{
			{Label: "Current", URL: "/workspace/brand-assets/1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Revision: 2, HeadRevision: 2, CanRemove: true},
			{Label: "Historical", URL: "/workspace/brand-assets/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Revision: 1, HeadRevision: 2, CanRollback: true},
		},
		OnChange: func(string) {}, OnRemove: func(int, func(error)) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(markup, `data-hcm-asset-action="remove"`)
	if start < 0 {
		t.Fatalf("remove action missing from picker: %s", markup)
	}
	end := strings.Index(markup[start:], ">")
	if end < 0 || !strings.Contains(markup[start:start+end], "disabled") {
		t.Fatalf("historical selection left Remove enabled: %s", markup)
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
		`class="appearance-actions appearance-actions-sticky sticky-actions"`, `data-hcm-sticky-actions="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("appearance preview missing %q in %s", want, markup)
		}
	}
}
