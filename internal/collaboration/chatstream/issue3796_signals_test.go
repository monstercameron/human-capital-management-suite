package chatstream

import (
	"context"
	"testing"
	"time"
)

func TestIssue3796_CountsSignalRoundTripsAndRefusesEmpty(t *testing.T) {
	in := CountsSignal{Host: "t1", Conversation: "c1", Unread: 4, Mentions: 1}
	out, err := DecodeCountsSignal(EncodeCountsSignal(in))
	if err != nil || out != in {
		t.Fatalf("round trip = %+v, %v", out, err)
	}
	if _, err := DecodeCountsSignal([]byte(`{"u":3}`)); err == nil {
		t.Fatal("a notice naming no conversation was accepted")
	}
	if _, err := DecodeCountsSignal([]byte(`nope`)); err == nil {
		t.Fatal("garbage was accepted")
	}
	if !IsCountsSignal(SignalConversationReadChanged) || !IsCountsSignal(SignalConversationUnreadChanged) || IsCountsSignal(SignalSidebarLayoutChanged) {
		t.Fatal("IsCountsSignal misclassifies")
	}
}

func TestIssue3796_CountsSignalReachesOnlyThePersonsOwnTabs(t *testing.T) {
	s, err := New(Config{Key: []byte("k"), Reader: signalReader{}, Authorizer: signalAuth{}, QueueSize: 4, ReplayLimit: 4, CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	watch := func(home, subject, conversation string) *Subscription {
		sub, werr := s.Watch(context.Background(), WatchRequest{TenantID: "host", HomeTenantID: home, SubjectID: subject, ConversationID: conversation, MembershipEpoch: 1})
		if werr != nil {
			t.Fatal(werr)
		}
		return sub
	}
	a1, a2, bob, otherTenantAlice := watch("home", "alice", "general"), watch("home", "alice", "random"), watch("home", "bob", "general"), watch("away", "alice", "general")
	live := s.LiveSubjects()
	if len(live) != 3 {
		t.Fatalf("live subjects = %+v, want three distinct people", live)
	}
	payload := EncodeCountsSignal(CountsSignal{Host: "host", Conversation: "elsewhere", Unread: 2, Mentions: 1})
	if got := s.PublishSignal(context.Background(), "home", "alice", SignalConversationUnreadChanged, payload); got != 2 {
		t.Fatalf("reached %d, want alice's two tabs", got)
	}
	for _, sub := range []*Subscription{a1, a2} {
		event, ok := nextWithin(t, sub)
		if !ok || event.Signal != SignalConversationUnreadChanged {
			t.Fatalf("tab got %+v ok=%v", event, ok)
		}
		if got, derr := DecodeCountsSignal(event.Payload); derr != nil || got.Unread != 2 || got.Mentions != 1 {
			t.Fatalf("payload = %+v, %v", got, derr)
		}
	}
	if _, ok := nextWithin(t, bob); ok {
		t.Fatal("bob received alice's counts")
	}
	if _, ok := nextWithin(t, otherTenantAlice); ok {
		t.Fatal("a same-named person from another home tenant received the counts")
	}
}
