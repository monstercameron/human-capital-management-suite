package productui

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// UXAUDIT-012: eliminate layout shifts during server-backed loading and
// navigation.
//
// RED (from the live audit): route changes remount the shell, server
// fetches collapse or expand major page regions, loading proxies do not
// preserve final geometry, or focus and scroll jump when data resolves.
//
// Investigating RED's sharpest clause -- "loading proxies do not preserve
// final geometry" -- found it literally true, not just plausible: the
// loading skeleton's row min-heights disagreed with their resolved
// counterparts' declared min-heights (typed_mig_C.go vs typed_styles.go):
// .loading-row was 76px against .work-row's 86px (a 10px-per-row shift on
// every Home, Work and Journeys cold load), .loading-table-row was 67px
// against .people-row's 65px, and .loading-table-head was 46px against
// .people-columns' 44px (a smaller shift on every People/History cold
// load). Those three pairs are fixed in this commit; this file is the proof
// they stay fixed and the model that keeps a fourth pair from drifting the
// same way.
//
// GREEN: software navigation retains shell and page chrome, each
// asynchronous region owns a size-compatible skeleton or inline progress
// state, only affected regions update, focus and scroll remain stable, and
// cumulative layout shift stays within the declared frontend budget.
//
// REFACTOR: one async-region state model (AsyncRegionState, async_region.go)
// covers loading, empty, stale, failure and resolved states across
// components.

// geometryRuleMinHeightPx extracts the numeric min-height, in pixels,
// declared for a rule whose selector list contains selector as an exact,
// standalone item, in css. Unlike uxaudit001_shell_drawer_test.go's
// cssDeclBlock (which finds the LAST textual occurrence of a prefix,
// because UXAUDIT-001 wanted whichever rule wins the cascade), this
// function requires selector to be a complete item of a rule's comma-
// separated selector list -- never a substring of a longer compound or
// descendant selector -- because this package also declares density-variant
// rules such as `:root[data-hcm-density="compact"] .work-row` that
// legitimately set a different min-height for the same bare class name
// under an unrelated attribute selector; a naive substring search would
// pick one of those up instead of (or in ambiguous conflict with) the base
// rule this test means to compare. cssRulePattern's non-nested match
// naturally un-wraps an @media(){...} block too, since the media
// condition's own text cannot itself satisfy `[^{}]*` up to a `}`.
//
// A selector with more than one rule declaring min-height for it as a
// standalone selector-list item, disagreeing on the value, is treated as an
// ambiguous fixture this test cannot safely reason about, and fails loudly
// rather than silently picking one.
var cssRulePattern = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
var cssMinHeightPattern = regexp.MustCompile(`min-height:(\d+)px`)

func geometryRuleMinHeightPx(t *testing.T, css, selector string) int {
	t.Helper()
	var found []int
	var matchedRules []string
	for _, rule := range cssRulePattern.FindAllStringSubmatch(css, -1) {
		exact := false
		for _, item := range strings.Split(rule[1], ",") {
			if strings.TrimSpace(item) == selector {
				exact = true
				break
			}
		}
		if !exact {
			continue
		}
		sub := cssMinHeightPattern.FindStringSubmatch(rule[2])
		if sub == nil {
			continue
		}
		px, err := strconv.Atoi(sub[1])
		if err != nil {
			t.Fatalf("stylesheet min-height for %s is not numeric: %v", selector, err)
		}
		found = append(found, px)
		matchedRules = append(matchedRules, rule[0])
	}
	if len(found) == 0 {
		t.Fatalf("stylesheet declares no min-height for the standalone selector %s", selector)
	}
	for _, px := range found[1:] {
		if px != found[0] {
			t.Fatalf("stylesheet declares ambiguous min-height for %s: %v", selector, matchedRules)
		}
	}
	return found[0]
}

// geometryPair is one (skeleton selector, resolved selector) pair this todo
// requires to preserve geometry.
type geometryPair struct {
	proxy, resolved string
}

// uxaudit012GeometryPairs is the exhaustive list this file proves parity
// for. Adding a new page family's loading proxy without adding its pairing
// here is exactly the kind of silent gap RED describes -- this list is
// deliberately named and complete rather than discovered by convention.
var uxaudit012GeometryPairs = []geometryPair{
	{proxy: ".loading-row", resolved: ".work-row"},
	{proxy: ".loading-table-row", resolved: ".people-row"},
	{proxy: ".loading-table-head", resolved: ".people-columns"},
}

