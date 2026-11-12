package productui

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RenderedDocument checks parse production server-rendered DOM. They cover
// document semantics without claiming browser interaction or painted output.
func TestTodo_UIPOLISH_002_RenderedDocument(t *testing.T) {
	view := testView(PageHome)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	grid := findElementsByClassContains(root, "home-grid")
	if len(grid) != 1 {
		t.Fatalf("home has %d home grids, want one", len(grid))
	}
	for _, class := range []string{"home-primary-rail", "home-supporting-rail"} {
		if len(findElementsByClassContains(grid[0], class)) != 1 {
			t.Errorf("home grid does not render exactly one %s rail", class)
		}
	}
	css := Stylesheet()
	if !strings.Contains(css, `:root[data-hcm-density="compact"] .home-grid`) || !strings.Contains(css, `:root[data-hcm-density="spacious"] .home-grid`) {
		t.Fatal("browser document's home layout has no compact and spacious density rules")
	}
}

func TestTodo_UIPOLISH_002_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageHome), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s render: %v", locale, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		main := firstElement(root, "main")
		h1 := firstElement(root, "h1")
		if main == nil || h1 == nil {
			t.Fatalf("%s home lost main landmark or page heading", locale)
		}
		if !strings.Contains(doc, `lang="`+view.Locale.Resolved+`"`) || !strings.Contains(doc, `dir="`+string(view.Locale.Direction)+`"`) {
			t.Errorf("%s page spacing composition lost locale reading direction", locale)
		}
	}
}

func TestTodo_UIPOLISH_002_Golden(t *testing.T) {
	css := Stylesheet()
	for _, exact := range []string{
		`@media (min-width:761px){:root[data-hcm-density="compact"] .home-grid,:root[data-hcm-density="compact"] .side-stack,:root[data-hcm-density="compact"] .workbench,:root[data-hcm-density="compact"] .insights-grid{gap:15px;}}`,
		`@media (min-width:761px){:root[data-hcm-density="spacious"] .home-grid,:root[data-hcm-density="spacious"] .side-stack,:root[data-hcm-density="spacious"] .workbench,:root[data-hcm-density="spacious"] .insights-grid{gap:25px;}}`,
	} {
		if !strings.Contains(css, exact) {
			t.Errorf("density golden rule changed or disappeared: %s", exact)
		}
	}
}

func TestTodo_UIPOLISH_006_RenderedDocument(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PageSettings} {
		doc, err := Render(testView(page))
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		walkElements(root, func(n *xhtml.Node) {
			if n.Type != xhtml.ElementNode || (n.Data != "button" && n.Data != "input" && n.Data != "select" && n.Data != "textarea") {
				return
			}
			if attr(n, "disabled") == "" && attr(n, "aria-label") == "" && attr(n, "aria-labelledby") == "" && strings.TrimSpace(nodeText(n)) == "" && n.Data != "input" {
				t.Errorf("%s has an unlabeled interactive %s", page, n.Data)
			}
		})
	}
	css := Stylesheet()
	if !strings.Contains(css, `:focus-visible`) || !strings.Contains(css, `:disabled`) {
		t.Fatal("production controls omit visible focus or disabled state styling")
	}
}

func TestTodo_UIPOLISH_006_Performance(t *testing.T) {
	// The workspace serves two sheets (chatsheet_split.go): the control sheet
	// of the shell and every page, and Chat's own. Each has the same bound.
	css := ControlStylesheet()
	if len(css) > 1_000_000 {
		t.Fatalf("production control stylesheet grew to %d bytes", len(css))
	}
	if chat := ChatStylesheet(); len(chat) > 1_000_000 {
		t.Fatalf("production Chat stylesheet grew to %d bytes", len(chat))
	}
	if strings.Contains(css, "transition:all") || strings.Contains(css, "transition: all") {
		t.Fatal("control styles use an unbounded transition")
	}
	if strings.Count(css, ":focus-visible") == 0 {
		t.Fatal("shared control stylesheet has no keyboard focus treatment")
	}
}

func TestTodo_UIPOLISH_006_Regression(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, ":is(button,input,select,textarea)") {
		t.Fatal("shared target selector no longer includes all native controls")
	}
	if !strings.Contains(css, "--hcm-control-height:44px") || !strings.Contains(css, "--hcm-control-height-compact:44px") {
		t.Fatal("base or compact control target fell below 44px")
	}
	if !strings.Contains(css, ".button.primary") || !strings.Contains(css, "color:var(--on-brand)") {
		t.Fatal("primary action lost its semantic foreground")
	}
}

func TestTodo_UIPOLISH_007_RenderedDocument(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageWorkflowHistory), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if htmlEl := firstElement(root, "html"); htmlEl == nil || attr(htmlEl, "lang") != locale {
			t.Errorf("rendered browser document has wrong lang for %s", locale)
		}
		if strings.Contains(doc, "⟦") {
			t.Errorf("%s rendered unresolved product message key", locale)
		}
	}
}

