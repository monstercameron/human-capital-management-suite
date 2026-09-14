package workspace

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_UXAUDIT_014_Browser exercises the browser-facing HTTP contract
// through a real loopback server. The complementary interactive browser matrix
// is recorded in the UI/UX devlog; this test does not execute a browser.
// Each seeded persona signs in with a hardened session cookie and attempts
// every route in the canonical product registry.
func TestTodo_UXAUDIT_014_Browser_Live(t *testing.T) {
	type personaCase struct {
		id, name, subject, landing string
		roles                      []string
	}
	personas := []personaCase{
		{id: "admin", name: "Rafael Torres", subject: "hc-050-rafael-torres", landing: PathProductHome, roles: []string{productui.RoleHCMAdmin, "comp_admin", "intent_author", "promotion_operator"}},
		{id: "hiring-manager", name: "Dominic Collins", subject: "hc-052-dominic-collins", landing: PathProductPrefix + "people", roles: []string{"hiring_manager", "manager", "intent_author", "promotion_operator"}},
		{id: "payroll-manager", name: "Thomas Baker", subject: "hc-054-thomas-baker", landing: PathProductPrefix + "work", roles: []string{"payroll_manager", "promotion_operator"}},
		{id: "individual-contributor", name: "Samuel Rivera", subject: "hc-022-samuel-rivera", landing: PathProductPrefix + "myself", roles: []string{"worker_self"}},
	}

	handler, _ := newShellHandler(t, true)
	grants := roleaccess.DefaultPagePermissions()
	for index := range grants {
		grant := &grants[index]
		if (grant.RoleID == "payroll_manager" || grant.RoleID == "promotion_operator") &&
			(grant.PageID == string(productui.PageOrganization) || grant.PageID == string(productui.PageOrgExplorer) ||
				grant.PageID == string(productui.PageOrgOutline) || grant.PageID == string(productui.PageOrgResponsive)) {
			grant.View = false
		}
		if grant.RoleID == "payroll_manager" && grant.PageID == string(productui.PageJourneys) {
			grant.Create = false
		}
	}
	handler.roleAccess = launcherRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: grants}}
	// The verifier clock is pinned for deterministic credentials, while the
	// real cookie jar correctly evaluates Expires against wall time.
	handler.now = func() time.Time { return time.Now().UTC() }
	handler.devPersonas = make(map[string]DevPersona, len(personas))
	for _, persona := range personas {
		handler.devPersonas[persona.id] = DevPersona{
			ID: persona.id, Name: persona.name, WorkerRef: persona.subject,
			Token: frontendE2EToken(t, persona.subject, persona.roles),
		}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	for _, persona := range personas {
		persona := persona
		t.Run(persona.id, func(t *testing.T) {
			permissions := roleaccess.EffectivePagePermissions(roleaccess.Snapshot{PagePermissions: grants}, persona.roles)
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client := server.Client()
			client.Jar = jar
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

			loginPage, err := client.Get(server.URL + PathLogin)
			if err != nil {
				t.Fatal(err)
			}
			loginBody, readErr := io.ReadAll(loginPage.Body)
			loginPage.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if loginPage.StatusCode != http.StatusOK {
				t.Fatalf("GET login = %d", loginPage.StatusCode)
			}
			if !strings.Contains(string(loginBody), persona.name) {
				t.Fatalf("login card did not show bound worker %q", persona.name)
			}

			form := url.Values{paramLoginPersona: {persona.id}}
			login, err := client.PostForm(server.URL+PathLogin, form)
			if err != nil {
				t.Fatal(err)
			}
			login.Body.Close()
			if login.StatusCode != http.StatusSeeOther || login.Header.Get("Location") != persona.landing {
				t.Fatalf("POST login = %d location %q", login.StatusCode, login.Header.Get("Location"))
			}
			landing, err := client.Get(server.URL + persona.landing)
			if err != nil {
				t.Fatal(err)
			}
			landingBytes, readErr := io.ReadAll(landing.Body)
			landing.Body.Close()
			if readErr != nil || landing.StatusCode != http.StatusOK {
				t.Fatalf("landing %s = %d, read %v", persona.landing, landing.StatusCode, readErr)
			}
			landingBody := string(landingBytes)
			if !strings.Contains(landingBody, `id="workspace-navigation"`) {
				t.Fatal("authenticated landing did not render the navigation component")
			}
			organizationNav := strings.Contains(landingBody, `href="/workspace/app/organization`)
			promote := len(island(t, landingBody).LauncherActions) > 0
			switch persona.id {
			case "hiring-manager":
				if !organizationNav || !promote {
					t.Fatal("hiring-manager landing lost its organization path or promotion action")
				}
			case "payroll-manager":
				if organizationNav || promote {
					t.Fatal("payroll-manager landing advertised organization browsing or promotion initiation")
				}
			case "individual-contributor":
				if promote || strings.Contains(landingBody, `href="/workspace/app/people`) {
					t.Fatal("self-service landing advertised people administration or promotion initiation")
				}
			}

			for _, definition := range productui.PageDefinitions() {
				definition := definition
				t.Run(string(definition.ID), func(t *testing.T) {
					response, err := client.Get(server.URL + definition.Route + "?locale=de-DE")
					if err != nil {
						t.Fatal(err)
					}
					bodyBytes, readErr := io.ReadAll(response.Body)
					response.Body.Close()
					if readErr != nil {
						t.Fatal(readErr)
					}
					body := string(bodyBytes)

					want := http.StatusForbidden
					if roleaccess.CanPageAction(permissions, string(definition.ID), roleaccess.ActionView) {
						want = http.StatusOK
					}
					if response.StatusCode != want {
						t.Fatalf("GET %s as %s = %d, want %d", definition.Route, persona.id, response.StatusCode, want)
					}
					if want != http.StatusOK {
						if strings.Contains(body, `id="`+JourneyRootElementID+`"`) || strings.Contains(body, `id="`+JourneyConfigElementID+`"`) {
							t.Error("forbidden response disclosed the authenticated product shell")
						}
						return
					}
					if got := response.Header.Get("Cache-Control"); got != "no-store" {
						t.Errorf("Cache-Control = %q", got)
					}
					if got := response.Header.Get("Content-Security-Policy"); !strings.Contains(got, "script-src '") ||
						!strings.Contains(got, PathAssetPrefix) ||
						!strings.Contains(got, PathTunnel) ||
						strings.Contains(got, "connect-src 'self'") ||
						strings.Contains(got, "script-src 'self'") {
						t.Errorf("product CSP is incomplete or grants origin-wide authority: %q", got)
					}
					for _, contract := range []string{`<html lang="de-DE" dir="ltr"`, `id="` + JourneyRootElementID + `"`, `id="main-content"`, `aria-busy="true"`} {
						if !strings.Contains(body, contract) {
							t.Errorf("served shell missing %q", contract)
						}
					}
					if strings.Contains(body, "⟦") {
						t.Error("served shell exposed an unresolved translation key")
					}
				})
			}
		})
	}
}

func frontendE2EToken(t *testing.T, subject string, roles []string) string {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience,
		Now: func() time.Time { return shellNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: shellIssuer, Audience: shellAudience, Subject: subject, SubjectKind: "human",
		Tenant: shellTenant, OrganizationScopeID: "org-north-america", Roles: roles,
		Purposes: []string{shellPurpose}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef:   "session-frontend-e2e-" + subject,
		IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}
