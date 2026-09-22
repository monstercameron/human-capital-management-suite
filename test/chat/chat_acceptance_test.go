package chat_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_CHAT_051(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	host := principal("company-a", "alice")
	foreign := principal("company-b", "bob")
	outsider := principal("company-c", "mallory")

	c, err := f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{
		Principal: host, TenantID: host.TenantID, Kind: chatcore.PublicChannel, Name: "shared",
		Members: []chatcore.MemberRef{{TenantID: foreign.TenantID, SubjectID: foreign.SubjectID}},
	})
	if err != nil {
		t.Fatalf("create host conversation: %v", err)
	}
	if _, err := f.service.GetConversation(ctx, chatcore.GetConversationRequest{
		Principal: foreign, TenantID: host.TenantID, ConversationID: c.ID,
	}); err != nil {
		t.Fatalf("bilateral member read error = %v, want admitted host timeline read", err)
	}
	if _, err := f.service.SendPost(ctx, chatcore.SendPostRequest{
		Principal: foreign, TenantID: host.TenantID, ConversationID: c.ID,
		Body: "bilateral write", IdempotencyKey: "foreign-1",
	}); err != nil {
		t.Fatalf("bilateral member write error = %v, want admitted host timeline write: %v", err, err)
	}
	if _, err := f.service.GetConversation(ctx, chatcore.GetConversationRequest{
		Principal: outsider, TenantID: host.TenantID, ConversationID: c.ID,
	}); !errors.Is(err, chatcore.ErrPermissionDenied) && !errors.Is(err, chatcore.ErrUnauthenticated) {
		t.Fatalf("ungranted company read error = %v, want permission denied", err)
	}
	if _, err := f.store.ListPosts(ctx, host, host.TenantID, c.ID, 0, chatcore.Page{PageSize: 20}, chatcore.PostWindow{}); err != nil {
		t.Fatalf("host list after foreign attempts: %v", err)
	}
}

func TestTodo_CHAT_051_Conformance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := principal("company-a", "alice")
	c, err := f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{
		Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: "api-parity",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	first, err := f.service.SendPost(ctx, chatcore.SendPostRequest{
		Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: "one", IdempotencyKey: "same-key",
	})
	if err != nil {
		t.Fatalf("first post: %v", err)
	}
	replay, err := f.service.SendPost(ctx, chatcore.SendPostRequest{
		Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: "one", IdempotencyKey: "same-key",
	})
	if err != nil {
		t.Fatalf("replayed post: %v", err)
	}
	if replay.ID != first.ID || replay.Sequence != first.Sequence {
		t.Fatalf("replay = {%s,%d}, first = {%s,%d}; durable idempotency changed result", replay.ID, replay.Sequence, first.ID, first.Sequence)
	}
	posts, err := f.store.ListPosts(ctx, p, p.TenantID, c.ID, 0, chatcore.Page{PageSize: 20}, chatcore.PostWindow{})
	if err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if len(posts.Posts) != 1 {
		t.Fatalf("post count = %d, want one committed post after replay", len(posts.Posts))
	}
}

func TestTodo_CHAT_051_Security(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	host := principal("company-a", "alice")
	member := principal("company-b", "bob")
	c, err := f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{
		Principal: host, TenantID: host.TenantID, Kind: chatcore.PrivateChannel, Name: "revocation",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := f.service.AddMembership(ctx, chatcore.AddMembershipRequest{
		Principal:  host,
		Membership: chatcore.Membership{TenantID: host.TenantID, HomeTenantID: member.TenantID, ConversationID: c.ID, SubjectID: member.SubjectID, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
	}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := f.service.SendPost(ctx, chatcore.SendPostRequest{
		Principal: member, TenantID: host.TenantID, ConversationID: c.ID, Body: "before revoke", IdempotencyKey: "before-revoke",
	}); err != nil {
		t.Fatalf("member post before revoke: %v", err)
	}
	if _, err := f.service.RemoveMembership(ctx, chatcore.RemoveMembershipRequest{
		Principal: host, TenantID: host.TenantID, HomeTenantID: member.TenantID, ConversationID: c.ID, SubjectID: member.SubjectID, ExpectedRevision: 1,
	}); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if _, err := f.service.SendPost(ctx, chatcore.SendPostRequest{
		Principal: member, TenantID: host.TenantID, ConversationID: c.ID, Body: "after revoke", IdempotencyKey: "after-revoke",
	}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("post after revoke error = %v, want permission denied", err)
	}
	if _, err := f.service.ListPosts(ctx, chatcore.ListPostsRequest{Principal: member, TenantID: host.TenantID, ConversationID: c.ID, Page: chatcore.Page{PageSize: 20}}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("history after revoke error = %v, want permission denied", err)
	}
}

func TestTodo_CHAT_052(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := principal("company-load", "load-user")
	c, err := f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: "load"})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	const writers = 8
	const postsPerWriter = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers*postsPerWriter)
	for w := 0; w < writers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < postsPerWriter; i++ {
				_, e := f.service.SendPost(ctx, chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: fmt.Sprintf("%d/%d", w, i), IdempotencyKey: fmt.Sprintf("load-%d-%d", w, i)})
				if e != nil {
					errs <- e
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent send: %v", err)
	}
	posts, err := f.store.ListPosts(ctx, p, p.TenantID, c.ID, 0, chatcore.Page{PageSize: 200}, chatcore.PostWindow{})
	if err != nil {
		t.Fatalf("list concurrent posts: %v", err)
	}
	if len(posts.Posts) != writers*postsPerWriter {
		t.Fatalf("post count = %d, want %d", len(posts.Posts), writers*postsPerWriter)
	}
	for i, post := range posts.Posts {
		want := uint64(i + 1)
		if post.Sequence != want {
			t.Fatalf("post %d sequence = %d, want %d", i, post.Sequence, want)
		}
	}
}

