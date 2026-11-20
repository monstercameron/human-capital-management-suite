package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3942Messages(sequences ...uint64) []chatui.Message {
	out := make([]chatui.Message, 0, len(sequences))
	for _, sequence := range sequences {
		out = append(out, chatui.Message{ID: fmt.Sprintf("p%d", sequence), Sequence: sequence})
	}
	return out
}

func TestIssue3942_AnchorIsMessageBasedWithOffset(t *testing.T) {
	now := time.Now()
	anchor, ok := chatReadingAnchorFor(issue3942Messages(5, 6, 7), "p6", -18, now)
	if !ok || anchor.PostID != "p6" || anchor.Sequence != 6 || anchor.Offset != -18 {
		t.Fatalf("anchor = %+v ok=%v", anchor, ok)
	}
	if _, ok := chatReadingAnchorFor(issue3942Messages(5, 6, 7), "gone", 0, now); ok {
		t.Fatal("a message that is not loaded anchored a place")
	}
	if _, ok := chatReadingAnchorFor([]chatui.Message{{ID: "local"}}, "local", 0, now); ok {
		t.Fatal("a message with no sequence anchored a place")
	}
}

func TestIssue3942_ReturnTakesThePlaceOnceAndForgetsIt(t *testing.T) {
	var store chatReadingAnchorStore
	now := time.Now()
	store.remember("t1", "ana", "design", chatReadingAnchor{PostID: "p6", Sequence: 6, Offset: 4, SavedAt: now})
	got, ok := store.take("t1", "ana", "design", now.Add(time.Minute))
	if !ok || got.PostID != "p6" || got.Offset != 4 {
		t.Fatalf("take = %+v ok=%v", got, ok)
	}
	if _, ok := store.take("t1", "ana", "design", now.Add(time.Minute)); ok {
		t.Fatal("a place was offered twice")
	}
}

func TestIssue3942_PlaceIsPrivateToThePersonAndTenant_Security(t *testing.T) {
	var store chatReadingAnchorStore
	now := time.Now()
	store.remember("t1", "ana", "design", chatReadingAnchor{PostID: "p6", Sequence: 6, SavedAt: now})
	if _, ok := store.take("t1", "bo", "design", now); ok {
		t.Fatal("another person was offered ana's place")
	}
	// Reading as someone else wiped the previous person's places for good.
	store.remember("t1", "ana", "design", chatReadingAnchor{PostID: "p6", Sequence: 6, SavedAt: now})
	store.remember("t1", "bo", "ops", chatReadingAnchor{PostID: "p2", Sequence: 2, SavedAt: now})
	if _, ok := store.take("t1", "ana", "design", now); ok {
		t.Fatal("ana's place survived another person using the tab")
	}
	store.remember("t2", "bo", "ops", chatReadingAnchor{PostID: "p2", Sequence: 2, SavedAt: now})
	if _, ok := store.take("t1", "bo", "ops", now); ok {
		t.Fatal("a place crossed tenants")
	}
	store.clear()
	if store.size() != 0 {
		t.Fatal("clear left places behind")
	}
}

func TestIssue3942_PlacesAreBoundedAndExpire(t *testing.T) {
	var store chatReadingAnchorStore
	now := time.Now()
	for i := 0; i < chatReadingAnchorLimit+10; i++ {
		store.remember("t", "s", fmt.Sprintf("room-%d", i), chatReadingAnchor{PostID: "p", Sequence: 1, SavedAt: now})
	}
	if store.size() != chatReadingAnchorLimit {
		t.Fatalf("store holds %d places, limit %d", store.size(), chatReadingAnchorLimit)
	}
	if _, ok := store.take("t", "s", "room-0", now); ok {
		t.Fatal("the oldest place was kept past the bound")
	}
	if _, ok := store.take("t", "s", fmt.Sprintf("room-%d", chatReadingAnchorLimit+9), now); !ok {
		t.Fatal("the newest place was dropped")
	}
	store.remember("t", "s", "late", chatReadingAnchor{PostID: "p", Sequence: 1, SavedAt: now})
	if _, ok := store.take("t", "s", "late", now.Add(chatReadingAnchorTTL+time.Second)); ok {
		t.Fatal("an expired place was offered")
	}
	store.remember("t", "s", "cleared", chatReadingAnchor{PostID: "p", Sequence: 1, SavedAt: now})
	store.forget("cleared")
	if _, ok := store.take("t", "s", "cleared", now); ok {
		t.Fatal("a forgotten place (reader at the newest messages) was offered")
	}
}

func TestIssue3942_ReturnLandsOnTheMessageOrTheClosestOne(t *testing.T) {
	anchor := chatReadingAnchor{PostID: "p6", Sequence: 6, Offset: -12}
	id, status, offset, ok := chatReadingResolve(anchor, issue3942Messages(4, 5, 6, 7))
	if !ok || id != "p6" || status != chatui.ReadingResumed || offset != -12 {
		t.Fatalf("present: %q %q %d %v", id, status, offset, ok)
	}
	// Deleted or no longer readable: the first message after its position.
	id, status, offset, ok = chatReadingResolve(anchor, issue3942Messages(4, 5, 7, 8))
	if !ok || id != "p7" || status != chatui.ReadingNearby || offset != 0 {
		t.Fatalf("gone: %q %q %d %v", id, status, offset, ok)
	}
	// Everything after it is gone too: the closest one before.
	id, status, _, ok = chatReadingResolve(anchor, issue3942Messages(3, 4, 5))
	if !ok || id != "p5" || status != chatui.ReadingNearby {
		t.Fatalf("trimmed: %q %q %v", id, status, ok)
	}
	if _, _, _, ok = chatReadingResolve(anchor, nil); ok {
		t.Fatal("an empty window produced a landing")
	}
}

func TestIssue3942_UnreadTargetsAndUnreadableRoomsWinOverTheSavedPlace(t *testing.T) {
	for _, tc := range []struct {
		name     string
		unread   int
		explicit bool
		readable bool
		want     bool
	}{
		{"quiet room", 0, false, true, true},
		{"unread messages", 3, false, true, false},
		{"search result or link", 0, true, true, false},
		{"no longer readable", 0, false, false, false},
	} {
		if got := chatReadingMayResume(tc.unread, tc.explicit, tc.readable); got != tc.want {
			t.Errorf("%s: may resume = %v, want %v", tc.name, got, tc.want)
		}
	}
	rooms := []chatui.Conversation{{ID: "a", Unread: 2}, {ID: "b"}}
	if chatConversationUnread(rooms, "a") != 2 || chatConversationUnread(rooms, "b") != 0 || chatConversationUnread(rooms, "zzz") != 0 {
		t.Fatal("unread lookup wrong")
	}
}

func TestIssue3942_ASwitchClearsTheReturnBarOfTheRoomLeft(t *testing.T) {
	state := newChatStateForTest(t)
	state.selectChatConversation("design")
	state.mutate(func(m *chatui.Model) {
		m.ReadingResume = chatui.ReadingResumeView{ConversationID: "design", Status: chatui.ReadingResumed}
	})
	away, _ := state.selectChatConversation("engineering")
	if away.ReadingResume != (chatui.ReadingResumeView{}) {
		t.Fatalf("the bar about #design followed the reader to #engineering: %+v", away.ReadingResume)
	}
}
