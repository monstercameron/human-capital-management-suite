package application

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatremoveHTTPFixture struct {
	chat.ModerationStore
	calls      int
	permission error
	principal  chat.Principal
	report     chatrecords.Report
}

func (f *chatremoveHTTPFixture) ModerationNotices(context.Context, chat.Principal, string) ([]chat.ModerationNotice, error) {
	return []chat.ModerationNotice{{Outcome: "remove", Reason: "spam", CanAppeal: true}}, nil
}
func (f *chatremoveHTTPFixture) PreviewRemoval(_ context.Context, r chat.RemovalRequest) ([]chat.RemovalCandidate, error) {
	f.principal = r.Principal
	return []chat.RemovalCandidate{{ID: "post", Revision: 1}}, nil
}
func (f *chatremoveHTTPFixture) SearchModeration(_ context.Context, p chat.Principal, tenant, query string) ([]chat.ModerationItem, error) {
	f.principal = p
	return []chat.ModerationItem{{ID: "report:one", Kind: "report", ConversationID: "room", PostID: "post"}}, nil
}
func (f *chatremoveHTTPFixture) Report(_ context.Context, p chat.Principal, r chatrecords.Report) error {
	f.calls++
	f.principal = p
	f.report = r
	return nil
}
func (f *chatremoveHTTPFixture) CanModerate(context.Context, chat.Principal, string, string, string) error {
	return f.permission
}
func (f *chatremoveHTTPFixture) CanReportMessage(context.Context, chat.Principal, string, string, string) error {
	return f.permission
}
func (f *chatremoveHTTPFixture) AssignModerationPermission(context.Context, chat.Principal, string, string, string, string, bool) error {
	f.calls++
	return f.permission
}

func (f *chatremoveHTTPFixture) ResolveModeration(context.Context, chat.Principal, string, string, string, string, time.Time) error {
	f.calls++
	return nil
}

func chatremoveHTTPRequest(t *testing.T, handler ChatModerationHTTP, path, body string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	method := http.MethodPost
	if body == "" {
		method = http.MethodGet
	}
	r := httptest.NewRequest(method, ChatModerationPath+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authenticated {
		r = r.WithContext(trust.WithPrincipal(r.Context(), personaDraftPrincipal(t, "tenant-a", "reviewer", time.Now())))
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestTodo_CHATMOD_004(t *testing.T) {
	f := &chatremoveHTTPFixture{}
	h := ChatModerationHTTP{Service: &chat.ModerationService{Store: f}}
	w := chatremoveHTTPRequest(t, h, "/preview", `{"Removal":{"Selection":{"ConversationID":"room","PostIDs":["post"]},"ReasonCode":"spam","Action":"remove"}}`, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"Count":1`) || f.principal.SubjectID != "reviewer" {
		t.Fatalf("status=%d body=%s principal=%+v", w.Code, w.Body.String(), f.principal)
	}
}

func TestTodo_CHATMOD_004_Security(t *testing.T) {
	f := &chatremoveHTTPFixture{}
	h := ChatModerationHTTP{Service: &chat.ModerationService{Store: f}}
	for _, body := range []string{`{"Removal":{"Principal":{"SubjectID":"other"}}}`, `{} {}`, `{"Unknown":true}`} {
		w := chatremoveHTTPRequest(t, h, "/preview", body, true)
		if w.Code != 400 {
			t.Fatalf("accepted %s status=%d", body, w.Code)
		}
	}
	w := chatremoveHTTPRequest(t, h, "/preview", `{}`, false)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = chatremoveHTTPRequest(t, h, "/preview", `{"Note":"`+strings.Repeat("x", 33000)+`"}`, true)
	if w.Code != 400 {
		t.Fatal("oversized request", w.Code)
	}
}

func TestTodo_CHATMOD_005(t *testing.T) {
	f := &chatremoveHTTPFixture{}
	h := ChatModerationHTTP{Service: &chat.ModerationService{Store: f}, Reports: f, Permissions: f}
	w := chatremoveHTTPRequest(t, h, "", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"Count":1`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = chatremoveHTTPRequest(t, h, "/report", `{"ConversationID":"room","PostID":"post","ReasonCode":"spam","Note":"details"}`, true)
	if w.Code != 200 || f.calls != 1 || f.principal.SubjectID != "reviewer" || f.report.TargetID != "post" || !strings.Contains(f.report.Reason, "details") {
		t.Fatalf("status=%d calls=%d report=%+v", w.Code, f.calls, f.report)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response cacheable")
	}
	r := httptest.NewRequest(http.MethodGet, "/chat/moderation?locale=de-DE", nil)
	r = r.WithContext(trust.WithPrincipal(r.Context(), personaDraftPrincipal(t, "tenant-a", "reviewer", time.Now())))
	page := httptest.NewRecorder()
	ChatModerationPageHTTP{HTTP: h}.ServeHTTP(page, r)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Überprüfung anfordern") || page.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("page=%d %s", page.Code, page.Body.String())
	}
	extensions := &ChatModerationExtensions{Moderation: h.Service}
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "reviewer"}
	if err := extensions.Moderate(context.Background(), p, "room", "one", "dismiss", "post", "reviewed", "evidence"); err != nil || f.calls != 2 {
		t.Fatalf("existing moderation service calls=%d err=%v", f.calls, err)
	}
	if err := extensions.Moderate(context.Background(), p, "room", "one", "remove", "different-post", "reviewed", "evidence"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("forged target accepted", err)
	}
}

func TestTodo_CHATMOD_005_Security(t *testing.T) {
	f := &chatremoveHTTPFixture{permission: chat.ErrPermissionDenied}
	h := ChatModerationHTTP{Service: &chat.ModerationService{Store: f}, Reports: f, Permissions: f}
	w := chatremoveHTTPRequest(t, h, "/report", `{"ConversationID":"private","PostID":"post","ReasonCode":"spam"}`, true)
	if w.Code != 403 || f.calls != 0 || !strings.Contains(w.Body.String(), "permission_denied") {
		t.Fatalf("%d %s calls=%d", w.Code, w.Body.String(), f.calls)
	}
	for _, err := range []error{chat.ErrConflict, chat.ErrUnavailable, chat.ErrNotFound, errors.New("secret database detail")} {
		w := httptest.NewRecorder()
		chatremoveHTTPError(w, err)
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("raw error leaked")
		}
		if w.Code < 400 {
			t.Fatal(w.Code)
		}
	}
}
