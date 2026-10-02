package chat

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"testing"
	"time"
)

type chatcmd002FixtureStore struct{ *fakeStore }

func (f chatcmd002FixtureStore) ReadChannelStatus(context.Context, string, string) (ChannelStatus, error) {
	return ChannelStatus{Status: chatpolicy.StatusOpen}, nil
}
func (f chatcmd002FixtureStore) CommitChannelStatus(context.Context, ChangeChannelStatusRequest, time.Time, func(context.Context, ChannelStatus) error) (ChannelStatus, error) {
	return ChannelStatus{}, ErrUnavailable
}
func (f chatcmd002FixtureStore) SweepChannelStatuses(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

type chatcmd002FixtureAuthority struct{ verifiedAuthority }

func (f chatcmd002FixtureAuthority) ChannelStatusPermissions(context.Context, Principal, Conversation, time.Time) (chatpolicy.StatusPermissions, error) {
	return chatpolicy.StatusPermissions{}, nil
}
func chatcmd002TestService(f *fakeStore, clock Clock) *Service {
	s := NewService(chatcmd002FixtureStore{f}, clock)
	s.SetAuthority(chatcmd002FixtureAuthority{verifiedAuthority{store: f}})
	return s
}

type chatcmd002FixtureRepository struct {
	read   func(context.Context, Chatcmd002Request, func(context.Context) error) (Chatcmd002View, error)
	mutate func(context.Context, Chatcmd002Request, func(context.Context) error) (Post, error)
}

func (f chatcmd002FixtureRepository) Chatcmd002Read(ctx context.Context, r Chatcmd002Request, a func(context.Context) error) (Chatcmd002View, error) {
	if f.read != nil {
		return f.read(ctx, r, a)
	}
	return Chatcmd002View{}, a(ctx)
}
func (f chatcmd002FixtureRepository) Chatcmd002Mutate(ctx context.Context, r Chatcmd002Request, a func(context.Context) error) (Post, error) {
	if f.mutate != nil {
		return f.mutate(ctx, r, a)
	}
	return Post{}, a(ctx)
}
func TestTodo_CHATCMD_002_Fault(t *testing.T) {
	now := time.Now()
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	s := &Chatcmd002Service{Chat: chatcmd002TestService(f, func() time.Time { return now }), Repository: chatcmd002FixtureRepository{}}
	d, _ := Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, now, nil)
	d.Card.Poll.Options[0].Count = 99
	r := Chatcmd002PostRequest{SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "accepted-card"}, Card: d.Card}
	if _, err := s.Post(context.Background(), r); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatal("unaccepted preview posted")
	}
	r.Accepted = true
	if _, err := s.Post(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	first := f.sent.Body
	card, ok := Chatcmd002Decode(first)
	if !ok || card.Poll.Options[0].Count != 0 || card.Poll.Options[0].ID == "" {
		t.Fatalf("forged counts or unbound option %+v", card)
	}
	now = now.Add(time.Hour)
	if _, err := s.Post(context.Background(), r); err != nil || f.sent.Body != first {
		t.Fatalf("retry changed body %v", err)
	}
}
func chatcmd004PostService(t *testing.T) {
	now := time.Now()
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	s := &Chatcmd002Service{Chat: chatcmd002TestService(f, func() time.Time { return now }), Repository: chatcmd002FixtureRepository{}}
	d, _ := Chatcmd004ParseTodo(`"Launch" 1="Book the room"`, nil, "writer", now, nil)
	r := Chatcmd002PostRequest{Accepted: true, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "list"}, Card: d.Card}
	if _, err := s.Post(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	card, ok := Chatcmd002Decode(f.sent.Body)
	if !ok || card.Todo.Items[0].CreatedBy != "writer" || card.Todo.Items[0].CreatedByHomeTenantID != "t" || card.Todo.Items[0].Completed || card.Todo.Items[0].ID == "" {
		t.Fatalf("task %+v", card)
	}
	now = now.Add(time.Hour)
	first := f.sent.Body
	if _, err := s.Post(context.Background(), r); err != nil || f.sent.Body != first {
		t.Fatalf("list retry changed %v", err)
	}
}
func chatcmd004ServiceSecurity(t *testing.T) {
	now := time.Now()
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PrivateChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	service := &Chatcmd002Service{Chat: chatcmd002TestService(f, func() time.Time { return now }), Repository: chatcmd002FixtureRepository{}}
	d, _ := Chatcmd004ParseTodo(`"Launch" 1="Task"`, nil, "writer", now, nil)
	d.Card.Todo.Items[0].AssigneeID = "outside"
	d.Card.Todo.Items[0].AssigneeHomeTenantID = "t"
	if _, err := service.Post(context.Background(), Chatcmd002PostRequest{Accepted: true, Card: d.Card, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "list"}}); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("outside assignment %v writes %d", err, f.mutations)
	}
	f.post = Post{ID: "p", TenantID: "t", ConversationID: "room", CreatedAt: now}
	request := Chatcmd002Request{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", PostID: "p", ExpectedRevision: 1, Mutation: Chatcmd002Mutation{Operation: "TICK"}}
	touched := false
	service.Repository = chatcmd002FixtureRepository{read: func(ctx context.Context, _ Chatcmd002Request, authorize func(context.Context) error) (Chatcmd002View, error) {
		touched = true
		left := now
		f.membership.LeftAt = &left
		return Chatcmd002View{}, authorize(ctx)
	}}
	if _, err := service.Read(context.Background(), request); !errors.Is(err, ErrPermissionDenied) || !touched {
		t.Fatalf("revocation not revalidated %v touched %v", err, touched)
	}
	f.membership.LeftAt = nil
	request.Principal.SubjectID = "outsider"
	touched = false
	if _, err := service.Mutate(context.Background(), request); !errors.Is(err, ErrPermissionDenied) || touched {
		t.Fatal("outsider reached mutation")
	}
}
