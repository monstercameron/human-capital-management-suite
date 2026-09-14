package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-049: tenant-federation entry experience. An identity without
// a tenant must reach an entry gate listing the server-composed federation
// options (tenant-grouped issuer links with protocol metadata) instead of
// tenant-scoped content, and an honest empty state when no issuer is
// configured. The frontend never authors issuers, protocols, or
// destinations: entries arrive as data.
func TestTodo_WEB_049(t *testing.T) {
	entries := []FederationEntry{
		{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo&issuer=https%3A%2F%2Flogin.harborcare.example"},
		{Tenant: "harborcare-demo", Issuer: "https://saml.harborcare.example", Protocol: "saml", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo&issuer=https%3A%2F%2Fsaml.harborcare.example"},
		{Tenant: "northwind", Issuer: "https://login.northwind.example", Protocol: "oidc", Assurance: "standard", Href: "/workspace/login/start?tenant=northwind&issuer=https%3A%2F%2Flogin.northwind.example"},
	}
	view := testView(PageHome)
	view.Tenant = ""
	view.FederationEntries = entries
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
		t.Fatal("tenantless document renders no federation entry gate")
	}
	if textContent(findFirst(gate, "h1")) == "" {
		t.Fatal("entry gate has no title")
	}
	links := collectElements(gate, "a")
	if len(links) != 3 {
		t.Fatalf("entry gate links = %d, want 3 issuer options", len(links))
	}
	for _, link := range links {
		parsed, err := url.Parse(xhtmlAttr(link, "href"))
		if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/workspace/") {
			t.Fatalf("entry link escapes the workspace: %q", xhtmlAttr(link, "href"))
		}
	}
	body := textContent(gate)
	for _, want := range []string{"harborcare-demo", "northwind", "oidc", "saml"} {
		if !strings.Contains(body, want) {
			t.Fatalf("entry gate missing %q", want)
		}
	}
	// Tenant-scoped chrome (page header, outlet) stays out of a tenantless
	// document; the gate is the content.
	if findPageHead(root) != nil {
		t.Fatal("tenantless document renders tenant-scoped page header")
	}

	// No configured issuer is an honest empty state, not an empty page.
	emptyView := testView(PageHome)
	emptyView.Tenant = ""
	emptyDoc, err := Render(emptyView)
	if err != nil {
		t.Fatal(err)
	}
	emptyRoot, err := xhtml.Parse(strings.NewReader(emptyDoc))
	if err != nil {
		t.Fatal(err)
	}
	emptyGate := findElementByID(emptyRoot, "federation-entry")
	if emptyGate == nil {
		t.Fatal("tenantless document without issuers renders no gate")
	}
	if len(collectElements(emptyGate, "a")) != 0 {
		t.Fatal("empty gate advertises destinations")
	}
	if !strings.Contains(textContent(emptyGate), "No federated sign-in") {
		t.Fatalf("empty gate is not honest: %q", textContent(emptyGate))
	}

	// A tenanted identity never sees the gate.
	tenantedDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	tenantedRoot, err := xhtml.Parse(strings.NewReader(tenantedDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(tenantedRoot, "federation-entry") != nil {
		t.Fatal("tenanted document renders the entry gate")
	}
}

// Golden: the entry gate for a fixed entry set.
func TestTodo_WEB_049_Golden(t *testing.T) {
	props := FederationEntryProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("")},
		Entries: []FederationEntry{
			{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo"},
		},
	}
	node, err := ui.RenderToString(FederationEntryList(props))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	got := hex.EncodeToString(digest[:])
	const want = "b5f9cf6576fea8f27e09fe48256cf4292d01c4c335bc5c68e140e6b3832ce3e7"
	if got != want {
		t.Fatalf("federation entry golden digest = %s, want %s", got, want)
	}
}

// Browser: gate placement and REPLACEMENT of tenant content.
func TestTodo_WEB_049_Browser(t *testing.T) {
	view := testView(PageHistory)
	view.Tenant = ""
	view.FederationEntries = []FederationEntry{
		{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo"},
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
		t.Fatal("tenantless history document renders no gate")
	}
	mains := collectElements(root, "main")
	if len(mains) != 1 {
		t.Fatal("gate document renders no main landmark")
	}
	for _, main := range mains {
		for _, anchor := range collectElements(main, "a") {
			if href := xhtmlAttr(anchor, "href"); strings.HasPrefix(href, "/workspace/app/history") {
				t.Fatal("gate document leaks tenant-scoped history links into main")
			}
		}
	}
}

// Conformance: locales, grouping order, no second authority.
func TestTodo_WEB_049_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Choose where to sign in", "de-DE": "Wählen Sie, wo Sie sich anmelden", "ar": "اختر مكان تسجيل الدخول"} {
		props := FederationEntryProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale(locale)},
			Entries: []FederationEntry{
				{Tenant: "b-tenant", Issuer: "https://b.example", Protocol: "oidc", Assurance: "standard", Href: "/workspace/login/start?tenant=b-tenant"},
				{Tenant: "a-tenant", Issuer: "https://a.example", Protocol: "saml", Assurance: "substantial", Href: "/workspace/login/start?tenant=a-tenant"},
			},
		}
		node, err := ui.RenderToString(FederationEntryList(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, title) {
			t.Fatalf("%s gate missing title %q: %s", locale, title, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s gate leaks an unresolved key: %s", locale, node)
		}
		aIndex := strings.Index(node, "a-tenant")
		bIndex := strings.Index(node, "b-tenant")
		if aIndex < 0 || bIndex < 0 || bIndex < aIndex {
			t.Fatalf("%s gate does not group tenants deterministically: %s", locale, node)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".federation-entry", ".federation-entry-list", ".federation-entry-empty"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func TestFederationEntryDropsUnsafeOrIncompleteIssuerProjection(t *testing.T) {
	props := FederationEntryProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Entries: []FederationEntry{
			{Tenant: "", Issuer: "missing-tenant", Protocol: "oidc", Href: "/workspace/login/start"},
			{Tenant: "harborcare-demo", Issuer: "unsafe", Protocol: "oidc", Href: "javascript:alert(1)"},
			{Tenant: "harborcare-demo", Issuer: "leaky", Protocol: "oidc", Href: "/workspace/login/start?access_token=secret"},
		},
	}
	node, err := ui.RenderToString(FederationEntryList(props))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(node, "unsafe") || strings.Contains(node, "leaky") || strings.Contains(node, "javascript:") || strings.Contains(node, "access_token") {
		t.Fatalf("unsafe issuer projection survived: %s", node)
	}
	if !strings.Contains(node, "No federated sign-in") || !strings.Contains(node, `role="status"`) {
		t.Fatalf("filtered issuer projection did not become an honest empty state: %s", node)
	}
}
