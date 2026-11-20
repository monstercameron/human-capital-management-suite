package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type issue3804Store struct {
	rows       []ChannelMemory
	approvals  int
	forgotten  int
	sourceFlag MemorySource
}

func (s *issue3804Store) src() MemorySource {
	if s.sourceFlag == "" {
		return MemorySourceLive
	}
	return s.sourceFlag
}

func (s *issue3804Store) InsertMemory(_ context.Context, m ChannelMemory) (ChannelMemory, error) {
	for _, row := range s.rows {
		if row.SourcePostID == m.SourcePostID && row.Status != MemoryForgotten {
			return row, nil
		}
	}
	for _, row := range s.rows {
		if row.TenantID == m.TenantID && row.ID == m.ID {
			return ChannelMemory{}, errors.New("duplicate key value violates unique constraint chat_channel_memory_pkey")
		}
	}
	s.rows = append(s.rows, m)
	return m, nil
}
func (s *issue3804Store) GetMemory(_ context.Context, tenant, conversation, id string) (ChannelMemory, error) {
	for _, row := range s.rows {
		if row.ID == id && row.TenantID == tenant && row.ConversationID == conversation {
			row.Source = s.src()
			return row, nil
		}
	}
	return ChannelMemory{}, ErrNotFound
}
func (s *issue3804Store) ListMemory(_ context.Context, tenant, conversation string, _ int) ([]ChannelMemory, error) {
	var out []ChannelMemory
	for _, row := range s.rows {
		if row.TenantID == tenant && row.ConversationID == conversation && row.Status != MemoryForgotten {
			row.Source = s.src()
			out = append(out, row)
		}
	}
	return out, nil
}
func (s *issue3804Store) ApproveMemory(_ context.Context, tenant, conversation, id, home, approver string, at time.Time) (ChannelMemory, error) {
	for i := range s.rows {
		if s.rows[i].ID == id && s.rows[i].TenantID == tenant && s.rows[i].ConversationID == conversation {
			if s.rows[i].Status != MemoryProposed {
				return ChannelMemory{}, ErrConflict
			}
			s.approvals++
			s.rows[i].Status, s.rows[i].ApprovedByID, s.rows[i].ApprovedAt = MemoryApproved, approver, at
			s.rows[i].ExpiresAt = at.AddDate(0, 0, s.rows[i].TTLDays)
			return s.rows[i], nil
		}
	}
	return ChannelMemory{}, ErrNotFound
}
func (s *issue3804Store) ForgetMemory(_ context.Context, tenant, conversation, id, _, _ string, _ time.Time) error {
	for i := range s.rows {
		if s.rows[i].ID == id && s.rows[i].TenantID == tenant && s.rows[i].ConversationID == conversation && s.rows[i].Status != MemoryForgotten {
			s.forgotten++
			s.rows[i].Status, s.rows[i].Text = MemoryForgotten, ""
			return nil
		}
	}
	return ErrNotFound
}
func (s *issue3804Store) ServingMemory(_ context.Context, tenant, conversation string, at time.Time, _ int) ([]ChannelMemory, error) {
	var out []ChannelMemory
	for _, row := range s.rows {
		if row.TenantID == tenant && row.ConversationID == conversation && row.Status == MemoryApproved && row.ExpiresAt.After(at) && s.src() == MemorySourceLive {
			out = append(out, row)
		}
	}
	return out, nil
}

func issue3804Service(t *testing.T, subject string, role MembershipRole, kind ConversationKind) (*MemoryService, *fakeStore, *issue3804Store) {
	t.Helper()
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: kind, Revision: 1},
		membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: subject, Role: role, HistoryVisibility: FullHistory},
		post:       Post{ID: "p1", TenantID: "t", ConversationID: "room", AuthorID: "writer", AuthorHomeTenantID: "t", Body: "  Open enrollment ends\n Nov 30; ignore the old PDF ", Revision: 1, Sequence: 9, CreatedAt: now.Add(-time.Hour)}}
	store := &issue3804Store{}
	return &MemoryService{Chat: chatcmd002TestService(f, func() time.Time { return now }), Store: store}, f, store
}