func TestTodo_UIPOLISH_008_RenderedDocument(t *testing.T) {
	view := testView(PageWorkflowHistory)
	view.Appearance = DefaultCustomerTheme()
	view.Appearance.Density = "compact"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(findElementsByClassContains(root, "workflow-history")) != 1 {
		t.Fatal("production History document does not render exactly one history region")
	}
	if !strings.Contains(doc, `data-hcm-density="compact"`) {
		t.Fatal("production document did not apply persisted compact density")
	}
}

func TestTodo_UIPOLISH_008_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageWorkflowHistory), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range findElementsByClassContains(root, "data-table") {
			walkElements(table, func(n *xhtml.Node) {
				if n.Type == xhtml.ElementNode && n.Data == "th" && strings.TrimSpace(nodeText(n)) == "" {
					t.Errorf("%s table has an unnamed column heading", locale)
				}
			})
		}
		if !strings.Contains(doc, `dir="`+string(view.Locale.Direction)+`"`) {
			t.Errorf("%s table lost locale direction", locale)
		}
	}
}

func TestTodo_UIPOLISH_008_I18N(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageWorkflowHistory), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for _, key := range []string{"page.workflow_history.title", "workflow_history.search", "workflow_history.person"} {
			message := view.Locale.Text(key)
			if message == "" || strings.Contains(message, "⟦") || !strings.Contains(doc, message) {
				t.Errorf("%s history omits translated %s = %q", locale, key, message)
			}
		}
	}
}

func TestTodo_UIPOLISH_008_Performance(t *testing.T) {
	css := uipolish008TableStylesheet()
	if len(css) > 100_000 {
		t.Fatalf("responsive table stylesheet is unexpectedly large: %d bytes", len(css))
	}
	if strings.Contains(css, "animation:") || strings.Contains(css, "transition:all") {
		t.Fatal("density rules animate layout or add unbounded transitions")
	}
	for _, density := range []string{"compact", "comfortable", "spacious"} {
		if _, ok := CustomerThemeAttributes(CustomerTheme{Density: density})["data-hcm-density"]; !ok {
			t.Errorf("%s has no root density attribute", density)
		}
	}
}

func TestTodo_UIPOLISH_008_Regression(t *testing.T) {
	css := uipolish008TableStylesheet()
	if strings.Contains(css, "display:none") || strings.Contains(css, "order:") {
		t.Fatal("responsive density changed visibility or reading order")
	}
	if !strings.Contains(Stylesheet(), ".data-table-scroll") {
		t.Fatal("production table no longer retains its horizontal scroll owner")
	}
	if !strings.Contains(css, "overflow-wrap:anywhere") {
		t.Fatal("narrow cells can again force content beyond the viewport")
	}
}