// TestTodo_UXAUDIT_012 is the PRIMARY. It proves the geometry claim by
// comparing declared dimensions rather than presence of a class, and proves
// the async-region state model is exhaustive and fails safe on its zero
// value, per the todo's proof standards.
func TestTodo_UXAUDIT_012(t *testing.T) {
	t.Run("geometry parity", func(t *testing.T) {
		css := Stylesheet()
		for _, pair := range uxaudit012GeometryPairs {
			proxyHeight := geometryRuleMinHeightPx(t, css, pair.proxy)
			resolvedHeight := geometryRuleMinHeightPx(t, css, pair.resolved)
			if proxyHeight != resolvedHeight {
				t.Errorf("%s declares min-height %dpx but %s declares %dpx -- the proxy does not preserve final geometry",
					pair.proxy, proxyHeight, pair.resolved, resolvedHeight)
			}
		}
	})

	t.Run("zero value fails safe to loading, never resolved", func(t *testing.T) {
		var zero AsyncRegionState
		if zero != AsyncRegionLoading {
			t.Fatalf("AsyncRegionState zero value = %v, want AsyncRegionLoading", zero)
		}
		usesProxy, err := zero.UsesGeometryProxy()
		if err != nil || !usesProxy {
			t.Fatalf("zero-value AsyncRegionState must render through the geometry proxy, got usesProxy=%v err=%v", usesProxy, err)
		}
		// An omitted State field (LoadingProxyProps{Page: ...}) must produce
		// exactly the same output as an explicit AsyncRegionLoading -- this
		// is what makes every pre-UXAUDIT-012 call site still correct.
		omitted, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: PagePeople}))
		if err != nil {
			t.Fatal(err)
		}
		explicit, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: PagePeople, State: AsyncRegionLoading}))
		if err != nil {
			t.Fatal(err)
		}
		if omitted != explicit {
			t.Fatal("omitting LoadingProxyProps.State does not render identically to explicit AsyncRegionLoading")
		}
		// The one thing the zero value must never do: render as resolved.
		// BuildAsyncRegion is the seam a real caller uses to decide; feed it
		// the zero value and a resolved node that is trivially
		// distinguishable, and require the proxy, not the resolved node.
		resolvedMarker := ui.Text("RESOLVED-CONTENT-MARKER")
		node, err := BuildAsyncRegion(PagePeople, zero, resolvedMarker, "")
		if err != nil {
			t.Fatal(err)
		}
		out, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "RESOLVED-CONTENT-MARKER") {
			t.Fatal("zero-value AsyncRegionState rendered the resolved marker -- an unset region state must never render as resolved")
		}
		if !strings.Contains(out, "loading-table-layout") {
			t.Fatal("zero-value AsyncRegionState did not render the People-shaped loading proxy")
		}
	})

	t.Run("every state is exhaustively handled with no permissive default", func(t *testing.T) {
		for _, state := range []AsyncRegionState{AsyncRegionLoading, AsyncRegionEmpty, AsyncRegionStale, AsyncRegionFailure, AsyncRegionResolved} {
			if _, err := state.UsesGeometryProxy(); err != nil {
				t.Errorf("UsesGeometryProxy(%v) unexpectedly errored: %v", state, err)
			}
			if _, _, err := state.Announcement(ResolveProductLocale("en-US"), "detail"); err != nil {
				t.Errorf("Announcement(%v) unexpectedly errored: %v", state, err)
			}
		}
		unknown := AsyncRegionState(99)
		if _, err := unknown.UsesGeometryProxy(); !errors.Is(err, ErrUnknownAsyncRegionState) {
			t.Fatalf("UsesGeometryProxy(99) = %v, want ErrUnknownAsyncRegionState", err)
		}
		if _, _, err := unknown.Announcement(ResolveProductLocale("en-US"), ""); !errors.Is(err, ErrUnknownAsyncRegionState) {
			t.Fatalf("Announcement(99) = %v, want ErrUnknownAsyncRegionState", err)
		}
		if _, err := BuildAsyncRegion(PagePeople, unknown, ui.Text("x"), ""); !errors.Is(err, ErrUnknownAsyncRegionState) {
			t.Fatalf("BuildAsyncRegion(99) = %v, want ErrUnknownAsyncRegionState", err)
		}
		if got, want := unknown.String(), "AsyncRegionState(99)"; got != want {
			t.Fatalf("String() = %q, want %q", got, want)
		}
		// LoadingProxy itself is the last line of defense: even called
		// directly with a value BuildAsyncRegion would have refused, it must
		// not panic and must not silently render either the decorative
		// shimmer or an unannounced failure -- see LoadingProxy's own doc
		// comment for why both of those are the wrong kind of permissive.
		invalidOut, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: PagePeople, State: unknown}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(invalidOut, `role="alert"`) || !strings.Contains(invalidOut, "AsyncRegionState(99)") {
			t.Fatalf("LoadingProxy given an unrecognized state did not surface a visible diagnostic: %q", invalidOut)
		}
	})
}

