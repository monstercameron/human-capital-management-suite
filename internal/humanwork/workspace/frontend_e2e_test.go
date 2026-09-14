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

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestFrontendE2EPersonaLoginAndEveryProductRoute drives the production HTTP
// handler through a real loopback server. Each supported development persona
// signs in through the same form as the browser, keeps its hardened session
// cookie, and then attempts every route in the canonical product registry.
func TestFrontendE2EPersonaLoginAndEveryProductRoute(t *testing.T) {
	type personaCase struct {
		id, name, access, description string
		roles                         []string
	}
	personas := []personaCase{
		{id: "admin", name: "Rafael Torres", access: "HCM administrator", description: "All product and administration areas.", roles: []string{productui.RoleHCMAdmin}},
		{id: "hiring-manager", name: "Maya Chen", access: "Hiring manager", description: "Hiring and workforce areas.", roles: []string{"hiring_manager"}},
		{id: "payroll-manager", name: "Priya Nair", access: "Payroll manager", description: "Payroll and workforce areas.", roles: []string{"payroll_manager"}},
		{id: "individual-contributor", name: "Omar Reyes", access: "Individual contributor", description: "Employee self service.", roles: []string{"worker_self"}},
		// PROMOUX-015: productui.PageVisible mirrors roleaccess's
		// finance_partner grant, so a handler with no roleaccess store admits
		// the same routes the live server does.
		{id: "finance-partner", name: "Thomas Baker", access: "Finance partner", description: "Finance approvals.", roles: []string{"finance_partner"}},
	}

	handler, _ := newShellHandler(t, true)
	// The verifier clock is pinned for deterministic credentials, while the
	// real cookie jar correctly evaluates Expires against wall time.
	handler.now = func() time.Time { return time.Now().UTC() }
	handler.devPersonas = make(map[string]DevPersona, len(personas))
	for _, persona := range personas {
		handler.devPersonas[persona.id] = DevPersona{
			ID: persona.id, Name: persona.name, Access: persona.access, Description: persona.description,
			Token: frontendE2EToken(t, persona.id, persona.roles),
		}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	for _, persona := range personas {
		persona := persona
		t.Run(persona.id, func(t *testing.T) {
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
			loginPage.Body.Close()
			if loginPage.StatusCode != http.StatusOK {
				t.Fatalf("GET login = %d", loginPage.StatusCode)
			}

			form := url.Values{paramLoginPersona: {persona.id}}
			login, err := client.PostForm(server.URL+PathLogin, form)
			if err != nil {
				t.Fatal(err)
			}
			login.Body.Close()
			if login.StatusCode != http.StatusSeeOther || login.Header.Get("Location") != PathProductHome {
				t.Fatalf("POST login = %d location %q", login.StatusCode, login.Header.Get("Location"))
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
					if productui.PageVisible(definition.ID, persona.roles) {
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
