package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// chatcmd002StatusStore reports a chosen channel status where the fixture
// store reports an open channel.
type chatcmd002StatusStore struct {
	chatcmd002FixtureStore
	status chatpolicy.ChannelStatus
}

func (f chatcmd002StatusStore) ReadChannelStatus(context.Context, string, string) (ChannelStatus, error) {
	return ChannelStatus{Status: f.status}, nil
}

func chatcmd002MarkerFixture(status chatpolicy.ChannelStatus, now time.Time) (*fakeStore, *Service, *Chatcmd002Service) {
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	core := NewService(chatcmd002StatusStore{chatcmd002FixtureStore{f}, status}, func() time.Time { return now })
	core.SetAuthority(chatcmd002FixtureAuthority{verifiedAuthority{store: f}})
	return f, core, &Chatcmd002Service{Chat: core, Repository: chatcmd002FixtureRepository{}}
}

// TestTodo_CHATCMD_002_CardMarker: a body that carries the card marker is
// written only by the card service; a hand-written one is refused on send and
// on edit, and a card's own message is not edited as text.
func TestTodo_CHATCMD_002_CardMarker(t *testing.T) {
	now := time.Now()
	f, core, cards := chatcmd002MarkerFixture(chatpolicy.StatusOpen, now)
	writer := Principal{TenantID: "t", SubjectID: "writer"}
	forged := "Lunch?" + Chatcmd002BodyMarker + `{"kind":"poll"}`
	if _, err := core.SendPost(context.Background(), SendPostRequest{Principal: writer, TenantID: "t", ConversationID: "room", IdempotencyKey: "forged", Body: forged}); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("a hand-written card was sent: %v, %d writes", err, f.mutations)
	}
	if _, err := core.SendPostWithReferences(context.Background(), SendPostWithReferencesRequest{SendPostRequest: SendPostRequest{Principal: writer, TenantID: "t", ConversationID: "room", IdempotencyKey: "forged2", Body: forged}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("a hand-written card was sent with references: %v", err)
	}
	f.post = Post{ID: "p", TenantID: "t", ConversationID: "room", AuthorID: "writer", AuthorHomeTenantID: "t", Body: "plain", Revision: 1}
	if _, err := core.EditPost(context.Background(), EditPostRequest{Principal: writer, TenantID: "t", ConversationID: "room", PostID: "p", ExpectedRevision: 1, Body: forged}); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("an edit wrote a card body: %v, %d writes", err, f.mutations)
	}
	if _, err := core.EditPost(context.Background(), EditPostRequest{Principal: writer, TenantID: "t", ConversationID: "room", PostID: "p", ExpectedRevision: 1, Body: "plain, edited"}); err != nil {
		t.Fatalf("an ordinary edit was refused: %v", err)
	}
	// The card service still posts its own body.
	draft, _ := Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, now, nil)
	before := f.mutations
	if _, err := cards.Post(context.Background(), Chatcmd002PostRequest{Accepted: true, Card: draft.Card, SendPostRequest: SendPostRequest{Principal: writer, TenantID: "t", ConversationID: "room", IdempotencyKey: "card"}}); err != nil || f.mutations != before+1 {
		t.Fatalf("the card service was refused: %v", err)
	}
	// And the card it wrote is not editable as text.
	f.post = Post{ID: "p", TenantID: "t", ConversationID: "room", AuthorID: "writer", AuthorHomeTenantID: "t", Body: f.sent.Body, Revision: 1}
	if _, err := core.EditPost(context.Background(), EditPostRequest{Principal: writer, TenantID: "t", ConversationID: "room", PostID: "p", ExpectedRevision: 1, Body: "now text"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("a card was edited as text: %v", err)
	}
}

// TestTodo_CHATCMD_002_VoteInAnnouncements: members of an announcements-only
// channel may vote, because a vote is not a post; posting a card still needs
// the announcement permission, and a locked channel refuses the vote.
func TestTodo_CHATCMD_002_VoteInAnnouncements(t *testing.T) {
	now := time.Now()
	vote := func(cards *Chatcmd002Service) error {
		_, err := cards.Mutate(context.Background(), Chatcmd002Request{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", PostID: "p", ExpectedRevision: 1, Mutation: Chatcmd002Mutation{Operation: "VOTE", Options: []string{"a"}}})
		return err
	}
	tick := func(cards *Chatcmd002Service) error {
		_, err := cards.Mutate(context.Background(), Chatcmd002Request{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", PostID: "p", ExpectedRevision: 1, Mutation: Chatcmd002Mutation{Operation: "TICK", ItemID: "i", Completed: true}})
		return err
	}
	for _, c := range []struct {
		status    chatpolicy.ChannelStatus
		voteOK    bool
		postOK    bool
		whatToSay string
	}{
		{chatpolicy.StatusOpen, true, true, "open"},
		{chatpolicy.StatusAnnouncements, true, false, "announcements"},
		{chatpolicy.StatusLocked, false, false, "locked"},
	} {
		f, _, cards := chatcmd002MarkerFixture(c.status, now)
		f.post = Post{ID: "p", TenantID: "t", ConversationID: "room", CreatedAt: now}
		if err := vote(cards); (err == nil) != c.voteOK {
			t.Errorf("%s: vote error %v, want ok=%v", c.whatToSay, err, c.voteOK)
		}
		if c.status == chatpolicy.StatusAnnouncements {
			if err := tick(cards); !errors.Is(err, ErrChannelStatus) {
				t.Errorf("%s: a tick is an edit of the list, got %v", c.whatToSay, err)
			}
		}
		draft, _ := Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, now, nil)
		_, err := cards.Post(context.Background(), Chatcmd002PostRequest{Accepted: true, Card: draft.Card, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "card-" + c.whatToSay}})
		if (err == nil) != c.postOK {
			t.Errorf("%s: posting a card error %v, want ok=%v", c.whatToSay, err, c.postOK)
		}
	}
}