// TestTodo_UXAUDIT_012_Browser is a structural SSR/DOM assertion parsing the
// rendered document, in the same idiom this package's other UXAUDIT Browser
// tests use (see TestTodo_UXAUDIT_001_Browser's and TestTodo_UXAUDIT_006_
// Browser's own boundary notes): it does not drive a live browser, which per
// this lane's operating constraints is reserved for the operator against
// the real dev server. It proves RED's "route changes remount the shell"
// concretely: the header and the navigation sidebar subtrees are rendered
// byte-identical whether the destination content region is still loading or
// has resolved.
func TestTodo_UXAUDIT_012_Browser(t *testing.T) {
	view := testView(PageWork)

	loadingDoc, err := ui.RenderToString(BuildContentLoading(view))
	if err != nil {
		t.Fatal(err)
	}
	resolvedDoc, err := ui.RenderToString(Build(view))
	if err != nil {
		t.Fatal(err)
	}

	loadingRoot, err := xhtml.Parse(strings.NewReader(loadingDoc))
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := xhtml.Parse(strings.NewReader(resolvedDoc))
	if err != nil {
		t.Fatal(err)
	}

	for _, tag := range []string{"header", "aside"} {
		loadingNode := firstElement(loadingRoot, tag)
		resolvedNode := firstElement(resolvedRoot, tag)
		if loadingNode == nil || resolvedNode == nil {
			t.Fatalf("<%s> missing from one of the two renders (loading=%v resolved=%v)", tag, loadingNode != nil, resolvedNode != nil)
		}
		var loadingBuf, resolvedBuf bytes.Buffer
		if err := xhtml.Render(&loadingBuf, loadingNode); err != nil {
			t.Fatal(err)
		}
		if err := xhtml.Render(&resolvedBuf, resolvedNode); err != nil {
			t.Fatal(err)
		}
		if loadingBuf.String() != resolvedBuf.String() {
			t.Errorf("<%s> is not identical between a content-loading transition and its resolved render -- the shell remounted instead of retaining chrome", tag)
		}
	}

	// Exactly one <main> region is the swappable outlet in both cases; the
	// shell (header, aside) surrounds it and is not duplicated or removed.
	if countElements(loadingRoot, "main") != 1 || countElements(resolvedRoot, "main") != 1 {
		t.Fatal("content-loading transition changed the number of main outlets")
	}
	mainNode := firstElement(loadingRoot, "main")
	if attr(mainNode, "id") != "main-content" {
		t.Fatal("content-loading outlet lost its stable id -- focus/skip-link targets would break across the transition")
	}
}

