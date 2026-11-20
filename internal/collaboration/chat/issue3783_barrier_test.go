package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// barrierPort is a stand-in for the workspace's rules: Sales and Advisory are
// kept apart, keyed by a "group:" prefix on the subject id.
type barrierPort struct {
	calls   int
	failure error
	asked   [][]string
}

func groupOf(subject string) string { return strings.SplitN(subject, ":", 2)[0] }

func (b *barrierPort) CheckJoin(ctx context.Context, _ string, joining []string, present func(context.Context) ([]string, error)) error {
	b.calls++
	b.asked = append(b.asked, joining)
	if b.failure != nil {
		return b.failure
	}
	have, err := present(ctx)
	if err != nil {
		return err
	}
	all := append(append([]string{}, have...), joining...)
	sales, advisory := false, false
	for _, id := range all {
		sales = sales || groupOf(id) == "sales"
		advisory = advisory || groupOf(id) == "advisory"
	}
	if sales && advisory {
		return ErrInformationBarrier
	}
	return nil
}

type barrierStore struct {
	*fakeStore
	members []Membership
	pages   int
}

func (b *barrierStore) ListMemberships(_ context.Context, _, _ string, p Page) (ListMembershipsResponse, error) {
	b.pages++
	if p.Cursor == "" && len(b.members) > 1 {
		return ListMembershipsResponse{Memberships: b.members[:1], NextCursor: "more"}, nil
	}
	if p.Cursor == "more" {
		return ListMembershipsResponse{Memberships: b.members[1:]}, nil
	}
	return ListMembershipsResponse{Memberships: b.members}, nil
}

func barrierFixture(kind ConversationKind, existing ...string) (*Service, *barrierStore, *barrierPort) {
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "tenant", Kind: kind, OwnerID: "owner", Revision: 1}}
	f.membership = Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: "owner", Role: Manager, JoinedAt: timePtr(time.Now())}
	store := &barrierStore{fakeStore: f}
	for _, id := range existing {
		store.members = append(store.members, Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: id, JoinedAt: timePtr(time.Now())})
	}
	s := NewService(store, time.Now)
	s.SetAuthority(verifiedAuthority{store: f})
	port := &barrierPort{}
	s.SetMembershipBarrier(port)
	return s, store, port
}

func addRequest(subject string) AddMembershipRequest {
	return AddMembershipRequest{Principal: Principal{TenantID: "tenant", SubjectID: "owner"}, Membership: Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: subject}}
}

func TestIssue3783_AddMembershipRefusedAcrossBarrier_Security(t *testing.T) {
	s, store, _ := barrierFixture(PrivateChannel, "sales:ann", "sales:bo")
	_, err := s.AddMembership(t.Context(), addRequest("advisory:cy"))
	if !errors.Is(err, ErrInformationBarrier) || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("want a barrier refusal that is also a denial, got %v", err)
	}
	if store.mutations != 0 {
		t.Fatalf("a refused add must not write: %d writes", store.mutations)
	}
	if strings.Contains(err.Error(), "sales") || strings.Contains(err.Error(), "advisory") {
		t.Fatalf("the refusal leaked a group: %v", err)
	}
	if _, err := s.AddMembership(t.Context(), addRequest("sales:dee")); err != nil || store.mutations != 1 {
		t.Fatalf("a member of the same side is admitted: %v writes=%d", err, store.mutations)
	}
}

func TestIssue3783_BarrierReadsEveryRosterPage(t *testing.T) {
	s, store, _ := barrierFixture(PrivateChannel, "sales:ann", "sales:bo", "sales:cy")
	if _, err := s.AddMembership(t.Context(), addRequest("advisory:di")); !errors.Is(err, ErrInformationBarrier) {
		t.Fatalf("a person on the second page of the roster was not counted: %v", err)
	}
	if store.pages < 2 {
		t.Fatalf("roster read in %d page(s)", store.pages)
	}
}

