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

type issue3786Port struct {
	reads, sets, seens int
	principal          chat.Principal
	host               string
	digest             chatrecipient.Digest
	set                chatrecipient.DigestPref
	through            time.Time
	err                error
}

func (f *issue3786Port) DigestPrefs(context.Context, chat.Principal) (chatrecipient.DigestPref, error) {
	return f.digest.Pref, f.err
}
func (f *issue3786Port) PutDigestPrefs(_ context.Context, p chat.Principal, want chatrecipient.DigestPref, _ time.Time) (chatrecipient.DigestPref, error) {
	f.sets++
	f.principal, f.set = p, want
	return want, f.err
}
func (f *issue3786Port) Digest(_ context.Context, p chat.Principal, host string, _ time.Time) (chatrecipient.Digest, error) {
	f.reads++
	f.principal, f.host = p, host
	return f.digest, f.err
}
func (f *issue3786Port) DigestSeen(_ context.Context, p chat.Principal, through, _ time.Time) error {
	f.seens++
	f.principal, f.through = p, through
	return f.err
}

func TestIssue3786_DigestHTTP_ReadsTheSessionsOwnDigest(t *testing.T) {
	at := time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC)
	port := &issue3786Port{digest: chatrecipient.Digest{Due: true, Through: at.Add(6 * time.Hour),
		Pref: chatrecipient.DigestPref{Cadence: chatrecipient.DigestDaily, Minute: 480, Timezone: "UTC"},
		Items: []chatrecipient.DigestItem{
			{Why: chatrecipient.DigestWhyQuietHours, PostID: "m1", ConversationID: "room", ConversationName: "ops", ConversationKind: "PUBLIC_CHANNEL", Sequence: 4, AuthorID: "dana", At: at, Count: 1},
			{Why: chatrecipient.DigestWhyFollowed, PostID: "m2", ConversationID: "room", ConversationKind: "PUBLIC_CHANNEL", ThreadRootID: "m0", ThreadRootSequence: 2, Sequence: 9, AuthorID: "lee", At: at, Count: 3},
		}}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, ChatDigestPath+"?subject=someone-else", nil).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	ChatDigestHandler{Service: port}.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if port.principal.SubjectID != "person" || port.host != "tenant-a" {
		t.Fatalf("identity not taken from the session: %+v host=%s", port.principal, port.host)
	}
	var reply ChatDigestReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || !reply.Due || len(reply.Items) != 2 || reply.Pref.Cadence != "daily" || reply.Pref.Minute != 480 {
		t.Fatalf("reply=%s err=%v", w.Body.String(), err)
	}
	if a := reply.Items[0]; a.Why != "quiet_hours" || a.ConversationName != "ops" || a.AuthorID != "dana" || a.PostID != "m1" {
		t.Fatalf("first=%+v", a)
	}
	if b := reply.Items[1]; b.Count != 3 || b.ThreadRootID != "m0" || b.Why != "followed" {
		t.Fatalf("second=%+v", b)
	}
	if strings.Contains(w.Body.String(), `"body"`) {
		t.Fatal("a digest names messages and people, never their text")
	}
}

func TestIssue3786_DigestHTTP_SavesTheChoiceAndSeen(t *testing.T) {
	port := &issue3786Port{}
	do := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, ChatDigestPath, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
		ChatDigestHandler{Service: port}.ServeHTTP(w, r)
		return w
	}
	if w := do(`{"action":"set","cadence":"quiet_end","minute":300,"timezone":"Europe/Berlin"}`); w.Code != 200 || port.sets != 1 || port.set.Cadence != "quiet_end" || port.set.Minute != 300 || port.set.Timezone != "Europe/Berlin" || port.principal.SubjectID != "person" {
		t.Fatalf("status=%d body=%s port=%+v", w.Code, w.Body.String(), port)
	}
	if w := do(`{"action":"seen","through":"2026-03-10T08:05:00Z"}`); w.Code != 200 || port.seens != 1 || !port.through.Equal(time.Date(2026, 3, 10, 8, 5, 0, 0, time.UTC)) {
		t.Fatalf("seen: status=%d port=%+v", w.Code, port)
	}
}

func TestIssue3786_DigestHTTP_Refusals_Security(t *testing.T) {
	human := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	set := `{"action":"set","cadence":"daily"}`
	cases := []struct {
		name   string
		ctx    context.Context
		method string
		body   string
		port   ChatDigestPort
		status int
		code   string
	}{
		{"no session", context.Background(), http.MethodGet, "", &issue3786Port{}, 401, "unauthenticated"},
		{"service account reads", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodGet, "", &issue3786Port{}, 403, "permission_denied"},
		{"service account writes", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodPost, set, &issue3786Port{}, 403, "permission_denied"},
		{"delete method", human, http.MethodDelete, "", &issue3786Port{}, 405, "method_not_allowed"},
		{"no store", human, http.MethodGet, "", nil, 503, "unavailable"},
		{"outage", human, http.MethodGet, "", &issue3786Port{err: chat.ErrUnavailable}, 503, "unavailable"},
		{"bad choice", human, http.MethodPost, set, &issue3786Port{err: chat.ErrInvalidArgument}, 400, "invalid_argument"},
		{"unknown action", human, http.MethodPost, `{"action":"wipe"}`, &issue3786Port{}, 400, "invalid_argument"},
		{"unknown field", human, http.MethodPost, `{"action":"seen","subject":"x"}`, &issue3786Port{}, 400, "invalid_argument"},
		{"trailing data", human, http.MethodPost, set + set, &issue3786Port{}, 400, "invalid_argument"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		var service ChatDigestPort = c.port
		if p, ok := c.port.(*issue3786Port); ok && p == nil {
			service = nil
		}
		ChatDigestHandler{Service: service}.ServeHTTP(w, httptest.NewRequest(c.method, ChatDigestPath, strings.NewReader(c.body)).WithContext(c.ctx))
		if w.Code != c.status || !strings.Contains(w.Body.String(), `"code":"`+c.code+`"`) {
			t.Fatalf("%s: status=%d body=%s", c.name, w.Code, w.Body.String())
		}
	}
	port := &issue3786Port{}
	w := httptest.NewRecorder()
	ChatDigestHandler{Service: port}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatDigestPath, strings.NewReader(`{"junk":1}`)).WithContext(human))
	if port.sets+port.seens+port.reads != 0 {
		t.Fatal("a refused request reached the service")
	}
}
