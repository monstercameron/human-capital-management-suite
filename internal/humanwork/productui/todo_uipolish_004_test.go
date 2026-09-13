package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// UIPOLISH-004: give each page and component an explicit scroll owner.
//
// RED, measured live at 1440x900 on /workspace/app/people (this todo's own
// section preamble): ".main-scroll" was the one scroll region with no
// scrollbar tokens (scrollbar-width/color both "auto", a 15px native bar
// where ".data-table-scroll" and ".primary-nav" both already used
// var(--hcm-nav-scrollbar-thumb)/var(--hcm-nav-scrollbar-track)) and no
// role/tabIndex/aria-label at all, so it was keyboard-unreachable. Scroll
// chaining, body scroll leakage and sticky-header detachment did NOT
// reproduce (UXAUDIT-001/008/012 already hold those); this todo's own text
// also names ".action-launcher-dialog" as a fourth ad hoc scroll region with
// the same untokenized-scrollbar defect plus an unneeded horizontal axis.
//
// GREEN: the shell, navigation, page, table, drawer and overlay declare
// noncompeting scroll ownership; sticky headers and action bars stay inside
// their container; focus is never hidden; keyboard/wheel/touch/RTL
// horizontal scrolling works; scrollbar tokens are shared.
//
// REFACTOR: one scroll-region component (scroll_region.go's ScrollRegion)
// owns overflow wiring, shadows, restoration, reduced motion and accessible
// naming for every one of ScrollRegionSelectors. "Shell" scroll ownership --
// the sixth surface GREEN names -- is the pre-existing, unchanged
// `html,body,#app{...;overflow:hidden;...}` contract
// (render_test.go's TestShellOwnsViewportAndSeparatesNavigationFromContentScroll
// pins it): the shell owns declaring NO scroll of its own, deferring to the
// five ScrollRegionSelectors beneath it, so there is no sixth
// ScrollRegion-rendered element for it.

func TestTodo_UIPOLISH_004(t *testing.T) {
	t.Run("main-scroll gains the same scrollbar tokens data-table-scroll and primary-nav already used", func(t *testing.T) {
		css := Stylesheet()
		blocks := cssRuleBlocks(t, css, ".main-scroll")
		if !cssBlocksContain(blocks, "scrollbar-width:thin") || !cssBlocksContain(blocks, "scrollbar-color:var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)") {
			t.Fatalf(".main-scroll rule blocks are missing the shared scrollbar tokens: %v", blocks)
		}
	})

	t.Run("main-scroll's original sizing and overflow contract is untouched", func(t *testing.T) {
		css := Stylesheet()
		// UXAUDIT-001 pinned this exact block; the new scrollbar tokens above
		// live in a separate declaration (see scroll_region.go's own comment)
		// specifically so this byte-pinned prefix never has to change.
		if !strings.Contains(css, `.main-scroll{background-color:var(--canvas);height:100%;min-height:0;min-width:0;overflow-x:hidden;overflow-y:auto;`) {
			t.Fatal(".main-scroll's original sizing/overflow rule regressed")
		}
	})

	t.Run("overflow-x on the page shell is a deliberate, documented decision, not a default", func(t *testing.T) {
		// RED names "horizontal overflow is hidden" as a possible defect.
		// The deliberate decision this todo makes: keep it hidden, because
		// every region beneath the shell that can need horizontal scroll
		// already owns it -- ".data-table-scroll" (asserted below) and the
		// sticky ".data-table thead" horizontal scroll under 1050px
		// (dataTableStylesStylesheet). Nothing should ever ask the shell
		// itself to overflow sideways; if something someday does, this test
		// (not a live user) is where that assumption breaks first.
		css := Stylesheet()
		if !cssBlocksContain(cssRuleBlocks(t, css, ".main-scroll"), "overflow-x:hidden") {
			t.Fatal(".main-scroll no longer clips horizontal overflow -- the documented decision above needs revisiting, not silently dropping")
		}
		if !cssBlocksContain(cssRuleBlocks(t, css, ".data-table-scroll"), "overflow-x:auto") &&
			!cssBlocksContain(cssRuleBlocks(t, css, ".data-table-scroll"), "overflow:auto") {
			t.Fatal(".data-table-scroll no longer owns its own horizontal overflow -- main-scroll hiding horizontal overflow would then have nowhere for wide table content to go")
		}
	})

	t.Run("action-launcher-dialog: the fourth ad hoc region this todo names is narrowed to vertical-only and tokenized", func(t *testing.T) {
		css := Stylesheet()
		blocks := cssRuleBlocks(t, css, ".action-launcher-dialog")
		if !cssBlocksContain(blocks, "overflow-x:hidden") {
			t.Fatal(".action-launcher-dialog still offers horizontal scrolling its text content never needs")
		}
		if !cssBlocksContain(blocks, "overflow-y:auto") {
			t.Fatal(".action-launcher-dialog lost its vertical scroll")
		}
		if !cssBlocksContain(blocks, "scrollbar-width:thin") || !cssBlocksContain(blocks, "scrollbar-color:var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)") {
			t.Fatal(".action-launcher-dialog is still missing the shared scrollbar tokens")
		}
	})

	t.Run("ScrollRegionSelectors names exactly the shell's five rendered scroll owners", func(t *testing.T) {
		want := map[string]bool{".main-scroll": true, ".primary-nav": true, ".sidebar": true, ".data-table-scroll": true, ".action-launcher-dialog": true}
		if len(ScrollRegionSelectors) != len(want) {
			t.Fatalf("ScrollRegionSelectors = %v, want exactly %d entries", ScrollRegionSelectors, len(want))
		}
		for _, selector := range ScrollRegionSelectors {
			if !want[selector] {
				t.Fatalf("ScrollRegionSelectors contains unexpected selector %q", selector)
			}
		}
	})
}

