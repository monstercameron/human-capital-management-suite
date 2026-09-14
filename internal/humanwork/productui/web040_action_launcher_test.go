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

// itemWithHref finds the first item whose Href equals route.
func itemWithHref(items []ActionLauncherItem, route string) *ActionLauncherItem {
	for index := range items {
		if items[index].Href == route {
			return &items[index]
		}
	}
	return nil
}

// itemMentioning finds the first item whose Href or Description contains
// needle -- used to find a ranked, person-specific action without hand-
// coding its full href shape.
func itemMentioning(items []ActionLauncherItem, needle string) *ActionLauncherItem {
	for index := range items {
		if strings.Contains(items[index].Href, needle) || strings.Contains(items[index].Description, needle) || strings.Contains(items[index].Label, needle) {
			return &items[index]
		}
	}
	return nil
}

// TestTodo_WEB_040 is UXAUDIT-003's REGRESSION matrix entry: WEB-040 first
// gave the shell a launcher offering only the two bare page destinations;
// UXAUDIT-003 changed it to rank real per-worker actions from the same
// registry the People directory uses (page_people.go's
// personWorkflowActions) while still falling back to those destinations
// once no ranked action survives authorization. This test pins both eras'
// invariants: the destinations survive as a fallback, a denied create
// grant still removes only the action (never the destinations), and no
// href ever executes rather than navigates.
func TestTodo_WEB_040(t *testing.T) {
	view := testView(PageHome)
	props := actionLauncherProps(view)
	props.Items = authorizedActionLauncherItems(view, props.Items)

	if itemWithHref(props.Items, statefulHref(view, PageJourneys)) == nil {
		t.Fatal("launcher did not offer the authorized promotion start destination")
	}
	if itemWithHref(props.Items, statefulHref(view, PagePeople)) == nil {
		t.Fatal("launcher did not offer the authorized worker selection destination")
	}
	if item := itemMentioning(props.Items, "worker-avery"); item == nil || item.IsNavigationDestination {
		t.Fatal("launcher did not rank the eligible worker's own authorized promotion action")
	}

	// The ranked action embeds into the actual rendered markup only once
	// the launcher is open -- see the doc comment on the dialog's results
	// gate in action_launcher.go: an always-embedded per-worker item list
	// leaked worker names into every page's raw HTML regardless of
	// whether the control was ever opened (caught by WEB-067/WEB-072
	// while implementing this todo).
	opened, err := ui.RenderToString(ui.CreateElement(ActionLauncher, ActionLauncherProps{
		I18nProps: props.I18nProps, Items: props.Items, InitialQuery: "avery",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(opened, "worker-avery") {
		t.Fatalf("opened launcher did not render the matched worker's action:\n%s", opened)
	}
	if strings.Contains(opened, "Jordan Lee") {
		t.Fatal("a query for one worker rendered an unrelated worker's action")
	}
	closedDoc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	closedRoot, err := xhtml.Parse(strings.NewReader(closedDoc))
	if err != nil {
		t.Fatal(err)
	}
	closedLauncher := findElementByID(closedRoot, "action-launcher")
	if closedLauncher == nil {
		t.Fatal("shell rendered no global action launcher")
	}
	var closedMarkup strings.Builder
	if err := xhtml.Render(&closedMarkup, closedLauncher); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(closedMarkup.String(), "worker-avery") {
		t.Fatal("the closed launcher embedded a worker-specific action before anyone opened it")
	}

	// A denied create grant removes the promotion start without removing the
	// authorized worker selection, and the launcher stays honest about it.
	denied := ApplyPagePermissions(testView(PageHome), []RolePagePermission{
		{Version: 1, RoleID: "viewer", Page: PagePeople, View: true},
	})
	deniedProps := actionLauncherProps(denied)
	deniedProps.Items = authorizedActionLauncherItems(denied, deniedProps.Items)
	if itemWithHref(deniedProps.Items, statefulHref(denied, PageJourneys)) != nil {
		t.Fatal("launcher advertised a promotion start the identity cannot create")
	}
	if itemWithHref(deniedProps.Items, statefulHref(denied, PagePeople)) == nil {
		t.Fatal("launcher hid the authorized worker selection")
	}
	for _, item := range deniedProps.Items {
		if item.Href != "" && strings.Contains(item.Label+item.Description, "Avery Patel") {
			t.Fatalf("launcher offered a launchable action the identity cannot create: %+v", item)
		}
	}

	// The launcher starts navigations only; it never executes an intent,
	// decision, or approval from a row.
	for _, item := range deniedProps.Items {
		if item.Href == "" {
			continue
		}
		if !strings.HasPrefix(item.Href, "/workspace/app/") {
			t.Fatalf("launcher href %q escapes the application shell", item.Href)
		}
		for _, verb := range []string{"execute", "decide", "approve", "complete", "resume"} {
			if strings.Contains(strings.ToLower(item.Href), verb) {
				t.Fatalf("launcher href %q executes instead of navigating", item.Href)
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

// web040GoldenDigest is pinned from the GREEN implementation run.
// UXAUDIT-003 changed the closed launcher's dialog to omit its results
// list entirely (see the results-gate doc comment in action_launcher.go)
// so a per-worker action never embeds into a page's markup before the
// viewer opens the control; re-pinned to the new, smaller closed-state
// bytes after visually confirming the trigger, dialog scaffold and input
// still render correctly.
// PROMOUX-015 re-pin: the global search and action launcher inputs gained a visually hidden associated label; that label is the only change.
const web040GoldenDigest = "dd3493fccf3776482a02429339fc924ce001a6042fd8494daf8a488ecf2a791d"

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