// TestTodo_UXAUDIT_012_Performance proves cumulative layout shift stays
// within the declared frontend budget (AsyncRegionLayoutShiftBudget,
// layout_shift.go) using a deterministic computed shift score -- never
// elapsed wall-clock time, which this machine's own latency gates already
// document as unreliable under load.
func TestTodo_UXAUDIT_012_Performance(t *testing.T) {
	const viewportHeightPx = 720 // the mobile viewport this package's own UXAUDIT-001 evidence verified live against.

	css := Stylesheet()
	t.Run("current declared geometry stays within budget", func(t *testing.T) {
		for _, pair := range uxaudit012GeometryPairs {
			before := float64(geometryRuleMinHeightPx(t, css, pair.proxy))
			after := float64(geometryRuleMinHeightPx(t, css, pair.resolved))
			score, err := CheckLayoutShift(before, after, viewportHeightPx, AsyncRegionLayoutShiftBudget)
			if err != nil {
				t.Errorf("%s -> %s: %v", pair.proxy, pair.resolved, err)
			}
			t.Logf("%s -> %s: shift score %.4f (budget %.2f)", pair.proxy, pair.resolved, score, AsyncRegionLayoutShiftBudget)
			if score != 0 {
				t.Errorf("%s -> %s: shift score %.4f, want exactly 0 now that the pair is aligned", pair.proxy, pair.resolved, score)
			}
		}
	})

	// The check itself must actually reject something: a region whose
	// declared height goes from 0 (nothing rendered) to more than a tenth of
	// the viewport is exactly the "collapses, then jumps" shape RED
	// describes, and must exceed budget.
	t.Run("check rejects a real violation", func(t *testing.T) {
		score, err := CheckLayoutShift(0, viewportHeightPx*0.5, viewportHeightPx, AsyncRegionLayoutShiftBudget)
		if err == nil {
			t.Fatalf("a region collapsing from full height to zero (score %.2f) must exceed budget %.2f", score, AsyncRegionLayoutShiftBudget)
		}
	})

	if _, err := LayoutShiftScore(10, 10, 0); err == nil {
		t.Fatal("LayoutShiftScore must refuse a non-positive viewport height rather than dividing by zero")
	}
}

// TestTodo_UXAUDIT_012_Accessibility proves every async region state carries
// a coherent, correctly-prioritized assistive-technology contract: Loading
// is quiet background progress (aria-hidden, no interrupting announcement),
// Failure interrupts (role="alert", aria-live="assertive", the failure
// detail actually present in the accessible tree), and both preserve the
// same landmark structure so focus targets do not move.
func TestTodo_UXAUDIT_012_Accessibility(t *testing.T) {
	loadingText, loadingAssertive, err := AsyncRegionLoading.Announcement(ResolveProductLocale("en-US"), "")
	if err != nil || loadingText == "" || loadingAssertive {
		t.Fatalf("Loading announcement = (%q, assertive=%v, err=%v), want non-empty polite text", loadingText, loadingAssertive, err)
	}
	failureText, failureAssertive, err := AsyncRegionFailure.Announcement(ResolveProductLocale("en-US"), "dial people: connection refused")
	if err != nil || !failureAssertive || !strings.Contains(failureText, "dial people: connection refused") {
		t.Fatalf("Failure announcement = (%q, assertive=%v, err=%v), want the detail present and assertive delivery", failureText, failureAssertive, err)
	}

	loadingOut, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: PagePeople, State: AsyncRegionLoading}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loadingOut, `aria-hidden="true"`) || strings.Contains(loadingOut, `role="alert"`) {
		t.Fatal("Loading proxy must be decorative (aria-hidden) and must not claim an interrupting alert role")
	}

	failureOut, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: PagePeople, State: AsyncRegionFailure, Message: "dial people: connection refused"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failureOut, `role="alert"`) || !strings.Contains(failureOut, `aria-live="assertive"`) {
		t.Fatal("Failure proxy must expose role=alert and aria-live=assertive so a user who stopped attending is still told")
	}
	if !strings.Contains(failureOut, "dial people: connection refused") {
		t.Fatal("Failure proxy dropped the failure detail from the accessible tree")
	}
	if strings.Contains(failureOut, `aria-hidden="true"`) {
		t.Fatal("Failure proxy must not be hidden from assistive technology like the decorative loading shimmer is")
	}
}

