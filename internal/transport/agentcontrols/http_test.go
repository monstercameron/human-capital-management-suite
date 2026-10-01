package agentcontrols

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type controlSurfaceTest struct {
	calls   int
	command productui.AgentControlsCommand
	draft   productui.AgentScheduleDraft
	err     error
}

func (s *controlSurfaceTest) Snapshot(context.Context) (Reply, error) {
	s.calls++
	return Reply{Snapshot: productui.AgentControlsSnapshot{Available: true}}, s.err
}
func (s *controlSurfaceTest) Control(_ context.Context, c productui.AgentControlsCommand) (Reply, error) {
	s.command = c
	return s.Snapshot(context.Background())
}
func (s *controlSurfaceTest) Draft(_ context.Context, d productui.AgentScheduleDraft) (Reply, error) {
	s.draft = d
	return s.Snapshot(context.Background())
}
func (s *controlSurfaceTest) Preview(ctx context.Context, d productui.AgentScheduleDraft) (Reply, error) {
	return s.Draft(ctx, d)
}

func TestTodo_AGENT_030_ControlsHTTP(t *testing.T) {
	surface := &controlSurfaceTest{}
	for _, path := range []string{Path, Path + "/control", Path + "/draft", Path + "/preview"} {
		method, body := http.MethodGet, ""
		if path != Path {
			method, body = http.MethodPost, `{"id":"weekly","expected_revision":7}`
		}
		response := httptest.NewRecorder()
		Handler{Surface: surface}.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %d", path, response.Code)
		}
		var reply Reply
		if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil || !reply.Snapshot.Available {
			t.Fatal("lost owner projection")
		}
	}
	if surface.command.ExpectedRevision != 7 || surface.draft.ExpectedRevision != 7 || surface.calls != 4 {
		t.Fatal("owner request altered")
	}
}

func TestTodo_AGENT_041_ControlsHTTPSecurity(t *testing.T) {
	surface := &controlSurfaceTest{}
	for _, body := range []string{`{"tenant_id":"forged"}`, `{"actor":"forged"}`, `{} {}`, `{"reason":"` + strings.Repeat("x", 17000) + `"}`, `{`} {
		response := httptest.NewRecorder()
		Handler{Surface: surface}.ServeHTTP(response, httptest.NewRequest(http.MethodPost, Path+"/control", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest || surface.calls != 0 {
			t.Fatal("malformed or caller-authorized payload reached owner")
		}
	}
	for _, entry := range []struct {
		err    error
		status int
	}{{ErrUnauthenticated, 401}, {ErrDenied, 403}, {ErrConflict, 409}, {ErrInvalid, 400}, {ErrUnavailable, 503}} {
		surface.err = entry.err
		response := httptest.NewRecorder()
		Handler{Surface: surface}.ServeHTTP(response, httptest.NewRequest(http.MethodGet, Path, nil))
		if response.Code != entry.status {
			t.Fatalf("wrong error projection %v: %d", entry.err, response.Code)
		}
	}
	response := httptest.NewRecorder()
	Handler{}.ServeHTTP(response, httptest.NewRequest(http.MethodGet, Path, nil))
	if response.Code != 503 {
		t.Fatal("nil surface did not fail closed")
	}
	response = httptest.NewRecorder()
	Handler{Surface: surface}.ServeHTTP(response, httptest.NewRequest(http.MethodGet, Path+"/unknown", nil))
	if response.Code != 404 {
		t.Fatal("unknown endpoint accepted")
	}
}
