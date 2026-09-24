package chatroutingadapter

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

// leaseRecorder demands a route lease on every conversation-scoped write and
// records which writes reached it. An unleased write is what the chat shard's
// route fence rejects, so the fake refuses it here too rather than letting a
// test pass on a call the store would have failed.
type leaseRecorder struct {
	chat.ConversationService
	seen  map[string]chatrouting.WriteLease
	calls int
}

func newLeaseRecorder() *leaseRecorder {
	return &leaseRecorder{seen: map[string]chatrouting.WriteLease{}}
}

func (f *leaseRecorder) record(ctx context.Context, name string) error {
	lease, ok := chatrouting.WriteLeaseFromContext(ctx)
	if !ok {
		return errors.New("missing route lease: " + name)
	}
	f.calls++
	f.seen[name] = lease
	return nil
}

func (f *leaseRecorder) UpdateConversation(ctx context.Context, _ chat.UpdateConversationRequest) (chat.Conversation, error) {
	return chat.Conversation{}, f.record(ctx, "UpdateConversation")
}
func (f *leaseRecorder) AddMembership(ctx context.Context, _ chat.AddMembershipRequest) (chat.Membership, error) {
	return chat.Membership{}, f.record(ctx, "AddMembership")
}
func (f *leaseRecorder) RemoveMembership(ctx context.Context, _ chat.RemoveMembershipRequest) (chat.Membership, error) {
	return chat.Membership{}, f.record(ctx, "RemoveMembership")
}
func (f *leaseRecorder) SendPost(ctx context.Context, _ chat.SendPostRequest) (chat.Post, error) {
	return chat.Post{}, f.record(ctx, "SendPost")
}
func (f *leaseRecorder) EditPost(ctx context.Context, _ chat.EditPostRequest) (chat.Post, error) {
	return chat.Post{}, f.record(ctx, "EditPost")
}
func (f *leaseRecorder) DeletePost(ctx context.Context, _ chat.DeletePostRequest) (chat.Post, error) {
	return chat.Post{}, f.record(ctx, "DeletePost")
}
func (f *leaseRecorder) AddReaction(ctx context.Context, _ chat.AddReactionRequest) (chat.Reaction, error) {
	return chat.Reaction{}, f.record(ctx, "AddReaction")
}
func (f *leaseRecorder) RemoveReaction(ctx context.Context, _ chat.RemoveReactionRequest) error {
	return f.record(ctx, "RemoveReaction")
}
func (f *leaseRecorder) PinPost(ctx context.Context, _ chat.PinPostRequest) (chat.Pin, error) {
	return chat.Pin{}, f.record(ctx, "PinPost")
}
func (f *leaseRecorder) UnpinPost(ctx context.Context, _ chat.UnpinPostRequest) error {
	return f.record(ctx, "UnpinPost")
}
func (f *leaseRecorder) UpdateReadState(ctx context.Context, _ chat.UpdateReadStateRequest) (chat.ReadState, error) {
	return chat.ReadState{}, f.record(ctx, "UpdateReadState")
}
func (f *leaseRecorder) UpdatePreferences(ctx context.Context, _ chat.UpdatePreferencesRequest) (chat.NotificationPreferences, error) {
	return chat.NotificationPreferences{}, f.record(ctx, "UpdatePreferences")
}
func (f *leaseRecorder) SendPostWithReferences(ctx context.Context, _ chat.SendPostWithReferencesRequest) (chat.Post, error) {
	return chat.Post{}, f.record(ctx, "SendPostWithReferences")
}
func (f *leaseRecorder) SuggestReferences(context.Context, chat.SuggestReferencesRequest) ([]chat.ReferenceCandidate, error) {
	return nil, nil
}
func (f *leaseRecorder) CreateShareLink(context.Context, chat.Principal, string, string, string) (chat.ConversationLink, error) {
	return chat.ConversationLink{}, nil
}
func (f *leaseRecorder) ResolveShareLink(context.Context, chat.Principal, string) (chat.Conversation, *chat.Post, error) {
	return chat.Conversation{}, nil, nil
}
func (f *leaseRecorder) ForwardPost(ctx context.Context, _ chat.ForwardPostRequest) (chat.Post, error) {
	return chat.Post{}, f.record(ctx, "ForwardPost")
}

