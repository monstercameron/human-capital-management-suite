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

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// TestIssue3733_ChatNotifications_Integration_Security drives GET and POST
// /api/chat/notifications through the served handler assembly over a really
// composed chat runtime: a mention and a direct message are listed for the
// person they are for and nobody else, reading one is remembered across
// sessions, another person and another company see nothing, a message cannot be
// marked read by someone it is not a notification for, and losing membership
// removes the items.
func TestIssue3733_ChatNotifications_Integration_Security(t *testing.T) {
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
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "issue-3733-notifications-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
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
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "notify-room", IdempotencyKey: "issue-3733-room"})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := chatDB.SQL.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state) VALUES('host',$1,'host','bob','member','active')`, room.ID)
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,created_at) VALUES('bob-mentions','host',$1,'bob','host',1000,'@alice can you confirm the swap?','[{"Kind":"PERSON_MENTION","TenantID":"host","ID":"alice","Display":"alice"}]',now()-interval '5 hours')`, room.ID)
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at) VALUES('bob-plain','host',$1,'bob','host',1001,'lunch anyone?',now()-interval '4 hours')`, room.ID)

	call := func(method, tenant, subject, body string) (int, ChatNotificationsReply, string) {
		t.Helper()
		admission, bearer := integrate1Admission(t, tenant, subject, time.Now)
		handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
		r := httptest.NewRequest(method, ChatNotificationsPath+"?host=host", strings.NewReader(body))
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var reply ChatNotificationsReply
		if method == http.MethodGet && w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, reply, w.Body.String()
	}
	code, reply, _ := call(http.MethodGet, "host", "alice", "")
	if code != http.StatusOK || len(reply.Items) != 1 || reply.Unread != 1 {
		t.Fatalf("alice's feed: %d %+v", code, reply)
	}
	if n := reply.Items[0]; n.PostID != "bob-mentions" || n.Kind != "mention" || n.AuthorID != "bob" || n.ConversationID != room.ID || n.ConversationName != "notify-room" || n.Read || n.Sequence != 1000 {
		t.Fatalf("item = %+v", n)
	}
	// The author of the message, another person, and the same subject id in
	// another company have nothing.
	if code, reply, _ := call(http.MethodGet, "host", "bob", ""); code == http.StatusOK && len(reply.Items) != 0 {
		t.Fatalf("bob was notified of his own message: %+v", reply)
	}
	if code, reply, _ := call(http.MethodGet, "other-company", "alice", ""); code == http.StatusOK && len(reply.Items) != 0 {
		t.Fatalf("another company saw alice's notifications: %+v", reply)
	}
	// bob cannot mark alice's notification: it is not one of his, and nothing is stored.
	if code, _, body := call(http.MethodPost, "host", "bob", `{"host":"host","ids":["bob-mentions"]}`); code != http.StatusOK || !strings.Contains(body, `"marked":0`) {
		t.Fatalf("bob marking alice's item: %d %s", code, body)
	}
	if code, reply, _ := call(http.MethodGet, "host", "alice", ""); code != http.StatusOK || reply.Unread != 1 {
		t.Fatalf("bob changed alice's read state: %d %+v", code, reply)
	}
	// Reading is remembered: every later read, from any session, sees it.
	if code, _, body := call(http.MethodPost, "host", "alice", `{"host":"host","ids":["bob-mentions"]}`); code != http.StatusOK || !strings.Contains(body, `"marked":1`) {
		t.Fatalf("alice marking: %d %s", code, body)
	}
	for i := 0; i < 2; i++ {
		if code, reply, _ := call(http.MethodGet, "host", "alice", ""); code != http.StatusOK || reply.Unread != 0 || len(reply.Items) != 1 || !reply.Items[0].Read {
			t.Fatalf("after marking (read %d): %d %+v", i, code, reply)
		}
	}
	// An unknown id and an unreadable body are refused or skipped, never stored.
	if code, _, _ := call(http.MethodPost, "host", "alice", `{"host":"host","ids":["bob-mentions"],"subject":"bob"}`); code != http.StatusBadRequest {
		t.Fatalf("a request naming a person: %d", code)
	}
	// Access loss: the item disappears and a write marks nothing.
	exec(`UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND conversation_id=$1 AND member_id='alice'`, room.ID)
	if code, reply, _ := call(http.MethodGet, "host", "alice", ""); code == http.StatusOK && len(reply.Items) != 0 {
		t.Fatalf("an item survived the loss of access: %d %+v", code, reply)
	}
	if code, _, body := call(http.MethodPost, "host", "alice", `{"host":"host","all":true}`); code == http.StatusOK && !strings.Contains(body, `"marked":0`) {
		t.Fatalf("a removed member marked something: %d %s", code, body)
	}
}
