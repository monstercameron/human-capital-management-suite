package chatstore

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func adapterDB(t *testing.T) *Adapter {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	store, err := New(context.Background(), Config{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return NewAdapter(store)
}

func TestTodo_CHAT_032_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "references", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	ref := chat.Reference{Kind: chat.PersonMention, TenantID: c.TenantID, ID: "bob", Display: "Bob"}
	source := &chat.SourceAttribution{TenantID: "source-tenant", ConversationID: "source-conversation", PostID: "source-post", PostRevision: 3}
	req := chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "meta", References: []chat.Reference{ref}, SourceAttribution: source, ParentID: "parent"}
	p, err := s.SendPost(ctx, req, chat.Post{AuthorID: "alice", Body: "hello Bob", References: []chat.Reference{ref}, SourceAttribution: source, ParentID: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetPost(ctx, c.TenantID, c.ID, p.ID)
	if err != nil || loaded.ParentID != "parent" || len(loaded.References) != 1 || loaded.References[0].ID != "bob" || loaded.SourceAttribution == nil || loaded.SourceAttribution.PostRevision != 3 {
		t.Fatalf("loaded metadata=%+v %v", loaded, err)
	}
	listed, err := s.ListPosts(ctx, req.Principal, c.TenantID, c.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(listed.Posts) != 1 || listed.Posts[0].SourceAttribution == nil {
		t.Fatalf("listed metadata=%+v %v", listed, err)
	}
	search, err := s.Search(ctx, chat.SearchRequest{Principal: req.Principal, TenantID: c.TenantID, Query: "hello"})
	if err != nil || len(search.Results) != 1 || len(search.Results[0].Post.References) != 1 {
		t.Fatalf("search metadata=%+v %v", search, err)
	}
	if _, err = s.SendPost(ctx, req, chat.Post{AuthorID: "alice", Body: "different", References: []chat.Reference{ref}, SourceAttribution: source, ParentID: "parent"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay=%v", err)
	}
	edit, err := s.EditPost(ctx, chat.EditPostRequest{Principal: req.Principal, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "edited", ExpectedRevision: 1})
	if err != nil || edit.SourceAttribution == nil || edit.SourceAttribution.PostID != "source-post" {
		t.Fatalf("edited metadata=%+v %v", edit, err)
	}
}

func TestTodo_CHAT_004_HomeIdentity(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "host", Kind: chat.PrivateChannel, Name: "cross", OwnerID: "sam", Revision: 1}
	local := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: "host", SubjectID: "sam", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{local}, ""); err != nil {
		t.Fatal(err)
	}
	foreign := local
	foreign.HomeTenantID = "foreign"
	foreign.Role = chat.Member
	if _, err := s.PutMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, foreign); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "host", SubjectID: "sam"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "local"}, chat.Post{AuthorID: "sam", Body: "mine"})
	if err != nil || p.AuthorHomeTenantID != "host" {
		t.Fatalf("local post=%+v %v", p, err)
	}
	if _, err = s.EditPost(ctx, chat.EditPostRequest{Principal: chat.Principal{TenantID: "foreign", SubjectID: "sam"}, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "forged", ExpectedRevision: 1}); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("foreign edited local author=%v", err)
	}
	localState := chat.NotificationPreferences{TenantID: c.TenantID, HomeTenantID: "host", ConversationID: c.ID, SubjectID: "sam", Muted: true}
	if _, err = s.PutPreferences(ctx, localState, 1); err != nil {
		t.Fatal(err)
	}
	foreignState := chat.NotificationPreferences{TenantID: c.TenantID, HomeTenantID: "foreign", ConversationID: c.ID, SubjectID: "sam", MentionsOnly: true}
	if _, err = s.PutPreferences(ctx, foreignState, 1); err != nil {
		t.Fatal(err)
	}
	gotLocal, err := s.GetPreferences(ctx, c.TenantID, c.ID, "host", "sam")
	if err != nil || !gotLocal.Muted || gotLocal.MentionsOnly {
		t.Fatalf("local prefs=%+v %v", gotLocal, err)
	}
	gotForeign, err := s.GetPreferences(ctx, c.TenantID, c.ID, "foreign", "sam")
	if err != nil || gotForeign.Muted || !gotForeign.MentionsOnly {
		t.Fatalf("foreign prefs=%+v %v", gotForeign, err)
	}
}

