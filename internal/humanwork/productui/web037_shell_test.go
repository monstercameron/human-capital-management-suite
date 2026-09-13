package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_WEB_037 exercises the production shell tree through GWC's server
// renderer. It checks semantic ownership and state transitions, rather than
// treating the shell as an HTML string fixture.
func TestTodo_WEB_037(t *testing.T) {
	view := testView(PagePeople)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if countElements(root, "header") != 1 || countElements(root, "aside") != 1 || countElements(root, "main") != 1 || countElements(root, "footer") != 1 {
		t.Fatalf("stable shell landmarks = header %d aside %d main %d footer %d", countElements(root, "header"), countElements(root, "aside"), countElements(root, "main"), countElements(root, "footer"))
	}
	main := firstElement(root, "main")
	if main == nil || attr(main, "id") != "main-content" || attr(main, "aria-labelledby") != "page-title" {
		t.Fatalf("main landmark = %v, want the stable page outlet and heading relationship", main)
	}
	navigation := findElementByID(root, "workspace-navigation")
	if navigation == nil || attr(navigation, "aria-label") == "" {
		t.Fatal("application navigation lost its accessible landmark")
	}
	if linkForRoute(navigation, Path(PagePeople)) == nil {
		t.Fatal("authorized People route is not discoverable in the shell")
	}
	focusable := collectFocusable(root)
	if len(focusable) == 0 || attr(focusable[0], "href") != "#main-content" {
		t.Fatal("skip link is not the first focusable shell control")
	}
	announcer := findClass(root, "route-announcer")
	if announcer == nil || attr(announcer, "role") != "status" || attr(announcer, "aria-live") != "polite" {
		t.Fatal("route status announcer is not polite and atomic")
	}

	// The shell projection is fail-closed: an admitted identity with no
	// workforce role retains only safe baseline destinations.
	restricted := ApplyRoleVisibility(NewView(PagePeople, "tenant-test", "Taylor", "manager"), []string{})
	restrictedDoc, err := Render(restricted)
	if err != nil {
		t.Fatal(err)
	}
	restrictedRoot, err := xhtml.Parse(strings.NewReader(restrictedDoc))
	if err != nil {
		t.Fatal(err)
	}
	if linkForRoute(findElementByID(restrictedRoot, "workspace-navigation"), Path(PagePeople)) != nil {
		t.Fatal("role-free navigation disclosed the People route")
	}

	contentLoading, err := ui.RenderToString(BuildContentLoading(view))
	if err != nil {
		t.Fatal(err)
	}
	refreshing, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	contentRoot, err := xhtml.Parse(strings.NewReader(contentLoading))
	if err != nil {
		t.Fatal(err)
	}
	if shell := findClass(contentRoot, "is-content-loading"); shell == nil || attr(firstElement(contentRoot, "main"), "aria-busy") != "true" {
		t.Fatal("cross-route loading did not keep the resolved shell busy")
	}
	if findClass(contentRoot, "viewer-profile-loading") != nil || findClass(contentRoot, "notification-loading") != nil {
		t.Fatal("cross-route loading replaced stable identity or attention slots")
	}
	refreshingRoot, err := xhtml.Parse(strings.NewReader(refreshing))
	if err != nil {
		t.Fatal(err)
	}
	if stage := findClass(refreshingRoot, "network-stage"); stage == nil || attr(stage, "data-network-state") != "refreshing" || findClass(refreshingRoot, "loading-table-layout") != nil {
		t.Fatal("warm refresh replaced the authorized outlet with a cold proxy")
	}
}

