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

// TestIssue3787_FollowUps_Integration_Security drives GET and POST
// /api/chat/follow-ups through the served handler assembly over a really
// composed chat runtime: a question the person asked and a message that
// mentions them are both listed, a choice survives a refresh, another person
// and another company see nothing of it, and losing membership removes the rows
// and refuses the write.
func TestIssue3787_FollowUps_Integration_Security(t *testing.T) {
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
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "issue-3787-followups-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
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
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "followups-room", IdempotencyKey: "issue-3787-room"})
	if err != nil {
		t.Fatal(err)
	}
	question, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Body: "Who has the key?", IdempotencyKey: "issue-3787-question"})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := chatDB.SQL.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE chat_post SET created_at=now()-interval '5 hours' WHERE tenant_id='host' AND id=$1`, question.ID)
	exec(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state) VALUES('host',$1,'host','bob','member','active')`, room.ID)
	exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,created_at) VALUES('bob-asks','host',$1,'bob','host',1000,'@alice can you confirm the swap?','[{"Kind":"PERSON_MENTION","TenantID":"host","ID":"alice","Display":"alice"}]',now()-interval '5 hours')`, room.ID)

	call := func(method, tenant, subject, body string) (int, FollowUpsReply) {
		t.Helper()
		admission, bearer := integrate1Admission(t, tenant, subject, time.Now)
		handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
		r := httptest.NewRequest(method, FollowUpsPath+"?host=host", strings.NewReader(body))
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var reply FollowUpsReply
		if method == http.MethodGet && w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, reply
	}
	code, reply := call(http.MethodGet, "host", "alice", "")
	if code != http.StatusOK || len(reply.Owed) != 1 || len(reply.Waiting) != 1 {
		t.Fatalf("alice's list: %d %+v", code, reply)
	}
	if o := reply.Owed[0]; o.PostID != "bob-asks" || o.AuthorID != "bob" || o.ConversationID != room.ID || o.Evidence != "" {
		t.Fatalf("owed = %+v", o)
	}
	if w := reply.Waiting[0]; w.PostID != question.ID || w.Body != "Who has the key?" || len(w.WaitingOn) != 0 {
		t.Fatalf("waiting = %+v", w)
	}
	// Another person and the same subject id in another company see none of it.
	if code, reply := call(http.MethodGet, "host", "bob", ""); code == http.StatusOK && len(reply.Owed) != 0 {
		t.Fatalf("bob saw alice's owed rows: %+v", reply)
	}
	if code, reply := call(http.MethodGet, "other-company", "alice", ""); code == http.StatusOK && len(reply.Owed)+len(reply.Waiting) != 0 {
		t.Fatalf("another company saw alice's rows: %+v", reply)
	}
	// A choice is stored and survives a refresh.
	resolve := `{"host":"host","conversation_id":"` + room.ID + `","post_id":"bob-asks","direction":"owed","action":"resolve"}`
	if code, _ := call(http.MethodPost, "host", "alice", resolve); code != http.StatusOK {
		t.Fatalf("resolve: %d", code)
	}
	for i := 0; i < 2; i++ {
		if code, reply := call(http.MethodGet, "host", "alice", ""); code != http.StatusOK || len(reply.Owed) != 0 || len(reply.Waiting) != 1 {
			t.Fatalf("after resolve (read %d): %d %+v", i, code, reply)
		}
	}
	// bob cannot set a choice on a message he cannot use as a waiting row, nor alice's.
	forged := `{"host":"host","conversation_id":"` + room.ID + `","post_id":"` + question.ID + `","direction":"waiting","action":"dismiss"}`
	if code, _ := call(http.MethodPost, "host", "bob", forged); code == http.StatusOK {
		t.Fatal("a person set a waiting row on somebody else's message")
	}
	if code, reply := call(http.MethodGet, "host", "alice", ""); code != http.StatusOK || len(reply.Waiting) != 1 {
		t.Fatalf("a forged choice hid alice's row: %d %+v", code, reply)
	}
	// Access loss: the rows disappear and a write is refused.
	exec(`UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND conversation_id=$1 AND member_id='alice'`, room.ID)
	if code, reply := call(http.MethodGet, "host", "alice", ""); code == http.StatusOK && len(reply.Owed)+len(reply.Waiting) != 0 {
		t.Fatalf("rows survived the loss of access: %d %+v", code, reply)
	}
	if code, _ := call(http.MethodPost, "host", "alice", strings.Replace(resolve, "resolve", "keep", 1)); code == http.StatusOK {
		t.Fatal("a removed member stored a choice")
	}
}
