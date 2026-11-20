package application

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

type issue3789Port struct {
	principal chat.Principal
	away      chatrecipient.Away
	saved     chatrecipient.Away
	puts      int
	expected  uint64
	asked     []string
	public    []chatrecipient.AwayPublic
	err       error
	putErr    error
}

func (f *issue3789Port) Away(_ context.Context, p chat.Principal, _ time.Time) (chatrecipient.Away, error) {
	f.principal = p
	return f.away, f.err
}
func (f *issue3789Port) PutAway(_ context.Context, p chat.Principal, a chatrecipient.Away, expected uint64, _ time.Time) (chatrecipient.Away, error) {
	f.puts++
	f.principal, f.saved, f.expected = p, a, expected
	if f.putErr != nil {
		return chatrecipient.Away{}, f.putErr
	}
	a.Revision = expected + 1
	f.away = a
	return a, nil
}
func (f *issue3789Port) AwayOf(_ context.Context, p chat.Principal, ids []string, _ time.Time) ([]chatrecipient.AwayPublic, error) {
	f.principal, f.asked = p, ids
	return f.public, f.err
}

var issue3789Now = time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

func issue3789Handler(port ChatAwayPort) ChatAwayHandler {
	return ChatAwayHandler{Service: port, Now: func() time.Time { return issue3789Now }}
}

func TestIssue3789_AwayHTTP_ReadsAndSavesTheSessionsOwnSetting(t *testing.T) {
	until := issue3789Now.Add(72 * time.Hour)
	port := &issue3789Port{away: chatrecipient.Away{Since: issue3789Now, Until: until, CoverID: "dana", NotifyCover: true, Revision: 3}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, ChatAwayPath+"?subject=someone-else", nil).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	issue3789Handler(port).ServeHTTP(w, r)
	var reply ChatAwayReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d body=%s err=%v", w.Code, w.Body.String(), err)
	}
	if port.principal.SubjectID != "person" || !reply.Active || reply.CoverID != "dana" || !reply.NotifyCover || reply.Revision != 3 || reply.Until == nil || !reply.Until.Equal(until) {
		t.Fatalf("reply=%s principal=%+v", w.Body.String(), port.principal)
	}

	w = httptest.NewRecorder()
	body := `{"expected_revision":3,"until":"2026-03-09T10:00:00Z","cover_id":"erin","hidden":true,"notify_cover":false}`
	r = httptest.NewRequest(http.MethodPost, ChatAwayPath, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	issue3789Handler(port).ServeHTTP(w, r)
	if w.Code != 200 || port.puts != 1 || port.expected != 3 || port.saved.CoverID != "erin" || !port.saved.Hidden || port.saved.NotifyCover || !port.saved.Until.Equal(time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("status=%d body=%s saved=%+v", w.Code, w.Body.String(), port.saved)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Revision != 4 || !reply.Hidden {
		t.Fatalf("reply=%s err=%v", w.Body.String(), err)
	}

	// An ended absence is saved by sending no until, and reads as not away.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, ChatAwayPath, strings.NewReader(`{"expected_revision":4}`)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	issue3789Handler(port).ServeHTTP(w, r)
	reply = ChatAwayReply{}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 || reply.Active || reply.Until != nil || reply.CoverID != "" {
		t.Fatalf("ended: %d %s", w.Code, w.Body.String())
	}
}

func TestIssue3789_AwayHTTP_LookupShowsOnlyWhatTheOwnerShares_Security(t *testing.T) {
	port := &issue3789Port{public: []chatrecipient.AwayPublic{{MemberID: "bob", Until: issue3789Now.Add(time.Hour), CoverID: "cy"}}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, ChatAwayLookupPath, strings.NewReader(`{"members":["bob","dee"]}`)).WithContext(chatsaveHTTPContext(t, trust.SubjectKindHuman))
	issue3789Handler(port).ServeHTTP(w, r)
	var reply ChatAwayLookupReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 || len(reply.People) != 1 || reply.People[0].MemberID != "bob" || reply.People[0].CoverID != "cy" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(port.asked) != 2 {
		t.Fatalf("asked=%v", port.asked)
	}
	// The wire shape carries nothing but who, until when and the cover.
	var raw map[string][]map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	for key := range raw["people"][0] {
		if key != "member_id" && key != "until" && key != "cover_id" {
			t.Fatalf("lookup leaks %q", key)
		}
	}
}

func TestIssue3789_AwayHTTP_Refusals_Security(t *testing.T) {
	human := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	cases := []struct {
		name   string
		ctx    context.Context
		method string
		path   string
		body   string
		port   *issue3789Port
		status int
		code   string
	}{
		{"no session", context.Background(), http.MethodGet, ChatAwayPath, "", &issue3789Port{}, 401, "unauthenticated"},
		{"service account reads", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodGet, ChatAwayPath, "", &issue3789Port{}, 403, "permission_denied"},
		{"service account writes", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodPost, ChatAwayPath, `{"expected_revision":1}`, &issue3789Port{}, 403, "permission_denied"},
		{"service account looks up", chatsaveHTTPContext(t, trust.SubjectKindService), http.MethodPost, ChatAwayLookupPath, `{"members":["a"]}`, &issue3789Port{}, 403, "permission_denied"},
		{"delete method", human, http.MethodDelete, ChatAwayPath, "", &issue3789Port{}, 405, "method_not_allowed"},
		{"lookup by get", human, http.MethodGet, ChatAwayLookupPath, "", &issue3789Port{}, 405, "method_not_allowed"},
		{"no store", human, http.MethodGet, ChatAwayPath, "", nil, 503, "unavailable"},
		{"outage", human, http.MethodGet, ChatAwayPath, "", &issue3789Port{err: chat.ErrUnavailable}, 503, "unavailable"},
		{"stale revision", human, http.MethodPost, ChatAwayPath, `{"expected_revision":1}`, &issue3789Port{putErr: chat.ErrConflict}, 409, "conflict"},
		{"refused cover", human, http.MethodPost, ChatAwayPath, `{"expected_revision":1,"until":"2026-03-09T10:00:00Z","cover_id":"x"}`, &issue3789Port{putErr: chat.ErrInvalidArgument}, 400, "invalid_argument"},
		{"unknown field", human, http.MethodPost, ChatAwayPath, `{"expected_revision":1,"subject":"x"}`, &issue3789Port{}, 400, "invalid_argument"},
		{"trailing data", human, http.MethodPost, ChatAwayPath, `{"expected_revision":1}{"expected_revision":1}`, &issue3789Port{}, 400, "invalid_argument"},
		{"bad time", human, http.MethodPost, ChatAwayPath, `{"expected_revision":1,"until":"tomorrow"}`, &issue3789Port{}, 400, "invalid_argument"},
		{"lookup without a body", human, http.MethodPost, ChatAwayLookupPath, ``, &issue3789Port{}, 400, "invalid_argument"},
		{"lookup unknown field", human, http.MethodPost, ChatAwayLookupPath, `{"members":["a"],"all":true}`, &issue3789Port{}, 400, "invalid_argument"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		var service ChatAwayPort
		if c.port != nil {
			service = c.port
		}
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body)).WithContext(c.ctx)
		issue3789Handler(service).ServeHTTP(w, r)
		if w.Code != c.status || !strings.Contains(w.Body.String(), `"`+c.code+`"`) {
			t.Fatalf("%s: status=%d body=%s", c.name, w.Code, w.Body.String())
		}
		if c.port != nil && c.status != 200 && c.port.puts != 0 && c.name != "stale revision" && c.name != "refused cover" {
			t.Fatalf("%s: a refused request still saved", c.name)
		}
		if strings.Contains(w.Body.String(), "chat:") {
			t.Fatalf("%s: the body carries a cause: %s", c.name, w.Body.String())
		}
	}
}