// TestTodo_WEB_037_Golden pins the deterministic bytes of the stable chrome,
// deliberately excluding stylesheet and page-specific leaf churn.
func TestTodo_WEB_037_Golden(t *testing.T) {
	view, outlet := web037StableShellFixture(PageHome)
	doc, err := ui.RenderToString(BuildShell(view, outlet, true))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(doc))
	got := hex.EncodeToString(digest[:])
	// Closed launcher omits active-option references and does not claim modality.
	// UXAUDIT-007 removed the page-identity header's unconditional
	// "Acting as yourself" span (see PageIdentityHeader): acting-context
	// notices now come from exactly one place, ActingAuthorityBanner, gated
	// on the server-resolved authority projection. That removal changes
	// this stable-chrome byte pin even though the fixture carries no
	// authority projection at all, so it was re-pinned to the new bytes.
	// UXAUDIT-006 reworded the header's "Authenticated scope" fallback to
	// "Workspace access" (task language, not an internal-sounding label),
	// which also touches this fixture's stable chrome, so it is re-pinned
	// again.
	// UXAUDIT-003 changed the shell action launcher: its closed dialog no
	// longer embeds its results list at all (previously the two bare page
	// destinations; a per-worker ranked action would otherwise leak into
	// every page's markup regardless of whether the control was ever
	// opened -- see the results-gate comment in action_launcher.go and
	// the WEB-067/WEB-072 regression that caught it). This fixture's view
	// carries no PersonWorkflows or People, so the launcher also now
	// labels itself "Go to" rather than "Start an action" (no ranked
	// action survives, so the control does not claim it starts one).
	// Verified by inspecting the launcher's rendered markup directly
	// before re-pinning: trigger and dialog scaffold intact, no leaked
	// per-record content, no claimed modality.
	// UIPOLISH-004 renders ".main-scroll", ".primary-nav" and ".sidebar"
	// through the new shared ScrollRegion component: each gains
	// tabIndex="0" (GREEN requires every scroll region be keyboard-
	// reachable; none of the three carried it before), ".primary-nav" also
	// gains id="primary-nav" (the scroll-restoration key), and the inlined
	// stylesheet gains ScrollRegion's shared focus-visible/reduced-motion
	// rules plus ".main-scroll"'s missing scrollbar-width/scrollbar-color
	// tokens. No other markup changed -- verified by diffing this fixture's
	// rendered document against the pre-change output directly (aria-label/
	// aria-labelledby values, landmark counts, and every other attribute
	// are byte-identical) before re-pinning.
	const want = "951a06ce729f18e4c21fb073bf0a491fd5a4e35af1759319141bd86abe79e54d"
	if got != want {
		t.Fatalf("stable shell golden digest = %s, want %s", got, want)
	}
}

// web037StableShellFixture composes the production shell with the smallest
// semantic route outlet. It keeps shell navigation measurements independent
// from each leaf's data volume while still exercising shared GWC components.
func web037StableShellFixture(page PageID) (View, ui.Node) {
	view := ApplyRoleVisibility(NewView(page, "tenant-web037", "Taylor", "manager"), []string{"manager"})
	view.Navigate = func(string) {}
	outlet := html.Section(html.Props{
		ID: "web037-route-outlet",
		Aria: map[string]string{
			"label": "Route outlet",
		},
		Raw: map[string]any{"data-route": Path(page)},
	}, html.H2(html.Props{}, ui.Text("Route content")))
	return view, outlet
}

// TestTodo_WEB_037_Conformance proves the server document and the WASM mount
// consume the same GWC component tree. The body emitted by Render must be
// byte-identical to rendering Build directly, which is what Mount invokes in
// the js/wasm build.
func TestTodo_WEB_037_Conformance(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PageSettings} {
		view := testView(page)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		bodyStart := strings.Index(doc, "<body>")
		bodyEnd := strings.LastIndex(doc, "</body>")
		if bodyStart < 0 || bodyEnd <= bodyStart {
			t.Fatalf("%s SSR document has no body boundary", page)
		}
		body := doc[bodyStart+len("<body>") : bodyEnd]
		mounted, err := ui.RenderToString(Build(view))
		if err != nil {
			t.Fatal(err)
		}
		if body != mounted {
			t.Fatalf("%s SSR/WASM GWC body differs", page)
		}
	}
}

func findElementByID(root *xhtml.Node, id string) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && attr(node, "id") == id {
			found = node
		}
	})
	return found
}

func findClass(root *xhtml.Node, class string) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found != nil {
			return
		}
		for _, value := range strings.Fields(attr(node, "class")) {
			if value == class {
				found = node
				return
			}
		}
	})
	return found
}

func linkForRoute(root *xhtml.Node, route string) *xhtml.Node {
	if root == nil {
		return nil
	}
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && node.Data == "a" && attr(node, "href") == route {
			found = node
		}
	})
	return found
}
