package chatstore

import (
	"context"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestDiscoverableListingReturnsUnjoinedPublicChannels_Integration proves the
// listing can answer "what could I join here" and not only "where am I", that a
// private room the caller is not in stays invisible, and that each row says
// whether the caller is already a member.
func TestDiscoverableListingReturnsUnjoinedPublicChannels_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant := "tenant-a"
	mine := chat.Conversation{ID: "c-mine", TenantID: tenant, Kind: chat.PrivateChannel, Name: "mine", OwnerID: "alice", Revision: 1}
	open := chat.Conversation{ID: "c-open", TenantID: tenant, Kind: chat.PublicChannel, Name: "general", OwnerID: "bob", Revision: 1}
	shut := chat.Conversation{ID: "c-shut", TenantID: tenant, Kind: chat.PrivateChannel, Name: "leadership", OwnerID: "bob", Revision: 1}
	for _, c := range []chat.Conversation{mine, open, shut} {
		owner := c.OwnerID
		if c.ID == mine.ID {
			owner = "alice"
		}
		member := chat.Membership{ConversationID: c.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: owner, Role: chat.Manager, HistoryVisibility: chat.FullHistory}
		if _, err := s.CreateConversation(ctx, c, []chat.Membership{member}, ""); err != nil {
			t.Fatal(err)
		}
	}
	alice := chat.Principal{TenantID: tenant, SubjectID: "alice"}

	joinedOnly, err := s.ListConversations(ctx, alice, tenant, chat.Page{PageSize: 50}, chat.ConversationScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(joinedOnly.Conversations) != 1 || joinedOnly.Conversations[0].ID != mine.ID {
		t.Fatalf("joined listing = %+v", joinedOnly.Conversations)
	}

	discoverable, err := s.ListConversations(ctx, alice, tenant, chat.Page{PageSize: 50}, chat.ConversationScope{IncludeDiscoverable: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range discoverable.Conversations {
		seen[c.ID] = c.Joined
	}
	if len(discoverable.Conversations) != 2 {
		t.Fatalf("discoverable listing = %+v, want the joined room and the public channel", discoverable.Conversations)
	}
	if joined, ok := seen[mine.ID]; !ok || !joined {
		t.Fatalf("own room reported joined=%v ok=%v", joined, ok)
	}
	if joined, ok := seen[open.ID]; !ok || joined {
		t.Fatalf("public channel reported joined=%v ok=%v", joined, ok)
	}
	if _, ok := seen[shut.ID]; ok {
		t.Fatal("a private channel the caller is not in was discoverable")
	}
}

// TestBackwardPostPagingWalksOlder_Integration proves a client can open a
// conversation at its newest posts and page older, that each page arrives in
// conversation order, and that the history-visibility rule is not relaxed by the
// new direction.
func TestBackwardPostPagingWalksOlder_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant := "tenant-a"
	c := chat.Conversation{ID: "c-page", TenantID: tenant, Kind: chat.PublicChannel, Name: "paging", OwnerID: "alice", Revision: 1}
	alice := chat.Membership{ConversationID: c.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{alice}, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		key := "post-" + string(rune('a'+i))
		if _, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: tenant, ConversationID: c.ID, IdempotencyKey: key}, chat.Post{AuthorID: "alice", Body: key}); err != nil {
			t.Fatal(err)
		}
	}
	p := chat.Principal{TenantID: tenant, SubjectID: "alice"}

	newest, err := s.ListPosts(ctx, p, tenant, c.ID, 0, chat.Page{PageSize: 3}, chat.PostWindow{Descending: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(newest.Posts) != 3 || newest.NextCursor == "" {
		t.Fatalf("newest page = %+v", newest)
	}
	if newest.Posts[0].Sequence != 5 || newest.Posts[2].Sequence != 7 {
		t.Fatalf("newest page sequences = %d..%d, want 5..7 in conversation order", newest.Posts[0].Sequence, newest.Posts[2].Sequence)
	}

	older, err := s.ListPosts(ctx, p, tenant, c.ID, 0, chat.Page{PageSize: 3, Cursor: newest.NextCursor}, chat.PostWindow{Descending: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Posts) != 3 || older.Posts[0].Sequence != 2 || older.Posts[2].Sequence != 4 {
		t.Fatalf("older page = %+v, want sequences 2..4", older.Posts)
	}

	oldest, err := s.ListPosts(ctx, p, tenant, c.ID, 0, chat.Page{PageSize: 3, Cursor: older.NextCursor}, chat.PostWindow{Descending: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(oldest.Posts) != 1 || oldest.Posts[0].Sequence != 1 || oldest.NextCursor != "" {
		t.Fatalf("oldest page = %+v, want the first post and no further cursor", oldest)
	}

	// An explicit edge is honoured, and it is exclusive.
	bounded, err := s.ListPosts(ctx, p, tenant, c.ID, 0, chat.Page{PageSize: 2}, chat.PostWindow{Descending: true, BeforeSequence: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Posts) != 2 || bounded.Posts[1].Sequence != 3 {
		t.Fatalf("bounded page = %+v, want sequences 2..3", bounded.Posts)
	}

	// A member with FROM_JOIN history sees nothing older than its join, whichever
	// direction it pages.
	bob := chat.Membership{ConversationID: c.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FromJoin}
	if _, err := s.PutMembership(ctx, chat.Principal{TenantID: tenant, SubjectID: "alice"}, bob); err != nil {
		t.Fatal(err)
	}
	hidden, err := s.ListPosts(ctx, chat.Principal{TenantID: tenant, SubjectID: "bob"}, tenant, c.ID, 0, chat.Page{PageSize: 50}, chat.PostWindow{Descending: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden.Posts) != 0 {
		t.Fatalf("backward page leaked pre-join history: %+v", hidden.Posts)
	}
}

// TestConversationSummaryColumnsDescribeTheRoom_Integration proves the two
// derived columns every conversation read now carries: how many people are
// actually in the room, and when it last said anything. It pins the three
// answers that a naive count(*) or max(created_at) would get wrong — a removed
// member is not a member, a tombstoned post is not activity, and a room that has
// never been posted in has no last activity rather than the zero time — and
// checks all three listings agree: GetConversation, the joined listing and the
// discoverable listing.
func TestConversationSummaryColumnsDescribeTheRoom_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant := "tenant-a"
	alice := chat.Principal{TenantID: tenant, SubjectID: "alice"}

	room := chat.Conversation{ID: "c-room", TenantID: tenant, Kind: chat.PublicChannel, Name: "general", OwnerID: "alice", Revision: 1}
	quiet := chat.Conversation{ID: "c-quiet", TenantID: tenant, Kind: chat.PublicChannel, Name: "quiet", OwnerID: "bob", Revision: 1}
	owner := func(c chat.Conversation) chat.Membership {
		return chat.Membership{ConversationID: c.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: c.OwnerID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	}
	for _, c := range []chat.Conversation{room, quiet} {
		if _, err := s.CreateConversation(ctx, c, []chat.Membership{owner(c)}, ""); err != nil {
			t.Fatal(err)
		}
	}
	// alice, bob and carol join the room; carol then leaves, so the count must
	// be two rather than the three membership rows the table holds.
	for _, who := range []string{"bob", "carol"} {
		m := chat.Membership{ConversationID: room.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: who, Role: chat.Member, HistoryVisibility: chat.FullHistory}
		if _, err := s.PutMembership(ctx, alice, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RemoveMembership(ctx, alice, tenant, room.ID, tenant, "carol", 1); err != nil {
		t.Fatal(err)
	}

	// Three posts with pinned creation times. The newest is deleted, so the
	// answer is the middle one, not the latest row in the table.
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	newest := base.Add(2 * time.Hour)
	want := base.Add(time.Hour)
	var doomed chat.Post
	for i, at := range []time.Time{base, want, newest} {
		key := "sum-" + string(rune('a'+i))
		p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: alice, TenantID: tenant, ConversationID: room.ID, IdempotencyKey: key}, chat.Post{AuthorID: "alice", Body: key, CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		doomed = p
	}
	if _, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: alice, TenantID: tenant, ConversationID: room.ID, PostID: doomed.ID, ExpectedRevision: doomed.Revision}); err != nil {
		t.Fatal(err)
	}

	assertRoom := func(where string, c chat.Conversation) {
		t.Helper()
		if c.MemberCount != 2 {
			t.Fatalf("%s: member count = %d, want 2 active memberships", where, c.MemberCount)
		}
		if c.LastActivityAt == nil {
			t.Fatalf("%s: last activity is nil, want the newest surviving post", where)
		}
		if !c.LastActivityAt.UTC().Equal(want) {
			t.Fatalf("%s: last activity = %s, want %s (the tombstoned post must not count)", where, c.LastActivityAt.UTC(), want)
		}
	}

	got, err := s.GetConversation(ctx, tenant, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertRoom("GetConversation", got)

	joined, err := s.ListConversations(ctx, alice, tenant, chat.Page{PageSize: 50}, chat.ConversationScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(joined.Conversations) != 1 {
		t.Fatalf("joined listing = %+v, want only the room alice is in", joined.Conversations)
	}
	assertRoom("joined listing", joined.Conversations[0])

	discoverable, err := s.ListConversations(ctx, alice, tenant, chat.Page{PageSize: 50}, chat.ConversationScope{IncludeDiscoverable: true})
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]chat.Conversation{}
	for _, c := range discoverable.Conversations {
		rows[c.ID] = c
	}
	if len(rows) != 2 {
		t.Fatalf("discoverable listing = %+v, want both public channels", discoverable.Conversations)
	}
	assertRoom("discoverable listing", rows[room.ID])

	// The channel alice has not joined still describes itself, and a room with no
	// posts reports no activity rather than a zero time.
	if n := rows[quiet.ID].MemberCount; n != 1 {
		t.Fatalf("unjoined channel member count = %d, want 1", n)
	}
	if at := rows[quiet.ID].LastActivityAt; at != nil {
		t.Fatalf("channel with no posts reported last activity %s, want nil", at)
	}
	empty, err := s.GetConversation(ctx, tenant, quiet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.MemberCount != 1 || empty.LastActivityAt != nil {
		t.Fatalf("GetConversation on an empty channel = count %d, activity %v", empty.MemberCount, empty.LastActivityAt)
	}
}
