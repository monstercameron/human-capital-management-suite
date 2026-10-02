package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// A channel's status change reaches the other members' event stream. It used to
// be written to the outbox and dropped by the stream reader, so a second open
// page only learned of a lock by polling, and the hole it left in the
// conversation's event numbers read as a gap.
func TestTodo_CHATBUG_075_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "c-1", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", Name: "general", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "u-1", Role: "manager", State: "active"}, {MemberID: "u-2", Role: "member", State: "active"}})
	adapter := NewAdapter(s)
	ctx := context.Background()
	watcher := chat.WatchConversationRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "u-2"}, TenantID: "tenant-a", ConversationID: "c-1"}

	before, err := adapter.ReadConversationEvents(ctx, watcher, 0, 100)
	if err != nil || len(before.Events) != 0 {
		t.Fatalf("events before any change = %+v %v", before.Events, err)
	}
	allow := func(context.Context, chat.ChannelStatus) error { return nil }
	if _, err = s.CommitChannelStatus(ctx, chatstateChange(chatpolicy.StatusLocked, 1), time.Now().UTC(), allow); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitChannelStatus(ctx, chatstateChange(chatpolicy.StatusOpen, 2), time.Now().UTC(), allow); err != nil {
		t.Fatal(err)
	}

	page, err := adapter.ReadConversationEvents(ctx, watcher, before.NextOffset, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("two status changes delivered %d events: %+v", len(page.Events), page.Events)
	}
	for i, delivered := range page.Events {
		event := delivered.Event
		if event.Kind != chat.ConversationUpdated || event.Conversation == nil {
			t.Fatalf("event %d = %+v, want a conversation update carrying the conversation", i, event)
		}
		// The whole conversation is sent, so the page that applies the update
		// keeps the channel's name and kind.
		if got := *event.Conversation; got.ID != "c-1" || got.TenantID != "tenant-a" || got.Kind != chat.PublicChannel || got.Name != "general" || got.Archived {
			t.Fatalf("event %d conversation = %+v", i, got)
		}
		if event.Sequence == 0 || delivered.ResumeCursor == "" {
			t.Fatalf("event %d has no position: %+v", i, delivered)
		}
	}
	if first, second := page.Events[0].Event.Sequence, page.Events[1].Event.Sequence; second != first+1 {
		t.Fatalf("status changes are numbered %d then %d: the page reads that as a gap", first, second)
	}
	if page.Events[0].Event.Revision != 2 || page.Events[1].Event.Revision != 3 {
		t.Fatalf("status revisions = %d, %d, want 2, 3", page.Events[0].Event.Revision, page.Events[1].Event.Revision)
	}

	// Nothing is delivered twice once the watcher has moved past it.
	again, err := adapter.ReadConversationEvents(ctx, watcher, page.NextOffset, 100)
	if err != nil || len(again.Events) != 0 {
		t.Fatalf("replay after the change = %+v %v", again.Events, err)
	}
}

// Someone who is not a member is still refused the stream, status change or not.
func TestTodo_CHATBUG_075_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	adapter := NewAdapter(s)
	ctx := context.Background()
	if _, err := s.CommitChannelStatus(ctx, chatstateChange(chatpolicy.StatusLocked, 1), time.Now().UTC(), func(context.Context, chat.ChannelStatus) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, outsider := range []chat.Principal{{TenantID: "tenant-a", SubjectID: "stranger"}, {TenantID: "tenant-b", SubjectID: "u-1"}} {
		page, err := adapter.ReadConversationEvents(ctx, chat.WatchConversationRequest{Principal: outsider, TenantID: "tenant-a", ConversationID: "c-1"}, 0, 100)
		if !errors.Is(err, chat.ErrPermissionDenied) || len(page.Events) != 0 {
			t.Fatalf("%+v read the status change: %+v %v", outsider, page.Events, err)
		}
	}
}
