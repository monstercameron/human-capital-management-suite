package application

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcontextstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// backgroundContextTx uses a real independently tenant-scoped PostgreSQL
// transaction for each checkpoint write/read, with the serving app role.
type backgroundContextTx struct {
	db *pgtest.DB
	t  *testing.T
}

func (r backgroundContextTx) RunTenantTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	conn := r.db.NewConn(r.t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TestTodo_AGENTP_008_Recovery_BackgroundContextRechecksRealChatAfterRestart(t *testing.T) {
	ctx := context.Background()
	tenant := "harborcare-demo"
	canonicalTenant := uuid.New()
	tenantUUID := func(ref string) uuid.UUID {
		if ref == tenant {
			return canonicalTenant
		}
		return uuid.Nil
	}
	agents := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agents.SQL); err != nil {
		t.Fatal(err)
	}
	agents.Exec(t, `INSERT INTO tenant(tenant_id) VALUES ($1)`, canonicalTenant)
	checkpoints, err := agentcontextstore.NewWithTenantUUID(backgroundContextTx{db: agents, t: t}, tenantUUID)
	if err != nil {
		t.Fatal(err)
	}
	chatDB := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chatDB.SQL, migrations, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(chatDB.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", chatDB.Schema)
	u.RawQuery = q.Encode()
	open := func() *chatstore.Store {
		t.Helper()
		s, e := chatstore.New(ctx, chatstore.Config{DSN: u.String()})
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	first := open()
	adapter := chatstore.NewAdapter(first)
	conversation := chat.Conversation{TenantID: tenant, ID: "room", Kind: chat.PrivateChannel, Name: "room", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{{TenantID: tenant, HomeTenantID: tenant, ConversationID: "room", SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}
	if _, err = adapter.CreateConversation(ctx, conversation, members, ""); err != nil {
		t.Fatal(err)
	}
	post, err := adapter.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: "room", IdempotencyKey: "invoke"}, chat.Post{AuthorID: "alice", Body: "Explain the policy"})
	if err != nil {
		t.Fatal(err)
	}
	checkpointSource := PersonaCheckpointThreadSnapshotSource{Source: adapter, Contexts: checkpoints}
	original, err := checkpointSource.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: "room", ThreadID: post.ID, InvokingPostID: post.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	restarted := open()
	t.Cleanup(restarted.Close)
	adapter = chatstore.NewAdapter(restarted)
	restored, err := agentcontextstore.NewWithTenantUUID(backgroundContextTx{db: agents, t: t}, tenantUUID)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &PersonaBackgroundRuntime{cfg: PersonaBackgroundRuntimeConfig{Contexts: restored, Threads: adapter}}
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: tenant, Ref: post.ID}, Principal: agentrun.PrincipalChain{InvokerID: "alice"}, Audience: agentrun.AudienceScope{ID: "room"}, Context: agentrun.ContextScope{ID: post.ID, SnapshotID: original.SnapshotID, Digest: original.Digest}}
	got, err := runtime.currentContext(ctx, request)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("restored source=%+v err=%v", got, err)
	}
	// A subsequent reply must not change the admitted context or replay itself.
	if _, err = adapter.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: "room", ParentID: post.ID, IdempotencyKey: "reply"}, chat.Post{AuthorID: "alice", Body: "new post outside the checkpoint"}); err != nil {
		t.Fatal(err)
	}
	got, err = runtime.currentContext(ctx, request)
	if err != nil || len(got.Posts) != 1 || got.Digest != original.Digest {
		t.Fatalf("new post polluted checkpoint: %+v %v", got, err)
	}
	if _, err = adapter.EditPost(ctx, chat.EditPostRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: "room", PostID: post.ID, Body: "changed question", ExpectedRevision: post.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.currentContext(ctx, request); !errors.Is(err, errPersonaRunCurrentAuthority) {
		t.Fatalf("edited original source retained: %v", err)
	}
	// Another fresh checkpoint proves membership withdrawal invalidates recovery.
	current, err := adapter.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: "room", ThreadID: post.ID, InvokingPostID: post.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Put(ctx, current); err != nil {
		t.Fatal(err)
	}
	request.Context.SnapshotID = current.SnapshotID
	request.Context.Digest = current.Digest
	chatDB.Exec(t, `UPDATE chat_membership SET state='left',left_at=CURRENT_TIMESTAMP WHERE tenant_id=$1 AND conversation_id='room' AND member_id='alice'`, tenant)
	if _, err = runtime.currentContext(ctx, request); !errors.Is(err, errPersonaRunCurrentAuthority) {
		t.Fatalf("revoked membership retained: %v", err)
	}
	if _, ok := trust.FromContext(ctx); ok {
		t.Fatal("background snapshot manufactured a human principal")
	}
}

func TestTodo_AGENTP_008_Security_BackgroundGrantTuplesDoNotUnionDimensions(t *testing.T) {
	row := agentgate.SkillGrant{Roles: []string{"manager"}, Population: "engineering", OrganizationScopes: []string{"org-a"}, Purposes: []string{"persona-mention"}}
	if !backgroundGrantTupleMatches(row, []string{"manager"}, []string{"engineering"}, "org-a", "persona-mention") {
		t.Fatal("exact current tuple denied")
	}
	for _, test := range []struct {
		roles, pops  []string
		org, purpose string
	}{{[]string{"employee"}, []string{"engineering"}, "org-a", "persona-mention"}, {[]string{"manager"}, []string{"finance"}, "org-a", "persona-mention"}, {[]string{"manager"}, []string{"engineering"}, "org-b", "persona-mention"}, {[]string{"manager"}, []string{"engineering"}, "org-a", "export"}} {
		if backgroundGrantTupleMatches(row, test.roles, test.pops, test.org, test.purpose) {
			t.Fatalf("broadened tuple accepted: %+v", test)
		}
	}
	a := agentpersona.Audience{Roles: row.Roles, Populations: []string{row.Population}, OrganizationScopes: row.OrganizationScopes}
	if !backgroundAudienceMatches(a, []string{"manager"}, []string{"engineering"}, "org-a") || backgroundAudienceMatches(a, []string{"manager"}, []string{"finance"}, "org-a") {
		t.Fatal("profile audience mismatch")
	}
	if _, err := NewPersonaBackgroundRuntime(PersonaBackgroundRuntimeConfig{}); !errors.Is(err, errPersonaRunCurrentAuthority) {
		t.Fatal(err)
	}
	var runtime *PersonaBackgroundRuntime
	if _, err := runtime.VerifyAdmission(context.Background(), agentrun.Request{}); !errors.Is(err, errPersonaRunCurrentAuthority) {
		t.Fatal(err)
	}
	if _, err := runtime.ForTenant(context.Background(), values.TenantId("a").String()); !errors.Is(err, errPersonaRunCurrentAuthority) {
		t.Fatal(err)
	}
}
