package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentuxDemoCleanupFixture struct {
	t       *testing.T
	posts   []chat.Post
	authors map[string]*trust.Principal
	fail    string
	deleted []string
}

func (f *agentuxDemoCleanupFixture) DemoPostAuthor(_ context.Context, tenant, subject string) (*trust.Principal, error) {
	if tenant != localAgentDemoTenant {
		return nil, chat.ErrPermissionDenied
	}
	return f.authors[subject], nil
}

func (f *agentuxDemoCleanupFixture) ListPosts(ctx context.Context, r chat.ListPostsRequest) (chat.ListPostsResponse, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p.Subject() != r.Principal.SubjectID || r.TenantID != localAgentDemoTenant || r.ConversationID != localDevPersonaDemoConversationID(localAgentDemoTenant, "general") {
		f.t.Fatal("cleanup listing was not tenant- and principal-bound")
	}
	return chat.ListPostsResponse{Posts: f.posts}, nil
}

func (f *agentuxDemoCleanupFixture) DeletePost(ctx context.Context, r chat.DeletePostRequest) (chat.Post, error) {
	for i, p := range f.posts {
		if p.ID != r.PostID {
			continue
		}
		author, ok := trust.FromContext(ctx)
		if !ok || author.Subject() != p.AuthorID || r.Principal.SubjectID != p.AuthorID || r.ExpectedRevision != p.Revision || r.TenantID != p.TenantID || r.ConversationID != p.ConversationID {
			f.t.Fatal("cleanup borrowed an author or ignored the current revision")
		}
		if f.fail == p.ID {
			return chat.Post{}, chat.ErrUnavailable
		}
		p.Deleted, p.Revision = true, p.Revision+1
		f.posts[i] = p
		f.deleted = append(f.deleted, p.ID)
		return p, nil
	}
	return chat.Post{}, chat.ErrNotFound
}

func agentuxDemoCleanupSetup(t *testing.T) (*agentuxDemoCleanupFixture, *trust.Principal, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return agentuxDemoCleanupSetupAt(t, now)
}

func agentuxDemoCleanupSetupAt(t *testing.T, now time.Time) (*agentuxDemoCleanupFixture, *trust.Principal, time.Time) {
	t.Helper()
	admin, err := localAgentDemoPrincipal(now, localAgentDemoTenant, localAgentDemoAdmin, "org:ironridge-demo:people")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(localAgentDemoTenant), Subject: localAgentDemoPersonaID, SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "cleanup-fixture", CredentialDigest: "sha256:cleanup-fixture", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	post := func(id, author, parent, body string, sequence uint64) chat.Post {
		return chat.Post{ID: id, TenantID: localAgentDemoTenant, AuthorHomeTenantID: localAgentDemoTenant, ConversationID: localDevPersonaDemoConversationID(localAgentDemoTenant, "general"), AuthorID: author, ParentID: parent, Body: body, Sequence: sequence, Revision: 1, CreatedAt: now.Add(-time.Hour)}
	}
	f := &agentuxDemoCleanupFixture{t: t, authors: map[string]*trust.Principal{admin.Subject(): admin, agent.Subject(): agent}, posts: []chat.Post{
		post("q", admin.Subject(), "", "@Policy Helper verification question", 1),
		post("answer", agent.Subject(), "q", "Verification failure", 2),
		post("nested", agent.Subject(), "answer", "Verification retry", 3),
		post("real-person", "ir-other-worker", "q", "A person's response", 4),
		post("real-question", "ir-other-worker", "", "@Policy Helper my leave question", 5),
		post("real-answer", agent.Subject(), "real-question", "A helpful cited answer", 6),
		post("assistant", localAgentDemoAssistantPersonaID, "q", "Another agent's answer", 7),
		post("unlinked", agent.Subject(), "", "An unrelated announcement", 8),
	}}
	return f, admin, now
}

func TestAgentUXDemo_GeneralCleanup(t *testing.T) {
	f, admin, now := agentuxDemoCleanupSetup(t)
	c := AgentUXDemoGeneralCleanup{Chat: f, Authors: f}
	removed, err := c.Run(context.Background(), admin, now)
	if err != nil || !reflect.DeepEqual(removed, []string{"nested", "answer", "q"}) {
		t.Fatalf("cleanup receipt: %v %v", removed, err)
	}
	if removed, err := c.Run(context.Background(), admin, now); err != nil || len(removed) != 0 || len(f.deleted) != 3 {
		t.Fatalf("rerun duplicated a deletion: %v %v", removed, err)
	}
	for _, p := range f.posts[3:] {
		if p.Deleted {
			t.Fatalf("removed real activity: %s", p.ID)
		}
	}
}

