package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

type personaAdminHTTPClient struct {
	request productui.PersonaAdminSnapshotRequest
	preview productui.PersonaAdminPreviewRequest
	err     error
}

func (c *personaAdminHTTPClient) Snapshot(_ context.Context, request productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	c.request = request
	if c.err != nil {
		return productui.PersonaAdminSnapshot{}, c.err
	}
	return productui.PersonaAdminSnapshot{Available: true, Personas: []productui.PersonaAdminPersona{{ID: "server-persona", Name: "Server Persona"}}}, nil
}
func (c *personaAdminHTTPClient) Preview(_ context.Context, request productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	c.preview = request
	return productui.PersonaAdminPreview{Subject: request.SubjectID, Conversation: request.ConversationID}, nil
}

func TestPersonaAdminDataRouteReturnsServerSelectedPreview(t *testing.T) {
	h, token := newShellHandler(t, false)
	client := &personaAdminHTTPClient{}
	h.personaAdmin = personaAdminHTTPFactory{client: client}
	req := httptest.NewRequest(http.MethodGet, PathPersonaAdminData+"?persona_id=server-persona&subject_id=worker-7&conversation_id=chat-9", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.servePersonaAdminData(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("persona preview status = %d: %s", rec.Code, rec.Body.String())
	}
	var payload personaAdminDataResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Snapshot.Preview.Subject != "worker-7" || payload.Snapshot.Preview.Conversation != "chat-9" {
		t.Fatalf("preview = %+v", payload.Snapshot.Preview)
	}
	if client.preview != (productui.PersonaAdminPreviewRequest{PersonaID: "server-persona", SubjectID: "worker-7", ConversationID: "chat-9"}) {
		t.Fatalf("preview request = %+v", client.preview)
	}
}
func (*personaAdminHTTPClient) RequestReview(string) error   { return nil }
func (*personaAdminHTTPClient) PublishPersona(string) error  { return nil }
func (*personaAdminHTTPClient) RollbackPersona(string) error { return nil }
func (*personaAdminHTTPClient) SuspendPersona(string) error  { return nil }
func (*personaAdminHTTPClient) RetirePersona(string) error   { return nil }

type personaAdminHTTPFactory struct{ client productui.PersonaAdminClient }

func (f personaAdminHTTPFactory) ClientForRequest(context.Context) productui.PersonaAdminClient {
	return f.client
}

func TestPersonaAdminDataRouteUsesAdmittedPrincipalAndNeverQueryIdentity(t *testing.T) {
	h, token := newShellHandler(t, false)
	client := &personaAdminHTTPClient{}
	h.personaAdmin = personaAdminHTTPFactory{client: client}
	req := httptest.NewRequest(http.MethodGet, PathPersonaAdminData+"?tenant_id=attacker&principal=attacker", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.servePersonaAdminData(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("persona data status = %d: %s", rec.Code, rec.Body.String())
	}
	var payload personaAdminDataResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Snapshot.Available || len(payload.Snapshot.Personas) != 1 || payload.Snapshot.Personas[0].ID != "server-persona" {
		t.Fatalf("payload = %+v", payload)
	}
	if client.request.TenantID != shellTenant || client.request.Principal != shellSubject {
		t.Fatalf("request identity = %+v, want admitted principal", client.request)
	}
}

func TestPersonaAdminDataRouteDeniesWhenNoServerBindingExists(t *testing.T) {
	h, token := newShellHandler(t, false)
	req := httptest.NewRequest(http.MethodGet, PathPersonaAdminData, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.servePersonaAdminData(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unbound persona data status = %d, want forbidden", rec.Code)
	}
}

func TestPersonaAdminDataRouteLogsBoundedSnapshotFailureStage(t *testing.T) {
	h, token := newShellHandler(t, false)
	var records []transport.LogRecord
	h.config.Logger = transport.LoggerFunc(func(record transport.LogRecord) { records = append(records, record) })
	h.personaAdmin = personaAdminHTTPFactory{client: &personaAdminHTTPClient{err: personaAdminStageTestError{stage: "member_facts_read"}}}
	req := httptest.NewRequest(http.MethodGet, PathPersonaAdminData, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.servePersonaAdminData(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("persona data status = %d, want unavailable", rec.Code)
	}
	if len(records) != 1 || records[0].ErrorType != "member_facts_read" || records[0].Method != "GET /workspace/app/persona-admin" || records[0].ReasonRef != "PERSONA_ADMIN_SNAPSHOT_UNAVAILABLE" {
		t.Fatalf("snapshot failure diagnostics = %+v", records)
	}
}
