package productui

import (
	"fmt"
	"strings"
	"testing"
)

// UXAUDIT-008: give the People directory one stable, high-density table
// viewport.
//
// RED, measured live on /workspace/app/people at 1024x768 (this todo's
// section preamble): one page-level scroll region already held (do not
// regress it), zero rows fully visible above the filters and page chrome,
// `thead th` computed `position` was `static` (there was no sticky header
// at all, not merely a detached one), and 10 of 20 rows repeated a large
// unavailable-workflow sentence.
//
// GREEN: filters and results share a stable layout, only the data region
// updates on sort/filter/page-size changes, headers remain aligned and
// sticky, density is usable at 100 rows, scroll position is preserved, and
// compact row actions communicate availability without visual noise --
// while PROMOUX-001's contract (every displayed availability state carries
// a non-empty, server-provided reason; an unauthorized viewer's reason is
// byte-identical across causes) keeps holding, unchanged.
//
// REFACTOR: People renders through the shared DataTable component
// (people_components.go's PeopleTable already called it before this todo)
// and now shares its pagination arithmetic with History through
// PaginateCollection (data_table.go) instead of each page carrying its own
// copy. Server-persisted table preferences for the People table already
// exist end to end (preferences.TablePreferences, written by
// tools/uxqual/cmd/journeywasm/preferences_wasm.go's PersistView and read
// back by tools/uxqual/productclient/client.go's applyTableDefaults) --
// this todo did not need to invent them.

func uxaudit008ManyPeople(count int, unavailableEvery int) []Person {
	people := make([]Person, count)
	for index := range people {
		person := Person{
			ID: fmt.Sprintf("worker-%03d", index), Name: fmt.Sprintf("Worker %03d", index),
			Role: "Engineer", Team: "Platform", Manager: "Ravi Shah", Location: "Remote",
			PromotionAvailability: PromotionEligible,
		}
		if unavailableEvery > 0 && index%unavailableEvery == 0 {
			person.PromotionAvailability = PromotionIneligible
		}
		people[index] = person
	}
	return people
}

