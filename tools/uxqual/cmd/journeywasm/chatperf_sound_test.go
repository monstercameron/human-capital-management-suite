//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATBUG_014_SoundPoll: an idle sidebar costs one read of the
// conversation list per tick. A conversation is read for new messages only when
// the list reports a newer message than the one it was last read through, when
// it has never been read through, or when the list could not say.
func TestTodo_CHATBUG_014_SoundPoll(t *testing.T) {
	var seen chatperfSoundActivity
	t0 := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

	// Never read through: always read, whatever the list says.
	if chatperfSoundSkip(&seen, "room", "open", t0, true, true) {
		t.Fatal("a conversation that was never read to its end was skipped")
	}
	seen.settled("room", t0)

	// Idle: eighteen conversations with nothing new are eighteen skips.
	rooms := make([]string, 18)
	for i := range rooms {
		rooms[i] = "room-" + string(rune('a'+i))
		seen.settled(rooms[i], t0)
	}
	reads := 0
	for _, id := range rooms {
		if !chatperfSoundSkip(&seen, id, "open", t0, true, true) {
			reads++
		}
	}
	if reads != 0 {
		t.Fatalf("an idle tick read %d of 18 conversations, want none", reads)
	}

	// One of them gets a message: that one is read, the others are not.
	reads = 0
	for i, id := range rooms {
		newest := t0
		if i == 3 {
			newest = t0.Add(time.Minute)
		}
		if !chatperfSoundSkip(&seen, id, "open", newest, true, true) {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("one new message cost %d reads, want 1", reads)
	}

	// Until that read gets its answer the conversation keeps being read; once
	// it has, it is quiet again.
	later := t0.Add(time.Minute)
	if chatperfSoundSkip(&seen, rooms[3], "open", later, true, true) {
		t.Fatal("a conversation whose read has not succeeded was skipped: its sound would be lost")
	}
	seen.settled(rooms[3], later)
	if !chatperfSoundSkip(&seen, rooms[3], "open", later, true, true) {
		t.Fatal("a conversation read through its newest message is still being read")
	}

	// The cases that must always read.
	for name, skipped := range map[string]bool{
		"the list could not say":         chatperfSoundSkip(&seen, "room", "open", t0, false, true),
		"no starting point yet":          chatperfSoundSkip(&seen, "room", "open", t0, true, false),
		"the open conversation":          chatperfSoundSkip(&seen, "room", "room", t0, true, true),
		"a newer message than last read": chatperfSoundSkip(&seen, "room", "open", t0.Add(time.Second), true, true),
	} {
		if skipped {
			t.Errorf("%s: the conversation was skipped", name)
		}
	}

	// An older list answer does not move the mark back and invent news.
	seen.settled("room", t0.Add(-time.Hour))
	if !chatperfSoundSkip(&seen, "room", "open", t0, true, true) {
		t.Fatal("a stale list answer moved the read mark back")
	}
	// A conversation with no messages at all is quiet once it has been read.
	seen.settled("empty", time.Time{})
	if !chatperfSoundSkip(&seen, "empty", "open", time.Time{}, true, true) {
		t.Fatal("an empty conversation is read every tick")
	}
	seen.reset()
	if chatperfSoundSkip(&seen, "room", "open", t0, true, true) {
		t.Fatal("a new sign-in inherited the previous reader's marks")
	}

	// The poll asks the list once per tick, skips through chatperfSoundSkip, and
	// marks a conversation read only after a read that got its answer.
	poll := chatperfBody(t, "chat_sound_poll_wasm.go", "func pollChatSoundConversations(")
	chatperfInOrder(t, "pollChatSoundConversations", poll,
		"chatperfSoundListing(pollCtx, client, cfg)",
		"read := pollChatSoundConversation(",
		"read && listed && known",
		"chatperfSoundSeen.settled(",
		"chatperfSoundSkip(&chatperfSoundSeen,",
		"continue",
		"jobs <- conversation",
	)
	if strings.Count(poll, "chatperfSoundListing(") != 1 {
		t.Fatal("the conversation list is read more than once per tick")
	}
}
