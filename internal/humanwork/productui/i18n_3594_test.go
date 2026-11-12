package productui

import (
	"strings"
	"testing"
)

// TestIssue3594RoleAndAppearanceCopyIsLocalized pins that the role catalog,
// role forms and Appearance page copy have a German and Arabic translation
// instead of falling back to the English catalog.
func TestIssue3594RoleAndAppearanceCopyIsLocalized(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		var untranslated []string
		for _, key := range MissingProductTranslations(locale) {
			if strings.HasPrefix(key, "roles.") || strings.HasPrefix(key, "appearance.") || strings.HasPrefix(key, "organization_visibility.no_roles") {
				untranslated = append(untranslated, key)
			}
		}
		if len(untranslated) > 0 {
			t.Fatalf("%s: role or appearance copy falls back to English: %v", locale, untranslated)
		}
	}
}

// TestIssue3594RoleCatalogRendersLocalizedHeadings renders the role catalog
// heading in German and Arabic and asserts no English fallback text.
func TestIssue3594RoleCatalogRendersLocalizedHeadings(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{
		{"de-DE", "Rollenkatalog"},
		{"ar", "كتالوج الأدوار"},
	} {
		locale := ResolveProductLocale(tc.locale)
		if got := locale.Text("roles.catalog"); got != tc.want {
			t.Fatalf("%s: roles.catalog = %q, want %q", tc.locale, got, tc.want)
		}
		if got := locale.Text("roles.save_employee"); strings.Contains(got, "Save") {
			t.Fatalf("%s: roles.save_employee still English: %q", tc.locale, got)
		}
	}
}
