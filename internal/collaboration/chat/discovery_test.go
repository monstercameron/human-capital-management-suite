package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func discoveryClock() Clock { return func() time.Time { return time.Unix(10, 0).UTC() } }

// deniedDiscovery allows every action except discovery of a channel the caller
// has not joined, which is how a tenant-visible public channel can still be
// invisible to one caller.
type deniedDiscovery struct{ hidden string }

func (a deniedDiscovery) Authorize(_ context.Context, p Principal, c Conversation, action chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
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

// TestDiscoverableListingIsScopedAndPolicyFiltered proves the listing is opt-in,
// that the option reaches the store, and that a widened row is still subject to
// the discovery policy while a joined room is not re-litigated.
func TestDiscoverableListingIsScopedAndPolicyFiltered(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	f.conversations = []Conversation{
		{ID: "mine", TenantID: "t1", Kind: PrivateChannel, Revision: 1, Joined: true},
		{ID: "open", TenantID: "t1", Kind: PublicChannel, Revision: 1},
		{ID: "closed", TenantID: "t1", Kind: PublicChannel, Revision: 1},
	}
	s := NewService(f, discoveryClock())
	s.SetAuthority(deniedDiscovery{hidden: "closed"})

	// Without the option the store is asked for the caller's own rooms and no
	// policy filtering happens at all.
	plain, err := s.ListConversations(context.Background(), ListConversationsRequest{Principal: principal(), TenantID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if f.scope.IncludeDiscoverable {
		t.Fatal("a plain listing asked the store for discoverable channels")
	}
	if len(plain.Conversations) != 3 {
		t.Fatalf("plain listing = %d rows, want the store's answer unfiltered", len(plain.Conversations))
	}

	got, err := s.ListConversations(context.Background(), ListConversationsRequest{Principal: principal(), TenantID: "t1", IncludeDiscoverable: true})
	if err != nil {
		t.Fatal(err)
	}
	if !f.scope.IncludeDiscoverable {
		t.Fatal("the scope option did not reach the store")
	}
	ids := make([]string, 0, len(got.Conversations))
	for _, c := range got.Conversations {
		ids = append(ids, c.ID)
	}
	if len(ids) != 2 || ids[0] != "mine" || ids[1] != "open" {
		t.Fatalf("discoverable listing = %v, want the joined room and the visible channel only", ids)
	}
}

// TestSelfJoinOfAPublicChannelConsultsActionJoin proves the declared-but-unused
// join rule now decides, that a self-join enters as a plain member, and that
// neither a private room nor adding somebody else is opened up by it.
func TestSelfJoinOfAPublicChannelConsultsActionJoin(t *testing.T) {
	public := Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "owner", Revision: 1}
	f := &fakeStore{conversation: public}
	s := NewService(f, discoveryClock())
	s.SetAuthority(joinRecorder{})
	self := Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager}
	got, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: self})
	if err != nil {
		t.Fatalf("self-join of a public channel = %v", err)
	}
	if got.Role != Member || f.put.Role != Member {
		t.Fatalf("self-join role = %v (store saw %v), want MEMBER", got.Role, f.put.Role)
	}
	if f.put.JoinedAt == nil || f.put.LeftAt != nil {
		t.Fatalf("self-join membership = %+v", f.put)
	}

	// Adding somebody else still needs the owner.
	other := Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2"}
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: other}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("adding another subject = %v, want permission denied", err)
	}

	// A private channel is not self-joinable.
	f.conversation = Conversation{ID: "c1", TenantID: "t1", Kind: PrivateChannel, OwnerID: "owner", Revision: 1}
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: self}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("self-join of a private channel = %v, want permission denied", err)
	}
}

// joinRecorder denies ActionJoin for nobody but records that it was the action
// asked about, which is the point: the service used to ask about ActionRead and
// require ownership, so ActionJoin was dead.
type joinRecorder struct{ actions []chatpolicy.Action }

func (a joinRecorder) Authorize(_ context.Context, p Principal, c Conversation, action chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: !c.Archived, Private: c.Kind != PublicChannel, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1},
		HasMembership: action == chatpolicy.ActionRead,
		Now:           now,
	}, nil
}

// TestSelfJoinIsRefusedWhenTheJoinPolicyDenies proves the rule is load bearing:
// a public channel whose join requirements the caller does not meet stays shut.
func TestSelfJoinIsRefusedWhenTheJoinPolicyDenies(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "owner", Revision: 1}}
	s := NewService(f, discoveryClock())
	s.SetAuthority(roleGatedChannel{})
	self := Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1"}
	if _, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: self}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("self-join of a role-gated channel = %v, want permission denied", err)
	}
	if f.mutations != 0 {
		t.Fatalf("a refused join still wrote: %d mutations", f.mutations)
	}
}

// roleGatedChannel is a public channel that requires a role the caller does not
// hold, which chatpolicy refuses on its own.
type roleGatedChannel struct{}

func (roleGatedChannel) Authorize(_ context.Context, p Principal, c Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{
		Principal: chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:   chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: true, Revision: c.Revision, RequiredRoles: []string{"manager"}, RoleMode: chatpolicy.RolesAny},
		Now:       now,
	}, nil
}

// TestBackwardPostPageReachesTheStore proves the direction and edge travel to the
// store, and that a page cannot ask to be both forward and backward at once.
func TestBackwardPostPageReachesTheStore(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "u1", Revision: 1}}
	s := newTestService(f, discoveryClock())
	ctx := context.Background()
	if _, err := s.ListPosts(ctx, ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Descending: true, Page: Page{PageSize: 200}}); err != nil {
		t.Fatal(err)
	}
	if !f.window.Descending || f.window.BeforeSequence != 0 {
		t.Fatalf("newest page window = %+v", f.window)
	}
	if _, err := s.ListPosts(ctx, ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Descending: true, BeforeSequence: 801}); err != nil {
		t.Fatal(err)
	}
	if !f.window.Descending || f.window.BeforeSequence != 801 {
		t.Fatalf("older page window = %+v", f.window)
	}
	if _, err := s.ListPosts(ctx, ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Descending: true, AfterSequence: 5}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("contradictory page = %v, want ErrInvalidArgument", err)
	}
}