// cssBlocksContain reports whether any block contains want as a substring.
// Used alongside cssRuleBlocks (uxaudit008_people_table_test.go) so a
// property assertion checks the selector's actual declared rule(s), not an
// unscoped grep of the whole sheet that some unrelated rule could satisfy.
func cssBlocksContain(blocks []string, want string) bool {
	for _, block := range blocks {
		if strings.Contains(block, want) {
			return true
		}
	}
	return false
}

// TestTodo_UIPOLISH_004_Browser is a static SSR/DOM assertion over the
// rendered document, following the precedent TestTodo_UXAUDIT_008_Browser's
// own comment records: this lane cannot rebuild or restart the dev server or
// run Playwright, so this proves what a browser would render structurally
// rather than what it paints.
func TestTodo_UIPOLISH_004_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.People = uxaudit008ManyPeople(5, 0)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	main := firstElement(root, "main")
	if main == nil || !hasAttr(main, "tabindex") || attr(main, "tabindex") != "0" {
		t.Fatal("page region (<main class=\"main-scroll\">) is not keyboard-focusable")
	}
	if attr(main, "aria-labelledby") == "" && attr(main, "aria-label") == "" {
		t.Fatal("page region has no accessible name")
	}

	primaryNav := findElementByID(root, "primary-nav")
	if primaryNav == nil || attr(primaryNav, "tabindex") != "0" || attr(primaryNav, "aria-label") == "" {
		t.Fatal("navigation region (#primary-nav) is not both keyboard-focusable and named")
	}

	drawer := findElementByID(root, "workspace-navigation")
	if drawer == nil || attr(drawer, "tabindex") != "0" || attr(drawer, "aria-label") == "" {
		t.Fatal("drawer region (#workspace-navigation) is not both keyboard-focusable and named")
	}

	table := findElementByID(root, "data-table-scroll")
	if table == nil || attr(table, "tabindex") != "0" || attr(table, "role") != "region" || attr(table, "aria-label") == "" {
		t.Fatal("table region (#data-table-scroll) lost its keyboard-focusable, named region contract")
	}

	overlay := findElementByID(root, "action-launcher-dialog")
	if overlay == nil || attr(overlay, "role") != "dialog" || attr(overlay, "aria-label") == "" {
		t.Fatal("overlay region (#action-launcher-dialog) lost its dialog contract")
	}
}

// TestTodo_UIPOLISH_004_Accessibility proves every scroll region that can
// actually scroll is keyboard-reachable and named -- derived from
// ScrollRegionSelectors (the same list scroll_region.go's stylesheet is
// built from), not a hardcoded, driftable copy of the selector names.
func TestTodo_UIPOLISH_004_Accessibility(t *testing.T) {
	view := testView(PagePeople)
	view.People = uxaudit008ManyPeople(5, 0)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	for _, selector := range ScrollRegionSelectors {
		class := strings.TrimPrefix(selector, ".")
		nodes := findElementsByClassContains(root, class)
		if len(nodes) == 0 {
			t.Fatalf("selector %q (from ScrollRegionSelectors) has no rendered element on the People page", selector)
		}
		for _, node := range nodes {
			// Keyboard reachability does not require tabIndex="0" on the
			// scroll region itself when the region already contains its own
			// focusable control: ".action-launcher-dialog" is a role="dialog"
			// combobox whose input receives focus (the standard ARIA dialog
			// pattern moves focus to a descendant, never doubles the dialog
			// itself as a tab stop) and already exposes a full keyboard model
			// (arrow keys move the active result) that predates this todo.
			// Every other adopted region has no such descendant and so must
			// carry tabIndex="0" directly to be reachable at all.
			if !hasFocusableDescendant(node) && attr(node, "tabindex") != "0" {
				t.Errorf("selector %q: element is neither focusable itself (tabindex=\"0\") nor does it contain a focusable control (element=%v)", selector, node)
			}
			if attr(node, "aria-label") == "" && attr(node, "aria-labelledby") == "" {
				t.Errorf("selector %q: element has no accessible name", selector)
			}
		}
	}
}

