package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-042: the attention notification center. The shell must offer
// one notification surface that reports the open-work count honestly, opens
// the work destination only when authorized, renders an honest pending state
// while loading, and stays deterministic for one view.

func TestTodo_WEB_042(t *testing.T) {
	doc, err := Render(testView(PageWork))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	center := findNotificationCenter(root)
	if center == nil {
		t.Fatal("shell rendered no attention notification center")
	}
	// The trigger announces the count it shows; the panel links the
	// authorized work destination.
	if got := attr(findSummary(center), "aria-label"); !strings.Contains(got, "Work overview") {
		t.Fatalf("notification trigger does not announce its count: %q", got)
	}
	if linkForRoute(center, "/workspace/app/work") == nil {
		t.Fatal("notification center did not link the authorized work destination")
	}

	// Without the work destination the link disappears but the count
	// surface stays honest instead of vanishing.
	denied := ApplyPagePermissions(testView(PageWork), []RolePagePermission{
		{Version: 1, RoleID: "viewer", Page: PagePeople, View: true},
	})
	deniedDoc, err := Render(denied)
	if err != nil {
		t.Fatal(err)
	}
	deniedRoot, err := xhtml.Parse(strings.NewReader(deniedDoc))
	if err != nil {
		t.Fatal(err)
	}
	deniedCenter := findNotificationCenter(deniedRoot)
	if deniedCenter == nil {
		t.Fatal("notification center disappeared instead of rendering its honest restricted state")
	}
	if linkForRoute(deniedCenter, "/workspace/app/work") != nil {
		t.Fatal("notification center advertised a work destination the identity cannot view")
	}

	// While loading, the slot reports pending and exposes nothing stale to
	// assistive technology.
	loading := testView(PageWork)
	loading.Loading = true
	loadingDoc, err := Render(loading)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`notifications notification-loading network-slot network-slot-pending`, `aria-hidden="true"`,
	} {
		if !strings.Contains(loadingDoc, want) {
			t.Fatalf("loading notification slot missing %q", want)
		}
	}

	// Two renders of one view agree on the notification subtree.
	first, second := renderNotificationSubtree(t, testView(PageWork)), renderNotificationSubtree(t, testView(PageWork))
	if first != second {
		t.Fatal("notification center is not deterministic for one view")
	}
}

func findNotificationCenter(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && node.Data == "details" && attr(node, "data-hcm-transient-popover") == "notification" {
			found = node
		}
	})
	return found
}

func findSummary(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && node.Data == "summary" {
			found = node
		}
	})
	return found
}

func renderNotificationSubtree(t *testing.T, view View) string {
	t.Helper()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	center := findNotificationCenter(root)
	if center == nil {
		t.Fatal("shell rendered no attention notification center")
	}
	var rendered strings.Builder
	if err := xhtml.Render(&rendered, center); err != nil {
		t.Fatal(err)
	}
	return rendered.String()
}

// web042GoldenDigest is pinned from the GREEN implementation run. PROMOUX-012
// re-pinned it for the actionable-count copy; substituting the previous
// "promotion journey is visible in this scope." back reproduces the old pin.
// NAAS-001 adds the localized empty inbox state before Open My Work.
const web042GoldenDigest = "8074f1f620384865746a6529c49ed4fd6e659a6bcb12e3a25a3b009427d9af8e"

func TestTodo_WEB_042_Golden(t *testing.T) {
	digest := sha256.Sum256([]byte(renderNotificationSubtree(t, testView(PageWork))))
	if got := hex.EncodeToString(digest[:]); got != web042GoldenDigest {
		t.Fatalf("notification golden digest mismatch: got %s want %s", got, web042GoldenDigest)
	}
}

func TestTodo_WEB_042_Browser(t *testing.T) {
	doc, err := Render(testView(PageWork))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	center := findNotificationCenter(root)
	if center == nil {
		t.Fatal("shell rendered no attention notification center")
	}
	// Native details/summary is the keyboard-operable baseline; the trigger
	// carries the accessible name.
	summary := findSummary(center)
	if summary == nil {
		t.Fatal("notification center has no keyboard-operable trigger")
	}
	if strings.TrimSpace(attr(summary, "aria-label")) == "" {
		t.Fatal("notification trigger has no accessible name")
	}
	css := Stylesheet()
	for _, want := range []string{
		`.notifications{position:relative;}`,
		`.notifications>.popover-surface{padding:18px;width:290px;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("notification stylesheet missing %q", want)
		}
	}
}

func TestTodo_WEB_042_Conformance(t *testing.T) {
	// Every locale renders named notification copy; unresolved keys stay
	// visible as ⟦key⟧ and must never ship in the shell.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageWork), ResolveProductLocale(locale))
		subtree := renderNotificationSubtree(t, view)
		if strings.Contains(subtree, "⟦") {
			t.Fatalf("locale %s rendered an unresolved notification message key", locale)
		}
		if !strings.Contains(subtree, "popover-root notifications") {
			t.Fatalf("locale %s lost the notification center surface", locale)
		}
	}
}