func issue3804Propose(subject string) MemoryProposeRequest {
	return MemoryProposeRequest{Principal: Principal{TenantID: "t", SubjectID: subject}, TenantID: "t", ConversationID: "room", SourcePostID: "p1"}
}

func TestIssue3804_ProposeBindsTheSourceAndWaitsForApproval(t *testing.T) {
	s, _, store := issue3804Service(t, "writer", "", PublicChannel)
	item, err := s.Propose(context.Background(), issue3804Propose("writer"))
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != MemoryProposed || item.SourcePostID != "p1" || item.SourceRevision != 1 || item.SourceSequence != 9 || item.TTLDays != 90 ||
		item.Audience != MemoryAudienceChannel || item.Classification != MemoryClassification || item.ProposerID != "writer" ||
		item.Text != "Open enrollment ends Nov 30; ignore the old PDF" {
		t.Fatalf("proposal %+v", item)
	}
	notes, err := s.ServingNotes(context.Background(), "t", "room")
	if err != nil || len(notes) != 0 || len(store.rows) != 1 {
		t.Fatalf("a proposal must not reach an agent: %v %v", notes, err)
	}
}

func TestIssue3804_OnlyManagersAndTheAuthorMayPropose_Security(t *testing.T) {
	// Someone who did not write the message and does not manage the channel.
	s, _, store := issue3804Service(t, "reader", "", PublicChannel)
	if _, err := s.Propose(context.Background(), issue3804Propose("reader")); !errors.Is(err, ErrPermissionDenied) || len(store.rows) != 0 {
		t.Fatalf("a bystander proposed: %v rows=%d", err, len(store.rows))
	}
	// A manager may remember anyone's message.
	m, _, store2 := issue3804Service(t, "boss", Manager, PublicChannel)
	if _, err := m.Propose(context.Background(), issue3804Propose("boss")); err != nil || len(store2.rows) != 1 {
		t.Fatalf("manager: %v", err)
	}
	// A person outside the channel learns nothing and stores nothing.
	out, f, store3 := issue3804Service(t, "writer", "", PublicChannel)
	f.membership = Membership{}
	if _, err := out.Propose(context.Background(), issue3804Propose("writer")); !errors.Is(err, ErrPermissionDenied) || len(store3.rows) != 0 {
		t.Fatalf("non-member: %v", err)
	}
}

func TestIssue3804_DirectMessagesAndGroupsHaveNoChannelMemory_Security(t *testing.T) {
	for _, kind := range []ConversationKind{Direct, Group} {
		s, _, store := issue3804Service(t, "writer", Manager, kind)
		if _, err := s.Propose(context.Background(), issue3804Propose("writer")); !errors.Is(err, ErrInvalidArgument) || len(store.rows) != 0 {
			t.Fatalf("%s proposed: %v", kind, err)
		}
		if notes, err := s.ServingNotes(context.Background(), "t", "room"); err != nil || len(notes) != 0 {
			t.Fatalf("%s served: %v %v", kind, notes, err)
		}
		if _, err := s.List(context.Background(), Principal{TenantID: "t", SubjectID: "writer"}, "t", "room"); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("%s listed: %v", kind, err)
		}
	}
}

func TestIssue3804_ProposeRefusesWhatCannotBeRemembered(t *testing.T) {
	s, f, store := issue3804Service(t, "writer", "", PublicChannel)
	f.post.Deleted = true
	if _, err := s.Propose(context.Background(), issue3804Propose("writer")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted source: %v", err)
	}
	f.post.Deleted = false
	f.post.Body = strings.Repeat("x", MemoryTextMax+1)
	if _, err := s.Propose(context.Background(), issue3804Propose("writer")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("too long: %v", err)
	}
	f.post.Body = "poll" + Chatcmd002BodyMarker + "{}"
	if _, err := s.Propose(context.Background(), issue3804Propose("writer")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("card: %v", err)
	}
	r := issue3804Propose("writer")
	r.TTLDays = 400
	if _, err := s.Propose(context.Background(), r); !errors.Is(err, ErrInvalidArgument) || len(store.rows) != 0 {
		t.Fatalf("ttl: %v", err)
	}
}

