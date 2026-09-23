package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

type streamIntegrationAuthority struct{ store *chatstore.Adapter }

func (a streamIntegrationAuthority) Authorize(ctx context.Context, p chatcore.Principal, c chatcore.Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	m, err := a.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	if err != nil || m.LeftAt != nil {
		return chatpolicy.Input{}, chatcore.ErrPermissionDenied
	}
	in := chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: true, Private: c.Kind != chatcore.PublicChannel, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: m.Revision},
		HasMembership: true, Now: now,
	}
	if p.TenantID != c.TenantID {
		in.HasGrant = true
		in.Grant = chatpolicy.Grant{ID: "integration-grant", ConversationID: c.ID, HostTenant: c.TenantID, ConsumerTenant: p.TenantID, Version: 1, Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true}
	}
	return in, nil
}

func streamIntegrationStore(t *testing.T) *chatstore.Adapter {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	return chatstore.NewAdapter(raw)
}

// The cursor delivered by the composed service must cover exactly the events
// delivered to the client, even when the durable outbox has other conversations.
func TestTodo_CHAT_018_Integration(t *testing.T) {
	store := streamIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	principal := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	seed := func(id string) {
		t.Helper()
		_, err := store.CreateConversation(ctx, chatcore.Conversation{ID: id, TenantID: "host", Kind: chatcore.PublicChannel, Name: id, OwnerID: "alice", Revision: 1}, []chatcore.Membership{{TenantID: "host", HomeTenantID: "host", ConversationID: id, SubjectID: "alice", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory}}, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("watched")
	seed("other")
	var first chatcore.Post
	for i := 0; i < 40; i++ {
		conversation := "watched"
		if i%5 == 0 {
			conversation = "other"
		}
		post, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: principal, TenantID: "host", ConversationID: conversation, IdempotencyKey: fmt.Sprintf("seed-%d", i)}, chatcore.Post{AuthorID: "alice", Body: fmt.Sprintf("post-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			first = post
		}
	}
	if _, err := store.EditPost(ctx, chatcore.EditPostRequest{Principal: principal, TenantID: "host", ConversationID: "watched", PostID: first.ID, Body: "edited post", ExpectedRevision: first.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutReaction(ctx, chatcore.Reaction{TenantID: "host", HomeTenantID: "host", ConversationID: "watched", PostID: first.ID, SubjectID: "alice", Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutMembership(ctx, principal, chatcore.Membership{TenantID: "host", HomeTenantID: "foreign", ConversationID: "watched", SubjectID: "bob", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory}); err != nil {
		t.Fatal(err)
	}
	service := chatcore.NewService(store, time.Now)
	service.SetAuthority(streamIntegrationAuthority{store: store})
	reader := chatServiceReader{service: service, membership: store, events: store}
	runtime, err := NewChatStreamRuntime(ChatStreamRuntimeConfig{CursorKey: "integration-key", Reader: reader, Authorizer: chatServiceStreamAuthorizer{service: service, membership: store}, PollInterval: 10 * time.Millisecond, PageLimit: 8, QueueSize: 64, ReplayLimit: 8, CursorTTL: time.Minute, Budgets: defaultChatAdmissionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	composed := &streamingChatService{ConversationService: service, runtime: runtime, membership: store}
	watchCtx, stop := context.WithCancel(ctx)
	defer stop()
	events, err := composed.WatchConversation(watchCtx, chatcore.WatchConversationRequest{Principal: principal, TenantID: "host", ConversationID: "watched"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	kinds := map[chatcore.ConversationEventKind]bool{}
	var cursor string
	for len(seen) < 32 || len(kinds) < 3 {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("stream closed after %d watched posts", len(seen))
			}
			if event.Event.Kind == chatcore.MembershipChanged && (event.Event.Membership == nil || event.Event.Membership.SubjectID != "bob") {
				continue
			}
			if event.Event.Kind == chatcore.PostEdited || event.Event.Kind == chatcore.ReactionChanged || event.Event.Kind == chatcore.MembershipChanged {
				kinds[event.Event.Kind] = true
				cursor = event.ResumeCursor
				continue
			}
			if event.Event.Kind != chatcore.PostCreated {
				continue
			}
			if event.Event.Post == nil || seen[event.Event.Post.Body] {
				t.Fatalf("duplicate or empty post: %+v", event.Event)
			}
			seen[event.Event.Post.Body] = true
			cursor = event.ResumeCursor
		case <-ctx.Done():
			t.Fatalf("stream replayed %d/32 watched posts: %v", len(seen), ctx.Err())
		}
	}
	stop()
	for i := 0; i < 40; i++ {
		body := fmt.Sprintf("post-%d", i)
		if i%5 != 0 && !seen[body] {
			t.Fatalf("missing %s", body)
		}
	}
	if cursor == "" {
		t.Fatal("missing signed resume cursor")
	}
	resumeCtx, resumeStop := context.WithTimeout(context.Background(), 2*time.Second)
	defer resumeStop()
	resume, err := composed.WatchConversation(resumeCtx, chatcore.WatchConversationRequest{Principal: principal, TenantID: "host", ConversationID: "watched", ResumeCursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-resume:
		t.Fatalf("duplicate event after consumed cursor: %+v", event)
	case <-time.After(400 * time.Millisecond):
	}
	foreignCtx, foreignStop := context.WithCancel(ctx)
	defer foreignStop()
	foreign, err := composed.WatchConversation(foreignCtx, chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "foreign", SubjectID: "bob"}, TenantID: "host", ConversationID: "watched"})
	if err != nil {
		t.Fatalf("admitted foreign home watch: %v", err)
	}
	var foreignCursor string
	select {
	case event := <-foreign:
		if event.Event.Sequence == 0 || event.ResumeCursor == "" {
			t.Fatalf("foreign event missing durable sequence or signed cursor: %+v", event)
		}
		foreignCursor = event.ResumeCursor
	case <-ctx.Done():
		t.Fatalf("foreign home watch: %v", ctx.Err())
	}
	if _, err := composed.WatchConversation(ctx, chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "foreign", SubjectID: "bob"}, TenantID: "host", ConversationID: "watched", ResumeCursor: cursor}); !errors.Is(err, chatstream.ErrInvalidCursor) {
		t.Fatalf("foreign member reused host cursor: %v", err)
	}
	if _, err := composed.WatchConversation(ctx, chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "foreign", SubjectID: "alice"}, TenantID: "host", ConversationID: "watched", ResumeCursor: cursor}); !errors.Is(err, chatcore.ErrPermissionDenied) && !errors.Is(err, chatstream.ErrInvalidCursor) {
		t.Fatalf("foreign home reused cursor: %v", err)
	}
	member, err := store.GetMembership(ctx, "host", "watched", "foreign", "bob")
	if err != nil {
		t.Fatal(err)
	}
	queued, lease, err := runtime.Watch(ctx, chatstream.WatchRequest{TenantID: "host", HomeTenantID: "foreign", SubjectID: "bob", ConversationID: "watched", MembershipEpoch: member.Revision})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	defer queued.Close()
	if _, err := store.RemoveMembership(ctx, principal, "host", "watched", "foreign", "bob", member.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := queued.Next(ctx); !errors.Is(err, chatstream.ErrRevoked) {
		t.Fatalf("queued event after revocation: %v", err)
	}
	rejoined, err := store.PutMembership(ctx, principal, chatcore.Membership{TenantID: "host", HomeTenantID: "foreign", ConversationID: "watched", SubjectID: "bob", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := composed.WatchConversation(ctx, chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "foreign", SubjectID: "bob"}, TenantID: "host", ConversationID: "watched", ResumeCursor: foreignCursor}); !errors.Is(err, chatstream.ErrInvalidCursor) {
		t.Fatalf("old epoch cursor accepted after rejoin at revision %d: %v", rejoined.Revision, err)
	}
	freshCtx, freshStop := context.WithCancel(ctx)
	defer freshStop()
	if _, err := composed.WatchConversation(freshCtx, chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "foreign", SubjectID: "bob"}, TenantID: "host", ConversationID: "watched"}); err != nil {
		t.Fatalf("fresh watch after rejoin: %v", err)
	}
	for i := 0; i < 105; i++ {
		if _, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: principal, TenantID: "host", ConversationID: "other", IdempotencyKey: fmt.Sprintf("prejoin-%d", i)}, chatcore.Post{AuthorID: "alice", Body: "before join"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.PutMembership(ctx, principal, chatcore.Membership{TenantID: "host", HomeTenantID: "foreign", ConversationID: "other", SubjectID: "late", Role: chatcore.Member, HistoryVisibility: chatcore.FromJoin}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: principal, TenantID: "host", ConversationID: "other", IdempotencyKey: "after-join"}, chatcore.Post{AuthorID: "alice", Body: "after join"}); err != nil {
		t.Fatal(err)
	}
	filtered, err := reader.Read(ctx, chatstream.ReadRequest{TenantID: "host", HomeTenantID: "foreign", SubjectID: "late", ConversationID: "other", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Events) != 0 || filtered.NextSequence == 0 || filtered.Complete {
		t.Fatalf("first filtered page = %+v, want advanced durable watermark and incomplete page", filtered)
	}
	lateCtx, lateStop := context.WithCancel(ctx)
	defer lateStop()
	late, err := composed.WatchConversation(lateCtx, chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "foreign", SubjectID: "late"}, TenantID: "host", ConversationID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event, ok := <-late:
			if !ok {
				t.Fatal("late join stream closed before new post")
			}
			if event.Event.Post == nil {
				continue
			}
			if event.Event.Post.Body != "after join" {
				t.Fatalf("prejoin history leaked: %+v", event.Event.Post)
			}
			return
		case <-ctx.Done():
			t.Fatalf("late join missed new post after >100 filtered events: %v", ctx.Err())
		}
	}
}
