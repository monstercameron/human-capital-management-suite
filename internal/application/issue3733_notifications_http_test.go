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
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3733Port struct {
	lists, marks int
	principal    chat.Principal
	host         string
	rows         []chatrecipient.Notification
	err          error
	ids          []string
	all          bool
}

func (f *issue3733Port) Notifications(_ context.Context, p chat.Principal, host string, _ time.Time) ([]chatrecipient.Notification, error) {
	f.lists++
	f.principal, f.host = p, host
	return f.rows, f.err
}

func (f *issue3733Port) MarkNotificationsRead(_ context.Context, p chat.Principal, host string, ids []string, all bool, _ time.Time) (int64, error) {
	f.marks++
	f.principal, f.host, f.ids, f.all = p, host, ids, all
	return int64(len(ids)), f.err
}

func TestIssue3733_NotificationsHTTP_ListsTheSessionsOwnItems(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	port := &issue3733Port{rows: []chatrecipient.Notification{
		{ID: "m1", PostID: "m1", Kind: chatrecipient.NotificationMention, ConversationID: "room", ConversationName: "ops", ConversationKind: "PUBLIC_CHANNEL", Sequence: 4, AuthorID: "dana", Body: "@you can you look", At: at},
		{ID: "m2", PostID: "m2", Kind: chatrecipient.NotificationReply, ConversationID: "room", ConversationKind: "PUBLIC_CHANNEL", Sequence: 9, ThreadRootID: "m0", ThreadRootSequence: 2, AuthorID: "lee", At: at.Add(time.Hour), Read: true},
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, ChatNotificationsPath+"?subject=someone-else", nil).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	ChatNotificationsHandler{Service: port}.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if port.principal.SubjectID != "person" || port.principal.TenantID != "tenant-a" || port.host != "tenant-a" {
		t.Fatalf("identity not taken from the session: %+v host=%s", port.principal, port.host)
	}
	var reply ChatNotificationsReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || len(reply.Items) != 2 || reply.Unread != 1 {
		t.Fatalf("reply=%s err=%v", w.Body.String(), err)
	}
	if a := reply.Items[0]; a.PostID != "m1" || a.Kind != "mention" || a.ConversationName != "ops" || a.AuthorID != "dana" || a.Read || a.Body == "" {
		t.Fatalf("first=%+v", a)
	}
	if b := reply.Items[1]; b.ThreadRootID != "m0" || b.ThreadRootSequence != 2 || !b.Read {
		t.Fatalf("second=%+v", b)
	}
}

func TestIssue3733_NotificationsHTTP_MarksRead(t *testing.T) {
	port := &issue3733Port{}
	do := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, ChatNotificationsPath, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
		ChatNotificationsHandler{Service: port}.ServeHTTP(w, r)
		return w
	}
	if w := do(`{"ids":["m1","m2"]}`); w.Code != 200 || port.marks != 1 || len(port.ids) != 2 || port.all || port.principal.SubjectID != "person" || !strings.Contains(w.Body.String(), `"marked":2`) {
		t.Fatalf("status=%d body=%s port=%+v", w.Code, w.Body.String(), port)
	}
	if w := do(`{"all":true}`); w.Code != 200 || port.marks != 2 || !port.all {
		t.Fatalf("mark all: status=%d port=%+v", w.Code, port)
	}
}

func TestIssue3733_NotificationsHTTP_Refusals_Security(t *testing.T) {
	human := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	good := `{"ids":["m1"]}`
	cases := []struct {
		name   string
		ctx    context.Context
		method string
		body   string
		port   ChatNotificationsPort
		status int
		code   string
	}{
		{"no session", context.Background(), http.MethodGet, "", &issue3733Port{}, 401, "unauthenticated"},
		{"service account reads", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodGet, "", &issue3733Port{}, 403, "permission_denied"},
		{"service account writes", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodPost, good, &issue3733Port{}, 403, "permission_denied"},
		{"delete method", human, http.MethodDelete, "", &issue3733Port{}, 405, "method_not_allowed"},
		{"no store", human, http.MethodGet, "", nil, 503, "unavailable"},
		{"outage", human, http.MethodGet, "", &issue3733Port{err: chat.ErrUnavailable}, 503, "unavailable"},
		{"bad ids", human, http.MethodPost, good, &issue3733Port{err: chat.ErrInvalidArgument}, 400, "invalid_argument"},
		{"unknown field", human, http.MethodPost, `{"ids":["m1"],"subject":"x"}`, &issue3733Port{}, 400, "invalid_argument"},
		{"trailing data", human, http.MethodPost, good + good, &issue3733Port{}, 400, "invalid_argument"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		var service ChatNotificationsPort = c.port
		if p, ok := c.port.(*issue3733Port); ok && p == nil {
			service = nil
		}
		ChatNotificationsHandler{Service: service}.ServeHTTP(w, httptest.NewRequest(c.method, ChatNotificationsPath, strings.NewReader(c.body)).WithContext(c.ctx))
		if w.Code != c.status || !strings.Contains(w.Body.String(), `"code":"`+c.code+`"`) {
			t.Fatalf("%s: status=%d body=%s", c.name, w.Code, w.Body.String())
		}
	}
	// A refused request reaches no port.
	port := &issue3733Port{}
	w := httptest.NewRecorder()
	ChatNotificationsHandler{Service: port}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatNotificationsPath, strings.NewReader(`{"junk":1}`)).WithContext(human))
	if port.marks != 0 || port.lists != 0 {
		t.Fatal("a refused request reached the service")
	}
}
