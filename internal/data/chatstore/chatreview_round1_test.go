package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// TestChatReview_ThreadSnapshotWithholdsRestricted proves the foreground thread
// snapshot applies the same audience rule as history, search and the background
// snapshot: a restricted post is visible to its author and frozen members only.
func TestChatReview_ThreadSnapshotWithholdsRestricted(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "thread-room", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant-a", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant-a", Role: "member", State: "active"},
	})
	ctx := context.Background()
	root, err := store.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "thread-room", AuthorID: "alice", ClientKey: "root", Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	restricted, err := store.sendPostRaw(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}, Restricted: true, Members: []string{"alice"}}), SendRequest{TenantID: "tenant-a", ConversationID: "thread-room", AuthorID: "alice", ClientKey: "restricted-reply", ParentID: root.ID, Body: "nights only"})
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := store.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "thread-room", AuthorID: "bob", ClientKey: "invoke", ParentID: root.ID, Body: "question"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(store)
	bob, err := adapter.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "bob"}, TenantID: "tenant-a", ConversationID: "thread-room", ThreadID: root.ID, InvokingPostID: invoking.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, post := range bob.Posts {
		if post.ID == restricted.ID {
			t.Fatalf("non-audience snapshot contains restricted post %s", post.ID)
		}
	}
	if len(bob.Posts) != 2 {
		t.Fatalf("non-audience snapshot holds %d posts, want root plus invoking", len(bob.Posts))
	}
	alice, err := adapter.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "thread-room", ThreadID: root.ID, InvokingPostID: restricted.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(alice.Posts) != 3 {
		t.Fatalf("audience snapshot holds %d posts, want all three", len(alice.Posts))
	}
}

// TestChatReview_RestrictedSendNilMembers proves a restricted send whose gate
// resolved no members stores an empty audience (author-only) instead of
// failing the write on a NOT NULL violation.
func TestChatReview_RestrictedSendNilMembers(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "secret", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant-a", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant-a", Role: "member", State: "active"},
	})
	ctx := context.Background()
	post, err := store.sendPostRaw(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}, Restricted: true}), SendRequest{TenantID: "tenant-a", ConversationID: "secret", AuthorID: "alice", ClientKey: "r1", Body: "nights only"})
	if err != nil {
		t.Fatalf("restricted send with nil members = %v, want author-only success", err)
	}
	adapter := NewAdapter(store)
	if readable, err := adapter.PostReadableBy(ctx, "tenant-a", "secret", post.ID, "alice"); err != nil || !readable {
		t.Fatalf("author readability = %v %v, want true", readable, err)
	}
	if readable, err := adapter.PostReadableBy(ctx, "tenant-a", "secret", post.ID, "bob"); err != nil || readable {
		t.Fatalf("non-member readability = %v %v, want false", readable, err)
	}
}

// TestChatReview_DisclosureTitleMatchingChannelName proves a citation title
// that names the channel authorizes, while a mismatched title still refuses.
func TestChatReview_DisclosureTitleMatchingChannelName(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	if _, err := s.CreateConversation(ctx, chat.Conversation{ID: "persona-reply", TenantID: owner.TenantID, Kind: chat.PublicChannel, Name: "general", OwnerID: owner.SubjectID, Revision: 1}, []chat.Membership{{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: "persona-reply", SubjectID: owner.SubjectID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	population := PublicAudiencePolicy{Classification: "INTERNAL", Principals: []PublicAudiencePrincipal{{HomeTenantID: owner.TenantID, SubjectID: owner.SubjectID}, {HomeTenantID: owner.TenantID, SubjectID: "future"}}}
	if _, err := s.Store.PutPublicAudiencePolicy(ctx, owner.TenantID, "persona-reply", 0, population); err != nil {
		t.Fatal(err)
	}
	ceiling := PersonaChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []string{"PUBLIC"}}
	if _, err := s.Store.PutPersonaChannelPolicy(ctx, owner.TenantID, "persona-reply", 0, ceiling); err != nil {
		t.Fatal(err)
	}
	post, err := s.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: "persona-reply", IdempotencyKey: "public-source"}, chat.Post{AuthorID: owner.SubjectID, Body: "Vacation policy"})
	if err != nil {
		t.Fatal(err)
	}
	digest := publicChatBodyDigest(post.Body)
	if err = s.Store.PutPublicChatPostClassification(ctx, owner.TenantID, "persona-reply", post.ID, digest, dlp.ClassPublic); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", owner.TenantID, owner.SubjectID, post.ID, digest, "body", "general", "PUBLIC"); err != nil {
		t.Fatalf("title matching the channel = %v, want authorized", err)
	}
	if err = s.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", owner.TenantID, owner.SubjectID, post.ID, digest, "body", "private title", "PUBLIC"); err == nil {
		t.Fatal("mismatched citation title was authorized")
	}
}

