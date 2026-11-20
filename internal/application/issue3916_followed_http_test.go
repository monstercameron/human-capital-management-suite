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

type issue3916Port struct {
	calls     int
	principal chat.Principal
	host      string
	rows      []chatrecipient.FollowedThread
	err       error
}

func (f *issue3916Port) FollowedThreads(_ context.Context, p chat.Principal, host string) ([]chatrecipient.FollowedThread, error) {
	f.calls++
	f.principal, f.host = p, host
	return f.rows, f.err
}

func TestIssue3916_FollowedThreadsHTTP_ListsTheSessionsOwnThreads(t *testing.T) {
	port := &issue3916Port{rows: []chatrecipient.FollowedThread{{ConversationID: "room", RootPostID: "root", RootSequence: 4, RootAuthorID: "ana", RootBody: "plans", ReplyCount: 2, LastActivity: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Revision: 3}}}
	w := httptest.NewRecorder()
	// A subject in the query string is ignored: the identity is the session's.
	r := httptest.NewRequest(http.MethodGet, FollowedThreadsPath+"?subject=someone-else", nil).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	FollowedThreadsHandler{Service: port}.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if port.principal.SubjectID != "person" || port.principal.TenantID != "tenant-a" || port.host != "tenant-a" {
		t.Fatalf("identity not taken from the session: %+v host=%s", port.principal, port.host)
	}
	var reply FollowedThreadsReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || len(reply.Threads) != 1 {
		t.Fatalf("reply=%s err=%v", w.Body.String(), err)
	}
	got := reply.Threads[0]
	if got.ConversationID != "room" || got.RootPostID != "root" || got.RootSequence != 4 || got.ReplyCount != 2 || got.Revision != 3 || got.RootBody != "plans" {
		t.Fatalf("thread=%+v", got)
	}
}

func TestIssue3916_FollowedThreadsHTTP_Refusals_Security(t *testing.T) {
	human := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	cases := []struct {
		name   string
		ctx    context.Context
		method string
		port   FollowedThreadsPort
		status int
		code   string
	}{
		{"no session", context.Background(), http.MethodGet, &issue3916Port{}, 401, "unauthenticated"},
		{"service account", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodGet, &issue3916Port{}, 403, "permission_denied"},
		{"write method", human, http.MethodPost, &issue3916Port{}, 405, "method_not_allowed"},
		{"no store", human, http.MethodGet, nil, 503, "unavailable"},
		{"outage", human, http.MethodGet, &issue3916Port{err: chat.ErrUnavailable}, 503, "unavailable"},
		{"not a member", human, http.MethodGet, &issue3916Port{err: chat.ErrPermissionDenied}, 403, "permission_denied"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		FollowedThreadsHandler{Service: c.port}.ServeHTTP(w, httptest.NewRequest(c.method, FollowedThreadsPath, nil).WithContext(c.ctx))
		if w.Code != c.status || !strings.Contains(w.Body.String(), `"code":"`+c.code+`"`) {
			t.Fatalf("%s: status=%d body=%s", c.name, w.Code, w.Body.String())
		}
	}
	if port := (&issue3916Port{}); port.calls != 0 {
		t.Fatal("unreachable")
	}
}

func TestIssue3916_FollowedThreadsHTTP_ServiceAccountNeverReachesTheStore_Security(t *testing.T) {
	port := &issue3916Port{}
	w := httptest.NewRecorder()
	FollowedThreadsHandler{Service: port}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, FollowedThreadsPath, nil).WithContext(chatsaveHTTPContext(t, trust.SubjectKindService)))
	if port.calls != 0 {
		t.Fatalf("the list was read for a service account")
	}
}
