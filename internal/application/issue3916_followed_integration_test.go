package application

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// TestIssue3916_FollowedThreads_Integration_Security drives GET
// /api/chat/followed-threads through the served handler assembly over a really
// composed chat runtime: the list is the follow the header wrote, another
// person and another company see nothing, and losing membership removes the
// thread without a trace.
func TestIssue3916_FollowedThreads_Integration_Security(t *testing.T) {
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
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "issue-3916-followed-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
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
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "followed-room", IdempotencyKey: "issue-3916-room"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Body: "Offsite plans", IdempotencyKey: "issue-3916-root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Body: "A reply", ParentID: root.ID, IdempotencyKey: "issue-3916-reply"}); err != nil {
		t.Fatal(err)
	}
	// The thread header writes the follow; the list must read the same row.
	if _, err := deployment.extensions.Recipients.PutFollow(aliceCtx, alice, "host", room.ID, chatrecipient.Follow{RootPostID: root.ID, Followed: true}, 1); err != nil {
		t.Fatal(err)
	}
	list := func(tenant, subject string) (int, FollowedThreadsReply) {
		t.Helper()
		admission, bearer := integrate1Admission(t, tenant, subject, time.Now)
		handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
		r := httptest.NewRequest(http.MethodGet, FollowedThreadsPath+"?host=host", nil)
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var reply FollowedThreadsReply
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, reply
	}
	code, reply := list("host", "alice")
	if code != http.StatusOK || len(reply.Threads) != 1 {
		t.Fatalf("alice's list: %d %+v", code, reply)
	}
	got := reply.Threads[0]
	if got.ConversationID != room.ID || got.RootPostID != root.ID || got.ReplyCount != 1 || got.RootBody != "Offsite plans" || got.Revision != 2 || got.LastActivity.IsZero() {
		t.Fatalf("thread = %+v", got)
	}
	// A person who never followed it, and the same subject id in another company,
	// see an empty list, never the thread or a count.
	for _, who := range [][2]string{{"host", "bob"}, {"other-company", "alice"}} {
		if code, reply := list(who[0], who[1]); code == http.StatusOK && len(reply.Threads) != 0 {
			t.Fatalf("%v saw alice's followed thread: %+v", who, reply)
		}
	}
	// The unfollow the list's button sends is the header's own write.
	if _, err := deployment.extensions.Recipients.PutFollow(aliceCtx, alice, "host", room.ID, chatrecipient.Follow{RootPostID: root.ID, Followed: false}, got.Revision); err != nil {
		t.Fatal(err)
	}
	if code, reply := list("host", "alice"); code != http.StatusOK || len(reply.Threads) != 0 {
		t.Fatalf("an unfollowed thread is still listed: %d %+v", code, reply)
	}
	if _, err := deployment.extensions.Recipients.PutFollow(aliceCtx, alice, "host", room.ID, chatrecipient.Follow{RootPostID: root.ID, Followed: true}, got.Revision+1); err != nil {
		t.Fatal(err)
	}
	if code, reply := list("host", "alice"); code != http.StatusOK || len(reply.Threads) != 1 {
		t.Fatalf("a refollowed thread is not listed: %d %+v", code, reply)
	}
	// Access loss: the membership row goes inactive and the thread disappears.
	if _, err := chatDB.SQL.ExecContext(ctx, `UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND conversation_id=$1 AND member_id='alice'`, room.ID); err != nil {
		t.Fatal(err)
	}
	if code, reply := list("host", "alice"); code == http.StatusOK && len(reply.Threads) != 0 {
		t.Fatalf("a thread survived the loss of access: %d %+v", code, reply)
	}
}