// TestChatReview_SealedBackgroundDMPostHonorsContentGuard proves the sealed
// background DM copy passes the installed trusted-content guard like every
// other server-owned write, and writes nothing when refused.
func TestChatReview_SealedBackgroundDMPostHonorsContentGuard(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "dm-1", TenantID: "tenant-a", Kind: "DIRECT", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant-a", Role: "manager", State: "active"}})
	ctx := context.Background()
	identity := agentsecurity.FinalOutputIdentity{TenantID: "tenant-a", OutputID: "o-1", PersonaID: "persona"}
	count := func() int {
		var n int
		if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM chat_post WHERE tenant_id='tenant-a' AND conversation_id='dm-1'`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	refused := errors.New("refused by workspace filter")
	var asked []string
	s.SetTrustedContentGuard(func(_ context.Context, tenant, conversation, author, body string) error {
		asked = append(asked, tenant+"/"+conversation+"/"+author+"/"+body)
		return refused
	})
	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := s.insertSealedBackgroundDMPost(ctx, tx, identity, "dm-1", "hello", "digest-1")
		return err
	})
	if !errors.Is(err, refused) {
		t.Fatalf("refused background copy = %v, want the guard refusal", err)
	}
	if count() != 0 {
		t.Fatal("a refused background copy was stored")
	}
	s.SetTrustedContentGuard(nil)
	if err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := s.insertSealedBackgroundDMPost(ctx, tx, identity, "dm-1", "hello", "digest-1")
		return err
	}); err != nil {
		t.Fatalf("unguarded background copy = %v", err)
	}
	if count() != 1 || len(asked) != 1 || asked[0] != "tenant-a/dm-1/persona/hello" {
		t.Fatalf("stored %d, guard asked %q", count(), asked)
	}
}

// TestChatReview_WatchPageDeliversStatusChanged proves the service watch reader
// delivers a channel status change as a conversation update, like the paged
// event reader does since CHATBUG-075.
func TestChatReview_WatchPageDeliversStatusChanged(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "c-1", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", Name: "general", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "u-1", Role: "manager", State: "active"}, {MemberID: "u-2", Role: "member", State: "active"}})
	adapter := NewAdapter(s)
	ctx := context.Background()
	allow := func(context.Context, chat.ChannelStatus) error { return nil }
	if _, err := s.CommitChannelStatus(ctx, chatstateChange(chatpolicy.StatusLocked, 1), time.Now().UTC(), allow); err != nil {
		t.Fatal(err)
	}
	watcher := chat.WatchConversationRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "u-2"}, TenantID: "tenant-a", ConversationID: "c-1"}
	_, events, _, err := adapter.watchPage(ctx, watcher, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event.Kind != chat.ConversationUpdated || events[0].Event.Conversation == nil || events[0].Event.Conversation.ID != "c-1" {
		t.Fatalf("watch page events = %+v, want one conversation update for c-1", events)
	}
}