// conversationWrites is every conversation-scoped write on the canonical
// service and its reference extension, addressed at one placed conversation.
func conversationWrites(s *Service) map[string]func(context.Context) error {
	p := principalFor("t1", "u1")
	return map[string]func(context.Context) error{
		"UpdateConversation": func(c context.Context) error {
			_, err := s.UpdateConversation(c, chat.UpdateConversationRequest{Principal: p, Conversation: chat.Conversation{ID: "c1", TenantID: "t1"}, ExpectedRevision: 1})
			return err
		},
		"AddMembership": func(c context.Context) error {
			_, err := s.AddMembership(c, chat.AddMembershipRequest{Principal: p, Membership: chat.Membership{ConversationID: "c1", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u2"}})
			return err
		},
		"RemoveMembership": func(c context.Context) error {
			_, err := s.RemoveMembership(c, chat.RemoveMembershipRequest{Principal: p, TenantID: "t1", ConversationID: "c1", SubjectID: "u2", ExpectedRevision: 1})
			return err
		},
		"SendPost": func(c context.Context) error {
			_, err := s.SendPost(c, chat.SendPostRequest{Principal: p, TenantID: "t1", ConversationID: "c1", Body: "hi", IdempotencyKey: "k"})
			return err
		},
		"EditPost": func(c context.Context) error {
			_, err := s.EditPost(c, chat.EditPostRequest{Principal: p, TenantID: "t1", ConversationID: "c1", PostID: "p1", Body: "edit", ExpectedRevision: 1})
			return err
		},
		"DeletePost": func(c context.Context) error {
			_, err := s.DeletePost(c, chat.DeletePostRequest{Principal: p, TenantID: "t1", ConversationID: "c1", PostID: "p1", ExpectedRevision: 1})
			return err
		},
		"AddReaction": func(c context.Context) error {
			_, err := s.AddReaction(c, chat.AddReactionRequest{Principal: p, Reaction: chat.Reaction{TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: "+1"}})
			return err
		},
		"RemoveReaction": func(c context.Context) error {
			return s.RemoveReaction(c, chat.RemoveReactionRequest{Principal: p, TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: "+1"})
		},
		"PinPost": func(c context.Context) error {
			_, err := s.PinPost(c, chat.PinPostRequest{Principal: p, Pin: chat.Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1"}})
			return err
		},
		"UnpinPost": func(c context.Context) error {
			return s.UnpinPost(c, chat.UnpinPostRequest{Principal: p, TenantID: "t1", ConversationID: "c1", PostID: "p1"})
		},
		"UpdateReadState": func(c context.Context) error {
			_, err := s.UpdateReadState(c, chat.UpdateReadStateRequest{Principal: p, ReadState: chat.ReadState{TenantID: "t1", ConversationID: "c1", SubjectID: "u1"}, ExpectedRevision: 1})
			return err
		},
		"UpdatePreferences": func(c context.Context) error {
			_, err := s.UpdatePreferences(c, chat.UpdatePreferencesRequest{Principal: p, Preferences: chat.NotificationPreferences{TenantID: "t1", ConversationID: "c1", SubjectID: "u1"}, ExpectedRevision: 1})
			return err
		},
		"SendPostWithReferences": func(c context.Context) error {
			_, err := s.SendPostWithReferences(c, chat.SendPostWithReferencesRequest{SendPostRequest: chat.SendPostRequest{Principal: p, TenantID: "t1", ConversationID: "c1", Body: "hi", IdempotencyKey: "k"}})
			return err
		},
		"ForwardPost": func(c context.Context) error {
			_, err := s.ForwardPost(c, chat.ForwardPostRequest{Principal: p, DestinationTenantID: "t1", DestinationConversationID: "c1", SourceTenantID: "t1", SourceConversationID: "c0", SourcePostID: "p0"})
			return err
		},
	}
}

func placeRoute(t *testing.T, d *chatrouting.MemoryDirectory) {
	t.Helper()
	route, err := d.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "c1", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(context.Background(), "c1", "t1", route.Epoch); err != nil {
		t.Fatal(err)
	}
}

// TestEveryConversationWriteCarriesARouteLease is the regression the live server
// found the hard way: only create, send and forward were leased, so every other
// write to a placed conversation failed in the store with ErrNoRouteLease.
func TestEveryConversationWriteCarriesARouteLease(t *testing.T) {
	f := newLeaseRecorder()
	s, d := newAdapter(t, f)
	placeRoute(t, d)
	writes := conversationWrites(s)
	for name, call := range writes {
		if err := call(context.Background()); err != nil {
			t.Fatalf("%s on a placed conversation = %v", name, err)
		}
		lease, ok := f.seen[name]
		if !ok || lease.Route.ShardID != "s1" || lease.Route.Epoch != 1 {
			t.Fatalf("%s lease = %+v ok=%v", name, lease, ok)
		}
	}
	if f.calls != len(writes) {
		t.Fatalf("writes that reached chat = %d, want %d", f.calls, len(writes))
	}
}

// TestEveryConversationWriteFailsOnAStaleEpoch proves the coverage is a fence
// and not a formality: once the move coordinator has bumped the epoch, no write
// reaches the chat owner, and the refusal is retryable rather than internal.
func TestEveryConversationWriteFailsOnAStaleEpoch(t *testing.T) {
	f := newLeaseRecorder()
	s, d := newAdapter(t, f)
	placeRoute(t, d)
	if _, err := d.BeginMove(context.Background(), "c1", "t1", 1, "s2"); err != nil {
		t.Fatal(err)
	}
	for name, call := range conversationWrites(s) {
		err := call(context.Background())
		if !errors.Is(err, chatrouting.ErrStaleEpoch) && !errors.Is(err, chatrouting.ErrNotWritable) {
			t.Fatalf("%s during a move = %v, want a fence refusal", name, err)
		}
		if !errors.Is(err, chat.ErrUnavailable) {
			t.Fatalf("%s fence refusal is not retryable: %v", name, err)
		}
	}
	if f.calls != 0 {
		t.Fatalf("%d stale writes reached chat", f.calls)
	}
}

// TestUnknownConversationWriteFailsClosedAndCreatedConversationIsRouted proves
// unknown identifiers cannot reach chat storage while the normal create path
// registers a route before subsequent writes.
func TestUnknownConversationWriteFailsClosedAndCreatedConversationIsRouted(t *testing.T) {
	passthrough := &unleasedRecorder{}
	s, _ := newAdapter(t, passthrough)
	if _, err := s.DeletePost(context.Background(), chat.DeletePostRequest{Principal: principalFor("t1", "u1"), TenantID: "t1", ConversationID: "unrouted", PostID: "p1", ExpectedRevision: 1}); !errors.Is(err, chatrouting.ErrNotFound) {
		t.Fatalf("delete on an unknown conversation = %v, want %v", err, chatrouting.ErrNotFound)
	}
	if passthrough.calls != 0 {
		t.Fatalf("unknown conversation reached chat %d times", passthrough.calls)
	}

	created, err := s.CreateConversation(context.Background(), chat.CreateConversationRequest{TenantID: "t1", Principal: principalFor("t1", "u1"), Kind: chat.PublicChannel, IdempotencyKey: "create-routed"})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := s.DeletePost(context.Background(), chat.DeletePostRequest{Principal: principalFor("t1", "u1"), TenantID: "t1", ConversationID: created.ID, PostID: "p1", ExpectedRevision: 1}); err != nil {
		t.Fatalf("delete on a created conversation = %v", err)
	}
	if !passthrough.leased || passthrough.calls != 1 {
		t.Fatalf("created conversation lease=%v calls=%d", passthrough.leased, passthrough.calls)
	}
}

type unleasedRecorder struct {
	chat.ConversationService
	calls  int
	leased bool
}

func (f *unleasedRecorder) DeletePost(ctx context.Context, _ chat.DeletePostRequest) (chat.Post, error) {
	f.calls++
	_, f.leased = chatrouting.WriteLeaseFromContext(ctx)
	return chat.Post{}, nil
}

func (f *unleasedRecorder) CreateConversation(ctx context.Context, r chat.CreateConversationRequest) (chat.Conversation, error) {
	return chat.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Kind: r.Kind}, nil
}
