package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatstateHTTPFixture struct {
	request chat.ChangeChannelStatusRequest
	calls   int
	err     error
}

func (f *chatstateHTTPFixture) GetChannelStatus(context.Context, chat.GetConversationRequest) (chat.ChannelStatus, error) {
	f.calls++
	return chat.ChannelStatus{Status: chatpolicy.StatusOpen, Revision: 1}, f.err
}
func (f *chatstateHTTPFixture) AllowedStatusTransitions(context.Context, chat.GetConversationRequest) ([]chat.StatusTransition, error) {
	f.calls++
	return []chat.StatusTransition{{Status: chatpolicy.StatusLocked}}, f.err
}
func (f *chatstateHTTPFixture) ChangeChannelStatus(_ context.Context, r chat.ChangeChannelStatusRequest) (chat.ChannelStatus, error) {
	f.calls++
	f.request = r
	return chat.ChannelStatus{Status: r.Status, Revision: 2}, f.err
}
func chatstateHTTPRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "admin", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org", Purposes: []string{"chat"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	return r.WithContext(trust.WithPrincipal(r.Context(), p))
}

func TestTodo_CHATSTATE_001(t *testing.T) {
	f := &chatstateHTTPFixture{}
	h := ChannelStatusHandler{Service: f}
	for _, path := range []string{ChannelStatusPath + "room", ChannelStatusPath + "room/transitions"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, chatstateHTTPRequest(t, "GET", path, ""))
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("read = %d %s", w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, chatstateHTTPRequest(t, "POST", ChannelStatusPath+"room", `{"status":"LOCKED","expected_revision":1,"reason":"incident"}`))
	if w.Code != 200 || f.request.Principal.SubjectID != "admin" || f.request.TenantID != "tenant-a" || f.request.ConversationID != "room" {
		t.Fatalf("change = %d %+v", w.Code, f.request)
	}
	for _, test := range []struct {
		err  error
		code int
		body string
	}{{chat.ErrConflict, 409, "revision_conflict"}, {chat.ErrPermissionDenied, 403, "permission_denied"}, {chat.ErrChannelHeld, 409, "channel_held"}, {chat.ErrLastReopener, 409, "last_reopener"}, {chat.ErrNotFound, 404, "not_found"}, {chat.ErrUnavailable, 503, "unavailable"}, {chat.ErrChannelStatus, 409, "channel_status"}, {chat.ErrInvalidArgument, 400, "invalid_argument"}} {
		f.err = test.err
		w := httptest.NewRecorder()
		h.ServeHTTP(w, chatstateHTTPRequest(t, "GET", ChannelStatusPath+"room", ""))
		if w.Code != test.code || !strings.Contains(w.Body.String(), test.body) {
			t.Fatalf("typed refusal = %d %s", w.Code, w.Body)
		}
	}
}

func TestTodo_CHATSTATE_001_Security(t *testing.T) {
	f := &chatstateHTTPFixture{}
	h := ChannelStatusHandler{Service: f}
	for _, body := range []string{`{"tenant_id":"other"}`, `{"conversation_id":"other"}`, `{"principal":{"subject":"forged"}}`, `{} {}`, strings.Repeat("x", 65<<10)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, chatstateHTTPRequest(t, "POST", ChannelStatusPath+"room", body))
		if w.Code != 400 || f.calls != 0 {
			t.Fatalf("forged request = %d calls %d", w.Code, f.calls)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", ChannelStatusPath+"room", nil))
	if w.Code != 401 || f.calls != 0 {
		t.Fatal("unauthenticated effect")
	}
	nextCalls := 0
	overlay := OverlayChannelStatus(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalls++; w.WriteHeader(204) }), f, transport.Config{})
	w = httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 204 || nextCalls != 1 {
		t.Fatal("overlay swallowed unrelated route")
	}
	w = httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest("GET", ChannelStatusPath+"room", nil))
	if w.Code == 200 || f.calls != 0 {
		t.Fatal("admission bypass")
	}
}
