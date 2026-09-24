package productui

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// These SECURITY cases exercise hostile projections at the rendered boundary.
func TestTodo_WEB_049_Security(t *testing.T) {
	props := FederationEntryProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Entries: []FederationEntry{
		{Tenant: "trusted", Issuer: "https://idp.example", Protocol: "oidc", Href: "//evil.example/login"},
		{Tenant: "trusted", Issuer: "https://idp.example", Protocol: "oidc", Href: "/workspace/login?tenant=trusted&access_token=secret"},
		{Tenant: "trusted", Issuer: "https://idp.example", Protocol: "oidc", Href: "/workspace/login?tenant=trusted"},
	}}
	node, err := ui.RenderToString(FederationEntryList(props))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(node))
	if err != nil {
		t.Fatal(err)
	}
	links := collectElements(root, "a")
	if len(links) != 1 || xhtmlAttr(links[0], "href") != "/workspace/login?tenant=trusted" {
		t.Fatalf("federation accepted unsafe destinations: %v", links)
	}
	if strings.Contains(textContent(root), "secret") {
		t.Fatal("federation projection disclosed credential material")
	}
}

func TestTodo_WEB_050_Security(t *testing.T) {
	for _, href := range []string{"javascript:alert(1)", "//evil.example/x", "http://idp.example/reset", "/workspace/x?token=abc", "https://idp.example/reset?client_secret=s"} {
		if validRecoveryHref(href) {
			t.Errorf("unsafe recovery destination accepted: %q", href)
		}
	}
	if !validRecoveryHref("https://idp.example/reset") || !validRecoveryHref("/workspace/login/start") {
		t.Fatal("safe recovery destinations rejected")
	}
}

func TestTodo_WEB_051_Security(t *testing.T) {
	for _, roles := range [][]string{nil, {"worker_self"}, {RoleHCMAdmin}} {
		view := ApplyRoleVisibility(testView(PageAdmin), roles)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !PageVisible(PageAdmin, roles) {
			for _, path := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/organization-visibility", "/workspace/app/admin/worker-ids"} {
				if strings.Contains(doc, path) {
					t.Fatalf("unauthorized admin route %q rendered for %v", path, roles)
				}
			}
		}
		if PageVisible(PageAdmin, roles) && !strings.Contains(doc, "Roles &amp; access") {
			t.Fatalf("authorized admin surface omitted its capability projection for %v", roles)
		}
		if !PageVisible(PageRoles, roles) && strings.Contains(doc, "/workspace/app/admin/roles") {
			t.Fatalf("unauthorized role link leaked for %v", roles)
		}
	}
}

