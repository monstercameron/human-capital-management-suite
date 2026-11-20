package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3785Port struct {
	calls                              int
	principal                          chat.Principal
	host, conversation, thread, anchor string
	brief                              chatrecipient.CatchUpBrief
	err                                error
}

func (f *issue3785Port) CatchUp(_ context.Context, p chat.Principal, host, conversation, thread, anchor string, _ time.Time) (chatrecipient.CatchUpBrief, error) {
	f.calls++
	f.principal, f.host, f.conversation, f.thread, f.anchor = p, host, conversation, thread, anchor
	return f.brief, f.err
}

func issue3785Do(t *testing.T, ctx context.Context, method, body string, port ChatCatchUpPort) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, ChatCatchUpPath, strings.NewReader(body)).WithContext(ctx)
	ChatCatchUpHandler{Service: port, Now: func() time.Time { return time.Date(2026, 3, 10, 8, 5, 0, 0, time.UTC) }}.ServeHTTP(w, r)
	return w
}

func TestIssue3785_CatchUpHTTP_ReturnsTheSessionsOwnPrivateBrief(t *testing.T) {
	at := time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC)
	port := &issue3785Port{brief: chatrecipient.CatchUpBrief{ConversationName: "ops", Cursor: 4, After: 4, Through: 20, CoveredFrom: 5, Total: 16, Covered: 16, Generated: at, Method: chatrecipient.CatchUpMethodExtractive,
		Sections: []chatrecipient.CatchUpSection{{Kind: chatrecipient.CatchUpAsked, More: 2, Lines: []chatrecipient.CatchUpLine{{PostID: "m1", Sequence: 7, AuthorID: "dana", Text: "can you look", At: at}}}}}}
	w := issue3785Do(t, chatsaveHTTPContext(t, trust.SubjectKindHuman), http.MethodPost,
		`{"conversation_id":"room","thread_root_id":"root","since_post_id":"m0"}`, port)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	if port.principal.SubjectID != "person" || port.host != "tenant-a" || port.conversation != "room" || port.thread != "root" || port.anchor != "m0" {
		t.Fatalf("the person comes from the session: %+v host=%s conv=%s thread=%s anchor=%s", port.principal, port.host, port.conversation, port.thread, port.anchor)
	}
	var reply ChatCatchUpReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Through != 20 || reply.Cursor != 4 || reply.Total != 16 || reply.Omitted != 0 || reply.Method != "extractive" || reply.ConversationName != "ops" || len(reply.Sections) != 1 {
		t.Fatalf("reply = %s", w.Body.String())
	}
	if s := reply.Sections[0]; s.Kind != "asked_you" || s.More != 2 || len(s.Lines) != 1 || s.Lines[0].PostID != "m1" || s.Lines[0].Sequence != 7 || s.Lines[0].AuthorID != "dana" || s.Lines[0].Text != "can you look" {
		t.Fatalf("section = %+v", s)
	}
}

func TestIssue3785_CatchUpHTTP_Refusals_Security(t *testing.T) {
	human := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	ask := `{"conversation_id":"room"}`
	cases := []struct {
		name   string
		ctx    context.Context
		method string
		body   string
		port   ChatCatchUpPort
		status int
		code   string
	}{
		{"no session", context.Background(), http.MethodPost, ask, &issue3785Port{}, 401, "unauthenticated"},
		{"service account", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodPost, ask, &issue3785Port{}, 403, "permission_denied"},
		{"get", human, http.MethodGet, "", &issue3785Port{}, 405, "method_not_allowed"},
		{"no store", human, http.MethodPost, ask, nil, 503, "unavailable"},
		{"outage", human, http.MethodPost, ask, &issue3785Port{err: chat.ErrUnavailable}, 503, "unavailable"},
		{"not a member", human, http.MethodPost, ask, &issue3785Port{err: chat.ErrPermissionDenied}, 403, "permission_denied"},
		{"unreadable anchor", human, http.MethodPost, `{"conversation_id":"room","since_post_id":"x"}`, &issue3785Port{err: chat.ErrNotFound}, 404, "not_found"},
		{"no conversation", human, http.MethodPost, `{}`, &issue3785Port{}, 400, "invalid_argument"},
		{"unknown field", human, http.MethodPost, `{"conversation_id":"room","subject":"x"}`, &issue3785Port{}, 400, "invalid_argument"},
		{"trailing data", human, http.MethodPost, ask + ask, &issue3785Port{}, 400, "invalid_argument"},
	}
	for _, c := range cases {
		var service ChatCatchUpPort = c.port
		if p, ok := c.port.(*issue3785Port); ok && p == nil {
			service = nil
		}
		w := issue3785Do(t, c.ctx, c.method, c.body, service)
		var failure ChatCatchUpError
		_ = json.Unmarshal(w.Body.Bytes(), &failure)
		if w.Code != c.status || failure.Code != c.code {
			t.Fatalf("%s: status=%d body=%s, want %d %s", c.name, w.Code, w.Body.String(), c.status, c.code)
		}
		if p, ok := c.port.(*issue3785Port); ok && p != nil && c.status != 503 && c.status != 403 && c.status != 404 && p.calls != 0 {
			t.Fatalf("%s: the service was reached before the request was admitted", c.name)
		}
	}
}

func TestIssue3785_CatchUpHTTP_RefusalNeverCarriesTheCause_Security(t *testing.T) {
	port := &issue3785Port{err: context.DeadlineExceeded}
	w := issue3785Do(t, chatsaveHTTPContext(t, trust.SubjectKindHuman), http.MethodPost, `{"conversation_id":"room"}`, port)
	if w.Code != 503 || strings.Contains(w.Body.String(), "deadline") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestIssue3785_CatchUpOverlay_AdmitsBeforeServing_Security(t *testing.T) {
	port := &issue3785Port{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := OverlayChatCatchUp(next, port, transport.Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/chat/other", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("other paths pass through: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatCatchUpPath, strings.NewReader(`{"conversation_id":"room"}`)))
	if w.Code == http.StatusOK || port.calls != 0 {
		t.Fatalf("an unauthenticated request must be denied: status=%d reads=%d", w.Code, port.calls)
	}
}
