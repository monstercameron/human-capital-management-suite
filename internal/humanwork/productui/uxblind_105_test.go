package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var uxblind105Footer = regexp.MustCompile(`(?s)<footer[^>]*class="footer"[^>]*>(.*?)</footer>`)

func uxblind105FooterText(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	match := uxblind105Footer.FindStringSubmatch(markup)
	if match == nil {
		t.Fatalf("page has no footer:\n%s", markup)
	}
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(match[1], " ")
}

// TestTodo_UXBLIND_105 pins the Admin overview's footer to the company name,
// on the resolved page and on the loading proxy the first paint uses, and pins
// the overview's cards.
func TestTodo_UXBLIND_105(t *testing.T) {
	view := testView(PageAdmin)
	view.Tenant = "Ironridge Builders"
	view.Appearance = DefaultCustomerTheme()
	for name, node := range map[string]ui.Node{"resolved": Build(view), "loading": BuildLoading(view)} {
		footer := uxblind105FooterText(t, node)
		if !strings.Contains(footer, "Ironridge Builders") || strings.Contains(footer, DefaultCustomerTheme().BrandName) {
			t.Fatalf("%s admin footer = %q, want the company name and not the product default", name, footer)
		}
	}
	markup, err := ui.RenderToString(Build(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []string{"Roles &amp; access", "Worker IDs", "Organization visibility"} {
		if !strings.Contains(markup, card) {
			t.Fatalf("admin overview lacks the %q card", card)
		}
	}
}

// TestTodo_UXBLIND_105_Browser holds the client's theme controller to the same
// rule: applying the appearance must rename the footer with the header's
// resolved company name, not the raw appearance name.
func TestTodo_UXBLIND_105_Browser(t *testing.T) {
	name, _ := HeaderBrandIdentity(DefaultCustomerTheme(), "Ironridge Builders")
	if name != "Ironridge Builders" {
		t.Fatalf("resolved company name = %q, want the tenant display name", name)
	}
	custom := DefaultCustomerTheme()
	custom.BrandName = "Ironridge"
	view := testView(PageAdmin)
	view.Tenant = "Ironridge Builders"
	view.Appearance = custom
	if footer := uxblind105FooterText(t, Build(view)); !strings.Contains(footer, "Ironridge") || strings.Contains(footer, "Human Capital") {
		t.Fatalf("customised footer = %q, want the configured brand", footer)
	}
}
