package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-068: semantic action-availability states. Actions today
// are binary and decorative: the capability card always renders a live
// action link even when its state reads "Unavailable", and the
// visibility save button disables with no reason and no recovery path.
// Per the spec, a temporarily unavailable action whose existence is
// safe must show as unavailable with its reason (and where the viewer
// can resolve it, the way back) — never as a live link to something
// unavailable — while an unsafe existence hides the action entirely.
func TestTodo_WEB_068(t *testing.T) {
	for _, availability := range []struct {
		allowed       bool
		existenceSafe bool
		want          ActionAvailability
	}{
		{true, true, ActionAvailable},
		{true, false, ActionAvailable},
		{false, true, ActionUnavailable},
		{false, false, ActionHidden},
	} {
		if got := ResolveActionAvailability(availability.allowed, availability.existenceSafe); got != availability.want {
			t.Fatalf("ResolveActionAvailability(%t, %t) = %q, want %q", availability.allowed, availability.existenceSafe, got, availability.want)
		}
	}

	// The admin page marks unavailable capabilities as unavailable: the
	// reason renders, and no live link leads to the unavailable action.
	doc, err := Render(web068AdminView("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	body := web063BodyText(t, doc)
	for _, want := range []string{"connection didn't respond", "Page configuration isn't available"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page shows no unavailability reason for %q", want)
		}
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	journeysCard := findCardByTitle(root, "Journey service")
	if journeysCard == nil {
		t.Fatal("admin page loses the journey card")
	}
	if href := actionHrefIn(journeysCard); href != "" {
		t.Fatalf("unavailable journey action keeps live link %q", href)
	}
	studioCard := findCardByTitle(root, "Experience configuration")
	if studioCard == nil {
		t.Fatal("admin page loses the studio card")
	}
	if href := actionHrefIn(studioCard); href != "" {
		t.Fatalf("unavailable studio action keeps live link %q", href)
	}
	rolesCard := findCardByTitle(root, "Roles & access")
	if rolesCard == nil {
		t.Fatal("admin page loses the roles card")
	}
	if href := actionHrefIn(rolesCard); href == "" {
		t.Fatal("available roles action loses its live link")
	}

	// A hidden capability leaves the page entirely.
	hiddenDoc, err := ui.RenderToString(ui.CreateElement(AdminPage, AdminPageProps{
		Hero: AdminHeroProps{Title: "Cell"},
		Capabilities: []CapabilityCardProps{
			{Title: "Visible", State: "Available", Action: ActionLinkProps{Label: "Open", Href: "/open"}},
			{Title: "Concealed", State: "Unavailable", Availability: ActionState{Availability: ActionHidden}, Action: ActionLinkProps{Label: "Open", Href: "/secret"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hiddenDoc, "Concealed") || strings.Contains(hiddenDoc, "/secret") {
		t.Fatal("hidden capability keeps markup on the admin page")
	}
	if !strings.Contains(hiddenDoc, "Visible") {
		t.Fatal("admin page loses the available capability")
	}

	// The visibility save without the update grant explains itself: a
	// disabled button, the reason, and the way back — never a live save.
	saveDoc, err := Render(web068VisibilityView("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	saveBody := web063BodyText(t, saveDoc)
	if !strings.Contains(saveBody, "update grant") {
		t.Fatal("unavailable save shows no reason")
	}
	saveRoot, err := xhtml.Parse(strings.NewReader(saveDoc))
	if err != nil {
		t.Fatal(err)
	}
	if !unavailableActionHasRecovery(saveRoot, "Manage roles") {
		t.Fatal("unavailable save shows no recovery path in its actions block")
	}
	if !hasDisabledSubmit(saveRoot) {
		t.Fatal("unavailable save is not a disabled button")
	}
}

// findCardByTitle returns the admin-card section naming a capability.
func findCardByTitle(root *xhtml.Node, title string) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "section" {
			isCard := false
			for _, attr := range node.Attr {
				if attr.Key == "class" {
					for _, field := range strings.Fields(attr.Val) {
						if field == "admin-card" {
							isCard = true
						}
					}
				}
			}
			if isCard && strings.Contains(textContent(node), title) {
				found = node
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

// actionHrefIn returns the first link href inside a subtree, if any.
func actionHrefIn(root *xhtml.Node) string {
	var href string
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if href != "" {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "a" {
			for _, attr := range node.Attr {
				if attr.Key == "href" {
					href = attr.Val
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return href
}

// unavailableActionHasRecovery reports a recovery link naming label
// inside the data-action-state="unavailable" actions block.
func unavailableActionHasRecovery(root *xhtml.Node, label string) bool {
	recovered := false
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "div" {
			for _, attr := range node.Attr {
				if attr.Key == "data-action-state" && attr.Val == "unavailable" {
					if strings.Contains(textContent(node), label) && actionHrefIn(node) != "" {
						recovered = true
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return recovered
}

// hasDisabledSubmit reports a disabled submit button in the subtree.
func hasDisabledSubmit(root *xhtml.Node) bool {
	disabled := false
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "button" {
			submit, off := false, false
			for _, attr := range node.Attr {
				if attr.Key == "type" && attr.Val == "submit" {
					submit = true
				}
				if attr.Key == "disabled" {
					off = true
				}
			}
			if submit && off {
				disabled = true
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return disabled
}

// Golden: the availability-resolution matrix digest.
func TestTodo_WEB_068_Golden(t *testing.T) {
	var builder strings.Builder
	for _, allowed := range []bool{false, true} {
		for _, safe := range []bool{false, true} {
			builder.WriteString(boolWord(allowed))
			builder.WriteString(",")
			builder.WriteString(boolWord(safe))
			builder.WriteString("\x00")
			builder.WriteString(string(ResolveActionAvailability(allowed, safe)))
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "6ea2df1caccefbbddb8d2dbb66c104868bd77402a7abb83eee1747a53b596444"
	if got != want {
		t.Fatalf("availability matrix digest = %s, want %s", got, want)
	}
}

func boolWord(value bool) string {
	if value {
		return "allowed"
	}
	return "denied"
}

// Browser: unavailable cards carry machine-readable state, live links
// only where available, and no positive tabindex stops.
func TestTodo_WEB_068_Browser(t *testing.T) {
	doc, err := Render(web068AdminView("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if count := countDataState(root, "unavailable"); count != 2 {
		t.Fatalf("admin page marks %d unavailable actions, want 2", count)
	}
	var positive int
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
					positive++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if positive != 0 {
		t.Fatalf("admin page carries %d positive tabindex stops", positive)
	}
}

// countDataState counts nodes carrying data-action-state=value.
func countDataState(root *xhtml.Node, value string) int {
	count := 0
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "data-action-state" && attr.Val == value {
					count++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return count
}

// Conformance: availability copy in three locales, determinism.
func TestTodo_WEB_068_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, doc := range []string{renderToString(t, web068AdminView(locale)), renderToString(t, web068VisibilityView(locale))} {
			if strings.Contains(doc, "⟦") {
				t.Fatalf("%s availability render leaks an unresolved key", locale)
			}
		}
	}
	firstAvailability, secondAvailability := ResolveActionAvailability(false, true), ResolveActionAvailability(false, true)
	if firstAvailability != secondAvailability {
		t.Fatal("availability resolution is nondeterministic")
	}
}

func renderToString(t *testing.T, view View) string {
	t.Helper()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// Security: unavailable actions expose no live href to the action in
// any catalog locale; the recovery path never points at the action.
func TestTodo_WEB_068_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		doc, err := Render(web068AdminView(locale))
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, title := range []string{"Journey service", "Experience configuration"} {
			card := findCardByTitle(root, title)
			if card == nil {
				t.Fatalf("%s admin page loses the %q card", locale, title)
			}
			if href := actionHrefIn(card); href != "" {
				t.Fatalf("%s unavailable %q keeps live link %q", locale, title, href)
			}
		}
	}
}

func web068AdminView(locale string) View {
	view := testView(PageAdmin)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.LoadError = "dial journeys: connection refused"
	return view
}

func web068VisibilityView(locale string) View {
	view := testView(PageOrganizationVisibility)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering"}}}
	view.EffectivePermissions = []RolePagePermission{{Page: PageOrganizationVisibility, View: true}}
	return view
}
