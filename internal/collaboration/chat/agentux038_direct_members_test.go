package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// directMembersStore is the fake store with a member list, which the fake
// itself does not keep.
type directMembersStore struct {
	*fakeStore
	members []Membership
	listErr error
	more    bool
}

func (s *directMembersStore) ListMemberships(context.Context, string, string, Page) (ListMembershipsResponse, error) {
	if s.listErr != nil {
		return ListMembershipsResponse{}, s.listErr
	}
	out := ListMembershipsResponse{Memberships: append([]Membership(nil), s.members...)}
	if s.more {
		out.NextCursor = "next"
	}
	return out, nil
}

func (s *directMembersStore) GetMembership(_ context.Context, _, _, home, subject string) (Membership, error) {
	for _, member := range s.members {
		if member.SubjectID == subject && member.HomeTenantID == home {
			return member, nil
		}
	}
	return Membership{}, ErrNotFound
}

// TestTodo_AGENTUX_038_Security: a second person cannot be added to a direct
// conversation that has its two members, by its owner or by anybody else, so
// nobody is put into the audience of a person's conversation with an agent; an
// existing member's own row can still be written again; a conversation short
// of its two members can be completed; and other kinds of conversation take
// new members as before.
func TestTodo_AGENTUX_038_Security(t *testing.T) {
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return at }
	owner := Principal{TenantID: "tenant-a", SubjectID: "alice"}
	direct := Conversation{ID: "dm", TenantID: "tenant-a", Kind: Direct, OwnerID: "alice", Revision: 1}
	joined := at.Add(-time.Hour)
	alice := Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "dm", SubjectID: "alice", Role: Manager, JoinedAt: &joined}
	agent := Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "dm", SubjectID: "agent-1", Role: Member, JoinedAt: &joined}
	build := func(members ...Membership) (*Service, *directMembersStore) {
		store := &directMembersStore{fakeStore: &fakeStore{conversation: direct, membership: alice}, members: members}
		s := NewService(store, clock)
		s.SetAuthority(verifiedAuthority{store: store.fakeStore})
		return s, store
	}

	intruder := Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "dm", SubjectID: "mallory", Role: Member}
	s, store := build(alice, agent)
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: intruder}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("the owner added a second person to a direct conversation: %v", err)
	}
	if store.mutations != 0 || store.put.SubjectID != "" {
		t.Fatalf("a membership was written: %+v", store.put)
	}
	// Somebody who is not the owner is refused before the store is asked.
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: Principal{TenantID: "tenant-a", SubjectID: "mallory"}, Membership: intruder}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("a stranger joined a direct conversation: %v", err)
	}
	// A guest from another workspace is refused the same way.
	guest := intruder
	guest.HomeTenantID = "tenant-b"
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: guest}); !errors.Is(err, ErrPermissionDenied) || store.mutations != 0 {
		t.Fatalf("a guest was added to a direct conversation: %v", err)
	}
	// A member who left does not make room for a stranger while two remain.
	left := Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "dm", SubjectID: "former", JoinedAt: &joined, LeftAt: &at}
	s, store = build(alice, agent, left)
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: intruder}); !errors.Is(err, ErrPermissionDenied) || store.mutations != 0 {
		t.Fatalf("a third member was added beside two current ones: %v", err)
	}
	// A member list that cannot be read, or is not a direct conversation's, is a refusal.
	s, store = build(alice, agent)
	store.listErr = errors.New("store offline")
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: intruder}); err == nil || store.mutations != 0 {
		t.Fatalf("a member was added although the member list could not be read: %v", err)
	}
	store.listErr, store.more = nil, true
	store.members = []Membership{alice}
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: intruder}); !errors.Is(err, ErrPermissionDenied) || store.mutations != 0 {
		t.Fatalf("a member was added to a direct conversation with more members than a page: %v", err)
	}

	// An existing member's row is written again: that is not an addition.
	s, store = build(alice, agent)
	again := Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "dm", SubjectID: "alice", Role: Manager}
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: again}); err != nil || store.put.SubjectID != "alice" {
		t.Fatalf("an existing member's row could not be written: %v %+v", err, store.put)
	}
	// A conversation created with its owner alone is completed with its second member.
	s, store = build(alice)
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: agent}); err != nil || store.put.SubjectID != "agent-1" {
		t.Fatalf("a direct conversation could not be given its second member: %v", err)
	}

	// A private channel still takes a new member from its owner.
	channel := &fakeStore{conversation: Conversation{ID: "room", TenantID: "tenant-a", Kind: PrivateChannel, OwnerID: "alice", Revision: 1}, membership: Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "room", SubjectID: "alice", Role: Manager, JoinedAt: &joined}}
	newcomer := Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "room", SubjectID: "bob", Role: Member}
	if _, err := newTestService(channel, clock).AddMembership(context.Background(), AddMembershipRequest{Principal: owner, Membership: newcomer}); err != nil || channel.put.SubjectID != "bob" {
		t.Fatalf("a private channel refused a new member: %v", err)
	}
}
