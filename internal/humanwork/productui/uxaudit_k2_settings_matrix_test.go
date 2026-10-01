package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func uxauditK2BrandPickerDocument(t *testing.T) string {
	t.Helper()
	markup, err := ui.RenderToString(BrandAssetPicker(BrandAssetPickerProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Name:      "Harborcare", Mark: "HC", Editable: true,
		LogoURL:          "/workspace/brand-assets/1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		PublishedLogoURL: "/workspace/brand-assets/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Approved: []BrandAssetOption{
			{Label: "Current logo", URL: "/workspace/brand-assets/1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Revision: 2, HeadRevision: 2, CanRemove: true, Digest: "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Width: 320, Height: 120},
			{Label: "Previous logo", URL: "/workspace/brand-assets/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Revision: 1, HeadRevision: 2, CanRollback: true, Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Width: 180, Height: 68},
		},
		OnUpload:   func(string, []byte, func(string, error)) {},
		OnLoad:     func(int, func([]BrandAssetOption, int, error)) {},
		OnPreview:  func(string) {},
		OnRemove:   func(int, func(error)) {},
		OnRollback: func(int, int, func(string, error)) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_UXAUDIT_022_Browser is the component-level browser contract for
// the governed picker selectors consumed by the live Appearance harness.
func TestTodo_UXAUDIT_022_Browser(t *testing.T) {
	markup := uxauditK2BrandPickerDocument(t)
	for _, marker := range []string{
		`data-hcm-brand-asset-picker="true"`, `data-hcm-brand-asset-governed="true"`,
		`data-hcm-asset-action="upload"`, `data-hcm-asset-action="preview"`,
		`data-hcm-asset-action="remove"`, `data-hcm-asset-action="rollback"`,
		`data-hcm-asset-variant="shell"`, `data-hcm-asset-variant="favicon"`,
		`data-hcm-asset-variant="compact"`, `data-hcm-asset-variant="contrast-light"`,
		`data-hcm-asset-variant="contrast-dark"`, `data-hcm-brand-asset-diff="changed"`,
	} {
		if !strings.Contains(markup, marker) {
			t.Fatalf("governed brand picker browser contract missing %q", marker)
		}
	}
	if strings.Contains(markup, `name="brand_logo_url"`) || strings.Contains(markup, `https://`) || strings.Contains(markup, `//evil`) {
		t.Fatal("browser picker exposed a free-form or remote asset reference")
	}
}

// TestTodo_UXAUDIT_022_Accessibility proves the picker has a native group
// name, help association, live status and keyboard-native action controls.
func TestTodo_UXAUDIT_022_Accessibility(t *testing.T) {
	markup := uxauditK2BrandPickerDocument(t)
	for _, marker := range []string{
		`<fieldset`, `<legend>`, `aria-describedby="appearance-brand-logo-help"`,
		`id="appearance-brand-logo-help"`, `role="status"`, `aria-live="polite"`,
		`type="button"`,
	} {
		if !strings.Contains(markup, marker) {
			t.Fatalf("brand picker accessibility contract missing %q", marker)
		}
	}
}

func uxauditK2SettingsDocument(t *testing.T, view View) string {
	t.Helper()
	document, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// TestTodo_UXAUDIT_023_Integration proves personal Settings links preserve
// shell state and keep tenant appearance outside the personal preference
// group.
func TestTodo_UXAUDIT_023_Integration(t *testing.T) {
	view := ApplyRequest(testView(PageSettings), PageRequest{NavCollapsed: true, Locale: "de-DE", FavoritePages: []PageID{PagePeople}})
	view.LogoutHref = "/workspace/logout"
	document := uxauditK2SettingsDocument(t, view)
	appearance := strings.Index(document, `data-hcm-setting-group="tenant-appearance"`)
	preferences := strings.Index(document, `data-hcm-setting-group="personal-preferences"`)
	if appearance < 0 || preferences < 0 || appearance > preferences {
		t.Fatalf("organization appearance crossed into personal preferences: appearance=%d preferences=%d", appearance, preferences)
	}
	for _, target := range []PageID{PageMyself, PageAppearance} {
		want := `href="` + strings.ReplaceAll(statefulHref(view, target), "&", "&amp;") + `"`
		if !strings.Contains(document, want) {
			t.Fatalf("Settings destination %s lost shell state %q", target, want)
		}
	}
	if !strings.Contains(document, `href="/workspace/logout"`) || !strings.Contains(document, `class="button danger settings-signout-action"`) {
		t.Fatal("sign-out is not exposed as a governed sensitive action")
	}
}

// TestTodo_UXAUDIT_023_Browser is the component-level browser contract. It
// checks the selectors and responsive rules a live browser harness consumes.
func TestTodo_UXAUDIT_023_Browser(t *testing.T) {
	view := testView(PageSettings)
	view.LogoutHref = "/workspace/logout"
	document := uxauditK2SettingsDocument(t, view)
	for _, group := range []string{"profile", "language", "accessibility", "notifications", "navigation", "account-security", "signout"} {
		if !strings.Contains(document, `data-hcm-setting-group="`+group+`"`) {
			t.Fatalf("browser Settings contract is missing group %q", group)
		}
	}
	css := Stylesheet()
	for _, rule := range []string{
		`@media (max-width:600px){.settings-preferences-group .locale-choice-list`,
		`@media (max-width:760px){.settings-preferences-group :is(.accessibility-group-contrast`,
		`min-height:44px`,
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("responsive Settings browser rule missing %q", rule)
		}
	}
}

// TestTodo_UXAUDIT_023_Accessibility proves every task group has a heading
// and that security-sensitive controls remain native keyboard controls.
func TestTodo_UXAUDIT_023_Accessibility(t *testing.T) {
	view := testView(PageSettings)
	view.LogoutHref = "/workspace/logout"
	markup, err := ui.RenderToString(settingsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for group, heading := range map[string]string{
		"profile": `id="settings-profile-title"`, "language": `id="locale-preferences-title"`,
		"accessibility": `id="accessibility-title"`, "notifications": `id="settings-notifications-title"`,
		"navigation": `id="settings-navigation-title"`, "account-security": `>Account &amp; security</h2>`,
		"signout": `id="settings-signout-title"`,
	} {
		if !strings.Contains(markup, `data-hcm-setting-group="`+group+`"`) || !strings.Contains(markup, heading) {
			t.Fatalf("group %q has no stable accessible heading %q", group, heading)
		}
	}
	if !strings.Contains(markup, `<a class="button danger settings-signout-action"`) || !strings.Contains(markup, `aria-current="true"`) {
		t.Fatal("sensitive action or selected navigation is not keyboard-visible")
	}
}

// TestTodo_UXAUDIT_023_I18N pins document language, direction and translated
// task-group copy for the supported Settings locales.
func TestTodo_UXAUDIT_023_I18N(t *testing.T) {
	for _, tc := range []struct {
		locale, direction string
	}{{"en-US", "ltr"}, {"de-DE", "ltr"}, {"ar", "rtl"}} {
		view := testView(PageSettings)
		view.Locale = ResolveProductLocale(tc.locale)
		document := uxauditK2SettingsDocument(t, view)
		if !strings.Contains(document, `<html lang="`+tc.locale+`" dir="`+tc.direction+`"`) {
			t.Fatalf("locale %s document root does not declare language/direction", tc.locale)
		}
		title := strings.ReplaceAll(view.Locale.Text("settings.account_group_title"), "&", "&amp;")
		if title == "" || !strings.Contains(document, title) {
			t.Fatalf("locale %s missing translated account heading %q", tc.locale, title)
		}
	}
}

// TestTodo_UXAUDIT_023_Regression keeps unavailable notifications honest and
// keeps the current navigation choice visible to assistive technology.
func TestTodo_UXAUDIT_023_Regression(t *testing.T) {
	view := ApplyRequest(testView(PageSettings), PageRequest{NavCollapsed: true, Locale: "de-DE"})
	view.LogoutHref = "/workspace/logout"
	markup, err := ui.RenderToString(settingsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	notifications := strings.Index(markup, `data-hcm-setting-group="notifications"`)
	navigation := strings.Index(markup, `data-hcm-setting-group="navigation"`)
	if notifications < 0 || navigation <= notifications {
		t.Fatal("Settings task groups are not ordered as a stable task flow")
	}
	if strings.Contains(markup[notifications:navigation], "<button") || strings.Contains(markup[notifications:navigation], `href=`) {
		t.Fatal("unavailable notifications exposes a fake action")
	}
	choices := markup[navigation:]
	if strings.Count(choices, `aria-current="true"`) != 1 || !strings.Contains(choices, `>Kompakt<span class="settings-navigation-current">Aktuell</span>`) {
		t.Fatalf("selected compact navigation is not visible and accessible: %s", choices)
	}
}
