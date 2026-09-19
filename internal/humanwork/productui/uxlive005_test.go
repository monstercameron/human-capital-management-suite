package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-005 was filed as "two admin pages never finish loading" and that
// RED was wrong: both load, the unary RPC deadline is 30s, and the failure
// path was then proved by stopping the server mid-session -- the page
// rendered "Live data unavailable" and announced "page loaded".
//
// What remained true is smaller: the panel's only recovery was the sentence
// "Refresh this page to try again", which costs a full document load and
// re-fetches the whole bundle. It now carries a control that asks again.

func uxlive005Shell(t *testing.T, view View) string {
	t.Helper()
	doc, err := ui.RenderToString(appShell(view, ui.Fragment()))
	if err != nil {
		t.Fatalf("render shell: %v", err)
	}
	return doc
}

// TestTodo_UXLIVE_005 is the primary red/green test: the failure state
// offers a way to ask again.
func TestTodo_UXLIVE_005(t *testing.T) {
	view := testView(PageWork)
	view.LoadError = "We couldn't load this page. Try again."
	doc := uxlive005Shell(t, view)

	retry := view.Locale.Text("shell.load_retry")
	if retry == "" || strings.HasPrefix(retry, "shell.") {
		t.Fatalf("no retry copy is published: %q", retry)
	}
	if !strings.Contains(doc, retry) {
		t.Fatalf("the unavailable panel offers no retry:\n%s", doc)
	}
	if !strings.Contains(doc, view.Locale.Text("shell.live_unavailable")) {
		t.Fatalf("the unavailable panel lost its title:\n%s", doc)
	}

	// The recovery sentence no longer tells the reader to reload as the only
	// way back.
	if strings.Contains(view.Locale.Text("shell.load_recovery"), "Refresh this page") {
		t.Fatalf("the recovery sentence still names a full reload as the recovery: %q", view.Locale.Text("shell.load_recovery"))
	}

	// A healthy page offers no retry at all.
	healthy := testView(PageWork)
	if doc := uxlive005Shell(t, healthy); strings.Contains(doc, retry) {
		t.Fatalf("a page with no load error offers a retry:\n%s", doc)
	}
}

// TestTodo_UXLIVE_005_Browser keeps the script-free path working: the retry
// is a real link to the page's own address, so it works without a mounted
// client and becomes an in-place route change with one.
func TestTodo_UXLIVE_005_Browser(t *testing.T) {
	view := testView(PageWork)
	view.LoadError = "We couldn't load this page. Try again."
	doc := uxlive005Shell(t, view)

	href := statefulHref(view, view.Page)
	if !strings.Contains(doc, `href="`+href+`"`) {
		t.Fatalf("the retry does not address the page it failed to load (%q):\n%s", href, doc)
	}
}

// TestTodo_UXLIVE_005_Fault proves the panel is reached from the failure
// state itself rather than from any particular page's own code.
func TestTodo_UXLIVE_005_Fault(t *testing.T) {
	retry := ResolveProductLocale("en-US").Text("shell.load_retry")
	for _, page := range []PageID{PageHome, PageWork, PagePeople, PageAdmin, PageInsights} {
		view := testView(page)
		view.LoadError = "unreachable"
		if doc := uxlive005Shell(t, view); !strings.Contains(doc, retry) {
			t.Fatalf("%s offers no retry when its data does not arrive", page)
		}
	}
}
