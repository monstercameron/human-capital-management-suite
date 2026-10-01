package productui

import (
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_012_Browser is the browser matrix entry. The lane cannot
// start the production server or drive a real browser, so it verifies the
// production document and stylesheet contracts that the browser receives:
// every registered page is rendered in each supported writing direction and
// in light/dark, high-contrast, and reduced-motion presentation states.
func TestTodo_UIPOLISH_012_Browser(t *testing.T) {
	css := Stylesheet()
	for _, contract := range []string{
		"@media (min-width:761px)",
		"@media (max-width:760px)",
		"@media (min-width:1440px)",
		":focus-visible",
		":disabled",
		"max-width:100%",
	} {
		if !strings.Contains(css, contract) {
			t.Fatalf("production browser contract is missing %q", contract)
		}
	}

	states := []struct {
		name      string
		colorMode string
		contrast  string
		motion    string
	}{
		{name: "light", colorMode: "light", contrast: "system", motion: "system"},
		{name: "dark-high-contrast-reduced", colorMode: "dark", contrast: "more", motion: "reduce"},
	}
	for _, localeCode := range []string{"en-US", "de-DE", "ar"} {
		localeCode := localeCode
		for _, state := range states {
			state := state
			for _, definition := range PageDefinitions() {
				definition := definition
				t.Run(strings.Join([]string{localeCode, state.name, string(definition.ID)}, "/"), func(t *testing.T) {
					view := testView(definition.ID)
					view.Locale = ResolveProductLocale(localeCode)
					view.Appearance = DefaultCustomerTheme()
					view.Appearance.ColorMode = state.colorMode
					view.Accessibility = AccessibilityPreferences{
						TextSize: "standard",
						Contrast: state.contrast,
						Motion:   state.motion,
						Links:    "underlined",
					}
					doc, err := Render(view)
					if err != nil {
						t.Fatalf("render %s: %v", definition.ID, err)
					}
					for _, marker := range []string{
						`<meta name="viewport" content="width=device-width, initial-scale=1">`,
						`data-hcm-catalog="product-ui.v1"`,
						`lang="` + view.Locale.Resolved + `"`,
						`dir="` + string(view.Locale.Direction) + `"`,
						`data-hcm-color-mode="` + state.colorMode + `"`,
						`data-hcm-contrast="` + state.contrast + `"`,
						`data-hcm-motion-preference="` + state.motion + `"`,
					} {
						if !strings.Contains(doc, marker) {
							t.Errorf("%s browser document is missing %q", definition.ID, marker)
						}
					}
					if strings.Count(doc, "<main") != 1 || strings.Count(doc, "<h1") != 1 {
						t.Fatalf("%s browser document must have one main and one h1", definition.ID)
					}
					if strings.Contains(doc, "TODO") || strings.Contains(doc, "private-backend-sentinel") {
						t.Fatalf("%s browser document contains technical or raw backend text", definition.ID)
					}
				})
			}
		}
	}
}