func BenchmarkTodo_CHAT_052(b *testing.B) {
	b.Skip("the measured mixed-load profile is TestTodo_CHAT_052; pgtest intentionally exposes test schemas only")
}

func TestTodo_CHAT_052_Recovery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := principal("company-restart", "restart-user")
	c, err := f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: "restart"})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	first, err := f.service.SendPost(ctx, chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: "durable", IdempotencyKey: "restart-key"})
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	f.store.Close()
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: f.dsn})
	if err != nil {
		t.Fatalf("reopen chat store: %v", err)
	}
	store := chatstore.NewAdapter(raw)
	defer store.Close()
	restarted := chatcore.NewService(store, time.Now)
	restarted.SetAuthority(fixtureAuthority{store: store})
	replay, err := restarted.SendPost(ctx, chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: "durable", IdempotencyKey: "restart-key"})
	if err != nil {
		t.Fatalf("replay after restart: %v", err)
	}
	if replay.ID != first.ID || replay.Sequence != first.Sequence {
		t.Fatalf("restart replay = {%s,%d}, first = {%s,%d}", replay.ID, replay.Sequence, first.ID, first.Sequence)
	}
}

type fixture struct {
	service *chatcore.Service
	store   *chatstore.Adapter
	dsn     string
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	db := pgtest.NewEmpty(t.(*testing.T))
	applyChatMigrations(t, db)
	dsn := schemaDSN(t, db.URL, db.Schema)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open chat store: %v", err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	service := chatcore.NewService(store, time.Now)
	service.SetAuthority(fixtureAuthority{store: store})
	return fixture{service: service, store: store, dsn: dsn}
}

func applyChatMigrations(t testing.TB, db *pgtest.DB) {
	t.Helper()
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatalf("chat migrations filesystem: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatalf("chat migration provider: %v", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatalf("chat migrations: %v", err)
	}
}

func schemaDSN(t testing.TB, raw, schema string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func principal(tenant, subject string) chatcore.Principal {
	return chatcore.Principal{TenantID: tenant, SubjectID: subject, Roles: []string{"employee"}, Qualifications: []string{"active"}}
}

type fixtureAuthority struct{ store *chatstore.Adapter }

func (a fixtureAuthority) Authorize(ctx context.Context, p chatcore.Principal, c chatcore.Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	m, err := a.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	ownerInitial := err != nil && p.TenantID == c.TenantID && (c.OwnerID == "" || p.SubjectID == c.OwnerID)
	if ownerInitial {
		m = chatcore.Membership{TenantID: c.TenantID, HomeTenantID: p.TenantID, ConversationID: c.ID, SubjectID: p.SubjectID, Revision: 1}
	}
	if (!ownerInitial && err != nil) || m.HomeTenantID != p.TenantID || m.LeftAt != nil {
		return chatpolicy.Input{}, errors.New("current membership is not active")
	}
	revision := c.Revision
	if revision == 0 {
		revision = 1
	}
	membershipRevision := m.Revision
	if membershipRevision == 0 {
		membershipRevision = 1
	}
	input := chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, Roles: p.Roles, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: !c.Archived, Private: c.Kind != chatcore.PublicChannel, Revision: revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: membershipRevision},
		HasMembership: true,
		Now:           now,
	}
	if p.TenantID != c.TenantID {
		input.HasGrant = true
		input.Grant = chatpolicy.Grant{ID: "fixture-grant", ConversationID: c.ID, HostTenant: c.TenantID, ConsumerTenant: p.TenantID, Version: 1, Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true}
	}
	return input, nil
}
