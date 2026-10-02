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

type proactiveSurface struct {
	calls   int
	draft   AnnouncementDraft
	command AnnouncementCommand
	err     error
}

func (s *proactiveSurface) ListAnnouncements(context.Context) (AnnouncementReply, error) {
	s.calls++
	return AnnouncementReply{Snapshot: productui.AgentAnnouncementsSnapshot{Available: true}}, s.err
}
func (s *proactiveSurface) PreviewAnnouncement(ctx context.Context, d AnnouncementDraft) (AnnouncementReply, error) {
	s.draft = d
	return s.ListAnnouncements(ctx)
}
func (s *proactiveSurface) CreateAnnouncement(ctx context.Context, d AnnouncementDraft) (AnnouncementReply, error) {
	s.draft = d
	return s.ListAnnouncements(ctx)
}
func (s *proactiveSurface) UpdateAnnouncement(ctx context.Context, d AnnouncementDraft) (AnnouncementReply, error) {
	s.draft = d
	return s.ListAnnouncements(ctx)
}
func (s *proactiveSurface) ControlAnnouncement(ctx context.Context, c AnnouncementCommand) (AnnouncementReply, error) {
	s.command = c
	return s.ListAnnouncements(ctx)
}

func TestAgentUXProactive_HTTP(t *testing.T) {
	s := &proactiveSurface{}
	for _, path := range []string{AnnouncementPath, AnnouncementPath + "/preview", AnnouncementPath + "/update", AnnouncementPath + "/control"} {
		method, body := http.MethodPost, `{"id":"holidays","expected_revision":7,"idempotency_key":"request-123"}`
		w := httptest.NewRecorder()
		AnnouncementHandler{Surface: s}.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		var reply AnnouncementReply
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &reply) != nil || !reply.Snapshot.Available {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	AnnouncementHandler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, AnnouncementPath, nil))
	if w.Code != 200 || s.calls != 5 || s.draft.ExpectedRevision != 7 || s.command.IdempotencyKey != "request-123" {
		t.Fatalf("owner commands changed: %+v", s)
	}
}

func TestAgentUXProactive_HTTP_Security(t *testing.T) {
	s := &proactiveSurface{}
	for _, body := range []string{`{"tenant_id":"forged"}`, `{"owner_id":"forged"}`, `{} {}`, `{`, `{"instruction":"` + strings.Repeat("x", 25000) + `"}`} {
		w := httptest.NewRecorder()
		AnnouncementHandler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, AnnouncementPath, strings.NewReader(body)))
		if w.Code != 400 || s.calls != 0 {
			t.Fatalf("untrusted payload reached a port: %d %d", w.Code, s.calls)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{ErrUnauthenticated, 401}, {ErrDenied, 403}, {ErrInvalid, 400}, {ErrConflict, 409}, {ErrUnavailable, 503}} {
		s.err = tc.err
		w := httptest.NewRecorder()
		AnnouncementHandler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, AnnouncementPath, nil))
		if w.Code != tc.status {
			t.Fatalf("%v mapped to %d", tc.err, w.Code)
		}
	}
	w := httptest.NewRecorder()
	AnnouncementHandler{}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, AnnouncementPath, nil))
	if w.Code != 503 {
		t.Fatal("uncomposed port accepted")
	}
	w = httptest.NewRecorder()
	AnnouncementHandler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, AnnouncementPath, nil))
	if w.Code != 404 {
		t.Fatal("unsupported verb accepted")
	}
}
