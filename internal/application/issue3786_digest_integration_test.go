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

// TestIssue3786_ChatDigest_Integration_Security drives /api/chat/digest through
// the served handler assembly over a really composed chat runtime: a person who
// chose a digest gets one holding the mention that arrived while they were
// away and the followed thread that moved; it carries no message text; someone
// else and another company get nothing; seeing it clears it; and a person who
// lost access to the room finds no trace of it.
func TestIssue3786_ChatDigest_Integration_Security(t *testing.T) {
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
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "issue-3786-digest-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
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
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "digest-room", IdempotencyKey: "issue-3786-room"})
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
	// Alice muted the room, so the mention is held back whatever the time of day.
	exec(`INSERT INTO chat_preference(tenant_id,home_tenant_id,member_id,conversation_id,marker,value) VALUES('host','host','alice',$1,'notification','{"Muted":true,"MentionsOnly":false}')`, room.ID)
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,created_at) VALUES('bob-mentions','host',$1,'bob','host',1000,'secret plans for the offsite','[{"Kind":"PERSON_MENTION","TenantID":"host","ID":"alice","Display":"alice"}]',now()-interval '2 minutes')`, room.ID)
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at) VALUES('root','host',$1,'alice','host',1001,'my thread',now()-interval '3 hours')`, room.ID)
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,parent_id,created_at) VALUES('reply','host',$1,'bob','host',1002,'a reply','root',now()-interval '1 minute')`, room.ID)
	exec(`INSERT INTO chat_thread_follow(tenant_id,home_tenant_id,member_id,conversation_id,root_post_id,followed) VALUES('host','host','alice',$1,'root',true)`, room.ID)

	call := func(method, query, tenant, subject, body string) (int, ChatDigestReply, string) {
		t.Helper()
		admission, bearer := integrate1Admission(t, tenant, subject, time.Now)
		handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
		r := httptest.NewRequest(method, ChatDigestPath+query, strings.NewReader(body))
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var reply ChatDigestReply
		if method == http.MethodGet && w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, reply, w.Body.String()
	}
	// Off by default: nothing is due, nothing is read.
	if code, reply, _ := call(http.MethodGet, "?host=host", "host", "alice", ""); code != http.StatusOK || reply.Due || reply.Pref.Cadence != "off" {
		t.Fatalf("default: %d %+v", code, reply)
	}
	// The choice is saved. Turning it on starts from now, so nothing older than
	// the choice is held; the last digest is moved back to put the two posts in.
	if code, _, body := call(http.MethodPost, "", "host", "alice", `{"host":"host","action":"set","cadence":"daily","minute":0,"timezone":"UTC"}`); code != http.StatusOK {
		t.Fatalf("set: %d %s", code, body)
	}
	exec(`UPDATE chat_digest_pref SET last_digest_at=now()-interval '26 hours' WHERE tenant_id='host' AND member_id='alice'`)
	code, reply, body := call(http.MethodGet, "?host=host", "host", "alice", "")
	if code != http.StatusOK || !reply.Due || len(reply.Items) != 2 || reply.Pref.Cadence != "daily" {
		t.Fatalf("alice's digest: %d %s", code, body)
	}
	if strings.Contains(body, "secret plans") || strings.Contains(body, "a reply") {
		t.Fatal("the digest carried message text")
	}
	why := map[string]string{}
	for _, it := range reply.Items {
		why[it.PostID] = it.Why
		if it.ConversationName != "digest-room" || it.AuthorID != "bob" {
			t.Fatalf("item = %+v", it)
		}
	}
	if why["bob-mentions"] != "muted" || why["reply"] != "followed" {
		t.Fatalf("reasons = %v", why)
	}
	// Nobody else, and nobody in another company, has this digest.
	if code, other, _ := call(http.MethodGet, "?host=host", "host", "bob", ""); code == http.StatusOK && (other.Due || len(other.Items) != 0) {
		t.Fatalf("bob got alice's digest: %+v", other)
	}
	if code, other, _ := call(http.MethodGet, "?host=host", "other-company", "alice", ""); code == http.StatusOK && (other.Due || len(other.Items) != 0) {
		t.Fatalf("another company got alice's digest: %+v", other)
	}
	// Losing access removes the items without a trace.
	exec(`UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND conversation_id=$1 AND member_id='alice'`, room.ID)
	if code, gone, _ := call(http.MethodGet, "?host=host", "host", "alice", ""); code == http.StatusOK && (gone.Due || len(gone.Items) != 0) {
		t.Fatalf("a digest item survived the loss of access: %+v", gone)
	}
	exec(`UPDATE chat_membership SET state='active' WHERE tenant_id='host' AND conversation_id=$1 AND member_id='alice'`, room.ID)
	// Seeing it is remembered: it does not come back.
	seen, _ := json.Marshal(map[string]any{"host": "host", "action": "seen", "through": reply.Through})
	if code, _, body := call(http.MethodPost, "", "host", "alice", string(seen)); code != http.StatusOK {
		t.Fatalf("seen: %d %s", code, body)
	}
	if code, again, _ := call(http.MethodGet, "?host=host", "host", "alice", ""); code != http.StatusOK || again.Due || len(again.Items) != 0 {
		t.Fatalf("the digest came back: %d %+v", code, again)
	}
}