// TestTodo_UXAUDIT_008 is the PRIMARY matrix test. It proves the two
// literal facts the live audit recorded are now false: the header cells
// carry position:sticky themselves (not just an ancestor), and the
// directory's own scroll region no longer reserves a fixed, filter-blind
// height budget. It also proves the compact-badge affordance never drops
// PROMOUX-001's server-provided reason.
func TestTodo_UXAUDIT_008(t *testing.T) {
	t.Run("header cells carry their own sticky positioning", func(t *testing.T) {
		css := Stylesheet()
		// The RED fact was specifically about <th>, not <thead> or the header
		// <tr> -- both of which already declared position:sticky before this
		// todo and are proven unchanged by TestTodo_UXAUDIT_008_Regression. A
		// plain <th> never inherits position from an ancestor, so the header
		// cells needed their own declaration.
		want := ".data-table thead th{position:sticky;top:0;}"
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet missing header-cell sticky rule %q", want)
		}
	})

	t.Run("the directory scroll region is no longer a fixed, filter-blind cap", func(t *testing.T) {
		css := uxaudit008TableDensityStylesheet()
		// UPDATED (live re-audit): the cap now lives inside a
		// `min-width:761px` media context rather than unconditionally -- see
		// the "scroll wrapper max-height and overflow stay a matched pair"
		// subtest below for why (a live-browser pass found 16 of 20 workers
		// unreachable at 1024x768
		// because the earlier, unconditional version of this rule beat the
		// shared component's card-mode `max-height:none` reset without also
		// restating `overflow`). The literal values themselves (82vh, 920px,
		// 14rem) are unchanged and still asserted here so 1440x900 behaviour
		// stays pinned; the media wrapper is asserted explicitly, visibly,
		// rather than only implicitly via a substring that happens to still
		// match.
		for _, want := range []string{
			"@media (min-width:761px){.people-directory .data-table-scroll{",
			"82vh", "920px", "14rem", "overflow:auto",
		} {
			if !strings.Contains(css, want) {
				t.Fatalf("directory density rule missing %q:\n%s", want, css)
			}
		}
		// Scoped under .people-directory, not the bare .data-table-scroll
		// class every other DataTable consumer (History, Organization) also
		// uses -- widening this budget must not change their sizing. The
		// selector text itself proves the scope; a bare, unscoped rule would
		// start the string with the class rather than the ancestor.
		if strings.HasPrefix(css, ".data-table-scroll{") {
			t.Fatalf("density rule leaked onto the unscoped .data-table-scroll selector: %s", css)
		}
	})

	// TestTodo_UXAUDIT_008 / "scroll wrapper max-height and overflow stay a
	// matched pair" is a rule-level invariant, not a string match: any CSS
	// block that declares this selector (`.people-directory
	// .data-table-scroll`) with a bounded (non-"none") max-height must also
	// declare a scrolling overflow in that SAME block. A test that only
	// grepped for "82vh" would have passed the exact broken build a live
	// browser pass caught (bounded max-height reachable at <=1050px while
	// `.data-table-scroll`'s shared card-mode reset left overflow:visible in
	// effect there) -- this one would not, because the offending block had
	// no overflow declaration of its own at all.
	//
	// Mutation-verified by hand against this invariant: temporarily
	// reintroducing the exact broken pairing --
	//   declareGlobal(".people-directory .data-table-scroll",
	//       gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(82), gwccss.Px(920))),
	//       gwccss.MinHeight(gwccss.Rem(14)),
	//   )
	// (unconditional, no media guard, no overflow) in place of the current
	// min-width-guarded rule -- made this subtest FAIL with "found a bounded
	// max-height with no scrolling overflow in the same block"; reverting to
	// the current rule made it PASS again.
	t.Run("scroll wrapper max-height and overflow stay a matched pair", func(t *testing.T) {
		css := Stylesheet()
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

	t.Run("compact badge never drops the server-provided reason", func(t *testing.T) {
		view := testView(PagePeople)
		view.People = []Person{{ID: "worker-ineligible", Name: "Ineligible Worker", PromotionAvailability: PromotionIneligible}}
		view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		reason := ResolveProductLocale("").Text("workflow.no_promotion_path")
		short := ResolveProductLocale("").Text("people.workflows_unavailable_short")
		if !strings.Contains(doc, `class="people-availability-badge muted"`) {
			t.Fatalf("unavailable row did not render the compact badge: %s", doc)
		}
		if !strings.Contains(doc, `title="`+reason+`"`) {
			t.Fatal("compact badge lost the full reason from its title attribute")
		}
		if !strings.Contains(doc, `class="sr-only"`) || !strings.Contains(doc, reason) {
			t.Fatal("compact badge lost the full reason from its screen-reader-only disclosure")
		}
		if !strings.Contains(doc, `aria-describedby="people-unavailable-worker-ineligible"`) {
			t.Fatal("compact badge is not associated with its full-reason disclosure via aria-describedby")
		}
		if !strings.Contains(doc, short) {
			t.Fatal("compact badge did not render the short, visible label")
		}
	})
}

// TestTodo_UXAUDIT_008_Browser is the BROWSER matrix entry. Direct browser
// evidence against the live server-backed UI was out of reach in this lane
// (the hard constraint against rebuilding or restarting the dev server, or
// running Playwright, is explicit); this test follows the same precedent
// UXAUDIT-004/006/007 recorded for their own Browser entries -- a static
// SSR/DOM assertion over the rendered document, not a live-browser check.
// It fails if the People directory stops emitting the scrollable, labeled
// table region a browser actually renders sticky headers inside.
func TestTodo_UXAUDIT_008_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.People = uxaudit008ManyPeople(20, 2)
	view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="data-table-scroll"`, `role="region"`, `tabIndex="0"`,
		`class="data-table people-table"`, `<thead`, `<tbody`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("rendered People page is missing the scrollable table fragment %q", want)
		}
	}
	// Half the rows carry the compact badge (every second worker was seeded
	// ineligible); none of them repeat the reason as flowing row content --
	// it lives in the title attribute and the visually-hidden span only.
	if got := strings.Count(doc, `class="people-availability-badge muted"`); got != 10 {
		t.Fatalf("compact badge count = %d, want 10 (half of 20 seeded rows)", got)
	}
}

// TestTodo_UXAUDIT_008_Accessibility asserts the real table semantics named
// in this todo: header cells associated with their columns via scope="col",
// sortable headers exposing aria-sort that tracks actual sort state, and
// the sticky-header CSS added by this todo never touching that markup
// association (position is presentation-only and shares nothing with the
// scope/aria-sort attributes it sits beside).
func TestTodo_UXAUDIT_008_Accessibility(t *testing.T) {
	view := testView(PagePeople)
	view.People = uxaudit008ManyPeople(5, 0)
	view.PeopleSort = peopleSortRole
	view.PeopleDirection = peopleSortDescending
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}

	// Every column header (five sortable People columns plus the static
	// Actions column) carries scope="col", associating each header cell with
	// its column regardless of which column is currently sorted.
	if got, want := strings.Count(doc, `scope="col"`), 6; got != want {
		t.Fatalf(`scope="col" count = %d, want %d`, got, want)
	}

	// aria-sort tracks the actual sort state: exactly one header carries it,
	// and it is the descending Role column this test requested -- not a
	// static or stale value.
	if got, want := strings.Count(doc, `aria-sort=`), 1; got != want {
		t.Fatalf("aria-sort attribute count = %d, want %d (only the active column)", got, want)
	}
	if !strings.Contains(doc, `aria-sort="descending"`) {
		t.Fatalf("active Role/descending column did not render aria-sort=\"descending\": %s", doc)
	}

	// The sticky-header CSS this todo adds is scoped to the stylesheet and
	// never rewrites markup, so the same document still associates every
	// header with scope="col" and the one active aria-sort -- the two
	// assertions above already prove that jointly; this restates it as the
	// explicit "sticky does not break association" claim GREEN makes.
	css := Stylesheet()
	if !strings.Contains(css, ".data-table thead th{position:sticky;top:0;}") {
		t.Fatal("sticky header rule regressed while proving column association")
	}
}

// TestTodo_UXAUDIT_008_Performance must not assert on elapsed wall-clock
// time (this machine's timing is unreliable). It asserts on rendered row
// count at a declared page size and on counted structural work: at
// page_size=100 against a 250-person population, exactly 100 rows render
// (never silently truncated or unbounded), the table emits exactly
// rows*columns data cells (a genuine rectangular matrix, not a duplicated
// or dropped one), and growing the row count fivefold (20 -> 100) grows the
// emitted document less than eightfold -- a loose linear bound that would
// fail on any accidental O(n^2) construction (for example, re-scanning the
// full population once per row) without depending on absolute timing.
func TestTodo_UXAUDIT_008_Performance(t *testing.T) {
	const columns = 6 // person, role, team, manager, location, actions

	population := uxaudit008ManyPeople(250, 3)

	view20 := testView(PagePeople)
	view20.People = population
	view20.PeoplePageSize = 20
	doc20, err := Render(view20)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(doc20, `class="data-table-row people-row-item people-row"`); got != 20 {
		t.Fatalf("page_size=20 rendered %d rows, want 20", got)
	}

	view100 := testView(PagePeople)
	view100.People = population
	view100.PeoplePageSize = 100
	doc100, err := Render(view100)
	if err != nil {
		t.Fatal(err)
	}
	rows100 := strings.Count(doc100, `class="data-table-row people-row-item people-row"`)
	if rows100 != 100 {
		t.Fatalf("page_size=100 rendered %d rows, want exactly 100 (density must be usable at 100 rows)", rows100)
	}
	if got, want := strings.Count(doc100, `class="data-table-cell`), rows100*columns; got != want {
		t.Fatalf("data cell count = %d, want %d (rows*columns, a rectangular matrix)", got, want)
	}

	if len(doc20) == 0 {
		t.Fatal("page_size=20 document is empty")
	}
	if ratio := float64(len(doc100)) / float64(len(doc20)); ratio >= 8 {
		t.Fatalf("document byte length grew %.2fx for a 5x row-count increase (20->100 rows); want a roughly linear bound (<8x), which an accidental O(n^2) render would exceed", ratio)
	}
}

