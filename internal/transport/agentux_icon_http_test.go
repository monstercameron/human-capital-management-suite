package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

type agentIconSurfaceFixture struct {
	command AgentIconCommand
	preview bool
	calls   int
	err     error
}

func (s *agentIconSurfaceFixture) ChangeAgentIcon(_ context.Context, c AgentIconCommand) (AgentIconReply, error) {
	s.command = c
	s.calls++
	return AgentIconReply{Icon: agenticon.Generate(agenticon.Input{Name: "Policy Helper"}), Revision: 2}, s.err
}
func (s *agentIconSurfaceFixture) PreviewAgentIcon(ctx context.Context, c AgentIconCommand) (AgentIconReply, error) {
	s.preview = true
	reply, err := s.ChangeAgentIcon(ctx, c)
	reply.Revision = 1
	return reply, err
}

func TestAgentUXIcon_Generated(t *testing.T) {
	for _, action := range []string{"regenerate", "shuffle", "reset", "undo", "preview"} {
		surface := &agentIconSurfaceFixture{}
		body := `{"persona_id":"policy","expected_revision":1}`
		if action == "preview" {
			body = `{"persona_id":"policy","expected_revision":1,"action":"regenerate"}`
		}
		w := httptest.NewRecorder()
		AgentIconHandler{Surface: surface}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, AgentIconPath+"/"+action, strings.NewReader(body)))
		if w.Code != 200 || surface.calls != 1 || surface.command.PersonaID != "policy" || surface.command.ExpectedRevision != 1 || surface.preview != (action == "preview") || !strings.Contains(w.Body.String(), `"glyph":"book"`) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(action, w.Code, w.Body.String(), surface)
		}
	}
}

func TestAgentUXIcon_Generated_Security(t *testing.T) {
	for _, body := range []string{`{"persona_id":"policy","tenant_id":"victim","expected_revision":1}`, `{"persona_id":"policy","administrator":true}`, `{"persona_id":"policy","icon":{"glyph":"<script>"}}`, `{"persona_id":"policy","action":"reset"}`, `{} {}`, `{`, strings.Repeat("x", 5000)} {
		surface := &agentIconSurfaceFixture{}
		w := httptest.NewRecorder()
		AgentIconHandler{Surface: surface}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, AgentIconPath+"/shuffle", strings.NewReader(body)))
		if w.Code != 400 || surface.calls != 0 {
			t.Fatal("unsafe body delegated", w.Code, body, surface.calls)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{ErrAgentIconUnauthenticated, 401, "unauthenticated"}, {ErrAgentIconDenied, 403, "denied"}, {ErrAgentIconInvalid, 400, "invalid"}, {ErrAgentIconConflict, 409, "conflict"}, {ErrAgentIconUnavailable, 503, "unavailable"}, {errors.New("private details"), 503, "unavailable"}} {
		w := httptest.NewRecorder()
		AgentIconHandler{Surface: &agentIconSurfaceFixture{err: tc.err}}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, AgentIconPath+"/shuffle", strings.NewReader(`{}`)))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "private details") {
			t.Fatal("error mapping", w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", AgentIconPath + "/shuffle", 405}, {"POST", AgentIconPath + "/upload", 404}, {"POST", "/shuffle", 404}} {
		w := httptest.NewRecorder()
		AgentIconHandler{}.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
	w := httptest.NewRecorder()
	AgentIconHandler{}.ServeHTTP(w, httptest.NewRequest("POST", AgentIconPath+"/reset", strings.NewReader(`{}`)))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
