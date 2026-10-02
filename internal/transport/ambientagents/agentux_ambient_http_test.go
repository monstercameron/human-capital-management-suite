package ambientagents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type agentUXAmbientSurface struct {
	calls int
	deny  bool
}

func (s *agentUXAmbientSurface) Snapshot(context.Context, string) (Snapshot, error) {
	s.calls++
	if s.deny {
		return Snapshot{}, errors.New("denied")
	}
	return Snapshot{}, nil
}
func (s *agentUXAmbientSurface) Control(ctx context.Context, c Command) (Snapshot, error) {
	return s.Snapshot(ctx, c.Conversation)
}
func (s *agentUXAmbientSurface) Grant(ctx context.Context, c GrantCommand) (Snapshot, error) {
	return s.Snapshot(ctx, c.Conversation)
}
func (s *agentUXAmbientSurface) OptOut(ctx context.Context, c OptOutCommand) (Snapshot, error) {
	return s.Snapshot(ctx, c.Conversation)
}
func TestAgentUXAmbient_HTTP_Security(t *testing.T) {
	s := &agentUXAmbientSurface{}
	h := Handler{Surface: s}
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{{"GET", Path + "?conversation=general", "", 200}, {"POST", Path + "/control", `{"conversation":"general","id":"card","action":"ADD","expected_revision":1}`, 200}, {"POST", Path + "/grant", `{"conversation":"general","agent":"task-catcher","enabled":true}`, 200}, {"POST", Path + "/opt-out", `{"conversation":"general","opt_out":true}`, 200}, {"POST", Path + "/control", `{"tenant":"other","actor":"author"}`, 400}, {"POST", Path + "/control", `{} {}`, 400}, {"POST", Path + "/control", `{"action":"ADD"` + strings.Repeat(" ", 9000) + `}`, 400}, {"PUT", Path, "", 405}, {"POST", Path + "/unknown", `{}`, 404}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%+v => %d %s", tc, w.Code, w.Body.String())
		}
	}
	if s.calls != 4 {
		t.Fatalf("invalid body reached port: %d", s.calls)
	}
	s.deny = true
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, Path, nil))
	if w.Code != 403 {
		t.Fatal("denied read returned snapshot")
	}
}
