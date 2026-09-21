package productui

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for UXAUDIT-001: at 390px or 320px the live app shell rendered the
// expanded navigation in document flow, consuming most of the viewport
// before any page content; the collapsed state still constrained the page;
// the header wrapped unpredictably; and the navigation's own internal
// scroll competed with the page's. See the todo's RED clause and the
// section-69 preamble in planning/todos.md for the measured numbers.
//
// GREEN, proven here structurally (see the package doc comment on
// TestTodo_UXAUDIT_001_Browser for what is proven live instead):
//   - narrow viewports get an accessible overlay drawer, closed and
//     off-canvas by default at every viewport (fail-closed), reachable
//     only through a trigger CSS removes from hit-testing and the tab
//     order outside the narrow breakpoint;
//   - the header keeps every one of its children in one grid row at every
//     narrow width tested by the live audit;
//   - the content's own main-scroll is the only scroll surface a closed
//     drawer ever contributes to;
//   - collapsed/expanded desktop state never touches the narrow-width
//     layout, so a transition between them cannot move content width.
func TestTodo_UXAUDIT_001(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	// One navigation model: exactly one nav landmark, one primary-nav list,
	// one drawer trigger — never a second, mobile-only copy of any of them.
	if got := countElements(root, "aside"); got != 1 {
		t.Fatalf("aside landmarks = %d, want exactly 1 (no forked mobile nav)", got)
	}
	if got := strings.Count(doc, `id="workspace-navigation"`); got != 1 {
		t.Fatalf(`id="workspace-navigation" occurrences = %d, want exactly 1`, got)
	}
	if got := strings.Count(doc, `class="primary-nav"`); got != 1 {
		t.Fatalf(`primary-nav occurrences = %d, want exactly 1`, got)
	}
	trigger := findElementByID(root, "nav-drawer-trigger")
	if trigger == nil {
		t.Fatal("no narrow-viewport drawer trigger in the shell")
	}
	if xhtmlAttr(trigger, "aria-haspopup") != "dialog" || xhtmlAttr(trigger, "aria-controls") != "workspace-navigation" {
		t.Fatalf("drawer trigger is not wired to the nav dialog: %+v", trigger.Attr)
	}
	// Closed by default: the only state a fresh SSR document (or any render
	// before a real user interacts) can ever show.
	if xhtmlAttr(trigger, "aria-expanded") != "false" {
		t.Fatalf("drawer trigger aria-expanded = %q, want false by default", xhtmlAttr(trigger, "aria-expanded"))
	}
	backdrop := findClassNode(root, "nav-drawer-backdrop")
	if backdrop == nil {
		t.Fatal("no drawer backdrop in the shell")
	}
	if xhtmlAttr(backdrop, "aria-hidden") != "true" {
		t.Fatal("decorative drawer backdrop is not hidden from assistive technology")
	}

	// Still exactly one main content outlet, unowned by the drawer.
	if got := countElements(root, "main"); got != 1 {
		t.Fatalf("main landmarks = %d, want exactly 1", got)
	}

	css := Stylesheet()
	for _, want := range []string{
		// Off-canvas, hidden, fail-closed default at every narrow width.
		`.sidebar,.sidebar.collapsed{border-inline-end:1px solid var(--line);border-right:0;box-shadow:0 18px 48px color-mix(in srgb,var(--ink) 22%,transparent);display:flex;flex-direction:column;height:100dvh;inset-block:0;inset-inline-start:-336px;max-width:100%;overflow:hidden;`,
		`visibility:hidden;width:min(86vw,320px);z-index:55;}`,
		// Only the drawer's own Open state ever brings it on screen, and does
		// so with !important: see the cascade-contract check below for why.
		`.sidebar.nav-drawer-open,.sidebar.collapsed.nav-drawer-open{inset-inline-start:0!important;visibility:visible!important;}`,
		// The compact header keeps every child in one grid row at narrow widths.
		`@media (max-width:760px){.topbar,.app-shell.nav-collapsed .topbar{grid-template-columns:minmax(0,120px) minmax(0,1fr) auto auto auto;}`,
		`.topbar>.header-navigation-tools{flex:1 1 auto;flex-wrap:nowrap;grid-column:auto;grid-row:auto;min-width:0;overflow-x:auto;overscroll-behavior-inline:contain;padding:0;}`,
		// One trigger reachable at a time: the desktop icon-rail toggle and
		// the drawer trigger are mutually exclusive by viewport.
		`.header-nav-toggle,.app-shell.nav-collapsed .header-nav-toggle{display:none;}`,
		`.nav-drawer-trigger{display:none;}`,
		// A closed drawer contributes no second page-level scroller.
		`.primary-nav,.sidebar nav:first-of-type{flex:1;max-width:100%;min-width:0;overflow:hidden;width:100%;}`,
		`.sidebar.nav-drawer-open .primary-nav,.sidebar.nav-drawer-open nav:first-of-type{overflow-x:hidden;overflow-y:auto;`,
		// The backdrop restates display on the open rule (display:none from
		// the base rule wins over an override that never mentions display).
		`.nav-drawer-backdrop{display:none;}`,
		`.nav-drawer-backdrop.nav-drawer-open{background:color-mix(in srgb,var(--ink) 42%,transparent);display:block!important;inset:0;position:fixed;z-index:54;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("shell stylesheet missing %q", want)
		}
	}

	// Desktop and the >760px "tablet" tier are untouched: the collapse
	// transition still only ever changes .shell-grid's column widths, never
	// its narrow single-column shape, so it cannot move content that is
	// off-canvas anyway.
	if !strings.Contains(css, `.shell-grid,.app-shell.nav-collapsed .shell-grid{grid-template-columns:minmax(0,1fr);grid-template-rows:auto minmax(0,1fr);}`) {
		t.Fatal("narrow-width single-column content grid regressed")
	}

	// Cascade contract, not just text presence: a live-browser check found
	// that an earlier version of the open rule had every property a
	// substring match wants, in the right CSSOM position, at higher
	// specificity than the closed rule, and STILL lost the cascade for
	// reasons the browser never explained. Proving the rule's text exists
	// is not proving it wins. So: parse both declaration blocks and require
	// the open rule to actually restate — with !important, so it cannot
	// depend on specificity bookkeeping staying correct — every property
	// the closed rule sets that moving the drawer on screen requires it to
	// override. The backdrop gets the same treatment for display.
	closed := cssDeclBlock(t, css, `@media (max-width:760px){.sidebar,.sidebar.collapsed{`)
	open := cssDeclBlock(t, css, `@media (max-width:760px){.sidebar.nav-drawer-open,.sidebar.collapsed.nav-drawer-open{`)
	for _, property := range []string{"inset-inline-start", "visibility"} {
		closedValue, ok := closed[property]
		if !ok {
			t.Fatalf("closed sidebar rule does not set %q, so there is nothing for the open state to override", property)
		}
		openValue, ok := open[property]
		if !ok {
			t.Fatalf("open sidebar rule never restates %q — it cannot move the drawer on screen regardless of how specificity resolves", property)
		}
		if openValue == closedValue {
			t.Fatalf("open sidebar rule's %q (%q) is identical to the closed value; it does not actually override anything", property, openValue)
		}
		if !strings.HasSuffix(openValue, "!important") {
			t.Fatalf("open sidebar rule's %q = %q is not !important, so it depends on specificity bookkeeping never drifting — exactly the failure mode a live browser hit here", property, openValue)
		}
	}
	backdropOpen := cssDeclBlock(t, css, `@media (max-width:760px){.nav-drawer-backdrop.nav-drawer-open{`)
	display, ok := backdropOpen["display"]
	if !ok {
		t.Fatal("open backdrop rule never sets display — display:none from the base rule always wins, so the backdrop can never become visible")
	}
	if display == "none" || !strings.HasSuffix(display, "!important") {
		t.Fatalf("open backdrop display = %q, want a real, !important-guarded non-none value", display)
	}
}

// cssDeclBlock finds the exact rule prefix (an @media header plus the
// selector and opening brace, verbatim) and parses the declarations up to
// its closing brace into a property->value map. It fails the test outright
// if the prefix or its closing brace is missing, so a renamed or vanished
// rule is caught here with a clear message rather than as a confusing
// map-lookup failure in the caller.
//
// It takes the LAST match, not the first: the platform stylesheet
// concatenates many stylesheet functions in a fixed order (styles.go), so
// more than one function can legally declare the same selector under the
// same media condition (this package's own .sidebar,.sidebar.collapsed
// does, three times, for three unrelated concerns) — the one that governs
// the cascade is whichever was emitted last.
func cssDeclBlock(t *testing.T, css, prefix string) map[string]string {
	t.Helper()
	start := strings.LastIndex(css, prefix)
	if start < 0 {
		t.Fatalf("stylesheet has no rule starting with %q", prefix)
	}
	bodyStart := start + len(prefix)
	end := strings.Index(css[bodyStart:], "}")
	if end < 0 {
		t.Fatalf("rule %q has no closing brace", prefix)
	}
	body := css[bodyStart : bodyStart+end]
	decls := map[string]string{}
	for _, decl := range strings.Split(body, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		property, value, found := strings.Cut(decl, ":")
		if !found {
			t.Fatalf("rule %q has a malformed declaration %q", prefix, decl)
		}
		decls[property] = value
	}
	return decls
}

// TestTodo_UXAUDIT_001_Browser exercises the shell across several distinct
// pages to prove the SAME drawer, trigger and header wiring serves every
// route — there is one responsive shell, not a per-page mobile variant.
//
// This test is structural: it renders server-side markup and CSS the same
// way every other test in this package does, which is what the traceability
// gate consumes. It does not measure the live, WASM-hydrated, real-browser
// layout the todo's section preamble requires as proof of completion. That
// evidence is tools/uxqual/browser/uxaudit001_mobile_shell.spec.mjs, which
// asserts the actual 320px/375px measurements (one page-level scroll
// region, main starting within the top ~15% of the viewport, a single-row
// header, focus containment and restoration) against the real dev server;
// per instruction it is not run here, and the operator runs it directly.
func TestTodo_UXAUDIT_001_Browser(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PagePerson, PageWork, PageSettings} {
		view := testView(page)
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("page %s: %v", page, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("page %s: %v", page, err)
		}
		trigger := findElementByID(root, "nav-drawer-trigger")
		if trigger == nil {
			t.Fatalf("page %s lost its drawer trigger", page)
		}
		if xhtmlAttr(trigger, "aria-controls") != "workspace-navigation" {
			t.Fatalf("page %s drawer trigger is not wired to the shared nav", page)
		}
		if findClassNode(root, "nav-drawer-backdrop") == nil {
			t.Fatalf("page %s lost its drawer backdrop", page)
		}
		nav := findElementByID(root, "workspace-navigation")
		if nav == nil || xhtmlAttr(nav, "aria-label") != "Workspace navigation" {
			t.Fatalf("page %s did not render the one shared navigation landmark", page)
		}
		main := firstElement(root, "main")
		if main == nil || xhtmlAttr(main, "id") != "main-content" {
			t.Fatalf("page %s content outlet is not the stable main-content region", page)
		}
	}
}

// TestTodo_UXAUDIT_001_Accessibility proves the drawer's actual semantics,
// not merely that some attribute exists: a dialog role with an accessible
// name that only ever appears while the drawer is genuinely open, live
// aria-expanded on the trigger, and Escape-to-close wired through the same
// drawerEscapeCloses predicate every other shell dialog uses (landmarks.go).
//
// Focus containment and restoration (drawer_focus_wasm.go) run only under
// js/wasm — that is what a browser tab actually executes — and are proven
// by TestDrawerFocusTrapWrapsTabAndRestoresFocusOnClose in
// drawer_focus_wasm_test.go, which this package's native `go test` build
// does not compile or run. This test proves instead that NavigationSidebar
// actually calls that hook (source-level wiring), since a native SSR
// render can only ever show the fail-closed default and cannot exercise a
// live focus trap.
func TestTodo_UXAUDIT_001_Accessibility(t *testing.T) {
	// The trigger's disclosure contract: haspopup, a real accessible name,
	// and aria-expanded that actually flips with state.
	closedAria := navigationDrawerTriggerAria(false, "Open navigation menu")
	if closedAria["haspopup"] != "dialog" || closedAria["controls"] != "workspace-navigation" {
		t.Fatalf("closed trigger aria = %+v, want a dialog-disclosure contract", closedAria)
	}
	if closedAria["expanded"] != "false" || closedAria["label"] != "Open navigation menu" {
		t.Fatalf("closed trigger aria = %+v", closedAria)
	}
	openAria := navigationDrawerTriggerAria(true, "Close navigation menu")
	if openAria["expanded"] != "true" || openAria["label"] != "Close navigation menu" {
		t.Fatalf("open trigger aria = %+v, want expanded=true and the close label", openAria)
	}

	// The dialog semantics themselves: absent while closed (so the default,
	// always-SSR-visible render keeps the plain complementary landmark
	// WEB-048 pins), present with an aria-modal flag while open.
	if attrs := navigationDrawerDialogAttrs(false); attrs != nil {
		t.Fatalf("closed dialog attrs = %+v, want none", attrs)
	}
	openAttrs := navigationDrawerDialogAttrs(true)
	if openAttrs["aria-modal"] != "true" {
		t.Fatalf("open dialog attrs = %+v, want aria-modal true", openAttrs)
	}

	// Render the real component with Open forced true (the same pattern
	// web039_navigation_performance_test.go already uses to exercise
	// NavigationSidebar directly) to prove the dialog role and accessible
	// name are wired end to end, not just returned by the helper above.
	openProps := navigationDrawerSidebarProps(testView(PageHome), true, func() {}, func() {})
	openDoc, err := ui.RenderToString(ui.CreateElement(NavigationSidebar, openProps))
	if err != nil {
		t.Fatal(err)
	}
	openRoot, err := xhtml.Parse(strings.NewReader(openDoc))
	if err != nil {
		t.Fatal(err)
	}
	nav := findElementByID(openRoot, "workspace-navigation")
	if nav == nil {
		t.Fatal("open drawer render lost the navigation landmark")
	}
	if xhtmlAttr(nav, "role") != "dialog" || xhtmlAttr(nav, "aria-modal") != "true" {
		t.Fatalf("open drawer = role %q aria-modal %q, want a real dialog", xhtmlAttr(nav, "role"), xhtmlAttr(nav, "aria-modal"))
	}
	if xhtmlAttr(nav, "aria-label") == "" {
		t.Fatal("open drawer dialog has no accessible name")
	}
	if !strings.Contains(xhtmlAttr(nav, "class"), "nav-drawer-open") {
		t.Fatalf("open drawer class = %q, missing the open visual state", xhtmlAttr(nav, "class"))
	}
	backdrop := findClassNode(openRoot, "nav-drawer-backdrop")
	if backdrop == nil || !strings.Contains(xhtmlAttr(backdrop, "class"), "nav-drawer-open") {
		t.Fatal("open drawer did not also open its backdrop")
	}

	// Escape-to-close is the shell's one predicate (landmarks.go), reused
	// here rather than a second, drifting keyboard contract.
	if !drawerEscapeCloses("Escape") || drawerEscapeCloses("Enter") || drawerEscapeCloses("Tab") {
		t.Fatal("drawer escape handling does not match the shell dialog contract")
	}
	source, err := os.ReadFile("navigation_components.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if !strings.Contains(body, "drawerEscapeCloses(event.GetKey())") {
		t.Fatal("NavigationSidebar does not wire its keydown handler through the shared drawerEscapeCloses predicate")
	}
	if !strings.Contains(body, "useDrawerFocusTrap(") {
		t.Fatal("NavigationSidebar does not call the drawer focus-containment hook")
	}
}

// TestTodo_UXAUDIT_001_Performance asserts the overlay drawer's open/close
// transition respects prefers-reduced-motion, the same pattern already
// used by action_launcher.go and focus.go: a real transition exists, and a
// dedicated media rule turns it off rather than leaving it to inherit
// whatever the reduced-motion default happens to be.
func TestTodo_UXAUDIT_001_Performance(t *testing.T) {
	css := Stylesheet()
	// The slide is one simple, single-property transition declared once on
	// the closed rule; the open rule only needs to override
	// inset-inline-start and visibility (proven by the cascade contract in
	// TestTodo_UXAUDIT_001), so this same declaration governs both
	// directions without a fragile multi-property, multi-delay shorthand.
	if !strings.Contains(css, `transition:inset-inline-start .22s ease`) {
		t.Fatal("the drawer has no real transition to respect prefers-reduced-motion for")
	}
	if !strings.Contains(css, `prefers-reduced-motion:reduce){.sidebar,.sidebar.collapsed{transition:none;}`) {
		t.Fatal("drawer transition does not honor prefers-reduced-motion")
	}
}

// TestTodo_UXAUDIT_001_Regression pins what the audit already found
// correct and must not break: no horizontal overflow at 320px, and the
// desktop (>=1191px) shell layout is untouched by the narrow-width drawer
// work.
func TestTodo_UXAUDIT_001_Regression(t *testing.T) {
	css := Stylesheet()

	// Desktop layout: the unscoped base rules this todo never touched.
	for _, want := range []string{
		`.topbar{align-items:center;background-color:var(--surface);border-bottom:1px solid var(--line);display:grid;gap:14px;grid-template-columns:232px minmax(220px,1fr) auto auto auto;min-height:81px;padding-inline-end:24px;position:sticky;top:0;z-index:20;}`,
		`.shell-grid{display:grid;grid-template-columns:232px minmax(0,1fr);min-height:calc(100vh - 81px);}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("desktop shell layout regressed: missing %q", want)
		}
	}

	// The drawer's fixed, off-canvas positioning is scoped to the narrow
	// breakpoint only — it must never leak into an unscoped, always-active
	// rule that would also apply on desktop.
	drawerRuleStart := strings.Index(css, `@media (max-width:760px){.sidebar,.sidebar.collapsed{border-inline-end:`)
	if drawerRuleStart < 0 {
		t.Fatal("narrow-width drawer rule not found")
	}
	drawerRuleEnd := strings.Index(css[drawerRuleStart:], "}}")
	if drawerRuleEnd < 0 {
		t.Fatal("narrow-width drawer rule has no closing media block")
	}
	drawerRule := css[drawerRuleStart : drawerRuleStart+drawerRuleEnd+2]
	if !strings.Contains(drawerRule, "position:fixed") {
		t.Fatalf("narrow-width drawer rule lost its fixed positioning: %s", drawerRule)
	}

	// No horizontal overflow at 320px: the pre-existing safety net for the
	// whole shell, and the drawer's own bound (it can never exceed the
	// viewport width it is fixed against).
	for _, want := range []string{
		`html,body,.app-shell,.shell-grid,.sidebar,.main{max-width:100%;min-width:0;}`,
		`width:min(86vw,320px)`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("320px overflow safety net regressed: missing %q", want)
		}
	}

	// The collapse/expand transition (desktop and the 761-1190px tier)
	// still only ever animates grid-template-columns and sidebar width —
	// exactly what it did before this todo, never anything narrow-width.
	if !strings.Contains(css, `.shell-grid{grid-template-columns:232px minmax(0,1fr);transition:grid-template-columns 0.18s ease;}`) {
		t.Fatal("desktop collapse/expand transition regressed")
	}
}