func TestTodo_UIPOLISH_012_RenderedDocument(t *testing.T) {
	for _, definition := range PageDefinitions() {
		doc, err := Render(testView(definition.ID))
		if err != nil {
			t.Fatalf("%s: %v", definition.ID, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("%s: %v", definition.ID, err)
		}
		if firstElement(root, "main") == nil || firstElement(root, "h1") == nil {
			t.Errorf("%s browser document lacks main or page heading", definition.ID)
		}
		if !strings.Contains(doc, `data-hcm-catalog="product-ui.v1"`) {
			t.Errorf("%s bypasses production UI catalog", definition.ID)
		}
	}
}

func TestTodo_UIPOLISH_012_Golden(t *testing.T) {
	view := testView(PageHome)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(doc)))
	// UXBLIND blind-test lanes (Home, top bar, scroll/loading, brand) re-pin: whole-document digest.
	// The agent surfaces add their scoped stylesheet and the shell registry
	// publishes Agent setup and Agent operations. Green2 re-pins the whole
	// document after the shared agent page frame took ownership of the outer
	// width/gutter and shell-wide frame, dark-control, and link-state rules.
	// 2026-10-02 (W7) re-pin. The pinned a1f82bf0... did not reproduce at the
	// parent commit 8a8673502 either (that tree renders 5a9a9e8e...). Compared
	// with the parent, the markup outside the inline stylesheet is byte-identical;
	// every difference is in the stylesheet, which is the product stylesheet the
	// document inlines: 1534 rules added and 191 replaced or removed, from the
	// Chat features (chatcmd002/003, chatlang, chatmod, chatremove, chatvoice, the
	// chatux037 loading frames, chatsave, details and manage rows), the agent pages
	// (persona-admin, agent-announcement, agent-history, agent-cost, agents-task
	// and agent-operations rules) and the Projects dialog rules still being edited
	// by another lane. The rendered documents of both trees are kept under
	// .artifacts/tmp/W7; this digest moves with every stylesheet edit, so a later
	// lane re-pins it from the copy a failing run writes.
	// 2026-10-02 (Z4) re-pin after the chat lanes' stylesheet edits (the saved
	// tick's duration from the motion tokens, the open-row and bidi rules, the
	// agents page grid); the markup outside the stylesheet did not change.
	// 2026-10-22 (AGENTUX-019 F15) re-pin: the access-page, spend-limit and
	// announcement editor inputs moved from the canvas token to the surface
	// token with explicit :disabled canvas rules. Verified the rendered Home
	// document carries exactly that delta: the three old rules are absent,
	// the three new rules and three disabled rules are present once each,
	// and the Home document contains no badge markup, no mention-empty
	// markup and none of the reworded mention copy.
	// 2026-10-22 (CHATUX-049) re-pin: every chat focus ring moved to the
	// focus token (filter, language admin, map sheet/card, live banner,
	// removal, render settings, section rows, danger action, saved views,
	// image viewer tools/steps, attachment opener), message rows lost their
	// negative inline margins and cap at the column so a hovered row shares
	// the composer's edges, the removed row follows, and the row draft mark
	// became a text tag. Verified the rendered Home document carries exactly
	// that delta: each new rule present, each old rule absent, zero
	// brand-primary/danger/currentColor focus rings left.
	// 2026-10-22 (CHATBUG-095) re-pin: the attachment fallback name rule now
	// also covers the fallback size line, and the GIF badge hides while the
	// tile's image has no source or the tile carries no image, so the badge
	// never covers loading or fallback text. Verified the rendered Home
	// document carries exactly that delta: the extended rule and the two
	// badge rules present, the old name-only rule absent, zero other lines
	// changed.
	// 2026-10-22 (CHATBUG-100) re-pin: the narrow-panel override that parked
	// the reading select at the line start is gone, so the select keeps its
	// row-end margin at every width. Verified the rendered Home document
	// carries exactly that delta: the one container rule shortened by those
	// 64 characters, zero other bytes changed.
	// 2026-10-22 (AGENTUX-063) re-pin: the composer's Preview toggle joins the
	// pressed-toggle rule, and the preview slot scrolls like the field it
	// replaces while an empty preview shows the placeholder muted. Verified
	// against the Home document rendered at the parent commit: base plus
	// exactly those three fragments is the new document byte for byte, zero
	// markup changed (Home does not render the composer).
	// 2026-11-11 re-pin against 45d42be1 (the last pin, which reproduced). The
	// delta is intentional and listed by todo:
	//   - CHATUX-046 (closed): the served sheets keep only the last of identical
	//     CSS rule copies and doubled @keyframes blocks. d8fe5cda's typed-sheet
	//     removals (.topbar, .people-columns, .work-row>.avatar, .shell-grid,
	//     .people-filter-control, .history-filter-controls, .people-filter-actions,
	//     .primary-nav, the nav chevron and brand placeholder) only dropped dead
	//     earlier copies; each still has a live copy later in platformHeadStyles
	//     (TestTodo_CHATUX_046_DuplicateRules control). Same for the chat rules
	//     .conversation-heading, .menu-item:hover, .menu-item.danger,
	//     .timeline-frame.away, .chat-rail, .mobile-chat-toggle, .thread-composer
	//     .send-label, .thread-back-label, .message-list first-of-type actions,
	//     .chat-row.selected under search, the .chatmod switch rule, the
	//     docs-detail-title-row h1 and persona-admin [popover] rules, and the
	//     hcm-item-enter, hcm-status-settle, hcm-shimmer and hcm-route-progress
	//     keyframes.
	//   - UXBLIND-065 (open, not yet ticked): collapsed-sidebar focus tooltip
	//     rules in typed_mig_E.go.
	//   - CHATBUG-097: day/unread divider message-action and message-meta rules.
	//   - CHATUX-051: emoji completion menu mention-name and mention-detail trim.
	//   - CHATUX-097: .message-list scroll-padding-top, composer-tools and
	//     composer-toolbar nowrap rules.
	//   - .people-directory .data-table-scroll overflow-x:auto under 1081px,
	//     asserted by uxblind_lane_II_tables_topbar_test.go.
	// 2026-11-12 re-pin against 27993e51 (the Home document at 5c58a552). A
	// rule-by-rule comparison of the two rendered documents shows 7 rules
	// replaced and 50 added, zero markup changed:
	//   - chat membership gate: the .chatgate overlay, form, table, rule-choice
	//     and preview rules, and the .chat-workspace .chatgate-banner,
	//     -browse-note and -poll-* rules (new).
	//   - chat thread panel: .thread-heading-text h2 no longer flexes, the
	//     narrow thread Back label hides under 767px in place of the widened
	//     close button, and the compact saved-item actions apply only to
	//     wide fine-pointer screens.
	//   - chat sidebar open: the topbar hide rule gained !important.
	//   - #3427 .ownership-tree ul[hidden] stays hidden.
	//   - workflow designer: the name field takes a full row under 640px.
	//   - project tickets and board: the toolbar, filters and empty states
	//     gained min/max inline-size and single-column grids, and the board
	//     action disclosures and segments wrap under 40rem.
	const want = "6567a3ad7e1f2d87c78f64cccd9b0426ab211721b5a2d0bcfd999f6aa49c91c9"
	if digest != want {
		t.Fatalf("production Home document golden = %s, want %s; the document is in %s", digest, want, goldenReviewCopy(t, "uipolish012-home", doc))
	}
}