// TestTodo_UXAUDIT_008_Regression pins what UXAUDIT-001 and UXAUDIT-012
// fixed and must not break: exactly one page-level scroll region (the
// .main-scroll contract UXAUDIT-001 established) survives untouched, and
// the loading proxy's geometry still matches the resolved row/head pairs
// UXAUDIT-012 aligned. It also pins that PROMOUX-001's two tests -- named
// explicitly because this todo's row markup change is the part most likely
// to disturb them -- keep passing by construction: the compact badge always
// carries WorkflowsUnavailableReason's exact value, never a stripped or
// truncated one.
func TestTodo_UXAUDIT_008_Regression(t *testing.T) {
	css := Stylesheet()

	// UXAUDIT-001: the shell's single page-level scroll region is untouched.
	if !strings.Contains(css, `.main-scroll{background-color:var(--canvas);height:100%;min-height:0;min-width:0;overflow-x:hidden;overflow-y:auto;`) {
		t.Fatal("the single page-level scroll region (.main-scroll) regressed")
	}
	// The People directory itself must not have become a second page-level
	// scroll region -- it stays overflow:visible; only its inner
	// .data-table-scroll child scrolls, and that nesting is not new.
	if !strings.Contains(css, `.people-directory{isolation:isolate;overflow:visible;position:relative;}`) {
		t.Fatal(".people-directory's non-scrolling contract regressed")
	}

	// UXAUDIT-012: the loading proxy's geometry still matches the row/head
	// it stands in for. A row-height change in this todo (the compact badge
	// touches row content, not row height rules) would silently break this
	// pairing if it ever touched .people-row or .people-columns directly --
	// it must not, and does not.
	for _, pair := range []struct{ proxy, resolved string }{
		{proxy: ".loading-table-row", resolved: ".people-row"},
		{proxy: ".loading-table-head", resolved: ".people-columns"},
	} {
		proxyHeight := geometryRuleMinHeightPx(t, css, pair.proxy)
		resolvedHeight := geometryRuleMinHeightPx(t, css, pair.resolved)
		if proxyHeight != resolvedHeight {
			t.Errorf("%s min-height=%dpx does not match %s min-height=%dpx", pair.proxy, proxyHeight, pair.resolved, resolvedHeight)
		}
	}

	// PROMOUX-001, restated against this todo's own row markup: an
	// unauthorized viewer still gets exactly one, indistinguishable reason
	// across every underlying code, now delivered through the compact badge
	// rather than as flowing row text.
	view := testView(PagePeople)
	view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: false}}
	view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
	reasons := map[string]bool{}
	for _, underlying := range []PromotionAvailabilityCode{PromotionEligible, PromotionIneligible, PromotionActiveConflict} {
		effective := ResolvePromotionAvailability(false, underlying == PromotionEligible, underlying == PromotionActiveConflict)
		view.People = []Person{{ID: "worker-under-test", Name: "Under Test", PromotionAvailability: effective}}
		rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 1, PageCount: 1, Page: 1})
		if len(rows) != 1 || rows[0].WorkflowsUnavailableReason == "" {
			t.Fatalf("underlying=%q: unauthorized viewer lost its reason: rows=%+v", underlying, rows)
		}
		reasons[rows[0].WorkflowsUnavailableReason] = true
	}
	if len(reasons) != 1 {
		t.Fatalf("unauthorized viewer saw %d distinct reasons across codes, want exactly 1 (PROMOUX-001 Security regression): %v", len(reasons), reasons)
	}
}

