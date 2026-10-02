package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

// TestTodo_CHATSIDE_001_PutSidebarPublishes proves a saved sidebar layout tells
// the person's own watches (here two conversations) the new revision, a refused
// save tells nobody, and another person's watch hears nothing.
func TestTodo_CHATSIDE_001_PutSidebarPublishes(t *testing.T) {
	config := runtimeConfig("test-key")
	config.Budgets.WatchConcurrent, config.Budgets.TenantConcurrent, config.Budgets.ConversationConcurrent = 4, 4, 4
	runtime, err := NewChatStreamRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	watch := func(subject, conversation string) *chatstream.Subscription {
		sub, lease, watchErr := runtime.Watch(context.Background(), chatstream.WatchRequest{TenantID: "home", HomeTenantID: "home", SubjectID: subject, ConversationID: conversation, MembershipEpoch: 1})
		if watchErr != nil {
			t.Fatal(watchErr)
		}
		t.Cleanup(func() { sub.Close(); _ = lease.Release() })
		return sub
	}
	tab := watch("member", "conv")
	stranger := watch("stranger", "conv")

	repo := &extensionRecipientRepo{t: t}
	s := &ChatExtensions{Recipients: &chatrecipient.Service{Repo: repo, Conversations: extensionConversations{}}, Admission: runtime}
	saved, err := s.PutSidebar(context.Background(), chat.Principal{TenantID: "home", SubjectID: "member"}, chatrecipient.Sidebar{Layout: []byte(`{"sections":[]}`)}, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := tab.Next(ctx)
	if err != nil || event.Signal != chatstream.SignalSidebarLayoutChanged || string(event.Payload) != "2" || saved.Revision != 2 {
		t.Fatalf("watch got %+v err=%v (saved revision %d)", event, err, saved.Revision)
	}
	watchEvent := chatSidebarSignalEvent(event, "cursor")
	if d := watchEvent.EphemeralDelivery; d == nil || d.ID != chatrecipient.SidebarChangedDeliveryID || d.Body != "2" || d.ThreadID != "" {
		t.Fatalf("watch event = %+v", watchEvent)
	}
	short, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	if event, err := stranger.Next(short); err == nil {
		t.Fatalf("another person's watch heard %+v", event)
	}

	// A refused save publishes nothing: the layout did not change.
	if _, err := s.PutSidebar(context.Background(), chat.Principal{TenantID: "home", SubjectID: "member"}, chatrecipient.Sidebar{Layout: []byte(`not json`)}, 2); err == nil {
		t.Fatal("invalid layout accepted")
	}
	short2, stop2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop2()
	if event, err := tab.Next(short2); err == nil {
		t.Fatalf("a refused save published %+v", event)
	}
}

// TestTodo_CHATSIDE_001_PublishWithoutStreaming proves a composition with
// streaming disabled saves and publishes to nobody.
func TestTodo_CHATSIDE_001_PublishWithoutStreaming(t *testing.T) {
	var runtime *ChatStreamRuntime
	if got := runtime.PublishSidebarChanged(context.Background(), chat.Principal{TenantID: "home", SubjectID: "member"}, 3); got != 0 {
		t.Fatalf("nil runtime reached %d", got)
	}
}
