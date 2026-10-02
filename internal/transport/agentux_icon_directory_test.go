package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

type agentIconDirectoryFixture struct {
	conversation string
	err          error
}

func (s *agentIconDirectoryFixture) DirectoryAgentIcons(_ context.Context, conversation string) (AgentIconDirectory, error) {
	s.conversation = conversation
	icon := agenticon.Generate(agenticon.Input{Name: "birthday"})
	return AgentIconDirectory{Personas: []AgentIconMentionProfile{{Profile: personachat.Profile{Reference: personachat.Reference{ID: "agent:a", Display: "Birthday"}}, Icon: icon, IconRevision: 1}}, PostActors: []AgentIconPostActor{{PostActor: personachat.PostActor{PostID: "post", PersonaID: "birthday"}, Icon: icon, IconRevision: 1}}}, s.err
}

func TestAgentUXIcon_Generated_Directory(t *testing.T) {
	source := &agentIconDirectoryFixture{}
	fallbackCalls := 0
	h := AgentIconDirectoryHandler{Source: source, Fallback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fallbackCalls++; w.WriteHeader(204) })}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", personachat.Path+"?conversation_id=room", nil))
	if w.Code != 200 || source.conversation != "room" || fallbackCalls != 0 || strings.Count(w.Body.String(), `"glyph":"cake"`) != 2 || !strings.Contains(w.Body.String(), `"reference"`) || !strings.Contains(w.Body.String(), `"post_id":"post"`) {
		t.Fatal("directory envelope", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", personachat.Path+"/invocations", nil))
	if w.Code != 204 || fallbackCalls != 1 {
		t.Fatal("progress not delegated")
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{personachat.ErrUnauthenticated, 401}, {personachat.ErrDenied, 403}, {personachat.ErrInvalid, 400}, {personachat.ErrUnavailable, 503}} {
		source.err = tc.err
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", personachat.Path, nil))
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
	w = httptest.NewRecorder()
	AgentIconDirectoryHandler{}.ServeHTTP(w, httptest.NewRequest("GET", personachat.Path, nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	AgentIconDirectoryHandler{}.ServeHTTP(w, httptest.NewRequest("POST", "/missing", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
