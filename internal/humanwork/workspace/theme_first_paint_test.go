package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The first paint is only final if the document the server sends already
// carries everything the client would otherwise correct. Measured on the
// running server, a workspace stored as light first painted light and then:
//
//	t=508ms   document parsed, data-hcm-color-mode="light"
//	t=7224ms  client boots and applies its defaults -> "system"
//	t=7858ms  page data arrives and the stored theme is re-applied -> "light"
//
// On a device set to dark, "system" resolves to dark, so that was a dark
// flash on every navigation. The client half of the fix is that it no longer
// applies its defaults before it has loaded anything; this file is the server
// half -- the document it defers to has to be right.

func firstPaintDocument(t *testing.T, theme productui.CustomerTheme, accessibility productui.AccessibilityPreferences) string {
	t.Helper()
	doc, err := productShellDocumentForRouteStateWithPreferences(JourneyConfig{}, false,
		productui.ResolveProductLocale("en-US"), productui.PageHome, "", "", theme, accessibility, productStylesheet())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func firstPaintRoot(t *testing.T, doc string) string {
	t.Helper()
	open := strings.Index(doc, "<html")
	end := strings.Index(doc[open:], ">")
	if open < 0 || end < 0 {
		t.Fatalf("no <html> element in the document")
	}
	return doc[open : open+end]
}

// TestFirstPaintCarriesTheStoredAccessibilityPreferences: the handler's own
// preference read returns the person's accessibility choices, and the document
// used to discard them and render the defaults. Large text and more contrast
// arrived as standard text and system contrast, then reflowed once the client
// loaded.
func TestFirstPaintCarriesTheStoredAccessibilityPreferences(t *testing.T) {
	stored := productui.AccessibilityPreferences{TextSize: "large", Contrast: "more", Motion: "reduce", Links: "underline"}
	root := firstPaintRoot(t, firstPaintDocument(t, productui.DefaultCustomerTheme(), stored))

	want := productui.AccessibilityPreferenceAttributes(productui.NormalizeAccessibilityPreferences(stored))
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		if !strings.Contains(root, name+`="`+want[name]+`"`) {
			t.Errorf("the first paint does not carry the stored %s=%q: %s", name, want[name], root)
		}
	}
	defaults := productui.AccessibilityPreferenceAttributes(productui.DefaultAccessibilityPreferences())
	if want["data-hcm-text-size"] == defaults["data-hcm-text-size"] {
		t.Fatalf("the stored text size normalised to the default; this test is not exercising a difference")
	}
}

// TestFirstPaintDeclaresTheStoredColourScheme: <meta name="color-scheme"> is
// what the browser uses before any stylesheet applies and for every control it
// draws itself. "light dark" is correct only for a workspace that follows the
// device.
func TestFirstPaintDeclaresTheStoredColourScheme(t *testing.T) {
	for mode, scheme := range map[string]string{"light": "light", "dark": "dark", "system": "light dark"} {
		theme := productui.DefaultCustomerTheme()
		theme.ColorMode = mode
		doc := firstPaintDocument(t, theme, productui.DefaultAccessibilityPreferences())
		if !strings.Contains(firstPaintRoot(t, doc), `data-hcm-color-mode="`+mode+`"`) {
			t.Errorf("%s: the first paint does not carry the stored colour mode", mode)
		}
		if !strings.Contains(doc, `<meta name="color-scheme" content="`+scheme+`">`) {
			t.Errorf("%s: the document declares the wrong color-scheme; want %q", mode, scheme)
		}
	}
}

// TestFirstPaintDefaultsStayWhereTheyWere keeps the older builder honest: it
// still renders the default accessibility preferences, so its existing callers
// mean what they meant.
func TestFirstPaintDefaultsStayWhereTheyWere(t *testing.T) {
	doc, err := productShellDocumentForRouteStateWithTheme(JourneyConfig{}, false,
		productui.ResolveProductLocale("en-US"), productui.PageHome, "", "", productui.DefaultCustomerTheme(), productStylesheet())
	if err != nil {
		t.Fatal(err)
	}
	defaults := productui.AccessibilityPreferenceAttributes(productui.DefaultAccessibilityPreferences())
	if !strings.Contains(firstPaintRoot(t, doc), `data-hcm-text-size="`+defaults["data-hcm-text-size"]+`"`) {
		t.Fatalf("the theme-only builder no longer renders the default preferences")
	}
}
