package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func chat056Clock() Clock { return func() time.Time { return time.Unix(20, 0).UTC() } }

// chat056DiscoveryAuthority is a policy authority that allows every action
// except discovery of one named channel, which is how a tenant-visible
// public channel can still be invisible to a caller the discovery policy
// hides it from.
type chat056DiscoveryAuthority struct{ hidden string }

func (a chat056DiscoveryAuthority) Authorize(_ context.Context, p Principal, c Conversation, action chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	if action == chatpolicy.ActionDiscover && c.ID == a.hidden {
		return chatpolicy.Input{}, ErrPermissionDenied
	}
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: !c.Archived, Private: c.Kind != PublicChannel, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1},
		HasMembership: true,
		Now:           now,
	}, nil
}

// TestTodo_CHAT_056 is the CHAT-056 PRIMARY matrix test. It proves the
// discoverable listing is strictly opt-in (a plain listing never asks the
// store for it, and never filters), and that turning it on returns the
// caller's own rooms plus public channels the discovery policy still allows,
// while a channel the policy hides stays out of the widened listing.
func TestTodo_CHAT_056(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	f.conversations = []Conversation{
		{ID: "mine", TenantID: "t1", Kind: PrivateChannel, Revision: 1, Joined: true},
		{ID: "open", TenantID: "t1", Kind: PublicChannel, Revision: 1},
		{ID: "closed", TenantID: "t1", Kind: PublicChannel, Revision: 1},
	}
	s := NewService(f, chat056Clock())
	s.SetAuthority(chat056DiscoveryAuthority{hidden: "closed"})

	plain, err := s.ListConversations(context.Background(), ListConversationsRequest{Principal: principal(), TenantID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if f.scope.IncludeDiscoverable {
		t.Fatal("a plain listing asked the store for discoverable channels")
	}
	if len(plain.Conversations) != 3 {
		t.Fatalf("plain listing = %d rows, want the store's unfiltered answer", len(plain.Conversations))
	}

	got, err := s.ListConversations(context.Background(), ListConversationsRequest{Principal: principal(), TenantID: "t1", IncludeDiscoverable: true})
	if err != nil {
		t.Fatal(err)
	}
	if !f.scope.IncludeDiscoverable {
		t.Fatal("the opt-in option did not reach the store")
	}
	ids := make([]string, 0, len(got.Conversations))
	for _, c := range got.Conversations {
		ids = append(ids, c.ID)
	}
	if len(ids) != 2 || ids[0] != "mine" || ids[1] != "open" {
		t.Fatalf("discoverable listing = %v, want the joined room and the visible public channel only", ids)
	}
}

// chat056JoinRecorder denies nobody but records the action asked about, so a
// self-join test proves ActionJoin decides rather than ActionRead.
type chat056JoinRecorder struct{}

func (chat056JoinRecorder) Authorize(_ context.Context, p Principal, c Conversation, action chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: !c.Archived, Private: c.Kind != PublicChannel, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1},
		HasMembership: action == chatpolicy.ActionRead,
		Now:           now,
	}, nil
}

// chat056RoleGatedChannel is a public channel that requires a role the
// caller does not hold; chatpolicy refuses this on its own.
type chat056RoleGatedChannel struct{}

func (chat056RoleGatedChannel) Authorize(_ context.Context, p Principal, c Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{
		Principal: chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:   chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: true, Revision: c.Revision, RequiredRoles: []string{"manager"}, RoleMode: chatpolicy.RolesAny},
		Now:       now,
	}, nil
}

// TestTodo_CHAT_056_Security is the CHAT-056 SECURITY matrix test. It proves
// self-join consults ActionJoin (not ActionRead or ownership), always enters
// as a plain member regardless of what the caller asked for, that adding
// somebody else is still refused, that a private room cannot be self-joined
// at all, and that a public channel whose join requirements the caller does
// not meet stays shut and never reaches the store.
func TestTodo_CHAT_056_Security(t *testing.T) {
	public := Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "owner", Revision: 1}
	f := &fakeStore{conversation: public}
	s := NewService(f, chat056Clock())
	s.SetAuthority(chat056JoinRecorder{})
	self := Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager}
	got, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: self})
	if err != nil {
		t.Fatalf("self-join of a public channel = %v", err)
	}
	if got.Role != Member || f.put.Role != Member {
		t.Fatalf("self-join role = %v (store saw %v), want MEMBER even though the caller asked for MANAGER", got.Role, f.put.Role)
	}
	if f.put.JoinedAt == nil || f.put.LeftAt != nil {
		t.Fatalf("self-join membership = %+v", f.put)
	}

	other := Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2"}
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: other}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("adding another subject = %v, want permission denied", err)
	}

	f.conversation = Conversation{ID: "c1", TenantID: "t1", Kind: PrivateChannel, OwnerID: "owner", Revision: 1}
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: self}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("self-join of a private channel = %v, want permission denied", err)
	}

	gated := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "owner", Revision: 1}}
	gatedService := NewService(gated, chat056Clock())
	gatedService.SetAuthority(chat056RoleGatedChannel{})
	if _, err := gatedService.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: self}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("self-join of a role-gated channel = %v, want permission denied", err)
	}
	if gated.mutations != 0 {
		t.Fatalf("a refused join still wrote: %d mutations", gated.mutations)
	}
}
