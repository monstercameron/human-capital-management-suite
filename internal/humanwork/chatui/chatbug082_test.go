package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func chatbug082Model(status chatpolicy.ChannelStatus) Model {
	m := chatbug074Model()
	m.ChannelTeam = ChannelTeamWidget{Revision: 3, Purpose: "Scratch space for QA", CanPin: true}
	m.ChannelProject = ChannelProjectWidget{Revision: 2, CanPin: true}
	m.ChannelPoll = ChannelPoll{Revision: 4, Question: "Lunch spot?", Options: []ChannelPollOption{{ID: "a", Text: "Tacos"}, {ID: "b", Text: "Pho"}}}
	m.Callbacks.VoteChannelPoll = func(string) {}
	m.Callbacks.SetChannelTeamPurpose = func(string) {}
	m.Callbacks.SetChannelWidgetPinned = func(string, bool) {}
	m.Chatcmd002.CloseChannelPoll = func() {}
	m.ChannelStatuses = map[string]ChannelStatusView{"room": {Status: chat.ChannelStatus{ConversationID: "room", Status: status}}}
	return m
}

// TestTodo_CHATBUG_082: an archived channel said its widgets could not be
// loaded, twice, and lost its purpose. The read is no longer refused (the
// store test of the same name); here the page shows what the channel held,
// read-only, and a failed read is said once.
func TestTodo_CHATBUG_082(t *testing.T) {
	open := chatbug082Model(chatpolicy.StatusOpen)
	if ChannelWidgetsReadOnly(open) {
		t.Fatal("an open channel is read-only")
	}
	if kept := WithReadOnlyChannelWidgets(open); kept.Callbacks.AddChannelTodo == nil || kept.Callbacks.SetChannelTeamPurpose == nil || kept.Chatcmd002.CloseChannelPoll == nil {
		t.Fatal("an open channel lost its controls")
	}
	for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusArchived, chatpolicy.StatusLocked, chatpolicy.StatusAnnouncements} {
		m := WithReadOnlyChannelWidgets(chatbug082Model(status))
		if !ChannelWidgetsReadOnly(m) {
			t.Fatalf("%s: not read-only", status)
		}
		// The purpose is still there, and shown as the header's line.
		if got := chatux001Purpose(m, m.selected()); got != "Scratch space for QA" {
			t.Fatalf("%s: purpose %q", status, got)
		}
		parts := channelWidgetPartsFor(m, handlers{})
		purpose := renderNode(t, parts.Purpose)
		if !strings.Contains(purpose, "Scratch space for QA") || !parts.Disabled || !strings.Contains(purpose, "disabled") {
			t.Fatalf("%s: purpose editor %s", status, purpose)
		}
		// The list and the poll are readable and nothing in them can be pressed.
		todo := renderNode(t, channelTodoSection(m, handlers{}))
		if !strings.Contains(todo, "Order pizza") || !strings.Contains(todo, "Book the room") {
			t.Fatalf("%s: list lost its tasks: %s", status, todo)
		}
		for _, control := range []string{`data-action="todo-toggle"`, `data-action="todo-delete"`, `type="submit"`} {
			if tag := chatbug074Tag(t, todo, control); !strings.Contains(tag, "disabled") {
				t.Errorf("%s: %s can be pressed: %s", status, control, tag)
			}
		}
		poll := renderNode(t, channelPollSection(m, handlers{}))
		if !strings.Contains(poll, "Lunch spot?") || !strings.Contains(poll, "Tacos") || strings.Contains(poll, "poll-close") {
			t.Fatalf("%s: poll %s", status, poll)
		}
		if strings.Contains(poll, `data-action="poll-vote"`) {
			t.Errorf("%s: a standing poll offers a vote: %s", status, poll)
		}
		// No error anywhere: the widgets were read.
		for name, markup := range map[string]string{"above the messages": renderNode(t, inlineChannelWidgets(m)), "the list": todo, "the poll": poll} {
			if strings.Contains(markup, `role="alert"`) || strings.Contains(markup, "Could not") {
				t.Errorf("%s: %s shows an error: %s", status, name, markup)
			}
		}
	}
	// A read that does fail is said once, where the purpose would be (the
	// details panel draws widgetError there), not above the messages as well.
	failed := chatbug082Model(chatpolicy.StatusOpen)
	failed.ChannelWidgetsError = "load"
	failed.Callbacks.RetryChannelWidgets = func() {}
	if above := renderNode(t, inlineChannelWidgets(failed)); strings.Contains(above, `role="alert"`) || strings.Contains(above, "widget-retry") {
		t.Fatalf("the failure is repeated above the messages: %s", above)
	}
	if once := renderNode(t, widgetError(failed)); strings.Count(once, `role="alert"`) != 1 || !strings.Contains(once, `data-action="widget-retry"`) {
		t.Fatalf("the failure is not said in its place: %s", once)
	}
	// A pinned widget still shows above the messages when the read worked.
	pinned := chatbug082Model(chatpolicy.StatusArchived)
	pinned.ChannelTeam.Pinned = true
	if above := renderNode(t, inlineChannelWidgets(pinned)); !strings.Contains(above, "Scratch space for QA") {
		t.Fatalf("pinned purpose lost: %s", above)
	}
}