func TestTodo_CHAT_007_ShardFenceIntegration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "fenced", TenantID: "host", Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	lease := chatrouting.WriteLease{Route: chatrouting.Route{ConversationID: c.ID, HostTenantID: c.TenantID, ShardID: "chat-local", Epoch: 1, State: chatrouting.StatePending}}
	if _, err := s.CreateConversation(chatrouting.WithWriteLease(ctx, lease), c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	req := chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "good"}
	lease.Route.State = chatrouting.StateActive
	if _, err := s.SendPost(chatrouting.WithWriteLease(ctx, lease), req, chat.Post{AuthorID: "alice", Body: "allowed"}); err != nil {
		t.Fatal(err)
	}
	lease.Route.ShardID = "other-shard"
	req.IdempotencyKey = "wrong-shard"
	if _, err := s.SendPost(chatrouting.WithWriteLease(ctx, lease), req, chat.Post{AuthorID: "alice", Body: "blocked"}); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("wrong shard write=%v", err)
	}
	lease.Route.ShardID = "chat-local"
	lease.Route.Epoch = 2
	req.IdempotencyKey = "wrong-epoch"
	if _, err := s.SendPost(chatrouting.WithWriteLease(ctx, lease), req, chat.Post{AuthorID: "alice", Body: "blocked"}); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("stale epoch write=%v", err)
	}
}

func TestTodo_CHAT_010_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "planning", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}
	got, err := s.CreateConversation(ctx, c, members, "key")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != c.ID {
		t.Fatalf("conversation=%+v", got)
	}
	got, err = s.CreateConversation(ctx, chat.Conversation{ID: "new-id", TenantID: c.TenantID, Kind: c.Kind, Name: c.Name, OwnerID: c.OwnerID, Revision: 1}, []chat.Membership{{ConversationID: "new-id", TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, "key")
	if err != nil || got.ID != c.ID {
		t.Fatalf("replay=%+v %v", got, err)
	}
	if _, err = s.CreateConversation(ctx, chat.Conversation{ID: "other", TenantID: c.TenantID, Kind: c.Kind, Name: "changed", OwnerID: c.OwnerID, Revision: 1}, []chat.Membership{{ConversationID: "other", TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, "key"); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("changed replay error=%v", err)
	}
	m, err := s.GetMembership(ctx, c.TenantID, c.ID, c.TenantID, "alice")
	if err != nil || m.HomeTenantID != c.TenantID || m.Revision != 1 {
		t.Fatalf("membership=%+v %v", m, err)
	}
	if _, err = s.UpdateConversation(ctx, c, 2); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale update=%v", err)
	}
	foreign := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: "tenant-b", SubjectID: "alice", Role: chat.Member, HistoryVisibility: chat.FromJoin}
	if _, err = s.PutMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, foreign); err != nil {
		t.Fatalf("same subject from foreign home: %v", err)
	}
	gotForeign, err := s.GetMembership(ctx, c.TenantID, c.ID, "tenant-b", "alice")
	if err != nil || gotForeign.HomeTenantID != "tenant-b" {
		t.Fatalf("foreign membership=%+v %v", gotForeign, err)
	}
	gotLocal, err := s.GetMembership(ctx, c.TenantID, c.ID, c.TenantID, "alice")
	if err != nil || gotLocal.HomeTenantID != c.TenantID {
		t.Fatalf("local membership=%+v %v", gotLocal, err)
	}
}

func TestTodo_CHAT_015_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "secret", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}
	if _, err := s.CreateConversation(ctx, c, members, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p1"}, chat.Post{AuthorID: "alice", Body: "unique blue secret"})
	if err != nil {
		t.Fatal(err)
	}
	search := chat.SearchRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, Query: "blue"}
	results, err := s.Search(ctx, search)
	if err != nil || len(results.Results) != 1 || results.Results[0].Post.ID != p.ID {
		t.Fatalf("member search=%+v %v", results, err)
	}
	search.Principal.SubjectID = "bob"
	results, err = s.Search(ctx, search)
	if err != nil || len(results.Results) != 0 {
		t.Fatalf("nonmember search=%+v %v", results, err)
	}
	search.Principal.SubjectID = "alice"
	search.Query = "absent"
	results, err = s.Search(ctx, search)
	if err != nil || len(results.Results) != 0 {
		t.Fatalf("query ignored=%+v %v", results, err)
	}
	edit := chat.EditPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, Body: "updated", ExpectedRevision: 1}
	got, err := s.EditPost(ctx, edit)
	if err != nil || got.Revision != 2 || got.Body != "updated" {
		t.Fatalf("edit=%+v %v", got, err)
	}
	if _, err = s.EditPost(ctx, edit); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale edit=%v", err)
	}
	wrong, err := s.GetPost(ctx, c.TenantID, "other", p.ID)
	if err == nil || wrong.ID != "" {
		t.Fatalf("cross conversation=%+v %v", wrong, err)
	}
}

