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

type issue3787Port struct {
	lists, sets int
	principal   chat.Principal
	host        string
	rows        []chatrecipient.FollowUp
	err         error
	set         [4]string
	until       time.Time
}

func (f *issue3787Port) FollowUps(_ context.Context, p chat.Principal, host string, _ time.Time) ([]chatrecipient.FollowUp, error) {
	f.lists++
	f.principal, f.host = p, host
	return f.rows, f.err
}

func (f *issue3787Port) SetFollowUp(_ context.Context, p chat.Principal, host, conversation, post, direction, status string, until, _ time.Time) error {
	f.sets++
	f.principal, f.host = p, host
	f.set = [4]string{conversation + "/" + post, direction, status, ""}
	f.until = until
	return f.err
}

func TestIssue3787_FollowUpsHTTP_ListsTheSessionsOwnRows(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	port := &issue3787Port{rows: []chatrecipient.FollowUp{
		{ConversationID: "room", PostID: "m1", Sequence: 4, Direction: chatrecipient.FollowUpOwed, AuthorID: "dana", Body: "can you confirm?", AskedAt: at, Evidence: chatrecipient.FollowUpEvidenceReacted, EvidenceAt: at.Add(time.Hour)},
		{ConversationID: "room", PostID: "m2", Sequence: 6, Direction: chatrecipient.FollowUpWaiting, AuthorID: "person", Body: "@Priya swap?", AskedAt: at, WaitingOn: []string{"priya"}, Kept: true},
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, FollowUpsPath+"?subject=someone-else", nil).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	FollowUpsHandler{Service: port}.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if port.principal.SubjectID != "person" || port.principal.TenantID != "tenant-a" || port.host != "tenant-a" {
		t.Fatalf("identity not taken from the session: %+v host=%s", port.principal, port.host)
	}
	var reply FollowUpsReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || len(reply.Owed) != 1 || len(reply.Waiting) != 1 {
		t.Fatalf("reply=%s err=%v", w.Body.String(), err)
	}
	if o := reply.Owed[0]; o.PostID != "m1" || o.AuthorID != "dana" || o.Evidence != "reacted" || o.EvidenceAt == nil || o.Body != "can you confirm?" {
		t.Fatalf("owed=%+v", o)
	}
	if x := reply.Waiting[0]; x.PostID != "m2" || len(x.WaitingOn) != 1 || x.WaitingOn[0] != "priya" || !x.Kept || x.EvidenceAt != nil {
		t.Fatalf("waiting=%+v", x)
	}
}

func TestIssue3787_FollowUpsHTTP_RecordsTheChoice(t *testing.T) {
	port := &issue3787Port{}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	until := now.Add(4 * time.Hour)
	body := `{"conversation_id":"room","post_id":"m1","direction":"owed","action":"snooze","until":"` + until.Format(time.RFC3339) + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, FollowUpsPath, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	FollowUpsHandler{Service: port, Now: func() time.Time { return now }}.ServeHTTP(w, r)
	if w.Code != 200 || port.sets != 1 || port.set != [4]string{"room/m1", "owed", "snoozed", ""} || !port.until.Equal(until) || port.principal.SubjectID != "person" {
		t.Fatalf("status=%d body=%s port=%+v", w.Code, w.Body.String(), port)
	}
	for action, want := range map[string]string{"dismiss": "dismissed", "resolve": "resolved", "keep": "kept"} {
		w = httptest.NewRecorder()
		r = httptest.NewRequest(http.MethodPost, FollowUpsPath, strings.NewReader(`{"conversation_id":"room","post_id":"m1","direction":"waiting","action":"`+action+`"}`)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
		FollowUpsHandler{Service: port}.ServeHTTP(w, r)
		if w.Code != 200 || port.set[2] != want {
			t.Fatalf("%s: status=%d stored=%v", action, w.Code, port.set)
		}
	}
}

func TestIssue3787_FollowUpsHTTP_Refusals_Security(t *testing.T) {
	human := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	good := `{"conversation_id":"room","post_id":"m1","direction":"owed","action":"dismiss"}`
	cases := []struct {
		name   string
		ctx    context.Context
		method string
		body   string
		port   FollowUpsPort
		status int
		code   string
	}{
		{"no session", context.Background(), http.MethodGet, "", &issue3787Port{}, 401, "unauthenticated"},
		{"service account reads", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodGet, "", &issue3787Port{}, 403, "permission_denied"},
		{"service account writes", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodPost, good, &issue3787Port{}, 403, "permission_denied"},
		{"delete method", human, http.MethodDelete, "", &issue3787Port{}, 405, "method_not_allowed"},
		{"no store", human, http.MethodGet, "", nil, 503, "unavailable"},
		{"outage", human, http.MethodGet, "", &issue3787Port{err: chat.ErrUnavailable}, 503, "unavailable"},
		{"not a member", human, http.MethodPost, good, &issue3787Port{err: chat.ErrPermissionDenied}, 403, "permission_denied"},
		{"unknown message", human, http.MethodPost, good, &issue3787Port{err: chat.ErrNotFound}, 404, "not_found"},
		{"unknown action", human, http.MethodPost, `{"conversation_id":"room","post_id":"m1","direction":"owed","action":"delete"}`, &issue3787Port{}, 400, "invalid_argument"},
		{"unknown field", human, http.MethodPost, `{"conversation_id":"room","post_id":"m1","direction":"owed","action":"dismiss","subject":"x"}`, &issue3787Port{}, 400, "invalid_argument"},
		{"trailing data", human, http.MethodPost, good + good, &issue3787Port{}, 400, "invalid_argument"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		var service FollowUpsPort = c.port
		if p, ok := c.port.(*issue3787Port); ok && p == nil {
			service = nil
		}
		FollowUpsHandler{Service: service}.ServeHTTP(w, httptest.NewRequest(c.method, FollowUpsPath, strings.NewReader(c.body)).WithContext(c.ctx))
		if w.Code != c.status || !strings.Contains(w.Body.String(), `"code":"`+c.code+`"`) {
			t.Fatalf("%s: status=%d body=%s", c.name, w.Code, w.Body.String())
		}
	}
	// A refused request reaches no port.
	port := &issue3787Port{}
	w := httptest.NewRecorder()
	FollowUpsHandler{Service: port}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, FollowUpsPath, strings.NewReader(`{"action":"delete"}`)).WithContext(human))
	if port.sets != 0 || port.lists != 0 {
		t.Fatal("a refused request reached the service")
	}
}