// TestTodo_UXAUDIT_012_Fault proves the failure state also preserves
// geometry: "a region that collapses when its fetch fails is the same
// defect as one that collapses while loading." BuildFailure renders through
// the identical loadingProxyBody call BuildLoading and BuildContentLoading
// use, so the visible skeleton markup is byte-identical between the two --
// not merely similarly sized, but the same bytes -- while the accessible
// contract differs as UXAUDIT_012_Accessibility proves.
func TestTodo_UXAUDIT_012_Fault(t *testing.T) {
	for _, page := range []PageID{PagePeople, PageWork, PageOrganization} {
		loading, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: page, State: AsyncRegionLoading}))
		if err != nil {
			t.Fatal(err)
		}
		failure, err := ui.RenderToString(ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: page, State: AsyncRegionFailure, Message: "dial " + string(page) + ": connection refused"}))
		if err != nil {
			t.Fatal(err)
		}
		loadingBody := loading[strings.Index(loading, "loading-progress"):]
		failureBody := failure[strings.Index(failure, "loading-progress"):]
		if loadingBody != failureBody {
			t.Errorf("%s: failure proxy body differs from loading proxy body -- a failed fetch renders a different, differently-sized shape instead of the same size-compatible one", page)
		}
	}

	// BuildFailure is the real call site: it must keep shell chrome mounted
	// (like BuildContentLoading) rather than collapsing the whole document
	// to a bare error message, and it must not disturb View.LoadError's own,
	// separate, pre-existing page-level degradation contract (page_admin.go
	// and others render their own partial-unavailable cards from it).
	view := testView(PageWork)
	view.LoadError = "should not be touched by BuildFailure"
	out, err := ui.RenderToString(BuildFailure(view, "dial work: connection refused"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="main-content"`, "loading-work-layout", "dial work: connection refused", `role="alert"`} {
		if !strings.Contains(out, want) {
			t.Errorf("BuildFailure output missing %q", want)
		}
	}
	if strings.Contains(out, "should not be touched by BuildFailure") {
		t.Fatal("BuildFailure leaked View.LoadError's own message instead of using its own message parameter")
	}
	root, err := xhtml.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if countElements(root, "header") != 1 || countElements(root, "aside") != 1 || countElements(root, "main") != 1 {
		t.Fatal("BuildFailure did not keep the application shell mounted around the failed region")
	}
}

// TestTodo_UXAUDIT_012_Regression guards three things a future change could
// silently undo: the two historical geometry bugs this file's package
// comment documents do not reappear even if someone edits only one side of
// a pair, the pre-existing warm-refresh and content-loading contracts
// (loading_components_test.go) still hold now that LoadingProxyProps grew a
// State field, and BuildAsyncRegion's Stale/Resolved branches really do
// pass real content through unchanged rather than re-wrapping it.
func TestTodo_UXAUDIT_012_Regression(t *testing.T) {
	css := Stylesheet()
	regressed := map[string]int{
		".loading-row":        76, // was mismatched against .work-row's 86px
		".loading-table-row":  67, // was mismatched against .people-row's 65px
		".loading-table-head": 46, // was mismatched against .people-columns' 44px
	}
	for selector, oldBuggyValue := range regressed {
		got := geometryRuleMinHeightPx(t, css, selector)
		if got == oldBuggyValue {
			t.Errorf("%s still declares the pre-fix min-height %dpx", selector, oldBuggyValue)
		}
	}

	// loading_components_test.go's pre-existing contracts, re-affirmed after
	// LoadingProxyProps gained State/Message: a warm refresh still keeps
	// resolved content mounted rather than reverting to a cold proxy, and a
	// content-loading transition still keeps the shell resolved.
	warm := testView(PagePeople)
	warmOut, err := ui.RenderToString(BuildRefreshing(warm))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warmOut, "Avery Patel") || strings.Contains(warmOut, "loading-table-layout") {
		t.Fatal("BuildRefreshing regressed: no longer keeps authorized content mounted during a warm refresh")
	}

	contentLoading := testView(PageWork)
	contentOut, err := ui.RenderToString(BuildContentLoading(contentLoading))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contentOut, `class="app-shell is-content-loading"`) || !strings.Contains(contentOut, "loading-work-layout") {
		t.Fatal("BuildContentLoading regressed: shell state class or page-shaped proxy missing")
	}

	// BuildAsyncRegion's non-proxy branches (Stale, Resolved) must return
	// the caller's resolved node completely unchanged, never re-wrapped or
	// substituted.
	marker := ui.Text("REGION-CONTENT-PASSTHROUGH")
	for _, state := range []AsyncRegionState{AsyncRegionStale, AsyncRegionResolved} {
		node, err := BuildAsyncRegion(PagePeople, state, marker, "")
		if err != nil {
			t.Fatal(err)
		}
		out, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "REGION-CONTENT-PASSTHROUGH") {
			t.Errorf("BuildAsyncRegion(%v) did not pass the resolved node through unchanged", state)
		}
	}
}
