package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-059: logout and revocation convergence. When the server
// projects a signed-out state, the shell must converge every authority
// surface to revoked: the session warning, step-up prompt, authority
// banner, break-glass prompt, simulation panel, context switcher, and
// delegation selector all render nothing — even with stale projections
// still on the view — and the converged signed-out panel (detail, revoked
// grants, sign-in path) becomes the content. The panel invents nothing:
// an unsafe sign-in destination means no link.
func TestTodo_WEB_059(t *testing.T) {
	view := testView(PageHome)
	view.SessionWarning = &SessionWarningProps{Detail: "Detail.", ReauthHref: "/workspace/app/settings"}
	view.StepUpChallenge = &StepUpChallengeProps{ActionLabel: "Act.", ReasonDetail: "Why.", ChallengeHref: "/workspace/app/journeys"}
	view.ContextSwitcher = web056Fixture()
	view.BreakGlassActivation = &BreakGlassActivationProps{IncidentRef: "INC-2026-118", ActivateHref: "/workspace/app/journeys"}
	view.PolicySimulation = &PolicySimulationProps{Subject: "Avery Patel (manager)", ExitHref: "/workspace/app/home"}
	view.SignedOut = &SignedOutProps{
		Detail:     "You signed out. Every grant on this device is revoked.",
		Revoked:    []string{"Delegation from Maya Chen", "Break-glass INC-2026-118"},
		SignInHref: "/workspace/login",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	panel := findElementByID(root, "signed-out")
	if panel == nil {
		t.Fatal("projected signed-out state renders no panel")
	}
	body := textContent(panel)
	for _, want := range []string{"Every grant on this device is revoked", "Delegation from Maya Chen", "Break-glass INC-2026-118"} {
		if !strings.Contains(body, want) {
			t.Fatalf("signed-out panel missing %q: %q", want, body)
		}
	}
	signin := findFirst(panel, "a")
	if signin == nil {
		t.Fatal("signed-out panel has no sign-in link")
	}
	if href := xhtmlAttr(signin, "href"); href != "/workspace/login" {
		t.Fatalf("sign-in link = %q, want the projected sign-in destination", href)
	}
	// Convergence: no authority surface survives, stale projections or not.
	for _, id := range []string{"session-warning", "step-up-challenge", "acting-authority", "break-glass-activation", "policy-simulation"} {
		if findElementByID(root, id) != nil {
			t.Fatalf("signed-out shell still renders #%s", id)
		}
	}
	var liveChrome []string
	var sweep func(node *xhtml.Node)
	sweep = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data != "style" && node.Data != "script" {
			for _, attr := range node.Attr {
				if attr.Key == "class" && (strings.Contains(attr.Val, "context-switcher") || strings.Contains(attr.Val, "delegation-selector")) {
					liveChrome = append(liveChrome, attr.Val)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			sweep(child)
		}
	}
	sweep(root)
	if len(liveChrome) > 0 {
		t.Fatalf("signed-out shell still renders authority chrome: %q", liveChrome)
	}
	// The panel is the content: no tenant page behind it.
	if leaked := textContent(root); strings.Contains(leaked, "Promotion journey") {
		t.Fatal("signed-out shell leaks tenant content behind the panel")
	}

	// No projection means the normal shell, untouched.
	plainDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	plainRoot, err := xhtml.Parse(strings.NewReader(plainDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(plainRoot, "signed-out") != nil {
		t.Fatal("shell renders a signed-out panel without a projection")
	}
}

// Golden: the signed-out panel for a fixed projection.
func TestTodo_WEB_059_Golden(t *testing.T) {
	node, err := ui.RenderToString(SignedOut(web059Fixture()))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	if got := hex.EncodeToString(digest[:]); got != "21b09c50a06720ecfcef64c3935a7fd8b3201046de7380daf4bf658e8f1fce58" {
		t.Fatalf("signed-out golden mismatch:\n%s\nwant digest 21b09c50a06720ecfcef64c3935a7fd8b3201046de7380daf4bf658e8f1fce58", node)
	}
}

// Browser: the panel parses as a labelled section with a projected
// sign-in destination and no positive tabindex stops.
func TestTodo_WEB_059_Browser(t *testing.T) {
	view := testView(PageHome)
	view.SignedOut = &SignedOutProps{
		Detail:     "You signed out.",
		Revoked:    []string{"Delegation from Maya Chen"},
		SignInHref: "/workspace/login",
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	panel := findElementByID(root, "signed-out")
	if panel == nil {
		t.Fatal("rendered document has no signed-out panel")
	}
	labelledBy := xhtmlAttr(panel, "aria-labelledby")
	if labelledBy == "" || findElementByID(root, labelledBy) == nil {
		t.Fatalf("signed-out panel labelling element %q missing", labelledBy)
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
	walk(panel)
	if positive != 0 {
		t.Fatalf("signed-out panel carries %d positive tabindex stops", positive)
	}
}

// Conformance: locales, stylesheet, fail-closed sign-in.
func TestTodo_WEB_059_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Signed out", "de-DE": "Abgemeldet", "ar": "تم تسجيل الخروج"} {
		props := web059Fixture()
		props.I18nProps = I18nProps{Locale: ResolveProductLocale(locale)}
		node, err := ui.RenderToString(SignedOut(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, title) {
			t.Fatalf("%s panel missing title %q: %s", locale, title, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s panel leaks an unresolved key: %s", locale, node)
		}
	}
	// An unsafe sign-in destination means no link, but the panel stands.
	unsafe := web059Fixture()
	unsafe.SignInHref = "javascript:login()"
	unsafeNode, err := ui.RenderToString(SignedOut(unsafe))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unsafeNode, "javascript:") || strings.Contains(unsafeNode, "signed-out-signin") {
		t.Fatalf("unsafe sign-in survives: %s", unsafeNode)
	}
	unsafe.SignInHref = "mailto:helpdesk@example.com?token=leak"
	unsafeNode, err = ui.RenderToString(SignedOut(unsafe))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unsafeNode, "token=leak") || strings.Contains(unsafeNode, "signed-out-signin") {
		t.Fatalf("credential-bearing sign-in survives: %s", unsafeNode)
	}
	css := Stylesheet()
	for _, want := range []string{".signed-out", ".signed-out-title", ".signed-out-detail", ".signed-out-revoked", ".signed-out-signin"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func TestSignedOutDeduplicatesRevokedGrantLabels(t *testing.T) {
	props := web059Fixture()
	props.Revoked = []string{"Delegation from Maya Chen", "  Delegation from Maya Chen  ", "Break-glass INC-2026-118"}
	node, err := ui.RenderToString(SignedOut(props))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(node, "Delegation from Maya Chen") != 1 {
		t.Fatalf("duplicate revoked grant rendered: %s", node)
	}
}

func web059Fixture() SignedOutProps {
	return SignedOutProps{
		I18nProps:  I18nProps{Locale: ResolveProductLocale("en-US")},
		Detail:     "You signed out. Every grant on this device is revoked.",
		Revoked:    []string{"Delegation from Maya Chen", "Break-glass INC-2026-118"},
		SignInHref: "/workspace/login",
	}
}