// cssRuleBlocks returns the declaration body (the text between `{` and `}`,
// media wrapper stripped by cssRulePattern's non-nested match, same as
// geometryRuleMinHeightPx in uxaudit012_layout_shift_test.go) of every rule
// in css whose selector list contains selector as an exact, standalone
// comma-separated item. A selector can legitimately appear more than once
// (e.g. once bare, once inside a `@media` context) -- callers that care
// about a per-occurrence invariant (such as "this block's max-height and
// overflow must agree") should check every returned block, not just the
// first.
func cssRuleBlocks(t *testing.T, css, selector string) []string {
	t.Helper()
	var blocks []string
	for _, rule := range cssRulePattern.FindAllStringSubmatch(css, -1) {
		for _, item := range strings.Split(rule[1], ",") {
			if strings.TrimSpace(item) == selector {
				blocks = append(blocks, rule[2])
				break
			}
		}
	}
	return blocks
}

// TestTodo_UXAUDIT_008_DirectoryTableModeRestoration is the defect-2
// regression: the live re-audit found 1024x768 -- a desktop viewport by
// UXAUDIT-008's own RED ("only a few rows fit in a desktop viewport") --
// still landed inside the shared DataTable component's card breakpoint
// (`@media (max-width:1050px)`), which stacks each row into a ~199px card
// of label/value pairs. This proves the People-scoped counter-rules in
// uxaudit008_table_density.go actually restore the dense, real-table
// layout (46px rows, the same one already used above 1050px) starting at
// 761px, rather than only asserting on isolated string fragments: the
// generic card-mode component only ever declares `display` for `.data-table`,
// `.data-table-body` and `.data-table .data-table-row` (the bare/ancestor
// selectors, not the `.people-directory`-scoped ones this test checks), so
// each People-scoped selector here has exactly one declaring rule; this
// asserts that rule's own value directly rather than only grepping the
// whole sheet for a matching substring that some unrelated rule could also
// satisfy.
func TestTodo_UXAUDIT_008_DirectoryTableModeRestoration(t *testing.T) {
	css := Stylesheet()
	for _, tc := range []struct {
		selector string
		want     string
	}{
		{".people-directory .data-table", "display:table"},
		{".people-directory .data-table-body", "display:table-row-group"},
		{".people-directory .data-table .data-table-row", "display:table-row"},
	} {
		blocks := cssRuleBlocks(t, css, tc.selector)
		if len(blocks) == 0 {
			t.Fatalf("no rule found for selector %q", tc.selector)
		}
		last := blocks[len(blocks)-1]
		if !strings.Contains(last, tc.want) {
			t.Fatalf("selector %q's cascade-winning (last) rule %q does not restore %q -- the directory would still be in card mode at 1024x768", tc.selector, last, tc.want)
		}
	}
	// The six shared column-width floors must win, as `!important`, over
	// the generic card-mode `min-width:0!important` reset -- otherwise the
	// restored table's columns collapse to their content's intrinsic width
	// instead of lining up with the header.
	for i, px := range [...]int{185, 180, 145, 125, 135, 110} {
		want := fmt.Sprintf("min-width:%dpx!important", px)
		if !strings.Contains(css, want) {
			t.Fatalf("nth-child(%d) column-width restoration missing %q", i+1, want)
		}
	}
}
