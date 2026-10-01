package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaAdminCommandTransportFake struct {
	request   productui.PersonaAdminCommandRequest
	principal *trust.Principal
	err       error
	calls     int
}

func (f *personaAdminCommandTransportFake) ExecutePersonaAdminCommand(ctx context.Context, request productui.PersonaAdminCommandRequest) error {
	f.calls++
	f.request = request
	f.principal, _ = trust.FromContext(ctx)
	return f.err
}

type personaAdminCommandFailure struct{ code string }

func TestTodo_AGENTP_018_ReviewCommandUsesViewPermission(t *testing.T) {
	h, token := personaAdminCommandAuthorizedHandler(t)
	h.roleAccess = personaRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{{RoleID: productui.RoleHCMAdmin, PageID: string(productui.PagePersonaAdmin), View: true}}}}
	commands := &personaAdminCommandTransportFake{}
	h.personaAdminCommands = commands
	request := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(`{"action":"REVIEW","persona_id":"reviewed-draft","decision":"APPROVE"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || commands.calls != 1 || commands.request.Action != "REVIEW" || commands.request.Decision != "APPROVE" {
		t.Fatalf("review transport refused the separate view-only reviewer: status=%d calls=%d", response.Code, commands.calls)
	}
}

func (e personaAdminCommandFailure) Error() string                   { return "private command failure detail" }
func (e personaAdminCommandFailure) PersonaAdminCommandCode() string { return e.code }

func personaAdminCommandAdminToken(t *testing.T) string {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience, Now: func() time.Time { return shellNow }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: shellIssuer, Audience: shellAudience, Subject: shellSubject, SubjectKind: "human", Tenant: shellTenant,
		OrganizationScopeID: "org-north-america", Roles: []string{productui.RoleHCMAdmin}, Purposes: []string{shellPurpose},
		AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-persona-admin",
		IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func personaAdminCommandAuthorizedHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	h, _ := newShellHandler(t, false)
	h.roleAccess = personaRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{{
		RoleID: productui.RoleHCMAdmin, PageID: string(productui.PagePersonaAdmin), View: true, Create: true, Update: true, Delete: true,
	}}}}
	return h, personaAdminCommandAdminToken(t)
}

func TestPersonaAdminCommandRouteUsesAdmittedPrincipalAndPassesSafeDraftFields(t *testing.T) {
	h, token := personaAdminCommandAuthorizedHandler(t)
	transport := &personaAdminCommandTransportFake{}
	h.personaAdminCommands = transport
	body, err := json.Marshal(productui.PersonaAdminCommandRequest{
		Action: "CREATE_DRAFT", PersonaID: "tenant-persona", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1,
		AvatarRef: "avatar:default", ManifestID: "agent.policy-helper", BusinessOwnerID: "owner-7", TechnicalStewardID: "steward-8",
		OrganizationScopes: []string{"org-east"},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || transport.calls != 1 {
		t.Fatalf("command response=%d calls=%d body=%s", rec.Code, transport.calls, rec.Body.String())
	}
	if transport.principal == nil || transport.principal.Subject() != shellSubject || string(transport.principal.Tenant()) != shellTenant {
		t.Fatalf("transport principal = %+v; want admitted workspace principal", transport.principal)
	}
	if transport.request.PersonaID != "tenant-persona" || transport.request.BusinessOwnerID != "owner-7" || len(transport.request.OrganizationScopes) != 1 || transport.request.OrganizationScopes[0] != "org-east" {
		t.Fatalf("transport command = %+v", transport.request)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestPersonaAdminCommandRouteRejectsInvalidRequestBeforeTransport(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "unknown fields", contentType: "application/json", body: `{"action":"SUSPEND","persona_id":"persona-a","tenant_id":"attacker"}`},
		{name: "unknown action", contentType: "application/json", body: `{"action":"DELETE_ALL","persona_id":"persona-a"}`},
		{name: "wrong media type", contentType: "text/plain", body: `{"action":"SUSPEND","persona_id":"persona-a"}`},
		{name: "extra json value", contentType: "application/json", body: `{"action":"SUSPEND","persona_id":"persona-a"} {}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, token := personaAdminCommandAuthorizedHandler(t)
			transport := &personaAdminCommandTransportFake{}
			h.personaAdminCommands = transport
			req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code < 400 || transport.calls != 0 {
				t.Fatalf("invalid command status=%d calls=%d body=%s", rec.Code, transport.calls, rec.Body.String())
			}
		})
	}
}

func TestPersonaAdminCommandRouteReturnsTypedErrorsWithoutBackendDetails(t *testing.T) {
	tests := []struct {
		code string
		want int
	}{
		{code: "forbidden", want: http.StatusForbidden},
		{code: "invalid", want: http.StatusUnprocessableEntity},
		{code: "conflict", want: http.StatusConflict},
		{code: "unavailable", want: http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			h, token := personaAdminCommandAuthorizedHandler(t)
			transport := &personaAdminCommandTransportFake{err: personaAdminCommandFailure{code: tc.code}}
			h.personaAdminCommands = transport
			req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(`{"action":"SUSPEND","persona_id":"persona-a"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			var payload personaAdminCommandResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.want || payload.Error == nil || payload.Error.Code != tc.code {
				t.Fatalf("error response status=%d payload=%+v", rec.Code, payload)
			}
			if strings.Contains(rec.Body.String(), "private command failure detail") {
				t.Fatalf("backend detail leaked: %s", rec.Body.String())
			}
		})
	}
}

func TestPersonaAdminCommandRouteFailsClosedWithoutTransport(t *testing.T) {
	h, token := personaAdminCommandAuthorizedHandler(t)
	req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(`{"action":"SUSPEND","persona_id":"persona-a"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing transport response = %d %s", rec.Code, rec.Body.String())
	}
}