// TestIssue3789_Away_Integration_Security drives the setting and the cover
// notice through the served handler assembly over a really composed chat
// runtime: bob says he is away with alice covering and asks her to be told;
// carol's mention of bob reaches alice as a notice that names bob and the room
// and carries no text; a colleague sees the absence until bob hides it; the
// cover gains no access to a room she is not in; and losing the room removes
// the notice.
func TestIssue3789_Away_Integration_Security(t *testing.T) {
	ctx := context.Background()
	chatDB, coreDB := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chatDB.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	databaseURL := func(raw, schema string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	core, err := pgxadapter.NewPool(ctx, databaseURL(coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "issue-3789-away-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer deployment.close()
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-alice", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-alice"})
	if err != nil {
		t.Fatal(err)
	}
	aliceCtx := trust.WithPrincipal(ctx, principal)
	alice := chat.Principal{TenantID: "host", SubjectID: "alice"}
	room, err := deployment.service.CreateConversation(aliceCtx, chat.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chat.PublicChannel, Name: "ops-room", IdempotencyKey: "issue-3789-room"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := deployment.service.CreateConversation(aliceCtx, chat.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chat.PublicChannel, Name: "board-room", IdempotencyKey: "issue-3789-secret"})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := chatDB.SQL.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, who := range []string{"bob", "carol"} {
		exec(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state) VALUES('host',$1,'host',$2,'member','active')`, room.ID, who)
	}
	// carol and bob share a second room that alice has been removed from.
	for _, who := range []string{"bob", "carol"} {
		exec(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state) VALUES('host',$1,'host',$2,'member','active')`, secret.ID, who)
	}
	exec(`UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND conversation_id=$1 AND member_id='alice'`, secret.ID)
	mention := `[{"Kind":"PERSON_MENTION","TenantID":"host","ID":"bob","Display":"bob"}]`
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,created_at) VALUES('carol-asks','host',$1,'carol','host',2000,'@bob the figures are ready','`+mention+`',now()+interval '1 minute')`, room.ID)
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,created_at) VALUES('carol-secret','host',$1,'carol','host',2001,'@bob private numbers','`+mention+`',now()+interval '1 minute')`, secret.ID)

	call := func(method, path, tenant, subject, body string) (int, string) {
		t.Helper()
		admission, bearer := integrate1Admission(t, tenant, subject, time.Now)
		handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	feed := func(subject string) ChatNotificationsReply {
		t.Helper()
		code, body := call(http.MethodGet, ChatNotificationsPath+"?host=host", "host", subject, "")
		var reply ChatNotificationsReply
		if code != http.StatusOK || json.Unmarshal([]byte(body), &reply) != nil {
			t.Fatalf("feed of %s: %d %s", subject, code, body)
		}
		return reply
	}
	until := time.Now().UTC().Add(96 * time.Hour).Format(time.RFC3339)

	code, body := call(http.MethodGet, ChatAwayPath, "host", "bob", "")
	if code != 200 || !strings.Contains(body, `"active":false`) || !strings.Contains(body, `"revision":1`) {
		t.Fatalf("default: %d %s", code, body)
	}
	// Nothing is shown to alice before bob asks for it.
	if reply := feed("alice"); len(reply.Items) != 0 {
		t.Fatalf("alice was told before bob chose: %+v", reply)
	}
	// A cover who is nobody here is refused.
	if code, body = call(http.MethodPost, ChatAwayPath, "host", "bob", `{"expected_revision":1,"until":"`+until+`","cover_id":"nobody","notify_cover":true}`); code != http.StatusBadRequest {
		t.Fatalf("unknown cover: %d %s", code, body)
	}
	if code, body = call(http.MethodPost, ChatAwayPath, "host", "bob", `{"expected_revision":1,"until":"`+until+`","cover_id":"alice","notify_cover":true}`); code != 200 || !strings.Contains(body, `"cover_id":"alice"`) || !strings.Contains(body, `"revision":2`) {
		t.Fatalf("save: %d %s", code, body)
	}
	if code, _ = call(http.MethodPost, ChatAwayPath, "host", "bob", `{"expected_revision":1}`); code != http.StatusConflict {
		t.Fatalf("stale: %d", code)
	}

	// carol sees the absence and the cover; the lookup is company-scoped.
	code, body = call(http.MethodPost, ChatAwayLookupPath, "host", "carol", `{"members":["bob","alice"]}`)
	if code != 200 || !strings.Contains(body, `"member_id":"bob"`) || !strings.Contains(body, `"cover_id":"alice"`) || strings.Contains(body, `"member_id":"alice"`) {
		t.Fatalf("lookup: %d %s", code, body)
	}
	if code, body = call(http.MethodPost, ChatAwayLookupPath, "other-company", "carol", `{"members":["bob"]}`); code != 200 || strings.Contains(body, "bob") {
		t.Fatalf("another company saw it: %d %s", code, body)
	}

	// alice is told about the mention she can read, and about nothing else.
	reply := feed("alice")
	if len(reply.Items) != 1 || reply.Unread != 1 {
		t.Fatalf("alice's feed: %+v", reply)
	}
	n := reply.Items[0]
	if n.Kind != "cover" || n.CoveringID != "bob" || n.PostID != "carol-asks" || n.AuthorID != "carol" || n.ConversationName != "ops-room" || n.Body != "" {
		t.Fatalf("cover notice: %+v", n)
	}
	if _, raw := call(http.MethodGet, ChatNotificationsPath+"?host=host", "host", "alice", ""); strings.Contains(raw, "figures") || strings.Contains(raw, "private numbers") || strings.Contains(raw, "board-room") {
		t.Fatalf("the notice leaked text or a room alice cannot open: %s", raw)
	}
	// Someone who is not the cover is not told.
	if reply := feed("dana"); len(reply.Items) != 0 {
		t.Fatalf("dana was told: %+v", reply)
	}
	// Reading it is remembered.
	if code, body = call(http.MethodPost, ChatNotificationsPath, "host", "alice", `{"host":"host","ids":["carol-asks"]}`); code != 200 || !strings.Contains(body, `"marked":1`) {
		t.Fatalf("mark: %d %s", code, body)
	}
	if reply := feed("alice"); reply.Unread != 0 || len(reply.Items) != 1 || !reply.Items[0].Read {
		t.Fatalf("after marking: %+v", reply)
	}

	// Hiding the absence takes it off every colleague's view but leaves the cover's notice.
	if code, body = call(http.MethodPost, ChatAwayPath, "host", "bob", `{"expected_revision":2,"until":"`+until+`","cover_id":"alice","hidden":true,"notify_cover":true}`); code != 200 {
		t.Fatalf("hide: %d %s", code, body)
	}
	if code, body = call(http.MethodPost, ChatAwayLookupPath, "host", "carol", `{"members":["bob"]}`); code != 200 || strings.Contains(body, "bob") {
		t.Fatalf("a hidden absence was shown: %d %s", code, body)
	}
	// Ending the absence stops the notices, and losing the room removes them.
	if code, body = call(http.MethodPost, ChatAwayPath, "host", "bob", `{"expected_revision":3,"hidden":true}`); code != 200 || !strings.Contains(body, `"active":false`) {
		t.Fatalf("end: %d %s", code, body)
	}
	if reply := feed("alice"); len(reply.Items) != 0 {
		t.Fatalf("an ended absence still notified: %+v", reply)
	}
}