func TestTodo_CHAT_022_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "prefs", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	initialPrefs, err := s.GetPreferences(ctx, c.TenantID, c.ID, c.TenantID, "alice")
	if err != nil || initialPrefs.Revision != 1 {
		t.Fatalf("initial prefs=%+v %v", initialPrefs, err)
	}
	initialRead, err := s.GetReadState(ctx, c.TenantID, c.ID, c.TenantID, "alice")
	if err != nil || initialRead.Revision != 1 {
		t.Fatalf("initial read=%+v %v", initialRead, err)
	}
	x, err := s.PutPreferences(ctx, chat.NotificationPreferences{TenantID: c.TenantID, ConversationID: c.ID, SubjectID: "alice", Muted: true}, 1)
	if err != nil || x.Revision != 2 {
		t.Fatalf("prefs=%+v %v", x, err)
	}
	x, err = s.GetPreferences(ctx, c.TenantID, c.ID, c.TenantID, "alice")
	if err != nil || !x.Muted || x.Revision != 2 {
		t.Fatalf("loaded prefs=%+v %v", x, err)
	}
	if _, err = s.PutPreferences(ctx, x, 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale prefs=%v", err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "read-position"}, chat.Post{AuthorID: "alice", Body: "visible"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.PutReadState(ctx, chat.ReadState{TenantID: c.TenantID, ConversationID: c.ID, SubjectID: "alice", LastReadSequence: p.Sequence}, 1)
	if err != nil || r.Revision != 2 || r.LastReadSequence != p.Sequence {
		t.Fatalf("read=%+v %v", r, err)
	}
	r, err = s.GetReadState(ctx, c.TenantID, c.ID, c.TenantID, "alice")
	if err != nil || r.LastReadSequence != p.Sequence {
		t.Fatalf("loaded read=%+v %v", r, err)
	}
	if _, err = s.PutReadState(ctx, r, 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale read=%v", err)
	}
}

