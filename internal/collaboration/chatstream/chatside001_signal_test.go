package chatstream

import (
	"context"
	"testing"
	"time"
)

type signalReader struct{}

func (signalReader) Read(context.Context, ReadRequest) (Page, error) {
	return Page{Complete: true}, nil
}

type signalAuth struct{}

func (signalAuth) Authorize(context.Context, Access) error { return nil }

func nextWithin(t *testing.T, sub *Subscription) (Event, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	event, err := sub.Next(ctx)
	return event, err == nil
}

// TestTodo_CHATSIDE_001_Signal proves the person-scoped notice reaches every
// live watch the person holds, whichever conversation it watches, reaches no one
// else, and leaves the subscriber's cursor where it was.
func TestTodo_CHATSIDE_001_Signal(t *testing.T) {
	s, err := New(Config{Key: []byte("k"), Reader: signalReader{}, Authorizer: signalAuth{}, QueueSize: 4, ReplayLimit: 4, CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	watch := func(subject, conversation string) *Subscription {
		sub, watchErr := s.Watch(context.Background(), WatchRequest{TenantID: "host", HomeTenantID: "home", SubjectID: subject, ConversationID: conversation, MembershipEpoch: 1})
		if watchErr != nil {
			t.Fatal(watchErr)
		}
		return sub
	}
	tabA, tabB, other := watch("alice", "general"), watch("alice", "random"), watch("bob", "general")
	cursorBefore := tabA.Cursor()

	if got := s.PublishSignal(context.Background(), "home", "alice", SignalSidebarLayoutChanged, []byte("7")); got != 2 {
		t.Fatalf("reached %d subscriptions, want alice's two", got)
	}
	for name, sub := range map[string]*Subscription{"tab on #general": tabA, "tab on #random": tabB} {
		event, ok := nextWithin(t, sub)
		if !ok || event.Signal != SignalSidebarLayoutChanged || string(event.Payload) != "7" {
			t.Fatalf("%s got %+v ok=%v", name, event, ok)
		}
	}
	if _, ok := nextWithin(t, other); ok {
		t.Fatal("another person's watch received alice's notice")
	}
	if tabA.Cursor() != cursorBefore {
		t.Fatal("the notice moved the subscriber's resume cursor")
	}
	if got := s.PublishSignal(context.Background(), "other-home", "alice", SignalSidebarLayoutChanged, nil); got != 0 {
		t.Fatalf("a notice for another home tenant reached %d watches", got)
	}
}

// TestTodo_CHATSIDE_001_SignalFullQueue proves a full queue drops the notice
// and keeps the subscription: it is only a hint to read again.
func TestTodo_CHATSIDE_001_SignalFullQueue(t *testing.T) {
	s, err := New(Config{Key: []byte("k"), Reader: signalReader{}, Authorizer: signalAuth{}, QueueSize: 2, ReplayLimit: 2, CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Watch(context.Background(), WatchRequest{TenantID: "host", HomeTenantID: "home", SubjectID: "alice", ConversationID: "general", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	reached := 0
	for i := 0; i < 5; i++ {
		reached += s.PublishSignal(context.Background(), "home", "alice", SignalSidebarLayoutChanged, []byte("1"))
	}
	if reached != 2 {
		t.Fatalf("queue of 2 accepted %d notices", reached)
	}
	select {
	case <-sub.Done():
		t.Fatal("a full queue closed the subscription")
	default:
	}
}
