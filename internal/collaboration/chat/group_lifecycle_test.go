package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestTodo_CHAT_016(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "existing-dm", TenantID: "t1", Kind: Direct, OwnerID: "u1", Revision: 1}}
	s := newTestService(f, func() time.Time { return time.Unix(100, 0).UTC() })
	group, err := s.CreatePrivateGroup(context.Background(), CreateConversationRequest{
		Principal: principal(), TenantID: "t1", ConversationID: "group-016", Kind: Group,
		Name: "Payroll launch", IdempotencyKey: "group-create-016",
		Members: []MemberRef{{TenantID: "t1", SubjectID: "u2"}, {TenantID: "t1", SubjectID: "u3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if group.ID == "existing-dm" || group.ID != "group-016" || group.Kind != Group || group.Name != "Payroll launch" {
		t.Fatalf("created group=%+v; expected a distinct named group", group)
	}
	if len(f.created) != 3 || f.created[0].ConversationID != group.ID || f.created[1].ConversationID != group.ID || f.created[2].ConversationID != group.ID {
		t.Fatalf("group memberships do not share the fresh group history: %+v", f.created)
	}
	if f.created[2].SubjectID != "u1" || f.created[2].Role != Manager {
		t.Fatalf("creator membership=%+v; want creator as manager", f.created[2])
	}
}

func TestTodo_CHAT_016_Security(t *testing.T) {
	ctx := context.Background()
	joined := time.Unix(10, 0).UTC()
	f := &chat016Store{fakeStore: &fakeStore{
		conversation: Conversation{ID: "group-016", TenantID: "t1", Kind: Group, Name: "Before", OwnerID: "u1", Revision: 4},
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "group-016", SubjectID: "u1", Role: Manager, JoinedAt: &joined, Revision: 2},
	}}
	s := NewService(f, func() time.Time { return time.Unix(100, 0).UTC() })
	fakeAuthority := verifiedAuthority{store: f.fakeStore}
	s.SetAuthority(fakeAuthority)

	renamed, err := s.RenamePrivateGroup(ctx, UpdateConversationRequest{Principal: principal(), Conversation: Conversation{ID: "group-016", TenantID: "t1", Kind: Direct, Name: "  Payroll launch ", OwnerID: "attacker"}, ExpectedRevision: 4})
	if err != nil || renamed.Name != "Payroll launch" || renamed.Kind != Group || renamed.OwnerID != "u1" {
		t.Fatalf("manager rename=%+v err=%v", renamed, err)
	}

	invite, err := s.InviteGroupMember(ctx, AddMembershipRequest{Principal: principal(), Membership: Membership{
		ConversationID: "group-016", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u2", Role: Manager, HistoryVisibility: FullHistory,
	}})
	if err != nil || invite.Role != Member || invite.HistoryVisibility != FromJoin || invite.JoinedAt == nil || invite.LeftAt != nil {
		t.Fatalf("invite=%+v err=%v; want ordinary member with history from invitation", invite, err)
	}
	if f.put.SubjectID != "u2" || f.put.Role != Member || f.put.HistoryVisibility != FromJoin {
		t.Fatalf("persisted invite=%+v", f.put)
	}
	beforeForeignInvite := f.mutations
	if _, err := s.InviteGroupMember(ctx, AddMembershipRequest{Principal: principal(), Membership: Membership{ConversationID: "group-016", TenantID: "t1", HomeTenantID: "t2", SubjectID: "foreign-user"}}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("cross-tenant group invite error=%v; want permission denied", err)
	}
	if f.mutations != beforeForeignInvite {
		t.Fatal("cross-tenant group invite reached the store")
	}

	ordinary := principal()
	ordinary.SubjectID = "u2"
	ordinaryStore := &fakeStore{conversation: f.conversation, membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "group-016", SubjectID: "u2", Role: Member, JoinedAt: &joined}}
	ordinaryService := newTestService(ordinaryStore, func() time.Time { return time.Unix(100, 0).UTC() })
	if _, err := ordinaryService.RenamePrivateGroup(ctx, UpdateConversationRequest{Principal: ordinary, Conversation: Conversation{ID: "group-016", TenantID: "t1", Name: "Unauthorized"}, ExpectedRevision: 4}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ordinary member rename error=%v; want permission denied", err)
	}
	if ordinaryStore.mutations != 0 {
		t.Fatalf("unauthorized rename made %d mutations", ordinaryStore.mutations)
	}
	if _, err := s.RevokeGroupMember(ctx, RemoveMembershipRequest{Principal: principal(), TenantID: "t1", HomeTenantID: "t1", ConversationID: "group-016", SubjectID: "u1", ExpectedRevision: 2}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("owner revoke error=%v; want permission denied", err)
	}
	if f.removedSubject != "" {
		t.Fatalf("owner revoke reached store for %q", f.removedSubject)
	}
	direct := principal()
	directStore := &fakeStore{conversation: Conversation{ID: "pair-016", TenantID: "t1", Kind: Direct, OwnerID: "u1", Revision: 1}, membership: f.membership}
	directService := newTestService(directStore, func() time.Time { return time.Unix(100, 0).UTC() })
	if _, err := directService.RenamePrivateGroup(ctx, UpdateConversationRequest{Principal: direct, Conversation: Conversation{ID: "pair-016", TenantID: "t1", Name: "Renamed pair"}, ExpectedRevision: 1}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("direct rename error=%v; want permission denied", err)
	}
	if _, err := directService.InviteGroupMember(ctx, AddMembershipRequest{Principal: direct, Membership: Membership{ConversationID: "pair-016", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u3"}}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("direct invite error=%v; want permission denied", err)
	}
	if directStore.mutations != 0 {
		t.Fatalf("group operations mutated direct conversation %d times", directStore.mutations)
	}
	foreignStore := &fakeStore{}
	foreignService := newTestService(foreignStore, func() time.Time { return time.Unix(100, 0).UTC() })
	if _, err := foreignService.CreatePrivateGroup(ctx, CreateConversationRequest{Principal: principal(), TenantID: "t1", Kind: Group, Members: []MemberRef{{TenantID: "t2", SubjectID: "u2"}, {TenantID: "t1", SubjectID: "u3"}}}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("cross-tenant group creation error=%v; want permission denied", err)
	}
	if foreignStore.mutations != 0 {
		t.Fatal("cross-tenant group creation reached the store")
	}
	if _, err := s.RevokeGroupMember(ctx, RemoveMembershipRequest{Principal: principal(), TenantID: "t1", HomeTenantID: "t1", ConversationID: "group-016", SubjectID: "u2", ExpectedRevision: 7}); err != nil {
		t.Fatalf("manager revoke member: %v", err)
	}
	if f.removedHome != "t1" || f.removedSubject != "u2" || f.removedRevision != 7 {
		t.Fatalf("revocation target = %s:%s at %d", f.removedHome, f.removedSubject, f.removedRevision)
	}
}

func TestTodo_CHAT_016_Golden(t *testing.T) {
	f := &fakeStore{}
	s := newTestService(f, func() time.Time { return time.Unix(100, 0).UTC() })
	group, err := s.CreatePrivateGroup(context.Background(), CreateConversationRequest{
		Principal: principal(), TenantID: "t1", ConversationID: "chat-016-golden", Kind: Group, Name: "Payroll launch",
		Members: []MemberRef{{TenantID: "t1", SubjectID: "u3"}, {TenantID: "t1", SubjectID: "u2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	type participant struct {
		TenantID          string          `json:"tenant_id"`
		SubjectID         string          `json:"subject_id"`
		Role              MembershipRole  `json:"role"`
		HistoryVisibility ReadHistoryFrom `json:"history_visibility"`
	}
	participants := make([]participant, 0, len(f.created))
	for _, m := range f.created {
		participants = append(participants, participant{m.HomeTenantID, m.SubjectID, m.Role, m.HistoryVisibility})
	}
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].TenantID != participants[j].TenantID {
			return participants[i].TenantID < participants[j].TenantID
		}
		return participants[i].SubjectID < participants[j].SubjectID
	})
	got, err := json.MarshalIndent(struct {
		ConversationID string           `json:"conversation_id"`
		Kind           ConversationKind `json:"kind"`
		Name           string           `json:"name"`
		Participants   []participant    `json:"participants"`
	}{group.ID, group.Kind, group.Name, participants}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join("testdata", "chat_016_private_group.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("group projection differs from golden\n got: %s\nwant: %s", got, want)
	}
}

type chat016Store struct {
	*fakeStore
	removedHome     string
	removedSubject  string
	removedRevision uint64
}

func (s *chat016Store) RemoveMembership(_ context.Context, _ Principal, _, _, home, subject string, revision uint64) (Membership, error) {
	s.removedHome, s.removedSubject, s.removedRevision = home, subject, revision
	s.mutations++
	return Membership{TenantID: "t1", HomeTenantID: home, ConversationID: "group-016", SubjectID: subject, LeftAt: timePtr(time.Unix(100, 0)), Revision: revision + 1}, nil
}
