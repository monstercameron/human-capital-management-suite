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
		for candidate := range strings.SplitSeq(chunk[:open], ",") {
			if strings.TrimSpace(candidate) == selector {
				bodies = append(bodies, chunk[open+1:])
				break
			}
		}
	}
	return strings.Join(bodies, ";")
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
