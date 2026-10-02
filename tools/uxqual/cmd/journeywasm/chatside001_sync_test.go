package main

import (
	"io"
	"strconv"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

type chatside001Feed struct {
	messages []*chatv1.WatchConversationResponse
}

func (f *chatside001Feed) Recv() (*chatv1.WatchConversationResponse, error) {
	if len(f.messages) == 0 {
		return nil, io.EOF
	}
	next := f.messages[0]
	f.messages = f.messages[1:]
	return next, nil
}

func chatside001Notice(revision uint64) *chatv1.WatchConversationResponse {
	return &chatv1.WatchConversationResponse{Event: &chatv1.ConversationEvent{}, EphemeralDelivery: &chatv1.EphemeralDelivery{Id: chatrecipient.SidebarChangedDeliveryID, Body: strconv.FormatUint(revision, 10), OnlyVisibleToYou: true}}
}

// chatside001Tab is one tab's side of the exchange: the revision of the layout
// it holds, whether a change of its own is still on its way, and how many times
// it read the layout again.
type chatside001Tab struct {
	revision uint64
	pending  bool
	reads    int
}

// hear is what the tab does with a notice (chatside001SidebarAnnounced): read
// the layout again and then hold the server's revision.
func (tab *chatside001Tab) hear(revision uint64) {
	if chatside001ShouldReread(tab.revision, revision, tab.pending) {
		tab.reads++
		tab.revision = revision
	}
}

func chatside001Drain(t *testing.T, tab *chatside001Tab, feed *chatside001Feed) []*chatv1.WatchConversationResponse {
	t.Helper()
	stream := &chatside001SignalStream{next: feed, onSignal: tab.hear}
	var passed []*chatv1.WatchConversationResponse
	for {
		message, err := stream.Recv()
		if err != nil {
			return passed
		}
		passed = append(passed, message)
	}
}

// TestTodo_CHATSIDE_001_SecondTab proves two tabs: the tab that saved revision 8
// ignores its own notice, the other tab reads once, a repeated or older notice
// reads nothing, and a tab with a change of its own still unsaved waits.
func TestTodo_CHATSIDE_001_SecondTab(t *testing.T) {
	saver := &chatside001Tab{revision: 8}
	other := &chatside001Tab{revision: 7}
	busy := &chatside001Tab{revision: 7, pending: true}

	for name, tab := range map[string]*chatside001Tab{"saver": saver, "other": other, "busy": busy} {
		passed := chatside001Drain(t, tab, &chatside001Feed{messages: []*chatv1.WatchConversationResponse{chatside001Notice(8), chatside001Notice(8), chatside001Notice(6)}})
		if len(passed) != 3 {
			t.Fatalf("%s: the notices were not passed on to the conversation drain: %d messages", name, len(passed))
		}
	}
	if saver.reads != 0 {
		t.Fatalf("the tab that saved the layout read it again %d times", saver.reads)
	}
	if other.reads != 1 || other.revision != 8 {
		t.Fatalf("second tab read %d times and holds revision %d, want one read and revision 8", other.reads, other.revision)
	}
	if busy.reads != 0 || busy.revision != 7 {
		t.Fatalf("a tab with an unsaved change of its own read %d times", busy.reads)
	}
}

func TestTodo_CHATSIDE_001_SignalIsOnlyTheSidebarNotice(t *testing.T) {
	cases := []*chatv1.EphemeralDelivery{
		nil,
		{Id: "reply-1", ThreadId: "t", Body: "8", OnlyVisibleToYou: true},
		{Id: chatrecipient.SidebarChangedDeliveryID, Body: "soon"},
		{Id: chatrecipient.SidebarChangedDeliveryID, Body: "0"},
	}
	for _, delivery := range cases {
		if revision, ok := chatside001SignalRevision(delivery); ok {
			t.Fatalf("%+v was taken as a layout notice for revision %d", delivery, revision)
		}
	}
	if revision, ok := chatside001SignalRevision(chatside001Notice(12).GetEphemeralDelivery()); !ok || revision != 12 {
		t.Fatalf("notice = %d, %v", revision, ok)
	}
	// A message that is not a notice reaches the drain untouched and calls nothing.
	called := false
	stream := &chatside001SignalStream{next: &chatside001Feed{messages: []*chatv1.WatchConversationResponse{{Event: &chatv1.ConversationEvent{Sequence: 3}}}}, onSignal: func(uint64) { called = true }}
	if message, err := stream.Recv(); err != nil || message.GetEvent().GetSequence() != 3 || called {
		t.Fatalf("plain event = %+v, %v, called=%v", message, err, called)
	}
}