func TestAgentUXDemo_GeneralCleanup_Security(t *testing.T) {
	f, admin, now := agentuxDemoCleanupSetup(t)
	delete(f.authors, localAgentDemoPersonaID)
	c := AgentUXDemoGeneralCleanup{Chat: f, Authors: f}
	if _, err := c.Run(context.Background(), admin, now); !errors.Is(err, chat.ErrPermissionDenied) || len(f.deleted) != 0 {
		t.Fatalf("missing author authority caused side effects: %v %v", f.deleted, err)
	}
	for _, mutate := range []func(*chat.Post){
		func(p *chat.Post) { p.TenantID = "another-tenant" },
		func(p *chat.Post) { p.AuthorHomeTenantID = "another-tenant" },
		func(p *chat.Post) { p.CreatedAt = time.Date(2026, 10, 1, 0, 59, 59, 0, time.UTC) },
		func(p *chat.Post) { p.CreatedAt = now.Add(time.Second) },
		func(p *chat.Post) { p.Body = "A genuine administrator message" },
	} {
		f, _, now := agentuxDemoCleanupSetup(t)
		mutate(&f.posts[0])
		if selected := agentuxDemoVerificationPosts(f.posts, now); len(selected) != 0 {
			t.Fatalf("unsafe selection: %+v", selected)
		}
	}
}

func TestAgentUXDemo_GeneralCleanup_Fault(t *testing.T) {
	f, admin, now := agentuxDemoCleanupSetup(t)
	f.fail = "answer"
	c := AgentUXDemoGeneralCleanup{Chat: f, Authors: f}
	removed, err := c.Run(context.Background(), admin, now)
	if !errors.Is(err, chat.ErrUnavailable) || !reflect.DeepEqual(removed, []string{"nested"}) {
		t.Fatalf("partial receipt: %v %v", removed, err)
	}
	f.fail = ""
	removed, err = c.Run(context.Background(), admin, now)
	if err != nil || !reflect.DeepEqual(removed, []string{"answer", "q"}) || !reflect.DeepEqual(f.deleted, []string{"nested", "answer", "q"}) {
		t.Fatalf("recovery duplicated a mutation: %v %v", removed, err)
	}
}

func TestAgentUXDemo_GeneralCleanup_Integration(t *testing.T) {
	f, admin, now := agentuxDemoCleanupSetupAt(t, time.Now().UTC().Truncate(time.Microsecond))
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	ctx := context.Background()
	store, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	adapter := chatstore.NewAdapter(store)
	service := chat.NewService(adapter, func() time.Time { return now })
	service.SetAuthority(servedPersonaChatAuthority{})
	conversation := localDevPersonaDemoConversationID(localAgentDemoTenant, "general")
	principal := chat.Principal{TenantID: localAgentDemoTenant, SubjectID: admin.Subject()}
	if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Name: "general", Kind: chat.PublicChannel}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMembership(trust.WithPrincipal(ctx, admin), chat.AddMembershipRequest{Principal: principal, Membership: chat.Membership{TenantID: localAgentDemoTenant, HomeTenantID: localAgentDemoTenant, ConversationID: conversation, SubjectID: localAgentDemoPersonaID}}); err != nil {
		t.Fatal(err)
	}
	// The historical machine author needs its actual chat scopes as well as
	// membership. Only authority fixtures use SQL; every post and deletion
	// below goes through the conversation service.
	if err := store.RunTenantTx(ctx, localAgentDemoTenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES($1,$2,$3,$4,1,$5,$6,'ACTIVE',$7,1,$8,$8)`, localAgentDemoTenant+":"+conversation+":"+localAgentDemoPersonaID, localAgentDemoTenant, conversation, localAgentDemoPersonaID, `{"app_id":"hcmnext.local.persona.policy_helper","version":1,"agent":{"display_name":"Policy Helper"}}`, []string{"chat.posts.read", "chat.posts.write"}, admin.Subject(), now.Add(-time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	question, err := service.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Body: "@pol verification", IdempotencyKey: "cleanup-question"})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := service.SendPost(trust.WithPrincipal(ctx, f.authors[localAgentDemoPersonaID]), chat.SendPostRequest{Principal: chat.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoPersonaID}, TenantID: localAgentDemoTenant, ConversationID: conversation, ParentID: question.ID, Body: "Verification failure", IdempotencyKey: "cleanup-answer"})
	if err != nil {
		t.Fatal(err)
	}
	c := AgentUXDemoGeneralCleanup{Chat: service, Authors: f}
	removed, err := c.Run(ctx, admin, now)
	if err != nil || !reflect.DeepEqual(removed, []string{reply.ID, question.ID}) {
		t.Fatalf("service cleanup: %v %v", removed, err)
	}
	for _, id := range removed {
		post, err := adapter.GetPost(ctx, localAgentDemoTenant, conversation, id)
		if err != nil || !post.Deleted || post.Revision != 2 {
			t.Fatalf("durable tombstone %s: %+v %v", id, post, err)
		}
	}
}