func TestTodo_CHAT_023_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "pins", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p1"}, chat.Post{AuthorID: "alice", Body: "pin me"})
	if err != nil {
		t.Fatal(err)
	}
	pin, err := s.PutPin(ctx, chat.Pin{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, PinnedBy: "alice"})
	if err != nil || pin.Revision != 1 {
		t.Fatalf("pin=%+v %v", pin, err)
	}
	pins, err := s.ListPins(ctx, c.TenantID, c.ID)
	if err != nil || len(pins) != 1 || pins[0].PostID != p.ID {
		t.Fatalf("pins=%+v %v", pins, err)
	}
	if err = s.RemovePin(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, c.TenantID, c.ID, p.ID, c.TenantID, 2); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale pin=%v", err)
	}
	if err = s.RemovePin(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, c.TenantID, c.ID, p.ID, c.TenantID, 1); err != nil {
		t.Fatal(err)
	}
	pins, err = s.ListPins(ctx, c.TenantID, c.ID)
	if err != nil || len(pins) != 0 {
		t.Fatalf("pins after remove=%+v %v", pins, err)
	}
	reaction, err := s.PutReaction(ctx, chat.Reaction{TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, SubjectID: "alice", Emoji: "+1"})
	if err != nil || reaction.CreatedAt.IsZero() {
		t.Fatalf("reaction=%+v %v", reaction, err)
	}
	if err = s.RemoveReaction(ctx, c.TenantID, c.ID, p.ID, c.TenantID, "bob", "+1"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("nonmember reaction removal=%v", err)
	}
	var count int
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM chat_reaction WHERE tenant_id=$1 AND post_id=$2`, c.TenantID, p.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("other principal removed reaction: %d", count)
	}
	if err = s.RemoveReaction(ctx, c.TenantID, c.ID, p.ID, c.TenantID, "alice", "+1"); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CHAT_012_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "lifecycle", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	convs, err := s.ListConversations(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, chat.Page{PageSize: 10}, chat.ConversationScope{})
	if err != nil || len(convs.Conversations) != 1 {
		t.Fatalf("conversations=%+v %v", convs, err)
	}
	members, err := s.ListMemberships(ctx, c.TenantID, c.ID, chat.Page{PageSize: 10})
	if err != nil || len(members.Memberships) != 1 {
		t.Fatalf("members=%+v %v", members, err)
	}
	oldPost, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "before-join"}, chat.Post{AuthorID: "alice", Body: "old"})
	if err != nil {
		t.Fatal(err)
	}
	bob := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FromJoin}
	bob, err = s.PutMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, bob)
	if err != nil || bob.Revision != 1 {
		t.Fatalf("add=%+v %v", bob, err)
	}
	firstMembers, err := s.ListMemberships(ctx, c.TenantID, c.ID, chat.Page{PageSize: 1})
	if err != nil || len(firstMembers.Memberships) != 1 || firstMembers.NextCursor == "" {
		t.Fatalf("first member page=%+v %v", firstMembers, err)
	}
	secondMembers, err := s.ListMemberships(ctx, c.TenantID, c.ID, chat.Page{PageSize: 1, Cursor: firstMembers.NextCursor})
	if err != nil || len(secondMembers.Memberships) != 1 || secondMembers.Memberships[0].SubjectID == firstMembers.Memberships[0].SubjectID {
		t.Fatalf("second member page=%+v %v", secondMembers, err)
	}
	newPost, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "after-join"}, chat.Post{AuthorID: "alice", Body: "new"})
	if err != nil {
		t.Fatal(err)
	}
	posts, err := s.ListPosts(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "bob"}, c.TenantID, c.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 1 || posts.Posts[0].ID != newPost.ID {
		t.Fatalf("join history=%+v old=%s %v", posts, oldPost.ID, err)
	}
	firstPosts, err := s.ListPosts(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, c.ID, 0, chat.Page{PageSize: 1}, chat.PostWindow{})
	if err != nil || len(firstPosts.Posts) != 1 || firstPosts.NextCursor == "" {
		t.Fatalf("first post page=%+v %v", firstPosts, err)
	}
	secondPosts, err := s.ListPosts(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c.TenantID, c.ID, 0, chat.Page{PageSize: 1, Cursor: firstPosts.NextCursor}, chat.PostWindow{})
	if err != nil || len(secondPosts.Posts) != 1 || secondPosts.Posts[0].ID != newPost.ID {
		t.Fatalf("second post page=%+v %v", secondPosts, err)
	}
	removed, err := s.RemoveMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, c.TenantID, c.ID, c.TenantID, "bob", 1)
	if err != nil || removed.LeftAt == nil {
		t.Fatalf("remove=%+v %v", removed, err)
	}
	if _, err = s.RemoveMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, c.TenantID, c.ID, c.TenantID, "bob", 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale remove=%v", err)
	}
	convs, err = s.ListConversations(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "bob"}, c.TenantID, chat.Page{PageSize: 10}, chat.ConversationScope{})
	if err != nil || len(convs.Conversations) != 0 {
		t.Fatalf("removed list=%+v %v", convs, err)
	}
	rejoined, err := s.PutMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, bob)
	if err != nil || rejoined.JoinedAt == nil || !rejoined.JoinedAt.After(*bob.JoinedAt) {
		t.Fatalf("rejoin timestamp=%+v prior=%+v %v", rejoined, bob, err)
	}
	posts, err = s.ListPosts(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "bob"}, c.TenantID, c.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 0 {
		t.Fatalf("rejoin leaked old history=%+v %v", posts, err)
	}
	c.Name = "renamed"
	updated, err := s.UpdateConversation(ctx, c, 1)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("update=%+v %v", updated, err)
	}
	loaded, err := s.GetConversation(ctx, c.TenantID, c.ID)
	if err != nil || loaded.Name != "renamed" {
		t.Fatalf("loaded=%+v %v", loaded, err)
	}
	if err = s.execTenant(ctx, c.TenantID, `ALTER TABLE chat_outbox ADD CONSTRAINT reject_next_update CHECK (event_type <> 'conversation.updated') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	c.Name = "must-roll-back"
	if _, err = s.UpdateConversation(ctx, c, 2); err == nil {
		t.Fatal("update committed without its audit event")
	}
	loaded, err = s.GetConversation(ctx, c.TenantID, c.ID)
	if err != nil || loaded.Name != "renamed" || loaded.Revision != 2 {
		t.Fatalf("audit failure did not roll back update: %+v %v", loaded, err)
	}
}

