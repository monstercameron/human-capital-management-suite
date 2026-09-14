package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// namedIcons is every icon the page can draw, so the assertions below run
// over the whole set rather than a sample of it.
func namedIcons() map[string]ui.Node {
	return map[string]ui.Node{
		"brand mark":  BrandMark(),
		"check":       iconCheck("c"),
		"arrow right": iconArrowRight("c"),
		"arrow left":  iconArrowLeft("c"),
		"info":        iconInfo("c"),
		"success":     iconSuccess("c"),
		"warning":     iconWarning("c"),
		"danger":      iconDanger("c"),
		"ledger":      iconLedger("c"),
		"empty":       iconEmpty("c"),
		"clock":       iconClock("c"),
		"person":      iconPerson("c"),
		"spark":       iconSpark("c"),
	}
}

func renderNode(t *testing.T, n ui.Node) string {
	t.Helper()
	out, err := ui.RenderToString(n)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return out
}

// TestIconsAreInlineSVG is the CSP check in miniature: nothing on this page
// may fetch a glyph, so every icon has to be markup.
func TestIconsAreInlineSVG(t *testing.T) {
	for name, node := range namedIcons() {
		t.Run(name, func(t *testing.T) {
			out := renderNode(t, node)
			if !strings.HasPrefix(out, "<svg ") {
				t.Fatalf("icon does not render as an inline svg: %s", out)
			}
			if !strings.Contains(out, `xmlns="http://www.w3.org/2000/svg"`) {
				t.Error("icon is missing the SVG namespace, so it will not render outside XHTML")
			}
			// The SVG namespace is a name, not a fetch, so it is stripped
			// before looking for anything that would actually load.
			fetches := strings.ReplaceAll(out, `xmlns="http://www.w3.org/2000/svg"`, "")
			for _, forbidden := range []string{"<image", "<use", "url(", "http://", "https://"} {
				if strings.Contains(fetches, forbidden) {
					t.Errorf("icon references %q; the content-security-policy blocks external loads", forbidden)
				}
			}
		})
	}
}

// TestIconsAreDecorative asserts every icon is hidden from assistive
// technology and out of the tab order. That is only correct because no icon
// on this page is the sole carrier of anything: each sits beside a word.
func TestIconsAreDecorative(t *testing.T) {
	for name, node := range namedIcons() {
		t.Run(name, func(t *testing.T) {
			out := renderNode(t, node)
			if !strings.Contains(out, `aria-hidden="true"`) {
				t.Error("icon is exposed to assistive technology but carries no meaning of its own")
			}
			if !strings.Contains(out, `focusable="false"`) {
				t.Error("icon is missing focusable=\"false\" and can land in the tab order")
			}
		})
	}
}

// TestIconsInheritTheirColor keeps tone styling in the stylesheet: an icon
// with a baked-in fill would not follow the chip or callout it sits in.
func TestIconsInheritTheirColor(t *testing.T) {
	for name, node := range namedIcons() {
		t.Run(name, func(t *testing.T) {
			out := renderNode(t, node)
			if !strings.Contains(out, "currentColor") {
				t.Errorf("icon does not paint with currentColor: %s", out)
			}
		})
	}
}

// TestIconForToneIsTotal checks the tone-to-icon mapping never returns nil,
// including for a tone the projecting lane might add later.
func TestIconForToneIsTotal(t *testing.T) {
	cases := []struct{ tone, want string }{
		{toneSuccess, "m8.5 12.2"},
		{toneWarning, "M10.3 4.3"},
		{toneDanger, "m15 9-6 6"},
		{toneInfo, "M12 11v5"},
		{toneNeutral, "M12 11v5"},
		{"a tone nobody has invented yet", "M12 11v5"},
	}
	for _, tc := range cases {
		t.Run(tc.tone, func(t *testing.T) {
			out := renderNode(t, iconForTone(tc.tone, "c"))
			if !strings.Contains(out, tc.want) {
				t.Errorf("iconForTone(%q) did not draw the expected glyph (looking for %q): %s", tc.tone, tc.want, out)
			}
		})
	}
}

// TestIconForSeverityIsTotal does the same for the findings vocabulary,
// which uses "blocking" where the tone vocabulary uses "danger".
func TestIconForSeverityIsTotal(t *testing.T) {
	cases := []struct{ severity, want string }{
		{severityBlocking, "m15 9-6 6"},
		{severityWarning, "M10.3 4.3"},
		{severitySuccess, "m8.5 12.2"},
		{severityInfo, "M12 11v5"},
		{"unrecognised", "M12 11v5"},
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			out := renderNode(t, iconForSeverity(tc.severity, "c"))
			if !strings.Contains(out, tc.want) {
				t.Errorf("iconForSeverity(%q) did not draw the expected glyph (looking for %q): %s", tc.severity, tc.want, out)
			}
		})
	}
}

// TestIconsCarryTheClassTheyAreGiven keeps the styling hook wired: the
// stylesheet targets .jn-notice-icon, .jn-finding-icon and friends.
func TestIconsCarryTheClassTheyAreGiven(t *testing.T) {
	out := renderNode(t, iconInfo("jn-notice-icon"))
	if !strings.Contains(out, `class="jn-notice-icon"`) {
		t.Errorf("icon dropped its class: %s", out)
	}
}
