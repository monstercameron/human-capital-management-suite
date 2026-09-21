package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-008's RED was measured on the running server: Brand & appearance
// showed brand_name "Human Capital Management Suite" under the help text
// "Accessible name and fallback shown in the global product header" and
// brand_mark "H", while the live header rendered "Harborcare Demo" and "HD".
//
// The header is right: HeaderBrandIdentity deliberately falls back to the
// admitted tenant name while the theme still holds the product default. The
// form was the misleading half -- it presented a stored default as the value
// in force. It now says what the header actually shows.

func uxlive008Brand(t *testing.T, theme CustomerTheme, tenant string) string {
	t.Helper()
	doc, err := ui.RenderToString(appearanceBrandSignature(
		I18nProps{Locale: ResolveProductLocale("en-US")}, theme, tenant, nil, nil, nil))
	if err != nil {
		t.Fatalf("render brand fields: %v", err)
	}
	return doc
}

// TestTodo_UXLIVE_008 is the primary red/green test: an unconfigured brand
// field says what the header shows instead of implying its own value is it.
func TestTodo_UXLIVE_008(t *testing.T) {
	defaults := DefaultCustomerTheme()
	unconfigured := uxlive008Brand(t, defaults, "Harborcare Demo")

	headerName, headerMark := HeaderBrandIdentity(defaults, "Harborcare Demo")
	if headerName == defaults.BrandName {
		t.Fatalf("fixture does not exercise the fallback: header shows %q", headerName)
	}
	if !strings.Contains(unconfigured, headerName) {
		t.Fatalf("the brand form never mentions the name the header actually shows (%q):\n%s", headerName, unconfigured)
	}
	if !strings.Contains(unconfigured, headerMark) {
		t.Fatalf("the brand form never mentions the mark the header actually shows (%q):\n%s", headerMark, unconfigured)
	}

	// A configured brand is in force, so the form states it plainly with no
	// extra explanation about a fallback that no longer applies.
	configured := defaults
	configured.BrandName, configured.BrandMark = "Harborcare", "HC"
	set := uxlive008Brand(t, configured, "Harborcare Demo")
	if strings.Contains(set, "currently shows") {
		t.Fatalf("a configured brand still explains a fallback it does not use:\n%s", set)
	}
	if !strings.Contains(set, `value="Harborcare"`) {
		t.Fatalf("the configured brand name is not in its field:\n%s", set)
	}
}

// TestTodo_UXLIVE_008_Browser keeps the fields themselves unchanged: the
// stored value is still what the field holds and what a save would send.
func TestTodo_UXLIVE_008_Browser(t *testing.T) {
	defaults := DefaultCustomerTheme()
	doc := uxlive008Brand(t, defaults, "Harborcare Demo")
	if !strings.Contains(doc, `value="`+defaults.BrandName+`"`) {
		t.Fatalf("the stored brand name left its field:\n%s", doc)
	}
	if !strings.Contains(doc, `name="brand_name"`) || !strings.Contains(doc, `name="brand_mark"`) {
		t.Fatalf("a brand field lost its form name:\n%s", doc)
	}
}
