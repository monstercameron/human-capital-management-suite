package productui

import (
	"strings"
	"testing"
)

// declarationsFor joins the bodies of every top-level rule whose selector
// list includes selector, so a style split across several declareGlobal calls
// -- or shared with :hover in one list -- is gathered back together. Rules
// inside @media are skipped: this reads what applies by default.
func declarationsFor(css, selector string) string {
	var bodies []string
	for chunk := range strings.SplitSeq(css, "}") {
		open := strings.Index(chunk, "{")
		if open < 0 || strings.Contains(chunk[:open], "@") {
			continue
		}
		for _, candidate := range splitSelectorList(chunk[:open]) {
			if strings.TrimSpace(candidate) == selector {
				bodies = append(bodies, chunk[open+1:])
				break
			}
		}
	}
	return strings.Join(bodies, ";")
}

// splitSelectorList splits a selector list at its top-level commas only, so
// the arguments of :is(), :where() and :not() stay with their selector.
func splitSelectorList(list string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range list {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, list[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, list[start:])
}

// TestActionLauncherDrawsSelectionLikeGlobalSearch: the launcher sits beside
// global search in the same bar and both are comboboxes over a list of
// results. The launcher drew its active row with a full accent outline, which
// no other menu in the shell does; it now uses the soft fill, faint accent
// edge and leading accent bar that global search and the navigation use,
// mirrored for right-to-left.
func TestActionLauncherDrawsSelectionLikeGlobalSearch(t *testing.T) {
	css := Stylesheet()
	search := declarationsFor(css, ".global-search-result.active")
	launcher := declarationsFor(css, ".action-launcher-result.active")
	for _, declaration := range []string{
		"box-shadow:inset 3px 0 0 0 var(--accent)",
		"background-color:var(--soft)",
		"border-color:color-mix(in srgb,var(--accent) 20%,transparent)",
	} {
		if !strings.Contains(launcher, declaration) {
			t.Errorf("the launcher's active row lacks %q; it has: %s", declaration, launcher)
		}
	}
	if !strings.Contains(search, "box-shadow:inset 3px 0 0 0 var(--accent)") {
		t.Fatalf("global search no longer draws the leading bar this test compares against: %s", search)
	}
	if strings.Contains(launcher, "border-color:var(--accent)") {
		t.Error("the launcher's active row is still outlined in the accent colour")
	}
	if rtl := declarationsFor(css, "[dir=rtl] .action-launcher-result.active"); !strings.Contains(rtl, "inset -3px 0 0 0 var(--accent)") {
		t.Errorf("in right-to-left the active bar is not on the leading side: %s", rtl)
	}
}

// TestActionLauncherListHasNoSecondFrame: the result list is a
// popover-surface, whose border, radius and shadow framed a card inside the
// dialog's own frame. The reset has to outrank the shared popover sheet,
// which is emitted later, so it is scoped under the dialog.
func TestActionLauncherListHasNoSecondFrame(t *testing.T) {
	reset := declarationsFor(Stylesheet(), ".action-launcher-dialog .action-launcher-panel")
	for _, declaration := range []string{"border:0", "box-shadow:none", "background:transparent"} {
		if !strings.Contains(reset, declaration) {
			t.Errorf("the launcher's result list keeps its popover frame: %q missing from %q", declaration, reset)
		}
	}
}

// TestActionLauncherFieldFocusesLikeGlobalSearch: the launcher's search field
// sits under the header search field and has to show focus the same way --
// the accent border and focus halo, not the generic control outline, which
// drew a dark ring offset from its border. Forced colours keep a system
// outline, since the halo is a box-shadow and is dropped there.
func TestActionLauncherFieldFocusesLikeGlobalSearch(t *testing.T) {
	css := Stylesheet()
	search := declarationsFor(css, ".global-search-input:focus")
	field := declarationsFor(css, ".action-launcher-input:focus")
	for _, declaration := range []string{"border-color:var(--accent)", "box-shadow:var(--hcm-focus-ring)", "outline:0"} {
		if !strings.Contains(search, declaration) {
			t.Fatalf("global search no longer focuses with %q; this test compares against it: %s", declaration, search)
		}
		if !strings.Contains(field, declaration) {
			t.Errorf("the launcher field does not focus like global search: %q missing from %s", declaration, field)
		}
	}
	if !strings.Contains(css, "@media (forced-colors:active){.action-launcher-input:focus{") {
		t.Error("the launcher field has no forced-colours focus indicator once its halo is dropped")
	}
}

// TestTextFieldsShareOneFocusStyle: every text field in the shell focuses
// with the accent border and halo, and a transparent outline that forced
// colours can paint. Its selector has to outrank the generic control ring,
// which would otherwise draw a dark outline off the field's border.
func TestTextFieldsShareOneFocusStyle(t *testing.T) {
	field := declarationsFor(Stylesheet(), textFieldFocusSelector)
	for _, declaration := range []string{"border-color:var(--accent)", "box-shadow:var(--hcm-focus-ring)", "outline:2px solid transparent"} {
		if !strings.Contains(field, declaration) {
			t.Errorf("text fields do not share the field focus style: %q missing from %s", declaration, field)
		}
	}
	for _, kept := range []string{"[type=checkbox]", "[type=radio]", "[type=submit]"} {
		if !strings.Contains(textFieldFocusSelector, kept) {
			t.Errorf("%s would lose the control ring it needs; the field rule must exclude it", kept)
		}
	}
}

// TestButtonResetKeepsTheButtonTypeSize: the shared reset that gives buttons
// the app's typeface must not reset their size or weight. With the font
// shorthand it did, at the same specificity as .button and later, so each
// button took its surroundings' text size.
func TestButtonResetKeepsTheButtonTypeSize(t *testing.T) {
	reset := declarationsFor(Stylesheet(), ":where(.app-shell,.jn-embedded) :is(.button,.jn-btn)")
	if !strings.Contains(reset, "font-family:inherit") || strings.Contains(reset, "font:inherit") {
		t.Fatalf("the button reset should set only the family: %s", reset)
	}
	if button := declarationsFor(Stylesheet(), ".button"); !strings.Contains(button, "font-size:0.875rem") {
		t.Fatalf(".button no longer declares the button type size this reset protects: %s", button)
	}
}

// TestHeadingScaleIsAnElementDefault: the type scale's bare h2..h6 sizes are
// zero-specificity defaults, so an element-level heading rule (the journey
// renderer's) can set its own size. Joined in one :is() with the role class,
// the element arm scored (0,1,0) and overrode them.
func TestHeadingScaleIsAnElementDefault(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{
		":where(.app-shell,.jn-embedded) :where(h2)",
		":where(.app-shell,.jn-embedded) :where(h3,h4,h5,h6)",
	} {
		if !strings.Contains(declarationsFor(css, selector), "font-size:") {
			t.Errorf("no zero-specificity heading default for %s", selector)
		}
	}
	if strings.Contains(css, ":is(h2,[data-type-role=") {
		t.Error("the h2 size is still joined to the role class inside :is(), which lifts it to class specificity")
	}
}

// TestTabsKeepOneWeight: the active tab is marked by colour and underline,
// not by turning bold, which widened it and shifted the tabs after it.
func TestTabsKeepOneWeight(t *testing.T) {
	css := Stylesheet()
	if active := declarationsFor(css, ".tab.active"); strings.Contains(active, "font-weight") {
		t.Fatalf("the active tab changes weight, so the strip reflows on selection: %s", active)
	}
	if count := declarationsFor(css, ".work-tab-count"); !strings.Contains(count, "border-radius:var(--hcm-radius-status)") {
		t.Fatalf("a tab's count is not drawn as the shell's count pill: %s", count)
	}
}

// TestRolesPageMatchesTheShell: the roles page's disclosures use the shell
// chevron (no browser triangle), its table header is sentence case like every
// other data table, and an employee's avatar sits beside their name.
func TestRolesPageMatchesTheShell(t *testing.T) {
	css := Stylesheet()
	if marker := declarationsFor(css, roleDisclosureSummaries); !strings.Contains(marker, "list-style:none") {
		t.Errorf("roles disclosures keep the browser marker: %s", marker)
	}
	if chevron := declarationsFor(css, roleDisclosureSummaries+"::after"); !strings.Contains(chevron, "rotate(45deg)") {
		t.Errorf("roles disclosures have no chevron: %s", chevron)
	}
	if head := declarationsFor(css, ".role-page-table thead th"); strings.Contains(head, "uppercase") {
		t.Errorf("the roles table header is still uppercase: %s", head)
	}
	if identity := declarationsFor(css, ".employee-role-identity"); !strings.Contains(identity, "grid-template-columns:auto minmax(0,1fr)") {
		t.Errorf("an employee's avatar is not beside their name: %s", identity)
	}
}

// TestLabelHeadingsSitBelowSectionHeadings: h3..h6 default to one fixed
// label step and a weight below the 700 of h1/h2, so a card's h3 is never
// bolder than the h2 above it or sized between two steps of the scale.
func TestLabelHeadingsSitBelowSectionHeadings(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, "--hcm-type-label:1rem;") {
		t.Error("the label heading size is not one fixed step")
	}
	if weight := declarationsFor(css, ":where(.app-shell,.jn-embedded) :is(h3,h4,h5,h6)"); !strings.Contains(weight, "font-weight:600") {
		t.Errorf("label headings are not a weight below section headings: %s", weight)
	}
}

// TestCardDescriptionsSitUnderTheirTitles: a card's description is one step
// (14px) under its 18px title and under the page's 16px subtitle; at 16px it
// matched the subtitle and read as a second title line.
func TestCardDescriptionsSitUnderTheirTitles(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{".section-head p", ".empty-state>h2+p"} {
		if got := declarationsFor(css, selector); !strings.Contains(got, "font-size:0.875rem") {
			t.Errorf("%s is not at the description size: %s", selector, got)
		}
	}
}

// TestFocusRingsUseTheFocusTokens: every keyboard focus ring is drawn in the
// theme's focus color at its ring width, so a customer's (or high-contrast)
// focus color reaches every control. Some were hard-coded in the brand accent.
func TestFocusRingsUseTheFocusTokens(t *testing.T) {
	for chunk := range strings.SplitSeq(Stylesheet(), "}") {
		open := strings.Index(chunk, "{")
		if open < 0 || !strings.Contains(chunk[:open], ":focus-visible") {
			continue
		}
		if body := chunk[open+1:]; strings.Contains(body, "outline:2px solid var(--accent)") {
			t.Errorf("%s draws its focus ring in the accent, not the focus token", chunk[:open])
		}
	}
}
