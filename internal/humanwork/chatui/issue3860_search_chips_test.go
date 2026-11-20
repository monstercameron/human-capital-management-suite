//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strings"
	"testing"
)

const issue3860Query = `a very long budget planning question about quarterly headcount in:general kind:message lang:de before:2026-02-01 after:2026-01-01`

// #3860: active filters are compact chips that wrap, each one still removable
// by an accessible name that says "Remove filter", and the words searched for
// are printed in full above them so a truncated heading is not the only place
// the query can be read.
func TestIssue3860_FilterChipsAreCompactAndStillNamedForRemoval(t *testing.T) {
	want := map[string]struct{ remove, scope string }{
		"en-US": {"Remove filter: ", "Results for"},
		"de-DE": {"Filter entfernen: ", "Ergebnisse für"},
		"ar":    {"إزالة المرشح: ", "نتائج"},
	}
	chip := regexp.MustCompile(`<button[^>]*data-chatsearch-action="remove"[^>]*>(.*?)</button>`)
	for locale, w := range want {
		markup := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: issue3860Query}))
		chips := chip.FindAllStringSubmatch(markup, -1)
		if len(chips) < 4 {
			t.Fatalf("%s: found %d removable chips in %s", locale, len(chips), markup)
		}
		for _, c := range chips {
			if !strings.Contains(c[0], `aria-label="`+w.remove) {
				t.Errorf("%s: chip lacks an accessible removal name: %s", locale, c[0])
			}
			visible := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(strings.ReplaceAll(c[1], `aria-hidden="true"`, ""), "")
			if strings.Contains(visible, w.remove) {
				t.Errorf("%s: the chip still prints the long removal prefix: %q", locale, visible)
			}
		}
		if !strings.Contains(markup, `class="chatsearch-scope"`) || !strings.Contains(markup, w.scope) || !strings.Contains(markup, "a very long budget planning question about quarterly headcount") {
			t.Errorf("%s: the full searched words are not printed in the results", locale)
		}
	}
	css := ChatSearchStyles
	for _, need := range []string{".chatsearch-chips button", "min-height:32px", "flex-wrap:wrap"} {
		if !strings.Contains(css, need) {
			t.Errorf("chip styles miss %q", need)
		}
	}
}

// #3858: the enabled Apply filters button takes the theme-paired ink and fill
// (the primary-button recipe), never the surface colour on the brand fill.
func TestIssue3858_ApplyFiltersUsesThemePairedInk(t *testing.T) {
	css := ChatSearchStyles
	rule := regexp.MustCompile(`\.chatsearch-primary\{[^}]*\}`).FindString(css)
	if rule == "" {
		t.Fatal("no .chatsearch-primary rule")
	}
	if strings.Contains(rule, "var(--hcm-color-surface)") || !strings.Contains(rule, "var(--on-brand") || !strings.Contains(rule, "var(--accent") {
		t.Errorf("Apply filters pairs %s", rule)
	}
	for _, sel := range []string{".chatsearch-primary:hover", ".chatsearch-primary:focus-visible"} {
		if !strings.Contains(css, sel) {
			t.Errorf("no %s rule", sel)
		}
	}
	if strings.Contains(css, ".chatsearch-view mark{background:var(--hcm-color-brand-primary);color:var(--hcm-color-surface)}") {
		t.Error("highlighted words still print surface ink on the brand fill")
	}
}
