package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestOrganizationThemeStylesheetAndCSPShareTheExactAdmittedBytes(t *testing.T) {
	theme := productui.DefaultCustomerTheme()
	theme.Palette = "custom"
	theme.TokenOverrides = map[string]string{
		"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
		"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
		"color.surface": "#ffffff", "color.border": "#d9cfdf",
	}
	theme.DarkTokenOverrides = map[string]string{"color.canvas": "#101019", "color.surface": "#1d1b28"}
	sheet, err := productStylesheetForTheme(theme)
	if err != nil {
		t.Fatal(err)
	}
	if sheet == productStylesheet() || !strings.Contains(sheet, "--hcm-color-brand-primary:#4d1f78") || !strings.Contains(sheet, "--canvas:#101019") {
		t.Fatal("custom colors did not reach the real product stylesheet")
	}
	doc, err := productShellDocumentForRouteStateWithTheme(JourneyConfig{}, false, productui.ResolveProductLocale("en-US"), productui.PageHome, "", "", theme, sheet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-hcm-palette="custom"`) || !strings.Contains(doc, "<style>"+sheet+"</style>") {
		t.Fatal("server did not render the organization theme in the initial document")
	}
	policy := productContentSecurityPolicyForStylesheet("cell.test", sheet)
	if !strings.Contains(policy, "'"+sha256Source(sheet)+"'") || strings.Contains(policy, "'"+productStylesheetHash+"'") {
		t.Fatal("CSP did not pin exactly the organization-specific stylesheet")
	}
}
