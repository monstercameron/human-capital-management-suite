package chatstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// TestTodo_CHATUX_022_Integration proves "Mark unread from here" against the
// real store: a rewind moves the read position back, the sidebar count follows
// it, and neither call can move the position the other way.
func TestTodo_CHATUX_022_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	const tenant = "tenant-a"
	c := chat.Conversation{ID: "rewind", TenantID: tenant, Kind: chat.PrivateChannel, Name: "rewind", OwnerID: "alice", Revision: 1}
	member := func(id string, role chat.MembershipRole) chat.Membership {
		return chat.Membership{ConversationID: c.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: id, Role: role, HistoryVisibility: chat.FullHistory}
	}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{member("alice", chat.Manager), member("bob", chat.Member)}, ""); err != nil {
		t.Fatal(err)
	}
	var posts []chat.Post
	for i := range 3 {
		p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "bob"}, TenantID: tenant, ConversationID: c.ID, IdempotencyKey: fmt.Sprintf("rewind-%d", i)}, chat.Post{AuthorID: "bob", Body: fmt.Sprintf("post %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		posts = append(posts, p)
	}
	unread := func() uint64 {
		t.Helper()
		counts, err := NewRecipientStateStore(s.Store).Counts(ctx, chatrecipient.Identity{HostTenantID: tenant, HomeTenantID: tenant, SubjectID: "alice", ConversationID: c.ID})
		if err != nil {
			t.Fatal(err)
		}
		return counts.Unread
	}
	state := func(sequence uint64) chat.ReadState {
		return chat.ReadState{TenantID: tenant, ConversationID: c.ID, SubjectID: "alice", LastReadSequence: sequence}
	}

	// Nothing read yet: a rewind has nothing to take back and leaves it so.
	r, err := s.RewindReadState(ctx, state(posts[1].Sequence), 1)
	if err != nil || r.LastReadSequence != 0 {
		t.Fatalf("rewind before any read=%+v %v, want position 0", r, err)
	}
	if got := unread(); got != 3 {
		t.Fatalf("unread before reading=%d, want 3", got)
	}

	r, err = s.PutReadState(ctx, state(posts[2].Sequence), r.Revision)
	if err != nil || r.LastReadSequence != posts[2].Sequence {
		t.Fatalf("read to the end=%+v %v", r, err)
	}
	if got := unread(); got != 0 {
		t.Fatalf("unread after reading=%d, want 0", got)
	}

	// Mark unread from the second post: the position is the post before it.
	r, err = s.RewindReadState(ctx, state(posts[1].Sequence-1), r.Revision)
	if err != nil || r.LastReadSequence != posts[0].Sequence {
		t.Fatalf("rewind=%+v %v, want position %d", r, err, posts[0].Sequence)
	}
	if got := unread(); got != 2 {
		t.Fatalf("unread after marking unread from the second post=%d, want 2", got)
	}
	loaded, err := s.GetReadState(ctx, tenant, c.ID, tenant, "alice")
	if err != nil || loaded.LastReadSequence != posts[0].Sequence || loaded.Revision != r.Revision {
		t.Fatalf("stored position=%+v %v, want %+v", loaded, err, r)
	}

	// A rewind never marks anything read, and an advance never un-reads.
	r, err = s.RewindReadState(ctx, state(posts[2].Sequence), r.Revision)
	if err != nil || r.LastReadSequence != posts[0].Sequence {
		t.Fatalf("a rewind to a later post moved the position forward: %+v %v", r, err)
	}
	r, err = s.PutReadState(ctx, state(0), r.Revision)
	if err != nil || r.LastReadSequence != posts[0].Sequence {
		t.Fatalf("an advance to an earlier post moved the position back: %+v %v", r, err)
	}

	if _, err = s.RewindReadState(ctx, state(0), 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale rewind=%v, want ErrConflict", err)
	}
	if _, err = s.RewindReadState(ctx, state(posts[2].Sequence+1), r.Revision); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("rewind past the newest post=%v, want ErrInvalidArgument", err)
	}
	stranger := state(0)
	stranger.SubjectID = "mallory"
	if _, err = s.RewindReadState(ctx, stranger, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("rewind by a non-member=%v, want ErrPermissionDenied", err)
	}
}