func TestIssue3783_SelfJoinOfPublicChannelIsChecked_Security(t *testing.T) {
	s, store, _ := barrierFixture(PublicChannel, "sales:ann")
	r := AddMembershipRequest{Principal: Principal{TenantID: "tenant", SubjectID: "advisory:cy"}, Membership: Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: "advisory:cy"}}
	if _, err := s.AddMembership(t.Context(), r); !errors.Is(err, ErrInformationBarrier) || store.mutations != 0 {
		t.Fatalf("self-join bypassed the barrier: %v writes=%d", err, store.mutations)
	}
}

func TestIssue3783_InviteToGroupIsChecked_Security(t *testing.T) {
	s, store, _ := barrierFixture(Group, "sales:ann")
	if _, err := s.InviteGroupMember(t.Context(), addRequest("advisory:cy")); !errors.Is(err, ErrInformationBarrier) || store.mutations != 0 {
		t.Fatalf("group invite bypassed the barrier: %v writes=%d", err, store.mutations)
	}
	if _, err := s.InviteGroupMember(t.Context(), addRequest("people:hal")); err != nil || store.mutations != 1 {
		t.Fatalf("an unrelated invite must pass: %v writes=%d", err, store.mutations)
	}
}

func TestIssue3783_CreateConversationChecksTheWholeStartingRoster_Security(t *testing.T) {
	s, store, port := barrierFixture(PrivateChannel)
	r := CreateConversationRequest{Principal: Principal{TenantID: "tenant", SubjectID: "sales:ann"}, TenantID: "tenant", Kind: Group, Name: "pitch", MemberIDs: []string{"advisory:cy"}}
	if _, err := s.CreateConversation(t.Context(), r); !errors.Is(err, ErrInformationBarrier) || store.mutations != 0 {
		t.Fatalf("a new conversation started across a barrier: %v writes=%d", err, store.mutations)
	}
	if len(port.asked) == 0 || len(port.asked[0]) != 2 {
		t.Fatalf("the creator and the invitee must both be asked about: %v", port.asked)
	}
	r.MemberIDs = []string{"sales:bo"}
	if _, err := s.CreateConversation(t.Context(), r); err != nil || store.mutations != 1 {
		t.Fatalf("a conversation within one side is created: %v writes=%d", err, store.mutations)
	}
}

func TestIssue3783_BarrierFailsClosed_Security(t *testing.T) {
	s, store, port := barrierFixture(PrivateChannel, "sales:ann")
	port.failure = errors.New("directory down")
	_, err := s.AddMembership(t.Context(), addRequest("people:hal"))
	if !errors.Is(err, ErrUnavailable) || store.mutations != 0 {
		t.Fatalf("an undecided barrier must refuse: %v writes=%d", err, store.mutations)
	}
	if strings.Contains(err.Error(), "directory down") {
		t.Fatalf("the cause must not reach the caller: %v", err)
	}
}

func TestIssue3783_NoBarrierPortKeepsTheOldBehaviour(t *testing.T) {
	s, store, _ := barrierFixture(PrivateChannel, "sales:ann")
	s.SetMembershipBarrier(nil)
	if _, err := s.AddMembership(t.Context(), addRequest("advisory:cy")); err != nil || store.mutations != 1 {
		t.Fatalf("a workspace with no barriers is unchanged: %v writes=%d", err, store.mutations)
	}
}

func TestIssue3783_GuestFromAnotherTenantIsNotAskedAbout(t *testing.T) {
	s, _, port := barrierFixture(PrivateChannel, "sales:ann")
	r := addRequest("advisory:guest")
	r.Membership.HomeTenantID = "partner"
	if _, err := s.AddMembership(t.Context(), r); err != nil && errors.Is(err, ErrInformationBarrier) {
		t.Fatalf("a guest has no group in this directory: %v", err)
	}
	for _, asked := range port.asked {
		if len(asked) != 0 {
			t.Fatalf("a guest was sent to the barrier port: %v", asked)
		}
	}
}
