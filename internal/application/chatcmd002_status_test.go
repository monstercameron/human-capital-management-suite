package application

import (
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// TestTodo_CHATCMD_002_ChannelWidgetStatus: the channel's own poll and to-do
// list obey the channel's status on the server. Open takes every change; an
// announcements-only channel takes a vote and a tick from a member and refuses
// everything else; a locked or archived channel refuses all of them and still
// reads. The really composed chat runtime answers, not a double.
func TestTodo_CHATCMD_002_ChannelWidgetStatus(t *testing.T) {
	h := newModHarness(t)
	bob, carol, alice := h.ctx("bob"), h.ctx("carol"), h.ctx("alice")
	ext := h.dep.extensions

	if _, err := ext.MutateChannelTodo(bob, h.principal("bob"), "host", h.room, 1, chatcore.ChannelTodoMutation{Operation: "ADD", Text: "Book the room"}); err != nil {
		t.Fatalf("an open channel refused a to-do item: %v", err)
	}
	list, err := ext.ChannelTodo(bob, h.principal("bob"), "host", h.room)
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("list %+v %v", list, err)
	}
	item := list.Items[0].ID
	if _, err := ext.MutateChannelPoll(bob, h.principal("bob"), "host", h.room, 1, chatcore.ChannelPollMutation{Operation: "CREATE", Question: "Lunch?", Options: []string{"Pizza", "Pho"}}); err != nil {
		t.Fatalf("an open channel refused a poll: %v", err)
	}
	poll, err := ext.ChannelPoll(bob, h.principal("bob"), "host", h.room)
	if err != nil || len(poll.Options) != 2 {
		t.Fatalf("poll %+v %v", poll, err)
	}

	setStatus := func(status chatpolicy.ChannelStatus) {
		t.Helper()
		current, err := h.dep.status.GetChannelStatus(alice, chatcore.GetConversationRequest{Principal: h.principal("alice"), TenantID: "host", ConversationID: h.room})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.dep.status.ChangeChannelStatus(alice, chatcore.ChangeChannelStatusRequest{Principal: h.principal("alice"), TenantID: "host", ConversationID: h.room, Status: status, ExpectedRevision: current.Revision, Reason: "test"}); err != nil {
			t.Fatalf("change to %s: %v", status, err)
		}
	}
	revisions := func() (todo, poll uint64) {
		t.Helper()
		l, err := ext.ChannelTodo(carol, h.principal("carol"), "host", h.room)
		if err != nil {
			t.Fatalf("a channel that is not open must still be read: %v", err)
		}
		p, err := ext.ChannelPoll(carol, h.principal("carol"), "host", h.room)
		if err != nil {
			t.Fatalf("a channel that is not open must still be read: %v", err)
		}
		return l.Revision, p.Revision
	}
	tick := func(done bool) error {
		todo, _ := revisions()
		_, err := ext.MutateChannelTodo(carol, h.principal("carol"), "host", h.room, todo, chatcore.ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: item, Completed: done})
		return err
	}
	add := func() error {
		todo, _ := revisions()
		_, err := ext.MutateChannelTodo(carol, h.principal("carol"), "host", h.room, todo, chatcore.ChannelTodoMutation{Operation: "ADD", Text: "One more"})
		return err
	}
	vote := func(option string) error {
		_, revision := revisions()
		_, err := ext.MutateChannelPoll(carol, h.principal("carol"), "host", h.room, revision, chatcore.ChannelPollMutation{Operation: "VOTE", OptionID: option})
		return err
	}
	closePoll := func() error {
		_, revision := revisions()
		_, err := ext.MutateChannelPoll(bob, h.principal("bob"), "host", h.room, revision, chatcore.ChannelPollMutation{Operation: "CLOSE"})
		return err
	}
	denied := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, chatcore.ErrChannelStatus) {
			t.Errorf("%s: %v, want the channel-status refusal", what, err)
		}
	}

	if err := tick(true); err != nil {
		t.Fatalf("an open channel refused a tick by a member: %v", err)
	}
	if err := vote(poll.Options[0].ID); err != nil {
		t.Fatalf("an open channel refused a vote by a member: %v", err)
	}
	setStatus(chatpolicy.StatusAnnouncements)
	denied("adding an item to an announcements-only channel", add())
	denied("closing the poll of an announcements-only channel", closePoll())
	if err := tick(false); err != nil {
		t.Errorf("a tick is a reaction and an announcements-only channel allows it: %v", err)
	}
	if err := vote(poll.Options[1].ID); err != nil {
		t.Errorf("a vote is a reaction and an announcements-only channel allows it: %v", err)
	}

	// Archived is the same table row as Locked for these actions; a channel under
	// a record hold cannot be archived around the hold, so it is not driven here.
	for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusLocked} {
		setStatus(status)
		denied(string(status)+": a tick", tick(false))
		denied(string(status)+": a vote", vote(poll.Options[0].ID))
		denied(string(status)+": an added item", add())
		denied(string(status)+": closing the poll", closePoll())
		revisions()
	}

	setStatus(chatpolicy.StatusOpen)
	if err := tick(false); err != nil {
		t.Errorf("a reopened channel takes ticks again: %v", err)
	}
}
