package workspace

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

var errPersonaAdminStoreDown = errors.New("persona store is not connected")

// TestTodo_AGENTP_018_LifecycleCommandsReachTransport proves the RED fix for
// "lifecycle buttons that cannot execute": every lifecycle action the Personas
// page offers travels the authenticated command transport with the admitted
// principal instead of ending at a dead control.
func TestTodo_AGENTP_018_LifecycleCommandsReachTransport(t *testing.T) {
	bump := "minor"
	tests := []struct {
		name string
		body string
		want productui.PersonaAdminCommandRequest
	}{
		{name: "publish", body: `{"action":"PUBLISH","persona_id":"policy-helper","version_bump":"minor"}`, want: productui.PersonaAdminCommandRequest{Action: "PUBLISH", PersonaID: "policy-helper", VersionBump: &bump}},
		{name: "rollback", body: `{"action":"ROLLBACK","persona_id":"policy-helper"}`, want: productui.PersonaAdminCommandRequest{Action: "ROLLBACK", PersonaID: "policy-helper"}},
		{name: "suspend", body: `{"action":"SUSPEND","persona_id":"policy-helper"}`, want: productui.PersonaAdminCommandRequest{Action: "SUSPEND", PersonaID: "policy-helper"}},
		{name: "retire", body: `{"action":"RETIRE","persona_id":"policy-helper"}`, want: productui.PersonaAdminCommandRequest{Action: "RETIRE", PersonaID: "policy-helper"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, token := personaAdminCommandAuthorizedHandler(t)
			transport := &personaAdminCommandTransportFake{}
			h.personaAdminCommands = transport
			req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || transport.calls != 1 {
				t.Fatalf("%s response=%d calls=%d body=%s", tc.name, rec.Code, transport.calls, rec.Body.String())
			}
			got, want := transport.request, tc.want
			if got.Action != want.Action || got.PersonaID != want.PersonaID {
				t.Fatalf("%s reached transport as %+v", tc.name, got)
			}
			if (got.VersionBump == nil) != (want.VersionBump == nil) || (want.VersionBump != nil && *got.VersionBump != *want.VersionBump) {
				t.Fatalf("%s version choice lost: %+v", tc.name, got)
			}
			if transport.principal == nil || transport.principal.Subject() != shellSubject || string(transport.principal.Tenant()) != shellTenant {
				t.Fatalf("%s transport principal = %+v; want admitted workspace principal", tc.name, transport.principal)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s cache control = %q", tc.name, rec.Header().Get("Cache-Control"))
			}
		})
	}
}

// TestTodo_AGENTP_018_Security_DataAndCommandRoutesDenyWithoutPermission proves
// the catalog transports never serve or mutate for a principal without persona
// administration permission: no snapshot read, no command execution, and no
// catalog bytes in either refusal.
func TestTodo_AGENTP_018_Security_DataAndCommandRoutesDenyWithoutPermission(t *testing.T) {
	denied := personaRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{{
		RoleID: productui.RoleHCMAdmin, PageID: string(productui.PagePersonaAdmin),
	}}}}
	t.Run("data", func(t *testing.T) {
		h, token := newShellHandler(t, false)
		h.roleAccess = denied
		client := &personaAdminHTTPClient{snapshot: productui.PersonaAdminSnapshot{Available: true, Personas: []productui.PersonaAdminPersona{{ID: "policy-helper", Name: "Policy Helper"}}}}
		h.personaAdmin = personaAdminHTTPFactory{client: client}
		req := httptest.NewRequest(http.MethodGet, PathPersonaAdminData, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.servePersonaAdminData(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("unpermitted catalog read status = %d, want forbidden", rec.Code)
		}
		if client.request != (productui.PersonaAdminSnapshotRequest{}) {
			t.Fatalf("denied catalog read reached the store: %+v", client.request)
		}
		if strings.Contains(rec.Body.String(), "policy-helper") {
			t.Fatalf("denied catalog read leaked catalog bytes: %s", rec.Body.String())
		}
	})
	t.Run("command", func(t *testing.T) {
		h, token := personaAdminCommandAuthorizedHandler(t)
		h.roleAccess = denied
		transport := &personaAdminCommandTransportFake{}
		h.personaAdminCommands = transport
		req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(`{"action":"SUSPEND","persona_id":"policy-helper"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || transport.calls != 0 {
			t.Fatalf("unpermitted lifecycle command status=%d calls=%d body=%s", rec.Code, transport.calls, rec.Body.String())
		}
	})
}

// TestTodo_AGENTP_018_DirectLoadEmbedsAuthorizedCatalog proves a direct load of
// the Personas page hydrates the authorized catalog into the served document,
// while a store failure serves the shell with no fabricated projection so the
// browser can say honestly that the service is not connected.
func TestTodo_AGENTP_018_DirectLoadEmbedsAuthorizedCatalog(t *testing.T) {
	route := productui.Path(productui.PagePersonaAdmin)
	t.Run("connected", func(t *testing.T) {
		h, token := newShellHandler(t, false)
		h.personaAdmin = personaAdminHTTPFactory{client: &personaAdminHTTPClient{snapshot: productui.PersonaAdminSnapshot{
			Available: true, Personas: []productui.PersonaAdminPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		}}}
		req := httptest.NewRequest(http.MethodGet, "http://cell.test"+route, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", route, rec.Code, rec.Body.String())
		}
		config := island(t, rec.Body.String())
		if config.PersonaAdminSnapshot == nil || !config.PersonaAdminSnapshot.Available || len(config.PersonaAdminSnapshot.Personas) != 1 || config.PersonaAdminSnapshot.Personas[0].ID != "policy-helper" {
			t.Fatalf("direct load did not hydrate the authorized catalog: %+v", config.PersonaAdminSnapshot)
		}
	})
	t.Run("store failure stays honest", func(t *testing.T) {
		h, token := newShellHandler(t, false)
		h.personaAdmin = personaAdminHTTPFactory{client: &personaAdminHTTPClient{err: errPersonaAdminStoreDown}}
		req := httptest.NewRequest(http.MethodGet, "http://cell.test"+route, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", route, rec.Code, rec.Body.String())
		}
		config := island(t, rec.Body.String())
		if config.PersonaAdminSnapshot != nil {
			t.Fatalf("store failure fabricated a catalog projection: %+v", config.PersonaAdminSnapshot)
		}
	})
}