func TestTodo_WEB_052_Security(t *testing.T) {
	doc, err := Render(web060View("credential-hrefs", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	warning := findElementByID(root, "session-warning")
	if warning == nil || !strings.Contains(textContent(warning), "Detail.") {
		t.Fatal("session warning was removed with its unsafe reauthentication link")
	}
	for _, link := range collectElements(warning, "a") {
		href := xhtmlAttr(link, "href")
		if !validRecoveryHref(href) || credentialParamInHref(href) {
			t.Fatalf("unsafe reauthentication link survived: %q", href)
		}
	}
}

func TestTodo_WEB_053_Security(t *testing.T) {
	got := reauthResumeHref(testView(PageHistory), "/workspace/app/settings")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	resume, err := url.Parse(parsed.Query().Get("resume"))
	if err != nil || resume.Path != pageHref(PageHistory) {
		t.Fatalf("resume target = %q, want registered history route", parsed.Query().Get("resume"))
	}
	if credentialParamInHref(got) {
		t.Fatalf("resume URL carries credential material: %q", got)
	}
}

func TestTodo_WEB_054_Security(t *testing.T) {
	doc, err := Render(web060View("credential-hrefs", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	prompt := findElementByID(root, "step-up-challenge")
	if prompt == nil || !strings.Contains(textContent(prompt), "Approve promotion.") {
		t.Fatal("step-up challenge lost its action context")
	}
	if len(collectElements(prompt, "a")) != 0 {
		t.Fatal("credential-bearing step-up destination survived")
	}
}

func TestTodo_WEB_055_Security(t *testing.T) {
	props := web055Fixture()
	props.Options = append(props.Options, AuthorityContextOption{TenantID: "forged", TenantName: "Intruder", ActingContextID: "forged-context", ActingContextName: "Intruder", Delegated: true})
	node, err := ui.RenderToString(DelegationSelector(props))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(node, "forged") || strings.Contains(node, "Intruder") {
		t.Fatalf("malformed delegation grant was exposed: %s", node)
	}
}

func TestTodo_WEB_056_Security(t *testing.T) {
	doc, err := Render(web060View("session", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	banner := findElementByID(root, "acting-authority")
	if banner == nil || !strings.Contains(textContent(banner), "Maya Chen") {
		t.Fatal("active delegated authority is not persistently identified")
	}
	if strings.Contains(textContent(banner), "grant-opaque-123") {
		t.Fatal("opaque grant identifier leaked into authority banner")
	}
}

func TestTodo_WEB_057_Security(t *testing.T) {
	doc, err := Render(web060View("credential-hrefs", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	activation := findElementByID(root, "break-glass-activation")
	if activation == nil || !strings.Contains(textContent(activation), "INC-2026-118") {
		t.Fatal("break-glass incident context disappeared")
	}
	if len(collectElements(activation, "a")) != 0 {
		t.Fatal("credential-bearing activation destination survived")
	}
}

func TestTodo_WEB_058_Security(t *testing.T) {
	doc, err := Render(web060View("credential-hrefs", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	simulation := findElementByID(root, "policy-simulation")
	if simulation == nil || !strings.Contains(textContent(simulation), "Avery Patel") || !strings.Contains(textContent(simulation), "Denied") {
		t.Fatal("view-as simulation lost its subject or decision")
	}
	for _, link := range collectElements(simulation, "a") {
		if credentialParamInHref(xhtmlAttr(link, "href")) {
			t.Fatal("simulation exit leaked credentials")
		}
	}
}

func TestTodo_WEB_059_Security(t *testing.T) {
	doc, err := Render(web060View("signed-out", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	panel := findElementByID(root, "signed-out")
	if panel == nil || !strings.Contains(textContent(panel), "signed out") {
		t.Fatal("revocation did not converge on signed-out state")
	}
	for _, link := range collectElements(panel, "a") {
		if credentialParamInHref(xhtmlAttr(link, "href")) {
			t.Fatal("signed-out recovery link leaked a credential")
		}
	}
}

func TestTodo_WEB_060_Security(t *testing.T) {
	for _, config := range []string{"session", "credential-hrefs", "signed-out", "entry"} {
		doc, err := Render(web060View(config, "en-US"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(doc), "access_token") || strings.Contains(strings.ToLower(doc), "client_secret") || strings.Contains(doc, "stolen") {
			t.Fatalf("%s rendered telemetry-sensitive credential data", config)
		}
	}
}

func TestTodo_WEB_061_Security(t *testing.T) {
	values := map[string]string{"salary": "120000", "name": "Avery"}
	before := map[string]string{"salary": "120000", "name": "Avery"}
	got := ProjectAuthorizedRecord(ResolveProductLocale("en-US"), "worker", values, &AuthorizedRecord{ID: "worker", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationAllow}, "salary": {Effect: PresentationEffect("allow-all")}}})
	if got.Values["salary"].Text == "120000" || got.Values["salary"].Text != "Withheld" {
		t.Fatalf("unknown effect failed open: %#v", got.Values["salary"])
	}
	if !reflect.DeepEqual(values, before) {
		t.Fatal("authorization projection mutated source record")
	}
}

func TestTodo_WEB_062_Security(t *testing.T) {
	verdicts := map[string]AuthorizedRecord{"allowed": {ID: "allowed", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationDenied}}}, "denied": {ID: "denied", Disclosable: false}}
	if !DiscoveryAdmitted("allowed", verdicts) || DiscoveryAdmitted("missing", verdicts) || DiscoveryAdmitted("denied", verdicts) {
		t.Fatal("discovery admitted missing or undisclosable records")
	}
	if got := DiscoveryLabel(ResolveProductLocale("en-US"), "allowed", "Private Name", "name", verdicts); got == "Private Name" {
		t.Fatal("discovery label leaked a denied name")
	}
}

func TestTodo_WEB_063_Security(t *testing.T) {
	view := testView(PagePerson)
	view.People = []Person{web063Person("worker-avery")}
	view.RecordVerdicts = map[string]AuthorizedRecord{"worker-avery": {ID: "worker-avery", Disclosable: false, DenialReason: "No record access."}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Avery Patel") || strings.Contains(doc, "120,000") {
		t.Fatal("record-level denial left identity or compensation in the page")
	}
}

func TestTodo_WEB_064_Security(t *testing.T) {
	available := []string{"Finance", "Engineering"}
	for _, mode := range []string{"ROOT", "SUPERUSER", "OWN_UNIT", ""} {
		if got := ResolveOrganizationScope(mode, []string{"Finance"}, available); len(got) != 0 {
			t.Errorf("untrusted mode %q invented scope %v", mode, got)
		}
	}
	if got := ResolveOrganizationScope("ALLOWLIST", []string{"unknown"}, available); len(got) != 0 {
		t.Fatalf("unknown unit was admitted: %v", got)
	}
}

func TestTodo_WEB_229_Security(t *testing.T) {
	for _, roles := range [][]string{nil, {"worker_self"}, {"manager"}} {
		doc, err := Render(ApplyRoleVisibility(testView(PageAdmin), roles))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/organization-visibility", "/workspace/app/admin/worker-ids"} {
			if strings.Contains(doc, path) {
				t.Errorf("non-admin %v received privileged route %q", roles, path)
			}
		}
	}
}

func TestTodo_WEB_229_Integration(t *testing.T) {
	admin, err := Render(ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := Render(ApplyRoleVisibility(testView(PageAdmin), []string{"worker_self"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(admin, "/workspace/app/admin/roles") || strings.Contains(worker, "/workspace/app/admin/roles") {
		t.Fatal("admin home projection diverged from registered page authorization")
	}
}

func TestTodo_WEB_229_Fault(t *testing.T) {
	for _, roles := range [][]string{nil, {"unknown"}} {
		doc, err := Render(ApplyRoleVisibility(testView(PageAdmin), roles))
		if err != nil {
			t.Fatalf("role fault prevented safe rendering: %v", err)
		}
		if strings.Contains(doc, "/workspace/app/admin/") {
			t.Fatalf("fail-closed role projection exposed admin controls for %v", roles)
		}
	}
}