// hasFocusableDescendant reports whether node contains a native interactive
// element (excluding node itself) that keyboard focus can reach.
func hasFocusableDescendant(node *xhtml.Node) bool {
	focusable := map[string]bool{"a": true, "button": true, "input": true, "select": true, "textarea": true}
	var found bool
	var walk func(*xhtml.Node, bool)
	walk = func(n *xhtml.Node, isRoot bool) {
		if found {
			return
		}
		if !isRoot && n.Type == xhtml.ElementNode {
			if focusable[n.Data] || (hasAttr(n, "tabindex") && attr(n, "tabindex") != "-1") {
				found = true
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, false)
		}
	}
	walk(node, true)
	return found
}

// findElementsByClassContains returns every element whose class attribute's
// space-separated token list contains class exactly (not merely as a
// substring of a longer class name).
func findElementsByClassContains(node *xhtml.Node, class string) []*xhtml.Node {
	var matches []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, token := range strings.Fields(attr(n, "class")) {
				if token == class {
					matches = append(matches, n)
					break
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return matches
}

// TestTodo_UIPOLISH_004_Performance asserts structurally, not on wall-clock
// time (this repo's own guidance: two existing latency gates are already
// load-sensitive and a third failed a commit recently under concurrent
// covergate load). ScrollRegion adoption is shell-level chrome, rendered
// exactly once per page regardless of how many data rows that page holds;
// this proves that stays true (a bug that re-rendered the shell once per row
// would silently multiply every tabIndex/aria-label ScrollRegion adds) and
// that overall document growth stays roughly linear in row count, the same
// bound TestTodo_UXAUDIT_008_Performance already established for the table
// alone.
func TestTodo_UIPOLISH_004_Performance(t *testing.T) {
	population := uxaudit008ManyPeople(250, 3)

	view20 := testView(PagePeople)
	view20.People = population
	view20.PeoplePageSize = 20
	doc20, err := Render(view20)
	if err != nil {
		t.Fatal(err)
	}

	view100 := testView(PagePeople)
	view100.People = population
	view100.PeoplePageSize = 100
	doc100, err := Render(view100)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly one of each ScrollRegion-rendered shell region regardless of
	// row count: the People table's own row content grows, but the shell
	// chrome that wraps it (main/nav/sidebar/table-wrapper/launcher) does not
	// get re-instantiated per row.
	for _, doc := range []string{doc20, doc100} {
		if got := strings.Count(doc, `id="main-content"`); got != 1 {
			t.Fatalf("id=\"main-content\" occurrences = %d, want exactly 1", got)
		}
		if got := strings.Count(doc, `id="primary-nav"`); got != 1 {
			t.Fatalf("id=\"primary-nav\" occurrences = %d, want exactly 1", got)
		}
		if got := strings.Count(doc, `id="data-table-scroll"`); got != 1 {
			t.Fatalf("id=\"data-table-scroll\" occurrences = %d, want exactly 1", got)
		}
	}

	if len(doc20) == 0 {
		t.Fatal("page_size=20 document is empty")
	}
	if ratio := float64(len(doc100)) / float64(len(doc20)); ratio >= 8 {
		t.Fatalf("document byte length grew %.2fx for a 5x row-count increase (20->100 rows); want a roughly linear bound (<8x)", ratio)
	}
}

// TestTodo_UIPOLISH_004_Regression pins the scrollbar tokens and the
// UXAUDIT-008 max-height/overflow matched pair as rule-level invariants (the
// same technique uxaudit008_people_table_test.go's own "matched pair"
// subtest uses via cssRuleBlocks), not substring greps that would pass on a
// coincidental match elsewhere in a 200KB+ stylesheet. It also pins RTL
// edge placement and the pre-existing non-competing-scroll contracts this
// todo's RED explicitly says must not be rebuilt.
func TestTodo_UIPOLISH_004_Regression(t *testing.T) {
	css := Stylesheet()

	t.Run("every ScrollRegionSelectors entry that declares a scrolling overflow also declares both scrollbar tokens, in the same rule set", func(t *testing.T) {
		for _, selector := range ScrollRegionSelectors {
			blocks := cssRuleBlocks(t, css, selector)
			if len(blocks) == 0 {
				t.Fatalf("selector %q has no declared CSS rule at all", selector)
			}
			scrolls := cssBlocksContain(blocks, "overflow:auto") || cssBlocksContain(blocks, "overflow-y:auto") || cssBlocksContain(blocks, "overflow-x:auto")
			if !scrolls {
				continue
			}
			if !cssBlocksContain(blocks, "scrollbar-width:thin") {
				t.Errorf("selector %q declares a scrolling overflow but no scrollbar-width token across its own rule blocks", selector)
			}
			if !cssBlocksContain(blocks, "scrollbar-color:var(--hcm-nav-scrollbar-thumb)") {
				t.Errorf("selector %q declares a scrolling overflow but no themed scrollbar-color across its own rule blocks", selector)
			}
		}
	})

	t.Run("UXAUDIT-008's max-height/overflow matched pair still holds after consolidation", func(t *testing.T) {
		// Reproduces uxaudit008_people_table_test.go's own invariant against
		// the live sheet this todo also changed, so a future edit to either
		// file cannot silently reintroduce the exact broken pairing that
		// todo's own comment records (a bounded max-height reachable with no
		// scrolling overflow in the same block).
		blocks := cssRuleBlocks(t, css, ".people-directory .data-table-scroll")
		if len(blocks) == 0 {
			t.Fatal("no .people-directory .data-table-scroll rule found")
		}
		for _, block := range blocks {
			boundedMaxHeight := strings.Contains(block, "max-height:") && !strings.Contains(block, "max-height:none")
			if !boundedMaxHeight {
				continue
			}
			scrolls := strings.Contains(block, "overflow:auto") || strings.Contains(block, "overflow:scroll")
			visible := strings.Contains(block, "overflow:visible")
			if !scrolls || visible {
				t.Fatalf("found a bounded max-height with no scrolling overflow in the same block: %s", block)
			}
		}
	})

	t.Run("RTL: the document declares dir=rtl for ar, and this todo's own CSS additions use no physical left/right", func(t *testing.T) {
		view := ApplyLocale(testView(PagePeople), ResolveProductLocale("ar"))
		view.People = uxaudit008ManyPeople(5, 0)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, `dir="rtl"`) {
			t.Fatal("ar-locale document did not declare dir=\"rtl\" on its root element")
		}
		// scrollRegionStylesheet's own rules never position anything on the
		// inline (horizontal) axis at all -- top/bottom placement (the
		// scroll-shadow pseudo-elements) is block-axis and unaffected by
		// writing direction either way, so there is nothing here for RTL to
		// flip. Asserting that directly, rather than assuming it, is what
		// this todo's own instruction to "verify edge placement under RTL
		// rather than assuming" asks for: a bare "left:"/"right:" property
		// appearing here later would be the regression this guards against.
		regionCSS := scrollRegionStylesheet()
		for _, physical := range []string{"left:", "right:"} {
			if strings.Contains(regionCSS, physical) {
				t.Fatalf("scrollRegionStylesheet uses a physical %q property, which does not flip under RTL", physical)
			}
		}
	})

	t.Run("scroll chaining, sticky headers and body-scroll containment -- proven not to reproduce, and this todo did not touch them", func(t *testing.T) {
		// RED's "not reproduced" list, restated as regression pins so a
		// future change to this same file cannot silently reintroduce them
		// without a test noticing.
		for _, want := range []string{
			`.main-scroll{background-color:var(--canvas);height:100%;min-height:0;min-width:0;overflow-x:hidden;overflow-y:auto;`,
			`.data-table thead th{position:sticky;top:0;}`,
		} {
			if !strings.Contains(css, want) {
				t.Fatalf("pre-existing scroll-ownership contract regressed: %q", want)
			}
		}
		for _, selector := range []string{".main-scroll", ".data-table-scroll", ".primary-nav"} {
			blocks := cssRuleBlocks(t, css, selector)
			if !cssBlocksContain(blocks, "overscroll-behavior:contain") && !cssBlocksContain(blocks, "overscroll-behavior-y:contain") {
				t.Errorf("selector %q lost its overscroll-behavior containment", selector)
			}
		}
	})

	t.Run("scrollRestorationEnabled guards on both a real id and the caller's opt-in", func(t *testing.T) {
		// The pure predicate scroll_restoration_wasm.go's real hook and
		// scroll_restoration_native.go's no-op both key off of --
		// exercised here natively so this lane's own `go test` run covers
		// it, not only the wasm-only test file this lane cannot execute.
		for _, tc := range []struct {
			id      string
			enabled bool
			want    bool
		}{
			{"main-content", true, true},
			{"main-content", false, false},
			{"", true, false},
			{"  ", true, false},
			{"", false, false},
		} {
			if got := scrollRestorationEnabled(tc.id, tc.enabled); got != tc.want {
				t.Errorf("scrollRestorationEnabled(%q, %v) = %v, want %v", tc.id, tc.enabled, got, tc.want)
			}
		}
	})
}
