package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestIssue3923_AssigneeOnlyListNeedsAnAssigneeForEveryItem_Security: a list
// that only the assignee may complete is refused by the server when an item has
// no assignee, whatever the page sent, and nothing is written; the same list is
// accepted once every item has one or when anyone may complete items.
func TestIssue3923_AssigneeOnlyListNeedsAnAssigneeForEveryItem_Security(t *testing.T) {
	now := time.Now()
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	s := &Chatcmd002Service{Chat: chatcmd002TestService(f, func() time.Time { return now }), Repository: chatcmd002FixtureRepository{}}
	d, err := Chatcmd004ParseTodo(`"Launch" 1="Book the room" 2="Send invites" tick=assignee`, nil, "writer", now, nil)
	if err != nil || d.Card.Todo.Tick != "assignee" || len(d.Card.Todo.Items) != 2 {
		t.Fatalf("%+v %v", d.Card.Todo, err)
	}
	post := func(card Chatcmd002Card, key string) error {
		_, err := s.Post(context.Background(), Chatcmd002PostRequest{Accepted: true, Card: card, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: key}})
		return err
	}
	if err := post(d.Card, "none"); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("no assignee at all: %v writes %d", err, f.mutations)
	}
	// One of two items assigned is still refused.
	d.Card.Todo.Items[0].AssigneeID, d.Card.Todo.Items[0].AssigneeHomeTenantID = "writer", "t"
	if err := post(d.Card, "one"); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("one of two assigned: %v writes %d", err, f.mutations)
	}
	if gap := d.Card.AssigneeOnlyGap(); gap != 1 {
		t.Fatalf("gap %d", gap)
	}
	// Anyone may complete: the same unassigned item is fine.
	anyone := chatcmd003Clone(d.Card)
	anyone.Todo.Tick = "anyone"
	if err := post(anyone, "anyone"); err != nil {
		t.Fatalf("anyone-completes list refused: %v", err)
	}
	// Every item assigned to a member: accepted.
	d.Card.Todo.Items[1].AssigneeID, d.Card.Todo.Items[1].AssigneeHomeTenantID = "writer", "t"
	if gap := d.Card.AssigneeOnlyGap(); gap != -1 {
		t.Fatalf("gap %d", gap)
	}
	if err := post(d.Card, "all"); err != nil {
		t.Fatalf("fully assigned list refused: %v", err)
	}
}

// TestIssue3923_CardsAlreadyPostedStillRead: the rule is for new lists; a card
// stored before it, with an unassigned item under the assignee policy, still
// decodes so it does not turn into plain text.
func TestIssue3923_CardsAlreadyPostedStillRead(t *testing.T) {
	d, _ := Chatcmd004ParseTodo(`"Launch" 1="Book the room" tick=assignee`, nil, "writer", time.Now(), nil)
	body, err := d.Card.Body()
	if err != nil {
		t.Fatalf("Body must not apply the creation rule: %v", err)
	}
	if card, ok := Chatcmd002Decode(body); !ok || card.Todo.Tick != "assignee" {
		t.Fatalf("legacy card no longer reads: %+v %v", card, ok)
	}
}
