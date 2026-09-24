package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// C1: the ordinary dark palette must redefine the registered brand-soft
// token. Selected rows, the bulk bar and the active nav paint it directly,
// and its light value #eaf3ef under dark ink measured 1.05:1.
func TestDocsDarkBrandSoftTokenIsDark(t *testing.T) {
	css := darkModeStylesStylesheet()
	for _, selector := range []string{
		`:root[data-hcm-color-mode="dark"]{`,
		`@media (prefers-color-scheme:dark){:root[data-hcm-color-mode="system"]{`,
	} {
		start := strings.Index(css, selector)
		if start < 0 {
			t.Fatalf("dark stylesheet lacks %q", selector)
		}
		block := css[start : start+strings.Index(css[start:], "}")]
		if !strings.Contains(block, "--hcm-color-brand-soft:color-mix(in srgb,var(--hcm-color-brand-primary) 18%,#16202a)") {
			t.Errorf("%s does not redefine --hcm-color-brand-soft: %.300s", selector, block)
		}
		if strings.Contains(block, "--hcm-color-brand-soft:var(--soft)") {
			t.Errorf("%s makes brand-soft refer to --soft, which print resets back to brand-soft (a cycle)", selector)
		}
	}
	// Print on a dark root resets the token to the registered light default.
	var def string
	for _, token := range registeredThemeTokens {
		if token.CSSVariable == "--hcm-color-brand-soft" {
			def = token.Default
		}
	}
	if !strings.Contains(css, "@media (print){") || !strings.Contains(css, "--hcm-color-brand-soft:"+def) {
		t.Errorf("print palette does not restore --hcm-color-brand-soft to %s", def)
	}
	// The value CSS paints is the one the theme qualifier checks.
	light, err := ResolveTheme(nil)
	if err != nil {
		t.Fatal(err)
	}
	dark := darkThemeValues(light.values)
	for _, pair := range [][2]string{
		{dark["color.text.primary"], dark["color.brand.soft"]},
		{dark["color.brand.primary"], dark["color.brand.soft"]},
		{dark["color.text.muted"], dark["color.brand.soft"]},
	} {
		ratio, err := contrastRatio(pair[0], pair[1])
		if err != nil || ratio < 4.5 {
			t.Errorf("dark %s on selected surface %s = %.2f:1, want >= 4.5 (%v)", pair[0], pair[1], ratio, err)
		}
	}
}

// M7, M10, M11, L1, L2, L5, L6 are CSS; hold the rules that fix them.
func TestDocsVisualStylesheetRules(t *testing.T) {
	css := docsStylesheet()
	for _, want := range []string{
		// M7: the measure moves from the block to its text children.
		`.docs-markdown>:is(p,ul,ol,dl,h1,h2,h3,h4,h5,h6,blockquote,pre,hr){max-width:72ch}`,
		`.docs-markdown>:is(.docs-diagram,.docs-table-scroll){max-width:calc(100% + 2rem)`,
		// M10: coarse pointers get 44px targets; checkboxes get a cell-sized hit label.
		`.docs-select-hit{display:grid;`,
		`.docs-nav-link,.docs-sort-link,`,
		`.docs-page-link{min-width:2.75rem;height:2.75rem}`,
		// M11: phones keep New folder and the folder menus, with an edge fade.
		`.docs-nav-section{display:flex;order:2;`,
		`.docs-folder-menu{display:block;position:static;opacity:1}`,
		`.docs-nav::after{content:"";order:3;position:sticky;`,
		`position-anchor:--docs-folder-menu`,
		// L5: the refresh bar moves by transform and stops under reduced motion.
		`@keyframes docs-progress{from{transform:translateX(0)}to{transform:translateX(150%)}}`,
		`.docs-table-wrap.is-refreshing::before{animation:none;`,
		// L6: the private stripe clears 3:1.
		`.docs-kind-private{--docs-kind:color-mix(in srgb,var(--muted) 75%,transparent)}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("docs stylesheet missing %q", want)
		}
	}
	// M7: the reading block itself is no longer capped.
	if regexp.MustCompile(`\.docs-markdown\{[^}]*max-width:(72|85)ch`).MatchString(css) {
		t.Error(".docs-markdown still caps figures and tables at a text measure")
	}
	// L1: no ad hoc rem font sizes in the library sheet.
	if bad := regexp.MustCompile(`font-size:\.[0-9]+rem`).FindAllString(docsLibraryStylesheet(), -1); len(bad) > 0 {
		t.Errorf("library stylesheet has ad hoc font sizes outside the type scale: %v", bad)
	}
	// L2: the legacy block no longer fights the library sheet.
	for _, stale := range []string{`max-width:85ch`, `.docs-create-actions{display:flex;justify-content:flex-start`, `.docs-comments,.docs-comment-compose{`, `background-position:-40% 0`} {
		if strings.Contains(css, stale) {
			t.Errorf("stale conflicting rule still present: %q", stale)
		}
	}
	// L5: the progress keyframes animate only transform.
	if regexp.MustCompile(`@keyframes docs-progress\{[^@]*background-position`).MatchString(css) {
		t.Error("docs-progress still animates background-position")
	}
}

// L6: the private stripe and icon colour clear the 3:1 non-text minimum
// against the row surface in both schemes (75% muted over the surface).
func TestDocsPrivateEdgeContrast(t *testing.T) {
	for _, scheme := range []struct{ muted, surface string }{{"#526171", "#ffffff"}, {"#aebdcb", "#131c26"}} {
		edge := blendThemeColor(scheme.muted, scheme.surface, .25)
		ratio, err := contrastRatio(edge, scheme.surface)
		if err != nil || ratio < 3 {
			t.Errorf("private edge %s on %s = %.2f:1, want >= 3 (%v)", edge, scheme.surface, ratio, err)
		}
	}
}

// M10: each row checkbox sits in a label that fills its cell, so the hit
// target is the cell, not the 16px box.
func TestDocsRowCheckboxesHaveCellSizedHitLabel(t *testing.T) {
	view := docsReviewView()
	markup := docsRender(t, docsTable(view, docsRouteOf(view), map[string]bool{}, func(DocumentSummary) bool { return false }, ui.Handler{}, false))
	hits := regexp.MustCompile(`<label class="docs-select-hit"><input [^>]*data-docs-action="select(-all)?"[^>]*type="checkbox"></label>`).FindAllString(markup, -1)
	if len(hits) != len(view.Documents)+1 {
		t.Fatalf("select hit labels = %d, want %d (header + rows): %s", len(hits), len(view.Documents)+1, markup)
	}
}
