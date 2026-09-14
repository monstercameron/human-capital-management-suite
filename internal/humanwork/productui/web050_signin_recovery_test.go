package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-050: accessible sign-in recovery. The entry gate must offer
// server-composed recovery options as a labelled link section, accept only
// safe destinations (https, workspace-relative, mailto), and fail closed
// on anything else without inventing a fallback control.
func TestTodo_WEB_050(t *testing.T) {
	props := FederationEntryProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("")},
		Entries: []FederationEntry{
			{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo"},
		},
		Recovery: []RecoveryOption{
			{Label: "Reset your password", Description: "Use the identity provider's password reset.", Href: "https://login.harborcare.example/reset"},
			{Label: "Contact the helpdesk", Description: "Mail the service desk.", Href: "mailto:helpdesk@harborcare.example"},
			{Label: "Evil", Description: "Must never render.", Href: "javascript:alert(1)"},
		},
	}
	node, err := ui.RenderToString(FederationEntryList(props))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(node))
	if err != nil {
		t.Fatal(err)
	}
	section := findElementByID(root, "federation-entry-recovery")
	if section == nil {
		t.Fatal("gate renders no recovery section")
	}
	if textContent(findFirst(section, "h2")) == "" {
		t.Fatal("recovery section has no heading")
	}
	links := collectElements(section, "a")
	if len(links) != 2 {
		t.Fatalf("recovery links = %d, want 2 (javascript: dropped)", len(links))
	}
	for _, link := range links {
		if href := xhtmlAttr(link, "href"); strings.HasPrefix(href, "javascript:") {
			t.Fatalf("recovery renders unsafe destination %q", href)
		}
		if textContent(link) == "" {
			t.Fatal("recovery link has no discernible text")
		}
	}

	// No valid option means no section — never a dead control.
	bare := props
	bare.Recovery = []RecoveryOption{{Label: "Evil", Href: "javascript:alert(1)"}}
	bareNode, err := ui.RenderToString(FederationEntryList(bare))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bareNode, "federation-entry-recovery") {
		t.Fatalf("gate renders a recovery section with no safe option: %s", bareNode)
	}
	emptyProps := FederationEntryProps{I18nProps: I18nProps{Locale: ResolveProductLocale("")}}
	emptyNode, err := ui.RenderToString(FederationEntryList(emptyProps))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(emptyNode, "federation-entry-recovery") {
		t.Fatalf("empty gate renders a recovery section: %s", emptyNode)
	}
}

// Golden: recovery section markup for a fixed option set.
func TestTodo_WEB_050_Golden(t *testing.T) {
	props := FederationEntryProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("")},
		Recovery: []RecoveryOption{
			{Label: "Reset your password", Description: "Use the reset flow.", Href: "https://login.harborcare.example/reset"},
		},
	}
	section, err := ui.RenderToString(federationRecoverySection(props))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(section))
	got := hex.EncodeToString(digest[:])
	const want = "8e43b2bc29f918ae325120494f8a9731ecb060ef7f893b2136dc8d8ce0a707b8"
	if got != want {
		t.Fatalf("recovery section golden digest = %s, want %s", got, want)
	}
}

// Browser: recovery section placement inside the tenantless gate document.
func TestTodo_WEB_050_Browser(t *testing.T) {
	view := testView(PageHome)
	view.Tenant = ""
	view.FederationEntries = []FederationEntry{
		{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo"},
	}
	view.RecoveryOptions = []RecoveryOption{
		{Label: "Reset your password", Description: "Use the reset flow.", Href: "https://login.harborcare.example/reset"},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	gate := findElementByID(root, "federation-entry")
	if gate == nil {
		t.Fatal("tenantless document renders no gate")
	}
	section := findElementByID(root, "federation-entry-recovery")
	if section == nil {
		t.Fatal("gate document renders no recovery section")
	}
	if !isBeforeIn(gate, findFirst(gate, "h1"), section) {
		t.Fatal("recovery section is not placed after the gate title")
	}
}

// Conformance: locales and scheme policy.
func TestTodo_WEB_050_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Having trouble signing in?", "de-DE": "Probleme bei der Anmeldung?", "ar": "هل تواجه مشكلة في تسجيل الدخول؟"} {
		props := FederationEntryProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale(locale)},
			Recovery: []RecoveryOption{
				{Label: "Reset", Description: "Reset flow.", Href: "https://login.harborcare.example/reset"},
			},
		}
		node, err := ui.RenderToString(federationRecoverySection(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, title) {
			t.Fatalf("%s recovery section missing title %q: %s", locale, title, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s recovery section leaks an unresolved key: %s", locale, node)
		}
	}
	for href, valid := range map[string]bool{
		"https://idp.example/reset": true, "http://idp.example/reset": false,
		"/workspace/login/start?tenant=x": true, "//evil.example": false,
		"mailto:helpdesk@example.com": true, "javascript:alert(1)": false,
		"mailto:helpdesk@example.com?token=leak":      false,
		"https://idp.example/reset?access_token=leak": false,
		"https://idp.example/reset#access_token=leak": false,
		"https://":                      false,
		"mailto:":                       false,
		"https://idp.example/reset/%zz": false,
		"data:text/html,hi":             false, "": false, "ftp://files.example/x": false,
	} {
		if got := validRecoveryHref(href); got != valid {
			t.Fatalf("validRecoveryHref(%q) = %v, want %v", href, got, valid)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".federation-entry-recovery", ".federation-entry-recovery-list"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}
