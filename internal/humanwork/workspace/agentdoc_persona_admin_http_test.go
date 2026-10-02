package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type agentdocPersonaAdminTransport struct {
	personaAdminCommandTransportFake
	references []agentdocref.Reference
}

func (f *agentdocPersonaAdminTransport) ExecutePersonaAdminCommandWithDocumentReferences(ctx context.Context, request productui.PersonaAdminCommandRequest, refs []agentdocref.Reference) error {
	f.references = append([]agentdocref.Reference(nil), refs...)
	return f.ExecutePersonaAdminCommand(ctx, request)
}

func TestTodo_AGENTDOC_002_PersonaAdminHTTP(t *testing.T) {
	h, token := personaAdminCommandAuthorizedHandler(t)
	transport := &agentdocPersonaAdminTransport{}
	h.personaAdminCommands = transport
	body := `{"action":"CREATE_VERSION","persona_id":"persona-a","starter_id":"hcmnext.persona_template.policy_helper","starter_version":1,"document_references":[{"document_id":"doc-123e4567-e89b-42d3-a456-426614174000","version_mode":"PINNED","pinned_version":4,"section_anchor":"leave-policy","label":"Leave policy"}]}`
	req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || transport.calls != 1 || len(transport.references) != 1 {
		t.Fatalf("create-version response=%d calls=%d refs=%#v body=%s", rec.Code, transport.calls, transport.references, rec.Body.String())
	}
	ref := transport.references[0]
	if ref.DocumentID != "doc-123e4567-e89b-42d3-a456-426614174000" || ref.VersionMode != agentdocref.ModePinned || ref.PinnedVersion != 4 || ref.SectionAnchor != "leave-policy" || ref.Label != "Leave policy" {
		t.Fatalf("decoded reference = %#v", ref)
	}
}

func TestTodo_AGENTDOC_002_SecurityPersonaAdminHTTP(t *testing.T) {
	h, token := personaAdminCommandAuthorizedHandler(t)
	transport := &agentdocPersonaAdminTransport{}
	transport.err = personaAdminCommandFailure{code: "document_unreadable"}
	h.personaAdminCommands = transport
	req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(`{"action":"CREATE_VERSION","persona_id":"persona-a","starter_id":"hcmnext.persona_template.policy_helper","starter_version":1,"document_references":[]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var payload personaAdminCommandResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden || payload.Error == nil || payload.Error.Code != "document_unreadable" {
		t.Fatalf("unreadable response=%d payload=%+v", rec.Code, payload)
	}

	plain := &personaAdminCommandTransportFake{}
	h.personaAdminCommands = plain
	req = httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(`{"action":"CREATE_VERSION","persona_id":"persona-a","starter_id":"hcmnext.persona_template.policy_helper","starter_version":1,"document_references":[]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || plain.calls != 0 {
		t.Fatalf("unsupported reference transport response=%d calls=%d", rec.Code, plain.calls)
	}
}

func TestTodo_AGENTDOC_002_GoldenPersonaAdminHTTP(t *testing.T) {
	h, token := newShellHandler(t, false)
	client := &personaAdminHTTPClient{snapshot: productui.PersonaAdminSnapshot{Available: true, Personas: []productui.PersonaAdminPersona{{ID: "persona-a", DocumentReferences: []productui.PersonaAdminDocumentReference{{DocumentID: "doc-123e4567-e89b-42d3-a456-426614174000", VersionMode: "PINNED", PinnedVersion: 4, Label: "Restricted policy", Readable: false}}}}}}
	h.personaAdmin = personaAdminHTTPFactory{client: client}
	req := httptest.NewRequest(http.MethodGet, PathPersonaAdminData, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.servePersonaAdminData(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"document_references":[{"document_id":"doc-123e4567-e89b-42d3-a456-426614174000"`) {
		t.Fatalf("catalog response=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"title"`) {
		t.Fatalf("unreadable catalog reference leaked title: %s", rec.Body.String())
	}
}

func TestTodo_AGENTDOC_008_Browser(t *testing.T) {
	for _, tc := range []struct {
		name, action, instructions string
	}{
		{name: "draft guidance", action: "CREATE_DRAFT", instructions: `Follow {{doc:doc-policy}}.`},
		{name: "version guidance", action: "CREATE_VERSION", instructions: `Follow {{doc:doc-policy}}.`},
		{name: "explicit clear", action: "CREATE_VERSION", instructions: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, token := personaAdminCommandAuthorizedHandler(t)
			transport := &agentdocPersonaAdminTransport{}
			h.personaAdminCommands = transport
			body := `{"action":"` + tc.action + `","persona_id":"persona-a","starter_id":"hcmnext.persona_template.policy_helper","starter_version":1,"instructions":` + string(mustJSON(t, tc.instructions)) + `,"document_references":[{"document_id":"doc-policy","version_mode":"PINNED","pinned_version":4,"label":"Travel policy"}]}`
			req := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || transport.calls != 1 || transport.request.Instructions == nil || *transport.request.Instructions != tc.instructions || len(transport.references) != 1 {
				t.Fatalf("guidance HTTP status=%d request=%+v refs=%+v body=%s", rec.Code, transport.request, transport.references, rec.Body.String())
			}
		})
	}
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