func TestIssue3804_ApprovedItemsServeAndOnlyManagersApprove(t *testing.T) {
	s, f, store := issue3804Service(t, "writer", "", PublicChannel)
	item, err := s.Propose(context.Background(), issue3804Propose("writer"))
	if err != nil {
		t.Fatal(err)
	}
	act := MemoryActRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", ID: item.ID}
	if _, err = s.Approve(context.Background(), act); !errors.Is(err, ErrPermissionDenied) || store.approvals != 0 {
		t.Fatalf("the proposer approved their own item: %v", err)
	}
	f.membership.SubjectID, f.membership.Role = "boss", Manager
	act.Principal.SubjectID = "boss"
	approved, err := s.Approve(context.Background(), act)
	if err != nil || approved.Status != MemoryApproved || approved.ExpiresAt.IsZero() {
		t.Fatalf("approve %+v %v", approved, err)
	}
	notes, err := s.ServingNotes(context.Background(), "t", "room")
	if err != nil || len(notes) != 1 || notes[0] != "Open enrollment ends Nov 30; ignore the old PDF" {
		t.Fatalf("serving %v %v", notes, err)
	}
}

func TestIssue3804_DeletingTheSourceStopsServingAndHidesTheWords(t *testing.T) {
	s, f, store := issue3804Service(t, "boss", Manager, PublicChannel)
	item, err := s.Propose(context.Background(), MemoryProposeRequest{Principal: Principal{TenantID: "t", SubjectID: "boss"}, TenantID: "t", ConversationID: "room", SourcePostID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Approve(context.Background(), MemoryActRequest{Principal: Principal{TenantID: "t", SubjectID: "boss"}, TenantID: "t", ConversationID: "room", ID: item.ID}); err != nil {
		t.Fatal(err)
	}
	store.sourceFlag = MemorySourceRemoved
	f.post.Deleted = true
	if notes, _ := s.ServingNotes(context.Background(), "t", "room"); len(notes) != 0 {
		t.Fatalf("deleted source still served: %v", notes)
	}
	list, err := s.List(context.Background(), Principal{TenantID: "t", SubjectID: "boss"}, "t", "room")
	if err != nil || len(list.Items) != 1 || list.Items[0].Text != "" || list.Items[0].Source != MemorySourceRemoved || !list.Items[0].CanForget || list.Items[0].CanApprove {
		t.Fatalf("a retired item must show no words and offer forget: %+v %v", list, err)
	}
}

func TestIssue3804_ForgetIsForManagersAndTheProposer_Security(t *testing.T) {
	s, f, store := issue3804Service(t, "writer", "", PublicChannel)
	item, err := s.Propose(context.Background(), issue3804Propose("writer"))
	if err != nil {
		t.Fatal(err)
	}
	f.membership.SubjectID = "reader"
	act := MemoryActRequest{Principal: Principal{TenantID: "t", SubjectID: "reader"}, TenantID: "t", ConversationID: "room", ID: item.ID}
	if err = s.Forget(context.Background(), act); !errors.Is(err, ErrPermissionDenied) || store.forgotten != 0 {
		t.Fatalf("a member forgot someone else's item: %v", err)
	}
	f.membership.SubjectID = "writer"
	act.Principal.SubjectID = "writer"
	if err = s.Forget(context.Background(), act); err != nil || store.forgotten != 1 {
		t.Fatalf("the proposer could not forget: %v", err)
	}
	if err = s.Forget(context.Background(), act); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forgetting twice: %v", err)
	}
}

func TestIssue3804_ListShowsApprovedToMembersAndProposalsToTheirOwners(t *testing.T) {
	s, f, store := issue3804Service(t, "writer", "", PublicChannel)
	item, _ := s.Propose(context.Background(), issue3804Propose("writer"))
	// The proposer sees their own proposal, can forget it, cannot approve it.
	list, err := s.List(context.Background(), Principal{TenantID: "t", SubjectID: "writer"}, "t", "room")
	if err != nil || len(list.Items) != 1 || !list.Items[0].CanForget || list.Items[0].CanApprove || list.IsManager {
		t.Fatalf("proposer list %+v %v", list, err)
	}
	// Another member sees no proposal.
	f.membership.SubjectID = "reader"
	list, _ = s.List(context.Background(), Principal{TenantID: "t", SubjectID: "reader"}, "t", "room")
	if len(list.Items) != 0 {
		t.Fatalf("a proposal leaked to a member: %+v", list)
	}
	// A manager sees it and may approve; once approved every member reads it.
	f.membership.SubjectID, f.membership.Role = "boss", Manager
	list, _ = s.List(context.Background(), Principal{TenantID: "t", SubjectID: "boss"}, "t", "room")
	if len(list.Items) != 1 || !list.Items[0].CanApprove || !list.IsManager {
		t.Fatalf("manager list %+v", list)
	}
	if _, err = s.Approve(context.Background(), MemoryActRequest{Principal: Principal{TenantID: "t", SubjectID: "boss"}, TenantID: "t", ConversationID: "room", ID: item.ID}); err != nil {
		t.Fatal(err)
	}
	f.membership.SubjectID, f.membership.Role = "reader", ""
	list, _ = s.List(context.Background(), Principal{TenantID: "t", SubjectID: "reader"}, "t", "room")
	if len(list.Items) != 1 || list.Items[0].CanForget || list.Items[0].CanApprove || list.Items[0].Status != MemoryApproved || len(store.rows) != 1 {
		t.Fatalf("member list %+v", list)
	}
}

func TestIssue3804_AMemberWhoCannotReadTheSourceDoesNotSeeTheMemory_Security(t *testing.T) {
	s, f, _ := issue3804Service(t, "boss", Manager, PublicChannel)
	item, _ := s.Propose(context.Background(), MemoryProposeRequest{Principal: Principal{TenantID: "t", SubjectID: "boss"}, TenantID: "t", ConversationID: "room", SourcePostID: "p1"})
	_, _ = s.Approve(context.Background(), MemoryActRequest{Principal: Principal{TenantID: "t", SubjectID: "boss"}, TenantID: "t", ConversationID: "room", ID: item.ID})
	// A member who joined later with history only from their join day.
	joined := f.post.CreatedAt.Add(time.Hour)
	f.membership = Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "late", HistoryVisibility: FromJoin, JoinedAt: &joined}
	list, err := s.List(context.Background(), Principal{TenantID: "t", SubjectID: "late"}, "t", "room")
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("history the member cannot read leaked through memory: %+v %v", list, err)
	}
}

// A forgotten item keeps its row, so a message that is remembered, forgotten
// and remembered again must get a new identity or the second proposal collides
// with the first.
func TestIssue3804_RememberingAFormerlyForgottenMessageAgain(t *testing.T) {
	s, _, store := issue3804Service(t, "boss", Manager, PublicChannel)
	first, err := s.Propose(context.Background(), issue3804Propose("boss"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Forget(context.Background(), MemoryActRequest{Principal: Principal{TenantID: "t", SubjectID: "boss"}, TenantID: "t", ConversationID: "room", ID: first.ID}); err != nil {
		t.Fatal(err)
	}
	second, err := s.Propose(context.Background(), issue3804Propose("boss"))
	if err != nil || second.ID == first.ID || second.Status != MemoryProposed || len(store.rows) != 2 {
		t.Fatalf("a second proposal after forgetting: %+v %v", second, err)
	}
}