func TestTodo_CHAT_025_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "watch", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	watch, err := s.Watch(ctx, chat.WatchConversationRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p1"}, chat.Post{AuthorID: "alice", Body: "watch me"})
	if err != nil {
		t.Fatal(err)
	}
	postSeen := false
	firstDeadline := time.After(3 * time.Second)
	for !postSeen {
		select {
		case event, open := <-watch:
			if !open {
				t.Fatal("watch closed before post")
			}
			if event.Event.Kind == chat.PostCreated {
				if event.Event.Post == nil || event.Event.Post.ID != p.ID || event.ResumeCursor == "" {
					t.Fatalf("event=%+v", event)
				}
				postSeen = true
			}
		case <-firstDeadline:
			t.Fatal("watch did not replay durable post")
		}
	}
	bob := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FromJoin}
	if _, err = s.PutMembership(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: c.OwnerID}, bob); err != nil {
		t.Fatal(err)
	}
	bobWatch, bobErr := s.Watch(ctx, chat.WatchConversationRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "bob"}, TenantID: c.TenantID, ConversationID: c.ID})
	if bobErr != nil {
		t.Fatal(bobErr)
	}
	quiet := time.After(500 * time.Millisecond)
	quietDone := false
	for !quietDone {
		select {
		case event, open := <-bobWatch:
			if !open {
				t.Fatal("watch closed unexpectedly")
			}
			if event.Event.Post != nil && event.Event.Post.ID == p.ID {
				t.Fatalf("pre-join post leaked: %+v", event)
			}
		case <-quiet:
			quietDone = true
		}
	}
	newPost, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "p2"}, chat.Post{AuthorID: "alice", Body: "new message"})
	if err != nil {
		t.Fatal(err)
	}
	newSeen := false
	newDeadline := time.After(3 * time.Second)
	for !newSeen {
		select {
		case event, open := <-bobWatch:
			if !open {
				t.Fatal("watch closed before new post")
			}
			if event.Event.Post != nil {
				if event.Event.Post.ID != newPost.ID {
					t.Fatalf("unexpected post=%+v", event)
				}
				newSeen = true
			}
		case <-newDeadline:
			t.Fatal("new event unavailable")
		}
	}
	other := chat.Conversation{ID: "c2", TenantID: c.TenantID, Kind: chat.PrivateChannel, Name: "other", OwnerID: "alice", Revision: 1}
	otherMember := m
	otherMember.ConversationID = other.ID
	if _, err = s.CreateConversation(ctx, other, []chat.Membership{otherMember}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: other.ID, IdempotencyKey: "other-post"}, chat.Post{AuthorID: "alice", Body: "other channel"}); err != nil {
		t.Fatal(err)
	}
	edited, err := s.EditPost(ctx, chat.EditPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, PostID: newPost.ID, Body: "edited", ExpectedRevision: 1})
	if err != nil || edited.Revision != 2 {
		t.Fatalf("edit=%+v %v", edited, err)
	}
	if _, err = s.PutReaction(ctx, chat.Reaction{TenantID: c.TenantID, HomeTenantID: c.TenantID, ConversationID: c.ID, PostID: newPost.ID, SubjectID: "alice", Emoji: "+1"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutPin(ctx, chat.Pin{TenantID: c.TenantID, PinnedByHomeTenantID: c.TenantID, ConversationID: c.ID, PostID: newPost.ID, PinnedBy: "alice"}); err != nil {
		t.Fatal(err)
	}
	seen := map[chat.ConversationEventKind]bool{}
	lastSeq := uint64(0)
	eventDeadline := time.After(3 * time.Second)
	for len(seen) < 3 {
		select {
		case event, open := <-watch:
			if !open {
				t.Fatal("watch closed before mutation events")
			}
			if event.Event.Sequence <= lastSeq {
				t.Fatalf("non-monotonic offset: %+v", event)
			}
			lastSeq = event.Event.Sequence
			if event.Event.Post != nil && event.Event.Post.ConversationID != c.ID {
				t.Fatalf("cross-conversation event=%+v", event)
			}
			switch event.Event.Kind {
			case chat.PostEdited:
				seen[chat.PostEdited] = true
			case chat.ReactionChanged:
				seen[chat.ReactionChanged] = true
			case chat.PinChanged:
				seen[chat.PinChanged] = true
			}
		case <-eventDeadline:
			t.Fatalf("missing mutation events: %+v", seen)
		}
	}
	cancel()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, open := <-watch:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("watch leaked")
		}
	}
}
