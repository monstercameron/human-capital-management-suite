package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-040: the shell has no global action launcher yet, so every
// assertion below must fail before implementation and pass after.

func TestActionLauncherComboboxReflectsVisibleResults(t *testing.T) {
	for _, tc := range []struct{ query, expanded string }{{"", "false"}, {"people", "true"}, {"zzzznotfound", "false"}} {
		doc, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{
			InitialQuery: tc.query, Items: []ActionLauncherItem{{ID: "people", Label: "People", Href: "/workspace/app/people"}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		input := findElementByID(root, "action-launcher-input")
		if input == nil || attr(input, "aria-expanded") != tc.expanded {
			t.Fatalf("query %q has incorrect expansion state", tc.query)
		}
		if tc.expanded == "false" && (attr(input, "aria-activedescendant") != "" || attr(input, "aria-controls") != "") {
			t.Fatalf("query %q references hidden/missing results", tc.query)
		}
		if tc.query == "zzzznotfound" {
			if !strings.Contains(doc, "No matching actions") || strings.Contains(doc, "No authorized action") {
				t.Fatal("filtered empty state implies missing authorization")
			}
			empty := findElementByID(root, "action-launcher-empty")
			if empty == nil || attr(empty, "role") != "status" {
				t.Fatal("no-results message is not an announced status")
			}
		}
	}
}

func TestTodo_WEB_040(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	launcher := findElementByID(root, "action-launcher")
	if launcher == nil {
		t.Fatal("shell rendered no global action launcher")
	}
	if linkForRoute(launcher, "/workspace/app/journeys") == nil {
		t.Fatal("launcher did not offer the authorized promotion start")
	}
	if linkForRoute(launcher, "/workspace/app/people") == nil {
		t.Fatal("launcher did not offer the authorized worker selection")
	}

	// A denied create grant removes the promotion start without removing the
	// authorized worker selection, and the launcher stays honest about it.
	denied := ApplyPagePermissions(testView(PageHome), []RolePagePermission{
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
	deniedLauncher := findElementByID(deniedRoot, "action-launcher")
	if deniedLauncher == nil {
		t.Fatal("launcher disappeared instead of rendering its honest empty state")
	}
	if linkForRoute(deniedLauncher, "/workspace/app/journeys") != nil {
		t.Fatal("launcher advertised a promotion start the identity cannot create")
	}
	if linkForRoute(deniedLauncher, "/workspace/app/people") == nil {
		t.Fatal("launcher hid the authorized worker selection")
	}

	// The launcher starts navigations only; it never executes an intent,
	// decision, or approval from a row.
	for _, href := range launcherHrefs(deniedLauncher) {
		if !strings.HasPrefix(href, "/workspace/app/") {
			t.Fatalf("launcher href %q escapes the application shell", href)
		}
		for _, verb := range []string{"execute", "decide", "approve", "complete", "resume"} {
			if strings.Contains(strings.ToLower(href), verb) {
				t.Fatalf("launcher href %q executes instead of navigating", href)
			}
		}
	}

	// Successive derivations from the same view agree; the launcher keeps no
	// mutable projection that a caller could poison between renders.
	first := actionLauncherProps(testView(PageHome))
	second := actionLauncherProps(testView(PageHome))
	if !reflect.DeepEqual(first.Items, second.Items) {
		t.Fatal("launcher derivation is not deterministic for one view")
	}
}

func launcherHrefs(root *xhtml.Node) []string {
	hrefs := []string{}
	if root == nil {
		return hrefs
	}
	walkElements(root, func(node *xhtml.Node) {
		if node.Data == "a" {
			for _, attr := range node.Attr {
				if attr.Key == "href" {
					hrefs = append(hrefs, attr.Val)
				}
			}
		}
	})
	return hrefs
}

// web040GoldenDigest is pinned from the GREEN implementation run.
// UXAUDIT-006 reworded page.people.subtitle -- rendered here as the launcher's
// "start:people" quick-action description -- to task language that no longer
// names the governed journey service; re-pinned to the new bytes.
const web040GoldenDigest = "18a77a993dccbcf60e086f647e3ad645315cb201ccbf2b15f1c1889fb22ca146"

func TestTodo_WEB_040_Golden(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	launcher := findElementByID(root, "action-launcher")
	if launcher == nil {
		t.Fatal("shell rendered no global action launcher")
	}
	var rendered strings.Builder
	if err := xhtml.Render(&rendered, launcher); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(rendered.String()))
	if got := hex.EncodeToString(digest[:]); got != web040GoldenDigest {
		t.Fatalf("launcher golden digest mismatch: got %s want %s", got, web040GoldenDigest)
	}
}

func TestTodo_WEB_040_Browser(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	launcher := findElementByID(root, "action-launcher")
	if launcher == nil {
		t.Fatal("shell rendered no global action launcher")
	}
	var trigger *xhtml.Node
	walkElements(launcher, func(node *xhtml.Node) {
		if trigger == nil && node.Data == "button" && attr(node, "id") == "action-launcher-trigger" {
			trigger = node
		}
	})
	if trigger == nil {
		t.Fatal("launcher trigger is not a keyboard-operable button")
	}
	if strings.TrimSpace(attr(trigger, "aria-label")) == "" && strings.TrimSpace(attr(trigger, "aria-labelledby")) == "" {
		t.Fatal("launcher trigger has no accessible name")
	}
	var dialog *xhtml.Node
	walkElements(launcher, func(node *xhtml.Node) {
		if dialog == nil && attr(node, "role") == "dialog" {
			dialog = node
		}
	})
	if dialog == nil {
		t.Fatal("launcher panel is not exposed as a dialog")
	}
	if attr(dialog, "aria-modal") == "true" {
		t.Fatal("dismiss-on-blur launcher must not claim to trap modal focus")
	}
	if strings.TrimSpace(attr(dialog, "aria-label")) == "" && strings.TrimSpace(attr(dialog, "aria-labelledby")) == "" {
		t.Fatal("launcher dialog has no accessible name")
	}
	css := Stylesheet()
	for _, want := range []string{
		".action-launcher-trigger{", ".action-launcher-dialog{", ".action-launcher-dialog-hidden{display:none;}",
		"@media (forced-colors:active){.action-launcher-trigger{",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("launcher stylesheet missing %q", want)
		}
	}
}

func TestTodo_WEB_040_Conformance(t *testing.T) {
	// Narrow props: the launcher contract must never carry the page-wide View.
	if typeContains(reflect.TypeOf(ActionLauncherProps{}), reflect.TypeOf(View{}), map[reflect.Type]bool{}) {
		t.Fatal("ActionLauncherProps embeds the page-wide View instead of narrow props")
	}
	// Every locale renders named launcher copy; unresolved keys stay visible
	// as ⟦key⟧ and must never ship in the shell.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageHome), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		launcher := findElementByID(root, "action-launcher")
		if launcher == nil {
			t.Fatalf("locale %s rendered no global action launcher", locale)
		}
		var rendered strings.Builder
		if err := xhtml.Render(&rendered, launcher); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendered.String(), "⟦") {
			t.Fatalf("locale %s rendered an unresolved launcher message key", locale)
		}
	}
}
