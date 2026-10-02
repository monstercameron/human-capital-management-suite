package agentdemo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type agentuxDemoSurfaceFixture struct {
	calls int
	email Email
	err   error
}

func (s *agentuxDemoSurfaceFixture) SimulateCustomerEmail(_ context.Context, e Email) (Receipt, error) {
	s.calls++
	s.email = e
	return Receipt{MessageID: "email-1", State: "QUEUED"}, s.err
}

func TestAgentUXDemo_InboxHTTP(t *testing.T) {
	s := &agentuxDemoSurfaceFixture{}
	w := httptest.NewRecorder()
	Handler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"from":"customer@example.test","subject":"Late delivery","body":"Please help","idempotency_key":"email-123"}`)))
	if w.Code != http.StatusAccepted || s.calls != 1 || s.email.Subject != "Late delivery" || !strings.Contains(w.Body.String(), `"message_id":"email-1"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response %d %s; calls=%d", w.Code, w.Body, s.calls)
	}
}

func TestAgentUXDemo_InboxHTTP_Security(t *testing.T) {
	for _, body := range []string{`{"tenant_id":"other"}`, `{"sender_verified":true}`, `{} {}`, `{"body":"` + strings.Repeat("x", 73*1024) + `"}`} {
		s := &agentuxDemoSurfaceFixture{}
		w := httptest.NewRecorder()
		Handler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body)))
		if w.Code != http.StatusBadRequest || s.calls != 0 {
			t.Fatalf("accepted untrusted envelope: status=%d calls=%d", w.Code, s.calls)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{ErrDenied, 403, "denied"}, {ErrConflict, 409, "conflict"}, {ErrInvalid, 400, "invalid"}, {errors.New("provider secret"), 503, "unavailable"}} {
		s := &agentuxDemoSurfaceFixture{err: tc.err}
		w := httptest.NewRecorder()
		Handler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{}`)))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "provider secret") {
			t.Fatalf("unsafe error: %d %s", w.Code, w.Body)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		w := httptest.NewRecorder()
		Handler{}.ServeHTTP(w, httptest.NewRequest(method, Path, nil))
		if w.Code != 405 {
			t.Fatalf("method %s: %d", method, w.Code)
		}
	}
}
